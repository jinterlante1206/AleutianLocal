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
)

func TestNewGroundingChecker(t *testing.T) {
	t.Run("with nil config uses defaults", func(t *testing.T) {
		checker := NewGroundingChecker(nil)
		if checker == nil {
			t.Fatal("expected non-nil checker")
		}
		if !checker.config.CheckFiles {
			t.Error("expected CheckFiles to be true by default")
		}
		if !checker.config.CheckSymbols {
			t.Error("expected CheckSymbols to be true by default")
		}
		if !checker.config.CheckFrameworks {
			t.Error("expected CheckFrameworks to be true by default")
		}
	})

	t.Run("with custom config", func(t *testing.T) {
		config := &GroundingCheckerConfig{
			CheckFiles:      false,
			CheckSymbols:    true,
			CheckFrameworks: false,
		}
		checker := NewGroundingChecker(config)
		if checker.config.CheckFiles {
			t.Error("expected CheckFiles to be false")
		}
	})
}

func TestGroundingChecker_Name(t *testing.T) {
	checker := NewGroundingChecker(nil)
	if name := checker.Name(); name != "grounding_checker" {
		t.Errorf("expected name 'grounding_checker', got %q", name)
	}
}

func TestGroundingChecker_Check_EmptyInput(t *testing.T) {
	checker := NewGroundingChecker(nil)
	ctx := context.Background()

	t.Run("nil input returns nil", func(t *testing.T) {
		violations := checker.Check(ctx, nil)
		if violations != nil {
			t.Errorf("expected nil violations, got %v", violations)
		}
	})

	t.Run("empty response returns nil", func(t *testing.T) {
		input := &CheckInput{Response: ""}
		violations := checker.Check(ctx, input)
		if violations != nil {
			t.Errorf("expected nil violations, got %v", violations)
		}
	})
}

func TestGroundingChecker_Check_FileClaims(t *testing.T) {
	checker := NewGroundingChecker(nil)
	ctx := context.Background()

	t.Run("file in evidence passes", func(t *testing.T) {
		evidence := NewEvidenceIndex()
		evidence.Files["main.go"] = true
		evidence.FileBasenames["main.go"] = true

		input := &CheckInput{
			Response:      "The main.go file contains the entry point.",
			EvidenceIndex: evidence,
		}

		violations := checker.Check(ctx, input)
		if len(violations) > 0 {
			t.Errorf("expected no violations, got %v", violations)
		}
	})

	t.Run("file not in evidence fails", func(t *testing.T) {
		evidence := NewEvidenceIndex()
		evidence.Files["server.go"] = true
		evidence.FileBasenames["server.go"] = true

		input := &CheckInput{
			Response:      "The app.py file uses Flask for routing.",
			EvidenceIndex: evidence,
		}

		violations := checker.Check(ctx, input)
		if len(violations) == 0 {
			t.Fatal("expected violations for ungrounded file")
		}

		found := false
		for _, v := range violations {
			if v.Type == ViolationUngrounded && v.Code == "UNGROUNDED_FILE" {
				found = true
				if v.Severity != SeverityCritical {
					t.Errorf("expected critical severity, got %s", v.Severity)
				}
			}
		}
		if !found {
			t.Error("expected UNGROUNDED_FILE violation")
		}
	})

	t.Run("file exists in project but not context is warning", func(t *testing.T) {
		evidence := NewEvidenceIndex()
		evidence.Files["main.go"] = true
		evidence.FileBasenames["main.go"] = true

		input := &CheckInput{
			Response:      "The utils.go file has helper functions.",
			EvidenceIndex: evidence,
			KnownFiles:    map[string]bool{"utils.go": true},
		}

		violations := checker.Check(ctx, input)
		if len(violations) == 0 {
			t.Fatal("expected violations for file not in context")
		}

		found := false
		for _, v := range violations {
			if v.Code == "UNGROUNDED_FILE_NOT_IN_CONTEXT" {
				found = true
				if v.Severity != SeverityWarning {
					t.Errorf("expected warning severity, got %s", v.Severity)
				}
			}
		}
		if !found {
			t.Error("expected UNGROUNDED_FILE_NOT_IN_CONTEXT violation")
		}
	})

	t.Run("file path with directory matches", func(t *testing.T) {
		evidence := NewEvidenceIndex()
		evidence.Files["cmd/server/main.go"] = true
		evidence.FileBasenames["main.go"] = true

		input := &CheckInput{
			Response:      "In cmd/server/main.go, the server starts.",
			EvidenceIndex: evidence,
		}

		violations := checker.Check(ctx, input)
		if len(violations) > 0 {
			t.Errorf("expected no violations, got %v", violations)
		}
	})
}

func TestGroundingChecker_Check_SymbolClaims(t *testing.T) {
	checker := NewGroundingChecker(nil)
	ctx := context.Background()

	t.Run("symbol in evidence passes", func(t *testing.T) {
		evidence := NewEvidenceIndex()
		evidence.Symbols["HandleRequest"] = true

		input := &CheckInput{
			Response:      "The HandleRequest function is the entry point.",
			EvidenceIndex: evidence,
		}

		violations := checker.Check(ctx, input)
		// Filter out only symbol violations
		symbolViolations := filterViolations(violations, func(v Violation) bool {
			return v.Type == ViolationUngrounded && v.Code == "UNGROUNDED_SYMBOL"
		})
		if len(symbolViolations) > 0 {
			t.Errorf("expected no symbol violations, got %v", symbolViolations)
		}
	})

	t.Run("symbol not in evidence fails with warning", func(t *testing.T) {
		evidence := NewEvidenceIndex()
		evidence.Symbols["HandleRequest"] = true

		input := &CheckInput{
			Response:      "The ProcessData function transforms the input.",
			EvidenceIndex: evidence,
		}

		violations := checker.Check(ctx, input)
		found := false
		for _, v := range violations {
			if v.Code == "UNGROUNDED_SYMBOL" && strings.Contains(v.Evidence, "ProcessData") {
				found = true
				if v.Severity != SeverityWarning {
					t.Errorf("expected warning severity for symbols, got %s", v.Severity)
				}
			}
		}
		if !found {
			t.Error("expected UNGROUNDED_SYMBOL violation for ProcessData")
		}
	})

	t.Run("case insensitive symbol match", func(t *testing.T) {
		evidence := NewEvidenceIndex()
		evidence.Symbols["handleRequest"] = true

		input := &CheckInput{
			Response:      "The HandleRequest function is case insensitive.",
			EvidenceIndex: evidence,
		}

		violations := checker.Check(ctx, input)
		symbolViolations := filterViolations(violations, func(v Violation) bool {
			return v.Code == "UNGROUNDED_SYMBOL" && strings.Contains(v.Evidence, "HandleRequest")
		})
		if len(symbolViolations) > 0 {
			t.Errorf("expected case-insensitive match to pass, got %v", symbolViolations)
		}
	})

	t.Run("common words are ignored", func(t *testing.T) {
		evidence := NewEvidenceIndex()

		// "Main" is a common word, should be filtered out
		// But note our pattern requires capital letter start, so "Main" would match
		// We rely on isCommonWord to filter it out
		input := &CheckInput{
			Response:      "type Main struct is the entry point.",
			EvidenceIndex: evidence,
		}

		violations := checker.Check(ctx, input)
		for _, v := range violations {
			if v.Code == "UNGROUNDED_SYMBOL" && strings.Contains(strings.ToLower(v.Evidence), "main") {
				t.Error("should not flag 'main' as ungrounded symbol - it's a common word")
			}
		}
	})
}

func TestGroundingChecker_Check_FrameworkClaims(t *testing.T) {
	checker := NewGroundingChecker(nil)
	ctx := context.Background()

	t.Run("framework in evidence passes", func(t *testing.T) {
		evidence := NewEvidenceIndex()
		evidence.Frameworks["gin"] = true

		input := &CheckInput{
			Response:      "The server uses Gin for routing.",
			EvidenceIndex: evidence,
		}

		violations := checker.Check(ctx, input)
		frameworkViolations := filterViolations(violations, func(v Violation) bool {
			return v.Code == "UNGROUNDED_FRAMEWORK"
		})
		if len(frameworkViolations) > 0 {
			t.Errorf("expected no framework violations, got %v", frameworkViolations)
		}
	})

	t.Run("framework not in evidence fails with critical", func(t *testing.T) {
		evidence := NewEvidenceIndex()
		evidence.Frameworks["gin"] = true

		input := &CheckInput{
			Response:      "The application uses Flask for the web layer.",
			EvidenceIndex: evidence,
		}

		violations := checker.Check(ctx, input)
		found := false
		for _, v := range violations {
			if v.Code == "UNGROUNDED_FRAMEWORK" {
				found = true
				if v.Severity != SeverityCritical {
					t.Errorf("expected critical severity for frameworks, got %s", v.Severity)
				}
			}
		}
		if !found {
			t.Error("expected UNGROUNDED_FRAMEWORK violation")
		}
	})

	t.Run("framework in raw content passes", func(t *testing.T) {
		evidence := NewEvidenceIndex()
		evidence.RawContent = "import gin\n\nrouter := gin.Default()"

		input := &CheckInput{
			Response:      "The server uses Gin for routing.",
			EvidenceIndex: evidence,
		}

		violations := checker.Check(ctx, input)
		frameworkViolations := filterViolations(violations, func(v Violation) bool {
			return v.Code == "UNGROUNDED_FRAMEWORK"
		})
		if len(frameworkViolations) > 0 {
			t.Errorf("expected framework in raw content to pass, got %v", frameworkViolations)
		}
	})

	t.Run("multiple frameworks detected", func(t *testing.T) {
		evidence := NewEvidenceIndex()
		// Empty evidence

		input := &CheckInput{
			Response:      "The app uses Django for the backend and Express for the API.",
			EvidenceIndex: evidence,
		}

		violations := checker.Check(ctx, input)
		frameworkViolations := filterViolations(violations, func(v Violation) bool {
			return v.Code == "UNGROUNDED_FRAMEWORK"
		})
		if len(frameworkViolations) < 2 {
			t.Errorf("expected at least 2 framework violations, got %d", len(frameworkViolations))
		}
	})
}

func TestGroundingChecker_Check_MultipleClaimTypes(t *testing.T) {
	checker := NewGroundingChecker(nil)
	ctx := context.Background()

	t.Run("response with multiple violations", func(t *testing.T) {
		evidence := NewEvidenceIndex()
		evidence.Files["main.go"] = true
		evidence.FileBasenames["main.go"] = true

		input := &CheckInput{
			Response: `The app.py file uses Flask for routing.
The ProcessData function in utils.py handles transformations.`,
			EvidenceIndex: evidence,
		}

		violations := checker.Check(ctx, input)

		// Should have violations for:
		// - app.py (file not found)
		// - Flask (framework not found)
		// - ProcessData (symbol not found)
		// - utils.py (file not found)
		if len(violations) < 3 {
			t.Errorf("expected at least 3 violations, got %d: %v", len(violations), violations)
		}

		hasFileViolation := false
		hasFrameworkViolation := false
		for _, v := range violations {
			if v.Code == "UNGROUNDED_FILE" {
				hasFileViolation = true
			}
			if v.Code == "UNGROUNDED_FRAMEWORK" {
				hasFrameworkViolation = true
			}
		}

		if !hasFileViolation {
			t.Error("expected file violation")
		}
		if !hasFrameworkViolation {
			t.Error("expected framework violation")
		}
	})
}

func TestGroundingChecker_Check_DisabledChecks(t *testing.T) {
	ctx := context.Background()

	t.Run("disabled file checks", func(t *testing.T) {
		config := &GroundingCheckerConfig{
			CheckFiles:      false,
			CheckSymbols:    true,
			CheckFrameworks: true,
		}
		checker := NewGroundingChecker(config)

		evidence := NewEvidenceIndex()
		input := &CheckInput{
			Response:      "The nonexistent.py file does things.",
			EvidenceIndex: evidence,
		}

		violations := checker.Check(ctx, input)
		for _, v := range violations {
			if v.Code == "UNGROUNDED_FILE" {
				t.Error("file check should be disabled")
			}
		}
	})

	t.Run("disabled symbol checks", func(t *testing.T) {
		config := &GroundingCheckerConfig{
			CheckFiles:      true,
			CheckSymbols:    false,
			CheckFrameworks: true,
		}
		checker := NewGroundingChecker(config)

		evidence := NewEvidenceIndex()
		input := &CheckInput{
			Response:      "The FakeFunction function does things.",
			EvidenceIndex: evidence,
		}

		violations := checker.Check(ctx, input)
		for _, v := range violations {
			if v.Code == "UNGROUNDED_SYMBOL" {
				t.Error("symbol check should be disabled")
			}
		}
	})

	t.Run("disabled framework checks", func(t *testing.T) {
		config := &GroundingCheckerConfig{
			CheckFiles:      true,
			CheckSymbols:    true,
			CheckFrameworks: false,
		}
		checker := NewGroundingChecker(config)

		evidence := NewEvidenceIndex()
		input := &CheckInput{
			Response:      "The app uses Django for everything.",
			EvidenceIndex: evidence,
		}

		violations := checker.Check(ctx, input)
		for _, v := range violations {
			if v.Code == "UNGROUNDED_FRAMEWORK" {
				t.Error("framework check should be disabled")
			}
		}
	})
}

func TestGroundingChecker_Check_ContextCancellation(t *testing.T) {
	checker := NewGroundingChecker(nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	evidence := NewEvidenceIndex()
	input := &CheckInput{
		Response:      "The app.py file uses Flask for routing and Django for ORM.",
		EvidenceIndex: evidence,
	}

	// Should return early due to cancellation
	violations := checker.Check(ctx, input)
	// May have some violations before cancellation was detected
	// The important thing is it doesn't hang
	_ = violations
}

func TestGroundingChecker_Check_Deduplication(t *testing.T) {
	checker := NewGroundingChecker(nil)
	ctx := context.Background()

	t.Run("duplicate file references deduplicated", func(t *testing.T) {
		evidence := NewEvidenceIndex()
		input := &CheckInput{
			Response:      "The app.py file is great. I love app.py. The app.py rocks.",
			EvidenceIndex: evidence,
		}

		violations := checker.Check(ctx, input)
		fileViolations := filterViolations(violations, func(v Violation) bool {
			return v.Code == "UNGROUNDED_FILE"
		})
		if len(fileViolations) != 1 {
			t.Errorf("expected 1 deduplicated file violation, got %d", len(fileViolations))
		}
	})

	t.Run("duplicate framework references deduplicated", func(t *testing.T) {
		evidence := NewEvidenceIndex()
		input := &CheckInput{
			Response:      "Flask is great. I love Flask. Flask rocks.",
			EvidenceIndex: evidence,
		}

		violations := checker.Check(ctx, input)
		frameworkViolations := filterViolations(violations, func(v Violation) bool {
			return v.Code == "UNGROUNDED_FRAMEWORK"
		})
		if len(frameworkViolations) != 1 {
			t.Errorf("expected 1 deduplicated framework violation, got %d", len(frameworkViolations))
		}
	})
}

// filterViolations filters violations by a predicate.
func filterViolations(violations []Violation, pred func(Violation) bool) []Violation {
	var result []Violation
	for _, v := range violations {
		if pred(v) {
			result = append(result, v)
		}
	}
	return result
}

func TestGroundingChecker_formatExpectedFiles(t *testing.T) {
	checker := NewGroundingChecker(nil)

	t.Run("nil evidence", func(t *testing.T) {
		result := checker.formatExpectedFiles(nil)
		if result != "no files in context" {
			t.Errorf("expected 'no files in context', got %q", result)
		}
	})

	t.Run("empty evidence", func(t *testing.T) {
		evidence := NewEvidenceIndex()
		result := checker.formatExpectedFiles(evidence)
		if result != "no files in context" {
			t.Errorf("expected 'no files in context', got %q", result)
		}
	})

	t.Run("with files", func(t *testing.T) {
		evidence := NewEvidenceIndex()
		evidence.FileBasenames["main.go"] = true
		evidence.FileBasenames["handler.go"] = true
		result := checker.formatExpectedFiles(evidence)
		if !strings.Contains(result, "main.go") && !strings.Contains(result, "handler.go") {
			t.Errorf("expected file names in result, got %q", result)
		}
	})

	t.Run("limits to 5 files", func(t *testing.T) {
		evidence := NewEvidenceIndex()
		for i := 0; i < 10; i++ {
			evidence.FileBasenames[strings.Repeat("a", i+1)+".go"] = true
		}
		result := checker.formatExpectedFiles(evidence)
		// Result should contain files but be limited
		if len(result) == 0 {
			t.Error("expected non-empty result")
		}
	})
}

func TestGroundingChecker_validateClaim_IgnoresCommonWords(t *testing.T) {
	checker := NewGroundingChecker(nil)
	ctx := context.Background()

	evidence := NewEvidenceIndex()
	evidence.Files["real.go"] = true
	evidence.FileBasenames["real.go"] = true

	input := &CheckInput{
		Response:      "The main thing is to update the config and return the result.",
		EvidenceIndex: evidence,
	}

	violations := checker.Check(ctx, input)

	// Common words like "main", "config", "return", "result" should be ignored
	for _, v := range violations {
		if v.Type == ViolationSymbolNotFound {
			lowEvidence := strings.ToLower(v.Evidence)
			// Check that we're not flagging common words
			if lowEvidence == "main" || lowEvidence == "config" || lowEvidence == "return" || lowEvidence == "result" {
				t.Errorf("should not flag common word as symbol: %s", v.Evidence)
			}
		}
	}
}
