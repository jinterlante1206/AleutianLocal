package handlers

import (
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"net/http"
	"strings" // Import strings

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/jinterlante1206/AleutianLocal/services/llm"
	"github.com/jinterlante1206/AleutianLocal/services/orchestrator/datatypes"
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

func HandleChatWebSocket(client *weaviate.Client, llmClient llm.LLMClient) gin.HandlerFunc {
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
					// Here you would plug in your *real* policy engine scanner
					// We decode the content to scan it
					contentBytes, err := base64.StdEncoding.DecodeString(req.Base64Data)
					if err != nil {
						slog.Error("Failed to decode Base64 data for scanning", "filename", req.Filename, "error", err)
						sendJSON(ws, map[string]interface{}{"action": "populate_final", "message": "Error: Invalid file data."})
						continue
					}

					// Mock Scan Logic (using file content length and name)
					slog.Info("Scanning file content", "filename", req.Filename, "size", len(contentBytes))
					if strings.Contains(req.Filename, "secret") || strings.Contains(req.Filename, "key") || strings.Contains(string(contentBytes), "pass:") {
						findings := []MockFinding{
							{Line: 1, Name: "Mock Secret", Match: "File contains sensitive keywords..."},
						}
						// Send back approval card
						if err = sendJSON(ws, map[string]interface{}{
							"action":   "populate_approval_required",
							"path":     req.Filename, // "path" is what the JS approval card expects
							"findings": findings,
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
						// Call ingestion goroutine
						ingestReq := IngestDocumentRequest{
							Content:    string(contentBytes),
							Source:     req.Filename,
							DataSpace:  "default",
							VersionTag: "latest",
							// SessionID: "", // This is a global upload
						}
						go runWebSocketIngestion(ws, client, ingestReq)
					}

				case "file_upload_confirm":
					if req.Decision == "approve" {
						slog.Info("User approved ingestion", "filename", req.Filename, "scope", req.Scope)

						// Decode the data *again* (stateless design)
						contentBytes, err := base64.StdEncoding.DecodeString(req.Base64Data)
						if err != nil {
							slog.Error("Failed to decode Base64 data for ingestion", "filename", req.Filename, "error", err)
							sendJSON(ws, map[string]interface{}{"action": "populate_final", "message": "Error: Invalid file data."})
							continue
						}

						ingestReq := IngestDocumentRequest{
							Content:    string(contentBytes),
							Source:     req.Filename,
							DataSpace:  "default",
							VersionTag: "latest",
						}
						// This is where you would add session scoping if the RAG engine supported it
						// if req.Scope == "session" {
						// 	ingestReq.SessionID = sessionID
						// }

						go runWebSocketIngestion(ws, client, ingestReq)
						sendJSON(ws, map[string]interface{}{"action": "populate_ingest", "message": "✅ Approved. Ingesting `" + req.Filename + "`..."})

					} else {
						slog.Info("User denied ingestion", "filename", req.Filename)
						sendJSON(ws, map[string]interface{}{"action": "populate_final", "message": "❌ Cancelled by user. Ingestion of `" + req.Filename + "` aborted."})
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

				params := llm.GenerationParams{}
				answer, llmErr := llmClient.Chat(ctx, messages, params)
				if llmErr != nil {
					resp.Error = llmErr.Error()
				} else {
					resp.Answer = answer
					// Save conversation turn in background
					go func() {
						turn := datatypes.Conversation{
							SessionId: sessionID,
							Question:  req.Query,
							Answer:    resp.Answer,
						}
						if err := turn.Save(client); err != nil {
							slog.Warn("Failed to save non-RAG conversation turn", "error", err, "sessionID", sessionID)
						}
						if isFirstTurn {
							SummarizeAndSaveSession(llmClient, client, sessionID, req.Query, resp.Answer)
						}
					}()
				}
			}

			err := sendJSON(ws, resp)
			if err != nil {
				return
			}
			isFirstTurn = false // Mark first turn as complete
		}
	}
}

// runWebSocketIngestion is a helper to run the ingestion in a goroutine
// and report success or failure back to the WebSocket client.
func runWebSocketIngestion(ws *websocket.Conn, client *weaviate.Client, ingestReq IngestDocumentRequest) {

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
		"action":  "populate_final",
		"message": fmt.Sprintf("🎉 **Success!** Ingested `%s` (%d chunks).", ingestReq.Source, chunks),
	}); err != nil {
		return
	}
}
