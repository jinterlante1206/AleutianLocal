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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// FileLockManager manages file locks for safe concurrent file operations.
//
// # Description
//
// Provides exclusive file locking with:
// - Advisory locks via syscall.Flock (Unix) or LockFileEx (Windows)
// - External change detection via fsnotify
// - Stale lock cleanup via PID checks and TTL expiration
// - Lock info files for debugging and visibility
//
// # Thread Safety
//
// All public methods are safe for concurrent use from multiple goroutines.
type FileLockManager struct {
	lockDir    string
	sessionID  string
	defaultTTL time.Duration
	locker     FileLocker
	locks      map[string]*lockEntry
	mu         sync.Mutex
	watcher    *fsnotify.Watcher
	watcherMu  sync.Mutex
	callbacks  map[string][]func(ExternalChangeEvent)
}

// NewFileLockManager creates a new file lock manager.
//
// # Description
//
// Creates a manager with the specified configuration. If CleanupOnInit is true,
// stale locks from crashed processes are cleaned up on creation.
//
// # Inputs
//
//   - config: Manager configuration. Use DefaultManagerConfig() for defaults.
//
// # Outputs
//
//   - *FileLockManager: Ready-to-use lock manager.
//   - error: Non-nil if setup fails (e.g., can't create lock directory).
//
// # Example
//
//	config := lock.DefaultManagerConfig()
//	config.SessionID = "sess-abc123"
//	manager, err := lock.NewFileLockManager(config)
func NewFileLockManager(config ManagerConfig) (*FileLockManager, error) {
	// Apply defaults
	if config.LockDir == "" {
		config.LockDir = ".aleutian/locks"
	}
	if config.DefaultTTL == 0 {
		config.DefaultTTL = time.Hour
	}

	// Ensure lock directory exists
	if err := os.MkdirAll(config.LockDir, 0755); err != nil {
		return nil, fmt.Errorf("creating lock directory %s: %w", config.LockDir, err)
	}

	// Create fsnotify watcher
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("creating file watcher: %w", err)
	}

	m := &FileLockManager{
		lockDir:    config.LockDir,
		sessionID:  config.SessionID,
		defaultTTL: config.DefaultTTL,
		locker:     newFileLocker(),
		locks:      make(map[string]*lockEntry),
		watcher:    watcher,
		callbacks:  make(map[string][]func(ExternalChangeEvent)),
	}

	// Start watcher goroutine
	go m.watchLoop()

	// Cleanup stale locks on init if configured
	if config.CleanupOnInit {
		cleaned, err := m.CleanupStaleLocks()
		if err != nil {
			slog.Warn("Failed to cleanup stale locks on init",
				"error", err)
		} else if cleaned > 0 {
			slog.Info("Cleaned up stale locks on init",
				"count", cleaned)
		}
	}

	return m, nil
}

// AcquireLock acquires an exclusive lock on a file.
//
// # Description
//
// Attempts to acquire an exclusive lock on the specified file.
// Non-blocking: returns immediately if the file is already locked.
// Creates a .lock info file in the lock directory for visibility.
//
// # Inputs
//
//   - filePath: Absolute or relative path to the file to lock.
//   - reason: Human-readable reason for the lock (for debugging).
//
// # Outputs
//
//   - error: nil on success, FileLockError if already locked, other errors on failure.
//
// # Example
//
//	err := manager.AcquireLock("/path/to/file.go", "Applying patch for CB-15")
//	if err != nil {
//	    if errors.Is(err, lock.ErrFileLocked) {
//	        // Handle lock conflict
//	    }
//	    return err
//	}
//	defer manager.ReleaseLock("/path/to/file.go")
func (m *FileLockManager) AcquireLock(filePath, reason string) error {
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return fmt.Errorf("resolving path %s: %w", filePath, err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if we already hold this lock
	if entry, ok := m.locks[absPath]; ok {
		// Already locked by us, update reason
		entry.info.Reason = reason
		return nil
	}

	// Ensure lock directory still exists (may have been deleted)
	if err := m.ensureLockDir(); err != nil {
		return err
	}

	// Check if another process holds the lock
	lockPath := m.lockPath(absPath)
	existingLock, err := m.readLockInfo(lockPath)
	if err == nil && existingLock != nil {
		// Lock file exists, check if it's stale
		if !existingLock.IsExpired() && IsProcessAlive(existingLock.PID) {
			return &FileLockError{
				Path:   absPath,
				Holder: existingLock,
				Err:    ErrFileLocked,
			}
		}
		// Stale lock, clean it up
		slog.Info("Removing stale lock",
			"path", absPath,
			"old_pid", existingLock.PID)
		_ = os.Remove(lockPath)
	}

	// Open the target file for locking
	f, err := os.OpenFile(absPath, os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("opening file for lock %s: %w", absPath, err)
	}

	// Acquire the lock
	if err := m.locker.Lock(f); err != nil {
		f.Close()
		if err == ErrFileLocked {
			return &FileLockError{
				Path: absPath,
				Err:  ErrFileLocked,
			}
		}
		return fmt.Errorf("acquiring lock on %s: %w", absPath, err)
	}

	// Create lock info
	now := time.Now()
	info := &LockInfo{
		FilePath:  absPath,
		PID:       os.Getpid(),
		SessionID: m.sessionID,
		LockedAt:  now,
		ExpiresAt: now.Add(m.defaultTTL),
		Reason:    reason,
	}

	// Write lock info file
	if err := m.writeLockInfo(lockPath, info); err != nil {
		m.locker.Unlock(f)
		f.Close()
		return fmt.Errorf("writing lock info: %w", err)
	}

	// Add to watcher
	m.addWatch(absPath)

	// Store the lock entry
	m.locks[absPath] = &lockEntry{
		file:     f,
		path:     absPath,
		lockPath: lockPath,
		info:     info,
	}

	slog.Debug("Acquired lock",
		"path", absPath,
		"reason", reason,
		"expires_at", info.ExpiresAt.Format(time.RFC3339))

	return nil
}

// ReleaseLock releases a lock on a file.
//
// # Description
//
// Releases a previously acquired lock. Safe to call on unlocked files
// (returns ErrLockNotHeld). Removes the .lock info file.
//
// # Inputs
//
//   - filePath: Path to the file to unlock (must match path used in AcquireLock).
//
// # Outputs
//
//   - error: nil on success, ErrLockNotHeld if not locked by this manager.
func (m *FileLockManager) ReleaseLock(filePath string) error {
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return fmt.Errorf("resolving path %s: %w", filePath, err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	entry, ok := m.locks[absPath]
	if !ok {
		return ErrLockNotHeld
	}

	return m.releaseLockEntry(absPath, entry)
}

// releaseLockEntry releases a lock entry (must be called with mu held).
func (m *FileLockManager) releaseLockEntry(absPath string, entry *lockEntry) error {
	// Remove from watcher
	m.removeWatch(absPath)

	// Unlock the file
	if f, ok := entry.file.(*os.File); ok {
		if err := m.locker.Unlock(f); err != nil {
			slog.Warn("Failed to unlock file",
				"path", absPath,
				"error", err)
		}
		f.Close()
	}

	// Remove lock info file
	if err := os.Remove(entry.lockPath); err != nil && !os.IsNotExist(err) {
		slog.Warn("Failed to remove lock file",
			"path", entry.lockPath,
			"error", err)
	}

	// Remove from our map
	delete(m.locks, absPath)

	slog.Debug("Released lock",
		"path", absPath)

	return nil
}

// ReleaseAll releases all locks held by this manager.
//
// # Description
//
// Releases all locks acquired by this manager. Should be called
// on session end or manager shutdown.
//
// # Outputs
//
//   - error: First error encountered (continues releasing on error).
func (m *FileLockManager) ReleaseAll() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var firstErr error
	for path, entry := range m.locks {
		if err := m.releaseLockEntry(path, entry); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// IsLocked checks if a file is locked by any process.
//
// # Description
//
// Checks both our internal state and lock info files to determine
// if a file is locked. Useful for pre-flight checks.
//
// # Inputs
//
//   - filePath: Path to check.
//
// # Outputs
//
//   - bool: True if file is locked.
//   - *LockInfo: Information about the lock holder (nil if not locked).
//   - error: Non-nil on failure to check.
func (m *FileLockManager) IsLocked(filePath string) (bool, *LockInfo, error) {
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return false, nil, fmt.Errorf("resolving path %s: %w", filePath, err)
	}

	m.mu.Lock()
	// Check our own locks first
	if entry, ok := m.locks[absPath]; ok {
		m.mu.Unlock()
		return true, entry.info, nil
	}
	m.mu.Unlock()

	// Check for external lock file
	lockPath := m.lockPath(absPath)
	info, err := m.readLockInfo(lockPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil, nil
		}
		return false, nil, err
	}

	if info == nil {
		return false, nil, nil
	}

	// Check if lock is stale
	if info.IsExpired() || !IsProcessAlive(info.PID) {
		return false, nil, nil // Stale lock
	}

	return true, info, nil
}

// CleanupStaleLocks removes locks from dead processes.
//
// # Description
//
// Scans the lock directory for lock files from processes that have
// exited or locks that have expired. Removes stale lock files.
//
// # Outputs
//
//   - int: Number of stale locks cleaned up.
//   - error: Non-nil on failure to scan directory.
func (m *FileLockManager) CleanupStaleLocks() (int, error) {
	entries, err := os.ReadDir(m.lockDir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("reading lock directory: %w", err)
	}

	cleaned := 0
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".lock" {
			continue
		}

		lockPath := filepath.Join(m.lockDir, entry.Name())
		info, err := m.readLockInfo(lockPath)
		if err != nil {
			slog.Warn("Failed to read lock info",
				"path", lockPath,
				"error", err)
			continue
		}

		if info == nil {
			continue
		}

		// Check if stale
		if info.IsExpired() || !IsProcessAlive(info.PID) {
			slog.Info("Cleaning up stale lock",
				"path", info.FilePath,
				"pid", info.PID,
				"expired", info.IsExpired())
			if err := os.Remove(lockPath); err != nil {
				slog.Warn("Failed to remove stale lock",
					"path", lockPath,
					"error", err)
			} else {
				cleaned++
			}
		}
	}

	return cleaned, nil
}

// RegisterCallback registers a callback for external file changes.
//
// # Description
//
// The callback is invoked when a locked file is modified externally.
// Multiple callbacks can be registered for the same file.
//
// # Inputs
//
//   - filePath: Path to monitor.
//   - callback: Function to call on change.
func (m *FileLockManager) RegisterCallback(filePath string, callback func(ExternalChangeEvent)) {
	absPath, _ := filepath.Abs(filePath)

	m.watcherMu.Lock()
	defer m.watcherMu.Unlock()

	m.callbacks[absPath] = append(m.callbacks[absPath], callback)
}

// Close shuts down the lock manager.
//
// # Description
//
// Releases all locks and stops the file watcher.
// Should be called when the manager is no longer needed.
//
// # Outputs
//
//   - error: First error encountered during shutdown.
func (m *FileLockManager) Close() error {
	// Release all locks
	if err := m.ReleaseAll(); err != nil {
		slog.Warn("Error releasing locks during close",
			"error", err)
	}

	// Close watcher
	return m.watcher.Close()
}

// =============================================================================
// Internal helpers
// =============================================================================

// lockPath generates the lock file path for a given file.
// Uses SHA256[:16] for collision resistance.
func (m *FileLockManager) lockPath(absPath string) string {
	hash := sha256.Sum256([]byte(absPath))
	hashStr := hex.EncodeToString(hash[:])[:16] // 64 bits = plenty of collision resistance
	return filepath.Join(m.lockDir, hashStr+".lock")
}

// ensureLockDir ensures the lock directory exists.
func (m *FileLockManager) ensureLockDir() error {
	if err := os.MkdirAll(m.lockDir, 0755); err != nil {
		return fmt.Errorf("creating lock directory: %w", err)
	}
	return nil
}

// writeLockInfo writes lock metadata to a JSON file.
func (m *FileLockManager) writeLockInfo(lockPath string, info *LockInfo) error {
	data, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(lockPath, data, 0644)
}

// readLockInfo reads lock metadata from a JSON file.
func (m *FileLockManager) readLockInfo(lockPath string) (*LockInfo, error) {
	data, err := os.ReadFile(lockPath)
	if err != nil {
		return nil, err
	}

	var info LockInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return nil, err
	}

	return &info, nil
}

// addWatch adds a file to the watcher.
func (m *FileLockManager) addWatch(path string) {
	m.watcherMu.Lock()
	defer m.watcherMu.Unlock()

	if err := m.watcher.Add(path); err != nil {
		slog.Warn("Failed to watch file",
			"path", path,
			"error", err)
	}
}

// removeWatch removes a file from the watcher.
func (m *FileLockManager) removeWatch(path string) {
	m.watcherMu.Lock()
	defer m.watcherMu.Unlock()

	if err := m.watcher.Remove(path); err != nil {
		// Ignore "not watching" errors
		if !os.IsNotExist(err) {
			slog.Debug("Note: file was not being watched",
				"path", path)
		}
	}

	// Remove callbacks for this path
	delete(m.callbacks, path)
}

// watchLoop handles fsnotify events.
func (m *FileLockManager) watchLoop() {
	for {
		select {
		case event, ok := <-m.watcher.Events:
			if !ok {
				return
			}
			m.handleWatchEvent(event)

		case err, ok := <-m.watcher.Errors:
			if !ok {
				return
			}
			slog.Warn("File watcher error",
				"error", err)
		}
	}
}

// handleWatchEvent processes a single fsnotify event.
func (m *FileLockManager) handleWatchEvent(event fsnotify.Event) {
	// Only care about writes, deletes, and renames
	var changeType ChangeType
	switch {
	case event.Op&fsnotify.Write != 0:
		changeType = ChangeWrite
	case event.Op&fsnotify.Remove != 0:
		changeType = ChangeDelete
	case event.Op&fsnotify.Rename != 0:
		changeType = ChangeRename
	default:
		return
	}

	absPath, _ := filepath.Abs(event.Name)

	// Check if we hold a lock on this file
	m.mu.Lock()
	_, weHoldLock := m.locks[absPath]
	m.mu.Unlock()

	if !weHoldLock {
		return
	}

	slog.Warn("External modification detected on locked file",
		"path", absPath,
		"event", changeType.String())

	// Invoke callbacks
	m.watcherMu.Lock()
	callbacks := m.callbacks[absPath]
	m.watcherMu.Unlock()

	changeEvent := ExternalChangeEvent{
		Path:      absPath,
		EventType: changeType,
	}

	for _, cb := range callbacks {
		cb(changeEvent)
	}
}

// WatchFile starts watching a file for external changes.
//
// # Description
//
// Watches the specified file and calls the callback when changes are detected.
// Stops watching when the context is cancelled.
//
// # Inputs
//
//   - ctx: Context for cancellation.
//   - filePath: File to watch.
//   - callback: Function to call on change.
func (m *FileLockManager) WatchFile(ctx context.Context, filePath string, callback func(string)) {
	absPath, _ := filepath.Abs(filePath)

	// Add to watcher
	m.addWatch(absPath)

	// Register callback
	m.RegisterCallback(absPath, func(event ExternalChangeEvent) {
		callback(event.Path)
	})

	// Wait for context cancellation
	<-ctx.Done()

	// Remove from watcher
	m.removeWatch(absPath)
}
