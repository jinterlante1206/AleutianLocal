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
	"errors"
	"fmt"
)

// Sentinel errors for lock operations.
var (
	// ErrFileLocked indicates the file is already locked by another process.
	ErrFileLocked = errors.New("file is locked by another process")

	// ErrLockNotHeld indicates an attempt to release a lock not held by this manager.
	ErrLockNotHeld = errors.New("lock not held by this process")

	// ErrExternalModification indicates the file was modified while locked.
	ErrExternalModification = errors.New("file was modified externally while locked")

	// ErrLockExpired indicates the lock has passed its TTL.
	ErrLockExpired = errors.New("lock has expired")

	// ErrInvalidPath indicates an invalid file path was provided.
	ErrInvalidPath = errors.New("invalid file path")
)

// FileLockError provides detailed information about a lock conflict.
//
// # Description
//
// Wraps ErrFileLocked with information about the current lock holder,
// allowing the caller to decide how to proceed (wait, abort, force).
//
// # Fields
//
//   - Path: The file that is locked.
//   - Holder: Information about the current lock holder.
//   - Err: The underlying error (typically ErrFileLocked).
type FileLockError struct {
	Path   string
	Holder *LockInfo
	Err    error
}

// Error returns a human-readable error message.
func (e *FileLockError) Error() string {
	if e.Holder != nil {
		return fmt.Sprintf("file %s is locked by PID %d (session %s) since %s: %v",
			e.Path, e.Holder.PID, e.Holder.SessionID,
			e.Holder.LockedAt.Format("15:04:05"), e.Err)
	}
	return fmt.Sprintf("file %s is locked: %v", e.Path, e.Err)
}

// Unwrap returns the underlying error for errors.Is/As support.
func (e *FileLockError) Unwrap() error {
	return e.Err
}

// ExternalModificationError provides details about an external file change.
//
// # Description
//
// Wraps ErrExternalModification with information about what changed,
// allowing the caller to reload, abort, or ask the user.
//
// # Fields
//
//   - Path: The file that was modified.
//   - ChangeType: Type of modification (write, delete, rename).
type ExternalModificationError struct {
	Path       string
	ChangeType ChangeType
}

// Error returns a human-readable error message.
func (e *ExternalModificationError) Error() string {
	return fmt.Sprintf("file %s was %s externally while locked", e.Path, e.ChangeType)
}

// Unwrap returns the underlying error for errors.Is/As support.
func (e *ExternalModificationError) Unwrap() error {
	return ErrExternalModification
}

// RaceConditionError indicates a race between lock acquisition and file change.
//
// # Description
//
// Occurs when a file is modified between starting to watch and acquiring
// the lock. The safe response is to re-read and retry.
//
// # Fields
//
//   - Path: The file with the race condition.
//   - Reason: Description of what happened.
type RaceConditionError struct {
	Path   string
	Reason string
}

// Error returns a human-readable error message.
func (e *RaceConditionError) Error() string {
	return fmt.Sprintf("race condition on %s: %s", e.Path, e.Reason)
}
