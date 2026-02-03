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

	"github.com/google/uuid"
	"github.com/weaviate/weaviate-go-client/v5/weaviate"
	"github.com/weaviate/weaviate-go-client/v5/weaviate/filters"
	"github.com/weaviate/weaviate-go-client/v5/weaviate/graphql"
	"github.com/weaviate/weaviate/entities/models"
)

// MemoryStore handles storage and retrieval of code memories in Weaviate.
type MemoryStore struct {
	client    *weaviate.Client
	config    MemoryStoreConfig
	dataSpace string
}

// NewMemoryStore creates a new memory store.
//
// Description:
//
//	Creates a MemoryStore configured for a specific data space.
//	The data space is used for project isolation.
//
// Inputs:
//
//	client - Weaviate client. Must not be nil.
//	dataSpace - Project isolation key. Must not be empty.
//
// Outputs:
//
//	*MemoryStore - The configured memory store
//	error - Non-nil if client is nil or dataSpace is empty
//
// Thread Safety: Individual methods are safe for concurrent use. However,
// compound operations (e.g., Get then UpdateConfidence) may have race conditions
// and should use external synchronization if atomicity is required.
func NewMemoryStore(client *weaviate.Client, dataSpace string) (*MemoryStore, error) {
	if client == nil {
		return nil, errors.New("client must not be nil")
	}
	if dataSpace == "" {
		return nil, errors.New("dataSpace must not be empty")
	}
	return &MemoryStore{
		client:    client,
		config:    DefaultMemoryStoreConfig(),
		dataSpace: dataSpace,
	}, nil
}

// NewMemoryStoreWithConfig creates a memory store with custom configuration.
func NewMemoryStoreWithConfig(client *weaviate.Client, config MemoryStoreConfig) *MemoryStore {
	return &MemoryStore{
		client:    client,
		config:    config,
		dataSpace: config.DataSpace,
	}
}

// Store persists a new code memory to Weaviate.
//
// Description:
//
//	Validates and stores a new memory. Sets defaults for optional fields
//	like MemoryID, CreatedAt, LastUsed, Status, and DataSpace.
//
// Inputs:
//
//	ctx - Context for cancellation
//	memory - The memory to store. Content, MemoryType, Scope, and Source are required.
//
// Outputs:
//
//	*CodeMemory - The stored memory with generated fields populated
//	error - Non-nil if validation or storage fails
func (s *MemoryStore) Store(ctx context.Context, memory CodeMemory) (*CodeMemory, error) {
	// Set defaults
	if memory.MemoryID == "" {
		memory.MemoryID = uuid.NewString()
	}
	if memory.Confidence == 0 {
		memory.Confidence = s.config.DefaultConfidence
	}
	if memory.CreatedAt.IsZero() {
		memory.CreatedAt = time.Now().UTC()
	}
	if memory.LastUsed.IsZero() {
		memory.LastUsed = memory.CreatedAt
	}
	if memory.Status == "" {
		memory.Status = StatusActive
	}
	if memory.DataSpace == "" {
		memory.DataSpace = s.dataSpace
	}

	// Validate
	if err := memory.Validate(); err != nil {
		return nil, fmt.Errorf("validating memory: %w", err)
	}

	// Convert to Weaviate object
	obj := &models.Object{
		Class: CodeMemoryClassName,
		Properties: map[string]interface{}{
			"memoryId":   memory.MemoryID,
			"content":    memory.Content,
			"memoryType": string(memory.MemoryType),
			"scope":      memory.Scope,
			"confidence": memory.Confidence,
			"source":     string(memory.Source),
			"createdAt":  memory.CreatedAt.Format(time.RFC3339),
			"lastUsed":   memory.LastUsed.Format(time.RFC3339),
			"useCount":   memory.UseCount,
			"dataSpace":  memory.DataSpace,
			"status":     string(memory.Status),
		},
	}

	// Store in Weaviate
	_, err := s.client.Data().Creator().
		WithClassName(CodeMemoryClassName).
		WithProperties(obj.Properties).
		Do(ctx)

	if err != nil {
		return nil, fmt.Errorf("storing memory in Weaviate: %w", err)
	}

	slog.Info("Stored memory",
		"memory_id", memory.MemoryID,
		"type", memory.MemoryType,
		"scope", memory.Scope)

	return &memory, nil
}

// Get retrieves a memory by its ID.
//
// Description:
//
//	Fetches a single memory by its memoryId field.
//
// Inputs:
//
//	ctx - Context for cancellation
//	memoryID - The memory's unique identifier
//
// Outputs:
//
//	*CodeMemory - The found memory
//	error - Non-nil if not found or query fails
func (s *MemoryStore) Get(ctx context.Context, memoryID string) (*CodeMemory, error) {
	whereFilter := filters.Where().
		WithOperator(filters.And).
		WithOperands([]*filters.WhereBuilder{
			filters.Where().
				WithPath([]string{"memoryId"}).
				WithOperator(filters.Equal).
				WithValueString(memoryID),
			filters.Where().
				WithPath([]string{"dataSpace"}).
				WithOperator(filters.Equal).
				WithValueString(s.dataSpace),
		})

	fields := s.getQueryFields()

	result, err := s.client.GraphQL().Get().
		WithClassName(CodeMemoryClassName).
		WithFields(fields...).
		WithWhere(whereFilter).
		WithLimit(1).
		Do(ctx)

	if err != nil {
		return nil, fmt.Errorf("querying memory: %w", err)
	}

	if result.Errors != nil && len(result.Errors) > 0 {
		return nil, fmt.Errorf("query error: %s", result.Errors[0].Message)
	}

	memories, err := s.parseResults(result)
	if err != nil {
		return nil, err
	}

	if len(memories) == 0 {
		return nil, ErrMemoryNotFound
	}

	return &memories[0], nil
}

// List retrieves memories for the data space.
//
// Description:
//
//	Lists memories with optional filtering by type, status, and confidence.
//
// Inputs:
//
//	ctx - Context for cancellation
//	limit - Maximum number of results (default 10)
//	offset - Number of results to skip (for pagination)
//	memoryType - Optional filter by memory type
//	includeArchived - Whether to include archived memories
//	minConfidence - Minimum confidence threshold
//
// Outputs:
//
//	[]CodeMemory - The found memories
//	error - Non-nil if query fails
func (s *MemoryStore) List(ctx context.Context, limit, offset int, memoryType string, includeArchived bool, minConfidence float64) ([]CodeMemory, error) {
	if limit <= 0 {
		limit = s.config.MaxResults
	}

	// Build where filter
	operands := []*filters.WhereBuilder{
		filters.Where().
			WithPath([]string{"dataSpace"}).
			WithOperator(filters.Equal).
			WithValueString(s.dataSpace),
	}

	if !includeArchived {
		operands = append(operands, filters.Where().
			WithPath([]string{"status"}).
			WithOperator(filters.Equal).
			WithValueString(string(StatusActive)))
	}

	if memoryType != "" {
		operands = append(operands, filters.Where().
			WithPath([]string{"memoryType"}).
			WithOperator(filters.Equal).
			WithValueString(memoryType))
	}

	if minConfidence > 0 {
		operands = append(operands, filters.Where().
			WithPath([]string{"confidence"}).
			WithOperator(filters.GreaterThanEqual).
			WithValueNumber(minConfidence))
	}

	whereFilter := filters.Where().
		WithOperator(filters.And).
		WithOperands(operands)

	fields := s.getQueryFields()

	result, err := s.client.GraphQL().Get().
		WithClassName(CodeMemoryClassName).
		WithFields(fields...).
		WithWhere(whereFilter).
		WithLimit(limit).
		WithOffset(offset).
		Do(ctx)

	if err != nil {
		return nil, fmt.Errorf("listing memories: %w", err)
	}

	if result.Errors != nil && len(result.Errors) > 0 {
		return nil, fmt.Errorf("query error: %s", result.Errors[0].Message)
	}

	return s.parseResults(result)
}

// UpdateConfidence adjusts a memory's confidence score.
//
// Description:
//
//	Increases or decreases the confidence score by the given delta.
//	The result is clamped to [0.0, 1.0].
//
// Inputs:
//
//	ctx - Context for cancellation
//	memoryID - The memory's unique identifier
//	delta - Amount to add to confidence (can be negative)
//
// Outputs:
//
//	error - Non-nil if update fails or memory not found
func (s *MemoryStore) UpdateConfidence(ctx context.Context, memoryID string, delta float64) error {
	memory, err := s.Get(ctx, memoryID)
	if err != nil {
		return err
	}

	newConfidence := memory.Confidence + delta
	if newConfidence < 0 {
		newConfidence = 0
	}
	if newConfidence > 1.0 {
		newConfidence = 1.0
	}

	return s.updateField(ctx, memoryID, "confidence", newConfidence)
}

// MarkUsed updates the last_used timestamp and increments use_count.
//
// Description:
//
//	Called when a memory is retrieved to track usage patterns.
//	Updates lastUsed to now and increments useCount.
//
// Inputs:
//
//	ctx - Context for cancellation
//	memoryID - The memory's unique identifier
//
// Outputs:
//
//	error - Non-nil if update fails or memory not found
func (s *MemoryStore) MarkUsed(ctx context.Context, memoryID string) error {
	memory, err := s.Get(ctx, memoryID)
	if err != nil {
		return err
	}

	// We need to update multiple fields, so we'll do a batch update
	weaviateID, err := s.getWeaviateID(ctx, memoryID)
	if err != nil {
		return err
	}

	err = s.client.Data().Updater().
		WithClassName(CodeMemoryClassName).
		WithID(weaviateID).
		WithProperties(map[string]interface{}{
			"lastUsed": time.Now().UTC().Format(time.RFC3339),
			"useCount": memory.UseCount + 1,
		}).
		WithMerge().
		Do(ctx)

	if err != nil {
		return fmt.Errorf("marking memory as used: %w", err)
	}

	return nil
}

// Archive sets a memory's status to archived.
//
// Description:
//
//	Marks a memory as archived. Archived memories are not returned
//	by default in retrieval queries but are not deleted.
//
// Inputs:
//
//	ctx - Context for cancellation
//	memoryID - The memory's unique identifier
//
// Outputs:
//
//	error - Non-nil if update fails or memory not found
func (s *MemoryStore) Archive(ctx context.Context, memoryID string) error {
	return s.updateField(ctx, memoryID, "status", string(StatusArchived))
}

// Delete removes a memory from Weaviate.
//
// Description:
//
//	Permanently deletes a memory. This cannot be undone.
//
// Inputs:
//
//	ctx - Context for cancellation
//	memoryID - The memory's unique identifier
//
// Outputs:
//
//	error - Non-nil if deletion fails or memory not found
func (s *MemoryStore) Delete(ctx context.Context, memoryID string) error {
	weaviateID, err := s.getWeaviateID(ctx, memoryID)
	if err != nil {
		return err
	}

	err = s.client.Data().Deleter().
		WithClassName(CodeMemoryClassName).
		WithID(weaviateID).
		Do(ctx)

	if err != nil {
		return fmt.Errorf("deleting memory: %w", err)
	}

	slog.Info("Deleted memory", "memory_id", memoryID)
	return nil
}

// getWeaviateID finds the Weaviate UUID for a memory by its memoryId.
func (s *MemoryStore) getWeaviateID(ctx context.Context, memoryID string) (string, error) {
	whereFilter := filters.Where().
		WithOperator(filters.And).
		WithOperands([]*filters.WhereBuilder{
			filters.Where().
				WithPath([]string{"memoryId"}).
				WithOperator(filters.Equal).
				WithValueString(memoryID),
			filters.Where().
				WithPath([]string{"dataSpace"}).
				WithOperator(filters.Equal).
				WithValueString(s.dataSpace),
		})

	result, err := s.client.GraphQL().Get().
		WithClassName(CodeMemoryClassName).
		WithFields(graphql.Field{Name: "_additional { id }"},
			graphql.Field{Name: "memoryId"}).
		WithWhere(whereFilter).
		WithLimit(1).
		Do(ctx)

	if err != nil {
		return "", fmt.Errorf("finding Weaviate ID: %w", err)
	}

	if result.Errors != nil && len(result.Errors) > 0 {
		return "", fmt.Errorf("query error: %s", result.Errors[0].Message)
	}

	data, ok := result.Data["Get"].(map[string]interface{})
	if !ok {
		return "", ErrMemoryNotFound
	}

	objects, ok := data[CodeMemoryClassName].([]interface{})
	if !ok || len(objects) == 0 {
		return "", ErrMemoryNotFound
	}

	obj := objects[0].(map[string]interface{})
	additional, ok := obj["_additional"].(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("missing _additional field")
	}

	id, ok := additional["id"].(string)
	if !ok {
		return "", fmt.Errorf("missing id in _additional")
	}

	return id, nil
}

// updateField updates a single field on a memory.
func (s *MemoryStore) updateField(ctx context.Context, memoryID string, field string, value interface{}) error {
	weaviateID, err := s.getWeaviateID(ctx, memoryID)
	if err != nil {
		return err
	}

	err = s.client.Data().Updater().
		WithClassName(CodeMemoryClassName).
		WithID(weaviateID).
		WithProperties(map[string]interface{}{
			field: value,
		}).
		WithMerge().
		Do(ctx)

	if err != nil {
		return fmt.Errorf("updating memory %s.%s: %w", memoryID, field, err)
	}

	return nil
}

// getQueryFields returns the fields to query.
func (s *MemoryStore) getQueryFields() []graphql.Field {
	return []graphql.Field{
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
}

// parseResults converts Weaviate results to CodeMemory slice.
func (s *MemoryStore) parseResults(result *models.GraphQLResponse) ([]CodeMemory, error) {
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

// getString safely extracts a string from a map.
func getString(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// getFloat64 safely extracts a float64 from a map.
func getFloat64(m map[string]interface{}, key string) float64 {
	if v, ok := m[key]; ok {
		switch n := v.(type) {
		case float64:
			return n
		case float32:
			return float64(n)
		case int:
			return float64(n)
		}
	}
	return 0
}

// getInt safely extracts an int from a map.
func getInt(m map[string]interface{}, key string) int {
	if v, ok := m[key]; ok {
		switch n := v.(type) {
		case int:
			return n
		case int64:
			return int(n)
		case float64:
			return int(n)
		}
	}
	return 0
}
