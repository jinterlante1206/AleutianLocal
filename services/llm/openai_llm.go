package llm

import (
	"context"
	"fmt"
	"github.com/sashabaranov/go-openai"
	"log/slog"
	"os"
	"strings"
)

type OpenAIClient struct {
	client *openai.Client
	model  string
}

func NewOpenAIClient() (*OpenAIClient, error) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	model := os.Getenv("OPENAI_MODEL") // e.g., "gpt-4o"
	if apiKey == "" {
		secretPath := "/run/secrets/openai_api_key"
		apiKeyBytes, err := os.ReadFile(secretPath)
		if err == nil {
			apiKey = strings.TrimSpace(string(apiKeyBytes))
			slog.Info("Read the OpenAI API Key from Podman Secrets")
		} else {
			slog.Error("OPENAI_API_KEY environment variable not set and secret not found", "path", secretPath)
			return nil, fmt.Errorf("OPENAI_API_KEY environment variable not set")
		}
	}
	if model == "" {
		model = "gpt-4o-mini"
		slog.Warn("OPENAI_MODEL not set, defaulting to gpt-4o-mini")
	}
	slog.Info("Initializing OpenAI client", "model", model)
	return &OpenAIClient{
		client: openai.NewClient(apiKey),
		model:  model,
	}, nil
}

// Generate implements the LLMClient interface
func (o *OpenAIClient) Generate(ctx context.Context, prompt string, params GenerationParams) (string, error) {
	slog.Debug("Generating text via OpenAI", "model", o.model)
	systemRoleContent := os.Getenv("SYSTEM_ROLE_PROMPT_PERSONA")
	if systemRoleContent == "" {
		systemRoleContent = "You are a helpful assistant."
	}
	req := openai.ChatCompletionRequest{
		Model: o.model,
		Messages: []openai.ChatCompletionMessage{
			{Role: openai.ChatMessageRoleSystem, Content: systemRoleContent},
			{Role: openai.ChatMessageRoleUser, Content: prompt},
		},
	}
	if params.Temperature != nil {
		req.Temperature = *params.Temperature
	}
	if params.MaxTokens != nil {
		req.MaxCompletionTokens = *params.MaxTokens
	}
	if params.TopP != nil {
		req.TopP = *params.TopP
	}
	if len(params.Stop) > 0 {
		req.Stop = params.Stop
	}

	resp, err := o.client.CreateChatCompletion(ctx, req)
	if err != nil {
		slog.Error("OpenAI API call failed", "error", err)
		return "", fmt.Errorf("OpenAI API call failed: %w", err)
	}

	if len(resp.Choices) == 0 {
		slog.Warn("OpenAI returned no choices or empty content")
		return "", fmt.Errorf("OpenAI returned no choices")
	}
	slog.Debug("Received response from OpenAI", "finish_reason", resp.Choices[0].FinishReason)
	return resp.Choices[0].Message.Content, nil
}
