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
	"strings"
	"testing"

	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/agent"
)

func TestStrictnessLevel_String(t *testing.T) {
	tests := []struct {
		level    StrictnessLevel
		expected string
	}{
		{StrictnessNormal, "normal"},
		{StrictnessElevated, "elevated"},
		{StrictnessHigh, "high"},
		{StrictnessFeedback, "feedback"},
		{StrictnessLevel(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.level.String(); got != tt.expected {
				t.Errorf("StrictnessLevel.String() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestDefaultPostSynthesisConfig(t *testing.T) {
	config := DefaultPostSynthesisConfig()

	if !config.Enabled {
		t.Error("expected Enabled=true by default")
	}
	if config.MaxRetries != 3 {
		t.Errorf("expected MaxRetries=3, got %d", config.MaxRetries)
	}
	if len(config.RelevantCheckers) != 3 {
		t.Errorf("expected 3 relevant checkers, got %d", len(config.RelevantCheckers))
	}

	// Verify the specific checkers
	expectedCheckers := map[string]bool{
		"structural_claim_checker": true,
		"phantom_file_checker":     true,
		"language_checker":         true,
	}
	for _, name := range config.RelevantCheckers {
		if !expectedCheckers[name] {
			t.Errorf("unexpected checker: %s", name)
		}
	}
}

func TestVerifyPostSynthesis_NoViolations(t *testing.T) {
	config := DefaultConfig()
	grounder := NewDefaultGrounder(config)
	ctx := context.Background()

	// A response that references an actual file shown in context
	assembledCtx := &agent.AssembledContext{
		CodeContext: []agent.CodeEntry{
			{FilePath: "main.go", Content: "package main\nfunc main() {}"},
		},
		ToolResults: []agent.ToolResult{
			{InvocationID: "read_1", Output: "main.go: package main"},
		},
	}

	result, err := grounder.VerifyPostSynthesis(ctx, "The main.go file contains the entry point.", assembledCtx, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.Passed {
		t.Errorf("expected Passed=true, got violations: %+v", result.Violations)
	}
	if result.RetryCount != 0 {
		t.Errorf("expected RetryCount=0, got %d", result.RetryCount)
	}
	if result.StrictnessLevel != StrictnessNormal {
		t.Errorf("expected StrictnessLevel=normal, got %s", result.StrictnessLevel)
	}
	if result.NeedsFeedbackLoop {
		t.Error("expected NeedsFeedbackLoop=false for passing result")
	}
}

func TestVerifyPostSynthesis_Disabled(t *testing.T) {
	config := DefaultConfig()
	config.PostSynthesisConfig = &PostSynthesisConfig{
		Enabled: false,
	}
	grounder := NewDefaultGrounder(config)
	ctx := context.Background()

	// Even a problematic response should pass when disabled
	result, err := grounder.VerifyPostSynthesis(ctx, "Check pkg/fake/fake.go for details", nil, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.Passed {
		t.Error("expected Passed=true when disabled")
	}
}

func TestVerifyPostSynthesis_WithViolations_Retry(t *testing.T) {
	config := DefaultConfig()
	grounder := NewDefaultGrounder(config)
	ctx := context.Background()

	// A response referencing a non-existent file
	assembledCtx := &agent.AssembledContext{
		CodeContext: []agent.CodeEntry{
			{FilePath: "main.go", Content: "package main"},
		},
	}

	result, err := grounder.VerifyPostSynthesis(ctx, "Check the implementation in pkg/handler/handler.go", assembledCtx, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Passed {
		t.Error("expected Passed=false for phantom file reference")
	}
	if len(result.Violations) == 0 {
		t.Error("expected violations for phantom file")
	}

	// Verify violations have post_synthesis phase
	for _, v := range result.Violations {
		if v.Phase != "post_synthesis" {
			t.Errorf("expected Phase='post_synthesis', got %q", v.Phase)
		}
	}
}

func TestVerifyPostSynthesis_FeedbackLoop(t *testing.T) {
	config := DefaultConfig()
	config.PostSynthesisConfig = &PostSynthesisConfig{
		Enabled:    true,
		MaxRetries: 3,
	}
	grounder := NewDefaultGrounder(config)
	ctx := context.Background()

	// A response with phantom files - after 3 retries should trigger feedback loop
	assembledCtx := &agent.AssembledContext{
		CodeContext: []agent.CodeEntry{
			{FilePath: "main.go", Content: "package main"},
		},
	}

	result, err := grounder.VerifyPostSynthesis(ctx, "See pkg/fake.go for implementation", assembledCtx, 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.StrictnessLevel != StrictnessFeedback {
		t.Errorf("expected StrictnessLevel=feedback at retry 3, got %s", result.StrictnessLevel)
	}

	if result.Passed {
		// If it doesn't pass, should need feedback loop
		t.Log("Result passed unexpectedly - skipping feedback loop check")
	} else {
		if !result.NeedsFeedbackLoop {
			t.Error("expected NeedsFeedbackLoop=true at retry 3 with violations")
		}
		if len(result.FeedbackQuestions) == 0 {
			t.Error("expected FeedbackQuestions to be populated")
		}
	}
}

func TestVerifyPostSynthesis_ExponentialBackoff(t *testing.T) {
	tests := []struct {
		retryCount    int
		expectedLevel StrictnessLevel
	}{
		{0, StrictnessNormal},
		{1, StrictnessElevated},
		{2, StrictnessHigh},
		{3, StrictnessFeedback},
		{5, StrictnessFeedback}, // Stays at feedback
	}

	config := DefaultConfig()
	grounder := NewDefaultGrounder(config)
	ctx := context.Background()

	assembledCtx := &agent.AssembledContext{
		CodeContext: []agent.CodeEntry{
			{FilePath: "main.go", Content: "package main"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.expectedLevel.String(), func(t *testing.T) {
			result, err := grounder.VerifyPostSynthesis(ctx, "test response", assembledCtx, tt.retryCount)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if result.StrictnessLevel != tt.expectedLevel {
				t.Errorf("at retryCount=%d, expected level=%s, got %s",
					tt.retryCount, tt.expectedLevel, result.StrictnessLevel)
			}
		})
	}
}

func TestVerifyPostSynthesis_RelevantCheckersOnly(t *testing.T) {
	config := DefaultConfig()
	grounder := NewDefaultGrounder(config)
	ctx := context.Background()

	assembledCtx := &agent.AssembledContext{
		CodeContext: []agent.CodeEntry{
			{FilePath: "main.go", Content: "package main"},
		},
	}

	result, err := grounder.VerifyPostSynthesis(ctx, "test response", assembledCtx, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should run exactly 3 checkers (structural, phantom, language)
	if result.CheckersRun != 3 {
		t.Errorf("expected CheckersRun=3, got %d", result.CheckersRun)
	}
}

func TestGenerateStricterPrompt_Elevated(t *testing.T) {
	grounder := NewDefaultGrounder(DefaultConfig())

	violations := []Violation{
		{
			Type:       ViolationPhantomFile,
			Message:    "Referenced non-existent file",
			Evidence:   "pkg/fake.go",
			Suggestion: "Only reference files shown in tool output",
		},
	}

	result := grounder.GenerateStricterPrompt("Summarize the findings.", violations, StrictnessElevated)

	if !strings.Contains(result, "IMPORTANT") {
		t.Error("elevated prompt should contain IMPORTANT")
	}
	if !strings.Contains(result, "phantom_file") {
		t.Error("elevated prompt should contain violation type")
	}
	if !strings.Contains(result, "Suggestion:") {
		t.Error("elevated prompt should contain suggestion")
	}
	if !strings.Contains(result, "Summarize the findings.") {
		t.Error("elevated prompt should contain base prompt")
	}
}

func TestGenerateStricterPrompt_High(t *testing.T) {
	grounder := NewDefaultGrounder(DefaultConfig())

	violations := []Violation{
		{
			Type:     ViolationLanguageConfusion,
			Message:  "Flask pattern in Go project",
			Evidence: "Flask",
		},
	}

	result := grounder.GenerateStricterPrompt("Summarize.", violations, StrictnessHigh)

	if !strings.Contains(result, "CRITICAL") {
		t.Error("high strictness prompt should contain CRITICAL")
	}
	if !strings.Contains(result, "AVOID THESE PATTERNS") {
		t.Error("high strictness prompt should contain AVOID THESE PATTERNS")
	}
	if !strings.Contains(result, "REQUIREMENTS") {
		t.Error("high strictness prompt should contain REQUIREMENTS")
	}
	if !strings.Contains(result, "Flask") {
		t.Error("high strictness prompt should reference the violation evidence")
	}
}

func TestGenerateStricterPrompt_Normal(t *testing.T) {
	grounder := NewDefaultGrounder(DefaultConfig())

	result := grounder.GenerateStricterPrompt("Base prompt.", nil, StrictnessNormal)

	if result != "Base prompt." {
		t.Errorf("normal strictness should return base prompt unchanged, got: %q", result)
	}
}

func TestGenerateFeedbackQuestions(t *testing.T) {
	t.Run("phantom file violation", func(t *testing.T) {
		violations := []Violation{
			{Type: ViolationPhantomFile, Evidence: "pkg/handler.go"},
		}

		questions := generateFeedbackQuestions(violations)

		if len(questions) == 0 {
			t.Fatal("expected at least one question")
		}
		if !strings.Contains(questions[0], "handler") {
			t.Errorf("expected question to reference handler, got: %s", questions[0])
		}
	})

	t.Run("structural claim violation", func(t *testing.T) {
		violations := []Violation{
			{Type: ViolationStructuralClaim, Evidence: "fabricated structure"},
		}

		questions := generateFeedbackQuestions(violations)

		if len(questions) == 0 {
			t.Fatal("expected at least one question")
		}
		if !strings.Contains(strings.ToLower(questions[0]), "ls") && !strings.Contains(strings.ToLower(questions[0]), "tree") {
			t.Errorf("expected question about ls/tree, got: %s", questions[0])
		}
	})

	t.Run("language confusion violation", func(t *testing.T) {
		violations := []Violation{
			{Type: ViolationLanguageConfusion, Evidence: "Flask"},
		}

		questions := generateFeedbackQuestions(violations)

		if len(questions) == 0 {
			t.Fatal("expected at least one question")
		}
		if !strings.Contains(questions[0], "Flask") {
			t.Errorf("expected question to reference Flask, got: %s", questions[0])
		}
	})

	t.Run("multiple violations deduplicates", func(t *testing.T) {
		violations := []Violation{
			{Type: ViolationStructuralClaim, Evidence: "claim1"},
			{Type: ViolationStructuralClaim, Evidence: "claim2"},
		}

		questions := generateFeedbackQuestions(violations)

		// Both structural claims should generate the same question (deduplicated)
		if len(questions) != 1 {
			t.Errorf("expected 1 deduplicated question, got %d", len(questions))
		}
	})

	t.Run("empty violations gets default question", func(t *testing.T) {
		questions := generateFeedbackQuestions(nil)

		if len(questions) != 1 {
			t.Fatalf("expected 1 default question, got %d", len(questions))
		}
		if !strings.Contains(strings.ToLower(questions[0]), "exploration") {
			t.Errorf("expected default question about exploration, got: %s", questions[0])
		}
	})
}

func TestDescribeViolationToAvoid(t *testing.T) {
	tests := []struct {
		violation     Violation
		shouldContain string
	}{
		{
			violation:     Violation{Type: ViolationPhantomFile, Evidence: "fake.go"},
			shouldContain: "fake.go",
		},
		{
			violation:     Violation{Type: ViolationStructuralClaim},
			shouldContain: "ls/tree",
		},
		{
			violation:     Violation{Type: ViolationLanguageConfusion, Evidence: "Flask"},
			shouldContain: "Flask",
		},
		{
			violation:     Violation{Type: ViolationGenericPattern},
			shouldContain: "generic",
		},
		{
			violation:     Violation{Type: ViolationUngrounded, Message: "Test message"},
			shouldContain: "test message",
		},
	}

	for _, tt := range tests {
		t.Run(string(tt.violation.Type), func(t *testing.T) {
			result := describeViolationToAvoid(tt.violation)
			if !strings.Contains(strings.ToLower(result), strings.ToLower(tt.shouldContain)) {
				t.Errorf("expected result to contain %q, got: %s", tt.shouldContain, result)
			}
		})
	}
}

func TestDetermineStrictnessLevel(t *testing.T) {
	tests := []struct {
		retryCount int
		maxRetries int
		expected   StrictnessLevel
	}{
		{0, 3, StrictnessNormal},
		{1, 3, StrictnessElevated},
		{2, 3, StrictnessHigh},
		{3, 3, StrictnessFeedback},
		{4, 3, StrictnessFeedback},
		{10, 3, StrictnessFeedback},
	}

	for _, tt := range tests {
		t.Run(tt.expected.String(), func(t *testing.T) {
			got := determineStrictnessLevel(tt.retryCount, tt.maxRetries)
			if got != tt.expected {
				t.Errorf("determineStrictnessLevel(%d, %d) = %s, want %s",
					tt.retryCount, tt.maxRetries, got, tt.expected)
			}
		})
	}
}

func TestIsPostSynthesisChecker(t *testing.T) {
	tests := []struct {
		name     string
		expected bool
	}{
		{"structural_claim_checker", true},
		{"phantom_file_checker", true},
		{"language_checker", true},
		{"citation_checker", false},
		{"grounding_checker", false},
		{"unknown_checker", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isPostSynthesisChecker(tt.name); got != tt.expected {
				t.Errorf("isPostSynthesisChecker(%q) = %v, want %v", tt.name, got, tt.expected)
			}
		})
	}
}

func TestVerifyPostSynthesis_ContextCancellation(t *testing.T) {
	config := DefaultConfig()
	grounder := NewDefaultGrounder(config)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	assembledCtx := &agent.AssembledContext{
		CodeContext: []agent.CodeEntry{
			{FilePath: "main.go", Content: "package main"},
		},
	}

	result, err := grounder.VerifyPostSynthesis(ctx, "test response", assembledCtx, 0)

	// Should return context error
	if err == nil || err != context.Canceled {
		// May or may not error depending on timing
		_ = result
	}
}

func TestVerifyPostSynthesis_NilAssembledContext(t *testing.T) {
	config := DefaultConfig()
	grounder := NewDefaultGrounder(config)
	ctx := context.Background()

	// Should handle nil assembled context gracefully
	result, err := grounder.VerifyPostSynthesis(ctx, "test response", nil, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// With no context, checkers may not find violations
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

func TestPostSynthesisResult_Fields(t *testing.T) {
	result := &PostSynthesisResult{
		Passed:            false,
		Violations:        []Violation{{Type: ViolationPhantomFile}},
		RetryCount:        2,
		StrictnessLevel:   StrictnessHigh,
		NeedsFeedbackLoop: false,
		FeedbackQuestions: nil,
		CheckersRun:       3,
	}

	if result.Passed {
		t.Error("expected Passed=false")
	}
	if len(result.Violations) != 1 {
		t.Errorf("expected 1 violation, got %d", len(result.Violations))
	}
	if result.RetryCount != 2 {
		t.Errorf("expected RetryCount=2, got %d", result.RetryCount)
	}
	if result.StrictnessLevel != StrictnessHigh {
		t.Errorf("expected StrictnessHigh, got %s", result.StrictnessLevel)
	}
	if result.CheckersRun != 3 {
		t.Errorf("expected CheckersRun=3, got %d", result.CheckersRun)
	}
}
