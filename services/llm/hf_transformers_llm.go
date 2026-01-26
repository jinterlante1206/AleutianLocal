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
	"fmt"
	"net/http"

	"github.com/AleutianAI/AleutianFOSS/services/orchestrator/datatypes"
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
