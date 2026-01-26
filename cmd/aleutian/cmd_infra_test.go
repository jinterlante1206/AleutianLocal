// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

// Tests for cmd_infra.go infrastructure helpers

package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// =============================================================================
// Path Analysis Tests
// =============================================================================

func TestResolvePath_NonExistentPath(t *testing.T) {
	// For non-existent paths, should return original
	path := "/nonexistent/path/to/test"
	resolved := resolvePath(path)
	if resolved != path {
		t.Errorf("Expected '%s' for non-existent path, got '%s'", path, resolved)
	}
}

func TestResolvePath_ExistingPath(t *testing.T) {
	// Create a temp directory
	tmpDir, err := os.MkdirTemp("", "aleutian-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Should resolve to the same path (no symlink)
	resolved := resolvePath(tmpDir)
	if resolved == "" {
		t.Error("Expected non-empty resolved path")
	}
}

func TestResolvePath_Symlink(t *testing.T) {
	// Create a temp directory and a symlink to it
	tmpDir, err := os.MkdirTemp("", "aleutian-test-real-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Resolve the tmpDir itself first (on macOS /var -> /private/var)
	resolvedTmpDir := resolvePath(tmpDir)

	symlinkPath := filepath.Join(os.TempDir(), "aleutian-test-symlink")
	os.Remove(symlinkPath) // Clean up any existing symlink
	if err := os.Symlink(tmpDir, symlinkPath); err != nil {
		t.Skipf("Could not create symlink (maybe no permission): %v", err)
	}
	defer os.Remove(symlinkPath)

	resolved := resolvePath(symlinkPath)
	// Compare against the fully resolved tmpDir (handles /var -> /private/var on macOS)
	if resolved != resolvedTmpDir {
		t.Errorf("Expected symlink to resolve to '%s', got '%s'", resolvedTmpDir, resolved)
	}
}

func TestIsExternalPath_HomeDirectory(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("Could not get home directory")
	}

	testPath := filepath.Join(home, "test/path")
	if isExternalPath(testPath) {
		t.Error("Home directory paths should not be considered external")
	}
}

func TestIsExternalPath_TempDirectory(t *testing.T) {
	testPath := "/tmp/some/cache/path"
	if isExternalPath(testPath) {
		t.Error("/tmp paths should not be considered external")
	}
}

func TestIsExternalPath_VolumesPath_Darwin(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("This test is macOS-specific")
	}

	// External volume should be detected
	external := "/Volumes/ExternalDrive/models"
	if !isExternalPath(external) {
		t.Error("/Volumes/ExternalDrive should be considered external on macOS")
	}

	// System volume should not be external
	system := "/Volumes/Macintosh HD/some/path"
	if isExternalPath(system) {
		t.Error("/Volumes/Macintosh HD should not be considered external")
	}
}

func TestIsExternalPath_MntPath_Linux(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("This test is Linux-specific")
	}

	testCases := []struct {
		path     string
		expected bool
	}{
		{"/mnt/nvme/data", true},
		{"/media/user/USB", true},
		{"/run/media/user/disk", true},
		{"/home/user/data", false},
	}

	for _, tc := range testCases {
		result := isExternalPath(tc.path)
		if result != tc.expected {
			t.Errorf("isExternalPath(%s) = %v, expected %v", tc.path, result, tc.expected)
		}
	}
}

func TestExtractMountRoot_Volumes(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("This test is macOS-specific")
	}

	testCases := []struct {
		input    string
		expected string
	}{
		{"/Volumes/ai_models/data/cache", "/Volumes/ai_models"},
		{"/Volumes/ExternalSSD/huggingface/hub", "/Volumes/ExternalSSD"},
		{"/Volumes/Drive", "/Volumes/Drive"},
	}

	for _, tc := range testCases {
		result := extractMountRoot(tc.input)
		if result != tc.expected {
			t.Errorf("extractMountRoot(%s) = %s, expected %s", tc.input, result, tc.expected)
		}
	}
}

func TestExtractMountRoot_LinuxMounts(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("This test is Linux-specific")
	}

	testCases := []struct {
		input    string
		expected string
	}{
		{"/mnt/nvme/aleutian/models", "/mnt/nvme"},
		{"/media/user/SSD/data", "/media/user/SSD"},
		{"/run/media/user/drive/files", "/run/media/user/drive"},
	}

	for _, tc := range testCases {
		result := extractMountRoot(tc.input)
		if result != tc.expected {
			t.Errorf("extractMountRoot(%s) = %s, expected %s", tc.input, result, tc.expected)
		}
	}
}

// =============================================================================
// Platform Detection Tests
// =============================================================================

func TestNeedsMachineMount(t *testing.T) {
	result := needsMachineMount()

	switch runtime.GOOS {
	case "darwin":
		if !result {
			t.Error("needsMachineMount() should return true on macOS")
		}
	case "linux":
		if result {
			t.Error("needsMachineMount() should return false on Linux")
		}
	}
}

// =============================================================================
// Lock File Management Tests
// =============================================================================

func TestClearStaleLocks_NonExistentDir(t *testing.T) {
	// Should not error if locks dir doesn't exist
	err := clearStaleLocks("/nonexistent/cache/path")
	if err != nil {
		t.Errorf("Expected no error for non-existent locks dir, got %v", err)
	}
}

func TestClearStaleLocks_EmptyLocksDir(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "aleutian-lock-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create empty .locks directory
	locksDir := filepath.Join(tmpDir, ".locks")
	if err := os.MkdirAll(locksDir, 0755); err != nil {
		t.Fatal(err)
	}

	err = clearStaleLocks(tmpDir)
	if err != nil {
		t.Errorf("Expected no error for empty locks dir, got %v", err)
	}
}

func TestClearStaleLocks_OnlyStaleLocks(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "aleutian-lock-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create .locks directory
	locksDir := filepath.Join(tmpDir, ".locks")
	if err := os.MkdirAll(locksDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create a "stale" lock file (modify time in the past)
	staleLock := filepath.Join(locksDir, "stale.lock")
	if err := os.WriteFile(staleLock, []byte("lock"), 0644); err != nil {
		t.Fatal(err)
	}
	// Set modification time to 15 minutes ago (stale threshold is 10 min)
	staleTime := time.Now().Add(-15 * time.Minute)
	if err := os.Chtimes(staleLock, staleTime, staleTime); err != nil {
		t.Fatal(err)
	}

	// Create a "fresh" lock file (should not be removed)
	freshLock := filepath.Join(locksDir, "fresh.lock")
	if err := os.WriteFile(freshLock, []byte("lock"), 0644); err != nil {
		t.Fatal(err)
	}

	err = clearStaleLocks(tmpDir)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	// Stale lock should be removed
	if _, err := os.Stat(staleLock); !os.IsNotExist(err) {
		t.Error("Stale lock should have been removed")
	}

	// Fresh lock should still exist
	if _, err := os.Stat(freshLock); err != nil {
		t.Error("Fresh lock should not have been removed")
	}
}

func TestClearStaleLocks_IgnoresNonLockFiles(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "aleutian-lock-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create .locks directory
	locksDir := filepath.Join(tmpDir, ".locks")
	if err := os.MkdirAll(locksDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create a non-lock file with old timestamp
	nonLock := filepath.Join(locksDir, "not-a-lock.txt")
	if err := os.WriteFile(nonLock, []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}
	staleTime := time.Now().Add(-15 * time.Minute)
	os.Chtimes(nonLock, staleTime, staleTime)

	err = clearStaleLocks(tmpDir)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	// Non-lock file should still exist
	if _, err := os.Stat(nonLock); err != nil {
		t.Error("Non-lock files should not be removed")
	}
}

// =============================================================================
// Disk Space Tests
// =============================================================================

func TestCheckDiskSpace_CurrentDir(t *testing.T) {
	// Should have at least 1 MB free in current directory
	err := checkDiskSpace(".", 1)
	if err != nil {
		t.Errorf("Expected current directory to have at least 1 MB free: %v", err)
	}
}

func TestCheckDiskSpace_NonExistentPath(t *testing.T) {
	err := checkDiskSpace("/nonexistent/path", 1)
	if err == nil {
		t.Error("Expected error for non-existent path")
	}
}

func TestCheckDiskSpace_UnrealisticRequirement(t *testing.T) {
	// Require an absurd amount of space (1 petabyte in MB)
	err := checkDiskSpace(".", 1024*1024*1024)
	if err == nil {
		t.Error("Expected error for unrealistic space requirement")
	}
}

// =============================================================================
// File Lock Tests
// =============================================================================

func TestAcquireFileLock_Success(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "aleutian-filelock-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	lockPath := filepath.Join(tmpDir, "test.lock")

	lock, err := acquireFileLock(lockPath, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to acquire lock: %v", err)
	}

	// Lock should exist
	if _, err := os.Stat(lockPath); err != nil {
		t.Error("Lock file should exist after acquire")
	}

	// Release should clean up
	lock.Release()

	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Error("Lock file should be removed after release")
	}
}

func TestAcquireFileLock_BlockedThenSucceed(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "aleutian-filelock-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	lockPath := filepath.Join(tmpDir, "test.lock")

	// Acquire first lock
	lock1, err := acquireFileLock(lockPath, 2*time.Second)
	if err != nil {
		t.Fatalf("Failed to acquire first lock: %v", err)
	}

	// Release lock in background after 100ms
	go func() {
		time.Sleep(100 * time.Millisecond)
		lock1.Release()
	}()

	// Second lock should eventually succeed
	lock2, err := acquireFileLock(lockPath, 2*time.Second)
	if err != nil {
		t.Fatalf("Failed to acquire second lock after first released: %v", err)
	}
	lock2.Release()
}

func TestAcquireFileLock_Timeout(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "aleutian-filelock-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	lockPath := filepath.Join(tmpDir, "test.lock")

	// Acquire first lock
	lock1, err := acquireFileLock(lockPath, 2*time.Second)
	if err != nil {
		t.Fatalf("Failed to acquire first lock: %v", err)
	}
	defer lock1.Release()

	// Second lock attempt should timeout quickly (200ms, less than one retry cycle)
	_, err = acquireFileLock(lockPath, 200*time.Millisecond)
	if err == nil {
		t.Error("Expected timeout error when lock is held")
	}
}

func TestAcquireFileLock_RemovesStaleLock(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "aleutian-filelock-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	lockPath := filepath.Join(tmpDir, "test.lock")

	// Create a stale lock file manually
	if err := os.WriteFile(lockPath, []byte("12345\n"), 0600); err != nil {
		t.Fatal(err)
	}
	// Set modification time to 10 minutes ago (stale threshold is 5 min in acquireFileLock)
	staleTime := time.Now().Add(-10 * time.Minute)
	if err := os.Chtimes(lockPath, staleTime, staleTime); err != nil {
		t.Fatal(err)
	}

	// Should be able to acquire lock by removing stale one
	lock, err := acquireFileLock(lockPath, 5*time.Second)
	if err != nil {
		t.Fatalf("Should have acquired lock by removing stale lock: %v", err)
	}
	lock.Release()
}

// =============================================================================
// Fallback Cache Tests
// =============================================================================

func TestFallbackToLocalCache_CreatesDirectory(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "aleutian-fallback-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	cachePath, err := fallbackToLocalCache(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create fallback cache: %v", err)
	}

	expectedPath := filepath.Join(tmpDir, "models_cache")
	if cachePath != expectedPath {
		t.Errorf("Expected cache path '%s', got '%s'", expectedPath, cachePath)
	}

	// Directory should exist
	if info, err := os.Stat(cachePath); err != nil || !info.IsDir() {
		t.Error("Fallback cache directory should exist and be a directory")
	}
}

func TestFallbackToLocalCache_HandlesExisting(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "aleutian-fallback-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create the cache dir first
	cachePath := filepath.Join(tmpDir, "models_cache")
	if err := os.MkdirAll(cachePath, 0755); err != nil {
		t.Fatal(err)
	}

	// Should still succeed
	result, err := fallbackToLocalCache(tmpDir)
	if err != nil {
		t.Fatalf("Should handle existing directory: %v", err)
	}
	if result != cachePath {
		t.Errorf("Expected '%s', got '%s'", cachePath, result)
	}
}

// =============================================================================
// Integration-style Tests (using actual temp directories)
// =============================================================================

func TestVerifyAndFixExternalCache_NonExternalPath(t *testing.T) {
	// Create temp directory to use as cache
	tmpDir, err := os.MkdirTemp("", "aleutian-cache-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	stackDir, err := os.MkdirTemp("", "aleutian-stack-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(stackDir)

	// For a temp directory path (non-external), should return as-is
	result, err := verifyAndFixExternalCache(tmpDir, stackDir, "podman-machine-default", true)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Should return the resolved path (which may be different on macOS due to /private/tmp)
	// Just verify it's not the fallback
	if filepath.Base(result) == "models_cache" {
		t.Error("Non-external path should not fall back to local cache")
	}
}
