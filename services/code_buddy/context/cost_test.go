// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package context

import (
	"sync"
	"testing"
)

func TestCostEstimator_EstimateForProject(t *testing.T) {
	e := NewCostEstimator(DefaultCostConfig(), DefaultCostLimits())

	estimate := e.EstimateForProject(1, 10, 50)

	// Should have counts
	if estimate.ProjectCount != 1 {
		t.Errorf("ProjectCount = %d, want 1", estimate.ProjectCount)
	}
	if estimate.PackageCount != 10 {
		t.Errorf("PackageCount = %d, want 10", estimate.PackageCount)
	}
	if estimate.FileCount != 50 {
		t.Errorf("FileCount = %d, want 50", estimate.FileCount)
	}

	// Should have non-zero token estimates
	if estimate.EstimatedInputTokens <= 0 {
		t.Error("expected positive input tokens")
	}
	if estimate.EstimatedOutputTokens <= 0 {
		t.Error("expected positive output tokens")
	}
	if estimate.EstimatedTotalTokens <= 0 {
		t.Error("expected positive total tokens")
	}

	// Total should equal input + output
	if estimate.EstimatedTotalTokens != estimate.EstimatedInputTokens+estimate.EstimatedOutputTokens {
		t.Error("total tokens should equal input + output")
	}

	// Should have positive cost
	if estimate.EstimatedCostUSD <= 0 {
		t.Error("expected positive cost")
	}

	// Should have level breakdown
	if len(estimate.LevelBreakdown) != 3 {
		t.Errorf("expected 3 levels in breakdown, got %d", len(estimate.LevelBreakdown))
	}
}

func TestCostEstimator_EstimateForLevel(t *testing.T) {
	e := NewCostEstimator(DefaultCostConfig(), DefaultCostLimits())

	tests := []struct {
		level HierarchyLevel
		count int
	}{
		{LevelProject, 1},
		{LevelPackage, 10},
		{LevelFile, 100},
		{LevelFunction, 500},
	}

	for _, tt := range tests {
		t.Run(tt.level.String(), func(t *testing.T) {
			cost := e.EstimateForLevel(tt.level, tt.count)

			if cost.EntityCount != tt.count {
				t.Errorf("EntityCount = %d, want %d", cost.EntityCount, tt.count)
			}
			if cost.InputTokens <= 0 {
				t.Error("expected positive input tokens")
			}
			if cost.OutputTokens <= 0 {
				t.Error("expected positive output tokens")
			}
			if cost.CostUSD <= 0 {
				t.Error("expected positive cost")
			}
		})
	}
}

func TestCostEstimator_RecordUsage(t *testing.T) {
	e := NewCostEstimator(DefaultCostConfig(), DefaultCostLimits())

	// Record some usage
	e.RecordUsage(1000, 500)

	input, output, cost := e.GetUsage()

	if input != 1000 {
		t.Errorf("input = %d, want 1000", input)
	}
	if output != 500 {
		t.Errorf("output = %d, want 500", output)
	}
	if cost <= 0 {
		t.Error("expected positive cost")
	}
}

func TestCostEstimator_RecordUsage_Accumulates(t *testing.T) {
	e := NewCostEstimator(DefaultCostConfig(), DefaultCostLimits())

	e.RecordUsage(1000, 500)
	e.RecordUsage(2000, 1000)

	input, output, _ := e.GetUsage()

	if input != 3000 {
		t.Errorf("input = %d, want 3000", input)
	}
	if output != 1500 {
		t.Errorf("output = %d, want 1500", output)
	}
}

func TestCostEstimator_ResetUsage(t *testing.T) {
	e := NewCostEstimator(DefaultCostConfig(), DefaultCostLimits())

	e.RecordUsage(1000, 500)
	e.ResetUsage()

	input, output, cost := e.GetUsage()

	if input != 0 {
		t.Errorf("input = %d, want 0 after reset", input)
	}
	if output != 0 {
		t.Errorf("output = %d, want 0 after reset", output)
	}
	if cost != 0 {
		t.Errorf("cost = %f, want 0 after reset", cost)
	}
}

func TestCostEstimator_CheckLimits_TokenLimit(t *testing.T) {
	limits := CostLimits{
		MaxTotalTokens:           10000,
		MaxCostUSD:               100.00,
		RequireConfirmation:      false,
		ConfirmationThresholdUSD: 10.00,
	}
	e := NewCostEstimator(DefaultCostConfig(), limits)

	// Use up most of budget
	e.RecordUsage(8000, 1000)

	// Estimate that would exceed limit
	estimate := &CostEstimate{
		EstimatedTotalTokens: 5000,
		EstimatedCostUSD:     0.01,
	}

	err := e.CheckLimits(estimate)
	if err != ErrTokenLimitExceeded {
		t.Errorf("expected ErrTokenLimitExceeded, got %v", err)
	}
}

func TestCostEstimator_CheckLimits_CostLimit(t *testing.T) {
	limits := CostLimits{
		MaxTotalTokens:           1_000_000,
		MaxCostUSD:               0.10, // Very low limit
		RequireConfirmation:      false,
		ConfirmationThresholdUSD: 0.05,
	}
	e := NewCostEstimator(DefaultCostConfig(), limits)

	// Estimate that would exceed cost
	estimate := &CostEstimate{
		EstimatedTotalTokens: 100000,
		EstimatedCostUSD:     0.50, // Over limit
	}

	err := e.CheckLimits(estimate)
	if err != ErrCostLimitExceeded {
		t.Errorf("expected ErrCostLimitExceeded, got %v", err)
	}
}

func TestCostEstimator_CheckLimits_ConfirmationRequired(t *testing.T) {
	limits := CostLimits{
		MaxTotalTokens:           1_000_000,
		MaxCostUSD:               100.00,
		RequireConfirmation:      true,
		ConfirmationThresholdUSD: 0.01,
	}
	e := NewCostEstimator(DefaultCostConfig(), limits)

	// Estimate that exceeds confirmation threshold
	estimate := &CostEstimate{
		EstimatedTotalTokens: 1000,
		EstimatedCostUSD:     0.50, // Over confirmation threshold
	}

	err := e.CheckLimits(estimate)
	if err != ErrConfirmationRequired {
		t.Errorf("expected ErrConfirmationRequired, got %v", err)
	}
}

func TestCostEstimator_CheckLimits_OK(t *testing.T) {
	e := NewCostEstimator(DefaultCostConfig(), DefaultCostLimits())

	estimate := &CostEstimate{
		EstimatedTotalTokens: 1000,
		EstimatedCostUSD:     0.001,
	}

	err := e.CheckLimits(estimate)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestCostEstimator_WouldExceedTokenLimit(t *testing.T) {
	limits := CostLimits{
		MaxTotalTokens: 10000,
		MaxCostUSD:     100.00,
	}
	e := NewCostEstimator(DefaultCostConfig(), limits)

	e.RecordUsage(5000, 4000) // 9000 total

	if !e.WouldExceedTokenLimit(2000) {
		t.Error("expected 2000 additional to exceed limit")
	}
	if e.WouldExceedTokenLimit(500) {
		t.Error("expected 500 additional to NOT exceed limit")
	}
}

func TestCostEstimator_RemainingBudget(t *testing.T) {
	limits := CostLimits{
		MaxTotalTokens: 10000,
		MaxCostUSD:     10.00,
	}
	e := NewCostEstimator(DefaultCostConfig(), limits)

	e.RecordUsage(2000, 1000)

	tokensRemaining, costRemaining := e.RemainingBudget()

	if tokensRemaining != 7000 {
		t.Errorf("tokensRemaining = %d, want 7000", tokensRemaining)
	}
	if costRemaining <= 0 {
		t.Error("expected positive cost remaining")
	}
}

func TestCostEstimator_CostSummary(t *testing.T) {
	e := NewCostEstimator(DefaultCostConfig(), DefaultCostLimits())

	e.RecordUsage(10000, 5000)

	summary := e.CostSummary()

	if summary.InputTokensUsed != 10000 {
		t.Errorf("InputTokensUsed = %d, want 10000", summary.InputTokensUsed)
	}
	if summary.OutputTokensUsed != 5000 {
		t.Errorf("OutputTokensUsed = %d, want 5000", summary.OutputTokensUsed)
	}
	if summary.TotalTokensUsed != 15000 {
		t.Errorf("TotalTokensUsed = %d, want 15000", summary.TotalTokensUsed)
	}
	if summary.CostUSD <= 0 {
		t.Error("expected positive cost")
	}
	if summary.TokenLimitPercent <= 0 {
		t.Error("expected positive token limit percent")
	}
	if summary.CostLimitPercent <= 0 {
		t.Error("expected positive cost limit percent")
	}
}

func TestCostEstimator_ConcurrentAccess(t *testing.T) {
	e := NewCostEstimator(DefaultCostConfig(), DefaultCostLimits())

	var wg sync.WaitGroup
	iterations := 100

	// Concurrent record usage
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			e.RecordUsage(100, 50)
		}
	}()

	// Concurrent get usage
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			e.GetUsage()
			e.RemainingBudget()
		}
	}()

	// Concurrent check limits
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			e.CheckLimits(&CostEstimate{EstimatedTotalTokens: 100})
		}
	}()

	wg.Wait()
	// Should not panic
}

func TestDefaultCostConfig(t *testing.T) {
	config := DefaultCostConfig()

	if config.InputPricePerMillion != DefaultInputPricePerMillion {
		t.Errorf("InputPricePerMillion = %f, want %f",
			config.InputPricePerMillion, DefaultInputPricePerMillion)
	}
	if config.OutputPricePerMillion != DefaultOutputPricePerMillion {
		t.Errorf("OutputPricePerMillion = %f, want %f",
			config.OutputPricePerMillion, DefaultOutputPricePerMillion)
	}
}

func TestDefaultCostLimits(t *testing.T) {
	limits := DefaultCostLimits()

	if limits.MaxTotalTokens != 1_000_000 {
		t.Errorf("MaxTotalTokens = %d, want 1000000", limits.MaxTotalTokens)
	}
	if limits.MaxCostUSD != 10.00 {
		t.Errorf("MaxCostUSD = %f, want 10.00", limits.MaxCostUSD)
	}
	if !limits.RequireConfirmation {
		t.Error("expected RequireConfirmation to be true")
	}
	if limits.ConfirmationThresholdUSD != 1.00 {
		t.Errorf("ConfirmationThresholdUSD = %f, want 1.00", limits.ConfirmationThresholdUSD)
	}
}
