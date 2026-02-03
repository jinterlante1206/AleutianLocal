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
	"sync"
	"time"
)

// -----------------------------------------------------------------------------
// Scheduler Interface
// -----------------------------------------------------------------------------

// Scheduler controls when faults are injected and reverted.
//
// Thread Safety: Implementations must be safe for concurrent use.
type Scheduler interface {
	// ShouldInject returns true if a fault should be injected now.
	ShouldInject(fault Fault) bool

	// ShouldRevert returns true if an active fault should be reverted.
	ShouldRevert(fault Fault, activeTime time.Duration) bool

	// NextInjectionTime returns the next time a fault should be considered.
	// Returns zero time if scheduling should happen immediately/continuously.
	NextInjectionTime() time.Time

	// Reset resets the scheduler state.
	Reset()
}

// -----------------------------------------------------------------------------
// Random Scheduler
// -----------------------------------------------------------------------------

// RandomScheduler injects faults with a configurable probability.
//
// Description:
//
//	RandomScheduler uses random sampling to decide when to inject faults.
//	Each call to ShouldInject has an independent probability of returning true.
//
// Thread Safety: Safe for concurrent use.
type RandomScheduler struct {
	mu           sync.Mutex
	probability  float64
	maxActiveDur time.Duration
	seed         uint64
}

// NewRandomScheduler creates a random scheduler.
//
// Inputs:
//   - probability: Probability of injection per check (0.0 to 1.0).
//   - maxActiveDuration: Maximum time a fault stays active. Zero means no limit.
//
// Outputs:
//   - *RandomScheduler: The new scheduler. Never nil.
func NewRandomScheduler(probability float64, maxActiveDuration time.Duration) *RandomScheduler {
	if probability < 0 {
		probability = 0
	}
	if probability > 1 {
		probability = 1
	}
	return &RandomScheduler{
		probability:  probability,
		maxActiveDur: maxActiveDuration,
		seed:         uint64(time.Now().UnixNano()),
	}
}

// ShouldInject implements Scheduler.
func (s *RandomScheduler) ShouldInject(_ Fault) bool {
	s.mu.Lock()
	s.seed = s.seed*6364136223846793005 + 1442695040888963407
	seed := s.seed
	s.mu.Unlock()

	return float64(seed%1000000)/1000000 < s.probability
}

// ShouldRevert implements Scheduler.
func (s *RandomScheduler) ShouldRevert(_ Fault, activeTime time.Duration) bool {
	if s.maxActiveDur > 0 && activeTime >= s.maxActiveDur {
		return true
	}
	return false
}

// NextInjectionTime implements Scheduler.
func (s *RandomScheduler) NextInjectionTime() time.Time {
	return time.Time{} // Check continuously
}

// Reset implements Scheduler.
func (s *RandomScheduler) Reset() {
	s.mu.Lock()
	s.seed = uint64(time.Now().UnixNano())
	s.mu.Unlock()
}

// -----------------------------------------------------------------------------
// Periodic Scheduler
// -----------------------------------------------------------------------------

// PeriodicScheduler injects faults at regular intervals.
//
// Description:
//
//	PeriodicScheduler injects faults at a fixed interval with a
//	fixed duration. Useful for predictable chaos testing.
//
// Thread Safety: Safe for concurrent use.
type PeriodicScheduler struct {
	mu            sync.Mutex
	interval      time.Duration
	duration      time.Duration
	lastInjection time.Time
}

// NewPeriodicScheduler creates a periodic scheduler.
//
// Inputs:
//   - interval: Time between fault injections.
//   - duration: How long each fault stays active.
//
// Outputs:
//   - *PeriodicScheduler: The new scheduler. Never nil.
func NewPeriodicScheduler(interval, duration time.Duration) *PeriodicScheduler {
	return &PeriodicScheduler{
		interval: interval,
		duration: duration,
	}
}

// ShouldInject implements Scheduler.
func (s *PeriodicScheduler) ShouldInject(_ Fault) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	if s.lastInjection.IsZero() || now.Sub(s.lastInjection) >= s.interval {
		s.lastInjection = now
		return true
	}
	return false
}

// ShouldRevert implements Scheduler.
func (s *PeriodicScheduler) ShouldRevert(_ Fault, activeTime time.Duration) bool {
	return activeTime >= s.duration
}

// NextInjectionTime implements Scheduler.
func (s *PeriodicScheduler) NextInjectionTime() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.lastInjection.IsZero() {
		return time.Now()
	}
	return s.lastInjection.Add(s.interval)
}

// Reset implements Scheduler.
func (s *PeriodicScheduler) Reset() {
	s.mu.Lock()
	s.lastInjection = time.Time{}
	s.mu.Unlock()
}

// -----------------------------------------------------------------------------
// Scenario Scheduler
// -----------------------------------------------------------------------------

// ScenarioEvent represents a planned fault injection.
type ScenarioEvent struct {
	// Offset is the time from scenario start to inject.
	Offset time.Duration

	// Duration is how long the fault should be active.
	Duration time.Duration

	// FaultName is the name of the fault to inject.
	FaultName string
}

// ScenarioScheduler runs a predefined sequence of fault injections.
//
// Description:
//
//	ScenarioScheduler executes a scripted chaos test where fault
//	injections and durations are predetermined.
//
// Thread Safety: Safe for concurrent use.
type ScenarioScheduler struct {
	mu         sync.Mutex
	events     []ScenarioEvent
	startTime  time.Time
	injected   map[string]time.Time // FaultName -> injection time
	eventIndex int
}

// NewScenarioScheduler creates a scenario scheduler.
//
// Inputs:
//   - events: Sequence of fault injection events.
//
// Outputs:
//   - *ScenarioScheduler: The new scheduler. Never nil.
func NewScenarioScheduler(events []ScenarioEvent) *ScenarioScheduler {
	return &ScenarioScheduler{
		events:   events,
		injected: make(map[string]time.Time),
	}
}

// Start begins the scenario from now.
func (s *ScenarioScheduler) Start() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.startTime = time.Now()
	s.eventIndex = 0
	s.injected = make(map[string]time.Time)
}

// ShouldInject implements Scheduler.
func (s *ScenarioScheduler) ShouldInject(fault Fault) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.startTime.IsZero() {
		return false
	}

	elapsed := time.Since(s.startTime)

	// Find events for this fault that should trigger
	for i := s.eventIndex; i < len(s.events); i++ {
		event := s.events[i]
		if event.FaultName != fault.Name() {
			continue
		}
		if elapsed >= event.Offset {
			if _, alreadyInjected := s.injected[fault.Name()]; !alreadyInjected {
				s.injected[fault.Name()] = time.Now()
				s.eventIndex = i + 1
				return true
			}
		}
	}

	return false
}

// ShouldRevert implements Scheduler.
func (s *ScenarioScheduler) ShouldRevert(fault Fault, _ time.Duration) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	injectedAt, ok := s.injected[fault.Name()]
	if !ok {
		return false
	}

	// Find the event for this fault
	for _, event := range s.events {
		if event.FaultName == fault.Name() {
			if time.Since(injectedAt) >= event.Duration {
				delete(s.injected, fault.Name())
				return true
			}
			return false
		}
	}

	return false
}

// NextInjectionTime implements Scheduler.
func (s *ScenarioScheduler) NextInjectionTime() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.startTime.IsZero() || s.eventIndex >= len(s.events) {
		return time.Time{}
	}

	return s.startTime.Add(s.events[s.eventIndex].Offset)
}

// Reset implements Scheduler.
func (s *ScenarioScheduler) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.startTime = time.Time{}
	s.eventIndex = 0
	s.injected = make(map[string]time.Time)
}

// -----------------------------------------------------------------------------
// Burst Scheduler
// -----------------------------------------------------------------------------

// BurstScheduler injects multiple faults in quick succession.
//
// Description:
//
//	BurstScheduler is useful for testing how systems handle
//	multiple concurrent failures.
//
// Thread Safety: Safe for concurrent use.
type BurstScheduler struct {
	mu              sync.Mutex
	burstSize       int
	burstInterval   time.Duration
	faultDuration   time.Duration
	cooldown        time.Duration
	lastBurst       time.Time
	currentBurst    int
	injectedInBurst int
}

// NewBurstScheduler creates a burst scheduler.
//
// Inputs:
//   - burstSize: Number of faults to inject per burst.
//   - burstInterval: Time between faults within a burst.
//   - faultDuration: How long each fault stays active.
//   - cooldown: Time between bursts.
//
// Outputs:
//   - *BurstScheduler: The new scheduler. Never nil.
func NewBurstScheduler(burstSize int, burstInterval, faultDuration, cooldown time.Duration) *BurstScheduler {
	return &BurstScheduler{
		burstSize:     burstSize,
		burstInterval: burstInterval,
		faultDuration: faultDuration,
		cooldown:      cooldown,
	}
}

// ShouldInject implements Scheduler.
func (s *BurstScheduler) ShouldInject(_ Fault) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()

	// Check if we need to start a new burst
	if s.lastBurst.IsZero() || now.Sub(s.lastBurst) >= s.cooldown {
		s.lastBurst = now
		s.injectedInBurst = 0
	}

	// Check if we can inject in current burst
	if s.injectedInBurst < s.burstSize {
		expectedTime := s.lastBurst.Add(time.Duration(s.injectedInBurst) * s.burstInterval)
		if now.After(expectedTime) || now.Equal(expectedTime) {
			s.injectedInBurst++
			return true
		}
	}

	return false
}

// ShouldRevert implements Scheduler.
func (s *BurstScheduler) ShouldRevert(_ Fault, activeTime time.Duration) bool {
	return activeTime >= s.faultDuration
}

// NextInjectionTime implements Scheduler.
func (s *BurstScheduler) NextInjectionTime() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.lastBurst.IsZero() {
		return time.Now()
	}

	if s.injectedInBurst < s.burstSize {
		return s.lastBurst.Add(time.Duration(s.injectedInBurst) * s.burstInterval)
	}

	return s.lastBurst.Add(s.cooldown)
}

// Reset implements Scheduler.
func (s *BurstScheduler) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastBurst = time.Time{}
	s.currentBurst = 0
	s.injectedInBurst = 0
}

// -----------------------------------------------------------------------------
// No-Op Scheduler
// -----------------------------------------------------------------------------

// NoOpScheduler never injects faults. Useful for testing.
type NoOpScheduler struct{}

// NewNoOpScheduler creates a no-op scheduler.
func NewNoOpScheduler() *NoOpScheduler {
	return &NoOpScheduler{}
}

// ShouldInject implements Scheduler.
func (s *NoOpScheduler) ShouldInject(_ Fault) bool { return false }

// ShouldRevert implements Scheduler.
func (s *NoOpScheduler) ShouldRevert(_ Fault, _ time.Duration) bool { return true }

// NextInjectionTime implements Scheduler.
func (s *NoOpScheduler) NextInjectionTime() time.Time { return time.Time{} }

// Reset implements Scheduler.
func (s *NoOpScheduler) Reset() {}

// -----------------------------------------------------------------------------
// Manual Scheduler
// -----------------------------------------------------------------------------

// ManualScheduler allows programmatic control over injection.
//
// Description:
//
//	ManualScheduler is controlled via method calls rather than
//	automatic scheduling. Useful for integration testing.
//
// Thread Safety: Safe for concurrent use.
type ManualScheduler struct {
	mu           sync.Mutex
	shouldInject map[string]bool
	maxDuration  time.Duration
}

// NewManualScheduler creates a manual scheduler.
func NewManualScheduler(maxDuration time.Duration) *ManualScheduler {
	return &ManualScheduler{
		shouldInject: make(map[string]bool),
		maxDuration:  maxDuration,
	}
}

// Enable enables injection for a fault.
func (s *ManualScheduler) Enable(faultName string) {
	s.mu.Lock()
	s.shouldInject[faultName] = true
	s.mu.Unlock()
}

// Disable disables injection for a fault.
func (s *ManualScheduler) Disable(faultName string) {
	s.mu.Lock()
	delete(s.shouldInject, faultName)
	s.mu.Unlock()
}

// ShouldInject implements Scheduler.
func (s *ManualScheduler) ShouldInject(fault Fault) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.shouldInject[fault.Name()]
}

// ShouldRevert implements Scheduler.
func (s *ManualScheduler) ShouldRevert(fault Fault, activeTime time.Duration) bool {
	s.mu.Lock()
	enabled := s.shouldInject[fault.Name()]
	s.mu.Unlock()

	if !enabled {
		return true
	}
	if s.maxDuration > 0 && activeTime >= s.maxDuration {
		return true
	}
	return false
}

// NextInjectionTime implements Scheduler.
func (s *ManualScheduler) NextInjectionTime() time.Time {
	return time.Time{}
}

// Reset implements Scheduler.
func (s *ManualScheduler) Reset() {
	s.mu.Lock()
	s.shouldInject = make(map[string]bool)
	s.mu.Unlock()
}

// -----------------------------------------------------------------------------
// Context-Aware Scheduler Wrapper
// -----------------------------------------------------------------------------

// ContextAwareScheduler wraps a scheduler to respect context cancellation.
type ContextAwareScheduler struct {
	inner Scheduler
}

// WrapScheduler wraps a scheduler to be context-aware.
func WrapScheduler(s Scheduler) *ContextAwareScheduler {
	return &ContextAwareScheduler{inner: s}
}

// ShouldInject implements Scheduler.
func (s *ContextAwareScheduler) ShouldInjectWithContext(ctx context.Context, fault Fault) bool {
	select {
	case <-ctx.Done():
		return false
	default:
		return s.inner.ShouldInject(fault)
	}
}

// ShouldInject implements Scheduler.
func (s *ContextAwareScheduler) ShouldInject(fault Fault) bool {
	return s.inner.ShouldInject(fault)
}

// ShouldRevert implements Scheduler.
func (s *ContextAwareScheduler) ShouldRevert(fault Fault, activeTime time.Duration) bool {
	return s.inner.ShouldRevert(fault, activeTime)
}

// NextInjectionTime implements Scheduler.
func (s *ContextAwareScheduler) NextInjectionTime() time.Time {
	return s.inner.NextInjectionTime()
}

// Reset implements Scheduler.
func (s *ContextAwareScheduler) Reset() {
	s.inner.Reset()
}
