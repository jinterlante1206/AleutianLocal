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
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// -----------------------------------------------------------------------------
// Mock Sink for Testing
// -----------------------------------------------------------------------------

// mockSink records all calls for verification.
type mockSink struct {
	mu           sync.Mutex
	benchmarks   []*BenchmarkData
	comparisons  []*ComparisonData
	errors       []*ErrorData
	flushCount   int
	closeCount   int
	closed       bool
	benchmarkErr error
	compareErr   error
	errorErr     error
	flushErr     error
	closeErr     error
}

func newMockSink() *mockSink {
	return &mockSink{}
}

func (m *mockSink) RecordBenchmark(ctx context.Context, data *BenchmarkData) error {
	if ctx == nil {
		return ErrNilContext
	}
	if data == nil {
		return ErrNilData
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return ErrSinkClosed
	}
	if m.benchmarkErr != nil {
		return m.benchmarkErr
	}
	m.benchmarks = append(m.benchmarks, data)
	return nil
}

func (m *mockSink) RecordComparison(ctx context.Context, data *ComparisonData) error {
	if ctx == nil {
		return ErrNilContext
	}
	if data == nil {
		return ErrNilData
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return ErrSinkClosed
	}
	if m.compareErr != nil {
		return m.compareErr
	}
	m.comparisons = append(m.comparisons, data)
	return nil
}

func (m *mockSink) RecordError(ctx context.Context, data *ErrorData) error {
	if ctx == nil {
		return ErrNilContext
	}
	if data == nil {
		return ErrNilData
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return ErrSinkClosed
	}
	if m.errorErr != nil {
		return m.errorErr
	}
	m.errors = append(m.errors, data)
	return nil
}

func (m *mockSink) Flush(ctx context.Context) error {
	if ctx == nil {
		return ErrNilContext
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return ErrSinkClosed
	}
	if m.flushErr != nil {
		return m.flushErr
	}
	m.flushCount++
	return nil
}

func (m *mockSink) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil
	}
	m.closed = true
	m.closeCount++
	return m.closeErr
}

func (m *mockSink) getBenchmarkCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.benchmarks)
}

func (m *mockSink) getComparisonCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.comparisons)
}

func (m *mockSink) getErrorCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.errors)
}

func (m *mockSink) getFlushCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.flushCount
}

// Verify mockSink implements Sink
var _ Sink = (*mockSink)(nil)

// -----------------------------------------------------------------------------
// Test Data Helpers
// -----------------------------------------------------------------------------

func createTestBenchmarkData() *BenchmarkData {
	return &BenchmarkData{
		Name:       "test_component",
		Timestamp:  time.Now(),
		Duration:   1 * time.Second,
		Iterations: 100,
		Latency: LatencyData{
			Min:    5 * time.Millisecond,
			Max:    50 * time.Millisecond,
			Mean:   10 * time.Millisecond,
			Median: 9 * time.Millisecond,
			StdDev: 3 * time.Millisecond,
			P50:    9 * time.Millisecond,
			P90:    20 * time.Millisecond,
			P95:    30 * time.Millisecond,
			P99:    45 * time.Millisecond,
			P999:   48 * time.Millisecond,
		},
		Throughput: ThroughputData{
			OpsPerSecond: 100.0,
		},
		Memory: &MemoryData{
			HeapAllocBefore: 1024 * 1024,
			HeapAllocAfter:  1024 * 1024 * 2,
			HeapAllocDelta:  1024 * 1024,
			GCPauses:        2,
			GCPauseTotal:    5 * time.Millisecond,
		},
		Labels: map[string]string{
			"env":     "test",
			"version": "1.0.0",
		},
		ErrorCount: 5,
		ErrorRate:  0.05,
	}
}

func createTestComparisonData() *ComparisonData {
	return &ComparisonData{
		Timestamp:          time.Now(),
		Components:         []string{"fast", "slow"},
		Winner:             "fast",
		Speedup:            10.0,
		Significant:        true,
		PValue:             0.001,
		ConfidenceLevel:    0.95,
		EffectSize:         2.5,
		EffectSizeCategory: "large",
		Labels: map[string]string{
			"experiment": "perf_comparison",
		},
	}
}

func createTestErrorData() *ErrorData {
	return &ErrorData{
		Timestamp: time.Now(),
		Component: "test_component",
		Operation: "benchmark",
		ErrorType: "timeout",
		Message:   "operation timed out after 5s",
		Labels: map[string]string{
			"severity": "warning",
		},
	}
}

// -----------------------------------------------------------------------------
// NoOpSink Tests
// -----------------------------------------------------------------------------

func TestNoOpSink(t *testing.T) {
	t.Run("RecordBenchmark accepts data", func(t *testing.T) {
		sink := NewNoOpSink()
		err := sink.RecordBenchmark(context.Background(), createTestBenchmarkData())
		if err != nil {
			t.Errorf("RecordBenchmark failed: %v", err)
		}
	})

	t.Run("RecordBenchmark rejects nil context", func(t *testing.T) {
		sink := NewNoOpSink()
		err := sink.RecordBenchmark(nil, createTestBenchmarkData())
		if !errors.Is(err, ErrNilContext) {
			t.Errorf("Expected ErrNilContext, got %v", err)
		}
	})

	t.Run("RecordBenchmark rejects nil data", func(t *testing.T) {
		sink := NewNoOpSink()
		err := sink.RecordBenchmark(context.Background(), nil)
		if !errors.Is(err, ErrNilData) {
			t.Errorf("Expected ErrNilData, got %v", err)
		}
	})

	t.Run("RecordComparison accepts data", func(t *testing.T) {
		sink := NewNoOpSink()
		err := sink.RecordComparison(context.Background(), createTestComparisonData())
		if err != nil {
			t.Errorf("RecordComparison failed: %v", err)
		}
	})

	t.Run("RecordComparison rejects nil context", func(t *testing.T) {
		sink := NewNoOpSink()
		err := sink.RecordComparison(nil, createTestComparisonData())
		if !errors.Is(err, ErrNilContext) {
			t.Errorf("Expected ErrNilContext, got %v", err)
		}
	})

	t.Run("RecordComparison rejects nil data", func(t *testing.T) {
		sink := NewNoOpSink()
		err := sink.RecordComparison(context.Background(), nil)
		if !errors.Is(err, ErrNilData) {
			t.Errorf("Expected ErrNilData, got %v", err)
		}
	})

	t.Run("RecordError accepts data", func(t *testing.T) {
		sink := NewNoOpSink()
		err := sink.RecordError(context.Background(), createTestErrorData())
		if err != nil {
			t.Errorf("RecordError failed: %v", err)
		}
	})

	t.Run("RecordError rejects nil context", func(t *testing.T) {
		sink := NewNoOpSink()
		err := sink.RecordError(nil, createTestErrorData())
		if !errors.Is(err, ErrNilContext) {
			t.Errorf("Expected ErrNilContext, got %v", err)
		}
	})

	t.Run("RecordError rejects nil data", func(t *testing.T) {
		sink := NewNoOpSink()
		err := sink.RecordError(context.Background(), nil)
		if !errors.Is(err, ErrNilData) {
			t.Errorf("Expected ErrNilData, got %v", err)
		}
	})

	t.Run("Flush succeeds", func(t *testing.T) {
		sink := NewNoOpSink()
		err := sink.Flush(context.Background())
		if err != nil {
			t.Errorf("Flush failed: %v", err)
		}
	})

	t.Run("Flush rejects nil context", func(t *testing.T) {
		sink := NewNoOpSink()
		err := sink.Flush(nil)
		if !errors.Is(err, ErrNilContext) {
			t.Errorf("Expected ErrNilContext, got %v", err)
		}
	})

	t.Run("Close succeeds", func(t *testing.T) {
		sink := NewNoOpSink()
		err := sink.Close()
		if err != nil {
			t.Errorf("Close failed: %v", err)
		}
	})
}

// -----------------------------------------------------------------------------
// CompositeSink Tests
// -----------------------------------------------------------------------------

func TestNewCompositeSink(t *testing.T) {
	t.Run("creates with single sink", func(t *testing.T) {
		mock := newMockSink()
		composite, err := NewCompositeSink(mock)
		if err != nil {
			t.Fatalf("NewCompositeSink failed: %v", err)
		}
		if composite == nil {
			t.Fatal("Expected non-nil composite")
		}
	})

	t.Run("creates with multiple sinks", func(t *testing.T) {
		mock1 := newMockSink()
		mock2 := newMockSink()
		composite, err := NewCompositeSink(mock1, mock2)
		if err != nil {
			t.Fatalf("NewCompositeSink failed: %v", err)
		}
		if composite == nil {
			t.Fatal("Expected non-nil composite")
		}
	})

	t.Run("rejects empty sinks", func(t *testing.T) {
		_, err := NewCompositeSink()
		if !errors.Is(err, ErrNoSinks) {
			t.Errorf("Expected ErrNoSinks, got %v", err)
		}
	})

	t.Run("rejects all nil sinks", func(t *testing.T) {
		_, err := NewCompositeSink(nil, nil)
		if !errors.Is(err, ErrNoSinks) {
			t.Errorf("Expected ErrNoSinks, got %v", err)
		}
	})

	t.Run("filters nil sinks", func(t *testing.T) {
		mock := newMockSink()
		composite, err := NewCompositeSink(nil, mock, nil)
		if err != nil {
			t.Fatalf("NewCompositeSink failed: %v", err)
		}
		if composite == nil {
			t.Fatal("Expected non-nil composite")
		}
		if len(composite.sinks) != 1 {
			t.Errorf("Expected 1 sink, got %d", len(composite.sinks))
		}
	})
}

func TestCompositeSink_RecordBenchmark(t *testing.T) {
	t.Run("forwards to all sinks", func(t *testing.T) {
		mock1 := newMockSink()
		mock2 := newMockSink()
		composite, _ := NewCompositeSink(mock1, mock2)

		data := createTestBenchmarkData()
		err := composite.RecordBenchmark(context.Background(), data)
		if err != nil {
			t.Fatalf("RecordBenchmark failed: %v", err)
		}

		if mock1.getBenchmarkCount() != 1 {
			t.Errorf("mock1 received %d benchmarks, want 1", mock1.getBenchmarkCount())
		}
		if mock2.getBenchmarkCount() != 1 {
			t.Errorf("mock2 received %d benchmarks, want 1", mock2.getBenchmarkCount())
		}
	})

	t.Run("rejects nil context", func(t *testing.T) {
		mock := newMockSink()
		composite, _ := NewCompositeSink(mock)

		err := composite.RecordBenchmark(nil, createTestBenchmarkData())
		if !errors.Is(err, ErrNilContext) {
			t.Errorf("Expected ErrNilContext, got %v", err)
		}
	})

	t.Run("rejects nil data", func(t *testing.T) {
		mock := newMockSink()
		composite, _ := NewCompositeSink(mock)

		err := composite.RecordBenchmark(context.Background(), nil)
		if !errors.Is(err, ErrNilData) {
			t.Errorf("Expected ErrNilData, got %v", err)
		}
	})

	t.Run("returns error after close", func(t *testing.T) {
		mock := newMockSink()
		composite, _ := NewCompositeSink(mock)
		composite.Close()

		err := composite.RecordBenchmark(context.Background(), createTestBenchmarkData())
		if !errors.Is(err, ErrSinkClosed) {
			t.Errorf("Expected ErrSinkClosed, got %v", err)
		}
	})

	t.Run("collects errors from multiple sinks", func(t *testing.T) {
		mock1 := newMockSink()
		mock1.benchmarkErr = errors.New("mock1 error")
		mock2 := newMockSink()
		mock2.benchmarkErr = errors.New("mock2 error")
		composite, _ := NewCompositeSink(mock1, mock2)

		err := composite.RecordBenchmark(context.Background(), createTestBenchmarkData())
		if err == nil {
			t.Error("Expected error, got nil")
		}
		// Should contain both errors
		errStr := err.Error()
		if !errors.Is(err, mock1.benchmarkErr) && !errors.Is(err, mock2.benchmarkErr) {
			t.Errorf("Error should contain child errors: %s", errStr)
		}
	})

	t.Run("continues on partial failure", func(t *testing.T) {
		mock1 := newMockSink()
		mock1.benchmarkErr = errors.New("mock1 error")
		mock2 := newMockSink() // This one succeeds
		composite, _ := NewCompositeSink(mock1, mock2)

		err := composite.RecordBenchmark(context.Background(), createTestBenchmarkData())
		if err == nil {
			t.Error("Expected error from mock1")
		}
		// mock2 should still have received the data
		if mock2.getBenchmarkCount() != 1 {
			t.Errorf("mock2 should have received data despite mock1 error")
		}
	})
}

func TestCompositeSink_RecordComparison(t *testing.T) {
	t.Run("forwards to all sinks", func(t *testing.T) {
		mock1 := newMockSink()
		mock2 := newMockSink()
		composite, _ := NewCompositeSink(mock1, mock2)

		data := createTestComparisonData()
		err := composite.RecordComparison(context.Background(), data)
		if err != nil {
			t.Fatalf("RecordComparison failed: %v", err)
		}

		if mock1.getComparisonCount() != 1 {
			t.Errorf("mock1 received %d comparisons, want 1", mock1.getComparisonCount())
		}
		if mock2.getComparisonCount() != 1 {
			t.Errorf("mock2 received %d comparisons, want 1", mock2.getComparisonCount())
		}
	})

	t.Run("rejects nil context", func(t *testing.T) {
		mock := newMockSink()
		composite, _ := NewCompositeSink(mock)

		err := composite.RecordComparison(nil, createTestComparisonData())
		if !errors.Is(err, ErrNilContext) {
			t.Errorf("Expected ErrNilContext, got %v", err)
		}
	})

	t.Run("rejects nil data", func(t *testing.T) {
		mock := newMockSink()
		composite, _ := NewCompositeSink(mock)

		err := composite.RecordComparison(context.Background(), nil)
		if !errors.Is(err, ErrNilData) {
			t.Errorf("Expected ErrNilData, got %v", err)
		}
	})
}

func TestCompositeSink_RecordError(t *testing.T) {
	t.Run("forwards to all sinks", func(t *testing.T) {
		mock1 := newMockSink()
		mock2 := newMockSink()
		composite, _ := NewCompositeSink(mock1, mock2)

		data := createTestErrorData()
		err := composite.RecordError(context.Background(), data)
		if err != nil {
			t.Fatalf("RecordError failed: %v", err)
		}

		if mock1.getErrorCount() != 1 {
			t.Errorf("mock1 received %d errors, want 1", mock1.getErrorCount())
		}
		if mock2.getErrorCount() != 1 {
			t.Errorf("mock2 received %d errors, want 1", mock2.getErrorCount())
		}
	})

	t.Run("rejects nil context", func(t *testing.T) {
		mock := newMockSink()
		composite, _ := NewCompositeSink(mock)

		err := composite.RecordError(nil, createTestErrorData())
		if !errors.Is(err, ErrNilContext) {
			t.Errorf("Expected ErrNilContext, got %v", err)
		}
	})

	t.Run("rejects nil data", func(t *testing.T) {
		mock := newMockSink()
		composite, _ := NewCompositeSink(mock)

		err := composite.RecordError(context.Background(), nil)
		if !errors.Is(err, ErrNilData) {
			t.Errorf("Expected ErrNilData, got %v", err)
		}
	})
}

func TestCompositeSink_Flush(t *testing.T) {
	t.Run("flushes all sinks", func(t *testing.T) {
		mock1 := newMockSink()
		mock2 := newMockSink()
		composite, _ := NewCompositeSink(mock1, mock2)

		err := composite.Flush(context.Background())
		if err != nil {
			t.Fatalf("Flush failed: %v", err)
		}

		if mock1.getFlushCount() != 1 {
			t.Errorf("mock1 flush count = %d, want 1", mock1.getFlushCount())
		}
		if mock2.getFlushCount() != 1 {
			t.Errorf("mock2 flush count = %d, want 1", mock2.getFlushCount())
		}
	})

	t.Run("rejects nil context", func(t *testing.T) {
		mock := newMockSink()
		composite, _ := NewCompositeSink(mock)

		err := composite.Flush(nil)
		if !errors.Is(err, ErrNilContext) {
			t.Errorf("Expected ErrNilContext, got %v", err)
		}
	})

	t.Run("returns error after close", func(t *testing.T) {
		mock := newMockSink()
		composite, _ := NewCompositeSink(mock)
		composite.Close()

		err := composite.Flush(context.Background())
		if !errors.Is(err, ErrSinkClosed) {
			t.Errorf("Expected ErrSinkClosed, got %v", err)
		}
	})

	t.Run("collects errors from concurrent flush", func(t *testing.T) {
		mock1 := newMockSink()
		mock1.flushErr = errors.New("mock1 flush error")
		mock2 := newMockSink()
		mock2.flushErr = errors.New("mock2 flush error")
		composite, _ := NewCompositeSink(mock1, mock2)

		err := composite.Flush(context.Background())
		if err == nil {
			t.Error("Expected error, got nil")
		}
	})
}

func TestCompositeSink_Close(t *testing.T) {
	t.Run("closes all sinks", func(t *testing.T) {
		mock1 := newMockSink()
		mock2 := newMockSink()
		composite, _ := NewCompositeSink(mock1, mock2)

		err := composite.Close()
		if err != nil {
			t.Fatalf("Close failed: %v", err)
		}

		if mock1.closeCount != 1 {
			t.Errorf("mock1 close count = %d, want 1", mock1.closeCount)
		}
		if mock2.closeCount != 1 {
			t.Errorf("mock2 close count = %d, want 1", mock2.closeCount)
		}
	})

	t.Run("is idempotent", func(t *testing.T) {
		mock := newMockSink()
		composite, _ := NewCompositeSink(mock)

		// Close multiple times
		composite.Close()
		composite.Close()
		composite.Close()

		// Should only close child once
		if mock.closeCount != 1 {
			t.Errorf("mock close count = %d, want 1", mock.closeCount)
		}
	})

	t.Run("collects errors from all sinks", func(t *testing.T) {
		mock1 := newMockSink()
		mock1.closeErr = errors.New("mock1 close error")
		mock2 := newMockSink()
		mock2.closeErr = errors.New("mock2 close error")
		composite, _ := NewCompositeSink(mock1, mock2)

		err := composite.Close()
		if err == nil {
			t.Error("Expected error, got nil")
		}
	})
}

func TestCompositeSink_Concurrent(t *testing.T) {
	t.Run("handles concurrent recording", func(t *testing.T) {
		mock := newMockSink()
		composite, _ := NewCompositeSink(mock)

		var wg sync.WaitGroup
		iterations := 100

		// Concurrent benchmark records
		for i := 0; i < iterations; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				composite.RecordBenchmark(context.Background(), createTestBenchmarkData())
			}()
		}

		// Concurrent comparison records
		for i := 0; i < iterations; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				composite.RecordComparison(context.Background(), createTestComparisonData())
			}()
		}

		// Concurrent error records
		for i := 0; i < iterations; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				composite.RecordError(context.Background(), createTestErrorData())
			}()
		}

		wg.Wait()

		if mock.getBenchmarkCount() != iterations {
			t.Errorf("benchmark count = %d, want %d", mock.getBenchmarkCount(), iterations)
		}
		if mock.getComparisonCount() != iterations {
			t.Errorf("comparison count = %d, want %d", mock.getComparisonCount(), iterations)
		}
		if mock.getErrorCount() != iterations {
			t.Errorf("error count = %d, want %d", mock.getErrorCount(), iterations)
		}
	})

	t.Run("handles concurrent close attempts", func(t *testing.T) {
		mock := newMockSink()
		composite, _ := NewCompositeSink(mock)

		var wg sync.WaitGroup
		var closeCount int32

		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if composite.Close() == nil {
					atomic.AddInt32(&closeCount, 1)
				}
			}()
		}

		wg.Wait()

		// Close should succeed at least once
		if closeCount < 1 {
			t.Error("Expected at least one successful close")
		}

		// But mock should only be closed once
		if mock.closeCount != 1 {
			t.Errorf("mock close count = %d, want 1", mock.closeCount)
		}
	})
}

// -----------------------------------------------------------------------------
// Interface Compliance Tests
// -----------------------------------------------------------------------------

func TestSinkInterfaceCompliance(t *testing.T) {
	// Verify all sinks implement the interface
	var _ Sink = (*NoOpSink)(nil)
	var _ Sink = (*CompositeSink)(nil)
	var _ Sink = (*mockSink)(nil)
}
