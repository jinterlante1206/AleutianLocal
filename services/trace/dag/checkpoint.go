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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// validDAGNamePattern defines valid characters for DAG names: alphanumeric, underscore, hyphen.
var validDAGNamePattern = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// CheckpointVersion is the current checkpoint format version (semver).
const CheckpointVersion = "1.0.0"

// checkpointState is a JSON-serializable copy of State for checksumming.
// This avoids issues with sync.RWMutex not being serializable.
type checkpointState struct {
	SessionID      string                `json:"session_id"`
	StartedAt      int64                 `json:"started_at"` // Unix milliseconds UTC
	CompletedNodes map[string]bool       `json:"completed_nodes"`
	NodeOutputs    map[string]any        `json:"node_outputs"`
	NodeStatuses   map[string]NodeStatus `json:"node_statuses"`
	CurrentNodes   []string              `json:"current_nodes"`
	FailedNode     string                `json:"failed_node,omitempty"`
	Error          string                `json:"error,omitempty"`
}

// toCheckpointState converts State to a serializable form.
// Uses JSON roundtrip for NodeOutputs to ensure deep copy of nested structures.
func (s *State) toCheckpointState() *checkpointState {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Deep copy maps
	completedNodes := make(map[string]bool, len(s.CompletedNodes))
	for k, v := range s.CompletedNodes {
		completedNodes[k] = v
	}

	// Deep copy NodeOutputs using JSON roundtrip to handle nested slices/maps
	nodeOutputs := make(map[string]any, len(s.NodeOutputs))
	if len(s.NodeOutputs) > 0 {
		data, err := json.Marshal(s.NodeOutputs)
		if err == nil {
			// Ignore unmarshal error - fall back to shallow copy if it fails
			_ = json.Unmarshal(data, &nodeOutputs)
		} else {
			// Fallback to shallow copy (shouldn't happen with valid JSON types)
			for k, v := range s.NodeOutputs {
				nodeOutputs[k] = v
			}
		}
	}

	nodeStatuses := make(map[string]NodeStatus, len(s.NodeStatuses))
	for k, v := range s.NodeStatuses {
		nodeStatuses[k] = v
	}

	currentNodes := make([]string, len(s.CurrentNodes))
	copy(currentNodes, s.CurrentNodes)

	return &checkpointState{
		SessionID:      s.SessionID,
		StartedAt:      s.StartedAt,
		CompletedNodes: completedNodes,
		NodeOutputs:    nodeOutputs,
		NodeStatuses:   nodeStatuses,
		CurrentNodes:   currentNodes,
		FailedNode:     s.FailedNode,
		Error:          s.Error,
	}
}

// toState converts checkpointState back to State.
func (cs *checkpointState) toState() *State {
	return &State{
		SessionID:      cs.SessionID,
		StartedAt:      cs.StartedAt,
		CompletedNodes: cs.CompletedNodes,
		NodeOutputs:    cs.NodeOutputs,
		NodeStatuses:   cs.NodeStatuses,
		CurrentNodes:   cs.CurrentNodes,
		FailedNode:     cs.FailedNode,
		Error:          cs.Error,
	}
}

// serializableCheckpoint is the on-disk format for checkpoints.
type serializableCheckpoint struct {
	State     *checkpointState `json:"state"`
	Timestamp time.Time        `json:"timestamp"`
	Version   string           `json:"version"`
	Checksum  string           `json:"checksum"`
	DAGName   string           `json:"dag_name"`
}

// computeChecksum calculates SHA256 of the state for integrity verification.
func computeChecksum(state *checkpointState, dagName string, timestamp time.Time) (string, error) {
	// Create a deterministic representation for checksumming
	// We exclude the checksum field itself
	data := struct {
		State     *checkpointState `json:"state"`
		Timestamp time.Time        `json:"timestamp"`
		Version   string           `json:"version"`
		DAGName   string           `json:"dag_name"`
	}{
		State:     state,
		Timestamp: timestamp,
		Version:   CheckpointVersion,
		DAGName:   dagName,
	}

	jsonData, err := json.Marshal(data)
	if err != nil {
		return "", fmt.Errorf("marshal for checksum: %w", err)
	}

	hash := sha256.Sum256(jsonData)
	return hex.EncodeToString(hash[:]), nil
}

// SaveCheckpoint serializes the current execution state to a file.
//
// Description:
//
//	Creates a checkpoint file containing the execution state, enabling
//	resume after failure or restart. Writes atomically using temp file + rename.
//
// Inputs:
//
//	state - The current execution state. Must not be nil.
//	dagName - Name of the DAG being executed.
//	path - File path to write checkpoint. Parent directory must exist.
//
// Outputs:
//
//	error - Non-nil if serialization or file write fails.
//
// Thread Safety:
//
//	Safe to call concurrently with DAG execution.
func SaveCheckpoint(state *State, dagName string, path string) error {
	if state == nil {
		return fmt.Errorf("%w: state must not be nil", ErrInvalidInput)
	}
	if dagName == "" {
		return fmt.Errorf("%w: dagName must not be empty", ErrInvalidInput)
	}
	if !validDAGNamePattern.MatchString(dagName) {
		return fmt.Errorf("%w: dagName must match pattern [a-zA-Z0-9_-]+, got %q", ErrInvalidInput, dagName)
	}
	if path == "" {
		return fmt.Errorf("%w: path must not be empty", ErrInvalidInput)
	}

	// Convert to serializable form
	cs := state.toCheckpointState()
	timestamp := time.Now()

	// Compute checksum
	checksum, err := computeChecksum(cs, dagName, timestamp)
	if err != nil {
		return fmt.Errorf("compute checksum: %w", err)
	}

	checkpoint := &serializableCheckpoint{
		State:     cs,
		Timestamp: timestamp,
		Version:   CheckpointVersion,
		Checksum:  checksum,
		DAGName:   dagName,
	}

	// Marshal to JSON with indentation for readability
	data, err := json.MarshalIndent(checkpoint, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal checkpoint: %w", err)
	}

	// Write atomically: temp file + rename
	dir := filepath.Dir(path)
	tempFile, err := os.CreateTemp(dir, ".checkpoint-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tempPath := tempFile.Name()

	// Ensure cleanup on failure
	success := false
	defer func() {
		if !success {
			os.Remove(tempPath)
		}
	}()

	if _, err := tempFile.Write(data); err != nil {
		tempFile.Close()
		return fmt.Errorf("write checkpoint: %w", err)
	}

	if err := tempFile.Sync(); err != nil {
		tempFile.Close()
		return fmt.Errorf("sync checkpoint: %w", err)
	}

	if err := tempFile.Close(); err != nil {
		return fmt.Errorf("close checkpoint: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("rename checkpoint: %w", err)
	}

	success = true
	return nil
}

// LoadCheckpoint reads and verifies a checkpoint from a file.
//
// Description:
//
//	Loads a previously saved checkpoint, verifying its integrity via checksum.
//	Returns an error if the checkpoint is corrupt or version mismatched.
//
// Inputs:
//
//	path - File path to read checkpoint from.
//
// Outputs:
//
//	*Checkpoint - The loaded checkpoint. Never nil on success.
//	error - Non-nil if file read, parse, or verification fails.
func LoadCheckpoint(path string) (*Checkpoint, error) {
	if path == "" {
		return nil, fmt.Errorf("%w: path must not be empty", ErrInvalidInput)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read checkpoint: %w", err)
	}

	var sc serializableCheckpoint
	if err := json.Unmarshal(data, &sc); err != nil {
		return nil, fmt.Errorf("unmarshal checkpoint: %w", err)
	}

	// Verify version
	if sc.Version != CheckpointVersion {
		return nil, fmt.Errorf("%w: got %s, want %s", ErrCheckpointVersionMismatch, sc.Version, CheckpointVersion)
	}

	// Verify checksum
	expectedChecksum, err := computeChecksum(sc.State, sc.DAGName, sc.Timestamp)
	if err != nil {
		return nil, fmt.Errorf("compute checksum for verification: %w", err)
	}

	if sc.Checksum != expectedChecksum {
		return nil, ErrCheckpointCorrupt
	}

	// Convert to Checkpoint with State
	return &Checkpoint{
		State:     sc.State.toState(),
		Timestamp: sc.Timestamp.UnixMilli(),
		Version:   sc.Version,
		Checksum:  sc.Checksum,
		DAGName:   sc.DAGName,
	}, nil
}

// Verify checks the checkpoint's integrity.
//
// Description:
//
//	Recalculates the checksum and compares it to the stored value.
//	Returns true if the checkpoint is valid.
//
// Outputs:
//
//	bool - True if checksum matches, false if corrupt.
func (c *Checkpoint) Verify() bool {
	if c == nil || c.State == nil {
		return false
	}

	cs := c.State.toCheckpointState()
	expectedChecksum, err := computeChecksum(cs, c.DAGName, time.UnixMilli(c.Timestamp))
	if err != nil {
		return false
	}

	return c.Checksum == expectedChecksum
}

// Resume continues DAG execution from a checkpoint.
//
// Description:
//
//	Verifies the checkpoint integrity and DAG compatibility, then resumes
//	execution from the saved state. Any previously failed node is cleared
//	to allow retry.
//
// Inputs:
//
//	ctx - Context for cancellation. Must not be nil.
//	checkpoint - The checkpoint to resume from. Must not be nil.
//
// Outputs:
//
//	*Result - Execution result including output and timing.
//	error - Non-nil if verification fails or execution fails.
func (e *Executor) Resume(ctx context.Context, checkpoint *Checkpoint) (*Result, error) {
	if ctx == nil {
		return nil, ErrNilContext
	}
	if checkpoint == nil {
		return nil, fmt.Errorf("%w: checkpoint must not be nil", ErrInvalidInput)
	}

	// Create span for resume operation
	ctx, span := tracer.Start(ctx, "dag.checkpoint.resume",
		trace.WithAttributes(
			attribute.String("dag.name", e.dag.Name()),
			attribute.String("checkpoint.dag_name", checkpoint.DAGName),
			attribute.String("checkpoint.session_id", checkpoint.State.SessionID),
			attribute.Int("checkpoint.completed_nodes", len(checkpoint.State.CompletedNodes)),
		),
	)
	defer span.End()

	// Verify checkpoint integrity
	if !checkpoint.Verify() {
		span.SetStatus(codes.Error, ErrCheckpointCorrupt.Error())
		return nil, ErrCheckpointCorrupt
	}

	// Verify DAG compatibility
	if checkpoint.DAGName != e.dag.Name() {
		err := fmt.Errorf("%w: checkpoint is for DAG %q, but executor has DAG %q",
			ErrInvalidInput, checkpoint.DAGName, e.dag.Name())
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}

	e.logger.Info("resuming from checkpoint",
		slog.String("session_id", checkpoint.State.SessionID),
		slog.Int("completed_nodes", len(checkpoint.State.CompletedNodes)),
		slog.Time("checkpoint_time", time.UnixMilli(checkpoint.Timestamp)),
	)

	// Delegate to RunFromState
	return e.RunFromState(ctx, checkpoint.State)
}
