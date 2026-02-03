// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package integration

import (
	"testing"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/trace/agent/mcts/activities"
	"github.com/AleutianAI/AleutianFOSS/services/trace/agent/mcts/algorithms"
	"github.com/AleutianAI/AleutianFOSS/services/trace/agent/mcts/crs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// -----------------------------------------------------------------------------
// Test Helpers
// -----------------------------------------------------------------------------

// mockActivityInput implements activities.ActivityInput for testing.
type mockActivityInput struct {
	typeName string
	source   crs.SignalSource
	target   string
}

func (m mockActivityInput) Type() string {
	return m.typeName
}

func (m mockActivityInput) Source() crs.SignalSource {
	return m.source
}

func (m mockActivityInput) Target() string {
	return m.target
}

// mockInputWithFilePath implements ActivityInput with FilePath method.
type mockInputWithFilePath struct {
	typeName string
	source   crs.SignalSource
	filePath string
}

func (m mockInputWithFilePath) Type() string {
	return m.typeName
}

func (m mockInputWithFilePath) Source() crs.SignalSource {
	return m.source
}

func (m mockInputWithFilePath) FilePath() string {
	return m.filePath
}

// mockOutputWithSymbols provides symbols via Symbols() method.
type mockOutputWithSymbols struct {
	symbols []string
}

func (m mockOutputWithSymbols) Symbols() []string {
	return m.symbols
}

// -----------------------------------------------------------------------------
// ExtractTraceStep Tests
// -----------------------------------------------------------------------------

func TestExtractTraceStep_NilInputs(t *testing.T) {
	t.Run("all nil inputs", func(t *testing.T) {
		startTime := time.Now()
		step := ExtractTraceStep(nil, nil, nil, startTime)

		assert.Equal(t, startTime, step.Timestamp)
		assert.Empty(t, step.Action)
		assert.Empty(t, step.Target)
		assert.Empty(t, step.SymbolsFound)
		assert.NotNil(t, step.ProofUpdates)
		assert.Empty(t, step.ProofUpdates)
		assert.NotNil(t, step.ConstraintsAdded)
		assert.Empty(t, step.ConstraintsAdded)
		assert.NotNil(t, step.DependenciesFound)
		assert.Empty(t, step.DependenciesFound)
	})

	t.Run("nil result with delta", func(t *testing.T) {
		startTime := time.Now()
		delta := crs.NewProofDelta(crs.SignalSourceHard, map[string]crs.ProofNumber{
			"node1": {Status: crs.ProofStatusProven},
		})

		step := ExtractTraceStep(nil, delta, nil, startTime)

		assert.Empty(t, step.Action)
		assert.Len(t, step.ProofUpdates, 1)
	})

	t.Run("result with nil delta", func(t *testing.T) {
		startTime := time.Now()
		result := &activities.ActivityResult{
			ActivityName: "explore",
			Duration:     100 * time.Millisecond,
			Success:      true,
		}

		step := ExtractTraceStep(result, nil, nil, startTime)

		assert.Equal(t, "explore", step.Action)
		assert.Equal(t, 100*time.Millisecond, step.Duration)
		assert.NotNil(t, step.ProofUpdates)
		assert.Empty(t, step.ProofUpdates)
	})
}

func TestExtractTraceStep_FullExtraction(t *testing.T) {
	startTime := time.Now()

	result := &activities.ActivityResult{
		ActivityName: "analyze",
		Duration:     250 * time.Millisecond,
		Success:      true,
		AlgorithmResults: []*algorithms.Result{
			{
				Name:   "symbol_search",
				Output: mockOutputWithSymbols{symbols: []string{"func1", "func2"}},
			},
		},
	}

	// Create composite delta with proof, constraint, and dependency changes
	proofDelta := crs.NewProofDelta(crs.SignalSourceHard, map[string]crs.ProofNumber{
		"node1": {Status: crs.ProofStatusProven, Source: crs.SignalSourceHard},
		"node2": {Status: crs.ProofStatusExpanded, Source: crs.SignalSourceSoft},
	})

	constraintDelta := crs.NewConstraintDelta(crs.SignalSourceSoft)
	constraintDelta.Add = []crs.Constraint{
		{ID: "c1", Type: crs.ConstraintTypeMutualExclusion, Nodes: []string{"a", "b"}},
	}

	depDelta := crs.NewDependencyDelta(crs.SignalSourceSoft)
	depDelta.AddEdges = [][2]string{{"from1", "to1"}, {"from2", "to2"}}

	compositeDelta := crs.NewCompositeDelta(proofDelta, constraintDelta, depDelta)

	input := mockActivityInput{
		typeName: "explore",
		source:   crs.SignalSourceSoft,
		target:   "main.go",
	}

	step := ExtractTraceStep(result, compositeDelta, input, startTime)

	// Verify result extraction
	assert.Equal(t, "analyze", step.Action)
	assert.Equal(t, 250*time.Millisecond, step.Duration)
	assert.Equal(t, startTime, step.Timestamp)
	assert.Equal(t, "main.go", step.Target)

	// Verify symbols extraction
	require.Len(t, step.SymbolsFound, 2)
	assert.Contains(t, step.SymbolsFound, "func1")
	assert.Contains(t, step.SymbolsFound, "func2")

	// Verify proof updates (sorted by node ID)
	require.Len(t, step.ProofUpdates, 2)
	assert.Equal(t, "node1", step.ProofUpdates[0].NodeID)
	assert.Equal(t, "proven", step.ProofUpdates[0].Status)
	assert.Equal(t, "hard", step.ProofUpdates[0].Source)
	assert.Equal(t, "node2", step.ProofUpdates[1].NodeID)
	assert.Equal(t, "expanded", step.ProofUpdates[1].Status)

	// Verify constraints
	require.Len(t, step.ConstraintsAdded, 1)
	assert.Equal(t, "c1", step.ConstraintsAdded[0].ID)
	assert.Equal(t, "mutual_exclusion", step.ConstraintsAdded[0].Type)
	assert.Equal(t, []string{"a", "b"}, step.ConstraintsAdded[0].Nodes)

	// Verify dependencies
	require.Len(t, step.DependenciesFound, 2)
	assert.Equal(t, "from1", step.DependenciesFound[0].From)
	assert.Equal(t, "to1", step.DependenciesFound[0].To)
}

func TestExtractTraceStep_FailedActivity(t *testing.T) {
	startTime := time.Now()
	result := &activities.ActivityResult{
		ActivityName: "explore",
		Success:      false,
	}

	step := ExtractTraceStep(result, nil, nil, startTime)

	assert.Equal(t, "activity failed", step.Error)
}

// -----------------------------------------------------------------------------
// extractProofUpdates Tests
// -----------------------------------------------------------------------------

func TestExtractProofUpdates_NilDelta(t *testing.T) {
	updates := extractProofUpdates(nil, 0)
	assert.NotNil(t, updates)
	assert.Empty(t, updates)
}

func TestExtractProofUpdates_EmptyProofDelta(t *testing.T) {
	delta := crs.NewProofDelta(crs.SignalSourceSoft, map[string]crs.ProofNumber{})
	updates := extractProofUpdates(delta, 0)
	assert.NotNil(t, updates)
	assert.Empty(t, updates)
}

func TestExtractProofUpdates_SingleUpdate(t *testing.T) {
	delta := crs.NewProofDelta(crs.SignalSourceHard, map[string]crs.ProofNumber{
		"node1": {Status: crs.ProofStatusProven, Source: crs.SignalSourceHard},
	})

	updates := extractProofUpdates(delta, 0)

	require.Len(t, updates, 1)
	assert.Equal(t, "node1", updates[0].NodeID)
	assert.Equal(t, "proven", updates[0].Status)
	assert.Equal(t, "hard", updates[0].Source)
}

func TestExtractProofUpdates_DeterministicOrder(t *testing.T) {
	// Create delta with multiple nodes (maps are unordered)
	delta := crs.NewProofDelta(crs.SignalSourceSoft, map[string]crs.ProofNumber{
		"zebra":  {Status: crs.ProofStatusUnknown},
		"apple":  {Status: crs.ProofStatusProven},
		"mango":  {Status: crs.ProofStatusExpanded},
		"banana": {Status: crs.ProofStatusDisproven},
	})

	// Run multiple times to verify determinism
	for i := 0; i < 10; i++ {
		updates := extractProofUpdates(delta, 0)
		require.Len(t, updates, 4)
		assert.Equal(t, "apple", updates[0].NodeID)
		assert.Equal(t, "banana", updates[1].NodeID)
		assert.Equal(t, "mango", updates[2].NodeID)
		assert.Equal(t, "zebra", updates[3].NodeID)
	}
}

func TestExtractProofUpdates_CompositeDelta(t *testing.T) {
	proof1 := crs.NewProofDelta(crs.SignalSourceHard, map[string]crs.ProofNumber{
		"node1": {Status: crs.ProofStatusProven},
	})
	proof2 := crs.NewProofDelta(crs.SignalSourceSoft, map[string]crs.ProofNumber{
		"node2": {Status: crs.ProofStatusExpanded},
	})

	composite := crs.NewCompositeDelta(proof1, proof2)
	updates := extractProofUpdates(composite, 0)

	require.Len(t, updates, 2)
}

func TestExtractProofUpdates_NestedCompositeDelta(t *testing.T) {
	proof1 := crs.NewProofDelta(crs.SignalSourceHard, map[string]crs.ProofNumber{
		"node1": {Status: crs.ProofStatusProven},
	})
	proof2 := crs.NewProofDelta(crs.SignalSourceSoft, map[string]crs.ProofNumber{
		"node2": {Status: crs.ProofStatusExpanded},
	})

	inner := crs.NewCompositeDelta(proof2)
	outer := crs.NewCompositeDelta(proof1, inner)

	updates := extractProofUpdates(outer, 0)

	require.Len(t, updates, 2)
}

func TestExtractProofUpdates_DepthLimit(t *testing.T) {
	// Ensure depth limit is respected (should return empty at depth > 100)
	delta := crs.NewProofDelta(crs.SignalSourceHard, map[string]crs.ProofNumber{
		"node1": {Status: crs.ProofStatusProven},
	})

	updates := extractProofUpdates(delta, maxCompositeDeltaDepth+1)
	assert.Empty(t, updates)
}

func TestExtractProofUpdates_NonProofDelta(t *testing.T) {
	// ConstraintDelta should return empty proof updates
	delta := crs.NewConstraintDelta(crs.SignalSourceSoft)
	updates := extractProofUpdates(delta, 0)
	assert.NotNil(t, updates)
	assert.Empty(t, updates)
}

// -----------------------------------------------------------------------------
// extractConstraints Tests
// -----------------------------------------------------------------------------

func TestExtractConstraints_NilDelta(t *testing.T) {
	constraints := extractConstraints(nil, 0)
	assert.NotNil(t, constraints)
	assert.Empty(t, constraints)
}

func TestExtractConstraints_EmptyConstraintDelta(t *testing.T) {
	delta := crs.NewConstraintDelta(crs.SignalSourceSoft)
	constraints := extractConstraints(delta, 0)
	assert.NotNil(t, constraints)
	assert.Empty(t, constraints)
}

func TestExtractConstraints_AddedConstraints(t *testing.T) {
	delta := crs.NewConstraintDelta(crs.SignalSourceSoft)
	delta.Add = []crs.Constraint{
		{ID: "c1", Type: crs.ConstraintTypeMutualExclusion, Nodes: []string{"a", "b"}},
		{ID: "c2", Type: crs.ConstraintTypeImplication, Nodes: []string{"x", "y", "z"}},
	}

	constraints := extractConstraints(delta, 0)

	require.Len(t, constraints, 2)
	assert.Equal(t, "c1", constraints[0].ID)
	assert.Equal(t, "mutual_exclusion", constraints[0].Type)
	assert.Equal(t, []string{"a", "b"}, constraints[0].Nodes)
	assert.Equal(t, "c2", constraints[1].ID)
	assert.Equal(t, "implication", constraints[1].Type)
}

func TestExtractConstraints_UpdatedConstraints(t *testing.T) {
	delta := crs.NewConstraintDelta(crs.SignalSourceSoft)
	delta.Update = map[string]crs.Constraint{
		"c1": {ID: "c1", Type: crs.ConstraintTypeOrdering, Nodes: []string{"1", "2"}},
	}

	constraints := extractConstraints(delta, 0)

	require.Len(t, constraints, 1)
	assert.Equal(t, "c1", constraints[0].ID)
	assert.Equal(t, "ordering", constraints[0].Type)
}

func TestExtractConstraints_NodesCopied(t *testing.T) {
	originalNodes := []string{"a", "b", "c"}
	delta := crs.NewConstraintDelta(crs.SignalSourceSoft)
	delta.Add = []crs.Constraint{
		{ID: "c1", Type: crs.ConstraintTypeMutualExclusion, Nodes: originalNodes},
	}

	constraints := extractConstraints(delta, 0)

	// Modify original slice
	originalNodes[0] = "modified"

	// Extracted nodes should not be affected
	assert.Equal(t, "a", constraints[0].Nodes[0])
}

func TestExtractConstraints_CompositeDelta(t *testing.T) {
	constraint1 := crs.NewConstraintDelta(crs.SignalSourceSoft)
	constraint1.Add = []crs.Constraint{
		{ID: "c1", Type: crs.ConstraintTypeMutualExclusion, Nodes: []string{"a"}},
	}

	constraint2 := crs.NewConstraintDelta(crs.SignalSourceSoft)
	constraint2.Add = []crs.Constraint{
		{ID: "c2", Type: crs.ConstraintTypeImplication, Nodes: []string{"b"}},
	}

	composite := crs.NewCompositeDelta(constraint1, constraint2)
	constraints := extractConstraints(composite, 0)

	require.Len(t, constraints, 2)
}

// -----------------------------------------------------------------------------
// extractDependencies Tests
// -----------------------------------------------------------------------------

func TestExtractDependencies_NilDelta(t *testing.T) {
	deps := extractDependencies(nil, 0)
	assert.NotNil(t, deps)
	assert.Empty(t, deps)
}

func TestExtractDependencies_EmptyDependencyDelta(t *testing.T) {
	delta := crs.NewDependencyDelta(crs.SignalSourceSoft)
	deps := extractDependencies(delta, 0)
	assert.NotNil(t, deps)
	assert.Empty(t, deps)
}

func TestExtractDependencies_AddedEdges(t *testing.T) {
	delta := crs.NewDependencyDelta(crs.SignalSourceSoft)
	delta.AddEdges = [][2]string{
		{"from1", "to1"},
		{"from2", "to2"},
	}

	deps := extractDependencies(delta, 0)

	require.Len(t, deps, 2)
	assert.Equal(t, "from1", deps[0].From)
	assert.Equal(t, "to1", deps[0].To)
	assert.Equal(t, "from2", deps[1].From)
	assert.Equal(t, "to2", deps[1].To)
}

func TestExtractDependencies_RemovedEdgesIgnored(t *testing.T) {
	delta := crs.NewDependencyDelta(crs.SignalSourceSoft)
	delta.RemoveEdges = [][2]string{
		{"from1", "to1"},
	}

	deps := extractDependencies(delta, 0)

	// RemoveEdges should not be included in trace
	assert.Empty(t, deps)
}

func TestExtractDependencies_CompositeDelta(t *testing.T) {
	dep1 := crs.NewDependencyDelta(crs.SignalSourceSoft)
	dep1.AddEdges = [][2]string{{"a", "b"}}

	dep2 := crs.NewDependencyDelta(crs.SignalSourceSoft)
	dep2.AddEdges = [][2]string{{"c", "d"}}

	composite := crs.NewCompositeDelta(dep1, dep2)
	deps := extractDependencies(composite, 0)

	require.Len(t, deps, 2)
}

// -----------------------------------------------------------------------------
// extractTarget Tests
// -----------------------------------------------------------------------------

func TestExtractTarget_NilInput(t *testing.T) {
	target := extractTarget(nil)
	assert.Empty(t, target)
}

func TestExtractTarget_WithTargetMethod(t *testing.T) {
	input := mockActivityInput{
		typeName: "explore",
		target:   "main.go",
	}

	target := extractTarget(input)
	assert.Equal(t, "main.go", target)
}

func TestExtractTarget_WithFilePathMethod(t *testing.T) {
	input := mockInputWithFilePath{
		typeName: "analyze",
		filePath: "/path/to/file.go",
	}

	target := extractTarget(input)
	assert.Equal(t, "/path/to/file.go", target)
}

func TestExtractTarget_BaseInput(t *testing.T) {
	// BaseInput doesn't have Target() or FilePath()
	input := activities.NewBaseInput("req-123", crs.SignalSourceSoft)

	target := extractTarget(input)
	assert.Empty(t, target)
}

// -----------------------------------------------------------------------------
// extractSymbolsFound Tests
// -----------------------------------------------------------------------------

func TestExtractSymbolsFound_NilResults(t *testing.T) {
	symbols := extractSymbolsFound(nil)
	assert.NotNil(t, symbols)
	assert.Empty(t, symbols)
}

func TestExtractSymbolsFound_EmptyResults(t *testing.T) {
	symbols := extractSymbolsFound([]*algorithms.Result{})
	assert.NotNil(t, symbols)
	assert.Empty(t, symbols)
}

func TestExtractSymbolsFound_NilResult(t *testing.T) {
	results := []*algorithms.Result{nil}
	symbols := extractSymbolsFound(results)
	assert.NotNil(t, symbols)
	assert.Empty(t, symbols)
}

func TestExtractSymbolsFound_FailedResult(t *testing.T) {
	results := []*algorithms.Result{
		{
			Name:   "test",
			Output: mockOutputWithSymbols{symbols: []string{"sym1"}},
			Err:    assert.AnError,
		},
	}

	symbols := extractSymbolsFound(results)
	assert.Empty(t, symbols) // Failed results should be skipped
}

func TestExtractSymbolsFound_WithSymbolsProvider(t *testing.T) {
	results := []*algorithms.Result{
		{
			Name:   "test",
			Output: mockOutputWithSymbols{symbols: []string{"func1", "func2"}},
		},
	}

	symbols := extractSymbolsFound(results)

	require.Len(t, symbols, 2)
	assert.Contains(t, symbols, "func1")
	assert.Contains(t, symbols, "func2")
}

func TestExtractSymbolsFound_WithStringSlice(t *testing.T) {
	results := []*algorithms.Result{
		{
			Name:   "test",
			Output: []string{"sym1", "sym2", "sym3"},
		},
	}

	symbols := extractSymbolsFound(results)

	require.Len(t, symbols, 3)
}

func TestExtractSymbolsFound_MultipleResults(t *testing.T) {
	results := []*algorithms.Result{
		{
			Name:   "test1",
			Output: mockOutputWithSymbols{symbols: []string{"a", "b"}},
		},
		{
			Name:   "test2",
			Output: []string{"c", "d"},
		},
	}

	symbols := extractSymbolsFound(results)

	require.Len(t, symbols, 4)
}

func TestExtractSymbolsFound_SymbolsCopied(t *testing.T) {
	original := []string{"a", "b"}
	results := []*algorithms.Result{
		{
			Name:   "test",
			Output: original,
		},
	}

	symbols := extractSymbolsFound(results)

	// Modify original
	original[0] = "modified"

	// Extracted should not be affected
	assert.Equal(t, "a", symbols[0])
}

// -----------------------------------------------------------------------------
// extractSymbolsFromOutput Tests
// -----------------------------------------------------------------------------

func TestExtractSymbolsFromOutput_NilOutput(t *testing.T) {
	symbols := extractSymbolsFromOutput(nil)
	assert.Nil(t, symbols)
}

func TestExtractSymbolsFromOutput_UnknownType(t *testing.T) {
	symbols := extractSymbolsFromOutput(42)
	assert.Nil(t, symbols)
}

func TestExtractSymbolsFromOutput_EmptySymbols(t *testing.T) {
	output := mockOutputWithSymbols{symbols: []string{}}
	symbols := extractSymbolsFromOutput(output)
	assert.Nil(t, symbols)
}

func TestExtractSymbolsFromOutput_EmptySlice(t *testing.T) {
	symbols := extractSymbolsFromOutput([]string{})
	assert.Nil(t, symbols)
}
