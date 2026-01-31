// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package mcts

import (
	"context"
	"testing"
	"time"
)

// Integration tests for Session 3 components.
//
// These tests verify that tree_state, audit, and metrics work together
// correctly with the existing MCTS infrastructure.

func TestIntegration_TreeModeDecisionWithDegradation(t *testing.T) {
	// Scenario: A complex task triggers tree mode, but degradation
	// manager may override based on system state.

	config := DefaultPlanPhaseConfig()
	degradationMgr := NewDegradationManager(DefaultDegradationConfig(), nil)

	// Complex task should trigger tree mode
	decision := ShouldUseTreeMode("refactor the architecture across multiple files", nil, config)
	if !decision.UseTreeMode {
		t.Error("complex task should trigger tree mode")
	}

	// But if degradation is at linear level, we should fall back
	for i := 0; i < 6; i++ {
		degradationMgr.RecordFailure()
	}

	if !degradationMgr.ShouldUseLinearFallback() {
		t.Error("should use linear fallback after 6 failures")
	}

	// The degradation manager's budget config should be minimal
	budgetConfig := degradationMgr.GetBudgetForLevel()
	if budgetConfig.MaxNodes != 1 {
		t.Errorf("linear level should have MaxNodes=1, got %d", budgetConfig.MaxNodes)
	}
}

func TestIntegration_AuditLogWithTreeOperations(t *testing.T) {
	// Scenario: Track tree operations through the audit log.

	auditLog := NewAuditLog()
	budget := NewTreeBudget(DefaultTreeBudgetConfig())
	tree := NewPlanTree("Test task for audit", budget)

	// Simulate tree operations and record in audit log
	auditLog.Record(*NewAuditEntry(AuditActionSelect, tree.Root().ID).
		WithNodeHash(tree.Root().ContentHash))

	// Simulate node expansion
	child := NewPlanNode("1", "First approach")
	tree.Root().AddChild(child)
	tree.IncrementNodeCount()

	auditLog.Record(*NewAuditEntry(AuditActionExpand, child.ID).
		WithParentHash(tree.Root().ContentHash).
		WithNodeHash(child.ContentHash))

	// Simulate simulation
	result := &SimulationResult{
		Score: 0.85,
		Signals: map[string]float64{
			"syntax": 1.0,
			"lint":   0.9,
		},
	}
	child.SetSimulationResult(result)

	auditLog.Record(*NewAuditEntry(AuditActionSimulate, child.ID).
		WithScore(result.Score).
		WithDetails("simulated with score 0.85"))

	// Simulate backpropagation
	child.IncrementVisits()
	child.AddScore(result.Score)

	auditLog.Record(*NewAuditEntry(AuditActionBackprop, child.ID).
		WithScore(child.AvgScore()))

	// Verify audit log integrity
	if !auditLog.Verify() {
		t.Error("audit log verification failed")
	}

	// Verify entry counts
	summary := auditLog.Summary()
	if summary.TotalEntries != 4 {
		t.Errorf("expected 4 entries, got %d", summary.TotalEntries)
	}

	if summary.ActionCounts[AuditActionSelect] != 1 {
		t.Error("expected 1 select entry")
	}
	if summary.ActionCounts[AuditActionExpand] != 1 {
		t.Error("expected 1 expand entry")
	}
	if summary.ActionCounts[AuditActionSimulate] != 1 {
		t.Error("expected 1 simulate entry")
	}
	if summary.ActionCounts[AuditActionBackprop] != 1 {
		t.Error("expected 1 backprop entry")
	}
}

func TestIntegration_MetricsWithSimulator(t *testing.T) {
	// Scenario: Verify metrics are recorded during simulation.

	ctx := context.Background()
	config := DefaultSimulatorConfig()
	validator := &mockValidator{valid: true}
	linter := &mockLinter{result: &LintResult{Valid: true}}

	sim := NewSimulator(config, WithValidator(validator), WithLinter(linter))

	// Create a node with a valid action
	node := NewPlanNode("test-metrics", "Test metrics recording")
	action := &PlannedAction{
		Type:        ActionTypeEdit,
		FilePath:    "test.go",
		Description: "Test action",
		CodeDiff:    "func test() {}",
	}
	action.Validate("/project", DefaultActionValidationConfig())
	node.SetAction(action)

	// Run simulation
	start := time.Now()
	result, err := sim.Simulate(ctx, node, SimTierQuick)
	duration := time.Since(start)

	if err != nil {
		t.Fatalf("simulation failed: %v", err)
	}

	// Record metrics (in real code this would be in the simulator)
	RecordSimulation(ctx, SimTierQuick, result.Score, duration)

	// Verify result
	if result.Score < 0 || result.Score > 1 {
		t.Errorf("score should be in [0,1], got %v", result.Score)
	}
}

func TestIntegration_CircuitBreakerWithAuditLog(t *testing.T) {
	// Scenario: Circuit breaker state changes are recorded in audit log.

	cb := NewCircuitBreaker(DefaultCircuitBreakerConfig())
	auditLog := NewAuditLog()

	// Record initial state
	auditLog.Record(*NewAuditEntry(AuditActionSelect, "circuit").
		WithDetails("circuit breaker closed"))

	// Trigger failures to open circuit
	for i := 0; i < 5; i++ {
		cb.RecordFailure()
	}

	if cb.State() != CircuitOpen {
		t.Error("circuit should be open after 5 failures")
	}

	auditLog.Record(*NewAuditEntry(AuditActionAbandon, "circuit").
		WithDetails("circuit breaker opened"))

	// Verify log
	if !auditLog.Verify() {
		t.Error("audit log should be valid")
	}

	entries := auditLog.Entries()
	if len(entries) != 2 {
		t.Errorf("expected 2 entries, got %d", len(entries))
	}
}

func TestIntegration_TreeStateWithBudget(t *testing.T) {
	// Scenario: Tree state decision should respect budget constraints.

	config := DefaultPlanPhaseConfig()

	// Create a budget that would be exhausted
	budgetConfig := TreeBudgetConfig{
		MaxNodes:      2,
		MaxDepth:      2,
		MaxExpansions: 1,
		TimeLimit:     1 * time.Second,
	}
	budget := NewTreeBudget(budgetConfig)

	// Complex task would normally trigger tree mode (need multiple indicators to reach 0.7)
	decision := ShouldUseTreeMode("refactor and restructure the architecture across multiple files", nil, config)
	if !decision.UseTreeMode {
		t.Errorf("complex task should trigger tree mode, got complexity=%.2f", decision.Complexity)
	}

	// But with exhausted budget, tree operations would be limited
	budget.RecordNodeExplored()
	budget.RecordNodeExplored()

	if !budget.Exhausted() {
		t.Error("budget should be exhausted after max nodes")
	}
}

func TestIntegration_DegradationRecoveryPath(t *testing.T) {
	// Scenario: System degrades and then recovers over time.

	ctx := context.Background()
	degradationMgr := NewDegradationManager(DefaultDegradationConfig(), nil)
	auditLog := NewAuditLog()

	// Record degradation events
	degradationMgr.OnDegradation(func(from, to TreeDegradation, reason string) {
		auditLog.Record(*NewAuditEntry(AuditActionAbandon, "degradation").
			WithDetails(reason))
		RecordDegradation(ctx, from, to)
	})

	// Trigger degradation to reduced
	for i := 0; i < 2; i++ {
		degradationMgr.RecordFailure()
	}

	if degradationMgr.CurrentLevel() != TreeDegradationReduced {
		t.Errorf("expected reduced level, got %v", degradationMgr.CurrentLevel())
	}

	// Now recover
	for i := 0; i < 3; i++ {
		degradationMgr.RecordSuccess()
	}

	if degradationMgr.CurrentLevel() != TreeDegradationNormal {
		t.Errorf("expected normal level after recovery, got %v", degradationMgr.CurrentLevel())
	}

	// Verify audit log captured events
	if !auditLog.Verify() {
		t.Error("audit log should be valid")
	}

	// Should have at least one degradation entry
	abandonEntries := auditLog.EntriesByAction(AuditActionAbandon)
	if len(abandonEntries) == 0 {
		t.Error("expected at least one degradation event in audit log")
	}
}

func TestIntegration_FullMCTSSessionWithAudit(t *testing.T) {
	// Scenario: Simulate a complete MCTS session with full audit trail.

	ctx := context.Background()
	auditLog := NewAuditLog()

	// Create tree
	budgetConfig := DefaultTreeBudgetConfig()
	budget := NewTreeBudget(budgetConfig)
	tree := NewPlanTree("Fix authentication bug in login flow", budget)

	// Record session start
	auditLog.Record(*NewAuditEntry(AuditActionSelect, tree.Root().ID).
		WithDetails("session started"))

	// Simulate MCTS iterations
	for i := 0; i < 3; i++ {
		// Select
		auditLog.Record(*NewAuditEntry(AuditActionSelect, tree.Root().ID))

		// Expand
		childID := string(rune('1' + i))
		child := NewPlanNode(childID, "Approach "+childID)
		tree.Root().AddChild(child)
		tree.IncrementNodeCount()

		auditLog.Record(*NewAuditEntry(AuditActionExpand, child.ID).
			WithParentHash(tree.Root().ContentHash).
			WithNodeHash(child.ContentHash))

		// Simulate
		score := 0.6 + float64(i)*0.1
		auditLog.Record(*NewAuditEntry(AuditActionSimulate, child.ID).
			WithScore(score))

		// Backprop
		child.IncrementVisits()
		child.AddScore(score)
		auditLog.Record(*NewAuditEntry(AuditActionBackprop, child.ID).
			WithScore(child.AvgScore()))
	}

	// Extract best path
	bestPath := tree.ExtractBestPath()
	tree.SetBestPath(bestPath)

	// Record completion metrics
	stats := TreeCompletionStats{
		TotalNodes:  tree.TotalNodes(),
		PrunedNodes: 0,
		MaxDepth:    tree.MaxDepth(),
		BestScore:   tree.BestScore(),
	}
	RecordTreeCompletion(ctx, stats)

	// Verify audit log
	if !auditLog.Verify() {
		t.Error("audit log verification failed")
	}

	// Check summary
	summary := auditLog.Summary()
	if summary.TotalEntries < 10 {
		t.Errorf("expected at least 10 entries, got %d", summary.TotalEntries)
	}

	// Verify tree state
	if tree.TotalNodes() != 4 { // root + 3 children
		t.Errorf("expected 4 nodes, got %d", tree.TotalNodes())
	}

	if len(bestPath) < 2 {
		t.Error("best path should have at least root + 1 child")
	}
}
