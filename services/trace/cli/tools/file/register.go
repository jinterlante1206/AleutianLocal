// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package file

import (
	"github.com/AleutianAI/AleutianFOSS/services/trace/cli/tools"
)

// RegisterFileTools registers all file operation tools with the registry.
//
// Description:
//
//	Registers all file tools (Read, Write, Edit, Glob, Grep, Diff, Tree, JSON)
//	with the provided tool registry. These tools require a Config that
//	specifies the working directory and allowed paths.
//
// Inputs:
//
//	registry - The tool registry to register with
//	config - Configuration for file tools (working directory, allowed paths)
//
// Example:
//
//	registry := tools.NewRegistry()
//	config := file.NewConfig("/path/to/project")
//	file.RegisterFileTools(registry, config)
//
// Thread Safety: This function is safe to call once during initialization.
func RegisterFileTools(registry *tools.Registry, config *Config) {
	// Core file operations
	registry.Register(NewReadTool(config))
	registry.Register(NewWriteTool(config))
	registry.Register(NewEditTool(config))
	registry.Register(NewGlobTool(config))
	registry.Register(NewGrepTool(config))

	// Enhanced tools
	registry.Register(NewDiffTool(config))
	registry.Register(NewTreeTool(config))
	registry.Register(NewJSONTool(config))
}

// StaticFileToolDefinitions returns tool definitions without requiring config.
//
// Description:
//
//	Returns the definitions for all file tools. These can be used for
//	query classification without initializing the full tool system.
//	The definitions include tool names, descriptions, and parameter schemas.
//
// Outputs:
//
//	[]tools.ToolDefinition - The static tool definitions.
//
// Example:
//
//	defs := file.StaticFileToolDefinitions()
//	// Use defs for LLM tool routing
//
// Thread Safety: This function is safe for concurrent use.
func StaticFileToolDefinitions() []tools.ToolDefinition {
	// Create a dummy config just for getting definitions
	dummyConfig := NewConfig("/")

	return []tools.ToolDefinition{
		// Core file operations
		NewReadTool(dummyConfig).Definition(),
		NewWriteTool(dummyConfig).Definition(),
		NewEditTool(dummyConfig).Definition(),
		NewGlobTool(dummyConfig).Definition(),
		NewGrepTool(dummyConfig).Definition(),

		// Enhanced tools
		NewDiffTool(dummyConfig).Definition(),
		NewTreeTool(dummyConfig).Definition(),
		NewJSONTool(dummyConfig).Definition(),
	}
}
