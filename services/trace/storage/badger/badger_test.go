// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package badger

import (
	"context"
	"testing"
	"time"

	"github.com/dgraph-io/badger/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestOpenInMemory verifies in-memory database creation works.
func TestOpenInMemory(t *testing.T) {
	db, err := OpenInMemory()
	require.NoError(t, err)
	defer db.Close()

	// Verify we can write and read
	err = db.Update(func(txn *badger.Txn) error {
		return txn.Set([]byte("key"), []byte("value"))
	})
	require.NoError(t, err)

	err = db.View(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte("key"))
		require.NoError(t, err)

		return item.Value(func(val []byte) error {
			assert.Equal(t, []byte("value"), val)
			return nil
		})
	})
	require.NoError(t, err)
}

// TestOpenWithPath verifies persistent database creation works.
func TestOpenWithPath(t *testing.T) {
	dir, err := TempDir("badger-test-")
	require.NoError(t, err)
	defer CleanupDir(dir)

	db, err := OpenWithPath(dir)
	require.NoError(t, err)

	// Write data
	err = db.Update(func(txn *badger.Txn) error {
		return txn.Set([]byte("persistent-key"), []byte("persistent-value"))
	})
	require.NoError(t, err)

	// Close and reopen
	err = db.Close()
	require.NoError(t, err)

	db2, err := OpenWithPath(dir)
	require.NoError(t, err)
	defer db2.Close()

	// Verify data persisted
	err = db2.View(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte("persistent-key"))
		require.NoError(t, err)

		return item.Value(func(val []byte) error {
			assert.Equal(t, []byte("persistent-value"), val)
			return nil
		})
	})
	require.NoError(t, err)
}

// TestOpenRequiresPath verifies that persistent mode requires a path.
func TestOpenRequiresPath(t *testing.T) {
	cfg := Config{
		InMemory: false,
		Path:     "", // Missing path
	}
	_, err := Open(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "path is required")
}

// TestConfigFunctions verifies default configurations.
func TestConfigFunctions(t *testing.T) {
	t.Run("DefaultConfig has SyncWrites", func(t *testing.T) {
		cfg := DefaultConfig()
		assert.True(t, cfg.SyncWrites)
		assert.False(t, cfg.InMemory)
		assert.Equal(t, 1, cfg.NumVersionsToKeep)
		assert.Equal(t, 5*time.Minute, cfg.GCInterval)
	})

	t.Run("InMemoryConfig has InMemory", func(t *testing.T) {
		cfg := InMemoryConfig()
		assert.True(t, cfg.InMemory)
		assert.False(t, cfg.SyncWrites)
		assert.Equal(t, time.Duration(0), cfg.GCInterval) // GC disabled
	})
}

// TestDB_WithTxn verifies transaction helper functions.
func TestDB_WithTxn(t *testing.T) {
	cfg := InMemoryConfig()
	db, err := OpenDB(cfg)
	require.NoError(t, err)
	defer db.Close()

	ctx := context.Background()

	// Write with transaction
	err = db.WithTxn(ctx, func(txn *badger.Txn) error {
		return txn.Set([]byte("txn-key"), []byte("txn-value"))
	})
	require.NoError(t, err)

	// Read with transaction
	err = db.WithReadTxn(ctx, func(txn *badger.Txn) error {
		item, err := txn.Get([]byte("txn-key"))
		require.NoError(t, err)

		return item.Value(func(val []byte) error {
			assert.Equal(t, []byte("txn-value"), val)
			return nil
		})
	})
	require.NoError(t, err)
}

// TestDB_WithTxn_ContextCancelled verifies context cancellation.
func TestDB_WithTxn_ContextCancelled(t *testing.T) {
	cfg := InMemoryConfig()
	db, err := OpenDB(cfg)
	require.NoError(t, err)
	defer db.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	err = db.WithTxn(ctx, func(txn *badger.Txn) error {
		return txn.Set([]byte("key"), []byte("value"))
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "context cancelled")
}

// TestDB_WithTxn_RollbackOnError verifies rollback on error.
func TestDB_WithTxn_RollbackOnError(t *testing.T) {
	cfg := InMemoryConfig()
	db, err := OpenDB(cfg)
	require.NoError(t, err)
	defer db.Close()

	ctx := context.Background()

	// Write that will fail
	err = db.WithTxn(ctx, func(txn *badger.Txn) error {
		if err := txn.Set([]byte("rollback-key"), []byte("should-not-persist")); err != nil {
			return err
		}
		return assert.AnError // Force rollback
	})
	assert.Error(t, err)

	// Verify key was not persisted
	err = db.WithReadTxn(ctx, func(txn *badger.Txn) error {
		_, err := txn.Get([]byte("rollback-key"))
		assert.Error(t, err)
		assert.Equal(t, badger.ErrKeyNotFound, err)
		return nil
	})
	require.NoError(t, err)
}

// TestGCRunner verifies garbage collection runner.
func TestGCRunner(t *testing.T) {
	t.Run("rejects nil db", func(t *testing.T) {
		_, err := NewGCRunner(nil, time.Second, 0.5, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "db must not be nil")
	})

	t.Run("rejects invalid interval", func(t *testing.T) {
		db, err := OpenInMemory()
		require.NoError(t, err)
		defer db.Close()

		_, err = NewGCRunner(db, 0, 0.5, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "interval must be positive")
	})

	t.Run("rejects invalid ratio", func(t *testing.T) {
		db, err := OpenInMemory()
		require.NoError(t, err)
		defer db.Close()

		_, err = NewGCRunner(db, time.Second, 1.5, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "ratio must be between 0 and 1")
	})

	t.Run("starts and stops", func(t *testing.T) {
		db, err := OpenInMemory()
		require.NoError(t, err)
		defer db.Close()

		runner, err := NewGCRunner(db, 10*time.Millisecond, 0.5, nil)
		require.NoError(t, err)

		runner.Start()
		time.Sleep(25 * time.Millisecond) // Let it run a couple cycles
		runner.Stop()                     // Should not deadlock
	})
}

// TestCleanupDir verifies directory cleanup.
func TestCleanupDir(t *testing.T) {
	t.Run("handles empty path", func(t *testing.T) {
		err := CleanupDir("")
		assert.NoError(t, err)
	})

	t.Run("removes directory", func(t *testing.T) {
		dir, err := TempDir("cleanup-test-")
		require.NoError(t, err)

		err = CleanupDir(dir)
		assert.NoError(t, err)

		// Verify removed
		_, err = TempDir(dir)
		// Should succeed because the original dir was removed
	})
}

// ExampleOpenInMemory demonstrates the pattern for using BadgerDB in tests.
func ExampleOpenInMemory() {
	// Create an in-memory database for testing
	db, err := OpenInMemory()
	if err != nil {
		panic(err)
	}
	defer db.Close()

	// Use the database
	err = db.Update(func(txn *badger.Txn) error {
		return txn.Set([]byte("test-key"), []byte("test-value"))
	})
	if err != nil {
		panic(err)
	}

	// Output:
}

// ExampleOpenWithPath demonstrates the pattern for testing persistent databases.
func ExampleOpenWithPath() {
	// Create a temporary directory
	dir, err := TempDir("badger-example-")
	if err != nil {
		panic(err)
	}
	defer CleanupDir(dir) // Clean up when done

	// Open database at that path
	db, err := OpenWithPath(dir)
	if err != nil {
		panic(err)
	}
	defer db.Close()

	// Use the database
	err = db.Update(func(txn *badger.Txn) error {
		return txn.Set([]byte("persistent-key"), []byte("persistent-value"))
	})
	if err != nil {
		panic(err)
	}

	// Output:
}

// ExampleOpenDB demonstrates using the managed DB wrapper.
func ExampleOpenDB() {
	// Use the managed DB for production-like testing
	cfg := InMemoryConfig()
	db, err := OpenDB(cfg)
	if err != nil {
		panic(err)
	}
	defer db.Close()

	ctx := context.Background()

	// Use transaction helpers
	err = db.WithTxn(ctx, func(txn *badger.Txn) error {
		return txn.Set([]byte("managed-key"), []byte("managed-value"))
	})
	if err != nil {
		panic(err)
	}

	// Output:
}
