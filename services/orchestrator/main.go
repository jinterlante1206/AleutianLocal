// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package main

import (
	"context"
	"log"
	"log/slog"
	"net/url"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/jinterlante1206/AleutianLocal/services/llm"
	"github.com/jinterlante1206/AleutianLocal/services/orchestrator/datatypes"
	"github.com/jinterlante1206/AleutianLocal/services/orchestrator/routes"
	"github.com/jinterlante1206/AleutianLocal/services/policy_engine"
	"github.com/weaviate/weaviate-go-client/v4/weaviate"
	"github.com/weaviate/weaviate/entities/models"
)

var policyEngine *policy_engine.PolicyEngine
var globalLLMClient llm.LLMClient

func main() {
	port := os.Getenv("ORCHESTRATOR_PORT")
	if port == "" {
		port = "12210"
	}

	logFile, err := os.OpenFile("/tmp/orchestrator.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		log.Fatalf("Failed to open the log file: %v", err)
	}
	defer logFile.Close()

	logger := slog.New(slog.NewJSONHandler(logFile, nil))
	slog.SetDefault(logger)

	weaviateURL := os.Getenv("WEAVIATE_SERVICE_URL")
	parsedURL, err := url.Parse(weaviateURL)
	if err != nil {
		log.Fatalf("FATAL: Could not find the WEAVIATE SERVICE URL")
	}
	clientConf := weaviate.Config{
		Host:   parsedURL.Host,
		Scheme: parsedURL.Scheme,
	}
	weaviateClient, err := weaviate.NewClient(clientConf)
	if err != nil {
		log.Fatalf("Failed to create a weaviate client, %v", err)
	}

	ensureWeaviateSchema(weaviateClient)

	policyEnginePath := os.Getenv("POLICY_ENGINE_DATA_CLASSIFICATION_PATTERNS_PATH")
	policyEngine, err = policy_engine.NewPolicyEngine(policyEnginePath)
	if err != nil {
		log.Fatalf("FATAL: Could not initialize the Policy Engine %v", err)
	}

	log.Println("Configuring the LLM Client")
	llmBackendType := os.Getenv("LLM_BACKEND_TYPE")

	switch llmBackendType {
	case "local":
		globalLLMClient, err = llm.NewLocalLlamaCppClient()
		slog.Info("Using Local Llama.cpp LLM backend")
	case "openai":
		globalLLMClient, err = llm.NewOpenAIClient()
		slog.Info("Using OpenAI LLM backend")
	// TODO: add cases for "gemini", "ollama", etc.
	default:
		slog.Warn("LLM_BACKEND_TYPE not set or invalid, defaulting to local")
		globalLLMClient, err = llm.NewLocalLlamaCppClient()
	}
	if err != nil {
		log.Fatalf("Failed to initialize LLM client: %v", err)
	}
	router := gin.Default()
	routes.SetupRoutes(router, weaviateClient, globalLLMClient)
	log.Println("started up the container")

	log.Println("Starting the orchestrator server on port ", port)
	if err := router.Run(":" + port); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}

func ensureWeaviateSchema(client *weaviate.Client) {
	// A list of functions that return our schema definitions.
	schemaGetters := []func() *models.Class{
		datatypes.GetDocumentSchema,
		datatypes.GetConversationSchema,
		datatypes.GetSessionSchema,
	}

	for _, getSchema := range schemaGetters {
		class := getSchema()
		slog.Info("Checking schema", "class", class.Class)

		// Check if the class already exists.
		_, err := client.Schema().ClassGetter().WithClassName(class.Class).Do(context.Background())
		if err != nil {
			// If it doesn't exist, the client returns an error. We can now create it.
			slog.Info("Schema not found, creating it...", "class", class.Class)
			err := client.Schema().ClassCreator().WithClass(class).Do(context.Background())
			if err != nil {
				// If we fail to create it, it's a fatal error.
				log.Fatalf("Failed to create schema for class %s: %v", class.Class, err)
			}
			slog.Info("Schema created successfully", "class", class.Class)
		} else {
			// If it exists, no error is returned.
			slog.Info("Schema already exists", "class", class.Class)
		}
	}
}
