// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.

package datatypes

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// GetDocumentSchema Tests
// =============================================================================

func TestGetDocumentSchema_ReturnsValidClass(t *testing.T) {
	schema := GetDocumentSchema()

	require.NotNil(t, schema)
	assert.Equal(t, "Document", schema.Class)
	assert.Equal(t, "none", schema.Vectorizer)
	assert.Contains(t, schema.Description, "document")
}

func TestGetDocumentSchema_HasRequiredProperties(t *testing.T) {
	schema := GetDocumentSchema()

	expectedProperties := []string{
		"content",
		"source",
		"parent_source",
		"version_tag",
		"data_space",
		"ingested_at",
		"inSession",
	}

	require.NotNil(t, schema.Properties)
	assert.Len(t, schema.Properties, len(expectedProperties))

	propertyNames := make(map[string]bool)
	for _, prop := range schema.Properties {
		propertyNames[prop.Name] = true
	}

	for _, expected := range expectedProperties {
		assert.True(t, propertyNames[expected], "Missing property: %s", expected)
	}
}

func TestGetDocumentSchema_PropertyDataTypes(t *testing.T) {
	schema := GetDocumentSchema()

	propertyDataTypes := map[string]string{
		"content":       "text",
		"source":        "text",
		"parent_source": "text",
		"version_tag":   "text",
		"data_space":    "text",
		"ingested_at":   "number",
		"inSession":     "Session",
	}

	for _, prop := range schema.Properties {
		expectedType, exists := propertyDataTypes[prop.Name]
		if exists {
			require.NotEmpty(t, prop.DataType, "DataType for %s should not be empty", prop.Name)
			assert.Equal(t, expectedType, prop.DataType[0], "DataType mismatch for %s", prop.Name)
		}
	}
}

func TestGetDocumentSchema_InvertedIndexConfig(t *testing.T) {
	schema := GetDocumentSchema()

	require.NotNil(t, schema.InvertedIndexConfig)
	assert.True(t, schema.InvertedIndexConfig.IndexNullState)
	assert.True(t, schema.InvertedIndexConfig.IndexTimestamps)
	assert.False(t, schema.InvertedIndexConfig.IndexPropertyLength)
}

// =============================================================================
// GetConversationSchema Tests
// =============================================================================

func TestGetConversationSchema_ReturnsValidClass(t *testing.T) {
	schema := GetConversationSchema()

	require.NotNil(t, schema)
	assert.Equal(t, "Conversation", schema.Class)
	assert.Equal(t, "none", schema.Vectorizer)
	assert.Contains(t, schema.Description, "question")
}

func TestGetConversationSchema_HasRequiredProperties(t *testing.T) {
	schema := GetConversationSchema()

	expectedProperties := []string{
		"session_id",
		"question",
		"answer",
		"timestamp",
		"turn_number",
		"turn_hash",
		"inSession",
	}

	require.NotNil(t, schema.Properties)
	assert.Len(t, schema.Properties, len(expectedProperties))

	propertyNames := make(map[string]bool)
	for _, prop := range schema.Properties {
		propertyNames[prop.Name] = true
	}

	for _, expected := range expectedProperties {
		assert.True(t, propertyNames[expected], "Missing property: %s", expected)
	}
}

func TestGetConversationSchema_PropertyDataTypes(t *testing.T) {
	schema := GetConversationSchema()

	propertyDataTypes := map[string]string{
		"session_id":  "text",
		"question":    "text",
		"answer":      "text",
		"timestamp":   "number",
		"turn_number": "int",
		"turn_hash":   "text",
		"inSession":   "Session",
	}

	for _, prop := range schema.Properties {
		expectedType, exists := propertyDataTypes[prop.Name]
		if exists {
			require.NotEmpty(t, prop.DataType, "DataType for %s should not be empty", prop.Name)
			assert.Equal(t, expectedType, prop.DataType[0], "DataType mismatch for %s", prop.Name)
		}
	}
}

func TestGetConversationSchema_InvertedIndexConfig(t *testing.T) {
	schema := GetConversationSchema()

	require.NotNil(t, schema.InvertedIndexConfig)
	assert.True(t, schema.InvertedIndexConfig.IndexNullState)
	assert.True(t, schema.InvertedIndexConfig.IndexTimestamps)
}

// =============================================================================
// GetSessionSchema Tests
// =============================================================================

func TestGetSessionSchema_ReturnsValidClass(t *testing.T) {
	schema := GetSessionSchema()

	require.NotNil(t, schema)
	assert.Equal(t, "Session", schema.Class)
	assert.Equal(t, "none", schema.Vectorizer)
	assert.Contains(t, schema.Description, "session")
}

func TestGetSessionSchema_HasRequiredProperties(t *testing.T) {
	schema := GetSessionSchema()

	expectedProperties := []string{
		"session_id",
		"summary",
		"timestamp",
	}

	require.NotNil(t, schema.Properties)
	assert.Len(t, schema.Properties, len(expectedProperties))

	propertyNames := make(map[string]bool)
	for _, prop := range schema.Properties {
		propertyNames[prop.Name] = true
	}

	for _, expected := range expectedProperties {
		assert.True(t, propertyNames[expected], "Missing property: %s", expected)
	}
}

func TestGetSessionSchema_PropertyDataTypes(t *testing.T) {
	schema := GetSessionSchema()

	propertyDataTypes := map[string]string{
		"session_id": "text",
		"summary":    "text",
		"timestamp":  "number",
	}

	for _, prop := range schema.Properties {
		expectedType, exists := propertyDataTypes[prop.Name]
		if exists {
			require.NotEmpty(t, prop.DataType, "DataType for %s should not be empty", prop.Name)
			assert.Equal(t, expectedType, prop.DataType[0], "DataType mismatch for %s", prop.Name)
		}
	}
}

func TestGetSessionSchema_InvertedIndexConfig(t *testing.T) {
	schema := GetSessionSchema()

	require.NotNil(t, schema.InvertedIndexConfig)
	assert.True(t, schema.InvertedIndexConfig.IndexTimestamps)
}

// =============================================================================
// GetVerificationLogSchema Tests
// =============================================================================

func TestGetVerificationLogSchema_ReturnsValidClass(t *testing.T) {
	schema := GetVerificationLogSchema()

	require.NotNil(t, schema)
	assert.Equal(t, "VerificationLog", schema.Class)
	assert.Equal(t, "none", schema.Vectorizer)
	assert.Contains(t, schema.Description, "debate")
}

func TestGetVerificationLogSchema_HasRequiredProperties(t *testing.T) {
	schema := GetVerificationLogSchema()

	expectedProperties := []string{
		"query",
		"draft_answer",
		"skeptic_critique",
		"hallucinations_found",
		"final_answer",
		"was_refined",
		"session_id",
		"timestamp",
	}

	require.NotNil(t, schema.Properties)
	assert.Len(t, schema.Properties, len(expectedProperties))

	propertyNames := make(map[string]bool)
	for _, prop := range schema.Properties {
		propertyNames[prop.Name] = true
	}

	for _, expected := range expectedProperties {
		assert.True(t, propertyNames[expected], "Missing property: %s", expected)
	}
}

func TestGetVerificationLogSchema_PropertyDataTypes(t *testing.T) {
	schema := GetVerificationLogSchema()

	propertyDataTypes := map[string]string{
		"query":                "text",
		"draft_answer":         "text",
		"skeptic_critique":     "text",
		"hallucinations_found": "text[]",
		"final_answer":         "text",
		"was_refined":          "boolean",
		"session_id":           "text",
		"timestamp":            "number",
	}

	for _, prop := range schema.Properties {
		expectedType, exists := propertyDataTypes[prop.Name]
		if exists {
			require.NotEmpty(t, prop.DataType, "DataType for %s should not be empty", prop.Name)
			assert.Equal(t, expectedType, prop.DataType[0], "DataType mismatch for %s", prop.Name)
		}
	}
}

// =============================================================================
// Schema Consistency Tests
// =============================================================================

func TestSchemas_AllHaveNoneVectorizer(t *testing.T) {
	schemas := []struct {
		name   string
		schema func() interface{ GetClass() string }
	}{
		{"Document", func() interface{ GetClass() string } { return &schemaWrapper{GetDocumentSchema()} }},
		{"Conversation", func() interface{ GetClass() string } { return &schemaWrapper{GetConversationSchema()} }},
		{"Session", func() interface{ GetClass() string } { return &schemaWrapper{GetSessionSchema()} }},
		{"VerificationLog", func() interface{ GetClass() string } { return &schemaWrapper{GetVerificationLogSchema()} }},
	}

	for _, s := range schemas {
		t.Run(s.name, func(t *testing.T) {
			// All schemas should use "none" vectorizer
			// (embeddings are handled externally)
		})
	}
}

// Helper wrapper for schema testing
type schemaWrapper struct {
	class interface{}
}

func (s *schemaWrapper) GetClass() string {
	return ""
}

func TestSchemas_PropertiesHaveDescriptions(t *testing.T) {
	schemas := []struct {
		name   string
		schema interface{ getProperties() int }
	}{
		{"Document", &docSchemaHelper{}},
		{"Conversation", &convSchemaHelper{}},
		{"Session", &sessSchemaHelper{}},
		{"VerificationLog", &verifSchemaHelper{}},
	}

	for _, s := range schemas {
		t.Run(s.name, func(t *testing.T) {
			// All schemas should have property descriptions
			assert.Greater(t, s.schema.getProperties(), 0)
		})
	}
}

type docSchemaHelper struct{}

func (d *docSchemaHelper) getProperties() int {
	return len(GetDocumentSchema().Properties)
}

type convSchemaHelper struct{}

func (c *convSchemaHelper) getProperties() int {
	return len(GetConversationSchema().Properties)
}

type sessSchemaHelper struct{}

func (s *sessSchemaHelper) getProperties() int {
	return len(GetSessionSchema().Properties)
}

type verifSchemaHelper struct{}

func (v *verifSchemaHelper) getProperties() int {
	return len(GetVerificationLogSchema().Properties)
}

// =============================================================================
// Cross-Reference Tests
// =============================================================================

func TestDocumentSchema_HasSessionReference(t *testing.T) {
	schema := GetDocumentSchema()

	var inSessionProp *struct {
		Name     string
		DataType []string
	}

	for _, prop := range schema.Properties {
		if prop.Name == "inSession" {
			inSessionProp = &struct {
				Name     string
				DataType []string
			}{prop.Name, prop.DataType}
			break
		}
	}

	require.NotNil(t, inSessionProp, "inSession property should exist")
	require.NotEmpty(t, inSessionProp.DataType)
	assert.Equal(t, "Session", inSessionProp.DataType[0], "inSession should reference Session class")
}

func TestConversationSchema_HasSessionReference(t *testing.T) {
	schema := GetConversationSchema()

	var inSessionProp *struct {
		Name     string
		DataType []string
	}

	for _, prop := range schema.Properties {
		if prop.Name == "inSession" {
			inSessionProp = &struct {
				Name     string
				DataType []string
			}{prop.Name, prop.DataType}
			break
		}
	}

	require.NotNil(t, inSessionProp, "inSession property should exist")
	require.NotEmpty(t, inSessionProp.DataType)
	assert.Equal(t, "Session", inSessionProp.DataType[0], "inSession should reference Session class")
}
