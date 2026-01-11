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
	"io/fs"
	"log/slog"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// =============================================================================
// INTERFACES
// =============================================================================

// ImageChecker determines whether a container image needs to be rebuilt.
//
// # Description
//
// ImageChecker compares source file timestamps against container image
// creation times to determine if a rebuild is necessary. This enables
// automatic detection of code changes without requiring explicit --build flags.
//
// # Methods
//
//   - NeedsRebuild: Returns true if source files are newer than the image.
//   - PruneDanglingImages: Removes unnamed/dangling images after builds.
//
// # Examples
//
//	checker := NewDefaultImageChecker()
//	if checker.NeedsRebuild("aleutian-go-orchestrator", "/path/to/services/orchestrator") {
//	    // Trigger rebuild
//	}
//
// # Limitations
//
//   - Only checks file modification times, not content hashes.
//   - Assumes podman is available in PATH.
//
// # Assumptions
//
//   - Podman is installed and accessible.
//   - Source directory contains the relevant source files.
type ImageChecker interface {
	// NeedsRebuild checks if source files are newer than the container image.
	//
	// # Inputs
	//
	//   - imageName: Name of the container image (e.g., "aleutian-go-orchestrator").
	//   - sourceDir: Path to the source directory to check for modifications.
	//   - extensions: File extensions to check (e.g., []string{".go", "Dockerfile"}).
	//
	// # Outputs
	//
	//   - bool: true if rebuild is needed, false otherwise.
	//   - error: Non-nil if checking failed (image doesn't exist returns false, nil).
	NeedsRebuild(imageName, sourceDir string, extensions []string) (bool, error)

	// PruneDanglingImages removes unnamed/dangling images to free disk space.
	//
	// # Outputs
	//
	//   - error: Non-nil if pruning failed (non-fatal, can be ignored).
	PruneDanglingImages() error
}

// CommandExecutor abstracts command execution for testing.
//
// # Description
//
// Allows mocking of exec.Command calls in unit tests.
//
// # Methods
//
//   - Execute: Runs a command and returns its output.
//
// # Limitations
//
//   - Does not support streaming output.
type CommandExecutor interface {
	// Execute runs the given command and returns stdout.
	//
	// # Inputs
	//
	//   - name: Command name.
	//   - args: Command arguments.
	//
	// # Outputs
	//
	//   - []byte: stdout output.
	//   - error: Non-nil if command failed.
	Execute(name string, args ...string) ([]byte, error)
}

// FileWalker abstracts filesystem walking for testing.
//
// # Description
//
// Allows mocking of filepath.WalkDir calls in unit tests.
//
// # Methods
//
//   - Walk: Traverses a directory tree calling fn for each file.
type FileWalker interface {
	// Walk traverses the directory tree rooted at root.
	//
	// # Inputs
	//
	//   - root: Starting directory.
	//   - fn: Callback function for each file/directory.
	//
	// # Outputs
	//
	//   - error: Non-nil if walking failed.
	Walk(root string, fn fs.WalkDirFunc) error
}

// =============================================================================
// STRUCTS
// =============================================================================

// DefaultImageChecker is the production implementation of ImageChecker.
//
// # Description
//
// Uses podman inspect to get image creation time and compares against
// file modification times in the source directory.
//
// # Fields
//
//   - executor: CommandExecutor for running podman commands.
//   - walker: FileWalker for traversing source directories.
//   - logger: Structured logger for debug output.
//
// # Thread Safety
//
// Safe for concurrent use. No shared mutable state.
//
// # Limitations
//
//   - Requires podman to be installed and accessible.
//
// # Assumptions
//
//   - Podman machine is running (on macOS).
type DefaultImageChecker struct {
	executor CommandExecutor
	walker   FileWalker
	logger   *slog.Logger
}

// defaultCommandExecutor is the production CommandExecutor.
type defaultCommandExecutor struct{}

// defaultFileWalker is the production FileWalker.
type defaultFileWalker struct{}

// =============================================================================
// CONSTRUCTORS
// =============================================================================

// NewDefaultImageChecker creates an ImageChecker with production dependencies.
//
// # Description
//
// Creates a DefaultImageChecker configured for production use with real
// command execution and filesystem walking.
//
// # Outputs
//
//   - ImageChecker: Ready-to-use image checker.
//
// # Examples
//
//	checker := NewDefaultImageChecker()
//	needsRebuild, err := checker.NeedsRebuild("my-image", "/src", []string{".go"})
func NewDefaultImageChecker() ImageChecker {
	return &DefaultImageChecker{
		executor: &defaultCommandExecutor{},
		walker:   &defaultFileWalker{},
		logger:   slog.Default(),
	}
}

// NewImageCheckerWithDeps creates an ImageChecker with injected dependencies.
//
// # Description
//
// Creates a DefaultImageChecker with custom dependencies for testing.
// Allows mocking of command execution and filesystem operations.
//
// # Inputs
//
//   - executor: CommandExecutor implementation.
//   - walker: FileWalker implementation.
//   - logger: Structured logger (can be nil for default).
//
// # Outputs
//
//   - *DefaultImageChecker: Configured checker (returns concrete type for testing).
//
// # Examples
//
//	mockExec := &mockCommandExecutor{output: []byte("2024-01-01T00:00:00Z")}
//	mockWalk := &mockFileWalker{}
//	checker := NewImageCheckerWithDeps(mockExec, mockWalk, nil)
func NewImageCheckerWithDeps(executor CommandExecutor, walker FileWalker, logger *slog.Logger) *DefaultImageChecker {
	if logger == nil {
		logger = slog.Default()
	}
	return &DefaultImageChecker{
		executor: executor,
		walker:   walker,
		logger:   logger,
	}
}

// =============================================================================
// METHOD IMPLEMENTATIONS - DefaultImageChecker
// =============================================================================

// NeedsRebuild checks if source files are newer than the container image.
//
// # Description
//
// Compares the modification time of files in sourceDir against the creation
// time of the container image. Returns true if any source file is newer.
//
// # Inputs
//
//   - imageName: Name of the container image to check.
//   - sourceDir: Path to source directory containing files to compare.
//   - extensions: File extensions to check (e.g., []string{".go", "Dockerfile"}).
//
// # Outputs
//
//   - bool: true if rebuild is needed, false otherwise.
//   - error: Non-nil only for unexpected errors (image not found returns false, nil).
//
// # Examples
//
//	needsRebuild, err := checker.NeedsRebuild(
//	    "aleutian-go-orchestrator",
//	    "/path/to/services/orchestrator",
//	    []string{".go", "Dockerfile"},
//	)
//
// # Limitations
//
//   - Only checks file modification times, not content.
//   - Stops at first newer file found (optimization).
//
// # Assumptions
//
//   - Podman is installed and the machine is running.
//   - sourceDir exists and is readable.
func (c *DefaultImageChecker) NeedsRebuild(imageName, sourceDir string, extensions []string) (bool, error) {
	// Step 1: Get image creation time
	imageTime, err := c.getImageCreationTime(imageName)
	if err != nil {
		// Image doesn't exist - compose will build it anyway
		c.logger.Debug("Image not found, will be built by compose",
			"image", imageName,
			"error", err)
		return false, nil
	}

	// Step 2: Check if any source files are newer
	needsRebuild := false
	walkErr := c.walker.Walk(sourceDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}

		if c.matchesExtension(path, extensions) {
			info, infoErr := d.Info()
			if infoErr == nil && info.ModTime().After(imageTime) {
				c.logger.Debug("Source file newer than image",
					"file", path,
					"file_time", info.ModTime(),
					"image_time", imageTime)
				needsRebuild = true
				return filepath.SkipAll // Found one, stop walking
			}
		}
		return nil
	})

	if walkErr != nil {
		c.logger.Debug("Error walking source directory", "error", walkErr)
	}

	return needsRebuild, nil
}

// PruneDanglingImages removes unnamed/dangling images.
//
// # Description
//
// Runs "podman image prune -f" to clean up dangling images that accumulate
// during rebuilds. This is a non-critical operation.
//
// # Outputs
//
//   - error: Non-nil if prune command failed.
//
// # Examples
//
//	if err := checker.PruneDanglingImages(); err != nil {
//	    log.Printf("Warning: could not prune images: %v", err)
//	}
//
// # Limitations
//
//   - Removes ALL dangling images, not just Aleutian-related ones.
//
// # Assumptions
//
//   - Podman is installed.
func (c *DefaultImageChecker) PruneDanglingImages() error {
	_, err := c.executor.Execute("podman", "image", "prune", "-f")
	return err
}

// getImageCreationTime retrieves the creation timestamp of a container image.
//
// # Description
//
// Uses "podman inspect" to get the image's Created field.
//
// # Inputs
//
//   - imageName: Name of the image to inspect.
//
// # Outputs
//
//   - time.Time: Image creation timestamp.
//   - error: Non-nil if image doesn't exist or inspect failed.
func (c *DefaultImageChecker) getImageCreationTime(imageName string) (time.Time, error) {
	output, err := c.executor.Execute("podman", "inspect", "--format", "{{.Created}}", imageName)
	if err != nil {
		return time.Time{}, err
	}

	timeStr := strings.TrimSpace(string(output))
	return time.Parse(time.RFC3339Nano, timeStr)
}

// matchesExtension checks if a file path ends with any of the given extensions.
//
// # Description
//
// Simple suffix matching for file extension filtering.
//
// # Inputs
//
//   - path: File path to check.
//   - extensions: Extensions to match (e.g., ".go", "Dockerfile").
//
// # Outputs
//
//   - bool: true if path ends with any extension.
func (c *DefaultImageChecker) matchesExtension(path string, extensions []string) bool {
	for _, ext := range extensions {
		if strings.HasSuffix(path, ext) {
			return true
		}
	}
	return false
}

// =============================================================================
// METHOD IMPLEMENTATIONS - defaultCommandExecutor
// =============================================================================

// Execute runs a command and returns its stdout.
func (e *defaultCommandExecutor) Execute(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).Output()
}

// =============================================================================
// METHOD IMPLEMENTATIONS - defaultFileWalker
// =============================================================================

// Walk traverses a directory tree.
func (w *defaultFileWalker) Walk(root string, fn fs.WalkDirFunc) error {
	return filepath.WalkDir(root, fn)
}
