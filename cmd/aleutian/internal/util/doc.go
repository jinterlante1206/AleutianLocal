// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.

// Package util provides foundational utilities for the Aleutian CLI.
//
// This package contains low-level utilities that have no dependencies on
// other internal packages. All utilities depend only on the Go standard
// library, making this a leaf package in the dependency graph.
//
// # Overview
//
// The util package provides seven categories of utilities:
//
//   - Timeout Management: Enforce minimum and default timeouts to prevent hangs
//   - Environment Variables: Type-safe environment variable handling with validation
//   - Command Errors: Rich error wrapping for command execution failures
//   - Ring Buffer: Thread-safe circular buffer for bounded data collection
//   - Progress Indicators: CLI spinners for long-running operations
//   - Saga Pattern: Multi-step transactions with automatic rollback
//   - Goroutine Safety: Panic recovery for background goroutines
//
// # Thread Safety
//
// All types in this package are safe for concurrent use from multiple
// goroutines unless their documentation explicitly states otherwise.
// Specifically:
//
//   - [RingBuffer] is fully thread-safe (protected by mutex)
//   - [Spinner] is thread-safe for Start/Stop/SetMessage
//   - [Saga] is NOT thread-safe (use from single goroutine)
//   - [EnvVars] is NOT thread-safe (do not modify concurrently)
//
// # Key Types
//
// Timeout utilities:
//
//	cfg := util.NewTimeoutConfig()
//	timeout := util.EnforceMinTimeout(requested, util.MinHTTPTimeout)
//
// Environment variables:
//
//	envs, err := util.NewEnvVars(
//	    util.EnvVar{Key: "API_KEY", Value: "secret", Sensitive: true},
//	)
//	fmt.Println(envs.RedactedSlice()) // Safe for logging
//
// Command errors:
//
//	err := util.NewCommandError("podman-compose up", 1, stderr, originalErr)
//	if cmdErr, ok := err.(*util.CommandError); ok {
//	    fmt.Println(cmdErr.Stderr)
//	}
//
// Ring buffer:
//
//	buffer := util.NewRingBuffer[string](1000)
//	buffer.Push("log line")
//	items := buffer.Drain()
//
// Progress spinner:
//
//	spinner := util.NewSpinner(util.SpinnerConfig{Message: "Loading..."})
//	spinner.Start()
//	defer spinner.Stop()
//
// Saga transactions:
//
//	saga := util.NewSaga(util.DefaultSagaConfig())
//	saga.AddStep(util.SagaStep{
//	    Name:       "Create Network",
//	    Execute:    createNetwork,
//	    Compensate: deleteNetwork,
//	})
//	if err := saga.Execute(ctx); err != nil {
//	    // All completed steps have been rolled back
//	}
//
// Safe goroutines:
//
//	util.SafeGo(func() {
//	    riskyOperation()
//	}, func(r util.SafeGoResult) {
//	    log.Printf("Panic recovered: %v\n%s", r.PanicValue, r.Stack)
//	})
//
// # File Mapping
//
// This package was extracted from cmd/aleutian/ as part of the codebase
// reorganization (Phase 1). The original files map as follows:
//
//	Original                    → New Location
//	timeouts.go                 → internal/util/timeouts.go
//	env_vars.go                 → internal/util/env.go
//	command_error.go            → internal/util/errors.go
//	ring_buffer.go              → internal/util/ring_buffer.go
//	progress.go                 → internal/util/progress.go
//	saga.go                     → internal/util/saga.go
//	goroutine_safety.go         → internal/util/goroutine.go
//
// # Design Principles
//
// All utilities in this package follow these principles:
//
//  1. Single Responsibility: Each type/function does one thing well
//  2. Interface First: Interfaces defined before implementations
//  3. Defensive Defaults: Safe defaults that prevent common mistakes
//  4. Explicit Over Implicit: No hidden behavior or magic
//  5. Testable: All functionality is easily unit testable
//
// # Security Considerations
//
//   - [EnvVar] supports sensitivity marking for safe logging
//   - [CommandError] captures stderr without exposing to end users
//   - [SafeGoResult] captures full stack traces for debugging
//
// # Performance Considerations
//
//   - [RingBuffer] pre-allocates memory to avoid runtime allocations
//   - [Spinner] uses efficient ticker-based animation
//   - [Saga] executes steps sequentially (no parallel execution)
package util
