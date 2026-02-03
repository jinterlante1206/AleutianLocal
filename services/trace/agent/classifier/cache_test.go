// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.

package classifier

import (
	"sync"
	"testing"
	"time"
)

func TestClassificationCache_BasicOperations(t *testing.T) {
	cache := NewClassificationCache(10*time.Minute, 100)

	t.Run("set and get", func(t *testing.T) {
		result := &ClassificationResult{
			IsAnalytical: true,
			Tool:         "find_entry_points",
		}

		cache.Set("test query", "toolsHash", result)

		cached, ok := cache.Get("test query", "toolsHash")
		if !ok {
			t.Fatal("expected cache hit")
		}
		if !cached.Cached {
			t.Error("expected Cached=true")
		}
		if cached.Tool != "find_entry_points" {
			t.Errorf("expected tool=find_entry_points, got %s", cached.Tool)
		}
	})

	t.Run("miss for different query", func(t *testing.T) {
		_, ok := cache.Get("different query", "toolsHash")
		if ok {
			t.Error("expected cache miss")
		}
	})

	t.Run("miss for different tools hash", func(t *testing.T) {
		_, ok := cache.Get("test query", "differentHash")
		if ok {
			t.Error("expected cache miss for different tools hash")
		}
	})
}

func TestClassificationCache_TTLExpiration(t *testing.T) {
	cache := NewClassificationCache(50*time.Millisecond, 100)

	result := &ClassificationResult{IsAnalytical: true}
	cache.Set("query", "hash", result)

	// Should hit immediately
	if _, ok := cache.Get("query", "hash"); !ok {
		t.Error("expected cache hit before expiration")
	}

	// Wait for expiration
	time.Sleep(60 * time.Millisecond)

	// Should miss after expiration
	if _, ok := cache.Get("query", "hash"); ok {
		t.Error("expected cache miss after expiration")
	}
}

func TestClassificationCache_LRUEviction(t *testing.T) {
	cache := NewClassificationCache(10*time.Minute, 3) // Max 3 entries

	// Add 3 entries
	cache.Set("query1", "hash", &ClassificationResult{Tool: "tool1"})
	cache.Set("query2", "hash", &ClassificationResult{Tool: "tool2"})
	cache.Set("query3", "hash", &ClassificationResult{Tool: "tool3"})

	// Access query1 to make it recently used
	cache.Get("query1", "hash")

	// Add a 4th entry - should evict query2 (LRU)
	cache.Set("query4", "hash", &ClassificationResult{Tool: "tool4"})

	// query1, query3, query4 should exist
	if _, ok := cache.Get("query1", "hash"); !ok {
		t.Error("query1 should exist (was accessed recently)")
	}
	if _, ok := cache.Get("query3", "hash"); !ok {
		t.Error("query3 should exist")
	}
	if _, ok := cache.Get("query4", "hash"); !ok {
		t.Error("query4 should exist")
	}

	// query2 should be evicted
	if _, ok := cache.Get("query2", "hash"); ok {
		t.Error("query2 should be evicted (LRU)")
	}
}

func TestClassificationCache_Update(t *testing.T) {
	cache := NewClassificationCache(10*time.Minute, 100)

	// Initial set
	cache.Set("query", "hash", &ClassificationResult{Tool: "tool1"})

	// Update
	cache.Set("query", "hash", &ClassificationResult{Tool: "tool2"})

	// Size should still be 1
	if cache.Size() != 1 {
		t.Errorf("expected size 1, got %d", cache.Size())
	}

	// Should get updated value
	result, _ := cache.Get("query", "hash")
	if result.Tool != "tool2" {
		t.Errorf("expected tool2, got %s", result.Tool)
	}
}

func TestClassificationCache_Delete(t *testing.T) {
	cache := NewClassificationCache(10*time.Minute, 100)

	cache.Set("query", "hash", &ClassificationResult{Tool: "tool1"})
	cache.Delete("query", "hash")

	if _, ok := cache.Get("query", "hash"); ok {
		t.Error("expected cache miss after delete")
	}
}

func TestClassificationCache_Clear(t *testing.T) {
	cache := NewClassificationCache(10*time.Minute, 100)

	cache.Set("query1", "hash", &ClassificationResult{})
	cache.Set("query2", "hash", &ClassificationResult{})
	cache.Clear()

	if cache.Size() != 0 {
		t.Errorf("expected size 0 after clear, got %d", cache.Size())
	}
}

func TestClassificationCache_Metrics(t *testing.T) {
	cache := NewClassificationCache(10*time.Minute, 100)

	cache.Set("query", "hash", &ClassificationResult{})

	// Miss
	cache.Get("nonexistent", "hash")

	// Hit
	cache.Get("query", "hash")

	// Hit
	cache.Get("query", "hash")

	if cache.Hits() != 2 {
		t.Errorf("expected 2 hits, got %d", cache.Hits())
	}
	if cache.Misses() != 1 {
		t.Errorf("expected 1 miss, got %d", cache.Misses())
	}

	hitRate := cache.HitRate()
	expectedRate := 2.0 / 3.0
	if hitRate < expectedRate-0.01 || hitRate > expectedRate+0.01 {
		t.Errorf("expected hit rate ~%.2f, got %.2f", expectedRate, hitRate)
	}

	cache.ResetMetrics()
	if cache.Hits() != 0 || cache.Misses() != 0 {
		t.Error("metrics should be 0 after reset")
	}
}

func TestClassificationCache_ConcurrentAccess(t *testing.T) {
	cache := NewClassificationCache(10*time.Minute, 1000)

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			key := string(rune('a' + n%26))
			cache.Set(key, "hash", &ClassificationResult{Tool: key})
			cache.Get(key, "hash")
		}(i)
	}
	wg.Wait()

	// Should not panic
	_ = cache.Size()
	_ = cache.HitRate()
}

func TestClassificationCache_NilResult(t *testing.T) {
	cache := NewClassificationCache(10*time.Minute, 100)

	// Setting nil should be a no-op
	cache.Set("query", "hash", nil)

	if cache.Size() != 0 {
		t.Error("nil result should not be cached")
	}
}

func TestClassificationCache_ImmutableCopy(t *testing.T) {
	cache := NewClassificationCache(10*time.Minute, 100)

	original := &ClassificationResult{
		Tool: "original",
	}
	cache.Set("query", "hash", original)

	// Modify original after caching
	original.Tool = "modified"

	// Get from cache
	cached, _ := cache.Get("query", "hash")

	// Cached should have original value (it's a copy)
	if cached.Tool != "original" {
		t.Error("cached value should be independent of original")
	}
}

func TestComputeToolsHash(t *testing.T) {
	t.Run("consistent hash", func(t *testing.T) {
		tools := []string{"tool1", "tool2", "tool3"}
		hash1 := ComputeToolsHash(tools)
		hash2 := ComputeToolsHash(tools)
		if hash1 != hash2 {
			t.Error("hash should be consistent")
		}
	})

	t.Run("different tools different hash", func(t *testing.T) {
		hash1 := ComputeToolsHash([]string{"tool1", "tool2"})
		hash2 := ComputeToolsHash([]string{"tool1", "tool3"})
		if hash1 == hash2 {
			t.Error("different tools should produce different hash")
		}
	})

	t.Run("empty tools", func(t *testing.T) {
		hash := ComputeToolsHash([]string{})
		if hash == "" {
			t.Error("empty tools should still produce a hash")
		}
	})
}
