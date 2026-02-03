// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package mcts

import (
	"context"
	"regexp"
	"testing"
)

func TestNewBasicSecurityScanner(t *testing.T) {
	scanner := NewBasicSecurityScanner()

	if scanner == nil {
		t.Fatal("NewBasicSecurityScanner returned nil")
	}
	if len(scanner.Patterns()) == 0 {
		t.Error("Expected default patterns")
	}
}

func TestBasicSecurityScanner_ScanCode_Clean(t *testing.T) {
	scanner := NewBasicSecurityScanner()

	code := `package main

func main() {
	fmt.Println("Hello, World!")
}
`

	result, err := scanner.ScanCode(context.Background(), code)
	if err != nil {
		t.Fatalf("ScanCode error: %v", err)
	}

	if result.Score != 1.0 {
		t.Errorf("Score = %f, want 1.0 for clean code", result.Score)
	}
	if len(result.Issues) != 0 {
		t.Errorf("Issues count = %d, want 0", len(result.Issues))
	}
}

func TestBasicSecurityScanner_ScanCode_CommandInjection(t *testing.T) {
	scanner := NewBasicSecurityScanner()

	code := `exec.Command("bash", "-c", userInput + " more")`

	result, err := scanner.ScanCode(context.Background(), code)
	if err != nil {
		t.Fatalf("ScanCode error: %v", err)
	}

	if result.Score >= 1.0 {
		t.Errorf("Score = %f, should be less than 1.0 for command injection", result.Score)
	}

	found := false
	for _, issue := range result.Issues {
		if issue.Pattern == "command_injection" {
			found = true
			if issue.Severity != "critical" {
				t.Errorf("Severity = %s, want critical", issue.Severity)
			}
			break
		}
	}
	if !found {
		t.Error("Expected command_injection issue")
	}
}

func TestBasicSecurityScanner_ScanCode_SQLInjection(t *testing.T) {
	scanner := NewBasicSecurityScanner()

	// Pattern: (SELECT|INSERT|UPDATE|DELETE).*\+.*['"]\s*\+
	// Matches: SELECT...+...'+
	code := `query := "SELECT * FROM users" + "WHERE id = '" + userID`

	result, err := scanner.ScanCode(context.Background(), code)
	if err != nil {
		t.Fatalf("ScanCode error: %v", err)
	}

	found := false
	for _, issue := range result.Issues {
		if issue.Pattern == "sql_injection" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected sql_injection issue")
	}
}

func TestBasicSecurityScanner_ScanCode_HardcodedPassword(t *testing.T) {
	scanner := NewBasicSecurityScanner()

	// Pattern requires: password/pwd/etc = "at_least_8_chars"
	code := `password = "supersecret123"`

	result, err := scanner.ScanCode(context.Background(), code)
	if err != nil {
		t.Fatalf("ScanCode error: %v", err)
	}

	found := false
	for _, issue := range result.Issues {
		if issue.Pattern == "hardcoded_password" {
			found = true
			if issue.Severity != "high" {
				t.Errorf("Severity = %s, want high", issue.Severity)
			}
			break
		}
	}
	if !found {
		t.Error("Expected hardcoded_password issue")
	}
}

func TestBasicSecurityScanner_ScanCode_PathTraversal(t *testing.T) {
	scanner := NewBasicSecurityScanner()

	code := `file := "../../../etc/passwd"`

	result, err := scanner.ScanCode(context.Background(), code)
	if err != nil {
		t.Fatalf("ScanCode error: %v", err)
	}

	found := false
	for _, issue := range result.Issues {
		if issue.Pattern == "path_traversal" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected path_traversal issue")
	}
}

func TestBasicSecurityScanner_ScanCode_WeakCrypto(t *testing.T) {
	scanner := NewBasicSecurityScanner()

	// Pattern matches md5( or sha1(
	code := `hash := md5(data)`

	result, err := scanner.ScanCode(context.Background(), code)
	if err != nil {
		t.Fatalf("ScanCode error: %v", err)
	}

	found := false
	for _, issue := range result.Issues {
		if issue.Pattern == "weak_crypto" {
			found = true
			if issue.Severity != "medium" {
				t.Errorf("Severity = %s, want medium", issue.Severity)
			}
			break
		}
	}
	if !found {
		t.Error("Expected weak_crypto issue")
	}
}

func TestBasicSecurityScanner_ScanCode_EvalUsage(t *testing.T) {
	scanner := NewBasicSecurityScanner()

	code := `result = eval(userCode)`

	result, err := scanner.ScanCode(context.Background(), code)
	if err != nil {
		t.Fatalf("ScanCode error: %v", err)
	}

	found := false
	for _, issue := range result.Issues {
		if issue.Pattern == "eval_usage" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected eval_usage issue")
	}
}

func TestBasicSecurityScanner_ScanCode_SSLDisabled(t *testing.T) {
	scanner := NewBasicSecurityScanner()

	code := `client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}`

	result, err := scanner.ScanCode(context.Background(), code)
	if err != nil {
		t.Fatalf("ScanCode error: %v", err)
	}

	found := false
	for _, issue := range result.Issues {
		if issue.Pattern == "ssl_disabled" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected ssl_disabled issue")
	}
}

func TestBasicSecurityScanner_ScanCode_MultipleIssues(t *testing.T) {
	scanner := NewBasicSecurityScanner()

	code := `
password = "supersecret123"
hash := md5(password)
result = eval(userInput)
`

	result, err := scanner.ScanCode(context.Background(), code)
	if err != nil {
		t.Fatalf("ScanCode error: %v", err)
	}

	if len(result.Issues) < 3 {
		t.Errorf("Issues count = %d, want at least 3", len(result.Issues))
	}
	if result.Score >= 0.5 {
		t.Errorf("Score = %f, should be low with multiple issues", result.Score)
	}
}

func TestBasicSecurityScanner_ScanCode_ScoreClamps(t *testing.T) {
	scanner := NewBasicSecurityScanner()

	// Code with many critical issues
	code := `
exec.Command("bash", "-c", input + " x")
exec.Command("bash", "-c", input2 + " y")
query := "SELECT * FROM users WHERE id = '" + id1 + "'"
query2 := "DELETE FROM users WHERE id = '" + id2 + "'"
`

	result, err := scanner.ScanCode(context.Background(), code)
	if err != nil {
		t.Fatalf("ScanCode error: %v", err)
	}

	if result.Score < 0 {
		t.Errorf("Score = %f, should not be negative", result.Score)
	}
	if result.Score > 1 {
		t.Errorf("Score = %f, should not be greater than 1", result.Score)
	}
}

func TestBasicSecurityScanner_ScanCodeWithLanguage_Go(t *testing.T) {
	scanner := NewBasicSecurityScanner()

	code := `gob.NewDecoder(r).Decode(&data)`

	result, err := scanner.ScanCodeWithLanguage(context.Background(), code, "go")
	if err != nil {
		t.Fatalf("ScanCodeWithLanguage error: %v", err)
	}

	found := false
	for _, issue := range result.Issues {
		if issue.Pattern == "insecure_deserialize_go" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected insecure_deserialize_go issue")
	}
}

func TestBasicSecurityScanner_ScanCodeWithLanguage_SkipsOtherLanguages(t *testing.T) {
	scanner := NewBasicSecurityScanner()

	// Go-specific pattern in Python code
	code := `gob.NewDecoder(r).Decode(&data)`

	result, err := scanner.ScanCodeWithLanguage(context.Background(), code, "python")
	if err != nil {
		t.Fatalf("ScanCodeWithLanguage error: %v", err)
	}

	// Should not match Go-specific pattern for Python
	for _, issue := range result.Issues {
		if issue.Pattern == "insecure_deserialize_go" {
			t.Error("Should not match Go-specific pattern for Python code")
		}
	}
}

func TestBasicSecurityScanner_ScanCodeWithLanguage_Python(t *testing.T) {
	scanner := NewBasicSecurityScanner()

	code := `data = pickle.load(file)`

	result, err := scanner.ScanCodeWithLanguage(context.Background(), code, "python")
	if err != nil {
		t.Fatalf("ScanCodeWithLanguage error: %v", err)
	}

	found := false
	for _, issue := range result.Issues {
		if issue.Pattern == "insecure_deserialize_python" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected insecure_deserialize_python issue")
	}
}

func TestBasicSecurityScanner_ContextCancellation(t *testing.T) {
	scanner := NewBasicSecurityScanner()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := scanner.ScanCode(ctx, "some code")
	if err == nil {
		t.Error("Expected error from cancelled context")
	}
}

func TestBasicSecurityScanner_CustomPatterns(t *testing.T) {
	patterns := []SecurityPattern{
		{
			Name:        "custom_issue",
			Pattern:     regexp.MustCompile(`CUSTOM_BAD_PATTERN`),
			Severity:    "high",
			Description: "Custom security issue",
		},
	}

	scanner := NewBasicSecurityScannerWithPatterns(patterns)

	code := `something CUSTOM_BAD_PATTERN here`

	result, err := scanner.ScanCode(context.Background(), code)
	if err != nil {
		t.Fatalf("ScanCode error: %v", err)
	}

	if len(result.Issues) != 1 {
		t.Fatalf("Issues count = %d, want 1", len(result.Issues))
	}
	if result.Issues[0].Pattern != "custom_issue" {
		t.Errorf("Pattern = %s, want custom_issue", result.Issues[0].Pattern)
	}
}

func TestBasicSecurityScanner_AddPattern(t *testing.T) {
	scanner := NewBasicSecurityScanner()
	initialCount := len(scanner.Patterns())

	scanner.AddPattern(SecurityPattern{
		Name:        "new_pattern",
		Pattern:     regexp.MustCompile(`NEW_ISSUE`),
		Severity:    "low",
		Description: "New issue",
	})

	if len(scanner.Patterns()) != initialCount+1 {
		t.Errorf("Pattern count = %d, want %d", len(scanner.Patterns()), initialCount+1)
	}

	// Verify it works
	result, _ := scanner.ScanCode(context.Background(), "NEW_ISSUE here")
	found := false
	for _, issue := range result.Issues {
		if issue.Pattern == "new_pattern" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected new_pattern issue")
	}
}

func TestTruncateMatch(t *testing.T) {
	tests := []struct {
		input  string
		maxLen int
		want   string
	}{
		{"short", 10, "short"},
		{"exactly10!", 10, "exactly10!"},
		{"this is a longer string", 10, "this is a ..."},
		{"  trimmed  ", 20, "trimmed"},
	}

	for _, tt := range tests {
		got := truncateMatch(tt.input, tt.maxLen)
		if got != tt.want {
			t.Errorf("truncateMatch(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.want)
		}
	}
}

func TestContainsLanguage(t *testing.T) {
	tests := []struct {
		languages []string
		target    string
		want      bool
	}{
		{[]string{"go", "python"}, "go", true},
		{[]string{"go", "python"}, "GO", true},
		{[]string{"go", "python"}, "java", false},
		{[]string{}, "go", false},
	}

	for _, tt := range tests {
		got := containsLanguage(tt.languages, tt.target)
		if got != tt.want {
			t.Errorf("containsLanguage(%v, %q) = %v, want %v", tt.languages, tt.target, got, tt.want)
		}
	}
}
