// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package crs

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/eval"
)

// -----------------------------------------------------------------------------
// Errors
// -----------------------------------------------------------------------------

var (
	// ErrNilContext is returned when context is nil.
	ErrNilContext = errors.New("context must not be nil")

	// ErrNilDelta is returned when delta is nil.
	ErrNilDelta = errors.New("delta must not be nil")

	// ErrDeltaValidation is returned when delta validation fails.
	ErrDeltaValidation = errors.New("delta validation failed")

	// ErrDeltaConflict is returned when deltas conflict.
	ErrDeltaConflict = errors.New("delta conflict detected")

	// ErrSnapshotStale is returned when snapshot is too old.
	ErrSnapshotStale = errors.New("snapshot is stale")

	// ErrIndexNotFound is returned when an index doesn't exist.
	ErrIndexNotFound = errors.New("index not found")

	// ErrHardSoftBoundaryViolation is returned when soft signal attempts hard action.
	ErrHardSoftBoundaryViolation = errors.New("soft signal cannot perform hard action")

	// ErrApplyRollback is returned when apply partially failed and rolled back.
	ErrApplyRollback = errors.New("apply failed, changes rolled back")
)

// -----------------------------------------------------------------------------
// CRS Interface
// -----------------------------------------------------------------------------

// CRS is the central mutable state container for the Aleutian Hybrid MCTS system.
//
// Description:
//
//	CRS manages 6 indexes that algorithms read from and write to. It provides
//	immutable snapshots for reading and delta-based mutations for writing.
//
// Thread Safety: Safe for concurrent use. Uses RWMutex internally.
type CRS interface {
	eval.Evaluable

	// Snapshot returns an immutable view of the current state.
	//
	// Description:
	//
	//   Creates a copy-on-write snapshot that algorithms can read from safely.
	//   The snapshot is immutable and will not change even if Apply() is called.
	//
	// Outputs:
	//   - Snapshot: The immutable snapshot. Never nil.
	//
	// Thread Safety: Safe for concurrent use.
	Snapshot() Snapshot

	// Apply atomically applies a delta to the state.
	//
	// Description:
	//
	//   Validates the delta, then applies all changes atomically. If any
	//   index update fails, all changes are rolled back.
	//
	// Inputs:
	//   - ctx: Context for cancellation. Must not be nil.
	//   - delta: The delta to apply. Must not be nil.
	//
	// Outputs:
	//   - ApplyMetrics: Metrics about the apply operation.
	//   - error: Non-nil if apply failed.
	//
	// Thread Safety: Safe for concurrent use. Acquires write lock.
	Apply(ctx context.Context, delta Delta) (ApplyMetrics, error)

	// Generation returns the current state version.
	//
	// Description:
	//
	//   Generation increments with each successful Apply(). Use for cache
	//   invalidation and ordering.
	//
	// Outputs:
	//   - int64: The current generation. Always >= 0.
	//
	// Thread Safety: Safe for concurrent use.
	Generation() int64

	// Checkpoint creates a restorable checkpoint for chaos testing.
	//
	// Description:
	//
	//   Creates a checkpoint that can be restored later. Used for chaos
	//   testing and debugging.
	//
	// Inputs:
	//   - ctx: Context for cancellation. Must not be nil.
	//
	// Outputs:
	//   - Checkpoint: The checkpoint. Never nil on success.
	//   - error: Non-nil if checkpoint creation failed.
	//
	// Thread Safety: Safe for concurrent use.
	Checkpoint(ctx context.Context) (Checkpoint, error)

	// Restore returns to a previous checkpoint.
	//
	// Description:
	//
	//   Restores CRS to the state at the checkpoint. All changes since
	//   the checkpoint are discarded.
	//
	// Inputs:
	//   - ctx: Context for cancellation. Must not be nil.
	//   - cp: The checkpoint to restore. Must not be nil.
	//
	// Outputs:
	//   - error: Non-nil if restore failed.
	//
	// Thread Safety: Safe for concurrent use. Acquires write lock.
	Restore(ctx context.Context, cp Checkpoint) error
}

// -----------------------------------------------------------------------------
// Snapshot Interface
// -----------------------------------------------------------------------------

// Snapshot is an immutable view of CRS at a point in time.
//
// Description:
//
//	Snapshots are created by CRS.Snapshot() and provide read-only access
//	to all indexes. Snapshots are thread-safe and can be shared across
//	goroutines.
//
// Thread Safety: Safe for concurrent use (immutable).
type Snapshot interface {
	// Generation returns the generation when this snapshot was created.
	Generation() int64

	// CreatedAt returns when this snapshot was created.
	CreatedAt() time.Time

	// ProofIndex returns the proof numbers index view.
	ProofIndex() ProofIndexView

	// ConstraintIndex returns the constraint index view.
	ConstraintIndex() ConstraintIndexView

	// SimilarityIndex returns the similarity index view.
	SimilarityIndex() SimilarityIndexView

	// DependencyIndex returns the dependency index view.
	DependencyIndex() DependencyIndexView

	// HistoryIndex returns the history index view.
	HistoryIndex() HistoryIndexView

	// StreamingIndex returns the streaming statistics index view.
	StreamingIndex() StreamingIndexView

	// Query returns the cross-index query API.
	//
	// Description:
	//
	//   Query provides methods that span multiple indexes, such as finding
	//   proven nodes that satisfy constraints or computing dependency chains.
	//
	// Outputs:
	//   - QueryAPI: The query API. Never nil.
	Query() QueryAPI
}

// -----------------------------------------------------------------------------
// Index View Interfaces (Read-Only)
// -----------------------------------------------------------------------------

// ProofIndexView provides read-only access to proof numbers.
//
// Thread Safety: Safe for concurrent use (immutable).
type ProofIndexView interface {
	// Get returns the proof number for a node.
	Get(nodeID string) (ProofNumber, bool)

	// All returns all proof numbers.
	All() map[string]ProofNumber

	// Size returns the number of entries.
	Size() int
}

// ConstraintIndexView provides read-only access to constraints.
//
// Thread Safety: Safe for concurrent use (immutable).
type ConstraintIndexView interface {
	// Get returns a constraint by ID.
	Get(constraintID string) (Constraint, bool)

	// FindByType returns all constraints of a type.
	FindByType(constraintType ConstraintType) []Constraint

	// FindByNode returns all constraints affecting a node.
	FindByNode(nodeID string) []Constraint

	// All returns all constraints.
	All() map[string]Constraint

	// Size returns the number of constraints.
	Size() int
}

// SimilarityIndexView provides read-only access to similarity scores.
//
// Thread Safety: Safe for concurrent use (immutable).
type SimilarityIndexView interface {
	// Distance returns the similarity distance between two nodes.
	Distance(node1, node2 string) (float64, bool)

	// NearestNeighbors returns the k nearest neighbors of a node.
	NearestNeighbors(nodeID string, k int) []SimilarityMatch

	// Size returns the number of entries.
	Size() int
}

// DependencyIndexView provides read-only access to dependencies.
//
// Thread Safety: Safe for concurrent use (immutable).
type DependencyIndexView interface {
	// DependsOn returns all nodes that nodeID depends on.
	DependsOn(nodeID string) []string

	// DependedBy returns all nodes that depend on nodeID.
	DependedBy(nodeID string) []string

	// HasCycle returns true if there's a cycle involving nodeID.
	HasCycle(nodeID string) bool

	// Size returns the number of dependency edges.
	Size() int
}

// HistoryIndexView provides read-only access to decision history.
//
// Thread Safety: Safe for concurrent use (immutable).
type HistoryIndexView interface {
	// Trace returns the decision trace for a node.
	Trace(nodeID string) []HistoryEntry

	// Recent returns the N most recent history entries.
	Recent(n int) []HistoryEntry

	// Size returns the number of history entries.
	Size() int
}

// StreamingIndexView provides read-only access to streaming statistics.
//
// Thread Safety: Safe for concurrent use (immutable).
type StreamingIndexView interface {
	// Estimate returns the frequency estimate for an item.
	Estimate(item string) uint64

	// Cardinality returns the estimated unique item count.
	Cardinality() uint64

	// Size returns the approximate memory usage in bytes.
	Size() int
}

// -----------------------------------------------------------------------------
// Data Types
// -----------------------------------------------------------------------------

// ProofNumber represents proof and disproof numbers for a node.
type ProofNumber struct {
	// Proof is the proof number (cost to prove this node).
	Proof uint64

	// Disproof is the disproof number (cost to disprove this node).
	Disproof uint64

	// Status is the current proof status.
	Status ProofStatus

	// Source indicates where this proof came from.
	Source SignalSource

	// UpdatedAt is when this proof was last updated.
	UpdatedAt time.Time
}

// ProofStatus represents the proof status of a node.
type ProofStatus int

const (
	// ProofStatusUnknown means the node's proof status is not determined.
	ProofStatusUnknown ProofStatus = iota

	// ProofStatusProven means the node is proven (leads to solution).
	ProofStatusProven

	// ProofStatusDisproven means the node is disproven (does not lead to solution).
	ProofStatusDisproven

	// ProofStatusExpanded means the node has been expanded but not proven/disproven.
	ProofStatusExpanded
)

// String returns the string representation of ProofStatus.
func (s ProofStatus) String() string {
	switch s {
	case ProofStatusUnknown:
		return "unknown"
	case ProofStatusProven:
		return "proven"
	case ProofStatusDisproven:
		return "disproven"
	case ProofStatusExpanded:
		return "expanded"
	default:
		return fmt.Sprintf("ProofStatus(%d)", s)
	}
}

// SignalSource indicates where a signal came from (hard vs soft).
type SignalSource int

const (
	// SignalSourceUnknown means the source is not known.
	SignalSourceUnknown SignalSource = iota

	// SignalSourceHard means the signal came from a hard source (compiler, tests).
	SignalSourceHard

	// SignalSourceSoft means the signal came from a soft source (LLM).
	SignalSourceSoft
)

// String returns the string representation of SignalSource.
func (s SignalSource) String() string {
	switch s {
	case SignalSourceUnknown:
		return "unknown"
	case SignalSourceHard:
		return "hard"
	case SignalSourceSoft:
		return "soft"
	default:
		return fmt.Sprintf("SignalSource(%d)", s)
	}
}

// IsHard returns true if this is a hard signal source.
func (s SignalSource) IsHard() bool {
	return s == SignalSourceHard
}

// Constraint represents a constraint on the search space.
type Constraint struct {
	// ID is the unique constraint identifier.
	ID string

	// Type is the constraint type.
	Type ConstraintType

	// Nodes are the nodes this constraint affects.
	Nodes []string

	// Expression is the constraint expression.
	Expression string

	// Active indicates if the constraint is currently active.
	Active bool

	// Source indicates where this constraint came from.
	Source SignalSource

	// CreatedAt is when this constraint was created.
	CreatedAt time.Time
}

// ConstraintType represents the type of constraint.
type ConstraintType int

const (
	// ConstraintTypeUnknown is an unknown constraint type.
	ConstraintTypeUnknown ConstraintType = iota

	// ConstraintTypeMutualExclusion means nodes cannot be selected together.
	ConstraintTypeMutualExclusion

	// ConstraintTypeImplication means selecting one node implies another.
	ConstraintTypeImplication

	// ConstraintTypeOrdering means nodes must be selected in order.
	ConstraintTypeOrdering

	// ConstraintTypeResource means nodes share a resource limit.
	ConstraintTypeResource
)

// String returns the string representation of ConstraintType.
func (t ConstraintType) String() string {
	switch t {
	case ConstraintTypeUnknown:
		return "unknown"
	case ConstraintTypeMutualExclusion:
		return "mutual_exclusion"
	case ConstraintTypeImplication:
		return "implication"
	case ConstraintTypeOrdering:
		return "ordering"
	case ConstraintTypeResource:
		return "resource"
	default:
		return fmt.Sprintf("ConstraintType(%d)", t)
	}
}

// SimilarityMatch represents a similarity search result.
type SimilarityMatch struct {
	// NodeID is the matching node.
	NodeID string

	// Distance is the similarity distance (lower = more similar).
	Distance float64
}

// HistoryEntry represents a single decision in the history.
type HistoryEntry struct {
	// ID is the unique entry identifier.
	ID string

	// NodeID is the node this decision was about.
	NodeID string

	// Action is the action taken.
	Action string

	// Result is the outcome of the action.
	Result string

	// Source indicates where this decision came from.
	Source SignalSource

	// Timestamp is when this decision was made.
	Timestamp time.Time

	// Metadata contains additional context.
	Metadata map[string]string
}

// -----------------------------------------------------------------------------
// Delta Types
// -----------------------------------------------------------------------------

// DeltaType identifies the type of delta.
type DeltaType int

const (
	// DeltaTypeUnknown is an unknown delta type.
	DeltaTypeUnknown DeltaType = iota

	// DeltaTypeProof updates proof numbers.
	DeltaTypeProof

	// DeltaTypeConstraint updates constraints.
	DeltaTypeConstraint

	// DeltaTypeSimilarity updates similarity scores.
	DeltaTypeSimilarity

	// DeltaTypeDependency updates dependencies.
	DeltaTypeDependency

	// DeltaTypeHistory adds history entries.
	DeltaTypeHistory

	// DeltaTypeStreaming updates streaming statistics.
	DeltaTypeStreaming

	// DeltaTypeComposite contains multiple deltas.
	DeltaTypeComposite
)

// String returns the string representation of DeltaType.
func (t DeltaType) String() string {
	switch t {
	case DeltaTypeUnknown:
		return "unknown"
	case DeltaTypeProof:
		return "proof"
	case DeltaTypeConstraint:
		return "constraint"
	case DeltaTypeSimilarity:
		return "similarity"
	case DeltaTypeDependency:
		return "dependency"
	case DeltaTypeHistory:
		return "history"
	case DeltaTypeStreaming:
		return "streaming"
	case DeltaTypeComposite:
		return "composite"
	default:
		return fmt.Sprintf("DeltaType(%d)", t)
	}
}

// Delta represents an atomic change to CRS state.
//
// Description:
//
//	Algorithms produce deltas that describe state changes. Deltas are
//	validated before application and can be merged with other deltas.
//
// Thread Safety: Implementations must be safe for concurrent use.
type Delta interface {
	// Type returns the delta type.
	Type() DeltaType

	// Validate checks if this delta can be applied to the snapshot.
	//
	// Inputs:
	//   - snapshot: The snapshot to validate against.
	//
	// Outputs:
	//   - error: Non-nil if validation fails.
	Validate(snapshot Snapshot) error

	// Merge combines this delta with another delta.
	//
	// Inputs:
	//   - other: The delta to merge with.
	//
	// Outputs:
	//   - Delta: The merged delta.
	//   - error: Non-nil if deltas cannot be merged.
	Merge(other Delta) (Delta, error)

	// ConflictsWith returns true if this delta conflicts with another.
	//
	// Inputs:
	//   - other: The delta to check for conflicts.
	//
	// Outputs:
	//   - bool: True if deltas conflict.
	ConflictsWith(other Delta) bool

	// Source returns the signal source for this delta.
	Source() SignalSource

	// Timestamp returns when this delta was created.
	Timestamp() time.Time

	// IndexesAffected returns which indexes this delta will modify.
	IndexesAffected() []string
}

// -----------------------------------------------------------------------------
// Apply Metrics
// -----------------------------------------------------------------------------

// ApplyMetrics contains metrics about an Apply operation.
type ApplyMetrics struct {
	// DeltaType is the type of delta applied.
	DeltaType DeltaType

	// ApplyDuration is how long the apply took.
	ApplyDuration time.Duration

	// ValidationDuration is how long validation took.
	ValidationDuration time.Duration

	// IndexesUpdated lists which indexes were updated.
	IndexesUpdated []string

	// EntriesModified is the number of entries modified.
	EntriesModified int

	// OldGeneration is the generation before apply.
	OldGeneration int64

	// NewGeneration is the generation after apply.
	NewGeneration int64
}

// -----------------------------------------------------------------------------
// Checkpoint
// -----------------------------------------------------------------------------

// Checkpoint represents a restorable state checkpoint.
type Checkpoint struct {
	// ID is the unique checkpoint identifier.
	ID string

	// Generation is the generation at checkpoint time.
	Generation int64

	// CreatedAt is when the checkpoint was created.
	CreatedAt time.Time

	// data holds the checkpoint data (opaque to consumers).
	data any
}

// -----------------------------------------------------------------------------
// Configuration
// -----------------------------------------------------------------------------

// Config configures a CRS instance.
type Config struct {
	// MaxGeneration is the maximum generation before wrapping.
	// Default: 0 (no maximum, wraps at int64 max).
	MaxGeneration int64

	// SnapshotEpochLimit is how many generations a snapshot is valid.
	// Default: 1000 (snapshots older than 1000 generations are stale).
	SnapshotEpochLimit int64

	// EnableMetrics enables metrics collection.
	// Default: true.
	EnableMetrics bool

	// EnableTracing enables OpenTelemetry tracing.
	// Default: true.
	EnableTracing bool
}

// DefaultConfig returns the default configuration.
func DefaultConfig() *Config {
	return &Config{
		MaxGeneration:      0,
		SnapshotEpochLimit: 1000,
		EnableMetrics:      true,
		EnableTracing:      true,
	}
}

// Validate checks if the configuration is valid.
func (c *Config) Validate() error {
	if c.SnapshotEpochLimit < 0 {
		return errors.New("snapshot epoch limit must be non-negative")
	}
	return nil
}
