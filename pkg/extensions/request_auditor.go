// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package extensions

import (
	"context"
	"time"
)

// =============================================================================
// Raw Capture Types (for Enterprise storage)
// =============================================================================

// HTTPHeaders represents HTTP headers as a map.
//
// Using a defined type provides clearer intent and allows future extension
// with helper methods if needed.
type HTTPHeaders map[string]string

// Get retrieves a header value by key (case-sensitive).
func (h HTTPHeaders) Get(key string) string {
	return h[key]
}

// Set adds or updates a header value.
func (h HTTPHeaders) Set(key, value string) {
	h[key] = value
}

// AuditableRequest contains raw request data for audit capture.
//
// This type is passed to CaptureRequest() to give Enterprise implementations
// access to the raw bytes for hashing, encryption, and storage. FOSS does
// NOT compute hashes - that's Enterprise's responsibility.
//
// # Usage
//
// Handlers create this struct with the raw request body and pass it to
// the RequestAuditor. Enterprise implementations then:
//  1. Compute content_hash = SHA256(Body)
//  2. Encrypt the body if required
//  3. Store to immutable storage (GCS, QLDB, etc.)
//
// Example:
//
//	req := &AuditableRequest{
//	    Method:    "POST",
//	    Path:      "/v1/chat/direct",
//	    Headers:   HTTPHeaders{"Content-Type": "application/json"},
//	    Body:      rawRequestBytes,
//	    UserID:    authInfo.UserID,
//	    SessionID: sessionID,
//	    RequestID: requestID,
//	    Timestamp: time.Now().UTC(),
//	}
//	auditID, err := auditor.CaptureRequest(ctx, req)
type AuditableRequest struct {
	// Method is the HTTP method (GET, POST, etc.)
	Method string

	// Path is the request path (e.g., "/v1/chat/direct")
	Path string

	// Headers contains the HTTP request headers.
	// Sensitive headers (Authorization) should be redacted by caller.
	Headers HTTPHeaders

	// Body is the raw request body bytes.
	// This is what Enterprise will hash and potentially encrypt.
	Body []byte

	// UserID identifies who made the request.
	// Extracted from AuthInfo by the handler.
	UserID string

	// SessionID is the conversation session identifier (if applicable).
	SessionID string

	// RequestID is the unique identifier for this request.
	RequestID string

	// Timestamp is when the request was received (always UTC).
	Timestamp time.Time
}

// AuditableResponse contains raw response data for audit capture.
//
// This type is passed to CaptureResponse() to complete the audit record.
// The auditID from CaptureRequest() links the request and response together.
//
// # Streaming Responses
//
// For streaming endpoints (SSE), the handler should accumulate all chunks
// and pass the concatenated body to CaptureResponse() at the end of the stream.
//
// Example:
//
//	resp := &AuditableResponse{
//	    StatusCode: 200,
//	    Headers:    HTTPHeaders{"Content-Type": "application/json"},
//	    Body:       responseBytes,
//	    Timestamp:  time.Now().UTC(),
//	}
//	err := auditor.CaptureResponse(ctx, auditID, resp)
type AuditableResponse struct {
	// StatusCode is the HTTP response status code.
	StatusCode int

	// Headers contains the HTTP response headers.
	Headers HTTPHeaders

	// Body is the raw response body bytes.
	// For streaming responses, this is all chunks concatenated.
	Body []byte

	// Timestamp is when the response was sent (always UTC).
	Timestamp time.Time
}

// =============================================================================
// Hash Chain Types (for FOSS local use)
// =============================================================================

// HashChainEntry represents a single entry in a tamper-evident audit chain.
//
// Hash chains provide cryptographic proof of the order and integrity of events.
// Each entry's hash incorporates the previous entry's hash, creating a chain
// that detects any modification to historical records.
//
// # Chain Structure
//
// Entry N hash = SHA256(Entry N-1 hash + Entry N content)
//
// This ensures:
//   - Insertion detection: Adding entries breaks the chain
//   - Deletion detection: Removing entries breaks the chain
//   - Modification detection: Changing entries breaks the chain
//
// Example:
//
//	entry := HashChainEntry{
//	    SessionID:    "sess-123",
//	    SequenceNum:  5,
//	    ContentHash:  "abc123...",
//	    PreviousHash: "def456...",
//	    ChainHash:    "ghi789...",
//	    Timestamp:    time.Now().UTC(),
//	    ContentType:  "conversation_turn",
//	    Metadata: NewMetadata().
//	        Set("user_id", "user-456").
//	        Set("request_id", "req-789"),
//	}
type HashChainEntry struct {
	// SessionID identifies the chain this entry belongs to.
	// Each session has its own independent hash chain.
	SessionID string

	// SequenceNum is the position in the chain (1-indexed).
	// Used to verify chain completeness and ordering.
	SequenceNum int

	// ContentHash is the hash of the content being recorded.
	// For conversation turns: SHA256(question + answer)
	// For requests: SHA256(request body)
	ContentHash string

	// PreviousHash is the ChainHash of the preceding entry.
	// Empty string for the first entry in a chain (SequenceNum == 1).
	PreviousHash string

	// ChainHash is the cumulative hash incorporating all previous entries.
	// ChainHash = SHA256(PreviousHash + ContentHash)
	// This is the value stored and used for verification.
	ChainHash string

	// Timestamp is when this entry was created (always UTC).
	Timestamp time.Time

	// ContentType describes what kind of content was hashed.
	// Examples: "conversation_turn", "request", "response", "document"
	ContentType string

	// Metadata contains additional context about the entry.
	// May include: user_id, request_id, turn_number, etc.
	//
	// Use NewMetadata() and type-safe accessors:
	//
	//   Metadata: NewMetadata().
	//       Set("user_id", userID).
	//       Set("request_id", requestID),
	Metadata Metadata
}

// ChainVerificationResult contains the outcome of hash chain verification.
//
// Example:
//
//	result := auditor.VerifyChain(ctx, sessionID)
//	if !result.IsValid {
//	    log.Error("chain integrity violation",
//	        "break_point", result.BreakPoint,
//	        "expected", result.ExpectedHash,
//	        "actual", result.ActualHash,
//	    )
//	}
type ChainVerificationResult struct {
	// IsValid is true if the entire chain is intact.
	IsValid bool

	// TotalEntries is the number of entries verified.
	TotalEntries int

	// BreakPoint is the sequence number where integrity failed.
	// Only meaningful when IsValid is false.
	// Zero means the chain is valid or empty.
	BreakPoint int

	// ExpectedHash is what the hash should be at BreakPoint.
	ExpectedHash string

	// ActualHash is what the hash actually was at BreakPoint.
	ActualHash string

	// Message provides human-readable verification status.
	Message string
}

// =============================================================================
// RequestAuditor Interface
// =============================================================================

// RequestAuditor provides tamper-evident audit logging via hash chains.
//
// Implementations must be safe for concurrent use by multiple goroutines.
//
// # Open Source Behavior
//
// The default NopRequestAuditor accepts all entries and always reports
// chains as valid. This allows the local CLI to function without
// cryptographic audit infrastructure.
//
// # Enterprise Implementation
//
// Enterprise versions implement persistent hash chains stored in:
//   - Append-only databases (e.g., Amazon QLDB)
//   - Immutable storage (e.g., S3 Object Lock)
//   - Blockchain-based ledgers
//   - Hardware security modules (HSMs)
//
// Example enterprise implementation:
//
//	type QLDBRequestAuditor struct {
//	    ledger *qldb.Driver
//	}
//
//	func (a *QLDBRequestAuditor) RecordEntry(ctx context.Context, entry HashChainEntry) error {
//	    // Verify chain continuity
//	    lastHash, err := a.getLastHash(ctx, entry.SessionID)
//	    if err != nil {
//	        return err
//	    }
//	    if entry.PreviousHash != lastHash {
//	        return errors.New("chain continuity violation")
//	    }
//	    // Persist to QLDB (immutable)
//	    return a.ledger.Execute(ctx, func(txn qldb.Transaction) error {
//	        return txn.Insert("audit_chain", entry)
//	    })
//	}
//
// # Usage
//
// Record entries when processing requests:
//
//	// After computing response hash
//	entry := HashChainEntry{
//	    SessionID:   sessionID,
//	    SequenceNum: turnNumber,
//	    ContentHash: responseHash,
//	    // PreviousHash filled by implementation or caller
//	    Timestamp:   time.Now().UTC(),
//	    ContentType: "conversation_turn",
//	    Metadata: NewMetadata().
//	        Set("user_id", userID).
//	        Set("request_id", requestID),
//	}
//	if err := auditor.RecordEntry(ctx, entry); err != nil {
//	    log.Error("audit recording failed", "error", err)
//	    // Consider failing the request for compliance
//	}
//
// # Regulatory Compliance
//
// Hash chains support:
//   - HIPAA: Audit controls (§164.312(b))
//   - SOX: Internal controls over financial reporting
//   - GDPR: Accountability and records of processing
//   - PCI DSS: Logging and monitoring (Requirement 10)
//
// # Limitations
//
//   - Cannot prevent real-time tampering (only detect after the fact)
//   - Chain verification requires all entries (no partial verification)
//   - Storage grows linearly with entries
//
// # Assumptions
//
//   - Clock synchronization across nodes (for timestamp ordering)
//   - SHA256 is collision-resistant (standard assumption)
//   - Enterprise storage is truly append-only
type RequestAuditor interface {
	// =========================================================================
	// Raw Capture Methods (Primary - for Enterprise)
	// =========================================================================

	// CaptureRequest records the raw request for audit purposes.
	//
	// # Description
	//
	// Called at the START of request processing with the raw request body.
	// Enterprise implementations receive the raw bytes to:
	//   1. Compute content_hash = SHA256(Body)
	//   2. Encrypt the body if required
	//   3. Store to immutable storage (GCS, QLDB, etc.)
	//
	// Returns an auditID that must be passed to CaptureResponse to link them.
	//
	// # Inputs
	//
	//   - ctx: Context for cancellation and timeout control.
	//   - req: Raw request data including body bytes.
	//
	// # Outputs
	//
	//   - string: Audit ID to pass to CaptureResponse. Empty for NopRequestAuditor.
	//   - error: Non-nil if capture failed.
	//
	// # Examples
	//
	//   auditID, err := auditor.CaptureRequest(ctx, &AuditableRequest{
	//       Method:    c.Request.Method,
	//       Path:      c.Request.URL.Path,
	//       Body:      requestBody,
	//       UserID:    authInfo.UserID,
	//       RequestID: req.RequestID,
	//       Timestamp: time.Now().UTC(),
	//   })
	//   if err != nil {
	//       log.Error("audit capture failed", "error", err)
	//   }
	//   // ... process request ...
	//   auditor.CaptureResponse(ctx, auditID, responseData)
	//
	// # Limitations
	//
	//   - Caller must preserve auditID to call CaptureResponse.
	//   - Request body should be read before calling (use io.TeeReader).
	//
	// # Assumptions
	//
	//   - Body contains the complete request payload.
	//   - Sensitive headers are redacted by caller if needed.
	//
	// # Thread Safety
	//
	// Safe to call concurrently.
	CaptureRequest(ctx context.Context, req *AuditableRequest) (auditID string, err error)

	// CaptureResponse records the raw response for audit purposes.
	//
	// # Description
	//
	// Called at the END of request processing with the raw response body.
	// The auditID links this response to its corresponding request.
	// Enterprise implementations receive the raw bytes to hash and store.
	//
	// For streaming endpoints, accumulate all chunks and call this once at the end.
	//
	// # Inputs
	//
	//   - ctx: Context for cancellation and timeout control.
	//   - auditID: The ID returned from CaptureRequest.
	//   - resp: Raw response data including body bytes.
	//
	// # Outputs
	//
	//   - error: Non-nil if capture failed.
	//
	// # Examples
	//
	//   err := auditor.CaptureResponse(ctx, auditID, &AuditableResponse{
	//       StatusCode: 200,
	//       Body:       responseBytes,
	//       Timestamp:  time.Now().UTC(),
	//   })
	//
	// # Limitations
	//
	//   - Must be called with valid auditID from CaptureRequest.
	//   - For streaming, caller must accumulate all chunks.
	//
	// # Assumptions
	//
	//   - Body contains the complete response payload.
	//   - auditID is valid and corresponds to a captured request.
	//
	// # Thread Safety
	//
	// Safe to call concurrently.
	CaptureResponse(ctx context.Context, auditID string, resp *AuditableResponse) error

	// =========================================================================
	// Hash Chain Methods (Secondary - for FOSS local advanced use)
	// =========================================================================

	// RecordEntry adds a new entry to the hash chain.
	//
	// # Description
	//
	// Persists an audit entry and updates the chain hash. Implementations
	// should verify chain continuity before accepting the entry.
	//
	// # Inputs
	//
	//   - ctx: Context for cancellation and timeout control.
	//   - entry: The hash chain entry to record.
	//
	// # Outputs
	//
	//   - error: Non-nil if recording failed or chain continuity was violated.
	//
	// # Examples
	//
	//   err := auditor.RecordEntry(ctx, HashChainEntry{
	//       SessionID:   "sess-123",
	//       SequenceNum: 1,
	//       ContentHash: "abc...",
	//       ContentType: "conversation_turn",
	//   })
	//
	// # Limitations
	//
	//   - Entry cannot be modified after recording.
	//   - Requires PreviousHash for SequenceNum > 1.
	//
	// # Assumptions
	//
	//   - ContentHash is correctly computed by caller.
	//   - SequenceNum is monotonically increasing per session.
	//
	// # Thread Safety
	//
	// Safe to call concurrently, but entries for the same session
	// should be serialized to maintain chain order.
	RecordEntry(ctx context.Context, entry HashChainEntry) error

	// GetLastEntry retrieves the most recent entry for a session.
	//
	// # Description
	//
	// Returns the last recorded entry, which is needed to compute
	// PreviousHash for the next entry in the chain.
	//
	// # Inputs
	//
	//   - ctx: Context for cancellation and timeout control.
	//   - sessionID: The session to query.
	//
	// # Outputs
	//
	//   - *HashChainEntry: The last entry, or nil if chain is empty.
	//   - error: Non-nil if retrieval failed.
	//
	// # Examples
	//
	//   lastEntry, err := auditor.GetLastEntry(ctx, sessionID)
	//   if lastEntry != nil {
	//       newEntry.PreviousHash = lastEntry.ChainHash
	//       newEntry.SequenceNum = lastEntry.SequenceNum + 1
	//   } else {
	//       newEntry.PreviousHash = ""
	//       newEntry.SequenceNum = 1
	//   }
	//
	// # Limitations
	//
	//   - Returns nil for non-existent sessions (not an error).
	//
	// # Assumptions
	//
	//   - Session IDs are unique across the system.
	//
	// # Thread Safety
	//
	// Safe to call concurrently.
	GetLastEntry(ctx context.Context, sessionID string) (*HashChainEntry, error)

	// VerifyChain validates the integrity of a session's hash chain.
	//
	// # Description
	//
	// Retrieves all entries for a session and verifies that each entry's
	// ChainHash correctly incorporates the previous entry's hash.
	//
	// # Inputs
	//
	//   - ctx: Context for cancellation and timeout control.
	//   - sessionID: The session to verify.
	//
	// # Outputs
	//
	//   - *ChainVerificationResult: Verification outcome with details.
	//   - error: Non-nil if verification could not be performed.
	//
	// # Examples
	//
	//   result, err := auditor.VerifyChain(ctx, sessionID)
	//   if err != nil {
	//       return fmt.Errorf("verification failed: %w", err)
	//   }
	//   if !result.IsValid {
	//       alertSecurityTeam(sessionID, result.BreakPoint)
	//   }
	//
	// # Limitations
	//
	//   - Requires loading all entries (may be slow for long chains).
	//   - Empty chains are considered valid.
	//
	// # Assumptions
	//
	//   - All entries for the session are retrievable.
	//   - Hash algorithm matches what was used during recording.
	//
	// # Thread Safety
	//
	// Safe to call concurrently.
	VerifyChain(ctx context.Context, sessionID string) (*ChainVerificationResult, error)

	// GetChainLength returns the number of entries in a session's chain.
	//
	// # Description
	//
	// Returns the count of recorded entries without loading them all.
	// Useful for quick checks and display purposes.
	//
	// # Inputs
	//
	//   - ctx: Context for cancellation and timeout control.
	//   - sessionID: The session to query.
	//
	// # Outputs
	//
	//   - int: Number of entries (0 for non-existent sessions).
	//   - error: Non-nil if count could not be retrieved.
	//
	// # Examples
	//
	//   count, err := auditor.GetChainLength(ctx, sessionID)
	//   fmt.Printf("Session has %d recorded turns\n", count)
	//
	// # Limitations
	//
	//   - Does not verify chain integrity.
	//
	// # Assumptions
	//
	//   - Count is consistent with actual entries.
	//
	// # Thread Safety
	//
	// Safe to call concurrently.
	GetChainLength(ctx context.Context, sessionID string) (int, error)
}

// =============================================================================
// No-Op Implementation
// =============================================================================

// NopRequestAuditor is the default auditor for open source.
//
// It accepts all operations without persisting anything. This allows
// the CLI to function without cryptographic audit infrastructure.
// Enterprise implementations replace this with actual storage.
//
// Thread-safe: This implementation has no mutable state (discards everything).
//
// Example:
//
//	auditor := &NopRequestAuditor{}
//	auditID, _ := auditor.CaptureRequest(ctx, req)
//	// auditID == "" (no tracking)
//	auditor.CaptureResponse(ctx, auditID, resp)
//	// No-op, nothing stored
type NopRequestAuditor struct{}

// CaptureRequest accepts the request without storing it.
//
// # Description
//
// Always succeeds and returns empty auditID. This is intentional for
// local deployments without audit requirements. Enterprise implementations
// would store the request and return a tracking ID.
//
// # Inputs
//
//   - ctx: Ignored (no external calls).
//   - req: Ignored (not stored).
//
// # Outputs
//
//   - string: Always empty string (no tracking).
//   - error: Always nil.
//
// # Examples
//
//	auditID, err := auditor.CaptureRequest(ctx, req)
//	// auditID == ""
//	// err == nil
//
// # Limitations
//
//   - Does not store request data.
//   - No audit trail maintained.
//
// # Assumptions
//
//   - Caller accepts that no audit is performed.
//
// # Thread Safety
//
// Safe to call concurrently (stateless).
func (a *NopRequestAuditor) CaptureRequest(_ context.Context, _ *AuditableRequest) (string, error) {
	return "", nil
}

// CaptureResponse accepts the response without storing it.
//
// # Description
//
// Always succeeds without storing anything. This is intentional for
// local deployments without audit requirements.
//
// # Inputs
//
//   - ctx: Ignored (no external calls).
//   - auditID: Ignored (no tracking).
//   - resp: Ignored (not stored).
//
// # Outputs
//
//   - error: Always nil.
//
// # Examples
//
//	err := auditor.CaptureResponse(ctx, "", resp)
//	// err == nil
//
// # Limitations
//
//   - Does not store response data.
//
// # Assumptions
//
//   - Caller accepts that no audit is performed.
//
// # Thread Safety
//
// Safe to call concurrently (stateless).
func (a *NopRequestAuditor) CaptureResponse(_ context.Context, _ string, _ *AuditableResponse) error {
	return nil
}

// RecordEntry accepts the entry without persisting it.
//
// # Description
//
// Always succeeds without storing anything. This is intentional for
// local deployments without audit requirements.
//
// # Inputs
//
//   - ctx: Ignored (no external calls).
//   - entry: Ignored (not persisted).
//
// # Outputs
//
//   - error: Always nil.
//
// # Examples
//
//	err := auditor.RecordEntry(ctx, entry)
//	// err == nil
//
// # Limitations
//
//   - Does not persist entries.
//   - No tamper detection capability.
//
// # Assumptions
//
//   - Caller accepts that no audit trail is maintained.
//
// # Thread Safety
//
// Safe to call concurrently (stateless).
func (a *NopRequestAuditor) RecordEntry(_ context.Context, _ HashChainEntry) error {
	return nil
}

// GetLastEntry returns nil, indicating an empty chain.
//
// # Description
//
// Always returns nil since no entries are persisted.
//
// # Inputs
//
//   - ctx: Ignored (no external calls).
//   - sessionID: Ignored (no entries exist).
//
// # Outputs
//
//   - *HashChainEntry: Always nil.
//   - error: Always nil.
//
// # Examples
//
//	entry, err := auditor.GetLastEntry(ctx, "any-session")
//	// entry == nil
//	// err == nil
//
// # Limitations
//
//   - Always returns nil (no persistence).
//
// # Assumptions
//
//   - Caller handles nil entry appropriately.
//
// # Thread Safety
//
// Safe to call concurrently (stateless).
func (a *NopRequestAuditor) GetLastEntry(_ context.Context, _ string) (*HashChainEntry, error) {
	return nil, nil
}

// VerifyChain always returns valid.
//
// # Description
//
// Returns a valid result since no entries are tracked.
// This is intentional for local deployments.
//
// # Inputs
//
//   - ctx: Ignored (no external calls).
//   - sessionID: Ignored (no entries exist).
//
// # Outputs
//
//   - *ChainVerificationResult: Always valid with zero entries.
//   - error: Always nil.
//
// # Examples
//
//	result, err := auditor.VerifyChain(ctx, "any-session")
//	// result.IsValid == true
//	// result.TotalEntries == 0
//
// # Limitations
//
//   - No actual verification performed.
//
// # Assumptions
//
//   - Caller accepts that chains are not tracked.
//
// # Thread Safety
//
// Safe to call concurrently (stateless).
func (a *NopRequestAuditor) VerifyChain(_ context.Context, _ string) (*ChainVerificationResult, error) {
	return &ChainVerificationResult{
		IsValid:      true,
		TotalEntries: 0,
		Message:      "no audit entries (NopRequestAuditor)",
	}, nil
}

// GetChainLength always returns zero.
//
// # Description
//
// Returns zero since no entries are persisted.
//
// # Inputs
//
//   - ctx: Ignored (no external calls).
//   - sessionID: Ignored (no entries exist).
//
// # Outputs
//
//   - int: Always 0.
//   - error: Always nil.
//
// # Examples
//
//	count, err := auditor.GetChainLength(ctx, "any-session")
//	// count == 0
//	// err == nil
//
// # Limitations
//
//   - Always returns zero (no persistence).
//
// # Assumptions
//
//   - Caller accepts that chains are not tracked.
//
// # Thread Safety
//
// Safe to call concurrently (stateless).
func (a *NopRequestAuditor) GetChainLength(_ context.Context, _ string) (int, error) {
	return 0, nil
}

// =============================================================================
// Interface Compliance
// =============================================================================

// Compile-time interface compliance check.
var _ RequestAuditor = (*NopRequestAuditor)(nil)
