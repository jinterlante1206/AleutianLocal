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
Package diagnostics_test contains tests for DefaultDiagnosticsCollector.

# Testing Strategy

These tests verify:
  - Collector construction with dependencies
  - Successful collection with mocked podman
  - Graceful degradation when podman unavailable
  - Container log collection
  - Storage and formatter integration
  - Thread safety under concurrent access
  - All helper function behavior
*/
package diagnostics

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/jinterlante1206/AleutianLocal/cmd/aleutian/internal/infra/process"
)

// -----------------------------------------------------------------------------
// Test Storage Helper - uses MockDiagnosticsStorage from mocks.go
// -----------------------------------------------------------------------------

// testDiagnosticsStorage wraps MockDiagnosticsStorage with test-specific helpers.
//
// # Description
//
// Provides a simplified interface for tests that need to capture stored data
// and configure specific return values. Uses the shared MockDiagnosticsStorage.
//
// # Thread Safety
//
// testDiagnosticsStorage is safe for concurrent use.
type testDiagnosticsStorage struct {
	*MockDiagnosticsStorage
	storedData []byte
	returnPath string
	storeError error
	mu         sync.Mutex
}

// newTestStorage creates a test storage wrapper with defaults.
//
// # Description
//
// Creates a storage that captures data and returns "/mock/diagnostics/test.json".
//
// # Outputs
//
//   - *testDiagnosticsStorage: Ready-to-use test storage
//
// # Examples
//
//	storage := newTestStorage()
//	storage.Store(ctx, data, meta)
//	stored := storage.GetStoredData()
//
// # Limitations
//
//   - None
//
// # Assumptions
//
//   - None
func newTestStorage() *testDiagnosticsStorage {
	ts := &testDiagnosticsStorage{
		MockDiagnosticsStorage: NewMockDiagnosticsStorage(),
		returnPath:             "/mock/diagnostics/test.json",
	}

	// Configure the mock to capture data and return the configured path
	ts.StoreFunc = func(ctx context.Context, data []byte, meta StorageMetadata) (string, error) {
		ts.mu.Lock()
		defer ts.mu.Unlock()
		if ts.storeError != nil {
			return "", ts.storeError
		}
		ts.storedData = data
		return ts.returnPath, nil
	}

	return ts
}

// GetStoredData returns what was stored.
//
// # Description
//
// Returns the data passed to the last Store call.
//
// # Outputs
//
//   - []byte: Last stored data
//
// # Examples
//
//	storage.Store(ctx, []byte("test"), meta)
//	data := storage.GetStoredData()
//
// # Limitations
//
//   - Only returns last store, not history
//
// # Assumptions
//
//   - Store was called
func (ts *testDiagnosticsStorage) GetStoredData() []byte {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return ts.storedData
}

// SetReturnPath configures the path returned by Store.
//
// # Description
//
// Sets the location string returned by Store.
//
// # Inputs
//
//   - path: Path to return
//
// # Examples
//
//	storage.SetReturnPath("/custom/path.json")
//
// # Limitations
//
//   - None
//
// # Assumptions
//
//   - None
func (ts *testDiagnosticsStorage) SetReturnPath(path string) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.returnPath = path
}

// SetStoreError configures Store to return an error.
//
// # Description
//
// Sets an error that Store will return.
//
// # Inputs
//
//   - err: Error to return
//
// # Examples
//
//	storage.SetStoreError(errors.New("disk full"))
//
// # Limitations
//
//   - None
//
// # Assumptions
//
//   - None
func (ts *testDiagnosticsStorage) SetStoreError(err error) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.storeError = err
}

// GetStoreCount returns how many times Store was called.
//
// # Description
//
// Returns the call count from the underlying mock.
//
// # Outputs
//
//   - int: Number of Store calls
//
// # Examples
//
//	storage.Store(ctx, data, meta)
//	count := storage.GetStoreCount()
//
// # Limitations
//
//   - None
//
// # Assumptions
//
//   - None
func (ts *testDiagnosticsStorage) GetStoreCount() int {
	return ts.StoreCallCount()
}

// -----------------------------------------------------------------------------
// Test Metrics Helper - uses MockDiagnosticsMetrics from mocks.go
// -----------------------------------------------------------------------------

// testDiagnosticsMetrics wraps MockDiagnosticsMetrics with test-specific helpers.
//
// # Description
//
// Provides a simplified interface for tests that need to track metrics calls.
//
// # Thread Safety
//
// testDiagnosticsMetrics is safe for concurrent use.
type testDiagnosticsMetrics struct {
	*MockDiagnosticsMetrics
}

// newTestMetrics creates a test metrics wrapper.
//
// # Description
//
// Creates a metrics recorder that tracks calls.
//
// # Outputs
//
//   - *testDiagnosticsMetrics: Ready-to-use test metrics
//
// # Examples
//
//	metrics := newTestMetrics()
//	metrics.RecordCollection(SeverityInfo, "test", 100, 1024)
//	count := metrics.GetCollectionCount()
//
// # Limitations
//
//   - None
//
// # Assumptions
//
//   - None
func newTestMetrics() *testDiagnosticsMetrics {
	return &testDiagnosticsMetrics{
		MockDiagnosticsMetrics: NewMockDiagnosticsMetrics(),
	}
}

// GetCollectionCount returns the collection count.
//
// # Description
//
// Returns the number of RecordCollection calls.
//
// # Outputs
//
//   - int: Number of RecordCollection calls
//
// # Examples
//
//	metrics.RecordCollection(SeverityInfo, "test", 100, 1024)
//	count := metrics.GetCollectionCount()
//
// # Limitations
//
//   - None
//
// # Assumptions
//
//   - None
func (tm *testDiagnosticsMetrics) GetCollectionCount() int {
	return tm.RecordCollectionCallCount()
}

// -----------------------------------------------------------------------------
// Mock Process Manager Helper
// -----------------------------------------------------------------------------

// newTestProcessManager creates a process.MockManager configured for common test scenarios.
func newTestProcessManager() *process.MockManager {
	return &process.MockManager{
		RunFunc: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			// Default: return empty for unknown commands
			return nil, fmt.Errorf("command not mocked: %s %v", name, args)
		},
	}
}

// configurePodmanAvailable sets up the mock to return successful podman responses.
func configurePodmanAvailable(pm *process.MockManager, containers string) {
	pm.RunFunc = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if name != "podman" {
			return nil, fmt.Errorf("unexpected command: %s", name)
		}

		if len(args) >= 3 && args[0] == "version" {
			return []byte("4.8.0"), nil
		}

		if len(args) >= 4 && args[0] == "ps" {
			return []byte(containers), nil
		}

		if len(args) >= 3 && args[0] == "machine" && args[1] == "list" {
			return []byte("[]"), nil
		}

		if len(args) >= 3 && args[0] == "logs" {
			return []byte("Log line 1\nLog line 2\n"), nil
		}

		return nil, fmt.Errorf("unhandled podman command: %v", args)
	}
}

// configurePodmanUnavailable sets up the mock to return podman not found.
func configurePodmanUnavailable(pm *process.MockManager) {
	pm.RunFunc = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if name == "podman" && len(args) >= 1 && args[0] == "version" {
			return nil, fmt.Errorf("podman: command not found")
		}
		return nil, fmt.Errorf("unexpected command: %s", name)
	}
}

// -----------------------------------------------------------------------------
// Constructor Tests
// -----------------------------------------------------------------------------

// TestNewDiagnosticsCollectorWithDeps verifies dependency injection.
func TestNewDiagnosticsCollectorWithDeps(t *testing.T) {
	mockPM := newTestProcessManager()
	mockStorage := newTestStorage()
	formatter := NewJSONDiagnosticsFormatter()

	collector := NewDiagnosticsCollectorWithDeps(mockPM, formatter, mockStorage, "test-version")

	if collector == nil {
		t.Fatal("Expected non-nil collector")
	}

	if collector.aleutianVersion != "test-version" {
		t.Errorf("Version = %q, want %q", collector.aleutianVersion, "test-version")
	}
}

// -----------------------------------------------------------------------------
// Collect Tests
// -----------------------------------------------------------------------------

// TestDefaultDiagnosticsCollector_Collect_Success verifies successful collection.
func TestDefaultDiagnosticsCollector_Collect_Success(t *testing.T) {
	mockPM := newTestProcessManager()
	mockStorage := newTestStorage()
	formatter := NewJSONDiagnosticsFormatter()

	// Configure mock to return podman version
	containerJSON := `[{"Id":"abc123","Names":["aleutian-go-orchestrator"],"State":"running","Image":"aleutian/orchestrator:latest"}]`
	configurePodmanAvailable(mockPM, containerJSON)

	collector := NewDiagnosticsCollectorWithDeps(mockPM, formatter, mockStorage, "0.4.0")
	ctx := context.Background()

	result, err := collector.Collect(ctx, CollectOptions{
		Reason:   "test_collection",
		Severity: SeverityInfo,
	})

	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}

	if result == nil {
		t.Fatal("Expected non-nil result")
	}

	if result.Location != "/mock/diagnostics/test.json" {
		t.Errorf("Location = %q, want %q", result.Location, "/mock/diagnostics/test.json")
	}

	if result.TraceID == "" {
		t.Error("TraceID should not be empty")
	}

	if result.SpanID == "" {
		t.Error("SpanID should not be empty")
	}

	if result.SizeBytes <= 0 {
		t.Errorf("SizeBytes = %d, want > 0", result.SizeBytes)
	}

	// Verify stored data is valid JSON
	stored := mockStorage.GetStoredData()
	var data DiagnosticsData
	if err := json.Unmarshal(stored, &data); err != nil {
		t.Fatalf("Stored data is not valid JSON: %v", err)
	}

	if data.Header.Reason != "test_collection" {
		t.Errorf("Reason = %q, want %q", data.Header.Reason, "test_collection")
	}

	if !data.Podman.Available {
		t.Error("Podman.Available should be true")
	}
}

// TestDefaultDiagnosticsCollector_Collect_PodmanUnavailable verifies graceful degradation.
func TestDefaultDiagnosticsCollector_Collect_PodmanUnavailable(t *testing.T) {
	mockPM := newTestProcessManager()
	mockStorage := newTestStorage()
	formatter := NewJSONDiagnosticsFormatter()

	// Configure mock to fail podman version
	configurePodmanUnavailable(mockPM)

	collector := NewDiagnosticsCollectorWithDeps(mockPM, formatter, mockStorage, "0.4.0")
	ctx := context.Background()

	result, err := collector.Collect(ctx, CollectOptions{
		Reason: "test_no_podman",
	})

	// Collection should succeed even without podman
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}

	if result == nil {
		t.Fatal("Expected non-nil result")
	}

	// Verify podman is marked unavailable
	stored := mockStorage.GetStoredData()
	var data DiagnosticsData
	if err := json.Unmarshal(stored, &data); err != nil {
		t.Fatalf("Stored data is not valid JSON: %v", err)
	}

	if data.Podman.Available {
		t.Error("Podman.Available should be false")
	}

	if data.Podman.Error == "" {
		t.Error("Podman.Error should be set")
	}
}

// TestDefaultDiagnosticsCollector_Collect_WithContainerLogs verifies log collection.
func TestDefaultDiagnosticsCollector_Collect_WithContainerLogs(t *testing.T) {
	mockPM := newTestProcessManager()
	mockStorage := newTestStorage()
	formatter := NewJSONDiagnosticsFormatter()

	// Configure mock responses
	containerJSON := `[{"Id":"abc123","Names":["aleutian-go-orchestrator"],"State":"running","Image":"test"}]`
	configurePodmanAvailable(mockPM, containerJSON)

	collector := NewDiagnosticsCollectorWithDeps(mockPM, formatter, mockStorage, "0.4.0")
	ctx := context.Background()

	result, err := collector.Collect(ctx, CollectOptions{
		Reason:               "test_with_logs",
		IncludeContainerLogs: true,
		ContainerLogLines:    50,
	})

	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}

	stored := mockStorage.GetStoredData()
	var data DiagnosticsData
	if err := json.Unmarshal(stored, &data); err != nil {
		t.Fatalf("Stored data is not valid JSON: %v", err)
	}

	if len(data.ContainerLogs) != 1 {
		t.Fatalf("Expected 1 container log, got %d", len(data.ContainerLogs))
	}

	if !strings.Contains(data.ContainerLogs[0].Logs, "Log line 1") {
		t.Error("Container logs should contain expected content")
	}

	// Verify result
	if result == nil {
		t.Fatal("Expected non-nil result")
	}
}

// TestDefaultDiagnosticsCollector_Collect_StorageError verifies storage failure handling.
func TestDefaultDiagnosticsCollector_Collect_StorageError(t *testing.T) {
	mockPM := newTestProcessManager()
	mockStorage := newTestStorage()
	mockStorage.SetStoreError(fmt.Errorf("disk full"))
	formatter := NewJSONDiagnosticsFormatter()

	configurePodmanAvailable(mockPM, "[]")

	collector := NewDiagnosticsCollectorWithDeps(mockPM, formatter, mockStorage, "0.4.0")
	ctx := context.Background()

	_, err := collector.Collect(ctx, CollectOptions{
		Reason: "test_storage_error",
	})

	if err == nil {
		t.Fatal("Expected error for storage failure")
	}

	if !strings.Contains(err.Error(), "failed to store") {
		t.Errorf("Error should mention storage failure, got: %v", err)
	}
}

// TestDefaultDiagnosticsCollector_Collect_WithTags verifies tag propagation.
func TestDefaultDiagnosticsCollector_Collect_WithTags(t *testing.T) {
	mockPM := newTestProcessManager()
	mockStorage := newTestStorage()
	formatter := NewJSONDiagnosticsFormatter()

	configurePodmanAvailable(mockPM, "[]")

	collector := NewDiagnosticsCollectorWithDeps(mockPM, formatter, mockStorage, "0.4.0")
	ctx := context.Background()

	_, err := collector.Collect(ctx, CollectOptions{
		Reason: "test_tags",
		Tags: map[string]string{
			"environment": "test",
			"component":   "stack",
		},
	})

	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}

	stored := mockStorage.GetStoredData()
	var data DiagnosticsData
	if err := json.Unmarshal(stored, &data); err != nil {
		t.Fatalf("Stored data is not valid JSON: %v", err)
	}

	if data.Tags["environment"] != "test" {
		t.Errorf("Tags[environment] = %q, want %q", data.Tags["environment"], "test")
	}

	if data.Tags["component"] != "stack" {
		t.Errorf("Tags[component] = %q, want %q", data.Tags["component"], "stack")
	}
}

// TestDefaultDiagnosticsCollector_Collect_WithMetrics verifies metrics recording.
func TestDefaultDiagnosticsCollector_Collect_WithMetrics(t *testing.T) {
	mockPM := newTestProcessManager()
	mockStorage := newTestStorage()
	mockMetrics := newTestMetrics()
	formatter := NewJSONDiagnosticsFormatter()

	configurePodmanAvailable(mockPM, "[]")

	collector := NewDiagnosticsCollectorWithDeps(mockPM, formatter, mockStorage, "0.4.0")
	collector.SetMetrics(mockMetrics)
	ctx := context.Background()

	_, err := collector.Collect(ctx, CollectOptions{
		Reason:   "test_metrics",
		Severity: SeverityWarning,
	})

	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}

	if mockMetrics.GetCollectionCount() != 1 {
		t.Errorf("Metrics collection count = %d, want 1", mockMetrics.GetCollectionCount())
	}
}

// -----------------------------------------------------------------------------
// GetLastResult Tests
// -----------------------------------------------------------------------------

// TestDefaultDiagnosticsCollector_GetLastResult verifies result caching.
func TestDefaultDiagnosticsCollector_GetLastResult(t *testing.T) {
	mockPM := newTestProcessManager()
	mockStorage := newTestStorage()
	formatter := NewJSONDiagnosticsFormatter()

	configurePodmanAvailable(mockPM, "[]")

	collector := NewDiagnosticsCollectorWithDeps(mockPM, formatter, mockStorage, "0.4.0")
	ctx := context.Background()

	// Before any collection
	if collector.GetLastResult() != nil {
		t.Error("GetLastResult() should return nil before any collection")
	}

	// After collection
	result, _ := collector.Collect(ctx, CollectOptions{Reason: "test"})
	lastResult := collector.GetLastResult()

	if lastResult == nil {
		t.Fatal("GetLastResult() should return result after collection")
	}

	if lastResult.TraceID != result.TraceID {
		t.Errorf("TraceID mismatch: %q != %q", lastResult.TraceID, result.TraceID)
	}
}

// -----------------------------------------------------------------------------
// SetStorage/SetFormatter Tests
// -----------------------------------------------------------------------------

// TestDefaultDiagnosticsCollector_SetStorage verifies storage swapping.
func TestDefaultDiagnosticsCollector_SetStorage(t *testing.T) {
	mockPM := newTestProcessManager()
	storage1 := newTestStorage()
	storage1.SetReturnPath("/path1/test.json")
	storage2 := newTestStorage()
	storage2.SetReturnPath("/path2/test.json")
	formatter := NewJSONDiagnosticsFormatter()

	configurePodmanAvailable(mockPM, "[]")

	collector := NewDiagnosticsCollectorWithDeps(mockPM, formatter, storage1, "0.4.0")
	ctx := context.Background()

	// First collection uses storage1
	result1, _ := collector.Collect(ctx, CollectOptions{Reason: "test1"})
	if result1.Location != "/path1/test.json" {
		t.Errorf("Location = %q, want %q", result1.Location, "/path1/test.json")
	}

	// Swap storage
	collector.SetStorage(storage2)

	// Second collection uses storage2
	result2, _ := collector.Collect(ctx, CollectOptions{Reason: "test2"})
	if result2.Location != "/path2/test.json" {
		t.Errorf("Location = %q, want %q", result2.Location, "/path2/test.json")
	}
}

// TestDefaultDiagnosticsCollector_SetFormatter verifies formatter swapping.
func TestDefaultDiagnosticsCollector_SetFormatter(t *testing.T) {
	mockPM := newTestProcessManager()
	mockStorage := newTestStorage()

	configurePodmanAvailable(mockPM, "[]")

	collector := NewDiagnosticsCollectorWithDeps(mockPM, NewJSONDiagnosticsFormatter(), mockStorage, "0.4.0")
	ctx := context.Background()

	// First collection uses JSON
	result1, _ := collector.Collect(ctx, CollectOptions{Reason: "test1"})
	if result1.Format != ".json" {
		t.Errorf("Format = %q, want %q", result1.Format, ".json")
	}

	// Swap to text formatter
	collector.SetFormatter(NewTextDiagnosticsFormatter())

	// Second collection uses text
	result2, _ := collector.Collect(ctx, CollectOptions{Reason: "test2"})
	if result2.Format != ".txt" {
		t.Errorf("Format = %q, want %q", result2.Format, ".txt")
	}
}

// -----------------------------------------------------------------------------
// Concurrent Access Tests
// -----------------------------------------------------------------------------

// TestDefaultDiagnosticsCollector_Concurrent verifies thread safety.
func TestDefaultDiagnosticsCollector_Concurrent(t *testing.T) {
	mockPM := newTestProcessManager()
	mockStorage := newTestStorage()
	formatter := NewJSONDiagnosticsFormatter()

	configurePodmanAvailable(mockPM, "[]")

	collector := NewDiagnosticsCollectorWithDeps(mockPM, formatter, mockStorage, "0.4.0")
	ctx := context.Background()

	var wg sync.WaitGroup
	errors := make(chan error, 10)

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			_, err := collector.Collect(ctx, CollectOptions{
				Reason: fmt.Sprintf("concurrent_test_%d", id),
			})
			if err != nil {
				errors <- err
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		t.Errorf("Concurrent collection error: %v", err)
	}

	if mockStorage.GetStoreCount() != 10 {
		t.Errorf("StoreCount = %d, want 10", mockStorage.GetStoreCount())
	}
}

// -----------------------------------------------------------------------------
// Helper Function Tests
// -----------------------------------------------------------------------------

// TestShortenContainerID verifies ID shortening.
func TestShortenContainerID(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"", ""},
		{"abc", "abc"},
		{"abc123def456", "abc123def456"},
		{"abc123def4567890", "abc123def456"},
		{"0123456789abcdef0123456789abcdef", "0123456789ab"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := shortenContainerID(tt.input)
			if got != tt.expected {
				t.Errorf("shortenContainerID(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

// TestInferServiceType verifies service type inference.
func TestInferServiceType(t *testing.T) {
	tests := []struct {
		name     string
		expected string
	}{
		{"aleutian-go-orchestrator", "orchestrator"},
		{"aleutian-weaviate", "vectordb"},
		{"aleutian-rag-engine", "rag"},
		{"aleutian-haystack", "rag"},
		{"aleutian-embedding", "embedding"},
		{"aleutian-ollama", "llm"},
		{"unknown-container", ""},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := inferServiceType(tt.name)
			if got != tt.expected {
				t.Errorf("inferServiceType(%q) = %q, want %q", tt.name, got, tt.expected)
			}
		})
	}
}

// TestCountNewlines verifies line counting.
func TestCountNewlines(t *testing.T) {
	tests := []struct {
		input    string
		expected int
	}{
		{"", 0},
		{"single line", 1},
		{"line1\n", 2},
		{"line1\nline2", 2},
		{"line1\nline2\n", 3},
		{"a\nb\nc\n", 4},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%d_chars", len(tt.input)), func(t *testing.T) {
			got := countNewlines(tt.input)
			if got != tt.expected {
				t.Errorf("countNewlines(%q) = %d, want %d", tt.input, got, tt.expected)
			}
		})
	}
}

// TestParseMemoryString verifies memory parsing.
func TestParseMemoryString(t *testing.T) {
	tests := []struct {
		input    string
		expected int64
	}{
		{"", 0},
		{"invalid", 0},
		{"4096MB", 4096},
		{"4096M", 4096},
		{"4GiB", 4096},
		{"4G", 4096},
		{"8GB", 8192},
		{"1024KB", 1},
		{"1024K", 1},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := parseMemoryString(tt.input)
			if got != tt.expected {
				t.Errorf("parseMemoryString(%q) = %d, want %d", tt.input, got, tt.expected)
			}
		})
	}
}

// TestParseDiskString verifies disk size parsing.
func TestParseDiskString(t *testing.T) {
	tests := []struct {
		input    string
		expected int64
	}{
		{"", 0},
		{"invalid", 0},
		{"100GB", 100},
		{"100G", 100},
		{"1TB", 1024},
		{"1T", 1024},
		{"2048MB", 2},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := parseDiskString(tt.input)
			if got != tt.expected {
				t.Errorf("parseDiskString(%q) = %d, want %d", tt.input, got, tt.expected)
			}
		})
	}
}

// -----------------------------------------------------------------------------
// Integration Tests with Mock Process Manager
// -----------------------------------------------------------------------------

// TestDefaultDiagnosticsCollector_ParseMachineList verifies machine parsing.
func TestDefaultDiagnosticsCollector_ParseMachineList(t *testing.T) {
	mockPM := newTestProcessManager()
	mockStorage := newTestStorage()
	formatter := NewJSONDiagnosticsFormatter()

	collector := NewDiagnosticsCollectorWithDeps(mockPM, formatter, mockStorage, "0.4.0")

	machineJSON := `[
		{"Name":"podman-machine-default","Running":true,"CPUs":4,"Memory":"4096MB","DiskSize":"100GB"},
		{"Name":"test-machine","Running":false,"Starting":true,"CPUs":2,"Memory":"2048MB"}
	]`

	machines := collector.parseMachineList([]byte(machineJSON))

	if len(machines) != 2 {
		t.Fatalf("Expected 2 machines, got %d", len(machines))
	}

	if machines[0].Name != "podman-machine-default" {
		t.Errorf("machines[0].Name = %q, want %q", machines[0].Name, "podman-machine-default")
	}

	if machines[0].State != "running" {
		t.Errorf("machines[0].State = %q, want %q", machines[0].State, "running")
	}

	if machines[1].State != "starting" {
		t.Errorf("machines[1].State = %q, want %q", machines[1].State, "starting")
	}
}

// TestDefaultDiagnosticsCollector_ParseContainerList verifies container parsing.
func TestDefaultDiagnosticsCollector_ParseContainerList(t *testing.T) {
	mockPM := newTestProcessManager()
	mockStorage := newTestStorage()
	formatter := NewJSONDiagnosticsFormatter()

	collector := NewDiagnosticsCollectorWithDeps(mockPM, formatter, mockStorage, "0.4.0")

	containerJSON := `[
		{"Id":"abc123def456789","Names":["aleutian-go-orchestrator"],"State":"running","Image":"aleutian/orchestrator:latest"},
		{"Id":"xyz789","Names":["aleutian-weaviate"],"State":"exited","Image":"weaviate/weaviate:latest"}
	]`

	containers := collector.parseContainerList([]byte(containerJSON))

	if len(containers) != 2 {
		t.Fatalf("Expected 2 containers, got %d", len(containers))
	}

	if containers[0].Name != "aleutian-go-orchestrator" {
		t.Errorf("containers[0].Name = %q, want %q", containers[0].Name, "aleutian-go-orchestrator")
	}

	if containers[0].ServiceType != "orchestrator" {
		t.Errorf("containers[0].ServiceType = %q, want %q", containers[0].ServiceType, "orchestrator")
	}

	if containers[1].ServiceType != "vectordb" {
		t.Errorf("containers[1].ServiceType = %q, want %q", containers[1].ServiceType, "vectordb")
	}

	// Verify ID was shortened
	if containers[0].ID != "abc123def456" {
		t.Errorf("containers[0].ID = %q, want %q", containers[0].ID, "abc123def456")
	}
}

// TestDefaultDiagnosticsCollector_GetContainerLog_NotRunning verifies log handling for stopped containers.
func TestDefaultDiagnosticsCollector_GetContainerLog_NotRunning(t *testing.T) {
	mockPM := newTestProcessManager()
	mockStorage := newTestStorage()
	formatter := NewJSONDiagnosticsFormatter()

	collector := NewDiagnosticsCollectorWithDeps(mockPM, formatter, mockStorage, "0.4.0")
	ctx := context.Background()

	container := ContainerInfo{
		Name:  "test-container",
		State: "exited",
	}

	log := collector.getContainerLog(ctx, container, 50)

	if log.Logs != "(container not running)" {
		t.Errorf("Logs = %q, want %q", log.Logs, "(container not running)")
	}
}

// -----------------------------------------------------------------------------
// Interface Compliance Test
// -----------------------------------------------------------------------------

// TestDefaultDiagnosticsCollector_InterfaceCompliance verifies interface implementation.
func TestDefaultDiagnosticsCollector_InterfaceCompliance(t *testing.T) {
	var _ DiagnosticsCollector = (*DefaultDiagnosticsCollector)(nil)
}
