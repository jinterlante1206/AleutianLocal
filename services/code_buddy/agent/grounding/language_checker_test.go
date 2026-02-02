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

	found := false
	for _, v := range violations {
		if v.Type == ViolationWrongLanguage && v.Severity == SeverityCritical {
			found = true
			if v.Code != "WRONG_LANGUAGE_PYTHON" {
				t.Errorf("expected code 'WRONG_LANGUAGE_PYTHON', got '%s'", v.Code)
			}
		}
	}

	if !found {
		t.Error("expected critical wrong_language violation for Python patterns")
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
		if v.Type == ViolationWrongLanguage {
			found = true
			if v.Code != "WRONG_LANGUAGE_GO" {
				t.Errorf("expected code 'WRONG_LANGUAGE_GO', got '%s'", v.Code)
			}
		}
	}

	if !found {
		t.Error("expected wrong_language violation for Go patterns")
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
		if v.Type == ViolationWrongLanguage {
			t.Errorf("unexpected wrong language violation: %s", v.Message)
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
		if v.Code == "WRONG_LANGUAGE_JAVASCRIPT" {
			found = true
		}
	}

	if !found {
		t.Error("expected wrong_language violation for JavaScript patterns")
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
	input := &CheckInput{
		Response:    `Check the .py file for the configuration.`,
		ProjectLang: "go",
	}

	violations := checker.Check(ctx, input)

	for _, v := range violations {
		if v.Type == ViolationWrongLanguage {
			t.Errorf("unexpected violation for single weak indicator: %s", v.Message)
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
		if v.Type == ViolationWrongLanguage && v.Code == "WRONG_LANGUAGE_PYTHON" {
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
		if v.Code == "WRONG_LANGUAGE_PYTHON" {
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

		// Should NOT flag Python patterns since Python is in EvidenceIndex
		for _, v := range violations {
			if v.Code == "WRONG_LANGUAGE_PYTHON" {
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
			if v.Code == "WRONG_LANGUAGE_PYTHON" {
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

		// Should NOT flag JavaScript patterns
		for _, v := range violations {
			if v.Code == "WRONG_LANGUAGE_JAVASCRIPT" {
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
			if v.Code == "WRONG_LANGUAGE_PYTHON" {
				found = true
			}
		}

		if !found {
			t.Error("expected to flag Python when EvidenceIndex is nil")
		}
	})
}
