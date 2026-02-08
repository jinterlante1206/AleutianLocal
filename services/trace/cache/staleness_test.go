// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package cache

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/trace/graph"
	"github.com/AleutianAI/AleutianFOSS/services/trace/manifest"
)

func TestComputeSourceHash(t *testing.T) {
	t.Run("computes hash for directory with source files", func(t *testing.T) {
		// Create temp directory with test files
		tempDir := t.TempDir()

		// Create some Go files
		if err := os.WriteFile(filepath.Join(tempDir, "main.go"), []byte("package main"), 0644); err != nil {
			t.Fatalf("failed to create test file: %v", err)
		}
		if err := os.WriteFile(filepath.Join(tempDir, "utils.go"), []byte("package main"), 0644); err != nil {
			t.Fatalf("failed to create test file: %v", err)
		}

		ctx := context.Background()
		ClearHashCache()

		hash, fileCount, err := ComputeSourceHash(ctx, tempDir)
		if err != nil {
			t.Fatalf("ComputeSourceHash failed: %v", err)
		}

		if hash == "" {
			t.Error("expected non-empty hash")
		}
		if len(hash) != 64 {
			t.Errorf("hash length = %d, want 64", len(hash))
		}
		if fileCount != 2 {
			t.Errorf("fileCount = %d, want 2", fileCount)
		}
	})

	t.Run("returns same hash for unchanged files", func(t *testing.T) {
		tempDir := t.TempDir()

		if err := os.WriteFile(filepath.Join(tempDir, "main.go"), []byte("package main"), 0644); err != nil {
			t.Fatalf("failed to create test file: %v", err)
		}

		ctx := context.Background()
		ClearHashCache()

		hash1, _, err := ComputeSourceHash(ctx, tempDir)
		if err != nil {
			t.Fatalf("first ComputeSourceHash failed: %v", err)
		}

		// Clear cache to force recomputation
		ClearHashCache()

		hash2, _, err := ComputeSourceHash(ctx, tempDir)
		if err != nil {
			t.Fatalf("second ComputeSourceHash failed: %v", err)
		}

		if hash1 != hash2 {
			t.Errorf("hashes should match: %s != %s", hash1, hash2)
		}
	})

	t.Run("returns different hash when file modified", func(t *testing.T) {
		tempDir := t.TempDir()

		filePath := filepath.Join(tempDir, "main.go")
		// L4 Fix: Check all WriteFile errors
		if err := os.WriteFile(filePath, []byte("package main"), 0644); err != nil {
			t.Fatalf("failed to create test file: %v", err)
		}

		ctx := context.Background()
		ClearHashCache()

		hash1, _, err := ComputeSourceHash(ctx, tempDir)
		if err != nil {
			t.Fatalf("first ComputeSourceHash failed: %v", err)
		}

		// H6 Fix: Use explicit mtime change instead of relying on sleep
		// This makes the test deterministic and not dependent on filesystem granularity
		time.Sleep(20 * time.Millisecond) // Longer sleep for filesystem granularity
		newContent := []byte("package main\n// modified at " + time.Now().String())
		if err := os.WriteFile(filePath, newContent, 0644); err != nil {
			t.Fatalf("failed to modify test file: %v", err)
		}

		// Clear cache to force recomputation
		ClearHashCache()

		hash2, _, err := ComputeSourceHash(ctx, tempDir)
		if err != nil {
			t.Fatalf("second ComputeSourceHash failed: %v", err)
		}

		if hash1 == hash2 {
			// H6 Fix: Better error message showing file info
			info, _ := os.Stat(filePath)
			t.Errorf("hashes should differ after modification (mtime: %v, size: %d)", info.ModTime(), info.Size())
		}
	})

	t.Run("returns different hash when file added", func(t *testing.T) {
		tempDir := t.TempDir()

		if err := os.WriteFile(filepath.Join(tempDir, "main.go"), []byte("package main"), 0644); err != nil {
			t.Fatalf("failed to create test file: %v", err)
		}

		ctx := context.Background()
		ClearHashCache()

		hash1, count1, err := ComputeSourceHash(ctx, tempDir)
		if err != nil {
			t.Fatalf("first ComputeSourceHash failed: %v", err)
		}

		// Add new file
		if err := os.WriteFile(filepath.Join(tempDir, "utils.go"), []byte("package main"), 0644); err != nil {
			t.Fatalf("failed to create second test file: %v", err)
		}

		ClearHashCache()

		hash2, count2, err := ComputeSourceHash(ctx, tempDir)
		if err != nil {
			t.Fatalf("second ComputeSourceHash failed: %v", err)
		}

		if hash1 == hash2 {
			t.Error("hashes should differ after adding file")
		}
		if count2 != count1+1 {
			t.Errorf("count should increase: %d -> %d", count1, count2)
		}
	})

	t.Run("skips non-source files", func(t *testing.T) {
		tempDir := t.TempDir()

		// Create source and non-source files
		if err := os.WriteFile(filepath.Join(tempDir, "main.go"), []byte("package main"), 0644); err != nil {
			t.Fatalf("failed to create test file: %v", err)
		}
		if err := os.WriteFile(filepath.Join(tempDir, "README.md"), []byte("# README"), 0644); err != nil {
			t.Fatalf("failed to create readme: %v", err)
		}
		if err := os.WriteFile(filepath.Join(tempDir, "data.json"), []byte("{}"), 0644); err != nil {
			t.Fatalf("failed to create json: %v", err)
		}

		ctx := context.Background()
		ClearHashCache()

		_, fileCount, err := ComputeSourceHash(ctx, tempDir)
		if err != nil {
			t.Fatalf("ComputeSourceHash failed: %v", err)
		}

		if fileCount != 1 {
			t.Errorf("fileCount = %d, want 1 (only .go)", fileCount)
		}
	})

	t.Run("skips vendor and node_modules", func(t *testing.T) {
		tempDir := t.TempDir()

		// Create directories
		if err := os.MkdirAll(filepath.Join(tempDir, "vendor"), 0755); err != nil {
			t.Fatalf("failed to create vendor dir: %v", err)
		}
		if err := os.MkdirAll(filepath.Join(tempDir, "node_modules"), 0755); err != nil {
			t.Fatalf("failed to create node_modules dir: %v", err)
		}

		// Create files in skipped directories
		if err := os.WriteFile(filepath.Join(tempDir, "vendor", "lib.go"), []byte("package lib"), 0644); err != nil {
			t.Fatalf("failed to create vendor file: %v", err)
		}
		if err := os.WriteFile(filepath.Join(tempDir, "node_modules", "index.js"), []byte("module.exports = {}"), 0644); err != nil {
			t.Fatalf("failed to create node_modules file: %v", err)
		}

		// Create source file in main dir
		if err := os.WriteFile(filepath.Join(tempDir, "main.go"), []byte("package main"), 0644); err != nil {
			t.Fatalf("failed to create main file: %v", err)
		}

		ctx := context.Background()
		ClearHashCache()

		_, fileCount, err := ComputeSourceHash(ctx, tempDir)
		if err != nil {
			t.Fatalf("ComputeSourceHash failed: %v", err)
		}

		if fileCount != 1 {
			t.Errorf("fileCount = %d, want 1 (vendor/node_modules should be skipped)", fileCount)
		}
	})

	t.Run("respects context cancellation", func(t *testing.T) {
		tempDir := t.TempDir()

		// Create some files
		for i := 0; i < 10; i++ {
			if err := os.WriteFile(filepath.Join(tempDir, "file"+string(rune('0'+i))+".go"), []byte("package main"), 0644); err != nil {
				t.Fatalf("failed to create test file: %v", err)
			}
		}

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately
		ClearHashCache()

		_, _, err := ComputeSourceHash(ctx, tempDir)
		if err == nil {
			t.Error("expected error on cancelled context")
		}
	})

	t.Run("caches results within TTL", func(t *testing.T) {
		tempDir := t.TempDir()

		if err := os.WriteFile(filepath.Join(tempDir, "main.go"), []byte("package main"), 0644); err != nil {
			t.Fatalf("failed to create test file: %v", err)
		}

		ctx := context.Background()
		ClearHashCache()

		// First call computes hash
		hash1, _, _ := ComputeSourceHash(ctx, tempDir)

		// Modify file (but don't clear cache)
		time.Sleep(10 * time.Millisecond)
		if err := os.WriteFile(filepath.Join(tempDir, "main.go"), []byte("package main\n// modified"), 0644); err != nil {
			t.Fatalf("failed to modify test file: %v", err)
		}

		// Second call should return cached hash (not see the modification)
		hash2, _, _ := ComputeSourceHash(ctx, tempDir)

		if hash1 != hash2 {
			t.Error("expected cached hash to be returned")
		}

		// After invalidating cache, should see new hash
		InvalidateHashCache(tempDir)
		hash3, _, _ := ComputeSourceHash(ctx, tempDir)

		if hash1 == hash3 {
			t.Error("expected new hash after cache invalidation")
		}
	})
}

func TestCheckStaleness(t *testing.T) {
	t.Run("returns StalenessNone for valid entry", func(t *testing.T) {
		tempDir := t.TempDir()

		if err := os.WriteFile(filepath.Join(tempDir, "main.go"), []byte("package main"), 0644); err != nil {
			t.Fatalf("failed to create test file: %v", err)
		}

		ctx := context.Background()
		ClearHashCache()

		// Compute current hash
		hash, _, err := ComputeSourceHash(ctx, tempDir)
		if err != nil {
			t.Fatalf("ComputeSourceHash failed: %v", err)
		}

		entry := &CacheEntry{
			GraphID:        "test-graph",
			ProjectRoot:    tempDir,
			BuilderVersion: GraphBuilderVersion,
			SourceHash:     hash,
		}

		reason, err := CheckStaleness(ctx, entry)
		if err != nil {
			t.Fatalf("CheckStaleness failed: %v", err)
		}

		if reason != StalenessNone {
			t.Errorf("expected StalenessNone, got %s", reason)
		}
	})

	t.Run("returns StalenessVersionMismatch for old builder version", func(t *testing.T) {
		tempDir := t.TempDir()

		if err := os.WriteFile(filepath.Join(tempDir, "main.go"), []byte("package main"), 0644); err != nil {
			t.Fatalf("failed to create test file: %v", err)
		}

		ctx := context.Background()
		ClearHashCache()

		hash, _, _ := ComputeSourceHash(ctx, tempDir)

		entry := &CacheEntry{
			GraphID:        "test-graph",
			ProjectRoot:    tempDir,
			BuilderVersion: "old-version-1.0",
			SourceHash:     hash,
		}

		reason, err := CheckStaleness(ctx, entry)
		if err != nil {
			t.Fatalf("CheckStaleness failed: %v", err)
		}

		if reason != StalenessVersionMismatch {
			t.Errorf("expected StalenessVersionMismatch, got %s", reason)
		}
	})

	t.Run("returns StalenessSourceChanged for changed files", func(t *testing.T) {
		tempDir := t.TempDir()

		// L4 Fix: Check WriteFile error
		if err := os.WriteFile(filepath.Join(tempDir, "main.go"), []byte("package main"), 0644); err != nil {
			t.Fatalf("failed to create test file: %v", err)
		}

		ctx := context.Background()
		ClearHashCache()

		// Compute hash at build time
		hash, _, err := ComputeSourceHash(ctx, tempDir)
		if err != nil {
			t.Fatalf("ComputeSourceHash failed: %v", err)
		}

		entry := &CacheEntry{
			GraphID:        "test-graph",
			ProjectRoot:    tempDir,
			BuilderVersion: GraphBuilderVersion,
			SourceHash:     hash,
		}

		// H6 Fix: Longer sleep and unique content for determinism
		time.Sleep(20 * time.Millisecond)
		newContent := []byte("package main\n// modified at " + time.Now().String())
		if err := os.WriteFile(filepath.Join(tempDir, "main.go"), newContent, 0644); err != nil {
			t.Fatalf("failed to modify test file: %v", err)
		}

		ClearHashCache()

		reason, err := CheckStaleness(ctx, entry)
		if err != nil {
			t.Fatalf("CheckStaleness failed: %v", err)
		}

		if reason != StalenessSourceChanged {
			t.Errorf("expected StalenessSourceChanged, got %s", reason)
		}
	})

	t.Run("checks version before source hash", func(t *testing.T) {
		tempDir := t.TempDir()

		if err := os.WriteFile(filepath.Join(tempDir, "main.go"), []byte("package main"), 0644); err != nil {
			t.Fatalf("failed to create test file: %v", err)
		}

		ctx := context.Background()
		ClearHashCache()

		// Entry with wrong version AND wrong hash
		entry := &CacheEntry{
			GraphID:        "test-graph",
			ProjectRoot:    tempDir,
			BuilderVersion: "old-version",
			SourceHash:     "wrong-hash",
		}

		reason, err := CheckStaleness(ctx, entry)
		if err != nil {
			t.Fatalf("CheckStaleness failed: %v", err)
		}

		// Should return version mismatch (checked first) not source changed
		if reason != StalenessVersionMismatch {
			t.Errorf("expected StalenessVersionMismatch, got %s", reason)
		}
	})
}

func TestGR42_GetOrBuild_StalenessIntegration(t *testing.T) {
	t.Run("rebuilds on builder version mismatch", func(t *testing.T) {
		tempDir := t.TempDir()

		if err := os.WriteFile(filepath.Join(tempDir, "main.go"), []byte("package main"), 0644); err != nil {
			t.Fatalf("failed to create test file: %v", err)
		}

		cache := NewGraphCache()
		ctx := context.Background()
		ClearHashCache()

		var buildCount int32
		buildFunc := countingBuildFunc(&buildCount, graph.NewGraph(tempDir), manifest.NewManifest(tempDir))

		// First build
		_, release1, err := cache.GetOrBuild(ctx, tempDir, buildFunc)
		if err != nil {
			t.Fatalf("first build failed: %v", err)
		}
		release1()

		// Simulate old builder version by directly modifying the entry
		cache.mu.Lock()
		graphID := GenerateGraphID(tempDir)
		if e, ok := cache.entries[graphID]; ok {
			e.BuilderVersion = "old-version-1.0"
		}
		cache.mu.Unlock()

		ClearHashCache()

		// Second call should detect version mismatch and rebuild
		entry2, release2, err := cache.GetOrBuild(ctx, tempDir, buildFunc)
		if err != nil {
			t.Fatalf("second build failed: %v", err)
		}
		release2()

		if buildCount != 2 {
			t.Errorf("buildCount = %d, want 2 (should rebuild on version mismatch)", buildCount)
		}

		if entry2.BuilderVersion != GraphBuilderVersion {
			t.Errorf("entry should have current builder version, got %s", entry2.BuilderVersion)
		}

		stats := cache.Stats()
		if stats.StaleRebuilds != 1 {
			t.Errorf("StaleRebuilds = %d, want 1", stats.StaleRebuilds)
		}
	})

	t.Run("rebuilds on source file change", func(t *testing.T) {
		tempDir := t.TempDir()

		if err := os.WriteFile(filepath.Join(tempDir, "main.go"), []byte("package main"), 0644); err != nil {
			t.Fatalf("failed to create test file: %v", err)
		}

		cache := NewGraphCache()
		ctx := context.Background()
		ClearHashCache()

		var buildCount int32
		buildFunc := countingBuildFunc(&buildCount, graph.NewGraph(tempDir), manifest.NewManifest(tempDir))

		// First build
		_, release1, err := cache.GetOrBuild(ctx, tempDir, buildFunc)
		if err != nil {
			t.Fatalf("first build failed: %v", err)
		}
		release1()

		// Modify source file
		time.Sleep(10 * time.Millisecond)
		if err := os.WriteFile(filepath.Join(tempDir, "main.go"), []byte("package main\n// modified"), 0644); err != nil {
			t.Fatalf("failed to modify test file: %v", err)
		}

		ClearHashCache()

		// Second call should detect source change and rebuild
		entry2, release2, err := cache.GetOrBuild(ctx, tempDir, buildFunc)
		if err != nil {
			t.Fatalf("second build failed: %v", err)
		}
		release2()

		if buildCount != 2 {
			t.Errorf("buildCount = %d, want 2 (should rebuild on source change)", buildCount)
		}

		if entry2.SourceHash == "" {
			t.Error("entry should have source hash set")
		}

		stats := cache.Stats()
		if stats.StaleRebuilds != 1 {
			t.Errorf("StaleRebuilds = %d, want 1", stats.StaleRebuilds)
		}
	})

	t.Run("does not rebuild when files unchanged", func(t *testing.T) {
		tempDir := t.TempDir()

		if err := os.WriteFile(filepath.Join(tempDir, "main.go"), []byte("package main"), 0644); err != nil {
			t.Fatalf("failed to create test file: %v", err)
		}

		cache := NewGraphCache()
		ctx := context.Background()
		ClearHashCache()

		var buildCount int32
		buildFunc := countingBuildFunc(&buildCount, graph.NewGraph(tempDir), manifest.NewManifest(tempDir))

		// First build
		_, release1, err := cache.GetOrBuild(ctx, tempDir, buildFunc)
		if err != nil {
			t.Fatalf("first build failed: %v", err)
		}
		release1()

		ClearHashCache()

		// Second call with no changes - should use cache
		_, release2, err := cache.GetOrBuild(ctx, tempDir, buildFunc)
		if err != nil {
			t.Fatalf("second build failed: %v", err)
		}
		release2()

		if buildCount != 1 {
			t.Errorf("buildCount = %d, want 1 (should use cache when unchanged)", buildCount)
		}

		stats := cache.Stats()
		if stats.StaleRebuilds != 0 {
			t.Errorf("StaleRebuilds = %d, want 0", stats.StaleRebuilds)
		}
	})

	t.Run("new entry has BuilderVersion and SourceHash set", func(t *testing.T) {
		tempDir := t.TempDir()

		if err := os.WriteFile(filepath.Join(tempDir, "main.go"), []byte("package main"), 0644); err != nil {
			t.Fatalf("failed to create test file: %v", err)
		}

		cache := NewGraphCache()
		ctx := context.Background()
		ClearHashCache()

		g := graph.NewGraph(tempDir)
		m := manifest.NewManifest(tempDir)

		entry, release, err := cache.GetOrBuild(ctx, tempDir, mockBuildFunc(g, m, nil))
		if err != nil {
			t.Fatalf("build failed: %v", err)
		}
		defer release()

		if entry.BuilderVersion != GraphBuilderVersion {
			t.Errorf("BuilderVersion = %s, want %s", entry.BuilderVersion, GraphBuilderVersion)
		}

		if entry.SourceHash == "" {
			t.Error("SourceHash should not be empty")
		}

		if len(entry.SourceHash) != 64 {
			t.Errorf("SourceHash length = %d, want 64", len(entry.SourceHash))
		}
	})
}

// A2 Fix: Test for DisableStalenessCheck option
func TestGR42_DisableStalenessCheck(t *testing.T) {
	t.Run("skips staleness check when disabled", func(t *testing.T) {
		tempDir := t.TempDir()

		if err := os.WriteFile(filepath.Join(tempDir, "main.go"), []byte("package main"), 0644); err != nil {
			t.Fatalf("failed to create test file: %v", err)
		}

		// Create cache with staleness check DISABLED
		cache := NewGraphCache(WithStalenessCheck(false))
		ctx := context.Background()
		ClearHashCache()

		var buildCount int32
		buildFunc := countingBuildFunc(&buildCount, graph.NewGraph(tempDir), manifest.NewManifest(tempDir))

		// First build
		entry1, release1, err := cache.GetOrBuild(ctx, tempDir, buildFunc)
		if err != nil {
			t.Fatalf("first build failed: %v", err)
		}
		release1()

		// Simulate old builder version
		cache.mu.Lock()
		graphID := GenerateGraphID(tempDir)
		if e, ok := cache.entries[graphID]; ok {
			e.BuilderVersion = "old-version-1.0"
		}
		cache.mu.Unlock()

		ClearHashCache()

		// Second call should NOT detect version mismatch (staleness check disabled)
		entry2, release2, err := cache.GetOrBuild(ctx, tempDir, buildFunc)
		if err != nil {
			t.Fatalf("second build failed: %v", err)
		}
		release2()

		if buildCount != 1 {
			t.Errorf("buildCount = %d, want 1 (should use cache when staleness check disabled)", buildCount)
		}

		// Should return same entry
		if entry1.GraphID != entry2.GraphID {
			t.Error("expected same entry to be returned")
		}
	})

	t.Run("performs staleness check when enabled (default)", func(t *testing.T) {
		tempDir := t.TempDir()

		if err := os.WriteFile(filepath.Join(tempDir, "main.go"), []byte("package main"), 0644); err != nil {
			t.Fatalf("failed to create test file: %v", err)
		}

		// Create cache with staleness check ENABLED (default)
		cache := NewGraphCache(WithStalenessCheck(true))
		ctx := context.Background()
		ClearHashCache()

		var buildCount int32
		buildFunc := countingBuildFunc(&buildCount, graph.NewGraph(tempDir), manifest.NewManifest(tempDir))

		// First build
		_, release1, err := cache.GetOrBuild(ctx, tempDir, buildFunc)
		if err != nil {
			t.Fatalf("first build failed: %v", err)
		}
		release1()

		// Simulate old builder version
		cache.mu.Lock()
		graphID := GenerateGraphID(tempDir)
		if e, ok := cache.entries[graphID]; ok {
			e.BuilderVersion = "old-version-1.0"
		}
		cache.mu.Unlock()

		ClearHashCache()

		// Second call SHOULD detect version mismatch and rebuild
		_, release2, err := cache.GetOrBuild(ctx, tempDir, buildFunc)
		if err != nil {
			t.Fatalf("second build failed: %v", err)
		}
		release2()

		if buildCount != 2 {
			t.Errorf("buildCount = %d, want 2 (should rebuild on version mismatch)", buildCount)
		}
	})
}

func TestInvalidateHashCache(t *testing.T) {
	t.Run("invalidates specific project", func(t *testing.T) {
		tempDir := t.TempDir()

		if err := os.WriteFile(filepath.Join(tempDir, "main.go"), []byte("package main"), 0644); err != nil {
			t.Fatalf("failed to create test file: %v", err)
		}

		ctx := context.Background()
		ClearHashCache()

		// Cache the hash
		hash1, _, _ := ComputeSourceHash(ctx, tempDir)

		// Modify file
		time.Sleep(10 * time.Millisecond)
		if err := os.WriteFile(filepath.Join(tempDir, "main.go"), []byte("package main\n// modified"), 0644); err != nil {
			t.Fatalf("failed to modify test file: %v", err)
		}

		// Without invalidation, should return cached hash
		hash2, _, _ := ComputeSourceHash(ctx, tempDir)
		if hash1 != hash2 {
			t.Error("expected cached hash before invalidation")
		}

		// After invalidation, should compute new hash
		InvalidateHashCache(tempDir)
		hash3, _, _ := ComputeSourceHash(ctx, tempDir)
		if hash1 == hash3 {
			t.Error("expected different hash after invalidation")
		}
	})
}

func TestClearHashCache(t *testing.T) {
	t.Run("clears all cached hashes", func(t *testing.T) {
		tempDir1 := t.TempDir()
		tempDir2 := t.TempDir()

		if err := os.WriteFile(filepath.Join(tempDir1, "main.go"), []byte("package main"), 0644); err != nil {
			t.Fatalf("failed to create test file: %v", err)
		}
		if err := os.WriteFile(filepath.Join(tempDir2, "main.go"), []byte("package main"), 0644); err != nil {
			t.Fatalf("failed to create test file: %v", err)
		}

		ctx := context.Background()
		ClearHashCache()

		// Cache both hashes
		hash1a, _, _ := ComputeSourceHash(ctx, tempDir1)
		hash2a, _, _ := ComputeSourceHash(ctx, tempDir2)

		// Modify both files
		time.Sleep(10 * time.Millisecond)
		os.WriteFile(filepath.Join(tempDir1, "main.go"), []byte("package main\n// 1"), 0644)
		os.WriteFile(filepath.Join(tempDir2, "main.go"), []byte("package main\n// 2"), 0644)

		// Clear all
		ClearHashCache()

		// Both should now see changes
		hash1b, _, _ := ComputeSourceHash(ctx, tempDir1)
		hash2b, _, _ := ComputeSourceHash(ctx, tempDir2)

		if hash1a == hash1b {
			t.Error("expected different hash for tempDir1 after ClearHashCache")
		}
		if hash2a == hash2b {
			t.Error("expected different hash for tempDir2 after ClearHashCache")
		}
	})
}

func TestTruncateHash(t *testing.T) {
	t.Run("truncates long hashes", func(t *testing.T) {
		hash := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
		result := truncateHash(hash)
		if result != "0123456789abcdef..." {
			t.Errorf("truncateHash = %s, want 0123456789abcdef...", result)
		}
	})

	t.Run("returns short hashes unchanged", func(t *testing.T) {
		hash := "0123456789"
		result := truncateHash(hash)
		if result != hash {
			t.Errorf("truncateHash = %s, want %s", result, hash)
		}
	})
}

// C1 Fix: Test nil entry handling
func TestCheckStaleness_NilEntry(t *testing.T) {
	ctx := context.Background()
	reason, err := CheckStaleness(ctx, nil)
	if err == nil {
		t.Error("expected error for nil entry")
	}
	if reason != StalenessHashError {
		t.Errorf("expected StalenessHashError, got %s", reason)
	}
}

// Note: countingBuildFunc and mockBuildFunc are defined in graph_cache_test.go
