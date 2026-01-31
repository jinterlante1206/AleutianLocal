// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package tdg

import "time"

// =============================================================================
// CONFIGURATION
// =============================================================================

// Config holds configuration for a TDG session.
type Config struct {
	// MaxTestRetries is the maximum test generation attempts.
	// When exceeded, TDG returns ErrMaxTestRetries.
	// Default: 3
	MaxTestRetries int

	// MaxFixRetries is the maximum fix generation attempts.
	// When exceeded, TDG returns ErrMaxFixRetries.
	// Default: 5
	MaxFixRetries int

	// MaxRegressionFixes is the maximum regression fix attempts.
	// When exceeded, TDG returns ErrMaxRegressionFixes.
	// Default: 3
	MaxRegressionFixes int

	// TestTimeout is the timeout for a single test execution.
	// Default: 30s
	TestTimeout time.Duration

	// SuiteTimeout is the timeout for running the full test suite.
	// Default: 5m
	SuiteTimeout time.Duration

	// TotalTimeout is the timeout for the entire TDG session.
	// Default: 10m
	TotalTimeout time.Duration

	// MaxOutputBytes is the maximum test output to capture.
	// Output beyond this is truncated.
	// Default: 65536 (64KB)
	MaxOutputBytes int

	// EnableLintCheck enables lint checking on generated code (CB-25).
	// Default: true
	EnableLintCheck bool

	// WorkingDir overrides the working directory for test execution.
	// If empty, uses the project root from the request.
	WorkingDir string
}

// DefaultConfig returns a Config with sensible defaults.
//
// Outputs:
//
//	*Config - Configuration with default values
func DefaultConfig() *Config {
	return &Config{
		MaxTestRetries:     3,
		MaxFixRetries:      5,
		MaxRegressionFixes: 3,
		TestTimeout:        30 * time.Second,
		SuiteTimeout:       5 * time.Minute,
		TotalTimeout:       10 * time.Minute,
		MaxOutputBytes:     64 * 1024, // 64KB
		EnableLintCheck:    true,
	}
}

// Validate checks that the configuration is valid.
//
// Outputs:
//
//	error - Non-nil if configuration is invalid
func (c *Config) Validate() error {
	if c.MaxTestRetries < 1 {
		c.MaxTestRetries = 1
	}
	if c.MaxFixRetries < 1 {
		c.MaxFixRetries = 1
	}
	if c.MaxRegressionFixes < 1 {
		c.MaxRegressionFixes = 1
	}
	if c.TestTimeout < time.Second {
		c.TestTimeout = time.Second
	}
	if c.SuiteTimeout < c.TestTimeout {
		c.SuiteTimeout = c.TestTimeout * 10
	}
	if c.TotalTimeout < c.SuiteTimeout {
		c.TotalTimeout = c.SuiteTimeout * 2
	}
	if c.MaxOutputBytes < 1024 {
		c.MaxOutputBytes = 1024
	}
	return nil
}

// =============================================================================
// CONFIGURATION OPTIONS
// =============================================================================

// Option is a function that modifies Config.
type Option func(*Config)

// WithMaxTestRetries sets the maximum test generation retries.
func WithMaxTestRetries(n int) Option {
	return func(c *Config) {
		c.MaxTestRetries = n
	}
}

// WithMaxFixRetries sets the maximum fix generation retries.
func WithMaxFixRetries(n int) Option {
	return func(c *Config) {
		c.MaxFixRetries = n
	}
}

// WithMaxRegressionFixes sets the maximum regression fix retries.
func WithMaxRegressionFixes(n int) Option {
	return func(c *Config) {
		c.MaxRegressionFixes = n
	}
}

// WithTestTimeout sets the per-test timeout.
func WithTestTimeout(d time.Duration) Option {
	return func(c *Config) {
		c.TestTimeout = d
	}
}

// WithSuiteTimeout sets the test suite timeout.
func WithSuiteTimeout(d time.Duration) Option {
	return func(c *Config) {
		c.SuiteTimeout = d
	}
}

// WithTotalTimeout sets the total TDG session timeout.
func WithTotalTimeout(d time.Duration) Option {
	return func(c *Config) {
		c.TotalTimeout = d
	}
}

// WithMaxOutputBytes sets the maximum test output capture size.
func WithMaxOutputBytes(n int) Option {
	return func(c *Config) {
		c.MaxOutputBytes = n
	}
}

// WithLintCheck enables or disables lint checking.
func WithLintCheck(enabled bool) Option {
	return func(c *Config) {
		c.EnableLintCheck = enabled
	}
}

// WithWorkingDir sets the working directory for test execution.
func WithWorkingDir(dir string) Option {
	return func(c *Config) {
		c.WorkingDir = dir
	}
}

// NewConfig creates a Config with the given options applied.
//
// Inputs:
//
//	opts - Options to apply to the default config
//
// Outputs:
//
//	*Config - Configuration with options applied
func NewConfig(opts ...Option) *Config {
	cfg := DefaultConfig()
	for _, opt := range opts {
		opt(cfg)
	}
	_ = cfg.Validate()
	return cfg
}
