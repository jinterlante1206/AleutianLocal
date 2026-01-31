// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package nodes

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	mainDag "github.com/AleutianAI/AleutianFOSS/services/code_buddy/dag"
)

// TDGConfig contains configuration for Test-Driven Generation.
type TDGConfig struct {
	// MaxRetries is the maximum number of generation/fix attempts.
	MaxRetries int

	// TestTimeout is the timeout for running tests.
	TestTimeout time.Duration

	// Logger is the logger to use.
	Logger *slog.Logger
}

// DefaultTDGConfig returns sensible defaults.
func DefaultTDGConfig() TDGConfig {
	return TDGConfig{
		MaxRetries:  3,
		TestTimeout: 30 * time.Second,
		Logger:      slog.Default(),
	}
}

// TDGNode wraps Test-Driven Generation as a DAG node.
//
// Description:
//
//	TDG (Test-Driven Generation) is an iterative process:
//	1. Generate a failing test from a user query
//	2. Generate implementation to make test pass
//	3. If test fails, iterate on implementation
//	4. Return when test passes or max retries exceeded
//
//	This node wraps the TDG workflow as a single DAG node that
//	internally manages its own state machine.
//
// Inputs (from map[string]any):
//
//	"query" (string): The user's request describing what to implement. Required.
//	"target_file" (string): File path where implementation should go. Required.
//	"test_file" (string): File path where test should go. Required.
//	"language" (string): Programming language (e.g., "go", "python"). Required.
//	"context" (string): Additional context about the codebase. Optional.
//
// Outputs:
//
//	*TDGOutput containing:
//	  - TestCode: The generated test code
//	  - Implementation: The generated implementation code
//	  - Iterations: Number of iterations needed
//	  - Success: Whether generation succeeded
//	  - Duration: Total generation time
//
// Thread Safety:
//
//	Safe for concurrent use.
type TDGNode struct {
	mainDag.BaseNode
	config TDGConfig
	subDAG *mainDag.DAG
	logger *slog.Logger
}

// TDGOutput contains the result of Test-Driven Generation.
type TDGOutput struct {
	// TestCode is the generated test code.
	TestCode string

	// Implementation is the generated implementation code.
	Implementation string

	// Iterations is the number of generate/fix cycles.
	Iterations int

	// TestsPassed indicates all tests passed.
	TestsPassed bool

	// Success indicates the overall operation succeeded.
	Success bool

	// ErrorMessage contains error details if Success is false.
	ErrorMessage string

	// Duration is the total generation time.
	Duration time.Duration
}

// TDGRequest contains the input for TDG.
type TDGRequest struct {
	Query      string
	TargetFile string
	TestFile   string
	Language   string
	Context    string
}

// TDGExecutor defines the interface for TDG execution.
// This allows injecting the actual TDG implementation.
type TDGExecutor interface {
	Execute(ctx context.Context, req TDGRequest) (*TDGOutput, error)
}

// NewTDGNode creates a new TDG node.
//
// Inputs:
//
//	config - TDG configuration.
//	deps - Names of nodes this node depends on.
//
// Outputs:
//
//	*TDGNode - The configured node.
func NewTDGNode(config TDGConfig, deps []string) *TDGNode {
	if config.Logger == nil {
		config.Logger = slog.Default()
	}

	return &TDGNode{
		BaseNode: mainDag.BaseNode{
			NodeName:         "TDG",
			NodeDependencies: deps,
			NodeTimeout:      10 * time.Minute, // TDG can take a while
			NodeRetryable:    false,            // TDG manages its own retries
		},
		config: config,
		logger: config.Logger,
	}
}

// WithSubDAG sets a custom sub-DAG for TDG execution.
// This allows composing TDG as a nested DAG instead of a single executor.
func (n *TDGNode) WithSubDAG(dag *mainDag.DAG) *TDGNode {
	n.subDAG = dag
	return n
}

// Execute runs the TDG workflow.
//
// Description:
//
//	Extracts the TDG request from inputs and runs the generation loop.
//	The actual LLM interaction is delegated to the configured executor
//	or sub-DAG.
//
// Inputs:
//
//	ctx - Context for cancellation.
//	inputs - Map containing "query", "target_file", "test_file", "language".
//
// Outputs:
//
//	*TDGOutput - The generation result.
//	error - Non-nil if extraction fails or TDG completely fails.
//
// Thread Safety:
//
//	Safe for concurrent use.
func (n *TDGNode) Execute(ctx context.Context, inputs map[string]any) (any, error) {
	// Extract request
	req, err := n.extractInputs(inputs)
	if err != nil {
		return nil, err
	}

	start := time.Now()

	n.logger.Info("starting TDG",
		slog.String("query", truncate(req.Query, 100)),
		slog.String("target_file", req.TargetFile),
		slog.String("language", req.Language),
	)

	// If we have a sub-DAG, execute it
	if n.subDAG != nil {
		return n.executeSubDAG(ctx, req, start)
	}

	// Otherwise, return a placeholder indicating TDG needs an executor
	return &TDGOutput{
		Success:      false,
		ErrorMessage: "TDG executor not configured - use WithSubDAG or configure a TDG executor",
		Duration:     time.Since(start),
	}, nil
}

// executeSubDAG runs TDG as a nested DAG.
func (n *TDGNode) executeSubDAG(ctx context.Context, req TDGRequest, start time.Time) (*TDGOutput, error) {
	executor, err := mainDag.NewExecutor(n.subDAG, n.logger)
	if err != nil {
		return nil, fmt.Errorf("create sub-DAG executor: %w", err)
	}

	// Prepare input for sub-DAG
	input := map[string]any{
		"query":       req.Query,
		"target_file": req.TargetFile,
		"test_file":   req.TestFile,
		"language":    req.Language,
		"context":     req.Context,
	}

	// Execute sub-DAG
	result, err := executor.Run(ctx, input)
	if err != nil {
		return &TDGOutput{
			Success:      false,
			ErrorMessage: fmt.Sprintf("sub-DAG execution failed: %v", err),
			Duration:     time.Since(start),
		}, nil
	}

	// Extract output from sub-DAG result
	if result.Success {
		// The sub-DAG should produce TDGOutput-compatible data
		// This would need to be mapped from the actual terminal node output
		return &TDGOutput{
			Success:  true,
			Duration: time.Since(start),
		}, nil
	}

	return &TDGOutput{
		Success:      false,
		ErrorMessage: result.Error,
		Duration:     time.Since(start),
	}, nil
}

// extractInputs validates and extracts inputs from the map.
func (n *TDGNode) extractInputs(inputs map[string]any) (TDGRequest, error) {
	req := TDGRequest{}

	// Extract query (required)
	queryRaw, ok := inputs["query"]
	if !ok {
		return req, fmt.Errorf("%w: query", ErrMissingInput)
	}
	query, ok := queryRaw.(string)
	if !ok {
		return req, fmt.Errorf("%w: query must be string", ErrInvalidInputType)
	}
	req.Query = query

	// Extract target_file (required)
	targetRaw, ok := inputs["target_file"]
	if !ok {
		return req, fmt.Errorf("%w: target_file", ErrMissingInput)
	}
	target, ok := targetRaw.(string)
	if !ok {
		return req, fmt.Errorf("%w: target_file must be string", ErrInvalidInputType)
	}
	req.TargetFile = target

	// Extract test_file (required)
	testRaw, ok := inputs["test_file"]
	if !ok {
		return req, fmt.Errorf("%w: test_file", ErrMissingInput)
	}
	testFile, ok := testRaw.(string)
	if !ok {
		return req, fmt.Errorf("%w: test_file must be string", ErrInvalidInputType)
	}
	req.TestFile = testFile

	// Extract language (required)
	langRaw, ok := inputs["language"]
	if !ok {
		return req, fmt.Errorf("%w: language", ErrMissingInput)
	}
	lang, ok := langRaw.(string)
	if !ok {
		return req, fmt.Errorf("%w: language must be string", ErrInvalidInputType)
	}
	req.Language = lang

	// Extract context (optional)
	if contextRaw, ok := inputs["context"]; ok {
		if ctx, ok := contextRaw.(string); ok {
			req.Context = ctx
		}
	}

	return req, nil
}

// truncate shortens a string for logging.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
