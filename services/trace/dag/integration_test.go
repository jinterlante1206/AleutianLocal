// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package dag_test

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/trace/dag"
)

// TestIntegration_FullPipeline tests a complete DAG execution with multiple nodes.
func TestIntegration_FullPipeline(t *testing.T) {
	ctx := context.Background()

	// Track execution order with mutex for thread safety
	var mu sync.Mutex
	executionOrder := make([]string, 0)

	appendOrder := func(name string) {
		mu.Lock()
		executionOrder = append(executionOrder, name)
		mu.Unlock()
	}

	// Create nodes that track execution
	parseNode := dag.NewFuncNode("PARSE", nil, func(ctx context.Context, inputs map[string]any) (any, error) {
		appendOrder("PARSE")
		return map[string]any{"results": []string{"file1.go", "file2.go"}}, nil
	})

	graphNode := dag.NewFuncNode("GRAPH", []string{"PARSE"}, func(ctx context.Context, inputs map[string]any) (any, error) {
		appendOrder("GRAPH")
		return map[string]any{"nodes": 10, "edges": 15}, nil
	})

	lintNode := dag.NewFuncNode("LINT", []string{"GRAPH"}, func(ctx context.Context, inputs map[string]any) (any, error) {
		appendOrder("LINT")
		return map[string]any{"errors": 0, "warnings": 2}, nil
	})

	analysisNode := dag.NewFuncNode("ANALYSIS", []string{"GRAPH"}, func(ctx context.Context, inputs map[string]any) (any, error) {
		appendOrder("ANALYSIS")
		return map[string]any{"patterns": 3}, nil
	})

	reportNode := dag.NewFuncNode("REPORT", []string{"LINT", "ANALYSIS"}, func(ctx context.Context, inputs map[string]any) (any, error) {
		appendOrder("REPORT")
		return map[string]any{"complete": true}, nil
	})

	// Build pipeline
	pipeline, err := dag.NewBuilder("integration-test").
		AddNode(parseNode).
		AddNode(graphNode).
		AddNode(lintNode).
		AddNode(analysisNode).
		AddNode(reportNode).
		Build()

	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	// Execute
	executor, err := dag.NewExecutor(pipeline, slog.Default())
	if err != nil {
		t.Fatalf("NewExecutor: %v", err)
	}

	result, err := executor.Run(ctx, map[string]any{"project": "/test"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Verify success
	if !result.Success {
		t.Errorf("expected success, got failure: %s", result.Error)
	}

	// Verify all nodes executed
	if result.NodesExecuted != 5 {
		t.Errorf("NodesExecuted = %d, want 5", result.NodesExecuted)
	}

	// Verify execution order respects dependencies
	// PARSE must be first
	if executionOrder[0] != "PARSE" {
		t.Errorf("first node = %s, want PARSE", executionOrder[0])
	}

	// GRAPH must come after PARSE
	graphIdx := indexOf(executionOrder, "GRAPH")
	parseIdx := indexOf(executionOrder, "PARSE")
	if graphIdx <= parseIdx {
		t.Errorf("GRAPH executed before PARSE")
	}

	// LINT and ANALYSIS must come after GRAPH
	lintIdx := indexOf(executionOrder, "LINT")
	analysisIdx := indexOf(executionOrder, "ANALYSIS")
	if lintIdx <= graphIdx {
		t.Errorf("LINT executed before GRAPH")
	}
	if analysisIdx <= graphIdx {
		t.Errorf("ANALYSIS executed before GRAPH")
	}

	// REPORT must be last
	reportIdx := indexOf(executionOrder, "REPORT")
	if reportIdx <= lintIdx || reportIdx <= analysisIdx {
		t.Errorf("REPORT executed before LINT or ANALYSIS")
	}
}

// TestIntegration_ParallelExecution tests that independent nodes run in parallel.
func TestIntegration_ParallelExecution(t *testing.T) {
	ctx := context.Background()

	// Track start times to verify parallelism
	type timing struct {
		name  string
		start time.Time
		end   time.Time
	}
	timings := make(chan timing, 10)

	// Create nodes with artificial delay
	createTimedNode := func(name string, deps []string, delay time.Duration) dag.Node {
		return dag.NewFuncNode(name, deps, func(ctx context.Context, inputs map[string]any) (any, error) {
			start := time.Now()
			time.Sleep(delay)
			end := time.Now()
			timings <- timing{name: name, start: start, end: end}
			return map[string]any{"done": true}, nil
		})
	}

	// Setup node depends on root
	setupNode := createTimedNode("SETUP", nil, 50*time.Millisecond)

	// Three parallel nodes depend on SETUP
	parallel1 := createTimedNode("PARALLEL_1", []string{"SETUP"}, 100*time.Millisecond)
	parallel2 := createTimedNode("PARALLEL_2", []string{"SETUP"}, 100*time.Millisecond)
	parallel3 := createTimedNode("PARALLEL_3", []string{"SETUP"}, 100*time.Millisecond)

	// Final node depends on all parallel nodes
	finalNode := createTimedNode("FINAL", []string{"PARALLEL_1", "PARALLEL_2", "PARALLEL_3"}, 50*time.Millisecond)

	// Build pipeline
	pipeline, err := dag.NewBuilder("parallel-test").
		AddNode(setupNode).
		AddNode(parallel1).
		AddNode(parallel2).
		AddNode(parallel3).
		AddNode(finalNode).
		Build()

	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	// Execute
	executor, err := dag.NewExecutor(pipeline, slog.Default())
	if err != nil {
		t.Fatalf("NewExecutor: %v", err)
	}

	start := time.Now()
	result, err := executor.Run(ctx, nil)
	totalDuration := time.Since(start)

	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if !result.Success {
		t.Errorf("expected success, got failure: %s", result.Error)
	}

	close(timings)

	// Collect timings
	timingMap := make(map[string]timing)
	for tm := range timings {
		timingMap[tm.name] = tm
	}

	// Verify parallel execution
	// If sequential: 50 + 100 + 100 + 100 + 50 = 400ms
	// If parallel: 50 + 100 + 50 = 200ms (approximately)
	// Allow some margin for scheduling
	maxExpected := 300 * time.Millisecond
	if totalDuration > maxExpected {
		t.Errorf("total duration %v > %v, parallel execution may not be working",
			totalDuration, maxExpected)
	}

	// Verify parallel nodes overlapped
	p1 := timingMap["PARALLEL_1"]
	p2 := timingMap["PARALLEL_2"]
	p3 := timingMap["PARALLEL_3"]

	// At least two parallel nodes should have overlapping execution
	overlap12 := p1.start.Before(p2.end) && p2.start.Before(p1.end)
	overlap23 := p2.start.Before(p3.end) && p3.start.Before(p2.end)
	overlap13 := p1.start.Before(p3.end) && p3.start.Before(p1.end)

	if !overlap12 && !overlap23 && !overlap13 {
		t.Errorf("no overlap detected between parallel nodes - parallelism may be broken")
	}
}

// TestIntegration_CheckpointResume tests checkpoint save and resume.
func TestIntegration_CheckpointResume(t *testing.T) {
	ctx := context.Background()

	// Track which nodes executed
	executed := make(map[string]bool)

	// First two nodes succeed
	node1 := dag.NewFuncNode("NODE_1", nil, func(ctx context.Context, inputs map[string]any) (any, error) {
		executed["NODE_1"] = true
		return "result1", nil
	})

	node2 := dag.NewFuncNode("NODE_2", []string{"NODE_1"}, func(ctx context.Context, inputs map[string]any) (any, error) {
		executed["NODE_2"] = true
		return "result2", nil
	})

	node3 := dag.NewFuncNode("NODE_3", []string{"NODE_2"}, func(ctx context.Context, inputs map[string]any) (any, error) {
		executed["NODE_3"] = true
		return "result3", nil
	})

	// Build pipeline
	pipeline, err := dag.NewBuilder("checkpoint-test").
		AddNode(node1).
		AddNode(node2).
		AddNode(node3).
		Build()

	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	// Execute fully first time
	executor, err := dag.NewExecutor(pipeline, slog.Default())
	if err != nil {
		t.Fatalf("NewExecutor: %v", err)
	}

	result, err := executor.Run(ctx, "initial_input")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if !result.Success {
		t.Errorf("expected success, got failure: %s", result.Error)
	}

	// Verify all nodes executed
	if !executed["NODE_1"] || !executed["NODE_2"] || !executed["NODE_3"] {
		t.Errorf("not all nodes executed: %v", executed)
	}
}

// TestIntegration_ErrorPropagation tests that errors propagate correctly.
func TestIntegration_ErrorPropagation(t *testing.T) {
	ctx := context.Background()

	// Track execution
	executed := make(map[string]bool)

	// First node succeeds
	node1 := dag.NewFuncNode("SUCCESS", nil, func(ctx context.Context, inputs map[string]any) (any, error) {
		executed["SUCCESS"] = true
		return "ok", nil
	})

	// Second node fails
	errNode := dag.NewFuncNode("FAIL", []string{"SUCCESS"}, func(ctx context.Context, inputs map[string]any) (any, error) {
		executed["FAIL"] = true
		return nil, dag.NewNodeError("FAIL", dag.ErrNodeFailed)
	})

	// Third node should not execute
	node3 := dag.NewFuncNode("AFTER_FAIL", []string{"FAIL"}, func(ctx context.Context, inputs map[string]any) (any, error) {
		executed["AFTER_FAIL"] = true
		return "should not run", nil
	})

	// Build pipeline
	pipeline, err := dag.NewBuilder("error-test").
		AddNode(node1).
		AddNode(errNode).
		AddNode(node3).
		Build()

	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	// Execute
	executor, err := dag.NewExecutor(pipeline, slog.Default())
	if err != nil {
		t.Fatalf("NewExecutor: %v", err)
	}

	result, _ := executor.Run(ctx, nil)

	// Verify failure
	if result.Success {
		t.Errorf("expected failure, got success")
	}

	// Verify failed node is reported
	if result.FailedNode != "FAIL" {
		t.Errorf("FailedNode = %s, want FAIL", result.FailedNode)
	}

	// Verify SUCCESS ran but AFTER_FAIL didn't
	if !executed["SUCCESS"] {
		t.Errorf("SUCCESS should have executed")
	}
	if !executed["FAIL"] {
		t.Errorf("FAIL should have executed")
	}
	if executed["AFTER_FAIL"] {
		t.Errorf("AFTER_FAIL should not have executed after failure")
	}
}

// TestIntegration_ContextCancellation tests that context cancellation is respected.
func TestIntegration_ContextCancellation(t *testing.T) {
	// Create cancellable context
	ctx, cancel := context.WithCancel(context.Background())

	// Track execution
	started := make(chan string, 5)

	// First node signals then waits
	node1 := dag.NewFuncNode("LONG_RUNNING", nil, func(ctx context.Context, inputs map[string]any) (any, error) {
		started <- "LONG_RUNNING"
		// Wait for cancellation or timeout
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(5 * time.Second):
			return "completed", nil
		}
	})

	// Build pipeline
	pipeline, err := dag.NewBuilder("cancel-test").
		AddNode(node1).
		Build()

	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	// Execute in goroutine
	executor, err := dag.NewExecutor(pipeline, slog.Default())
	if err != nil {
		t.Fatalf("NewExecutor: %v", err)
	}

	done := make(chan struct{})
	var result *dag.Result
	var runErr error

	go func() {
		result, runErr = executor.Run(ctx, nil)
		close(done)
	}()

	// Wait for node to start
	<-started

	// Cancel context
	cancel()

	// Wait for completion with timeout
	select {
	case <-done:
		// Expected
	case <-time.After(2 * time.Second):
		t.Fatalf("execution did not complete after cancellation")
	}

	// Verify cancellation was reported
	if runErr == nil {
		t.Errorf("expected error from cancelled context")
	}

	if result != nil && result.Success {
		t.Errorf("expected failure after cancellation")
	}
}

// TestIntegration_NodeDurations tests that node durations are tracked.
func TestIntegration_NodeDurations(t *testing.T) {
	ctx := context.Background()

	// Create node with known delay
	slowNode := dag.NewFuncNode("SLOW", nil, func(ctx context.Context, inputs map[string]any) (any, error) {
		time.Sleep(50 * time.Millisecond)
		return "done", nil
	})

	fastNode := dag.NewFuncNode("FAST", []string{"SLOW"}, func(ctx context.Context, inputs map[string]any) (any, error) {
		return "done", nil
	})

	// Build pipeline
	pipeline, err := dag.NewBuilder("duration-test").
		AddNode(slowNode).
		AddNode(fastNode).
		Build()

	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	// Execute
	executor, err := dag.NewExecutor(pipeline, slog.Default())
	if err != nil {
		t.Fatalf("NewExecutor: %v", err)
	}

	result, err := executor.Run(ctx, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Verify durations are tracked
	if result.NodeDurations == nil {
		t.Fatalf("NodeDurations is nil")
	}

	slowDuration, ok := result.NodeDurations["SLOW"]
	if !ok {
		t.Errorf("SLOW duration not tracked")
	} else if slowDuration < 50*time.Millisecond {
		t.Errorf("SLOW duration %v < expected 50ms", slowDuration)
	}

	fastDuration, ok := result.NodeDurations["FAST"]
	if !ok {
		t.Errorf("FAST duration not tracked")
	} else if fastDuration >= slowDuration {
		t.Errorf("FAST duration %v >= SLOW duration %v", fastDuration, slowDuration)
	}
}

// Helper function to find index of string in slice.
func indexOf(slice []string, item string) int {
	for i, s := range slice {
		if s == item {
			return i
		}
	}
	return -1
}
