// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package util

import "time"

// =============================================================================
// Constants
// =============================================================================

// Timeout constants define minimum and default values for various operations.
//
// These constants prevent accidental infinite hangs by ensuring all
// operations have a reasonable timeout even if misconfigured.
const (
	// MinHTTPTimeout is the absolute minimum for any HTTP operation.
	// Prevents accidental infinite hangs from zero timeouts.
	MinHTTPTimeout = 1 * time.Second

	// MinTCPTimeout is the absolute minimum for TCP connection checks.
	MinTCPTimeout = 500 * time.Millisecond

	// MinProcessTimeout is the absolute minimum for process operations.
	MinProcessTimeout = 5 * time.Second

	// DefaultHTTPTimeout is the standard timeout for HTTP health checks.
	DefaultHTTPTimeout = 30 * time.Second

	// DefaultTCPTimeout is the standard timeout for TCP connectivity checks.
	DefaultTCPTimeout = 5 * time.Second

	// DefaultProcessTimeout is the standard timeout for process operations.
	DefaultProcessTimeout = 2 * time.Minute

	// DefaultComposeTimeout is the standard timeout for compose operations.
	DefaultComposeTimeout = 5 * time.Minute
)

// =============================================================================
// TimeoutValidator Interface
// =============================================================================

// TimeoutValidator defines the contract for timeout configuration validation.
//
// # Description
//
// TimeoutValidator provides a standard interface for validating timeout
// configurations. Implementations should ensure all timeout values meet
// their respective minimums to prevent infinite hangs.
//
// # Thread Safety
//
// Implementations should be safe for concurrent use from multiple goroutines.
//
// # Example
//
//	func configureClient(v util.TimeoutValidator) {
//	    validated := v.Validated()
//	    client.Timeout = validated.HTTP
//	}
type TimeoutValidator interface {
	// Validated returns a copy with all timeouts at least at their minimums.
	//
	// # Description
	//
	// Returns a new TimeoutConfig where any value below its minimum has been
	// raised to the minimum. The original config is not modified.
	//
	// # Outputs
	//
	//   - TimeoutConfig: A validated copy with enforced minimums
	//
	// # Assumptions
	//
	//   - The receiver is not nil
	Validated() TimeoutConfig
}

// =============================================================================
// TimeoutConfig Struct
// =============================================================================

// TimeoutConfig holds timeout settings with validation.
//
// # Description
//
// Provides a validated set of timeout configurations for various
// operations. Use NewTimeoutConfig to create with proper defaults.
//
// # Thread Safety
//
// TimeoutConfig is safe for concurrent reads. For concurrent modifications,
// external synchronization is required.
//
// # Example
//
//	cfg := util.NewTimeoutConfig()
//	cfg.HTTP = 60 * time.Second  // Custom HTTP timeout
//	validCfg := cfg.Validated()  // Ensures minimums are met
//
// # Limitations
//
//   - Does not enforce maximum timeouts
//   - No built-in validation on field assignment
//
// # Assumptions
//
//   - Consumers call Validated() before using values in production
type TimeoutConfig struct {
	// HTTP is the timeout for HTTP operations.
	HTTP time.Duration

	// TCP is the timeout for TCP connection checks.
	TCP time.Duration

	// Process is the timeout for process operations.
	Process time.Duration

	// Compose is the timeout for compose operations.
	Compose time.Duration
}

// =============================================================================
// TimeoutConfig Methods
// =============================================================================

// Validated returns a copy with all timeouts at least at their minimums.
//
// # Description
//
// Returns a new TimeoutConfig where any value below its minimum has been
// raised to the minimum. The original config is not modified.
//
// # Inputs
//
//   - c: The TimeoutConfig to validate (receiver)
//
// # Outputs
//
//   - TimeoutConfig: A validated copy with enforced minimums
//
// # Example
//
//	cfg := &util.TimeoutConfig{HTTP: 0}  // Invalid
//	valid := cfg.Validated()
//	// valid.HTTP == util.MinHTTPTimeout
//
// # Limitations
//
//   - Does not enforce maximum timeouts
//
// # Assumptions
//
//   - The receiver is not nil
//   - Minimum constants are positive durations
func (c *TimeoutConfig) Validated() TimeoutConfig {
	return TimeoutConfig{
		HTTP:    EnforceMinTimeout(c.HTTP, MinHTTPTimeout),
		TCP:     EnforceMinTimeout(c.TCP, MinTCPTimeout),
		Process: EnforceMinTimeout(c.Process, MinProcessTimeout),
		Compose: EnforceMinTimeout(c.Compose, MinProcessTimeout),
	}
}

// Compile-time interface satisfaction check
var _ TimeoutValidator = (*TimeoutConfig)(nil)

// =============================================================================
// Constructor Functions
// =============================================================================

// NewTimeoutConfig creates a TimeoutConfig with sensible defaults.
//
// # Description
//
// Returns a TimeoutConfig initialized with the default timeout values.
// All values are guaranteed to be at least the minimum.
//
// # Inputs
//
//   - None
//
// # Outputs
//
//   - TimeoutConfig: Configuration with default timeouts
//
// # Example
//
//	cfg := util.NewTimeoutConfig()
//	// cfg.HTTP == util.DefaultHTTPTimeout
//
// # Limitations
//
//   - Returns value type, not pointer
//
// # Assumptions
//
//   - Default constants are defined and positive
func NewTimeoutConfig() TimeoutConfig {
	return TimeoutConfig{
		HTTP:    DefaultHTTPTimeout,
		TCP:     DefaultTCPTimeout,
		Process: DefaultProcessTimeout,
		Compose: DefaultComposeTimeout,
	}
}

// =============================================================================
// Utility Functions
// =============================================================================

// EnforceMinTimeout returns at least the minimum timeout.
//
// # Description
//
// Ensures a timeout is never below the specified minimum. If the requested
// timeout is zero, negative, or below the minimum, returns the minimum
// instead. This prevents misconfiguration from causing infinite hangs.
//
// # Inputs
//
//   - requested: The timeout value requested by the caller
//   - minimum: The absolute minimum acceptable timeout
//
// # Outputs
//
//   - time.Duration: The requested timeout if valid, otherwise the minimum
//
// # Example
//
//	// User configured 0 timeout (infinite) - enforce 1s minimum
//	timeout := util.EnforceMinTimeout(config.Timeout, util.MinHTTPTimeout)
//	client := &http.Client{Timeout: timeout}
//
// # Limitations
//
//   - Does not enforce maximum timeouts
//   - Does not validate that minimum is positive
//
// # Assumptions
//
//   - minimum is a positive duration
func EnforceMinTimeout(requested, minimum time.Duration) time.Duration {
	if requested <= 0 || requested < minimum {
		return minimum
	}
	return requested
}

// EnforceDefaultTimeout returns the default if the requested is zero or negative.
//
// # Description
//
// Unlike EnforceMinTimeout, this only applies the default when the value
// is explicitly zero or negative. Useful when you want to allow any
// positive value but provide a sensible default.
//
// # Inputs
//
//   - requested: The timeout value requested by the caller
//   - defaultVal: The default timeout to use if requested is invalid
//
// # Outputs
//
//   - time.Duration: The requested timeout if positive, otherwise the default
//
// # Example
//
//	// Use default only if not specified
//	timeout := util.EnforceDefaultTimeout(opts.Timeout, util.DefaultHTTPTimeout)
//
// # Limitations
//
//   - Does not enforce minimum - caller should also check if needed
//   - Does not validate that defaultVal is positive
//
// # Assumptions
//
//   - defaultVal is a positive duration
func EnforceDefaultTimeout(requested, defaultVal time.Duration) time.Duration {
	if requested <= 0 {
		return defaultVal
	}
	return requested
}
