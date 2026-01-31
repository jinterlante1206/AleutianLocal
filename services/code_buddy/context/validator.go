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
	"errors"
	"strings"
)

// MinSummaryLength is the minimum acceptable summary content length.
const MinSummaryLength = 20

// SummaryValidator validates LLM-generated summaries before caching.
//
// Thread Safety: Safe for concurrent use (stateless).
type SummaryValidator struct {
	hierarchy LanguageHierarchy
}

// NewSummaryValidator creates a new validator.
//
// Inputs:
//   - hierarchy: The language hierarchy for validation.
//
// Outputs:
//   - *SummaryValidator: A new validator instance.
func NewSummaryValidator(hierarchy LanguageHierarchy) *SummaryValidator {
	return &SummaryValidator{
		hierarchy: hierarchy,
	}
}

// SourceInfo provides information about the source code being summarized.
type SourceInfo struct {
	// Symbols is a list of symbol names in the source.
	Symbols []string

	// EntityIDs is a list of valid entity IDs that can be children.
	EntityIDs []string

	// ParentID is the expected parent for this entity.
	ParentID string

	// Content is the raw source content (for hash verification).
	Content string
}

// ContainsSymbol checks if a symbol exists in the source.
func (s *SourceInfo) ContainsSymbol(name string) bool {
	for _, sym := range s.Symbols {
		if sym == name {
			return true
		}
	}
	return false
}

// EntityExists checks if an entity ID is valid.
func (s *SourceInfo) EntityExists(id string) bool {
	for _, eid := range s.EntityIDs {
		if eid == id {
			return true
		}
	}
	return false
}

// ValidationError contains details about a validation failure.
type ValidationError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
	Value   any    `json:"value,omitempty"`
}

// Error implements the error interface.
func (e *ValidationError) Error() string {
	return e.Field + ": " + e.Message
}

// ValidationResult contains the results of summary validation.
type ValidationResult struct {
	Valid  bool              `json:"valid"`
	Errors []ValidationError `json:"errors,omitempty"`
}

// Validate validates a summary against source information.
//
// Inputs:
//   - summary: The summary to validate.
//   - source: Information about the source being summarized.
//
// Outputs:
//   - *ValidationResult: The validation result with any errors.
func (v *SummaryValidator) Validate(summary *Summary, source *SourceInfo) *ValidationResult {
	result := &ValidationResult{Valid: true}

	// 1. Check summary content is non-empty
	if len(summary.Content) < MinSummaryLength {
		result.addError("content", "summary too short", len(summary.Content))
	}

	// 2. Check content doesn't look like an error or refusal
	if v.looksLikeError(summary.Content) {
		result.addError("content", "summary appears to be an error message", nil)
	}

	// 3. Check keywords exist in source
	if source != nil {
		for _, kw := range summary.Keywords {
			if !v.isValidKeyword(kw, source) {
				result.addError("keywords", "keyword not found in source", kw)
			}
		}
	}

	// 4. Check level is correct
	if v.hierarchy != nil {
		expectedLevel := v.hierarchy.EntityLevel(summary.ID)
		if summary.Level != expectedLevel {
			result.addError("level", "level mismatch", map[string]int{
				"expected": expectedLevel,
				"actual":   summary.Level,
			})
		}
	}

	// 5. Check children are valid
	if source != nil && len(summary.Children) > 0 {
		for _, childID := range summary.Children {
			if !source.EntityExists(childID) {
				result.addError("children", "invalid child reference", childID)
			}
		}
	}

	// 6. Check parent is correct (for non-root summaries)
	if source != nil && summary.Level > 0 {
		if summary.ParentID == "" {
			result.addError("parent_id", "non-root summary missing parent", nil)
		} else if source.ParentID != "" && summary.ParentID != source.ParentID {
			result.addError("parent_id", "parent mismatch", map[string]string{
				"expected": source.ParentID,
				"actual":   summary.ParentID,
			})
		}
	}

	return result
}

// ValidateMinimal performs minimal validation (for partial summaries).
//
// Inputs:
//   - summary: The summary to validate.
//
// Outputs:
//   - error: Non-nil if summary is completely invalid.
//
// This is more lenient than full validation, used for degraded mode.
func (v *SummaryValidator) ValidateMinimal(summary *Summary) error {
	var errs []error

	if summary.ID == "" {
		errs = append(errs, errors.New("summary ID is empty"))
	}

	if summary.Content == "" {
		errs = append(errs, errors.New("summary content is empty"))
	}

	if summary.Level < 0 || summary.Level > 3 {
		errs = append(errs, errors.New("summary level out of range"))
	}

	return errors.Join(errs...)
}

// looksLikeError checks if content appears to be an error message.
func (v *SummaryValidator) looksLikeError(content string) bool {
	lower := strings.ToLower(content)

	errorPatterns := []string{
		"i cannot",
		"i'm unable",
		"i am unable",
		"error:",
		"exception:",
		"failed to",
		"sorry,",
		"apologize",
		"as an ai",
		"as a language model",
	}

	for _, pattern := range errorPatterns {
		if strings.Contains(lower, pattern) {
			return true
		}
	}

	return false
}

// isValidKeyword checks if a keyword is valid.
func (v *SummaryValidator) isValidKeyword(keyword string, source *SourceInfo) bool {
	// Allow common terms without checking source
	if isCommonTerm(keyword) {
		return true
	}

	// Check if it's a symbol in the source
	if source.ContainsSymbol(keyword) {
		return true
	}

	// Check if it's a partial match (case-insensitive)
	lower := strings.ToLower(keyword)
	for _, sym := range source.Symbols {
		if strings.Contains(strings.ToLower(sym), lower) {
			return true
		}
	}

	return false
}

// isCommonTerm returns true for common programming terms that don't need source verification.
func isCommonTerm(term string) bool {
	commonTerms := map[string]bool{
		// Common programming terms
		"function": true, "method": true, "class": true, "struct": true,
		"interface": true, "type": true, "package": true, "module": true,
		"import": true, "export": true, "variable": true, "constant": true,

		// Common action terms
		"handle": true, "process": true, "parse": true, "validate": true,
		"create": true, "read": true, "update": true, "delete": true,
		"get": true, "set": true, "init": true, "close": true,

		// Common concept terms
		"authentication": true, "authorization": true, "database": true,
		"api": true, "http": true, "request": true, "response": true,
		"error": true, "config": true, "configuration": true, "setting": true,
		"service": true, "client": true, "server": true, "handler": true,
		"middleware": true, "router": true, "controller": true, "model": true,
		"repository": true, "storage": true, "cache": true, "queue": true,

		// Common data terms
		"string": true, "int": true, "bool": true, "map": true, "slice": true,
		"array": true, "list": true, "json": true, "xml": true, "yaml": true,
	}

	return commonTerms[strings.ToLower(term)]
}

// addError adds an error to the validation result.
func (r *ValidationResult) addError(field, message string, value any) {
	r.Valid = false
	r.Errors = append(r.Errors, ValidationError{
		Field:   field,
		Message: message,
		Value:   value,
	})
}

// Error returns the first error message, or empty string if valid.
func (r *ValidationResult) Error() string {
	if r.Valid || len(r.Errors) == 0 {
		return ""
	}
	return r.Errors[0].Error()
}

// AllErrors returns all error messages joined.
func (r *ValidationResult) AllErrors() string {
	if r.Valid {
		return ""
	}

	msgs := make([]string, len(r.Errors))
	for i, e := range r.Errors {
		msgs[i] = e.Error()
	}
	return strings.Join(msgs, "; ")
}
