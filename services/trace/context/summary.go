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
	"crypto/sha256"
	"encoding/hex"
	"time"
)

// Summary represents a hierarchical summary of code.
type Summary struct {
	// ID is the unique identifier (entity path).
	// Examples: "pkg/auth", "pkg/auth/validator.go", "pkg/auth/validator.go#ValidateToken"
	ID string `json:"id"`

	// Level is the hierarchy level (0=project, 1=package, 2=file, 3=function).
	Level int `json:"level"`

	// Content is the natural language summary.
	Content string `json:"content"`

	// Keywords are key terms for search matching.
	Keywords []string `json:"keywords"`

	// Children are IDs of child summaries.
	Children []string `json:"children"`

	// ParentID is the parent summary ID (empty for project root).
	ParentID string `json:"parent_id"`

	// CreatedAt is when this summary was created.
	CreatedAt time.Time `json:"created_at"`

	// UpdatedAt is when this summary was last updated.
	UpdatedAt time.Time `json:"updated_at"`

	// Hash is a hash of the source content for invalidation.
	Hash string `json:"hash"`

	// Partial indicates this is a degraded summary (generated without LLM).
	Partial bool `json:"partial"`

	// Language is the source code language.
	Language string `json:"language"`

	// TokensUsed is the LLM tokens consumed to generate this summary.
	TokensUsed int `json:"tokens_used"`

	// Version is for optimistic concurrency control.
	Version int64 `json:"version"`
}

// IsStale returns true if the hash doesn't match the provided hash.
func (s *Summary) IsStale(currentHash string) bool {
	return s.Hash != currentHash
}

// IsFresh returns true if the summary was created/updated recently.
func (s *Summary) IsFresh(maxAge time.Duration) bool {
	return time.Since(s.UpdatedAt) < maxAge
}

// HierarchyLevel returns the hierarchy level as the enum type.
func (s *Summary) HierarchyLevel() HierarchyLevel {
	switch s.Level {
	case 0:
		return LevelProject
	case 1:
		return LevelPackage
	case 2:
		return LevelFile
	case 3:
		return LevelFunction
	default:
		return LevelFunction
	}
}

// SummaryBatch represents a batch of summaries for atomic updates.
type SummaryBatch struct {
	// Version is the batch version for idempotency.
	Version int64 `json:"version"`

	// Summaries are the summaries to upsert.
	Summaries []Summary `json:"summaries"`

	// DeleteIDs are summary IDs to delete.
	DeleteIDs []string `json:"delete_ids"`

	// Checksum is for integrity verification.
	Checksum string `json:"checksum"`
}

// Validate checks the batch integrity.
func (b *SummaryBatch) Validate() error {
	if b.Checksum == "" {
		return nil // Checksum is optional
	}

	// Verify checksum matches computed value
	computed := b.computeChecksum()
	if computed != b.Checksum {
		return ErrBatchValidationFailed
	}

	return nil
}

// ComputeChecksum calculates the batch checksum.
func (b *SummaryBatch) ComputeChecksum() string {
	return b.computeChecksum()
}

func (b *SummaryBatch) computeChecksum() string {
	h := sha256.New()

	// Include version
	h.Write([]byte{byte(b.Version >> 56), byte(b.Version >> 48),
		byte(b.Version >> 40), byte(b.Version >> 32),
		byte(b.Version >> 24), byte(b.Version >> 16),
		byte(b.Version >> 8), byte(b.Version)})

	// Include summary IDs and hashes
	for _, s := range b.Summaries {
		h.Write([]byte(s.ID))
		h.Write([]byte(s.Hash))
	}

	// Include delete IDs
	for _, id := range b.DeleteIDs {
		h.Write([]byte("D:"))
		h.Write([]byte(id))
	}

	return hex.EncodeToString(h.Sum(nil))[:16]
}

// SetChecksum computes and sets the checksum.
func (b *SummaryBatch) SetChecksum() {
	b.Checksum = b.computeChecksum()
}

// ComputeContentHash computes a hash for content used in summaries.
func ComputeContentHash(content string) string {
	h := sha256.Sum256([]byte(content))
	return hex.EncodeToString(h[:])[:16]
}
