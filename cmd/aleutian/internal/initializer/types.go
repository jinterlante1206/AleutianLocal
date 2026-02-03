// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package initializer

import (
	"time"
)

// FormatVersion is the current index format version.
// Increment when making breaking changes to storage format.
const FormatVersion = "1.0"

// APIVersion is the JSON output API version.
const APIVersion = "1.0"

// Default configuration values.
const (
	DefaultMaxWorkers    = 8
	DefaultMaxFileSize   = 10 * 1024 * 1024 // 10MB
	DefaultFileTimeout   = 30 * time.Second
	DefaultChannelBuffer = 100
	DefaultProgressBatch = 100
	DefaultMemoryLimitMB = 500
	AleutianDir          = ".aleutian"
	LockFileName         = ".lock"
	ManifestFileName     = "manifest.json"
	IndexFileName        = "index.json"
	ConfigFileName       = "config.yaml"
)

// Config holds initialization configuration.
//
// # Fields
//
//   - ProjectRoot: Absolute path to project root. Must not be empty.
//   - Languages: Languages to parse. Empty means auto-detect.
//   - ExcludePatterns: Glob patterns to exclude.
//   - MaxWorkers: Maximum parallel workers. Must be > 0.
//   - MaxFileSize: Maximum file size in bytes. Files larger are skipped.
//   - FileTimeout: Per-file parse timeout. Zero means no timeout.
//   - Force: If true, rebuild index even if exists.
//   - DryRun: If true, show what would be indexed without writing.
//   - Quiet: If true, suppress progress output.
//   - Verbose: If true, show detailed per-file output.
type Config struct {
	ProjectRoot     string
	Languages       []string
	ExcludePatterns []string
	MaxWorkers      int
	MaxFileSize     int64
	FileTimeout     time.Duration
	Force           bool
	DryRun          bool
	Quiet           bool
	Verbose         bool
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig(projectRoot string) Config {
	return Config{
		ProjectRoot:     projectRoot,
		Languages:       nil, // auto-detect
		ExcludePatterns: []string{"vendor/**", "node_modules/**", ".git/**", "*.min.js"},
		MaxWorkers:      DefaultMaxWorkers,
		MaxFileSize:     DefaultMaxFileSize,
		FileTimeout:     DefaultFileTimeout,
		Force:           false,
		DryRun:          false,
		Quiet:           false,
		Verbose:         false,
	}
}

// Validate checks that the Config has valid field values.
func (c Config) Validate() error {
	if c.ProjectRoot == "" {
		return ErrEmptyProjectRoot
	}
	if c.MaxWorkers <= 0 {
		return ErrInvalidMaxWorkers
	}
	if c.MaxFileSize <= 0 {
		return ErrInvalidMaxFileSize
	}
	return nil
}

// Result holds the initialization result.
//
// # Fields
//
//   - ProjectRoot: The initialized project path.
//   - Languages: Languages detected/used.
//   - FilesIndexed: Number of files successfully indexed.
//   - SymbolsFound: Total symbols extracted.
//   - EdgesBuilt: Call graph edges created.
//   - DurationMs: Total initialization time in milliseconds.
//   - IndexPath: Path to .aleutian/ directory.
//   - Warnings: Non-fatal issues encountered.
//   - Incremental: True if this was an incremental update.
//   - FilesChanged: Number of files changed (for incremental).
type Result struct {
	APIVersion   string   `json:"api_version"`
	ProjectRoot  string   `json:"project_root"`
	Languages    []string `json:"languages"`
	FilesIndexed int      `json:"files_indexed"`
	SymbolsFound int      `json:"symbols_found"`
	EdgesBuilt   int      `json:"edges_built"`
	DurationMs   int64    `json:"duration_ms"`
	IndexPath    string   `json:"index_path"`
	Warnings     []string `json:"warnings,omitempty"`
	Incremental  bool     `json:"incremental"`
	FilesChanged int      `json:"files_changed,omitempty"`
}

// NewResult creates a new Result with the API version set.
func NewResult() *Result {
	return &Result{
		APIVersion: APIVersion,
		Languages:  make([]string, 0),
		Warnings:   make([]string, 0),
	}
}

// ProjectConfig holds project-specific settings stored in config.yaml.
type ProjectConfig struct {
	FormatVersion   string   `yaml:"format_version" json:"format_version"`
	Languages       []string `yaml:"languages" json:"languages"`
	ExcludePatterns []string `yaml:"exclude_patterns" json:"exclude_patterns"`
	CreatedAt       string   `yaml:"created_at" json:"created_at"`
	UpdatedAt       string   `yaml:"updated_at" json:"updated_at"`
}

// ManifestFile holds the file manifest with checksums.
type ManifestFile struct {
	FormatVersion  string               `json:"format_version"`
	ProjectRoot    string               `json:"project_root"`
	Files          map[string]FileEntry `json:"files"`
	IndexChecksum  string               `json:"index_checksum"`
	GraphChecksum  string               `json:"graph_checksum"`
	CreatedAtMilli int64                `json:"created_at_milli"`
	UpdatedAtMilli int64                `json:"updated_at_milli"`
}

// FileEntry represents a single file in the manifest.
type FileEntry struct {
	Path  string `json:"path"`
	Hash  string `json:"hash"`
	Mtime int64  `json:"mtime"`
	Size  int64  `json:"size"`
}

// ParseJob represents a file to be parsed by a worker.
type ParseJob struct {
	FilePath string
	Language string
	Content  []byte
}

// ParseResult holds the result of parsing a single file.
type ParseResult struct {
	FilePath string
	Symbols  []Symbol
	Edges    []Edge
	Error    error
}

// Symbol represents a code symbol (function, type, etc.).
type Symbol struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	Kind      string            `json:"kind"`
	FilePath  string            `json:"file_path"`
	StartLine int               `json:"start_line"`
	EndLine   int               `json:"end_line"`
	StartCol  int               `json:"start_col"`
	EndCol    int               `json:"end_col"`
	Signature string            `json:"signature,omitempty"`
	Parent    string            `json:"parent,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

// Edge represents a relationship between symbols.
type Edge struct {
	FromID   string `json:"from_id"`
	ToID     string `json:"to_id"`
	Kind     string `json:"kind"` // calls, imports, implements, etc.
	FilePath string `json:"file_path"`
	Line     int    `json:"line"`
}

// Progress represents initialization progress.
type Progress struct {
	Phase        string
	FilesTotal   int
	FilesScanned int
	FilesCurrent string
	Percent      int
}

// ProgressCallback is called during initialization with progress updates.
type ProgressCallback func(Progress)
