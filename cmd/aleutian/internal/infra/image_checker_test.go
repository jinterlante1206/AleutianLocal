// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package infra

import (
	"errors"
	"io/fs"
	"testing"
	"time"
)

// =============================================================================
// MOCK IMPLEMENTATIONS
// =============================================================================

// mockCommandExecutor is a test double for CommandExecutor.
type mockCommandExecutor struct {
	output []byte
	err    error
	calls  []mockCall
}

type mockCall struct {
	name string
	args []string
}

func (m *mockCommandExecutor) Execute(name string, args ...string) ([]byte, error) {
	m.calls = append(m.calls, mockCall{name: name, args: args})
	return m.output, m.err
}

// mockFileWalker is a test double for FileWalker.
type mockFileWalker struct {
	files []mockFileInfo
	err   error
}

type mockFileInfo struct {
	path    string
	isDir   bool
	modTime time.Time
}

// mockDirEntry implements fs.DirEntry for testing.
type mockDirEntry struct {
	name    string
	isDir   bool
	modTime time.Time
}

func (m *mockDirEntry) Name() string               { return m.name }
func (m *mockDirEntry) IsDir() bool                { return m.isDir }
func (m *mockDirEntry) Type() fs.FileMode          { return 0 }
func (m *mockDirEntry) Info() (fs.FileInfo, error) { return &mockFileInfoImpl{modTime: m.modTime}, nil }

// mockFileInfoImpl implements fs.FileInfo for testing.
type mockFileInfoImpl struct {
	modTime time.Time
}

func (m *mockFileInfoImpl) Name() string       { return "" }
func (m *mockFileInfoImpl) Size() int64        { return 0 }
func (m *mockFileInfoImpl) Mode() fs.FileMode  { return 0 }
func (m *mockFileInfoImpl) ModTime() time.Time { return m.modTime }
func (m *mockFileInfoImpl) IsDir() bool        { return false }
func (m *mockFileInfoImpl) Sys() interface{}   { return nil }

func (m *mockFileWalker) Walk(root string, fn fs.WalkDirFunc) error {
	if m.err != nil {
		return m.err
	}
	for _, f := range m.files {
		entry := &mockDirEntry{
			name:    f.path,
			isDir:   f.isDir,
			modTime: f.modTime,
		}
		if err := fn(f.path, entry, nil); err != nil {
			if errors.Is(err, fs.SkipAll) {
				return nil
			}
			return err
		}
	}
	return nil
}

// =============================================================================
// TEST CASES - NeedsRebuild
// =============================================================================

func TestDefaultImageChecker_NeedsRebuild_ImageNotFound(t *testing.T) {
	// Arrange
	mockExec := &mockCommandExecutor{
		err: errors.New("image not found"),
	}
	mockWalk := &mockFileWalker{}
	checker := NewImageCheckerWithDeps(mockExec, mockWalk, nil)

	// Act
	needsRebuild, err := checker.NeedsRebuild("nonexistent-image", "/src", []string{".go"})

	// Assert
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}
	if needsRebuild {
		t.Error("Expected needsRebuild=false when image doesn't exist")
	}
}

func TestDefaultImageChecker_NeedsRebuild_NoNewerFiles(t *testing.T) {
	// Arrange
	imageTime := time.Date(2025, 1, 10, 12, 0, 0, 0, time.UTC)
	fileTime := time.Date(2025, 1, 9, 12, 0, 0, 0, time.UTC) // File is older

	mockExec := &mockCommandExecutor{
		output: []byte(imageTime.Format(time.RFC3339Nano)),
	}
	mockWalk := &mockFileWalker{
		files: []mockFileInfo{
			{path: "/src/main.go", isDir: false, modTime: fileTime},
			{path: "/src/utils.go", isDir: false, modTime: fileTime},
		},
	}
	checker := NewImageCheckerWithDeps(mockExec, mockWalk, nil)

	// Act
	needsRebuild, err := checker.NeedsRebuild("my-image", "/src", []string{".go"})

	// Assert
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}
	if needsRebuild {
		t.Error("Expected needsRebuild=false when all files are older than image")
	}
}

func TestDefaultImageChecker_NeedsRebuild_NewerFileExists(t *testing.T) {
	// Arrange
	imageTime := time.Date(2025, 1, 10, 12, 0, 0, 0, time.UTC)
	olderFileTime := time.Date(2025, 1, 9, 12, 0, 0, 0, time.UTC)
	newerFileTime := time.Date(2025, 1, 11, 12, 0, 0, 0, time.UTC) // File is newer

	mockExec := &mockCommandExecutor{
		output: []byte(imageTime.Format(time.RFC3339Nano)),
	}
	mockWalk := &mockFileWalker{
		files: []mockFileInfo{
			{path: "/src/old.go", isDir: false, modTime: olderFileTime},
			{path: "/src/new.go", isDir: false, modTime: newerFileTime},
		},
	}
	checker := NewImageCheckerWithDeps(mockExec, mockWalk, nil)

	// Act
	needsRebuild, err := checker.NeedsRebuild("my-image", "/src", []string{".go"})

	// Assert
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}
	if !needsRebuild {
		t.Error("Expected needsRebuild=true when a newer file exists")
	}
}

func TestDefaultImageChecker_NeedsRebuild_OnlyChecksMatchingExtensions(t *testing.T) {
	// Arrange
	imageTime := time.Date(2025, 1, 10, 12, 0, 0, 0, time.UTC)
	newerFileTime := time.Date(2025, 1, 11, 12, 0, 0, 0, time.UTC)

	mockExec := &mockCommandExecutor{
		output: []byte(imageTime.Format(time.RFC3339Nano)),
	}
	mockWalk := &mockFileWalker{
		files: []mockFileInfo{
			// .txt file is newer but shouldn't be checked
			{path: "/src/readme.txt", isDir: false, modTime: newerFileTime},
		},
	}
	checker := NewImageCheckerWithDeps(mockExec, mockWalk, nil)

	// Act
	needsRebuild, err := checker.NeedsRebuild("my-image", "/src", []string{".go", "Dockerfile"})

	// Assert
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}
	if needsRebuild {
		t.Error("Expected needsRebuild=false when only non-matching files are newer")
	}
}

func TestDefaultImageChecker_NeedsRebuild_DockerfileMatch(t *testing.T) {
	// Arrange
	imageTime := time.Date(2025, 1, 10, 12, 0, 0, 0, time.UTC)
	newerFileTime := time.Date(2025, 1, 11, 12, 0, 0, 0, time.UTC)

	mockExec := &mockCommandExecutor{
		output: []byte(imageTime.Format(time.RFC3339Nano)),
	}
	mockWalk := &mockFileWalker{
		files: []mockFileInfo{
			{path: "/src/Dockerfile", isDir: false, modTime: newerFileTime},
		},
	}
	checker := NewImageCheckerWithDeps(mockExec, mockWalk, nil)

	// Act
	needsRebuild, err := checker.NeedsRebuild("my-image", "/src", []string{".go", "Dockerfile"})

	// Assert
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}
	if !needsRebuild {
		t.Error("Expected needsRebuild=true when Dockerfile is newer")
	}
}

func TestDefaultImageChecker_NeedsRebuild_SkipsDirectories(t *testing.T) {
	// Arrange
	imageTime := time.Date(2025, 1, 10, 12, 0, 0, 0, time.UTC)
	newerFileTime := time.Date(2025, 1, 11, 12, 0, 0, 0, time.UTC)

	mockExec := &mockCommandExecutor{
		output: []byte(imageTime.Format(time.RFC3339Nano)),
	}
	mockWalk := &mockFileWalker{
		files: []mockFileInfo{
			// Directory should be skipped even if modtime is newer
			{path: "/src/handlers", isDir: true, modTime: newerFileTime},
		},
	}
	checker := NewImageCheckerWithDeps(mockExec, mockWalk, nil)

	// Act
	needsRebuild, err := checker.NeedsRebuild("my-image", "/src", []string{".go"})

	// Assert
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}
	if needsRebuild {
		t.Error("Expected needsRebuild=false when only directories exist")
	}
}

func TestDefaultImageChecker_NeedsRebuild_InvalidTimestamp(t *testing.T) {
	// Arrange
	mockExec := &mockCommandExecutor{
		output: []byte("invalid-timestamp"),
	}
	mockWalk := &mockFileWalker{}
	checker := NewImageCheckerWithDeps(mockExec, mockWalk, nil)

	// Act
	needsRebuild, err := checker.NeedsRebuild("my-image", "/src", []string{".go"})

	// Assert
	// When timestamp parsing fails, getImageCreationTime returns an error,
	// which NeedsRebuild treats as "image not found" and returns false, nil
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}
	if needsRebuild {
		t.Error("Expected needsRebuild=false when timestamp parsing fails")
	}
}

// =============================================================================
// TEST CASES - PruneDanglingImages
// =============================================================================

func TestDefaultImageChecker_PruneDanglingImages_Success(t *testing.T) {
	// Arrange
	mockExec := &mockCommandExecutor{
		output: []byte("Deleted Images:\nabc123\n"),
	}
	checker := NewImageCheckerWithDeps(mockExec, &mockFileWalker{}, nil)

	// Act
	err := checker.PruneDanglingImages()

	// Assert
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}
	if len(mockExec.calls) != 1 {
		t.Errorf("Expected 1 call, got %d", len(mockExec.calls))
	}
	if mockExec.calls[0].name != "podman" {
		t.Errorf("Expected command 'podman', got '%s'", mockExec.calls[0].name)
	}
	expectedArgs := []string{"image", "prune", "-f"}
	for i, arg := range expectedArgs {
		if mockExec.calls[0].args[i] != arg {
			t.Errorf("Expected arg[%d]='%s', got '%s'", i, arg, mockExec.calls[0].args[i])
		}
	}
}

func TestDefaultImageChecker_PruneDanglingImages_Error(t *testing.T) {
	// Arrange
	mockExec := &mockCommandExecutor{
		err: errors.New("podman not found"),
	}
	checker := NewImageCheckerWithDeps(mockExec, &mockFileWalker{}, nil)

	// Act
	err := checker.PruneDanglingImages()

	// Assert
	if err == nil {
		t.Error("Expected error, got nil")
	}
}

// =============================================================================
// TEST CASES - matchesExtension
// =============================================================================

func TestDefaultImageChecker_matchesExtension(t *testing.T) {
	checker := NewImageCheckerWithDeps(&mockCommandExecutor{}, &mockFileWalker{}, nil)

	tests := []struct {
		name       string
		path       string
		extensions []string
		want       bool
	}{
		{
			name:       "matches .go extension",
			path:       "/src/main.go",
			extensions: []string{".go"},
			want:       true,
		},
		{
			name:       "matches Dockerfile",
			path:       "/src/Dockerfile",
			extensions: []string{".go", "Dockerfile"},
			want:       true,
		},
		{
			name:       "no match",
			path:       "/src/readme.md",
			extensions: []string{".go", "Dockerfile"},
			want:       false,
		},
		{
			name:       "empty extensions",
			path:       "/src/main.go",
			extensions: []string{},
			want:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := checker.matchesExtension(tt.path, tt.extensions)
			if got != tt.want {
				t.Errorf("matchesExtension(%q, %v) = %v, want %v", tt.path, tt.extensions, got, tt.want)
			}
		})
	}
}

// =============================================================================
// TEST CASES - Constructor
// =============================================================================

func TestNewDefaultImageChecker(t *testing.T) {
	checker := NewDefaultImageChecker()
	if checker == nil {
		t.Error("Expected non-nil checker")
	}
}

func TestNewImageCheckerWithDeps_NilLogger(t *testing.T) {
	checker := NewImageCheckerWithDeps(&mockCommandExecutor{}, &mockFileWalker{}, nil)
	if checker == nil {
		t.Error("Expected non-nil checker")
	}
	if checker.logger == nil {
		t.Error("Expected default logger when nil provided")
	}
}
