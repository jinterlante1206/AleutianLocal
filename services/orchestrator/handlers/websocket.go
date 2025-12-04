package handlers

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/jinterlante1206/AleutianLocal/services/llm"
	"github.com/jinterlante1206/AleutianLocal/services/orchestrator/datatypes"
	"github.com/jinterlante1206/AleutianLocal/services/policy_engine"
	"github.com/weaviate/weaviate-go-client/v5/weaviate"
)

// Updated WSRequest to handle actions
type WSRequest struct {
	Mode       string              `json:"mode"`
	Query      string              `json:"query"`
	History    []datatypes.Message `json:"history,omitempty"`
	Action     string              `json:"action,omitempty"`   // e.g., "populate_scan", "populate_confirm"
	Decision   string              `json:"decision,omitempty"` // For "approve" / "deny"
	Base64Data string              `json:"base64data,omitempty"`
	Scope      string              `json:"scope,omitempty"`
	Filename   string              `json:"filename,omitempty"`
}

// WSResponse is used for standard chat replies
type WSResponse struct {
	Answer  string                 `json:"answer"`
	Sources []datatypes.SourceInfo `json:"sources,omitempty"`
	Mode    string                 `json:"mode"`
	Error   string                 `json:"error,omitempty"`
}

// ActionResponse is used for non-chat messages (e.g., session ID, populate commands)
// Using map[string]interface{} for flexibility
// type ActionResponse map[string]interface{}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
	// 10MB Read Buffer
	ReadBufferSize: 10 * 1024 * 1024,
	// 10MB Write Buffer
	WriteBufferSize: 10 * 1024 * 1024,
}

// MockFinding is a placeholder for your real policy_engine.ScanFinding
type MockFinding struct {
	Line  int    `json:"line"`
	Name  string `json:"name"`
	Match string `json:"match"`
}

func sendJSON(ws *websocket.Conn, v interface{}) error {
	err := ws.WriteJSON(v)
	if err != nil {
		slog.Warn("Failed to write WebSocket JSON", "error", err)
	}
	return err
}

func HandleChatWebSocket(client *weaviate.Client, llmClient llm.LLMClient,
	policyEngine *policy_engine.PolicyEngine) gin.HandlerFunc {

	return func(c *gin.Context) {
		ws, err := upgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			slog.Error("failed to upgrade the websocket", "error", err)
			return
		}
		defer ws.Close()
		slog.Info("Websocket client connected")

		// --- WebSocket Connection State ---
		sessionID := uuid.New().String()
		isFirstTurn := true
		slog.Info("New websocket session started", "sessionID", sessionID)

		// --- Send Session ID to client immediately on connect ---
		if err := sendJSON(ws, map[string]interface{}{
			"action":    "session_created",
			"sessionId": sessionID,
		}); err != nil {
			return // Close if we can't even send the first message
		}

		for {
			var req WSRequest
			if err := ws.ReadJSON(&req); err != nil {
				slog.Info("Websocket client disconnected", "error", err.Error())
				break
			}

			ctx := c.Request.Context()

			// --- 1. ROUTE BY ACTION ---
			if req.Action != "" {
				slog.Info("Received action request", "action", req.Action, "path", req.Filename)
				switch req.Action {
				case "file_upload_scan":
					contentBytes, err := base64.StdEncoding.DecodeString(req.Base64Data)
					if err != nil {
						slog.Error("Failed to decode Base64 data for scanning", "filename", req.Filename, "error", err)
						err := sendJSON(ws, map[string]interface{}{"action": "populate_final", "message": "Error: Invalid file data."})
						if err != nil {
							return
						}
						continue
					}

					// --- REAL SCAN LOGIC ---
					slog.Info("Scanning file content", "filename", req.Filename, "size", len(contentBytes))
					// Use the real policy engine
					findings := policyEngine.ScanFileContent(string(contentBytes))

					if len(findings) > 0 {
						// Send back approval card with real findings
						if err = sendJSON(ws, map[string]interface{}{
							"action":   "populate_approval_required",
							"path":     req.Filename,
							"findings": findings, // Pass the real findings array
						}); err != nil {
							return
						}
					} else {
						// No issues found, auto-approve and ingest
						if err = sendJSON(ws, map[string]interface{}{
							"action":  "populate_ingest",
							"message": "✅ Scan complete. No issues found. Ingesting `" + req.Filename + "`...",
						}); err != nil {
							return
						}
						ingestReq := IngestDocumentRequest{
							Content:    string(contentBytes),
							Source:     req.Filename,
							DataSpace:  "default",
							VersionTag: "latest",
						}
						go runWebSocketIngestion(ws, client, ingestReq, sessionID, req.Scope)
					}

				case "file_upload_confirm":
					// --- REAL AUDIT LOGIC ---
					contentBytes, err := base64.StdEncoding.DecodeString(req.Base64Data)
					if err != nil {
						slog.Error("Failed to decode Base64 data for ingestion", "filename", req.Filename, "error", err)
						err := sendJSON(ws, map[string]interface{}{"action": "populate_final", "message": "Error: Invalid file data."})
						if err != nil {
							return
						}
						continue
					}

					// Re-run scan to get findings to log
					findings := policyEngine.ScanFileContent(string(contentBytes))

					// Populate findings with decision metadata
					// TODO: update the .Reviewer with the username
					for i := range findings {
						findings[i].FilePath = req.Filename
						findings[i].UserDecision = req.Decision
						findings[i].Reviewer = "WebAppUser"
						findings[i].ReviewTimestamp = time.Now().UnixMilli()
					}

					go logFindingsToFile(findings)
					if req.Decision == "approve" {
						slog.Info("User approved ingestion", "filename", req.Filename, "scope", req.Scope)
						ingestReq := IngestDocumentRequest{
							Content:    string(contentBytes),
							Source:     req.Filename,
							DataSpace:  "default",
							VersionTag: "latest",
						}
						go runWebSocketIngestion(ws, client, ingestReq, sessionID, req.Scope)
						err := sendJSON(ws, map[string]interface{}{"action": "populate_ingest", "message": "✅ Approved. Ingesting `" + req.Filename + "`..."})
						if err != nil {
							return
						}

					} else {
						slog.Info("User denied ingestion", "filename", req.Filename)
						err := sendJSON(ws, map[string]interface{}{"action": "populate_final", "message": "❌ Cancelled by user. Ingestion of `" + req.Filename + "` aborted."})
						if err != nil {
							return
						}
					}
				}
				continue
			}

			// --- 2. ROUTE BY CHAT MODE ---
			slog.Info("Received chat request", "mode", req.Mode, "query", req.Query)
			var resp WSResponse
			resp.Mode = req.Mode

			switch req.Mode {
			case "chat-rag", "single-rag":
				ragReq := datatypes.RAGRequest{
					Query:     req.Query,
					Pipeline:  "reranking",
					SessionId: sessionID,
				}
				ragResp, ragErr := runRAGLogic(ctx, client, llmClient, ragReq)
				if ragErr != nil {
					resp.Error = ragErr.Error()
				} else {
					resp.Answer = ragResp.Answer
					resp.Sources = ragResp.Sources
				}

			case "chat-no-rag", "single-no-rag":
				messages := req.History
				messages = append(messages, datatypes.Message{Role: "user", Content: req.Query})

				params := llm.GenerationParams{
					Stop: []string{"\nUser:", "\nQuestion:", "\nAleutian AI:", "[USER}"},
				}
				answer, llmErr := llmClient.Chat(ctx, messages, params)
				if llmErr != nil {
					resp.Error = llmErr.Error()
				} else {
					resp.Answer = answer
				}
			}
			if resp.Error == "" {
				// Save conversation turn in background
				go func(isFirstTurn bool) {
					turn := datatypes.Conversation{
						SessionId: sessionID,
						Question:  req.Query,
						Answer:    resp.Answer,
					}
					if err := turn.Save(client); err != nil {
						slog.Warn("Failed to save non-RAG conversation turn", "error", err, "sessionID", sessionID)
					} else {
						go SaveMemoryChunk(client, sessionID, req.Query, resp.Answer)
					}
					if isFirstTurn {
						SummarizeAndSaveSession(llmClient, client, sessionID, req.Query, resp.Answer)
					}
				}(isFirstTurn)
			}
			if resp.Error == "" && strings.TrimSpace(resp.Answer) == "" {
				resp.Answer = "(The model returned an empty response. This may be because 'no RAG' mode is active and the model has no context.)"
			}

			err := sendJSON(ws, resp)
			if err != nil {
				return
			}
			isFirstTurn = false
		}
	}
}

// runWebSocketIngestion is a helper to run the ingestion in a goroutine
// and report success or failure back to the WebSocket client.
func runWebSocketIngestion(ws *websocket.Conn, client *weaviate.Client,
	ingestReq IngestDocumentRequest, sessionID string, scope string) {

	if scope == "session" && sessionID != "" {
		// This is a session-scoped document. Get the Weaviate UUID for the link.
		sessionUUID, err := datatypes.FindOrCreateSessionUUID(context.Background(), client, sessionID)
		if err != nil {
			slog.Error("Failed to find or create parent session for document ingestion", "error", err, "sessionID", sessionID)
		} else {
			ingestReq.SessionUUID = sessionUUID
		}
	}

	chunks, err := RunIngestion(context.Background(), client, ingestReq)
	if err != nil {
		slog.Error("WebSocket ingestion failed", "path", ingestReq.Source, "error", err)
		if err := sendJSON(ws, map[string]interface{}{
			"action":  "populate_final",
			"message": "**Error:** Ingestion failed for `" + ingestReq.Source + "`. " + err.Error(),
		}); err != nil {
			return
		}
		return
	}

	slog.Info("WebSocket ingestion successful", "path", ingestReq.Source, "chunks", chunks)
	if err = sendJSON(ws, map[string]interface{}{
		"action": "populate_final",
		"message": fmt.Sprintf("🎉 **Success!** Ingested `%s` (%d chunks).",
			ingestReq.Source, chunks),
	}); err != nil {
		return
	}
}

// logFindingsToFile appends scan findings to a JSON Lines log file.
// It runs in a goroutine so it doesn't block the API response.
func logFindingsToFile(findings []policy_engine.ScanFinding) {
	if len(findings) == 0 {
		return // Nothing to log
	}

	// Log to /tmp/ inside the container. This can be mounted out with a volume if needed.
	logFilePath := "/tmp/scan_audit_log.jsonl"

	// Open the file in append mode, creating it if it doesn't exist.
	file, err := os.OpenFile(logFilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		slog.Error("Failed to open audit log file", "path", logFilePath, "error", err)
		return
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	for _, finding := range findings {
		// Marshal each finding as a new line
		if err := encoder.Encode(finding); err != nil {
			slog.Warn("Failed to write finding to audit log", "error", err)
		}
	}
	slog.Info("Successfully wrote findings to audit log", "count", len(findings), "path",
		logFilePath)
}
