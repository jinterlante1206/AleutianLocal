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
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestSaveCheckpoint_Basic(t *testing.T) {
	state := NewState("test-session")
	state.SetCompleted("node-a", "output-a")
	state.SetCompleted("node-b", 42)

	path := filepath.Join(t.TempDir(), "checkpoint.json")

	err := SaveCheckpoint(state, "test-dag", path)
	if err != nil {
		t.Fatalf("SaveCheckpoint: %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("checkpoint file not created: %v", err)
	}
}

func TestSaveCheckpoint_NilState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "checkpoint.json")

	err := SaveCheckpoint(nil, "test-dag", path)
	if err == nil {
		t.Fatal("expected error for nil state")
	}
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("expected ErrInvalidInput, got: %v", err)
	}
}

func TestSaveCheckpoint_EmptyPath(t *testing.T) {
	state := NewState("test-session")

	err := SaveCheckpoint(state, "test-dag", "")
	if err == nil {
		t.Fatal("expected error for empty path")
	}
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("expected ErrInvalidInput, got: %v", err)
	}
}

func TestSaveCheckpoint_EmptyDAGName(t *testing.T) {
	state := NewState("test-session")
	path := filepath.Join(t.TempDir(), "checkpoint.json")

	err := SaveCheckpoint(state, "", path)
	if err == nil {
		t.Fatal("expected error for empty dagName")
	}
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("expected ErrInvalidInput, got: %v", err)
	}
}

func TestSaveCheckpoint_InvalidDAGName(t *testing.T) {
	state := NewState("test-session")
	path := filepath.Join(t.TempDir(), "checkpoint.json")

	testCases := []struct {
		name    string
		dagName string
	}{
		{"spaces", "my dag"},
		{"special chars", "my@dag!"},
		{"dots", "my.dag"},
		{"slashes", "my/dag"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := SaveCheckpoint(state, tc.dagName, path)
			if err == nil {
				t.Fatalf("expected error for invalid dagName %q", tc.dagName)
			}
			if !errors.Is(err, ErrInvalidInput) {
				t.Errorf("expected ErrInvalidInput, got: %v", err)
			}
		})
	}
}

func TestSaveCheckpoint_ValidDAGNames(t *testing.T) {
	state := NewState("test-session")
	dir := t.TempDir()

	validNames := []string{
		"simple",
		"with-dashes",
		"with_underscores",
		"CamelCase",
		"mixedCase123",
		"UPPERCASE",
		"a1b2c3",
	}

	for _, name := range validNames {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(dir, name+".json")
			err := SaveCheckpoint(state, name, path)
			if err != nil {
				t.Errorf("unexpected error for valid dagName %q: %v", name, err)
			}
		})
	}
}

func TestLoadCheckpoint_Basic(t *testing.T) {
	// First save a checkpoint
	state := NewState("test-session")
	state.SetCompleted("node-a", "output-a")
	state.SetCompleted("node-b", float64(42)) // JSON numbers are float64

	path := filepath.Join(t.TempDir(), "checkpoint.json")

	if err := SaveCheckpoint(state, "test-dag", path); err != nil {
		t.Fatalf("SaveCheckpoint: %v", err)
	}

	// Now load it
	loaded, err := LoadCheckpoint(path)
	if err != nil {
		t.Fatalf("LoadCheckpoint: %v", err)
	}

	if loaded.State.SessionID != "test-session" {
		t.Errorf("SessionID = %q, want %q", loaded.State.SessionID, "test-session")
	}

	if loaded.DAGName != "test-dag" {
		t.Errorf("DAGName = %q, want %q", loaded.DAGName, "test-dag")
	}

	if loaded.Version != CheckpointVersion {
		t.Errorf("Version = %q, want %q", loaded.Version, CheckpointVersion)
	}

	if !loaded.State.IsCompleted("node-a") {
		t.Error("node-a should be completed")
	}

	if !loaded.State.IsCompleted("node-b") {
		t.Error("node-b should be completed")
	}

	// Check output values
	outputA, _ := loaded.State.GetOutput("node-a")
	if outputA != "output-a" {
		t.Errorf("node-a output = %v, want %v", outputA, "output-a")
	}

	outputB, _ := loaded.State.GetOutput("node-b")
	if outputB != float64(42) {
		t.Errorf("node-b output = %v, want %v", outputB, 42)
	}
}

func TestLoadCheckpoint_EmptyPath(t *testing.T) {
	_, err := LoadCheckpoint("")
	if err == nil {
		t.Fatal("expected error for empty path")
	}
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("expected ErrInvalidInput, got: %v", err)
	}
}

func TestLoadCheckpoint_FileNotFound(t *testing.T) {
	_, err := LoadCheckpoint("/nonexistent/path/checkpoint.json")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoadCheckpoint_InvalidJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "invalid.json")
	if err := os.WriteFile(path, []byte("not valid json"), 0644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	_, err := LoadCheckpoint(path)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestLoadCheckpoint_CorruptChecksum(t *testing.T) {
	// Save a valid checkpoint
	state := NewState("test-session")
	state.SetCompleted("node-a", "output-a")

	path := filepath.Join(t.TempDir(), "checkpoint.json")
	if err := SaveCheckpoint(state, "test-dag", path); err != nil {
		t.Fatalf("SaveCheckpoint: %v", err)
	}

	// Read and corrupt the file
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}

	// Corrupt by replacing some content
	corrupted := make([]byte, len(data))
	copy(corrupted, data)
	for i := range corrupted {
		if corrupted[i] == 'a' {
			corrupted[i] = 'z'
			break
		}
	}

	if err := os.WriteFile(path, corrupted, 0644); err != nil {
		t.Fatalf("write corrupted file: %v", err)
	}

	_, err = LoadCheckpoint(path)
	if err == nil {
		t.Fatal("expected error for corrupt checkpoint")
	}
	if !errors.Is(err, ErrCheckpointCorrupt) {
		t.Errorf("expected ErrCheckpointCorrupt, got: %v", err)
	}
}

func TestCheckpoint_Verify(t *testing.T) {
	state := NewState("test-session")
	state.SetCompleted("node-a", "output-a")

	path := filepath.Join(t.TempDir(), "checkpoint.json")
	if err := SaveCheckpoint(state, "test-dag", path); err != nil {
		t.Fatalf("SaveCheckpoint: %v", err)
	}

	loaded, err := LoadCheckpoint(path)
	if err != nil {
		t.Fatalf("LoadCheckpoint: %v", err)
	}

	if !loaded.Verify() {
		t.Error("Verify should return true for valid checkpoint")
	}

	// Corrupt the checkpoint in memory
	loaded.State.SessionID = "tampered"
	if loaded.Verify() {
		t.Error("Verify should return false for tampered checkpoint")
	}
}

func TestCheckpoint_Verify_Nil(t *testing.T) {
	var c *Checkpoint
	if c.Verify() {
		t.Error("Verify should return false for nil checkpoint")
	}

	c = &Checkpoint{}
	if c.Verify() {
		t.Error("Verify should return false for checkpoint with nil state")
	}
}

func TestSaveLoadCheckpoint_Roundtrip(t *testing.T) {
	// Create state with various data types
	state := NewState("roundtrip-session")
	state.SetCompleted("string-node", "hello")
	state.SetCompleted("number-node", float64(123.45))
	state.SetCompleted("bool-node", true)
	state.SetCompleted("list-node", []any{"a", "b", "c"})
	state.SetCompleted("map-node", map[string]any{"key": "value"})
	state.SetStatus("pending-node", NodeStatusPending)

	path := filepath.Join(t.TempDir(), "roundtrip.json")

	// Save
	if err := SaveCheckpoint(state, "roundtrip-dag", path); err != nil {
		t.Fatalf("SaveCheckpoint: %v", err)
	}

	// Load
	loaded, err := LoadCheckpoint(path)
	if err != nil {
		t.Fatalf("LoadCheckpoint: %v", err)
	}

	// Verify all data roundtripped correctly
	if loaded.State.SessionID != "roundtrip-session" {
		t.Errorf("SessionID = %q, want %q", loaded.State.SessionID, "roundtrip-session")
	}

	if loaded.DAGName != "roundtrip-dag" {
		t.Errorf("DAGName = %q, want %q", loaded.DAGName, "roundtrip-dag")
	}

	// Check completed nodes
	expectedCompleted := []string{"string-node", "number-node", "bool-node", "list-node", "map-node"}
	for _, name := range expectedCompleted {
		if !loaded.State.IsCompleted(name) {
			t.Errorf("node %q should be completed", name)
		}
	}

	// Check status
	if loaded.State.GetStatus("pending-node") != NodeStatusPending {
		t.Errorf("pending-node status = %v, want %v", loaded.State.GetStatus("pending-node"), NodeStatusPending)
	}
}

func TestSaveCheckpoint_Concurrent(t *testing.T) {
	state := NewState("concurrent-session")
	dir := t.TempDir()

	// Run multiple concurrent saves
	var wg sync.WaitGroup
	errCh := make(chan error, 10)

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			// Modify state (safely)
			nodeName := "node-" + string(rune('a'+idx))
			state.SetCompleted(nodeName, idx)

			// Save to unique file
			path := filepath.Join(dir, "checkpoint-"+string(rune('a'+idx))+".json")
			if err := SaveCheckpoint(state, "concurrent-dag", path); err != nil {
				errCh <- err
			}
		}(i)
	}

	wg.Wait()
	close(errCh)

	// Check for errors
	for err := range errCh {
		t.Errorf("concurrent save error: %v", err)
	}
}

func TestLoadCheckpoint_VersionMismatch(t *testing.T) {
	// Save a checkpoint and manually modify the version
	state := NewState("test-session")
	path := filepath.Join(t.TempDir(), "checkpoint.json")

	if err := SaveCheckpoint(state, "test-dag", path); err != nil {
		t.Fatalf("SaveCheckpoint: %v", err)
	}

	// Read file and modify version
	data, _ := os.ReadFile(path)
	// This is a bit hacky but works for testing
	modified := []byte(`{"state":{"session_id":"test-session","started_at":"2024-01-01T00:00:00Z","completed_nodes":{},"node_outputs":{},"node_statuses":{},"current_nodes":[]},"timestamp":"2024-01-01T00:00:00Z","version":"0.0","checksum":"invalid","dag_name":"test-dag"}`)
	_ = data // silence unused variable

	if err := os.WriteFile(path, modified, 0644); err != nil {
		t.Fatalf("write modified file: %v", err)
	}

	_, err := LoadCheckpoint(path)
	if err == nil {
		t.Fatal("expected error for version mismatch")
	}
	if !errors.Is(err, ErrCheckpointVersionMismatch) {
		t.Errorf("expected ErrCheckpointVersionMismatch, got: %v", err)
	}
}

func TestExecutor_Resume_Basic(t *testing.T) {
	// Build a simple DAG: A -> B -> C
	builder := NewBuilder("resume-test")
	builder.AddNode(NewFuncNode("A", nil, func(ctx context.Context, inputs map[string]any) (any, error) {
		return "output-a", nil
	}))
	builder.AddNode(NewFuncNode("B", []string{"A"}, func(ctx context.Context, inputs map[string]any) (any, error) {
		return "output-b", nil
	}))
	builder.AddNode(NewFuncNode("C", []string{"B"}, func(ctx context.Context, inputs map[string]any) (any, error) {
		return "output-c", nil
	}))

	dag, err := builder.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	executor, err := NewExecutor(dag, nil)
	if err != nil {
		t.Fatalf("NewExecutor: %v", err)
	}

	// Create a checkpoint with A completed
	state := NewState("resume-session")
	state.NodeOutputs["root"] = nil
	state.SetCompleted("A", "output-a")

	path := filepath.Join(t.TempDir(), "checkpoint.json")
	if err := SaveCheckpoint(state, "resume-test", path); err != nil {
		t.Fatalf("SaveCheckpoint: %v", err)
	}

	// Load and resume
	checkpoint, err := LoadCheckpoint(path)
	if err != nil {
		t.Fatalf("LoadCheckpoint: %v", err)
	}

	ctx := context.Background()
	result, err := executor.Resume(ctx, checkpoint)
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}

	if !result.Success {
		t.Errorf("expected success, got error: %s", result.Error)
	}

	// Should have executed B and C (A was already done)
	// NodesExecuted counts from the resume point
	if result.NodesExecuted != 3 { // All 3 are in state as completed
		t.Errorf("NodesExecuted = %d, want 3", result.NodesExecuted)
	}

	if result.Output != "output-c" {
		t.Errorf("Output = %v, want %v", result.Output, "output-c")
	}
}

func TestExecutor_Resume_NilContext(t *testing.T) {
	builder := NewBuilder("test")
	builder.AddNode(NewFuncNode("A", nil, func(ctx context.Context, inputs map[string]any) (any, error) {
		return nil, nil
	}))

	dag, _ := builder.Build()
	executor, _ := NewExecutor(dag, nil)

	checkpoint := &Checkpoint{
		State:     NewState("test"),
		Timestamp: time.Now().UnixMilli(),
		Version:   CheckpointVersion,
		DAGName:   "test",
	}
	checkpoint.Checksum = "will-be-recalculated"

	_, err := executor.Resume(nil, checkpoint)
	if err == nil {
		t.Fatal("expected error for nil context")
	}
	if !errors.Is(err, ErrNilContext) {
		t.Errorf("expected ErrNilContext, got: %v", err)
	}
}

func TestExecutor_Resume_NilCheckpoint(t *testing.T) {
	builder := NewBuilder("test")
	builder.AddNode(NewFuncNode("A", nil, func(ctx context.Context, inputs map[string]any) (any, error) {
		return nil, nil
	}))

	dag, _ := builder.Build()
	executor, _ := NewExecutor(dag, nil)

	_, err := executor.Resume(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for nil checkpoint")
	}
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("expected ErrInvalidInput, got: %v", err)
	}
}

func TestExecutor_Resume_DAGMismatch(t *testing.T) {
	builder := NewBuilder("dag-one")
	builder.AddNode(NewFuncNode("A", nil, func(ctx context.Context, inputs map[string]any) (any, error) {
		return nil, nil
	}))

	dag, _ := builder.Build()
	executor, _ := NewExecutor(dag, nil)

	// Create checkpoint for a different DAG
	state := NewState("test")
	path := filepath.Join(t.TempDir(), "checkpoint.json")
	if err := SaveCheckpoint(state, "dag-two", path); err != nil {
		t.Fatalf("SaveCheckpoint: %v", err)
	}

	checkpoint, _ := LoadCheckpoint(path)

	_, err := executor.Resume(context.Background(), checkpoint)
	if err == nil {
		t.Fatal("expected error for DAG mismatch")
	}
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("expected ErrInvalidInput, got: %v", err)
	}
}

func TestExecutor_Resume_CorruptCheckpoint(t *testing.T) {
	builder := NewBuilder("test")
	builder.AddNode(NewFuncNode("A", nil, func(ctx context.Context, inputs map[string]any) (any, error) {
		return nil, nil
	}))

	dag, _ := builder.Build()
	executor, _ := NewExecutor(dag, nil)

	// Create a checkpoint with invalid checksum
	checkpoint := &Checkpoint{
		State:     NewState("test"),
		Timestamp: time.Now().UnixMilli(),
		Version:   CheckpointVersion,
		Checksum:  "invalid-checksum",
		DAGName:   "test",
	}

	_, err := executor.Resume(context.Background(), checkpoint)
	if err == nil {
		t.Fatal("expected error for corrupt checkpoint")
	}
	if !errors.Is(err, ErrCheckpointCorrupt) {
		t.Errorf("expected ErrCheckpointCorrupt, got: %v", err)
	}
}

func TestExecutor_Resume_FromFailedState(t *testing.T) {
	// Build a DAG where B fails on first attempt but succeeds on retry
	attempts := 0
	builder := NewBuilder("retry-test")
	builder.AddNode(NewFuncNode("A", nil, func(ctx context.Context, inputs map[string]any) (any, error) {
		return "output-a", nil
	}))
	builder.AddNode(NewFuncNode("B", []string{"A"}, func(ctx context.Context, inputs map[string]any) (any, error) {
		attempts++
		if attempts == 1 {
			return nil, errors.New("first attempt fails")
		}
		return "output-b", nil
	}))

	dag, _ := builder.Build()
	executor, _ := NewExecutor(dag, nil)

	// First run - will fail at B
	ctx := context.Background()
	result, _ := executor.Run(ctx, nil)
	if result.Success {
		t.Fatal("first run should fail")
	}

	// Create checkpoint from failed state
	state := NewState(result.SessionID)
	state.NodeOutputs["root"] = nil
	state.SetCompleted("A", "output-a")
	// B failed, so it's not completed

	path := filepath.Join(t.TempDir(), "checkpoint.json")
	if err := SaveCheckpoint(state, "retry-test", path); err != nil {
		t.Fatalf("SaveCheckpoint: %v", err)
	}

	// Load and resume - should succeed now
	checkpoint, _ := LoadCheckpoint(path)
	result, err := executor.Resume(ctx, checkpoint)
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}

	if !result.Success {
		t.Errorf("resume should succeed, got error: %s", result.Error)
	}

	if attempts != 2 {
		t.Errorf("attempts = %d, want 2", attempts)
	}
}

func TestCheckpointState_DeepCopy(t *testing.T) {
	// Verify that toCheckpointState creates a deep copy
	state := NewState("test")
	state.CompletedNodes["A"] = true
	state.NodeOutputs["A"] = "original"

	cs := state.toCheckpointState()

	// Modify original
	state.CompletedNodes["B"] = true
	state.NodeOutputs["A"] = "modified"

	// Copy should be unchanged
	if cs.CompletedNodes["B"] {
		t.Error("checkpoint state should not have B")
	}
	if cs.NodeOutputs["A"] != "original" {
		t.Error("checkpoint state output should be original")
	}
}

func TestCheckpointState_DeepCopy_NestedStructures(t *testing.T) {
	// Verify that toCheckpointState deep copies nested slices and maps
	state := NewState("test")

	// Create nested structures
	originalSlice := []any{"a", "b", "c"}
	originalMap := map[string]any{"key1": "value1", "key2": float64(42)}

	state.NodeOutputs["slice-node"] = originalSlice
	state.NodeOutputs["map-node"] = originalMap

	cs := state.toCheckpointState()

	// Modify the original slice (if shallow copy, this would affect cs)
	originalSlice[0] = "MODIFIED"

	// Modify the original map
	originalMap["key1"] = "MODIFIED"

	// Checkpoint state should be unchanged (deep copy)
	csSlice, ok := cs.NodeOutputs["slice-node"].([]any)
	if !ok {
		t.Fatal("slice-node should be []any")
	}
	if csSlice[0] == "MODIFIED" {
		t.Error("deep copy failed: slice modification affected checkpoint")
	}

	csMap, ok := cs.NodeOutputs["map-node"].(map[string]any)
	if !ok {
		t.Fatal("map-node should be map[string]any")
	}
	if csMap["key1"] == "MODIFIED" {
		t.Error("deep copy failed: map modification affected checkpoint")
	}
}
