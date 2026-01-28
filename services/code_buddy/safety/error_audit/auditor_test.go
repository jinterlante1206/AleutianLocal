// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package error_audit

import (
	"context"
	"testing"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/ast"
	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/graph"
	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/index"
	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/safety"
)

func createTestGraph() (*graph.Graph, *index.SymbolIndex) {
	g := graph.NewGraph("/test")
	idx := index.NewSymbolIndex()

	handler := &ast.Symbol{
		ID:        "handlers.HandleAuth",
		Name:      "HandleAuth",
		Kind:      ast.SymbolKindFunction,
		Language:  "go",
		FilePath:  "handlers/auth.go",
		Package:   "handlers",
		StartLine: 10,
	}

	g.AddNode(handler)
	idx.Add(handler)
	g.Freeze()

	return g, idx
}

// --- Pattern Tests ---

func TestIsSecurityFunction(t *testing.T) {
	tests := []struct {
		funcName string
		expected bool
	}{
		{"checkAuth", true},
		{"ValidateToken", true},
		{"AuthenticateUser", true},
		{"authorize", true},
		{"CheckPermission", true},
		{"sanitizeInput", true},
		{"HandleSearch", false},
		{"ProcessData", false},
		{"GetUser", false},
	}

	for _, tt := range tests {
		t.Run(tt.funcName, func(t *testing.T) {
			result := IsSecurityFunction(tt.funcName)
			if result != tt.expected {
				t.Errorf("IsSecurityFunction(%q) = %v, expected %v", tt.funcName, result, tt.expected)
			}
		})
	}
}

func TestFailOpenPattern_FindErrorChecks(t *testing.T) {
	pattern := DefaultFailOpenPatterns["go"]

	tests := []struct {
		name           string
		content        string
		expectCount    int
		expectFailOpen bool
	}{
		{
			name: "fail-open - no return",
			content: `
func handler() {
	if err != nil {
		log.Println(err)
	}
	// continues here
}`,
			expectCount:    1,
			expectFailOpen: true,
		},
		{
			name: "fail-closed - has return",
			content: `
func handler() error {
	if err != nil {
		return err
	}
	return nil
}`,
			expectCount:    1,
			expectFailOpen: false,
		},
		{
			name: "fail-closed - has panic",
			content: `
func handler() {
	if err != nil {
		panic(err)
	}
}`,
			expectCount:    1,
			expectFailOpen: false,
		},
		{
			name: "multiple error checks",
			content: `
func handler() {
	if err != nil {
		log.Println(err)
	}
	if err2 != nil {
		return err2
	}
}`,
			expectCount: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			blocks := pattern.FindErrorChecks(tt.content)

			if len(blocks) != tt.expectCount {
				t.Errorf("Expected %d blocks, got %d", tt.expectCount, len(blocks))
			}

			if tt.expectCount == 1 && blocks[0].IsFailOpen != tt.expectFailOpen {
				t.Errorf("Expected IsFailOpen=%v, got %v", tt.expectFailOpen, blocks[0].IsFailOpen)
			}
		})
	}
}

func TestFindSwallowedErrors(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		language string
		expect   int
	}{
		{
			name:     "Go - swallowed error",
			content:  `if err != nil { }`,
			language: "go",
			expect:   1,
		},
		{
			name:     "Go - handled error",
			content:  `if err != nil { return err }`,
			language: "go",
			expect:   0,
		},
		{
			name: "Python - swallowed exception",
			content: `try:
    foo()
except:
    pass
`,
			language: "python",
			expect:   1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matches := FindSwallowedErrors(tt.content, tt.language)
			if len(matches) != tt.expect {
				t.Errorf("Expected %d matches, got %d", tt.expect, len(matches))
			}
		})
	}
}

func TestInfoLeakPattern_Match(t *testing.T) {
	tests := []struct {
		name        string
		pattern     *InfoLeakPattern
		content     string
		expectMatch bool
	}{
		{
			name:        "stack trace detected",
			pattern:     DefaultInfoLeakPatterns[0], // stack_trace pattern
			content:     `debug.PrintStack()`,
			expectMatch: true,
		},
		{
			name:        "db error detected",
			pattern:     DefaultInfoLeakPatterns[3], // db_error pattern
			content:     `pq: connection refused - Write to client`,
			expectMatch: true,
		},
		{
			name:        "verbose error detected",
			pattern:     DefaultInfoLeakPatterns[5], // verbose_error pattern
			content:     `w.Write([]byte(err.Error()))`,
			expectMatch: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matches := tt.pattern.Match(tt.content)
			hasMatch := len(matches) > 0
			if hasMatch != tt.expectMatch {
				t.Errorf("Expected match=%v, got %v", tt.expectMatch, hasMatch)
			}
		})
	}
}

// --- Auditor Tests ---

func TestErrorAuditor_AuditErrorHandling_EmptyScope(t *testing.T) {
	g, idx := createTestGraph()
	auditor := NewErrorAuditor(g, idx)

	ctx := context.Background()
	_, err := auditor.AuditErrorHandling(ctx, "")

	if err != safety.ErrInvalidInput {
		t.Errorf("Expected ErrInvalidInput, got %v", err)
	}
}

func TestErrorAuditor_AuditErrorHandling_ContextCanceled(t *testing.T) {
	g, idx := createTestGraph()
	auditor := NewErrorAuditor(g, idx)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := auditor.AuditErrorHandling(ctx, "handlers")

	if err != safety.ErrContextCanceled {
		t.Errorf("Expected ErrContextCanceled, got %v", err)
	}
}

func TestErrorAuditor_AuditErrorHandling_DetectsFailOpen(t *testing.T) {
	g, idx := createTestGraph()
	auditor := NewErrorAuditor(g, idx)

	// Set file content with fail-open pattern
	auditor.SetFileContent("handlers/auth.go", `
package handlers

func CheckAuth(user string) error {
	err := validateToken(user)
	if err != nil {
		log.Printf("auth error: %v", err)
		// NO RETURN - continues execution!
	}
	// This code runs even on auth failure
	return nil
}
`)

	ctx := context.Background()
	result, err := auditor.AuditErrorHandling(ctx, "handlers")

	if err != nil {
		t.Fatalf("AuditErrorHandling failed: %v", err)
	}

	// Should detect fail-open
	foundFailOpen := false
	for _, issue := range result.Issues {
		if issue.Type == "fail_open" {
			foundFailOpen = true
			// Should be CRITICAL because it's in a security function
			if issue.Severity != safety.SeverityCritical {
				t.Errorf("Expected CRITICAL severity for fail-open in security function, got %s", issue.Severity)
			}
			if issue.CWE != "CWE-755" {
				t.Errorf("Expected CWE-755, got %s", issue.CWE)
			}
			break
		}
	}

	if !foundFailOpen {
		t.Error("Expected to detect fail-open issue")
	}

	if result.Summary.FailOpenPaths == 0 {
		t.Error("Expected FailOpenPaths > 0 in summary")
	}
}

func TestErrorAuditor_AuditErrorHandling_DetectsInfoLeak(t *testing.T) {
	g, idx := createTestGraph()
	auditor := NewErrorAuditor(g, idx)

	// Set file content with info leak
	auditor.SetFileContent("handlers/auth.go", `
package handlers

func HandleError(w http.ResponseWriter, err error) {
	// BAD: Sending full error to client
	w.Write([]byte(err.Error()))
}
`)

	ctx := context.Background()
	result, err := auditor.AuditErrorHandling(ctx, "handlers",
		safety.WithAuditFocus("info_leak"))

	if err != nil {
		t.Fatalf("AuditErrorHandling failed: %v", err)
	}

	// Should detect info leak
	foundInfoLeak := false
	for _, issue := range result.Issues {
		if issue.Type == "verbose_error" {
			foundInfoLeak = true
			if issue.CWE != "CWE-209" {
				t.Errorf("Expected CWE-209, got %s", issue.CWE)
			}
			break
		}
	}

	if !foundInfoLeak {
		t.Error("Expected to detect verbose error info leak")
	}
}

func TestErrorAuditor_AuditErrorHandling_DetectsSwallowedError(t *testing.T) {
	g, idx := createTestGraph()
	auditor := NewErrorAuditor(g, idx)

	// Set file content with swallowed error
	auditor.SetFileContent("handlers/auth.go", `
package handlers

func DoSomething() {
	err := riskyOperation()
	if err != nil { }
	// Error is completely ignored
}
`)

	ctx := context.Background()
	result, err := auditor.AuditErrorHandling(ctx, "handlers")

	if err != nil {
		t.Fatalf("AuditErrorHandling failed: %v", err)
	}

	// Should detect swallowed error
	foundSwallow := false
	for _, issue := range result.Issues {
		if issue.Type == "swallow" {
			foundSwallow = true
			if issue.CWE != "CWE-390" {
				t.Errorf("Expected CWE-390, got %s", issue.CWE)
			}
			break
		}
	}

	if !foundSwallow {
		t.Error("Expected to detect swallowed error")
	}

	if result.Summary.Swallowed == 0 {
		t.Error("Expected Swallowed > 0 in summary")
	}
}

func TestErrorAuditor_AuditErrorHandling_SafeCode(t *testing.T) {
	g, idx := createTestGraph()
	auditor := NewErrorAuditor(g, idx)

	// Set file content with proper error handling
	auditor.SetFileContent("handlers/auth.go", `
package handlers

func CheckAuth(user string) error {
	err := validateToken(user)
	if err != nil {
		log.Printf("auth error: %v", err)
		return fmt.Errorf("authentication failed: %w", err)
	}
	return nil
}
`)

	ctx := context.Background()
	result, err := auditor.AuditErrorHandling(ctx, "handlers",
		safety.WithAuditFocus("fail_open"))

	if err != nil {
		t.Fatalf("AuditErrorHandling failed: %v", err)
	}

	// Should NOT detect fail-open
	for _, issue := range result.Issues {
		if issue.Type == "fail_open" {
			t.Error("Should NOT detect fail-open in properly handled code")
		}
	}
}

func TestErrorAuditor_AuditErrorHandling_Performance(t *testing.T) {
	g, idx := createTestGraph()
	auditor := NewErrorAuditor(g, idx)

	auditor.SetFileContent("handlers/auth.go", `
package handlers

func Handler() error {
	if err != nil {
		return err
	}
	return nil
}
`)

	ctx := context.Background()
	start := time.Now()

	_, err := auditor.AuditErrorHandling(ctx, "handlers")

	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("AuditErrorHandling failed: %v", err)
	}

	// Target: < 500ms
	if elapsed > 500*time.Millisecond {
		t.Errorf("AuditErrorHandling took %v, expected < 500ms", elapsed)
	}
}
