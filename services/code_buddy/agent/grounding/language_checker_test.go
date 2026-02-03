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

func TestLanguageChecker_Name(t *testing.T) {
	checker := NewLanguageChecker(nil)
	if checker.Name() != "language_checker" {
		t.Errorf("expected name 'language_checker', got '%s'", checker.Name())
	}
}

func TestLanguageChecker_Check_PythonInGoProject(t *testing.T) {
	checker := NewLanguageChecker(nil)
	ctx := context.Background()

	// Response contains Python patterns in a Go project
	input := &CheckInput{
		Response: `To implement this feature, you should use Flask:

from flask import Flask, request
app = Flask(__name__)

@app.route('/api/endpoint')
def handle_request():
    return jsonify({"status": "ok"})

if __name__ == '__main__':
    app.run()

You can install it with pip install flask.`,
		ProjectLang: "go",
	}

	violations := checker.Check(ctx, input)

	if len(violations) == 0 {
		t.Fatal("expected violations for Python code in Go project, got none")
	}

	// Should detect either framework blocklist or language confusion
	foundFramework := false
	foundLanguage := false
	for _, v := range violations {
		if v.Type == ViolationLanguageConfusion && v.Severity == SeverityCritical {
			if v.Code == "LANGUAGE_CONFUSION_FRAMEWORK" {
				foundFramework = true
			}
			if v.Code == "LANGUAGE_CONFUSION_PYTHON" {
				foundLanguage = true
			}
		}
	}

	if !foundFramework && !foundLanguage {
		t.Error("expected critical language_confusion violation for Python patterns")
	}
}

func TestLanguageChecker_Check_GoInPythonProject(t *testing.T) {
	checker := NewLanguageChecker(nil)
	ctx := context.Background()

	// Response contains Go patterns in a Python project
	input := &CheckInput{
		Response: `Here's how to handle errors in your code:

func (s *Service) Process(ctx context.Context, input *Input) error {
    if err != nil {
        return fmt.Errorf("processing: %w", err)
    }

    result := make(map[string]interface{})
    return nil
}

You can run this with go build and go test.`,
		ProjectLang: "python",
	}

	violations := checker.Check(ctx, input)

	if len(violations) == 0 {
		t.Fatal("expected violations for Go code in Python project, got none")
	}

	found := false
	for _, v := range violations {
		if v.Type == ViolationLanguageConfusion {
			found = true
			if v.Code != "LANGUAGE_CONFUSION_GO" && v.Code != "LANGUAGE_CONFUSION_FRAMEWORK" {
				t.Errorf("expected code 'LANGUAGE_CONFUSION_GO' or 'LANGUAGE_CONFUSION_FRAMEWORK', got '%s'", v.Code)
			}
		}
	}

	if !found {
		t.Error("expected language_confusion violation for Go patterns")
	}
}

func TestLanguageChecker_Check_CorrectLanguage(t *testing.T) {
	checker := NewLanguageChecker(nil)
	ctx := context.Background()

	// Response contains Go patterns in a Go project (correct)
	input := &CheckInput{
		Response: `Here's the implementation:

func (s *Service) Process(ctx context.Context, input *Input) error {
    if err != nil {
        return fmt.Errorf("processing: %w", err)
    }
    return nil
}

Run go test to verify.`,
		ProjectLang: "go",
	}

	violations := checker.Check(ctx, input)

	for _, v := range violations {
		if v.Type == ViolationLanguageConfusion {
			t.Errorf("unexpected language confusion violation: %s", v.Message)
		}
	}
}

func TestLanguageChecker_Check_JavaScriptInGoProject(t *testing.T) {
	checker := NewLanguageChecker(nil)
	ctx := context.Background()

	input := &CheckInput{
		Response: `You should use Express.js for this:

const express = require('express');
const app = express();

app.get('/api/users', async (req, res) => {
    const users = await fetchUsers();
    res.json(users);
});

Run npm install express and then node server.js.`,
		ProjectLang: "go",
	}

	violations := checker.Check(ctx, input)

	if len(violations) == 0 {
		t.Fatal("expected violations for JavaScript code in Go project, got none")
	}

	found := false
	for _, v := range violations {
		if v.Code == "LANGUAGE_CONFUSION_JAVASCRIPT" || v.Code == "LANGUAGE_CONFUSION_FRAMEWORK" {
			found = true
		}
	}

	if !found {
		t.Error("expected language_confusion violation for JavaScript patterns")
	}
}

func TestLanguageChecker_Check_ContextCancellation(t *testing.T) {
	checker := NewLanguageChecker(nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	input := &CheckInput{
		Response: `from flask import Flask
def hello():
    pass`,
		ProjectLang: "go",
	}

	// Should return early due to context cancellation
	violations := checker.Check(ctx, input)

	// May have partial results, but should not panic
	_ = violations
}

func TestLanguageChecker_Check_BelowThreshold(t *testing.T) {
	checker := NewLanguageChecker(&LanguageCheckerConfig{
		WeightThreshold:  1.5,
		EnablePython:     true,
		EnableJavaScript: true,
		EnableGo:         true,
	})
	ctx := context.Background()

	// Single weak indicator should not trigger violation
	// Using a pattern that won't match framework blocklists
	input := &CheckInput{
		Response:    `The variable assignment uses equals sign.`,
		ProjectLang: "go",
	}

	violations := checker.Check(ctx, input)

	for _, v := range violations {
		if v.Type == ViolationLanguageConfusion && v.Code == "LANGUAGE_CONFUSION_PYTHON" {
			t.Errorf("unexpected violation for weak indicator: %s", v.Message)
		}
	}
}

func TestLanguageChecker_Check_ResponseTruncation(t *testing.T) {
	checker := NewLanguageChecker(nil)
	ctx := context.Background()

	// Create a very long response
	longPrefix := make([]byte, 15000)
	for i := range longPrefix {
		longPrefix[i] = 'x'
	}

	input := &CheckInput{
		Response: string(longPrefix) + `
from flask import Flask
@app.route('/api')
def handle():
    pass`,
		ProjectLang: "go",
	}

	// Should not find Python patterns after truncation point
	violations := checker.Check(ctx, input)

	for _, v := range violations {
		if v.Type == ViolationLanguageConfusion && v.Code == "LANGUAGE_CONFUSION_PYTHON" {
			t.Error("should not detect Python patterns beyond truncation limit")
		}
	}
}

func TestLanguageChecker_Check_EmptyResponse(t *testing.T) {
	checker := NewLanguageChecker(nil)
	ctx := context.Background()

	input := &CheckInput{
		Response:    "",
		ProjectLang: "go",
	}

	violations := checker.Check(ctx, input)

	if len(violations) != 0 {
		t.Errorf("expected no violations for empty response, got %d", len(violations))
	}
}

func TestLanguageChecker_Check_UnknownProjectLanguage(t *testing.T) {
	checker := NewLanguageChecker(nil)
	ctx := context.Background()

	input := &CheckInput{
		Response: `from flask import Flask
def hello():
    pass`,
		ProjectLang: "rust", // Not in our patterns
	}

	violations := checker.Check(ctx, input)

	// Should still detect Python patterns as wrong for a Rust project
	found := false
	for _, v := range violations {
		if v.Code == "LANGUAGE_CONFUSION_PYTHON" {
			found = true
		}
	}

	if !found {
		t.Error("expected to detect Python patterns in non-Python project")
	}
}

// TestLanguageChecker_Check_HybridRepo verifies that Python patterns are NOT flagged
// when Python files are actually in the EvidenceIndex (hybrid repo scenario).
//
// This is critical for repos like "Go backend + Python scripts" where discussing
// the Python scripts should not be flagged as hallucination.
func TestLanguageChecker_Check_HybridRepo(t *testing.T) {
	t.Run("python_in_evidence_not_flagged", func(t *testing.T) {
		checker := NewLanguageChecker(nil)
		ctx := context.Background()

		// Simulate a Go project with a Python script in context
		evidence := NewEvidenceIndex()
		evidence.Languages["python"] = true // Python file was shown
		evidence.Files["scripts/setup.py"] = true

		input := &CheckInput{
			Response: `The project includes a Python setup script.
The scripts/setup.py file uses:
from setuptools import setup
def main():
    setup(name="myproject")

This handles package installation.`,
			ProjectLang:   "go", // Main project is Go
			EvidenceIndex: evidence,
		}

		violations := checker.Check(ctx, input)

		// Should NOT flag Python pattern violations since Python is in EvidenceIndex
		for _, v := range violations {
			if v.Code == "LANGUAGE_CONFUSION_PYTHON" {
				t.Errorf("should not flag Python when Python files are in evidence: %s", v.Message)
			}
		}
	})

	t.Run("python_not_in_evidence_is_flagged", func(t *testing.T) {
		checker := NewLanguageChecker(nil)
		ctx := context.Background()

		// Go project with NO Python in evidence
		evidence := NewEvidenceIndex()
		evidence.Languages["go"] = true
		evidence.Files["main.go"] = true

		input := &CheckInput{
			Response: `The project is built with Flask.
from flask import Flask
def create_app():
    app = Flask(__name__)
    return app

This handles HTTP requests.`,
			ProjectLang:   "go",
			EvidenceIndex: evidence,
		}

		violations := checker.Check(ctx, input)

		// SHOULD flag Python patterns - no Python files in evidence
		found := false
		for _, v := range violations {
			if v.Code == "LANGUAGE_CONFUSION_PYTHON" || v.Code == "LANGUAGE_CONFUSION_FRAMEWORK" {
				found = true
			}
		}

		if !found {
			t.Error("expected to detect Python hallucination when no Python files in evidence")
		}
	})

	t.Run("javascript_in_evidence_not_flagged", func(t *testing.T) {
		checker := NewLanguageChecker(nil)
		ctx := context.Background()

		// Go project with a JavaScript frontend in context
		evidence := NewEvidenceIndex()
		evidence.Languages["javascript"] = true
		evidence.Languages["go"] = true
		evidence.Files["frontend/index.js"] = true
		evidence.Files["main.go"] = true

		input := &CheckInput{
			Response: `The Go backend serves a JavaScript frontend.
The frontend/index.js file:
import React from 'react';
export default function App() {
    return <div>Hello</div>;
}

The Go backend is in main.go.`,
			ProjectLang:   "go",
			EvidenceIndex: evidence,
		}

		violations := checker.Check(ctx, input)

		// Should NOT flag JavaScript pattern violations
		for _, v := range violations {
			if v.Code == "LANGUAGE_CONFUSION_JAVASCRIPT" {
				t.Errorf("should not flag JavaScript when JS files are in evidence: %s", v.Message)
			}
		}
	})

	t.Run("nil_evidence_falls_back_to_project_lang", func(t *testing.T) {
		checker := NewLanguageChecker(nil)
		ctx := context.Background()

		input := &CheckInput{
			Response: `from flask import Flask
def hello():
    pass`,
			ProjectLang:   "go",
			EvidenceIndex: nil, // No evidence index
		}

		violations := checker.Check(ctx, input)

		// Should still flag Python when no EvidenceIndex is available
		found := false
		for _, v := range violations {
			if v.Code == "LANGUAGE_CONFUSION_PYTHON" || v.Code == "LANGUAGE_CONFUSION_FRAMEWORK" {
				found = true
			}
		}

		if !found {
			t.Error("expected to flag Python when EvidenceIndex is nil")
		}
	})
}

func TestDetectProjectLanguage(t *testing.T) {
	tests := []struct {
		name     string
		counts   map[string]int
		expected string
	}{
		{
			name:     "empty counts",
			counts:   map[string]int{},
			expected: "",
		},
		{
			name: "go project",
			counts: map[string]int{
				".go": 50,
				".md": 5,
			},
			expected: "go",
		},
		{
			name: "python project",
			counts: map[string]int{
				".py":   30,
				".yaml": 10,
				".json": 5,
			},
			expected: "python",
		},
		{
			name: "typescript project",
			counts: map[string]int{
				".ts":   40,
				".tsx":  20,
				".json": 10,
			},
			expected: "typescript",
		},
		{
			name: "mixed but go dominant",
			counts: map[string]int{
				".go": 100,
				".py": 5,
				".js": 10,
			},
			expected: "go",
		},
		{
			name: "only config files",
			counts: map[string]int{
				".md":   10,
				".yaml": 5,
				".json": 20,
			},
			expected: "",
		},
		{
			name:     "nil counts",
			counts:   nil,
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := DetectProjectLanguage(tt.counts)
			if result != tt.expected {
				t.Errorf("DetectProjectLanguage() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestGetBlockedFrameworks(t *testing.T) {
	t.Run("go blocklist", func(t *testing.T) {
		blocklist := GetBlockedFrameworks("go")

		// Should contain Python frameworks
		if !blocklist["flask"] {
			t.Error("Go blocklist should contain 'flask'")
		}
		if !blocklist["django"] {
			t.Error("Go blocklist should contain 'django'")
		}

		// Should contain JavaScript frameworks
		if !blocklist["express"] {
			t.Error("Go blocklist should contain 'express'")
		}
	})

	t.Run("python blocklist", func(t *testing.T) {
		blocklist := GetBlockedFrameworks("python")

		// Should contain Go frameworks
		if !blocklist["gin"] {
			t.Error("Python blocklist should contain 'gin'")
		}
		if !blocklist["echo"] {
			t.Error("Python blocklist should contain 'echo'")
		}
	})

	t.Run("unknown language returns empty", func(t *testing.T) {
		blocklist := GetBlockedFrameworks("cobol")
		if len(blocklist) != 0 {
			t.Errorf("Unknown language should return empty blocklist, got %d items", len(blocklist))
		}
	})

	t.Run("case insensitive", func(t *testing.T) {
		blocklist := GetBlockedFrameworks("GO")
		if !blocklist["flask"] {
			t.Error("Should handle uppercase language name")
		}
	})
}

func TestGetLanguageSuggestion(t *testing.T) {
	tests := []struct {
		name         string
		projectLang  string
		wrongTerm    string
		wantNonEmpty bool
	}{
		{
			name:         "flask in go project",
			projectLang:  "go",
			wrongTerm:    "flask",
			wantNonEmpty: true,
		},
		{
			name:         "gin in python project",
			projectLang:  "python",
			wrongTerm:    "gin",
			wantNonEmpty: true,
		},
		{
			name:         "unknown term",
			projectLang:  "go",
			wrongTerm:    "completely_unknown_xyz",
			wantNonEmpty: false,
		},
		{
			name:         "partial match",
			projectLang:  "go",
			wrongTerm:    "flask route decorator",
			wantNonEmpty: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			suggestion := GetLanguageSuggestion(tt.projectLang, tt.wrongTerm)
			if tt.wantNonEmpty && suggestion == "" {
				t.Errorf("expected non-empty suggestion for %q in %s project", tt.wrongTerm, tt.projectLang)
			}
			if !tt.wantNonEmpty && suggestion != "" {
				t.Errorf("expected empty suggestion, got %q", suggestion)
			}
		})
	}
}

func TestLanguageChecker_FrameworkBlocklist(t *testing.T) {
	checker := NewLanguageChecker(nil)
	ctx := context.Background()

	t.Run("flask mentioned in go project", func(t *testing.T) {
		input := &CheckInput{
			Response:    "You can use Flask to handle HTTP requests.",
			ProjectLang: "go",
		}

		violations := checker.Check(ctx, input)

		found := false
		for _, v := range violations {
			if v.Code == "LANGUAGE_CONFUSION_FRAMEWORK" && strings.Contains(v.Evidence, "flask") {
				found = true
				// Should have a suggestion
				if v.Suggestion == "" {
					t.Error("framework violation should have a suggestion")
				}
			}
		}

		if !found {
			t.Error("expected framework blocklist violation for Flask in Go project")
		}
	})

	t.Run("gin mentioned in python project", func(t *testing.T) {
		input := &CheckInput{
			Response:    "Use Gin framework for your API endpoints.",
			ProjectLang: "python",
		}

		violations := checker.Check(ctx, input)

		found := false
		for _, v := range violations {
			if v.Code == "LANGUAGE_CONFUSION_FRAMEWORK" && strings.Contains(v.Evidence, "gin") {
				found = true
			}
		}

		if !found {
			t.Error("expected framework blocklist violation for Gin in Python project")
		}
	})

	t.Run("correct framework not flagged", func(t *testing.T) {
		input := &CheckInput{
			Response:    "Use the Gin framework for your API endpoints.",
			ProjectLang: "go",
		}

		violations := checker.Check(ctx, input)

		// Should NOT flag Gin in a Go project
		for _, v := range violations {
			if v.Code == "LANGUAGE_CONFUSION_FRAMEWORK" && strings.Contains(v.Evidence, "gin") {
				t.Errorf("should not flag Gin in Go project: %s", v.Message)
			}
		}
	})
}

func TestLanguageChecker_ViolationSuggestions(t *testing.T) {
	checker := NewLanguageChecker(nil)
	ctx := context.Background()

	input := &CheckInput{
		Response: `To build this feature, use Flask:

from flask import Flask
app = Flask(__name__)

@app.route('/api')
def handle():
    return "ok"`,
		ProjectLang: "go",
	}

	violations := checker.Check(ctx, input)

	if len(violations) == 0 {
		t.Fatal("expected violations")
	}

	// Check that at least one violation has a helpful suggestion
	hasSuggestion := false
	for _, v := range violations {
		if v.Suggestion != "" {
			hasSuggestion = true
			// Should mention Go alternatives
			if !strings.Contains(v.Suggestion, "go") &&
				!strings.Contains(v.Suggestion, "Go") &&
				!strings.Contains(v.Suggestion, "net/http") &&
				!strings.Contains(v.Suggestion, "Gin") &&
				!strings.Contains(v.Suggestion, "Echo") {
				t.Logf("suggestion may not be helpful: %s", v.Suggestion)
			}
		}
	}

	if !hasSuggestion {
		t.Error("expected at least one violation to have a suggestion")
	}
}

// TestLanguageChecker_Test12Scenario reproduces the exact scenario from Test 12
// that motivated this checker enhancement.
func TestLanguageChecker_Test12Scenario(t *testing.T) {
	checker := NewLanguageChecker(nil)
	ctx := context.Background()

	// Test 12: Flask/Python patterns described in a Go codebase
	input := &CheckInput{
		Response: `The data flows through the Flask request pipeline:

1. Request comes in to the Flask app
2. The @app.route decorator routes it to the handler
3. The handler calls the service layer
4. Results are returned via jsonify()

You can trace this in app/routes.py which defines the Flask blueprints.`,
		ProjectLang: "go",
	}

	violations := checker.Check(ctx, input)

	if len(violations) == 0 {
		t.Fatal("Test 12 scenario should have been caught - Flask in Go project")
	}

	// Should detect Flask framework in blocklist
	foundFlask := false
	for _, v := range violations {
		if v.Type == ViolationLanguageConfusion {
			if strings.Contains(strings.ToLower(v.Evidence), "flask") ||
				strings.Contains(strings.ToLower(v.Message), "flask") {
				foundFlask = true
				// Verify it has helpful correction info
				if v.Suggestion == "" {
					t.Error("Flask violation should have a suggestion")
				}
			}
		}
	}

	if !foundFlask {
		t.Error("Test 12: Should specifically detect Flask framework confusion")
	}
}

func TestLanguageChecker_ViolationPriority(t *testing.T) {
	checker := NewLanguageChecker(nil)
	ctx := context.Background()

	input := &CheckInput{
		Response:    "Use Flask to handle the requests.",
		ProjectLang: "go",
	}

	violations := checker.Check(ctx, input)

	if len(violations) == 0 {
		t.Fatal("expected violation")
	}

	// Verify the violation has correct priority
	for _, v := range violations {
		if v.Type == ViolationLanguageConfusion {
			priority := v.Priority()
			if priority != PriorityLanguageConfusion {
				t.Errorf("violation priority = %v, want %v", priority, PriorityLanguageConfusion)
			}
		}
	}
}
