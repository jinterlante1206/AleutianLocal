// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package diagnostics

import (
	"context"
	"errors"
	"sync"
	"testing"
)

// -----------------------------------------------------------------------------
// MockDiagnosticsCollector Tests
// -----------------------------------------------------------------------------

func TestMockDiagnosticsCollector_Collect_Default(t *testing.T) {
	mock := NewMockDiagnosticsCollector()
	ctx := context.Background()

	result, err := mock.Collect(ctx, CollectOptions{Reason: "test"})

	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if result == nil {
		t.Error("expected non-nil result")
	}
	if mock.CollectCallCount() != 1 {
		t.Errorf("expected call count 1, got %d", mock.CollectCallCount())
	}
}

func TestMockDiagnosticsCollector_Collect_CustomFunc(t *testing.T) {
	mock := NewMockDiagnosticsCollector()
	expectedTraceID := "custom-trace-123"

	mock.CollectFunc = func(ctx context.Context, opts CollectOptions) (*DiagnosticsResult, error) {
		return &DiagnosticsResult{TraceID: expectedTraceID}, nil
	}

	result, err := mock.Collect(context.Background(), CollectOptions{})

	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if result.TraceID != expectedTraceID {
		t.Errorf("expected trace ID %s, got %s", expectedTraceID, result.TraceID)
	}
}

func TestMockDiagnosticsCollector_Collect_ErrorFunc(t *testing.T) {
	mock := NewMockDiagnosticsCollector()
	expectedErr := errors.New("collection failed")

	mock.CollectFunc = func(ctx context.Context, opts CollectOptions) (*DiagnosticsResult, error) {
		return nil, expectedErr
	}

	result, err := mock.Collect(context.Background(), CollectOptions{})

	if err != expectedErr {
		t.Errorf("expected error %v, got %v", expectedErr, err)
	}
	if result != nil {
		t.Error("expected nil result on error")
	}
}

func TestMockDiagnosticsCollector_CollectInputAt(t *testing.T) {
	mock := NewMockDiagnosticsCollector()
	ctx := context.Background()

	mock.Collect(ctx, CollectOptions{Reason: "first"})
	mock.Collect(ctx, CollectOptions{Reason: "second"})

	opts, ok := mock.CollectInputAt(0)
	if !ok {
		t.Error("expected ok for index 0")
	}
	if opts.Reason != "first" {
		t.Errorf("expected reason 'first', got '%s'", opts.Reason)
	}

	opts, ok = mock.CollectInputAt(1)
	if !ok {
		t.Error("expected ok for index 1")
	}
	if opts.Reason != "second" {
		t.Errorf("expected reason 'second', got '%s'", opts.Reason)
	}

	_, ok = mock.CollectInputAt(2)
	if ok {
		t.Error("expected not ok for index 2")
	}
}

func TestMockDiagnosticsCollector_GetLastResult(t *testing.T) {
	mock := NewMockDiagnosticsCollector()

	// Default returns nil
	result := mock.GetLastResult()
	if result != nil {
		t.Error("expected nil default result")
	}
	if mock.GetLastResultCallCount() != 1 {
		t.Errorf("expected call count 1, got %d", mock.GetLastResultCallCount())
	}

	// Custom function
	mock.GetLastResultFunc = func() *DiagnosticsResult {
		return &DiagnosticsResult{TraceID: "custom"}
	}

	result = mock.GetLastResult()
	if result == nil || result.TraceID != "custom" {
		t.Error("expected custom result")
	}
}

func TestMockDiagnosticsCollector_SetStorage(t *testing.T) {
	mock := NewMockDiagnosticsCollector()
	storage := NewMockDiagnosticsStorage()

	var capturedStorage DiagnosticsStorage
	mock.SetStorageFunc = func(s DiagnosticsStorage) {
		capturedStorage = s
	}

	mock.SetStorage(storage)

	if mock.SetStorageCallCount() != 1 {
		t.Errorf("expected call count 1, got %d", mock.SetStorageCallCount())
	}
	if capturedStorage != storage {
		t.Error("expected storage to be captured")
	}
}

func TestMockDiagnosticsCollector_SetFormatter(t *testing.T) {
	mock := NewMockDiagnosticsCollector()
	formatter := NewMockDiagnosticsFormatter()

	mock.SetFormatter(formatter)

	if mock.SetFormatterCallCount() != 1 {
		t.Errorf("expected call count 1, got %d", mock.SetFormatterCallCount())
	}
}

func TestMockDiagnosticsCollector_Reset(t *testing.T) {
	mock := NewMockDiagnosticsCollector()
	ctx := context.Background()

	mock.Collect(ctx, CollectOptions{})
	mock.GetLastResult()
	mock.SetStorage(nil)
	mock.SetFormatter(nil)

	mock.Reset()

	if mock.CollectCallCount() != 0 {
		t.Error("expected collect count 0 after reset")
	}
	if mock.GetLastResultCallCount() != 0 {
		t.Error("expected get last result count 0 after reset")
	}
	if mock.SetStorageCallCount() != 0 {
		t.Error("expected set storage count 0 after reset")
	}
	if mock.SetFormatterCallCount() != 0 {
		t.Error("expected set formatter count 0 after reset")
	}
}

func TestMockDiagnosticsCollector_ThreadSafety(t *testing.T) {
	mock := NewMockDiagnosticsCollector()
	ctx := context.Background()
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			mock.Collect(ctx, CollectOptions{})
		}()
	}

	wg.Wait()

	if mock.CollectCallCount() != 100 {
		t.Errorf("expected 100 calls, got %d", mock.CollectCallCount())
	}
}

// -----------------------------------------------------------------------------
// MockDiagnosticsFormatter Tests
// -----------------------------------------------------------------------------

func TestMockDiagnosticsFormatter_Format_Default(t *testing.T) {
	mock := NewMockDiagnosticsFormatter()

	output, err := mock.Format(&DiagnosticsData{})

	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if len(output) != 0 {
		t.Errorf("expected empty output, got %d bytes", len(output))
	}
	if mock.FormatCallCount() != 1 {
		t.Errorf("expected call count 1, got %d", mock.FormatCallCount())
	}
}

func TestMockDiagnosticsFormatter_Format_CustomFunc(t *testing.T) {
	mock := NewMockDiagnosticsFormatter()
	expectedOutput := []byte(`{"test": true}`)

	mock.FormatFunc = func(data *DiagnosticsData) ([]byte, error) {
		return expectedOutput, nil
	}

	output, err := mock.Format(&DiagnosticsData{})

	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if string(output) != string(expectedOutput) {
		t.Errorf("expected output %s, got %s", expectedOutput, output)
	}
}

func TestMockDiagnosticsFormatter_ContentType(t *testing.T) {
	mock := NewMockDiagnosticsFormatter()

	// Default
	if mock.ContentType() != "text/plain" {
		t.Errorf("expected 'text/plain', got '%s'", mock.ContentType())
	}

	// Custom
	mock.ContentTypeFunc = func() string { return "application/json" }
	if mock.ContentType() != "application/json" {
		t.Errorf("expected 'application/json', got '%s'", mock.ContentType())
	}
}

func TestMockDiagnosticsFormatter_FileExtension(t *testing.T) {
	mock := NewMockDiagnosticsFormatter()

	// Default
	if mock.FileExtension() != ".txt" {
		t.Errorf("expected '.txt', got '%s'", mock.FileExtension())
	}

	// Custom
	mock.FileExtensionFunc = func() string { return ".json" }
	if mock.FileExtension() != ".json" {
		t.Errorf("expected '.json', got '%s'", mock.FileExtension())
	}
}

func TestMockDiagnosticsFormatter_FormatInputAt(t *testing.T) {
	mock := NewMockDiagnosticsFormatter()
	data1 := &DiagnosticsData{}
	data2 := &DiagnosticsData{}

	mock.Format(data1)
	mock.Format(data2)

	input, ok := mock.FormatInputAt(0)
	if !ok || input != data1 {
		t.Error("expected data1 at index 0")
	}

	input, ok = mock.FormatInputAt(1)
	if !ok || input != data2 {
		t.Error("expected data2 at index 1")
	}

	_, ok = mock.FormatInputAt(2)
	if ok {
		t.Error("expected not ok for index 2")
	}
}

func TestMockDiagnosticsFormatter_Reset(t *testing.T) {
	mock := NewMockDiagnosticsFormatter()
	mock.Format(&DiagnosticsData{})
	mock.Reset()

	if mock.FormatCallCount() != 0 {
		t.Error("expected 0 after reset")
	}
}

// -----------------------------------------------------------------------------
// MockDiagnosticsStorage Tests
// -----------------------------------------------------------------------------

func TestMockDiagnosticsStorage_Store_Default(t *testing.T) {
	mock := NewMockDiagnosticsStorage()
	ctx := context.Background()

	location, err := mock.Store(ctx, []byte("test"), StorageMetadata{FilenameHint: "test.json"})

	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if location != "test.json" {
		t.Errorf("expected 'test.json', got '%s'", location)
	}
	if mock.StoreCallCount() != 1 {
		t.Errorf("expected call count 1, got %d", mock.StoreCallCount())
	}
}

func TestMockDiagnosticsStorage_Store_Load(t *testing.T) {
	mock := NewMockDiagnosticsStorage()
	ctx := context.Background()
	testData := []byte("test data")

	location, _ := mock.Store(ctx, testData, StorageMetadata{FilenameHint: "test.json"})
	loaded, err := mock.Load(ctx, location)

	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if string(loaded) != string(testData) {
		t.Errorf("expected '%s', got '%s'", testData, loaded)
	}
	if mock.LoadCallCount() != 1 {
		t.Errorf("expected load call count 1, got %d", mock.LoadCallCount())
	}
}

func TestMockDiagnosticsStorage_List(t *testing.T) {
	mock := NewMockDiagnosticsStorage()
	ctx := context.Background()

	mock.Store(ctx, []byte("a"), StorageMetadata{FilenameHint: "a.json"})
	mock.Store(ctx, []byte("b"), StorageMetadata{FilenameHint: "b.json"})

	locations, err := mock.List(ctx, 0)

	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if len(locations) != 2 {
		t.Errorf("expected 2 locations, got %d", len(locations))
	}
	if mock.ListCallCount() != 1 {
		t.Errorf("expected list call count 1, got %d", mock.ListCallCount())
	}
}

func TestMockDiagnosticsStorage_List_WithLimit(t *testing.T) {
	mock := NewMockDiagnosticsStorage()
	ctx := context.Background()

	mock.Store(ctx, []byte("a"), StorageMetadata{FilenameHint: "a.json"})
	mock.Store(ctx, []byte("b"), StorageMetadata{FilenameHint: "b.json"})
	mock.Store(ctx, []byte("c"), StorageMetadata{FilenameHint: "c.json"})

	locations, _ := mock.List(ctx, 2)

	if len(locations) != 2 {
		t.Errorf("expected 2 locations with limit, got %d", len(locations))
	}
}

func TestMockDiagnosticsStorage_Prune(t *testing.T) {
	mock := NewMockDiagnosticsStorage()
	ctx := context.Background()

	// Default returns 0
	pruned, err := mock.Prune(ctx)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if pruned != 0 {
		t.Errorf("expected 0 pruned, got %d", pruned)
	}

	// Custom function
	mock.PruneFunc = func(ctx context.Context) (int, error) {
		return 5, nil
	}
	pruned, _ = mock.Prune(ctx)
	if pruned != 5 {
		t.Errorf("expected 5 pruned, got %d", pruned)
	}
	if mock.PruneCallCount() != 2 {
		t.Errorf("expected prune call count 2, got %d", mock.PruneCallCount())
	}
}

func TestMockDiagnosticsStorage_RetentionDays(t *testing.T) {
	mock := NewMockDiagnosticsStorage()

	// Default is 30
	if mock.GetRetentionDays() != 30 {
		t.Errorf("expected 30 days, got %d", mock.GetRetentionDays())
	}

	mock.SetRetentionDays(7)
	if mock.GetRetentionDays() != 7 {
		t.Errorf("expected 7 days, got %d", mock.GetRetentionDays())
	}
}

func TestMockDiagnosticsStorage_Type(t *testing.T) {
	mock := NewMockDiagnosticsStorage()

	// Default
	if mock.Type() != "mock" {
		t.Errorf("expected 'mock', got '%s'", mock.Type())
	}

	// Custom
	mock.TypeFunc = func() string { return "custom" }
	if mock.Type() != "custom" {
		t.Errorf("expected 'custom', got '%s'", mock.Type())
	}
}

func TestMockDiagnosticsStorage_Reset(t *testing.T) {
	mock := NewMockDiagnosticsStorage()
	ctx := context.Background()

	mock.Store(ctx, []byte("data"), StorageMetadata{FilenameHint: "test.json"})
	mock.Load(ctx, "test.json")
	mock.List(ctx, 10)
	mock.Prune(ctx)

	mock.Reset()

	if mock.StoreCallCount() != 0 {
		t.Error("expected store count 0 after reset")
	}
	if mock.LoadCallCount() != 0 {
		t.Error("expected load count 0 after reset")
	}

	// Stored data should be cleared
	locations, _ := mock.List(ctx, 10)
	if len(locations) != 0 {
		t.Error("expected no stored data after reset")
	}
}

// -----------------------------------------------------------------------------
// MockDiagnosticsMetrics Tests
// -----------------------------------------------------------------------------

func TestMockDiagnosticsMetrics_RecordCollection(t *testing.T) {
	mock := NewMockDiagnosticsMetrics()

	mock.RecordCollection(SeverityError, "test", 100, 1024)

	if mock.RecordCollectionCallCount() != 1 {
		t.Errorf("expected call count 1, got %d", mock.RecordCollectionCallCount())
	}
}

func TestMockDiagnosticsMetrics_RecordCollection_CustomFunc(t *testing.T) {
	mock := NewMockDiagnosticsMetrics()
	var capturedSeverity DiagnosticsSeverity
	var capturedReason string

	mock.RecordCollectionFunc = func(severity DiagnosticsSeverity, reason string, durationMs int64, sizeBytes int64) {
		capturedSeverity = severity
		capturedReason = reason
	}

	mock.RecordCollection(SeverityWarning, "custom", 100, 1024)

	if capturedSeverity != SeverityWarning {
		t.Errorf("expected SeverityWarning, got %v", capturedSeverity)
	}
	if capturedReason != "custom" {
		t.Errorf("expected 'custom', got '%s'", capturedReason)
	}
}

func TestMockDiagnosticsMetrics_RecordError(t *testing.T) {
	mock := NewMockDiagnosticsMetrics()

	mock.RecordError("storage_failure")

	if mock.RecordErrorCallCount() != 1 {
		t.Errorf("expected call count 1, got %d", mock.RecordErrorCallCount())
	}
}

func TestMockDiagnosticsMetrics_RecordContainerHealth(t *testing.T) {
	mock := NewMockDiagnosticsMetrics()
	var capturedContainer, capturedService, capturedStatus string

	mock.RecordContainerHealthFunc = func(containerName, serviceType, status string) {
		capturedContainer = containerName
		capturedService = serviceType
		capturedStatus = status
	}

	mock.RecordContainerHealth("weaviate", "vectordb", "healthy")

	if capturedContainer != "weaviate" {
		t.Errorf("expected 'weaviate', got '%s'", capturedContainer)
	}
	if capturedService != "vectordb" {
		t.Errorf("expected 'vectordb', got '%s'", capturedService)
	}
	if capturedStatus != "healthy" {
		t.Errorf("expected 'healthy', got '%s'", capturedStatus)
	}
}

func TestMockDiagnosticsMetrics_RecordContainerMetrics(t *testing.T) {
	mock := NewMockDiagnosticsMetrics()
	var capturedCPU float64
	var capturedMemory int64

	mock.RecordContainerMetricsFunc = func(containerName string, cpuPercent float64, memoryMB int64) {
		capturedCPU = cpuPercent
		capturedMemory = memoryMB
	}

	mock.RecordContainerMetrics("rag", 45.5, 2048)

	if capturedCPU != 45.5 {
		t.Errorf("expected 45.5, got %f", capturedCPU)
	}
	if capturedMemory != 2048 {
		t.Errorf("expected 2048, got %d", capturedMemory)
	}
}

func TestMockDiagnosticsMetrics_RecordPruned(t *testing.T) {
	mock := NewMockDiagnosticsMetrics()
	var capturedCount int

	mock.RecordPrunedFunc = func(count int) {
		capturedCount = count
	}

	mock.RecordPruned(10)

	if capturedCount != 10 {
		t.Errorf("expected 10, got %d", capturedCount)
	}
}

func TestMockDiagnosticsMetrics_RecordStoredCount(t *testing.T) {
	mock := NewMockDiagnosticsMetrics()
	var capturedCount int

	mock.RecordStoredCountFunc = func(count int) {
		capturedCount = count
	}

	mock.RecordStoredCount(42)

	if capturedCount != 42 {
		t.Errorf("expected 42, got %d", capturedCount)
	}
}

func TestMockDiagnosticsMetrics_Register(t *testing.T) {
	mock := NewMockDiagnosticsMetrics()

	err := mock.Register()

	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	// Custom error
	mock.RegisterFunc = func() error {
		return errors.New("registration failed")
	}
	err = mock.Register()
	if err == nil {
		t.Error("expected error")
	}
}

func TestMockDiagnosticsMetrics_Reset(t *testing.T) {
	mock := NewMockDiagnosticsMetrics()

	mock.RecordCollection(SeverityInfo, "test", 100, 1024)
	mock.RecordError("test")
	mock.Reset()

	if mock.RecordCollectionCallCount() != 0 {
		t.Error("expected 0 after reset")
	}
	if mock.RecordErrorCallCount() != 0 {
		t.Error("expected 0 after reset")
	}
}

// -----------------------------------------------------------------------------
// MockDiagnosticsViewer Tests
// -----------------------------------------------------------------------------

func TestMockDiagnosticsViewer_Get_Default(t *testing.T) {
	mock := NewMockDiagnosticsViewer()
	ctx := context.Background()

	data, err := mock.Get(ctx, "test-id")

	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if data != nil {
		t.Error("expected nil data by default")
	}
	if mock.GetCallCount() != 1 {
		t.Errorf("expected call count 1, got %d", mock.GetCallCount())
	}
}

func TestMockDiagnosticsViewer_Get_CustomFunc(t *testing.T) {
	mock := NewMockDiagnosticsViewer()
	ctx := context.Background()
	expectedData := &DiagnosticsData{}

	mock.GetFunc = func(ctx context.Context, id string) (*DiagnosticsData, error) {
		if id == "found" {
			return expectedData, nil
		}
		return nil, errors.New("not found")
	}

	data, err := mock.Get(ctx, "found")
	if err != nil || data != expectedData {
		t.Error("expected data for 'found'")
	}

	_, err = mock.Get(ctx, "missing")
	if err == nil {
		t.Error("expected error for 'missing'")
	}
}

func TestMockDiagnosticsViewer_List(t *testing.T) {
	mock := NewMockDiagnosticsViewer()
	ctx := context.Background()

	// Default returns empty slice
	summaries, err := mock.List(ctx, ListOptions{})
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if len(summaries) != 0 {
		t.Errorf("expected empty slice, got %d items", len(summaries))
	}

	// Custom function
	mock.ListFunc = func(ctx context.Context, opts ListOptions) ([]DiagnosticsSummary, error) {
		return []DiagnosticsSummary{{Reason: "test"}}, nil
	}
	summaries, _ = mock.List(ctx, ListOptions{})
	if len(summaries) != 1 {
		t.Errorf("expected 1 summary, got %d", len(summaries))
	}
	if mock.ListCallCount() != 2 {
		t.Errorf("expected call count 2, got %d", mock.ListCallCount())
	}
}

func TestMockDiagnosticsViewer_GetByTraceID(t *testing.T) {
	mock := NewMockDiagnosticsViewer()
	ctx := context.Background()

	// Default returns nil
	data, err := mock.GetByTraceID(ctx, "trace-123")
	if err != nil || data != nil {
		t.Error("expected nil, nil by default")
	}

	// Custom function
	mock.GetByTraceIDFunc = func(ctx context.Context, traceID string) (*DiagnosticsData, error) {
		return &DiagnosticsData{}, nil
	}
	data, _ = mock.GetByTraceID(ctx, "trace-123")
	if data == nil {
		t.Error("expected non-nil data")
	}
	if mock.GetByTraceIDCallCount() != 2 {
		t.Errorf("expected call count 2, got %d", mock.GetByTraceIDCallCount())
	}
}

func TestMockDiagnosticsViewer_Reset(t *testing.T) {
	mock := NewMockDiagnosticsViewer()
	ctx := context.Background()

	mock.Get(ctx, "id")
	mock.List(ctx, ListOptions{})
	mock.GetByTraceID(ctx, "trace")
	mock.Reset()

	if mock.GetCallCount() != 0 {
		t.Error("expected get count 0 after reset")
	}
	if mock.ListCallCount() != 0 {
		t.Error("expected list count 0 after reset")
	}
	if mock.GetByTraceIDCallCount() != 0 {
		t.Error("expected get by trace id count 0 after reset")
	}
}

// -----------------------------------------------------------------------------
// MockDiagnosticsTracer Tests
// -----------------------------------------------------------------------------

func TestMockDiagnosticsTracer_StartSpan_Default(t *testing.T) {
	mock := NewMockDiagnosticsTracer()
	ctx := context.Background()

	ctx, finish := mock.StartSpan(ctx, "test-span", map[string]string{"key": "value"})
	finish(nil)

	if mock.StartSpanCallCount() != 1 {
		t.Errorf("expected call count 1, got %d", mock.StartSpanCallCount())
	}

	name, ok := mock.SpanNameAt(0)
	if !ok || name != "test-span" {
		t.Errorf("expected 'test-span', got '%s'", name)
	}

	// Context should have trace/span IDs
	traceID := mock.GetTraceID(ctx)
	if traceID == "" {
		t.Error("expected non-empty trace ID")
	}

	spanID := mock.GetSpanID(ctx)
	if spanID == "" {
		t.Error("expected non-empty span ID")
	}
}

func TestMockDiagnosticsTracer_GenerateTraceID_Sequential(t *testing.T) {
	mock := NewMockDiagnosticsTracer()

	id1 := mock.GenerateTraceID()
	id2 := mock.GenerateTraceID()

	if len(id1) != 32 {
		t.Errorf("expected 32-char trace ID, got %d chars", len(id1))
	}
	if id1 == id2 {
		t.Error("expected sequential IDs to be different")
	}
}

func TestMockDiagnosticsTracer_GenerateSpanID_Sequential(t *testing.T) {
	mock := NewMockDiagnosticsTracer()

	id1 := mock.GenerateSpanID()
	id2 := mock.GenerateSpanID()

	if len(id1) != 16 {
		t.Errorf("expected 16-char span ID, got %d chars", len(id1))
	}
	if id1 == id2 {
		t.Error("expected sequential IDs to be different")
	}
}

func TestMockDiagnosticsTracer_SetTraceID(t *testing.T) {
	mock := NewMockDiagnosticsTracer()
	customID := "00000000000000000000customtrace1"

	mock.SetTraceID(customID)
	ctx, _ := mock.StartSpan(context.Background(), "span", nil)

	traceID := mock.GetTraceID(ctx)
	if traceID != customID {
		t.Errorf("expected '%s', got '%s'", customID, traceID)
	}
}

func TestMockDiagnosticsTracer_SetSpanID(t *testing.T) {
	mock := NewMockDiagnosticsTracer()
	customID := "customspan123456"

	mock.SetSpanID(customID)
	ctx, _ := mock.StartSpan(context.Background(), "span", nil)

	spanID := mock.GetSpanID(ctx)
	if spanID != customID {
		t.Errorf("expected '%s', got '%s'", customID, spanID)
	}
}

func TestMockDiagnosticsTracer_Shutdown(t *testing.T) {
	mock := NewMockDiagnosticsTracer()
	ctx := context.Background()

	err := mock.Shutdown(ctx)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	// Custom error
	mock.ShutdownFunc = func(ctx context.Context) error {
		return errors.New("shutdown failed")
	}
	err = mock.Shutdown(ctx)
	if err == nil {
		t.Error("expected error")
	}
}

func TestMockDiagnosticsTracer_Reset(t *testing.T) {
	mock := NewMockDiagnosticsTracer()
	ctx := context.Background()

	// Change the trace ID from default
	mock.SetTraceID("changed-trace-id-00000000000000")
	mock.StartSpan(ctx, "span1", nil)
	mock.StartSpan(ctx, "span2", nil)
	mock.GenerateTraceID()
	mock.Reset()

	if mock.StartSpanCallCount() != 0 {
		t.Error("expected 0 after reset")
	}

	// IDs should reset to initial values
	ctx, _ = mock.StartSpan(ctx, "span", nil)
	traceID := mock.GetTraceID(ctx)
	if traceID != "00000000000000000000000000000001" {
		t.Errorf("expected reset trace ID, got '%s'", traceID)
	}
}

// -----------------------------------------------------------------------------
// MockPanicRecoveryHandler Tests
// -----------------------------------------------------------------------------

func TestMockPanicRecoveryHandler_Wrap_Default(t *testing.T) {
	mock := NewMockPanicRecoveryHandler()

	wrapper := mock.Wrap()
	wrapper() // Should be a no-op

	if mock.WrapCallCount() != 1 {
		t.Errorf("expected call count 1, got %d", mock.WrapCallCount())
	}
}

func TestMockPanicRecoveryHandler_Wrap_CustomFunc(t *testing.T) {
	mock := NewMockPanicRecoveryHandler()
	called := false

	mock.WrapFunc = func() func() {
		return func() {
			called = true
		}
	}

	wrapper := mock.Wrap()
	wrapper()

	if !called {
		t.Error("expected custom wrap function to be called")
	}
}

func TestMockPanicRecoveryHandler_SetCollector(t *testing.T) {
	mock := NewMockPanicRecoveryHandler()
	collector := NewMockDiagnosticsCollector()
	var capturedCollector DiagnosticsCollector

	mock.SetCollectorFunc = func(c DiagnosticsCollector) {
		capturedCollector = c
	}

	mock.SetCollector(collector)

	if mock.SetCollectorCallCount() != 1 {
		t.Errorf("expected call count 1, got %d", mock.SetCollectorCallCount())
	}
	if capturedCollector != collector {
		t.Error("expected collector to be captured")
	}
}

func TestMockPanicRecoveryHandler_GetLastPanicResult_Default(t *testing.T) {
	mock := NewMockPanicRecoveryHandler()

	result := mock.GetLastPanicResult()
	if result != nil {
		t.Error("expected nil by default")
	}
	if mock.GetLastPanicResultCallCount() != 1 {
		t.Errorf("expected call count 1, got %d", mock.GetLastPanicResultCallCount())
	}
}

func TestMockPanicRecoveryHandler_SetLastPanicResult(t *testing.T) {
	mock := NewMockPanicRecoveryHandler()
	expected := &DiagnosticsResult{TraceID: "panic-trace"}

	mock.SetLastPanicResult(expected)
	result := mock.GetLastPanicResult()

	if result != expected {
		t.Error("expected configured result")
	}
}

func TestMockPanicRecoveryHandler_GetLastPanicResult_CustomFunc(t *testing.T) {
	mock := NewMockPanicRecoveryHandler()
	expected := &DiagnosticsResult{TraceID: "func-trace"}

	mock.GetLastPanicResultFunc = func() *DiagnosticsResult {
		return expected
	}

	// Even if SetLastPanicResult was called, the func takes precedence
	mock.SetLastPanicResult(&DiagnosticsResult{TraceID: "other"})
	result := mock.GetLastPanicResult()

	if result != expected {
		t.Error("expected func result to take precedence")
	}
}

func TestMockPanicRecoveryHandler_Reset(t *testing.T) {
	mock := NewMockPanicRecoveryHandler()

	mock.Wrap()
	mock.SetCollector(nil)
	mock.SetLastPanicResult(&DiagnosticsResult{})
	mock.GetLastPanicResult()
	mock.Reset()

	if mock.WrapCallCount() != 0 {
		t.Error("expected wrap count 0 after reset")
	}
	if mock.SetCollectorCallCount() != 0 {
		t.Error("expected set collector count 0 after reset")
	}
	if mock.GetLastPanicResultCallCount() != 0 {
		t.Error("expected get last panic result count 0 after reset")
	}
	if mock.GetLastPanicResult() != nil {
		t.Error("expected nil result after reset")
	}
}

// -----------------------------------------------------------------------------
// Helper Function Tests
// -----------------------------------------------------------------------------

func TestFormatInt64Hex(t *testing.T) {
	tests := []struct {
		input    int64
		expected string
	}{
		{0, "0"},
		{1, "1"},
		{15, "f"},
		{16, "10"},
		{255, "ff"},
		{256, "100"},
	}

	for _, tc := range tests {
		result := formatInt64Hex(tc.input)
		if result != tc.expected {
			t.Errorf("formatInt64Hex(%d) = %s, expected %s", tc.input, result, tc.expected)
		}
	}
}

func TestTruncateOrPad(t *testing.T) {
	tests := []struct {
		input    string
		length   int
		expected string
	}{
		{"f", 4, "000f"},
		{"ff", 4, "00ff"},
		{"ffff", 4, "ffff"},
		{"abcde", 4, "bcde"}, // Truncate from left
		{"", 4, "0000"},
	}

	for _, tc := range tests {
		result := truncateOrPad(tc.input, tc.length)
		if result != tc.expected {
			t.Errorf("truncateOrPad(%s, %d) = %s, expected %s", tc.input, tc.length, result, tc.expected)
		}
	}
}

func TestFormatMockTraceID(t *testing.T) {
	result := formatMockTraceID(1)
	if len(result) != 32 {
		t.Errorf("expected 32 chars, got %d", len(result))
	}
	if result != "00000000000000000000000000000001" {
		t.Errorf("expected 32 zeros + 1, got %s", result)
	}
}

func TestFormatMockSpanID(t *testing.T) {
	result := formatMockSpanID(1)
	if len(result) != 16 {
		t.Errorf("expected 16 chars, got %d", len(result))
	}
	if result != "0000000000000001" {
		t.Errorf("expected 16 zeros + 1, got %s", result)
	}
}

// -----------------------------------------------------------------------------
// Interface Compliance Tests
// -----------------------------------------------------------------------------

func TestMockDiagnosticsCollector_InterfaceCompliance(t *testing.T) {
	var _ DiagnosticsCollector = (*MockDiagnosticsCollector)(nil)
}

func TestMockDiagnosticsFormatter_InterfaceCompliance(t *testing.T) {
	var _ DiagnosticsFormatter = (*MockDiagnosticsFormatter)(nil)
}

func TestMockDiagnosticsStorage_InterfaceCompliance(t *testing.T) {
	var _ DiagnosticsStorage = (*MockDiagnosticsStorage)(nil)
}

func TestMockDiagnosticsMetrics_InterfaceCompliance(t *testing.T) {
	var _ DiagnosticsMetrics = (*MockDiagnosticsMetrics)(nil)
}

func TestMockDiagnosticsViewer_InterfaceCompliance(t *testing.T) {
	var _ DiagnosticsViewer = (*MockDiagnosticsViewer)(nil)
}

func TestMockDiagnosticsTracer_InterfaceCompliance(t *testing.T) {
	var _ DiagnosticsTracer = (*MockDiagnosticsTracer)(nil)
}

func TestMockPanicRecoveryHandler_InterfaceCompliance(t *testing.T) {
	var _ PanicRecoveryHandler = (*MockPanicRecoveryHandler)(nil)
}
