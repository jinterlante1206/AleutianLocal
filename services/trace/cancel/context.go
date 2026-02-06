// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package cancel

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// -----------------------------------------------------------------------------
// Base Context
// -----------------------------------------------------------------------------

// baseContext provides common functionality for all cancellable contexts.
type baseContext struct {
	id           string
	level        Level
	state        atomic.Int32 // State
	startTime    int64
	lastProgress atomic.Int64 // Unix nano timestamp

	ctx    context.Context
	cancel context.CancelFunc

	reason   *CancelReason
	reasonMu sync.RWMutex

	// Partial result collection
	partialCollector PartialResultCollector
	partialResult    any
	partialMu        sync.Mutex

	// Parent reference (nil for sessions)
	parent Cancellable

	// Controller reference
	controller *CancellationController
}

// ID returns the unique identifier for this context.
func (b *baseContext) ID() string {
	return b.id
}

// Level returns the hierarchical level.
func (b *baseContext) Level() Level {
	return b.level
}

// State returns the current state.
func (b *baseContext) State() State {
	return State(b.state.Load())
}

// setState atomically sets the state.
func (b *baseContext) setState(s State) {
	b.state.Store(int32(s))
}

// Context returns the underlying context.Context.
func (b *baseContext) Context() context.Context {
	return b.ctx
}

// Done returns a channel that is closed when this context is cancelled or done.
func (b *baseContext) Done() <-chan struct{} {
	return b.ctx.Done()
}

// Err returns the error after Done is closed.
func (b *baseContext) Err() error {
	return b.ctx.Err()
}

// Cancel initiates cancellation with the given reason.
func (b *baseContext) Cancel(reason CancelReason) {
	// Only transition from Running to Cancelling
	if !b.state.CompareAndSwap(int32(StateRunning), int32(StateCancelling)) {
		return // Already cancelling or terminal
	}

	// Store the reason
	b.reasonMu.Lock()
	if reason.Timestamp == 0 {
		reason.Timestamp = time.Now().UnixMilli()
	}
	b.reason = &reason
	b.reasonMu.Unlock()

	// Cancel the context
	b.cancel()
}

// markDone marks the context as done (normal completion).
func (b *baseContext) markDone() {
	b.state.CompareAndSwap(int32(StateRunning), int32(StateDone))
}

// markCancelled marks the context as fully cancelled (after cleanup).
func (b *baseContext) markCancelled() {
	b.state.CompareAndSwap(int32(StateCancelling), int32(StateCancelled))
}

// ReportProgress updates the last progress timestamp.
func (b *baseContext) ReportProgress() {
	b.lastProgress.Store(time.Now().UnixNano())
}

// LastProgress returns the last progress timestamp.
func (b *baseContext) LastProgress() int64 {
	nano := b.lastProgress.Load()
	if nano == 0 {
		return b.startTime
	}
	return nano / 1e6
}

// SetPartialCollector sets the function to collect partial results.
func (b *baseContext) SetPartialCollector(collector PartialResultCollector) {
	b.partialMu.Lock()
	defer b.partialMu.Unlock()
	b.partialCollector = collector
}

// collectPartialResult attempts to collect partial results.
func (b *baseContext) collectPartialResult() (any, error) {
	b.partialMu.Lock()
	defer b.partialMu.Unlock()

	if b.partialCollector == nil {
		return nil, nil
	}

	result, err := b.partialCollector()
	if err == nil {
		b.partialResult = result
	}
	return result, err
}

// PartialResult returns any collected partial result.
func (b *baseContext) PartialResult() any {
	b.partialMu.Lock()
	defer b.partialMu.Unlock()
	return b.partialResult
}

// getCancelReason returns the cancel reason if set.
func (b *baseContext) getCancelReason() *CancelReason {
	b.reasonMu.RLock()
	defer b.reasonMu.RUnlock()
	return b.reason
}

// baseStatus returns the common status fields.
func (b *baseContext) baseStatus() Status {
	return Status{
		ID:                      b.id,
		Level:                   b.level,
		State:                   b.State(),
		CancelReason:            b.getCancelReason(),
		StartTime:               b.startTime,
		LastProgress:            b.LastProgress(),
		Duration:                time.Duration(time.Now().UnixMilli()-b.startTime) * time.Millisecond,
		PartialResultsAvailable: b.PartialResult() != nil,
	}
}

// -----------------------------------------------------------------------------
// Session Context
// -----------------------------------------------------------------------------

// SessionContext is the top-level cancellable context for an MCTS session.
//
// Thread Safety: Safe for concurrent use.
type SessionContext struct {
	baseContext

	config SessionConfig

	// Child activities
	activities   map[string]*ActivityContext
	activitiesMu sync.RWMutex

	// Resource limits
	resourceLimits ResourceLimits
}

// newSessionContext creates a new session context.
func newSessionContext(parent context.Context, config SessionConfig, ctrl *CancellationController) *SessionContext {
	ctx, cancel := context.WithCancel(parent)

	s := &SessionContext{
		baseContext: baseContext{
			id:         config.ID,
			level:      LevelSession,
			startTime:  time.Now().UnixMilli(),
			ctx:        ctx,
			cancel:     cancel,
			controller: ctrl,
		},
		config:         config,
		activities:     make(map[string]*ActivityContext),
		resourceLimits: config.ResourceLimits,
	}

	s.state.Store(int32(StateRunning))
	s.lastProgress.Store(time.Now().UnixNano())

	// Store controller in context for ReportProgress helper
	s.ctx = context.WithValue(s.ctx, controllerKey, ctrl)
	s.ctx = context.WithValue(s.ctx, contextIDKey, s.id)

	return s
}

// NewActivity creates a new activity context within this session.
//
// Description:
//
//	Creates an activity context that groups related algorithms.
//	The activity inherits cancellation from this session.
//
// Inputs:
//   - name: Unique name for the activity within this session.
//
// Outputs:
//   - *ActivityContext: The created activity context. Never nil.
//
// Thread Safety: Safe for concurrent use.
func (s *SessionContext) NewActivity(name string) *ActivityContext {
	s.activitiesMu.Lock()
	defer s.activitiesMu.Unlock()

	id := fmt.Sprintf("%s/%s", s.id, name)
	ctx, cancel := context.WithCancel(s.ctx)

	a := &ActivityContext{
		baseContext: baseContext{
			id:         id,
			level:      LevelActivity,
			startTime:  time.Now().UnixMilli(),
			ctx:        ctx,
			cancel:     cancel,
			parent:     s,
			controller: s.controller,
		},
		name:       name,
		session:    s,
		algorithms: make(map[string]*AlgorithmContext),
	}

	a.state.Store(int32(StateRunning))
	a.lastProgress.Store(time.Now().UnixNano())

	// Update context with new ID
	a.ctx = context.WithValue(a.ctx, contextIDKey, a.id)

	s.activities[name] = a

	// Register with controller
	if s.controller != nil {
		s.controller.registerContext(a)
	}

	return a
}

// Activity returns the activity with the given name, or nil if not found.
func (s *SessionContext) Activity(name string) *ActivityContext {
	s.activitiesMu.RLock()
	defer s.activitiesMu.RUnlock()
	return s.activities[name]
}

// Activities returns all activity contexts.
func (s *SessionContext) Activities() []*ActivityContext {
	s.activitiesMu.RLock()
	defer s.activitiesMu.RUnlock()

	result := make([]*ActivityContext, 0, len(s.activities))
	for _, a := range s.activities {
		result = append(result, a)
	}
	return result
}

// Cancel cancels this session and all its activities and algorithms.
func (s *SessionContext) Cancel(reason CancelReason) {
	// Cancel all children first
	s.activitiesMu.RLock()
	activities := make([]*ActivityContext, 0, len(s.activities))
	for _, a := range s.activities {
		activities = append(activities, a)
	}
	s.activitiesMu.RUnlock()

	childReason := CancelReason{
		Type:      CancelParent,
		Message:   fmt.Sprintf("Parent session cancelled: %s", reason.Message),
		Component: s.id,
		Timestamp: time.Now().UnixMilli(),
	}
	for _, a := range activities {
		a.Cancel(childReason)
	}

	// Cancel self
	s.baseContext.Cancel(reason)
}

// Done marks the session as normally completed.
func (s *SessionContext) Done() <-chan struct{} {
	return s.baseContext.Done()
}

// Status returns the current status including all children.
func (s *SessionContext) Status() Status {
	status := s.baseStatus()

	s.activitiesMu.RLock()
	defer s.activitiesMu.RUnlock()

	status.Children = make([]Status, 0, len(s.activities))
	for _, a := range s.activities {
		status.Children = append(status.Children, a.Status())
	}

	return status
}

// ProgressInterval returns the configured progress interval.
func (s *SessionContext) ProgressInterval() time.Duration {
	return s.config.ProgressInterval
}

// -----------------------------------------------------------------------------
// Activity Context
// -----------------------------------------------------------------------------

// ActivityContext groups related algorithms within a session.
//
// Thread Safety: Safe for concurrent use.
type ActivityContext struct {
	baseContext

	name    string
	session *SessionContext

	// Child algorithms
	algorithms   map[string]*AlgorithmContext
	algorithmsMu sync.RWMutex
}

// Name returns the activity name.
func (a *ActivityContext) Name() string {
	return a.name
}

// Session returns the parent session.
func (a *ActivityContext) Session() *SessionContext {
	return a.session
}

// NewAlgorithm creates a new algorithm context within this activity.
//
// Description:
//
//	Creates an algorithm context for a specific algorithm execution.
//	The algorithm inherits cancellation from this activity.
//
// Inputs:
//   - name: Unique name for the algorithm within this activity.
//   - timeout: Maximum execution time. Zero uses the controller default.
//
// Outputs:
//   - *AlgorithmContext: The created algorithm context. Never nil.
//
// Thread Safety: Safe for concurrent use.
func (a *ActivityContext) NewAlgorithm(name string, timeout time.Duration) *AlgorithmContext {
	a.algorithmsMu.Lock()
	defer a.algorithmsMu.Unlock()

	id := fmt.Sprintf("%s/%s", a.id, name)

	// Apply timeout
	var ctx context.Context
	var cancel context.CancelFunc
	if timeout > 0 {
		ctx, cancel = context.WithTimeout(a.ctx, timeout)
	} else if a.controller != nil && a.controller.config.DefaultTimeout > 0 {
		ctx, cancel = context.WithTimeout(a.ctx, a.controller.config.DefaultTimeout)
	} else {
		ctx, cancel = context.WithCancel(a.ctx)
	}

	alg := &AlgorithmContext{
		baseContext: baseContext{
			id:         id,
			level:      LevelAlgorithm,
			startTime:  time.Now().UnixMilli(),
			ctx:        ctx,
			cancel:     cancel,
			parent:     a,
			controller: a.controller,
		},
		name:     name,
		activity: a,
		timeout:  timeout,
	}

	alg.state.Store(int32(StateRunning))
	alg.lastProgress.Store(time.Now().UnixNano())

	// Update context with new ID and progress reporter
	alg.ctx = context.WithValue(alg.ctx, contextIDKey, alg.id)
	alg.ctx = context.WithValue(alg.ctx, progressReporterKey, ProgressReporter(alg.ReportProgress))

	a.algorithms[name] = alg

	// Register with controller
	if a.controller != nil {
		a.controller.registerContext(alg)
	}

	return alg
}

// Algorithm returns the algorithm with the given name, or nil if not found.
func (a *ActivityContext) Algorithm(name string) *AlgorithmContext {
	a.algorithmsMu.RLock()
	defer a.algorithmsMu.RUnlock()
	return a.algorithms[name]
}

// Algorithms returns all algorithm contexts.
func (a *ActivityContext) Algorithms() []*AlgorithmContext {
	a.algorithmsMu.RLock()
	defer a.algorithmsMu.RUnlock()

	result := make([]*AlgorithmContext, 0, len(a.algorithms))
	for _, alg := range a.algorithms {
		result = append(result, alg)
	}
	return result
}

// Cancel cancels this activity and all its algorithms.
func (a *ActivityContext) Cancel(reason CancelReason) {
	// Cancel all children first
	a.algorithmsMu.RLock()
	algorithms := make([]*AlgorithmContext, 0, len(a.algorithms))
	for _, alg := range a.algorithms {
		algorithms = append(algorithms, alg)
	}
	a.algorithmsMu.RUnlock()

	childReason := CancelReason{
		Type:      CancelParent,
		Message:   fmt.Sprintf("Parent activity cancelled: %s", reason.Message),
		Component: a.id,
		Timestamp: time.Now().UnixMilli(),
	}
	for _, alg := range algorithms {
		alg.Cancel(childReason)
	}

	// Cancel self
	a.baseContext.Cancel(reason)
}

// Status returns the current status including all children.
func (a *ActivityContext) Status() Status {
	status := a.baseStatus()

	a.algorithmsMu.RLock()
	defer a.algorithmsMu.RUnlock()

	status.Children = make([]Status, 0, len(a.algorithms))
	for _, alg := range a.algorithms {
		status.Children = append(status.Children, alg.Status())
	}

	return status
}

// -----------------------------------------------------------------------------
// Algorithm Context
// -----------------------------------------------------------------------------

// AlgorithmContext represents a single algorithm execution.
//
// Thread Safety: Safe for concurrent use.
type AlgorithmContext struct {
	baseContext

	name     string
	activity *ActivityContext
	timeout  time.Duration
}

// Name returns the algorithm name.
func (a *AlgorithmContext) Name() string {
	return a.name
}

// Activity returns the parent activity.
func (a *AlgorithmContext) Activity() *ActivityContext {
	return a.activity
}

// Timeout returns the configured timeout.
func (a *AlgorithmContext) Timeout() time.Duration {
	return a.timeout
}

// Status returns the current status.
func (a *AlgorithmContext) Status() Status {
	return a.baseStatus()
}

// MarkDone marks the algorithm as normally completed.
func (a *AlgorithmContext) MarkDone() {
	a.markDone()
	if a.controller != nil {
		a.controller.unregisterContext(a.id)
	}
}

// -----------------------------------------------------------------------------
// Helper Functions
// -----------------------------------------------------------------------------

// ReportProgress reports progress from within an algorithm.
// This resets the deadlock detection timer.
//
// Description:
//
//	Algorithms should call this function periodically to indicate they are
//	making progress. If no progress is reported within the deadlock threshold,
//	the algorithm will be automatically cancelled.
//
// Inputs:
//   - ctx: The context passed to the algorithm's Process method.
//
// Example:
//
//	func (a *MyAlgo) Process(ctx context.Context, ...) {
//	    for {
//	        cancel.ReportProgress(ctx)  // Report progress
//	        // ... do work ...
//	    }
//	}
func ReportProgress(ctx context.Context) {
	if reporter, ok := ctx.Value(progressReporterKey).(ProgressReporter); ok {
		reporter()
	}
}

// GetContextID returns the context ID from the context, if available.
func GetContextID(ctx context.Context) string {
	if id, ok := ctx.Value(contextIDKey).(string); ok {
		return id
	}
	return ""
}

// GetController returns the CancellationController from the context, if available.
func GetController(ctx context.Context) *CancellationController {
	if ctrl, ok := ctx.Value(controllerKey).(*CancellationController); ok {
		return ctrl
	}
	return nil
}
