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
Package diagnostics provides DefaultDiagnosticsCollector for gathering system diagnostics.

The DefaultDiagnosticsCollector is the FOSS-tier implementation that collects:

  - System information (OS, architecture, hostname, versions)
  - Podman state (version, machines, containers)
  - Container logs (optional)
  - System metrics (optional)

# Open Core Architecture

This collector follows the Open Core model:

  - FOSS (this file): Collects local diagnostics, stores to ~/.aleutian/diagnostics/
  - Enterprise: EvaluationDiagnosticsCollector adds ML evaluation metrics, PII redaction

# Integration Points

The collector is designed for observability integration:

  - OpenTelemetry: Span creation hooks (Phase 3.6)
  - Prometheus: Metric recording hooks (Phase 3.7)
  - Jaeger: Trace ID correlation via Header.TraceID
*/
package diagnostics

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/AleutianAI/AleutianFOSS/cmd/aleutian/internal/infra/process"
)

// -----------------------------------------------------------------------------
// DefaultDiagnosticsCollector Implementation
// -----------------------------------------------------------------------------

// DefaultDiagnosticsCollector gathers system and container diagnostics.
//
// This is the FOSS-tier collector that provides local diagnostic collection
// for developers debugging their own machines.
//
// # Enterprise Alternative
//
// EvaluationDiagnosticsCollector (Enterprise) adds:
//   - ML model evaluation metrics
//   - PII redaction before storage
//   - Push to centralized Loki/Splunk
//
// # Capabilities
//
//   - System information collection (OS, arch, versions)
//   - Podman machine and container enumeration
//   - Container log collection (with line limits)
//   - System resource metrics (CPU, memory, disk)
//   - Pluggable formatter and storage backends
//
// # Thread Safety
//
// DefaultDiagnosticsCollector is safe for concurrent use.
// Multiple goroutines can call Collect() concurrently.
type DefaultDiagnosticsCollector struct {
	// processManager executes external commands (podman).
	processManager process.Manager

	// formatter converts collected data to output format.
	formatter DiagnosticsFormatter

	// storage persists formatted output.
	storage DiagnosticsStorage

	// lastResult caches the most recent collection result.
	lastResult *DiagnosticsResult

	// mu protects lastResult and other mutable state.
	mu sync.RWMutex

	// aleutianVersion is the CLI version string.
	aleutianVersion string

	// metricsRecorder records collection metrics (nil if no metrics).
	// Set via SetMetrics() for Prometheus integration.
	metricsRecorder DiagnosticsMetrics
}

// NewDefaultDiagnosticsCollector creates a collector with default settings.
//
// # Description
//
// Creates a FOSS-tier collector that uses:
//   - DefaultProcessManager for podman commands
//   - JSONDiagnosticsFormatter for output
//   - FileDiagnosticsStorage for persistence
//
// # Inputs
//
//   - version: Aleutian CLI version string (e.g., "0.4.0")
//
// # Outputs
//
//   - *DefaultDiagnosticsCollector: Ready-to-use collector
//   - error: Non-nil if storage initialization fails
//
// # Examples
//
//	collector, err := NewDefaultDiagnosticsCollector("0.4.0")
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	result, err := collector.Collect(ctx, CollectOptions{
//	    Reason: "startup_failure",
//	})
//
// # Limitations
//
//   - Requires podman to be installed for container info
//   - System metrics may require elevated permissions on some platforms
//
// # Assumptions
//
//   - User has read access to podman socket
//   - Filesystem is writable for storage
func NewDefaultDiagnosticsCollector(version string) (*DefaultDiagnosticsCollector, error) {
	storage, err := NewFileDiagnosticsStorage("")
	if err != nil {
		return nil, fmt.Errorf("failed to create diagnostics storage: %w", err)
	}

	return &DefaultDiagnosticsCollector{
		processManager:  process.NewDefaultManager(),
		formatter:       NewJSONDiagnosticsFormatter(),
		storage:         storage,
		aleutianVersion: version,
	}, nil
}

// NewDiagnosticsCollectorWithDeps creates a collector with injected dependencies.
//
// # Description
//
// Creates a collector with custom dependencies for testing or alternative backends.
// This constructor enables full dependency injection for unit testing.
//
// # Inputs
//
//   - pm: ProcessManager for external commands (use MockProcessManager in tests)
//   - formatter: DiagnosticsFormatter for output format
//   - storage: DiagnosticsStorage for persistence
//   - version: Aleutian CLI version string
//
// # Outputs
//
//   - *DefaultDiagnosticsCollector: Configured collector
//
// # Examples
//
//	// In tests:
//	mockPM := NewMockProcessManager()
//	mockStorage := &MockDiagnosticsStorage{}
//	collector := NewDiagnosticsCollectorWithDeps(
//	    mockPM,
//	    NewJSONDiagnosticsFormatter(),
//	    mockStorage,
//	    "test",
//	)
//
// # Limitations
//
//   - None
//
// # Assumptions
//
//   - All dependencies are non-nil and properly initialized
func NewDiagnosticsCollectorWithDeps(
	pm process.Manager,
	formatter DiagnosticsFormatter,
	storage DiagnosticsStorage,
	version string,
) *DefaultDiagnosticsCollector {
	return &DefaultDiagnosticsCollector{
		processManager:  pm,
		formatter:       formatter,
		storage:         storage,
		aleutianVersion: version,
	}
}

// Collect gathers system diagnostics and stores them.
//
// # Description
//
// Collects system information, Podman state, and optional container logs/metrics.
// The collected data is formatted and stored, returning a result with the
// storage location and trace ID for support ticket correlation.
//
// # Inputs
//
//   - ctx: Context for cancellation and timeout
//   - opts: Collection options (reason, severity, what to include)
//
// # Outputs
//
//   - *DiagnosticsResult: Collection outcome with location and trace ID
//   - error: Non-nil if collection fails catastrophically
//
// # Examples
//
//	result, err := collector.Collect(ctx, CollectOptions{
//	    Reason:               "machine_drift",
//	    Severity:             SeverityWarning,
//	    IncludeContainerLogs: true,
//	    ContainerLogLines:    100,
//	})
//	if err != nil {
//	    log.Printf("Collection failed: %v", err)
//	}
//	fmt.Printf("Diagnostics saved to: %s\n", result.Location)
//	fmt.Printf("Trace ID for support: %s\n", result.TraceID)
//
// # Limitations
//
//   - Container logs may be truncated if containers have excessive output
//   - Some system metrics may be unavailable on certain platforms
//
// # Assumptions
//
//   - Podman is installed and accessible (degrades gracefully if not)
//   - Storage backend is writable
func (c *DefaultDiagnosticsCollector) Collect(ctx context.Context, opts CollectOptions) (*DiagnosticsResult, error) {
	startTime := time.Now()
	opts = opts.WithDefaults()

	// Generate trace/span IDs
	traceID := c.generateTraceID()
	spanID := c.generateSpanID()

	// Build diagnostic data
	data := c.buildDiagnosticsData(ctx, opts, traceID, spanID, startTime)

	// Calculate collection duration
	durationMs := time.Since(startTime).Milliseconds()
	data.Header.DurationMs = durationMs

	// Format and store
	result, err := c.formatAndStore(ctx, data, opts)
	if err != nil {
		return nil, err
	}

	// Complete result with timing info
	result.TraceID = traceID
	result.SpanID = spanID
	result.TimestampMs = startTime.UnixMilli()
	result.DurationMs = durationMs

	// Cache result
	c.cacheResult(result)

	// Record metrics if available
	c.recordMetrics(opts, durationMs, result.SizeBytes)

	return result, nil
}

// GetLastResult returns the most recent collection result.
//
// # Description
//
// Returns the cached result from the last successful Collect() call.
// Useful for displaying previous diagnostic location/trace ID.
//
// # Outputs
//
//   - *DiagnosticsResult: Last result, or nil if never collected
//
// # Examples
//
//	if last := collector.GetLastResult(); last != nil {
//	    fmt.Printf("Last collection: %s\n", last.Location)
//	}
//
// # Limitations
//
//   - Only returns the single most recent result
//   - Result is lost when process exits
//
// # Assumptions
//
//   - None
func (c *DefaultDiagnosticsCollector) GetLastResult() *DiagnosticsResult {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.lastResult
}

// SetStorage replaces the storage backend.
//
// # Description
//
// Allows swapping storage backends after construction.
// Useful for testing or switching to enterprise backends.
//
// # Inputs
//
//   - storage: New storage backend to use (must not be nil)
//
// # Examples
//
//	// Switch to S3 storage (Enterprise)
//	collector.SetStorage(s3Storage)
//
// # Limitations
//
//   - No validation that storage is working
//
// # Assumptions
//
//   - Storage backend is properly initialized
func (c *DefaultDiagnosticsCollector) SetStorage(storage DiagnosticsStorage) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.storage = storage
}

// SetFormatter replaces the output formatter.
//
// # Description
//
// Allows swapping formatters after construction.
// Useful for switching between JSON and text output.
//
// # Inputs
//
//   - formatter: New formatter to use (must not be nil)
//
// # Examples
//
//	// Switch to text output for display
//	collector.SetFormatter(NewTextDiagnosticsFormatter())
//
// # Limitations
//
//   - No validation that formatter is working
//
// # Assumptions
//
//   - Formatter is properly initialized
func (c *DefaultDiagnosticsCollector) SetFormatter(formatter DiagnosticsFormatter) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.formatter = formatter
}

// SetMetrics sets the metrics recorder for Prometheus integration.
//
// # Description
//
// Configures Prometheus metrics recording for collection operations.
// When set, each Collect() call records duration and size metrics.
//
// # Inputs
//
//   - recorder: Metrics recorder, or nil to disable metrics
//
// # Examples
//
//	// Enable Prometheus metrics
//	collector.SetMetrics(promMetrics)
//
// # Limitations
//
//   - Metrics are not recorded for failed collections
//
// # Assumptions
//
//   - Metrics recorder is registered with Prometheus
func (c *DefaultDiagnosticsCollector) SetMetrics(recorder DiagnosticsMetrics) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.metricsRecorder = recorder
}

// -----------------------------------------------------------------------------
// Private Methods - Data Building
// -----------------------------------------------------------------------------

// buildDiagnosticsData constructs the complete diagnostic data structure.
//
// # Description
//
// Orchestrates collection of all diagnostic components based on options.
// This is the core data assembly method.
//
// # Inputs
//
//   - ctx: Context for cancellation
//   - opts: Collection options
//   - traceID: Generated trace ID for correlation
//   - spanID: Generated span ID for this collection
//   - startTime: When collection started
//
// # Outputs
//
//   - *DiagnosticsData: Assembled diagnostic data
func (c *DefaultDiagnosticsCollector) buildDiagnosticsData(
	ctx context.Context,
	opts CollectOptions,
	traceID, spanID string,
	startTime time.Time,
) *DiagnosticsData {
	data := &DiagnosticsData{
		Header: DiagnosticsHeader{
			Version:     DiagnosticsVersion,
			TimestampMs: startTime.UnixMilli(),
			TraceID:     traceID,
			SpanID:      spanID,
			Reason:      opts.Reason,
			Details:     opts.Details,
			Severity:    opts.Severity,
		},
		Tags: opts.Tags,
	}

	// Collect system info (always)
	data.System = c.collectSystemInfo()

	// Collect Podman info (always)
	data.Podman = c.collectPodmanInfo(ctx)

	// Collect container logs (optional)
	if opts.IncludeContainerLogs && data.Podman.Available {
		data.ContainerLogs = c.collectContainerLogs(ctx, data.Podman.Containers, opts.ContainerLogLines)
	}

	// Collect system metrics (optional)
	if opts.IncludeSystemMetrics {
		data.Metrics = c.collectSystemMetrics()
	}

	return data
}

// formatAndStore formats data and persists it to storage.
//
// # Description
//
// Takes assembled diagnostic data, formats it, and stores it.
// Returns a partial result with location and size.
//
// # Inputs
//
//   - ctx: Context for storage operations
//   - data: Assembled diagnostic data
//   - opts: Collection options for metadata
//
// # Outputs
//
//   - *DiagnosticsResult: Partial result with Location, Format, SizeBytes
//   - error: Non-nil if formatting or storage fails
func (c *DefaultDiagnosticsCollector) formatAndStore(
	ctx context.Context,
	data *DiagnosticsData,
	opts CollectOptions,
) (*DiagnosticsResult, error) {
	c.mu.RLock()
	formatter := c.formatter
	storage := c.storage
	c.mu.RUnlock()

	output, err := formatter.Format(data)
	if err != nil {
		return nil, fmt.Errorf("failed to format diagnostics: %w", err)
	}

	location, err := storage.Store(ctx, output, StorageMetadata{
		FilenameHint: sanitizeFilenameHint(opts.Reason),
		ContentType:  formatter.ContentType(),
		Tags:         opts.Tags,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to store diagnostics: %w", err)
	}

	return &DiagnosticsResult{
		Location:  location,
		Format:    formatter.FileExtension(),
		SizeBytes: int64(len(output)),
	}, nil
}

// cacheResult stores the result for GetLastResult().
//
// # Description
//
// Thread-safely caches the collection result.
//
// # Inputs
//
//   - result: Result to cache
func (c *DefaultDiagnosticsCollector) cacheResult(result *DiagnosticsResult) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lastResult = result
}

// recordMetrics records collection metrics if recorder is set.
//
// # Description
//
// Sends metrics to Prometheus if a recorder is configured.
//
// # Inputs
//
//   - opts: Collection options for labels
//   - durationMs: How long collection took
//   - sizeBytes: Size of output
func (c *DefaultDiagnosticsCollector) recordMetrics(opts CollectOptions, durationMs, sizeBytes int64) {
	c.mu.RLock()
	recorder := c.metricsRecorder
	c.mu.RUnlock()

	if recorder != nil {
		recorder.RecordCollection(opts.Severity, opts.Reason, durationMs, sizeBytes)
	}
}

// -----------------------------------------------------------------------------
// Private Methods - System Collection
// -----------------------------------------------------------------------------

// collectSystemInfo gathers OS and runtime information.
//
// # Description
//
// Collects static system information that doesn't require external commands.
//
// # Outputs
//
//   - SystemInfo: Populated system information
func (c *DefaultDiagnosticsCollector) collectSystemInfo() SystemInfo {
	hostname, _ := os.Hostname()

	return SystemInfo{
		OS:              runtime.GOOS,
		Arch:            runtime.GOARCH,
		Hostname:        hostname,
		GoVersion:       runtime.Version(),
		AleutianVersion: c.aleutianVersion,
	}
}

// collectPodmanInfo gathers Podman state information.
//
// # Description
//
// Checks Podman availability and collects machine/container state.
// Degrades gracefully if Podman is not installed.
//
// # Inputs
//
//   - ctx: Context for command execution
//
// # Outputs
//
//   - PodmanInfo: Populated Podman information (Available=false if not installed)
func (c *DefaultDiagnosticsCollector) collectPodmanInfo(ctx context.Context) PodmanInfo {
	info := PodmanInfo{
		Available: false,
	}

	// Check Podman version
	version, err := c.getPodmanVersion(ctx)
	if err != nil {
		info.Error = fmt.Sprintf("podman not available: %v", err)
		return info
	}

	info.Available = true
	info.Version = version

	// List machines (macOS/Windows only)
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		info.MachineList = c.collectMachineInfo(ctx)
	}

	// List containers
	info.Containers = c.collectContainerInfo(ctx)

	return info
}

// getPodmanVersion retrieves the Podman version string.
//
// # Description
//
// Executes `podman version` and extracts the client version.
//
// # Inputs
//
//   - ctx: Context for command execution
//
// # Outputs
//
//   - string: Version string (e.g., "4.8.0")
//   - error: Non-nil if podman is not available
func (c *DefaultDiagnosticsCollector) getPodmanVersion(ctx context.Context) (string, error) {
	output, err := c.processManager.Run(ctx, "podman", "version", "--format", "{{.Client.Version}}")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

// collectMachineInfo gathers Podman machine state.
//
// # Description
//
// Executes `podman machine list` and parses the JSON output.
// Only called on macOS/Windows where Podman uses a VM.
//
// # Inputs
//
//   - ctx: Context for command execution
//
// # Outputs
//
//   - []MachineInfo: List of machines, or nil on error
func (c *DefaultDiagnosticsCollector) collectMachineInfo(ctx context.Context) []MachineInfo {
	output, err := c.processManager.Run(ctx, "podman", "machine", "list", "--format", "json")
	if err != nil {
		return nil
	}

	return c.parseMachineList(output)
}

// parseMachineList parses JSON machine list output.
//
// # Description
//
// Converts podman machine list JSON to MachineInfo slice.
//
// # Inputs
//
//   - output: Raw JSON bytes from podman machine list
//
// # Outputs
//
//   - []MachineInfo: Parsed machines, or nil on parse error
func (c *DefaultDiagnosticsCollector) parseMachineList(output []byte) []MachineInfo {
	var machines []struct {
		Name     string   `json:"Name"`
		Running  bool     `json:"Running"`
		CPUs     int      `json:"CPUs"`
		Memory   string   `json:"Memory"`
		DiskSize string   `json:"DiskSize"`
		Mounts   []string `json:"Mounts,omitempty"`
		Starting bool     `json:"Starting,omitempty"`
	}

	if err := json.Unmarshal(output, &machines); err != nil {
		return nil
	}

	result := make([]MachineInfo, len(machines))
	for i, m := range machines {
		result[i] = MachineInfo{
			Name:     m.Name,
			State:    c.determineMachineState(m.Running, m.Starting),
			CPUs:     m.CPUs,
			MemoryMB: parseMemoryString(m.Memory),
			DiskGB:   parseDiskString(m.DiskSize),
			Mounts:   m.Mounts,
		}
	}

	return result
}

// determineMachineState converts boolean flags to state string.
//
// # Description
//
// Maps running/starting booleans to human-readable state.
//
// # Inputs
//
//   - running: Whether machine is running
//   - starting: Whether machine is starting
//
// # Outputs
//
//   - string: "running", "starting", or "stopped"
func (c *DefaultDiagnosticsCollector) determineMachineState(running, starting bool) string {
	if running {
		return "running"
	}
	if starting {
		return "starting"
	}
	return "stopped"
}

// collectContainerInfo gathers Aleutian container state.
//
// # Description
//
// Executes `podman ps` filtered to Aleutian containers.
//
// # Inputs
//
//   - ctx: Context for command execution
//
// # Outputs
//
//   - []ContainerInfo: List of containers, or nil on error
func (c *DefaultDiagnosticsCollector) collectContainerInfo(ctx context.Context) []ContainerInfo {
	output, err := c.processManager.Run(ctx, "podman", "ps", "-a",
		"--filter", "name=aleutian",
		"--format", "json")
	if err != nil {
		return nil
	}

	return c.parseContainerList(output)
}

// parseContainerList parses JSON container list output.
//
// # Description
//
// Converts podman ps JSON to ContainerInfo slice.
//
// # Inputs
//
//   - output: Raw JSON bytes from podman ps
//
// # Outputs
//
//   - []ContainerInfo: Parsed containers, or nil on parse error
func (c *DefaultDiagnosticsCollector) parseContainerList(output []byte) []ContainerInfo {
	var containers []struct {
		ID      string   `json:"Id"`
		Names   []string `json:"Names"`
		State   string   `json:"State"`
		Image   string   `json:"Image"`
		Created int64    `json:"Created"`
		Started int64    `json:"StartedAt"`
	}

	if err := json.Unmarshal(output, &containers); err != nil {
		return nil
	}

	result := make([]ContainerInfo, len(containers))
	for i, container := range containers {
		name := ""
		if len(container.Names) > 0 {
			name = container.Names[0]
		}

		result[i] = ContainerInfo{
			ID:          shortenContainerID(container.ID),
			Name:        name,
			State:       container.State,
			Image:       container.Image,
			ServiceType: inferServiceType(name),
			CreatedAt:   container.Created * 1000,
			StartedAt:   container.Started * 1000,
		}
	}

	return result
}

// collectContainerLogs gathers logs from specified containers.
//
// # Description
//
// Retrieves logs from each running container, respecting line limits.
//
// # Inputs
//
//   - ctx: Context for command execution
//   - containers: Containers to get logs from
//   - maxLines: Maximum lines per container
//
// # Outputs
//
//   - []ContainerLog: Logs for each container
func (c *DefaultDiagnosticsCollector) collectContainerLogs(
	ctx context.Context,
	containers []ContainerInfo,
	maxLines int,
) []ContainerLog {
	if len(containers) == 0 {
		return nil
	}

	logs := make([]ContainerLog, 0, len(containers))
	for _, container := range containers {
		log := c.getContainerLog(ctx, container, maxLines)
		logs = append(logs, log)
	}

	return logs
}

// getContainerLog retrieves logs for a single container.
//
// # Description
//
// Gets logs from one container if it's running.
//
// # Inputs
//
//   - ctx: Context for command execution
//   - container: Container to get logs from
//   - maxLines: Maximum lines to retrieve
//
// # Outputs
//
//   - ContainerLog: Log data for the container
func (c *DefaultDiagnosticsCollector) getContainerLog(
	ctx context.Context,
	container ContainerInfo,
	maxLines int,
) ContainerLog {
	log := ContainerLog{
		Name: container.Name,
	}

	if container.State != "running" {
		log.Logs = "(container not running)"
		return log
	}

	output, err := c.processManager.Run(ctx, "podman", "logs",
		"--tail", fmt.Sprintf("%d", maxLines),
		container.Name)
	if err != nil {
		log.Error = fmt.Sprintf("failed to get logs: %v", err)
		return log
	}

	log.Logs = string(output)
	log.LineCount = countNewlines(string(output))
	log.Truncated = log.LineCount >= maxLines

	return log
}

// collectSystemMetrics gathers CPU, memory, and disk usage.
//
// # Description
//
// Collects system resource metrics. Currently returns zeros as
// a placeholder - full implementation requires platform-specific code.
//
// # Outputs
//
//   - *SystemMetrics: Resource metrics (currently zeros)
func (c *DefaultDiagnosticsCollector) collectSystemMetrics() *SystemMetrics {
	// Placeholder implementation - full metrics require platform-specific code
	// (e.g., gopsutil or direct syscalls)
	return &SystemMetrics{
		CPUUsagePercent: 0,
		MemoryUsedMB:    0,
		MemoryTotalMB:   0,
		MemoryPercent:   0,
		DiskUsedGB:      0,
		DiskTotalGB:     0,
		DiskPercent:     0,
	}
}

// -----------------------------------------------------------------------------
// Private Methods - ID Generation
// -----------------------------------------------------------------------------

// generateTraceID creates a trace ID for correlation.
//
// # Description
//
// Generates a unique trace ID for correlating this diagnostic with
// other telemetry. Will be replaced with OpenTelemetry in Phase 3.6.
//
// # Outputs
//
//   - string: Unique trace ID
func (c *DefaultDiagnosticsCollector) generateTraceID() string {
	return fmt.Sprintf("%d-%d", time.Now().UnixNano(), os.Getpid())
}

// generateSpanID creates a span ID for this collection.
//
// # Description
//
// Generates a unique span ID for this specific collection operation.
// Will be replaced with OpenTelemetry in Phase 3.6.
//
// # Outputs
//
//   - string: Unique span ID
func (c *DefaultDiagnosticsCollector) generateSpanID() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

// -----------------------------------------------------------------------------
// Package-Level Helper Functions
// -----------------------------------------------------------------------------

// shortenContainerID returns first 12 characters of a container ID.
//
// # Description
//
// Truncates container IDs for display purposes, matching Docker/Podman
// convention of showing short IDs.
//
// # Inputs
//
//   - id: Full container ID (64 hex characters)
//
// # Outputs
//
//   - string: First 12 characters, or full string if shorter
//
// # Examples
//
//	shortenContainerID("abc123def456789...") // "abc123def456"
//
// # Limitations
//
//   - Does not validate ID format
//
// # Assumptions
//
//   - ID is a valid hex string
func shortenContainerID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

// inferServiceType determines service type from container name.
//
// # Description
//
// Maps container name patterns to service type labels for Prometheus.
// Used for categorizing containers in metrics and dashboards.
//
// # Inputs
//
//   - name: Container name (e.g., "aleutian-go-orchestrator")
//
// # Outputs
//
//   - string: Service type ("orchestrator", "vectordb", etc.) or empty
//
// # Examples
//
//	inferServiceType("aleutian-go-orchestrator") // "orchestrator"
//	inferServiceType("aleutian-weaviate")        // "vectordb"
//	inferServiceType("unknown-container")        // ""
//
// # Limitations
//
//   - Only recognizes known Aleutian service patterns
//   - Case-sensitive matching
//
// # Assumptions
//
//   - Container names follow Aleutian naming conventions
func inferServiceType(name string) string {
	switch {
	case strings.Contains(name, "orchestrator"):
		return "orchestrator"
	case strings.Contains(name, "weaviate"):
		return "vectordb"
	case strings.Contains(name, "rag") || strings.Contains(name, "haystack"):
		return "rag"
	case strings.Contains(name, "embedding"):
		return "embedding"
	case strings.Contains(name, "ollama"):
		return "llm"
	default:
		return ""
	}
}

// countNewlines counts the number of newlines in a string.
//
// # Description
//
// Counts newline characters to determine line count.
// Used for log truncation detection.
//
// # Inputs
//
//   - s: String to count lines in
//
// # Outputs
//
//   - int: Number of lines (0 for empty string)
//
// # Examples
//
//	countNewlines("line1\nline2\n") // 2
//	countNewlines("")               // 0
//
// # Limitations
//
//   - Only counts \n, not \r\n
//
// # Assumptions
//
//   - Unix-style line endings
func countNewlines(s string) int {
	if s == "" {
		return 0
	}
	return strings.Count(s, "\n") + 1
}

// parseMemoryString parses memory strings like "4096MB" or "4GiB" to MB.
//
// # Description
//
// Converts human-readable memory strings from podman output to megabytes.
//
// # Inputs
//
//   - s: Memory string (e.g., "4096MB", "4GiB", "8G")
//
// # Outputs
//
//   - int64: Memory in megabytes, or 0 if parse fails
//
// # Examples
//
//	parseMemoryString("4096MB")  // 4096
//	parseMemoryString("4GiB")    // 4096
//	parseMemoryString("8G")      // 8192
//	parseMemoryString("invalid") // 0
//
// # Limitations
//
//   - Does not handle decimal values
//   - Limited unit recognition
//
// # Assumptions
//
//   - Input follows common memory notation patterns
func parseMemoryString(s string) int64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}

	var value int64
	var unit string

	if n, err := fmt.Sscanf(s, "%d%s", &value, &unit); err == nil && n >= 1 {
		unit = strings.ToUpper(unit)
		switch {
		case strings.HasPrefix(unit, "G"):
			return value * 1024
		case strings.HasPrefix(unit, "M"):
			return value
		case strings.HasPrefix(unit, "K"):
			return value / 1024
		default:
			return value
		}
	}

	return 0
}

// parseDiskString parses disk size strings like "100GB" to GB.
//
// # Description
//
// Converts human-readable disk size strings from podman output to gigabytes.
//
// # Inputs
//
//   - s: Disk size string (e.g., "100GB", "1TB")
//
// # Outputs
//
//   - int64: Disk size in gigabytes, or 0 if parse fails
//
// # Examples
//
//	parseDiskString("100GB") // 100
//	parseDiskString("1TB")   // 1024
//	parseDiskString("")      // 0
//
// # Limitations
//
//   - Does not handle decimal values
//   - Limited unit recognition
//
// # Assumptions
//
//   - Input follows common disk size notation patterns
func parseDiskString(s string) int64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}

	var value int64
	var unit string

	if n, err := fmt.Sscanf(s, "%d%s", &value, &unit); err == nil && n >= 1 {
		unit = strings.ToUpper(unit)
		switch {
		case strings.HasPrefix(unit, "T"):
			return value * 1024
		case strings.HasPrefix(unit, "G"):
			return value
		case strings.HasPrefix(unit, "M"):
			return value / 1024
		default:
			return value
		}
	}

	return 0
}

// Compile-time interface compliance check.
var _ DiagnosticsCollector = (*DefaultDiagnosticsCollector)(nil)
