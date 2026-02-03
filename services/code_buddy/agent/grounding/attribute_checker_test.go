// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package grounding

import (
	"context"
	"testing"
)

func TestAttributeChecker_Name(t *testing.T) {
	checker := NewAttributeChecker(nil)
	if checker.Name() != "attribute_checker" {
		t.Errorf("expected name 'attribute_checker', got %s", checker.Name())
	}
}

func TestAttributeChecker_Disabled(t *testing.T) {
	config := &AttributeCheckerConfig{Enabled: false}
	checker := NewAttributeChecker(config)

	input := &CheckInput{
		Response: "Parse returns error",
		EvidenceIndex: &EvidenceIndex{
			SymbolDetails: map[string][]SymbolInfo{
				"Parse": {{
					Name:        "Parse",
					Kind:        "function",
					ReturnTypes: []string{"*Result", "error"},
				}},
			},
		},
	}

	violations := checker.Check(context.Background(), input)
	if len(violations) != 0 {
		t.Errorf("expected no violations when disabled, got %d", len(violations))
	}
}

func TestAttributeChecker_NilInput(t *testing.T) {
	checker := NewAttributeChecker(nil)

	violations := checker.Check(context.Background(), nil)
	if len(violations) != 0 {
		t.Errorf("expected no violations for nil input, got %d", len(violations))
	}
}

func TestAttributeChecker_EmptyResponse(t *testing.T) {
	checker := NewAttributeChecker(nil)

	input := &CheckInput{Response: ""}
	violations := checker.Check(context.Background(), input)
	if len(violations) != 0 {
		t.Errorf("expected no violations for empty response, got %d", len(violations))
	}
}

func TestAttributeChecker_NoSymbolDetails(t *testing.T) {
	checker := NewAttributeChecker(nil)

	input := &CheckInput{
		Response:      "Parse returns error",
		EvidenceIndex: NewEvidenceIndex(),
	}

	violations := checker.Check(context.Background(), input)
	if len(violations) != 0 {
		t.Errorf("expected no violations when no symbol details, got %d", len(violations))
	}
}

func TestAttributeChecker_ReturnType_Correct(t *testing.T) {
	checker := NewAttributeChecker(nil)

	input := &CheckInput{
		Response: "The Parse function returns (*Result, error)",
		EvidenceIndex: &EvidenceIndex{
			SymbolDetails: map[string][]SymbolInfo{
				"Parse": {{
					Name:        "Parse",
					Kind:        "function",
					File:        "parser.go",
					Line:        10,
					ReturnTypes: []string{"*Result", "error"},
				}},
			},
		},
	}

	violations := checker.Check(context.Background(), input)
	if len(violations) != 0 {
		t.Errorf("expected no violations for correct return type, got %d: %+v", len(violations), violations)
	}
}

func TestAttributeChecker_ReturnType_WrongOrder(t *testing.T) {
	checker := NewAttributeChecker(nil)

	// Claim: returns (error, *Result) but actual is (*Result, error)
	input := &CheckInput{
		Response: "The Parse function returns (error, *Result)",
		EvidenceIndex: &EvidenceIndex{
			SymbolDetails: map[string][]SymbolInfo{
				"Parse": {{
					Name:        "Parse",
					Kind:        "function",
					File:        "parser.go",
					Line:        10,
					ReturnTypes: []string{"*Result", "error"},
				}},
			},
		},
	}

	violations := checker.Check(context.Background(), input)
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation for wrong return order, got %d", len(violations))
	}

	v := violations[0]
	if v.Type != ViolationAttributeHallucination {
		t.Errorf("expected ViolationAttributeHallucination, got %s", v.Type)
	}
	if v.Code != "ATTR_WRONG_RETURN_TYPE" {
		t.Errorf("expected code ATTR_WRONG_RETURN_TYPE, got %s", v.Code)
	}
}

func TestAttributeChecker_ReturnType_SingleVsTuple(t *testing.T) {
	checker := NewAttributeChecker(nil)

	// Claim: returns error but actual is (*Result, error)
	input := &CheckInput{
		Response: "The Parse function returns error",
		EvidenceIndex: &EvidenceIndex{
			SymbolDetails: map[string][]SymbolInfo{
				"Parse": {{
					Name:        "Parse",
					Kind:        "function",
					File:        "parser.go",
					Line:        10,
					ReturnTypes: []string{"*Result", "error"},
				}},
			},
		},
	}

	violations := checker.Check(context.Background(), input)
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation for wrong return count, got %d", len(violations))
	}
}

func TestAttributeChecker_ParameterCount_Correct(t *testing.T) {
	checker := NewAttributeChecker(nil)

	input := &CheckInput{
		Response: "The Process function takes 2 arguments",
		EvidenceIndex: &EvidenceIndex{
			SymbolDetails: map[string][]SymbolInfo{
				"Process": {{
					Name:       "Process",
					Kind:       "function",
					File:       "handler.go",
					Line:       25,
					Parameters: []string{"context.Context", "string"},
				}},
			},
		},
	}

	violations := checker.Check(context.Background(), input)
	if len(violations) != 0 {
		t.Errorf("expected no violations for correct param count, got %d", len(violations))
	}
}

func TestAttributeChecker_ParameterCount_Wrong(t *testing.T) {
	checker := NewAttributeChecker(nil)

	// Claim: takes 3 arguments but actual is 2
	input := &CheckInput{
		Response: "The Process function takes 3 arguments",
		EvidenceIndex: &EvidenceIndex{
			SymbolDetails: map[string][]SymbolInfo{
				"Process": {{
					Name:       "Process",
					Kind:       "function",
					File:       "handler.go",
					Line:       25,
					Parameters: []string{"context.Context", "string"},
				}},
			},
		},
	}

	violations := checker.Check(context.Background(), input)
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation for wrong param count, got %d", len(violations))
	}

	v := violations[0]
	if v.Code != "ATTR_WRONG_PARAM_COUNT" {
		t.Errorf("expected code ATTR_WRONG_PARAM_COUNT, got %s", v.Code)
	}
}

func TestAttributeChecker_FieldCount_Correct(t *testing.T) {
	checker := NewAttributeChecker(nil)

	input := &CheckInput{
		Response: "The Config struct has 3 fields",
		EvidenceIndex: &EvidenceIndex{
			SymbolDetails: map[string][]SymbolInfo{
				"Config": {{
					Name:   "Config",
					Kind:   "struct",
					File:   "config.go",
					Line:   5,
					Fields: []string{"Name", "Value", "Timeout"},
				}},
			},
		},
	}

	violations := checker.Check(context.Background(), input)
	if len(violations) != 0 {
		t.Errorf("expected no violations for correct field count, got %d", len(violations))
	}
}

func TestAttributeChecker_FieldCount_Wrong(t *testing.T) {
	checker := NewAttributeChecker(nil)

	// Claim: has 5 fields but actual is 3
	input := &CheckInput{
		Response: "The Config struct has 5 fields",
		EvidenceIndex: &EvidenceIndex{
			SymbolDetails: map[string][]SymbolInfo{
				"Config": {{
					Name:   "Config",
					Kind:   "struct",
					File:   "config.go",
					Line:   5,
					Fields: []string{"Name", "Value", "Timeout"},
				}},
			},
		},
	}

	violations := checker.Check(context.Background(), input)
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation for wrong field count, got %d", len(violations))
	}

	v := violations[0]
	if v.Code != "ATTR_WRONG_FIELD_COUNT" {
		t.Errorf("expected code ATTR_WRONG_FIELD_COUNT, got %s", v.Code)
	}
}

func TestAttributeChecker_FieldNames_AllExist(t *testing.T) {
	checker := NewAttributeChecker(nil)

	input := &CheckInput{
		Response: "The Config struct has fields: Name, Value",
		EvidenceIndex: &EvidenceIndex{
			SymbolDetails: map[string][]SymbolInfo{
				"Config": {{
					Name:   "Config",
					Kind:   "struct",
					File:   "config.go",
					Line:   5,
					Fields: []string{"Name", "Value", "Timeout"},
				}},
			},
		},
	}

	violations := checker.Check(context.Background(), input)
	if len(violations) != 0 {
		t.Errorf("expected no violations for existing fields, got %d", len(violations))
	}
}

func TestAttributeChecker_FieldNames_SomeMissing(t *testing.T) {
	checker := NewAttributeChecker(nil)

	// Claim: has fields Name, Value, Debug but actual doesn't have Debug
	input := &CheckInput{
		Response: "The Config struct has fields: Name, Value, Debug",
		EvidenceIndex: &EvidenceIndex{
			SymbolDetails: map[string][]SymbolInfo{
				"Config": {{
					Name:   "Config",
					Kind:   "struct",
					File:   "config.go",
					Line:   5,
					Fields: []string{"Name", "Value", "Timeout"},
				}},
			},
		},
	}

	violations := checker.Check(context.Background(), input)
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation for missing field, got %d", len(violations))
	}

	v := violations[0]
	if v.Code != "ATTR_WRONG_FIELD_NAMES" {
		t.Errorf("expected code ATTR_WRONG_FIELD_NAMES, got %s", v.Code)
	}
}

func TestAttributeChecker_MethodCount_Correct(t *testing.T) {
	checker := NewAttributeChecker(nil)

	input := &CheckInput{
		Response: "The Reader interface defines 2 methods",
		EvidenceIndex: &EvidenceIndex{
			SymbolDetails: map[string][]SymbolInfo{
				"Reader": {{
					Name:    "Reader",
					Kind:    "interface",
					File:    "io.go",
					Line:    15,
					Methods: []string{"Read", "Close"},
				}},
			},
		},
	}

	violations := checker.Check(context.Background(), input)
	if len(violations) != 0 {
		t.Errorf("expected no violations for correct method count, got %d", len(violations))
	}
}

func TestAttributeChecker_MethodCount_Wrong(t *testing.T) {
	checker := NewAttributeChecker(nil)

	// Claim: has 4 methods but actual is 2
	input := &CheckInput{
		Response: "The Reader interface has 4 methods",
		EvidenceIndex: &EvidenceIndex{
			SymbolDetails: map[string][]SymbolInfo{
				"Reader": {{
					Name:    "Reader",
					Kind:    "interface",
					File:    "io.go",
					Line:    15,
					Methods: []string{"Read", "Close"},
				}},
			},
		},
	}

	violations := checker.Check(context.Background(), input)
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation for wrong method count, got %d", len(violations))
	}

	v := violations[0]
	if v.Code != "ATTR_WRONG_METHOD_COUNT" {
		t.Errorf("expected code ATTR_WRONG_METHOD_COUNT, got %s", v.Code)
	}
}

func TestAttributeChecker_SymbolNotInEvidence(t *testing.T) {
	checker := NewAttributeChecker(nil)

	// Claim about Unknown which isn't in evidence
	input := &CheckInput{
		Response: "The Unknown function returns error",
		EvidenceIndex: &EvidenceIndex{
			SymbolDetails: map[string][]SymbolInfo{
				"Parse": {{
					Name:        "Parse",
					Kind:        "function",
					ReturnTypes: []string{"*Result", "error"},
				}},
			},
		},
	}

	violations := checker.Check(context.Background(), input)
	if len(violations) != 0 {
		t.Errorf("expected no violations for symbol not in evidence, got %d", len(violations))
	}
}

func TestAttributeChecker_WrongSymbolKind(t *testing.T) {
	checker := NewAttributeChecker(nil)

	// Claim about return type on a struct (wrong kind)
	input := &CheckInput{
		Response: "The Config function returns error",
		EvidenceIndex: &EvidenceIndex{
			SymbolDetails: map[string][]SymbolInfo{
				"Config": {{
					Name:   "Config",
					Kind:   "struct",
					Fields: []string{"Name"},
				}},
			},
		},
	}

	violations := checker.Check(context.Background(), input)
	// Should skip validation since Config is a struct, not function
	if len(violations) != 0 {
		t.Errorf("expected no violations for wrong symbol kind, got %d", len(violations))
	}
}

func TestAttributeChecker_MultipleSymbolLocations(t *testing.T) {
	checker := NewAttributeChecker(nil)

	// Same symbol in multiple files with different signatures
	input := &CheckInput{
		Response: "The Parse function returns error",
		EvidenceIndex: &EvidenceIndex{
			SymbolDetails: map[string][]SymbolInfo{
				"Parse": {
					{
						Name:        "Parse",
						Kind:        "function",
						File:        "json_parser.go",
						ReturnTypes: []string{"*JSONResult", "error"},
					},
					{
						Name:        "Parse",
						Kind:        "function",
						File:        "xml_parser.go",
						ReturnTypes: []string{"error"}, // This matches the claim
					},
				},
			},
		},
	}

	violations := checker.Check(context.Background(), input)
	// Should not violate since one of the locations matches
	if len(violations) != 0 {
		t.Errorf("expected no violations when any location matches, got %d", len(violations))
	}
}

func TestAttributeChecker_ContextCancellation(t *testing.T) {
	checker := NewAttributeChecker(nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	input := &CheckInput{
		Response: "Parse returns error",
		EvidenceIndex: &EvidenceIndex{
			SymbolDetails: map[string][]SymbolInfo{
				"Parse": {{Name: "Parse", Kind: "function", ReturnTypes: []string{"*Result"}}},
			},
		},
	}

	violations := checker.Check(ctx, input)
	// Should return early due to cancellation
	if len(violations) > 0 {
		t.Logf("got %d violations before cancellation check", len(violations))
	}
}

func TestAttributeChecker_MaxClaimsLimit(t *testing.T) {
	config := &AttributeCheckerConfig{
		Enabled:          true,
		MaxClaimsToCheck: 2,
		MinSymbolLength:  3,
	}
	checker := NewAttributeChecker(config)

	// Many claims but only first 2 should be checked
	input := &CheckInput{
		Response: "Alpha takes 5 args. Beta takes 5 args. Gamma takes 5 args. Delta takes 5 args.",
		EvidenceIndex: &EvidenceIndex{
			SymbolDetails: map[string][]SymbolInfo{
				"Alpha": {{Name: "Alpha", Kind: "function", Parameters: []string{"int"}}},
				"Beta":  {{Name: "Beta", Kind: "function", Parameters: []string{"int"}}},
				"Gamma": {{Name: "Gamma", Kind: "function", Parameters: []string{"int"}}},
				"Delta": {{Name: "Delta", Kind: "function", Parameters: []string{"int"}}},
			},
		},
	}

	violations := checker.Check(context.Background(), input)
	// Should only check first 2 claims due to limit
	if len(violations) > 2 {
		t.Errorf("expected at most 2 violations due to limit, got %d", len(violations))
	}
}

func TestAttributeChecker_IgnorePartialClaims(t *testing.T) {
	config := &AttributeCheckerConfig{
		Enabled:             true,
		MaxClaimsToCheck:    50,
		IgnorePartialClaims: true,
		MinSymbolLength:     3,
	}
	checker := NewAttributeChecker(config)

	// Field name claim should be ignored
	input := &CheckInput{
		Response: "Config has fields: Name, Debug",
		EvidenceIndex: &EvidenceIndex{
			SymbolDetails: map[string][]SymbolInfo{
				"Config": {{Name: "Config", Kind: "struct", Fields: []string{"Name", "Value"}}},
			},
		},
	}

	violations := checker.Check(context.Background(), input)
	if len(violations) != 0 {
		t.Errorf("expected no violations when ignoring partial claims, got %d", len(violations))
	}
}

func TestAttributeChecker_ShortSymbolIgnored(t *testing.T) {
	config := &AttributeCheckerConfig{
		Enabled:          true,
		MaxClaimsToCheck: 50,
		MinSymbolLength:  5,
	}
	checker := NewAttributeChecker(config)

	// "Do" is shorter than MinSymbolLength
	input := &CheckInput{
		Response: "Do returns error",
		EvidenceIndex: &EvidenceIndex{
			SymbolDetails: map[string][]SymbolInfo{
				"Do": {{Name: "Do", Kind: "function", ReturnTypes: []string{"*Result"}}},
			},
		},
	}

	violations := checker.Check(context.Background(), input)
	if len(violations) != 0 {
		t.Errorf("expected no violations for short symbol name, got %d", len(violations))
	}
}

func TestAttributeChecker_CaseInsensitiveFieldMatch(t *testing.T) {
	checker := NewAttributeChecker(nil)

	// Field names claimed in different case
	input := &CheckInput{
		Response: "Config has fields: name, value",
		EvidenceIndex: &EvidenceIndex{
			SymbolDetails: map[string][]SymbolInfo{
				"Config": {{Name: "Config", Kind: "struct", Fields: []string{"Name", "Value"}}},
			},
		},
	}

	violations := checker.Check(context.Background(), input)
	if len(violations) != 0 {
		t.Errorf("expected no violations for case-insensitive field match, got %d", len(violations))
	}
}

func TestAttributeChecker_NoReturnTypeInfo(t *testing.T) {
	checker := NewAttributeChecker(nil)

	// Symbol exists but no return type info
	input := &CheckInput{
		Response: "Parse returns error",
		EvidenceIndex: &EvidenceIndex{
			SymbolDetails: map[string][]SymbolInfo{
				"Parse": {{Name: "Parse", Kind: "function"}}, // No ReturnTypes
			},
		},
	}

	violations := checker.Check(context.Background(), input)
	// Should skip validation when no attribute info available
	if len(violations) != 0 {
		t.Errorf("expected no violations when no return type info, got %d", len(violations))
	}
}

func TestAttributeChecker_FieldNamesWithAnd(t *testing.T) {
	checker := NewAttributeChecker(nil)

	// Field names with "and" connector
	input := &CheckInput{
		Response: "Config has fields Name and Value",
		EvidenceIndex: &EvidenceIndex{
			SymbolDetails: map[string][]SymbolInfo{
				"Config": {{Name: "Config", Kind: "struct", Fields: []string{"Name", "Value", "Timeout"}}},
			},
		},
	}

	violations := checker.Check(context.Background(), input)
	if len(violations) != 0 {
		t.Errorf("expected no violations for field names with 'and', got %d", len(violations))
	}
}

func TestAttributeChecker_GenericTypeNormalization(t *testing.T) {
	checker := NewAttributeChecker(nil)

	// Claim about generic type
	input := &CheckInput{
		Response: "Fetch returns Result",
		EvidenceIndex: &EvidenceIndex{
			SymbolDetails: map[string][]SymbolInfo{
				"Fetch": {{
					Name:        "Fetch",
					Kind:        "function",
					ReturnTypes: []string{"Result[T]"}, // Generic type
				}},
			},
		},
	}

	violations := checker.Check(context.Background(), input)
	// Should match despite generic parameter difference
	if len(violations) != 0 {
		t.Errorf("expected no violations for generic type normalization, got %d", len(violations))
	}
}

// Test helper functions

func TestParseReturnTypes(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{"error", []string{"error"}},
		{"(error)", []string{"error"}},
		{"(*Result, error)", []string{"*Result", "error"}},
		{"(int, string, error)", []string{"int", "string", "error"}},
		{"", nil},
	}

	for _, tt := range tests {
		result := parseReturnTypes(tt.input)
		if len(result) != len(tt.expected) {
			t.Errorf("parseReturnTypes(%q): expected %v, got %v", tt.input, tt.expected, result)
			continue
		}
		for i := range result {
			if result[i] != tt.expected[i] {
				t.Errorf("parseReturnTypes(%q)[%d]: expected %q, got %q", tt.input, i, tt.expected[i], result[i])
			}
		}
	}
}

func TestParseFieldNameList(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{"Name, Value, Timeout", []string{"Name", "Value", "Timeout"}},
		{"Name and Value", []string{"Name", "Value"}},
		{"Name, Value and Timeout", []string{"Name", "Value", "Timeout"}},
		{"`Name`, `Value`", []string{"Name", "Value"}},
		{"", nil},
	}

	for _, tt := range tests {
		result := parseFieldNameList(tt.input)
		if len(result) != len(tt.expected) {
			t.Errorf("parseFieldNameList(%q): expected %v, got %v", tt.input, tt.expected, result)
			continue
		}
		for i := range result {
			if result[i] != tt.expected[i] {
				t.Errorf("parseFieldNameList(%q)[%d]: expected %q, got %q", tt.input, i, tt.expected[i], result[i])
			}
		}
	}
}

func TestTypesMatch(t *testing.T) {
	tests := []struct {
		a, b     string
		expected bool
	}{
		{"error", "error", true},
		{"Error", "error", true},
		{"*Result", "*Result", true},
		{"*result", "*Result", true},
		{"Result[T]", "Result", true},
		{"error", "*Result", false},
		{"int", "string", false},
	}

	for _, tt := range tests {
		result := typesMatch(tt.a, tt.b)
		if result != tt.expected {
			t.Errorf("typesMatch(%q, %q): expected %v, got %v", tt.a, tt.b, tt.expected, result)
		}
	}
}

func TestIsValidFieldName(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"Name", true},
		{"Value", true},
		{"fieldName", true},
		{"field_name", true},
		{"Field123", true},
		{"", false},
		{"X", false}, // too short
		{"123field", false},
		{"*field", false},
	}

	for _, tt := range tests {
		result := isValidFieldName(tt.input)
		if result != tt.expected {
			t.Errorf("isValidFieldName(%q): expected %v, got %v", tt.input, tt.expected, result)
		}
	}
}
