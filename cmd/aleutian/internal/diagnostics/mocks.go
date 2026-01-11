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
Package diagnostics provides mock implementations for the Distributed Health Agent.

This file contains test doubles for all diagnostic interfaces, enabling fast,
isolated unit tests without network or filesystem dependencies.

# Mock Design Pattern

All mocks use the "function field" pattern:

	mock := &MockDiagnosticsCollector{
	    CollectFunc: func(ctx context.Context, opts CollectOptions) (*DiagnosticsResult, error) {
	        return &DiagnosticsResult{TraceID: "test-123"}, nil
	    },
	}

This pattern provides:

  - Per-test behavior customization without subclassing
  - Type safety via interface compliance checks
  - Call tracking for verification
  - Simple defaults when functions aren't set

# Thread Safety

All mocks are thread-safe and can be used in concurrent tests. Call counts
and recorded inputs are protected by mutexes.

# Usage Example

	func TestMyFunction(t *testing.T) {
	    mock := NewMockDiagnosticsCollector()
	    mock.CollectFunc = func(ctx context.Context, opts CollectOptions) (*DiagnosticsResult, error) {
	        return &DiagnosticsResult{TraceID: "test-trace-id"}, nil
	    }

	    result, err := functionUnderTest(mock)

	    assert.NoError(t, err)
	    assert.Equal(t, 1, mock.CollectCallCount())
	}
*/
package diagnostics

import (
	"context"
	"sync"
)

// -----------------------------------------------------------------------------
// MockDiagnosticsCollector
// -----------------------------------------------------------------------------

// MockDiagnosticsCollector is a test double for DiagnosticsCollector.
//
// # Description
//
// Provides configurable behavior for DiagnosticsCollector in unit tests.
// Each method can be customized via function fields, and call counts are
// tracked for verification.
//
// # Thread Safety
//
// MockDiagnosticsCollector is safe for concurrent use. All call counts
// and recorded inputs are protected by a mutex.
//
// # Example
//
//	mock := NewMockDiagnosticsCollector()
//	mock.CollectFunc = func(ctx context.Context, opts CollectOptions) (*DiagnosticsResult, error) {
//	    return &DiagnosticsResult{TraceID: "test-123"}, nil
//	}
type MockDiagnosticsCollector struct {
	// CollectFunc is called by Collect. Set this to customize behavior.
	CollectFunc func(ctx context.Context, opts CollectOptions) (*DiagnosticsResult, error)

	// GetLastResultFunc is called by GetLastResult. Set this to customize behavior.
	GetLastResultFunc func() *DiagnosticsResult

	// SetStorageFunc is called by SetStorage. Set this to customize behavior.
	SetStorageFunc func(storage DiagnosticsStorage)

	// SetFormatterFunc is called by SetFormatter. Set this to customize behavior.
	SetFormatterFunc func(formatter DiagnosticsFormatter)

	// mu protects call counts and recorded inputs.
	mu sync.RWMutex

	// collectCallCount tracks calls to Collect.
	collectCallCount int

	// collectInputs records inputs to Collect calls.
	collectInputs []CollectOptions

	// getLastResultCallCount tracks calls to GetLastResult.
	getLastResultCallCount int

	// setStorageCallCount tracks calls to SetStorage.
	setStorageCallCount int

	// setFormatterCallCount tracks calls to SetFormatter.
	setFormatterCallCount int
}

// NewMockDiagnosticsCollector creates a mock with default no-op behavior.
//
// # Description
//
// Creates a mock collector with sensible defaults: Collect returns an empty
// result, GetLastResult returns nil, and setters are no-ops.
//
// # Outputs
//
//   - *MockDiagnosticsCollector: Ready-to-use mock
//
// # Examples
//
//	mock := NewMockDiagnosticsCollector()
//	// Use defaults or customize:
//	mock.CollectFunc = func(ctx context.Context, opts CollectOptions) (*DiagnosticsResult, error) {
//	    return &DiagnosticsResult{TraceID: "custom"}, nil
//	}
//
// # Limitations
//
//   - None
//
// # Assumptions
//
//   - None
func NewMockDiagnosticsCollector() *MockDiagnosticsCollector {
	return &MockDiagnosticsCollector{
		collectInputs: make([]CollectOptions, 0),
	}
}

// Collect invokes CollectFunc and tracks the call.
//
// # Description
//
// Calls the configured CollectFunc if set, otherwise returns a default
// empty result. Records the input options for later verification.
//
// # Inputs
//
//   - ctx: Context for cancellation (passed to CollectFunc)
//   - opts: Collection options (recorded for verification)
//
// # Outputs
//
//   - *DiagnosticsResult: Result from CollectFunc, or empty default
//   - error: Error from CollectFunc, or nil
//
// # Examples
//
//	mock := NewMockDiagnosticsCollector()
//	result, err := mock.Collect(ctx, CollectOptions{Reason: "test"})
//	assert.Equal(t, 1, mock.CollectCallCount())
//
// # Limitations
//
//   - Default behavior returns empty result, not realistic data
//
// # Assumptions
//
//   - CollectFunc handles all business logic if set
func (m *MockDiagnosticsCollector) Collect(ctx context.Context, opts CollectOptions) (*DiagnosticsResult, error) {
	m.mu.Lock()
	m.collectCallCount++
	m.collectInputs = append(m.collectInputs, opts)
	m.mu.Unlock()

	if m.CollectFunc != nil {
		return m.CollectFunc(ctx, opts)
	}
	return &DiagnosticsResult{}, nil
}

// GetLastResult invokes GetLastResultFunc and tracks the call.
//
// # Description
//
// Calls the configured GetLastResultFunc if set, otherwise returns nil.
//
// # Outputs
//
//   - *DiagnosticsResult: Result from GetLastResultFunc, or nil
//
// # Examples
//
//	mock := NewMockDiagnosticsCollector()
//	mock.GetLastResultFunc = func() *DiagnosticsResult {
//	    return &DiagnosticsResult{TraceID: "last-trace"}
//	}
//	result := mock.GetLastResult()
//
// # Limitations
//
//   - Default returns nil, not a realistic result
//
// # Assumptions
//
//   - GetLastResultFunc handles all logic if set
func (m *MockDiagnosticsCollector) GetLastResult() *DiagnosticsResult {
	m.mu.Lock()
	m.getLastResultCallCount++
	m.mu.Unlock()

	if m.GetLastResultFunc != nil {
		return m.GetLastResultFunc()
	}
	return nil
}

// SetStorage invokes SetStorageFunc and tracks the call.
//
// # Description
//
// Calls the configured SetStorageFunc if set, otherwise does nothing.
//
// # Inputs
//
//   - storage: Storage backend to configure (passed to SetStorageFunc)
//
// # Examples
//
//	mock := NewMockDiagnosticsCollector()
//	mock.SetStorage(fileStorage)
//	assert.Equal(t, 1, mock.SetStorageCallCount())
//
// # Limitations
//
//   - Default is a no-op
//
// # Assumptions
//
//   - SetStorageFunc handles storage if set
func (m *MockDiagnosticsCollector) SetStorage(storage DiagnosticsStorage) {
	m.mu.Lock()
	m.setStorageCallCount++
	m.mu.Unlock()

	if m.SetStorageFunc != nil {
		m.SetStorageFunc(storage)
	}
}

// SetFormatter invokes SetFormatterFunc and tracks the call.
//
// # Description
//
// Calls the configured SetFormatterFunc if set, otherwise does nothing.
//
// # Inputs
//
//   - formatter: Formatter to configure (passed to SetFormatterFunc)
//
// # Examples
//
//	mock := NewMockDiagnosticsCollector()
//	mock.SetFormatter(jsonFormatter)
//	assert.Equal(t, 1, mock.SetFormatterCallCount())
//
// # Limitations
//
//   - Default is a no-op
//
// # Assumptions
//
//   - SetFormatterFunc handles formatter if set
func (m *MockDiagnosticsCollector) SetFormatter(formatter DiagnosticsFormatter) {
	m.mu.Lock()
	m.setFormatterCallCount++
	m.mu.Unlock()

	if m.SetFormatterFunc != nil {
		m.SetFormatterFunc(formatter)
	}
}

// CollectCallCount returns the number of times Collect was called.
//
// # Description
//
// Thread-safe accessor for verifying Collect invocations in tests.
//
// # Outputs
//
//   - int: Number of Collect calls
//
// # Examples
//
//	mock.Collect(ctx, opts)
//	mock.Collect(ctx, opts)
//	assert.Equal(t, 2, mock.CollectCallCount())
//
// # Limitations
//
//   - None
//
// # Assumptions
//
//   - None
func (m *MockDiagnosticsCollector) CollectCallCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.collectCallCount
}

// CollectInputAt returns the input from a specific Collect call.
//
// # Description
//
// Retrieves recorded input options for verification. Index 0 is the first call.
//
// # Inputs
//
//   - index: Call index (0-based)
//
// # Outputs
//
//   - CollectOptions: Recorded input options
//   - bool: True if index was valid
//
// # Examples
//
//	mock.Collect(ctx, CollectOptions{Reason: "test"})
//	opts, ok := mock.CollectInputAt(0)
//	assert.True(t, ok)
//	assert.Equal(t, "test", opts.Reason)
//
// # Limitations
//
//   - Returns false if index out of bounds
//
// # Assumptions
//
//   - Index is valid (0 <= index < CollectCallCount)
func (m *MockDiagnosticsCollector) CollectInputAt(index int) (CollectOptions, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if index < 0 || index >= len(m.collectInputs) {
		return CollectOptions{}, false
	}
	return m.collectInputs[index], true
}

// GetLastResultCallCount returns the number of times GetLastResult was called.
//
// # Description
//
// Thread-safe accessor for verifying GetLastResult invocations.
//
// # Outputs
//
//   - int: Number of GetLastResult calls
//
// # Examples
//
//	mock.GetLastResult()
//	assert.Equal(t, 1, mock.GetLastResultCallCount())
//
// # Limitations
//
//   - None
//
// # Assumptions
//
//   - None
func (m *MockDiagnosticsCollector) GetLastResultCallCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.getLastResultCallCount
}

// SetStorageCallCount returns the number of times SetStorage was called.
//
// # Description
//
// Thread-safe accessor for verifying SetStorage invocations.
//
// # Outputs
//
//   - int: Number of SetStorage calls
//
// # Examples
//
//	mock.SetStorage(storage)
//	assert.Equal(t, 1, mock.SetStorageCallCount())
//
// # Limitations
//
//   - None
//
// # Assumptions
//
//   - None
func (m *MockDiagnosticsCollector) SetStorageCallCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.setStorageCallCount
}

// SetFormatterCallCount returns the number of times SetFormatter was called.
//
// # Description
//
// Thread-safe accessor for verifying SetFormatter invocations.
//
// # Outputs
//
//   - int: Number of SetFormatter calls
//
// # Examples
//
//	mock.SetFormatter(formatter)
//	assert.Equal(t, 1, mock.SetFormatterCallCount())
//
// # Limitations
//
//   - None
//
// # Assumptions
//
//   - None
func (m *MockDiagnosticsCollector) SetFormatterCallCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.setFormatterCallCount
}

// Reset clears all call counts and recorded inputs.
//
// # Description
//
// Resets the mock to initial state for reuse across test cases.
//
// # Examples
//
//	mock.Collect(ctx, opts)
//	mock.Reset()
//	assert.Equal(t, 0, mock.CollectCallCount())
//
// # Limitations
//
//   - Does not reset function fields (CollectFunc, etc.)
//
// # Assumptions
//
//   - None
func (m *MockDiagnosticsCollector) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.collectCallCount = 0
	m.collectInputs = make([]CollectOptions, 0)
	m.getLastResultCallCount = 0
	m.setStorageCallCount = 0
	m.setFormatterCallCount = 0
}

// -----------------------------------------------------------------------------
// MockDiagnosticsFormatter
// -----------------------------------------------------------------------------

// MockDiagnosticsFormatter is a test double for DiagnosticsFormatter.
//
// # Description
//
// Provides configurable behavior for DiagnosticsFormatter in unit tests.
//
// # Thread Safety
//
// MockDiagnosticsFormatter is safe for concurrent use.
type MockDiagnosticsFormatter struct {
	// FormatFunc is called by Format. Set this to customize behavior.
	FormatFunc func(data *DiagnosticsData) ([]byte, error)

	// ContentTypeFunc is called by ContentType. Set this to customize behavior.
	ContentTypeFunc func() string

	// FileExtensionFunc is called by FileExtension. Set this to customize behavior.
	FileExtensionFunc func() string

	// mu protects call counts.
	mu sync.RWMutex

	// formatCallCount tracks calls to Format.
	formatCallCount int

	// formatInputs records inputs to Format.
	formatInputs []*DiagnosticsData
}

// NewMockDiagnosticsFormatter creates a mock formatter with defaults.
//
// # Description
//
// Creates a mock formatter that returns empty bytes, "text/plain" content type,
// and ".txt" extension by default.
//
// # Outputs
//
//   - *MockDiagnosticsFormatter: Ready-to-use mock
//
// # Examples
//
//	mock := NewMockDiagnosticsFormatter()
//	mock.FormatFunc = func(data *DiagnosticsData) ([]byte, error) {
//	    return []byte(`{"test": true}`), nil
//	}
//
// # Limitations
//
//   - None
//
// # Assumptions
//
//   - None
func NewMockDiagnosticsFormatter() *MockDiagnosticsFormatter {
	return &MockDiagnosticsFormatter{
		formatInputs: make([]*DiagnosticsData, 0),
	}
}

// Format invokes FormatFunc and tracks the call.
//
// # Description
//
// Calls the configured FormatFunc if set, otherwise returns empty bytes.
// Records the input data for later verification.
//
// # Inputs
//
//   - data: Diagnostic data to format (recorded for verification)
//
// # Outputs
//
//   - []byte: Formatted output from FormatFunc, or empty bytes
//   - error: Error from FormatFunc, or nil
//
// # Examples
//
//	mock := NewMockDiagnosticsFormatter()
//	output, err := mock.Format(&DiagnosticsData{})
//	assert.Equal(t, 1, mock.FormatCallCount())
//
// # Limitations
//
//   - Default returns empty bytes
//
// # Assumptions
//
//   - FormatFunc handles formatting if set
func (m *MockDiagnosticsFormatter) Format(data *DiagnosticsData) ([]byte, error) {
	m.mu.Lock()
	m.formatCallCount++
	m.formatInputs = append(m.formatInputs, data)
	m.mu.Unlock()

	if m.FormatFunc != nil {
		return m.FormatFunc(data)
	}
	return []byte{}, nil
}

// ContentType invokes ContentTypeFunc or returns default.
//
// # Description
//
// Calls the configured ContentTypeFunc if set, otherwise returns "text/plain".
//
// # Outputs
//
//   - string: MIME type from ContentTypeFunc, or "text/plain"
//
// # Examples
//
//	mock := NewMockDiagnosticsFormatter()
//	mock.ContentTypeFunc = func() string { return "application/json" }
//	contentType := mock.ContentType()
//
// # Limitations
//
//   - Default is "text/plain", not JSON
//
// # Assumptions
//
//   - ContentTypeFunc handles type if set
func (m *MockDiagnosticsFormatter) ContentType() string {
	if m.ContentTypeFunc != nil {
		return m.ContentTypeFunc()
	}
	return "text/plain"
}

// FileExtension invokes FileExtensionFunc or returns default.
//
// # Description
//
// Calls the configured FileExtensionFunc if set, otherwise returns ".txt".
//
// # Outputs
//
//   - string: Extension from FileExtensionFunc, or ".txt"
//
// # Examples
//
//	mock := NewMockDiagnosticsFormatter()
//	mock.FileExtensionFunc = func() string { return ".json" }
//	ext := mock.FileExtension()
//
// # Limitations
//
//   - Default is ".txt", not ".json"
//
// # Assumptions
//
//   - FileExtensionFunc handles extension if set
func (m *MockDiagnosticsFormatter) FileExtension() string {
	if m.FileExtensionFunc != nil {
		return m.FileExtensionFunc()
	}
	return ".txt"
}

// FormatCallCount returns the number of times Format was called.
//
// # Description
//
// Thread-safe accessor for verifying Format invocations.
//
// # Outputs
//
//   - int: Number of Format calls
//
// # Examples
//
//	mock.Format(&DiagnosticsData{})
//	assert.Equal(t, 1, mock.FormatCallCount())
//
// # Limitations
//
//   - None
//
// # Assumptions
//
//   - None
func (m *MockDiagnosticsFormatter) FormatCallCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.formatCallCount
}

// FormatInputAt returns the input from a specific Format call.
//
// # Description
//
// Retrieves recorded input data for verification.
//
// # Inputs
//
//   - index: Call index (0-based)
//
// # Outputs
//
//   - *DiagnosticsData: Recorded input data
//   - bool: True if index was valid
//
// # Examples
//
//	data := &DiagnosticsData{}
//	mock.Format(data)
//	input, ok := mock.FormatInputAt(0)
//	assert.True(t, ok)
//
// # Limitations
//
//   - Returns nil, false if index out of bounds
//
// # Assumptions
//
//   - Index is valid
func (m *MockDiagnosticsFormatter) FormatInputAt(index int) (*DiagnosticsData, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if index < 0 || index >= len(m.formatInputs) {
		return nil, false
	}
	return m.formatInputs[index], true
}

// Reset clears all call counts and recorded inputs.
//
// # Description
//
// Resets the mock to initial state for reuse.
//
// # Examples
//
//	mock.Format(data)
//	mock.Reset()
//	assert.Equal(t, 0, mock.FormatCallCount())
//
// # Limitations
//
//   - Does not reset function fields
//
// # Assumptions
//
//   - None
func (m *MockDiagnosticsFormatter) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.formatCallCount = 0
	m.formatInputs = make([]*DiagnosticsData, 0)
}

// -----------------------------------------------------------------------------
// MockDiagnosticsStorage
// -----------------------------------------------------------------------------

// MockDiagnosticsStorage is a test double for DiagnosticsStorage.
//
// # Description
//
// Provides configurable behavior for DiagnosticsStorage in unit tests.
// Supports in-memory storage simulation without filesystem access.
//
// # Thread Safety
//
// MockDiagnosticsStorage is safe for concurrent use.
type MockDiagnosticsStorage struct {
	// StoreFunc is called by Store. Set this to customize behavior.
	StoreFunc func(ctx context.Context, data []byte, metadata StorageMetadata) (string, error)

	// LoadFunc is called by Load. Set this to customize behavior.
	LoadFunc func(ctx context.Context, location string) ([]byte, error)

	// ListFunc is called by List. Set this to customize behavior.
	ListFunc func(ctx context.Context, limit int) ([]string, error)

	// PruneFunc is called by Prune. Set this to customize behavior.
	PruneFunc func(ctx context.Context) (int, error)

	// TypeFunc is called by Type. Set this to customize behavior.
	TypeFunc func() string

	// retentionDays stores the configured retention period.
	retentionDays int

	// mu protects call counts and storage.
	mu sync.RWMutex

	// storeCallCount tracks calls to Store.
	storeCallCount int

	// loadCallCount tracks calls to Load.
	loadCallCount int

	// listCallCount tracks calls to List.
	listCallCount int

	// pruneCallCount tracks calls to Prune.
	pruneCallCount int

	// storedData simulates in-memory storage.
	storedData map[string][]byte
}

// NewMockDiagnosticsStorage creates a mock storage with defaults.
//
// # Description
//
// Creates a mock storage that simulates in-memory storage. Store adds to
// an internal map, Load retrieves from it, List returns keys.
//
// # Outputs
//
//   - *MockDiagnosticsStorage: Ready-to-use mock
//
// # Examples
//
//	mock := NewMockDiagnosticsStorage()
//	location, _ := mock.Store(ctx, []byte("data"), StorageMetadata{})
//	data, _ := mock.Load(ctx, location)
//
// # Limitations
//
//   - In-memory storage is lost on test completion
//
// # Assumptions
//
//   - None
func NewMockDiagnosticsStorage() *MockDiagnosticsStorage {
	return &MockDiagnosticsStorage{
		retentionDays: 30,
		storedData:    make(map[string][]byte),
	}
}

// Store invokes StoreFunc and tracks the call.
//
// # Description
//
// Calls the configured StoreFunc if set, otherwise stores in internal map
// using FilenameHint as the key.
//
// # Inputs
//
//   - ctx: Context for cancellation (passed to StoreFunc)
//   - data: Data to store
//   - metadata: Storage hints including filename
//
// # Outputs
//
//   - string: Location (FilenameHint or StoreFunc result)
//   - error: Error from StoreFunc, or nil
//
// # Examples
//
//	mock := NewMockDiagnosticsStorage()
//	location, err := mock.Store(ctx, []byte("test"), StorageMetadata{FilenameHint: "test.json"})
//	assert.Equal(t, "test.json", location)
//
// # Limitations
//
//   - Default uses FilenameHint as location
//
// # Assumptions
//
//   - StoreFunc handles storage if set
func (m *MockDiagnosticsStorage) Store(ctx context.Context, data []byte, metadata StorageMetadata) (string, error) {
	m.mu.Lock()
	m.storeCallCount++
	m.mu.Unlock()

	if m.StoreFunc != nil {
		return m.StoreFunc(ctx, data, metadata)
	}

	// Default: store in memory
	location := metadata.FilenameHint
	if location == "" {
		location = "mock-diagnostic.json"
	}
	m.mu.Lock()
	m.storedData[location] = data
	m.mu.Unlock()
	return location, nil
}

// Load invokes LoadFunc and tracks the call.
//
// # Description
//
// Calls the configured LoadFunc if set, otherwise retrieves from internal map.
//
// # Inputs
//
//   - ctx: Context for cancellation (passed to LoadFunc)
//   - location: Storage location to load
//
// # Outputs
//
//   - []byte: Data from LoadFunc or internal map
//   - error: Error from LoadFunc, or nil
//
// # Examples
//
//	mock := NewMockDiagnosticsStorage()
//	mock.Store(ctx, []byte("test"), StorageMetadata{FilenameHint: "test.json"})
//	data, err := mock.Load(ctx, "test.json")
//	assert.Equal(t, []byte("test"), data)
//
// # Limitations
//
//   - Returns nil if location not found in default mode
//
// # Assumptions
//
//   - LoadFunc handles loading if set
func (m *MockDiagnosticsStorage) Load(ctx context.Context, location string) ([]byte, error) {
	m.mu.Lock()
	m.loadCallCount++
	m.mu.Unlock()

	if m.LoadFunc != nil {
		return m.LoadFunc(ctx, location)
	}

	// Default: load from memory
	m.mu.RLock()
	data := m.storedData[location]
	m.mu.RUnlock()
	return data, nil
}

// List invokes ListFunc and tracks the call.
//
// # Description
//
// Calls the configured ListFunc if set, otherwise returns keys from internal map.
//
// # Inputs
//
//   - ctx: Context for cancellation (passed to ListFunc)
//   - limit: Maximum entries to return (0 = all)
//
// # Outputs
//
//   - []string: Locations from ListFunc or internal map keys
//   - error: Error from ListFunc, or nil
//
// # Examples
//
//	mock := NewMockDiagnosticsStorage()
//	mock.Store(ctx, []byte("a"), StorageMetadata{FilenameHint: "a.json"})
//	mock.Store(ctx, []byte("b"), StorageMetadata{FilenameHint: "b.json"})
//	locations, _ := mock.List(ctx, 10)
//	assert.Len(t, locations, 2)
//
// # Limitations
//
//   - Default doesn't sort by modification time
//
// # Assumptions
//
//   - ListFunc handles listing if set
func (m *MockDiagnosticsStorage) List(ctx context.Context, limit int) ([]string, error) {
	m.mu.Lock()
	m.listCallCount++
	m.mu.Unlock()

	if m.ListFunc != nil {
		return m.ListFunc(ctx, limit)
	}

	// Default: return all keys
	m.mu.RLock()
	defer m.mu.RUnlock()
	locations := make([]string, 0, len(m.storedData))
	for k := range m.storedData {
		locations = append(locations, k)
		if limit > 0 && len(locations) >= limit {
			break
		}
	}
	return locations, nil
}

// Prune invokes PruneFunc and tracks the call.
//
// # Description
//
// Calls the configured PruneFunc if set, otherwise returns 0 (nothing pruned).
//
// # Inputs
//
//   - ctx: Context for cancellation (passed to PruneFunc)
//
// # Outputs
//
//   - int: Number pruned from PruneFunc, or 0
//   - error: Error from PruneFunc, or nil
//
// # Examples
//
//	mock := NewMockDiagnosticsStorage()
//	mock.PruneFunc = func(ctx context.Context) (int, error) {
//	    return 5, nil // Simulated prune
//	}
//	pruned, _ := mock.Prune(ctx)
//	assert.Equal(t, 5, pruned)
//
// # Limitations
//
//   - Default doesn't actually prune stored data
//
// # Assumptions
//
//   - PruneFunc handles pruning if set
func (m *MockDiagnosticsStorage) Prune(ctx context.Context) (int, error) {
	m.mu.Lock()
	m.pruneCallCount++
	m.mu.Unlock()

	if m.PruneFunc != nil {
		return m.PruneFunc(ctx)
	}
	return 0, nil
}

// SetRetentionDays sets the retention period.
//
// # Description
//
// Stores the retention period for later retrieval.
//
// # Inputs
//
//   - days: Retention period in days
//
// # Examples
//
//	mock := NewMockDiagnosticsStorage()
//	mock.SetRetentionDays(7)
//	assert.Equal(t, 7, mock.GetRetentionDays())
//
// # Limitations
//
//   - None
//
// # Assumptions
//
//   - None
func (m *MockDiagnosticsStorage) SetRetentionDays(days int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.retentionDays = days
}

// GetRetentionDays returns the configured retention period.
//
// # Description
//
// Thread-safe accessor for the retention period.
//
// # Outputs
//
//   - int: Retention period in days
//
// # Examples
//
//	days := mock.GetRetentionDays()
//
// # Limitations
//
//   - None
//
// # Assumptions
//
//   - None
func (m *MockDiagnosticsStorage) GetRetentionDays() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.retentionDays
}

// Type invokes TypeFunc or returns default.
//
// # Description
//
// Calls the configured TypeFunc if set, otherwise returns "mock".
//
// # Outputs
//
//   - string: Type from TypeFunc, or "mock"
//
// # Examples
//
//	mock := NewMockDiagnosticsStorage()
//	assert.Equal(t, "mock", mock.Type())
//
// # Limitations
//
//   - None
//
// # Assumptions
//
//   - TypeFunc handles type if set
func (m *MockDiagnosticsStorage) Type() string {
	if m.TypeFunc != nil {
		return m.TypeFunc()
	}
	return "mock"
}

// StoreCallCount returns the number of times Store was called.
//
// # Description
//
// Thread-safe accessor for verifying Store invocations.
//
// # Outputs
//
//   - int: Number of Store calls
//
// # Examples
//
//	mock.Store(ctx, data, meta)
//	assert.Equal(t, 1, mock.StoreCallCount())
//
// # Limitations
//
//   - None
//
// # Assumptions
//
//   - None
func (m *MockDiagnosticsStorage) StoreCallCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.storeCallCount
}

// LoadCallCount returns the number of times Load was called.
//
// # Description
//
// Thread-safe accessor for verifying Load invocations.
//
// # Outputs
//
//   - int: Number of Load calls
//
// # Examples
//
//	mock.Load(ctx, "location")
//	assert.Equal(t, 1, mock.LoadCallCount())
//
// # Limitations
//
//   - None
//
// # Assumptions
//
//   - None
func (m *MockDiagnosticsStorage) LoadCallCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.loadCallCount
}

// ListCallCount returns the number of times List was called.
//
// # Description
//
// Thread-safe accessor for verifying List invocations.
//
// # Outputs
//
//   - int: Number of List calls
//
// # Examples
//
//	mock.List(ctx, 10)
//	assert.Equal(t, 1, mock.ListCallCount())
//
// # Limitations
//
//   - None
//
// # Assumptions
//
//   - None
func (m *MockDiagnosticsStorage) ListCallCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.listCallCount
}

// PruneCallCount returns the number of times Prune was called.
//
// # Description
//
// Thread-safe accessor for verifying Prune invocations.
//
// # Outputs
//
//   - int: Number of Prune calls
//
// # Examples
//
//	mock.Prune(ctx)
//	assert.Equal(t, 1, mock.PruneCallCount())
//
// # Limitations
//
//   - None
//
// # Assumptions
//
//   - None
func (m *MockDiagnosticsStorage) PruneCallCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.pruneCallCount
}

// Reset clears all call counts and stored data.
//
// # Description
//
// Resets the mock to initial state including clearing stored data.
//
// # Examples
//
//	mock.Store(ctx, data, meta)
//	mock.Reset()
//	assert.Equal(t, 0, mock.StoreCallCount())
//
// # Limitations
//
//   - Does not reset function fields
//
// # Assumptions
//
//   - None
func (m *MockDiagnosticsStorage) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.storeCallCount = 0
	m.loadCallCount = 0
	m.listCallCount = 0
	m.pruneCallCount = 0
	m.storedData = make(map[string][]byte)
}

// -----------------------------------------------------------------------------
// MockDiagnosticsMetrics
// -----------------------------------------------------------------------------

// MockDiagnosticsMetrics is a test double for DiagnosticsMetrics.
//
// # Description
//
// Provides configurable behavior for DiagnosticsMetrics in unit tests.
// Tracks calls and recorded values for verification.
//
// # Thread Safety
//
// MockDiagnosticsMetrics is safe for concurrent use.
type MockDiagnosticsMetrics struct {
	// RecordCollectionFunc is called by RecordCollection. Set this to customize behavior.
	RecordCollectionFunc func(severity DiagnosticsSeverity, reason string, durationMs int64, sizeBytes int64)

	// RecordErrorFunc is called by RecordError. Set this to customize behavior.
	RecordErrorFunc func(errorType string)

	// RecordContainerHealthFunc is called by RecordContainerHealth. Set this to customize behavior.
	RecordContainerHealthFunc func(containerName, serviceType, status string)

	// RecordContainerMetricsFunc is called by RecordContainerMetrics. Set this to customize behavior.
	RecordContainerMetricsFunc func(containerName string, cpuPercent float64, memoryMB int64)

	// RecordPrunedFunc is called by RecordPruned. Set this to customize behavior.
	RecordPrunedFunc func(count int)

	// RecordStoredCountFunc is called by RecordStoredCount. Set this to customize behavior.
	RecordStoredCountFunc func(count int)

	// RegisterFunc is called by Register. Set this to customize behavior.
	RegisterFunc func() error

	// mu protects call counts.
	mu sync.RWMutex

	// collectionCallCount tracks calls to RecordCollection.
	collectionCallCount int

	// errorCallCount tracks calls to RecordError.
	errorCallCount int

	// containerHealthCallCount tracks calls to RecordContainerHealth.
	containerHealthCallCount int

	// containerMetricsCallCount tracks calls to RecordContainerMetrics.
	containerMetricsCallCount int

	// prunedCallCount tracks calls to RecordPruned.
	prunedCallCount int

	// storedCountCallCount tracks calls to RecordStoredCount.
	storedCountCallCount int

	// registerCallCount tracks calls to Register.
	registerCallCount int
}

// NewMockDiagnosticsMetrics creates a mock metrics recorder with defaults.
//
// # Description
//
// Creates a mock that tracks all method calls with no-op defaults.
//
// # Outputs
//
//   - *MockDiagnosticsMetrics: Ready-to-use mock
//
// # Examples
//
//	mock := NewMockDiagnosticsMetrics()
//	mock.RecordCollection(SeverityError, "test", 100, 1024)
//	assert.Equal(t, 1, mock.RecordCollectionCallCount())
//
// # Limitations
//
//   - None
//
// # Assumptions
//
//   - None
func NewMockDiagnosticsMetrics() *MockDiagnosticsMetrics {
	return &MockDiagnosticsMetrics{}
}

// RecordCollection invokes RecordCollectionFunc and tracks the call.
//
// # Description
//
// Calls the configured RecordCollectionFunc if set, otherwise does nothing.
//
// # Inputs
//
//   - severity: Diagnostic severity level
//   - reason: Collection reason
//   - durationMs: Duration in milliseconds
//   - sizeBytes: Output size in bytes
//
// # Examples
//
//	mock := NewMockDiagnosticsMetrics()
//	mock.RecordCollection(SeverityError, "test", 100, 1024)
//
// # Limitations
//
//   - Default is a no-op
//
// # Assumptions
//
//   - RecordCollectionFunc handles recording if set
func (m *MockDiagnosticsMetrics) RecordCollection(severity DiagnosticsSeverity, reason string, durationMs int64, sizeBytes int64) {
	m.mu.Lock()
	m.collectionCallCount++
	m.mu.Unlock()

	if m.RecordCollectionFunc != nil {
		m.RecordCollectionFunc(severity, reason, durationMs, sizeBytes)
	}
}

// RecordError invokes RecordErrorFunc and tracks the call.
//
// # Description
//
// Calls the configured RecordErrorFunc if set, otherwise does nothing.
//
// # Inputs
//
//   - errorType: Error category
//
// # Examples
//
//	mock := NewMockDiagnosticsMetrics()
//	mock.RecordError("storage_failure")
//
// # Limitations
//
//   - Default is a no-op
//
// # Assumptions
//
//   - RecordErrorFunc handles recording if set
func (m *MockDiagnosticsMetrics) RecordError(errorType string) {
	m.mu.Lock()
	m.errorCallCount++
	m.mu.Unlock()

	if m.RecordErrorFunc != nil {
		m.RecordErrorFunc(errorType)
	}
}

// RecordContainerHealth invokes RecordContainerHealthFunc and tracks the call.
//
// # Description
//
// Calls the configured RecordContainerHealthFunc if set, otherwise does nothing.
//
// # Inputs
//
//   - containerName: Container identifier
//   - serviceType: Service category
//   - status: Health status
//
// # Examples
//
//	mock := NewMockDiagnosticsMetrics()
//	mock.RecordContainerHealth("weaviate", "vectordb", "healthy")
//
// # Limitations
//
//   - Default is a no-op
//
// # Assumptions
//
//   - RecordContainerHealthFunc handles recording if set
func (m *MockDiagnosticsMetrics) RecordContainerHealth(containerName, serviceType, status string) {
	m.mu.Lock()
	m.containerHealthCallCount++
	m.mu.Unlock()

	if m.RecordContainerHealthFunc != nil {
		m.RecordContainerHealthFunc(containerName, serviceType, status)
	}
}

// RecordContainerMetrics invokes RecordContainerMetricsFunc and tracks the call.
//
// # Description
//
// Calls the configured RecordContainerMetricsFunc if set, otherwise does nothing.
//
// # Inputs
//
//   - containerName: Container identifier
//   - cpuPercent: CPU usage percentage
//   - memoryMB: Memory usage in megabytes
//
// # Examples
//
//	mock := NewMockDiagnosticsMetrics()
//	mock.RecordContainerMetrics("rag-engine", 45.5, 2048)
//
// # Limitations
//
//   - Default is a no-op
//
// # Assumptions
//
//   - RecordContainerMetricsFunc handles recording if set
func (m *MockDiagnosticsMetrics) RecordContainerMetrics(containerName string, cpuPercent float64, memoryMB int64) {
	m.mu.Lock()
	m.containerMetricsCallCount++
	m.mu.Unlock()

	if m.RecordContainerMetricsFunc != nil {
		m.RecordContainerMetricsFunc(containerName, cpuPercent, memoryMB)
	}
}

// RecordPruned invokes RecordPrunedFunc and tracks the call.
//
// # Description
//
// Calls the configured RecordPrunedFunc if set, otherwise does nothing.
//
// # Inputs
//
//   - count: Number of diagnostics pruned
//
// # Examples
//
//	mock := NewMockDiagnosticsMetrics()
//	mock.RecordPruned(5)
//
// # Limitations
//
//   - Default is a no-op
//
// # Assumptions
//
//   - RecordPrunedFunc handles recording if set
func (m *MockDiagnosticsMetrics) RecordPruned(count int) {
	m.mu.Lock()
	m.prunedCallCount++
	m.mu.Unlock()

	if m.RecordPrunedFunc != nil {
		m.RecordPrunedFunc(count)
	}
}

// RecordStoredCount invokes RecordStoredCountFunc and tracks the call.
//
// # Description
//
// Calls the configured RecordStoredCountFunc if set, otherwise does nothing.
//
// # Inputs
//
//   - count: Current stored count
//
// # Examples
//
//	mock := NewMockDiagnosticsMetrics()
//	mock.RecordStoredCount(42)
//
// # Limitations
//
//   - Default is a no-op
//
// # Assumptions
//
//   - RecordStoredCountFunc handles recording if set
func (m *MockDiagnosticsMetrics) RecordStoredCount(count int) {
	m.mu.Lock()
	m.storedCountCallCount++
	m.mu.Unlock()

	if m.RecordStoredCountFunc != nil {
		m.RecordStoredCountFunc(count)
	}
}

// Register invokes RegisterFunc and tracks the call.
//
// # Description
//
// Calls the configured RegisterFunc if set, otherwise returns nil.
//
// # Outputs
//
//   - error: Error from RegisterFunc, or nil
//
// # Examples
//
//	mock := NewMockDiagnosticsMetrics()
//	err := mock.Register()
//	assert.NoError(t, err)
//
// # Limitations
//
//   - Default returns nil (success)
//
// # Assumptions
//
//   - RegisterFunc handles registration if set
func (m *MockDiagnosticsMetrics) Register() error {
	m.mu.Lock()
	m.registerCallCount++
	m.mu.Unlock()

	if m.RegisterFunc != nil {
		return m.RegisterFunc()
	}
	return nil
}

// RecordCollectionCallCount returns the number of times RecordCollection was called.
//
// # Description
//
// Thread-safe accessor for verifying RecordCollection invocations.
//
// # Outputs
//
//   - int: Number of RecordCollection calls
//
// # Examples
//
//	mock.RecordCollection(SeverityInfo, "test", 100, 1024)
//	assert.Equal(t, 1, mock.RecordCollectionCallCount())
//
// # Limitations
//
//   - None
//
// # Assumptions
//
//   - None
func (m *MockDiagnosticsMetrics) RecordCollectionCallCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.collectionCallCount
}

// RecordErrorCallCount returns the number of times RecordError was called.
//
// # Description
//
// Thread-safe accessor for verifying RecordError invocations.
//
// # Outputs
//
//   - int: Number of RecordError calls
//
// # Examples
//
//	mock.RecordError("test_error")
//	assert.Equal(t, 1, mock.RecordErrorCallCount())
//
// # Limitations
//
//   - None
//
// # Assumptions
//
//   - None
func (m *MockDiagnosticsMetrics) RecordErrorCallCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.errorCallCount
}

// Reset clears all call counts.
//
// # Description
//
// Resets the mock to initial state for reuse.
//
// # Examples
//
//	mock.RecordCollection(SeverityInfo, "test", 100, 1024)
//	mock.Reset()
//	assert.Equal(t, 0, mock.RecordCollectionCallCount())
//
// # Limitations
//
//   - Does not reset function fields
//
// # Assumptions
//
//   - None
func (m *MockDiagnosticsMetrics) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.collectionCallCount = 0
	m.errorCallCount = 0
	m.containerHealthCallCount = 0
	m.containerMetricsCallCount = 0
	m.prunedCallCount = 0
	m.storedCountCallCount = 0
	m.registerCallCount = 0
}

// -----------------------------------------------------------------------------
// MockDiagnosticsViewer
// -----------------------------------------------------------------------------

// MockDiagnosticsViewer is a test double for DiagnosticsViewer.
//
// # Description
//
// Provides configurable behavior for DiagnosticsViewer in unit tests.
//
// # Thread Safety
//
// MockDiagnosticsViewer is safe for concurrent use.
type MockDiagnosticsViewer struct {
	// GetFunc is called by Get. Set this to customize behavior.
	GetFunc func(ctx context.Context, id string) (*DiagnosticsData, error)

	// ListFunc is called by List. Set this to customize behavior.
	ListFunc func(ctx context.Context, opts ListOptions) ([]DiagnosticsSummary, error)

	// GetByTraceIDFunc is called by GetByTraceID. Set this to customize behavior.
	GetByTraceIDFunc func(ctx context.Context, traceID string) (*DiagnosticsData, error)

	// mu protects call counts.
	mu sync.RWMutex

	// getCallCount tracks calls to Get.
	getCallCount int

	// listCallCount tracks calls to List.
	listCallCount int

	// getByTraceIDCallCount tracks calls to GetByTraceID.
	getByTraceIDCallCount int
}

// NewMockDiagnosticsViewer creates a mock viewer with defaults.
//
// # Description
//
// Creates a mock viewer that returns nil for all Get calls by default.
//
// # Outputs
//
//   - *MockDiagnosticsViewer: Ready-to-use mock
//
// # Examples
//
//	mock := NewMockDiagnosticsViewer()
//	mock.GetFunc = func(ctx context.Context, id string) (*DiagnosticsData, error) {
//	    return &DiagnosticsData{}, nil
//	}
//
// # Limitations
//
//   - None
//
// # Assumptions
//
//   - None
func NewMockDiagnosticsViewer() *MockDiagnosticsViewer {
	return &MockDiagnosticsViewer{}
}

// Get invokes GetFunc and tracks the call.
//
// # Description
//
// Calls the configured GetFunc if set, otherwise returns nil.
//
// # Inputs
//
//   - ctx: Context for cancellation
//   - id: Diagnostic ID or path
//
// # Outputs
//
//   - *DiagnosticsData: Data from GetFunc, or nil
//   - error: Error from GetFunc, or nil
//
// # Examples
//
//	mock := NewMockDiagnosticsViewer()
//	data, err := mock.Get(ctx, "diag-123")
//
// # Limitations
//
//   - Default returns nil, nil
//
// # Assumptions
//
//   - GetFunc handles retrieval if set
func (m *MockDiagnosticsViewer) Get(ctx context.Context, id string) (*DiagnosticsData, error) {
	m.mu.Lock()
	m.getCallCount++
	m.mu.Unlock()

	if m.GetFunc != nil {
		return m.GetFunc(ctx, id)
	}
	return nil, nil
}

// List invokes ListFunc and tracks the call.
//
// # Description
//
// Calls the configured ListFunc if set, otherwise returns empty slice.
//
// # Inputs
//
//   - ctx: Context for cancellation
//   - opts: Filter and pagination options
//
// # Outputs
//
//   - []DiagnosticsSummary: Summaries from ListFunc, or empty slice
//   - error: Error from ListFunc, or nil
//
// # Examples
//
//	mock := NewMockDiagnosticsViewer()
//	summaries, err := mock.List(ctx, ListOptions{Limit: 10})
//
// # Limitations
//
//   - Default returns empty slice
//
// # Assumptions
//
//   - ListFunc handles listing if set
func (m *MockDiagnosticsViewer) List(ctx context.Context, opts ListOptions) ([]DiagnosticsSummary, error) {
	m.mu.Lock()
	m.listCallCount++
	m.mu.Unlock()

	if m.ListFunc != nil {
		return m.ListFunc(ctx, opts)
	}
	return []DiagnosticsSummary{}, nil
}

// GetByTraceID invokes GetByTraceIDFunc and tracks the call.
//
// # Description
//
// Calls the configured GetByTraceIDFunc if set, otherwise returns nil.
//
// # Inputs
//
//   - ctx: Context for cancellation
//   - traceID: OpenTelemetry trace ID
//
// # Outputs
//
//   - *DiagnosticsData: Data from GetByTraceIDFunc, or nil
//   - error: Error from GetByTraceIDFunc, or nil
//
// # Examples
//
//	mock := NewMockDiagnosticsViewer()
//	data, err := mock.GetByTraceID(ctx, "abc123...")
//
// # Limitations
//
//   - Default returns nil, nil
//
// # Assumptions
//
//   - GetByTraceIDFunc handles lookup if set
func (m *MockDiagnosticsViewer) GetByTraceID(ctx context.Context, traceID string) (*DiagnosticsData, error) {
	m.mu.Lock()
	m.getByTraceIDCallCount++
	m.mu.Unlock()

	if m.GetByTraceIDFunc != nil {
		return m.GetByTraceIDFunc(ctx, traceID)
	}
	return nil, nil
}

// GetCallCount returns the number of times Get was called.
//
// # Description
//
// Thread-safe accessor for verifying Get invocations.
//
// # Outputs
//
//   - int: Number of Get calls
//
// # Examples
//
//	mock.Get(ctx, "id")
//	assert.Equal(t, 1, mock.GetCallCount())
//
// # Limitations
//
//   - None
//
// # Assumptions
//
//   - None
func (m *MockDiagnosticsViewer) GetCallCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.getCallCount
}

// ListCallCount returns the number of times List was called.
//
// # Description
//
// Thread-safe accessor for verifying List invocations.
//
// # Outputs
//
//   - int: Number of List calls
//
// # Examples
//
//	mock.List(ctx, ListOptions{})
//	assert.Equal(t, 1, mock.ListCallCount())
//
// # Limitations
//
//   - None
//
// # Assumptions
//
//   - None
func (m *MockDiagnosticsViewer) ListCallCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.listCallCount
}

// GetByTraceIDCallCount returns the number of times GetByTraceID was called.
//
// # Description
//
// Thread-safe accessor for verifying GetByTraceID invocations.
//
// # Outputs
//
//   - int: Number of GetByTraceID calls
//
// # Examples
//
//	mock.GetByTraceID(ctx, "trace-123")
//	assert.Equal(t, 1, mock.GetByTraceIDCallCount())
//
// # Limitations
//
//   - None
//
// # Assumptions
//
//   - None
func (m *MockDiagnosticsViewer) GetByTraceIDCallCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.getByTraceIDCallCount
}

// Reset clears all call counts.
//
// # Description
//
// Resets the mock to initial state for reuse.
//
// # Examples
//
//	mock.Get(ctx, "id")
//	mock.Reset()
//	assert.Equal(t, 0, mock.GetCallCount())
//
// # Limitations
//
//   - Does not reset function fields
//
// # Assumptions
//
//   - None
func (m *MockDiagnosticsViewer) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.getCallCount = 0
	m.listCallCount = 0
	m.getByTraceIDCallCount = 0
}

// -----------------------------------------------------------------------------
// MockDiagnosticsTracer
// -----------------------------------------------------------------------------

// MockDiagnosticsTracer is a test double for DiagnosticsTracer.
//
// # Description
//
// Provides configurable behavior for DiagnosticsTracer in unit tests.
// Tracks spans started and IDs generated for verification.
//
// # Thread Safety
//
// MockDiagnosticsTracer is safe for concurrent use.
type MockDiagnosticsTracer struct {
	// StartSpanFunc is called by StartSpan. Set this to customize behavior.
	StartSpanFunc func(ctx context.Context, name string, attrs map[string]string) (context.Context, func(error))

	// GetTraceIDFunc is called by GetTraceID. Set this to customize behavior.
	GetTraceIDFunc func(ctx context.Context) string

	// GetSpanIDFunc is called by GetSpanID. Set this to customize behavior.
	GetSpanIDFunc func(ctx context.Context) string

	// GenerateTraceIDFunc is called by GenerateTraceID. Set this to customize behavior.
	GenerateTraceIDFunc func() string

	// GenerateSpanIDFunc is called by GenerateSpanID. Set this to customize behavior.
	GenerateSpanIDFunc func() string

	// ShutdownFunc is called by Shutdown. Set this to customize behavior.
	ShutdownFunc func(ctx context.Context) error

	// mu protects call counts and state.
	mu sync.RWMutex

	// startSpanCallCount tracks calls to StartSpan.
	startSpanCallCount int

	// spanNames records names passed to StartSpan.
	spanNames []string

	// currentTraceID is the active trace ID.
	currentTraceID string

	// currentSpanID is the active span ID.
	currentSpanID string

	// traceIDCounter for generating sequential IDs.
	traceIDCounter int
}

// NewMockDiagnosticsTracer creates a mock tracer with defaults.
//
// # Description
//
// Creates a mock tracer that generates sequential trace/span IDs and
// tracks all span creations. Useful for deterministic testing.
//
// # Outputs
//
//   - *MockDiagnosticsTracer: Ready-to-use mock
//
// # Examples
//
//	mock := NewMockDiagnosticsTracer()
//	ctx, finish := mock.StartSpan(ctx, "test-span", nil)
//	defer finish(nil)
//	assert.Equal(t, 1, mock.StartSpanCallCount())
//
// # Limitations
//
//   - Generated IDs are not cryptographically random
//
// # Assumptions
//
//   - Deterministic IDs are acceptable for testing
func NewMockDiagnosticsTracer() *MockDiagnosticsTracer {
	return &MockDiagnosticsTracer{
		spanNames:      make([]string, 0),
		currentTraceID: "00000000000000000000000000000001",
		currentSpanID:  "0000000000000001",
	}
}

// StartSpan invokes StartSpanFunc and tracks the call.
//
// # Description
//
// Calls the configured StartSpanFunc if set, otherwise creates a mock span
// context and returns a no-op finish function.
//
// # Inputs
//
//   - ctx: Parent context
//   - name: Span name (recorded for verification)
//   - attrs: Span attributes
//
// # Outputs
//
//   - context.Context: Context with mock span data
//   - func(error): Finish function (no-op by default)
//
// # Examples
//
//	mock := NewMockDiagnosticsTracer()
//	ctx, finish := mock.StartSpan(ctx, "test-operation", map[string]string{"key": "value"})
//	defer finish(nil)
//
// # Limitations
//
//   - Default doesn't create real spans
//
// # Assumptions
//
//   - StartSpanFunc handles span creation if set
func (m *MockDiagnosticsTracer) StartSpan(ctx context.Context, name string, attrs map[string]string) (context.Context, func(error)) {
	m.mu.Lock()
	m.startSpanCallCount++
	m.spanNames = append(m.spanNames, name)
	m.mu.Unlock()

	if m.StartSpanFunc != nil {
		return m.StartSpanFunc(ctx, name, attrs)
	}

	// Default: store trace/span ID in context using mock keys
	m.mu.RLock()
	traceID := m.currentTraceID
	spanID := m.currentSpanID
	m.mu.RUnlock()

	ctx = context.WithValue(ctx, mockTraceIDKey{}, traceID)
	ctx = context.WithValue(ctx, mockSpanIDKey{}, spanID)

	return ctx, func(err error) {
		// No-op finish function
	}
}

// GetTraceID invokes GetTraceIDFunc or returns from context.
//
// # Description
//
// Calls the configured GetTraceIDFunc if set, otherwise extracts from context.
//
// # Inputs
//
//   - ctx: Context potentially containing trace ID
//
// # Outputs
//
//   - string: Trace ID from GetTraceIDFunc or context
//
// # Examples
//
//	ctx, _ := mock.StartSpan(ctx, "span", nil)
//	traceID := mock.GetTraceID(ctx)
//
// # Limitations
//
//   - Returns empty if no trace in context
//
// # Assumptions
//
//   - GetTraceIDFunc handles extraction if set
func (m *MockDiagnosticsTracer) GetTraceID(ctx context.Context) string {
	if m.GetTraceIDFunc != nil {
		return m.GetTraceIDFunc(ctx)
	}
	if id, ok := ctx.Value(mockTraceIDKey{}).(string); ok {
		return id
	}
	return ""
}

// GetSpanID invokes GetSpanIDFunc or returns from context.
//
// # Description
//
// Calls the configured GetSpanIDFunc if set, otherwise extracts from context.
//
// # Inputs
//
//   - ctx: Context potentially containing span ID
//
// # Outputs
//
//   - string: Span ID from GetSpanIDFunc or context
//
// # Examples
//
//	ctx, _ := mock.StartSpan(ctx, "span", nil)
//	spanID := mock.GetSpanID(ctx)
//
// # Limitations
//
//   - Returns empty if no span in context
//
// # Assumptions
//
//   - GetSpanIDFunc handles extraction if set
func (m *MockDiagnosticsTracer) GetSpanID(ctx context.Context) string {
	if m.GetSpanIDFunc != nil {
		return m.GetSpanIDFunc(ctx)
	}
	if id, ok := ctx.Value(mockSpanIDKey{}).(string); ok {
		return id
	}
	return ""
}

// GenerateTraceID invokes GenerateTraceIDFunc or generates sequential ID.
//
// # Description
//
// Calls the configured GenerateTraceIDFunc if set, otherwise generates
// a sequential 32-character hex ID for deterministic testing.
//
// # Outputs
//
//   - string: 32-character hex trace ID
//
// # Examples
//
//	id1 := mock.GenerateTraceID() // "00000000000000000000000000000001"
//	id2 := mock.GenerateTraceID() // "00000000000000000000000000000002"
//
// # Limitations
//
//   - Sequential IDs, not random
//
// # Assumptions
//
//   - GenerateTraceIDFunc handles generation if set
func (m *MockDiagnosticsTracer) GenerateTraceID() string {
	if m.GenerateTraceIDFunc != nil {
		return m.GenerateTraceIDFunc()
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.traceIDCounter++
	return formatMockTraceID(m.traceIDCounter)
}

// GenerateSpanID invokes GenerateSpanIDFunc or generates sequential ID.
//
// # Description
//
// Calls the configured GenerateSpanIDFunc if set, otherwise generates
// a sequential 16-character hex ID for deterministic testing.
//
// # Outputs
//
//   - string: 16-character hex span ID
//
// # Examples
//
//	id := mock.GenerateSpanID() // "0000000000000001"
//
// # Limitations
//
//   - Sequential IDs, not random
//
// # Assumptions
//
//   - GenerateSpanIDFunc handles generation if set
func (m *MockDiagnosticsTracer) GenerateSpanID() string {
	if m.GenerateSpanIDFunc != nil {
		return m.GenerateSpanIDFunc()
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.traceIDCounter++
	return formatMockSpanID(m.traceIDCounter)
}

// Shutdown invokes ShutdownFunc and returns.
//
// # Description
//
// Calls the configured ShutdownFunc if set, otherwise returns nil.
//
// # Inputs
//
//   - ctx: Context for timeout control
//
// # Outputs
//
//   - error: Error from ShutdownFunc, or nil
//
// # Examples
//
//	err := mock.Shutdown(ctx)
//	assert.NoError(t, err)
//
// # Limitations
//
//   - Default returns nil
//
// # Assumptions
//
//   - ShutdownFunc handles shutdown if set
func (m *MockDiagnosticsTracer) Shutdown(ctx context.Context) error {
	if m.ShutdownFunc != nil {
		return m.ShutdownFunc(ctx)
	}
	return nil
}

// StartSpanCallCount returns the number of times StartSpan was called.
//
// # Description
//
// Thread-safe accessor for verifying StartSpan invocations.
//
// # Outputs
//
//   - int: Number of StartSpan calls
//
// # Examples
//
//	mock.StartSpan(ctx, "span1", nil)
//	mock.StartSpan(ctx, "span2", nil)
//	assert.Equal(t, 2, mock.StartSpanCallCount())
//
// # Limitations
//
//   - None
//
// # Assumptions
//
//   - None
func (m *MockDiagnosticsTracer) StartSpanCallCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.startSpanCallCount
}

// SpanNameAt returns the span name from a specific StartSpan call.
//
// # Description
//
// Retrieves the recorded span name for verification.
//
// # Inputs
//
//   - index: Call index (0-based)
//
// # Outputs
//
//   - string: Recorded span name
//   - bool: True if index was valid
//
// # Examples
//
//	mock.StartSpan(ctx, "my-operation", nil)
//	name, ok := mock.SpanNameAt(0)
//	assert.Equal(t, "my-operation", name)
//
// # Limitations
//
//   - Returns "", false if index out of bounds
//
// # Assumptions
//
//   - Index is valid
func (m *MockDiagnosticsTracer) SpanNameAt(index int) (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if index < 0 || index >= len(m.spanNames) {
		return "", false
	}
	return m.spanNames[index], true
}

// SetTraceID sets the trace ID returned by GetTraceID and StartSpan.
//
// # Description
//
// Configures the trace ID for subsequent operations.
//
// # Inputs
//
//   - traceID: Trace ID to use
//
// # Examples
//
//	mock.SetTraceID("custom-trace-id-12345678901234")
//	ctx, _ := mock.StartSpan(ctx, "span", nil)
//	assert.Equal(t, "custom-trace-id-12345678901234", mock.GetTraceID(ctx))
//
// # Limitations
//
//   - None
//
// # Assumptions
//
//   - Trace ID is valid 32-character hex string
func (m *MockDiagnosticsTracer) SetTraceID(traceID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.currentTraceID = traceID
}

// SetSpanID sets the span ID returned by GetSpanID and StartSpan.
//
// # Description
//
// Configures the span ID for subsequent operations.
//
// # Inputs
//
//   - spanID: Span ID to use
//
// # Examples
//
//	mock.SetSpanID("custom-span-1234")
//	ctx, _ := mock.StartSpan(ctx, "span", nil)
//	assert.Equal(t, "custom-span-1234", mock.GetSpanID(ctx))
//
// # Limitations
//
//   - None
//
// # Assumptions
//
//   - Span ID is valid 16-character hex string
func (m *MockDiagnosticsTracer) SetSpanID(spanID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.currentSpanID = spanID
}

// Reset clears all call counts and state.
//
// # Description
//
// Resets the mock to initial state for reuse.
//
// # Examples
//
//	mock.StartSpan(ctx, "span", nil)
//	mock.Reset()
//	assert.Equal(t, 0, mock.StartSpanCallCount())
//
// # Limitations
//
//   - Does not reset function fields
//
// # Assumptions
//
//   - None
func (m *MockDiagnosticsTracer) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.startSpanCallCount = 0
	m.spanNames = make([]string, 0)
	m.currentTraceID = "00000000000000000000000000000001"
	m.currentSpanID = "0000000000000001"
	m.traceIDCounter = 0
}

// Context keys for mock tracer.
type mockTraceIDKey struct{}
type mockSpanIDKey struct{}

// formatMockTraceID formats an integer as a 32-character hex trace ID.
//
// # Description
//
// Creates a deterministic trace ID from a counter value.
//
// # Inputs
//
//   - n: Counter value
//
// # Outputs
//
//   - string: 32-character hex trace ID
//
// # Examples
//
//	id := formatMockTraceID(1) // "00000000000000000000000000000001"
//
// # Limitations
//
//   - Not cryptographically random
//
// # Assumptions
//
//   - Used only for testing
func formatMockTraceID(n int) string {
	return formatHexID(n, 32)
}

// formatMockSpanID formats an integer as a 16-character hex span ID.
//
// # Description
//
// Creates a deterministic span ID from a counter value.
//
// # Inputs
//
//   - n: Counter value
//
// # Outputs
//
//   - string: 16-character hex span ID
//
// # Examples
//
//	id := formatMockSpanID(1) // "0000000000000001"
//
// # Limitations
//
//   - Not cryptographically random
//
// # Assumptions
//
//   - Used only for testing
func formatMockSpanID(n int) string {
	return formatHexID(n, 16)
}

// formatHexID formats an integer as a fixed-length hex string.
//
// # Description
//
// Creates a zero-padded hex string of the specified length.
//
// # Inputs
//
//   - n: Counter value to format
//   - length: Desired string length
//
// # Outputs
//
//   - string: Zero-padded hex string
//
// # Examples
//
//	id := formatHexID(255, 8) // "000000ff"
//
// # Limitations
//
//   - Truncates if value is too large for length
//
// # Assumptions
//
//   - Length is sufficient for value
func formatHexID(n int, length int) string {
	return truncateOrPad(formatInt64Hex(int64(n)), length)
}

// formatInt64Hex formats an int64 as hexadecimal.
//
// # Description
//
// Simple hex conversion helper.
//
// # Inputs
//
//   - n: Value to format
//
// # Outputs
//
//   - string: Hex representation
//
// # Examples
//
//	s := formatInt64Hex(255) // "ff"
//
// # Limitations
//
//   - None
//
// # Assumptions
//
//   - None
func formatInt64Hex(n int64) string {
	const hexDigits = "0123456789abcdef"
	if n == 0 {
		return "0"
	}
	result := make([]byte, 0, 16)
	for n > 0 {
		result = append([]byte{hexDigits[n%16]}, result...)
		n /= 16
	}
	return string(result)
}

// truncateOrPad ensures a string is exactly the specified length.
//
// # Description
//
// Zero-pads or truncates a string to the exact length.
//
// # Inputs
//
//   - s: Input string
//   - length: Desired length
//
// # Outputs
//
//   - string: String of exact length
//
// # Examples
//
//	s := truncateOrPad("ff", 4) // "00ff"
//
// # Limitations
//
//   - Truncates from the left if too long
//
// # Assumptions
//
//   - None
func truncateOrPad(s string, length int) string {
	if len(s) >= length {
		return s[len(s)-length:]
	}
	padding := make([]byte, length-len(s))
	for i := range padding {
		padding[i] = '0'
	}
	return string(padding) + s
}

// -----------------------------------------------------------------------------
// MockPanicRecoveryHandler
// -----------------------------------------------------------------------------

// MockPanicRecoveryHandler is a test double for PanicRecoveryHandler.
//
// # Description
//
// Provides configurable behavior for PanicRecoveryHandler in unit tests.
// Does NOT actually recover panics by default to maintain test clarity.
//
// # Thread Safety
//
// MockPanicRecoveryHandler is safe for concurrent use.
type MockPanicRecoveryHandler struct {
	// WrapFunc is called by Wrap. Set this to customize behavior.
	WrapFunc func() func()

	// SetCollectorFunc is called by SetCollector. Set this to customize behavior.
	SetCollectorFunc func(collector DiagnosticsCollector)

	// GetLastPanicResultFunc is called by GetLastPanicResult. Set this to customize behavior.
	GetLastPanicResultFunc func() *DiagnosticsResult

	// mu protects call counts and state.
	mu sync.RWMutex

	// wrapCallCount tracks calls to Wrap.
	wrapCallCount int

	// setCollectorCallCount tracks calls to SetCollector.
	setCollectorCallCount int

	// getLastPanicResultCallCount tracks calls to GetLastPanicResult.
	getLastPanicResultCallCount int

	// lastResult stores the configured result.
	lastResult *DiagnosticsResult
}

// NewMockPanicRecoveryHandler creates a mock panic handler with defaults.
//
// # Description
//
// Creates a mock panic handler that returns a no-op wrapper by default.
// Does NOT capture real panics unless WrapFunc is configured.
//
// # Outputs
//
//   - *MockPanicRecoveryHandler: Ready-to-use mock
//
// # Examples
//
//	mock := NewMockPanicRecoveryHandler()
//	defer mock.Wrap()()
//	// Panic will NOT be captured by default
//
// # Limitations
//
//   - Default Wrap does NOT capture panics
//
// # Assumptions
//
//   - Tests will configure WrapFunc if panic capture is needed
func NewMockPanicRecoveryHandler() *MockPanicRecoveryHandler {
	return &MockPanicRecoveryHandler{}
}

// Wrap invokes WrapFunc and tracks the call.
//
// # Description
//
// Calls the configured WrapFunc if set, otherwise returns a no-op function.
// The returned function does NOT recover panics by default.
//
// # Outputs
//
//   - func(): Closure to defer (no-op by default)
//
// # Examples
//
//	mock := NewMockPanicRecoveryHandler()
//	defer mock.Wrap()()
//	assert.Equal(t, 1, mock.WrapCallCount())
//
// # Limitations
//
//   - Default does NOT capture panics
//
// # Assumptions
//
//   - WrapFunc handles recovery if set
func (m *MockPanicRecoveryHandler) Wrap() func() {
	m.mu.Lock()
	m.wrapCallCount++
	m.mu.Unlock()

	if m.WrapFunc != nil {
		return m.WrapFunc()
	}

	// Default: no-op that doesn't recover
	return func() {}
}

// SetCollector invokes SetCollectorFunc and tracks the call.
//
// # Description
//
// Calls the configured SetCollectorFunc if set, otherwise does nothing.
//
// # Inputs
//
//   - collector: Collector to configure
//
// # Examples
//
//	mock := NewMockPanicRecoveryHandler()
//	mock.SetCollector(realCollector)
//	assert.Equal(t, 1, mock.SetCollectorCallCount())
//
// # Limitations
//
//   - Default is a no-op
//
// # Assumptions
//
//   - SetCollectorFunc handles collector if set
func (m *MockPanicRecoveryHandler) SetCollector(collector DiagnosticsCollector) {
	m.mu.Lock()
	m.setCollectorCallCount++
	m.mu.Unlock()

	if m.SetCollectorFunc != nil {
		m.SetCollectorFunc(collector)
	}
}

// GetLastPanicResult invokes GetLastPanicResultFunc or returns configured result.
//
// # Description
//
// Calls the configured GetLastPanicResultFunc if set, otherwise returns
// the result configured via SetLastPanicResult.
//
// # Outputs
//
//   - *DiagnosticsResult: Configured result, or nil
//
// # Examples
//
//	mock := NewMockPanicRecoveryHandler()
//	mock.SetLastPanicResult(&DiagnosticsResult{TraceID: "panic-trace"})
//	result := mock.GetLastPanicResult()
//	assert.Equal(t, "panic-trace", result.TraceID)
//
// # Limitations
//
//   - Default returns nil
//
// # Assumptions
//
//   - GetLastPanicResultFunc handles result if set
func (m *MockPanicRecoveryHandler) GetLastPanicResult() *DiagnosticsResult {
	m.mu.Lock()
	m.getLastPanicResultCallCount++
	m.mu.Unlock()

	if m.GetLastPanicResultFunc != nil {
		return m.GetLastPanicResultFunc()
	}

	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.lastResult
}

// SetLastPanicResult configures the result returned by GetLastPanicResult.
//
// # Description
//
// Sets the result that GetLastPanicResult will return (when GetLastPanicResultFunc
// is not set).
//
// # Inputs
//
//   - result: Result to return
//
// # Examples
//
//	mock := NewMockPanicRecoveryHandler()
//	mock.SetLastPanicResult(&DiagnosticsResult{
//	    TraceID:  "test-trace-123",
//	    Location: "/path/to/diagnostic.json",
//	})
//
// # Limitations
//
//   - Ignored if GetLastPanicResultFunc is set
//
// # Assumptions
//
//   - None
func (m *MockPanicRecoveryHandler) SetLastPanicResult(result *DiagnosticsResult) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastResult = result
}

// WrapCallCount returns the number of times Wrap was called.
//
// # Description
//
// Thread-safe accessor for verifying Wrap invocations.
//
// # Outputs
//
//   - int: Number of Wrap calls
//
// # Examples
//
//	mock.Wrap()
//	assert.Equal(t, 1, mock.WrapCallCount())
//
// # Limitations
//
//   - None
//
// # Assumptions
//
//   - None
func (m *MockPanicRecoveryHandler) WrapCallCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.wrapCallCount
}

// SetCollectorCallCount returns the number of times SetCollector was called.
//
// # Description
//
// Thread-safe accessor for verifying SetCollector invocations.
//
// # Outputs
//
//   - int: Number of SetCollector calls
//
// # Examples
//
//	mock.SetCollector(collector)
//	assert.Equal(t, 1, mock.SetCollectorCallCount())
//
// # Limitations
//
//   - None
//
// # Assumptions
//
//   - None
func (m *MockPanicRecoveryHandler) SetCollectorCallCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.setCollectorCallCount
}

// GetLastPanicResultCallCount returns the number of times GetLastPanicResult was called.
//
// # Description
//
// Thread-safe accessor for verifying GetLastPanicResult invocations.
//
// # Outputs
//
//   - int: Number of GetLastPanicResult calls
//
// # Examples
//
//	mock.GetLastPanicResult()
//	assert.Equal(t, 1, mock.GetLastPanicResultCallCount())
//
// # Limitations
//
//   - None
//
// # Assumptions
//
//   - None
func (m *MockPanicRecoveryHandler) GetLastPanicResultCallCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.getLastPanicResultCallCount
}

// Reset clears all call counts and state.
//
// # Description
//
// Resets the mock to initial state for reuse.
//
// # Examples
//
//	mock.Wrap()
//	mock.Reset()
//	assert.Equal(t, 0, mock.WrapCallCount())
//
// # Limitations
//
//   - Does not reset function fields
//
// # Assumptions
//
//   - None
func (m *MockPanicRecoveryHandler) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.wrapCallCount = 0
	m.setCollectorCallCount = 0
	m.getLastPanicResultCallCount = 0
	m.lastResult = nil
}

// -----------------------------------------------------------------------------
// Compile-time Interface Compliance Checks
// -----------------------------------------------------------------------------

var _ DiagnosticsCollector = (*MockDiagnosticsCollector)(nil)
var _ DiagnosticsFormatter = (*MockDiagnosticsFormatter)(nil)
var _ DiagnosticsStorage = (*MockDiagnosticsStorage)(nil)
var _ DiagnosticsMetrics = (*MockDiagnosticsMetrics)(nil)
var _ DiagnosticsViewer = (*MockDiagnosticsViewer)(nil)
var _ DiagnosticsTracer = (*MockDiagnosticsTracer)(nil)
var _ PanicRecoveryHandler = (*MockPanicRecoveryHandler)(nil)
