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
Package main provides FileDiagnosticsViewer for local diagnostic retrieval.

FileDiagnosticsViewer is the FOSS-tier implementation that retrieves diagnostics
from local file storage. This enables:

  - `aleutian diagnose --view <id>` command
  - Local trace ID lookup ("Support Ticket Revolution")
  - Historical diagnostic listing with filtering

# Open Core Architecture

This viewer follows the Open Core model:

  - FOSS (this file): Looks up diagnostics from ~/.aleutian/diagnostics/
  - Enterprise: LokiDiagnosticsViewer queries centralized Loki/Splunk

The interface is public; the implementation dictates the value.

# Support Ticket Revolution

The GetByTraceID method is the key feature: users provide a trace ID from
Jaeger or logs instead of pasting 500 lines of output.
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
)

// -----------------------------------------------------------------------------
// FileDiagnosticsViewer Implementation
// -----------------------------------------------------------------------------

// FileDiagnosticsViewer retrieves diagnostics from local file storage.
//
// This is the FOSS-tier viewer that enables local diagnostic retrieval.
// Users can look up diagnostics by path, ID, or trace ID.
//
// # Enterprise Alternative
//
// LokiDiagnosticsViewer (Enterprise) enables:
//   - Centralized trace ID lookup across all machines
//   - Query diagnostics from Alice's dashboard for Bob's machine
//   - Full-text search across diagnostic content
//
// # Capabilities
//
//   - Get by file path or trace ID
//   - List with filtering by severity, time, reason
//   - Pagination support for large diagnostic histories
//
// # Thread Safety
//
// FileDiagnosticsViewer is safe for concurrent use.
type FileDiagnosticsViewer struct {
	// storage is the file storage backend to read from.
	storage *FileDiagnosticsStorage

	// cache stores parsed diagnostics to avoid re-reading files.
	// Key is file path, value is parsed DiagnosticsData.
	cache map[string]*DiagnosticsData

	// mu protects the cache.
	mu sync.RWMutex
}

// NewFileDiagnosticsViewer creates a viewer backed by file storage.
//
// # Description
//
// Creates a FOSS-tier viewer that retrieves diagnostics from the local
// filesystem. Uses the provided storage backend for file operations.
//
// # Inputs
//
//   - storage: FileDiagnosticsStorage to read from
//
// # Outputs
//
//   - *FileDiagnosticsViewer: Ready-to-use viewer
//
// # Examples
//
//	storage, _ := NewFileDiagnosticsStorage("")
//	viewer := NewFileDiagnosticsViewer(storage)
//	data, err := viewer.GetByTraceID(ctx, "abc123...")
//
// # Limitations
//
//   - Only reads from local filesystem
//   - Cannot query other machines' diagnostics
//   - Cache is not shared across viewer instances
//
// # Assumptions
//
//   - Storage is initialized and readable
//   - Storage base directory contains valid diagnostic files
func NewFileDiagnosticsViewer(storage *FileDiagnosticsStorage) *FileDiagnosticsViewer {
	return &FileDiagnosticsViewer{
		storage: storage,
		cache:   make(map[string]*DiagnosticsData),
	}
}

// Get retrieves a diagnostic by ID or path.
//
// # Description
//
// Loads and parses a diagnostic from storage. The id parameter can be:
//   - Full file path: /path/to/diag.json
//   - Relative filename: diag-20240105-100000.json
//   - Trace ID: 1704463200000000000-12345
//
// # Inputs
//
//   - ctx: Context for cancellation and timeout
//   - id: Diagnostic ID, filename, or path
//
// # Outputs
//
//   - *DiagnosticsData: Parsed diagnostic data
//   - error: Non-nil if not found or parse fails
//
// # Examples
//
//	// By full path
//	data, err := viewer.Get(ctx, "/home/user/.aleutian/diagnostics/diag-xxx.json")
//	if err != nil {
//	    log.Printf("Not found: %v", err)
//	}
//
//	// By filename only
//	data, err := viewer.Get(ctx, "diag-20240105-100000.json")
//
// # Limitations
//
//   - Pruned diagnostics cannot be retrieved
//   - Only JSON format is supported
//   - Trace ID lookup scans all files (use GetByTraceID directly for clarity)
//
// # Assumptions
//
//   - File exists and is readable
//   - File contains valid JSON diagnostic data
func (v *FileDiagnosticsViewer) Get(ctx context.Context, id string) (*DiagnosticsData, error) {
	// Check if id looks like a trace ID
	if v.looksLikeTraceID(id) {
		return v.GetByTraceID(ctx, id)
	}

	// Resolve to full path if needed
	path := v.resolveToPath(id)

	// Check cache first
	if data := v.getCached(path); data != nil {
		return data, nil
	}

	// Load from storage
	content, err := v.storage.Load(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("failed to load diagnostic: %w", err)
	}

	// Parse JSON
	data, err := v.parseDiagnostics(content)
	if err != nil {
		return nil, fmt.Errorf("failed to parse diagnostic: %w", err)
	}

	// Cache for future lookups
	v.setCached(path, data)

	return data, nil
}

// List returns recent diagnostics metadata.
//
// # Description
//
// Returns summaries of stored diagnostics for listing without loading
// full content. Supports filtering by severity, time range, and reason.
// Results are sorted by timestamp descending (newest first).
//
// # Inputs
//
//   - ctx: Context for cancellation and timeout
//   - opts: Filter and pagination options (use ListOptions{} for defaults)
//
// # Outputs
//
//   - []DiagnosticsSummary: Metadata for matching diagnostics
//   - error: Non-nil if listing fails
//
// # Examples
//
//	// List errors only
//	summaries, err := viewer.List(ctx, ListOptions{
//	    Limit:    20,
//	    Severity: SeverityError,
//	})
//	for _, s := range summaries {
//	    fmt.Printf("[%s] %s - %s\n", s.Severity, s.Reason, s.TraceID)
//	}
//
//	// List all with pagination
//	summaries, err := viewer.List(ctx, ListOptions{
//	    Limit:  10,
//	    Offset: 20,
//	})
//
// # Limitations
//
//   - Requires reading each file's header for filtering
//   - May be slow with thousands of diagnostics
//   - Filtering is done in memory, not indexed
//
// # Assumptions
//
//   - Storage is accessible and contains valid diagnostic files
//   - Files are JSON format
func (v *FileDiagnosticsViewer) List(ctx context.Context, opts ListOptions) ([]DiagnosticsSummary, error) {
	opts = opts.WithDefaults()

	// Get all diagnostic paths
	paths, err := v.storage.List(ctx, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to list diagnostics: %w", err)
	}

	// Parse each file and filter
	summaries := v.buildFilteredSummaries(ctx, paths, opts)

	// Sort by timestamp descending (newest first)
	v.sortSummariesByTimestamp(summaries)

	// Apply pagination
	summaries = v.applyPagination(summaries, opts)

	return summaries, nil
}

// GetByTraceID retrieves a diagnostic by its OpenTelemetry trace ID.
//
// # Description
//
// Finds and loads a diagnostic using the trace ID from Jaeger or logs.
// This is the "Support Ticket Revolution" - users provide trace ID
// instead of pasting 500 lines of output.
//
// # Inputs
//
//   - ctx: Context for cancellation and timeout
//   - traceID: Trace ID string from Jaeger or diagnostic output
//
// # Outputs
//
//   - *DiagnosticsData: Parsed diagnostic data
//   - error: Non-nil if not found
//
// # Examples
//
//	// User provides trace ID from error output
//	data, err := viewer.GetByTraceID(ctx, "1704463200000000000-12345")
//	if err != nil {
//	    fmt.Println("Trace not found - may have been pruned")
//	    return
//	}
//	fmt.Printf("Found: %s - %s\n", data.Header.Reason, data.Header.Details)
//
// # Limitations
//
//   - Must scan all files to find matching trace ID (O(n) complexity)
//   - May be slow with thousands of diagnostics
//   - Enterprise uses indexed Loki/Splunk for O(1) lookup
//
// # Assumptions
//
//   - Trace ID is from a diagnostic stored on this machine
//   - Diagnostic has not been pruned by retention policy
func (v *FileDiagnosticsViewer) GetByTraceID(ctx context.Context, traceID string) (*DiagnosticsData, error) {
	// Get all diagnostic paths
	paths, err := v.storage.List(ctx, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to list diagnostics: %w", err)
	}

	// Search for matching trace ID
	for _, path := range paths {
		// Check cancellation
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		data, err := v.loadDiagnostic(ctx, path)
		if err != nil {
			continue // Skip unparseable files
		}

		if data.Header.TraceID == traceID {
			return data, nil
		}
	}

	return nil, fmt.Errorf("diagnostic with trace ID %q not found", traceID)
}

// -----------------------------------------------------------------------------
// Private Methods - Path Resolution
// -----------------------------------------------------------------------------

// looksLikeTraceID checks if the string appears to be a trace ID.
//
// # Description
//
// Heuristic to distinguish trace IDs from file paths.
// Trace IDs typically contain only digits and hyphens with significant length.
//
// # Inputs
//
//   - s: String to check
//
// # Outputs
//
//   - bool: True if appears to be a trace ID
//
// # Examples
//
//	looksLikeTraceID("1704463200000000000-12345")  // true
//	looksLikeTraceID("/path/to/file.json")        // false
//	looksLikeTraceID("diag-20240105.json")        // false
//
// # Limitations
//
//   - Heuristic may produce false positives/negatives for edge cases
//
// # Assumptions
//
//   - Trace IDs follow the format: nanoseconds-pid
func (v *FileDiagnosticsViewer) looksLikeTraceID(s string) bool {
	// Not a trace ID if it looks like a path
	if strings.Contains(s, "/") || strings.Contains(s, "\\") {
		return false
	}

	// Not a trace ID if it has file extension
	if strings.HasSuffix(s, ".json") || strings.HasSuffix(s, ".txt") {
		return false
	}

	// Trace ID should have significant length and contain a hyphen
	if len(s) > 10 && strings.Contains(s, "-") {
		// Check if it's mostly digits and hyphens
		digitCount := 0
		for _, r := range s {
			if r >= '0' && r <= '9' {
				digitCount++
			}
		}
		return digitCount > len(s)/2
	}

	return false
}

// resolveToPath converts an ID to a full file path.
//
// # Description
//
// If the ID is already a full path, returns it. Otherwise, joins
// with the storage base directory.
//
// # Inputs
//
//   - id: Diagnostic ID or path
//
// # Outputs
//
//   - string: Full file path
//
// # Examples
//
//	resolveToPath("/full/path/diag.json")  // "/full/path/diag.json"
//	resolveToPath("diag.json")             // "/home/user/.aleutian/diagnostics/diag.json"
//
// # Limitations
//
//   - Does not validate path existence
//
// # Assumptions
//
//   - Storage base directory is set correctly
func (v *FileDiagnosticsViewer) resolveToPath(id string) string {
	if filepath.IsAbs(id) {
		return id
	}
	return filepath.Join(v.storage.BaseDir(), id)
}

// -----------------------------------------------------------------------------
// Private Methods - Data Loading
// -----------------------------------------------------------------------------

// loadDiagnostic loads and parses a diagnostic file.
//
// # Description
//
// Loads from cache if available, otherwise reads and parses the file.
// Automatically caches successfully parsed diagnostics.
//
// # Inputs
//
//   - ctx: Context for cancellation
//   - path: Full file path
//
// # Outputs
//
//   - *DiagnosticsData: Parsed diagnostic
//   - error: Non-nil if load/parse fails
//
// # Examples
//
//	data, err := v.loadDiagnostic(ctx, "/path/to/diag.json")
//
// # Limitations
//
//   - Reads entire file into memory
//
// # Assumptions
//
//   - File exists and contains valid JSON
func (v *FileDiagnosticsViewer) loadDiagnostic(ctx context.Context, path string) (*DiagnosticsData, error) {
	// Check cache first
	if data := v.getCached(path); data != nil {
		return data, nil
	}

	// Load from storage
	content, err := v.storage.Load(ctx, path)
	if err != nil {
		return nil, err
	}

	// Parse JSON
	data, err := v.parseDiagnostics(content)
	if err != nil {
		return nil, err
	}

	// Cache for future lookups
	v.setCached(path, data)

	return data, nil
}

// parseDiagnostics parses JSON content into DiagnosticsData.
//
// # Description
//
// Unmarshals JSON bytes into a DiagnosticsData struct.
//
// # Inputs
//
//   - content: Raw JSON bytes
//
// # Outputs
//
//   - *DiagnosticsData: Parsed data
//   - error: Non-nil if JSON is invalid
//
// # Examples
//
//	data, err := v.parseDiagnostics([]byte(`{"header":{}}`))
//
// # Limitations
//
//   - Only supports current schema version
//
// # Assumptions
//
//   - Content is valid UTF-8 JSON
func (v *FileDiagnosticsViewer) parseDiagnostics(content []byte) (*DiagnosticsData, error) {
	var data DiagnosticsData
	if err := json.Unmarshal(content, &data); err != nil {
		return nil, err
	}
	return &data, nil
}

// -----------------------------------------------------------------------------
// Private Methods - Filtering and Pagination
// -----------------------------------------------------------------------------

// buildFilteredSummaries creates summaries for paths matching filters.
//
// # Description
//
// Iterates through paths, loads each diagnostic, applies filters,
// and builds summary objects for matching diagnostics.
//
// # Inputs
//
//   - ctx: Context for cancellation
//   - paths: File paths to process
//   - opts: Filter options
//
// # Outputs
//
//   - []DiagnosticsSummary: Summaries for matching diagnostics
//
// # Examples
//
//	summaries := v.buildFilteredSummaries(ctx, paths, opts)
//
// # Limitations
//
//   - Loads each file to apply filters
//
// # Assumptions
//
//   - Paths are valid diagnostic file paths
func (v *FileDiagnosticsViewer) buildFilteredSummaries(ctx context.Context, paths []string, opts ListOptions) []DiagnosticsSummary {
	var summaries []DiagnosticsSummary

	for _, path := range paths {
		// Check cancellation
		if ctx.Err() != nil {
			break
		}

		// Load diagnostic
		data, err := v.loadDiagnostic(ctx, path)
		if err != nil {
			continue // Skip unparseable files
		}

		// Apply filters
		if !v.matchesFilter(data, opts) {
			continue
		}

		// Build summary
		summary := v.buildSummary(path, data)
		summaries = append(summaries, summary)
	}

	return summaries
}

// buildSummary creates a DiagnosticsSummary from path and data.
//
// # Description
//
// Extracts summary information from loaded diagnostic data.
//
// # Inputs
//
//   - path: File path
//   - data: Parsed diagnostic data
//
// # Outputs
//
//   - DiagnosticsSummary: Summary object
//
// # Examples
//
//	summary := v.buildSummary("/path/to/diag.json", data)
//
// # Limitations
//
//   - File size requires stat call
//
// # Assumptions
//
//   - Data is valid
func (v *FileDiagnosticsViewer) buildSummary(path string, data *DiagnosticsData) DiagnosticsSummary {
	var sizeBytes int64
	if info, err := os.Stat(path); err == nil {
		sizeBytes = info.Size()
	}

	return DiagnosticsSummary{
		ID:          filepath.Base(path),
		TraceID:     data.Header.TraceID,
		TimestampMs: data.Header.TimestampMs,
		Reason:      data.Header.Reason,
		Severity:    data.Header.Severity,
		Location:    path,
		SizeBytes:   sizeBytes,
	}
}

// matchesFilter checks if diagnostic matches filter options.
//
// # Description
//
// Applies all filter criteria from ListOptions.
//
// # Inputs
//
//   - data: Diagnostic to check
//   - opts: Filter options
//
// # Outputs
//
//   - bool: True if matches all filters
//
// # Examples
//
//	if v.matchesFilter(data, opts) {
//	    // Include in results
//	}
//
// # Limitations
//
//   - All filters are AND'd together
//
// # Assumptions
//
//   - Data header fields are populated
func (v *FileDiagnosticsViewer) matchesFilter(data *DiagnosticsData, opts ListOptions) bool {
	// Severity filter
	if opts.Severity != "" && data.Header.Severity != opts.Severity {
		return false
	}

	// Reason filter
	if opts.Reason != "" && data.Header.Reason != opts.Reason {
		return false
	}

	// Since filter
	if opts.Since > 0 && data.Header.TimestampMs < opts.Since {
		return false
	}

	// Until filter
	if opts.Until > 0 && data.Header.TimestampMs > opts.Until {
		return false
	}

	return true
}

// sortSummariesByTimestamp sorts summaries by timestamp descending.
//
// # Description
//
// In-place sort of summaries slice, newest first.
//
// # Inputs
//
//   - summaries: Slice to sort (modified in place)
//
// # Examples
//
//	v.sortSummariesByTimestamp(summaries)
//	// summaries[0] is now the newest
//
// # Limitations
//
//   - Modifies input slice
//
// # Assumptions
//
//   - TimestampMs is populated in all summaries
func (v *FileDiagnosticsViewer) sortSummariesByTimestamp(summaries []DiagnosticsSummary) {
	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].TimestampMs > summaries[j].TimestampMs
	})
}

// applyPagination applies offset and limit to summaries.
//
// # Description
//
// Returns a slice of summaries with pagination applied.
//
// # Inputs
//
//   - summaries: Full result set
//   - opts: Pagination options
//
// # Outputs
//
//   - []DiagnosticsSummary: Paginated results
//
// # Examples
//
//	paginated := v.applyPagination(summaries, ListOptions{Offset: 10, Limit: 5})
//
// # Limitations
//
//   - Returns empty slice if offset exceeds length
//
// # Assumptions
//
//   - Opts has been through WithDefaults()
func (v *FileDiagnosticsViewer) applyPagination(summaries []DiagnosticsSummary, opts ListOptions) []DiagnosticsSummary {
	// Apply offset
	if opts.Offset > 0 {
		if opts.Offset >= len(summaries) {
			return []DiagnosticsSummary{}
		}
		summaries = summaries[opts.Offset:]
	}

	// Apply limit
	if opts.Limit > 0 && len(summaries) > opts.Limit {
		summaries = summaries[:opts.Limit]
	}

	return summaries
}

// -----------------------------------------------------------------------------
// Private Methods - Cache Management
// -----------------------------------------------------------------------------

// getCached retrieves a diagnostic from cache.
//
// # Description
//
// Thread-safe cache lookup.
//
// # Inputs
//
//   - path: File path as cache key
//
// # Outputs
//
//   - *DiagnosticsData: Cached data, or nil if not cached
//
// # Examples
//
//	if data := v.getCached(path); data != nil {
//	    return data, nil
//	}
//
// # Limitations
//
//   - Cache hit rate depends on access patterns
//
// # Assumptions
//
//   - Path is the canonical file path
func (v *FileDiagnosticsViewer) getCached(path string) *DiagnosticsData {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.cache[path]
}

// setCached stores a diagnostic in cache.
//
// # Description
//
// Thread-safe cache insertion with automatic size limiting.
// Clears cache when size limit is reached.
//
// # Inputs
//
//   - path: File path as cache key
//   - data: Diagnostic data to cache
//
// # Examples
//
//	v.setCached("/path/to/diag.json", data)
//
// # Limitations
//
//   - Simple cache eviction (clear all when full)
//   - Maximum 100 entries
//
// # Assumptions
//
//   - Data is immutable after caching
func (v *FileDiagnosticsViewer) setCached(path string, data *DiagnosticsData) {
	v.mu.Lock()
	defer v.mu.Unlock()

	const maxCacheSize = 100
	if len(v.cache) >= maxCacheSize {
		v.cache = make(map[string]*DiagnosticsData)
	}

	v.cache[path] = data
}

// ClearCache removes all cached diagnostics.
//
// # Description
//
// Clears the internal cache. Useful after pruning or when memory is tight.
//
// # Examples
//
//	viewer.ClearCache()
//	// Next Get() will read from disk
//
// # Limitations
//
//   - No selective eviction
//
// # Assumptions
//
//   - Caller accepts cache miss on next access
func (v *FileDiagnosticsViewer) ClearCache() {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.cache = make(map[string]*DiagnosticsData)
}

// Compile-time interface compliance check.
var _ DiagnosticsViewer = (*FileDiagnosticsViewer)(nil)
