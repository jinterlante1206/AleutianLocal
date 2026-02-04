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
	"fmt"
	"log/slog"
	"math"
	"sort"
	"sync"

	"github.com/AleutianAI/AleutianFOSS/services/trace/agent/mcts/crs"
)

// =============================================================================
// UCB1 Scorer (CRS-05)
// =============================================================================

// ToolScore represents a tool's UCB1-based selection score.
//
// Description:
//
//	Contains the breakdown of the UCB1 scoring components for transparency
//	and debugging. The final score is computed as:
//	  score = exploitation + exploration
//	  exploitation = router_confidence - proof_penalty
//	  exploration = C * sqrt(ln(total_selections) / tool_selections)
//
// Thread Safety: ToolScore is immutable after creation.
type ToolScore struct {
	// Tool is the tool name.
	Tool string `json:"tool"`

	// RouterConfidence is the router's confidence for this tool (0.0-1.0).
	RouterConfidence float64 `json:"router_confidence"`

	// ProofPenalty is the penalty based on proof number (0.0-1.0).
	// Higher proof number = higher penalty = harder to prove = less preferred.
	ProofPenalty float64 `json:"proof_penalty"`

	// ExplorationBonus is the UCB1 exploration term.
	// Encourages trying less-used tools.
	ExplorationBonus float64 `json:"exploration_bonus"`

	// FinalScore is the combined UCB1 score.
	// Higher score = more preferred tool.
	FinalScore float64 `json:"final_score"`

	// Blocked indicates if the tool is blocked by a learned clause.
	Blocked bool `json:"blocked"`

	// BlockReason explains why the tool is blocked.
	BlockReason string `json:"block_reason,omitempty"`

	// ProofStatus contains the proof index status if available.
	ProofStatus *crs.ProofNumber `json:"proof_status,omitempty"`
}

// UCB1Scorer scores tools using UCB1 formula with proof integration.
//
// Description:
//
//	UCB1 (Upper Confidence Bound) is the right algorithm for tool selection:
//	  - Exploits tools with high router confidence and low proof penalty
//	  - Explores less-used tools to discover alternatives
//	  - Respects learned clauses that block certain tool sequences
//
//	UCB1 Formula:
//	  score = exploitation + exploration
//	  exploitation = router_confidence - proof_penalty
//	  exploration = C * sqrt(ln(total_selections) / tool_selections)
//
//	Where:
//	  - router_confidence: From granite4:micro-h (0.0-1.0)
//	  - proof_penalty: Based on proof number (higher PN = more penalty)
//	  - C: Exploration constant (default sqrt(2) ≈ 1.41)
//	  - N: Total tool selections across all tools
//	  - n_i: Times this specific tool was selected
//
// Thread Safety: UCB1Scorer is safe for concurrent use.
type UCB1Scorer struct {
	// explorationConst is the exploration constant C in UCB1.
	// Default: sqrt(2) ≈ 1.41 (standard UCB1).
	explorationConst float64

	// proofWeight is the weight applied to proof penalty.
	// Default: 0.5 (proof penalty can reduce score by up to 50%).
	proofWeight float64

	// maxUnexploredBonus is the bonus for never-selected tools.
	// Default: 2.0 * explorationConst.
	maxUnexploredBonus float64

	// maxProofNumber is the maximum expected proof number for normalization.
	// Proof numbers are normalized to [0,1] by dividing by this value.
	// Default: 100.
	maxProofNumber float64

	// logger for debug output.
	logger *slog.Logger
}

// UCB1ScorerConfig configures the UCB1 scorer.
type UCB1ScorerConfig struct {
	// ExplorationConst is the exploration constant C.
	// Default: sqrt(2) ≈ 1.41.
	ExplorationConst float64

	// ProofWeight is the weight for proof penalty.
	// Default: 0.5.
	ProofWeight float64

	// MaxUnexploredBonus is the bonus for never-selected tools.
	// Default: 2.0 * ExplorationConst.
	MaxUnexploredBonus float64

	// MaxProofNumber is the maximum expected proof number for normalization.
	// Proof numbers are normalized to [0,1] by dividing by this value.
	// Default: 100.
	MaxProofNumber float64

	// Logger for debug output. If nil, uses default logger.
	Logger *slog.Logger
}

// DefaultMaxProofNumber is the default maximum proof number for normalization.
// Proof numbers in PN-MCTS typically range from 1-100 for most tool selections.
const DefaultMaxProofNumber = 100.0

// DefaultUCB1ScorerConfig returns the default UCB1 scorer configuration.
//
// Description:
//
//	Uses standard UCB1 exploration constant sqrt(2) and moderate proof weight.
//
// Outputs:
//
//	*UCB1ScorerConfig - Default configuration.
func DefaultUCB1ScorerConfig() *UCB1ScorerConfig {
	return &UCB1ScorerConfig{
		ExplorationConst:   math.Sqrt(2), // 1.41421356...
		ProofWeight:        0.5,
		MaxUnexploredBonus: 0,                     // Will be set to 2.0 * ExplorationConst if 0
		MaxProofNumber:     DefaultMaxProofNumber, // Default: 100
		Logger:             nil,
	}
}

// NewUCB1Scorer creates a new UCB1 scorer with default configuration.
//
// Outputs:
//
//	*UCB1Scorer - The scorer instance.
func NewUCB1Scorer() *UCB1Scorer {
	return NewUCB1ScorerWithConfig(DefaultUCB1ScorerConfig())
}

// NewUCB1ScorerWithConfig creates a new UCB1 scorer with the given configuration.
//
// Inputs:
//
//	config - The configuration. If nil, uses default.
//
// Outputs:
//
//	*UCB1Scorer - The scorer instance.
func NewUCB1ScorerWithConfig(config *UCB1ScorerConfig) *UCB1Scorer {
	if config == nil {
		config = DefaultUCB1ScorerConfig()
	}

	explorationConst := config.ExplorationConst
	if explorationConst <= 0 {
		explorationConst = math.Sqrt(2)
	}

	proofWeight := config.ProofWeight
	if proofWeight < 0 {
		proofWeight = 0.5
	}
	if proofWeight > 1.0 {
		proofWeight = 1.0
	}

	maxUnexploredBonus := config.MaxUnexploredBonus
	if maxUnexploredBonus <= 0 {
		maxUnexploredBonus = 2.0 * explorationConst
	}

	maxProofNumber := config.MaxProofNumber
	if maxProofNumber <= 0 {
		maxProofNumber = DefaultMaxProofNumber
	}

	logger := config.Logger
	if logger == nil {
		logger = slog.Default()
	}

	return &UCB1Scorer{
		explorationConst:   explorationConst,
		proofWeight:        proofWeight,
		maxUnexploredBonus: maxUnexploredBonus,
		maxProofNumber:     maxProofNumber,
		logger:             logger,
	}
}

// RouterResult represents a tool selection from the router.
//
// Description:
//
//	Used as input to ScoreTools to provide the router's initial ranking.
type RouterResult struct {
	// Tool is the tool name.
	Tool string

	// Confidence is the router's confidence (0.0-1.0).
	Confidence float64
}

// ClauseChecker checks if an assignment violates learned clauses.
//
// Description:
//
//	Interface for checking clause violations without tight coupling to CRS.
type ClauseChecker interface {
	// IsBlocked checks if the assignment violates any clause.
	//
	// Inputs:
	//   - assignment: The variable assignment to check.
	//
	// Outputs:
	//   - bool: True if blocked by a clause.
	//   - string: Reason for blocking.
	IsBlocked(assignment map[string]bool) (bool, string)
}

// clauseCheckerAdapter adapts ConstraintIndexView to ClauseChecker.
type clauseCheckerAdapter struct {
	view crs.ConstraintIndexView
}

// IsBlocked implements ClauseChecker.
func (a *clauseCheckerAdapter) IsBlocked(assignment map[string]bool) (bool, string) {
	result := a.view.CheckAssignment(assignment)
	if result.Conflict {
		return true, result.Reason
	}
	return false, ""
}

// NewClauseCheckerFromConstraintIndex creates a ClauseChecker from a ConstraintIndexView.
//
// Inputs:
//
//	view - The constraint index view.
//
// Outputs:
//
//	ClauseChecker - The adapter.
func NewClauseCheckerFromConstraintIndex(view crs.ConstraintIndexView) ClauseChecker {
	if view == nil {
		return nil
	}
	return &clauseCheckerAdapter{view: view}
}

// ScoreTools scores all tools using UCB1 and returns sorted by score.
//
// Description:
//
//	Applies the UCB1 formula to each tool, combining router confidence with
//	proof penalty and exploration bonus. Tools blocked by clauses get a
//	negative score and are sorted to the end.
//
// Inputs:
//
//	routerResults - Tool selections from router with confidence.
//	proofIndex - Proof numbers for each tool (can be nil).
//	selectionCounts - How many times each tool was selected.
//	clauseChecker - Learned clauses for blocking (can be nil).
//	currentAssignment - Current variable assignment for clause checking.
//
// Outputs:
//
//	[]ToolScore - Tools sorted by score (highest first, blocked last).
//
// Thread Safety: Safe for concurrent use.
func (s *UCB1Scorer) ScoreTools(
	routerResults []RouterResult,
	proofIndex crs.ProofIndexView,
	selectionCounts map[string]int,
	clauseChecker ClauseChecker,
	currentAssignment map[string]bool,
) []ToolScore {
	// Calculate total selections for exploration term
	totalSelections := 0
	for _, count := range selectionCounts {
		totalSelections += count
	}
	if totalSelections == 0 {
		totalSelections = 1 // Avoid log(0)
	}

	scores := make([]ToolScore, 0, len(routerResults))

	for _, result := range routerResults {
		// CR-8: Skip invalid entries with empty tool names
		if result.Tool == "" {
			s.logger.Debug("UCB1: skipping router result with empty tool name")
			continue
		}

		score := ToolScore{
			Tool:             result.Tool,
			RouterConfidence: result.Confidence,
		}

		// Check if blocked by learned clause
		if clauseChecker != nil && currentAssignment != nil {
			testAssignment := copyAssignment(currentAssignment)
			testAssignment["tool:"+result.Tool] = true

			if blocked, reason := clauseChecker.IsBlocked(testAssignment); blocked {
				score.Blocked = true
				score.BlockReason = reason
				score.FinalScore = -1.0 // Blocked tools get negative score
				scores = append(scores, score)

				s.logger.Debug("UCB1: tool blocked by clause",
					slog.String("tool", result.Tool),
					slog.String("reason", reason),
				)
				continue
			}
		}

		// Calculate proof penalty
		if proofIndex != nil {
			proofStatus, exists := proofIndex.Get(fmt.Sprintf("tool:%s", result.Tool))
			if exists {
				score.ProofStatus = &proofStatus

				// Disproven tools get maximum penalty
				if proofStatus.Status == crs.ProofStatusDisproven {
					score.ProofPenalty = s.proofWeight // Full penalty
				} else if proofStatus.Proof > 0 {
					// Higher proof number = higher cost = more penalty
					// Normalize to 0-1 range using configured max proof number
					normalizedProof := math.Min(1.0, float64(proofStatus.Proof)/s.maxProofNumber)
					score.ProofPenalty = normalizedProof * s.proofWeight
				}
			}
		}

		// Calculate exploration bonus (UCB1 term)
		toolSelections := selectionCounts[result.Tool]
		if toolSelections == 0 {
			// Never selected - maximum exploration bonus
			score.ExplorationBonus = s.maxUnexploredBonus
		} else {
			// UCB1 exploration term: C * sqrt(ln(N) / n_i)
			score.ExplorationBonus = s.explorationConst *
				math.Sqrt(math.Log(float64(totalSelections))/float64(toolSelections))
		}

		// Final score = exploitation + exploration
		// exploitation = router_confidence - proof_penalty
		exploitation := score.RouterConfidence - score.ProofPenalty
		score.FinalScore = exploitation + score.ExplorationBonus

		s.logger.Debug("UCB1: scored tool",
			slog.String("tool", result.Tool),
			slog.Float64("router_conf", score.RouterConfidence),
			slog.Float64("proof_penalty", score.ProofPenalty),
			slog.Float64("exploration", score.ExplorationBonus),
			slog.Float64("final_score", score.FinalScore),
		)

		scores = append(scores, score)
	}

	// Sort by final score (highest first)
	// Blocked tools (negative score) will naturally sort to the end
	sort.Slice(scores, func(i, j int) bool {
		return scores[i].FinalScore > scores[j].FinalScore
	})

	return scores
}

// SelectBest selects the best non-blocked tool from scored results.
//
// Description:
//
//	Returns the highest-scoring tool that is not blocked. If all tools are
//	blocked, returns empty string.
//
// Inputs:
//
//	scores - Sorted tool scores from ScoreTools.
//
// Outputs:
//
//	string - Best tool name, or empty if none available.
//	ToolScore - The selected tool's score, or zero value if none.
func (s *UCB1Scorer) SelectBest(scores []ToolScore) (string, ToolScore) {
	for _, score := range scores {
		if !score.Blocked {
			return score.Tool, score
		}
	}
	return "", ToolScore{}
}

// copyAssignment creates a copy of a variable assignment.
func copyAssignment(assignment map[string]bool) map[string]bool {
	if assignment == nil {
		return make(map[string]bool)
	}
	copy := make(map[string]bool, len(assignment))
	for k, v := range assignment {
		copy[k] = v
	}
	return copy
}

// =============================================================================
// Selection Counts Tracker
// =============================================================================

// SelectionCounts tracks tool selection counts for UCB1 exploration term.
//
// Description:
//
//	Maintains per-session tool selection counts for the UCB1 exploration term.
//	Selection counts are used to calculate the exploration bonus:
//	  exploration = C * sqrt(ln(total) / count)
//
// Thread Safety: Safe for concurrent use.
type SelectionCounts struct {
	mu     sync.RWMutex
	counts map[string]int
	total  int
}

// NewSelectionCounts creates a new selection counter.
//
// Outputs:
//
//	*SelectionCounts - The counter instance.
func NewSelectionCounts() *SelectionCounts {
	return &SelectionCounts{
		counts: make(map[string]int),
		total:  0,
	}
}

// Increment increases the count for a tool.
//
// Inputs:
//
//	tool - The tool name.
//
// Thread Safety: Safe for concurrent use.
func (c *SelectionCounts) Increment(tool string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.counts[tool]++
	c.total++
}

// Get returns the count for a tool.
//
// Inputs:
//
//	tool - The tool name.
//
// Outputs:
//
//	int - The selection count (0 if never selected).
//
// Thread Safety: Safe for concurrent use.
func (c *SelectionCounts) Get(tool string) int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.counts[tool]
}

// Total returns the total selections across all tools.
//
// Outputs:
//
//	int - Total selection count.
//
// Thread Safety: Safe for concurrent use.
func (c *SelectionCounts) Total() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.total
}

// AsMap returns the counts as a map.
//
// Outputs:
//
//	map[string]int - Copy of the counts map.
//
// Thread Safety: Safe for concurrent use.
func (c *SelectionCounts) AsMap() map[string]int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	result := make(map[string]int, len(c.counts))
	for k, v := range c.counts {
		result[k] = v
	}
	return result
}

// Reset clears all counts.
//
// Thread Safety: Safe for concurrent use.
func (c *SelectionCounts) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.counts = make(map[string]int)
	c.total = 0
}
