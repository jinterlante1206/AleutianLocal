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
	"testing"
	"time"
)

func TestSummaryValidator_Validate_ValidSummary(t *testing.T) {
	h := &GoHierarchy{}
	v := NewSummaryValidator(h)

	summary := &Summary{
		ID:        "pkg/auth",
		Level:     1,
		Content:   "This package handles authentication and authorization for the application.",
		Keywords:  []string{"authentication", "jwt", "handler"},
		ParentID:  "pkg",
		UpdatedAt: time.Now(),
	}

	source := &SourceInfo{
		Symbols:   []string{"Authenticate", "ValidateToken", "jwt", "handler"},
		EntityIDs: []string{"pkg/auth/handler.go", "pkg/auth/token.go"},
		ParentID:  "pkg",
	}

	result := v.Validate(summary, source)

	if !result.Valid {
		t.Errorf("expected valid, got errors: %v", result.AllErrors())
	}
}

func TestSummaryValidator_Validate_ContentTooShort(t *testing.T) {
	h := &GoHierarchy{}
	v := NewSummaryValidator(h)

	summary := &Summary{
		ID:        "pkg/auth",
		Level:     1,
		Content:   "Short", // Too short
		UpdatedAt: time.Now(),
	}

	result := v.Validate(summary, nil)

	if result.Valid {
		t.Error("expected invalid for short content")
	}

	found := false
	for _, err := range result.Errors {
		if err.Field == "content" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected content error")
	}
}

func TestSummaryValidator_Validate_ErrorMessage(t *testing.T) {
	h := &GoHierarchy{}
	v := NewSummaryValidator(h)

	errorMessages := []string{
		"I cannot process this request",
		"I'm unable to generate a summary",
		"Error: Something went wrong",
		"Sorry, I can't help with that",
		"As an AI, I cannot do this",
	}

	for _, msg := range errorMessages {
		summary := &Summary{
			ID:        "pkg/auth",
			Level:     1,
			Content:   msg + " - some additional text to make it long enough",
			UpdatedAt: time.Now(),
		}

		result := v.Validate(summary, nil)

		if result.Valid {
			t.Errorf("expected invalid for error message: %q", msg)
		}
	}
}

func TestSummaryValidator_Validate_InvalidKeyword(t *testing.T) {
	h := &GoHierarchy{}
	v := NewSummaryValidator(h)

	summary := &Summary{
		ID:        "pkg/auth",
		Level:     1,
		Content:   "This package handles authentication for the application.",
		Keywords:  []string{"NonexistentSymbol123"},
		ParentID:  "pkg",
		UpdatedAt: time.Now(),
	}

	source := &SourceInfo{
		Symbols:   []string{"Authenticate", "ValidateToken"},
		EntityIDs: []string{},
		ParentID:  "pkg",
	}

	result := v.Validate(summary, source)

	if result.Valid {
		t.Error("expected invalid for nonexistent keyword")
	}

	found := false
	for _, err := range result.Errors {
		if err.Field == "keywords" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected keywords error")
	}
}

func TestSummaryValidator_Validate_CommonKeywordsAllowed(t *testing.T) {
	h := &GoHierarchy{}
	v := NewSummaryValidator(h)

	summary := &Summary{
		ID:        "pkg/auth",
		Level:     1,
		Content:   "This package handles authentication for the application.",
		Keywords:  []string{"function", "interface", "authentication", "handler"},
		ParentID:  "pkg",
		UpdatedAt: time.Now(),
	}

	source := &SourceInfo{
		Symbols:   []string{}, // No symbols, but common terms should pass
		EntityIDs: []string{},
		ParentID:  "pkg",
	}

	result := v.Validate(summary, source)

	// All keywords are common terms, should be valid
	keywordErrors := 0
	for _, err := range result.Errors {
		if err.Field == "keywords" {
			keywordErrors++
		}
	}
	if keywordErrors > 0 {
		t.Errorf("got %d keyword errors, expected 0 for common terms", keywordErrors)
	}
}

func TestSummaryValidator_Validate_LevelMismatch(t *testing.T) {
	h := &GoHierarchy{}
	v := NewSummaryValidator(h)

	summary := &Summary{
		ID:        "pkg/auth/handler.go",
		Level:     1, // Should be 2 for file
		Content:   "This file contains the authentication handler.",
		UpdatedAt: time.Now(),
	}

	result := v.Validate(summary, nil)

	if result.Valid {
		t.Error("expected invalid for level mismatch")
	}

	found := false
	for _, err := range result.Errors {
		if err.Field == "level" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected level error")
	}
}

func TestSummaryValidator_Validate_InvalidChild(t *testing.T) {
	h := &GoHierarchy{}
	v := NewSummaryValidator(h)

	summary := &Summary{
		ID:        "pkg/auth",
		Level:     1,
		Content:   "This package handles authentication for the application.",
		Children:  []string{"pkg/auth/nonexistent.go"},
		ParentID:  "pkg",
		UpdatedAt: time.Now(),
	}

	source := &SourceInfo{
		Symbols:   []string{},
		EntityIDs: []string{"pkg/auth/handler.go"}, // nonexistent.go not in list
		ParentID:  "pkg",
	}

	result := v.Validate(summary, source)

	if result.Valid {
		t.Error("expected invalid for invalid child reference")
	}

	found := false
	for _, err := range result.Errors {
		if err.Field == "children" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected children error")
	}
}

func TestSummaryValidator_Validate_MissingParent(t *testing.T) {
	h := &GoHierarchy{}
	v := NewSummaryValidator(h)

	summary := &Summary{
		ID:        "pkg/auth",
		Level:     1,
		Content:   "This package handles authentication for the application.",
		ParentID:  "", // Missing parent for non-root
		UpdatedAt: time.Now(),
	}

	source := &SourceInfo{
		ParentID: "pkg",
	}

	result := v.Validate(summary, source)

	if result.Valid {
		t.Error("expected invalid for missing parent")
	}

	found := false
	for _, err := range result.Errors {
		if err.Field == "parent_id" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected parent_id error")
	}
}

func TestSummaryValidator_Validate_ParentMismatch(t *testing.T) {
	h := &GoHierarchy{}
	v := NewSummaryValidator(h)

	summary := &Summary{
		ID:        "pkg/auth",
		Level:     1,
		Content:   "This package handles authentication for the application.",
		ParentID:  "wrong_parent",
		UpdatedAt: time.Now(),
	}

	source := &SourceInfo{
		ParentID: "pkg",
	}

	result := v.Validate(summary, source)

	if result.Valid {
		t.Error("expected invalid for parent mismatch")
	}
}

func TestSummaryValidator_ValidateMinimal_Valid(t *testing.T) {
	v := NewSummaryValidator(nil)

	summary := &Summary{
		ID:      "pkg/auth",
		Level:   1,
		Content: "Some content",
	}

	err := v.ValidateMinimal(summary)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestSummaryValidator_ValidateMinimal_EmptyID(t *testing.T) {
	v := NewSummaryValidator(nil)

	summary := &Summary{
		ID:      "",
		Level:   1,
		Content: "Some content",
	}

	err := v.ValidateMinimal(summary)
	if err == nil {
		t.Error("expected error for empty ID")
	}
}

func TestSummaryValidator_ValidateMinimal_EmptyContent(t *testing.T) {
	v := NewSummaryValidator(nil)

	summary := &Summary{
		ID:      "pkg/auth",
		Level:   1,
		Content: "",
	}

	err := v.ValidateMinimal(summary)
	if err == nil {
		t.Error("expected error for empty content")
	}
}

func TestSummaryValidator_ValidateMinimal_InvalidLevel(t *testing.T) {
	v := NewSummaryValidator(nil)

	summary := &Summary{
		ID:      "pkg/auth",
		Level:   5, // Out of range
		Content: "Some content",
	}

	err := v.ValidateMinimal(summary)
	if err == nil {
		t.Error("expected error for invalid level")
	}
}

func TestSourceInfo_ContainsSymbol(t *testing.T) {
	source := &SourceInfo{
		Symbols: []string{"Authenticate", "ValidateToken", "handler"},
	}

	if !source.ContainsSymbol("Authenticate") {
		t.Error("expected to find Authenticate")
	}
	if !source.ContainsSymbol("ValidateToken") {
		t.Error("expected to find ValidateToken")
	}
	if source.ContainsSymbol("NonExistent") {
		t.Error("should not find NonExistent")
	}
}

func TestSourceInfo_EntityExists(t *testing.T) {
	source := &SourceInfo{
		EntityIDs: []string{"pkg/auth/handler.go", "pkg/auth/token.go"},
	}

	if !source.EntityExists("pkg/auth/handler.go") {
		t.Error("expected to find handler.go")
	}
	if source.EntityExists("pkg/auth/nonexistent.go") {
		t.Error("should not find nonexistent.go")
	}
}

func TestValidationError_Error(t *testing.T) {
	err := &ValidationError{
		Field:   "content",
		Message: "too short",
	}

	expected := "content: too short"
	if err.Error() != expected {
		t.Errorf("Error() = %q, want %q", err.Error(), expected)
	}
}

func TestValidationResult_Error(t *testing.T) {
	result := &ValidationResult{
		Valid: false,
		Errors: []ValidationError{
			{Field: "content", Message: "too short"},
			{Field: "level", Message: "mismatch"},
		},
	}

	errStr := result.Error()
	if errStr != "content: too short" {
		t.Errorf("Error() = %q, want first error", errStr)
	}

	result.Valid = true
	if result.Error() != "" {
		t.Error("Error() should return empty string when valid")
	}
}

func TestValidationResult_AllErrors(t *testing.T) {
	result := &ValidationResult{
		Valid: false,
		Errors: []ValidationError{
			{Field: "content", Message: "too short"},
			{Field: "level", Message: "mismatch"},
		},
	}

	allErrs := result.AllErrors()
	if allErrs != "content: too short; level: mismatch" {
		t.Errorf("AllErrors() = %q, unexpected format", allErrs)
	}

	result.Valid = true
	if result.AllErrors() != "" {
		t.Error("AllErrors() should return empty string when valid")
	}
}

func TestIsCommonTerm(t *testing.T) {
	commonTerms := []string{
		"function", "method", "class", "struct", "interface",
		"authentication", "database", "api", "handler",
	}

	for _, term := range commonTerms {
		if !isCommonTerm(term) {
			t.Errorf("expected %q to be a common term", term)
		}
	}

	// Case insensitivity
	if !isCommonTerm("FUNCTION") {
		t.Error("expected common term check to be case-insensitive")
	}

	// Non-common term
	if isCommonTerm("xyz123") {
		t.Error("unexpected common term xyz123")
	}
}
