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
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
)

// FileLock provides advisory file locking to prevent concurrent init operations.
//
// # Thread Safety
//
// FileLock is NOT safe for concurrent use. Each goroutine should have its own instance.
//
// # Platform Support
//
// Uses flock(2) on Unix systems. On Windows, uses LockFileEx.
type FileLock struct {
	path string
	file *os.File
}

// NewFileLock creates a new FileLock for the given project root.
//
// # Description
//
// Creates a lock file at {projectRoot}/.aleutian/.lock.
// The .aleutian/ directory is created if it doesn't exist.
//
// # Inputs
//
//   - projectRoot: Absolute path to project root. Must not be empty.
//
// # Outputs
//
//   - *FileLock: The lock instance (not yet acquired).
//   - error: ErrEmptyProjectRoot if projectRoot is empty.
func NewFileLock(projectRoot string) (*FileLock, error) {
	if projectRoot == "" {
		return nil, ErrEmptyProjectRoot
	}

	lockPath := filepath.Join(projectRoot, AleutianDir, LockFileName)
	return &FileLock{path: lockPath}, nil
}

// Acquire attempts to acquire an exclusive lock.
//
// # Description
//
// Creates the lock file and attempts to acquire an exclusive advisory lock.
// If the lock is already held by another process, returns ErrLockHeld.
//
// # Inputs
//
//   - None
//
// # Outputs
//
//   - error: ErrLockHeld if lock is held, or other error on failure.
//
// # Limitations
//
//   - Advisory lock only - other processes can ignore it.
//   - Must call Release() to free the lock.
func (l *FileLock) Acquire() error {
	// Ensure directory exists
	dir := filepath.Dir(l.path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("%w: creating lock directory: %v", ErrLockAcquireFailed, err)
	}

	// Create or open lock file
	file, err := os.OpenFile(l.path, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return fmt.Errorf("%w: opening lock file: %v", ErrLockAcquireFailed, err)
	}

	// Try to acquire exclusive lock (non-blocking)
	err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err != nil {
		file.Close()
		if err == syscall.EWOULDBLOCK {
			return ErrLockHeld
		}
		return fmt.Errorf("%w: flock: %v", ErrLockAcquireFailed, err)
	}

	// Write PID to lock file for debugging
	if err := file.Truncate(0); err != nil {
		// Non-fatal, continue
	}
	if _, err := file.Seek(0, 0); err != nil {
		// Non-fatal, continue
	}
	content := fmt.Sprintf("pid=%d\ntime=%s\n", os.Getpid(), time.Now().Format(time.RFC3339))
	if _, err := file.WriteString(content); err != nil {
		// Non-fatal, continue
	}

	l.file = file
	return nil
}

// Release releases the lock and removes the lock file.
//
// # Description
//
// Releases the advisory lock and closes the file.
// Safe to call multiple times or on an unacquired lock.
//
// # Inputs
//
//   - None
//
// # Outputs
//
//   - error: Any error from closing the file (usually nil).
func (l *FileLock) Release() error {
	if l.file == nil {
		return nil
	}

	// Release the lock
	_ = syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN)

	// Close the file
	err := l.file.Close()
	l.file = nil

	// Try to remove the lock file
	_ = os.Remove(l.path)

	return err
}

// IsHeld checks if the lock is currently held by another process.
//
// # Description
//
// Attempts to acquire the lock non-blocking. If successful, releases it
// immediately. This is a quick check without actually holding the lock.
//
// # Inputs
//
//   - None
//
// # Outputs
//
//   - bool: True if lock is held by another process.
//   - error: Any error (other than lock-held condition).
func (l *FileLock) IsHeld() (bool, error) {
	// Check if lock file exists
	info, err := os.Stat(l.path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	// Check if it's a file
	if info.IsDir() {
		return false, fmt.Errorf("lock path is a directory")
	}

	// Try to acquire lock to check if it's held
	file, err := os.OpenFile(l.path, os.O_RDWR, 0644)
	if err != nil {
		return false, err
	}
	defer file.Close()

	err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err == syscall.EWOULDBLOCK {
		return true, nil
	}
	if err != nil {
		return false, err
	}

	// Lock was acquired, release it
	_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
	return false, nil
}

// HolderPID returns the PID of the process holding the lock.
//
// # Description
//
// Reads the lock file to get the PID of the holder.
// Returns 0 if the lock file doesn't exist or is empty.
//
// # Inputs
//
//   - None
//
// # Outputs
//
//   - int: PID of lock holder, or 0 if unknown.
func (l *FileLock) HolderPID() int {
	content, err := os.ReadFile(l.path)
	if err != nil {
		return 0
	}

	// Parse "pid=12345\n..."
	var pid int
	_, err = fmt.Sscanf(string(content), "pid=%d", &pid)
	if err != nil {
		return 0
	}

	return pid
}

// StaleLockDuration is the time after which a lock is considered stale.
const StaleLockDuration = 1 * time.Hour

// IsStale checks if the lock file appears to be stale.
//
// # Description
//
// A lock is considered stale if:
//   - The lock file is older than StaleLockDuration
//   - The holding process no longer exists
//
// # Inputs
//
//   - None
//
// # Outputs
//
//   - bool: True if the lock appears stale.
func (l *FileLock) IsStale() bool {
	info, err := os.Stat(l.path)
	if err != nil {
		return false
	}

	// Check age
	if time.Since(info.ModTime()) > StaleLockDuration {
		return true
	}

	// Check if holder process exists
	pid := l.HolderPID()
	if pid > 0 {
		// Check if process exists by sending signal 0
		process, err := os.FindProcess(pid)
		if err != nil {
			return true
		}
		err = process.Signal(syscall.Signal(0))
		if err != nil {
			return true // Process doesn't exist
		}
	}

	return false
}

// ForceRelease removes a stale lock file.
//
// # Description
//
// Only call this after confirming the lock is stale via IsStale().
// This is a recovery mechanism for crashed processes.
//
// # Inputs
//
//   - None
//
// # Outputs
//
//   - error: Any error from removing the file.
//
// # Limitations
//
//   - Race condition: Another process could grab the lock between
//     IsStale() and ForceRelease().
func (l *FileLock) ForceRelease() error {
	return os.Remove(l.path)
}

// readPIDFromContent extracts the PID from lock file content.
func readPIDFromContent(content string) int {
	var pid int
	for i := 0; i < len(content); i++ {
		if content[i] >= '0' && content[i] <= '9' {
			start := i
			for i < len(content) && content[i] >= '0' && content[i] <= '9' {
				i++
			}
			pid, _ = strconv.Atoi(content[start:i])
			break
		}
	}
	return pid
}
