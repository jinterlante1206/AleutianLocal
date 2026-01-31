// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

// JSONRPCVersion is the JSON-RPC version used by LSP.
const JSONRPCVersion = "2.0"

// =============================================================================
// JSON-RPC MESSAGE TYPES
// =============================================================================

// Request represents a JSON-RPC request.
type Request struct {
	// JSONRPC is the protocol version, always "2.0".
	JSONRPC string `json:"jsonrpc"`

	// ID is the request identifier. Omit for notifications.
	ID int64 `json:"id,omitempty"`

	// Method is the method to invoke.
	Method string `json:"method"`

	// Params contains the method parameters.
	Params interface{} `json:"params,omitempty"`
}

// Response represents a JSON-RPC response.
type Response struct {
	// JSONRPC is the protocol version, always "2.0".
	JSONRPC string `json:"jsonrpc"`

	// ID is the request identifier this response corresponds to.
	ID int64 `json:"id"`

	// Result contains the method result (mutually exclusive with Error).
	Result json.RawMessage `json:"result,omitempty"`

	// Error contains error information (mutually exclusive with Result).
	Error *ResponseError `json:"error,omitempty"`
}

// ResponseError represents a JSON-RPC error.
type ResponseError struct {
	// Code is the error code.
	Code int `json:"code"`

	// Message is a short description of the error.
	Message string `json:"message"`

	// Data contains additional error information.
	Data interface{} `json:"data,omitempty"`
}

// Notification represents a JSON-RPC notification (no ID, no response).
type Notification struct {
	// JSONRPC is the protocol version, always "2.0".
	JSONRPC string `json:"jsonrpc"`

	// Method is the method to invoke.
	Method string `json:"method"`

	// Params contains the method parameters.
	Params interface{} `json:"params,omitempty"`
}

// =============================================================================
// PROTOCOL HANDLER
// =============================================================================

// Protocol handles JSON-RPC communication over stdin/stdout.
//
// Description:
//
//	Implements the LSP base protocol using Content-Length headers.
//	Manages request/response correlation and supports notifications.
//
// Thread Safety:
//
//	Safe for concurrent use. Multiple goroutines can send requests
//	and notifications simultaneously.
type Protocol struct {
	reader    *bufio.Reader
	writer    io.Writer
	writeMu   sync.Mutex
	nextID    int64
	pending   map[int64]chan Response
	pendingMu sync.Mutex
	closed    int32 // atomic: 1 if closed
}

// NewProtocol creates a new protocol handler.
//
// Description:
//
//	Creates a protocol handler that communicates over the provided
//	reader (server stdout) and writer (server stdin).
//
// Inputs:
//
//	r - Reader for server responses (e.g., stdout pipe)
//	w - Writer for client requests (e.g., stdin pipe)
//
// Outputs:
//
//	*Protocol - The protocol handler
func NewProtocol(r io.Reader, w io.Writer) *Protocol {
	var reader *bufio.Reader
	if r != nil {
		reader = bufio.NewReader(r)
	}
	return &Protocol{
		reader:  reader,
		writer:  w,
		pending: make(map[int64]chan Response),
	}
}

// SendRequest sends a request and waits for the response.
//
// Description:
//
//	Sends a JSON-RPC request to the server and blocks until a response
//	is received or the context is cancelled.
//
// Inputs:
//
//	ctx - Context for cancellation and timeout
//	method - The LSP method to invoke (e.g., "textDocument/definition")
//	params - Method parameters (will be JSON-marshaled)
//
// Outputs:
//
//	*Response - The server's response
//	error - Non-nil if sending failed, timeout, or server returned error
//
// Thread Safety:
//
//	Safe for concurrent use.
func (p *Protocol) SendRequest(ctx context.Context, method string, params interface{}) (*Response, error) {
	if ctx == nil {
		return nil, fmt.Errorf("ctx must not be nil")
	}
	if atomic.LoadInt32(&p.closed) == 1 {
		return nil, ErrServerNotRunning
	}

	id := atomic.AddInt64(&p.nextID, 1)

	req := Request{
		JSONRPC: JSONRPCVersion,
		ID:      id,
		Method:  method,
		Params:  params,
	}

	// Create response channel
	respCh := make(chan Response, 1)
	p.pendingMu.Lock()
	p.pending[id] = respCh
	p.pendingMu.Unlock()

	defer func() {
		p.pendingMu.Lock()
		delete(p.pending, id)
		p.pendingMu.Unlock()
	}()

	// Send the request
	if err := p.writeMessage(req); err != nil {
		return nil, fmt.Errorf("write request: %w", err)
	}

	// Wait for response
	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("%w: %v", ErrRequestTimeout, ctx.Err())
	case resp := <-respCh:
		if resp.Error != nil {
			return nil, &LSPError{
				Code:    resp.Error.Code,
				Message: resp.Error.Message,
				Data:    resp.Error.Data,
			}
		}
		return &resp, nil
	}
}

// SendNotification sends a notification (no response expected).
//
// Description:
//
//	Sends a JSON-RPC notification to the server. Notifications do not
//	expect a response.
//
// Inputs:
//
//	method - The LSP method to invoke (e.g., "initialized")
//	params - Method parameters (will be JSON-marshaled)
//
// Outputs:
//
//	error - Non-nil if sending failed
//
// Thread Safety:
//
//	Safe for concurrent use.
func (p *Protocol) SendNotification(method string, params interface{}) error {
	if atomic.LoadInt32(&p.closed) == 1 {
		return ErrServerNotRunning
	}

	notif := Notification{
		JSONRPC: JSONRPCVersion,
		Method:  method,
		Params:  params,
	}
	return p.writeMessage(notif)
}

// writeMessage marshals and writes a message with Content-Length header.
func (p *Protocol) writeMessage(v interface{}) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	p.writeMu.Lock()
	defer p.writeMu.Unlock()

	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(data))
	if _, err := p.writer.Write([]byte(header)); err != nil {
		return fmt.Errorf("write header: %w", err)
	}
	if _, err := p.writer.Write(data); err != nil {
		return fmt.Errorf("write body: %w", err)
	}
	return nil
}

// ReadLoop reads messages from the server and dispatches responses.
//
// Description:
//
//	Continuously reads messages from the server. Responses are matched
//	to pending requests. Notifications are ignored. Call this in a
//	goroutine after starting the server.
//
// Inputs:
//
//	ctx - Context for cancellation
//
// Outputs:
//
//	error - Non-nil if reading fails or context is cancelled
//
// Thread Safety:
//
//	Must be called from a single goroutine. Safe to run while other
//	goroutines call SendRequest/SendNotification.
func (p *Protocol) ReadLoop(ctx context.Context) error {
	if p.reader == nil {
		return fmt.Errorf("no reader configured")
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		msg, err := p.readMessage()
		if err != nil {
			if err == io.EOF {
				return ErrServerCrashed
			}
			// Check if we're shutting down
			if atomic.LoadInt32(&p.closed) == 1 {
				return nil
			}
			return fmt.Errorf("read: %w", err)
		}

		p.handleMessage(msg)
	}
}

// readMessage reads a single message from the server.
func (p *Protocol) readMessage() (json.RawMessage, error) {
	var contentLength int

	// Read headers
	for {
		line, err := p.reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimSpace(line)

		// Empty line marks end of headers
		if line == "" {
			break
		}

		// Parse Content-Length header
		if strings.HasPrefix(line, "Content-Length:") {
			lenStr := strings.TrimSpace(strings.TrimPrefix(line, "Content-Length:"))
			var err error
			contentLength, err = strconv.Atoi(lenStr)
			if err != nil {
				return nil, fmt.Errorf("invalid Content-Length value %q: %w", lenStr, err)
			}
			if contentLength < 0 {
				return nil, fmt.Errorf("negative Content-Length: %d", contentLength)
			}
		}
		// Ignore other headers (Content-Type, etc.)
	}

	if contentLength == 0 {
		return nil, fmt.Errorf("missing or zero Content-Length header")
	}

	// Read body
	body := make([]byte, contentLength)
	if _, err := io.ReadFull(p.reader, body); err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	return body, nil
}

// handleMessage dispatches a received message.
func (p *Protocol) handleMessage(msg json.RawMessage) {
	// Try to parse as response (has ID)
	var resp Response
	if err := json.Unmarshal(msg, &resp); err == nil && resp.ID != 0 {
		p.pendingMu.Lock()
		ch, ok := p.pending[resp.ID]
		p.pendingMu.Unlock()

		if ok {
			// Non-blocking send in case channel is full
			select {
			case ch <- resp:
			default:
			}
		}
		return
	}

	// Could be a notification from server - we ignore these for now
	// Future: handle window/logMessage, window/showMessage, etc.
}

// Close marks the protocol as closed.
//
// Description:
//
//	Marks the protocol as closed to prevent further sends. Cancels all
//	pending requests with an error response. Does not close underlying
//	readers/writers.
//
// Thread Safety:
//
//	Safe for concurrent use.
func (p *Protocol) Close() {
	atomic.StoreInt32(&p.closed, 1)

	// Cancel all pending requests by sending an error response
	p.pendingMu.Lock()
	for id, ch := range p.pending {
		// Send error response so waiting goroutines don't receive zero value
		select {
		case ch <- Response{
			JSONRPC: JSONRPCVersion,
			ID:      id,
			Error: &ResponseError{
				Code:    -32099, // Server error
				Message: "server connection closed",
			},
		}:
		default:
			// Channel full, close it
		}
		close(ch)
		delete(p.pending, id)
	}
	p.pendingMu.Unlock()
}
