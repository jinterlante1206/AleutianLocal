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
	"os"
)

// FileLocker abstracts platform-specific file locking operations.
//
// # Description
//
// Provides a unified interface for file locking across Unix and Windows.
// Unix uses syscall.Flock, Windows uses LockFileEx.
//
// # Thread Safety
//
// Implementations must be safe for concurrent use on different files.
// Locking the same file from multiple goroutines is undefined behavior.
type FileLocker interface {
	// Lock acquires an exclusive lock on the file.
	//
	// # Description
	//
	// Attempts to acquire an exclusive (write) lock on the file.
	// Non-blocking: returns immediately if lock cannot be acquired.
	//
	// # Inputs
	//
	//   - f: Open file handle to lock.
	//
	// # Outputs
	//
	//   - error: nil on success, ErrFileLocked if already locked.
	Lock(f *os.File) error

	// Unlock releases the lock on the file.
	//
	// # Description
	//
	// Releases a previously acquired lock. Safe to call even if not locked.
	//
	// # Inputs
	//
	//   - f: Open file handle to unlock.
	//
	// # Outputs
	//
	//   - error: nil on success, error on system failure.
	Unlock(f *os.File) error
}

// IsProcessAlive checks if a process with the given PID is still running.
//
// # Description
//
// Used for stale lock detection. On Unix, uses kill -0.
// On Windows, uses OpenProcess.
//
// # Inputs
//
//   - pid: Process ID to check.
//
// # Outputs
//
//   - bool: True if process exists, false otherwise.
//
// # Platform Notes
//
// This function is implemented in platform-specific files.
func IsProcessAlive(pid int) bool {
	return isProcessAlive(pid)
}

// newFileLocker creates a platform-appropriate FileLocker.
//
// # Description
//
// Returns UnixFileLocker on Linux/macOS, WindowsFileLocker on Windows.
// Used internally by FileLockManager.
//
// # Outputs
//
//   - FileLocker: Platform-specific implementation.
func newFileLocker() FileLocker {
	return newPlatformLocker()
}
