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

	"github.com/AleutianAI/AleutianFOSS/services/trace/dag"
	"github.com/AleutianAI/AleutianFOSS/services/trace/safety"
)

// SafetyScanNode performs security vulnerability scanning.
//
// Description:
//
//	Uses the security scanning interfaces to detect vulnerabilities
//	like SQL injection, XSS, command injection, and hardcoded secrets.
//
// Inputs (from map[string]any):
//
//	"scanner" (safety.SecurityScanner): The security scanner. Required.
//	"scope" (string): Package/file scope to scan. Required.
//	"min_severity" (string): Minimum severity to report. Optional.
//	"min_confidence" (float64): Minimum confidence to report. Optional.
//
// Outputs:
//
//	*SafetyScanOutput containing:
//	  - Result: The full scan result
//	  - IssueCount: Number of issues found
//	  - CriticalCount: Number of critical issues
//	  - Duration: Scan time
//
// Thread Safety:
//
//	Safe for concurrent use.
type SafetyScanNode struct {
	dag.BaseNode
	scanner       safety.SecurityScanner
	minSeverity   safety.Severity
	minConfidence float64
}

// SafetyScanOutput contains the result of security scanning.
type SafetyScanOutput struct {
	// Result is the full scan result.
	Result *safety.ScanResult

	// IssueCount is the total number of issues found.
	IssueCount int

	// CriticalCount is the number of critical issues.
	CriticalCount int

	// HighCount is the number of high severity issues.
	HighCount int

	// Passed indicates no critical or high issues were found.
	Passed bool

	// Duration is the scan time.
	Duration time.Duration
}

// NewSafetyScanNode creates a new safety scan node.
//
// Inputs:
//
//	scanner - The security scanner to use. Must not be nil.
//	deps - Names of nodes this node depends on.
//
// Outputs:
//
//	*SafetyScanNode - The configured node.
func NewSafetyScanNode(scanner safety.SecurityScanner, deps []string) *SafetyScanNode {
	return &SafetyScanNode{
		BaseNode: dag.BaseNode{
			NodeName:         "SAFETY_SCAN",
			NodeDependencies: deps,
			NodeTimeout:      3 * time.Minute,
			NodeRetryable:    false,
		},
		scanner:       scanner,
		minSeverity:   safety.SeverityMedium,
		minConfidence: 0.5,
	}
}

// WithMinSeverity sets the minimum severity to report.
func (n *SafetyScanNode) WithMinSeverity(sev safety.Severity) *SafetyScanNode {
	n.minSeverity = sev
	return n
}

// WithMinConfidence sets the minimum confidence to report.
func (n *SafetyScanNode) WithMinConfidence(conf float64) *SafetyScanNode {
	n.minConfidence = conf
	return n
}

// Execute performs security scanning on the specified scope.
//
// Description:
//
//	Runs the security scanner against the specified scope.
//
// Inputs:
//
//	ctx - Context for cancellation.
//	inputs - Map containing "scope" and optionally severity/confidence thresholds.
//
// Outputs:
//
//	*SafetyScanOutput - The scan result.
//	error - Non-nil if scanner is nil or scope is missing.
//
// Thread Safety:
//
//	Safe for concurrent use.
func (n *SafetyScanNode) Execute(ctx context.Context, inputs map[string]any) (any, error) {
	if n.scanner == nil {
		return nil, fmt.Errorf("%w: security scanner", ErrNilDependency)
	}

	// Extract inputs
	scope, err := n.extractInputs(inputs)
	if err != nil {
		return nil, err
	}

	start := time.Now()

	// Configure scan options
	opts := []safety.ScanOption{
		safety.WithMinSeverity(n.minSeverity),
		safety.WithMinConfidence(n.minConfidence),
	}

	// Run scan
	result, err := n.scanner.ScanForSecurityIssues(ctx, scope, opts...)
	if err != nil {
		return nil, fmt.Errorf("security scan: %w", err)
	}

	// Calculate counts
	criticalCount := 0
	highCount := 0
	for _, issue := range result.Issues {
		switch issue.Severity {
		case safety.SeverityCritical:
			criticalCount++
		case safety.SeverityHigh:
			highCount++
		}
	}

	passed := criticalCount == 0 && highCount == 0

	return &SafetyScanOutput{
		Result:        result,
		IssueCount:    len(result.Issues),
		CriticalCount: criticalCount,
		HighCount:     highCount,
		Passed:        passed,
		Duration:      time.Since(start),
	}, nil
}

// extractInputs validates and extracts inputs from the map.
func (n *SafetyScanNode) extractInputs(inputs map[string]any) (string, error) {
	scopeRaw, ok := inputs["scope"]
	if !ok {
		return "", fmt.Errorf("%w: scope", ErrMissingInput)
	}

	scope, ok := scopeRaw.(string)
	if !ok {
		return "", fmt.Errorf("%w: scope must be string", ErrInvalidInputType)
	}

	return scope, nil
}
