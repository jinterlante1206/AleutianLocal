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
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

var tracer = otel.Tracer("aleutian.llm.ollama") // Specific tracer name

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

type ollamaChatRequest struct {
	Model    string                 `json:"model"`
	Messages []datatypes.Message    `json:"messages"`
	Stream   bool                   `json:"stream"`
	Options  map[string]interface{} `json:"options,omitempty"`
}

type ollamaChatResponse struct {
	Message   datatypes.Message `json:"message"`
	CreatedAt string            `json:"created_at"`
	Done      bool              `json:"done"`
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

	ctx, span := tracer.Start(ctx, "OllamaClient.Generate")
	defer span.End()
	span.SetAttributes(attribute.String("llm.model", o.model))
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
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return "", fmt.Errorf("failed to marshal request to Ollama: %w", err)
	}

	// Use NewRequestWithContext to respect context cancellation/timeout
	req, err := http.NewRequestWithContext(ctx, "POST", generateURL, bytes.NewBuffer(reqBodyBytes))
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return "", fmt.Errorf("failed to create request to Ollama: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := o.httpClient.Do(req)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		slog.Error("Ollama API call failed", "error", err)
		return "", fmt.Errorf("Ollama API call failed: %w", err)
	}
	defer resp.Body.Close()

	respBodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return "", fmt.Errorf("failed to read response body from Ollama: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusNotFound {
			var errResp struct {
				Error string `json:"error"`
			}
			if err := json.Unmarshal(respBodyBytes, &errResp); err == nil && strings.Contains(errResp.Error, "model") && strings.Contains(errResp.Error, "not found") {
				slog.Warn("Ollama model not found", "model", o.model)
				// Return a specific, user-friendly error
				return "", fmt.Errorf("model '%s' not found. Please run: 'ollama pull %s'", o.model, o.model)
			}
		}
		slog.Error("Ollama returned an error", "status_code", resp.StatusCode, "response", string(respBodyBytes))
		return "", fmt.Errorf("Ollama failed with status %d: %s", resp.StatusCode, string(respBodyBytes))
	}

	var ollamaResp ollamaGenerateResponse
	if err := json.Unmarshal(respBodyBytes, &ollamaResp); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		slog.Error("Failed to parse JSON response from Ollama", "error", err, "response", string(respBodyBytes))
		return "", fmt.Errorf("failed to parse Ollama response: %w", err)
	}

	slog.Debug("Received response from Ollama")
	return ollamaResp.Response, nil
}

func (o *OllamaClient) Chat(ctx context.Context, messages []datatypes.Message,
	params GenerationParams) (string, error) {

	ctx, span := tracer.Start(ctx, "OllamaClient.Chat")
	defer span.End()
	span.SetAttributes(attribute.String("llm.model", o.model))
	span.SetAttributes(attribute.Int("llm.num_messages", len(messages)))

	slog.Debug("Generating text via Ollama", "model", o.model)
	chatURL := o.baseURL + "/api/chat"
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
	payload := ollamaChatRequest{
		Model:    o.model,
		Messages: messages,
		Stream:   false,
		Options:  options,
	}
	reqBody, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("failed to marshal chat request to Ollama: %w", err)
	}

	// Use NewRequestWithContext to respect context cancellation/timeout
	req, err := http.NewRequestWithContext(ctx, "POST", chatURL, bytes.NewBuffer(reqBody))
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return "", fmt.Errorf("failed to create chat request to Ollama: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := o.httpClient.Do(req)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return "", fmt.Errorf("failed to send the request to %s: %v", chatURL, err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		slog.Error("Ollama chat returned an error", "status_code", resp.StatusCode,
			"response", string(respBody))
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return "", fmt.Errorf("ollama chat failed with status %d: %s", resp.StatusCode,
			string(respBody))
	}
	var ollamaResp ollamaChatResponse
	if err = json.Unmarshal(respBody, &ollamaResp); err != nil {
		slog.Error("Failed to parse JSON chat response from Ollama", "error", err,
			"response", string(respBody))
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return "", fmt.Errorf("failed to marshal the response from the ollama Chat")
	}
	if ollamaResp.Message.Role != "assistant" {
		slog.Warn("Ollama chat response message role was not 'assistant'", "role", ollamaResp.Message.Role)
	}
	return ollamaResp.Message.Content, nil
}
