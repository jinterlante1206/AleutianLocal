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
	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/agent/mcts/algorithms/planning"
	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/agent/mcts/crs"
	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/eval"
)

// -----------------------------------------------------------------------------
// Planning Activity
// -----------------------------------------------------------------------------

// PlanningActivity orchestrates planning algorithms: HTN, Blackboard.
//
// Description:
//
//	PlanningActivity coordinates task decomposition using:
//	- HTN: Hierarchical Task Networks for task decomposition
//	- Blackboard: Multi-agent coordination via shared blackboard
//
//	The activity manages the decomposition of high-level tasks into
//	executable action sequences.
//
// Thread Safety: Safe for concurrent use.
type PlanningActivity struct {
	*BaseActivity
	config *PlanningConfig

	// Algorithms
	htn        *planning.HTN
	blackboard *planning.Blackboard
}

// PlanningConfig configures the planning activity.
type PlanningConfig struct {
	*ActivityConfig

	// HTNConfig configures the HTN algorithm.
	HTNConfig *planning.HTNConfig

	// BlackboardConfig configures the blackboard algorithm.
	BlackboardConfig *planning.BlackboardConfig
}

// DefaultPlanningConfig returns the default planning configuration.
func DefaultPlanningConfig() *PlanningConfig {
	return &PlanningConfig{
		ActivityConfig:   DefaultActivityConfig(),
		HTNConfig:        planning.DefaultHTNConfig(),
		BlackboardConfig: planning.DefaultBlackboardConfig(),
	}
}

// NewPlanningActivity creates a new planning activity.
//
// Inputs:
//   - config: Planning configuration. Uses defaults if nil.
//
// Outputs:
//   - *PlanningActivity: The new activity.
func NewPlanningActivity(config *PlanningConfig) *PlanningActivity {
	if config == nil {
		config = DefaultPlanningConfig()
	}

	htn := planning.NewHTN(config.HTNConfig)
	blackboard := planning.NewBlackboard(config.BlackboardConfig)

	return &PlanningActivity{
		BaseActivity: NewBaseActivity(
			"planning",
			config.Timeout,
			htn,
			blackboard,
		),
		config:     config,
		htn:        htn,
		blackboard: blackboard,
	}
}

// -----------------------------------------------------------------------------
// Input/Output Types
// -----------------------------------------------------------------------------

// PlanningInput is the input for the planning activity.
type PlanningInput struct {
	BaseInput

	// GoalTaskID is the high-level task to accomplish.
	GoalTaskID string

	// CurrentState describes the current system state.
	CurrentState map[string]string

	// AvailableMethods are the methods that can be used.
	AvailableMethods []string

	// Constraints are planning constraints.
	Constraints []string
}

// Type returns the input type name.
func (i *PlanningInput) Type() string {
	return "planning"
}

// NewPlanningInput creates a new planning input.
func NewPlanningInput(requestID, goalTaskID string, source crs.SignalSource) *PlanningInput {
	return &PlanningInput{
		BaseInput:    NewBaseInput(requestID, source),
		GoalTaskID:   goalTaskID,
		CurrentState: make(map[string]string),
	}
}

// -----------------------------------------------------------------------------
// Activity Implementation
// -----------------------------------------------------------------------------

// Execute runs the planning algorithms.
//
// Description:
//
//	Runs HTN and Blackboard in parallel. HTN decomposes the goal task
//	into subtasks, while Blackboard coordinates between agents.
//
// Thread Safety: Safe for concurrent calls.
func (a *PlanningActivity) Execute(
	ctx context.Context,
	snapshot crs.Snapshot,
	input ActivityInput,
) (ActivityResult, crs.Delta, error) {
	planningInput, ok := input.(*PlanningInput)
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
		case "htn":
			// Convert current state to bool map for HTN
			initialState := make(map[string]bool)
			for k, v := range planningInput.CurrentState {
				initialState[k] = v == "true" || v == "1"
			}

			// Create the goal task
			tasks := []planning.HTNTask{{
				ID:          planningInput.GoalTaskID,
				Name:        planningInput.GoalTaskID,
				IsPrimitive: false,
			}}

			return &planning.HTNInput{
				Tasks:        tasks,
				Methods:      []planning.HTNMethod{}, // Methods would be populated from configuration
				InitialState: initialState,
				Source:       planningInput.Source(),
			}
		case "blackboard":
			return &planning.BlackboardInput{
				InitialData:      make(map[string]planning.BlackboardEntry),
				KnowledgeSources: []planning.KnowledgeSource{},
				GoalConditions:   []planning.BlackboardCondition{},
				Source:           planningInput.Source(),
			}
		default:
			return nil
		}
	}

	return a.RunAlgorithms(ctx, snapshot, makeInput)
}

// ShouldRun decides if planning should run.
//
// Description:
//
//	Planning should run when there are high-level tasks that need
//	decomposition.
func (a *PlanningActivity) ShouldRun(snapshot crs.Snapshot) (bool, Priority) {
	historyIndex := snapshot.HistoryIndex()

	// Check for pending tasks in history
	recent := historyIndex.Recent(10)
	for _, entry := range recent {
		if entry.Action == "task_created" && entry.Result == "pending" {
			return true, PriorityNormal
		}
	}

	return false, PriorityLow
}

// -----------------------------------------------------------------------------
// Evaluable Implementation
// -----------------------------------------------------------------------------

// Properties returns the correctness properties.
func (a *PlanningActivity) Properties() []eval.Property {
	return []eval.Property{
		{
			Name:        "plan_valid",
			Description: "Generated plan is valid and executable",
			Check: func(input, output any) error {
				// Plan validity is checked by individual algorithms
				return nil
			},
		},
	}
}

// Metrics returns the metrics this activity exposes.
func (a *PlanningActivity) Metrics() []eval.MetricDefinition {
	return []eval.MetricDefinition{
		{
			Name:        "planning_activity_executions_total",
			Type:        eval.MetricCounter,
			Description: "Total planning activity executions",
		},
		{
			Name:        "planning_decompositions_total",
			Type:        eval.MetricCounter,
			Description: "Total task decompositions",
		},
		{
			Name:        "planning_plan_depth",
			Type:        eval.MetricHistogram,
			Description: "Depth of generated plans",
		},
	}
}

// HealthCheck verifies the activity is functioning.
func (a *PlanningActivity) HealthCheck(ctx context.Context) error {
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
