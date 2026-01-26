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
	"fmt"
	"log/slog"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// GoroutineTrackable defines the interface for goroutine lifecycle tracking.
//
// # Description
//
// GoroutineTrackable provides methods to track goroutine creation and
// completion, helping identify goroutine leaks and long-running operations.
//
// # Thread Safety
//
// Implementations must be safe for concurrent use from multiple goroutines.
type GoroutineTrackable interface {
	// Track registers a goroutine and returns a done function to call when complete.
	Track(name string) func()

	// TrackContext tracks a goroutine that respects context cancellation.
	TrackContext(ctx context.Context, name string, fn func(ctx context.Context)) error

	// Active returns the number of currently tracked goroutines.
	Active() int64

	// Peak returns the highest number of concurrent goroutines seen.
	Peak() int64

	// Stats returns detailed statistics about tracked goroutines.
	Stats() GoroutineStats

	// WaitAll waits for all tracked goroutines to complete with a timeout.
	WaitAll(timeout time.Duration) error
}

// GoroutineStats contains statistics about tracked goroutines.
//
// # Description
//
// Provides insights into goroutine usage patterns for debugging
// and monitoring.
type GoroutineStats struct {
	// Active is the current count of tracked goroutines.
	Active int64

	// Peak is the maximum concurrent goroutines seen.
	Peak int64

	// Total is the total number of goroutines tracked since creation.
	Total int64

	// LongRunning lists goroutines running longer than the threshold.
	LongRunning []TrackedGoroutine

	// RuntimeGoroutines is the total goroutine count from runtime.
	RuntimeGoroutines int
}

// TrackedGoroutine represents a currently active tracked goroutine.
type TrackedGoroutine struct {
	// Name identifies the goroutine.
	Name string

	// StartedAt is when the goroutine started.
	StartedAt time.Time

	// Duration is how long the goroutine has been running.
	Duration time.Duration
}

// GoroutineTrackerConfig configures goroutine tracking behavior.
//
// # Description
//
// Controls thresholds and logging for goroutine tracking.
//
// # Example
//
//	config := GoroutineTrackerConfig{
//	    LongRunningThreshold: 30 * time.Second,
//	    OnLongRunning: func(name string, duration time.Duration) {
//	        log.Printf("WARNING: Goroutine %s running for %v", name, duration)
//	    },
//	}
type GoroutineTrackerConfig struct {
	// LongRunningThreshold defines when a goroutine is considered long-running.
	// Default: 30 seconds
	LongRunningThreshold time.Duration

	// OnLongRunning is called when a goroutine exceeds the threshold.
	OnLongRunning func(name string, duration time.Duration)

	// OnComplete is called when a tracked goroutine completes.
	OnComplete func(name string, duration time.Duration)

	// Logger for debug output.
	// Default: slog.Default()
	Logger *slog.Logger
}

// DefaultGoroutineTrackerConfig returns sensible defaults.
//
// # Description
//
// Returns a configuration with a 30-second long-running threshold
// and default logging.
//
// # Outputs
//
//   - GoroutineTrackerConfig: Configuration with default values
func DefaultGoroutineTrackerConfig() GoroutineTrackerConfig {
	return GoroutineTrackerConfig{
		LongRunningThreshold: 30 * time.Second,
		Logger:               slog.Default(),
	}
}

// GoroutineTracker tracks goroutine lifecycles to detect leaks.
//
// # Description
//
// GoroutineTracker monitors goroutine creation and completion to help
// identify:
//   - Goroutine leaks (goroutines that never complete)
//   - Long-running goroutines (potential hangs)
//   - Peak concurrency (resource pressure)
//
// # Use Cases
//
//   - Debug goroutine leaks during development
//   - Monitor production for hanging operations
//   - Ensure graceful shutdown (wait for all goroutines)
//
// # Thread Safety
//
// GoroutineTracker is safe for concurrent use from multiple goroutines.
//
// # Limitations
//
//   - Only tracks goroutines explicitly registered with Track()
//   - Cannot track goroutines started without using the tracker
//   - Memory usage grows with number of active goroutines
//
// # Assumptions
//
//   - Callers will call the done function returned by Track()
//   - Names are descriptive enough for debugging
//
// # Example
//
//	tracker := NewGoroutineTracker(DefaultGoroutineTrackerConfig())
//
//	// Track a goroutine
//	go func() {
//	    done := tracker.Track("health-check")
//	    defer done()
//	    // ... do work
//	}()
//
//	// Check for leaks
//	stats := tracker.Stats()
//	if stats.Active > 0 {
//	    log.Printf("WARNING: %d goroutines still active", stats.Active)
//	}
type GoroutineTracker struct {
	config GoroutineTrackerConfig

	active     int64
	peak       int64
	total      int64
	goroutines map[uint64]*trackedEntry
	nextID     uint64
	mu         sync.RWMutex
	wg         sync.WaitGroup
}

// trackedEntry tracks a single goroutine.
type trackedEntry struct {
	name      string
	startedAt time.Time
}

// NewGoroutineTracker creates a new goroutine tracker.
//
// # Description
//
// Creates a tracker with the given configuration. The tracker
// starts monitoring immediately but only tracks goroutines that
// are explicitly registered with Track().
//
// # Inputs
//
//   - config: Configuration for tracking behavior
//
// # Outputs
//
//   - *GoroutineTracker: New tracker ready to use
//
// # Example
//
//	tracker := NewGoroutineTracker(GoroutineTrackerConfig{
//	    LongRunningThreshold: time.Minute,
//	    OnLongRunning: alertOncall,
//	})
func NewGoroutineTracker(config GoroutineTrackerConfig) *GoroutineTracker {
	if config.LongRunningThreshold <= 0 {
		config.LongRunningThreshold = 30 * time.Second
	}
	if config.Logger == nil {
		config.Logger = slog.Default()
	}

	return &GoroutineTracker{
		config:     config,
		goroutines: make(map[uint64]*trackedEntry),
	}
}

// Track registers a goroutine and returns a done function.
//
// # Description
//
// Registers a new goroutine with the given name. The returned function
// MUST be called when the goroutine completes (typically via defer).
//
// # Inputs
//
//   - name: Descriptive name for the goroutine (for debugging)
//
// # Outputs
//
//   - func(): Function to call when goroutine completes
//
// # Example
//
//	go func() {
//	    done := tracker.Track("process-request")
//	    defer done()
//	    processRequest()
//	}()
//
// # Important
//
// Always use defer to ensure done() is called even on panic:
//
//	done := tracker.Track("my-goroutine")
//	defer done()
func (t *GoroutineTracker) Track(name string) func() {
	t.mu.Lock()

	// Generate unique ID
	id := t.nextID
	t.nextID++

	// Record goroutine
	t.goroutines[id] = &trackedEntry{
		name:      name,
		startedAt: time.Now(),
	}

	// Update counters
	active := atomic.AddInt64(&t.active, 1)
	atomic.AddInt64(&t.total, 1)

	// Update peak
	for {
		peak := atomic.LoadInt64(&t.peak)
		if active <= peak {
			break
		}
		if atomic.CompareAndSwapInt64(&t.peak, peak, active) {
			break
		}
	}

	t.wg.Add(1)
	t.mu.Unlock()

	// Return done function
	return func() {
		t.mu.Lock()
		entry, exists := t.goroutines[id]
		if exists {
			duration := time.Since(entry.startedAt)
			delete(t.goroutines, id)

			atomic.AddInt64(&t.active, -1)
			t.wg.Done()

			// Check if it was long-running
			if duration > t.config.LongRunningThreshold {
				if t.config.OnLongRunning != nil {
					t.config.OnLongRunning(name, duration)
				} else {
					t.config.Logger.Warn("Long-running goroutine detected",
						"goroutine", name,
						"duration", duration,
						"threshold", t.config.LongRunningThreshold)
				}
			}

			// Call completion callback
			if t.config.OnComplete != nil {
				t.config.OnComplete(name, duration)
			}
		}
		t.mu.Unlock()
	}
}

// TrackContext tracks a goroutine that respects context cancellation.
//
// # Description
//
// Starts a goroutine that will be cancelled when the context is done.
// This is a convenience method that handles tracking and context
// cancellation together.
//
// # Inputs
//
//   - ctx: Context for cancellation
//   - name: Descriptive name for the goroutine
//   - fn: Function to run in the goroutine
//
// # Outputs
//
//   - error: nil (goroutine started successfully)
//
// # Example
//
//	tracker.TrackContext(ctx, "background-sync", func(ctx context.Context) {
//	    for {
//	        select {
//	        case <-ctx.Done():
//	            return
//	        case <-time.After(time.Minute):
//	            sync()
//	        }
//	    }
//	})
func (t *GoroutineTracker) TrackContext(ctx context.Context, name string, fn func(ctx context.Context)) error {
	go func() {
		done := t.Track(name)
		defer done()
		fn(ctx)
	}()
	return nil
}

// Active returns the number of currently tracked goroutines.
//
// # Description
//
// Returns the current count of goroutines that have been registered
// with Track() but have not yet called their done function.
//
// # Outputs
//
//   - int64: Number of active tracked goroutines
func (t *GoroutineTracker) Active() int64 {
	return atomic.LoadInt64(&t.active)
}

// Peak returns the highest number of concurrent goroutines seen.
//
// # Description
//
// Returns the maximum number of goroutines that were active at the
// same time since the tracker was created.
//
// # Outputs
//
//   - int64: Peak concurrent goroutine count
func (t *GoroutineTracker) Peak() int64 {
	return atomic.LoadInt64(&t.peak)
}

// Stats returns detailed statistics about tracked goroutines.
//
// # Description
//
// Returns comprehensive statistics including active count, peak,
// total, and details about long-running goroutines.
//
// # Outputs
//
//   - GoroutineStats: Detailed statistics
//
// # Example
//
//	stats := tracker.Stats()
//	if len(stats.LongRunning) > 0 {
//	    for _, g := range stats.LongRunning {
//	        log.Printf("Long-running: %s for %v", g.Name, g.Duration)
//	    }
//	}
func (t *GoroutineTracker) Stats() GoroutineStats {
	t.mu.RLock()
	defer t.mu.RUnlock()

	stats := GoroutineStats{
		Active:            atomic.LoadInt64(&t.active),
		Peak:              atomic.LoadInt64(&t.peak),
		Total:             atomic.LoadInt64(&t.total),
		RuntimeGoroutines: runtime.NumGoroutine(),
		LongRunning:       make([]TrackedGoroutine, 0),
	}

	now := time.Now()
	for _, entry := range t.goroutines {
		duration := now.Sub(entry.startedAt)
		if duration > t.config.LongRunningThreshold {
			stats.LongRunning = append(stats.LongRunning, TrackedGoroutine{
				Name:      entry.name,
				StartedAt: entry.startedAt,
				Duration:  duration,
			})
		}
	}

	return stats
}

// WaitAll waits for all tracked goroutines to complete.
//
// # Description
//
// Blocks until all tracked goroutines have called their done function
// or the timeout expires. Useful for graceful shutdown.
//
// # Inputs
//
//   - timeout: Maximum time to wait
//
// # Outputs
//
//   - error: nil if all completed, error if timeout
//
// # Example
//
//	// During shutdown
//	if err := tracker.WaitAll(30 * time.Second); err != nil {
//	    log.Printf("WARNING: %d goroutines still running", tracker.Active())
//	}
func (t *GoroutineTracker) WaitAll(timeout time.Duration) error {
	done := make(chan struct{})

	go func() {
		t.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-time.After(timeout):
		active := t.Active()
		return fmt.Errorf("timeout waiting for %d goroutines to complete", active)
	}
}

// ListActive returns details of all currently active goroutines.
//
// # Description
//
// Returns a snapshot of all tracked goroutines that are currently
// running. Useful for debugging and monitoring.
//
// # Outputs
//
//   - []TrackedGoroutine: Currently active goroutines
func (t *GoroutineTracker) ListActive() []TrackedGoroutine {
	t.mu.RLock()
	defer t.mu.RUnlock()

	now := time.Now()
	result := make([]TrackedGoroutine, 0, len(t.goroutines))

	for _, entry := range t.goroutines {
		result = append(result, TrackedGoroutine{
			Name:      entry.name,
			StartedAt: entry.startedAt,
			Duration:  now.Sub(entry.startedAt),
		})
	}

	return result
}

// Reset clears all statistics (for testing).
//
// # Description
//
// Resets counters but does NOT affect running goroutines.
// Primarily useful for testing.
func (t *GoroutineTracker) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()

	atomic.StoreInt64(&t.peak, atomic.LoadInt64(&t.active))
	atomic.StoreInt64(&t.total, atomic.LoadInt64(&t.active))
}

// Compile-time interface satisfaction check
var _ GoroutineTrackable = (*GoroutineTracker)(nil)
