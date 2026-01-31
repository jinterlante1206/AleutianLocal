// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package tdg

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
)

// =============================================================================
// FILE MANAGER
// =============================================================================

// FileManager handles file operations for TDG including writing tests,
// applying patches, and rollback functionality.
//
// Thread Safety: NOT safe for concurrent use on the same files.
// Each TDG session should have its own FileManager.
type FileManager struct {
	projectRoot  string
	backups      map[string][]byte   // filepath → original content
	createdFiles map[string]struct{} // files created by TDG
	mu           sync.Mutex
	logger       *slog.Logger
}

// NewFileManager creates a new file manager.
//
// Inputs:
//
//	projectRoot - Root directory of the project
//	logger - Logger for structured logging
//
// Outputs:
//
//	*FileManager - Configured file manager
func NewFileManager(projectRoot string, logger *slog.Logger) *FileManager {
	if logger == nil {
		logger = slog.Default()
	}
	return &FileManager{
		projectRoot:  projectRoot,
		backups:      make(map[string][]byte),
		createdFiles: make(map[string]struct{}),
		logger:       logger,
	}
}

// WriteTest writes a test file to disk.
//
// Description:
//
//	Writes the test case content to the specified file path.
//	Creates parent directories if needed. Tracks the file for cleanup.
//	Uses atomic write (temp file + rename) for safety.
//
// Inputs:
//
//	tc - The test case with file path and content
//
// Outputs:
//
//	error - Non-nil on write failure
//
// Thread Safety: Uses internal locking.
func (m *FileManager) WriteTest(tc *TestCase) error {
	if err := tc.Validate(); err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Make path absolute if relative
	filePath := tc.FilePath
	if !filepath.IsAbs(filePath) {
		filePath = filepath.Join(m.projectRoot, filePath)
	}

	m.logger.Debug("Writing test file",
		slog.String("path", filePath),
		slog.Int("size", len(tc.Content)),
		slog.String("test_name", tc.Name),
	)

	// Ensure directory exists
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		m.logger.Error("Failed to create directory",
			slog.String("path", dir),
			slog.String("error", err.Error()),
		)
		return fmt.Errorf("%w: create directory: %v", ErrTestWriteFailed, err)
	}

	// Check if file exists (for backup)
	if existing, err := os.ReadFile(filePath); err == nil {
		m.backups[filePath] = existing
		m.logger.Debug("Backed up existing file",
			slog.String("path", filePath),
			slog.Int("size", len(existing)),
		)
	}

	// Atomic write: write to temp file, then rename
	tempPath := filePath + ".tdg.tmp"
	if err := os.WriteFile(tempPath, []byte(tc.Content), 0644); err != nil {
		m.logger.Error("Failed to write temp file",
			slog.String("path", tempPath),
			slog.String("error", err.Error()),
		)
		return fmt.Errorf("%w: write temp: %v", ErrTestWriteFailed, err)
	}

	if err := os.Rename(tempPath, filePath); err != nil {
		_ = os.Remove(tempPath) // Clean up temp file
		m.logger.Error("Failed to rename temp file",
			slog.String("temp", tempPath),
			slog.String("target", filePath),
			slog.String("error", err.Error()),
		)
		return fmt.Errorf("%w: rename: %v", ErrTestWriteFailed, err)
	}

	// Track as created (for cleanup if it didn't exist before)
	if _, hadBackup := m.backups[filePath]; !hadBackup {
		m.createdFiles[filePath] = struct{}{}
	}

	m.logger.Info("Wrote test file",
		slog.String("path", filePath),
		slog.Int("size", len(tc.Content)),
	)

	return nil
}

// ApplyPatch applies a code change and stores backup for rollback.
//
// Description:
//
//	Reads the current file content (for rollback), then writes the new
//	content. Updates the patch with old content if not already set.
//	Uses atomic write for safety.
//
// Inputs:
//
//	patch - The patch to apply
//
// Outputs:
//
//	error - Non-nil on apply failure
//
// Thread Safety: Uses internal locking.
func (m *FileManager) ApplyPatch(patch *Patch) error {
	if err := patch.Validate(); err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Make path absolute if relative
	filePath := patch.FilePath
	if !filepath.IsAbs(filePath) {
		filePath = filepath.Join(m.projectRoot, filePath)
	}

	m.logger.Debug("Applying patch",
		slog.String("path", filePath),
		slog.Int("new_size", len(patch.NewContent)),
	)

	// Read existing content for backup
	existing, err := os.ReadFile(filePath)
	if err != nil && !os.IsNotExist(err) {
		m.logger.Error("Failed to read existing file",
			slog.String("path", filePath),
			slog.String("error", err.Error()),
		)
		return fmt.Errorf("%w: read existing: %v", ErrPatchApplyFailed, err)
	}

	// Store backup
	if existing != nil {
		m.backups[filePath] = existing
		if patch.OldContent == "" {
			patch.OldContent = string(existing)
		}
	} else {
		// File doesn't exist, track as created
		m.createdFiles[filePath] = struct{}{}
	}

	// Ensure directory exists
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		m.logger.Error("Failed to create directory",
			slog.String("path", dir),
			slog.String("error", err.Error()),
		)
		return fmt.Errorf("%w: create directory: %v", ErrPatchApplyFailed, err)
	}

	// Atomic write
	tempPath := filePath + ".tdg.tmp"
	if err := os.WriteFile(tempPath, []byte(patch.NewContent), 0644); err != nil {
		m.logger.Error("Failed to write temp file",
			slog.String("path", tempPath),
			slog.String("error", err.Error()),
		)
		return fmt.Errorf("%w: write temp: %v", ErrPatchApplyFailed, err)
	}

	if err := os.Rename(tempPath, filePath); err != nil {
		_ = os.Remove(tempPath)
		m.logger.Error("Failed to rename temp file",
			slog.String("temp", tempPath),
			slog.String("target", filePath),
			slog.String("error", err.Error()),
		)
		return fmt.Errorf("%w: rename: %v", ErrPatchApplyFailed, err)
	}

	patch.Applied = true

	m.logger.Info("Applied patch",
		slog.String("path", filePath),
		slog.Int("old_size", len(existing)),
		slog.Int("new_size", len(patch.NewContent)),
	)

	return nil
}

// Rollback restores all backed-up files to their original state.
//
// Description:
//
//	Restores files that were modified by patches back to their original
//	content. Removes files that were created by TDG. Should be called
//	when TDG fails or is cancelled.
//
// Outputs:
//
//	error - Non-nil if any rollback failed
//
// Thread Safety: Uses internal locking.
func (m *FileManager) Rollback() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.logger.Info("Rolling back changes",
		slog.Int("backups", len(m.backups)),
		slog.Int("created", len(m.createdFiles)),
	)

	var lastErr error

	// Restore backed-up files
	for filePath, content := range m.backups {
		if err := os.WriteFile(filePath, content, 0644); err != nil {
			m.logger.Error("Failed to restore file",
				slog.String("path", filePath),
				slog.String("error", err.Error()),
			)
			lastErr = fmt.Errorf("%w: restore %s: %v", ErrPatchRollbackFailed, filePath, err)
		} else {
			m.logger.Debug("Restored file",
				slog.String("path", filePath),
				slog.Int("size", len(content)),
			)
		}
	}

	// Remove created files
	for filePath := range m.createdFiles {
		if _, ok := m.backups[filePath]; ok {
			// File was backed up, already restored
			continue
		}
		if err := os.Remove(filePath); err != nil && !os.IsNotExist(err) {
			m.logger.Error("Failed to remove created file",
				slog.String("path", filePath),
				slog.String("error", err.Error()),
			)
			lastErr = fmt.Errorf("%w: remove %s: %v", ErrPatchRollbackFailed, filePath, err)
		} else {
			m.logger.Debug("Removed created file",
				slog.String("path", filePath),
			)
		}
	}

	// Clear tracking
	m.backups = make(map[string][]byte)
	m.createdFiles = make(map[string]struct{})

	if lastErr != nil {
		return lastErr
	}

	m.logger.Info("Rollback complete")
	return nil
}

// Cleanup removes test files that were created during TDG.
//
// Description:
//
//	Similar to Rollback but only removes newly created files,
//	leaves modified files with their new content. Used after
//	successful TDG completion if test files shouldn't be kept.
//
// Outputs:
//
//	error - Non-nil if any removal failed
//
// Thread Safety: Uses internal locking.
func (m *FileManager) Cleanup() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.logger.Debug("Cleaning up created files",
		slog.Int("count", len(m.createdFiles)),
	)

	var lastErr error

	for filePath := range m.createdFiles {
		if _, ok := m.backups[filePath]; ok {
			// File existed before, don't remove
			continue
		}
		if err := os.Remove(filePath); err != nil && !os.IsNotExist(err) {
			m.logger.Error("Failed to remove file",
				slog.String("path", filePath),
				slog.String("error", err.Error()),
			)
			lastErr = err
		} else {
			m.logger.Debug("Removed file",
				slog.String("path", filePath),
			)
		}
	}

	m.createdFiles = make(map[string]struct{})
	return lastErr
}

// HasBackups returns true if there are any backed-up files.
//
// Thread Safety: Safe for concurrent use.
func (m *FileManager) HasBackups() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.backups) > 0
}

// BackupCount returns the number of backed-up files.
//
// Thread Safety: Safe for concurrent use.
func (m *FileManager) BackupCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.backups)
}

// CreatedCount returns the number of created files.
//
// Thread Safety: Safe for concurrent use.
func (m *FileManager) CreatedCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.createdFiles)
}
