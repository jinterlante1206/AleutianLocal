// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package seeder

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/weaviate/weaviate-go-client/v5/weaviate"
	"github.com/weaviate/weaviate-go-client/v5/weaviate/filters"
	"github.com/weaviate/weaviate-go-client/v5/weaviate/graphql"
	"github.com/weaviate/weaviate/entities/models"
)

// LibraryDocClassName is the Weaviate class name for library documentation.
const LibraryDocClassName = "LibraryDoc"

// BatchSize is the number of documents to batch import at once.
const BatchSize = 100

// GetLibraryDocSchema returns the Weaviate schema for LibraryDoc class.
func GetLibraryDocSchema() *models.Class {
	indexFilterable := new(bool)
	*indexFilterable = true

	return &models.Class{
		Class:       LibraryDocClassName,
		Description: "Documentation for library symbols (functions, types, etc.)",
		Vectorizer:  "none",
		InvertedIndexConfig: &models.InvertedIndexConfig{
			IndexTimestamps: true,
		},
		Properties: []*models.Property{
			{
				Name:            "docId",
				DataType:        []string{"text"},
				Description:     "Unique identifier: library@version:symbol_path",
				IndexFilterable: indexFilterable,
				Tokenization:    "field",
			},
			{
				Name:            "library",
				DataType:        []string{"text"},
				Description:     "Library/module path (e.g., github.com/gin-gonic/gin)",
				IndexFilterable: indexFilterable,
				Tokenization:    "field",
			},
			{
				Name:            "version",
				DataType:        []string{"text"},
				Description:     "Library version (e.g., v1.9.1)",
				IndexFilterable: indexFilterable,
				Tokenization:    "field",
			},
			{
				Name:         "symbolPath",
				DataType:     []string{"text"},
				Description:  "Fully qualified symbol path (e.g., gin.Context.JSON)",
				Tokenization: "word",
			},
			{
				Name:            "symbolKind",
				DataType:        []string{"text"},
				Description:     "Type of symbol: function, method, type, constant, variable",
				IndexFilterable: indexFilterable,
				Tokenization:    "field",
			},
			{
				Name:         "signature",
				DataType:     []string{"text"},
				Description:  "Type signature (e.g., func(code int, obj interface{}))",
				Tokenization: "word",
			},
			{
				Name:         "docContent",
				DataType:     []string{"text"},
				Description:  "Documentation text",
				Tokenization: "word",
			},
			{
				Name:         "example",
				DataType:     []string{"text"},
				Description:  "Optional code example",
				Tokenization: "word",
			},
			{
				Name:            "dataSpace",
				DataType:        []string{"text"},
				Description:     "Data space for multi-tenant isolation",
				IndexFilterable: indexFilterable,
				Tokenization:    "field",
			},
		},
	}
}

// EnsureSchema creates the LibraryDoc class if it doesn't exist.
//
// Description:
//
//	Checks if the LibraryDoc class exists in Weaviate and creates it if not.
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
func EnsureSchema(ctx context.Context, client *weaviate.Client) error {
	schema := GetLibraryDocSchema()

	// Check if class already exists
	_, err := client.Schema().ClassGetter().WithClassName(LibraryDocClassName).Do(ctx)
	if err == nil {
		slog.Info("LibraryDoc schema already exists")
		return nil
	}

	// Create the class
	slog.Info("Creating LibraryDoc schema")
	if err := client.Schema().ClassCreator().WithClass(schema).Do(ctx); err != nil {
		return fmt.Errorf("creating LibraryDoc schema: %w", err)
	}

	slog.Info("LibraryDoc schema created successfully")
	return nil
}

// IndexDocs batch imports library documentation into Weaviate.
//
// Description:
//
//	Imports documents in batches for efficiency. Handles duplicates
//	gracefully by using the docId as a deterministic ID.
//
// Inputs:
//
//	ctx - Context for cancellation
//	client - Weaviate client
//	docs - Documents to index
//
// Outputs:
//
//	int - Number of documents successfully indexed
//	error - Non-nil if batch import fails
func IndexDocs(ctx context.Context, client *weaviate.Client, docs []LibraryDoc) (int, error) {
	if len(docs) == 0 {
		return 0, nil
	}

	indexed := 0

	// Process in batches
	for i := 0; i < len(docs); i += BatchSize {
		if err := ctx.Err(); err != nil {
			return indexed, err
		}

		end := i + BatchSize
		if end > len(docs) {
			end = len(docs)
		}
		batch := docs[i:end]

		// Create objects for batch
		objects := make([]*models.Object, len(batch))
		for j, doc := range batch {
			objects[j] = &models.Object{
				Class: LibraryDocClassName,
				Properties: map[string]interface{}{
					"docId":      doc.DocID,
					"library":    doc.Library,
					"version":    doc.Version,
					"symbolPath": doc.SymbolPath,
					"symbolKind": doc.SymbolKind,
					"signature":  doc.Signature,
					"docContent": doc.DocContent,
					"example":    doc.Example,
					"dataSpace":  doc.DataSpace,
				},
			}
		}

		// Execute batch import
		result, err := client.Batch().ObjectsBatcher().WithObjects(objects...).Do(ctx)
		if err != nil {
			return indexed, fmt.Errorf("batch import failed: %w", err)
		}

		// Count successes
		for _, obj := range result {
			if obj.Result != nil && obj.Result.Errors == nil {
				indexed++
			}
		}

		slog.Info("Indexed batch", "count", len(batch), "total_indexed", indexed)
	}

	return indexed, nil
}

// SearchDocs searches library documentation in Weaviate.
//
// Description:
//
//	Searches for library documentation matching the query. Supports
//	filtering by library name and data space.
//
// Inputs:
//
//	ctx - Context for cancellation
//	client - Weaviate client
//	query - Search query
//	library - Optional library filter (empty = all libraries)
//	dataSpace - Data space filter
//	limit - Maximum number of results
//
// Outputs:
//
//	[]LibraryDoc - Matching documents
//	error - Non-nil if search fails
func SearchDocs(ctx context.Context, client *weaviate.Client, query, library, dataSpace string, limit int) ([]LibraryDoc, error) {
	if limit <= 0 {
		limit = 10
	}

	// Build where filter
	var whereFilter *filters.WhereBuilder
	if dataSpace != "" {
		whereFilter = filters.Where().
			WithPath([]string{"dataSpace"}).
			WithOperator(filters.Equal).
			WithValueString(dataSpace)

		if library != "" {
			whereFilter = filters.Where().
				WithOperator(filters.And).
				WithOperands([]*filters.WhereBuilder{
					whereFilter,
					filters.Where().
						WithPath([]string{"library"}).
						WithOperator(filters.Equal).
						WithValueString(library),
				})
		}
	} else if library != "" {
		whereFilter = filters.Where().
			WithPath([]string{"library"}).
			WithOperator(filters.Equal).
			WithValueString(library)
	}

	// Build query
	fields := []graphql.Field{
		{Name: "docId"},
		{Name: "library"},
		{Name: "version"},
		{Name: "symbolPath"},
		{Name: "symbolKind"},
		{Name: "signature"},
		{Name: "docContent"},
		{Name: "example"},
		{Name: "dataSpace"},
	}

	getBuilder := client.GraphQL().Get().
		WithClassName(LibraryDocClassName).
		WithFields(fields...).
		WithBM25(client.GraphQL().Bm25ArgBuilder().WithQuery(query)).
		WithLimit(limit)

	if whereFilter != nil {
		getBuilder = getBuilder.WithWhere(whereFilter)
	}

	result, err := getBuilder.Do(ctx)
	if err != nil {
		return nil, fmt.Errorf("search failed: %w", err)
	}

	if result.Errors != nil && len(result.Errors) > 0 {
		return nil, fmt.Errorf("search error: %s", result.Errors[0].Message)
	}

	// Parse results
	data, ok := result.Data["Get"].(map[string]interface{})
	if !ok {
		return []LibraryDoc{}, nil // No results
	}
	objects, ok := data[LibraryDocClassName].([]interface{})
	if !ok {
		return []LibraryDoc{}, nil // No results
	}

	docs := make([]LibraryDoc, 0, len(objects))
	for _, obj := range objects {
		m, ok := obj.(map[string]interface{})
		if !ok {
			continue // skip malformed objects
		}
		docs = append(docs, LibraryDoc{
			DocID:      getString(m, "docId"),
			Library:    getString(m, "library"),
			Version:    getString(m, "version"),
			SymbolPath: getString(m, "symbolPath"),
			SymbolKind: getString(m, "symbolKind"),
			Signature:  getString(m, "signature"),
			DocContent: getString(m, "docContent"),
			Example:    getString(m, "example"),
			DataSpace:  getString(m, "dataSpace"),
		})
	}

	return docs, nil
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

// DeleteByDataSpace removes all library docs for a data space.
//
// Description:
//
//	Deletes all LibraryDoc objects matching the given data space.
//	Used for cleanup when a project is removed.
//
// Inputs:
//
//	ctx - Context for cancellation
//	client - Weaviate client
//	dataSpace - Data space to delete
//
// Outputs:
//
//	error - Non-nil if deletion fails
func DeleteByDataSpace(ctx context.Context, client *weaviate.Client, dataSpace string) error {
	whereFilter := filters.Where().
		WithPath([]string{"dataSpace"}).
		WithOperator(filters.Equal).
		WithValueString(dataSpace)

	_, err := client.Batch().ObjectsBatchDeleter().
		WithClassName(LibraryDocClassName).
		WithWhere(whereFilter).
		Do(ctx)

	if err != nil {
		return fmt.Errorf("deleting by data space: %w", err)
	}

	slog.Info("Deleted library docs by data space", "dataSpace", dataSpace)
	return nil
}
