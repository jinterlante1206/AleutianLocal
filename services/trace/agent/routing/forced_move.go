// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package routing

import (
	"log/slog"

	"github.com/AleutianAI/AleutianFOSS/services/trace/agent/mcts/crs"
)

// =============================================================================
// Forced Move Detection (CRS-05 - Unit Propagation)
// =============================================================================

// ForcedMoveResult contains the result of forced move detection.
//
// Description:
//
//	When unit propagation determines that only one tool is viable (all others
//	are blocked by learned clauses), that tool is "forced". This allows the
//	agent to skip the router entirely in deterministic situations.
type ForcedMoveResult struct {
	// IsForced is true if exactly one tool is viable.
	IsForced bool `json:"is_forced"`

	// ForcedTool is the tool name if IsForced is true.
	ForcedTool string `json:"forced_tool,omitempty"`

	// ViableCount is the number of viable (non-blocked) tools.
	ViableCount int `json:"viable_count"`

	// BlockedTools lists tools blocked by clauses.
	BlockedTools []string `json:"blocked_tools,omitempty"`

	// BlockReasons maps blocked tool to the reason.
	BlockReasons map[string]string `json:"block_reasons,omitempty"`
}

// ForcedMoveChecker detects when only one tool is viable.
//
// Description:
//
//	Uses unit propagation logic from CDCL to detect forced moves.
//	A move is forced when all but one tool are blocked by learned clauses.
//
//	This is analogous to SAT unit propagation:
//	  - Each tool is a "variable"
//	  - Clauses constrain which tools can be used
//	  - When only one tool satisfies all clauses, it's forced
//
// Thread Safety: Safe for concurrent use (stateless).
type ForcedMoveChecker struct {
	logger *slog.Logger
}

// ForcedMoveCheckerConfig configures the checker.
type ForcedMoveCheckerConfig struct {
	// Logger for debug output. If nil, uses default.
	Logger *slog.Logger
}

// NewForcedMoveChecker creates a new forced move checker.
//
// Outputs:
//
//	*ForcedMoveChecker - The checker instance.
func NewForcedMoveChecker() *ForcedMoveChecker {
	return NewForcedMoveCheckerWithConfig(nil)
}

// NewForcedMoveCheckerWithConfig creates a checker with config.
//
// Inputs:
//
//	config - The configuration. If nil, uses defaults.
//
// Outputs:
//
//	*ForcedMoveChecker - The checker instance.
func NewForcedMoveCheckerWithConfig(config *ForcedMoveCheckerConfig) *ForcedMoveChecker {
	logger := slog.Default()
	if config != nil && config.Logger != nil {
		logger = config.Logger
	}

	return &ForcedMoveChecker{
		logger: logger,
	}
}

// CheckForcedMove uses unit propagation to detect if only one tool is viable.
//
// Description:
//
//	For each available tool, checks if selecting it would violate any
//	learned clause. If only one tool passes all clause checks, it's forced.
//
// Inputs:
//
//	availableTools - List of tool names that could be selected.
//	clauseChecker - The clause checker for violation detection.
//	currentAssignment - Current variable assignment (step history context).
//
// Outputs:
//
//	ForcedMoveResult - Result containing forced tool or viable count.
//
// Thread Safety: Safe for concurrent use.
func (c *ForcedMoveChecker) CheckForcedMove(
	availableTools []string,
	clauseChecker ClauseChecker,
	currentAssignment map[string]bool,
) ForcedMoveResult {
	result := ForcedMoveResult{
		BlockReasons: make(map[string]string),
	}

	if len(availableTools) == 0 {
		return result
	}

	// If no clause checker, all tools are viable
	if clauseChecker == nil {
		result.ViableCount = len(availableTools)
		return result
	}

	var viableTools []string

	for _, tool := range availableTools {
		// Build test assignment with this tool selected
		testAssignment := copyAssignment(currentAssignment)
		testAssignment["tool:"+tool] = true

		// Check if any clause blocks this tool
		blocked, reason := clauseChecker.IsBlocked(testAssignment)
		if blocked {
			result.BlockedTools = append(result.BlockedTools, tool)
			result.BlockReasons[tool] = reason

			c.logger.Debug("ForcedMoveChecker: tool blocked",
				slog.String("tool", tool),
				slog.String("reason", reason),
			)
		} else {
			viableTools = append(viableTools, tool)
		}
	}

	result.ViableCount = len(viableTools)

	// Check if exactly one tool is viable (forced move)
	if len(viableTools) == 1 {
		result.IsForced = true
		result.ForcedTool = viableTools[0]

		c.logger.Info("ForcedMoveChecker: forced move detected",
			slog.String("forced_tool", result.ForcedTool),
			slog.Int("blocked_count", len(result.BlockedTools)),
		)
	}

	return result
}

// CheckForcedMoveFromSteps is a convenience method using CRS step history.
//
// Description:
//
//	Builds the current assignment from step history and checks for forced moves.
//
// Inputs:
//
//	availableTools - List of tool names.
//	steps - Step history from CRS.
//	clauseChecker - The clause checker.
//
// Outputs:
//
//	ForcedMoveResult - Result containing forced tool or viable count.
//
// Thread Safety: Safe for concurrent use.
func (c *ForcedMoveChecker) CheckForcedMoveFromSteps(
	availableTools []string,
	steps []crs.StepRecord,
	clauseChecker ClauseChecker,
) ForcedMoveResult {
	assignment := BuildAssignmentFromSteps(steps)
	return c.CheckForcedMove(availableTools, clauseChecker, assignment)
}

// BuildAssignmentFromSteps builds a variable assignment from step history.
//
// Description:
//
//	Creates the assignment map used for clause checking. Includes:
//	  - prev_tool:<name> for the previous tool
//	  - outcome:<result> for the previous outcome
//	  - error:<category> for the previous error category
//	  - prev_prev_tool:<name> for the tool before previous
//
// Inputs:
//
//	steps - Step history from CRS.
//
// Outputs:
//
//	map[string]bool - The variable assignment.
//
// Thread Safety: Safe for concurrent use.
func BuildAssignmentFromSteps(steps []crs.StepRecord) map[string]bool {
	assignment := make(map[string]bool)

	n := len(steps)
	if n == 0 {
		return assignment
	}

	// Previous step context
	lastStep := steps[n-1]
	if lastStep.Tool != "" {
		assignment["prev_tool:"+lastStep.Tool] = true
	}
	if lastStep.Outcome != "" {
		assignment["outcome:"+string(lastStep.Outcome)] = true
	}
	if lastStep.ErrorCategory != "" {
		assignment["error:"+string(lastStep.ErrorCategory)] = true
	}

	// Previous-previous step context
	if n > 1 {
		prevPrevStep := steps[n-2]
		if prevPrevStep.Tool != "" {
			assignment["prev_prev_tool:"+prevPrevStep.Tool] = true
		}
	}

	return assignment
}

// =============================================================================
// All Tools Blocked Detection
// =============================================================================

// AllBlockedResult contains the result when all tools are blocked.
type AllBlockedResult struct {
	// AllBlocked is true if every tool is blocked by clauses.
	AllBlocked bool `json:"all_blocked"`

	// BlockedCount is the number of blocked tools.
	BlockedCount int `json:"blocked_count"`

	// TotalTools is the total number of tools checked.
	TotalTools int `json:"total_tools"`

	// Reasons maps tool names to block reasons.
	Reasons map[string]string `json:"reasons,omitempty"`
}

// CheckAllBlocked checks if all available tools are blocked.
//
// Description:
//
//	When all tools are blocked, the agent must synthesize an answer from
//	gathered information. This detects that situation.
//
// Inputs:
//
//	availableTools - List of tool names.
//	clauseChecker - The clause checker.
//	currentAssignment - Current variable assignment.
//
// Outputs:
//
//	AllBlockedResult - Result indicating if all blocked.
//
// Thread Safety: Safe for concurrent use.
func (c *ForcedMoveChecker) CheckAllBlocked(
	availableTools []string,
	clauseChecker ClauseChecker,
	currentAssignment map[string]bool,
) AllBlockedResult {
	result := AllBlockedResult{
		TotalTools: len(availableTools),
		Reasons:    make(map[string]string),
	}

	if clauseChecker == nil || len(availableTools) == 0 {
		return result
	}

	for _, tool := range availableTools {
		testAssignment := copyAssignment(currentAssignment)
		testAssignment["tool:"+tool] = true

		if blocked, reason := clauseChecker.IsBlocked(testAssignment); blocked {
			result.BlockedCount++
			result.Reasons[tool] = reason
		}
	}

	result.AllBlocked = result.BlockedCount == result.TotalTools

	if result.AllBlocked {
		c.logger.Warn("ForcedMoveChecker: all tools blocked",
			slog.Int("blocked_count", result.BlockedCount),
		)
	}

	return result
}
