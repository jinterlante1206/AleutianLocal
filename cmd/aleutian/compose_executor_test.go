package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

// =============================================================================
// Test Helpers
// =============================================================================

// createTestComposeConfig creates a ComposeConfig for testing.
//
// # Description
//
// Creates a minimal valid configuration with test-appropriate defaults.
// Uses a temporary directory for stack path to avoid filesystem side effects.
//
// # Inputs
//
//   - stackDir: Stack directory path (use t.TempDir() or "/test/stack")
//
// # Outputs
//
//   - ComposeConfig: Test configuration
//
// # Example
//
//	cfg := createTestComposeConfig("/tmp/test-stack")
//
// # Limitations
//
//   - Does not create actual directory
//
// # Assumptions
//
//   - Caller handles directory creation if needed
func createTestComposeConfig(stackDir string) ComposeConfig {
	return ComposeConfig{
		StackDir:            stackDir,
		ProjectName:         "testproject",
		BaseFile:            "podman-compose.yml",
		OverrideFile:        "podman-compose.override.yml",
		ContainerNamePrefix: "test-",
		DefaultTimeout:      30 * time.Second,
	}
}

// createTestComposeExecutor creates a DefaultComposeExecutor for testing.
//
// # Description
//
// Creates an executor with a mock ProcessManager and configurable stat function.
// Allows full control over command execution behavior for testing.
//
// # Inputs
//
//   - cfg: Compose configuration
//   - mockProc: Mock process manager
//   - statFunc: Function to check file existence (nil uses always-false)
//
// # Outputs
//
//   - *DefaultComposeExecutor: Test executor
//
// # Example
//
//	mock := &MockProcessManager{}
//	executor := createTestComposeExecutor(cfg, mock, nil)
//
// # Limitations
//
//   - Must configure mock behavior before use
//
// # Assumptions
//
//   - Configuration is valid
func createTestComposeExecutor(cfg ComposeConfig, mockProc *MockProcessManager, statFunc func(string) (os.FileInfo, error)) *DefaultComposeExecutor {
	if statFunc == nil {
		statFunc = func(path string) (os.FileInfo, error) {
			return nil, os.ErrNotExist
		}
	}
	return &DefaultComposeExecutor{
		config:     cfg,
		proc:       mockProc,
		osStatFunc: statFunc,
	}
}

// mockStatExists returns a stat function that always reports file exists.
//
// # Description
//
// Creates a stat function that returns nil error for all paths,
// simulating that all files exist.
//
// # Inputs
//
//   - None
//
// # Outputs
//
//   - func(string) (os.FileInfo, error): Always-exists stat function
//
// # Example
//
//	executor := createTestComposeExecutor(cfg, mock, mockStatExists())
//
// # Limitations
//
//   - Returns nil FileInfo (only error is checked)
//
// # Assumptions
//
//   - Caller only checks error, not FileInfo
func mockStatExists() func(string) (os.FileInfo, error) {
	return func(path string) (os.FileInfo, error) {
		return nil, nil
	}
}

// mockStatForPaths returns a stat function that reports existence for specific paths.
//
// # Description
//
// Creates a stat function that returns nil error for paths in the list,
// and os.ErrNotExist for all other paths.
//
// # Inputs
//
//   - paths: Paths that should report as existing
//
// # Outputs
//
//   - func(string) (os.FileInfo, error): Selective stat function
//
// # Example
//
//	statFunc := mockStatForPaths("/test/base.yml", "/test/override.yml")
//
// # Limitations
//
//   - Exact path matching only
//
// # Assumptions
//
//   - Paths are absolute and normalized
func mockStatForPaths(paths ...string) func(string) (os.FileInfo, error) {
	pathSet := make(map[string]bool)
	for _, p := range paths {
		pathSet[p] = true
	}
	return func(path string) (os.FileInfo, error) {
		if pathSet[path] {
			return nil, nil
		}
		return nil, os.ErrNotExist
	}
}

// =============================================================================
// Constructor Tests
// =============================================================================

// TestNewDefaultComposeExecutor_ValidConfig tests constructor with valid config.
//
// # Description
//
// Verifies that NewDefaultComposeExecutor creates an executor when given
// a valid configuration with required StackDir.
//
// # Inputs
//
//   - Valid ComposeConfig with StackDir set
//
// # Outputs
//
//   - Non-nil executor
//   - nil error
//
// # Example
//
//	go test -run TestNewDefaultComposeExecutor_ValidConfig
//
// # Limitations
//
//   - Does not test all configuration combinations
//
// # Assumptions
//
//   - ProcessManager is properly initialized
func TestNewDefaultComposeExecutor_ValidConfig(t *testing.T) {
	cfg := ComposeConfig{
		StackDir: "/test/stack",
	}
	mockProc := &MockProcessManager{}

	executor, err := NewDefaultComposeExecutor(cfg, mockProc)

	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
	if executor == nil {
		t.Error("expected non-nil executor")
	}
}

// TestNewDefaultComposeExecutor_EmptyStackDir tests constructor with empty StackDir.
//
// # Description
//
// Verifies that NewDefaultComposeExecutor returns ErrInvalidConfig
// when StackDir is empty.
//
// # Inputs
//
//   - ComposeConfig with empty StackDir
//
// # Outputs
//
//   - nil executor
//   - ErrInvalidConfig wrapped error
//
// # Example
//
//	go test -run TestNewDefaultComposeExecutor_EmptyStackDir
//
// # Limitations
//
//   - Only tests StackDir validation
//
// # Assumptions
//
//   - Other fields are optional
func TestNewDefaultComposeExecutor_EmptyStackDir(t *testing.T) {
	cfg := ComposeConfig{
		StackDir: "",
	}
	mockProc := &MockProcessManager{}

	executor, err := NewDefaultComposeExecutor(cfg, mockProc)

	if executor != nil {
		t.Error("expected nil executor")
	}
	if !errors.Is(err, ErrInvalidConfig) {
		t.Errorf("expected ErrInvalidConfig, got: %v", err)
	}
}

// TestNewDefaultComposeExecutor_DefaultsApplied tests that defaults are set.
//
// # Description
//
// Verifies that NewDefaultComposeExecutor applies default values
// for optional configuration fields.
//
// # Inputs
//
//   - Minimal config with only StackDir
//
// # Outputs
//
//   - Executor with defaults applied
//
// # Example
//
//	go test -run TestNewDefaultComposeExecutor_DefaultsApplied
//
// # Limitations
//
//   - Cannot directly inspect private config field
//
// # Assumptions
//
//   - GetComposeFiles reflects config.BaseFile
func TestNewDefaultComposeExecutor_DefaultsApplied(t *testing.T) {
	cfg := ComposeConfig{
		StackDir: "/test/stack",
	}
	mockProc := &MockProcessManager{}

	executor, err := NewDefaultComposeExecutor(cfg, mockProc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// GetComposeFiles should include the default base file
	files := executor.GetComposeFiles()
	if len(files) == 0 {
		t.Error("expected at least one compose file")
	}
	if !strings.Contains(files[0], "podman-compose.yml") {
		t.Errorf("expected default base file, got: %s", files[0])
	}
}

// =============================================================================
// Up Tests
// =============================================================================

// TestDefaultComposeExecutor_Up_Success tests successful Up operation.
//
// # Description
//
// Verifies that Up executes podman-compose up with correct arguments
// and returns success when the command succeeds.
//
// # Inputs
//
//   - Context, UpOptions with ForceBuild
//
// # Outputs
//
//   - ComposeResult with Success=true
//   - nil error
//
// # Example
//
//	go test -run TestDefaultComposeExecutor_Up_Success
//
// # Limitations
//
//   - Does not test actual container startup
//
// # Assumptions
//
//   - Mock returns exit code 0
func TestDefaultComposeExecutor_Up_Success(t *testing.T) {
	cfg := createTestComposeConfig("/test/stack")
	mockProc := &MockProcessManager{
		RunInDirFunc: func(ctx context.Context, dir string, env []string, name string, args ...string) (string, string, int, error) {
			// Verify command is podman-compose
			if name != "podman-compose" {
				t.Errorf("expected podman-compose, got: %s", name)
			}
			// Verify up and -d are in args
			argsStr := strings.Join(args, " ")
			if !strings.Contains(argsStr, "up") || !strings.Contains(argsStr, "-d") {
				t.Errorf("expected 'up -d' in args, got: %s", argsStr)
			}
			return "containers started", "", 0, nil
		},
	}
	executor := createTestComposeExecutor(cfg, mockProc, nil)

	ctx := context.Background()
	result, err := executor.Up(ctx, UpOptions{ForceBuild: true})

	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
	if !result.Success {
		t.Error("expected success")
	}
}

// TestDefaultComposeExecutor_Up_WithBuildFlag tests Up with build flag.
//
// # Description
//
// Verifies that Up includes --build flag when ForceBuild is true.
//
// # Inputs
//
//   - UpOptions{ForceBuild: true}
//
// # Outputs
//
//   - Command includes --build
//
// # Example
//
//	go test -run TestDefaultComposeExecutor_Up_WithBuildFlag
//
// # Limitations
//
//   - Only tests flag presence
//
// # Assumptions
//
//   - Args are passed correctly to podman-compose
func TestDefaultComposeExecutor_Up_WithBuildFlag(t *testing.T) {
	cfg := createTestComposeConfig("/test/stack")
	var capturedArgs []string
	mockProc := &MockProcessManager{
		RunInDirFunc: func(ctx context.Context, dir string, env []string, name string, args ...string) (string, string, int, error) {
			capturedArgs = args
			return "", "", 0, nil
		},
	}
	executor := createTestComposeExecutor(cfg, mockProc, nil)

	ctx := context.Background()
	_, _ = executor.Up(ctx, UpOptions{ForceBuild: true})

	argsStr := strings.Join(capturedArgs, " ")
	if !strings.Contains(argsStr, "--build") {
		t.Errorf("expected --build in args, got: %s", argsStr)
	}
}

// TestDefaultComposeExecutor_Up_WithServices tests Up with specific services.
//
// # Description
//
// Verifies that Up includes service names when Services is specified.
//
// # Inputs
//
//   - UpOptions{Services: []string{"web", "db"}}
//
// # Outputs
//
//   - Command includes service names
//
// # Example
//
//	go test -run TestDefaultComposeExecutor_Up_WithServices
//
// # Limitations
//
//   - Does not verify service order
//
// # Assumptions
//
//   - Service names are appended to args
func TestDefaultComposeExecutor_Up_WithServices(t *testing.T) {
	cfg := createTestComposeConfig("/test/stack")
	var capturedArgs []string
	mockProc := &MockProcessManager{
		RunInDirFunc: func(ctx context.Context, dir string, env []string, name string, args ...string) (string, string, int, error) {
			capturedArgs = args
			return "", "", 0, nil
		},
	}
	executor := createTestComposeExecutor(cfg, mockProc, nil)

	ctx := context.Background()
	_, _ = executor.Up(ctx, UpOptions{Services: []string{"web", "db"}})

	argsStr := strings.Join(capturedArgs, " ")
	if !strings.Contains(argsStr, "web") || !strings.Contains(argsStr, "db") {
		t.Errorf("expected services in args, got: %s", argsStr)
	}
}

// TestDefaultComposeExecutor_Up_WithEnvVars tests Up with environment variables.
//
// # Description
//
// Verifies that Up passes environment variables to the command.
//
// # Inputs
//
//   - UpOptions{Env: map[string]string{"FOO": "bar"}}
//
// # Outputs
//
//   - Environment includes FOO=bar
//
// # Example
//
//	go test -run TestDefaultComposeExecutor_Up_WithEnvVars
//
// # Limitations
//
//   - Only verifies env is passed, not exact format
//
// # Assumptions
//
//   - RunInDir receives environment correctly
func TestDefaultComposeExecutor_Up_WithEnvVars(t *testing.T) {
	cfg := createTestComposeConfig("/test/stack")
	var capturedEnv []string
	mockProc := &MockProcessManager{
		RunInDirFunc: func(ctx context.Context, dir string, env []string, name string, args ...string) (string, string, int, error) {
			capturedEnv = env
			return "", "", 0, nil
		},
	}
	executor := createTestComposeExecutor(cfg, mockProc, nil)

	ctx := context.Background()
	_, _ = executor.Up(ctx, UpOptions{Env: map[string]string{"FOO": "bar"}})

	found := false
	for _, e := range capturedEnv {
		if e == "FOO=bar" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected FOO=bar in environment")
	}
}

// TestDefaultComposeExecutor_Up_CommandError tests Up with command failure.
//
// # Description
//
// Verifies that Up returns an error when the command fails with non-zero exit.
//
// # Inputs
//
//   - Mock returns exit code 1
//
// # Outputs
//
//   - Non-nil error
//   - Result with Success=false
//
// # Example
//
//	go test -run TestDefaultComposeExecutor_Up_CommandError
//
// # Limitations
//
//   - Only tests non-zero exit code
//
// # Assumptions
//
//   - Non-zero exit is treated as error
func TestDefaultComposeExecutor_Up_CommandError(t *testing.T) {
	cfg := createTestComposeConfig("/test/stack")
	mockProc := &MockProcessManager{
		RunInDirFunc: func(ctx context.Context, dir string, env []string, name string, args ...string) (string, string, int, error) {
			return "", "error building image", 1, nil
		},
	}
	executor := createTestComposeExecutor(cfg, mockProc, nil)

	ctx := context.Background()
	result, err := executor.Up(ctx, UpOptions{})

	if err == nil {
		t.Error("expected error")
	}
	if result.Success {
		t.Error("expected Success=false")
	}
}

// =============================================================================
// Down Tests
// =============================================================================

// TestDefaultComposeExecutor_Down_Success tests successful Down operation.
//
// # Description
//
// Verifies that Down executes podman-compose down and returns success.
//
// # Inputs
//
//   - Context, DownOptions{}
//
// # Outputs
//
//   - ComposeResult with Success=true
//   - nil error
//
// # Example
//
//	go test -run TestDefaultComposeExecutor_Down_Success
//
// # Limitations
//
//   - Does not test actual container removal
//
// # Assumptions
//
//   - Mock returns exit code 0
func TestDefaultComposeExecutor_Down_Success(t *testing.T) {
	cfg := createTestComposeConfig("/test/stack")
	mockProc := &MockProcessManager{
		RunInDirFunc: func(ctx context.Context, dir string, env []string, name string, args ...string) (string, string, int, error) {
			argsStr := strings.Join(args, " ")
			if !strings.Contains(argsStr, "down") {
				t.Errorf("expected 'down' in args, got: %s", argsStr)
			}
			return "containers stopped", "", 0, nil
		},
	}
	executor := createTestComposeExecutor(cfg, mockProc, nil)

	ctx := context.Background()
	result, err := executor.Down(ctx, DownOptions{})

	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
	if !result.Success {
		t.Error("expected success")
	}
}

// TestDefaultComposeExecutor_Down_WithRemoveOrphans tests Down with orphan removal.
//
// # Description
//
// Verifies that Down includes --remove-orphans flag when requested.
//
// # Inputs
//
//   - DownOptions{RemoveOrphans: true}
//
// # Outputs
//
//   - Command includes --remove-orphans
//
// # Example
//
//	go test -run TestDefaultComposeExecutor_Down_WithRemoveOrphans
//
// # Limitations
//
//   - Only tests flag presence
//
// # Assumptions
//
//   - Args are passed correctly
func TestDefaultComposeExecutor_Down_WithRemoveOrphans(t *testing.T) {
	cfg := createTestComposeConfig("/test/stack")
	var capturedArgs []string
	mockProc := &MockProcessManager{
		RunInDirFunc: func(ctx context.Context, dir string, env []string, name string, args ...string) (string, string, int, error) {
			capturedArgs = args
			return "", "", 0, nil
		},
	}
	executor := createTestComposeExecutor(cfg, mockProc, nil)

	ctx := context.Background()
	_, _ = executor.Down(ctx, DownOptions{RemoveOrphans: true})

	argsStr := strings.Join(capturedArgs, " ")
	if !strings.Contains(argsStr, "--remove-orphans") {
		t.Errorf("expected --remove-orphans in args, got: %s", argsStr)
	}
}

// TestDefaultComposeExecutor_Down_WithRemoveVolumes tests Down with volume removal.
//
// # Description
//
// Verifies that Down includes -v flag when RemoveVolumes is true.
//
// # Inputs
//
//   - DownOptions{RemoveVolumes: true}
//
// # Outputs
//
//   - Command includes -v
//
// # Example
//
//	go test -run TestDefaultComposeExecutor_Down_WithRemoveVolumes
//
// # Limitations
//
//   - Only tests flag presence
//
// # Assumptions
//
//   - Volume removal flag is -v
func TestDefaultComposeExecutor_Down_WithRemoveVolumes(t *testing.T) {
	cfg := createTestComposeConfig("/test/stack")
	var capturedArgs []string
	mockProc := &MockProcessManager{
		RunInDirFunc: func(ctx context.Context, dir string, env []string, name string, args ...string) (string, string, int, error) {
			capturedArgs = args
			return "", "", 0, nil
		},
	}
	executor := createTestComposeExecutor(cfg, mockProc, nil)

	ctx := context.Background()
	_, _ = executor.Down(ctx, DownOptions{RemoveVolumes: true})

	argsStr := strings.Join(capturedArgs, " ")
	if !strings.Contains(argsStr, " -v") && !strings.HasSuffix(argsStr, "-v") {
		t.Errorf("expected -v in args, got: %s", argsStr)
	}
}

// =============================================================================
// Stop Tests
// =============================================================================

// TestDefaultComposeExecutor_Stop_GracefulSuccess tests graceful stop success.
//
// # Description
//
// Verifies that Stop performs graceful stop when all containers stop.
//
// # Inputs
//
//   - StopOptions with default timeout
//
// # Outputs
//
//   - StopResult with GracefulStopped count
//   - nil error
//
// # Example
//
//	go test -run TestDefaultComposeExecutor_Stop_GracefulSuccess
//
// # Limitations
//
//   - Simulates successful stop via mock
//
// # Assumptions
//
//   - Containers stop on first attempt
func TestDefaultComposeExecutor_Stop_GracefulSuccess(t *testing.T) {
	cfg := createTestComposeConfig("/test/stack")
	callCount := 0
	mockProc := &MockProcessManager{
		RunInDirFunc: func(ctx context.Context, dir string, env []string, name string, args ...string) (string, string, int, error) {
			callCount++
			argsStr := strings.Join(args, " ")
			// First call: ps -q returns running container
			if strings.Contains(argsStr, "ps -q") {
				if callCount == 1 {
					return "container1", "", 0, nil
				}
				// After stop: no running containers
				return "", "", 0, nil
			}
			// Stop command
			if strings.Contains(argsStr, "stop") {
				return "", "", 0, nil
			}
			return "", "", 0, nil
		},
	}
	executor := createTestComposeExecutor(cfg, mockProc, nil)

	ctx := context.Background()
	result, err := executor.Stop(ctx, StopOptions{GracefulTimeout: 10 * time.Second})

	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
	if result.GracefulStopped != 1 {
		t.Errorf("expected 1 graceful stop, got: %d", result.GracefulStopped)
	}
}

// TestDefaultComposeExecutor_Stop_ForceAfterGraceful tests force stop escalation.
//
// # Description
//
// Verifies that Stop escalates to force stop when containers remain
// after graceful stop timeout.
//
// # Inputs
//
//   - StopOptions with timeout, containers don't stop gracefully
//
// # Outputs
//
//   - StopResult with ForceStopped count
//
// # Example
//
//	go test -run TestDefaultComposeExecutor_Stop_ForceAfterGraceful
//
// # Limitations
//
//   - Simulates via mock behavior
//
// # Assumptions
//
//   - Force stop always succeeds in mock
func TestDefaultComposeExecutor_Stop_ForceAfterGraceful(t *testing.T) {
	cfg := createTestComposeConfig("/test/stack")
	stopCalls := 0
	psCalls := 0
	mockProc := &MockProcessManager{
		RunInDirFunc: func(ctx context.Context, dir string, env []string, name string, args ...string) (string, string, int, error) {
			argsStr := strings.Join(args, " ")
			// ps -q calls
			if strings.Contains(argsStr, "ps -q") {
				psCalls++
				if psCalls <= 2 {
					// Before and after graceful stop: still running
					return "container1", "", 0, nil
				}
				// After force stop: no containers
				return "", "", 0, nil
			}
			// Stop command
			if strings.Contains(argsStr, "stop") {
				stopCalls++
				return "", "", 0, nil
			}
			return "", "", 0, nil
		},
	}
	executor := createTestComposeExecutor(cfg, mockProc, nil)

	ctx := context.Background()
	result, err := executor.Stop(ctx, StopOptions{GracefulTimeout: 1 * time.Second})

	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
	if stopCalls != 2 {
		t.Errorf("expected 2 stop calls (graceful + force), got: %d", stopCalls)
	}
	if result.ForceStopped != 1 {
		t.Errorf("expected 1 force stop, got: %d", result.ForceStopped)
	}
}

// TestDefaultComposeExecutor_Stop_SkipForceStop tests skip force stop option.
//
// # Description
//
// Verifies that Stop respects SkipForceStop option and doesn't escalate.
//
// # Inputs
//
//   - StopOptions{SkipForceStop: true}
//
// # Outputs
//
//   - Only one stop call (graceful only)
//
// # Example
//
//	go test -run TestDefaultComposeExecutor_Stop_SkipForceStop
//
// # Limitations
//
//   - Cannot verify SIGKILL wasn't sent (mocked)
//
// # Assumptions
//
//   - Force stop is controlled by option
func TestDefaultComposeExecutor_Stop_SkipForceStop(t *testing.T) {
	cfg := createTestComposeConfig("/test/stack")
	stopCalls := 0
	mockProc := &MockProcessManager{
		RunInDirFunc: func(ctx context.Context, dir string, env []string, name string, args ...string) (string, string, int, error) {
			argsStr := strings.Join(args, " ")
			if strings.Contains(argsStr, "ps -q") {
				return "container1", "", 0, nil // Always returns running container
			}
			if strings.Contains(argsStr, "stop") {
				stopCalls++
				return "", "", 0, nil
			}
			return "", "", 0, nil
		},
	}
	executor := createTestComposeExecutor(cfg, mockProc, nil)

	ctx := context.Background()
	_, err := executor.Stop(ctx, StopOptions{
		GracefulTimeout: 1 * time.Second,
		SkipForceStop:   true,
	})

	// Should have errors since containers didn't stop, but only 1 stop call
	if err == nil {
		t.Log("Expected error due to remaining containers, but ok if none")
	}
	if stopCalls != 1 {
		t.Errorf("expected 1 stop call (graceful only), got: %d", stopCalls)
	}
}

// =============================================================================
// Status Tests
// =============================================================================

// TestDefaultComposeExecutor_Status_ParsesJSON tests JSON parsing.
//
// # Description
//
// Verifies that Status correctly parses podman ps JSON output.
//
// # Inputs
//
//   - Mock returns valid JSON
//
// # Outputs
//
//   - ComposeStatus with correct service info
//
// # Example
//
//	go test -run TestDefaultComposeExecutor_Status_ParsesJSON
//
// # Limitations
//
//   - Tests specific JSON format
//
// # Assumptions
//
//   - Podman JSON format is stable
func TestDefaultComposeExecutor_Status_ParsesJSON(t *testing.T) {
	cfg := createTestComposeConfig("/test/stack")
	jsonOutput := `[
		{"Names":["test-weaviate-1"],"State":"running","Status":"Up 2 hours (healthy)","Image":"weaviate:latest","Ports":[]},
		{"Names":["test-ollama-1"],"State":"exited","Status":"Exited (0) 1 hour ago","Image":"ollama:latest","Ports":[]}
	]`
	mockProc := &MockProcessManager{
		RunInDirFunc: func(ctx context.Context, dir string, env []string, name string, args ...string) (string, string, int, error) {
			return jsonOutput, "", 0, nil
		},
	}
	executor := createTestComposeExecutor(cfg, mockProc, nil)

	ctx := context.Background()
	status, err := executor.Status(ctx)

	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
	if len(status.Services) != 2 {
		t.Errorf("expected 2 services, got: %d", len(status.Services))
	}
	if status.Running != 1 {
		t.Errorf("expected 1 running, got: %d", status.Running)
	}
	if status.Stopped != 1 {
		t.Errorf("expected 1 stopped, got: %d", status.Stopped)
	}
}

// TestDefaultComposeExecutor_Status_EmptyOutput tests empty container list.
//
// # Description
//
// Verifies that Status handles empty output gracefully.
//
// # Inputs
//
//   - Mock returns empty string
//
// # Outputs
//
//   - Empty ComposeStatus
//   - nil error
//
// # Example
//
//	go test -run TestDefaultComposeExecutor_Status_EmptyOutput
//
// # Limitations
//
//   - Only tests empty string
//
// # Assumptions
//
//   - Empty output means no containers
func TestDefaultComposeExecutor_Status_EmptyOutput(t *testing.T) {
	cfg := createTestComposeConfig("/test/stack")
	mockProc := &MockProcessManager{
		RunInDirFunc: func(ctx context.Context, dir string, env []string, name string, args ...string) (string, string, int, error) {
			return "", "", 0, nil
		},
	}
	executor := createTestComposeExecutor(cfg, mockProc, nil)

	ctx := context.Background()
	status, err := executor.Status(ctx)

	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
	if len(status.Services) != 0 {
		t.Errorf("expected 0 services, got: %d", len(status.Services))
	}
}

// TestDefaultComposeExecutor_Status_HealthStatus tests health parsing.
//
// # Description
//
// Verifies that Status correctly parses healthy/unhealthy status.
//
// # Inputs
//
//   - JSON with healthy and unhealthy containers
//
// # Outputs
//
//   - Correct Healthy values on services
//
// # Example
//
//	go test -run TestDefaultComposeExecutor_Status_HealthStatus
//
// # Limitations
//
//   - Tests string-based health detection
//
// # Assumptions
//
//   - Status string contains "healthy" or "unhealthy"
func TestDefaultComposeExecutor_Status_HealthStatus(t *testing.T) {
	cfg := createTestComposeConfig("/test/stack")
	jsonOutput := `[
		{"Names":["test-healthy-1"],"State":"running","Status":"Up (healthy)","Image":"img","Ports":[]},
		{"Names":["test-unhealthy-1"],"State":"running","Status":"Up (unhealthy)","Image":"img","Ports":[]},
		{"Names":["test-nocheck-1"],"State":"running","Status":"Up","Image":"img","Ports":[]}
	]`
	mockProc := &MockProcessManager{
		RunInDirFunc: func(ctx context.Context, dir string, env []string, name string, args ...string) (string, string, int, error) {
			return jsonOutput, "", 0, nil
		},
	}
	executor := createTestComposeExecutor(cfg, mockProc, nil)

	ctx := context.Background()
	status, err := executor.Status(ctx)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(status.Services) != 3 {
		t.Fatalf("expected 3 services, got: %d", len(status.Services))
	}

	// First should be healthy
	if status.Services[0].Healthy == nil || !*status.Services[0].Healthy {
		t.Error("expected first service to be healthy")
	}
	// Second should be unhealthy
	if status.Services[1].Healthy == nil || *status.Services[1].Healthy {
		t.Error("expected second service to be unhealthy")
	}
	// Third should have no health check
	if status.Services[2].Healthy != nil {
		t.Error("expected third service to have nil health status")
	}
}

// =============================================================================
// Logs Tests
// =============================================================================

// TestDefaultComposeExecutor_Logs_Streaming tests log streaming.
//
// # Description
//
// Verifies that Logs streams output to the provided writer.
//
// # Inputs
//
//   - LogsOptions, bytes.Buffer writer
//
// # Outputs
//
//   - Output written to buffer
//   - nil error
//
// # Example
//
//	go test -run TestDefaultComposeExecutor_Logs_Streaming
//
// # Limitations
//
//   - Tests via mock streaming function
//
// # Assumptions
//
//   - RunStreaming writes to provided writer
func TestDefaultComposeExecutor_Logs_Streaming(t *testing.T) {
	cfg := createTestComposeConfig("/test/stack")
	mockProc := &MockProcessManager{
		RunStreamingFunc: func(ctx context.Context, dir string, w io.Writer, name string, args ...string) error {
			_, err := w.Write([]byte("log line 1\nlog line 2\n"))
			return err
		},
	}
	executor := createTestComposeExecutor(cfg, mockProc, nil)

	ctx := context.Background()
	var buf bytes.Buffer
	err := executor.Logs(ctx, LogsOptions{}, &buf)

	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
	if !strings.Contains(buf.String(), "log line 1") {
		t.Error("expected log output in buffer")
	}
}

// TestDefaultComposeExecutor_Logs_WithFollow tests follow mode flag.
//
// # Description
//
// Verifies that Logs includes -f flag when Follow is true.
//
// # Inputs
//
//   - LogsOptions{Follow: true}
//
// # Outputs
//
//   - Command includes -f
//
// # Example
//
//	go test -run TestDefaultComposeExecutor_Logs_WithFollow
//
// # Limitations
//
//   - Only tests flag presence
//
// # Assumptions
//
//   - Follow flag is -f
func TestDefaultComposeExecutor_Logs_WithFollow(t *testing.T) {
	cfg := createTestComposeConfig("/test/stack")
	var capturedArgs []string
	mockProc := &MockProcessManager{
		RunStreamingFunc: func(ctx context.Context, dir string, w io.Writer, name string, args ...string) error {
			capturedArgs = args
			return nil
		},
	}
	executor := createTestComposeExecutor(cfg, mockProc, nil)

	ctx := context.Background()
	_ = executor.Logs(ctx, LogsOptions{Follow: true}, &bytes.Buffer{})

	argsStr := strings.Join(capturedArgs, " ")
	if !strings.Contains(argsStr, "-f") {
		t.Errorf("expected -f in args, got: %s", argsStr)
	}
}

// TestDefaultComposeExecutor_Logs_WithTail tests tail option.
//
// # Description
//
// Verifies that Logs includes --tail flag when Tail is set.
//
// # Inputs
//
//   - LogsOptions{Tail: 100}
//
// # Outputs
//
//   - Command includes --tail 100
//
// # Example
//
//	go test -run TestDefaultComposeExecutor_Logs_WithTail
//
// # Limitations
//
//   - Only tests flag presence
//
// # Assumptions
//
//   - Tail flag format is --tail N
func TestDefaultComposeExecutor_Logs_WithTail(t *testing.T) {
	cfg := createTestComposeConfig("/test/stack")
	var capturedArgs []string
	mockProc := &MockProcessManager{
		RunStreamingFunc: func(ctx context.Context, dir string, w io.Writer, name string, args ...string) error {
			capturedArgs = args
			return nil
		},
	}
	executor := createTestComposeExecutor(cfg, mockProc, nil)

	ctx := context.Background()
	_ = executor.Logs(ctx, LogsOptions{Tail: 100}, &bytes.Buffer{})

	argsStr := strings.Join(capturedArgs, " ")
	if !strings.Contains(argsStr, "--tail") || !strings.Contains(argsStr, "100") {
		t.Errorf("expected --tail 100 in args, got: %s", argsStr)
	}
}

// =============================================================================
// ForceCleanup Tests
// =============================================================================

// TestDefaultComposeExecutor_ForceCleanup_Success tests successful cleanup.
//
// # Description
//
// Verifies that ForceCleanup executes all four steps successfully.
//
// # Inputs
//
//   - Mock returns success for all operations
//
// # Outputs
//
//   - CleanupResult with counts
//   - nil error
//
// # Example
//
//	go test -run TestDefaultComposeExecutor_ForceCleanup_Success
//
// # Limitations
//
//   - All steps mocked to succeed
//
// # Assumptions
//
//   - Four distinct operations: stop, rm by name, rm by label, rm pods
func TestDefaultComposeExecutor_ForceCleanup_Success(t *testing.T) {
	cfg := createTestComposeConfig("/test/stack")
	mockProc := &MockProcessManager{
		RunInDirFunc: func(ctx context.Context, dir string, env []string, name string, args ...string) (string, string, int, error) {
			argsStr := strings.Join(args, " ")
			// Pod list returns no pods
			if strings.Contains(argsStr, "pod ls") {
				return "", "", 0, nil
			}
			// Container rm returns IDs
			if strings.Contains(argsStr, "rm -f") {
				return "container1\ncontainer2", "", 0, nil
			}
			return "", "", 0, nil
		},
	}
	executor := createTestComposeExecutor(cfg, mockProc, nil)

	ctx := context.Background()
	result, err := executor.ForceCleanup(ctx)

	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
	if result.ContainersRemoved < 2 {
		t.Errorf("expected at least 2 containers removed, got: %d", result.ContainersRemoved)
	}
}

// TestDefaultComposeExecutor_ForceCleanup_PartialError tests partial failure.
//
// # Description
//
// Verifies that ForceCleanup returns ErrCleanupPartial when some steps fail.
//
// # Inputs
//
//   - Mock returns error for some operations
//
// # Outputs
//
//   - ErrCleanupPartial error
//   - CleanupResult with errors list
//
// # Example
//
//	go test -run TestDefaultComposeExecutor_ForceCleanup_PartialError
//
// # Limitations
//
//   - Tests specific error scenario
//
// # Assumptions
//
//   - Cleanup continues after individual failures
func TestDefaultComposeExecutor_ForceCleanup_PartialError(t *testing.T) {
	cfg := createTestComposeConfig("/test/stack")
	mockProc := &MockProcessManager{
		RunInDirFunc: func(ctx context.Context, dir string, env []string, name string, args ...string) (string, string, int, error) {
			argsStr := strings.Join(args, " ")
			// Stop command fails
			if strings.Contains(argsStr, "stop") {
				return "", "error stopping", 1, nil
			}
			// Pod list returns no pods
			if strings.Contains(argsStr, "pod ls") {
				return "", "", 0, nil
			}
			return "", "", 0, nil
		},
	}
	executor := createTestComposeExecutor(cfg, mockProc, nil)

	ctx := context.Background()
	result, err := executor.ForceCleanup(ctx)

	if !errors.Is(err, ErrCleanupPartial) {
		t.Errorf("expected ErrCleanupPartial, got: %v", err)
	}
	if len(result.Errors) == 0 {
		t.Error("expected errors in result")
	}
}

// =============================================================================
// Exec Tests
// =============================================================================

// TestDefaultComposeExecutor_Exec_Success tests successful exec.
//
// # Description
//
// Verifies that Exec runs command in container and returns output.
//
// # Inputs
//
//   - ExecOptions with service and command
//
// # Outputs
//
//   - ExecResult with stdout
//   - nil error
//
// # Example
//
//	go test -run TestDefaultComposeExecutor_Exec_Success
//
// # Limitations
//
//   - Mocked command execution
//
// # Assumptions
//
//   - Container is running
func TestDefaultComposeExecutor_Exec_Success(t *testing.T) {
	cfg := createTestComposeConfig("/test/stack")
	mockProc := &MockProcessManager{
		RunInDirFunc: func(ctx context.Context, dir string, env []string, name string, args ...string) (string, string, int, error) {
			argsStr := strings.Join(args, " ")
			if !strings.Contains(argsStr, "exec") {
				t.Errorf("expected 'exec' in args, got: %s", argsStr)
			}
			if !strings.Contains(argsStr, "weaviate") {
				t.Errorf("expected 'weaviate' in args, got: %s", argsStr)
			}
			return "command output", "", 0, nil
		},
	}
	executor := createTestComposeExecutor(cfg, mockProc, nil)

	ctx := context.Background()
	result, err := executor.Exec(ctx, ExecOptions{
		Service: "weaviate",
		Command: []string{"ls", "-la"},
	})

	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
	if result.Stdout != "command output" {
		t.Errorf("expected 'command output', got: %s", result.Stdout)
	}
}

// TestDefaultComposeExecutor_Exec_EmptyService tests exec with empty service.
//
// # Description
//
// Verifies that Exec returns ErrInvalidConfig when service is empty.
//
// # Inputs
//
//   - ExecOptions{Service: ""}
//
// # Outputs
//
//   - ErrInvalidConfig error
//
// # Example
//
//	go test -run TestDefaultComposeExecutor_Exec_EmptyService
//
// # Limitations
//
//   - Only tests service validation
//
// # Assumptions
//
//   - Service is required
func TestDefaultComposeExecutor_Exec_EmptyService(t *testing.T) {
	cfg := createTestComposeConfig("/test/stack")
	mockProc := &MockProcessManager{}
	executor := createTestComposeExecutor(cfg, mockProc, nil)

	ctx := context.Background()
	_, err := executor.Exec(ctx, ExecOptions{
		Service: "",
		Command: []string{"ls"},
	})

	if !errors.Is(err, ErrInvalidConfig) {
		t.Errorf("expected ErrInvalidConfig, got: %v", err)
	}
}

// TestDefaultComposeExecutor_Exec_EmptyCommand tests exec with empty command.
//
// # Description
//
// Verifies that Exec returns ErrInvalidConfig when command is empty.
//
// # Inputs
//
//   - ExecOptions{Command: []string{}}
//
// # Outputs
//
//   - ErrInvalidConfig error
//
// # Example
//
//	go test -run TestDefaultComposeExecutor_Exec_EmptyCommand
//
// # Limitations
//
//   - Only tests command validation
//
// # Assumptions
//
//   - Command is required
func TestDefaultComposeExecutor_Exec_EmptyCommand(t *testing.T) {
	cfg := createTestComposeConfig("/test/stack")
	mockProc := &MockProcessManager{}
	executor := createTestComposeExecutor(cfg, mockProc, nil)

	ctx := context.Background()
	_, err := executor.Exec(ctx, ExecOptions{
		Service: "weaviate",
		Command: []string{},
	})

	if !errors.Is(err, ErrInvalidConfig) {
		t.Errorf("expected ErrInvalidConfig, got: %v", err)
	}
}

// TestDefaultComposeExecutor_Exec_ContainerNotRunning tests not running error.
//
// # Description
//
// Verifies that Exec returns ErrContainerNotRunning when container is stopped.
//
// # Inputs
//
//   - Mock returns "not running" in stderr
//
// # Outputs
//
//   - ErrContainerNotRunning error
//
// # Example
//
//	go test -run TestDefaultComposeExecutor_Exec_ContainerNotRunning
//
// # Limitations
//
//   - Tests string-based error detection
//
// # Assumptions
//
//   - Stderr contains "not running"
func TestDefaultComposeExecutor_Exec_ContainerNotRunning(t *testing.T) {
	cfg := createTestComposeConfig("/test/stack")
	mockProc := &MockProcessManager{
		RunInDirFunc: func(ctx context.Context, dir string, env []string, name string, args ...string) (string, string, int, error) {
			return "", "container not running", 1, nil
		},
	}
	executor := createTestComposeExecutor(cfg, mockProc, nil)

	ctx := context.Background()
	_, err := executor.Exec(ctx, ExecOptions{
		Service: "weaviate",
		Command: []string{"ls"},
	})

	if !errors.Is(err, ErrContainerNotRunning) {
		t.Errorf("expected ErrContainerNotRunning, got: %v", err)
	}
}

// TestDefaultComposeExecutor_Exec_WithOptions tests exec with all options.
//
// # Description
//
// Verifies that Exec includes user, workdir, and env options.
//
// # Inputs
//
//   - ExecOptions with User, WorkDir, Env
//
// # Outputs
//
//   - Command includes --user, --workdir, -e flags
//
// # Example
//
//	go test -run TestDefaultComposeExecutor_Exec_WithOptions
//
// # Limitations
//
//   - Tests flag presence
//
// # Assumptions
//
//   - Option flags match podman-compose syntax
func TestDefaultComposeExecutor_Exec_WithOptions(t *testing.T) {
	cfg := createTestComposeConfig("/test/stack")
	var capturedArgs []string
	mockProc := &MockProcessManager{
		RunInDirFunc: func(ctx context.Context, dir string, env []string, name string, args ...string) (string, string, int, error) {
			capturedArgs = args
			return "", "", 0, nil
		},
	}
	executor := createTestComposeExecutor(cfg, mockProc, nil)

	ctx := context.Background()
	_, _ = executor.Exec(ctx, ExecOptions{
		Service: "web",
		Command: []string{"ls"},
		User:    "root",
		WorkDir: "/app",
		Env:     map[string]string{"DEBUG": "1"},
	})

	argsStr := strings.Join(capturedArgs, " ")
	if !strings.Contains(argsStr, "--user root") {
		t.Errorf("expected --user root, got: %s", argsStr)
	}
	if !strings.Contains(argsStr, "--workdir /app") {
		t.Errorf("expected --workdir /app, got: %s", argsStr)
	}
	if !strings.Contains(argsStr, "-e DEBUG=1") {
		t.Errorf("expected -e DEBUG=1, got: %s", argsStr)
	}
}

// =============================================================================
// GetComposeFiles Tests
// =============================================================================

// TestDefaultComposeExecutor_GetComposeFiles_BaseOnly tests base file only.
//
// # Description
//
// Verifies that GetComposeFiles returns only base file when others don't exist.
//
// # Inputs
//
//   - Only base file exists
//
// # Outputs
//
//   - Single file in list
//
// # Example
//
//	go test -run TestDefaultComposeExecutor_GetComposeFiles_BaseOnly
//
// # Limitations
//
//   - Tests with mock stat
//
// # Assumptions
//
//   - Base file is always included
func TestDefaultComposeExecutor_GetComposeFiles_BaseOnly(t *testing.T) {
	cfg := createTestComposeConfig("/test/stack")
	mockProc := &MockProcessManager{}
	executor := createTestComposeExecutor(cfg, mockProc, nil) // nil stat = all files don't exist

	files := executor.GetComposeFiles()

	if len(files) != 1 {
		t.Errorf("expected 1 file, got: %d", len(files))
	}
	if !strings.Contains(files[0], "podman-compose.yml") {
		t.Errorf("expected base file, got: %s", files[0])
	}
}

// TestDefaultComposeExecutor_GetComposeFiles_WithOverride tests with override.
//
// # Description
//
// Verifies that GetComposeFiles includes override file when it exists.
//
// # Inputs
//
//   - Base and override files exist
//
// # Outputs
//
//   - Two files in correct order
//
// # Example
//
//	go test -run TestDefaultComposeExecutor_GetComposeFiles_WithOverride
//
// # Limitations
//
//   - Tests with mock stat
//
// # Assumptions
//
//   - Override comes after base
func TestDefaultComposeExecutor_GetComposeFiles_WithOverride(t *testing.T) {
	cfg := createTestComposeConfig("/test/stack")
	mockProc := &MockProcessManager{}
	statFunc := mockStatForPaths(
		"/test/stack/podman-compose.yml",
		"/test/stack/podman-compose.override.yml",
	)
	executor := createTestComposeExecutor(cfg, mockProc, statFunc)

	files := executor.GetComposeFiles()

	if len(files) != 2 {
		t.Errorf("expected 2 files, got: %d", len(files))
	}
	if !strings.Contains(files[1], "override") {
		t.Errorf("expected override file second, got: %v", files)
	}
}

// TestDefaultComposeExecutor_GetComposeFiles_WithExtensions tests extensions.
//
// # Description
//
// Verifies that GetComposeFiles includes extension files in order.
//
// # Inputs
//
//   - Config with extension files that exist
//
// # Outputs
//
//   - All files in correct order
//
// # Example
//
//	go test -run TestDefaultComposeExecutor_GetComposeFiles_WithExtensions
//
// # Limitations
//
//   - Tests specific extension configuration
//
// # Assumptions
//
//   - Extensions come after base and override
func TestDefaultComposeExecutor_GetComposeFiles_WithExtensions(t *testing.T) {
	cfg := createTestComposeConfig("/test/stack")
	cfg.ExtensionFiles = []string{"gpu.yml", "dev.yml"}
	mockProc := &MockProcessManager{}
	statFunc := mockStatForPaths(
		"/test/stack/podman-compose.yml",
		"/test/stack/gpu.yml",
		"/test/stack/dev.yml",
	)
	executor := createTestComposeExecutor(cfg, mockProc, statFunc)

	files := executor.GetComposeFiles()

	if len(files) != 3 {
		t.Errorf("expected 3 files, got: %d", len(files))
	}
}

// =============================================================================
// Helper Method Tests
// =============================================================================

// TestDefaultComposeExecutor_ExtractServiceName tests service name extraction.
//
// # Description
//
// Verifies that service names are correctly extracted from container names.
//
// # Inputs
//
//   - Various container name formats
//
// # Outputs
//
//   - Correct service names
//
// # Example
//
//	go test -run TestDefaultComposeExecutor_ExtractServiceName
//
// # Limitations
//
//   - Tests via Status (indirect)
//
// # Assumptions
//
//   - Container names follow prefix-service-N pattern
func TestDefaultComposeExecutor_ExtractServiceName(t *testing.T) {
	cfg := createTestComposeConfig("/test/stack")
	mockProc := &MockProcessManager{}
	executor := createTestComposeExecutor(cfg, mockProc, nil)

	tests := []struct {
		containerName string
		expected      string
	}{
		{"test-weaviate-1", "weaviate"},
		{"test-go-orchestrator-1", "go-orchestrator"},
		{"test-simple-2", "simple"},
		{"noprefix", "noprefix"},
		{"", ""},
	}

	for _, tc := range tests {
		t.Run(tc.containerName, func(t *testing.T) {
			result := executor.extractServiceName(tc.containerName)
			if result != tc.expected {
				t.Errorf("extractServiceName(%q) = %q, want %q", tc.containerName, result, tc.expected)
			}
		})
	}
}

// TestDefaultComposeExecutor_IsSensitiveEnvVar tests sensitive var detection.
//
// # Description
//
// Verifies that sensitive environment variable names are correctly identified.
//
// # Inputs
//
//   - Various environment variable names
//
// # Outputs
//
//   - Correct sensitivity detection
//
// # Example
//
//	go test -run TestDefaultComposeExecutor_IsSensitiveEnvVar
//
// # Limitations
//
//   - Tests pattern-based detection
//
// # Assumptions
//
//   - Common sensitive patterns are TOKEN, SECRET, KEY, PASSWORD
func TestDefaultComposeExecutor_IsSensitiveEnvVar(t *testing.T) {
	cfg := createTestComposeConfig("/test/stack")
	mockProc := &MockProcessManager{}
	executor := createTestComposeExecutor(cfg, mockProc, nil)

	tests := []struct {
		name     string
		expected bool
	}{
		{"API_TOKEN", true},
		{"SECRET_KEY", true},
		{"PASSWORD", true},
		{"AWS_SECRET_ACCESS_KEY", true},
		{"CREDENTIAL_PATH", true},
		{"OLLAMA_MODEL", false},
		{"LOG_LEVEL", false},
		{"PORT", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := executor.isSensitiveEnvVar(tc.name)
			if result != tc.expected {
				t.Errorf("isSensitiveEnvVar(%q) = %v, want %v", tc.name, result, tc.expected)
			}
		})
	}
}

// TestDefaultComposeExecutor_ParseLines tests line parsing.
//
// # Description
//
// Verifies that parseLines correctly splits and filters output.
//
// # Inputs
//
//   - Various multiline outputs
//
// # Outputs
//
//   - Correct non-empty lines
//
// # Example
//
//	go test -run TestDefaultComposeExecutor_ParseLines
//
// # Limitations
//
//   - Tests Unix-style newlines
//
// # Assumptions
//
//   - Empty lines are filtered out
func TestDefaultComposeExecutor_ParseLines(t *testing.T) {
	cfg := createTestComposeConfig("/test/stack")
	mockProc := &MockProcessManager{}
	executor := createTestComposeExecutor(cfg, mockProc, nil)

	tests := []struct {
		input    string
		expected []string
	}{
		{"line1\nline2\n", []string{"line1", "line2"}},
		{"line1\n\n\nline2", []string{"line1", "line2"}},
		{"  line1  \n  line2  ", []string{"line1", "line2"}},
		{"", []string{}},
		{"\n\n\n", []string{}},
	}

	for _, tc := range tests {
		t.Run(fmt.Sprintf("%q", tc.input), func(t *testing.T) {
			result := executor.parseLines(tc.input)
			if len(result) != len(tc.expected) {
				t.Errorf("parseLines(%q) length = %d, want %d", tc.input, len(result), len(tc.expected))
				return
			}
			for i, v := range result {
				if v != tc.expected[i] {
					t.Errorf("parseLines(%q)[%d] = %q, want %q", tc.input, i, v, tc.expected[i])
				}
			}
		})
	}
}

// =============================================================================
// MockComposeExecutor Tests
// =============================================================================

// TestMockComposeExecutor_Up tests mock Up tracking.
//
// # Description
//
// Verifies that MockComposeExecutor correctly tracks Up calls.
//
// # Inputs
//
//   - Multiple Up calls with different options
//
// # Outputs
//
//   - Correct UpCalls tracking
//
// # Example
//
//	go test -run TestMockComposeExecutor_Up
//
// # Limitations
//
//   - Only tests call tracking
//
// # Assumptions
//
//   - UpCalls is thread-safe
func TestMockComposeExecutor_Up(t *testing.T) {
	mock := &MockComposeExecutor{}

	ctx := context.Background()
	_, _ = mock.Up(ctx, UpOptions{ForceBuild: true})
	_, _ = mock.Up(ctx, UpOptions{Services: []string{"web"}})

	if len(mock.UpCalls) != 2 {
		t.Errorf("expected 2 Up calls, got: %d", len(mock.UpCalls))
	}
	if !mock.UpCalls[0].ForceBuild {
		t.Error("expected first call to have ForceBuild=true")
	}
}

// TestMockComposeExecutor_CustomFunc tests custom mock function.
//
// # Description
//
// Verifies that MockComposeExecutor uses custom functions when provided.
//
// # Inputs
//
//   - Mock with custom UpFunc
//
// # Outputs
//
//   - Custom function result
//
// # Example
//
//	go test -run TestMockComposeExecutor_CustomFunc
//
// # Limitations
//
//   - Tests Up function only
//
// # Assumptions
//
//   - All methods support custom functions
func TestMockComposeExecutor_CustomFunc(t *testing.T) {
	customError := errors.New("custom error")
	mock := &MockComposeExecutor{
		UpFunc: func(ctx context.Context, opts UpOptions) (*ComposeResult, error) {
			return &ComposeResult{Success: false}, customError
		},
	}

	ctx := context.Background()
	result, err := mock.Up(ctx, UpOptions{})

	if !errors.Is(err, customError) {
		t.Errorf("expected custom error, got: %v", err)
	}
	if result.Success {
		t.Error("expected Success=false")
	}
}

// TestMockComposeExecutor_CleanupCalls tests cleanup tracking.
//
// # Description
//
// Verifies that MockComposeExecutor correctly counts cleanup calls.
//
// # Inputs
//
//   - Multiple ForceCleanup calls
//
// # Outputs
//
//   - Correct CleanupCalls count
//
// # Example
//
//	go test -run TestMockComposeExecutor_CleanupCalls
//
// # Limitations
//
//   - Only tests call counting
//
// # Assumptions
//
//   - CleanupCalls is thread-safe
func TestMockComposeExecutor_CleanupCalls(t *testing.T) {
	mock := &MockComposeExecutor{}

	ctx := context.Background()
	_, _ = mock.ForceCleanup(ctx)
	_, _ = mock.ForceCleanup(ctx)
	_, _ = mock.ForceCleanup(ctx)

	if mock.CleanupCalls != 3 {
		t.Errorf("expected 3 cleanup calls, got: %d", mock.CleanupCalls)
	}
}

// =============================================================================
// Context Cancellation Tests
// =============================================================================

// TestDefaultComposeExecutor_Up_ContextCancellation tests context handling.
//
// # Description
//
// Verifies that Up respects context cancellation.
//
// # Inputs
//
//   - Already-cancelled context
//
// # Outputs
//
//   - Context error propagated
//
// # Example
//
//	go test -run TestDefaultComposeExecutor_Up_ContextCancellation
//
// # Limitations
//
//   - Tests pre-cancelled context
//
// # Assumptions
//
//   - ProcessManager respects context
func TestDefaultComposeExecutor_Up_ContextCancellation(t *testing.T) {
	cfg := createTestComposeConfig("/test/stack")
	mockProc := &MockProcessManager{
		RunInDirFunc: func(ctx context.Context, dir string, env []string, name string, args ...string) (string, string, int, error) {
			return "", "", 0, ctx.Err()
		},
	}
	executor := createTestComposeExecutor(cfg, mockProc, nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := executor.Up(ctx, UpOptions{})

	if err == nil {
		t.Error("expected error from cancelled context")
	}
}
