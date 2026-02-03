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

func TestRecordLLMCall(t *testing.T) {
	ctx := context.Background()

	// Should not panic
	RecordLLMCall(ctx, 100, 0.01, true)
	RecordLLMCall(ctx, 200, 0.02, false)
}

func TestRecordNodeCreated(t *testing.T) {
	ctx := context.Background()

	// Should not panic
	RecordNodeCreated(ctx)
}

func TestRecordNodeAbandoned(t *testing.T) {
	ctx := context.Background()

	// Should not panic
	RecordNodeAbandoned(ctx, "low_score")
	RecordNodeAbandoned(ctx, "timeout")
}

func TestRecordNodesPruned(t *testing.T) {
	ctx := context.Background()

	// Should not panic
	RecordNodesPruned(ctx, 10)
}

func TestRecordSimulation(t *testing.T) {
	ctx := context.Background()

	// Should not panic
	RecordSimulation(ctx, SimTierQuick, 0.9, 100*time.Millisecond)
	RecordSimulation(ctx, SimTierStandard, 0.3, 500*time.Millisecond)
	RecordSimulation(ctx, SimTierFull, 0.7, 2*time.Second)
}

func TestRecordTreeCompletion(t *testing.T) {
	ctx := context.Background()

	stats := TreeCompletionStats{
		TotalNodes:  15,
		PrunedNodes: 3,
		MaxDepth:    4,
		BestScore:   0.85,
	}

	// Should not panic
	RecordTreeCompletion(ctx, stats)
}

func TestRecordBudgetUtilization(t *testing.T) {
	ctx := context.Background()

	usage := BudgetUtilizationStats{
		NodesUsed:   10,
		NodesMax:    20,
		TokensUsed:  5000,
		TokensMax:   10000,
		CallsUsed:   5,
		CallsMax:    10,
		CostUsedUSD: 0.05,
		CostMaxUSD:  0.10,
		Elapsed:     30 * time.Second,
		TimeLimit:   60 * time.Second,
	}

	// Should not panic
	RecordBudgetUtilization(ctx, usage)
}

func TestRecordBudgetUtilization_ZeroMax(t *testing.T) {
	ctx := context.Background()

	// With zero max values, should not divide by zero
	usage := BudgetUtilizationStats{
		NodesUsed:  10,
		NodesMax:   0, // Zero max
		TokensUsed: 5000,
		TokensMax:  0, // Zero max
	}

	// Should not panic
	RecordBudgetUtilization(ctx, usage)
}

func TestRecordDegradation(t *testing.T) {
	ctx := context.Background()

	// Should not panic
	RecordDegradation(ctx, TreeDegradationNormal, TreeDegradationReduced)
	RecordDegradation(ctx, TreeDegradationReduced, TreeDegradationMinimal)
	RecordDegradation(ctx, TreeDegradationMinimal, TreeDegradationLinear)
}

func TestRecordCircuitBreakerState(t *testing.T) {
	ctx := context.Background()

	// Should not panic
	RecordCircuitBreakerState(ctx, CircuitClosed)
	RecordCircuitBreakerState(ctx, CircuitHalfOpen)
	RecordCircuitBreakerState(ctx, CircuitOpen)
}

func TestStartMCTSSpan(t *testing.T) {
	ctx := context.Background()

	newCtx, span := StartMCTSSpan(ctx, "test_operation", "test task")
	defer span.End()

	if newCtx == nil {
		t.Error("context should not be nil")
	}
	if span == nil {
		t.Error("span should not be nil")
	}
}

func TestSetMCTSSpanResult(t *testing.T) {
	ctx := context.Background()

	_, span := StartMCTSSpan(ctx, "test_operation", "test task")
	defer span.End()

	// Should not panic
	SetMCTSSpanResult(span, true, 15, 0.9)
	SetMCTSSpanResult(span, false, 5, 0.3)
}

func TestAddMCTSEvent(t *testing.T) {
	ctx := context.Background()

	_, span := StartMCTSSpan(ctx, "test_operation", "test task")
	defer span.End()

	// Should not panic
	AddMCTSEvent(span, "state_transition")
}

func TestTruncateForAttribute(t *testing.T) {
	tests := []struct {
		input  string
		maxLen int
		want   string
	}{
		{"short", 10, "short"},
		{"exactly10!", 10, "exactly10!"},
		{"this is a long string", 10, "this is..."},
		{"", 10, ""},
		{"abc", 3, "abc"},
		{"abcd", 3, "..."},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := truncateForAttribute(tt.input, tt.maxLen)
			if got != tt.want {
				t.Errorf("truncateForAttribute(%q, %d) = %q, want %q",
					tt.input, tt.maxLen, got, tt.want)
			}
		})
	}
}

func TestInitMetrics_Multiple(t *testing.T) {
	// Should be safe to call multiple times
	err1 := initMetrics()
	err2 := initMetrics()
	err3 := initMetrics()

	// All should return the same result (due to sync.Once)
	if err1 != err2 || err2 != err3 {
		t.Error("initMetrics should return same error on multiple calls")
	}
}
