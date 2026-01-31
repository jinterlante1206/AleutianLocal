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
	"errors"
	"fmt"
)

// Sentinel errors for LSP operations.
var (
	// ErrServerNotRunning indicates the LSP server is not in a ready state.
	ErrServerNotRunning = errors.New("lsp server not running")

	// ErrServerNotInstalled indicates the LSP server binary was not found.
	ErrServerNotInstalled = errors.New("lsp server not installed")

	// ErrUnsupportedLanguage indicates no LSP configuration exists for the language.
	ErrUnsupportedLanguage = errors.New("no lsp configuration for language")

	// ErrInitializeFailed indicates the LSP initialize handshake failed.
	ErrInitializeFailed = errors.New("lsp initialize failed")

	// ErrRequestTimeout indicates the LSP request exceeded the timeout.
	ErrRequestTimeout = errors.New("lsp request timeout")

	// ErrServerCrashed indicates the LSP server process terminated unexpectedly.
	ErrServerCrashed = errors.New("lsp server crashed")

	// ErrInvalidResponse indicates the LSP response could not be parsed.
	ErrInvalidResponse = errors.New("invalid lsp response")

	// ErrServerAlreadyStarted indicates Start was called on an already running server.
	ErrServerAlreadyStarted = errors.New("server already started")
)

// LSPError represents an error returned by the language server via JSON-RPC.
//
// LSP error codes follow the JSON-RPC spec plus LSP-specific codes:
//   - -32700: Parse error
//   - -32600: Invalid request
//   - -32601: Method not found
//   - -32602: Invalid params
//   - -32603: Internal error
//   - -32099 to -32000: Server error (reserved)
//   - -32802: Server not initialized
//   - -32801: Unknown error code
//   - -32800: Request cancelled
type LSPError struct {
	// Code is the JSON-RPC error code.
	Code int

	// Message is the error message from the server.
	Message string

	// Data contains optional additional data about the error.
	Data interface{}
}

// Error implements the error interface.
func (e *LSPError) Error() string {
	if e.Data != nil {
		return fmt.Sprintf("LSP error %d: %s (data: %v)", e.Code, e.Message, e.Data)
	}
	return fmt.Sprintf("LSP error %d: %s", e.Code, e.Message)
}

// IsParseError returns true if this is a JSON-RPC parse error.
func (e *LSPError) IsParseError() bool {
	return e.Code == -32700
}

// IsMethodNotFound returns true if the method is not supported by the server.
func (e *LSPError) IsMethodNotFound() bool {
	return e.Code == -32601
}

// IsRequestCancelled returns true if the request was cancelled.
func (e *LSPError) IsRequestCancelled() bool {
	return e.Code == -32800
}

// IsServerNotInitialized returns true if the server is not initialized.
func (e *LSPError) IsServerNotInitialized() bool {
	return e.Code == -32802
}
