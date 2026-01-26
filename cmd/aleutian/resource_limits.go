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
	"fmt"
	"net/http"
	"runtime"
	"sync"
	"syscall"
	"time"
)

// ResourceChecker defines the interface for system resource checking.
//
// # Description
//
// ResourceChecker validates that system resources (file descriptors,
// memory, etc.) are sufficient for operation and provides warnings
// when limits are too low.
//
// # Thread Safety
//
// Implementations must be safe for concurrent use.
type ResourceChecker interface {
	// Check validates all resource limits and returns warnings.
	Check() ResourceLimits

	// CheckFDLimit checks only file descriptor limits.
	CheckFDLimit() (soft, hard uint64, warnings []string)

	// IsHealthy returns true if all limits are acceptable.
	IsHealthy() bool
}

// ResourceLimits contains system resource limit information.
//
// # Description
//
// Provides a snapshot of current system resource limits and any
// warnings about limits that may cause problems.
type ResourceLimits struct {
	// SoftFD is the current soft limit for file descriptors.
	SoftFD uint64

	// HardFD is the hard limit for file descriptors.
	HardFD uint64

	// RecommendedFD is the minimum recommended FD limit.
	RecommendedFD uint64

	// CurrentFDUsage is an estimate of currently used FDs.
	CurrentFDUsage int

	// Warnings lists any resource concerns.
	Warnings []string

	// CheckedAt is when this check was performed.
	CheckedAt time.Time
}

// HasWarnings returns true if there are any warnings.
func (r ResourceLimits) HasWarnings() bool {
	return len(r.Warnings) > 0
}

// ResourceLimitsConfig configures resource limit checking.
//
// # Description
//
// Allows customization of thresholds for resource warnings.
//
// # Example
//
//	config := ResourceLimitsConfig{
//	    MinRecommendedFD: 2048,
//	    WarnAtFDPercent:  80,
//	}
type ResourceLimitsConfig struct {
	// MinRecommendedFD is the minimum recommended file descriptor limit.
	// Default: 1024
	MinRecommendedFD uint64

	// WarnAtFDPercent warns when FD usage exceeds this percentage.
	// Default: 80
	WarnAtFDPercent int
}

// DefaultResourceLimitsConfig returns sensible defaults.
//
// # Description
//
// Returns configuration suitable for most CLI applications.
//
// # Outputs
//
//   - ResourceLimitsConfig: Configuration with default values
func DefaultResourceLimitsConfig() ResourceLimitsConfig {
	return ResourceLimitsConfig{
		MinRecommendedFD: 1024,
		WarnAtFDPercent:  80,
	}
}

// DefaultResourceLimitsChecker implements ResourceChecker.
//
// # Description
//
// Checks system resource limits using syscalls and provides
// warnings when limits are too low. On macOS, the default
// ulimit -n is often 256, which is insufficient for applications
// with multiple services, health checks, and log files.
//
// # Use Cases
//
//   - Startup validation
//   - Pre-flight checks before heavy operations
//   - Debugging "too many open files" errors
//
// # Thread Safety
//
// DefaultResourceLimitsChecker is safe for concurrent use.
//
// # Limitations
//
//   - FD usage estimation is approximate
//   - Some platforms may not support all syscalls
//
// # Assumptions
//
//   - syscall.RLIMIT_NOFILE is available
//   - Running on Unix-like system (macOS, Linux)
//
// # Example
//
//	checker := NewResourceLimitsChecker(DefaultResourceLimitsConfig())
//	limits := checker.Check()
//	if limits.HasWarnings() {
//	    for _, w := range limits.Warnings {
//	        log.Printf("WARNING: %s", w)
//	    }
//	}
type DefaultResourceLimitsChecker struct {
	config ResourceLimitsConfig
	mu     sync.RWMutex
}

// NewResourceLimitsChecker creates a new resource limits checker.
//
// # Description
//
// Creates a checker with the specified configuration.
//
// # Inputs
//
//   - config: Configuration for limit thresholds
//
// # Outputs
//
//   - *DefaultResourceLimitsChecker: New checker
//
// # Example
//
//	checker := NewResourceLimitsChecker(ResourceLimitsConfig{
//	    MinRecommendedFD: 2048,
//	})
func NewResourceLimitsChecker(config ResourceLimitsConfig) *DefaultResourceLimitsChecker {
	if config.MinRecommendedFD == 0 {
		config.MinRecommendedFD = 1024
	}
	if config.WarnAtFDPercent == 0 {
		config.WarnAtFDPercent = 80
	}

	return &DefaultResourceLimitsChecker{
		config: config,
	}
}

// Check validates all resource limits.
//
// # Description
//
// Performs a comprehensive check of system resource limits and
// returns warnings for any that are too low.
//
// # Outputs
//
//   - ResourceLimits: Current limits and any warnings
//
// # Example
//
//	limits := checker.Check()
//	if !limits.HasWarnings() {
//	    log.Println("All resource limits are acceptable")
//	}
func (c *DefaultResourceLimitsChecker) Check() ResourceLimits {
	soft, hard, warnings := c.CheckFDLimit()

	limits := ResourceLimits{
		SoftFD:        soft,
		HardFD:        hard,
		RecommendedFD: c.config.MinRecommendedFD,
		Warnings:      warnings,
		CheckedAt:     time.Now(),
	}

	// Estimate current FD usage
	limits.CurrentFDUsage = c.estimateFDUsage()

	// Warn if usage is high relative to limit
	if soft > 0 && limits.CurrentFDUsage > 0 {
		usagePercent := (limits.CurrentFDUsage * 100) / int(soft)
		if usagePercent >= c.config.WarnAtFDPercent {
			limits.Warnings = append(limits.Warnings,
				fmt.Sprintf("File descriptor usage is %d%% (%d/%d). "+
					"Consider closing unused connections.",
					usagePercent, limits.CurrentFDUsage, soft))
		}
	}

	return limits
}

// CheckFDLimit checks file descriptor limits.
//
// # Description
//
// Retrieves the soft and hard file descriptor limits and generates
// warnings if the soft limit is below the recommended minimum.
//
// # Outputs
//
//   - soft: Current soft limit
//   - hard: Hard limit (maximum possible)
//   - warnings: Any concerns about the limits
//
// # Example
//
//	soft, hard, warnings := checker.CheckFDLimit()
//	if soft < 1024 {
//	    fmt.Printf("Run 'ulimit -n %d' to increase limit\n", hard)
//	}
func (c *DefaultResourceLimitsChecker) CheckFDLimit() (soft, hard uint64, warnings []string) {
	var rLimit syscall.Rlimit
	err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &rLimit)
	if err != nil {
		warnings = append(warnings,
			fmt.Sprintf("Unable to check file descriptor limits: %v", err))
		return 0, 0, warnings
	}

	soft = rLimit.Cur
	hard = rLimit.Max

	if soft < c.config.MinRecommendedFD {
		suggestion := c.config.MinRecommendedFD
		if suggestion > hard {
			suggestion = hard
		}

		warnings = append(warnings,
			fmt.Sprintf("File descriptor limit is %d (recommended: %d). "+
				"Run 'ulimit -n %d' to increase.",
				soft, c.config.MinRecommendedFD, suggestion))
	}

	return soft, hard, warnings
}

// IsHealthy returns true if resource limits are acceptable.
//
// # Description
//
// Quick check to determine if the system has sufficient resources.
// Does not provide detailed warnings.
//
// # Outputs
//
//   - bool: true if all limits are acceptable
func (c *DefaultResourceLimitsChecker) IsHealthy() bool {
	limits := c.Check()
	return !limits.HasWarnings()
}

// estimateFDUsage estimates current file descriptor usage.
func (c *DefaultResourceLimitsChecker) estimateFDUsage() int {
	// Use runtime.NumGoroutine as a rough proxy
	// Each goroutine with a network connection uses at least one FD
	goroutines := runtime.NumGoroutine()

	// Add estimate for other FDs (stdin, stdout, stderr, log files, etc.)
	baselineFDs := 10

	return goroutines + baselineFDs
}

// Compile-time interface check
var _ ResourceChecker = (*DefaultResourceLimitsChecker)(nil)

// SharedHTTPClient provides a connection-pooled HTTP client.
//
// # Description
//
// A pre-configured HTTP client with connection pooling to reduce
// file descriptor usage. Should be used instead of creating new
// http.Client instances.
//
// # Thread Safety
//
// Safe for concurrent use from multiple goroutines.
//
// # Example
//
//	client := GetSharedHTTPClient()
//	resp, err := client.Get("http://localhost:8080/health")
var sharedHTTPClient *http.Client
var sharedHTTPClientOnce sync.Once

// GetSharedHTTPClient returns a singleton HTTP client with pooling.
//
// # Description
//
// Returns a shared HTTP client configured with connection pooling
// and sensible timeouts. Use this instead of creating new http.Client
// instances to reduce file descriptor usage.
//
// # Outputs
//
//   - *http.Client: Shared, pooled HTTP client
//
// # Example
//
//	// Good: Use shared client
//	client := GetSharedHTTPClient()
//	resp, err := client.Get(url)
//
//	// Bad: Creates new client and transport each time
//	client := &http.Client{}
//	resp, err := client.Get(url)
func GetSharedHTTPClient() *http.Client {
	sharedHTTPClientOnce.Do(func() {
		sharedHTTPClient = &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 10,
				MaxConnsPerHost:     20,
				IdleConnTimeout:     90 * time.Second,
				DisableKeepAlives:   false,
			},
		}
	})
	return sharedHTTPClient
}

// GetSharedHTTPClientWithTimeout returns the shared client with custom timeout.
//
// # Description
//
// Returns a copy of the shared client's transport but with a custom
// timeout. This maintains connection pooling while allowing timeout
// customization.
//
// # Inputs
//
//   - timeout: Request timeout
//
// # Outputs
//
//   - *http.Client: HTTP client with custom timeout
//
// # Note
//
// This creates a new Client struct but shares the Transport,
// so connections are still pooled.
func GetSharedHTTPClientWithTimeout(timeout time.Duration) *http.Client {
	base := GetSharedHTTPClient()
	return &http.Client{
		Timeout:   timeout,
		Transport: base.Transport,
	}
}
