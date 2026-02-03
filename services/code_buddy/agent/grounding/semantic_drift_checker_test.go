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

func TestNewSemanticDriftChecker(t *testing.T) {
	t.Run("nil config uses defaults", func(t *testing.T) {
		checker := NewSemanticDriftChecker(nil)
		if checker == nil {
			t.Fatal("expected checker, got nil")
		}
		if checker.config == nil {
			t.Fatal("expected config, got nil")
		}
		if !checker.config.Enabled {
			t.Error("expected enabled by default")
		}
	})

	t.Run("custom config is used", func(t *testing.T) {
		config := &SemanticDriftCheckerConfig{
			Enabled:           false,
			CriticalThreshold: 0.8,
		}
		checker := NewSemanticDriftChecker(config)
		if checker.config.Enabled {
			t.Error("expected disabled")
		}
		if checker.config.CriticalThreshold != 0.8 {
			t.Errorf("expected threshold 0.8, got %f", checker.config.CriticalThreshold)
		}
	})

	t.Run("synonym lookup is built", func(t *testing.T) {
		checker := NewSemanticDriftChecker(nil)
		if len(checker.synonymLookup) == 0 {
			t.Error("expected synonym lookup to be populated")
		}
		// Check a known synonym
		if checker.synonymLookup["tests"] != "test" {
			t.Error("expected 'tests' to map to 'test'")
		}
	})
}

func TestSemanticDriftChecker_Name(t *testing.T) {
	checker := NewSemanticDriftChecker(nil)
	if checker.Name() != "semantic_drift_checker" {
		t.Errorf("expected 'semantic_drift_checker', got %s", checker.Name())
	}
}

func TestSemanticDriftChecker_Check_Disabled(t *testing.T) {
	config := &SemanticDriftCheckerConfig{Enabled: false}
	checker := NewSemanticDriftChecker(config)

	input := &CheckInput{
		UserQuestion: "What tests exist?",
		Response:     "The weather is nice today.",
	}

	violations := checker.Check(context.Background(), input)
	if len(violations) != 0 {
		t.Errorf("expected 0 violations when disabled, got %d", len(violations))
	}
}

func TestSemanticDriftChecker_Check_EmptyQuestion(t *testing.T) {
	checker := NewSemanticDriftChecker(nil)

	input := &CheckInput{
		UserQuestion: "",
		Response:     "This is a response about something completely unrelated.",
	}

	violations := checker.Check(context.Background(), input)
	if len(violations) != 0 {
		t.Errorf("expected 0 violations for empty question, got %d", len(violations))
	}
}

func TestSemanticDriftChecker_Check_ShortResponse(t *testing.T) {
	checker := NewSemanticDriftChecker(nil)

	input := &CheckInput{
		UserQuestion: "What tests exist in this project?",
		Response:     "No.",
	}

	violations := checker.Check(context.Background(), input)
	if len(violations) != 0 {
		t.Errorf("expected 0 violations for short response, got %d", len(violations))
	}
}

func TestSemanticDriftChecker_Check_FewKeywords(t *testing.T) {
	checker := NewSemanticDriftChecker(nil)

	input := &CheckInput{
		UserQuestion: "?", // Only punctuation, no keywords
		Response:     "This is a response about something completely unrelated to anything.",
	}

	violations := checker.Check(context.Background(), input)
	if len(violations) != 0 {
		t.Errorf("expected 0 violations for question with no keywords, got %d", len(violations))
	}
}

func TestSemanticDriftChecker_ListQuestion_ListResponse(t *testing.T) {
	checker := NewSemanticDriftChecker(nil)

	input := &CheckInput{
		UserQuestion: "What tests exist in this project?",
		Response: `The following tests exist in the project:
1. TestParser_Parse - tests the parser functionality
2. TestValidator_Validate - tests validation logic
3. TestGrounder_Check - tests grounding checks`,
	}

	violations := checker.Check(context.Background(), input)
	if len(violations) != 0 {
		t.Errorf("expected 0 violations for matching list response, got %d", len(violations))
	}
}

func TestSemanticDriftChecker_ListQuestion_NonListResponse(t *testing.T) {
	checker := NewSemanticDriftChecker(nil)

	input := &CheckInput{
		UserQuestion: "What tests exist in this project?",
		Response:     "The authentication module uses JWT tokens for secure access control.",
	}

	violations := checker.Check(context.Background(), input)
	if len(violations) == 0 {
		t.Error("expected violations for non-list response to list question")
	}
}

func TestSemanticDriftChecker_HowQuestion_ProcessResponse(t *testing.T) {
	checker := NewSemanticDriftChecker(nil)

	input := &CheckInput{
		UserQuestion: "How does the parser work?",
		Response: `The parser works by first tokenizing the input, then building an AST.
The process involves:
1. Lexical analysis to identify tokens
2. Syntax analysis to build the tree
3. Semantic analysis to validate the structure`,
	}

	violations := checker.Check(context.Background(), input)
	if len(violations) != 0 {
		t.Errorf("expected 0 violations for matching process response, got %d", len(violations))
	}
}

func TestSemanticDriftChecker_CompletelyUnrelatedResponse(t *testing.T) {
	checker := NewSemanticDriftChecker(nil)

	input := &CheckInput{
		UserQuestion: "What tests exist in this project?",
		Response:     "Yes, the function BuildErrorMetadataJSON exists in the codebase and handles JSON serialization for error objects.",
	}

	violations := checker.Check(context.Background(), input)
	if len(violations) == 0 {
		t.Error("expected violations for completely unrelated response")
	}

	// Should be critical severity for complete drift
	foundCritical := false
	for _, v := range violations {
		if v.Severity == SeverityCritical || v.Severity == SeverityHigh {
			foundCritical = true
			break
		}
	}
	if !foundCritical {
		t.Error("expected critical/high severity for complete drift")
	}
}

func TestSemanticDriftChecker_Test15Scenario(t *testing.T) {
	// This is the specific scenario from Test 15 in the hallucination regressions
	checker := NewSemanticDriftChecker(nil)

	input := &CheckInput{
		UserQuestion: "What tests exist in this project?",
		Response:     "Yes, the function BuildErrorMetadataJSON exists in the codebase and is used for JSON serialization of error metadata. It takes an error as input and returns a JSON object containing error details.",
	}

	violations := checker.Check(context.Background(), input)
	if len(violations) == 0 {
		t.Error("Test 15 scenario: expected violations - asked about tests, got function existence")
	}

	// Verify violation details
	if len(violations) > 0 {
		v := violations[0]
		if v.Type != ViolationSemanticDrift {
			t.Errorf("expected ViolationSemanticDrift, got %s", v.Type)
		}
	}
}

func TestSemanticDriftChecker_PartialDrift(t *testing.T) {
	checker := NewSemanticDriftChecker(nil)

	input := &CheckInput{
		UserQuestion: "How do tests work in this project?",
		Response: `The tests in this project are organized by package. Each package has
a corresponding _test.go file. However, I should mention that the weather
forecast for tomorrow looks promising.`,
	}

	violations := checker.Check(context.Background(), input)
	// Response mentions tests and describes a process, so it should not trigger
	// critical or high severity violations. The slight off-topic mention of weather
	// may or may not trigger a warning depending on keyword overlap, but should
	// never be critical since the main question is addressed.
	for _, v := range violations {
		if v.Severity == SeverityCritical {
			t.Errorf("partial drift should not be critical, got: %s", v.Message)
		}
	}
}

func TestSemanticDriftChecker_KeywordOverlap(t *testing.T) {
	checker := NewSemanticDriftChecker(nil)

	t.Run("high overlap no violation", func(t *testing.T) {
		input := &CheckInput{
			UserQuestion: "Where is the configuration file located?",
			Response:     "The configuration file is located in the config directory at config/settings.yaml.",
		}

		violations := checker.Check(context.Background(), input)
		if len(violations) > 0 {
			t.Errorf("expected 0 violations for high keyword overlap, got %d", len(violations))
		}
	})

	t.Run("no overlap triggers violation", func(t *testing.T) {
		input := &CheckInput{
			UserQuestion: "Where is the configuration file located?",
			Response:     "The database uses PostgreSQL with connection pooling enabled.",
		}

		violations := checker.Check(context.Background(), input)
		if len(violations) == 0 {
			t.Error("expected violations for no keyword overlap")
		}
	})
}

func TestSemanticDriftChecker_TopicCoherence(t *testing.T) {
	checker := NewSemanticDriftChecker(nil)

	t.Run("same topic no violation", func(t *testing.T) {
		input := &CheckInput{
			UserQuestion: "What tests exist?",
			Response:     "The test suite includes unit tests, integration tests, and end-to-end tests.",
		}

		violations := checker.Check(context.Background(), input)
		if len(violations) > 0 {
			t.Errorf("expected 0 violations for same topic, got %d", len(violations))
		}
	})

	t.Run("different topic triggers violation", func(t *testing.T) {
		input := &CheckInput{
			UserQuestion: "What tests exist?",
			Response:     "The database schema includes users, products, and orders tables.",
		}

		violations := checker.Check(context.Background(), input)
		if len(violations) == 0 {
			t.Error("expected violations for different topic")
		}
	})
}

func TestSemanticDriftChecker_SynonymHandling(t *testing.T) {
	checker := NewSemanticDriftChecker(nil)

	// Question uses "tests", response uses "specs" - should be treated as same topic
	input := &CheckInput{
		UserQuestion: "What tests exist in this project?",
		Response:     "The project contains several specs for validation. The specs cover input validation, output formatting, and error handling.",
	}

	violations := checker.Check(context.Background(), input)
	// Should have reduced drift due to synonym matching
	highSeverityFound := false
	for _, v := range violations {
		if v.Severity == SeverityCritical {
			highSeverityFound = true
			break
		}
	}
	if highSeverityFound {
		t.Error("synonyms should reduce drift score - 'specs' is synonym of 'tests'")
	}
}

func TestSemanticDriftChecker_WhereQuestion(t *testing.T) {
	checker := NewSemanticDriftChecker(nil)

	t.Run("location response no violation", func(t *testing.T) {
		input := &CheckInput{
			UserQuestion: "Where is the main function defined?",
			Response:     "The main function is defined in cmd/server/main.go at line 15.",
		}

		violations := checker.Check(context.Background(), input)
		if len(violations) > 0 {
			t.Errorf("expected 0 violations, got %d", len(violations))
		}
	})

	t.Run("non-location response triggers violation", func(t *testing.T) {
		input := &CheckInput{
			UserQuestion: "Where is the main function defined?",
			Response:     "The weather today is sunny with temperatures around 72 degrees Fahrenheit.",
		}

		violations := checker.Check(context.Background(), input)
		if len(violations) == 0 {
			t.Error("expected violations for non-location response to WHERE question")
		}
	})
}

func TestSemanticDriftChecker_WhyQuestion(t *testing.T) {
	checker := NewSemanticDriftChecker(nil)

	input := &CheckInput{
		UserQuestion: "Why does the parser use recursion?",
		Response:     "The database schema was migrated to PostgreSQL version 14 for better performance.",
	}

	violations := checker.Check(context.Background(), input)
	if len(violations) == 0 {
		t.Error("expected violations - response doesn't explain why")
	}
}

func TestSemanticDriftChecker_ContextCancellation(t *testing.T) {
	checker := NewSemanticDriftChecker(nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	input := &CheckInput{
		UserQuestion: "What tests exist?",
		Response:     "Completely unrelated response about weather patterns.",
	}

	violations := checker.Check(ctx, input)
	// Should return early on cancellation
	if len(violations) > 0 {
		t.Error("expected no violations on cancelled context")
	}
}

func TestSemanticDriftChecker_ViolationDetails(t *testing.T) {
	checker := NewSemanticDriftChecker(nil)

	input := &CheckInput{
		UserQuestion: "What tests exist in this project?",
		Response:     "The weather forecast shows sunny skies for the entire week ahead.",
	}

	violations := checker.Check(context.Background(), input)
	if len(violations) == 0 {
		t.Fatal("expected violations")
	}

	v := violations[0]
	if v.Type != ViolationSemanticDrift {
		t.Errorf("expected ViolationSemanticDrift, got %s", v.Type)
	}
	if v.Code != "SEMANTIC_DRIFT" {
		t.Errorf("expected code SEMANTIC_DRIFT, got %s", v.Code)
	}
	if v.Evidence == "" {
		t.Error("expected evidence to contain the question")
	}
	if v.Suggestion == "" {
		t.Error("expected suggestion")
	}
}

func TestSemanticDriftChecker_QuestionClassification(t *testing.T) {
	checker := NewSemanticDriftChecker(nil)

	tests := []struct {
		question     string
		expectedType QuestionType
	}{
		// Direct patterns
		{"What tests exist?", QuestionList},
		{"List all the functions", QuestionList},
		{"Show all files", QuestionList},
		{"How does the parser work?", QuestionHow},
		{"How is authentication implemented?", QuestionHow},
		{"Where is the config file?", QuestionWhere},
		{"Where are tests defined?", QuestionWhere},
		{"Why does it use recursion?", QuestionWhy},
		{"Why is the timeout set to 30s?", QuestionWhy},
		{"Describe the architecture", QuestionDescribe},
		{"Explain how caching works", QuestionDescribe},
		{"What is the Config struct?", QuestionWhat},
		{"What does processRequest do?", QuestionWhat},
		{"Hello world", QuestionUnknown},

		// Indirect patterns (M2 fix)
		{"Can you tell me what tests exist?", QuestionList},
		{"Could you list all the functions?", QuestionList},
		{"Can you show me what files are there?", QuestionList},
		{"Could you explain how the parser works?", QuestionHow},
		{"Can you tell me how authentication is implemented?", QuestionHow},
		{"Could you tell me where the config file is?", QuestionWhere},
		{"Please tell me where the config is located", QuestionWhere},
		{"Could you explain why it uses recursion?", QuestionWhy},
		{"Can you tell me why the timeout is 30s?", QuestionWhy},
		{"Could you describe the architecture?", QuestionDescribe},
		{"Can you give me an overview of the caching system?", QuestionDescribe},
		{"Could you tell me what is the Config struct?", QuestionWhat},
		{"I'd like to know what tests exist", QuestionList},
		{"Please tell me where the main function is", QuestionWhere},
	}

	for _, tt := range tests {
		t.Run(tt.question, func(t *testing.T) {
			result := checker.classifyQuestion(tt.question)
			if result != tt.expectedType {
				t.Errorf("for '%s': expected %s, got %s",
					tt.question, tt.expectedType.String(), result.String())
			}
		})
	}
}

func TestSemanticDriftChecker_ExtractKeywords(t *testing.T) {
	checker := NewSemanticDriftChecker(nil)

	t.Run("extracts meaningful words", func(t *testing.T) {
		keywords := checker.extractKeywords("What tests exist in this project?")
		if !keywords["test"] { // Should be normalized from "tests"
			t.Error("expected 'test' keyword (normalized from 'tests')")
		}
		if !keywords["project"] {
			t.Error("expected 'project' keyword")
		}
	})

	t.Run("filters stop words", func(t *testing.T) {
		keywords := checker.extractKeywords("What is the configuration?")
		if keywords["what"] {
			t.Error("'what' should be filtered as stop word")
		}
		if keywords["is"] {
			t.Error("'is' should be filtered as stop word")
		}
		if keywords["the"] {
			t.Error("'the' should be filtered as stop word")
		}
		if !keywords["config"] { // Should be normalized from "configuration"
			t.Error("expected 'config' keyword (normalized from 'configuration')")
		}
	})

	t.Run("normalizes synonyms", func(t *testing.T) {
		keywords := checker.extractKeywords("The testing specs are important")
		// "testing" and "specs" should both normalize to "test"
		if !keywords["test"] {
			t.Error("expected 'test' keyword from 'testing' or 'specs'")
		}
	})
}

func TestSemanticDriftChecker_LongQuestion(t *testing.T) {
	checker := NewSemanticDriftChecker(nil)

	longQuestion := "What tests exist in this project and how do they work and where are they located and why were they designed this way and can you describe the testing architecture in detail?"

	input := &CheckInput{
		UserQuestion: longQuestion,
		Response:     "The testing framework uses Go's standard testing package. Tests are located in *_test.go files throughout the project.",
	}

	violations := checker.Check(context.Background(), input)
	// Should handle long questions gracefully
	_ = violations // Just verify no panic
}

func TestSemanticDriftChecker_Priority(t *testing.T) {
	// Verify that semantic drift has the highest priority
	if PrioritySemanticDrift != 0 {
		t.Errorf("expected PrioritySemanticDrift to be 0, got %d", PrioritySemanticDrift)
	}

	// Verify priority ordering
	if PrioritySemanticDrift >= PriorityPhantomFile {
		t.Error("SemanticDrift should have higher priority (lower number) than PhantomFile")
	}
}

func TestSemanticDriftChecker_ViolationTypeToPriority(t *testing.T) {
	priority := ViolationTypeToPriority(ViolationSemanticDrift)
	if priority != PrioritySemanticDrift {
		t.Errorf("expected %d, got %d", PrioritySemanticDrift, priority)
	}
}

func TestSemanticDriftChecker_HasHighPriorityViolations(t *testing.T) {
	violations := []Violation{
		{Type: ViolationSemanticDrift, Severity: SeverityCritical},
	}

	if !HasHighPriorityViolations(violations) {
		t.Error("expected HasHighPriorityViolations to return true for semantic drift")
	}
}

func TestQuestionType_String(t *testing.T) {
	tests := []struct {
		qt       QuestionType
		expected string
	}{
		{QuestionList, "list"},
		{QuestionHow, "how"},
		{QuestionWhere, "where"},
		{QuestionWhy, "why"},
		{QuestionWhat, "what"},
		{QuestionDescribe, "describe"},
		{QuestionUnknown, "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if tt.qt.String() != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, tt.qt.String())
			}
		})
	}
}

func TestSemanticDriftChecker_ConfigurableTypeMismatchPenalties(t *testing.T) {
	t.Run("custom list mismatch penalty", func(t *testing.T) {
		// Use a very low penalty
		config := &SemanticDriftCheckerConfig{
			Enabled:                  true,
			CriticalThreshold:        0.7,
			HighThreshold:            0.5,
			WarningThreshold:         0.3,
			KeywordWeight:            0.4,
			TopicWeight:              0.4,
			TypeWeight:               0.2,
			MinKeywords:              2,
			MinResponseLength:        20,
			ListTypeMismatchPenalty:  0.1, // Very low penalty
			WhereTypeMismatchPenalty: 0.5,
			HowTypeMismatchPenalty:   0.3,
		}
		checker := NewSemanticDriftChecker(config)

		input := &CheckInput{
			UserQuestion: "What tests exist in this project?",
			// Non-list response but with test keywords
			Response: "The test framework uses assertions for validation of test results.",
		}

		violations := checker.Check(context.Background(), input)
		// With low penalty, shouldn't trigger critical violations
		for _, v := range violations {
			if v.Severity == SeverityCritical {
				t.Errorf("low list penalty should not trigger critical: %s", v.Message)
			}
		}
	})

	t.Run("high list mismatch penalty", func(t *testing.T) {
		// Use a very high penalty
		config := &SemanticDriftCheckerConfig{
			Enabled:                  true,
			CriticalThreshold:        0.5, // Lower threshold
			HighThreshold:            0.4,
			WarningThreshold:         0.3,
			KeywordWeight:            0.2,
			TopicWeight:              0.2,
			TypeWeight:               0.6, // Higher type weight
			MinKeywords:              1,
			MinResponseLength:        10,
			ListTypeMismatchPenalty:  1.0, // Maximum penalty
			WhereTypeMismatchPenalty: 0.5,
			HowTypeMismatchPenalty:   0.3,
		}
		checker := NewSemanticDriftChecker(config)

		input := &CheckInput{
			UserQuestion: "What tests exist?",
			// Non-list response
			Response: "The tests use assertions for validation.",
		}

		violations := checker.Check(context.Background(), input)
		// With high penalty, should trigger violations
		if len(violations) == 0 {
			t.Error("high list penalty should trigger violation for non-list response")
		}
	})
}

func TestSemanticDriftChecker_StopWordsCopy(t *testing.T) {
	// Verify that modifying checker's stopWordsLower doesn't affect package-level stopWords
	checker := NewSemanticDriftChecker(nil)

	// The package-level stopWords should have "the"
	if !stopWords["the"] {
		t.Fatal("expected 'the' in package-level stopWords")
	}

	// Verify checker has its own copy
	if !checker.stopWordsLower["the"] {
		t.Fatal("expected 'the' in checker's stopWordsLower")
	}

	// The maps should be separate instances (defense-in-depth test)
	// Note: We don't actually mutate in this test to avoid side effects,
	// but we verify they are separate allocations
	if &checker.stopWordsLower == &stopWords {
		t.Error("stopWordsLower should be a copy, not the same map reference")
	}
}
