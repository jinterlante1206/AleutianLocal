// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestStackInfrastructure validates that the v0.3.4 self-healing features
// resulted in a healthy, usable stack.
func TestStackInfrastructure(t *testing.T) {
	// 1. Build CLI
	tmpBin := "./aleutian_test_bin"
	buildCmd := exec.Command("go", "build", "-o", tmpBin,
		"../../cmd/aleutian") // Adjust path as needed
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to build CLI: %v\n%s", err, string(out))
	}
	defer os.Remove(tmpBin)

	// 2. Verify "Status" Command (Connectivity Check)
	t.Log("Running 'aleutian stack status'...")
	statusCmd := exec.Command(tmpBin, "stack", "status")
	out, err := statusCmd.CombinedOutput()
	output := string(out)

	if err != nil {
		t.Fatalf("Stack status command failed: %v", err)
	}

	// 3. Assertions: Check for "Healthy" markers
	// If the volume mounts failed, these would show "Unreachable" or "HTTP 500"
	requiredServices := []string{"Orchestrator", "Weaviate"}
	for _, svc := range requiredServices {
		if !strings.Contains(output, fmt.Sprintf("%s: ✅ Healthy", svc)) {
			t.Errorf("FAIL: Service %s is not healthy. Output:\n%s", svc, output)
		}
	}

	// 4. Verify Volume Mount (The "Statfs" Fix)
	// We check if the 'models_cache' directory exists on the HOST.
	// If the stack started successfully, this directory must exist.
	cwd, _ := os.Getwd()
	// We're in test/release/, go up TWO levels to reach project root
	projectRoot := filepath.Join(cwd, "../..")
	cachePath := filepath.Join(projectRoot, "models_cache")

	if _, err := os.Stat(cachePath); os.IsNotExist(err) {
		// Note: If using external drive, check env var, otherwise default
		if os.Getenv("ALEUTIAN_MODELS_CACHE") == "" {
			t.Errorf("FAIL: Default models_cache directory was not created at %s", cachePath)
		}
	} else {
		t.Logf("SUCCESS: Models cache directory exists at %s", cachePath)
	}
}
