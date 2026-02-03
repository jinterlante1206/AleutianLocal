// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package git

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/fsnotify/fsnotify"
)

// HeadWatcher watches for changes to .git/HEAD (branch switches).
//
// # Description
//
// Detects when the repository HEAD changes (e.g., via external git checkout
// from another terminal). Triggers cache invalidation when detected.
//
// # Thread Safety
//
// Safe for concurrent use. Start should only be called once.
type HeadWatcher struct {
	gitDir   string
	cache    CacheInvalidator
	watcher  *fsnotify.Watcher
	callback func()
}

// NewHeadWatcher creates a watcher for git HEAD changes.
//
// # Description
//
// Creates a watcher that monitors .git/HEAD and invokes the callback
// when changes are detected. The callback should invalidate the cache.
//
// # Inputs
//
//   - gitDir: Path to .git directory.
//   - cache: Cache invalidator (may be nil).
//   - callback: Optional callback on HEAD change (in addition to cache invalidation).
//
// # Outputs
//
//   - *HeadWatcher: Ready-to-start watcher.
//   - error: Non-nil if watcher creation fails.
func NewHeadWatcher(gitDir string, cache CacheInvalidator, callback func()) (*HeadWatcher, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	return &HeadWatcher{
		gitDir:   gitDir,
		cache:    cache,
		watcher:  watcher,
		callback: callback,
	}, nil
}

// Start begins watching for HEAD changes.
//
// # Description
//
// Watches .git/HEAD and related files for changes. Blocks until context
// is cancelled. Should be run in a goroutine.
//
// # Inputs
//
//   - ctx: Context for cancellation.
//
// # Example
//
//	watcher, _ := git.NewHeadWatcher(gitDir, cache, nil)
//	go watcher.Start(ctx)
func (w *HeadWatcher) Start(ctx context.Context) {
	// Watch .git/HEAD
	headPath := filepath.Join(w.gitDir, "HEAD")
	if err := w.watcher.Add(headPath); err != nil {
		slog.Warn("Failed to watch .git/HEAD",
			"path", headPath,
			"error", err)
	}

	// Also watch .git/refs/heads for branch updates
	refsPath := filepath.Join(w.gitDir, "refs", "heads")
	if _, err := os.Stat(refsPath); err == nil {
		if err := w.watcher.Add(refsPath); err != nil {
			slog.Debug("Failed to watch refs/heads",
				"path", refsPath,
				"error", err)
		}
	}

	// Watch for packed-refs changes (after git gc)
	packedRefs := filepath.Join(w.gitDir, "packed-refs")
	if _, err := os.Stat(packedRefs); err == nil {
		if err := w.watcher.Add(packedRefs); err != nil {
			slog.Debug("Failed to watch packed-refs",
				"path", packedRefs,
				"error", err)
		}
	}

	slog.Debug("Started watching git HEAD",
		"git_dir", w.gitDir)

	for {
		select {
		case event, ok := <-w.watcher.Events:
			if !ok {
				return
			}
			w.handleEvent(event)

		case err, ok := <-w.watcher.Errors:
			if !ok {
				return
			}
			slog.Warn("Git HEAD watcher error",
				"error", err)

		case <-ctx.Done():
			slog.Debug("Git HEAD watcher stopping")
			return
		}
	}
}

// handleEvent processes a single fsnotify event.
func (w *HeadWatcher) handleEvent(event fsnotify.Event) {
	// Only care about writes
	if event.Op&fsnotify.Write == 0 {
		return
	}

	slog.Info("Git HEAD changed externally, invalidating cache",
		"path", event.Name)

	// Invalidate cache
	if w.cache != nil {
		if err := w.cache.InvalidateAll(); err != nil {
			slog.Warn("Failed to invalidate cache after HEAD change",
				"error", err)
		}
	}

	// Call optional callback
	if w.callback != nil {
		w.callback()
	}
}

// Stop stops the watcher.
//
// # Description
//
// Stops watching and releases resources. Safe to call multiple times.
func (w *HeadWatcher) Stop() error {
	return w.watcher.Close()
}

// IsWorktree checks if the .git path is a worktree reference.
//
// # Description
//
// Git worktrees have a .git file (not directory) that points to the
// actual git directory. This affects where to watch for changes.
//
// # Inputs
//
//   - gitPath: Path to .git (file or directory).
//
// # Outputs
//
//   - bool: True if gitPath is a worktree reference file.
func IsWorktree(gitPath string) bool {
	info, err := os.Stat(gitPath)
	if err != nil {
		return false
	}
	// Worktrees have a .git file, not directory
	return !info.IsDir()
}

// ResolveWorktreeGitDir resolves the actual .git directory for a worktree.
//
// # Description
//
// If the repository is a worktree, .git is a file containing a path
// to the actual git directory. This resolves that reference.
//
// # Inputs
//
//   - gitPath: Path to .git file.
//
// # Outputs
//
//   - string: Path to actual git directory.
//   - error: Non-nil if not a worktree or parse fails.
func ResolveWorktreeGitDir(gitPath string) (string, error) {
	content, err := os.ReadFile(gitPath)
	if err != nil {
		return "", err
	}

	// Format: "gitdir: /path/to/actual/.git/worktrees/name"
	line := string(content)
	if len(line) < 8 || line[:8] != "gitdir: " {
		return "", os.ErrInvalid
	}

	actualPath := line[8:]
	// Remove trailing newline
	if len(actualPath) > 0 && actualPath[len(actualPath)-1] == '\n' {
		actualPath = actualPath[:len(actualPath)-1]
	}

	return actualPath, nil
}
