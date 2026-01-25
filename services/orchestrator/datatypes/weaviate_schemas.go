// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package datatypes

import (
	"context"
	"log"
	"log/slog"

	"github.com/weaviate/weaviate-go-client/v5/weaviate"
	"github.com/weaviate/weaviate/entities/models"
)

func GetDocumentSchema() *models.Class {
	indexFilterable := new(bool)
	*indexFilterable = true

	return &models.Class{
		Class:       "Document",
		Description: "A document containing text content and its source.",
		Vectorizer:  "none",
		InvertedIndexConfig: &models.InvertedIndexConfig{
			Bm25:                   nil,
			CleanupIntervalSeconds: 0,
			IndexNullState:         true,
			IndexPropertyLength:    false,
			IndexTimestamps:        true,
			Stopwords:              nil,
			UsingBlockMaxWAND:      false,
		},
		Properties: []*models.Property{
			{
				Name:         "content",
				DataType:     []string{"text"},
				Description:  "The main content of the document.",
				Tokenization: "word",
			},
			{
				Name:            "source",
				DataType:        []string{"text"},
				Description:     "The original file path or source of the document.",
				IndexFilterable: indexFilterable,
				Tokenization:    "field",
			},
			{
				Name:            "parent_source",
				DataType:        []string{"text"},
				Description:     "The original parent file",
				IndexFilterable: indexFilterable,
				Tokenization:    "field",
			},
			{
				Name:            "version_tag",
				DataType:        []string{"text"},
				Description:     "A version tag (e.g., 'v1', 'v2') for this document.",
				IndexFilterable: indexFilterable,
				Tokenization:    "field",
			},
			{
				Name:            "version_number",
				DataType:        []string{"int"},
				Description:     "Numeric version for ordering (1, 2, 3...). Auto-incremented on re-ingest.",
				IndexFilterable: indexFilterable,
			},
			{
				Name:            "is_current",
				DataType:        []string{"boolean"},
				Description:     "True if this is the latest version of the document.",
				IndexFilterable: indexFilterable,
			},
			{
				Name:            "data_space",
				DataType:        []string{"text"},
				Description:     "Logical data space for segmentation (e.g., 'work', 'personal').",
				IndexFilterable: indexFilterable,
				Tokenization:    "field",
			},
			{
				Name:            "ingested_at",
				DataType:        []string{"number"},
				Description:     "Timestamp (Unix ms) of when the chunk was ingested.",
				IndexFilterable: indexFilterable,
			},
			{
				Name:            "turn_number",
				DataType:        []string{"int"},
				Description:     "Turn number within conversation (for chat_memory filtering).",
				IndexFilterable: indexFilterable,
			},
			{
				Name:            "ttl_expires_at",
				DataType:        []string{"number"},
				Description:     "Unix milliseconds timestamp when document expires. 0 = never expires.",
				IndexFilterable: indexFilterable,
			},
			{
				Name:     "inSession",
				DataType: []string{"Session"},
				Description: "A direct graph link to the parent Session object (" +
					"if this is a session-scoped document)",
				IndexFilterable: indexFilterable,
			},
		},
	}
}

func GetConversationSchema() *models.Class {
	indexFilterable := new(bool)
	*indexFilterable = true

	return &models.Class{
		Class:       "Conversation",
		Description: "A record of a user question and the AI's answer.",
		Vectorizer:  "none",
		InvertedIndexConfig: &models.InvertedIndexConfig{
			Bm25:                   nil,
			CleanupIntervalSeconds: 0,
			IndexNullState:         true,
			IndexPropertyLength:    false,
			IndexTimestamps:        true,
			Stopwords:              nil,
			UsingBlockMaxWAND:      false,
		},
		Properties: []*models.Property{
			{
				Name:            "session_id",
				DataType:        []string{"text"},
				Description:     "The unique ID for the conversation session.",
				IndexFilterable: indexFilterable,
			},
			{
				Name:         "question",
				DataType:     []string{"text"},
				Description:  "The user's query to the LLM",
				Tokenization: "word",
			},
			{
				Name:         "answer",
				DataType:     []string{"text"},
				Description:  "The LLMs response",
				Tokenization: "word",
			},
			{
				Name:            "timestamp",
				DataType:        []string{"number"},
				Description:     "The timestamp of the conversation action.",
				IndexFilterable: indexFilterable,
			},
			{
				Name:            "turn_number",
				DataType:        []string{"int"},
				Description:     "The sequential turn number within the session (1-indexed).",
				IndexFilterable: indexFilterable,
			},
			{
				Name:            "turn_hash",
				DataType:        []string{"text"},
				Description:     "SHA-256 hash of turn content for integrity verification.",
				IndexFilterable: indexFilterable,
				Tokenization:    "field",
			},
			{
				Name:            "inSession",
				DataType:        []string{"Session"},
				Description:     "A direct graph link to the parent Session object.",
				IndexFilterable: indexFilterable,
			},
		},
	}
}

func GetSessionSchema() *models.Class {
	indexFilterable := new(bool)
	*indexFilterable = true

	return &models.Class{
		Class:               "Session",
		Description:         "Metadata for a single conversation session, including a summary",
		Vectorizer:          "none",
		InvertedIndexConfig: &models.InvertedIndexConfig{IndexTimestamps: true},
		Properties: []*models.Property{
			{
				Name:            "session_id",
				DataType:        []string{"text"},
				Description:     "The unique ID for the conversation session.",
				IndexFilterable: indexFilterable,
				Tokenization:    "field",
			},
			{
				Name:         "summary",
				DataType:     []string{"text"},
				Description:  "A short, LLM-generated summary of the conversation.",
				Tokenization: "word",
			},
			{
				Name:            "timestamp",
				DataType:        []string{"number"},
				Description:     "The timestamp when the session began.",
				IndexFilterable: indexFilterable,
			},
			{
				Name:            "ttl_expires_at",
				DataType:        []string{"number"},
				Description:     "Unix milliseconds timestamp when session expires. 0 = never expires. Resets on each new message.",
				IndexFilterable: indexFilterable,
			},
			{
				Name:            "ttl_duration_ms",
				DataType:        []string{"number"},
				Description:     "Original TTL duration in milliseconds. Used to recalculate ttl_expires_at on activity.",
				IndexFilterable: indexFilterable,
			},
		},
	}
}

// GetDataspaceConfigSchema returns the schema for the DataspaceConfig class.
//
// # Description
//
// DataspaceConfig stores configuration for a logical data space, including
// default retention policies. This enables per-dataspace TTL settings.
//
// # Properties
//
//   - data_space_name: Unique identifier for the data space (e.g., 'work', 'personal').
//   - retention_days: Default retention period in days for new documents. 0 = indefinite.
//   - created_at: Unix milliseconds when this config was created.
//   - modified_at: Unix milliseconds when this config was last modified.
//
// # Example
//
//	config := GetDataspaceConfigSchema()
//	client.Schema().ClassCreator().WithClass(config).Do(ctx)
func GetDataspaceConfigSchema() *models.Class {
	indexFilterable := new(bool)
	*indexFilterable = true

	return &models.Class{
		Class:       "DataspaceConfig",
		Description: "Configuration for a logical data space including retention policies.",
		Vectorizer:  "none",
		InvertedIndexConfig: &models.InvertedIndexConfig{
			IndexTimestamps: true,
		},
		Properties: []*models.Property{
			{
				Name:            "data_space_name",
				DataType:        []string{"text"},
				Description:     "Unique identifier for this data space (e.g., 'work', 'personal').",
				IndexFilterable: indexFilterable,
				Tokenization:    "field",
			},
			{
				Name:            "retention_days",
				DataType:        []string{"int"},
				Description:     "Default retention period in days for new documents. 0 = indefinite.",
				IndexFilterable: indexFilterable,
			},
			{
				Name:            "created_at",
				DataType:        []string{"number"},
				Description:     "Unix milliseconds when this config was created.",
				IndexFilterable: indexFilterable,
			},
			{
				Name:            "modified_at",
				DataType:        []string{"number"},
				Description:     "Unix milliseconds when this config was last modified.",
				IndexFilterable: indexFilterable,
			},
		},
	}
}

func EnsureWeaviateSchema(client *weaviate.Client) {
	// A list of functions that return our schema definitions.
	schemaGetters := []func() *models.Class{
		GetSessionSchema,
		GetDocumentSchema,
		GetConversationSchema,
		GetVerificationLogSchema,
		GetDataspaceConfigSchema,
	}

	for _, getSchema := range schemaGetters {
		class := getSchema()
		slog.Info("Checking schema", "class", class.Class)

		// Check if the class already exists.
		_, err := client.Schema().ClassGetter().WithClassName(class.Class).Do(context.Background())
		if err != nil {
			// If it doesn't exist, the client returns an error. We can now create it.
			slog.Info("Schema not found, creating it...", "class", class.Class)
			err := client.Schema().ClassCreator().WithClass(class).Do(context.Background())
			if err != nil {
				// If we fail to create it, it's a fatal error.
				log.Fatalf("Failed to create schema for class %s: %v", class.Class, err)
			}
			slog.Info("Successfully created schema", "class", class.Class)
		} else {
			slog.Info("Schema already exists", "class", class.Class)
			// TODO: Add logic to check and update properties if they differ?
		}
	}
}

func GetVerificationLogSchema() *models.Class {
	indexFilterable := new(bool)
	*indexFilterable = true

	return &models.Class{
		Class:       "VerificationLog",
		Description: "Logs the debate between Optimist and Skeptic for offline evaluation.",
		Vectorizer:  "none",
		Properties: []*models.Property{
			{
				Name:        "query",
				DataType:    []string{"text"},
				Description: "The original user query",
			},
			{
				Name:        "draft_answer",
				DataType:    []string{"text"},
				Description: "The initial, potentially hallucinated answer from the Optimist",
			},
			{
				Name:        "skeptic_critique",
				DataType:    []string{"text"},
				Description: "The reasoning provided by the Skeptic",
			},
			{
				Name:        "hallucinations_found",
				DataType:    []string{"text[]"}, // Array of strings
				Description: "Specific claims flagged as hallucinations",
			},
			{
				Name:        "final_answer",
				DataType:    []string{"text"},
				Description: "The refined answer after correction",
			},
			{
				Name:        "was_refined",
				DataType:    []string{"boolean"},
				Description: "True if the answer had to be corrected",
			},
			{
				Name:            "session_id",
				DataType:        []string{"text"},
				Description:     "Link to the chat session",
				IndexFilterable: indexFilterable,
			},
			{
				Name:     "timestamp",
				DataType: []string{"number"},
			},
		},
	}
}
