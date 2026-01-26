// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package lock

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestNewFileLockManager(t *testing.T) {
	t.Run("creates manager with defaults", func(t *testing.T) {
		tmpDir := t.TempDir()
		config := DefaultManagerConfig()
		config.LockDir = filepath.Join(tmpDir, "locks")
		config.SessionID = "test-session"
		config.CleanupOnInit = false

		manager, err := NewFileLockManager(config)
		if err != nil {
			t.Fatalf("NewFileLockManager failed: %v", err)
		}
		defer manager.Close()

		// Verify lock directory was created
		if _, err := os.Stat(config.LockDir); os.IsNotExist(err) {
			t.Error("Lock directory was not created")
		}
	})

	t.Run("fails with invalid lock directory", func(t *testing.T) {
		config := DefaultManagerConfig()
		config.LockDir = "/nonexistent/readonly/path/that/should/fail"
		config.CleanupOnInit = false

		_, err := NewFileLockManager(config)
		if err == nil {
			t.Error("Expected error for invalid lock directory")
		}
	})
}

func TestFileLockManager_AcquireRelease(t *testing.T) {
	t.Run("acquire and release lock successfully", func(t *testing.T) {
		tmpDir := t.TempDir()
		manager := createTestManager(t, tmpDir)
		defer manager.Close()

		// Create a test file
		testFile := filepath.Join(tmpDir, "test.txt")
		if err := os.WriteFile(testFile, []byte("test content"), 0644); err != nil {
			t.Fatalf("Failed to create test file: %v", err)
		}

		// Acquire lock
		err := manager.AcquireLock(testFile, "test reason")
		if err != nil {
			t.Fatalf("AcquireLock failed: %v", err)
		}

		// Verify lock is held
		locked, info, err := manager.IsLocked(testFile)
		if err != nil {
			t.Fatalf("IsLocked failed: %v", err)
		}
		if !locked {
			t.Error("Expected file to be locked")
		}
		if info == nil {
			t.Error("Expected lock info")
		} else {
			if info.Reason != "test reason" {
				t.Errorf("Expected reason 'test reason', got %q", info.Reason)
			}
			if info.PID != os.Getpid() {
				t.Errorf("Expected PID %d, got %d", os.Getpid(), info.PID)
			}
		}

		// Release lock
		err = manager.ReleaseLock(testFile)
		if err != nil {
			t.Fatalf("ReleaseLock failed: %v", err)
		}

		// Verify lock is released
		locked, _, err = manager.IsLocked(testFile)
		if err != nil {
			t.Fatalf("IsLocked failed: %v", err)
		}
		if locked {
			t.Error("Expected file to be unlocked")
		}
	})

	t.Run("double acquire same file succeeds", func(t *testing.T) {
		tmpDir := t.TempDir()
		manager := createTestManager(t, tmpDir)
		defer manager.Close()

		testFile := filepath.Join(tmpDir, "test.txt")
		if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
			t.Fatalf("Failed to create test file: %v", err)
		}

		// Acquire twice
		err := manager.AcquireLock(testFile, "first")
		if err != nil {
			t.Fatalf("First AcquireLock failed: %v", err)
		}

		err = manager.AcquireLock(testFile, "second")
		if err != nil {
			t.Fatalf("Second AcquireLock failed: %v", err)
		}

		// Verify reason was updated
		_, info, _ := manager.IsLocked(testFile)
		if info.Reason != "second" {
			t.Errorf("Expected reason 'second', got %q", info.Reason)
		}

		manager.ReleaseLock(testFile)
	})

	t.Run("release without lock returns error", func(t *testing.T) {
		tmpDir := t.TempDir()
		manager := createTestManager(t, tmpDir)
		defer manager.Close()

		testFile := filepath.Join(tmpDir, "test.txt")
		if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
			t.Fatalf("Failed to create test file: %v", err)
		}

		err := manager.ReleaseLock(testFile)
		if !errors.Is(err, ErrLockNotHeld) {
			t.Errorf("Expected ErrLockNotHeld, got %v", err)
		}
	})
}

func TestFileLockManager_ReleaseAll(t *testing.T) {
	tmpDir := t.TempDir()
	manager := createTestManager(t, tmpDir)
	defer manager.Close()

	// Create and lock multiple files
	files := []string{
		filepath.Join(tmpDir, "file1.txt"),
		filepath.Join(tmpDir, "file2.txt"),
		filepath.Join(tmpDir, "file3.txt"),
	}

	for _, f := range files {
		if err := os.WriteFile(f, []byte("test"), 0644); err != nil {
			t.Fatalf("Failed to create test file: %v", err)
		}
		if err := manager.AcquireLock(f, "test"); err != nil {
			t.Fatalf("Failed to lock %s: %v", f, err)
		}
	}

	// Verify all locked
	for _, f := range files {
		locked, _, _ := manager.IsLocked(f)
		if !locked {
			t.Errorf("Expected %s to be locked", f)
		}
	}

	// Release all
	if err := manager.ReleaseAll(); err != nil {
		t.Fatalf("ReleaseAll failed: %v", err)
	}

	// Verify all unlocked
	for _, f := range files {
		locked, _, _ := manager.IsLocked(f)
		if locked {
			t.Errorf("Expected %s to be unlocked", f)
		}
	}
}

func TestFileLockManager_LockInfoFile(t *testing.T) {
	tmpDir := t.TempDir()
	lockDir := filepath.Join(tmpDir, "locks")
	config := DefaultManagerConfig()
	config.LockDir = lockDir
	config.SessionID = "test-session-123"
	config.CleanupOnInit = false

	manager, err := NewFileLockManager(config)
	if err != nil {
		t.Fatalf("NewFileLockManager failed: %v", err)
	}
	defer manager.Close()

	testFile := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Acquire lock
	if err := manager.AcquireLock(testFile, "testing lock info"); err != nil {
		t.Fatalf("AcquireLock failed: %v", err)
	}

	// Verify lock file was created
	entries, err := os.ReadDir(lockDir)
	if err != nil {
		t.Fatalf("Failed to read lock dir: %v", err)
	}

	foundLockFile := false
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) == ".lock" {
			foundLockFile = true
			break
		}
	}

	if !foundLockFile {
		t.Error("Expected .lock file to be created")
	}

	// Release and verify lock file removed
	manager.ReleaseLock(testFile)

	entries, _ = os.ReadDir(lockDir)
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) == ".lock" {
			t.Error("Expected .lock file to be removed after release")
		}
	}
}

func TestFileLockManager_CleanupStaleLocks(t *testing.T) {
	tmpDir := t.TempDir()
	lockDir := filepath.Join(tmpDir, "locks")

	// Create a stale lock file manually
	if err := os.MkdirAll(lockDir, 0755); err != nil {
		t.Fatalf("Failed to create lock dir: %v", err)
	}

	staleLock := LockInfo{
		FilePath:  "/nonexistent/file.txt",
		PID:       999999, // Non-existent PID
		SessionID: "old-session",
		LockedAt:  time.Now().Add(-2 * time.Hour),
		ExpiresAt: time.Now().Add(-1 * time.Hour), // Expired
		Reason:    "old lock",
	}

	lockPath := filepath.Join(lockDir, "stale123456789012.lock")
	data, _ := json.Marshal(staleLock)
	if err := os.WriteFile(lockPath, data, 0644); err != nil {
		t.Fatalf("Failed to create stale lock file: %v", err)
	}

	// Create manager (cleanup on init)
	config := DefaultManagerConfig()
	config.LockDir = lockDir
	config.CleanupOnInit = true

	manager, err := NewFileLockManager(config)
	if err != nil {
		t.Fatalf("NewFileLockManager failed: %v", err)
	}
	defer manager.Close()

	// Verify stale lock was cleaned up
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Error("Expected stale lock file to be removed")
	}
}

func TestFileLockManager_ExternalChangeDetection(t *testing.T) {
	tmpDir := t.TempDir()
	manager := createTestManager(t, tmpDir)
	defer manager.Close()

	testFile := filepath.Join(tmpDir, "watched.txt")
	if err := os.WriteFile(testFile, []byte("initial"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Acquire lock
	if err := manager.AcquireLock(testFile, "watch test"); err != nil {
		t.Fatalf("AcquireLock failed: %v", err)
	}

	// Register callback
	changeDetected := make(chan ExternalChangeEvent, 1)
	manager.RegisterCallback(testFile, func(event ExternalChangeEvent) {
		changeDetected <- event
	})

	// Modify file externally
	time.Sleep(100 * time.Millisecond) // Give watcher time to start
	if err := os.WriteFile(testFile, []byte("modified"), 0644); err != nil {
		t.Fatalf("Failed to modify test file: %v", err)
	}

	// Wait for callback
	select {
	case event := <-changeDetected:
		if event.EventType != ChangeWrite {
			t.Errorf("Expected ChangeWrite, got %v", event.EventType)
		}
	case <-time.After(2 * time.Second):
		t.Error("Timeout waiting for external change callback")
	}

	manager.ReleaseLock(testFile)
}

func TestFileLockManager_ConcurrentAccess(t *testing.T) {
	tmpDir := t.TempDir()
	manager := createTestManager(t, tmpDir)
	defer manager.Close()

	testFile := filepath.Join(tmpDir, "concurrent.txt")
	if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Acquire initial lock
	if err := manager.AcquireLock(testFile, "initial"); err != nil {
		t.Fatalf("AcquireLock failed: %v", err)
	}

	// Try concurrent operations
	var wg sync.WaitGroup
	errors := make(chan error, 10)

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			// These should all succeed (same process)
			if err := manager.AcquireLock(testFile, "concurrent"); err != nil {
				errors <- err
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		t.Errorf("Concurrent acquire failed: %v", err)
	}

	manager.ReleaseLock(testFile)
}

func TestFileLockManager_WatchFile(t *testing.T) {
	tmpDir := t.TempDir()
	manager := createTestManager(t, tmpDir)
	defer manager.Close()

	testFile := filepath.Join(tmpDir, "watch_context.txt")
	if err := os.WriteFile(testFile, []byte("initial"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	changeChan := make(chan string, 1)
	go manager.WatchFile(ctx, testFile, func(path string) {
		changeChan <- path
	})

	// Give watcher time to start
	time.Sleep(100 * time.Millisecond)

	// Acquire lock to enable change detection
	if err := manager.AcquireLock(testFile, "watch test"); err != nil {
		t.Fatalf("AcquireLock failed: %v", err)
	}

	// Modify file
	if err := os.WriteFile(testFile, []byte("changed"), 0644); err != nil {
		t.Fatalf("Failed to modify file: %v", err)
	}

	select {
	case path := <-changeChan:
		absPath, _ := filepath.Abs(testFile)
		if path != absPath {
			t.Errorf("Expected path %s, got %s", absPath, path)
		}
	case <-time.After(2 * time.Second):
		t.Error("Timeout waiting for file change")
	}

	cancel()
	manager.ReleaseLock(testFile)
}

func TestLockInfo_IsExpired(t *testing.T) {
	t.Run("not expired", func(t *testing.T) {
		info := LockInfo{
			ExpiresAt: time.Now().Add(1 * time.Hour),
		}
		if info.IsExpired() {
			t.Error("Expected not expired")
		}
	})

	t.Run("expired", func(t *testing.T) {
		info := LockInfo{
			ExpiresAt: time.Now().Add(-1 * time.Hour),
		}
		if !info.IsExpired() {
			t.Error("Expected expired")
		}
	})
}

func TestIsProcessAlive(t *testing.T) {
	t.Run("current process is alive", func(t *testing.T) {
		if !IsProcessAlive(os.Getpid()) {
			t.Error("Current process should be alive")
		}
	})

	t.Run("non-existent PID", func(t *testing.T) {
		// Use a very high PID that's unlikely to exist
		if IsProcessAlive(999999999) {
			t.Error("Non-existent PID should not be alive")
		}
	})
}

// =============================================================================
// Test helpers
// =============================================================================

func createTestManager(t *testing.T, tmpDir string) *FileLockManager {
	t.Helper()

	config := DefaultManagerConfig()
	config.LockDir = filepath.Join(tmpDir, "locks")
	config.SessionID = "test-session"
	config.CleanupOnInit = false

	manager, err := NewFileLockManager(config)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	return manager
}
