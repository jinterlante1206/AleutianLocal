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
	"testing"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/trace/agent/mcts/crs"
)

// =============================================================================
// UCB1 Scorer Tests
// =============================================================================

func TestUCB1Scorer_NewDefault(t *testing.T) {
	scorer := NewUCB1Scorer()
	if scorer == nil {
		t.Fatal("NewUCB1Scorer returned nil")
	}
	if scorer.explorationConst <= 0 {
		t.Errorf("exploration const should be positive, got %f", scorer.explorationConst)
	}
	if scorer.proofWeight <= 0 || scorer.proofWeight > 1 {
		t.Errorf("proof weight should be in (0,1], got %f", scorer.proofWeight)
	}
}

func TestUCB1Scorer_ScoreTools_BasicScoring(t *testing.T) {
	scorer := NewUCB1Scorer()

	routerResults := []RouterResult{
		{Tool: "read_file", Confidence: 0.9},
		{Tool: "list_packages", Confidence: 0.7},
		{Tool: "run_tests", Confidence: 0.5},
	}

	// No proof index, no clauses, no selection history
	scores := scorer.ScoreTools(routerResults, nil, nil, nil, nil)

	if len(scores) != 3 {
		t.Fatalf("expected 3 scores, got %d", len(scores))
	}

	// All tools should be unblocked
	for _, score := range scores {
		if score.Blocked {
			t.Errorf("tool %s should not be blocked", score.Tool)
		}
	}

	// Higher confidence = higher score (since no proof penalty or selection history)
	// With no selections, all tools get max exploration bonus, so order is by confidence
	if scores[0].RouterConfidence < scores[1].RouterConfidence {
		t.Errorf("expected highest confidence first, got %s before %s",
			scores[0].Tool, scores[1].Tool)
	}
}

func TestUCB1Scorer_ScoreTools_WithSelectionCounts(t *testing.T) {
	scorer := NewUCB1Scorer()

	routerResults := []RouterResult{
		{Tool: "read_file", Confidence: 0.8},
		{Tool: "list_packages", Confidence: 0.8}, // Same confidence
	}

	selectionCounts := map[string]int{
		"read_file":     10, // Heavily used
		"list_packages": 1,  // Rarely used
	}

	scores := scorer.ScoreTools(routerResults, nil, selectionCounts, nil, nil)

	// list_packages should have higher exploration bonus due to lower selection count
	var readFileScore, listPackagesScore ToolScore
	for _, s := range scores {
		if s.Tool == "read_file" {
			readFileScore = s
		} else if s.Tool == "list_packages" {
			listPackagesScore = s
		}
	}

	if listPackagesScore.ExplorationBonus <= readFileScore.ExplorationBonus {
		t.Errorf("list_packages should have higher exploration bonus (%.4f) than read_file (%.4f)",
			listPackagesScore.ExplorationBonus, readFileScore.ExplorationBonus)
	}
}

func TestUCB1Scorer_ScoreTools_NeverSelected(t *testing.T) {
	scorer := NewUCB1Scorer()

	routerResults := []RouterResult{
		{Tool: "new_tool", Confidence: 0.5},      // Low confidence but never used
		{Tool: "familiar_tool", Confidence: 0.9}, // High confidence, used often
	}

	selectionCounts := map[string]int{
		"familiar_tool": 20,
		// new_tool not in map (never selected)
	}

	scores := scorer.ScoreTools(routerResults, nil, selectionCounts, nil, nil)

	var newToolScore, familiarToolScore ToolScore
	for _, s := range scores {
		if s.Tool == "new_tool" {
			newToolScore = s
		} else if s.Tool == "familiar_tool" {
			familiarToolScore = s
		}
	}

	// Never-selected tool should get max exploration bonus
	if newToolScore.ExplorationBonus != scorer.maxUnexploredBonus {
		t.Errorf("never-selected tool should get max exploration bonus, got %.4f, expected %.4f",
			newToolScore.ExplorationBonus, scorer.maxUnexploredBonus)
	}

	// Max exploration bonus might outweigh confidence difference
	if newToolScore.ExplorationBonus <= familiarToolScore.ExplorationBonus {
		t.Errorf("new tool exploration bonus (%.4f) should exceed familiar tool (%.4f)",
			newToolScore.ExplorationBonus, familiarToolScore.ExplorationBonus)
	}
}

func TestUCB1Scorer_ScoreTools_WithProofPenalty(t *testing.T) {
	scorer := NewUCB1Scorer()

	routerResults := []RouterResult{
		{Tool: "proven_tool", Confidence: 0.8},
		{Tool: "disproven_tool", Confidence: 0.8}, // Same confidence
	}

	// Mock proof index
	proofIndex := &mockProofIndex{
		proofs: map[string]crs.ProofNumber{
			"tool:proven_tool":    {Proof: 0, Status: crs.ProofStatusProven},
			"tool:disproven_tool": {Proof: 100, Status: crs.ProofStatusDisproven},
		},
	}

	scores := scorer.ScoreTools(routerResults, proofIndex, nil, nil, nil)

	var provenScore, disprovenScore ToolScore
	for _, s := range scores {
		if s.Tool == "proven_tool" {
			provenScore = s
		} else if s.Tool == "disproven_tool" {
			disprovenScore = s
		}
	}

	// Disproven tool should have max proof penalty
	if disprovenScore.ProofPenalty < provenScore.ProofPenalty {
		t.Errorf("disproven tool should have higher penalty (%.4f) than proven (%.4f)",
			disprovenScore.ProofPenalty, provenScore.ProofPenalty)
	}

	// Disproven tool should rank lower
	if disprovenScore.FinalScore >= provenScore.FinalScore {
		t.Errorf("disproven tool score (%.4f) should be lower than proven (%.4f)",
			disprovenScore.FinalScore, provenScore.FinalScore)
	}
}

func TestUCB1Scorer_ScoreTools_WithClauseBlocking(t *testing.T) {
	scorer := NewUCB1Scorer()

	routerResults := []RouterResult{
		{Tool: "allowed_tool", Confidence: 0.9},
		{Tool: "blocked_tool", Confidence: 0.95}, // Higher confidence but blocked
	}

	// Mock clause checker that blocks blocked_tool
	clauseChecker := &mockClauseChecker{
		blockedTools: map[string]string{
			"tool:blocked_tool": "violates clause: (¬tool:blocked_tool ∨ ¬prev_tool:A)",
		},
	}

	currentAssignment := map[string]bool{
		"prev_tool:A": true, // This triggers the clause
	}

	scores := scorer.ScoreTools(routerResults, nil, nil, clauseChecker, currentAssignment)

	// Blocked tool should be last (negative score)
	if len(scores) != 2 {
		t.Fatalf("expected 2 scores, got %d", len(scores))
	}

	// Blocked tool should have blocked flag and negative score
	var blockedScore ToolScore
	for _, s := range scores {
		if s.Tool == "blocked_tool" {
			blockedScore = s
		}
	}

	if !blockedScore.Blocked {
		t.Error("blocked_tool should have Blocked=true")
	}
	if blockedScore.FinalScore >= 0 {
		t.Errorf("blocked tool should have negative score, got %.4f", blockedScore.FinalScore)
	}

	// Non-blocked tool should be first
	if scores[0].Tool != "allowed_tool" {
		t.Errorf("allowed_tool should be ranked first, got %s", scores[0].Tool)
	}
}

func TestUCB1Scorer_SelectBest(t *testing.T) {
	scorer := NewUCB1Scorer()

	scores := []ToolScore{
		{Tool: "best", FinalScore: 2.5, Blocked: false},
		{Tool: "second", FinalScore: 2.0, Blocked: false},
		{Tool: "blocked", FinalScore: -1, Blocked: true},
	}

	tool, selected := scorer.SelectBest(scores)

	if tool != "best" {
		t.Errorf("expected best tool, got %s", tool)
	}
	if selected.FinalScore != 2.5 {
		t.Errorf("expected score 2.5, got %.4f", selected.FinalScore)
	}
}

func TestUCB1Scorer_SelectBest_AllBlocked(t *testing.T) {
	scorer := NewUCB1Scorer()

	scores := []ToolScore{
		{Tool: "blocked1", FinalScore: -1, Blocked: true},
		{Tool: "blocked2", FinalScore: -1, Blocked: true},
	}

	tool, _ := scorer.SelectBest(scores)

	if tool != "" {
		t.Errorf("expected empty tool when all blocked, got %s", tool)
	}
}

// =============================================================================
// Tool Selection Cache Tests
// =============================================================================

func TestToolSelectionCache_BasicOperations(t *testing.T) {
	cache := NewToolSelectionCache()

	// Put and Get
	cache.Put("key1", "read_file", 1.5, 100)

	tool, score, ok := cache.Get("key1", 100)
	if !ok {
		t.Fatal("expected cache hit")
	}
	if tool != "read_file" || score != 1.5 {
		t.Errorf("unexpected values: tool=%s, score=%.2f", tool, score)
	}
}

func TestToolSelectionCache_GenerationInvalidation(t *testing.T) {
	cache := NewToolSelectionCache()

	cache.Put("key1", "read_file", 1.5, 100)

	// Should miss with different generation
	_, _, ok := cache.Get("key1", 101)
	if ok {
		t.Error("expected cache miss with different generation")
	}
}

func TestToolSelectionCache_TTLExpiration(t *testing.T) {
	config := DefaultToolSelectionCacheConfig()
	config.TTL = 10 * time.Millisecond
	cache := NewToolSelectionCacheWithConfig(config)

	cache.Put("key1", "read_file", 1.5, 100)

	// Wait for TTL
	time.Sleep(20 * time.Millisecond)

	_, _, ok := cache.Get("key1", 100)
	if ok {
		t.Error("expected cache miss after TTL")
	}
}

func TestToolSelectionCache_MaxLenEviction(t *testing.T) {
	config := DefaultToolSelectionCacheConfig()
	config.MaxLen = 2
	cache := NewToolSelectionCacheWithConfig(config)

	cache.Put("key1", "tool1", 1.0, 100)
	time.Sleep(1 * time.Millisecond) // Ensure different timestamps
	cache.Put("key2", "tool2", 2.0, 100)
	time.Sleep(1 * time.Millisecond)
	cache.Put("key3", "tool3", 3.0, 100) // Should evict key1 (oldest)

	if cache.Size() != 2 {
		t.Errorf("expected size 2, got %d", cache.Size())
	}

	// key1 should be evicted
	_, _, ok := cache.Get("key1", 100)
	if ok {
		t.Error("key1 should have been evicted")
	}

	// key2 and key3 should still exist
	_, _, ok = cache.Get("key2", 100)
	if !ok {
		t.Error("key2 should still exist")
	}
	_, _, ok = cache.Get("key3", 100)
	if !ok {
		t.Error("key3 should still exist")
	}
}

func TestToolSelectionCache_Metrics(t *testing.T) {
	cache := NewToolSelectionCache()

	cache.Put("key1", "tool1", 1.0, 100)

	// One hit
	cache.Get("key1", 100)

	// One miss (wrong key)
	cache.Get("key2", 100)

	// One invalidation (wrong generation)
	cache.Get("key1", 101)

	metrics := cache.Metrics()
	if metrics.Hits != 1 {
		t.Errorf("expected 1 hit, got %d", metrics.Hits)
	}
	if metrics.Misses < 2 {
		t.Errorf("expected at least 2 misses, got %d", metrics.Misses)
	}
}

// =============================================================================
// State Key Builder Tests
// =============================================================================

func TestStateKeyBuilder_BuildKey(t *testing.T) {
	builder := NewStateKeyBuilder()

	steps := []crs.StepRecord{
		{
			Decision: crs.DecisionExecuteTool,
			Tool:     "list_packages",
			Outcome:  crs.OutcomeSuccess,
		},
		{
			Decision: crs.DecisionExecuteTool,
			Tool:     "read_file",
			Outcome:  crs.OutcomeFailure,
		},
	}

	key := builder.BuildKey(steps, 42)

	expected := "gen:42|list_packages:success→read_file:failure"
	if key != expected {
		t.Errorf("expected key %q, got %q", expected, key)
	}
}

func TestStateKeyBuilder_SkipsNonExecuteDecisions(t *testing.T) {
	builder := NewStateKeyBuilder()

	steps := []crs.StepRecord{
		{Decision: crs.DecisionSelectTool, Tool: "selected"}, // Should be skipped
		{Decision: crs.DecisionExecuteTool, Tool: "executed", Outcome: crs.OutcomeSuccess},
	}

	key := builder.BuildKey(steps, 1)

	if key != "gen:1|executed:success" {
		t.Errorf("unexpected key: %q", key)
	}
}

func TestStateKeyBuilder_EmptySteps(t *testing.T) {
	builder := NewStateKeyBuilder()

	key := builder.BuildKey(nil, 5)

	if key != "gen:5|" {
		t.Errorf("unexpected key for empty steps: %q", key)
	}
}

// =============================================================================
// Forced Move Checker Tests
// =============================================================================

func TestForcedMoveChecker_NoClauseChecker(t *testing.T) {
	checker := NewForcedMoveChecker()

	result := checker.CheckForcedMove(
		[]string{"tool1", "tool2", "tool3"},
		nil, // No clause checker
		nil,
	)

	if result.IsForced {
		t.Error("should not be forced without clause checker")
	}
	if result.ViableCount != 3 {
		t.Errorf("expected 3 viable, got %d", result.ViableCount)
	}
}

func TestForcedMoveChecker_SingleViable(t *testing.T) {
	checker := NewForcedMoveChecker()

	clauseChecker := &mockClauseChecker{
		blockedTools: map[string]string{
			"tool:tool1": "blocked by clause 1",
			"tool:tool2": "blocked by clause 2",
			// tool3 is not blocked
		},
	}

	result := checker.CheckForcedMove(
		[]string{"tool1", "tool2", "tool3"},
		clauseChecker,
		map[string]bool{},
	)

	if !result.IsForced {
		t.Error("expected forced move when only one tool viable")
	}
	if result.ForcedTool != "tool3" {
		t.Errorf("expected forced tool=tool3, got %s", result.ForcedTool)
	}
	if result.ViableCount != 1 {
		t.Errorf("expected 1 viable, got %d", result.ViableCount)
	}
	if len(result.BlockedTools) != 2 {
		t.Errorf("expected 2 blocked, got %d", len(result.BlockedTools))
	}
}

func TestForcedMoveChecker_MultipleViable(t *testing.T) {
	checker := NewForcedMoveChecker()

	clauseChecker := &mockClauseChecker{
		blockedTools: map[string]string{
			"tool:tool1": "blocked",
		},
	}

	result := checker.CheckForcedMove(
		[]string{"tool1", "tool2", "tool3"},
		clauseChecker,
		map[string]bool{},
	)

	if result.IsForced {
		t.Error("should not be forced with multiple viable tools")
	}
	if result.ViableCount != 2 {
		t.Errorf("expected 2 viable, got %d", result.ViableCount)
	}
}

func TestForcedMoveChecker_AllBlocked(t *testing.T) {
	checker := NewForcedMoveChecker()

	clauseChecker := &mockClauseChecker{
		blockedTools: map[string]string{
			"tool:tool1": "blocked",
			"tool:tool2": "blocked",
		},
	}

	result := checker.CheckAllBlocked(
		[]string{"tool1", "tool2"},
		clauseChecker,
		map[string]bool{},
	)

	if !result.AllBlocked {
		t.Error("expected all blocked")
	}
	if result.BlockedCount != 2 {
		t.Errorf("expected 2 blocked, got %d", result.BlockedCount)
	}
}

func TestBuildAssignmentFromSteps(t *testing.T) {
	steps := []crs.StepRecord{
		{
			Tool:          "tool_A",
			Outcome:       crs.OutcomeSuccess,
			ErrorCategory: crs.ErrorCategoryNone,
		},
		{
			Tool:          "tool_B",
			Outcome:       crs.OutcomeFailure,
			ErrorCategory: crs.ErrorCategoryTimeout,
		},
	}

	assignment := BuildAssignmentFromSteps(steps)

	// Check last step values
	if !assignment["prev_tool:tool_B"] {
		t.Error("expected prev_tool:tool_B to be true")
	}
	if !assignment["outcome:failure"] {
		t.Error("expected outcome:failure to be true")
	}
	if !assignment["error:timeout"] {
		t.Error("expected error:timeout to be true")
	}

	// Check previous-previous step
	if !assignment["prev_prev_tool:tool_A"] {
		t.Error("expected prev_prev_tool:tool_A to be true")
	}
}

// =============================================================================
// Selection Counts Tests
// =============================================================================

func TestSelectionCounts_BasicOperations(t *testing.T) {
	counts := NewSelectionCounts()

	counts.Increment("read_file")
	counts.Increment("read_file")
	counts.Increment("list_packages")

	if counts.Get("read_file") != 2 {
		t.Errorf("expected 2, got %d", counts.Get("read_file"))
	}
	if counts.Get("list_packages") != 1 {
		t.Errorf("expected 1, got %d", counts.Get("list_packages"))
	}
	if counts.Get("unknown") != 0 {
		t.Errorf("expected 0 for unknown, got %d", counts.Get("unknown"))
	}
	if counts.Total() != 3 {
		t.Errorf("expected total 3, got %d", counts.Total())
	}
}

func TestSelectionCounts_Reset(t *testing.T) {
	counts := NewSelectionCounts()

	counts.Increment("read_file")
	counts.Reset()

	if counts.Get("read_file") != 0 {
		t.Error("expected 0 after reset")
	}
	if counts.Total() != 0 {
		t.Error("expected total 0 after reset")
	}
}

func TestSelectionCounts_AsMap(t *testing.T) {
	counts := NewSelectionCounts()
	counts.Increment("a")
	counts.Increment("b")

	m := counts.AsMap()

	// Modify returned map - shouldn't affect original
	m["a"] = 999

	if counts.Get("a") == 999 {
		t.Error("AsMap should return a copy")
	}
}

// =============================================================================
// Mock Types
// =============================================================================

type mockProofIndex struct {
	proofs map[string]crs.ProofNumber
}

func (m *mockProofIndex) Get(nodeID string) (crs.ProofNumber, bool) {
	pn, ok := m.proofs[nodeID]
	return pn, ok
}

func (m *mockProofIndex) All() map[string]crs.ProofNumber {
	return m.proofs
}

func (m *mockProofIndex) Size() int {
	return len(m.proofs)
}

type mockClauseChecker struct {
	blockedTools map[string]string // tool variable -> reason
}

func (m *mockClauseChecker) IsBlocked(assignment map[string]bool) (bool, string) {
	for variable, reason := range m.blockedTools {
		if assignment[variable] {
			return true, reason
		}
	}
	return false, ""
}

// =============================================================================
// Benchmarks
// =============================================================================

func BenchmarkUCB1Scorer_ScoreTools(b *testing.B) {
	scorer := NewUCB1Scorer()

	// Create 10 tools
	routerResults := make([]RouterResult, 10)
	for i := 0; i < 10; i++ {
		routerResults[i] = RouterResult{
			Tool:       "tool_" + string(rune('A'+i)),
			Confidence: 0.5 + float64(i)*0.05,
		}
	}

	selectionCounts := make(map[string]int)
	for i := 0; i < 10; i++ {
		selectionCounts["tool_"+string(rune('A'+i))] = i + 1
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		scorer.ScoreTools(routerResults, nil, selectionCounts, nil, nil)
	}
}

func BenchmarkToolSelectionCache_GetPut(b *testing.B) {
	cache := NewToolSelectionCache()

	// Pre-populate
	for i := 0; i < 100; i++ {
		cache.Put("key_"+string(rune(i)), "tool", 1.0, 1)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache.Get("key_50", 1)
		cache.Put("new_key", "tool", 1.0, 1)
	}
}
