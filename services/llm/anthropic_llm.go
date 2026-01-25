// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package llm

import (
	"bufio"
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

	"github.com/AleutianAI/AleutianFOSS/services/orchestrator/datatypes"
)

const (
	anthropicAPIVersion = "2023-06-01"
	defaultBaseURL      = "https://api.anthropic.com/v1/messages"
)

type anthropicRequest struct {
	Model     string             `json:"model"`
	Messages  []anthropicMessage `json:"messages"`
	System    []systemBlock      `json:"system,omitempty"` // Top-level system prompt
	MaxTokens int                `json:"max_tokens"`
	// Optional params
	Thinking *thinkingParams   `json:"thinking,omitempty"`
	Tools    []toolsDefinition `json:"tools,omitempty"`

	Temperature *float32 `json:"temperature,omitempty"`
	TopP        *float32 `json:"top_p,omitempty"`
	TopK        *int     `json:"top_k,omitempty"`
	StopSeqs    []string `json:"stop_sequences,omitempty"`
	Stream      bool     `json:"stream,omitempty"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicResponse struct {
	ID      string             `json:"id"`
	Type    string             `json:"type"`
	Role    string             `json:"role"`
	Content []anthropicContent `json:"content"`
	Error   *anthropicError    `json:"error,omitempty"`
}

type systemBlock struct {
	Type         string        `json:"type"`
	Text         string        `json:"text"`
	CacheControl *cacheControl `json:"cache_control,omitempty"`
}

type thinkingParams struct {
	Type         string `json:"type"` // Must be "enabled"
	BudgetTokens int    `json:"budget_tokens"`
}

type cacheControl struct {
	Type string `json:"type"` // Must be "ephemeral"
}

type toolsDefinition struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	InputSchema interface{} `json:"input_schema"` // JSON Schema
}

type anthropicContent struct {
	Type     string `json:"type"`
	Text     string `json:"text"`
	Thinking string `json:"thinking,omitempty"`
}

type anthropicError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// --- Client Implementation ---

type AnthropicClient struct {
	httpClient *http.Client
	apiKey     string
	model      string
}

func NewAnthropicClient() (*AnthropicClient, error) {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	model := os.Getenv("CLAUDE_MODEL")

	// 1. Robust Secret Loading
	if apiKey == "" {
		secretPath := "/run/secrets/anthropic_api_key"
		if content, err := os.ReadFile(secretPath); err == nil {
			apiKey = strings.TrimSpace(string(content))
			slog.Info("Read Anthropic API Key from Podman Secrets")
		}
	}

	// 2. Graceful Failure
	if apiKey == "" {
		slog.Warn("Anthropic API Key is missing.")
		return nil, fmt.Errorf("ANTHROPIC_API_KEY is missing")
	}

	if model == "" {
		// Because we are using raw strings now, we can default to the ID directly
		// without needing a library constant.
		model = "claude-3-5-sonnet-20240620"
		slog.Info("CLAUDE_MODEL not set, defaulting to", "model", model)
	}

	return &AnthropicClient{
		httpClient: &http.Client{Timeout: 60 * time.Second},
		apiKey:     apiKey,
		model:      model,
	}, nil
}

// Generate implements the LLMClient interface
func (a *AnthropicClient) Generate(ctx context.Context, prompt string, params GenerationParams) (string, error) {
	messages := []datatypes.Message{
		{Role: "user", Content: prompt},
	}
	return a.Chat(ctx, messages, params)
}

// Chat implements the LLMClient interface
func (a *AnthropicClient) Chat(ctx context.Context, messages []datatypes.Message, params GenerationParams) (string, error) {
	var apiMessages []anthropicMessage
	var systemPrompt string

	// 1. Convert generic messages to Anthropic format
	for _, msg := range messages {
		if strings.ToLower(msg.Role) == "system" {
			systemPrompt = msg.Content
			continue
		}

		role := msg.Role
		// Map "assistant" (standard) to "assistant" (anthropic) - usually same
		// Map "user" to "user"

		apiMessages = append(apiMessages, anthropicMessage{
			Role:    role,
			Content: msg.Content,
		})
	}

	// Handle System Prompt with Caching
	var systemBlocks []systemBlock
	if systemPrompt != "" {
		block := systemBlock{
			Type: "text",
			Text: systemPrompt,
		}
		if len(systemPrompt) > 1024 {
			block.CacheControl = &cacheControl{Type: "ephemeral"}
		}
		systemBlocks = append(systemBlocks, block)
	}

	// Build Payload
	reqPayload := anthropicRequest{
		Model:     a.model,
		Messages:  apiMessages,
		System:    systemBlocks,
		MaxTokens: 4096,
	}

	if len(params.ToolDefinitions) > 0 {
		var tools []toolsDefinition
		bytes, _ := json.Marshal(params.ToolDefinitions)
		_ = json.Unmarshal(bytes, &tools)
		reqPayload.Tools = tools
	}

	// Enable Thinking if requested
	if params.EnableThinking {
		minRequired := params.BudgetTokens + 2048 // Budget + Room for answer
		if reqPayload.MaxTokens < minRequired {
			slog.Info("Adjusting MaxTokens to accommodate Thinking budget", "old", reqPayload.MaxTokens, "new", minRequired)
			reqPayload.MaxTokens = minRequired
		}
	}

	reqBodyBytes, err := json.Marshal(reqPayload)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", defaultBaseURL, bytes.NewBuffer(reqBodyBytes))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("x-api-key", a.apiKey)
	req.Header.Set("anthropic-version", anthropicAPIVersion)
	req.Header.Set("content-type", "application/json")

	slog.Debug("Sending REST request to Anthropic", "model", a.model)

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	slog.Info("Raw Anthropic Response", "status", resp.StatusCode, "body_length", len(bodyBytes), "body_snippet", string(bodyBytes))

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("anthropic API returned status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var apiResp anthropicResponse
	if err := json.Unmarshal(bodyBytes, &apiResp); err != nil {
		return "", fmt.Errorf("failed to parse response JSON: %w", err)
	}

	if apiResp.Error != nil {
		return "", fmt.Errorf("anthropic API error: %s - %s", apiResp.Error.Type, apiResp.Error.Message)
	}

	if len(apiResp.Content) == 0 {
		return "", fmt.Errorf("received empty content from Anthropic")
	}

	finalText := ""

	for _, block := range apiResp.Content {
		if block.Type == "text" {
			finalText += block.Text
		}
		if block.Type == "thinking" {
			slog.Info("Claude Thoughts", "thinking", block.Thinking)
		}
	}

	if finalText == "" {
		return "", fmt.Errorf("received content but no text block found (check logs for thoughts)")
	}

	return finalText, nil
}

// =============================================================================
// Streaming Types (for SSE parsing)
// =============================================================================

// anthropicStreamEvent represents a single SSE event from Anthropic.
type anthropicStreamEvent struct {
	Type string `json:"type"`
}

// anthropicContentBlockDelta contains delta content for streaming.
type anthropicContentBlockDelta struct {
	Type  string                `json:"type"`
	Index int                   `json:"index"`
	Delta anthropicDeltaContent `json:"delta"`
}

// anthropicDeltaContent contains the actual text delta.
type anthropicDeltaContent struct {
	Type     string `json:"type"` // "text_delta" or "thinking_delta"
	Text     string `json:"text,omitempty"`
	Thinking string `json:"thinking,omitempty"`
}

// anthropicMessageDelta contains the message-level delta (stop reason, etc).
type anthropicMessageDelta struct {
	Type  string `json:"type"`
	Delta struct {
		StopReason string `json:"stop_reason,omitempty"`
	} `json:"delta"`
}

// anthropicStreamError represents an error event in the stream.
type anthropicStreamError struct {
	Type  string `json:"type"`
	Error struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

// =============================================================================
// Streaming Implementation
// =============================================================================

// ChatStream implements streaming chat for the LLMClient interface.
//
// # Description
//
// Sends a chat request to Anthropic with streaming enabled, then reads
// the SSE response line-by-line and calls the callback for each token.
// Handles both regular text tokens and thinking tokens.
//
// # Inputs
//
//   - ctx: Context for cancellation and timeout.
//   - messages: Conversation history.
//   - params: Generation parameters.
//   - callback: Called for each streaming event.
//
// # Outputs
//
//   - error: Non-nil on network failure, API error, or callback abort.
//
// # Examples
//
//	err := client.ChatStream(ctx, messages, params, func(e StreamEvent) error {
//	    if e.Type == StreamEventToken {
//	        fmt.Print(e.Content)
//	    }
//	    return nil
//	})
//
// # Limitations
//
//   - Requires valid Anthropic API key
//   - Timeout applies to entire stream duration
//
// # Assumptions
//
//   - Anthropic API is available
//   - Network is stable for stream duration
func (a *AnthropicClient) ChatStream(
	ctx context.Context,
	messages []datatypes.Message,
	params GenerationParams,
	callback StreamCallback,
) error {
	// Build the streaming request (reuse logic from Chat)
	reqPayload, err := a.buildStreamRequest(messages, params)
	if err != nil {
		return err
	}

	// Create HTTP request
	reqBodyBytes, err := json.Marshal(reqPayload)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", defaultBaseURL, bytes.NewBuffer(reqBodyBytes))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("x-api-key", a.apiKey)
	req.Header.Set("anthropic-version", anthropicAPIVersion)
	req.Header.Set("content-type", "application/json")
	req.Header.Set("accept", "text/event-stream")

	slog.Debug("Sending streaming request to Anthropic", "model", a.model)

	// Use a longer timeout for streaming
	streamClient := &http.Client{Timeout: 5 * time.Minute}
	resp, err := streamClient.Do(req)
	if err != nil {
		// Send error event to callback
		_ = callback(StreamEvent{Type: StreamEventError, Error: err.Error()})
		return fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		errMsg := fmt.Sprintf("Anthropic API returned status %d", resp.StatusCode)
		_ = callback(StreamEvent{Type: StreamEventError, Error: errMsg})
		return fmt.Errorf("%s: %s", errMsg, string(bodyBytes))
	}

	// Process SSE stream
	return a.processSSEStream(ctx, resp.Body, callback)
}

// buildStreamRequest creates the Anthropic request payload with streaming enabled.
//
// # Description
//
// Builds the request payload similar to Chat but with Stream: true.
// Extracts system prompts and converts messages to Anthropic format.
//
// # Inputs
//
//   - messages: Conversation history.
//   - params: Generation parameters.
//
// # Outputs
//
//   - anthropicRequest: Request payload ready for JSON marshaling.
//   - error: Non-nil if construction fails.
func (a *AnthropicClient) buildStreamRequest(
	messages []datatypes.Message,
	params GenerationParams,
) (anthropicRequest, error) {
	var apiMessages []anthropicMessage
	var systemPrompt string

	// Convert generic messages to Anthropic format
	for _, msg := range messages {
		if strings.ToLower(msg.Role) == "system" {
			systemPrompt = msg.Content
			continue
		}
		apiMessages = append(apiMessages, anthropicMessage{
			Role:    msg.Role,
			Content: msg.Content,
		})
	}

	// Handle System Prompt with Caching
	var systemBlocks []systemBlock
	if systemPrompt != "" {
		block := systemBlock{
			Type: "text",
			Text: systemPrompt,
		}
		if len(systemPrompt) > 1024 {
			block.CacheControl = &cacheControl{Type: "ephemeral"}
		}
		systemBlocks = append(systemBlocks, block)
	}

	// Build Payload with streaming enabled
	reqPayload := anthropicRequest{
		Model:     a.model,
		Messages:  apiMessages,
		System:    systemBlocks,
		MaxTokens: 4096,
		Stream:    true, // Enable streaming
	}

	// Apply optional parameters
	if params.Temperature != nil {
		reqPayload.Temperature = params.Temperature
	}
	if params.TopP != nil {
		reqPayload.TopP = params.TopP
	}
	if params.TopK != nil {
		reqPayload.TopK = params.TopK
	}
	if len(params.Stop) > 0 {
		reqPayload.StopSeqs = params.Stop
	}

	// Handle tools
	if len(params.ToolDefinitions) > 0 {
		var tools []toolsDefinition
		toolBytes, _ := json.Marshal(params.ToolDefinitions)
		_ = json.Unmarshal(toolBytes, &tools)
		reqPayload.Tools = tools
	}

	// Enable Thinking if requested
	if params.EnableThinking {
		reqPayload.Thinking = &thinkingParams{
			Type:         "enabled",
			BudgetTokens: params.BudgetTokens,
		}
		minRequired := params.BudgetTokens + 2048
		if reqPayload.MaxTokens < minRequired {
			reqPayload.MaxTokens = minRequired
		}
	}

	return reqPayload, nil
}

// processSSEStream reads and processes the SSE event stream.
//
// # Description
//
// Reads the SSE stream line-by-line, parses events, and calls the
// callback for token and thinking events. Handles errors gracefully
// by calling the callback with an error event.
//
// # Inputs
//
//   - ctx: Context for cancellation.
//   - body: HTTP response body containing SSE events.
//   - callback: Called for each streaming event.
//
// # Outputs
//
//   - error: Non-nil on parse error, stream error, or callback abort.
func (a *AnthropicClient) processSSEStream(
	ctx context.Context,
	body io.Reader,
	callback StreamCallback,
) error {
	scanner := bufio.NewScanner(body)
	var eventType string
	var dataBuffer strings.Builder

	for scanner.Scan() {
		// Check for context cancellation
		select {
		case <-ctx.Done():
			_ = callback(StreamEvent{Type: StreamEventError, Error: "stream cancelled"})
			return ctx.Err()
		default:
		}

		line := scanner.Text()

		// Empty line signals end of event
		if line == "" {
			if dataBuffer.Len() > 0 && eventType != "" {
				if err := a.handleSSEEvent(eventType, dataBuffer.String(), callback); err != nil {
					return err
				}
				dataBuffer.Reset()
				eventType = ""
			}
			continue
		}

		// Parse SSE format
		if strings.HasPrefix(line, "event: ") {
			eventType = strings.TrimPrefix(line, "event: ")
		} else if strings.HasPrefix(line, "data: ") {
			dataBuffer.WriteString(strings.TrimPrefix(line, "data: "))
		}
	}

	if err := scanner.Err(); err != nil {
		_ = callback(StreamEvent{Type: StreamEventError, Error: err.Error()})
		return fmt.Errorf("stream read error: %w", err)
	}

	return nil
}

// handleSSEEvent processes a single SSE event.
//
// # Description
//
// Parses the SSE event data and calls the appropriate callback based
// on event type. Handles content_block_delta (tokens), error events,
// and message completion.
//
// # Inputs
//
//   - eventType: SSE event type (content_block_delta, error, etc.)
//   - data: JSON data payload.
//   - callback: Callback to invoke.
//
// # Outputs
//
//   - error: Non-nil on parse error or callback error.
func (a *AnthropicClient) handleSSEEvent(
	eventType string,
	data string,
	callback StreamCallback,
) error {
	switch eventType {
	case "content_block_delta":
		var delta anthropicContentBlockDelta
		if err := json.Unmarshal([]byte(data), &delta); err != nil {
			slog.Warn("Failed to parse content_block_delta", "error", err, "data", data)
			return nil // Don't fail on parse errors, continue stream
		}

		// Determine event type based on delta type
		switch delta.Delta.Type {
		case "text_delta":
			if delta.Delta.Text != "" {
				if err := callback(StreamEvent{
					Type:    StreamEventToken,
					Content: delta.Delta.Text,
				}); err != nil {
					return fmt.Errorf("callback error: %w", err)
				}
			}
		case "thinking_delta":
			if delta.Delta.Thinking != "" {
				if err := callback(StreamEvent{
					Type:    StreamEventThinking,
					Content: delta.Delta.Thinking,
				}); err != nil {
					return fmt.Errorf("callback error: %w", err)
				}
			}
		}

	case "error":
		var streamErr anthropicStreamError
		if err := json.Unmarshal([]byte(data), &streamErr); err != nil {
			slog.Warn("Failed to parse error event", "error", err, "data", data)
			_ = callback(StreamEvent{Type: StreamEventError, Error: "stream error"})
			return fmt.Errorf("stream error: %s", data)
		}
		errMsg := fmt.Sprintf("%s: %s", streamErr.Error.Type, streamErr.Error.Message)
		_ = callback(StreamEvent{Type: StreamEventError, Error: errMsg})
		return fmt.Errorf("Anthropic stream error: %s", errMsg)

	case "message_start", "content_block_start", "content_block_stop", "message_delta", "message_stop", "ping":
		// These are informational events, ignore them
		slog.Debug("Received SSE event", "type", eventType)

	default:
		slog.Debug("Unknown SSE event type", "type", eventType, "data", data)
	}

	return nil
}
