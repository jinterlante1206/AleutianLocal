package llm

import "context"

type GenerationParams struct {
	Temperature *float32 `json:"temperature"`
	TopK        *int     `json:"top_k"`
	TopP        *float32 `json:"top_p"`
	MaxTokens   *int     `json:"max_tokens"`
	Stop        []string `json:"stop"`
}

// LLMClient defines the standard interface for any LLM backend
// TODO: Add more methods to this interface.
type LLMClient interface {
	Generate(ctx context.Context, prompt string, params GenerationParams) (string, error)
}
