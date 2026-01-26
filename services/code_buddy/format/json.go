// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package format

import (
	"encoding/json"
	"io"
)

// JSONFormatter formats results as full JSON.
type JSONFormatter struct {
	indent bool
}

// NewJSONFormatter creates a new JSON formatter.
func NewJSONFormatter() *JSONFormatter {
	return &JSONFormatter{indent: true}
}

// NewJSONFormatterCompact creates a JSON formatter without indentation.
func NewJSONFormatterCompact() *JSONFormatter {
	return &JSONFormatter{indent: false}
}

// Format converts the result to JSON string.
func (f *JSONFormatter) Format(result interface{}) (string, error) {
	var data []byte
	var err error

	if f.indent {
		data, err = json.MarshalIndent(result, "", "  ")
	} else {
		data, err = json.Marshal(result)
	}

	if err != nil {
		return "", err
	}

	return string(data), nil
}

// Name returns the format name.
func (f *JSONFormatter) Name() FormatType {
	return FormatJSON
}

// TokenEstimate estimates the number of tokens.
func (f *JSONFormatter) TokenEstimate(result interface{}, tokenizer ...string) int {
	output, err := f.Format(result)
	if err != nil {
		return 0
	}

	ratio := TokenRatio["default"]
	if len(tokenizer) > 0 {
		if r, ok := TokenRatio[tokenizer[0]]; ok {
			ratio = r
		}
	}

	return int(float64(len(output)) / ratio)
}

// IsReversible returns true - JSON has full fidelity.
func (f *JSONFormatter) IsReversible() bool {
	return true
}

// SupportsStreaming returns true - JSON can be streamed.
func (f *JSONFormatter) SupportsStreaming() bool {
	return true
}

// FormatStreaming writes JSON to a writer.
func (f *JSONFormatter) FormatStreaming(result interface{}, w io.Writer) error {
	encoder := json.NewEncoder(w)
	if f.indent {
		encoder.SetIndent("", "  ")
	}
	return encoder.Encode(result)
}
