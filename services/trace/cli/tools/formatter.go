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
	"encoding/json"
	"fmt"
	"html"
	"strings"
)

// DefaultMaxOutputLen is the default maximum output length before truncation.
const DefaultMaxOutputLen = 30000

// DefaultTruncateMsg is the message appended when output is truncated.
const DefaultTruncateMsg = "\n... [output truncated]"

// Formatter formats tool execution results for LLM consumption.
//
// Description:
//
//	Converts ExecutionResults into a format suitable for including
//	in LLM context. Handles truncation of large outputs.
//
// Thread Safety: Formatter is safe for concurrent use.
type Formatter struct {
	maxOutputLen int
	truncateMsg  string
}

// FormatterOption configures the Formatter.
type FormatterOption func(*Formatter)

// WithMaxOutputLen sets the maximum output length before truncation.
func WithMaxOutputLen(n int) FormatterOption {
	return func(f *Formatter) {
		f.maxOutputLen = n
	}
}

// WithTruncateMessage sets the truncation indicator message.
func WithTruncateMessage(msg string) FormatterOption {
	return func(f *Formatter) {
		f.truncateMsg = msg
	}
}

// NewFormatter creates a new result formatter.
//
// Inputs:
//
//	opts - Configuration options
//
// Outputs:
//
//	*Formatter - The configured formatter
func NewFormatter(opts ...FormatterOption) *Formatter {
	f := &Formatter{
		maxOutputLen: DefaultMaxOutputLen,
		truncateMsg:  DefaultTruncateMsg,
	}
	for _, opt := range opts {
		opt(f)
	}
	return f
}

// ExecutionResult represents the outcome of a single tool execution.
// This is used by the dispatcher for tracking execution details.
type ExecutionResult struct {
	// Call is the original tool call.
	Call ToolCall `json:"call"`

	// Result is the execution result.
	Result *Result `json:"result"`

	// Approved indicates if the tool required and received approval.
	Approved bool `json:"approved,omitempty"`

	// Declined indicates if the user declined approval for the tool.
	Declined bool `json:"declined,omitempty"`
}

// Format formats multiple execution results for LLM consumption.
//
// Description:
//
//	Formats all results into XML blocks suitable for including in
//	the LLM's context window. Each result is formatted individually
//	and joined with newlines.
//
// Inputs:
//
//	results - The execution results to format
//
// Outputs:
//
//	string - Formatted results as XML
//
// Thread Safety: This method is safe for concurrent use.
func (f *Formatter) Format(results []ExecutionResult) string {
	if len(results) == 0 {
		return ""
	}

	var sb strings.Builder
	for i, result := range results {
		if i > 0 {
			sb.WriteString("\n\n")
		}
		sb.WriteString(f.FormatSingle(result))
	}
	return sb.String()
}

// FormatSingle formats a single execution result for LLM consumption.
//
// Description:
//
//	Formats a single result into an XML block. The format is:
//	<tool_result>
//	<name>tool_name</name>
//	<success>true/false</success>
//	<output>...</output>
//	</tool_result>
//
// Inputs:
//
//	result - The execution result to format
//
// Outputs:
//
//	string - Formatted result as XML
//
// Thread Safety: This method is safe for concurrent use.
func (f *Formatter) FormatSingle(result ExecutionResult) string {
	var sb strings.Builder

	sb.WriteString("<tool_result>\n")
	sb.WriteString(fmt.Sprintf("<name>%s</name>\n", html.EscapeString(result.Call.Name)))

	if result.Result != nil {
		sb.WriteString(fmt.Sprintf("<success>%t</success>\n", result.Result.Success))

		if result.Result.Success {
			output := f.formatOutput(result.Result)
			truncated := f.Truncate(output)
			sb.WriteString("<output>\n")
			sb.WriteString(html.EscapeString(truncated))
			sb.WriteString("\n</output>\n")
		} else {
			sb.WriteString(fmt.Sprintf("<error>%s</error>\n", html.EscapeString(result.Result.Error)))
		}

		if result.Result.Truncated {
			sb.WriteString("<truncated>true</truncated>\n")
		}
	} else {
		sb.WriteString("<success>false</success>\n")
		sb.WriteString("<error>no result returned</error>\n")
	}

	sb.WriteString("</tool_result>")
	return sb.String()
}

// formatOutput extracts the best output representation from a Result.
//
// Description:
//
//	Attempts to get a string representation of the result output.
//	Prefers OutputText if available, falls back to JSON marshaling.
//
// Inputs:
//
//	result - The result to format. Must not be nil.
//
// Outputs:
//
//	string - The formatted output, or error description if marshaling fails
func (f *Formatter) formatOutput(result *Result) string {
	// Prefer OutputText if available
	if result.OutputText != "" {
		return result.OutputText
	}

	// Fall back to JSON marshaling of Output
	if result.Output != nil {
		data, err := json.MarshalIndent(result.Output, "", "  ")
		if err != nil {
			// Return error info rather than silently swallowing
			return fmt.Sprintf("[output could not be serialized: %v]", err)
		}
		return string(data)
	}

	return ""
}

// Truncate truncates output to the maximum length.
//
// Description:
//
//	If the output exceeds maxOutputLen, truncates it and appends
//	the truncation message.
//
// Inputs:
//
//	output - The output to potentially truncate
//
// Outputs:
//
//	string - The (possibly truncated) output
//
// Thread Safety: This method is safe for concurrent use.
func (f *Formatter) Truncate(output string) string {
	if len(output) <= f.maxOutputLen {
		return output
	}

	// Truncate at a word boundary if possible
	cutoff := f.maxOutputLen - len(f.truncateMsg)
	if cutoff < 0 {
		cutoff = f.maxOutputLen / 2
	}

	// Try to find a newline near the cutoff to make a clean break
	lastNewline := strings.LastIndex(output[:cutoff], "\n")
	if lastNewline > cutoff*3/4 {
		cutoff = lastNewline
	}

	return output[:cutoff] + f.truncateMsg
}

// FormatError formats an error for LLM consumption.
//
// Description:
//
//	Formats an error that occurred during tool execution into
//	a consistent XML format.
//
// Inputs:
//
//	toolName - The name of the tool that failed
//	err - The error that occurred
//
// Outputs:
//
//	string - Formatted error as XML
//
// Thread Safety: This method is safe for concurrent use.
func (f *Formatter) FormatError(toolName string, err error) string {
	return fmt.Sprintf(`<tool_result>
<name>%s</name>
<success>false</success>
<error>%s</error>
</tool_result>`, html.EscapeString(toolName), html.EscapeString(err.Error()))
}

// FormatToolList formats a list of available tools for the LLM system prompt.
//
// Description:
//
//	Generates a human-readable description of available tools
//	for inclusion in the system prompt.
//
// Inputs:
//
//	definitions - The tool definitions to format
//
// Outputs:
//
//	string - Formatted tool list
//
// Thread Safety: This method is safe for concurrent use.
func (f *Formatter) FormatToolList(definitions []ToolDefinition) string {
	if len(definitions) == 0 {
		return "No tools available."
	}

	var sb strings.Builder
	sb.WriteString("You have access to the following tools:\n\n")

	for _, def := range definitions {
		sb.WriteString(fmt.Sprintf("## %s\n", def.Name))
		sb.WriteString(fmt.Sprintf("%s\n\n", def.Description))

		if len(def.Parameters) > 0 {
			sb.WriteString("**Parameters:**\n")
			for name, param := range def.Parameters {
				required := ""
				if param.Required {
					required = " (required)"
				}
				sb.WriteString(fmt.Sprintf("- `%s` (%s)%s: %s\n", name, param.Type, required, param.Description))
			}
			sb.WriteString("\n")
		}

		if len(def.Examples) > 0 {
			sb.WriteString("**Example:**\n")
			for _, ex := range def.Examples {
				params, _ := json.Marshal(ex.Parameters)
				sb.WriteString(fmt.Sprintf("```\n<tool_call>\n<name>%s</name>\n<params>%s</params>\n</tool_call>\n```\n", def.Name, string(params)))
				break // Just show first example
			}
		}

		sb.WriteString("\n")
	}

	sb.WriteString("---\n\n")
	sb.WriteString("To use a tool, output:\n")
	sb.WriteString("```\n<tool_call>\n<name>tool_name</name>\n<params>{\"key\": \"value\"}</params>\n</tool_call>\n```\n")

	return sb.String()
}

// FormatCompact formats results in a more compact format.
//
// Description:
//
//	Produces a more compact representation when context space is limited.
//	Shows only tool name, success status, and first line of output.
//
// Inputs:
//
//	results - The execution results to format
//
// Outputs:
//
//	string - Compact formatted results
//
// Thread Safety: This method is safe for concurrent use.
func (f *Formatter) FormatCompact(results []ExecutionResult) string {
	if len(results) == 0 {
		return ""
	}

	var sb strings.Builder
	for _, result := range results {
		status := "OK"
		detail := ""

		if result.Result != nil {
			if !result.Result.Success {
				status = "FAIL"
				detail = result.Result.Error
			} else {
				// Get first line of output
				output := f.formatOutput(result.Result)
				if idx := strings.Index(output, "\n"); idx > 0 {
					detail = output[:idx]
				} else if len(output) > 80 {
					detail = output[:77] + "..."
				} else {
					detail = output
				}
			}
		} else {
			status = "FAIL"
			detail = "no result"
		}

		sb.WriteString(fmt.Sprintf("[%s] %s: %s\n", status, result.Call.Name, detail))
	}

	return strings.TrimSpace(sb.String())
}
