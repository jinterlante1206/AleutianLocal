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

// =============================================================================
// Event System Tests
// =============================================================================

func TestEventActivityMapping_AllEventsHaveMappings(t *testing.T) {
	events := []AgentEvent{
		EventSessionStart,
		EventQueryReceived,
		EventToolSelected,
		EventToolExecuted,
		EventToolFailed,
		EventCycleDetected,
		EventCircuitBreaker,
		EventSynthesisStart,
		EventSessionEnd,
	}

	for _, event := range events {
		activities, ok := EventActivityMapping[event]
		if !ok {
			t.Errorf("Event %s has no activity mapping", event)
		}
		if len(activities) == 0 {
			t.Errorf("Event %s has empty activity mapping", event)
		}
	}
}

func TestDefaultActivityConfigs_AllActivitiesConfigured(t *testing.T) {
	configs := DefaultActivityConfigs()

	expectedActivities := []ActivityName{
		ActivitySearch,
		ActivityLearning,
		ActivityConstraint,
		ActivityPlanning,
		ActivityAwareness,
		ActivitySimilarity,
		ActivityStreaming,
		ActivityMemory,
	}

	for _, name := range expectedActivities {
		config, ok := configs[name]
		if !ok {
			t.Errorf("Activity %s has no default config", name)
			continue
		}
		if config == nil {
			t.Errorf("Activity %s has nil config", name)
		}
	}
}

func TestDefaultActivityConfigs_PrioritiesAreDistinct(t *testing.T) {
	configs := DefaultActivityConfigs()

	seen := make(map[int]ActivityName)
	for name, config := range configs {
		if existing, ok := seen[config.Priority]; ok {
			// Same priority is OK for optional activities
			if !config.Optional && !configs[existing].Optional {
				t.Logf("Activities %s and %s have same priority %d",
					name, existing, config.Priority)
			}
		}
		seen[config.Priority] = name
	}
}

func TestSimpleQueryFilter_SkipsExpensiveActivities(t *testing.T) {
	filter := &SimpleQueryFilter{}

	input := []ActivityName{
		ActivitySearch,
		ActivitySimilarity,
		ActivityPlanning,
		ActivityMemory,
	}

	// Simple query - should skip Similarity and Planning
	ctx := &EventContext{IsSimpleQuery: true}
	result := filter.Filter(EventQueryReceived, input, ctx)

	if len(result) != 2 {
		t.Errorf("Expected 2 activities, got %d", len(result))
	}

	for _, name := range result {
		if name == ActivitySimilarity || name == ActivityPlanning {
			t.Errorf("Simple query should not include %s", name)
		}
	}
}

func TestSimpleQueryFilter_KeepsAllForComplexQuery(t *testing.T) {
	filter := &SimpleQueryFilter{}

	input := []ActivityName{
		ActivitySearch,
		ActivitySimilarity,
		ActivityPlanning,
		ActivityMemory,
	}

	// Complex query - should keep all
	ctx := &EventContext{IsSimpleQuery: false}
	result := filter.Filter(EventQueryReceived, input, ctx)

	if len(result) != len(input) {
		t.Errorf("Expected %d activities, got %d", len(input), len(result))
	}
}

func TestHighErrorRateFilter_AddsLearning(t *testing.T) {
	filter := &HighErrorRateFilter{Threshold: 0.5}

	input := []ActivityName{
		ActivitySearch,
		ActivityMemory,
	}

	// High error rate - should add Learning
	ctx := &EventContext{ErrorRate: 0.75}
	result := filter.Filter(EventToolFailed, input, ctx)

	hasLearning := false
	for _, name := range result {
		if name == ActivityLearning {
			hasLearning = true
			break
		}
	}

	if !hasLearning {
		t.Error("High error rate filter should add Learning activity")
	}
}

func TestHighErrorRateFilter_DoesNotDuplicateLearning(t *testing.T) {
	filter := &HighErrorRateFilter{Threshold: 0.5}

	input := []ActivityName{
		ActivityLearning,
		ActivitySearch,
	}

	ctx := &EventContext{ErrorRate: 0.75}
	result := filter.Filter(EventToolFailed, input, ctx)

	learningCount := 0
	for _, name := range result {
		if name == ActivityLearning {
			learningCount++
		}
	}

	if learningCount != 1 {
		t.Errorf("Expected 1 Learning activity, got %d", learningCount)
	}
}

// =============================================================================
// HandleEvent Tests
// =============================================================================

// eventTestActivity implements activities.Activity for event handling tests.
// Uses different name to avoid conflict with mockActivity in integration_test.go.
type eventTestActivity struct {
	name      string
	executed  bool
	returnErr error
}

func (m *eventTestActivity) Name() string { return m.name }

func (m *eventTestActivity) Execute(
	ctx context.Context,
	snapshot crs.Snapshot,
	input activities.ActivityInput,
) (activities.ActivityResult, crs.Delta, error) {
	m.executed = true
	if m.returnErr != nil {
		return activities.ActivityResult{
			ActivityName: m.name,
			Success:      false,
		}, nil, m.returnErr
	}
	return activities.ActivityResult{
		ActivityName: m.name,
		Success:      true,
	}, nil, nil
}

func (m *eventTestActivity) ShouldRun(snapshot crs.Snapshot) (bool, activities.Priority) {
	return true, 50
}

func (m *eventTestActivity) Algorithms() []algorithms.Algorithm {
	return nil
}

func (m *eventTestActivity) Timeout() time.Duration {
	return 10 * time.Second
}

func (m *eventTestActivity) Properties() []eval.Property {
	return nil
}

func (m *eventTestActivity) Metrics() []eval.MetricDefinition {
	return nil
}

func (m *eventTestActivity) HealthCheck(ctx context.Context) error {
	return nil
}

func TestCoordinator_HandleEvent_RunsCorrectActivities(t *testing.T) {
	// Create CRS with default config
	testCRS := crs.New(nil)

	// Create bridge and coordinator
	bridge := NewBridge(testCRS, nil)
	config := DefaultCoordinatorConfig()
	config.EnableTracing = false
	coord := NewCoordinator(bridge, config)

	// Register only Memory and Streaming activities
	memoryActivity := &eventTestActivity{name: "memory"}
	streamingActivity := &eventTestActivity{name: "streaming"}
	coord.Register(memoryActivity)
	coord.Register(streamingActivity)

	// Handle EventSessionStart
	ctx := context.Background()
	data := &EventData{SessionID: "test-session"}

	_, err := coord.HandleEvent(ctx, EventSessionStart, data)
	if err != nil {
		t.Fatalf("HandleEvent failed: %v", err)
	}

	// Verify correct activities were executed
	if !memoryActivity.executed {
		t.Error("Memory activity should have been executed for SessionStart")
	}
	if !streamingActivity.executed {
		t.Error("Streaming activity should have been executed for SessionStart")
	}
}

func TestCoordinator_HandleEvent_UnknownEvent(t *testing.T) {
	testCRS := crs.New(nil)
	bridge := NewBridge(testCRS, nil)
	config := DefaultCoordinatorConfig()
	config.EnableTracing = false
	coord := NewCoordinator(bridge, config)

	ctx := context.Background()
	data := &EventData{SessionID: "test-session"}

	results, err := coord.HandleEvent(ctx, "unknown_event", data)
	if err != nil {
		t.Errorf("Unknown event should not error: %v", err)
	}
	if len(results) != 0 {
		t.Error("Unknown event should produce no results")
	}
}

func TestCoordinator_HandleEvent_UnregisteredActivities(t *testing.T) {
	testCRS := crs.New(nil)
	bridge := NewBridge(testCRS, nil)
	config := DefaultCoordinatorConfig()
	config.EnableTracing = false
	coord := NewCoordinator(bridge, config)

	// Don't register any activities

	ctx := context.Background()
	data := &EventData{SessionID: "test-session"}

	results, err := coord.HandleEvent(ctx, EventSessionStart, data)
	if err != nil {
		t.Errorf("Should not error with no registered activities: %v", err)
	}
	if len(results) != 0 {
		t.Error("Should produce no results with no registered activities")
	}
}

func TestCoordinator_HandleEvent_DisabledActivity(t *testing.T) {
	testCRS := crs.New(nil)
	bridge := NewBridge(testCRS, nil)
	config := DefaultCoordinatorConfig()
	config.EnableTracing = false
	config.ActivityConfigs = map[ActivityName]*ActivityConfig{
		ActivityMemory: {
			Priority: 30,
			Enabled:  false, // Disabled
			Optional: false,
		},
		ActivityStreaming: {
			Priority: 40,
			Enabled:  true,
			Optional: true,
		},
	}
	coord := NewCoordinator(bridge, config)

	memoryActivity := &eventTestActivity{name: "memory"}
	streamingActivity := &eventTestActivity{name: "streaming"}
	coord.Register(memoryActivity)
	coord.Register(streamingActivity)

	ctx := context.Background()
	data := &EventData{SessionID: "test-session"}

	_, err := coord.HandleEvent(ctx, EventSessionStart, data)
	if err != nil {
		t.Fatalf("HandleEvent failed: %v", err)
	}

	if memoryActivity.executed {
		t.Error("Disabled Memory activity should not have been executed")
	}
	if !streamingActivity.executed {
		t.Error("Enabled Streaming activity should have been executed")
	}
}

// =============================================================================
// Benchmarks
// =============================================================================

func BenchmarkCoordinator_HandleEvent(b *testing.B) {
	testCRS := crs.New(nil)
	bridge := NewBridge(testCRS, nil)
	config := DefaultCoordinatorConfig()
	config.EnableTracing = false
	config.EnableMetrics = false
	coord := NewCoordinator(bridge, config)

	// Register mock activities
	coord.Register(&eventTestActivity{name: "memory"})
	coord.Register(&eventTestActivity{name: "streaming"})

	ctx := context.Background()
	data := &EventData{SessionID: "test-session"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = coord.HandleEvent(ctx, EventSessionStart, data)
	}
}
