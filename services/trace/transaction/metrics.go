// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package transaction

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// Package-level meter for transaction metrics.
var meter = otel.Meter("aleutian.transaction")

// Metric instruments for transaction operations.
var (
	beginTotal          metric.Int64Counter
	commitTotal         metric.Int64Counter
	rollbackTotal       metric.Int64Counter
	expiredTotal        metric.Int64Counter
	transactionDuration metric.Float64Histogram
	filesModified       metric.Int64Histogram
	activeGauge         metric.Int64UpDownCounter
	gitOpDuration       metric.Float64Histogram
	gitOpErrors         metric.Int64Counter

	metricsOnce sync.Once
	metricsErr  error
)

// metricsEnabled controls whether metrics are recorded.
// Set by the Manager on initialization.
//
// Thread Safety: Uses atomic operations for safe concurrent access.
var metricsEnabled atomic.Bool

func init() {
	metricsEnabled.Store(true)
}

// SetMetricsEnabled controls whether metrics are recorded.
//
// Thread Safety: Safe for concurrent use.
func SetMetricsEnabled(enabled bool) {
	metricsEnabled.Store(enabled)
}

// initMetrics initializes all metric instruments.
// Safe to call multiple times; uses sync.Once internally.
func initMetrics() error {
	metricsOnce.Do(func() {
		var err error

		beginTotal, err = meter.Int64Counter(
			"transaction_begin_total",
			metric.WithDescription("Total number of transaction begin operations"),
		)
		if err != nil {
			metricsErr = err
			return
		}

		commitTotal, err = meter.Int64Counter(
			"transaction_commit_total",
			metric.WithDescription("Total number of transaction commit operations"),
		)
		if err != nil {
			metricsErr = err
			return
		}

		rollbackTotal, err = meter.Int64Counter(
			"transaction_rollback_total",
			metric.WithDescription("Total number of transaction rollback operations"),
		)
		if err != nil {
			metricsErr = err
			return
		}

		expiredTotal, err = meter.Int64Counter(
			"transaction_expired_total",
			metric.WithDescription("Total number of transactions that expired"),
		)
		if err != nil {
			metricsErr = err
			return
		}

		transactionDuration, err = meter.Float64Histogram(
			"transaction_duration_seconds",
			metric.WithDescription("Duration of transactions in seconds"),
			metric.WithUnit("s"),
		)
		if err != nil {
			metricsErr = err
			return
		}

		filesModified, err = meter.Int64Histogram(
			"transaction_files_modified",
			metric.WithDescription("Number of files modified per transaction"),
		)
		if err != nil {
			metricsErr = err
			return
		}

		activeGauge, err = meter.Int64UpDownCounter(
			"transaction_active",
			metric.WithDescription("Number of currently active transactions"),
		)
		if err != nil {
			metricsErr = err
			return
		}

		gitOpDuration, err = meter.Float64Histogram(
			"transaction_git_operation_duration_seconds",
			metric.WithDescription("Duration of git operations in seconds"),
			metric.WithUnit("s"),
		)
		if err != nil {
			metricsErr = err
			return
		}

		gitOpErrors, err = meter.Int64Counter(
			"transaction_git_operation_errors_total",
			metric.WithDescription("Total number of git operation errors"),
		)
		if err != nil {
			metricsErr = err
			return
		}
	})
	return metricsErr
}

// recordBegin records a transaction begin operation.
//
// # Inputs
//
//   - ctx: Context for metric recording.
//   - strategy: The checkpoint strategy used.
//   - success: Whether the begin operation succeeded.
func recordBegin(ctx context.Context, strategy Strategy, success bool) {
	if !metricsEnabled.Load() {
		return
	}
	if err := initMetrics(); err != nil {
		return
	}

	status := "success"
	if !success {
		status = "error"
	}

	beginTotal.Add(ctx, 1, metric.WithAttributes(
		attribute.String("strategy", string(strategy)),
		attribute.String("status", status),
	))
}

// recordCommit records a transaction commit operation.
//
// # Inputs
//
//   - ctx: Context for metric recording.
//   - duration: How long the transaction was active.
//   - files: Number of files modified.
//   - success: Whether the commit operation succeeded.
func recordCommit(ctx context.Context, duration time.Duration, files int, success bool) {
	if !metricsEnabled.Load() {
		return
	}
	if err := initMetrics(); err != nil {
		return
	}

	status := "success"
	if !success {
		status = "error"
	}

	attrs := metric.WithAttributes(attribute.String("status", status))

	commitTotal.Add(ctx, 1, attrs)
	transactionDuration.Record(ctx, duration.Seconds(), attrs)
	filesModified.Record(ctx, int64(files), attrs)
}

// recordRollback records a transaction rollback operation.
//
// # Inputs
//
//   - ctx: Context for metric recording.
//   - duration: How long the transaction was active.
//   - files: Number of files modified.
//   - reason: Why the rollback occurred (user, expired, error, manager_close).
func recordRollback(ctx context.Context, duration time.Duration, files int, reason string) {
	if !metricsEnabled.Load() {
		return
	}
	if err := initMetrics(); err != nil {
		return
	}

	// Normalize reason to bounded set
	normalizedReason := normalizeRollbackReason(reason)

	attrs := metric.WithAttributes(
		attribute.String("status", "rolled_back"),
		attribute.String("reason", normalizedReason),
	)

	rollbackTotal.Add(ctx, 1, metric.WithAttributes(
		attribute.String("reason", normalizedReason),
	))
	transactionDuration.Record(ctx, duration.Seconds(), attrs)
	filesModified.Record(ctx, int64(files), attrs)
}

// normalizeRollbackReason normalizes rollback reasons to a bounded set.
func normalizeRollbackReason(reason string) string {
	switch {
	case reason == "transaction expired":
		return "expired"
	case reason == "manager closed":
		return "manager_close"
	case reason == "stale transaction cleanup":
		return "cleanup"
	default:
		return "user"
	}
}

// recordExpired records a transaction expiration.
//
// # Inputs
//
//   - ctx: Context for metric recording.
func recordExpired(ctx context.Context) {
	if !metricsEnabled.Load() {
		return
	}
	if err := initMetrics(); err != nil {
		return
	}

	expiredTotal.Add(ctx, 1)
}

// recordGitOp records a git operation.
//
// # Inputs
//
//   - ctx: Context for metric recording.
//   - operation: Name of the git operation (e.g., "stash_push", "create_branch").
//   - duration: How long the operation took.
//   - err: Error if the operation failed (nil on success).
func recordGitOp(ctx context.Context, operation string, duration time.Duration, opErr error) {
	if !metricsEnabled.Load() {
		return
	}
	if err := initMetrics(); err != nil {
		return
	}

	attrs := metric.WithAttributes(attribute.String("operation", operation))

	gitOpDuration.Record(ctx, duration.Seconds(), attrs)

	if opErr != nil {
		gitOpErrors.Add(ctx, 1, attrs)
	}
}

// incActive increments the active transaction gauge.
//
// # Inputs
//
//   - ctx: Context for metric recording.
func incActive(ctx context.Context) {
	if !metricsEnabled.Load() {
		return
	}
	if err := initMetrics(); err != nil {
		return
	}

	activeGauge.Add(ctx, 1)
}

// decActive decrements the active transaction gauge.
//
// # Inputs
//
//   - ctx: Context for metric recording.
func decActive(ctx context.Context) {
	if !metricsEnabled.Load() {
		return
	}
	if err := initMetrics(); err != nil {
		return
	}

	activeGauge.Add(ctx, -1)
}
