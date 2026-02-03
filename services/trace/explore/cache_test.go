// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package explore

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/trace/ast"
	"github.com/AleutianAI/AleutianFOSS/services/trace/graph"
	"github.com/AleutianAI/AleutianFOSS/services/trace/index"
)

func TestExplorationCache_EntryPoints(t *testing.T) {
	cache := NewExplorationCache(DefaultCacheConfig())

	t.Run("cache miss then hit", func(t *testing.T) {
		opts := DefaultEntryPointOptions()
		opts.Type = EntryPointMain

		// Should be a miss
		result := cache.GetEntryPoints(opts)
		if result != nil {
			t.Error("expected cache miss")
		}

		// Store in cache
		expected := &EntryPointResult{
			EntryPoints: []EntryPoint{
				{ID: "test", Name: "main", Type: EntryPointMain},
			},
			TotalFound: 1,
		}
		cache.SetEntryPoints(opts, expected)

		// Should be a hit
		result = cache.GetEntryPoints(opts)
		if result == nil {
			t.Error("expected cache hit")
		}

		if len(result.EntryPoints) != 1 {
			t.Errorf("expected 1 entry point, got %d", len(result.EntryPoints))
		}
	})

	t.Run("different options have different keys", func(t *testing.T) {
		opts1 := DefaultEntryPointOptions()
		opts1.Type = EntryPointMain

		opts2 := DefaultEntryPointOptions()
		opts2.Type = EntryPointHandler

		cache.SetEntryPoints(opts1, &EntryPointResult{TotalFound: 1})
		cache.SetEntryPoints(opts2, &EntryPointResult{TotalFound: 2})

		result1 := cache.GetEntryPoints(opts1)
		result2 := cache.GetEntryPoints(opts2)

		if result1.TotalFound != 1 || result2.TotalFound != 2 {
			t.Error("options should have different cache keys")
		}
	})
}

func TestExplorationCache_FileSummary(t *testing.T) {
	cache := NewExplorationCache(DefaultCacheConfig())

	t.Run("cache miss then hit", func(t *testing.T) {
		filePath := "handlers/user.go"

		// Should be a miss
		result := cache.GetFileSummary(filePath)
		if result != nil {
			t.Error("expected cache miss")
		}

		// Store in cache
		expected := &FileSummary{
			FilePath: filePath,
			Package:  "handlers",
		}
		cache.SetFileSummary(filePath, expected)

		// Should be a hit
		result = cache.GetFileSummary(filePath)
		if result == nil {
			t.Error("expected cache hit")
		}

		if result.FilePath != filePath {
			t.Errorf("expected file path %s, got %s", filePath, result.FilePath)
		}
	})
}

func TestExplorationCache_PackageAPI(t *testing.T) {
	cache := NewExplorationCache(DefaultCacheConfig())

	t.Run("cache miss then hit", func(t *testing.T) {
		pkgPath := "handlers"

		// Should be a miss
		result := cache.GetPackageAPI(pkgPath)
		if result != nil {
			t.Error("expected cache miss")
		}

		// Store in cache
		expected := &PackageAPI{
			Package: pkgPath,
			Types:   []APISymbol{{Name: "Handler"}},
		}
		cache.SetPackageAPI(pkgPath, expected)

		// Should be a hit
		result = cache.GetPackageAPI(pkgPath)
		if result == nil {
			t.Error("expected cache hit")
		}

		if result.Package != pkgPath {
			t.Errorf("expected package %s, got %s", pkgPath, result.Package)
		}
	})
}

func TestExplorationCache_TTL(t *testing.T) {
	config := CacheConfig{
		MaxSize: 100,
		TTL:     50 * time.Millisecond, // Very short TTL for testing
	}
	cache := NewExplorationCache(config)

	filePath := "test.go"
	cache.SetFileSummary(filePath, &FileSummary{FilePath: filePath})

	// Should be a hit immediately
	if cache.GetFileSummary(filePath) == nil {
		t.Error("expected cache hit")
	}

	// Wait for TTL to expire
	time.Sleep(100 * time.Millisecond)

	// Should be a miss after TTL
	if cache.GetFileSummary(filePath) != nil {
		t.Error("expected cache miss after TTL")
	}
}

func TestExplorationCache_Eviction(t *testing.T) {
	config := CacheConfig{
		MaxSize: 3,
		TTL:     5 * time.Minute,
	}
	cache := NewExplorationCache(config)

	// Add 3 entries
	cache.SetFileSummary("file1.go", &FileSummary{FilePath: "file1.go"})
	cache.SetFileSummary("file2.go", &FileSummary{FilePath: "file2.go"})
	cache.SetFileSummary("file3.go", &FileSummary{FilePath: "file3.go"})

	// All should exist
	if cache.GetFileSummary("file1.go") == nil {
		t.Error("expected file1.go to exist")
	}

	// Add a 4th entry, should evict the oldest
	cache.SetFileSummary("file4.go", &FileSummary{FilePath: "file4.go"})

	// file1.go should be evicted
	if cache.GetFileSummary("file1.go") != nil {
		t.Error("expected file1.go to be evicted")
	}

	// file4.go should exist
	if cache.GetFileSummary("file4.go") == nil {
		t.Error("expected file4.go to exist")
	}
}

func TestExplorationCache_Invalidation(t *testing.T) {
	cache := NewExplorationCache(DefaultCacheConfig())

	t.Run("invalidate file", func(t *testing.T) {
		filePath := "handlers/user.go"
		cache.SetFileSummary(filePath, &FileSummary{FilePath: filePath})

		if cache.GetFileSummary(filePath) == nil {
			t.Error("expected cache entry to exist")
		}

		cache.InvalidateFile(filePath)

		if cache.GetFileSummary(filePath) != nil {
			t.Error("expected cache entry to be invalidated")
		}
	})

	t.Run("invalidate package", func(t *testing.T) {
		pkgPath := "handlers"
		cache.SetPackageAPI(pkgPath, &PackageAPI{Package: pkgPath})

		if cache.GetPackageAPI(pkgPath) == nil {
			t.Error("expected cache entry to exist")
		}

		cache.InvalidatePackage(pkgPath)

		if cache.GetPackageAPI(pkgPath) != nil {
			t.Error("expected cache entry to be invalidated")
		}
	})
}

func TestExplorationCache_Clear(t *testing.T) {
	cache := NewExplorationCache(DefaultCacheConfig())

	// Add various entries
	cache.SetEntryPoints(DefaultEntryPointOptions(), &EntryPointResult{TotalFound: 1})
	cache.SetFileSummary("file.go", &FileSummary{FilePath: "file.go"})
	cache.SetPackageAPI("pkg", &PackageAPI{Package: "pkg"})

	cache.Clear()

	// All should be cleared
	if cache.GetEntryPoints(DefaultEntryPointOptions()) != nil {
		t.Error("expected entry points to be cleared")
	}
	if cache.GetFileSummary("file.go") != nil {
		t.Error("expected file summary to be cleared")
	}
	if cache.GetPackageAPI("pkg") != nil {
		t.Error("expected package API to be cleared")
	}
}

func TestExplorationCache_Stats(t *testing.T) {
	cache := NewExplorationCache(DefaultCacheConfig())

	// Generate some hits and misses
	opts := DefaultEntryPointOptions()

	cache.GetEntryPoints(opts) // Miss
	cache.SetEntryPoints(opts, &EntryPointResult{})
	cache.GetEntryPoints(opts)      // Hit
	cache.GetEntryPoints(opts)      // Hit
	cache.GetFileSummary("test.go") // Miss

	stats := cache.Stats()

	if stats.Hits != 2 {
		t.Errorf("expected 2 hits, got %d", stats.Hits)
	}
	if stats.Misses != 2 {
		t.Errorf("expected 2 misses, got %d", stats.Misses)
	}
	if stats.HitRate != 0.5 {
		t.Errorf("expected 0.5 hit rate, got %f", stats.HitRate)
	}
}

func TestExplorationCache_Concurrency(t *testing.T) {
	cache := NewExplorationCache(DefaultCacheConfig())
	var wg sync.WaitGroup

	// Run concurrent operations
	for i := 0; i < 100; i++ {
		wg.Add(3)

		// Writer
		go func(n int) {
			defer wg.Done()
			cache.SetFileSummary("file.go", &FileSummary{FilePath: "file.go"})
		}(i)

		// Reader
		go func(n int) {
			defer wg.Done()
			cache.GetFileSummary("file.go")
		}(i)

		// Stats reader
		go func(n int) {
			defer wg.Done()
			cache.Stats()
		}(i)
	}

	wg.Wait()
	// If we get here without deadlock or panic, concurrency is working
}

func TestCachedEntryPointFinder(t *testing.T) {
	g, idx := createTestGraph(t)
	finder := NewEntryPointFinder(g, idx)
	cache := NewExplorationCache(DefaultCacheConfig())
	cachedFinder := NewCachedEntryPointFinder(finder, cache)

	ctx := context.Background()
	opts := DefaultEntryPointOptions()

	// First call should compute and cache
	result1, err := cachedFinder.FindEntryPoints(ctx, opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Second call should hit cache
	result2, err := cachedFinder.FindEntryPoints(ctx, opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Results should be the same
	if len(result1.EntryPoints) != len(result2.EntryPoints) {
		t.Error("cached results should match")
	}

	// Check stats show a hit
	stats := cache.Stats()
	if stats.Hits < 1 {
		t.Error("expected at least 1 cache hit")
	}
}

func TestCachedFileSummarizer(t *testing.T) {
	g, idx := createSummarizeTestGraph(t)
	summarizer := NewFileSummarizer(g, idx)
	cache := NewExplorationCache(DefaultCacheConfig())
	cachedSummarizer := NewCachedFileSummarizer(summarizer, cache)

	ctx := context.Background()
	filePath := "handlers/user.go"

	// First call should compute and cache
	result1, err := cachedSummarizer.SummarizeFile(ctx, filePath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Second call should hit cache
	result2, err := cachedSummarizer.SummarizeFile(ctx, filePath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Results should be the same
	if result1.FilePath != result2.FilePath {
		t.Error("cached results should match")
	}

	// Check stats show a hit
	stats := cache.Stats()
	if stats.Hits < 1 {
		t.Error("expected at least 1 cache hit")
	}
}

func TestCachedPackageAPISummarizer(t *testing.T) {
	g, idx := createSummarizeTestGraph(t)
	summarizer := NewPackageAPISummarizer(g, idx)
	cache := NewExplorationCache(DefaultCacheConfig())
	cachedSummarizer := NewCachedPackageAPISummarizer(summarizer, cache)

	ctx := context.Background()
	pkgPath := "internal"

	// First call should compute and cache
	result1, err := cachedSummarizer.FindPackageAPI(ctx, pkgPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Second call should hit cache
	result2, err := cachedSummarizer.FindPackageAPI(ctx, pkgPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Results should be the same
	if result1.Package != result2.Package {
		t.Error("cached results should match")
	}

	// Check stats show a hit
	stats := cache.Stats()
	if stats.Hits < 1 {
		t.Error("expected at least 1 cache hit")
	}
}

func BenchmarkExplorationCache_GetSet(b *testing.B) {
	cache := NewExplorationCache(DefaultCacheConfig())
	filePath := "test.go"
	summary := &FileSummary{FilePath: filePath}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache.SetFileSummary(filePath, summary)
		cache.GetFileSummary(filePath)
	}
}

func BenchmarkExplorationCache_Concurrent(b *testing.B) {
	cache := NewExplorationCache(DefaultCacheConfig())
	filePath := "test.go"
	summary := &FileSummary{FilePath: filePath}
	cache.SetFileSummary(filePath, summary)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			cache.GetFileSummary(filePath)
		}
	})
}

// Helper to create test graph for entry point tests (same as in entry_points_test.go)
func createTestGraphForCache(t *testing.T) (*graph.Graph, *index.SymbolIndex) {
	t.Helper()

	g := graph.NewGraph("/test/project")
	idx := index.NewSymbolIndex()

	sym := &ast.Symbol{
		ID:        "cmd/main.go:1:main",
		Name:      "main",
		Kind:      ast.SymbolKindFunction,
		FilePath:  "cmd/main.go",
		Package:   "main",
		Language:  "go",
		StartLine: 1,
		EndLine:   10,
	}

	g.AddNode(sym)
	idx.Add(sym)
	g.Freeze()

	return g, idx
}
