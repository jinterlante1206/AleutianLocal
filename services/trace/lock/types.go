// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

// Package lock provides file locking capabilities for safe concurrent file operations.
//
// # Description
//
// This package implements advisory file locking to prevent conflicts when the agent
// is editing files. It detects external modifications via fsnotify and supports
// stale lock cleanup via PID checks and TTL expiration.
//
// # Thread Safety
//
// FileLockManager is safe for concurrent use. All public methods are synchronized.
package lock

import (
	"time"
)

// LockInfo contains metadata about a held lock.
//
// # Description
//
// Stored in JSON format in .aleutian/locks/<hash>.lock files for visibility
// and debugging. Includes PID for stale lock detection.
//
// # Fields
//
//   - FilePath: Absolute path to the locked file.
//   - PID: Process ID of the lock holder.
//   - SessionID: Aleutian session ID for correlation.
//   - LockedAt: When the lock was acquired.
//   - ExpiresAt: When the lock expires (for stale detection).
//   - Reason: Human-readable reason for the lock.
type LockInfo struct {
	FilePath  string    `json:"file_path"`
	PID       int       `json:"pid"`
	SessionID string    `json:"session_id"`
	LockedAt  time.Time `json:"locked_at"`
	ExpiresAt time.Time `json:"expires_at"`
	Reason    string    `json:"reason"`
}

// IsExpired checks if the lock has passed its TTL.
//
// # Description
//
// Returns true if the current time is after ExpiresAt.
// Expired locks are treated as stale and can be cleaned up.
//
// # Outputs
//
//   - bool: True if the lock has expired.
func (l *LockInfo) IsExpired() bool {
	return time.Now().After(l.ExpiresAt)
}

// lockEntry represents an active lock held by this process.
//
// # Description
//
// Internal struct tracking the file handle and paths for a lock.
// Used by FileLockManager to manage held locks.
type lockEntry struct {
	file     interface{} // Platform-specific file handle
	path     string      // Path to the locked file
	lockPath string      // Path to the .lock info file
	info     *LockInfo   // Lock metadata
}

// ManagerConfig configures the FileLockManager behavior.
//
// # Description
//
// Allows customization of lock directory, TTL, and session identification.
// All fields have sensible defaults if not specified.
//
// # Fields
//
//   - LockDir: Directory for lock files (default: .aleutian/locks).
//   - SessionID: Session identifier for lock ownership.
//   - DefaultTTL: Default lock expiration time (default: 1 hour).
//   - CleanupOnInit: Run stale lock cleanup on initialization.
type ManagerConfig struct {
	LockDir       string
	SessionID     string
	DefaultTTL    time.Duration
	CleanupOnInit bool
}

// DefaultManagerConfig returns a ManagerConfig with sensible defaults.
//
// # Description
//
// Creates a configuration suitable for most use cases.
// LockDir defaults to .aleutian/locks, TTL defaults to 1 hour.
//
// # Outputs
//
//   - ManagerConfig: Configuration with default values.
func DefaultManagerConfig() ManagerConfig {
	return ManagerConfig{
		LockDir:       ".aleutian/locks",
		SessionID:     "",
		DefaultTTL:    time.Hour,
		CleanupOnInit: true,
	}
}

// ExternalChangeEvent represents a file modification detected by the watcher.
//
// # Description
//
// Sent to callbacks when a locked file is modified externally.
// Includes the path and type of change detected.
//
// # Fields
//
//   - Path: Absolute path to the modified file.
//   - EventType: Type of change (write, delete, rename).
type ExternalChangeEvent struct {
	Path      string
	EventType ChangeType
}

// ChangeType indicates the type of external file change.
type ChangeType int

const (
	// ChangeWrite indicates the file content was modified.
	ChangeWrite ChangeType = iota
	// ChangeDelete indicates the file was deleted.
	ChangeDelete
	// ChangeRename indicates the file was renamed.
	ChangeRename
)

// String returns a human-readable name for the change type.
func (c ChangeType) String() string {
	switch c {
	case ChangeWrite:
		return "write"
	case ChangeDelete:
		return "delete"
	case ChangeRename:
		return "rename"
	default:
		return "unknown"
	}
}
