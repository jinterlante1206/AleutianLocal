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
	"io"
	"os"
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

	// RunInDir executes a command in a specific directory with environment variables.
	//
	// # Description
	//
	// Executes the specified command in the given working directory with custom
	// environment variables. Returns separate stdout/stderr and the exit code,
	// enabling detailed error diagnostics.
	//
	// # Inputs
	//
	//   - ctx: Context for cancellation/timeout
	//   - dir: Working directory for command execution
	//   - env: Environment variables in KEY=VALUE format (appended to current env)
	//   - name: The executable name or path
	//   - args: Command arguments (variadic)
	//
	// # Outputs
	//
	//   - stdout: Standard output as string
	//   - stderr: Standard error as string
	//   - exitCode: Process exit code (0 for success)
	//   - error: Non-nil only for execution failures (not non-zero exit)
	//
	// # Example
	//
	//   stdout, stderr, code, err := pm.RunInDir(ctx,
	//       "/home/user/.aleutian",
	//       []string{"OLLAMA_MODEL=gpt-oss"},
	//       "podman-compose", "up", "-d",
	//   )
	//   if err != nil {
	//       return fmt.Errorf("failed to execute: %w", err)
	//   }
	//   if code != 0 {
	//       return fmt.Errorf("command failed (exit %d): %s", code, stderr)
	//   }
	//
	// # Limitations
	//
	//   - All output is buffered in memory
	//   - Environment is inherited from parent plus provided env
	//
	// # Assumptions
	//
	//   - Working directory exists and is accessible
	//   - Environment variables are properly formatted
	RunInDir(ctx context.Context, dir string, env []string, name string, args ...string) (stdout, stderr string, exitCode int, err error)

	// RunStreaming executes a command and streams output to a writer.
	//
	// # Description
	//
	// Executes the specified command and streams combined stdout/stderr to
	// the provided writer in real-time. Useful for long-running commands
	// where output should be displayed as it's produced.
	//
	// # Inputs
	//
	//   - ctx: Context for cancellation (terminates the process)
	//   - dir: Working directory for command execution
	//   - w: Writer to receive output (typically os.Stdout)
	//   - name: The executable name or path
	//   - args: Command arguments (variadic)
	//
	// # Outputs
	//
	//   - error: Non-nil if command fails to start or is cancelled
	//
	// # Example
	//
	//   ctx, cancel := context.WithCancel(context.Background())
	//   defer cancel()
	//   err := pm.RunStreaming(ctx,
	//       "/home/user/.aleutian",
	//       os.Stdout,
	//       "podman-compose", "logs", "-f",
	//   )
	//   // Returns when context is cancelled or command exits
	//
	// # Limitations
	//
	//   - Cannot capture output separately (streams directly)
	//   - Exit code is embedded in error (use exec.ExitError)
	//   - Combines stdout and stderr into single stream
	//
	// # Assumptions
	//
	//   - Writer is safe for concurrent writes
	//   - Working directory exists and is accessible
	RunStreaming(ctx context.Context, dir string, w io.Writer, name string, args ...string) error
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

// RunInDir executes a command in a specific directory with environment variables.
//
// # Description
//
// Executes the specified command in the given working directory with custom
// environment variables. Returns separate stdout/stderr and the exit code.
//
// # Inputs
//
//   - ctx: Context for cancellation/timeout
//   - dir: Working directory for command execution
//   - env: Environment variables in KEY=VALUE format
//   - name: The executable name or path
//   - args: Command arguments (variadic)
//
// # Outputs
//
//   - stdout: Standard output as string
//   - stderr: Standard error as string
//   - exitCode: Process exit code (0 for success)
//   - error: Non-nil only for execution failures (not non-zero exit)
//
// # Example
//
//	stdout, stderr, code, err := pm.RunInDir(ctx,
//	    "/home/user/.aleutian",
//	    []string{"OLLAMA_MODEL=gpt-oss"},
//	    "podman-compose", "up", "-d",
//	)
//
// # Limitations
//
//   - All output is buffered in memory
//
// # Assumptions
//
//   - Working directory exists
func (pm *DefaultProcessManager) RunInDir(ctx context.Context, dir string, env []string, name string, args ...string) (stdout, stderr string, exitCode int, err error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir

	// Inherit current environment and add custom variables
	cmd.Env = append(os.Environ(), env...)

	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	runErr := cmd.Run()

	stdout = stdoutBuf.String()
	stderr = stderrBuf.String()

	// Extract exit code
	if runErr != nil {
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			exitCode = exitErr.ExitCode()
			// Non-zero exit is not an error for this method
			return stdout, stderr, exitCode, nil
		}
		// Actual execution failure (command not found, etc.)
		return stdout, stderr, -1, runErr
	}

	return stdout, stderr, 0, nil
}

// RunStreaming executes a command and streams output to a writer.
//
// # Description
//
// Executes the specified command and streams combined stdout/stderr to
// the provided writer in real-time.
//
// # Inputs
//
//   - ctx: Context for cancellation (terminates the process)
//   - dir: Working directory for command execution
//   - w: Writer to receive output
//   - name: The executable name or path
//   - args: Command arguments (variadic)
//
// # Outputs
//
//   - error: Non-nil if command fails to start or is cancelled
//
// # Example
//
//	err := pm.RunStreaming(ctx, "/path", os.Stdout, "podman-compose", "logs", "-f")
//
// # Limitations
//
//   - Cannot capture output separately
//
// # Assumptions
//
//   - Writer is safe for concurrent writes
func (pm *DefaultProcessManager) RunStreaming(ctx context.Context, dir string, w io.Writer, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Stdout = w
	cmd.Stderr = w

	if err := cmd.Run(); err != nil {
		// Context cancellation is expected for streaming commands
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return err
	}

	return nil
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

	// RunInDirFunc is called when RunInDir is invoked
	RunInDirFunc func(ctx context.Context, dir string, env []string, name string, args ...string) (stdout, stderr string, exitCode int, err error)

	// RunStreamingFunc is called when RunStreaming is invoked
	RunStreamingFunc func(ctx context.Context, dir string, w io.Writer, name string, args ...string) error

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
	Dir    string
	Env    []string
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

// RunInDir delegates to RunInDirFunc and records the call.
//
// # Description
//
// Records the call with dir and env, then delegates to RunInDirFunc.
// Panics if RunInDirFunc is nil to catch missing mock configuration.
//
// # Inputs
//
//   - ctx: Context for cancellation/timeout
//   - dir: Working directory for command execution
//   - env: Environment variables in KEY=VALUE format
//   - name: The executable name or path
//   - args: Command arguments (variadic)
//
// # Outputs
//
//   - stdout: Standard output as string
//   - stderr: Standard error as string
//   - exitCode: Process exit code
//   - error: From RunInDirFunc
//
// # Example
//
//	mock.RunInDirFunc = func(ctx context.Context, dir string, env []string, name string, args ...string) (string, string, int, error) {
//	    return "output", "", 0, nil
//	}
//
// # Limitations
//
//   - Panics if RunInDirFunc not set
//
// # Assumptions
//
//   - RunInDirFunc is set before calling
func (m *MockProcessManager) RunInDir(ctx context.Context, dir string, env []string, name string, args ...string) (stdout, stderr string, exitCode int, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, ProcessManagerCall{
		Method: "RunInDir",
		Name:   name,
		Args:   args,
		Dir:    dir,
		Env:    env,
	})
	if m.RunInDirFunc == nil {
		panic("MockProcessManager.RunInDirFunc not set")
	}
	return m.RunInDirFunc(ctx, dir, env, name, args...)
}

// RunStreaming delegates to RunStreamingFunc and records the call.
//
// # Description
//
// Records the call with dir, then delegates to RunStreamingFunc.
// Panics if RunStreamingFunc is nil to catch missing mock configuration.
//
// # Inputs
//
//   - ctx: Context for cancellation
//   - dir: Working directory for command execution
//   - w: Writer to receive output
//   - name: The executable name or path
//   - args: Command arguments (variadic)
//
// # Outputs
//
//   - error: From RunStreamingFunc
//
// # Example
//
//	mock.RunStreamingFunc = func(ctx context.Context, dir string, w io.Writer, name string, args ...string) error {
//	    w.Write([]byte("streaming output"))
//	    return nil
//	}
//
// # Limitations
//
//   - Panics if RunStreamingFunc not set
//
// # Assumptions
//
//   - RunStreamingFunc is set before calling
func (m *MockProcessManager) RunStreaming(ctx context.Context, dir string, w io.Writer, name string, args ...string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, ProcessManagerCall{
		Method: "RunStreaming",
		Name:   name,
		Args:   args,
		Dir:    dir,
	})
	if m.RunStreamingFunc == nil {
		panic("MockProcessManager.RunStreamingFunc not set")
	}
	return m.RunStreamingFunc(ctx, dir, w, name, args...)
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
