// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package diff

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// =============================================================================
// Apply Options
// =============================================================================

// ApplyOptions configures how changes are applied.
type ApplyOptions struct {
	// DryRun simulates application without writing files.
	DryRun bool

	// CreateBackups creates .orig backups before modifying.
	CreateBackups bool

	// BackupSuffix is the suffix for backup files (default: ".orig").
	BackupSuffix string

	// CreateDirs creates parent directories if needed.
	CreateDirs bool

	// FileMode is the mode for newly created files (default: 0644).
	FileMode os.FileMode

	// DirMode is the mode for newly created directories (default: 0755).
	DirMode os.FileMode
}

// DefaultApplyOptions returns sensible defaults.
func DefaultApplyOptions() ApplyOptions {
	return ApplyOptions{
		DryRun:        false,
		CreateBackups: false,
		BackupSuffix:  ".orig",
		CreateDirs:    true,
		FileMode:      0644,
		DirMode:       0755,
	}
}

// =============================================================================
// Apply Result
// =============================================================================

// ApplyResult contains the outcome of applying changes.
type ApplyResult struct {
	// FilePath is the path that was modified.
	FilePath string

	// Success indicates if the apply succeeded.
	Success bool

	// Applied indicates if changes were actually written (false for dry run).
	Applied bool

	// Error contains any error message.
	Error string

	// BackupPath is the path to the backup file (if created).
	BackupPath string

	// HunksApplied is the number of hunks that were applied.
	HunksApplied int

	// HunksRejected is the number of hunks that were rejected by user.
	HunksRejected int

	// BytesWritten is the size of the final file.
	BytesWritten int64
}

// =============================================================================
// Applier
// =============================================================================

// Applier applies proposed changes to files.
//
// # Description
//
// Handles the application of reviewed changes to the filesystem,
// respecting user decisions about which hunks to accept/reject.
//
// # Thread Safety
//
// Applier is safe for concurrent use. Individual Apply calls are serialized
// per-file using internal locking.
type Applier struct {
	basePath string
	options  ApplyOptions

	// mu protects concurrent file operations
	mu sync.Mutex

	// fileLocks provides per-file locking for concurrent safety
	fileLocks   map[string]*sync.Mutex
	fileLocksMu sync.Mutex
}

// NewApplier creates a new change applier.
//
// # Inputs
//
//   - basePath: Base directory for relative paths. Must be absolute.
//   - options: Configuration options.
//
// # Outputs
//
//   - *Applier: Ready-to-use applier.
//   - error: Non-nil if basePath is invalid.
func NewApplier(basePath string, options ApplyOptions) (*Applier, error) {
	if !filepath.IsAbs(basePath) {
		return nil, fmt.Errorf("basePath must be absolute: %s", basePath)
	}

	info, err := os.Stat(basePath)
	if err != nil {
		return nil, fmt.Errorf("stat basePath: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("basePath is not a directory: %s", basePath)
	}

	return &Applier{
		basePath:  basePath,
		options:   options,
		fileLocks: make(map[string]*sync.Mutex),
	}, nil
}

// Apply applies a single proposed change to the filesystem.
//
// # Description
//
// Applies the accepted hunks from a ProposedChange, respecting user
// decisions. Rejected hunks are not applied.
//
// # Inputs
//
//   - ctx: Context for cancellation.
//   - change: The proposed change to apply.
//
// # Outputs
//
//   - *ApplyResult: Result of the apply operation.
//   - error: Non-nil on system errors (file access, etc.).
func (a *Applier) Apply(ctx context.Context, change *ProposedChange) (*ApplyResult, error) {
	result := &ApplyResult{
		FilePath: change.FilePath,
	}

	// Build full path
	fullPath := a.resolvePath(change.FilePath)

	// Validate path is within basePath (security check)
	if !a.isPathSafe(fullPath) {
		result.Error = "path escapes base directory"
		return result, fmt.Errorf("security: path escapes base directory: %s", change.FilePath)
	}

	// Acquire per-file lock for thread safety
	fileLock := a.getFileLock(fullPath)
	fileLock.Lock()
	defer fileLock.Unlock()

	// Check for cancellation
	select {
	case <-ctx.Done():
		result.Error = ctx.Err().Error()
		return result, ctx.Err()
	default:
	}

	// Handle deletions
	if change.IsDelete {
		return a.applyDelete(ctx, fullPath, result)
	}

	// Handle new file creation
	if change.IsNew {
		return a.applyNew(ctx, fullPath, change, result)
	}

	// Handle modification - apply accepted hunks
	return a.applyModification(ctx, fullPath, change, result)
}

// getFileLock returns a per-file mutex for thread-safe file operations.
func (a *Applier) getFileLock(path string) *sync.Mutex {
	a.fileLocksMu.Lock()
	defer a.fileLocksMu.Unlock()

	if lock, ok := a.fileLocks[path]; ok {
		return lock
	}

	lock := &sync.Mutex{}
	a.fileLocks[path] = lock
	return lock
}

// ApplyAll applies multiple changes, returning results for each.
//
// # Description
//
// Applies all changes in order. Continues even if some changes fail.
//
// # Inputs
//
//   - ctx: Context for cancellation.
//   - changes: The changes to apply.
//
// # Outputs
//
//   - []*ApplyResult: Results for each change.
//   - error: Non-nil only on context cancellation.
func (a *Applier) ApplyAll(ctx context.Context, changes []*ProposedChange) ([]*ApplyResult, error) {
	results := make([]*ApplyResult, 0, len(changes))

	for _, change := range changes {
		select {
		case <-ctx.Done():
			return results, ctx.Err()
		default:
		}

		result, err := a.Apply(ctx, change)
		if err != nil {
			// Log but continue
			result.Error = err.Error()
		}
		results = append(results, result)
	}

	return results, nil
}

// applyDelete handles file deletion.
func (a *Applier) applyDelete(ctx context.Context, fullPath string, result *ApplyResult) (*ApplyResult, error) {
	if a.options.DryRun {
		result.Success = true
		return result, nil
	}

	// Create backup if requested
	if a.options.CreateBackups {
		backupPath := fullPath + a.options.BackupSuffix
		if err := copyFile(fullPath, backupPath); err == nil {
			result.BackupPath = backupPath
		}
	}

	// Delete the file
	if err := os.Remove(fullPath); err != nil {
		if !os.IsNotExist(err) {
			result.Error = err.Error()
			return result, fmt.Errorf("removing file: %w", err)
		}
	}

	result.Success = true
	result.Applied = true
	return result, nil
}

// applyNew handles new file creation.
func (a *Applier) applyNew(ctx context.Context, fullPath string, change *ProposedChange, result *ApplyResult) (*ApplyResult, error) {
	// Check all hunks are accepted
	for _, hunk := range change.Hunks {
		if hunk.Status == HunkAccepted || hunk.Status == HunkEdited {
			result.HunksApplied++
		} else if hunk.Status == HunkRejected {
			result.HunksRejected++
		}
	}

	// If all hunks rejected, don't create the file
	if result.HunksApplied == 0 {
		result.Success = true
		return result, nil
	}

	if a.options.DryRun {
		result.Success = true
		return result, nil
	}

	// Create parent directories if needed
	if a.options.CreateDirs {
		dir := filepath.Dir(fullPath)
		if err := os.MkdirAll(dir, a.options.DirMode); err != nil {
			result.Error = err.Error()
			return result, fmt.Errorf("creating directories: %w", err)
		}
	}

	// Build content from accepted hunks
	content := a.buildNewFileContent(change)

	// Write the file
	if err := os.WriteFile(fullPath, []byte(content), a.options.FileMode); err != nil {
		result.Error = err.Error()
		return result, fmt.Errorf("writing file: %w", err)
	}

	result.Success = true
	result.Applied = true
	result.BytesWritten = int64(len(content))
	return result, nil
}

// applyModification handles file modifications.
func (a *Applier) applyModification(ctx context.Context, fullPath string, change *ProposedChange, result *ApplyResult) (*ApplyResult, error) {
	// Read current file content
	currentContent, err := os.ReadFile(fullPath)
	if err != nil {
		result.Error = err.Error()
		return result, fmt.Errorf("reading file: %w", err)
	}

	// Count accepted/rejected hunks
	for _, hunk := range change.Hunks {
		if hunk.Status == HunkAccepted || hunk.Status == HunkEdited {
			result.HunksApplied++
		} else if hunk.Status == HunkRejected {
			result.HunksRejected++
		}
	}

	// If no hunks accepted, no changes to apply
	if result.HunksApplied == 0 {
		result.Success = true
		return result, nil
	}

	// Apply accepted hunks to content
	newContent, err := a.applyHunks(string(currentContent), change.Hunks)
	if err != nil {
		result.Error = err.Error()
		return result, fmt.Errorf("applying hunks: %w", err)
	}

	if a.options.DryRun {
		result.Success = true
		return result, nil
	}

	// Create backup if requested
	if a.options.CreateBackups {
		backupPath := fullPath + a.options.BackupSuffix
		if err := os.WriteFile(backupPath, currentContent, a.options.FileMode); err == nil {
			result.BackupPath = backupPath
		}
	}

	// Write the new content
	if err := os.WriteFile(fullPath, []byte(newContent), a.options.FileMode); err != nil {
		result.Error = err.Error()
		return result, fmt.Errorf("writing file: %w", err)
	}

	result.Success = true
	result.Applied = true
	result.BytesWritten = int64(len(newContent))
	return result, nil
}

// applyHunks applies accepted hunks to content.
func (a *Applier) applyHunks(content string, hunks []*Hunk) (string, error) {
	lines := strings.Split(content, "\n")

	// Process hunks in reverse order to preserve line numbers
	for i := len(hunks) - 1; i >= 0; i-- {
		hunk := hunks[i]

		// Skip non-accepted hunks
		if hunk.Status != HunkAccepted && hunk.Status != HunkEdited {
			continue
		}

		effectiveLines := hunk.EffectiveLines()

		// Build the new lines for this hunk
		var newHunkLines []string
		for _, line := range effectiveLines {
			if line.Type == LineContext || line.Type == LineAdded {
				newHunkLines = append(newHunkLines, line.Content)
			}
			// LineRemoved lines are not included in output
		}

		// Calculate the range to replace
		startLine := hunk.OldStart - 1 // Convert to 0-based
		endLine := startLine + hunk.OldCount

		// Bounds checking
		if startLine < 0 {
			startLine = 0
		}
		if endLine > len(lines) {
			endLine = len(lines)
		}

		// Replace the lines
		newLines := make([]string, 0, len(lines)-hunk.OldCount+len(newHunkLines))
		newLines = append(newLines, lines[:startLine]...)
		newLines = append(newLines, newHunkLines...)
		newLines = append(newLines, lines[endLine:]...)
		lines = newLines
	}

	return strings.Join(lines, "\n"), nil
}

// buildNewFileContent builds content for a new file from accepted hunks.
func (a *Applier) buildNewFileContent(change *ProposedChange) string {
	var lines []string

	for _, hunk := range change.Hunks {
		if hunk.Status != HunkAccepted && hunk.Status != HunkEdited {
			continue
		}

		for _, line := range hunk.EffectiveLines() {
			if line.Type == LineAdded || line.Type == LineContext {
				lines = append(lines, line.Content)
			}
		}
	}

	return strings.Join(lines, "\n")
}

// resolvePath resolves a relative path against basePath.
func (a *Applier) resolvePath(relPath string) string {
	if filepath.IsAbs(relPath) {
		return relPath
	}
	return filepath.Join(a.basePath, relPath)
}

// isPathSafe checks if a path is within the base directory.
func (a *Applier) isPathSafe(fullPath string) bool {
	// Clean both paths
	cleanBase := filepath.Clean(a.basePath)
	cleanPath := filepath.Clean(fullPath)

	// Check if the path is within basePath
	rel, err := filepath.Rel(cleanBase, cleanPath)
	if err != nil {
		return false
	}

	// Reject paths that escape via ..
	if strings.HasPrefix(rel, "..") {
		return false
	}

	return true
}

// copyFile copies a file from src to dst.
func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}

	info, err := os.Stat(src)
	if err != nil {
		return err
	}

	return os.WriteFile(dst, data, info.Mode())
}
