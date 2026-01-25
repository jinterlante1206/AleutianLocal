// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package ttl

import (
	"testing"
	"time"
)

// =============================================================================
// SEC-002: Query-Time TTL Filtering Tests
// =============================================================================

// TestTTLQueryFilter_IsExpired_ZeroMeansNever tests that documents with
// ttl_expires_at = 0 are never considered expired.
func TestTTLQueryFilter_IsExpired_ZeroMeansNever(t *testing.T) {
	filter := NewTTLQueryFilter(5 * time.Second)

	if filter.IsExpired(0) {
		t.Error("ttl_expires_at = 0 should never expire")
	}
}

// TestTTLQueryFilter_IsExpired_PastTime tests that documents with
// ttl_expires_at in the past are considered expired.
func TestTTLQueryFilter_IsExpired_PastTime(t *testing.T) {
	filter := NewTTLQueryFilter(5 * time.Second)

	// Set expiry to 1 minute ago (well past the 5s tolerance)
	pastTime := time.Now().Add(-1 * time.Minute).UnixMilli()

	if !filter.IsExpired(pastTime) {
		t.Error("Document with TTL 1 minute in the past should be expired")
	}
}

// TestTTLQueryFilter_IsExpired_FutureTime tests that documents with
// ttl_expires_at in the future are not considered expired.
func TestTTLQueryFilter_IsExpired_FutureTime(t *testing.T) {
	filter := NewTTLQueryFilter(5 * time.Second)

	// Set expiry to 1 hour in the future
	futureTime := time.Now().Add(1 * time.Hour).UnixMilli()

	if filter.IsExpired(futureTime) {
		t.Error("Document with TTL 1 hour in the future should not be expired")
	}
}

// TestTTLQueryFilter_IsExpired_WithinTolerance tests that documents that
// just barely expired (within clock skew tolerance) are NOT filtered.
func TestTTLQueryFilter_IsExpired_WithinTolerance(t *testing.T) {
	tolerance := 5 * time.Second
	filter := NewTTLQueryFilter(tolerance)

	// Set expiry to 2 seconds ago (within 5s tolerance)
	recentPast := time.Now().Add(-2 * time.Second).UnixMilli()

	if filter.IsExpired(recentPast) {
		t.Error("Document expired 2s ago should not be filtered with 5s tolerance")
	}
}

// TestTTLQueryFilter_IsExpired_BeyondTolerance tests that documents expired
// beyond the clock skew tolerance are filtered.
func TestTTLQueryFilter_IsExpired_BeyondTolerance(t *testing.T) {
	tolerance := 5 * time.Second
	filter := NewTTLQueryFilter(tolerance)

	// Set expiry to 10 seconds ago (beyond 5s tolerance)
	olderPast := time.Now().Add(-10 * time.Second).UnixMilli()

	if !filter.IsExpired(olderPast) {
		t.Error("Document expired 10s ago should be filtered with 5s tolerance")
	}
}

// TestTTLQueryFilter_FilterCount_NoExpired tests that FilterCount returns
// all indices when nothing is expired.
func TestTTLQueryFilter_FilterCount_NoExpired(t *testing.T) {
	filter := NewTTLQueryFilter(5 * time.Second)

	futureTime := time.Now().Add(1 * time.Hour).UnixMilli()
	expirations := []int64{0, futureTime, 0, futureTime}

	valid, expired := filter.FilterCount(expirations)

	if expired != 0 {
		t.Errorf("Expected 0 expired, got %d", expired)
	}

	if len(valid) != 4 {
		t.Errorf("Expected 4 valid indices, got %d", len(valid))
	}
}

// TestTTLQueryFilter_FilterCount_AllExpired tests that FilterCount returns
// no valid indices when everything is expired.
func TestTTLQueryFilter_FilterCount_AllExpired(t *testing.T) {
	filter := NewTTLQueryFilter(5 * time.Second)

	pastTime := time.Now().Add(-1 * time.Hour).UnixMilli()
	expirations := []int64{pastTime, pastTime, pastTime}

	valid, expired := filter.FilterCount(expirations)

	if expired != 3 {
		t.Errorf("Expected 3 expired, got %d", expired)
	}

	if len(valid) != 0 {
		t.Errorf("Expected 0 valid indices, got %d", len(valid))
	}
}

// TestTTLQueryFilter_FilterCount_Mixed tests that FilterCount correctly
// identifies valid and expired items in a mixed set.
func TestTTLQueryFilter_FilterCount_Mixed(t *testing.T) {
	filter := NewTTLQueryFilter(5 * time.Second)

	pastTime := time.Now().Add(-1 * time.Hour).UnixMilli()
	futureTime := time.Now().Add(1 * time.Hour).UnixMilli()

	expirations := []int64{
		0,          // never expires (valid)
		pastTime,   // expired
		futureTime, // not expired (valid)
		pastTime,   // expired
		0,          // never expires (valid)
	}

	valid, expired := filter.FilterCount(expirations)

	if expired != 2 {
		t.Errorf("Expected 2 expired, got %d", expired)
	}

	if len(valid) != 3 {
		t.Errorf("Expected 3 valid indices, got %d", len(valid))
	}

	// Verify the correct indices were returned
	expectedIndices := []int{0, 2, 4}
	for i, idx := range valid {
		if idx != expectedIndices[i] {
			t.Errorf("Expected valid index %d at position %d, got %d", expectedIndices[i], i, idx)
		}
	}
}

// TestTTLQueryFilter_FilterCount_EmptySlice tests that FilterCount handles
// empty input gracefully.
func TestTTLQueryFilter_FilterCount_EmptySlice(t *testing.T) {
	filter := NewTTLQueryFilter(5 * time.Second)

	valid, expired := filter.FilterCount([]int64{})

	if expired != 0 {
		t.Errorf("Expected 0 expired for empty input, got %d", expired)
	}

	if len(valid) != 0 {
		t.Errorf("Expected 0 valid for empty input, got %d", len(valid))
	}
}

// TestTTLQueryFilter_DefaultTolerance tests that zero tolerance defaults to 5s.
func TestTTLQueryFilter_DefaultTolerance(t *testing.T) {
	filter := NewTTLQueryFilter(0) // Should default to 5s

	// Set expiry to 3 seconds ago (within default 5s tolerance)
	recentPast := time.Now().Add(-3 * time.Second).UnixMilli()

	if filter.IsExpired(recentPast) {
		t.Error("3s expired should not be filtered with default 5s tolerance")
	}

	// Set expiry to 10 seconds ago (beyond default 5s tolerance)
	olderPast := time.Now().Add(-10 * time.Second).UnixMilli()

	if !filter.IsExpired(olderPast) {
		t.Error("10s expired should be filtered with default 5s tolerance")
	}
}
