## Template 2: Integrating an External LLM API (e.g., Claude)

This guide explains how to configure Aleutian to use a cloud-based LLM API (like Anthropic's Claude, OpenAI, or Google Gemini) instead of a locally hosted model.

**Goal:** Use the Aleutian platform (orchestrator, RAG engine, UI) but have the final text generation performed by an external API.

**Prerequisites:**

* An API key for the desired service (e.g., Anthropic).
* Familiarity with Go programming (to implement the client interface).

**Steps:**

1.  **Implement the `LLMClient` Interface (Go):**
    * Create a new Go file in the orchestrator's `services/llm/` directory (e.g., `claude_llm.go`).
    * Define a struct (e.g., `ClaudeClient`) that holds necessary information (like API key, model name, HTTP client).
    * Implement the `Generate` method required by the `llm.LLMClient` interface. This method will:
        * Read necessary configuration (API key from secret, model name from env var).
        * Construct the request payload according to the external API's specification.
        * Make the HTTP request to the external API endpoint.
        * Parse the response and return the generated text.
    * Create a constructor function (e.g., `NewClaudeClient`) that reads configuration and returns an instance of your client struct.

    **`llm/claude_llm.go.template` (Skeleton):**
    ```go
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
    
    // --- Define Structs for Claude API ---
    // (Refer to Anthropic API documentation for exact request/response formats)
    type ClaudeMessage struct {
    	Role    string `json:"role"` // "user" or "assistant"
    	Content string `json:"content"`
    }
    
    type ClaudeRequest struct {
    	Model     string          `json:"model"`
    	Messages  []ClaudeMessage `json:"messages"`
    	MaxTokens int             `json:"max_tokens"`
    	// Add other params like temperature, top_p, stop_sequences
    	Temperature *float32 `json:"temperature,omitempty"`
    	Stop        []string `json:"stop_sequences,omitempty"`
    }
    
    type ClaudeResponseContent struct {
    	Type string `json:"type"` // e.g., "text"
    	Text string `json:"text"`
    }
    
    type ClaudeResponse struct {
    	Content      []ClaudeResponseContent `json:"content"`
    	StopReason   string                  `json:"stop_reason"`
    	// Add other fields as needed (usage, id, etc.)
    }
    // --- End Claude API Structs ---
    
    
    type ClaudeClient struct {
    	httpClient *http.Client
    	apiKey     string
    	model      string
    	apiURL     string // e.g., "[https://api.anthropic.com/v1/messages](https://api.anthropic.com/v1/messages)"
    }
    
    func NewClaudeClient() (*ClaudeClient, error) {
    	apiKey := ""
    	secretPath := "/run/secrets/anthropic_api_key" // Standard secret path
    	apiKeyBytes, err := os.ReadFile(secretPath)
    	if err == nil {
    		apiKey = strings.TrimSpace(string(apiKeyBytes))
    		slog.Info("Read Anthropic API Key from Podman secret")
    	} else {
    		// Fallback to environment variable (less secure)
    		apiKey = os.Getenv("ANTHROPIC_API_KEY")
    		if apiKey == "" {
    			slog.Error("Anthropic API Key not found in secret or ANTHROPIC_API_KEY env var", "path", secretPath)
    			return nil, fmt.Errorf("Anthropic API key not configured")
    		}
    		slog.Warn("Reading Anthropic API Key from environment variable (recommend using secrets)")
    	}
    
    	model := os.Getenv("CLAUDE_MODEL")
    	if model == "" {
    		model = "claude-3-haiku-20240307" // Sensible default
    		slog.Warn("CLAUDE_MODEL not set, defaulting to claude-3-haiku")
    	}
    
    	apiURL := "[https://api.anthropic.com/v1/messages](https://api.anthropic.com/v1/messages)" // Standard Claude API endpoint
    
    	slog.Info("Initializing Claude client", "model", model)
    	return &ClaudeClient{
    		httpClient: &http.Client{Timeout: 2 * time.Minute},
    		apiKey:     apiKey,
    		model:      model,
    		apiURL:     apiURL,
    	}, nil
    }
    
    // Generate implements the LLMClient interface
    func (c *ClaudeClient) Generate(ctx context.Context, prompt string, params GenerationParams) (string, error) {
    	slog.Debug("Generating text via Claude", "model", c.model)
    
    	// Construct Claude API Request
    	claudeReq := ClaudeRequest{
    		Model: c.model,
    		Messages: []ClaudeMessage{
    			{Role: "user", Content: prompt}, // Simple user prompt for Generate
    		},
    		// Set default and map params
    		MaxTokens: 1024, // Default MaxTokens for Claude
    	}
    	if params.MaxTokens != nil {
    		claudeReq.MaxTokens = *params.MaxTokens
    	}
    	if params.Temperature != nil {
    		claudeReq.Temperature = params.Temperature
    	}
    	if len(params.Stop) > 0 {
    		claudeReq.Stop = params.Stop
    	}
    	// Add mappings for TopP, TopK if Claude supports them in this API version
    
    	reqBodyBytes, err := json.Marshal(claudeReq)
    	if err != nil {
    		return "", fmt.Errorf("failed to marshal request body for Claude: %w", err)
    	}
    
    	// Create HTTP Request
    	req, err := http.NewRequestWithContext(ctx, "POST", c.apiURL, bytes.NewBuffer(reqBodyBytes))
    	if err != nil {
    		return "", fmt.Errorf("failed to create Claude request: %w", err)
    	}
    
    	// Set Headers (Check Claude API Docs for current requirements)
    	req.Header.Set("Content-Type", "application/json")
    	req.Header.Set("x-api-key", c.apiKey)
    	req.Header.Set("anthropic-version", "2023-06-01") // Example version header
    
    	// Send Request
    	resp, err := c.httpClient.Do(req)
    	if err != nil {
    		slog.Error("Claude API call failed", "error", err)
    		return "", fmt.Errorf("Claude API call failed: %w", err)
    	}
    	defer resp.Body.Close()
    
    	respBodyBytes, err := io.ReadAll(resp.Body)
    	if err != nil {
    		return "", fmt.Errorf("failed to read response body from Claude: %w", err)
    	}
    
    	// Handle Errors
    	if resp.StatusCode != http.StatusOK {
    		slog.Error("Claude API returned an error", "status_code", resp.StatusCode, "response", string(respBodyBytes))
    		return "", fmt.Errorf("Claude API failed with status %d: %s", resp.StatusCode, string(respBodyBytes))
    	}
    
    	// Parse Response
    	var claudeResp ClaudeResponse
    	if err := json.Unmarshal(respBodyBytes, &claudeResp); err != nil {
    		slog.Error("Failed to parse JSON response from Claude", "error", err, "response", string(respBodyBytes))
    		return "", fmt.Errorf("failed to parse Claude response: %w", err)
    	}
    
    	// Extract Text Content
    	if len(claudeResp.Content) > 0 && claudeResp.Content[0].Type == "text" {
    		slog.Debug("Received response from Claude", "stop_reason", claudeResp.StopReason)
    		return claudeResp.Content[0].Text, nil
    	}
    
    	slog.Warn("Claude response did not contain expected text content")
    	return "", fmt.Errorf("Claude response did not contain text content")
    }

    ```

2.  **Update Orchestrator `main.go`:**
    * Add a new `case` to the `switch llmBackendType` block:
        ```go
        // In services/orchestrator/main.go
        // ... inside main() ...
        switch llmBackendType {
        // ... other cases (local, openai, ollama) ...
        case "claude": // <-- Add this case
        	globalLLMClient, err = llm.NewClaudeClient()
        	slog.Info("Using Claude LLM backend")
        default:
        // ...
        }
        // ...
        ```

3.  **Configure Podman Secret:**
    * Create the secret using the Podman CLI. Replace `sk-ant-xxx...` with your actual key.
        ```bash
        echo "sk-ant-xxx..." | podman secret create anthropic_api_key -
        ```

4.  **Update `podman-compose.yml` (or Override):**
    * Add the secret definition to the top-level `secrets:` block.
    * Mount the secret into the `orchestrator` service.

    ```yaml
    # --- podman-compose.yml --- 

    secrets:
      aleutian_hf_token:
        external: true
      anthropic_api_key: # <-- Add this
        external: true 
      # Add other secrets like openai_api_key if needed

    services:
      orchestrator:
        # ... build, ports ...
        environment:
          # --- LLM Backend Configuration ---
          LLM_BACKEND_TYPE: ${LLM_BACKEND_TYPE:-claude} # <-- Set to 'claude' via .env or override
          CLAUDE_MODEL: ${CLAUDE_MODEL:-claude-3-haiku-20240307} # Set desired model
          # Make sure other backend URLs/models are commented out or ignored
          # ... other env vars ...
        secrets:
          - source: aleutian_hf_token
            target: aleutian_hf_token
          - source: anthropic_api_key # <-- Add this mount
            target: anthropic_api_key 
        # ... volumes, networks, depends_on ...

      # --- IMPORTANT ---
      # Comment out or remove unused local LLM server services 
      # (e.g., llm-server, ollama, hf-server) 
      # when using an external API like Claude.
      # llm-server:
      #   ...
      # ollama:
      #   ...

    # ... rest of file ... 
    ```

5.  **Configure via `.env` (Example):**
    * Create or edit your `.env` file in the project root:
        ```.env
        # .env
        LLM_BACKEND_TYPE=claude
        CLAUDE_MODEL=claude-3-sonnet-20240229 
        # ANTHROPIC_API_KEY=... # Don't put the key here, use the Podman secret!
        ```

6.  **Deploy/Restart:**
    * Run `./aleutian stack stop` if running.
    * Run `./aleutian stack start --build` to rebuild the orchestrator with the new client code and apply environment variables.

Now, your orchestrator will use the `ClaudeClient` to make calls to the Anthropic API whenever `llmClient.Generate` is invoked (e.g., during session summarization).