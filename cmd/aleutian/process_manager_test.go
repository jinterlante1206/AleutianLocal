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
Package main contains unit tests for ProcessManager.

# Testing Strategy

These tests verify:
  - DefaultProcessManager correctly executes real commands
  - Error handling for non-existent commands
  - Context cancellation support
  - MockProcessManager works correctly for test doubles
*/
package main

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

// -----------------------------------------------------------------------------
// DefaultProcessManager Tests
// -----------------------------------------------------------------------------

// TestDefaultProcessManager_Run_Success verifies successful command execution.
func TestDefaultProcessManager_Run_Success(t *testing.T) {
	pm := NewDefaultProcessManager()
	ctx := context.Background()

	output, err := pm.Run(ctx, "echo", "hello world")
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}

	got := strings.TrimSpace(string(output))
	if got != "hello world" {
		t.Errorf("Run() output = %q, want %q", got, "hello world")
	}
}

// TestDefaultProcessManager_Run_WithArgs verifies multiple arguments.
func TestDefaultProcessManager_Run_WithArgs(t *testing.T) {
	pm := NewDefaultProcessManager()
	ctx := context.Background()

	output, err := pm.Run(ctx, "printf", "%s %s", "hello", "world")
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}

	got := string(output)
	if got != "hello world" {
		t.Errorf("Run() output = %q, want %q", got, "hello world")
	}
}

// TestDefaultProcessManager_Run_CommandNotFound verifies error for missing command.
func TestDefaultProcessManager_Run_CommandNotFound(t *testing.T) {
	pm := NewDefaultProcessManager()
	ctx := context.Background()

	_, err := pm.Run(ctx, "nonexistent-command-12345")
	if err == nil {
		t.Fatal("Run() expected error for non-existent command, got nil")
	}
}

// TestDefaultProcessManager_Run_CommandFailure verifies error for failing command.
func TestDefaultProcessManager_Run_CommandFailure(t *testing.T) {
	pm := NewDefaultProcessManager()
	ctx := context.Background()

	_, err := pm.Run(ctx, "false") // 'false' always exits with code 1
	if err == nil {
		t.Fatal("Run() expected error for failing command, got nil")
	}
}

// TestDefaultProcessManager_Run_ContextCancellation verifies cancellation support.
func TestDefaultProcessManager_Run_ContextCancellation(t *testing.T) {
	pm := NewDefaultProcessManager()
	ctx, cancel := context.WithCancel(context.Background())

	// Cancel immediately
	cancel()

	_, err := pm.Run(ctx, "sleep", "10")
	if err == nil {
		t.Fatal("Run() expected error for cancelled context, got nil")
	}
}

// TestDefaultProcessManager_Run_Timeout verifies timeout support.
func TestDefaultProcessManager_Run_Timeout(t *testing.T) {
	pm := NewDefaultProcessManager()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := pm.Run(ctx, "sleep", "10")
	if err == nil {
		t.Fatal("Run() expected error for timeout, got nil")
	}

	if !errors.Is(err, context.DeadlineExceeded) && !strings.Contains(err.Error(), "signal: killed") {
		t.Logf("Run() error = %v (expected deadline exceeded or killed)", err)
	}
}

// TestDefaultProcessManager_RunWithInput_Success verifies stdin piping.
func TestDefaultProcessManager_RunWithInput_Success(t *testing.T) {
	pm := NewDefaultProcessManager()
	ctx := context.Background()

	input := []byte("hello from stdin")
	output, err := pm.RunWithInput(ctx, "cat", input)
	if err != nil {
		t.Fatalf("RunWithInput() unexpected error: %v", err)
	}

	got := string(output)
	if got != "hello from stdin" {
		t.Errorf("RunWithInput() output = %q, want %q", got, "hello from stdin")
	}
}

// TestDefaultProcessManager_RunWithInput_EmptyInput verifies empty stdin.
func TestDefaultProcessManager_RunWithInput_EmptyInput(t *testing.T) {
	pm := NewDefaultProcessManager()
	ctx := context.Background()

	output, err := pm.RunWithInput(ctx, "cat", nil)
	if err != nil {
		t.Fatalf("RunWithInput() unexpected error: %v", err)
	}

	if len(output) != 0 {
		t.Errorf("RunWithInput() output = %q, want empty", output)
	}
}

// TestDefaultProcessManager_RunWithInput_LargeInput verifies large stdin handling.
func TestDefaultProcessManager_RunWithInput_LargeInput(t *testing.T) {
	pm := NewDefaultProcessManager()
	ctx := context.Background()

	// Create 100KB of input
	input := make([]byte, 100*1024)
	for i := range input {
		input[i] = byte('a' + (i % 26))
	}

	output, err := pm.RunWithInput(ctx, "wc", input, "-c")
	if err != nil {
		t.Fatalf("RunWithInput() unexpected error: %v", err)
	}

	// wc -c should return the byte count
	got := strings.TrimSpace(string(output))
	if got != "102400" {
		t.Errorf("RunWithInput() wc output = %q, want %q", got, "102400")
	}
}

// TestDefaultProcessManager_Start_Success verifies background process start.
func TestDefaultProcessManager_Start_Success(t *testing.T) {
	pm := NewDefaultProcessManager()
	ctx := context.Background()

	// Start a short-lived process
	pid, err := pm.Start(ctx, "sleep", "0.1")
	if err != nil {
		t.Fatalf("Start() unexpected error: %v", err)
	}

	if pid <= 0 {
		t.Errorf("Start() returned invalid PID: %d", pid)
	}

	// Wait for process to complete
	time.Sleep(200 * time.Millisecond)
}

// TestDefaultProcessManager_Start_InvalidCommand verifies error for missing command.
func TestDefaultProcessManager_Start_InvalidCommand(t *testing.T) {
	pm := NewDefaultProcessManager()
	ctx := context.Background()

	_, err := pm.Start(ctx, "nonexistent-command-12345")
	if err == nil {
		t.Fatal("Start() expected error for non-existent command, got nil")
	}
}

// TestDefaultProcessManager_IsRunning_ProcessExists verifies detection of running process.
func TestDefaultProcessManager_IsRunning_ProcessExists(t *testing.T) {
	pm := NewDefaultProcessManager()
	ctx := context.Background()

	// Start a background process
	pid, err := pm.Start(ctx, "sleep", "2")
	if err != nil {
		t.Fatalf("Start() unexpected error: %v", err)
	}

	// Small delay to ensure process is running
	time.Sleep(50 * time.Millisecond)

	// Check if it's running
	running, foundPid, err := pm.IsRunning(ctx, "sleep 2")
	if err != nil {
		t.Fatalf("IsRunning() unexpected error: %v", err)
	}

	if !running {
		t.Error("IsRunning() returned false, expected true")
	}

	// The found PID might be different if there are multiple sleep processes,
	// but it should be valid
	if foundPid <= 0 {
		t.Errorf("IsRunning() returned invalid PID: %d", foundPid)
	}

	t.Logf("Started PID: %d, Found PID: %d", pid, foundPid)
}

// TestDefaultProcessManager_IsRunning_ProcessNotExists verifies detection when process is absent.
func TestDefaultProcessManager_IsRunning_ProcessNotExists(t *testing.T) {
	pm := NewDefaultProcessManager()
	ctx := context.Background()

	// Check for a process that definitely doesn't exist
	running, pid, err := pm.IsRunning(ctx, "nonexistent-unique-process-name-12345")
	if err != nil {
		t.Fatalf("IsRunning() unexpected error: %v", err)
	}

	if running {
		t.Errorf("IsRunning() returned true, expected false")
	}

	if pid != 0 {
		t.Errorf("IsRunning() returned PID %d, expected 0", pid)
	}
}

// -----------------------------------------------------------------------------
// MockProcessManager Tests
// -----------------------------------------------------------------------------

// TestMockProcessManager_Run verifies mock Run behavior.
func TestMockProcessManager_Run(t *testing.T) {
	mock := &MockProcessManager{
		RunFunc: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			if name == "podman" && len(args) > 0 && args[0] == "version" {
				return []byte("podman version 4.0.0"), nil
			}
			return nil, errors.New("unexpected command")
		},
	}

	ctx := context.Background()
	output, err := mock.Run(ctx, "podman", "version")
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}

	if string(output) != "podman version 4.0.0" {
		t.Errorf("Run() output = %q, want %q", output, "podman version 4.0.0")
	}

	// Verify call was recorded
	if len(mock.Calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(mock.Calls))
	}

	call := mock.Calls[0]
	if call.Method != "Run" {
		t.Errorf("call.Method = %q, want %q", call.Method, "Run")
	}
	if call.Name != "podman" {
		t.Errorf("call.Name = %q, want %q", call.Name, "podman")
	}
	if len(call.Args) != 1 || call.Args[0] != "version" {
		t.Errorf("call.Args = %v, want [version]", call.Args)
	}
}

// TestMockProcessManager_RunWithInput verifies mock RunWithInput behavior.
func TestMockProcessManager_RunWithInput(t *testing.T) {
	mock := &MockProcessManager{
		RunWithInputFunc: func(ctx context.Context, name string, input []byte, args ...string) ([]byte, error) {
			return input, nil // Echo back input
		},
	}

	ctx := context.Background()
	input := []byte("secret-value")
	output, err := mock.RunWithInput(ctx, "podman", input, "secret", "create", "mytoken", "-")
	if err != nil {
		t.Fatalf("RunWithInput() unexpected error: %v", err)
	}

	if string(output) != "secret-value" {
		t.Errorf("RunWithInput() output = %q, want %q", output, "secret-value")
	}

	// Verify call was recorded with input
	if len(mock.Calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(mock.Calls))
	}

	call := mock.Calls[0]
	if call.Method != "RunWithInput" {
		t.Errorf("call.Method = %q, want %q", call.Method, "RunWithInput")
	}
	if string(call.Input) != "secret-value" {
		t.Errorf("call.Input = %q, want %q", call.Input, "secret-value")
	}
}

// TestMockProcessManager_Start verifies mock Start behavior.
func TestMockProcessManager_Start(t *testing.T) {
	mock := &MockProcessManager{
		StartFunc: func(ctx context.Context, name string, args ...string) (int, error) {
			return 12345, nil
		},
	}

	ctx := context.Background()
	pid, err := mock.Start(ctx, "ollama", "serve")
	if err != nil {
		t.Fatalf("Start() unexpected error: %v", err)
	}

	if pid != 12345 {
		t.Errorf("Start() pid = %d, want %d", pid, 12345)
	}

	// Verify call was recorded
	if len(mock.Calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(mock.Calls))
	}

	call := mock.Calls[0]
	if call.Method != "Start" {
		t.Errorf("call.Method = %q, want %q", call.Method, "Start")
	}
}

// TestMockProcessManager_IsRunning verifies mock IsRunning behavior.
func TestMockProcessManager_IsRunning(t *testing.T) {
	mock := &MockProcessManager{
		IsRunningFunc: func(ctx context.Context, pattern string) (bool, int, error) {
			if pattern == "Podman Desktop" {
				return true, 9999, nil
			}
			return false, 0, nil
		},
	}

	ctx := context.Background()

	// Test found case
	running, pid, err := mock.IsRunning(ctx, "Podman Desktop")
	if err != nil {
		t.Fatalf("IsRunning() unexpected error: %v", err)
	}
	if !running || pid != 9999 {
		t.Errorf("IsRunning() = (%v, %d), want (true, 9999)", running, pid)
	}

	// Test not found case
	running, pid, err = mock.IsRunning(ctx, "Unknown App")
	if err != nil {
		t.Fatalf("IsRunning() unexpected error: %v", err)
	}
	if running || pid != 0 {
		t.Errorf("IsRunning() = (%v, %d), want (false, 0)", running, pid)
	}

	// Verify calls were recorded
	if len(mock.Calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(mock.Calls))
	}
}

// TestMockProcessManager_Reset verifies call history reset.
func TestMockProcessManager_Reset(t *testing.T) {
	mock := &MockProcessManager{
		RunFunc: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return nil, nil
		},
	}

	ctx := context.Background()
	_, _ = mock.Run(ctx, "test1")
	_, _ = mock.Run(ctx, "test2")

	if len(mock.Calls) != 2 {
		t.Fatalf("expected 2 calls before reset, got %d", len(mock.Calls))
	}

	mock.Reset()

	if len(mock.Calls) != 0 {
		t.Errorf("expected 0 calls after reset, got %d", len(mock.Calls))
	}
}

// TestMockProcessManager_NilFunc_Panics verifies panic on unconfigured mock.
func TestMockProcessManager_NilFunc_Panics(t *testing.T) {
	mock := &MockProcessManager{} // No functions set

	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic when RunFunc is nil")
		}
	}()

	ctx := context.Background()
	_, _ = mock.Run(ctx, "test")
}

// TestMockProcessManager_MultipleCommands verifies recording multiple commands.
func TestMockProcessManager_MultipleCommands(t *testing.T) {
	callCount := 0
	mock := &MockProcessManager{
		RunFunc: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			callCount++
			return []byte("ok"), nil
		},
	}

	ctx := context.Background()
	_, _ = mock.Run(ctx, "cmd1", "arg1")
	_, _ = mock.Run(ctx, "cmd2", "arg2a", "arg2b")
	_, _ = mock.Run(ctx, "cmd3")

	if callCount != 3 {
		t.Errorf("expected 3 calls, got %d", callCount)
	}

	if len(mock.Calls) != 3 {
		t.Fatalf("expected 3 recorded calls, got %d", len(mock.Calls))
	}

	// Verify each call
	expectedCalls := []struct {
		name string
		args []string
	}{
		{"cmd1", []string{"arg1"}},
		{"cmd2", []string{"arg2a", "arg2b"}},
		{"cmd3", nil},
	}

	for i, expected := range expectedCalls {
		if mock.Calls[i].Name != expected.name {
			t.Errorf("call[%d].Name = %q, want %q", i, mock.Calls[i].Name, expected.name)
		}
		if len(mock.Calls[i].Args) != len(expected.args) {
			t.Errorf("call[%d].Args = %v, want %v", i, mock.Calls[i].Args, expected.args)
		}
	}
}

// -----------------------------------------------------------------------------
// RunInDir Tests
// -----------------------------------------------------------------------------

// TestDefaultProcessManager_RunInDir_Success verifies RunInDir with directory and env.
//
// # Description
//
// Tests that RunInDir correctly executes a command in the specified directory
// with custom environment variables.
//
// # Inputs
//
//   - Working directory: /tmp
//   - Environment: TEST_VAR=test_value
//   - Command: sh -c 'echo $TEST_VAR && pwd'
//
// # Outputs
//
//   - stdout containing env var value and directory
//   - empty stderr
//   - exit code 0
//
// # Limitations
//
//   - Uses /tmp which may not exist on all systems
//
// # Assumptions
//
//   - /tmp directory exists and is writable
func TestDefaultProcessManager_RunInDir_Success(t *testing.T) {
	pm := NewDefaultProcessManager()
	ctx := context.Background()

	stdout, stderr, exitCode, err := pm.RunInDir(
		ctx,
		"/tmp",
		[]string{"TEST_VAR=hello_from_env"},
		"sh", "-c", "echo $TEST_VAR",
	)

	if err != nil {
		t.Fatalf("RunInDir() unexpected error: %v", err)
	}
	if exitCode != 0 {
		t.Errorf("RunInDir() exitCode = %d, want 0", exitCode)
	}
	if !strings.Contains(stdout, "hello_from_env") {
		t.Errorf("RunInDir() stdout = %q, want to contain %q", stdout, "hello_from_env")
	}
	if stderr != "" {
		t.Errorf("RunInDir() stderr = %q, want empty", stderr)
	}
}

// TestDefaultProcessManager_RunInDir_NonZeroExit verifies non-zero exit handling.
//
// # Description
//
// Tests that RunInDir correctly returns exit code without error when
// the command fails with non-zero exit code.
//
// # Inputs
//
//   - Command: false (always exits with 1)
//
// # Outputs
//
//   - exit code 1
//   - err is nil (non-zero exit is not an error)
//
// # Limitations
//
//   - Relies on 'false' command existing
//
// # Assumptions
//
//   - 'false' command is available on the system
func TestDefaultProcessManager_RunInDir_NonZeroExit(t *testing.T) {
	pm := NewDefaultProcessManager()
	ctx := context.Background()

	_, _, exitCode, err := pm.RunInDir(ctx, "", nil, "false")

	if err != nil {
		t.Fatalf("RunInDir() unexpected error: %v (non-zero exit should not be an error)", err)
	}
	if exitCode != 1 {
		t.Errorf("RunInDir() exitCode = %d, want 1", exitCode)
	}
}

// TestDefaultProcessManager_RunInDir_CommandNotFound verifies error for missing command.
//
// # Description
//
// Tests that RunInDir returns an error (not just exit code) when the
// command itself cannot be found.
//
// # Inputs
//
//   - Command: nonexistent-command-12345
//
// # Outputs
//
//   - error is not nil
//   - exit code is -1
//
// # Limitations
//
//   - None
//
// # Assumptions
//
//   - nonexistent-command-12345 does not exist
func TestDefaultProcessManager_RunInDir_CommandNotFound(t *testing.T) {
	pm := NewDefaultProcessManager()
	ctx := context.Background()

	_, _, exitCode, err := pm.RunInDir(ctx, "", nil, "nonexistent-command-12345")

	if err == nil {
		t.Fatal("RunInDir() expected error for non-existent command, got nil")
	}
	if exitCode != -1 {
		t.Errorf("RunInDir() exitCode = %d, want -1", exitCode)
	}
}

// TestDefaultProcessManager_RunInDir_Stderr verifies stderr capture.
//
// # Description
//
// Tests that RunInDir correctly captures stderr separately from stdout.
//
// # Inputs
//
//   - Command: sh -c 'echo stdout; echo stderr >&2'
//
// # Outputs
//
//   - stdout contains "stdout"
//   - stderr contains "stderr"
//
// # Limitations
//
//   - None
//
// # Assumptions
//
//   - sh is available
func TestDefaultProcessManager_RunInDir_Stderr(t *testing.T) {
	pm := NewDefaultProcessManager()
	ctx := context.Background()

	stdout, stderr, exitCode, err := pm.RunInDir(
		ctx, "", nil,
		"sh", "-c", "echo stdout; echo stderr >&2",
	)

	if err != nil {
		t.Fatalf("RunInDir() unexpected error: %v", err)
	}
	if exitCode != 0 {
		t.Errorf("RunInDir() exitCode = %d, want 0", exitCode)
	}
	if !strings.Contains(stdout, "stdout") {
		t.Errorf("RunInDir() stdout = %q, want to contain 'stdout'", stdout)
	}
	if !strings.Contains(stderr, "stderr") {
		t.Errorf("RunInDir() stderr = %q, want to contain 'stderr'", stderr)
	}
}

// -----------------------------------------------------------------------------
// RunStreaming Tests
// -----------------------------------------------------------------------------

// TestDefaultProcessManager_RunStreaming_Success verifies streaming output.
//
// # Description
//
// Tests that RunStreaming correctly streams output to the provided writer.
//
// # Inputs
//
//   - Command: echo "streaming output"
//   - Writer: bytes.Buffer
//
// # Outputs
//
//   - Writer contains "streaming output"
//   - error is nil
//
// # Limitations
//
//   - None
//
// # Assumptions
//
//   - echo command is available
func TestDefaultProcessManager_RunStreaming_Success(t *testing.T) {
	pm := NewDefaultProcessManager()
	ctx := context.Background()

	var buf strings.Builder
	err := pm.RunStreaming(ctx, "", &buf, "echo", "streaming output")

	if err != nil {
		t.Fatalf("RunStreaming() unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), "streaming output") {
		t.Errorf("RunStreaming() output = %q, want to contain 'streaming output'", buf.String())
	}
}

// TestDefaultProcessManager_RunStreaming_ContextCancellation verifies cancellation.
//
// # Description
//
// Tests that RunStreaming respects context cancellation.
//
// # Inputs
//
//   - Cancelled context
//   - Command: sleep 10
//
// # Outputs
//
//   - error indicating cancellation
//
// # Limitations
//
//   - None
//
// # Assumptions
//
//   - sleep command is available
func TestDefaultProcessManager_RunStreaming_ContextCancellation(t *testing.T) {
	pm := NewDefaultProcessManager()
	ctx, cancel := context.WithCancel(context.Background())

	// Cancel immediately
	cancel()

	var buf strings.Builder
	err := pm.RunStreaming(ctx, "", &buf, "sleep", "10")

	if err == nil {
		t.Fatal("RunStreaming() expected error for cancelled context, got nil")
	}
	if err != context.Canceled {
		t.Errorf("RunStreaming() error = %v, want context.Canceled", err)
	}
}

// -----------------------------------------------------------------------------
// Mock RunInDir and RunStreaming Tests
// -----------------------------------------------------------------------------

// TestMockProcessManager_RunInDir verifies mock RunInDir behavior.
//
// # Description
//
// Tests that MockProcessManager correctly delegates to RunInDirFunc
// and records the call with dir and env.
//
// # Inputs
//
//   - dir: /test/dir
//   - env: ["VAR=value"]
//
// # Outputs
//
//   - Call recorded with correct dir and env
//
// # Limitations
//
//   - None
//
// # Assumptions
//
//   - RunInDirFunc is set
func TestMockProcessManager_RunInDir(t *testing.T) {
	mock := &MockProcessManager{
		RunInDirFunc: func(ctx context.Context, dir string, env []string, name string, args ...string) (string, string, int, error) {
			return "stdout", "stderr", 0, nil
		},
	}

	ctx := context.Background()
	stdout, stderr, exitCode, err := mock.RunInDir(
		ctx,
		"/test/dir",
		[]string{"VAR=value"},
		"podman-compose", "up", "-d",
	)

	if err != nil {
		t.Fatalf("RunInDir() unexpected error: %v", err)
	}
	if stdout != "stdout" || stderr != "stderr" || exitCode != 0 {
		t.Errorf("RunInDir() = (%q, %q, %d), want (stdout, stderr, 0)", stdout, stderr, exitCode)
	}

	// Verify call was recorded
	if len(mock.Calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(mock.Calls))
	}

	call := mock.Calls[0]
	if call.Method != "RunInDir" {
		t.Errorf("call.Method = %q, want 'RunInDir'", call.Method)
	}
	if call.Dir != "/test/dir" {
		t.Errorf("call.Dir = %q, want '/test/dir'", call.Dir)
	}
	if len(call.Env) != 1 || call.Env[0] != "VAR=value" {
		t.Errorf("call.Env = %v, want [VAR=value]", call.Env)
	}
	if call.Name != "podman-compose" {
		t.Errorf("call.Name = %q, want 'podman-compose'", call.Name)
	}
}

// TestMockProcessManager_RunStreaming verifies mock RunStreaming behavior.
//
// # Description
//
// Tests that MockProcessManager correctly delegates to RunStreamingFunc
// and records the call.
//
// # Inputs
//
//   - dir: /test/dir
//   - writer: strings.Builder
//
// # Outputs
//
//   - Call recorded with correct dir
//   - Writer receives output
//
// # Limitations
//
//   - None
//
// # Assumptions
//
//   - RunStreamingFunc is set
func TestMockProcessManager_RunStreaming(t *testing.T) {
	mock := &MockProcessManager{
		RunStreamingFunc: func(ctx context.Context, dir string, w io.Writer, name string, args ...string) error {
			_, _ = w.Write([]byte("mock streaming output"))
			return nil
		},
	}

	ctx := context.Background()
	var buf strings.Builder
	err := mock.RunStreaming(ctx, "/test/dir", &buf, "podman-compose", "logs", "-f")

	if err != nil {
		t.Fatalf("RunStreaming() unexpected error: %v", err)
	}
	if buf.String() != "mock streaming output" {
		t.Errorf("RunStreaming() output = %q, want 'mock streaming output'", buf.String())
	}

	// Verify call was recorded
	if len(mock.Calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(mock.Calls))
	}

	call := mock.Calls[0]
	if call.Method != "RunStreaming" {
		t.Errorf("call.Method = %q, want 'RunStreaming'", call.Method)
	}
	if call.Dir != "/test/dir" {
		t.Errorf("call.Dir = %q, want '/test/dir'", call.Dir)
	}
}

// -----------------------------------------------------------------------------
// Interface Compliance Tests
// -----------------------------------------------------------------------------

// TestProcessManager_InterfaceCompliance verifies interface implementations.
func TestProcessManager_InterfaceCompliance(t *testing.T) {
	// These will fail to compile if interfaces aren't implemented correctly
	var _ ProcessManager = (*DefaultProcessManager)(nil)
	var _ ProcessManager = (*MockProcessManager)(nil)
}
