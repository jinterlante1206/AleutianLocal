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
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/weaviate/weaviate-go-client/v5/weaviate"
	"github.com/weaviate/weaviate-go-client/v5/weaviate/filters"
	"github.com/weaviate/weaviate-go-client/v5/weaviate/graphql"
	"github.com/weaviate/weaviate/entities/models"
)

// LifecycleManager handles memory lifecycle transitions.
type LifecycleManager struct {
	client    *weaviate.Client
	store     *MemoryStore
	config    LifecycleConfig
	dataSpace string
}

// NewLifecycleManager creates a new lifecycle manager.
//
// Description:
//
//	Creates a LifecycleManager for handling memory lifecycle transitions
//	including archival, validation, and contradiction handling.
//
// Inputs:
//
//	client - Weaviate client. Must not be nil.
//	store - MemoryStore for updates. Must not be nil.
//	dataSpace - Project isolation key.
//
// Outputs:
//
//	*LifecycleManager - The configured manager
//	error - Non-nil if client or store is nil
//
// Thread Safety: All methods are safe for concurrent use.
func NewLifecycleManager(client *weaviate.Client, store *MemoryStore, dataSpace string) (*LifecycleManager, error) {
	if client == nil {
		return nil, errors.New("client must not be nil")
	}
	if store == nil {
		return nil, errors.New("store must not be nil")
	}
	return &LifecycleManager{
		client:    client,
		store:     store,
		config:    DefaultLifecycleConfig(),
		dataSpace: dataSpace,
	}, nil
}

// NewLifecycleManagerWithConfig creates a manager with custom configuration.
func NewLifecycleManagerWithConfig(client *weaviate.Client, store *MemoryStore, dataSpace string, config LifecycleConfig) *LifecycleManager {
	return &LifecycleManager{
		client:    client,
		store:     store,
		config:    config,
		dataSpace: dataSpace,
	}
}

// CleanupResult contains the results of a cleanup run.
type CleanupResult struct {
	// MemoriesArchived is the count of memories archived due to staleness.
	MemoriesArchived int

	// MemoriesDeleted is the count of memories deleted due to low confidence.
	MemoriesDeleted int

	// Errors contains any non-fatal errors encountered.
	Errors []string
}

// RunCleanup performs lifecycle maintenance on memories.
//
// Description:
//
//	Archives memories that haven't been used within the stale threshold.
//	Deletes memories with confidence below a critical threshold.
//	This should be run periodically (e.g., daily via cron).
//
// Inputs:
//
//	ctx - Context for cancellation
//
// Outputs:
//
//	*CleanupResult - Statistics about cleanup actions
//	error - Non-nil if cleanup fails completely
func (m *LifecycleManager) RunCleanup(ctx context.Context) (*CleanupResult, error) {
	result := &CleanupResult{
		Errors: make([]string, 0),
	}

	slog.Info("Starting memory cleanup", "data_space", m.dataSpace)

	// Find stale memories (not used within threshold)
	staleThreshold := time.Now().Add(-m.config.StaleThreshold)

	staleMemories, err := m.findStaleMemories(ctx, staleThreshold)
	if err != nil {
		return result, fmt.Errorf("finding stale memories: %w", err)
	}

	// Archive stale memories
	const maxErrorsBeforeAbort = 10
	for _, memory := range staleMemories {
		// Check context cancellation
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		// Check error threshold
		if len(result.Errors) >= maxErrorsBeforeAbort {
			return result, fmt.Errorf("too many errors (%d), aborting cleanup", len(result.Errors))
		}
		if err := m.store.Archive(ctx, memory.MemoryID); err != nil {
			result.Errors = append(result.Errors,
				fmt.Sprintf("archiving %s: %v", memory.MemoryID, err))
			continue
		}
		result.MemoriesArchived++
	}

	// Check context before next phase
	if ctx.Err() != nil {
		return result, ctx.Err()
	}

	// Find very low confidence memories to delete
	lowConfidenceMemories, err := m.findLowConfidenceMemories(ctx, m.config.DeleteBelowConfidence)
	if err != nil {
		return result, fmt.Errorf("finding low confidence memories: %w", err)
	}

	// Delete very low confidence memories
	for _, memory := range lowConfidenceMemories {
		// Check context cancellation
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		// Check error threshold
		if len(result.Errors) >= maxErrorsBeforeAbort {
			return result, fmt.Errorf("too many errors (%d), aborting cleanup", len(result.Errors))
		}
		if err := m.store.Delete(ctx, memory.MemoryID); err != nil {
			result.Errors = append(result.Errors,
				fmt.Sprintf("deleting %s: %v", memory.MemoryID, err))
			continue
		}
		result.MemoriesDeleted++
	}

	slog.Info("Memory cleanup complete",
		"archived", result.MemoriesArchived,
		"deleted", result.MemoriesDeleted,
		"errors", len(result.Errors))

	return result, nil
}

// ValidateMemory boosts confidence when a memory is confirmed useful.
//
// Description:
//
//	Increases the confidence score when the agent or user confirms
//	that a memory was helpful. Also reactivates archived memories.
//
// Inputs:
//
//	ctx - Context for cancellation
//	memoryID - The memory's unique identifier
//
// Outputs:
//
//	error - Non-nil if update fails or memory not found
func (m *LifecycleManager) ValidateMemory(ctx context.Context, memoryID string) error {
	memory, err := m.store.Get(ctx, memoryID)
	if err != nil {
		return err
	}

	// Boost confidence
	newConfidence := memory.Confidence + m.config.ConfidenceBoostOnValidation
	if newConfidence > 1.0 {
		newConfidence = 1.0
	}

	// Update confidence
	if err := m.store.UpdateConfidence(ctx, memoryID, m.config.ConfidenceBoostOnValidation); err != nil {
		return fmt.Errorf("boosting confidence: %w", err)
	}

	// Reactivate if archived
	if memory.Status == StatusArchived {
		if err := m.reactivateMemory(ctx, memoryID); err != nil {
			return fmt.Errorf("reactivating memory: %w", err)
		}
	}

	slog.Info("Validated memory",
		"memory_id", memoryID,
		"new_confidence", newConfidence)

	return nil
}

// ContradictMemory handles when evidence contradicts a memory.
//
// Description:
//
//	Decreases confidence significantly when a memory is found to be
//	incorrect or outdated. If confidence drops below threshold, deletes.
//
// Inputs:
//
//	ctx - Context for cancellation
//	memoryID - The memory's unique identifier
//	reason - Why the memory is being contradicted
//
// Outputs:
//
//	error - Non-nil if update fails or memory not found
func (m *LifecycleManager) ContradictMemory(ctx context.Context, memoryID, reason string) error {
	memory, err := m.store.Get(ctx, memoryID)
	if err != nil {
		return err
	}

	newConfidence := memory.Confidence - m.config.ConfidenceDecayOnContradiction

	slog.Info("Memory contradicted",
		"memory_id", memoryID,
		"reason", reason,
		"old_confidence", memory.Confidence,
		"new_confidence", newConfidence)

	// If confidence too low, delete entirely
	if newConfidence <= 0.1 {
		return m.store.Delete(ctx, memoryID)
	}

	// Otherwise just reduce confidence
	return m.store.UpdateConfidence(ctx, memoryID, -m.config.ConfidenceDecayOnContradiction)
}

// PromoteToActive promotes a memory to active status if confidence is high enough.
//
// Description:
//
//	Checks if a memory's confidence meets the threshold for active status
//	and promotes it from created/archived to active.
//
// Inputs:
//
//	ctx - Context for cancellation
//	memoryID - The memory's unique identifier
//
// Outputs:
//
//	bool - Whether the memory was promoted
//	error - Non-nil if check/update fails
func (m *LifecycleManager) PromoteToActive(ctx context.Context, memoryID string) (bool, error) {
	memory, err := m.store.Get(ctx, memoryID)
	if err != nil {
		return false, err
	}

	if memory.Confidence < m.config.MinActiveConfidence {
		return false, nil
	}

	if memory.Status == StatusActive {
		return false, nil // Already active
	}

	if err := m.reactivateMemory(ctx, memoryID); err != nil {
		return false, err
	}

	return true, nil
}

// ScopeValidationResult contains the results of scope validation.
type ScopeValidationResult struct {
	// MemoriesOrphaned is the count of memories marked as orphaned.
	MemoriesOrphaned int

	// MemoriesValidated is the count of memories with valid scopes.
	MemoriesValidated int

	// Errors contains any non-fatal errors encountered.
	Errors []string
}

// ValidateScopes checks memory scopes against current project files.
//
// # Description
//
// After a graph rebuild, call this method with the current file list to detect
// memories whose scope patterns no longer match any files. Such memories are
// marked as Orphaned because they reference files that no longer exist.
//
// This solves the "Ephemeral Graph vs Persistent Memory" problem where:
// 1. Agent learns: "Always check auth in `pkg/users/handler.go`"
// 2. User refactors: `pkg/users/` → `internal/identity/`
// 3. Graph rebuilds correctly (source-driven)
// 4. This method detects the memory is now orphaned
//
// # Inputs
//
//   - ctx: Context for cancellation.
//   - currentFiles: List of file paths currently in the project.
//
// # Outputs
//
//   - *ScopeValidationResult: Statistics about validation.
//   - error: Non-nil if validation fails completely.
//
// # Thread Safety
//
// Safe for concurrent use.
func (m *LifecycleManager) ValidateScopes(ctx context.Context, currentFiles []string) (*ScopeValidationResult, error) {
	result := &ScopeValidationResult{
		Errors: make([]string, 0),
	}

	slog.Info("Starting scope validation", "data_space", m.dataSpace, "file_count", len(currentFiles))

	// Build file set for fast lookup
	fileSet := make(map[string]bool, len(currentFiles))
	for _, f := range currentFiles {
		fileSet[f] = true
	}

	// Find all active memories with non-empty scopes
	memories, err := m.findScopedMemories(ctx)
	if err != nil {
		return result, fmt.Errorf("finding scoped memories: %w", err)
	}

	const maxErrorsBeforeAbort = 10
	for _, memory := range memories {
		// Check context cancellation
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		// Check error threshold
		if len(result.Errors) >= maxErrorsBeforeAbort {
			return result, fmt.Errorf("too many errors (%d), aborting validation", len(result.Errors))
		}

		// Check if scope matches any current file
		if !m.scopeMatchesAny(memory.Scope, fileSet) {
			// Mark as orphaned
			if err := m.markOrphaned(ctx, memory.MemoryID); err != nil {
				result.Errors = append(result.Errors,
					fmt.Sprintf("marking %s as orphaned: %v", memory.MemoryID, err))
				continue
			}
			result.MemoriesOrphaned++
			slog.Info("Memory orphaned",
				"memory_id", memory.MemoryID,
				"scope", memory.Scope)
		} else {
			result.MemoriesValidated++
		}
	}

	slog.Info("Scope validation complete",
		"orphaned", result.MemoriesOrphaned,
		"validated", result.MemoriesValidated,
		"errors", len(result.Errors))

	return result, nil
}

// findScopedMemories finds all active memories with non-empty scopes.
func (m *LifecycleManager) findScopedMemories(ctx context.Context) ([]CodeMemory, error) {
	whereFilter := filters.Where().
		WithOperator(filters.And).
		WithOperands([]*filters.WhereBuilder{
			filters.Where().
				WithPath([]string{"dataSpace"}).
				WithOperator(filters.Equal).
				WithValueString(m.dataSpace),
			filters.Where().
				WithPath([]string{"status"}).
				WithOperator(filters.Equal).
				WithValueString(string(StatusActive)),
			filters.Where().
				WithPath([]string{"scope"}).
				WithOperator(filters.NotEqual).
				WithValueString(""),
		})

	fields := []graphql.Field{
		{Name: "memoryId"},
		{Name: "content"},
		{Name: "memoryType"},
		{Name: "scope"},
		{Name: "confidence"},
		{Name: "source"},
		{Name: "createdAt"},
		{Name: "lastUsed"},
		{Name: "useCount"},
		{Name: "dataSpace"},
		{Name: "status"},
	}

	result, err := m.client.GraphQL().Get().
		WithClassName(CodeMemoryClassName).
		WithFields(fields...).
		WithWhere(whereFilter).
		WithLimit(1000). // Process larger batch for scope validation
		Do(ctx)

	if err != nil {
		return nil, fmt.Errorf("querying scoped memories: %w", err)
	}

	if result.Errors != nil && len(result.Errors) > 0 {
		return nil, fmt.Errorf("query error: %s", result.Errors[0].Message)
	}

	return m.parseResults(result)
}

// scopeMatchesAny checks if a scope glob pattern matches any file in the set.
func (m *LifecycleManager) scopeMatchesAny(scope string, fileSet map[string]bool) bool {
	// Direct match check first
	if fileSet[scope] {
		return true
	}

	// For glob patterns, we need to check if any file matches
	// Common patterns: "pkg/users/*", "*.go", "internal/**/*.go"
	for file := range fileSet {
		if matchGlob(scope, file) {
			return true
		}
	}

	return false
}

// matchGlob performs simple glob matching.
// Supports: * (any single path segment), ** (any path depth), ? (any single char)
func matchGlob(pattern, path string) bool {
	// Simple implementation - for production, use doublestar or similar
	// This handles the most common cases
	if pattern == path {
		return true
	}
	if pattern == "*" {
		return true
	}

	// Handle ** for recursive matching
	if len(pattern) >= 2 && pattern[:2] == "**" {
		// ** matches any path, try matching rest of pattern against any suffix
		rest := ""
		if len(pattern) > 2 {
			if pattern[2] == '/' {
				rest = pattern[3:]
			} else {
				rest = pattern[2:]
			}
		}
		if rest == "" {
			return true
		}
		// Try matching rest against path and all suffixes
		for i := 0; i <= len(path); i++ {
			if matchGlob(rest, path[i:]) {
				return true
			}
		}
		return false
	}

	// Handle single * wildcard
	if len(pattern) > 0 && pattern[0] == '*' {
		rest := pattern[1:]
		// * matches until next /
		for i := 0; i <= len(path); i++ {
			if i > 0 && path[i-1] == '/' {
				break // * doesn't cross directory boundaries
			}
			if matchGlob(rest, path[i:]) {
				return true
			}
		}
		return false
	}

	// Character-by-character matching
	if len(pattern) == 0 || len(path) == 0 {
		return len(pattern) == 0 && len(path) == 0
	}

	if pattern[0] == '?' || pattern[0] == path[0] {
		return matchGlob(pattern[1:], path[1:])
	}

	return false
}

// markOrphaned sets a memory's status to orphaned.
func (m *LifecycleManager) markOrphaned(ctx context.Context, memoryID string) error {
	weaviateID, err := m.store.getWeaviateID(ctx, memoryID)
	if err != nil {
		return err
	}

	err = m.client.Data().Updater().
		WithClassName(CodeMemoryClassName).
		WithID(weaviateID).
		WithProperties(map[string]interface{}{
			"status": string(StatusOrphaned),
		}).
		WithMerge().
		Do(ctx)

	if err != nil {
		return fmt.Errorf("marking memory as orphaned: %w", err)
	}

	return nil
}

// findStaleMemories finds memories that haven't been used within the threshold.
func (m *LifecycleManager) findStaleMemories(ctx context.Context, threshold time.Time) ([]CodeMemory, error) {
	whereFilter := filters.Where().
		WithOperator(filters.And).
		WithOperands([]*filters.WhereBuilder{
			filters.Where().
				WithPath([]string{"dataSpace"}).
				WithOperator(filters.Equal).
				WithValueString(m.dataSpace),
			filters.Where().
				WithPath([]string{"status"}).
				WithOperator(filters.Equal).
				WithValueString(string(StatusActive)),
			filters.Where().
				WithPath([]string{"lastUsed"}).
				WithOperator(filters.LessThan).
				WithValueDate(threshold),
		})

	fields := []graphql.Field{
		{Name: "memoryId"},
		{Name: "content"},
		{Name: "memoryType"},
		{Name: "scope"},
		{Name: "confidence"},
		{Name: "source"},
		{Name: "createdAt"},
		{Name: "lastUsed"},
		{Name: "useCount"},
		{Name: "dataSpace"},
		{Name: "status"},
	}

	result, err := m.client.GraphQL().Get().
		WithClassName(CodeMemoryClassName).
		WithFields(fields...).
		WithWhere(whereFilter).
		WithLimit(100). // Process in batches
		Do(ctx)

	if err != nil {
		return nil, fmt.Errorf("querying stale memories: %w", err)
	}

	if result.Errors != nil && len(result.Errors) > 0 {
		return nil, fmt.Errorf("query error: %s", result.Errors[0].Message)
	}

	return m.parseResults(result)
}

// findLowConfidenceMemories finds memories with confidence below threshold.
func (m *LifecycleManager) findLowConfidenceMemories(ctx context.Context, threshold float64) ([]CodeMemory, error) {
	whereFilter := filters.Where().
		WithOperator(filters.And).
		WithOperands([]*filters.WhereBuilder{
			filters.Where().
				WithPath([]string{"dataSpace"}).
				WithOperator(filters.Equal).
				WithValueString(m.dataSpace),
			filters.Where().
				WithPath([]string{"confidence"}).
				WithOperator(filters.LessThan).
				WithValueNumber(threshold),
		})

	fields := []graphql.Field{
		{Name: "memoryId"},
		{Name: "content"},
		{Name: "memoryType"},
		{Name: "scope"},
		{Name: "confidence"},
		{Name: "source"},
		{Name: "createdAt"},
		{Name: "lastUsed"},
		{Name: "useCount"},
		{Name: "dataSpace"},
		{Name: "status"},
	}

	result, err := m.client.GraphQL().Get().
		WithClassName(CodeMemoryClassName).
		WithFields(fields...).
		WithWhere(whereFilter).
		WithLimit(100).
		Do(ctx)

	if err != nil {
		return nil, fmt.Errorf("querying low confidence memories: %w", err)
	}

	if result.Errors != nil && len(result.Errors) > 0 {
		return nil, fmt.Errorf("query error: %s", result.Errors[0].Message)
	}

	return m.parseResults(result)
}

// reactivateMemory sets a memory's status to active.
func (m *LifecycleManager) reactivateMemory(ctx context.Context, memoryID string) error {
	weaviateID, err := m.store.getWeaviateID(ctx, memoryID)
	if err != nil {
		return err
	}

	err = m.client.Data().Updater().
		WithClassName(CodeMemoryClassName).
		WithID(weaviateID).
		WithProperties(map[string]interface{}{
			"status": string(StatusActive),
		}).
		WithMerge().
		Do(ctx)

	if err != nil {
		return fmt.Errorf("reactivating memory: %w", err)
	}

	return nil
}

// parseResults converts Weaviate results to CodeMemory slice.
func (m *LifecycleManager) parseResults(result *models.GraphQLResponse) ([]CodeMemory, error) {
	data, ok := result.Data["Get"].(map[string]interface{})
	if !ok {
		return []CodeMemory{}, nil
	}

	objects, ok := data[CodeMemoryClassName].([]interface{})
	if !ok {
		return []CodeMemory{}, nil
	}

	memories := make([]CodeMemory, 0, len(objects))
	for _, obj := range objects {
		m, ok := obj.(map[string]interface{})
		if !ok {
			continue // skip malformed objects
		}
		memory := CodeMemory{
			MemoryID:   getString(m, "memoryId"),
			Content:    getString(m, "content"),
			MemoryType: MemoryType(getString(m, "memoryType")),
			Scope:      getString(m, "scope"),
			Confidence: getFloat64(m, "confidence"),
			Source:     MemorySource(getString(m, "source")),
			UseCount:   getInt(m, "useCount"),
			DataSpace:  getString(m, "dataSpace"),
			Status:     MemoryStatus(getString(m, "status")),
		}

		if createdStr := getString(m, "createdAt"); createdStr != "" {
			if t, err := time.Parse(time.RFC3339, createdStr); err == nil {
				memory.CreatedAt = t
			}
		}

		if lastUsedStr := getString(m, "lastUsed"); lastUsedStr != "" {
			if t, err := time.Parse(time.RFC3339, lastUsedStr); err == nil {
				memory.LastUsed = t
			}
		}

		memories = append(memories, memory)
	}

	return memories, nil
}
