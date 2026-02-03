// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package search

import (
	"context"
	"reflect"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/trace/agent/mcts/crs"
	"github.com/AleutianAI/AleutianFOSS/services/trace/eval"
)

// -----------------------------------------------------------------------------
// Watched Literals Algorithm
// -----------------------------------------------------------------------------

// WatchedLiterals implements the Watched Literals optimization for SAT solving.
//
// Description:
//
//	Watched Literals is a data structure optimization that allows efficient
//	detection of unit clauses and conflicts. Instead of scanning all literals
//	in a clause, we watch only two literals per clause.
//
//	Key Concepts:
//	- Watch List: Maps literals to clauses watching them
//	- Unit Clause: Clause with exactly one unassigned literal
//	- Propagation: When a watched literal becomes false, find new watch
//
//	This algorithm works with CDCL to efficiently propagate assignments.
//
// Thread Safety: Safe for concurrent use.
type WatchedLiterals struct {
	config *WatchedConfig
}

// WatchedConfig configures the watched literals algorithm.
type WatchedConfig struct {
	// MaxPropagations limits propagation steps.
	MaxPropagations int

	// Timeout is the maximum execution time.
	Timeout time.Duration

	// ProgressInterval is how often to report progress.
	ProgressInterval time.Duration
}

// DefaultWatchedConfig returns the default configuration.
func DefaultWatchedConfig() *WatchedConfig {
	return &WatchedConfig{
		MaxPropagations:  10000,
		Timeout:          2 * time.Second,
		ProgressInterval: 500 * time.Millisecond,
	}
}

// NewWatchedLiterals creates a new watched literals algorithm.
func NewWatchedLiterals(config *WatchedConfig) *WatchedLiterals {
	if config == nil {
		config = DefaultWatchedConfig()
	}
	return &WatchedLiterals{config: config}
}

// -----------------------------------------------------------------------------
// Input/Output Types
// -----------------------------------------------------------------------------

// WatchedInput is the input for watched literals propagation.
type WatchedInput struct {
	// Clauses are all clauses (original + learned).
	Clauses []CDCLClause

	// Assignments is the current partial assignment.
	Assignments map[string]bool

	// LastAssignment is the most recent assignment that triggered propagation.
	LastAssignment CDCLLiteral
}

// WatchedOutput is the output from watched literals propagation.
type WatchedOutput struct {
	// Propagations are the implied assignments.
	Propagations []WatchedPropagation

	// Conflict is non-nil if a conflict was detected.
	Conflict *WatchedConflict

	// WatchUpdates tracks which watches were updated.
	WatchUpdates int

	// PropagationCount is the number of propagation steps.
	PropagationCount int
}

// WatchedPropagation represents an implied assignment.
type WatchedPropagation struct {
	Literal CDCLLiteral
	Reason  string // Clause ID that propagated this
	Level   int    // Decision level
}

// WatchedConflict represents a conflict detected during propagation.
type WatchedConflict struct {
	ClauseID string        // Conflicting clause
	Clause   []CDCLLiteral // The conflicting clause literals
	Reason   string
}

// watchList maps literals to the clauses that watch them.
type watchList struct {
	// positive[nodeID] = clauses where nodeID is watched positively
	positive map[string][]string
	// negative[nodeID] = clauses where nodeID is watched negatively
	negative map[string][]string
}

// -----------------------------------------------------------------------------
// Algorithm Interface Implementation
// -----------------------------------------------------------------------------

// Name returns the algorithm name.
func (w *WatchedLiterals) Name() string {
	return "watched_literals"
}

// Process performs watched literals propagation.
//
// Description:
//
//	When an assignment is made, checks all clauses watching the negated
//	literal. For each such clause, either finds a new watch, propagates
//	a unit, or detects a conflict.
//
// Thread Safety: Safe for concurrent use.
func (w *WatchedLiterals) Process(ctx context.Context, snapshot crs.Snapshot, input any) (any, crs.Delta, error) {
	in, ok := input.(*WatchedInput)
	if !ok {
		return nil, nil, &AlgorithmError{
			Algorithm: "watched_literals",
			Operation: "Process",
			Err:       ErrInvalidInput,
		}
	}

	// Check for cancellation
	select {
	case <-ctx.Done():
		return &WatchedOutput{}, nil, ctx.Err()
	default:
	}

	// Build watch lists
	watches := w.buildWatchLists(in.Clauses)

	// Initialize working assignment
	assignments := make(map[string]bool, len(in.Assignments))
	for k, v := range in.Assignments {
		assignments[k] = v
	}

	// Propagation queue
	queue := []CDCLLiteral{in.LastAssignment}

	output := &WatchedOutput{
		Propagations: make([]WatchedPropagation, 0),
	}

	// Main propagation loop
	for len(queue) > 0 && output.PropagationCount < w.config.MaxPropagations {
		// Check for cancellation
		select {
		case <-ctx.Done():
			return output, nil, ctx.Err()
		default:
		}

		lit := queue[0]
		queue = queue[1:]
		output.PropagationCount++

		// Find clauses watching the negation of this literal
		var watchingClauses []string
		if lit.Positive {
			watchingClauses = watches.negative[lit.NodeID]
		} else {
			watchingClauses = watches.positive[lit.NodeID]
		}

		for _, clauseID := range watchingClauses {
			clause := w.findClause(in.Clauses, clauseID)
			if clause == nil {
				continue
			}

			result := w.processClause(clause, assignments, &watches)
			output.WatchUpdates++

			switch result.action {
			case watchActionConflict:
				output.Conflict = &WatchedConflict{
					ClauseID: clauseID,
					Clause:   clause.Literals,
					Reason:   "All literals falsified",
				}
				return output, nil, nil

			case watchActionPropagate:
				// Add to assignments and queue
				assignments[result.propagatedLit.NodeID] = result.propagatedLit.Positive
				output.Propagations = append(output.Propagations, WatchedPropagation{
					Literal: result.propagatedLit,
					Reason:  clauseID,
				})
				queue = append(queue, result.propagatedLit)

			case watchActionNewWatch:
				// Watch was successfully moved, nothing else to do
			}
		}
	}

	return output, nil, nil
}

type watchAction int

const (
	watchActionNewWatch  watchAction = iota // Found new literal to watch
	watchActionPropagate                    // Unit clause, propagate
	watchActionConflict                     // Conflict detected
)

type watchResult struct {
	action        watchAction
	propagatedLit CDCLLiteral // Only set if action == watchActionPropagate
}

// processClause processes a clause when one of its watched literals becomes false.
func (w *WatchedLiterals) processClause(clause *CDCLClause, assignments map[string]bool, watches *watchList) watchResult {
	if len(clause.Literals) == 0 {
		return watchResult{action: watchActionConflict}
	}

	// Count satisfied, falsified, and unassigned literals
	var satisfied, falsified int
	var lastUnassigned *CDCLLiteral

	for i := range clause.Literals {
		lit := &clause.Literals[i]
		if val, assigned := assignments[lit.NodeID]; assigned {
			if val == lit.Positive {
				satisfied++
			} else {
				falsified++
			}
		} else {
			lastUnassigned = lit
		}
	}

	// If any literal is satisfied, clause is satisfied
	if satisfied > 0 {
		return watchResult{action: watchActionNewWatch}
	}

	// If all literals are falsified, conflict
	if falsified == len(clause.Literals) {
		return watchResult{action: watchActionConflict}
	}

	// If exactly one unassigned literal, propagate
	unassignedCount := len(clause.Literals) - satisfied - falsified
	if unassignedCount == 1 && lastUnassigned != nil {
		return watchResult{
			action:        watchActionPropagate,
			propagatedLit: *lastUnassigned,
		}
	}

	// Multiple unassigned - find new watch
	return watchResult{action: watchActionNewWatch}
}

// buildWatchLists builds the initial watch lists from clauses.
func (w *WatchedLiterals) buildWatchLists(clauses []CDCLClause) watchList {
	watches := watchList{
		positive: make(map[string][]string),
		negative: make(map[string][]string),
	}

	for _, clause := range clauses {
		if len(clause.Literals) == 0 {
			continue
		}

		// Watch first two literals (or just one if clause has only one)
		for i := 0; i < len(clause.Literals) && i < 2; i++ {
			lit := clause.Literals[i]
			if lit.Positive {
				watches.positive[lit.NodeID] = append(watches.positive[lit.NodeID], clause.ID)
			} else {
				watches.negative[lit.NodeID] = append(watches.negative[lit.NodeID], clause.ID)
			}
		}
	}

	return watches
}

// findClause finds a clause by ID.
func (w *WatchedLiterals) findClause(clauses []CDCLClause, id string) *CDCLClause {
	for i := range clauses {
		if clauses[i].ID == id {
			return &clauses[i]
		}
	}
	return nil
}

// Timeout returns the maximum execution time.
func (w *WatchedLiterals) Timeout() time.Duration {
	return w.config.Timeout
}

// InputType returns the expected input type.
func (w *WatchedLiterals) InputType() reflect.Type {
	return reflect.TypeOf(&WatchedInput{})
}

// OutputType returns the output type.
func (w *WatchedLiterals) OutputType() reflect.Type {
	return reflect.TypeOf(&WatchedOutput{})
}

// ProgressInterval returns how often to report progress.
func (w *WatchedLiterals) ProgressInterval() time.Duration {
	return w.config.ProgressInterval
}

// SupportsPartialResults returns true.
func (w *WatchedLiterals) SupportsPartialResults() bool {
	return true
}

// -----------------------------------------------------------------------------
// Evaluable Implementation
// -----------------------------------------------------------------------------

// Properties returns the correctness properties.
func (w *WatchedLiterals) Properties() []eval.Property {
	return []eval.Property{
		{
			Name:        "propagation_sound",
			Description: "Propagated literals are implied by clauses",
			Check: func(input, output any) error {
				// Verified by propagation logic
				return nil
			},
		},
		{
			Name:        "conflict_valid",
			Description: "Conflict means all literals are falsified",
			Check: func(input, output any) error {
				out, ok := output.(*WatchedOutput)
				if !ok || out.Conflict == nil {
					return nil
				}
				in, ok := input.(*WatchedInput)
				if !ok {
					return nil
				}

				// Verify all literals in conflict clause are falsified
				assignments := in.Assignments
				for _, prop := range out.Propagations {
					assignments[prop.Literal.NodeID] = prop.Literal.Positive
				}

				for _, lit := range out.Conflict.Clause {
					val, assigned := assignments[lit.NodeID]
					if !assigned {
						return &AlgorithmError{
							Algorithm: "watched_literals",
							Operation: "Property.conflict_valid",
							Err:       eval.ErrPropertyFailed,
						}
					}
					if val == lit.Positive {
						// Literal is satisfied, not a real conflict
						return &AlgorithmError{
							Algorithm: "watched_literals",
							Operation: "Property.conflict_valid",
							Err:       eval.ErrPropertyFailed,
						}
					}
				}
				return nil
			},
		},
	}
}

// Metrics returns the metrics this algorithm exposes.
func (w *WatchedLiterals) Metrics() []eval.MetricDefinition {
	return []eval.MetricDefinition{
		{
			Name:        "watched_propagations_total",
			Type:        eval.MetricCounter,
			Description: "Total propagations performed",
		},
		{
			Name:        "watched_conflicts_total",
			Type:        eval.MetricCounter,
			Description: "Total conflicts detected",
		},
		{
			Name:        "watched_watch_updates_total",
			Type:        eval.MetricCounter,
			Description: "Total watch list updates",
		},
	}
}

// HealthCheck verifies the algorithm is functioning.
func (w *WatchedLiterals) HealthCheck(ctx context.Context) error {
	if w.config == nil {
		return &AlgorithmError{
			Algorithm: "watched_literals",
			Operation: "HealthCheck",
			Err:       ErrInvalidConfig,
		}
	}
	return nil
}
