// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// Tests for SSE writer

package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AleutianAI/AleutianFOSS/services/orchestrator/datatypes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// SSE Writer Tests
// =============================================================================

// mockResponseWriter implements http.ResponseWriter and http.Flusher
type mockResponseWriter struct {
	*httptest.ResponseRecorder
	flushed bool
}

func newMockResponseWriter() *mockResponseWriter {
	return &mockResponseWriter{
		ResponseRecorder: httptest.NewRecorder(),
	}
}

func (m *mockResponseWriter) Flush() {
	m.flushed = true
}

// =============================================================================
// WriteThinking Tests
// =============================================================================

func TestSSEWriter_WriteThinking(t *testing.T) {
	w := newMockResponseWriter()
	SetSSEHeaders(w)

	writer, err := NewSSEWriter(w)
	require.NoError(t, err)
	require.NotNil(t, writer)

	err = writer.WriteThinking("Let me analyze this step by step...")
	assert.NoError(t, err)

	body := w.Body.String()
	assert.Contains(t, body, "thinking")
	assert.Contains(t, body, "Let me analyze")
}

func TestSSEWriter_WriteThinking_EmptyContent(t *testing.T) {
	w := newMockResponseWriter()
	SetSSEHeaders(w)

	writer, err := NewSSEWriter(w)
	require.NoError(t, err)

	err = writer.WriteThinking("")
	assert.NoError(t, err)
}

func TestSSEWriter_WriteThinking_LongContent(t *testing.T) {
	w := newMockResponseWriter()
	SetSSEHeaders(w)

	writer, err := NewSSEWriter(w)
	require.NoError(t, err)

	longContent := strings.Repeat("thinking deeply ", 1000)
	err = writer.WriteThinking(longContent)
	assert.NoError(t, err)
}

// =============================================================================
// WriteSources Tests
// =============================================================================

func TestSSEWriter_WriteSources(t *testing.T) {
	w := newMockResponseWriter()
	SetSSEHeaders(w)

	writer, err := NewSSEWriter(w)
	require.NoError(t, err)

	sources := []datatypes.SourceInfo{
		{Source: "auth.md", Score: 0.95},
		{Source: "api.go", Score: 0.87},
	}

	err = writer.WriteSources(sources)
	assert.NoError(t, err)

	body := w.Body.String()
	assert.Contains(t, body, "sources")
}

func TestSSEWriter_WriteSources_Empty(t *testing.T) {
	w := newMockResponseWriter()
	SetSSEHeaders(w)

	writer, err := NewSSEWriter(w)
	require.NoError(t, err)

	err = writer.WriteSources([]datatypes.SourceInfo{})
	assert.NoError(t, err)
}

func TestSSEWriter_WriteSources_Nil(t *testing.T) {
	w := newMockResponseWriter()
	SetSSEHeaders(w)

	writer, err := NewSSEWriter(w)
	require.NoError(t, err)

	err = writer.WriteSources(nil)
	assert.NoError(t, err)
}

func TestSSEWriter_WriteSources_SingleSource(t *testing.T) {
	w := newMockResponseWriter()
	SetSSEHeaders(w)

	writer, err := NewSSEWriter(w)
	require.NoError(t, err)

	sources := []datatypes.SourceInfo{
		{Source: "document.pdf", Score: 0.99, Distance: 0.01},
	}

	err = writer.WriteSources(sources)
	assert.NoError(t, err)
}

// =============================================================================
// WriteError Tests
// =============================================================================

func TestSSEWriter_WriteError(t *testing.T) {
	w := newMockResponseWriter()
	SetSSEHeaders(w)

	writer, err := NewSSEWriter(w)
	require.NoError(t, err)

	err = writer.WriteError("Service temporarily unavailable")
	assert.NoError(t, err)

	body := w.Body.String()
	assert.Contains(t, body, "error")
	assert.Contains(t, body, "Service temporarily unavailable")
}

func TestSSEWriter_WriteError_Empty(t *testing.T) {
	w := newMockResponseWriter()
	SetSSEHeaders(w)

	writer, err := NewSSEWriter(w)
	require.NoError(t, err)

	err = writer.WriteError("")
	assert.NoError(t, err)

	body := w.Body.String()
	assert.Contains(t, body, "error")
}

func TestSSEWriter_WriteError_SanitizedMessage(t *testing.T) {
	w := newMockResponseWriter()
	SetSSEHeaders(w)

	writer, err := NewSSEWriter(w)
	require.NoError(t, err)

	// Per SEC-005, error messages should be sanitized before passing here
	err = writer.WriteError("Connection timeout")
	assert.NoError(t, err)
}

// =============================================================================
// WriteKeepAlive Tests
// =============================================================================

func TestSSEWriter_WriteKeepAlive(t *testing.T) {
	w := newMockResponseWriter()
	SetSSEHeaders(w)

	writer, err := NewSSEWriter(w)
	require.NoError(t, err)

	err = writer.WriteKeepAlive()
	assert.NoError(t, err)

	body := w.Body.String()
	assert.Contains(t, body, ": ping")
	assert.True(t, w.flushed, "Flusher should be called")
}

func TestSSEWriter_WriteKeepAlive_Multiple(t *testing.T) {
	w := newMockResponseWriter()
	SetSSEHeaders(w)

	writer, err := NewSSEWriter(w)
	require.NoError(t, err)

	// Send multiple keep-alives
	for i := 0; i < 5; i++ {
		err = writer.WriteKeepAlive()
		assert.NoError(t, err)
	}

	body := w.Body.String()
	assert.Equal(t, 5, strings.Count(body, ": ping"))
}

// =============================================================================
// SetSSEHeaders Tests
// =============================================================================

func TestSetSSEHeaders(t *testing.T) {
	w := httptest.NewRecorder()

	SetSSEHeaders(w)

	headers := w.Header()
	assert.Equal(t, "text/event-stream", headers.Get("Content-Type"))
	assert.Equal(t, "no-cache", headers.Get("Cache-Control"))
	assert.Equal(t, "keep-alive", headers.Get("Connection"))
	assert.Equal(t, "no", headers.Get("X-Accel-Buffering"))
}

// =============================================================================
// Integration Tests
// =============================================================================

func TestSSEWriter_FullWorkflow(t *testing.T) {
	w := newMockResponseWriter()
	SetSSEHeaders(w)

	writer, err := NewSSEWriter(w)
	require.NoError(t, err)

	// Simulate a full streaming workflow
	err = writer.WriteStatus("Processing request...")
	assert.NoError(t, err)

	err = writer.WriteToken("Hello")
	assert.NoError(t, err)

	err = writer.WriteToken(" ")
	assert.NoError(t, err)

	err = writer.WriteToken("World")
	assert.NoError(t, err)

	err = writer.WriteDone("sess-123")
	assert.NoError(t, err)

	body := w.Body.String()
	assert.Contains(t, body, "status")
	assert.Contains(t, body, "token")
	assert.Contains(t, body, "done")
}

func TestSSEWriter_RAGWorkflow(t *testing.T) {
	w := newMockResponseWriter()
	SetSSEHeaders(w)

	writer, err := NewSSEWriter(w)
	require.NoError(t, err)

	// Simulate a RAG streaming workflow
	err = writer.WriteStatus("Searching knowledge base...")
	assert.NoError(t, err)

	sources := []datatypes.SourceInfo{
		{Source: "doc1.md", Score: 0.95},
		{Source: "doc2.md", Score: 0.87},
	}
	err = writer.WriteSources(sources)
	assert.NoError(t, err)

	err = writer.WriteToken("Based on the documents, ")
	assert.NoError(t, err)

	err = writer.WriteDone("sess-456")
	assert.NoError(t, err)
}

func TestSSEWriter_ErrorWorkflow(t *testing.T) {
	w := newMockResponseWriter()
	SetSSEHeaders(w)

	writer, err := NewSSEWriter(w)
	require.NoError(t, err)

	// Simulate an error workflow
	err = writer.WriteStatus("Processing...")
	assert.NoError(t, err)

	err = writer.WriteError("Service unavailable")
	assert.NoError(t, err)

	body := w.Body.String()
	assert.Contains(t, body, "status")
	assert.Contains(t, body, "error")
}

// =============================================================================
// NewSSEWriter Tests
// =============================================================================

func TestNewSSEWriter_InvalidWriter(t *testing.T) {
	// Create a writer that doesn't implement Flusher
	w := &nonFlushingWriter{}

	_, err := NewSSEWriter(w)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "Flusher")
}

// nonFlushingWriter implements http.ResponseWriter but not http.Flusher
type nonFlushingWriter struct{}

func (w *nonFlushingWriter) Header() http.Header {
	return make(http.Header)
}

func (w *nonFlushingWriter) Write(data []byte) (int, error) {
	return len(data), nil
}

func (w *nonFlushingWriter) WriteHeader(statusCode int) {}

func TestNewSSEWriter_ValidWriter(t *testing.T) {
	w := newMockResponseWriter()

	writer, err := NewSSEWriter(w)
	assert.NoError(t, err)
	assert.NotNil(t, writer)
}
