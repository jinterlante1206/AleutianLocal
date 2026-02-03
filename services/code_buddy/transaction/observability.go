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
	"log/slog"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

const transactionTracerName = "aleutian.transaction"

// Tracer provides OpenTelemetry tracing for transaction operations.
//
// # Description
//
// Wraps the OpenTelemetry tracer with transaction-specific span creation
// and attribute management. When disabled, returns noop spans for zero overhead.
//
// # Thread Safety
//
// All methods are safe for concurrent use.
type Tracer struct {
	tracer  trace.Tracer
	logger  *slog.Logger
	enabled bool
}

// NewTracer creates a new transaction tracer.
//
// # Inputs
//
//   - logger: Logger for structured logging. Uses slog.Default() if nil.
//   - enabled: Whether tracing is enabled. When false, uses noop spans.
//
// # Outputs
//
//   - *Tracer: Ready-to-use tracer instance.
func NewTracer(logger *slog.Logger, enabled bool) *Tracer {
	if logger == nil {
		logger = slog.Default()
	}
	return &Tracer{
		tracer:  otel.Tracer(transactionTracerName),
		logger:  logger,
		enabled: enabled,
	}
}

// StartBegin starts a span for a transaction begin operation.
//
// # Inputs
//
//   - ctx: Parent context for span creation.
//   - sessionID: Agent session identifier.
//   - strategy: Checkpoint strategy being used.
//
// # Outputs
//
//   - context.Context: Context with span attached.
//   - trace.Span: The created span. Caller must call End() when done.
func (t *Tracer) StartBegin(ctx context.Context, sessionID string, strategy Strategy) (context.Context, trace.Span) {
	if !t.enabled {
		return ctx, noop.Span{}
	}

	ctx, span := t.tracer.Start(ctx, "transaction.begin",
		trace.WithAttributes(
			attribute.String("tx.session_id", truncateForTrace(sessionID, 36)),
			attribute.String("tx.strategy", string(strategy)),
		),
		trace.WithSpanKind(trace.SpanKindInternal),
	)

	t.logger.DebugContext(ctx, "starting transaction",
		slog.String("session_id", sessionID),
		slog.String("strategy", string(strategy)),
	)

	return ctx, span
}

// EndBegin completes a transaction begin span.
//
// # Inputs
//
//   - span: The span to end.
//   - tx: The created transaction (may be nil on error).
//   - err: Error if begin failed.
func (t *Tracer) EndBegin(span trace.Span, tx *Transaction, err error) {
	if span == nil {
		return
	}
	defer span.End()

	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return
	}

	span.SetStatus(codes.Ok, "")
	if tx != nil {
		span.SetAttributes(
			attribute.String("tx.id", tx.ID),
			attribute.String("tx.checkpoint_ref", truncateForTrace(tx.CheckpointRef, 40)),
			attribute.String("tx.original_branch", tx.OriginalBranch),
		)
	}
}

// StartCommit starts a span for a transaction commit operation.
//
// # Inputs
//
//   - ctx: Parent context for span creation.
//   - tx: The transaction being committed.
//   - message: Commit message.
//
// # Outputs
//
//   - context.Context: Context with span attached.
//   - trace.Span: The created span. Caller must call End() when done.
func (t *Tracer) StartCommit(ctx context.Context, tx *Transaction, message string) (context.Context, trace.Span) {
	if !t.enabled {
		return ctx, noop.Span{}
	}

	ctx, span := t.tracer.Start(ctx, "transaction.commit",
		trace.WithAttributes(
			attribute.String("tx.id", tx.ID),
			attribute.String("tx.session_id", truncateForTrace(tx.SessionID, 36)),
			attribute.String("tx.message", truncateForTrace(message, 100)),
			attribute.Int("tx.files_count", tx.FileCount()),
		),
		trace.WithSpanKind(trace.SpanKindInternal),
	)

	t.logger.DebugContext(ctx, "committing transaction",
		slog.String("tx_id", tx.ID),
		slog.Int("files", tx.FileCount()),
	)

	return ctx, span
}

// EndCommit completes a transaction commit span.
//
// # Inputs
//
//   - span: The span to end.
//   - result: The commit result (may be nil on error).
//   - err: Error if commit failed.
func (t *Tracer) EndCommit(span trace.Span, result *Result, err error) {
	if span == nil {
		return
	}
	defer span.End()

	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return
	}

	span.SetStatus(codes.Ok, "")
	if result != nil {
		span.SetAttributes(
			attribute.String("tx.commit_sha", truncateForTrace(result.CommitSHA, 40)),
			attribute.Int64("tx.duration_ms", result.Duration.Milliseconds()),
			attribute.Int("tx.files_modified", result.FilesModified),
		)
	}
}

// StartRollback starts a span for a transaction rollback operation.
//
// # Inputs
//
//   - ctx: Parent context for span creation.
//   - tx: The transaction being rolled back.
//   - reason: Why the rollback is occurring.
//
// # Outputs
//
//   - context.Context: Context with span attached.
//   - trace.Span: The created span. Caller must call End() when done.
func (t *Tracer) StartRollback(ctx context.Context, tx *Transaction, reason string) (context.Context, trace.Span) {
	if !t.enabled {
		return ctx, noop.Span{}
	}

	ctx, span := t.tracer.Start(ctx, "transaction.rollback",
		trace.WithAttributes(
			attribute.String("tx.id", tx.ID),
			attribute.String("tx.session_id", truncateForTrace(tx.SessionID, 36)),
			attribute.String("tx.reason", truncateForTrace(reason, 100)),
			attribute.Int("tx.files_count", tx.FileCount()),
		),
		trace.WithSpanKind(trace.SpanKindInternal),
	)

	t.logger.DebugContext(ctx, "rolling back transaction",
		slog.String("tx_id", tx.ID),
		slog.String("reason", reason),
	)

	return ctx, span
}

// EndRollback completes a transaction rollback span.
//
// # Inputs
//
//   - span: The span to end.
//   - result: The rollback result (may be nil on error).
//   - err: Error if rollback failed.
func (t *Tracer) EndRollback(span trace.Span, result *Result, err error) {
	if span == nil {
		return
	}
	defer span.End()

	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return
	}

	span.SetStatus(codes.Ok, "")
	if result != nil {
		span.SetAttributes(
			attribute.Int64("tx.duration_ms", result.Duration.Milliseconds()),
			attribute.Int("tx.files_modified", result.FilesModified),
		)
	}
}

// StartGitOp starts a child span for a git operation.
//
// # Inputs
//
//   - ctx: Parent context (should contain parent span).
//   - operation: Name of the git operation (e.g., "create_branch", "reset_hard").
//
// # Outputs
//
//   - context.Context: Context with span attached.
//   - trace.Span: The created span. Caller must call End() when done.
func (t *Tracer) StartGitOp(ctx context.Context, operation string) (context.Context, trace.Span) {
	if !t.enabled {
		return ctx, noop.Span{}
	}

	return t.tracer.Start(ctx, "transaction.git."+operation,
		trace.WithAttributes(
			attribute.String("git.operation", operation),
		),
	)
}

// EndGitOp completes a git operation span.
//
// # Inputs
//
//   - span: The span to end.
//   - err: Error if the operation failed.
func (t *Tracer) EndGitOp(span trace.Span, err error) {
	if span == nil {
		return
	}
	defer span.End()

	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return
	}

	span.SetStatus(codes.Ok, "")
}

// RecordStateTransition records a state transition event on the current span.
//
// # Inputs
//
//   - ctx: Context containing the active span.
//   - txID: Transaction identifier.
//   - from: Previous state.
//   - to: New state.
//   - duration: Time spent in the previous state.
func (t *Tracer) RecordStateTransition(ctx context.Context, txID string, from, to Status, duration time.Duration) {
	span := trace.SpanFromContext(ctx)
	// Note: SpanFromContext returns noop span (not nil) when no span exists.
	// We check validity to avoid unnecessary calls to noop spans.
	if !span.SpanContext().IsValid() {
		return
	}

	span.AddEvent("state_transition",
		trace.WithAttributes(
			attribute.String("tx.id", txID),
			attribute.String("tx.from_state", string(from)),
			attribute.String("tx.to_state", string(to)),
			attribute.Int64("tx.duration_in_state_ms", duration.Milliseconds()),
		),
	)

	t.logger.DebugContext(ctx, "transaction state transition",
		slog.String("tx_id", txID),
		slog.String("from", string(from)),
		slog.String("to", string(to)),
		slog.Duration("duration", duration),
	)
}

// RecordExpiration records a transaction expiration event.
//
// # Inputs
//
//   - ctx: Context containing the active span.
//   - txID: Transaction identifier.
func (t *Tracer) RecordExpiration(ctx context.Context, txID string) {
	span := trace.SpanFromContext(ctx)
	// Note: SpanFromContext returns noop span (not nil) when no span exists.
	// We check validity to avoid unnecessary calls to noop spans.
	if span.SpanContext().IsValid() {
		span.AddEvent("transaction_expired",
			trace.WithAttributes(
				attribute.String("tx.id", txID),
			),
		)
	}

	t.logger.WarnContext(ctx, "transaction expired",
		slog.String("tx_id", txID),
	)
}

// truncateForTrace truncates a string for use in span attributes.
// Prevents excessive memory usage from long strings.
//
// If maxLen is less than 4, returns at most maxLen characters without suffix.
func truncateForTrace(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	// Need at least 4 chars to add "..." suffix (1 char + "...")
	if maxLen < 4 {
		if maxLen <= 0 {
			return ""
		}
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

// LoggerWithTrace returns a logger with trace context fields.
//
// # Description
//
// Extracts trace_id and span_id from the context and adds them
// to the logger for correlation with distributed traces.
//
// # Inputs
//
//   - ctx: Context that may contain trace information.
//   - logger: Base logger to extend.
//
// # Outputs
//
//   - *slog.Logger: Logger with trace_id and span_id if available.
func LoggerWithTrace(ctx context.Context, logger *slog.Logger) *slog.Logger {
	spanCtx := trace.SpanContextFromContext(ctx)
	if !spanCtx.IsValid() {
		return logger
	}
	return logger.With(
		slog.String("trace_id", spanCtx.TraceID().String()),
		slog.String("span_id", spanCtx.SpanID().String()),
	)
}
