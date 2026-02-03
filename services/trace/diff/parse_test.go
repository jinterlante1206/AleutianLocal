// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package diff

import (
	"strings"
	"testing"
)

func TestGenerateDiff_SimpleAddition(t *testing.T) {
	old := "line1\nline2\nline3"
	new := "line1\nline2\nnew line\nline3"

	change, err := GenerateDiff("test.go", old, new, "Added a new line")
	if err != nil {
		t.Fatalf("GenerateDiff() error = %v", err)
	}

	if change.FilePath != "test.go" {
		t.Errorf("FilePath = %q, want %q", change.FilePath, "test.go")
	}

	if change.Rationale != "Added a new line" {
		t.Errorf("Rationale = %q, want %q", change.Rationale, "Added a new line")
	}

	if change.Language != "go" {
		t.Errorf("Language = %q, want %q", change.Language, "go")
	}

	if len(change.Hunks) == 0 {
		t.Fatal("Expected at least one hunk")
	}

	// Should have additions
	added, _ := change.LineStats()
	if added == 0 {
		t.Error("Expected added lines > 0")
	}
}

func TestGenerateDiff_SimpleDeletion(t *testing.T) {
	old := "line1\nline2\nline3\nline4"
	new := "line1\nline3\nline4"

	change, err := GenerateDiff("test.py", old, new, "Removed a line")
	if err != nil {
		t.Fatalf("GenerateDiff() error = %v", err)
	}

	_, removed := change.LineStats()
	if removed == 0 {
		t.Error("Expected removed lines > 0")
	}

	if change.Language != "python" {
		t.Errorf("Language = %q, want %q", change.Language, "python")
	}
}

func TestGenerateDiff_NewFile(t *testing.T) {
	new := "package main\n\nfunc main() {}\n"

	change, err := GenerateDiff("main.go", "", new, "New file")
	if err != nil {
		t.Fatalf("GenerateDiff() error = %v", err)
	}

	if !change.IsNew {
		t.Error("Expected IsNew to be true")
	}

	if change.IsDelete {
		t.Error("Expected IsDelete to be false")
	}
}

func TestGenerateDiff_DeleteFile(t *testing.T) {
	old := "package main\n\nfunc main() {}\n"

	change, err := GenerateDiff("main.go", old, "", "Deleting file")
	if err != nil {
		t.Fatalf("GenerateDiff() error = %v", err)
	}

	if change.IsNew {
		t.Error("Expected IsNew to be false")
	}

	if !change.IsDelete {
		t.Error("Expected IsDelete to be true")
	}
}

func TestGenerateDiff_NoChanges(t *testing.T) {
	content := "line1\nline2\nline3"

	change, err := GenerateDiff("test.go", content, content, "No changes")
	if err != nil {
		t.Fatalf("GenerateDiff() error = %v", err)
	}

	if len(change.Hunks) != 0 {
		t.Errorf("Expected 0 hunks for identical content, got %d", len(change.Hunks))
	}
}

func TestGenerateDiff_Modification(t *testing.T) {
	old := "func hello() {\n    return \"hello\"\n}"
	new := "func hello() {\n    return \"world\"\n}"

	change, err := GenerateDiff("test.go", old, new, "Changed return value")
	if err != nil {
		t.Fatalf("GenerateDiff() error = %v", err)
	}

	added, removed := change.LineStats()
	if added == 0 || removed == 0 {
		t.Errorf("Expected both additions and deletions, got +%d -%d", added, removed)
	}
}

func TestDetectLanguage(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"main.go", "go"},
		{"script.py", "python"},
		{"app.js", "javascript"},
		{"component.tsx", "typescriptreact"},
		{"style.css", "css"},
		{"config.yaml", "yaml"},
		{"data.json", "json"},
		{"readme.md", "markdown"},
		{"unknown.xyz", "text"},
		{"Makefile", "text"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := detectLanguage(tt.path); got != tt.want {
				t.Errorf("detectLanguage(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestAssessRisk(t *testing.T) {
	t.Run("deletion_is_high_risk", func(t *testing.T) {
		change := &ProposedChange{IsDelete: true}
		if got := assessRisk(change); got != RiskHigh {
			t.Errorf("assessRisk() = %v, want %v", got, RiskHigh)
		}
	})

	t.Run("security_file_is_critical", func(t *testing.T) {
		change := &ProposedChange{
			FilePath: "auth/security.go",
			Hunks: []*Hunk{
				{Lines: []DiffLine{{Type: LineAdded}}},
			},
		}
		if got := assessRisk(change); got != RiskCritical {
			t.Errorf("assessRisk() = %v, want %v", got, RiskCritical)
		}
	})

	t.Run("pure_addition_is_low_risk", func(t *testing.T) {
		change := &ProposedChange{
			FilePath: "utils.go",
			Hunks: []*Hunk{
				{Lines: []DiffLine{
					{Type: LineAdded, Content: "line1"},
					{Type: LineAdded, Content: "line2"},
				}},
			},
		}
		if got := assessRisk(change); got != RiskLow {
			t.Errorf("assessRisk() = %v, want %v", got, RiskLow)
		}
	})

	t.Run("large_deletion_is_high_risk", func(t *testing.T) {
		lines := make([]DiffLine, 25)
		for i := range lines {
			lines[i] = DiffLine{Type: LineRemoved, Content: "deleted"}
		}
		change := &ProposedChange{
			FilePath: "utils.go",
			Hunks:    []*Hunk{{Lines: lines}},
		}
		if got := assessRisk(change); got != RiskHigh {
			t.Errorf("assessRisk() = %v, want %v", got, RiskHigh)
		}
	})
}

func TestIsSecuritySensitive(t *testing.T) {
	sensitive := []string{
		"auth/handler.go",
		"pkg/security/validate.go",
		"internal/credential/store.go",
		"password_utils.py",
		"token_generator.js",
		"crypto/aes.go",
		"pkg/permission/checker.go",
	}

	for _, path := range sensitive {
		if !isSecuritySensitive(path) {
			t.Errorf("isSecuritySensitive(%q) = false, want true", path)
		}
	}

	notSensitive := []string{
		"main.go",
		"utils/string.go",
		"models/user.go",
		"handlers/home.go",
	}

	for _, path := range notSensitive {
		if isSecuritySensitive(path) {
			t.Errorf("isSecuritySensitive(%q) = true, want false", path)
		}
	}
}

func TestSplitLines(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    []string
	}{
		{"empty", "", nil},
		{"single_line", "hello", []string{"hello"}},
		{"two_lines", "a\nb", []string{"a", "b"}},
		{"trailing_newline", "a\nb\n", []string{"a", "b", ""}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := splitLines(tt.content)
			if len(got) != len(tt.want) {
				t.Errorf("splitLines() len = %d, want %d", len(got), len(tt.want))
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("splitLines()[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestCleanDiffPath(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"a/main.go", "main.go"},
		{"b/main.go", "main.go"},
		{"main.go", "main.go"},
		{"a/pkg/utils/helper.go", "pkg/utils/helper.go"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := cleanDiffPath(tt.input); got != tt.want {
				t.Errorf("cleanDiffPath(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseMultiFileDiff(t *testing.T) {
	diffText := `--- a/file1.go
+++ b/file1.go
@@ -1,3 +1,4 @@
 package main
+
 func main() {}
 // end
--- a/file2.go
+++ b/file2.go
@@ -1,2 +1,2 @@
-var old = 1
+var new = 2
 // end
`

	changes, err := ParseMultiFileDiff(diffText)
	if err != nil {
		t.Fatalf("ParseMultiFileDiff() error = %v", err)
	}

	if len(changes) != 2 {
		t.Errorf("Expected 2 files, got %d", len(changes))
	}

	// Verify first file
	if changes[0].FilePath != "file1.go" {
		t.Errorf("First file path = %q, want %q", changes[0].FilePath, "file1.go")
	}

	// Verify second file
	if changes[1].FilePath != "file2.go" {
		t.Errorf("Second file path = %q, want %q", changes[1].FilePath, "file2.go")
	}
}

func TestComputeEdits(t *testing.T) {
	t.Run("simple_insertion", func(t *testing.T) {
		old := []string{"a", "b", "c"}
		new := []string{"a", "x", "b", "c"}

		edits := computeEdits(old, new)

		// Should have equal, insert, equal, equal
		insertCount := 0
		for _, e := range edits {
			if e.kind == editInsert {
				insertCount++
				if e.text != "x" {
					t.Errorf("Expected inserted text 'x', got %q", e.text)
				}
			}
		}
		if insertCount != 1 {
			t.Errorf("Expected 1 insertion, got %d", insertCount)
		}
	})

	t.Run("simple_deletion", func(t *testing.T) {
		old := []string{"a", "b", "c"}
		new := []string{"a", "c"}

		edits := computeEdits(old, new)

		deleteCount := 0
		for _, e := range edits {
			if e.kind == editDelete {
				deleteCount++
				if e.text != "b" {
					t.Errorf("Expected deleted text 'b', got %q", e.text)
				}
			}
		}
		if deleteCount != 1 {
			t.Errorf("Expected 1 deletion, got %d", deleteCount)
		}
	})

	t.Run("empty_inputs", func(t *testing.T) {
		edits := computeEdits(nil, nil)
		if edits != nil {
			t.Error("Expected nil for empty inputs")
		}
	})
}

func TestParseHunkBody(t *testing.T) {
	body := ` context line
-removed line
+added line
 another context`

	lines := parseHunkBody(body, 10, 10)

	if len(lines) != 4 {
		t.Fatalf("Expected 4 lines, got %d", len(lines))
	}

	// Check line types
	if lines[0].Type != LineContext {
		t.Errorf("Line 0 type = %v, want context", lines[0].Type)
	}
	if lines[1].Type != LineRemoved {
		t.Errorf("Line 1 type = %v, want removed", lines[1].Type)
	}
	if lines[2].Type != LineAdded {
		t.Errorf("Line 2 type = %v, want added", lines[2].Type)
	}
	if lines[3].Type != LineContext {
		t.Errorf("Line 3 type = %v, want context", lines[3].Type)
	}

	// Check line numbers
	if lines[0].OldNum != 10 || lines[0].NewNum != 10 {
		t.Errorf("Context line numbers wrong: old=%d new=%d", lines[0].OldNum, lines[0].NewNum)
	}
	if lines[1].OldNum != 11 || lines[1].NewNum != 0 {
		t.Errorf("Removed line numbers wrong: old=%d new=%d", lines[1].OldNum, lines[1].NewNum)
	}
	if lines[2].OldNum != 0 || lines[2].NewNum != 11 {
		t.Errorf("Added line numbers wrong: old=%d new=%d", lines[2].OldNum, lines[2].NewNum)
	}
}

func TestGenerateDiff_LargeFile(t *testing.T) {
	// Test with a larger file to exercise hunk grouping
	var oldBuilder strings.Builder

	// Create a 100 line file
	for i := 0; i < 100; i++ {
		oldBuilder.WriteString("line ")
		oldBuilder.WriteString(string(rune('0' + i%10)))
		oldBuilder.WriteString("\n")
	}
	old := oldBuilder.String()

	// Modify lines 20, 50, and 80
	lines := strings.Split(old, "\n")
	lines[20] = "modified line 20"
	lines[50] = "modified line 50"
	lines[80] = "modified line 80"
	new := strings.Join(lines, "\n")

	change, err := GenerateDiff("large.go", old, new, "Multiple modifications")
	if err != nil {
		t.Fatalf("GenerateDiff() error = %v", err)
	}

	// Should have multiple hunks (changes are far apart)
	if len(change.Hunks) < 2 {
		t.Errorf("Expected multiple hunks, got %d", len(change.Hunks))
	}
}
