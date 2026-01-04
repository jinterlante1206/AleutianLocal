// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

/*
Package main provides FileDiagnosticsStorage for local filesystem storage.

File storage is the FOSS-tier implementation of DiagnosticsStorage, providing
local diagnostic retention for developers debugging their own machines.

# Open Core Architecture

This implementation follows the Open Core model:

  - FOSS (this file): Writes JSON to ~/.aleutian/diagnostics/
  - Enterprise: S3DiagnosticsStorage, SplunkHECStorage (centralized fleet management)

The interface is public; the implementation dictates the value.

# Design Goals

  - Local diagnostic retention with automatic pruning
  - GDPR-compliant 30-day default retention period
  - Fast local access for DiagnosticsViewer
  - Cross-platform compatibility (macOS, Linux, Windows)

Files are stored with timestamped filenames for easy identification and
chronological ordering. The storage directory is created automatically
if it does not exist.
*/
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// -----------------------------------------------------------------------------
// FileDiagnosticsStorage Implementation
// -----------------------------------------------------------------------------

// FileDiagnosticsStorage stores diagnostics in the local filesystem.
//
// This is the FOSS-tier implementation of DiagnosticsStorage, useful for
// developers debugging their own machines.
//
// # Enterprise Alternative
//
// For fleet management (IT admins managing 500+ laptops), Enterprise customers
// use S3DiagnosticsStorage or SplunkHECStorage to stream diagnostics to
// centralized storage.
//
// # Capabilities
//
//   - Persistent local storage in a configurable directory
//   - Automatic file naming with timestamps
//   - 30-day retention by default (GDPR compliance)
//   - Thread-safe concurrent access
//   - Cross-platform path handling
//
// # Thread Safety
//
// FileDiagnosticsStorage uses a mutex to protect concurrent operations.
// Multiple goroutines can safely Store, Load, List, and Prune concurrently.
type FileDiagnosticsStorage struct {
	// baseDir is the directory where diagnostics are stored.
	baseDir string

	// retentionDays is how long to keep diagnostics before pruning.
	retentionDays int

	// mu protects concurrent access to storage operations.
	mu sync.RWMutex

	// filePrefix is prepended to generated filenames.
	filePrefix string

	// fileExtension is the extension for stored files.
	fileExtension string
}

// NewFileDiagnosticsStorage creates a file-based storage backend.
//
// # Description
//
// Creates a FOSS-tier storage backend that saves diagnostics to the local
// filesystem. The directory is created if it does not exist.
//
// # Inputs
//
//   - baseDir: Directory path for diagnostic files. Use empty string for default
//     (~/.aleutian/diagnostics)
//
// # Outputs
//
//   - *FileDiagnosticsStorage: Ready-to-use storage backend
//   - error: Non-nil if directory creation fails
//
// # Examples
//
//	storage, err := NewFileDiagnosticsStorage("")
//	if err != nil {
//	    log.Fatal(err)
//	}
//	// Uses ~/.aleutian/diagnostics
//
//	storage, err := NewFileDiagnosticsStorage("/var/log/aleutian/diagnostics")
//	// Uses custom directory
//
// # Limitations
//
//   - Requires write permissions to the base directory
//   - Not suitable for fleet management (use Enterprise S3/Splunk instead)
//   - No encryption at rest (rely on filesystem encryption)
//
// # Assumptions
//
//   - Filesystem supports standard file operations
//   - Clock is reasonably synchronized for timestamp generation
func NewFileDiagnosticsStorage(baseDir string) (*FileDiagnosticsStorage, error) {
	if baseDir == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("failed to get home directory: %w", err)
		}
		baseDir = filepath.Join(homeDir, ".aleutian", "diagnostics")
	}

	if err := os.MkdirAll(baseDir, 0750); err != nil {
		return nil, fmt.Errorf("failed to create diagnostics directory %s: %w", baseDir, err)
	}

	return &FileDiagnosticsStorage{
		baseDir:       baseDir,
		retentionDays: DefaultRetentionDays,
		filePrefix:    "diag",
		fileExtension: ".json",
	}, nil
}

// Store saves diagnostic data to a file.
//
// # Description
//
// Writes data to a timestamped file in the base directory. Uses atomic
// write (temp file + rename) to prevent partial files on crash.
//
// # Inputs
//
//   - ctx: Context for cancellation (currently not used for file I/O)
//   - data: Raw diagnostic bytes to store
//   - metadata: Hints for filename and content type
//
// # Outputs
//
//   - string: Absolute path to the stored file
//   - error: Non-nil if write fails
//
// # Examples
//
//	location, err := storage.Store(ctx, jsonBytes, StorageMetadata{
//	    FilenameHint: "startup_failure",
//	    ContentType:  "application/json",
//	})
//	// location: "/home/user/.aleutian/diagnostics/diag-20240105-100000-startup_failure.json"
//
// # Limitations
//
//   - Large files may be slow on spinning disks
//
// # Assumptions
//
//   - Base directory exists and is writable
//   - Sufficient disk space available
func (s *FileDiagnosticsStorage) Store(ctx context.Context, data []byte, metadata StorageMetadata) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	filename := s.generateFilename(metadata)
	filePath := filepath.Join(s.baseDir, filename)

	// Write to temp file first for atomic operation
	tempPath := filePath + ".tmp"
	if err := os.WriteFile(tempPath, data, 0640); err != nil {
		return "", fmt.Errorf("failed to write diagnostic file: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tempPath, filePath); err != nil {
		// Clean up temp file on failure
		_ = os.Remove(tempPath)
		return "", fmt.Errorf("failed to finalize diagnostic file: %w", err)
	}

	return filePath, nil
}

// Load retrieves diagnostic data from a file.
//
// # Description
//
// Reads the contents of a previously stored diagnostic file.
// Includes path traversal protection to prevent directory escape attacks.
//
// # Inputs
//
//   - ctx: Context for cancellation (currently not used for file I/O)
//   - location: Absolute path to the diagnostic file
//
// # Outputs
//
//   - []byte: Raw diagnostic data
//   - error: Non-nil if read fails or file not found
//
// # Examples
//
//	data, err := storage.Load(ctx, "/home/user/.aleutian/diagnostics/diag-xxx.json")
//	if err != nil {
//	    if os.IsNotExist(err) {
//	        // File was pruned or never existed
//	    }
//	}
//
// # Security
//
//   - Only loads files within the base directory (prevents path traversal)
//
// # Limitations
//
//   - Large files are loaded entirely into memory
//
// # Assumptions
//
//   - Location path was returned by Store() or List()
func (s *FileDiagnosticsStorage) Load(ctx context.Context, location string) ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Security: Ensure path is within base directory (prevent path traversal)
	cleanPath := filepath.Clean(location)
	absBase, err := filepath.Abs(s.baseDir)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve base directory: %w", err)
	}
	absPath, err := filepath.Abs(cleanPath)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve path: %w", err)
	}

	if !strings.HasPrefix(absPath, absBase+string(filepath.Separator)) && absPath != absBase {
		return nil, fmt.Errorf("path outside storage directory: %s", location)
	}

	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read diagnostic file: %w", err)
	}

	return data, nil
}

// List returns paths to stored diagnostics, most recent first.
//
// # Description
//
// Returns absolute paths to diagnostic files, sorted by modification time
// in descending order (newest first).
//
// # Inputs
//
//   - ctx: Context for cancellation (currently not used)
//   - limit: Maximum number of paths to return. Use 0 or negative for all.
//
// # Outputs
//
//   - []string: Absolute paths to diagnostic files
//   - error: Non-nil if directory listing fails
//
// # Examples
//
//	paths, err := storage.List(ctx, 10)
//	// Returns up to 10 most recent diagnostic paths
//
//	paths, err := storage.List(ctx, 0)
//	// Returns all diagnostic paths
//
// # Limitations
//
//   - Only returns files matching the prefix and extension pattern
//   - Sorting by modification time may be slow for many files
//
// # Assumptions
//
//   - Base directory contains only diagnostic files
func (s *FileDiagnosticsStorage) List(ctx context.Context, limit int) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entries, err := os.ReadDir(s.baseDir)
	if err != nil {
		return nil, fmt.Errorf("failed to list diagnostics directory: %w", err)
	}

	// Filter and collect file info
	type fileWithTime struct {
		path    string
		modTime time.Time
	}

	var files []fileWithTime
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if !strings.HasPrefix(name, s.filePrefix) || !strings.HasSuffix(name, s.fileExtension) {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue // Skip files we can't stat
		}

		files = append(files, fileWithTime{
			path:    filepath.Join(s.baseDir, name),
			modTime: info.ModTime(),
		})
	}

	// Sort by modification time, newest first
	sort.Slice(files, func(i, j int) bool {
		return files[i].modTime.After(files[j].modTime)
	})

	// Apply limit
	if limit > 0 && len(files) > limit {
		files = files[:limit]
	}

	// Extract paths
	paths := make([]string, len(files))
	for i, f := range files {
		paths[i] = f.path
	}

	return paths, nil
}

// Prune removes diagnostics older than the retention period.
//
// # Description
//
// Deletes diagnostic files whose modification time is older than the
// configured retention period. This supports GDPR data minimization
// and prevents unbounded disk usage.
//
// # Inputs
//
//   - ctx: Context for cancellation (currently not used)
//
// # Outputs
//
//   - int: Number of files deleted
//   - error: Non-nil if deletion fails (partial deletion may have occurred)
//
// # Examples
//
//	deleted, err := storage.Prune(ctx)
//	if err != nil {
//	    log.Printf("Prune error (deleted %d files): %v", deleted, err)
//	}
//	log.Printf("Pruned %d old diagnostic files", deleted)
//
// # GDPR Compliance
//
//   - Default 30-day retention aligns with GDPR Data Minimization
//   - Can be adjusted via SetRetentionDays for organizational policy
//
// # Limitations
//
//   - Returns on first error; some files may remain undeleted
//   - Uses file modification time, not creation time
//
// # Assumptions
//
//   - Retention period is configured via SetRetentionDays
//   - File modification times are reliable
func (s *FileDiagnosticsStorage) Prune(ctx context.Context) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	cutoff := time.Now().AddDate(0, 0, -s.retentionDays)

	entries, err := os.ReadDir(s.baseDir)
	if err != nil {
		return 0, fmt.Errorf("failed to list diagnostics directory: %w", err)
	}

	var deleted int
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if !strings.HasPrefix(name, s.filePrefix) || !strings.HasSuffix(name, s.fileExtension) {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue // Skip files we can't stat
		}

		if info.ModTime().Before(cutoff) {
			filePath := filepath.Join(s.baseDir, name)
			if err := os.Remove(filePath); err != nil {
				return deleted, fmt.Errorf("failed to delete %s: %w", name, err)
			}
			deleted++
		}
	}

	return deleted, nil
}

// SetRetentionDays configures the retention period.
//
// # Description
//
// Sets the number of days to retain diagnostic files before Prune() deletes them.
// Changes take effect on the next Prune() call.
//
// # Inputs
//
//   - days: Number of days to retain. Must be positive.
//     Values <= 0 are ignored.
//
// # Examples
//
//	storage.SetRetentionDays(7)  // Keep diagnostics for 1 week
//	storage.SetRetentionDays(90) // Keep diagnostics for 90 days
//
// # Limitations
//
//   - Does not immediately prune; call Prune() after changing
//   - Minimum is 1 day
func (s *FileDiagnosticsStorage) SetRetentionDays(days int) {
	if days <= 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.retentionDays = days
}

// GetRetentionDays returns the current retention period.
//
// # Outputs
//
//   - int: Number of days diagnostics are retained
func (s *FileDiagnosticsStorage) GetRetentionDays() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.retentionDays
}

// Type returns the storage backend identifier.
//
// # Description
//
// Returns "file" to identify this as the FOSS-tier local storage backend.
// Enterprise backends return "s3", "splunk", etc.
//
// # Outputs
//
//   - string: "file"
func (s *FileDiagnosticsStorage) Type() string {
	return "file"
}

// BaseDir returns the storage directory path.
//
// # Description
//
// Returns the absolute path where diagnostic files are stored.
// Useful for displaying to users or configuring other tools.
//
// # Outputs
//
//   - string: Absolute path to the storage directory
func (s *FileDiagnosticsStorage) BaseDir() string {
	return s.baseDir
}

// Count returns the number of stored diagnostic files.
//
// # Description
//
// Counts diagnostic files matching the prefix and extension pattern.
// Useful for metrics and display purposes.
//
// # Outputs
//
//   - int: Number of diagnostic files
//   - error: Non-nil if directory listing fails
func (s *FileDiagnosticsStorage) Count() (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entries, err := os.ReadDir(s.baseDir)
	if err != nil {
		return 0, fmt.Errorf("failed to list diagnostics directory: %w", err)
	}

	var count int
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasPrefix(name, s.filePrefix) && strings.HasSuffix(name, s.fileExtension) {
			count++
		}
	}

	return count, nil
}

// generateFilename creates a unique timestamped filename.
//
// The filename includes nanoseconds to ensure uniqueness even when
// multiple diagnostics are stored within the same second.
func (s *FileDiagnosticsStorage) generateFilename(metadata StorageMetadata) string {
	now := time.Now()
	// Include nanoseconds for uniqueness within the same second
	timestamp := now.Format("20060102-150405")
	nanos := now.Nanosecond()

	hint := sanitizeFilenameHint(metadata.FilenameHint)
	if hint != "" {
		return fmt.Sprintf("%s-%s-%09d-%s%s", s.filePrefix, timestamp, nanos, hint, s.fileExtension)
	}

	return fmt.Sprintf("%s-%s-%09d%s", s.filePrefix, timestamp, nanos, s.fileExtension)
}

// sanitizeFilenameHint removes unsafe characters from the filename hint.
func sanitizeFilenameHint(hint string) string {
	if hint == "" {
		return ""
	}

	// Replace unsafe characters with underscores
	var result strings.Builder
	for _, r := range hint {
		switch {
		case r >= 'a' && r <= 'z':
			result.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			result.WriteRune(r)
		case r >= '0' && r <= '9':
			result.WriteRune(r)
		case r == '-' || r == '_':
			result.WriteRune(r)
		default:
			result.WriteRune('_')
		}
	}

	// Truncate to reasonable length
	s := result.String()
	if len(s) > 50 {
		s = s[:50]
	}

	return s
}

// -----------------------------------------------------------------------------
// Helper Functions
// -----------------------------------------------------------------------------

// ParseDiagnosticsFromFile loads and parses a diagnostic JSON file.
//
// # Description
//
// Convenience function that loads a file and parses it as DiagnosticsData.
//
// # Inputs
//
//   - filePath: Absolute path to the diagnostic file
//
// # Outputs
//
//   - *DiagnosticsData: Parsed diagnostic data
//   - error: Non-nil if file read or JSON parse fails
//
// # Examples
//
//	data, err := ParseDiagnosticsFromFile("/path/to/diag.json")
//	if err != nil {
//	    log.Fatal(err)
//	}
//	fmt.Printf("Reason: %s\n", data.Header.Reason)
func ParseDiagnosticsFromFile(filePath string) (*DiagnosticsData, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	var data DiagnosticsData
	if err := json.Unmarshal(content, &data); err != nil {
		return nil, fmt.Errorf("failed to parse diagnostics: %w", err)
	}

	return &data, nil
}

// Compile-time interface compliance check.
var _ DiagnosticsStorage = (*FileDiagnosticsStorage)(nil)
