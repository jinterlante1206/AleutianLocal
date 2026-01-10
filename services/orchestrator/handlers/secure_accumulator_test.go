// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package handlers

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Test: TokenAccumulator Interface
// =============================================================================

// TestTokenAccumulator_Write_SingleToken verifies basic token writing.
//
// # Description
//
// Tests that a single token is written correctly and can be retrieved.
func TestTokenAccumulator_Write_SingleToken(t *testing.T) {
	acc := newTestAccumulator(t)
	defer acc.Destroy()

	token := "Hello"
	err := acc.Write(token)
	require.NoError(t, err, "Write should succeed")

	answer, _, err := acc.Finalize()
	require.NoError(t, err, "Finalize should succeed")
	assert.Equal(t, token, answer, "Answer should match written token")
}

// TestTokenAccumulator_Write_MultipleTokens verifies sequential token writing.
//
// # Description
//
// Tests that multiple tokens are accumulated in sequence correctly.
func TestTokenAccumulator_Write_MultipleTokens(t *testing.T) {
	acc := newTestAccumulator(t)
	defer acc.Destroy()

	tokens := []string{"Hello", " ", "world", "!"}
	expected := "Hello world!"

	for _, token := range tokens {
		err := acc.Write(token)
		require.NoError(t, err, "Write should succeed for token: %q", token)
	}

	answer, _, err := acc.Finalize()
	require.NoError(t, err, "Finalize should succeed")
	assert.Equal(t, expected, answer, "Answer should concatenate all tokens")
}

// TestTokenAccumulator_Write_EmptyToken verifies empty token handling.
//
// # Description
//
// Tests that empty tokens are handled correctly (they should be allowed).
func TestTokenAccumulator_Write_EmptyToken(t *testing.T) {
	acc := newTestAccumulator(t)
	defer acc.Destroy()

	err := acc.Write("")
	require.NoError(t, err, "Empty token write should succeed")

	err = acc.Write("Hello")
	require.NoError(t, err, "Write after empty should succeed")

	answer, _, err := acc.Finalize()
	require.NoError(t, err, "Finalize should succeed")
	assert.Equal(t, "Hello", answer, "Answer should only contain non-empty token")
}

// TestTokenAccumulator_Write_UnicodeTokens verifies UTF-8 handling.
//
// # Description
//
// Tests that Unicode characters are accumulated correctly.
func TestTokenAccumulator_Write_UnicodeTokens(t *testing.T) {
	acc := newTestAccumulator(t)
	defer acc.Destroy()

	tokens := []string{"こんにちは", " ", "世界", "! 🌍"}
	expected := "こんにちは 世界! 🌍"

	for _, token := range tokens {
		err := acc.Write(token)
		require.NoError(t, err, "Write should succeed for unicode token")
	}

	answer, _, err := acc.Finalize()
	require.NoError(t, err, "Finalize should succeed")
	assert.Equal(t, expected, answer, "Answer should preserve Unicode")
}

// TestTokenAccumulator_Write_AfterDestroy verifies destroyed state.
//
// # Description
//
// Tests that writing to a destroyed accumulator returns an error.
func TestTokenAccumulator_Write_AfterDestroy(t *testing.T) {
	acc := newTestAccumulator(t)
	acc.Destroy()

	err := acc.Write("Hello")
	assert.Error(t, err, "Write after Destroy should fail")
	assert.Contains(t, err.Error(), "destroyed", "Error should mention destroyed state")
}

// TestTokenAccumulator_Write_AfterFinalize verifies finalized state.
//
// # Description
//
// Tests that writing to a finalized accumulator returns an error.
func TestTokenAccumulator_Write_AfterFinalize(t *testing.T) {
	acc := newTestAccumulator(t)
	_, _, err := acc.Finalize()
	require.NoError(t, err, "Finalize should succeed")

	err = acc.Write("Hello")
	assert.Error(t, err, "Write after Finalize should fail")
	assert.Contains(t, err.Error(), "destroyed", "Error should mention destroyed state")
}

// =============================================================================
// Test: Finalize
// =============================================================================

// TestTokenAccumulator_Finalize_ReturnsCorrectHash verifies hash computation.
//
// # Description
//
// Tests that Finalize returns the correct SHA-256 hash of accumulated tokens.
func TestTokenAccumulator_Finalize_ReturnsCorrectHash(t *testing.T) {
	acc := newTestAccumulator(t)
	defer acc.Destroy()

	content := "Hello, World!"
	err := acc.Write(content)
	require.NoError(t, err, "Write should succeed")

	answer, hash, err := acc.Finalize()
	require.NoError(t, err, "Finalize should succeed")
	assert.Equal(t, content, answer, "Answer should match input")

	// Verify hash manually
	expectedHash := sha256.Sum256([]byte(content))
	expectedHashStr := hex.EncodeToString(expectedHash[:])
	assert.Equal(t, expectedHashStr, hash, "Hash should match SHA-256 of content")
}

// TestTokenAccumulator_Finalize_IncrementalHashMatchesFinalHash verifies hash consistency.
//
// # Description
//
// Tests that incrementally hashing tokens produces the same result as hashing the final string.
func TestTokenAccumulator_Finalize_IncrementalHashMatchesFinalHash(t *testing.T) {
	acc := newTestAccumulator(t)
	defer acc.Destroy()

	tokens := []string{"The ", "quick ", "brown ", "fox ", "jumps."}
	fullContent := "The quick brown fox jumps."

	for _, token := range tokens {
		err := acc.Write(token)
		require.NoError(t, err, "Write should succeed")
	}

	_, hash, err := acc.Finalize()
	require.NoError(t, err, "Finalize should succeed")

	// Compute expected hash from full content
	expectedHash := sha256.Sum256([]byte(fullContent))
	expectedHashStr := hex.EncodeToString(expectedHash[:])

	assert.Equal(t, expectedHashStr, hash, "Incremental hash should match full content hash")
}

// TestTokenAccumulator_Finalize_HashIs64Characters verifies hash format.
//
// # Description
//
// Tests that the returned hash is a valid 64-character hex string.
func TestTokenAccumulator_Finalize_HashIs64Characters(t *testing.T) {
	acc := newTestAccumulator(t)
	defer acc.Destroy()

	err := acc.Write("test")
	require.NoError(t, err, "Write should succeed")

	_, hash, err := acc.Finalize()
	require.NoError(t, err, "Finalize should succeed")

	assert.Len(t, hash, 64, "SHA-256 hex hash should be 64 characters")

	// Verify it's valid hex
	_, err = hex.DecodeString(hash)
	assert.NoError(t, err, "Hash should be valid hex string")
}

// TestTokenAccumulator_Finalize_EmptyContent verifies empty accumulator handling.
//
// # Description
//
// Tests that finalizing an empty accumulator returns empty string with correct hash.
func TestTokenAccumulator_Finalize_EmptyContent(t *testing.T) {
	acc := newTestAccumulator(t)
	defer acc.Destroy()

	answer, hash, err := acc.Finalize()
	require.NoError(t, err, "Finalize with no content should succeed")
	assert.Empty(t, answer, "Answer should be empty")

	// Hash of empty string
	expectedHash := sha256.Sum256([]byte(""))
	expectedHashStr := hex.EncodeToString(expectedHash[:])
	assert.Equal(t, expectedHashStr, hash, "Hash should match SHA-256 of empty string")
}

// TestTokenAccumulator_Finalize_CannotCallTwice verifies single-use nature.
//
// # Description
//
// Tests that Finalize can only be called once.
func TestTokenAccumulator_Finalize_CannotCallTwice(t *testing.T) {
	acc := newTestAccumulator(t)

	err := acc.Write("Hello")
	require.NoError(t, err, "Write should succeed")

	_, _, err = acc.Finalize()
	require.NoError(t, err, "First Finalize should succeed")

	_, _, err = acc.Finalize()
	assert.Error(t, err, "Second Finalize should fail")
	assert.Contains(t, err.Error(), "destroyed", "Error should mention destroyed state")
}

// =============================================================================
// Test: Destroy
// =============================================================================

// TestTokenAccumulator_Destroy_IsIdempotent verifies idempotent destruction.
//
// # Description
//
// Tests that Destroy can be called multiple times safely.
func TestTokenAccumulator_Destroy_IsIdempotent(t *testing.T) {
	acc := newTestAccumulator(t)

	err := acc.Write("Hello")
	require.NoError(t, err, "Write should succeed")

	// Multiple destroys should not panic
	acc.Destroy()
	acc.Destroy()
	acc.Destroy()
}

// TestTokenAccumulator_Destroy_PreventsSubsequentOperations verifies cleanup.
//
// # Description
//
// Tests that operations fail after Destroy is called.
func TestTokenAccumulator_Destroy_PreventsSubsequentOperations(t *testing.T) {
	acc := newTestAccumulator(t)
	acc.Destroy()

	err := acc.Write("Hello")
	assert.Error(t, err, "Write after Destroy should fail")

	_, _, err = acc.Finalize()
	assert.Error(t, err, "Finalize after Destroy should fail")
}

// =============================================================================
// Test: ID and CreatedAt
// =============================================================================

// TestTokenAccumulator_ID_IsValidUUID verifies ID format.
//
// # Description
//
// Tests that the accumulator ID is a valid UUID.
func TestTokenAccumulator_ID_IsValidUUID(t *testing.T) {
	acc := newTestAccumulator(t)
	defer acc.Destroy()

	id := acc.ID()
	assert.NotEmpty(t, id, "ID should not be empty")

	_, err := uuid.Parse(id)
	assert.NoError(t, err, "ID should be a valid UUID")
}

// TestTokenAccumulator_ID_UniquePerInstance verifies ID uniqueness.
//
// # Description
//
// Tests that each accumulator instance has a unique ID.
func TestTokenAccumulator_ID_UniquePerInstance(t *testing.T) {
	acc1 := newTestAccumulator(t)
	defer acc1.Destroy()

	acc2 := newTestAccumulator(t)
	defer acc2.Destroy()

	assert.NotEqual(t, acc1.ID(), acc2.ID(), "Each accumulator should have a unique ID")
}

// TestTokenAccumulator_CreatedAt_IsRecent verifies timestamp accuracy.
//
// # Description
//
// Tests that CreatedAt returns a recent timestamp.
func TestTokenAccumulator_CreatedAt_IsRecent(t *testing.T) {
	before := time.Now()

	acc := newTestAccumulator(t)
	defer acc.Destroy()

	after := time.Now()

	createdAt := acc.CreatedAt()
	assert.True(t, createdAt.After(before) || createdAt.Equal(before),
		"CreatedAt should be after or equal to test start time")
	assert.True(t, createdAt.Before(after) || createdAt.Equal(after),
		"CreatedAt should be before or equal to test end time")
}

// =============================================================================
// Test: Buffer Overflow
// =============================================================================

// TestTokenAccumulator_Write_Overflow verifies overflow handling.
//
// # Description
//
// Tests that writing more data than buffer capacity returns an error.
func TestTokenAccumulator_Write_Overflow(t *testing.T) {
	acc := newTestAccumulator(t)
	defer acc.Destroy()

	// Create a token that exceeds buffer size
	oversizedToken := make([]byte, SecureBufferSize+1)
	for i := range oversizedToken {
		oversizedToken[i] = 'A'
	}

	err := acc.Write(string(oversizedToken))
	assert.Error(t, err, "Write should fail when exceeding buffer size")
	assert.Contains(t, err.Error(), "overflow", "Error should mention overflow")
}

// TestTokenAccumulator_Write_GradualOverflow verifies cumulative overflow.
//
// # Description
//
// Tests that accumulating tokens until overflow is detected correctly.
func TestTokenAccumulator_Write_GradualOverflow(t *testing.T) {
	acc := newTestAccumulator(t)
	defer acc.Destroy()

	// Write chunks until we overflow
	chunk := make([]byte, 1024) // 1KB chunks
	for i := range chunk {
		chunk[i] = 'X'
	}

	var err error
	for i := 0; i < SecureBufferSize/1024+10; i++ {
		err = acc.Write(string(chunk))
		if err != nil {
			break
		}
	}

	assert.Error(t, err, "Should eventually overflow")
	assert.Contains(t, err.Error(), "overflow", "Error should mention overflow")
}

// TestTokenAccumulator_Finalize_AfterOverflow verifies overflow state.
//
// # Description
//
// Tests that Finalize fails after an overflow has occurred.
func TestTokenAccumulator_Finalize_AfterOverflow(t *testing.T) {
	acc := newTestAccumulator(t)
	defer acc.Destroy()

	// Trigger overflow
	oversizedToken := make([]byte, SecureBufferSize+1)
	for i := range oversizedToken {
		oversizedToken[i] = 'A'
	}
	_ = acc.Write(string(oversizedToken))

	// Finalize should fail
	_, _, err := acc.Finalize()
	assert.Error(t, err, "Finalize after overflow should fail")
}

// =============================================================================
// Test: Concurrency
// =============================================================================

// TestTokenAccumulator_Concurrent_WritesAreSafe verifies thread safety.
//
// # Description
//
// Tests that concurrent writes are handled safely without data corruption.
func TestTokenAccumulator_Concurrent_WritesAreSafe(t *testing.T) {
	acc := newTestAccumulator(t)
	defer acc.Destroy()

	// Number of concurrent writers
	numWriters := 10
	tokensPerWriter := 100

	var wg sync.WaitGroup
	wg.Add(numWriters)

	for i := 0; i < numWriters; i++ {
		go func(writerID int) {
			defer wg.Done()
			for j := 0; j < tokensPerWriter; j++ {
				token := fmt.Sprintf("[%d:%d]", writerID, j)
				_ = acc.Write(token)
			}
		}(i)
	}

	wg.Wait()

	// Should be able to finalize without error
	answer, hash, err := acc.Finalize()
	assert.NoError(t, err, "Finalize should succeed after concurrent writes")
	assert.NotEmpty(t, answer, "Should have accumulated data")
	assert.Len(t, hash, 64, "Hash should be valid")
}

// TestTokenAccumulator_Concurrent_WriteAndDestroy verifies race safety.
//
// # Description
//
// Tests that concurrent Write and Destroy operations don't cause panics.
func TestTokenAccumulator_Concurrent_WriteAndDestroy(t *testing.T) {
	for i := 0; i < 100; i++ {
		acc := newTestAccumulator(t)

		var wg sync.WaitGroup
		wg.Add(2)

		// Writer goroutine
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_ = acc.Write("token")
			}
		}()

		// Destroyer goroutine
		go func() {
			defer wg.Done()
			time.Sleep(time.Microsecond * 10)
			acc.Destroy()
		}()

		wg.Wait()
	}
}

// =============================================================================
// Test: Insecure Accumulator Fallback
// =============================================================================

// TestInsecureAccumulator_FallbackWorks verifies insecure mode.
//
// # Description
//
// Tests that the insecure accumulator fallback works correctly when
// ALEUTIAN_INSECURE_MEMORY is set.
func TestInsecureAccumulator_FallbackWorks(t *testing.T) {
	// Force insecure mode
	original := os.Getenv("ALEUTIAN_INSECURE_MEMORY")
	os.Setenv("ALEUTIAN_INSECURE_MEMORY", "true")
	defer os.Setenv("ALEUTIAN_INSECURE_MEMORY", original)

	acc := newInsecureTokenAccumulator()
	defer acc.Destroy()

	err := acc.Write("Hello")
	require.NoError(t, err, "Write should succeed")

	err = acc.Write(" World")
	require.NoError(t, err, "Second write should succeed")

	answer, hash, err := acc.Finalize()
	require.NoError(t, err, "Finalize should succeed")

	assert.Equal(t, "Hello World", answer, "Answer should be correct")

	// Verify hash
	expectedHash := sha256.Sum256([]byte("Hello World"))
	expectedHashStr := hex.EncodeToString(expectedHash[:])
	assert.Equal(t, expectedHashStr, hash, "Hash should be correct")
}

// TestInsecureAccumulator_HasUniqueID verifies insecure accumulator ID.
//
// # Description
//
// Tests that insecure accumulators also have unique IDs.
func TestInsecureAccumulator_HasUniqueID(t *testing.T) {
	acc1 := newInsecureTokenAccumulator()
	defer acc1.Destroy()

	acc2 := newInsecureTokenAccumulator()
	defer acc2.Destroy()

	assert.NotEqual(t, acc1.ID(), acc2.ID(), "Each accumulator should have unique ID")

	_, err := uuid.Parse(acc1.ID())
	assert.NoError(t, err, "ID should be valid UUID")
}

// =============================================================================
// Test: Utility Functions
// =============================================================================

// TestIsMlockAvailable_ReturnsConsistentResults verifies utility function.
//
// # Description
//
// Tests that IsMlockAvailable returns consistent results across calls.
func TestIsMlockAvailable_ReturnsConsistentResults(t *testing.T) {
	available1, limit1 := IsMlockAvailable()
	available2, limit2 := IsMlockAvailable()

	assert.Equal(t, available1, available2, "Availability should be consistent")
	assert.Equal(t, limit1, limit2, "Limit should be consistent")
}

// =============================================================================
// Test Helpers
// =============================================================================

// newTestAccumulator creates an accumulator for testing.
//
// # Description
//
// Creates a TokenAccumulator suitable for testing. If secure memory is not
// available, falls back to insecure accumulator with env override.
//
// # Inputs
//
//   - t: Test instance for error reporting
//
// # Outputs
//
//   - TokenAccumulator: Ready for testing
func newTestAccumulator(t *testing.T) TokenAccumulator {
	t.Helper()

	// Try secure first
	acc, err := NewSecureTokenAccumulator()
	if err == nil {
		return acc
	}

	// Fall back to insecure for CI environments without mlock
	t.Logf("Falling back to insecure accumulator: %v", err)
	return newInsecureTokenAccumulator()
}
