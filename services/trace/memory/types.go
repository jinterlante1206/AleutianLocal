// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package memory

import (
	"errors"
	"time"
)

// MemoryType represents the kind of learned knowledge.
type MemoryType string

const (
	MemoryTypeConstraint   MemoryType = "constraint"
	MemoryTypePattern      MemoryType = "pattern"
	MemoryTypeConvention   MemoryType = "convention"
	MemoryTypeBugPattern   MemoryType = "bug_pattern"
	MemoryTypeOptimization MemoryType = "optimization"
	MemoryTypeSecurity     MemoryType = "security"
)

// ValidMemoryTypes is the set of valid memory types.
var ValidMemoryTypes = map[MemoryType]bool{
	MemoryTypeConstraint:   true,
	MemoryTypePattern:      true,
	MemoryTypeConvention:   true,
	MemoryTypeBugPattern:   true,
	MemoryTypeOptimization: true,
	MemoryTypeSecurity:     true,
}

// MemorySource represents how a memory was learned.
type MemorySource string

const (
	SourceAgentDiscovery MemorySource = "agent_discovery"
	SourceUserFeedback   MemorySource = "user_feedback"
	SourceTestFailure    MemorySource = "test_failure"
	SourceCodeReview     MemorySource = "code_review"
	SourceManual         MemorySource = "manual"
)

// ValidMemorySources is the set of valid memory sources.
var ValidMemorySources = map[MemorySource]bool{
	SourceAgentDiscovery: true,
	SourceUserFeedback:   true,
	SourceTestFailure:    true,
	SourceCodeReview:     true,
	SourceManual:         true,
}

// MemoryStatus represents the lifecycle status of a memory.
type MemoryStatus string

const (
	StatusActive   MemoryStatus = "active"
	StatusArchived MemoryStatus = "archived"
	// StatusOrphaned indicates a memory's scope no longer matches any files.
	// This happens when files are deleted or refactored and the memory's
	// file glob pattern no longer matches any existing files.
	StatusOrphaned MemoryStatus = "orphaned"
)

// Sentinel errors for memory operations.
var (
	ErrMemoryNotFound      = errors.New("memory not found")
	ErrInvalidMemoryType   = errors.New("invalid memory type")
	ErrInvalidMemorySource = errors.New("invalid memory source")
	ErrInvalidConfidence   = errors.New("confidence must be between 0.0 and 1.0")
	ErrEmptyContent        = errors.New("memory content cannot be empty")
	ErrEmptyScope          = errors.New("memory scope cannot be empty")
	ErrMemoryAlreadyExists = errors.New("memory with this ID already exists")
)

// CodeMemory represents a learned constraint or pattern.
type CodeMemory struct {
	// MemoryID is the unique identifier (UUID).
	MemoryID string `json:"memory_id"`

	// Content is the learned rule or constraint.
	Content string `json:"content"`

	// MemoryType categorizes the memory.
	MemoryType MemoryType `json:"memory_type"`

	// Scope is a file glob pattern indicating where this memory applies.
	Scope string `json:"scope"`

	// Confidence is a score from 0.0 to 1.0 indicating reliability.
	Confidence float64 `json:"confidence"`

	// Source indicates how this memory was learned.
	Source MemorySource `json:"source"`

	// CreatedAt is when the memory was first stored (Unix milliseconds UTC).
	CreatedAt int64 `json:"created_at"`

	// LastUsed is when the memory was last retrieved (Unix milliseconds UTC).
	LastUsed int64 `json:"last_used"`

	// UseCount tracks how many times this memory has been retrieved.
	UseCount int `json:"use_count"`

	// DataSpace is the project isolation key.
	DataSpace string `json:"data_space"`

	// Status indicates the lifecycle stage.
	Status MemoryStatus `json:"status"`
}

// Validate checks that the memory has valid fields.
//
// Description:
//
//	Validates all required fields and constraints on a CodeMemory.
//	Should be called before storing a new memory.
//
// Outputs:
//
//	error - Non-nil if validation fails
func (m *CodeMemory) Validate() error {
	if m.Content == "" {
		return ErrEmptyContent
	}
	if m.Scope == "" {
		return ErrEmptyScope
	}
	if !ValidMemoryTypes[m.MemoryType] {
		return ErrInvalidMemoryType
	}
	if !ValidMemorySources[m.Source] {
		return ErrInvalidMemorySource
	}
	if m.Confidence < 0.0 || m.Confidence > 1.0 {
		return ErrInvalidConfidence
	}
	return nil
}

// MemoryStoreConfig configures the memory store.
type MemoryStoreConfig struct {
	// DataSpace is the project isolation key.
	DataSpace string

	// DefaultConfidence is the initial confidence for new memories.
	DefaultConfidence float64

	// MaxResults is the default limit for retrieval queries.
	MaxResults int
}

// DefaultMemoryStoreConfig returns sensible defaults.
func DefaultMemoryStoreConfig() MemoryStoreConfig {
	return MemoryStoreConfig{
		DefaultConfidence: 0.5,
		MaxResults:        10,
	}
}

// LifecycleConfig configures the memory lifecycle manager.
type LifecycleConfig struct {
	// StaleThreshold is how long before a memory is considered stale.
	StaleThreshold time.Duration

	// MinActiveConfidence is the minimum confidence for active status.
	MinActiveConfidence float64

	// ConfidenceBoostOnValidation is how much to increase confidence on validation.
	ConfidenceBoostOnValidation float64

	// ConfidenceDecayOnContradiction is how much to decrease confidence on contradiction.
	ConfidenceDecayOnContradiction float64

	// DeleteBelowConfidence is the confidence threshold below which memories are deleted.
	DeleteBelowConfidence float64
}

// DefaultLifecycleConfig returns sensible defaults.
func DefaultLifecycleConfig() LifecycleConfig {
	return LifecycleConfig{
		StaleThreshold:                 90 * 24 * time.Hour, // 90 days
		MinActiveConfidence:            0.7,
		ConfidenceBoostOnValidation:    0.1,
		ConfidenceDecayOnContradiction: 0.3,
		DeleteBelowConfidence:          0.1, // Delete if below 10%
	}
}

// RetrieveOptions configures retrieval behavior.
type RetrieveOptions struct {
	// Query is the semantic search query.
	Query string

	// Scope filters memories by their scope (glob pattern).
	Scope string

	// Limit is the maximum number of results.
	Limit int

	// IncludeArchived includes archived memories in results.
	IncludeArchived bool

	// MinConfidence filters out memories below this threshold.
	MinConfidence float64
}

// RetrieveResult contains a memory and its retrieval score.
type RetrieveResult struct {
	// Memory is the retrieved memory.
	Memory CodeMemory `json:"memory"`

	// Score is the combined relevance score.
	Score float64 `json:"score"`
}

// StoreRequest is the HTTP request for storing a memory.
type StoreRequest struct {
	Content    string     `json:"content" binding:"required"`
	MemoryType MemoryType `json:"memory_type" binding:"required"`
	Scope      string     `json:"scope" binding:"required"`
	Confidence float64    `json:"confidence"`
	Source     string     `json:"source"`
}

// RetrieveRequest is the HTTP request for retrieving memories.
type RetrieveRequest struct {
	Query           string  `json:"query" binding:"required"`
	Scope           string  `json:"scope"`
	Limit           int     `json:"limit"`
	IncludeArchived bool    `json:"include_archived"`
	MinConfidence   float64 `json:"min_confidence"`
}

// ListRequest is the HTTP request for listing memories.
type ListRequest struct {
	Limit           int     `form:"limit"`
	Offset          int     `form:"offset"`
	MemoryType      string  `form:"memory_type"`
	IncludeArchived bool    `form:"include_archived"`
	MinConfidence   float64 `form:"min_confidence"`
}

// MemoryResponse is the HTTP response for a single memory.
type MemoryResponse struct {
	Memory CodeMemory `json:"memory"`
}

// MemoriesResponse is the HTTP response for multiple memories.
type MemoriesResponse struct {
	Memories []CodeMemory `json:"memories"`
	Total    int          `json:"total"`
}

// RetrieveResponse is the HTTP response for memory retrieval.
type RetrieveResponse struct {
	Results []RetrieveResult `json:"results"`
}
