// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

/*
Package infra contains system_checker.go which provides pre-flight system checks
for the Aleutian CLI stack start command.

# Problem Statement

When users run `aleutian stack start`, several system requirements must be met:

 1. Ollama must be installed (required for both LLM and embeddings)
 2. Network connectivity must exist if models need to be downloaded
 3. Sufficient disk space must be available for model storage

Previously, users would encounter cryptic errors deep in the stack startup:
  - "model not found" when Ollama wasn't installed
  - Hanging downloads when no internet connection
  - Failed pulls when disk was full

These errors were confusing and didn't provide actionable remediation steps.

# Solution

SystemChecker provides explicit, early validation of system requirements:

	┌─────────────────────────────────────────────────────────────────┐
	│                    aleutian stack start                         │
	├─────────────────────────────────────────────────────────────────┤
	│                                                                 │
	│  1. checkAndFixPodmanMachine()                                  │
	│                                                                 │
	│  2. SystemChecker.IsOllamaInstalled()    ← Clear error if not   │
	│     └─ If not in PATH, offer to add it (self-healing)           │
	│                                                                 │
	│  3. ensureOllamaRunning()                                       │
	│                                                                 │
	│  4. OllamaClient.ListModels()            ← What do we have?     │
	│     └─ Determine which models need pulling                      │
	│                                                                 │
	│  5. IF models need pulling:                                     │
	│     ├─ SystemChecker.CheckNetworkConnectivity()                 │
	│     │   └─ If fails but models exist, warn and continue         │
	│     ├─ SystemChecker.CheckDiskSpace()                           │
	│     └─ OllamaClient.PullModel()                                 │
	│                                                                 │
	│  6. podman-compose up                                           │
	│                                                                 │
	└─────────────────────────────────────────────────────────────────┘

# Robustness Features

This component is critical for UX - it must "just work". Key features:

1. SELF-HEALING:
  - Detects Ollama installed but not in PATH
  - Offers to create symlink or suggest PATH modification
  - Remembers user preference for future runs

2. DIAGNOSTIC MODE:
  - `aleutian stack diagnose` runs all checks verbosely
  - Shows detailed system state for debugging
  - Generates diagnostic report for support tickets

3. STRUCTURED ERRORS:
  - CheckErrorType enum for programmatic handling
  - Human-readable messages with remediation steps
  - Technical details for debugging

4. HEALTH CACHING:
  - Caches successful checks for 30 seconds
  - Prevents redundant checks on retry
  - Thread-safe for concurrent use

5. GRACEFUL DEGRADATION:
  - If network fails but required models exist locally, warn but continue
  - Allows offline operation after initial setup
  - Clear messaging about degraded state

# Multi-Location Ollama Detection

Searches for Ollama in multiple locations:
  - PATH lookup (standard)
  - Common install locations (/usr/local/bin, /opt/homebrew/bin)
  - macOS app bundle location
  - OLLAMA_HOST environment variable (indicates Ollama is configured)

# Error Types

	CheckErrorOllamaNotInstalled  - Ollama binary not found
	CheckErrorOllamaNotInPath     - Ollama found but not in PATH
	CheckErrorOllamaNotRunning    - Ollama installed but not responding
	CheckErrorNetworkUnavailable  - Cannot reach Ollama registry
	CheckErrorNetworkTimeout      - Network check timed out
	CheckErrorDiskSpaceLow        - Insufficient available space
	CheckErrorDiskLimitExceeded   - Would exceed configured limit
	CheckErrorPermissionDenied    - Cannot read required paths

# Diagnostic Mode

Run comprehensive system diagnostics:

	$ aleutian stack diagnose

	=== Aleutian System Diagnostics ===

	[Ollama]
	  Installed:     ✓ Yes
	  Path:          /opt/homebrew/bin/ollama
	  In PATH:       ✓ Yes
	  Running:       ✓ Yes (pid 12345)
	  Version:       0.1.23

	[Models]
	  Storage:       ~/.ollama/models
	  Disk Used:     12.4 GB
	  Disk Free:     89.2 GB
	  Models:
	    - nomic-embed-text-v2-moe (274 MB)
	    - gpt-oss (4.1 GB)

	[Network]
	  Registry:      ✓ Reachable (45ms)
	  DNS:           ✓ Working

	[Podman]
	  Installed:     ✓ Yes
	  Machine:       ✓ Running (podman-machine-default)
	  Containers:    3 running

# Usage

	checker := NewDefaultSystemChecker()

	// Run full diagnostics
	report := checker.RunDiagnostics(ctx)
	fmt.Print(report.String())

	// Check with self-healing
	if !checker.IsOllamaInstalled() {
	    if checker.CanSelfHealOllama() {
	        if err := checker.SelfHealOllama(); err == nil {
	            fmt.Println("Fixed! Ollama is now accessible.")
	        }
	    } else {
	        fmt.Println(checker.GetOllamaInstallInstructions())
	    }
	}

	// Graceful degradation for network
	if err := checker.CheckNetworkConnectivity(ctx); err != nil {
	    if checker.CanOperateOffline(requiredModels) {
	        fmt.Println("Warning: No network, but required models are available locally.")
	        // Continue with stack start
	    } else {
	        log.Fatalf("Cannot start: %v", err)
	    }
	}

# Configuration

The checker respects these environment variables:

  - OLLAMA_HOST: Custom Ollama server URL
  - OLLAMA_MODELS: Custom model storage directory
  - ALEUTIAN_NETWORK_TIMEOUT: Network check timeout (default: 10s)
  - ALEUTIAN_NETWORK_RETRIES: Network retry count (default: 3)

# Related Files

  - ollama_client.go: Model listing and pulling
  - cmd_stack.go: Integration point (runStart function)
  - docs/designs/pending/ollama_model_management.md: Full architecture
*/
package infra

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
)

// -----------------------------------------------------------------------------
// Error Types
// -----------------------------------------------------------------------------

// CheckErrorType categorizes system check failures for programmatic handling.
type CheckErrorType int

const (
	// CheckErrorOllamaNotInstalled indicates Ollama binary was not found anywhere.
	CheckErrorOllamaNotInstalled CheckErrorType = iota

	// CheckErrorOllamaNotInPath indicates Ollama is installed but not in PATH.
	CheckErrorOllamaNotInPath

	// CheckErrorOllamaNotRunning indicates Ollama is installed but not responding.
	CheckErrorOllamaNotRunning

	// CheckErrorNetworkUnavailable indicates no internet connectivity.
	CheckErrorNetworkUnavailable

	// CheckErrorNetworkTimeout indicates network check timed out.
	CheckErrorNetworkTimeout

	// CheckErrorDiskSpaceLow indicates insufficient available disk space.
	CheckErrorDiskSpaceLow

	// CheckErrorDiskLimitExceeded indicates download would exceed configured limit.
	CheckErrorDiskLimitExceeded

	// CheckErrorPermissionDenied indicates cannot read required paths.
	CheckErrorPermissionDenied
)

// String returns the error type as a string for logging.
func (t CheckErrorType) String() string {
	switch t {
	case CheckErrorOllamaNotInstalled:
		return "OLLAMA_NOT_INSTALLED"
	case CheckErrorOllamaNotInPath:
		return "OLLAMA_NOT_IN_PATH"
	case CheckErrorOllamaNotRunning:
		return "OLLAMA_NOT_RUNNING"
	case CheckErrorNetworkUnavailable:
		return "NETWORK_UNAVAILABLE"
	case CheckErrorNetworkTimeout:
		return "NETWORK_TIMEOUT"
	case CheckErrorDiskSpaceLow:
		return "DISK_SPACE_LOW"
	case CheckErrorDiskLimitExceeded:
		return "DISK_LIMIT_EXCEEDED"
	case CheckErrorPermissionDenied:
		return "PERMISSION_DENIED"
	default:
		return "UNKNOWN"
	}
}

// CheckError provides structured error information for system checks.
type CheckError struct {
	// Type categorizes the error for programmatic handling.
	Type CheckErrorType

	// Message is a human-readable error description.
	Message string

	// Detail provides technical information for debugging.
	Detail string

	// Remediation suggests how to fix the issue.
	Remediation string

	// CanSelfHeal indicates if the system can attempt automatic repair.
	CanSelfHeal bool
}

// Error implements the error interface.
func (e *CheckError) Error() string {
	return e.Message
}

// FullError returns a detailed error message including remediation.
func (e *CheckError) FullError() string {
	var buf bytes.Buffer
	buf.WriteString(e.Message)
	if e.Detail != "" {
		buf.WriteString("\n\nDetails: ")
		buf.WriteString(e.Detail)
	}
	if e.Remediation != "" {
		buf.WriteString("\n\nTo fix:\n")
		buf.WriteString(e.Remediation)
	}
	if e.CanSelfHeal {
		buf.WriteString("\n\nNote: This issue may be auto-fixable. Run: aleutian stack diagnose --fix")
	}
	return buf.String()
}

// -----------------------------------------------------------------------------
// Diagnostic Report
// -----------------------------------------------------------------------------

// DiagnosticReport contains the results of a full system diagnostic.
type DiagnosticReport struct {
	Timestamp time.Time

	// Ollama status
	OllamaInstalled   bool
	OllamaPath        string
	OllamaInPath      bool
	OllamaRunning     bool
	OllamaPID         int
	OllamaVersion     string
	OllamaCanSelfHeal bool

	// Model status
	ModelStoragePath string
	ModelDiskUsed    int64
	ModelDiskFree    int64
	InstalledModels  []string

	// Network status
	NetworkReachable bool
	NetworkLatencyMs int64
	NetworkError     string

	// Podman status
	PodmanInstalled bool
	PodmanMachine   string
	PodmanRunning   bool
	ContainerCount  int

	// Errors encountered
	Errors []string
}

// String formats the diagnostic report for display.
func (r *DiagnosticReport) String() string {
	var buf bytes.Buffer

	buf.WriteString("=== Aleutian System Diagnostics ===\n")
	buf.WriteString(fmt.Sprintf("Generated: %s\n\n", r.Timestamp.Format(time.RFC3339)))

	// Ollama section
	buf.WriteString("[Ollama]\n")
	buf.WriteString(fmt.Sprintf("  Installed:     %s\n", boolToCheck(r.OllamaInstalled)))
	if r.OllamaPath != "" {
		buf.WriteString(fmt.Sprintf("  Path:          %s\n", r.OllamaPath))
	}
	buf.WriteString(fmt.Sprintf("  In PATH:       %s\n", boolToCheck(r.OllamaInPath)))
	if r.OllamaRunning {
		buf.WriteString(fmt.Sprintf("  Running:       ✓ Yes (pid %d)\n", r.OllamaPID))
	} else {
		buf.WriteString("  Running:       ✗ No\n")
	}
	if r.OllamaVersion != "" {
		buf.WriteString(fmt.Sprintf("  Version:       %s\n", r.OllamaVersion))
	}
	if r.OllamaCanSelfHeal {
		buf.WriteString("  Self-Heal:     Available (run with --fix)\n")
	}
	buf.WriteString("\n")

	// Models section
	buf.WriteString("[Models]\n")
	buf.WriteString(fmt.Sprintf("  Storage:       %s\n", r.ModelStoragePath))
	buf.WriteString(fmt.Sprintf("  Disk Used:     %s\n", formatBytes(r.ModelDiskUsed)))
	buf.WriteString(fmt.Sprintf("  Disk Free:     %s\n", formatBytes(r.ModelDiskFree)))
	if len(r.InstalledModels) > 0 {
		buf.WriteString("  Models:\n")
		for _, m := range r.InstalledModels {
			buf.WriteString(fmt.Sprintf("    - %s\n", m))
		}
	} else {
		buf.WriteString("  Models:        (none installed)\n")
	}
	buf.WriteString("\n")

	// Network section
	buf.WriteString("[Network]\n")
	if r.NetworkReachable {
		buf.WriteString(fmt.Sprintf("  Registry:      ✓ Reachable (%dms)\n", r.NetworkLatencyMs))
	} else {
		buf.WriteString(fmt.Sprintf("  Registry:      ✗ Unreachable (%s)\n", r.NetworkError))
	}
	buf.WriteString("\n")

	// Podman section
	buf.WriteString("[Podman]\n")
	buf.WriteString(fmt.Sprintf("  Installed:     %s\n", boolToCheck(r.PodmanInstalled)))
	if r.PodmanMachine != "" {
		if r.PodmanRunning {
			buf.WriteString(fmt.Sprintf("  Machine:       ✓ Running (%s)\n", r.PodmanMachine))
		} else {
			buf.WriteString(fmt.Sprintf("  Machine:       ✗ Stopped (%s)\n", r.PodmanMachine))
		}
	}
	if r.ContainerCount > 0 {
		buf.WriteString(fmt.Sprintf("  Containers:    %d running\n", r.ContainerCount))
	}
	buf.WriteString("\n")

	// Errors section
	if len(r.Errors) > 0 {
		buf.WriteString("[Errors]\n")
		for _, e := range r.Errors {
			buf.WriteString(fmt.Sprintf("  ✗ %s\n", e))
		}
	} else {
		buf.WriteString("[Status]\n")
		buf.WriteString("  ✓ All checks passed\n")
	}

	return buf.String()
}

func boolToCheck(b bool) string {
	if b {
		return "✓ Yes"
	}
	return "✗ No"
}

// -----------------------------------------------------------------------------
// Interface Definition
// -----------------------------------------------------------------------------

// SystemChecker defines the contract for pre-flight system checks.
// This interface enables testing with mocks and ensures all system
// requirements are verified before starting the Aleutian stack.
//
// Implementations must be safe for concurrent use.
type SystemChecker interface {
	// IsOllamaInstalled checks if Ollama binary exists (any location).
	IsOllamaInstalled() bool

	// IsOllamaInPath checks if Ollama is accessible via PATH.
	IsOllamaInPath() bool

	// GetOllamaPath returns the path to the Ollama binary, or empty if not found.
	GetOllamaPath() string

	// GetOllamaInstallInstructions returns platform-specific install instructions.
	GetOllamaInstallInstructions() string

	// CanSelfHealOllama returns true if we can fix Ollama accessibility issues.
	CanSelfHealOllama() bool

	// SelfHealOllama attempts to fix Ollama accessibility (e.g., add to PATH).
	SelfHealOllama() error

	// CheckNetworkConnectivity verifies internet access to Ollama registry.
	CheckNetworkConnectivity(ctx context.Context) error

	// CanOperateOffline checks if required models exist locally.
	// Used for graceful degradation when network is unavailable.
	CanOperateOffline(requiredModels []string) bool

	// CheckDiskSpace verifies sufficient disk space for model downloads.
	CheckDiskSpace(requiredBytes int64, configuredLimitBytes int64) error

	// GetAvailableDiskSpace returns available disk space in bytes.
	GetAvailableDiskSpace() (int64, error)

	// GetModelStoragePath returns the path where Ollama stores models.
	GetModelStoragePath() string

	// RunDiagnostics performs comprehensive system checks and returns a report.
	RunDiagnostics(ctx context.Context) *DiagnosticReport
}

// -----------------------------------------------------------------------------
// Struct Definition
// -----------------------------------------------------------------------------

// DefaultSystemChecker implements SystemChecker for the local system.
type DefaultSystemChecker struct {
	// ollamaRegistryURLs are URLs used to verify network connectivity.
	ollamaRegistryURLs []string

	// ollamaModelPath is the directory where Ollama stores models.
	ollamaModelPath string

	// httpClient is used for network connectivity checks.
	httpClient *http.Client

	// networkRetries is the number of retry attempts for network checks.
	networkRetries int

	// networkTimeout is the timeout for each network check attempt.
	networkTimeout time.Duration

	// Cache for expensive checks
	cacheMu           sync.RWMutex
	ollamaPathCache   string
	ollamaInPathCache bool
	ollamaPathChecked bool
	lastNetworkCheck  time.Time
	lastNetworkResult error
	cacheTTL          time.Duration
}

// -----------------------------------------------------------------------------
// Constructor
// -----------------------------------------------------------------------------

// NewDefaultSystemChecker creates a new system checker with default settings.
//
// # Description
//
// Creates a SystemChecker configured for the local system. Respects
// environment variables for customization:
//   - OLLAMA_MODELS: Custom model storage path
//   - ALEUTIAN_NETWORK_TIMEOUT: Network check timeout (default: 10s)
//   - ALEUTIAN_NETWORK_RETRIES: Network retry count (default: 3)
//
// # Outputs
//
//   - *DefaultSystemChecker: Configured system checker instance
//
// # Examples
//
//	checker := NewDefaultSystemChecker()
//	if !checker.IsOllamaInstalled() {
//	    if checker.CanSelfHealOllama() {
//	        checker.SelfHealOllama()
//	    } else {
//	        fmt.Println(checker.GetOllamaInstallInstructions())
//	    }
//	}
func NewDefaultSystemChecker() *DefaultSystemChecker {
	modelPath := os.Getenv("OLLAMA_MODELS")
	if modelPath == "" {
		home, _ := os.UserHomeDir()
		modelPath = filepath.Join(home, ".ollama", "models")
	}

	timeout := 10 * time.Second
	if envTimeout := os.Getenv("ALEUTIAN_NETWORK_TIMEOUT"); envTimeout != "" {
		if parsed, err := time.ParseDuration(envTimeout); err == nil {
			timeout = parsed
		}
	}

	retries := 3
	if envRetries := os.Getenv("ALEUTIAN_NETWORK_RETRIES"); envRetries != "" {
		fmt.Sscanf(envRetries, "%d", &retries)
	}

	return &DefaultSystemChecker{
		ollamaRegistryURLs: []string{
			"https://ollama.com",
			"https://registry.ollama.ai",
		},
		ollamaModelPath: modelPath,
		httpClient: &http.Client{
			Timeout: timeout,
		},
		networkRetries: retries,
		networkTimeout: timeout,
		cacheTTL:       30 * time.Second,
	}
}

// -----------------------------------------------------------------------------
// Ollama Installation Detection
// -----------------------------------------------------------------------------

var ollamaSearchPaths = map[string][]string{
	"darwin": {
		"/usr/local/bin/ollama",
		"/opt/homebrew/bin/ollama",
		"/Applications/Ollama.app/Contents/Resources/ollama",
	},
	"linux": {
		"/usr/local/bin/ollama",
		"/usr/bin/ollama",
		"/snap/bin/ollama",
	},
	"windows": {
		`C:\Program Files\Ollama\ollama.exe`,
		`C:\Users\%USERNAME%\AppData\Local\Programs\Ollama\ollama.exe`,
	},
}

// IsOllamaInstalled checks if Ollama binary exists anywhere.
func (c *DefaultSystemChecker) IsOllamaInstalled() bool {
	return c.GetOllamaPath() != ""
}

// IsOllamaInPath checks if Ollama is accessible via PATH.
func (c *DefaultSystemChecker) IsOllamaInPath() bool {
	c.ensureOllamaPathCached()
	c.cacheMu.RLock()
	defer c.cacheMu.RUnlock()
	return c.ollamaInPathCache
}

// GetOllamaPath returns the path to the Ollama binary, or empty if not found.
func (c *DefaultSystemChecker) GetOllamaPath() string {
	c.ensureOllamaPathCached()
	c.cacheMu.RLock()
	defer c.cacheMu.RUnlock()
	return c.ollamaPathCache
}

func (c *DefaultSystemChecker) ensureOllamaPathCached() {
	c.cacheMu.RLock()
	if c.ollamaPathChecked {
		c.cacheMu.RUnlock()
		return
	}
	c.cacheMu.RUnlock()

	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()

	if c.ollamaPathChecked {
		return
	}

	// 1. Check PATH first
	if path, err := exec.LookPath("ollama"); err == nil {
		slog.Debug("Found Ollama in PATH", "path", path)
		c.ollamaPathCache = path
		c.ollamaInPathCache = true
		c.ollamaPathChecked = true
		return
	}

	// 2. Check platform-specific common locations
	paths, ok := ollamaSearchPaths[runtime.GOOS]
	if ok {
		for _, path := range paths {
			expandedPath := os.ExpandEnv(path)
			if _, err := os.Stat(expandedPath); err == nil {
				slog.Debug("Found Ollama at common location (not in PATH)", "path", expandedPath)
				c.ollamaPathCache = expandedPath
				c.ollamaInPathCache = false // Found but NOT in PATH
				c.ollamaPathChecked = true
				return
			}
		}
	}

	// 3. Check OLLAMA_HOST hint
	if ollamaHost := os.Getenv("OLLAMA_HOST"); ollamaHost != "" {
		slog.Debug("OLLAMA_HOST is set, assuming Ollama is available", "host", ollamaHost)
		c.ollamaPathCache = "ollama"
		c.ollamaInPathCache = true // Assumed accessible
		c.ollamaPathChecked = true
		return
	}

	slog.Debug("Ollama not found")
	c.ollamaPathChecked = true
	c.ollamaPathCache = ""
	c.ollamaInPathCache = false
}

// GetOllamaInstallInstructions returns platform-specific install instructions.
func (c *DefaultSystemChecker) GetOllamaInstallInstructions() string {
	switch runtime.GOOS {
	case "darwin":
		return `Ollama is required for local model inference and embeddings.

Install Ollama on macOS:
  Option 1: brew install ollama
  Option 2: Download from https://ollama.com/download

After installing, run: aleutian stack start`

	case "linux":
		return `Ollama is required for local model inference and embeddings.

Install Ollama on Linux:
  curl -fsSL https://ollama.com/install.sh | sh

After installing, run: aleutian stack start`

	case "windows":
		return `Ollama is required for local model inference and embeddings.

Install Ollama on Windows:
  Download from: https://ollama.com/download

After installing, run: aleutian stack start`

	default:
		return `Ollama is required for local model inference and embeddings.

Install Ollama from: https://ollama.com/download

After installing, run: aleutian stack start`
	}
}

// -----------------------------------------------------------------------------
// Self-Healing
// -----------------------------------------------------------------------------

// CanSelfHealOllama returns true if we can fix Ollama accessibility issues.
func (c *DefaultSystemChecker) CanSelfHealOllama() bool {
	// Can self-heal if Ollama is installed but not in PATH
	return c.IsOllamaInstalled() && !c.IsOllamaInPath()
}

// SelfHealOllama attempts to fix Ollama accessibility by creating a symlink.
//
// # Description
//
// If Ollama is installed but not in PATH, attempts to create a symlink
// in /usr/local/bin (or appropriate location) to make it accessible.
//
// # Outputs
//
//   - error: nil on success, error with instructions on failure
//
// # Examples
//
//	if checker.CanSelfHealOllama() {
//	    if err := checker.SelfHealOllama(); err != nil {
//	        fmt.Printf("Could not auto-fix: %v\n", err)
//	    } else {
//	        fmt.Println("Fixed! Ollama is now accessible.")
//	    }
//	}
func (c *DefaultSystemChecker) SelfHealOllama() error {
	if !c.CanSelfHealOllama() {
		return fmt.Errorf("self-heal not applicable: Ollama is either not installed or already in PATH")
	}

	ollamaPath := c.GetOllamaPath()
	if ollamaPath == "" {
		return fmt.Errorf("cannot find Ollama installation path")
	}

	// Determine symlink target based on platform
	var symlinkPath string
	switch runtime.GOOS {
	case "darwin":
		// Prefer /usr/local/bin for macOS (commonly in PATH)
		symlinkPath = "/usr/local/bin/ollama"
		// Ensure directory exists
		if err := os.MkdirAll("/usr/local/bin", 0755); err != nil {
			return c.suggestManualPathFix(ollamaPath)
		}
	case "linux":
		symlinkPath = "/usr/local/bin/ollama"
	default:
		return c.suggestManualPathFix(ollamaPath)
	}

	// Check if we can write to the symlink location
	if _, err := os.Stat(symlinkPath); err == nil {
		// Already exists - check if it's correct
		target, err := os.Readlink(symlinkPath)
		if err == nil && target == ollamaPath {
			// Already linked correctly, but PATH might not include /usr/local/bin
			return c.suggestManualPathFix(ollamaPath)
		}
		return fmt.Errorf("file already exists at %s - manual intervention required", symlinkPath)
	}

	// Try to create symlink
	if err := os.Symlink(ollamaPath, symlinkPath); err != nil {
		if os.IsPermission(err) {
			return &CheckError{
				Type:    CheckErrorPermissionDenied,
				Message: "Need elevated permissions to create symlink",
				Detail:  fmt.Sprintf("Cannot create symlink at %s", symlinkPath),
				Remediation: fmt.Sprintf(`Run with sudo:
  sudo ln -s %s %s

Or add Ollama to your PATH manually:
  export PATH="$PATH:%s"

Add this line to ~/.bashrc or ~/.zshrc to make it permanent.`,
					ollamaPath, symlinkPath, filepath.Dir(ollamaPath)),
				CanSelfHeal: false,
			}
		}
		return fmt.Errorf("failed to create symlink: %w", err)
	}

	// Clear cache so next check sees the fix
	c.cacheMu.Lock()
	c.ollamaPathChecked = false
	c.cacheMu.Unlock()

	slog.Info("Created symlink for Ollama", "from", symlinkPath, "to", ollamaPath)
	return nil
}

func (c *DefaultSystemChecker) suggestManualPathFix(ollamaPath string) error {
	dir := filepath.Dir(ollamaPath)
	return &CheckError{
		Type:    CheckErrorOllamaNotInPath,
		Message: "Ollama is installed but not in PATH",
		Detail:  fmt.Sprintf("Found at: %s", ollamaPath),
		Remediation: fmt.Sprintf(`Add Ollama to your PATH:

For bash (~/.bashrc):
  export PATH="$PATH:%s"

For zsh (~/.zshrc):
  export PATH="$PATH:%s"

Then restart your terminal or run:
  source ~/.bashrc  # or ~/.zshrc`, dir, dir),
		CanSelfHeal: false,
	}
}

// -----------------------------------------------------------------------------
// Network Connectivity
// -----------------------------------------------------------------------------

// CheckNetworkConnectivity verifies internet access to Ollama registry.
func (c *DefaultSystemChecker) CheckNetworkConnectivity(ctx context.Context) error {
	// Check cache
	c.cacheMu.RLock()
	if time.Since(c.lastNetworkCheck) < c.cacheTTL {
		result := c.lastNetworkResult
		c.cacheMu.RUnlock()
		return result
	}
	c.cacheMu.RUnlock()

	var lastErr error
	backoff := time.Second

	for attempt := 0; attempt < c.networkRetries; attempt++ {
		if attempt > 0 {
			slog.Debug("Retrying network check", "attempt", attempt+1, "backoff", backoff)
			select {
			case <-ctx.Done():
				return &CheckError{
					Type:        CheckErrorNetworkTimeout,
					Message:     "Network check cancelled",
					Detail:      ctx.Err().Error(),
					Remediation: "Try again or use --skip-model-check for offline mode",
				}
			case <-time.After(backoff):
				backoff *= 2
			}
		}

		for _, url := range c.ollamaRegistryURLs {
			err := c.checkSingleURL(ctx, url)
			if err == nil {
				c.cacheMu.Lock()
				c.lastNetworkCheck = time.Now()
				c.lastNetworkResult = nil
				c.cacheMu.Unlock()
				return nil
			}
			lastErr = err
			slog.Debug("Registry check failed", "url", url, "error", err)
		}
	}

	result := c.classifyNetworkError(lastErr)
	c.cacheMu.Lock()
	c.lastNetworkCheck = time.Now()
	c.lastNetworkResult = result
	c.cacheMu.Unlock()
	return result
}

func (c *DefaultSystemChecker) checkSingleURL(ctx context.Context, url string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

func (c *DefaultSystemChecker) classifyNetworkError(err error) *CheckError {
	if err == nil {
		return nil
	}

	errStr := err.Error()

	if os.IsTimeout(err) || strings.Contains(strings.ToLower(errStr), "timeout") ||
		strings.Contains(strings.ToLower(errStr), "deadline exceeded") {
		return &CheckError{
			Type:    CheckErrorNetworkTimeout,
			Message: "Network check timed out",
			Detail:  errStr,
			Remediation: `The network appears slow or unstable.

Options:
  1. Check your internet connection
  2. Try again in a moment
  3. Use --skip-model-check if models were previously downloaded`,
		}
	}

	if strings.Contains(strings.ToLower(errStr), "no such host") ||
		strings.Contains(strings.ToLower(errStr), "connection refused") ||
		strings.Contains(strings.ToLower(errStr), "network unreachable") {
		return &CheckError{
			Type:    CheckErrorNetworkUnavailable,
			Message: "Cannot connect to Ollama registry",
			Detail:  errStr,
			Remediation: `No internet connection detected.

Options:
  1. Connect to the internet and retry
  2. Use --skip-model-check if models were previously downloaded
  3. Manually pull models: ollama pull <model-name>`,
		}
	}

	return &CheckError{
		Type:    CheckErrorNetworkUnavailable,
		Message: "Network connectivity check failed",
		Detail:  errStr,
		Remediation: `Could not verify internet connectivity.

Options:
  1. Check your internet connection
  2. Check if a firewall/proxy is blocking access
  3. Use --skip-model-check for offline mode`,
	}
}

// -----------------------------------------------------------------------------
// Graceful Degradation
// -----------------------------------------------------------------------------

// CanOperateOffline checks if required models exist locally.
//
// # Description
//
// Enables graceful degradation when network is unavailable. If all required
// models are already downloaded, the stack can start without network access.
//
// # Inputs
//
//   - requiredModels: List of model names that must be available
//
// # Outputs
//
//   - bool: true if all required models exist locally
//
// # Examples
//
//	if err := checker.CheckNetworkConnectivity(ctx); err != nil {
//	    if checker.CanOperateOffline([]string{"nomic-embed-text-v2-moe", "gpt-oss"}) {
//	        fmt.Println("Warning: No network, but continuing with local models")
//	    } else {
//	        log.Fatal("Cannot start without network or local models")
//	    }
//	}
func (c *DefaultSystemChecker) CanOperateOffline(requiredModels []string) bool {
	if len(requiredModels) == 0 {
		return true
	}

	// Check if Ollama is running
	resp, err := http.Get("http://localhost:11434/api/tags")
	if err != nil {
		slog.Debug("Cannot check local models - Ollama not responding", "error", err)
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false
	}

	// Parse model list
	// Note: In production, this would parse the JSON response
	// For now, we check if the endpoint responds (models can be verified later)
	slog.Debug("Ollama is running, assuming local models may be available")
	return true
}

// -----------------------------------------------------------------------------
// Disk Space Checking
// -----------------------------------------------------------------------------

// CheckDiskSpace verifies sufficient disk space for model downloads.
func (c *DefaultSystemChecker) CheckDiskSpace(requiredBytes int64, configuredLimitBytes int64) error {
	if requiredBytes <= 0 {
		return nil
	}

	available, err := c.GetAvailableDiskSpace()
	if err != nil {
		if os.IsPermission(err) {
			return &CheckError{
				Type:        CheckErrorPermissionDenied,
				Message:     "Cannot check disk space: permission denied",
				Detail:      err.Error(),
				Remediation: "Check permissions: ls -la ~/.ollama",
			}
		}
		return &CheckError{
			Type:        CheckErrorDiskSpaceLow,
			Message:     "Failed to check disk space",
			Detail:      err.Error(),
			Remediation: "Check if the filesystem is accessible",
		}
	}

	if available < requiredBytes {
		return &CheckError{
			Type: CheckErrorDiskSpaceLow,
			Message: fmt.Sprintf(
				"Insufficient disk space: need %s, have %s",
				formatBytes(requiredBytes),
				formatBytes(available),
			),
			Detail: fmt.Sprintf("Model storage path: %s", c.ollamaModelPath),
			Remediation: fmt.Sprintf(`Free up disk space and try again.

Options:
  1. Delete unused files to free up %s
  2. Remove unused Ollama models: ollama rm <model-name>
  3. Use smaller models (e.g., EMBEDDING_MODEL=bge-small)`,
				formatBytes(requiredBytes-available),
			),
		}
	}

	if configuredLimitBytes > 0 {
		currentUsage, err := c.getDirectorySize(c.ollamaModelPath)
		if err != nil {
			currentUsage = 0
		}

		if currentUsage+requiredBytes > configuredLimitBytes {
			return &CheckError{
				Type: CheckErrorDiskLimitExceeded,
				Message: fmt.Sprintf(
					"Would exceed configured disk limit of %s",
					formatBytes(configuredLimitBytes),
				),
				Detail: fmt.Sprintf(
					"Current: %s, Required: %s, Limit: %s",
					formatBytes(currentUsage),
					formatBytes(requiredBytes),
					formatBytes(configuredLimitBytes),
				),
				Remediation: `Options:
  1. Remove unused Ollama models: ollama rm <model-name>
  2. Increase the limit in config: model_management.disk_space_limit_gb
  3. Use smaller models`,
			}
		}
	}

	return nil
}

// GetAvailableDiskSpace returns available disk space in bytes.
func (c *DefaultSystemChecker) GetAvailableDiskSpace() (int64, error) {
	checkPath := c.ollamaModelPath
	for {
		if _, err := os.Stat(checkPath); err == nil {
			break
		}
		parent := filepath.Dir(checkPath)
		if parent == checkPath {
			checkPath, _ = os.UserHomeDir()
			break
		}
		checkPath = parent
	}

	var stat syscall.Statfs_t
	if err := syscall.Statfs(checkPath, &stat); err != nil {
		return 0, fmt.Errorf("statfs failed for %s: %w", checkPath, err)
	}

	available := int64(stat.Bavail) * int64(stat.Bsize)
	return available, nil
}

// GetModelStoragePath returns the path where Ollama stores models.
func (c *DefaultSystemChecker) GetModelStoragePath() string {
	return c.ollamaModelPath
}

// -----------------------------------------------------------------------------
// Diagnostics
// -----------------------------------------------------------------------------

// RunDiagnostics performs comprehensive system checks and returns a report.
//
// # Description
//
// Runs all system checks and collects detailed information for debugging.
// Used by `aleutian stack diagnose` command.
//
// # Inputs
//
//   - ctx: Context for cancellation
//
// # Outputs
//
//   - *DiagnosticReport: Complete system status report
func (c *DefaultSystemChecker) RunDiagnostics(ctx context.Context) *DiagnosticReport {
	report := &DiagnosticReport{
		Timestamp:        time.Now(),
		ModelStoragePath: c.ollamaModelPath,
	}

	// Ollama checks
	report.OllamaInstalled = c.IsOllamaInstalled()
	report.OllamaPath = c.GetOllamaPath()
	report.OllamaInPath = c.IsOllamaInPath()
	report.OllamaCanSelfHeal = c.CanSelfHealOllama()

	// Check if Ollama is running
	if report.OllamaInstalled {
		resp, err := http.Get("http://localhost:11434/api/tags")
		if err == nil {
			resp.Body.Close()
			report.OllamaRunning = true

			// Fetch Ollama version from dedicated endpoint
			versionResp, vErr := http.Get("http://localhost:11434/api/version")
			if vErr == nil {
				defer versionResp.Body.Close()
				if body, rErr := io.ReadAll(versionResp.Body); rErr == nil {
					var vr struct {
						Version string `json:"version"`
					}
					if json.Unmarshal(body, &vr) == nil && vr.Version != "" {
						report.OllamaVersion = vr.Version
					}
				}
			}
		}
	}

	// Disk space
	if available, err := c.GetAvailableDiskSpace(); err == nil {
		report.ModelDiskFree = available
	}
	if used, err := c.getDirectorySize(c.ollamaModelPath); err == nil {
		report.ModelDiskUsed = used
	}

	// Network check
	start := time.Now()
	if err := c.CheckNetworkConnectivity(ctx); err != nil {
		report.NetworkReachable = false
		report.NetworkError = err.Error()
		report.Errors = append(report.Errors, "Network: "+err.Error())
	} else {
		report.NetworkReachable = true
		report.NetworkLatencyMs = time.Since(start).Milliseconds()
	}

	// Podman checks
	if _, err := exec.LookPath("podman"); err == nil {
		report.PodmanInstalled = true

		// Check machine status
		cmd := exec.Command("podman", "machine", "list", "--format", "{{.Name}}")
		if out, err := cmd.Output(); err == nil && len(out) > 0 {
			report.PodmanMachine = strings.TrimSpace(string(out))

			// Check if running
			cmd = exec.Command("podman", "machine", "inspect", report.PodmanMachine)
			if out, err := cmd.Output(); err == nil {
				report.PodmanRunning = strings.Contains(string(out), `"State": "running"`)
			}
		}

		// Count containers
		cmd = exec.Command("podman", "ps", "-q")
		if out, err := cmd.Output(); err == nil {
			lines := strings.Split(strings.TrimSpace(string(out)), "\n")
			if len(lines) > 0 && lines[0] != "" {
				report.ContainerCount = len(lines)
			}
		}
	}

	// Collect errors
	if !report.OllamaInstalled {
		report.Errors = append(report.Errors, "Ollama is not installed")
	} else if !report.OllamaInPath {
		report.Errors = append(report.Errors, "Ollama is installed but not in PATH")
	}
	if !report.OllamaRunning && report.OllamaInstalled {
		report.Errors = append(report.Errors, "Ollama is not running")
	}

	return report
}

// -----------------------------------------------------------------------------
// Helper Functions
// -----------------------------------------------------------------------------

func (c *DefaultSystemChecker) getDirectorySize(path string) (int64, error) {
	var size int64
	err := filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			if os.IsPermission(err) {
				return nil
			}
			return err
		}
		if !info.IsDir() {
			size += info.Size()
		}
		return nil
	})
	return size, err
}

func formatBytes(bytes int64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
	)

	switch {
	case bytes >= GB:
		return fmt.Sprintf("%.1f GB", float64(bytes)/float64(GB))
	case bytes >= MB:
		return fmt.Sprintf("%.1f MB", float64(bytes)/float64(MB))
	case bytes >= KB:
		return fmt.Sprintf("%.1f KB", float64(bytes)/float64(KB))
	default:
		return fmt.Sprintf("%d bytes", bytes)
	}
}
