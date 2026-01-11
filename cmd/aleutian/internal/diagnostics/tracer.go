// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

/*
Package diagnostics provides OpenTelemetry integration for the Distributed Health Agent.

This file implements the DiagnosticsTracer interface, enabling trace-based
debugging via Jaeger and other OpenTelemetry-compatible backends.

# Open Core Architecture

This follows the Open Core model:

  - FOSS (NoOpDiagnosticsTracer): Generates valid IDs but no network export
  - Enterprise (OTelDiagnosticsTracer): Full Jaeger/OTLP export with context propagation

The interface is public; the implementation dictates the value.

# Why OpenTelemetry?

OpenTelemetry enables the "Support Ticket Revolution":

  - User reports: "Error occurred, trace ID: abc123..."
  - Support opens Jaeger, sees entire request flow
  - Root cause identified in minutes instead of hours

# Integration Points

  - DiagnosticsCollector: Creates spans for collection operations
  - DiagnosticsViewer: Extracts trace ID for lookup
  - PanicRecoveryHandler: Links crash diagnostics to triggering request

# Trace ID Format

Both implementations generate W3C-compatible 32-character hex trace IDs
and 16-character hex span IDs for compatibility with Jaeger/Zipkin.
*/
package diagnostics

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.21.0"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// -----------------------------------------------------------------------------
// DiagnosticsTracer Interface
// -----------------------------------------------------------------------------

// DiagnosticsTracer provides OpenTelemetry tracing for diagnostics operations.
//
// # Description
//
// Abstracts OpenTelemetry span creation to enable both FOSS (no export)
// and Enterprise (full Jaeger export) modes.
//
// # Thread Safety
//
// All implementations must be safe for concurrent use.
type DiagnosticsTracer interface {
	// StartSpan creates a new span for a diagnostic operation.
	//
	// # Description
	//
	// Creates an OpenTelemetry span with the given name and attributes.
	// Returns a context with the span and a finish function.
	//
	// # Inputs
	//
	//   - ctx: Parent context (may contain existing trace)
	//   - name: Span name (e.g., "diagnostics.collect")
	//   - attrs: Attributes to attach to the span
	//
	// # Outputs
	//
	//   - context.Context: Context with span for propagation
	//   - func(error): Call to end span (pass nil for success, error for failure)
	//
	// # Examples
	//
	//	ctx, finish := tracer.StartSpan(ctx, "diagnostics.collect",
	//	    map[string]string{"reason": "startup_failure"})
	//	defer finish(nil)
	//
	// # Limitations
	//
	//   - NoOp tracer generates IDs but doesn't export
	//
	// # Assumptions
	//
	//   - Called from within a goroutine with proper context
	StartSpan(ctx context.Context, name string, attrs map[string]string) (context.Context, func(error))

	// GetTraceID returns the trace ID from the current context.
	//
	// # Description
	//
	// Extracts the W3C trace ID from the context's span.
	// Returns empty string if no span exists.
	//
	// # Inputs
	//
	//   - ctx: Context potentially containing a span
	//
	// # Outputs
	//
	//   - string: 32-character hex trace ID, or empty string
	//
	// # Examples
	//
	//	traceID := tracer.GetTraceID(ctx)
	//	if traceID != "" {
	//	    log.Printf("Trace ID: %s", traceID)
	//	}
	//
	// # Limitations
	//
	//   - Returns empty if context has no span
	//
	// # Assumptions
	//
	//   - Context was created by StartSpan or propagated from upstream
	GetTraceID(ctx context.Context) string

	// GetSpanID returns the span ID from the current context.
	//
	// # Description
	//
	// Extracts the 16-character hex span ID from the context's span.
	// Returns empty string if no span exists.
	//
	// # Inputs
	//
	//   - ctx: Context potentially containing a span
	//
	// # Outputs
	//
	//   - string: 16-character hex span ID, or empty string
	//
	// # Examples
	//
	//	spanID := tracer.GetSpanID(ctx)
	//
	// # Limitations
	//
	//   - Returns empty if context has no span
	//
	// # Assumptions
	//
	//   - Context was created by StartSpan
	GetSpanID(ctx context.Context) string

	// GenerateTraceID creates a new random trace ID.
	//
	// # Description
	//
	// Generates a W3C-compatible 32-character hex trace ID.
	// Used when no parent span exists.
	//
	// # Outputs
	//
	//   - string: 32-character hex trace ID
	//
	// # Examples
	//
	//	traceID := tracer.GenerateTraceID()
	//
	// # Limitations
	//
	//   - Uses crypto/rand; may block if entropy exhausted
	//
	// # Assumptions
	//
	//   - System has adequate entropy source
	GenerateTraceID() string

	// GenerateSpanID creates a new random span ID.
	//
	// # Description
	//
	// Generates a W3C-compatible 16-character hex span ID.
	//
	// # Outputs
	//
	//   - string: 16-character hex span ID
	//
	// # Examples
	//
	//	spanID := tracer.GenerateSpanID()
	//
	// # Limitations
	//
	//   - Uses crypto/rand; may block if entropy exhausted
	//
	// # Assumptions
	//
	//   - System has adequate entropy source
	GenerateSpanID() string

	// Shutdown gracefully stops the tracer.
	//
	// # Description
	//
	// Flushes any pending spans and releases resources.
	// Should be called before application exit.
	//
	// # Inputs
	//
	//   - ctx: Context for timeout control
	//
	// # Outputs
	//
	//   - error: Non-nil if shutdown fails
	//
	// # Examples
	//
	//	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	//	defer cancel()
	//	if err := tracer.Shutdown(ctx); err != nil {
	//	    log.Printf("Tracer shutdown failed: %v", err)
	//	}
	//
	// # Limitations
	//
	//   - NoOp tracer has nothing to flush
	//
	// # Assumptions
	//
	//   - Application is shutting down
	Shutdown(ctx context.Context) error
}

// -----------------------------------------------------------------------------
// NoOpDiagnosticsTracer Implementation (FOSS Tier)
// -----------------------------------------------------------------------------

// NoOpDiagnosticsTracer is the FOSS-tier tracer that generates IDs but doesn't export.
//
// # Description
//
// This implementation satisfies the DiagnosticsTracer interface without
// requiring network connectivity or an OpenTelemetry collector. It generates
// valid W3C-format trace and span IDs for logging and correlation.
//
// # Enterprise Alternative
//
// OTelDiagnosticsTracer (Enterprise) provides:
//   - Full Jaeger/OTLP export for distributed tracing
//   - Context propagation across service boundaries
//   - Span hierarchy visualization in Jaeger UI
//
// # Capabilities
//
//   - Generates cryptographically random trace/span IDs
//   - No network dependencies
//   - Zero configuration
//   - Works offline
//
// # Thread Safety
//
// NoOpDiagnosticsTracer is safe for concurrent use.
type NoOpDiagnosticsTracer struct {
	// serviceName identifies this service in trace metadata.
	serviceName string

	// mu protects concurrent ID generation.
	mu sync.Mutex
}

// NewNoOpDiagnosticsTracer creates a FOSS-tier tracer that doesn't export.
//
// # Description
//
// Creates a tracer that generates valid IDs but doesn't send them anywhere.
// Useful for development, testing, or air-gapped environments.
//
// # Inputs
//
//   - serviceName: Service identifier for trace metadata
//
// # Outputs
//
//   - *NoOpDiagnosticsTracer: Ready-to-use tracer
//
// # Examples
//
//	tracer := NewNoOpDiagnosticsTracer("aleutian-cli")
//	ctx, finish := tracer.StartSpan(ctx, "operation", nil)
//	defer finish(nil)
//
// # Limitations
//
//   - Traces are not visible in Jaeger or other backends
//   - Spans are not linked across process boundaries
//
// # Assumptions
//
//   - Caller doesn't require distributed tracing visualization
func NewNoOpDiagnosticsTracer(serviceName string) *NoOpDiagnosticsTracer {
	if serviceName == "" {
		serviceName = "aleutian-cli"
	}
	return &NoOpDiagnosticsTracer{
		serviceName: serviceName,
	}
}

// StartSpan creates a no-op span that tracks IDs in context.
//
// # Description
//
// Creates a context-only span without exporting. The returned context
// carries trace/span IDs for logging and correlation.
//
// # Inputs
//
//   - ctx: Parent context
//   - name: Span name (logged but not exported)
//   - attrs: Attributes (ignored in no-op mode)
//
// # Outputs
//
//   - context.Context: Context with span metadata
//   - func(error): Finish function (does nothing in no-op mode)
//
// # Examples
//
//	ctx, finish := tracer.StartSpan(ctx, "collect", map[string]string{"reason": "test"})
//	defer finish(nil)
//
// # Limitations
//
//   - Attributes are ignored
//   - No span hierarchy is maintained
//
// # Assumptions
//
//   - Caller only needs IDs, not full tracing
func (t *NoOpDiagnosticsTracer) StartSpan(ctx context.Context, name string, attrs map[string]string) (context.Context, func(error)) {
	// Generate IDs and store in context
	traceID := t.GenerateTraceID()
	spanID := t.GenerateSpanID()

	// Store in context using a key type
	ctx = context.WithValue(ctx, noOpTraceIDKey{}, traceID)
	ctx = context.WithValue(ctx, noOpSpanIDKey{}, spanID)

	// Return no-op finish function
	return ctx, func(err error) {
		// No-op: nothing to export
	}
}

// GetTraceID extracts the trace ID from context.
//
// # Description
//
// Retrieves the trace ID stored by StartSpan.
//
// # Inputs
//
//   - ctx: Context from StartSpan
//
// # Outputs
//
//   - string: 32-character hex trace ID, or empty string
//
// # Examples
//
//	traceID := tracer.GetTraceID(ctx)
//
// # Limitations
//
//   - Returns empty if context wasn't from StartSpan
//
// # Assumptions
//
//   - Context was created by this tracer's StartSpan
func (t *NoOpDiagnosticsTracer) GetTraceID(ctx context.Context) string {
	if id, ok := ctx.Value(noOpTraceIDKey{}).(string); ok {
		return id
	}
	return ""
}

// GetSpanID extracts the span ID from context.
//
// # Description
//
// Retrieves the span ID stored by StartSpan.
//
// # Inputs
//
//   - ctx: Context from StartSpan
//
// # Outputs
//
//   - string: 16-character hex span ID, or empty string
//
// # Examples
//
//	spanID := tracer.GetSpanID(ctx)
//
// # Limitations
//
//   - Returns empty if context wasn't from StartSpan
//
// # Assumptions
//
//   - Context was created by this tracer's StartSpan
func (t *NoOpDiagnosticsTracer) GetSpanID(ctx context.Context) string {
	if id, ok := ctx.Value(noOpSpanIDKey{}).(string); ok {
		return id
	}
	return ""
}

// GenerateTraceID creates a random 32-character hex trace ID.
//
// # Description
//
// Generates a W3C Trace Context compatible trace ID (128 bits / 32 hex chars).
//
// # Outputs
//
//   - string: 32-character hex trace ID
//
// # Examples
//
//	traceID := tracer.GenerateTraceID()
//	// "a1b2c3d4e5f60718293a4b5c6d7e8f90"
//
// # Limitations
//
//   - Uses crypto/rand; falls back to timestamp on failure
//
// # Assumptions
//
//   - System entropy is available
func (t *NoOpDiagnosticsTracer) GenerateTraceID() string {
	t.mu.Lock()
	defer t.mu.Unlock()

	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		// Fallback to timestamp-based ID
		return fmt.Sprintf("%016x%016x", time.Now().UnixNano(), os.Getpid())
	}
	return hex.EncodeToString(bytes)
}

// GenerateSpanID creates a random 16-character hex span ID.
//
// # Description
//
// Generates a W3C Trace Context compatible span ID (64 bits / 16 hex chars).
//
// # Outputs
//
//   - string: 16-character hex span ID
//
// # Examples
//
//	spanID := tracer.GenerateSpanID()
//	// "a1b2c3d4e5f60718"
//
// # Limitations
//
//   - Uses crypto/rand; falls back to timestamp on failure
//
// # Assumptions
//
//   - System entropy is available
func (t *NoOpDiagnosticsTracer) GenerateSpanID() string {
	t.mu.Lock()
	defer t.mu.Unlock()

	bytes := make([]byte, 8)
	if _, err := rand.Read(bytes); err != nil {
		// Fallback to timestamp-based ID
		return fmt.Sprintf("%016x", time.Now().UnixNano())
	}
	return hex.EncodeToString(bytes)
}

// Shutdown is a no-op for the NoOpDiagnosticsTracer.
//
// # Description
//
// Does nothing since there are no resources to release.
//
// # Inputs
//
//   - ctx: Context (ignored)
//
// # Outputs
//
//   - error: Always nil
//
// # Examples
//
//	_ = tracer.Shutdown(ctx) // Always succeeds
//
// # Limitations
//
//   - None
//
// # Assumptions
//
//   - None
func (t *NoOpDiagnosticsTracer) Shutdown(ctx context.Context) error {
	return nil
}

// Context keys for no-op tracer.
type noOpTraceIDKey struct{}
type noOpSpanIDKey struct{}

// -----------------------------------------------------------------------------
// OTelDiagnosticsTracer Implementation (Enterprise Tier)
// -----------------------------------------------------------------------------

// OTelDiagnosticsTracer provides full OpenTelemetry tracing with export.
//
// # Description
//
// This is the Enterprise-tier tracer that exports spans to Jaeger/OTLP
// collectors for visualization and distributed tracing.
//
// # FOSS Alternative
//
// NoOpDiagnosticsTracer (FOSS) works offline without collectors.
//
// # Capabilities
//
//   - Full Jaeger UI visualization
//   - Span hierarchy and timing
//   - Attribute search and filtering
//   - Context propagation across services
//
// # Thread Safety
//
// OTelDiagnosticsTracer is safe for concurrent use.
type OTelDiagnosticsTracer struct {
	// tracer is the underlying OpenTelemetry tracer.
	tracer trace.Tracer

	// provider is the trace provider for shutdown.
	provider *sdktrace.TracerProvider

	// serviceName identifies this service.
	serviceName string
}

// OTelTracerConfig configures the OTelDiagnosticsTracer.
type OTelTracerConfig struct {
	// ServiceName is the service identifier in traces.
	// Default: "aleutian-cli"
	ServiceName string

	// Endpoint is the OTLP collector endpoint.
	// Default: "localhost:4317"
	Endpoint string

	// Insecure disables TLS for the connection.
	// Default: true (for local development)
	Insecure bool
}

// NewOTelDiagnosticsTracer creates an Enterprise-tier tracer with export.
//
// # Description
//
// Creates a tracer that exports spans to an OTLP collector (Jaeger, etc.).
// Requires a running collector at the configured endpoint.
//
// # Inputs
//
//   - ctx: Context for initialization
//   - config: Tracer configuration
//
// # Outputs
//
//   - *OTelDiagnosticsTracer: Ready-to-use tracer
//   - error: Non-nil if connection fails
//
// # Examples
//
//	tracer, err := NewOTelDiagnosticsTracer(ctx, OTelTracerConfig{
//	    ServiceName: "aleutian-cli",
//	    Endpoint:    "jaeger:4317",
//	})
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer tracer.Shutdown(context.Background())
//
// # Limitations
//
//   - Requires network access to collector
//   - Collector must be running and accessible
//
// # Assumptions
//
//   - OTLP collector is available at endpoint
//   - Network connectivity exists
func NewOTelDiagnosticsTracer(ctx context.Context, config OTelTracerConfig) (*OTelDiagnosticsTracer, error) {
	if config.ServiceName == "" {
		config.ServiceName = "aleutian-cli"
	}
	if config.Endpoint == "" {
		config.Endpoint = "localhost:4317"
	}

	// Create gRPC connection
	var dialOpts []grpc.DialOption
	if config.Insecure {
		dialOpts = append(dialOpts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}

	conn, err := grpc.NewClient(config.Endpoint, dialOpts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create gRPC connection: %w", err)
	}

	// Create OTLP exporter
	exporter, err := otlptracegrpc.New(ctx, otlptracegrpc.WithGRPCConn(conn))
	if err != nil {
		return nil, fmt.Errorf("failed to create OTLP exporter: %w", err)
	}

	// Create resource with service name
	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceNameKey.String(config.ServiceName),
			attribute.String("deployment.environment", getEnvironment()),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}

	// Create trace provider
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithResource(res),
		sdktrace.WithBatcher(exporter),
	)

	// Set global provider and propagator
	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return &OTelDiagnosticsTracer{
		tracer:      provider.Tracer(config.ServiceName),
		provider:    provider,
		serviceName: config.ServiceName,
	}, nil
}

// StartSpan creates an OpenTelemetry span with attributes.
//
// # Description
//
// Creates a span that will be exported to the configured collector.
// Supports span hierarchy when parent context has an active span.
//
// # Inputs
//
//   - ctx: Parent context (may contain parent span)
//   - name: Span name (e.g., "diagnostics.collect")
//   - attrs: Attributes to attach (key-value pairs)
//
// # Outputs
//
//   - context.Context: Context with new span
//   - func(error): Call to end span (pass error for failure, nil for success)
//
// # Examples
//
//	ctx, finish := tracer.StartSpan(ctx, "diagnostics.collect",
//	    map[string]string{
//	        "reason": "startup_failure",
//	        "severity": "error",
//	    })
//	defer finish(nil)
//
// # Limitations
//
//   - Requires active collector for export
//
// # Assumptions
//
//   - Collector is running and accepting connections
func (t *OTelDiagnosticsTracer) StartSpan(ctx context.Context, name string, attrs map[string]string) (context.Context, func(error)) {
	// Convert string map to attributes
	otelAttrs := make([]attribute.KeyValue, 0, len(attrs))
	for k, v := range attrs {
		otelAttrs = append(otelAttrs, attribute.String(k, v))
	}

	// Start span
	ctx, span := t.tracer.Start(ctx, name,
		trace.WithAttributes(otelAttrs...),
		trace.WithSpanKind(trace.SpanKindInternal),
	)

	// Return finish function
	finish := func(err error) {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		} else {
			span.SetStatus(codes.Ok, "")
		}
		span.End()
	}

	return ctx, finish
}

// GetTraceID extracts the trace ID from the span in context.
//
// # Description
//
// Returns the W3C trace ID from the active span.
//
// # Inputs
//
//   - ctx: Context with span
//
// # Outputs
//
//   - string: 32-character hex trace ID, or empty string
//
// # Examples
//
//	traceID := tracer.GetTraceID(ctx)
//
// # Limitations
//
//   - Returns empty if no span in context
//
// # Assumptions
//
//   - Context was created by StartSpan
func (t *OTelDiagnosticsTracer) GetTraceID(ctx context.Context) string {
	span := trace.SpanFromContext(ctx)
	if span == nil {
		return ""
	}
	traceID := span.SpanContext().TraceID()
	if !traceID.IsValid() {
		return ""
	}
	return traceID.String()
}

// GetSpanID extracts the span ID from the span in context.
//
// # Description
//
// Returns the W3C span ID from the active span.
//
// # Inputs
//
//   - ctx: Context with span
//
// # Outputs
//
//   - string: 16-character hex span ID, or empty string
//
// # Examples
//
//	spanID := tracer.GetSpanID(ctx)
//
// # Limitations
//
//   - Returns empty if no span in context
//
// # Assumptions
//
//   - Context was created by StartSpan
func (t *OTelDiagnosticsTracer) GetSpanID(ctx context.Context) string {
	span := trace.SpanFromContext(ctx)
	if span == nil {
		return ""
	}
	spanID := span.SpanContext().SpanID()
	if !spanID.IsValid() {
		return ""
	}
	return spanID.String()
}

// GenerateTraceID creates a random trace ID using OTel.
//
// # Description
//
// Generates a W3C-compatible trace ID using crypto/rand.
//
// # Outputs
//
//   - string: 32-character hex trace ID
//
// # Examples
//
//	traceID := tracer.GenerateTraceID()
//
// # Limitations
//
//   - Uses crypto/rand
//
// # Assumptions
//
//   - System has entropy
func (t *OTelDiagnosticsTracer) GenerateTraceID() string {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return fmt.Sprintf("%016x%016x", time.Now().UnixNano(), os.Getpid())
	}
	return hex.EncodeToString(bytes)
}

// GenerateSpanID creates a random span ID using OTel.
//
// # Description
//
// Generates a W3C-compatible span ID using crypto/rand.
//
// # Outputs
//
//   - string: 16-character hex span ID
//
// # Examples
//
//	spanID := tracer.GenerateSpanID()
//
// # Limitations
//
//   - Uses crypto/rand
//
// # Assumptions
//
//   - System has entropy
func (t *OTelDiagnosticsTracer) GenerateSpanID() string {
	bytes := make([]byte, 8)
	if _, err := rand.Read(bytes); err != nil {
		return fmt.Sprintf("%016x", time.Now().UnixNano())
	}
	return hex.EncodeToString(bytes)
}

// Shutdown flushes spans and releases resources.
//
// # Description
//
// Flushes any pending spans to the collector and closes connections.
// Should be called before application exit.
//
// # Inputs
//
//   - ctx: Context for timeout control
//
// # Outputs
//
//   - error: Non-nil if shutdown fails
//
// # Examples
//
//	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
//	defer cancel()
//	if err := tracer.Shutdown(ctx); err != nil {
//	    log.Printf("Shutdown failed: %v", err)
//	}
//
// # Limitations
//
//   - May timeout if collector is unreachable
//
// # Assumptions
//
//   - Application is shutting down
func (t *OTelDiagnosticsTracer) Shutdown(ctx context.Context) error {
	if t.provider != nil {
		return t.provider.Shutdown(ctx)
	}
	return nil
}

// -----------------------------------------------------------------------------
// Helper Functions
// -----------------------------------------------------------------------------

// getEnvironment returns the deployment environment.
//
// # Description
//
// Checks environment variables for deployment context.
//
// # Outputs
//
//   - string: "production", "staging", or "development"
//
// # Examples
//
//	env := getEnvironment()
//
// # Limitations
//
//   - Defaults to "development" if unset
//
// # Assumptions
//
//   - Environment variables follow standard naming
func getEnvironment() string {
	if env := os.Getenv("ALEUTIAN_ENV"); env != "" {
		return env
	}
	if env := os.Getenv("ENVIRONMENT"); env != "" {
		return env
	}
	return "development"
}

// NewDefaultDiagnosticsTracer creates the appropriate tracer based on environment.
//
// # Description
//
// Factory function that returns NoOpDiagnosticsTracer for FOSS tier
// or OTelDiagnosticsTracer if OTEL_EXPORTER_OTLP_ENDPOINT is set.
//
// # Inputs
//
//   - ctx: Context for initialization
//   - serviceName: Service identifier
//
// # Outputs
//
//   - DiagnosticsTracer: Appropriate tracer for the environment
//   - error: Non-nil if OTel initialization fails
//
// # Examples
//
//	tracer, err := NewDefaultDiagnosticsTracer(ctx, "aleutian-cli")
//	if err != nil {
//	    log.Printf("Using no-op tracer: %v", err)
//	    tracer = NewNoOpDiagnosticsTracer("aleutian-cli")
//	}
//
// # Limitations
//
//   - OTel requires collector endpoint
//
// # Assumptions
//
//   - OTEL_EXPORTER_OTLP_ENDPOINT indicates Enterprise mode
func NewDefaultDiagnosticsTracer(ctx context.Context, serviceName string) (DiagnosticsTracer, error) {
	endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	if endpoint == "" {
		// FOSS tier: no collector configured
		return NewNoOpDiagnosticsTracer(serviceName), nil
	}

	// Enterprise tier: collector is configured
	return NewOTelDiagnosticsTracer(ctx, OTelTracerConfig{
		ServiceName: serviceName,
		Endpoint:    endpoint,
		Insecure:    os.Getenv("OTEL_INSECURE") != "false",
	})
}

// Compile-time interface compliance checks.
var _ DiagnosticsTracer = (*NoOpDiagnosticsTracer)(nil)
var _ DiagnosticsTracer = (*OTelDiagnosticsTracer)(nil)
