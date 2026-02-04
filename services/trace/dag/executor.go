// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package dag

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/google/uuid"
)

var (
	tracer = otel.Tracer("aleutian.dag")
	meter  = otel.Meter("aleutian.dag")
)

// Executor runs a DAG with parallelism and observability.
//
// Description:
//
//	Executor manages DAG execution, running independent nodes in parallel,
//	tracking state, and providing observability via OpenTelemetry.
//
// Thread Safety:
//
//	Executor is safe for concurrent use. Multiple DAG executions can run
//	concurrently on the same Executor.
type Executor struct {
	dag    *DAG
	logger *slog.Logger

	// Metrics (initialized lazily)
	metricsOnce     sync.Once
	nodeLatency     metric.Float64Histogram
	nodeSuccesses   metric.Int64Counter
	nodeFailures    metric.Int64Counter
	activeNodes     metric.Int64UpDownCounter
	pipelineLatency metric.Float64Histogram
}

// NewExecutor creates a new DAG executor.
//
// Inputs:
//
//	dag - The DAG to execute. Must not be nil.
//	logger - Logger for execution logs. If nil, uses slog.Default().
//
// Outputs:
//
//	*Executor - The configured executor.
//	error - Non-nil if initialization fails.
func NewExecutor(dag *DAG, logger *slog.Logger) (*Executor, error) {
	if dag == nil {
		return nil, ErrInvalidInput
	}
	if logger == nil {
		logger = slog.Default()
	}

	return &Executor{
		dag:    dag,
		logger: logger,
	}, nil
}

// initMetrics lazily initializes metrics.
// Logs errors if metric creation fails but continues execution (graceful degradation).
func (e *Executor) initMetrics() {
	e.metricsOnce.Do(func() {
		var initErrors []string

		var err error
		e.nodeLatency, err = meter.Float64Histogram("dag_node_duration_seconds",
			metric.WithDescription("Time spent executing each DAG node"),
			metric.WithUnit("s"),
		)
		if err != nil {
			initErrors = append(initErrors, "node_latency: "+err.Error())
		}

		e.nodeSuccesses, err = meter.Int64Counter("dag_node_success_total",
			metric.WithDescription("Number of successful node executions"),
		)
		if err != nil {
			initErrors = append(initErrors, "node_successes: "+err.Error())
		}

		e.nodeFailures, err = meter.Int64Counter("dag_node_failure_total",
			metric.WithDescription("Number of failed node executions"),
		)
		if err != nil {
			initErrors = append(initErrors, "node_failures: "+err.Error())
		}

		e.activeNodes, err = meter.Int64UpDownCounter("dag_active_nodes",
			metric.WithDescription("Number of currently executing nodes"),
		)
		if err != nil {
			initErrors = append(initErrors, "active_nodes: "+err.Error())
		}

		e.pipelineLatency, err = meter.Float64Histogram("dag_pipeline_duration_seconds",
			metric.WithDescription("Total pipeline execution time"),
			metric.WithUnit("s"),
		)
		if err != nil {
			initErrors = append(initErrors, "pipeline_latency: "+err.Error())
		}

		// Log all errors at once at Error level for visibility
		if len(initErrors) > 0 {
			e.logger.Error("failed to initialize some DAG metrics (observability degraded)",
				slog.Int("failed_count", len(initErrors)),
				slog.Any("errors", initErrors),
			)
		}
	})
}

// Run executes the DAG from start to completion.
//
// Description:
//
//	Executes all nodes in the DAG, respecting dependencies and running
//	independent nodes in parallel. Creates a root span for tracing.
//
// Inputs:
//
//	ctx - Context for cancellation. Must not be nil.
//	input - Initial input passed to root nodes (nodes with no dependencies).
//
// Outputs:
//
//	*Result - Execution result including output and timing.
//	error - Non-nil on failure.
func (e *Executor) Run(ctx context.Context, input any) (*Result, error) {
	if ctx == nil {
		return nil, ErrNilContext
	}

	e.initMetrics()

	// Create root span
	ctx, span := tracer.Start(ctx, "dag.Pipeline",
		trace.WithAttributes(
			attribute.String("dag.name", e.dag.Name()),
			attribute.Int("dag.node_count", e.dag.NodeCount()),
		),
	)
	defer span.End()

	start := time.Now()
	sessionID := uuid.NewString()[:12] // 48 bits of entropy

	e.logger.Info("pipeline started",
		slog.String("dag", e.dag.Name()),
		slog.String("session_id", sessionID),
		slog.Int("nodes", e.dag.NodeCount()),
	)

	// Initialize state
	state := NewState(sessionID)
	state.NodeOutputs["root"] = input

	nodeDurations := make(map[string]time.Duration)

	// Execute until all nodes complete or failure
	for !state.IsDAGComplete(e.dag) && !state.IsFailed() {
		select {
		case <-ctx.Done():
			span.RecordError(ctx.Err())
			span.SetStatus(codes.Error, "context canceled")
			return e.buildResult(state, start, nodeDurations, ctx.Err()), ctx.Err()
		default:
		}

		// Find nodes ready to execute (all deps satisfied)
		ready := e.findReadyNodes(state)
		if len(ready) == 0 {
			if state.IsFailed() {
				break
			}
			err := ErrNoProgress
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return e.buildResult(state, start, nodeDurations, err), err
		}

		// Execute ready nodes in parallel
		if err := e.executeParallel(ctx, ready, state, nodeDurations); err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return e.buildResult(state, start, nodeDurations, err), err
		}
	}

	duration := time.Since(start)
	if e.pipelineLatency != nil {
		e.pipelineLatency.Record(ctx, duration.Seconds(),
			metric.WithAttributes(attribute.String("dag", e.dag.Name())),
		)
	}

	result := e.buildResult(state, start, nodeDurations, nil)

	if result.Success {
		span.SetStatus(codes.Ok, "")
		e.logger.Info("pipeline completed",
			slog.String("session_id", sessionID),
			slog.Duration("duration", duration),
			slog.Int("nodes_executed", result.NodesExecuted),
		)
	} else {
		span.SetStatus(codes.Error, result.Error)
		e.logger.Error("pipeline failed",
			slog.String("session_id", sessionID),
			slog.String("failed_node", result.FailedNode),
			slog.String("error", result.Error),
		)
	}

	return result, nil
}

// findReadyNodes returns nodes that are ready to execute.
// A node is ready if all its dependencies have completed.
func (e *Executor) findReadyNodes(state *State) []Node {
	ready := make([]Node, 0)

	for _, name := range e.dag.NodeNames() {
		// Skip already completed
		if state.IsCompleted(name) {
			continue
		}

		// Skip already running
		if state.GetStatus(name) == NodeStatusRunning {
			continue
		}

		// Check all dependencies completed
		deps := e.dag.GetDependencies(name)
		allDepsComplete := true
		for _, dep := range deps {
			if !state.IsCompleted(dep) {
				allDepsComplete = false
				break
			}
		}

		if allDepsComplete {
			node, _ := e.dag.GetNode(name)
			ready = append(ready, node)
		}
	}

	return ready
}

// executeParallel runs multiple nodes concurrently.
func (e *Executor) executeParallel(
	ctx context.Context,
	nodes []Node,
	state *State,
	nodeDurations map[string]time.Duration,
) error {
	var wg sync.WaitGroup
	errCh := make(chan error, len(nodes))
	durationCh := make(chan struct {
		name     string
		duration time.Duration
	}, len(nodes))

	// Update current nodes
	names := make([]string, len(nodes))
	for i, n := range nodes {
		names[i] = n.Name()
	}
	state.SetCurrentNodes(names)

	for _, node := range nodes {
		wg.Add(1)
		go func(n Node) {
			defer wg.Done()

			state.SetStatus(n.Name(), NodeStatusRunning)
			nodeStart := time.Now()

			if err := e.executeNode(ctx, n, state); err != nil {
				errCh <- err
			}

			durationCh <- struct {
				name     string
				duration time.Duration
			}{n.Name(), time.Since(nodeStart)}
		}(node)
	}

	wg.Wait()
	close(errCh)
	close(durationCh)

	// Collect durations
	for d := range durationCh {
		nodeDurations[d.name] = d.duration
	}

	state.SetCurrentNodes(nil)

	// Return first error
	for err := range errCh {
		return err
	}
	return nil
}

// executeNode runs a single node with observability.
func (e *Executor) executeNode(ctx context.Context, node Node, state *State) error {
	// Create child span
	ctx, span := tracer.Start(ctx, node.Name(),
		trace.WithAttributes(
			attribute.String("dag.node", node.Name()),
			attribute.StringSlice("dag.dependencies", node.Dependencies()),
			attribute.String("dag.session_id", state.SessionID),
			attribute.Bool("dag.retryable", node.Retryable()),
		),
	)
	defer span.End()

	// Track active nodes
	if e.activeNodes != nil {
		e.activeNodes.Add(ctx, 1)
		defer e.activeNodes.Add(ctx, -1)
	}

	e.logger.Debug("node starting",
		slog.String("node", node.Name()),
		slog.String("session_id", state.SessionID),
	)

	// Gather inputs from dependencies
	inputs := make(map[string]any)
	for _, dep := range node.Dependencies() {
		output, ok := state.GetOutput(dep)
		if !ok {
			// Use root input if no dependency output
			output, _ = state.GetOutput("root")
		}
		inputs[dep] = output
	}

	// If node has no deps, pass root input
	if len(node.Dependencies()) == 0 {
		rootOutput, _ := state.GetOutput("root")
		inputs["root"] = rootOutput
	}

	// Execute with timeout
	start := time.Now()
	timeout := node.Timeout()
	if timeout == 0 {
		timeout = DefaultNodeTimeout
	}

	nodeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	output, err := node.Execute(nodeCtx, inputs)
	duration := time.Since(start)

	// Record latency metric
	if e.nodeLatency != nil {
		e.nodeLatency.Record(ctx, duration.Seconds(),
			metric.WithAttributes(attribute.String("node", node.Name())),
		)
	}

	if err != nil {
		// Check if it was a timeout
		if nodeCtx.Err() == context.DeadlineExceeded {
			err = fmt.Errorf("%w: %s", ErrNodeTimeout, node.Name())
		}

		if e.nodeFailures != nil {
			e.nodeFailures.Add(ctx, 1,
				metric.WithAttributes(attribute.String("node", node.Name())),
			)
		}
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())

		state.SetFailed(node.Name(), err)

		e.logger.Error("node failed",
			slog.String("node", node.Name()),
			slog.Duration("duration", duration),
			slog.String("error", err.Error()),
		)

		return NewNodeError(node.Name(), err)
	}

	if e.nodeSuccesses != nil {
		e.nodeSuccesses.Add(ctx, 1,
			metric.WithAttributes(attribute.String("node", node.Name())),
		)
	}
	span.SetStatus(codes.Ok, "")

	// Store output and mark complete
	state.SetCompleted(node.Name(), output)

	e.logger.Info("node completed",
		slog.String("node", node.Name()),
		slog.Duration("duration", duration),
	)

	return nil
}

// buildResult constructs the execution result.
func (e *Executor) buildResult(
	state *State,
	start time.Time,
	nodeDurations map[string]time.Duration,
	err error,
) *Result {
	result := &Result{
		SessionID:     state.SessionID,
		Duration:      time.Since(start),
		NodesExecuted: state.CompletedCount(),
		NodeDurations: nodeDurations,
	}

	if err != nil {
		result.Success = false
		result.Error = err.Error()
		result.FailedNode = state.FailedNode
	} else if state.IsFailed() {
		result.Success = false
		result.Error = state.Error
		result.FailedNode = state.FailedNode
	} else {
		result.Success = true
		// Get terminal node output
		if e.dag.Terminal() != "" {
			result.Output, _ = state.GetOutput(e.dag.Terminal())
		}
	}

	return result
}

// RunFromState continues execution from a saved state.
//
// Description:
//
//	Resumes DAG execution from a previously saved state, useful for
//	implementing checkpoint/resume functionality. Before resuming,
//	it calls OnResume on all completed RehydratableNodes to restore
//	any ephemeral state (e.g., respawn LSP processes).
//
// Inputs:
//
//	ctx - Context for cancellation.
//	state - Previously saved state to resume from.
//
// Outputs:
//
//	*Result - Execution result.
//	error - Non-nil on failure.
func (e *Executor) RunFromState(ctx context.Context, state *State) (*Result, error) {
	if ctx == nil {
		return nil, ErrNilContext
	}
	if state == nil {
		return nil, ErrInvalidInput
	}

	e.initMetrics()

	ctx, span := tracer.Start(ctx, "dag.Pipeline.Resume",
		trace.WithAttributes(
			attribute.String("dag.name", e.dag.Name()),
			attribute.String("dag.session_id", state.SessionID),
			attribute.Int("dag.completed_nodes", state.CompletedCount()),
		),
	)
	defer span.End()

	start := time.Now()

	// Clear any previous failure to retry
	state.mu.Lock()
	state.FailedNode = ""
	state.Error = ""
	state.mu.Unlock()

	e.logger.Info("pipeline resuming",
		slog.String("dag", e.dag.Name()),
		slog.String("session_id", state.SessionID),
		slog.Int("completed_nodes", state.CompletedCount()),
	)

	// Rehydrate completed nodes that have ephemeral state
	if err := e.rehydrateNodes(ctx, state); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "rehydration failed")
		return nil, fmt.Errorf("rehydrating nodes: %w", err)
	}

	nodeDurations := make(map[string]time.Duration)

	// Execute remaining nodes
	for !state.IsDAGComplete(e.dag) && !state.IsFailed() {
		select {
		case <-ctx.Done():
			span.RecordError(ctx.Err())
			return e.buildResult(state, start, nodeDurations, ctx.Err()), ctx.Err()
		default:
		}

		ready := e.findReadyNodes(state)
		if len(ready) == 0 {
			if state.IsFailed() {
				break
			}
			err := ErrNoProgress
			span.RecordError(err)
			return e.buildResult(state, start, nodeDurations, err), err
		}

		if err := e.executeParallel(ctx, ready, state, nodeDurations); err != nil {
			span.RecordError(err)
			return e.buildResult(state, start, nodeDurations, err), err
		}
	}

	result := e.buildResult(state, start, nodeDurations, nil)

	if result.Success {
		span.SetStatus(codes.Ok, "")
	} else {
		span.SetStatus(codes.Error, result.Error)
	}

	return result, nil
}

// rehydrateNodes restores ephemeral state for completed RehydratableNodes.
//
// Description:
//
//	Iterates through all completed nodes and calls OnResume on those that
//	implement RehydratableNode. If rehydration fails, the node is marked
//	as pending so it will be re-executed.
//
// The Zombie Problem This Solves:
//
//	After checkpoint restore, a node marked "complete" may have lost its
//	ephemeral resources (e.g., LSP process died). Without rehydration:
//	  1. Checkpoint says LSP_SPAWN = Complete
//	  2. TYPE_CHECK runs, calls LSP
//	  3. PANIC: LSP manager is nil
//
//	With rehydration:
//	  1. Checkpoint says LSP_SPAWN = Complete
//	  2. rehydrateNodes calls LSPNode.OnResume()
//	  3. OnResume sees process is dead, respawns it
//	  4. TYPE_CHECK runs successfully
//
// Inputs:
//
//	ctx - Context for cancellation.
//	state - The restored state with completed nodes.
//
// Outputs:
//
//	error - Non-nil if any critical rehydration fails.
//
// Thread Safety:
//
//	Safe for concurrent use.
func (e *Executor) rehydrateNodes(ctx context.Context, state *State) error {
	ctx, span := tracer.Start(ctx, "dag.rehydrateNodes",
		trace.WithAttributes(
			attribute.String("dag.name", e.dag.Name()),
			attribute.String("dag.session_id", state.SessionID),
		),
	)
	defer span.End()

	state.mu.RLock()
	completedNames := make([]string, 0, len(state.CompletedNodes))
	for name := range state.CompletedNodes {
		completedNames = append(completedNames, name)
	}
	state.mu.RUnlock()

	rehydrated := 0
	failed := 0

	for _, name := range completedNames {
		node, ok := e.dag.GetNode(name)
		if !ok {
			continue
		}

		// Check if node implements RehydratableNode
		rehydratable, ok := node.(RehydratableNode)
		if !ok {
			continue
		}

		// Get the node's output for rehydration
		output, _ := state.GetOutput(name)

		e.logger.Debug("rehydrating node",
			slog.String("node", name),
		)

		// Call OnResume to restore ephemeral state
		if err := rehydratable.OnResume(ctx, output); err != nil {
			e.logger.Warn("node rehydration failed, will re-execute",
				slog.String("node", name),
				slog.String("error", err.Error()),
			)

			// Mark node as not completed so it will be re-executed
			state.mu.Lock()
			delete(state.CompletedNodes, name)
			state.NodeStatuses[name] = NodeStatusPending
			state.mu.Unlock()

			span.AddEvent("rehydration_failed", trace.WithAttributes(
				attribute.String("node", name),
				attribute.String("error", err.Error()),
			))

			failed++
		} else {
			rehydrated++
		}
	}

	span.SetAttributes(
		attribute.Int("rehydrated_count", rehydrated),
		attribute.Int("failed_count", failed),
	)

	e.logger.Info("node rehydration complete",
		slog.Int("rehydrated", rehydrated),
		slog.Int("failed_will_rerun", failed),
	)

	return nil
}
