// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package history

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// HistoryStore persists blast radius events for trending analysis.
//
// # Description
//
// Uses a two-tier in-memory storage strategy:
//   - Hot tier: Ring buffer for recent events (fast queries)
//   - Cold tier: Slice with periodic persistence to JSON file
//
// NO external dependencies (no SQLite, no Redis).
//
// # Thread Safety
//
// Safe for concurrent use.
type HistoryStore struct {
	mu       sync.RWMutex
	ring     *RingBuffer[HistoryEvent]
	cold     []HistoryEvent // In-memory cold storage
	dataDir  string
	ringSize int
	maxCold  int // Maximum cold storage entries

	// Configuration
	retentionDays int
}

// HistoryEvent represents a blast radius event to track.
type HistoryEvent struct {
	// Timestamp is when this event occurred.
	Timestamp time.Time `json:"timestamp"`

	// SymbolID is the symbol that was analyzed.
	SymbolID string `json:"symbol_id"`

	// RiskLevel is the computed risk level.
	RiskLevel string `json:"risk_level"`

	// DirectCallers is the number of direct callers.
	DirectCallers int `json:"direct_callers"`

	// TransitiveCallers is the number of transitive callers.
	TransitiveCallers int `json:"transitive_callers,omitempty"`

	// Confidence is the analysis confidence (0-100).
	Confidence int `json:"confidence"`

	// GraphGen is the graph generation when analyzed.
	GraphGen uint64 `json:"graph_gen"`

	// ProjectRoot is the project identifier.
	ProjectRoot string `json:"project_root,omitempty"`
}

// StoreOptions configures the history store.
type StoreOptions struct {
	// RingSize is the size of the in-memory ring buffer (hot tier).
	// Default: 1000
	RingSize int

	// MaxColdEntries is the maximum entries in cold storage.
	// Default: 10000
	MaxColdEntries int

	// RetentionDays is how long to keep events.
	// Default: 90 days
	RetentionDays int

	// PersistPath is the optional path for JSON persistence.
	// If empty, data is memory-only (lost on restart).
	PersistPath string
}

// DefaultStoreOptions returns sensible defaults.
func DefaultStoreOptions() StoreOptions {
	return StoreOptions{
		RingSize:       1000,
		MaxColdEntries: 10000,
		RetentionDays:  90,
	}
}

// NewHistoryStore creates a new history store.
//
// # Inputs
//
//   - dataDir: Directory for optional JSON persistence. Empty for memory-only.
//   - opts: Optional configuration (nil uses defaults).
//
// # Outputs
//
//   - *HistoryStore: Ready-to-use store.
//   - error: Non-nil if loading persisted data failed.
func NewHistoryStore(dataDir string, opts *StoreOptions) (*HistoryStore, error) {
	if opts == nil {
		defaults := DefaultStoreOptions()
		opts = &defaults
	}

	store := &HistoryStore{
		ring:          NewRingBuffer[HistoryEvent](opts.RingSize),
		cold:          make([]HistoryEvent, 0, opts.MaxColdEntries),
		dataDir:       dataDir,
		ringSize:      opts.RingSize,
		maxCold:       opts.MaxColdEntries,
		retentionDays: opts.RetentionDays,
	}

	// Try to load persisted data if dataDir is provided
	if dataDir != "" {
		if err := store.loadPersisted(); err != nil {
			// Non-fatal, just start fresh
			_ = err
		}
	}

	return store, nil
}

// loadPersisted loads data from JSON file.
func (s *HistoryStore) loadPersisted() error {
	if s.dataDir == "" {
		return nil
	}

	path := filepath.Join(s.dataDir, "history.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // File doesn't exist, that's OK
		}
		return err
	}

	var events []HistoryEvent
	if err := json.Unmarshal(data, &events); err != nil {
		return err
	}

	s.cold = events
	return nil
}

// Start begins background maintenance (no-op for in-memory store).
func (s *HistoryStore) Start(ctx context.Context) {
	// No background goroutines needed for in-memory store
	// Periodic vacuum can be done on-demand
}

// Record adds an event to the ring buffer.
//
// # Description
//
// Non-blocking. If the ring buffer is full, events are moved to cold storage.
//
// # Inputs
//
//   - event: The event to record.
func (s *HistoryStore) Record(event HistoryEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Ensure timestamp is set
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	// If ring is full, flush oldest to cold before adding new
	if s.ring.IsFull() {
		s.flushOldestToCold()
	}

	s.ring.Push(event)
}

// flushOldestToCold moves oldest ring entries to cold storage.
// Must be called with lock held.
func (s *HistoryStore) flushOldestToCold() {
	// Move half the ring to cold
	count := s.ring.Len() / 2
	events := s.ring.First(count)

	for _, e := range events {
		s.ring.Pop()
		s.cold = append(s.cold, e)
	}

	// Trim cold storage if too large
	if len(s.cold) > s.maxCold {
		// Keep most recent
		s.cold = s.cold[len(s.cold)-s.maxCold:]
	}
}

// Flush triggers moving ring data to cold storage.
func (s *HistoryStore) Flush() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.flushOldestToCold()
}

// Query retrieves events for a symbol within a time range.
//
// # Inputs
//
//   - symbolID: The symbol to query.
//   - since: Start of time range.
//   - until: End of time range.
//
// # Outputs
//
//   - []HistoryEvent: Events in the time range.
//   - error: Non-nil on database error.
func (s *HistoryStore) Query(symbolID string, since, until time.Time) ([]HistoryEvent, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var events []HistoryEvent

	// Search cold storage
	for _, e := range s.cold {
		if e.SymbolID == symbolID &&
			!e.Timestamp.Before(since) &&
			!e.Timestamp.After(until) {
			events = append(events, e)
		}
	}

	// Search ring buffer
	s.ring.ForEach(func(e HistoryEvent) bool {
		if e.SymbolID == symbolID &&
			!e.Timestamp.Before(since) &&
			!e.Timestamp.After(until) {
			events = append(events, e)
		}
		return true
	})

	// Sort by timestamp
	sort.Slice(events, func(i, j int) bool {
		return events[i].Timestamp.Before(events[j].Timestamp)
	})

	return events, nil
}

// QueryRecent returns the most recent events across all symbols.
//
// # Inputs
//
//   - limit: Maximum number of events to return.
//
// # Outputs
//
//   - []HistoryEvent: Recent events, newest first.
//   - error: Non-nil on failure.
func (s *HistoryStore) QueryRecent(limit int) ([]HistoryEvent, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Collect all events
	var all []HistoryEvent
	all = append(all, s.cold...)

	s.ring.ForEach(func(e HistoryEvent) bool {
		all = append(all, e)
		return true
	})

	// Sort by timestamp descending
	sort.Slice(all, func(i, j int) bool {
		return all[i].Timestamp.After(all[j].Timestamp)
	})

	// Limit
	if len(all) > limit {
		all = all[:limit]
	}

	return all, nil
}

// GetSymbolHistory returns the full history for a symbol.
func (s *HistoryStore) GetSymbolHistory(symbolID string) ([]HistoryEvent, error) {
	return s.Query(symbolID, time.Time{}, time.Now())
}

// GetRecentForSymbol returns recent events for a symbol.
func (s *HistoryStore) GetRecentForSymbol(symbolID string, limit int) ([]HistoryEvent, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var events []HistoryEvent

	// Search all storage
	for _, e := range s.cold {
		if e.SymbolID == symbolID {
			events = append(events, e)
		}
	}

	s.ring.ForEach(func(e HistoryEvent) bool {
		if e.SymbolID == symbolID {
			events = append(events, e)
		}
		return true
	})

	// Sort by timestamp descending
	sort.Slice(events, func(i, j int) bool {
		return events[i].Timestamp.After(events[j].Timestamp)
	})

	// Limit
	if len(events) > limit {
		events = events[:limit]
	}

	return events, nil
}

// Vacuum removes records older than retention period.
func (s *HistoryStore) Vacuum() {
	s.mu.Lock()
	defer s.mu.Unlock()

	cutoff := time.Now().Add(-time.Duration(s.retentionDays) * 24 * time.Hour)

	// Filter cold storage
	filtered := make([]HistoryEvent, 0, len(s.cold))
	for _, e := range s.cold {
		if e.Timestamp.After(cutoff) {
			filtered = append(filtered, e)
		}
	}
	s.cold = filtered
}

// StoreStats contains store statistics.
type StoreStats struct {
	RingCount    int   `json:"ring_count"`
	RingCapacity int   `json:"ring_capacity"`
	ColdCount    int   `json:"cold_count"`
	TotalEvents  int   `json:"total_events"`
	OldestEvent  int64 `json:"oldest_event_ms,omitempty"`
	NewestEvent  int64 `json:"newest_event_ms,omitempty"`
}

// Stats returns current statistics.
func (s *HistoryStore) Stats() StoreStats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	stats := StoreStats{
		RingCount:    s.ring.Len(),
		RingCapacity: s.ring.Cap(),
		ColdCount:    len(s.cold),
		TotalEvents:  s.ring.Len() + len(s.cold),
	}

	// Find oldest and newest
	var oldest, newest time.Time

	for _, e := range s.cold {
		if oldest.IsZero() || e.Timestamp.Before(oldest) {
			oldest = e.Timestamp
		}
		if newest.IsZero() || e.Timestamp.After(newest) {
			newest = e.Timestamp
		}
	}

	s.ring.ForEach(func(e HistoryEvent) bool {
		if oldest.IsZero() || e.Timestamp.Before(oldest) {
			oldest = e.Timestamp
		}
		if newest.IsZero() || e.Timestamp.After(newest) {
			newest = e.Timestamp
		}
		return true
	})

	if !oldest.IsZero() {
		stats.OldestEvent = oldest.UnixMilli()
	}
	if !newest.IsZero() {
		stats.NewestEvent = newest.UnixMilli()
	}

	return stats
}

// Persist saves data to JSON file (if dataDir was provided).
func (s *HistoryStore) Persist() error {
	if s.dataDir == "" {
		return nil
	}

	s.mu.RLock()
	// Collect all events
	var all []HistoryEvent
	all = append(all, s.cold...)
	all = append(all, s.ring.Slice()...)
	s.mu.RUnlock()

	// Ensure directory exists
	if err := os.MkdirAll(s.dataDir, 0755); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}

	data, err := json.Marshal(all)
	if err != nil {
		return fmt.Errorf("marshal events: %w", err)
	}

	path := filepath.Join(s.dataDir, "history.json")
	return os.WriteFile(path, data, 0644)
}

// Close persists data and cleans up.
func (s *HistoryStore) Close() error {
	return s.Persist()
}
