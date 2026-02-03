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
	"log/slog"

	"github.com/AleutianAI/AleutianFOSS/services/trace/agent"
	agentcontext "github.com/AleutianAI/AleutianFOSS/services/trace/agent/context"
	"github.com/AleutianAI/AleutianFOSS/services/trace/agent/events"
	"github.com/AleutianAI/AleutianFOSS/services/trace/agent/grounding"
	"github.com/AleutianAI/AleutianFOSS/services/trace/agent/llm"
	"github.com/AleutianAI/AleutianFOSS/services/trace/agent/phases"
	"github.com/AleutianAI/AleutianFOSS/services/trace/agent/safety"
	"github.com/AleutianAI/AleutianFOSS/services/trace/cli/tools"
	"github.com/AleutianAI/AleutianFOSS/services/trace/cli/tools/file"
)

// DefaultDependenciesFactory creates phase Dependencies for agent sessions.
//
// Description:
//
//	DefaultDependenciesFactory holds shared components (LLM client, tool registry,
//	etc.) and creates per-session Dependencies structs when Create is called.
//	When enableContext or enableTools are set, it creates ContextManager and
//	ToolRegistry dynamically using the graph from the Service.
//
// Thread Safety: DefaultDependenciesFactory is safe for concurrent use.
type DefaultDependenciesFactory struct {
	llmClient        llm.Client
	graphProvider    phases.GraphProvider
	toolRegistry     *tools.Registry
	toolExecutor     *tools.Executor
	safetyGate       safety.Gate
	eventEmitter     *events.Emitter
	responseGrounder grounding.Grounder

	// service provides access to cached graphs for context/tools
	service *Service

	// enableContext enables ContextManager creation when graph is available
	enableContext bool

	// enableTools enables ToolRegistry creation when graph is available
	enableTools bool
}

// DependenciesFactoryOption configures a DefaultDependenciesFactory.
type DependenciesFactoryOption func(*DefaultDependenciesFactory)

// NewDependenciesFactory creates a new dependencies factory.
//
// Description:
//
//	Creates a factory with the provided options. Use the With* functions
//	to configure the shared components.
//
// Inputs:
//
//	opts - Configuration options.
//
// Outputs:
//
//	*DefaultDependenciesFactory - The configured factory.
func NewDependenciesFactory(opts ...DependenciesFactoryOption) *DefaultDependenciesFactory {
	f := &DefaultDependenciesFactory{}

	for _, opt := range opts {
		opt(f)
	}

	return f
}

// WithLLMClient sets the LLM client.
func WithLLMClient(client llm.Client) DependenciesFactoryOption {
	return func(f *DefaultDependenciesFactory) {
		f.llmClient = client
	}
}

// WithGraphProvider sets the graph provider.
func WithGraphProvider(provider phases.GraphProvider) DependenciesFactoryOption {
	return func(f *DefaultDependenciesFactory) {
		f.graphProvider = provider
	}
}

// WithToolRegistry sets the tool registry.
func WithToolRegistry(registry *tools.Registry) DependenciesFactoryOption {
	return func(f *DefaultDependenciesFactory) {
		f.toolRegistry = registry
	}
}

// WithToolExecutor sets the tool executor.
func WithToolExecutor(executor *tools.Executor) DependenciesFactoryOption {
	return func(f *DefaultDependenciesFactory) {
		f.toolExecutor = executor
	}
}

// WithSafetyGate sets the safety gate.
func WithSafetyGate(gate safety.Gate) DependenciesFactoryOption {
	return func(f *DefaultDependenciesFactory) {
		f.safetyGate = gate
	}
}

// WithEventEmitter sets the event emitter.
func WithEventEmitter(emitter *events.Emitter) DependenciesFactoryOption {
	return func(f *DefaultDependenciesFactory) {
		f.eventEmitter = emitter
	}
}

// WithService sets the service for accessing cached graphs.
func WithService(svc *Service) DependenciesFactoryOption {
	return func(f *DefaultDependenciesFactory) {
		f.service = svc
	}
}

// WithContextEnabled enables ContextManager creation.
func WithContextEnabled(enabled bool) DependenciesFactoryOption {
	return func(f *DefaultDependenciesFactory) {
		f.enableContext = enabled
	}
}

// WithToolsEnabled enables ToolRegistry creation.
func WithToolsEnabled(enabled bool) DependenciesFactoryOption {
	return func(f *DefaultDependenciesFactory) {
		f.enableTools = enabled
	}
}

// WithResponseGrounder sets the response grounding validator.
func WithResponseGrounder(grounder grounding.Grounder) DependenciesFactoryOption {
	return func(f *DefaultDependenciesFactory) {
		f.responseGrounder = grounder
	}
}

// Create implements agent.DependenciesFactory.
//
// Description:
//
//	Creates a Dependencies struct for the given session and query.
//	Uses the pre-configured shared components. Retrieves existing
//	context from the session if available (for cross-phase context sharing).
//	When enableContext or enableTools are set, creates ContextManager and
//	ToolRegistry using the graph from the Service.
//
// Inputs:
//
//	session - The current session.
//	query - The user's query.
//
// Outputs:
//
//	any - The Dependencies struct (as *phases.Dependencies).
//	error - Non-nil if creation failed.
//
// Thread Safety: This method is safe for concurrent use.
func (f *DefaultDependenciesFactory) Create(session *agent.Session, query string) (any, error) {
	deps := &phases.Dependencies{
		Session:          session,
		Query:            query,
		LLMClient:        f.llmClient,
		GraphProvider:    f.graphProvider,
		ToolRegistry:     f.toolRegistry,
		ToolExecutor:     f.toolExecutor,
		SafetyGate:       f.safetyGate,
		EventEmitter:     f.eventEmitter,
		ResponseGrounder: f.responseGrounder,
		// Retrieve existing context from session (persisted by PlanPhase)
		Context: session.GetCurrentContext(),
	}

	// Try to get the cached graph if we need context or tools
	if (f.enableContext || f.enableTools) && f.service != nil {
		graphID := session.GetGraphID()
		if graphID != "" {
			cached, err := f.service.GetGraph(graphID)
			if err == nil && cached != nil {
				slog.Info("Creating dependencies with graph",
					slog.String("session_id", session.ID),
					slog.String("graph_id", graphID),
					slog.Bool("with_context", f.enableContext),
					slog.Bool("with_tools", f.enableTools),
				)

				// Create ContextManager if enabled
				if f.enableContext && cached.Graph != nil && cached.Index != nil {
					mgr, err := agentcontext.NewManager(cached.Graph, cached.Index, nil)
					if err != nil {
						slog.Warn("Failed to create ContextManager",
							slog.String("error", err.Error()),
						)
					} else {
						deps.ContextManager = mgr
						slog.Info("ContextManager created",
							slog.String("session_id", session.ID),
						)
					}
				}

				// Create ToolRegistry if enabled
				if f.enableTools && cached.Graph != nil && cached.Index != nil {
					registry := tools.NewRegistry()

					// Register all CB-20 exploration tools (graph-based)
					registry.Register(tools.NewFindEntryPointsTool(cached.Graph, cached.Index))
					registry.Register(tools.NewTraceDataFlowTool(cached.Graph, cached.Index))
					registry.Register(tools.NewTraceErrorFlowTool(cached.Graph, cached.Index))
					registry.Register(tools.NewBuildMinimalContextTool(cached.Graph, cached.Index))
					registry.Register(tools.NewFindSimilarCodeTool(cached.Graph, cached.Index))
					registry.Register(tools.NewSummarizeFileTool(cached.Graph, cached.Index))
					registry.Register(tools.NewFindConfigUsageTool(cached.Graph, cached.Index))

					// Register CB-30 file operation tools (Read, Write, Edit, Glob, Grep, Diff, Tree, JSON)
					projectRoot := session.GetProjectRoot()
					if projectRoot != "" {
						fileConfig := file.NewConfig(projectRoot)
						file.RegisterFileTools(registry, fileConfig)
						slog.Info("File tools registered",
							slog.String("session_id", session.ID),
							slog.String("project_root", projectRoot),
						)
					}

					deps.ToolRegistry = registry
					deps.ToolExecutor = tools.NewExecutor(registry, nil)

					// Mark graph_initialized requirement as satisfied since we have a valid graph
					deps.ToolExecutor.SatisfyRequirement("graph_initialized")

					slog.Info("ToolRegistry created",
						slog.String("session_id", session.ID),
						slog.Int("tool_count", registry.Count()),
					)
				}
			}
		}
	}

	return deps, nil
}

// Ensure DefaultDependenciesFactory implements agent.DependenciesFactory.
var _ agent.DependenciesFactory = (*DefaultDependenciesFactory)(nil)
