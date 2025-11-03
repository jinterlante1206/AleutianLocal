package datatypes

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"
)

type embeddingServiceRequest struct {
	Texts []string `json:"texts"`
}

type embeddingServiceResponse struct {
	Vectors   [][]float32 `json:"vectors"`
	Model     string      `json:"model"`
	Dim       int         `json:"dim"`
	Timestamp int64       `json:"timestamp"`
	Id        string      `json:"id"`
}

type EmbeddingRequest struct {
	Text string `json:"text"`
}

type EmbeddingResponse struct {
	Id        string    `json:"id"`
	Timestamp int       `json:"timestamp"`
	Text      string    `json:"text"`
	Vector    []float32 `json:"vector"`
	Dim       int       `json:"dim"`
}

type CodeSnippetProperties struct {
	Content  string `json:"content"`
	Filename string `json:"filename"`
	Language string `json:"language"`
}

type WeaviateObject struct {
	Class      string                `json:"class"`
	Properties CodeSnippetProperties `json:"properties"`
	Vector     []float32             `json:"vector"`
}

type WeaviateConversationMemoryObject struct {
	Class      string                 `json:"class"`
	Properties ConversationProperties `json:"properties"`
	Vector     []float32              `json:"vector"`
}

type ConversationProperties struct {
	SessionId string `json:"session_id"`
	Question  string `json:"question"`
	Answer    string `json:"answer"`
	Timestamp int64  `json:"timestamp"`
}

type WeaviateSessionObject struct {
	Class      string            `json:"class"`
	Properties SessionProperties `json:"properties"`
}

type SessionProperties struct {
	SessionId string `json:"session_id"`
	Summary   string `json:"summary"`
	Timestamp int64  `json:"timestamp"`
}

type RAGRequest struct {
	Query     string `json:"query"`
	SessionId string `json:"session_id"`
	Pipeline  string `json:"pipeline"`
	NoRag     bool   `json:"no_rag"`
}

type SourceInfo struct {
	Source   string  `json:"source"`
	Distance float64 `json:"distance,omitempty"`
	Score    float64 `json:"score,omitempty"`
}

type RAGResponse struct {
	Answer    string       `json:"answer"`
	SessionId string       `json:"session_id"`
	Sources   []SourceInfo `json:"sources,omitempty"`
}

type RagEngineResponse struct {
	Answer  string       `json:"answer"`
	Sources []SourceInfo `json:"sources,omitempty"`
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

var httpClient = &http.Client{
	Timeout: 30 * time.Second,
}

func (e *EmbeddingResponse) Get(text string) error {
	embeddingServiceURL := os.Getenv("EMBEDDING_SERVICE_URL")

	// Use the correct request struct: {"texts": ["..."]}
	embReq := embeddingServiceRequest{Texts: []string{text}}
	reqBody, err := json.Marshal(embReq)
	if err != nil {
		return fmt.Errorf("failed to marshal embedding request: %w", err)
	}

	// This part is unchanged
	req, err := http.NewRequest(http.MethodPost, embeddingServiceURL, bytes.NewBuffer(reqBody))
	if err != nil {
		return fmt.Errorf("failed to setup a new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cache-Control", "no-cache, no-store, must-revalidate")

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to make the request to the embedding service: %w", err)
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			log.Println("Failed to close out the body on func close")
		}
	}(resp.Body)

	bodyBytes, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("embedding service returned non-200 status: %s, %d", string(bodyBytes), resp.StatusCode)
	}

	// Use the correct response struct to parse: {"vectors": [[...]]}
	var serviceResp embeddingServiceResponse
	if err := json.Unmarshal(bodyBytes, &serviceResp); err != nil {
		slog.Warn("Failed to parse embedding service response as batch, trying single", "error", err)
		if err := json.Unmarshal(bodyBytes, &e); err != nil {
			return fmt.Errorf("failed to parse response from embedding service in any format: %w", err)
		}
		return nil
	}

	// Check that we got at least one vector back
	if len(serviceResp.Vectors) == 0 || len(serviceResp.Vectors[0]) == 0 {
		return fmt.Errorf("embedding service returned no vectors")
	}

	e.Vector = serviceResp.Vectors[0]
	e.Dim = len(e.Vector)
	e.Text = text
	e.Timestamp = int(time.Now().Unix()) // Use current time
	e.Id = serviceResp.Id

	return nil
}

type WeaviateSchemas struct {
	Schemas []struct {
		Class       string `json:"class"`
		Description string `json:"description"`
		Vectorizer  string `json:"vectorizer"`
		Properties  []struct {
			Name        string   `json:"name"`
			DataType    []string `json:"dataType"`
			Description string   `json:"description"`
		} `json:"properties"`
	} `json:"schemas"`
}

func (w *WeaviateSchemas) InitializeSchemas() {
	for _, schema := range w.Schemas {
		schemaToString, err := json.Marshal(schema)
		if err != nil {
			slog.Error("failed to convert the schema back to a string", "error", err)
		}
		resp, err := http.Post(fmt.Sprintf("%s/schema", os.Getenv("WEAVIATE_SERVICE_URL")),
			"application/json", strings.NewReader(string(schemaToString)))
		if err != nil {
			log.Fatalf("FATAL: Could not send a schema to Weaviate: %v", err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 {
			slog.Warn(
				"Weaviate returned a non-200 status while creating a schema", "class", schema.Class, "status_code", resp.StatusCode, "response", string(body))
		} else {
			slog.Info("Successfully created or verified schema", "class", schema.Class)
		}
	}
}
