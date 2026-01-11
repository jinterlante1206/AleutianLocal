// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package process

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// ProcessLocker defines the interface for CLI instance locking.
//
// # Description
//
// ProcessLocker prevents multiple CLI instances from running simultaneously,
// avoiding race conditions that could corrupt state or cause unpredictable
// behavior when one instance is starting while another is stopping.
//
// # Thread Safety
//
// Implementations must be safe for use from a single goroutine. The lock
// itself provides inter-process synchronization, not intra-process.
type ProcessLocker interface {
	// Acquire attempts to get an exclusive lock.
	// Returns nil if lock acquired, error otherwise.
	Acquire() error

	// Release releases the lock if held.
	// Safe to call multiple times or if lock was never acquired.
	Release() error

	// IsHeld returns true if this instance currently holds the lock.
	IsHeld() bool

	// HolderPID returns the PID of the process holding the lock.
	// Returns 0 if no process holds the lock or if unable to determine.
	HolderPID() int
}

// ProcessLockConfig configures process lock behavior.
//
// # Description
//
// Allows customization of lock file location and behavior.
//
// # Example
//
//	config := ProcessLockConfig{
//	    LockDir:  "/var/run/aleutian",
//	    LockName: "aleutian",
//	}
type ProcessLockConfig struct {
	// LockDir is the directory for lock files.
	// Default: system temp directory
	LockDir string

	// LockName is the base name for lock files.
	// Default: "aleutian"
	LockName string
}

// DefaultProcessLockConfig returns sensible defaults.
//
// # Description
//
// Uses the system temp directory and "aleutian" as the lock name.
// This ensures the lock file is in a writable location on all platforms.
//
// # Outputs
//
//   - ProcessLockConfig: Configuration with default values
func DefaultProcessLockConfig() ProcessLockConfig {
	return ProcessLockConfig{
		LockDir:  os.TempDir(),
		LockName: "aleutian",
	}
}

// ProcessLock implements ProcessLocker using file-based locking.
//
// # Description
//
// Uses flock(2) system call for advisory file locking. This prevents
// multiple aleutian instances from running mutating operations simultaneously,
// avoiding race conditions like:
//
//   - Terminal A: `aleutian stack start` (waiting for Weaviate)
//   - Terminal B: `aleutian stack stop` (deletes resources A is creating)
//
// # How It Works
//
//  1. Creates a lock file at {LockDir}/{LockName}.lock
//  2. Attempts exclusive flock on the file
//  3. Writes PID to {LockDir}/{LockName}.pid for debugging
//  4. On release, removes PID file and releases flock
//
// # Thread Safety
//
// ProcessLock is NOT safe for concurrent use from multiple goroutines.
// Use from a single goroutine (typically main).
//
// # Limitations
//
//   - Advisory lock only - other processes can ignore it if they don't check
//   - NFS and some network filesystems don't support flock properly
//   - Lock survives if process crashes without calling Release (OS releases flock)
//
// # Assumptions
//
//   - LockDir exists and is writable
//   - Only one ProcessLock instance per process
//   - OS supports flock(2) system call
//
// # Example
//
//	lock := NewProcessLock(DefaultProcessLockConfig())
//	if err := lock.Acquire(); err != nil {
//	    fmt.Fprintf(os.Stderr, "Error: %v\n", err)
//	    os.Exit(1)
//	}
//	defer lock.Release()
//
//	// ... rest of CLI
type ProcessLock struct {
	config   ProcessLockConfig
	lockPath string
	pidPath  string
	lockFile *os.File
	held     bool
}

// NewProcessLock creates a new process lock.
//
// # Description
//
// Creates a ProcessLock configured to use the specified directory
// and name for lock files. Does not acquire the lock.
//
// # Inputs
//
//   - config: Configuration for lock file location
//
// # Outputs
//
//   - *ProcessLock: New lock instance (not yet acquired)
//
// # Example
//
//	lock := NewProcessLock(ProcessLockConfig{
//	    LockDir:  "/var/run/myapp",
//	    LockName: "myapp",
//	})
func NewProcessLock(config ProcessLockConfig) *ProcessLock {
	if config.LockDir == "" {
		config.LockDir = os.TempDir()
	}
	if config.LockName == "" {
		config.LockName = "aleutian"
	}

	return &ProcessLock{
		config:   config,
		lockPath: filepath.Join(config.LockDir, config.LockName+".lock"),
		pidPath:  filepath.Join(config.LockDir, config.LockName+".pid"),
	}
}

// Acquire attempts to get an exclusive lock.
//
// # Description
//
// Uses a non-blocking flock to try to acquire the lock. If another
// process holds the lock, returns immediately with an error containing
// the PID of the holder (if available).
//
// # Outputs
//
//   - error: nil if lock acquired, descriptive error otherwise
//
// # Error Conditions
//
//   - Another aleutian instance is running (returns holder PID)
//   - Cannot create lock file (permission denied, disk full)
//   - Cannot acquire flock (system error)
//
// # Example
//
//	if err := lock.Acquire(); err != nil {
//	    if strings.Contains(err.Error(), "another aleutian instance") {
//	        fmt.Println("Please wait for the other instance to finish")
//	    }
//	    os.Exit(1)
//	}
func (p *ProcessLock) Acquire() error {
	if p.held {
		return nil // Already held
	}

	// Create lock file
	f, err := os.OpenFile(p.lockPath, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return fmt.Errorf("failed to create lock file %s: %w", p.lockPath, err)
	}

	// Try non-blocking exclusive lock
	err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err != nil {
		f.Close()

		// Lock is held by another process
		if err == syscall.EWOULDBLOCK {
			holderPID := p.readHolderPID()
			if holderPID > 0 {
				return fmt.Errorf("another aleutian instance is running (PID %d). "+
					"If this is stale, remove %s", holderPID, p.pidPath)
			}
			return fmt.Errorf("another aleutian instance is running. "+
				"Check: lsof %s", p.lockPath)
		}

		return fmt.Errorf("failed to acquire lock: %w", err)
	}

	p.lockFile = f
	p.held = true

	// Write our PID for debugging
	if err := p.writePID(); err != nil {
		// Non-fatal - lock is still held
		// Log warning but continue
	}

	return nil
}

// Release releases the lock if held.
//
// # Description
//
// Removes the PID file and releases the flock. Safe to call multiple
// times or if the lock was never acquired.
//
// # Outputs
//
//   - error: nil on success, error if release fails
//
// # Example
//
//	defer func() {
//	    if err := lock.Release(); err != nil {
//	        log.Printf("Warning: failed to release lock: %v", err)
//	    }
//	}()
func (p *ProcessLock) Release() error {
	if !p.held || p.lockFile == nil {
		return nil
	}

	// Remove PID file first
	os.Remove(p.pidPath)

	// Release flock
	err := syscall.Flock(int(p.lockFile.Fd()), syscall.LOCK_UN)

	// Close file (also releases lock if flock failed)
	p.lockFile.Close()
	p.lockFile = nil
	p.held = false

	// Optionally remove lock file
	// Note: We leave it for faster subsequent acquires
	// os.Remove(p.lockPath)

	if err != nil {
		return fmt.Errorf("failed to release lock: %w", err)
	}

	return nil
}

// IsHeld returns true if this instance currently holds the lock.
//
// # Description
//
// Checks local state only - does not verify the flock is still valid.
// Useful for conditional cleanup in defer blocks.
//
// # Outputs
//
//   - bool: true if lock is held by this instance
func (p *ProcessLock) IsHeld() bool {
	return p.held
}

// HolderPID returns the PID of the process holding the lock.
//
// # Description
//
// Reads the PID file to determine which process holds the lock.
// Returns 0 if no PID file exists or if unable to read it.
//
// # Outputs
//
//   - int: PID of holder, or 0 if unknown
//
// # Limitations
//
//   - May return stale PID if holder crashed without cleanup
//   - Relies on PID file, which may not exist
func (p *ProcessLock) HolderPID() int {
	return p.readHolderPID()
}

// writePID writes the current process PID to the PID file.
func (p *ProcessLock) writePID() error {
	pid := os.Getpid()
	content := fmt.Sprintf("%d\n", pid)
	return os.WriteFile(p.pidPath, []byte(content), 0644)
}

// readHolderPID reads the PID from the PID file.
func (p *ProcessLock) readHolderPID() int {
	data, err := os.ReadFile(p.pidPath)
	if err != nil {
		return 0
	}

	pidStr := strings.TrimSpace(string(data))
	pid, err := strconv.Atoi(pidStr)
	if err != nil {
		return 0
	}

	return pid
}

// LockPath returns the path to the lock file.
//
// # Description
//
// Useful for error messages and debugging.
//
// # Outputs
//
//   - string: Absolute path to the lock file
func (p *ProcessLock) LockPath() string {
	return p.lockPath
}

// PIDPath returns the path to the PID file.
//
// # Description
//
// Useful for error messages and debugging.
//
// # Outputs
//
//   - string: Absolute path to the PID file
func (p *ProcessLock) PIDPath() string {
	return p.pidPath
}

// ErrLockHeld is returned when the lock is held by another process.
type ErrLockHeld struct {
	HolderPID int
	LockPath  string
}

// Error implements the error interface.
func (e *ErrLockHeld) Error() string {
	if e.HolderPID > 0 {
		return fmt.Sprintf("another aleutian instance is running (PID %d)", e.HolderPID)
	}
	return fmt.Sprintf("another aleutian instance is running (check: lsof %s)", e.LockPath)
}

// Compile-time interface satisfaction check
var _ ProcessLocker = (*ProcessLock)(nil)
