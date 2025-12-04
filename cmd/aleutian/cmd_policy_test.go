package main

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// Maps to the "Minor Bug Fix" verification.
// We capture Stdout to ensure formatting (%d, %x) is actually applied.
func TestVerifyPolicyOutputFormat(t *testing.T) {
	// 1. Pipe stdout to a buffer
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// 2. Run the command function directly
	// Note: We pass a dummy command and args since verifyPolicies ignores them
	dummyCmd := &cobra.Command{}
	verifyPolicies(dummyCmd, []string{})

	// 3. Restore stdout and read the buffer
	w.Close()
	os.Stdout = oldStdout
	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()

	// 4. Validate output
	if strings.Contains(output, "%d") {
		t.Errorf("Bug Regression: Found literal '%%d' in output. Use fmt.Printf, not Println.")
	}
	if strings.Contains(output, "%x") {
		t.Errorf("Bug Regression: Found literal '%%x' in output. Use fmt.Printf, not Println.")
	}
	if !strings.Contains(output, "SHA256 Fingerprint:") {
		t.Errorf("Unexpected output format: %s", output)
	}
	t.Log("Output formatting verified")
}
