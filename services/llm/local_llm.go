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

type LocalLlamaCppClient struct {
	httpClient *http.Client `json:"http_client"`
	baseURL    string       `json:"base_url"`
}

type LocalLlamaCppClientPayload struct {
	Prompt      string   `json:"prompt"`
	NPredict    int      `json:"n_predict"`
	Temperature *float32 `json:"temperature,omitempty"`
	TopK        *int     `json:"top_k,omitempty"`
	TopP        *float32 `json:"top_p,omitempty"`
	MaxTokens   *int     `json:"max_tokens,omitempty"`
	Stop        []string `json:"stop,omitempty"`
}

func NewLocalLlamaCppClient() (*LocalLlamaCppClient, error) {
	baseURL := os.Getenv("LLM_SERVICE_URL_BASE")
	if baseURL == "" {
		return nil, fmt.Errorf("LLM_SERVICE_URL_BASE environment variable not set")
	}
	baseURL = strings.TrimSuffix(baseURL, "/")
	return &LocalLlamaCppClient{
		httpClient: &http.Client{Timeout: 5 * time.Minute},
		baseURL:    baseURL,
	}, nil
}

// Generate implements the LLMClient interface
func (l *LocalLlamaCppClient) Generate(ctx context.Context, prompt string,
	params GenerationParams) (string, error) {

	completionURL := l.baseURL + "/completion"
	payload := LocalLlamaCppClientPayload{Prompt: prompt}
	if params.MaxTokens != nil {
		payload.NPredict = *params.MaxTokens
	} else {
		payload.NPredict = 512
	}
	if params.Temperature != nil {
		payload.Temperature = params.Temperature
	} else {
		var defaultTemperature float32 = 0.2
		payload.Temperature = &defaultTemperature
	}
	if params.TopK != nil {
		payload.TopK = params.TopK
	} else {
		defaultTopK := 20
		payload.TopK = &defaultTopK
	}
	if params.TopP != nil {
		payload.TopP = params.TopP
	} else {
		var defaultTopP float32 = 0.9
		payload.TopP = &defaultTopP
	}
	if params.MaxTokens != nil {
		payload.MaxTokens = params.MaxTokens
	} else {
		var defaultMaxTokens int = 2048
		params.MaxTokens = &defaultMaxTokens
	}
	if params.Stop != nil {
		payload.Stop = params.Stop
	} else {
		payload.Stop = []string{"\n"}
	}

	reqBodyBytes, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("Failed to marshal the payload %w", err)
	}
	slog.Info("Calling Llama.cpp Generate", "url", completionURL)
	resp, err := l.httpClient.Post(completionURL, "application/json", bytes.NewBuffer(reqBodyBytes))
	if err != nil {
		return "", fmt.Errorf("failed to make a request to the llm: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read the llm's response: %w", err)
	}
	var llmResponseBody llamaCppResp
	if err := json.Unmarshal(body, &llmResponseBody); err != nil {
		return "", fmt.Errorf("failed to parse the llm response %w", err)
	}
	return llmResponseBody.Content, nil
}

type llamaCppResp struct {
	Content string `json:"content"`
}

// Chat TODO: Implement
func (l *LocalLlamaCppClient) Chat(ctx context.Context, messages []datatypes.Message,
	params GenerationParams) (string, error) {
	slog.Warn("LocalLlamaCppClient.Chat is not fully implemented yet. Using Generate with last message.")
	return "", fmt.Errorf("chat method not implemented for LocalLlamaCppClient")
}
