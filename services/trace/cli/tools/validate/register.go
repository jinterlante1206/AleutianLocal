// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package validate

import (
	"github.com/AleutianAI/AleutianFOSS/services/trace/cli/tools"
)

// RegisterValidateTools registers all validation tools with the registry.
//
// Description:
//
//	Registers all CB-56 validation tools (validate_syntax, run_tests, etc.)
//	with the provided tool registry. These tools support the Multi-Step
//	Change Execution Framework for safe code modifications.
//
// Inputs:
//
//	registry - The tool registry to register with.
//	config - Configuration for validation tools.
//
// Example:
//
//	registry := tools.NewRegistry()
//	config := validate.NewConfig("/path/to/project")
//	validate.RegisterValidateTools(registry, config)
//
// Thread Safety: This function is safe to call once during initialization.
func RegisterValidateTools(registry *tools.Registry, config *Config) {
	// CB-56a: Syntax validation
	registry.Register(NewSyntaxTool(config))

	// CB-56b: Test execution (to be added)
	// registry.Register(NewTestTool(config))

	// CB-56c: Breaking change detection (to be added)
	// registry.Register(NewBreakingTool(config, graph, index))

	// CB-56d: Impact estimation (to be added)
	// registry.Register(NewImpactTool(config, graph, index))
}

// StaticValidateToolDefinitions returns tool definitions without requiring config.
//
// Description:
//
//	Returns the definitions for all validation tools. These can be used for
//	query classification without initializing the full tool system.
//
// Outputs:
//
//	[]tools.ToolDefinition - The static tool definitions.
//
// Thread Safety: This function is safe for concurrent use.
func StaticValidateToolDefinitions() []tools.ToolDefinition {
	dummyConfig := NewConfig("/")

	return []tools.ToolDefinition{
		NewSyntaxTool(dummyConfig).Definition(),
	}
}
