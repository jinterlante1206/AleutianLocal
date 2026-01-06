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
Package main provides MetricsStore for ephemeral metric storage.

MetricsStore enables metric trend analysis without requiring external
infrastructure like Prometheus. It stores metrics in a rolling window
with optional JSONL persistence for historical queries.

# Design Rationale

Health intelligence needs historical data to detect trends, but we don't
want to require Prometheus/Grafana for the local use case. By providing
a lightweight in-memory store with optional JSONL persistence, we can:
  - Track metrics without external dependencies
  - Compute baselines for anomaly detection
  - Persist data across restarts when needed
  - Avoid SQL injection attack vectors by using JSONL

# Security

JSONL files are human-readable and can be encrypted at rest using the
system's encryption facilities. Unlike SQLite, there are no query
injection vulnerabilities to consider.
*/
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// =============================================================================
// INTERFACE DEFINITIONS
// =============================================================================

// MetricsStore provides ephemeral metric storage with optional persistence.
//
// # Description
//
// This interface enables metric trend analysis without requiring external
// infrastructure like Prometheus. Metrics are stored in a rolling window
// with optional JSONL persistence for historical queries.
//
// # Thread Safety
//
// Implementations must be safe for concurrent use from multiple goroutines.
//
// # Examples
//
//	store, _ := NewEphemeralMetricsStore(DefaultMetricsStoreConfig("/path/to/stack"))
//	defer store.Close()
//	store.Record("orchestrator", "latency_p99", 150.0, time.Now())
//	points := store.Query("orchestrator", "latency_p99", start, end)
//
// # Limitations
//
//   - In-memory data is lost on restart (unless persistence enabled)
//   - Query performance degrades with many data points
//
// # Assumptions
//
//   - Callers provide valid metric names
//   - Timestamps are in UTC or consistent timezone
type MetricsStore interface {
	// Record stores a metric data point.
	//
	// # Description
	//
	// Adds a metric value to the store. In-memory storage has a rolling
	// window; older points are pruned automatically.
	//
	// # Inputs
	//
	//   - service: Service name (e.g., "orchestrator")
	//   - metric: Metric name (e.g., "latency_p99", "error_rate")
	//   - value: Metric value as float64
	//   - timestamp: When the metric was observed
	//
	// # Outputs
	//
	//   - None
	//
	// # Examples
	//
	//   store.Record("orchestrator", "latency_p99", 150.0, time.Now())
	//
	// # Limitations
	//
	//   - Very high write rates may cause contention
	//
	// # Assumptions
	//
	//   - Metric name is non-empty
	Record(service, metric string, value float64, timestamp time.Time)

	// Query returns metrics within a time window.
	//
	// # Description
	//
	// Returns all data points for the specified service and metric
	// within the given time range. Points are sorted by timestamp.
	//
	// # Inputs
	//
	//   - service: Service name
	//   - metric: Metric name
	//   - start, end: Time range (inclusive)
	//
	// # Outputs
	//
	//   - []MetricPoint: Data points in range, sorted by time
	//
	// # Examples
	//
	//   points := store.Query("orchestrator", "latency_p99",
	//       time.Now().Add(-1*time.Hour), time.Now())
	//
	// # Limitations
	//
	//   - Returns empty slice if no data in range
	//   - Large ranges may be slow
	//
	// # Assumptions
	//
	//   - start <= end
	Query(service, metric string, start, end time.Time) []MetricPoint

	// GetBaseline returns baseline statistics for comparison.
	//
	// # Description
	//
	// Computes statistical baseline (p50, p99, mean, stddev) from
	// historical data within the specified window. Used for trend
	// detection and anomaly alerting.
	//
	// # Inputs
	//
	//   - service: Service name
	//   - metric: Metric name
	//   - window: How far back to calculate baseline
	//
	// # Outputs
	//
	//   - *BaselineStats: Statistical baseline, or nil if insufficient data
	//
	// # Examples
	//
	//   baseline := store.GetBaseline("orchestrator", "latency_p99", 24*time.Hour)
	//   if baseline != nil && current > baseline.P99*1.5 {
	//       fmt.Println("Warning: latency 50% above baseline")
	//   }
	//
	// # Limitations
	//
	//   - Returns nil if fewer than 2 data points
	//   - Baseline quality depends on data availability
	//
	// # Assumptions
	//
	//   - Data is relatively uniform (not heavily skewed)
	GetBaseline(service, metric string, window time.Duration) *BaselineStats

	// Flush persists in-memory data to JSONL file (if configured).
	//
	// # Description
	//
	// Writes all in-memory data points to JSONL file storage. Called
	// periodically by background goroutine or manually for testing.
	//
	// # Inputs
	//
	//   - ctx: Context for cancellation
	//
	// # Outputs
	//
	//   - error: If persistence fails
	//
	// # Examples
	//
	//   if err := store.Flush(ctx); err != nil {
	//       log.Printf("Failed to flush metrics: %v", err)
	//   }
	//
	// # Limitations
	//
	//   - No-op if InMemoryOnly is true
	//
	// # Assumptions
	//
	//   - File path is writable
	Flush(ctx context.Context) error

	// Prune removes data older than retention period.
	//
	// # Description
	//
	// Removes data points older than the specified duration from both
	// in-memory and persistent storage.
	//
	// # Inputs
	//
	//   - olderThan: Maximum age of data to keep
	//
	// # Outputs
	//
	//   - int: Number of points removed
	//   - error: If pruning fails
	//
	// # Examples
	//
	//   removed, _ := store.Prune(24 * time.Hour)
	//   fmt.Printf("Pruned %d old data points\n", removed)
	//
	// # Limitations
	//
	//   - May be slow with large datasets
	//
	// # Assumptions
	//
	//   - None
	Prune(olderThan time.Duration) (int, error)

	// Close releases resources.
	//
	// # Description
	//
	// Flushes pending data and closes any open files.
	// Should be called when done using the store.
	//
	// # Inputs
	//
	//   - None
	//
	// # Outputs
	//
	//   - error: If close fails
	//
	// # Examples
	//
	//   defer store.Close()
	//
	// # Limitations
	//
	//   - None
	//
	// # Assumptions
	//
	//   - None
	Close() error
}

// =============================================================================
// STRUCT DEFINITIONS
// =============================================================================

// MetricPoint is a single metric observation.
//
// # Description
//
// Represents one data point with service, metric name, value, and timestamp.
// Used for queries and baseline calculations.
//
// # Examples
//
//	point := MetricPoint{
//	    ID:        GenerateID(),
//	    Service:   "orchestrator",
//	    Metric:    "latency_p99",
//	    Value:     150.0,
//	    Timestamp: time.Now(),
//	    CreatedAt: time.Now(),
//	}
//
// # Limitations
//
//   - Value is float64, may lose precision for very large integers
//
// # Assumptions
//
//   - ID is unique across all points
type MetricPoint struct {
	// ID is a unique identifier for this point.
	ID string `json:"id"`

	// Service is the service name.
	Service string `json:"service"`

	// Metric is the metric name.
	Metric string `json:"metric"`

	// Value is the metric value.
	Value float64 `json:"value"`

	// Timestamp is when the metric was observed.
	Timestamp time.Time `json:"timestamp"`

	// CreatedAt is when this point was stored.
	CreatedAt time.Time `json:"created_at"`
}

// BaselineStats provides statistical baseline for comparison.
//
// # Description
//
// Contains percentiles, mean, and standard deviation computed from
// historical data. Used for trend detection and anomaly alerting.
//
// # Examples
//
//	baseline := store.GetBaseline("svc", "latency", time.Hour)
//	if baseline != nil {
//	    fmt.Printf("P99: %f, Mean: %f\n", baseline.P99, baseline.Mean)
//	}
//
// # Limitations
//
//   - Requires at least 2 data points
//   - Percentiles are approximate for small datasets
//
// # Assumptions
//
//   - Data is numeric and finite
type BaselineStats struct {
	// ID is a unique identifier for this baseline.
	ID string

	// P50 is the 50th percentile (median).
	P50 float64

	// P99 is the 99th percentile.
	P99 float64

	// Mean is the arithmetic mean.
	Mean float64

	// StdDev is the standard deviation.
	StdDev float64

	// Min is the minimum value.
	Min float64

	// Max is the maximum value.
	Max float64

	// DataPoints is how many points were used.
	DataPoints int

	// WindowStart is when the baseline window begins.
	WindowStart time.Time

	// WindowEnd is when the baseline window ends.
	WindowEnd time.Time

	// CreatedAt is when this baseline was computed.
	CreatedAt time.Time
}

// MetricsStoreConfig configures the metrics store.
//
// # Description
//
// Controls in-memory buffer size, retention period, and optional
// JSONL persistence settings. Includes log rotation to prevent
// unbounded file growth and O(N) scan performance issues.
//
// # Examples
//
//	config := DefaultMetricsStoreConfig("/path/to/stack")
//	config.MaxPointsPerMetric = 2000 // Double the default
//
// # Limitations
//
//   - Large MaxPointsPerMetric values increase memory usage
//
// # Assumptions
//
//   - StackDir exists and is writable
type MetricsStoreConfig struct {
	// ID is a unique identifier for this config.
	ID string

	// InMemoryOnly disables JSONL persistence.
	InMemoryOnly bool

	// MaxPointsPerMetric limits in-memory buffer per service:metric pair.
	// Default: 1000 (enough for 1 point/second for ~16 minutes)
	MaxPointsPerMetric int

	// RetentionPeriod is how long to keep metrics in memory.
	// Default: 1 hour in-memory, 24 hours in JSONL
	RetentionPeriod time.Duration

	// JSONLPath is the JSONL file path for persistence.
	// Default: {stackDir}/health_metrics.jsonl
	JSONLPath string

	// FlushInterval is how often to persist to JSONL.
	// Default: 5 minutes
	FlushInterval time.Duration

	// MaxJSONLSize is the maximum file size in bytes before rotation.
	// Default: 10MB. Set to 0 to disable rotation.
	MaxJSONLSize int64

	// MaxRotatedFiles is how many rotated files to keep.
	// Default: 3 (keeps .1, .2, .3 backups)
	MaxRotatedFiles int

	// CreatedAt is when this config was created.
	CreatedAt time.Time
}

// EphemeralMetricsStore implements MetricsStore with in-memory + JSONL.
//
// # Description
//
// Provides a rolling window of metrics in memory with optional
// JSONL persistence for longer-term storage and survival across restarts.
//
// # Thread Safety
//
// Safe for concurrent use via RWMutex.
//
// # Examples
//
//	store, _ := NewEphemeralMetricsStore(config)
//	defer store.Close()
//	store.Record("svc", "metric", 100.0, time.Now())
//
// # Limitations
//
//   - JSONL file grows until pruned
//   - Large files may slow down load time
//
// # Assumptions
//
//   - File system is writable
type EphemeralMetricsStore struct {
	config    MetricsStoreConfig
	mu        sync.RWMutex
	inmemory  map[string][]MetricPoint // key: "service:metric"
	createdAt time.Time

	// Background flush control
	flushTicker *time.Ticker
	stopChan    chan struct{}
	wg          sync.WaitGroup
}

// MockMetricsStore is a test double for MetricsStore.
//
// # Description
//
// Allows tests to control metrics store behavior and verify calls.
//
// # Examples
//
//	mock := &MockMetricsStore{
//	    QueryResults: []MetricPoint{{Value: 100}},
//	}
//	points := mock.Query("svc", "metric", start, end)
//
// # Limitations
//
//   - Does not simulate all production behaviors
//
// # Assumptions
//
//   - Used only in tests
type MockMetricsStore struct {
	RecordedPoints []MetricPoint
	QueryResults   []MetricPoint
	BaselineResult *BaselineStats
	FlushError     error
	PruneCount     int
	PruneError     error
	CloseError     error

	mu sync.Mutex
}

// =============================================================================
// CONSTRUCTOR FUNCTIONS
// =============================================================================

// DefaultMetricsStoreConfig returns sensible defaults.
//
// # Description
//
// Creates a configuration with in-memory storage and optional JSONL
// persistence at {stackDir}/health_metrics.jsonl.
//
// # Inputs
//
//   - stackDir: Base directory for the Aleutian stack
//
// # Outputs
//
//   - MetricsStoreConfig: Default configuration
//
// # Examples
//
//	config := DefaultMetricsStoreConfig("/opt/aleutian")
//
// # Limitations
//
//   - None
//
// # Assumptions
//
//   - stackDir is a valid path
func DefaultMetricsStoreConfig(stackDir string) MetricsStoreConfig {
	return MetricsStoreConfig{
		ID:                 GenerateID(),
		InMemoryOnly:       false,
		MaxPointsPerMetric: 1000,
		RetentionPeriod:    1 * time.Hour,
		JSONLPath:          filepath.Join(stackDir, "health_metrics.jsonl"),
		FlushInterval:      5 * time.Minute,
		MaxJSONLSize:       10 * 1024 * 1024, // 10MB
		MaxRotatedFiles:    3,
		CreatedAt:          time.Now(),
	}
}

// NewEphemeralMetricsStore creates a new metrics store.
//
// # Description
//
// Creates an EphemeralMetricsStore with the given configuration.
// If persistence is enabled, loads existing data from JSONL file.
// Starts a background goroutine for periodic flushing.
//
// # Inputs
//
//   - config: Store configuration
//
// # Outputs
//
//   - *EphemeralMetricsStore: Ready-to-use store
//   - error: If file loading fails
//
// # Examples
//
//	store, err := NewEphemeralMetricsStore(DefaultMetricsStoreConfig("/path"))
//	if err != nil {
//	    log.Fatalf("Failed to create metrics store: %v", err)
//	}
//	defer store.Close()
//
// # Limitations
//
//   - Loading large JSONL files may be slow
//
// # Assumptions
//
//   - File path is readable/writable
func NewEphemeralMetricsStore(config MetricsStoreConfig) (*EphemeralMetricsStore, error) {
	store := &EphemeralMetricsStore{
		config:    config,
		inmemory:  make(map[string][]MetricPoint),
		createdAt: time.Now(),
		stopChan:  make(chan struct{}),
	}

	// Load existing data from JSONL if persistence enabled
	if !config.InMemoryOnly && config.JSONLPath != "" {
		if err := store.loadFromJSONL(); err != nil {
			// File may not exist yet, that's okay
			if !os.IsNotExist(err) {
				return nil, fmt.Errorf("failed to load metrics from JSONL: %w", err)
			}
		}

		// Start background flush
		if config.FlushInterval > 0 {
			store.flushTicker = time.NewTicker(config.FlushInterval)
			store.wg.Add(1)
			go store.backgroundFlush()
		}
	}

	return store, nil
}

// NewInMemoryMetricsStore creates a memory-only store for testing.
//
// # Description
//
// Creates an EphemeralMetricsStore without JSONL persistence.
// Useful for testing and scenarios where persistence is not needed.
//
// # Inputs
//
//   - None
//
// # Outputs
//
//   - *EphemeralMetricsStore: Memory-only store
//
// # Examples
//
//	store := NewInMemoryMetricsStore()
//	defer store.Close()
//
// # Limitations
//
//   - Data is lost when store is closed
//
// # Assumptions
//
//   - None
func NewInMemoryMetricsStore() *EphemeralMetricsStore {
	return &EphemeralMetricsStore{
		config: MetricsStoreConfig{
			ID:                 GenerateID(),
			InMemoryOnly:       true,
			MaxPointsPerMetric: 1000,
			RetentionPeriod:    1 * time.Hour,
			CreatedAt:          time.Now(),
		},
		inmemory:  make(map[string][]MetricPoint),
		createdAt: time.Now(),
		stopChan:  make(chan struct{}),
	}
}

// =============================================================================
// EphemeralMetricsStore METHODS
// =============================================================================

// Record stores a metric data point.
//
// # Description
//
// Adds a metric value to the in-memory store. Automatically enforces
// the rolling window limit by removing oldest points when necessary.
//
// # Inputs
//
//   - service: Service name
//   - metric: Metric name
//   - value: Metric value
//   - timestamp: Observation time
//
// # Outputs
//
//   - None
//
// # Examples
//
//	store.Record("orchestrator", "latency_p99", 150.0, time.Now())
//
// # Limitations
//
//   - Does not immediately persist to file
//
// # Assumptions
//
//   - service and metric are non-empty
func (s *EphemeralMetricsStore) Record(service, metric string, value float64, timestamp time.Time) {
	key := service + ":" + metric
	point := MetricPoint{
		ID:        GenerateID(),
		Service:   service,
		Metric:    metric,
		Value:     value,
		Timestamp: timestamp,
		CreatedAt: time.Now(),
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	points := s.inmemory[key]
	points = append(points, point)

	// Enforce max points limit (rolling window)
	if len(points) > s.config.MaxPointsPerMetric {
		points = points[len(points)-s.config.MaxPointsPerMetric:]
	}

	s.inmemory[key] = points
}

// Query returns metrics within a time window.
//
// # Description
//
// Returns all in-memory data points for the specified service and metric
// within the given time range, sorted by timestamp ascending.
//
// # Inputs
//
//   - service: Service name
//   - metric: Metric name
//   - start: Start time (inclusive)
//   - end: End time (inclusive)
//
// # Outputs
//
//   - []MetricPoint: Matching points, sorted by time
//
// # Examples
//
//	points := store.Query("svc", "latency", start, end)
//	for _, p := range points {
//	    fmt.Printf("%v: %f\n", p.Timestamp, p.Value)
//	}
//
// # Limitations
//
//   - Only queries in-memory data
//
// # Assumptions
//
//   - start <= end
func (s *EphemeralMetricsStore) Query(service, metric string, start, end time.Time) []MetricPoint {
	key := service + ":" + metric

	s.mu.RLock()
	points := s.inmemory[key]
	s.mu.RUnlock()

	// Filter by time range
	var result []MetricPoint
	for _, p := range points {
		if (p.Timestamp.Equal(start) || p.Timestamp.After(start)) &&
			(p.Timestamp.Equal(end) || p.Timestamp.Before(end)) {
			result = append(result, p)
		}
	}

	// Sort by timestamp
	sort.Slice(result, func(i, j int) bool {
		return result[i].Timestamp.Before(result[j].Timestamp)
	})

	return result
}

// GetBaseline returns baseline statistics for comparison.
//
// # Description
//
// Computes statistical baseline from data within the specified window.
// Returns nil if fewer than 2 data points are available.
//
// # Inputs
//
//   - service: Service name
//   - metric: Metric name
//   - window: Time window for baseline calculation
//
// # Outputs
//
//   - *BaselineStats: Statistics, or nil if insufficient data
//
// # Examples
//
//	baseline := store.GetBaseline("svc", "latency", 24*time.Hour)
//	if baseline != nil && currentValue > baseline.P99*1.5 {
//	    log.Println("Warning: value exceeds baseline")
//	}
//
// # Limitations
//
//   - Percentile calculation is linear interpolation
//
// # Assumptions
//
//   - Window is positive duration
func (s *EphemeralMetricsStore) GetBaseline(service, metric string, window time.Duration) *BaselineStats {
	end := time.Now()
	start := end.Add(-window)
	points := s.Query(service, metric, start, end)

	if len(points) < 2 {
		return nil
	}

	// Extract values
	values := make([]float64, len(points))
	for i, p := range points {
		values[i] = p.Value
	}

	// Sort for percentiles
	sorted := make([]float64, len(values))
	copy(sorted, values)
	sort.Float64s(sorted)

	// Calculate statistics
	n := len(sorted)
	p50Idx := int(float64(n) * 0.50)
	p99Idx := int(float64(n) * 0.99)
	if p99Idx >= n {
		p99Idx = n - 1
	}

	sum := 0.0
	for _, v := range values {
		sum += v
	}
	mean := sum / float64(n)

	variance := 0.0
	for _, v := range values {
		variance += (v - mean) * (v - mean)
	}
	stddev := math.Sqrt(variance / float64(n))

	return &BaselineStats{
		ID:          GenerateID(),
		P50:         sorted[p50Idx],
		P99:         sorted[p99Idx],
		Mean:        mean,
		StdDev:      stddev,
		Min:         sorted[0],
		Max:         sorted[n-1],
		DataPoints:  n,
		WindowStart: start,
		WindowEnd:   end,
		CreatedAt:   time.Now(),
	}
}

// Flush persists in-memory data to JSONL file.
//
// # Description
//
// Writes all in-memory data points to the JSONL file. Each point
// is written as a single JSON line, allowing for append-only writes.
//
// # Inputs
//
//   - ctx: Context for cancellation
//
// # Outputs
//
//   - error: If write fails
//
// # Examples
//
//	if err := store.Flush(ctx); err != nil {
//	    log.Printf("Flush failed: %v", err)
//	}
//
// # Limitations
//
//   - Overwrites the entire file each time
//
// # Assumptions
//
//   - File path is writable
func (s *EphemeralMetricsStore) Flush(ctx context.Context) error {
	if s.config.InMemoryOnly || s.config.JSONLPath == "" {
		return nil
	}

	s.mu.RLock()
	// Collect all points
	var allPoints []MetricPoint
	for _, points := range s.inmemory {
		allPoints = append(allPoints, points...)
	}
	s.mu.RUnlock()

	if len(allPoints) == 0 {
		return nil
	}

	// Check for cancellation
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// Write to JSONL file
	file, err := os.Create(s.config.JSONLPath)
	if err != nil {
		return fmt.Errorf("failed to create JSONL file: %w", err)
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	for _, p := range allPoints {
		if err := encoder.Encode(p); err != nil {
			return fmt.Errorf("failed to write metric point: %w", err)
		}
	}

	// Rotate if file exceeds size limit
	if err := s.rotateIfNeeded(); err != nil {
		return fmt.Errorf("failed to rotate JSONL file: %w", err)
	}

	return nil
}

// rotateIfNeeded checks file size and rotates if it exceeds MaxJSONLSize.
//
// # Description
//
// Implements log rotation to prevent unbounded file growth. When the JSONL
// file exceeds MaxJSONLSize, it is rotated: current file becomes .1, .1
// becomes .2, etc. Files beyond MaxRotatedFiles are deleted.
//
// # Inputs
//
//   - None
//
// # Outputs
//
//   - error: If rotation fails
//
// # Examples
//
//	// Called automatically after Flush
//	// Manual: if err := store.rotateIfNeeded(); err != nil { ... }
//
// # Limitations
//
//   - Rotation is not atomic (brief window where data could be lost)
//
// # Assumptions
//
//   - File path is writable
//   - MaxJSONLSize > 0 for rotation to occur
func (s *EphemeralMetricsStore) rotateIfNeeded() error {
	if s.config.MaxJSONLSize <= 0 || s.config.JSONLPath == "" {
		return nil
	}

	info, err := os.Stat(s.config.JSONLPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to stat JSONL file: %w", err)
	}

	if info.Size() < s.config.MaxJSONLSize {
		return nil
	}

	// File exceeds limit, perform rotation
	// Delete oldest rotated file if it exists
	oldestPath := fmt.Sprintf("%s.%d", s.config.JSONLPath, s.config.MaxRotatedFiles)
	_ = os.Remove(oldestPath) // Ignore error if doesn't exist

	// Shift existing rotated files: .2 -> .3, .1 -> .2, etc.
	for i := s.config.MaxRotatedFiles - 1; i >= 1; i-- {
		oldPath := fmt.Sprintf("%s.%d", s.config.JSONLPath, i)
		newPath := fmt.Sprintf("%s.%d", s.config.JSONLPath, i+1)
		_ = os.Rename(oldPath, newPath) // Ignore error if doesn't exist
	}

	// Rotate current file to .1
	rotatedPath := fmt.Sprintf("%s.1", s.config.JSONLPath)
	if err := os.Rename(s.config.JSONLPath, rotatedPath); err != nil {
		return fmt.Errorf("failed to rotate JSONL file: %w", err)
	}

	return nil
}

// Prune removes data older than retention period.
//
// # Description
//
// Removes data points older than the specified duration from in-memory
// storage. Also rewrites the JSONL file if persistence is enabled.
//
// # Inputs
//
//   - olderThan: Maximum age of data to keep
//
// # Outputs
//
//   - int: Number of points removed
//   - error: If file rewrite fails
//
// # Examples
//
//	removed, err := store.Prune(24 * time.Hour)
//	fmt.Printf("Removed %d old points\n", removed)
//
// # Limitations
//
//   - Requires rewriting JSONL file for persistence
//
// # Assumptions
//
//   - olderThan is positive duration
func (s *EphemeralMetricsStore) Prune(olderThan time.Duration) (int, error) {
	cutoff := time.Now().Add(-olderThan)
	removed := 0

	// Prune in-memory
	s.mu.Lock()
	for key, points := range s.inmemory {
		var kept []MetricPoint
		for _, p := range points {
			if p.Timestamp.After(cutoff) {
				kept = append(kept, p)
			} else {
				removed++
			}
		}
		if len(kept) > 0 {
			s.inmemory[key] = kept
		} else {
			delete(s.inmemory, key)
		}
	}
	s.mu.Unlock()

	// Rewrite JSONL file to remove pruned data
	if !s.config.InMemoryOnly && s.config.JSONLPath != "" {
		if err := s.Flush(context.Background()); err != nil {
			return removed, fmt.Errorf("failed to rewrite JSONL after prune: %w", err)
		}
	}

	return removed, nil
}

// Close releases resources.
//
// # Description
//
// Stops the background flush goroutine, performs a final flush,
// and releases any resources.
//
// # Inputs
//
//   - None
//
// # Outputs
//
//   - error: If final flush fails
//
// # Examples
//
//	defer store.Close()
//
// # Limitations
//
//   - None
//
// # Assumptions
//
//   - None
func (s *EphemeralMetricsStore) Close() error {
	// Stop background flush
	if s.flushTicker != nil {
		s.flushTicker.Stop()
		close(s.stopChan)
		s.wg.Wait()
	}

	// Final flush
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return s.Flush(ctx)
}

// loadFromJSONL loads existing metrics from the JSONL file.
//
// # Description
//
// Reads the JSONL file line by line and loads metrics into memory.
// Automatically prunes data older than the retention period.
//
// # Inputs
//
//   - None
//
// # Outputs
//
//   - error: If file reading fails
//
// # Limitations
//
//   - Large files may be slow to load
//
// # Assumptions
//
//   - File contains valid JSONL data
func (s *EphemeralMetricsStore) loadFromJSONL() error {
	file, err := os.Open(s.config.JSONLPath)
	if err != nil {
		return err
	}
	defer file.Close()

	cutoff := time.Now().Add(-s.config.RetentionPeriod)
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		var point MetricPoint
		if err := json.Unmarshal(scanner.Bytes(), &point); err != nil {
			// Skip malformed lines
			continue
		}

		// Only load data within retention period
		if point.Timestamp.After(cutoff) {
			key := point.Service + ":" + point.Metric
			s.inmemory[key] = append(s.inmemory[key], point)
		}
	}

	return scanner.Err()
}

// backgroundFlush runs periodic flushes.
//
// # Description
//
// Runs in a goroutine and periodically flushes in-memory data to JSONL.
//
// # Limitations
//
//   - Errors are silently ignored
//
// # Assumptions
//
//   - Stop channel is closed when shutting down
func (s *EphemeralMetricsStore) backgroundFlush() {
	defer s.wg.Done()

	for {
		select {
		case <-s.flushTicker.C:
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			s.Flush(ctx)
			cancel()
		case <-s.stopChan:
			return
		}
	}
}

// =============================================================================
// MockMetricsStore METHODS
// =============================================================================

// Record stores the point for verification.
//
// # Description
//
// Appends the point to RecordedPoints for test verification.
func (m *MockMetricsStore) Record(service, metric string, value float64, timestamp time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.RecordedPoints = append(m.RecordedPoints, MetricPoint{
		ID:        GenerateID(),
		Service:   service,
		Metric:    metric,
		Value:     value,
		Timestamp: timestamp,
		CreatedAt: time.Now(),
	})
}

// Query returns configured results.
//
// # Description
//
// Returns QueryResults as configured in the mock.
func (m *MockMetricsStore) Query(service, metric string, start, end time.Time) []MetricPoint {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.QueryResults
}

// GetBaseline returns configured baseline.
//
// # Description
//
// Returns BaselineResult as configured in the mock.
func (m *MockMetricsStore) GetBaseline(service, metric string, window time.Duration) *BaselineStats {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.BaselineResult
}

// Flush returns configured error.
//
// # Description
//
// Returns FlushError as configured in the mock.
func (m *MockMetricsStore) Flush(ctx context.Context) error {
	return m.FlushError
}

// Prune returns configured count and error.
//
// # Description
//
// Returns PruneCount and PruneError as configured in the mock.
func (m *MockMetricsStore) Prune(olderThan time.Duration) (int, error) {
	return m.PruneCount, m.PruneError
}

// Close returns configured error.
//
// # Description
//
// Returns CloseError as configured in the mock.
func (m *MockMetricsStore) Close() error {
	return m.CloseError
}
