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
	"sync"
	"time"
)

// StateAuditor defines the interface for detecting state drift.
//
// # Description
//
// StateAuditor prevents split-brain scenarios where cached state diverges
// from actual state. It periodically validates cached values against
// their source of truth.
//
// # Thread Safety
//
// Implementations must be safe for concurrent use.
type StateAuditor interface {
	// RegisterState registers a piece of state for auditing.
	RegisterState(name string, opts StateRegistration) error

	// AuditAll checks all registered states for drift.
	AuditAll(ctx context.Context) (AuditResult, error)

	// AuditOne checks a specific state for drift.
	AuditOne(ctx context.Context, name string) (StateAudit, error)

	// StartPeriodicAudit starts background audit loop.
	StartPeriodicAudit(interval time.Duration)

	// StopPeriodicAudit stops background audit loop.
	StopPeriodicAudit()

	// GetDriftReport returns the latest drift findings.
	GetDriftReport() DriftReport

	// OnDrift registers a callback for drift detection.
	OnDrift(callback DriftCallback)
}

// StateRegistration configures state monitoring.
//
// # Description
//
// Defines how a piece of state should be validated.
//
// # Example
//
//	reg := StateRegistration{
//	    GetCached:    func() (interface{}, error) { return cachedValue, nil },
//	    GetActual:    func(ctx context.Context) (interface{}, error) { return fetchFromDB(ctx) },
//	    CompareFunc:  reflect.DeepEqual,
//	    MaxStaleness: 30 * time.Second,
//	    OnDrift:      handleDrift,
//	}
type StateRegistration struct {
	// GetCached returns the cached value.
	GetCached func() (interface{}, error)

	// GetActual fetches the actual value from source of truth.
	GetActual func(ctx context.Context) (interface{}, error)

	// CompareFunc compares cached and actual values.
	// Default: reflect.DeepEqual
	CompareFunc func(cached, actual interface{}) bool

	// MaxStaleness is how old the cache can be before considered stale.
	// Default: 1 minute
	MaxStaleness time.Duration

	// OnDrift is called when drift is detected.
	OnDrift func(cached, actual interface{})

	// Critical indicates if drift should halt operations.
	Critical bool

	// Description explains what this state represents.
	Description string
}

// StateAudit contains the result of auditing a single state.
//
// # Description
//
// Provides details about whether a state has drifted.
type StateAudit struct {
	// Name identifies the state.
	Name string

	// HasDrift indicates if cached differs from actual.
	HasDrift bool

	// CachedValue is the value from cache.
	CachedValue interface{}

	// ActualValue is the value from source of truth.
	ActualValue interface{}

	// LastChecked is when this audit was performed.
	LastChecked time.Time

	// Latency is how long the audit took.
	Latency time.Duration

	// Error is any error that occurred during audit.
	Error error

	// IsCritical indicates if this drift is critical.
	IsCritical bool
}

// AuditResult contains the results of auditing all states.
//
// # Description
//
// Aggregates audit results across all registered states.
type AuditResult struct {
	// StartTime is when the audit began.
	StartTime time.Time

	// EndTime is when the audit completed.
	EndTime time.Time

	// Audits contains individual audit results.
	Audits []StateAudit

	// TotalChecked is how many states were audited.
	TotalChecked int

	// DriftCount is how many states had drift.
	DriftCount int

	// CriticalDriftCount is how many critical states had drift.
	CriticalDriftCount int

	// ErrorCount is how many audits failed.
	ErrorCount int
}

// HasCriticalDrift returns true if any critical state has drifted.
func (r AuditResult) HasCriticalDrift() bool {
	return r.CriticalDriftCount > 0
}

// DriftReport is a summary of detected drift.
type DriftReport struct {
	// LastFullAudit is when a full audit was last run.
	LastFullAudit time.Time

	// DriftingStates lists states currently showing drift.
	DriftingStates []string

	// TotalAudits is total audits performed.
	TotalAudits int64

	// TotalDriftDetected is total drift detections.
	TotalDriftDetected int64
}

// DriftCallback is called when drift is detected.
type DriftCallback func(audit StateAudit)

// StateAuditConfig configures the state auditor.
//
// # Description
//
// Global configuration for state auditing.
//
// # Example
//
//	config := StateAuditConfig{
//	    DefaultMaxStaleness: time.Minute,
//	    AuditTimeout:        10 * time.Second,
//	}
type StateAuditConfig struct {
	// DefaultMaxStaleness is the default staleness threshold.
	// Default: 1 minute
	DefaultMaxStaleness time.Duration

	// AuditTimeout is the timeout for individual audits.
	// Default: 10 seconds
	AuditTimeout time.Duration

	// ContinueOnError continues auditing other states if one fails.
	// Default: true
	ContinueOnError bool

	// LogDrift logs drift detection.
	// Default: true
	LogDrift bool
}

// DefaultStateAuditConfig returns sensible defaults.
//
// # Description
//
// Returns configuration with reasonable default values.
//
// # Outputs
//
//   - StateAuditConfig: Default configuration
func DefaultStateAuditConfig() StateAuditConfig {
	return StateAuditConfig{
		DefaultMaxStaleness: time.Minute,
		AuditTimeout:        10 * time.Second,
		ContinueOnError:     true,
		LogDrift:            true,
	}
}

// DefaultStateAuditor implements StateAuditor.
//
// # Description
//
// Detects split-brain scenarios where cached state diverges from
// actual state. Useful for systems that cache database records,
// configuration, or external API responses.
//
// # Use Cases
//
//   - Cached service health status vs actual status
//   - Cached configuration vs file/database config
//   - Cached container status vs docker inspect
//
// # Thread Safety
//
// DefaultStateAuditor is safe for concurrent use.
//
// # Limitations
//
//   - Cannot detect partial drift (object fields)
//   - Requires source of truth to be available
//
// # Example
//
//	auditor := NewStateAuditor(DefaultStateAuditConfig())
//	auditor.RegisterState("service_health", StateRegistration{
//	    GetCached: func() (interface{}, error) { return cache.Get("health") },
//	    GetActual: func(ctx context.Context) (interface{}, error) {
//	        return checkActualHealth(ctx)
//	    },
//	    Critical: true,
//	})
//	result, _ := auditor.AuditAll(ctx)
type DefaultStateAuditor struct {
	config StateAuditConfig

	// Registered states
	states map[string]registeredState
	mu     sync.RWMutex

	// Audit statistics
	lastFullAudit      time.Time
	totalAudits        int64
	totalDriftDetected int64
	driftingStates     map[string]bool
	statsMu            sync.Mutex

	// Callbacks
	driftCallbacks []DriftCallback
	callbackMu     sync.RWMutex

	// Periodic audit
	stopCh chan struct{}
	wg     sync.WaitGroup
}

type registeredState struct {
	registration StateRegistration
	lastAudit    time.Time
	lastDrift    bool
}

// NewStateAuditor creates a new state auditor.
//
// # Description
//
// Creates an auditor with the specified configuration.
//
// # Inputs
//
//   - config: Configuration for audit behavior
//
// # Outputs
//
//   - *DefaultStateAuditor: New auditor
func NewStateAuditor(config StateAuditConfig) *DefaultStateAuditor {
	if config.DefaultMaxStaleness <= 0 {
		config.DefaultMaxStaleness = time.Minute
	}
	if config.AuditTimeout <= 0 {
		config.AuditTimeout = 10 * time.Second
	}

	return &DefaultStateAuditor{
		config:         config,
		states:         make(map[string]registeredState),
		driftingStates: make(map[string]bool),
	}
}

// RegisterState registers a piece of state for auditing.
//
// # Description
//
// Registers a state with its getter functions and comparison logic.
//
// # Inputs
//
//   - name: Unique identifier for this state
//   - opts: Registration options
//
// # Outputs
//
//   - error: Non-nil if registration fails
func (a *DefaultStateAuditor) RegisterState(name string, opts StateRegistration) error {
	if name == "" {
		return fmt.Errorf("state name cannot be empty")
	}
	if opts.GetCached == nil {
		return fmt.Errorf("GetCached function is required")
	}
	if opts.GetActual == nil {
		return fmt.Errorf("GetActual function is required")
	}

	// Apply defaults
	if opts.MaxStaleness <= 0 {
		opts.MaxStaleness = a.config.DefaultMaxStaleness
	}
	if opts.CompareFunc == nil {
		opts.CompareFunc = defaultCompare
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	a.states[name] = registeredState{
		registration: opts,
	}

	return nil
}

// AuditAll checks all registered states for drift.
//
// # Description
//
// Iterates through all registered states and checks each for drift.
//
// # Inputs
//
//   - ctx: Context for cancellation
//
// # Outputs
//
//   - AuditResult: Aggregated audit results
//   - error: Non-nil if context cancelled or critical failure
func (a *DefaultStateAuditor) AuditAll(ctx context.Context) (AuditResult, error) {
	result := AuditResult{
		StartTime: time.Now(),
	}

	a.mu.RLock()
	names := make([]string, 0, len(a.states))
	for name := range a.states {
		names = append(names, name)
	}
	a.mu.RUnlock()

	for _, name := range names {
		select {
		case <-ctx.Done():
			result.EndTime = time.Now()
			return result, ctx.Err()
		default:
		}

		audit, err := a.AuditOne(ctx, name)
		if err != nil && !a.config.ContinueOnError {
			result.EndTime = time.Now()
			return result, err
		}

		result.Audits = append(result.Audits, audit)
		result.TotalChecked++

		if audit.Error != nil {
			result.ErrorCount++
		}
		if audit.HasDrift {
			result.DriftCount++
			if audit.IsCritical {
				result.CriticalDriftCount++
			}
		}
	}

	result.EndTime = time.Now()

	// Update stats
	a.statsMu.Lock()
	a.lastFullAudit = result.EndTime
	a.totalAudits += int64(result.TotalChecked)
	a.totalDriftDetected += int64(result.DriftCount)
	a.statsMu.Unlock()

	return result, nil
}

// AuditOne checks a specific state for drift.
//
// # Description
//
// Fetches both cached and actual values and compares them.
//
// # Inputs
//
//   - ctx: Context for cancellation
//   - name: The state to audit
//
// # Outputs
//
//   - StateAudit: Audit result
//   - error: Non-nil if state not found or audit fails
func (a *DefaultStateAuditor) AuditOne(ctx context.Context, name string) (StateAudit, error) {
	audit := StateAudit{
		Name:        name,
		LastChecked: time.Now(),
	}

	a.mu.RLock()
	state, exists := a.states[name]
	a.mu.RUnlock()

	if !exists {
		audit.Error = fmt.Errorf("state not registered: %s", name)
		return audit, audit.Error
	}

	audit.IsCritical = state.registration.Critical

	// Create timeout context
	timeoutCtx, cancel := context.WithTimeout(ctx, a.config.AuditTimeout)
	defer cancel()

	start := time.Now()

	// Get cached value
	cached, err := state.registration.GetCached()
	if err != nil {
		audit.Error = fmt.Errorf("failed to get cached value: %w", err)
		audit.Latency = time.Since(start)
		return audit, nil
	}
	audit.CachedValue = cached

	// Get actual value
	actual, err := state.registration.GetActual(timeoutCtx)
	if err != nil {
		audit.Error = fmt.Errorf("failed to get actual value: %w", err)
		audit.Latency = time.Since(start)
		return audit, nil
	}
	audit.ActualValue = actual

	audit.Latency = time.Since(start)

	// Compare
	audit.HasDrift = !state.registration.CompareFunc(cached, actual)

	// Update state tracking
	a.mu.Lock()
	stateEntry := a.states[name]
	stateEntry.lastAudit = audit.LastChecked
	stateEntry.lastDrift = audit.HasDrift
	a.states[name] = stateEntry
	a.mu.Unlock()

	// Track drifting states
	a.statsMu.Lock()
	if audit.HasDrift {
		a.driftingStates[name] = true
	} else {
		delete(a.driftingStates, name)
	}
	a.statsMu.Unlock()

	// Handle drift
	if audit.HasDrift {
		// Call registration callback
		if state.registration.OnDrift != nil {
			state.registration.OnDrift(cached, actual)
		}

		// Call global callbacks
		a.callbackMu.RLock()
		callbacks := append([]DriftCallback{}, a.driftCallbacks...)
		a.callbackMu.RUnlock()

		for _, cb := range callbacks {
			cb(audit)
		}
	}

	return audit, nil
}

// StartPeriodicAudit starts background audit loop.
//
// # Description
//
// Starts a goroutine that periodically audits all states.
//
// # Inputs
//
//   - interval: How often to audit
func (a *DefaultStateAuditor) StartPeriodicAudit(interval time.Duration) {
	if a.stopCh != nil {
		return // Already running
	}

	a.stopCh = make(chan struct{})
	a.wg.Add(1)

	go func() {
		defer a.wg.Done()

		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-a.stopCh:
				return
			case <-ticker.C:
				a.AuditAll(context.Background())
			}
		}
	}()
}

// StopPeriodicAudit stops background audit loop.
//
// # Description
//
// Stops the periodic audit goroutine.
func (a *DefaultStateAuditor) StopPeriodicAudit() {
	if a.stopCh == nil {
		return
	}

	close(a.stopCh)
	a.wg.Wait()
	a.stopCh = nil
}

// GetDriftReport returns the latest drift findings.
//
// # Description
//
// Returns a summary of current drift status.
//
// # Outputs
//
//   - DriftReport: Current drift report
func (a *DefaultStateAuditor) GetDriftReport() DriftReport {
	a.statsMu.Lock()
	defer a.statsMu.Unlock()

	drifting := make([]string, 0, len(a.driftingStates))
	for name := range a.driftingStates {
		drifting = append(drifting, name)
	}

	return DriftReport{
		LastFullAudit:      a.lastFullAudit,
		DriftingStates:     drifting,
		TotalAudits:        a.totalAudits,
		TotalDriftDetected: a.totalDriftDetected,
	}
}

// OnDrift registers a callback for drift detection.
//
// # Description
//
// The callback is invoked whenever drift is detected in any state.
//
// # Inputs
//
//   - callback: Function to call on drift
func (a *DefaultStateAuditor) OnDrift(callback DriftCallback) {
	a.callbackMu.Lock()
	defer a.callbackMu.Unlock()

	a.driftCallbacks = append(a.driftCallbacks, callback)
}

// defaultCompare is a simple equality check.
func defaultCompare(cached, actual interface{}) bool {
	// Simple string comparison for common types
	return fmt.Sprintf("%v", cached) == fmt.Sprintf("%v", actual)
}

// Compile-time interface check
var _ StateAuditor = (*DefaultStateAuditor)(nil)

// ErrStateDrift is returned when critical state drift is detected.
var ErrStateDrift = fmt.Errorf("state drift detected")

// AuditContainerState creates a state registration for container status.
//
// # Description
//
// Helper for auditing container status against cached value.
//
// # Inputs
//
//   - containerName: Name of the container
//   - getCached: Function to get cached status
//   - getActual: Function to get actual status
//
// # Outputs
//
//   - StateRegistration: Registration for container state
func AuditContainerState(containerName string, getCached func() (string, error), getActual func(ctx context.Context) (string, error)) StateRegistration {
	return StateRegistration{
		GetCached: func() (interface{}, error) {
			return getCached()
		},
		GetActual: func(ctx context.Context) (interface{}, error) {
			return getActual(ctx)
		},
		CompareFunc: func(cached, actual interface{}) bool {
			return cached.(string) == actual.(string)
		},
		MaxStaleness: 30 * time.Second,
		Critical:     true,
		Description:  fmt.Sprintf("Container %s status", containerName),
	}
}

// AuditConfigState creates a state registration for configuration.
//
// # Description
//
// Helper for auditing configuration files against cached values.
//
// # Inputs
//
//   - configPath: Path to configuration file
//   - getCached: Function to get cached config hash
//   - getActual: Function to get actual config hash
//
// # Outputs
//
//   - StateRegistration: Registration for config state
func AuditConfigState(configPath string, getCached func() (string, error), getActual func(ctx context.Context) (string, error)) StateRegistration {
	return StateRegistration{
		GetCached: func() (interface{}, error) {
			return getCached()
		},
		GetActual: func(ctx context.Context) (interface{}, error) {
			return getActual(ctx)
		},
		CompareFunc: func(cached, actual interface{}) bool {
			return cached.(string) == actual.(string)
		},
		MaxStaleness: time.Minute,
		Critical:     false,
		Description:  fmt.Sprintf("Config file %s", configPath),
	}
}
