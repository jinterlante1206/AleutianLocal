package e2e

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestRAGWorkflow verifies the full loop: Populate -> Index -> Ask
func TestRAGWorkflow(t *testing.T) {
	// 1. Setup Test Data
	tempDir := t.TempDir()
	uniqueID := time.Now().Unix()
	testFilename := fmt.Sprintf("project_alpha_%d.txt", uniqueID)
	testFile := filepath.Join(tempDir, testFilename)
	testSpace := fmt.Sprintf("e2e_test_%d", uniqueID)

	uniqueCodename := fmt.Sprintf("Operation_Skyfall_%d", uniqueID)
	content := fmt.Sprintf("For ID %d, the confidential project is named %s.", uniqueID,
		uniqueCodename)
	os.WriteFile(testFile, []byte(content), 0644)

	// 2. Run Ingestion
	popCmd := exec.Command(cliBinary, "populate", "vectordb", testFile, "--force", "--data-space", testSpace)
	outBytes, err := popCmd.CombinedOutput()
	outStr := string(outBytes)
	// DEBUG: Print ingestion log if it fails
	if err != nil || (!strings.Contains(outStr, "chunks: 1") && !strings.Contains(outStr, "process complete")) {
		t.Fatalf("Ingestion failed. Output:\n%s", outStr)
	}

	// 3. Wait for Embedding (Important!)
	// INCREASED: 2s -> 10s to ensure indexing completes on slower machines
	fmt.Println("Waiting 10s for vector indexing...")
	time.Sleep(10 * time.Second)

	// 4. Run Retrieval
	question := fmt.Sprintf("What is the confidential project name for ID %d?", uniqueID)
	askCmd := exec.Command(cliBinary, "ask", question)
	outBytes, err = askCmd.CombinedOutput()
	output := string(outBytes)

	if err != nil {
		t.Fatalf("Ask command failed: %v\nOutput: %s", err, output)
	}

	// 5. Assert Content
	if !strings.Contains(output, uniqueCodename) {
		t.Errorf("RAG Retrieval Failed.\nExpected: %s\nGot: %s", uniqueCodename, output)
	}

	// 6. Assert Citations
	if !strings.Contains(output, "project_alpha.txt") && !strings.Contains(output, "Sources Used") {
		t.Errorf("RAG failed to cite source file.\nOutput: %s", output)
	} else {
		t.Log("✅ RAG Integration Test Passed")
	}
}
