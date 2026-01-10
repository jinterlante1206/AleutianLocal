// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.

package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// =============================================================================
// CLI TEST HARNESS
// =============================================================================

// CLITestHarness provides utilities for testing CLI commands end-to-end.
//
// # Description
//
// The harness builds the CLI binary once, then executes it with various
// arguments, capturing stdout, stderr, and exit codes for assertions.
//
// # Usage
//
//	harness := NewCLITestHarness(t)
//	defer harness.Cleanup()
//
//	result := harness.Run("--version")
//	if result.ExitCode != 0 {
//	    t.Errorf("Expected exit code 0, got %d", result.ExitCode)
//	}
//
// # Thread Safety
//
// The harness is safe for concurrent use after Build() is called.
type CLITestHarness struct {
	// BinaryPath is the path to the compiled CLI binary
	BinaryPath string

	// WorkDir is the working directory for test execution
	WorkDir string

	// Env is additional environment variables to set
	Env map[string]string

	// Timeout is the default timeout for command execution
	Timeout time.Duration

	// built tracks whether the binary has been built
	built bool
	mu    sync.Mutex
}

// CLIResult holds the result of executing a CLI command.
type CLIResult struct {
	// Stdout is the standard output
	Stdout string

	// Stderr is the standard error
	Stderr string

	// ExitCode is the process exit code
	ExitCode int

	// Duration is how long the command took
	Duration time.Duration

	// Command is the full command that was run (for debugging)
	Command string

	// TimedOut indicates if the command was killed due to timeout
	TimedOut bool
}

// NewCLITestHarness creates a new test harness.
//
// # Inputs
//
//   - workDir: Working directory for tests (uses temp dir if empty)
//
// # Outputs
//
//   - *CLITestHarness: Configured harness ready for Build()
func NewCLITestHarness(workDir string) *CLITestHarness {
	if workDir == "" {
		workDir, _ = os.MkdirTemp("", "aleutian-cli-test-*")
	}

	return &CLITestHarness{
		WorkDir: workDir,
		Env:     make(map[string]string),
		Timeout: 30 * time.Second,
	}
}

// Build compiles the CLI binary for testing.
//
// # Description
//
// Builds the aleutian binary in a temporary location. This is called
// automatically on first Run() if not called explicitly.
//
// # Outputs
//
//   - error: If compilation fails
func (h *CLITestHarness) Build() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.built {
		return nil
	}

	// Find the source directory
	sourceDir, err := findSourceDir()
	if err != nil {
		return fmt.Errorf("failed to find source directory: %w", err)
	}

	// Create temp binary path
	h.BinaryPath = filepath.Join(h.WorkDir, "aleutian-test")

	// Build the binary
	cmd := exec.Command("go", "build", "-o", h.BinaryPath, "./cmd/aleutian")
	cmd.Dir = sourceDir
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to build CLI: %w\nOutput: %s", err, string(output))
	}

	h.built = true
	return nil
}

// Run executes the CLI with the given arguments.
//
// # Description
//
// Runs the CLI binary with the specified arguments, capturing all output.
// Automatically builds the binary if not already built.
//
// # Inputs
//
//   - args: Command-line arguments (e.g., "stack", "start", "--profile", "low")
//
// # Outputs
//
//   - *CLIResult: Captured output and exit code
//   - error: If execution setup fails (not if command fails)
//
// # Examples
//
//	result, err := harness.Run("--version")
//	result, err := harness.Run("stack", "start", "--skip-model-check")
//	result, err := harness.Run("policy", "test", "123-45-6789")
func (h *CLITestHarness) Run(args ...string) (*CLIResult, error) {
	return h.RunWithOptions(CLIRunOptions{Args: args})
}

// CLIRunOptions provides additional options for command execution.
type CLIRunOptions struct {
	// Args are the command-line arguments
	Args []string

	// Stdin is input to pipe to the command
	Stdin string

	// Timeout overrides the default timeout
	Timeout time.Duration

	// WorkDir overrides the working directory
	WorkDir string

	// Env adds/overrides environment variables
	Env map[string]string
}

// RunWithOptions executes the CLI with full options.
func (h *CLITestHarness) RunWithOptions(opts CLIRunOptions) (*CLIResult, error) {
	// Ensure binary is built
	if err := h.Build(); err != nil {
		return nil, err
	}

	// Set up timeout
	timeout := h.Timeout
	if opts.Timeout > 0 {
		timeout = opts.Timeout
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Create command
	cmd := exec.CommandContext(ctx, h.BinaryPath, opts.Args...)

	// Set working directory
	workDir := h.WorkDir
	if opts.WorkDir != "" {
		workDir = opts.WorkDir
	}
	cmd.Dir = workDir

	// Set environment
	cmd.Env = os.Environ()
	for k, v := range h.Env {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}
	for k, v := range opts.Env {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}

	// Capture output
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Handle stdin
	if opts.Stdin != "" {
		cmd.Stdin = strings.NewReader(opts.Stdin)
	}

	// Execute
	start := time.Now()
	err := cmd.Run()
	duration := time.Since(start)

	// Build result
	result := &CLIResult{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		Duration: duration,
		Command:  fmt.Sprintf("%s %s", h.BinaryPath, strings.Join(opts.Args, " ")),
		TimedOut: ctx.Err() == context.DeadlineExceeded,
	}

	// Get exit code
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else {
			return result, err
		}
	}

	return result, nil
}

// RunInteractive runs a command that expects interactive input.
//
// # Description
//
// Sends lines of input to stdin, waiting for each line to be processed.
// Useful for commands like `aleutian weaviate wipeout --force` that require
// confirmation.
//
// # Inputs
//
//   - args: Command-line arguments
//   - inputs: Lines to send to stdin (sent sequentially)
//
// # Examples
//
//	// Answer "yes" to confirmation prompt
//	result, err := harness.RunInteractive(
//	    []string{"weaviate", "wipeout", "--force"},
//	    []string{"yes"},
//	)
func (h *CLITestHarness) RunInteractive(args []string, inputs []string) (*CLIResult, error) {
	return h.RunWithOptions(CLIRunOptions{
		Args:  args,
		Stdin: strings.Join(inputs, "\n") + "\n",
	})
}

// Cleanup removes temporary files created by the harness.
func (h *CLITestHarness) Cleanup() {
	if h.WorkDir != "" && strings.Contains(h.WorkDir, "aleutian-cli-test") {
		os.RemoveAll(h.WorkDir)
	}
}

// =============================================================================
// ASSERTION HELPERS
// =============================================================================

// AssertExitCode checks the exit code matches expected.
func (r *CLIResult) AssertExitCode(expected int) error {
	if r.ExitCode != expected {
		return fmt.Errorf("exit code: got %d, want %d\nstdout: %s\nstderr: %s",
			r.ExitCode, expected, r.Stdout, r.Stderr)
	}
	return nil
}

// AssertSuccess checks exit code is 0.
func (r *CLIResult) AssertSuccess() error {
	return r.AssertExitCode(0)
}

// AssertFailure checks exit code is non-zero.
func (r *CLIResult) AssertFailure() error {
	if r.ExitCode == 0 {
		return fmt.Errorf("expected failure but got exit code 0\nstdout: %s", r.Stdout)
	}
	return nil
}

// AssertStdoutContains checks stdout contains the substring.
func (r *CLIResult) AssertStdoutContains(substr string) error {
	if !strings.Contains(r.Stdout, substr) {
		return fmt.Errorf("stdout does not contain %q\nstdout: %s", substr, r.Stdout)
	}
	return nil
}

// AssertStderrContains checks stderr contains the substring.
func (r *CLIResult) AssertStderrContains(substr string) error {
	if !strings.Contains(r.Stderr, substr) {
		return fmt.Errorf("stderr does not contain %q\nstderr: %s", substr, r.Stderr)
	}
	return nil
}

// AssertOutputContains checks either stdout or stderr contains the substring.
func (r *CLIResult) AssertOutputContains(substr string) error {
	if !strings.Contains(r.Stdout, substr) && !strings.Contains(r.Stderr, substr) {
		return fmt.Errorf("output does not contain %q\nstdout: %s\nstderr: %s",
			substr, r.Stdout, r.Stderr)
	}
	return nil
}

// AssertStdoutNotContains checks stdout does not contain the substring.
func (r *CLIResult) AssertStdoutNotContains(substr string) error {
	if strings.Contains(r.Stdout, substr) {
		return fmt.Errorf("stdout should not contain %q\nstdout: %s", substr, r.Stdout)
	}
	return nil
}

// AssertStdoutMatches checks stdout matches the pattern.
func (r *CLIResult) AssertStdoutMatches(pattern string) error {
	// Simple glob-style matching (* = any)
	if !globMatch(pattern, r.Stdout) {
		return fmt.Errorf("stdout does not match pattern %q\nstdout: %s", pattern, r.Stdout)
	}
	return nil
}

// AssertNoTimeout checks the command did not timeout.
func (r *CLIResult) AssertNoTimeout() error {
	if r.TimedOut {
		return fmt.Errorf("command timed out after %v\nstdout: %s\nstderr: %s",
			r.Duration, r.Stdout, r.Stderr)
	}
	return nil
}

// =============================================================================
// HELPERS
// =============================================================================

// findSourceDir finds the AleutianLocal source directory.
func findSourceDir() (string, error) {
	// Try current directory
	if _, err := os.Stat("cmd/aleutian/main.go"); err == nil {
		return ".", nil
	}

	// Try going up directories
	dir, _ := os.Getwd()
	for i := 0; i < 5; i++ {
		if _, err := os.Stat(filepath.Join(dir, "cmd/aleutian/main.go")); err == nil {
			return dir, nil
		}
		dir = filepath.Dir(dir)
	}

	return "", fmt.Errorf("could not find AleutianLocal source directory")
}

// globMatch performs simple glob matching (* = any characters).
func globMatch(pattern, s string) bool {
	parts := strings.Split(pattern, "*")
	if len(parts) == 1 {
		return pattern == s
	}

	pos := 0
	for i, part := range parts {
		if part == "" {
			continue
		}
		idx := strings.Index(s[pos:], part)
		if idx == -1 {
			return false
		}
		if i == 0 && idx != 0 {
			return false // Must match at start
		}
		pos += idx + len(part)
	}
	// If pattern doesn't end with *, must match to end
	if !strings.HasSuffix(pattern, "*") && pos != len(s) {
		return false
	}
	return true
}

// =============================================================================
// TEST FIXTURES
// =============================================================================

// TestFixtures provides common test data.
type TestFixtures struct {
	// TempDir is a temporary directory for test files
	TempDir string

	// Files maps filename to content
	Files map[string]string
}

// NewTestFixtures creates fixtures and temp files.
func NewTestFixtures() (*TestFixtures, error) {
	tempDir, err := os.MkdirTemp("", "aleutian-fixtures-*")
	if err != nil {
		return nil, err
	}

	f := &TestFixtures{
		TempDir: tempDir,
		Files:   make(map[string]string),
	}

	// Create common test files
	testFiles := map[string]string{
		"test_doc.md": `# Test Document

This is a test document for ingestion testing.

## Section 1
Some content here.

## Section 2
More content here.
`,
		"secret_doc.txt": `Database credentials:
AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE
AWS_SECRET_ACCESS_KEY=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY

SSN: 123-45-6789
Credit Card: 4111-1111-1111-1111
`,
		"clean_doc.txt": `This is a clean document.
No secrets or PII here.
Just regular text content.
`,
	}

	for name, content := range testFiles {
		path := filepath.Join(tempDir, name)
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			return nil, err
		}
		f.Files[name] = path
	}

	return f, nil
}

// Cleanup removes all fixture files.
func (f *TestFixtures) Cleanup() {
	if f.TempDir != "" {
		os.RemoveAll(f.TempDir)
	}
}

// =============================================================================
// MOCK ORCHESTRATOR SERVER
// =============================================================================

// MockOrchestrator provides a fake orchestrator for offline testing.
type MockOrchestrator struct {
	server io.Closer
	URL    string
}

// NewMockOrchestrator starts a mock orchestrator server.
//
// # Description
//
// Provides fake endpoints for testing CLI commands without requiring
// the full stack to be running. Useful for unit tests.
//
// # Endpoints
//
//   - GET /health: Returns 200 OK
//   - GET /v1/sessions: Returns empty session list
//   - POST /v1/sessions/:id/verify: Returns verified=true
//   - GET /v1/weaviate/summary: Returns mock summary
func NewMockOrchestrator() (*MockOrchestrator, error) {
	// Implementation would use httptest.Server
	// Placeholder for now
	return &MockOrchestrator{
		URL: "http://localhost:12125",
	}, nil
}

// Close shuts down the mock server.
func (m *MockOrchestrator) Close() error {
	if m.server != nil {
		return m.server.Close()
	}
	return nil
}
