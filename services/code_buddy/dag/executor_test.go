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
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestNode is a simple test node that records execution.
type TestNode struct {
	BaseNode
	executed    bool
	executedMu  sync.Mutex
	returnValue any
	returnError error
	delay       time.Duration
}

func NewTestNode(name string, deps []string) *TestNode {
	return &TestNode{
		BaseNode: BaseNode{
			NodeName:         name,
			NodeDependencies: deps,
			NodeTimeout:      5 * time.Second,
		},
		returnValue: name + "_output",
	}
}

func (n *TestNode) Execute(ctx context.Context, inputs map[string]any) (any, error) {
	if n.delay > 0 {
		select {
		case <-time.After(n.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	n.executedMu.Lock()
	n.executed = true
	n.executedMu.Unlock()

	if n.returnError != nil {
		return nil, n.returnError
	}
	return n.returnValue, nil
}

func (n *TestNode) WasExecuted() bool {
	n.executedMu.Lock()
	defer n.executedMu.Unlock()
	return n.executed
}

func (n *TestNode) WithError(err error) *TestNode {
	n.returnError = err
	return n
}

func (n *TestNode) WithDelay(d time.Duration) *TestNode {
	n.delay = d
	return n
}

func (n *TestNode) WithOutput(output any) *TestNode {
	n.returnValue = output
	return n
}

// --- Builder Tests ---

func TestBuilder_AddNode(t *testing.T) {
	node := NewTestNode("A", nil)

	dag, err := NewBuilder("test").
		AddNode(node).
		Build()

	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	if dag.NodeCount() != 1 {
		t.Errorf("NodeCount() = %d, want 1", dag.NodeCount())
	}

	if dag.Name() != "test" {
		t.Errorf("Name() = %q, want %q", dag.Name(), "test")
	}
}

func TestBuilder_AddNode_Nil(t *testing.T) {
	_, err := NewBuilder("test").
		AddNode(nil).
		Build()

	if err == nil {
		t.Error("Build() should fail with nil node")
	}
	if !errors.Is(err, ErrNilNode) {
		t.Errorf("error = %v, want %v", err, ErrNilNode)
	}
}

func TestBuilder_AddNode_Duplicate(t *testing.T) {
	node1 := NewTestNode("A", nil)
	node2 := NewTestNode("A", nil) // Same name

	_, err := NewBuilder("test").
		AddNode(node1).
		AddNode(node2).
		Build()

	if err == nil {
		t.Error("Build() should fail with duplicate node")
	}
}

func TestBuilder_Build_EmptyDAG(t *testing.T) {
	_, err := NewBuilder("test").Build()

	if err == nil {
		t.Error("Build() should fail with empty DAG")
	}
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("error = %v, want %v", err, ErrInvalidInput)
	}
}

func TestBuilder_Build_MissingDependency(t *testing.T) {
	node := NewTestNode("A", []string{"B"}) // B doesn't exist

	_, err := NewBuilder("test").
		AddNode(node).
		Build()

	if err == nil {
		t.Error("Build() should fail with missing dependency")
	}
}

func TestBuilder_Build_CycleDetection(t *testing.T) {
	// A → B → C → A (cycle)
	nodeA := NewTestNode("A", []string{"C"})
	nodeB := NewTestNode("B", []string{"A"})
	nodeC := NewTestNode("C", []string{"B"})

	_, err := NewBuilder("test").
		AddNode(nodeA).
		AddNode(nodeB).
		AddNode(nodeC).
		Build()

	if err == nil {
		t.Error("Build() should fail with cycle")
	}

	var cycleErr *CycleError
	if !errors.As(err, &cycleErr) {
		t.Errorf("error should be CycleError, got %T", err)
	}
}

func TestBuilder_Build_LinearDAG(t *testing.T) {
	// A → B → C
	nodeA := NewTestNode("A", nil)
	nodeB := NewTestNode("B", []string{"A"})
	nodeC := NewTestNode("C", []string{"B"})

	dag, err := NewBuilder("test").
		AddNode(nodeA).
		AddNode(nodeB).
		AddNode(nodeC).
		Build()

	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	if dag.NodeCount() != 3 {
		t.Errorf("NodeCount() = %d, want 3", dag.NodeCount())
	}

	if dag.Terminal() != "C" {
		t.Errorf("Terminal() = %q, want %q", dag.Terminal(), "C")
	}

	deps := dag.GetDependencies("C")
	if len(deps) != 1 || deps[0] != "B" {
		t.Errorf("GetDependencies(C) = %v, want [B]", deps)
	}
}

func TestBuilder_Build_DiamondDAG(t *testing.T) {
	//     A
	//    / \
	//   B   C
	//    \ /
	//     D
	nodeA := NewTestNode("A", nil)
	nodeB := NewTestNode("B", []string{"A"})
	nodeC := NewTestNode("C", []string{"A"})
	nodeD := NewTestNode("D", []string{"B", "C"})

	dag, err := NewBuilder("test").
		AddNode(nodeA).
		AddNode(nodeB).
		AddNode(nodeC).
		AddNode(nodeD).
		Build()

	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	if dag.Terminal() != "D" {
		t.Errorf("Terminal() = %q, want %q", dag.Terminal(), "D")
	}
}

// --- Executor Tests ---

func TestExecutor_Run_SingleNode(t *testing.T) {
	node := NewTestNode("A", nil).WithOutput("result")

	dag, err := NewBuilder("test").AddNode(node).Build()
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	executor, err := NewExecutor(dag, nil)
	if err != nil {
		t.Fatalf("NewExecutor() error = %v", err)
	}

	result, err := executor.Run(context.Background(), "input")
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if !result.Success {
		t.Errorf("Success = false, want true")
	}

	if result.NodesExecuted != 1 {
		t.Errorf("NodesExecuted = %d, want 1", result.NodesExecuted)
	}

	if result.Output != "result" {
		t.Errorf("Output = %v, want %q", result.Output, "result")
	}

	if !node.WasExecuted() {
		t.Error("node was not executed")
	}
}

func TestExecutor_Run_LinearDAG(t *testing.T) {
	nodeA := NewTestNode("A", nil).WithOutput("A_out")
	nodeB := NewTestNode("B", []string{"A"}).WithOutput("B_out")
	nodeC := NewTestNode("C", []string{"B"}).WithOutput("C_out")

	dag, err := NewBuilder("test").
		AddNode(nodeA).
		AddNode(nodeB).
		AddNode(nodeC).
		Build()
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	executor, _ := NewExecutor(dag, nil)
	result, err := executor.Run(context.Background(), nil)

	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if !result.Success {
		t.Errorf("Success = false, want true")
	}

	if result.NodesExecuted != 3 {
		t.Errorf("NodesExecuted = %d, want 3", result.NodesExecuted)
	}

	if result.Output != "C_out" {
		t.Errorf("Output = %v, want %q", result.Output, "C_out")
	}

	// Verify all executed
	for _, n := range []*TestNode{nodeA, nodeB, nodeC} {
		if !n.WasExecuted() {
			t.Errorf("node %s was not executed", n.Name())
		}
	}
}

func TestExecutor_Run_ParallelExecution(t *testing.T) {
	//     A
	//    / \
	//   B   C   (B and C should run in parallel)
	//    \ /
	//     D
	var bStarted, cStarted int64

	nodeA := NewFuncNode("A", nil, func(ctx context.Context, inputs map[string]any) (any, error) {
		return "A_out", nil
	})

	nodeB := NewFuncNode("B", []string{"A"}, func(ctx context.Context, inputs map[string]any) (any, error) {
		atomic.StoreInt64(&bStarted, time.Now().UnixNano())
		time.Sleep(50 * time.Millisecond)
		return "B_out", nil
	})

	nodeC := NewFuncNode("C", []string{"A"}, func(ctx context.Context, inputs map[string]any) (any, error) {
		atomic.StoreInt64(&cStarted, time.Now().UnixNano())
		time.Sleep(50 * time.Millisecond)
		return "C_out", nil
	})

	nodeD := NewFuncNode("D", []string{"B", "C"}, func(ctx context.Context, inputs map[string]any) (any, error) {
		return "D_out", nil
	})

	dag, _ := NewBuilder("test").
		AddNode(nodeA).
		AddNode(nodeB).
		AddNode(nodeC).
		AddNode(nodeD).
		Build()

	executor, _ := NewExecutor(dag, nil)
	start := time.Now()
	result, err := executor.Run(context.Background(), nil)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if !result.Success {
		t.Errorf("Success = false, want true")
	}

	// B and C should run in parallel, so total time should be ~100ms not ~150ms
	// (A: instant, B+C: parallel 50ms, D: instant)
	if elapsed > 200*time.Millisecond {
		t.Errorf("elapsed = %v, expected < 200ms (parallel execution)", elapsed)
	}

	// Verify B and C started within 20ms of each other (parallel)
	bStart := atomic.LoadInt64(&bStarted)
	cStart := atomic.LoadInt64(&cStarted)
	diff := bStart - cStart
	if diff < 0 {
		diff = -diff
	}
	if diff > int64(20*time.Millisecond) {
		t.Errorf("B and C start times differ by %v, expected parallel start", time.Duration(diff))
	}
}

func TestExecutor_Run_NodeFailure(t *testing.T) {
	testErr := errors.New("test error")

	nodeA := NewTestNode("A", nil)
	nodeB := NewTestNode("B", []string{"A"}).WithError(testErr)
	nodeC := NewTestNode("C", []string{"B"})

	dag, _ := NewBuilder("test").
		AddNode(nodeA).
		AddNode(nodeB).
		AddNode(nodeC).
		Build()

	executor, _ := NewExecutor(dag, nil)
	result, err := executor.Run(context.Background(), nil)

	if err == nil {
		t.Fatal("Run() should return error")
	}

	if result.Success {
		t.Error("Success = true, want false")
	}

	if result.FailedNode != "B" {
		t.Errorf("FailedNode = %q, want %q", result.FailedNode, "B")
	}

	// C should not have executed
	if nodeC.WasExecuted() {
		t.Error("node C should not have executed after B failed")
	}
}

func TestExecutor_Run_ContextCancellation(t *testing.T) {
	nodeA := NewTestNode("A", nil).WithDelay(500 * time.Millisecond)

	dag, _ := NewBuilder("test").AddNode(nodeA).Build()
	executor, _ := NewExecutor(dag, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	result, err := executor.Run(ctx, nil)

	if err == nil {
		t.Fatal("Run() should return error on cancellation")
	}

	if result.Success {
		t.Error("Success = true, want false")
	}
}

func TestExecutor_Run_NodeTimeout(t *testing.T) {
	node := NewTestNode("A", nil).WithDelay(500 * time.Millisecond)
	node.NodeTimeout = 50 * time.Millisecond

	dag, _ := NewBuilder("test").AddNode(node).Build()
	executor, _ := NewExecutor(dag, nil)

	result, err := executor.Run(context.Background(), nil)

	if err == nil {
		t.Fatal("Run() should return error on timeout")
	}

	if result.Success {
		t.Error("Success = true, want false")
	}

	if !errors.Is(err, ErrNodeTimeout) {
		t.Errorf("error should wrap ErrNodeTimeout, got %v", err)
	}
}

func TestExecutor_Run_NilContext(t *testing.T) {
	node := NewTestNode("A", nil)
	dag, _ := NewBuilder("test").AddNode(node).Build()
	executor, _ := NewExecutor(dag, nil)

	_, err := executor.Run(nil, nil)

	if !errors.Is(err, ErrNilContext) {
		t.Errorf("error = %v, want %v", err, ErrNilContext)
	}
}

func TestExecutor_Run_InputPropagation(t *testing.T) {
	var receivedInput any

	nodeA := NewFuncNode("A", nil, func(ctx context.Context, inputs map[string]any) (any, error) {
		receivedInput = inputs["root"]
		return "from_A", nil
	})

	var receivedFromA any
	nodeB := NewFuncNode("B", []string{"A"}, func(ctx context.Context, inputs map[string]any) (any, error) {
		receivedFromA = inputs["A"]
		return "from_B", nil
	})

	dag, _ := NewBuilder("test").AddNode(nodeA).AddNode(nodeB).Build()
	executor, _ := NewExecutor(dag, nil)

	_, err := executor.Run(context.Background(), "initial_input")
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if receivedInput != "initial_input" {
		t.Errorf("A received input = %v, want %q", receivedInput, "initial_input")
	}

	if receivedFromA != "from_A" {
		t.Errorf("B received from A = %v, want %q", receivedFromA, "from_A")
	}
}

func TestNewExecutor_NilDAG(t *testing.T) {
	_, err := NewExecutor(nil, nil)

	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("error = %v, want %v", err, ErrInvalidInput)
	}
}

// --- State Tests ---

func TestState_Lifecycle(t *testing.T) {
	state := NewState("test-session")

	if state.SessionID != "test-session" {
		t.Errorf("SessionID = %q, want %q", state.SessionID, "test-session")
	}

	if state.IsCompleted("A") {
		t.Error("IsCompleted(A) = true before completion")
	}

	state.SetCompleted("A", "output")

	if !state.IsCompleted("A") {
		t.Error("IsCompleted(A) = false after completion")
	}

	output, ok := state.GetOutput("A")
	if !ok || output != "output" {
		t.Errorf("GetOutput(A) = %v, %v; want %q, true", output, ok, "output")
	}

	if state.IsFailed() {
		t.Error("IsFailed() = true before failure")
	}

	state.SetFailed("B", errors.New("test error"))

	if !state.IsFailed() {
		t.Error("IsFailed() = false after failure")
	}

	if state.FailedNode != "B" {
		t.Errorf("FailedNode = %q, want %q", state.FailedNode, "B")
	}
}

func TestState_Concurrent(t *testing.T) {
	state := NewState("test")
	var wg sync.WaitGroup

	// Concurrent writes
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			name := string(rune('A' + n%26))
			state.SetCompleted(name, n)
			state.GetOutput(name)
			state.IsCompleted(name)
		}(i)
	}

	wg.Wait()

	// Should not panic or corrupt state
	if state.CompletedCount() == 0 {
		t.Error("no nodes completed after concurrent writes")
	}
}

// --- FuncNode Tests ---

func TestFuncNode_Execute(t *testing.T) {
	executed := false
	node := NewFuncNode("test", []string{"dep"}, func(ctx context.Context, inputs map[string]any) (any, error) {
		executed = true
		return "result", nil
	})

	output, err := node.Execute(context.Background(), nil)

	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if !executed {
		t.Error("function was not executed")
	}

	if output != "result" {
		t.Errorf("output = %v, want %q", output, "result")
	}

	if node.Name() != "test" {
		t.Errorf("Name() = %q, want %q", node.Name(), "test")
	}

	deps := node.Dependencies()
	if len(deps) != 1 || deps[0] != "dep" {
		t.Errorf("Dependencies() = %v, want [dep]", deps)
	}
}

func TestFuncNode_WithTimeout(t *testing.T) {
	node := NewFuncNode("test", nil, nil).WithTimeout(5 * time.Second)

	if node.Timeout() != 5*time.Second {
		t.Errorf("Timeout() = %v, want %v", node.Timeout(), 5*time.Second)
	}
}

func TestFuncNode_WithRetryable(t *testing.T) {
	node := NewFuncNode("test", nil, nil).WithRetryable(true)

	if !node.Retryable() {
		t.Error("Retryable() = false, want true")
	}
}

func TestFuncNode_NilFunction(t *testing.T) {
	node := NewFuncNode("test", nil, nil)

	_, err := node.Execute(context.Background(), nil)

	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("error = %v, want %v", err, ErrInvalidInput)
	}
}

// --- Error Tests ---

func TestNodeError(t *testing.T) {
	inner := errors.New("inner error")
	err := NewNodeError("MY_NODE", inner)

	if err.Error() != `node "MY_NODE": inner error` {
		t.Errorf("Error() = %q", err.Error())
	}

	if !errors.Is(err, inner) {
		t.Error("Unwrap should return inner error")
	}
}

func TestCycleError(t *testing.T) {
	err := NewCycleError([]string{"A", "B", "C", "A"})

	if err.Error() != "cycle detected: [A B C A]" {
		t.Errorf("Error() = %q", err.Error())
	}
}

func TestBuilder_MultipleTerminals(t *testing.T) {
	// Test behavior when DAG has multiple terminal nodes (nodes with no dependents)
	//     A
	//    / \
	//   B   C  ← Both are terminals (no one depends on them)
	nodeA := NewTestNode("A", nil)
	nodeB := NewTestNode("B", []string{"A"})
	nodeC := NewTestNode("C", []string{"A"})

	dag, err := NewBuilder("test").
		AddNode(nodeA).
		AddNode(nodeB).
		AddNode(nodeC).
		Build()

	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	// Terminal should be "B" (lexicographically first among terminals)
	terminal := dag.Terminal()
	if terminal != "B" {
		t.Errorf("Terminal() = %q, want B (lexicographically first)", terminal)
	}

	// Execution should still work - all nodes should complete
	executor, _ := NewExecutor(dag, nil)
	result, err := executor.Run(context.Background(), nil)

	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if !result.Success {
		t.Error("expected success")
	}

	if result.NodesExecuted != 3 {
		t.Errorf("NodesExecuted = %d, want 3", result.NodesExecuted)
	}
}

func TestBaseNode_Execute_ReturnsError(t *testing.T) {
	// Verify BaseNode.Execute returns error instead of panicking
	node := &BaseNode{NodeName: "test"}

	_, err := node.Execute(context.Background(), nil)

	if err == nil {
		t.Fatal("expected error from BaseNode.Execute")
	}

	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("expected ErrInvalidInput, got: %v", err)
	}
}
