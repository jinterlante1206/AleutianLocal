// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package handlers

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jinterlante1206/AleutianLocal/services/orchestrator/datatypes"
)

// =============================================================================
// Interface Definition
// =============================================================================

// SSEWriter defines the contract for writing Server-Sent Events to HTTP responses.
//
// # Description
//
// SSEWriter abstracts SSE event serialization and writing, enabling
// testability and separation from HTTP response mechanics. Implementations
// handle the SSE wire format (event: type\ndata: json\n\n) internally.
//
// Each event is automatically assigned:
//   - Id: UUID v4 for ordering and deduplication
//   - CreatedAt: Unix timestamp in milliseconds
//   - Hash: SHA-256 hash of event content for integrity
//   - PrevHash: Hash of previous event for chain verification
//
// # Thread Safety
//
// Implementations must be safe for concurrent use by multiple goroutines.
// Streaming handlers may emit events from different sources concurrently.
//
// # Limitations
//
//   - Must be used with http.Flusher-compatible ResponseWriter
//   - Response headers must be set before first write
//
// # Assumptions
//
//   - Caller has set Content-Type: text/event-stream before writing
//   - Caller has disabled buffering (X-Accel-Buffering: no)
type SSEWriter interface {
	// WriteEvent writes a single SSE event to the response.
	//
	// # Description
	//
	// Populates event metadata (Id, CreatedAt, Hash, PrevHash), serializes
	// to JSON, and writes in SSE format. Flushes immediately after writing.
	//
	// # Inputs
	//
	//   - event: StreamEvent to write. Id, CreatedAt, Hash, PrevHash are auto-set.
	//
	// # Outputs
	//
	//   - error: Non-nil if JSON marshaling or writing failed.
	//
	// # Limitations
	//
	//   - Event must be JSON-serializable
	//
	// # Assumptions
	//
	//   - Connection is still open
	WriteEvent(event datatypes.StreamEvent) error

	// WriteStatus writes a status event with the given message.
	//
	// # Description
	//
	// Convenience method for writing status events. Creates a StreamEvent
	// with Type="status" and the provided message.
	//
	// # Inputs
	//
	//   - message: Status message to display (e.g., "Searching documents...")
	//
	// # Outputs
	//
	//   - error: Non-nil if writing failed.
	//
	// # Limitations
	//
	//   - Message is not validated for length
	//
	// # Assumptions
	//
	//   - Message is human-readable
	WriteStatus(message string) error

	// WriteToken writes a token event with the given content.
	//
	// # Description
	//
	// Convenience method for writing token events. Creates a StreamEvent
	// with Type="token" and the provided content.
	//
	// # Inputs
	//
	//   - content: Token text to stream (may be partial word or whitespace)
	//
	// # Outputs
	//
	//   - error: Non-nil if writing failed.
	//
	// # Limitations
	//
	//   - No buffering; each token is sent immediately
	//
	// # Assumptions
	//
	//   - Tokens are in display order
	WriteToken(content string) error

	// WriteThinking writes a thinking event (Claude extended thinking).
	//
	// # Description
	//
	// Convenience method for writing thinking events from Claude's
	// extended thinking feature.
	//
	// # Inputs
	//
	//   - content: Thinking text from Claude.
	//
	// # Outputs
	//
	//   - error: Non-nil if writing failed.
	//
	// # Limitations
	//
	//   - Only applicable when extended thinking is enabled
	//
	// # Assumptions
	//
	//   - Content is from Claude's thinking block
	WriteThinking(content string) error

	// WriteSources writes a sources event with retrieved documents.
	//
	// # Description
	//
	// Convenience method for writing sources events (RAG only).
	// Includes source documents with relevance scores.
	//
	// # Inputs
	//
	//   - sources: Retrieved document sources with scores.
	//
	// # Outputs
	//
	//   - error: Non-nil if writing failed.
	//
	// # Limitations
	//
	//   - Only applicable for RAG streaming
	//
	// # Assumptions
	//
	//   - Sources are ordered by relevance score
	WriteSources(sources []datatypes.SourceInfo) error

	// WriteError writes an error event and signals stream failure.
	//
	// # Description
	//
	// Writes an error event to inform the client of a failure.
	// Should be followed by closing the stream.
	//
	// # Inputs
	//
	//   - errMsg: Error message for the client (sanitized, no internal details)
	//
	// # Outputs
	//
	//   - error: Non-nil if writing failed.
	//
	// # Limitations
	//
	//   - Error message should be sanitized (SEC-005)
	//
	// # Assumptions
	//
	//   - Stream will be closed after error event
	//
	// # Security References
	//
	//   - SEC-005: Internal errors not exposed to client
	WriteError(errMsg string) error

	// WriteDone writes the done event with session ID and closes the stream.
	//
	// # Description
	//
	// Writes the final event indicating successful stream completion.
	// Includes session ID for multi-turn conversation tracking.
	//
	// # Inputs
	//
	//   - sessionID: Session identifier for conversation continuity
	//
	// # Outputs
	//
	//   - error: Non-nil if writing failed.
	//
	// # Limitations
	//
	//   - Should only be called once per stream
	//
	// # Assumptions
	//
	//   - No more events will be written after done
	WriteDone(sessionID string) error

	// WriteKeepAlive sends a comment line to prevent connection timeouts.
	//
	// # Description
	//
	// Sends an SSE comment (": ping\n\n") to keep the connection alive during
	// long operations like RAG retrieval or LLM thinking. SSE comments are
	// ignored by clients but keep the TCP connection active, preventing
	// timeout disconnections from load balancers (AWS ALB, Nginx default 60s).
	//
	// # Outputs
	//
	//   - error: Non-nil if writing failed.
	//
	// # Examples
	//
	//	// In a goroutine during long operations:
	//	ticker := time.NewTicker(15 * time.Second)
	//	defer ticker.Stop()
	//	for {
	//	    select {
	//	    case <-ticker.C:
	//	        writer.WriteKeepAlive()
	//	    case <-done:
	//	        return
	//	    }
	//	}
	//
	// # Limitations
	//
	//   - Does not update the hash chain (comments are not events)
	//
	// # Assumptions
	//
	//   - Connection is still open
	WriteKeepAlive() error
}

// =============================================================================
// Struct Definition
// =============================================================================

// sseWriter implements SSEWriter for HTTP SSE responses.
//
// # Description
//
// sseWriter wraps an http.ResponseWriter to emit SSE-formatted events.
// Each event is written in the format:
//
//	event: {type}
//	data: {json}
//
// The writer maintains a hash chain for integrity verification:
//   - Each event's Hash is SHA-256 of its content (including sources)
//   - Each event's PrevHash links to the previous event
//
// This provides chain of custody for content, sources, and timestamps.
//
// # Fields
//
//   - writer: Underlying http.ResponseWriter
//   - flusher: http.Flusher interface for immediate send
//   - prevHash: Hash of the last written event (for chain)
//   - mu: Mutex for thread-safe writes
//
// # Thread Safety
//
// Thread-safe via mutex. Multiple goroutines can write events concurrently.
// Hash chain integrity is maintained across concurrent writes.
//
// # Limitations
//
//   - Panics if ResponseWriter doesn't implement http.Flusher
//   - Cannot be reused across requests
//
// # Assumptions
//
//   - Response headers already set by caller
//   - ResponseWriter supports http.Flusher interface
type sseWriter struct {
	writer   http.ResponseWriter
	flusher  http.Flusher
	prevHash string
	mu       sync.Mutex
}

// =============================================================================
// Constructor
// =============================================================================

// NewSSEWriter creates a new SSEWriter for the given ResponseWriter.
//
// # Description
//
// Creates an sseWriter that wraps the ResponseWriter. The caller must
// set appropriate SSE headers before creating the writer.
//
// # Inputs
//
//   - w: HTTP ResponseWriter. Must implement http.Flusher.
//
// # Outputs
//
//   - SSEWriter: Ready to write SSE events.
//   - error: Non-nil if ResponseWriter doesn't support flushing.
//
// # Examples
//
//	SetSSEHeaders(w)
//	writer, err := NewSSEWriter(w)
//	if err != nil {
//	    http.Error(w, "Streaming not supported", http.StatusInternalServerError)
//	    return
//	}
//	writer.WriteStatus("Processing...")
//	writer.WriteToken("Hello")
//	writer.WriteDone("sess-123")
//
// # Limitations
//
//   - Requires http.Flusher support (most ResponseWriters have it)
//
// # Assumptions
//
//   - Caller has set SSE headers via SetSSEHeaders()
func NewSSEWriter(w http.ResponseWriter) (SSEWriter, error) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil, fmt.Errorf("ResponseWriter does not support http.Flusher")
	}

	return &sseWriter{
		writer:   w,
		flusher:  flusher,
		prevHash: "",
	}, nil
}

// =============================================================================
// Methods
// =============================================================================

// WriteEvent writes a single SSE event to the response.
//
// # Description
//
// Populates event metadata (Id, CreatedAt, Hash, PrevHash), serializes
// to JSON, and writes in SSE format. Flushes immediately after writing.
//
// The hash covers all content fields including sources for complete
// chain of custody.
//
// # Inputs
//
//   - event: StreamEvent to write. Id, CreatedAt, Hash, PrevHash are auto-set.
//
// # Outputs
//
//   - error: Non-nil if JSON marshaling or writing failed.
//
// # Examples
//
//	err := w.WriteEvent(datatypes.StreamEvent{
//	    Type:    "sources",
//	    Sources: []datatypes.SourceInfo{{Source: "doc.pdf", Score: 0.95}},
//	})
//
// # Limitations
//
//   - Event must be JSON-serializable
//
// # Assumptions
//
//   - Connection is still open
func (w *sseWriter) WriteEvent(event datatypes.StreamEvent) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Populate metadata
	event.Id = uuid.New().String()
	event.CreatedAt = time.Now().UnixMilli()
	event.PrevHash = w.prevHash

	// Compute hash of event content (before setting Hash field)
	event.Hash = w.computeEventHash(event)

	// Update chain for next event
	w.prevHash = event.Hash

	// Serialize to JSON
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}

	// Write SSE format: event: type\ndata: json\n\n
	if _, err := fmt.Fprintf(w.writer, "event: %s\ndata: %s\n\n", event.Type, data); err != nil {
		return fmt.Errorf("write event: %w", err)
	}

	w.flusher.Flush()
	return nil
}

// computeEventHash computes SHA-256 hash of event content.
//
// # Description
//
// Hashes all content fields for complete chain of custody:
//   - Id, Type, CreatedAt, PrevHash (metadata)
//   - Content, Message, Error, SessionId (content fields)
//   - Sources (serialized to JSON for consistent hashing)
//
// # Inputs
//
//   - event: Event to hash (Hash field should be empty when called).
//
// # Outputs
//
//   - string: Hex-encoded SHA-256 hash.
//
// # Limitations
//
//   - Sources are JSON-serialized which adds overhead for large source lists.
//
// # Assumptions
//
//   - Called before setting event.Hash field.
func (w *sseWriter) computeEventHash(event datatypes.StreamEvent) string {
	// Serialize sources for consistent hashing
	sourcesJSON := ""
	if len(event.Sources) > 0 {
		if data, err := json.Marshal(event.Sources); err == nil {
			sourcesJSON = string(data)
		}
	}

	// Hash all content fields for complete chain of custody
	hashInput := fmt.Sprintf("%s|%s|%d|%s|%s|%s|%s|%s|%s",
		event.Id,
		event.Type,
		event.CreatedAt,
		event.PrevHash,
		event.Content,
		event.Message,
		event.Error,
		event.SessionId,
		sourcesJSON,
	)

	hash := sha256.Sum256([]byte(hashInput))
	return hex.EncodeToString(hash[:])
}

// WriteStatus writes a status event with the given message.
//
// # Description
//
// Convenience method for writing status events.
//
// # Inputs
//
//   - message: Status message to display (e.g., "Searching documents...")
//
// # Outputs
//
//   - error: Non-nil if writing failed.
//
// # Examples
//
//	err := writer.WriteStatus("Retrieving context from knowledge base...")
//
// # Limitations
//
//   - None.
//
// # Assumptions
//
//   - Message is suitable for display to user.
func (w *sseWriter) WriteStatus(message string) error {
	return w.WriteEvent(datatypes.StreamEvent{
		Type:    "status",
		Message: message,
	})
}

// WriteToken writes a token event with the given content.
//
// # Description
//
// Convenience method for writing token events.
//
// # Inputs
//
//   - content: Token text to stream (may be partial word or whitespace)
//
// # Outputs
//
//   - error: Non-nil if writing failed.
//
// # Examples
//
//	err := writer.WriteToken("Hello")
//	err = writer.WriteToken(" world")
//
// # Limitations
//
//   - Each call flushes immediately (no batching).
//
// # Assumptions
//
//   - Tokens arrive in display order.
func (w *sseWriter) WriteToken(content string) error {
	return w.WriteEvent(datatypes.StreamEvent{
		Type:    "token",
		Content: content,
	})
}

// WriteThinking writes a thinking event (Claude extended thinking).
//
// # Description
//
// Convenience method for writing thinking events from Claude's
// extended thinking feature.
//
// # Inputs
//
//   - content: Thinking text from Claude.
//
// # Outputs
//
//   - error: Non-nil if writing failed.
//
// # Examples
//
//	err := writer.WriteThinking("Let me analyze this step by step...")
//
// # Limitations
//
//   - Only applicable when extended thinking is enabled.
//
// # Assumptions
//
//   - Content is from Claude's thinking block.
func (w *sseWriter) WriteThinking(content string) error {
	return w.WriteEvent(datatypes.StreamEvent{
		Type:    "thinking",
		Content: content,
	})
}

// WriteSources writes a sources event with retrieved documents.
//
// # Description
//
// Convenience method for writing sources events (RAG only).
//
// # Inputs
//
//   - sources: Retrieved document sources with scores.
//
// # Outputs
//
//   - error: Non-nil if writing failed.
//
// # Examples
//
//	err := writer.WriteSources([]datatypes.SourceInfo{
//	    {Source: "auth.md", Score: 0.95},
//	})
//
// # Limitations
//
//   - Only applicable for RAG streaming.
//
// # Assumptions
//
//   - Sources are ordered by relevance score.
func (w *sseWriter) WriteSources(sources []datatypes.SourceInfo) error {
	return w.WriteEvent(datatypes.StreamEvent{
		Type:    "sources",
		Sources: sources,
	})
}

// WriteError writes an error event.
//
// # Description
//
// Writes an error event to inform the client of a failure.
// Per SEC-005: Error messages must be sanitized before passing to this method.
//
// # Inputs
//
//   - errMsg: Sanitized error message for client display.
//
// # Outputs
//
//   - error: Non-nil if writing failed.
//
// # Examples
//
//	err := writer.WriteError("Service temporarily unavailable")
//
// # Limitations
//
//   - Caller must sanitize error messages (no internal details).
//
// # Assumptions
//
//   - Stream will be closed after this event.
//
// # Security References
//
//   - SEC-005: Internal errors not exposed to client
func (w *sseWriter) WriteError(errMsg string) error {
	return w.WriteEvent(datatypes.StreamEvent{
		Type:  "error",
		Error: errMsg,
	})
}

// WriteDone writes the done event with session ID.
//
// # Description
//
// Writes the final event indicating successful stream completion.
//
// # Inputs
//
//   - sessionID: Session identifier for conversation continuity.
//
// # Outputs
//
//   - error: Non-nil if writing failed.
//
// # Examples
//
//	err := writer.WriteDone("sess-abc123")
//
// # Limitations
//
//   - Should only be called once per stream.
//
// # Assumptions
//
//   - All content has been written before calling.
func (w *sseWriter) WriteDone(sessionID string) error {
	return w.WriteEvent(datatypes.StreamEvent{
		Type:      "done",
		SessionId: sessionID,
	})
}

// WriteKeepAlive sends a comment line to keep the connection alive.
//
// # Description
//
// Writes an SSE comment (": ping\n\n") to keep the TCP connection active
// during long operations. Comments are ignored by SSE clients but reset
// load balancer timeout counters.
//
// # Outputs
//
//   - error: Non-nil if writing failed.
//
// # Examples
//
//	err := writer.WriteKeepAlive()
//
// # Limitations
//
//   - Does not update the hash chain.
//
// # Assumptions
//
//   - Connection is still open.
func (w *sseWriter) WriteKeepAlive() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	// SSE comment format: colon followed by text, then double newline
	if _, err := fmt.Fprintf(w.writer, ": ping\n\n"); err != nil {
		return fmt.Errorf("write keepalive: %w", err)
	}

	w.flusher.Flush()
	return nil
}

// =============================================================================
// Helper Functions
// =============================================================================

// SetSSEHeaders configures HTTP response headers for SSE streaming.
//
// # Description
//
// Sets the required headers for Server-Sent Events:
//   - Content-Type: text/event-stream
//   - Cache-Control: no-cache
//   - Connection: keep-alive
//   - X-Accel-Buffering: no (disables nginx buffering)
//
// Must be called before writing any response body.
//
// # Inputs
//
//   - w: HTTP ResponseWriter to configure.
//
// # Outputs
//
// None.
//
// # Examples
//
//	func HandleStream(w http.ResponseWriter, r *http.Request) {
//	    SetSSEHeaders(w)
//	    writer, _ := NewSSEWriter(w)
//	    // ... write events ...
//	}
//
// # Limitations
//
//   - Must be called before any writes to ResponseWriter.
//
// # Assumptions
//
//   - No response has been written yet.
func SetSSEHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
}

// =============================================================================
// Compile-time Interface Check
// =============================================================================

var _ SSEWriter = (*sseWriter)(nil)
