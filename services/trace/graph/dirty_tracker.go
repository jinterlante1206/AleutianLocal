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
	"sync"
	"time"
)

// DirtyEntry contains metadata about a dirty file.
type DirtyEntry struct {
	// Path is the file path (absolute or project-relative).
	Path string

	// MarkedAt is when the file was marked dirty.
	MarkedAt time.Time

	// Source indicates how the file became dirty ("agent", "watcher", "manual").
	Source string
}

// DirtyTracker tracks files modified during agent execution.
//
// Description:
//
//	Tracks which files have been modified and need graph refresh.
//	Used by the execute phase to trigger incremental graph updates
//	before tool queries return stale data.
//
// Thread Safety:
//
//	All methods are safe for concurrent use.
type DirtyTracker struct {
	mu         sync.RWMutex
	dirtyFiles map[string]DirtyEntry // path → entry
	enabled    bool
}

// NewDirtyTracker creates a new tracker.
//
// Description:
//
//	Creates a tracker for monitoring file modifications during
//	agent execution. Tracks which files need re-parsing.
//
// Inputs:
//
//	None.
//
// Outputs:
//
//	*DirtyTracker - Ready to track file changes.
//
// Thread Safety:
//
//	The returned tracker is safe for concurrent use.
func NewDirtyTracker() *DirtyTracker {
	return &DirtyTracker{
		dirtyFiles: make(map[string]DirtyEntry),
		enabled:    true,
	}
}

// MarkDirty marks a file as modified and needing refresh.
//
// Description:
//
//	Called by tools after writing to a file. Records the file
//	path and timestamp for later incremental refresh.
//
// Inputs:
//
//	path - Absolute or project-relative file path.
//
// Outputs:
//
//	None.
//
// Thread Safety:
//
//	Safe for concurrent use.
func (d *DirtyTracker) MarkDirty(path string) {
	d.MarkDirtyWithSource(path, "agent")
}

// MarkDirtyWithSource marks a file dirty with a specific source.
//
// Description:
//
//	Same as MarkDirty but allows specifying the source of the change.
//	Useful for distinguishing between agent modifications and external
//	file watcher events.
//
// Inputs:
//
//	path - Absolute or project-relative file path.
//	source - The source of the modification ("agent", "watcher", "manual").
//
// Outputs:
//
//	None.
//
// Thread Safety:
//
//	Safe for concurrent use.
func (d *DirtyTracker) MarkDirtyWithSource(path, source string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if !d.enabled {
		return
	}

	d.dirtyFiles[path] = DirtyEntry{
		Path:     path,
		MarkedAt: time.Now(),
		Source:   source,
	}
}

// MarkDirtyFromWatcher handles a file change from the FileWatcher.
//
// Description:
//
//	Integrates with the existing FileWatcher to mark files dirty
//	when external changes are detected. Skips removed files.
//
// Inputs:
//
//	change - The file change event from FileWatcher.
//
// Outputs:
//
//	None.
//
// Thread Safety:
//
//	Safe for concurrent use.
func (d *DirtyTracker) MarkDirtyFromWatcher(change FileChange) {
	// Skip removed files - they need deletion, not refresh
	if change.Op == FileOpRemove {
		return
	}

	d.MarkDirtyWithSource(change.Path, "watcher")
}

// HasDirty returns true if any files are marked dirty.
//
// Description:
//
//	Quick check before tool execution to determine if
//	incremental refresh is needed.
//
// Inputs:
//
//	None.
//
// Outputs:
//
//	bool - True if dirty files exist.
//
// Thread Safety:
//
//	Safe for concurrent use.
func (d *DirtyTracker) HasDirty() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return len(d.dirtyFiles) > 0
}

// Count returns the number of dirty files.
//
// Outputs:
//
//	int - Number of dirty files.
//
// Thread Safety:
//
//	Safe for concurrent use.
func (d *DirtyTracker) Count() int {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return len(d.dirtyFiles)
}

// GetDirtyFiles returns all dirty file paths without clearing.
//
// Description:
//
//	Returns a copy of the dirty file paths. Does NOT clear the
//	dirty set. Use Clear() after successful refresh.
//
// Inputs:
//
//	None.
//
// Outputs:
//
//	[]string - Paths of dirty files. Empty slice if none.
//
// Thread Safety:
//
//	Safe for concurrent use. Returns a copy.
func (d *DirtyTracker) GetDirtyFiles() []string {
	d.mu.RLock()
	defer d.mu.RUnlock()

	if len(d.dirtyFiles) == 0 {
		return []string{}
	}

	paths := make([]string, 0, len(d.dirtyFiles))
	for path := range d.dirtyFiles {
		paths = append(paths, path)
	}

	return paths
}

// GetDirtyEntries returns all dirty entries with metadata.
//
// Description:
//
//	Returns full metadata about dirty files including timestamps
//	and sources. Does NOT clear the dirty set.
//
// Inputs:
//
//	None.
//
// Outputs:
//
//	[]DirtyEntry - Dirty file entries. Empty slice if none.
//
// Thread Safety:
//
//	Safe for concurrent use. Returns copies.
func (d *DirtyTracker) GetDirtyEntries() []DirtyEntry {
	d.mu.RLock()
	defer d.mu.RUnlock()

	if len(d.dirtyFiles) == 0 {
		return []DirtyEntry{}
	}

	entries := make([]DirtyEntry, 0, len(d.dirtyFiles))
	for _, entry := range d.dirtyFiles {
		entries = append(entries, entry)
	}

	return entries
}

// Clear removes specified paths from the dirty set.
//
// Description:
//
//	Called after successful refresh to clear the dirty files
//	that were refreshed. Only clears the specified paths.
//
// Inputs:
//
//	paths - Paths to clear from dirty set.
//
// Outputs:
//
//	int - Number of paths actually cleared.
//
// Thread Safety:
//
//	Safe for concurrent use.
func (d *DirtyTracker) Clear(paths []string) int {
	d.mu.Lock()
	defer d.mu.Unlock()

	cleared := 0
	for _, path := range paths {
		if _, exists := d.dirtyFiles[path]; exists {
			delete(d.dirtyFiles, path)
			cleared++
		}
	}

	return cleared
}

// ClearAll removes all paths from the dirty set.
//
// Description:
//
//	Clears the entire dirty set. Use after a full refresh
//	or when resetting the tracker.
//
// Outputs:
//
//	int - Number of paths cleared.
//
// Thread Safety:
//
//	Safe for concurrent use.
func (d *DirtyTracker) ClearAll() int {
	d.mu.Lock()
	defer d.mu.Unlock()

	count := len(d.dirtyFiles)
	d.dirtyFiles = make(map[string]DirtyEntry)
	return count
}

// Enable enables dirty tracking.
//
// Thread Safety:
//
//	Safe for concurrent use.
func (d *DirtyTracker) Enable() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.enabled = true
}

// Disable turns off dirty tracking.
//
// Description:
//
//	When disabled, MarkDirty calls are no-ops. Useful for
//	read-only sessions where no file modifications are expected.
//
// Thread Safety:
//
//	Safe for concurrent use.
func (d *DirtyTracker) Disable() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.enabled = false
}

// IsEnabled returns true if tracking is enabled.
//
// Thread Safety:
//
//	Safe for concurrent use.
func (d *DirtyTracker) IsEnabled() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.enabled
}
