// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package telemetry

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/trace"
)

// LoggerWithTrace returns a logger with trace context injected.
//
// Description:
//
//	Extracts trace_id and span_id from the context and adds them as
//	structured log fields. This enables log correlation in Grafana/Loki
//	with traces in Jaeger.
//
// Inputs:
//
//	ctx - Context containing span context. May be nil or have no active span.
//	logger - Base logger to enhance. Must not be nil.
//
// Outputs:
//
//	*slog.Logger - Logger with trace_id and span_id fields added if available.
//	              Returns the original logger if no valid span context.
//
// Example:
//
//	func (s *Service) Process(ctx context.Context) error {
//	    logger := telemetry.LoggerWithTrace(ctx, s.logger)
//	    logger.Info("processing started")
//	    // Log output: {"level":"INFO","msg":"processing started","trace_id":"abc123","span_id":"def456"}
//	}
//
// Thread Safety: Safe for concurrent use.
func LoggerWithTrace(ctx context.Context, logger *slog.Logger) *slog.Logger {
	if logger == nil {
		logger = slog.Default()
	}
	if ctx == nil {
		return logger
	}

	spanCtx := trace.SpanContextFromContext(ctx)
	if !spanCtx.IsValid() {
		return logger
	}

	return logger.With(
		slog.String("trace_id", spanCtx.TraceID().String()),
		slog.String("span_id", spanCtx.SpanID().String()),
	)
}

// LoggerWithNode returns a logger with trace context and node name.
//
// Description:
//
//	Combines LoggerWithTrace with a node identifier for DAG execution logging.
//	Useful for distinguishing log entries from different pipeline nodes.
//
// Inputs:
//
//	ctx - Context containing span context.
//	logger - Base logger to enhance.
//	nodeName - Name of the current DAG node (e.g., "PARSE_FILES").
//
// Outputs:
//
//	*slog.Logger - Logger with trace_id, span_id, and node fields.
//
// Example:
//
//	func (n *ParseNode) Execute(ctx context.Context, inputs map[string]any) (any, error) {
//	    logger := telemetry.LoggerWithNode(ctx, n.logger, n.Name())
//	    logger.Info("parsing files", slog.Int("count", len(files)))
//	}
//
// Thread Safety: Safe for concurrent use.
func LoggerWithNode(ctx context.Context, logger *slog.Logger, nodeName string) *slog.Logger {
	return LoggerWithTrace(ctx, logger).With(
		slog.String("node", nodeName),
	)
}

// LoggerWithSession returns a logger with trace context and session ID.
//
// Description:
//
//	Adds a session identifier for tracking related operations across
//	multiple requests or pipeline executions.
//
// Inputs:
//
//	ctx - Context containing span context.
//	logger - Base logger to enhance.
//	sessionID - Unique session identifier.
//
// Outputs:
//
//	*slog.Logger - Logger with trace_id, span_id, and session_id fields.
//
// Thread Safety: Safe for concurrent use.
func LoggerWithSession(ctx context.Context, logger *slog.Logger, sessionID string) *slog.Logger {
	return LoggerWithTrace(ctx, logger).With(
		slog.String("session_id", sessionID),
	)
}
