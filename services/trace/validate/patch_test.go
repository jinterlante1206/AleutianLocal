// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package validate

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestNewPatchValidator(t *testing.T) {
	config := DefaultValidatorConfig()
	v, err := NewPatchValidator(config)
	if err != nil {
		t.Fatalf("NewPatchValidator failed: %v", err)
	}
	if v == nil {
		t.Fatal("validator is nil")
	}
}

func TestPatchValidator_CheckSize(t *testing.T) {
	config := DefaultValidatorConfig()
	config.MaxLines = 10
	v, err := NewPatchValidator(config)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name      string
		patch     string
		wantValid bool
	}{
		{
			name:      "small patch passes",
			patch:     "line1\nline2\nline3\n",
			wantValid: true,
		},
		{
			name:      "oversized patch rejected",
			patch:     "1\n2\n3\n4\n5\n6\n7\n8\n9\n10\n11\n12\n",
			wantValid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := &ValidationResult{Valid: true}
			v.checkSize(tt.patch, result)
			if result.Valid != tt.wantValid {
				t.Errorf("Valid = %v, want %v", result.Valid, tt.wantValid)
			}
		})
	}
}

func TestPatchValidator_ParseDiff(t *testing.T) {
	v, _ := NewPatchValidator(DefaultValidatorConfig())

	tests := []struct {
		name      string
		patch     string
		wantFiles int
		wantErr   bool
	}{
		{
			name: "valid unified diff",
			patch: `--- a/file.go
+++ b/file.go
@@ -1,3 +1,4 @@
 package main
+
+import "fmt"
`,
			wantFiles: 1,
			wantErr:   false,
		},
		{
			name: "multi-file diff",
			patch: `--- a/file1.go
+++ b/file1.go
@@ -1 +1 @@
-old
+new
--- a/file2.go
+++ b/file2.go
@@ -1 +1 @@
-old2
+new2
`,
			wantFiles: 2,
			wantErr:   false,
		},
		{
			name:      "empty diff",
			patch:     "",
			wantFiles: 0,
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			files, err := v.parseDiff(tt.patch)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseDiff() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if len(files) != tt.wantFiles {
				t.Errorf("parseDiff() got %d files, want %d", len(files), tt.wantFiles)
			}
		})
	}
}

func TestPatchValidator_CalculateStats(t *testing.T) {
	v, _ := NewPatchValidator(DefaultValidatorConfig())

	patch := `--- a/file.go
+++ b/file.go
@@ -1,3 +1,5 @@
 package main
+
+import "fmt"
+import "os"
-var x = 1
`
	files, err := v.parseDiff(patch)
	if err != nil {
		t.Fatal(err)
	}

	stats := v.calculateStats(files)

	if stats.FilesAffected != 1 {
		t.Errorf("FilesAffected = %d, want 1", stats.FilesAffected)
	}
	if stats.LinesAdded != 3 {
		t.Errorf("LinesAdded = %d, want 3", stats.LinesAdded)
	}
	if stats.LinesRemoved != 1 {
		t.Errorf("LinesRemoved = %d, want 1", stats.LinesRemoved)
	}
}

func TestPatchValidator_Validate_DangerousPatterns(t *testing.T) {
	config := DefaultValidatorConfig()
	config.BlockDangerous = true
	v, _ := NewPatchValidator(config)

	tmpDir := t.TempDir()

	tests := []struct {
		name         string
		patch        string
		wantWarnings int
		wantBlocking bool
	}{
		{
			name: "go exec.Command warning",
			patch: `--- a/main.go
+++ b/main.go
@@ -0,0 +1,5 @@
+package main
+import "os/exec"
+func run() {
+    exec.Command("ls", "-la")
+}
`,
			wantWarnings: 1,
			wantBlocking: false, // exec.Command is warning, not blocking
		},
		{
			name: "python eval blocking",
			patch: `--- a/main.py
+++ b/main.py
@@ -0,0 +1,2 @@
+def run(code):
+    return eval(code)
`,
			wantWarnings: 1,
			wantBlocking: true,
		},
		{
			name: "javascript eval blocking",
			patch: `--- a/main.js
+++ b/main.js
@@ -0,0 +1,3 @@
+function run(code) {
+    return eval(code);
+}
`,
			wantWarnings: 1,
			wantBlocking: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			result, err := v.Validate(ctx, tt.patch, tmpDir)
			if err != nil {
				t.Fatalf("Validate error: %v", err)
			}

			if len(result.Warnings) < tt.wantWarnings {
				t.Errorf("got %d warnings, want at least %d", len(result.Warnings), tt.wantWarnings)
			}

			if tt.wantBlocking && result.Valid {
				t.Error("expected invalid due to blocking warning")
			}
		})
	}
}

func TestPatchValidator_Validate_Secrets(t *testing.T) {
	config := DefaultValidatorConfig()
	v, _ := NewPatchValidator(config)

	tmpDir := t.TempDir()

	tests := []struct {
		name         string
		patch        string
		wantWarnings bool
	}{
		{
			name: "AWS key detected",
			patch: `--- a/config.go
+++ b/config.go
@@ -0,0 +1,3 @@
+package config
+
+const awsKey = "AKIAIOSFODNN7EXAMPLE"
`,
			wantWarnings: true,
		},
		{
			name: "generic password detected",
			patch: `--- a/config.go
+++ b/config.go
@@ -0,0 +1,3 @@
+package config
+
+const password = "xK9#mL2$pQr5tUvW"
`,
			wantWarnings: true,
		},
		{
			name: "safe code no warnings",
			patch: `--- a/main.go
+++ b/main.go
@@ -0,0 +1,5 @@
+package main
+
+func main() {
+    fmt.Println("Hello")
+}
`,
			wantWarnings: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			result, err := v.Validate(ctx, tt.patch, tmpDir)
			if err != nil {
				t.Fatalf("Validate error: %v", err)
			}

			hasSecretWarning := false
			for _, w := range result.Warnings {
				if w.Type == WarnTypeSecret {
					hasSecretWarning = true
					break
				}
			}

			if hasSecretWarning != tt.wantWarnings {
				t.Errorf("hasSecretWarning = %v, want %v", hasSecretWarning, tt.wantWarnings)
			}
		})
	}
}

func TestPatchValidator_Validate_SyntaxError(t *testing.T) {
	config := DefaultValidatorConfig()
	v, _ := NewPatchValidator(config)

	tmpDir := t.TempDir()

	// Create a base file
	basePath := filepath.Join(tmpDir, "main.go")
	baseContent := `package main

func main() {
	fmt.Println("hello")
}
`
	if err := os.WriteFile(basePath, []byte(baseContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Patch that introduces syntax error
	patch := `--- a/main.go
+++ b/main.go
@@ -1,5 +1,6 @@
 package main

 func main() {
+    if true {  // unclosed brace
 	fmt.Println("hello")
 }
`

	ctx := context.Background()
	result, err := v.Validate(ctx, patch, tmpDir)
	if err != nil {
		t.Fatalf("Validate error: %v", err)
	}

	// Should detect syntax error
	hasSyntaxError := false
	for _, e := range result.Errors {
		if e.Type == ErrorTypeSyntax {
			hasSyntaxError = true
			break
		}
	}

	if !hasSyntaxError {
		t.Error("expected syntax error to be detected")
	}

	if result.Valid {
		t.Error("expected result to be invalid due to syntax error")
	}
}

func TestPatchValidator_Validate_Permissions(t *testing.T) {
	config := DefaultValidatorConfig()
	v, _ := NewPatchValidator(config)

	tmpDir := t.TempDir()

	// Create a read-only file
	readOnlyPath := filepath.Join(tmpDir, "readonly.go")
	if err := os.WriteFile(readOnlyPath, []byte("package main"), 0444); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(readOnlyPath, 0644) // Cleanup

	patch := `--- a/readonly.go
+++ b/readonly.go
@@ -1 +1,2 @@
 package main
+// new line
`

	ctx := context.Background()
	result, err := v.Validate(ctx, patch, tmpDir)
	if err != nil {
		t.Fatalf("Validate error: %v", err)
	}

	if len(result.Permissions) == 0 {
		t.Error("expected permission issue to be detected")
	}

	if result.Valid {
		t.Error("expected result to be invalid due to permission issue")
	}
}

func TestCalculateEntropy(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantAbove float64
		wantBelow float64
	}{
		{
			name:      "empty string",
			input:     "",
			wantAbove: -1,
			wantBelow: 0.1,
		},
		{
			name:      "single char",
			input:     "aaaa",
			wantAbove: -1,
			wantBelow: 0.1,
		},
		{
			name:      "random-looking string",
			input:     "aB3$kL9mNpQrStUv",
			wantAbove: 3.0,
			wantBelow: 5.0,
		},
		{
			name:      "high entropy",
			input:     "sk-abc123XYZ789!@#qwertyuiopasdfgh",
			wantAbove: 4.0,
			wantBelow: 6.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entropy := calculateEntropy(tt.input)
			if tt.wantAbove >= 0 && entropy < tt.wantAbove {
				t.Errorf("entropy = %v, want above %v", entropy, tt.wantAbove)
			}
			if entropy > tt.wantBelow {
				t.Errorf("entropy = %v, want below %v", entropy, tt.wantBelow)
			}
		})
	}
}

func TestASTScanner_Scan(t *testing.T) {
	scanner := NewASTScanner()
	ctx := context.Background()

	tests := []struct {
		name         string
		source       string
		language     string
		wantWarnings int
	}{
		{
			name: "go exec.Command",
			source: `package main
import "os/exec"
func main() {
	exec.Command("ls")
}`,
			language:     "go",
			wantWarnings: 1,
		},
		{
			name: "python eval",
			source: `def run(code):
    return eval(code)`,
			language:     "python",
			wantWarnings: 1,
		},
		{
			name: "javascript eval",
			source: `function run(code) {
    return eval(code);
}`,
			language:     "javascript",
			wantWarnings: 1,
		},
		{
			name:         "safe go code",
			source:       `package main\nfunc main() { fmt.Println("hi") }`,
			language:     "go",
			wantWarnings: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			warnings, err := scanner.Scan(ctx, []byte(tt.source), tt.language, "test.go")
			if err != nil {
				t.Fatalf("Scan error: %v", err)
			}
			if len(warnings) != tt.wantWarnings {
				t.Errorf("got %d warnings, want %d", len(warnings), tt.wantWarnings)
			}
		})
	}
}

func TestSecretScanner_Allowlist(t *testing.T) {
	config := DefaultValidatorConfig()
	scanner, _ := NewSecretScanner(config)

	tests := []struct {
		name      string
		filePath  string
		wantAllow bool
	}{
		{
			name:      "test file",
			filePath:  "pkg/auth/auth_test.go",
			wantAllow: true,
		},
		{
			name:      "fixture file",
			filePath:  "testdata/fixtures/config.json",
			wantAllow: true,
		},
		{
			name:      "__tests__ directory",
			filePath:  "src/__tests__/auth.test.js",
			wantAllow: true,
		},
		{
			name:      "production file",
			filePath:  "pkg/auth/auth.go",
			wantAllow: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			allowed := scanner.isAllowlisted(tt.filePath)
			if allowed != tt.wantAllow {
				t.Errorf("isAllowlisted(%s) = %v, want %v", tt.filePath, allowed, tt.wantAllow)
			}
		})
	}
}

func TestPatchValidator_ValidPatch(t *testing.T) {
	config := DefaultValidatorConfig()
	v, _ := NewPatchValidator(config)

	tmpDir := t.TempDir()

	// Create a base file
	basePath := filepath.Join(tmpDir, "main.go")
	baseContent := `package main

func main() {
	fmt.Println("hello")
}
`
	if err := os.WriteFile(basePath, []byte(baseContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Valid patch
	patch := `--- a/main.go
+++ b/main.go
@@ -1,5 +1,6 @@
 package main

 func main() {
+	fmt.Println("world")
 	fmt.Println("hello")
 }
`

	ctx := context.Background()
	result, err := v.Validate(ctx, patch, tmpDir)
	if err != nil {
		t.Fatalf("Validate error: %v", err)
	}

	if !result.Valid {
		t.Errorf("expected valid patch, got errors: %+v", result.Errors)
	}

	if result.PatternVersion != PatternVersion {
		t.Errorf("PatternVersion = %s, want %s", result.PatternVersion, PatternVersion)
	}
}

func BenchmarkValidate_500Lines(b *testing.B) {
	config := DefaultValidatorConfig()
	v, _ := NewPatchValidator(config)

	tmpDir := b.TempDir()

	// Generate a 500-line patch
	var lines []string
	lines = append(lines, "--- a/main.go", "+++ b/main.go", "@@ -0,0 +1,500 @@")
	lines = append(lines, "+package main")
	for i := 0; i < 498; i++ {
		lines = append(lines, "+// line "+string(rune('0'+i%10)))
	}
	patch := ""
	for _, l := range lines {
		patch += l + "\n"
	}

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = v.Validate(ctx, patch, tmpDir)
	}
}
