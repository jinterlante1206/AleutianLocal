// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

// Package ttl provides time-to-live (TTL) management for documents and sessions
// in the Aleutian RAG system. It implements automatic expiration and cleanup
// for GDPR/CCPA compliance.
package ttl

import (
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// =============================================================================
// SEC-006: Clock Sanity Checking
// =============================================================================

// ClockChecker provides sanity checking for system time.
//
// # Description
//
// Validates that the system clock is within acceptable bounds before
// performing time-sensitive operations like TTL expiration checks.
// Prevents premature deletion due to clock drift or manipulation.
//
// # Security Context
//
// If the system clock is manipulated:
//   - Set to the future: Documents will be deleted prematurely (data loss)
//   - Set to the past: Documents will never expire (compliance violation)
//
// This checker provides defense-in-depth against clock manipulation attacks.
//
// # Thread Safety
//
// All methods are safe for concurrent use.
type ClockChecker interface {
	// CheckClockSanity verifies the system clock is reasonable.
	//
	// # Description
	//
	// Validates that:
	//   1. Current time is after minValidTime
	//   2. Current time is before maxValidTime
	//   3. Time hasn't jumped more than allowed threshold from last check
	//
	// # Outputs
	//
	//   - error: Non-nil if clock appears invalid.
	//
	// # Limitations
	//
	//   - Cannot detect slow clock drift within acceptable bounds.
	//   - First call after restart may flag legitimate time corrections.
	CheckClockSanity() error

	// CurrentTimeMs returns current time in milliseconds if clock is sane.
	//
	// # Description
	//
	// Performs a clock sanity check and returns the current time only if
	// the check passes. Use this instead of time.Now().UnixMilli() in
	// TTL-sensitive code paths.
	//
	// # Outputs
	//
	//   - int64: Current Unix milliseconds.
	//   - error: Non-nil if clock sanity check fails.
	//
	// # Example
	//
	//   timeMs, err := checker.CurrentTimeMs()
	//   if err != nil {
	//       return fmt.Errorf("clock sanity check failed: %w", err)
	//   }
	CurrentTimeMs() (int64, error)

	// ResetJumpDetection resets the jump detection baseline.
	//
	// # Description
	//
	// Call this after a known legitimate time change (e.g., NTP sync,
	// system resume from sleep) to prevent false positive jump detection.
	ResetJumpDetection()
}

// ClockConfig contains configuration for the clock checker.
//
// # Description
//
// Allows customization of clock validation bounds and thresholds.
//
// # Fields
//
//   - MinValidTime: Earliest acceptable time (default: 2025-01-01)
//   - MaxValidTime: Latest acceptable time (default: 2035-12-31)
//   - MaxBackwardJump: Maximum allowed backward time jump (default: 1 hour)
//   - MaxForwardJump: Maximum allowed forward time jump (default: 2 hours)
type ClockConfig struct {
	MinValidTime    time.Time
	MaxValidTime    time.Time
	MaxBackwardJump time.Duration
	MaxForwardJump  time.Duration
}

// DefaultClockConfig returns sensible default configuration.
//
// # Description
//
// Returns a config suitable for production use:
//   - Min time: 2025-01-01 (reasonable past bound)
//   - Max time: 2035-12-31 (10 years future)
//   - Max backward jump: 1 hour
//   - Max forward jump: 2 hours
//
// # Outputs
//
//   - ClockConfig: Default configuration values.
func DefaultClockConfig() ClockConfig {
	return ClockConfig{
		MinValidTime:    time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		MaxValidTime:    time.Date(2035, 12, 31, 23, 59, 59, 0, time.UTC),
		MaxBackwardJump: 1 * time.Hour,
		MaxForwardJump:  2 * time.Hour,
	}
}

// clockChecker implements ClockChecker interface.
//
// # Description
//
// Validates system time against configurable bounds and tracks time
// progression to detect suspicious jumps that might indicate clock
// manipulation or system time correction.
//
// # Thread Safety
//
// All methods are thread-safe via mutex protection.
type clockChecker struct {
	config            ClockConfig
	lastKnownGoodTime time.Time
	mu                sync.RWMutex
	checkCount        int64
}

// NewClockChecker creates a clock checker with default configuration.
//
// # Description
//
// Creates a clock checker using DefaultClockConfig(). Suitable for most
// production deployments.
//
// # Outputs
//
//   - ClockChecker: Ready to validate system time.
//
// # Example
//
//	checker := NewClockChecker()
//	if err := checker.CheckClockSanity(); err != nil {
//	    log.Fatal("Clock appears invalid:", err)
//	}
func NewClockChecker() ClockChecker {
	return NewClockCheckerWithConfig(DefaultClockConfig())
}

// NewClockCheckerWithConfig creates a clock checker with custom configuration.
//
// # Description
//
// Creates a clock checker with specified bounds and thresholds.
// Use this when default values don't fit your deployment.
//
// # Inputs
//
//   - config: Custom clock validation configuration.
//
// # Outputs
//
//   - ClockChecker: Ready to validate system time.
//
// # Example
//
//	config := ClockConfig{
//	    MinValidTime:    time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
//	    MaxValidTime:    time.Date(2030, 12, 31, 23, 59, 59, 0, time.UTC),
//	    MaxBackwardJump: 30 * time.Minute,
//	    MaxForwardJump:  1 * time.Hour,
//	}
//	checker := NewClockCheckerWithConfig(config)
func NewClockCheckerWithConfig(config ClockConfig) ClockChecker {
	return &clockChecker{
		config:            config,
		lastKnownGoodTime: time.Now(),
		checkCount:        0,
	}
}

// CheckClockSanity verifies the system clock is reasonable.
//
// # Description
//
// Performs three validations:
//  1. Current time >= minValidTime (not in distant past)
//  2. Current time <= maxValidTime (not in distant future)
//  3. No suspicious jumps from last known good time
//
// On first call or after ResetJumpDetection(), jump detection is skipped.
//
// # Outputs
//
//   - error: Non-nil with descriptive message if clock appears invalid.
//
// # Limitations
//
//   - Cannot detect slow drift within acceptable bounds.
//   - Jump detection requires previous successful check.
func (c *clockChecker) CheckClockSanity() error {
	now := time.Now()

	// Check bounds
	if now.Before(c.config.MinValidTime) {
		return fmt.Errorf("clock sanity: time %v is before minimum valid time %v (possible clock set to past)",
			now.Format(time.RFC3339), c.config.MinValidTime.Format(time.RFC3339))
	}

	if now.After(c.config.MaxValidTime) {
		return fmt.Errorf("clock sanity: time %v is after maximum valid time %v (possible clock set to future)",
			now.Format(time.RFC3339), c.config.MaxValidTime.Format(time.RFC3339))
	}

	// Check for suspicious jumps (skip on first check)
	c.mu.RLock()
	lastGood := c.lastKnownGoodTime
	checkCount := c.checkCount
	c.mu.RUnlock()

	if checkCount > 0 {
		timeDiff := now.Sub(lastGood)

		// Check backward jump
		if timeDiff < -c.config.MaxBackwardJump {
			return fmt.Errorf("clock sanity: suspicious backward jump of %v detected (max allowed: %v)",
				-timeDiff, c.config.MaxBackwardJump)
		}

		// Check forward jump
		if timeDiff > c.config.MaxForwardJump {
			return fmt.Errorf("clock sanity: suspicious forward jump of %v detected (max allowed: %v)",
				timeDiff, c.config.MaxForwardJump)
		}
	}

	// Update last known good time
	c.mu.Lock()
	c.lastKnownGoodTime = now
	c.checkCount++
	c.mu.Unlock()

	return nil
}

// CurrentTimeMs returns current time if clock is sane.
//
// # Description
//
// Combines sanity check with time retrieval. Returns error if clock
// appears to be manipulated or drifted outside acceptable bounds.
//
// # Outputs
//
//   - int64: Current time in Unix milliseconds.
//   - error: Non-nil if clock sanity check fails.
func (c *clockChecker) CurrentTimeMs() (int64, error) {
	if err := c.CheckClockSanity(); err != nil {
		slog.Warn("clock sanity check failed",
			"error", err,
		)
		return 0, err
	}
	return time.Now().UnixMilli(), nil
}

// ResetJumpDetection resets the jump detection baseline.
//
// # Description
//
// Resets the last known good time to current time and clears the
// check counter. Call this when you expect a legitimate time change:
//   - After NTP synchronization
//   - After system resume from sleep/hibernate
//   - During startup after config change
func (c *clockChecker) ResetJumpDetection() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.lastKnownGoodTime = time.Now()
	c.checkCount = 0

	slog.Info("clock checker: jump detection reset",
		"new_baseline", c.lastKnownGoodTime.Format(time.RFC3339),
	)
}

// =============================================================================
// No-op Clock Checker (for testing)
// =============================================================================

// noopClockChecker always passes sanity checks.
//
// # Description
//
// Used in tests or when clock checking should be disabled.
// Always returns current time without validation.
type noopClockChecker struct{}

// NewNoopClockChecker creates a clock checker that always passes.
//
// # Description
//
// Returns a checker that performs no validation. Use only in tests
// or when you have external guarantees about clock correctness.
//
// # Outputs
//
//   - ClockChecker: No-op checker that always succeeds.
func NewNoopClockChecker() ClockChecker {
	return &noopClockChecker{}
}

// CheckClockSanity always returns nil.
func (n *noopClockChecker) CheckClockSanity() error {
	return nil
}

// CurrentTimeMs returns current time without validation.
func (n *noopClockChecker) CurrentTimeMs() (int64, error) {
	return time.Now().UnixMilli(), nil
}

// ResetJumpDetection is a no-op.
func (n *noopClockChecker) ResetJumpDetection() {}
