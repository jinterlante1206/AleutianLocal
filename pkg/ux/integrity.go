// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

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
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"time"
)

// secureHashEqual performs constant-time comparison of two hash strings.
// This prevents timing attacks where an attacker could determine how many
// leading characters of a hash are correct by measuring response times.
func secureHashEqual(a, b string) bool {
	// subtle.ConstantTimeCompare returns 1 if equal, 0 if not
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// =============================================================================
// Interfaces
// =============================================================================

// -----------------------------------------------------------------------------
// Enterprise Extension Points
// -----------------------------------------------------------------------------
//
// The following interfaces are designed for enterprise deployments requiring
// enhanced security, compliance, and audit capabilities. Implementations are
// NOT included in the open-source release.
//
// Extension interfaces:
//   - KeyedHashComputer: HMAC-based verification with key management
//   - SignatureVerifier: Digital signature verification (RSA, ECDSA)
//   - TimestampAuthority: RFC 3161 trusted timestamping
//   - HSMProvider: Hardware Security Module integration
//   - AuditLogger: Compliance audit trail logging
//   - VerificationAuthorizer: Access control for verification operations
//
// To implement enterprise features, create implementations of these interfaces
// and inject them via constructor functions.
// -----------------------------------------------------------------------------

// KeyedHashComputer computes keyed hashes (HMAC) for enhanced security.
//
// # Description
//
// Enterprise extension for HMAC-based verification. Unlike simple SHA-256,
// HMAC requires a secret key, providing authentication in addition to
// integrity verification.
//
// # Thread Safety
//
// Implementations must be safe for concurrent use.
//
// # Enterprise Use Cases
//
//   - Multi-tenant environments requiring tenant-specific keys
//   - Regulatory compliance requiring keyed verification (FIPS 140-2)
//   - Non-repudiation with organizational keys
type KeyedHashComputer interface {
	// ComputeHMAC computes a keyed hash for content.
	//
	// # Inputs
	//
	//   - keyID: Identifier for the key to use (for key rotation)
	//   - content: Content to hash
	//
	// # Outputs
	//
	//   - string: Hex-encoded HMAC
	//   - error: Non-nil if key not found or HSM unavailable
	ComputeHMAC(keyID string, content string) (string, error)

	// VerifyHMAC verifies a keyed hash.
	//
	// # Inputs
	//
	//   - keyID: Identifier for the key used
	//   - content: Original content
	//   - expectedHMAC: HMAC to verify against
	//
	// # Outputs
	//
	//   - bool: True if HMAC matches
	//   - error: Non-nil if verification could not be performed
	VerifyHMAC(keyID string, content string, expectedHMAC string) (bool, error)
}

// SignatureVerifier verifies digital signatures on content.
//
// # Description
//
// Enterprise extension for cryptographic signature verification.
// Supports RSA, ECDSA, and Ed25519 signatures for non-repudiation.
//
// # Thread Safety
//
// Implementations must be safe for concurrent use.
//
// # Enterprise Use Cases
//
//   - Legal non-repudiation requirements
//   - Regulatory compliance (eIDAS, ESIGN)
//   - Multi-party verification
type SignatureVerifier interface {
	// VerifySignature verifies a digital signature.
	//
	// # Inputs
	//
	//   - content: Content that was signed
	//   - signature: Base64-encoded signature
	//   - signerID: Identifier for the signer's public key
	//
	// # Outputs
	//
	//   - bool: True if signature is valid
	//   - error: Non-nil if verification could not be performed
	VerifySignature(content string, signature string, signerID string) (bool, error)
}

// TimestampAuthority provides trusted timestamping services.
//
// # Description
//
// Enterprise extension for RFC 3161 trusted timestamps.
// Proves that content existed at a specific point in time.
//
// # Thread Safety
//
// Implementations must be safe for concurrent use.
//
// # Enterprise Use Cases
//
//   - Legal evidence timestamping
//   - Regulatory compliance (MiFID II, SOX)
//   - Audit trail integrity
type TimestampAuthority interface {
	// GetTimestamp requests a trusted timestamp for content hash.
	//
	// # Inputs
	//
	//   - contentHash: Hash of content to timestamp
	//
	// # Outputs
	//
	//   - TimestampToken: RFC 3161 timestamp token
	//   - error: Non-nil if TSA unavailable
	GetTimestamp(contentHash string) (*TimestampToken, error)

	// VerifyTimestamp verifies a timestamp token.
	//
	// # Inputs
	//
	//   - token: Previously obtained timestamp token
	//   - contentHash: Hash to verify against
	//
	// # Outputs
	//
	//   - bool: True if timestamp is valid
	//   - error: Non-nil if verification failed
	VerifyTimestamp(token *TimestampToken, contentHash string) (bool, error)
}

// TimestampToken represents an RFC 3161 timestamp token.
//
// # Description
//
// Contains the timestamp response from a Timestamp Authority.
// Used for proving content existed at a specific time.
type TimestampToken struct {
	// Token is the DER-encoded timestamp token
	Token []byte `json:"token"`

	// Timestamp is the time asserted by the TSA
	Timestamp time.Time `json:"timestamp"`

	// TSAName is the name of the Timestamp Authority
	TSAName string `json:"tsa_name"`

	// SerialNumber is the unique token serial number
	SerialNumber string `json:"serial_number"`
}

// HSMProvider provides Hardware Security Module integration.
//
// # Description
//
// Enterprise extension for PKCS#11 HSM integration.
// Keys never leave the HSM, providing highest security level.
//
// # Thread Safety
//
// Implementations must be safe for concurrent use.
//
// # Enterprise Use Cases
//
//   - FIPS 140-2 Level 3 compliance
//   - PCI-DSS key management
//   - Government security requirements
type HSMProvider interface {
	// SignWithHSM signs content using an HSM-stored key.
	//
	// # Inputs
	//
	//   - keyLabel: Label of the key in HSM
	//   - content: Content to sign
	//
	// # Outputs
	//
	//   - []byte: Signature bytes
	//   - error: Non-nil if HSM operation failed
	SignWithHSM(keyLabel string, content []byte) ([]byte, error)

	// VerifyWithHSM verifies a signature using HSM.
	//
	// # Inputs
	//
	//   - keyLabel: Label of the public key in HSM
	//   - content: Original content
	//   - signature: Signature to verify
	//
	// # Outputs
	//
	//   - bool: True if signature valid
	//   - error: Non-nil if HSM operation failed
	VerifyWithHSM(keyLabel string, content []byte, signature []byte) (bool, error)
}

// AuditLogger logs verification events for compliance.
//
// # Description
//
// Enterprise extension for compliance audit logging.
// All verification attempts are logged with full context.
//
// # Thread Safety
//
// Implementations must be safe for concurrent use.
//
// # Enterprise Use Cases
//
//   - SOC 2 audit requirements
//   - GDPR access logging
//   - HIPAA audit trails
type AuditLogger interface {
	// LogVerificationAttempt logs a verification attempt.
	//
	// # Inputs
	//
	//   - event: Verification event details
	//
	// # Outputs
	//
	//   - error: Non-nil if logging failed (should not block verification)
	LogVerificationAttempt(event *VerificationAuditEvent) error
}

// VerificationAuditEvent contains details for audit logging.
//
// # Description
//
// Captures all relevant information about a verification attempt
// for compliance audit trails.
type VerificationAuditEvent struct {
	// SessionID being verified
	SessionID string `json:"session_id"`

	// UserID who requested verification (from auth context)
	UserID string `json:"user_id"`

	// TenantID for multi-tenant deployments
	TenantID string `json:"tenant_id"`

	// Timestamp of verification attempt
	Timestamp time.Time `json:"timestamp"`

	// Success indicates if verification passed
	Success bool `json:"success"`

	// FailureReason if verification failed
	FailureReason string `json:"failure_reason,omitempty"`

	// IPAddress of requester
	IPAddress string `json:"ip_address"`

	// RequestID for correlation
	RequestID string `json:"request_id"`
}

// VerificationAuthorizer checks authorization for verification operations.
//
// # Description
//
// Enterprise extension for access control on verification.
// Ensures users can only verify sessions they have access to.
//
// # Thread Safety
//
// Implementations must be safe for concurrent use.
//
// # Enterprise Use Cases
//
//   - Multi-tenant session isolation
//   - Role-based access control
//   - Data sovereignty compliance
type VerificationAuthorizer interface {
	// CanVerify checks if a user can verify a session.
	//
	// # Inputs
	//
	//   - userID: User requesting verification
	//   - sessionID: Session to verify
	//
	// # Outputs
	//
	//   - bool: True if authorized
	//   - error: Non-nil if authorization check failed
	CanVerify(userID string, sessionID string) (bool, error)
}

// -----------------------------------------------------------------------------
// Core Interfaces (Open Source)
// -----------------------------------------------------------------------------

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
	//   verifier := NewFullChainVerifier(hashComputer)
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
//	verifier := NewFullChainVerifier(hashComputer)
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
		// Verify PrevHash links correctly (constant-time comparison to prevent timing attacks)
		if !secureHashEqual(event.PrevHash, prevHash) {
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
		// Constant-time comparison to prevent timing attacks
		if !secureHashEqual(computedHash, event.Hash) {
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
	// Use null byte delimiter to prevent collision attacks where different inputs
	// produce the same concatenated string (e.g., "abc"+123 vs "abc1"+23)
	data := fmt.Sprintf("%s\x00%d\x00%s", content, createdAt, prevHash)
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
