package llm

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

	"github.com/jinterlante1206/AleutianLocal/services/orchestrator/datatypes"
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
