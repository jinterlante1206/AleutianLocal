// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package graph

import (
	"fmt"
	"sync"
	"testing"
)

func TestLRUCache_Basic(t *testing.T) {
	t.Run("get and set", func(t *testing.T) {
		cache := NewLRUCache[string, int](10)

		cache.Set("a", 1)
		cache.Set("b", 2)

		if val, ok := cache.Get("a"); !ok || val != 1 {
			t.Errorf("expected (1, true), got (%d, %v)", val, ok)
		}
		if val, ok := cache.Get("b"); !ok || val != 2 {
			t.Errorf("expected (2, true), got (%d, %v)", val, ok)
		}
	})

	t.Run("get missing key", func(t *testing.T) {
		cache := NewLRUCache[string, int](10)

		val, ok := cache.Get("missing")
		if ok {
			t.Error("expected ok=false for missing key")
		}
		if val != 0 {
			t.Errorf("expected zero value, got %d", val)
		}
	})

	t.Run("update existing key", func(t *testing.T) {
		cache := NewLRUCache[string, int](10)

		cache.Set("a", 1)
		cache.Set("a", 2)

		if val, ok := cache.Get("a"); !ok || val != 2 {
			t.Errorf("expected (2, true), got (%d, %v)", val, ok)
		}
		if cache.Len() != 1 {
			t.Errorf("expected len=1, got %d", cache.Len())
		}
	})

	t.Run("delete", func(t *testing.T) {
		cache := NewLRUCache[string, int](10)

		cache.Set("a", 1)
		if !cache.Delete("a") {
			t.Error("expected delete to return true")
		}
		if _, ok := cache.Get("a"); ok {
			t.Error("expected key to be deleted")
		}
		if cache.Delete("a") {
			t.Error("expected delete of missing key to return false")
		}
	})

	t.Run("purge", func(t *testing.T) {
		cache := NewLRUCache[string, int](10)

		cache.Set("a", 1)
		cache.Set("b", 2)
		cache.Get("a") // Generate a hit

		// Check stats before purge
		hitsBefore, _ := cache.Stats()
		if hitsBefore != 1 {
			t.Errorf("expected 1 hit before purge, got %d", hitsBefore)
		}

		cache.Purge()

		if cache.Len() != 0 {
			t.Errorf("expected len=0 after purge, got %d", cache.Len())
		}

		// Stats should be reset after purge
		hits, misses := cache.Stats()
		if hits != 0 || misses != 0 {
			t.Errorf("expected stats reset after purge, got hits=%d misses=%d", hits, misses)
		}

		// This Get after purge will create a new miss
		if _, ok := cache.Get("a"); ok {
			t.Error("expected key to be purged")
		}

		// Now we should have 1 miss
		_, missesAfter := cache.Stats()
		if missesAfter != 1 {
			t.Errorf("expected 1 miss after Get on purged cache, got %d", missesAfter)
		}
	})
}

func TestLRUCache_Eviction(t *testing.T) {
	t.Run("evicts oldest when full", func(t *testing.T) {
		cache := NewLRUCache[string, int](3)

		cache.Set("a", 1)
		cache.Set("b", 2)
		cache.Set("c", 3)
		cache.Set("d", 4) // Should evict "a"

		if _, ok := cache.Get("a"); ok {
			t.Error("expected 'a' to be evicted")
		}
		if val, ok := cache.Get("b"); !ok || val != 2 {
			t.Errorf("expected 'b' to exist with value 2, got (%d, %v)", val, ok)
		}
		if val, ok := cache.Get("d"); !ok || val != 4 {
			t.Errorf("expected 'd' to exist with value 4, got (%d, %v)", val, ok)
		}
	})

	t.Run("access updates recency", func(t *testing.T) {
		cache := NewLRUCache[string, int](3)

		cache.Set("a", 1)
		cache.Set("b", 2)
		cache.Set("c", 3)

		// Access "a" to make it most recently used
		cache.Get("a")

		// Add "d" - should evict "b" (now oldest)
		cache.Set("d", 4)

		if _, ok := cache.Get("a"); !ok {
			t.Error("expected 'a' to still exist (was accessed)")
		}
		if _, ok := cache.Get("b"); ok {
			t.Error("expected 'b' to be evicted (oldest)")
		}
	})

	t.Run("update preserves slot", func(t *testing.T) {
		cache := NewLRUCache[string, int](3)

		cache.Set("a", 1)
		cache.Set("b", 2)
		cache.Set("c", 3)

		// Update "a" - should not add new entry
		cache.Set("a", 10)

		if cache.Len() != 3 {
			t.Errorf("expected len=3, got %d", cache.Len())
		}

		// Add "d" - should evict "b" (oldest non-updated)
		cache.Set("d", 4)

		if val, ok := cache.Get("a"); !ok || val != 10 {
			t.Errorf("expected 'a'=10, got (%d, %v)", val, ok)
		}
	})
}

func TestLRUCache_Stats(t *testing.T) {
	cache := NewLRUCache[string, int](10)

	cache.Set("a", 1)
	cache.Set("b", 2)

	// Generate hits and misses
	cache.Get("a") // hit
	cache.Get("a") // hit
	cache.Get("b") // hit
	cache.Get("c") // miss
	cache.Get("d") // miss

	hits, misses := cache.Stats()
	if hits != 3 {
		t.Errorf("expected 3 hits, got %d", hits)
	}
	if misses != 2 {
		t.Errorf("expected 2 misses, got %d", misses)
	}
}

func TestLRUCache_ZeroCapacity(t *testing.T) {
	// Zero capacity should use default
	cache := NewLRUCache[string, int](0)

	// Should work with default capacity
	cache.Set("a", 1)
	if val, ok := cache.Get("a"); !ok || val != 1 {
		t.Errorf("expected (1, true), got (%d, %v)", val, ok)
	}
}

func TestLRUCache_Concurrent(t *testing.T) {
	cache := NewLRUCache[string, int](100)

	var wg sync.WaitGroup
	numGoroutines := 10
	numOps := 1000

	// Concurrent writes
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numOps; j++ {
				key := fmt.Sprintf("key-%d-%d", id, j)
				cache.Set(key, j)
			}
		}(i)
	}

	// Concurrent reads
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numOps; j++ {
				key := fmt.Sprintf("key-%d-%d", id, j)
				cache.Get(key)
			}
		}(i)
	}

	wg.Wait()

	// Should not panic or deadlock
	if cache.Len() > 100 {
		t.Errorf("cache exceeded capacity: %d", cache.Len())
	}
}

func TestLRUCache_PointerValues(t *testing.T) {
	type Item struct {
		Value int
	}

	cache := NewLRUCache[string, *Item](10)

	item := &Item{Value: 42}
	cache.Set("item", item)

	retrieved, ok := cache.Get("item")
	if !ok {
		t.Fatal("expected to find item")
	}
	if retrieved.Value != 42 {
		t.Errorf("expected Value=42, got %d", retrieved.Value)
	}

	// Verify it's the same pointer
	if retrieved != item {
		t.Error("expected same pointer")
	}
}

func BenchmarkLRUCache_Set(b *testing.B) {
	cache := NewLRUCache[string, int](1000)
	keys := make([]string, b.N)
	for i := 0; i < b.N; i++ {
		keys[i] = fmt.Sprintf("key-%d", i)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache.Set(keys[i], i)
	}
}

func BenchmarkLRUCache_Get(b *testing.B) {
	cache := NewLRUCache[string, int](1000)
	for i := 0; i < 1000; i++ {
		cache.Set(fmt.Sprintf("key-%d", i), i)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache.Get(fmt.Sprintf("key-%d", i%1000))
	}
}

func BenchmarkLRUCache_Mixed(b *testing.B) {
	cache := NewLRUCache[string, int](1000)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("key-%d", i%2000)
		if i%2 == 0 {
			cache.Set(key, i)
		} else {
			cache.Get(key)
		}
	}
}

// -----------------------------------------------------------------------------
// Additional Edge Case Tests (GR-10 Review)
// -----------------------------------------------------------------------------

func TestLRUCache_NegativeCapacity(t *testing.T) {
	// Negative capacity should use default
	cache := NewLRUCache[string, int](-10)

	// Should work with default capacity (100)
	for i := 0; i < 150; i++ {
		cache.Set(fmt.Sprintf("key-%d", i), i)
	}

	// Should have evicted some entries
	if cache.Len() > 100 {
		t.Errorf("expected max len=100 (default), got %d", cache.Len())
	}

	// Most recent entries should still exist
	if _, ok := cache.Get("key-149"); !ok {
		t.Error("expected most recent key to exist")
	}
}

func TestLRUCache_LargeCapacity(t *testing.T) {
	// Large capacity should work without panic
	cache := NewLRUCache[string, int](100000)

	// Add some entries
	for i := 0; i < 1000; i++ {
		cache.Set(fmt.Sprintf("key-%d", i), i)
	}

	if cache.Len() != 1000 {
		t.Errorf("expected len=1000, got %d", cache.Len())
	}
}

func TestLRUCache_EmptyStringKey(t *testing.T) {
	cache := NewLRUCache[string, int](10)

	// Empty string is a valid key
	cache.Set("", 42)

	val, ok := cache.Get("")
	if !ok {
		t.Error("expected empty string key to work")
	}
	if val != 42 {
		t.Errorf("expected value 42, got %d", val)
	}

	// Delete empty string key
	if !cache.Delete("") {
		t.Error("expected delete of empty key to succeed")
	}

	if _, ok := cache.Get(""); ok {
		t.Error("expected empty key to be deleted")
	}
}

func TestLRUCache_NilPointerValue(t *testing.T) {
	type Item struct {
		Value int
	}

	cache := NewLRUCache[string, *Item](10)

	// Nil pointer is a valid value
	cache.Set("nil-item", nil)

	val, ok := cache.Get("nil-item")
	if !ok {
		t.Error("expected nil pointer value to work")
	}
	if val != nil {
		t.Error("expected nil value")
	}
}

func TestLRUCache_ConcurrentDelete(t *testing.T) {
	cache := NewLRUCache[string, int](100)

	// Pre-populate cache
	for i := 0; i < 100; i++ {
		cache.Set(fmt.Sprintf("key-%d", i), i)
	}

	var wg sync.WaitGroup
	numGoroutines := 10

	// Concurrent deletes
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				cache.Delete(fmt.Sprintf("key-%d", j))
			}
		}(i)
	}

	// Concurrent sets while deleting
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				cache.Set(fmt.Sprintf("new-key-%d-%d", id, j), j)
			}
		}(i)
	}

	wg.Wait()

	// Should not panic or deadlock
	// Cache should have some entries from the new sets
	if cache.Len() == 0 {
		t.Error("expected cache to have some entries after concurrent operations")
	}
}

func TestLRUCache_ConcurrentPurge(t *testing.T) {
	cache := NewLRUCache[string, int](100)

	var wg sync.WaitGroup
	numGoroutines := 10

	// Concurrent operations including purge
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				cache.Set(fmt.Sprintf("key-%d-%d", id, j), j)
				if j%50 == 0 {
					cache.Purge()
				}
				cache.Get(fmt.Sprintf("key-%d-%d", id, j))
			}
		}(i)
	}

	wg.Wait()

	// Should not panic or deadlock
	// Stats should be consistent
	hits, misses := cache.Stats()
	if hits < 0 || misses < 0 {
		t.Errorf("stats should be non-negative: hits=%d, misses=%d", hits, misses)
	}
}

func TestLRUCache_StatsAccuracy(t *testing.T) {
	cache := NewLRUCache[string, int](10)

	// Known pattern of operations
	cache.Set("a", 1)
	cache.Set("b", 2)

	cache.Get("a")       // hit
	cache.Get("a")       // hit
	cache.Get("b")       // hit
	cache.Get("missing") // miss
	cache.Get("missing") // miss
	cache.Get("c")       // miss

	hits, misses := cache.Stats()
	if hits != 3 {
		t.Errorf("expected exactly 3 hits, got %d", hits)
	}
	if misses != 3 {
		t.Errorf("expected exactly 3 misses, got %d", misses)
	}
}

func TestLRUCache_EvictionOrder(t *testing.T) {
	cache := NewLRUCache[string, int](3)

	// Add in order: a, b, c
	cache.Set("a", 1)
	cache.Set("b", 2)
	cache.Set("c", 3)

	// Access in order: c, b (a is now oldest)
	cache.Get("c")
	cache.Get("b")

	// Add d - should evict a
	cache.Set("d", 4)

	if _, ok := cache.Get("a"); ok {
		t.Error("expected 'a' to be evicted (oldest)")
	}

	// Verify b, c, d exist
	if _, ok := cache.Get("b"); !ok {
		t.Error("expected 'b' to exist")
	}
	if _, ok := cache.Get("c"); !ok {
		t.Error("expected 'c' to exist")
	}
	if _, ok := cache.Get("d"); !ok {
		t.Error("expected 'd' to exist")
	}
}

func TestLRUCache_SingleCapacity(t *testing.T) {
	cache := NewLRUCache[string, int](1)

	cache.Set("a", 1)
	if val, ok := cache.Get("a"); !ok || val != 1 {
		t.Errorf("expected (1, true), got (%d, %v)", val, ok)
	}

	// Adding second item evicts first
	cache.Set("b", 2)
	if _, ok := cache.Get("a"); ok {
		t.Error("expected 'a' to be evicted")
	}
	if val, ok := cache.Get("b"); !ok || val != 2 {
		t.Errorf("expected (2, true), got (%d, %v)", val, ok)
	}

	if cache.Len() != 1 {
		t.Errorf("expected len=1, got %d", cache.Len())
	}
}

func TestLRUCache_UpdateDoesNotEvict(t *testing.T) {
	cache := NewLRUCache[string, int](2)

	cache.Set("a", 1)
	cache.Set("b", 2)

	// Update a multiple times - should not cause eviction
	cache.Set("a", 10)
	cache.Set("a", 100)
	cache.Set("a", 1000)

	if cache.Len() != 2 {
		t.Errorf("expected len=2 after updates, got %d", cache.Len())
	}

	if val, ok := cache.Get("a"); !ok || val != 1000 {
		t.Errorf("expected a=1000, got (%d, %v)", val, ok)
	}
	if val, ok := cache.Get("b"); !ok || val != 2 {
		t.Errorf("expected b=2, got (%d, %v)", val, ok)
	}
}

func TestLRUCache_DeleteFromEmpty(t *testing.T) {
	cache := NewLRUCache[string, int](10)

	// Delete from empty cache should not panic
	if cache.Delete("nonexistent") {
		t.Error("expected delete from empty cache to return false")
	}

	if cache.Len() != 0 {
		t.Errorf("expected len=0, got %d", cache.Len())
	}
}

func TestLRUCache_PurgeEmpty(t *testing.T) {
	cache := NewLRUCache[string, int](10)

	// Purge empty cache should not panic
	cache.Purge()

	if cache.Len() != 0 {
		t.Errorf("expected len=0, got %d", cache.Len())
	}

	hits, misses := cache.Stats()
	if hits != 0 || misses != 0 {
		t.Errorf("expected stats=0 after purge, got hits=%d, misses=%d", hits, misses)
	}
}

func TestLRUCache_ConcurrentMixedOperations(t *testing.T) {
	cache := NewLRUCache[string, int](50)

	var wg sync.WaitGroup
	numGoroutines := 20
	opsPerGoroutine := 500

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < opsPerGoroutine; j++ {
				key := fmt.Sprintf("key-%d", j%100)
				switch j % 5 {
				case 0:
					cache.Set(key, j)
				case 1:
					cache.Get(key)
				case 2:
					cache.Delete(key)
				case 3:
					cache.Len()
				case 4:
					cache.Stats()
				}
			}
		}(i)
	}

	wg.Wait()

	// Verify cache is in consistent state
	if cache.Len() < 0 || cache.Len() > 50 {
		t.Errorf("cache len out of bounds: %d", cache.Len())
	}

	hits, misses := cache.Stats()
	if hits < 0 || misses < 0 {
		t.Errorf("stats should be non-negative: hits=%d, misses=%d", hits, misses)
	}
}

// -----------------------------------------------------------------------------
// Eviction Counter Tests (GR-10 Review O-1)
// -----------------------------------------------------------------------------

func TestLRUCache_Evictions(t *testing.T) {
	t.Run("tracks evictions when full", func(t *testing.T) {
		cache := NewLRUCache[string, int](3)

		// Fill the cache
		cache.Set("a", 1)
		cache.Set("b", 2)
		cache.Set("c", 3)

		// No evictions yet
		if evictions := cache.Evictions(); evictions != 0 {
			t.Errorf("expected 0 evictions, got %d", evictions)
		}

		// Add one more - causes eviction
		cache.Set("d", 4)

		if evictions := cache.Evictions(); evictions != 1 {
			t.Errorf("expected 1 eviction, got %d", evictions)
		}

		// Add more
		cache.Set("e", 5)
		cache.Set("f", 6)

		if evictions := cache.Evictions(); evictions != 3 {
			t.Errorf("expected 3 evictions, got %d", evictions)
		}
	})

	t.Run("evictions reset on purge", func(t *testing.T) {
		cache := NewLRUCache[string, int](2)

		cache.Set("a", 1)
		cache.Set("b", 2)
		cache.Set("c", 3) // eviction

		if evictions := cache.Evictions(); evictions != 1 {
			t.Errorf("expected 1 eviction, got %d", evictions)
		}

		cache.Purge()

		if evictions := cache.Evictions(); evictions != 0 {
			t.Errorf("expected 0 evictions after purge, got %d", evictions)
		}
	})

	t.Run("update does not cause eviction", func(t *testing.T) {
		cache := NewLRUCache[string, int](2)

		cache.Set("a", 1)
		cache.Set("b", 2)
		cache.Set("a", 10) // update, not new entry

		if evictions := cache.Evictions(); evictions != 0 {
			t.Errorf("expected 0 evictions for update, got %d", evictions)
		}
	})

	t.Run("concurrent evictions tracked correctly", func(t *testing.T) {
		cache := NewLRUCache[string, int](10)

		var wg sync.WaitGroup
		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				for j := 0; j < 100; j++ {
					cache.Set(fmt.Sprintf("key-%d-%d", id, j), j)
				}
			}(i)
		}
		wg.Wait()

		// Total sets = 1000, capacity = 10, so evictions >= 990
		evictions := cache.Evictions()
		if evictions < 990 {
			t.Errorf("expected at least 990 evictions, got %d", evictions)
		}
	})
}
