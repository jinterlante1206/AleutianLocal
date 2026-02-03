// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package regression

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// -----------------------------------------------------------------------------
// Errors
// -----------------------------------------------------------------------------

var (
	// ErrBaselineNotFound indicates no baseline exists for the component.
	ErrBaselineNotFound = errors.New("baseline not found")

	// ErrInvalidBaseline indicates the baseline data is corrupted.
	ErrInvalidBaseline = errors.New("invalid baseline data")
)

// -----------------------------------------------------------------------------
// Baseline Interface
// -----------------------------------------------------------------------------

// Baseline stores and retrieves performance baselines.
//
// Thread Safety: Implementations must be safe for concurrent use.
type Baseline interface {
	// Get retrieves the baseline for a component.
	// Returns ErrBaselineNotFound if no baseline exists.
	Get(ctx context.Context, component string) (*BaselineData, error)

	// Set stores a new baseline for a component.
	Set(ctx context.Context, component string, data *BaselineData) error

	// List returns all available baseline names.
	List(ctx context.Context) ([]string, error)

	// Delete removes a baseline.
	Delete(ctx context.Context, component string) error
}

// BaselineData holds the performance metrics for a baseline.
type BaselineData struct {
	// Component is the name of the component.
	Component string `json:"component"`

	// Version identifies this baseline version.
	Version string `json:"version"`

	// CreatedAt is when the baseline was created.
	CreatedAt time.Time `json:"created_at"`

	// UpdatedAt is when the baseline was last updated.
	UpdatedAt time.Time `json:"updated_at"`

	// Latency holds latency metrics.
	Latency LatencyBaseline `json:"latency"`

	// Throughput holds throughput metrics.
	Throughput ThroughputBaseline `json:"throughput"`

	// Memory holds memory metrics.
	Memory MemoryBaseline `json:"memory"`

	// Error holds error rate metrics.
	Error ErrorBaseline `json:"error"`

	// SampleCount is the number of samples in this baseline.
	SampleCount int `json:"sample_count"`

	// Metadata holds arbitrary additional data.
	Metadata map[string]string `json:"metadata,omitempty"`
}

// LatencyBaseline holds latency performance metrics.
type LatencyBaseline struct {
	// P50 is the 50th percentile latency.
	P50 time.Duration `json:"p50"`

	// P95 is the 95th percentile latency.
	P95 time.Duration `json:"p95"`

	// P99 is the 99th percentile latency.
	P99 time.Duration `json:"p99"`

	// Mean is the average latency.
	Mean time.Duration `json:"mean"`

	// StdDev is the standard deviation.
	StdDev time.Duration `json:"std_dev"`
}

// ThroughputBaseline holds throughput metrics.
type ThroughputBaseline struct {
	// OpsPerSecond is operations per second.
	OpsPerSecond float64 `json:"ops_per_second"`

	// BytesPerSecond is throughput in bytes.
	BytesPerSecond float64 `json:"bytes_per_second,omitempty"`
}

// MemoryBaseline holds memory usage metrics.
type MemoryBaseline struct {
	// AllocBytesPerOp is bytes allocated per operation.
	AllocBytesPerOp uint64 `json:"alloc_bytes_per_op"`

	// AllocsPerOp is number of allocations per operation.
	AllocsPerOp uint64 `json:"allocs_per_op"`

	// HeapInUse is the typical heap usage.
	HeapInUse uint64 `json:"heap_in_use,omitempty"`
}

// ErrorBaseline holds error rate metrics.
type ErrorBaseline struct {
	// Rate is the error rate (0 to 1).
	Rate float64 `json:"rate"`

	// Count is the typical error count.
	Count int64 `json:"count,omitempty"`
}

// -----------------------------------------------------------------------------
// Memory Baseline (for testing)
// -----------------------------------------------------------------------------

// MemoryBaselineStore stores baselines in memory.
//
// Description:
//
//	MemoryBaselineStore is useful for testing and short-lived processes.
//	Data is lost when the process exits.
//
// Thread Safety: Safe for concurrent use.
type MemoryBaselineStore struct {
	mu   sync.RWMutex
	data map[string]*BaselineData
}

// NewMemoryBaseline creates a new memory-backed baseline store.
//
// Outputs:
//   - *MemoryBaselineStore: The new store. Never nil.
func NewMemoryBaseline() *MemoryBaselineStore {
	return &MemoryBaselineStore{
		data: make(map[string]*BaselineData),
	}
}

// Get implements Baseline.
func (m *MemoryBaselineStore) Get(_ context.Context, component string) (*BaselineData, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	data, ok := m.data[component]
	if !ok {
		return nil, ErrBaselineNotFound
	}

	// Return a copy to prevent mutation
	dataCopy := *data
	return &dataCopy, nil
}

// Set implements Baseline.
func (m *MemoryBaselineStore) Set(_ context.Context, component string, data *BaselineData) error {
	if data == nil {
		return errors.New("baseline data must not be nil")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Store a copy
	dataCopy := *data
	dataCopy.UpdatedAt = time.Now()
	if dataCopy.CreatedAt.IsZero() {
		dataCopy.CreatedAt = dataCopy.UpdatedAt
	}
	m.data[component] = &dataCopy
	return nil
}

// List implements Baseline.
func (m *MemoryBaselineStore) List(_ context.Context) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	names := make([]string, 0, len(m.data))
	for name := range m.data {
		names = append(names, name)
	}
	return names, nil
}

// Delete implements Baseline.
func (m *MemoryBaselineStore) Delete(_ context.Context, component string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.data[component]; !ok {
		return ErrBaselineNotFound
	}
	delete(m.data, component)
	return nil
}

// -----------------------------------------------------------------------------
// File Baseline
// -----------------------------------------------------------------------------

// FileBaselineStore stores baselines in JSON files.
//
// Description:
//
//	FileBaselineStore persists baselines to disk as JSON files.
//	Each component gets its own file: {dir}/{component}.json
//
// Thread Safety: Safe for concurrent use.
type FileBaselineStore struct {
	dir string
	mu  sync.RWMutex
}

// NewFileBaseline creates a file-backed baseline store.
//
// Inputs:
//   - dir: Directory to store baseline files. Created if not exists.
//
// Outputs:
//   - *FileBaselineStore: The new store. Never nil.
//   - error: Non-nil if directory cannot be created.
func NewFileBaseline(dir string) (*FileBaselineStore, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}
	return &FileBaselineStore{dir: dir}, nil
}

// Get implements Baseline.
func (f *FileBaselineStore) Get(_ context.Context, component string) (*BaselineData, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	path := f.filePath(component)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrBaselineNotFound
		}
		return nil, err
	}

	var baseline BaselineData
	if err := json.Unmarshal(data, &baseline); err != nil {
		return nil, ErrInvalidBaseline
	}

	return &baseline, nil
}

// Set implements Baseline.
func (f *FileBaselineStore) Set(_ context.Context, component string, data *BaselineData) error {
	if data == nil {
		return errors.New("baseline data must not be nil")
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	data.UpdatedAt = time.Now()
	if data.CreatedAt.IsZero() {
		data.CreatedAt = data.UpdatedAt
	}

	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(f.filePath(component), jsonData, 0644)
}

// List implements Baseline.
func (f *FileBaselineStore) List(_ context.Context) ([]string, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	entries, err := os.ReadDir(f.dir)
	if err != nil {
		return nil, err
	}

	var names []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if filepath.Ext(name) == ".json" {
			names = append(names, name[:len(name)-5])
		}
	}
	return names, nil
}

// Delete implements Baseline.
func (f *FileBaselineStore) Delete(_ context.Context, component string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	path := f.filePath(component)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return ErrBaselineNotFound
	}
	return os.Remove(path)
}

// filePath returns the file path for a component.
func (f *FileBaselineStore) filePath(component string) string {
	return filepath.Join(f.dir, component+".json")
}

// -----------------------------------------------------------------------------
// Baseline Builder
// -----------------------------------------------------------------------------

// BaselineBuilder helps construct BaselineData from benchmark results.
type BaselineBuilder struct {
	data *BaselineData
}

// NewBaselineBuilder creates a new baseline builder.
//
// Inputs:
//   - component: Component name.
//   - version: Version string.
//
// Outputs:
//   - *BaselineBuilder: The new builder. Never nil.
func NewBaselineBuilder(component, version string) *BaselineBuilder {
	return &BaselineBuilder{
		data: &BaselineData{
			Component: component,
			Version:   version,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
			Metadata:  make(map[string]string),
		},
	}
}

// WithLatency sets latency metrics.
func (b *BaselineBuilder) WithLatency(p50, p95, p99, mean, stdDev time.Duration) *BaselineBuilder {
	b.data.Latency = LatencyBaseline{
		P50:    p50,
		P95:    p95,
		P99:    p99,
		Mean:   mean,
		StdDev: stdDev,
	}
	return b
}

// WithThroughput sets throughput metrics.
func (b *BaselineBuilder) WithThroughput(opsPerSecond, bytesPerSecond float64) *BaselineBuilder {
	b.data.Throughput = ThroughputBaseline{
		OpsPerSecond:   opsPerSecond,
		BytesPerSecond: bytesPerSecond,
	}
	return b
}

// WithMemory sets memory metrics.
func (b *BaselineBuilder) WithMemory(allocBytesPerOp, allocsPerOp, heapInUse uint64) *BaselineBuilder {
	b.data.Memory = MemoryBaseline{
		AllocBytesPerOp: allocBytesPerOp,
		AllocsPerOp:     allocsPerOp,
		HeapInUse:       heapInUse,
	}
	return b
}

// WithErrorRate sets error metrics.
func (b *BaselineBuilder) WithErrorRate(rate float64) *BaselineBuilder {
	b.data.Error = ErrorBaseline{Rate: rate}
	return b
}

// WithSampleCount sets the sample count.
func (b *BaselineBuilder) WithSampleCount(count int) *BaselineBuilder {
	b.data.SampleCount = count
	return b
}

// WithMetadata adds metadata.
func (b *BaselineBuilder) WithMetadata(key, value string) *BaselineBuilder {
	b.data.Metadata[key] = value
	return b
}

// Build returns the constructed baseline.
func (b *BaselineBuilder) Build() *BaselineData {
	return b.data
}
