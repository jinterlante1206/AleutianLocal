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
	"errors"
	"log/slog"
	"os"
	"testing"
	"time"

	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

func TestNewMCTSTracer(t *testing.T) {
	config := ObservabilityConfig{
		TracingEnabled: true,
		LogLevel:       "debug",
	}

	tracer := NewMCTSTracer(nil, config)
	if tracer == nil {
		t.Fatal("NewMCTSTracer returned nil")
	}
	if !tracer.enabled {
		t.Error("tracer should be enabled")
	}
}

func TestNewMCTSTracer_Disabled(t *testing.T) {
	config := ObservabilityConfig{
		TracingEnabled: false,
	}

	tracer := NewMCTSTracer(nil, config)
	if tracer.enabled {
		t.Error("tracer should be disabled")
	}
}

func TestMCTSTracer_StartMCTSRun(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	config := ObservabilityConfig{TracingEnabled: true}
	tracer := NewMCTSTracer(logger, config)

	budget := NewTreeBudget(DefaultTreeBudgetConfig())
	ctx, span := tracer.StartMCTSRun(context.Background(), "Test task", budget)

	if ctx == nil {
		t.Error("context should not be nil")
	}
	if span == nil {
		t.Error("span should not be nil")
	}

	// End the span
	tracer.EndMCTSRun(span, nil, budget, nil)
}

func TestMCTSTracer_StartMCTSRun_Disabled(t *testing.T) {
	config := ObservabilityConfig{TracingEnabled: false}
	tracer := NewMCTSTracer(nil, config)

	budget := NewTreeBudget(DefaultTreeBudgetConfig())
	ctx, span := tracer.StartMCTSRun(context.Background(), "Test task", budget)

	if ctx == nil {
		t.Error("context should not be nil even when disabled")
	}
	// Span should be noop
	span.End() // Should not panic
}

func TestMCTSTracer_EndMCTSRun_WithError(t *testing.T) {
	config := ObservabilityConfig{TracingEnabled: true}
	tracer := NewMCTSTracer(nil, config)

	budget := NewTreeBudget(DefaultTreeBudgetConfig())
	_, span := tracer.StartMCTSRun(context.Background(), "Test task", budget)

	// Should not panic
	tracer.EndMCTSRun(span, nil, budget, errors.New("test error"))
}

func TestMCTSTracer_EndMCTSRun_WithTree(t *testing.T) {
	config := ObservabilityConfig{TracingEnabled: true}
	tracer := NewMCTSTracer(nil, config)

	budget := NewTreeBudget(DefaultTreeBudgetConfig())
	tree := NewPlanTree("test task", budget)

	_, span := tracer.StartMCTSRun(context.Background(), "Test task", budget)

	// Should not panic
	tracer.EndMCTSRun(span, tree, budget, nil)
}

func TestMCTSTracer_TraceIteration(t *testing.T) {
	config := ObservabilityConfig{TracingEnabled: true}
	tracer := NewMCTSTracer(nil, config)

	ctx, span := tracer.TraceIteration(context.Background(), 1)

	if ctx == nil {
		t.Error("context should not be nil")
	}
	span.End() // Should not panic
}

func TestMCTSTracer_TraceSelect(t *testing.T) {
	config := ObservabilityConfig{TracingEnabled: true}
	tracer := NewMCTSTracer(nil, config)

	node := NewPlanNode("test-node", "Test description")
	node.IncrementVisits()
	node.AddScore(0.8)

	ctx, span := tracer.TraceSelect(context.Background(), node)

	if ctx == nil {
		t.Error("context should not be nil")
	}
	span.End()
}

func TestMCTSTracer_TraceExpand(t *testing.T) {
	config := ObservabilityConfig{TracingEnabled: true}
	tracer := NewMCTSTracer(nil, config)

	node := NewPlanNode("parent", "Parent node")

	ctx, span := tracer.TraceExpand(context.Background(), node)
	if ctx == nil {
		t.Error("context should not be nil")
	}

	// Test EndExpand
	children := []*PlanNode{
		NewPlanNode("child1", "Child 1"),
		NewPlanNode("child2", "Child 2"),
	}
	tracer.EndExpand(span, children, 500, 0.01, nil)
}

func TestMCTSTracer_TraceExpand_WithError(t *testing.T) {
	config := ObservabilityConfig{TracingEnabled: true}
	tracer := NewMCTSTracer(nil, config)

	node := NewPlanNode("parent", "Parent node")

	_, span := tracer.TraceExpand(context.Background(), node)
	tracer.EndExpand(span, nil, 0, 0, errors.New("expansion failed"))
}

func TestMCTSTracer_TraceSimulate(t *testing.T) {
	config := ObservabilityConfig{TracingEnabled: true}
	tracer := NewMCTSTracer(nil, config)

	node := NewPlanNode("test", "Test node")

	ctx, span := tracer.TraceSimulate(context.Background(), node, SimTierQuick)
	if ctx == nil {
		t.Error("context should not be nil")
	}

	result := &SimulationResult{
		Score:         0.85,
		Tier:          SimTierQuick.String(),
		PromoteToNext: true,
		Duration:      100 * time.Millisecond,
		Signals: map[string]float64{
			"syntax":     1.0,
			"complexity": 0.8,
		},
		Errors:   []string{},
		Warnings: []string{"minor issue"},
	}

	tracer.EndSimulate(span, result, nil)
}

func TestMCTSTracer_TraceBackpropagate(t *testing.T) {
	config := ObservabilityConfig{TracingEnabled: true}
	tracer := NewMCTSTracer(nil, config)

	node := NewPlanNode("test", "Test node")

	_, span := tracer.TraceBackpropagate(context.Background(), node, 0.9)
	tracer.EndBackpropagate(span, 5)
}

func TestMCTSTracer_TraceNodeAbandon(t *testing.T) {
	config := ObservabilityConfig{TracingEnabled: true}
	tracer := NewMCTSTracer(nil, config)

	_, span := tracer.TraceIteration(context.Background(), 1)
	defer span.End()

	node := NewPlanNode("abandoned", "Abandoned node")
	node.IncrementVisits()
	node.AddScore(0.2)

	// Get context with span
	ctx := context.Background()
	ctx, _ = tracer.TraceIteration(ctx, 1)

	// Should not panic
	tracer.TraceNodeAbandon(ctx, node, "low score")
}

func TestMCTSTracer_TraceDegradation(t *testing.T) {
	config := ObservabilityConfig{TracingEnabled: true}
	tracer := NewMCTSTracer(nil, config)

	// Should not panic even without span
	tracer.TraceDegradation(context.Background(), TreeDegradationNormal, TreeDegradationReduced, "consecutive failures")
}

func TestMCTSTracer_TraceCircuitBreakerStateChange(t *testing.T) {
	config := ObservabilityConfig{TracingEnabled: true}
	tracer := NewMCTSTracer(nil, config)

	// Should not panic even without span
	tracer.TraceCircuitBreakerStateChange(context.Background(), CircuitClosed, CircuitOpen)
}

func TestMCTSTracer_TraceBudgetExhaustion(t *testing.T) {
	config := ObservabilityConfig{TracingEnabled: true}
	tracer := NewMCTSTracer(nil, config)

	budget := NewTreeBudget(DefaultTreeBudgetConfig())
	budget.RecordNodeExplored()
	budget.RecordLLMCall(100, 0.01)

	// Should not panic
	tracer.TraceBudgetExhaustion(context.Background(), "max nodes reached", budget)
}

func TestTruncateForObs(t *testing.T) {
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
			got := truncateForObs(tt.input, tt.maxLen)
			if got != tt.want {
				t.Errorf("truncateForObs(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.want)
			}
		})
	}
}

func TestLoggerWithTrace(t *testing.T) {
	logger := slog.Default()

	// Without span context
	result := LoggerWithTrace(context.Background(), logger)
	if result == nil {
		t.Error("should return a logger")
	}
}

func TestNoopSpan(t *testing.T) {
	span := noop.Span{}

	// All methods should not panic
	span.End()
	span.AddEvent("test")
	span.RecordError(errors.New("test"))
	span.SetStatus(0, "test")
	span.SetName("test")
	span.SetAttributes()
	span.AddLink(trace.Link{})

	if span.IsRecording() {
		t.Error("noop span should not be recording")
	}
}
