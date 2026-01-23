// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

// Package ttl provides time-to-live (TTL) management for documents and sessions
// in the Aleutian RAG system. It implements automatic expiration and cleanup
// for GDPR/CCPA compliance.
package ttl

import (
	"log/slog"
	"time"
)

// =============================================================================
// SEC-002: Query-Time TTL Filtering
// =============================================================================

// TTLQueryFilter provides query-time defense-in-depth by checking document
// expiration before results are returned to users.
//
// # Description
//
// The background TTL scheduler runs periodically (default: 1 hour) to clean up
// expired documents. Between cleanup cycles, expired documents could still be
// returned in query results. TTLQueryFilter provides a safety net by checking
// expiration at query time.
//
// This is defense-in-depth: the scheduler handles bulk cleanup, the filter
// prevents expired data from leaking through queries.
//
// # Security Context
//
// Without query-time filtering, expired documents remain accessible for up to
// one cleanup interval after their TTL. This violates GDPR Article 5(1)(e)
// "storage limitation" principle.
//
// # Thread Safety
//
// All methods are safe for concurrent use (stateless).
type TTLQueryFilter interface {
	// IsExpired checks if a document has passed its TTL.
	//
	// # Description
	//
	// Returns true if the document's TTL has passed, accounting for
	// configured clock skew tolerance. Documents with ttlExpiresAt = 0
	// are considered non-expiring and never return true.
	//
	// # Inputs
	//
	//   - ttlExpiresAt: Unix milliseconds expiration timestamp. 0 = never expires.
	//
	// # Outputs
	//
	//   - bool: True if the document is expired and should be filtered out.
	//
	// # Example
	//
	//   if filter.IsExpired(doc.TTLExpiresAt) {
	//       continue // Skip this document
	//   }
	IsExpired(ttlExpiresAt int64) bool

	// FilterCount filters a slice of TTL values and returns the count of expired items.
	//
	// # Description
	//
	// Given a slice of ttl_expires_at values, returns indices of non-expired items
	// and the count of expired items. Useful for batch filtering without knowing
	// the concrete document type.
	//
	// # Inputs
	//
	//   - expirations: Slice of Unix millisecond expiration timestamps.
	//
	// # Outputs
	//
	//   - validIndices: Indices of non-expired items.
	//   - expiredCount: Number of expired items filtered out.
	FilterCount(expirations []int64) (validIndices []int, expiredCount int)
}

// ttlQueryFilter implements TTLQueryFilter with configurable clock skew tolerance.
//
// # Description
//
// Stateless filter that checks document expiration at query time. Includes
// a clock skew tolerance to avoid filtering documents that are about to
// expire due to minor clock differences between services.
//
// # Fields
//
//   - clockSkewTolerance: Grace period to account for clock drift.
//
// # Thread Safety
//
// Stateless and safe for concurrent use.
type ttlQueryFilter struct {
	clockSkewTolerance time.Duration
}

// NewTTLQueryFilter creates a new query-time TTL filter.
//
// # Description
//
// Creates a filter that checks document expiration at query time.
// Includes configurable clock skew tolerance for distributed systems.
//
// # Inputs
//
//   - clockSkewTolerance: Grace period for clock drift. If 0, defaults to 5 seconds.
//
// # Outputs
//
//   - TTLQueryFilter: Ready to filter query results.
//
// # Example
//
//	filter := NewTTLQueryFilter(5 * time.Second)
//	if filter.IsExpired(doc.TTLExpiresAt) {
//	    // Don't return this document
//	}
//
// # Limitations
//
//   - Cannot detect documents that should have expired but have TTL=0 (never set).
//   - Tolerance should be small (seconds) to minimize compliance window.
func NewTTLQueryFilter(clockSkewTolerance time.Duration) TTLQueryFilter {
	if clockSkewTolerance == 0 {
		clockSkewTolerance = 5 * time.Second
	}
	return &ttlQueryFilter{
		clockSkewTolerance: clockSkewTolerance,
	}
}

// IsExpired checks if a document has passed its TTL.
//
// # Description
//
// Returns true if the document's TTL has passed. Documents with
// ttlExpiresAt = 0 never expire. The clock skew tolerance is subtracted
// from current time to provide a small grace period.
//
// # Inputs
//
//   - ttlExpiresAt: Unix milliseconds expiration timestamp. 0 = never expires.
//
// # Outputs
//
//   - bool: True if expired and should be filtered out.
func (f *ttlQueryFilter) IsExpired(ttlExpiresAt int64) bool {
	if ttlExpiresAt == 0 {
		return false // Never expires
	}
	// Subtract tolerance from current time to provide a small grace period.
	// This prevents filtering documents that are just barely expired due to
	// minor clock differences between Weaviate and this service.
	currentTimeMs := time.Now().Add(-f.clockSkewTolerance).UnixMilli()
	return ttlExpiresAt < currentTimeMs
}

// FilterCount filters a slice of TTL expiration values.
//
// # Description
//
// Given a slice of ttl_expires_at values, returns the indices of items
// that are still valid (not expired) and the count of expired items.
// The caller can use the indices to build a filtered result set.
//
// # Inputs
//
//   - expirations: Slice of Unix millisecond expiration timestamps.
//
// # Outputs
//
//   - validIndices: Indices of non-expired items in the original slice.
//   - expiredCount: Number of expired items.
//
// # Example
//
//	expirations := make([]int64, len(docs))
//	for i, doc := range docs {
//	    expirations[i] = doc.TTLExpiresAt
//	}
//	valid, expired := filter.FilterCount(expirations)
//	if expired > 0 {
//	    slog.Debug("Filtered expired docs", "count", expired)
//	}
func (f *ttlQueryFilter) FilterCount(expirations []int64) (validIndices []int, expiredCount int) {
	validIndices = make([]int, 0, len(expirations))
	for i, exp := range expirations {
		if f.IsExpired(exp) {
			expiredCount++
		} else {
			validIndices = append(validIndices, i)
		}
	}

	if expiredCount > 0 {
		slog.Debug("ttl.query_filter: filtered expired items",
			"expired_count", expiredCount,
			"valid_count", len(validIndices),
		)
	}

	return validIndices, expiredCount
}
