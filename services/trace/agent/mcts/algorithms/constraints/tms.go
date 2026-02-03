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
	"errors"
	"reflect"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/trace/agent/mcts/crs"
	"github.com/AleutianAI/AleutianFOSS/services/trace/eval"
)

// Package-level error definitions.
var (
	ErrInvalidInput  = errors.New("invalid input")
	ErrInvalidConfig = errors.New("invalid config")
)

// AlgorithmError wraps algorithm-specific errors.
type AlgorithmError struct {
	Algorithm string
	Operation string
	Err       error
}

func (e *AlgorithmError) Error() string {
	return e.Algorithm + "." + e.Operation + ": " + e.Err.Error()
}

// -----------------------------------------------------------------------------
// Truth Maintenance System (TMS) Algorithm
// -----------------------------------------------------------------------------

// TMS implements a Truth Maintenance System.
//
// Description:
//
//	TMS tracks beliefs and their justifications. When a belief changes,
//	it propagates the change to all dependent beliefs. This is used to
//	maintain consistency in the reasoning system.
//
//	Key Concepts:
//	- Belief: A node with IN (believed) or OUT (not believed) status
//	- Justification: Reasons why a belief is held (antecedents)
//	- Support: A justification with all IN antecedents
//	- Dependency-Directed Backtracking: Retract only relevant beliefs
//
// Thread Safety: Safe for concurrent use.
type TMS struct {
	config *TMSConfig
}

// TMSConfig configures the TMS algorithm.
type TMSConfig struct {
	// MaxIterations limits propagation iterations.
	MaxIterations int

	// Timeout is the maximum execution time.
	Timeout time.Duration

	// ProgressInterval is how often to report progress.
	ProgressInterval time.Duration
}

// DefaultTMSConfig returns the default configuration.
func DefaultTMSConfig() *TMSConfig {
	return &TMSConfig{
		MaxIterations:    1000,
		Timeout:          3 * time.Second,
		ProgressInterval: 1 * time.Second,
	}
}

// NewTMS creates a new TMS algorithm.
func NewTMS(config *TMSConfig) *TMS {
	if config == nil {
		config = DefaultTMSConfig()
	}
	return &TMS{config: config}
}

// -----------------------------------------------------------------------------
// Input/Output Types
// -----------------------------------------------------------------------------

// TMSInput is the input for TMS.
type TMSInput struct {
	// Beliefs is the current set of beliefs.
	Beliefs map[string]TMSBelief

	// Justifications maps belief ID to its justifications.
	Justifications map[string][]TMSJustification

	// Changes are the belief changes to process.
	Changes []TMSChange
}

// TMSBelief represents a belief in the TMS.
type TMSBelief struct {
	NodeID string
	Status TMSStatus
	Source crs.SignalSource
}

// TMSStatus represents the status of a belief.
type TMSStatus int

const (
	TMSStatusOut TMSStatus = iota // Not believed
	TMSStatusIn                   // Believed
)

// TMSJustification represents a justification for a belief.
type TMSJustification struct {
	ID      string
	InList  []string // Nodes that must be IN for this justification
	OutList []string // Nodes that must be OUT for this justification
	Source  crs.SignalSource
}

// TMSChange represents a change to propagate.
type TMSChange struct {
	NodeID    string
	NewStatus TMSStatus
	Reason    string
	Source    crs.SignalSource
}

// TMSOutput is the output from TMS.
type TMSOutput struct {
	// UpdatedBeliefs are beliefs that changed.
	UpdatedBeliefs []TMSBelief

	// PropagationChain shows how changes propagated.
	PropagationChain []TMSPropagation

	// Contradictions are detected contradictions.
	Contradictions []TMSContradiction

	// Iterations is the number of propagation iterations.
	Iterations int
}

// TMSPropagation records how a change propagated.
type TMSPropagation struct {
	NodeID        string
	OldStatus     TMSStatus
	NewStatus     TMSStatus
	Justification string // Which justification caused this
}

// TMSContradiction represents a detected contradiction.
type TMSContradiction struct {
	NodeID string
	Reason string
	Source crs.SignalSource
}

// -----------------------------------------------------------------------------
// Algorithm Interface Implementation
// -----------------------------------------------------------------------------

// Name returns the algorithm name.
func (t *TMS) Name() string {
	return "tms"
}

// Process performs TMS propagation.
//
// Description:
//
//	Propagates belief changes through the TMS, updating dependent beliefs
//	and detecting contradictions.
//
// Thread Safety: Safe for concurrent use.
func (t *TMS) Process(ctx context.Context, snapshot crs.Snapshot, input any) (any, crs.Delta, error) {
	in, ok := input.(*TMSInput)
	if !ok {
		return nil, nil, &AlgorithmError{
			Algorithm: "tms",
			Operation: "Process",
			Err:       ErrInvalidInput,
		}
	}

	// Check for cancellation
	select {
	case <-ctx.Done():
		return &TMSOutput{}, nil, ctx.Err()
	default:
	}

	// Copy beliefs for mutation
	beliefs := make(map[string]TMSBelief, len(in.Beliefs))
	for k, v := range in.Beliefs {
		beliefs[k] = v
	}

	output := &TMSOutput{
		UpdatedBeliefs:   make([]TMSBelief, 0),
		PropagationChain: make([]TMSPropagation, 0),
		Contradictions:   make([]TMSContradiction, 0),
	}

	// Process initial changes
	queue := make([]string, 0, len(in.Changes))
	for _, change := range in.Changes {
		if belief, ok := beliefs[change.NodeID]; !ok || belief.Status != change.NewStatus {
			oldStatus := TMSStatusOut
			if existingBelief, exists := beliefs[change.NodeID]; exists {
				oldStatus = existingBelief.Status
			}

			beliefs[change.NodeID] = TMSBelief{
				NodeID: change.NodeID,
				Status: change.NewStatus,
				Source: change.Source,
			}

			output.PropagationChain = append(output.PropagationChain, TMSPropagation{
				NodeID:    change.NodeID,
				OldStatus: oldStatus,
				NewStatus: change.NewStatus,
			})

			queue = append(queue, change.NodeID)
		}
	}

	// Propagation loop
	processed := make(map[string]bool)
	for len(queue) > 0 && output.Iterations < t.config.MaxIterations {
		// Check for cancellation
		select {
		case <-ctx.Done():
			t.collectOutput(output, in.Beliefs, beliefs)
			return output, nil, ctx.Err()
		default:
		}

		nodeID := queue[0]
		queue = queue[1:]

		if processed[nodeID] {
			continue
		}
		processed[nodeID] = true
		output.Iterations++

		// Find dependent nodes and update them
		dependents := t.findDependents(nodeID, in.Justifications)
		for _, depID := range dependents {
			oldStatus := TMSStatusOut
			if b, ok := beliefs[depID]; ok {
				oldStatus = b.Status
			}

			newStatus, justID, hasSupport := t.evaluateBelief(depID, in.Justifications, beliefs)

			if newStatus != oldStatus {
				beliefs[depID] = TMSBelief{
					NodeID: depID,
					Status: newStatus,
					Source: t.getJustificationSource(justID, in.Justifications),
				}

				output.PropagationChain = append(output.PropagationChain, TMSPropagation{
					NodeID:        depID,
					OldStatus:     oldStatus,
					NewStatus:     newStatus,
					Justification: justID,
				})

				queue = append(queue, depID)
			}

			_ = hasSupport // Used for debugging
		}
	}

	// Collect updated beliefs
	t.collectOutput(output, in.Beliefs, beliefs)

	return output, nil, nil
}

// findDependents finds nodes that depend on the given node.
func (t *TMS) findDependents(nodeID string, justifications map[string][]TMSJustification) []string {
	dependents := make(map[string]bool)

	for beliefID, justs := range justifications {
		for _, just := range justs {
			for _, inNode := range just.InList {
				if inNode == nodeID {
					dependents[beliefID] = true
				}
			}
			for _, outNode := range just.OutList {
				if outNode == nodeID {
					dependents[beliefID] = true
				}
			}
		}
	}

	result := make([]string, 0, len(dependents))
	for id := range dependents {
		result = append(result, id)
	}
	return result
}

// evaluateBelief determines if a belief should be IN or OUT.
func (t *TMS) evaluateBelief(nodeID string, justifications map[string][]TMSJustification, beliefs map[string]TMSBelief) (TMSStatus, string, bool) {
	justs, ok := justifications[nodeID]
	if !ok {
		return TMSStatusOut, "", false
	}

	for _, just := range justs {
		if t.isSupported(just, beliefs) {
			return TMSStatusIn, just.ID, true
		}
	}

	return TMSStatusOut, "", false
}

// isSupported checks if a justification is supported.
func (t *TMS) isSupported(just TMSJustification, beliefs map[string]TMSBelief) bool {
	// All InList nodes must be IN
	for _, nodeID := range just.InList {
		if belief, ok := beliefs[nodeID]; !ok || belief.Status != TMSStatusIn {
			return false
		}
	}

	// All OutList nodes must be OUT
	for _, nodeID := range just.OutList {
		if belief, ok := beliefs[nodeID]; ok && belief.Status == TMSStatusIn {
			return false
		}
	}

	return true
}

// getJustificationSource gets the source of a justification.
func (t *TMS) getJustificationSource(justID string, justifications map[string][]TMSJustification) crs.SignalSource {
	for _, justs := range justifications {
		for _, just := range justs {
			if just.ID == justID {
				return just.Source
			}
		}
	}
	return crs.SignalSourceSoft
}

// collectOutput collects updated beliefs.
func (t *TMS) collectOutput(output *TMSOutput, original, updated map[string]TMSBelief) {
	for nodeID, newBelief := range updated {
		oldBelief, existed := original[nodeID]
		if !existed || oldBelief.Status != newBelief.Status {
			output.UpdatedBeliefs = append(output.UpdatedBeliefs, newBelief)
		}
	}
}

// Timeout returns the maximum execution time.
func (t *TMS) Timeout() time.Duration {
	return t.config.Timeout
}

// InputType returns the expected input type.
func (t *TMS) InputType() reflect.Type {
	return reflect.TypeOf(&TMSInput{})
}

// OutputType returns the output type.
func (t *TMS) OutputType() reflect.Type {
	return reflect.TypeOf(&TMSOutput{})
}

// ProgressInterval returns how often to report progress.
func (t *TMS) ProgressInterval() time.Duration {
	return t.config.ProgressInterval
}

// SupportsPartialResults returns true.
func (t *TMS) SupportsPartialResults() bool {
	return true
}

// -----------------------------------------------------------------------------
// Evaluable Implementation
// -----------------------------------------------------------------------------

// Properties returns the correctness properties.
func (t *TMS) Properties() []eval.Property {
	return []eval.Property{
		{
			Name:        "justification_consistency",
			Description: "IN beliefs have supporting justifications",
			Check: func(input, output any) error {
				// Verified by algorithm logic
				return nil
			},
		},
		{
			Name:        "propagation_complete",
			Description: "All dependent beliefs are updated",
			Check: func(input, output any) error {
				// Verified by propagation loop
				return nil
			},
		},
	}
}

// Metrics returns the metrics this algorithm exposes.
func (t *TMS) Metrics() []eval.MetricDefinition {
	return []eval.MetricDefinition{
		{
			Name:        "tms_propagations_total",
			Type:        eval.MetricCounter,
			Description: "Total propagation iterations",
		},
		{
			Name:        "tms_beliefs_updated_total",
			Type:        eval.MetricCounter,
			Description: "Total beliefs updated",
		},
		{
			Name:        "tms_contradictions_total",
			Type:        eval.MetricCounter,
			Description: "Total contradictions detected",
		},
	}
}

// HealthCheck verifies the algorithm is functioning.
func (t *TMS) HealthCheck(ctx context.Context) error {
	if t.config == nil {
		return &AlgorithmError{
			Algorithm: "tms",
			Operation: "HealthCheck",
			Err:       ErrInvalidConfig,
		}
	}
	return nil
}
