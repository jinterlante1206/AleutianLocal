package e2e

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

var cliBinary string

func TestMain(m *testing.M) {
	// 1. Build the binary
	cwd, _ := os.Getwd()
	cliBinary = filepath.Join(cwd, "aleutian_e2e")

	// Assuming running from test/e2e/, go up to root
	cmd := exec.Command("go", "build", "-o", cliBinary, "../../cmd/aleutian")
	if out, err := cmd.CombinedOutput(); err != nil {
		fmt.Printf("Failed to build CLI: %v\n%s\n", err, out)
		os.Exit(1)
	}

	// 2. Run Tests
	exitCode := m.Run()

	// 3. Cleanup
	os.Remove(cliBinary)
	os.Exit(exitCode)
}
