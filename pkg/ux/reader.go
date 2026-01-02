// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.

// Package ux provides user experience components for the Aleutian CLI.
//
// This file contains stream readers that consume io.Reader sources
// and emit parsed events via callbacks.
//
// Single Responsibility:
//
//	Readers handle I/O and event sequencing. They use parsers to convert
//	bytes to events, but do not render output. This separation enables
//	flexible composition with different renderers.
//
// Context Support:
//
//	All readers accept context.Context for cancellation and timeout.
//	When context is cancelled, reading stops and the error is returned.
package ux

import (
	"bufio"
	"context"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"
)

// =============================================================================
// Stream Reader Interface
// =============================================================================

// StreamReader reads streaming responses and invokes callbacks.
//
// This interface abstracts the reading of streaming LLM responses.
// Implementations handle the specific wire format (SSE, JSONL, etc.)
// and emit parsed StreamEvent structs.
//
// Thread Safety:
//
//	StreamReader implementations must be safe for concurrent use.
//	However, a single Read/ReadAll operation should not be called
//	concurrently on the same reader instance.
//
// Example:
//
//	reader := NewSSEStreamReader(NewSSEParser())
//
//	err := reader.Read(ctx, httpResp.Body, func(event StreamEvent) error {
//	    switch event.Type {
//	    case StreamEventToken:
//	        fmt.Print(event.Content)
//	    case StreamEventError:
//	        return errors.New(event.Error)
//	    }
//	    return nil
//	})
type StreamReader interface {
	// Read processes a stream, invoking callback for each event.
	//
	// Parameters:
	//   - ctx: Context for cancellation. When cancelled, stops reading.
	//   - r: The source to read from. Caller is responsible for closing.
	//   - callback: Invoked for each parsed event. Return error to stop.
	//
	// Returns:
	//   - error: nil on successful completion, otherwise the error that
	//     stopped reading (context cancellation, parse error, or callback error)
	//
	// The stream is considered complete when:
	//   - EOF is reached
	//   - A terminal event (done/error) is received
	//   - Context is cancelled
	//   - Callback returns an error
	Read(ctx context.Context, r io.Reader, callback StreamCallback) error

	// ReadAll reads the entire stream and returns aggregated result.
	//
	// This is a convenience method that collects all events into a
	// StreamResult. Use Read() when you need real-time event processing.
	//
	// Parameters:
	//   - ctx: Context for cancellation.
	//   - r: The source to read from. Caller is responsible for closing.
	//
	// Returns:
	//   - *StreamResult: Aggregated result with answer, sources, etc.
	//   - error: nil on success, otherwise the error that stopped reading.
	//
	// Note: If the stream ends with an error event, the error is captured
	// in StreamResult.Error and this method returns nil (not an error).
	ReadAll(ctx context.Context, r io.Reader) (*StreamResult, error)
}

// =============================================================================
// SSE Stream Reader
// =============================================================================

// sseStreamReader implements StreamReader for Server-Sent Events.
//
// This reader uses bufio.Scanner to read lines and an SSEParser to
// parse each line into events.
type sseStreamReader struct {
	parser SSEParser
}

// NewSSEStreamReader creates a new SSE stream reader.
//
// Parameters:
//   - parser: The SSE parser to use for line parsing.
//
// Returns a StreamReader that handles SSE format.
//
// Example:
//
//	reader := NewSSEStreamReader(NewSSEParser())
func NewSSEStreamReader(parser SSEParser) StreamReader {
	return &sseStreamReader{
		parser: parser,
	}
}

// Read processes an SSE stream, invoking callback for each event.
//
// Lines are read using bufio.Scanner. Each line is parsed by the
// SSE parser. Nil events (empty lines, comments) are skipped.
func (r *sseStreamReader) Read(ctx context.Context, reader io.Reader, callback StreamCallback) error {
	scanner := bufio.NewScanner(reader)
	eventIndex := 0

	for scanner.Scan() {
		// Check for context cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		line := scanner.Text()

		// Parse the line
		event, err := r.parser.ParseLine(line)
		if err != nil {
			return err
		}

		// Skip nil events (empty lines, comments)
		if event == nil {
			continue
		}

		// Set the event index
		event.Index = eventIndex
		eventIndex++

		// Invoke the callback
		if err := callback(*event); err != nil {
			return err
		}

		// Stop on terminal events
		if event.IsTerminal() {
			return nil
		}
	}

	if err := scanner.Err(); err != nil {
		return err
	}

	return nil
}

// ReadAll reads the entire stream and returns aggregated result.
//
// Collects all tokens into Answer, all thinking into Thinking,
// captures sources, session ID, and error.
func (r *sseStreamReader) ReadAll(ctx context.Context, reader io.Reader) (*StreamResult, error) {
	result := &StreamResult{
		Id:        uuid.New().String(),
		CreatedAt: time.Now().UnixMilli(),
	}

	var answerBuilder strings.Builder
	var thinkingBuilder strings.Builder

	err := r.Read(ctx, reader, func(event StreamEvent) error {
		result.TotalEvents++

		switch event.Type {
		case StreamEventToken:
			if result.FirstTokenAt == 0 {
				result.FirstTokenAt = time.Now().UnixMilli()
			}
			answerBuilder.WriteString(event.Content)
			result.TotalTokens++

		case StreamEventThinking:
			thinkingBuilder.WriteString(event.Content)
			result.ThinkingTokens++

		case StreamEventSources:
			result.Sources = append(result.Sources, event.Sources...)

		case StreamEventDone:
			result.SessionID = event.SessionID
			result.CompletedAt = time.Now().UnixMilli()

		case StreamEventError:
			result.Error = event.Error
			result.CompletedAt = time.Now().UnixMilli()
		}

		// Propagate request ID if present
		if event.RequestID != "" && result.RequestID == "" {
			result.RequestID = event.RequestID
		}

		return nil
	})

	result.Answer = answerBuilder.String()
	result.Thinking = thinkingBuilder.String()

	// Ensure CompletedAt is set even if no terminal event
	if result.CompletedAt == 0 {
		result.CompletedAt = time.Now().UnixMilli()
	}

	return result, err
}

// =============================================================================
// Compile-time Interface Check
// =============================================================================

var _ StreamReader = (*sseStreamReader)(nil)
