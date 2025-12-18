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
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jinterlante1206/AleutianLocal/services/llm"
	"github.com/jinterlante1206/AleutianLocal/services/orchestrator/datatypes"
	"github.com/jinterlante1206/AleutianLocal/services/orchestrator/routes"
	"github.com/jinterlante1206/AleutianLocal/services/policy_engine"
	"github.com/weaviate/weaviate-go-client/v5/weaviate"
	"google.golang.org/grpc/credentials/insecure"

	// --- OpenTelemetry imports ---
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.21.0"
	"google.golang.org/grpc"
)

var policyEngine *policy_engine.PolicyEngine
var globalLLMClient llm.LLMClient

func initTracer() (func(context.Context), error) {
	ctx := context.Background()

	// Get the collector URL from the env var we set in podman-compose.yml
	otelEndpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	if otelEndpoint == "" {
		otelEndpoint = "aleutian-otel-collector:4317"
	}
	conn, err := grpc.NewClient(otelEndpoint,
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, err
	}
	traceExporter, err := otlptracegrpc.New(ctx, otlptracegrpc.WithGRPCConn(conn))
	if err != nil {
		return nil, err
	}
	res, err := resource.New(ctx,
		resource.WithAttributes(semconv.ServiceNameKey.String("orchestrator-service")))
	if err != nil {
		return nil, err
	}
	bsp := sdktrace.NewBatchSpanProcessor(traceExporter)
	traceProvider := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithResource(res),
		sdktrace.WithSpanProcessor(bsp))
	otel.SetTracerProvider(traceProvider)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.
		TraceContext{}, propagation.Baggage{}))

	return func(ctx context.Context) {
		ctx, cancel := context.WithTimeout(ctx, time.Second*5)
		defer cancel()
		if err := traceExporter.Shutdown(ctx); err != nil {
			slog.Error("failed to shutdown OTLP exporter", "error", err)
		}
	}, nil
}

func main() {
	port := os.Getenv("ORCHESTRATOR_PORT")
	if port == "" {
		port = "12210"
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	// --- Init the tracer ---
	cleanup, err := initTracer()
	if err != nil {
		log.Fatalf("failed to setup the OTLP tracer: %v", err)
	}
	defer cleanup(context.Background())

	weaviateURL := os.Getenv("WEAVIATE_SERVICE_URL")
	// Sanitize: Trim quotes and whitespace just in case Podman passes them literally
	weaviateURL = strings.Trim(weaviateURL, "\"' ")

	var weaviateClient *weaviate.Client

	// Robust Check: URL must exist AND have a scheme (http/https)
	if weaviateURL != "" && strings.Contains(weaviateURL, "http") {
		parsedURL, err := url.Parse(weaviateURL)

		// Extra validation: Ensure parsing worked
		if err != nil || parsedURL.Scheme == "" || parsedURL.Host == "" {
			slog.Warn("WEAVIATE_SERVICE_URL is invalid. Running in lightweight mode.",
				"url", weaviateURL, "error", err)
		} else {
			clientConf := weaviate.Config{
				Host:   parsedURL.Host,
				Scheme: parsedURL.Scheme,
			}
			weaviateClient, err = weaviate.NewClient(clientConf)
			if err != nil {
				slog.Error("Failed to create Weaviate client", "error", err)
				weaviateClient = nil
			} else {
				// Only attempt schema check if client creation succeeded
				datatypes.EnsureWeaviateSchema(weaviateClient)
			}
		}
	} else {
		slog.Info("WEAVIATE_SERVICE_URL not set or empty. Running in lightweight mode (Time Series / Chat Only).")
	}

	policyEngine, err = policy_engine.NewPolicyEngine()
	if err != nil {
		log.Fatalf("FATAL: Could not initialize the Policy Engine %v", err)
	}
	modelName := os.Getenv("EMBEDDING_MODEL_NAME")
	if modelName == "" {
		slog.Warn("EMBEDDING_MODEL_NAME is not set, defaulting to 'google/embeddinggemma-300m'")
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
	case "ollama":
		globalLLMClient, err = llm.NewOllamaClient()
		slog.Info("Using Ollama LLM backend")
	case "claude", "anthropic":
		globalLLMClient, err = llm.NewAnthropicClient()
		slog.Info("Using Anthropic (Claude) LLM backend")
	// TODO: add cases for "gemini", "huggingface", etc.
	default:
		slog.Warn("LLM_BACKEND_TYPE not set or invalid, defaulting to local")
		globalLLMClient, err = llm.NewLocalLlamaCppClient()
	}
	if err != nil {
		log.Fatalf("Failed to initialize LLM client: %v", err)
	}
	router := gin.Default()
	router.Use(otelgin.Middleware("orchestrator-service"))

	routes.SetupRoutes(router, weaviateClient, globalLLMClient, policyEngine)
	log.Println("started up the container")

	log.Println("Starting the orchestrator server on port ", port)
	if err := router.Run(":" + port); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
