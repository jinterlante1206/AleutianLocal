// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package tools

import (
	"sort"
	"sync"
)

// Registry manages tool registration and lookup.
//
// The registry follows the same pattern as ast.ParserRegistry, providing
// thread-safe registration and lookup of tools by name and category.
//
// Thread Safety:
//
//	Registry is fully thread-safe. All methods can be called concurrently.
type Registry struct {
	mu sync.RWMutex

	// byName maps tool names to tool instances.
	byName map[string]Tool

	// byCategory maps categories to lists of tools.
	byCategory map[ToolCategory][]Tool
}

// NewRegistry creates a new empty tool registry.
//
// Outputs:
//
//	*Registry - Empty registry ready for tool registration
func NewRegistry() *Registry {
	return &Registry{
		byName:     make(map[string]Tool),
		byCategory: make(map[ToolCategory][]Tool),
	}
}

// Register adds a tool to the registry.
//
// Description:
//
//	Registers a tool under its Name() and Category(). If a tool with
//	the same name is already registered, it will be replaced.
//
// Inputs:
//
//	tool - The tool to register. Must not be nil.
//
// Thread Safety: This method is safe for concurrent use.
//
// Example:
//
//	registry := NewRegistry()
//	registry.Register(NewFindEntryPointsTool(graph, index))
//	registry.Register(NewTraceDataFlowTool(graph, index))
func (r *Registry) Register(tool Tool) {
	if tool == nil {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	name := tool.Name()
	category := tool.Category()

	// Check if we're replacing an existing tool
	if existing, ok := r.byName[name]; ok {
		// Remove from old category if category changed
		oldCategory := existing.Category()
		if oldCategory != category {
			r.removeFromCategory(oldCategory, name)
		}
	}

	// Register by name
	r.byName[name] = tool

	// Register by category
	if _, ok := r.byCategory[category]; !ok {
		r.byCategory[category] = make([]Tool, 0)
	}

	// Check if already in category (for replacement)
	found := false
	for i, t := range r.byCategory[category] {
		if t.Name() == name {
			r.byCategory[category][i] = tool
			found = true
			break
		}
	}
	if !found {
		r.byCategory[category] = append(r.byCategory[category], tool)
	}
}

// removeFromCategory removes a tool from a category list.
// Caller must hold the write lock.
func (r *Registry) removeFromCategory(category ToolCategory, name string) {
	tools, ok := r.byCategory[category]
	if !ok {
		return
	}

	for i, t := range tools {
		if t.Name() == name {
			r.byCategory[category] = append(tools[:i], tools[i+1:]...)
			return
		}
	}
}

// Get returns a tool by name.
//
// Inputs:
//
//	name - The tool name
//
// Outputs:
//
//	Tool - The registered tool, or nil if not found
//	bool - True if the tool was found
//
// Thread Safety: This method is safe for concurrent use.
func (r *Registry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	tool, ok := r.byName[name]
	return tool, ok
}

// GetByCategory returns all tools in a category.
//
// Inputs:
//
//	category - The category to filter by
//
// Outputs:
//
//	[]Tool - Tools in the category (may be empty)
//
// Thread Safety: This method is safe for concurrent use.
func (r *Registry) GetByCategory(category ToolCategory) []Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	tools, ok := r.byCategory[category]
	if !ok {
		return nil
	}

	// Return a copy to avoid race conditions
	result := make([]Tool, len(tools))
	copy(result, tools)
	return result
}

// GetAll returns all registered tools.
//
// Outputs:
//
//	[]Tool - All registered tools
//
// Thread Safety: This method is safe for concurrent use.
func (r *Registry) GetAll() []Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]Tool, 0, len(r.byName))
	for _, tool := range r.byName {
		result = append(result, tool)
	}
	return result
}

// GetByCategories returns tools from multiple categories.
//
// Inputs:
//
//	categories - Categories to include
//
// Outputs:
//
//	[]Tool - Tools in any of the specified categories
//
// Thread Safety: This method is safe for concurrent use.
func (r *Registry) GetByCategories(categories ...ToolCategory) []Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	seen := make(map[string]bool)
	var result []Tool

	for _, category := range categories {
		if tools, ok := r.byCategory[category]; ok {
			for _, tool := range tools {
				if !seen[tool.Name()] {
					seen[tool.Name()] = true
					result = append(result, tool)
				}
			}
		}
	}

	return result
}

// GetEnabled returns tools that match the enabled criteria.
//
// Inputs:
//
//	enabledCategories - Categories to include (empty = all)
//	disabledTools - Specific tool names to exclude
//
// Outputs:
//
//	[]Tool - Enabled tools sorted by priority
//
// Thread Safety: This method is safe for concurrent use.
func (r *Registry) GetEnabled(enabledCategories []string, disabledTools []string) []Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Build disabled set
	disabled := make(map[string]bool)
	for _, name := range disabledTools {
		disabled[name] = true
	}

	// Build enabled categories set
	enabledSet := make(map[ToolCategory]bool)
	for _, cat := range enabledCategories {
		enabledSet[ToolCategory(cat)] = true
	}

	var result []Tool

	for _, tool := range r.byName {
		// Check if disabled
		if disabled[tool.Name()] {
			continue
		}

		// Check category filter
		if len(enabledCategories) > 0 && !enabledSet[tool.Category()] {
			continue
		}

		result = append(result, tool)
	}

	// Sort by priority (higher first)
	sort.Slice(result, func(i, j int) bool {
		return result[i].Definition().Priority > result[j].Definition().Priority
	})

	return result
}

// Names returns all registered tool names.
//
// Outputs:
//
//	[]string - All tool names
//
// Thread Safety: This method is safe for concurrent use.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.byName))
	for name := range r.byName {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Categories returns all categories that have registered tools.
//
// Outputs:
//
//	[]ToolCategory - Categories with at least one tool
//
// Thread Safety: This method is safe for concurrent use.
func (r *Registry) Categories() []ToolCategory {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var categories []ToolCategory
	for category, tools := range r.byCategory {
		if len(tools) > 0 {
			categories = append(categories, category)
		}
	}
	return categories
}

// Count returns the number of registered tools.
//
// Outputs:
//
//	int - Total number of tools
//
// Thread Safety: This method is safe for concurrent use.
func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.byName)
}

// CountByCategory returns the number of tools in a category.
//
// Inputs:
//
//	category - The category to count
//
// Outputs:
//
//	int - Number of tools in the category
//
// Thread Safety: This method is safe for concurrent use.
func (r *Registry) CountByCategory(category ToolCategory) int {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if tools, ok := r.byCategory[category]; ok {
		return len(tools)
	}
	return 0
}

// Unregister removes a tool from the registry.
//
// Inputs:
//
//	name - The tool name to remove
//
// Outputs:
//
//	bool - True if the tool was found and removed
//
// Thread Safety: This method is safe for concurrent use.
func (r *Registry) Unregister(name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	tool, ok := r.byName[name]
	if !ok {
		return false
	}

	delete(r.byName, name)
	r.removeFromCategory(tool.Category(), name)
	return true
}

// GetDefinitions returns definitions for all registered tools.
//
// Outputs:
//
//	[]ToolDefinition - Definitions for all tools
//
// Thread Safety: This method is safe for concurrent use.
func (r *Registry) GetDefinitions() []ToolDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()

	definitions := make([]ToolDefinition, 0, len(r.byName))
	for _, tool := range r.byName {
		definitions = append(definitions, tool.Definition())
	}

	// Sort by priority for consistent ordering
	sort.Slice(definitions, func(i, j int) bool {
		if definitions[i].Priority != definitions[j].Priority {
			return definitions[i].Priority > definitions[j].Priority
		}
		return definitions[i].Name < definitions[j].Name
	})

	return definitions
}

// GetDefinitionsFiltered returns definitions for enabled tools.
//
// Inputs:
//
//	enabledCategories - Categories to include (empty = all)
//	disabledTools - Specific tool names to exclude
//
// Outputs:
//
//	[]ToolDefinition - Definitions for enabled tools
//
// Thread Safety: This method is safe for concurrent use.
func (r *Registry) GetDefinitionsFiltered(enabledCategories []string, disabledTools []string) []ToolDefinition {
	tools := r.GetEnabled(enabledCategories, disabledTools)
	definitions := make([]ToolDefinition, len(tools))
	for i, tool := range tools {
		definitions[i] = tool.Definition()
	}
	return definitions
}
