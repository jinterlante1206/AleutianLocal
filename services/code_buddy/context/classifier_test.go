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
)

func TestPatternClassifier_Overview(t *testing.T) {
	c := NewPatternClassifier()

	tests := []struct {
		query string
		want  QueryType
	}{
		{"What does this codebase do?", QueryTypeOverview},
		{"what is this project about?", QueryTypeOverview},
		{"Give me an overview of the system", QueryTypeOverview},
		{"Explain this codebase", QueryTypeOverview},
		{"summarize the code", QueryTypeOverview},
		{"High-level overview please", QueryTypeOverview},
	}

	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			result := c.Classify(tt.query)
			if result.Type != tt.want {
				t.Errorf("Classify(%q) = %v, want %v (signals: %v)",
					tt.query, result.Type, tt.want, result.Signals)
			}
		})
	}
}

func TestPatternClassifier_Conceptual(t *testing.T) {
	c := NewPatternClassifier()

	tests := []struct {
		query string
		want  QueryType
	}{
		{"How does authentication work?", QueryTypeConceptual},
		{"How do these components interact?", QueryTypeConceptual},
		{"Why does the system use JWT?", QueryTypeConceptual},
		{"What is the architecture of this service?", QueryTypeConceptual},
		{"Explain the data flow", QueryTypeConceptual},
		{"How is the database connected?", QueryTypeConceptual},
	}

	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			result := c.Classify(tt.query)
			if result.Type != tt.want {
				t.Errorf("Classify(%q) = %v, want %v (signals: %v)",
					tt.query, result.Type, tt.want, result.Signals)
			}
		})
	}
}

func TestPatternClassifier_Specific(t *testing.T) {
	c := NewPatternClassifier()

	tests := []struct {
		query string
		want  QueryType
	}{
		{"Where is ValidateToken defined?", QueryTypeSpecific},
		{"Find the ParseRequest function", QueryTypeSpecific},
		{"Locate HandleAuth", QueryTypeSpecific},
		{"Definition of UserService", QueryTypeSpecific},
		{"Where is the AuthController?", QueryTypeSpecific},
		{"What is the signature of CreateUser?", QueryTypeSpecific},
	}

	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			result := c.Classify(tt.query)
			if result.Type != tt.want {
				t.Errorf("Classify(%q) = %v, want %v (signals: %v, terms: %v)",
					tt.query, result.Type, tt.want, result.Signals, result.ExtractedTerms)
			}
		})
	}
}

func TestPatternClassifier_CamelCase(t *testing.T) {
	c := NewPatternClassifier()

	tests := []struct {
		query        string
		expectedType QueryType
		shouldFind   []string
	}{
		{"What is ValidateToken?", QueryTypeSpecific, []string{"ValidateToken"}},
		{"Explain UserService and AuthHandler", QueryTypeSpecific, []string{"UserService", "AuthHandler"}},
		{"ParseJSONResponse implementation", QueryTypeSpecific, []string{"ParseJSONResponse"}},
	}

	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			result := c.Classify(tt.query)
			if result.Type != tt.expectedType {
				t.Errorf("Classify(%q) = %v, want %v", tt.query, result.Type, tt.expectedType)
			}

			for _, term := range tt.shouldFind {
				found := false
				for _, extracted := range result.ExtractedTerms {
					if extracted == term {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Expected to extract %q from %q, got %v", term, tt.query, result.ExtractedTerms)
				}
			}
		})
	}
}

func TestPatternClassifier_Locational(t *testing.T) {
	c := NewPatternClassifier()

	tests := []struct {
		query string
		want  QueryType
	}{
		{"Show me the auth package", QueryTypeLocational},
		{"Open the handlers file", QueryTypeLocational},
		{"Look at pkg/auth/validator.go", QueryTypeLocational},
		{"Go to the database module", QueryTypeLocational},
		{"Show me the code in internal/service", QueryTypeLocational},
		{"List the files in pkg/handlers", QueryTypeLocational},
	}

	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			result := c.Classify(tt.query)
			if result.Type != tt.want {
				t.Errorf("Classify(%q) = %v, want %v (signals: %v)",
					tt.query, result.Type, tt.want, result.Signals)
			}
		})
	}
}

func TestPatternClassifier_PathExtraction(t *testing.T) {
	c := NewPatternClassifier()

	tests := []struct {
		query      string
		shouldFind string
	}{
		{"Show me the code in pkg/auth", "pkg/auth"},
		{"Look at internal/service/handler", "internal/service/handler"},
		{"in the src/utils directory", "src/utils"},
	}

	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			result := c.Classify(tt.query)

			found := false
			for _, term := range result.ExtractedTerms {
				if term == tt.shouldFind {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("Expected to extract path %q from %q, got %v",
					tt.shouldFind, tt.query, result.ExtractedTerms)
			}
		})
	}
}

func TestPatternClassifier_EmptyQuery(t *testing.T) {
	c := NewPatternClassifier()

	result := c.Classify("")
	if result.Confidence != 0.0 {
		t.Errorf("Expected 0 confidence for empty query, got %f", result.Confidence)
	}
}

func TestPatternClassifier_AmbiguousQuery(t *testing.T) {
	c := NewPatternClassifier()

	// This query has signals for multiple types
	result := c.Classify("show me how the ValidateToken function works")

	// Should have confidence between 0 and 1
	if result.Confidence <= 0 || result.Confidence > 1 {
		t.Errorf("Expected confidence between 0 and 1, got %f", result.Confidence)
	}

	// Should have multiple signals
	if len(result.Signals) == 0 {
		t.Error("Expected signals for ambiguous query")
	}
}

func TestPatternClassifier_ConfidenceRanges(t *testing.T) {
	c := NewPatternClassifier()

	tests := []struct {
		query   string
		minConf float64
	}{
		{"What does this codebase do?", 0.5}, // Clear overview
		{"ValidateToken", 0.5},               // Clear specific (CamelCase)
		{"the system", 0.0},                  // Unclear
	}

	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			result := c.Classify(tt.query)
			if result.Confidence < tt.minConf {
				t.Errorf("Classify(%q).Confidence = %f, want >= %f",
					tt.query, result.Confidence, tt.minConf)
			}
		})
	}
}

func TestQueryType_String(t *testing.T) {
	tests := []struct {
		qt       QueryType
		expected string
	}{
		{QueryTypeOverview, "overview"},
		{QueryTypeConceptual, "conceptual"},
		{QueryTypeSpecific, "specific"},
		{QueryTypeLocational, "locational"},
		{QueryType(99), "unknown"},
	}

	for _, tt := range tests {
		if got := tt.qt.String(); got != tt.expected {
			t.Errorf("QueryType(%d).String() = %q, want %q", tt.qt, got, tt.expected)
		}
	}
}

func TestContainsCamelCase(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"ValidateToken", true},
		{"validateToken", true},
		{"validate_token", false},
		{"VALIDATE", false},
		{"validate", false},
		{"", false},
		{"aB", true},
	}

	for _, tt := range tests {
		if got := ContainsCamelCase(tt.input); got != tt.expected {
			t.Errorf("ContainsCamelCase(%q) = %v, want %v", tt.input, got, tt.expected)
		}
	}
}

func TestClassifyQuery_Convenience(t *testing.T) {
	// Test the convenience function uses the default classifier
	result := ClassifyQuery("What does this do?")
	if result == nil {
		t.Error("ClassifyQuery returned nil")
	}
	if result.Type != QueryTypeOverview {
		t.Errorf("Expected overview, got %v", result.Type)
	}
}
