package handlers

import (
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/jinterlante1206/AleutianLocal/services/llm"
	"github.com/jinterlante1206/AleutianLocal/services/orchestrator/datatypes"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
)

var chatTracer = otel.Tracer("aleutian.orchestrator.handlers")

type DirectChatRequest struct {
	Messages []datatypes.Message `json:"messages"`
}

func HandleDirectChat(llmClient llm.LLMClient) gin.HandlerFunc {
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

		// TODO: Pass params from the request if needed
		//params := llm.GenerationParams{
		//	Temperature: nil,
		//	TopK:        nil,
		//	TopP:        nil,
		//	MaxTokens:   nil,
		//	Stop:        nil,
		//}
		params := llm.GenerationParams{}
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
