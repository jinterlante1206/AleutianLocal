package llm

import (
	"context"
	"fmt"
	"os"
	// Use the official OpenAI Go library or just HTTP
	"github.com/sashabaranov/go-openai"
)

type OpenAIClient struct {
	client *openai.Client
	model  string
}

func NewOpenAIClient() (*OpenAIClient, error) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	model := os.Getenv("OPENAI_MODEL") // e.g., "gpt-4o"
	if apiKey == "" {
		return nil, fmt.Errorf("OPENAI_API_KEY environment variable not set")
	}
	if model == "" {
		model = "gpt-4o-mini" // Default model
	}

	return &OpenAIClient{
		client: openai.NewClient(apiKey),
		model:  model,
	}, nil
}

// Generate implements the LLMClient interface
func (o *OpenAIClient) Generate(ctx context.Context, prompt string, params GenerationParams) (string, error) {
	req := openai.ChatCompletionRequest{
		Model: o.model,
		Messages: []openai.ChatCompletionMessage{
			{Role: openai.ChatMessageRoleUser, Content: prompt},
		},
	}
	if params.Temperature != nil {
		req.Temperature = *params.Temperature
	}
	if params.MaxTokens != nil {
		req.MaxTokens = *params.MaxTokens
	}

	resp, err := o.client.CreateChatCompletion(ctx, req)
	if err != nil {
		return "", fmt.Errorf("OpenAI API call failed: %w", err)
	}

	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("OpenAI returned no choices")
	}
	return resp.Choices[0].Message.Content, nil
}
