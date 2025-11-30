package llm

import (
	"context"

	"github.com/jinterlante1206/AleutianLocal/services/orchestrator/datatypes"
)

type GenerationParams struct {
	Temperature     *float32      `json:"temperature"`
	TopK            *int          `json:"top_k"`
	TopP            *float32      `json:"top_p"`
	MaxTokens       *int          `json:"max_tokens"`
	Stop            []string      `json:"stop"`
	ToolDefinitions []interface{} `json:"tools,omitempty"`
	EnableThinking  bool          `json:"thinking,omitempty"`
	BudgetTokens    int           `json:"budget_tokens,omitempty"`
}

// LLMClient defines the standard interface for any LLM backend
// TODO: Add more methods to this interface.
type LLMClient interface {
	Generate(ctx context.Context, prompt string, params GenerationParams) (string, error)
	Chat(ctx context.Context, messages []datatypes.Message, params GenerationParams) (string, error)
}
