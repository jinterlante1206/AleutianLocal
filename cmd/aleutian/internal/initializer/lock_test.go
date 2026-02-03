// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package initializer

import (
	"os"
	"path/filepath"
	"testing"
)

// TestFileLock_AcquireRelease tests basic lock acquire and release.
func TestFileLock_AcquireRelease(t *testing.T) {
	tempDir := t.TempDir()

	lock, err := NewFileLock(tempDir)
	if err != nil {
		t.Fatalf("NewFileLock failed: %v", err)
	}

	// Acquire lock
	if err := lock.Acquire(); err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}

	// Verify lock file exists
	lockPath := filepath.Join(tempDir, AleutianDir, LockFileName)
	if _, err := os.Stat(lockPath); err != nil {
		t.Errorf("Lock file not created: %v", err)
	}

	// Release lock
	if err := lock.Release(); err != nil {
		t.Errorf("Release failed: %v", err)
	}
}

// TestFileLock_DoubleAcquire tests that same process can't acquire twice.
func TestFileLock_DoubleAcquire(t *testing.T) {
	tempDir := t.TempDir()

	lock1, _ := NewFileLock(tempDir)
	lock2, _ := NewFileLock(tempDir)

	// First acquire
	if err := lock1.Acquire(); err != nil {
		t.Fatalf("First acquire failed: %v", err)
	}
	defer lock1.Release()

	// Second acquire should fail
	err := lock2.Acquire()
	if err == nil {
		lock2.Release()
		t.Error("Second acquire should fail")
	}
}

// TestFileLock_IsHeld tests lock held detection.
func TestFileLock_IsHeld(t *testing.T) {
	tempDir := t.TempDir()

	lock, _ := NewFileLock(tempDir)

	// Should not be held initially
	held, err := lock.IsHeld()
	if err != nil {
		t.Fatalf("IsHeld failed: %v", err)
	}
	if held {
		t.Error("IsHeld returned true for unheld lock")
	}

	// Acquire
	if err := lock.Acquire(); err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}

	// Check with another lock instance
	lock2, _ := NewFileLock(tempDir)
	held, err = lock2.IsHeld()
	if err != nil {
		t.Fatalf("IsHeld failed: %v", err)
	}
	if !held {
		t.Error("IsHeld returned false for held lock")
	}

	// Release
	lock.Release()

	// Should not be held after release
	held, err = lock2.IsHeld()
	if err != nil {
		t.Fatalf("IsHeld failed after release: %v", err)
	}
	if held {
		t.Error("IsHeld returned true after release")
	}
}

// TestFileLock_HolderPID tests PID retrieval.
func TestFileLock_HolderPID(t *testing.T) {
	tempDir := t.TempDir()

	lock, _ := NewFileLock(tempDir)

	// No PID before acquire
	pid := lock.HolderPID()
	if pid != 0 {
		t.Errorf("HolderPID = %d, want 0 before acquire", pid)
	}

	// Acquire
	if err := lock.Acquire(); err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}
	defer lock.Release()

	// Should have our PID
	pid = lock.HolderPID()
	if pid != os.Getpid() {
		t.Errorf("HolderPID = %d, want %d", pid, os.Getpid())
	}
}

// TestFileLock_EmptyProjectRoot tests error on empty project root.
func TestFileLock_EmptyProjectRoot(t *testing.T) {
	_, err := NewFileLock("")
	if err == nil {
		t.Error("NewFileLock should fail with empty project root")
	}
}

// TestFileLock_ReleaseWithoutAcquire tests releasing unheld lock.
func TestFileLock_ReleaseWithoutAcquire(t *testing.T) {
	tempDir := t.TempDir()

	lock, _ := NewFileLock(tempDir)

	// Release without acquire should not error
	err := lock.Release()
	if err != nil {
		t.Errorf("Release without acquire failed: %v", err)
	}
}

// TestFileLock_DoubleRelease tests releasing lock twice.
func TestFileLock_DoubleRelease(t *testing.T) {
	tempDir := t.TempDir()

	lock, _ := NewFileLock(tempDir)

	if err := lock.Acquire(); err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}

	// First release
	if err := lock.Release(); err != nil {
		t.Errorf("First release failed: %v", err)
	}

	// Second release should not error
	if err := lock.Release(); err != nil {
		t.Errorf("Second release failed: %v", err)
	}
}

// TestFileLock_ForceRelease tests force release of lock file.
func TestFileLock_ForceRelease(t *testing.T) {
	tempDir := t.TempDir()

	lock, _ := NewFileLock(tempDir)

	// Create lock file manually (simulate crashed process)
	aleutianDir := filepath.Join(tempDir, AleutianDir)
	if err := os.MkdirAll(aleutianDir, 0755); err != nil {
		t.Fatalf("Failed to create directory: %v", err)
	}
	lockPath := filepath.Join(aleutianDir, LockFileName)
	if err := os.WriteFile(lockPath, []byte("pid=99999\n"), 0644); err != nil {
		t.Fatalf("Failed to create lock file: %v", err)
	}

	// Force release
	if err := lock.ForceRelease(); err != nil {
		t.Errorf("ForceRelease failed: %v", err)
	}

	// Lock file should be gone
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Error("Lock file still exists after ForceRelease")
	}
}
