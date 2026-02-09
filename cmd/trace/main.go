// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

// Command trace starts the Aleutian Trace API server.
//
// Aleutian Trace provides AST-powered code intelligence with:
//   - Ephemeral code graphs (in-memory, rebuilt from source)
//   - Multi-language support (Go, Python, TypeScript, JavaScript, HTML, CSS)
//   - 30+ agentic tools for exploration, reasoning, and safety
//   - LLM-powered agent loop with tool calling
//
// Usage:
//
//	go run ./cmd/trace
//	go run ./cmd/trace -port 9090
//
// With Ollama (for agent loop):
//
//	OLLAMA_BASE_URL=http://localhost:11434 OLLAMA_MODEL=glm-4.7-flash go run ./cmd/trace
//
// With context assembly (sends code to LLM):
//
//	OLLAMA_BASE_URL=http://localhost:11434 OLLAMA_MODEL=glm-4.7-flash go run ./cmd/trace -with-context
//
// With tools enabled (LLM can use exploration tools):
//
//	OLLAMA_BASE_URL=http://localhost:11434 OLLAMA_MODEL=glm-4.7-flash go run ./cmd/trace -with-tools
//
// Full features:
//
//	OLLAMA_BASE_URL=http://localhost:11434 OLLAMA_MODEL=glm-4.7-flash go run ./cmd/trace -with-context -with-tools
//
// Example requests:
//
//	# Health check
//	curl http://localhost:8080/v1/trace/health
//
//	# Get all available tools
//	curl http://localhost:8080/v1/trace/tools | jq
//
//	# Initialize a code graph
//	curl -X POST http://localhost:8080/v1/trace/init \
//	  -H "Content-Type: application/json" \
//	  -d '{"project_root": "/path/to/project"}'
//
//	# Run agent query (requires Ollama)
//	curl -X POST http://localhost:8080/v1/trace/agent/run \
//	  -H "Content-Type: application/json" \
//	  -d '{"project_root": "/path/to/project", "query": "What are the main entry points?"}'
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/llm"
	"github.com/AleutianAI/AleutianFOSS/services/orchestrator/datatypes"
	"github.com/AleutianAI/AleutianFOSS/services/trace"
	"github.com/AleutianAI/AleutianFOSS/services/trace/agent"
	"github.com/AleutianAI/AleutianFOSS/services/trace/agent/classifier"
	"github.com/AleutianAI/AleutianFOSS/services/trace/agent/events"
	agentllm "github.com/AleutianAI/AleutianFOSS/services/trace/agent/llm"
	"github.com/AleutianAI/AleutianFOSS/services/trace/agent/phases"
	"github.com/AleutianAI/AleutianFOSS/services/trace/agent/safety"
	"github.com/AleutianAI/AleutianFOSS/services/trace/cli/tools"
	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	oteltrace "go.opentelemetry.io/otel/trace"
)

// warmupStatus tracks whether the main LLM model has completed warming up.
// 0 = not complete, 1 = complete.
// Used to return 503 Service Unavailable for agent endpoints during warmup.
var warmupStatus atomic.Int32

// IsWarmupComplete returns true if the main model warmup has finished.
//
// Thread Safety: This function is safe for concurrent use.
func IsWarmupComplete() bool {
	return warmupStatus.Load() == 1
}

// markWarmupComplete marks the warmup as complete.
//
// Thread Safety: This function is safe for concurrent use.
func markWarmupComplete() {
	warmupStatus.Store(1)
}

// WarmupGuardMiddleware returns 503 Service Unavailable for agent endpoints
// if the model warmup has not yet completed.
//
// Description:
//
//	This middleware protects agent endpoints from receiving requests before
//	the LLM model is fully loaded into VRAM. Without this guard, early requests
//	would receive empty responses or errors due to model cold-start issues.
//
// Behavior:
//
//   - Returns 503 with Retry-After header if warmup not complete
//   - Creates an OTel span for rejected requests with trace context from headers
//   - Passes through to handler if warmup is complete
//   - Health check and non-agent endpoints are not affected (use different routes)
//
// Tracing:
//
//	I-3: Inherits trace context from W3C TraceContext headers (traceparent).
//	When rejecting requests, creates a span with the inherited trace ID so
//	clients can correlate 503 responses with their distributed traces.
//
// Thread Safety: This middleware is safe for concurrent use.
func WarmupGuardMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !IsWarmupComplete() {
			// I-3: Create span with inherited trace context for observability.
			// The otelgin middleware has already extracted trace context from headers.
			ctx := c.Request.Context()
			_, span := otel.Tracer("aleutian.trace").Start(ctx, "warmup_guard.reject",
				oteltrace.WithAttributes(
					attribute.String("path", c.Request.URL.Path),
					attribute.String("method", c.Request.Method),
					attribute.Int("http.status_code", http.StatusServiceUnavailable),
				),
			)
			defer span.End()

			// Extract trace_id for structured logging
			spanCtx := span.SpanContext()
			traceID := ""
			if spanCtx.HasTraceID() {
				traceID = spanCtx.TraceID().String()
			}

			slog.Warn("Agent request rejected: model warmup in progress",
				slog.String("path", c.Request.URL.Path),
				slog.String("method", c.Request.Method),
				slog.String("trace_id", traceID))

			span.SetStatus(codes.Error, "service unavailable during warmup")

			c.Header("Retry-After", "30")
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"error":    "Model warmup in progress",
				"code":     "SERVICE_WARMING_UP",
				"message":  "The LLM model is still loading. Please retry in 30 seconds.",
				"trace_id": traceID, // Include trace_id in response for client correlation
			})
			c.Abort()
			return
		}
		c.Next()
	}
}

func main() {
	port := flag.Int("port", 8080, "Port to listen on")
	debug := flag.Bool("debug", false, "Enable debug mode")
	withContext := flag.Bool("with-context", false, "Enable ContextManager for code context assembly")
	withTools := flag.Bool("with-tools", false, "Enable tool registry for agentic exploration")
	flag.Parse()

	// Set Gin mode
	if *debug {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}

	// I-3: Set up W3C TraceContext propagator for distributed tracing.
	// This enables trace context to flow from incoming HTTP headers through
	// all handlers and middleware, including WarmupGuardMiddleware.
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	// Create service with default config
	cfg := code_buddy.DefaultServiceConfig()
	svc := code_buddy.NewService(cfg)

	// Create handlers
	handlers := code_buddy.NewHandlers(svc)

	// Setup router
	router := gin.New()
	router.Use(gin.Recovery())
	// I-3: Add OTel middleware for distributed tracing context extraction.
	// This extracts trace context from W3C TraceContext headers (traceparent, tracestate)
	// and propagates it through the request context to all handlers.
	router.Use(otelgin.Middleware("aleutian-trace"))
	if *debug {
		router.Use(gin.Logger())
	}

	// Register routes under /v1/trace (aliased from code_buddy for compatibility)
	v1 := router.Group("/v1")
	code_buddy.RegisterRoutes(v1, handlers)

	// Setup agent loop and register routes
	agentEnabled := setupAgentLoop(v1, svc, *withContext, *withTools)

	// Print startup banner
	printBanner(*port, agentEnabled)

	// Handle graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-quit
		slog.Info("Shutting down Aleutian Trace server")
		os.Exit(0)
	}()

	// Start server
	addr := fmt.Sprintf(":%d", *port)
	slog.Info("Starting Aleutian Trace server", slog.String("address", addr))
	if err := router.Run(addr); err != nil {
		slog.Error("Failed to start server", slog.String("error", err.Error()))
		os.Exit(1)
	}
}

// setupAgentLoop initializes the agent loop and registers routes.
//
// Returns true if the agent is fully enabled with LLM support.
func setupAgentLoop(v1 *gin.RouterGroup, svc *code_buddy.Service, withContext, withTools bool) bool {
	ollamaClient, err := llm.NewOllamaClient()
	if err != nil {
		slog.Warn("Ollama not available", slog.String("error", err.Error()))
		slog.Info("Agent endpoints will use mock mode (default state transitions only)")
		slog.Info("Set OLLAMA_BASE_URL and OLLAMA_MODEL to enable LLM-powered agent")

		// Mark warmup complete immediately for mock mode (no model to warm)
		markWarmupComplete()

		// Create agent loop without LLM (uses default phase execution)
		agentLoop := agent.NewDefaultAgentLoop()
		agentHandlers := code_buddy.NewAgentHandlers(agentLoop, svc)
		// No warmup guard needed for mock mode since warmup is already complete
		code_buddy.RegisterAgentRoutesWithMiddleware(v1, agentHandlers, nil)
		return false
	}

	model := os.Getenv("OLLAMA_MODEL")
	if model == "" {
		model = "glm-4.7-flash"
	}
	slog.Info("Ollama connected", slog.String("model", model))

	// Create LLM adapter
	llmClient := agentllm.NewOllamaAdapter(ollamaClient, model)

	// S-1: Move warmup to background goroutine for non-blocking startup.
	// Server starts immediately and responds with 503 if warmup not complete.
	// The WarmupGuardMiddleware protects agent endpoints during warmup.
	slog.Info("Server starting, model warmup in progress...",
		slog.String("model", model))

	go func() {
		warmupCtx, warmupCancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer warmupCancel()

		startTime := time.Now()
		if warmErr := warmMainModel(warmupCtx, ollamaClient, model); warmErr != nil {
			slog.Warn("Main model warmup failed, LLM classifier may fall back to regex",
				slog.String("model", model),
				slog.String("error", warmErr.Error()),
				slog.Duration("duration", time.Since(startTime)))
		} else {
			slog.Info("Model warmup completed successfully",
				slog.String("model", model),
				slog.Duration("duration", time.Since(startTime)))
		}

		// Mark warmup complete regardless of success/failure.
		// If warmup failed, the LLM classifier will fall back to regex on first call.
		markWarmupComplete()
		slog.Info("Server ready to accept agent requests",
			slog.String("model", model))
	}()

	// Create LLM classifier for better query classification.
	// The classifier is created immediately but will work correctly once warmup completes.
	// If called before warmup, it may experience slower first call or fall back to regex.
	llmClassifier, classifierErr := createLLMClassifier(llmClient)
	if classifierErr != nil {
		slog.Warn("Failed to create LLM classifier, using regex fallback",
			slog.String("error", classifierErr.Error()))
	}

	// Create phase registry with actual phase implementations
	registry := agent.NewPhaseRegistry()
	registry.Register(agent.StateInit, code_buddy.NewPhaseAdapter(phases.NewInitPhase()))
	registry.Register(agent.StatePlan, code_buddy.NewPhaseAdapter(phases.NewPlanPhase()))

	// Use LLM classifier if available
	if llmClassifier != nil {
		slog.Info("Using LLM-based query classifier")
		registry.Register(agent.StateExecute, code_buddy.NewPhaseAdapter(
			phases.NewExecutePhase(phases.WithQueryClassifier(llmClassifier)),
		))
	} else {
		registry.Register(agent.StateExecute, code_buddy.NewPhaseAdapter(phases.NewExecutePhase()))
	}

	registry.Register(agent.StateReflect, code_buddy.NewPhaseAdapter(phases.NewReflectPhase()))
	registry.Register(agent.StateClarify, code_buddy.NewPhaseAdapter(phases.NewClarifyPhase()))
	slog.Info("Registered phases", slog.Int("count", registry.Count()))

	// Create graph provider wrapping the service
	serviceAdapter := code_buddy.NewServiceAdapter(svc)
	graphProvider := agent.NewServiceGraphProvider(serviceAdapter)

	// Create event emitter
	eventEmitter := events.NewEmitter()

	// Create safety gate
	safetyGate := safety.NewDefaultGate(nil)

	// Create dependencies factory
	// GR-39: Enable Coordinator and Session Restore for CRS persistence
	depsFactory := code_buddy.NewDependenciesFactory(
		code_buddy.WithLLMClient(llmClient),
		code_buddy.WithGraphProvider(graphProvider),
		code_buddy.WithEventEmitter(eventEmitter),
		code_buddy.WithSafetyGate(safetyGate),
		code_buddy.WithService(svc),
		code_buddy.WithContextEnabled(withContext),
		code_buddy.WithToolsEnabled(withTools),
		code_buddy.WithCoordinatorEnabled(true),
		code_buddy.WithSessionRestoreEnabled(true),
	)

	if withContext {
		slog.Info("ContextManager ENABLED (code context will be assembled)")
	}
	if withTools {
		slog.Info("ToolRegistry ENABLED (agent can use exploration tools)")
	}

	// Create agent loop with phases and dependency factory
	agentLoop := agent.NewDefaultAgentLoop(
		agent.WithPhaseRegistry(registry),
		agent.WithDependenciesFactory(depsFactory),
	)
	agentHandlers := code_buddy.NewAgentHandlers(agentLoop, svc)

	// S-1: Apply warmup guard middleware to agent routes.
	// This returns 503 Service Unavailable for agent requests during model warmup.
	code_buddy.RegisterAgentRoutesWithMiddleware(v1, agentHandlers, WarmupGuardMiddleware())
	return true
}

func printBanner(port int, agentEnabled bool) {
	agentStatus := "DISABLED (set OLLAMA_BASE_URL to enable)"
	if agentEnabled {
		agentStatus = "ENABLED (Ollama connected)"
	}

	banner := `
╔═══════════════════════════════════════════════════════════════════╗
║                      ALEUTIAN TRACE SERVER                        ║
╠═══════════════════════════════════════════════════════════════════╣
║                                                                   ║
║  AST-powered code intelligence with LLM agent capabilities.       ║
║  Agent Loop: %-50s ║
║                                                                   ║
║  Quick Start:                                                     ║
║  ┌─────────────────────────────────────────────────────────────┐  ║
║  │ # Health check                                              │  ║
║  │ curl http://localhost:%d/v1/codebuddy/health              │  ║
║  │                                                             │  ║
║  │ # List all 30+ agentic tools                                │  ║
║  │ curl http://localhost:%d/v1/codebuddy/tools | jq          │  ║
║  │                                                             │  ║
║  │ # Initialize a graph (required first!)                      │  ║
║  │ curl -X POST http://localhost:%d/v1/codebuddy/init \      │  ║
║  │   -H "Content-Type: application/json" \                     │  ║
║  │   -d '{"project_root": "/your/project/path"}'               │  ║
║  │                                                             │  ║
║  │ # Run agent query (requires Ollama)                         │  ║
║  │ curl -X POST http://localhost:%d/v1/codebuddy/agent/run \ │  ║
║  │   -H "Content-Type: application/json" \                     │  ║
║  │   -d '{"project_root": ".", "query": "What does this do?"}' │  ║
║  └─────────────────────────────────────────────────────────────┘  ║
║                                                                   ║
║  Endpoints:                                                       ║
║  ├── Core: /init, /context, /symbol/:id, /callers, /impl         ║
║  ├── Explore (9): entry_points, data_flow, error_flow, etc.      ║
║  ├── Reason (6): breaking_changes, simulate, validate, etc.      ║
║  ├── Coordinate (3): plan_changes, validate_plan, preview        ║
║  ├── Patterns (6): detect, code_smells, duplication, etc.        ║
║  └── Agent (4): /run, /continue, /abort, /:id                    ║
║                                                                   ║
║  Press Ctrl+C to stop                                             ║
╚═══════════════════════════════════════════════════════════════════╝
`
	fmt.Printf(banner, agentStatus, port, port, port, port)
}

// createLLMClassifier creates an LLM-based query classifier.
//
// Description:
//
//	Creates an LLMClassifier using the provided LLM client and static
//	tool definitions. This enables better query classification than
//	regex patterns alone.
//
// Inputs:
//
//	client - The LLM client for classification calls.
//
// Outputs:
//
//	classifier.QueryClassifier - The LLM classifier, or nil on error.
//	error - Non-nil if classifier creation fails.
//
// Limitations:
//
//   - Requires tool definitions to be non-empty. Returns error if none available.
//   - Relies on default classifier configuration. For custom config, use NewLLMClassifier directly.
//   - May fail if LLM client is in degraded state (timeout, rate-limited).
//   - First classification call may be slow due to model cold-start.
//
// Assumptions:
//
//   - LLM client is fully initialized and connected.
//   - Tool definitions are stable during operation.
//   - DefaultClassifierConfig() returns valid configuration for this LLM.
//
// Thread Safety: This function is safe for concurrent use.
func createLLMClassifier(client agentllm.Client) (classifier.QueryClassifier, error) {
	toolDefs := tools.StaticToolDefinitions()
	if len(toolDefs) == 0 {
		return nil, fmt.Errorf("no static tool definitions available")
	}

	config := classifier.DefaultClassifierConfig()
	return classifier.NewLLMClassifier(client, toolDefs, config)
}

// warmMainModel pre-loads the main LLM model into VRAM to prevent cold-start issues.
//
// Description:
//
//	Sends a minimal "ping" request to the Ollama server to trigger model loading.
//	This prevents empty response errors when the LLMClassifier makes its first call.
//	The model is kept alive with keep_alive=-1 to prevent unloading.
//
// Inputs:
//
//	ctx - Context for cancellation/timeout. Should have 60-120s timeout.
//	client - The OllamaClient to use for warmup.
//	model - The model name to warm (e.g., "glm-4.7-flash").
//
// Outputs:
//
//	error - Non-nil if warmup fails. Caller should log warning but continue.
//
// Example:
//
//	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
//	defer cancel()
//	if err := warmMainModel(ctx, ollamaClient, model); err != nil {
//	    slog.Warn("Model warmup failed", slog.String("error", err.Error()))
//	}
//
// Limitations:
//
//   - Warmup failure is non-fatal; system falls back to lazy-loading on first request.
//   - Very large models (>50GB) may timeout even with 2-minute context.
//   - Context window (65536 tokens) is hardcoded to match main agent configuration.
//   - No retry logic; single attempt only. Caller may implement retry if needed.
//
// Assumptions:
//
//   - Ollama server is reachable at its configured endpoint.
//   - Model has already been pulled by Ollama (not downloaded during warmup).
//   - No other processes are competing for VRAM during warmup.
//
// Thread Safety: This function is safe for concurrent use.
func warmMainModel(ctx context.Context, client *llm.OllamaClient, model string) error {
	// R-5: Validate model parameter
	if model == "" {
		return fmt.Errorf("model must not be empty")
	}

	startTime := time.Now()

	// O-1: Add OTel span for distributed tracing
	ctx, span := otel.Tracer("aleutian.trace").Start(ctx, "warmMainModel")
	defer span.End()
	// Use 24h keep_alive to match router configuration.
	// Note: "-1" is invalid Go duration format and causes Ollama 400 error.
	// 24h is long enough to keep model warm during testing sessions.
	const keepAlive = "24h"

	span.SetAttributes(
		attribute.String("model", model),
		attribute.Int("num_ctx", 65536),
		attribute.String("keep_alive", keepAlive),
	)

	slog.Info("Warming main model",
		slog.String("model", model),
		slog.String("keep_alive", keepAlive),
	)

	// Build minimal warmup request with large context window for main model.
	// The context window MUST match what the main agent uses (64K tokens)
	// to ensure the model is loaded with the correct configuration.
	numCtx := 65536
	params := llm.GenerationParams{
		KeepAlive: keepAlive,
		NumCtx:    &numCtx,
	}

	// Send minimal message to trigger model loading
	messages := []datatypes.Message{
		{Role: "user", Content: "ping"},
	}

	// Call Chat to trigger model loading
	response, err := client.Chat(ctx, messages, params)
	duration := time.Since(startTime)

	// R-1: Check context cancellation after Chat returns
	if ctx.Err() != nil {
		span.SetStatus(codes.Error, "context cancelled")
		slog.Error("Main model warmup cancelled",
			slog.String("model", model),
			slog.String("error", ctx.Err().Error()),
			slog.Duration("duration", duration),
		)
		// O-2: Record warmup failure metric
		recordWarmupMetric(model, duration, false)
		return fmt.Errorf("warmup cancelled: %w", ctx.Err())
	}

	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "warmup failed")
		slog.Error("Main model warmup failed",
			slog.String("model", model),
			slog.String("error", err.Error()),
			slog.String("error_type", fmt.Sprintf("%T", err)),
			slog.Duration("duration", duration),
		)
		// O-2: Record warmup failure metric
		recordWarmupMetric(model, duration, false)
		return fmt.Errorf("warmup chat failed: %w", err)
	}

	// O-2 (OllamaClient): Validate response is non-empty
	if len(strings.TrimSpace(response)) == 0 {
		span.SetStatus(codes.Error, "empty response")
		slog.Error("Main model warmup received empty response",
			slog.String("model", model),
			slog.Duration("duration", duration),
		)
		// O-2: Record warmup failure metric
		recordWarmupMetric(model, duration, false)
		return fmt.Errorf("warmup received empty response from model %s", model)
	}

	span.SetStatus(codes.Ok, "warmup successful")
	span.SetAttributes(
		attribute.Int("response_len", len(response)),
		attribute.Int64("duration_ms", duration.Milliseconds()),
	)

	slog.Info("Main model warmed successfully",
		slog.String("model", model),
		slog.Duration("duration", duration),
		slog.Int("response_len", len(response)),
	)

	// O-2: Record warmup success metric
	recordWarmupMetric(model, duration, true)

	return nil
}

// recordWarmupMetric records model warmup metrics for Prometheus.
//
// Description:
//
//	Records warmup duration and success/failure status for monitoring.
//	Uses Prometheus histogram for duration and counter for success/failure.
//
// Thread Safety: This function is safe for concurrent use.
func recordWarmupMetric(model string, duration time.Duration, success bool) {
	// Note: This is a placeholder for actual Prometheus metrics.
	// In production, this should call:
	//   routing.RecordModelWarmup(model, duration.Seconds(), success)
	// For now, just log at debug level.
	status := "success"
	if !success {
		status = "failure"
	}
	slog.Debug("Model warmup metric recorded",
		slog.String("model", model),
		slog.Duration("duration", duration),
		slog.String("status", status),
	)
}
