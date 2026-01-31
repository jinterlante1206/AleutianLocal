// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package mcts

import (
	"sync"
	"time"
)

// TreeDegradation represents the current degradation level.
type TreeDegradation int

const (
	// TreeDegradationNormal is full MCTS with all features.
	TreeDegradationNormal TreeDegradation = iota

	// TreeDegradationReduced has fewer expansions and quick simulation only.
	TreeDegradationReduced

	// TreeDegradationMinimal has single expansion and no simulation.
	TreeDegradationMinimal

	// TreeDegradationLinear falls back to linear planning.
	TreeDegradationLinear
)

// String returns a human-readable degradation level name.
func (d TreeDegradation) String() string {
	switch d {
	case TreeDegradationNormal:
		return "normal"
	case TreeDegradationReduced:
		return "reduced"
	case TreeDegradationMinimal:
		return "minimal"
	case TreeDegradationLinear:
		return "linear"
	default:
		return "unknown"
	}
}

// DegradationConfig configures degradation behavior.
type DegradationConfig struct {
	// ConsecutiveFailuresForReduced is failures before reduced mode (default: 2).
	ConsecutiveFailuresForReduced int

	// ConsecutiveFailuresForMinimal is failures before minimal mode (default: 4).
	ConsecutiveFailuresForMinimal int

	// ConsecutiveFailuresForLinear is failures before linear fallback (default: 6).
	ConsecutiveFailuresForLinear int

	// CircuitOpenDegradation is the level when circuit opens (default: Linear).
	CircuitOpenDegradation TreeDegradation

	// SuccessesForRecovery is successes to recover one level (default: 3).
	SuccessesForRecovery int

	// NotifyOnDegradation enables callbacks on degradation events.
	NotifyOnDegradation bool
}

// DefaultDegradationConfig returns sensible defaults.
func DefaultDegradationConfig() DegradationConfig {
	return DegradationConfig{
		ConsecutiveFailuresForReduced: 2,
		ConsecutiveFailuresForMinimal: 4,
		ConsecutiveFailuresForLinear:  6,
		CircuitOpenDegradation:        TreeDegradationLinear,
		SuccessesForRecovery:          3,
		NotifyOnDegradation:           true,
	}
}

// DegradationStatus contains current status.
type DegradationStatus struct {
	Level                string    `json:"level"`
	ConsecutiveFailures  int       `json:"consecutive_failures"`
	ConsecutiveSuccesses int       `json:"consecutive_successes"`
	LastDegradation      time.Time `json:"last_degradation,omitempty"`
	CircuitState         string    `json:"circuit_state"`
}

// DegradationManager manages degradation state.
//
// It progressively reduces tree mode capabilities in response to failures,
// and recovers after sustained successes. This provides graceful degradation
// when LLM calls are failing or expensive.
//
// Thread Safety: Safe for concurrent use.
type DegradationManager struct {
	config         DegradationConfig
	circuitBreaker *CircuitBreaker

	mu                   sync.RWMutex
	currentLevel         TreeDegradation
	consecutiveFailures  int
	consecutiveSuccesses int
	lastDegradation      time.Time

	// Callbacks
	onDegradation func(from, to TreeDegradation, reason string)
}

// NewDegradationManager creates a degradation manager.
//
// Inputs:
//   - config: Degradation configuration.
//   - cb: Optional circuit breaker for coordination (may be nil).
//
// Outputs:
//   - *DegradationManager: Ready to use degradation manager.
//
// Thread Safety: The returned manager is safe for concurrent use.
func NewDegradationManager(config DegradationConfig, cb *CircuitBreaker) *DegradationManager {
	return &DegradationManager{
		config:         config,
		circuitBreaker: cb,
		currentLevel:   TreeDegradationNormal,
	}
}

// OnDegradation sets a callback for degradation events.
func (m *DegradationManager) OnDegradation(fn func(from, to TreeDegradation, reason string)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onDegradation = fn
}

// RecordSuccess records a successful operation.
func (m *DegradationManager) RecordSuccess() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.consecutiveFailures = 0
	m.consecutiveSuccesses++

	// Try to recover
	if m.consecutiveSuccesses >= m.config.SuccessesForRecovery {
		m.tryRecover()
		m.consecutiveSuccesses = 0
	}
}

// RecordFailure records a failed operation.
func (m *DegradationManager) RecordFailure() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.consecutiveSuccesses = 0
	m.consecutiveFailures++

	// Check for degradation
	m.checkDegradation()
}

// checkDegradation evaluates if we need to degrade.
// Must be called with lock held.
func (m *DegradationManager) checkDegradation() {
	// Circuit breaker takes priority
	if m.circuitBreaker != nil && m.circuitBreaker.State() == CircuitOpen {
		m.degradeTo(m.config.CircuitOpenDegradation, "circuit breaker open")
		return
	}

	// Progressive degradation based on failures
	if m.consecutiveFailures >= m.config.ConsecutiveFailuresForLinear {
		m.degradeTo(TreeDegradationLinear, "consecutive failures")
	} else if m.consecutiveFailures >= m.config.ConsecutiveFailuresForMinimal {
		m.degradeTo(TreeDegradationMinimal, "consecutive failures")
	} else if m.consecutiveFailures >= m.config.ConsecutiveFailuresForReduced {
		m.degradeTo(TreeDegradationReduced, "consecutive failures")
	}
}

// degradeTo changes to a lower degradation level.
// Must be called with lock held.
func (m *DegradationManager) degradeTo(level TreeDegradation, reason string) {
	if level <= m.currentLevel {
		return // Already at or below this level
	}

	from := m.currentLevel
	m.currentLevel = level
	m.lastDegradation = time.Now()

	if m.config.NotifyOnDegradation && m.onDegradation != nil {
		// Release lock before callback to avoid deadlock
		fn := m.onDegradation
		m.mu.Unlock()
		fn(from, level, reason)
		m.mu.Lock()
	}
}

// tryRecover attempts to recover to a higher level.
// Must be called with lock held.
func (m *DegradationManager) tryRecover() {
	if m.currentLevel == TreeDegradationNormal {
		return // Already at best level
	}

	// Check if circuit breaker is still open
	if m.circuitBreaker != nil && m.circuitBreaker.State() == CircuitOpen {
		return // Can't recover while circuit is open
	}

	// Only recover one level at a time
	from := m.currentLevel
	switch m.currentLevel {
	case TreeDegradationLinear:
		m.currentLevel = TreeDegradationMinimal
	case TreeDegradationMinimal:
		m.currentLevel = TreeDegradationReduced
	case TreeDegradationReduced:
		m.currentLevel = TreeDegradationNormal
	}

	if m.config.NotifyOnDegradation && m.onDegradation != nil {
		fn := m.onDegradation
		m.mu.Unlock()
		fn(from, m.currentLevel, "recovery after successes")
		m.mu.Lock()
	}
}

// CurrentLevel returns the current degradation level.
func (m *DegradationManager) CurrentLevel() TreeDegradation {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.currentLevel
}

// GetBudgetForLevel returns an appropriate budget for the current degradation level.
//
// Outputs:
//   - TreeBudgetConfig: Budget configuration appropriate for the current level.
func (m *DegradationManager) GetBudgetForLevel() TreeBudgetConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()

	switch m.currentLevel {
	case TreeDegradationNormal:
		return DefaultTreeBudgetConfig()
	case TreeDegradationReduced:
		return TreeBudgetConfig{
			MaxNodes:      5,
			MaxDepth:      3,
			MaxExpansions: 2,
			TimeLimit:     15 * time.Second,
			LLMTokenLimit: 2000,
			LLMCallLimit:  5,
			CostLimitUSD:  0.05,
		}
	case TreeDegradationMinimal:
		return TreeBudgetConfig{
			MaxNodes:      2,
			MaxDepth:      2,
			MaxExpansions: 1,
			TimeLimit:     10 * time.Second,
			LLMTokenLimit: 1000,
			LLMCallLimit:  2,
			CostLimitUSD:  0.02,
		}
	case TreeDegradationLinear:
		// Linear mode doesn't use tree budget, but return minimal for interface
		return TreeBudgetConfig{
			MaxNodes:      1,
			MaxDepth:      1,
			MaxExpansions: 1,
			TimeLimit:     5 * time.Second,
			LLMTokenLimit: 500,
			LLMCallLimit:  1,
			CostLimitUSD:  0.01,
		}
	default:
		return DefaultTreeBudgetConfig()
	}
}

// ShouldUseLinearFallback returns true if we should skip tree mode entirely.
func (m *DegradationManager) ShouldUseLinearFallback() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.currentLevel == TreeDegradationLinear
}

// Status returns current degradation status for observability.
func (m *DegradationManager) Status() DegradationStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	circuitState := "closed"
	if m.circuitBreaker != nil {
		circuitState = m.circuitBreaker.State().String()
	}

	return DegradationStatus{
		Level:                m.currentLevel.String(),
		ConsecutiveFailures:  m.consecutiveFailures,
		ConsecutiveSuccesses: m.consecutiveSuccesses,
		LastDegradation:      m.lastDegradation,
		CircuitState:         circuitState,
	}
}

// Reset resets the degradation manager to normal level.
func (m *DegradationManager) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.currentLevel = TreeDegradationNormal
	m.consecutiveFailures = 0
	m.consecutiveSuccesses = 0
	m.lastDegradation = time.Time{}
}

// Config returns the degradation configuration.
func (m *DegradationManager) Config() DegradationConfig {
	return m.config
}
