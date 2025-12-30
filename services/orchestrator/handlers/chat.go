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
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/jinterlante1206/AleutianLocal/services/llm"
	"github.com/jinterlante1206/AleutianLocal/services/orchestrator/datatypes"
	"github.com/jinterlante1206/AleutianLocal/services/policy_engine"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
)

var chatTracer = otel.Tracer("aleutian.orchestrator.handlers")

type DirectChatRequest struct {
	Messages       []datatypes.Message `json:"messages"`
	EnableThinking bool                `json:"enable_thinking"` // New
	BudgetTokens   int                 `json:"budget_tokens"`   // New
	Tools          []interface{}       `json:"tools"`           // New
}

func HandleDirectChat(llmClient llm.LLMClient, pe *policy_engine.PolicyEngine) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, span := chatTracer.Start(c.Request.Context(), "HandleDirectChat")
		defer span.End()
		var req DirectChatRequest
		if err := c.BindJSON(&req); err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			slog.Error("Failed to parse the chat request", "error", err)
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
			return
		}

		if len(req.Messages) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "no messages provided"})
			return
		}

		// Scan the last message (the user's new input)
		// Optionally loop through all if you want to be extra safe
		lastMsg := req.Messages[len(req.Messages)-1]
		if lastMsg.Role == "user" {
			findings := pe.ScanFileContent(lastMsg.Content)
			if len(findings) > 0 {
				slog.Warn("Blocked chat request due to policy violation", "findings", len(findings))
				c.JSON(http.StatusForbidden, gin.H{
					"error":    "Policy Violation: Message contains sensitive data.",
					"findings": findings,
				})
				return
			}
		}

		params := llm.GenerationParams{
			EnableThinking:  req.EnableThinking,
			BudgetTokens:    req.BudgetTokens,
			ToolDefinitions: req.Tools,
		}
		answer, err := llmClient.Chat(ctx, req.Messages, params)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			slog.Error("LLMClient.Chat failed", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"answer": answer})
	}
}
