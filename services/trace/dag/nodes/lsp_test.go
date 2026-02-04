// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package nodes

import (
	"context"
	"errors"
	"testing"

	"github.com/AleutianAI/AleutianFOSS/services/trace/dag"
	"github.com/AleutianAI/AleutianFOSS/services/trace/lsp"
)

func TestLSPSpawnNode_ImplementsRehydratableNode(t *testing.T) {
	// Verify that LSPSpawnNode implements the RehydratableNode interface
	var _ dag.RehydratableNode = (*LSPSpawnNode)(nil)
}

func TestLSPSpawnNode_OnResume_NilManager(t *testing.T) {
	node := &LSPSpawnNode{
		BaseNode: dag.BaseNode{NodeName: "LSP_SPAWN"},
		manager:  nil,
	}

	err := node.OnResume(context.Background(), nil)
	if err == nil {
		t.Error("expected error for nil manager")
	}
	if !errors.Is(err, dag.ErrRehydrationFailed) {
		t.Errorf("expected ErrRehydrationFailed, got: %v", err)
	}
}

func TestLSPSpawnNode_OnResume_InvalidOutputType(t *testing.T) {
	config := lsp.DefaultManagerConfig()
	mgr := lsp.NewManager("/tmp/test", config)
	defer mgr.ShutdownAll(context.Background())

	node := NewLSPSpawnNode(mgr, nil)

	// Pass wrong type as output
	err := node.OnResume(context.Background(), "wrong_type")
	if err == nil {
		t.Error("expected error for invalid output type")
	}
	if !errors.Is(err, dag.ErrRehydrationFailed) {
		t.Errorf("expected ErrRehydrationFailed, got: %v", err)
	}
}

func TestLSPSpawnNode_OnResume_NilOutput(t *testing.T) {
	config := lsp.DefaultManagerConfig()
	mgr := lsp.NewManager("/tmp/test", config)
	defer mgr.ShutdownAll(context.Background())

	node := NewLSPSpawnNode(mgr, nil)

	// Pass nil as output
	err := node.OnResume(context.Background(), nil)
	if err == nil {
		t.Error("expected error for nil output")
	}
	if !errors.Is(err, dag.ErrRehydrationFailed) {
		t.Errorf("expected ErrRehydrationFailed, got: %v", err)
	}
}

func TestLSPSpawnNode_OnResume_EmptySpawnedList(t *testing.T) {
	config := lsp.DefaultManagerConfig()
	mgr := lsp.NewManager("/tmp/test", config)
	defer mgr.ShutdownAll(context.Background())

	node := NewLSPSpawnNode(mgr, nil)

	// Create valid output with empty spawned list
	output := &LSPSpawnOutput{
		Spawned: []string{},
		Failed:  []LSPSpawnError{},
		Manager: mgr,
	}

	err := node.OnResume(context.Background(), output)
	if err != nil {
		t.Errorf("expected no error for empty spawned list, got: %v", err)
	}

	// Verify output references were updated
	if output.Manager != mgr {
		t.Error("manager reference not updated")
	}
	if output.Operations == nil {
		t.Error("operations reference not set")
	}
}

func TestLSPSpawnNode_OnResume_UpdatesOutputReferences(t *testing.T) {
	config := lsp.DefaultManagerConfig()
	mgr := lsp.NewManager("/tmp/test", config)
	defer mgr.ShutdownAll(context.Background())

	node := NewLSPSpawnNode(mgr, nil)

	// Create output with nil manager/operations (simulating checkpoint load)
	output := &LSPSpawnOutput{
		Spawned:    []string{},
		Manager:    nil, // Would be nil after JSON unmarshal
		Operations: nil, // Would be nil after JSON unmarshal
	}

	err := node.OnResume(context.Background(), output)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// Verify output references were restored
	if output.Manager != mgr {
		t.Error("manager reference not restored")
	}
	if output.Operations == nil {
		t.Error("operations reference not restored")
	}
}

func TestLSPSpawnNode_Name(t *testing.T) {
	node := NewLSPSpawnNode(nil, nil)
	if node.Name() != "LSP_SPAWN" {
		t.Errorf("Name() = %q, want LSP_SPAWN", node.Name())
	}
}

func TestLSPSpawnNode_Dependencies(t *testing.T) {
	deps := []string{"PARSE", "GRAPH"}
	node := NewLSPSpawnNode(nil, deps)

	got := node.Dependencies()
	if len(got) != 2 || got[0] != "PARSE" || got[1] != "GRAPH" {
		t.Errorf("Dependencies() = %v, want %v", got, deps)
	}
}

func TestLSPSpawnNode_Execute_NilManager(t *testing.T) {
	node := NewLSPSpawnNode(nil, nil)

	_, err := node.Execute(context.Background(), map[string]any{
		"languages": []string{"go"},
	})

	if err == nil {
		t.Error("expected error for nil manager")
	}
	if !errors.Is(err, ErrNilDependency) {
		t.Errorf("expected ErrNilDependency, got: %v", err)
	}
}

func TestLSPSpawnNode_Execute_MissingLanguages(t *testing.T) {
	config := lsp.DefaultManagerConfig()
	mgr := lsp.NewManager("/tmp/test", config)
	defer mgr.ShutdownAll(context.Background())

	node := NewLSPSpawnNode(mgr, nil)

	_, err := node.Execute(context.Background(), map[string]any{})

	if err == nil {
		t.Error("expected error for missing languages")
	}
	if !errors.Is(err, ErrMissingInput) {
		t.Errorf("expected ErrMissingInput, got: %v", err)
	}
}

func TestLSPSpawnNode_Execute_InvalidLanguagesType(t *testing.T) {
	config := lsp.DefaultManagerConfig()
	mgr := lsp.NewManager("/tmp/test", config)
	defer mgr.ShutdownAll(context.Background())

	node := NewLSPSpawnNode(mgr, nil)

	_, err := node.Execute(context.Background(), map[string]any{
		"languages": 123, // wrong type
	})

	if err == nil {
		t.Error("expected error for invalid languages type")
	}
	if !errors.Is(err, ErrInvalidInputType) {
		t.Errorf("expected ErrInvalidInputType, got: %v", err)
	}
}
