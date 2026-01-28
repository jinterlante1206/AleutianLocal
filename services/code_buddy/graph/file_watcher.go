// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package graph

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// FileChange represents a file system change event.
type FileChange struct {
	// Path is the absolute path to the changed file.
	Path string

	// Op is the type of change.
	Op FileOp

	// Time is when the change was detected.
	Time time.Time
}

// FileOp represents the type of file operation.
type FileOp int

const (
	// FileOpCreate indicates a file was created.
	FileOpCreate FileOp = iota

	// FileOpWrite indicates a file was modified.
	FileOpWrite

	// FileOpRemove indicates a file was deleted.
	FileOpRemove

	// FileOpRename indicates a file was renamed.
	FileOpRename
)

// String returns the string representation of the operation.
func (op FileOp) String() string {
	switch op {
	case FileOpCreate:
		return "create"
	case FileOpWrite:
		return "write"
	case FileOpRemove:
		return "remove"
	case FileOpRename:
		return "rename"
	default:
		return "unknown"
	}
}

// FileChangeHandler is called when debounced changes are ready.
type FileChangeHandler func(changes []FileChange)

// FileWatcher watches for file changes with debouncing.
//
// # Description
//
// Watches a directory for file changes and batches them using a debounce
// window. This prevents triggering updates for every keystroke during
// active editing.
//
// # Debouncing
//
// Changes are collected into a buffer. When the debounce period expires
// without new changes, all collected changes are sent to the handler.
// This is implemented using channels for efficient, non-blocking operation.
//
// # Thread Safety
//
// Safe for concurrent use. The handler is called from a single goroutine.
type FileWatcher struct {
	root          string
	watcher       *fsnotify.Watcher
	handler       FileChangeHandler
	debounce      time.Duration
	ignorePattern []string

	// Channels for communication
	changes  chan FileChange
	done     chan struct{}
	stopOnce sync.Once

	mu       sync.RWMutex
	watching bool
}

// FileWatcherOptions configures the FileWatcher.
type FileWatcherOptions struct {
	// DebounceWindow is how long to wait for more changes before triggering.
	// Default: 100ms
	DebounceWindow time.Duration

	// IgnorePatterns are glob patterns for files/directories to ignore.
	// Default: [".git", "node_modules", ".idea", "*.swp", "*.tmp"]
	IgnorePatterns []string

	// BufferSize is the size of the change buffer channel.
	// Default: 1000
	BufferSize int
}

// DefaultFileWatcherOptions returns sensible defaults.
func DefaultFileWatcherOptions() FileWatcherOptions {
	return FileWatcherOptions{
		DebounceWindow: 100 * time.Millisecond,
		IgnorePatterns: []string{".git", "node_modules", ".idea", "*.swp", "*.tmp", "__pycache__"},
		BufferSize:     1000,
	}
}

// NewFileWatcher creates a new file watcher for the given root directory.
//
// # Inputs
//
//   - root: Absolute path to the directory to watch.
//   - handler: Function called with batched changes after debounce.
//   - opts: Optional configuration (nil uses defaults).
//
// # Outputs
//
//   - *FileWatcher: Ready-to-use watcher (call Start to begin watching).
//   - error: Non-nil if the watcher could not be created.
//
// # Example
//
//	watcher, err := NewFileWatcher("/path/to/project", func(changes []FileChange) {
//	    log.Printf("Changes detected: %d files", len(changes))
//	    // Trigger incremental graph update
//	}, nil)
//	if err != nil {
//	    return err
//	}
//	defer watcher.Stop()
//	if err := watcher.Start(ctx); err != nil {
//	    return err
//	}
func NewFileWatcher(root string, handler FileChangeHandler, opts *FileWatcherOptions) (*FileWatcher, error) {
	if opts == nil {
		defaults := DefaultFileWatcherOptions()
		opts = &defaults
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	return &FileWatcher{
		root:          root,
		watcher:       watcher,
		handler:       handler,
		debounce:      opts.DebounceWindow,
		ignorePattern: opts.IgnorePatterns,
		changes:       make(chan FileChange, opts.BufferSize),
		done:          make(chan struct{}),
	}, nil
}

// Start begins watching for file changes.
//
// # Description
//
// Recursively watches the root directory and all subdirectories.
// Changes are debounced and sent to the handler in batches.
//
// # Inputs
//
//   - ctx: Context for cancellation. When canceled, watching stops.
//
// # Outputs
//
//   - error: Non-nil if watching could not be started.
//
// # Behavior
//
// Spawns two goroutines:
//   - Event processor: Converts fsnotify events to FileChange
//   - Debouncer: Batches changes and calls handler
//
// Both goroutines exit when Stop() is called or context is canceled.
func (w *FileWatcher) Start(ctx context.Context) error {
	w.mu.Lock()
	if w.watching {
		w.mu.Unlock()
		return nil // Already watching
	}
	w.watching = true
	w.mu.Unlock()

	// Add root directory recursively
	if err := w.addRecursive(w.root); err != nil {
		return err
	}

	// Start event processor
	go w.processEvents(ctx)

	// Start debouncer
	go w.debounceLoop(ctx)

	return nil
}

// Stop stops the file watcher.
func (w *FileWatcher) Stop() {
	w.stopOnce.Do(func() {
		close(w.done)
		w.watcher.Close()

		w.mu.Lock()
		w.watching = false
		w.mu.Unlock()
	})
}

// IsWatching returns true if the watcher is currently active.
func (w *FileWatcher) IsWatching() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.watching
}

// addRecursive adds a directory and all subdirectories to the watch list.
func (w *FileWatcher) addRecursive(root string) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // Ignore errors, continue walking
		}

		// Only watch directories
		if !d.IsDir() {
			return nil
		}

		// Check ignore patterns
		if w.shouldIgnore(path) {
			return filepath.SkipDir
		}

		// Add to watcher
		return w.watcher.Add(path)
	})
}

// shouldIgnore checks if a path matches any ignore pattern.
func (w *FileWatcher) shouldIgnore(path string) bool {
	base := filepath.Base(path)

	for _, pattern := range w.ignorePattern {
		// Check direct name match
		if base == pattern {
			return true
		}

		// Check glob pattern
		matched, _ := filepath.Match(pattern, base)
		if matched {
			return true
		}

		// Check if path contains the pattern (for directories)
		if strings.Contains(path, pattern) {
			return true
		}
	}

	return false
}

// processEvents converts fsnotify events to FileChange and sends to channel.
func (w *FileWatcher) processEvents(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-w.done:
			return
		case event, ok := <-w.watcher.Events:
			if !ok {
				return
			}

			// Skip ignored paths
			if w.shouldIgnore(event.Name) {
				continue
			}

			// Convert to FileChange
			change := FileChange{
				Path: event.Name,
				Time: time.Now(),
				Op:   w.convertOp(event.Op),
			}

			// Send to debounce channel (non-blocking)
			select {
			case w.changes <- change:
			default:
				// Buffer full, drop oldest or skip
				// In practice, the debouncer should keep up
			}

			// If new directory created, add it to watcher
			if event.Has(fsnotify.Create) {
				if info, err := getFileInfo(event.Name); err == nil && info {
					w.watcher.Add(event.Name)
				}
			}

		case err, ok := <-w.watcher.Errors:
			if !ok {
				return
			}
			// Log error but continue watching
			_ = err // In production, log this
		}
	}
}

// getFileInfo returns true if path is a directory.
func getFileInfo(path string) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		return false, err
	}
	return info.IsDir(), nil
}

// convertOp converts fsnotify.Op to FileOp.
func (w *FileWatcher) convertOp(op fsnotify.Op) FileOp {
	switch {
	case op.Has(fsnotify.Create):
		return FileOpCreate
	case op.Has(fsnotify.Write):
		return FileOpWrite
	case op.Has(fsnotify.Remove):
		return FileOpRemove
	case op.Has(fsnotify.Rename):
		return FileOpRename
	default:
		return FileOpWrite // Default to write
	}
}

// debounceLoop batches changes and calls handler after debounce window.
func (w *FileWatcher) debounceLoop(ctx context.Context) {
	var batch []FileChange
	var timer *time.Timer
	var timerC <-chan time.Time

	flush := func() {
		if len(batch) > 0 {
			// Deduplicate changes
			deduped := w.deduplicateChanges(batch)
			if len(deduped) > 0 && w.handler != nil {
				w.handler(deduped)
			}
			batch = batch[:0]
		}
		if timer != nil {
			timer.Stop()
			timer = nil
			timerC = nil
		}
	}

	for {
		select {
		case <-ctx.Done():
			flush()
			return
		case <-w.done:
			flush()
			return
		case change := <-w.changes:
			batch = append(batch, change)

			// Reset or start debounce timer
			if timer == nil {
				timer = time.NewTimer(w.debounce)
				timerC = timer.C
			} else {
				timer.Reset(w.debounce)
			}

		case <-timerC:
			// Debounce window expired, flush batch
			flush()
		}
	}
}

// deduplicateChanges removes duplicate changes for the same file.
// Keeps the most recent change per path.
func (w *FileWatcher) deduplicateChanges(changes []FileChange) []FileChange {
	seen := make(map[string]int) // path -> index in result
	result := make([]FileChange, 0, len(changes))

	for _, change := range changes {
		if idx, exists := seen[change.Path]; exists {
			// Replace with newer change
			result[idx] = change
		} else {
			seen[change.Path] = len(result)
			result = append(result, change)
		}
	}

	return result
}

// AddPattern adds an ignore pattern.
func (w *FileWatcher) AddPattern(pattern string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.ignorePattern = append(w.ignorePattern, pattern)
}

// SetHandler changes the change handler.
func (w *FileWatcher) SetHandler(handler FileChangeHandler) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.handler = handler
}
