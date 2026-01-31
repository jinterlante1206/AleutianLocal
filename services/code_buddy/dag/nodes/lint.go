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
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/dag"
	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/lint"
)

// LintAnalyzeNode runs linters on source files.
//
// Description:
//
//	Uses the lint.LintRunner to analyze source files for code quality
//	issues. Supports multiple languages and runs linting in parallel.
//
// Inputs (from map[string]any):
//
//	"files" ([]string): File paths to lint. Required.
//	"project_root" (string): Project root for relative paths. Optional.
//
// Outputs:
//
//	*LintAnalyzeOutput containing:
//	  - Results: Lint results for each file
//	  - TotalErrors: Count of errors across all files
//	  - TotalWarnings: Count of warnings across all files
//	  - Duration: Lint time
//
// Thread Safety:
//
//	Safe for concurrent use.
type LintAnalyzeNode struct {
	dag.BaseNode
	runner *lint.LintRunner
}

// LintAnalyzeOutput contains the result of linting files.
type LintAnalyzeOutput struct {
	// Results contains lint results for each file.
	Results []*lint.LintResult

	// TotalErrors is the count of errors across all files.
	TotalErrors int

	// TotalWarnings is the count of warnings across all files.
	TotalWarnings int

	// FilesLinted is the number of files that were linted.
	FilesLinted int

	// FilesSkipped is the number of files skipped (unsupported language).
	FilesSkipped int

	// Duration is the lint time.
	Duration time.Duration
}

// NewLintAnalyzeNode creates a new lint analyze node.
//
// Inputs:
//
//	runner - The lint runner to use. Must not be nil.
//	deps - Names of nodes this node depends on.
//
// Outputs:
//
//	*LintAnalyzeNode - The configured node.
func NewLintAnalyzeNode(runner *lint.LintRunner, deps []string) *LintAnalyzeNode {
	return &LintAnalyzeNode{
		BaseNode: dag.BaseNode{
			NodeName:         "LINT_ANALYZE",
			NodeDependencies: deps,
			NodeTimeout:      5 * time.Minute,
			NodeRetryable:    false,
		},
		runner: runner,
	}
}

// Execute runs linting on the specified files.
//
// Description:
//
//	Lints each file using the appropriate linter based on file extension.
//	Files with unsupported extensions are skipped.
//
// Inputs:
//
//	ctx - Context for cancellation.
//	inputs - Map containing "files" and optionally "project_root".
//
// Outputs:
//
//	*LintAnalyzeOutput - The lint results.
//	error - Non-nil if runner is nil.
//
// Thread Safety:
//
//	Safe for concurrent use.
func (n *LintAnalyzeNode) Execute(ctx context.Context, inputs map[string]any) (any, error) {
	if n.runner == nil {
		return nil, fmt.Errorf("%w: lint runner", ErrNilDependency)
	}

	// Extract inputs
	files, err := n.extractInputs(inputs)
	if err != nil {
		return nil, err
	}

	if len(files) == 0 {
		return &LintAnalyzeOutput{
			Results:       make([]*lint.LintResult, 0),
			TotalErrors:   0,
			TotalWarnings: 0,
			FilesLinted:   0,
			FilesSkipped:  0,
		}, nil
	}

	start := time.Now()

	// Lint files (LintRunner handles parallelism internally)
	results, err := n.runner.LintFiles(ctx, files)
	if err != nil {
		// Don't fail completely - partial results may be available
		if results == nil {
			return nil, fmt.Errorf("%w: %v", ErrLintFailed, err)
		}
	}

	// Calculate totals
	totalErrors := 0
	totalWarnings := 0
	filesLinted := 0
	filesSkipped := 0

	for _, r := range results {
		if r == nil {
			filesSkipped++
			continue
		}
		if !r.LinterAvailable {
			filesSkipped++
			continue
		}
		filesLinted++
		totalErrors += len(r.Errors)
		totalWarnings += len(r.Warnings)
	}

	return &LintAnalyzeOutput{
		Results:       results,
		TotalErrors:   totalErrors,
		TotalWarnings: totalWarnings,
		FilesLinted:   filesLinted,
		FilesSkipped:  filesSkipped,
		Duration:      time.Since(start),
	}, nil
}

// extractInputs validates and extracts inputs from the map.
func (n *LintAnalyzeNode) extractInputs(inputs map[string]any) ([]string, error) {
	filesRaw, ok := inputs["files"]
	if !ok {
		return nil, fmt.Errorf("%w: files", ErrMissingInput)
	}

	if files, ok := filesRaw.([]string); ok {
		return files, nil
	}

	// Handle []any
	if filesAny, ok := filesRaw.([]any); ok {
		files := make([]string, len(filesAny))
		for i, f := range filesAny {
			s, ok := f.(string)
			if !ok {
				return nil, fmt.Errorf("%w: files[%d] must be string", ErrInvalidInputType, i)
			}
			files[i] = s
		}
		return files, nil
	}

	return nil, fmt.Errorf("%w: files must be []string", ErrInvalidInputType)
}

// LintCheckNode validates lint results against policies.
//
// Description:
//
//	Checks lint results from LINT_ANALYZE against configured policies.
//	Can be used as a quality gate to block pipelines with too many issues.
//
// Inputs (from map[string]any):
//
//	"lint_results" (*LintAnalyzeOutput): Results from LINT_ANALYZE. Required.
//	"max_errors" (int): Maximum allowed errors. Optional, default 0.
//	"max_warnings" (int): Maximum allowed warnings. Optional, default -1 (unlimited).
//
// Outputs:
//
//	*LintCheckOutput containing:
//	  - Passed: Whether the check passed
//	  - ErrorCount: Number of errors found
//	  - WarningCount: Number of warnings found
//	  - Violations: Details of policy violations
//
// Thread Safety:
//
//	Safe for concurrent use.
type LintCheckNode struct {
	dag.BaseNode
	maxErrors   int
	maxWarnings int
}

// LintCheckOutput contains the result of lint policy checking.
type LintCheckOutput struct {
	// Passed indicates whether all policies were satisfied.
	Passed bool

	// ErrorCount is the number of lint errors.
	ErrorCount int

	// WarningCount is the number of lint warnings.
	WarningCount int

	// Violations contains details of policy violations.
	Violations []string

	// Duration is the check time.
	Duration time.Duration
}

// NewLintCheckNode creates a new lint check node.
//
// Inputs:
//
//	maxErrors - Maximum allowed errors (0 = none allowed).
//	maxWarnings - Maximum allowed warnings (-1 = unlimited).
//	deps - Names of nodes this node depends on.
//
// Outputs:
//
//	*LintCheckNode - The configured node.
func NewLintCheckNode(maxErrors, maxWarnings int, deps []string) *LintCheckNode {
	return &LintCheckNode{
		BaseNode: dag.BaseNode{
			NodeName:         "LINT_CHECK",
			NodeDependencies: deps,
			NodeTimeout:      10 * time.Second,
			NodeRetryable:    false,
		},
		maxErrors:   maxErrors,
		maxWarnings: maxWarnings,
	}
}

// Execute checks lint results against policies.
//
// Description:
//
//	Compares lint results against configured thresholds. Fails if
//	error or warning counts exceed the maximum allowed.
//
// Inputs:
//
//	ctx - Context for cancellation.
//	inputs - Map containing "lint_results".
//
// Outputs:
//
//	*LintCheckOutput - The check result.
//	error - Non-nil if inputs are invalid.
//
// Thread Safety:
//
//	Safe for concurrent use.
func (n *LintCheckNode) Execute(ctx context.Context, inputs map[string]any) (any, error) {
	start := time.Now()

	// Extract inputs
	lintOutput, err := n.extractInputs(inputs)
	if err != nil {
		return nil, err
	}

	violations := make([]string, 0)
	passed := true

	// Check errors
	if lintOutput.TotalErrors > n.maxErrors {
		passed = false
		violations = append(violations, fmt.Sprintf(
			"error count %d exceeds maximum %d",
			lintOutput.TotalErrors, n.maxErrors,
		))
	}

	// Check warnings (if limit is set)
	if n.maxWarnings >= 0 && lintOutput.TotalWarnings > n.maxWarnings {
		passed = false
		violations = append(violations, fmt.Sprintf(
			"warning count %d exceeds maximum %d",
			lintOutput.TotalWarnings, n.maxWarnings,
		))
	}

	return &LintCheckOutput{
		Passed:       passed,
		ErrorCount:   lintOutput.TotalErrors,
		WarningCount: lintOutput.TotalWarnings,
		Violations:   violations,
		Duration:     time.Since(start),
	}, nil
}

// extractInputs validates and extracts inputs from the map.
func (n *LintCheckNode) extractInputs(inputs map[string]any) (*LintAnalyzeOutput, error) {
	resultsRaw, ok := inputs["lint_results"]
	if !ok {
		// Try to get from LINT_ANALYZE output
		if analyzeOutput, ok := inputs["LINT_ANALYZE"]; ok {
			if output, ok := analyzeOutput.(*LintAnalyzeOutput); ok {
				return output, nil
			}
		}
		return nil, fmt.Errorf("%w: lint_results", ErrMissingInput)
	}

	output, ok := resultsRaw.(*LintAnalyzeOutput)
	if !ok {
		return nil, fmt.Errorf("%w: lint_results must be *LintAnalyzeOutput", ErrInvalidInputType)
	}

	return output, nil
}
