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
Package main provides OpenTelemetry export for Health Intelligence.

This file implements the HealthOTelExporter interface, enabling health metrics
and traces to be exported to OTel-compatible backends (Jaeger, Prometheus, etc.).

# Design Rationale

HealthOTelExporter bridges the Health Intelligence system with OpenTelemetry:
  - Exports health analysis results as spans
  - Records health metrics for Prometheus scraping
  - Enables trace correlation between health checks and user requests

# Open Core Architecture

  - FOSS (NoOpHealthOTelExporter): Generates trace IDs for correlation, no export
  - Enterprise (DefaultHealthOTelExporter): Full OTel export with metrics

# Integration Points

  - DefaultHealthIntelligence.AnalyzeHealth creates spans for analysis
  - Health metrics exported to Prometheus/OTLP endpoint
  - Trace IDs included in health reports for correlation
*/
package main

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// =============================================================================
// INTERFACE DEFINITIONS
// =============================================================================

// HealthOTelExporter provides OpenTelemetry integration for Health Intelligence.
//
// # Description
//
// Abstracts the export of health metrics and traces to OTel-compatible backends.
// Enables the "Support Ticket Revolution" for health issues:
//   - User reports: "Health check failed, trace ID: abc123..."
//   - Support views the entire health analysis in Jaeger
//
// # Thread Safety
//
// All implementations must be safe for concurrent use.
//
// # Dependencies
//
//   - Uses DiagnosticsTracer for span creation
//   - Exports to configured OTel endpoint
//
// # Examples
//
//	exporter := NewDefaultHealthOTelExporter(tracer, config)
//	ctx, finish := exporter.StartHealthAnalysisSpan(ctx, "periodic_check")
//	defer finish(nil)
//	report, _ := intel.AnalyzeHealth(ctx, opts)
//	exporter.ExportHealthReport(ctx, report)
//
// # Limitations
//
//   - Requires OTel collector for export (Enterprise)
//   - NoOp mode generates IDs but doesn't export
//
// # Assumptions
//
//   - DiagnosticsTracer is properly initialized
//   - OTel collector accepts OTLP metrics (if configured)
type HealthOTelExporter interface {
	// StartHealthAnalysisSpan creates a span for health analysis operations.
	//
	// # Description
	//
	// Creates an OpenTelemetry span for tracing health analysis.
	// The span includes attributes for the analysis type and configuration.
	//
	// # Inputs
	//
	//   - ctx: Parent context (may contain existing trace)
	//   - analysisType: Type of analysis (e.g., "periodic", "on_demand", "startup")
	//
	// # Outputs
	//
	//   - context.Context: Context with span for propagation
	//   - func(error): Call to end span (pass nil for success, error for failure)
	//
	// # Examples
	//
	//	ctx, finish := exporter.StartHealthAnalysisSpan(ctx, "periodic")
	//	defer finish(nil)
	//
	// # Limitations
	//
	//   - NoOp implementation generates IDs but doesn't export
	//
	// # Assumptions
	//
	//   - DiagnosticsTracer is initialized
	StartHealthAnalysisSpan(ctx context.Context, analysisType string) (context.Context, func(error))

	// ExportHealthReport exports a health report as OTel metrics and traces.
	//
	// # Description
	//
	// Records health metrics (state, latency, error rates) and creates
	// child spans for each service analyzed. Enables visualization of
	// health analysis in Grafana and Jaeger.
	//
	// # Inputs
	//
	//   - ctx: Context with parent span
	//   - report: Health report to export
	//
	// # Outputs
	//
	//   - error: Non-nil if export fails
	//
	// # Examples
	//
	//	report, _ := intel.AnalyzeHealth(ctx, opts)
	//	if err := exporter.ExportHealthReport(ctx, report); err != nil {
	//	    log.Printf("Export failed: %v", err)
	//	}
	//
	// # Limitations
	//
	//   - Requires OTel collector for actual export
	//
	// # Assumptions
	//
	//   - Report contains valid data
	ExportHealthReport(ctx context.Context, report *IntelligentHealthReport) error

	// ExportServiceInsights exports detailed service insights.
	//
	// # Description
	//
	// Creates a child span with service-specific attributes including
	// latency percentiles, error rates, and detected anomalies.
	//
	// # Inputs
	//
	//   - ctx: Context with parent span
	//   - insights: Service insights to export
	//
	// # Outputs
	//
	//   - error: Non-nil if export fails
	//
	// # Examples
	//
	//	for _, svc := range report.Services {
	//	    exporter.ExportServiceInsights(ctx, &svc)
	//	}
	//
	// # Limitations
	//
	//   - High cardinality labels avoided to prevent metric explosion
	//
	// # Assumptions
	//
	//   - Context contains active span
	ExportServiceInsights(ctx context.Context, insights *ServiceInsights) error

	// ExportAlert exports a health alert as an OTel event.
	//
	// # Description
	//
	// Records an alert as a span event with severity and details.
	// Enables alerting integrations via OTel-compatible backends.
	//
	// # Inputs
	//
	//   - ctx: Context with parent span
	//   - alert: Alert to export
	//
	// # Outputs
	//
	//   - error: Non-nil if export fails
	//
	// # Examples
	//
	//	for _, alert := range report.Alerts {
	//	    exporter.ExportAlert(ctx, &alert)
	//	}
	//
	// # Limitations
	//
	//   - Events are recorded, not separate spans
	//
	// # Assumptions
	//
	//   - Context contains active span
	ExportAlert(ctx context.Context, alert *HealthAlert) error

	// GetTraceID returns the trace ID from the current context.
	//
	// # Description
	//
	// Extracts the W3C trace ID for inclusion in health reports.
	// Enables correlation between health analysis and user requests.
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
	//	traceID := exporter.GetTraceID(ctx)
	//	report.TraceID = traceID
	//
	// # Limitations
	//
	//   - Returns empty if no span in context
	//
	// # Assumptions
	//
	//   - Context was created by StartHealthAnalysisSpan
	GetTraceID(ctx context.Context) string
}

// =============================================================================
// STRUCT DEFINITIONS
// =============================================================================

// HealthOTelConfig configures the health OTel exporter.
//
// # Description
//
// Configuration for health-specific OTel export including metric names
// and export intervals.
//
// # Examples
//
//	config := DefaultHealthOTelConfig()
//	config.MetricPrefix = "myapp_health"
//
// # Limitations
//
//   - None
//
// # Assumptions
//
//   - DiagnosticsTracer is separately configured
type HealthOTelConfig struct {
	// ID is a unique identifier for this configuration.
	ID string

	// MetricPrefix is the prefix for health metrics.
	// Default: "aleutian_health"
	MetricPrefix string

	// IncludeServiceMetrics enables per-service metric export.
	// Default: true
	IncludeServiceMetrics bool

	// IncludeAlertEvents enables alert event export.
	// Default: true
	IncludeAlertEvents bool

	// CreatedAt is when this configuration was created.
	CreatedAt time.Time
}

// DefaultHealthOTelExporter implements HealthOTelExporter with OTel integration.
//
// # Description
//
// Enterprise-tier implementation that exports health metrics and traces
// using the existing DiagnosticsTracer infrastructure.
//
// # Thread Safety
//
// DefaultHealthOTelExporter is safe for concurrent use.
type DefaultHealthOTelExporter struct {
	tracer DiagnosticsTracer
	config HealthOTelConfig
	mu     sync.RWMutex

	// Metrics counters for rate limiting
	exportCount    int64
	lastExportTime time.Time
	errorsExported int64
	alertsExported int64
}

// NoOpHealthOTelExporter is the FOSS-tier exporter that generates IDs but doesn't export.
//
// # Description
//
// This implementation satisfies the HealthOTelExporter interface without
// requiring an OTel collector. It uses NoOpDiagnosticsTracer for ID generation.
//
// # Capabilities
//
//   - Generates valid trace IDs for correlation
//   - No network dependencies
//   - Zero configuration
//
// # Thread Safety
//
// NoOpHealthOTelExporter is safe for concurrent use.
type NoOpHealthOTelExporter struct {
	tracer DiagnosticsTracer
	config HealthOTelConfig
}

// MockHealthOTelExporter is a test double for HealthOTelExporter.
//
// # Description
//
// Allows tests to verify OTel export behavior without a real collector.
type MockHealthOTelExporter struct {
	StartSpanFunc      func(ctx context.Context, analysisType string) (context.Context, func(error))
	ExportReportFunc   func(ctx context.Context, report *IntelligentHealthReport) error
	ExportInsightsFunc func(ctx context.Context, insights *ServiceInsights) error
	ExportAlertFunc    func(ctx context.Context, alert *HealthAlert) error
	GetTraceIDFunc     func(ctx context.Context) string

	StartSpanCalls    []string
	ExportReportCalls []*IntelligentHealthReport
	ExportAlertCalls  []*HealthAlert
	mu                sync.Mutex
}

// =============================================================================
// CONSTRUCTOR FUNCTIONS
// =============================================================================

// DefaultHealthOTelConfig returns sensible defaults.
//
// # Description
//
// Returns configuration suitable for typical Aleutian deployments.
//
// # Outputs
//
//   - HealthOTelConfig: Default configuration
//
// # Examples
//
//	config := DefaultHealthOTelConfig()
//
// # Limitations
//
//   - None
//
// # Assumptions
//
//   - None
func DefaultHealthOTelConfig() HealthOTelConfig {
	return HealthOTelConfig{
		ID:                    GenerateID(),
		MetricPrefix:          "aleutian_health",
		IncludeServiceMetrics: true,
		IncludeAlertEvents:    true,
		CreatedAt:             time.Now(),
	}
}

// NewDefaultHealthOTelExporter creates an Enterprise-tier health OTel exporter.
//
// # Description
//
// Creates an exporter that uses the existing DiagnosticsTracer for spans
// and exports health metrics.
//
// # Inputs
//
//   - tracer: DiagnosticsTracer for span creation
//   - config: Export configuration
//
// # Outputs
//
//   - *DefaultHealthOTelExporter: Ready-to-use exporter
//
// # Examples
//
//	tracer, _ := NewDefaultDiagnosticsTracer(ctx, "aleutian-cli")
//	exporter := NewDefaultHealthOTelExporter(tracer, DefaultHealthOTelConfig())
//
// # Limitations
//
//   - Requires DiagnosticsTracer to be initialized
//
// # Assumptions
//
//   - DiagnosticsTracer handles actual OTel export
func NewDefaultHealthOTelExporter(tracer DiagnosticsTracer, config HealthOTelConfig) *DefaultHealthOTelExporter {
	return &DefaultHealthOTelExporter{
		tracer:         tracer,
		config:         config,
		lastExportTime: time.Now(),
	}
}

// NewNoOpHealthOTelExporter creates a FOSS-tier exporter.
//
// # Description
//
// Creates an exporter that generates valid IDs but doesn't export.
// Suitable for air-gapped environments or when no collector is available.
//
// # Inputs
//
//   - config: Export configuration (used for consistency)
//
// # Outputs
//
//   - *NoOpHealthOTelExporter: Ready-to-use exporter
//
// # Examples
//
//	exporter := NewNoOpHealthOTelExporter(DefaultHealthOTelConfig())
//
// # Limitations
//
//   - No actual export occurs
//
// # Assumptions
//
//   - Caller doesn't require distributed tracing
func NewNoOpHealthOTelExporter(config HealthOTelConfig) *NoOpHealthOTelExporter {
	return &NoOpHealthOTelExporter{
		tracer: NewNoOpDiagnosticsTracer("aleutian-health"),
		config: config,
	}
}

// =============================================================================
// DefaultHealthOTelExporter METHODS
// =============================================================================

// StartHealthAnalysisSpan creates a span for health analysis.
func (e *DefaultHealthOTelExporter) StartHealthAnalysisSpan(ctx context.Context, analysisType string) (context.Context, func(error)) {
	attrs := map[string]string{
		"health.analysis.type": analysisType,
		"health.exporter.id":   e.config.ID,
	}
	return e.tracer.StartSpan(ctx, "health.analysis", attrs)
}

// ExportHealthReport exports a health report as OTel metrics and events.
func (e *DefaultHealthOTelExporter) ExportHealthReport(ctx context.Context, report *IntelligentHealthReport) error {
	e.mu.Lock()
	e.exportCount++
	e.lastExportTime = time.Now()
	e.mu.Unlock()

	// Create child span for report export
	attrs := map[string]string{
		"health.state":          string(report.OverallState),
		"health.services.total": fmt.Sprintf("%d", len(report.Services)),
		"health.alerts.count":   fmt.Sprintf("%d", len(report.Alerts)),
		"health.duration.ms":    fmt.Sprintf("%d", report.Duration.Milliseconds()),
		"health.report.id":      report.ID,
	}

	_, finish := e.tracer.StartSpan(ctx, "health.export.report", attrs)
	defer finish(nil)

	// Export per-service insights
	if e.config.IncludeServiceMetrics {
		for i := range report.Services {
			if err := e.ExportServiceInsights(ctx, &report.Services[i]); err != nil {
				// Log but don't fail the entire export
				continue
			}
		}
	}

	// Export alerts
	if e.config.IncludeAlertEvents {
		for i := range report.Alerts {
			if err := e.ExportAlert(ctx, &report.Alerts[i]); err != nil {
				continue
			}
		}
	}

	return nil
}

// ExportServiceInsights exports service-specific health data.
func (e *DefaultHealthOTelExporter) ExportServiceInsights(ctx context.Context, insights *ServiceInsights) error {
	attrs := map[string]string{
		"service.name":           insights.Name,
		"service.state":          string(insights.IntelligentState),
		"service.latency.p99.ms": fmt.Sprintf("%d", insights.LatencyP99.Milliseconds()),
		"service.error.rate":     fmt.Sprintf("%.4f", insights.ErrorRate),
		"service.errors.recent":  fmt.Sprintf("%d", insights.RecentErrors),
		"service.memory.mb":      fmt.Sprintf("%d", insights.MemoryUsageMB),
		"service.cpu.percent":    fmt.Sprintf("%.2f", insights.CPUPercent),
		"service.latency.trend":  string(insights.LatencyTrend),
		"service.stale":          fmt.Sprintf("%t", insights.IsStale),
	}

	_, finish := e.tracer.StartSpan(ctx, "health.service.insights", attrs)
	finish(nil)

	return nil
}

// ExportAlert exports a health alert as an OTel span event.
func (e *DefaultHealthOTelExporter) ExportAlert(ctx context.Context, alert *HealthAlert) error {
	e.mu.Lock()
	e.alertsExported++
	e.mu.Unlock()

	attrs := map[string]string{
		"alert.id":          alert.ID,
		"alert.severity":    string(alert.Severity),
		"alert.service":     alert.Service,
		"alert.title":       alert.Title,
		"alert.description": alert.Description,
		"alert.metric":      alert.Metric,
		"alert.value":       alert.Value,
		"alert.threshold":   alert.Threshold,
	}

	_, finish := e.tracer.StartSpan(ctx, "health.alert", attrs)
	finish(nil)

	return nil
}

// GetTraceID returns the trace ID from the current context.
func (e *DefaultHealthOTelExporter) GetTraceID(ctx context.Context) string {
	return e.tracer.GetTraceID(ctx)
}

// =============================================================================
// NoOpHealthOTelExporter METHODS
// =============================================================================

// StartHealthAnalysisSpan creates a no-op span with trace ID.
func (e *NoOpHealthOTelExporter) StartHealthAnalysisSpan(ctx context.Context, analysisType string) (context.Context, func(error)) {
	return e.tracer.StartSpan(ctx, "health.analysis", map[string]string{
		"health.analysis.type": analysisType,
	})
}

// ExportHealthReport is a no-op that returns nil.
func (e *NoOpHealthOTelExporter) ExportHealthReport(ctx context.Context, report *IntelligentHealthReport) error {
	// No-op: nothing to export
	return nil
}

// ExportServiceInsights is a no-op that returns nil.
func (e *NoOpHealthOTelExporter) ExportServiceInsights(ctx context.Context, insights *ServiceInsights) error {
	return nil
}

// ExportAlert is a no-op that returns nil.
func (e *NoOpHealthOTelExporter) ExportAlert(ctx context.Context, alert *HealthAlert) error {
	return nil
}

// GetTraceID returns the trace ID from the no-op tracer.
func (e *NoOpHealthOTelExporter) GetTraceID(ctx context.Context) string {
	return e.tracer.GetTraceID(ctx)
}

// =============================================================================
// MockHealthOTelExporter METHODS
// =============================================================================

// StartHealthAnalysisSpan records the call and returns mock values.
func (m *MockHealthOTelExporter) StartHealthAnalysisSpan(ctx context.Context, analysisType string) (context.Context, func(error)) {
	m.mu.Lock()
	m.StartSpanCalls = append(m.StartSpanCalls, analysisType)
	m.mu.Unlock()

	if m.StartSpanFunc != nil {
		return m.StartSpanFunc(ctx, analysisType)
	}

	// Default: return context with mock trace ID
	ctx = context.WithValue(ctx, noOpTraceIDKey{}, "mock-trace-id-12345678")
	return ctx, func(err error) {}
}

// ExportHealthReport records the call and invokes custom function if set.
func (m *MockHealthOTelExporter) ExportHealthReport(ctx context.Context, report *IntelligentHealthReport) error {
	m.mu.Lock()
	m.ExportReportCalls = append(m.ExportReportCalls, report)
	m.mu.Unlock()

	if m.ExportReportFunc != nil {
		return m.ExportReportFunc(ctx, report)
	}
	return nil
}

// ExportServiceInsights invokes custom function if set.
func (m *MockHealthOTelExporter) ExportServiceInsights(ctx context.Context, insights *ServiceInsights) error {
	if m.ExportInsightsFunc != nil {
		return m.ExportInsightsFunc(ctx, insights)
	}
	return nil
}

// ExportAlert records the call and invokes custom function if set.
func (m *MockHealthOTelExporter) ExportAlert(ctx context.Context, alert *HealthAlert) error {
	m.mu.Lock()
	m.ExportAlertCalls = append(m.ExportAlertCalls, alert)
	m.mu.Unlock()

	if m.ExportAlertFunc != nil {
		return m.ExportAlertFunc(ctx, alert)
	}
	return nil
}

// GetTraceID returns mock trace ID or invokes custom function.
func (m *MockHealthOTelExporter) GetTraceID(ctx context.Context) string {
	if m.GetTraceIDFunc != nil {
		return m.GetTraceIDFunc(ctx)
	}
	if id, ok := ctx.Value(noOpTraceIDKey{}).(string); ok {
		return id
	}
	return "mock-trace-id"
}

// =============================================================================
// FACTORY FUNCTIONS
// =============================================================================

// NewHealthOTelExporter creates the appropriate exporter based on environment.
//
// # Description
//
// Factory function that returns NoOpHealthOTelExporter for FOSS tier
// or DefaultHealthOTelExporter if OTel is configured.
//
// # Inputs
//
//   - ctx: Context for tracer initialization
//   - config: Export configuration
//
// # Outputs
//
//   - HealthOTelExporter: Appropriate exporter for the environment
//   - error: Non-nil if OTel initialization fails
//
// # Examples
//
//	exporter, err := NewHealthOTelExporter(ctx, DefaultHealthOTelConfig())
//
// # Limitations
//
//   - Requires OTEL_EXPORTER_OTLP_ENDPOINT for Enterprise mode
//
// # Assumptions
//
//   - Environment variable indicates Enterprise mode
func NewHealthOTelExporter(ctx context.Context, config HealthOTelConfig) (HealthOTelExporter, error) {
	tracer, err := NewDefaultDiagnosticsTracer(ctx, "aleutian-health")
	if err != nil {
		return NewNoOpHealthOTelExporter(config), nil
	}

	// Check if we got a NoOp tracer (FOSS tier)
	if _, isNoOp := tracer.(*NoOpDiagnosticsTracer); isNoOp {
		return NewNoOpHealthOTelExporter(config), nil
	}

	return NewDefaultHealthOTelExporter(tracer, config), nil
}

// =============================================================================
// COMPILE-TIME INTERFACE CHECKS
// =============================================================================

var _ HealthOTelExporter = (*DefaultHealthOTelExporter)(nil)
var _ HealthOTelExporter = (*NoOpHealthOTelExporter)(nil)
var _ HealthOTelExporter = (*MockHealthOTelExporter)(nil)
