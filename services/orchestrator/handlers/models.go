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
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"strings"

	"github.com/AleutianAI/AleutianFOSS/services/orchestrator/datatypes"
	"github.com/gin-gonic/gin"
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
