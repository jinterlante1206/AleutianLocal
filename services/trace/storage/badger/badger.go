// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

// Package badger provides factory functions and configuration for BadgerDB.
//
// BadgerDB is used for local embedded storage with low-latency access (~100µs).
// This is part of the tiered persistence model:
//
//	Hot (RAM) → Warm (BadgerDB) → Cold (Weaviate)
//
// Use cases:
//   - CRS Journal (WAL for crash recovery)
//   - Session state persistence
//   - Local caching
//
// License: BadgerDB is Apache 2.0 licensed (github.com/dgraph-io/badger).
// This package follows Apache 2.0 guidelines for attribution and usage.
package badger

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/dgraph-io/badger/v4"
)

// Config holds configuration for a BadgerDB instance.
type Config struct {
	// Path is the directory for BadgerDB files.
	// Required for persistent databases.
	// Ignored when InMemory is true.
	Path string

	// InMemory enables in-memory mode (no disk persistence).
	// Useful for testing.
	InMemory bool

	// SyncWrites enables synchronous writes for durability.
	// Default: true for production, false for testing.
	SyncWrites bool

	// Logger is the logger for BadgerDB operations.
	// If nil, BadgerDB's internal logging is disabled.
	Logger *slog.Logger

	// NumVersionsToKeep is the number of versions to keep per key.
	// Default: 1 (we don't use multi-version concurrency control).
	NumVersionsToKeep int

	// GCInterval is how often to run value log garbage collection.
	// Default: 5 minutes. Set to 0 to disable.
	GCInterval time.Duration

	// GCDiscardRatio is the minimum ratio of discardable data before GC.
	// Default: 0.5 (GC when 50% of value log is garbage).
	GCDiscardRatio float64
}

// DefaultConfig returns sensible defaults for production use.
//
// Description:
//
//	Returns a Config with:
//	- SyncWrites enabled for durability
//	- Single version retention
//	- 5-minute GC interval
//	- 50% discard ratio threshold
//
// Outputs:
//
//	Config - Ready-to-use production configuration
func DefaultConfig() Config {
	return Config{
		SyncWrites:        true,
		NumVersionsToKeep: 1,
		GCInterval:        5 * time.Minute,
		GCDiscardRatio:    0.5,
	}
}

// InMemoryConfig returns configuration optimized for testing.
//
// Description:
//
//	Returns a Config with:
//	- InMemory mode enabled (no disk I/O)
//	- SyncWrites disabled (faster tests)
//	- GC disabled
//
// Outputs:
//
//	Config - Ready-to-use test configuration
func InMemoryConfig() Config {
	return Config{
		InMemory:          true,
		SyncWrites:        false,
		NumVersionsToKeep: 1,
		GCInterval:        0, // disabled
	}
}

// badgerLogger adapts slog.Logger to BadgerDB's Logger interface.
type badgerLogger struct {
	logger *slog.Logger
}

func (l *badgerLogger) Errorf(format string, args ...interface{}) {
	l.logger.Error(fmt.Sprintf(format, args...))
}

func (l *badgerLogger) Warningf(format string, args ...interface{}) {
	l.logger.Warn(fmt.Sprintf(format, args...))
}

func (l *badgerLogger) Infof(format string, args ...interface{}) {
	l.logger.Info(fmt.Sprintf(format, args...))
}

func (l *badgerLogger) Debugf(format string, args ...interface{}) {
	l.logger.Debug(fmt.Sprintf(format, args...))
}

// Open creates and opens a BadgerDB instance with the given configuration.
//
// Description:
//
//	Opens a BadgerDB database at the configured path, or in memory if
//	InMemory is true. Creates the directory if it doesn't exist.
//
// Inputs:
//
//	cfg - Database configuration. Path is required unless InMemory is true.
//
// Outputs:
//
//	*badger.DB - The opened database. Caller must call Close() when done.
//	error - Non-nil if path is invalid or database cannot be opened.
//
// Thread Safety: The returned *badger.DB is safe for concurrent use.
func Open(cfg Config) (*badger.DB, error) {
	if !cfg.InMemory && cfg.Path == "" {
		return nil, errors.New("path is required for persistent database")
	}

	var opts badger.Options
	if cfg.InMemory {
		opts = badger.DefaultOptions("").WithInMemory(true)
	} else {
		// Ensure directory exists
		if err := os.MkdirAll(cfg.Path, 0750); err != nil {
			return nil, fmt.Errorf("create database directory %s: %w", cfg.Path, err)
		}
		opts = badger.DefaultOptions(cfg.Path)
	}

	// Apply configuration
	opts = opts.WithSyncWrites(cfg.SyncWrites)
	opts = opts.WithNumVersionsToKeep(cfg.NumVersionsToKeep)

	// Configure logging
	if cfg.Logger != nil {
		opts = opts.WithLogger(&badgerLogger{logger: cfg.Logger})
	} else {
		opts = opts.WithLogger(nil) // Disable BadgerDB's internal logging
	}

	db, err := badger.Open(opts)
	if err != nil {
		return nil, fmt.Errorf("open badger database: %w", err)
	}

	return db, nil
}

// OpenWithPath is a convenience function for opening a database at a path.
//
// Description:
//
//	Opens a persistent BadgerDB with production defaults at the given path.
//
// Inputs:
//
//	path - Directory for database files. Created if it doesn't exist.
//
// Outputs:
//
//	*badger.DB - The opened database. Caller must call Close() when done.
//	error - Non-nil if path is invalid or database cannot be opened.
//
// Thread Safety: The returned *badger.DB is safe for concurrent use.
func OpenWithPath(path string) (*badger.DB, error) {
	cfg := DefaultConfig()
	cfg.Path = path
	return Open(cfg)
}

// OpenInMemory is a convenience function for opening an in-memory database.
//
// Description:
//
//	Opens an in-memory BadgerDB for testing. Data is lost when closed.
//
// Outputs:
//
//	*badger.DB - The opened database. Caller must call Close() when done.
//	error - Non-nil if database cannot be opened (unlikely for in-memory).
//
// Thread Safety: The returned *badger.DB is safe for concurrent use.
func OpenInMemory() (*badger.DB, error) {
	return Open(InMemoryConfig())
}

// GCRunner runs periodic garbage collection on a BadgerDB instance.
type GCRunner struct {
	db       *badger.DB
	interval time.Duration
	ratio    float64
	stopCh   chan struct{}
	doneCh   chan struct{}
	logger   *slog.Logger
}

// NewGCRunner creates a garbage collection runner.
//
// Description:
//
//	Creates a runner that periodically triggers BadgerDB value log GC.
//	Call Start() to begin GC and Stop() to halt it.
//
// Inputs:
//
//	db - The BadgerDB instance. Must not be nil.
//	interval - How often to run GC. Must be positive.
//	ratio - Minimum garbage ratio to trigger GC (0.0-1.0).
//	logger - Optional logger for GC events.
//
// Outputs:
//
//	*GCRunner - The runner. Not started until Start() is called.
//	error - Non-nil if inputs are invalid.
//
// Thread Safety: Safe for concurrent use after creation.
func NewGCRunner(db *badger.DB, interval time.Duration, ratio float64, logger *slog.Logger) (*GCRunner, error) {
	if db == nil {
		return nil, errors.New("db must not be nil")
	}
	if interval <= 0 {
		return nil, errors.New("interval must be positive")
	}
	if ratio < 0 || ratio > 1 {
		return nil, errors.New("ratio must be between 0 and 1")
	}

	return &GCRunner{
		db:       db,
		interval: interval,
		ratio:    ratio,
		stopCh:   make(chan struct{}),
		doneCh:   make(chan struct{}),
		logger:   logger,
	}, nil
}

// Start begins periodic garbage collection.
//
// Description:
//
//	Starts a goroutine that runs GC at the configured interval.
//	Safe to call multiple times; subsequent calls are no-ops.
//
// Thread Safety: Safe for concurrent use.
func (r *GCRunner) Start() {
	go r.run()
}

// Stop halts garbage collection.
//
// Description:
//
//	Signals the GC goroutine to stop and waits for it to finish.
//	Safe to call multiple times; subsequent calls are no-ops.
//
// Thread Safety: Safe for concurrent use.
func (r *GCRunner) Stop() {
	close(r.stopCh)
	<-r.doneCh
}

func (r *GCRunner) run() {
	defer close(r.doneCh)

	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()

	for {
		select {
		case <-r.stopCh:
			return
		case <-ticker.C:
			r.runGC()
		}
	}
}

func (r *GCRunner) runGC() {
	// RunValueLogGC returns nil if GC was triggered, error if not needed
	err := r.db.RunValueLogGC(r.ratio)
	if err == nil {
		if r.logger != nil {
			r.logger.Debug("badger value log GC completed")
		}
	} else if !errors.Is(err, badger.ErrNoRewrite) {
		// ErrNoRewrite means no GC was needed, not an error
		if r.logger != nil {
			r.logger.Warn("badger value log GC error", slog.String("error", err.Error()))
		}
	}
}

// DB wraps a BadgerDB instance with lifecycle management.
type DB struct {
	*badger.DB
	gcRunner *GCRunner
	path     string
	inMemory bool
}

// OpenDB opens a BadgerDB with full lifecycle management.
//
// Description:
//
//	Opens a BadgerDB with the given configuration and optionally
//	starts a GC runner if GCInterval is configured.
//
// Inputs:
//
//	cfg - Database configuration.
//
// Outputs:
//
//	*DB - The managed database. Call Close() when done.
//	error - Non-nil if database cannot be opened.
//
// Thread Safety: Safe for concurrent use.
func OpenDB(cfg Config) (*DB, error) {
	db, err := Open(cfg)
	if err != nil {
		return nil, err
	}

	wrapped := &DB{
		DB:       db,
		path:     cfg.Path,
		inMemory: cfg.InMemory,
	}

	// Start GC runner if configured
	if cfg.GCInterval > 0 && !cfg.InMemory {
		runner, err := NewGCRunner(db, cfg.GCInterval, cfg.GCDiscardRatio, cfg.Logger)
		if err != nil {
			db.Close()
			return nil, fmt.Errorf("create GC runner: %w", err)
		}
		wrapped.gcRunner = runner
		runner.Start()
	}

	return wrapped, nil
}

// Close closes the database and stops the GC runner.
//
// Description:
//
//	Stops garbage collection (if running) and closes the database.
//	Safe to call multiple times.
//
// Outputs:
//
//	error - Non-nil if database close fails.
//
// Thread Safety: Safe for concurrent use.
func (d *DB) Close() error {
	if d.gcRunner != nil {
		d.gcRunner.Stop()
	}
	return d.DB.Close()
}

// Path returns the database path, or empty string for in-memory databases.
func (d *DB) Path() string {
	return d.path
}

// InMemory returns true if this is an in-memory database.
func (d *DB) InMemory() bool {
	return d.inMemory
}

// Sync flushes pending writes to disk.
//
// Description:
//
//	For in-memory databases, this is a no-op.
//	For persistent databases, forces a sync to disk.
//
// Outputs:
//
//	error - Non-nil if sync fails.
//
// Thread Safety: Safe for concurrent use.
func (d *DB) Sync() error {
	if d.inMemory {
		return nil // No-op for in-memory
	}
	return d.DB.Sync()
}

// WithTxn executes a function within a read-write transaction.
//
// Description:
//
//	Opens a read-write transaction, executes the function, and commits
//	if the function returns nil. Rolls back on error or panic.
//
// Inputs:
//
//	ctx - Context for cancellation (used for deadline checks).
//	fn - Function to execute within the transaction.
//
// Outputs:
//
//	error - Non-nil if transaction fails or function returns error.
//
// Thread Safety: Safe for concurrent use.
func (d *DB) WithTxn(ctx context.Context, fn func(txn *badger.Txn) error) error {
	// Check context before starting
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("context cancelled: %w", err)
	}

	txn := d.DB.NewTransaction(true)
	defer txn.Discard()

	if err := fn(txn); err != nil {
		return err
	}

	return txn.Commit()
}

// WithReadTxn executes a function within a read-only transaction.
//
// Description:
//
//	Opens a read-only transaction and executes the function.
//
// Inputs:
//
//	ctx - Context for cancellation (used for deadline checks).
//	fn - Function to execute within the transaction.
//
// Outputs:
//
//	error - Non-nil if function returns error.
//
// Thread Safety: Safe for concurrent use.
func (d *DB) WithReadTxn(ctx context.Context, fn func(txn *badger.Txn) error) error {
	// Check context before starting
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("context cancelled: %w", err)
	}

	txn := d.DB.NewTransaction(false)
	defer txn.Discard()

	return fn(txn)
}

// TempDir creates a temporary directory for testing databases.
//
// Description:
//
//	Creates a temporary directory that will be cleaned up when the
//	test completes. Useful for testing persistent database configurations.
//
// Inputs:
//
//	prefix - Prefix for the directory name.
//
// Outputs:
//
//	string - Path to the temporary directory.
//	error - Non-nil if directory cannot be created.
func TempDir(prefix string) (string, error) {
	dir, err := os.MkdirTemp("", prefix)
	if err != nil {
		return "", fmt.Errorf("create temp dir: %w", err)
	}
	return dir, nil
}

// CleanupDir removes a database directory and all its contents.
//
// Description:
//
//	Removes the directory and all files within it.
//	Safe to call with empty string (no-op).
//
// Inputs:
//
//	path - Directory to remove. Empty string is a no-op.
//
// Outputs:
//
//	error - Non-nil if removal fails.
func CleanupDir(path string) error {
	if path == "" {
		return nil
	}
	// Resolve to absolute path to avoid accidental removal of important dirs
	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("resolve path: %w", err)
	}
	return os.RemoveAll(absPath)
}
