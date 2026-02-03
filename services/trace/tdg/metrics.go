// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package tdg

import (
	"context"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// Package-level tracer and meter for TDG operations.
var (
	tracer = otel.Tracer("aleutian.tdg")
	meter  = otel.Meter("aleutian.tdg")
)

// Metrics for TDG operations.
var (
	sessionLatency   metric.Float64Histogram
	sessionTotal     metric.Int64Counter
	stateTransitions metric.Int64Counter
	testAttempts     metric.Int64Counter
	fixAttempts      metric.Int64Counter
	llmCalls         metric.Int64Counter

	metricsOnce sync.Once
	metricsErr  error
)

// initMetrics initializes the metrics. Safe to call multiple times.
func initMetrics() error {
	metricsOnce.Do(func() {
		var err error

		sessionLatency, err = meter.Float64Histogram(
			"tdg_session_duration_seconds",
			metric.WithDescription("Duration of TDG sessions"),
			metric.WithUnit("s"),
		)
		if err != nil {
			metricsErr = err
			return
		}

		sessionTotal, err = meter.Int64Counter(
			"tdg_session_total",
			metric.WithDescription("Total number of TDG sessions"),
		)
		if err != nil {
			metricsErr = err
			return
		}

		stateTransitions, err = meter.Int64Counter(
			"tdg_state_transitions_total",
			metric.WithDescription("Total number of TDG state transitions"),
		)
		if err != nil {
			metricsErr = err
			return
		}

		testAttempts, err = meter.Int64Counter(
			"tdg_test_attempts_total",
			metric.WithDescription("Total number of test generation attempts"),
		)
		if err != nil {
			metricsErr = err
			return
		}

		fixAttempts, err = meter.Int64Counter(
			"tdg_fix_attempts_total",
			metric.WithDescription("Total number of fix generation attempts"),
		)
		if err != nil {
			metricsErr = err
			return
		}

		llmCalls, err = meter.Int64Counter(
			"tdg_llm_calls_total",
			metric.WithDescription("Total number of LLM calls"),
		)
		if err != nil {
			metricsErr = err
			return
		}
	})
	return metricsErr
}

// startSessionSpan creates a span for a TDG session.
func startSessionSpan(ctx context.Context, sessionID, language string) (context.Context, trace.Span) {
	return tracer.Start(ctx, "Controller.Run",
		trace.WithAttributes(
			attribute.String("tdg.session_id", sessionID),
			attribute.String("tdg.language", language),
		),
	)
}

// setSessionSpanResult sets the result attributes on a session span.
func setSessionSpanResult(span trace.Span, success bool, finalState string, testAttempts, fixAttempts int) {
	span.SetAttributes(
		attribute.Bool("tdg.success", success),
		attribute.String("tdg.final_state", finalState),
		attribute.Int("tdg.test_attempts", testAttempts),
		attribute.Int("tdg.fix_attempts", fixAttempts),
	)
}

// recordSessionMetrics records metrics for a TDG session.
func recordSessionMetrics(ctx context.Context, language string, duration time.Duration, success bool, testAttemptCount, fixAttemptCount, llmCallCount int) {
	if err := initMetrics(); err != nil {
		return
	}

	attrs := metric.WithAttributes(
		attribute.String("language", language),
		attribute.Bool("success", success),
	)

	sessionLatency.Record(ctx, duration.Seconds(), attrs)
	sessionTotal.Add(ctx, 1, attrs)
	testAttempts.Add(ctx, int64(testAttemptCount), attrs)
	fixAttempts.Add(ctx, int64(fixAttemptCount), attrs)
	llmCalls.Add(ctx, int64(llmCallCount), attrs)
}

// recordStateTransition records a state transition event.
func recordStateTransition(ctx context.Context, from, to string) {
	if err := initMetrics(); err != nil {
		return
	}
	stateTransitions.Add(ctx, 1, metric.WithAttributes(
		attribute.String("from", from),
		attribute.String("to", to),
	))
}

// addStateTransitionEvent adds a state transition event to the span.
func addStateTransitionEvent(span trace.Span, from, to State, testRetries, fixRetries int) {
	span.AddEvent("state_transition", trace.WithAttributes(
		attribute.String("from", string(from)),
		attribute.String("to", string(to)),
		attribute.Int("test_retries", testRetries),
		attribute.Int("fix_retries", fixRetries),
	))
}
