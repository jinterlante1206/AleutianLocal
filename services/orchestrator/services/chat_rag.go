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
	"time"

	"github.com/jinterlante1206/AleutianLocal/services/llm"
	"github.com/jinterlante1206/AleutianLocal/services/orchestrator/datatypes"
	"github.com/jinterlante1206/AleutianLocal/services/policy_engine"
	"github.com/weaviate/weaviate-go-client/v5/weaviate"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// chatRAGTracer is the OpenTelemetry tracer for ChatRAGService operations.
var chatRAGTracer = otel.Tracer("aleutian.orchestrator.services.chat_rag")

// Compile-time interface implementation check.
var _ DocumentRetriever = (*ChatRAGService)(nil)

// =============================================================================
// Interfaces
// =============================================================================

// DocumentRetriever defines the contract for retrieving document content
// from a RAG engine without LLM generation.
//
// # Description
//
// This interface abstracts document retrieval for streaming integration.
// It enables the Go orchestrator to receive raw document content from
// the Python RAG engine, then stream LLM responses with full context.
//
// This fixes the "two-LLM problem" where:
//  1. Python RAG generated an answer (first LLM call)
//  2. Go used that answer as "context" for streaming (second LLM call)
//  3. The streaming LLM said "no documents found" despite sources being shown
//
// Now:
//  1. Python RAG retrieves and reranks documents (no LLM call)
//  2. Go receives raw document content
//  3. Go streams LLM response with actual document context (single LLM call)
//
// # Supported Pipelines
//
//   - "reranking": Cross-encoder reranking for better relevance
//   - "verified": Skeptic/optimist debate for hallucination prevention
//
// # Thread Safety
//
// Implementations must be safe for concurrent use.
//
// # Example Implementation
//
//	type myRetriever struct { /* ... */ }
//
//	func (r *myRetriever) RetrieveDocuments(
//	    ctx context.Context,
//	    req *datatypes.RetrievalRequest,
//	) (*datatypes.RetrievalResponse, error) {
//	    // Call RAG engine, handle retries, return documents
//	}
type DocumentRetriever interface {
	// RetrieveDocuments retrieves document content from the RAG engine
	// without LLM generation.
	//
	// # Description
	//
	// Calls the Python RAG engine's retrieval-only endpoint to get document
	// chunks for streaming integration. Includes retry logic with exponential
	// backoff for transient failures.
	//
	// # Inputs
	//
	//   - ctx: Context for cancellation, timeouts, and tracing.
	//     Recommended timeout: 30 seconds for retrieval operations.
	//   - req: RetrievalRequest containing query and retrieval parameters.
	//     Must have non-empty Query field.
	//
	// # Outputs
	//
	//   - *datatypes.RetrievalResponse: Contains document chunks and formatted
	//     context text (e.g., "[Document 1: source]\ncontent...").
	//   - error: Non-nil if retrieval failed after all retries.
	//     May be *RetrievalError for HTTP errors or wrapped errors for others.
	//
	// # Errors
	//
	//   - *RetrievalError with StatusCode 400: Invalid request (not retried)
	//   - *RetrievalError with StatusCode 501: Pipeline not implemented (not retried)
	//   - *RetrievalError with StatusCode 503: Service unavailable (retried)
	//   - context.DeadlineExceeded: Context timeout
	//   - context.Canceled: Context canceled
	//
	// # Examples
	//
	//   // Default reranking pipeline
	//   req := datatypes.NewRetrievalRequest("What is OAuth?", "sess_123")
	//   resp, err := retriever.RetrieveDocuments(ctx, req)
	//   if err != nil {
	//       return err
	//   }
	//   fmt.Println(resp.ContextText)  // "[Document 1: auth.md]\n..."
	//
	//   // Use verified pipeline for hallucination prevention
	//   req := datatypes.NewRetrievalRequest("query", "sess_123").WithPipeline("verified")
	//   resp, err := retriever.RetrieveDocuments(ctx, req)
	//
	// # Limitations
	//
	//   - Only "reranking" and "verified" pipelines are supported.
	//   - Maximum 3 retries with 1s, 2s, 4s exponential backoff.
	//   - Timeout must be set via context; no internal timeout enforcement.
	//
	// # Assumptions
	//
	//   - Python RAG engine is running and reachable.
	//   - The /rag/retrieve/{pipeline} endpoint follows the API contract.
	//   - Network failures are transient and benefit from retries.
	//   - 503 errors indicate temporary overload, not permanent failure.
	RetrieveDocuments(ctx context.Context, req *datatypes.RetrievalRequest) (*datatypes.RetrievalResponse, error)
}

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

	// Add verified pipeline configuration (P4-2: temperature overrides)
	if req.Pipeline == "verified" {
		// Add temperature overrides if provided
		if req.TemperatureOverrides != nil {
			tempMap := req.TemperatureOverrides.ToMap()
			if tempMap != nil {
				ragPayload["temperature_overrides"] = tempMap
				span.SetAttributes(attribute.Bool("rag.has_temp_overrides", true))

				// Log individual temperatures for debugging
				if req.TemperatureOverrides.Optimist != nil {
					span.SetAttributes(attribute.Float64("rag.temp.optimist", *req.TemperatureOverrides.Optimist))
				}
				if req.TemperatureOverrides.Skeptic != nil {
					span.SetAttributes(attribute.Float64("rag.temp.skeptic", *req.TemperatureOverrides.Skeptic))
				}
				if req.TemperatureOverrides.Refiner != nil {
					span.SetAttributes(attribute.Float64("rag.temp.refiner", *req.TemperatureOverrides.Refiner))
				}
			}
		}

		// Add strictness mode if specified
		if req.Strictness != "" {
			ragPayload["strictness"] = req.Strictness
			span.SetAttributes(attribute.String("rag.strictness", req.Strictness))
		}
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
// Document Retrieval Methods
// =============================================================================

// Retrieval configuration constants.
const (
	// maxRetrievalRetries is the maximum number of retry attempts for
	// retrieval operations. Retries use exponential backoff.
	maxRetrievalRetries = 3

	// initialRetryDelay is the delay before the first retry attempt.
	// Subsequent retries double this delay (1s, 2s, 4s).
	initialRetryDelay = 1 * time.Second
)

// supportedRetrievalPipelines defines which pipelines support retrieval-only mode.
var supportedRetrievalPipelines = map[string]bool{
	"reranking": true,
	"verified":  true,
}

// RetrieveDocuments retrieves document content from the RAG engine without
// LLM generation.
//
// # Description
//
// This method implements the DocumentRetriever interface. It calls the Python
// RAG engine's retrieval-only endpoint to get document chunks for streaming
// integration. The method includes retry logic with exponential backoff for
// transient failures (503 Service Unavailable).
//
// # Inputs
//
//   - ctx: Context for cancellation, timeouts, and tracing. Should have a
//     reasonable timeout (recommended: 30 seconds for retrieval operations).
//   - req: RetrievalRequest containing query and retrieval parameters.
//     Must have non-empty Query field.
//
// # Outputs
//
//   - *datatypes.RetrievalResponse: Contains document chunks and formatted
//     context text ready for LLM consumption.
//   - error: Non-nil if retrieval failed after all retries. May be:
//   - *RetrievalError for HTTP errors
//   - context.DeadlineExceeded for timeout
//   - context.Canceled for cancellation
//   - Wrapped errors for other failures
//
// # Examples
//
//	// Basic usage with default reranking pipeline
//	req := datatypes.NewRetrievalRequest("What is OAuth?", "sess_123")
//	resp, err := service.RetrieveDocuments(ctx, req)
//	if err != nil {
//	    return fmt.Errorf("retrieval failed: %w", err)
//	}
//	fmt.Println(resp.ContextText)  // "[Document 1: auth.md]\n..."
//
//	// Use verified pipeline for hallucination prevention
//	req := datatypes.NewRetrievalRequest("query", "sess_123").WithPipeline("verified")
//	resp, err := service.RetrieveDocuments(ctx, req)
//
//	// Handling unsupported pipeline errors
//	req := datatypes.NewRetrievalRequest("query", "").WithPipeline("graph")
//	resp, err := service.RetrieveDocuments(ctx, req)
//	// err: "pipeline 'graph' does not support retrieval-only mode"
//
// # Limitations
//
//   - Only "reranking" and "verified" pipelines are supported.
//   - Maximum 3 retries with 1s, 2s, 4s exponential backoff.
//   - Timeout must be set via context; no internal timeout enforcement.
//   - Large document sets may exceed response size limits.
//
// # Assumptions
//
//   - Python RAG engine is running and reachable at RAG_ENGINE_URL.
//   - The /rag/retrieve/{pipeline} endpoint follows the API contract.
//   - Network failures are transient and benefit from retries.
//   - 503 errors indicate temporary overload, not permanent failure.
func (s *ChatRAGService) RetrieveDocuments(
	ctx context.Context,
	req *datatypes.RetrievalRequest,
) (*datatypes.RetrievalResponse, error) {
	ctx, span := chatRAGTracer.Start(ctx, "ChatRAGService.RetrieveDocuments")
	defer span.End()

	// Validate request
	if req == nil {
		err := fmt.Errorf("request is nil")
		span.RecordError(err)
		span.SetStatus(codes.Error, "nil request")
		return nil, err
	}

	if req.Query == "" {
		err := fmt.Errorf("query is required")
		span.RecordError(err)
		span.SetStatus(codes.Error, "empty query")
		return nil, err
	}

	// Determine pipeline (default to reranking)
	pipeline := req.Pipeline
	if pipeline == "" {
		pipeline = "reranking"
	}

	// Validate pipeline is supported for retrieval-only mode
	if !supportedRetrievalPipelines[pipeline] {
		err := fmt.Errorf("pipeline '%s' does not support retrieval-only mode; use 'reranking' or 'verified'", pipeline)
		span.RecordError(err)
		span.SetStatus(codes.Error, "unsupported pipeline")
		return nil, err
	}

	span.SetAttributes(
		attribute.String("query", req.Query),
		attribute.String("pipeline", pipeline),
		attribute.Int("max_chunks", req.MaxChunks),
		attribute.Bool("strict_mode", req.StrictMode),
	)

	if req.SessionId != "" {
		span.SetAttributes(attribute.String("session_id", req.SessionId))
	}

	// Retry loop with exponential backoff
	var lastErr error
	retryDelay := initialRetryDelay

	for attempt := 0; attempt <= maxRetrievalRetries; attempt++ {
		if attempt > 0 {
			span.AddEvent("retry_attempt", trace.WithAttributes(
				attribute.Int("attempt", attempt),
				attribute.String("delay", retryDelay.String()),
			))
			slog.Info("Retrying retrieval",
				"attempt", attempt,
				"delay", retryDelay,
				"lastError", lastErr,
			)

			select {
			case <-ctx.Done():
				span.RecordError(ctx.Err())
				span.SetStatus(codes.Error, "context canceled during retry")
				return nil, ctx.Err()
			case <-time.After(retryDelay):
				// Continue with retry
			}
			retryDelay *= 2 // Exponential backoff
		}

		resp, err := s.callRetrievalEndpoint(ctx, pipeline, req)
		if err == nil {
			span.SetAttributes(
				attribute.Int("chunks_count", len(resp.Chunks)),
				attribute.Bool("has_relevant_docs", resp.HasRelevantDocs),
				attribute.Int("attempts", attempt+1),
			)
			return resp, nil
		}

		lastErr = err

		// Check if error is retryable
		if !s.isRetryableError(err) {
			span.RecordError(err)
			span.SetStatus(codes.Error, "non-retryable error")
			return nil, err
		}
	}

	// All retries exhausted
	span.RecordError(lastErr)
	span.SetStatus(codes.Error, "all retries exhausted")
	span.SetAttributes(attribute.Int("total_attempts", maxRetrievalRetries+1))
	return nil, fmt.Errorf("retrieval failed after %d attempts: %w", maxRetrievalRetries+1, lastErr)
}

// callRetrievalEndpoint makes a single HTTP request to the retrieval-only endpoint.
//
// # Description
//
// This is a private helper method that performs a single HTTP call to the
// Python RAG engine's /rag/retrieve/{pipeline} endpoint. It handles request
// serialization, response parsing, and error categorization.
//
// # Inputs
//
//   - ctx: Context for cancellation and tracing.
//   - pipeline: The RAG pipeline to use (e.g., "reranking", "verified").
//   - req: The retrieval request payload.
//
// # Outputs
//
//   - *datatypes.RetrievalResponse: Parsed response from the RAG engine.
//   - error: Non-nil on failure. Returns *RetrievalError for HTTP errors.
//
// # Assumptions
//
//   - The RAG engine URL is configured and reachable.
//   - The endpoint returns JSON matching RetrievalResponse schema.
func (s *ChatRAGService) callRetrievalEndpoint(
	ctx context.Context,
	pipeline string,
	req *datatypes.RetrievalRequest,
) (*datatypes.RetrievalResponse, error) {
	ctx, span := chatRAGTracer.Start(ctx, "ChatRAGService.callRetrievalEndpoint")
	defer span.End()

	// Construct URL: e.g., http://aleutian-rag-engine:8000/rag/retrieve/reranking
	retrievalURL := fmt.Sprintf("%s/rag/retrieve/%s",
		strings.TrimSuffix(s.ragEngineURL, "/"),
		pipeline,
	)
	span.SetAttributes(attribute.String("retrieval.url", retrievalURL))

	// Serialize request
	payloadBytes, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal retrieval request: %w", err)
	}

	// Create HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, "POST", retrievalURL, bytes.NewBuffer(payloadBytes))
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
			attribute.Int("retrieval.status_code", resp.StatusCode),
			attribute.String("retrieval.error_body", string(body)),
		)

		retryable := s.isRetryableStatusCode(resp.StatusCode)
		return nil, &RetrievalError{
			StatusCode: resp.StatusCode,
			Message:    string(body),
			Retryable:  retryable,
		}
	}

	// Parse response
	var retrievalResp datatypes.RetrievalResponse
	if err := json.Unmarshal(body, &retrievalResp); err != nil {
		return nil, fmt.Errorf("failed to parse retrieval response: %w", err)
	}

	span.SetAttributes(attribute.Int("retrieval.chunks_count", len(retrievalResp.Chunks)))
	return &retrievalResp, nil
}

// isRetryableStatusCode determines if an HTTP status code is retryable.
//
// # Description
//
// Returns true for status codes that indicate transient failures where
// a retry may succeed. These include server overload (503), bad gateway
// (502), and gateway timeout (504).
//
// # Inputs
//
//   - statusCode: The HTTP status code to evaluate.
//
// # Outputs
//
//   - bool: True if the status code indicates a retryable error.
//
// # Retryable Codes
//
//   - 502 Bad Gateway
//   - 503 Service Unavailable
//   - 504 Gateway Timeout
func (s *ChatRAGService) isRetryableStatusCode(statusCode int) bool {
	switch statusCode {
	case http.StatusBadGateway, // 502
		http.StatusServiceUnavailable, // 503
		http.StatusGatewayTimeout:     // 504
		return true
	default:
		return false
	}
}

// isRetryableError determines if an error should trigger a retry.
//
// # Description
//
// Examines an error to determine if it represents a transient failure
// that may succeed on retry. This includes retryable HTTP status codes
// and certain network errors.
//
// # Inputs
//
//   - err: The error to evaluate.
//
// # Outputs
//
//   - bool: True if the error is retryable.
//
// # Retryable Errors
//
//   - *RetrievalError with Retryable=true
//   - Network timeout errors (not context timeout)
//
// # Non-Retryable Errors
//
//   - *RetrievalError with Retryable=false (4xx, 501)
//   - context.Canceled
//   - context.DeadlineExceeded
func (s *ChatRAGService) isRetryableError(err error) bool {
	if err == nil {
		return false
	}

	// Check for RetrievalError
	if re, ok := err.(*RetrievalError); ok {
		return re.Retryable
	}

	// Context errors are not retryable
	if err == context.Canceled || err == context.DeadlineExceeded {
		return false
	}

	// Network errors may be retryable
	// (connection refused, temporary network issues)
	return true
}

// =============================================================================
// Error Types
// =============================================================================

// RetrievalError wraps errors from the RAG retrieval endpoint.
//
// # Description
//
// RetrievalError provides structured error information for retrieval failures,
// including the HTTP status code and whether the error is retryable. This
// enables callers to make informed decisions about retry logic and fallback
// strategies.
//
// # Fields
//
//   - StatusCode: HTTP status code from the RAG engine (e.g., 400, 501, 503).
//   - Message: Error message from the RAG engine response body.
//   - Retryable: Whether this error should be retried with backoff.
//
// # Retryable Status Codes
//
//   - 503 (Service Unavailable): RAG engine overloaded, retry recommended.
//   - 502 (Bad Gateway): Upstream failure, retry may help.
//   - 504 (Gateway Timeout): Upstream timeout, retry may help.
//
// # Non-Retryable Status Codes
//
//   - 400 (Bad Request): Invalid request, will fail on retry.
//   - 404 (Not Found): Endpoint doesn't exist, will fail on retry.
//   - 501 (Not Implemented): Pipeline doesn't support retrieval-only.
//
// # Example
//
//	if err != nil {
//	    if re, ok := err.(*RetrievalError); ok {
//	        if re.Retryable {
//	            // Wait and retry
//	        } else if re.StatusCode == 501 {
//	            // Fall back to full RAG pipeline
//	        }
//	    }
//	}
type RetrievalError struct {
	StatusCode int
	Message    string
	Retryable  bool
}

// Error implements the error interface for RetrievalError.
func (e *RetrievalError) Error() string {
	return fmt.Sprintf("retrieval error (status %d): %s", e.StatusCode, e.Message)
}

// IsRetrievalError checks if an error is a RetrievalError.
//
// # Description
//
// Type assertion helper for RetrievalError. Useful for determining
// the appropriate HTTP status code or retry strategy in handlers.
//
// # Inputs
//
//   - err: The error to check.
//
// # Outputs
//
//   - bool: True if err is a *RetrievalError.
//
// # Example
//
//	if services.IsRetrievalError(err) {
//	    re := err.(*RetrievalError)
//	    if re.StatusCode == 501 {
//	        // Handle unsupported pipeline
//	    }
//	}
func IsRetrievalError(err error) bool {
	_, ok := err.(*RetrievalError)
	return ok
}

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
