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
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
)

// =============================================================================
// CONTROLLER
// =============================================================================

// Controller orchestrates the TDG loop.
//
// Thread Safety: NOT safe for concurrent use. Each TDG session should
// have its own Controller instance.
type Controller struct {
	config    *Config
	runner    *TestRunner
	files     *FileManager
	generator *TestGenerator
	ctx       *Context
	logger    *slog.Logger
	running   bool
}

// NewController creates a new TDG controller.
//
// Inputs:
//
//	cfg - TDG configuration
//	runner - Test runner for executing tests
//	files - File manager for test/patch files
//	gen - Test generator for LLM interactions
//	logger - Logger for structured logging
//
// Outputs:
//
//	*Controller - Configured controller
func NewController(cfg *Config, runner *TestRunner, files *FileManager, gen *TestGenerator, logger *slog.Logger) *Controller {
	if cfg == nil {
		cfg = DefaultConfig()
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Controller{
		config:    cfg,
		runner:    runner,
		files:     files,
		generator: gen,
		logger:    logger,
	}
}

// Run executes the full TDG loop.
//
// Description:
//
//	Orchestrates the TDG state machine:
//	  1. Generate reproducer test
//	  2. Verify test fails
//	  3. Generate fix
//	  4. Verify test passes
//	  5. Check for regressions
//
//	Handles retries, timeouts, and rollback on failure.
//
// Inputs:
//
//	ctx - Context for cancellation
//	req - The TDG request
//
// Outputs:
//
//	*Result - TDG execution result
//	error - Non-nil on unrecoverable failure
func (c *Controller) Run(ctx context.Context, req *Request) (*Result, error) {
	if ctx == nil {
		return nil, ErrNilContext
	}
	if err := req.Validate(); err != nil {
		return nil, err
	}
	if c.running {
		return nil, ErrAlreadyRunning
	}
	if c.runner == nil {
		return nil, errors.New("runner must not be nil")
	}

	c.running = true
	defer func() { c.running = false }()

	// Initialize context
	sessionID := uuid.New().String()[:8]
	c.ctx = NewContext(sessionID, req)

	// Start tracing span for the session
	ctx, span := startSessionSpan(ctx, sessionID, req.Language)
	defer span.End()

	c.logger.Info("Starting TDG session",
		slog.String("session_id", sessionID),
		slog.String("language", req.Language),
		slog.Int("bug_desc_length", len(req.BugDescription)),
	)

	// Apply total timeout
	ctx, cancel := context.WithTimeout(ctx, c.config.TotalTimeout)
	defer cancel()

	// Set working directory for runner
	c.runner.SetWorkingDir(req.ProjectRoot)

	// Run the state machine
	var err error
	for !c.ctx.State.IsTerminal() {
		select {
		case <-ctx.Done():
			c.ctx.State = StateFailed
			c.ctx.LastError = ErrTotalTimeout
			c.logger.Warn("TDG session timed out",
				slog.String("session_id", sessionID),
				slog.Duration("elapsed", c.ctx.Elapsed()),
			)
			// Rollback on timeout
			if rollbackErr := c.files.Rollback(); rollbackErr != nil {
				c.logger.Error("Rollback failed", slog.String("error", rollbackErr.Error()))
			}
			// Record timeout metrics
			setSessionSpanResult(span, false, string(StateFailed), c.ctx.TestRetries, c.ctx.FixRetries)
			recordSessionMetrics(ctx, req.Language, c.ctx.Elapsed(), false,
				c.ctx.TestRetries, c.ctx.FixRetries, c.ctx.Metrics.LLMCalls)
			return c.buildResult(), ErrTotalTimeout
		default:
			if err = c.step(ctx); err != nil {
				c.logger.Error("Step failed",
					slog.String("state", string(c.ctx.State)),
					slog.String("error", err.Error()),
				)
				// Don't return immediately, let state machine handle it
			}
		}
	}

	result := c.buildResult()

	// Record session metrics
	setSessionSpanResult(span, result.Success, string(c.ctx.State), c.ctx.TestRetries, c.ctx.FixRetries)
	recordSessionMetrics(ctx, req.Language, result.Duration, result.Success,
		c.ctx.TestRetries, c.ctx.FixRetries, c.ctx.Metrics.LLMCalls)

	c.logger.Info("TDG session complete",
		slog.String("session_id", sessionID),
		slog.String("final_state", string(c.ctx.State)),
		slog.Bool("success", result.Success),
		slog.Duration("duration", result.Duration),
	)

	return result, nil
}

// step executes one step of the TDG state machine.
func (c *Controller) step(ctx context.Context) error {
	from := c.ctx.State
	var err error

	switch c.ctx.State {
	case StateIdle:
		c.transition(StateUnderstand)

	case StateUnderstand:
		// For now, understanding is implicit in test generation
		c.transition(StateWriteTest)

	case StateWriteTest:
		err = c.stepWriteTest(ctx)

	case StateVerifyFail:
		err = c.stepVerifyFail(ctx)

	case StateWriteFix:
		err = c.stepWriteFix(ctx)

	case StateVerifyPass:
		err = c.stepVerifyPass(ctx)

	case StateRegression:
		err = c.stepRegression(ctx)

	default:
		err = &StateTransitionError{From: from, To: c.ctx.State}
	}

	return err
}

// transition changes state with logging.
func (c *Controller) transition(to State) {
	from := c.ctx.State
	c.ctx.State = to

	// Record state transition metric
	recordStateTransition(context.Background(), string(from), string(to))

	c.logger.Info("TDG state transition",
		slog.String("session_id", c.ctx.SessionID),
		slog.String("from", string(from)),
		slog.String("to", string(to)),
		slog.Int("test_attempts", c.ctx.TestRetries),
		slog.Int("fix_attempts", c.ctx.FixRetries),
		slog.Duration("elapsed", c.ctx.Elapsed()),
	)
}

// =============================================================================
// STATE HANDLERS
// =============================================================================

func (c *Controller) stepWriteTest(ctx context.Context) error {
	c.ctx.TestRetries++
	c.ctx.Metrics.TestAttempts++

	c.logger.Debug("Generating test",
		slog.Int("attempt", c.ctx.TestRetries),
		slog.Int("max", c.config.MaxTestRetries),
	)

	var tc *TestCase
	var err error

	if c.ctx.ReproducerTest == nil {
		// First attempt
		tc, err = c.generator.GenerateReproducerTest(ctx, c.ctx.Request)
	} else {
		// Refinement attempt
		tc, err = c.generator.RefineTest(ctx, c.ctx.Request, c.ctx.ReproducerTest, c.ctx.LastTestOutput)
	}

	if err != nil {
		if c.ctx.TestRetries >= c.config.MaxTestRetries {
			c.ctx.LastError = ErrMaxTestRetries
			c.transition(StateFailed)
			return ErrMaxTestRetries
		}
		return err
	}

	c.ctx.ReproducerTest = tc
	c.ctx.Metrics.LLMCalls = c.generator.CallCount()

	// Write test to disk
	if err := c.files.WriteTest(tc); err != nil {
		c.ctx.LastError = err
		c.transition(StateFailed)
		return err
	}

	c.transition(StateVerifyFail)
	return nil
}

func (c *Controller) stepVerifyFail(ctx context.Context) error {
	c.logger.Debug("Verifying test fails",
		slog.String("test_name", c.ctx.ReproducerTest.Name),
	)

	c.ctx.Metrics.TestsRun++
	result, err := c.runner.RunTest(ctx, c.ctx.ReproducerTest)
	if err != nil && err != ErrTestTimeout {
		// Execution error (not test failure)
		c.ctx.LastError = err
		c.transition(StateFailed)
		return err
	}

	c.ctx.Metrics.TotalTestDuration += result.Duration
	c.ctx.LastTestOutput = result.Output

	if result.Passed {
		// Test passed when it should fail - bad reproducer
		c.logger.Warn("Test passed unexpectedly",
			slog.String("test_name", c.ctx.ReproducerTest.Name),
			slog.Int("attempt", c.ctx.TestRetries),
		)

		if c.ctx.TestRetries >= c.config.MaxTestRetries {
			c.ctx.LastError = ErrMaxTestRetries
			c.transition(StateFailed)
			_ = c.files.Rollback()
			return ErrMaxTestRetries
		}

		// Go back to write a better test
		c.transition(StateWriteTest)
		return nil
	}

	// Test failed as expected - good reproducer!
	c.logger.Info("Test correctly fails (reproduces bug)",
		slog.String("test_name", c.ctx.ReproducerTest.Name),
	)

	c.transition(StateWriteFix)
	return nil
}

func (c *Controller) stepWriteFix(ctx context.Context) error {
	c.ctx.FixRetries++
	c.ctx.Metrics.FixAttempts++

	c.logger.Debug("Generating fix",
		slog.Int("attempt", c.ctx.FixRetries),
		slog.Int("max", c.config.MaxFixRetries),
	)

	var patch *Patch
	var err error

	if len(c.ctx.ProposedPatches) == 0 {
		// First attempt
		patch, err = c.generator.GenerateFix(ctx, c.ctx.Request, c.ctx.ReproducerTest, c.ctx.LastTestOutput)
	} else {
		// Refinement attempt
		lastPatch := c.ctx.ProposedPatches[len(c.ctx.ProposedPatches)-1]
		patch, err = c.generator.RefineFix(ctx, c.ctx.Request, c.ctx.ReproducerTest, lastPatch, c.ctx.LastTestOutput)
	}

	if err != nil {
		if c.ctx.FixRetries >= c.config.MaxFixRetries {
			c.ctx.LastError = ErrMaxFixRetries
			c.transition(StateFailed)
			_ = c.files.Rollback()
			return ErrMaxFixRetries
		}
		return err
	}

	c.ctx.ProposedPatches = append(c.ctx.ProposedPatches, patch)
	c.ctx.Metrics.LLMCalls = c.generator.CallCount()

	// Apply the patch
	if err := c.files.ApplyPatch(patch); err != nil {
		c.ctx.LastError = err
		c.transition(StateFailed)
		_ = c.files.Rollback()
		return err
	}

	c.ctx.AppliedPatches = append(c.ctx.AppliedPatches, patch)
	c.transition(StateVerifyPass)
	return nil
}

func (c *Controller) stepVerifyPass(ctx context.Context) error {
	c.logger.Debug("Verifying test passes",
		slog.String("test_name", c.ctx.ReproducerTest.Name),
	)

	c.ctx.Metrics.TestsRun++
	result, err := c.runner.RunTest(ctx, c.ctx.ReproducerTest)
	if err != nil && err != ErrTestTimeout {
		c.ctx.LastError = err
		c.transition(StateFailed)
		_ = c.files.Rollback()
		return err
	}

	c.ctx.Metrics.TotalTestDuration += result.Duration
	c.ctx.LastTestOutput = result.Output

	if !result.Passed {
		// Test still fails - fix didn't work
		c.logger.Warn("Test still fails after fix",
			slog.String("test_name", c.ctx.ReproducerTest.Name),
			slog.Int("attempt", c.ctx.FixRetries),
		)

		if c.ctx.FixRetries >= c.config.MaxFixRetries {
			c.ctx.LastError = ErrMaxFixRetries
			c.transition(StateFailed)
			_ = c.files.Rollback()
			return ErrMaxFixRetries
		}

		// Rollback and try again
		if err := c.files.Rollback(); err != nil {
			c.logger.Error("Rollback failed", slog.String("error", err.Error()))
		}
		c.ctx.AppliedPatches = nil

		c.transition(StateWriteFix)
		return nil
	}

	// Test passes - fix works!
	c.logger.Info("Test passes with fix",
		slog.String("test_name", c.ctx.ReproducerTest.Name),
	)

	c.transition(StateRegression)
	return nil
}

func (c *Controller) stepRegression(ctx context.Context) error {
	c.logger.Debug("Running regression check",
		slog.String("package", c.ctx.Request.ProjectRoot),
	)

	c.ctx.Metrics.TestsRun++
	result, err := c.runner.RunSuite(ctx, c.ctx.Request.Language, ".")
	if err != nil && err != ErrTestTimeout && err != ErrSuiteTimeout {
		c.ctx.LastError = err
		c.transition(StateFailed)
		_ = c.files.Rollback()
		return err
	}

	c.ctx.Metrics.TotalTestDuration += result.Duration
	c.ctx.LastTestOutput = result.Output

	if !result.Passed {
		// Some tests failed - regression!
		c.logger.Warn("Regression detected",
			slog.Int("failed_count", len(result.FailedTests)),
			slog.Any("failed_tests", result.FailedTests),
		)

		c.ctx.RegressionFixes++
		c.ctx.Metrics.RegressionFixes++

		if c.ctx.RegressionFixes >= c.config.MaxRegressionFixes {
			c.ctx.LastError = ErrMaxRegressionFixes
			c.transition(StateFailed)
			_ = c.files.Rollback()
			return ErrMaxRegressionFixes
		}

		// Rollback and try to fix again (with regression info)
		if err := c.files.Rollback(); err != nil {
			c.logger.Error("Rollback failed", slog.String("error", err.Error()))
		}
		c.ctx.AppliedPatches = nil

		// Include regression info in context for next fix attempt
		c.ctx.LastTestOutput = fmt.Sprintf("REGRESSION: The following tests failed after your fix:\n%s\n\nOutput:\n%s",
			result.FailedTests, result.Output)

		c.transition(StateWriteFix)
		return nil
	}

	// All tests pass - success!
	c.logger.Info("No regressions detected",
		slog.Int("total_tests", result.TotalTests),
	)

	c.transition(StateDone)
	return nil
}

// =============================================================================
// RESULT BUILDING
// =============================================================================

func (c *Controller) buildResult() *Result {
	result := &Result{
		Success:        c.ctx.State == StateDone,
		State:          c.ctx.State,
		ReproducerTest: c.ctx.ReproducerTest,
		AppliedPatches: c.ctx.AppliedPatches,
		Duration:       c.ctx.Elapsed(),
		Metrics:        c.ctx.Metrics,
	}

	if c.ctx.LastError != nil {
		result.Error = c.ctx.LastError.Error()
	}

	if c.ctx.LastTestOutput != "" && c.ctx.State == StateDone {
		result.TestResults = &TestResult{
			Passed: true,
			Output: c.ctx.LastTestOutput,
		}
	}

	return result
}

// GetContext returns the current TDG context (for inspection/debugging).
func (c *Controller) GetContext() *Context {
	return c.ctx
}

// IsRunning returns true if the controller is currently running.
func (c *Controller) IsRunning() bool {
	return c.running
}
