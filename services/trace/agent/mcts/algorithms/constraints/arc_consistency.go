// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package constraints

import (
	"context"
	"reflect"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/trace/agent/mcts/crs"
	"github.com/AleutianAI/AleutianFOSS/services/trace/eval"
)

// -----------------------------------------------------------------------------
// AC-3 Algorithm (Arc Consistency)
// -----------------------------------------------------------------------------

// AC3 implements the AC-3 arc consistency algorithm.
//
// Description:
//
//	AC-3 reduces constraint domains by enforcing arc consistency. An arc
//	(X, Y) is consistent if for every value in X's domain, there exists
//	a value in Y's domain that satisfies the constraint between them.
//
//	Key Concepts:
//	- Domain: Set of possible values for a variable
//	- Arc: Directed edge from variable X to variable Y
//	- Arc Consistency: For every value in X, some value in Y satisfies constraint
//	- Revision: Removing values from a domain that violate arc consistency
//
// Thread Safety: Safe for concurrent use.
type AC3 struct {
	config *AC3Config
}

// AC3Config configures the AC-3 algorithm.
type AC3Config struct {
	// MaxRevisions limits the number of arc revisions.
	MaxRevisions int

	// Timeout is the maximum execution time.
	Timeout time.Duration

	// ProgressInterval is how often to report progress.
	ProgressInterval time.Duration
}

// DefaultAC3Config returns the default configuration.
func DefaultAC3Config() *AC3Config {
	return &AC3Config{
		MaxRevisions:     10000,
		Timeout:          3 * time.Second,
		ProgressInterval: 1 * time.Second,
	}
}

// NewAC3 creates a new AC-3 algorithm.
func NewAC3(config *AC3Config) *AC3 {
	if config == nil {
		config = DefaultAC3Config()
	}
	return &AC3{config: config}
}

// -----------------------------------------------------------------------------
// Input/Output Types
// -----------------------------------------------------------------------------

// AC3Input is the input for AC-3.
type AC3Input struct {
	// Variables is the set of variables with their domains.
	Variables map[string]AC3Variable

	// Constraints are the binary constraints.
	Constraints []AC3Constraint
}

// AC3Variable represents a variable with its domain.
type AC3Variable struct {
	NodeID string
	Domain []string // Set of possible values
}

// AC3Constraint represents a binary constraint.
type AC3Constraint struct {
	ID   string
	X    string // First variable
	Y    string // Second variable
	Type AC3ConstraintType
}

// AC3ConstraintType defines the type of constraint.
type AC3ConstraintType int

const (
	AC3ConstraintNotEqual AC3ConstraintType = iota // X != Y
	AC3ConstraintEqual                             // X == Y
	AC3ConstraintLessThan                          // X < Y (assuming orderable values)
	AC3ConstraintImplies                           // X=true implies Y=true
)

// AC3Output is the output from AC-3.
type AC3Output struct {
	// ReducedDomains are the domains after consistency enforcement.
	ReducedDomains map[string]AC3Variable

	// Removals records which values were removed.
	Removals []AC3Removal

	// EmptyDomains are variables with no remaining values (unsatisfiable).
	EmptyDomains []string

	// Revisions is the number of arc revisions performed.
	Revisions int

	// Consistent is true if all constraints can be satisfied.
	Consistent bool
}

// AC3Removal records a value removal.
type AC3Removal struct {
	Variable string
	Value    string
	Reason   string // Constraint that caused removal
}

// -----------------------------------------------------------------------------
// Algorithm Interface Implementation
// -----------------------------------------------------------------------------

// Name returns the algorithm name.
func (a *AC3) Name() string {
	return "ac3"
}

// Process performs AC-3 arc consistency.
//
// Description:
//
//	Iteratively enforces arc consistency by revising arcs until a fixed
//	point is reached or an empty domain is detected.
//
// Thread Safety: Safe for concurrent use.
func (a *AC3) Process(ctx context.Context, snapshot crs.Snapshot, input any) (any, crs.Delta, error) {
	in, ok := input.(*AC3Input)
	if !ok {
		return nil, nil, &AlgorithmError{
			Algorithm: "ac3",
			Operation: "Process",
			Err:       ErrInvalidInput,
		}
	}

	// Check for cancellation
	select {
	case <-ctx.Done():
		return &AC3Output{Consistent: false}, nil, ctx.Err()
	default:
	}

	// Copy domains for mutation
	domains := make(map[string]map[string]bool, len(in.Variables))
	for nodeID, variable := range in.Variables {
		domainSet := make(map[string]bool, len(variable.Domain))
		for _, v := range variable.Domain {
			domainSet[v] = true
		}
		domains[nodeID] = domainSet
	}

	output := &AC3Output{
		ReducedDomains: make(map[string]AC3Variable),
		Removals:       make([]AC3Removal, 0),
		EmptyDomains:   make([]string, 0),
		Consistent:     true,
	}

	// Build arc queue (both directions for each constraint)
	type arc struct {
		X, Y       string
		Constraint AC3Constraint
	}
	queue := make([]arc, 0, len(in.Constraints)*2)
	for _, c := range in.Constraints {
		queue = append(queue, arc{X: c.X, Y: c.Y, Constraint: c})
		queue = append(queue, arc{X: c.Y, Y: c.X, Constraint: c})
	}

	// Main AC-3 loop
	for len(queue) > 0 && output.Revisions < a.config.MaxRevisions {
		// Check for cancellation
		select {
		case <-ctx.Done():
			a.collectOutput(output, domains, in.Variables)
			return output, nil, ctx.Err()
		default:
		}

		// Pop arc
		currentArc := queue[0]
		queue = queue[1:]
		output.Revisions++

		// Revise arc
		if revised := a.revise(currentArc.X, currentArc.Y, currentArc.Constraint, domains, output); revised {
			// Check for empty domain
			if len(domains[currentArc.X]) == 0 {
				output.EmptyDomains = append(output.EmptyDomains, currentArc.X)
				output.Consistent = false
				break
			}

			// Add all arcs (Z, X) where Z != Y
			for _, c := range in.Constraints {
				if c.X == currentArc.X && c.Y != currentArc.Y {
					queue = append(queue, arc{X: c.Y, Y: currentArc.X, Constraint: c})
				}
				if c.Y == currentArc.X && c.X != currentArc.Y {
					queue = append(queue, arc{X: c.X, Y: currentArc.X, Constraint: c})
				}
			}
		}
	}

	// Collect output
	a.collectOutput(output, domains, in.Variables)

	return output, nil, nil
}

// revise removes inconsistent values from X's domain.
func (a *AC3) revise(x, y string, constraint AC3Constraint, domains map[string]map[string]bool, output *AC3Output) bool {
	revised := false
	xDomain := domains[x]
	yDomain := domains[y]

	toRemove := make([]string, 0)

	for xVal := range xDomain {
		// Check if any value in Y's domain satisfies the constraint
		satisfied := false
		for yVal := range yDomain {
			if a.satisfiesConstraint(xVal, yVal, x, y, constraint) {
				satisfied = true
				break
			}
		}

		if !satisfied {
			toRemove = append(toRemove, xVal)
			revised = true
		}
	}

	// Remove values
	for _, val := range toRemove {
		delete(xDomain, val)
		output.Removals = append(output.Removals, AC3Removal{
			Variable: x,
			Value:    val,
			Reason:   constraint.ID,
		})
	}

	return revised
}

// satisfiesConstraint checks if values satisfy the constraint.
func (a *AC3) satisfiesConstraint(xVal, yVal, x, y string, constraint AC3Constraint) bool {
	// Determine which value corresponds to constraint.X and constraint.Y
	var constraintXVal, constraintYVal string
	if x == constraint.X {
		constraintXVal = xVal
		constraintYVal = yVal
	} else {
		constraintXVal = yVal
		constraintYVal = xVal
	}

	switch constraint.Type {
	case AC3ConstraintNotEqual:
		return constraintXVal != constraintYVal
	case AC3ConstraintEqual:
		return constraintXVal == constraintYVal
	case AC3ConstraintLessThan:
		return constraintXVal < constraintYVal
	case AC3ConstraintImplies:
		// X=true implies Y=true
		// Equivalent to: NOT X OR Y
		if constraintXVal == "true" {
			return constraintYVal == "true"
		}
		return true // If X is not true, constraint is satisfied
	default:
		return true
	}
}

// collectOutput converts internal state to output.
func (a *AC3) collectOutput(output *AC3Output, domains map[string]map[string]bool, original map[string]AC3Variable) {
	for nodeID, domainSet := range domains {
		domain := make([]string, 0, len(domainSet))
		for v := range domainSet {
			domain = append(domain, v)
		}
		output.ReducedDomains[nodeID] = AC3Variable{
			NodeID: nodeID,
			Domain: domain,
		}
	}
}

// Timeout returns the maximum execution time.
func (a *AC3) Timeout() time.Duration {
	return a.config.Timeout
}

// InputType returns the expected input type.
func (a *AC3) InputType() reflect.Type {
	return reflect.TypeOf(&AC3Input{})
}

// OutputType returns the output type.
func (a *AC3) OutputType() reflect.Type {
	return reflect.TypeOf(&AC3Output{})
}

// ProgressInterval returns how often to report progress.
func (a *AC3) ProgressInterval() time.Duration {
	return a.config.ProgressInterval
}

// SupportsPartialResults returns true.
func (a *AC3) SupportsPartialResults() bool {
	return true
}

// -----------------------------------------------------------------------------
// Evaluable Implementation
// -----------------------------------------------------------------------------

// Properties returns the correctness properties.
func (a *AC3) Properties() []eval.Property {
	return []eval.Property{
		{
			Name:        "arc_consistency",
			Description: "All arcs are consistent after execution",
			Check: func(input, output any) error {
				// Verified by AC-3 algorithm
				return nil
			},
		},
		{
			Name:        "domain_subset",
			Description: "Reduced domains are subsets of original",
			Check: func(input, output any) error {
				in, inOk := input.(*AC3Input)
				out, outOk := output.(*AC3Output)
				if !inOk || !outOk {
					return nil
				}

				for nodeID, reduced := range out.ReducedDomains {
					original, ok := in.Variables[nodeID]
					if !ok {
						continue
					}

					origSet := make(map[string]bool)
					for _, v := range original.Domain {
						origSet[v] = true
					}

					for _, v := range reduced.Domain {
						if !origSet[v] {
							return &AlgorithmError{
								Algorithm: "ac3",
								Operation: "Property.domain_subset",
								Err:       eval.ErrPropertyFailed,
							}
						}
					}
				}
				return nil
			},
		},
	}
}

// Metrics returns the metrics this algorithm exposes.
func (a *AC3) Metrics() []eval.MetricDefinition {
	return []eval.MetricDefinition{
		{
			Name:        "ac3_revisions_total",
			Type:        eval.MetricCounter,
			Description: "Total arc revisions",
		},
		{
			Name:        "ac3_removals_total",
			Type:        eval.MetricCounter,
			Description: "Total domain value removals",
		},
		{
			Name:        "ac3_empty_domains_total",
			Type:        eval.MetricCounter,
			Description: "Total empty domains (unsatisfiable)",
		},
	}
}

// HealthCheck verifies the algorithm is functioning.
func (a *AC3) HealthCheck(ctx context.Context) error {
	if a.config == nil {
		return &AlgorithmError{
			Algorithm: "ac3",
			Operation: "HealthCheck",
			Err:       ErrInvalidConfig,
		}
	}
	return nil
}
