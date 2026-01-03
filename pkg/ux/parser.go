// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.

// Package ux provides user experience components for the Aleutian CLI.
//
// This file contains parsers for streaming response formats.
// Parsers are responsible for converting raw bytes/lines into StreamEvent structs.
//
// Single Responsibility:
//
//	Parsers ONLY parse. They do not perform I/O, rendering, or state management.
//	This separation enables easy testing and format extensibility.
//
// Supported Formats:
//
//   - SSE (Server-Sent Events): Standard format for HTTP streaming
//   - Future: JSONL, NDJSON
package ux

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"
)

// =============================================================================
// SSE Parser Interface
// =============================================================================

// SSEParser parses Server-Sent Events format into StreamEvent structs.
//
// SSE Format Reference (https://developer.mozilla.org/en-US/docs/Web/API/Server-sent_events):
//
//	data: {"type":"token","content":"Hello"}\n
//	\n
//	data: {"type":"token","content":" world"}\n
//	\n
//
// Each line starting with "data: " contains a JSON payload.
// Empty lines are event delimiters (ignored by this parser).
// Lines starting with ":" are comments (ignored).
//
// Thread Safety:
//
//	SSEParser implementations must be safe for concurrent use.
//	The default implementation is stateless and inherently thread-safe.
//
// Example:
//
//	parser := NewSSEParser()
//	event, err := parser.ParseLine(`data: {"type":"token","content":"Hi"}`)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	if event != nil {
//	    fmt.Println(event.Content) // "Hi"
//	}
type SSEParser interface {
	// ParseLine parses a single line of SSE input.
	//
	// Parameters:
	//   - line: A single line from the SSE stream (without trailing newline)
	//
	// Returns:
	//   - *StreamEvent: The parsed event, or nil for empty/comment lines
	//   - error: Non-nil if JSON parsing failed
	//
	// Line handling:
	//   - Empty lines: Returns nil, nil (event delimiter)
	//   - Comment lines (":"): Returns nil, nil (ignored)
	//   - Data lines ("data: "): Parses JSON payload
	//   - Other lines: Treated as raw token content
	ParseLine(line string) (*StreamEvent, error)

	// ParseRawJSON parses a raw JSON payload into a StreamEvent.
	//
	// Use this when you have JSON without the "data: " prefix.
	// Automatically generates Id and sets CreatedAt.
	//
	// Parameters:
	//   - jsonData: Raw JSON bytes
	//
	// Returns:
	//   - *StreamEvent: The parsed event
	//   - error: Non-nil if JSON parsing failed
	ParseRawJSON(jsonData []byte) (*StreamEvent, error)
}

// =============================================================================
// SSE Parser Implementation
// =============================================================================

// sseParser implements SSEParser for Server-Sent Events format.
//
// This implementation is stateless and safe for concurrent use.
// All parsed events are assigned fresh Id and CreatedAt values.
type sseParser struct{}

// NewSSEParser creates a new SSE parser.
//
// The returned parser is stateless and can be safely shared across goroutines.
//
// Example:
//
//	parser := NewSSEParser()
//	event, _ := parser.ParseLine(`data: {"type":"done","session_id":"sess-123"}`)
func NewSSEParser() SSEParser {
	return &sseParser{}
}

// ParseLine parses a single SSE line.
//
// Handles the following line types:
//   - Empty: Returns nil (event boundary)
//   - Comment (starts with ":"): Returns nil (ignored)
//   - Data (starts with "data: "): Parses JSON after prefix
//   - Other: Treats entire line as token content
func (p *sseParser) ParseLine(line string) (*StreamEvent, error) {
	// Trim whitespace
	line = strings.TrimSpace(line)

	// Empty lines are event delimiters
	if line == "" {
		return nil, nil
	}

	// Comments start with ":"
	if strings.HasPrefix(line, ":") {
		return nil, nil
	}

	// Data lines start with "data: "
	if strings.HasPrefix(line, "data: ") {
		jsonData := strings.TrimPrefix(line, "data: ")
		return p.ParseRawJSON([]byte(jsonData))
	}

	// Also handle "data:" without space (some servers do this)
	if strings.HasPrefix(line, "data:") {
		jsonData := strings.TrimPrefix(line, "data:")
		return p.ParseRawJSON([]byte(jsonData))
	}

	// Non-JSON line - treat as raw token
	// This handles servers that send plain text tokens
	return &StreamEvent{
		Id:        uuid.New().String(),
		CreatedAt: time.Now().UnixMilli(),
		Type:      StreamEventToken,
		Content:   line,
	}, nil
}

// ParseRawJSON parses a JSON payload into a StreamEvent.
//
// The JSON should have a "type" field indicating the event type.
// Missing fields are handled gracefully with zero values.
//
// Example JSON:
//
//	{"type":"token","content":"Hello"}
//	{"type":"sources","sources":[{"source":"doc.pdf","score":0.9}]}
//	{"type":"done","session_id":"sess-123"}
func (p *sseParser) ParseRawJSON(jsonData []byte) (*StreamEvent, error) {
	// Parse into a temporary struct that matches server format
	var raw struct {
		Type      string       `json:"type"`
		Content   string       `json:"content"`
		Message   string       `json:"message"`
		Sources   []SourceInfo `json:"sources"`
		SessionID string       `json:"session_id"`
		Error     string       `json:"error"`
		RequestID string       `json:"request_id"`
	}

	if err := json.Unmarshal(jsonData, &raw); err != nil {
		return nil, err
	}

	// Build the event with generated Id and timestamp
	event := &StreamEvent{
		Id:        uuid.New().String(),
		CreatedAt: time.Now().UnixMilli(),
		Type:      StreamEventType(raw.Type),
		Content:   raw.Content,
		Message:   raw.Message,
		Sources:   raw.Sources,
		SessionID: raw.SessionID,
		Error:     raw.Error,
		RequestID: raw.RequestID,
	}

	return event, nil
}

// =============================================================================
// Compile-time Interface Check
// =============================================================================

var _ SSEParser = (*sseParser)(nil)
