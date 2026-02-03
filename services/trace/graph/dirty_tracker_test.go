// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.

package graph

import (
	"sync"
	"testing"
	"time"
)

func TestDirtyTracker_NewDirtyTracker(t *testing.T) {
	tracker := NewDirtyTracker()

	if tracker == nil {
		t.Fatal("NewDirtyTracker returned nil")
	}
	if tracker.HasDirty() {
		t.Error("New tracker should not have dirty files")
	}
	if !tracker.IsEnabled() {
		t.Error("New tracker should be enabled by default")
	}
	if tracker.Count() != 0 {
		t.Error("New tracker should have count 0")
	}
}

func TestDirtyTracker_MarkDirty(t *testing.T) {
	tracker := NewDirtyTracker()

	tracker.MarkDirty("foo.go")

	if !tracker.HasDirty() {
		t.Error("Expected HasDirty() == true after MarkDirty")
	}
	if tracker.Count() != 1 {
		t.Errorf("Expected count 1, got %d", tracker.Count())
	}

	files := tracker.GetDirtyFiles()
	if len(files) != 1 {
		t.Errorf("Expected 1 file, got %d", len(files))
	}
	if files[0] != "foo.go" {
		t.Errorf("Expected 'foo.go', got %q", files[0])
	}
}

func TestDirtyTracker_MarkDirtyMultiple(t *testing.T) {
	tracker := NewDirtyTracker()

	tracker.MarkDirty("foo.go")
	tracker.MarkDirty("bar.go")
	tracker.MarkDirty("baz.go")

	if tracker.Count() != 3 {
		t.Errorf("Expected count 3, got %d", tracker.Count())
	}

	files := tracker.GetDirtyFiles()
	if len(files) != 3 {
		t.Errorf("Expected 3 files, got %d", len(files))
	}

	// Verify all files are present
	fileSet := make(map[string]bool)
	for _, f := range files {
		fileSet[f] = true
	}
	for _, expected := range []string{"foo.go", "bar.go", "baz.go"} {
		if !fileSet[expected] {
			t.Errorf("Missing expected file %q", expected)
		}
	}
}

func TestDirtyTracker_MarkDirtyDuplicate(t *testing.T) {
	tracker := NewDirtyTracker()

	tracker.MarkDirty("foo.go")
	tracker.MarkDirty("foo.go") // Duplicate

	if tracker.Count() != 1 {
		t.Errorf("Expected count 1 (deduped), got %d", tracker.Count())
	}
}

func TestDirtyTracker_MarkDirtyWithSource(t *testing.T) {
	tracker := NewDirtyTracker()

	tracker.MarkDirtyWithSource("foo.go", "agent")
	tracker.MarkDirtyWithSource("bar.go", "watcher")

	entries := tracker.GetDirtyEntries()
	if len(entries) != 2 {
		t.Fatalf("Expected 2 entries, got %d", len(entries))
	}

	entryMap := make(map[string]DirtyEntry)
	for _, e := range entries {
		entryMap[e.Path] = e
	}

	if entryMap["foo.go"].Source != "agent" {
		t.Errorf("foo.go source = %q, want 'agent'", entryMap["foo.go"].Source)
	}
	if entryMap["bar.go"].Source != "watcher" {
		t.Errorf("bar.go source = %q, want 'watcher'", entryMap["bar.go"].Source)
	}
}

func TestDirtyTracker_GetDirtyFilesDoesNotClear(t *testing.T) {
	tracker := NewDirtyTracker()

	tracker.MarkDirty("foo.go")
	tracker.MarkDirty("bar.go")

	// First call
	files1 := tracker.GetDirtyFiles()
	if len(files1) != 2 {
		t.Errorf("First call: expected 2 files, got %d", len(files1))
	}

	// Second call should return same files (NOT cleared)
	files2 := tracker.GetDirtyFiles()
	if len(files2) != 2 {
		t.Errorf("Second call: expected 2 files, got %d", len(files2))
	}

	// HasDirty should still be true
	if !tracker.HasDirty() {
		t.Error("HasDirty should still be true after GetDirtyFiles")
	}
}

func TestDirtyTracker_Clear(t *testing.T) {
	tracker := NewDirtyTracker()

	tracker.MarkDirty("foo.go")
	tracker.MarkDirty("bar.go")
	tracker.MarkDirty("baz.go")

	// Clear only two files
	cleared := tracker.Clear([]string{"foo.go", "bar.go"})

	if cleared != 2 {
		t.Errorf("Expected 2 cleared, got %d", cleared)
	}
	if tracker.Count() != 1 {
		t.Errorf("Expected 1 remaining, got %d", tracker.Count())
	}

	files := tracker.GetDirtyFiles()
	if len(files) != 1 || files[0] != "baz.go" {
		t.Errorf("Expected only 'baz.go', got %v", files)
	}
}

func TestDirtyTracker_ClearNonexistent(t *testing.T) {
	tracker := NewDirtyTracker()

	tracker.MarkDirty("foo.go")

	// Clear file that doesn't exist
	cleared := tracker.Clear([]string{"nonexistent.go"})

	if cleared != 0 {
		t.Errorf("Expected 0 cleared, got %d", cleared)
	}
	if tracker.Count() != 1 {
		t.Errorf("Count should still be 1, got %d", tracker.Count())
	}
}

func TestDirtyTracker_ClearAll(t *testing.T) {
	tracker := NewDirtyTracker()

	tracker.MarkDirty("foo.go")
	tracker.MarkDirty("bar.go")
	tracker.MarkDirty("baz.go")

	cleared := tracker.ClearAll()

	if cleared != 3 {
		t.Errorf("Expected 3 cleared, got %d", cleared)
	}
	if tracker.HasDirty() {
		t.Error("Expected HasDirty() == false after ClearAll")
	}
	if tracker.Count() != 0 {
		t.Errorf("Expected count 0, got %d", tracker.Count())
	}
}

func TestDirtyTracker_Disable(t *testing.T) {
	tracker := NewDirtyTracker()

	tracker.Disable()

	if tracker.IsEnabled() {
		t.Error("Expected IsEnabled() == false after Disable")
	}

	// MarkDirty should be a no-op when disabled
	tracker.MarkDirty("foo.go")

	if tracker.HasDirty() {
		t.Error("MarkDirty should not work when disabled")
	}
	if tracker.Count() != 0 {
		t.Errorf("Count should be 0 when disabled, got %d", tracker.Count())
	}
}

func TestDirtyTracker_EnableAfterDisable(t *testing.T) {
	tracker := NewDirtyTracker()

	tracker.Disable()
	tracker.MarkDirty("foo.go") // Should be ignored

	tracker.Enable()
	tracker.MarkDirty("bar.go") // Should work

	if tracker.Count() != 1 {
		t.Errorf("Expected 1 file (only bar.go), got %d", tracker.Count())
	}

	files := tracker.GetDirtyFiles()
	if len(files) != 1 || files[0] != "bar.go" {
		t.Errorf("Expected only 'bar.go', got %v", files)
	}
}

func TestDirtyTracker_MarkDirtyFromWatcher(t *testing.T) {
	tracker := NewDirtyTracker()

	// Write event should mark dirty
	tracker.MarkDirtyFromWatcher(FileChange{
		Path: "foo.go",
		Op:   FileOpWrite,
		Time: time.Now(),
	})

	if tracker.Count() != 1 {
		t.Errorf("Expected 1 dirty file, got %d", tracker.Count())
	}

	// Remove event should be skipped
	tracker.MarkDirtyFromWatcher(FileChange{
		Path: "bar.go",
		Op:   FileOpRemove,
		Time: time.Now(),
	})

	if tracker.Count() != 1 {
		t.Errorf("Remove should be skipped, still expect 1, got %d", tracker.Count())
	}

	// Create event should mark dirty
	tracker.MarkDirtyFromWatcher(FileChange{
		Path: "baz.go",
		Op:   FileOpCreate,
		Time: time.Now(),
	})

	if tracker.Count() != 2 {
		t.Errorf("Expected 2 dirty files, got %d", tracker.Count())
	}

	// Verify source is "watcher"
	entries := tracker.GetDirtyEntries()
	for _, e := range entries {
		if e.Source != "watcher" {
			t.Errorf("Expected source 'watcher', got %q for %s", e.Source, e.Path)
		}
	}
}

func TestDirtyTracker_ConcurrentAccess(t *testing.T) {
	tracker := NewDirtyTracker()

	var wg sync.WaitGroup
	numGoroutines := 100
	filesPerGoroutine := 10

	// Concurrent writers
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < filesPerGoroutine; j++ {
				tracker.MarkDirty(string(rune('a'+(id+j)%26)) + ".go")
			}
		}(i)
	}

	// Concurrent readers
	for i := 0; i < numGoroutines/2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < filesPerGoroutine; j++ {
				_ = tracker.HasDirty()
				_ = tracker.GetDirtyFiles()
				_ = tracker.Count()
			}
		}()
	}

	wg.Wait()

	// Verify no data race occurred (test passes if no race detector errors)
	if tracker.Count() == 0 {
		t.Error("Expected some dirty files after concurrent writes")
	}
}

func TestDirtyTracker_EntryTimestamp(t *testing.T) {
	tracker := NewDirtyTracker()

	before := time.Now()
	tracker.MarkDirty("foo.go")
	after := time.Now()

	entries := tracker.GetDirtyEntries()
	if len(entries) != 1 {
		t.Fatalf("Expected 1 entry, got %d", len(entries))
	}

	entry := entries[0]
	if entry.MarkedAt.Before(before) || entry.MarkedAt.After(after) {
		t.Errorf("MarkedAt %v not between %v and %v", entry.MarkedAt, before, after)
	}
}

func TestDirtyTracker_EmptyGetDirtyFiles(t *testing.T) {
	tracker := NewDirtyTracker()

	files := tracker.GetDirtyFiles()

	// Should return empty slice, not nil
	if files == nil {
		t.Error("GetDirtyFiles should return empty slice, not nil")
	}
	if len(files) != 0 {
		t.Errorf("Expected 0 files, got %d", len(files))
	}
}

func TestDirtyTracker_EmptyGetDirtyEntries(t *testing.T) {
	tracker := NewDirtyTracker()

	entries := tracker.GetDirtyEntries()

	// Should return empty slice, not nil
	if entries == nil {
		t.Error("GetDirtyEntries should return empty slice, not nil")
	}
	if len(entries) != 0 {
		t.Errorf("Expected 0 entries, got %d", len(entries))
	}
}
