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
	"testing"
	"time"
)

func TestTreeDegradation_String(t *testing.T) {
	tests := []struct {
		level TreeDegradation
		want  string
	}{
		{TreeDegradationNormal, "normal"},
		{TreeDegradationReduced, "reduced"},
		{TreeDegradationMinimal, "minimal"},
		{TreeDegradationLinear, "linear"},
		{TreeDegradation(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.level.String(); got != tt.want {
				t.Errorf("String() = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestDefaultDegradationConfig(t *testing.T) {
	config := DefaultDegradationConfig()

	if config.ConsecutiveFailuresForReduced != 2 {
		t.Errorf("ConsecutiveFailuresForReduced = %d, want 2", config.ConsecutiveFailuresForReduced)
	}
	if config.ConsecutiveFailuresForMinimal != 4 {
		t.Errorf("ConsecutiveFailuresForMinimal = %d, want 4", config.ConsecutiveFailuresForMinimal)
	}
	if config.ConsecutiveFailuresForLinear != 6 {
		t.Errorf("ConsecutiveFailuresForLinear = %d, want 6", config.ConsecutiveFailuresForLinear)
	}
	if config.CircuitOpenDegradation != TreeDegradationLinear {
		t.Errorf("CircuitOpenDegradation = %v, want Linear", config.CircuitOpenDegradation)
	}
	if config.SuccessesForRecovery != 3 {
		t.Errorf("SuccessesForRecovery = %d, want 3", config.SuccessesForRecovery)
	}
}

func TestNewDegradationManager(t *testing.T) {
	config := DefaultDegradationConfig()
	dm := NewDegradationManager(config, nil)

	if dm == nil {
		t.Fatal("NewDegradationManager returned nil")
	}
	if dm.CurrentLevel() != TreeDegradationNormal {
		t.Errorf("Initial level = %v, want normal", dm.CurrentLevel())
	}
}

func TestDegradationManager_DegradesToReduced(t *testing.T) {
	config := DegradationConfig{
		ConsecutiveFailuresForReduced: 2,
		ConsecutiveFailuresForMinimal: 4,
		ConsecutiveFailuresForLinear:  6,
	}
	dm := NewDegradationManager(config, nil)

	// Record failures
	dm.RecordFailure()
	dm.RecordFailure()

	if dm.CurrentLevel() != TreeDegradationReduced {
		t.Errorf("Level = %v, want reduced", dm.CurrentLevel())
	}
}

func TestDegradationManager_DegradesToMinimal(t *testing.T) {
	config := DegradationConfig{
		ConsecutiveFailuresForReduced: 2,
		ConsecutiveFailuresForMinimal: 4,
		ConsecutiveFailuresForLinear:  6,
	}
	dm := NewDegradationManager(config, nil)

	// Record failures
	for i := 0; i < 4; i++ {
		dm.RecordFailure()
	}

	if dm.CurrentLevel() != TreeDegradationMinimal {
		t.Errorf("Level = %v, want minimal", dm.CurrentLevel())
	}
}

func TestDegradationManager_DegradesToLinear(t *testing.T) {
	config := DegradationConfig{
		ConsecutiveFailuresForReduced: 2,
		ConsecutiveFailuresForMinimal: 4,
		ConsecutiveFailuresForLinear:  6,
	}
	dm := NewDegradationManager(config, nil)

	// Record failures
	for i := 0; i < 6; i++ {
		dm.RecordFailure()
	}

	if dm.CurrentLevel() != TreeDegradationLinear {
		t.Errorf("Level = %v, want linear", dm.CurrentLevel())
	}
}

func TestDegradationManager_SuccessResetsFailures(t *testing.T) {
	config := DegradationConfig{
		ConsecutiveFailuresForReduced: 3,
		ConsecutiveFailuresForMinimal: 100, // High to avoid triggering
		ConsecutiveFailuresForLinear:  100, // High to avoid triggering
	}
	dm := NewDegradationManager(config, nil)

	// Record some failures
	dm.RecordFailure()
	dm.RecordFailure()

	// Success resets
	dm.RecordSuccess()

	// Two more failures shouldn't trigger degradation
	dm.RecordFailure()
	dm.RecordFailure()

	if dm.CurrentLevel() != TreeDegradationNormal {
		t.Errorf("Level = %v, want normal (failures were reset)", dm.CurrentLevel())
	}
}

func TestDegradationManager_Recovers(t *testing.T) {
	config := DegradationConfig{
		ConsecutiveFailuresForReduced: 2,
		ConsecutiveFailuresForMinimal: 100, // High to avoid triggering
		ConsecutiveFailuresForLinear:  100, // High to avoid triggering
		SuccessesForRecovery:          2,
	}
	dm := NewDegradationManager(config, nil)

	// Degrade to reduced
	dm.RecordFailure()
	dm.RecordFailure()

	if dm.CurrentLevel() != TreeDegradationReduced {
		t.Fatal("Should be reduced")
	}

	// Recover with successes
	dm.RecordSuccess()
	dm.RecordSuccess()

	if dm.CurrentLevel() != TreeDegradationNormal {
		t.Errorf("Level = %v, want normal (should recover)", dm.CurrentLevel())
	}
}

func TestDegradationManager_RecoversOneLevel(t *testing.T) {
	config := DegradationConfig{
		ConsecutiveFailuresForReduced: 1,
		ConsecutiveFailuresForMinimal: 2,
		ConsecutiveFailuresForLinear:  100, // High to avoid triggering
		SuccessesForRecovery:          1,
	}
	dm := NewDegradationManager(config, nil)

	// Degrade to minimal
	dm.RecordFailure()
	dm.RecordFailure()

	if dm.CurrentLevel() != TreeDegradationMinimal {
		t.Fatal("Should be minimal")
	}

	// First recovery: minimal -> reduced
	dm.RecordSuccess()

	if dm.CurrentLevel() != TreeDegradationReduced {
		t.Errorf("Level = %v, want reduced", dm.CurrentLevel())
	}

	// Second recovery: reduced -> normal
	dm.RecordSuccess()

	if dm.CurrentLevel() != TreeDegradationNormal {
		t.Errorf("Level = %v, want normal", dm.CurrentLevel())
	}
}

func TestDegradationManager_CircuitBreakerTriggersDegradation(t *testing.T) {
	cbConfig := CircuitBreakerConfig{
		FailureThreshold: 1,
		OpenDuration:     time.Hour,
	}
	cb := NewCircuitBreaker(cbConfig)

	config := DefaultDegradationConfig()
	dm := NewDegradationManager(config, cb)

	// Open the circuit breaker
	cb.Allow()
	cb.RecordFailure()

	// Record a failure on degradation manager (triggers check)
	dm.RecordFailure()

	if dm.CurrentLevel() != TreeDegradationLinear {
		t.Errorf("Level = %v, want linear (circuit open)", dm.CurrentLevel())
	}
}

func TestDegradationManager_NoRecoveryWhileCircuitOpen(t *testing.T) {
	cbConfig := CircuitBreakerConfig{
		FailureThreshold: 1,
		OpenDuration:     time.Hour,
	}
	cb := NewCircuitBreaker(cbConfig)

	config := DegradationConfig{
		ConsecutiveFailuresForReduced: 1,
		SuccessesForRecovery:          1,
		CircuitOpenDegradation:        TreeDegradationReduced,
	}
	dm := NewDegradationManager(config, cb)

	// Open the circuit breaker and trigger degradation
	cb.Allow()
	cb.RecordFailure()
	dm.RecordFailure()

	if dm.CurrentLevel() != TreeDegradationReduced {
		t.Fatal("Should be reduced")
	}

	// Try to recover while circuit is still open
	dm.RecordSuccess()

	// Should not recover
	if dm.CurrentLevel() != TreeDegradationReduced {
		t.Errorf("Level = %v, want reduced (circuit still open)", dm.CurrentLevel())
	}
}

func TestDegradationManager_OnDegradationCallback(t *testing.T) {
	config := DegradationConfig{
		ConsecutiveFailuresForReduced: 1,
		ConsecutiveFailuresForMinimal: 100, // High to avoid triggering
		ConsecutiveFailuresForLinear:  100, // High to avoid triggering
		NotifyOnDegradation:           true,
	}
	dm := NewDegradationManager(config, nil)

	var called bool
	var fromLevel, toLevel TreeDegradation
	var reasonStr string

	dm.OnDegradation(func(from, to TreeDegradation, reason string) {
		called = true
		fromLevel = from
		toLevel = to
		reasonStr = reason
	})

	dm.RecordFailure()

	if !called {
		t.Error("OnDegradation callback not called")
	}
	if fromLevel != TreeDegradationNormal {
		t.Errorf("from = %v, want normal", fromLevel)
	}
	if toLevel != TreeDegradationReduced {
		t.Errorf("to = %v, want reduced", toLevel)
	}
	if reasonStr != "consecutive failures" {
		t.Errorf("reason = %s, want 'consecutive failures'", reasonStr)
	}
}

func TestDegradationManager_ShouldUseLinearFallback(t *testing.T) {
	config := DegradationConfig{
		ConsecutiveFailuresForLinear: 1,
	}
	dm := NewDegradationManager(config, nil)

	if dm.ShouldUseLinearFallback() {
		t.Error("Should not use linear fallback initially")
	}

	dm.RecordFailure()

	if !dm.ShouldUseLinearFallback() {
		t.Error("Should use linear fallback after degradation")
	}
}

func TestDegradationManager_GetBudgetForLevel(t *testing.T) {
	config := DegradationConfig{
		ConsecutiveFailuresForReduced: 1,
		ConsecutiveFailuresForMinimal: 2,
		ConsecutiveFailuresForLinear:  3,
	}
	dm := NewDegradationManager(config, nil)

	// Normal budget
	normalBudget := dm.GetBudgetForLevel()
	if normalBudget.MaxNodes != 20 {
		t.Errorf("Normal MaxNodes = %d, want 20", normalBudget.MaxNodes)
	}

	// Reduced budget
	dm.RecordFailure()
	reducedBudget := dm.GetBudgetForLevel()
	if reducedBudget.MaxNodes != 5 {
		t.Errorf("Reduced MaxNodes = %d, want 5", reducedBudget.MaxNodes)
	}

	// Minimal budget
	dm.RecordFailure()
	minimalBudget := dm.GetBudgetForLevel()
	if minimalBudget.MaxNodes != 2 {
		t.Errorf("Minimal MaxNodes = %d, want 2", minimalBudget.MaxNodes)
	}

	// Linear budget
	dm.RecordFailure()
	linearBudget := dm.GetBudgetForLevel()
	if linearBudget.MaxNodes != 1 {
		t.Errorf("Linear MaxNodes = %d, want 1", linearBudget.MaxNodes)
	}
}

func TestDegradationManager_Status(t *testing.T) {
	config := DegradationConfig{
		ConsecutiveFailuresForReduced: 100, // High to avoid triggering
		ConsecutiveFailuresForMinimal: 100, // High to avoid triggering
		ConsecutiveFailuresForLinear:  100, // High to avoid triggering
		SuccessesForRecovery:          100, // High to avoid triggering
	}
	dm := NewDegradationManager(config, nil)

	dm.RecordFailure()
	dm.RecordSuccess()

	status := dm.Status()

	if status.Level != "normal" {
		t.Errorf("Level = %s, want normal", status.Level)
	}
	if status.ConsecutiveSuccesses != 1 {
		t.Errorf("ConsecutiveSuccesses = %d, want 1", status.ConsecutiveSuccesses)
	}
	if status.ConsecutiveFailures != 0 {
		t.Errorf("ConsecutiveFailures = %d, want 0", status.ConsecutiveFailures)
	}
	if status.CircuitState != "closed" {
		t.Errorf("CircuitState = %s, want closed", status.CircuitState)
	}
}

func TestDegradationManager_StatusWithCircuitBreaker(t *testing.T) {
	cbConfig := CircuitBreakerConfig{
		FailureThreshold: 1,
		OpenDuration:     time.Hour,
	}
	cb := NewCircuitBreaker(cbConfig)

	dm := NewDegradationManager(DefaultDegradationConfig(), cb)

	// Open the circuit
	cb.Allow()
	cb.RecordFailure()

	status := dm.Status()
	if status.CircuitState != "open" {
		t.Errorf("CircuitState = %s, want open", status.CircuitState)
	}
}

func TestDegradationManager_Reset(t *testing.T) {
	config := DegradationConfig{
		ConsecutiveFailuresForLinear: 1,
	}
	dm := NewDegradationManager(config, nil)

	dm.RecordFailure()

	if dm.CurrentLevel() != TreeDegradationLinear {
		t.Fatal("Should be linear")
	}

	dm.Reset()

	if dm.CurrentLevel() != TreeDegradationNormal {
		t.Errorf("Level = %v, want normal after reset", dm.CurrentLevel())
	}

	status := dm.Status()
	if status.ConsecutiveFailures != 0 {
		t.Errorf("ConsecutiveFailures = %d, want 0", status.ConsecutiveFailures)
	}
}

func TestDegradationManager_Config(t *testing.T) {
	config := DegradationConfig{
		ConsecutiveFailuresForReduced: 99,
	}
	dm := NewDegradationManager(config, nil)

	got := dm.Config()
	if got.ConsecutiveFailuresForReduced != 99 {
		t.Errorf("Config().ConsecutiveFailuresForReduced = %d, want 99", got.ConsecutiveFailuresForReduced)
	}
}

func TestDegradationManager_Concurrency(t *testing.T) {
	config := DegradationConfig{
		ConsecutiveFailuresForReduced: 10,
		ConsecutiveFailuresForMinimal: 20,
		ConsecutiveFailuresForLinear:  30,
		SuccessesForRecovery:          5,
	}
	dm := NewDegradationManager(config, nil)

	const numGoroutines = 100
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			if idx%2 == 0 {
				dm.RecordSuccess()
			} else {
				dm.RecordFailure()
			}
			dm.CurrentLevel()
			dm.Status()
			dm.GetBudgetForLevel()
		}(i)
	}

	wg.Wait()
	// No race condition = success
}

func TestDegradationManager_NoDegradeAtSameLevel(t *testing.T) {
	callCount := 0
	config := DegradationConfig{
		ConsecutiveFailuresForReduced: 1,
		ConsecutiveFailuresForMinimal: 2,
		ConsecutiveFailuresForLinear:  100, // High to avoid triggering
		NotifyOnDegradation:           true,
	}
	dm := NewDegradationManager(config, nil)
	dm.OnDegradation(func(from, to TreeDegradation, reason string) {
		callCount++
	})

	// First failure degrades to reduced
	dm.RecordFailure()
	if callCount != 1 {
		t.Errorf("callCount = %d, want 1", callCount)
	}

	// Second failure degrades to minimal
	dm.RecordFailure()
	if callCount != 2 {
		t.Errorf("callCount = %d, want 2", callCount)
	}

	// Third failure should NOT trigger callback (already at minimal, and linear threshold is 100)
	dm.RecordFailure()
	if callCount != 2 {
		t.Errorf("callCount = %d, want 2 (no new degradation)", callCount)
	}
}
