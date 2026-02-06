// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package cancel

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// CancellationController manages hierarchical cancellation for the CRS system.
//
// Thread Safety: Safe for concurrent use.
type CancellationController struct {
	config ControllerConfig
	logger *slog.Logger

	// Sessions indexed by ID
	sessions   map[string]*SessionContext
	sessionsMu sync.RWMutex

	// All contexts indexed by ID for fast lookup
	contexts   map[string]Cancellable
	contextsMu sync.RWMutex

	// Deadlock detector
	deadlockDetector *DeadlockDetector

	// Resource monitor
	resourceMonitor *ResourceMonitor

	// Shutdown coordination
	closed     bool
	closedMu   sync.RWMutex
	shutdownCh chan struct{}
	shutdownWg sync.WaitGroup

	// Metrics
	metrics *Metrics
}

// NewController creates a new CancellationController.
//
// Description:
//
//	Creates and initializes a new cancellation controller with the given configuration.
//	The controller starts background goroutines for deadlock detection and resource
//	monitoring.
//
// Inputs:
//   - config: Controller configuration. Zero values use defaults.
//   - logger: Logger for cancellation events. If nil, uses slog.Default().
//
// Outputs:
//   - *CancellationController: The created controller. Never nil.
//   - error: Non-nil if configuration is invalid.
//
// Example:
//
//	ctrl, err := cancel.NewController(cancel.ControllerConfig{
//	    DefaultTimeout: 30 * time.Second,
//	}, slog.Default())
//	if err != nil {
//	    return err
//	}
//	defer ctrl.Close()
//
// Thread Safety: The returned controller is safe for concurrent use.
func NewController(config ControllerConfig, logger *slog.Logger) (*CancellationController, error) {
	// Apply defaults first
	config.ApplyDefaults()

	// Validate configuration
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	if logger == nil {
		logger = slog.Default()
	}

	c := &CancellationController{
		config:     config,
		logger:     logger.With(slog.String("component", "cancel_controller")),
		sessions:   make(map[string]*SessionContext),
		contexts:   make(map[string]Cancellable),
		shutdownCh: make(chan struct{}),
	}

	// Initialize metrics
	if config.EnableMetrics {
		c.metrics = NewMetrics()
	}

	// Initialize deadlock detector
	c.deadlockDetector = NewDeadlockDetector(c, config.ProgressCheckInterval, config.DeadlockMultiplier)

	// Initialize resource monitor (but don't start - started per session with limits)
	c.resourceMonitor = NewResourceMonitor(c)

	// Start background workers
	c.shutdownWg.Add(1)
	go c.deadlockDetector.Run(c.shutdownCh, &c.shutdownWg)

	return c, nil
}

// NewSession creates a new cancellable session context.
//
// Description:
//
//	Creates a top-level session context that can contain activities and algorithms.
//	The session inherits cancellation from the parent context.
//
// Inputs:
//   - parent: Parent context. Must not be nil.
//   - config: Session configuration. ID is required.
//
// Outputs:
//   - *SessionContext: The created session context. Never nil on success.
//   - error: Non-nil if parent is nil, controller is closed, or config is invalid.
//
// Thread Safety: Safe for concurrent use.
func (c *CancellationController) NewSession(parent context.Context, config SessionConfig) (*SessionContext, error) {
	if parent == nil {
		return nil, ErrNilContext
	}

	c.closedMu.RLock()
	if c.closed {
		c.closedMu.RUnlock()
		return nil, ErrControllerClosed
	}
	c.closedMu.RUnlock()

	// Apply defaults and validate
	config.ApplyDefaults()
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid session config: %w", err)
	}

	// Create session
	session := newSessionContext(parent, config, c)

	// Register session
	c.sessionsMu.Lock()
	c.sessions[config.ID] = session
	c.sessionsMu.Unlock()

	c.contextsMu.Lock()
	c.contexts[config.ID] = session
	c.contextsMu.Unlock()

	// Start resource monitor if limits are configured
	if config.ResourceLimits.HasLimits() {
		c.shutdownWg.Add(1)
		go c.resourceMonitor.MonitorSession(session, c.shutdownCh, &c.shutdownWg)
	}

	c.logger.Info("session created",
		slog.String("session_id", config.ID),
		slog.Duration("timeout", config.Timeout),
		slog.Duration("progress_interval", config.ProgressInterval),
	)

	if c.metrics != nil {
		c.metrics.SessionsCreated.Inc()
	}

	return session, nil
}

// registerContext registers a context for tracking.
func (c *CancellationController) registerContext(ctx Cancellable) {
	c.contextsMu.Lock()
	defer c.contextsMu.Unlock()
	c.contexts[ctx.ID()] = ctx
}

// unregisterContext removes a context from tracking.
func (c *CancellationController) unregisterContext(id string) {
	c.contextsMu.Lock()
	defer c.contextsMu.Unlock()
	delete(c.contexts, id)
}

// Cancel initiates cancellation for the specified target.
//
// Description:
//
//	Cancels the context with the given ID. The ID can refer to a session,
//	activity, or algorithm. Cancellation propagates to all children.
//
// Inputs:
//   - id: The ID of the context to cancel.
//   - reason: The reason for cancellation.
//
// Outputs:
//   - error: Non-nil if the ID is not found or controller is closed.
//
// Thread Safety: Safe for concurrent use.
func (c *CancellationController) Cancel(id string, reason CancelReason) error {
	c.closedMu.RLock()
	if c.closed {
		c.closedMu.RUnlock()
		return ErrControllerClosed
	}
	c.closedMu.RUnlock()

	c.contextsMu.RLock()
	ctx, ok := c.contexts[id]
	c.contextsMu.RUnlock()

	if !ok {
		// Try to find by partial match (algorithm name without full path)
		c.contextsMu.RLock()
		for ctxID, ctxVal := range c.contexts {
			if ctxVal.Level() == LevelAlgorithm {
				if alg, ok := ctxVal.(*AlgorithmContext); ok && alg.Name() == id {
					ctx = ctxVal
					id = ctxID
					break
				}
			}
		}
		c.contextsMu.RUnlock()

		if ctx == nil {
			return fmt.Errorf("%w: %s", ErrAlgorithmNotFound, id)
		}
	}

	// Set timestamp if not provided
	if reason.Timestamp == 0 {
		reason.Timestamp = time.Now().UnixMilli()
	}

	c.logger.Info("cancelling context",
		slog.String("id", id),
		slog.String("level", ctx.Level().String()),
		slog.String("type", reason.Type.String()),
		slog.String("message", reason.Message),
	)

	ctx.Cancel(reason)

	if c.metrics != nil {
		c.metrics.CancelTotal.WithLabelValues(
			reason.Type.String(),
			ctx.Level().String(),
			reason.Component,
		).Inc()
	}

	return nil
}

// CancelAll cancels all active contexts immediately.
//
// Description:
//
//	Emergency stop for all sessions, activities, and algorithms.
//
// Inputs:
//   - reason: The reason for cancellation.
//
// Thread Safety: Safe for concurrent use.
func (c *CancellationController) CancelAll(reason CancelReason) {
	c.closedMu.RLock()
	if c.closed {
		c.closedMu.RUnlock()
		return
	}
	c.closedMu.RUnlock()

	if reason.Timestamp == 0 {
		reason.Timestamp = time.Now().UnixMilli()
	}

	c.logger.Warn("cancelling all contexts",
		slog.String("type", reason.Type.String()),
		slog.String("message", reason.Message),
	)

	c.sessionsMu.RLock()
	sessions := make([]*SessionContext, 0, len(c.sessions))
	for _, s := range c.sessions {
		sessions = append(sessions, s)
	}
	c.sessionsMu.RUnlock()

	for _, s := range sessions {
		s.Cancel(reason)
	}

	if c.metrics != nil {
		c.metrics.CancelAllTotal.Inc()
	}
}

// Status returns the current status of all contexts.
//
// Description:
//
//	Returns a snapshot of the current state of all sessions and their children.
//
// Outputs:
//   - *ControllerStatus: Current status. Never nil.
//
// Thread Safety: Safe for concurrent use. Returns a snapshot.
func (c *CancellationController) Status() *ControllerStatus {
	c.sessionsMu.RLock()
	defer c.sessionsMu.RUnlock()

	status := &ControllerStatus{
		Sessions: make([]Status, 0, len(c.sessions)),
	}

	for _, s := range c.sessions {
		sessionStatus := s.Status()
		status.Sessions = append(status.Sessions, sessionStatus)

		// Count states
		countStates(&sessionStatus, &status.TotalActive, &status.TotalCancelled, &status.TotalCompleted)
	}

	return status
}

// countStates recursively counts context states.
func countStates(status *Status, active, cancelled, completed *int) {
	switch status.State {
	case StateRunning, StateCancelling:
		*active++
	case StateCancelled:
		*cancelled++
	case StateDone:
		*completed++
	}

	for i := range status.Children {
		countStates(&status.Children[i], active, cancelled, completed)
	}
}

// Shutdown gracefully shuts down the controller.
//
// Description:
//
//	Cancels all contexts, waits for graceful shutdown, then force kills
//	any remaining contexts. Blocks until complete or context is cancelled.
//
// Inputs:
//   - ctx: Context for the shutdown operation itself.
//
// Outputs:
//   - *ShutdownResult: Results of the shutdown operation.
//   - error: Non-nil if ctx was cancelled before shutdown completed.
//
// Thread Safety: Safe for concurrent use. Only the first call performs shutdown.
func (c *CancellationController) Shutdown(ctx context.Context) (*ShutdownResult, error) {
	c.closedMu.Lock()
	if c.closed {
		c.closedMu.Unlock()
		return &ShutdownResult{Success: true}, nil
	}
	c.closed = true
	c.closedMu.Unlock()

	startTime := time.Now()
	result := &ShutdownResult{}

	c.logger.Info("initiating shutdown")

	// Signal all background workers to stop
	close(c.shutdownCh)

	// Cancel all sessions with shutdown reason
	c.CancelAll(CancelReason{
		Type:      CancelShutdown,
		Message:   "Controller shutdown",
		Timestamp: time.Now().UnixMilli(),
	})

	// Wait for graceful shutdown
	graceDone := make(chan struct{})
	go func() {
		c.shutdownWg.Wait()
		close(graceDone)
	}()

	select {
	case <-graceDone:
		// Graceful shutdown completed
	case <-time.After(c.config.GracePeriod):
		// Grace period expired, collect partial results
		c.logger.Warn("grace period expired, collecting partial results")
		result.PartialResultsCollected = c.collectAllPartialResults()
	case <-ctx.Done():
		return result, ctx.Err()
	}

	// Force kill if needed
	forceKillDone := make(chan struct{})
	go func() {
		c.forceKillRemaining()
		close(forceKillDone)
	}()

	select {
	case <-forceKillDone:
		// Force kill completed
	case <-time.After(c.config.ForceKillTimeout - c.config.GracePeriod):
		c.logger.Error("force kill timeout exceeded")
		result.ForceKilled = c.countRunningContexts()
	case <-ctx.Done():
		return result, ctx.Err()
	}

	result.Success = true
	result.Duration = time.Since(startTime)

	c.logger.Info("shutdown complete",
		slog.Duration("duration", result.Duration),
		slog.Int("partial_collected", result.PartialResultsCollected),
		slog.Int("force_killed", result.ForceKilled),
	)

	return result, nil
}

// collectAllPartialResults attempts to collect partial results from all contexts.
func (c *CancellationController) collectAllPartialResults() int {
	c.contextsMu.RLock()
	defer c.contextsMu.RUnlock()

	collected := 0
	for _, ctx := range c.contexts {
		if base, ok := ctx.(*AlgorithmContext); ok {
			if _, err := base.collectPartialResult(); err == nil {
				collected++
			}
		}
	}
	return collected
}

// forceKillRemaining force-terminates any remaining contexts.
func (c *CancellationController) forceKillRemaining() {
	c.contextsMu.RLock()
	defer c.contextsMu.RUnlock()

	for id, ctx := range c.contexts {
		if !ctx.State().IsTerminal() {
			c.logger.Warn("force killing context",
				slog.String("id", id),
				slog.String("state", ctx.State().String()),
			)
			ctx.Cancel(CancelReason{
				Type:      CancelShutdown,
				Message:   "Force killed during shutdown",
				Timestamp: time.Now().UnixMilli(),
			})
			if c.metrics != nil {
				c.metrics.ForceKilledTotal.Inc()
			}
		}
	}
}

// countRunningContexts returns the count of non-terminal contexts.
func (c *CancellationController) countRunningContexts() int {
	c.contextsMu.RLock()
	defer c.contextsMu.RUnlock()

	count := 0
	for _, ctx := range c.contexts {
		if !ctx.State().IsTerminal() {
			count++
		}
	}
	return count
}

// Close releases all resources held by the controller.
//
// Description:
//
//	Should be called when the controller is no longer needed.
//	Calls Shutdown internally if not already called.
//
// Outputs:
//   - error: Non-nil if shutdown encountered errors.
//
// Thread Safety: Safe for concurrent use. Idempotent.
func (c *CancellationController) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), c.config.ForceKillTimeout)
	defer cancel()

	_, err := c.Shutdown(ctx)
	return err
}

// GetContext returns the context with the given ID.
func (c *CancellationController) GetContext(id string) (Cancellable, bool) {
	c.contextsMu.RLock()
	defer c.contextsMu.RUnlock()
	ctx, ok := c.contexts[id]
	return ctx, ok
}

// GetSession returns the session with the given ID.
func (c *CancellationController) GetSession(id string) (*SessionContext, bool) {
	c.sessionsMu.RLock()
	defer c.sessionsMu.RUnlock()
	s, ok := c.sessions[id]
	return s, ok
}

// getProgressInterval returns the progress interval for a context.
func (c *CancellationController) getProgressInterval(ctx Cancellable) time.Duration {
	switch v := ctx.(type) {
	case *SessionContext:
		return v.ProgressInterval()
	case *ActivityContext:
		return v.Session().ProgressInterval()
	case *AlgorithmContext:
		return v.Activity().Session().ProgressInterval()
	default:
		return c.config.DefaultTimeout / 10 // Fallback
	}
}
