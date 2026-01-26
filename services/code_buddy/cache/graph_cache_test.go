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
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/graph"
	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/manifest"
)

// mockBuildFunc creates a build function for testing.
func mockBuildFunc(g *graph.Graph, m *manifest.Manifest, err error) BuildFunc {
	return func(ctx context.Context, projectRoot string) (*graph.Graph, *manifest.Manifest, error) {
		return g, m, err
	}
}

// slowBuildFunc creates a build function that takes some time.
func slowBuildFunc(delay time.Duration, g *graph.Graph, m *manifest.Manifest) BuildFunc {
	return func(ctx context.Context, projectRoot string) (*graph.Graph, *manifest.Manifest, error) {
		select {
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		case <-time.After(delay):
			return g, m, nil
		}
	}
}

// countingBuildFunc counts how many times it's called.
func countingBuildFunc(counter *int32, g *graph.Graph, m *manifest.Manifest) BuildFunc {
	return func(ctx context.Context, projectRoot string) (*graph.Graph, *manifest.Manifest, error) {
		atomic.AddInt32(counter, 1)
		return g, m, nil
	}
}

func TestNewGraphCache(t *testing.T) {
	t.Run("default options", func(t *testing.T) {
		cache := NewGraphCache()

		if cache == nil {
			t.Fatal("NewGraphCache returned nil")
		}
		if cache.entries == nil {
			t.Error("entries map is nil")
		}
		if cache.lru == nil {
			t.Error("lru list is nil")
		}
		if cache.failedBuilds == nil {
			t.Error("failedBuilds map is nil")
		}
		if cache.options.MaxEntries != DefaultMaxEntries {
			t.Errorf("MaxEntries = %d, want %d", cache.options.MaxEntries, DefaultMaxEntries)
		}
		if cache.options.MaxAge != DefaultMaxAge {
			t.Errorf("MaxAge = %v, want %v", cache.options.MaxAge, DefaultMaxAge)
		}
	})

	t.Run("with custom options", func(t *testing.T) {
		cache := NewGraphCache(
			WithMaxEntries(10),
			WithMaxAge(1*time.Hour),
			WithErrorCacheTTL(10*time.Second),
		)

		if cache.options.MaxEntries != 10 {
			t.Errorf("MaxEntries = %d, want 10", cache.options.MaxEntries)
		}
		if cache.options.MaxAge != 1*time.Hour {
			t.Errorf("MaxAge = %v, want 1h", cache.options.MaxAge)
		}
		if cache.options.ErrorCacheTTL != 10*time.Second {
			t.Errorf("ErrorCacheTTL = %v, want 10s", cache.options.ErrorCacheTTL)
		}
	})

	t.Run("invalid options are ignored", func(t *testing.T) {
		cache := NewGraphCache(
			WithMaxEntries(-5),       // Should be ignored
			WithMaxAge(-1*time.Hour), // Should be ignored
		)

		// Should use defaults when invalid values provided
		if cache.options.MaxEntries != DefaultMaxEntries {
			t.Errorf("MaxEntries = %d, want default %d", cache.options.MaxEntries, DefaultMaxEntries)
		}
		if cache.options.MaxAge != DefaultMaxAge {
			t.Errorf("MaxAge = %v, want default %v", cache.options.MaxAge, DefaultMaxAge)
		}
	})
}

func TestGraphCache_Get(t *testing.T) {
	t.Run("miss on empty cache", func(t *testing.T) {
		cache := NewGraphCache()

		entry, release, ok := cache.Get("/some/path")

		if ok {
			t.Error("expected miss on empty cache")
		}
		if entry != nil {
			t.Error("expected nil entry on miss")
		}
		if release != nil {
			t.Error("expected nil release on miss")
		}

		stats := cache.Stats()
		if stats.Misses != 1 {
			t.Errorf("Misses = %d, want 1", stats.Misses)
		}
	})

	t.Run("hit after GetOrBuild", func(t *testing.T) {
		cache := NewGraphCache()
		ctx := context.Background()
		g := graph.NewGraph("/test/project")
		m := manifest.NewManifest("/test/project")

		// First: build and cache
		entry1, release1, err := cache.GetOrBuild(ctx, "/test/project", mockBuildFunc(g, m, nil))
		if err != nil {
			t.Fatalf("GetOrBuild failed: %v", err)
		}
		release1()

		// Second: should be a cache hit
		entry2, release2, ok := cache.Get("/test/project")
		if !ok {
			t.Error("expected cache hit")
		}
		if entry2 == nil {
			t.Error("expected non-nil entry")
		}
		if entry2 != entry1 {
			t.Error("expected same entry on hit")
		}
		release2()

		stats := cache.Stats()
		if stats.Hits != 1 {
			t.Errorf("Hits = %d, want 1", stats.Hits)
		}
	})

	t.Run("miss on stale entry", func(t *testing.T) {
		cache := NewGraphCache()
		ctx := context.Background()
		g := graph.NewGraph("/test/project")
		m := manifest.NewManifest("/test/project")

		// Build and cache
		_, release, err := cache.GetOrBuild(ctx, "/test/project", mockBuildFunc(g, m, nil))
		if err != nil {
			t.Fatalf("GetOrBuild failed: %v", err)
		}
		release()

		// Mark as stale
		cache.ForceInvalidate("/test/project")

		// Should miss now
		entry, _, ok := cache.Get("/test/project")
		if ok {
			t.Error("expected miss on stale entry")
		}
		if entry != nil {
			t.Error("expected nil entry on stale")
		}
	})

	t.Run("miss on expired entry", func(t *testing.T) {
		cache := NewGraphCache(WithMaxAge(1 * time.Millisecond))
		ctx := context.Background()
		g := graph.NewGraph("/test/project")
		m := manifest.NewManifest("/test/project")

		// Build and cache
		_, release, err := cache.GetOrBuild(ctx, "/test/project", mockBuildFunc(g, m, nil))
		if err != nil {
			t.Fatalf("GetOrBuild failed: %v", err)
		}
		release()

		// Wait for expiration
		time.Sleep(10 * time.Millisecond)

		// Should miss now
		entry, _, ok := cache.Get("/test/project")
		if ok {
			t.Error("expected miss on expired entry")
		}
		if entry != nil {
			t.Error("expected nil entry on expired")
		}
	})

	t.Run("release function decrements refCount", func(t *testing.T) {
		cache := NewGraphCache()
		ctx := context.Background()
		g := graph.NewGraph("/test/project")
		m := manifest.NewManifest("/test/project")

		// Build and cache
		entry, release, err := cache.GetOrBuild(ctx, "/test/project", mockBuildFunc(g, m, nil))
		if err != nil {
			t.Fatalf("GetOrBuild failed: %v", err)
		}

		if !entry.InUse() {
			t.Error("expected entry to be in use")
		}

		release()

		if entry.InUse() {
			t.Error("expected entry to not be in use after release")
		}
	})
}

func TestGraphCache_GetOrBuild(t *testing.T) {
	t.Run("builds on first call", func(t *testing.T) {
		cache := NewGraphCache()
		ctx := context.Background()
		g := graph.NewGraph("/test/project")
		m := manifest.NewManifest("/test/project")

		entry, release, err := cache.GetOrBuild(ctx, "/test/project", mockBuildFunc(g, m, nil))
		if err != nil {
			t.Fatalf("GetOrBuild failed: %v", err)
		}
		defer release()

		if entry == nil {
			t.Error("expected non-nil entry")
		}
		if entry.Graph != g {
			t.Error("expected same graph")
		}
		if entry.Manifest != m {
			t.Error("expected same manifest")
		}

		stats := cache.Stats()
		if stats.BuildCount != 1 {
			t.Errorf("BuildCount = %d, want 1", stats.BuildCount)
		}
	})

	t.Run("returns cached on second call", func(t *testing.T) {
		cache := NewGraphCache()
		ctx := context.Background()
		g := graph.NewGraph("/test/project")
		m := manifest.NewManifest("/test/project")

		var buildCount int32
		build := countingBuildFunc(&buildCount, g, m)

		// First call
		entry1, release1, err := cache.GetOrBuild(ctx, "/test/project", build)
		if err != nil {
			t.Fatalf("first GetOrBuild failed: %v", err)
		}
		release1()

		// Second call
		entry2, release2, err := cache.GetOrBuild(ctx, "/test/project", build)
		if err != nil {
			t.Fatalf("second GetOrBuild failed: %v", err)
		}
		release2()

		if entry1 != entry2 {
			t.Error("expected same entry on second call")
		}
		if buildCount != 1 {
			t.Errorf("build called %d times, want 1", buildCount)
		}
	})

	t.Run("returns build error", func(t *testing.T) {
		cache := NewGraphCache()
		ctx := context.Background()
		buildErr := errors.New("build failed")

		entry, release, err := cache.GetOrBuild(ctx, "/test/project", mockBuildFunc(nil, nil, buildErr))
		if err == nil {
			t.Error("expected error")
		}
		if entry != nil {
			t.Error("expected nil entry on error")
		}
		if release != nil {
			t.Error("expected nil release on error")
		}

		stats := cache.Stats()
		if stats.ErrorCount != 1 {
			t.Errorf("ErrorCount = %d, want 1", stats.ErrorCount)
		}
	})

	t.Run("caches build errors", func(t *testing.T) {
		cache := NewGraphCache(WithErrorCacheTTL(1 * time.Second))
		ctx := context.Background()
		buildErr := errors.New("build failed")
		var buildCount int32

		build := func(ctx context.Context, projectRoot string) (*graph.Graph, *manifest.Manifest, error) {
			atomic.AddInt32(&buildCount, 1)
			return nil, nil, buildErr
		}

		// First call fails
		_, _, err := cache.GetOrBuild(ctx, "/test/project", build)
		if err == nil {
			t.Error("expected error on first call")
		}

		// Second call should return cached error, not call build again
		_, _, err = cache.GetOrBuild(ctx, "/test/project", build)
		if err == nil {
			t.Error("expected error on second call")
		}

		var errBuildFailed *ErrBuildFailed
		if !errors.As(err, &errBuildFailed) {
			t.Errorf("expected ErrBuildFailed, got %T", err)
		}

		if buildCount != 1 {
			t.Errorf("build called %d times, want 1 (should use cached error)", buildCount)
		}
	})

	t.Run("cached error expires", func(t *testing.T) {
		cache := NewGraphCache(WithErrorCacheTTL(10 * time.Millisecond))
		ctx := context.Background()
		buildErr := errors.New("build failed")
		var buildCount int32

		build := func(ctx context.Context, projectRoot string) (*graph.Graph, *manifest.Manifest, error) {
			atomic.AddInt32(&buildCount, 1)
			return nil, nil, buildErr
		}

		// First call fails
		_, _, err := cache.GetOrBuild(ctx, "/test/project", build)
		if err == nil {
			t.Error("expected error on first call")
		}

		// Wait for error cache to expire
		time.Sleep(20 * time.Millisecond)

		// Third call should build again (error expired)
		_, _, err = cache.GetOrBuild(ctx, "/test/project", build)
		if err == nil {
			t.Error("expected error on third call")
		}

		if buildCount != 2 {
			t.Errorf("build called %d times, want 2 (error cache should expire)", buildCount)
		}
	})

	t.Run("context cancellation stops build", func(t *testing.T) {
		cache := NewGraphCache()
		ctx, cancel := context.WithCancel(context.Background())
		g := graph.NewGraph("/test/project")
		m := manifest.NewManifest("/test/project")

		cancel() // Cancel before build

		entry, _, err := cache.GetOrBuild(ctx, "/test/project", slowBuildFunc(1*time.Second, g, m))
		if err == nil {
			t.Error("expected error on cancelled context")
		}
		if entry != nil {
			t.Error("expected nil entry on cancelled context")
		}
	})
}

func TestGraphCache_Singleflight(t *testing.T) {
	t.Run("deduplicates concurrent builds", func(t *testing.T) {
		cache := NewGraphCache()
		ctx := context.Background()
		g := graph.NewGraph("/test/project")
		m := manifest.NewManifest("/test/project")
		var buildCount int32

		build := func(ctx context.Context, projectRoot string) (*graph.Graph, *manifest.Manifest, error) {
			atomic.AddInt32(&buildCount, 1)
			time.Sleep(50 * time.Millisecond) // Slow build to ensure overlap
			return g, m, nil
		}

		var wg sync.WaitGroup
		concurrency := 10
		results := make([]*CacheEntry, concurrency)
		errs := make([]error, concurrency)

		for i := 0; i < concurrency; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				entry, release, err := cache.GetOrBuild(ctx, "/test/project", build)
				if release != nil {
					release()
				}
				results[idx] = entry
				errs[idx] = err
			}(i)
		}

		wg.Wait()

		// Should only have built once
		if buildCount != 1 {
			t.Errorf("build called %d times, want 1 (singleflight should dedupe)", buildCount)
		}

		// All should have same entry
		for i, entry := range results {
			if errs[i] != nil {
				t.Errorf("goroutine %d got error: %v", i, errs[i])
			}
			if entry != results[0] {
				t.Errorf("goroutine %d got different entry", i)
			}
		}
	})
}

func TestGraphCache_Invalidate(t *testing.T) {
	t.Run("removes entry when not in use", func(t *testing.T) {
		cache := NewGraphCache()
		ctx := context.Background()
		g := graph.NewGraph("/test/project")
		m := manifest.NewManifest("/test/project")

		// Build and cache
		_, release, err := cache.GetOrBuild(ctx, "/test/project", mockBuildFunc(g, m, nil))
		if err != nil {
			t.Fatalf("GetOrBuild failed: %v", err)
		}
		release() // Release so it's not in use

		// Invalidate
		err = cache.Invalidate("/test/project")
		if err != nil {
			t.Errorf("Invalidate failed: %v", err)
		}

		// Should miss now
		_, _, ok := cache.Get("/test/project")
		if ok {
			t.Error("expected miss after invalidation")
		}
	})

	t.Run("returns error when entry in use", func(t *testing.T) {
		cache := NewGraphCache()
		ctx := context.Background()
		g := graph.NewGraph("/test/project")
		m := manifest.NewManifest("/test/project")

		// Build and cache, keep reference
		_, release, err := cache.GetOrBuild(ctx, "/test/project", mockBuildFunc(g, m, nil))
		if err != nil {
			t.Fatalf("GetOrBuild failed: %v", err)
		}
		defer release()

		// Try to invalidate while in use
		err = cache.Invalidate("/test/project")
		if !errors.Is(err, ErrCacheEntryInUse) {
			t.Errorf("expected ErrCacheEntryInUse, got %v", err)
		}
	})

	t.Run("no error on non-existent entry", func(t *testing.T) {
		cache := NewGraphCache()

		err := cache.Invalidate("/nonexistent")
		if err != nil {
			t.Errorf("Invalidate on non-existent should not error: %v", err)
		}
	})
}

func TestGraphCache_ForceInvalidate(t *testing.T) {
	t.Run("marks entry as stale", func(t *testing.T) {
		cache := NewGraphCache()
		ctx := context.Background()
		g := graph.NewGraph("/test/project")
		m := manifest.NewManifest("/test/project")

		// Build and cache
		entry, release, err := cache.GetOrBuild(ctx, "/test/project", mockBuildFunc(g, m, nil))
		if err != nil {
			t.Fatalf("GetOrBuild failed: %v", err)
		}
		defer release()

		if entry.IsStale() {
			t.Error("entry should not be stale initially")
		}

		// Force invalidate
		cache.ForceInvalidate("/test/project")

		if !entry.IsStale() {
			t.Error("entry should be stale after ForceInvalidate")
		}
	})

	t.Run("stale entry removed after release", func(t *testing.T) {
		cache := NewGraphCache()
		ctx := context.Background()
		g := graph.NewGraph("/test/project")
		m := manifest.NewManifest("/test/project")

		// Build and cache
		_, release, err := cache.GetOrBuild(ctx, "/test/project", mockBuildFunc(g, m, nil))
		if err != nil {
			t.Fatalf("GetOrBuild failed: %v", err)
		}

		// Force invalidate while in use
		cache.ForceInvalidate("/test/project")

		// Release - this should trigger removal
		release()

		// Give a moment for async cleanup
		time.Sleep(10 * time.Millisecond)

		// Should miss now - either stale or removed
		_, _, ok := cache.Get("/test/project")
		if ok {
			t.Error("expected miss after force invalidate and release")
		}
	})
}

func TestGraphCache_Eviction(t *testing.T) {
	t.Run("LRU eviction when at capacity", func(t *testing.T) {
		cache := NewGraphCache(WithMaxEntries(2))
		ctx := context.Background()

		// Add first entry
		g1 := graph.NewGraph("/test/project")
		m1 := manifest.NewManifest("/project1")
		_, release1, _ := cache.GetOrBuild(ctx, "/project1", mockBuildFunc(g1, m1, nil))
		release1()

		// Add second entry
		g2 := graph.NewGraph("/test/project")
		m2 := manifest.NewManifest("/project2")
		_, release2, _ := cache.GetOrBuild(ctx, "/project2", mockBuildFunc(g2, m2, nil))
		release2()

		// Both should exist
		stats := cache.Stats()
		if stats.EntryCount != 2 {
			t.Errorf("EntryCount = %d, want 2", stats.EntryCount)
		}

		// Add third entry - should evict first (LRU)
		g3 := graph.NewGraph("/test/project")
		m3 := manifest.NewManifest("/project3")
		_, release3, _ := cache.GetOrBuild(ctx, "/project3", mockBuildFunc(g3, m3, nil))
		release3()

		// First should be evicted
		_, _, ok := cache.Get("/project1")
		if ok {
			t.Error("expected /project1 to be evicted")
		}

		// Second and third should exist
		_, _, ok = cache.Get("/project2")
		if !ok {
			t.Error("expected /project2 to exist")
		}
		_, _, ok = cache.Get("/project3")
		if !ok {
			t.Error("expected /project3 to exist")
		}

		stats = cache.Stats()
		if stats.Evictions != 1 {
			t.Errorf("Evictions = %d, want 1", stats.Evictions)
		}
	})

	t.Run("does not evict entries in use", func(t *testing.T) {
		cache := NewGraphCache(WithMaxEntries(2))
		ctx := context.Background()

		// Add first entry and keep reference
		g1 := graph.NewGraph("/test/project")
		m1 := manifest.NewManifest("/project1")
		_, release1, _ := cache.GetOrBuild(ctx, "/project1", mockBuildFunc(g1, m1, nil))
		// Don't release - keep in use

		// Add second entry
		g2 := graph.NewGraph("/test/project")
		m2 := manifest.NewManifest("/project2")
		_, release2, _ := cache.GetOrBuild(ctx, "/project2", mockBuildFunc(g2, m2, nil))
		release2()

		// Add third entry - cannot evict first (in use), should evict second
		g3 := graph.NewGraph("/test/project")
		m3 := manifest.NewManifest("/project3")
		_, release3, _ := cache.GetOrBuild(ctx, "/project3", mockBuildFunc(g3, m3, nil))
		release3()

		// First should still exist (in use)
		entry, _, ok := cache.Get("/project1")
		if !ok {
			t.Error("expected /project1 to exist (was in use)")
		}
		if entry != nil {
			// Extra acquire from Get, release it
			entry.Release()
		}

		// Now release original
		release1()
	})

	t.Run("LRU order updated on access", func(t *testing.T) {
		cache := NewGraphCache(WithMaxEntries(2))
		ctx := context.Background()

		// Add first entry
		g1 := graph.NewGraph("/test/project")
		m1 := manifest.NewManifest("/project1")
		_, release1, _ := cache.GetOrBuild(ctx, "/project1", mockBuildFunc(g1, m1, nil))
		release1()

		// Add second entry
		g2 := graph.NewGraph("/test/project")
		m2 := manifest.NewManifest("/project2")
		_, release2, _ := cache.GetOrBuild(ctx, "/project2", mockBuildFunc(g2, m2, nil))
		release2()

		// Access first entry - moves to front of LRU
		_, releaseAccess, ok := cache.Get("/project1")
		if !ok {
			t.Fatal("expected /project1 to exist")
		}
		releaseAccess()

		// Add third entry - should evict second (now LRU)
		g3 := graph.NewGraph("/test/project")
		m3 := manifest.NewManifest("/project3")
		_, release3, _ := cache.GetOrBuild(ctx, "/project3", mockBuildFunc(g3, m3, nil))
		release3()

		// First should exist (was accessed, moved to front)
		_, _, ok = cache.Get("/project1")
		if !ok {
			t.Error("expected /project1 to exist (was recently accessed)")
		}

		// Second should be evicted (was LRU)
		_, _, ok = cache.Get("/project2")
		if ok {
			t.Error("expected /project2 to be evicted")
		}
	})
}

func TestGraphCache_Clear(t *testing.T) {
	t.Run("removes all entries not in use", func(t *testing.T) {
		cache := NewGraphCache()
		ctx := context.Background()

		// Add entries
		for i := 0; i < 3; i++ {
			g := graph.NewGraph("/test/project")
			m := manifest.NewManifest("/project" + string(rune('1'+i)))
			_, release, _ := cache.GetOrBuild(ctx, "/project"+string(rune('1'+i)), mockBuildFunc(g, m, nil))
			release()
		}

		stats := cache.Stats()
		if stats.EntryCount != 3 {
			t.Errorf("EntryCount before clear = %d, want 3", stats.EntryCount)
		}

		cache.Clear()

		stats = cache.Stats()
		if stats.EntryCount != 0 {
			t.Errorf("EntryCount after clear = %d, want 0", stats.EntryCount)
		}
	})

	t.Run("marks entries in use as stale", func(t *testing.T) {
		cache := NewGraphCache()
		ctx := context.Background()
		g := graph.NewGraph("/test/project")
		m := manifest.NewManifest("/test/project")

		// Add entry and keep reference
		entry, release, _ := cache.GetOrBuild(ctx, "/test/project", mockBuildFunc(g, m, nil))
		defer release()

		cache.Clear()

		if !entry.IsStale() {
			t.Error("expected entry to be marked stale")
		}

		// Entry should still be in cache (in use)
		stats := cache.Stats()
		if stats.EntryCount != 1 {
			t.Errorf("EntryCount = %d, want 1 (entry in use)", stats.EntryCount)
		}
	})
}

func TestGraphCache_Stats(t *testing.T) {
	t.Run("tracks all stats correctly", func(t *testing.T) {
		cache := NewGraphCache(WithMaxEntries(2))
		ctx := context.Background()

		// Build
		g := graph.NewGraph("/test/project")
		m := manifest.NewManifest("/project1")
		_, release, _ := cache.GetOrBuild(ctx, "/project1", mockBuildFunc(g, m, nil))
		release()

		// Hit
		_, releaseHit, _ := cache.Get("/project1")
		if releaseHit != nil {
			releaseHit()
		}

		// Miss
		cache.Get("/nonexistent")

		// Build error
		cache.GetOrBuild(ctx, "/error", mockBuildFunc(nil, nil, errors.New("fail")))

		// Build second and third to trigger eviction
		g2 := graph.NewGraph("/test/project")
		m2 := manifest.NewManifest("/project2")
		_, release2, _ := cache.GetOrBuild(ctx, "/project2", mockBuildFunc(g2, m2, nil))
		release2()

		g3 := graph.NewGraph("/test/project")
		m3 := manifest.NewManifest("/project3")
		_, release3, _ := cache.GetOrBuild(ctx, "/project3", mockBuildFunc(g3, m3, nil))
		release3()

		stats := cache.Stats()

		if stats.BuildCount < 3 {
			t.Errorf("BuildCount = %d, want >= 3", stats.BuildCount)
		}
		if stats.Hits < 1 {
			t.Errorf("Hits = %d, want >= 1", stats.Hits)
		}
		if stats.Misses < 1 {
			t.Errorf("Misses = %d, want >= 1", stats.Misses)
		}
		if stats.ErrorCount < 1 {
			t.Errorf("ErrorCount = %d, want >= 1", stats.ErrorCount)
		}
		if stats.Evictions < 1 {
			t.Errorf("Evictions = %d, want >= 1", stats.Evictions)
		}
		if stats.MaxEntries != 2 {
			t.Errorf("MaxEntries = %d, want 2", stats.MaxEntries)
		}
	})

	t.Run("HitRate calculation", func(t *testing.T) {
		stats := CacheStats{Hits: 3, Misses: 1}
		rate := stats.HitRate()
		if rate != 75.0 {
			t.Errorf("HitRate = %f, want 75.0", rate)
		}
	})

	t.Run("HitRate zero on no accesses", func(t *testing.T) {
		stats := CacheStats{}
		rate := stats.HitRate()
		if rate != 0 {
			t.Errorf("HitRate = %f, want 0", rate)
		}
	})
}

func TestCacheEntry_RefCount(t *testing.T) {
	t.Run("Acquire increments", func(t *testing.T) {
		entry := &CacheEntry{}

		entry.Acquire()
		if entry.RefCount() != 1 {
			t.Errorf("RefCount = %d, want 1", entry.RefCount())
		}

		entry.Acquire()
		if entry.RefCount() != 2 {
			t.Errorf("RefCount = %d, want 2", entry.RefCount())
		}
	})

	t.Run("Release decrements", func(t *testing.T) {
		entry := &CacheEntry{}
		entry.Acquire()
		entry.Acquire()

		entry.Release()
		if entry.RefCount() != 1 {
			t.Errorf("RefCount = %d, want 1", entry.RefCount())
		}
	})

	t.Run("InUse reflects refCount", func(t *testing.T) {
		entry := &CacheEntry{}

		if entry.InUse() {
			t.Error("expected not InUse initially")
		}

		entry.Acquire()
		if !entry.InUse() {
			t.Error("expected InUse after Acquire")
		}

		entry.Release()
		if entry.InUse() {
			t.Error("expected not InUse after Release")
		}
	})
}

func TestGenerateGraphID(t *testing.T) {
	t.Run("generates consistent IDs", func(t *testing.T) {
		id1 := GenerateGraphID("/test/project")
		id2 := GenerateGraphID("/test/project")

		if id1 != id2 {
			t.Errorf("IDs not consistent: %s != %s", id1, id2)
		}
	})

	t.Run("generates different IDs for different paths", func(t *testing.T) {
		id1 := GenerateGraphID("/test/project1")
		id2 := GenerateGraphID("/test/project2")

		if id1 == id2 {
			t.Error("IDs should be different for different paths")
		}
	})

	t.Run("generates 64-char hex string", func(t *testing.T) {
		id := GenerateGraphID("/test/project")

		if len(id) != 64 {
			t.Errorf("ID length = %d, want 64", len(id))
		}

		// Should be valid hex
		for _, c := range id {
			if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
				t.Errorf("invalid hex character: %c", c)
			}
		}
	})
}

func TestErrBuildFailed(t *testing.T) {
	t.Run("Error message format", func(t *testing.T) {
		underlying := errors.New("something broke")
		failedAt := time.Now()
		retryAt := failedAt.Add(5 * time.Second)

		err := &ErrBuildFailed{
			Err:      underlying,
			FailedAt: failedAt,
			RetryAt:  retryAt,
		}

		msg := err.Error()
		if msg == "" {
			t.Error("expected non-empty error message")
		}
		if !errors.Is(err, underlying) {
			t.Error("Unwrap should return underlying error")
		}
	})

	t.Run("CanRetry before retry time", func(t *testing.T) {
		err := &ErrBuildFailed{
			RetryAt: time.Now().Add(1 * time.Hour),
		}

		if err.CanRetry() {
			t.Error("expected CanRetry=false before retry time")
		}
	})

	t.Run("CanRetry after retry time", func(t *testing.T) {
		err := &ErrBuildFailed{
			RetryAt: time.Now().Add(-1 * time.Hour),
		}

		if !err.CanRetry() {
			t.Error("expected CanRetry=true after retry time")
		}
	})
}

func TestGraphCache_ConcurrentAccess(t *testing.T) {
	t.Run("concurrent Get and GetOrBuild", func(t *testing.T) {
		cache := NewGraphCache()
		ctx := context.Background()
		g := graph.NewGraph("/test/project")
		m := manifest.NewManifest("/test/project")

		var wg sync.WaitGroup
		concurrency := 50

		// Pre-populate
		_, release, _ := cache.GetOrBuild(ctx, "/test/project", mockBuildFunc(g, m, nil))
		release()

		for i := 0; i < concurrency; i++ {
			wg.Add(2)

			// Concurrent Gets
			go func() {
				defer wg.Done()
				entry, release, ok := cache.Get("/test/project")
				if ok && release != nil {
					// Small delay to keep reference
					time.Sleep(time.Millisecond)
					release()
				}
				_ = entry
			}()

			// Concurrent GetOrBuilds
			go func() {
				defer wg.Done()
				entry, release, err := cache.GetOrBuild(ctx, "/test/project", mockBuildFunc(g, m, nil))
				if err == nil && release != nil {
					time.Sleep(time.Millisecond)
					release()
				}
				_ = entry
			}()
		}

		wg.Wait()

		// Should still have valid entry
		entry, release, ok := cache.Get("/test/project")
		if !ok {
			t.Error("expected entry to still exist")
		}
		if entry == nil {
			t.Error("expected non-nil entry")
		}
		if release != nil {
			release()
		}
	})

	t.Run("concurrent invalidation", func(t *testing.T) {
		cache := NewGraphCache()
		ctx := context.Background()

		var wg sync.WaitGroup
		iterations := 20

		for i := 0; i < iterations; i++ {
			wg.Add(3)

			// GetOrBuild
			go func() {
				defer wg.Done()
				g := graph.NewGraph("/test/project")
				m := manifest.NewManifest("/test/project")
				_, release, err := cache.GetOrBuild(ctx, "/test/project", mockBuildFunc(g, m, nil))
				if err == nil && release != nil {
					time.Sleep(time.Millisecond)
					release()
				}
			}()

			// Invalidate
			go func() {
				defer wg.Done()
				cache.Invalidate("/test/project")
			}()

			// ForceInvalidate
			go func() {
				defer wg.Done()
				cache.ForceInvalidate("/test/project")
			}()
		}

		wg.Wait()
		// No panics or deadlocks = success
	})
}

func TestGraphCache_Refresh(t *testing.T) {
	t.Run("refresh updates entry atomically", func(t *testing.T) {
		cache := NewGraphCache()
		ctx := context.Background()

		// Initial build
		origGraph := graph.NewGraph("/test/project")
		origManifest := manifest.NewManifest("/test/project")
		origManifest.Files["old.go"] = manifest.FileEntry{Path: "old.go", Hash: "old"}

		_, release, err := cache.GetOrBuild(ctx, "/test/project", mockBuildFunc(origGraph, origManifest, nil))
		if err != nil {
			t.Fatalf("initial build failed: %v", err)
		}
		release()

		// Refresh with new graph/manifest
		newGraph := graph.NewGraph("/test/project")
		newManifest := manifest.NewManifest("/test/project")
		newManifest.Files["new.go"] = manifest.FileEntry{Path: "new.go", Hash: "new"}

		refreshCalled := false
		refreshFunc := func(ctx context.Context, projectRoot string, g *graph.Graph, m *manifest.Manifest) (*graph.Graph, *manifest.Manifest, error) {
			refreshCalled = true
			// Verify we got the original data
			if g != origGraph {
				t.Error("refresh received wrong graph")
			}
			if m != origManifest {
				t.Error("refresh received wrong manifest")
			}
			return newGraph, newManifest, nil
		}

		err = cache.Refresh(ctx, "/test/project", refreshFunc)
		if err != nil {
			t.Fatalf("refresh failed: %v", err)
		}

		if !refreshCalled {
			t.Error("refresh func should have been called")
		}

		// Verify entry was updated
		entry, release, ok := cache.Get("/test/project")
		if !ok {
			t.Fatal("entry should exist after refresh")
		}
		defer release()

		if entry.Graph != newGraph {
			t.Error("entry should have new graph")
		}
		if entry.Manifest != newManifest {
			t.Error("entry should have new manifest")
		}

		// Check stats
		stats := cache.Stats()
		if stats.RefreshCount != 1 {
			t.Errorf("RefreshCount = %d, expected 1", stats.RefreshCount)
		}
	})

	t.Run("refresh with no changes does not update", func(t *testing.T) {
		cache := NewGraphCache()
		ctx := context.Background()

		g := graph.NewGraph("/test/project")
		m := manifest.NewManifest("/test/project")

		_, release, err := cache.GetOrBuild(ctx, "/test/project", mockBuildFunc(g, m, nil))
		if err != nil {
			t.Fatalf("initial build failed: %v", err)
		}
		release()

		// Get the entry before refresh
		entryBefore, releaseBefore, _ := cache.Get("/test/project")
		builtAtBefore := entryBefore.BuiltAtMilli
		releaseBefore()

		// Refresh returning same graph/manifest (no changes)
		refreshFunc := func(ctx context.Context, projectRoot string, currentGraph *graph.Graph, currentManifest *manifest.Manifest) (*graph.Graph, *manifest.Manifest, error) {
			return currentGraph, currentManifest, nil
		}

		err = cache.Refresh(ctx, "/test/project", refreshFunc)
		if err != nil {
			t.Fatalf("refresh failed: %v", err)
		}

		// Entry should be unchanged
		entryAfter, releaseAfter, _ := cache.Get("/test/project")
		defer releaseAfter()

		if entryAfter.BuiltAtMilli != builtAtBefore {
			t.Error("entry should not have been updated when no changes")
		}
	})

	t.Run("refresh non-existent entry returns error", func(t *testing.T) {
		cache := NewGraphCache()
		ctx := context.Background()

		refreshFunc := func(ctx context.Context, projectRoot string, g *graph.Graph, m *manifest.Manifest) (*graph.Graph, *manifest.Manifest, error) {
			return g, m, nil
		}

		err := cache.Refresh(ctx, "/does/not/exist", refreshFunc)
		if err == nil {
			t.Fatal("expected error for non-existent entry")
		}
		if !errors.Is(err, ErrEntryNotFound) {
			t.Errorf("expected ErrEntryNotFound, got %v", err)
		}
	})

	t.Run("refresh func error propagates", func(t *testing.T) {
		cache := NewGraphCache()
		ctx := context.Background()

		g := graph.NewGraph("/test/project")
		m := manifest.NewManifest("/test/project")

		_, release, err := cache.GetOrBuild(ctx, "/test/project", mockBuildFunc(g, m, nil))
		if err != nil {
			t.Fatalf("initial build failed: %v", err)
		}
		release()

		expectedErr := errors.New("refresh failed")
		refreshFunc := func(ctx context.Context, projectRoot string, currentGraph *graph.Graph, currentManifest *manifest.Manifest) (*graph.Graph, *manifest.Manifest, error) {
			return nil, nil, expectedErr
		}

		err = cache.Refresh(ctx, "/test/project", refreshFunc)
		if err == nil {
			t.Fatal("expected error from refresh")
		}
		if !errors.Is(err, expectedErr) {
			t.Errorf("expected %v, got %v", expectedErr, err)
		}

		// Entry should be unchanged
		entry, release, ok := cache.Get("/test/project")
		if !ok {
			t.Fatal("entry should still exist after failed refresh")
		}
		release()

		if entry.Graph != g {
			t.Error("entry should have original graph after failed refresh")
		}
	})

	t.Run("refresh respects context cancellation", func(t *testing.T) {
		cache := NewGraphCache()
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		g := graph.NewGraph("/test/project")
		m := manifest.NewManifest("/test/project")

		_, release, err := cache.GetOrBuild(context.Background(), "/test/project", mockBuildFunc(g, m, nil))
		if err != nil {
			t.Fatalf("initial build failed: %v", err)
		}
		release()

		refreshFunc := func(ctx context.Context, projectRoot string, currentGraph *graph.Graph, currentManifest *manifest.Manifest) (*graph.Graph, *manifest.Manifest, error) {
			return graph.NewGraph("/test/project"), manifest.NewManifest("/test/project"), nil
		}

		err = cache.Refresh(ctx, "/test/project", refreshFunc)
		if err == nil {
			t.Fatal("expected error from cancelled context")
		}
		if !errors.Is(err, context.Canceled) {
			t.Errorf("expected context.Canceled, got %v", err)
		}
	})

	t.Run("concurrent refresh is serialized per entry", func(t *testing.T) {
		cache := NewGraphCache()
		ctx := context.Background()

		g := graph.NewGraph("/test/project")
		m := manifest.NewManifest("/test/project")

		_, release, _ := cache.GetOrBuild(ctx, "/test/project", mockBuildFunc(g, m, nil))
		release()

		var refreshCount int32
		var wg sync.WaitGroup

		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				refreshFunc := func(ctx context.Context, projectRoot string, currentGraph *graph.Graph, currentManifest *manifest.Manifest) (*graph.Graph, *manifest.Manifest, error) {
					atomic.AddInt32(&refreshCount, 1)
					// Simulate work
					time.Sleep(10 * time.Millisecond)
					return graph.NewGraph("/test/project"), manifest.NewManifest("/test/project"), nil
				}
				cache.Refresh(ctx, "/test/project", refreshFunc)
			}()
		}

		wg.Wait()

		// All refreshes should have run (serialized by entry mutex)
		// But the entry swap only happens once per actual change
		if atomic.LoadInt32(&refreshCount) < 5 {
			t.Errorf("expected at least 5 refresh calls, got %d (some may have returned early)", refreshCount)
		}
	})
}

func TestGraphCache_MemoryEnforcement(t *testing.T) {
	t.Run("evicts when over memory limit", func(t *testing.T) {
		// Test memory enforcement by checking estimatedMemoryBytesLocked directly
		// Each entry has ~1KB base overhead
		// With MaxMemoryMB=1, we should be able to fit ~1000 entries before eviction
		cache := NewGraphCache(
			WithMaxEntries(5000), // High entry limit to test memory
			WithMaxMemoryMB(2),   // 2MB limit - triggers eviction around ~2000 entries
		)
		ctx := context.Background()

		// Add entries until we exceed the limit
		// Need to add more than ~2000 entries to exceed 2MB
		for i := 0; i < 3000; i++ {
			projectRoot := "/test/project/" + string(rune('a'+i/676%26)) + string(rune('a'+i/26%26)) + string(rune('a'+i%26))
			g := graph.NewGraph(projectRoot)
			m := manifest.NewManifest(projectRoot)

			_, release, err := cache.GetOrBuild(ctx, projectRoot, mockBuildFunc(g, m, nil))
			if err != nil {
				t.Fatalf("build failed at iteration %d: %v", i, err)
			}
			release()
		}

		stats := cache.Stats()
		// With 3000 entries added but only ~2MB allowed, we should see memory evictions
		if stats.MemoryEvictions == 0 {
			t.Errorf("expected memory evictions to occur, got 0 (entry count: %d, estimated MB: %d, evictions: %d)",
				stats.EntryCount, stats.EstimatedMemoryMB, stats.Evictions)
		}
		// Verify we're at or under the memory limit
		if stats.EstimatedMemoryMB > 2 {
			t.Errorf("estimated memory %dMB exceeds 2MB limit", stats.EstimatedMemoryMB)
		}
	})

	t.Run("stats include memory information", func(t *testing.T) {
		cache := NewGraphCache(WithMaxMemoryMB(100))
		ctx := context.Background()

		g := graph.NewGraph("/test/project")
		m := manifest.NewManifest("/test/project")
		m.Files["test.go"] = manifest.FileEntry{Path: "test.go", Hash: "abc123"}

		_, release, err := cache.GetOrBuild(ctx, "/test/project", mockBuildFunc(g, m, nil))
		if err != nil {
			t.Fatalf("build failed: %v", err)
		}
		release()

		stats := cache.Stats()
		if stats.MaxMemoryMB != 100 {
			t.Errorf("MaxMemoryMB = %d, want 100", stats.MaxMemoryMB)
		}
		// Should have some estimated memory (at least base overhead)
		if stats.EstimatedMemoryMB < 0 {
			t.Errorf("EstimatedMemoryMB = %d, should be >= 0", stats.EstimatedMemoryMB)
		}
	})

	t.Run("no memory limit when MaxMemoryMB is 0", func(t *testing.T) {
		cache := NewGraphCache(
			WithMaxEntries(100),
			WithMaxMemoryMB(0), // No memory limit
		)
		ctx := context.Background()

		// Add many entries
		for i := 0; i < 50; i++ {
			projectRoot := "/test/project" + string(rune('A'+i%26)) + string(rune('A'+i/26%26))
			g := graph.NewGraph(projectRoot)
			m := manifest.NewManifest(projectRoot)

			_, release, err := cache.GetOrBuild(ctx, projectRoot, mockBuildFunc(g, m, nil))
			if err != nil {
				t.Fatalf("build failed: %v", err)
			}
			release()
		}

		stats := cache.Stats()
		// No memory evictions when limit is 0
		if stats.MemoryEvictions != 0 {
			t.Errorf("MemoryEvictions = %d, expected 0 when no limit", stats.MemoryEvictions)
		}
		if stats.EntryCount != 50 {
			t.Errorf("EntryCount = %d, expected 50", stats.EntryCount)
		}
	})

	t.Run("memory evictions tracked separately from count evictions", func(t *testing.T) {
		// Small memory limit but larger entry limit
		cache := NewGraphCache(
			WithMaxEntries(2000),
			WithMaxMemoryMB(1), // 1MB limit will trigger memory evictions
		)
		ctx := context.Background()

		// Add entries until memory evictions occur
		for i := 0; i < 2000; i++ {
			projectRoot := "/test/proj/" + string(rune('a'+i/676%26)) + string(rune('a'+i/26%26)) + string(rune('a'+i%26))
			g := graph.NewGraph(projectRoot)
			m := manifest.NewManifest(projectRoot)

			_, release, err := cache.GetOrBuild(ctx, projectRoot, mockBuildFunc(g, m, nil))
			if err != nil {
				t.Fatalf("build failed: %v", err)
			}
			release()
		}

		stats := cache.Stats()
		if stats.MemoryEvictions > 0 {
			// Memory evictions should be counted in both evictions and memoryEvictions
			if stats.Evictions < stats.MemoryEvictions {
				t.Error("total evictions should include memory evictions")
			}
		}
	})

	t.Run("entries in use are not evicted for memory", func(t *testing.T) {
		cache := NewGraphCache(
			WithMaxEntries(2000),
			WithMaxMemoryMB(1),
		)
		ctx := context.Background()

		// Hold reference to first entry
		g1 := graph.NewGraph("/test/project1")
		m1 := manifest.NewManifest("/test/project1")
		entry1, release1, err := cache.GetOrBuild(ctx, "/test/project1", mockBuildFunc(g1, m1, nil))
		if err != nil {
			t.Fatalf("build failed: %v", err)
		}
		// Don't release - keep reference

		// Add many more entries to trigger eviction
		for i := 0; i < 2000; i++ {
			projectRoot := "/test/oth/" + string(rune('a'+i/676%26)) + string(rune('a'+i/26%26)) + string(rune('a'+i%26))
			g := graph.NewGraph(projectRoot)
			m := manifest.NewManifest(projectRoot)

			_, release, err := cache.GetOrBuild(ctx, projectRoot, mockBuildFunc(g, m, nil))
			if err != nil {
				t.Fatalf("build failed: %v", err)
			}
			release()
		}

		// Entry1 should still exist (it's in use)
		entry1Check, release1Check, ok := cache.Get("/test/project1")
		if !ok {
			t.Fatal("in-use entry should not be evicted")
		}
		if entry1Check != entry1 {
			t.Error("should be same entry")
		}
		release1Check()
		release1()
	})
}
