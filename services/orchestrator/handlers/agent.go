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
	"github.com/jinterlante1206/AleutianLocal/services/policy_engine"
)

func HandleAgentTrace(pe *policy_engine.PolicyEngine) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req datatypes.AgentTraceRequest
		if err := c.BindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
			return
		}

		slog.Info("Scanning agent query for sensitive data...")
		findings := pe.ScanFileContent(req.Query)
		if len(findings) > 0 {
			slog.Warn("Blocked agent trace due to policy violation", "findings", len(findings))
			c.JSON(http.StatusForbidden, gin.H{
				"error":    "Policy Violation: Query contains sensitive data.",
				"findings": findings,
			})
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
