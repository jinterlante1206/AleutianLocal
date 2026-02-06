// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package mcts

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sync"
	"time"
)

// AuditAction represents an action type in the audit log.
type AuditAction string

const (
	// AuditActionExpand records node expansion.
	AuditActionExpand AuditAction = "expand"

	// AuditActionSimulate records simulation execution.
	AuditActionSimulate AuditAction = "simulate"

	// AuditActionBackprop records score backpropagation.
	AuditActionBackprop AuditAction = "backprop"

	// AuditActionAbandon records node abandonment.
	AuditActionAbandon AuditAction = "abandon"

	// AuditActionPrune records tree pruning.
	AuditActionPrune AuditAction = "prune"

	// AuditActionSelect records node selection.
	AuditActionSelect AuditAction = "select"
)

// String returns the string representation.
func (a AuditAction) String() string {
	return string(a)
}

// AuditEntry records an action in the tree for audit trail.
//
// Each entry is immutable once created. The hash chain ensures
// the integrity of the audit log can be verified.
type AuditEntry struct {
	// Timestamp when this entry was created (Unix milliseconds UTC).
	Timestamp int64 `json:"timestamp"`

	// Action is the type of operation performed.
	Action AuditAction `json:"action"`

	// NodeID identifies the affected node.
	NodeID string `json:"node_id"`

	// NodeHash is the content hash of the node at this time.
	NodeHash string `json:"node_hash,omitempty"`

	// ParentHash is the content hash of the parent node.
	ParentHash string `json:"parent_hash,omitempty"`

	// LLMRequestID links to the LLM request that generated this action.
	LLMRequestID string `json:"llm_request_id,omitempty"`

	// Score is the score associated with this action.
	Score float64 `json:"score,omitempty"`

	// Details contains additional action-specific information.
	Details string `json:"details,omitempty"`

	// ChainHash is the running hash at this entry (computed during Record).
	ChainHash string `json:"chain_hash,omitempty"`
}

// NewAuditEntry creates a new audit entry.
//
// Inputs:
//   - action: The action being recorded.
//   - nodeID: ID of the affected node.
//
// Outputs:
//   - *AuditEntry: A new entry with timestamp set.
func NewAuditEntry(action AuditAction, nodeID string) *AuditEntry {
	return &AuditEntry{
		Timestamp: time.Now().UnixMilli(),
		Action:    action,
		NodeID:    nodeID,
	}
}

// WithNodeHash sets the node hash.
func (e *AuditEntry) WithNodeHash(hash string) *AuditEntry {
	e.NodeHash = hash
	return e
}

// WithParentHash sets the parent hash.
func (e *AuditEntry) WithParentHash(hash string) *AuditEntry {
	e.ParentHash = hash
	return e
}

// WithLLMRequestID sets the LLM request ID.
func (e *AuditEntry) WithLLMRequestID(id string) *AuditEntry {
	e.LLMRequestID = id
	return e
}

// WithScore sets the score.
func (e *AuditEntry) WithScore(score float64) *AuditEntry {
	e.Score = score
	return e
}

// WithDetails sets the details.
func (e *AuditEntry) WithDetails(details string) *AuditEntry {
	e.Details = details
	return e
}

// genesisHash is the initial hash value for the audit chain.
const genesisHash = "genesis"

// AuditLog maintains an immutable audit trail with hash chain verification.
//
// The audit log uses a hash chain where each entry includes a hash of
// itself combined with the previous hash. This allows verification
// of the log's integrity.
//
// Thread Safety: Safe for concurrent use.
type AuditLog struct {
	mu      sync.RWMutex
	entries []AuditEntry
	hash    string
}

// NewAuditLog creates a new audit log.
//
// Outputs:
//   - *AuditLog: An empty audit log with genesis hash.
//
// Thread Safety: The returned log is safe for concurrent use.
func NewAuditLog() *AuditLog {
	return &AuditLog{
		entries: make([]AuditEntry, 0),
		hash:    genesisHash,
	}
}

// Record adds an entry to the audit log.
//
// The entry's timestamp is set to the current time, and a chain hash
// is computed that includes the previous hash and entry data.
//
// Inputs:
//   - entry: The audit entry to record.
//
// Thread Safety: Safe for concurrent use.
func (l *AuditLog) Record(entry AuditEntry) {
	l.mu.Lock()
	defer l.mu.Unlock()

	// Ensure timestamp is set
	if entry.Timestamp == 0 {
		entry.Timestamp = time.Now().UnixMilli()
	}

	// Compute chain hash
	h := sha256.New()
	h.Write([]byte(l.hash))
	data, _ := json.Marshal(entry)
	h.Write(data)
	l.hash = hex.EncodeToString(h.Sum(nil))

	// Set chain hash on entry
	entry.ChainHash = l.hash

	l.entries = append(l.entries, entry)
}

// Verify checks the integrity of the audit log.
//
// Recomputes the hash chain from genesis and verifies it matches
// the current hash. Returns false if any tampering is detected.
//
// Outputs:
//   - bool: True if the log is intact, false if tampered.
//
// Thread Safety: Safe for concurrent use.
func (l *AuditLog) Verify() bool {
	l.mu.RLock()
	defer l.mu.RUnlock()

	hash := genesisHash
	for _, entry := range l.entries {
		// Make a copy without the chain hash for verification
		entryCopy := entry
		entryCopy.ChainHash = ""

		h := sha256.New()
		h.Write([]byte(hash))
		data, _ := json.Marshal(entryCopy)
		h.Write(data)
		hash = hex.EncodeToString(h.Sum(nil))

		// Verify this entry's chain hash matches
		if entry.ChainHash != hash {
			return false
		}
	}

	return hash == l.hash
}

// Entries returns a copy of all audit entries.
//
// Outputs:
//   - []AuditEntry: A copy of all entries.
//
// Thread Safety: Safe for concurrent use.
func (l *AuditLog) Entries() []AuditEntry {
	l.mu.RLock()
	defer l.mu.RUnlock()

	result := make([]AuditEntry, len(l.entries))
	copy(result, l.entries)
	return result
}

// Len returns the number of entries.
//
// Thread Safety: Safe for concurrent use.
func (l *AuditLog) Len() int {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return len(l.entries)
}

// CurrentHash returns the current chain hash.
//
// Thread Safety: Safe for concurrent use.
func (l *AuditLog) CurrentHash() string {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.hash
}

// EntriesAfter returns entries recorded after the given timestamp.
//
// Inputs:
//   - after: Return entries after this time.
//
// Outputs:
//   - []AuditEntry: Entries after the given time.
//
// Thread Safety: Safe for concurrent use.
func (l *AuditLog) EntriesAfter(after time.Time) []AuditEntry {
	l.mu.RLock()
	defer l.mu.RUnlock()

	afterMillis := after.UnixMilli()
	result := make([]AuditEntry, 0)
	for _, entry := range l.entries {
		if entry.Timestamp > afterMillis {
			result = append(result, entry)
		}
	}
	return result
}

// EntriesByAction returns entries with the given action type.
//
// Inputs:
//   - action: The action type to filter by.
//
// Outputs:
//   - []AuditEntry: Entries with the given action.
//
// Thread Safety: Safe for concurrent use.
func (l *AuditLog) EntriesByAction(action AuditAction) []AuditEntry {
	l.mu.RLock()
	defer l.mu.RUnlock()

	result := make([]AuditEntry, 0)
	for _, entry := range l.entries {
		if entry.Action == action {
			result = append(result, entry)
		}
	}
	return result
}

// EntriesByNode returns entries for a specific node.
//
// Inputs:
//   - nodeID: The node ID to filter by.
//
// Outputs:
//   - []AuditEntry: Entries for the given node.
//
// Thread Safety: Safe for concurrent use.
func (l *AuditLog) EntriesByNode(nodeID string) []AuditEntry {
	l.mu.RLock()
	defer l.mu.RUnlock()

	result := make([]AuditEntry, 0)
	for _, entry := range l.entries {
		if entry.NodeID == nodeID {
			result = append(result, entry)
		}
	}
	return result
}

// Summary returns a summary of the audit log.
//
// Outputs:
//   - AuditSummary: Summary statistics.
//
// Thread Safety: Safe for concurrent use.
func (l *AuditLog) Summary() AuditSummary {
	l.mu.RLock()
	defer l.mu.RUnlock()

	summary := AuditSummary{
		TotalEntries: len(l.entries),
		ActionCounts: make(map[AuditAction]int),
	}

	if len(l.entries) > 0 {
		summary.FirstEntry = l.entries[0].Timestamp
		summary.LastEntry = l.entries[len(l.entries)-1].Timestamp
	}

	for _, entry := range l.entries {
		summary.ActionCounts[entry.Action]++
	}

	return summary
}

// AuditSummary contains summary statistics for the audit log.
type AuditSummary struct {
	TotalEntries int                 `json:"total_entries"`
	FirstEntry   int64               `json:"first_entry,omitempty"` // Unix milliseconds UTC
	LastEntry    int64               `json:"last_entry,omitempty"`  // Unix milliseconds UTC
	ActionCounts map[AuditAction]int `json:"action_counts"`
}
