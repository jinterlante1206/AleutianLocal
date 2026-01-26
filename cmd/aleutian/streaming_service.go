// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

// Package main contains the Aleutian CLI streaming chat service implementations.
//
// This file defines the StreamingChatService interface and its implementations
// for communicating with the Aleutian orchestrator's streaming chat endpoints.
// It follows the layered streaming architecture:
//
//	HTTP Response Body → SSEParser → SSEStreamReader → StreamRenderer → StreamResult
//
// # Architecture
//
//	CLI Loop → StreamingChatService Interface → HTTPClient Interface → http.Client
//	                    ↓                                ↓
//	          ragStreamingChatService           SSEParser → SSEStreamReader
//	          directStreamingChatService                         ↓
//	                                                      StreamRenderer
//
// # File Organization
//
// This file follows optimal Go code style:
//  1. Interfaces (contracts first)
//  2. Configuration structs
//  3. Implementation structs
//  4. Constructor functions
//  5. Methods on structs
//
// See docs/code_quality_lessons/005_layered_streaming_architecture.md for design.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"sync"
	"time"

	"github.com/AleutianAI/AleutianFOSS/pkg/ux"
	"github.com/AleutianAI/AleutianFOSS/services/orchestrator/datatypes"
	"github.com/google/uuid"
)

// =============================================================================
// INTERFACES
// =============================================================================

// StreamingChatService defines the contract for streaming chat operations.
//
// # Description
//
// This interface provides streaming versions of chat operations, where the
// response is delivered token-by-token in real-time rather than as a single
// complete response. Implementations handle SSE parsing, rendering, and
// state management internally.
//
// # Inputs
//
// Methods accept context.Context for cancellation and timeout control.
// Message inputs must be non-empty strings.
//
// # Outputs
//
// SendMessage returns *ux.StreamResult containing:
//   - Answer: Complete concatenated response
//   - Thinking: Extended thinking content (if enabled)
//   - Sources: Retrieved documents (RAG only)
//   - SessionID: Session identifier for multi-turn
//   - Metrics: TotalTokens, FirstTokenAt, Duration, etc.
//
// # Examples
//
//	service := NewRAGStreamingChatService(RAGStreamingChatServiceConfig{
//	    BaseURL:     "http://localhost:8080",
//	    Pipeline:    "reranking",
//	    Personality: ux.PersonalityFull,
//	})
//	defer service.Close()
//
//	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
//	defer cancel()
//
//	result, err := service.SendMessage(ctx, "What is authentication?")
//	if err != nil {
//	    log.Fatal(err)
//	}
//	fmt.Printf("Tokens: %d, Duration: %v\n", result.TotalTokens, result.Duration())
//
// # Limitations
//
//   - Streaming requires server support for SSE format
//   - Large responses may timeout if Timeout is too short
//   - Context cancellation may result in partial results
//
// # Assumptions
//
//   - Server returns valid SSE-formatted responses
//   - Network connectivity is stable for stream duration
//   - Caller handles context lifecycle (cancellation, timeout)
type StreamingChatService interface {
	// SendMessage sends a user message and streams the assistant's response.
	//
	// Description:
	//   Sends message to streaming endpoint, parses SSE events, renders
	//   tokens in real-time, and returns accumulated result.
	//
	// Inputs:
	//   - ctx: Context for cancellation/timeout. When cancelled, stream stops.
	//   - message: User's input text. Must not be empty.
	//
	// Outputs:
	//   - *ux.StreamResult: Complete result with answer, sources, metrics.
	//   - error: Non-nil on network, server, or parse errors.
	//
	// Examples:
	//   result, err := service.SendMessage(ctx, "Explain OAuth2")
	//   if err != nil { return err }
	//   fmt.Println(result.Answer) // Already displayed during streaming
	//
	// Limitations:
	//   - Empty messages will likely cause server errors
	//   - Very long messages may exceed server limits
	//
	// Assumptions:
	//   - Server endpoint is reachable
	//   - Response is valid SSE format
	SendMessage(ctx context.Context, message string) (*ux.StreamResult, error)

	// GetSessionID returns the current session identifier.
	//
	// Description:
	//   Returns the session ID for multi-turn conversation tracking.
	//   For RAG: server-assigned after first response.
	//   For Direct: client-provided or empty.
	//
	// Inputs:
	//   None.
	//
	// Outputs:
	//   - string: Session ID, or empty string if no session established.
	//
	// Examples:
	//   sessionID := service.GetSessionID()
	//   if sessionID != "" {
	//       fmt.Printf("Session: %s\n", sessionID)
	//   }
	//
	// Limitations:
	//   - Returns empty before first successful SendMessage (RAG)
	//
	// Assumptions:
	//   - Thread-safe access is handled internally
	GetSessionID() string

	// Close releases any resources held by the service.
	//
	// Description:
	//   Performs cleanup. Currently no-op for HTTP implementations,
	//   but required for interface completeness and future extensibility.
	//
	// Inputs:
	//   None.
	//
	// Outputs:
	//   - error: Currently always nil.
	//
	// Examples:
	//   service := NewRAGStreamingChatService(config)
	//   defer service.Close()
	//
	// Limitations:
	//   - Does not cancel in-flight requests
	//
	// Assumptions:
	//   - Caller manages request lifecycle separately
	Close() error
}

// =============================================================================
// CONFIGURATION STRUCTS
// =============================================================================

// RAGStreamingChatServiceConfig holds configuration for RAG streaming chat service.
//
// # Description
//
// Configuration struct for creating ragStreamingChatService instances.
// Only BaseURL is required; all other fields have sensible defaults.
//
// # Fields
//
//   - BaseURL: Required. Orchestrator URL without trailing slash.
//   - SessionID: Optional. Resume existing session.
//   - Pipeline: Optional. RAG pipeline name. Default: "reranking".
//   - Writer: Optional. Output destination. Default: os.Stdout.
//   - Personality: Optional. Output styling. Default: PersonalityFull.
//   - Timeout: Optional. HTTP timeout. Default: 5 minutes.
//
// # Examples
//
//	config := RAGStreamingChatServiceConfig{
//	    BaseURL:     "http://localhost:8080",
//	    Pipeline:    "reranking",
//	    Personality: ux.PersonalityFull,
//	}
//
// # Limitations
//
//   - BaseURL validation is not performed; invalid URLs cause runtime errors
//
// # Assumptions
//
//   - BaseURL points to a valid orchestrator instance
//   - Pipeline name is valid for the orchestrator
type RAGStreamingChatServiceConfig struct {
	BaseURL     string              // Base URL of orchestrator (required)
	SessionID   string              // Session ID to resume (optional)
	Pipeline    string              // RAG pipeline name (optional)
	Writer      io.Writer           // Output destination (optional)
	Personality ux.PersonalityLevel // Output styling (optional)
	Timeout     time.Duration       // HTTP timeout (optional)
	StrictMode  bool                // Strict RAG mode: only answer from docs (optional)
	Verbosity   int                 // Verified pipeline verbosity: 0=silent, 1=summary, 2=detailed (optional)
	DataSpace   string              // Data space to filter queries by (optional, e.g., "work", "personal")
	DocVersion  string              // Specific document version to query (optional, e.g., "v1")
	SessionTTL  string              // Session TTL (optional, e.g., "24h", "7d"). Resets on each message.
	RecencyBias string              // Recency bias preset (optional): none, gentle, moderate, aggressive
}

// DirectStreamingChatServiceConfig holds configuration for direct streaming chat service.
//
// # Description
//
// Configuration struct for creating directStreamingChatService instances.
// Only BaseURL is required; all other fields have sensible defaults.
//
// # Fields
//
//   - BaseURL: Required. Orchestrator URL without trailing slash.
//   - SessionID: Optional. Client-side tracking identifier.
//   - EnableThinking: Optional. Enable Claude extended thinking.
//   - BudgetTokens: Optional. Token budget for thinking. Default: 2048.
//   - Writer: Optional. Output destination. Default: os.Stdout.
//   - Personality: Optional. Output styling. Default: PersonalityFull.
//   - Timeout: Optional. HTTP timeout. Default: 5 minutes.
//
// # Examples
//
//	config := DirectStreamingChatServiceConfig{
//	    BaseURL:        "http://localhost:8080",
//	    EnableThinking: true,
//	    BudgetTokens:   4096,
//	}
//
// # Limitations
//
//   - EnableThinking only works with Claude models
//   - BudgetTokens is ignored if EnableThinking is false
//
// # Assumptions
//
//   - BaseURL points to a valid orchestrator instance
//   - Orchestrator supports extended thinking if enabled
type DirectStreamingChatServiceConfig struct {
	BaseURL        string              // Base URL of orchestrator (required)
	SessionID      string              // Client-side session ID (optional)
	EnableThinking bool                // Enable extended thinking (optional)
	BudgetTokens   int                 // Token budget for thinking (optional)
	Writer         io.Writer           // Output destination (optional)
	Personality    ux.PersonalityLevel // Output styling (optional)
	Timeout        time.Duration       // HTTP timeout (optional)
}

// =============================================================================
// IMPLEMENTATION STRUCTS
// =============================================================================

// ragStreamingChatService implements StreamingChatService for RAG streaming.
//
// # Description
//
// Communicates with /v1/chat/rag/stream endpoint. Uses server-side session
// management and streams responses via SSE.
//
// # Fields
//
//   - client: HTTP client for requests
//   - parser: SSE event parser
//   - reader: Stream reader for I/O orchestration
//   - baseURL: Orchestrator base URL
//   - sessionID: Current session ID (server-assigned)
//   - pipeline: RAG pipeline name
//   - writer: Output destination
//   - personality: Output styling level
//   - mu: Mutex for thread safety
//
// # Thread Safety
//
// All public methods are protected by mutex. Safe for concurrent use.
//
// # Limitations
//
//   - Requires server to support /v1/chat/rag/stream endpoint
//   - Session state is lost if service is recreated
//
// # Assumptions
//
//   - Server assigns session IDs via done events
//   - Server returns sources via sources events
type ragStreamingChatService struct {
	client      HTTPClient
	parser      ux.SSEParser
	reader      ux.StreamReader
	baseURL     string
	sessionID   string
	pipeline    string
	writer      io.Writer
	personality ux.PersonalityLevel
	strictMode  bool   // Strict RAG mode: only answer from docs
	verbosity   int    // Verified pipeline verbosity: 0=silent, 1=summary, 2=detailed
	dataSpace   string // Data space to filter queries by
	docVersion  string // Specific document version to query
	sessionTTL  string // Session TTL (e.g., "24h", "7d")
	recencyBias string // Recency bias preset: none, gentle, moderate, aggressive
	mu          sync.Mutex
}

// directStreamingChatService implements StreamingChatService for direct streaming.
//
// # Description
//
// Communicates with /v1/chat/direct/stream endpoint. Maintains client-side
// message history and streams responses via SSE.
//
// # Fields
//
//   - client: HTTP client for requests
//   - parser: SSE event parser
//   - reader: Stream reader for I/O orchestration
//   - baseURL: Orchestrator base URL
//   - sessionID: Client-side session identifier
//   - messages: Conversation history (system + user + assistant)
//   - enableThinking: Whether extended thinking is enabled
//   - budgetTokens: Token budget for thinking mode
//   - writer: Output destination
//   - personality: Output styling level
//   - mu: Mutex for thread safety
//
// # Thread Safety
//
// All public methods are protected by mutex. Safe for concurrent use.
//
// # Limitations
//
//   - Message history grows unbounded; caller should manage long conversations
//   - Extended thinking requires Claude model support
//
// # Assumptions
//
//   - Server accepts full message history in requests
//   - Server streams thinking tokens before answer tokens
type directStreamingChatService struct {
	client         HTTPClient
	parser         ux.SSEParser
	reader         ux.StreamReader
	baseURL        string
	sessionID      string
	messages       []datatypes.Message
	enableThinking bool
	budgetTokens   int
	writer         io.Writer
	personality    ux.PersonalityLevel
	mu             sync.Mutex
}

// =============================================================================
// CONSTRUCTOR FUNCTIONS
// =============================================================================

// NewRAGStreamingChatService creates a new RAG streaming chat service.
//
// # Description
//
// Creates a ragStreamingChatService with production HTTP client.
// Initializes SSE parser and stream reader for event processing.
//
// # Inputs
//
//   - config: Service configuration. Only BaseURL is required.
//
// # Outputs
//
//   - StreamingChatService: Ready-to-use streaming service.
//
// # Examples
//
//	service := NewRAGStreamingChatService(RAGStreamingChatServiceConfig{
//	    BaseURL:  "http://localhost:8080",
//	    Pipeline: "reranking",
//	})
//	defer service.Close()
//
// # Limitations
//
//   - Does not validate BaseURL format
//   - Does not test connectivity
//
// # Assumptions
//
//   - Caller will call Close when done
//   - BaseURL is valid and reachable
func NewRAGStreamingChatService(config RAGStreamingChatServiceConfig) StreamingChatService {
	timeout := config.Timeout
	if timeout == 0 {
		timeout = 5 * time.Minute
	}

	writer := config.Writer
	if writer == nil {
		writer = os.Stdout
	}

	personality := config.Personality
	if personality == "" {
		personality = ux.PersonalityFull
	}

	parser := ux.NewSSEParser()

	verbosity := config.Verbosity
	if verbosity == 0 && config.Pipeline != "verified" {
		verbosity = 2 // Default to detailed for verified pipeline
	}

	return &ragStreamingChatService{
		client: &defaultHTTPClient{
			client: &http.Client{Timeout: timeout},
		},
		parser:      parser,
		reader:      ux.NewSSEStreamReader(parser),
		baseURL:     config.BaseURL,
		sessionID:   config.SessionID,
		pipeline:    config.Pipeline,
		writer:      writer,
		personality: personality,
		strictMode:  config.StrictMode,
		verbosity:   verbosity,
		dataSpace:   config.DataSpace,
		docVersion:  config.DocVersion,
		sessionTTL:  config.SessionTTL,
		recencyBias: config.RecencyBias,
	}
}

// NewRAGStreamingChatServiceWithClient creates a RAG streaming service with custom HTTP client.
//
// # Description
//
// Creates a ragStreamingChatService with injected HTTP client.
// Use this constructor for testing with mock clients.
//
// # Inputs
//
//   - client: HTTP client implementation (production or mock).
//   - config: Service configuration.
//
// # Outputs
//
//   - StreamingChatService: Ready-to-use streaming service.
//
// # Examples
//
//	mock := &mockHTTPClient{response: mockSSEResponse}
//	service := NewRAGStreamingChatServiceWithClient(mock, config)
//
// # Limitations
//
//   - Client must implement HTTPClient interface correctly
//
// # Assumptions
//
//   - Client is properly initialized
//   - Client handles context cancellation
func NewRAGStreamingChatServiceWithClient(client HTTPClient, config RAGStreamingChatServiceConfig) StreamingChatService {
	writer := config.Writer
	if writer == nil {
		writer = os.Stdout
	}

	personality := config.Personality
	if personality == "" {
		personality = ux.PersonalityFull
	}

	parser := ux.NewSSEParser()

	verbosity := config.Verbosity
	if verbosity == 0 && config.Pipeline != "verified" {
		verbosity = 2 // Default to detailed for verified pipeline
	}

	return &ragStreamingChatService{
		client:      client,
		parser:      parser,
		reader:      ux.NewSSEStreamReader(parser),
		baseURL:     config.BaseURL,
		sessionID:   config.SessionID,
		pipeline:    config.Pipeline,
		writer:      writer,
		personality: personality,
		strictMode:  config.StrictMode,
		verbosity:   verbosity,
		dataSpace:   config.DataSpace,
		docVersion:  config.DocVersion,
		sessionTTL:  config.SessionTTL,
		recencyBias: config.RecencyBias,
	}
}

// NewDirectStreamingChatService creates a new direct streaming chat service.
//
// # Description
//
// Creates a directStreamingChatService with production HTTP client.
// Initializes with system message for assistant personality.
//
// # Inputs
//
//   - config: Service configuration. Only BaseURL is required.
//
// # Outputs
//
//   - *directStreamingChatService: Ready-to-use streaming service.
//     Returns concrete type to expose LoadSessionHistory method.
//
// # Examples
//
//	service := NewDirectStreamingChatService(DirectStreamingChatServiceConfig{
//	    BaseURL:        "http://localhost:8080",
//	    EnableThinking: true,
//	})
//	defer service.Close()
//
// # Limitations
//
//   - Does not validate BaseURL format
//   - System message is hardcoded
//
// # Assumptions
//
//   - Caller will call Close when done
//   - BaseURL is valid and reachable
func NewDirectStreamingChatService(config DirectStreamingChatServiceConfig) *directStreamingChatService {
	timeout := config.Timeout
	if timeout == 0 {
		timeout = 5 * time.Minute
	}

	writer := config.Writer
	if writer == nil {
		writer = os.Stdout
	}

	personality := config.Personality
	if personality == "" {
		personality = ux.PersonalityFull
	}

	parser := ux.NewSSEParser()

	svc := &directStreamingChatService{
		client: &defaultHTTPClient{
			client: &http.Client{Timeout: timeout},
		},
		parser:         parser,
		reader:         ux.NewSSEStreamReader(parser),
		baseURL:        config.BaseURL,
		sessionID:      config.SessionID,
		enableThinking: config.EnableThinking,
		budgetTokens:   config.BudgetTokens,
		writer:         writer,
		personality:    personality,
		messages:       make([]datatypes.Message, 0, 10),
	}

	svc.messages = append(svc.messages, datatypes.Message{
		Role:    "system",
		Content: "You are a helpful, technically gifted assistant",
	})

	return svc
}

// NewDirectStreamingChatServiceWithClient creates a direct streaming service with custom HTTP client.
//
// # Description
//
// Creates a directStreamingChatService with injected HTTP client.
// Use this constructor for testing with mock clients.
//
// # Inputs
//
//   - client: HTTP client implementation (production or mock).
//   - config: Service configuration.
//
// # Outputs
//
//   - *directStreamingChatService: Ready-to-use streaming service.
//
// # Examples
//
//	mock := &mockHTTPClient{response: mockSSEResponse}
//	service := NewDirectStreamingChatServiceWithClient(mock, config)
//
// # Limitations
//
//   - Client must implement HTTPClient interface correctly
//
// # Assumptions
//
//   - Client is properly initialized
//   - Client handles context cancellation
func NewDirectStreamingChatServiceWithClient(client HTTPClient, config DirectStreamingChatServiceConfig) *directStreamingChatService {
	writer := config.Writer
	if writer == nil {
		writer = os.Stdout
	}

	personality := config.Personality
	if personality == "" {
		personality = ux.PersonalityFull
	}

	parser := ux.NewSSEParser()

	svc := &directStreamingChatService{
		client:         client,
		parser:         parser,
		reader:         ux.NewSSEStreamReader(parser),
		baseURL:        config.BaseURL,
		sessionID:      config.SessionID,
		enableThinking: config.EnableThinking,
		budgetTokens:   config.BudgetTokens,
		writer:         writer,
		personality:    personality,
		messages:       make([]datatypes.Message, 0, 10),
	}

	svc.messages = append(svc.messages, datatypes.Message{
		Role:    "system",
		Content: "You are a helpful, technically gifted assistant",
	})

	return svc
}

// =============================================================================
// RAG STREAMING CHAT SERVICE METHODS
// =============================================================================

// SendMessage sends a message and streams the RAG response.
//
// # Description
//
// Sends message to /v1/chat/rag/stream endpoint, parses SSE events,
// routes events to renderer, and returns accumulated result.
// Session ID is updated from done event.
//
// # Inputs
//
//   - ctx: Context for cancellation/timeout.
//   - message: User's input text.
//
// # Outputs
//
//   - *ux.StreamResult: Accumulated result with answer, sources, metrics.
//   - error: Non-nil on marshal, network, server, or parse errors.
//
// # Examples
//
//	result, err := service.SendMessage(ctx, "What is OAuth2?")
//	if err != nil {
//	    return fmt.Errorf("streaming failed: %w", err)
//	}
//	fmt.Printf("Sources: %d\n", len(result.Sources))
//
// # Limitations
//
//   - Does not retry on transient errors
//   - Partial results on context cancellation may be incomplete
//
// # Assumptions
//
//   - Server returns valid SSE format
//   - Server sends done event with session ID
func (s *ragStreamingChatService) SendMessage(ctx context.Context, message string) (*ux.StreamResult, error) {
	requestID := uuid.New().String()
	currentSessionID := s.getSessionID()

	slog.Debug("sending RAG streaming chat message",
		"request_id", requestID,
		"session_id", currentSessionID,
		"pipeline", s.pipeline,
		"message_length", len(message),
	)

	reqBody := s.buildRAGRequest(requestID, message, currentSessionID)

	resp, err := s.postRequest(ctx, requestID, reqBody)
	if err != nil {
		return nil, err
	}
	defer func(Body io.ReadCloser) {
		if err := Body.Close(); err != nil {
			slog.Error("failed to close response body", "error", err)
		}
	}(resp.Body)

	if err := s.validateResponse(requestID, resp); err != nil {
		return nil, err
	}

	result, newSessionID, err := s.processStream(ctx, requestID, resp.Body)
	if err != nil {
		return nil, err
	}

	s.updateSessionID(requestID, newSessionID)

	return result, nil
}

// getSessionID retrieves the current session ID with thread safety.
//
// # Description
//
// Thread-safe accessor for sessionID field.
//
// # Inputs
//
// None.
//
// # Outputs
//
//   - string: Current session ID.
//
// # Limitations
//
// None.
//
// # Assumptions
//
// Mutex is not held by caller.
func (s *ragStreamingChatService) getSessionID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sessionID
}

// buildRAGRequest constructs the request body for RAG streaming.
//
// # Description
//
// Creates a ChatRAGRequest with all required fields populated.
//
// # Inputs
//
//   - requestID: Unique identifier for this request.
//   - message: User's input text.
//   - sessionID: Current session ID (may be empty).
//
// # Outputs
//
//   - datatypes.ChatRAGRequest: Populated request struct.
//
// # Limitations
//
// None.
//
// # Assumptions
//
// All inputs are valid.
func (s *ragStreamingChatService) buildRAGRequest(requestID, message, sessionID string) datatypes.ChatRAGRequest {
	return datatypes.ChatRAGRequest{
		Id:          requestID,
		CreatedAt:   time.Now().UnixMilli(),
		Message:     message,
		SessionId:   sessionID,
		Pipeline:    s.pipeline,
		StrictMode:  s.strictMode,
		DataSpace:   s.dataSpace,
		VersionTag:  s.docVersion,
		SessionTTL:  s.sessionTTL,
		RecencyBias: s.recencyBias,
	}
}

// postRequest sends the HTTP POST request for streaming.
//
// # Description
//
// Marshals request body and sends POST to streaming endpoint.
// Routes to verified endpoint when pipeline is "verified" and includes
// X-Verbosity header for progress event filtering.
//
// # Inputs
//
//   - ctx: Context for cancellation.
//   - requestID: Request identifier for logging.
//   - reqBody: Request body to marshal and send.
//
// # Outputs
//
//   - *http.Response: HTTP response (caller must close Body).
//   - error: Non-nil on marshal or network errors.
//
// # Limitations
//
// Does not close response body on success.
//
// # Assumptions
//
// Caller will close response body.
func (s *ragStreamingChatService) postRequest(ctx context.Context, requestID string, reqBody datatypes.ChatRAGRequest) (*http.Response, error) {
	// Route to verified endpoint for verified pipeline
	var targetURL string
	if s.pipeline == "verified" {
		targetURL = fmt.Sprintf("%s/v1/chat/rag/verified/stream", s.baseURL)
	} else {
		targetURL = fmt.Sprintf("%s/v1/chat/rag/stream", s.baseURL)
	}

	postBody, err := json.Marshal(reqBody)
	if err != nil {
		slog.Error("failed to marshal RAG streaming request",
			"request_id", requestID,
			"error", err,
		)
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	resp, err := s.client.PostWithHeaders(ctx, targetURL, "application/json", bytes.NewBuffer(postBody), map[string]string{
		"X-Verbosity": fmt.Sprintf("%d", s.verbosity),
	})
	if err != nil {
		slog.Error("RAG streaming HTTP request failed",
			"request_id", requestID,
			"url", targetURL,
			"error", err,
		)
		return nil, fmt.Errorf("http post: %w", err)
	}

	return resp, nil
}

// validateResponse checks HTTP response status.
//
// # Description
//
// Validates that response has 200 OK status. Reads and logs error body
// for non-200 responses.
//
// # Inputs
//
//   - requestID: Request identifier for logging.
//   - resp: HTTP response to validate.
//
// # Outputs
//
//   - error: Non-nil if status is not 200.
//
// # Limitations
//
// Reads response body on error, consuming it.
//
// # Assumptions
//
// Response body is readable.
func (s *ragStreamingChatService) validateResponse(requestID string, resp *http.Response) error {
	if resp.StatusCode != http.StatusOK {
		bodyBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			slog.Error("RAG streaming server returned error (failed to read body)",
				"request_id", requestID,
				"status_code", resp.StatusCode,
				"read_error", err,
			)
			return fmt.Errorf("server error (%d): failed to read response body", resp.StatusCode)
		}
		slog.Error("RAG streaming server returned error",
			"request_id", requestID,
			"status_code", resp.StatusCode,
			"response_body", string(bodyBytes),
		)
		return fmt.Errorf("server error (%d): %s", resp.StatusCode, string(bodyBytes))
	}
	return nil
}

// processStream reads and renders the SSE stream.
//
// # Description
//
// Creates renderer, reads SSE events from body, routes events to renderer,
// and returns accumulated result with captured session ID.
//
// # Inputs
//
//   - ctx: Context for cancellation.
//   - requestID: Request identifier for logging.
//   - body: Response body containing SSE stream.
//
// # Outputs
//
//   - *ux.StreamResult: Accumulated result.
//   - string: Session ID from done event (may be empty).
//   - error: Non-nil on stream read errors.
//
// # Limitations
//
// Stream errors are returned as errors, not in result.
//
// # Assumptions
//
// Body contains valid SSE format.
func (s *ragStreamingChatService) processStream(ctx context.Context, requestID string, body io.Reader) (*ux.StreamResult, string, error) {
	renderer := ux.NewTerminalStreamRenderer(s.writer, s.personality)
	defer renderer.Finalize()

	var newSessionID string

	err := s.reader.Read(ctx, body, func(event ux.StreamEvent) error {
		switch event.Type {
		case ux.StreamEventStatus:
			renderer.OnStatus(ctx, event.Message)
		case ux.StreamEventToken:
			renderer.OnToken(ctx, event.Content)
		case ux.StreamEventThinking:
			renderer.OnThinking(ctx, event.Content)
		case ux.StreamEventSources:
			renderer.OnSources(ctx, event.Sources)
		case ux.StreamEventDone:
			newSessionID = event.SessionID
			renderer.OnDone(ctx, event.SessionID)
		case ux.StreamEventError:
			renderer.OnError(ctx, fmt.Errorf("%s", event.Error))
		}
		return nil
	})

	if err != nil {
		slog.Error("RAG stream reading failed",
			"request_id", requestID,
			"error", err,
		)
		return nil, "", fmt.Errorf("read stream: %w", err)
	}

	result := renderer.Result()
	result.RequestID = requestID

	slog.Debug("RAG streaming chat completed",
		"request_id", requestID,
		"session_id", result.SessionID,
		"total_tokens", result.TotalTokens,
		"duration_ms", result.Duration().Milliseconds(),
		"sources_count", len(result.Sources),
	)

	return result, newSessionID, nil
}

// updateSessionID stores the new session ID if changed.
//
// # Description
//
// Thread-safe update of sessionID field. Logs when ID changes.
//
// # Inputs
//
//   - requestID: Request identifier for logging.
//   - newSessionID: New session ID from done event.
//
// # Outputs
//
// None.
//
// # Limitations
//
// Empty newSessionID is ignored.
//
// # Assumptions
//
// Mutex is not held by caller.
func (s *ragStreamingChatService) updateSessionID(requestID, newSessionID string) {
	if newSessionID == "" {
		return
	}

	s.mu.Lock()
	oldSessionID := s.sessionID
	s.sessionID = newSessionID
	s.mu.Unlock()

	if oldSessionID != newSessionID {
		slog.Info("RAG session ID updated from stream",
			"request_id", requestID,
			"old_session_id", oldSessionID,
			"new_session_id", newSessionID,
		)
	}
}

// GetSessionID returns the current session ID.
//
// # Description
//
// Returns server-assigned session ID for multi-turn conversations.
//
// # Inputs
//
// None.
//
// # Outputs
//
//   - string: Current session ID, or empty if no session.
//
// # Examples
//
//	sessionID := service.GetSessionID()
//
// # Limitations
//
// Returns empty string before first successful SendMessage.
//
// # Assumptions
//
// Thread-safe; mutex protects access.
func (s *ragStreamingChatService) GetSessionID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sessionID
}

// Close releases resources held by the service.
//
// # Description
//
// No-op for HTTP-based implementation. Provided for interface compliance.
//
// # Inputs
//
// None.
//
// # Outputs
//
//   - error: Always nil.
//
// # Examples
//
//	defer service.Close()
//
// # Limitations
//
// Does not cancel in-flight requests.
//
// # Assumptions
//
// None.
func (s *ragStreamingChatService) Close() error {
	return nil
}

// =============================================================================
// DIRECT STREAMING CHAT SERVICE METHODS
// =============================================================================

// SendMessage sends a message and streams the direct chat response.
//
// # Description
//
// Appends user message to history, sends to /v1/chat/direct/stream endpoint,
// parses SSE events, routes events to renderer, and returns accumulated result.
// On error, user message is removed from history to maintain consistency.
//
// # Inputs
//
//   - ctx: Context for cancellation/timeout.
//   - message: User's input text.
//
// # Outputs
//
//   - *ux.StreamResult: Accumulated result with answer and metrics.
//   - error: Non-nil on marshal, network, server, or parse errors.
//
// # Examples
//
//	result, err := service.SendMessage(ctx, "Explain quantum computing")
//	if err != nil {
//	    return fmt.Errorf("streaming failed: %w", err)
//	}
//	fmt.Printf("Thinking: %s\n", result.Thinking)
//
// # Limitations
//
//   - Does not retry on transient errors
//   - Message history grows unbounded
//
// # Assumptions
//
//   - Server accepts full message history
//   - Server returns valid SSE format
func (s *directStreamingChatService) SendMessage(ctx context.Context, message string) (*ux.StreamResult, error) {
	requestID := uuid.New().String()

	s.mu.Lock()
	defer s.mu.Unlock()

	slog.Debug("sending direct streaming chat message",
		"request_id", requestID,
		"session_id", s.sessionID,
		"message_length", len(message),
		"history_length", len(s.messages),
		"thinking_enabled", s.enableThinking,
	)

	s.messages = append(s.messages, datatypes.Message{Role: "user", Content: message})

	result, err := s.executeStreamingRequest(ctx, requestID)
	if err != nil {
		s.removeLastMessageLocked()
		return nil, err
	}

	if err := s.validateResult(requestID, result); err != nil {
		s.removeLastMessageLocked()
		return result, err
	}

	s.messages = append(s.messages, datatypes.Message{Role: "assistant", Content: result.Answer})

	slog.Debug("direct streaming chat completed",
		"request_id", requestID,
		"session_id", s.sessionID,
		"total_tokens", result.TotalTokens,
		"thinking_tokens", result.ThinkingTokens,
		"duration_ms", result.Duration().Milliseconds(),
		"new_history_length", len(s.messages),
	)

	return result, nil
}

// executeStreamingRequest performs the HTTP request and stream processing.
//
// # Description
//
// Marshals request, sends to endpoint, validates response, and processes stream.
// Must be called while holding s.mu lock.
//
// # Inputs
//
//   - ctx: Context for cancellation.
//   - requestID: Request identifier for logging.
//
// # Outputs
//
//   - *ux.StreamResult: Accumulated result.
//   - error: Non-nil on any failure.
//
// # Limitations
//
// Must be called while holding lock.
//
// # Assumptions
//
// s.messages contains current history including new user message.
func (s *directStreamingChatService) executeStreamingRequest(ctx context.Context, requestID string) (*ux.StreamResult, error) {
	targetURL := fmt.Sprintf("%s/v1/chat/direct/stream", s.baseURL)

	reqBody := DirectChatRequest{
		Messages:       s.messages,
		EnableThinking: s.enableThinking,
		BudgetTokens:   s.budgetTokens,
	}

	postBody, err := json.Marshal(reqBody)
	if err != nil {
		slog.Error("failed to marshal direct streaming request",
			"request_id", requestID,
			"error", err,
		)
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	resp, err := s.client.Post(ctx, targetURL, "application/json", bytes.NewBuffer(postBody))
	if err != nil {
		slog.Error("direct streaming HTTP request failed",
			"request_id", requestID,
			"url", targetURL,
			"error", err,
		)
		return nil, fmt.Errorf("http post: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			slog.Error("failed to close response body", "error", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			slog.Error("direct streaming server returned error (failed to read body)",
				"request_id", requestID,
				"status_code", resp.StatusCode,
				"read_error", err,
			)
			return nil, fmt.Errorf("server error (%d): failed to read response body", resp.StatusCode)
		}
		slog.Error("direct streaming server returned error",
			"request_id", requestID,
			"status_code", resp.StatusCode,
			"response_body", string(bodyBytes),
		)
		return nil, fmt.Errorf("server error (%d): %s", resp.StatusCode, string(bodyBytes))
	}

	return s.processDirectStream(ctx, requestID, resp.Body)
}

// processDirectStream reads and renders the SSE stream.
//
// # Description
//
// Creates renderer, reads SSE events from body, routes events to renderer,
// and returns accumulated result.
//
// # Inputs
//
//   - ctx: Context for cancellation.
//   - requestID: Request identifier for logging.
//   - body: Response body containing SSE stream.
//
// # Outputs
//
//   - *ux.StreamResult: Accumulated result.
//   - error: Non-nil on stream read errors.
//
// # Limitations
//
// Stream errors are returned as errors, not in result.
//
// # Assumptions
//
// Body contains valid SSE format.
func (s *directStreamingChatService) processDirectStream(ctx context.Context, requestID string, body io.Reader) (*ux.StreamResult, error) {
	renderer := ux.NewTerminalStreamRenderer(s.writer, s.personality)
	defer renderer.Finalize()

	err := s.reader.Read(ctx, body, func(event ux.StreamEvent) error {
		switch event.Type {
		case ux.StreamEventStatus:
			renderer.OnStatus(ctx, event.Message)
		case ux.StreamEventToken:
			renderer.OnToken(ctx, event.Content)
		case ux.StreamEventThinking:
			renderer.OnThinking(ctx, event.Content)
		case ux.StreamEventSources:
			renderer.OnSources(ctx, event.Sources)
		case ux.StreamEventDone:
			renderer.OnDone(ctx, event.SessionID)
		case ux.StreamEventError:
			renderer.OnError(ctx, fmt.Errorf("%s", event.Error))
		}
		return nil
	})

	if err != nil {
		slog.Error("direct stream reading failed",
			"request_id", requestID,
			"error", err,
		)
		return nil, fmt.Errorf("read stream: %w", err)
	}

	result := renderer.Result()
	result.RequestID = requestID

	return result, nil
}

// validateResult checks if the result is valid for history update.
//
// # Description
//
// Validates that result has non-empty answer and no error.
//
// # Inputs
//
//   - requestID: Request identifier for logging.
//   - result: Result to validate.
//
// # Outputs
//
//   - error: Non-nil if result is invalid.
//
// # Limitations
//
// Returns result along with error for empty/error cases.
//
// # Assumptions
//
// Result is not nil.
func (s *directStreamingChatService) validateResult(requestID string, result *ux.StreamResult) error {
	if result.Answer == "" && result.Error == "" {
		slog.Warn("direct streaming returned empty response",
			"request_id", requestID,
		)
		return fmt.Errorf("empty response from server")
	}

	if result.HasError() {
		slog.Warn("direct streaming ended with error",
			"request_id", requestID,
			"error", result.Error,
		)
		return fmt.Errorf("stream error: %s", result.Error)
	}

	return nil
}

// removeLastMessageLocked removes the last message from history.
//
// # Description
//
// Removes last message on error to maintain history consistency.
// Must be called while holding s.mu lock.
//
// # Inputs
//
// None.
//
// # Outputs
//
// None.
//
// # Limitations
//
// No-op if messages is empty.
//
// # Assumptions
//
// Caller holds s.mu lock.
func (s *directStreamingChatService) removeLastMessageLocked() {
	if len(s.messages) > 0 {
		s.messages = s.messages[:len(s.messages)-1]
	}
}

// GetSessionID returns the client-side session identifier.
//
// # Description
//
// Returns session ID for client tracking. Not used by server.
//
// # Inputs
//
// None.
//
// # Outputs
//
//   - string: Client-side session ID, or empty.
//
// # Examples
//
//	sessionID := service.GetSessionID()
//
// # Limitations
//
// Session ID is for client tracking only.
//
// # Assumptions
//
// Thread-safe; mutex protects access.
func (s *directStreamingChatService) GetSessionID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sessionID
}

// Close releases resources held by the service.
//
// # Description
//
// No-op for HTTP-based implementation. Provided for interface compliance.
//
// # Inputs
//
// None.
//
// # Outputs
//
//   - error: Always nil.
//
// # Examples
//
//	defer service.Close()
//
// # Limitations
//
// Does not cancel in-flight requests.
//
// # Assumptions
//
// None.
func (s *directStreamingChatService) Close() error {
	return nil
}

// LoadSessionHistory loads previous conversation history for session resume.
//
// # Description
//
// Fetches conversation history from server and populates message history.
// Allows continuation of a previous conversation.
//
// # Inputs
//
//   - ctx: Context for cancellation/timeout.
//   - sessionID: Session ID to resume (URL-escaped for security).
//
// # Outputs
//
//   - int: Number of conversation turns loaded.
//   - error: Non-nil if loading failed.
//
// # Examples
//
//	turns, err := service.LoadSessionHistory(ctx, "sess-abc123")
//	if err != nil {
//	    log.Fatal(err)
//	}
//	fmt.Printf("Loaded %d previous turns\n", turns)
//
// # Limitations
//
//   - Replaces current history entirely
//   - Requires server to support history endpoint
//
// # Assumptions
//
//   - SessionID is valid and exists on server
//   - Server returns Weaviate-style response format
func (s *directStreamingChatService) LoadSessionHistory(ctx context.Context, sessionID string) (int, error) {
	escapedSessionID := url.PathEscape(sessionID)
	historyURL := fmt.Sprintf("%s/v1/sessions/%s/history", s.baseURL, escapedSessionID)

	slog.Debug("loading session history for streaming service",
		"session_id", sessionID,
		"escaped_session_id", escapedSessionID,
		"url", historyURL,
	)

	resp, err := s.client.Get(ctx, historyURL)
	if err != nil {
		slog.Error("failed to load session history",
			"session_id", sessionID,
			"error", err,
		)
		return 0, fmt.Errorf("http get: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			slog.Error("failed to close response body", "error", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		slog.Error("session history request failed",
			"session_id", sessionID,
			"status_code", resp.StatusCode,
		)
		return 0, fmt.Errorf("failed to get history (status %d)", resp.StatusCode)
	}

	return s.parseAndLoadHistory(sessionID, resp.Body)
}

// parseAndLoadHistory parses history response and updates messages.
//
// # Description
//
// Parses Weaviate-style response and populates message history.
// Thread-safe; acquires lock for state mutation.
//
// # Inputs
//
//   - sessionID: Session ID being loaded.
//   - body: Response body to parse.
//
// # Outputs
//
//   - int: Number of turns loaded.
//   - error: Non-nil on parse errors or missing data.
//
// # Limitations
//
// Expects specific Weaviate response format.
//
// # Assumptions
//
// Response format matches expected structure.
func (s *directStreamingChatService) parseAndLoadHistory(sessionID string, body io.Reader) (int, error) {
	type HistoryTurn struct {
		Question string `json:"question"`
		Answer   string `json:"answer"`
	}
	var historyResp map[string]map[string][]HistoryTurn

	if err := json.NewDecoder(body).Decode(&historyResp); err != nil {
		slog.Error("failed to parse session history",
			"session_id", sessionID,
			"error", err,
		)
		return 0, fmt.Errorf("parse history: %w", err)
	}

	history, ok := historyResp["Get"]["Conversation"]
	if !ok {
		slog.Warn("no conversation data in history response",
			"session_id", sessionID,
		)
		return 0, fmt.Errorf("no conversation data in history response")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.messages = make([]datatypes.Message, 0, len(history)*2+1)
	s.messages = append(s.messages, datatypes.Message{
		Role:    "system",
		Content: "You are a helpful, technically gifted assistant",
	})

	for _, turn := range history {
		s.messages = append(s.messages, datatypes.Message{Role: "user", Content: turn.Question})
		s.messages = append(s.messages, datatypes.Message{Role: "assistant", Content: turn.Answer})
	}

	s.sessionID = sessionID

	slog.Info("session history loaded for streaming service",
		"session_id", sessionID,
		"turns_loaded", len(history),
		"total_messages", len(s.messages),
	)

	return len(history), nil
}

// =============================================================================
// COMPILE-TIME INTERFACE CHECKS
// =============================================================================

var _ StreamingChatService = (*ragStreamingChatService)(nil)
var _ StreamingChatService = (*directStreamingChatService)(nil)
