// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

// Command orchestrator starts the AleutianLocal orchestrator HTTP server.
//
// This is the main entry point for the containerized orchestrator service.
// It reads configuration from environment variables and starts the server.
//
// # Environment Variables
//
//   - ORCHESTRATOR_PORT: HTTP server port (default: 12210)
//   - LLM_BACKEND_TYPE: LLM provider - local, openai, ollama, claude (default: local)
//   - WEAVIATE_SERVICE_URL: Weaviate vector DB URL (optional)
//   - OTEL_EXPORTER_OTLP_ENDPOINT: OpenTelemetry collector (default: aleutian-otel-collector:4317)
//
// # Usage
//
//	# Build
//	go build -o orchestrator ./cmd/orchestrator
//
//	# Run
//	./orchestrator
//
//	# Or via container
//	podman-compose up orchestrator
package main

import (
	"log"
	"log/slog"
	"os"
	"strconv"

	"github.com/jinterlante1206/AleutianLocal/services/orchestrator"
)

func main() {
	// Setup structured logging
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	// Build configuration from environment variables
	cfg := orchestrator.Config{
		Port:         getEnvInt("ORCHESTRATOR_PORT", 12210),
		LLMBackend:   getEnvString("LLM_BACKEND_TYPE", "local"),
		WeaviateURL:  os.Getenv("WEAVIATE_SERVICE_URL"),
		OTelEndpoint: getEnvString("OTEL_EXPORTER_OTLP_ENDPOINT", "aleutian-otel-collector:4317"),
	}

	slog.Info("Starting orchestrator",
		"port", cfg.Port,
		"llm_backend", cfg.LLMBackend,
		"weaviate_url", cfg.WeaviateURL,
	)

	// Create orchestrator with default (no-op) extension options
	// Enterprise builds will pass custom ServiceOptions here
	svc, err := orchestrator.New(cfg, nil)
	if err != nil {
		log.Fatalf("Failed to create orchestrator: %v", err)
	}

	// Run the server (blocks until shutdown)
	if err := svc.Run(); err != nil {
		log.Fatalf("Orchestrator error: %v", err)
	}
}

// getEnvString returns the environment variable value or a default.
func getEnvString(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// getEnvInt returns the environment variable as int or a default.
func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intVal, err := strconv.Atoi(value); err == nil {
			return intVal
		}
	}
	return defaultValue
}
