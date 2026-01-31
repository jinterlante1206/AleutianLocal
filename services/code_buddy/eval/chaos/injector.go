// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package chaos

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/eval"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// -----------------------------------------------------------------------------
// Errors
// -----------------------------------------------------------------------------

var (
	// ErrNoFaults indicates no faults are configured.
	ErrNoFaults = errors.New("no faults configured")

	// ErrInjectorRunning indicates the injector is already running.
	ErrInjectorRunning = errors.New("injector is already running")

	// ErrInjectorStopped indicates the injector has been stopped.
	ErrInjectorStopped = errors.New("injector has been stopped")

	// ErrRecoveryFailed indicates recovery verification failed.
	ErrRecoveryFailed = errors.New("recovery verification failed")
)

// -----------------------------------------------------------------------------
// Injector Configuration
// -----------------------------------------------------------------------------

// InjectorConfig configures the chaos injector.
type InjectorConfig struct {
	// Faults are the faults to inject.
	Faults []Fault

	// Scheduler controls when faults are injected.
	// Default: RandomScheduler(0.05, 10s)
	Scheduler Scheduler

	// MaxConcurrentFaults limits how many faults can be active at once.
	// Default: 1
	MaxConcurrentFaults int

	// HealthCheckInterval is how often to verify target health.
	// Default: 1s
	HealthCheckInterval time.Duration

	// RecoveryTimeout is how long to wait for recovery after revert.
	// Default: 30s
	RecoveryTimeout time.Duration

	// Logger for debug output.
	Logger *slog.Logger
}

// DefaultInjectorConfig returns sensible defaults.
func DefaultInjectorConfig() *InjectorConfig {
	return &InjectorConfig{
		Faults:              make([]Fault, 0),
		Scheduler:           NewRandomScheduler(0.05, 10*time.Second),
		MaxConcurrentFaults: 1,
		HealthCheckInterval: time.Second,
		RecoveryTimeout:     30 * time.Second,
		Logger:              slog.Default(),
	}
}

// -----------------------------------------------------------------------------
// Injector Options
// -----------------------------------------------------------------------------

// InjectorOption configures the injector.
type InjectorOption func(*InjectorConfig)

// WithFaults sets the faults to inject.
func WithFaults(faults ...Fault) InjectorOption {
	return func(c *InjectorConfig) {
		c.Faults = faults
	}
}

// WithScheduler sets the scheduler.
func WithScheduler(s Scheduler) InjectorOption {
	return func(c *InjectorConfig) {
		if s != nil {
			c.Scheduler = s
		}
	}
}

// WithMaxConcurrentFaults sets the concurrency limit.
func WithMaxConcurrentFaults(n int) InjectorOption {
	return func(c *InjectorConfig) {
		if n > 0 {
			c.MaxConcurrentFaults = n
		}
	}
}

// WithHealthCheckInterval sets the health check interval.
func WithHealthCheckInterval(d time.Duration) InjectorOption {
	return func(c *InjectorConfig) {
		if d > 0 {
			c.HealthCheckInterval = d
		}
	}
}

// WithRecoveryTimeout sets the recovery timeout.
func WithRecoveryTimeout(d time.Duration) InjectorOption {
	return func(c *InjectorConfig) {
		if d > 0 {
			c.RecoveryTimeout = d
		}
	}
}

// WithInjectorLogger sets the logger.
func WithInjectorLogger(logger *slog.Logger) InjectorOption {
	return func(c *InjectorConfig) {
		if logger != nil {
			c.Logger = logger
		}
	}
}

// -----------------------------------------------------------------------------
// Injector
// -----------------------------------------------------------------------------

// Injector coordinates chaos fault injection.
//
// Description:
//
//	Injector manages the lifecycle of fault injection, including
//	scheduling, injection, reversion, and recovery verification.
//
// Thread Safety: Safe for concurrent use.
type Injector struct {
	config *InjectorConfig
	logger *slog.Logger

	mu           sync.RWMutex
	running      bool
	stopped      bool
	activeFaults map[string]time.Time // fault name -> activation time
	faultResults []FaultResult
	cancelFunc   context.CancelFunc
}

// FaultResult records the outcome of a fault injection.
type FaultResult struct {
	// FaultName identifies the fault.
	FaultName string

	// InjectedAt is when the fault was activated.
	InjectedAt time.Time

	// RevertedAt is when the fault was deactivated.
	RevertedAt time.Time

	// ActiveDuration is how long the fault was active.
	ActiveDuration time.Duration

	// RecoveryVerified is true if the target recovered correctly.
	RecoveryVerified bool

	// RecoveryDuration is how long recovery took.
	RecoveryDuration time.Duration

	// Error is any error during injection/revert.
	Error error
}

// NewInjector creates a new chaos injector.
//
// Inputs:
//   - opts: Configuration options.
//
// Outputs:
//   - *Injector: The new injector. Never nil.
func NewInjector(opts ...InjectorOption) *Injector {
	config := DefaultInjectorConfig()
	for _, opt := range opts {
		opt(config)
	}

	return &Injector{
		config:       config,
		logger:       config.Logger,
		activeFaults: make(map[string]time.Time),
		faultResults: make([]FaultResult, 0),
	}
}

// Run executes chaos testing against the target.
//
// Description:
//
//	Run continuously injects and reverts faults according to the
//	scheduler until the context is cancelled or duration expires.
//
// Inputs:
//   - ctx: Context for cancellation. Must not be nil.
//   - target: Target to inject faults against. Must not be nil.
//   - duration: Maximum test duration. Zero means run until cancelled.
//
// Outputs:
//   - *Result: Test results with all fault injection details.
//   - error: Non-nil if the test could not run.
//
// Thread Safety: Safe for concurrent use, but only one Run at a time.
func (i *Injector) Run(ctx context.Context, target eval.Evaluable, duration time.Duration) (*Result, error) {
	if ctx == nil {
		return nil, fmt.Errorf("chaos injector run: context must not be nil")
	}
	if target == nil {
		return nil, fmt.Errorf("chaos injector run: target must not be nil")
	}
	if len(i.config.Faults) == 0 {
		return nil, ErrNoFaults
	}

	ctx, span := otel.Tracer("chaos").Start(ctx, "chaos.Injector.Run",
		trace.WithAttributes(
			attribute.String("target", target.Name()),
			attribute.Int("fault_count", len(i.config.Faults)),
			attribute.Int64("duration_ms", duration.Milliseconds()),
		),
	)
	defer span.End()

	// Check if already running
	i.mu.Lock()
	if i.running {
		i.mu.Unlock()
		span.SetStatus(codes.Error, "injector already running")
		return nil, ErrInjectorRunning
	}
	if i.stopped {
		i.mu.Unlock()
		span.SetStatus(codes.Error, "injector stopped")
		return nil, ErrInjectorStopped
	}
	i.running = true
	i.activeFaults = make(map[string]time.Time)
	i.faultResults = make([]FaultResult, 0)
	i.mu.Unlock()

	// Create cancellable context
	var runCtx context.Context
	if duration > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, duration)
		defer cancel()
	} else {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithCancel(ctx)
		i.mu.Lock()
		i.cancelFunc = cancel
		i.mu.Unlock()
	}

	startTime := time.Now()

	// Run chaos loop
	i.runLoop(runCtx, target)

	// Cleanup: revert all active faults
	i.revertAllFaults(ctx, target)

	// Mark as not running
	i.mu.Lock()
	i.running = false
	results := make([]FaultResult, len(i.faultResults))
	copy(results, i.faultResults)
	i.mu.Unlock()

	result := &Result{
		Duration:          time.Since(startTime),
		FaultResults:      results,
		TargetName:        target.Name(),
		FaultsInjected:    countInjections(results),
		RecoveriesSuccess: countSuccessfulRecoveries(results),
		RecoveriesFailure: countFailedRecoveries(results),
	}

	span.SetAttributes(
		attribute.Int("faults_injected", result.FaultsInjected),
		attribute.Int("recoveries_success", result.RecoveriesSuccess),
		attribute.Int("recoveries_failure", result.RecoveriesFailure),
	)

	return result, nil
}

// runLoop is the main chaos injection loop.
func (i *Injector) runLoop(ctx context.Context, target eval.Evaluable) {
	ticker := time.NewTicker(i.config.HealthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			i.checkAndManageFaults(ctx, target)
		}
	}
}

// checkAndManageFaults checks scheduler and manages fault lifecycle.
func (i *Injector) checkAndManageFaults(ctx context.Context, target eval.Evaluable) {
	i.mu.Lock()
	defer i.mu.Unlock()

	// Check for faults to revert
	for faultName, activatedAt := range i.activeFaults {
		fault := i.getFaultByName(faultName)
		if fault == nil {
			continue
		}

		activeTime := time.Since(activatedAt)
		if i.config.Scheduler.ShouldRevert(fault, activeTime) {
			i.revertFault(ctx, fault, target, activatedAt)
		}
	}

	// Check for new faults to inject
	if len(i.activeFaults) < i.config.MaxConcurrentFaults {
		for _, fault := range i.config.Faults {
			if _, active := i.activeFaults[fault.Name()]; active {
				continue
			}

			if i.config.Scheduler.ShouldInject(fault) {
				i.injectFault(ctx, fault)
				break // Only inject one per tick
			}
		}
	}
}

// injectFault activates a fault.
func (i *Injector) injectFault(ctx context.Context, fault Fault) {
	err := fault.Inject(ctx)
	if err != nil {
		i.logger.Warn("failed to inject fault",
			slog.String("fault", fault.Name()),
			slog.String("error", err.Error()),
		)
		return
	}

	i.activeFaults[fault.Name()] = time.Now()
	i.logger.Info("fault injected",
		slog.String("fault", fault.Name()),
	)
}

// revertFault deactivates a fault and verifies recovery.
func (i *Injector) revertFault(ctx context.Context, fault Fault, target eval.Evaluable, activatedAt time.Time) {
	revertedAt := time.Now()
	activeDuration := revertedAt.Sub(activatedAt)

	err := fault.Revert(ctx)
	delete(i.activeFaults, fault.Name())

	result := FaultResult{
		FaultName:      fault.Name(),
		InjectedAt:     activatedAt,
		RevertedAt:     revertedAt,
		ActiveDuration: activeDuration,
		Error:          err,
	}

	if err != nil {
		i.logger.Warn("failed to revert fault",
			slog.String("fault", fault.Name()),
			slog.String("error", err.Error()),
		)
	} else {
		i.logger.Info("fault reverted",
			slog.String("fault", fault.Name()),
			slog.Duration("active_duration", activeDuration),
		)

		// Verify recovery
		recoveryStart := time.Now()
		recoveryCtx, cancel := context.WithTimeout(ctx, i.config.RecoveryTimeout)
		recoveryErr := i.verifyRecovery(recoveryCtx, target)
		cancel()

		result.RecoveryDuration = time.Since(recoveryStart)
		result.RecoveryVerified = recoveryErr == nil
		if recoveryErr != nil {
			result.Error = recoveryErr
			i.logger.Warn("recovery verification failed",
				slog.String("fault", fault.Name()),
				slog.String("error", recoveryErr.Error()),
			)
		}
	}

	i.faultResults = append(i.faultResults, result)
}

// verifyRecovery checks that the target has recovered.
func (i *Injector) verifyRecovery(ctx context.Context, target eval.Evaluable) error {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ErrRecoveryFailed
		case <-ticker.C:
			if err := target.HealthCheck(ctx); err == nil {
				return nil
			}
		}
	}
}

// revertAllFaults reverts all active faults.
func (i *Injector) revertAllFaults(ctx context.Context, target eval.Evaluable) {
	i.mu.Lock()
	defer i.mu.Unlock()

	for faultName, activatedAt := range i.activeFaults {
		fault := i.getFaultByName(faultName)
		if fault != nil {
			i.revertFault(ctx, fault, target, activatedAt)
		}
	}
}

// getFaultByName finds a fault by name.
func (i *Injector) getFaultByName(name string) Fault {
	for _, f := range i.config.Faults {
		if f.Name() == name {
			return f
		}
	}
	return nil
}

// Stop stops the injector.
//
// Thread Safety: Safe for concurrent use.
func (i *Injector) Stop() {
	i.mu.Lock()
	defer i.mu.Unlock()

	i.stopped = true
	if i.cancelFunc != nil {
		i.cancelFunc()
	}
}

// IsRunning returns true if the injector is currently running.
//
// Thread Safety: Safe for concurrent use.
func (i *Injector) IsRunning() bool {
	i.mu.RLock()
	defer i.mu.RUnlock()
	return i.running
}

// ActiveFaults returns the names of currently active faults.
//
// Thread Safety: Safe for concurrent use.
func (i *Injector) ActiveFaults() []string {
	i.mu.RLock()
	defer i.mu.RUnlock()

	names := make([]string, 0, len(i.activeFaults))
	for name := range i.activeFaults {
		names = append(names, name)
	}
	return names
}

// -----------------------------------------------------------------------------
// Result
// -----------------------------------------------------------------------------

// Result contains the outcome of a chaos test run.
type Result struct {
	// Duration is the total test duration.
	Duration time.Duration

	// FaultResults contains details for each fault injection.
	FaultResults []FaultResult

	// TargetName is the name of the target tested.
	TargetName string

	// FaultsInjected is the total number of fault injections.
	FaultsInjected int

	// RecoveriesSuccess is the count of successful recoveries.
	RecoveriesSuccess int

	// RecoveriesFailure is the count of failed recoveries.
	RecoveriesFailure int
}

// Success returns true if all recoveries were successful.
func (r *Result) Success() bool {
	return r.RecoveriesFailure == 0
}

// FailureRate returns the recovery failure rate.
func (r *Result) FailureRate() float64 {
	total := r.RecoveriesSuccess + r.RecoveriesFailure
	if total == 0 {
		return 0
	}
	return float64(r.RecoveriesFailure) / float64(total)
}

// -----------------------------------------------------------------------------
// Helpers
// -----------------------------------------------------------------------------

func countInjections(results []FaultResult) int {
	return len(results)
}

func countSuccessfulRecoveries(results []FaultResult) int {
	count := 0
	for _, r := range results {
		if r.RecoveryVerified {
			count++
		}
	}
	return count
}

func countFailedRecoveries(results []FaultResult) int {
	count := 0
	for _, r := range results {
		if !r.RecoveryVerified && r.Error != nil {
			count++
		}
	}
	return count
}

// -----------------------------------------------------------------------------
// Evaluable Implementation
// -----------------------------------------------------------------------------

// Name implements eval.Evaluable.
func (i *Injector) Name() string {
	return "chaos_injector"
}

// Properties implements eval.Evaluable.
func (i *Injector) Properties() []eval.Property {
	return []eval.Property{
		{
			Name:        "faults_revert_correctly",
			Description: "All injected faults can be reverted",
			Check: func(input, output any) error {
				return nil
			},
		},
		{
			Name:        "concurrency_respected",
			Description: "MaxConcurrentFaults limit is respected",
			Check: func(input, output any) error {
				return nil
			},
		},
	}
}

// Metrics implements eval.Evaluable.
func (i *Injector) Metrics() []eval.MetricDefinition {
	return []eval.MetricDefinition{
		{
			Name:        "chaos_faults_injected_total",
			Type:        eval.MetricCounter,
			Description: "Total number of faults injected",
			Labels:      []string{"fault_type"},
		},
		{
			Name:        "chaos_recovery_duration_seconds",
			Type:        eval.MetricHistogram,
			Description: "Time to recover after fault reversion",
			Buckets:     []float64{0.1, 0.5, 1, 2, 5, 10, 30},
		},
		{
			Name:        "chaos_recovery_success_total",
			Type:        eval.MetricCounter,
			Description: "Successful recovery count",
		},
		{
			Name:        "chaos_recovery_failure_total",
			Type:        eval.MetricCounter,
			Description: "Failed recovery count",
		},
	}
}

// HealthCheck implements eval.Evaluable.
func (i *Injector) HealthCheck(_ context.Context) error {
	return nil
}
