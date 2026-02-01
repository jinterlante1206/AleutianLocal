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
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/google/uuid"
)

// ParseFormat represents the format of tool calls in LLM output.
type ParseFormat string

const (
	// FormatXML parses <tool_call><name>...</name><params>...</params></tool_call>
	FormatXML ParseFormat = "xml"

	// FormatJSON parses {"tool": "...", "params": {...}}
	FormatJSON ParseFormat = "json"

	// FormatFunctionCall parses OpenAI function calling format.
	FormatFunctionCall ParseFormat = "function_call"

	// FormatAnthropicXML parses Anthropic's function call XML format.
	FormatAnthropicXML ParseFormat = "anthropic_xml"
)

// Sentinel errors for parsing.
var (
	// ErrNoToolCalls indicates no tool calls were found in the input.
	ErrNoToolCalls = errors.New("no tool calls found")

	// ErrMalformedToolCall indicates a tool call was found but malformed.
	ErrMalformedToolCall = errors.New("malformed tool call")

	// ErrInvalidParams indicates parameters could not be parsed as JSON.
	ErrInvalidParams = errors.New("invalid parameters JSON")
)

// ToolCall represents a parsed tool invocation from LLM output.
type ToolCall struct {
	// ID is a unique identifier for this call.
	ID string `json:"id"`

	// Name is the tool name to invoke.
	Name string `json:"name"`

	// Params contains the parsed parameters.
	Params json.RawMessage `json:"params"`

	// Raw is the original text for debugging.
	Raw string `json:"raw"`
}

// ParamsMap returns the parameters as a map.
//
// Description:
//
//	Deserializes the JSON parameters into a map for easier access.
//	Returns an empty map if Params is empty or nil.
//
// Outputs:
//
//	map[string]any - Parameters as a map
//	error - Non-nil if JSON parsing fails
//
// Thread Safety: This method is safe for concurrent use.
func (tc *ToolCall) ParamsMap() (map[string]any, error) {
	if len(tc.Params) == 0 {
		return make(map[string]any), nil
	}

	var result map[string]any
	if err := json.Unmarshal(tc.Params, &result); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidParams, err)
	}
	return result, nil
}

// Parser extracts tool calls from LLM output text.
//
// Description:
//
//	Parses LLM output to extract tool calls in various formats.
//	The parser tries each enabled format in order until it finds matches.
//
// Thread Safety: Parser is safe for concurrent use.
type Parser struct {
	formats []ParseFormat
}

// NewParser creates a new tool call parser.
//
// Description:
//
//	Creates a parser that will try the specified formats in order.
//	If no formats are specified, defaults to trying all formats.
//
// Inputs:
//
//	formats - Formats to try, in order of preference
//
// Outputs:
//
//	*Parser - The configured parser
func NewParser(formats ...ParseFormat) *Parser {
	if len(formats) == 0 {
		formats = []ParseFormat{FormatAnthropicXML, FormatXML, FormatJSON, FormatFunctionCall}
	}
	return &Parser{formats: formats}
}

// Parse extracts tool calls from text.
//
// Description:
//
//	Parses the input text trying each configured format in order.
//	Returns all found tool calls, the remaining text (without tool calls),
//	and any error encountered.
//
// Inputs:
//
//	text - The LLM output text to parse
//
// Outputs:
//
//	[]ToolCall - Extracted tool calls
//	string - Remaining text after removing tool calls
//	error - Non-nil if parsing failed completely
//
// Thread Safety: This method is safe for concurrent use.
func (p *Parser) Parse(text string) ([]ToolCall, string, error) {
	if text == "" {
		return nil, "", nil
	}

	var allCalls []ToolCall
	remaining := text

	for _, format := range p.formats {
		var calls []ToolCall
		var newRemaining string
		var err error

		switch format {
		case FormatXML:
			calls, newRemaining, err = p.parseXML(remaining)
		case FormatJSON:
			calls, newRemaining, err = p.parseJSON(remaining)
		case FormatAnthropicXML:
			calls, newRemaining, err = p.parseAnthropicXML(remaining)
		case FormatFunctionCall:
			// Function calls come from structured API response, not text
			continue
		}

		if err == nil && len(calls) > 0 {
			allCalls = append(allCalls, calls...)
			remaining = newRemaining
		}
	}

	return allCalls, remaining, nil
}

// ParseFunctionCalls parses OpenAI-style function calls from a structured response.
//
// Description:
//
//	Parses function calls from an OpenAI-compatible chat completion response.
//	This handles the structured tool_calls array from the API response.
//
// Inputs:
//
//	toolCalls - Array of tool calls from API response
//
// Outputs:
//
//	[]ToolCall - Parsed tool calls
//	error - Non-nil if parsing fails
//
// Thread Safety: This method is safe for concurrent use.
func (p *Parser) ParseFunctionCalls(toolCalls []FunctionCallResponse) ([]ToolCall, error) {
	if len(toolCalls) == 0 {
		return nil, nil
	}

	result := make([]ToolCall, 0, len(toolCalls))
	for _, tc := range toolCalls {
		id := tc.ID
		if id == "" {
			id = uuid.NewString()
		}

		// Parse arguments string as JSON
		var params json.RawMessage
		if tc.Function.Arguments != "" {
			if json.Valid([]byte(tc.Function.Arguments)) {
				params = json.RawMessage(tc.Function.Arguments)
			} else {
				return nil, fmt.Errorf("%w: invalid arguments for %s", ErrInvalidParams, tc.Function.Name)
			}
		}

		result = append(result, ToolCall{
			ID:     id,
			Name:   tc.Function.Name,
			Params: params,
			Raw:    tc.Function.Arguments,
		})
	}

	return result, nil
}

// FunctionCallResponse represents an OpenAI-style function call from API response.
type FunctionCallResponse struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// XML format parsing
// Pattern: <tool_call><name>tool_name</name><params>{...}</params></tool_call>
var xmlToolCallRegex = regexp.MustCompile(`(?s)<tool_call>\s*<name>\s*([^<]+)\s*</name>\s*<params>\s*(.*?)\s*</params>\s*</tool_call>`)

func (p *Parser) parseXML(text string) ([]ToolCall, string, error) {
	matches := xmlToolCallRegex.FindAllStringSubmatchIndex(text, -1)
	if len(matches) == 0 {
		return nil, text, ErrNoToolCalls
	}

	var calls []ToolCall
	remaining := text

	// Process in reverse order to preserve indices when removing
	for i := len(matches) - 1; i >= 0; i-- {
		match := matches[i]
		fullStart, fullEnd := match[0], match[1]
		nameStart, nameEnd := match[2], match[3]
		paramsStart, paramsEnd := match[4], match[5]

		name := strings.TrimSpace(text[nameStart:nameEnd])
		paramsStr := strings.TrimSpace(text[paramsStart:paramsEnd])

		// Validate params is valid JSON
		var params json.RawMessage
		if paramsStr != "" {
			if !json.Valid([]byte(paramsStr)) {
				// Try to be lenient - maybe it's just empty
				if paramsStr == "{}" || paramsStr == "" {
					params = json.RawMessage("{}")
				} else {
					continue // Skip malformed
				}
			} else {
				params = json.RawMessage(paramsStr)
			}
		} else {
			params = json.RawMessage("{}")
		}

		calls = append([]ToolCall{{
			ID:     uuid.NewString(),
			Name:   name,
			Params: params,
			Raw:    text[fullStart:fullEnd],
		}}, calls...)

		// Remove from remaining text
		remaining = remaining[:fullStart] + remaining[fullEnd:]
	}

	return calls, strings.TrimSpace(remaining), nil
}

// JSON format parsing
// Pattern: {"tool": "name", "params": {...}}
//
// Limitation: This regex only handles single-level nested JSON objects for params.
// Deeply nested objects within params will not be captured correctly.
// For complex nested structures, use XML or function calling format instead.
var jsonToolCallRegex = regexp.MustCompile(`\{[^{}]*"tool"\s*:\s*"([^"]+)"[^{}]*"params"\s*:\s*(\{[^{}]*\})[^{}]*\}`)

func (p *Parser) parseJSON(text string) ([]ToolCall, string, error) {
	matches := jsonToolCallRegex.FindAllStringSubmatchIndex(text, -1)
	if len(matches) == 0 {
		return nil, text, ErrNoToolCalls
	}

	var calls []ToolCall
	remaining := text

	for i := len(matches) - 1; i >= 0; i-- {
		match := matches[i]
		fullStart, fullEnd := match[0], match[1]
		nameStart, nameEnd := match[2], match[3]
		paramsStart, paramsEnd := match[4], match[5]

		name := text[nameStart:nameEnd]
		paramsStr := text[paramsStart:paramsEnd]

		var params json.RawMessage
		if json.Valid([]byte(paramsStr)) {
			params = json.RawMessage(paramsStr)
		} else {
			params = json.RawMessage("{}")
		}

		calls = append([]ToolCall{{
			ID:     uuid.NewString(),
			Name:   name,
			Params: params,
			Raw:    text[fullStart:fullEnd],
		}}, calls...)

		remaining = remaining[:fullStart] + remaining[fullEnd:]
	}

	return calls, strings.TrimSpace(remaining), nil
}

// Anthropic XML format parsing
// Pattern: <function_calls><invoke name="tool_name"><parameter name="key">value</parameter>...</invoke></function_calls>
// Also supports: <function_calls><invoke name="...">...
var anthropicFunctionCallsRegex = regexp.MustCompile(`(?s)<(?:antml:)?function_calls>\s*(.*?)\s*</(?:antml:)?function_calls>`)
var anthropicInvokeRegex = regexp.MustCompile(`(?s)<(?:antml:)?invoke\s+name="([^"]+)">\s*(.*?)\s*</(?:antml:)?invoke>`)
var anthropicParamRegex = regexp.MustCompile(`(?s)<(?:antml:)?parameter\s+name="([^"]+)">\s*(.*?)\s*</(?:antml:)?parameter>`)

func (p *Parser) parseAnthropicXML(text string) ([]ToolCall, string, error) {
	blockMatches := anthropicFunctionCallsRegex.FindAllStringSubmatchIndex(text, -1)
	if len(blockMatches) == 0 {
		return nil, text, ErrNoToolCalls
	}

	var calls []ToolCall
	remaining := text

	for i := len(blockMatches) - 1; i >= 0; i-- {
		blockMatch := blockMatches[i]
		blockStart, blockEnd := blockMatch[0], blockMatch[1]
		innerStart, innerEnd := blockMatch[2], blockMatch[3]
		innerText := text[innerStart:innerEnd]

		// Find all invoke tags within this block
		invokeMatches := anthropicInvokeRegex.FindAllStringSubmatch(innerText, -1)
		for _, invokeMatch := range invokeMatches {
			name := invokeMatch[1]
			invokeBody := invokeMatch[2]

			// Parse parameters
			params := make(map[string]any)
			paramMatches := anthropicParamRegex.FindAllStringSubmatch(invokeBody, -1)
			for _, paramMatch := range paramMatches {
				paramName := paramMatch[1]
				paramValue := strings.TrimSpace(paramMatch[2])

				// Try to parse as JSON first, fall back to string
				var value any
				if err := json.Unmarshal([]byte(paramValue), &value); err != nil {
					value = paramValue
				}
				params[paramName] = value
			}

			paramsJSON, _ := json.Marshal(params)
			calls = append(calls, ToolCall{
				ID:     uuid.NewString(),
				Name:   name,
				Params: paramsJSON,
				Raw:    text[blockStart:blockEnd],
			})
		}

		remaining = remaining[:blockStart] + remaining[blockEnd:]
	}

	return calls, strings.TrimSpace(remaining), nil
}

// ExtractTextBetweenToolCalls returns text segments not part of tool calls.
//
// Description:
//
//	Useful for getting the LLM's explanatory text separate from tool calls.
//
// Inputs:
//
//	text - The full LLM output
//
// Outputs:
//
//	string - Text with all tool call markup removed
func (p *Parser) ExtractTextBetweenToolCalls(text string) string {
	_, remaining, _ := p.Parse(text)
	return remaining
}
