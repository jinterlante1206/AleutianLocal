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
	"errors"
	"time"
)

// -----------------------------------------------------------------------------
// Errors
// -----------------------------------------------------------------------------

var (
	// ErrControllerClosed is returned when operations are attempted on a closed controller.
	ErrControllerClosed = errors.New("cancellation controller is closed")

	// ErrSessionNotFound is returned when a session ID is not found.
	ErrSessionNotFound = errors.New("session not found")

	// ErrActivityNotFound is returned when an activity is not found.
	ErrActivityNotFound = errors.New("activity not found")

	// ErrAlgorithmNotFound is returned when an algorithm is not found.
	ErrAlgorithmNotFound = errors.New("algorithm not found")

	// ErrAlreadyCancelled is returned when attempting to cancel an already cancelled context.
	ErrAlreadyCancelled = errors.New("context already cancelled")

	// ErrInvalidConfig is returned when configuration is invalid.
	ErrInvalidConfig = errors.New("invalid configuration")

	// ErrNilContext is returned when a nil context is provided.
	ErrNilContext = errors.New("context must not be nil")
)

// -----------------------------------------------------------------------------
// Enums
// -----------------------------------------------------------------------------

// CancelType indicates why cancellation occurred.
type CancelType int

const (
	// CancelUser indicates user-initiated cancellation (API, Ctrl+C, stop button).
	CancelUser CancelType = iota

	// CancelTimeout indicates the algorithm exceeded its configured timeout.
	CancelTimeout

	// CancelDeadlock indicates no progress was reported within the deadlock threshold.
	CancelDeadlock

	// CancelResourceLimit indicates memory or CPU limits were exceeded.
	CancelResourceLimit

	// CancelParent indicates the parent context was cancelled.
	CancelParent

	// CancelShutdown indicates system shutdown is in progress.
	CancelShutdown
)

// String returns the string representation of the cancel type.
func (t CancelType) String() string {
	switch t {
	case CancelUser:
		return "user"
	case CancelTimeout:
		return "timeout"
	case CancelDeadlock:
		return "deadlock"
	case CancelResourceLimit:
		return "resource_limit"
	case CancelParent:
		return "parent"
	case CancelShutdown:
		return "shutdown"
	default:
		return "unknown"
	}
}

// State represents the current state of a cancellable context.
type State int

const (
	// StateRunning indicates the context is actively running.
	StateRunning State = iota

	// StateCancelling indicates cancellation has been signaled but cleanup is in progress.
	StateCancelling

	// StateCancelled indicates cancellation and cleanup are complete.
	StateCancelled

	// StateDone indicates normal completion (not cancelled).
	StateDone
)

// String returns the string representation of the state.
func (s State) String() string {
	switch s {
	case StateRunning:
		return "running"
	case StateCancelling:
		return "cancelling"
	case StateCancelled:
		return "cancelled"
	case StateDone:
		return "done"
	default:
		return "unknown"
	}
}

// IsTerminal returns true if this is a terminal state.
func (s State) IsTerminal() bool {
	return s == StateCancelled || s == StateDone
}

// Level indicates the hierarchical level of a cancellable context.
type Level int

const (
	// LevelSession is the top-level context for an entire MCTS session.
	LevelSession Level = iota

	// LevelActivity is the context for an activity (group of algorithms).
	LevelActivity

	// LevelAlgorithm is the context for a single algorithm.
	LevelAlgorithm
)

// String returns the string representation of the level.
func (l Level) String() string {
	switch l {
	case LevelSession:
		return "session"
	case LevelActivity:
		return "activity"
	case LevelAlgorithm:
		return "algorithm"
	default:
		return "unknown"
	}
}

// -----------------------------------------------------------------------------
// Configuration Types
// -----------------------------------------------------------------------------

// ControllerConfig configures the CancellationController.
type ControllerConfig struct {
	// DefaultTimeout is applied to algorithms that don't specify their own.
	// Must be > 0. Default: 30 seconds.
	DefaultTimeout time.Duration

	// DeadlockMultiplier determines when deadlock is detected.
	// Deadlock is detected after DeadlockMultiplier * ProgressInterval with no progress.
	// Must be >= 2. Default: 3.
	DeadlockMultiplier int

	// GracePeriod is how long to wait for graceful shutdown before force kill.
	// Must be > 0. Default: 500 milliseconds.
	GracePeriod time.Duration

	// ForceKillTimeout is the maximum time to wait for force kill to complete.
	// Must be > GracePeriod. Default: 2 seconds.
	ForceKillTimeout time.Duration

	// ProgressCheckInterval is how often the deadlock detector checks for progress.
	// Must be > 0. Default: 100 milliseconds.
	ProgressCheckInterval time.Duration

	// EnableMetrics enables Prometheus metrics collection.
	// Default: true.
	EnableMetrics bool
}

// Validate checks if the configuration is valid.
//
// Description:
//
//	Validates all configuration fields and applies defaults where needed.
//
// Outputs:
//   - error: Non-nil if configuration is invalid.
func (c *ControllerConfig) Validate() error {
	if c.DefaultTimeout < 0 {
		return errors.New("DefaultTimeout must be >= 0")
	}
	if c.DeadlockMultiplier < 2 {
		return errors.New("DeadlockMultiplier must be >= 2")
	}
	if c.GracePeriod < 0 {
		return errors.New("GracePeriod must be >= 0")
	}
	if c.ForceKillTimeout < 0 {
		return errors.New("ForceKillTimeout must be >= 0")
	}
	if c.ForceKillTimeout > 0 && c.GracePeriod > 0 && c.ForceKillTimeout <= c.GracePeriod {
		return errors.New("ForceKillTimeout must be > GracePeriod")
	}
	if c.ProgressCheckInterval < 0 {
		return errors.New("ProgressCheckInterval must be >= 0")
	}
	return nil
}

// ApplyDefaults fills in zero values with sensible defaults.
func (c *ControllerConfig) ApplyDefaults() {
	if c.DefaultTimeout == 0 {
		c.DefaultTimeout = 30 * time.Second
	}
	if c.DeadlockMultiplier == 0 {
		c.DeadlockMultiplier = 3
	}
	if c.GracePeriod == 0 {
		c.GracePeriod = 500 * time.Millisecond
	}
	if c.ForceKillTimeout == 0 {
		c.ForceKillTimeout = 2 * time.Second
	}
	if c.ProgressCheckInterval == 0 {
		c.ProgressCheckInterval = 100 * time.Millisecond
	}
}

// SessionConfig configures a new session context.
type SessionConfig struct {
	// ID is the unique identifier for this session.
	// Required.
	ID string

	// ResourceLimits defines resource constraints for this session.
	// Optional. Zero values mean no limit.
	ResourceLimits ResourceLimits

	// Timeout overrides the controller's default timeout for this session.
	// Zero means use the controller default.
	Timeout time.Duration

	// ProgressInterval is how often algorithms should report progress.
	// Zero means use the default (1 second).
	ProgressInterval time.Duration
}

// Validate checks if the session configuration is valid.
func (c *SessionConfig) Validate() error {
	if c.ID == "" {
		return errors.New("session ID is required")
	}
	if c.Timeout < 0 {
		return errors.New("Timeout must be >= 0")
	}
	if c.ProgressInterval < 0 {
		return errors.New("ProgressInterval must be >= 0")
	}
	return c.ResourceLimits.Validate()
}

// ApplyDefaults fills in zero values with sensible defaults.
func (c *SessionConfig) ApplyDefaults() {
	if c.ProgressInterval == 0 {
		c.ProgressInterval = 1 * time.Second
	}
}

// ResourceLimits defines resource constraints for cancellation.
type ResourceLimits struct {
	// MaxMemoryBytes is the maximum memory usage before triggering cancellation.
	// Zero means no limit.
	MaxMemoryBytes int64

	// MaxCPUPercent is the maximum CPU usage (0-100) before triggering cancellation.
	// Zero means no limit.
	MaxCPUPercent float64

	// MaxGoroutines is the maximum number of goroutines before triggering cancellation.
	// Zero means no limit.
	MaxGoroutines int
}

// Validate checks if resource limits are valid.
func (r *ResourceLimits) Validate() error {
	if r.MaxMemoryBytes < 0 {
		return errors.New("MaxMemoryBytes must be >= 0")
	}
	if r.MaxCPUPercent < 0 || r.MaxCPUPercent > 100 {
		return errors.New("MaxCPUPercent must be between 0 and 100")
	}
	if r.MaxGoroutines < 0 {
		return errors.New("MaxGoroutines must be >= 0")
	}
	return nil
}

// HasLimits returns true if any limits are configured.
func (r *ResourceLimits) HasLimits() bool {
	return r.MaxMemoryBytes > 0 || r.MaxCPUPercent > 0 || r.MaxGoroutines > 0
}

// -----------------------------------------------------------------------------
// Result Types
// -----------------------------------------------------------------------------

// CancelReason describes why cancellation occurred.
type CancelReason struct {
	// Type indicates the category of cancellation.
	Type CancelType

	// Message provides a human-readable description.
	Message string

	// Threshold describes the limit that was exceeded (for timeout/resource cancellations).
	// Example: "memory > 1GB" or "timeout > 5s"
	Threshold string

	// Component identifies which component triggered the cancellation.
	Component string

	// Timestamp is when the cancellation was triggered.
	Timestamp time.Time
}

// Status provides the current status of a cancellable context.
type Status struct {
	// ID is the unique identifier of this context.
	ID string

	// Level indicates whether this is a session, activity, or algorithm.
	Level Level

	// State is the current state.
	State State

	// CancelReason is set if State is Cancelling or Cancelled.
	CancelReason *CancelReason

	// StartTime is when this context was created.
	StartTime time.Time

	// LastProgress is the last time progress was reported.
	LastProgress time.Time

	// Duration is how long this context has been running.
	Duration time.Duration

	// Children contains the status of child contexts (if any).
	Children []Status

	// PartialResultsAvailable indicates if partial results were collected.
	PartialResultsAvailable bool
}

// ControllerStatus provides the overall status of the cancellation controller.
type ControllerStatus struct {
	// Sessions contains the status of all active sessions.
	Sessions []Status

	// TotalActive is the count of non-terminal contexts.
	TotalActive int

	// TotalCancelled is the count of cancelled contexts.
	TotalCancelled int

	// TotalCompleted is the count of normally completed contexts.
	TotalCompleted int
}

// ShutdownResult contains the results of a graceful shutdown.
type ShutdownResult struct {
	// Success is true if all contexts were cleanly shut down.
	Success bool

	// Duration is how long the shutdown took.
	Duration time.Duration

	// PartialResultsCollected is the count of algorithms that returned partial results.
	PartialResultsCollected int

	// ForceKilled is the count of algorithms that had to be force killed.
	ForceKilled int

	// Errors contains any errors encountered during shutdown.
	Errors []error
}

// -----------------------------------------------------------------------------
// Callback Types
// -----------------------------------------------------------------------------

// ProgressReporter is called by algorithms to report progress.
// This resets the deadlock detection timer.
type ProgressReporter func()

// PartialResultCollector is called during graceful shutdown to collect partial results.
type PartialResultCollector func() (result any, err error)

// -----------------------------------------------------------------------------
// Context Key Types
// -----------------------------------------------------------------------------

// contextKey is used for storing values in context.Context.
type contextKey int

const (
	// controllerKey stores the CancellationController in context.
	controllerKey contextKey = iota

	// contextIDKey stores the context ID in context.
	contextIDKey

	// progressReporterKey stores the ProgressReporter in context.
	progressReporterKey
)

// -----------------------------------------------------------------------------
// Interface Definitions
// -----------------------------------------------------------------------------

// Controller manages hierarchical cancellation across sessions, activities, and algorithms.
//
// Thread Safety: Safe for concurrent use.
type Controller interface {
	// NewSession creates a new cancellable session context.
	//
	// Description:
	//   Creates a top-level session context that can contain activities and algorithms.
	//   The session inherits cancellation from the parent context.
	//
	// Inputs:
	//   - parent: Parent context. Must not be nil.
	//   - config: Session configuration. ID is required.
	//
	// Outputs:
	//   - *SessionContext: The created session context. Never nil on success.
	//   - error: Non-nil if parent is nil or config is invalid.
	//
	// Thread Safety: Safe for concurrent use.
	NewSession(parent context.Context, config SessionConfig) (*SessionContext, error)

	// Cancel initiates cancellation for the specified target.
	//
	// Description:
	//   Cancels the context with the given ID. The ID can refer to a session,
	//   activity, or algorithm. Cancellation propagates to all children.
	//
	// Inputs:
	//   - id: The ID of the context to cancel.
	//   - reason: The reason for cancellation.
	//
	// Outputs:
	//   - error: Non-nil if the ID is not found.
	//
	// Thread Safety: Safe for concurrent use.
	Cancel(id string, reason CancelReason) error

	// CancelAll cancels all active contexts immediately.
	//
	// Description:
	//   Emergency stop for all sessions, activities, and algorithms.
	//
	// Inputs:
	//   - reason: The reason for cancellation.
	//
	// Thread Safety: Safe for concurrent use.
	CancelAll(reason CancelReason)

	// Status returns the current status of all contexts.
	//
	// Description:
	//   Returns a snapshot of the current state of all sessions and their children.
	//
	// Outputs:
	//   - *ControllerStatus: Current status. Never nil.
	//
	// Thread Safety: Safe for concurrent use. Returns a snapshot.
	Status() *ControllerStatus

	// Shutdown gracefully shuts down the controller.
	//
	// Description:
	//   Cancels all contexts, waits for graceful shutdown, then force kills
	//   any remaining contexts. Blocks until complete or context is cancelled.
	//
	// Inputs:
	//   - ctx: Context for the shutdown operation itself.
	//
	// Outputs:
	//   - *ShutdownResult: Results of the shutdown operation.
	//   - error: Non-nil if ctx was cancelled before shutdown completed.
	//
	// Thread Safety: Safe for concurrent use. Only the first call performs shutdown.
	Shutdown(ctx context.Context) (*ShutdownResult, error)

	// Close releases all resources held by the controller.
	//
	// Description:
	//   Should be called when the controller is no longer needed.
	//   Calls Shutdown internally if not already called.
	//
	// Outputs:
	//   - error: Non-nil if shutdown encountered errors.
	//
	// Thread Safety: Safe for concurrent use. Idempotent.
	Close() error
}

// Cancellable represents any context that can be cancelled.
//
// Thread Safety: Safe for concurrent use.
type Cancellable interface {
	// ID returns the unique identifier for this context.
	ID() string

	// Level returns the hierarchical level (session, activity, algorithm).
	Level() Level

	// State returns the current state.
	State() State

	// Context returns the underlying context.Context.
	Context() context.Context

	// Cancel initiates cancellation with the given reason.
	Cancel(reason CancelReason)

	// Done returns a channel that is closed when this context is cancelled or done.
	Done() <-chan struct{}

	// Err returns the error after Done is closed.
	Err() error

	// Status returns the current status.
	Status() Status
}
