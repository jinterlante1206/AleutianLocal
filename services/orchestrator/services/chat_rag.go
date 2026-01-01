// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

// Package services provides business logic services for the orchestrator.
//
// This package contains service structs that encapsulate business logic,
// separating it from HTTP handlers. Services are responsible for:
//   - Orchestrating calls to external services (RAG engine, LLM, Weaviate)
//   - Applying business rules and validation
//   - Managing transactions and error handling
//
// Services are designed to be:
//   - Testable: Dependencies are injected via constructors
//   - Composable: Services can call other services
//   - Traceable: All methods accept context for distributed tracing
package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/jinterlante1206/AleutianLocal/services/llm"
	"github.com/jinterlante1206/AleutianLocal/services/orchestrator/datatypes"
	"github.com/jinterlante1206/AleutianLocal/services/policy_engine"
	"github.com/weaviate/weaviate-go-client/v5/weaviate"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

// chatRAGTracer is the OpenTelemetry tracer for ChatRAGService operations.
var chatRAGTracer = otel.Tracer("aleutian.orchestrator.services.chat_rag")

// =============================================================================
// ChatRAGService
// =============================================================================

// ChatRAGService handles conversational RAG (Retrieval-Augmented Generation)
// requests. It orchestrates the flow between:
//   - Policy engine: Scans user input for sensitive data
//   - RAG engine: Retrieves relevant document chunks
//   - LLM client: Generates responses based on retrieved context
//   - Weaviate: Stores and retrieves session data
//
// The service is designed to be stateless - all state is passed in via requests
// or stored in Weaviate. This allows horizontal scaling of the orchestrator.
//
// Usage:
//
//	service := NewChatRAGService(weaviateClient, llmClient, policyEngine)
//	resp, err := service.Process(ctx, &req)
type ChatRAGService struct {
	weaviateClient *weaviate.Client
	llmClient      llm.LLMClient
	policyEngine   *policy_engine.PolicyEngine
	ragEngineURL   string
}

// NewChatRAGService creates a new ChatRAGService with the provided dependencies.
//
// Parameters:
//   - weaviateClient: Client for Weaviate vector database operations. Used for
//     session storage and retrieval. Must not be nil.
//   - llmClient: Client for LLM operations (generation, chat). The specific
//     implementation (Ollama, OpenAI, etc.) is determined by configuration.
//     Must not be nil.
//   - policyEngine: Engine for scanning content against data classification
//     policies. Used to block requests containing sensitive data. Must not be nil.
//
// The RAG engine URL is read from the RAG_ENGINE_URL environment variable,
// defaulting to "http://aleutian-rag-engine:8000" if not set.
//
// Returns a pointer to an initialized ChatRAGService ready for use.
//
// Example:
//
//	service := NewChatRAGService(weaviateClient, llmClient, policyEngine)
func NewChatRAGService(
	weaviateClient *weaviate.Client,
	llmClient llm.LLMClient,
	policyEngine *policy_engine.PolicyEngine,
) *ChatRAGService {
	ragEngineURL := os.Getenv("RAG_ENGINE_URL")
	if ragEngineURL == "" {
		ragEngineURL = "http://aleutian-rag-engine:8000"
		slog.Warn("RAG_ENGINE_URL not set, using default", "url", ragEngineURL)
	}

	return &ChatRAGService{
		weaviateClient: weaviateClient,
		llmClient:      llmClient,
		policyEngine:   policyEngine,
		ragEngineURL:   ragEngineURL,
	}
}

// =============================================================================
// Core Processing Methods
// =============================================================================

// Process handles a conversational RAG request end-to-end.
//
// The processing flow is:
//  1. Ensure request has defaults (ID, timestamp, pipeline)
//  2. Validate the request
//  3. Scan user message for policy violations
//  4. Generate or retrieve session ID
//  5. Call RAG engine for retrieval and generation
//  6. Build and return response
//
// Parameters:
//   - ctx: Context for cancellation, timeouts, and tracing. Should have a
//     reasonable timeout set (recommended: 2-3 minutes for complex queries).
//   - req: The ChatRAGRequest containing the user's message and options.
//     The request is modified in place to populate defaults.
//
// Returns:
//   - *datatypes.ChatRAGResponse: The response containing the LLM answer,
//     retrieved sources, and session information.
//   - error: Non-nil if processing failed. Errors are categorized as:
//   - ErrPolicyViolation: User input contains sensitive data
//   - ErrValidation: Request validation failed
//   - ErrRAGEngine: RAG engine call failed
//   - ErrLLM: LLM generation failed
//
// The method is safe for concurrent use - it does not modify shared state.
//
// Example:
//
//	req := &datatypes.ChatRAGRequest{
//	    Message:   "What is the authentication flow?",
//	    Pipeline:  "reranking",
//	    SessionId: "sess_abc123", // Optional: omit for new session
//	}
//	resp, err := service.Process(ctx, req)
//	if err != nil {
//	    // Handle error based on type
//	}
//	fmt.Println(resp.Answer)
func (s *ChatRAGService) Process(ctx context.Context, req *datatypes.ChatRAGRequest) (*datatypes.ChatRAGResponse, error) {
	ctx, span := chatRAGTracer.Start(ctx, "ChatRAGService.Process")
	defer span.End()

	// Step 1: Ensure defaults are set
	req.EnsureDefaults()
	span.SetAttributes(
		attribute.String("request.id", req.Id),
		attribute.String("request.pipeline", req.Pipeline),
	)

	// Step 2: Validate the request
	if err := req.Validate(); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "validation failed")
		return nil, fmt.Errorf("validation failed: %w", err)
	}

	// Step 3: Scan for policy violations
	if findings := s.ScanPolicy(req.Message); len(findings) > 0 {
		span.SetStatus(codes.Error, "policy violation")
		span.SetAttributes(attribute.Int("policy.findings", len(findings)))
		return nil, &PolicyViolationError{Findings: findings}
	}

	// Step 4: Ensure session ID exists
	sessionId := req.EnsureSessionId()
	span.SetAttributes(attribute.String("session.id", sessionId))
	slog.Info("Processing chat RAG request",
		"requestId", req.Id,
		"sessionId", sessionId,
		"pipeline", req.Pipeline,
		"bearing", req.Bearing,
	)

	// Step 5: Call RAG engine
	ragResp, err := s.callRAGEngine(ctx, req)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "RAG engine failed")
		return nil, fmt.Errorf("RAG engine failed: %w", err)
	}

	// Step 6: Build response
	turnCount := len(req.History) + 1
	resp := datatypes.NewChatRAGResponse(ragResp.Answer, sessionId, ragResp.Sources, turnCount)

	span.SetAttributes(
		attribute.String("response.id", resp.Id),
		attribute.Int("response.sources_count", len(resp.Sources)),
		attribute.Int("response.turn_count", resp.TurnCount),
	)

	return resp, nil
}

// ScanPolicy scans the provided content against the policy engine's rules.
//
// This method checks for sensitive data patterns defined in the policy
// configuration (e.g., SSN, credit cards, API keys, PII).
//
// Parameters:
//   - content: The text content to scan. Typically the user's message.
//
// Returns a slice of policy findings. An empty slice indicates no violations.
// Each finding contains details about what was detected and where.
//
// This method does not return an error - an empty policy engine or scan
// failure results in an empty findings slice (fail open for availability).
func (s *ChatRAGService) ScanPolicy(content string) []policy_engine.ScanFinding {
	if s.policyEngine == nil {
		return nil
	}
	return s.policyEngine.ScanFileContent(content)
}

// =============================================================================
// Private Methods
// =============================================================================

// callRAGEngine makes an HTTP request to the RAG engine service.
//
// This method:
//  1. Constructs the appropriate pipeline URL
//  2. Builds the request payload
//  3. Makes the HTTP request with the provided context
//  4. Parses and returns the response
//
// The method respects context cancellation and timeouts.
func (s *ChatRAGService) callRAGEngine(ctx context.Context, req *datatypes.ChatRAGRequest) (*datatypes.RagEngineResponse, error) {
	ctx, span := chatRAGTracer.Start(ctx, "ChatRAGService.callRAGEngine")
	defer span.End()

	// Construct URL: e.g., http://aleutian-rag-engine:8000/rag/reranking
	pipelineURL := fmt.Sprintf("%s/rag/%s",
		strings.TrimSuffix(s.ragEngineURL, "/"),
		req.Pipeline,
	)
	span.SetAttributes(attribute.String("rag.url", pipelineURL))

	// Build request payload
	// Convert ChatRAGRequest to the format expected by RAG engine
	ragPayload := map[string]interface{}{
		"query":      req.Message,
		"session_id": req.SessionId,
		"pipeline":   req.Pipeline,
	}

	// Add bearing as filter if specified
	if req.Bearing != "" {
		ragPayload["bearing"] = req.Bearing
		span.SetAttributes(attribute.String("rag.bearing", req.Bearing))
	}

	// Add conversation history if provided
	if len(req.History) > 0 {
		ragPayload["history"] = req.History
		span.SetAttributes(attribute.Int("rag.history_turns", len(req.History)))
	}

	payloadBytes, err := json.Marshal(ragPayload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal RAG request: %w", err)
	}

	// Create HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, "POST", pipelineURL, bytes.NewBuffer(payloadBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	// Execute request
	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	// Check status code
	if resp.StatusCode != http.StatusOK {
		span.SetAttributes(
			attribute.Int("rag.status_code", resp.StatusCode),
			attribute.String("rag.error_body", string(body)),
		)
		return nil, fmt.Errorf("RAG engine returned status %d: %s", resp.StatusCode, string(body))
	}

	// Parse response
	var ragResp datatypes.RagEngineResponse
	if err := json.Unmarshal(body, &ragResp); err != nil {
		return nil, fmt.Errorf("failed to parse RAG response: %w", err)
	}

	span.SetAttributes(attribute.Int("rag.sources_count", len(ragResp.Sources)))
	return &ragResp, nil
}

// =============================================================================
// Error Types
// =============================================================================

// PolicyViolationError is returned when user input violates data classification
// policies. This error should result in an HTTP 403 Forbidden response.
type PolicyViolationError struct {
	Findings []policy_engine.ScanFinding
}

// Error implements the error interface for PolicyViolationError.
func (e *PolicyViolationError) Error() string {
	return fmt.Sprintf("policy violation: %d findings", len(e.Findings))
}

// IsPolicyViolation checks if an error is a PolicyViolationError.
// This is useful for handlers to determine the appropriate HTTP status code.
//
// Example:
//
//	resp, err := service.Process(ctx, req)
//	if err != nil {
//	    if services.IsPolicyViolation(err) {
//	        c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
//	        return
//	    }
//	    c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
//	}
func IsPolicyViolation(err error) bool {
	_, ok := err.(*PolicyViolationError)
	return ok
}

// GetPolicyFindings extracts policy findings from a PolicyViolationError.
// Returns nil if the error is not a PolicyViolationError.
//
// Example:
//
//	if findings := services.GetPolicyFindings(err); findings != nil {
//	    for _, f := range findings {
//	        log.Printf("Found %s at line %d", f.ClassificationName, f.LineNumber)
//	    }
//	}
func GetPolicyFindings(err error) []policy_engine.ScanFinding {
	if pve, ok := err.(*PolicyViolationError); ok {
		return pve.Findings
	}
	return nil
}
