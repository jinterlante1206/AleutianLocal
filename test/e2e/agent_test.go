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

// TestAgentTrace_HappyPath verifies the agent can list and read files safely.
// Matches Checklist 2.1: "The Happy Path"
func TestAgentTrace_HappyPath(t *testing.T) {
	// 1. Setup
	tempDir := t.TempDir()
	targetFile := filepath.Join(tempDir, "readme.md")
	secretMsg := fmt.Sprintf("ALEUTIAN_SECRET_LICENSE_%d", time.Now().Unix())
	os.WriteFile(targetFile, []byte(secretMsg), 0644)

	// 2. Execute Trace
	// We give it a direct instruction to read the file we just made
	absPath, _ := filepath.Abs(targetFile)
	fmt.Println("absolute path is: ", absPath)
	prompt := fmt.Sprintf("Read the file at this exact absolute path: '%s'. Do not use a relative path.", absPath)
	cmd := exec.Command(cliBinary, "trace", prompt)

	// Timeout safety
	timer := time.AfterFunc(60*time.Second, func() {
		if cmd.Process != nil {
			cmd.Process.Kill()
		}
	})
	defer timer.Stop()

	outBytes, err := cmd.CombinedOutput()
	output := string(outBytes)

	if err != nil {
		t.Fatalf("Agent Trace failed: %v\nOutput: %s", err, output)
	}

	// 3. Assertions
	// Check if CLI executed the tool
	if !strings.Contains(output, "Agent requests: read_file") {
		t.Error("FAIL: CLI did not report tool execution.")
	}

	// Check if Agent saw the content
	// The LLM should repeat the content or summarize it
	if !strings.Contains(output, secretMsg) {
		t.Errorf("FAIL: Agent answer did not contain file content.\nOutput: %s", output)
	} else {
		t.Log("✅ Agent Trace Happy Path Passed")
	}
}
