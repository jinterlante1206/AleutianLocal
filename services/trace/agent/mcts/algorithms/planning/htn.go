// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package planning

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
	ErrNoPlan        = errors.New("no valid plan found")
	ErrCycleDetected = errors.New("cycle detected in task decomposition")
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
// Hierarchical Task Network (HTN) Algorithm
// -----------------------------------------------------------------------------

// HTN implements Hierarchical Task Network planning.
//
// Description:
//
//	HTN planning decomposes high-level compound tasks into primitive actions
//	using decomposition methods. Each method specifies how a compound task
//	can be broken down into subtasks.
//
//	Key Concepts:
//	- Primitive Task: An atomic action that can be executed directly
//	- Compound Task: A high-level task that must be decomposed
//	- Method: A recipe for decomposing a compound task into subtasks
//	- Precondition: State conditions that must hold for a method
//	- Plan: An ordered sequence of primitive tasks
//
//	Algorithm:
//	1. Start with goal tasks
//	2. For each compound task, find applicable methods
//	3. Apply preconditions to filter methods
//	4. Decompose using the first applicable method
//	5. Repeat until only primitives remain
//
// Thread Safety: Safe for concurrent use.
type HTN struct {
	config *HTNConfig
}

// HTNConfig configures the HTN algorithm.
type HTNConfig struct {
	// MaxDepth limits decomposition depth to prevent infinite recursion.
	MaxDepth int

	// MaxPlanLength limits the maximum plan length.
	MaxPlanLength int

	// Timeout is the maximum execution time.
	Timeout time.Duration

	// ProgressInterval is how often to report progress.
	ProgressInterval time.Duration
}

// DefaultHTNConfig returns the default configuration.
func DefaultHTNConfig() *HTNConfig {
	return &HTNConfig{
		MaxDepth:         20,
		MaxPlanLength:    100,
		Timeout:          5 * time.Second,
		ProgressInterval: 1 * time.Second,
	}
}

// NewHTN creates a new HTN algorithm.
func NewHTN(config *HTNConfig) *HTN {
	if config == nil {
		config = DefaultHTNConfig()
	}
	return &HTN{config: config}
}

// -----------------------------------------------------------------------------
// Input/Output Types
// -----------------------------------------------------------------------------

// HTNInput is the input for HTN planning.
type HTNInput struct {
	// Tasks are the top-level goal tasks to achieve.
	Tasks []HTNTask

	// Methods are the decomposition methods available.
	Methods []HTNMethod

	// InitialState is the starting world state.
	InitialState map[string]bool

	// Source indicates where the planning request originated.
	Source crs.SignalSource
}

// HTNTask represents a task in the HTN.
type HTNTask struct {
	// ID uniquely identifies this task.
	ID string

	// Name is the task type (e.g., "build_project", "run_tests").
	Name string

	// Parameters are task-specific parameters.
	Parameters map[string]string

	// IsPrimitive indicates if this is a directly executable action.
	IsPrimitive bool
}

// HTNMethod represents a decomposition method.
type HTNMethod struct {
	// ID uniquely identifies this method.
	ID string

	// TaskName is the compound task this method decomposes.
	TaskName string

	// Preconditions are state predicates that must be true.
	Preconditions []HTNPrecondition

	// Subtasks are the tasks this method produces.
	Subtasks []HTNTask

	// Priority determines method ordering (higher = prefer).
	Priority int
}

// HTNPrecondition represents a precondition for a method.
type HTNPrecondition struct {
	// Predicate is the state variable name.
	Predicate string

	// Value is the required value (true/false).
	Value bool
}

// HTNOutput is the output from HTN planning.
type HTNOutput struct {
	// Plan is the sequence of primitive tasks.
	Plan []HTNTask

	// Success indicates if planning succeeded.
	Success bool

	// DecompositionTree shows how tasks were decomposed.
	DecompositionTree []HTNDecomposition

	// FinalState is the predicted state after plan execution.
	FinalState map[string]bool

	// DepthReached is the maximum decomposition depth reached.
	DepthReached int

	// MethodsConsidered counts methods evaluated.
	MethodsConsidered int

	// FailureReason explains why planning failed (if applicable).
	FailureReason string
}

// HTNDecomposition records a decomposition step.
type HTNDecomposition struct {
	TaskID   string   // The compound task that was decomposed
	MethodID string   // The method used
	Subtasks []string // IDs of resulting subtasks
	Depth    int      // Decomposition depth
}

// -----------------------------------------------------------------------------
// Algorithm Interface Implementation
// -----------------------------------------------------------------------------

// Name returns the algorithm name.
func (h *HTN) Name() string {
	return "htn"
}

// Process performs HTN planning.
//
// Description:
//
//	Decomposes compound tasks into primitive actions using hierarchical
//	decomposition methods. Returns a plan if one exists.
//
// Thread Safety: Safe for concurrent use.
func (h *HTN) Process(ctx context.Context, snapshot crs.Snapshot, input any) (any, crs.Delta, error) {
	in, ok := input.(*HTNInput)
	if !ok {
		return nil, nil, &AlgorithmError{
			Algorithm: "htn",
			Operation: "Process",
			Err:       ErrInvalidInput,
		}
	}

	// Check for cancellation
	select {
	case <-ctx.Done():
		return &HTNOutput{Success: false, FailureReason: "cancelled"}, nil, ctx.Err()
	default:
	}

	output := &HTNOutput{
		Plan:              make([]HTNTask, 0),
		DecompositionTree: make([]HTNDecomposition, 0),
		FinalState:        make(map[string]bool),
		Success:           false,
	}

	// Copy initial state
	state := make(map[string]bool, len(in.InitialState))
	for k, v := range in.InitialState {
		state[k] = v
	}

	// Build method index by task name
	methodsByTask := make(map[string][]HTNMethod)
	for _, m := range in.Methods {
		methodsByTask[m.TaskName] = append(methodsByTask[m.TaskName], m)
	}

	// Sort methods by priority (descending)
	for taskName := range methodsByTask {
		methods := methodsByTask[taskName]
		sortMethodsByPriority(methods)
		methodsByTask[taskName] = methods
	}

	// Create task queue from input tasks
	taskQueue := make([]htnTaskWithDepth, 0, len(in.Tasks))
	for _, t := range in.Tasks {
		taskQueue = append(taskQueue, htnTaskWithDepth{task: t, depth: 0})
	}

	// Track visited to detect cycles
	visited := make(map[string]bool)

	// Process tasks
	for len(taskQueue) > 0 {
		// Check for cancellation
		select {
		case <-ctx.Done():
			output.FailureReason = "cancelled"
			return output, nil, ctx.Err()
		default:
		}

		// Check plan length limit
		if len(output.Plan) >= h.config.MaxPlanLength {
			output.FailureReason = "max plan length exceeded"
			return output, nil, nil
		}

		// Dequeue first task
		current := taskQueue[0]
		taskQueue = taskQueue[1:]

		if current.depth > output.DepthReached {
			output.DepthReached = current.depth
		}

		// Check depth limit
		if current.depth > h.config.MaxDepth {
			output.FailureReason = "max depth exceeded"
			return output, nil, nil
		}

		// If primitive, add to plan
		if current.task.IsPrimitive {
			output.Plan = append(output.Plan, current.task)
			continue
		}

		// Cycle detection
		if visited[current.task.ID] {
			output.FailureReason = "cycle detected: " + current.task.ID
			return output, nil, nil
		}
		visited[current.task.ID] = true

		// Find applicable methods
		methods := methodsByTask[current.task.Name]
		if len(methods) == 0 {
			output.FailureReason = "no methods for task: " + current.task.Name
			return output, nil, nil
		}

		// Find first applicable method
		var appliedMethod *HTNMethod
		for i := range methods {
			m := &methods[i]
			output.MethodsConsidered++

			if h.checkPreconditions(m.Preconditions, state) {
				appliedMethod = m
				break
			}
		}

		if appliedMethod == nil {
			output.FailureReason = "no applicable method for: " + current.task.Name
			return output, nil, nil
		}

		// Record decomposition
		subtaskIDs := make([]string, len(appliedMethod.Subtasks))
		for i, st := range appliedMethod.Subtasks {
			subtaskIDs[i] = st.ID
		}
		output.DecompositionTree = append(output.DecompositionTree, HTNDecomposition{
			TaskID:   current.task.ID,
			MethodID: appliedMethod.ID,
			Subtasks: subtaskIDs,
			Depth:    current.depth,
		})

		// Add subtasks to front of queue (depth-first)
		newQueue := make([]htnTaskWithDepth, 0, len(appliedMethod.Subtasks)+len(taskQueue))
		for _, st := range appliedMethod.Subtasks {
			newQueue = append(newQueue, htnTaskWithDepth{
				task:  st,
				depth: current.depth + 1,
			})
		}
		newQueue = append(newQueue, taskQueue...)
		taskQueue = newQueue

		// Clear visited for this path (allows same task in different branches)
		delete(visited, current.task.ID)
	}

	output.Success = true
	output.FinalState = state

	return output, nil, nil
}

// htnTaskWithDepth tracks task depth during decomposition.
type htnTaskWithDepth struct {
	task  HTNTask
	depth int
}

// checkPreconditions verifies all preconditions are met.
func (h *HTN) checkPreconditions(preconditions []HTNPrecondition, state map[string]bool) bool {
	for _, p := range preconditions {
		if stateValue, exists := state[p.Predicate]; !exists || stateValue != p.Value {
			return false
		}
	}
	return true
}

// sortMethodsByPriority sorts methods by priority (descending).
func sortMethodsByPriority(methods []HTNMethod) {
	for i := 0; i < len(methods)-1; i++ {
		for j := i + 1; j < len(methods); j++ {
			if methods[j].Priority > methods[i].Priority {
				methods[i], methods[j] = methods[j], methods[i]
			}
		}
	}
}

// Timeout returns the maximum execution time.
func (h *HTN) Timeout() time.Duration {
	return h.config.Timeout
}

// InputType returns the expected input type.
func (h *HTN) InputType() reflect.Type {
	return reflect.TypeOf(&HTNInput{})
}

// OutputType returns the output type.
func (h *HTN) OutputType() reflect.Type {
	return reflect.TypeOf(&HTNOutput{})
}

// ProgressInterval returns how often to report progress.
func (h *HTN) ProgressInterval() time.Duration {
	return h.config.ProgressInterval
}

// SupportsPartialResults returns true.
func (h *HTN) SupportsPartialResults() bool {
	return true
}

// -----------------------------------------------------------------------------
// Evaluable Implementation
// -----------------------------------------------------------------------------

// Properties returns the correctness properties.
func (h *HTN) Properties() []eval.Property {
	return []eval.Property{
		{
			Name:        "plan_only_primitives",
			Description: "Final plan contains only primitive tasks",
			Check: func(input, output any) error {
				out, ok := output.(*HTNOutput)
				if !ok || !out.Success {
					return nil
				}

				for _, task := range out.Plan {
					if !task.IsPrimitive {
						return &AlgorithmError{
							Algorithm: "htn",
							Operation: "Property.plan_only_primitives",
							Err:       eval.ErrPropertyFailed,
						}
					}
				}
				return nil
			},
		},
		{
			Name:        "decomposition_valid",
			Description: "Each decomposition uses a valid method for the task",
			Check: func(input, output any) error {
				in, okIn := input.(*HTNInput)
				out, okOut := output.(*HTNOutput)
				if !okIn || !okOut || !out.Success {
					return nil
				}

				// Build task map
				taskMap := make(map[string]HTNTask)
				for _, t := range in.Tasks {
					taskMap[t.ID] = t
				}

				// Build method map
				methodsByTask := make(map[string]map[string]bool)
				for _, m := range in.Methods {
					if methodsByTask[m.TaskName] == nil {
						methodsByTask[m.TaskName] = make(map[string]bool)
					}
					methodsByTask[m.TaskName][m.ID] = true
				}

				// Verify each decomposition
				for _, d := range out.DecompositionTree {
					task, exists := taskMap[d.TaskID]
					if !exists {
						continue
					}

					methods := methodsByTask[task.Name]
					if !methods[d.MethodID] {
						return &AlgorithmError{
							Algorithm: "htn",
							Operation: "Property.decomposition_valid",
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
func (h *HTN) Metrics() []eval.MetricDefinition {
	return []eval.MetricDefinition{
		{
			Name:        "htn_plans_generated_total",
			Type:        eval.MetricCounter,
			Description: "Total plans successfully generated",
		},
		{
			Name:        "htn_plan_length",
			Type:        eval.MetricHistogram,
			Description: "Distribution of plan lengths",
			Buckets:     []float64{1, 5, 10, 20, 50, 100},
		},
		{
			Name:        "htn_depth_reached",
			Type:        eval.MetricHistogram,
			Description: "Distribution of decomposition depths",
			Buckets:     []float64{1, 3, 5, 10, 15, 20},
		},
		{
			Name:        "htn_methods_considered_total",
			Type:        eval.MetricCounter,
			Description: "Total methods evaluated during planning",
		},
	}
}

// HealthCheck verifies the algorithm is functioning.
func (h *HTN) HealthCheck(ctx context.Context) error {
	if h.config == nil {
		return &AlgorithmError{
			Algorithm: "htn",
			Operation: "HealthCheck",
			Err:       ErrInvalidConfig,
		}
	}
	if h.config.MaxDepth <= 0 {
		return &AlgorithmError{
			Algorithm: "htn",
			Operation: "HealthCheck",
			Err:       errors.New("max depth must be positive"),
		}
	}
	return nil
}
