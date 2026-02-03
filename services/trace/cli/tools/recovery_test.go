// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package tools

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
)

func TestErrorRecovery_SuggestFix_FileNotFound(t *testing.T) {
	recovery := NewErrorRecovery()

	call := ToolCall{
		Name:   "read_file",
		Params: json.RawMessage(`{"path": "/foo/bar.go"}`),
	}

	suggestion := recovery.SuggestFix(os.ErrNotExist, call)

	if suggestion == "" {
		t.Error("SuggestFix() should return suggestion for file not found")
	}
	if !strings.Contains(suggestion, "bar.go") {
		t.Errorf("SuggestFix() should mention filename, got: %s", suggestion)
	}
}

func TestErrorRecovery_SuggestFix_PermissionDenied(t *testing.T) {
	recovery := NewErrorRecovery()

	call := ToolCall{Name: "write_file"}

	suggestion := recovery.SuggestFix(os.ErrPermission, call)

	if suggestion == "" {
		t.Error("SuggestFix() should return suggestion for permission denied")
	}
	if !strings.Contains(strings.ToLower(suggestion), "permission") {
		t.Errorf("SuggestFix() should mention permission, got: %s", suggestion)
	}
}

func TestErrorRecovery_SuggestFix_Timeout(t *testing.T) {
	recovery := NewErrorRecovery()

	tests := []struct {
		name     string
		toolName string
		wantText string
	}{
		{
			name:     "generic timeout",
			toolName: "some_tool",
			wantText: "timed out", // The message uses "timed out" not "timeout"
		},
		{
			name:     "analysis tool timeout",
			toolName: "find_entry_points",
			wantText: "scope",
		},
		{
			name:     "similarity search timeout",
			toolName: "find_similar_code",
			wantText: "limit",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			call := ToolCall{Name: tt.toolName}
			suggestion := recovery.SuggestFix(context.DeadlineExceeded, call)

			if suggestion == "" {
				t.Error("SuggestFix() should return suggestion for timeout")
			}
			if !strings.Contains(strings.ToLower(suggestion), tt.wantText) {
				t.Errorf("SuggestFix() should mention %q, got: %s", tt.wantText, suggestion)
			}
		})
	}
}

func TestErrorRecovery_SuggestFix_ValidationError(t *testing.T) {
	recovery := NewErrorRecovery()

	// Note: SuggestFix checks for "validation" in error message or errors.Is(ErrValidationFailed)
	// The error messages must contain "validation" to trigger the validation handler
	tests := []struct {
		name     string
		errMsg   string
		wantText string
	}{
		{
			name:     "required parameter",
			errMsg:   "validation: required parameter missing",
			wantText: "required",
		},
		{
			name:     "expected string",
			errMsg:   "validation: expected string but got int",
			wantText: "string",
		},
		{
			name:     "expected integer",
			errMsg:   "validation: expected integer",
			wantText: "numeric",
		},
		{
			name:     "enum validation",
			errMsg:   "validation: value not in allowed enum",
			wantText: "allowed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			call := ToolCall{Name: "test_tool"}
			err := errors.New(tt.errMsg)
			suggestion := recovery.SuggestFix(err, call)

			if suggestion == "" {
				t.Error("SuggestFix() should return suggestion for validation error")
			}
			if !strings.Contains(strings.ToLower(suggestion), tt.wantText) {
				t.Errorf("SuggestFix() should mention %q, got: %s", tt.wantText, suggestion)
			}
		})
	}
}

func TestErrorRecovery_SuggestFix_ToolNotFound(t *testing.T) {
	recovery := NewErrorRecovery()

	call := ToolCall{Name: "unknown_tool"}
	suggestion := recovery.SuggestFix(ErrToolNotFound, call)

	if suggestion == "" {
		t.Error("SuggestFix() should return suggestion for tool not found")
	}
	if !strings.Contains(suggestion, "unknown_tool") {
		t.Errorf("SuggestFix() should mention tool name, got: %s", suggestion)
	}
}

func TestErrorRecovery_SuggestFix_ContextCancelled(t *testing.T) {
	recovery := NewErrorRecovery()

	call := ToolCall{Name: "some_tool"}
	suggestion := recovery.SuggestFix(context.Canceled, call)

	if suggestion == "" {
		t.Error("SuggestFix() should return suggestion for context cancelled")
	}
	if !strings.Contains(strings.ToLower(suggestion), "cancel") {
		t.Errorf("SuggestFix() should mention cancellation, got: %s", suggestion)
	}
}

func TestErrorRecovery_SuggestFix_NetworkError(t *testing.T) {
	recovery := NewErrorRecovery()

	call := ToolCall{Name: "some_tool"}
	err := errors.New("dial tcp: connection refused")
	suggestion := recovery.SuggestFix(err, call)

	if suggestion == "" {
		t.Error("SuggestFix() should return suggestion for network error")
	}
	if !strings.Contains(strings.ToLower(suggestion), "network") {
		t.Errorf("SuggestFix() should mention network, got: %s", suggestion)
	}
}

func TestErrorRecovery_SuggestFix_JSONError(t *testing.T) {
	recovery := NewErrorRecovery()

	call := ToolCall{Name: "some_tool"}
	err := errors.New("json unmarshal error")
	suggestion := recovery.SuggestFix(err, call)

	if suggestion == "" {
		t.Error("SuggestFix() should return suggestion for JSON error")
	}
	if !strings.Contains(strings.ToLower(suggestion), "json") {
		t.Errorf("SuggestFix() should mention JSON, got: %s", suggestion)
	}
}

func TestErrorRecovery_SuggestFix_NilError(t *testing.T) {
	recovery := NewErrorRecovery()

	call := ToolCall{Name: "some_tool"}
	suggestion := recovery.SuggestFix(nil, call)

	if suggestion != "" {
		t.Errorf("SuggestFix() should return empty for nil error, got: %s", suggestion)
	}
}

func TestErrorRecovery_SuggestFix_UnknownError(t *testing.T) {
	recovery := NewErrorRecovery()

	call := ToolCall{Name: "some_tool"}
	err := errors.New("some completely unknown error type")
	suggestion := recovery.SuggestFix(err, call)

	// Unknown errors should return empty suggestion
	if suggestion != "" {
		t.Errorf("SuggestFix() should return empty for unknown error, got: %s", suggestion)
	}
}

func TestErrorRecovery_AddCustomSuggestion(t *testing.T) {
	recovery := NewErrorRecovery()

	recovery.AddCustomSuggestion("custom error pattern", "Try this custom fix")

	call := ToolCall{Name: "some_tool"}
	err := errors.New("got a custom error pattern here")
	suggestion := recovery.SuggestFix(err, call)

	if suggestion != "Try this custom fix" {
		t.Errorf("SuggestFix() = %q, want 'Try this custom fix'", suggestion)
	}
}

func TestErrorRecovery_AddCustomSuggestion_Priority(t *testing.T) {
	recovery := NewErrorRecovery()

	// Custom suggestions should take priority over built-in ones
	recovery.AddCustomSuggestion("not found", "Custom file not found message")

	call := ToolCall{Name: "read_file", Params: json.RawMessage(`{"path": "/test.go"}`)}
	err := errors.New("file not found")
	suggestion := recovery.SuggestFix(err, call)

	if suggestion != "Custom file not found message" {
		t.Errorf("Custom suggestion should take priority, got: %s", suggestion)
	}
}

func TestErrorRecovery_Analyze(t *testing.T) {
	recovery := NewErrorRecovery()

	tests := []struct {
		name          string
		err           error
		wantCategory  string
		wantRetryable bool
	}{
		{
			name:          "file not found",
			err:           os.ErrNotExist,
			wantCategory:  "file_not_found",
			wantRetryable: false,
		},
		{
			name:          "permission denied",
			err:           os.ErrPermission,
			wantCategory:  "permission_denied",
			wantRetryable: false,
		},
		{
			name:          "timeout",
			err:           context.DeadlineExceeded,
			wantCategory:  "timeout",
			wantRetryable: true,
		},
		{
			name:          "cancelled",
			err:           context.Canceled,
			wantCategory:  "cancelled",
			wantRetryable: true,
		},
		{
			name:          "network error",
			err:           errors.New("connection refused"),
			wantCategory:  "network",
			wantRetryable: true,
		},
		{
			name:          "validation",
			err:           ErrValidationFailed,
			wantCategory:  "validation",
			wantRetryable: false,
		},
		{
			name:          "tool not found",
			err:           ErrToolNotFound,
			wantCategory:  "tool_not_found",
			wantRetryable: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			call := ToolCall{Name: "test_tool"}
			result := recovery.Analyze(tt.err, call)

			if result == nil {
				t.Fatal("Analyze() returned nil")
			}

			if result.Category != tt.wantCategory {
				t.Errorf("Analyze() category = %q, want %q", result.Category, tt.wantCategory)
			}

			if result.Retryable != tt.wantRetryable {
				t.Errorf("Analyze() retryable = %v, want %v", result.Retryable, tt.wantRetryable)
			}

			if result.OriginalError == "" {
				t.Error("Analyze() OriginalError should not be empty")
			}
		})
	}
}

func TestErrorRecovery_Analyze_NilError(t *testing.T) {
	recovery := NewErrorRecovery()

	call := ToolCall{Name: "test_tool"}
	result := recovery.Analyze(nil, call)

	if result != nil {
		t.Error("Analyze() should return nil for nil error")
	}
}

func TestErrorRecovery_ConcurrentAccess(t *testing.T) {
	recovery := NewErrorRecovery()

	var wg sync.WaitGroup

	// Concurrent writes
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			recovery.AddCustomSuggestion("pattern"+string(rune('0'+n%10)), "suggestion")
		}(i)
	}

	// Concurrent reads
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			call := ToolCall{Name: "test"}
			_ = recovery.SuggestFix(errors.New("pattern5"), call)
		}()
	}

	wg.Wait()
	// If we get here without a race condition, the test passes
}
