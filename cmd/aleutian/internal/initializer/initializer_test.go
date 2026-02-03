// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package initializer

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestInitializer_Init_BasicGo tests initialization of a simple Go project.
func TestInitializer_Init_BasicGo(t *testing.T) {
	// Create temp directory with Go files
	tempDir := t.TempDir()

	// Create a simple Go file
	goFile := filepath.Join(tempDir, "main.go")
	goContent := `package main

func main() {
	hello()
}

func hello() {
	println("Hello, World!")
}
`
	if err := os.WriteFile(goFile, []byte(goContent), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Create initializer
	storage := NewStorage(tempDir)
	init := NewInitializer(storage)

	// Run init
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cfg := DefaultConfig(tempDir)
	result, err := init.Init(ctx, cfg, nil)

	// Verify success
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	if result == nil {
		t.Fatal("Result is nil")
	}

	// Verify result fields
	if result.ProjectRoot != tempDir {
		t.Errorf("ProjectRoot = %q, want %q", result.ProjectRoot, tempDir)
	}

	if result.FilesIndexed != 1 {
		t.Errorf("FilesIndexed = %d, want 1", result.FilesIndexed)
	}

	if result.SymbolsFound < 2 {
		t.Errorf("SymbolsFound = %d, want >= 2 (file + functions)", result.SymbolsFound)
	}

	// Verify .aleutian directory was created
	if !storage.Exists() {
		t.Error(".aleutian directory not created")
	}

	// Verify manifest can be loaded
	manifest, err := storage.LoadManifest(true)
	if err != nil {
		t.Errorf("LoadManifest failed: %v", err)
	}

	if manifest.FormatVersion != FormatVersion {
		t.Errorf("FormatVersion = %q, want %q", manifest.FormatVersion, FormatVersion)
	}
}

// TestInitializer_Init_MultiLanguage tests detection of multiple languages.
func TestInitializer_Init_MultiLanguage(t *testing.T) {
	tempDir := t.TempDir()

	// Create Go file
	goFile := filepath.Join(tempDir, "main.go")
	if err := os.WriteFile(goFile, []byte("package main\nfunc main() {}\n"), 0644); err != nil {
		t.Fatalf("Failed to create Go file: %v", err)
	}

	// Create Python file
	pyFile := filepath.Join(tempDir, "script.py")
	if err := os.WriteFile(pyFile, []byte("def main():\n    pass\n"), 0644); err != nil {
		t.Fatalf("Failed to create Python file: %v", err)
	}

	storage := NewStorage(tempDir)
	init := NewInitializer(storage)

	ctx := context.Background()
	cfg := DefaultConfig(tempDir)
	result, err := init.Init(ctx, cfg, nil)

	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	// Should detect both languages
	if len(result.Languages) < 2 {
		t.Errorf("Languages = %v, want at least 2", result.Languages)
	}

	// Should index both files
	if result.FilesIndexed < 2 {
		t.Errorf("FilesIndexed = %d, want >= 2", result.FilesIndexed)
	}
}

// TestInitializer_Init_SpecificLanguage tests limiting to specific languages.
func TestInitializer_Init_SpecificLanguage(t *testing.T) {
	tempDir := t.TempDir()

	// Create Go file
	goFile := filepath.Join(tempDir, "main.go")
	if err := os.WriteFile(goFile, []byte("package main\nfunc main() {}\n"), 0644); err != nil {
		t.Fatalf("Failed to create Go file: %v", err)
	}

	// Create Python file (should be ignored)
	pyFile := filepath.Join(tempDir, "script.py")
	if err := os.WriteFile(pyFile, []byte("def main():\n    pass\n"), 0644); err != nil {
		t.Fatalf("Failed to create Python file: %v", err)
	}

	storage := NewStorage(tempDir)
	init := NewInitializer(storage)

	ctx := context.Background()
	cfg := DefaultConfig(tempDir)
	cfg.Languages = []string{"go"} // Only Go

	result, err := init.Init(ctx, cfg, nil)

	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	// Should only have Go
	if len(result.Languages) != 1 || result.Languages[0] != "go" {
		t.Errorf("Languages = %v, want [go]", result.Languages)
	}

	// Should only index Go file
	if result.FilesIndexed != 1 {
		t.Errorf("FilesIndexed = %d, want 1", result.FilesIndexed)
	}
}

// TestInitializer_Init_DryRun tests dry run mode.
func TestInitializer_Init_DryRun(t *testing.T) {
	tempDir := t.TempDir()

	// Create Go file
	goFile := filepath.Join(tempDir, "main.go")
	if err := os.WriteFile(goFile, []byte("package main\nfunc main() {}\n"), 0644); err != nil {
		t.Fatalf("Failed to create Go file: %v", err)
	}

	storage := NewStorage(tempDir)
	init := NewInitializer(storage)

	ctx := context.Background()
	cfg := DefaultConfig(tempDir)
	cfg.DryRun = true

	result, err := init.Init(ctx, cfg, nil)

	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	// Should report files found
	if result.FilesIndexed != 1 {
		t.Errorf("FilesIndexed = %d, want 1", result.FilesIndexed)
	}

	// Should NOT create .aleutian directory
	if storage.Exists() {
		t.Error(".aleutian directory created in dry-run mode")
	}
}

// TestInitializer_Init_Excludes tests exclude patterns.
func TestInitializer_Init_Excludes(t *testing.T) {
	tempDir := t.TempDir()

	// Create main file
	mainFile := filepath.Join(tempDir, "main.go")
	if err := os.WriteFile(mainFile, []byte("package main\nfunc main() {}\n"), 0644); err != nil {
		t.Fatalf("Failed to create main file: %v", err)
	}

	// Create test file (should be excluded)
	testFile := filepath.Join(tempDir, "main_test.go")
	if err := os.WriteFile(testFile, []byte("package main\nfunc TestMain() {}\n"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	storage := NewStorage(tempDir)
	init := NewInitializer(storage)

	ctx := context.Background()
	cfg := DefaultConfig(tempDir)
	cfg.ExcludePatterns = append(cfg.ExcludePatterns, "*_test.go")

	result, err := init.Init(ctx, cfg, nil)

	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	// Should only index main file (test excluded)
	if result.FilesIndexed != 1 {
		t.Errorf("FilesIndexed = %d, want 1 (test file should be excluded)", result.FilesIndexed)
	}
}

// TestInitializer_Init_Force tests force rebuild.
func TestInitializer_Init_Force(t *testing.T) {
	tempDir := t.TempDir()

	// Create Go file
	goFile := filepath.Join(tempDir, "main.go")
	if err := os.WriteFile(goFile, []byte("package main\nfunc main() {}\n"), 0644); err != nil {
		t.Fatalf("Failed to create Go file: %v", err)
	}

	storage := NewStorage(tempDir)
	init := NewInitializer(storage)

	ctx := context.Background()
	cfg := DefaultConfig(tempDir)

	// First init
	_, err := init.Init(ctx, cfg, nil)
	if err != nil {
		t.Fatalf("First init failed: %v", err)
	}

	// Add another file
	goFile2 := filepath.Join(tempDir, "helper.go")
	if err := os.WriteFile(goFile2, []byte("package main\nfunc helper() {}\n"), 0644); err != nil {
		t.Fatalf("Failed to create second Go file: %v", err)
	}

	// Second init with force
	cfg.Force = true
	result, err := init.Init(ctx, cfg, nil)
	if err != nil {
		t.Fatalf("Force init failed: %v", err)
	}

	// Should index both files
	if result.FilesIndexed != 2 {
		t.Errorf("FilesIndexed = %d, want 2", result.FilesIndexed)
	}
}

// TestInitializer_Init_Cancellation tests context cancellation.
func TestInitializer_Init_Cancellation(t *testing.T) {
	tempDir := t.TempDir()

	// Create many files to increase chance of hitting cancellation
	for i := 0; i < 100; i++ {
		goFile := filepath.Join(tempDir, filepath.Base(t.Name())+"_"+string(rune('a'+i%26))+".go")
		if err := os.WriteFile(goFile, []byte("package main\nfunc f() {}\n"), 0644); err != nil {
			t.Fatalf("Failed to create Go file: %v", err)
		}
	}

	storage := NewStorage(tempDir)
	init := NewInitializer(storage)

	// Create already-cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	cfg := DefaultConfig(tempDir)
	_, err := init.Init(ctx, cfg, nil)

	// Should fail with context error
	if err == nil {
		t.Error("Init should fail with cancelled context")
	}
}

// TestInitializer_Init_EmptyProject tests initialization of empty project.
func TestInitializer_Init_EmptyProject(t *testing.T) {
	tempDir := t.TempDir()

	storage := NewStorage(tempDir)
	init := NewInitializer(storage)

	ctx := context.Background()
	cfg := DefaultConfig(tempDir)
	_, err := init.Init(ctx, cfg, nil)

	// Should fail with no languages
	if err == nil {
		t.Error("Init should fail on empty project")
	}
}

// TestInitializer_Init_Progress tests progress callback.
func TestInitializer_Init_Progress(t *testing.T) {
	tempDir := t.TempDir()

	// Create Go file
	goFile := filepath.Join(tempDir, "main.go")
	if err := os.WriteFile(goFile, []byte("package main\nfunc main() {}\n"), 0644); err != nil {
		t.Fatalf("Failed to create Go file: %v", err)
	}

	storage := NewStorage(tempDir)
	init := NewInitializer(storage)

	var progressCalls []string
	progressCb := func(p Progress) {
		progressCalls = append(progressCalls, p.Phase)
	}

	ctx := context.Background()
	cfg := DefaultConfig(tempDir)
	_, err := init.Init(ctx, cfg, progressCb)

	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	// Should have progress calls
	if len(progressCalls) == 0 {
		t.Error("No progress callbacks received")
	}

	// Should include key phases
	phases := make(map[string]bool)
	for _, phase := range progressCalls {
		phases[phase] = true
	}

	expectedPhases := []string{"detecting", "scanning", "writing", "complete"}
	for _, phase := range expectedPhases {
		if !phases[phase] {
			t.Errorf("Missing progress phase: %s", phase)
		}
	}
}

// TestConfig_Validate tests configuration validation.
func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{
			name:    "valid config",
			cfg:     DefaultConfig("/tmp/test"),
			wantErr: false,
		},
		{
			name: "empty project root",
			cfg: Config{
				ProjectRoot: "",
				MaxWorkers:  4,
				MaxFileSize: 1024,
			},
			wantErr: true,
		},
		{
			name: "zero max workers",
			cfg: Config{
				ProjectRoot: "/tmp/test",
				MaxWorkers:  0,
				MaxFileSize: 1024,
			},
			wantErr: true,
		},
		{
			name: "negative max file size",
			cfg: Config{
				ProjectRoot: "/tmp/test",
				MaxWorkers:  4,
				MaxFileSize: -1,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestMemoryIndex_Queries tests in-memory index queries.
func TestMemoryIndex_Queries(t *testing.T) {
	index := NewMemoryIndex()

	// Add symbols
	index.Symbols = []Symbol{
		{ID: "sym1", Name: "main", Kind: "function", FilePath: "main.go"},
		{ID: "sym2", Name: "helper", Kind: "function", FilePath: "main.go"},
		{ID: "sym3", Name: "main", Kind: "function", FilePath: "other.go"},
	}

	// Add edges
	index.Edges = []Edge{
		{FromID: "sym1", ToID: "sym2", Kind: "calls"},
		{FromID: "sym3", ToID: "sym2", Kind: "calls"},
	}

	// Build indexes
	index.BuildIndexes()

	// Test GetByID
	sym := index.GetByID("sym1")
	if sym == nil || sym.Name != "main" {
		t.Errorf("GetByID(sym1) = %v, want main", sym)
	}

	// Test GetByName
	syms := index.GetByName("main")
	if len(syms) != 2 {
		t.Errorf("GetByName(main) returned %d symbols, want 2", len(syms))
	}

	// Test GetByFile
	syms = index.GetByFile("main.go")
	if len(syms) != 2 {
		t.Errorf("GetByFile(main.go) returned %d symbols, want 2", len(syms))
	}

	// Test GetCallers
	edges := index.GetCallers("sym2", 0)
	if len(edges) != 2 {
		t.Errorf("GetCallers(sym2) returned %d edges, want 2", len(edges))
	}

	// Test GetCallees
	edges = index.GetCallees("sym1", 0)
	if len(edges) != 1 {
		t.Errorf("GetCallees(sym1) returned %d edges, want 1", len(edges))
	}
}
