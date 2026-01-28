// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package events

import (
	"sync"
	"testing"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/agent"
)

func TestEmitter_Subscribe(t *testing.T) {
	emitter := NewEmitter()

	var received []Event
	subID := emitter.Subscribe(func(e *Event) {
		received = append(received, *e)
	})

	if subID == "" {
		t.Error("expected non-empty subscription ID")
	}
	if emitter.SubscriptionCount() != 1 {
		t.Errorf("SubscriptionCount = %d, want 1", emitter.SubscriptionCount())
	}

	// Emit an event
	emitter.Emit(TypeStateTransition, &StateTransitionData{
		FromState: agent.StateIdle,
		ToState:   agent.StateInit,
	})

	if len(received) != 1 {
		t.Fatalf("expected 1 event, got %d", len(received))
	}
	if received[0].Type != TypeStateTransition {
		t.Errorf("Type = %s, want %s", received[0].Type, TypeStateTransition)
	}
}

func TestEmitter_SubscribeWithFilter(t *testing.T) {
	emitter := NewEmitter()

	var received []Event
	emitter.SubscribeWithFilter(func(e *Event) {
		received = append(received, *e)
	}, func(e *Event) bool {
		return e.Step > 5
	})

	emitter.SetStep(3)
	emitter.Emit(TypeToolInvocation, nil) // Should be filtered out

	emitter.SetStep(10)
	emitter.Emit(TypeToolResult, nil) // Should pass filter

	if len(received) != 1 {
		t.Fatalf("expected 1 event, got %d", len(received))
	}
	if received[0].Type != TypeToolResult {
		t.Errorf("Type = %s, want %s", received[0].Type, TypeToolResult)
	}
}

func TestEmitter_SubscribeByType(t *testing.T) {
	emitter := NewEmitter()

	var received []Event
	emitter.Subscribe(func(e *Event) {
		received = append(received, *e)
	}, TypeError, TypeSafetyCheck)

	emitter.Emit(TypeStateTransition, nil) // Should be filtered
	emitter.Emit(TypeError, &ErrorData{Error: "test"})
	emitter.Emit(TypeToolInvocation, nil) // Should be filtered
	emitter.Emit(TypeSafetyCheck, &SafetyCheckData{Passed: true})

	if len(received) != 2 {
		t.Fatalf("expected 2 events, got %d", len(received))
	}
	if received[0].Type != TypeError {
		t.Errorf("received[0].Type = %s, want %s", received[0].Type, TypeError)
	}
	if received[1].Type != TypeSafetyCheck {
		t.Errorf("received[1].Type = %s, want %s", received[1].Type, TypeSafetyCheck)
	}
}

func TestEmitter_Unsubscribe(t *testing.T) {
	emitter := NewEmitter()

	callCount := 0
	subID := emitter.Subscribe(func(e *Event) {
		callCount++
	})

	emitter.Emit(TypeStateTransition, nil)
	if callCount != 1 {
		t.Errorf("callCount = %d, want 1", callCount)
	}

	if !emitter.Unsubscribe(subID) {
		t.Error("Unsubscribe should return true for existing subscription")
	}

	emitter.Emit(TypeStateTransition, nil)
	if callCount != 1 {
		t.Errorf("callCount after unsubscribe = %d, want 1", callCount)
	}

	if emitter.Unsubscribe(subID) {
		t.Error("Unsubscribe should return false for already removed subscription")
	}
}

func TestEmitter_SessionID(t *testing.T) {
	emitter := NewEmitter(WithSessionID("session-123"))

	var received *Event
	emitter.Subscribe(func(e *Event) {
		received = e
	})

	emitter.Emit(TypeStateTransition, nil)

	if received.SessionID != "session-123" {
		t.Errorf("SessionID = %s, want session-123", received.SessionID)
	}

	// Update session ID
	emitter.SetSessionID("session-456")
	emitter.Emit(TypeStateTransition, nil)

	if received.SessionID != "session-456" {
		t.Errorf("SessionID after update = %s, want session-456", received.SessionID)
	}
}

func TestEmitter_Step(t *testing.T) {
	emitter := NewEmitter()

	var received *Event
	emitter.Subscribe(func(e *Event) {
		received = e
	})

	emitter.SetStep(5)
	emitter.Emit(TypeStateTransition, nil)
	if received.Step != 5 {
		t.Errorf("Step = %d, want 5", received.Step)
	}

	step := emitter.IncrementStep()
	if step != 6 {
		t.Errorf("IncrementStep returned %d, want 6", step)
	}

	emitter.Emit(TypeStateTransition, nil)
	if received.Step != 6 {
		t.Errorf("Step after increment = %d, want 6", received.Step)
	}
}

func TestEmitter_Buffer(t *testing.T) {
	emitter := NewEmitter(WithBufferSize(5))

	for i := 0; i < 10; i++ {
		emitter.Emit(TypeStateTransition, nil)
	}

	buffer := emitter.GetBuffer()
	if len(buffer) != 5 {
		t.Errorf("buffer size = %d, want 5", len(buffer))
	}
}

func TestEmitter_GetBufferSince(t *testing.T) {
	emitter := NewEmitter()

	emitter.Emit(TypeStateTransition, nil)
	time.Sleep(10 * time.Millisecond)
	midpoint := time.Now()
	time.Sleep(10 * time.Millisecond)
	emitter.Emit(TypeToolInvocation, nil)
	emitter.Emit(TypeToolResult, nil)

	events := emitter.GetBufferSince(midpoint)
	if len(events) != 2 {
		t.Errorf("events since midpoint = %d, want 2", len(events))
	}
}

func TestEmitter_GetBufferByType(t *testing.T) {
	emitter := NewEmitter()

	emitter.Emit(TypeStateTransition, nil)
	emitter.Emit(TypeToolInvocation, nil)
	emitter.Emit(TypeStateTransition, nil)
	emitter.Emit(TypeToolResult, nil)

	transitions := emitter.GetBufferByType(TypeStateTransition)
	if len(transitions) != 2 {
		t.Errorf("state transitions = %d, want 2", len(transitions))
	}
}

func TestEmitter_ClearBuffer(t *testing.T) {
	emitter := NewEmitter()

	emitter.Emit(TypeStateTransition, nil)
	emitter.Emit(TypeToolInvocation, nil)

	emitter.ClearBuffer()

	if len(emitter.GetBuffer()) != 0 {
		t.Error("buffer should be empty after clear")
	}
}

func TestEmitter_Reset(t *testing.T) {
	emitter := NewEmitter()

	emitter.Subscribe(func(e *Event) {})
	emitter.SetSessionID("test")
	emitter.SetStep(10)
	emitter.Emit(TypeStateTransition, nil)

	emitter.Reset()

	if emitter.SubscriptionCount() != 0 {
		t.Error("subscriptions should be cleared")
	}
	if len(emitter.GetBuffer()) != 0 {
		t.Error("buffer should be cleared")
	}
}

func TestEmitter_ConcurrentAccess(t *testing.T) {
	emitter := NewEmitter()

	var wg sync.WaitGroup
	var mu sync.Mutex
	received := make([]Event, 0)

	// Subscribe
	emitter.Subscribe(func(e *Event) {
		mu.Lock()
		received = append(received, *e)
		mu.Unlock()
	})

	// Concurrent emits
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			emitter.Emit(TypeStateTransition, nil)
		}()
	}

	wg.Wait()

	mu.Lock()
	count := len(received)
	mu.Unlock()

	if count != 100 {
		t.Errorf("received %d events, want 100", count)
	}
}

func TestEmitter_Metadata(t *testing.T) {
	emitter := NewEmitter()

	var received *Event
	emitter.Subscribe(func(e *Event) {
		received = e
	})

	emitter.EmitWithMetadata(TypeStateTransition, nil, &EventMetadata{
		TraceID:  "trace123",
		Source:   "test",
		Priority: 5,
		Tags: map[string]string{
			"key1": "value1",
		},
	})

	if received.Metadata == nil {
		t.Fatal("expected metadata")
	}
	if received.Metadata.TraceID != "trace123" {
		t.Errorf("Metadata.TraceID = %v, want trace123", received.Metadata.TraceID)
	}
	if received.Metadata.Source != "test" {
		t.Errorf("Metadata.Source = %v, want test", received.Metadata.Source)
	}
	if received.Metadata.Priority != 5 {
		t.Errorf("Metadata.Priority = %v, want 5", received.Metadata.Priority)
	}
	if received.Metadata.Tags["key1"] != "value1" {
		t.Errorf("Metadata.Tags[key1] = %v, want value1", received.Metadata.Tags["key1"])
	}
}

func TestMockEmitter(t *testing.T) {
	mock := NewMockEmitter()

	mock.Emit(TypeStateTransition, nil)
	mock.Emit(TypeError, &ErrorData{Error: "test"})
	mock.Emit(TypeStateTransition, nil)

	if mock.EventCount() != 3 {
		t.Errorf("EventCount = %d, want 3", mock.EventCount())
	}

	transitions := mock.GetEventsByType(TypeStateTransition)
	if len(transitions) != 2 {
		t.Errorf("state transitions = %d, want 2", len(transitions))
	}

	mock.Clear()
	if mock.EventCount() != 0 {
		t.Error("events should be cleared")
	}
}

func TestMetricsCollector(t *testing.T) {
	collector := NewMetricsCollector()
	emitter := NewEmitter()
	emitter.Subscribe(collector.Handler())

	// Emit various events
	emitter.Emit(TypeSessionStart, &SessionStartData{Query: "test"})
	emitter.Emit(TypeToolInvocation, &ToolInvocationData{ToolName: "test_tool"})
	emitter.Emit(TypeToolResult, &ToolResultData{
		ToolName: "test_tool",
		Success:  true,
		Duration: 100 * time.Millisecond,
	})
	emitter.Emit(TypeLLMRequest, &LLMRequestData{
		Model:    "test-model",
		TokensIn: 100,
	})
	emitter.Emit(TypeLLMResponse, &LLMResponseData{
		Model:     "test-model",
		TokensOut: 50,
		Duration:  200 * time.Millisecond,
	})
	emitter.Emit(TypeError, &ErrorData{Error: "test error"})
	emitter.Emit(TypeSafetyCheck, &SafetyCheckData{Passed: true, Blocked: false})
	emitter.Emit(TypeStepComplete, &StepCompleteData{
		StepNumber: 1,
		Duration:   300 * time.Millisecond,
	})

	metrics := collector.GetMetrics()

	if metrics.SessionCount != 1 {
		t.Errorf("SessionCount = %d, want 1", metrics.SessionCount)
	}
	if metrics.ToolInvocations != 1 {
		t.Errorf("ToolInvocations = %d, want 1", metrics.ToolInvocations)
	}
	if metrics.LLMRequests != 1 {
		t.Errorf("LLMRequests = %d, want 1", metrics.LLMRequests)
	}
	if metrics.ErrorCount != 1 {
		t.Errorf("ErrorCount = %d, want 1", metrics.ErrorCount)
	}
	if metrics.SafetyChecks != 1 {
		t.Errorf("SafetyChecks = %d, want 1", metrics.SafetyChecks)
	}
	if metrics.StepCount != 1 {
		t.Errorf("StepCount = %d, want 1", metrics.StepCount)
	}
	if metrics.TotalInputTokens != 100 {
		t.Errorf("TotalInputTokens = %d, want 100", metrics.TotalInputTokens)
	}
	if metrics.TotalOutputTokens != 50 {
		t.Errorf("TotalOutputTokens = %d, want 50", metrics.TotalOutputTokens)
	}

	collector.Reset()
	metrics = collector.GetMetrics()
	if metrics.SessionCount != 0 {
		t.Error("metrics should be reset")
	}
}

func TestChannelHandler(t *testing.T) {
	t.Run("non-blocking", func(t *testing.T) {
		ch := make(chan Event, 2)
		handler := ChannelHandler(ch, false)

		handler(&Event{Type: TypeStateTransition})
		handler(&Event{Type: TypeToolInvocation})

		if len(ch) != 2 {
			t.Errorf("channel has %d events, want 2", len(ch))
		}
	})

	t.Run("drop on full", func(t *testing.T) {
		ch := make(chan Event, 1)
		handler := ChannelHandler(ch, true)

		handler(&Event{Type: TypeStateTransition})
		handler(&Event{Type: TypeToolInvocation}) // Should be dropped

		if len(ch) != 1 {
			t.Errorf("channel has %d events, want 1", len(ch))
		}
	})
}

func TestMultiHandler(t *testing.T) {
	callCount1 := 0
	callCount2 := 0

	handler := MultiHandler(
		func(e *Event) { callCount1++ },
		func(e *Event) { callCount2++ },
	)

	handler(&Event{Type: TypeStateTransition})

	if callCount1 != 1 || callCount2 != 1 {
		t.Errorf("callCount1=%d, callCount2=%d, want 1,1", callCount1, callCount2)
	}
}

func TestFilteredHandler(t *testing.T) {
	callCount := 0
	handler := FilteredHandler(
		func(e *Event) { callCount++ },
		TypeFilter(TypeError),
	)

	handler(&Event{Type: TypeStateTransition})
	handler(&Event{Type: TypeError})
	handler(&Event{Type: TypeToolInvocation})

	if callCount != 1 {
		t.Errorf("callCount = %d, want 1", callCount)
	}
}

func TestTypeFilter(t *testing.T) {
	filter := TypeFilter(TypeError, TypeSafetyCheck)

	if !filter(&Event{Type: TypeError}) {
		t.Error("should pass TypeError")
	}
	if !filter(&Event{Type: TypeSafetyCheck}) {
		t.Error("should pass TypeSafetyCheck")
	}
	if filter(&Event{Type: TypeStateTransition}) {
		t.Error("should not pass TypeStateTransition")
	}
}

func TestSessionFilter(t *testing.T) {
	filter := SessionFilter("session-123")

	if !filter(&Event{SessionID: "session-123"}) {
		t.Error("should pass matching session")
	}
	if filter(&Event{SessionID: "session-456"}) {
		t.Error("should not pass different session")
	}
}
