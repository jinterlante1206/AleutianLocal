// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

// Command codebuddy starts a standalone Code Buddy API server for testing.
//
// Usage:
//
//	go run ./cmd/codebuddy
//	go run ./cmd/codebuddy -port 9090
//
// With Ollama (for agent loop):
//
//	OLLAMA_BASE_URL=http://localhost:11434 OLLAMA_MODEL=gpt-oss:20b go run ./cmd/codebuddy
//
// With context assembly (sends code to LLM):
//
//	OLLAMA_BASE_URL=http://localhost:11434 OLLAMA_MODEL=gpt-oss:20b go run ./cmd/codebuddy -with-context
//
// With tools enabled (LLM can use exploration tools):
//
//	OLLAMA_BASE_URL=http://localhost:11434 OLLAMA_MODEL=gpt-oss:20b go run ./cmd/codebuddy -with-tools
//
// Full features:
//
//	OLLAMA_BASE_URL=http://localhost:11434 OLLAMA_MODEL=gpt-oss:20b go run ./cmd/codebuddy -with-context -with-tools
//
// Example requests:
//
//	# Health check
//	curl http://localhost:8080/v1/codebuddy/health
//
//	# Get all available tools
//	curl http://localhost:8080/v1/codebuddy/tools | jq
//
//	# Initialize a code graph
//	curl -X POST http://localhost:8080/v1/codebuddy/init \
//	  -H "Content-Type: application/json" \
//	  -d '{"project_root": "/path/to/project"}'
//
//	# Run agent query (requires Ollama)
//	curl -X POST http://localhost:8080/v1/codebuddy/agent/run \
//	  -H "Content-Type: application/json" \
//	  -d '{"project_root": "/path/to/project", "query": "What are the main entry points?"}'
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/AleutianAI/AleutianFOSS/services/code_buddy"
	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/agent"
	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/agent/events"
	agentllm "github.com/AleutianAI/AleutianFOSS/services/code_buddy/agent/llm"
	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/agent/phases"
	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/agent/safety"
	"github.com/AleutianAI/AleutianFOSS/services/llm"
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

	// Register routes
	v1 := router.Group("/v1")
	code_buddy.RegisterRoutes(v1, handlers)

	// Try to initialize Ollama client for agent loop
	agentEnabled := false
	ollamaClient, err := llm.NewOllamaClient()
	if err != nil {
		log.Printf("Ollama not available: %v", err)
		log.Println("Agent endpoints will use mock mode (default state transitions only)")
		log.Println("Set OLLAMA_BASE_URL and OLLAMA_MODEL to enable LLM-powered agent")

		// Create agent loop without LLM (uses default phase execution)
		agentLoop := agent.NewDefaultAgentLoop()
		agentHandlers := code_buddy.NewAgentHandlers(agentLoop, svc)
		code_buddy.RegisterAgentRoutes(v1, agentHandlers)
	} else {
		agentEnabled = true
		model := os.Getenv("OLLAMA_MODEL")
		if model == "" {
			model = "gpt-oss"
		}
		log.Printf("Ollama connected: model=%s", model)

		// Create LLM adapter
		llmClient := agentllm.NewOllamaAdapter(ollamaClient, model)

		// Create phase registry with actual phase implementations
		registry := agent.NewPhaseRegistry()
		registry.Register(agent.StateInit, code_buddy.NewPhaseAdapter(phases.NewInitPhase()))
		registry.Register(agent.StatePlan, code_buddy.NewPhaseAdapter(phases.NewPlanPhase()))
		registry.Register(agent.StateExecute, code_buddy.NewPhaseAdapter(phases.NewExecutePhase()))
		registry.Register(agent.StateReflect, code_buddy.NewPhaseAdapter(phases.NewReflectPhase()))
		registry.Register(agent.StateClarify, code_buddy.NewPhaseAdapter(phases.NewClarifyPhase()))
		log.Printf("Registered %d phases", registry.Count())

		// Create graph provider wrapping the service
		serviceAdapter := code_buddy.NewServiceAdapter(svc)
		graphProvider := agent.NewServiceGraphProvider(serviceAdapter)

		// Create event emitter
		eventEmitter := events.NewEmitter()

		// Create safety gate
		safetyGate := safety.NewDefaultGate(nil)

		// Create dependencies factory
		depsFactory := code_buddy.NewDependenciesFactory(
			code_buddy.WithLLMClient(llmClient),
			code_buddy.WithGraphProvider(graphProvider),
			code_buddy.WithEventEmitter(eventEmitter),
			code_buddy.WithSafetyGate(safetyGate),
			code_buddy.WithService(svc),
			code_buddy.WithContextEnabled(*withContext),
			code_buddy.WithToolsEnabled(*withTools),
		)

		if *withContext {
			log.Println("ContextManager ENABLED (code context will be assembled)")
		}
		if *withTools {
			log.Println("ToolRegistry ENABLED (agent can use exploration tools)")
		}

		// Create agent loop with phases and dependency factory
		agentLoop := agent.NewDefaultAgentLoop(
			agent.WithPhaseRegistry(registry),
			agent.WithDependenciesFactory(depsFactory),
		)
		agentHandlers := code_buddy.NewAgentHandlers(agentLoop, svc)
		code_buddy.RegisterAgentRoutes(v1, agentHandlers)
	}

	// Print startup banner
	printBanner(*port, agentEnabled)

	// Handle graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-quit
		log.Println("\nShutting down Code Buddy server...")
		os.Exit(0)
	}()

	// Start server
	addr := fmt.Sprintf(":%d", *port)
	log.Printf("Starting Code Buddy server on %s", addr)
	if err := router.Run(addr); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}

func printBanner(port int, agentEnabled bool) {
	agentStatus := "DISABLED (set OLLAMA_BASE_URL to enable)"
	if agentEnabled {
		agentStatus = "ENABLED (Ollama connected)"
	}

	banner := `
╔═══════════════════════════════════════════════════════════════════╗
║                     CODE BUDDY TEST SERVER                        ║
╠═══════════════════════════════════════════════════════════════════╣
║                                                                   ║
║  A standalone server for testing Code Buddy HTTP endpoints.       ║
║  Agent Loop: %-50s ║
║                                                                   ║
║  Quick Start:                                                     ║
║  ┌─────────────────────────────────────────────────────────────┐  ║
║  │ # Health check                                              │  ║
║  │ curl http://localhost:%d/v1/codebuddy/health              │  ║
║  │                                                             │  ║
║  │ # List all 24 agentic tools                                 │  ║
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
