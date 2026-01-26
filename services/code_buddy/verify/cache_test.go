// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package verify

import (
	"sync"
	"testing"
	"time"
)

func TestNewVerificationCache(t *testing.T) {
	t.Run("default TTL", func(t *testing.T) {
		cache := NewVerificationCache()

		if cache == nil {
			t.Fatal("NewVerificationCache returned nil")
		}
		if cache.TTL() != DefaultVerificationTTL {
			t.Errorf("TTL = %v, want %v", cache.TTL(), DefaultVerificationTTL)
		}
		if cache.Size() != 0 {
			t.Errorf("Size = %d, want 0", cache.Size())
		}
	})

	t.Run("custom TTL", func(t *testing.T) {
		cache := NewVerificationCache(WithCacheTTL(1 * time.Second))

		if cache.TTL() != 1*time.Second {
			t.Errorf("TTL = %v, want 1s", cache.TTL())
		}
	})

	t.Run("invalid TTL ignored", func(t *testing.T) {
		cache := NewVerificationCache(WithCacheTTL(-1 * time.Second))

		if cache.TTL() != DefaultVerificationTTL {
			t.Errorf("TTL = %v, want default %v", cache.TTL(), DefaultVerificationTTL)
		}
	})
}

func TestVerificationCache_NeedsVerification(t *testing.T) {
	t.Run("uncached file needs verification", func(t *testing.T) {
		cache := NewVerificationCache()

		if !cache.NeedsVerification("test.go") {
			t.Error("expected NeedsVerification=true for uncached file")
		}
	})

	t.Run("recently verified file does not need verification", func(t *testing.T) {
		cache := NewVerificationCache(WithCacheTTL(1 * time.Minute))

		cache.MarkVerified("test.go")

		if cache.NeedsVerification("test.go") {
			t.Error("expected NeedsVerification=false for recently verified file")
		}
	})

	t.Run("expired verification needs re-verification", func(t *testing.T) {
		cache := NewVerificationCache(WithCacheTTL(10 * time.Millisecond))

		cache.MarkVerified("test.go")

		// Wait for TTL to expire
		time.Sleep(20 * time.Millisecond)

		if !cache.NeedsVerification("test.go") {
			t.Error("expected NeedsVerification=true after TTL expiry")
		}
	})
}

func TestVerificationCache_MarkVerified(t *testing.T) {
	t.Run("marks file as verified", func(t *testing.T) {
		cache := NewVerificationCache()

		cache.MarkVerified("test.go")

		if cache.Size() != 1 {
			t.Errorf("Size = %d, want 1", cache.Size())
		}
	})

	t.Run("updates existing entry", func(t *testing.T) {
		cache := NewVerificationCache(WithCacheTTL(1 * time.Minute))

		cache.MarkVerified("test.go")
		time.Sleep(10 * time.Millisecond)
		cache.MarkVerified("test.go")

		// Should still just be one entry
		if cache.Size() != 1 {
			t.Errorf("Size = %d, want 1", cache.Size())
		}

		// Should not need verification (updated timestamp)
		if cache.NeedsVerification("test.go") {
			t.Error("expected NeedsVerification=false after re-marking")
		}
	})
}

func TestVerificationCache_MarkVerifiedBatch(t *testing.T) {
	t.Run("marks multiple files", func(t *testing.T) {
		cache := NewVerificationCache()

		paths := []string{"a.go", "b.go", "c.go"}
		cache.MarkVerifiedBatch(paths)

		if cache.Size() != 3 {
			t.Errorf("Size = %d, want 3", cache.Size())
		}

		for _, path := range paths {
			if cache.NeedsVerification(path) {
				t.Errorf("expected NeedsVerification=false for %s", path)
			}
		}
	})

	t.Run("empty batch does nothing", func(t *testing.T) {
		cache := NewVerificationCache()

		cache.MarkVerifiedBatch(nil)
		cache.MarkVerifiedBatch([]string{})

		if cache.Size() != 0 {
			t.Errorf("Size = %d, want 0", cache.Size())
		}
	})
}

func TestVerificationCache_Invalidate(t *testing.T) {
	t.Run("invalidates single file", func(t *testing.T) {
		cache := NewVerificationCache()

		cache.MarkVerified("test.go")
		cache.Invalidate("test.go")

		if cache.Size() != 0 {
			t.Errorf("Size = %d, want 0", cache.Size())
		}
		if !cache.NeedsVerification("test.go") {
			t.Error("expected NeedsVerification=true after invalidation")
		}
	})

	t.Run("invalidating non-existent file is no-op", func(t *testing.T) {
		cache := NewVerificationCache()

		cache.MarkVerified("a.go")
		cache.Invalidate("b.go") // doesn't exist

		if cache.Size() != 1 {
			t.Errorf("Size = %d, want 1", cache.Size())
		}
	})
}

func TestVerificationCache_InvalidateAll(t *testing.T) {
	t.Run("clears all entries", func(t *testing.T) {
		cache := NewVerificationCache()

		cache.MarkVerifiedBatch([]string{"a.go", "b.go", "c.go"})
		cache.InvalidateAll()

		if cache.Size() != 0 {
			t.Errorf("Size = %d, want 0", cache.Size())
		}
	})
}

func TestVerificationCache_Cleanup(t *testing.T) {
	t.Run("removes expired entries", func(t *testing.T) {
		cache := NewVerificationCache(WithCacheTTL(10 * time.Millisecond))

		cache.MarkVerified("old.go")
		time.Sleep(20 * time.Millisecond)
		cache.MarkVerified("new.go")

		removed := cache.Cleanup()

		if removed != 1 {
			t.Errorf("removed = %d, want 1", removed)
		}
		if cache.Size() != 1 {
			t.Errorf("Size = %d, want 1", cache.Size())
		}
		if !cache.NeedsVerification("old.go") {
			t.Error("old.go should need verification after cleanup")
		}
		if cache.NeedsVerification("new.go") {
			t.Error("new.go should not need verification after cleanup")
		}
	})
}

func TestVerificationCache_Concurrency(t *testing.T) {
	t.Run("concurrent mark and check is safe", func(t *testing.T) {
		cache := NewVerificationCache(WithCacheTTL(1 * time.Minute))

		var wg sync.WaitGroup
		concurrency := 50
		iterations := 100

		for i := 0; i < concurrency; i++ {
			wg.Add(2)

			// Concurrent marks
			go func(id int) {
				defer wg.Done()
				for j := 0; j < iterations; j++ {
					cache.MarkVerified("test.go")
				}
			}(i)

			// Concurrent checks
			go func(id int) {
				defer wg.Done()
				for j := 0; j < iterations; j++ {
					cache.NeedsVerification("test.go")
				}
			}(i)
		}

		wg.Wait()
		// No panics = success
	})

	t.Run("concurrent invalidate is safe", func(t *testing.T) {
		cache := NewVerificationCache()

		var wg sync.WaitGroup
		iterations := 100

		for i := 0; i < iterations; i++ {
			wg.Add(3)

			go func() {
				defer wg.Done()
				cache.MarkVerified("test.go")
			}()

			go func() {
				defer wg.Done()
				cache.Invalidate("test.go")
			}()

			go func() {
				defer wg.Done()
				cache.InvalidateAll()
			}()
		}

		wg.Wait()
		// No panics = success
	})
}
