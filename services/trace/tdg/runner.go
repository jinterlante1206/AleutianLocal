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

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"strings"
	"time"
)

// =============================================================================
// TEST RUNNER
// =============================================================================

// TestRunner executes tests and captures results.
//
// Thread Safety: Safe for concurrent use. Each execution creates its own process.
type TestRunner struct {
	configs      *LanguageConfigRegistry
	timeout      time.Duration
	suiteTimeout time.Duration
	maxOutput    int
	workingDir   string
	logger       *slog.Logger
}

// NewTestRunner creates a new test runner.
//
// Inputs:
//
//	cfg - TDG configuration
//	logger - Logger for structured logging
//
// Outputs:
//
//	*TestRunner - Configured test runner
func NewTestRunner(cfg *Config, logger *slog.Logger) *TestRunner {
	if logger == nil {
		logger = slog.Default()
	}
	return &TestRunner{
		configs:      DefaultLanguageConfigs,
		timeout:      cfg.TestTimeout,
		suiteTimeout: cfg.SuiteTimeout,
		maxOutput:    cfg.MaxOutputBytes,
		workingDir:   cfg.WorkingDir,
		logger:       logger,
	}
}

// RunTest executes a single test by name.
//
// Description:
//
//	Executes the specified test using the appropriate test runner for
//	the language. Captures stdout/stderr and parses the result.
//
// Inputs:
//
//	ctx - Context for cancellation and timeout
//	tc - The test case to execute
//
// Outputs:
//
//	*TestResult - Test execution result
//	error - Non-nil on execution failure
//
// Thread Safety: Safe for concurrent use.
func (r *TestRunner) RunTest(ctx context.Context, tc *TestCase) (*TestResult, error) {
	if ctx == nil {
		return nil, ErrNilContext
	}
	if err := tc.Validate(); err != nil {
		return nil, err
	}

	langCfg, ok := r.configs.Get(tc.Language)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedLanguage, tc.Language)
	}

	start := time.Now()
	r.logger.Debug("Running single test",
		slog.String("test_name", tc.Name),
		slog.String("file", tc.FilePath),
		slog.String("language", tc.Language),
	)

	// Build command with substitutions
	args := r.substituteArgs(langCfg.TestArgs, tc.Name, tc.FilePath, tc.PackagePath)
	result, err := r.execute(ctx, langCfg.TestCommand, args, r.timeout)

	duration := time.Since(start)
	result.Duration = duration

	// Parse output to extract pass/fail
	parser := GetTestOutputParser(tc.Language)
	if parser != nil {
		passed, failedTests := parser([]byte(result.Output))
		result.Passed = passed
		result.FailedTests = failedTests
	} else {
		// Fallback: check exit code
		result.Passed = result.ExitCode == 0
	}

	r.logger.Info("Test completed",
		slog.String("test_name", tc.Name),
		slog.Bool("passed", result.Passed),
		slog.Duration("duration", duration),
		slog.Int("exit_code", result.ExitCode),
		slog.Int("output_bytes", len(result.Output)),
	)

	return result, err
}

// RunSuite executes all tests in a package/directory.
//
// Description:
//
//	Runs the full test suite for a package using the appropriate test
//	runner. Used for regression checking after a fix is applied.
//
// Inputs:
//
//	ctx - Context for cancellation and timeout
//	language - The programming language
//	packagePath - The package or directory path
//
// Outputs:
//
//	*TestResult - Suite execution result with all failures
//	error - Non-nil on execution failure
//
// Thread Safety: Safe for concurrent use.
func (r *TestRunner) RunSuite(ctx context.Context, language, packagePath string) (*TestResult, error) {
	if ctx == nil {
		return nil, ErrNilContext
	}

	langCfg, ok := r.configs.Get(language)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedLanguage, language)
	}

	start := time.Now()
	r.logger.Debug("Running test suite",
		slog.String("language", language),
		slog.String("package", packagePath),
	)

	// Build command with substitutions
	args := r.substituteArgs(langCfg.SuiteArgs, "", "", packagePath)

	// Use configured suite timeout
	result, err := r.execute(ctx, langCfg.TestCommand, args, r.suiteTimeout)

	duration := time.Since(start)
	result.Duration = duration

	// Parse output to extract pass/fail and failed tests
	parser := GetTestOutputParser(language)
	if parser != nil {
		passed, failedTests := parser([]byte(result.Output))
		result.Passed = passed
		result.FailedTests = failedTests
	} else {
		result.Passed = result.ExitCode == 0
	}

	r.logger.Info("Suite completed",
		slog.String("language", language),
		slog.String("package", packagePath),
		slog.Bool("passed", result.Passed),
		slog.Duration("duration", duration),
		slog.Int("failed_count", len(result.FailedTests)),
		slog.Int("exit_code", result.ExitCode),
	)

	return result, err
}

// execute runs a command with timeout and output capture.
func (r *TestRunner) execute(ctx context.Context, command string, args []string, timeout time.Duration) (*TestResult, error) {
	// Apply timeout
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, command, args...)

	// Set working directory
	if r.workingDir != "" {
		cmd.Dir = r.workingDir
	}

	// Capture output with size limit
	var stdout, stderr bytes.Buffer
	stdoutLimited := &limitedWriter{w: &stdout, limit: r.maxOutput}
	stderrLimited := &limitedWriter{w: &stderr, limit: r.maxOutput}

	cmd.Stdout = stdoutLimited
	cmd.Stderr = stderrLimited

	r.logger.Debug("Executing command",
		slog.String("command", command),
		slog.Any("args", args),
		slog.Duration("timeout", timeout),
	)

	err := cmd.Run()

	result := &TestResult{
		Output:    stdout.String() + stderr.String(),
		Truncated: stdoutLimited.truncated || stderrLimited.truncated,
	}

	// Handle context cancellation (timeout)
	if ctx.Err() == context.DeadlineExceeded {
		result.TimedOut = true
		result.ExitCode = -1
		r.logger.Warn("Test execution timed out",
			slog.Duration("timeout", timeout),
		)
		return result, ErrTestTimeout
	}

	// Extract exit code
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else {
			result.ExitCode = -1
			return result, fmt.Errorf("command execution failed: %w", err)
		}
	} else {
		result.ExitCode = 0
	}

	return result, nil
}

// substituteArgs replaces placeholders in command arguments.
func (r *TestRunner) substituteArgs(args []string, testName, filePath, packagePath string) []string {
	result := make([]string, len(args))
	for i, arg := range args {
		s := arg
		s = strings.ReplaceAll(s, "{name}", testName)
		s = strings.ReplaceAll(s, "{file}", filePath)
		s = strings.ReplaceAll(s, "{package}", packagePath)
		result[i] = s
	}
	return result
}

// SetWorkingDir sets the working directory for test execution.
func (r *TestRunner) SetWorkingDir(dir string) {
	r.workingDir = dir
}

// =============================================================================
// LIMITED WRITER
// =============================================================================

// limitedWriter wraps a writer with a size limit.
type limitedWriter struct {
	w         io.Writer
	limit     int
	written   int
	truncated bool
}

func (lw *limitedWriter) Write(p []byte) (n int, err error) {
	if lw.written >= lw.limit {
		lw.truncated = true
		return len(p), nil // Silently discard
	}

	remaining := lw.limit - lw.written
	if len(p) > remaining {
		p = p[:remaining]
		lw.truncated = true
	}

	n, err = lw.w.Write(p)
	lw.written += n
	return len(p), err // Return original length to avoid breaking callers
}
