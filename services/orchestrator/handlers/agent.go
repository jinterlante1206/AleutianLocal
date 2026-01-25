// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"

	"github.com/AleutianAI/AleutianFOSS/services/orchestrator/datatypes"
	"github.com/AleutianAI/AleutianFOSS/services/policy_engine"
	"github.com/gin-gonic/gin"
)

func HandleAgentStep(pe *policy_engine.PolicyEngine) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req datatypes.AgentStepRequest
		if err := c.BindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
			return
		}

		// 1. Security: Policy Check
		// We scan the 'Query' (User Input)
		findings := pe.ScanFileContent(req.Query)

		// CRITICAL SECURITY FIX: Scan the latest History entry (Tool Output)
		// If the CLI just read a file, the content is here. We must block it if it's sensitive.
		if len(req.History) > 0 {
			lastMsg := req.History[len(req.History)-1]
			// Only scan "tool" outputs or "user" follow-ups
			if lastMsg.Role == "tool" || lastMsg.Role == "user" {
				newFindings := pe.ScanFileContent(lastMsg.Content)
				findings = append(findings, newFindings...)
			}
		}

		if len(findings) > 0 {
			slog.Warn("Blocked agent step due to policy violation", "findings", len(findings))
			c.JSON(http.StatusForbidden, gin.H{
				"error":    "Policy Violation: Input contains sensitive data.",
				"findings": findings,
			})
			return
		}

		// 2. Proxy to Python RAG Engine
		ragURL := os.Getenv("RAG_ENGINE_URL")
		if ragURL == "" {
			ragURL = "http://aleutian-rag-engine:8000"
		}
		pythonEndpoint := fmt.Sprintf("%s/agent/step", ragURL)

		// Marshal the request to send to Python
		jsonBody, err := json.Marshal(req)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to marshal request"})
			return
		}

		// Call Python
		resp, err := http.Post(pythonEndpoint, "application/json", bytes.NewBuffer(jsonBody))
		if err != nil {
			slog.Error("Failed to call Python Agent Brain", "error", err)
			c.JSON(http.StatusBadGateway, gin.H{"error": "Agent Brain unavailable"})
			return
		}
		defer resp.Body.Close()

		// Read Python response
		bodyBytes, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			slog.Error("Python Agent returned error", "status", resp.StatusCode, "body", string(bodyBytes))
			c.JSON(resp.StatusCode, gin.H{"error": "Agent Brain error", "details": string(bodyBytes)})
			return
		}

		// Return decision to CLI
		c.Data(http.StatusOK, "application/json", bodyBytes)
	}
}
