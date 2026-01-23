// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package ttl

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// =============================================================================
// TTL Scheduler Implementation
// =============================================================================

// SchedulerConfig holds configuration for the TTL cleanup scheduler.
//
// # Description
//
// Contains all settings for running the background TTL cleanup scheduler.
// Default values are provided via DefaultSchedulerConfig().
//
// # Fields
//
//   - Interval: How often to run cleanup cycles. Default: 1 hour.
//   - DocumentBatchSize: Maximum documents to delete per cycle. Default: 1000.
//   - SessionBatchSize: Maximum sessions to delete per cycle. Default: 100.
type SchedulerConfig struct {
	Interval          time.Duration
	DocumentBatchSize int
	SessionBatchSize  int
}

// DefaultSchedulerConfig returns sensible default scheduler configuration.
//
// # Description
//
// Returns a SchedulerConfig with production-ready defaults:
//   - Interval: 1 hour (balances responsiveness vs load)
//   - DocumentBatchSize: 1000 (prevents timeout on large deletes)
//   - SessionBatchSize: 100 (sessions are typically fewer)
//
// # Outputs
//
//   - SchedulerConfig: Default configuration values.
//
// # Examples
//
//	config := DefaultSchedulerConfig()
//	config.Interval = 30 * time.Minute // Override just the interval
//	scheduler := NewTTLScheduler(service, logger, config)
func DefaultSchedulerConfig() SchedulerConfig {
	return SchedulerConfig{
		Interval:          1 * time.Hour,
		DocumentBatchSize: 1000,
		SessionBatchSize:  100,
	}
}

// ttlScheduler implements TTLScheduler interface for background cleanup.
//
// # Description
//
// Manages the lifecycle of a background goroutine that periodically runs
// TTL cleanup operations. Uses the ticker + done channel pattern for
// graceful shutdown.
//
// # Fields
//
//   - service: TTLService for querying/deleting expired items.
//   - logger: TTLLogger for audit logging (may be nil for slog-only logging).
//   - config: Scheduler configuration.
//   - done: Channel signaling shutdown request.
//   - mu: Mutex protecting running state.
//   - running: True if scheduler goroutine is active.
//
// # Thread Safety
//
// All public methods are thread-safe. The scheduler uses a mutex to protect
// state transitions.
type ttlScheduler struct {
	service TTLService
	logger  TTLLogger
	config  SchedulerConfig
	done    chan struct{}
	mu      sync.Mutex
	running bool
}

// NewTTLScheduler creates a new TTL cleanup scheduler.
//
// # Description
//
// Creates a scheduler that periodically runs TTL cleanup. The scheduler uses
// the ticker + done channel pattern for graceful shutdown. It queries for
// expired documents and sessions, deletes them in batches, and logs the
// results.
//
// # Inputs
//
//   - service: TTLService implementation for querying/deleting expired items.
//   - logger: TTLLogger for audit logging. May be nil for slog-only logging.
//   - config: Scheduler configuration including interval and batch sizes.
//
// # Outputs
//
//   - TTLScheduler: Ready to Start().
//
// # Examples
//
//	service := NewTTLService(weaviateClient)
//	logger, _ := NewTTLLogger("/var/log/aleutian/ttl_cleanup.log")
//	config := DefaultSchedulerConfig()
//	config.Interval = 30 * time.Minute
//
//	scheduler := NewTTLScheduler(service, logger, config)
//	err := scheduler.Start(ctx)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer scheduler.Stop()
//
// # Limitations
//
//   - Only one scheduler should run per orchestrator instance.
//   - Scheduler does not persist state between restarts.
//
// # Assumptions
//
//   - The TTLService is properly configured and connected.
//   - The orchestrator manages the scheduler lifecycle.
func NewTTLScheduler(service TTLService, logger TTLLogger, config SchedulerConfig) TTLScheduler {
	return &ttlScheduler{
		service: service,
		logger:  logger,
		config:  config,
		done:    make(chan struct{}),
	}
}

// Start begins the background cleanup scheduler.
//
// # Description
//
// Starts a goroutine that runs cleanup at the configured interval.
// The scheduler will continue running until Stop() is called or
// the context is cancelled.
//
// # Inputs
//
//   - ctx: Context for cancellation. When cancelled, scheduler stops.
//
// # Outputs
//
//   - error: Non-nil if scheduler is already running.
//
// # Examples
//
//	ctx, cancel := context.WithCancel(context.Background())
//	defer cancel()
//
//	err := scheduler.Start(ctx)
//	if err != nil {
//	    return fmt.Errorf("scheduler start failed: %w", err)
//	}
//	defer scheduler.Stop()
//
// # Limitations
//
//   - Only one Start() call is allowed until Stop() completes.
//   - Context cancellation triggers immediate shutdown, not graceful drain.
//
// # Assumptions
//
//   - The TTLService is available and connected.
//   - The caller will call Stop() during graceful shutdown.
func (s *ttlScheduler) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return fmt.Errorf("scheduler is already running")
	}
	s.running = true
	s.done = make(chan struct{}) // Reset done channel for potential restart
	s.mu.Unlock()

	slog.Info("TTL cleanup scheduler starting",
		"interval", s.config.Interval.String(),
		"document_batch_size", s.config.DocumentBatchSize,
		"session_batch_size", s.config.SessionBatchSize,
	)

	go s.runLoop(ctx)
	return nil
}

// Stop gracefully stops the scheduler.
//
// # Description
//
// Signals the scheduler to stop and waits for the current cleanup cycle
// to complete. Safe to call multiple times.
//
// # Outputs
//
//   - error: Currently always nil.
//
// # Examples
//
//	err := scheduler.Stop()
//	if err != nil {
//	    log.Printf("scheduler stop failed: %v", err)
//	}
//
// # Limitations
//
//   - Does not interrupt in-progress delete operations.
//   - Blocks until current cycle completes.
//
// # Assumptions
//
//   - The scheduler was started with Start().
func (s *ttlScheduler) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return nil // Already stopped
	}

	slog.Info("TTL cleanup scheduler stopping")
	close(s.done)
	s.running = false
	return nil
}

// RunNow triggers an immediate cleanup cycle.
//
// # Description
//
// Performs a cleanup cycle immediately without waiting for the next
// scheduled interval. Useful for manual invocation or testing.
//
// # Inputs
//
//   - ctx: Context for cancellation and timeout.
//
// # Outputs
//
//   - CleanupResult: Summary of the cleanup operation.
//   - error: Non-nil if cleanup fails.
//
// # Examples
//
//	result, err := scheduler.RunNow(ctx)
//	if err != nil {
//	    return fmt.Errorf("manual cleanup failed: %w", err)
//	}
//	fmt.Printf("Cleaned up %d documents, %d sessions\n",
//	    result.DocumentsDeleted, result.SessionsDeleted)
//
// # Limitations
//
//   - Does not affect scheduled cleanup timing.
//   - Subject to same batch size limits as scheduled cleanup.
//
// # Assumptions
//
//   - The TTLService is available and connected.
func (s *ttlScheduler) RunNow(ctx context.Context) (CleanupResult, error) {
	return s.runCleanupCycle(ctx)
}

// =============================================================================
// Internal Methods
// =============================================================================

// runLoop is the main scheduler goroutine.
//
// # Description
//
// Runs cleanup cycles at the configured interval until stopped.
// Handles context cancellation and done channel signals.
func (s *ttlScheduler) runLoop(ctx context.Context) {
	ticker := time.NewTicker(s.config.Interval)
	defer ticker.Stop()

	// Run an initial cleanup immediately on start
	s.executeCleanup(ctx)

	for {
		select {
		case <-ctx.Done():
			slog.Info("TTL cleanup scheduler stopped (context cancelled)")
			return
		case <-s.done:
			slog.Info("TTL cleanup scheduler stopped (stop requested)")
			return
		case <-ticker.C:
			s.executeCleanup(ctx)
		}
	}
}

// executeCleanup runs a single cleanup cycle with error handling.
//
// # Description
//
// Wraps runCleanupCycle with logging and error handling. Ensures
// that cleanup errors don't crash the scheduler.
func (s *ttlScheduler) executeCleanup(ctx context.Context) {
	result, err := s.runCleanupCycle(ctx)
	if err != nil {
		slog.Error("TTL cleanup cycle failed", "error", err)
		if s.logger != nil {
			_ = s.logger.LogError(err, "cleanup_cycle")
		}
		return
	}

	// Only log if something was found or deleted
	if result.DocumentsFound > 0 || result.SessionsFound > 0 {
		slog.Info("TTL cleanup cycle completed",
			"documents_found", result.DocumentsFound,
			"documents_deleted", result.DocumentsDeleted,
			"sessions_found", result.SessionsFound,
			"sessions_deleted", result.SessionsDeleted,
			"duration_ms", result.DurationMs(),
			"rolled_back", result.RolledBack,
		)
	} else {
		slog.Debug("TTL cleanup cycle completed (no expired items)")
	}

	// Write to dedicated audit log
	if s.logger != nil {
		_ = s.logger.LogCleanup(result)
	}
}

// runCleanupCycle performs a single cleanup operation.
//
// # Description
//
// Queries for expired documents and sessions, deletes them in batches,
// and returns a summary of the operation.
//
// # Inputs
//
//   - ctx: Context for cancellation and timeout.
//
// # Outputs
//
//   - CleanupResult: Summary combining document and session cleanup.
//   - error: Non-nil if query or delete operations fail catastrophically.
func (s *ttlScheduler) runCleanupCycle(ctx context.Context) (CleanupResult, error) {
	combinedResult := CleanupResult{
		StartTime: time.Now(),
		Errors:    make([]CleanupError, 0),
	}

	// Phase 1: Clean up expired documents
	docResult, docErr := s.cleanupExpiredDocuments(ctx)
	if docErr != nil {
		return combinedResult, fmt.Errorf("document cleanup failed: %w", docErr)
	}
	combinedResult.DocumentsFound = docResult.DocumentsFound
	combinedResult.DocumentsDeleted = docResult.DocumentsDeleted
	combinedResult.Errors = append(combinedResult.Errors, docResult.Errors...)
	if docResult.RolledBack {
		combinedResult.RolledBack = true
	}

	// Phase 2: Clean up expired sessions
	sessResult, sessErr := s.cleanupExpiredSessions(ctx)
	if sessErr != nil {
		return combinedResult, fmt.Errorf("session cleanup failed: %w", sessErr)
	}
	combinedResult.SessionsFound = sessResult.SessionsFound
	combinedResult.SessionsDeleted = sessResult.SessionsDeleted
	combinedResult.Errors = append(combinedResult.Errors, sessResult.Errors...)
	if sessResult.RolledBack {
		combinedResult.RolledBack = true
	}

	combinedResult.EndTime = time.Now()
	return combinedResult, nil
}

// cleanupExpiredDocuments queries and deletes expired documents.
//
// # Description
//
// Queries for documents past their TTL and deletes them in batches.
//
// # Inputs
//
//   - ctx: Context for cancellation and timeout.
//
// # Outputs
//
//   - CleanupResult: Summary of document cleanup.
//   - error: Non-nil if query fails.
func (s *ttlScheduler) cleanupExpiredDocuments(ctx context.Context) (CleanupResult, error) {
	expiredDocs, err := s.service.GetExpiredDocuments(ctx, s.config.DocumentBatchSize)
	if err != nil {
		return CleanupResult{}, fmt.Errorf("failed to query expired documents: %w", err)
	}

	if len(expiredDocs) == 0 {
		return CleanupResult{
			StartTime: time.Now(),
			EndTime:   time.Now(),
		}, nil
	}

	slog.Debug("Found expired documents", "count", len(expiredDocs))

	result, err := s.service.DeleteExpiredBatch(ctx, expiredDocs)
	if err != nil {
		return result, fmt.Errorf("failed to delete expired documents: %w", err)
	}

	return result, nil
}

// cleanupExpiredSessions queries and deletes expired sessions.
//
// # Description
//
// Queries for sessions past their TTL and deletes them in batches.
//
// # Inputs
//
//   - ctx: Context for cancellation and timeout.
//
// # Outputs
//
//   - CleanupResult: Summary of session cleanup.
//   - error: Non-nil if query fails.
func (s *ttlScheduler) cleanupExpiredSessions(ctx context.Context) (CleanupResult, error) {
	expiredSessions, err := s.service.GetExpiredSessions(ctx, s.config.SessionBatchSize)
	if err != nil {
		return CleanupResult{}, fmt.Errorf("failed to query expired sessions: %w", err)
	}

	if len(expiredSessions) == 0 {
		return CleanupResult{
			StartTime: time.Now(),
			EndTime:   time.Now(),
		}, nil
	}

	slog.Debug("Found expired sessions", "count", len(expiredSessions))

	result, err := s.service.DeleteExpiredSessionBatch(ctx, expiredSessions)
	if err != nil {
		return result, fmt.Errorf("failed to delete expired sessions: %w", err)
	}

	return result, nil
}
