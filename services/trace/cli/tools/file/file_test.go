// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package file

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ============================================================================
// Test Helpers
// ============================================================================

func setupTestDir(t *testing.T) (string, *Config, func()) {
	t.Helper()
	dir, err := os.MkdirTemp("", "file_tools_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	config := NewConfig(dir)
	cleanup := func() {
		os.RemoveAll(dir)
	}
	return dir, config, cleanup
}

func createTestFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}
	return path
}

// ============================================================================
// Read Tool Tests
// ============================================================================

func TestReadTool_Execute_SimpleFile(t *testing.T) {
	dir, config, cleanup := setupTestDir(t)
	defer cleanup()

	content := "line 1\nline 2\nline 3\n"
	path := createTestFile(t, dir, "test.txt", content)

	tool := NewReadTool(config)
	ctx := context.Background()

	result, err := tool.Execute(ctx, map[string]any{
		"file_path": path,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected success, got error: %s", result.Error)
	}

	readResult, ok := result.Output.(*ReadResult)
	if !ok {
		t.Fatalf("expected *ReadResult, got %T", result.Output)
	}
	if readResult.TotalLines != 3 {
		t.Errorf("expected 3 lines, got %d", readResult.TotalLines)
	}
	if readResult.LinesRead != 3 {
		t.Errorf("expected 3 lines read, got %d", readResult.LinesRead)
	}
}

func TestReadTool_Execute_WithOffset(t *testing.T) {
	dir, config, cleanup := setupTestDir(t)
	defer cleanup()

	var content strings.Builder
	for i := 1; i <= 100; i++ {
		content.WriteString("line " + string(rune('0'+i%10)) + "\n")
	}
	path := createTestFile(t, dir, "large.txt", content.String())

	tool := NewReadTool(config)
	ctx := context.Background()

	result, err := tool.Execute(ctx, map[string]any{
		"file_path": path,
		"offset":    50,
		"limit":     10,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected success, got error: %s", result.Error)
	}

	readResult := result.Output.(*ReadResult)
	if readResult.LinesRead != 10 {
		t.Errorf("expected 10 lines read, got %d", readResult.LinesRead)
	}
	if readResult.TotalLines != 100 {
		t.Errorf("expected 100 total lines, got %d", readResult.TotalLines)
	}
	if !readResult.Truncated {
		t.Error("expected truncated=true")
	}
}

func TestReadTool_Execute_FileNotFound(t *testing.T) {
	dir, config, cleanup := setupTestDir(t)
	defer cleanup()

	tool := NewReadTool(config)
	ctx := context.Background()

	result, err := tool.Execute(ctx, map[string]any{
		"file_path": filepath.Join(dir, "nonexistent.txt"),
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Success {
		t.Fatal("expected failure for nonexistent file")
	}
	// Accept either "file not found" or "no such file" error messages
	if !strings.Contains(result.Error, "not found") && !strings.Contains(result.Error, "no such file") {
		t.Errorf("expected error about missing file, got: %s", result.Error)
	}
}

func TestReadTool_Execute_Directory(t *testing.T) {
	dir, config, cleanup := setupTestDir(t)
	defer cleanup()

	tool := NewReadTool(config)
	ctx := context.Background()

	result, err := tool.Execute(ctx, map[string]any{
		"file_path": dir,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Success {
		t.Fatal("expected failure for directory")
	}
	if !strings.Contains(result.Error, "directory") {
		t.Errorf("expected 'directory' error, got: %s", result.Error)
	}
}

func TestReadTool_Execute_LineNumberFormat(t *testing.T) {
	dir, config, cleanup := setupTestDir(t)
	defer cleanup()

	content := "hello\nworld\n"
	path := createTestFile(t, dir, "test.txt", content)

	tool := NewReadTool(config)
	ctx := context.Background()

	result, err := tool.Execute(ctx, map[string]any{
		"file_path": path,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Check line number format
	outputText := result.OutputText
	if !strings.Contains(outputText, "     1→hello") {
		t.Errorf("expected line 1 format, got: %s", outputText)
	}
	if !strings.Contains(outputText, "     2→world") {
		t.Errorf("expected line 2 format, got: %s", outputText)
	}
}

// ============================================================================
// Write Tool Tests
// ============================================================================

func TestWriteTool_Execute_NewFile(t *testing.T) {
	dir, config, cleanup := setupTestDir(t)
	defer cleanup()

	tool := NewWriteTool(config)
	ctx := context.Background()
	path := filepath.Join(dir, "new_file.txt")
	content := "new content"

	result, err := tool.Execute(ctx, map[string]any{
		"file_path": path,
		"content":   content,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected success, got error: %s", result.Error)
	}

	writeResult := result.Output.(*WriteResult)
	if !writeResult.Created {
		t.Error("expected Created=true for new file")
	}
	if writeResult.BytesWritten != int64(len(content)) {
		t.Errorf("expected %d bytes written, got %d", len(content), writeResult.BytesWritten)
	}

	// Verify file contents
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read written file: %v", err)
	}
	if string(data) != content {
		t.Errorf("file content mismatch: expected %q, got %q", content, string(data))
	}

	// Verify ModifiedFiles
	if len(result.ModifiedFiles) != 1 || result.ModifiedFiles[0] != path {
		t.Errorf("expected ModifiedFiles=[%s], got %v", path, result.ModifiedFiles)
	}
}

func TestWriteTool_Execute_Overwrite(t *testing.T) {
	dir, config, cleanup := setupTestDir(t)
	defer cleanup()

	path := createTestFile(t, dir, "existing.txt", "old content")

	tool := NewWriteTool(config)
	ctx := context.Background()
	newContent := "new content"

	result, err := tool.Execute(ctx, map[string]any{
		"file_path": path,
		"content":   newContent,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected success, got error: %s", result.Error)
	}

	writeResult := result.Output.(*WriteResult)
	if writeResult.Created {
		t.Error("expected Created=false for overwrite")
	}
}

func TestWriteTool_Execute_CreateParentDirs(t *testing.T) {
	dir, config, cleanup := setupTestDir(t)
	defer cleanup()

	tool := NewWriteTool(config)
	ctx := context.Background()
	path := filepath.Join(dir, "subdir", "nested", "file.txt")

	result, err := tool.Execute(ctx, map[string]any{
		"file_path": path,
		"content":   "content",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected success, got error: %s", result.Error)
	}

	// Verify file exists
	if _, err := os.Stat(path); err != nil {
		t.Errorf("file should exist: %v", err)
	}
}

func TestWriteTool_Execute_OutsideAllowed(t *testing.T) {
	_, config, cleanup := setupTestDir(t)
	defer cleanup()

	tool := NewWriteTool(config)
	ctx := context.Background()

	result, err := tool.Execute(ctx, map[string]any{
		"file_path": "/tmp/outside_allowed.txt",
		"content":   "malicious",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Success {
		t.Fatal("expected failure for path outside allowed directories")
	}
	if !strings.Contains(result.Error, "outside allowed") {
		t.Errorf("expected 'outside allowed' error, got: %s", result.Error)
	}
}

// ============================================================================
// Edit Tool Tests
// ============================================================================

func TestEditTool_Execute_SingleMatch(t *testing.T) {
	dir, config, cleanup := setupTestDir(t)
	defer cleanup()

	original := "func oldName() {\n  // body\n}\n"
	path := createTestFile(t, dir, "code.go", original)

	// Mark file as read (required for edit)
	config.MarkFileRead(path)

	tool := NewEditTool(config)
	ctx := context.Background()

	result, err := tool.Execute(ctx, map[string]any{
		"file_path":  path,
		"old_string": "func oldName()",
		"new_string": "func newName()",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected success, got error: %s", result.Error)
	}

	editResult := result.Output.(*EditResult)
	if editResult.Replacements != 1 {
		t.Errorf("expected 1 replacement, got %d", editResult.Replacements)
	}

	// Verify file changed
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "func newName()") {
		t.Error("file should contain new function name")
	}

	// Verify diff generated
	if !strings.Contains(editResult.Diff, "-func oldName()") {
		t.Error("diff should show removed line")
	}
	if !strings.Contains(editResult.Diff, "+func newName()") {
		t.Error("diff should show added line")
	}
}

func TestEditTool_Execute_NoMatch(t *testing.T) {
	dir, config, cleanup := setupTestDir(t)
	defer cleanup()

	path := createTestFile(t, dir, "code.go", "some content")
	config.MarkFileRead(path)

	tool := NewEditTool(config)
	ctx := context.Background()

	result, err := tool.Execute(ctx, map[string]any{
		"file_path":  path,
		"old_string": "nonexistent",
		"new_string": "replacement",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Success {
		t.Fatal("expected failure for no match")
	}
	if !strings.Contains(result.Error, "not found") {
		t.Errorf("expected 'not found' error, got: %s", result.Error)
	}
}

func TestEditTool_Execute_MultipleMatch_NoReplaceAll(t *testing.T) {
	dir, config, cleanup := setupTestDir(t)
	defer cleanup()

	path := createTestFile(t, dir, "code.go", "foo\nfoo\nfoo\n")
	config.MarkFileRead(path)

	tool := NewEditTool(config)
	ctx := context.Background()

	result, err := tool.Execute(ctx, map[string]any{
		"file_path":  path,
		"old_string": "foo",
		"new_string": "bar",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Success {
		t.Fatal("expected failure for multiple matches")
	}
	if !strings.Contains(result.Error, "multiple") {
		t.Errorf("expected 'multiple' error, got: %s", result.Error)
	}
}

func TestEditTool_Execute_ReplaceAll(t *testing.T) {
	dir, config, cleanup := setupTestDir(t)
	defer cleanup()

	path := createTestFile(t, dir, "code.go", "foo\nfoo\nfoo\n")
	config.MarkFileRead(path)

	tool := NewEditTool(config)
	ctx := context.Background()

	result, err := tool.Execute(ctx, map[string]any{
		"file_path":   path,
		"old_string":  "foo",
		"new_string":  "bar",
		"replace_all": true,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected success, got error: %s", result.Error)
	}

	editResult := result.Output.(*EditResult)
	if editResult.Replacements != 3 {
		t.Errorf("expected 3 replacements, got %d", editResult.Replacements)
	}

	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), "foo") {
		t.Error("file should not contain 'foo' after replace all")
	}
}

func TestEditTool_Execute_FileNotRead(t *testing.T) {
	dir, config, cleanup := setupTestDir(t)
	defer cleanup()

	path := createTestFile(t, dir, "code.go", "content")
	// Don't mark as read

	tool := NewEditTool(config)
	ctx := context.Background()

	result, err := tool.Execute(ctx, map[string]any{
		"file_path":  path,
		"old_string": "content",
		"new_string": "new",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Success {
		t.Fatal("expected failure when file not read first")
	}
	if !strings.Contains(result.Error, "read before") {
		t.Errorf("expected 'read before' error, got: %s", result.Error)
	}
}

// ============================================================================
// Glob Tool Tests
// ============================================================================

func TestGlobTool_Execute_SimplePattern(t *testing.T) {
	dir, config, cleanup := setupTestDir(t)
	defer cleanup()

	// Create test files
	createTestFile(t, dir, "file1.go", "go content")
	createTestFile(t, dir, "file2.go", "go content")
	createTestFile(t, dir, "file.txt", "txt content")

	tool := NewGlobTool(config)
	ctx := context.Background()

	result, err := tool.Execute(ctx, map[string]any{
		"pattern": "*.go",
		"path":    dir,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected success, got error: %s", result.Error)
	}

	globResult := result.Output.(*GlobResult)
	if globResult.Count != 2 {
		t.Errorf("expected 2 matches, got %d", globResult.Count)
	}
}

func TestGlobTool_Execute_RecursivePattern(t *testing.T) {
	dir, config, cleanup := setupTestDir(t)
	defer cleanup()

	// Create nested structure
	subdir := filepath.Join(dir, "subdir")
	os.MkdirAll(subdir, 0755)
	createTestFile(t, dir, "root.go", "content")
	createTestFile(t, subdir, "nested.go", "content")

	tool := NewGlobTool(config)
	ctx := context.Background()

	result, err := tool.Execute(ctx, map[string]any{
		"pattern": "**/*.go",
		"path":    dir,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected success, got error: %s", result.Error)
	}

	globResult := result.Output.(*GlobResult)
	if globResult.Count != 2 {
		t.Errorf("expected 2 matches (recursive), got %d", globResult.Count)
	}
}

func TestGlobTool_Execute_SortedByMtime(t *testing.T) {
	dir, config, cleanup := setupTestDir(t)
	defer cleanup()

	// Create files with different modification times
	path1 := createTestFile(t, dir, "older.go", "old")
	time.Sleep(10 * time.Millisecond) // Ensure different mtime
	path2 := createTestFile(t, dir, "newer.go", "new")

	tool := NewGlobTool(config)
	ctx := context.Background()

	result, err := tool.Execute(ctx, map[string]any{
		"pattern": "*.go",
		"path":    dir,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	globResult := result.Output.(*GlobResult)
	if len(globResult.Files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(globResult.Files))
	}

	// Newer file should be first (sorted by mtime desc)
	if globResult.Files[0].Path != path2 {
		t.Errorf("expected newer file first, got %s", globResult.Files[0].Path)
	}
	if globResult.Files[1].Path != path1 {
		t.Errorf("expected older file second, got %s", globResult.Files[1].Path)
	}
}

func TestGlobTool_Execute_Limit(t *testing.T) {
	dir, config, cleanup := setupTestDir(t)
	defer cleanup()

	// Create many files
	for i := 0; i < 10; i++ {
		createTestFile(t, dir, "file"+string(rune('0'+i))+".go", "content")
	}

	tool := NewGlobTool(config)
	ctx := context.Background()

	result, err := tool.Execute(ctx, map[string]any{
		"pattern": "*.go",
		"path":    dir,
		"limit":   5,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	globResult := result.Output.(*GlobResult)
	if globResult.Count != 5 {
		t.Errorf("expected 5 results (limited), got %d", globResult.Count)
	}
	if !globResult.Truncated {
		t.Error("expected truncated=true")
	}
}

// ============================================================================
// Grep Tool Tests
// ============================================================================

func TestGrepTool_Execute_SimpleMatch(t *testing.T) {
	dir, config, cleanup := setupTestDir(t)
	defer cleanup()

	createTestFile(t, dir, "code.go", "func main() {\n  fmt.Println(\"hello\")\n}\n")

	tool := NewGrepTool(config)
	ctx := context.Background()

	result, err := tool.Execute(ctx, map[string]any{
		"pattern": "main",
		"path":    dir,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected success, got error: %s", result.Error)
	}

	grepResult := result.Output.(*GrepResult)
	if grepResult.Count != 1 {
		t.Errorf("expected 1 match, got %d", grepResult.Count)
	}
	if grepResult.Matches[0].Line != 1 {
		t.Errorf("expected match on line 1, got %d", grepResult.Matches[0].Line)
	}
}

func TestGrepTool_Execute_WithContext(t *testing.T) {
	dir, config, cleanup := setupTestDir(t)
	defer cleanup()

	content := "before1\nbefore2\nMATCH\nafter1\nafter2\n"
	createTestFile(t, dir, "test.txt", content)

	tool := NewGrepTool(config)
	ctx := context.Background()

	result, err := tool.Execute(ctx, map[string]any{
		"pattern":       "MATCH",
		"path":          dir,
		"context_lines": 2,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected success, got error: %s", result.Error)
	}

	grepResult := result.Output.(*GrepResult)
	if len(grepResult.Matches) != 1 {
		t.Fatalf("expected 1 match, got %d", len(grepResult.Matches))
	}

	match := grepResult.Matches[0]
	if len(match.ContextBefore) != 2 {
		t.Errorf("expected 2 context lines before, got %d", len(match.ContextBefore))
	}
	if len(match.ContextAfter) != 2 {
		t.Errorf("expected 2 context lines after, got %d", len(match.ContextAfter))
	}
}

func TestGrepTool_Execute_CaseInsensitive(t *testing.T) {
	dir, config, cleanup := setupTestDir(t)
	defer cleanup()

	createTestFile(t, dir, "test.txt", "Hello World\n")

	tool := NewGrepTool(config)
	ctx := context.Background()

	// Case sensitive (should not match)
	result1, _ := tool.Execute(ctx, map[string]any{
		"pattern": "hello",
		"path":    dir,
	})
	if result1.Output.(*GrepResult).Count != 0 {
		t.Error("case sensitive should not match")
	}

	// Case insensitive (should match)
	result2, _ := tool.Execute(ctx, map[string]any{
		"pattern":          "hello",
		"path":             dir,
		"case_insensitive": true,
	})
	if result2.Output.(*GrepResult).Count != 1 {
		t.Error("case insensitive should match")
	}
}

func TestGrepTool_Execute_GlobFilter(t *testing.T) {
	dir, config, cleanup := setupTestDir(t)
	defer cleanup()

	createTestFile(t, dir, "code.go", "func main()")
	createTestFile(t, dir, "code.py", "def main()")

	tool := NewGrepTool(config)
	ctx := context.Background()

	result, err := tool.Execute(ctx, map[string]any{
		"pattern": "main",
		"path":    dir,
		"glob":    "*.go",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	grepResult := result.Output.(*GrepResult)
	if grepResult.Count != 1 {
		t.Errorf("expected 1 match (filtered), got %d", grepResult.Count)
	}
}

func TestGrepTool_Execute_RegexPattern(t *testing.T) {
	dir, config, cleanup := setupTestDir(t)
	defer cleanup()

	createTestFile(t, dir, "code.go", "func NewService() *Service\nfunc NewClient() *Client\n")

	tool := NewGrepTool(config)
	ctx := context.Background()

	result, err := tool.Execute(ctx, map[string]any{
		"pattern": `func New\w+\(`,
		"path":    dir,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	grepResult := result.Output.(*GrepResult)
	if grepResult.Count != 2 {
		t.Errorf("expected 2 regex matches, got %d", grepResult.Count)
	}
}

// ============================================================================
// Parameter Validation Tests
// ============================================================================

func TestReadParams_Validate(t *testing.T) {
	tests := []struct {
		name    string
		params  ReadParams
		wantErr bool
	}{
		{
			name:    "empty file_path",
			params:  ReadParams{},
			wantErr: true,
		},
		{
			name:    "relative path",
			params:  ReadParams{FilePath: "relative/path.txt"},
			wantErr: true,
		},
		{
			name:    "path with ..",
			params:  ReadParams{FilePath: "/path/../etc/passwd"},
			wantErr: true,
		},
		{
			name:    "negative offset",
			params:  ReadParams{FilePath: "/valid/path.txt", Offset: -1},
			wantErr: true,
		},
		{
			name:    "limit too high",
			params:  ReadParams{FilePath: "/valid/path.txt", Limit: MaxReadLimit + 1},
			wantErr: true,
		},
		{
			name:    "valid params",
			params:  ReadParams{FilePath: "/valid/path.txt", Offset: 10, Limit: 100},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.params.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestGlobParams_Validate(t *testing.T) {
	tests := []struct {
		name    string
		params  GlobParams
		wantErr bool
	}{
		{
			name:    "empty pattern",
			params:  GlobParams{},
			wantErr: true,
		},
		{
			name:    "too many wildcards",
			params:  GlobParams{Pattern: "**/**/**/**/file"},
			wantErr: true,
		},
		{
			name:    "valid pattern",
			params:  GlobParams{Pattern: "**/*.go"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.params.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// ============================================================================
// Config Tests
// ============================================================================

func TestConfig_IsPathAllowed(t *testing.T) {
	dir, config, cleanup := setupTestDir(t)
	defer cleanup()

	// Create a subdir for testing
	subdir := filepath.Join(dir, "subdir")
	os.MkdirAll(subdir, 0755)

	// Create another dir outside allowed paths
	otherDir, err := os.MkdirTemp("", "other_test")
	if err != nil {
		t.Fatalf("failed to create other dir: %v", err)
	}
	defer os.RemoveAll(otherDir)

	tests := []struct {
		name    string
		path    string
		allowed bool
	}{
		{"file in project", filepath.Join(dir, "file.go"), true},
		{"file in subdir", filepath.Join(subdir, "file.go"), true},
		{"file in other dir", filepath.Join(otherDir, "file.go"), false},
		{"system file", "/etc/passwd", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := config.IsPathAllowed(tt.path); got != tt.allowed {
				t.Errorf("IsPathAllowed(%s) = %v, want %v", tt.path, got, tt.allowed)
			}
		})
	}
}

func TestConfig_ReadTracking(t *testing.T) {
	dir, config, cleanup := setupTestDir(t)
	defer cleanup()

	path := filepath.Join(dir, "file.go")

	if config.WasFileRead(path) {
		t.Error("file should not be marked as read initially")
	}

	config.MarkFileRead(path)

	if !config.WasFileRead(path) {
		t.Error("file should be marked as read after MarkFileRead")
	}
}

func TestIsSensitivePath(t *testing.T) {
	tests := []struct {
		path      string
		sensitive bool
	}{
		{"/etc/passwd", true},
		{"/home/user/.ssh/id_rsa", true},
		{"/home/user/.aws/credentials", true},
		{"/home/user/project/.env", true},
		{"/home/user/project/main.go", false},
		{"/home/user/project/config.yaml", false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := IsSensitivePath(tt.path); got != tt.sensitive {
				t.Errorf("IsSensitivePath(%s) = %v, want %v", tt.path, got, tt.sensitive)
			}
		})
	}
}

// ============================================================================
// Helper Function Tests
// ============================================================================

func TestExpandBraces(t *testing.T) {
	tests := []struct {
		pattern  string
		expected []string
	}{
		{"*.go", []string{"*.go"}},
		{"*.{go,ts}", []string{"*.go", "*.ts"}},
		{"*.{go,ts,py}", []string{"*.go", "*.ts", "*.py"}},
		{"{a,b}.{c,d}", []string{"a.c", "a.d", "b.c", "b.d"}},
	}

	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			got := expandBraces(tt.pattern)
			if len(got) != len(tt.expected) {
				t.Errorf("expandBraces(%s) = %v, want %v", tt.pattern, got, tt.expected)
			}
		})
	}
}
