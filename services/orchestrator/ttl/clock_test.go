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
	"testing"
	"time"
)

// =============================================================================
// SEC-006: Clock Sanity Checking Tests
// =============================================================================

// TestClockChecker_CheckClockSanity_ValidTime tests that a valid system clock
// passes the sanity check.
func TestClockChecker_CheckClockSanity_ValidTime(t *testing.T) {
	checker := NewClockChecker()

	err := checker.CheckClockSanity()
	if err != nil {
		t.Errorf("Valid system clock should pass sanity check, got: %v", err)
	}
}

// TestClockChecker_CheckClockSanity_PastTime tests that a clock set before
// the minimum valid time is rejected.
func TestClockChecker_CheckClockSanity_PastTime(t *testing.T) {
	config := ClockConfig{
		MinValidTime:    time.Now().Add(1 * time.Hour), // Min is in the future = current time is "in the past"
		MaxValidTime:    time.Now().Add(10 * time.Hour),
		MaxBackwardJump: 1 * time.Hour,
		MaxForwardJump:  2 * time.Hour,
	}
	checker := NewClockCheckerWithConfig(config)

	err := checker.CheckClockSanity()
	if err == nil {
		t.Error("Clock before minimum valid time should fail sanity check")
	}
}

// TestClockChecker_CheckClockSanity_FutureTime tests that a clock set after
// the maximum valid time is rejected.
func TestClockChecker_CheckClockSanity_FutureTime(t *testing.T) {
	config := ClockConfig{
		MinValidTime:    time.Now().Add(-10 * time.Hour),
		MaxValidTime:    time.Now().Add(-1 * time.Hour), // Max is in the past = current time is "in the future"
		MaxBackwardJump: 1 * time.Hour,
		MaxForwardJump:  2 * time.Hour,
	}
	checker := NewClockCheckerWithConfig(config)

	err := checker.CheckClockSanity()
	if err == nil {
		t.Error("Clock after maximum valid time should fail sanity check")
	}
}

// TestClockChecker_CheckClockSanity_DetectsBackwardJump tests that a backward
// time jump beyond the threshold is detected.
func TestClockChecker_CheckClockSanity_DetectsBackwardJump(t *testing.T) {
	config := ClockConfig{
		MinValidTime:    time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
		MaxValidTime:    time.Date(2040, 12, 31, 23, 59, 59, 0, time.UTC),
		MaxBackwardJump: 1 * time.Hour,
		MaxForwardJump:  2 * time.Hour,
	}

	checker := &clockChecker{
		config:            config,
		lastKnownGoodTime: time.Now().Add(2 * time.Hour), // Last check was "2 hours from now"
		checkCount:        1,                             // Not first check
	}

	err := checker.CheckClockSanity()
	if err == nil {
		t.Error("Backward time jump of 2 hours (threshold: 1 hour) should fail")
	}
}

// TestClockChecker_CheckClockSanity_DetectsForwardJump tests that a forward
// time jump beyond the threshold is detected.
func TestClockChecker_CheckClockSanity_DetectsForwardJump(t *testing.T) {
	config := ClockConfig{
		MinValidTime:    time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
		MaxValidTime:    time.Date(2040, 12, 31, 23, 59, 59, 0, time.UTC),
		MaxBackwardJump: 1 * time.Hour,
		MaxForwardJump:  2 * time.Hour,
	}

	checker := &clockChecker{
		config:            config,
		lastKnownGoodTime: time.Now().Add(-3 * time.Hour), // Last check was 3 hours ago
		checkCount:        1,                              // Not first check
	}

	err := checker.CheckClockSanity()
	if err == nil {
		t.Error("Forward time jump of 3 hours (threshold: 2 hours) should fail")
	}
}

// TestClockChecker_CheckClockSanity_AllowsSmallJumps tests that time changes
// within the acceptable threshold are allowed.
func TestClockChecker_CheckClockSanity_AllowsSmallJumps(t *testing.T) {
	config := ClockConfig{
		MinValidTime:    time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
		MaxValidTime:    time.Date(2040, 12, 31, 23, 59, 59, 0, time.UTC),
		MaxBackwardJump: 1 * time.Hour,
		MaxForwardJump:  2 * time.Hour,
	}

	checker := &clockChecker{
		config:            config,
		lastKnownGoodTime: time.Now().Add(-30 * time.Minute), // 30 min ago
		checkCount:        1,
	}

	err := checker.CheckClockSanity()
	if err != nil {
		t.Errorf("30 minute forward jump should be allowed, got: %v", err)
	}
}

// TestClockChecker_CheckClockSanity_FirstCheckSkipsJump tests that the first
// clock check after startup skips jump detection.
func TestClockChecker_CheckClockSanity_FirstCheckSkipsJump(t *testing.T) {
	config := ClockConfig{
		MinValidTime:    time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
		MaxValidTime:    time.Date(2040, 12, 31, 23, 59, 59, 0, time.UTC),
		MaxBackwardJump: 1 * time.Hour,
		MaxForwardJump:  2 * time.Hour,
	}

	checker := &clockChecker{
		config:            config,
		lastKnownGoodTime: time.Now().Add(-100 * time.Hour), // Very old last check
		checkCount:        0,                                // First check
	}

	err := checker.CheckClockSanity()
	if err != nil {
		t.Errorf("First check should skip jump detection, got: %v", err)
	}
}

// TestClockChecker_CurrentTimeMs_ValidClock tests that CurrentTimeMs returns
// a valid timestamp when the clock is sane.
func TestClockChecker_CurrentTimeMs_ValidClock(t *testing.T) {
	checker := NewClockChecker()

	timeMs, err := checker.CurrentTimeMs()
	if err != nil {
		t.Fatalf("CurrentTimeMs failed with valid clock: %v", err)
	}

	// Should be within 1 second of actual time
	actualMs := time.Now().UnixMilli()
	diff := actualMs - timeMs
	if diff < 0 {
		diff = -diff
	}
	if diff > 1000 {
		t.Errorf("CurrentTimeMs returned %d, expected within 1s of %d", timeMs, actualMs)
	}
}

// TestClockChecker_CurrentTimeMs_InvalidClock tests that CurrentTimeMs returns
// an error when the clock sanity check fails.
func TestClockChecker_CurrentTimeMs_InvalidClock(t *testing.T) {
	config := ClockConfig{
		MinValidTime:    time.Now().Add(1 * time.Hour), // Min in future = clock "in past"
		MaxValidTime:    time.Now().Add(10 * time.Hour),
		MaxBackwardJump: 1 * time.Hour,
		MaxForwardJump:  2 * time.Hour,
	}
	checker := NewClockCheckerWithConfig(config)

	_, err := checker.CurrentTimeMs()
	if err == nil {
		t.Error("CurrentTimeMs should return error when clock is invalid")
	}
}

// TestClockChecker_ResetJumpDetection tests that ResetJumpDetection clears
// the jump detection state.
func TestClockChecker_ResetJumpDetection(t *testing.T) {
	config := ClockConfig{
		MinValidTime:    time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
		MaxValidTime:    time.Date(2040, 12, 31, 23, 59, 59, 0, time.UTC),
		MaxBackwardJump: 1 * time.Hour,
		MaxForwardJump:  2 * time.Hour,
	}

	checker := &clockChecker{
		config:            config,
		lastKnownGoodTime: time.Now().Add(-5 * time.Hour), // Would trigger forward jump
		checkCount:        5,
	}

	// This would fail without reset
	checker.ResetJumpDetection()

	// After reset, check should pass
	err := checker.CheckClockSanity()
	if err != nil {
		t.Errorf("Clock check should pass after reset, got: %v", err)
	}
}

// TestClockChecker_ConcurrentAccess tests that the clock checker is safe
// for concurrent use.
func TestClockChecker_ConcurrentAccess(t *testing.T) {
	checker := NewClockChecker()

	done := make(chan bool, 10)

	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				_ = checker.CheckClockSanity()
				_, _ = checker.CurrentTimeMs()
			}
			done <- true
		}()
	}

	for i := 0; i < 10; i++ {
		<-done
	}
}

// TestClockChecker_MultipleConsecutiveChecks tests that multiple consecutive
// checks pass when time progresses normally.
func TestClockChecker_MultipleConsecutiveChecks(t *testing.T) {
	checker := NewClockChecker()

	for i := 0; i < 10; i++ {
		err := checker.CheckClockSanity()
		if err != nil {
			t.Fatalf("Check %d failed: %v", i, err)
		}
	}
}

// TestClockChecker_DefaultConfig tests that DefaultClockConfig returns
// sensible values.
func TestClockChecker_DefaultConfig(t *testing.T) {
	config := DefaultClockConfig()

	if config.MinValidTime.Year() != 2025 {
		t.Errorf("Expected min year 2025, got %d", config.MinValidTime.Year())
	}

	if config.MaxValidTime.Year() != 2035 {
		t.Errorf("Expected max year 2035, got %d", config.MaxValidTime.Year())
	}

	if config.MaxBackwardJump != 1*time.Hour {
		t.Errorf("Expected max backward jump 1h, got %v", config.MaxBackwardJump)
	}

	if config.MaxForwardJump != 2*time.Hour {
		t.Errorf("Expected max forward jump 2h, got %v", config.MaxForwardJump)
	}
}

// TestNoopClockChecker_AlwaysPasses tests that the no-op checker always passes.
func TestNoopClockChecker_AlwaysPasses(t *testing.T) {
	checker := NewNoopClockChecker()

	err := checker.CheckClockSanity()
	if err != nil {
		t.Errorf("Noop checker should always pass, got: %v", err)
	}

	timeMs, err := checker.CurrentTimeMs()
	if err != nil {
		t.Errorf("Noop checker CurrentTimeMs should always succeed, got: %v", err)
	}

	if timeMs == 0 {
		t.Error("Noop checker should return non-zero time")
	}

	// ResetJumpDetection should not panic
	checker.ResetJumpDetection()
}
