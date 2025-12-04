package handlers

import (
	"bytes"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/jinterlante1206/AleutianLocal/services/orchestrator/datatypes"
)

// HandleModelPull triggers the huggingface-cli to download the model to the shared cache
func HandleModelPull() gin.HandlerFunc {
	return func(c *gin.Context) {
		var req datatypes.ModelPullRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
			return
		}
		if req.Revision == "" {
			req.Revision = "main"
		}

		slog.Info("Received model pull request", "model", req.ModelID, "revision", req.Revision)

		// We assume the standard cache location inside the container
		// This must match the volume mount in podman-compose.yml
		cacheDir := "/root/.cache/huggingface/"

		// Check for HF Token secret (mounted by Podman)
		tokenPath := "/run/secrets/aleutian_hf_token"
		token := ""
		if content, err := os.ReadFile(tokenPath); err == nil {
			token = strings.TrimSpace(string(content))
		}

		// Prepare the command
		args := []string{
			"download",
			req.ModelID,
			"--revision", req.Revision,
			"--cache-dir", cacheDir,
		}
		if token != "" {
			args = append(args, "--token", token)
		}

		cmd := exec.Command("hf", args...)

		// Capture output for debugging
		var out bytes.Buffer
		var stderr bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &stderr

		slog.Info("Executing download...", "command", "hf "+strings.Join(args, " "))
		err := cmd.Run()

		if err != nil {
			slog.Error("Model download failed", "error", err, "stderr", stderr.String())
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Failed to download model",
				"details": stderr.String(),
			})
			return
		}

		// Determine the final snapshot path (heuristic)
		// Note: Realistically, we just confirm it's in the cache.
		slog.Info("Model download complete", "model", req.ModelID)

		c.JSON(http.StatusOK, datatypes.ModelPullResponse{
			Status:    "success",
			ModelID:   req.ModelID,
			LocalPath: cacheDir,
			Message:   fmt.Sprintf("Model %s cached successfully", req.ModelID),
		})
	}
}
