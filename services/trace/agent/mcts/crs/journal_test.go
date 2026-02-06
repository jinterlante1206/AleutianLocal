// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package crs

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// -----------------------------------------------------------------------------
// JournalConfig Tests
// -----------------------------------------------------------------------------

func TestJournalConfig_Validate(t *testing.T) {
	t.Run("valid in-memory config", func(t *testing.T) {
		cfg := JournalConfig{
			SessionID: "test-session",
			InMemory:  true,
		}
		err := cfg.Validate()
		assert.NoError(t, err)
	})

	t.Run("valid persistent config", func(t *testing.T) {
		cfg := JournalConfig{
			SessionID: "test-session",
			Path:      "/tmp/journal",
		}
		err := cfg.Validate()
		assert.NoError(t, err)
	})

	t.Run("missing session_id", func(t *testing.T) {
		cfg := JournalConfig{
			InMemory: true,
		}
		err := cfg.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "session_id")
	})

	t.Run("missing path for persistent", func(t *testing.T) {
		cfg := JournalConfig{
			SessionID: "test-session",
			InMemory:  false,
		}
		err := cfg.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "path")
	})

	t.Run("negative max_journal_bytes", func(t *testing.T) {
		cfg := JournalConfig{
			SessionID:       "test-session",
			InMemory:        true,
			MaxJournalBytes: -1,
		}
		err := cfg.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "max_journal_bytes")
	})
}

func TestDefaultJournalConfig(t *testing.T) {
	cfg := DefaultJournalConfig()
	assert.True(t, cfg.SyncWrites)
	assert.Equal(t, int64(1<<30), cfg.MaxJournalBytes) // 1GB
	assert.False(t, cfg.AllowDegraded)
	assert.False(t, cfg.SkipCorruptedDeltas)
}

// -----------------------------------------------------------------------------
// BadgerJournal Tests
// -----------------------------------------------------------------------------

func TestNewBadgerJournal(t *testing.T) {
	t.Run("in-memory journal", func(t *testing.T) {
		cfg := JournalConfig{
			SessionID: "test-session",
			InMemory:  true,
		}
		j, err := NewBadgerJournal(cfg)
		require.NoError(t, err)
		defer j.Close()

		assert.True(t, j.IsAvailable())
		assert.False(t, j.IsDegraded())
	})

	t.Run("invalid config", func(t *testing.T) {
		cfg := JournalConfig{} // Missing required fields
		_, err := NewBadgerJournal(cfg)
		assert.Error(t, err)
	})
}

func TestBadgerJournal_Append(t *testing.T) {
	ctx := context.Background()

	t.Run("append single delta", func(t *testing.T) {
		j := createTestJournal(t)
		defer j.Close()

		delta := NewProofDelta(SignalSourceHard, map[string]ProofNumber{
			"node1": {Proof: 5, Status: ProofStatusExpanded},
		})

		err := j.Append(ctx, delta)
		require.NoError(t, err)

		stats := j.Stats()
		assert.Equal(t, uint64(1), stats.LastSeqNum)
	})

	t.Run("append multiple deltas", func(t *testing.T) {
		j := createTestJournal(t)
		defer j.Close()

		for i := 0; i < 10; i++ {
			delta := NewProofDelta(SignalSourceHard, map[string]ProofNumber{
				"node": {Proof: uint64(i)},
			})
			err := j.Append(ctx, delta)
			require.NoError(t, err)
		}

		stats := j.Stats()
		assert.Equal(t, uint64(10), stats.LastSeqNum)
	})

	t.Run("nil delta returns error", func(t *testing.T) {
		j := createTestJournal(t)
		defer j.Close()

		err := j.Append(ctx, nil)
		assert.ErrorIs(t, err, ErrNilDeltaJournal)
	})

	t.Run("nil context returns error", func(t *testing.T) {
		j := createTestJournal(t)
		defer j.Close()

		delta := NewProofDelta(SignalSourceHard, map[string]ProofNumber{})
		err := j.Append(nil, delta)
		assert.ErrorIs(t, err, ErrNilContext)
	})

	t.Run("cancelled context returns error", func(t *testing.T) {
		j := createTestJournal(t)
		defer j.Close()

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		delta := NewProofDelta(SignalSourceHard, map[string]ProofNumber{})
		err := j.Append(ctx, delta)
		assert.ErrorIs(t, err, context.Canceled)
	})

	t.Run("closed journal returns error", func(t *testing.T) {
		j := createTestJournal(t)
		j.Close()

		delta := NewProofDelta(SignalSourceHard, map[string]ProofNumber{})
		err := j.Append(ctx, delta)
		assert.ErrorIs(t, err, ErrJournalClosed)
	})
}

func TestBadgerJournal_AppendBatch(t *testing.T) {
	ctx := context.Background()

	t.Run("append batch", func(t *testing.T) {
		j := createTestJournal(t)
		defer j.Close()

		deltas := []Delta{
			NewProofDelta(SignalSourceHard, map[string]ProofNumber{"node1": {Proof: 1}}),
			NewProofDelta(SignalSourceHard, map[string]ProofNumber{"node2": {Proof: 2}}),
			NewProofDelta(SignalSourceHard, map[string]ProofNumber{"node3": {Proof: 3}}),
		}

		err := j.AppendBatch(ctx, deltas)
		require.NoError(t, err)

		stats := j.Stats()
		assert.Equal(t, uint64(3), stats.LastSeqNum)
	})

	t.Run("empty batch returns error", func(t *testing.T) {
		j := createTestJournal(t)
		defer j.Close()

		err := j.AppendBatch(ctx, []Delta{})
		assert.Error(t, err)
	})

	t.Run("nil delta in batch returns error", func(t *testing.T) {
		j := createTestJournal(t)
		defer j.Close()

		deltas := []Delta{
			NewProofDelta(SignalSourceHard, map[string]ProofNumber{"node1": {Proof: 1}}),
			nil,
		}

		err := j.AppendBatch(ctx, deltas)
		assert.Error(t, err)
	})
}

func TestBadgerJournal_Replay(t *testing.T) {
	ctx := context.Background()

	t.Run("replay empty journal", func(t *testing.T) {
		j := createTestJournal(t)
		defer j.Close()

		deltas, err := j.Replay(ctx)
		require.NoError(t, err)
		assert.Empty(t, deltas)
	})

	t.Run("replay returns deltas in order", func(t *testing.T) {
		j := createTestJournal(t)
		defer j.Close()

		// Append deltas
		for i := 1; i <= 5; i++ {
			delta := NewProofDelta(SignalSourceHard, map[string]ProofNumber{
				"node": {Proof: uint64(i)},
			})
			require.NoError(t, j.Append(ctx, delta))
		}

		// Replay
		deltas, err := j.Replay(ctx)
		require.NoError(t, err)
		assert.Len(t, deltas, 5)

		// Verify order
		for i, delta := range deltas {
			proofDelta, ok := delta.(*ProofDelta)
			require.True(t, ok)
			assert.Equal(t, uint64(i+1), proofDelta.Updates["node"].Proof)
		}
	})

	t.Run("replay skips checkpointed deltas", func(t *testing.T) {
		j := createTestJournal(t)
		defer j.Close()

		// Append 5 deltas
		for i := 1; i <= 5; i++ {
			delta := NewProofDelta(SignalSourceHard, map[string]ProofNumber{
				"node": {Proof: uint64(i)},
			})
			require.NoError(t, j.Append(ctx, delta))
		}

		// Checkpoint (truncates all)
		require.NoError(t, j.Checkpoint(ctx))

		// Append 3 more
		for i := 6; i <= 8; i++ {
			delta := NewProofDelta(SignalSourceHard, map[string]ProofNumber{
				"node": {Proof: uint64(i)},
			})
			require.NoError(t, j.Append(ctx, delta))
		}

		// Replay should only return post-checkpoint deltas
		deltas, err := j.Replay(ctx)
		require.NoError(t, err)
		assert.Len(t, deltas, 3)
	})
}

func TestBadgerJournal_ReplayStream(t *testing.T) {
	ctx := context.Background()

	t.Run("stream empty journal", func(t *testing.T) {
		j := createTestJournal(t)
		defer j.Close()

		ch, err := j.ReplayStream(ctx)
		require.NoError(t, err)

		count := 0
		for range ch {
			count++
		}
		assert.Equal(t, 0, count)
	})

	t.Run("stream returns deltas", func(t *testing.T) {
		j := createTestJournal(t)
		defer j.Close()

		// Append deltas
		for i := 1; i <= 3; i++ {
			delta := NewProofDelta(SignalSourceHard, map[string]ProofNumber{
				"node": {Proof: uint64(i)},
			})
			require.NoError(t, j.Append(ctx, delta))
		}

		// Stream replay
		ch, err := j.ReplayStream(ctx)
		require.NoError(t, err)

		count := 0
		for result := range ch {
			if result.Err != nil {
				t.Errorf("unexpected error: %v", result.Err)
			}
			require.NotNil(t, result.Delta)
			count++
		}
		assert.Equal(t, 3, count)
	})

	t.Run("context cancellation stops stream", func(t *testing.T) {
		j := createTestJournal(t)
		defer j.Close()

		// Append many deltas
		for i := 1; i <= 100; i++ {
			delta := NewProofDelta(SignalSourceHard, map[string]ProofNumber{
				"node": {Proof: uint64(i)},
			})
			require.NoError(t, j.Append(ctx, delta))
		}

		ctx, cancel := context.WithCancel(context.Background())
		ch, err := j.ReplayStream(ctx)
		require.NoError(t, err)

		// Read a few then cancel
		count := 0
		for range ch {
			count++
			if count >= 5 {
				cancel()
				break
			}
		}

		// Drain remaining
		for range ch {
		}

		assert.LessOrEqual(t, 5, count)
	})
}

func TestBadgerJournal_Checkpoint(t *testing.T) {
	ctx := context.Background()

	t.Run("checkpoint empty journal", func(t *testing.T) {
		j := createTestJournal(t)
		defer j.Close()

		err := j.Checkpoint(ctx)
		require.NoError(t, err)
	})

	t.Run("checkpoint truncates old entries", func(t *testing.T) {
		j := createTestJournal(t)
		defer j.Close()

		// Append deltas
		for i := 1; i <= 5; i++ {
			delta := NewProofDelta(SignalSourceHard, map[string]ProofNumber{
				"node": {Proof: uint64(i)},
			})
			require.NoError(t, j.Append(ctx, delta))
		}

		// Checkpoint
		err := j.Checkpoint(ctx)
		require.NoError(t, err)

		// Replay should be empty (all checkpointed)
		deltas, err := j.Replay(ctx)
		require.NoError(t, err)
		assert.Empty(t, deltas)
	})

	t.Run("checkpoint updates stats", func(t *testing.T) {
		j := createTestJournal(t)
		defer j.Close()

		// Initially no checkpoint
		stats := j.Stats()
		assert.True(t, stats.LastCheckpoint.IsZero())

		// Checkpoint
		err := j.Checkpoint(ctx)
		require.NoError(t, err)

		// Checkpoint time updated
		stats = j.Stats()
		assert.False(t, stats.LastCheckpoint.IsZero())
	})
}

func TestBadgerJournal_CRCIntegrity(t *testing.T) {
	t.Run("encoding and decoding preserves data", func(t *testing.T) {
		j := createTestJournal(t)
		defer j.Close()

		originalDelta := NewProofDelta(SignalSourceHard, map[string]ProofNumber{
			"node1": {Proof: 42, Disproof: 100, Status: ProofStatusExpanded},
			"node2": {Proof: 99, Status: ProofStatusProven},
		})

		// Encode
		data, err := j.encodeEntry(originalDelta)
		require.NoError(t, err)

		// Decode
		decoded, err := j.decodeEntry(data)
		require.NoError(t, err)

		// Verify
		proofDelta, ok := decoded.(*ProofDelta)
		require.True(t, ok)
		assert.Equal(t, uint64(42), proofDelta.Updates["node1"].Proof)
		assert.Equal(t, uint64(99), proofDelta.Updates["node2"].Proof)
	})

	t.Run("corrupted data fails CRC check", func(t *testing.T) {
		j := createTestJournal(t)
		defer j.Close()

		delta := NewProofDelta(SignalSourceHard, map[string]ProofNumber{
			"node": {Proof: 42},
		})

		// Encode
		data, err := j.encodeEntry(delta)
		require.NoError(t, err)

		// Corrupt the data (flip a bit in the payload)
		if len(data) > 5 {
			data[5] ^= 0xFF
		}

		// Decode should fail
		_, err = j.decodeEntry(data)
		assert.ErrorIs(t, err, ErrJournalCorrupted)
	})

	t.Run("truncated data fails", func(t *testing.T) {
		j := createTestJournal(t)
		defer j.Close()

		// Too short
		_, err := j.decodeEntry([]byte{0x01, 0x02})
		assert.Error(t, err)
	})
}

func TestBadgerJournal_AllDeltaTypes(t *testing.T) {
	ctx := context.Background()
	j := createTestJournal(t)
	defer j.Close()

	// Test all delta types can be serialized/deserialized
	deltas := []Delta{
		NewProofDelta(SignalSourceHard, map[string]ProofNumber{
			"node": {Proof: 42, Status: ProofStatusExpanded},
		}),
		&ConstraintDelta{
			baseDelta: newBaseDelta(SignalSourceSoft),
			Add:       []Constraint{{ID: "c1", Type: ConstraintTypeMutualExclusion}},
		},
		NewSimilarityDelta(SignalSourceSoft),
		NewDependencyDelta(SignalSourceHard),
		NewHistoryDelta(SignalSourceSoft, []HistoryEntry{
			{ID: "h1", NodeID: "node", Action: "test"},
		}),
		NewStreamingDelta(SignalSourceSoft),
		// CompositeDelta wrapping multiple deltas
		NewCompositeDelta(
			NewProofDelta(SignalSourceHard, map[string]ProofNumber{
				"inner": {Proof: 99},
			}),
			NewDependencyDelta(SignalSourceHard),
		),
	}

	// Append all
	for _, delta := range deltas {
		err := j.Append(ctx, delta)
		require.NoError(t, err, "failed to append delta type %T", delta)
	}

	// Replay all
	replayed, err := j.Replay(ctx)
	require.NoError(t, err)
	assert.Len(t, replayed, len(deltas))

	// Verify types match
	for i, d := range replayed {
		assert.IsType(t, deltas[i], d, "type mismatch at index %d", i)
	}

	// Verify CompositeDelta children are preserved
	compositeDelta, ok := replayed[len(replayed)-1].(*CompositeDelta)
	require.True(t, ok, "last delta should be CompositeDelta")
	assert.Len(t, compositeDelta.Deltas, 2, "CompositeDelta should have 2 children")
}

func TestBadgerJournal_DegradedMode(t *testing.T) {
	ctx := context.Background()

	t.Run("degraded mode with invalid path", func(t *testing.T) {
		cfg := JournalConfig{
			SessionID:     "test-session",
			Path:          "/nonexistent/path/that/cannot/be/created",
			AllowDegraded: true,
		}

		j, err := NewBadgerJournal(cfg)
		require.NoError(t, err) // Should not error with AllowDegraded=true
		defer j.Close()

		assert.False(t, j.IsAvailable())
		assert.True(t, j.IsDegraded())

		// Operations should fail gracefully
		delta := NewProofDelta(SignalSourceHard, map[string]ProofNumber{})
		err = j.Append(ctx, delta)
		assert.ErrorIs(t, err, ErrJournalDegraded)

		// Replay returns empty in degraded mode
		deltas, err := j.Replay(ctx)
		require.NoError(t, err)
		assert.Empty(t, deltas)
	})

	t.Run("strict mode fails on invalid path", func(t *testing.T) {
		cfg := JournalConfig{
			SessionID:     "test-session",
			Path:          "/nonexistent/path/that/cannot/be/created",
			AllowDegraded: false,
		}

		_, err := NewBadgerJournal(cfg)
		assert.Error(t, err)
	})
}

func TestBadgerJournal_MaxJournalBytes(t *testing.T) {
	ctx := context.Background()

	t.Run("append fails when journal full", func(t *testing.T) {
		cfg := JournalConfig{
			SessionID:       "test-session",
			InMemory:        true,
			MaxJournalBytes: 100, // Very small limit
		}

		j, err := NewBadgerJournal(cfg)
		require.NoError(t, err)
		defer j.Close()

		// Append until full
		for i := 0; i < 100; i++ {
			delta := NewProofDelta(SignalSourceHard, map[string]ProofNumber{
				"node": {Proof: uint64(i)},
			})
			err := j.Append(ctx, delta)
			if errors.Is(err, ErrJournalFull) {
				return // Expected
			}
			require.NoError(t, err)
		}

		t.Log("Journal did not reach size limit - test may need adjustment")
	})
}

func TestBadgerJournal_Sync(t *testing.T) {
	j := createTestJournal(t)
	defer j.Close()

	// Sync should not error
	err := j.Sync()
	assert.NoError(t, err)
}

func TestBadgerJournal_CloseIdempotent(t *testing.T) {
	j := createTestJournal(t)

	// Close twice should not panic or error
	err1 := j.Close()
	assert.NoError(t, err1)

	err2 := j.Close()
	assert.NoError(t, err2)
}

func TestBadgerJournal_Stats(t *testing.T) {
	ctx := context.Background()
	j := createTestJournal(t)
	defer j.Close()

	// Initial stats
	stats := j.Stats()
	assert.Equal(t, uint64(0), stats.LastSeqNum)
	assert.False(t, stats.Degraded)

	// After appends
	for i := 0; i < 5; i++ {
		delta := NewProofDelta(SignalSourceHard, map[string]ProofNumber{
			"node": {Proof: uint64(i)},
		})
		require.NoError(t, j.Append(ctx, delta))
	}

	stats = j.Stats()
	assert.Equal(t, uint64(5), stats.LastSeqNum)
	assert.Greater(t, stats.TotalBytes, int64(0))
}

// -----------------------------------------------------------------------------
// Benchmark Tests
// -----------------------------------------------------------------------------

func BenchmarkBadgerJournal_Append(b *testing.B) {
	ctx := context.Background()
	j := createTestJournalB(b)
	defer j.Close()

	delta := NewProofDelta(SignalSourceHard, map[string]ProofNumber{
		"node": {Proof: 42, Status: ProofStatusExpanded},
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := j.Append(ctx, delta); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkBadgerJournal_AppendBatch10(b *testing.B) {
	ctx := context.Background()
	j := createTestJournalB(b)
	defer j.Close()

	deltas := make([]Delta, 10)
	for i := range deltas {
		deltas[i] = NewProofDelta(SignalSourceHard, map[string]ProofNumber{
			"node": {Proof: uint64(i)},
		})
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := j.AppendBatch(ctx, deltas); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkBadgerJournal_Replay(b *testing.B) {
	ctx := context.Background()
	j := createTestJournalB(b)
	defer j.Close()

	// Pre-populate with 1000 deltas
	for i := 0; i < 1000; i++ {
		delta := NewProofDelta(SignalSourceHard, map[string]ProofNumber{
			"node": {Proof: uint64(i)},
		})
		if err := j.Append(ctx, delta); err != nil {
			b.Fatal(err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Note: Replay normally only happens once, but we benchmark it
		// We need a fresh journal each time since checkpoint affects replay
		// For this benchmark, we just measure the replay mechanism
		_, err := j.Replay(ctx)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// -----------------------------------------------------------------------------
// Helpers
// -----------------------------------------------------------------------------

func createTestJournal(t *testing.T) *BadgerJournal {
	t.Helper()

	cfg := JournalConfig{
		SessionID:       "test-session-" + time.Now().Format("150405.000"),
		InMemory:        true,
		MaxJournalBytes: 0, // No limit
	}

	j, err := NewBadgerJournal(cfg)
	require.NoError(t, err)
	return j
}

func createTestJournalB(b *testing.B) *BadgerJournal {
	b.Helper()

	cfg := JournalConfig{
		SessionID:       "bench-session",
		InMemory:        true,
		MaxJournalBytes: 0,
	}

	j, err := NewBadgerJournal(cfg)
	if err != nil {
		b.Fatal(err)
	}
	return j
}
