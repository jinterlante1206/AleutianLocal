package llm

import (
	"context"
	"fmt"
	"net/http"

	"github.com/jinterlante1206/AleutianLocal/services/orchestrator/datatypes"
)

type HFTransformersClient struct {
	httpClient *http.Client
	baseURL    string
}

// Generate produces text from a single prompt.
//
// # Description
//
// Currently not implemented for HFTransformersClient. Returns an error
// indicating that this backend is not implemented.
//
// # Inputs
//
//   - ctx: Context for cancellation and timeout.
//   - prompt: Text prompt to complete.
//   - params: Generation parameters.
//
// # Outputs
//
//   - string: Empty string.
//   - error: Always returns not implemented error.
//
// # Limitations
//
//   - Not implemented.
//
// # Assumptions
//
//   - None.
func (h *HFTransformersClient) Generate(ctx context.Context, prompt string,
	params GenerationParams) (string, error) {
	return "", fmt.Errorf("Generate not implemented for HFTransformersClient")
}

// Chat conducts a conversation with message history.
//
// # Description
//
// Currently not implemented for HFTransformersClient. Returns an error
// indicating that this backend is not implemented.
//
// # Inputs
//
//   - ctx: Context for cancellation and timeout.
//   - messages: Conversation history.
//   - params: Generation parameters.
//
// # Outputs
//
//   - string: Empty string.
//   - error: Always returns not implemented error.
//
// # Limitations
//
//   - Not implemented.
//
// # Assumptions
//
//   - None.
func (h *HFTransformersClient) Chat(ctx context.Context, messages []datatypes.Message,
	params GenerationParams) (string, error) {
	return "", fmt.Errorf("Chat not implemented for HFTransformersClient")
}

// ChatStream streams a conversation response token-by-token.
//
// # Description
//
// Currently not implemented for HFTransformersClient. Returns an error
// indicating that streaming is not supported for this backend.
//
// # Inputs
//
//   - ctx: Context for cancellation and timeout.
//   - messages: Conversation history.
//   - params: Generation parameters.
//   - callback: Callback for streaming events.
//
// # Outputs
//
//   - error: Always returns ErrStreamingNotSupported.
//
// # Limitations
//
//   - Streaming is not implemented for HuggingFace Transformers backend.
//
// # Assumptions
//
//   - None.
func (h *HFTransformersClient) ChatStream(ctx context.Context, messages []datatypes.Message,
	params GenerationParams, callback StreamCallback) error {
	return fmt.Errorf("streaming not supported for HFTransformersClient")
}
