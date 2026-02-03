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
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// Package-level tracer and meter for MCTS operations.
var (
	tracer = otel.Tracer("aleutian.mcts")
	meter  = otel.Meter("aleutian.mcts")
)

// Metrics for MCTS operations.
var (
	// Cost metrics
	llmCostTotal   metric.Float64Counter
	llmCallsTotal  metric.Int64Counter
	llmTokensTotal metric.Int64Counter

	// Node metrics
	nodesCreated   metric.Int64Counter
	nodesAbandoned metric.Int64Counter
	nodesPruned    metric.Int64Counter

	// Simulation metrics
	simulationsTotal   metric.Int64Counter
	simulationDuration metric.Float64Histogram

	// Tree metrics
	treeDepth     metric.Int64Histogram
	bestPathScore metric.Float64Histogram

	// Budget utilization
	budgetUtilization metric.Float64Histogram

	// Degradation metrics
	degradationEvents metric.Int64Counter

	// Circuit breaker metrics
	circuitBreakerState metric.Int64UpDownCounter

	metricsOnce sync.Once
	metricsErr  error
)

// initMetrics initializes the metrics. Safe to call multiple times.
func initMetrics() error {
	metricsOnce.Do(func() {
		var err error

		// Cost metrics
		llmCostTotal, err = meter.Float64Counter(
			"mcts_llm_cost_usd_total",
			metric.WithDescription("Total LLM cost in USD for MCTS planning"),
			metric.WithUnit("USD"),
		)
		if err != nil {
			metricsErr = err
			return
		}

		llmCallsTotal, err = meter.Int64Counter(
			"mcts_llm_calls_total",
			metric.WithDescription("Total LLM calls by outcome"),
		)
		if err != nil {
			metricsErr = err
			return
		}

		llmTokensTotal, err = meter.Int64Counter(
			"mcts_tokens_total",
			metric.WithDescription("Total tokens used in MCTS planning"),
		)
		if err != nil {
			metricsErr = err
			return
		}

		// Node metrics
		nodesCreated, err = meter.Int64Counter(
			"mcts_nodes_created_total",
			metric.WithDescription("Total nodes created in MCTS trees"),
		)
		if err != nil {
			metricsErr = err
			return
		}

		nodesAbandoned, err = meter.Int64Counter(
			"mcts_nodes_abandoned_total",
			metric.WithDescription("Total nodes abandoned during exploration"),
		)
		if err != nil {
			metricsErr = err
			return
		}

		nodesPruned, err = meter.Int64Counter(
			"mcts_nodes_pruned_total",
			metric.WithDescription("Total nodes pruned from trees"),
		)
		if err != nil {
			metricsErr = err
			return
		}

		// Simulation metrics
		simulationsTotal, err = meter.Int64Counter(
			"mcts_simulations_total",
			metric.WithDescription("Total simulations by tier and outcome"),
		)
		if err != nil {
			metricsErr = err
			return
		}

		simulationDuration, err = meter.Float64Histogram(
			"mcts_simulation_duration_seconds",
			metric.WithDescription("Simulation duration by tier"),
			metric.WithUnit("s"),
		)
		if err != nil {
			metricsErr = err
			return
		}

		// Tree metrics
		treeDepth, err = meter.Int64Histogram(
			"mcts_tree_depth",
			metric.WithDescription("Final tree depth"),
		)
		if err != nil {
			metricsErr = err
			return
		}

		bestPathScore, err = meter.Float64Histogram(
			"mcts_best_path_score",
			metric.WithDescription("Score of best path found"),
		)
		if err != nil {
			metricsErr = err
			return
		}

		// Budget utilization
		budgetUtilization, err = meter.Float64Histogram(
			"mcts_budget_utilization_percent",
			metric.WithDescription("Budget utilization at completion"),
			metric.WithUnit("%"),
		)
		if err != nil {
			metricsErr = err
			return
		}

		// Degradation metrics
		degradationEvents, err = meter.Int64Counter(
			"mcts_degradation_events_total",
			metric.WithDescription("Total degradation events by from/to level"),
		)
		if err != nil {
			metricsErr = err
			return
		}

		// Circuit breaker metrics
		circuitBreakerState, err = meter.Int64UpDownCounter(
			"mcts_circuit_breaker_state",
			metric.WithDescription("Current circuit breaker state (0=closed, 1=half-open, 2=open)"),
		)
		if err != nil {
			metricsErr = err
			return
		}
	})
	return metricsErr
}

// RecordLLMCall records metrics for an LLM call.
//
// Inputs:
//   - ctx: Context for metric recording.
//   - tokens: Number of tokens used.
//   - costUSD: Cost in USD.
//   - success: Whether the call succeeded.
//
// Thread Safety: Safe for concurrent use.
func RecordLLMCall(ctx context.Context, tokens int, costUSD float64, success bool) {
	if err := initMetrics(); err != nil {
		return
	}

	outcome := "success"
	if !success {
		outcome = "failure"
	}

	attrs := metric.WithAttributes(attribute.String("outcome", outcome))

	llmTokensTotal.Add(ctx, int64(tokens), attrs)
	llmCostTotal.Add(ctx, costUSD, attrs)
	llmCallsTotal.Add(ctx, 1, attrs)
}

// RecordNodeCreated records that a node was created.
//
// Thread Safety: Safe for concurrent use.
func RecordNodeCreated(ctx context.Context) {
	if err := initMetrics(); err != nil {
		return
	}
	nodesCreated.Add(ctx, 1)
}

// RecordNodeAbandoned records that a node was abandoned.
//
// Inputs:
//   - ctx: Context for metric recording.
//   - reason: Reason for abandonment.
//
// Thread Safety: Safe for concurrent use.
func RecordNodeAbandoned(ctx context.Context, reason string) {
	if err := initMetrics(); err != nil {
		return
	}
	nodesAbandoned.Add(ctx, 1, metric.WithAttributes(attribute.String("reason", reason)))
}

// RecordNodesPruned records the number of nodes pruned.
//
// Thread Safety: Safe for concurrent use.
func RecordNodesPruned(ctx context.Context, count int) {
	if err := initMetrics(); err != nil {
		return
	}
	nodesPruned.Add(ctx, int64(count))
}

// RecordSimulation records metrics for a simulation.
//
// Inputs:
//   - ctx: Context for metric recording.
//   - tier: Simulation tier (quick, standard, full).
//   - score: Simulation score.
//   - duration: Time taken for simulation.
//
// Thread Safety: Safe for concurrent use.
func RecordSimulation(ctx context.Context, tier SimulationTier, score float64, duration time.Duration) {
	if err := initMetrics(); err != nil {
		return
	}

	outcome := "pass"
	if score < 0.5 {
		outcome = "fail"
	}

	attrs := metric.WithAttributes(
		attribute.String("tier", tier.String()),
		attribute.String("outcome", outcome),
	)

	simulationsTotal.Add(ctx, 1, attrs)
	simulationDuration.Record(ctx, duration.Seconds(), attrs)
}

// TreeCompletionStats contains statistics for tree completion.
type TreeCompletionStats struct {
	TotalNodes  int64
	PrunedNodes int64
	MaxDepth    int
	BestScore   float64
}

// RecordTreeCompletion records metrics at tree completion.
//
// Inputs:
//   - ctx: Context for metric recording.
//   - stats: Tree completion statistics.
//
// Thread Safety: Safe for concurrent use.
func RecordTreeCompletion(ctx context.Context, stats TreeCompletionStats) {
	if err := initMetrics(); err != nil {
		return
	}

	treeDepth.Record(ctx, int64(stats.MaxDepth))
	bestPathScore.Record(ctx, stats.BestScore)
}

// BudgetUtilizationStats contains budget utilization information.
type BudgetUtilizationStats struct {
	NodesUsed   int64
	NodesMax    int64
	TokensUsed  int64
	TokensMax   int64
	CallsUsed   int64
	CallsMax    int64
	CostUsedUSD float64
	CostMaxUSD  float64
	Elapsed     time.Duration
	TimeLimit   time.Duration
}

// RecordBudgetUtilization records budget utilization metrics.
//
// Inputs:
//   - ctx: Context for metric recording.
//   - usage: Budget utilization statistics.
//
// Thread Safety: Safe for concurrent use.
func RecordBudgetUtilization(ctx context.Context, usage BudgetUtilizationStats) {
	if err := initMetrics(); err != nil {
		return
	}

	if usage.NodesMax > 0 {
		pct := float64(usage.NodesUsed) / float64(usage.NodesMax) * 100
		budgetUtilization.Record(ctx, pct, metric.WithAttributes(attribute.String("dimension", "nodes")))
	}
	if usage.TokensMax > 0 {
		pct := float64(usage.TokensUsed) / float64(usage.TokensMax) * 100
		budgetUtilization.Record(ctx, pct, metric.WithAttributes(attribute.String("dimension", "tokens")))
	}
	if usage.CallsMax > 0 {
		pct := float64(usage.CallsUsed) / float64(usage.CallsMax) * 100
		budgetUtilization.Record(ctx, pct, metric.WithAttributes(attribute.String("dimension", "calls")))
	}
	if usage.CostMaxUSD > 0 {
		pct := usage.CostUsedUSD / usage.CostMaxUSD * 100
		budgetUtilization.Record(ctx, pct, metric.WithAttributes(attribute.String("dimension", "cost")))
	}
	if usage.TimeLimit > 0 {
		pct := float64(usage.Elapsed) / float64(usage.TimeLimit) * 100
		budgetUtilization.Record(ctx, pct, metric.WithAttributes(attribute.String("dimension", "time")))
	}
}

// RecordDegradation records a degradation event.
//
// Inputs:
//   - ctx: Context for metric recording.
//   - from: Previous degradation level.
//   - to: New degradation level.
//
// Thread Safety: Safe for concurrent use.
func RecordDegradation(ctx context.Context, from, to TreeDegradation) {
	if err := initMetrics(); err != nil {
		return
	}
	degradationEvents.Add(ctx, 1, metric.WithAttributes(
		attribute.String("from", from.String()),
		attribute.String("to", to.String()),
	))
}

// RecordCircuitBreakerState records the circuit breaker state change.
//
// Inputs:
//   - ctx: Context for metric recording.
//   - state: New circuit breaker state.
//
// Thread Safety: Safe for concurrent use.
func RecordCircuitBreakerState(ctx context.Context, state CircuitState) {
	if err := initMetrics(); err != nil {
		return
	}

	// Reset all states and set current
	// This is a simplification - a more sophisticated approach would track per-instance
	var value int64
	switch state {
	case CircuitClosed:
		value = 0
	case CircuitHalfOpen:
		value = 1
	case CircuitOpen:
		value = 2
	}
	circuitBreakerState.Add(ctx, value)
}

// StartMCTSSpan creates a span for MCTS operations.
//
// Inputs:
//   - ctx: Parent context.
//   - operation: Operation name.
//   - task: Task description.
//
// Outputs:
//   - context.Context: Context with span.
//   - trace.Span: The created span.
//
// Thread Safety: Safe for concurrent use.
func StartMCTSSpan(ctx context.Context, operation, task string) (context.Context, trace.Span) {
	return tracer.Start(ctx, operation,
		trace.WithAttributes(
			attribute.String("mcts.task", truncateForAttribute(task, 100)),
		),
	)
}

// SetMCTSSpanResult sets result attributes on an MCTS span.
//
// Thread Safety: Safe for concurrent use.
func SetMCTSSpanResult(span trace.Span, success bool, nodesExplored int64, bestScore float64) {
	span.SetAttributes(
		attribute.Bool("mcts.success", success),
		attribute.Int64("mcts.nodes_explored", nodesExplored),
		attribute.Float64("mcts.best_score", bestScore),
	)
}

// AddMCTSEvent adds an event to the current span.
//
// Thread Safety: Safe for concurrent use.
func AddMCTSEvent(span trace.Span, name string, attrs ...attribute.KeyValue) {
	span.AddEvent(name, trace.WithAttributes(attrs...))
}

// truncateForAttribute truncates a string for use in span attributes.
func truncateForAttribute(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
