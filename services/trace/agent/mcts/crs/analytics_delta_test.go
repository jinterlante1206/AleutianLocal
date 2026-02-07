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
	"testing"
	"time"
)

func TestAnalyticsQueryType_Constants(t *testing.T) {
	tests := []struct {
		qt       AnalyticsQueryType
		expected string
	}{
		{AnalyticsQueryHotspots, "hotspots"},
		{AnalyticsQueryDeadCode, "dead_code"},
		{AnalyticsQueryCycles, "cycles"},
		{AnalyticsQueryPath, "path"},
		{AnalyticsQueryReferences, "references"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if string(tt.qt) != tt.expected {
				t.Errorf("AnalyticsQueryType = %v, want %v", tt.qt, tt.expected)
			}
		})
	}
}

func TestProofKeyConstants(t *testing.T) {
	tests := []struct {
		name string
		key  string
	}{
		{"hotspots_done", ProofKeyAnalyticsHotspotsDone},
		{"hotspots_found", ProofKeyAnalyticsHotspotsFound},
		{"dead_code_done", ProofKeyAnalyticsDeadCodeDone},
		{"dead_code_found", ProofKeyAnalyticsDeadCodeFound},
		{"cycles_done", ProofKeyAnalyticsCyclesDone},
		{"cycles_found", ProofKeyAnalyticsCyclesFound},
		{"path_done", ProofKeyAnalyticsPathDone},
		{"path_found", ProofKeyAnalyticsPathFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.key == "" {
				t.Error("proof key should not be empty")
			}
		})
	}
}

func TestAnalyticsRecord_Creation(t *testing.T) {
	now := time.Now().UnixMilli()

	record := NewAnalyticsRecord(
		AnalyticsQueryHotspots,
		now,
		5,
		100,
	)

	if record.QueryType != AnalyticsQueryHotspots {
		t.Errorf("QueryType = %v, want %v", record.QueryType, AnalyticsQueryHotspots)
	}
	if record.QueryTime != now {
		t.Errorf("QueryTime = %v, want %v", record.QueryTime, now)
	}
	if record.ResultCount != 5 {
		t.Errorf("ResultCount = %d, want 5", record.ResultCount)
	}
	if record.ExecutionMs != 100 {
		t.Errorf("ExecutionMs = %d, want 100", record.ExecutionMs)
	}
	if record.ID == "" {
		t.Error("ID should not be empty")
	}
}

func TestAnalyticsRecord_WithMethods(t *testing.T) {
	record := NewAnalyticsRecord(AnalyticsQueryHotspots, time.Now().UnixMilli(), 5, 100).
		WithResults([]string{"sym1", "sym2"}).
		WithParams(map[string]any{"k": 10})

	if len(record.Results) != 2 {
		t.Errorf("Results = %v, want 2 items", record.Results)
	}
	if record.QueryParams["k"] != 10 {
		t.Errorf("QueryParams[k] = %v, want 10", record.QueryParams["k"])
	}
}

func TestAnalyticsRecord_GetProofKeys(t *testing.T) {
	tests := []struct {
		queryType AnalyticsQueryType
		wantDone  string
		wantFound string
	}{
		{AnalyticsQueryHotspots, ProofKeyAnalyticsHotspotsDone, ProofKeyAnalyticsHotspotsFound},
		{AnalyticsQueryDeadCode, ProofKeyAnalyticsDeadCodeDone, ProofKeyAnalyticsDeadCodeFound},
		{AnalyticsQueryCycles, ProofKeyAnalyticsCyclesDone, ProofKeyAnalyticsCyclesFound},
		{AnalyticsQueryPath, ProofKeyAnalyticsPathDone, ProofKeyAnalyticsPathFound},
	}

	for _, tt := range tests {
		t.Run(string(tt.queryType), func(t *testing.T) {
			record := NewAnalyticsRecord(tt.queryType, time.Now().UnixMilli(), 1, 10)

			if got := record.GetProofDoneKey(); got != tt.wantDone {
				t.Errorf("GetProofDoneKey() = %v, want %v", got, tt.wantDone)
			}
			if got := record.GetProofFoundKey(); got != tt.wantFound {
				t.Errorf("GetProofFoundKey() = %v, want %v", got, tt.wantFound)
			}
		})
	}
}

func TestAnalyticsRecord_HasResults(t *testing.T) {
	tests := []struct {
		name       string
		queryType  AnalyticsQueryType
		setup      func(*AnalyticsRecord)
		wantResult bool
	}{
		{
			name:      "hotspots with results",
			queryType: AnalyticsQueryHotspots,
			setup: func(r *AnalyticsRecord) {
				r.ResultCount = 5
			},
			wantResult: true,
		},
		{
			name:      "hotspots no results",
			queryType: AnalyticsQueryHotspots,
			setup: func(r *AnalyticsRecord) {
				r.ResultCount = 0
			},
			wantResult: false,
		},
		{
			name:      "cycles with results",
			queryType: AnalyticsQueryCycles,
			setup: func(r *AnalyticsRecord) {
				r.Cycles = [][]string{{"a", "b", "a"}}
			},
			wantResult: true,
		},
		{
			name:      "cycles no results",
			queryType: AnalyticsQueryCycles,
			setup: func(r *AnalyticsRecord) {
				r.Cycles = nil
			},
			wantResult: false,
		},
		{
			name:      "path with results",
			queryType: AnalyticsQueryPath,
			setup: func(r *AnalyticsRecord) {
				r.Path = []string{"a", "b", "c"}
			},
			wantResult: true,
		},
		{
			name:      "path no results",
			queryType: AnalyticsQueryPath,
			setup: func(r *AnalyticsRecord) {
				r.Path = nil
			},
			wantResult: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			record := NewAnalyticsRecord(tt.queryType, time.Now().UnixMilli(), 0, 10)
			tt.setup(record)

			if got := record.HasResults(); got != tt.wantResult {
				t.Errorf("HasResults() = %v, want %v", got, tt.wantResult)
			}
		})
	}
}

func TestAnalyticsDelta_Validate(t *testing.T) {
	tests := []struct {
		name      string
		delta     *AnalyticsDelta
		wantError bool
	}{
		{
			name: "valid delta",
			delta: NewAnalyticsDelta(
				SignalSourceHard,
				NewAnalyticsRecord(AnalyticsQueryHotspots, time.Now().UnixMilli(), 5, 100),
			),
			wantError: false,
		},
		{
			name:      "nil record",
			delta:     NewAnalyticsDelta(SignalSourceHard, nil),
			wantError: true,
		},
		{
			name: "empty query type",
			delta: NewAnalyticsDelta(SignalSourceHard, &AnalyticsRecord{
				QueryTime:   time.Now().UnixMilli(),
				ResultCount: 1,
			}),
			wantError: true,
		},
		{
			name: "zero query time",
			delta: NewAnalyticsDelta(SignalSourceHard, &AnalyticsRecord{
				QueryType:   AnalyticsQueryHotspots,
				ResultCount: 1,
			}),
			wantError: true,
		},
		{
			name: "negative result count",
			delta: NewAnalyticsDelta(SignalSourceHard, &AnalyticsRecord{
				QueryType:   AnalyticsQueryHotspots,
				QueryTime:   time.Now().UnixMilli(),
				ResultCount: -1,
			}),
			wantError: true,
		},
		{
			name: "cycles with results but no cycles",
			delta: NewAnalyticsDelta(SignalSourceHard, &AnalyticsRecord{
				QueryType:   AnalyticsQueryCycles,
				QueryTime:   time.Now().UnixMilli(),
				ResultCount: 5,
				Cycles:      nil,
			}),
			wantError: true,
		},
		{
			name: "path with results but no path",
			delta: NewAnalyticsDelta(SignalSourceHard, &AnalyticsRecord{
				QueryType:   AnalyticsQueryPath,
				QueryTime:   time.Now().UnixMilli(),
				ResultCount: 1,
				Path:        nil,
			}),
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.delta.Validate(nil)
			if (err != nil) != tt.wantError {
				t.Errorf("Validate() error = %v, wantError %v", err, tt.wantError)
			}
		})
	}
}

func TestAnalyticsDelta_Type(t *testing.T) {
	delta := NewAnalyticsDelta(
		SignalSourceHard,
		NewAnalyticsRecord(AnalyticsQueryHotspots, time.Now().UnixMilli(), 5, 100),
	)

	if delta.Type() != DeltaTypeAnalytics {
		t.Errorf("Type() = %v, want %v", delta.Type(), DeltaTypeAnalytics)
	}
}

func TestAnalyticsDelta_ConflictsWith(t *testing.T) {
	delta1 := NewAnalyticsDelta(
		SignalSourceHard,
		NewAnalyticsRecord(AnalyticsQueryHotspots, time.Now().UnixMilli(), 5, 100),
	)
	delta2 := NewAnalyticsDelta(
		SignalSourceHard,
		NewAnalyticsRecord(AnalyticsQueryCycles, time.Now().UnixMilli(), 2, 50),
	)

	// Analytics deltas never conflict (append-only)
	if delta1.ConflictsWith(delta2) {
		t.Error("AnalyticsDelta should never conflict")
	}
}

func TestAnalyticsHistory(t *testing.T) {
	t.Run("add and get", func(t *testing.T) {
		h := NewAnalyticsHistory(10)

		record := NewAnalyticsRecord(AnalyticsQueryHotspots, time.Now().UnixMilli(), 5, 100)
		h.Add(record)

		if h.Size() != 1 {
			t.Errorf("Size() = %d, want 1", h.Size())
		}

		got := h.GetLast(AnalyticsQueryHotspots)
		if got == nil {
			t.Fatal("GetLast returned nil")
		}
		if got.QueryType != AnalyticsQueryHotspots {
			t.Errorf("QueryType = %v, want %v", got.QueryType, AnalyticsQueryHotspots)
		}
	})

	t.Run("has run", func(t *testing.T) {
		h := NewAnalyticsHistory(10)

		if h.HasRun(AnalyticsQueryHotspots) {
			t.Error("HasRun should return false for empty history")
		}

		h.Add(NewAnalyticsRecord(AnalyticsQueryHotspots, time.Now().UnixMilli(), 5, 100))

		if !h.HasRun(AnalyticsQueryHotspots) {
			t.Error("HasRun should return true after adding record")
		}
		if h.HasRun(AnalyticsQueryCycles) {
			t.Error("HasRun should return false for unrecorded type")
		}
	})

	t.Run("ring buffer behavior", func(t *testing.T) {
		h := NewAnalyticsHistory(3) // Small buffer for testing

		// Add 5 records
		for i := 0; i < 5; i++ {
			h.Add(NewAnalyticsRecord(
				AnalyticsQueryHotspots,
				time.Now().UnixMilli()+int64(i),
				i,
				int64(i*10),
			))
		}

		if h.Size() != 3 {
			t.Errorf("Size() = %d, want 3", h.Size())
		}

		// All() should return records in chronological order
		records := h.All()
		if len(records) != 3 {
			t.Fatalf("All() returned %d records, want 3", len(records))
		}
	})

	t.Run("nil add is safe", func(t *testing.T) {
		h := NewAnalyticsHistory(10)
		h.Add(nil) // Should not panic

		if h.Size() != 0 {
			t.Errorf("Size() = %d, want 0", h.Size())
		}
	})

	t.Run("clone creates deep copy", func(t *testing.T) {
		h := NewAnalyticsHistory(10)
		h.Add(NewAnalyticsRecord(AnalyticsQueryHotspots, time.Now().UnixMilli(), 5, 100).
			WithResults([]string{"sym1"}))

		cloned := h.clone()

		// Modify original
		h.Add(NewAnalyticsRecord(AnalyticsQueryCycles, time.Now().UnixMilli(), 2, 50))

		// Cloned should not see the new record
		if cloned.Size() != 1 {
			t.Errorf("cloned.Size() = %d, want 1", cloned.Size())
		}
		if cloned.HasRun(AnalyticsQueryCycles) {
			t.Error("cloned should not have cycles record")
		}
	})
}

func TestCRS_ApplyAnalyticsDelta(t *testing.T) {
	ctx := context.Background()
	c := New(nil)

	record := NewAnalyticsRecord(AnalyticsQueryHotspots, time.Now().UnixMilli(), 5, 100).
		WithResults([]string{"sym1", "sym2"})

	delta := NewAnalyticsDelta(SignalSourceHard, record)

	metrics, err := c.Apply(ctx, delta)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	if metrics.DeltaType != DeltaTypeAnalytics {
		t.Errorf("DeltaType = %v, want %v", metrics.DeltaType, DeltaTypeAnalytics)
	}

	// Verify analytics was recorded
	if !c.HasRunAnalytics(AnalyticsQueryHotspots) {
		t.Error("HasRunAnalytics should return true")
	}

	last := c.GetLastAnalytics(AnalyticsQueryHotspots)
	if last == nil {
		t.Fatal("GetLastAnalytics returned nil")
	}
	if last.ResultCount != 5 {
		t.Errorf("ResultCount = %d, want 5", last.ResultCount)
	}

	// Verify proof markers were set
	snap := c.Snapshot()
	proofIdx := snap.ProofIndex()

	doneProof, ok := proofIdx.Get(ProofKeyAnalyticsHotspotsDone)
	if !ok {
		t.Error("done proof should be set")
	}
	if doneProof.Status != ProofStatusProven {
		t.Errorf("done proof status = %v, want %v", doneProof.Status, ProofStatusProven)
	}

	foundProof, ok := proofIdx.Get(ProofKeyAnalyticsHotspotsFound)
	if !ok {
		t.Error("found proof should be set")
	}
	if foundProof.Status != ProofStatusProven {
		t.Errorf("found proof status = %v, want %v", foundProof.Status, ProofStatusProven)
	}
}

func TestCRS_AnalyticsHistory(t *testing.T) {
	ctx := context.Background()
	c := New(nil)

	// Apply multiple analytics
	_, err := c.Apply(ctx, NewAnalyticsDelta(SignalSourceHard,
		NewAnalyticsRecord(AnalyticsQueryHotspots, time.Now().UnixMilli(), 5, 100)))
	if err != nil {
		t.Fatalf("Apply hotspots delta error: %v", err)
	}

	// For cycles, use ResultCount=0 (no cycles found) to pass validation,
	// OR provide actual cycles data. Using 0 is simpler for this test.
	_, err = c.Apply(ctx, NewAnalyticsDelta(SignalSourceHard,
		NewAnalyticsRecord(AnalyticsQueryDeadCode, time.Now().UnixMilli(), 3, 50)))
	if err != nil {
		t.Fatalf("Apply dead_code delta error: %v", err)
	}

	history := c.GetAnalyticsHistory()
	if len(history) != 2 {
		t.Errorf("GetAnalyticsHistory() returned %d records, want 2", len(history))
	}

	// Verify both types are tracked
	if !c.HasRunAnalytics(AnalyticsQueryHotspots) {
		t.Error("HasRunAnalytics(hotspots) should return true")
	}
	if !c.HasRunAnalytics(AnalyticsQueryDeadCode) {
		t.Error("HasRunAnalytics(dead_code) should return true")
	}
}

func TestSnapshot_AnalyticsHistory(t *testing.T) {
	ctx := context.Background()
	c := New(nil)

	// Apply analytics
	_, _ = c.Apply(ctx, NewAnalyticsDelta(SignalSourceHard,
		NewAnalyticsRecord(AnalyticsQueryHotspots, time.Now().UnixMilli(), 5, 100)))

	snap := c.Snapshot()

	// Snapshot should have analytics
	if !snap.HasRunAnalytics(AnalyticsQueryHotspots) {
		t.Error("snapshot.HasRunAnalytics should return true")
	}

	last := snap.LastAnalytics(AnalyticsQueryHotspots)
	if last == nil {
		t.Fatal("snapshot.LastAnalytics returned nil")
	}
	if last.ResultCount != 5 {
		t.Errorf("ResultCount = %d, want 5", last.ResultCount)
	}

	// Analytics added after snapshot should not appear
	// Use DeadCode (no special validation) instead of Cycles (requires Cycles field)
	_, err := c.Apply(ctx, NewAnalyticsDelta(SignalSourceHard,
		NewAnalyticsRecord(AnalyticsQueryDeadCode, time.Now().UnixMilli(), 2, 50)))
	if err != nil {
		t.Fatalf("Apply dead_code delta error: %v", err)
	}

	// Verify it was added to CRS but not visible in snapshot
	if !c.HasRunAnalytics(AnalyticsQueryDeadCode) {
		t.Error("CRS should have dead_code analytics")
	}
	if snap.HasRunAnalytics(AnalyticsQueryDeadCode) {
		t.Error("snapshot should not see analytics added after it was taken")
	}
}

func TestCreateAnalyticsDelta_Helper(t *testing.T) {
	delta := CreateAnalyticsDelta(AnalyticsQueryHotspots, 5, 100)

	if delta.Type() != DeltaTypeAnalytics {
		t.Errorf("Type() = %v, want %v", delta.Type(), DeltaTypeAnalytics)
	}
	if delta.Source() != SignalSourceHard {
		t.Errorf("Source() = %v, want %v", delta.Source(), SignalSourceHard)
	}
	if delta.Record == nil {
		t.Fatal("Record is nil")
	}
	if delta.Record.QueryType != AnalyticsQueryHotspots {
		t.Errorf("QueryType = %v, want %v", delta.Record.QueryType, AnalyticsQueryHotspots)
	}
	if delta.Record.ResultCount != 5 {
		t.Errorf("ResultCount = %d, want 5", delta.Record.ResultCount)
	}
	if delta.Record.ExecutionMs != 100 {
		t.Errorf("ExecutionMs = %d, want 100", delta.Record.ExecutionMs)
	}
}

func TestDeltaType_Analytics_String(t *testing.T) {
	if DeltaTypeAnalytics.String() != "analytics" {
		t.Errorf("DeltaTypeAnalytics.String() = %v, want 'analytics'", DeltaTypeAnalytics.String())
	}
}

func TestAnalyticsHistory_Concurrent(t *testing.T) {
	h := NewAnalyticsHistory(100)
	done := make(chan struct{})

	// Concurrent writes
	for i := 0; i < 10; i++ {
		go func(n int) {
			for j := 0; j < 100; j++ {
				h.Add(NewAnalyticsRecord(
					AnalyticsQueryHotspots,
					time.Now().UnixMilli()+int64(n*1000+j),
					j,
					int64(j*10),
				))
			}
			done <- struct{}{}
		}(i)
	}

	// Concurrent reads
	for i := 0; i < 5; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				_ = h.Size()
				_ = h.All()
				_ = h.HasRun(AnalyticsQueryHotspots)
				_ = h.GetLast(AnalyticsQueryHotspots)
			}
			done <- struct{}{}
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 15; i++ {
		<-done
	}

	// Size should be capped at max
	if h.Size() > 100 {
		t.Errorf("Size() = %d, want <= 100", h.Size())
	}
}

func TestAnalyticsHistory_RingBufferWrap(t *testing.T) {
	// Small buffer to test wrap-around
	h := NewAnalyticsHistory(3)

	// Add 10 records to force wrap-around
	for i := 0; i < 10; i++ {
		h.Add(NewAnalyticsRecord(
			AnalyticsQueryHotspots,
			time.Now().UnixMilli()+int64(i),
			i,
			int64(i*10),
		))
	}

	// Should only have 3 records (the last 3)
	if h.Size() != 3 {
		t.Errorf("Size() = %d, want 3", h.Size())
	}

	all := h.All()
	if len(all) != 3 {
		t.Fatalf("All() returned %d records, want 3", len(all))
	}

	// Should be in chronological order (oldest first)
	// After 10 adds with buffer size 3, we should have records 7, 8, 9
	for i, rec := range all {
		expectedCount := i + 7 // 7, 8, 9
		if rec.ResultCount != expectedCount {
			t.Errorf("Record[%d].ResultCount = %d, want %d", i, rec.ResultCount, expectedCount)
		}
	}
}

func TestAnalyticsDelta_ValidCyclesQuery(t *testing.T) {
	// Create a valid cycles query with actual cycles data
	record := NewAnalyticsRecord(AnalyticsQueryCycles, time.Now().UnixMilli(), 2, 50).
		WithCycles([][]string{{"A", "B", "A"}, {"C", "D", "E", "C"}})

	delta := NewAnalyticsDelta(SignalSourceHard, record)

	if err := delta.Validate(nil); err != nil {
		t.Errorf("Validate() error = %v, want nil", err)
	}

	if !delta.Record.HasResults() {
		t.Error("HasResults() should return true for cycles with data")
	}
}

func TestAnalyticsDelta_ValidPathQuery(t *testing.T) {
	// Create a valid path query with actual path data
	record := NewAnalyticsRecord(AnalyticsQueryPath, time.Now().UnixMilli(), 1, 30).
		WithPath([]string{"main", "handler", "db.Query", "sql.Exec"})

	delta := NewAnalyticsDelta(SignalSourceHard, record)

	if err := delta.Validate(nil); err != nil {
		t.Errorf("Validate() error = %v, want nil", err)
	}

	if !delta.Record.HasResults() {
		t.Error("HasResults() should return true for path with data")
	}
}

func TestAnalyticsDelta_Merge(t *testing.T) {
	delta1 := NewAnalyticsDelta(SignalSourceHard,
		NewAnalyticsRecord(AnalyticsQueryHotspots, time.Now().UnixMilli(), 5, 100))
	delta2 := NewAnalyticsDelta(SignalSourceHard,
		NewAnalyticsRecord(AnalyticsQueryDeadCode, time.Now().UnixMilli(), 3, 50))

	merged, err := delta1.Merge(delta2)
	if err != nil {
		t.Fatalf("Merge() error = %v", err)
	}

	// Should create a composite delta
	composite, ok := merged.(*CompositeDelta)
	if !ok {
		t.Fatalf("Merged result should be *CompositeDelta, got %T", merged)
	}

	if len(composite.Deltas) != 2 {
		t.Errorf("Composite should have 2 deltas, got %d", len(composite.Deltas))
	}
}

func TestAnalyticsRecord_GetProofKeys_CustomType(t *testing.T) {
	// Test custom query type that isn't predefined
	record := &AnalyticsRecord{
		QueryType:   "custom_analysis",
		QueryTime:   time.Now().UnixMilli(),
		ResultCount: 1,
	}

	doneKey := record.GetProofDoneKey()
	if doneKey != "analytics:custom_analysis:done" {
		t.Errorf("GetProofDoneKey() = %v, want analytics:custom_analysis:done", doneKey)
	}

	foundKey := record.GetProofFoundKey()
	if foundKey != "analytics:custom_analysis:found" {
		t.Errorf("GetProofFoundKey() = %v, want analytics:custom_analysis:found", foundKey)
	}
}

func TestCRS_ApplyAnalyticsDelta_WithNilRecord(t *testing.T) {
	ctx := context.Background()
	c := New(nil)

	// Delta with nil record should fail validation
	delta := NewAnalyticsDelta(SignalSourceHard, nil)

	_, err := c.Apply(ctx, delta)
	if err == nil {
		t.Error("Apply() should fail for nil record")
	}
}

func TestCRS_ApplyAnalyticsDelta_NoResults(t *testing.T) {
	ctx := context.Background()
	c := New(nil)

	// Apply analytics with no results (ResultCount = 0)
	record := NewAnalyticsRecord(AnalyticsQueryHotspots, time.Now().UnixMilli(), 0, 100)
	delta := NewAnalyticsDelta(SignalSourceHard, record)

	_, err := c.Apply(ctx, delta)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	// Verify "done" proof is set but "found" proof is NOT set
	snap := c.Snapshot()
	proofIdx := snap.ProofIndex()

	doneProof, ok := proofIdx.Get(ProofKeyAnalyticsHotspotsDone)
	if !ok {
		t.Error("done proof should be set")
	}
	if doneProof.Status != ProofStatusProven {
		t.Errorf("done proof status = %v, want %v", doneProof.Status, ProofStatusProven)
	}

	// found proof should NOT be set since ResultCount = 0
	_, ok = proofIdx.Get(ProofKeyAnalyticsHotspotsFound)
	if ok {
		t.Error("found proof should NOT be set when no results")
	}
}

// -----------------------------------------------------------------------------
// GR-31 Review Fixes Tests
// -----------------------------------------------------------------------------

func TestAnalyticsQueryType_IsValid(t *testing.T) {
	tests := []struct {
		queryType AnalyticsQueryType
		wantValid bool
	}{
		{AnalyticsQueryHotspots, true},
		{AnalyticsQueryDeadCode, true},
		{AnalyticsQueryCycles, true},
		{AnalyticsQueryPath, true},
		{AnalyticsQueryReferences, true},
		{AnalyticsQueryCoupling, true},
		{"unknown_type", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(string(tt.queryType), func(t *testing.T) {
			if got := tt.queryType.IsValid(); got != tt.wantValid {
				t.Errorf("IsValid() = %v, want %v", got, tt.wantValid)
			}
		})
	}
}

func TestAnalyticsQueryCoupling_ProofKeys(t *testing.T) {
	record := NewAnalyticsRecord(AnalyticsQueryCoupling, time.Now().UnixMilli(), 5, 100)

	doneKey := record.GetProofDoneKey()
	if doneKey != ProofKeyAnalyticsCouplingDone {
		t.Errorf("GetProofDoneKey() = %v, want %v", doneKey, ProofKeyAnalyticsCouplingDone)
	}

	foundKey := record.GetProofFoundKey()
	if foundKey != ProofKeyAnalyticsCouplingFound {
		t.Errorf("GetProofFoundKey() = %v, want %v", foundKey, ProofKeyAnalyticsCouplingFound)
	}
}

func TestAnalyticsQueryReferences_ProofKeys(t *testing.T) {
	record := NewAnalyticsRecord(AnalyticsQueryReferences, time.Now().UnixMilli(), 10, 50)

	doneKey := record.GetProofDoneKey()
	if doneKey != ProofKeyAnalyticsReferencesDone {
		t.Errorf("GetProofDoneKey() = %v, want %v", doneKey, ProofKeyAnalyticsReferencesDone)
	}

	foundKey := record.GetProofFoundKey()
	if foundKey != ProofKeyAnalyticsReferencesFound {
		t.Errorf("GetProofFoundKey() = %v, want %v", foundKey, ProofKeyAnalyticsReferencesFound)
	}
}

func TestAnalyticsDelta_Validate_NegativeExecutionMs(t *testing.T) {
	record := NewAnalyticsRecord(AnalyticsQueryHotspots, time.Now().UnixMilli(), 5, -1)
	delta := NewAnalyticsDelta(SignalSourceHard, record)

	err := delta.Validate(nil)
	if err == nil {
		t.Error("Validate() should fail for negative execution_ms")
	}
}

func TestAnalyticsRecord_WithTypedParams(t *testing.T) {
	params := AnalyticsQueryParams{
		Limit:      10,
		FromSymbol: "main.Go",
		ToSymbol:   "db.Query",
	}

	record := NewAnalyticsRecord(AnalyticsQueryPath, time.Now().UnixMilli(), 1, 50).
		WithTypedParams(params)

	if record.Params.Limit != 10 {
		t.Errorf("Params.Limit = %d, want 10", record.Params.Limit)
	}
	if record.Params.FromSymbol != "main.Go" {
		t.Errorf("Params.FromSymbol = %s, want main.Go", record.Params.FromSymbol)
	}
	if record.Params.ToSymbol != "db.Query" {
		t.Errorf("Params.ToSymbol = %s, want db.Query", record.Params.ToSymbol)
	}
}

func TestAnalyticsRecord_WithGraphGeneration(t *testing.T) {
	record := NewAnalyticsRecord(AnalyticsQueryHotspots, time.Now().UnixMilli(), 5, 100).
		WithGraphGeneration(42)

	if record.GraphGeneration != 42 {
		t.Errorf("GraphGeneration = %d, want 42", record.GraphGeneration)
	}
}

func TestAnalyticsRecord_TruncateResults(t *testing.T) {
	// Create record with more than MaxResultsPerRecord
	results := make([]string, MaxResultsPerRecord+20)
	for i := range results {
		results[i] = "sym"
	}

	record := NewAnalyticsRecord(AnalyticsQueryHotspots, time.Now().UnixMilli(), len(results), 100).
		WithResults(results).
		TruncateResults()

	if len(record.Results) != MaxResultsPerRecord {
		t.Errorf("Results length = %d, want %d", len(record.Results), MaxResultsPerRecord)
	}
}

func TestAnalyticsRecord_TruncateResults_BelowLimit(t *testing.T) {
	// Create record below limit - should not truncate
	results := []string{"sym1", "sym2", "sym3"}

	record := NewAnalyticsRecord(AnalyticsQueryHotspots, time.Now().UnixMilli(), 3, 100).
		WithResults(results).
		TruncateResults()

	if len(record.Results) != 3 {
		t.Errorf("Results length = %d, want 3", len(record.Results))
	}
}

func TestAnalyticsRecord_UniqueIDs(t *testing.T) {
	now := time.Now().UnixMilli()

	// Create multiple records at the same timestamp
	record1 := NewAnalyticsRecord(AnalyticsQueryHotspots, now, 5, 100)
	record2 := NewAnalyticsRecord(AnalyticsQueryHotspots, now, 5, 100)
	record3 := NewAnalyticsRecord(AnalyticsQueryHotspots, now, 5, 100)

	if record1.ID == record2.ID {
		t.Errorf("record1.ID and record2.ID should be unique, both are %s", record1.ID)
	}
	if record2.ID == record3.ID {
		t.Errorf("record2.ID and record3.ID should be unique, both are %s", record2.ID)
	}
}

func TestAnalyticsDelta_IndexesAffected(t *testing.T) {
	delta := NewAnalyticsDelta(SignalSourceHard,
		NewAnalyticsRecord(AnalyticsQueryHotspots, time.Now().UnixMilli(), 5, 100))

	indexes := delta.IndexesAffected()

	// Should include both analytics and proof
	hasAnalytics := false
	hasProof := false
	for _, idx := range indexes {
		if idx == "analytics" {
			hasAnalytics = true
		}
		if idx == "proof" {
			hasProof = true
		}
	}

	if !hasAnalytics {
		t.Error("IndexesAffected should include 'analytics'")
	}
	if !hasProof {
		t.Error("IndexesAffected should include 'proof'")
	}
}

func TestCRS_ApplyAnalyticsDelta_Coupling(t *testing.T) {
	ctx := context.Background()
	c := New(nil)

	record := NewAnalyticsRecord(AnalyticsQueryCoupling, time.Now().UnixMilli(), 3, 75).
		WithTypedParams(AnalyticsQueryParams{PackageName: "github.com/test/pkg"})

	delta := NewAnalyticsDelta(SignalSourceHard, record)

	_, err := c.Apply(ctx, delta)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	// Verify proof markers
	snap := c.Snapshot()
	proofIdx := snap.ProofIndex()

	doneProof, ok := proofIdx.Get(ProofKeyAnalyticsCouplingDone)
	if !ok {
		t.Error("coupling done proof should be set")
	}
	if doneProof.Status != ProofStatusProven {
		t.Errorf("done proof status = %v, want %v", doneProof.Status, ProofStatusProven)
	}

	foundProof, ok := proofIdx.Get(ProofKeyAnalyticsCouplingFound)
	if !ok {
		t.Error("coupling found proof should be set")
	}
	if foundProof.Status != ProofStatusProven {
		t.Errorf("found proof status = %v, want %v", foundProof.Status, ProofStatusProven)
	}
}

func TestCRS_ApplyAnalyticsDelta_TruncatesLargeResults(t *testing.T) {
	ctx := context.Background()
	c := New(nil)

	// Create record with many results
	results := make([]string, MaxResultsPerRecord+50)
	for i := range results {
		results[i] = "sym"
	}

	record := NewAnalyticsRecord(AnalyticsQueryHotspots, time.Now().UnixMilli(), len(results), 100).
		WithResults(results)

	delta := NewAnalyticsDelta(SignalSourceHard, record)

	_, err := c.Apply(ctx, delta)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	// Verify results were truncated
	last := c.GetLastAnalytics(AnalyticsQueryHotspots)
	if last == nil {
		t.Fatal("GetLastAnalytics returned nil")
	}
	if len(last.Results) != MaxResultsPerRecord {
		t.Errorf("Results should be truncated to %d, got %d", MaxResultsPerRecord, len(last.Results))
	}
}
