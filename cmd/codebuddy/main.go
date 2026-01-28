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
//	# Find entry points
//	curl -X POST http://localhost:8080/v1/codebuddy/explore/entry_points \
//	  -H "Content-Type: application/json" \
//	  -d '{"graph_id": "YOUR_GRAPH_ID"}'
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/AleutianAI/AleutianFOSS/services/code_buddy"
	"github.com/gin-gonic/gin"
)

func main() {
	port := flag.Int("port", 8080, "Port to listen on")
	debug := flag.Bool("debug", false, "Enable debug mode")
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

	// Print startup banner
	printBanner(*port)

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

func printBanner(port int) {
	banner := `
╔═══════════════════════════════════════════════════════════════════╗
║                     CODE BUDDY TEST SERVER                        ║
╠═══════════════════════════════════════════════════════════════════╣
║                                                                   ║
║  A standalone server for testing Code Buddy HTTP endpoints.       ║
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
║  └─────────────────────────────────────────────────────────────┘  ║
║                                                                   ║
║  Endpoints:                                                       ║
║  ├── Core: /init, /context, /symbol/:id, /callers, /impl         ║
║  ├── Explore (9): entry_points, data_flow, error_flow, etc.      ║
║  ├── Reason (6): breaking_changes, simulate, validate, etc.      ║
║  ├── Coordinate (3): plan_changes, validate_plan, preview        ║
║  └── Patterns (6): detect, code_smells, duplication, etc.        ║
║                                                                   ║
║  Press Ctrl+C to stop                                             ║
╚═══════════════════════════════════════════════════════════════════╝
`
	fmt.Printf(banner, port, port, port)
}
