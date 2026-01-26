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
	"fmt"
	"log/slog"

	"github.com/weaviate/weaviate-go-client/v5/weaviate"
	"github.com/weaviate/weaviate/entities/models"
)

// CodeMemoryClassName is the Weaviate class name for code memories.
const CodeMemoryClassName = "CodeMemory"

// GetCodeMemorySchema returns the Weaviate schema for CodeMemory class.
//
// Description:
//
//	Defines the schema for storing code memories in Weaviate.
//	Uses text2vec-transformers for vectorizing content and scope fields.
//
// Outputs:
//
//	*models.Class - The Weaviate class definition
func GetCodeMemorySchema() *models.Class {
	indexFilterable := new(bool)
	*indexFilterable = true

	indexSearchable := new(bool)
	*indexSearchable = true

	// Skip vectorization for non-content fields
	skipVectorization := new(bool)
	*skipVectorization = true

	return &models.Class{
		Class:       CodeMemoryClassName,
		Description: "Learned constraints, patterns, and conventions from code analysis",
		Vectorizer:  "text2vec-transformers",
		ModuleConfig: map[string]interface{}{
			"text2vec-transformers": map[string]interface{}{
				"vectorizeClassName": false,
			},
		},
		InvertedIndexConfig: &models.InvertedIndexConfig{
			IndexTimestamps: true,
		},
		Properties: []*models.Property{
			{
				Name:            "memoryId",
				DataType:        []string{"text"},
				Description:     "Unique identifier (UUID)",
				IndexFilterable: indexFilterable,
				Tokenization:    "field",
				ModuleConfig: map[string]interface{}{
					"text2vec-transformers": map[string]interface{}{
						"skip": true,
					},
				},
			},
			{
				Name:            "content",
				DataType:        []string{"text"},
				Description:     "The learned rule or constraint",
				IndexSearchable: indexSearchable,
				Tokenization:    "word",
				// Content is vectorized for semantic search
			},
			{
				Name:            "memoryType",
				DataType:        []string{"text"},
				Description:     "Type: constraint, pattern, convention, bug_pattern, optimization, security",
				IndexFilterable: indexFilterable,
				Tokenization:    "field",
				ModuleConfig: map[string]interface{}{
					"text2vec-transformers": map[string]interface{}{
						"skip": true,
					},
				},
			},
			{
				Name:            "scope",
				DataType:        []string{"text"},
				Description:     "File glob pattern indicating where this memory applies",
				IndexSearchable: indexSearchable,
				Tokenization:    "word",
				// Scope is vectorized to help find relevant memories
			},
			{
				Name:        "confidence",
				DataType:    []string{"number"},
				Description: "Confidence score from 0.0 to 1.0",
				ModuleConfig: map[string]interface{}{
					"text2vec-transformers": map[string]interface{}{
						"skip": true,
					},
				},
			},
			{
				Name:            "source",
				DataType:        []string{"text"},
				Description:     "How this memory was learned: agent_discovery, user_feedback, test_failure, code_review, manual",
				IndexFilterable: indexFilterable,
				Tokenization:    "field",
				ModuleConfig: map[string]interface{}{
					"text2vec-transformers": map[string]interface{}{
						"skip": true,
					},
				},
			},
			{
				Name:        "createdAt",
				DataType:    []string{"date"},
				Description: "When the memory was first stored",
				ModuleConfig: map[string]interface{}{
					"text2vec-transformers": map[string]interface{}{
						"skip": true,
					},
				},
			},
			{
				Name:        "lastUsed",
				DataType:    []string{"date"},
				Description: "When the memory was last retrieved",
				ModuleConfig: map[string]interface{}{
					"text2vec-transformers": map[string]interface{}{
						"skip": true,
					},
				},
			},
			{
				Name:        "useCount",
				DataType:    []string{"int"},
				Description: "How many times this memory has been retrieved",
				ModuleConfig: map[string]interface{}{
					"text2vec-transformers": map[string]interface{}{
						"skip": true,
					},
				},
			},
			{
				Name:            "dataSpace",
				DataType:        []string{"text"},
				Description:     "Project isolation key",
				IndexFilterable: indexFilterable,
				Tokenization:    "field",
				ModuleConfig: map[string]interface{}{
					"text2vec-transformers": map[string]interface{}{
						"skip": true,
					},
				},
			},
			{
				Name:            "status",
				DataType:        []string{"text"},
				Description:     "Lifecycle status: active, archived",
				IndexFilterable: indexFilterable,
				Tokenization:    "field",
				ModuleConfig: map[string]interface{}{
					"text2vec-transformers": map[string]interface{}{
						"skip": true,
					},
				},
			},
		},
	}
}

// EnsureCodeMemorySchema creates the CodeMemory class if it doesn't exist.
//
// Description:
//
//	Checks if the CodeMemory class exists in Weaviate and creates it if not.
//	This operation is idempotent.
//
// Inputs:
//
//	ctx - Context for cancellation
//	client - Weaviate client
//
// Outputs:
//
//	error - Non-nil if schema creation fails
func EnsureCodeMemorySchema(ctx context.Context, client *weaviate.Client) error {
	schema := GetCodeMemorySchema()

	// Check if class already exists
	_, err := client.Schema().ClassGetter().WithClassName(CodeMemoryClassName).Do(ctx)
	if err == nil {
		slog.Info("CodeMemory schema already exists")
		return nil
	}

	// Create the class
	slog.Info("Creating CodeMemory schema")
	if err := client.Schema().ClassCreator().WithClass(schema).Do(ctx); err != nil {
		return fmt.Errorf("creating CodeMemory schema: %w", err)
	}

	slog.Info("CodeMemory schema created successfully")
	return nil
}

// DeleteCodeMemorySchema removes the CodeMemory class from Weaviate.
//
// Description:
//
//	Deletes the CodeMemory class and all its objects.
//	Use with caution - this is irreversible.
//
// Inputs:
//
//	ctx - Context for cancellation
//	client - Weaviate client
//
// Outputs:
//
//	error - Non-nil if deletion fails
func DeleteCodeMemorySchema(ctx context.Context, client *weaviate.Client) error {
	if err := client.Schema().ClassDeleter().WithClassName(CodeMemoryClassName).Do(ctx); err != nil {
		return fmt.Errorf("deleting CodeMemory schema: %w", err)
	}

	slog.Info("CodeMemory schema deleted")
	return nil
}
