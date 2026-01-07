// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.

// Package ux provides user experience components for the Aleutian CLI.
//
// This file defines integrity verification types for hash chain validation.
// The hash chain provides tamper-evident logging for streaming conversations.
//
// Hash Chain Design:
//
//	Each StreamEvent has a Hash computed from its content and a PrevHash
//	linking to the previous event. This creates a chain similar to blockchain:
//
//	Event[0] → Event[1] → Event[2] → ... → Event[N]
//	  Hash₀     Hash₁     Hash₂           HashN
//	    ↑         ↑         ↑               ↑
//	    └─────────┴─────────┴───────────────┘
//	           Each PrevHash links to previous Hash
//
// If any event is modified, its hash changes, breaking the chain.
package ux

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"
)

// =============================================================================
// Interfaces
// =============================================================================

// ChainVerifier verifies the integrity of a hash chain.
//
// # Description
//
// Abstracts the verification of event chains, allowing different
// verification strategies (quick PrevHash check vs full recompute).
//
// # Thread Safety
//
// Implementations must be safe for concurrent use.
type ChainVerifier interface {
	// Verify checks the integrity of a sequence of stream events.
	//
	// # Description
	//
	// Verifies that the hash chain is unbroken and valid.
	//
	// # Inputs
	//
	//   - events: Ordered list of stream events from the session
	//
	// # Outputs
	//
	//   - *ChainVerificationResult: Detailed verification results
	//
	// # Examples
	//
	//   verifier := NewQuickChainVerifier()
	//   result := verifier.Verify(events)
	//   if !result.Valid {
	//       log.Warn("chain broken", "error", result.ErrorMessage)
	//   }
	//
	// # Limitations
	//
	//   - Implementation-specific verification depth
	//
	// # Assumptions
	//
	//   - Events are in chronological order
	//   - First event has empty PrevHash
	Verify(events []StreamEvent) *ChainVerificationResult
}

// HashComputer computes cryptographic hashes.
//
// # Description
//
// Abstracts hash computation for testability and algorithm flexibility.
//
// # Thread Safety
//
// Implementations must be safe for concurrent use.
type HashComputer interface {
	// ComputeEventHash computes the hash for a stream event.
	//
	// # Description
	//
	// Computes hash using: SHA256(Content || CreatedAt || PrevHash)
	//
	// # Inputs
	//
	//   - content: The event content
	//   - createdAt: Creation timestamp (Unix ms)
	//   - prevHash: Hash of previous event
	//
	// # Outputs
	//
	//   - string: 64-character hex hash
	//
	// # Examples
	//
	//   computer := NewSHA256HashComputer()
	//   hash := computer.ComputeEventHash("Hello", 1735657200000, "abc...")
	//
	// # Limitations
	//
	//   - Hash algorithm is implementation-specific
	//
	// # Assumptions
	//
	//   - Inputs are valid (no nil checks)
	ComputeEventHash(content string, createdAt int64, prevHash string) string

	// ComputeContentHash computes a simple hash of content.
	//
	// # Description
	//
	// Computes SHA256 hash of the provided content string.
	//
	// # Inputs
	//
	//   - content: The content to hash
	//
	// # Outputs
	//
	//   - string: 64-character hex hash
	//
	// # Examples
	//
	//   hash := computer.ComputeContentHash("The answer is 42.")
	//
	// # Limitations
	//
	//   - Empty content produces a valid hash
	//
	// # Assumptions
	//
	//   - Content is valid UTF-8
	ComputeContentHash(content string) string
}

// =============================================================================
// Structs
// =============================================================================

// IntegrityInfo contains hash chain and integrity verification information.
//
// # Description
//
// Surfaces the cryptographic integrity features to users, showing them
// that their conversation is protected by a hash chain. This builds trust
// and enables verification of data integrity.
//
// The hash chain works like a blockchain:
//   - Each StreamEvent has a SHA-256 Hash of its content
//   - Each event's PrevHash links to the previous event
//   - The ChainHash is the final hash of the entire stream
//   - Any tampering breaks the chain (hash mismatch)
//
// # Existing Implementation (pkg/ux/events.go)
//
// This surfaces values already computed by the streaming infrastructure:
//   - StreamEvent.Hash (line 191): SHA-256 of Content || CreatedAt || PrevHash
//   - StreamEvent.PrevHash (line 195): Links to previous event
//   - StreamResult.ChainHash (line 282): Final hash from last event
//   - StreamResult.ContentHash (line 287): SHA-256 of accumulated answer
//
// # Fields
//
//   - ChainHash: Final hash of the streaming chain (64-char hex)
//   - ContentHash: SHA-256 of last response content
//   - TurnHashes: Hash of each Q&A turn
//   - SourceHashes: Hash of each retrieved source
//   - ChainLength: Number of events in chain
//   - IntegrityVerified: Whether verification passed
//   - VerificationError: Details if verification failed
//   - VerifiedAt: When verification was performed
//
// # Privacy
//
// Hashes are safe to display - they cannot be reversed to reveal content.
// They serve as fingerprints that prove content hasn't been modified.
//
// # Thread Safety
//
// IntegrityInfo is NOT thread-safe. Use external synchronization if
// modifying from multiple goroutines.
type IntegrityInfo struct {
	ChainHash         string            `json:"chain_hash"`
	ContentHash       string            `json:"content_hash"`
	TurnHashes        map[int]string    `json:"turn_hashes,omitempty"`
	SourceHashes      map[string]string `json:"source_hashes,omitempty"`
	ChainLength       int               `json:"chain_length"`
	IntegrityVerified bool              `json:"integrity_verified"`
	VerificationError string            `json:"verification_error,omitempty"`
	VerifiedAt        int64             `json:"verified_at,omitempty"`
}

// ChainVerificationResult contains detailed results from chain verification.
//
// # Description
//
// Returned by ChainVerifier.Verify to provide detailed information about
// the verification process, including where any failures occurred.
//
// # Fields
//
//   - Valid: Whether the entire chain is valid
//   - ChainLength: Number of events verified
//   - FinalHash: The hash of the last event in the chain
//   - InvalidEventIndex: Index of first invalid event (-1 if all valid)
//   - ExpectedHash: What the hash should have been (if invalid)
//   - ActualHash: What the hash actually was (if invalid)
//   - ErrorMessage: Human-readable error description
//
// # Thread Safety
//
// Immutable after creation. Safe for concurrent read access.
type ChainVerificationResult struct {
	Valid             bool   `json:"valid"`
	ChainLength       int    `json:"chain_length"`
	FinalHash         string `json:"final_hash,omitempty"`
	InvalidEventIndex int    `json:"invalid_event_index"`
	ExpectedHash      string `json:"expected_hash,omitempty"`
	ActualHash        string `json:"actual_hash,omitempty"`
	ErrorMessage      string `json:"error_message,omitempty"`
}

// quickChainVerifier verifies chains by checking PrevHash links only.
//
// # Description
//
// Fast verification that only checks PrevHash chain consistency.
// Does NOT recompute hashes from content.
//
// # Fields
//
// None. Stateless implementation.
//
// # Thread Safety
//
// Thread-safe. No shared state.
type quickChainVerifier struct{}

// fullChainVerifier verifies chains by recomputing all hashes.
//
// # Description
//
// Complete verification that recomputes each event's hash from content
// and verifies both hash correctness and chain links.
//
// # Fields
//
//   - hashComputer: Used to compute hashes from content
//
// # Thread Safety
//
// Thread-safe if hashComputer is thread-safe.
type fullChainVerifier struct {
	hashComputer HashComputer
}

// sha256HashComputer computes hashes using SHA-256.
//
// # Description
//
// Production implementation of HashComputer using SHA-256.
//
// # Fields
//
// None. Stateless implementation.
//
// # Thread Safety
//
// Thread-safe. No shared state.
type sha256HashComputer struct{}

// =============================================================================
// Constructor Functions
// =============================================================================

// NewIntegrityInfo creates an IntegrityInfo from a StreamResult.
//
// # Description
//
// Extracts hash chain information from a completed stream result.
// This is the primary way to create IntegrityInfo after streaming.
//
// # Inputs
//
//   - result: The completed StreamResult containing hash chain data
//   - verified: Whether the chain has been verified
//
// # Outputs
//
//   - *IntegrityInfo: Populated integrity information
//
// # Examples
//
//	result := &StreamResult{ChainHash: "abc...", ContentHash: "def..."}
//	info := NewIntegrityInfo(result, true)
//
// # Limitations
//
//   - TurnHashes and SourceHashes are not populated by this function
//   - Caller must populate those fields separately if needed
//
// # Assumptions
//
//   - result is non-nil and contains valid hash data
func NewIntegrityInfo(result *StreamResult, verified bool) *IntegrityInfo {
	return &IntegrityInfo{
		ChainHash:         result.ChainHash,
		ContentHash:       result.ContentHash,
		ChainLength:       result.TotalEvents,
		IntegrityVerified: verified,
		VerifiedAt:        time.Now().UnixMilli(),
		TurnHashes:        make(map[int]string),
		SourceHashes:      make(map[string]string),
	}
}

// NewIntegrityInfoFromVerification creates IntegrityInfo from verification result.
//
// # Description
//
// Creates an IntegrityInfo with verification results populated.
// Use after calling Verify on a ChainVerifier.
//
// # Inputs
//
//   - verification: Result from ChainVerifier.Verify
//
// # Outputs
//
//   - *IntegrityInfo: Populated with verification results
//
// # Examples
//
//	verifier := NewQuickChainVerifier()
//	verification := verifier.Verify(events)
//	info := NewIntegrityInfoFromVerification(verification)
//
// # Limitations
//
//   - ContentHash is not set (not available in verification result)
//
// # Assumptions
//
//   - verification is non-nil
func NewIntegrityInfoFromVerification(verification *ChainVerificationResult) *IntegrityInfo {
	return &IntegrityInfo{
		ChainHash:         verification.FinalHash,
		ChainLength:       verification.ChainLength,
		IntegrityVerified: verification.Valid,
		VerificationError: verification.ErrorMessage,
		VerifiedAt:        time.Now().UnixMilli(),
		TurnHashes:        make(map[int]string),
		SourceHashes:      make(map[string]string),
	}
}

// NewQuickChainVerifier creates a verifier that checks PrevHash links only.
//
// # Description
//
// Creates a fast verifier that only checks chain link consistency.
// Use when you trust the stored hashes and just need to verify links.
//
// # Outputs
//
//   - ChainVerifier: Quick verification implementation
//
// # Examples
//
//	verifier := NewQuickChainVerifier()
//	result := verifier.Verify(events)
//
// # Limitations
//
//   - Does not detect content modification if Hash field is also modified
//
// # Assumptions
//
//   - Event Hash fields were computed correctly at creation time
func NewQuickChainVerifier() ChainVerifier {
	return &quickChainVerifier{}
}

// NewFullChainVerifier creates a verifier that recomputes all hashes.
//
// # Description
//
// Creates a comprehensive verifier that recomputes each event's hash
// and verifies both hash correctness and chain links.
//
// # Outputs
//
//   - ChainVerifier: Full verification implementation
//
// # Examples
//
//	verifier := NewFullChainVerifier()
//	result := verifier.Verify(events)
//
// # Limitations
//
//   - Slower than quick verification (O(n) hash computations)
//
// # Assumptions
//
//   - Events contain correct Content and CreatedAt fields
func NewFullChainVerifier() ChainVerifier {
	return &fullChainVerifier{
		hashComputer: NewSHA256HashComputer(),
	}
}

// NewSHA256HashComputer creates a hash computer using SHA-256.
//
// # Description
//
// Creates the production hash computer implementation.
//
// # Outputs
//
//   - HashComputer: SHA-256 implementation
//
// # Examples
//
//	computer := NewSHA256HashComputer()
//	hash := computer.ComputeContentHash("Hello")
//
// # Limitations
//
//   - SHA-256 only (no algorithm selection)
//
// # Assumptions
//
//   - SHA-256 is available in the runtime
func NewSHA256HashComputer() HashComputer {
	return &sha256HashComputer{}
}

// =============================================================================
// IntegrityInfo Methods
// =============================================================================

// AddTurnHash adds a hash for a conversation turn.
//
// # Description
//
// Computes and stores a hash for a Q&A turn. The hash is computed
// from the concatenation of question and answer.
//
// # Inputs
//
//   - turnNumber: 1-indexed turn number
//   - question: The user's question
//   - answer: The LLM's answer
//
// # Outputs
//
// None. Modifies TurnHashes in place.
//
// # Examples
//
//	info := NewIntegrityInfo(result, true)
//	info.AddTurnHash(1, "What is 2+2?", "2+2 equals 4.")
//
// # Limitations
//
//   - Overwrites existing hash for the same turn number
//
// # Assumptions
//
//   - TurnHashes map is initialized
func (i *IntegrityInfo) AddTurnHash(turnNumber int, question, answer string) {
	computer := NewSHA256HashComputer()
	content := question + answer
	i.TurnHashes[turnNumber] = computer.ComputeContentHash(content)
}

// AddSourceHash adds a hash for a retrieved source.
//
// # Description
//
// Stores the content hash for a retrieved document source.
// Used to verify source content hasn't been modified.
//
// # Inputs
//
//   - sourceName: The source identifier/name
//   - content: The source content
//
// # Outputs
//
// None. Modifies SourceHashes in place.
//
// # Examples
//
//	info := NewIntegrityInfo(result, true)
//	info.AddSourceHash("document.pdf", "The content...")
//
// # Limitations
//
//   - Overwrites existing hash for the same source name
//
// # Assumptions
//
//   - SourceHashes map is initialized
func (i *IntegrityInfo) AddSourceHash(sourceName, content string) {
	computer := NewSHA256HashComputer()
	i.SourceHashes[sourceName] = computer.ComputeContentHash(content)
}

// FormatForDisplay returns a formatted string for UI display.
//
// # Description
//
// Creates a human-readable summary of the integrity information
// suitable for display in the session summary.
//
// # Outputs
//
//   - string: Formatted integrity summary
//
// # Examples
//
//	info := &IntegrityInfo{ChainLength: 47, IntegrityVerified: true}
//	fmt.Println(info.FormatForDisplay())
//	// "✓ Verified | Chain: 47 events | Hash: a3f2c8d9...e7f8a9b0"
//
// # Limitations
//
//   - Hash is truncated for display
//
// # Assumptions
//
//   - ChainHash is a valid hex string or empty
func (i *IntegrityInfo) FormatForDisplay() string {
	status := "✓ Verified"
	if !i.IntegrityVerified {
		status = "✗ FAILED"
	}

	hashDisplay := truncateHash(i.ChainHash)
	if i.ChainHash == "" {
		hashDisplay = "N/A"
	}

	return fmt.Sprintf("%s | Chain: %d events | Hash: %s",
		status, i.ChainLength, hashDisplay)
}

// GetTurnHash returns the hash for a specific turn.
//
// # Description
//
// Retrieves the previously stored hash for a conversation turn.
//
// # Inputs
//
//   - turnNumber: 1-indexed turn number
//
// # Outputs
//
//   - string: The turn hash, or empty string if not found
//   - bool: True if the turn hash exists
//
// # Examples
//
//	hash, ok := info.GetTurnHash(1)
//	if ok {
//	    fmt.Println("Turn 1 hash:", hash)
//	}
//
// # Limitations
//
//   - Returns empty string for non-existent turns
//
// # Assumptions
//
//   - TurnHashes map is initialized
func (i *IntegrityInfo) GetTurnHash(turnNumber int) (string, bool) {
	hash, ok := i.TurnHashes[turnNumber]
	return hash, ok
}

// GetSourceHash returns the hash for a specific source.
//
// # Description
//
// Retrieves the previously stored hash for a source document.
//
// # Inputs
//
//   - sourceName: The source identifier
//
// # Outputs
//
//   - string: The source hash, or empty string if not found
//   - bool: True if the source hash exists
//
// # Examples
//
//	hash, ok := info.GetSourceHash("document.pdf")
//	if ok {
//	    fmt.Println("Document hash:", hash)
//	}
//
// # Limitations
//
//   - Returns empty string for non-existent sources
//
// # Assumptions
//
//   - SourceHashes map is initialized
func (i *IntegrityInfo) GetSourceHash(sourceName string) (string, bool) {
	hash, ok := i.SourceHashes[sourceName]
	return hash, ok
}

// =============================================================================
// quickChainVerifier Methods
// =============================================================================

// Verify checks the chain by verifying PrevHash links only.
//
// # Description
//
// Walks through the events and verifies that each event's PrevHash
// matches the previous event's Hash. Does NOT recompute hashes.
//
// # Inputs
//
//   - events: Ordered list of stream events from the session
//
// # Outputs
//
//   - *ChainVerificationResult: Detailed verification results
//
// # Examples
//
//	verifier := NewQuickChainVerifier()
//	events := []StreamEvent{
//	    {Hash: "abc", PrevHash: ""},
//	    {Hash: "def", PrevHash: "abc"},
//	}
//	result := verifier.Verify(events)
//	// result.Valid == true
//
// # Limitations
//
//   - Does not detect content modification if Hash is also modified
//   - Trusts that Hash fields were computed correctly
//
// # Assumptions
//
//   - Events are in chronological order
//   - First event has empty PrevHash
func (v *quickChainVerifier) Verify(events []StreamEvent) *ChainVerificationResult {
	result := &ChainVerificationResult{
		Valid:             true,
		ChainLength:       len(events),
		InvalidEventIndex: -1,
	}

	if len(events) == 0 {
		return result
	}

	// First event should have empty PrevHash
	if events[0].PrevHash != "" {
		result.Valid = false
		result.InvalidEventIndex = 0
		result.ExpectedHash = ""
		result.ActualHash = events[0].PrevHash
		result.ErrorMessage = "first event should have empty PrevHash"
		return result
	}

	// Walk the chain verifying PrevHash links
	for i := 1; i < len(events); i++ {
		expectedPrevHash := events[i-1].Hash
		actualPrevHash := events[i].PrevHash

		if actualPrevHash != expectedPrevHash {
			result.Valid = false
			result.InvalidEventIndex = i
			result.ExpectedHash = expectedPrevHash
			result.ActualHash = actualPrevHash
			result.ErrorMessage = fmt.Sprintf(
				"chain broken at event %d: expected PrevHash %s, got %s",
				i, truncateHash(expectedPrevHash), truncateHash(actualPrevHash),
			)
			return result
		}
	}

	result.FinalHash = events[len(events)-1].Hash
	return result
}

// =============================================================================
// fullChainVerifier Methods
// =============================================================================

// Verify fully verifies the chain by recomputing all hashes.
//
// # Description
//
// Performs complete verification by:
//  1. Checking first event has empty PrevHash
//  2. Verifying each event's PrevHash matches previous event's Hash
//  3. Recomputing each event's hash from content
//  4. Verifying computed hash matches stored Hash
//
// # Inputs
//
//   - events: Ordered list of stream events from the session
//
// # Outputs
//
//   - *ChainVerificationResult: Detailed verification results
//
// # Examples
//
//	verifier := NewFullChainVerifier()
//	events := loadEventsFromStorage(sessionID)
//	result := verifier.Verify(events)
//	if !result.Valid {
//	    log.Warn("tampering detected", "error", result.ErrorMessage)
//	}
//
// # Limitations
//
//   - Computationally expensive for large event chains
//   - Requires access to original event content
//
// # Assumptions
//
//   - Events contain valid Content and CreatedAt fields
//   - Events are in chronological order
func (v *fullChainVerifier) Verify(events []StreamEvent) *ChainVerificationResult {
	result := &ChainVerificationResult{
		Valid:             true,
		ChainLength:       len(events),
		InvalidEventIndex: -1,
	}

	if len(events) == 0 {
		return result
	}

	// First event should have empty PrevHash
	if events[0].PrevHash != "" {
		result.Valid = false
		result.InvalidEventIndex = 0
		result.ExpectedHash = ""
		result.ActualHash = events[0].PrevHash
		result.ErrorMessage = "first event should have empty PrevHash"
		return result
	}

	// Walk the chain verifying both hash computation and chain links
	prevHash := ""
	for i, event := range events {
		// Verify PrevHash links correctly
		if event.PrevHash != prevHash {
			result.Valid = false
			result.InvalidEventIndex = i
			result.ExpectedHash = prevHash
			result.ActualHash = event.PrevHash
			result.ErrorMessage = fmt.Sprintf(
				"chain broken at event %d: expected PrevHash %s, got %s",
				i, truncateHash(prevHash), truncateHash(event.PrevHash),
			)
			return result
		}

		// Recompute hash from content
		computedHash := v.hashComputer.ComputeEventHash(
			event.Content, event.CreatedAt, event.PrevHash,
		)
		if computedHash != event.Hash {
			result.Valid = false
			result.InvalidEventIndex = i
			result.ExpectedHash = computedHash
			result.ActualHash = event.Hash
			result.ErrorMessage = fmt.Sprintf(
				"hash mismatch at event %d: computed %s, stored %s (content may have been modified)",
				i, truncateHash(computedHash), truncateHash(event.Hash),
			)
			return result
		}

		prevHash = event.Hash
	}

	result.FinalHash = events[len(events)-1].Hash
	return result
}

// =============================================================================
// sha256HashComputer Methods
// =============================================================================

// ComputeEventHash computes the SHA-256 hash for a stream event.
//
// # Description
//
// Computes the hash using the formula: SHA256(Content || CreatedAt || PrevHash)
// This matches the hash computation performed server-side.
//
// # Inputs
//
//   - content: The event content (token text, error message, etc.)
//   - createdAt: The event creation timestamp (Unix milliseconds)
//   - prevHash: The hash of the previous event (empty for first event)
//
// # Outputs
//
//   - string: 64-character lowercase hexadecimal hash
//
// # Examples
//
//	computer := NewSHA256HashComputer()
//	hash := computer.ComputeEventHash("Hello", 1735657200000, "abc123...")
//	// Returns: "7b8c9d0e1f2a..."
//
// # Limitations
//
//   - Format must match server-side computation exactly
//
// # Assumptions
//
//   - Inputs are valid strings/integers
func (c *sha256HashComputer) ComputeEventHash(content string, createdAt int64, prevHash string) string {
	data := fmt.Sprintf("%s%d%s", content, createdAt, prevHash)
	hash := sha256.Sum256([]byte(data))
	return hex.EncodeToString(hash[:])
}

// ComputeContentHash computes the SHA-256 hash of content.
//
// # Description
//
// Simple SHA-256 hash of the provided content string.
// Used for content integrity verification.
//
// # Inputs
//
//   - content: The content to hash
//
// # Outputs
//
//   - string: 64-character lowercase hexadecimal hash
//
// # Examples
//
//	computer := NewSHA256HashComputer()
//	hash := computer.ComputeContentHash("The answer is 42.")
//	// Returns: "abc123..."
//
// # Limitations
//
//   - Empty content produces a valid hash (not an error)
//
// # Assumptions
//
//   - Content is valid UTF-8
func (c *sha256HashComputer) ComputeContentHash(content string) string {
	hash := sha256.Sum256([]byte(content))
	return hex.EncodeToString(hash[:])
}

// =============================================================================
// Helper Functions
// =============================================================================

// truncateHash returns a truncated hash for display in error messages.
//
// # Description
//
// Shows first 8 and last 4 characters with "..." in between.
// Full 64-char hashes are unwieldy in error messages.
//
// # Inputs
//
//   - hash: The full hash string
//
// # Outputs
//
//   - string: Truncated hash for display
//
// # Examples
//
//	short := truncateHash("abc123def456abc123def456abc123def456abc123def456abc123def456abc1")
//	// Returns: "abc123de...abc1"
//
// # Limitations
//
//   - Returns original string if <= 16 characters
//
// # Assumptions
//
//   - Input is a valid hex string or empty
func truncateHash(hash string) string {
	if len(hash) <= 16 {
		return hash
	}
	return hash[:8] + "..." + hash[len(hash)-4:]
}
