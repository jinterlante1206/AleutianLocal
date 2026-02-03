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
	"sync"
	"sync/atomic"
	"time"
)

// ConcurrencyConfig configures parallel processing limits.
type ConcurrencyConfig struct {
	// MaxLLMConcurrency is the maximum concurrent LLM calls.
	// Prevents rate limiting. Default: 5
	MaxLLMConcurrency int `json:"max_llm_concurrency"`

	// MaxWeaviateConcurrency is the maximum concurrent Weaviate operations.
	// Weaviate can handle more than LLM. Default: 10
	MaxWeaviateConcurrency int `json:"max_weaviate_concurrency"`

	// WeaviateBatchSize is the batch size for Weaviate bulk operations.
	// Default: 20
	WeaviateBatchSize int `json:"weaviate_batch_size"`

	// PerEntityTimeout is the timeout for processing a single entity.
	// Default: 30s
	PerEntityTimeout time.Duration `json:"per_entity_timeout"`

	// TotalTimeout is the total timeout for batch operations.
	// Default: 10m
	TotalTimeout time.Duration `json:"total_timeout"`
}

// DefaultConcurrencyConfig returns sensible defaults for concurrency.
func DefaultConcurrencyConfig() ConcurrencyConfig {
	return ConcurrencyConfig{
		MaxLLMConcurrency:      5,
		MaxWeaviateConcurrency: 10,
		WeaviateBatchSize:      20,
		PerEntityTimeout:       30 * time.Second,
		TotalTimeout:           10 * time.Minute,
	}
}

// Semaphore implements a counting semaphore for bounded concurrency.
//
// Thread Safety: Safe for concurrent use.
type Semaphore struct {
	ch chan struct{}
}

// NewSemaphore creates a new semaphore with the given capacity.
//
// Inputs:
//   - capacity: Maximum concurrent acquisitions. Must be > 0.
//
// Outputs:
//   - *Semaphore: A new semaphore.
func NewSemaphore(capacity int) *Semaphore {
	if capacity <= 0 {
		capacity = 1
	}
	return &Semaphore{
		ch: make(chan struct{}, capacity),
	}
}

// Acquire acquires a slot, blocking until one is available.
//
// Inputs:
//   - ctx: Context for cancellation.
//
// Outputs:
//   - error: Non-nil if context was cancelled.
func (s *Semaphore) Acquire(ctx context.Context) error {
	select {
	case s.ch <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// TryAcquire attempts to acquire a slot without blocking.
//
// Outputs:
//   - bool: True if acquired, false if no slots available.
func (s *Semaphore) TryAcquire() bool {
	select {
	case s.ch <- struct{}{}:
		return true
	default:
		return false
	}
}

// Release releases a slot back to the semaphore.
// Must be called after Acquire/TryAcquire succeeds.
func (s *Semaphore) Release() {
	select {
	case <-s.ch:
	default:
		// Semaphore was empty - this is a bug in caller
		panic("semaphore: release without acquire")
	}
}

// Available returns the number of available slots.
func (s *Semaphore) Available() int {
	return cap(s.ch) - len(s.ch)
}

// WorkerPool manages a pool of concurrent workers.
//
// Thread Safety: Safe for concurrent use.
type WorkerPool struct {
	semaphore *Semaphore
	config    ConcurrencyConfig
}

// NewWorkerPool creates a new worker pool.
//
// Inputs:
//   - concurrency: Maximum concurrent workers.
//   - config: Configuration for timeouts.
//
// Outputs:
//   - *WorkerPool: A new worker pool.
func NewWorkerPool(concurrency int, config ConcurrencyConfig) *WorkerPool {
	return &WorkerPool{
		semaphore: NewSemaphore(concurrency),
		config:    config,
	}
}

// WorkItem represents a unit of work for the pool.
type WorkItem struct {
	// ID is a unique identifier for this work item.
	ID string

	// Work is the function to execute.
	// It receives the context and should respect cancellation.
	Work func(ctx context.Context) error
}

// WorkResult contains the result of processing a work item.
type WorkResult struct {
	// ID is the work item ID.
	ID string

	// Error is non-nil if the work failed.
	Error error

	// Duration is how long the work took.
	Duration time.Duration
}

// BatchResult contains results from a batch operation.
type BatchResult struct {
	// Results contains results for each work item.
	Results []WorkResult

	// SuccessCount is the number of successful items.
	SuccessCount int

	// FailureCount is the number of failed items.
	FailureCount int

	// TotalDuration is the total time for the batch.
	TotalDuration time.Duration

	// Cancelled indicates if the batch was cancelled.
	Cancelled bool
}

// ProgressCallback is called to report batch progress.
type ProgressCallback func(completed, total int, lastResult *WorkResult)

// ProcessBatch processes a batch of work items concurrently.
//
// Inputs:
//   - ctx: Context for cancellation.
//   - items: Work items to process.
//   - progress: Optional callback for progress updates.
//
// Outputs:
//   - *BatchResult: Results of the batch operation.
//
// The batch continues processing even if some items fail.
// Cancelling the context will stop new work but allow in-progress work to complete.
func (p *WorkerPool) ProcessBatch(ctx context.Context, items []WorkItem, progress ProgressCallback) *BatchResult {
	start := time.Now()

	// Create result channel
	resultCh := make(chan WorkResult, len(items))

	// Track completion
	var wg sync.WaitGroup
	var completed int32

	// Create deadline context if needed
	var cancel context.CancelFunc
	if p.config.TotalTimeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, p.config.TotalTimeout)
		defer cancel()
	}

	// Process items
	for _, item := range items {
		item := item // capture for goroutine

		wg.Add(1)
		go func() {
			defer wg.Done()

			// Acquire semaphore slot
			if err := p.semaphore.Acquire(ctx); err != nil {
				resultCh <- WorkResult{ID: item.ID, Error: err}
				return
			}
			defer p.semaphore.Release()

			// Create per-item context with timeout
			itemCtx := ctx
			var itemCancel context.CancelFunc
			if p.config.PerEntityTimeout > 0 {
				itemCtx, itemCancel = context.WithTimeout(ctx, p.config.PerEntityTimeout)
				defer itemCancel()
			}

			// Execute work
			itemStart := time.Now()
			err := item.Work(itemCtx)
			duration := time.Since(itemStart)

			result := WorkResult{
				ID:       item.ID,
				Error:    err,
				Duration: duration,
			}
			resultCh <- result

			// Update progress
			count := atomic.AddInt32(&completed, 1)
			if progress != nil {
				progress(int(count), len(items), &result)
			}
		}()
	}

	// Wait for all goroutines and close channel
	go func() {
		wg.Wait()
		close(resultCh)
	}()

	// Collect results
	results := make([]WorkResult, 0, len(items))
	successCount := 0
	failureCount := 0

	for result := range resultCh {
		results = append(results, result)
		if result.Error != nil {
			failureCount++
		} else {
			successCount++
		}
	}

	return &BatchResult{
		Results:       results,
		SuccessCount:  successCount,
		FailureCount:  failureCount,
		TotalDuration: time.Since(start),
		Cancelled:     ctx.Err() != nil,
	}
}

// MapReduce processes items in parallel and aggregates results.
//
// Inputs:
//   - ctx: Context for cancellation.
//   - items: Items to process.
//   - mapper: Function to process each item.
//
// Outputs:
//   - []R: Results for each item (in same order as input).
//   - error: Non-nil if any item failed.
//
// Type parameters:
//   - T: Input item type.
//   - R: Result type.
func MapReduce[T any, R any](
	ctx context.Context,
	pool *WorkerPool,
	items []T,
	mapper func(ctx context.Context, item T) (R, error),
) ([]R, error) {
	type indexedResult struct {
		index  int
		result R
		err    error
	}

	resultCh := make(chan indexedResult, len(items))
	var wg sync.WaitGroup

	// Process items in parallel
	for i, item := range items {
		i, item := i, item // capture
		wg.Add(1)

		go func() {
			defer wg.Done()

			if err := pool.semaphore.Acquire(ctx); err != nil {
				resultCh <- indexedResult{index: i, err: err}
				return
			}
			defer pool.semaphore.Release()

			result, err := mapper(ctx, item)
			resultCh <- indexedResult{index: i, result: result, err: err}
		}()
	}

	// Close channel when done
	go func() {
		wg.Wait()
		close(resultCh)
	}()

	// Collect results
	results := make([]R, len(items))
	var firstErr error

	for ir := range resultCh {
		if ir.err != nil && firstErr == nil {
			firstErr = ir.err
		}
		results[ir.index] = ir.result
	}

	return results, firstErr
}

// LLMWorkerPool is a worker pool configured for LLM operations.
func LLMWorkerPool(config ConcurrencyConfig) *WorkerPool {
	return NewWorkerPool(config.MaxLLMConcurrency, config)
}

// WeaviateWorkerPool is a worker pool configured for Weaviate operations.
func WeaviateWorkerPool(config ConcurrencyConfig) *WorkerPool {
	return NewWorkerPool(config.MaxWeaviateConcurrency, config)
}
