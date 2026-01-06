package main

import "time"

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
//	timeout := EnforceMinTimeout(config.Timeout, MinHTTPTimeout)
//	client := &http.Client{Timeout: timeout}
//
// # Limitations
//
//   - Does not enforce maximum timeouts
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
//	timeout := EnforceDefaultTimeout(opts.Timeout, DefaultHTTPTimeout)
//
// # Limitations
//
//   - Does not enforce minimum - caller should also check if needed
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

// TimeoutConfig holds timeout settings with validation.
//
// # Description
//
// Provides a validated set of timeout configurations for various
// operations. Use NewTimeoutConfig to create with proper defaults.
//
// # Example
//
//	cfg := NewTimeoutConfig()
//	cfg.HTTP = 60 * time.Second  // Custom HTTP timeout
//	validCfg := cfg.Validated()  // Ensures minimums are met
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

// NewTimeoutConfig creates a TimeoutConfig with sensible defaults.
//
// # Description
//
// Returns a TimeoutConfig initialized with the default timeout values.
// All values are guaranteed to be at least the minimum.
//
// # Outputs
//
//   - TimeoutConfig: Configuration with default timeouts
//
// # Example
//
//	cfg := NewTimeoutConfig()
//	// cfg.HTTP == DefaultHTTPTimeout
func NewTimeoutConfig() TimeoutConfig {
	return TimeoutConfig{
		HTTP:    DefaultHTTPTimeout,
		TCP:     DefaultTCPTimeout,
		Process: DefaultProcessTimeout,
		Compose: DefaultComposeTimeout,
	}
}

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
// # Example
//
//	cfg := TimeoutConfig{HTTP: 0}  // Invalid
//	valid := cfg.Validated()
//	// valid.HTTP == MinHTTPTimeout
func (c TimeoutConfig) Validated() TimeoutConfig {
	return TimeoutConfig{
		HTTP:    EnforceMinTimeout(c.HTTP, MinHTTPTimeout),
		TCP:     EnforceMinTimeout(c.TCP, MinTCPTimeout),
		Process: EnforceMinTimeout(c.Process, MinProcessTimeout),
		Compose: EnforceMinTimeout(c.Compose, MinProcessTimeout),
	}
}
