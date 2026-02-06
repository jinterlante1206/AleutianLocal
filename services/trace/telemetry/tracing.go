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
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// StartSpan creates a new span from the context using the global tracer.
//
// Description:
//
//	Convenience wrapper that uses otel.Tracer() to create spans without
//	explicitly managing tracer instances. Uses consistent naming conventions.
//
// Inputs:
//
//	ctx - Parent context. May contain existing span context.
//	tracerName - Tracer name (typically package path, e.g., "trace.graph").
//	spanName - Span name (typically "package.Type.Method" or operation name).
//	opts - Optional span start options (attributes, links, etc.).
//
// Outputs:
//
//	context.Context - Context with the new span attached.
//	trace.Span - The created span. Caller must call span.End().
//
// Example:
//
//	func (g *Graph) Query(ctx context.Context, query string) ([]Result, error) {
//	    ctx, span := telemetry.StartSpan(ctx, "trace.graph", "Graph.Query",
//	        trace.WithAttributes(attribute.String("query", query)),
//	    )
//	    defer span.End()
//	    // ... perform query
//	}
//
// Thread Safety: Safe for concurrent use.
func StartSpan(ctx context.Context, tracerName, spanName string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	return otel.Tracer(tracerName).Start(ctx, spanName, opts...)
}

// SpanFromContext returns the current span from the context.
//
// Description:
//
//	Convenience wrapper around trace.SpanFromContext.
//	Returns a no-op span if no span is present in the context.
//
// Inputs:
//
//	ctx - Context potentially containing a span.
//
// Outputs:
//
//	trace.Span - The current span, or a no-op span if none exists.
//
// Example:
//
//	func logWithSpan(ctx context.Context, msg string) {
//	    span := telemetry.SpanFromContext(ctx)
//	    span.AddEvent(msg)
//	}
//
// Thread Safety: Safe for concurrent use.
func SpanFromContext(ctx context.Context) trace.Span {
	return trace.SpanFromContext(ctx)
}

// RecordError records an error on the current span with proper status.
//
// Description:
//
//	Records the error as a span event and sets the span status to Error.
//	If the span or error is nil, this is a no-op.
//
// Inputs:
//
//	span - The span to record the error on. May be nil.
//	err - The error to record. May be nil.
//	attrs - Optional additional attributes to record with the error.
//
// Example:
//
//	result, err := performOperation()
//	if err != nil {
//	    telemetry.RecordError(span, err, attribute.String("operation", "parse"))
//	    return err
//	}
//
// Thread Safety: Safe for concurrent use.
func RecordError(span trace.Span, err error, attrs ...attribute.KeyValue) {
	if span == nil || err == nil {
		return
	}

	// Record the error as a span event with additional attributes
	opts := make([]trace.EventOption, 0, 1)
	if len(attrs) > 0 {
		opts = append(opts, trace.WithAttributes(attrs...))
	}
	span.RecordError(err, opts...)

	// Set span status to error
	span.SetStatus(codes.Error, err.Error())
}

// RecordErrorf records a formatted error message on the current span.
//
// Description:
//
//	Creates an error from the format string and records it on the span.
//	Useful when you want to add context to an error before recording.
//
// Inputs:
//
//	span - The span to record the error on. May be nil.
//	format - Printf-style format string.
//	args - Format arguments.
//
// Example:
//
//	if err := validate(input); err != nil {
//	    telemetry.RecordErrorf(span, "validation failed for %s: %v", input.Name, err)
//	    return err
//	}
//
// Thread Safety: Safe for concurrent use.
func RecordErrorf(span trace.Span, format string, args ...interface{}) {
	if span == nil {
		return
	}
	err := fmt.Errorf(format, args...)
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
}

// SetSpanOK marks the span as successful.
//
// Description:
//
//	Sets the span status to OK. Use this when an operation completes
//	successfully and you want to explicitly mark the span.
//
// Inputs:
//
//	span - The span to mark as OK. May be nil.
//
// Example:
//
//	result, err := performOperation()
//	if err != nil {
//	    telemetry.RecordError(span, err)
//	    return nil, err
//	}
//	telemetry.SetSpanOK(span)
//	return result, nil
//
// Thread Safety: Safe for concurrent use.
func SetSpanOK(span trace.Span) {
	if span == nil {
		return
	}
	span.SetStatus(codes.Ok, "")
}

// AddSpanEvent adds an event to the span with optional attributes.
//
// Description:
//
//	Records a timestamped event on the span. Events are useful for
//	marking significant points in a span's lifecycle.
//
// Inputs:
//
//	span - The span to add the event to. May be nil.
//	name - Event name describing what happened.
//	attrs - Optional attributes to include with the event.
//
// Example:
//
//	telemetry.AddSpanEvent(span, "cache_miss", attribute.String("key", cacheKey))
//
// Thread Safety: Safe for concurrent use.
func AddSpanEvent(span trace.Span, name string, attrs ...attribute.KeyValue) {
	if span == nil {
		return
	}
	span.AddEvent(name, trace.WithAttributes(attrs...))
}

// SetSpanAttributes sets attributes on the span.
//
// Description:
//
//	Adds or updates attributes on the span. Safe to call with nil span.
//
// Inputs:
//
//	span - The span to set attributes on. May be nil.
//	attrs - Attributes to set.
//
// Example:
//
//	telemetry.SetSpanAttributes(span,
//	    attribute.Int("result_count", len(results)),
//	    attribute.String("query_type", "find_callers"),
//	)
//
// Thread Safety: Safe for concurrent use.
func SetSpanAttributes(span trace.Span, attrs ...attribute.KeyValue) {
	if span == nil {
		return
	}
	span.SetAttributes(attrs...)
}

// TraceID returns the trace ID from the context as a string.
//
// Description:
//
//	Extracts the trace ID from the span context. Returns empty string
//	if no valid span context is present.
//
// Inputs:
//
//	ctx - Context potentially containing a span.
//
// Outputs:
//
//	string - Hex-encoded trace ID, or empty string if unavailable.
//
// Example:
//
//	traceID := telemetry.TraceID(ctx)
//	logger.Info("operation complete", slog.String("trace_id", traceID))
//
// Thread Safety: Safe for concurrent use.
func TraceID(ctx context.Context) string {
	spanCtx := trace.SpanContextFromContext(ctx)
	if !spanCtx.IsValid() {
		return ""
	}
	return spanCtx.TraceID().String()
}

// SpanID returns the span ID from the context as a string.
//
// Description:
//
//	Extracts the span ID from the span context. Returns empty string
//	if no valid span context is present.
//
// Inputs:
//
//	ctx - Context potentially containing a span.
//
// Outputs:
//
//	string - Hex-encoded span ID, or empty string if unavailable.
//
// Thread Safety: Safe for concurrent use.
func SpanID(ctx context.Context) string {
	spanCtx := trace.SpanContextFromContext(ctx)
	if !spanCtx.IsValid() {
		return ""
	}
	return spanCtx.SpanID().String()
}

// HasActiveSpan returns true if the context contains a valid, recording span.
//
// Description:
//
//	Checks whether the context has an active span that is recording.
//	Useful for conditional instrumentation.
//
// Inputs:
//
//	ctx - Context to check.
//
// Outputs:
//
//	bool - True if context has a valid, recording span.
//
// Example:
//
//	if telemetry.HasActiveSpan(ctx) {
//	    span := telemetry.SpanFromContext(ctx)
//	    span.AddEvent("expensive_operation_start")
//	}
//
// Thread Safety: Safe for concurrent use.
func HasActiveSpan(ctx context.Context) bool {
	span := trace.SpanFromContext(ctx)
	return span.SpanContext().IsValid() && span.IsRecording()
}
