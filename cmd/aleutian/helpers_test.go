// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// TestGetOrchestratorBaseURL checks that the default URL matches expectations
func TestGetOrchestratorBaseURL(t *testing.T) {
	url := getOrchestratorBaseURL()
	expected := fmt.Sprintf("http://%s:%d", DefaultOrchestratorHost, DefaultOrchestratorPort)
	if url != expected {
		t.Errorf("Expected %s, got %s", expected, url)
	}
}

// TestEnsureEssentialDirs verifies directory creation logic
func TestEnsureEssentialDirs(t *testing.T) {
	// 1. Create a temp directory to act as the stack dir
	tmpDir := t.TempDir()

	// 2. Run the function
	if err := ensureEssentialDirs(tmpDir); err != nil {
		t.Fatalf("ensureEssentialDirs failed: %v", err)
	}

	// 3. Verify 'models' and 'models_cache' exist
	expected := []string{"models", "models_cache"}
	for _, dir := range expected {
		path := filepath.Join(tmpDir, dir)
		info, err := os.Stat(path)
		if os.IsNotExist(err) {
			t.Errorf("Directory %s was not created", dir)
		}
		if err == nil && !info.IsDir() {
			t.Errorf("%s exists but is not a directory", dir)
		}
	}
}

// TestStackDirCleanupLogic verifies that we protect specific files during cleanup
// This acts as a unit test for the logic inside ensureStackDir without making network calls.
func TestStackDirCleanupLogic(t *testing.T) {
	tmpDir := t.TempDir()

	// 1. Setup Dummy Environment
	// File that SHOULD be deleted (simulating old code)
	os.WriteFile(filepath.Join(tmpDir, "old_service.go"), []byte("package main"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "podman-compose.yml"), []byte("services:"), 0644)

	// Files that SHOULD be preserved (User Data)
	os.WriteFile(filepath.Join(tmpDir, "podman-compose.override.yml"), []byte("services:"), 0644)
	os.Mkdir(filepath.Join(tmpDir, "models"), 0755)
	os.WriteFile(filepath.Join(tmpDir, "models", "dummy.gguf"), []byte("data"), 0644)

	// 2. Run the Cleanup Logic
	// (Replicating the exact loop from helpers.go to test the logic in isolation)
	dirEntries, _ := os.ReadDir(tmpDir)
	for _, entry := range dirEntries {
		name := entry.Name()
		// THE LOGIC BEING TESTED:
		if name == "models" || name == "models_cache" || name == "podman-compose.override.yml" {
			continue // Skip deletion
		}
		entryPath := filepath.Join(tmpDir, name)
		os.RemoveAll(entryPath)
	}

	// 3. Verify Results

	// Deleted?
	if _, err := os.Stat(filepath.Join(tmpDir, "old_service.go")); !os.IsNotExist(err) {
		t.Error("old_service.go should have been deleted")
	}
	if _, err := os.Stat(filepath.Join(tmpDir, "podman-compose.yml")); !os.IsNotExist(err) {
		t.Error("podman-compose.yml should have been deleted (to be re-downloaded)")
	}

	// Preserved?
	if _, err := os.Stat(filepath.Join(tmpDir, "podman-compose.override.yml")); os.IsNotExist(err) {
		t.Error("podman-compose.override.yml should have been preserved")
	}
	if _, err := os.Stat(filepath.Join(tmpDir, "models", "dummy.gguf")); os.IsNotExist(err) {
		t.Error("models directory content should have been preserved")
	}
}
