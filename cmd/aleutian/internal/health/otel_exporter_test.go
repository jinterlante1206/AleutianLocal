// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package health

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/AleutianAI/AleutianFOSS/cmd/aleutian/internal/diagnostics"
)

// =============================================================================
// DefaultHealthOTelConfig TESTS
// =============================================================================

func TestDefaultHealthOTelConfig(t *testing.T) {
	config := DefaultHealthOTelConfig()

	if config.ID == "" {
		t.Error("ID should be set")
	}
	if config.MetricPrefix != "aleutian_health" {
		t.Errorf("MetricPrefix = %q, want %q", config.MetricPrefix, "aleutian_health")
	}
	if !config.IncludeServiceMetrics {
		t.Error("IncludeServiceMetrics should be true by default")
	}
	if !config.IncludeAlertEvents {
		t.Error("IncludeAlertEvents should be true by default")
	}
	if config.CreatedAt.IsZero() {
		t.Error("CreatedAt should be set")
	}
}

// =============================================================================
// DefaultHealthOTelExporter TESTS
// =============================================================================

func TestNewDefaultHealthOTelExporter(t *testing.T) {
	tracer := diagnostics.NewNoOpDiagnosticsTracer("test")
	config := DefaultHealthOTelConfig()

	exporter := NewDefaultHealthOTelExporter(tracer, config)

	if exporter == nil {
		t.Fatal("NewDefaultHealthOTelExporter returned nil")
	}
	if exporter.tracer == nil {
		t.Error("tracer should be set")
	}
	if exporter.config.ID != config.ID {
		t.Error("config should be preserved")
	}
}

func TestDefaultHealthOTelExporter_StartHealthAnalysisSpan(t *testing.T) {
	tracer := diagnostics.NewNoOpDiagnosticsTracer("test")
	config := DefaultHealthOTelConfig()
	exporter := NewDefaultHealthOTelExporter(tracer, config)

	ctx := context.Background()
	ctx, finish := exporter.StartHealthAnalysisSpan(ctx, "periodic")
	defer finish(nil)

	traceID := exporter.GetTraceID(ctx)
	if traceID == "" {
		t.Error("Should have trace ID in context")
	}
	if len(traceID) != 32 {
		t.Errorf("Trace ID should be 32 chars, got %d", len(traceID))
	}
}

func TestDefaultHealthOTelExporter_ExportHealthReport(t *testing.T) {
	tracer := diagnostics.NewNoOpDiagnosticsTracer("test")
	config := DefaultHealthOTelConfig()
	exporter := NewDefaultHealthOTelExporter(tracer, config)

	ctx, finish := exporter.StartHealthAnalysisSpan(context.Background(), "test")
	defer finish(nil)

	report := &IntelligentHealthReport{
		ID:           GenerateID(),
		Timestamp:    time.Now(),
		OverallState: IntelligentStateHealthy,
		Services: []ServiceInsights{
			{
				ID:               GenerateID(),
				Name:             "TestService",
				IntelligentState: IntelligentStateHealthy,
				LatencyP99:       100 * time.Millisecond,
				ErrorRate:        0.01,
			},
		},
		Alerts: []HealthAlert{
			{
				ID:       GenerateID(),
				Severity: AlertSeverityWarning,
				Service:  "TestService",
				Title:    "Test Alert",
			},
		},
		Duration:  50 * time.Millisecond,
		CreatedAt: time.Now(),
	}

	err := exporter.ExportHealthReport(ctx, report)
	if err != nil {
		t.Errorf("ExportHealthReport failed: %v", err)
	}

	// Verify export count
	if exporter.exportCount != 1 {
		t.Errorf("exportCount = %d, want 1", exporter.exportCount)
	}
	if exporter.alertsExported != 1 {
		t.Errorf("alertsExported = %d, want 1", exporter.alertsExported)
	}
}

func TestDefaultHealthOTelExporter_ExportServiceInsights(t *testing.T) {
	tracer := diagnostics.NewNoOpDiagnosticsTracer("test")
	config := DefaultHealthOTelConfig()
	exporter := NewDefaultHealthOTelExporter(tracer, config)

	ctx, finish := exporter.StartHealthAnalysisSpan(context.Background(), "test")
	defer finish(nil)

	insights := &ServiceInsights{
		ID:               GenerateID(),
		Name:             "TestService",
		IntelligentState: IntelligentStateDegraded,
		LatencyP50:       50 * time.Millisecond,
		LatencyP99:       200 * time.Millisecond,
		LatencyTrend:     TrendIncreasing,
		ErrorRate:        0.05,
		RecentErrors:     5,
		MemoryUsageMB:    512,
		CPUPercent:       45.5,
		IsStale:          false,
	}

	err := exporter.ExportServiceInsights(ctx, insights)
	if err != nil {
		t.Errorf("ExportServiceInsights failed: %v", err)
	}
}

func TestDefaultHealthOTelExporter_ExportAlert(t *testing.T) {
	tracer := diagnostics.NewNoOpDiagnosticsTracer("test")
	config := DefaultHealthOTelConfig()
	exporter := NewDefaultHealthOTelExporter(tracer, config)

	ctx, finish := exporter.StartHealthAnalysisSpan(context.Background(), "test")
	defer finish(nil)

	alert := &HealthAlert{
		ID:          GenerateID(),
		Severity:    AlertSeverityCritical,
		Service:     "TestService",
		Title:       "Critical Alert",
		Description: "Service is failing",
		Metric:      "error_rate",
		Value:       "0.15",
		Threshold:   "0.05",
		DetectedAt:  time.Now(),
	}

	err := exporter.ExportAlert(ctx, alert)
	if err != nil {
		t.Errorf("ExportAlert failed: %v", err)
	}

	if exporter.alertsExported != 1 {
		t.Errorf("alertsExported = %d, want 1", exporter.alertsExported)
	}
}

func TestDefaultHealthOTelExporter_GetTraceID(t *testing.T) {
	tracer := diagnostics.NewNoOpDiagnosticsTracer("test")
	config := DefaultHealthOTelConfig()
	exporter := NewDefaultHealthOTelExporter(tracer, config)

	// Without span, should return empty
	emptyID := exporter.GetTraceID(context.Background())
	if emptyID != "" {
		t.Errorf("GetTraceID without span should return empty, got %q", emptyID)
	}

	// With span, should return trace ID
	ctx, finish := exporter.StartHealthAnalysisSpan(context.Background(), "test")
	defer finish(nil)

	traceID := exporter.GetTraceID(ctx)
	if traceID == "" {
		t.Error("GetTraceID with span should return trace ID")
	}
}

// =============================================================================
// NoOpHealthOTelExporter TESTS
// =============================================================================

func TestNewNoOpHealthOTelExporter(t *testing.T) {
	config := DefaultHealthOTelConfig()
	exporter := NewNoOpHealthOTelExporter(config)

	if exporter == nil {
		t.Fatal("NewNoOpHealthOTelExporter returned nil")
	}
	if exporter.tracer == nil {
		t.Error("tracer should be set")
	}
}

func TestNoOpHealthOTelExporter_StartHealthAnalysisSpan(t *testing.T) {
	config := DefaultHealthOTelConfig()
	exporter := NewNoOpHealthOTelExporter(config)

	ctx, finish := exporter.StartHealthAnalysisSpan(context.Background(), "test")
	defer finish(nil)

	// Should still have trace ID for correlation
	traceID := exporter.GetTraceID(ctx)
	if traceID == "" {
		t.Error("NoOp exporter should still generate trace IDs")
	}
}

func TestNoOpHealthOTelExporter_ExportHealthReport(t *testing.T) {
	config := DefaultHealthOTelConfig()
	exporter := NewNoOpHealthOTelExporter(config)

	ctx, finish := exporter.StartHealthAnalysisSpan(context.Background(), "test")
	defer finish(nil)

	report := &IntelligentHealthReport{
		ID:           GenerateID(),
		OverallState: IntelligentStateHealthy,
		CreatedAt:    time.Now(),
	}

	err := exporter.ExportHealthReport(ctx, report)
	if err != nil {
		t.Errorf("NoOp ExportHealthReport should return nil, got: %v", err)
	}
}

func TestNoOpHealthOTelExporter_ExportServiceInsights(t *testing.T) {
	config := DefaultHealthOTelConfig()
	exporter := NewNoOpHealthOTelExporter(config)

	insights := &ServiceInsights{
		ID:   GenerateID(),
		Name: "Test",
	}

	err := exporter.ExportServiceInsights(context.Background(), insights)
	if err != nil {
		t.Errorf("NoOp ExportServiceInsights should return nil, got: %v", err)
	}
}

func TestNoOpHealthOTelExporter_ExportAlert(t *testing.T) {
	config := DefaultHealthOTelConfig()
	exporter := NewNoOpHealthOTelExporter(config)

	alert := &HealthAlert{
		ID:       GenerateID(),
		Severity: AlertSeverityWarning,
	}

	err := exporter.ExportAlert(context.Background(), alert)
	if err != nil {
		t.Errorf("NoOp ExportAlert should return nil, got: %v", err)
	}
}

// =============================================================================
// MockHealthOTelExporter TESTS
// =============================================================================

func TestMockHealthOTelExporter_StartHealthAnalysisSpan(t *testing.T) {
	mock := &MockHealthOTelExporter{}

	ctx, finish := mock.StartHealthAnalysisSpan(context.Background(), "test_analysis")
	defer finish(nil)

	if len(mock.StartSpanCalls) != 1 {
		t.Errorf("StartSpanCalls = %d, want 1", len(mock.StartSpanCalls))
	}
	if mock.StartSpanCalls[0] != "test_analysis" {
		t.Errorf("StartSpanCalls[0] = %q, want %q", mock.StartSpanCalls[0], "test_analysis")
	}

	// Should have mock trace ID
	traceID := mock.GetTraceID(ctx)
	if traceID == "" {
		t.Error("Mock should return trace ID")
	}
}

func TestMockHealthOTelExporter_ExportHealthReport(t *testing.T) {
	mock := &MockHealthOTelExporter{}

	report := &IntelligentHealthReport{
		ID:           GenerateID(),
		OverallState: IntelligentStateDegraded,
		CreatedAt:    time.Now(),
	}

	err := mock.ExportHealthReport(context.Background(), report)
	if err != nil {
		t.Errorf("ExportHealthReport failed: %v", err)
	}

	if len(mock.ExportReportCalls) != 1 {
		t.Errorf("ExportReportCalls = %d, want 1", len(mock.ExportReportCalls))
	}
	if mock.ExportReportCalls[0].ID != report.ID {
		t.Error("Report should be recorded")
	}
}

func TestMockHealthOTelExporter_CustomFunctions(t *testing.T) {
	customTraceID := "custom-trace-12345678901234567890"
	customError := fmt.Errorf("custom error")

	mock := &MockHealthOTelExporter{
		GetTraceIDFunc: func(ctx context.Context) string {
			return customTraceID
		},
		ExportReportFunc: func(ctx context.Context, report *IntelligentHealthReport) error {
			return customError
		},
	}

	traceID := mock.GetTraceID(context.Background())
	if traceID != customTraceID {
		t.Errorf("GetTraceID = %q, want %q", traceID, customTraceID)
	}

	err := mock.ExportHealthReport(context.Background(), &IntelligentHealthReport{})
	if err != customError {
		t.Errorf("ExportHealthReport error = %v, want %v", err, customError)
	}
}

func TestMockHealthOTelExporter_ExportAlert(t *testing.T) {
	mock := &MockHealthOTelExporter{}

	alert1 := &HealthAlert{ID: "alert-1", Severity: AlertSeverityWarning}
	alert2 := &HealthAlert{ID: "alert-2", Severity: AlertSeverityCritical}

	mock.ExportAlert(context.Background(), alert1)
	mock.ExportAlert(context.Background(), alert2)

	if len(mock.ExportAlertCalls) != 2 {
		t.Errorf("ExportAlertCalls = %d, want 2", len(mock.ExportAlertCalls))
	}
	if mock.ExportAlertCalls[0].ID != "alert-1" {
		t.Error("First alert should be alert-1")
	}
	if mock.ExportAlertCalls[1].ID != "alert-2" {
		t.Error("Second alert should be alert-2")
	}
}

// =============================================================================
// FACTORY FUNCTION TESTS
// =============================================================================

func TestNewHealthOTelExporter_NoEnvVar(t *testing.T) {
	// Without OTEL_EXPORTER_OTLP_ENDPOINT, should return NoOp
	config := DefaultHealthOTelConfig()

	exporter, err := NewHealthOTelExporter(context.Background(), config)
	if err != nil {
		t.Fatalf("NewHealthOTelExporter failed: %v", err)
	}

	// Should be NoOp exporter (wrapped in the return type)
	// Test that it works like NoOp
	ctx, finish := exporter.StartHealthAnalysisSpan(context.Background(), "test")
	defer finish(nil)

	err = exporter.ExportHealthReport(ctx, &IntelligentHealthReport{
		ID:        GenerateID(),
		CreatedAt: time.Now(),
	})
	if err != nil {
		t.Errorf("NoOp exporter should succeed: %v", err)
	}
}

// =============================================================================
// INTERFACE COMPLIANCE TESTS
// =============================================================================

func TestHealthOTelExporter_InterfaceCompliance(t *testing.T) {
	// Compile-time checks are in the main file, but verify at runtime too
	var _ HealthOTelExporter = (*DefaultHealthOTelExporter)(nil)
	var _ HealthOTelExporter = (*NoOpHealthOTelExporter)(nil)
	var _ HealthOTelExporter = (*MockHealthOTelExporter)(nil)
}

// =============================================================================
// CONCURRENCY TESTS
// =============================================================================

func TestDefaultHealthOTelExporter_ConcurrentExports(t *testing.T) {
	tracer := diagnostics.NewNoOpDiagnosticsTracer("test")
	config := DefaultHealthOTelConfig()
	exporter := NewDefaultHealthOTelExporter(tracer, config)

	ctx, finish := exporter.StartHealthAnalysisSpan(context.Background(), "concurrent")
	defer finish(nil)

	// Run concurrent exports
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func(id int) {
			report := &IntelligentHealthReport{
				ID:           GenerateID(),
				OverallState: IntelligentStateHealthy,
				CreatedAt:    time.Now(),
			}
			exporter.ExportHealthReport(ctx, report)
			done <- true
		}(i)
	}

	// Wait for all
	for i := 0; i < 10; i++ {
		<-done
	}

	if exporter.exportCount != 10 {
		t.Errorf("exportCount = %d, want 10", exporter.exportCount)
	}
}

func TestMockHealthOTelExporter_ConcurrentRecording(t *testing.T) {
	mock := &MockHealthOTelExporter{}

	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func(id int) {
			mock.StartHealthAnalysisSpan(context.Background(), fmt.Sprintf("test-%d", id))
			done <- true
		}(i)
	}

	for i := 0; i < 10; i++ {
		<-done
	}

	if len(mock.StartSpanCalls) != 10 {
		t.Errorf("StartSpanCalls = %d, want 10", len(mock.StartSpanCalls))
	}
}
