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
)

// GateCondition defines a function that checks whether the gate should pass.
type GateCondition func(ctx context.Context, inputs map[string]any) (bool, string)

// GateNode provides conditional execution control.
//
// Description:
//
//	Evaluates a condition and either passes through or blocks execution.
//	Use this to implement quality gates, feature flags, or conditional
//	pipeline branches.
//
// Inputs (from map[string]any):
//
//	Depends on the configured condition function.
//
// Outputs:
//
//	*GateOutput containing:
//	  - Passed: Whether the gate condition was met
//	  - Reason: Explanation of why the gate passed or failed
//	  - Duration: Check time
//
// Thread Safety:
//
//	Safe for concurrent use.
type GateNode struct {
	dag.BaseNode
	condition   GateCondition
	blockOnFail bool
	passThrough bool // If true, passes inputs through on success
}

// GateOutput contains the result of gate evaluation.
type GateOutput struct {
	// Passed indicates whether the gate condition was met.
	Passed bool

	// Reason explains why the gate passed or failed.
	Reason string

	// PassedInputs contains inputs passed through (if passThrough is enabled).
	PassedInputs map[string]any

	// Duration is the check time.
	Duration time.Duration
}

// NewGateNode creates a new gate node with a condition.
//
// Inputs:
//
//	name - Unique name for this gate.
//	condition - Function that evaluates the gate condition.
//	deps - Names of nodes this node depends on.
//
// Outputs:
//
//	*GateNode - The configured node.
func NewGateNode(name string, condition GateCondition, deps []string) *GateNode {
	return &GateNode{
		BaseNode: dag.BaseNode{
			NodeName:         name,
			NodeDependencies: deps,
			NodeTimeout:      10 * time.Second,
			NodeRetryable:    false,
		},
		condition:   condition,
		blockOnFail: true,
		passThrough: true,
	}
}

// WithBlockOnFail configures whether to return an error when gate fails.
func (n *GateNode) WithBlockOnFail(block bool) *GateNode {
	n.blockOnFail = block
	return n
}

// WithPassThrough configures whether to pass inputs through on success.
func (n *GateNode) WithPassThrough(pass bool) *GateNode {
	n.passThrough = pass
	return n
}

// Execute evaluates the gate condition.
//
// Description:
//
//	Runs the configured condition function. If blockOnFail is true and
//	the condition returns false, returns ErrGateBlocked.
//
// Inputs:
//
//	ctx - Context for cancellation.
//	inputs - Inputs passed to the condition function.
//
// Outputs:
//
//	*GateOutput - The gate result.
//	error - ErrGateBlocked if condition fails and blockOnFail is true.
//
// Thread Safety:
//
//	Safe for concurrent use.
func (n *GateNode) Execute(ctx context.Context, inputs map[string]any) (any, error) {
	if n.condition == nil {
		return nil, fmt.Errorf("%w: gate condition", ErrNilDependency)
	}

	start := time.Now()

	// Evaluate condition
	passed, reason := n.condition(ctx, inputs)

	output := &GateOutput{
		Passed:   passed,
		Reason:   reason,
		Duration: time.Since(start),
	}

	// Pass through inputs if enabled and passed
	if n.passThrough && passed {
		output.PassedInputs = inputs
	}

	// Block if condition failed and blocking is enabled
	if !passed && n.blockOnFail {
		return output, fmt.Errorf("%w: %s", ErrGateBlocked, reason)
	}

	return output, nil
}

// Common gate conditions

// LintPassedCondition creates a condition that checks lint results.
func LintPassedCondition() GateCondition {
	return func(ctx context.Context, inputs map[string]any) (bool, string) {
		// Try to get lint check output
		if checkOutput, ok := inputs["LINT_CHECK"]; ok {
			if output, ok := checkOutput.(*LintCheckOutput); ok {
				if output.Passed {
					return true, "lint check passed"
				}
				return false, fmt.Sprintf("lint check failed: %d errors, %d warnings",
					output.ErrorCount, output.WarningCount)
			}
		}

		// Try to get lint analyze output
		if analyzeOutput, ok := inputs["LINT_ANALYZE"]; ok {
			if output, ok := analyzeOutput.(*LintAnalyzeOutput); ok {
				if output.TotalErrors == 0 {
					return true, fmt.Sprintf("no lint errors (%d warnings)", output.TotalWarnings)
				}
				return false, fmt.Sprintf("lint errors found: %d", output.TotalErrors)
			}
		}

		return false, "no lint results found"
	}
}

// SafetyPassedCondition creates a condition that checks security scan results.
func SafetyPassedCondition() GateCondition {
	return func(ctx context.Context, inputs map[string]any) (bool, string) {
		if scanOutput, ok := inputs["SAFETY_SCAN"]; ok {
			if output, ok := scanOutput.(*SafetyScanOutput); ok {
				if output.Passed {
					return true, "security scan passed (no critical/high issues)"
				}
				return false, fmt.Sprintf("security scan failed: %d critical, %d high",
					output.CriticalCount, output.HighCount)
			}
		}
		return false, "no security scan results found"
	}
}

// AllPassedCondition creates a condition that checks multiple node outputs.
func AllPassedCondition(nodeNames ...string) GateCondition {
	return func(ctx context.Context, inputs map[string]any) (bool, string) {
		for _, name := range nodeNames {
			output, ok := inputs[name]
			if !ok {
				return false, fmt.Sprintf("missing output from %s", name)
			}

			// Check for common "Passed" field pattern
			switch v := output.(type) {
			case *LintCheckOutput:
				if !v.Passed {
					return false, fmt.Sprintf("%s failed", name)
				}
			case *SafetyScanOutput:
				if !v.Passed {
					return false, fmt.Sprintf("%s failed", name)
				}
			case *GateOutput:
				if !v.Passed {
					return false, fmt.Sprintf("%s failed: %s", name, v.Reason)
				}
			}
		}
		return true, "all checks passed"
	}
}

// RiskLevelCondition creates a condition that checks blast radius risk level.
func RiskLevelCondition(maxLevel string) GateCondition {
	levelOrder := map[string]int{
		"low":      0,
		"medium":   1,
		"high":     2,
		"critical": 3,
	}

	maxLevelOrder, ok := levelOrder[maxLevel]
	if !ok {
		maxLevelOrder = 1 // Default to medium
	}

	return func(ctx context.Context, inputs map[string]any) (bool, string) {
		if blastOutput, ok := inputs["BLAST_RADIUS"]; ok {
			if output, ok := blastOutput.(*BlastRadiusOutput); ok {
				actualLevel := levelOrder[output.RiskLevel]
				if actualLevel <= maxLevelOrder {
					return true, fmt.Sprintf("risk level %s is acceptable (max: %s)",
						output.RiskLevel, maxLevel)
				}
				return false, fmt.Sprintf("risk level %s exceeds maximum %s",
					output.RiskLevel, maxLevel)
			}
		}
		return false, "no blast radius results found"
	}
}
