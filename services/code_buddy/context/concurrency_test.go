// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package context

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestSemaphore_AcquireRelease(t *testing.T) {
	sem := NewSemaphore(2)

	ctx := context.Background()

	// Should be able to acquire twice
	if err := sem.Acquire(ctx); err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	if err := sem.Acquire(ctx); err != nil {
		t.Fatalf("second acquire: %v", err)
	}

	if sem.Available() != 0 {
		t.Errorf("Available = %d, want 0", sem.Available())
	}

	// Release one
	sem.Release()
	if sem.Available() != 1 {
		t.Errorf("Available after release = %d, want 1", sem.Available())
	}

	// Release another
	sem.Release()
	if sem.Available() != 2 {
		t.Errorf("Available after second release = %d, want 2", sem.Available())
	}
}

func TestSemaphore_TryAcquire(t *testing.T) {
	sem := NewSemaphore(1)

	// First try should succeed
	if !sem.TryAcquire() {
		t.Error("first TryAcquire should succeed")
	}

	// Second try should fail (non-blocking)
	if sem.TryAcquire() {
		t.Error("second TryAcquire should fail")
	}

	// Release and try again
	sem.Release()
	if !sem.TryAcquire() {
		t.Error("TryAcquire after release should succeed")
	}
}

func TestSemaphore_AcquireBlocks(t *testing.T) {
	sem := NewSemaphore(1)

	ctx := context.Background()
	sem.Acquire(ctx)

	acquired := make(chan bool, 1)
	go func() {
		sem.Acquire(ctx)
		acquired <- true
	}()

	// Should not acquire immediately
	select {
	case <-acquired:
		t.Error("should not acquire while held")
	case <-time.After(50 * time.Millisecond):
		// Expected
	}

	// Release should unblock
	sem.Release()

	select {
	case <-acquired:
		// Expected
	case <-time.After(100 * time.Millisecond):
		t.Error("should acquire after release")
	}
}

func TestSemaphore_AcquireContextCancellation(t *testing.T) {
	sem := NewSemaphore(1)

	ctx := context.Background()
	sem.Acquire(ctx)

	cancelCtx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() {
		errCh <- sem.Acquire(cancelCtx)
	}()

	// Cancel the context
	cancel()

	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("expected context.Canceled, got %v", err)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("acquire should return after context cancellation")
	}
}

func TestSemaphore_ReleasePanicOnEmpty(t *testing.T) {
	sem := NewSemaphore(1)

	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic on release without acquire")
		}
	}()

	sem.Release() // Should panic
}

func TestSemaphore_ZeroCapacity(t *testing.T) {
	sem := NewSemaphore(0) // Should become 1

	ctx := context.Background()
	if err := sem.Acquire(ctx); err != nil {
		t.Fatalf("acquire: %v", err)
	}

	// Second acquire should block (capacity is 1)
	if sem.TryAcquire() {
		t.Error("should not acquire beyond capacity 1")
	}
}

func TestWorkerPool_ProcessBatch_AllSuccess(t *testing.T) {
	config := ConcurrencyConfig{
		MaxLLMConcurrency: 5,
		PerEntityTimeout:  time.Second,
		TotalTimeout:      time.Minute,
	}
	pool := NewWorkerPool(5, config)

	items := make([]WorkItem, 10)
	for i := range items {
		id := fmt.Sprintf("item-%d", i)
		items[i] = WorkItem{
			ID: id,
			Work: func(ctx context.Context) error {
				return nil
			},
		}
	}

	ctx := context.Background()
	result := pool.ProcessBatch(ctx, items, nil)

	if result.SuccessCount != 10 {
		t.Errorf("SuccessCount = %d, want 10", result.SuccessCount)
	}
	if result.FailureCount != 0 {
		t.Errorf("FailureCount = %d, want 0", result.FailureCount)
	}
	if len(result.Results) != 10 {
		t.Errorf("Results = %d, want 10", len(result.Results))
	}
	if result.Cancelled {
		t.Error("should not be cancelled")
	}
}

func TestWorkerPool_ProcessBatch_SomeFailures(t *testing.T) {
	config := ConcurrencyConfig{
		MaxLLMConcurrency: 5,
		PerEntityTimeout:  time.Second,
		TotalTimeout:      time.Minute,
	}
	pool := NewWorkerPool(5, config)

	testErr := errors.New("test error")
	items := make([]WorkItem, 10)
	for i := range items {
		shouldFail := i%2 == 0
		items[i] = WorkItem{
			ID: fmt.Sprintf("item-%d", i),
			Work: func(ctx context.Context) error {
				if shouldFail {
					return testErr
				}
				return nil
			},
		}
	}

	ctx := context.Background()
	result := pool.ProcessBatch(ctx, items, nil)

	if result.SuccessCount != 5 {
		t.Errorf("SuccessCount = %d, want 5", result.SuccessCount)
	}
	if result.FailureCount != 5 {
		t.Errorf("FailureCount = %d, want 5", result.FailureCount)
	}
}

func TestWorkerPool_ProcessBatch_ProgressCallback(t *testing.T) {
	config := ConcurrencyConfig{
		MaxLLMConcurrency: 2,
		PerEntityTimeout:  time.Second,
		TotalTimeout:      time.Minute,
	}
	pool := NewWorkerPool(2, config)

	items := make([]WorkItem, 5)
	for i := range items {
		items[i] = WorkItem{
			ID: fmt.Sprintf("item-%d", i),
			Work: func(ctx context.Context) error {
				return nil
			},
		}
	}

	var progressCalls int32
	var maxCompleted int32

	ctx := context.Background()
	pool.ProcessBatch(ctx, items, func(completed, total int, lastResult *WorkResult) {
		atomic.AddInt32(&progressCalls, 1)
		// Atomically update max
		for {
			current := atomic.LoadInt32(&maxCompleted)
			if int32(completed) <= current {
				break
			}
			if atomic.CompareAndSwapInt32(&maxCompleted, current, int32(completed)) {
				break
			}
		}
		if total != 5 {
			t.Errorf("total = %d, want 5", total)
		}
	})

	if atomic.LoadInt32(&progressCalls) != 5 {
		t.Errorf("progressCalls = %d, want 5", progressCalls)
	}
	if atomic.LoadInt32(&maxCompleted) != 5 {
		t.Errorf("maxCompleted = %d, want 5", maxCompleted)
	}
}

func TestWorkerPool_ProcessBatch_ConcurrencyLimit(t *testing.T) {
	config := ConcurrencyConfig{
		MaxLLMConcurrency: 2, // Only 2 concurrent
		PerEntityTimeout:  time.Second,
		TotalTimeout:      time.Minute,
	}
	pool := NewWorkerPool(2, config)

	var maxConcurrent int32
	var currentConcurrent int32

	items := make([]WorkItem, 10)
	for i := range items {
		items[i] = WorkItem{
			ID: fmt.Sprintf("item-%d", i),
			Work: func(ctx context.Context) error {
				current := atomic.AddInt32(&currentConcurrent, 1)
				defer atomic.AddInt32(&currentConcurrent, -1)

				// Track max
				for {
					max := atomic.LoadInt32(&maxConcurrent)
					if current <= max {
						break
					}
					if atomic.CompareAndSwapInt32(&maxConcurrent, max, current) {
						break
					}
				}

				time.Sleep(10 * time.Millisecond)
				return nil
			},
		}
	}

	ctx := context.Background()
	pool.ProcessBatch(ctx, items, nil)

	if atomic.LoadInt32(&maxConcurrent) > 2 {
		t.Errorf("maxConcurrent = %d, should not exceed 2", maxConcurrent)
	}
}

func TestWorkerPool_ProcessBatch_ContextCancellation(t *testing.T) {
	config := ConcurrencyConfig{
		MaxLLMConcurrency: 2,
		PerEntityTimeout:  time.Second,
		TotalTimeout:      time.Minute,
	}
	pool := NewWorkerPool(2, config)

	items := make([]WorkItem, 10)
	for i := range items {
		items[i] = WorkItem{
			ID: fmt.Sprintf("item-%d", i),
			Work: func(ctx context.Context) error {
				time.Sleep(100 * time.Millisecond)
				return nil
			},
		}
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Cancel after a short time
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	result := pool.ProcessBatch(ctx, items, nil)

	if !result.Cancelled {
		t.Error("expected Cancelled to be true")
	}
}

func TestWorkerPool_ProcessBatch_PerEntityTimeout(t *testing.T) {
	config := ConcurrencyConfig{
		MaxLLMConcurrency: 5,
		PerEntityTimeout:  10 * time.Millisecond, // Very short timeout
		TotalTimeout:      time.Minute,
	}
	pool := NewWorkerPool(5, config)

	items := []WorkItem{
		{
			ID: "slow-item",
			Work: func(ctx context.Context) error {
				select {
				case <-time.After(time.Second):
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			},
		},
	}

	ctx := context.Background()
	result := pool.ProcessBatch(ctx, items, nil)

	if result.FailureCount != 1 {
		t.Errorf("FailureCount = %d, want 1 (timeout)", result.FailureCount)
	}
}

func TestMapReduce_Success(t *testing.T) {
	config := DefaultConcurrencyConfig()
	pool := NewWorkerPool(5, config)

	items := []int{1, 2, 3, 4, 5}

	ctx := context.Background()
	results, err := MapReduce(ctx, pool, items, func(ctx context.Context, item int) (int, error) {
		return item * 2, nil
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := []int{2, 4, 6, 8, 10}
	for i, r := range results {
		if r != expected[i] {
			t.Errorf("results[%d] = %d, want %d", i, r, expected[i])
		}
	}
}

func TestMapReduce_PreservesOrder(t *testing.T) {
	config := DefaultConcurrencyConfig()
	pool := NewWorkerPool(2, config)

	items := []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}

	ctx := context.Background()
	results, err := MapReduce(ctx, pool, items, func(ctx context.Context, item int) (string, error) {
		// Add some jitter to execution order
		time.Sleep(time.Duration(item%3) * time.Millisecond)
		return fmt.Sprintf("item-%d", item), nil
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Results should be in same order as input
	for i, r := range results {
		expected := fmt.Sprintf("item-%d", items[i])
		if r != expected {
			t.Errorf("results[%d] = %q, want %q", i, r, expected)
		}
	}
}

func TestMapReduce_Error(t *testing.T) {
	config := DefaultConcurrencyConfig()
	pool := NewWorkerPool(5, config)

	items := []int{1, 2, 3}
	testErr := errors.New("test error")

	ctx := context.Background()
	_, err := MapReduce(ctx, pool, items, func(ctx context.Context, item int) (int, error) {
		if item == 2 {
			return 0, testErr
		}
		return item, nil
	})

	if err == nil {
		t.Error("expected error")
	}
}

func TestMapReduce_ContextCancellation(t *testing.T) {
	config := DefaultConcurrencyConfig()
	pool := NewWorkerPool(2, config)

	items := []int{1, 2, 3, 4, 5}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := MapReduce(ctx, pool, items, func(ctx context.Context, item int) (int, error) {
		time.Sleep(100 * time.Millisecond)
		return item, nil
	})

	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestLLMWorkerPool(t *testing.T) {
	config := ConcurrencyConfig{
		MaxLLMConcurrency: 3,
	}
	pool := LLMWorkerPool(config)

	if pool.semaphore.Available() != 3 {
		t.Errorf("Available = %d, want 3", pool.semaphore.Available())
	}
}

func TestWeaviateWorkerPool(t *testing.T) {
	config := ConcurrencyConfig{
		MaxWeaviateConcurrency: 10,
	}
	pool := WeaviateWorkerPool(config)

	if pool.semaphore.Available() != 10 {
		t.Errorf("Available = %d, want 10", pool.semaphore.Available())
	}
}

func TestDefaultConcurrencyConfig(t *testing.T) {
	config := DefaultConcurrencyConfig()

	if config.MaxLLMConcurrency != 5 {
		t.Errorf("MaxLLMConcurrency = %d, want 5", config.MaxLLMConcurrency)
	}
	if config.MaxWeaviateConcurrency != 10 {
		t.Errorf("MaxWeaviateConcurrency = %d, want 10", config.MaxWeaviateConcurrency)
	}
	if config.WeaviateBatchSize != 20 {
		t.Errorf("WeaviateBatchSize = %d, want 20", config.WeaviateBatchSize)
	}
	if config.PerEntityTimeout != 30*time.Second {
		t.Errorf("PerEntityTimeout = %v, want 30s", config.PerEntityTimeout)
	}
	if config.TotalTimeout != 10*time.Minute {
		t.Errorf("TotalTimeout = %v, want 10m", config.TotalTimeout)
	}
}

func TestBatchResult_Fields(t *testing.T) {
	result := &BatchResult{
		Results: []WorkResult{
			{ID: "1", Error: nil},
			{ID: "2", Error: errors.New("fail")},
		},
		SuccessCount:  1,
		FailureCount:  1,
		TotalDuration: time.Second,
		Cancelled:     false,
	}

	if len(result.Results) != 2 {
		t.Errorf("Results count = %d, want 2", len(result.Results))
	}
	if result.SuccessCount != 1 {
		t.Errorf("SuccessCount = %d, want 1", result.SuccessCount)
	}
	if result.FailureCount != 1 {
		t.Errorf("FailureCount = %d, want 1", result.FailureCount)
	}
}

func TestSemaphore_ConcurrentAccess(t *testing.T) {
	sem := NewSemaphore(5)

	var wg sync.WaitGroup
	iterations := 100

	ctx := context.Background()

	for i := 0; i < iterations; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := sem.Acquire(ctx); err != nil {
				return
			}
			time.Sleep(time.Millisecond)
			sem.Release()
		}()
	}

	wg.Wait()

	if sem.Available() != 5 {
		t.Errorf("Available after all release = %d, want 5", sem.Available())
	}
}
