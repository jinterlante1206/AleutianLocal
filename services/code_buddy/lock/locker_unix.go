// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

//go:build unix

package lock

import (
	"os"
	"syscall"
)

// UnixFileLocker implements FileLocker using syscall.Flock.
//
// # Description
//
// Uses advisory file locking via flock(2). Locks are:
// - Process-scoped (inherited by child processes)
// - Released on file close or process exit
// - Non-blocking when LOCK_NB is specified
//
// # Thread Safety
//
// Safe for concurrent use on different files.
type UnixFileLocker struct{}

// Lock acquires an exclusive lock using flock(2).
//
// # Description
//
// Uses LOCK_EX|LOCK_NB for non-blocking exclusive lock.
// Returns immediately if the file is already locked.
//
// # Inputs
//
//   - f: Open file handle to lock.
//
// # Outputs
//
//   - error: nil on success, ErrFileLocked if already locked by another process.
func (l *UnixFileLocker) Lock(f *os.File) error {
	err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err != nil {
		if err == syscall.EWOULDBLOCK {
			return ErrFileLocked
		}
		return err
	}
	return nil
}

// Unlock releases the lock using flock(2).
//
// # Description
//
// Uses LOCK_UN to release the lock. Safe to call even if not locked.
//
// # Inputs
//
//   - f: Open file handle to unlock.
//
// # Outputs
//
//   - error: nil on success, error on system failure.
func (l *UnixFileLocker) Unlock(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
}

// isProcessAlive checks if a process exists using kill -0.
//
// # Description
//
// Sends signal 0 to the process, which checks existence without affecting it.
// Returns false if the process doesn't exist or we lack permission.
//
// # Inputs
//
//   - pid: Process ID to check.
//
// # Outputs
//
//   - bool: True if process exists and we can signal it.
func isProcessAlive(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// Signal 0 doesn't actually send anything, just checks if process exists
	err = process.Signal(syscall.Signal(0))
	return err == nil
}

// newPlatformLocker returns a Unix-specific file locker.
func newPlatformLocker() FileLocker {
	return &UnixFileLocker{}
}
