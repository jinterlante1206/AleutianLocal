// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.

package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// =============================================================================
// Platform Detection
// =============================================================================

// needsMachineMount returns true if the platform requires podman machine
// volume mounts (macOS runs containers in a VM that needs explicit shares)
func needsMachineMount() bool {
	return runtime.GOOS == "darwin"
}

// =============================================================================
// Path Analysis
// =============================================================================

// resolvePath resolves symlinks to get the actual filesystem path.
// This is important because a symlink in $HOME might point to /Volumes/...
func resolvePath(path string) string {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return path // Return original if can't resolve
	}
	return resolved
}

// isExternalPath determines if a path is on an external/removable mount point
// that may not be automatically accessible to containers.
//
// macOS: /Volumes/* (except boot volume)
// Linux: /mnt/*, /media/*, /run/media/*
func isExternalPath(path string) bool {
	// Resolve symlinks first
	resolved := resolvePath(path)

	switch runtime.GOOS {
	case "darwin":
		// On macOS, /Volumes/ contains mounted drives
		// Exclude paths that are actually on the boot volume
		if strings.HasPrefix(resolved, "/Volumes/") {
			// /Volumes/Macintosh HD is typically the boot volume (symlinked to /)
			// But user volumes like /Volumes/ai_models are external
			parts := strings.Split(resolved, "/")
			if len(parts) >= 3 {
				volumeName := parts[2]
				// Skip if it's the system volume
				if volumeName == "Macintosh HD" || volumeName == "Macintosh HD - Data" {
					return false
				}
				return true
			}
		}
		// Default shared paths on macOS (don't need special handling)
		home, _ := os.UserHomeDir()
		if strings.HasPrefix(resolved, home) {
			return false
		}
		if strings.HasPrefix(resolved, "/tmp") || strings.HasPrefix(resolved, "/private/tmp") {
			return false
		}
		if strings.HasPrefix(resolved, "/var/folders/") || strings.HasPrefix(resolved, "/private/var/folders/") {
			return false
		}

	case "linux":
		// On Linux, these are common external mount points
		// Note: Linux doesn't need machine mounts, but we track this for consistency
		if strings.HasPrefix(resolved, "/mnt/") {
			return true
		}
		if strings.HasPrefix(resolved, "/media/") {
			return true
		}
		if strings.HasPrefix(resolved, "/run/media/") {
			return true
		}
	}

	return false
}

// extractMountRoot extracts the root mount point from a full path.
// This is the path that needs to be mounted in the podman machine.
//
// Examples:
//
//	/Volumes/ai_models/data/cache → /Volumes/ai_models
//	/mnt/nvme/aleutian/models    → /mnt/nvme
//	/media/user/SSD/data         → /media/user/SSD
func extractMountRoot(path string) string {
	resolved := resolvePath(path)

	switch runtime.GOOS {
	case "darwin":
		// /Volumes/DriveName/... → /Volumes/DriveName
		if strings.HasPrefix(resolved, "/Volumes/") {
			parts := strings.Split(resolved, "/")
			if len(parts) >= 3 {
				return "/" + parts[1] + "/" + parts[2]
			}
		}

	case "linux":
		// /mnt/point/... → /mnt/point
		if strings.HasPrefix(resolved, "/mnt/") {
			parts := strings.Split(resolved, "/")
			if len(parts) >= 3 {
				return "/" + parts[1] + "/" + parts[2]
			}
		}
		// /media/user/drive/... → /media/user/drive
		if strings.HasPrefix(resolved, "/media/") {
			parts := strings.Split(resolved, "/")
			if len(parts) >= 4 {
				return "/" + parts[1] + "/" + parts[2] + "/" + parts[3]
			}
		}
		// /run/media/user/drive/... → /run/media/user/drive
		if strings.HasPrefix(resolved, "/run/media/") {
			parts := strings.Split(resolved, "/")
			if len(parts) >= 5 {
				return "/" + parts[1] + "/" + parts[2] + "/" + parts[3] + "/" + parts[4]
			}
		}
	}

	return resolved // Return as-is if no pattern matches
}

// =============================================================================
// Podman Version Detection
// =============================================================================

// getPodmanVersion returns the major and minor version of podman
func getPodmanVersion() (major, minor int, err error) {
	cmd := exec.Command("podman", "version", "--format", "{{.Client.Version}}")
	out, err := cmd.Output()
	if err != nil {
		return 0, 0, fmt.Errorf("failed to get podman version: %w", err)
	}

	version := strings.TrimSpace(string(out))
	// Parse "5.0.2" or "4.9.1"
	re := regexp.MustCompile(`^(\d+)\.(\d+)`)
	matches := re.FindStringSubmatch(version)
	if len(matches) < 3 {
		return 0, 0, fmt.Errorf("could not parse version: %s", version)
	}

	major, _ = strconv.Atoi(matches[1])
	minor, _ = strconv.Atoi(matches[2])
	return major, minor, nil
}

// supportsMachineSet checks if the podman version supports `machine set --volume`
// This was added in podman 4.5+
func supportsMachineSet() bool {
	major, minor, err := getPodmanVersion()
	if err != nil {
		return false
	}
	return major >= 5 || (major == 4 && minor >= 5)
}

// =============================================================================
// Container/Machine State Checks
// =============================================================================

// hasRunningContainers checks if there are any running containers
// (not just Aleutian ones - any container)
func hasRunningContainers() bool {
	cmd := exec.Command("podman", "ps", "-q")
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) != ""
}

// hasRunningAleutianContainers checks specifically for running Aleutian containers
func hasRunningAleutianContainers() bool {
	// Use name filter as it's more reliable than compose project label
	cmd := exec.Command("podman", "ps", "-q", "--filter", "name=aleutian-")
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) != ""
}

// canAccessPath verifies that containers can access the given path.
// On macOS, this tests via podman machine ssh.
// On Linux, this just checks if the path exists (containers have direct access).
func canAccessPath(path string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	switch runtime.GOOS {
	case "darwin":
		// Test via podman machine ssh - this checks if the VM can see the path
		cmd := exec.CommandContext(ctx, "podman", "machine", "ssh", "ls", path)
		return cmd.Run() == nil

	case "linux":
		// On Linux, just check if path exists and is accessible
		_, err := os.Stat(path)
		return err == nil

	default:
		// Unknown platform, assume accessible
		return true
	}
}

// =============================================================================
// Machine Mount Management
// =============================================================================

// ensurePathAccessible adds a volume mount to the podman machine if needed.
// This may require stopping and restarting the machine.
// Returns nil if successful or if no action was needed.
func ensurePathAccessible(machineName, mountPath string, autoFix bool) error {
	if !needsMachineMount() {
		// Linux: just verify path exists
		if _, err := os.Stat(mountPath); err != nil {
			return fmt.Errorf("path %s is not accessible: %w", mountPath, err)
		}
		return nil
	}

	// macOS: need to add mount to podman machine
	mountSpec := fmt.Sprintf("%s:%s", mountPath, mountPath)

	// Check for running containers first
	if hasRunningContainers() {
		if !autoFix {
			fmt.Println("\n⚠️  Running containers detected.")
			fmt.Println("   Adding a mount requires restarting the podman machine,")
			fmt.Println("   which will terminate all running containers.")
			fmt.Print("\n   Continue anyway? (yes/no): ")

			reader := bufio.NewReader(os.Stdin)
			input, _ := reader.ReadString('\n')
			if strings.ToLower(strings.TrimSpace(input)) != "yes" {
				return fmt.Errorf("aborted: user declined to restart machine with running containers")
			}
		} else {
			fmt.Println("⚠️  Auto-fix enabled: will restart machine with running containers")
		}
	}

	// Try `podman machine set` first (podman 4.5+)
	if supportsMachineSet() {
		fmt.Printf("   Adding mount %s to podman machine...\n", mountPath)

		// Stop the machine
		fmt.Print("   Stopping machine... ")
		stopCmd := exec.Command("podman", "machine", "stop", machineName)
		if err := stopCmd.Run(); err != nil {
			fmt.Println("failed")
			return fmt.Errorf("failed to stop machine: %w", err)
		}
		fmt.Println("done")

		// Add the volume mount
		fmt.Print("   Adding volume mount... ")
		setCmd := exec.Command("podman", "machine", "set", machineName, "-v", mountSpec)
		if err := setCmd.Run(); err != nil {
			fmt.Println("failed")
			// Try to restart the machine anyway
			exec.Command("podman", "machine", "start", machineName).Run()
			return fmt.Errorf("failed to add mount (podman machine set): %w", err)
		}
		fmt.Println("done")

		// Restart the machine
		fmt.Print("   Starting machine... ")
		startCmd := exec.Command("podman", "machine", "start", machineName)
		if err := startCmd.Run(); err != nil {
			fmt.Println("failed")
			return fmt.Errorf("failed to restart machine: %w", err)
		}
		fmt.Println("done")

		return nil
	}

	// Fallback: older podman requires machine rm/init
	fmt.Println("\n   Your podman version doesn't support hot-patching mounts.")
	fmt.Println("   The machine needs to be recreated to add the mount.")
	fmt.Println("   (This only removes the VM - your data will be preserved)")

	if !autoFix {
		fmt.Print("\n   Recreate machine now? (yes/no): ")
		reader := bufio.NewReader(os.Stdin)
		input, _ := reader.ReadString('\n')
		if strings.ToLower(strings.TrimSpace(input)) != "yes" {
			return fmt.Errorf("aborted: user declined to recreate machine")
		}
	}

	// Get current machine config for recreation
	// Note: This is a simplified version - full implementation would parse
	// existing machine config to preserve CPU/memory settings
	return fmt.Errorf("machine recreation required - please run: podman machine rm %s && aleutian stack start", machineName)
}

// =============================================================================
// Lock File Management
// =============================================================================

const lockStaleThreshold = 10 * time.Minute

// clearStaleLocks removes stale HuggingFace lock files that can block model downloads.
// Only removes locks older than lockStaleThreshold to avoid race conditions.
func clearStaleLocks(cachePath string) error {
	locksDir := filepath.Join(cachePath, ".locks")

	// Check if locks directory exists
	if _, err := os.Stat(locksDir); os.IsNotExist(err) {
		return nil // Nothing to do
	}

	now := time.Now()
	clearedCount := 0

	err := filepath.Walk(locksDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			// Log but continue
			return nil
		}

		// Skip directories
		if info.IsDir() {
			return nil
		}

		// Check if it's a lock file
		if !strings.HasSuffix(info.Name(), ".lock") {
			return nil
		}

		// Check age - only delete if older than threshold
		age := now.Sub(info.ModTime())
		if age > lockStaleThreshold {
			relativePath, _ := filepath.Rel(cachePath, path)
			if err := os.Remove(path); err != nil {
				// Log warning but continue
				fmt.Printf("   ⚠️  Could not remove stale lock: %s (%v)\n", relativePath, err)
			} else {
				clearedCount++
				if clearedCount <= 3 { // Only show first few
					fmt.Printf("   Cleared stale lock: %s (age: %s)\n", relativePath, age.Round(time.Minute))
				}
			}
		}

		return nil
	})

	if clearedCount > 3 {
		fmt.Printf("   ... and %d more stale locks cleared\n", clearedCount-3)
	}

	return err
}

// =============================================================================
// Disk Space Check
// =============================================================================

// checkDiskSpace verifies there's enough free space at the given path.
// Returns nil if sufficient space, error otherwise.
func checkDiskSpace(path string, requiredMB int64) error {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return fmt.Errorf("could not check disk space: %w", err)
	}

	// Available space in MB
	availableMB := int64(stat.Bavail) * int64(stat.Bsize) / (1024 * 1024)

	if availableMB < requiredMB {
		return fmt.Errorf("insufficient disk space: %d MB available, %d MB required", availableMB, requiredMB)
	}

	return nil
}

// =============================================================================
// File Locking (Prevent Concurrent Modifications)
// =============================================================================

// FileLock represents a file-based lock for preventing concurrent operations
type FileLock struct {
	path string
	file *os.File
}

// acquireFileLock attempts to acquire an exclusive lock on the given path.
// Returns error if lock cannot be acquired within timeout.
func acquireFileLock(lockPath string, timeout time.Duration) (*FileLock, error) {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		// Try to create the lock file exclusively
		file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err == nil {
			// Write PID for debugging
			fmt.Fprintf(file, "%d\n", os.Getpid())
			return &FileLock{path: lockPath, file: file}, nil
		}

		// Check if existing lock is stale (process died)
		if info, statErr := os.Stat(lockPath); statErr == nil {
			if time.Since(info.ModTime()) > 5*time.Minute {
				// Stale lock, remove and retry
				os.Remove(lockPath)
				continue
			}
		}

		time.Sleep(500 * time.Millisecond)
	}

	return nil, fmt.Errorf("could not acquire lock within %v (another aleutian process may be running)", timeout)
}

// Release releases the file lock
func (l *FileLock) Release() {
	if l.file != nil {
		l.file.Close()
	}
	os.Remove(l.path)
}

// =============================================================================
// Main Orchestrator: verifyAndFixExternalCache
// =============================================================================

// verifyAndFixExternalCache ensures the model cache path is accessible to containers.
// If the path is on an external drive that's not mounted in the podman machine,
// it will attempt to add the mount or fall back to a local cache.
//
// Parameters:
//   - cachePath: The detected or configured cache path
//   - stackDir: The local stack directory (for fallback)
//   - machineName: The podman machine name (macOS only)
//   - autoFix: If true, fix issues without prompting
//
// Returns:
//   - string: The final cache path to use (may be different if fallback was needed)
//   - error: Only for fatal errors that should stop startup
func verifyAndFixExternalCache(cachePath, stackDir, machineName string, autoFix bool) (string, error) {
	// Resolve any symlinks to get the real path
	resolvedPath := resolvePath(cachePath)
	if resolvedPath != cachePath {
		fmt.Printf("   (Resolved symlink: %s → %s)\n", cachePath, resolvedPath)
		cachePath = resolvedPath
	}

	// If not an external path, just verify it exists and clear locks
	if !isExternalPath(cachePath) {
		if err := clearStaleLocks(cachePath); err != nil {
			fmt.Printf("   ⚠️  Warning: Could not clear stale locks: %v\n", err)
		}
		return cachePath, nil
	}

	// External path detected
	mountRoot := extractMountRoot(cachePath)
	fmt.Printf("   External cache detected: %s\n", cachePath)
	fmt.Printf("   Mount root: %s\n", mountRoot)

	// Check if it's already accessible
	if canAccessPath(mountRoot) {
		fmt.Printf("   ✓ Mount is accessible to containers\n")

		// Check disk space (warn but don't fail)
		if err := checkDiskSpace(cachePath, 2048); err != nil {
			fmt.Printf("   ⚠️  Warning: %v\n", err)
		}

		if err := clearStaleLocks(cachePath); err != nil {
			fmt.Printf("   ⚠️  Warning: Could not clear stale locks: %v\n", err)
		}

		return cachePath, nil
	}

	// Mount is not accessible
	fmt.Println()
	fmt.Println("╔═══════════════════════════════════════════════════════════════════╗")
	fmt.Println("║           ⚠️  EXTERNAL CACHE NOT ACCESSIBLE                        ║")
	fmt.Println("╚═══════════════════════════════════════════════════════════════════╝")
	fmt.Printf("\n   Cache path: %s\n", cachePath)
	fmt.Printf("   Mount root: %s\n", mountRoot)

	if needsMachineMount() {
		fmt.Println("\n   The podman machine VM cannot access this external drive.")
		fmt.Println("   A volume mount needs to be added to the machine configuration.")

		// Try to fix
		if autoFix || promptYesNo("   Add mount and restart podman machine?") {
			// Acquire lock to prevent concurrent modifications
			lockPath := filepath.Join(os.TempDir(), "aleutian-machine-config.lock")
			lock, err := acquireFileLock(lockPath, 30*time.Second)
			if err != nil {
				fmt.Printf("   ⚠️  %v\n", err)
				fmt.Println("   Falling back to local cache...")
				return fallbackToLocalCache(stackDir)
			}
			defer lock.Release()

			if err := ensurePathAccessible(machineName, mountRoot, autoFix); err != nil {
				fmt.Printf("   ⚠️  Failed to add mount: %v\n", err)
				fmt.Println("   Falling back to local cache...")
				return fallbackToLocalCache(stackDir)
			}

			// Verify it worked
			if canAccessPath(mountRoot) {
				fmt.Println("   ✓ Mount added successfully!")
				if err := clearStaleLocks(cachePath); err != nil {
					fmt.Printf("   ⚠️  Warning: Could not clear stale locks: %v\n", err)
				}
				return cachePath, nil
			} else {
				fmt.Println("   ⚠️  Mount still not accessible after fix attempt")
				return fallbackToLocalCache(stackDir)
			}
		} else {
			fmt.Println("   Falling back to local cache...")
			return fallbackToLocalCache(stackDir)
		}
	} else {
		// Linux - path should be accessible but isn't
		fmt.Println("\n   The path exists but may have permission issues.")
		fmt.Println("   Check that the path is readable by your user.")
		fmt.Printf("   Try: ls -la %s\n", mountRoot)
		return fallbackToLocalCache(stackDir)
	}
}

// fallbackToLocalCache creates and returns the local cache path
func fallbackToLocalCache(stackDir string) (string, error) {
	localCache := filepath.Join(stackDir, "models_cache")
	if err := os.MkdirAll(localCache, 0755); err != nil {
		return "", fmt.Errorf("failed to create local cache directory: %w", err)
	}
	fmt.Printf("   Using local cache: %s\n", localCache)
	return localCache, nil
}

// promptYesNo displays a yes/no prompt and returns true for yes
func promptYesNo(message string) bool {
	fmt.Printf("%s (yes/no): ", message)
	reader := bufio.NewReader(os.Stdin)
	input, _ := reader.ReadString('\n')
	input = strings.ToLower(strings.TrimSpace(input))
	return input == "yes" || input == "y"
}
