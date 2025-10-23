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
)

type OllamaClient struct {
	httpClient *http.Client
	baseURL    string
	model      string
}

// Ollama API request structure
type ollamaGenerateRequest struct {
	Model   string                 `json:"model"`
	Prompt  string                 `json:"prompt"`
	Stream  bool                   `json:"stream"`
	Options map[string]interface{} `json:"options,omitempty"`
}

type ollamaGenerateResponse struct {
	Model     string `json:"model"`
	CreatedAt string `json:"created_at"`
	Response  string `json:"response"`
	Done      bool   `json:"done"`
}

func NewOllamaClient() (*OllamaClient, error) {
	baseURL := os.Getenv("OLLAMA_BASE_URL")
	model := os.Getenv("OLLAMA_MODEL")
	if baseURL == "" {
		return nil, fmt.Errorf("OLLAMA_BASE_URL environment variable not set")
	}
	if model == "" {
		slog.Warn("OLLAMA_MODEL not set, requests must specify model, default gpt-oss")
		model = "gpt-oss"
	}
	baseURL = strings.TrimSuffix(baseURL, "/")
	slog.Info("Initializing Ollama client", "base_url", baseURL, "default_model", model)
	return &OllamaClient{
		httpClient: &http.Client{Timeout: 5 * time.Minute},
		baseURL:    baseURL,
		model:      model,
	}, nil
}

// Generate implements the LLMClient interface
func (o *OllamaClient) Generate(ctx context.Context, prompt string,
	params GenerationParams) (string, error) {

	slog.Debug("Generating text via Ollama", "model", o.model)
	generateURL := o.baseURL + "/api/generate"
	options := make(map[string]interface{})
	if params.Temperature != nil {
		options["temperature"] = *params.Temperature
	} else {
		defaultTemp := float32(0.2)
		options["temperature"] = &defaultTemp
	}
	if params.TopK != nil {
		options["top_k"] = *params.TopK
	} else {
		defaultTopK := 20
		options["top_k"] = defaultTopK
	}
	if params.TopP != nil {
		options["top_p"] = *params.TopP
	} else {
		defaultTopP := float32(0.9)
		options["top_p"] = defaultTopP
	}
	if params.MaxTokens != nil {
		options["num_predict"] = *params.MaxTokens
	} else {
		defaultMaxTokens := 8192
		options["num_predict"] = defaultMaxTokens
	}
	if len(params.Stop) > 0 {
		options["stop"] = params.Stop
	}
	payload := ollamaGenerateRequest{
		Model:   o.model,
		Prompt:  prompt,
		Stream:  false,
		Options: options,
	}

	reqBodyBytes, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request to Ollama: %w", err)
	}
	resp, err := o.httpClient.Post(generateURL, "application/json", bytes.NewBuffer(reqBodyBytes))
	if err != nil {
		slog.Error("Ollama API call failed", "error", err)
		return "", fmt.Errorf("Ollama API call failed: %w", err)
	}
	defer resp.Body.Close()

	respBodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response body from Ollama: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		slog.Error("Ollama returned an error", "status_code", resp.StatusCode, "response", string(respBodyBytes))
		return "", fmt.Errorf("Ollama failed with status %d: %s", resp.StatusCode, string(respBodyBytes))
	}

	var ollamaResp ollamaGenerateResponse
	if err := json.Unmarshal(respBodyBytes, &ollamaResp); err != nil {
		slog.Error("Failed to parse JSON response from Ollama", "error", err, "response", string(respBodyBytes))
		return "", fmt.Errorf("failed to parse Ollama response: %w", err)
	}

	slog.Debug("Received response from Ollama")
	return ollamaResp.Response, nil
}
