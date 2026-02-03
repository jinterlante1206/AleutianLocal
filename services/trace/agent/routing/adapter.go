// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package routing

import (
	"context"

	"github.com/AleutianAI/AleutianFOSS/services/trace/agent"
)

// RouterAdapter adapts a routing.ToolRouter to the agent.ToolRouter interface.
//
// # Description
//
// The routing package defines its own types (ToolSpec, CodeContext, ToolSelection)
// for internal use, but the agent package expects its own types (ToolRouterSpec,
// ToolRouterCodeContext, ToolRouterSelection). This adapter bridges the two.
//
// # Thread Safety
//
// RouterAdapter is safe for concurrent use if the underlying router is.
type RouterAdapter struct {
	router *Granite4Router
}

// NewRouterAdapter creates an adapter that implements agent.ToolRouter.
//
// # Inputs
//
//   - router: The underlying Granite4Router.
//
// # Outputs
//
//   - agent.ToolRouter: The adapted router.
func NewRouterAdapter(router *Granite4Router) agent.ToolRouter {
	return &RouterAdapter{router: router}
}

// SelectTool implements agent.ToolRouter.
//
// Converts agent types to routing types, calls the underlying router,
// and converts the result back to agent types.
func (a *RouterAdapter) SelectTool(ctx context.Context, query string, availableTools []agent.ToolRouterSpec, codeContext *agent.ToolRouterCodeContext) (*agent.ToolRouterSelection, error) {
	// Convert agent.ToolRouterSpec to routing.ToolSpec
	routingSpecs := make([]ToolSpec, len(availableTools))
	for i, spec := range availableTools {
		routingSpecs[i] = ToolSpec{
			Name:        spec.Name,
			Description: spec.Description,
			BestFor:     spec.BestFor,
			Params:      spec.Params,
			Category:    spec.Category,
		}
	}

	// Convert agent.ToolRouterCodeContext to routing.CodeContext
	var routingContext *CodeContext
	if codeContext != nil {
		routingContext = &CodeContext{
			Language:    codeContext.Language,
			Files:       codeContext.Files,
			Symbols:     codeContext.Symbols,
			CurrentFile: codeContext.CurrentFile,
			RecentTools: codeContext.RecentTools,
		}

		// Convert PreviousErrors if present
		if len(codeContext.PreviousErrors) > 0 {
			routingContext.PreviousErrors = make([]ToolError, len(codeContext.PreviousErrors))
			for i, err := range codeContext.PreviousErrors {
				routingContext.PreviousErrors[i] = ToolError{
					Tool:      err.Tool,
					Error:     err.Error,
					Timestamp: err.Timestamp,
				}
			}
		}
	}

	// Call the underlying router
	selection, err := a.router.SelectTool(ctx, query, routingSpecs, routingContext)
	if err != nil {
		return nil, err
	}

	// Convert routing.ToolSelection to agent.ToolRouterSelection
	return &agent.ToolRouterSelection{
		Tool:       selection.Tool,
		Confidence: selection.Confidence,
		ParamsHint: selection.ParamsHint,
		Reasoning:  selection.Reasoning,
		Duration:   selection.Duration,
	}, nil
}

// Model implements agent.ToolRouter.
func (a *RouterAdapter) Model() string {
	return a.router.Model()
}

// Close implements agent.ToolRouter.
func (a *RouterAdapter) Close() error {
	return a.router.Close()
}

// WarmRouter exposes the underlying router's WarmRouter method.
//
// # Description
//
// Allows callers to warm the router model. This is not part of the
// agent.ToolRouter interface but is useful during initialization.
func (a *RouterAdapter) WarmRouter(ctx context.Context) error {
	return a.router.WarmRouter(ctx)
}
