// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

// Package handlers provides HTTP request handlers for the orchestrator service.
//
// This file implements secure token accumulation for streaming LLM responses.
// Tokens are stored in mlocked memory to prevent swapping to disk, and are
// incrementally hashed for integrity verification.
package handlers

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/awnumar/memguard"
	"github.com/google/uuid"
	"golang.org/x/sys/unix"
)

// =============================================================================
// Constants
// =============================================================================

const (
	// SecureBufferSize is the size of the mlocked buffer for token accumulation.
	// 512 KB provides ample room for long LLM responses with metadata overhead.
	//
	// Capacity:
	//   - 512 KB = 524,288 bytes
	//   - ~131,000 tokens (at 4 bytes/token average)
	//
	// System must be configured with adequate mlock limits.
	// See docs/deployment/memory_security.md for configuration.
	SecureBufferSize = 512 * 1024 // 512 KB (kilobytes)

	// MinMlockLimitKB is the minimum mlock limit required in kilobytes.
	MinMlockLimitKB = 512
)

// =============================================================================
// Package Variables
// =============================================================================

var (
	// memguardInitOnce ensures memguard initialization happens only once.
	memguardInitOnce sync.Once

	// mlockSufficient is set during initialization to indicate if secure memory is available.
	mlockSufficient bool

	// currentMlockLimitKB stores the current mlock limit for logging.
	currentMlockLimitKB int64
)

// =============================================================================
// Interfaces
// =============================================================================

// TokenAccumulator defines the contract for accumulating streamed tokens.
//
// # Description
//
// TokenAccumulator abstracts token storage during LLM streaming, allowing
// different implementations (secure/insecure) based on system capabilities.
// Tokens are hashed incrementally as they arrive for integrity verification.
//
// # Thread Safety
//
// Implementations must be safe for concurrent use.
//
// # Security
//
// Implementations should securely handle token data and support memory wiping.
//
// # Examples
//
//	acc, err := NewSecureTokenAccumulator()
//	if err != nil {
//	    return err
//	}
//	defer acc.Destroy()
//
//	acc.Write("Hello ")
//	acc.Write("world!")
//	answer, hash, _ := acc.Finalize()
//
// # Limitations
//
//   - Buffer size is fixed (cannot grow dynamically)
//   - Accumulator cannot be reused after Finalize() or Destroy()
//
// # Assumptions
//
//   - Tokens are valid UTF-8 strings
//   - System is configured with adequate mlock limits for secure mode
type TokenAccumulator interface {
	// Write appends a token to the accumulator.
	//
	// # Description
	//
	// Copies token bytes into the buffer and updates the incremental hash.
	// Tokens are hashed immediately as they arrive (never "sitting unhashed").
	//
	// # Inputs
	//
	//   - token: Token string to append (must be valid UTF-8)
	//
	// # Outputs
	//
	//   - error: Non-nil if accumulation failed (e.g., buffer overflow)
	//
	// # Examples
	//
	//	if err := acc.Write("Hello"); err != nil {
	//	    log.Error("Write failed", "error", err)
	//	}
	//
	// # Limitations
	//
	//   - Cannot write after Destroy() or Finalize()
	//   - Cannot recover from overflow
	//
	// # Assumptions
	//
	//   - Token is a valid UTF-8 string
	Write(token string) error

	// Finalize returns the accumulated answer and its hash, then wipes memory.
	//
	// # Description
	//
	// Extracts the complete answer string and SHA-256 hash, then securely
	// wipes the buffer. After calling Finalize(), the accumulator cannot be reused.
	//
	// # Outputs
	//
	//   - answer: Complete accumulated answer string
	//   - hash: SHA-256 hash of the answer (hex encoded, 64 characters)
	//   - error: Non-nil if finalization failed
	//
	// # Examples
	//
	//	answer, hash, err := acc.Finalize()
	//	if err != nil {
	//	    return err
	//	}
	//	fmt.Printf("Hash: %s\n", hash)
	//
	// # Limitations
	//
	//   - Can only be called once
	//   - Accumulator is unusable after this call
	//
	// # Assumptions
	//
	//   - Caller will handle the returned strings securely
	Finalize() (answer string, hash string, err error)

	// Destroy wipes memory without returning data.
	//
	// # Description
	//
	// Use this to clean up on error paths where the accumulated data is not needed.
	// Safe to call multiple times (idempotent).
	//
	// # Examples
	//
	//	acc, _ := NewSecureTokenAccumulator()
	//	defer acc.Destroy() // Always clean up
	//
	// # Limitations
	//
	//   - Accumulator is unusable after this call
	//
	// # Assumptions
	//
	//   - None
	Destroy()

	// ID returns a unique identifier for this accumulator instance.
	//
	// # Description
	//
	// Returns a UUID that uniquely identifies this accumulator for logging
	// and debugging purposes.
	//
	// # Outputs
	//
	//   - string: UUID identifying this accumulator
	//
	// # Examples
	//
	//	slog.Info("Processing", "accumulator_id", acc.ID())
	//
	// # Limitations
	//
	//   - None
	//
	// # Assumptions
	//
	//   - None
	ID() string

	// CreatedAt returns when this accumulator was created.
	//
	// # Description
	//
	// Returns the timestamp when the accumulator was instantiated.
	// Useful for tracking accumulator lifetime and debugging.
	//
	// # Outputs
	//
	//   - time.Time: Creation timestamp
	//
	// # Examples
	//
	//	lifetime := time.Since(acc.CreatedAt())
	//
	// # Limitations
	//
	//   - None
	//
	// # Assumptions
	//
	//   - None
	CreatedAt() time.Time
}

// =============================================================================
// Structs: Secure Implementation
// =============================================================================

// secureTokenAccumulator stores tokens in mlocked memory with incremental hashing.
//
// # Description
//
// Uses memguard LockedBuffer for secure in-memory storage of LLM response tokens.
// Memory protections include:
//   - Locked (mlock) to prevent swapping to disk
//   - Guard pages to detect buffer overflows
//   - Canary values to detect buffer underflows
//   - Explicit zeroing on Destroy() to prevent memory forensics
//   - Incremental SHA-256 hashing as tokens arrive
//
// # Fields
//
//   - id: Unique identifier for this accumulator instance
//   - createdAt: When the accumulator was created
//   - mu: Mutex for thread safety
//   - buffer: memguard LockedBuffer for secure storage
//   - offset: Current write position in buffer
//   - hasher: Incremental SHA-256 hasher
//   - overflow: Set if buffer capacity exceeded
//   - destroyed: Set after Destroy() or Finalize() called
//
// # Thread Safety
//
// Safe for concurrent use. Uses mutex to protect internal state.
//
// # System Requirements
//
// Requires mlock limit >= SecureBufferSize (512 KB).
// See docs/deployment/memory_security.md for configuration.
type secureTokenAccumulator struct {
	id        string
	createdAt time.Time
	mu        sync.Mutex
	buffer    *memguard.LockedBuffer
	offset    int
	hasher    hash.Hash
	overflow  bool
	destroyed bool
}

// =============================================================================
// Structs: Insecure Fallback Implementation
// =============================================================================

// insecureTokenAccumulator is a fallback for systems without sufficient mlock.
//
// # Description
//
// Provides the same interface as secureTokenAccumulator but uses standard
// Go memory ([]byte). This is used when:
//   - mlock limits are insufficient
//   - ALEUTIAN_INSECURE_MEMORY=true is set
//
// # Security Warning
//
// This implementation does NOT provide the security guarantees of the secure
// version. Data may be swapped to disk and is not protected by guard pages.
//
// # Fields
//
//   - id: Unique identifier for this accumulator instance
//   - createdAt: When the accumulator was created
//   - mu: Mutex for thread safety
//   - data: Standard byte slice for storage
//   - hasher: Incremental SHA-256 hasher
//   - overflow: Set if buffer capacity exceeded
//   - destroyed: Set after Destroy() or Finalize() called
//
// # Thread Safety
//
// Safe for concurrent use.
type insecureTokenAccumulator struct {
	id        string
	createdAt time.Time
	mu        sync.Mutex
	data      []byte
	hasher    hash.Hash
	overflow  bool
	destroyed bool
}

// =============================================================================
// Constructor Functions
// =============================================================================

// NewSecureTokenAccumulator creates a new secure token accumulator.
//
// # Description
//
// Allocates a mlocked buffer of SecureBufferSize bytes for storing LLM tokens.
// If mlock limit is insufficient and ALEUTIAN_INSECURE_MEMORY is not set,
// returns an error. If ALEUTIAN_INSECURE_MEMORY=true, falls back to
// insecure accumulator with a warning.
//
// # Outputs
//
//   - TokenAccumulator: Ready for use (may be secure or insecure based on system)
//   - error: Non-nil if allocation failed and no fallback available
//
// # Examples
//
//	acc, err := NewSecureTokenAccumulator()
//	if err != nil {
//	    return err
//	}
//	defer acc.Destroy()
//
//	acc.Write("Hello ")
//	acc.Write("world!")
//	answer, hash, _ := acc.Finalize()
//
// # Limitations
//
//   - May return insecure accumulator if mlock limits insufficient
//
// # Assumptions
//
//   - System is properly configured (see deployment docs)
func NewSecureTokenAccumulator() (TokenAccumulator, error) {
	initMemguard()

	if !mlockSufficient {
		return handleInsufficientMlock()
	}

	return allocateSecureBuffer()
}

// newInsecureTokenAccumulator creates an insecure fallback accumulator.
//
// # Description
//
// Creates a token accumulator using standard Go memory instead of mlocked memory.
// Used when secure memory is unavailable and user has acknowledged the risk.
//
// # Outputs
//
//   - TokenAccumulator: Insecure accumulator ready for use
//
// # Limitations
//
//   - Data may be swapped to disk
//   - No guard page protection
//
// # Assumptions
//
//   - User has acknowledged security implications
func newInsecureTokenAccumulator() TokenAccumulator {
	accID := uuid.New().String()

	slog.Warn("Created INSECURE token accumulator - data may be swapped to disk",
		"accumulator_id", accID,
	)

	return &insecureTokenAccumulator{
		id:        accID,
		createdAt: time.Now(),
		data:      make([]byte, 0, SecureBufferSize),
		hasher:    sha256.New(),
		overflow:  false,
		destroyed: false,
	}
}

// =============================================================================
// secureTokenAccumulator Methods
// =============================================================================

// Write appends a token to the secure buffer.
//
// # Description
//
// Copies token bytes into the mlocked buffer and updates the incremental hash.
// If buffer would overflow, sets overflow flag and returns error.
// Tokens are hashed immediately as they arrive (never "sitting unhashed").
//
// # Inputs
//
//   - token: Token string to append
//
// # Outputs
//
//   - error: Non-nil if buffer overflow would occur or accumulator destroyed
//
// # Examples
//
//	if err := acc.Write("Hello"); err != nil {
//	    log.Error("Write failed", "error", err)
//	}
//
// # Limitations
//
//   - Cannot write after Destroy() or Finalize()
//   - Cannot recover from overflow
//
// # Assumptions
//
//   - Token is a valid UTF-8 string
func (a *secureTokenAccumulator) Write(token string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if err := a.validateWriteState(); err != nil {
		return err
	}

	tokenBytes := []byte(token)

	if err := a.checkBufferCapacity(len(tokenBytes)); err != nil {
		return err
	}

	a.copyToBuffer(tokenBytes)
	a.updateHash(tokenBytes)

	return nil
}

// Finalize returns the accumulated answer and its hash, then wipes the buffer.
//
// # Description
//
// Extracts the complete answer string and SHA-256 hash from the secure buffer,
// then securely wipes the buffer memory. After calling Finalize(), the
// accumulator cannot be reused.
//
// # Outputs
//
//   - answer: Complete accumulated answer (copy of secure buffer contents)
//   - hash: SHA-256 hash of the answer (hex encoded, 64 characters)
//   - error: Non-nil if overflow occurred or accumulator already destroyed
//
// # Examples
//
//	answer, hash, err := acc.Finalize()
//	if err != nil {
//	    return err
//	}
//	fmt.Printf("Answer: %s\nHash: %s\n", answer, hash)
//
// # Limitations
//
//   - Can only be called once
//   - Accumulator is unusable after this call
//
// # Assumptions
//
//   - Caller will handle the returned strings securely
func (a *secureTokenAccumulator) Finalize() (string, string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if err := a.validateFinalizeState(); err != nil {
		return "", "", err
	}

	answer := a.extractAnswer()
	hashStr := a.finalizeHash()
	a.wipeBuffer()

	a.logFinalization(len(answer), hashStr)

	return answer, hashStr, nil
}

// Destroy wipes the buffer without returning data.
//
// # Description
//
// Securely wipes the mlocked buffer memory. Use this to clean up on error
// paths where the accumulated data is not needed. Safe to call multiple
// times (idempotent).
//
// # Examples
//
//	acc, _ := NewSecureTokenAccumulator()
//	defer acc.Destroy() // Always clean up
//
// # Limitations
//
//   - Accumulator is unusable after this call
//
// # Assumptions
//
//   - None
func (a *secureTokenAccumulator) Destroy() {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.destroyed {
		return
	}

	a.wipeBuffer()
	a.logDestruction()
}

// ID returns the unique identifier for this accumulator instance.
//
// # Description
//
// Returns a UUID that uniquely identifies this accumulator for logging
// and debugging purposes.
//
// # Outputs
//
//   - string: UUID identifying this accumulator
//
// # Examples
//
//	slog.Info("Processing", "accumulator_id", acc.ID())
//
// # Limitations
//
//   - None
//
// # Assumptions
//
//   - None
func (a *secureTokenAccumulator) ID() string {
	return a.id
}

// CreatedAt returns when this accumulator was created.
//
// # Description
//
// Returns the timestamp when the accumulator was instantiated.
//
// # Outputs
//
//   - time.Time: Creation timestamp
//
// # Examples
//
//	lifetime := time.Since(acc.CreatedAt())
//
// # Limitations
//
//   - None
//
// # Assumptions
//
//   - None
func (a *secureTokenAccumulator) CreatedAt() time.Time {
	return a.createdAt
}

// =============================================================================
// secureTokenAccumulator Private Methods
// =============================================================================

// validateWriteState checks if the accumulator is in a valid state for writing.
func (a *secureTokenAccumulator) validateWriteState() error {
	if a.destroyed {
		return fmt.Errorf("accumulator already destroyed")
	}
	if a.overflow {
		return fmt.Errorf("secure buffer overflow - response too large")
	}
	return nil
}

// checkBufferCapacity verifies there is room for the token.
func (a *secureTokenAccumulator) checkBufferCapacity(tokenLen int) error {
	if a.offset+tokenLen > SecureBufferSize {
		a.overflow = true
		return fmt.Errorf("secure buffer overflow: need %d bytes, have %d remaining",
			tokenLen, SecureBufferSize-a.offset)
	}
	return nil
}

// copyToBuffer copies token bytes into the secure buffer.
func (a *secureTokenAccumulator) copyToBuffer(tokenBytes []byte) {
	copy(a.buffer.Bytes()[a.offset:], tokenBytes)
	a.offset += len(tokenBytes)
}

// updateHash adds token bytes to the incremental hash.
func (a *secureTokenAccumulator) updateHash(tokenBytes []byte) {
	a.hasher.Write(tokenBytes)
}

// validateFinalizeState checks if the accumulator can be finalized.
func (a *secureTokenAccumulator) validateFinalizeState() error {
	if a.destroyed {
		return fmt.Errorf("accumulator already destroyed")
	}
	if a.overflow {
		a.wipeBuffer()
		return fmt.Errorf("buffer overflowed during accumulation")
	}
	return nil
}

// extractAnswer copies the answer out of secure memory.
func (a *secureTokenAccumulator) extractAnswer() string {
	return string(a.buffer.Bytes()[:a.offset])
}

// finalizeHash returns the final hash as a hex string.
func (a *secureTokenAccumulator) finalizeHash() string {
	hashBytes := a.hasher.Sum(nil)
	return hex.EncodeToString(hashBytes)
}

// wipeBuffer destroys the secure buffer and marks as destroyed.
func (a *secureTokenAccumulator) wipeBuffer() {
	if a.buffer != nil {
		a.buffer.Destroy()
	}
	a.destroyed = true
}

// logFinalization logs successful finalization.
func (a *secureTokenAccumulator) logFinalization(answerLen int, hashStr string) {
	slog.Debug("Finalized secure token accumulator",
		"accumulator_id", a.id,
		"answer_length", answerLen,
		"hash", hashStr[:16]+"...",
	)
}

// logDestruction logs accumulator destruction.
func (a *secureTokenAccumulator) logDestruction() {
	slog.Debug("Destroyed secure token accumulator",
		"accumulator_id", a.id,
	)
}

// =============================================================================
// insecureTokenAccumulator Methods
// =============================================================================

// Write appends a token to the insecure buffer.
//
// # Description
//
// Copies token bytes into the byte slice and updates the incremental hash.
//
// # Inputs
//
//   - token: Token string to append
//
// # Outputs
//
//   - error: Non-nil if buffer overflow or accumulator destroyed
//
// # Examples
//
//	if err := acc.Write("Hello"); err != nil {
//	    log.Error("Write failed", "error", err)
//	}
//
// # Limitations
//
//   - Data is NOT protected by mlock
//
// # Assumptions
//
//   - Token is a valid UTF-8 string
func (a *insecureTokenAccumulator) Write(token string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if err := a.validateWriteState(); err != nil {
		return err
	}

	tokenBytes := []byte(token)

	if err := a.checkBufferCapacity(len(tokenBytes)); err != nil {
		return err
	}

	a.appendToData(tokenBytes)
	a.updateHash(tokenBytes)

	return nil
}

// Finalize returns the accumulated answer and hash, attempting to zero memory.
//
// # Description
//
// Extracts the answer and hash, then attempts to zero the byte slice.
// Note: Due to Go's garbage collector, copies of the data may remain in memory.
//
// # Outputs
//
//   - answer: Complete accumulated answer
//   - hash: SHA-256 hash (hex encoded)
//   - error: Non-nil if overflow or already destroyed
//
// # Examples
//
//	answer, hash, err := acc.Finalize()
//
// # Limitations
//
//   - Memory wiping is best-effort only
//
// # Assumptions
//
//   - Caller acknowledges reduced security
func (a *insecureTokenAccumulator) Finalize() (string, string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if err := a.validateFinalizeState(); err != nil {
		return "", "", err
	}

	answer := string(a.data)
	hashStr := a.finalizeHash()
	a.wipeData()

	a.logFinalization(len(answer))

	return answer, hashStr, nil
}

// Destroy attempts to wipe memory (best effort).
//
// # Description
//
// Zeros the byte slice and releases it. Due to Go's GC, this is best-effort.
//
// # Examples
//
//	defer acc.Destroy()
//
// # Limitations
//
//   - Memory wiping is best-effort only
//
// # Assumptions
//
//   - None
func (a *insecureTokenAccumulator) Destroy() {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.destroyed {
		return
	}

	a.wipeData()
	a.logDestruction()
}

// ID returns the unique identifier for this accumulator instance.
//
// # Description
//
// Returns a UUID that uniquely identifies this accumulator.
//
// # Outputs
//
//   - string: UUID identifying this accumulator
//
// # Examples
//
//	slog.Info("Processing", "accumulator_id", acc.ID())
//
// # Limitations
//
//   - None
//
// # Assumptions
//
//   - None
func (a *insecureTokenAccumulator) ID() string {
	return a.id
}

// CreatedAt returns when this accumulator was created.
//
// # Description
//
// Returns the timestamp when the accumulator was instantiated.
//
// # Outputs
//
//   - time.Time: Creation timestamp
//
// # Examples
//
//	lifetime := time.Since(acc.CreatedAt())
//
// # Limitations
//
//   - None
//
// # Assumptions
//
//   - None
func (a *insecureTokenAccumulator) CreatedAt() time.Time {
	return a.createdAt
}

// =============================================================================
// insecureTokenAccumulator Private Methods
// =============================================================================

// validateWriteState checks if the accumulator is in a valid state for writing.
func (a *insecureTokenAccumulator) validateWriteState() error {
	if a.destroyed {
		return fmt.Errorf("accumulator already destroyed")
	}
	if a.overflow {
		return fmt.Errorf("buffer overflow - response too large")
	}
	return nil
}

// checkBufferCapacity verifies there is room for the token.
func (a *insecureTokenAccumulator) checkBufferCapacity(tokenLen int) error {
	if len(a.data)+tokenLen > SecureBufferSize {
		a.overflow = true
		return fmt.Errorf("buffer overflow: need %d bytes, have %d remaining",
			tokenLen, SecureBufferSize-len(a.data))
	}
	return nil
}

// appendToData appends token bytes to the data slice.
func (a *insecureTokenAccumulator) appendToData(tokenBytes []byte) {
	a.data = append(a.data, tokenBytes...)
}

// updateHash adds token bytes to the incremental hash.
func (a *insecureTokenAccumulator) updateHash(tokenBytes []byte) {
	a.hasher.Write(tokenBytes)
}

// validateFinalizeState checks if the accumulator can be finalized.
func (a *insecureTokenAccumulator) validateFinalizeState() error {
	if a.destroyed {
		return fmt.Errorf("accumulator already destroyed")
	}
	if a.overflow {
		a.wipeData()
		return fmt.Errorf("buffer overflowed during accumulation")
	}
	return nil
}

// finalizeHash returns the final hash as a hex string.
func (a *insecureTokenAccumulator) finalizeHash() string {
	hashBytes := a.hasher.Sum(nil)
	return hex.EncodeToString(hashBytes)
}

// wipeData zeros the data slice (best effort).
func (a *insecureTokenAccumulator) wipeData() {
	for i := range a.data {
		a.data[i] = 0
	}
	a.data = nil
	a.destroyed = true
}

// logFinalization logs successful finalization.
func (a *insecureTokenAccumulator) logFinalization(answerLen int) {
	slog.Debug("Finalized insecure token accumulator",
		"accumulator_id", a.id,
		"answer_length", answerLen,
	)
}

// logDestruction logs accumulator destruction.
func (a *insecureTokenAccumulator) logDestruction() {
	slog.Debug("Destroyed insecure token accumulator",
		"accumulator_id", a.id,
	)
}

// =============================================================================
// Package Initialization Functions
// =============================================================================

// initMemguard initializes the memguard library and checks mlock limits.
//
// # Description
//
// Performs one-time initialization of memguard and validates that the system
// has sufficient mlock limits for secure memory operations. Called automatically
// when creating the first SecureTokenAccumulator.
//
// # Outputs
//
// None. Sets package-level variables mlockSufficient and currentMlockLimitKB.
//
// # Examples
//
//	initMemguard() // Called automatically by NewSecureTokenAccumulator
//
// # Limitations
//
//   - Only initializes once (subsequent calls are no-ops)
//
// # Assumptions
//
//   - Called before any secure memory operations
func initMemguard() {
	memguardInitOnce.Do(func() {
		memguard.CatchInterrupt()
		mlockSufficient, currentMlockLimitKB = checkMlockLimit()
		logMlockStatus()
	})
}

// checkMlockLimit checks if the system has sufficient mlock limits.
//
// # Description
//
// Queries the kernel for the current mlock resource limit and compares
// it against the minimum required for secure token accumulation.
//
// # Outputs
//
//   - bool: True if limit is sufficient (>= MinMlockLimitKB)
//   - int64: Current limit in kilobytes (-1 if unlimited)
//
// # Examples
//
//	sufficient, limitKB := checkMlockLimit()
//	if !sufficient {
//	    log.Warn("mlock limit too low", "limit_kb", limitKB)
//	}
//
// # Limitations
//
//   - Only works on Unix-like systems (Linux, macOS, BSD)
//
// # Assumptions
//
//   - Running on a Unix-like system
func checkMlockLimit() (bool, int64) {
	var rlimit unix.Rlimit
	if err := unix.Getrlimit(unix.RLIMIT_MEMLOCK, &rlimit); err != nil {
		slog.Warn("Could not determine mlock limit", "error", err)
		return true, -1
	}

	if rlimit.Cur == unix.RLIM_INFINITY {
		return true, -1
	}

	limitKB := int64(rlimit.Cur / 1024)
	return limitKB >= MinMlockLimitKB, limitKB
}

// logMlockStatus logs the current mlock status.
func logMlockStatus() {
	if mlockSufficient {
		slog.Info("Secure memory initialized",
			"mlock_limit_kb", currentMlockLimitKB,
			"required_kb", MinMlockLimitKB,
			"status", "sufficient",
		)
	} else {
		logInsufficientMlock()
	}
}

// logInsufficientMlock logs a warning about insufficient mlock limits.
func logInsufficientMlock() {
	insecureMode := os.Getenv("ALEUTIAN_INSECURE_MEMORY") == "true"
	if insecureMode {
		slog.Warn("SECURITY: Running with insecure memory - mlock limit insufficient",
			"current_limit_kb", currentMlockLimitKB,
			"required_kb", MinMlockLimitKB,
			"env_override", "ALEUTIAN_INSECURE_MEMORY=true",
		)
	} else {
		slog.Error("mlock limit insufficient for secure memory",
			"current_limit_kb", currentMlockLimitKB,
			"required_kb", MinMlockLimitKB,
			"help", "See docs/deployment/memory_security.md or set ALEUTIAN_INSECURE_MEMORY=true",
		)
	}
}

// handleInsufficientMlock handles the case when mlock limits are insufficient.
func handleInsufficientMlock() (TokenAccumulator, error) {
	if os.Getenv("ALEUTIAN_INSECURE_MEMORY") == "true" {
		slog.Warn("Using insecure memory accumulator due to mlock limits",
			"current_limit_kb", currentMlockLimitKB,
			"required_kb", MinMlockLimitKB,
		)
		return newInsecureTokenAccumulator(), nil
	}
	return nil, fmt.Errorf(
		"mlock limit insufficient: have %d KB, need %d KB. "+
			"Configure system limits or set ALEUTIAN_INSECURE_MEMORY=true",
		currentMlockLimitKB, MinMlockLimitKB,
	)
}

// allocateSecureBuffer allocates a new secure buffer.
func allocateSecureBuffer() (TokenAccumulator, error) {
	buf := memguard.NewBuffer(SecureBufferSize)
	if buf == nil {
		return nil, fmt.Errorf("failed to allocate secure buffer of %d bytes", SecureBufferSize)
	}
	buf.Melt()

	accID := uuid.New().String()

	slog.Debug("Created secure token accumulator",
		"accumulator_id", accID,
		"buffer_size", SecureBufferSize,
	)

	return &secureTokenAccumulator{
		id:        accID,
		createdAt: time.Now(),
		buffer:    buf,
		offset:    0,
		hasher:    sha256.New(),
		overflow:  false,
		destroyed: false,
	}, nil
}

// =============================================================================
// Utility Functions
// =============================================================================

// IsMlockAvailable returns whether secure memory is available on this system.
//
// # Description
//
// Checks if the system has sufficient mlock limits for secure token accumulation.
// Can be used to inform users about security status.
//
// # Outputs
//
//   - bool: True if secure memory is available
//   - int64: Current mlock limit in KB (-1 if unlimited)
//
// # Examples
//
//	if available, limit := IsMlockAvailable(); !available {
//	    log.Warn("Secure memory not available", "limit_kb", limit)
//	}
//
// # Limitations
//
//   - Result may change if system limits are modified
//
// # Assumptions
//
//   - None
func IsMlockAvailable() (bool, int64) {
	initMemguard()
	return mlockSufficient, currentMlockLimitKB
}

// PurgeAllSecureMemory wipes all memguard-allocated memory.
//
// # Description
//
// Should be called during graceful shutdown to ensure all sensitive data
// is wiped from memory. This is automatically called on SIGINT/SIGTERM
// if memguard.CatchInterrupt() was called.
//
// # Examples
//
//	defer PurgeAllSecureMemory()
//
// # Limitations
//
//   - After calling this, all existing LockedBuffers are invalid
//
// # Assumptions
//
//   - Called during application shutdown
func PurgeAllSecureMemory() {
	memguard.Purge()
	slog.Info("Purged all secure memory")
}
