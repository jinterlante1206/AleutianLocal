// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package config

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestCreateDefault verifies default config creation.
func TestCreateDefault(t *testing.T) {
	// Create a temp directory
	tempDir, err := os.MkdirTemp("", "aleutian-config-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	configPath := filepath.Join(tempDir, ".aleutian", "aleutian.yaml")

	// Create the config
	err = createDefault(configPath)
	if err != nil {
		t.Fatalf("createDefault() failed: %v", err)
	}

	// Verify the file exists
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Fatal("config file was not created")
	}

	// Read and verify the config
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read config file: %v", err)
	}

	var cfg AleutianConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("failed to parse config: %v", err)
	}

	// Verify some defaults
	if cfg.ModelBackend.Type != "ollama" {
		t.Errorf("ModelBackend.Type = %q, want %q", cfg.ModelBackend.Type, "ollama")
	}
	if cfg.Meta.Version != CurrentConfigVersion {
		t.Errorf("Meta.Version = %q, want %q", cfg.Meta.Version, CurrentConfigVersion)
	}
}

// TestCreateDefault_DirectoryCreation verifies directory is created.
func TestCreateDefault_DirectoryCreation(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "aleutian-config-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Use a nested path
	configPath := filepath.Join(tempDir, "deep", "nested", "path", "aleutian.yaml")

	err = createDefault(configPath)
	if err != nil {
		t.Fatalf("createDefault() failed with nested path: %v", err)
	}

	// Verify the directories were created
	dirPath := filepath.Dir(configPath)
	if _, err := os.Stat(dirPath); os.IsNotExist(err) {
		t.Fatal("nested directories were not created")
	}
}

// TestFindExternalDrives_EmptyDir verifies empty volume directory handling.
func TestFindExternalDrives(t *testing.T) {
	// This test verifies the function doesn't panic and returns expected results
	// On test systems, /Volumes may or may not exist
	result := findExternalDrives()

	// Result should be a slice (possibly empty)
	if result == nil {
		t.Error("findExternalDrives() returned nil")
	}
}

// TestBuildDefaultDrives verifies default drive configuration.
func TestBuildDefaultDrives(t *testing.T) {
	drives := buildDefaultDrives()

	// Should have at least one default drive (the home directory)
	if len(drives) == 0 {
		t.Skip("No default drives found, skipping test")
	}

	// Verify first drive is the home directory
	home, _ := os.UserHomeDir()
	if drives[0] != home {
		t.Errorf("First drive should be home directory, got %q", drives[0])
	}
}
