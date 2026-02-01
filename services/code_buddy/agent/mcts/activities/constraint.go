// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package activities

import (
	"context"

	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/agent/mcts/algorithms"
	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/agent/mcts/algorithms/constraints"
	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/agent/mcts/crs"
	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/eval"
)

// -----------------------------------------------------------------------------
// Constraint Activity
// -----------------------------------------------------------------------------

// ConstraintActivity orchestrates constraint algorithms: TMS, AC-3, SemanticBackprop.
//
// Description:
//
//	ConstraintActivity coordinates constraint management using:
//	- TMS: Truth Maintenance System for belief revision
//	- AC-3: Arc Consistency for constraint propagation
//	- SemanticBackprop: Error attribution for debugging
//
//	The activity maintains consistency of constraints and propagates
//	updates when beliefs change.
//
// Thread Safety: Safe for concurrent use.
type ConstraintActivity struct {
	*BaseActivity
	config *ConstraintConfig

	// Algorithms
	tms              *constraints.TMS
	ac3              *constraints.AC3
	semanticBackprop *constraints.SemanticBackprop
}

// ConstraintConfig configures the constraint activity.
type ConstraintConfig struct {
	*ActivityConfig

	// TMSConfig configures the TMS algorithm.
	TMSConfig *constraints.TMSConfig

	// AC3Config configures the AC-3 algorithm.
	AC3Config *constraints.AC3Config

	// SemanticBackpropConfig configures the semantic backprop algorithm.
	SemanticBackpropConfig *constraints.SemanticBackpropConfig
}

// DefaultConstraintConfig returns the default constraint configuration.
func DefaultConstraintConfig() *ConstraintConfig {
	return &ConstraintConfig{
		ActivityConfig:         DefaultActivityConfig(),
		TMSConfig:              constraints.DefaultTMSConfig(),
		AC3Config:              constraints.DefaultAC3Config(),
		SemanticBackpropConfig: constraints.DefaultSemanticBackpropConfig(),
	}
}

// NewConstraintActivity creates a new constraint activity.
//
// Inputs:
//   - config: Constraint configuration. Uses defaults if nil.
//
// Outputs:
//   - *ConstraintActivity: The new activity.
func NewConstraintActivity(config *ConstraintConfig) *ConstraintActivity {
	if config == nil {
		config = DefaultConstraintConfig()
	}

	tms := constraints.NewTMS(config.TMSConfig)
	ac3 := constraints.NewAC3(config.AC3Config)
	semanticBackprop := constraints.NewSemanticBackprop(config.SemanticBackpropConfig)

	return &ConstraintActivity{
		BaseActivity: NewBaseActivity(
			"constraint",
			config.Timeout,
			tms,
			ac3,
			semanticBackprop,
		),
		config:           config,
		tms:              tms,
		ac3:              ac3,
		semanticBackprop: semanticBackprop,
	}
}

// -----------------------------------------------------------------------------
// Input/Output Types
// -----------------------------------------------------------------------------

// ConstraintInput is the input for the constraint activity.
type ConstraintInput struct {
	BaseInput

	// Operation is the constraint operation to perform.
	// Options: "propagate", "revise", "attribute"
	Operation string

	// ConstraintIDs are the constraints to process.
	ConstraintIDs []string

	// BeliefChanges are beliefs that have changed.
	BeliefChanges map[string]bool

	// ErrorNodeID is the node with the error (for attribution).
	ErrorNodeID string

	// ErrorMessage is the error message (for attribution).
	ErrorMessage string
}

// Type returns the input type name.
func (i *ConstraintInput) Type() string {
	return "constraint"
}

// NewConstraintInput creates a new constraint input.
func NewConstraintInput(requestID, operation string, source crs.SignalSource) *ConstraintInput {
	return &ConstraintInput{
		BaseInput:     NewBaseInput(requestID, source),
		Operation:     operation,
		BeliefChanges: make(map[string]bool),
	}
}

// -----------------------------------------------------------------------------
// Activity Implementation
// -----------------------------------------------------------------------------

// Execute runs the constraint algorithms.
//
// Description:
//
//	Runs TMS, AC-3, and SemanticBackprop based on the operation type.
//	- "propagate": Run AC-3 for constraint propagation
//	- "revise": Run TMS for belief revision
//	- "attribute": Run SemanticBackprop for error attribution
//
// Thread Safety: Safe for concurrent calls.
func (a *ConstraintActivity) Execute(
	ctx context.Context,
	snapshot crs.Snapshot,
	input ActivityInput,
) (ActivityResult, crs.Delta, error) {
	constraintInput, ok := input.(*ConstraintInput)
	if !ok {
		return ActivityResult{}, nil, &ActivityError{
			Activity:  a.Name(),
			Operation: "Execute",
			Err:       ErrNilInput,
		}
	}

	// Create algorithm-specific inputs
	makeInput := func(algo algorithms.Algorithm) any {
		switch algo.Name() {
		case "tms":
			return &constraints.TMSInput{}
		case "ac3":
			return &constraints.AC3Input{}
		case "semantic_backprop":
			errorNodes := []constraints.ErrorNode{}
			if constraintInput.ErrorNodeID != "" {
				errorNodes = append(errorNodes, constraints.ErrorNode{
					NodeID:  constraintInput.ErrorNodeID,
					Message: constraintInput.ErrorMessage,
				})
			}
			return &constraints.SemanticBackpropInput{
				ErrorNodes: errorNodes,
			}
		default:
			return nil
		}
	}

	return a.RunAlgorithms(ctx, snapshot, makeInput)
}

// ShouldRun decides if constraint activity should run.
//
// Description:
//
//	Constraint activity should run when there are active constraints
//	that may need propagation or revision.
func (a *ConstraintActivity) ShouldRun(snapshot crs.Snapshot) (bool, Priority) {
	constraintIndex := snapshot.ConstraintIndex()

	// Check if there are active constraints
	if constraintIndex.Size() > 0 {
		return true, PriorityNormal
	}

	return false, PriorityLow
}

// -----------------------------------------------------------------------------
// Evaluable Implementation
// -----------------------------------------------------------------------------

// Properties returns the correctness properties.
func (a *ConstraintActivity) Properties() []eval.Property {
	return []eval.Property{
		{
			Name:        "constraint_consistency",
			Description: "Constraints remain consistent after propagation",
			Check: func(input, output any) error {
				// Constraint consistency is validated by individual algorithms
				return nil
			},
		},
	}
}

// Metrics returns the metrics this activity exposes.
func (a *ConstraintActivity) Metrics() []eval.MetricDefinition {
	return []eval.MetricDefinition{
		{
			Name:        "constraint_activity_executions_total",
			Type:        eval.MetricCounter,
			Description: "Total constraint activity executions",
		},
		{
			Name:        "constraint_propagations_total",
			Type:        eval.MetricCounter,
			Description: "Total constraint propagations",
		},
		{
			Name:        "constraint_revisions_total",
			Type:        eval.MetricCounter,
			Description: "Total belief revisions",
		},
	}
}

// HealthCheck verifies the activity is functioning.
func (a *ConstraintActivity) HealthCheck(ctx context.Context) error {
	if a.config == nil {
		return &ActivityError{
			Activity:  a.Name(),
			Operation: "HealthCheck",
			Err:       ErrInvalidConfig,
		}
	}

	for _, algo := range a.Algorithms() {
		if err := algo.HealthCheck(ctx); err != nil {
			return &ActivityError{
				Activity:  a.Name(),
				Operation: "HealthCheck",
				Err:       err,
			}
		}
	}

	return nil
}
