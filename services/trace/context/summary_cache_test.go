// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package context

import (
	"sync"
	"testing"
	"time"
)

func TestSummaryCache_GetSet(t *testing.T) {
	cache := NewSummaryCache(DefaultCacheConfig())

	summary := &Summary{
		ID:        "pkg/auth",
		Level:     1,
		Content:   "Auth package handles authentication",
		Keywords:  []string{"auth", "jwt"},
		CreatedAt: time.Now().UnixMilli(),
		UpdatedAt: time.Now().UnixMilli(),
	}

	// Set
	if err := cache.Set(summary); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	// Get
	got, ok := cache.Get(summary.ID)
	if !ok {
		t.Fatal("Get returned false for existing entry")
	}
	if got.ID != summary.ID {
		t.Errorf("Get returned wrong ID: got %q, want %q", got.ID, summary.ID)
	}
	if got.Content != summary.Content {
		t.Errorf("Get returned wrong Content")
	}
}

func TestSummaryCache_GetMiss(t *testing.T) {
	cache := NewSummaryCache(DefaultCacheConfig())

	_, ok := cache.Get("nonexistent")
	if ok {
		t.Error("Get returned true for nonexistent entry")
	}
}

func TestSummaryCache_Invalidate(t *testing.T) {
	cache := NewSummaryCache(DefaultCacheConfig())

	summary := &Summary{
		ID:        "pkg/auth",
		Level:     1,
		Content:   "Auth package",
		UpdatedAt: time.Now().UnixMilli(),
	}

	cache.Set(summary)

	// Should be able to get it
	_, ok := cache.Get(summary.ID)
	if !ok {
		t.Fatal("Get failed before invalidate")
	}

	// Invalidate
	cache.Invalidate(summary.ID)

	// Fresh get should fail
	_, ok = cache.Get(summary.ID)
	if ok {
		t.Error("Get returned true after invalidate")
	}
}

func TestSummaryCache_GetStale(t *testing.T) {
	config := DefaultCacheConfig()
	config.StaleReadEnabled = true
	config.FreshTTL = 1 * time.Millisecond
	cache := NewSummaryCache(config)

	summary := &Summary{
		ID:        "pkg/auth",
		Level:     1,
		Content:   "Auth package",
		UpdatedAt: time.Now().UnixMilli(),
	}

	cache.Set(summary)

	// Wait for it to become stale
	time.Sleep(10 * time.Millisecond)

	// Fresh get should fail
	_, ok := cache.Get(summary.ID)
	if ok {
		t.Error("Get returned true for stale entry")
	}

	// Stale get should succeed
	got, ok, isStale := cache.GetStale(summary.ID)
	if !ok {
		t.Fatal("GetStale returned false")
	}
	if !isStale {
		t.Error("GetStale didn't report entry as stale")
	}
	if got.ID != summary.ID {
		t.Error("GetStale returned wrong entry")
	}
}

func TestSummaryCache_InvalidateIfStale(t *testing.T) {
	cache := NewSummaryCache(DefaultCacheConfig())

	summary := &Summary{
		ID:        "pkg/auth",
		Level:     1,
		Content:   "Auth package",
		Hash:      "hash1",
		UpdatedAt: time.Now().UnixMilli(),
	}

	cache.Set(summary)

	// Same hash - shouldn't invalidate
	invalidated := cache.InvalidateIfStale(summary.ID, "hash1")
	if invalidated {
		t.Error("InvalidateIfStale returned true for same hash")
	}

	// Different hash - should invalidate
	invalidated = cache.InvalidateIfStale(summary.ID, "hash2")
	if !invalidated {
		t.Error("InvalidateIfStale returned false for different hash")
	}
}

func TestSummaryCache_SetIfUnchanged(t *testing.T) {
	cache := NewSummaryCache(DefaultCacheConfig())

	summary := &Summary{
		ID:        "pkg/auth",
		Level:     1,
		Content:   "Auth package v1",
		Version:   1,
		UpdatedAt: time.Now().UnixMilli(),
	}

	// Initial set should succeed
	ok, err := cache.SetIfUnchanged(summary, 0)
	if err != nil {
		t.Fatalf("SetIfUnchanged failed: %v", err)
	}
	if !ok {
		t.Error("SetIfUnchanged returned false for initial set")
	}

	// Update with correct version
	summary2 := &Summary{
		ID:        "pkg/auth",
		Level:     1,
		Content:   "Auth package v2",
		Version:   2,
		UpdatedAt: time.Now().UnixMilli(),
	}
	ok, err = cache.SetIfUnchanged(summary2, 1)
	if err != nil {
		t.Fatalf("SetIfUnchanged failed: %v", err)
	}
	if !ok {
		t.Error("SetIfUnchanged returned false for correct version")
	}

	// Update with wrong version should fail
	summary3 := &Summary{
		ID:        "pkg/auth",
		Level:     1,
		Content:   "Auth package v3",
		Version:   3,
		UpdatedAt: time.Now().UnixMilli(),
	}
	_, err = cache.SetIfUnchanged(summary3, 1) // Wrong version
	if err != ErrCacheVersionConflict {
		t.Errorf("SetIfUnchanged expected ErrCacheVersionConflict, got %v", err)
	}
}

func TestSummaryCache_ApplyBatch(t *testing.T) {
	cache := NewSummaryCache(DefaultCacheConfig())

	// Add initial entries
	cache.Set(&Summary{ID: "pkg/old", Level: 1, Content: "Old package", UpdatedAt: time.Now().UnixMilli()})

	batch := &SummaryBatch{
		Version: 1,
		Summaries: []Summary{
			{ID: "pkg/auth", Level: 1, Content: "Auth package", UpdatedAt: time.Now().UnixMilli()},
			{ID: "pkg/db", Level: 1, Content: "DB package", UpdatedAt: time.Now().UnixMilli()},
		},
		DeleteIDs: []string{"pkg/old"},
	}
	batch.SetChecksum()

	err := cache.ApplyBatch(batch)
	if err != nil {
		t.Fatalf("ApplyBatch failed: %v", err)
	}

	// New entries should exist
	if _, ok := cache.Get("pkg/auth"); !ok {
		t.Error("pkg/auth not found after batch")
	}
	if _, ok := cache.Get("pkg/db"); !ok {
		t.Error("pkg/db not found after batch")
	}

	// Deleted entry should not exist
	if cache.Has("pkg/old") {
		t.Error("pkg/old still exists after batch delete")
	}
}

func TestSummaryCache_ApplyBatch_InvalidChecksum(t *testing.T) {
	cache := NewSummaryCache(DefaultCacheConfig())

	batch := &SummaryBatch{
		Version: 1,
		Summaries: []Summary{
			{ID: "pkg/auth", Level: 1, Content: "Auth package", UpdatedAt: time.Now().UnixMilli()},
		},
		Checksum: "invalid",
	}

	err := cache.ApplyBatch(batch)
	if err != ErrBatchValidationFailed {
		t.Errorf("ApplyBatch expected ErrBatchValidationFailed, got %v", err)
	}
}

func TestSummaryCache_GetByLevel(t *testing.T) {
	cache := NewSummaryCache(DefaultCacheConfig())

	now := time.Now().UnixMilli()
	cache.Set(&Summary{ID: "pkg/auth", Level: 1, Content: "Auth", UpdatedAt: now})
	cache.Set(&Summary{ID: "pkg/db", Level: 1, Content: "DB", UpdatedAt: now})
	cache.Set(&Summary{ID: "pkg/auth/validator.go", Level: 2, Content: "Validator", UpdatedAt: now})

	// Get level 1
	pkgs := cache.GetByLevel(1)
	if len(pkgs) != 2 {
		t.Errorf("GetByLevel(1) returned %d entries, want 2", len(pkgs))
	}

	// Get level 2
	files := cache.GetByLevel(2)
	if len(files) != 1 {
		t.Errorf("GetByLevel(2) returned %d entries, want 1", len(files))
	}

	// Get level 3 (empty)
	funcs := cache.GetByLevel(3)
	if len(funcs) != 0 {
		t.Errorf("GetByLevel(3) returned %d entries, want 0", len(funcs))
	}
}

func TestSummaryCache_GetChildren(t *testing.T) {
	cache := NewSummaryCache(DefaultCacheConfig())

	now := time.Now().UnixMilli()
	cache.Set(&Summary{ID: "pkg/auth", Level: 1, Content: "Auth", UpdatedAt: now})
	cache.Set(&Summary{ID: "pkg/auth/validator.go", Level: 2, ParentID: "pkg/auth", Content: "Validator", UpdatedAt: now})
	cache.Set(&Summary{ID: "pkg/auth/handler.go", Level: 2, ParentID: "pkg/auth", Content: "Handler", UpdatedAt: now})
	cache.Set(&Summary{ID: "pkg/db/conn.go", Level: 2, ParentID: "pkg/db", Content: "Conn", UpdatedAt: now})

	children := cache.GetChildren("pkg/auth")
	if len(children) != 2 {
		t.Errorf("GetChildren(pkg/auth) returned %d entries, want 2", len(children))
	}
}

func TestSummaryCache_Delete(t *testing.T) {
	cache := NewSummaryCache(DefaultCacheConfig())

	cache.Set(&Summary{ID: "pkg/auth", Level: 1, Content: "Auth", UpdatedAt: time.Now().UnixMilli()})

	if !cache.Has("pkg/auth") {
		t.Fatal("Entry not found after Set")
	}

	cache.Delete("pkg/auth")

	if cache.Has("pkg/auth") {
		t.Error("Entry still exists after Delete")
	}
}

func TestSummaryCache_Clear(t *testing.T) {
	cache := NewSummaryCache(DefaultCacheConfig())

	cache.Set(&Summary{ID: "pkg/auth", Level: 1, Content: "Auth", UpdatedAt: time.Now().UnixMilli()})
	cache.Set(&Summary{ID: "pkg/db", Level: 1, Content: "DB", UpdatedAt: time.Now().UnixMilli()})

	if cache.Count() != 2 {
		t.Fatalf("expected 2 entries, got %d", cache.Count())
	}

	cache.Clear()

	if cache.Count() != 0 {
		t.Errorf("expected 0 entries after Clear, got %d", cache.Count())
	}
}

func TestSummaryCache_Stats(t *testing.T) {
	cache := NewSummaryCache(DefaultCacheConfig())

	cache.Set(&Summary{ID: "pkg/auth", Level: 1, Content: "Auth", UpdatedAt: time.Now().UnixMilli()})

	// Hit
	cache.Get("pkg/auth")

	// Miss
	cache.Get("nonexistent")

	stats := cache.Stats()

	if stats.Entries != 1 {
		t.Errorf("expected 1 entry, got %d", stats.Entries)
	}
	if stats.Hits != 1 {
		t.Errorf("expected 1 hit, got %d", stats.Hits)
	}
	if stats.Misses != 1 {
		t.Errorf("expected 1 miss, got %d", stats.Misses)
	}
}

func TestSummaryCache_Eviction(t *testing.T) {
	config := CacheConfig{
		MaxEntries:       2,
		FreshTTL:         1 * time.Hour,
		StaleReadEnabled: false,
	}
	cache := NewSummaryCache(config)

	now := time.Now().UnixMilli()

	// Add 3 entries (exceeds max)
	cache.Set(&Summary{ID: "pkg/a", Level: 1, Content: "A", UpdatedAt: now})
	time.Sleep(1 * time.Millisecond)
	cache.Set(&Summary{ID: "pkg/b", Level: 1, Content: "B", UpdatedAt: now})
	time.Sleep(1 * time.Millisecond)
	cache.Set(&Summary{ID: "pkg/c", Level: 1, Content: "C", UpdatedAt: now})

	// Should have evicted oldest
	if cache.Count() != 2 {
		t.Errorf("expected 2 entries after eviction, got %d", cache.Count())
	}

	// pkg/a should be evicted (oldest)
	if cache.Has("pkg/a") {
		t.Error("pkg/a should have been evicted")
	}

	// Newer entries should exist
	if !cache.Has("pkg/b") {
		t.Error("pkg/b should exist")
	}
	if !cache.Has("pkg/c") {
		t.Error("pkg/c should exist")
	}
}

func TestSummaryCache_ConcurrentAccess(t *testing.T) {
	cache := NewSummaryCache(DefaultCacheConfig())

	var wg sync.WaitGroup
	iterations := 100

	// Concurrent writes
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			cache.Set(&Summary{
				ID:        "pkg/auth",
				Level:     1,
				Content:   "Auth",
				UpdatedAt: time.Now().UnixMilli(),
			})
		}
	}()

	// Concurrent reads
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			cache.Get("pkg/auth")
			cache.GetStale("pkg/auth")
		}
	}()

	// Concurrent invalidates
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			cache.Invalidate("pkg/auth")
		}
	}()

	wg.Wait()
	// Should not panic
}

func TestCacheStats_HitRate(t *testing.T) {
	tests := []struct {
		hits   int64
		misses int64
		want   float64
	}{
		{10, 0, 1.0},
		{0, 10, 0.0},
		{5, 5, 0.5},
		{0, 0, 0.0},
	}

	for _, tt := range tests {
		stats := CacheStats{Hits: tt.hits, Misses: tt.misses}
		if got := stats.HitRate(); got != tt.want {
			t.Errorf("HitRate(hits=%d, misses=%d) = %f, want %f",
				tt.hits, tt.misses, got, tt.want)
		}
	}
}
