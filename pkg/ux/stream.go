// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.

package ux

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

// StreamEventType represents the type of streaming event
type StreamEventType string

const (
	StreamEventStatus  StreamEventType = "status"
	StreamEventToken   StreamEventType = "token"
	StreamEventSources StreamEventType = "sources"
	StreamEventDone    StreamEventType = "done"
	StreamEventError   StreamEventType = "error"
)

// StreamEvent represents a single streaming event from the server
type StreamEvent struct {
	Type      StreamEventType `json:"type"`
	Content   string          `json:"content,omitempty"`
	Message   string          `json:"message,omitempty"`
	Sources   []SourceInfo    `json:"sources,omitempty"`
	SessionID string          `json:"session_id,omitempty"`
	Error     string          `json:"error,omitempty"`
}

// StreamResult contains the complete result of processing a stream
type StreamResult struct {
	Answer    string
	Sources   []SourceInfo
	SessionID string
}

// StreamProcessor defines the interface for processing streaming responses.
type StreamProcessor interface {
	// Process reads and processes a streaming response from the reader.
	// Returns the complete answer, sources, session ID, and any error.
	Process(reader io.Reader) (*StreamResult, error)
}

// sseStreamProcessor implements StreamProcessor for Server-Sent Events
type sseStreamProcessor struct {
	writer      io.Writer
	personality PersonalityLevel
	spinner     *Spinner
	answer      strings.Builder
	sources     []SourceInfo
	sessionID   string
}

// NewStreamProcessor creates a new SSE stream processor
func NewStreamProcessor() StreamProcessor {
	return &sseStreamProcessor{
		writer:      os.Stdout,
		personality: GetPersonality().Level,
	}
}

// NewStreamProcessorWithWriter creates a stream processor with custom writer (for testing)
func NewStreamProcessorWithWriter(w io.Writer, personality PersonalityLevel) StreamProcessor {
	return &sseStreamProcessor{
		writer:      w,
		personality: personality,
	}
}

// Process reads and processes a streaming response
func (p *sseStreamProcessor) Process(reader io.Reader) (*StreamResult, error) {
	scanner := bufio.NewScanner(reader)

	for scanner.Scan() {
		line := scanner.Text()

		// Skip empty lines
		if strings.TrimSpace(line) == "" {
			continue
		}

		// Parse SSE format: "data: {...}"
		if strings.HasPrefix(line, "data: ") {
			line = strings.TrimPrefix(line, "data: ")
		}

		var event StreamEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			// If it's not JSON, treat it as a token
			p.handleToken(line)
			continue
		}

		switch event.Type {
		case StreamEventStatus:
			p.handleStatus(event.Message)
		case StreamEventToken:
			p.handleToken(event.Content)
		case StreamEventSources:
			p.sources = event.Sources
		case StreamEventDone:
			p.sessionID = event.SessionID
			p.finalize()
			return &StreamResult{
				Answer:    p.answer.String(),
				Sources:   p.sources,
				SessionID: p.sessionID,
			}, nil
		case StreamEventError:
			p.finalize()
			return nil, fmt.Errorf("%s", event.Error)
		}
	}

	if err := scanner.Err(); err != nil {
		p.finalize()
		return nil, err
	}

	// Stream ended without explicit done event
	p.finalize()
	return &StreamResult{
		Answer:    p.answer.String(),
		Sources:   p.sources,
		SessionID: p.sessionID,
	}, nil
}

func (p *sseStreamProcessor) handleStatus(message string) {
	if p.personality == PersonalityMachine {
		fmt.Fprintf(p.writer, "STATUS: %s\n", message)
		return
	}

	// Start or update spinner
	if p.spinner == nil {
		p.spinner = NewSpinner(message)
		p.spinner.Start()
	} else {
		p.spinner.UpdateMessage(message)
	}
}

func (p *sseStreamProcessor) handleToken(token string) {
	// Stop spinner when first token arrives
	if p.spinner != nil {
		p.spinner.Stop()
		p.spinner = nil
		if p.personality != PersonalityMachine {
			fmt.Fprintln(p.writer) // New line after spinner
		}
	}

	p.answer.WriteString(token)

	if p.personality == PersonalityMachine {
		// In machine mode, buffer until done
		return
	}

	// Print token immediately for streaming effect
	fmt.Fprint(p.writer, token)
}

func (p *sseStreamProcessor) finalize() {
	// Stop spinner if still running
	if p.spinner != nil {
		p.spinner.Stop()
		p.spinner = nil
	}

	if p.personality == PersonalityMachine {
		// Print buffered answer
		if p.answer.Len() > 0 {
			fmt.Fprintf(p.writer, "ANSWER: %s\n", p.answer.String())
		}
	} else {
		// Ensure we end with a newline
		if p.answer.Len() > 0 && !strings.HasSuffix(p.answer.String(), "\n") {
			fmt.Fprintln(p.writer)
		}
	}
}

// SimpleStreamReader defines interface for simple non-SSE streaming
type SimpleStreamReader interface {
	// Read processes a simple stream where tokens arrive as plain text
	Read(reader io.Reader) (string, error)
}

// plainStreamReader implements SimpleStreamReader
type plainStreamReader struct {
	writer      io.Writer
	personality PersonalityLevel
}

// NewSimpleStreamReader creates a new plain text stream reader
func NewSimpleStreamReader() SimpleStreamReader {
	return &plainStreamReader{
		writer:      os.Stdout,
		personality: GetPersonality().Level,
	}
}

// NewSimpleStreamReaderWithWriter creates a stream reader with custom writer (for testing)
func NewSimpleStreamReaderWithWriter(w io.Writer, personality PersonalityLevel) SimpleStreamReader {
	return &plainStreamReader{
		writer:      w,
		personality: personality,
	}
}

// Read processes a simple stream
func (r *plainStreamReader) Read(reader io.Reader) (string, error) {
	var answer strings.Builder

	buf := make([]byte, 256)
	for {
		n, err := reader.Read(buf)
		if n > 0 {
			chunk := string(buf[:n])
			answer.WriteString(chunk)

			if r.personality != PersonalityMachine {
				fmt.Fprint(r.writer, chunk)
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return answer.String(), err
		}
	}

	if r.personality == PersonalityMachine && answer.Len() > 0 {
		fmt.Fprintf(r.writer, "ANSWER: %s\n", answer.String())
	} else if !strings.HasSuffix(answer.String(), "\n") {
		fmt.Fprintln(r.writer)
	}

	return answer.String(), nil
}

// Convenience function for backward compatibility

// SimpleStreamPrint handles simple non-SSE streaming where tokens arrive as plain text
func SimpleStreamPrint(reader io.Reader) (string, error) {
	return NewSimpleStreamReader().Read(reader)
}
