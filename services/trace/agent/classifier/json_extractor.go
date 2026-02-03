// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.

package classifier

import (
	"encoding/json"
	"errors"
	"strings"
)

// ExtractJSON extracts JSON from potentially wrapped LLM responses.
//
// Description:
//
//	Attempts to extract valid JSON from LLM responses that may be wrapped
//	in various formats. Handles these cases in order:
//	1. Clean JSON: {"is_analytical": true, ...}
//	2. Markdown wrapped: ```json\n{"is_analytical": true}\n```
//	3. Generic code block: ```\n{...}\n```
//	4. With preamble: "Here is the classification:\n{...}"
//	5. Multiple JSON objects: Takes first valid one
//
// Inputs:
//
//	response - Raw LLM response text.
//
// Outputs:
//
//	[]byte - Extracted JSON bytes.
//	error - If no valid JSON found.
//
// Example:
//
//	jsonBytes, err := ExtractJSON("```json\n{\"is_analytical\":true}\n```")
//	if err != nil {
//	    return err
//	}
//	var result ClassificationResult
//	json.Unmarshal(jsonBytes, &result)
//
// Thread Safety: This function is safe for concurrent use.
func ExtractJSON(response string) ([]byte, error) {
	// Try direct parse first
	response = strings.TrimSpace(response)
	if response == "" {
		return nil, errors.New("empty response")
	}

	if json.Valid([]byte(response)) {
		return []byte(response), nil
	}

	// Try extracting from markdown code block with json language
	if idx := strings.Index(response, "```json"); idx >= 0 {
		start := idx + 7
		// Skip any whitespace/newlines after ```json
		for start < len(response) && (response[start] == '\n' || response[start] == '\r' || response[start] == ' ') {
			start++
		}
		end := strings.Index(response[start:], "```")
		if end > 0 {
			extracted := strings.TrimSpace(response[start : start+end])
			if json.Valid([]byte(extracted)) {
				return []byte(extracted), nil
			}
		}
	}

	// Try extracting from generic code block
	if idx := strings.Index(response, "```"); idx >= 0 {
		start := idx + 3
		// Skip language identifier if present (e.g., "json", "JSON")
		if newline := strings.Index(response[start:], "\n"); newline >= 0 && newline < 20 {
			start += newline + 1
		}
		end := strings.Index(response[start:], "```")
		if end > 0 {
			extracted := strings.TrimSpace(response[start : start+end])
			if json.Valid([]byte(extracted)) {
				return []byte(extracted), nil
			}
		}
	}

	// Try finding JSON object in text (handles preamble text)
	if start := strings.Index(response, "{"); start >= 0 {
		// Find matching closing brace
		depth := 0
		inString := false
		escaped := false

		for i := start; i < len(response); i++ {
			c := response[i]

			if escaped {
				escaped = false
				continue
			}

			if c == '\\' && inString {
				escaped = true
				continue
			}

			if c == '"' {
				inString = !inString
				continue
			}

			if inString {
				continue
			}

			switch c {
			case '{':
				depth++
			case '}':
				depth--
				if depth == 0 {
					extracted := response[start : i+1]
					if json.Valid([]byte(extracted)) {
						return []byte(extracted), nil
					}
					// If this JSON was invalid, continue looking for next object
					break
				}
			}
		}
	}

	return nil, errors.New("no valid JSON found in response")
}

// ExtractJSONWithFallback tries to extract JSON and returns a default on failure.
//
// Description:
//
//	Convenience wrapper around ExtractJSON that returns a default
//	ClassificationResult (non-analytical) on extraction failure.
//
// Inputs:
//
//	response - Raw LLM response text.
//
// Outputs:
//
//	*ClassificationResult - Extracted result or non-analytical default.
//	bool - True if extraction succeeded, false if using fallback.
//
// Thread Safety: This function is safe for concurrent use.
func ExtractJSONWithFallback(response string) (*ClassificationResult, bool) {
	jsonBytes, err := ExtractJSON(response)
	if err != nil {
		return &ClassificationResult{
			IsAnalytical: false,
			Reasoning:    "JSON extraction failed: " + err.Error(),
		}, false
	}

	var result ClassificationResult
	if err := json.Unmarshal(jsonBytes, &result); err != nil {
		return &ClassificationResult{
			IsAnalytical: false,
			Reasoning:    "JSON parsing failed: " + err.Error(),
		}, false
	}

	return &result, true
}

// ParseClassificationResponse parses an LLM response into a ClassificationResult.
//
// Description:
//
//	Complete parsing pipeline that extracts JSON from the response and
//	unmarshals it into a ClassificationResult. Handles all common response
//	formats from LLMs.
//
// Inputs:
//
//	response - Raw LLM response text.
//
// Outputs:
//
//	*ClassificationResult - Parsed result.
//	error - If extraction or parsing failed.
//
// Thread Safety: This function is safe for concurrent use.
func ParseClassificationResponse(response string) (*ClassificationResult, error) {
	jsonBytes, err := ExtractJSON(response)
	if err != nil {
		return nil, err
	}

	var result ClassificationResult
	if err := json.Unmarshal(jsonBytes, &result); err != nil {
		return nil, err
	}

	return &result, nil
}
