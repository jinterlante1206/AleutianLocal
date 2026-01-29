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
	"context"
	"log/slog"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/llm"
	"github.com/AleutianAI/AleutianFOSS/services/orchestrator/datatypes"
)

// OllamaAdapter adapts services/llm.OllamaClient to the agent's Client interface.
//
// Description:
//
//	OllamaAdapter wraps the existing OllamaClient to provide LLM capabilities
//	for the agent loop. This allows Code Buddy to use local Ollama models
//	(like gpt-oss:20b) for code assistance.
//
// Thread Safety:
//
//	OllamaAdapter is safe for concurrent use.
type OllamaAdapter struct {
	client *llm.OllamaClient
	model  string
}

// NewOllamaAdapter creates a new OllamaAdapter.
//
// Description:
//
//	Creates an adapter wrapping the provided OllamaClient.
//
// Inputs:
//
//	client - The OllamaClient to wrap. Must not be nil.
//	model - The model name for identification.
//
// Outputs:
//
//	*OllamaAdapter - The configured adapter.
//
// Example:
//
//	ollamaClient, _ := llm.NewOllamaClient()
//	adapter := NewOllamaAdapter(ollamaClient, "gpt-oss:20b")
func NewOllamaAdapter(client *llm.OllamaClient, model string) *OllamaAdapter {
	return &OllamaAdapter{
		client: client,
		model:  model,
	}
}

// Complete implements Client.
//
// Description:
//
//	Sends a completion request to Ollama and returns the response.
//	Converts between agent message format and Ollama's datatypes.Message format.
//
// Inputs:
//
//	ctx - Context for cancellation and timeout.
//	request - The completion request.
//
// Outputs:
//
//	*Response - The LLM response.
//	error - Non-nil if the request failed.
//
// Thread Safety: This method is safe for concurrent use.
func (a *OllamaAdapter) Complete(ctx context.Context, request *Request) (*Response, error) {
	if request == nil {
		slog.Warn("OllamaAdapter.Complete called with nil request")
		return &Response{
			Content:    "",
			StopReason: "end",
		}, nil
	}

	// Convert agent messages to datatypes.Message format
	messages := a.convertMessages(request)

	slog.Info("OllamaAdapter sending request",
		slog.String("model", a.model),
		slog.Int("message_count", len(messages)),
		slog.String("system_prompt_preview", truncate(request.SystemPrompt, 100)),
	)

	// Log each message for debugging (use Info level so it shows)
	for i, msg := range messages {
		slog.Info("OllamaAdapter message",
			slog.Int("index", i),
			slog.String("role", msg.Role),
			slog.Int("content_len", len(msg.Content)),
			slog.String("content_preview", truncate(msg.Content, 300)),
		)
	}

	// Build generation params
	params := a.buildParams(request)

	// Call Ollama
	startTime := time.Now()
	content, err := a.client.Chat(ctx, messages, params)
	if err != nil {
		slog.Error("OllamaAdapter.Chat failed",
			slog.String("error", err.Error()),
		)
		return nil, err
	}

	slog.Info("OllamaAdapter received response",
		slog.Int("content_len", len(content)),
		slog.String("content_preview", truncate(content, 200)),
		slog.Duration("duration", time.Since(startTime)),
	)

	// Build response
	duration := time.Since(startTime)
	return &Response{
		Content:      content,
		StopReason:   "end",
		TokensUsed:   estimateTokens(content),
		InputTokens:  estimateInputTokens(messages),
		OutputTokens: estimateTokens(content),
		Duration:     duration,
		Model:        a.model,
	}, nil
}

// truncate truncates a string to maxLen chars.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// Name implements Client.
func (a *OllamaAdapter) Name() string {
	return "ollama"
}

// Model implements Client.
func (a *OllamaAdapter) Model() string {
	return a.model
}

// convertMessages converts agent messages to Ollama format.
//
// Inputs:
//
//	request - The agent request containing messages.
//
// Outputs:
//
//	[]datatypes.Message - Messages in Ollama format.
func (a *OllamaAdapter) convertMessages(request *Request) []datatypes.Message {
	messages := make([]datatypes.Message, 0, len(request.Messages)+1)

	// Add system prompt as first message if present
	if request.SystemPrompt != "" {
		messages = append(messages, datatypes.Message{
			Role:    "system",
			Content: request.SystemPrompt,
		})
	}

	// Convert each message
	for _, msg := range request.Messages {
		messages = append(messages, datatypes.Message{
			Role:    msg.Role,
			Content: msg.Content,
		})
	}

	return messages
}

// buildParams converts agent request parameters to Ollama format.
//
// Inputs:
//
//	request - The agent request containing parameters.
//
// Outputs:
//
//	llm.GenerationParams - Parameters in Ollama format.
func (a *OllamaAdapter) buildParams(request *Request) llm.GenerationParams {
	params := llm.GenerationParams{}

	if request.MaxTokens > 0 {
		maxTokens := request.MaxTokens
		params.MaxTokens = &maxTokens
	}

	if request.Temperature > 0 {
		temp := float32(request.Temperature)
		params.Temperature = &temp
	}

	if len(request.StopSequences) > 0 {
		params.Stop = request.StopSequences
	}

	return params
}

// estimateTokens provides a rough token estimate.
//
// Description:
//
//	Estimates token count as ~4 characters per token.
//	This is a rough approximation; actual counts depend on the tokenizer.
//
// Inputs:
//
//	content - The text to estimate tokens for.
//
// Outputs:
//
//	int - Estimated token count.
func estimateTokens(content string) int {
	return len(content) / 4
}

// estimateInputTokens estimates input tokens from messages.
//
// Inputs:
//
//	messages - The input messages.
//
// Outputs:
//
//	int - Estimated input token count.
func estimateInputTokens(messages []datatypes.Message) int {
	total := 0
	for _, msg := range messages {
		total += len(msg.Content)
	}
	return total / 4
}
