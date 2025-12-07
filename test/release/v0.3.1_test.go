package test

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// TestAgentTraceE2E builds the CLI and runs a real trace command.
// Requires the Aleutian Stack to be running (`aleutian stack start`).
func TestAgentTraceE2E(t *testing.T) {
	// 1. Build the latest CLI binary
	// We build it to a temp location to avoid messing with the user's install
	tmpBin := "./aleutian_test_bin"
	buildCmd := exec.Command("go", "build", "-o", tmpBin, "../../cmd/aleutian")
	if output, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to build CLI: %v\nOutput: %s", err, string(output))
	}
	defer os.Remove(tmpBin) // Cleanup binary

	// 2. Create a dummy target file
	targetFile := "test_trace_target.txt"
	secretCode := "BLUE_HORIZON"
	content := fmt.Sprintf("The secret code is %s.", secretCode)
	if err := os.WriteFile(targetFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}
	defer os.Remove(targetFile) // Cleanup file

	// 3. Run the Trace Command
	prompt := fmt.Sprintf("Read the file %s and tell me the secret code.", targetFile)
	cmd := exec.Command(tmpBin, "trace", prompt)

	// Set a timeout to prevent hanging if the loop breaks
	// (We can't easily use CommandContext with a timeout that kills the *process group* robustly in tests without complexity,
	// so standard timeout logic or manual kill is used. For simplicity in test:)
	timer := time.AfterFunc(60*time.Second, func() {
		cmd.Process.Kill()
	})

	outputBytes, err := cmd.CombinedOutput()
	timer.Stop()

	output := string(outputBytes)
	t.Logf("Agent Output:\n%s", output)

	if err != nil {
		t.Fatalf("CLI command failed: %v", err)
	}

	// 4. Assertions
	// Did it trigger the tool?
	if !strings.Contains(output, "Agent requests: read_file") {
		t.Error("FAIL: Agent did not trigger 'read_file' tool execution.")
	}

	// Did it find the answer?
	if !strings.Contains(output, secretCode) {
		t.Errorf("FAIL: Agent did not find the secret code '%s'.", secretCode)
	} else {
		t.Log("SUCCESS: Agent found the secret code.")
	}
}
