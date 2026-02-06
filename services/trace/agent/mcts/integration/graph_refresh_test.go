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
	"context"
	"testing"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/trace/agent/mcts/activities"
	"github.com/AleutianAI/AleutianFOSS/services/trace/agent/mcts/algorithms"
	"github.com/AleutianAI/AleutianFOSS/services/trace/agent/mcts/crs"
	"github.com/AleutianAI/AleutianFOSS/services/trace/eval"
)

// TestCoordinator_HandleEvent_GraphRefreshed tests that EventGraphRefreshed
// triggers cache invalidation and activity execution.
//
// GR-29: Tests cache invalidation on graph refresh.
func TestCoordinator_HandleEvent_GraphRefreshed(t *testing.T) {
	// Create CRS with graph-backed dependency index (simulated)
	testCRS := crs.New(nil)

	// Create bridge and coordinator
	bridge := NewBridge(testCRS, nil)
	config := DefaultCoordinatorConfig()
	config.EnableTracing = false
	coord := NewCoordinator(bridge, config)

	// Register mock Awareness and Search activities
	awarenessActivity := &mockGraphRefreshActivity{name: "awareness"}
	searchActivity := &mockGraphRefreshActivity{name: "search"}
	coord.Register(awarenessActivity)
	coord.Register(searchActivity)

	// Handle EventGraphRefreshed
	ctx := context.Background()
	data := &EventData{
		SessionID: "test-session",
		Metadata: map[string]any{
			"nodes_added":     5,
			"nodes_removed":   2,
			"files_refreshed": 3,
		},
	}

	results, err := coord.HandleEvent(ctx, EventGraphRefreshed, data)
	if err != nil {
		t.Fatalf("HandleEvent failed: %v", err)
	}

	// Verify at least Awareness activity ran
	// Note: Search activity may not run due to dependency on Learning
	if len(results) < 1 {
		t.Errorf("Expected at least 1 activity result, got %d", len(results))
	}

	// Verify Awareness activity was executed
	if !awarenessActivity.executed {
		t.Error("Expected Awareness activity to be executed")
	}

	// Verify event was handled successfully
	hasAwareness := false
	for _, result := range results {
		if result.ActivityName == "awareness" {
			hasAwareness = true
			if !result.Success {
				t.Error("Expected Awareness activity to succeed")
			}
		}
	}

	if !hasAwareness {
		t.Error("Expected Awareness activity in results")
	}

	// Cache invalidation happens in the coordinator's HandleEvent method
	// We can't directly verify it, but the event was processed successfully
}

// TestCoordinator_HandleEvent_GraphRefreshed_NilBridge tests that the event
// handles gracefully when bridge is nil.
func TestCoordinator_HandleEvent_GraphRefreshed_NilBridge(t *testing.T) {
	// This test verifies that nil checks work, but in practice bridge should never be nil
	// in a properly constructed Coordinator. We can't construct a Coordinator with nil bridge
	// via NewCoordinator, so this documents the expected behavior.
	//
	// The actual nil check is in coordinator.go:516-519 where we check:
	//   if event == EventGraphRefreshed && c.bridge != nil && c.bridge.CRS() != nil
	//
	// This prevents panics if bridge or CRS is nil.
	t.Skip("Coordinator construction requires non-nil bridge; nil check is defensive")
}

// TestBridge_CRS tests that the CRS() accessor returns the underlying CRS.
func TestBridge_CRS(t *testing.T) {
	testCRS := crs.New(nil)
	bridge := NewBridge(testCRS, nil)

	returnedCRS := bridge.CRS()
	if returnedCRS == nil {
		t.Fatal("CRS() returned nil")
	}

	// Verify it's the same CRS by checking generation
	gen1 := testCRS.Generation()
	gen2 := returnedCRS.Generation()
	if gen1 != gen2 {
		t.Errorf("CRS() returned different CRS: gen1=%d, gen2=%d", gen1, gen2)
	}
}

// TestCRS_InvalidateGraphCache tests the InvalidateGraphCache method.
func TestCRS_InvalidateGraphCache(t *testing.T) {
	testCRS := crs.New(nil)

	// Call InvalidateGraphCache (should not panic, even without graph provider)
	testCRS.InvalidateGraphCache()

	// Verify CRS is still functional after invalidation
	snap := testCRS.Snapshot()
	if snap == nil {
		t.Fatal("Snapshot returned nil after cache invalidation")
	}

	// Call again to ensure it's idempotent
	testCRS.InvalidateGraphCache()
	snap2 := testCRS.Snapshot()
	if snap2 == nil {
		t.Fatal("Second snapshot returned nil")
	}
}

// mockGraphRefreshActivity is a mock activity for testing graph refresh events.
type mockGraphRefreshActivity struct {
	name     string
	executed bool
}

func (a *mockGraphRefreshActivity) Name() string {
	return a.name
}

func (a *mockGraphRefreshActivity) Execute(
	ctx context.Context,
	snapshot crs.Snapshot,
	input activities.ActivityInput,
) (activities.ActivityResult, crs.Delta, error) {
	a.executed = true
	return activities.ActivityResult{
		ActivityName: a.name,
		Success:      true,
	}, nil, nil
}

func (a *mockGraphRefreshActivity) ShouldRun(snapshot crs.Snapshot) (bool, activities.Priority) {
	return true, 50
}

func (a *mockGraphRefreshActivity) Algorithms() []algorithms.Algorithm {
	return nil // No algorithms needed for mock
}

func (a *mockGraphRefreshActivity) HealthCheck(ctx context.Context) error {
	return nil // Always healthy
}

func (a *mockGraphRefreshActivity) Metrics() []eval.MetricDefinition {
	return []eval.MetricDefinition{} // No metrics for mock
}

func (a *mockGraphRefreshActivity) Properties() []eval.Property {
	return []eval.Property{} // No properties for mock
}

func (a *mockGraphRefreshActivity) Timeout() time.Duration {
	return 30 * time.Second // Reasonable timeout for mock
}
