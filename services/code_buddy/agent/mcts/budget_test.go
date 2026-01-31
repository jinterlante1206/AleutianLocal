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
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestNewTreeBudget(t *testing.T) {
	config := DefaultTreeBudgetConfig()
	budget := NewTreeBudget(config)

	if budget == nil {
		t.Fatal("NewTreeBudget returned nil")
	}
	if budget.NodesExplored() != 0 {
		t.Errorf("Initial NodesExplored = %d, want 0", budget.NodesExplored())
	}
	if budget.LLMCalls() != 0 {
		t.Errorf("Initial LLMCalls = %d, want 0", budget.LLMCalls())
	}
	if budget.Exhausted() {
		t.Error("Initial budget should not be exhausted")
	}
}

func TestDefaultTreeBudgetConfig(t *testing.T) {
	config := DefaultTreeBudgetConfig()

	if config.MaxNodes != 20 {
		t.Errorf("MaxNodes = %d, want 20", config.MaxNodes)
	}
	if config.MaxDepth != 5 {
		t.Errorf("MaxDepth = %d, want 5", config.MaxDepth)
	}
	if config.MaxExpansions != 3 {
		t.Errorf("MaxExpansions = %d, want 3", config.MaxExpansions)
	}
	if config.TimeLimit != 30*time.Second {
		t.Errorf("TimeLimit = %v, want 30s", config.TimeLimit)
	}
	if config.LLMCallLimit != 50 {
		t.Errorf("LLMCallLimit = %d, want 50", config.LLMCallLimit)
	}
	if config.CostLimitUSD != 1.0 {
		t.Errorf("CostLimitUSD = %f, want 1.0", config.CostLimitUSD)
	}
}

func TestTreeBudget_RecordNodeExplored(t *testing.T) {
	config := DefaultTreeBudgetConfig()
	budget := NewTreeBudget(config)

	n := budget.RecordNodeExplored()
	if n != 1 {
		t.Errorf("RecordNodeExplored returned %d, want 1", n)
	}
	if budget.NodesExplored() != 1 {
		t.Errorf("NodesExplored = %d, want 1", budget.NodesExplored())
	}
}

func TestTreeBudget_RecordNodeExploredConcurrency(t *testing.T) {
	config := DefaultTreeBudgetConfig()
	config.MaxNodes = 10000 // High limit
	budget := NewTreeBudget(config)

	const numGoroutines = 100
	const numRecords = 100

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < numRecords; j++ {
				budget.RecordNodeExplored()
			}
		}()
	}

	wg.Wait()

	expected := int64(numGoroutines * numRecords)
	if budget.NodesExplored() != expected {
		t.Errorf("NodesExplored = %d, want %d", budget.NodesExplored(), expected)
	}
}

func TestTreeBudget_RecordLLMCall(t *testing.T) {
	config := DefaultTreeBudgetConfig()
	budget := NewTreeBudget(config)

	err := budget.RecordLLMCall(1000, 0.01)
	if err != nil {
		t.Errorf("RecordLLMCall error: %v", err)
	}

	if budget.LLMCalls() != 1 {
		t.Errorf("LLMCalls = %d, want 1", budget.LLMCalls())
	}
	if budget.TokensUsed() != 1000 {
		t.Errorf("TokensUsed = %d, want 1000", budget.TokensUsed())
	}
	if budget.CostUSD() != 0.01 {
		t.Errorf("CostUSD = %f, want 0.01", budget.CostUSD())
	}
}

func TestTreeBudget_RecordLLMCallConcurrency(t *testing.T) {
	config := DefaultTreeBudgetConfig()
	config.LLMCallLimit = 10000 // High limit
	config.CostLimitUSD = 100.0 // High limit
	budget := NewTreeBudget(config)

	const numGoroutines = 100
	const tokensPerCall = int64(100)
	const costPerCall = 0.01

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			budget.RecordLLMCall(tokensPerCall, costPerCall)
		}()
	}

	wg.Wait()

	if budget.LLMCalls() != numGoroutines {
		t.Errorf("LLMCalls = %d, want %d", budget.LLMCalls(), numGoroutines)
	}

	expectedTokens := int64(numGoroutines) * tokensPerCall
	if budget.TokensUsed() != expectedTokens {
		t.Errorf("TokensUsed = %d, want %d", budget.TokensUsed(), expectedTokens)
	}

	expectedCost := float64(numGoroutines) * costPerCall
	gotCost := budget.CostUSD()
	// Allow small floating point variance
	if gotCost < expectedCost-0.001 || gotCost > expectedCost+0.001 {
		t.Errorf("CostUSD = %f, want approximately %f", gotCost, expectedCost)
	}
}

func TestTreeBudget_NodeLimitExceeded(t *testing.T) {
	config := TreeBudgetConfig{
		MaxNodes:  5,
		TimeLimit: time.Hour, // Long timeout
	}
	budget := NewTreeBudget(config)

	// Record 5 nodes (at limit)
	for i := 0; i < 5; i++ {
		budget.RecordNodeExplored()
	}

	if !budget.Exhausted() {
		t.Error("Budget should be exhausted after reaching node limit")
	}
	if budget.ExhaustedBy() != "nodes" {
		t.Errorf("ExhaustedBy = %s, want 'nodes'", budget.ExhaustedBy())
	}
}

func TestTreeBudget_LLMCallLimitExceeded(t *testing.T) {
	config := TreeBudgetConfig{
		LLMCallLimit:  3,
		TimeLimit:     time.Hour,
		CostLimitUSD:  100.0,
		LLMTokenLimit: 100000,
	}
	budget := NewTreeBudget(config)

	// Make 3 calls
	for i := 0; i < 3; i++ {
		budget.RecordLLMCall(100, 0.01)
	}

	if !budget.Exhausted() {
		t.Error("Budget should be exhausted after reaching LLM call limit")
	}
	if budget.ExhaustedBy() != "llm_calls" {
		t.Errorf("ExhaustedBy = %s, want 'llm_calls'", budget.ExhaustedBy())
	}
}

func TestTreeBudget_CostLimitExceeded(t *testing.T) {
	config := TreeBudgetConfig{
		CostLimitUSD: 0.05,
		TimeLimit:    time.Hour,
		LLMCallLimit: 100,
	}
	budget := NewTreeBudget(config)

	// Make calls until cost exceeded
	for i := 0; i < 10; i++ {
		budget.RecordLLMCall(100, 0.01)
	}

	if !budget.Exhausted() {
		t.Error("Budget should be exhausted after exceeding cost limit")
	}
	if budget.ExhaustedBy() != "cost" {
		t.Errorf("ExhaustedBy = %s, want 'cost'", budget.ExhaustedBy())
	}

	// Further calls should return error
	err := budget.RecordLLMCall(100, 0.01)
	if err == nil {
		t.Error("Expected error after budget exhausted")
	}
	if !errors.Is(err, ErrBudgetExhausted) {
		t.Errorf("Expected ErrBudgetExhausted, got: %v", err)
	}
}

func TestTreeBudget_TimeLimitExceeded(t *testing.T) {
	config := TreeBudgetConfig{
		TimeLimit: 10 * time.Millisecond,
		MaxNodes:  1000, // High limit
	}
	budget := NewTreeBudget(config)

	// Wait for time limit
	time.Sleep(20 * time.Millisecond)

	if !budget.Exhausted() {
		t.Error("Budget should be exhausted after time limit")
	}
	if budget.ExhaustedBy() != "time" {
		t.Errorf("ExhaustedBy = %s, want 'time'", budget.ExhaustedBy())
	}
}

func TestTreeBudget_CheckDepth(t *testing.T) {
	config := TreeBudgetConfig{
		MaxDepth: 3,
	}
	budget := NewTreeBudget(config)

	// Depth 0, 1, 2 should be OK
	for d := 0; d < 3; d++ {
		if err := budget.CheckDepth(d); err != nil {
			t.Errorf("CheckDepth(%d) error: %v", d, err)
		}
	}

	// Depth 3+ should fail
	err := budget.CheckDepth(3)
	if err == nil {
		t.Error("CheckDepth(3) should return error")
	}
	if !errors.Is(err, ErrDepthLimitExceeded) {
		t.Errorf("Expected ErrDepthLimitExceeded, got: %v", err)
	}
}

func TestTreeBudget_CanExpand(t *testing.T) {
	config := TreeBudgetConfig{
		MaxExpansions: 3,
		MaxNodes:      100,
		TimeLimit:     time.Hour,
	}
	budget := NewTreeBudget(config)

	// Can expand with 0, 1, 2 children
	for c := 0; c < 3; c++ {
		if !budget.CanExpand(c) {
			t.Errorf("CanExpand(%d) = false, want true", c)
		}
	}

	// Cannot expand with 3+ children
	if budget.CanExpand(3) {
		t.Error("CanExpand(3) = true, want false")
	}
	if budget.CanExpand(5) {
		t.Error("CanExpand(5) = true, want false")
	}
}

func TestTreeBudget_CanExpand_ExhaustedBudget(t *testing.T) {
	config := TreeBudgetConfig{
		MaxExpansions: 10,
		MaxNodes:      1,
		TimeLimit:     time.Hour,
	}
	budget := NewTreeBudget(config)

	// Exhaust node budget
	budget.RecordNodeExplored()

	// Should not be able to expand even with 0 children
	if budget.CanExpand(0) {
		t.Error("CanExpand(0) = true, want false when budget exhausted")
	}
}

func TestTreeBudget_Remaining(t *testing.T) {
	config := TreeBudgetConfig{
		MaxNodes:      20,
		TimeLimit:     30 * time.Second,
		LLMCallLimit:  50,
		LLMTokenLimit: 100000,
		CostLimitUSD:  1.0,
	}
	budget := NewTreeBudget(config)

	// Record some usage
	budget.RecordNodeExplored()
	budget.RecordNodeExplored()
	budget.RecordLLMCall(5000, 0.25)

	remaining := budget.Remaining()

	if remaining.Nodes != 18 {
		t.Errorf("Remaining.Nodes = %d, want 18", remaining.Nodes)
	}
	if remaining.LLMCalls != 49 {
		t.Errorf("Remaining.LLMCalls = %d, want 49", remaining.LLMCalls)
	}
	if remaining.Tokens != 95000 {
		t.Errorf("Remaining.Tokens = %d, want 95000", remaining.Tokens)
	}
	if remaining.CostUSD != 0.75 {
		t.Errorf("Remaining.CostUSD = %f, want 0.75", remaining.CostUSD)
	}
}

func TestTreeBudget_String(t *testing.T) {
	config := DefaultTreeBudgetConfig()
	budget := NewTreeBudget(config)

	str := budget.String()
	if str == "" {
		t.Error("String() should not be empty")
	}

	// Should contain key info
	if !strings.Contains(str, "Budget{") {
		t.Error("String() should start with 'Budget{'")
	}
	if !strings.Contains(str, "nodes=") {
		t.Error("String() should contain 'nodes='")
	}
	if !strings.Contains(str, "cost=") {
		t.Error("String() should contain 'cost='")
	}
}

func TestTreeBudget_String_Exhausted(t *testing.T) {
	config := TreeBudgetConfig{
		MaxNodes:  1,
		TimeLimit: time.Hour,
	}
	budget := NewTreeBudget(config)
	budget.RecordNodeExplored()

	str := budget.String()
	if !strings.Contains(str, "[EXHAUSTED by nodes]") {
		t.Errorf("String() should indicate exhaustion reason, got: %s", str)
	}
}

func TestTreeBudget_Report(t *testing.T) {
	config := DefaultTreeBudgetConfig()
	budget := NewTreeBudget(config)

	budget.RecordNodeExplored()
	budget.RecordLLMCall(1000, 0.05)

	report := budget.Report()

	if report.NodesExplored != 1 {
		t.Errorf("Report.NodesExplored = %d, want 1", report.NodesExplored)
	}
	if report.LLMCalls != 1 {
		t.Errorf("Report.LLMCalls = %d, want 1", report.LLMCalls)
	}
	if report.TokensUsed != 1000 {
		t.Errorf("Report.TokensUsed = %d, want 1000", report.TokensUsed)
	}
	if report.CostUSD != 0.05 {
		t.Errorf("Report.CostUSD = %f, want 0.05", report.CostUSD)
	}
	if report.Exhausted {
		t.Error("Report.Exhausted = true, want false")
	}
}

func TestTreeBudget_Reset(t *testing.T) {
	config := TreeBudgetConfig{
		MaxNodes:      10,
		LLMCallLimit:  10,
		LLMTokenLimit: 10000,
		CostLimitUSD:  1.0,
		TimeLimit:     time.Hour,
	}
	budget := NewTreeBudget(config)

	// Use up budget
	for i := 0; i < 10; i++ {
		budget.RecordNodeExplored()
	}
	budget.RecordLLMCall(5000, 0.5)

	if !budget.Exhausted() {
		t.Error("Budget should be exhausted")
	}

	// Reset
	budget.Reset()

	if budget.Exhausted() {
		t.Error("Budget should not be exhausted after reset")
	}
	if budget.NodesExplored() != 0 {
		t.Errorf("NodesExplored after reset = %d, want 0", budget.NodesExplored())
	}
	if budget.LLMCalls() != 0 {
		t.Errorf("LLMCalls after reset = %d, want 0", budget.LLMCalls())
	}
	if budget.TokensUsed() != 0 {
		t.Errorf("TokensUsed after reset = %d, want 0", budget.TokensUsed())
	}
	if budget.CostUSD() != 0 {
		t.Errorf("CostUSD after reset = %f, want 0", budget.CostUSD())
	}
	if budget.ExhaustedBy() != "" {
		t.Errorf("ExhaustedBy after reset = %s, want empty", budget.ExhaustedBy())
	}
}

func TestTreeBudget_ZeroLimits(t *testing.T) {
	// Zero limits should mean unlimited
	config := TreeBudgetConfig{
		MaxNodes:      0, // Unlimited
		LLMCallLimit:  0, // Unlimited
		LLMTokenLimit: 0, // Unlimited
		CostLimitUSD:  0, // Unlimited
		TimeLimit:     0, // Unlimited
	}
	budget := NewTreeBudget(config)

	// Should never exhaust
	for i := 0; i < 100; i++ {
		budget.RecordNodeExplored()
		budget.RecordLLMCall(1000, 1.0)
	}

	if budget.Exhausted() {
		t.Error("Budget with zero limits should never exhaust")
	}
}

func TestTreeBudget_Config(t *testing.T) {
	config := TreeBudgetConfig{
		MaxNodes: 42,
	}
	budget := NewTreeBudget(config)

	got := budget.Config()
	if got.MaxNodes != 42 {
		t.Errorf("Config().MaxNodes = %d, want 42", got.MaxNodes)
	}
}
