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
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/AleutianAI/AleutianFOSS/services/llm"
	"github.com/AleutianAI/AleutianFOSS/services/trace"
	"github.com/AleutianAI/AleutianFOSS/services/trace/agent"
	"github.com/AleutianAI/AleutianFOSS/services/trace/agent/classifier"
	"github.com/AleutianAI/AleutianFOSS/services/trace/agent/events"
	agentllm "github.com/AleutianAI/AleutianFOSS/services/trace/agent/llm"
	"github.com/AleutianAI/AleutianFOSS/services/trace/agent/phases"
	"github.com/AleutianAI/AleutianFOSS/services/trace/agent/safety"
	"github.com/AleutianAI/AleutianFOSS/services/trace/cli/tools"
	"github.com/gin-gonic/gin"
)

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

	// Create service with default config
	cfg := code_buddy.DefaultServiceConfig()
	svc := code_buddy.NewService(cfg)

	// Create handlers
	handlers := code_buddy.NewHandlers(svc)

	// Setup router
	router := gin.New()
	router.Use(gin.Recovery())
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

		// Create agent loop without LLM (uses default phase execution)
		agentLoop := agent.NewDefaultAgentLoop()
		agentHandlers := code_buddy.NewAgentHandlers(agentLoop, svc)
		code_buddy.RegisterAgentRoutes(v1, agentHandlers)
		return false
	}

	model := os.Getenv("OLLAMA_MODEL")
	if model == "" {
		model = "glm-4.7-flash"
	}
	slog.Info("Ollama connected", slog.String("model", model))

	// Create LLM adapter
	llmClient := agentllm.NewOllamaAdapter(ollamaClient, model)

	// Create LLM classifier for better query classification
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
	code_buddy.RegisterAgentRoutes(v1, agentHandlers)
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
func createLLMClassifier(client agentllm.Client) (classifier.QueryClassifier, error) {
	toolDefs := tools.StaticToolDefinitions()
	if len(toolDefs) == 0 {
		return nil, fmt.Errorf("no static tool definitions available")
	}

	config := classifier.DefaultClassifierConfig()
	return classifier.NewLLMClassifier(client, toolDefs, config)
}
