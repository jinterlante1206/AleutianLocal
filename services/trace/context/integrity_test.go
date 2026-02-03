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
	"context"
	"testing"
	"time"
)

func TestIntegrityChecker_Validate_Valid(t *testing.T) {
	cache := NewSummaryCache(DefaultCacheConfig())
	h := &GoHierarchy{}
	checker := NewIntegrityChecker(cache, h)

	now := time.Now()

	// Add a valid hierarchy: project -> package -> file
	cache.Set(&Summary{ID: "", Level: 0, Content: "Project", UpdatedAt: now, Children: []string{"pkg/auth"}})
	cache.Set(&Summary{ID: "pkg/auth", Level: 1, Content: "Auth package", ParentID: "", UpdatedAt: now, Children: []string{"pkg/auth/handler.go"}})
	cache.Set(&Summary{ID: "pkg/auth/handler.go", Level: 2, Content: "Handler file", ParentID: "pkg/auth", UpdatedAt: now})

	ctx := context.Background()
	report, err := checker.Validate(ctx)

	if err != nil {
		t.Fatalf("Validate error: %v", err)
	}
	if !report.Valid {
		t.Errorf("expected valid, got issues: orphans=%v, missing=%v, levels=%v",
			report.OrphanedChildren, report.MissingChildren, report.LevelMismatches)
	}
	if report.TotalChecked != 3 {
		t.Errorf("TotalChecked = %d, want 3", report.TotalChecked)
	}
}

func TestIntegrityChecker_Validate_OrphanedChild(t *testing.T) {
	cache := NewSummaryCache(DefaultCacheConfig())
	h := &GoHierarchy{}
	checker := NewIntegrityChecker(cache, h)

	now := time.Now()

	// Add child without parent
	cache.Set(&Summary{ID: "pkg/auth/handler.go", Level: 2, Content: "Handler file", ParentID: "pkg/auth", UpdatedAt: now})
	// Note: pkg/auth doesn't exist

	ctx := context.Background()
	report, err := checker.Validate(ctx)

	if err != nil {
		t.Fatalf("Validate error: %v", err)
	}
	if report.Valid {
		t.Error("expected invalid for orphaned child")
	}
	if len(report.OrphanedChildren) != 1 {
		t.Errorf("OrphanedChildren = %d, want 1", len(report.OrphanedChildren))
	}
	if report.OrphanedChildren[0] != "pkg/auth/handler.go" {
		t.Errorf("OrphanedChildren[0] = %q, want pkg/auth/handler.go", report.OrphanedChildren[0])
	}
}

func TestIntegrityChecker_Validate_MissingChild(t *testing.T) {
	cache := NewSummaryCache(DefaultCacheConfig())
	h := &GoHierarchy{}
	checker := NewIntegrityChecker(cache, h)

	now := time.Now()

	// Add parent referencing non-existent child
	cache.Set(&Summary{ID: "pkg/auth", Level: 1, Content: "Auth package", ParentID: "", UpdatedAt: now, Children: []string{"pkg/auth/nonexistent.go"}})

	ctx := context.Background()
	report, err := checker.Validate(ctx)

	if err != nil {
		t.Fatalf("Validate error: %v", err)
	}
	if report.Valid {
		t.Error("expected invalid for missing child")
	}
	if len(report.MissingChildren) != 1 {
		t.Errorf("MissingChildren = %d, want 1", len(report.MissingChildren))
	}
	if report.MissingChildren[0].ParentID != "pkg/auth" {
		t.Errorf("MissingChildren[0].ParentID = %q, want pkg/auth", report.MissingChildren[0].ParentID)
	}
	if report.MissingChildren[0].ChildID != "pkg/auth/nonexistent.go" {
		t.Errorf("MissingChildren[0].ChildID = %q", report.MissingChildren[0].ChildID)
	}
}

func TestIntegrityChecker_Validate_LevelMismatch(t *testing.T) {
	cache := NewSummaryCache(DefaultCacheConfig())
	h := &GoHierarchy{}
	checker := NewIntegrityChecker(cache, h)

	now := time.Now()

	// Add summary with wrong level
	cache.Set(&Summary{ID: "pkg/auth/handler.go", Level: 1, Content: "Handler file", ParentID: "", UpdatedAt: now}) // Should be level 2

	ctx := context.Background()
	report, err := checker.Validate(ctx)

	if err != nil {
		t.Fatalf("Validate error: %v", err)
	}
	if report.Valid {
		t.Error("expected invalid for level mismatch")
	}
	if len(report.LevelMismatches) != 1 {
		t.Errorf("LevelMismatches = %d, want 1", len(report.LevelMismatches))
	}
	if report.LevelMismatches[0].ExpectedLevel != 2 {
		t.Errorf("ExpectedLevel = %d, want 2", report.LevelMismatches[0].ExpectedLevel)
	}
	if report.LevelMismatches[0].ActualLevel != 1 {
		t.Errorf("ActualLevel = %d, want 1", report.LevelMismatches[0].ActualLevel)
	}
}

func TestIntegrityChecker_ValidateWithHashes_StaleEntry(t *testing.T) {
	cache := NewSummaryCache(DefaultCacheConfig())
	h := &GoHierarchy{}
	checker := NewIntegrityChecker(cache, h)

	now := time.Now()

	cache.Set(&Summary{ID: "pkg/auth", Level: 1, Content: "Auth package", Hash: "oldhash", ParentID: "", UpdatedAt: now})

	hashProvider := func(entityID string) (string, error) {
		return "newhash", nil // Different from stored
	}

	ctx := context.Background()
	report, err := checker.ValidateWithHashes(ctx, hashProvider)

	if err != nil {
		t.Fatalf("ValidateWithHashes error: %v", err)
	}
	if report.Valid {
		t.Error("expected invalid for stale entry")
	}
	if len(report.StaleEntries) != 1 {
		t.Errorf("StaleEntries = %d, want 1", len(report.StaleEntries))
	}
	if report.StaleEntries[0].StoredHash != "oldhash" {
		t.Errorf("StoredHash = %q, want oldhash", report.StaleEntries[0].StoredHash)
	}
	if report.StaleEntries[0].CurrentHash != "newhash" {
		t.Errorf("CurrentHash = %q, want newhash", report.StaleEntries[0].CurrentHash)
	}
}

func TestIntegrityChecker_Validate_ContextCancellation(t *testing.T) {
	cache := NewSummaryCache(DefaultCacheConfig())
	h := &GoHierarchy{}
	checker := NewIntegrityChecker(cache, h)

	now := time.Now()

	// Add many entries
	for i := 0; i < 100; i++ {
		cache.Set(&Summary{ID: "pkg/test", Level: 1, Content: "Test", UpdatedAt: now})
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := checker.Validate(ctx)

	if err != context.Canceled {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestIntegrityChecker_Repair_OrphansRemoved(t *testing.T) {
	cache := NewSummaryCache(DefaultCacheConfig())
	h := &GoHierarchy{}
	checker := NewIntegrityChecker(cache, h)

	now := time.Now()

	// Add orphaned child
	cache.Set(&Summary{ID: "pkg/auth/handler.go", Level: 2, Content: "Handler file", ParentID: "pkg/auth", UpdatedAt: now})

	ctx := context.Background()
	report, _ := checker.Validate(ctx)

	result, err := checker.Repair(ctx, report)

	if err != nil {
		t.Fatalf("Repair error: %v", err)
	}
	if result.OrphansRemoved != 1 {
		t.Errorf("OrphansRemoved = %d, want 1", result.OrphansRemoved)
	}

	// Should be removed from cache
	if cache.Has("pkg/auth/handler.go") {
		t.Error("orphan should have been removed from cache")
	}
}

func TestIntegrityChecker_Repair_StaleInvalidated(t *testing.T) {
	cache := NewSummaryCache(DefaultCacheConfig())
	h := &GoHierarchy{}
	checker := NewIntegrityChecker(cache, h)

	now := time.Now()

	cache.Set(&Summary{ID: "pkg/auth", Level: 1, Content: "Auth package", Hash: "oldhash", ParentID: "", UpdatedAt: now})

	hashProvider := func(entityID string) (string, error) {
		return "newhash", nil
	}

	ctx := context.Background()
	report, _ := checker.ValidateWithHashes(ctx, hashProvider)

	result, err := checker.Repair(ctx, report)

	if err != nil {
		t.Fatalf("Repair error: %v", err)
	}
	if result.StaleInvalidated != 1 {
		t.Errorf("StaleInvalidated = %d, want 1", result.StaleInvalidated)
	}

	// Should be invalidated (Get returns false)
	_, ok := cache.Get("pkg/auth")
	if ok {
		t.Error("stale entry should be invalidated")
	}
}

func TestIntegrityChecker_Repair_LevelsFixed(t *testing.T) {
	cache := NewSummaryCache(DefaultCacheConfig())
	h := &GoHierarchy{}
	checker := NewIntegrityChecker(cache, h)

	now := time.Now()

	// Add summary with wrong level
	cache.Set(&Summary{ID: "pkg/auth/handler.go", Level: 1, Content: "Handler file", ParentID: "", UpdatedAt: now})

	ctx := context.Background()
	report, _ := checker.Validate(ctx)

	result, err := checker.Repair(ctx, report)

	if err != nil {
		t.Fatalf("Repair error: %v", err)
	}
	if result.LevelsFixed != 1 {
		t.Errorf("LevelsFixed = %d, want 1", result.LevelsFixed)
	}

	// Level should be corrected
	summary, ok := cache.Get("pkg/auth/handler.go")
	if !ok {
		t.Fatal("summary not found after repair")
	}
	if summary.Level != 2 {
		t.Errorf("Level = %d, want 2 after repair", summary.Level)
	}
}

func TestIntegrityReport_IssueCount(t *testing.T) {
	report := &IntegrityReport{
		OrphanedChildren: []string{"a", "b"},
		MissingChildren:  []MissingChild{{}, {}},
		LevelMismatches:  []LevelMismatch{{}},
		StaleEntries:     []StaleEntry{{}, {}, {}},
	}

	if report.IssueCount() != 8 {
		t.Errorf("IssueCount = %d, want 8", report.IssueCount())
	}
}

func TestRepairResult_TotalRepairs(t *testing.T) {
	result := &RepairResult{
		OrphansRemoved:      2,
		StaleInvalidated:    3,
		LevelsFixed:         1,
		ChildrenRegenerated: 0,
	}

	if result.TotalRepairs() != 6 {
		t.Errorf("TotalRepairs = %d, want 6", result.TotalRepairs())
	}
}

func TestIntegrityChecker_Repair_ContextCancellation(t *testing.T) {
	cache := NewSummaryCache(DefaultCacheConfig())
	h := &GoHierarchy{}
	checker := NewIntegrityChecker(cache, h)

	now := time.Now()

	// Add orphaned children
	for i := 0; i < 10; i++ {
		cache.Set(&Summary{ID: "pkg/auth/handler.go", Level: 2, Content: "Handler file", ParentID: "pkg/auth", UpdatedAt: now})
	}

	ctx := context.Background()
	report, _ := checker.Validate(ctx)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := checker.Repair(ctx, report)

	if err != context.Canceled {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestIntegrityChecker_ProjectRootNoParent(t *testing.T) {
	cache := NewSummaryCache(DefaultCacheConfig())
	h := &GoHierarchy{}
	checker := NewIntegrityChecker(cache, h)

	now := time.Now()

	// Project root (level 0) doesn't need a parent
	cache.Set(&Summary{ID: "", Level: 0, Content: "Project root", ParentID: "", UpdatedAt: now})

	ctx := context.Background()
	report, err := checker.Validate(ctx)

	if err != nil {
		t.Fatalf("Validate error: %v", err)
	}
	if !report.Valid {
		t.Error("project root without parent should be valid")
	}
}
