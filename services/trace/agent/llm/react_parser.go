// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.

package llm

import (
	"regexp"
	"strings"

	"github.com/google/uuid"
)

// ReActParseResult contains parsed ReAct components from LLM response text.
//
// Thread Safety: This type is immutable and safe for concurrent read access.
type ReActParseResult struct {
	// Thought is the LLM's reasoning (may be empty).
	Thought string

	// Action is the tool name to invoke (empty if no action).
	Action string

	// ActionInput is the JSON arguments (empty if no action).
	ActionInput string

	// FinalAnswer is the final response (empty if not done).
	FinalAnswer string

	// RemainingText is any unparsed text.
	RemainingText string
}

// ReAct pattern regexes with flexible matching.
// Uses case-insensitive matching and allows variable whitespace.
var (
	// thoughtPattern matches "Thought: ..." allowing multiline content
	// until Action:, Final Answer:, or end of string.
	thoughtPattern = regexp.MustCompile(`(?i)Thought\s*:\s*(.+?)(?:\n(?:Action|Final Answer)|$)`)

	// actionPattern matches "Action: tool_name" with flexible whitespace.
	actionPattern = regexp.MustCompile(`(?i)Action\s*:\s*(\S+)`)

	// actionInputPattern matches "Action Input: {...}" allowing multiline JSON.
	// Uses (?s) flag for dotall mode (. matches newlines).
	actionInputPattern = regexp.MustCompile(`(?is)Action\s+Input\s*:\s*(\{.*?\})(?:\n|$)`)

	// finalAnswerPattern matches "Final Answer: ..." to end of string.
	finalAnswerPattern = regexp.MustCompile(`(?is)Final\s+Answer\s*:\s*(.+)$`)
)

// ParseReAct extracts ReAct components from LLM response text.
//
// Description:
//
//	Parses LLM output for ReAct-style structured text. The ReAct format is:
//	  Thought: [reasoning]
//	  Action: [tool_name]
//	  Action Input: {"param": "value"}
//
//	Or for final responses:
//	  Thought: [reasoning]
//	  Final Answer: [response]
//
// Inputs:
//
//	text - The LLM response text to parse.
//
// Outputs:
//
//	*ReActParseResult - Parsed components. Never nil.
//
// Example:
//
//	result := ParseReAct("Thought: I need to find tests.\nAction: find_entry_points\nAction Input: {\"type\": \"test\"}")
//	// result.Action = "find_entry_points"
//	// result.ActionInput = "{\"type\": \"test\"}"
//
// Thread Safety: This function is safe for concurrent use.
func ParseReAct(text string) *ReActParseResult {
	result := &ReActParseResult{
		RemainingText: text,
	}

	// Extract Thought
	if matches := thoughtPattern.FindStringSubmatch(text); len(matches) > 1 {
		result.Thought = strings.TrimSpace(matches[1])
	}

	// Extract Action (tool name)
	if matches := actionPattern.FindStringSubmatch(text); len(matches) > 1 {
		result.Action = strings.TrimSpace(matches[1])
	}

	// Extract Action Input (JSON arguments)
	if matches := actionInputPattern.FindStringSubmatch(text); len(matches) > 1 {
		result.ActionInput = strings.TrimSpace(matches[1])
	}

	// Extract Final Answer
	if matches := finalAnswerPattern.FindStringSubmatch(text); len(matches) > 1 {
		result.FinalAnswer = strings.TrimSpace(matches[1])
	}

	return result
}

// HasAction returns true if an action was parsed.
//
// Description:
//
//	Indicates whether the LLM output contains a tool invocation.
//
// Outputs:
//
//	bool - True if Action field is non-empty.
//
// Thread Safety: This method is safe for concurrent use.
func (r *ReActParseResult) HasAction() bool {
	return r.Action != ""
}

// HasFinalAnswer returns true if a final answer was parsed.
//
// Description:
//
//	Indicates whether the LLM has provided a final response
//	(meaning no more tool calls are expected).
//
// Outputs:
//
//	bool - True if FinalAnswer field is non-empty.
//
// Thread Safety: This method is safe for concurrent use.
func (r *ReActParseResult) HasFinalAnswer() bool {
	return r.FinalAnswer != ""
}

// ToToolCall converts a ReAct action to a ToolCall.
//
// Description:
//
//	Creates a ToolCall struct from the parsed ReAct components.
//	Returns nil if no action was parsed.
//
// Outputs:
//
//	*ToolCall - The tool call, or nil if no action.
//
// Example:
//
//	result := ParseReAct("Action: find_entry_points\nAction Input: {\"type\": \"test\"}")
//	call := result.ToToolCall()
//	// call.Name = "find_entry_points"
//	// call.Arguments = "{\"type\": \"test\"}"
//
// Thread Safety: This method is safe for concurrent use.
func (r *ReActParseResult) ToToolCall() *ToolCall {
	if !r.HasAction() {
		return nil
	}

	// Generate unique ID for the tool call
	id := "react-" + uuid.NewString()[:8]

	// Use empty JSON if no ActionInput was provided
	args := r.ActionInput
	if args == "" {
		args = "{}"
	}

	return &ToolCall{
		ID:        id,
		Name:      r.Action,
		Arguments: args,
	}
}

// ReActInstructions returns the system prompt addition for ReAct mode.
//
// Description:
//
//	Returns the instruction text to append to the system prompt
//	when ReAct mode is enabled. This teaches the LLM the ReAct format.
//
// Outputs:
//
//	string - The ReAct instructions to append.
//
// Thread Safety: This function is safe for concurrent use.
func ReActInstructions() string {
	return `
## TOOL USAGE FORMAT

To use a tool, output EXACTLY this format:

Thought: [Your reasoning about what information you need]
Action: [tool_name]
Action Input: {"param": "value"}

Then STOP and wait for the Observation (tool result).

After receiving the Observation, you may:
- Use another tool (repeat the Thought/Action/Action Input pattern)
- Provide your final answer

When you have enough information, output:

Thought: I now have enough information to answer the question.
Final Answer: [Your complete answer with [file:line] citations]

IMPORTANT:
- Always include Thought before Action
- Action must be one of the available tool names
- Action Input must be valid JSON
- Wait for Observation before continuing
`
}

// FormatAsObservation formats a tool result as a ReAct observation.
//
// Description:
//
//	Converts a tool result into the "Observation:" format expected
//	by the ReAct pattern. This is injected into the conversation
//	after tool execution.
//
// Inputs:
//
//	toolName - The name of the tool that was executed.
//	success - Whether the tool execution succeeded.
//	output - The tool output text (or error message if failed).
//
// Outputs:
//
//	string - The formatted observation.
//
// Example:
//
//	obs := FormatAsObservation("find_entry_points", true, "Found 5 entry points...")
//	// obs = "Observation: Found 5 entry points..."
//
// Thread Safety: This function is safe for concurrent use.
func FormatAsObservation(toolName string, success bool, output string) string {
	if success {
		return "Observation: " + output
	}
	return "Observation: Error executing " + toolName + " - " + output
}
