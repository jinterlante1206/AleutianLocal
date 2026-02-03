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

// TestStorage_WriteIndex_AtomicWrite tests atomic write behavior.
func TestStorage_WriteIndex_AtomicWrite(t *testing.T) {
	tempDir := t.TempDir()
	storage := NewStorage(tempDir)

	ctx := context.Background()
	symbols := []Symbol{
		{ID: "sym1", Name: "main", Kind: "function", FilePath: "main.go", StartLine: 1, EndLine: 5},
	}
	edges := []Edge{
		{FromID: "sym1", ToID: "sym2", Kind: "calls", FilePath: "main.go", Line: 3},
	}

	now := time.Now().Format(time.RFC3339)
	manifest := &ManifestFile{
		FormatVersion:  FormatVersion,
		ProjectRoot:    tempDir,
		Files:          make(map[string]FileEntry),
		CreatedAtMilli: time.Now().UnixMilli(),
	}
	config := &ProjectConfig{
		FormatVersion:   FormatVersion,
		Languages:       []string{"go"},
		ExcludePatterns: []string{"vendor/**"},
		CreatedAt:       now,
		UpdatedAt:       now,
	}

	result, err := storage.WriteIndex(ctx, symbols, edges, manifest, config)
	if err != nil {
		t.Fatalf("WriteIndex failed: %v", err)
	}

	// Verify .aleutian exists
	if !storage.Exists() {
		t.Error(".aleutian directory not created")
	}

	// Verify result
	if result.IndexPath != storage.AleutianPath() {
		t.Errorf("IndexPath = %q, want %q", result.IndexPath, storage.AleutianPath())
	}

	if result.IndexChecksum == "" {
		t.Error("IndexChecksum is empty")
	}

	// Verify files exist
	files := []string{ManifestFileName, IndexFileName, ConfigFileName}
	for _, f := range files {
		path := filepath.Join(storage.AleutianPath(), f)
		if _, err := os.Stat(path); err != nil {
			t.Errorf("File %s not created: %v", f, err)
		}
	}
}

// TestStorage_WriteIndex_Overwrite tests overwriting existing index.
func TestStorage_WriteIndex_Overwrite(t *testing.T) {
	tempDir := t.TempDir()
	storage := NewStorage(tempDir)

	ctx := context.Background()
	now := time.Now().Format(time.RFC3339)
	manifest := &ManifestFile{
		FormatVersion:  FormatVersion,
		ProjectRoot:    tempDir,
		Files:          make(map[string]FileEntry),
		CreatedAtMilli: time.Now().UnixMilli(),
	}
	config := &ProjectConfig{
		FormatVersion: FormatVersion,
		Languages:     []string{"go"},
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	// First write
	symbols1 := []Symbol{{ID: "sym1", Name: "first", Kind: "function", FilePath: "a.go"}}
	_, err := storage.WriteIndex(ctx, symbols1, nil, manifest, config)
	if err != nil {
		t.Fatalf("First WriteIndex failed: %v", err)
	}

	// Second write (overwrite)
	symbols2 := []Symbol{{ID: "sym2", Name: "second", Kind: "function", FilePath: "b.go"}}
	_, err = storage.WriteIndex(ctx, symbols2, nil, manifest, config)
	if err != nil {
		t.Fatalf("Second WriteIndex failed: %v", err)
	}

	// Load and verify second write took effect
	index, err := storage.LoadIndex(ctx)
	if err != nil {
		t.Fatalf("LoadIndex failed: %v", err)
	}

	if len(index.Symbols) != 1 {
		t.Errorf("Expected 1 symbol, got %d", len(index.Symbols))
	}

	if index.Symbols[0].Name != "second" {
		t.Errorf("Symbol name = %q, want 'second'", index.Symbols[0].Name)
	}
}

// TestStorage_LoadManifest_ValidateChecksums tests checksum validation.
func TestStorage_LoadManifest_ValidateChecksums(t *testing.T) {
	tempDir := t.TempDir()
	storage := NewStorage(tempDir)

	ctx := context.Background()
	now := time.Now().Format(time.RFC3339)
	manifest := &ManifestFile{
		FormatVersion:  FormatVersion,
		ProjectRoot:    tempDir,
		Files:          make(map[string]FileEntry),
		CreatedAtMilli: time.Now().UnixMilli(),
	}
	config := &ProjectConfig{
		FormatVersion: FormatVersion,
		Languages:     []string{"go"},
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	// Write index
	symbols := []Symbol{{ID: "sym1", Name: "main", Kind: "function", FilePath: "main.go"}}
	_, err := storage.WriteIndex(ctx, symbols, nil, manifest, config)
	if err != nil {
		t.Fatalf("WriteIndex failed: %v", err)
	}

	// Load with checksum validation
	loadedManifest, err := storage.LoadManifest(true)
	if err != nil {
		t.Fatalf("LoadManifest with validation failed: %v", err)
	}

	if loadedManifest.IndexChecksum == "" {
		t.Error("IndexChecksum is empty")
	}

	// Corrupt the index file
	indexPath := filepath.Join(storage.AleutianPath(), IndexFileName)
	if err := os.WriteFile(indexPath, []byte("corrupted"), 0644); err != nil {
		t.Fatalf("Failed to corrupt index: %v", err)
	}

	// Load with validation should fail
	_, err = storage.LoadManifest(true)
	if err == nil {
		t.Error("LoadManifest should fail with corrupted index")
	}
}

// TestStorage_LoadIndex tests loading index into memory.
func TestStorage_LoadIndex(t *testing.T) {
	tempDir := t.TempDir()
	storage := NewStorage(tempDir)

	ctx := context.Background()
	now := time.Now().Format(time.RFC3339)
	manifest := &ManifestFile{
		FormatVersion:  FormatVersion,
		ProjectRoot:    tempDir,
		Files:          make(map[string]FileEntry),
		CreatedAtMilli: time.Now().UnixMilli(),
	}
	config := &ProjectConfig{
		FormatVersion: FormatVersion,
		Languages:     []string{"go"},
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	// Write index with symbols and edges
	symbols := []Symbol{
		{ID: "sym1", Name: "main", Kind: "function", FilePath: "main.go"},
		{ID: "sym2", Name: "helper", Kind: "function", FilePath: "main.go"},
	}
	edges := []Edge{
		{FromID: "sym1", ToID: "sym2", Kind: "calls", FilePath: "main.go", Line: 5},
	}

	_, err := storage.WriteIndex(ctx, symbols, edges, manifest, config)
	if err != nil {
		t.Fatalf("WriteIndex failed: %v", err)
	}

	// Load index
	index, err := storage.LoadIndex(ctx)
	if err != nil {
		t.Fatalf("LoadIndex failed: %v", err)
	}

	// Verify symbols
	if len(index.Symbols) != 2 {
		t.Errorf("Symbols count = %d, want 2", len(index.Symbols))
	}

	// Verify edges
	if len(index.Edges) != 1 {
		t.Errorf("Edges count = %d, want 1", len(index.Edges))
	}

	// Verify indexes are built
	sym := index.GetByID("sym1")
	if sym == nil {
		t.Error("GetByID returned nil for existing symbol")
	}

	callers := index.GetCallers("sym2", 0)
	if len(callers) != 1 {
		t.Errorf("GetCallers returned %d edges, want 1", len(callers))
	}
}

// TestStorage_Exists tests existence check.
func TestStorage_Exists(t *testing.T) {
	tempDir := t.TempDir()
	storage := NewStorage(tempDir)

	// Should not exist initially
	if storage.Exists() {
		t.Error("Exists() returned true for non-existent index")
	}

	// Create .aleutian directory without manifest
	aleutianPath := storage.AleutianPath()
	if err := os.MkdirAll(aleutianPath, 0755); err != nil {
		t.Fatalf("Failed to create directory: %v", err)
	}

	// Should still not exist (no manifest)
	if storage.Exists() {
		t.Error("Exists() returned true without manifest file")
	}

	// Create manifest
	manifestPath := filepath.Join(aleutianPath, ManifestFileName)
	if err := os.WriteFile(manifestPath, []byte("{}"), 0644); err != nil {
		t.Fatalf("Failed to create manifest: %v", err)
	}

	// Now should exist
	if !storage.Exists() {
		t.Error("Exists() returned false with manifest file")
	}
}

// TestStorage_WriteIndex_NilContext tests nil context handling.
func TestStorage_WriteIndex_NilContext(t *testing.T) {
	tempDir := t.TempDir()
	storage := NewStorage(tempDir)

	manifest := &ManifestFile{}
	config := &ProjectConfig{}

	_, err := storage.WriteIndex(nil, nil, nil, manifest, config)
	if err == nil {
		t.Error("WriteIndex should fail with nil context")
	}
}

// TestStorage_WriteIndex_NilManifest tests nil manifest handling.
func TestStorage_WriteIndex_NilManifest(t *testing.T) {
	tempDir := t.TempDir()
	storage := NewStorage(tempDir)

	ctx := context.Background()
	config := &ProjectConfig{}

	_, err := storage.WriteIndex(ctx, nil, nil, nil, config)
	if err == nil {
		t.Error("WriteIndex should fail with nil manifest")
	}
}

// TestStorage_WriteIndex_NilConfig tests nil config handling.
func TestStorage_WriteIndex_NilConfig(t *testing.T) {
	tempDir := t.TempDir()
	storage := NewStorage(tempDir)

	ctx := context.Background()
	manifest := &ManifestFile{}

	_, err := storage.WriteIndex(ctx, nil, nil, manifest, nil)
	if err == nil {
		t.Error("WriteIndex should fail with nil config")
	}
}
