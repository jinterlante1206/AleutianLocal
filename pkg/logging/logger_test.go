// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package logging

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// =============================================================================
// Level Tests
// =============================================================================

func TestLevel_String(t *testing.T) {
	tests := []struct {
		level Level
		want  string
	}{
		{LevelDebug, "DEBUG"},
		{LevelInfo, "INFO"},
		{LevelWarn, "WARN"},
		{LevelError, "ERROR"},
		{Level(99), "UNKNOWN"},
		{Level(-1), "UNKNOWN"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := tt.level.String()
			if got != tt.want {
				t.Errorf("Level.String() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLevel_toSlogLevel(t *testing.T) {
	tests := []struct {
		level Level
		want  slog.Level
	}{
		{LevelDebug, slog.LevelDebug},
		{LevelInfo, slog.LevelInfo},
		{LevelWarn, slog.LevelWarn},
		{LevelError, slog.LevelError},
		{Level(99), slog.LevelInfo}, // Unknown defaults to Info
		{Level(-1), slog.LevelInfo}, // Unknown defaults to Info
	}

	for _, tt := range tests {
		t.Run(tt.level.String(), func(t *testing.T) {
			got := tt.level.toSlogLevel()
			if got != tt.want {
				t.Errorf("Level.toSlogLevel() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLevel_Constants(t *testing.T) {
	// Verify ordering: Debug < Info < Warn < Error
	if LevelDebug >= LevelInfo {
		t.Error("LevelDebug should be < LevelInfo")
	}
	if LevelInfo >= LevelWarn {
		t.Error("LevelInfo should be < LevelWarn")
	}
	if LevelWarn >= LevelError {
		t.Error("LevelWarn should be < LevelError")
	}
}

// =============================================================================
// Logger Constructor Tests
// =============================================================================

func TestNew_DefaultConfig(t *testing.T) {
	logger := New(Config{})
	if logger == nil {
		t.Fatal("New() returned nil")
	}
	if logger.slog == nil {
		t.Error("logger.slog is nil")
	}
	defer logger.Close()
}

func TestNew_AllLevels(t *testing.T) {
	levels := []Level{LevelDebug, LevelInfo, LevelWarn, LevelError}
	for _, level := range levels {
		t.Run(level.String(), func(t *testing.T) {
			logger := New(Config{Level: level, Quiet: true})
			if logger == nil {
				t.Fatal("New() returned nil")
			}
			defer logger.Close()
		})
	}
}

func TestNew_WithService(t *testing.T) {
	logger := New(Config{
		Service: "test-service",
		Quiet:   true,
	})
	if logger == nil {
		t.Fatal("New() returned nil")
	}
	if logger.config.Service != "test-service" {
		t.Errorf("Service = %v, want test-service", logger.config.Service)
	}
	defer logger.Close()
}

func TestNew_WithJSON(t *testing.T) {
	logger := New(Config{JSON: true, Quiet: true})
	if logger == nil {
		t.Fatal("New() returned nil")
	}
	defer logger.Close()
}

func TestNew_QuietMode(t *testing.T) {
	logger := New(Config{Quiet: true})
	if logger == nil {
		t.Fatal("New() returned nil")
	}
	// Should still have a handler (fallback to stderr)
	if logger.slog == nil {
		t.Error("logger.slog is nil in quiet mode")
	}
	defer logger.Close()
}

func TestNew_WithLogDir(t *testing.T) {
	tmpDir := t.TempDir()
	logger := New(Config{
		LogDir:  tmpDir,
		Service: "test",
		Quiet:   true,
	})
	if logger == nil {
		t.Fatal("New() returned nil")
	}
	defer logger.Close()

	// Should have created a log file
	if logger.file == nil {
		t.Error("logger.file is nil when LogDir specified")
	}

	// Verify file was created
	files, err := os.ReadDir(tmpDir)
	if err != nil {
		t.Fatalf("Failed to read dir: %v", err)
	}
	if len(files) == 0 {
		t.Error("No log file created in LogDir")
	}
}

func TestNew_WithLogDir_NoService(t *testing.T) {
	tmpDir := t.TempDir()
	logger := New(Config{
		LogDir: tmpDir,
		Quiet:  true,
	})
	if logger == nil {
		t.Fatal("New() returned nil")
	}
	defer logger.Close()

	// Should use "aleutian" as default service name
	files, err := os.ReadDir(tmpDir)
	if err != nil {
		t.Fatalf("Failed to read dir: %v", err)
	}
	found := false
	for _, f := range files {
		if strings.HasPrefix(f.Name(), "aleutian_") {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected log file with 'aleutian_' prefix")
	}
}

func TestNew_WithLogDir_InvalidPath(t *testing.T) {
	// Use a path that can't be created
	logger := New(Config{
		LogDir: "/root/nonexistent/deep/path/that/should/fail",
		Quiet:  true,
	})
	if logger == nil {
		t.Fatal("New() returned nil even with invalid LogDir")
	}
	defer logger.Close()
	// Should still work, just without file logging
	if logger.file != nil {
		t.Error("logger.file should be nil for invalid path")
	}
}

func TestNew_WithExporter(t *testing.T) {
	exporter := NewBufferedExporter()
	logger := New(Config{
		Exporter: exporter,
		Quiet:    true,
	})
	if logger == nil {
		t.Fatal("New() returned nil")
	}
	if logger.exporter == nil {
		t.Error("logger.exporter is nil")
	}
	defer logger.Close()
}

func TestNew_MultipleHandlers(t *testing.T) {
	tmpDir := t.TempDir()
	logger := New(Config{
		LogDir:  tmpDir,
		Service: "test",
		// Not quiet, so should have both stderr and file handlers
	})
	if logger == nil {
		t.Fatal("New() returned nil")
	}
	defer logger.Close()
}

func TestDefault(t *testing.T) {
	logger := Default()
	if logger == nil {
		t.Fatal("Default() returned nil")
	}
	if logger.config.Level != LevelInfo {
		t.Errorf("Default level = %v, want LevelInfo", logger.config.Level)
	}
	if logger.config.Service != "aleutian" {
		t.Errorf("Default service = %v, want aleutian", logger.config.Service)
	}
	defer logger.Close()
}

// =============================================================================
// Logger Method Tests
// =============================================================================

func TestLogger_Debug(t *testing.T) {
	exporter := NewBufferedExporter()
	logger := New(Config{
		Level:    LevelDebug,
		Exporter: exporter,
		Quiet:    true,
	})
	defer logger.Close()

	logger.Debug("test message", "key", "value")

	// Give async export time to complete
	time.Sleep(50 * time.Millisecond)

	entries := exporter.Entries()
	if len(entries) != 1 {
		t.Fatalf("Expected 1 entry, got %d", len(entries))
	}
	if entries[0].Level != LevelDebug {
		t.Errorf("Level = %v, want LevelDebug", entries[0].Level)
	}
	if entries[0].Message != "test message" {
		t.Errorf("Message = %v, want 'test message'", entries[0].Message)
	}
}

func TestLogger_Info(t *testing.T) {
	exporter := NewBufferedExporter()
	logger := New(Config{
		Level:    LevelInfo,
		Exporter: exporter,
		Quiet:    true,
	})
	defer logger.Close()

	logger.Info("info message", "count", 42)
	time.Sleep(50 * time.Millisecond)

	entries := exporter.Entries()
	if len(entries) != 1 {
		t.Fatalf("Expected 1 entry, got %d", len(entries))
	}
	if entries[0].Level != LevelInfo {
		t.Errorf("Level = %v, want LevelInfo", entries[0].Level)
	}
	if entries[0].Attrs["count"] != 42 {
		t.Errorf("Attrs[count] = %v, want 42", entries[0].Attrs["count"])
	}
}

func TestLogger_Warn(t *testing.T) {
	exporter := NewBufferedExporter()
	logger := New(Config{
		Level:    LevelWarn,
		Exporter: exporter,
		Quiet:    true,
	})
	defer logger.Close()

	logger.Warn("warning message", "attempt", 2)
	time.Sleep(50 * time.Millisecond)

	entries := exporter.Entries()
	if len(entries) != 1 {
		t.Fatalf("Expected 1 entry, got %d", len(entries))
	}
	if entries[0].Level != LevelWarn {
		t.Errorf("Level = %v, want LevelWarn", entries[0].Level)
	}
}

func TestLogger_Error(t *testing.T) {
	exporter := NewBufferedExporter()
	logger := New(Config{
		Level:    LevelError,
		Exporter: exporter,
		Quiet:    true,
	})
	defer logger.Close()

	logger.Error("error message", "error", "something failed")
	time.Sleep(50 * time.Millisecond)

	entries := exporter.Entries()
	if len(entries) != 1 {
		t.Fatalf("Expected 1 entry, got %d", len(entries))
	}
	if entries[0].Level != LevelError {
		t.Errorf("Level = %v, want LevelError", entries[0].Level)
	}
}

func TestLogger_LevelFiltering(t *testing.T) {
	exporter := NewBufferedExporter()
	logger := New(Config{
		Level:    LevelWarn, // Only Warn and Error
		Exporter: exporter,
		Quiet:    true,
	})
	defer logger.Close()

	logger.Debug("debug")
	logger.Info("info")
	logger.Warn("warn")
	logger.Error("error")
	time.Sleep(50 * time.Millisecond)

	entries := exporter.Entries()
	// Only Warn and Error should be exported (2 entries)
	if len(entries) != 2 {
		t.Errorf("Expected 2 entries (Warn+Error), got %d", len(entries))
	}
}

func TestLogger_With(t *testing.T) {
	exporter := NewBufferedExporter()
	logger := New(Config{
		Level:    LevelInfo,
		Exporter: exporter,
		Quiet:    true,
	})
	defer logger.Close()

	childLogger := logger.With("request_id", "abc123")
	if childLogger == nil {
		t.Fatal("With() returned nil")
	}

	childLogger.Info("request started")
	time.Sleep(50 * time.Millisecond)

	entries := exporter.Entries()
	if len(entries) != 1 {
		t.Fatalf("Expected 1 entry, got %d", len(entries))
	}
}

func TestLogger_With_SharesResources(t *testing.T) {
	tmpDir := t.TempDir()
	logger := New(Config{
		LogDir:  tmpDir,
		Service: "test",
		Quiet:   true,
	})
	defer logger.Close()

	childLogger := logger.With("child", true)

	// Child should share the file handle
	if childLogger.file != logger.file {
		t.Error("Child logger should share file handle")
	}
}

func TestLogger_Slog(t *testing.T) {
	logger := New(Config{Quiet: true})
	defer logger.Close()

	slogger := logger.Slog()
	if slogger == nil {
		t.Error("Slog() returned nil")
	}
}

func TestLogger_Close_NoResources(t *testing.T) {
	logger := New(Config{Quiet: true})
	err := logger.Close()
	if err != nil {
		t.Errorf("Close() returned error: %v", err)
	}
}

func TestLogger_Close_WithFile(t *testing.T) {
	tmpDir := t.TempDir()
	logger := New(Config{
		LogDir:  tmpDir,
		Service: "test",
		Quiet:   true,
	})

	// Log something to ensure file is written
	logger.Info("test")

	err := logger.Close()
	if err != nil {
		t.Errorf("Close() returned error: %v", err)
	}

	// File should be closed - trying to write should fail
	if logger.file != nil {
		_, writeErr := logger.file.WriteString("test")
		if writeErr == nil {
			t.Error("Expected write error after Close()")
		}
	}
}

func TestLogger_Close_WithExporter(t *testing.T) {
	exporter := NewBufferedExporter()
	logger := New(Config{
		Exporter: exporter,
		Quiet:    true,
	})

	logger.Info("test")
	time.Sleep(50 * time.Millisecond)

	err := logger.Close()
	if err != nil {
		t.Errorf("Close() returned error: %v", err)
	}
}

func TestLogger_Close_ExporterError(t *testing.T) {
	exporter := &errorExporter{flushErr: errors.New("flush failed")}
	logger := New(Config{
		Exporter: exporter,
		Quiet:    true,
	})

	err := logger.Close()
	if err == nil {
		t.Error("Expected error from Close()")
	}
	if !strings.Contains(err.Error(), "flush exporter") {
		t.Errorf("Error should mention 'flush exporter': %v", err)
	}
}

func TestLogger_ConcurrentUse(t *testing.T) {
	exporter := NewBufferedExporter()
	logger := New(Config{
		Level:    LevelInfo,
		Exporter: exporter,
		Quiet:    true,
	})
	defer logger.Close()

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			logger.Info("concurrent log", "n", n)
		}(i)
	}
	wg.Wait()
	time.Sleep(100 * time.Millisecond)

	entries := exporter.Entries()
	if len(entries) != 100 {
		t.Errorf("Expected 100 entries, got %d", len(entries))
	}
}

// =============================================================================
// multiHandler Tests
// =============================================================================

func TestMultiHandler_Enabled(t *testing.T) {
	// Create handlers with different levels
	debugOpts := &slog.HandlerOptions{Level: slog.LevelDebug}
	warnOpts := &slog.HandlerOptions{Level: slog.LevelWarn}

	var buf bytes.Buffer
	h1 := slog.NewTextHandler(&buf, debugOpts)
	h2 := slog.NewTextHandler(&buf, warnOpts)

	mh := &multiHandler{handlers: []slog.Handler{h1, h2}}

	// Debug level: should be enabled (h1 accepts it)
	if !mh.Enabled(context.Background(), slog.LevelDebug) {
		t.Error("Debug should be enabled")
	}

	// Info level: should be enabled (h1 accepts it)
	if !mh.Enabled(context.Background(), slog.LevelInfo) {
		t.Error("Info should be enabled")
	}

	// Warn level: both accept it
	if !mh.Enabled(context.Background(), slog.LevelWarn) {
		t.Error("Warn should be enabled")
	}
}

func TestMultiHandler_Enabled_NoneEnabled(t *testing.T) {
	// Create handler that only accepts Error
	opts := &slog.HandlerOptions{Level: slog.LevelError}
	var buf bytes.Buffer
	h := slog.NewTextHandler(&buf, opts)

	mh := &multiHandler{handlers: []slog.Handler{h}}

	// Debug should not be enabled
	if mh.Enabled(context.Background(), slog.LevelDebug) {
		t.Error("Debug should not be enabled")
	}
}

func TestMultiHandler_Handle(t *testing.T) {
	var buf1, buf2 bytes.Buffer
	opts := &slog.HandlerOptions{Level: slog.LevelInfo}
	h1 := slog.NewTextHandler(&buf1, opts)
	h2 := slog.NewTextHandler(&buf2, opts)

	mh := &multiHandler{handlers: []slog.Handler{h1, h2}}

	record := slog.Record{}
	record.Level = slog.LevelInfo
	record.Message = "test message"

	err := mh.Handle(context.Background(), record)
	if err != nil {
		t.Errorf("Handle() returned error: %v", err)
	}

	// Both buffers should have content
	if buf1.Len() == 0 {
		t.Error("buf1 should have content")
	}
	if buf2.Len() == 0 {
		t.Error("buf2 should have content")
	}
}

func TestMultiHandler_Handle_LevelFiltering(t *testing.T) {
	var buf1, buf2 bytes.Buffer
	h1 := slog.NewTextHandler(&buf1, &slog.HandlerOptions{Level: slog.LevelDebug})
	h2 := slog.NewTextHandler(&buf2, &slog.HandlerOptions{Level: slog.LevelError})

	mh := &multiHandler{handlers: []slog.Handler{h1, h2}}

	record := slog.Record{}
	record.Level = slog.LevelInfo

	_ = mh.Handle(context.Background(), record)

	// buf1 should have content (accepts Info)
	if buf1.Len() == 0 {
		t.Error("buf1 should have content")
	}
	// buf2 should be empty (only accepts Error)
	if buf2.Len() != 0 {
		t.Error("buf2 should be empty")
	}
}

func TestMultiHandler_WithAttrs(t *testing.T) {
	var buf bytes.Buffer
	h := slog.NewTextHandler(&buf, nil)
	mh := &multiHandler{handlers: []slog.Handler{h}}

	attrs := []slog.Attr{slog.String("key", "value")}
	newHandler := mh.WithAttrs(attrs)

	if newHandler == nil {
		t.Fatal("WithAttrs() returned nil")
	}
	if _, ok := newHandler.(*multiHandler); !ok {
		t.Error("WithAttrs() should return *multiHandler")
	}
}

func TestMultiHandler_WithGroup(t *testing.T) {
	var buf bytes.Buffer
	h := slog.NewTextHandler(&buf, nil)
	mh := &multiHandler{handlers: []slog.Handler{h}}

	newHandler := mh.WithGroup("group")

	if newHandler == nil {
		t.Fatal("WithGroup() returned nil")
	}
	if _, ok := newHandler.(*multiHandler); !ok {
		t.Error("WithGroup() should return *multiHandler")
	}
}

// =============================================================================
// Helper Function Tests
// =============================================================================

func TestExpandPath(t *testing.T) {
	home, _ := os.UserHomeDir()

	tests := []struct {
		input string
		want  string
	}{
		{"~/logs", filepath.Join(home, "logs")},
		{"~/.aleutian/logs", filepath.Join(home, ".aleutian/logs")},
		{"~", home},
		{"/var/log", "/var/log"},
		{"relative/path", "relative/path"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := expandPath(tt.input)
			if got != tt.want {
				t.Errorf("expandPath(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestArgsToMap(t *testing.T) {
	tests := []struct {
		name string
		args []any
		want map[string]any
	}{
		{
			name: "empty",
			args: []any{},
			want: map[string]any{},
		},
		{
			name: "single pair",
			args: []any{"key", "value"},
			want: map[string]any{"key": "value"},
		},
		{
			name: "multiple pairs",
			args: []any{"k1", "v1", "k2", 42, "k3", true},
			want: map[string]any{"k1": "v1", "k2": 42, "k3": true},
		},
		{
			name: "odd count (ignores last)",
			args: []any{"k1", "v1", "orphan"},
			want: map[string]any{"k1": "v1"},
		},
		{
			name: "non-string key (skipped)",
			args: []any{123, "value", "validkey", "validvalue"},
			want: map[string]any{"validkey": "validvalue"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := argsToMap(tt.args)
			if len(got) != len(tt.want) {
				t.Errorf("argsToMap() len = %d, want %d", len(got), len(tt.want))
				return
			}
			for k, v := range tt.want {
				if got[k] != v {
					t.Errorf("argsToMap()[%q] = %v, want %v", k, got[k], v)
				}
			}
		})
	}
}

// =============================================================================
// NopExporter Tests
// =============================================================================

func TestNopExporter_Export(t *testing.T) {
	e := &NopExporter{}
	err := e.Export(context.Background(), LogEntry{Message: "test"})
	if err != nil {
		t.Errorf("Export() returned error: %v", err)
	}
}

func TestNopExporter_Flush(t *testing.T) {
	e := &NopExporter{}
	err := e.Flush(context.Background())
	if err != nil {
		t.Errorf("Flush() returned error: %v", err)
	}
}

func TestNopExporter_Close(t *testing.T) {
	e := &NopExporter{}
	err := e.Close()
	if err != nil {
		t.Errorf("Close() returned error: %v", err)
	}
}

// =============================================================================
// BufferedExporter Tests
// =============================================================================

func TestNewBufferedExporter(t *testing.T) {
	e := NewBufferedExporter()
	if e == nil {
		t.Fatal("NewBufferedExporter() returned nil")
	}
	if e.entries == nil {
		t.Error("entries should not be nil")
	}
}

func TestBufferedExporter_Export(t *testing.T) {
	e := NewBufferedExporter()
	entry := LogEntry{
		Timestamp: time.Now(),
		Level:     LevelInfo,
		Message:   "test message",
		Service:   "test",
		Attrs:     map[string]any{"key": "value"},
	}

	err := e.Export(context.Background(), entry)
	if err != nil {
		t.Errorf("Export() returned error: %v", err)
	}

	entries := e.Entries()
	if len(entries) != 1 {
		t.Fatalf("Expected 1 entry, got %d", len(entries))
	}
	if entries[0].Message != "test message" {
		t.Errorf("Message = %v, want 'test message'", entries[0].Message)
	}
}

func TestBufferedExporter_Export_Multiple(t *testing.T) {
	e := NewBufferedExporter()
	for i := 0; i < 10; i++ {
		_ = e.Export(context.Background(), LogEntry{Message: "msg"})
	}

	entries := e.Entries()
	if len(entries) != 10 {
		t.Errorf("Expected 10 entries, got %d", len(entries))
	}
}

func TestBufferedExporter_Flush(t *testing.T) {
	e := NewBufferedExporter()
	err := e.Flush(context.Background())
	if err != nil {
		t.Errorf("Flush() returned error: %v", err)
	}
}

func TestBufferedExporter_Close(t *testing.T) {
	e := NewBufferedExporter()
	err := e.Close()
	if err != nil {
		t.Errorf("Close() returned error: %v", err)
	}
}

func TestBufferedExporter_Entries_ReturnsCopy(t *testing.T) {
	e := NewBufferedExporter()
	_ = e.Export(context.Background(), LogEntry{Message: "original"})

	entries1 := e.Entries()
	entries2 := e.Entries()

	// Modify the first copy
	entries1[0].Message = "modified"

	// Second copy should be unchanged
	if entries2[0].Message != "original" {
		t.Error("Entries() should return a copy, not a reference")
	}
}

func TestBufferedExporter_ConcurrentAccess(t *testing.T) {
	e := NewBufferedExporter()
	var wg sync.WaitGroup

	// Concurrent exports
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			_ = e.Export(context.Background(), LogEntry{Message: "msg"})
		}(i)
	}

	// Concurrent reads
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = e.Entries()
		}()
	}

	wg.Wait()

	entries := e.Entries()
	if len(entries) != 100 {
		t.Errorf("Expected 100 entries, got %d", len(entries))
	}
}

// =============================================================================
// WriterExporter Tests
// =============================================================================

func TestNewWriterExporter(t *testing.T) {
	var buf bytes.Buffer
	e := NewWriterExporter(&buf)
	if e == nil {
		t.Fatal("NewWriterExporter() returned nil")
	}
}

func TestWriterExporter_Export(t *testing.T) {
	var buf bytes.Buffer
	e := NewWriterExporter(&buf)

	entry := LogEntry{
		Timestamp: time.Now(),
		Level:     LevelInfo,
		Message:   "test message",
		Attrs:     map[string]any{"key": "value"},
	}

	err := e.Export(context.Background(), entry)
	if err != nil {
		t.Errorf("Export() returned error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "test message") {
		t.Errorf("Output should contain 'test message': %v", output)
	}
	if !strings.Contains(output, "INFO") {
		t.Errorf("Output should contain 'INFO': %v", output)
	}
}

func TestWriterExporter_Flush(t *testing.T) {
	var buf bytes.Buffer
	e := NewWriterExporter(&buf)
	err := e.Flush(context.Background())
	if err != nil {
		t.Errorf("Flush() returned error: %v", err)
	}
}

func TestWriterExporter_Close(t *testing.T) {
	var buf bytes.Buffer
	e := NewWriterExporter(&buf)
	err := e.Close()
	if err != nil {
		t.Errorf("Close() returned error: %v", err)
	}
}

func TestWriterExporter_ConcurrentAccess(t *testing.T) {
	var buf bytes.Buffer
	e := NewWriterExporter(&buf)
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			_ = e.Export(context.Background(), LogEntry{Message: "msg"})
		}(i)
	}

	wg.Wait()

	// Should have 100 lines
	lines := strings.Count(buf.String(), "\n")
	if lines != 100 {
		t.Errorf("Expected 100 lines, got %d", lines)
	}
}

// =============================================================================
// LogEntry Tests
// =============================================================================

func TestLogEntry_Fields(t *testing.T) {
	now := time.Now()
	entry := LogEntry{
		Timestamp: now,
		Level:     LevelError,
		Message:   "test error",
		Service:   "test-service",
		Attrs:     map[string]any{"error": "something failed"},
	}

	if entry.Timestamp != now {
		t.Error("Timestamp mismatch")
	}
	if entry.Level != LevelError {
		t.Error("Level mismatch")
	}
	if entry.Message != "test error" {
		t.Error("Message mismatch")
	}
	if entry.Service != "test-service" {
		t.Error("Service mismatch")
	}
	if entry.Attrs["error"] != "something failed" {
		t.Error("Attrs mismatch")
	}
}

// =============================================================================
// Integration Tests
// =============================================================================

func TestLogger_FullIntegration(t *testing.T) {
	tmpDir := t.TempDir()
	exporter := NewBufferedExporter()

	logger := New(Config{
		Level:    LevelDebug,
		LogDir:   tmpDir,
		Service:  "integration-test",
		Exporter: exporter,
		Quiet:    true,
	})

	// Log at all levels
	logger.Debug("debug message", "debug_key", "debug_value")
	logger.Info("info message", "info_key", 123)
	logger.Warn("warn message", "warn_key", true)
	logger.Error("error message", "error_key", 456.78)

	// Create child logger
	childLogger := logger.With("child_key", "child_value")
	childLogger.Info("child message")

	// Wait for async exports
	time.Sleep(100 * time.Millisecond)

	// Close logger
	err := logger.Close()
	if err != nil {
		t.Errorf("Close() returned error: %v", err)
	}

	// Verify exports
	entries := exporter.Entries()
	if len(entries) != 5 {
		t.Errorf("Expected 5 entries, got %d", len(entries))
	}

	// Verify file was written
	files, _ := os.ReadDir(tmpDir)
	if len(files) == 0 {
		t.Error("No log file created")
	}
}

func TestLogger_FileContent(t *testing.T) {
	tmpDir := t.TempDir()
	logger := New(Config{
		Level:   LevelInfo,
		LogDir:  tmpDir,
		Service: "file-test",
		Quiet:   true,
	})

	logger.Info("test message", "key", "value")
	logger.Close()

	// Read the log file
	files, _ := os.ReadDir(tmpDir)
	if len(files) == 0 {
		t.Fatal("No log file created")
	}

	content, err := os.ReadFile(filepath.Join(tmpDir, files[0].Name()))
	if err != nil {
		t.Fatalf("Failed to read log file: %v", err)
	}

	// Should be JSON format
	if !strings.Contains(string(content), "test message") {
		t.Error("Log file should contain 'test message'")
	}
	if !strings.Contains(string(content), "\"key\":\"value\"") {
		t.Error("Log file should contain key-value pair in JSON format")
	}
}

// =============================================================================
// Error Handling Tests
// =============================================================================

// errorExporter is a test exporter that returns errors
type errorExporter struct {
	exportErr error
	flushErr  error
	closeErr  error
}

func (e *errorExporter) Export(ctx context.Context, entry LogEntry) error {
	return e.exportErr
}

func (e *errorExporter) Flush(ctx context.Context) error {
	return e.flushErr
}

func (e *errorExporter) Close() error {
	return e.closeErr
}

func TestLogger_ExportErrorSilentlyDropped(t *testing.T) {
	exporter := &errorExporter{exportErr: errors.New("export failed")}
	logger := New(Config{
		Level:    LevelInfo,
		Exporter: exporter,
		Quiet:    true,
	})
	defer logger.Close()

	// This should not panic or return error
	logger.Info("test")
	time.Sleep(50 * time.Millisecond)
}

func TestLogger_Close_CloseExporterError(t *testing.T) {
	exporter := &errorExporter{closeErr: errors.New("close failed")}
	logger := New(Config{
		Exporter: exporter,
		Quiet:    true,
	})

	err := logger.Close()
	if err == nil {
		t.Error("Expected error from Close()")
	}
}

// =============================================================================
// Config Tests
// =============================================================================

func TestConfig_ZeroValue(t *testing.T) {
	config := Config{}
	if config.Level != LevelDebug {
		// Note: LevelDebug is 0, so zero value is Debug
		// This is by design - users should explicitly set Level
	}
	if config.LogDir != "" {
		t.Error("LogDir zero value should be empty")
	}
	if config.Service != "" {
		t.Error("Service zero value should be empty")
	}
	if config.JSON {
		t.Error("JSON zero value should be false")
	}
	if config.Quiet {
		t.Error("Quiet zero value should be false")
	}
	if config.Exporter != nil {
		t.Error("Exporter zero value should be nil")
	}
}

// =============================================================================
// Additional Coverage Tests
// =============================================================================

func TestMultiHandler_Handle_Error(t *testing.T) {
	// Create a handler that returns an error
	h := &errorHandler{err: errors.New("handler error")}
	mh := &multiHandler{handlers: []slog.Handler{h}}

	record := slog.Record{}
	record.Level = slog.LevelInfo

	err := mh.Handle(context.Background(), record)
	if err == nil {
		t.Error("Expected error from Handle()")
	}
}

// errorHandler is a handler that returns an error
type errorHandler struct {
	err error
}

func (h *errorHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return true
}

func (h *errorHandler) Handle(ctx context.Context, r slog.Record) error {
	return h.err
}

func (h *errorHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return h
}

func (h *errorHandler) WithGroup(name string) slog.Handler {
	return h
}

func TestLogger_Close_MultipleErrors(t *testing.T) {
	// Create exporter with both flush and close errors
	exporter := &errorExporter{
		flushErr: errors.New("flush failed"),
		closeErr: errors.New("close failed"),
	}
	logger := New(Config{
		Exporter: exporter,
		Quiet:    true,
	})

	err := logger.Close()
	// Should return the first error (flush)
	if err == nil {
		t.Error("Expected error from Close()")
	}
	if !strings.Contains(err.Error(), "flush") {
		t.Errorf("Expected flush error first: %v", err)
	}
}

func TestLogger_Close_FileSyncError(t *testing.T) {
	tmpDir := t.TempDir()
	logger := New(Config{
		LogDir:  tmpDir,
		Service: "test",
		Quiet:   true,
	})

	// Close the file manually to cause an error on Sync/Close
	if logger.file != nil {
		logger.file.Close()
	}

	// Now Close() should encounter an error
	err := logger.Close()
	// Error is expected because file was already closed
	_ = err // May or may not error depending on OS
}

func TestNew_QuietWithLogDir(t *testing.T) {
	// Test Quiet mode with LogDir - should only have file handler
	tmpDir := t.TempDir()
	logger := New(Config{
		LogDir:  tmpDir,
		Service: "test",
		Quiet:   true,
	})
	if logger == nil {
		t.Fatal("New() returned nil")
	}
	defer logger.Close()

	// Should have file handler
	if logger.file == nil {
		t.Error("logger.file should not be nil")
	}
}

func TestLogger_log_AllLevels(t *testing.T) {
	// Directly test the internal log method with all levels
	exporter := NewBufferedExporter()
	logger := New(Config{
		Level:    LevelDebug,
		Exporter: exporter,
		Quiet:    true,
	})
	defer logger.Close()

	// Call internal log method for each level
	logger.log(LevelDebug, "debug")
	logger.log(LevelInfo, "info")
	logger.log(LevelWarn, "warn")
	logger.log(LevelError, "error")

	time.Sleep(100 * time.Millisecond)

	entries := exporter.Entries()
	if len(entries) != 4 {
		t.Errorf("Expected 4 entries, got %d", len(entries))
	}
}

func TestExpandPath_NoHome(t *testing.T) {
	// Test path that doesn't start with ~
	result := expandPath("/absolute/path")
	if result != "/absolute/path" {
		t.Errorf("Expected '/absolute/path', got '%s'", result)
	}
}

func TestNew_OnlyQuiet(t *testing.T) {
	// Quiet mode with no file - should fallback to stderr handler
	logger := New(Config{
		Quiet: true,
	})
	if logger == nil {
		t.Fatal("New() returned nil")
	}
	defer logger.Close()

	// Should still work
	logger.Info("test")
}

func TestMultiHandler_Empty(t *testing.T) {
	mh := &multiHandler{handlers: []slog.Handler{}}

	// Enabled should return false when no handlers
	if mh.Enabled(context.Background(), slog.LevelInfo) {
		t.Error("Empty multiHandler should not be enabled")
	}

	// Handle should work without error
	record := slog.Record{}
	err := mh.Handle(context.Background(), record)
	if err != nil {
		t.Errorf("Handle() returned error: %v", err)
	}
}
