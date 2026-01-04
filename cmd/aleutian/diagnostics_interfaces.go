// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

/*
Package main provides the Distributed Health Agent interfaces for Aleutian.

The Distributed Health Agent is a first-class telemetry pipeline that transforms
the CLI from a tool into a self-reporting, self-healing platform. It solves the
fundamental problem of distributed CLI tools: "It works on my machine, why
doesn't it work on yours?"

# Architecture

The Health Agent consists of these core interfaces:

  - DiagnosticsCollector: Primary orchestrator for diagnostic collection
  - DiagnosticsFormatter: Converts diagnostic data to output formats (JSON, Text)
  - DiagnosticsStorage: Pluggable storage backends (File, GCS, Loki, Splunk)
  - DiagnosticsMetrics: Prometheus metrics export
  - DiagnosticsViewer: Retrieves and displays stored diagnostics
  - PanicRecoveryHandler: Captures diagnostics on application panic

# Design Principles

  - Every CLI invocation is a "transient microservice" that emits telemetry
  - OpenTelemetry spans enable trace-based debugging via Jaeger
  - Prometheus metrics enable fleet-wide monitoring and alerting
  - Pluggable storage supports local, cloud, and enterprise backends
  - 30-day retention policy aligns with GDPR Data Minimization

# Related Documentation

See docs/architecture/distributed_health_agent.md for the conceptual overview.
See docs/designs/pending/diagnostics_collector_architecture.md for implementation details.
*/
package main

import (
	"context"
)

// -----------------------------------------------------------------------------
// DiagnosticsCollector Interface
// -----------------------------------------------------------------------------

// DiagnosticsCollector orchestrates diagnostic collection with full observability.
//
// This is the primary interface for the Distributed Health Agent. It coordinates
// data gathering, formatting, storage, and metrics export. All operations emit
// OpenTelemetry spans for tracing.
//
// # Thread Safety
//
// Implementations must be safe for concurrent use from multiple goroutines.
// Each Collect call should be independent.
//
// # Context Handling
//
// All methods accept context.Context for cancellation and timeout support.
// Long-running operations should respect context cancellation.
type DiagnosticsCollector interface {
	// Collect gathers system diagnostics and stores them.
	//
	// # Description
	//
	// Creates an OpenTelemetry span, collects system state (OS, Podman,
	// containers), formats output (JSON/Text), stores to configured backend,
	// and exports Prometheus metrics.
	//
	// # Inputs
	//
	//   - ctx: Context for cancellation/timeout, carries trace context
	//   - opts: Configuration for this collection operation
	//
	// # Outputs
	//
	//   - *DiagnosticsResult: Contains location, trace ID, duration, size
	//   - error: Non-nil if collection fails critically
	//
	// # Examples
	//
	//   result, err := dc.Collect(ctx, CollectOptions{
	//       Reason:   "startup_failure",
	//       Details:  "compose exited with code 1",
	//       Severity: SeverityError,
	//       IncludeContainerLogs: true,
	//   })
	//   if err != nil {
	//       log.Printf("Diagnostic collection failed: %v", err)
	//   } else {
	//       log.Printf("Diagnostics saved. Trace ID: %s", result.TraceID)
	//   }
	//
	// # Limitations
	//
	//   - Container logs require containers to exist (even if stopped)
	//   - Some commands may fail if Podman is not installed
	//   - Large container logs may be truncated
	//
	// # Assumptions
	//
	//   - OpenTelemetry SDK is initialized (or gracefully degraded)
	//   - Storage backend is configured and accessible
	//   - ProcessManager is available for command execution
	Collect(ctx context.Context, opts CollectOptions) (*DiagnosticsResult, error)

	// GetLastResult returns the most recent collection result.
	//
	// # Description
	//
	// Returns the result from the last successful Collect call. Useful for
	// displaying trace IDs or locations after errors.
	//
	// # Outputs
	//
	//   - *DiagnosticsResult: Last result, or nil if no collection yet
	//
	// # Examples
	//
	//   if result := dc.GetLastResult(); result != nil {
	//       fmt.Printf("Last diagnostic: %s\n", result.Location)
	//   }
	//
	// # Thread Safety
	//
	// Safe to call concurrently with Collect.
	GetLastResult() *DiagnosticsResult

	// SetStorage configures the storage backend.
	//
	// # Description
	//
	// Replaces the current storage backend. Use this to switch between
	// local file storage and cloud/enterprise backends.
	//
	// # Inputs
	//
	//   - storage: New storage backend implementation
	//
	// # Examples
	//
	//   dc.SetStorage(NewGCSDiagnosticsStorage(bucket, credentials))
	//
	// # Thread Safety
	//
	// Should not be called concurrently with Collect.
	SetStorage(storage DiagnosticsStorage)

	// SetFormatter configures the output formatter.
	//
	// # Description
	//
	// Replaces the current output formatter. JSON is default for machine
	// parsing; Text is available for human readability.
	//
	// # Inputs
	//
	//   - formatter: New formatter implementation
	//
	// # Examples
	//
	//   dc.SetFormatter(NewTextDiagnosticsFormatter())
	//
	// # Thread Safety
	//
	// Should not be called concurrently with Collect.
	SetFormatter(formatter DiagnosticsFormatter)
}

// -----------------------------------------------------------------------------
// DiagnosticsFormatter Interface
// -----------------------------------------------------------------------------

// DiagnosticsFormatter converts diagnostic data to output format.
//
// Implementations handle specific output formats optimized for their use case:
//
//   - JSON: Machine parsing, Grafana/Loki ingestion, structured queries
//   - Text: Human readability, terminal display, quick inspection
//
// # Thread Safety
//
// Implementations must be safe for concurrent use. Format should be a pure
// function with no side effects.
type DiagnosticsFormatter interface {
	// Format converts diagnostic data to the target format.
	//
	// # Description
	//
	// Transforms the structured DiagnosticsData into the target format.
	// Output should be complete and ready for storage.
	//
	// # Inputs
	//
	//   - data: Collected diagnostic information to format
	//
	// # Outputs
	//
	//   - []byte: Formatted output (JSON, plain text, etc.)
	//   - error: Non-nil if formatting fails (e.g., marshaling error)
	//
	// # Examples
	//
	//   output, err := formatter.Format(data)
	//   if err != nil {
	//       return fmt.Errorf("format failed: %w", err)
	//   }
	//   storage.Store(ctx, output, metadata)
	//
	// # Limitations
	//
	//   - Large data may result in large output
	//   - Binary data in logs may not format cleanly as text
	//
	// # Assumptions
	//
	//   - Input data is valid and complete
	Format(data *DiagnosticsData) ([]byte, error)

	// ContentType returns the MIME type for the format.
	//
	// # Description
	//
	// Returns the appropriate MIME type for HTTP headers or storage metadata.
	//
	// # Outputs
	//
	//   - string: MIME type (e.g., "application/json", "text/plain")
	//
	// # Examples
	//
	//   metadata := StorageMetadata{
	//       ContentType: formatter.ContentType(),
	//   }
	ContentType() string

	// FileExtension returns the appropriate file extension.
	//
	// # Description
	//
	// Returns the file extension (with dot) for the format.
	//
	// # Outputs
	//
	//   - string: File extension (e.g., ".json", ".txt")
	//
	// # Examples
	//
	//   filename := fmt.Sprintf("diag-%s%s", timestamp, formatter.FileExtension())
	FileExtension() string
}

// -----------------------------------------------------------------------------
// DiagnosticsStorage Interface
// -----------------------------------------------------------------------------

// DiagnosticsStorage handles persistent storage of diagnostic data.
//
// Implementations support different backends with varying characteristics:
//
//   - FileStorage: Local filesystem (default, fast, offline-capable)
//   - GCSStorage: Google Cloud Storage (persistent, shared, auditable)
//   - LokiStorage: Stream to Loki (real-time, queryable)
//   - EnterpriseStorage: Splunk, ServiceNow (enterprise integration)
//
// # Data Retention
//
// All implementations must support automatic pruning via Prune().
// Default retention: 30 days (GDPR Data Minimization compliant).
//
// # Thread Safety
//
// Implementations must be safe for concurrent use.
type DiagnosticsStorage interface {
	// Store saves diagnostic data and returns the location.
	//
	// # Description
	//
	// Persists formatted diagnostic data to the storage backend.
	// Returns a location identifier (path or URI) for later retrieval.
	//
	// # Inputs
	//
	//   - ctx: Context with tracing span for observability
	//   - data: Formatted diagnostic data (from DiagnosticsFormatter)
	//   - metadata: Storage hints (filename, content type, tags)
	//
	// # Outputs
	//
	//   - string: Storage location (path for files, URI for cloud)
	//   - error: Non-nil if storage fails
	//
	// # Examples
	//
	//   location, err := storage.Store(ctx, jsonData, StorageMetadata{
	//       FilenameHint: "diag-20240105-100000.json",
	//       ContentType:  "application/json",
	//       Tags:         map[string]string{"severity": "error"},
	//   })
	//
	// # Limitations
	//
	//   - Cloud storage requires network connectivity
	//   - Large data may be slow to upload
	//
	// # Assumptions
	//
	//   - Storage backend is accessible and has sufficient space
	Store(ctx context.Context, data []byte, metadata StorageMetadata) (string, error)

	// Load retrieves diagnostic data by location.
	//
	// # Description
	//
	// Retrieves previously stored diagnostic data. Used by DiagnosticsViewer
	// to hydrate historical diagnostics.
	//
	// # Inputs
	//
	//   - ctx: Context for cancellation
	//   - location: Storage path or URI returned by Store
	//
	// # Outputs
	//
	//   - []byte: Raw diagnostic data
	//   - error: Non-nil if not found or read fails
	//
	// # Examples
	//
	//   data, err := storage.Load(ctx, "/path/to/diag.json")
	//   if err != nil {
	//       return fmt.Errorf("load failed: %w", err)
	//   }
	//
	// # Limitations
	//
	//   - Pruned data cannot be loaded
	//   - Cloud storage requires network connectivity
	Load(ctx context.Context, location string) ([]byte, error)

	// List returns recent diagnostic locations.
	//
	// # Description
	//
	// Returns locations of stored diagnostics, newest first.
	// Used by DiagnosticsViewer for listing historical data.
	//
	// # Inputs
	//
	//   - ctx: Context for cancellation
	//   - limit: Maximum entries to return (0 = all)
	//
	// # Outputs
	//
	//   - []string: Storage locations, newest first
	//   - error: Non-nil if listing fails
	//
	// # Examples
	//
	//   locations, err := storage.List(ctx, 20)
	//   for _, loc := range locations {
	//       fmt.Println(loc)
	//   }
	List(ctx context.Context, limit int) ([]string, error)

	// Prune removes diagnostics older than the retention period.
	//
	// # Description
	//
	// Enforces data retention policy by removing old diagnostics.
	// Should be called on startup, shutdown, and via explicit command.
	//
	// # Inputs
	//
	//   - ctx: Context for cancellation
	//
	// # Outputs
	//
	//   - int: Number of diagnostics pruned
	//   - error: Non-nil if pruning fails
	//
	// # Examples
	//
	//   pruned, err := storage.Prune(ctx)
	//   if err != nil {
	//       log.Printf("Prune failed: %v", err)
	//   } else {
	//       log.Printf("Pruned %d old diagnostics", pruned)
	//   }
	//
	// # Compliance
	//
	// Default 30-day retention aligns with:
	//   - GDPR Article 5 (Data Minimization)
	//   - NIST SP 800-92
	//   - ISO 27001
	//   - CCPA
	Prune(ctx context.Context) (int, error)

	// SetRetentionDays configures the retention period.
	//
	// # Inputs
	//
	//   - days: Retention period in days (default: 30)
	SetRetentionDays(days int)

	// GetRetentionDays returns the current retention period.
	//
	// # Outputs
	//
	//   - int: Current retention days
	GetRetentionDays() int

	// Type returns the storage backend type identifier.
	//
	// # Outputs
	//
	//   - string: Backend type (e.g., "file", "gcs", "loki", "splunk")
	Type() string
}

// -----------------------------------------------------------------------------
// DiagnosticsMetrics Interface
// -----------------------------------------------------------------------------

// DiagnosticsMetrics exports Prometheus metrics for diagnostics.
//
// # Metrics Exported
//
// Collection metrics:
//   - aleutian_diagnostics_collections_total (counter, labels: severity, reason)
//   - aleutian_diagnostics_collection_duration_seconds (histogram)
//   - aleutian_diagnostics_size_bytes (histogram)
//   - aleutian_diagnostics_errors_total (counter, labels: error_type)
//
// Container metrics (for Root Cause Analysis):
//   - aleutian_container_health (gauge, labels: container_name, service_type, status)
//   - aleutian_container_cpu_percent (gauge, labels: container_name)
//   - aleutian_container_memory_mb (gauge, labels: container_name)
//
// Retention metrics:
//   - aleutian_diagnostics_pruned_total (counter)
//   - aleutian_diagnostics_stored_total (gauge)
//
// # Thread Safety
//
// All methods must be safe for concurrent use.
type DiagnosticsMetrics interface {
	// RecordCollection records a successful diagnostic collection.
	//
	// # Description
	//
	// Updates collection counter, duration histogram, and size histogram.
	// Called after each successful Collect operation.
	//
	// # Inputs
	//
	//   - severity: Diagnostic severity level
	//   - reason: Why diagnostics were collected
	//   - durationMs: Collection duration in milliseconds
	//   - sizeBytes: Size of formatted output in bytes
	//
	// # Examples
	//
	//   metrics.RecordCollection(SeverityError, "startup_failure", 1500, 102400)
	RecordCollection(severity DiagnosticsSeverity, reason string, durationMs int64, sizeBytes int64)

	// RecordError records a collection error.
	//
	// # Description
	//
	// Increments the error counter with the error type label.
	// Called when Collect fails.
	//
	// # Inputs
	//
	//   - errorType: Category of error (e.g., "storage_failure", "container_unreachable")
	//
	// # Examples
	//
	//   metrics.RecordError("storage_failure")
	RecordError(errorType string)

	// RecordContainerHealth records container health status.
	//
	// # Description
	//
	// Updates the container health gauge. Essential for root cause analysis
	// in Grafana dashboards.
	//
	// # Inputs
	//
	//   - containerName: Container identifier (e.g., "aleutian-go-orchestrator")
	//   - serviceType: Service category (e.g., "orchestrator", "vectordb", "rag")
	//   - status: Health status ("healthy", "unhealthy", "unknown")
	//
	// # Examples
	//
	//   metrics.RecordContainerHealth("aleutian-weaviate", "vectordb", "healthy")
	RecordContainerHealth(containerName, serviceType, status string)

	// RecordContainerMetrics records container resource usage.
	//
	// # Description
	//
	// Updates CPU and memory gauges for a container. Enables performance
	// monitoring and bottleneck identification.
	//
	// # Inputs
	//
	//   - containerName: Container identifier
	//   - cpuPercent: CPU usage percentage (0-100+)
	//   - memoryMB: Memory usage in megabytes
	//
	// # Examples
	//
	//   metrics.RecordContainerMetrics("aleutian-rag-engine", 78.5, 4096)
	RecordContainerMetrics(containerName string, cpuPercent float64, memoryMB int64)

	// RecordPruned records diagnostics pruned by retention policy.
	//
	// # Inputs
	//
	//   - count: Number of diagnostics pruned
	RecordPruned(count int)

	// RecordStoredCount updates the current stored diagnostics count.
	//
	// # Inputs
	//
	//   - count: Current number of stored diagnostics
	RecordStoredCount(count int)

	// Register registers all metrics with Prometheus.
	//
	// # Description
	//
	// Registers metric collectors with the Prometheus default registry.
	// Should be called once during application startup.
	//
	// # Outputs
	//
	//   - error: Non-nil if registration fails (e.g., duplicate metrics)
	//
	// # Examples
	//
	//   if err := metrics.Register(); err != nil {
	//       log.Fatalf("Failed to register metrics: %v", err)
	//   }
	Register() error
}

// -----------------------------------------------------------------------------
// DiagnosticsViewer Interface
// -----------------------------------------------------------------------------

// DiagnosticsViewer retrieves and displays stored diagnostics.
//
// This interface completes the CRUD cycle for diagnostics, enabling:
//
//   - `aleutian diagnose --view <id>` command
//   - Future TUI dashboard historical views
//   - Hydration of diagnostics regardless of storage backend
//
// # Thread Safety
//
// Implementations must be safe for concurrent use.
type DiagnosticsViewer interface {
	// Get retrieves a diagnostic by ID or path.
	//
	// # Description
	//
	// Loads and parses a diagnostic from storage. Accepts various
	// identifier formats: file path, trace ID, or generated ID.
	//
	// # Inputs
	//
	//   - ctx: Context for cancellation
	//   - id: Diagnostic ID, trace ID, or storage path
	//
	// # Outputs
	//
	//   - *DiagnosticsData: Parsed diagnostic data
	//   - error: Non-nil if not found or parse fails
	//
	// # Examples
	//
	//   data, err := viewer.Get(ctx, "/path/to/diag.json")
	//   if err != nil {
	//       return fmt.Errorf("diagnostic not found: %w", err)
	//   }
	//   fmt.Printf("Reason: %s\n", data.Header.Reason)
	//
	// # Limitations
	//
	//   - Pruned diagnostics cannot be retrieved
	//   - Parse errors if format changed between versions
	Get(ctx context.Context, id string) (*DiagnosticsData, error)

	// List returns recent diagnostics metadata.
	//
	// # Description
	//
	// Returns summaries of stored diagnostics for listing without
	// loading full content. Supports filtering and pagination.
	//
	// # Inputs
	//
	//   - ctx: Context for cancellation
	//   - opts: Filter and pagination options
	//
	// # Outputs
	//
	//   - []DiagnosticsSummary: Metadata for matching diagnostics
	//   - error: Non-nil if listing fails
	//
	// # Examples
	//
	//   summaries, err := viewer.List(ctx, ListOptions{
	//       Limit:    20,
	//       Severity: SeverityError,
	//   })
	//   for _, s := range summaries {
	//       fmt.Printf("[%s] %s - %s\n", s.Severity, s.Reason, s.TraceID)
	//   }
	List(ctx context.Context, opts ListOptions) ([]DiagnosticsSummary, error)

	// GetByTraceID retrieves a diagnostic by its OpenTelemetry trace ID.
	//
	// # Description
	//
	// Finds and loads a diagnostic using the trace ID from Jaeger or
	// other tracing tools. This is the "Support Ticket Revolution" -
	// users provide trace ID instead of pasting logs.
	//
	// # Inputs
	//
	//   - ctx: Context for cancellation
	//   - traceID: OpenTelemetry trace ID (hex string)
	//
	// # Outputs
	//
	//   - *DiagnosticsData: Parsed diagnostic data
	//   - error: Non-nil if not found
	//
	// # Examples
	//
	//   data, err := viewer.GetByTraceID(ctx, "abc123def456...")
	//   if err != nil {
	//       return fmt.Errorf("trace not found: %w", err)
	//   }
	//
	// # Limitations
	//
	//   - Requires storage backend to index by trace ID
	//   - May be slower than direct path lookup
	GetByTraceID(ctx context.Context, traceID string) (*DiagnosticsData, error)
}

// -----------------------------------------------------------------------------
// PanicRecoveryHandler Interface
// -----------------------------------------------------------------------------

// PanicRecoveryHandler captures diagnostics when the application panics.
//
// This implements the "Black Box Recorder" pattern - capturing system state
// exactly when a crash occurs, when it's most valuable for debugging.
//
// # Safety
//
// The panic handler MUST respect Privacy/PII policy. It does NOT dump memory
// containing user prompts or sensitive data. All output passes through the
// sanitization pipeline.
//
// # Thread Safety
//
// Must be safe for use across goroutines. The deferred Wrap() function may
// be called from any goroutine's panic.
type PanicRecoveryHandler interface {
	// Wrap returns a function suitable for defer that captures panics.
	//
	// # Description
	//
	// Returns a closure that should be deferred at the start of main() or
	// critical goroutines. On panic, it captures diagnostics before the
	// process terminates.
	//
	// # Outputs
	//
	//   - func(): Closure to defer; call with () to execute
	//
	// # Examples
	//
	//   func main() {
	//       panicHandler := NewDefaultPanicRecoveryHandler(collector)
	//       defer panicHandler.Wrap()()
	//
	//       // Normal execution...
	//   }
	//
	// # Behavior on Panic
	//
	//   1. Recovers the panic value
	//   2. Collects diagnostics with SeverityCritical
	//   3. Prints trace ID and location to stderr
	//   4. Re-panics with original value (process still terminates)
	//
	// # Limitations
	//
	//   - Cannot capture diagnostics if defer is not set up
	//   - Storage failures are logged but don't prevent panic propagation
	//   - Memory exhaustion panics may not have resources to collect
	//
	// # Assumptions
	//
	//   - Collector is properly initialized before panic
	//   - Storage backend is accessible (or fails gracefully)
	Wrap() func()

	// SetCollector configures the diagnostics collector to use.
	//
	// # Inputs
	//
	//   - collector: DiagnosticsCollector for capturing panic state
	SetCollector(collector DiagnosticsCollector)

	// GetLastPanicResult returns the result of the last panic capture.
	//
	// # Description
	//
	// Returns the diagnostic result from the most recent panic recovery.
	// Useful for tests or for logging after a recovered panic.
	//
	// # Outputs
	//
	//   - *DiagnosticsResult: Last panic diagnostic, or nil
	GetLastPanicResult() *DiagnosticsResult
}
