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
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

// blockingReader is a reader that blocks forever on Read.
type blockingReader struct{}

func (b *blockingReader) Read(p []byte) (int, error) {
	// Block forever by waiting on a channel that will never receive
	select {}
}

func TestProtocol_WriteMessage(t *testing.T) {
	t.Run("writes Content-Length header", func(t *testing.T) {
		var buf bytes.Buffer
		p := NewProtocol(nil, &buf)

		req := Request{
			JSONRPC: "2.0",
			ID:      1,
			Method:  "test",
		}

		if err := p.writeMessage(req); err != nil {
			t.Fatalf("writeMessage: %v", err)
		}

		output := buf.String()
		if !strings.Contains(output, "Content-Length:") {
			t.Errorf("missing Content-Length header in: %s", output)
		}
	})

	t.Run("writes valid JSON body", func(t *testing.T) {
		var buf bytes.Buffer
		p := NewProtocol(nil, &buf)

		req := Request{
			JSONRPC: "2.0",
			ID:      1,
			Method:  "test",
		}

		if err := p.writeMessage(req); err != nil {
			t.Fatalf("writeMessage: %v", err)
		}

		output := buf.String()
		if !strings.Contains(output, `"jsonrpc":"2.0"`) {
			t.Errorf("missing jsonrpc field in: %s", output)
		}
		if !strings.Contains(output, `"id":1`) {
			t.Errorf("missing id field in: %s", output)
		}
		if !strings.Contains(output, `"method":"test"`) {
			t.Errorf("missing method field in: %s", output)
		}
	})

	t.Run("writes params when provided", func(t *testing.T) {
		var buf bytes.Buffer
		p := NewProtocol(nil, &buf)

		req := Request{
			JSONRPC: "2.0",
			ID:      1,
			Method:  "test",
			Params:  map[string]string{"key": "value"},
		}

		if err := p.writeMessage(req); err != nil {
			t.Fatalf("writeMessage: %v", err)
		}

		output := buf.String()
		if !strings.Contains(output, `"key":"value"`) {
			t.Errorf("missing params in: %s", output)
		}
	})
}

func TestProtocol_ReadMessage(t *testing.T) {
	t.Run("reads valid message", func(t *testing.T) {
		msg := `{"jsonrpc":"2.0","id":1,"result":null}`
		input := fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(msg), msg)

		p := NewProtocol(strings.NewReader(input), nil)

		body, err := p.readMessage()
		if err != nil {
			t.Fatalf("readMessage: %v", err)
		}

		if string(body) != msg {
			t.Errorf("got %s, want %s", body, msg)
		}
	})

	t.Run("handles multiple headers", func(t *testing.T) {
		msg := `{"jsonrpc":"2.0","id":1,"result":null}`
		input := fmt.Sprintf("Content-Length: %d\r\nContent-Type: application/json\r\n\r\n%s", len(msg), msg)

		p := NewProtocol(strings.NewReader(input), nil)

		body, err := p.readMessage()
		if err != nil {
			t.Fatalf("readMessage: %v", err)
		}

		if string(body) != msg {
			t.Errorf("got %s, want %s", body, msg)
		}
	})

	t.Run("returns error for missing Content-Length", func(t *testing.T) {
		input := "\r\n{\"test\":true}"

		p := NewProtocol(strings.NewReader(input), nil)

		_, err := p.readMessage()
		if err == nil {
			t.Error("expected error for missing Content-Length")
		}
	})

	t.Run("returns EOF for empty input", func(t *testing.T) {
		p := NewProtocol(strings.NewReader(""), nil)

		_, err := p.readMessage()
		if err != io.EOF {
			t.Errorf("expected EOF, got %v", err)
		}
	})
}

func TestProtocol_HandleMessage(t *testing.T) {
	t.Run("dispatches response to pending request", func(t *testing.T) {
		p := NewProtocol(nil, nil)

		// Register pending request
		respCh := make(chan Response, 1)
		p.pendingMu.Lock()
		p.pending[42] = respCh
		p.pendingMu.Unlock()

		// Simulate receiving response
		msg := []byte(`{"jsonrpc":"2.0","id":42,"result":"test"}`)
		p.handleMessage(msg)

		select {
		case resp := <-respCh:
			if resp.ID != 42 {
				t.Errorf("ID = %d, want 42", resp.ID)
			}
		case <-time.After(100 * time.Millisecond):
			t.Error("timeout waiting for response")
		}
	})

	t.Run("ignores unknown request ID", func(t *testing.T) {
		p := NewProtocol(nil, nil)

		// No pending requests
		msg := []byte(`{"jsonrpc":"2.0","id":999,"result":"test"}`)
		p.handleMessage(msg) // Should not panic
	})

	t.Run("ignores notifications", func(t *testing.T) {
		p := NewProtocol(nil, nil)

		// Notification has no ID
		msg := []byte(`{"jsonrpc":"2.0","method":"window/logMessage","params":{}}`)
		p.handleMessage(msg) // Should not panic
	})
}

func TestProtocol_SendRequest(t *testing.T) {
	t.Run("returns error for nil context", func(t *testing.T) {
		var buf bytes.Buffer
		p := NewProtocol(nil, &buf)

		_, err := p.SendRequest(nil, "test", nil) //nolint:staticcheck
		if err == nil {
			t.Error("expected error for nil context")
		}
	})

	t.Run("returns error when closed", func(t *testing.T) {
		var buf bytes.Buffer
		p := NewProtocol(nil, &buf)
		p.Close()

		ctx := context.Background()
		_, err := p.SendRequest(ctx, "test", nil)
		if err != ErrServerNotRunning {
			t.Errorf("expected ErrServerNotRunning, got %v", err)
		}
	})

	t.Run("returns error on timeout", func(t *testing.T) {
		// Create a reader that blocks forever and a buffer for writes
		blockingReader := &blockingReader{}
		var buf bytes.Buffer
		p := NewProtocol(blockingReader, &buf)

		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()

		_, err := p.SendRequest(ctx, "test", nil)
		if err == nil {
			t.Error("expected timeout error")
		}
		if !strings.Contains(err.Error(), "timeout") {
			t.Errorf("expected timeout error, got %v", err)
		}
	})
}

func TestProtocol_SendNotification(t *testing.T) {
	t.Run("sends notification", func(t *testing.T) {
		var buf bytes.Buffer
		p := NewProtocol(nil, &buf)

		err := p.SendNotification("initialized", struct{}{})
		if err != nil {
			t.Fatalf("SendNotification: %v", err)
		}

		output := buf.String()
		if !strings.Contains(output, `"method":"initialized"`) {
			t.Errorf("missing method in: %s", output)
		}
		// Notifications should not have ID
		if strings.Contains(output, `"id":`) {
			t.Errorf("notification should not have ID in: %s", output)
		}
	})

	t.Run("returns error when closed", func(t *testing.T) {
		var buf bytes.Buffer
		p := NewProtocol(nil, &buf)
		p.Close()

		err := p.SendNotification("test", nil)
		if err != ErrServerNotRunning {
			t.Errorf("expected ErrServerNotRunning, got %v", err)
		}
	})
}

func TestProtocol_Close(t *testing.T) {
	t.Run("cancels pending requests with error response", func(t *testing.T) {
		p := NewProtocol(nil, nil)

		// Register pending request
		respCh := make(chan Response, 1)
		p.pendingMu.Lock()
		p.pending[1] = respCh
		p.pendingMu.Unlock()

		p.Close()

		// Should receive error response before channel closes
		select {
		case resp, ok := <-respCh:
			if !ok {
				// Channel closed without response, try reading again for closed status
				_, ok2 := <-respCh
				if ok2 {
					t.Error("expected channel to be closed after error response")
				}
			} else {
				// Verify error response was sent
				if resp.Error == nil {
					t.Error("expected error response, got nil error")
				} else if resp.Error.Code != -32099 {
					t.Errorf("expected error code -32099, got %d", resp.Error.Code)
				}
				// Channel should be closed now
				_, ok2 := <-respCh
				if ok2 {
					t.Error("expected channel to be closed after error response")
				}
			}
		case <-time.After(100 * time.Millisecond):
			t.Error("timeout waiting for response or channel close")
		}
	})

	t.Run("is idempotent", func(t *testing.T) {
		p := NewProtocol(nil, nil)
		p.Close()
		p.Close() // Should not panic
	})
}

func TestProtocol_Concurrent(t *testing.T) {
	t.Run("handles concurrent writes", func(t *testing.T) {
		var buf bytes.Buffer
		p := NewProtocol(nil, &buf)

		var wg sync.WaitGroup
		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func(n int) {
				defer wg.Done()
				err := p.SendNotification("test", map[string]int{"n": n})
				if err != nil {
					t.Errorf("SendNotification: %v", err)
				}
			}(i)
		}
		wg.Wait()

		// All messages should be complete (no interleaving)
		output := buf.String()
		count := strings.Count(output, `"method":"test"`)
		if count != 10 {
			t.Errorf("expected 10 messages, found %d", count)
		}
	})
}

func TestRequest_MarshalJSON(t *testing.T) {
	req := Request{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "textDocument/definition",
		Params: TextDocumentPositionParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///test.go"},
			Position:     Position{Line: 10, Character: 5},
		},
	}

	var buf bytes.Buffer
	p := NewProtocol(nil, &buf)
	if err := p.writeMessage(req); err != nil {
		t.Fatalf("writeMessage: %v", err)
	}

	output := buf.String()
	expected := []string{
		`"jsonrpc":"2.0"`,
		`"id":1`,
		`"method":"textDocument/definition"`,
		`"textDocument":{"uri":"file:///test.go"}`,
		`"position":{"line":10,"character":5}`,
	}

	for _, s := range expected {
		if !strings.Contains(output, s) {
			t.Errorf("missing %q in: %s", s, output)
		}
	}
}

func TestNotification_MarshalJSON(t *testing.T) {
	notif := Notification{
		JSONRPC: "2.0",
		Method:  "textDocument/didOpen",
		Params: DidOpenTextDocumentParams{
			TextDocument: TextDocumentItem{
				URI:        "file:///test.go",
				LanguageID: "go",
				Version:    1,
				Text:       "package main",
			},
		},
	}

	var buf bytes.Buffer
	p := NewProtocol(nil, &buf)
	if err := p.writeMessage(notif); err != nil {
		t.Fatalf("writeMessage: %v", err)
	}

	output := buf.String()
	if strings.Contains(output, `"id":`) {
		t.Errorf("notification should not have ID in: %s", output)
	}
	if !strings.Contains(output, `"languageId":"go"`) {
		t.Errorf("missing languageId in: %s", output)
	}
}
