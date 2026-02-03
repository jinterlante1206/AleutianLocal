// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package patterns

import (
	"hash/fnv"
	"sync"
)

// LSHIndex provides O(n log n) near-duplicate detection using locality-sensitive hashing.
//
// # Description
//
// LSHIndex uses banded MinHash for approximate nearest neighbor search.
// Instead of comparing every pair of fingerprints (O(n²)), it hashes
// fingerprints into buckets where similar items are likely to collide.
//
// The index divides MinHash signatures into bands. Two fingerprints
// are candidates if they share at least one band. More bands mean
// higher recall but more false positives.
//
// # Thread Safety
//
// This type is safe for concurrent use.
type LSHIndex struct {
	numBands     int
	rowsPerBand  int
	buckets      []map[uint64][]string // Per-band buckets: hash → symbol IDs
	fingerprints map[string]*CodeFingerprint
	mu           sync.RWMutex
}

// NewLSHIndex creates an LSH index with the specified parameters.
//
// # Description
//
// Creates an index configured for the desired similarity threshold.
// Higher numBands increases recall (finds more similar pairs) but
// also increases false positives. A good rule of thumb:
//
//   - For 80% similarity threshold: numBands=20, rowsPerBand=5
//   - For 90% similarity threshold: numBands=50, rowsPerBand=2
//
// The signature length must be numBands * rowsPerBand.
//
// # Inputs
//
//   - numBands: Number of hash bands.
//   - rowsPerBand: Rows per band.
//
// # Outputs
//
//   - *LSHIndex: The configured index.
//
// # Example
//
//	index := NewLSHIndex(20, 5) // For 100-element MinHash signatures
//	index.Add(fingerprint)
//	candidates := index.Query(queryFingerprint)
func NewLSHIndex(numBands, rowsPerBand int) *LSHIndex {
	buckets := make([]map[uint64][]string, numBands)
	for i := range buckets {
		buckets[i] = make(map[uint64][]string)
	}

	return &LSHIndex{
		numBands:     numBands,
		rowsPerBand:  rowsPerBand,
		buckets:      buckets,
		fingerprints: make(map[string]*CodeFingerprint),
	}
}

// Add adds a fingerprint to the index.
//
// # Description
//
// Hashes the fingerprint's MinHash signature into band buckets.
// If a fingerprint with the same SymbolID already exists, it is replaced.
//
// # Inputs
//
//   - fp: The fingerprint to add.
//
// # Errors
//
// Silently ignores nil fingerprints or those with empty signatures.
func (l *LSHIndex) Add(fp *CodeFingerprint) {
	if fp == nil || len(fp.MinHashSig) == 0 {
		return
	}

	expectedLen := l.numBands * l.rowsPerBand
	if len(fp.MinHashSig) < expectedLen {
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	// Remove old entry if exists
	if old, exists := l.fingerprints[fp.SymbolID]; exists {
		l.removeFromBuckets(old)
	}

	// Store fingerprint
	l.fingerprints[fp.SymbolID] = fp

	// Add to band buckets
	for band := 0; band < l.numBands; band++ {
		bandHash := l.hashBand(fp.MinHashSig, band)
		l.buckets[band][bandHash] = append(l.buckets[band][bandHash], fp.SymbolID)
	}
}

// Remove removes a fingerprint from the index.
//
// # Inputs
//
//   - symbolID: The ID of the fingerprint to remove.
func (l *LSHIndex) Remove(symbolID string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	fp, exists := l.fingerprints[symbolID]
	if !exists {
		return
	}

	l.removeFromBuckets(fp)
	delete(l.fingerprints, symbolID)
}

// removeFromBuckets removes a fingerprint from all band buckets.
// Caller must hold the write lock.
func (l *LSHIndex) removeFromBuckets(fp *CodeFingerprint) {
	for band := 0; band < l.numBands; band++ {
		bandHash := l.hashBand(fp.MinHashSig, band)
		bucket := l.buckets[band][bandHash]

		// Filter out this symbol
		filtered := make([]string, 0, len(bucket))
		for _, id := range bucket {
			if id != fp.SymbolID {
				filtered = append(filtered, id)
			}
		}

		if len(filtered) > 0 {
			l.buckets[band][bandHash] = filtered
		} else {
			delete(l.buckets[band], bandHash)
		}
	}
}

// Query finds candidate matches for a fingerprint.
//
// # Description
//
// Returns symbol IDs of fingerprints that share at least one band
// with the query fingerprint. These are candidates that should be
// verified with actual similarity computation.
//
// # Inputs
//
//   - fp: The query fingerprint.
//
// # Outputs
//
//   - []string: Symbol IDs of candidate matches.
func (l *LSHIndex) Query(fp *CodeFingerprint) []string {
	if fp == nil || len(fp.MinHashSig) == 0 {
		return nil
	}

	expectedLen := l.numBands * l.rowsPerBand
	if len(fp.MinHashSig) < expectedLen {
		return nil
	}

	l.mu.RLock()
	defer l.mu.RUnlock()

	// Collect candidates from all bands
	candidateSet := make(map[string]bool)

	for band := 0; band < l.numBands; band++ {
		bandHash := l.hashBand(fp.MinHashSig, band)
		for _, id := range l.buckets[band][bandHash] {
			if id != fp.SymbolID { // Don't return self
				candidateSet[id] = true
			}
		}
	}

	// Convert to slice
	candidates := make([]string, 0, len(candidateSet))
	for id := range candidateSet {
		candidates = append(candidates, id)
	}

	return candidates
}

// QueryWithThreshold finds matches above a similarity threshold.
//
// # Description
//
// Uses LSH to find candidates, then verifies each candidate
// using estimated Jaccard similarity. Returns only matches
// above the threshold.
//
// # Inputs
//
//   - fp: The query fingerprint.
//   - threshold: Minimum similarity (0.0 - 1.0).
//
// # Outputs
//
//   - []LSHMatch: Matches above the threshold.
func (l *LSHIndex) QueryWithThreshold(fp *CodeFingerprint, threshold float64) []LSHMatch {
	candidates := l.Query(fp)
	if len(candidates) == 0 {
		return nil
	}

	l.mu.RLock()
	defer l.mu.RUnlock()

	matches := make([]LSHMatch, 0)

	for _, id := range candidates {
		candidateFP, exists := l.fingerprints[id]
		if !exists {
			continue
		}

		similarity := fp.EstimatedJaccard(candidateFP)
		if similarity >= threshold {
			matches = append(matches, LSHMatch{
				SymbolID:   id,
				Similarity: similarity,
			})
		}
	}

	return matches
}

// LSHMatch represents a similarity match from the LSH index.
type LSHMatch struct {
	// SymbolID is the matched symbol's ID.
	SymbolID string

	// Similarity is the estimated Jaccard similarity.
	Similarity float64
}

// hashBand computes the hash for a specific band of the signature.
func (l *LSHIndex) hashBand(sig []uint64, band int) uint64 {
	start := band * l.rowsPerBand
	end := start + l.rowsPerBand

	h := fnv.New64a()
	for i := start; i < end && i < len(sig); i++ {
		// Write each element as bytes
		b := make([]byte, 8)
		for j := 0; j < 8; j++ {
			b[j] = byte(sig[i] >> (j * 8))
		}
		h.Write(b)
	}

	return h.Sum64()
}

// Size returns the number of fingerprints in the index.
func (l *LSHIndex) Size() int {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return len(l.fingerprints)
}

// GetFingerprint retrieves a fingerprint by symbol ID.
//
// # Outputs
//
//   - *CodeFingerprint: The fingerprint, or nil if not found.
//   - bool: True if found.
func (l *LSHIndex) GetFingerprint(symbolID string) (*CodeFingerprint, bool) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	fp, exists := l.fingerprints[symbolID]
	return fp, exists
}

// FindAllDuplicates finds all pairs of similar fingerprints.
//
// # Description
//
// Efficiently finds all pairs of fingerprints with similarity above
// the threshold. Uses LSH to avoid O(n²) comparisons.
//
// # Inputs
//
//   - threshold: Minimum similarity (0.0 - 1.0).
//
// # Outputs
//
//   - []DuplicatePair: All pairs above the threshold.
func (l *LSHIndex) FindAllDuplicates(threshold float64) []DuplicatePair {
	l.mu.RLock()
	defer l.mu.RUnlock()

	// Track pairs we've already checked to avoid duplicates
	checked := make(map[string]bool)
	var pairs []DuplicatePair

	for _, fp := range l.fingerprints {
		candidates := l.queryCandidatesLocked(fp)

		for _, id := range candidates {
			// Create canonical pair key
			pairKey := canonicalPairKey(fp.SymbolID, id)
			if checked[pairKey] {
				continue
			}
			checked[pairKey] = true

			candidateFP, exists := l.fingerprints[id]
			if !exists {
				continue
			}

			similarity := fp.EstimatedJaccard(candidateFP)
			if similarity >= threshold {
				pairs = append(pairs, DuplicatePair{
					SymbolID1:  fp.SymbolID,
					SymbolID2:  id,
					Similarity: similarity,
				})
			}
		}
	}

	return pairs
}

// queryCandidatesLocked finds candidates without locking (caller must hold lock).
func (l *LSHIndex) queryCandidatesLocked(fp *CodeFingerprint) []string {
	candidateSet := make(map[string]bool)

	for band := 0; band < l.numBands; band++ {
		bandHash := l.hashBand(fp.MinHashSig, band)
		for _, id := range l.buckets[band][bandHash] {
			if id != fp.SymbolID {
				candidateSet[id] = true
			}
		}
	}

	candidates := make([]string, 0, len(candidateSet))
	for id := range candidateSet {
		candidates = append(candidates, id)
	}

	return candidates
}

// DuplicatePair represents a pair of similar fingerprints.
type DuplicatePair struct {
	// SymbolID1 is the first symbol's ID.
	SymbolID1 string

	// SymbolID2 is the second symbol's ID.
	SymbolID2 string

	// Similarity is the estimated Jaccard similarity.
	Similarity float64
}

// canonicalPairKey creates a consistent key for a pair regardless of order.
func canonicalPairKey(id1, id2 string) string {
	if id1 < id2 {
		return id1 + "|" + id2
	}
	return id2 + "|" + id1
}

// Stats returns statistics about the index.
func (l *LSHIndex) Stats() LSHStats {
	l.mu.RLock()
	defer l.mu.RUnlock()

	totalBuckets := 0
	maxBucketSize := 0

	for _, bandBuckets := range l.buckets {
		totalBuckets += len(bandBuckets)
		for _, bucket := range bandBuckets {
			if len(bucket) > maxBucketSize {
				maxBucketSize = len(bucket)
			}
		}
	}

	return LSHStats{
		NumFingerprints: len(l.fingerprints),
		NumBands:        l.numBands,
		RowsPerBand:     l.rowsPerBand,
		TotalBuckets:    totalBuckets,
		MaxBucketSize:   maxBucketSize,
	}
}

// LSHStats contains statistics about the LSH index.
type LSHStats struct {
	// NumFingerprints is the number of indexed fingerprints.
	NumFingerprints int

	// NumBands is the number of bands.
	NumBands int

	// RowsPerBand is the number of rows per band.
	RowsPerBand int

	// TotalBuckets is the total number of non-empty buckets.
	TotalBuckets int

	// MaxBucketSize is the size of the largest bucket.
	MaxBucketSize int
}
