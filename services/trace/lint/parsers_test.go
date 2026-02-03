// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package lint

import (
	"testing"
)

func TestParseGolangCIOutput(t *testing.T) {
	t.Run("valid output with issues", func(t *testing.T) {
		// Real golangci-lint JSON output format
		output := []byte(`{
			"Issues": [
				{
					"FromLinter": "errcheck",
					"Text": "Error return value of 'file.Close' is not checked",
					"Severity": "warning",
					"SourceLines": ["file.Close()"],
					"Pos": {
						"Filename": "main.go",
						"Line": 42,
						"Column": 2
					}
				},
				{
					"FromLinter": "staticcheck",
					"Text": "this value of 'err' is never used",
					"Pos": {
						"Filename": "main.go",
						"Line": 50,
						"Column": 5
					},
					"LineRange": {
						"From": 50,
						"To": 52
					}
				}
			]
		}`)

		issues, err := parseGolangCIOutput(output)
		if err != nil {
			t.Fatalf("parseGolangCIOutput: %v", err)
		}

		if len(issues) != 2 {
			t.Fatalf("Expected 2 issues, got %d", len(issues))
		}

		// Check first issue
		if issues[0].Rule != "errcheck" {
			t.Errorf("Issue 0 Rule = %q, want errcheck", issues[0].Rule)
		}
		if issues[0].Line != 42 {
			t.Errorf("Issue 0 Line = %d, want 42", issues[0].Line)
		}
		if issues[0].File != "main.go" {
			t.Errorf("Issue 0 File = %q, want main.go", issues[0].File)
		}

		// Check second issue has line range
		if issues[1].EndLine != 52 {
			t.Errorf("Issue 1 EndLine = %d, want 52", issues[1].EndLine)
		}
	})

	t.Run("empty issues", func(t *testing.T) {
		output := []byte(`{"Issues": []}`)
		issues, err := parseGolangCIOutput(output)
		if err != nil {
			t.Fatalf("parseGolangCIOutput: %v", err)
		}
		if len(issues) != 0 {
			t.Errorf("Expected 0 issues, got %d", len(issues))
		}
	})

	t.Run("null issues", func(t *testing.T) {
		output := []byte(`{"Issues": null}`)
		issues, err := parseGolangCIOutput(output)
		if err != nil {
			t.Fatalf("parseGolangCIOutput: %v", err)
		}
		if len(issues) != 0 {
			t.Errorf("Expected 0 issues, got %d", len(issues))
		}
	})

	t.Run("with replacement", func(t *testing.T) {
		output := []byte(`{
			"Issues": [
				{
					"FromLinter": "gofmt",
					"Text": "File is not gofmted",
					"Pos": {"Filename": "main.go", "Line": 1, "Column": 1},
					"Replacement": {
						"NeedOnlyDelete": false,
						"NewLines": ["package main", "", "import \"fmt\""]
					}
				}
			]
		}`)

		issues, err := parseGolangCIOutput(output)
		if err != nil {
			t.Fatalf("parseGolangCIOutput: %v", err)
		}

		if !issues[0].CanAutoFix {
			t.Error("Issue should be auto-fixable")
		}
		if issues[0].Replacement == "" {
			t.Error("Replacement should be set")
		}
	})

	t.Run("invalid JSON", func(t *testing.T) {
		output := []byte(`not json`)
		_, err := parseGolangCIOutput(output)
		if err == nil {
			t.Error("Expected error for invalid JSON")
		}
	})
}

func TestParseRuffOutput(t *testing.T) {
	t.Run("valid output with issues", func(t *testing.T) {
		// Real Ruff JSON output format
		output := []byte(`[
			{
				"code": "F401",
				"end_location": {"column": 11, "row": 1},
				"filename": "test.py",
				"fix": {
					"applicability": "safe",
					"edits": [{"content": "", "end_location": {"column": 1, "row": 2}, "location": {"column": 1, "row": 1}}],
					"message": "Remove unused import: os"
				},
				"location": {"column": 8, "row": 1},
				"message": "'os' imported but unused",
				"noqa_row": 1,
				"url": "https://docs.astral.sh/ruff/rules/unused-import"
			},
			{
				"code": "E501",
				"end_location": {"column": 120, "row": 10},
				"filename": "test.py",
				"fix": null,
				"location": {"column": 80, "row": 10},
				"message": "Line too long (119 > 79 characters)"
			}
		]`)

		issues, err := parseRuffOutput(output)
		if err != nil {
			t.Fatalf("parseRuffOutput: %v", err)
		}

		if len(issues) != 2 {
			t.Fatalf("Expected 2 issues, got %d", len(issues))
		}

		// Check first issue
		if issues[0].Rule != "F401" {
			t.Errorf("Issue 0 Rule = %q, want F401", issues[0].Rule)
		}
		if issues[0].CanAutoFix != true {
			t.Error("F401 should be auto-fixable")
		}
		if issues[0].RuleURL == "" {
			t.Error("Rule URL should be set")
		}

		// Check second issue
		if issues[1].Rule != "E501" {
			t.Errorf("Issue 1 Rule = %q, want E501", issues[1].Rule)
		}
		if issues[1].CanAutoFix {
			t.Error("E501 should not be auto-fixable (fix is null)")
		}
	})

	t.Run("empty array", func(t *testing.T) {
		output := []byte(`[]`)
		issues, err := parseRuffOutput(output)
		if err != nil {
			t.Fatalf("parseRuffOutput: %v", err)
		}
		if len(issues) != 0 {
			t.Errorf("Expected 0 issues, got %d", len(issues))
		}
	})

	t.Run("invalid JSON", func(t *testing.T) {
		output := []byte(`{invalid}`)
		_, err := parseRuffOutput(output)
		if err == nil {
			t.Error("Expected error for invalid JSON")
		}
	})
}

func TestParseESLintOutput(t *testing.T) {
	t.Run("valid output with issues", func(t *testing.T) {
		// Real ESLint JSON output format
		output := []byte(`[
			{
				"filePath": "/path/to/file.ts",
				"messages": [
					{
						"ruleId": "no-unused-vars",
						"severity": 2,
						"message": "'foo' is defined but never used.",
						"line": 5,
						"column": 7,
						"endLine": 5,
						"endColumn": 10
					},
					{
						"ruleId": "eqeqeq",
						"severity": 1,
						"message": "Expected '===' and instead saw '=='.",
						"line": 10,
						"column": 5,
						"fix": {
							"range": [100, 102],
							"text": "==="
						}
					}
				],
				"errorCount": 1,
				"warningCount": 1,
				"fixableErrorCount": 0,
				"fixableWarningCount": 1
			}
		]`)

		issues, err := parseESLintOutput(output)
		if err != nil {
			t.Fatalf("parseESLintOutput: %v", err)
		}

		if len(issues) != 2 {
			t.Fatalf("Expected 2 issues, got %d", len(issues))
		}

		// Check first issue (severity 2 = error)
		if issues[0].Severity != SeverityError {
			t.Errorf("Issue 0 Severity = %v, want Error", issues[0].Severity)
		}
		if issues[0].Rule != "no-unused-vars" {
			t.Errorf("Issue 0 Rule = %q, want no-unused-vars", issues[0].Rule)
		}

		// Check second issue (has fix)
		if issues[1].Severity != SeverityWarning {
			t.Errorf("Issue 1 Severity = %v, want Warning", issues[1].Severity)
		}
		if !issues[1].CanAutoFix {
			t.Error("Issue 1 should be auto-fixable")
		}
	})

	t.Run("multiple files", func(t *testing.T) {
		output := []byte(`[
			{
				"filePath": "file1.ts",
				"messages": [{"ruleId": "rule1", "severity": 1, "message": "msg1", "line": 1, "column": 1}]
			},
			{
				"filePath": "file2.ts",
				"messages": [{"ruleId": "rule2", "severity": 2, "message": "msg2", "line": 2, "column": 2}]
			}
		]`)

		issues, err := parseESLintOutput(output)
		if err != nil {
			t.Fatalf("parseESLintOutput: %v", err)
		}

		if len(issues) != 2 {
			t.Fatalf("Expected 2 issues, got %d", len(issues))
		}

		if issues[0].File != "file1.ts" {
			t.Errorf("Issue 0 File = %q, want file1.ts", issues[0].File)
		}
		if issues[1].File != "file2.ts" {
			t.Errorf("Issue 1 File = %q, want file2.ts", issues[1].File)
		}
	})

	t.Run("empty messages", func(t *testing.T) {
		output := []byte(`[{"filePath": "clean.ts", "messages": []}]`)
		issues, err := parseESLintOutput(output)
		if err != nil {
			t.Fatalf("parseESLintOutput: %v", err)
		}
		if len(issues) != 0 {
			t.Errorf("Expected 0 issues, got %d", len(issues))
		}
	})

	t.Run("with suggestions", func(t *testing.T) {
		output := []byte(`[
			{
				"filePath": "test.ts",
				"messages": [
					{
						"ruleId": "prefer-const",
						"severity": 1,
						"message": "'x' is never reassigned.",
						"line": 1,
						"column": 5,
						"suggestions": [
							{
								"desc": "Use 'const' instead.",
								"fix": {"range": [0, 3], "text": "const"}
							}
						]
					}
				]
			}
		]`)

		issues, err := parseESLintOutput(output)
		if err != nil {
			t.Fatalf("parseESLintOutput: %v", err)
		}

		if issues[0].Suggestion != "Use 'const' instead." {
			t.Errorf("Suggestion = %q, want 'Use 'const' instead.'", issues[0].Suggestion)
		}
	})
}

func TestMapRuffSeverity(t *testing.T) {
	tests := []struct {
		code string
		want Severity
	}{
		{"E501", SeverityError},   // pycodestyle error
		{"F401", SeverityError},   // Pyflakes
		{"S101", SeverityError},   // Security
		{"W291", SeverityWarning}, // pycodestyle warning
		{"C901", SeverityWarning}, // Complexity
		{"I001", SeverityInfo},    // isort
		{"D100", SeverityInfo},    // pydocstyle
	}

	for _, tt := range tests {
		got := mapRuffSeverity(tt.code)
		if got != tt.want {
			t.Errorf("mapRuffSeverity(%q) = %v, want %v", tt.code, got, tt.want)
		}
	}
}

func TestMapESLintSeverity(t *testing.T) {
	tests := []struct {
		severity int
		want     Severity
	}{
		{2, SeverityError},
		{1, SeverityWarning},
		{0, SeverityInfo},
	}

	for _, tt := range tests {
		got := mapESLintSeverity(tt.severity)
		if got != tt.want {
			t.Errorf("mapESLintSeverity(%d) = %v, want %v", tt.severity, got, tt.want)
		}
	}
}
