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
	"log/slog"
	"sync"
	"time"
)

// ShutdownPhase represents a phase in the graceful shutdown protocol.
type ShutdownPhase int

const (
	// PhaseSignal is the initial phase where cancel signals are sent.
	PhaseSignal ShutdownPhase = iota

	// PhaseCollect is when partial results are being collected.
	PhaseCollect

	// PhaseForceKill is when non-responsive contexts are force-terminated.
	PhaseForceKill

	// PhaseReport is when the shutdown report is being generated.
	PhaseReport

	// PhaseComplete indicates shutdown is complete.
	PhaseComplete
)

// String returns the string representation of the shutdown phase.
func (p ShutdownPhase) String() string {
	switch p {
	case PhaseSignal:
		return "signal"
	case PhaseCollect:
		return "collect"
	case PhaseForceKill:
		return "force_kill"
	case PhaseReport:
		return "report"
	case PhaseComplete:
		return "complete"
	default:
		return "unknown"
	}
}

// ShutdownCoordinator manages the graceful shutdown protocol.
//
// The protocol follows this timeline:
//
//	T+0ms     Signal cancel (set cancellation flag)
//	T+100ms   Algorithms should detect ctx.Done()
//	T+500ms   Collect partial results from cooperative algorithms
//	T+2000ms  Force kill algorithms that haven't responded
//	T+5000ms  Generate final report and release resources
//
// Thread Safety: Safe for concurrent use.
type ShutdownCoordinator struct {
	controller       *CancellationController
	logger           *slog.Logger
	gracePeriod      time.Duration
	forceKillTimeout time.Duration

	phase   ShutdownPhase
	phaseMu sync.RWMutex
	started bool
	startMu sync.Mutex
}

// NewShutdownCoordinator creates a new shutdown coordinator.
//
// Description:
//
//	Creates a coordinator that manages the graceful shutdown protocol
//	with configurable timeouts.
//
// Inputs:
//   - controller: The cancellation controller.
//   - gracePeriod: Time to wait for graceful shutdown before collecting partial results.
//   - forceKillTimeout: Time to wait after grace period before force killing.
//
// Outputs:
//   - *ShutdownCoordinator: The created coordinator. Never nil.
func NewShutdownCoordinator(controller *CancellationController, gracePeriod, forceKillTimeout time.Duration) *ShutdownCoordinator {
	return &ShutdownCoordinator{
		controller:       controller,
		logger:           controller.logger.With(slog.String("subsystem", "shutdown_coordinator")),
		gracePeriod:      gracePeriod,
		forceKillTimeout: forceKillTimeout,
		phase:            PhaseSignal,
	}
}

// Phase returns the current shutdown phase.
func (s *ShutdownCoordinator) Phase() ShutdownPhase {
	s.phaseMu.RLock()
	defer s.phaseMu.RUnlock()
	return s.phase
}

// setPhase updates the shutdown phase.
func (s *ShutdownCoordinator) setPhase(phase ShutdownPhase) {
	s.phaseMu.Lock()
	defer s.phaseMu.Unlock()
	s.phase = phase
	s.logger.Info("shutdown phase changed",
		slog.String("phase", phase.String()),
	)
}

// Execute runs the graceful shutdown protocol.
//
// Description:
//
//	Executes all phases of the shutdown protocol in order:
//	1. Signal all contexts to cancel
//	2. Wait for grace period, collecting partial results
//	3. Force kill remaining contexts
//	4. Generate and return the shutdown report
//
// Inputs:
//   - ctx: Context for the shutdown operation. If cancelled, shutdown aborts.
//   - reason: The reason for shutdown.
//
// Outputs:
//   - *ShutdownResult: The results of the shutdown operation.
//   - error: Non-nil if ctx was cancelled or another error occurred.
//
// Thread Safety: Safe for concurrent use. Only the first call executes shutdown.
func (s *ShutdownCoordinator) Execute(ctx context.Context, reason CancelReason) (*ShutdownResult, error) {
	// Ensure only one shutdown executes
	s.startMu.Lock()
	if s.started {
		s.startMu.Unlock()
		return &ShutdownResult{Success: true}, nil
	}
	s.started = true
	s.startMu.Unlock()

	startTime := time.Now()
	result := &ShutdownResult{}
	var errs []error

	// Phase 1: Signal
	s.setPhase(PhaseSignal)
	if err := s.executeSignalPhase(ctx, reason); err != nil {
		errs = append(errs, err)
		if ctx.Err() != nil {
			result.Errors = errs
			return result, ctx.Err()
		}
	}

	// Wait for algorithms to detect cancellation (100ms)
	select {
	case <-time.After(100 * time.Millisecond):
	case <-ctx.Done():
		result.Errors = errs
		return result, ctx.Err()
	}

	// Phase 2: Collect partial results
	s.setPhase(PhaseCollect)
	collected, err := s.executeCollectPhase(ctx)
	if err != nil {
		errs = append(errs, err)
		if ctx.Err() != nil {
			result.Errors = errs
			return result, ctx.Err()
		}
	}
	result.PartialResultsCollected = collected

	// Wait for grace period
	graceRemaining := s.gracePeriod - time.Since(startTime)
	if graceRemaining > 0 {
		select {
		case <-time.After(graceRemaining):
		case <-ctx.Done():
			result.Errors = errs
			return result, ctx.Err()
		}
	}

	// Phase 3: Force kill remaining
	s.setPhase(PhaseForceKill)
	killed, err := s.executeForceKillPhase(ctx)
	if err != nil {
		errs = append(errs, err)
		if ctx.Err() != nil {
			result.Errors = errs
			return result, ctx.Err()
		}
	}
	result.ForceKilled = killed

	// Phase 4: Report
	s.setPhase(PhaseReport)
	// Reporting is just logging, already done throughout

	// Phase 5: Complete
	s.setPhase(PhaseComplete)

	result.Success = true
	result.Duration = time.Since(startTime)
	result.Errors = errs

	s.logger.Info("shutdown complete",
		slog.Duration("duration", result.Duration),
		slog.Int("partial_collected", result.PartialResultsCollected),
		slog.Int("force_killed", result.ForceKilled),
		slog.Int("errors", len(errs)),
	)

	return result, nil
}

// executeSignalPhase sends cancel signals to all contexts.
func (s *ShutdownCoordinator) executeSignalPhase(ctx context.Context, reason CancelReason) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	s.logger.Info("signaling all contexts to cancel")
	s.controller.CancelAll(reason)
	return nil
}

// executeCollectPhase collects partial results from all contexts.
func (s *ShutdownCoordinator) executeCollectPhase(ctx context.Context) (int, error) {
	select {
	case <-ctx.Done():
		return 0, ctx.Err()
	default:
	}

	s.logger.Info("collecting partial results")

	s.controller.contextsMu.RLock()
	contexts := make([]Cancellable, 0, len(s.controller.contexts))
	for _, c := range s.controller.contexts {
		contexts = append(contexts, c)
	}
	s.controller.contextsMu.RUnlock()

	collected := 0
	var wg sync.WaitGroup
	var mu sync.Mutex

	for _, c := range contexts {
		if alg, ok := c.(*AlgorithmContext); ok {
			wg.Add(1)
			go func(a *AlgorithmContext) {
				defer wg.Done()

				// Try to collect partial result with timeout
				collectCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
				defer cancel()

				done := make(chan struct{})
				go func() {
					if _, err := a.collectPartialResult(); err == nil {
						mu.Lock()
						collected++
						mu.Unlock()
					}
					close(done)
				}()

				select {
				case <-done:
				case <-collectCtx.Done():
				}
			}(alg)
		}
	}

	wg.Wait()

	s.logger.Info("partial results collected",
		slog.Int("count", collected),
	)

	if s.controller.metrics != nil {
		s.controller.metrics.PartialResultsCollected.Add(float64(collected))
	}

	return collected, nil
}

// executeForceKillPhase force-terminates remaining contexts.
func (s *ShutdownCoordinator) executeForceKillPhase(ctx context.Context) (int, error) {
	select {
	case <-ctx.Done():
		return 0, ctx.Err()
	default:
	}

	s.logger.Info("force killing remaining contexts")

	s.controller.contextsMu.RLock()
	contexts := make([]Cancellable, 0)
	for _, c := range s.controller.contexts {
		if !c.State().IsTerminal() {
			contexts = append(contexts, c)
		}
	}
	s.controller.contextsMu.RUnlock()

	killed := 0
	for _, c := range contexts {
		s.logger.Warn("force killing context",
			slog.String("id", c.ID()),
			slog.String("level", c.Level().String()),
			slog.String("state", c.State().String()),
		)

		// Force mark as cancelled
		switch v := c.(type) {
		case *SessionContext:
			v.markCancelled()
		case *ActivityContext:
			v.markCancelled()
		case *AlgorithmContext:
			v.markCancelled()
		}

		killed++
	}

	s.logger.Info("force kill complete",
		slog.Int("killed", killed),
	)

	if s.controller.metrics != nil && killed > 0 {
		s.controller.metrics.ForceKilledTotal.Add(float64(killed))
	}

	return killed, nil
}

// WaitForCompletion waits for all contexts to reach a terminal state.
//
// Description:
//
//	Blocks until all contexts are either cancelled or done, or until
//	the provided context is cancelled.
//
// Inputs:
//   - ctx: Context for the wait operation.
//   - checkInterval: How often to check context states.
//
// Outputs:
//   - error: Non-nil if ctx was cancelled before all contexts completed.
func (s *ShutdownCoordinator) WaitForCompletion(ctx context.Context, checkInterval time.Duration) error {
	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if s.allContextsTerminal() {
				return nil
			}
		}
	}
}

// allContextsTerminal returns true if all contexts are in a terminal state.
func (s *ShutdownCoordinator) allContextsTerminal() bool {
	s.controller.contextsMu.RLock()
	defer s.controller.contextsMu.RUnlock()

	for _, c := range s.controller.contexts {
		if !c.State().IsTerminal() {
			return false
		}
	}
	return true
}
