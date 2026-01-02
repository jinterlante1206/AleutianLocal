// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.

// Package logging provides structured logging for Aleutian components.
//
// This package implements a layered logging architecture designed for
// both open source CLI usage and enterprise deployment:
//
//   - Default: stderr output for CLI compatibility (follows Unix conventions)
//   - Optional: file logging with automatic directory creation
//   - Enterprise: extensible via LogExporter interface for cloud upload
//
// # Architecture
//
// The logging system is built on Go's standard library slog package,
// with extensions for multi-destination output and enterprise export:
//
//	┌─────────────────────────────────────────────────────────────┐
//	│                         Logger                              │
//	│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────────┐ │
//	│  │   stderr    │  │  log file   │  │   LogExporter       │ │
//	│  │  (default)  │  │  (optional) │  │   (enterprise)      │ │
//	│  └─────────────┘  └─────────────┘  └─────────────────────┘ │
//	└─────────────────────────────────────────────────────────────┘
//
// # Basic Usage
//
// For simple CLI usage with stderr output:
//
//	logger := logging.Default()
//	logger.Info("starting chat", "session_id", sessionID)
//	logger.Error("request failed", "error", err)
//
// # File Logging
//
// To enable file logging alongside stderr:
//
//	logger := logging.New(logging.Config{
//	    Level:   logging.LevelInfo,
//	    LogDir:  "~/.aleutian/logs",  // Supports ~ expansion
//	    Service: "cli",
//	})
//	defer logger.Close()  // Important: flushes and closes file
//
// This creates log files named `{service}_{date}.log` in JSON format.
//
// # Enterprise Export
//
// For enterprise deployments, implement LogExporter to send logs
// to external systems (GCS, Loki, Datadog, etc.):
//
//	exporter := enterprise.NewGCSExporter(bucket, credentials)
//	logger := logging.New(logging.Config{
//	    Level:    logging.LevelInfo,
//	    Service:  "cli",
//	    Exporter: exporter,
//	})
//
// The exporter receives LogEntry structs asynchronously and should
// buffer internally for efficiency.
//
// # Log Levels
//
// Four levels are supported, matching slog conventions:
//
//   - Debug: Development troubleshooting, verbose output
//   - Info: Normal operations (request start/end, state changes)
//   - Warn: Recoverable issues (retry attempts, degraded mode)
//   - Error: Operation failures (but system continues)
//
// # Thread Safety
//
// Logger is safe for concurrent use. Internal state is protected
// by a mutex, and the underlying slog.Logger is thread-safe.
//
// # Security Considerations
//
// This package does NOT automatically redact sensitive data.
// Callers must ensure PII, tokens, and secrets are not logged:
//
//	// BAD: logs sensitive data
//	logger.Info("auth", "token", authToken)
//
//	// GOOD: log metadata only
//	logger.Info("auth", "token_present", authToken != "")
package logging

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// =============================================================================
// Log Levels
// =============================================================================

// Level represents log severity levels.
//
// Levels follow the slog convention and are ordered by severity:
// Debug < Info < Warn < Error
//
// Setting a minimum level filters out all logs below that level.
// For example, LevelWarn filters out Debug and Info messages.
type Level int

const (
	// LevelDebug is for development troubleshooting.
	// Use for verbose output that helps trace execution flow.
	// Example: "entering function", "loop iteration 5"
	LevelDebug Level = iota

	// LevelInfo is for normal operational messages.
	// Use for significant events that confirm correct operation.
	// Example: "request started", "session created", "file uploaded"
	LevelInfo

	// LevelWarn is for potentially problematic situations.
	// Use when something unexpected happened but the system can continue.
	// Example: "retry attempt 2 of 3", "using fallback value"
	LevelWarn

	// LevelError is for error conditions.
	// Use when an operation failed but the system continues.
	// Example: "request failed", "connection lost", "invalid input"
	LevelError
)

// String returns the human-readable name of the level.
//
// Returns "DEBUG", "INFO", "WARN", "ERROR", or "UNKNOWN".
func (l Level) String() string {
	switch l {
	case LevelDebug:
		return "DEBUG"
	case LevelInfo:
		return "INFO"
	case LevelWarn:
		return "WARN"
	case LevelError:
		return "ERROR"
	default:
		return "UNKNOWN"
	}
}

// toSlogLevel converts our Level to slog.Level.
//
// This internal method bridges our Level type to the standard library.
func (l Level) toSlogLevel() slog.Level {
	switch l {
	case LevelDebug:
		return slog.LevelDebug
	case LevelInfo:
		return slog.LevelInfo
	case LevelWarn:
		return slog.LevelWarn
	case LevelError:
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// =============================================================================
// Configuration
// =============================================================================

// Config configures the Logger behavior.
//
// All fields have sensible defaults. A zero-value Config creates
// a logger that writes Info+ messages to stderr in text format.
//
// Example configurations:
//
// Minimal (CLI default):
//
//	Config{}  // Info level, stderr, text format
//
// Development:
//
//	Config{
//	    Level: LevelDebug,
//	    JSON:  false,  // Human-readable
//	}
//
// Production with file logging:
//
//	Config{
//	    Level:   LevelInfo,
//	    LogDir:  "/var/log/aleutian",
//	    Service: "orchestrator",
//	    JSON:    true,
//	}
//
// Enterprise with cloud export:
//
//	Config{
//	    Level:    LevelInfo,
//	    Service:  "cli",
//	    LogDir:   "~/.aleutian/logs",
//	    Exporter: gcsExporter,
//	}
type Config struct {
	// Level sets the minimum log level.
	//
	// Messages below this level are discarded.
	// Default: LevelInfo
	Level Level

	// LogDir enables file logging to the specified directory.
	//
	// When set, logs are written to both stderr and a file.
	// The file is named "{Service}_{YYYY-MM-DD}.log" in JSON format.
	// Directory is created with 0750 permissions if it doesn't exist.
	//
	// Supports ~ for home directory expansion:
	//   "~/.aleutian/logs" -> "/home/user/.aleutian/logs"
	//
	// Default: "" (file logging disabled)
	LogDir string

	// Service identifies the component generating logs.
	//
	// This value is included in every log entry as the "service" attribute,
	// making it easy to filter logs by component in aggregated systems.
	//
	// Recommended values: "cli", "orchestrator", "rag-engine", "llm-service"
	// Default: "" (no service attribute)
	Service string

	// JSON enables JSON output format.
	//
	// When true, logs are formatted as JSON objects (machine-parseable).
	// When false, logs are formatted as human-readable text.
	//
	// Note: File logs are always JSON regardless of this setting,
	// as they're intended for machine processing.
	//
	// Default: false (text format for stderr)
	JSON bool

	// Quiet disables stderr output.
	//
	// When true, logs are only written to file (if LogDir is set)
	// and sent to the Exporter (if configured).
	//
	// Useful for daemon processes where stderr isn't monitored.
	//
	// Default: false (stderr enabled)
	Quiet bool

	// Exporter is an optional enterprise extension for log export.
	//
	// When set, log entries are also sent to the exporter asynchronously.
	// This enables cloud upload, centralized logging, or custom processing.
	//
	// The exporter should buffer internally and handle backpressure.
	// Export failures are silently ignored to not disrupt normal logging.
	//
	// This is an extension point for AleutianEnterprise.
	// Default: nil (no export)
	Exporter LogExporter
}

// =============================================================================
// Enterprise Extension Interface
// =============================================================================

// LogExporter defines the interface for enterprise log export.
//
// Implementations can upload logs to cloud storage (GCS, S3),
// send to log aggregation systems (Loki, Datadog, Splunk),
// or forward to OpenTelemetry collectors.
//
// # Implementation Requirements
//
//  1. Export should be non-blocking. Buffer entries internally
//     and flush in batches for efficiency.
//
//  2. Handle backpressure gracefully. If the buffer is full,
//     consider dropping oldest entries rather than blocking.
//
//  3. Flush should send all buffered entries before returning.
//     It's called during graceful shutdown.
//
//  4. Close should release all resources (connections, files).
//     It's called after Flush during shutdown.
//
// # Example Implementation
//
//	type GCSExporter struct {
//	    client  *storage.Client
//	    bucket  string
//	    buffer  []LogEntry
//	    mu      sync.Mutex
//	}
//
//	func (e *GCSExporter) Export(ctx context.Context, entry LogEntry) error {
//	    e.mu.Lock()
//	    e.buffer = append(e.buffer, entry)
//	    if len(e.buffer) >= 100 {
//	        go e.uploadBatch()
//	    }
//	    e.mu.Unlock()
//	    return nil
//	}
//
// This is an extension point for AleutianEnterprise.
// The open source version uses nil (no export).
type LogExporter interface {
	// Export sends a log entry to the external system.
	//
	// This method is called asynchronously for each log entry.
	// Implementations should buffer entries and batch uploads.
	//
	// Parameters:
	//   - ctx: Context for cancellation (with 1-second timeout)
	//   - entry: The log entry to export
	//
	// Returns:
	//   - error: Non-nil if export failed (logged but not propagated)
	Export(ctx context.Context, entry LogEntry) error

	// Flush ensures all buffered entries are sent.
	//
	// This method is called during graceful shutdown.
	// It should block until all pending entries are uploaded.
	//
	// Parameters:
	//   - ctx: Context for cancellation (with 5-second timeout)
	//
	// Returns:
	//   - error: Non-nil if flush failed
	Flush(ctx context.Context) error

	// Close releases resources held by the exporter.
	//
	// This method is called after Flush during shutdown.
	// It should close connections, files, and other resources.
	//
	// Returns:
	//   - error: Non-nil if cleanup failed
	Close() error
}

// LogEntry represents a structured log entry for export.
//
// This struct is passed to LogExporter implementations.
// It contains all information needed to reconstruct the log
// in the destination system.
type LogEntry struct {
	// Timestamp when the log was generated (local time)
	Timestamp time.Time

	// Level of the log (Debug, Info, Warn, Error)
	Level Level

	// Message is the primary log message
	Message string

	// Service identifies the component (from Config.Service)
	Service string

	// Attrs contains all key-value attributes
	// Keys are strings, values are any JSON-serializable type
	Attrs map[string]any
}

// =============================================================================
// Logger
// =============================================================================

// Logger provides structured logging with multi-destination output.
//
// Logger wraps slog.Logger with additional functionality:
//   - Multi-destination output (stderr + file + export)
//   - Enterprise export via LogExporter interface
//   - Proper cleanup via Close()
//
// # Thread Safety
//
// Logger is safe for concurrent use from multiple goroutines.
// All mutable state is protected by a mutex.
//
// # Resource Management
//
// Always call Close() when done with the logger to ensure
// file handles are closed and exporters are flushed:
//
//	logger := logging.New(config)
//	defer logger.Close()
//
// # Creating Child Loggers
//
// Use With() to create a logger with additional attributes:
//
//	requestLogger := logger.With("request_id", reqID, "user_id", userID)
//	requestLogger.Info("processing request")  // Includes request_id, user_id
type Logger struct {
	// slog is the underlying structured logger
	slog *slog.Logger

	// config stores the configuration for reference
	config Config

	// file is the optional log file handle (nil if file logging disabled)
	file *os.File

	// exporter is the optional enterprise log exporter
	exporter LogExporter

	// mu protects mutable state (file, exporter)
	mu sync.Mutex
}

// New creates a new Logger with the given configuration.
//
// This constructor sets up all logging destinations based on config:
//   - stderr handler (unless Quiet is true)
//   - file handler (if LogDir is set)
//   - exporter connection (if Exporter is set)
//
// The returned Logger must be closed with Close() to release resources.
//
// Parameters:
//   - config: Logger configuration (see Config for options)
//
// Returns:
//   - *Logger: Configured logger ready for use
//
// Example:
//
//	logger := logging.New(logging.Config{
//	    Level:   logging.LevelInfo,
//	    LogDir:  "~/.aleutian/logs",
//	    Service: "cli",
//	})
//	defer logger.Close()
func New(config Config) *Logger {
	var handlers []slog.Handler

	// Configure log level filter
	opts := &slog.HandlerOptions{
		Level: config.Level.toSlogLevel(),
	}

	// Add stderr handler (unless quiet mode)
	if !config.Quiet {
		var stderrHandler slog.Handler
		if config.JSON {
			stderrHandler = slog.NewJSONHandler(os.Stderr, opts)
		} else {
			stderrHandler = slog.NewTextHandler(os.Stderr, opts)
		}
		handlers = append(handlers, stderrHandler)
	}

	logger := &Logger{
		config:   config,
		exporter: config.Exporter,
	}

	// Add file handler (if LogDir specified)
	if config.LogDir != "" {
		logDir := expandPath(config.LogDir)
		if err := os.MkdirAll(logDir, 0750); err == nil {
			// Filename: {service}_{date}.log
			serviceName := config.Service
			if serviceName == "" {
				serviceName = "aleutian"
			}
			filename := fmt.Sprintf("%s_%s.log", serviceName, time.Now().Format("2006-01-02"))
			logPath := filepath.Join(logDir, filename)

			// Open file with append mode, create if not exists
			file, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0640)
			if err == nil {
				logger.file = file
				// Always use JSON for file logs (machine-parseable)
				fileHandler := slog.NewJSONHandler(file, opts)
				handlers = append(handlers, fileHandler)
			}
		}
	}

	// Create combined handler
	var handler slog.Handler
	switch len(handlers) {
	case 0:
		// Fallback: at least write to stderr
		handler = slog.NewTextHandler(os.Stderr, opts)
	case 1:
		handler = handlers[0]
	default:
		handler = &multiHandler{handlers: handlers}
	}

	// Add service attribute to all logs
	if config.Service != "" {
		handler = handler.WithAttrs([]slog.Attr{
			slog.String("service", config.Service),
		})
	}

	logger.slog = slog.New(handler)
	return logger
}

// Default returns a logger with default settings.
//
// The default configuration:
//   - Level: Info
//   - Output: stderr only
//   - Format: text (human-readable)
//   - Service: "aleutian"
//
// This is suitable for simple CLI applications that don't need
// file logging or enterprise features.
//
// Returns:
//   - *Logger: Default-configured logger
func Default() *Logger {
	return New(Config{
		Level:   LevelInfo,
		Service: "aleutian",
	})
}

// Debug logs a message at Debug level.
//
// Debug messages are for development troubleshooting and are
// typically filtered out in production (Level >= Info).
//
// Parameters:
//   - msg: The log message
//   - args: Key-value pairs of attributes (e.g., "user_id", 123)
//
// Example:
//
//	logger.Debug("entering function", "function", "SendMessage")
func (l *Logger) Debug(msg string, args ...any) {
	l.log(LevelDebug, msg, args...)
}

// Info logs a message at Info level.
//
// Info messages indicate normal operational events that confirm
// the system is working correctly.
//
// Parameters:
//   - msg: The log message
//   - args: Key-value pairs of attributes
//
// Example:
//
//	logger.Info("request completed",
//	    "request_id", reqID,
//	    "duration_ms", elapsed.Milliseconds(),
//	)
func (l *Logger) Info(msg string, args ...any) {
	l.log(LevelInfo, msg, args...)
}

// Warn logs a message at Warn level.
//
// Warn messages indicate potentially problematic situations
// that don't prevent the system from continuing.
//
// Parameters:
//   - msg: The log message
//   - args: Key-value pairs of attributes
//
// Example:
//
//	logger.Warn("retry attempt",
//	    "attempt", 2,
//	    "max_attempts", 3,
//	    "error", err.Error(),
//	)
func (l *Logger) Warn(msg string, args ...any) {
	l.log(LevelWarn, msg, args...)
}

// Error logs a message at Error level.
//
// Error messages indicate operation failures. The system continues
// but the specific operation did not succeed.
//
// Note: For fatal errors that should terminate the program,
// use Error() followed by os.Exit() or panic.
//
// Parameters:
//   - msg: The log message
//   - args: Key-value pairs of attributes
//
// Example:
//
//	logger.Error("request failed",
//	    "request_id", reqID,
//	    "error", err.Error(),
//	    "status_code", resp.StatusCode,
//	)
func (l *Logger) Error(msg string, args ...any) {
	l.log(LevelError, msg, args...)
}

// With returns a new Logger with additional attributes.
//
// The returned logger includes all attributes from the parent
// plus the new ones. This is useful for adding context that
// should appear in all subsequent logs.
//
// The parent logger is not modified.
//
// Parameters:
//   - args: Key-value pairs of attributes to add
//
// Returns:
//   - *Logger: New logger with additional attributes
//
// Example:
//
//	// Create a request-scoped logger
//	reqLogger := logger.With("request_id", reqID, "user_id", userID)
//
//	// All logs include request_id and user_id
//	reqLogger.Info("processing")
//	reqLogger.Info("completed")
func (l *Logger) With(args ...any) *Logger {
	return &Logger{
		slog:     l.slog.With(args...),
		config:   l.config,
		file:     l.file,     // Share file handle
		exporter: l.exporter, // Share exporter
	}
}

// Slog returns the underlying slog.Logger.
//
// This provides direct access to slog features not exposed
// by this wrapper, such as LogAttrs or custom Record handling.
//
// Returns:
//   - *slog.Logger: The underlying structured logger
func (l *Logger) Slog() *slog.Logger {
	return l.slog
}

// Close flushes and closes the logger.
//
// This method:
//  1. Flushes the exporter (sends buffered entries)
//  2. Closes the exporter connection
//  3. Syncs the log file (ensures all data written)
//  4. Closes the log file
//
// Always call Close when done with a logger that has file
// logging or an exporter configured:
//
//	logger := logging.New(config)
//	defer logger.Close()
//
// Returns:
//   - error: First error encountered during cleanup (others logged)
func (l *Logger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	var errs []error

	// Flush and close exporter
	if l.exporter != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := l.exporter.Flush(ctx); err != nil {
			errs = append(errs, fmt.Errorf("flush exporter: %w", err))
		}
		if err := l.exporter.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close exporter: %w", err))
		}
	}

	// Sync and close file
	if l.file != nil {
		if err := l.file.Sync(); err != nil {
			errs = append(errs, fmt.Errorf("sync log file: %w", err))
		}
		if err := l.file.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close log file: %w", err))
		}
	}

	if len(errs) > 0 {
		return errs[0]
	}
	return nil
}

// log is the internal method that writes to all destinations.
func (l *Logger) log(level Level, msg string, args ...any) {
	// Write to slog (handles stderr and file)
	switch level {
	case LevelDebug:
		l.slog.Debug(msg, args...)
	case LevelInfo:
		l.slog.Info(msg, args...)
	case LevelWarn:
		l.slog.Warn(msg, args...)
	case LevelError:
		l.slog.Error(msg, args...)
	}

	// Export to enterprise system (if configured)
	if l.exporter != nil && level >= l.config.Level {
		entry := LogEntry{
			Timestamp: time.Now(),
			Level:     level,
			Message:   msg,
			Service:   l.config.Service,
			Attrs:     argsToMap(args),
		}
		// Async export to avoid blocking the log call
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			_ = l.exporter.Export(ctx, entry) // Errors are silently dropped
		}()
	}
}

// =============================================================================
// Multi-Handler (Internal)
// =============================================================================

// multiHandler fans out log records to multiple slog handlers.
//
// This enables simultaneous output to stderr and file with
// potentially different formats (text vs JSON).
type multiHandler struct {
	handlers []slog.Handler
}

// Enabled returns true if any handler is enabled for the level.
func (h *multiHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, handler := range h.handlers {
		if handler.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

// Handle sends the record to all enabled handlers.
func (h *multiHandler) Handle(ctx context.Context, r slog.Record) error {
	for _, handler := range h.handlers {
		if handler.Enabled(ctx, r.Level) {
			if err := handler.Handle(ctx, r); err != nil {
				return err
			}
		}
	}
	return nil
}

// WithAttrs returns a new handler with additional attributes.
func (h *multiHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	handlers := make([]slog.Handler, len(h.handlers))
	for i, handler := range h.handlers {
		handlers[i] = handler.WithAttrs(attrs)
	}
	return &multiHandler{handlers: handlers}
}

// WithGroup returns a new handler with a group name.
func (h *multiHandler) WithGroup(name string) slog.Handler {
	handlers := make([]slog.Handler, len(h.handlers))
	for i, handler := range h.handlers {
		handlers[i] = handler.WithGroup(name)
	}
	return &multiHandler{handlers: handlers}
}

// =============================================================================
// Helper Functions
// =============================================================================

// expandPath expands ~ to the user's home directory.
//
// Examples:
//   - "~/.aleutian/logs" -> "/home/user/.aleutian/logs"
//   - "/var/log" -> "/var/log" (unchanged)
//   - "relative/path" -> "relative/path" (unchanged)
func expandPath(path string) string {
	if len(path) > 0 && path[0] == '~' {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[1:])
		}
	}
	return path
}

// argsToMap converts slog-style key-value args to a map.
//
// This is used for LogEntry.Attrs when exporting.
//
// Example:
//
//	argsToMap("key1", "value1", "key2", 123)
//	// Returns: map[string]any{"key1": "value1", "key2": 123}
func argsToMap(args []any) map[string]any {
	result := make(map[string]any)
	for i := 0; i < len(args)-1; i += 2 {
		if key, ok := args[i].(string); ok {
			result[key] = args[i+1]
		}
	}
	return result
}

// =============================================================================
// Built-in Exporters
// =============================================================================

// NopExporter is a no-op exporter that discards all entries.
//
// Useful for testing or when export is disabled.
type NopExporter struct{}

// Export discards the entry (no-op).
func (e *NopExporter) Export(ctx context.Context, entry LogEntry) error { return nil }

// Flush is a no-op.
func (e *NopExporter) Flush(ctx context.Context) error { return nil }

// Close is a no-op.
func (e *NopExporter) Close() error { return nil }

// Ensure NopExporter implements LogExporter
var _ LogExporter = (*NopExporter)(nil)

// BufferedExporter collects log entries in memory.
//
// Useful for testing to verify log output:
//
//	exporter := logging.NewBufferedExporter()
//	logger := logging.New(logging.Config{Exporter: exporter})
//
//	logger.Info("test message", "key", "value")
//
//	entries := exporter.Entries()
//	assert.Equal(t, "test message", entries[0].Message)
type BufferedExporter struct {
	mu      sync.Mutex
	entries []LogEntry
}

// NewBufferedExporter creates a new BufferedExporter.
func NewBufferedExporter() *BufferedExporter {
	return &BufferedExporter{
		entries: make([]LogEntry, 0, 100),
	}
}

// Export adds the entry to the buffer.
func (e *BufferedExporter) Export(ctx context.Context, entry LogEntry) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.entries = append(e.entries, entry)
	return nil
}

// Flush is a no-op (entries are already in memory).
func (e *BufferedExporter) Flush(ctx context.Context) error {
	return nil
}

// Close is a no-op.
func (e *BufferedExporter) Close() error {
	return nil
}

// Entries returns a copy of all collected entries.
//
// The returned slice is a copy; modifications don't affect
// the exporter's internal buffer.
func (e *BufferedExporter) Entries() []LogEntry {
	e.mu.Lock()
	defer e.mu.Unlock()
	result := make([]LogEntry, len(e.entries))
	copy(result, e.entries)
	return result
}

// WriterExporter writes log entries to an io.Writer.
//
// Useful for testing or directing logs to a custom destination:
//
//	var buf bytes.Buffer
//	exporter := logging.NewWriterExporter(&buf)
//	logger := logging.New(logging.Config{Exporter: exporter})
//
//	logger.Info("hello")
//	fmt.Println(buf.String())  // Contains the log entry
type WriterExporter struct {
	w  io.Writer
	mu sync.Mutex
}

// NewWriterExporter creates a new WriterExporter.
func NewWriterExporter(w io.Writer) *WriterExporter {
	return &WriterExporter{w: w}
}

// Export writes the entry to the writer.
func (e *WriterExporter) Export(ctx context.Context, entry LogEntry) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	_, err := fmt.Fprintf(e.w, "[%s] %s: %s %v\n",
		entry.Timestamp.Format(time.RFC3339),
		entry.Level,
		entry.Message,
		entry.Attrs,
	)
	return err
}

// Flush is a no-op (writes are immediate).
func (e *WriterExporter) Flush(ctx context.Context) error { return nil }

// Close is a no-op (doesn't own the writer).
func (e *WriterExporter) Close() error { return nil }
