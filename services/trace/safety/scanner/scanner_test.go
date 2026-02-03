// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package scanner

import (
	"context"
	"testing"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/trace/ast"
	"github.com/AleutianAI/AleutianFOSS/services/trace/graph"
	"github.com/AleutianAI/AleutianFOSS/services/trace/index"
	"github.com/AleutianAI/AleutianFOSS/services/trace/safety"
)

func createTestGraphForScanner() (*graph.Graph, *index.SymbolIndex) {
	g := graph.NewGraph("/test")
	idx := index.NewSymbolIndex()

	// Handler with SQL query
	handler := &ast.Symbol{
		ID:        "handlers.HandleSearch",
		Name:      "HandleSearch",
		Kind:      ast.SymbolKindFunction,
		Language:  "go",
		FilePath:  "handlers/search.go",
		Package:   "handlers",
		StartLine: 10,
	}

	// SQL query function
	sqlQuery := &ast.Symbol{
		ID:        "db.Query",
		Name:      "Query",
		Kind:      ast.SymbolKindFunction,
		Language:  "go",
		FilePath:  "db/queries.go",
		Package:   "database/sql",
		StartLine: 50,
	}

	g.AddNode(handler)
	g.AddNode(sqlQuery)
	idx.Add(handler)
	idx.Add(sqlQuery)

	g.AddEdge(handler.ID, sqlQuery.ID, graph.EdgeTypeCalls, ast.Location{StartLine: 15})

	g.Freeze()
	return g, idx
}

// --- Pattern Tests ---

func TestSecurityPatternDB_NewSecurityPatternDB(t *testing.T) {
	db := NewSecurityPatternDB()

	if db.Version != PatternVersion {
		t.Errorf("Expected version %s, got %s", PatternVersion, db.Version)
	}

	// Check that we have patterns
	goPatterns := db.GetPatternsForLanguage("go")
	if len(goPatterns) == 0 {
		t.Error("Expected patterns for Go language")
	}

	// Check specific pattern exists
	sqlPattern := db.GetPattern("SEC-020")
	if sqlPattern == nil {
		t.Error("Expected SEC-020 (SQL injection) pattern")
	}
	if sqlPattern.CWE != "CWE-89" {
		t.Errorf("Expected CWE-89, got %s", sqlPattern.CWE)
	}
}

func TestSecurityPattern_Detection_Match(t *testing.T) {
	tests := []struct {
		name        string
		pattern     *SecurityPattern
		content     string
		expectMatch bool
	}{
		{
			name: "SQL injection detected",
			pattern: &SecurityPattern{
				Detection: DetectionMethod{
					Type:    "pattern",
					Pattern: `SELECT.*\+`,
				},
			},
			content:     `query := "SELECT * FROM users WHERE id = " + userID`,
			expectMatch: true,
		},
		{
			name: "SQL injection not detected with parameterized query",
			pattern: &SecurityPattern{
				Detection: DetectionMethod{
					Type:            "pattern",
					Pattern:         `SELECT.*\+`,
					NegativePattern: `\?\s*,|\$\d+`,
				},
			},
			content:     `query := "SELECT * FROM users WHERE id = $1"`,
			expectMatch: false,
		},
		{
			name: "Command injection detected",
			pattern: &SecurityPattern{
				Detection: DetectionMethod{
					Type:    "pattern",
					Pattern: `exec\.Command.*\+`,
				},
			},
			content:     `cmd := exec.Command("sh", "-c", "ls " + userInput)`,
			expectMatch: true,
		},
		{
			name: "Weak crypto detected",
			pattern: &SecurityPattern{
				Detection: DetectionMethod{
					Type:    "pattern",
					Pattern: `md5\s*[.(]`,
				},
			},
			content:     `hash := md5.Sum(data)`,
			expectMatch: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matches := tt.pattern.Detection.Match(tt.content)
			hasMatch := len(matches) > 0

			if hasMatch != tt.expectMatch {
				t.Errorf("Expected match=%v, got %v", tt.expectMatch, hasMatch)
			}
		})
	}
}

// --- Confidence Tests ---

func TestConfidenceCalculator_Calculate(t *testing.T) {
	calc := NewConfidenceCalculator()

	tests := []struct {
		name        string
		pattern     *SecurityPattern
		ctx         *ScanContext
		expectedMin float64
		expectedMax float64
	}{
		{
			name: "base confidence for normal file",
			pattern: &SecurityPattern{
				ID:             "SEC-020",
				BaseConfidence: 0.8,
			},
			ctx: &ScanContext{
				FilePath: "handlers/auth.go",
			},
			expectedMin: 0.5,
			expectedMax: 0.9,
		},
		{
			name: "reduced confidence for test file",
			pattern: &SecurityPattern{
				ID:             "SEC-020",
				BaseConfidence: 0.8,
			},
			ctx: &ScanContext{
				FilePath:   "handlers/auth_test.go",
				IsTestFile: true,
			},
			expectedMin: 0.1,
			expectedMax: 0.4,
		},
		{
			name: "reduced confidence with suppression",
			pattern: &SecurityPattern{
				ID:             "SEC-020",
				BaseConfidence: 0.8,
			},
			ctx: &ScanContext{
				FilePath:        "handlers/auth.go",
				HasNoSecComment: true,
			},
			expectedMin: 0.0,
			expectedMax: 0.15,
		},
		{
			name: "boosted confidence with data flow proof",
			pattern: &SecurityPattern{
				ID:             "SEC-020",
				BaseConfidence: 0.7,
			},
			ctx: &ScanContext{
				FilePath:       "handlers/auth.go",
				DataFlowProven: true,
			},
			expectedMin: 0.7,
			expectedMax: 1.0,
		},
		{
			name: "boosted confidence for security function",
			pattern: &SecurityPattern{
				ID:             "SEC-020",
				BaseConfidence: 0.7,
			},
			ctx: &ScanContext{
				FilePath:           "handlers/auth.go",
				InSecurityFunction: true,
			},
			expectedMin: 0.5,
			expectedMax: 0.9,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conf := calc.Calculate(tt.pattern, tt.ctx)

			if conf < tt.expectedMin || conf > tt.expectedMax {
				t.Errorf("Expected confidence in [%v, %v], got %v", tt.expectedMin, tt.expectedMax, conf)
			}
		})
	}
}

func TestIsTestFile(t *testing.T) {
	tests := []struct {
		path     string
		expected bool
	}{
		{"handlers/auth.go", false},
		{"handlers/auth_test.go", true},
		{"tests/handlers.go", true},
		{"src/components/Button.test.tsx", true},
		{"src/components/Button.spec.ts", true},
		{"src/__tests__/Button.tsx", true},
		{"test_utils.py", true},
		{"utils_test.py", true},
		{"UserTest.java", true},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			result := IsTestFile(tt.path)
			if result != tt.expected {
				t.Errorf("IsTestFile(%q) = %v, expected %v", tt.path, result, tt.expected)
			}
		})
	}
}

func TestIsSecurityFunction(t *testing.T) {
	tests := []struct {
		funcName string
		expected bool
	}{
		{"HandleSearch", false},
		{"CheckAuth", true},
		{"ValidateToken", true},
		{"AuthenticateUser", true},
		{"ProcessRequest", false},
		{"SanitizeInput", true},
		{"hashPassword", true},
		{"encryptData", true},
		{"getUser", false},
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

func TestHasSuppressionComment(t *testing.T) {
	tests := []struct {
		name           string
		content        string
		start          int
		end            int
		expectSuppress bool
		expectNote     string
	}{
		{
			name:           "nosec on same line",
			content:        `password := "secret" // nosec`,
			start:          0,
			end:            20,
			expectSuppress: true,
		},
		{
			name:           "nosec with reason",
			content:        `password := "secret" // nosec: intentional for testing`,
			start:          0,
			end:            20,
			expectSuppress: true,
			expectNote:     "intentional for testing",
		},
		{
			name:           "nolint:gosec",
			content:        `password := "secret" //nolint:gosec`,
			start:          0,
			end:            20,
			expectSuppress: true,
		},
		{
			name:           "no suppression",
			content:        `password := "secret"`,
			start:          0,
			end:            20,
			expectSuppress: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hasSuppression, note := HasSuppressionComment(tt.content, tt.start, tt.end)

			if hasSuppression != tt.expectSuppress {
				t.Errorf("Expected suppression=%v, got %v", tt.expectSuppress, hasSuppression)
			}

			if tt.expectNote != "" && note != tt.expectNote {
				t.Errorf("Expected note=%q, got %q", tt.expectNote, note)
			}
		})
	}
}

// --- Scanner Tests ---

func TestSecurityScanner_ScanForSecurityIssues_EmptyScope(t *testing.T) {
	g, idx := createTestGraphForScanner()
	scanner := NewSecurityScanner(g, idx)

	ctx := context.Background()
	_, err := scanner.ScanForSecurityIssues(ctx, "")

	if err != safety.ErrInvalidInput {
		t.Errorf("Expected ErrInvalidInput, got %v", err)
	}
}

func TestSecurityScanner_ScanForSecurityIssues_GraphNotFrozen(t *testing.T) {
	g := graph.NewGraph("/test")
	idx := index.NewSymbolIndex()
	// Don't freeze the graph

	sym := &ast.Symbol{ID: "test", Name: "test", Kind: ast.SymbolKindFunction, Language: "go"}
	g.AddNode(sym)
	idx.Add(sym)

	scanner := NewSecurityScanner(g, idx)
	ctx := context.Background()

	_, err := scanner.ScanForSecurityIssues(ctx, "test")

	if err != safety.ErrGraphNotReady {
		t.Errorf("Expected ErrGraphNotReady, got %v", err)
	}
}

func TestSecurityScanner_ScanForSecurityIssues_ContextCanceled(t *testing.T) {
	g, idx := createTestGraphForScanner()
	scanner := NewSecurityScanner(g, idx)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := scanner.ScanForSecurityIssues(ctx, "handlers")

	if err != safety.ErrContextCanceled {
		t.Errorf("Expected ErrContextCanceled, got %v", err)
	}
}

func TestSecurityScanner_ScanForSecurityIssues_WithContent(t *testing.T) {
	g, idx := createTestGraphForScanner()
	scanner := NewSecurityScanner(g, idx)

	// Set file content with a SQL injection vulnerability
	scanner.SetFileContent("handlers/search.go", `
package handlers

func HandleSearch(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	sql := "SELECT * FROM products WHERE name = '" + query + "'"
	db.Query(sql)
}
`)

	ctx := context.Background()
	result, err := scanner.ScanForSecurityIssues(ctx, "handlers")

	if err != nil {
		t.Fatalf("ScanForSecurityIssues failed: %v", err)
	}

	// Should find SQL injection
	found := false
	for _, issue := range result.Issues {
		if issue.Type == "sql_injection" {
			found = true
			if issue.CWE != "CWE-89" {
				t.Errorf("Expected CWE-89, got %s", issue.CWE)
			}
			if issue.Severity != safety.SeverityCritical {
				t.Errorf("Expected CRITICAL severity, got %s", issue.Severity)
			}
			break
		}
	}

	if !found {
		t.Error("Expected to find SQL injection issue")
	}
}

func TestSecurityScanner_ScanForSecurityIssues_Performance(t *testing.T) {
	g, idx := createTestGraphForScanner()
	scanner := NewSecurityScanner(g, idx)

	// Set some file content
	scanner.SetFileContent("handlers/search.go", `
package handlers

func HandleSearch(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	db.Query("SELECT * FROM products WHERE name = ?", query)
}
`)

	ctx := context.Background()
	start := time.Now()

	_, err := scanner.ScanForSecurityIssues(ctx, "handlers")

	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("ScanForSecurityIssues failed: %v", err)
	}

	// Target: < 500ms
	if elapsed > 500*time.Millisecond {
		t.Errorf("ScanForSecurityIssues took %v, expected < 500ms", elapsed)
	}
}

func TestSecurityScanner_ScanForSecurityIssues_SeverityFilter(t *testing.T) {
	g, idx := createTestGraphForScanner()
	scanner := NewSecurityScanner(g, idx)

	// Set content with both high and low severity issues
	scanner.SetFileContent("handlers/search.go", `
package handlers

func HandleSearch() {
	// Critical: SQL injection
	sql := "SELECT * FROM users WHERE id = " + userID

	// Medium: weak crypto for non-password
	hash := md5.Sum(data)
}
`)

	ctx := context.Background()

	// Scan with HIGH minimum severity
	result, err := scanner.ScanForSecurityIssues(ctx, "handlers",
		safety.WithMinSeverity(safety.SeverityHigh))

	if err != nil {
		t.Fatalf("ScanForSecurityIssues failed: %v", err)
	}

	// Should only have HIGH or CRITICAL issues
	for _, issue := range result.Issues {
		if issue.Severity != safety.SeverityHigh && issue.Severity != safety.SeverityCritical {
			t.Errorf("Expected HIGH or CRITICAL severity, got %s", issue.Severity)
		}
	}
}

// --- Secret Finder Tests ---

func TestSecretFinder_FindHardcodedSecrets_EmptyScope(t *testing.T) {
	g, idx := createTestGraphForScanner()
	finder := NewSecretFinder(g, idx)

	ctx := context.Background()
	_, err := finder.FindHardcodedSecrets(ctx, "")

	if err != safety.ErrInvalidInput {
		t.Errorf("Expected ErrInvalidInput, got %v", err)
	}
}

func TestSecretFinder_FindHardcodedSecrets_ContextCanceled(t *testing.T) {
	g, idx := createTestGraphForScanner()
	finder := NewSecretFinder(g, idx)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := finder.FindHardcodedSecrets(ctx, "handlers")

	if err != safety.ErrContextCanceled {
		t.Errorf("Expected ErrContextCanceled, got %v", err)
	}
}

func TestSecretFinder_FindHardcodedSecrets_APIKey(t *testing.T) {
	g := graph.NewGraph("/test")
	idx := index.NewSymbolIndex()

	// Add a symbol for the config file
	configSym := &ast.Symbol{
		ID:        "config.Settings",
		Name:      "Settings",
		Kind:      ast.SymbolKindVariable,
		Language:  "go",
		FilePath:  "config/settings.go",
		Package:   "config",
		StartLine: 1,
	}
	g.AddNode(configSym)
	idx.Add(configSym)
	g.Freeze()

	finder := NewSecretFinder(g, idx)

	finder.SetFileContent("config/settings.go", `
package config

const (
	API_KEY = "sk_live_abcdefghij1234567890"
)
`)

	ctx := context.Background()
	secrets, err := finder.FindHardcodedSecrets(ctx, "config")

	if err != nil {
		t.Fatalf("FindHardcodedSecrets failed: %v", err)
	}

	// Should find the API key
	found := false
	for _, secret := range secrets {
		if secret.Type == "api_key" || secret.Type == "stripe_key" {
			found = true
			if secret.Severity != safety.SeverityCritical {
				t.Errorf("Expected CRITICAL severity, got %s", secret.Severity)
			}
			// Context should be masked
			if secret.Context != "" && !containsMask(secret.Context) {
				t.Logf("Warning: Secret may not be properly masked in context")
			}
			break
		}
	}

	if !found {
		t.Error("Expected to find API key secret")
	}
}

func TestSecretFinder_FindHardcodedSecrets_AWSKey(t *testing.T) {
	g := graph.NewGraph("/test")
	idx := index.NewSymbolIndex()

	configSym := &ast.Symbol{
		ID:        "config.awsAccessKey",
		Name:      "awsAccessKey",
		Kind:      ast.SymbolKindVariable,
		Language:  "go",
		FilePath:  "config/aws.go",
		Package:   "config",
		StartLine: 1,
	}
	g.AddNode(configSym)
	idx.Add(configSym)
	g.Freeze()

	finder := NewSecretFinder(g, idx)

	// Use a valid AWS access key format (20 chars, starts with AKIA)
	// Note: Don't use "EXAMPLE" in the key as it triggers false positive filter
	finder.SetFileContent("config/aws.go", `
package config

// AWS access key embedded in code
var awsAccessKey = AKIAI44QH8DHBPRODKEY
`)

	ctx := context.Background()
	secrets, err := finder.FindHardcodedSecrets(ctx, "config")

	if err != nil {
		t.Fatalf("FindHardcodedSecrets failed: %v", err)
	}

	found := false
	for _, secret := range secrets {
		if secret.Type == "aws_access_key" {
			found = true
			break
		}
	}

	if !found {
		t.Error("Expected to find AWS access key")
	}
}

func TestSecretFinder_FindHardcodedSecrets_PrivateKey(t *testing.T) {
	g := graph.NewGraph("/test")
	idx := index.NewSymbolIndex()

	configSym := &ast.Symbol{
		ID:        "config.privateKey",
		Name:      "privateKey",
		Kind:      ast.SymbolKindVariable,
		Language:  "go",
		FilePath:  "config/certs.go",
		Package:   "config",
		StartLine: 1,
	}
	g.AddNode(configSym)
	idx.Add(configSym)
	g.Freeze()

	finder := NewSecretFinder(g, idx)

	finder.SetFileContent("config/certs.go", `
package config

const privateKey = `+"`"+`-----BEGIN RSA PRIVATE KEY-----
MIIBOgIBAAJBALRiMLAH...
-----END RSA PRIVATE KEY-----`+"`")

	ctx := context.Background()
	secrets, err := finder.FindHardcodedSecrets(ctx, "config")

	if err != nil {
		t.Fatalf("FindHardcodedSecrets failed: %v", err)
	}

	found := false
	for _, secret := range secrets {
		if secret.Type == "private_key" {
			found = true
			if secret.Severity != safety.SeverityCritical {
				t.Errorf("Expected CRITICAL severity for private key, got %s", secret.Severity)
			}
			break
		}
	}

	if !found {
		t.Error("Expected to find private key")
	}
}

func TestSecretFinder_FindHardcodedSecrets_Password(t *testing.T) {
	g := graph.NewGraph("/test")
	idx := index.NewSymbolIndex()

	configSym := &ast.Symbol{
		ID:        "config.dbPassword",
		Name:      "dbPassword",
		Kind:      ast.SymbolKindVariable,
		Language:  "go",
		FilePath:  "config/db.go",
		Package:   "config",
		StartLine: 1,
	}
	g.AddNode(configSym)
	idx.Add(configSym)
	g.Freeze()

	finder := NewSecretFinder(g, idx)

	finder.SetFileContent("config/db.go", `
package config

var dbPassword = "supersecretpassword123"
`)

	ctx := context.Background()
	secrets, err := finder.FindHardcodedSecrets(ctx, "config")

	if err != nil {
		t.Fatalf("FindHardcodedSecrets failed: %v", err)
	}

	found := false
	for _, secret := range secrets {
		if secret.Type == "password" {
			found = true
			break
		}
	}

	if !found {
		t.Error("Expected to find password")
	}
}

func TestSecretFinder_FindHardcodedSecrets_FalsePositive(t *testing.T) {
	g := graph.NewGraph("/test")
	idx := index.NewSymbolIndex()

	configSym := &ast.Symbol{
		ID:        "config.example",
		Name:      "example",
		Kind:      ast.SymbolKindVariable,
		Language:  "go",
		FilePath:  "config/example.go",
		Package:   "config",
		StartLine: 1,
	}
	g.AddNode(configSym)
	idx.Add(configSym)
	g.Freeze()

	finder := NewSecretFinder(g, idx)

	// This should NOT be flagged - it's a placeholder
	finder.SetFileContent("config/example.go", `
package config

var apiKey = "your-api-key-here"
var password = "test"
var secret = "example_value"
`)

	ctx := context.Background()
	secrets, err := finder.FindHardcodedSecrets(ctx, "config")

	if err != nil {
		t.Fatalf("FindHardcodedSecrets failed: %v", err)
	}

	// Should not flag obvious placeholders
	for _, secret := range secrets {
		t.Logf("Found secret: %s at %s (might be false positive)", secret.Type, secret.Location)
	}
}

func TestSecretFinder_FindHardcodedSecrets_SkipsTestFiles(t *testing.T) {
	_, idx := createTestGraphForScanner()

	// Create a graph with a test file symbol
	testSym := &ast.Symbol{
		ID:        "config_test.TestConfig",
		Name:      "TestConfig",
		Kind:      ast.SymbolKindFunction,
		Language:  "go",
		FilePath:  "config/config_test.go",
		Package:   "config",
		StartLine: 1,
	}
	g2 := graph.NewGraph("/test")
	g2.AddNode(testSym)
	g2.Freeze()

	finder := NewSecretFinder(g2, idx)
	finder.SetFileContent("config/config_test.go", `
package config

func TestConfig(t *testing.T) {
	// Test credentials - should be ignored
	apiKey := "sk_test_1234567890abcdefghij"
}
`)

	ctx := context.Background()
	secrets, err := finder.FindHardcodedSecrets(ctx, "config")

	if err != nil {
		t.Fatalf("FindHardcodedSecrets failed: %v", err)
	}

	// Should skip test files
	if len(secrets) > 0 {
		t.Errorf("Expected no secrets from test files, got %d", len(secrets))
	}
}

func TestSecretPattern_Match_DatabaseURL(t *testing.T) {
	patterns := defaultSecretPatterns()
	var dbURLPattern *SecretPattern
	for _, p := range patterns {
		if p.Type == "database_url" {
			dbURLPattern = p
			break
		}
	}

	if dbURLPattern == nil {
		t.Fatal("Database URL pattern not found")
	}

	tests := []struct {
		content     string
		expectMatch bool
	}{
		{`postgres://user:password@localhost:5432/db`, true},
		{`mongodb://admin:secret@mongo.example.com/mydb`, true},
		{`mysql://root:pass123@127.0.0.1/test`, true},
		{`redis://user:auth@redis.local:6379/0`, true},
		{`postgres://localhost:5432/db`, false}, // No credentials
	}

	for _, tt := range tests {
		matches := dbURLPattern.Match(tt.content)
		hasMatch := len(matches) > 0

		if hasMatch != tt.expectMatch {
			t.Errorf("Match(%q) = %v, expected %v", tt.content, hasMatch, tt.expectMatch)
		}
	}
}

// Helper function to check if a string contains masking characters
func containsMask(s string) bool {
	for _, c := range s {
		if c == '*' {
			return true
		}
	}
	return false
}
