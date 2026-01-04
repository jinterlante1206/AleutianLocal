// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

/*
Package main provides ProcessManager for abstracting external process execution.

ProcessManager enables testable interaction with the operating system's process
management capabilities. All exec.Command calls in the stack management code
should go through this interface to enable mocking in unit tests.

# Design Rationale

Direct calls to exec.Command are not testable because they execute real processes.
By abstracting process execution behind an interface, we can:
  - Mock process execution in tests
  - Capture and verify command invocations
  - Simulate success/failure scenarios without real processes
*/
package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"sync"
)

// -----------------------------------------------------------------------------
// Interface Definition
// -----------------------------------------------------------------------------

// ProcessManager handles external process operations.
//
// This interface abstracts all interaction with the operating system's process
// management, enabling testable code that doesn't require real process execution.
//
// # Thread Safety
//
// Implementations must be safe for concurrent use from multiple goroutines.
//
// # Context Handling
//
// All methods accept a context.Context for cancellation and timeout support.
// Long-running processes should respect context cancellation.
type ProcessManager interface {
	// Run executes a command synchronously and returns its output.
	//
	// # Description
	//
	// Executes the specified command with arguments and waits for completion.
	// Returns the combined stdout output on success.
	//
	// # Inputs
	//
	//   - ctx: Context for cancellation/timeout
	//   - name: The executable name or path
	//   - args: Command arguments (variadic)
	//
	// # Outputs
	//
	//   - []byte: Combined stdout output
	//   - error: Non-nil if command fails or is cancelled
	//
	// # Examples
	//
	//   output, err := pm.Run(ctx, "podman", "machine", "list")
	//   if err != nil {
	//       return fmt.Errorf("failed to list machines: %w", err)
	//   }
	//
	// # Limitations
	//
	//   - Stderr is captured but not returned separately
	//   - Large output may consume significant memory
	Run(ctx context.Context, name string, args ...string) ([]byte, error)

	// RunWithInput executes a command with data piped to stdin.
	//
	// # Description
	//
	// Executes the specified command and pipes the input data to the process's
	// stdin. Useful for commands that read from stdin (e.g., podman secret create).
	//
	// # Inputs
	//
	//   - ctx: Context for cancellation/timeout
	//   - name: The executable name or path
	//   - input: Data to write to stdin
	//   - args: Command arguments (variadic)
	//
	// # Outputs
	//
	//   - []byte: Combined stdout output
	//   - error: Non-nil if command fails, stdin write fails, or cancelled
	//
	// # Examples
	//
	//   secret := []byte("my-secret-value")
	//   _, err := pm.RunWithInput(ctx, "podman", secret, "secret", "create", "mytoken", "-")
	//   if err != nil {
	//       return fmt.Errorf("failed to create secret: %w", err)
	//   }
	//
	// # Limitations
	//
	//   - Input is fully buffered in memory before being written
	RunWithInput(ctx context.Context, name string, input []byte, args ...string) ([]byte, error)

	// Start launches a background process and returns immediately.
	//
	// # Description
	//
	// Starts a process in the background without waiting for completion.
	// Returns the process ID (PID) for tracking.
	//
	// # Inputs
	//
	//   - ctx: Context (not used for cancellation, but for future extensions)
	//   - name: The executable name or path
	//   - args: Command arguments (variadic)
	//
	// # Outputs
	//
	//   - int: Process ID of the started process
	//   - error: Non-nil if process fails to start
	//
	// # Examples
	//
	//   pid, err := pm.Start(ctx, "ollama", "serve")
	//   if err != nil {
	//       return fmt.Errorf("failed to start ollama: %w", err)
	//   }
	//   fmt.Printf("Started ollama with PID %d\n", pid)
	//
	// # Limitations
	//
	//   - Process output is discarded (not captured)
	//   - No automatic cleanup when parent process exits
	//   - Context cancellation does not kill started process
	Start(ctx context.Context, name string, args ...string) (int, error)

	// IsRunning checks if a process matching the pattern exists.
	//
	// # Description
	//
	// Searches for running processes whose command line matches the given pattern.
	// Uses pgrep on Unix systems for process detection.
	//
	// # Inputs
	//
	//   - ctx: Context for cancellation/timeout
	//   - pattern: String pattern to match against process command lines
	//
	// # Outputs
	//
	//   - bool: True if at least one matching process is running
	//   - int: PID of first matching process (0 if not found)
	//   - error: Non-nil if process detection fails (not for "not found")
	//
	// # Examples
	//
	//   running, pid, err := pm.IsRunning(ctx, "Podman Desktop")
	//   if err != nil {
	//       return fmt.Errorf("failed to check process: %w", err)
	//   }
	//   if running {
	//       fmt.Printf("Podman Desktop is running (PID %d)\n", pid)
	//   }
	//
	// # Limitations
	//
	//   - Pattern matching behavior depends on the platform's pgrep
	//   - Only returns first matching PID, not all matches
	//
	// # Assumptions
	//
	//   - pgrep is available on the system (standard on macOS/Linux)
	IsRunning(ctx context.Context, pattern string) (bool, int, error)
}

// -----------------------------------------------------------------------------
// Implementation
// -----------------------------------------------------------------------------

// DefaultProcessManager implements ProcessManager using os/exec.
//
// This is the production implementation that executes real processes on the
// system. Use MockProcessManager in tests instead.
type DefaultProcessManager struct{}

// NewDefaultProcessManager creates a new DefaultProcessManager.
//
// # Description
//
// Creates a ProcessManager that executes real processes using os/exec.
// This should be used in production code.
//
// # Outputs
//
//   - *DefaultProcessManager: Ready-to-use process manager
//
// # Examples
//
//	pm := NewDefaultProcessManager()
//	output, err := pm.Run(ctx, "podman", "version")
func NewDefaultProcessManager() *DefaultProcessManager {
	return &DefaultProcessManager{}
}

// Run executes a command synchronously and returns its output.
func (pm *DefaultProcessManager) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		// Include stderr in error for debugging
		if stderr.Len() > 0 {
			return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
		}
		return nil, err
	}

	return stdout.Bytes(), nil
}

// RunWithInput executes a command with data piped to stdin.
func (pm *DefaultProcessManager) RunWithInput(ctx context.Context, name string, input []byte, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = bytes.NewReader(input)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		if stderr.Len() > 0 {
			return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
		}
		return nil, err
	}

	return stdout.Bytes(), nil
}

// Start launches a background process and returns immediately.
func (pm *DefaultProcessManager) Start(ctx context.Context, name string, args ...string) (int, error) {
	cmd := exec.Command(name, args...)

	// Detach from parent process group
	// Note: Process will continue running after parent exits

	if err := cmd.Start(); err != nil {
		return 0, fmt.Errorf("failed to start %s: %w", name, err)
	}

	return cmd.Process.Pid, nil
}

// IsRunning checks if a process matching the pattern exists.
func (pm *DefaultProcessManager) IsRunning(ctx context.Context, pattern string) (bool, int, error) {
	cmd := exec.CommandContext(ctx, "pgrep", "-f", pattern)
	output, err := cmd.Output()

	if err != nil {
		// pgrep returns exit code 1 when no processes found - this is not an error
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return false, 0, nil
		}
		return false, 0, fmt.Errorf("pgrep failed: %w", err)
	}

	// Parse the first PID from output
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) > 0 && lines[0] != "" {
		pid, err := strconv.Atoi(lines[0])
		if err != nil {
			return true, 0, nil // Process found but PID parse failed
		}
		return true, pid, nil
	}

	return false, 0, nil
}

// -----------------------------------------------------------------------------
// Mock Implementation for Testing
// -----------------------------------------------------------------------------

// MockProcessManager is a test double for ProcessManager.
//
// Configure the mock by setting function fields before use. If a function
// field is nil and the corresponding method is called, it will panic.
//
// # Examples
//
//	mock := &MockProcessManager{
//	    RunFunc: func(ctx context.Context, name string, args ...string) ([]byte, error) {
//	        if name == "podman" && args[0] == "version" {
//	            return []byte("podman version 4.0.0"), nil
//	        }
//	        return nil, fmt.Errorf("unexpected command: %s", name)
//	    },
//	}
type MockProcessManager struct {
	// RunFunc is called when Run is invoked
	RunFunc func(ctx context.Context, name string, args ...string) ([]byte, error)

	// RunWithInputFunc is called when RunWithInput is invoked
	RunWithInputFunc func(ctx context.Context, name string, input []byte, args ...string) ([]byte, error)

	// StartFunc is called when Start is invoked
	StartFunc func(ctx context.Context, name string, args ...string) (int, error)

	// IsRunningFunc is called when IsRunning is invoked
	IsRunningFunc func(ctx context.Context, pattern string) (bool, int, error)

	// Calls records all method invocations for verification
	Calls []ProcessManagerCall

	// mu protects Calls for concurrent access
	mu sync.Mutex
}

// ProcessManagerCall records a single method invocation.
type ProcessManagerCall struct {
	Method string
	Name   string
	Args   []string
	Input  []byte
}

// Run delegates to RunFunc and records the call.
func (m *MockProcessManager) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, ProcessManagerCall{
		Method: "Run",
		Name:   name,
		Args:   args,
	})
	if m.RunFunc == nil {
		panic("MockProcessManager.RunFunc not set")
	}
	return m.RunFunc(ctx, name, args...)
}

// RunWithInput delegates to RunWithInputFunc and records the call.
func (m *MockProcessManager) RunWithInput(ctx context.Context, name string, input []byte, args ...string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, ProcessManagerCall{
		Method: "RunWithInput",
		Name:   name,
		Args:   args,
		Input:  input,
	})
	if m.RunWithInputFunc == nil {
		panic("MockProcessManager.RunWithInputFunc not set")
	}
	return m.RunWithInputFunc(ctx, name, input, args...)
}

// Start delegates to StartFunc and records the call.
func (m *MockProcessManager) Start(ctx context.Context, name string, args ...string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, ProcessManagerCall{
		Method: "Start",
		Name:   name,
		Args:   args,
	})
	if m.StartFunc == nil {
		panic("MockProcessManager.StartFunc not set")
	}
	return m.StartFunc(ctx, name, args...)
}

// IsRunning delegates to IsRunningFunc and records the call.
func (m *MockProcessManager) IsRunning(ctx context.Context, pattern string) (bool, int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, ProcessManagerCall{
		Method: "IsRunning",
		Name:   pattern,
	})
	if m.IsRunningFunc == nil {
		panic("MockProcessManager.IsRunningFunc not set")
	}
	return m.IsRunningFunc(ctx, pattern)
}

// Reset clears all recorded calls.
func (m *MockProcessManager) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = nil
}

// GetCalls returns a copy of all recorded calls.
func (m *MockProcessManager) GetCalls() []ProcessManagerCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]ProcessManagerCall, len(m.Calls))
	copy(result, m.Calls)
	return result
}

// Compile-time interface compliance check.
var (
	_ ProcessManager = (*DefaultProcessManager)(nil)
	_ ProcessManager = (*MockProcessManager)(nil)
)
