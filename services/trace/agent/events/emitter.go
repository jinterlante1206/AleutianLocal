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
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
)

// EventMetadata contains typed additional context for events.
type EventMetadata struct {
	// TraceID links the event to a distributed trace.
	TraceID string `json:"trace_id,omitempty"`

	// SpanID links the event to a specific span.
	SpanID string `json:"span_id,omitempty"`

	// Source identifies where the event originated.
	Source string `json:"source,omitempty"`

	// Tags are key-value pairs for categorization.
	Tags map[string]string `json:"tags,omitempty"`

	// Priority indicates event importance (higher = more important).
	Priority int `json:"priority,omitempty"`
}

// Handler is a function that processes events.
type Handler func(event *Event)

// Filter is a function that determines if an event should be handled.
type Filter func(event *Event) bool

// Subscription represents a subscription to events.
type Subscription struct {
	// ID uniquely identifies this subscription.
	ID string

	// Handler processes matching events.
	Handler Handler

	// Filter determines which events to handle (nil = all events).
	Filter Filter

	// Types limits which event types to handle (nil = all types).
	Types []Type
}

// Emitter broadcasts events to subscribers.
//
// Thread Safety: Emitter is safe for concurrent use.
type Emitter struct {
	mu            sync.RWMutex
	subscriptions map[string]*Subscription
	buffer        []Event
	bufferSize    int
	sessionID     string
	currentStep   int
}

// EmitterOption configures an Emitter.
type EmitterOption func(*Emitter)

// WithBufferSize sets the event buffer size.
func WithBufferSize(size int) EmitterOption {
	return func(e *Emitter) {
		e.bufferSize = size
	}
}

// WithSessionID sets the session ID for all events.
func WithSessionID(id string) EmitterOption {
	return func(e *Emitter) {
		e.sessionID = id
	}
}

// NewEmitter creates a new event emitter.
func NewEmitter(opts ...EmitterOption) *Emitter {
	e := &Emitter{
		subscriptions: make(map[string]*Subscription),
		bufferSize:    1000,
	}

	for _, opt := range opts {
		opt(e)
	}

	e.buffer = make([]Event, 0, e.bufferSize)

	return e
}

// Subscribe registers a handler for events.
//
// Inputs:
//
//	handler - Function to call for each event.
//	types - Event types to subscribe to (nil = all types).
//
// Outputs:
//
//	string - Subscription ID for unsubscribing.
func (e *Emitter) Subscribe(handler Handler, types ...Type) string {
	return e.SubscribeWithFilter(handler, nil, types...)
}

// SubscribeWithFilter registers a handler with a custom filter.
//
// Inputs:
//
//	handler - Function to call for matching events.
//	filter - Custom filter function (nil = no filter).
//	types - Event types to subscribe to (nil = all types).
//
// Outputs:
//
//	string - Subscription ID for unsubscribing.
func (e *Emitter) SubscribeWithFilter(handler Handler, filter Filter, types ...Type) string {
	e.mu.Lock()
	defer e.mu.Unlock()

	sub := &Subscription{
		ID:      uuid.NewString(),
		Handler: handler,
		Filter:  filter,
		Types:   types,
	}

	e.subscriptions[sub.ID] = sub
	return sub.ID
}

// Unsubscribe removes a subscription.
//
// Inputs:
//
//	id - The subscription ID to remove.
//
// Outputs:
//
//	bool - True if the subscription was found and removed.
func (e *Emitter) Unsubscribe(id string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()

	if _, ok := e.subscriptions[id]; ok {
		delete(e.subscriptions, id)
		return true
	}
	return false
}

// Emit broadcasts an event to all matching subscribers.
//
// Inputs:
//
//	eventType - The type of event.
//	data - Event-specific data.
func (e *Emitter) Emit(eventType Type, data any) {
	e.EmitWithMetadata(eventType, data, nil)
}

// EmitWithMetadata broadcasts an event with additional metadata.
//
// Description:
//
//	Creates an event with the specified type, data, and metadata, then
//	broadcasts it to all matching subscribers. The event is also buffered
//	for later retrieval. Handler panics are recovered to prevent one
//	failing handler from crashing the emitter.
//
// Inputs:
//
//	eventType - The type of event.
//	data - Event-specific data (use typed data structs from types.go).
//	metadata - Additional context (nil is allowed).
//
// Thread Safety: This method is safe for concurrent use.
func (e *Emitter) EmitWithMetadata(eventType Type, data any, metadata *EventMetadata) {
	e.mu.RLock()
	sessionID := e.sessionID
	step := e.currentStep
	subs := make([]*Subscription, 0, len(e.subscriptions))
	for _, sub := range e.subscriptions {
		subs = append(subs, sub)
	}
	e.mu.RUnlock()

	event := Event{
		ID:        uuid.NewString(),
		Type:      eventType,
		SessionID: sessionID,
		Timestamp: time.Now(),
		Step:      step,
		Data:      data,
		Metadata:  metadata,
	}

	// Buffer the event
	e.mu.Lock()
	if len(e.buffer) >= e.bufferSize {
		// Remove oldest event
		e.buffer = e.buffer[1:]
	}
	e.buffer = append(e.buffer, event)
	e.mu.Unlock()

	// Notify subscribers with panic recovery
	for _, sub := range subs {
		if e.shouldHandle(sub, &event) {
			e.safeInvokeHandler(sub.Handler, &event)
		}
	}
}

// safeInvokeHandler invokes a handler with panic recovery.
//
// Description:
//
//	Calls the handler function with panic recovery to prevent one
//	misbehaving handler from crashing the entire emitter or causing
//	other handlers to miss events.
//
// Inputs:
//
//	handler - The handler function to invoke.
//	event - The event to pass to the handler.
func (e *Emitter) safeInvokeHandler(handler Handler, event *Event) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("event handler panicked",
				"event_type", event.Type,
				"event_id", event.ID,
				"panic", r,
			)
		}
	}()
	handler(event)
}

// shouldHandle determines if a subscription should handle an event.
func (e *Emitter) shouldHandle(sub *Subscription, event *Event) bool {
	// Check type filter
	if len(sub.Types) > 0 {
		typeMatch := false
		for _, t := range sub.Types {
			if t == event.Type {
				typeMatch = true
				break
			}
		}
		if !typeMatch {
			return false
		}
	}

	// Check custom filter
	if sub.Filter != nil && !sub.Filter(event) {
		return false
	}

	return true
}

// SetSessionID updates the session ID for future events.
func (e *Emitter) SetSessionID(id string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.sessionID = id
}

// SetStep updates the current step number.
func (e *Emitter) SetStep(step int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.currentStep = step
}

// IncrementStep increments and returns the new step number.
func (e *Emitter) IncrementStep() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.currentStep++
	return e.currentStep
}

// CurrentStep returns the current step number without incrementing.
func (e *Emitter) CurrentStep() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.currentStep
}

// GetBuffer returns a copy of buffered events.
func (e *Emitter) GetBuffer() []Event {
	e.mu.RLock()
	defer e.mu.RUnlock()

	events := make([]Event, len(e.buffer))
	copy(events, e.buffer)
	return events
}

// GetBufferSince returns events since a timestamp.
func (e *Emitter) GetBufferSince(since time.Time) []Event {
	e.mu.RLock()
	defer e.mu.RUnlock()

	var events []Event
	for _, event := range e.buffer {
		if event.Timestamp.After(since) {
			events = append(events, event)
		}
	}
	return events
}

// GetBufferByType returns buffered events of a specific type.
func (e *Emitter) GetBufferByType(eventType Type) []Event {
	e.mu.RLock()
	defer e.mu.RUnlock()

	var events []Event
	for _, event := range e.buffer {
		if event.Type == eventType {
			events = append(events, event)
		}
	}
	return events
}

// ClearBuffer removes all buffered events.
func (e *Emitter) ClearBuffer() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.buffer = make([]Event, 0, e.bufferSize)
}

// SubscriptionCount returns the number of active subscriptions.
func (e *Emitter) SubscriptionCount() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return len(e.subscriptions)
}

// Reset clears all state including subscriptions and buffer.
func (e *Emitter) Reset() {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.subscriptions = make(map[string]*Subscription)
	e.buffer = make([]Event, 0, e.bufferSize)
	e.currentStep = 0
}

// MockEmitter is a mock emitter for testing.
type MockEmitter struct {
	mu     sync.RWMutex
	Events []Event
}

// NewMockEmitter creates a new mock emitter.
func NewMockEmitter() *MockEmitter {
	return &MockEmitter{
		Events: make([]Event, 0),
	}
}

// Emit records an event.
func (m *MockEmitter) Emit(eventType Type, data any) {
	m.EmitWithMetadata(eventType, data, nil)
}

// EmitWithMetadata records an event with metadata.
func (m *MockEmitter) EmitWithMetadata(eventType Type, data any, metadata *EventMetadata) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.Events = append(m.Events, Event{
		ID:        uuid.NewString(),
		Type:      eventType,
		Timestamp: time.Now(),
		Data:      data,
		Metadata:  metadata,
	})
}

// EventCount returns the number of recorded events.
func (m *MockEmitter) EventCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.Events)
}

// GetEvents returns all recorded events.
func (m *MockEmitter) GetEvents() []Event {
	m.mu.RLock()
	defer m.mu.RUnlock()

	events := make([]Event, len(m.Events))
	copy(events, m.Events)
	return events
}

// GetEventsByType returns events of a specific type.
func (m *MockEmitter) GetEventsByType(eventType Type) []Event {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var events []Event
	for _, e := range m.Events {
		if e.Type == eventType {
			events = append(events, e)
		}
	}
	return events
}

// Clear removes all recorded events.
func (m *MockEmitter) Clear() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Events = make([]Event, 0)
}
