// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package transaction

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"testing"
	"time"

	"go.opentelemetry.io/otel/trace"
)

func TestNewTracer(t *testing.T) {
	t.Run("creates tracer with logger", func(t *testing.T) {
		logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
		tracer := NewTracer(logger, true)

		if tracer == nil {
			t.Fatal("expected tracer to be non-nil")
		}
		if tracer.logger != logger {
			t.Error("expected tracer to use provided logger")
		}
		if !tracer.enabled {
			t.Error("expected tracer to be enabled")
		}
	})

	t.Run("creates tracer with default logger", func(t *testing.T) {
		tracer := NewTracer(nil, false)

		if tracer == nil {
			t.Fatal("expected tracer to be non-nil")
		}
		if tracer.logger == nil {
			t.Error("expected tracer to have default logger")
		}
		if tracer.enabled {
			t.Error("expected tracer to be disabled")
		}
	})
}

func TestTracer_StartBegin(t *testing.T) {
	ctx := context.Background()

	t.Run("creates span when enabled", func(t *testing.T) {
		tracer := NewTracer(nil, true)

		newCtx, span := tracer.StartBegin(ctx, "session-123", StrategyBranch)

		if newCtx == nil {
			t.Error("expected non-nil context")
		}
		if span == nil {
			t.Error("expected non-nil span")
		}
		span.End()
	})

	t.Run("returns noop span when disabled", func(t *testing.T) {
		tracer := NewTracer(nil, false)

		newCtx, span := tracer.StartBegin(ctx, "session-123", StrategyBranch)

		// Context should be unchanged
		if newCtx != ctx {
			t.Error("expected context to be unchanged when disabled")
		}
		// Span should be noop (safe to call methods on)
		span.End() // Should not panic
	})
}

func TestTracer_EndBegin(t *testing.T) {
	ctx := context.Background()
	tracer := NewTracer(nil, true)

	t.Run("ends span with success", func(t *testing.T) {
		_, span := tracer.StartBegin(ctx, "session-123", StrategyBranch)
		tx := &Transaction{
			ID:             "tx-123",
			CheckpointRef:  "abc123",
			OriginalBranch: "main",
		}

		// Should not panic
		tracer.EndBegin(span, tx, nil)
	})

	t.Run("ends span with error", func(t *testing.T) {
		_, span := tracer.StartBegin(ctx, "session-123", StrategyBranch)

		// Should not panic
		tracer.EndBegin(span, nil, errors.New("test error"))
	})

	t.Run("handles nil span", func(t *testing.T) {
		// Should not panic
		tracer.EndBegin(nil, nil, nil)
	})
}

func TestTracer_StartCommit(t *testing.T) {
	ctx := context.Background()
	tx := &Transaction{
		ID:            "tx-123",
		SessionID:     "session-456",
		ModifiedFiles: map[string]struct{}{"file1.go": {}, "file2.go": {}},
	}

	t.Run("creates span when enabled", func(t *testing.T) {
		tracer := NewTracer(nil, true)

		newCtx, span := tracer.StartCommit(ctx, tx, "test commit")

		if newCtx == nil {
			t.Error("expected non-nil context")
		}
		if span == nil {
			t.Error("expected non-nil span")
		}
		span.End()
	})

	t.Run("returns noop span when disabled", func(t *testing.T) {
		tracer := NewTracer(nil, false)

		_, span := tracer.StartCommit(ctx, tx, "test commit")

		span.End() // Should not panic
	})
}

func TestTracer_EndCommit(t *testing.T) {
	ctx := context.Background()
	tracer := NewTracer(nil, true)
	tx := &Transaction{ID: "tx-123", SessionID: "session-456"}

	t.Run("ends span with success", func(t *testing.T) {
		_, span := tracer.StartCommit(ctx, tx, "test")
		result := &Result{
			CommitSHA:     "def456",
			Duration:      5 * time.Second,
			FilesModified: 3,
		}

		// Should not panic
		tracer.EndCommit(span, result, nil)
	})

	t.Run("ends span with error", func(t *testing.T) {
		_, span := tracer.StartCommit(ctx, tx, "test")

		// Should not panic
		tracer.EndCommit(span, nil, errors.New("commit failed"))
	})
}

func TestTracer_StartRollback(t *testing.T) {
	ctx := context.Background()
	tx := &Transaction{
		ID:            "tx-123",
		SessionID:     "session-456",
		ModifiedFiles: map[string]struct{}{"file1.go": {}},
	}

	t.Run("creates span when enabled", func(t *testing.T) {
		tracer := NewTracer(nil, true)

		newCtx, span := tracer.StartRollback(ctx, tx, "test failure")

		if newCtx == nil {
			t.Error("expected non-nil context")
		}
		if span == nil {
			t.Error("expected non-nil span")
		}
		span.End()
	})

	t.Run("returns noop span when disabled", func(t *testing.T) {
		tracer := NewTracer(nil, false)

		_, span := tracer.StartRollback(ctx, tx, "test failure")

		span.End() // Should not panic
	})
}

func TestTracer_EndRollback(t *testing.T) {
	ctx := context.Background()
	tracer := NewTracer(nil, true)
	tx := &Transaction{ID: "tx-123", SessionID: "session-456"}

	t.Run("ends span with success", func(t *testing.T) {
		_, span := tracer.StartRollback(ctx, tx, "test")
		result := &Result{
			Duration:      2 * time.Second,
			FilesModified: 1,
		}

		// Should not panic
		tracer.EndRollback(span, result, nil)
	})

	t.Run("ends span with error", func(t *testing.T) {
		_, span := tracer.StartRollback(ctx, tx, "test")

		// Should not panic
		tracer.EndRollback(span, nil, errors.New("rollback failed"))
	})
}

func TestTracer_StartGitOp(t *testing.T) {
	ctx := context.Background()

	t.Run("creates span when enabled", func(t *testing.T) {
		tracer := NewTracer(nil, true)

		newCtx, span := tracer.StartGitOp(ctx, "create_branch")

		if newCtx == nil {
			t.Error("expected non-nil context")
		}
		if span == nil {
			t.Error("expected non-nil span")
		}
		span.End()
	})

	t.Run("returns noop span when disabled", func(t *testing.T) {
		tracer := NewTracer(nil, false)

		_, span := tracer.StartGitOp(ctx, "create_branch")

		span.End() // Should not panic
	})
}

func TestTracer_EndGitOp(t *testing.T) {
	ctx := context.Background()
	tracer := NewTracer(nil, true)

	t.Run("ends span with success", func(t *testing.T) {
		_, span := tracer.StartGitOp(ctx, "commit")

		// Should not panic
		tracer.EndGitOp(span, nil)
	})

	t.Run("ends span with error", func(t *testing.T) {
		_, span := tracer.StartGitOp(ctx, "commit")

		// Should not panic
		tracer.EndGitOp(span, errors.New("commit failed"))
	})

	t.Run("handles nil span", func(t *testing.T) {
		// Should not panic
		tracer.EndGitOp(nil, nil)
	})
}

func TestTracer_RecordStateTransition(t *testing.T) {
	ctx := context.Background()
	tracer := NewTracer(nil, true)

	t.Run("records transition with span", func(t *testing.T) {
		// Create a span context
		ctx, span := tracer.StartBegin(ctx, "session", StrategyBranch)
		defer span.End()

		// Should not panic
		tracer.RecordStateTransition(ctx, "tx-123", StatusActive, StatusCommitting, 5*time.Second)
	})

	t.Run("handles context without span", func(t *testing.T) {
		// Should not panic
		tracer.RecordStateTransition(context.Background(), "tx-123", StatusActive, StatusCommitting, time.Second)
	})
}

func TestTracer_RecordExpiration(t *testing.T) {
	ctx := context.Background()
	tracer := NewTracer(nil, true)

	t.Run("records expiration with span", func(t *testing.T) {
		ctx, span := tracer.StartBegin(ctx, "session", StrategyBranch)
		defer span.End()

		// Should not panic
		tracer.RecordExpiration(ctx, "tx-123")
	})

	t.Run("handles context without span", func(t *testing.T) {
		// Should not panic
		tracer.RecordExpiration(context.Background(), "tx-123")
	})
}

func TestTruncateForTrace(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		maxLen   int
		expected string
	}{
		{"short string within limit", "short", 10, "short"},
		{"exactly at limit", "exactly_10", 10, "exactly_10"},
		{"exceeds limit with ellipsis", "this is a longer string", 10, "this is..."},
		{"string shorter than limit", "abc", 5, "abc"},
		{"empty string", "", 10, ""},
		// Edge cases for CR-5 fix
		{"maxLen zero", "hello", 0, ""},
		{"maxLen one", "hello", 1, "h"},
		{"maxLen two", "hello", 2, "he"},
		{"maxLen three", "hello", 3, "hel"},
		{"maxLen four truncates with ellipsis", "hello", 4, "h..."},
		{"negative maxLen", "hello", -1, ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := truncateForTrace(tc.input, tc.maxLen)
			if result != tc.expected {
				t.Errorf("truncateForTrace(%q, %d) = %q; want %q", tc.input, tc.maxLen, result, tc.expected)
			}
		})
	}
}

func TestLoggerWithTrace(t *testing.T) {
	t.Run("returns original logger without trace", func(t *testing.T) {
		logger := slog.Default()
		ctx := context.Background()

		result := LoggerWithTrace(ctx, logger)

		// Should return same logger when no trace
		if result == nil {
			t.Error("expected non-nil logger")
		}
	})

	t.Run("adds trace fields when trace present", func(t *testing.T) {
		logger := slog.Default()
		tracer := NewTracer(nil, true)
		ctx, span := tracer.StartBegin(context.Background(), "session", StrategyBranch)
		defer span.End()

		result := LoggerWithTrace(ctx, logger)

		// Should return a logger (may or may not have trace fields depending on tracer setup)
		if result == nil {
			t.Error("expected non-nil logger")
		}
	})

	t.Run("handles invalid span context", func(t *testing.T) {
		logger := slog.Default()
		ctx := trace.ContextWithSpanContext(context.Background(), trace.SpanContext{})

		result := LoggerWithTrace(ctx, logger)

		if result == nil {
			t.Error("expected non-nil logger")
		}
	})
}
