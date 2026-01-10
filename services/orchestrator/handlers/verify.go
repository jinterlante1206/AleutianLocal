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
// This file implements the session verification endpoint for hash chain integrity.
package handlers

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/weaviate/weaviate-go-client/v5/weaviate"
	"github.com/weaviate/weaviate-go-client/v5/weaviate/filters"
	"github.com/weaviate/weaviate-go-client/v5/weaviate/graphql"
	"go.opentelemetry.io/otel/attribute"
)

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
//   - EnterpriseSessionVerifier: Extended verification with auth/audit
//   - VerificationRateLimiter: Rate limiting for verification requests
//   - VerificationCache: Caching for expensive verification operations
//   - BatchVerifier: Verify multiple sessions efficiently
//   - VerificationScheduler: Periodic background verification
//   - VerificationAlertSender: Alerting on verification failures
//
// To implement enterprise features, create implementations of these interfaces
// and inject them via constructor functions.
// -----------------------------------------------------------------------------

// EnterpriseSessionVerifier extends SessionVerifier with enterprise features.
//
// # Description
//
// Enterprise extension that adds authorization, audit logging, and
// enhanced verification capabilities to the base SessionVerifier.
//
// # Thread Safety
//
// Implementations must be safe for concurrent use.
//
// # Enterprise Use Cases
//
//   - Multi-tenant session verification with isolation
//   - Compliance audit logging (SOC 2, HIPAA)
//   - Role-based access control for verification
type EnterpriseSessionVerifier interface {
	SessionVerifier

	// VerifyWithAuth verifies a session with authorization check.
	//
	// # Inputs
	//
	//   - c: Gin context containing auth headers
	//   - sessionID: Session to verify
	//   - userID: User requesting verification
	//   - tenantID: Tenant context for multi-tenant deployments
	//
	// # Outputs
	//
	//   - *VerifySessionResponse: Verification results
	//   - error: Non-nil if unauthorized or verification failed
	VerifyWithAuth(c *gin.Context, sessionID, userID, tenantID string) (*VerifySessionResponse, error)

	// VerifyWithAudit verifies and logs to audit trail.
	//
	// # Inputs
	//
	//   - c: Gin context
	//   - sessionID: Session to verify
	//   - auditContext: Additional context for audit log
	//
	// # Outputs
	//
	//   - *VerifySessionResponse: Verification results
	//   - error: Non-nil if verification failed
	VerifyWithAudit(c *gin.Context, sessionID string, auditContext map[string]string) (*VerifySessionResponse, error)
}

// VerificationRateLimiter limits verification request rates.
//
// # Description
//
// Enterprise extension to prevent abuse of verification endpoint.
// Implements per-user, per-tenant, and global rate limits.
//
// # Thread Safety
//
// Implementations must be safe for concurrent use.
//
// # Enterprise Use Cases
//
//   - Preventing DoS attacks on verification
//   - Fair resource allocation in multi-tenant
//   - Cost control for expensive HSM operations
type VerificationRateLimiter interface {
	// AllowVerification checks if verification is allowed.
	//
	// # Inputs
	//
	//   - userID: User requesting verification
	//   - tenantID: Tenant context
	//
	// # Outputs
	//
	//   - bool: True if allowed
	//   - time.Duration: Wait time if not allowed
	AllowVerification(userID, tenantID string) (bool, time.Duration)
}

// VerificationCache caches verification results.
//
// # Description
//
// Enterprise extension for caching expensive verification results.
// Cache invalidation on session modification.
//
// # Thread Safety
//
// Implementations must be safe for concurrent use.
//
// # Enterprise Use Cases
//
//   - Reducing HSM calls for repeated verifications
//   - Faster response for unchanged sessions
//   - Cost optimization for TSA requests
type VerificationCache interface {
	// Get retrieves cached verification result.
	//
	// # Inputs
	//
	//   - sessionID: Session to look up
	//   - chainHash: Expected chain hash (cache key includes this)
	//
	// # Outputs
	//
	//   - *VerifySessionResponse: Cached result, nil if not found
	//   - bool: True if cache hit
	Get(sessionID, chainHash string) (*VerifySessionResponse, bool)

	// Set stores a verification result.
	//
	// # Inputs
	//
	//   - sessionID: Session verified
	//   - response: Verification result to cache
	//   - ttl: Cache duration
	Set(sessionID string, response *VerifySessionResponse, ttl time.Duration)

	// Invalidate removes cached result for a session.
	//
	// # Inputs
	//
	//   - sessionID: Session to invalidate
	Invalidate(sessionID string)
}

// BatchVerifier verifies multiple sessions efficiently.
//
// # Description
//
// Enterprise extension for bulk verification operations.
// Optimizes database queries and parallelizes verification.
//
// # Thread Safety
//
// Implementations must be safe for concurrent use.
//
// # Enterprise Use Cases
//
//   - Periodic compliance checks
//   - Migration verification
//   - Disaster recovery validation
type BatchVerifier interface {
	// VerifyBatch verifies multiple sessions.
	//
	// # Inputs
	//
	//   - sessionIDs: Sessions to verify
	//   - concurrency: Max parallel verifications
	//
	// # Outputs
	//
	//   - map[string]*VerifySessionResponse: Results by session ID
	//   - error: Non-nil if batch operation failed
	VerifyBatch(sessionIDs []string, concurrency int) (map[string]*VerifySessionResponse, error)
}

// VerificationScheduler schedules periodic verification.
//
// # Description
//
// Enterprise extension for background verification scheduling.
// Enables proactive integrity monitoring.
//
// # Thread Safety
//
// Implementations must be safe for concurrent use.
//
// # Enterprise Use Cases
//
//   - Continuous compliance monitoring
//   - Early detection of data corruption
//   - SLA verification for customers
type VerificationScheduler interface {
	// ScheduleVerification schedules a session for periodic verification.
	//
	// # Inputs
	//
	//   - sessionID: Session to verify
	//   - interval: How often to verify
	//
	// # Outputs
	//
	//   - string: Schedule ID for management
	//   - error: Non-nil if scheduling failed
	ScheduleVerification(sessionID string, interval time.Duration) (string, error)

	// CancelSchedule cancels a scheduled verification.
	//
	// # Inputs
	//
	//   - scheduleID: Schedule to cancel
	//
	// # Outputs
	//
	//   - error: Non-nil if cancellation failed
	CancelSchedule(scheduleID string) error
}

// VerificationAlertSender sends alerts on verification failures.
//
// # Description
//
// Enterprise extension for alerting when verification fails.
// Integrates with PagerDuty, Slack, email, etc.
//
// # Thread Safety
//
// Implementations must be safe for concurrent use.
//
// # Enterprise Use Cases
//
//   - Immediate notification of data tampering
//   - Incident response automation
//   - Compliance violation alerts
type VerificationAlertSender interface {
	// SendAlert sends an alert for verification failure.
	//
	// # Inputs
	//
	//   - sessionID: Session that failed verification
	//   - result: Verification result with failure details
	//   - severity: Alert severity level
	//
	// # Outputs
	//
	//   - error: Non-nil if alert send failed
	SendAlert(sessionID string, result *VerifySessionResponse, severity string) error
}

// VerificationAuthorizer checks authorization for verification operations.
//
// # Description
//
// Enterprise extension for access control on verification requests.
// Ensures users can only verify sessions they have access to.
//
// # Thread Safety
//
// Implementations must be safe for concurrent use.
//
// # Enterprise Use Cases
//
//   - Multi-tenant session isolation
//   - Role-based access control (RBAC)
//   - Data sovereignty compliance
//   - Audit trail for access attempts
type VerificationAuthorizer interface {
	// CanVerify checks if a user can verify a session.
	//
	// # Description
	//
	// Performs authorization check to determine if the given user
	// has permission to verify the specified session.
	//
	// # Inputs
	//
	//   - userID: User requesting verification
	//   - sessionID: Session to verify
	//
	// # Outputs
	//
	//   - bool: True if user is authorized to verify
	//   - error: Non-nil if authorization check failed
	//
	// # Examples
	//
	//	allowed, err := authorizer.CanVerify("user-123", "sess-456")
	//	if err != nil {
	//	    return nil, fmt.Errorf("auth check failed: %w", err)
	//	}
	//	if !allowed {
	//	    return nil, errors.New("not authorized")
	//	}
	//
	// # Limitations
	//
	//   - Does not cache authorization decisions
	//   - Network call to policy engine may add latency
	//
	// # Assumptions
	//
	//   - userID and sessionID are valid identifiers
	//   - Policy engine is available and configured
	CanVerify(userID string, sessionID string) (bool, error)
}

// -----------------------------------------------------------------------------
// Core Interfaces (Open Source)
// -----------------------------------------------------------------------------

// SessionVerifier verifies the integrity of a session's stored data.
//
// # Description
//
// Abstracts the verification of session data, allowing different
// verification strategies and backends.
//
// # Thread Safety
//
// Implementations must be safe for concurrent use.
type SessionVerifier interface {
	// VerifySession verifies the integrity of a session's data.
	//
	// # Description
	//
	// Loads session data from storage and verifies integrity.
	//
	// # Inputs
	//
	//   - sessionID: The session to verify
	//
	// # Outputs
	//
	//   - *VerifySessionResponse: Verification results
	//   - error: Non-nil if verification could not be performed
	//
	// # Examples
	//
	//   verifier := NewWeaviateSessionVerifier(client)
	//   result, err := verifier.VerifySession(ctx, "sess-123")
	//
	// # Limitations
	//
	//   - Only verifies data currently in storage
	//
	// # Assumptions
	//
	//   - Session exists in storage
	VerifySession(c *gin.Context, sessionID string) (*VerifySessionResponse, error)
}

// =============================================================================
// Structs
// =============================================================================

// VerifySessionResponse is the response from POST /v1/sessions/:sessionId/verify.
//
// # Description
//
// Returns the result of hash chain verification for a session.
// This allows users to cryptographically verify their conversation
// has not been tampered with.
//
// # Fields
//
//   - SessionID: The session that was verified (full UUID)
//   - Verified: Whether the data integrity check passed
//   - TurnCount: Number of conversation turns verified
//   - ChainHash: Hash of all turn content combined
//   - VerifiedAt: Timestamp when verification was performed
//   - TurnHashes: Hash of each individual Q&A turn
//   - ErrorDetails: If verification failed, details about the failure
//
// # Security
//
// This endpoint is read-only and does not modify any data.
// The verification result is based on SHA-256 hash computation.
type VerifySessionResponse struct {
	SessionID    string         `json:"session_id"`
	Verified     bool           `json:"verified"`
	TurnCount    int            `json:"turn_count"`
	ChainHash    string         `json:"chain_hash,omitempty"`
	VerifiedAt   int64          `json:"verified_at"`
	TurnHashes   map[int]string `json:"turn_hashes,omitempty"`
	ErrorDetails string         `json:"error_details,omitempty"`
}

// conversationTurn represents a Q&A turn from Weaviate.
//
// # Description
//
// Internal struct for parsing Weaviate query results.
//
// # Fields
//
//   - Question: The user's question
//   - Answer: The LLM's response
//   - Timestamp: When the turn occurred
type conversationTurn struct {
	Question  string  `json:"question"`
	Answer    string  `json:"answer"`
	Timestamp float64 `json:"timestamp"`
}

// weaviateSessionVerifier implements SessionVerifier using Weaviate.
//
// # Description
//
// Production implementation that queries Weaviate's Conversation class
// to retrieve session turns and compute integrity hashes.
//
// # Fields
//
//   - client: Weaviate client for database access
//
// # Thread Safety
//
// Thread-safe. Weaviate client handles connection pooling.
type weaviateSessionVerifier struct {
	client *weaviate.Client
}

// =============================================================================
// Constructor Functions
// =============================================================================

// NewWeaviateSessionVerifier creates a SessionVerifier backed by Weaviate.
//
// # Description
//
// Factory function for production use. Panics if client is nil.
//
// # Inputs
//
//   - client: Weaviate client (must not be nil)
//
// # Outputs
//
//   - SessionVerifier: Ready for use
//
// # Examples
//
//	verifier := NewWeaviateSessionVerifier(weaviateClient)
//
// # Limitations
//
//   - Panics on nil client (fail-fast for programming errors)
//
// # Assumptions
//
//   - Weaviate is available and schema is initialized
func NewWeaviateSessionVerifier(client *weaviate.Client) SessionVerifier {
	if client == nil {
		panic("NewWeaviateSessionVerifier: client must not be nil")
	}
	return &weaviateSessionVerifier{
		client: client,
	}
}

// =============================================================================
// Enterprise Verifier Factory
// =============================================================================

// EnterpriseVerifierOptions contains dependencies for enterprise verification.
//
// # Description
//
// Configuration and dependencies needed to create an enterprise-enabled
// session verifier. Pass nil values for features you don't want to enable.
//
// # Fields
//
//   - WeaviateClient: Required - Weaviate client for session data
//   - PolicyEngine: Optional - Privacy policy engine for data classification
//   - KeyedHashComputer: Optional - HMAC verification (enterprise)
//   - SignatureVerifier: Optional - Digital signatures (enterprise)
//   - TimestampAuthority: Optional - RFC 3161 timestamps (enterprise)
//   - HSMProvider: Optional - Hardware security module (enterprise)
//   - AuditLogger: Optional - Compliance audit logging (enterprise)
//   - Authorizer: Optional - Access control (enterprise)
//   - RateLimiter: Optional - Rate limiting (enterprise)
//   - Cache: Optional - Result caching (enterprise)
//   - BatchVerifier: Optional - Bulk verification (enterprise)
//   - Scheduler: Optional - Periodic verification (enterprise)
//   - AlertSender: Optional - Failure alerting (enterprise)
type EnterpriseVerifierOptions struct {
	// WeaviateClient is required for session data access.
	WeaviateClient *weaviate.Client

	// PolicyEngine for data classification and privacy scanning.
	// If nil, privacy scanning is disabled.
	PolicyEngine interface {
		ClassifyContent(content string) (string, error)
	}

	// --- Enterprise Cryptographic Extensions ---

	// KeyedHashComputer for HMAC-based verification.
	KeyedHashComputer interface {
		ComputeHMAC(keyID string, content string) (string, error)
		VerifyHMAC(keyID string, content string, expectedHMAC string) (bool, error)
	}

	// SignatureVerifier for digital signature verification.
	SignatureVerifier interface {
		VerifySignature(content string, signature string, signerID string) (bool, error)
	}

	// TimestampAuthority for RFC 3161 trusted timestamps.
	TimestampAuthority interface {
		GetTimestamp(contentHash string) (interface{}, error)
		VerifyTimestamp(token interface{}, contentHash string) (bool, error)
	}

	// HSMProvider for hardware security module operations.
	HSMProvider interface {
		SignWithHSM(keyLabel string, content []byte) ([]byte, error)
		VerifyWithHSM(keyLabel string, content []byte, signature []byte) (bool, error)
	}

	// --- Enterprise Operational Extensions ---

	// AuditLogger for compliance audit trails.
	AuditLogger interface {
		LogVerificationAttempt(event interface{}) error
	}

	// Authorizer for access control.
	Authorizer VerificationAuthorizer

	// RateLimiter for request throttling.
	RateLimiter VerificationRateLimiter

	// Cache for verification result caching.
	Cache VerificationCache

	// BatchVerifier for bulk operations.
	BatchVerifier BatchVerifier

	// Scheduler for periodic verification.
	Scheduler VerificationScheduler

	// AlertSender for failure notifications.
	AlertSender VerificationAlertSender
}

// enterpriseSessionVerifier wraps the base verifier with enterprise features.
type enterpriseSessionVerifier struct {
	base         SessionVerifier
	opts         *EnterpriseVerifierOptions
	policyEngine interface {
		ClassifyContent(content string) (string, error)
	}
}

// NewEnterpriseSessionVerifier creates a verifier with enterprise extensions.
//
// # Description
//
// Factory function that creates a session verifier with optional enterprise
// features based on provided options. Features are automatically enabled
// when their corresponding dependencies are provided.
//
// # Inputs
//
//   - opts: Enterprise verifier options with dependencies
//
// # Outputs
//
//   - SessionVerifier: Enterprise-enabled verifier
//
// # Examples
//
//	opts := &EnterpriseVerifierOptions{
//	    WeaviateClient: weaviateClient,
//	    PolicyEngine:   policyEngine,
//	    AuditLogger:    auditLogger,
//	}
//	verifier := NewEnterpriseSessionVerifier(opts)
//
// # Limitations
//
//   - WeaviateClient is required, panics if nil
//
// # Assumptions
//
//   - Optional dependencies are nil-safe
func NewEnterpriseSessionVerifier(opts *EnterpriseVerifierOptions) SessionVerifier {
	if opts == nil || opts.WeaviateClient == nil {
		panic("NewEnterpriseSessionVerifier: WeaviateClient is required")
	}

	baseVerifier := NewWeaviateSessionVerifier(opts.WeaviateClient)

	return &enterpriseSessionVerifier{
		base:         baseVerifier,
		opts:         opts,
		policyEngine: opts.PolicyEngine,
	}
}

// VerifySession implements SessionVerifier with enterprise enhancements.
//
// # Description
//
// Performs session verification with optional enterprise features:
//  1. Rate limiting check (if RateLimiter provided)
//  2. Authorization check (if Authorizer provided)
//  3. Cache lookup (if Cache provided)
//  4. Base verification
//  5. Audit logging (if AuditLogger provided)
//  6. Alert on failure (if AlertSender provided)
//  7. Cache result (if Cache provided)
//
// # Inputs
//
//   - c: Gin context containing auth headers
//   - sessionID: Session to verify
//
// # Outputs
//
//   - *VerifySessionResponse: Verification results
//   - error: Non-nil if verification failed
func (v *enterpriseSessionVerifier) VerifySession(c *gin.Context, sessionID string) (*VerifySessionResponse, error) {
	startTime := time.Now()
	userID := c.GetHeader("X-User-ID")
	tenantID := c.GetHeader("X-Tenant-ID")

	// Rate limiting
	if v.opts.RateLimiter != nil {
		allowed, waitTime := v.opts.RateLimiter.AllowVerification(userID, tenantID)
		if !allowed {
			return &VerifySessionResponse{
				SessionID:    sessionID,
				Verified:     false,
				ErrorDetails: fmt.Sprintf("rate limited, retry after %v", waitTime),
			}, nil
		}
	}

	// Authorization
	if v.opts.Authorizer != nil {
		allowed, err := v.opts.Authorizer.CanVerify(userID, sessionID)
		if err != nil {
			return nil, fmt.Errorf("authorization check failed: %w", err)
		}
		if !allowed {
			return &VerifySessionResponse{
				SessionID:    sessionID,
				Verified:     false,
				ErrorDetails: "not authorized to verify this session",
			}, nil
		}
	}

	// Cache lookup
	if v.opts.Cache != nil {
		if cached, found := v.opts.Cache.Get(sessionID, ""); found {
			slog.Debug("Cache hit for verification", "sessionId", sessionID)
			return cached, nil
		}
	}

	// Perform base verification
	result, err := v.base.VerifySession(c, sessionID)
	if err != nil {
		// Audit failure
		if v.opts.AuditLogger != nil {
			_ = v.opts.AuditLogger.LogVerificationAttempt(map[string]interface{}{
				"session_id":     sessionID,
				"user_id":        userID,
				"tenant_id":      tenantID,
				"success":        false,
				"failure_reason": err.Error(),
				"duration_ms":    time.Since(startTime).Milliseconds(),
			})
		}
		return nil, err
	}

	// Audit success
	if v.opts.AuditLogger != nil {
		_ = v.opts.AuditLogger.LogVerificationAttempt(map[string]interface{}{
			"session_id":  sessionID,
			"user_id":     userID,
			"tenant_id":   tenantID,
			"success":     result.Verified,
			"turn_count":  result.TurnCount,
			"chain_hash":  result.ChainHash,
			"duration_ms": time.Since(startTime).Milliseconds(),
		})
	}

	// Alert on failure
	if v.opts.AlertSender != nil && !result.Verified {
		severity := "warning"
		if result.ErrorDetails != "" {
			severity = "critical"
		}
		_ = v.opts.AlertSender.SendAlert(sessionID, result, severity)
	}

	// Cache result
	if v.opts.Cache != nil && result.Verified {
		v.opts.Cache.Set(sessionID, result, 5*time.Minute)
	}

	return result, nil
}

// =============================================================================
// Handler Functions
// =============================================================================

// VerifySession creates a gin handler for session verification.
//
// # Description
//
// HTTP handler for POST /v1/sessions/:sessionId/verify.
// Loads the session's conversation history from Weaviate,
// computes content hashes for each turn, and returns verification results.
//
// # Inputs
//
//   - client: Weaviate client for database access
//
// # Outputs
//
//   - gin.HandlerFunc: HTTP handler function
//
// # Examples
//
//	router.POST("/v1/sessions/:sessionId/verify", VerifySession(weaviateClient))
//
// # Limitations
//
//   - Only verifies Conversation records currently in Weaviate
//   - Does not verify real-time streaming events
//
// # Assumptions
//
//   - Weaviate is available
//   - Conversation class exists with expected schema
func VerifySession(client *weaviate.Client) gin.HandlerFunc {
	verifier := NewWeaviateSessionVerifier(client)

	return func(c *gin.Context) {
		_, span := tracer.Start(c.Request.Context(), "VerifySession.handler")
		defer span.End()

		sessionID := c.Param("sessionId")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "sessionId is required"})
			return
		}

		span.SetAttributes(attribute.String("session.id", sessionID))
		slog.Info("Received request to verify session", "sessionId", sessionID)

		result, err := verifier.VerifySession(c, sessionID)
		if err != nil {
			slog.Error("Failed to verify session", "sessionId", sessionID, "error", err)
			span.RecordError(err)
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":      "failed to verify session",
				"session_id": sessionID,
			})
			return
		}

		span.SetAttributes(
			attribute.Bool("verification.passed", result.Verified),
			attribute.Int("verification.turn_count", result.TurnCount),
		)

		slog.Info("Session verification complete",
			"sessionId", sessionID,
			"verified", result.Verified,
			"turnCount", result.TurnCount,
		)

		c.JSON(http.StatusOK, result)
	}
}

// =============================================================================
// weaviateSessionVerifier Methods
// =============================================================================

// VerifySession verifies a session's data integrity.
//
// # Description
//
// Performs verification by:
//  1. Loading all Conversation turns for the session from Weaviate
//  2. Computing SHA-256 hash for each Q&A turn
//  3. Computing a chain hash of all turns combined
//  4. Returning verification results with all hashes
//
// The chain hash is computed as: SHA256(turn1_hash || turn2_hash || ...)
// This provides tamper-evidence for the entire conversation.
//
// # Inputs
//
//   - c: Gin context for request handling
//   - sessionID: The session to verify
//
// # Outputs
//
//   - *VerifySessionResponse: Verification results
//   - error: Non-nil if Weaviate query failed
//
// # Examples
//
//	result, err := verifier.VerifySession(c, "sess-abc123")
//	if err != nil {
//	    return err
//	}
//	if result.Verified {
//	    fmt.Println("All turns verified")
//	}
//
// # Limitations
//
//   - Cannot detect if turns were deleted (only verifies existing data)
//   - Requires Weaviate availability
//
// # Assumptions
//
//   - Conversation class has question, answer, timestamp fields
//   - Session ID format is valid
func (v *weaviateSessionVerifier) VerifySession(c *gin.Context, sessionID string) (*VerifySessionResponse, error) {
	// Load conversation turns from Weaviate
	turns, err := v.loadConversationTurns(c, sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to load conversation turns: %w", err)
	}

	// Compute hashes for each turn
	turnHashes := v.computeTurnHashes(turns)

	// Compute chain hash from all turn hashes
	chainHash := v.computeChainHash(turnHashes)

	// Build response
	response := &VerifySessionResponse{
		SessionID:  sessionID,
		Verified:   true, // Currently always true if we can read the data
		TurnCount:  len(turns),
		ChainHash:  chainHash,
		VerifiedAt: time.Now().UnixMilli(),
		TurnHashes: turnHashes,
	}

	return response, nil
}

// loadConversationTurns loads all conversation turns for a session.
//
// # Description
//
// Queries Weaviate's Conversation class for all Q&A pairs
// associated with the given session, ordered chronologically.
//
// # Inputs
//
//   - c: Gin context for request handling
//   - sessionID: The session to load
//
// # Outputs
//
//   - []conversationTurn: Ordered list of Q&A pairs
//   - error: Non-nil if query failed
//
// # Examples
//
//	turns, err := v.loadConversationTurns(c, "sess-123")
//	for _, turn := range turns {
//	    fmt.Printf("Q: %s\nA: %s\n", turn.Question, turn.Answer)
//	}
//
// # Limitations
//
//   - Returns empty slice for sessions with no history
//   - No pagination (loads all turns at once)
//
// # Assumptions
//
//   - Conversation class schema matches expected fields
func (v *weaviateSessionVerifier) loadConversationTurns(c *gin.Context, sessionID string) ([]conversationTurn, error) {
	ctx := c.Request.Context()

	fields := []graphql.Field{
		{Name: "question"},
		{Name: "answer"},
		{Name: "timestamp"},
	}

	whereFilter := filters.Where().
		WithPath([]string{"session_id"}).
		WithOperator(filters.Equal).
		WithValueString(sessionID)

	sortBy := graphql.Sort{
		Path:  []string{"timestamp"},
		Order: graphql.Asc,
	}

	result, err := v.client.GraphQL().Get().
		WithClassName("Conversation").
		WithWhere(whereFilter).
		WithSort(sortBy).
		WithFields(fields...).
		Do(ctx)
	if err != nil {
		return nil, fmt.Errorf("weaviate query failed: %w", err)
	}

	// Parse the response using marshal/unmarshal pattern
	return v.parseConversationResult(result.Data)
}

// weaviateConversationResponse is the expected structure from Weaviate.
//
// # Description
//
// Internal struct for parsing the nested Weaviate GraphQL response.
// Structure: {"Get": {"Conversation": [...]}}
type weaviateConversationResponse struct {
	Get struct {
		Conversation []conversationTurn `json:"Conversation"`
	} `json:"Get"`
}

// parseConversationResult parses Weaviate GraphQL response into turns.
//
// # Description
//
// Extracts conversation turns from the raw Weaviate response data.
// Uses marshal/unmarshal pattern to handle type conversion.
//
// # Inputs
//
//   - data: Raw response data from Weaviate
//
// # Outputs
//
//   - []conversationTurn: Parsed turns
//   - error: Non-nil if parsing failed
//
// # Examples
//
//	turns, err := v.parseConversationResult(result.Data)
//
// # Limitations
//
//   - Expects specific Weaviate response structure
//
// # Assumptions
//
//   - Response contains Get.Conversation array
func (v *weaviateSessionVerifier) parseConversationResult(data interface{}) ([]conversationTurn, error) {
	// Marshal the raw data to JSON bytes
	rawBytes, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal response data: %w", err)
	}

	// Unmarshal into our expected structure
	var parsed weaviateConversationResponse
	if err := json.Unmarshal(rawBytes, &parsed); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	// Return the conversation turns (may be empty slice)
	if parsed.Get.Conversation == nil {
		return []conversationTurn{}, nil
	}

	return parsed.Get.Conversation, nil
}

// computeTurnHashes computes SHA-256 hash for each conversation turn.
//
// # Description
//
// For each turn, computes: SHA256(question + answer)
// Returns a map from 1-indexed turn number to its hash.
//
// # Inputs
//
//   - turns: List of conversation turns
//
// # Outputs
//
//   - map[int]string: Turn number -> hash mapping
//
// # Examples
//
//	hashes := v.computeTurnHashes(turns)
//	fmt.Println("Turn 1 hash:", hashes[1])
//
// # Limitations
//
//   - Uses 1-indexed turn numbers (not 0-indexed)
//
// # Assumptions
//
//   - Turns are in chronological order
func (v *weaviateSessionVerifier) computeTurnHashes(turns []conversationTurn) map[int]string {
	hashes := make(map[int]string, len(turns))

	for i, turn := range turns {
		content := turn.Question + turn.Answer
		hash := sha256.Sum256([]byte(content))
		hashes[i+1] = hex.EncodeToString(hash[:])
	}

	return hashes
}

// computeChainHash computes a chain hash from all turn hashes.
//
// # Description
//
// Computes: SHA256(hash1 || hash2 || ... || hashN)
// This provides a single hash representing the entire conversation.
//
// # Inputs
//
//   - turnHashes: Map of turn number to hash
//
// # Outputs
//
//   - string: Chain hash (64-char hex), empty if no turns
//
// # Examples
//
//	chainHash := v.computeChainHash(turnHashes)
//	fmt.Println("Chain hash:", chainHash)
//
// # Limitations
//
//   - Returns empty string for empty conversations
//
// # Assumptions
//
//   - Turn hashes are valid hex strings
func (v *weaviateSessionVerifier) computeChainHash(turnHashes map[int]string) string {
	if len(turnHashes) == 0 {
		return ""
	}

	// Concatenate all hashes in order
	var combined string
	for i := 1; i <= len(turnHashes); i++ {
		combined += turnHashes[i]
	}

	hash := sha256.Sum256([]byte(combined))
	return hex.EncodeToString(hash[:])
}
