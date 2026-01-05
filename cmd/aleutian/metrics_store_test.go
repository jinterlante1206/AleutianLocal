// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package main

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// =============================================================================
// EphemeralMetricsStore TESTS
// =============================================================================

func TestEphemeralMetricsStore_Record_Query(t *testing.T) {
	store := NewInMemoryMetricsStore()
	defer store.Close()

	now := time.Now()

	// Record some points
	store.Record("orchestrator", "latency_p99", 100.0, now.Add(-5*time.Minute))
	store.Record("orchestrator", "latency_p99", 150.0, now.Add(-3*time.Minute))
	store.Record("orchestrator", "latency_p99", 200.0, now.Add(-1*time.Minute))

	// Query the range
	points := store.Query("orchestrator", "latency_p99", now.Add(-10*time.Minute), now)

	if len(points) != 3 {
		t.Errorf("Expected 3 points, got %d", len(points))
	}

	// Verify order
	for i := 1; i < len(points); i++ {
		if points[i].Timestamp.Before(points[i-1].Timestamp) {
			t.Error("Points should be sorted by timestamp")
		}
	}

	// Verify values
	expectedValues := []float64{100.0, 150.0, 200.0}
	for i, p := range points {
		if p.Value != expectedValues[i] {
			t.Errorf("Point %d: expected value %f, got %f", i, expectedValues[i], p.Value)
		}
	}
}

func TestEphemeralMetricsStore_Query_TimeFilter(t *testing.T) {
	store := NewInMemoryMetricsStore()
	defer store.Close()

	now := time.Now()

	store.Record("svc", "metric", 1.0, now.Add(-10*time.Minute))
	store.Record("svc", "metric", 2.0, now.Add(-5*time.Minute))
	store.Record("svc", "metric", 3.0, now)

	// Query only middle point
	points := store.Query("svc", "metric", now.Add(-6*time.Minute), now.Add(-4*time.Minute))

	if len(points) != 1 {
		t.Errorf("Expected 1 point, got %d", len(points))
	}
	if len(points) > 0 && points[0].Value != 2.0 {
		t.Errorf("Expected value 2.0, got %f", points[0].Value)
	}
}

func TestEphemeralMetricsStore_Query_EmptyResult(t *testing.T) {
	store := NewInMemoryMetricsStore()
	defer store.Close()

	now := time.Now()
	store.Record("svc", "metric", 1.0, now)

	// Query different metric
	points := store.Query("svc", "other_metric", now.Add(-1*time.Hour), now)

	if len(points) != 0 {
		t.Errorf("Expected 0 points, got %d", len(points))
	}
}

func TestEphemeralMetricsStore_Query_DifferentServices(t *testing.T) {
	store := NewInMemoryMetricsStore()
	defer store.Close()

	now := time.Now()

	store.Record("svc1", "latency", 100.0, now)
	store.Record("svc2", "latency", 200.0, now)

	points1 := store.Query("svc1", "latency", now.Add(-1*time.Minute), now.Add(1*time.Minute))
	points2 := store.Query("svc2", "latency", now.Add(-1*time.Minute), now.Add(1*time.Minute))

	if len(points1) != 1 || points1[0].Value != 100.0 {
		t.Error("svc1 query incorrect")
	}
	if len(points2) != 1 || points2[0].Value != 200.0 {
		t.Error("svc2 query incorrect")
	}
}

func TestEphemeralMetricsStore_RollingWindow(t *testing.T) {
	store := &EphemeralMetricsStore{
		config: MetricsStoreConfig{
			InMemoryOnly:       true,
			MaxPointsPerMetric: 5, // Small limit for testing
		},
		inmemory: make(map[string][]MetricPoint),
		stopChan: make(chan struct{}),
	}
	defer store.Close()

	now := time.Now()

	// Add more points than limit
	for i := 0; i < 10; i++ {
		store.Record("svc", "metric", float64(i), now.Add(time.Duration(i)*time.Minute))
	}

	// Query all
	points := store.Query("svc", "metric", now.Add(-1*time.Hour), now.Add(1*time.Hour))

	if len(points) != 5 {
		t.Errorf("Expected 5 points (limit), got %d", len(points))
	}

	// Should have the most recent 5 points (values 5-9)
	if points[0].Value != 5.0 || points[4].Value != 9.0 {
		t.Error("Rolling window should keep most recent points")
	}
}

func TestEphemeralMetricsStore_GetBaseline_Basic(t *testing.T) {
	store := NewInMemoryMetricsStore()
	defer store.Close()

	now := time.Now()

	// Add predictable data: 1, 2, 3, 4, 5, 6, 7, 8, 9, 10
	for i := 1; i <= 10; i++ {
		store.Record("svc", "metric", float64(i), now.Add(-time.Duration(11-i)*time.Minute))
	}

	baseline := store.GetBaseline("svc", "metric", 1*time.Hour)

	if baseline == nil {
		t.Fatal("Expected baseline, got nil")
	}

	// Check basic stats
	if baseline.DataPoints != 10 {
		t.Errorf("Expected 10 data points, got %d", baseline.DataPoints)
	}
	if baseline.Min != 1.0 {
		t.Errorf("Expected min 1.0, got %f", baseline.Min)
	}
	if baseline.Max != 10.0 {
		t.Errorf("Expected max 10.0, got %f", baseline.Max)
	}
	if baseline.Mean != 5.5 {
		t.Errorf("Expected mean 5.5, got %f", baseline.Mean)
	}
	if baseline.ID == "" {
		t.Error("Expected baseline to have an ID")
	}
	if baseline.CreatedAt.IsZero() {
		t.Error("Expected baseline to have a CreatedAt timestamp")
	}
}

func TestEphemeralMetricsStore_GetBaseline_InsufficientData(t *testing.T) {
	store := NewInMemoryMetricsStore()
	defer store.Close()

	now := time.Now()

	// Only 1 point
	store.Record("svc", "metric", 100.0, now)

	baseline := store.GetBaseline("svc", "metric", 1*time.Hour)

	if baseline != nil {
		t.Error("Expected nil baseline for insufficient data")
	}
}

func TestEphemeralMetricsStore_GetBaseline_NoData(t *testing.T) {
	store := NewInMemoryMetricsStore()
	defer store.Close()

	baseline := store.GetBaseline("svc", "metric", 1*time.Hour)

	if baseline != nil {
		t.Error("Expected nil baseline for no data")
	}
}

func TestEphemeralMetricsStore_Prune(t *testing.T) {
	store := NewInMemoryMetricsStore()
	defer store.Close()

	now := time.Now()

	// Add old and new data
	store.Record("svc", "metric", 1.0, now.Add(-2*time.Hour))
	store.Record("svc", "metric", 2.0, now.Add(-30*time.Minute))
	store.Record("svc", "metric", 3.0, now)

	// Prune data older than 1 hour
	removed, err := store.Prune(1 * time.Hour)

	if err != nil {
		t.Fatalf("Prune failed: %v", err)
	}
	if removed != 1 {
		t.Errorf("Expected 1 removed, got %d", removed)
	}

	// Verify remaining data
	points := store.Query("svc", "metric", now.Add(-3*time.Hour), now.Add(1*time.Hour))
	if len(points) != 2 {
		t.Errorf("Expected 2 remaining points, got %d", len(points))
	}
}

func TestEphemeralMetricsStore_ThreadSafety(t *testing.T) {
	store := NewInMemoryMetricsStore()
	defer store.Close()

	var wg sync.WaitGroup
	iterations := 100

	// Concurrent writes
	for i := 0; i < iterations; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			store.Record("svc", "metric", float64(n), time.Now())
		}(i)
	}

	// Concurrent reads
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			store.Query("svc", "metric", time.Now().Add(-1*time.Hour), time.Now())
			store.GetBaseline("svc", "metric", 1*time.Hour)
		}()
	}

	wg.Wait()

	// Verify data integrity
	points := store.Query("svc", "metric", time.Now().Add(-1*time.Hour), time.Now().Add(1*time.Hour))
	if len(points) != iterations {
		t.Errorf("Expected %d points, got %d", iterations, len(points))
	}
}

func TestEphemeralMetricsStore_JSONLPersistence(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "metrics_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	config := MetricsStoreConfig{
		ID:                 GenerateID(),
		InMemoryOnly:       false,
		MaxPointsPerMetric: 100,
		RetentionPeriod:    1 * time.Hour,
		JSONLPath:          filepath.Join(tmpDir, "test_metrics.jsonl"),
		FlushInterval:      0, // Manual flush only
		CreatedAt:          time.Now(),
	}

	store, err := NewEphemeralMetricsStore(config)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}

	now := time.Now()
	store.Record("svc", "metric", 100.0, now)
	store.Record("svc", "metric", 200.0, now.Add(1*time.Minute))

	// Flush to JSONL
	ctx := context.Background()
	if err := store.Flush(ctx); err != nil {
		t.Fatalf("Flush failed: %v", err)
	}

	store.Close()

	// Verify JSONL file exists
	if _, err := os.Stat(config.JSONLPath); os.IsNotExist(err) {
		t.Error("JSONL file was not created")
	}
}

func TestEphemeralMetricsStore_Flush_NoDatabase(t *testing.T) {
	store := NewInMemoryMetricsStore()
	defer store.Close()

	store.Record("svc", "metric", 100.0, time.Now())

	// Flush should be no-op for in-memory store
	err := store.Flush(context.Background())
	if err != nil {
		t.Errorf("Flush should not error for in-memory store: %v", err)
	}
}

func TestEphemeralMetricsStore_JSONLReload(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "metrics_reload_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	jsonlPath := filepath.Join(tmpDir, "test_metrics.jsonl")

	// Create first store and write data
	config1 := MetricsStoreConfig{
		ID:                 GenerateID(),
		InMemoryOnly:       false,
		MaxPointsPerMetric: 100,
		RetentionPeriod:    1 * time.Hour,
		JSONLPath:          jsonlPath,
		FlushInterval:      0, // Manual flush only
		CreatedAt:          time.Now(),
	}

	store1, err := NewEphemeralMetricsStore(config1)
	if err != nil {
		t.Fatalf("Failed to create first store: %v", err)
	}

	now := time.Now()
	store1.Record("svc", "metric", 100.0, now.Add(-10*time.Minute))
	store1.Record("svc", "metric", 200.0, now.Add(-5*time.Minute))
	store1.Record("svc", "metric", 300.0, now)

	// Flush to JSONL
	if err := store1.Flush(context.Background()); err != nil {
		t.Fatalf("Flush failed: %v", err)
	}
	store1.Close()

	// Create second store and verify it loads existing data
	config2 := MetricsStoreConfig{
		ID:                 GenerateID(),
		InMemoryOnly:       false,
		MaxPointsPerMetric: 100,
		RetentionPeriod:    1 * time.Hour,
		JSONLPath:          jsonlPath,
		FlushInterval:      0,
		CreatedAt:          time.Now(),
	}

	store2, err := NewEphemeralMetricsStore(config2)
	if err != nil {
		t.Fatalf("Failed to create second store: %v", err)
	}
	defer store2.Close()

	// Query the reloaded data
	points := store2.Query("svc", "metric", now.Add(-1*time.Hour), now.Add(1*time.Hour))

	if len(points) != 3 {
		t.Errorf("Expected 3 reloaded points, got %d", len(points))
	}

	// Verify values were correctly reloaded
	expectedValues := []float64{100.0, 200.0, 300.0}
	for i, p := range points {
		if p.Value != expectedValues[i] {
			t.Errorf("Reloaded point %d: expected %f, got %f", i, expectedValues[i], p.Value)
		}
	}
}

func TestNewEphemeralMetricsStore_InvalidPath(t *testing.T) {
	// Use a path that definitely cannot be written to
	invalidPath := "/dev/null/invalid/path/metrics.jsonl"

	config := MetricsStoreConfig{
		ID:           GenerateID(),
		InMemoryOnly: false,
		JSONLPath:    invalidPath,
		CreatedAt:    time.Now(),
	}

	// JSONL file doesn't need to exist at creation (unlike SQLite)
	// But it will fail when we try to flush to an invalid directory
	store, err := NewEphemeralMetricsStore(config)
	if err != nil {
		// If loading fails due to missing file, that's expected and OK
		return
	}
	defer store.Close()

	// Record some data
	store.Record("svc", "metric", 100.0, time.Now())

	// Flush should fail because /dev/null cannot have subdirectories
	err = store.Flush(context.Background())
	if err == nil {
		t.Skip("System allowed write to invalid path (unexpected)")
	}
}

func TestDefaultMetricsStoreConfig(t *testing.T) {
	config := DefaultMetricsStoreConfig("/path/to/stack")

	if config.InMemoryOnly {
		t.Error("Expected InMemoryOnly to be false")
	}
	if config.MaxPointsPerMetric != 1000 {
		t.Errorf("Expected MaxPointsPerMetric 1000, got %d", config.MaxPointsPerMetric)
	}
	if config.RetentionPeriod != 1*time.Hour {
		t.Errorf("Expected RetentionPeriod 1h, got %v", config.RetentionPeriod)
	}
	if config.JSONLPath != "/path/to/stack/health_metrics.jsonl" {
		t.Errorf("Expected JSONLPath '/path/to/stack/health_metrics.jsonl', got %s", config.JSONLPath)
	}
	if config.FlushInterval != 5*time.Minute {
		t.Errorf("Expected FlushInterval 5m, got %v", config.FlushInterval)
	}
	if config.ID == "" {
		t.Error("Expected config to have an ID")
	}
	if config.CreatedAt.IsZero() {
		t.Error("Expected config to have a CreatedAt timestamp")
	}
}

func TestMetricPoint_HasRequiredFields(t *testing.T) {
	store := NewInMemoryMetricsStore()
	defer store.Close()

	now := time.Now()
	store.Record("svc", "metric", 100.0, now)

	points := store.Query("svc", "metric", now.Add(-1*time.Minute), now.Add(1*time.Minute))

	if len(points) != 1 {
		t.Fatal("Expected 1 point")
	}

	p := points[0]
	if p.ID == "" {
		t.Error("Expected point to have an ID")
	}
	if p.Service != "svc" {
		t.Errorf("Expected service 'svc', got %s", p.Service)
	}
	if p.Metric != "metric" {
		t.Errorf("Expected metric 'metric', got %s", p.Metric)
	}
	if p.Value != 100.0 {
		t.Errorf("Expected value 100.0, got %f", p.Value)
	}
	if p.CreatedAt.IsZero() {
		t.Error("Expected point to have a CreatedAt timestamp")
	}
}

// =============================================================================
// MockMetricsStore TESTS
// =============================================================================

func TestMockMetricsStore_Record(t *testing.T) {
	mock := &MockMetricsStore{}

	mock.Record("svc", "metric", 100.0, time.Now())
	mock.Record("svc", "metric", 200.0, time.Now())

	if len(mock.RecordedPoints) != 2 {
		t.Errorf("Expected 2 recorded points, got %d", len(mock.RecordedPoints))
	}
}

func TestMockMetricsStore_Query(t *testing.T) {
	mock := &MockMetricsStore{
		QueryResults: []MetricPoint{
			{Value: 100.0},
			{Value: 200.0},
		},
	}

	points := mock.Query("svc", "metric", time.Now(), time.Now())

	if len(points) != 2 {
		t.Errorf("Expected 2 points, got %d", len(points))
	}
}

func TestMockMetricsStore_GetBaseline(t *testing.T) {
	mock := &MockMetricsStore{
		BaselineResult: &BaselineStats{
			P50:  100.0,
			P99:  200.0,
			Mean: 150.0,
		},
	}

	baseline := mock.GetBaseline("svc", "metric", time.Hour)

	if baseline == nil {
		t.Fatal("Expected baseline")
	}
	if baseline.P50 != 100.0 {
		t.Errorf("Expected P50 100.0, got %f", baseline.P50)
	}
}

func TestMockMetricsStore_Flush(t *testing.T) {
	mock := &MockMetricsStore{}

	err := mock.Flush(context.Background())
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
}

func TestMockMetricsStore_Prune(t *testing.T) {
	mock := &MockMetricsStore{
		PruneCount: 5,
	}

	count, err := mock.Prune(time.Hour)

	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
	if count != 5 {
		t.Errorf("Expected count 5, got %d", count)
	}
}

// =============================================================================
// BENCHMARK TESTS
// =============================================================================

func BenchmarkEphemeralMetricsStore_Record(b *testing.B) {
	store := NewInMemoryMetricsStore()
	defer store.Close()

	now := time.Now()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		store.Record("svc", "metric", float64(i), now)
	}
}

func BenchmarkEphemeralMetricsStore_Query(b *testing.B) {
	store := NewInMemoryMetricsStore()
	defer store.Close()

	now := time.Now()
	for i := 0; i < 1000; i++ {
		store.Record("svc", "metric", float64(i), now.Add(-time.Duration(i)*time.Minute))
	}

	start := now.Add(-1 * time.Hour)
	end := now

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		store.Query("svc", "metric", start, end)
	}
}

func BenchmarkEphemeralMetricsStore_GetBaseline(b *testing.B) {
	store := NewInMemoryMetricsStore()
	defer store.Close()

	now := time.Now()
	for i := 0; i < 1000; i++ {
		store.Record("svc", "metric", float64(i), now.Add(-time.Duration(i)*time.Minute))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		store.GetBaseline("svc", "metric", 24*time.Hour)
	}
}
