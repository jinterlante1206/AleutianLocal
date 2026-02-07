// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package phases

// execute_routing.go contains UCB1 tool selection and routing functions
// extracted from execute.go as part of CB-30c Phase 2 decomposition.

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/trace/agent"
	"github.com/AleutianAI/AleutianFOSS/services/trace/agent/mcts/crs"
	"github.com/AleutianAI/AleutianFOSS/services/trace/agent/routing"
)

// =============================================================================
// UCB1 Session Context Management
// =============================================================================

// ucb1SessionContexts stores per-session UCB1 contexts.
// Key is session ID, value is *ucb1SelectionContext.
var ucb1SessionContexts = &sync.Map{}

// getUCB1Context returns or creates the UCB1 context for a session.
func getUCB1Context(sessionID string) *ucb1SelectionContext {
	if ctx, ok := ucb1SessionContexts.Load(sessionID); ok {
		return ctx.(*ucb1SelectionContext)
	}
	newCtx := initUCB1Context()
	actual, _ := ucb1SessionContexts.LoadOrStore(sessionID, newCtx)
	return actual.(*ucb1SelectionContext)
}

// cleanupUCB1Context removes the UCB1 context for a session.
// Call this when a session ends.
func cleanupUCB1Context(sessionID string) {
	ucb1SessionContexts.Delete(sessionID)
}

// CleanupUCB1Context removes the UCB1 context for a session.
// This is the exported version for use by the agent loop during session cleanup.
//
// Description:
//
//	Removes all UCB1-related state (scorer, selection counts, cache, key builder)
//	for the specified session. Should be called when a session is definitively
//	closed (not just completed, as sessions can be continued with follow-up questions).
//
// Inputs:
//
//	sessionID - The session ID to clean up.
//
// Thread Safety: Safe for concurrent use.
func CleanupUCB1Context(sessionID string) {
	cleanupUCB1Context(sessionID)
}

// init registers the UCB1 cleanup hook with the agent loop.
// This ensures UCB1 session state is cleaned up when sessions are closed.
func init() {
	agent.RegisterSessionCleanupHook("ucb1", cleanupUCB1Context)
}

// =============================================================================
// UCB1-Enhanced Tool Selection (CRS-05)
// =============================================================================

// ucb1SelectionContext holds UCB1 selection state for a session.
type ucb1SelectionContext struct {
	scorer            *routing.UCB1Scorer
	cache             *routing.ToolSelectionCache
	forcedMoveChecker *routing.ForcedMoveChecker
	selectionCounts   *routing.SelectionCounts
	stateKeyBuilder   *routing.StateKeyBuilder
}

// initUCB1Context initializes the UCB1 selection context for a session.
//
// Description:
//
//	Creates the UCB1 scorer, cache, and forced move checker. Called once
//	per session when UCB1-enhanced selection is first used.
//
// Outputs:
//
//	*ucb1SelectionContext - The initialized context.
func initUCB1Context() *ucb1SelectionContext {
	return &ucb1SelectionContext{
		scorer:            routing.NewUCB1Scorer(),
		cache:             routing.NewToolSelectionCache(),
		forcedMoveChecker: routing.NewForcedMoveChecker(),
		selectionCounts:   routing.NewSelectionCounts(),
		stateKeyBuilder:   routing.NewStateKeyBuilder(),
	}
}

// selectToolWithUCB1 enhances router selection with UCB1 scoring.
//
// Description:
//
//	Implements the CRS-05 UCB1-enhanced tool selection flow:
//	1. Check cache for cached selection
//	2. Check for forced move (unit propagation)
//	3. Get router's tool suggestion
//	4. Score all tools using UCB1
//	5. Select best non-blocked tool
//	6. Cache result
//
// Inputs:
//
//	ctx - Context for cancellation.
//	deps - Phase dependencies.
//	ucb1Ctx - UCB1 selection context.
//	routerSelection - Initial selection from router.
//	availableTools - List of available tool names.
//
// Outputs:
//
//	*agent.ToolRouterSelection - Enhanced selection (may differ from router).
//	bool - True if selection was modified by UCB1.
func (p *ExecutePhase) selectToolWithUCB1(
	ctx context.Context,
	deps *Dependencies,
	ucb1Ctx *ucb1SelectionContext,
	routerSelection *agent.ToolRouterSelection,
	availableTools []string,
) (*agent.ToolRouterSelection, bool) {
	startTime := time.Now()
	defer func() {
		routing.RecordUCB1ScoringLatency(time.Since(startTime).Seconds())
	}()

	// Get CRS for proof index and clause checking
	var proofIndex crs.ProofIndexView
	var clauseChecker routing.ClauseChecker
	var generation int64

	if deps.Session != nil && deps.Session.HasCRS() {
		crsInstance := deps.Session.GetCRS()
		snapshot := crsInstance.Snapshot()
		proofIndex = snapshot.ProofIndex()
		clauseChecker = routing.NewClauseCheckerFromConstraintIndex(snapshot.ConstraintIndex())
		generation = crsInstance.Generation()
	}

	// Get step history for assignment building
	var steps []crs.StepRecord
	if deps.Session != nil && deps.Session.HasCRS() {
		steps = deps.Session.GetCRS().GetStepHistory(deps.Session.ID)
	}

	// Build current assignment from step history
	currentAssignment := routing.BuildAssignmentFromSteps(steps)

	// Step 1: Check cache
	cacheKey := ucb1Ctx.stateKeyBuilder.BuildKey(steps, generation)
	if tool, score, ok := ucb1Ctx.cache.Get(cacheKey, generation); ok {
		routing.RecordUCB1CacheHit()
		slog.Debug("CRS-05: UCB1 cache hit",
			slog.String("session_id", deps.Session.ID),
			slog.String("tool", tool),
			slog.Float64("score", score),
		)
		return &agent.ToolRouterSelection{
			Tool:       tool,
			Confidence: score,
			Reasoning:  "UCB1 cache hit",
			Duration:   time.Since(startTime),
		}, true
	}
	routing.RecordUCB1CacheMiss()

	// Step 2: Check for forced move (unit propagation)
	// This also gives us the viable count to detect all-blocked scenario
	forcedResult := ucb1Ctx.forcedMoveChecker.CheckForcedMove(
		availableTools,
		clauseChecker,
		currentAssignment,
	)

	if forcedResult.IsForced {
		routing.RecordUCB1ForcedMove(forcedResult.ForcedTool)
		slog.Info("CRS-05: Forced move detected",
			slog.String("session_id", deps.Session.ID),
			slog.String("forced_tool", forcedResult.ForcedTool),
			slog.Int("blocked_count", len(forcedResult.BlockedTools)),
		)

		// Cache the forced selection
		ucb1Ctx.cache.Put(cacheKey, forcedResult.ForcedTool, 1.0, generation)
		ucb1Ctx.selectionCounts.Increment(forcedResult.ForcedTool)

		return &agent.ToolRouterSelection{
			Tool:       forcedResult.ForcedTool,
			Confidence: 1.0,
			Reasoning:  fmt.Sprintf("Forced move: only viable tool (blocked %d others)", len(forcedResult.BlockedTools)),
			Duration:   time.Since(startTime),
		}, true
	}

	// Step 3: Check if all tools are blocked (reuse forcedResult to avoid duplicate work)
	// CR-4 fix: Use ViableCount from CheckForcedMove instead of calling CheckAllBlocked
	if forcedResult.ViableCount == 0 && len(availableTools) > 0 {
		routing.RecordUCB1AllBlocked()
		slog.Warn("CRS-05: All tools blocked by clauses",
			slog.String("session_id", deps.Session.ID),
			slog.Int("blocked_count", len(forcedResult.BlockedTools)),
		)
		return &agent.ToolRouterSelection{
			Tool:       "answer",
			Confidence: 0.7,
			Reasoning:  "All tools blocked by learned clauses - synthesizing answer",
			Duration:   time.Since(startTime),
		}, true
	}

	// Step 4: Build router results for UCB1 scoring
	// Use the router's selection as primary, give other tools lower base confidence
	routerResults := make([]routing.RouterResult, 0, len(availableTools))

	for _, tool := range availableTools {
		confidence := 0.3 // Base confidence for non-selected tools
		if tool == routerSelection.Tool {
			confidence = routerSelection.Confidence
		}
		routerResults = append(routerResults, routing.RouterResult{
			Tool:       tool,
			Confidence: confidence,
		})
	}

	// Step 5: Score tools using UCB1 with semantic awareness (C1.2 Fix)
	// Build semantic history from session for duplicate detection
	var toolHistory *routing.ToolCallHistory
	if deps.Session != nil {
		toolHistory = buildSemanticToolHistoryFromSession(deps.Session)
	}

	scores := ucb1Ctx.scorer.ScoreToolsWithSemantic(
		routerResults,
		proofIndex,
		ucb1Ctx.selectionCounts.AsMap(),
		clauseChecker,
		currentAssignment,
		toolHistory,
		routerSelection.Tool, // proposed tool
		deps.Query,           // proposed query for semantic comparison
	)

	// Step 6: Select best non-blocked tool
	bestTool, bestScore := ucb1Ctx.scorer.SelectBest(scores)

	if bestTool == "" {
		// All tools blocked (shouldn't happen after allBlockedResult check, but defensive)
		return &agent.ToolRouterSelection{
			Tool:       "answer",
			Confidence: 0.7,
			Reasoning:  "No viable tools available",
			Duration:   time.Since(startTime),
		}, true
	}

	// Record metrics for selected tool
	routing.RecordUCB1Selection(bestTool, bestScore.FinalScore, bestScore.ProofPenalty, bestScore.ExplorationBonus)

	// Record blocked tools (C1.2 Fix: include semantic blocking)
	for _, score := range scores {
		if score.Blocked {
			reason := "clause_violation"
			if score.SimilarCall != nil {
				reason = "semantic_duplicate"
			}
			routing.RecordUCB1BlockedSelection(score.Tool, reason)
		}
	}

	// Update selection count
	ucb1Ctx.selectionCounts.Increment(bestTool)

	// Cache the selection
	ucb1Ctx.cache.Put(cacheKey, bestTool, bestScore.FinalScore, generation)

	// Check if UCB1 changed the selection
	modified := bestTool != routerSelection.Tool

	if modified {
		slog.Info("CRS-05: UCB1 modified tool selection",
			slog.String("session_id", deps.Session.ID),
			slog.String("router_suggested", routerSelection.Tool),
			slog.String("ucb1_selected", bestTool),
			slog.Float64("router_conf", routerSelection.Confidence),
			slog.Float64("ucb1_score", bestScore.FinalScore),
			slog.Float64("proof_penalty", bestScore.ProofPenalty),
			slog.Float64("exploration_bonus", bestScore.ExplorationBonus),
		)
	} else {
		slog.Debug("CRS-05: UCB1 confirmed router selection",
			slog.String("session_id", deps.Session.ID),
			slog.String("tool", bestTool),
			slog.Float64("ucb1_score", bestScore.FinalScore),
		)
	}

	return &agent.ToolRouterSelection{
		Tool:       bestTool,
		Confidence: bestScore.FinalScore,
		Reasoning:  fmt.Sprintf("UCB1: router_conf=%.2f, proof_penalty=%.2f, exploration=%.2f", bestScore.RouterConfidence, bestScore.ProofPenalty, bestScore.ExplorationBonus),
		Duration:   time.Since(startTime),
	}, modified
}
