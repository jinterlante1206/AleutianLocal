// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.

package context

import (
	"strings"
	"testing"

	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/agent"
	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/agent/tools"
)

func TestDefaultManagerConfig_SystemPrompt(t *testing.T) {
	cfg := DefaultManagerConfig()

	t.Run("starts with mandatory instruction", func(t *testing.T) {
		if !strings.HasPrefix(cfg.SystemPrompt, "## MANDATORY") {
			t.Error("System prompt must start with MANDATORY section")
		}
	})

	t.Run("includes tool-first requirement", func(t *testing.T) {
		if !strings.Contains(cfg.SystemPrompt, "MUST start with a tool call") {
			t.Error("System prompt missing 'MUST start with a tool call' instruction")
		}
	})

	t.Run("includes DO NOT prohibitions", func(t *testing.T) {
		prohibitions := []string{
			"I'm ready to help",
			"What would you like",
			"DECIDE AND ACT",
		}
		for _, p := range prohibitions {
			if !strings.Contains(cfg.SystemPrompt, p) {
				t.Errorf("System prompt missing prohibition: %s", p)
			}
		}
	})

	t.Run("includes question-tool mapping", func(t *testing.T) {
		if !strings.Contains(cfg.SystemPrompt, "QUESTION → TOOL MAPPING") {
			t.Error("System prompt missing question-tool mapping section")
		}
	})

	t.Run("includes grounding rules", func(t *testing.T) {
		if !strings.Contains(cfg.SystemPrompt, "GROUNDING RULES") {
			t.Error("System prompt missing grounding rules")
		}
	})

	t.Run("includes tool examples", func(t *testing.T) {
		examples := []string{
			"find_entry_points",
			"trace_data_flow",
			"trace_error_flow",
		}
		for _, example := range examples {
			if !strings.Contains(cfg.SystemPrompt, example) {
				t.Errorf("System prompt missing tool example: %s", example)
			}
		}
	})

	t.Run("includes citation format", func(t *testing.T) {
		// Check for citation format examples - we have several in the enhanced prompt
		hasCitation := strings.Contains(cfg.SystemPrompt, "[file.go:line]") ||
			strings.Contains(cfg.SystemPrompt, "[cmd/main.go:") ||
			strings.Contains(cfg.SystemPrompt, "[complexity.go:") ||
			strings.Contains(cfg.SystemPrompt, "[main.go:")
		if !hasCitation {
			t.Error("System prompt missing citation format example")
		}
	})

	t.Run("includes response pattern", func(t *testing.T) {
		if !strings.Contains(cfg.SystemPrompt, "RESPONSE PATTERN") {
			t.Error("System prompt missing response pattern section")
		}
	})
}

func TestManager_InjectToolList(t *testing.T) {
	// Create a minimal manager for testing
	m := &Manager{
		config: DefaultManagerConfig(),
	}

	t.Run("empty tools returns unchanged prompt", func(t *testing.T) {
		prompt := "test prompt"
		result := m.injectToolList(prompt, nil)
		if result != prompt {
			t.Errorf("Expected unchanged prompt for nil tools, got %q", result)
		}

		result = m.injectToolList(prompt, []tools.ToolDefinition{})
		if result != prompt {
			t.Errorf("Expected unchanged prompt for empty tools, got %q", result)
		}
	})

	t.Run("injects tool list before QUESTION marker", func(t *testing.T) {
		// Use a prompt with the expected marker
		prompt := "## MANDATORY section\n\n## QUESTION → TOOL MAPPING\n\nMapping content"
		toolDefs := []tools.ToolDefinition{
			{Name: "test_tool", Description: "A test tool"},
			{Name: "another_tool", Description: "Another test tool"},
		}

		result := m.injectToolList(prompt, toolDefs)

		// Should start with MANDATORY (unchanged)
		if !strings.HasPrefix(result, "## MANDATORY") {
			t.Error("MANDATORY section should remain at the beginning")
		}

		// Should contain AVAILABLE TOOLS
		if !strings.Contains(result, "## AVAILABLE TOOLS") {
			t.Error("Missing AVAILABLE TOOLS section")
		}

		// Should contain tool names
		if !strings.Contains(result, "test_tool") {
			t.Error("Missing test_tool in injected list")
		}
		if !strings.Contains(result, "another_tool") {
			t.Error("Missing another_tool in injected list")
		}

		// AVAILABLE TOOLS should come BEFORE QUESTION → TOOL MAPPING
		toolsIdx := strings.Index(result, "## AVAILABLE TOOLS")
		questionIdx := strings.Index(result, "## QUESTION → TOOL MAPPING")
		if toolsIdx >= questionIdx {
			t.Error("AVAILABLE TOOLS should be injected before QUESTION → TOOL MAPPING")
		}
	})

	t.Run("appends to end if marker not found", func(t *testing.T) {
		prompt := "Prompt without marker"
		toolDefs := []tools.ToolDefinition{
			{Name: "test_tool", Description: "A test tool"},
		}

		result := m.injectToolList(prompt, toolDefs)

		// Should have original prompt at start
		if !strings.HasPrefix(result, prompt) {
			t.Error("Original prompt should be preserved at start")
		}
		// Should have tools appended
		if !strings.Contains(result, "test_tool") {
			t.Error("Tools should be appended")
		}
	})

	t.Run("truncates long descriptions", func(t *testing.T) {
		prompt := "## QUESTION → TOOL MAPPING\ntest"
		longDesc := strings.Repeat("x", 100)
		toolDefs := []tools.ToolDefinition{
			{Name: "tool", Description: longDesc},
		}

		result := m.injectToolList(prompt, toolDefs)

		// Should not contain full description
		if strings.Contains(result, longDesc) {
			t.Error("Long description should be truncated")
		}
		// Should contain truncation indicator
		if !strings.Contains(result, "...") {
			t.Error("Truncated description should end with ...")
		}
	})
}

func TestManager_InjectToolsIntoPrompt(t *testing.T) {
	m := &Manager{
		config: DefaultManagerConfig(),
	}

	t.Run("nil context is safe", func(t *testing.T) {
		// Should not panic
		m.InjectToolsIntoPrompt(nil, []tools.ToolDefinition{})
	})

	t.Run("empty tools is safe", func(t *testing.T) {
		ctx := &agent.AssembledContext{
			SystemPrompt: "original",
		}
		m.InjectToolsIntoPrompt(ctx, nil)
		if ctx.SystemPrompt != "original" {
			t.Error("Empty tools should not modify prompt")
		}
	})

	t.Run("injects tools into context", func(t *testing.T) {
		ctx := &agent.AssembledContext{
			SystemPrompt: "## QUESTION → TOOL MAPPING\noriginal prompt",
		}
		toolDefs := []tools.ToolDefinition{
			{Name: "find_entry_points", Description: "Find entry points"},
		}

		m.InjectToolsIntoPrompt(ctx, toolDefs)

		if !strings.Contains(ctx.SystemPrompt, "AVAILABLE TOOLS") {
			t.Error("Tools should be injected into context")
		}
		if !strings.Contains(ctx.SystemPrompt, "find_entry_points") {
			t.Error("Tool name should appear in prompt")
		}
		if !strings.Contains(ctx.SystemPrompt, "original prompt") {
			t.Error("Original prompt should be preserved")
		}
	})
}

func TestManager_InjectProjectLanguage(t *testing.T) {
	m := &Manager{
		config: DefaultManagerConfig(),
	}

	t.Run("injects language notice at correct position", func(t *testing.T) {
		prompt := "## MANDATORY section\n\n## QUESTION → TOOL MAPPING\nMapping content"
		result := m.injectProjectLanguage(prompt, "Go")

		// Should start with MANDATORY (unchanged)
		if !strings.HasPrefix(result, "## MANDATORY") {
			t.Error("MANDATORY section should remain at the beginning")
		}

		// Should contain PROJECT LANGUAGE section
		if !strings.Contains(result, "## PROJECT LANGUAGE") {
			t.Error("Missing PROJECT LANGUAGE section")
		}

		// Should contain the language
		if !strings.Contains(result, "**Go**") {
			t.Error("Language should be emphasized")
		}

		// PROJECT LANGUAGE should come BEFORE QUESTION → TOOL MAPPING
		langIdx := strings.Index(result, "## PROJECT LANGUAGE")
		questionIdx := strings.Index(result, "## QUESTION → TOOL MAPPING")
		if langIdx >= questionIdx {
			t.Error("PROJECT LANGUAGE should be inserted before QUESTION → TOOL MAPPING")
		}
	})

	t.Run("includes language in notice", func(t *testing.T) {
		prompt := "## QUESTION → TOOL MAPPING\ntest"
		result := m.injectProjectLanguage(prompt, "Python")
		if !strings.Contains(result, "**Python**") {
			t.Error("Language should be emphasized in notice")
		}
	})

	t.Run("handles prompt without markers", func(t *testing.T) {
		prompt := "Simple prompt without markers"
		result := m.injectProjectLanguage(prompt, "Go")

		// Should still contain the language notice somewhere
		if !strings.Contains(result, "**Go**") {
			t.Error("Language should still be injected")
		}
		// Should contain original prompt
		if !strings.Contains(result, "Simple prompt") {
			t.Error("Original prompt should be preserved")
		}
	})
}

func TestSystemPromptOrder(t *testing.T) {
	cfg := DefaultManagerConfig()

	// Verify the order of sections
	t.Run("MANDATORY comes first", func(t *testing.T) {
		mandatoryIdx := strings.Index(cfg.SystemPrompt, "## MANDATORY")
		if mandatoryIdx != 0 {
			t.Error("MANDATORY section must be at position 0")
		}
	})

	t.Run("QUESTION → TOOL MAPPING comes before GROUNDING RULES", func(t *testing.T) {
		questionIdx := strings.Index(cfg.SystemPrompt, "## QUESTION → TOOL MAPPING")
		groundingIdx := strings.Index(cfg.SystemPrompt, "## GROUNDING RULES")

		if questionIdx >= groundingIdx {
			t.Error("QUESTION → TOOL MAPPING should come before GROUNDING RULES")
		}
	})

	t.Run("GROUNDING RULES comes before RESPONSE PATTERN", func(t *testing.T) {
		groundingIdx := strings.Index(cfg.SystemPrompt, "## GROUNDING RULES")
		responseIdx := strings.Index(cfg.SystemPrompt, "## RESPONSE PATTERN")

		if groundingIdx >= responseIdx {
			t.Error("GROUNDING RULES should come before RESPONSE PATTERN")
		}
	})
}
