package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/jinterlante1206/AleutianLocal/services/orchestrator/datatypes"
)

func HandleAgentTrace() gin.HandlerFunc {
	return func(c *gin.Context) {
		var req datatypes.AgentTraceRequest
		if err := c.BindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
			return
		}

		// 1. Find Python Service
		ragUrl := os.Getenv("RAG_ENGINE_URL")
		if ragUrl == "" {
			ragUrl = "http://aleutian-rag-engine:8000"
		}
		targetUrl := fmt.Sprintf("%s/agent/trace", ragUrl)

		slog.Info("Starting Agent Trace", "query", req.Query)

		// 2. Call Python
		reqBody, _ := json.Marshal(req)
		resp, err := http.Post(targetUrl, "application/json", bytes.NewBuffer(reqBody))
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Failed to contact Agent service"})
			return
		}
		defer resp.Body.Close()

		// 3. Stream response back to CLI
		c.Status(resp.StatusCode)
		io.Copy(c.Writer, resp.Body)
	}
}
