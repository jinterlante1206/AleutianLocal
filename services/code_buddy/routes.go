// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package code_buddy

import (
	"github.com/gin-gonic/gin"
)

// RegisterRoutes registers all Code Buddy routes with the router.
//
// Description:
//
//	Registers all /v1/codebuddy/* endpoints with the given Gin router group.
//	The router group should already have any required middleware applied.
//
// Inputs:
//
//	rg - Gin router group (typically /v1)
//	handlers - The handlers instance
//
// Endpoints:
//
//	POST /v1/codebuddy/init - Initialize a code graph
//	POST /v1/codebuddy/context - Assemble context for LLM prompt
//	GET  /v1/codebuddy/symbol/:id - Get symbol by ID
//	GET  /v1/codebuddy/callers - Find function callers
//	GET  /v1/codebuddy/implementations - Find interface implementations
//	POST /v1/codebuddy/seed - Seed library documentation
//	GET  /v1/codebuddy/memories - List memories
//	POST /v1/codebuddy/memories - Store a new memory
//	POST /v1/codebuddy/memories/retrieve - Semantic memory retrieval
//	DELETE /v1/codebuddy/memories/:id - Delete a memory
//	POST /v1/codebuddy/memories/:id/validate - Validate a memory
//	POST /v1/codebuddy/memories/:id/contradict - Contradict a memory
//	GET  /v1/codebuddy/health - Health check
//	GET  /v1/codebuddy/ready - Readiness check
//
// Example:
//
//	service := code_buddy.NewService(code_buddy.DefaultServiceConfig())
//	handlers := code_buddy.NewHandlers(service)
//
//	v1 := router.Group("/v1")
//	code_buddy.RegisterRoutes(v1, handlers)
func RegisterRoutes(rg *gin.RouterGroup, handlers *Handlers) {
	codebuddy := rg.Group("/codebuddy")
	{
		// Graph lifecycle
		codebuddy.POST("/init", handlers.HandleInit)

		// Context assembly
		codebuddy.POST("/context", handlers.HandleContext)

		// Symbol queries
		codebuddy.GET("/symbol/:id", handlers.HandleSymbol)
		codebuddy.GET("/callers", handlers.HandleCallers)
		codebuddy.GET("/implementations", handlers.HandleImplementations)

		// Library documentation seeding
		codebuddy.POST("/seed", handlers.HandleSeed)

		// Memory management
		codebuddy.GET("/memories", handlers.HandleListMemories)
		codebuddy.POST("/memories", handlers.HandleStoreMemory)
		codebuddy.POST("/memories/retrieve", handlers.HandleRetrieveMemories)
		codebuddy.DELETE("/memories/:id", handlers.HandleDeleteMemory)
		codebuddy.POST("/memories/:id/validate", handlers.HandleValidateMemory)
		codebuddy.POST("/memories/:id/contradict", handlers.HandleContradictMemory)

		// Health checks
		codebuddy.GET("/health", handlers.HandleHealth)
		codebuddy.GET("/ready", handlers.HandleReady)
	}
}

// RegisterAgentRoutes registers the Code Buddy agent routes with the router.
//
// Description:
//
//	Registers all /v1/codebuddy/agent/* endpoints with the given Gin router group.
//	These endpoints provide the agent loop functionality for AI-driven code
//	assistance with multi-step reasoning, tool execution, and clarification.
//
// Inputs:
//
//	rg - Gin router group (typically /v1)
//	handlers - The agent handlers instance
//
// Endpoints:
//
//	POST /v1/codebuddy/agent/run - Start a new agent session
//	POST /v1/codebuddy/agent/continue - Continue from CLARIFY state
//	POST /v1/codebuddy/agent/abort - Abort an active session
//	GET  /v1/codebuddy/agent/:id - Get session state
//
// Example:
//
//	loop := agent.NewDefaultAgentLoop()
//	service := code_buddy.NewService(config)
//	agentHandlers := code_buddy.NewAgentHandlers(loop, service)
//
//	v1 := router.Group("/v1")
//	code_buddy.RegisterAgentRoutes(v1, agentHandlers)
func RegisterAgentRoutes(rg *gin.RouterGroup, handlers *AgentHandlers) {
	agent := rg.Group("/codebuddy/agent")
	{
		// Session lifecycle
		agent.POST("/run", handlers.HandleAgentRun)
		agent.POST("/continue", handlers.HandleAgentContinue)
		agent.POST("/abort", handlers.HandleAgentAbort)

		// Session state
		agent.GET("/:id", handlers.HandleAgentState)
	}
}
