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
	"sync"
	"testing"
	"time"
)

func TestAuditAction_String(t *testing.T) {
	tests := []struct {
		action   AuditAction
		expected string
	}{
		{AuditActionExpand, "expand"},
		{AuditActionSimulate, "simulate"},
		{AuditActionBackprop, "backprop"},
		{AuditActionAbandon, "abandon"},
		{AuditActionPrune, "prune"},
		{AuditActionSelect, "select"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.action.String(); got != tt.expected {
				t.Errorf("String() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestNewAuditEntry(t *testing.T) {
	before := time.Now().UnixMilli()
	entry := NewAuditEntry(AuditActionExpand, "node-1")
	after := time.Now().UnixMilli()

	if entry.Action != AuditActionExpand {
		t.Errorf("Action = %v, want expand", entry.Action)
	}
	if entry.NodeID != "node-1" {
		t.Errorf("NodeID = %v, want node-1", entry.NodeID)
	}
	if entry.Timestamp < before || entry.Timestamp > after {
		t.Errorf("Timestamp not in expected range")
	}
}

func TestAuditEntry_Fluent(t *testing.T) {
	entry := NewAuditEntry(AuditActionSimulate, "node-2").
		WithNodeHash("abc123").
		WithParentHash("def456").
		WithLLMRequestID("req-789").
		WithScore(0.85).
		WithDetails("test details")

	if entry.NodeHash != "abc123" {
		t.Errorf("NodeHash = %v, want abc123", entry.NodeHash)
	}
	if entry.ParentHash != "def456" {
		t.Errorf("ParentHash = %v, want def456", entry.ParentHash)
	}
	if entry.LLMRequestID != "req-789" {
		t.Errorf("LLMRequestID = %v, want req-789", entry.LLMRequestID)
	}
	if entry.Score != 0.85 {
		t.Errorf("Score = %v, want 0.85", entry.Score)
	}
	if entry.Details != "test details" {
		t.Errorf("Details = %v, want test details", entry.Details)
	}
}

func TestNewAuditLog(t *testing.T) {
	log := NewAuditLog()

	if log.Len() != 0 {
		t.Errorf("Len() = %d, want 0", log.Len())
	}
	if log.CurrentHash() != genesisHash {
		t.Errorf("CurrentHash() = %v, want %v", log.CurrentHash(), genesisHash)
	}
}

func TestAuditLog_Record(t *testing.T) {
	log := NewAuditLog()

	entry1 := NewAuditEntry(AuditActionExpand, "node-1")
	log.Record(*entry1)

	if log.Len() != 1 {
		t.Errorf("Len() = %d, want 1", log.Len())
	}

	entries := log.Entries()
	if entries[0].Action != AuditActionExpand {
		t.Errorf("Entry action = %v, want expand", entries[0].Action)
	}
	if entries[0].ChainHash == "" {
		t.Error("ChainHash should be set")
	}
	if entries[0].ChainHash == genesisHash {
		t.Error("ChainHash should be different from genesis")
	}
}

func TestAuditLog_RecordMultiple(t *testing.T) {
	log := NewAuditLog()

	log.Record(*NewAuditEntry(AuditActionExpand, "node-1"))
	hash1 := log.CurrentHash()

	log.Record(*NewAuditEntry(AuditActionSimulate, "node-2"))
	hash2 := log.CurrentHash()

	log.Record(*NewAuditEntry(AuditActionBackprop, "node-3"))
	hash3 := log.CurrentHash()

	if log.Len() != 3 {
		t.Errorf("Len() = %d, want 3", log.Len())
	}

	// Each hash should be different
	if hash1 == hash2 || hash2 == hash3 || hash1 == hash3 {
		t.Error("Each entry should produce a different hash")
	}
}

func TestAuditLog_Verify_Valid(t *testing.T) {
	log := NewAuditLog()

	log.Record(*NewAuditEntry(AuditActionExpand, "node-1"))
	log.Record(*NewAuditEntry(AuditActionSimulate, "node-1").WithScore(0.9))
	log.Record(*NewAuditEntry(AuditActionBackprop, "node-1"))

	if !log.Verify() {
		t.Error("Verify() should return true for valid log")
	}
}

func TestAuditLog_Verify_EmptyLog(t *testing.T) {
	log := NewAuditLog()

	if !log.Verify() {
		t.Error("Verify() should return true for empty log")
	}
}

func TestAuditLog_Verify_Tampered(t *testing.T) {
	log := NewAuditLog()

	log.Record(*NewAuditEntry(AuditActionExpand, "node-1"))
	log.Record(*NewAuditEntry(AuditActionSimulate, "node-1"))
	log.Record(*NewAuditEntry(AuditActionBackprop, "node-1"))

	// Tamper with the log by directly modifying an entry
	log.mu.Lock()
	log.entries[1].Score = 999.0 // Modify a field
	log.mu.Unlock()

	if log.Verify() {
		t.Error("Verify() should return false for tampered log")
	}
}

func TestAuditLog_Entries_Copy(t *testing.T) {
	log := NewAuditLog()

	log.Record(*NewAuditEntry(AuditActionExpand, "node-1"))

	entries := log.Entries()
	entries[0].NodeID = "modified"

	// Original should be unchanged
	originalEntries := log.Entries()
	if originalEntries[0].NodeID == "modified" {
		t.Error("Entries() should return a copy")
	}
}

func TestAuditLog_EntriesAfter(t *testing.T) {
	log := NewAuditLog()

	before := time.Now()
	time.Sleep(10 * time.Millisecond)

	log.Record(*NewAuditEntry(AuditActionExpand, "node-1"))
	log.Record(*NewAuditEntry(AuditActionSimulate, "node-2"))

	entries := log.EntriesAfter(before)
	if len(entries) != 2 {
		t.Errorf("EntriesAfter() = %d entries, want 2", len(entries))
	}

	future := time.Now().Add(time.Hour)
	entries = log.EntriesAfter(future)
	if len(entries) != 0 {
		t.Errorf("EntriesAfter(future) = %d entries, want 0", len(entries))
	}
}

func TestAuditLog_EntriesByAction(t *testing.T) {
	log := NewAuditLog()

	log.Record(*NewAuditEntry(AuditActionExpand, "node-1"))
	log.Record(*NewAuditEntry(AuditActionSimulate, "node-1"))
	log.Record(*NewAuditEntry(AuditActionExpand, "node-2"))
	log.Record(*NewAuditEntry(AuditActionBackprop, "node-1"))

	entries := log.EntriesByAction(AuditActionExpand)
	if len(entries) != 2 {
		t.Errorf("EntriesByAction(expand) = %d entries, want 2", len(entries))
	}

	entries = log.EntriesByAction(AuditActionAbandon)
	if len(entries) != 0 {
		t.Errorf("EntriesByAction(abandon) = %d entries, want 0", len(entries))
	}
}

func TestAuditLog_EntriesByNode(t *testing.T) {
	log := NewAuditLog()

	log.Record(*NewAuditEntry(AuditActionExpand, "node-1"))
	log.Record(*NewAuditEntry(AuditActionSimulate, "node-1"))
	log.Record(*NewAuditEntry(AuditActionExpand, "node-2"))
	log.Record(*NewAuditEntry(AuditActionBackprop, "node-1"))

	entries := log.EntriesByNode("node-1")
	if len(entries) != 3 {
		t.Errorf("EntriesByNode(node-1) = %d entries, want 3", len(entries))
	}

	entries = log.EntriesByNode("node-99")
	if len(entries) != 0 {
		t.Errorf("EntriesByNode(node-99) = %d entries, want 0", len(entries))
	}
}

func TestAuditLog_Summary(t *testing.T) {
	log := NewAuditLog()

	log.Record(*NewAuditEntry(AuditActionExpand, "node-1"))
	log.Record(*NewAuditEntry(AuditActionSimulate, "node-1"))
	log.Record(*NewAuditEntry(AuditActionExpand, "node-2"))
	log.Record(*NewAuditEntry(AuditActionBackprop, "node-1"))

	summary := log.Summary()

	if summary.TotalEntries != 4 {
		t.Errorf("TotalEntries = %d, want 4", summary.TotalEntries)
	}
	if summary.ActionCounts[AuditActionExpand] != 2 {
		t.Errorf("ActionCounts[expand] = %d, want 2", summary.ActionCounts[AuditActionExpand])
	}
	if summary.ActionCounts[AuditActionSimulate] != 1 {
		t.Errorf("ActionCounts[simulate] = %d, want 1", summary.ActionCounts[AuditActionSimulate])
	}
	if summary.ActionCounts[AuditActionBackprop] != 1 {
		t.Errorf("ActionCounts[backprop] = %d, want 1", summary.ActionCounts[AuditActionBackprop])
	}
	if summary.FirstEntry == 0 {
		t.Error("FirstEntry should be set")
	}
	if summary.LastEntry == 0 {
		t.Error("LastEntry should be set")
	}
}

func TestAuditLog_Summary_Empty(t *testing.T) {
	log := NewAuditLog()

	summary := log.Summary()

	if summary.TotalEntries != 0 {
		t.Errorf("TotalEntries = %d, want 0", summary.TotalEntries)
	}
	if summary.FirstEntry != 0 {
		t.Error("FirstEntry should be zero for empty log")
	}
}

func TestAuditLog_Concurrent(t *testing.T) {
	log := NewAuditLog()

	var wg sync.WaitGroup
	const numGoroutines = 100

	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func(i int) {
			defer wg.Done()
			entry := NewAuditEntry(AuditActionExpand, "node-concurrent").WithScore(float64(i))
			log.Record(*entry)
		}(i)
	}

	wg.Wait()

	if log.Len() != numGoroutines {
		t.Errorf("Len() = %d, want %d", log.Len(), numGoroutines)
	}

	if !log.Verify() {
		t.Error("Verify() should return true for concurrent writes")
	}
}

func TestAuditLog_Record_SetsTimestamp(t *testing.T) {
	log := NewAuditLog()

	// Entry with zero timestamp
	entry := AuditEntry{
		Action: AuditActionExpand,
		NodeID: "node-1",
	}

	log.Record(entry)

	entries := log.Entries()
	if entries[0].Timestamp == 0 {
		t.Error("Record should set timestamp if zero")
	}
}
