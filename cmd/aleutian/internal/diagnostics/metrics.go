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
Package diagnostics provides Prometheus metrics for the Distributed Health Agent.

This file implements the DiagnosticsMetrics interface, enabling fleet-wide
monitoring and alerting for diagnostic collection operations.

# Open Core Architecture

This follows the Open Core model:

  - FOSS (NoOpDiagnosticsMetrics): Records metrics in memory, no export
  - Enterprise (PrometheusDiagnosticsMetrics): Full Prometheus export with labels

The interface is public; the implementation dictates the value.

# Metrics Exported

Collection metrics (diagnostics subsystem):

  - aleutian_diagnostics_collections_total: Counter by severity and reason
  - aleutian_diagnostics_collection_duration_seconds: Histogram of durations
  - aleutian_diagnostics_size_bytes: Histogram of output sizes
  - aleutian_diagnostics_errors_total: Counter by error type

Container metrics (container subsystem):

  - aleutian_container_health: Gauge by container, service type, status
  - aleutian_container_cpu_percent: Gauge of CPU usage
  - aleutian_container_memory_mb: Gauge of memory usage

Retention metrics (retention subsystem):

  - aleutian_diagnostics_pruned_total: Counter of pruned diagnostics
  - aleutian_diagnostics_stored_count: Gauge of current stored count

# Grafana Dashboard

Use these metrics to build dashboards showing:

  - Collection rate by severity (error spikes indicate problems)
  - Container health across the fleet
  - Storage growth and pruning effectiveness
  - Collection duration trends (performance regression detection)
*/
package diagnostics

import (
	"sync"
	"sync/atomic"

	"github.com/prometheus/client_golang/prometheus"
)

// -----------------------------------------------------------------------------
// Constants
// -----------------------------------------------------------------------------

// Metric namespace and subsystems.
const (
	// metricsNamespaceDiag is the namespace for all diagnostic metrics.
	metricsNamespaceDiag = "aleutian"

	// metricsSubsystemDiag is the subsystem for diagnostic collection metrics.
	metricsSubsystemDiag = "diagnostics"

	// metricsSubsystemContainer is the subsystem for container metrics.
	metricsSubsystemContainer = "container"
)

// -----------------------------------------------------------------------------
// NoOpDiagnosticsMetrics Implementation (FOSS Tier)
// -----------------------------------------------------------------------------

// NoOpDiagnosticsMetrics is the FOSS-tier metrics recorder that doesn't export.
//
// # Description
//
// This implementation records metrics in memory for local inspection but
// doesn't export to Prometheus. Useful for development and air-gapped
// environments.
//
// # Enterprise Alternative
//
// PrometheusDiagnosticsMetrics (Enterprise) provides:
//   - Full Prometheus export for Grafana dashboards
//   - Label-based filtering and aggregation
//   - Alerting integration via Alertmanager
//
// # Capabilities
//
//   - Tracks counts and totals in memory
//   - Zero network dependencies
//   - Thread-safe atomic operations
//
// # Thread Safety
//
// NoOpDiagnosticsMetrics is safe for concurrent use.
type NoOpDiagnosticsMetrics struct {
	// collectionsTotal is the total collection count.
	collectionsTotal atomic.Int64

	// errorsTotal is the total error count.
	errorsTotal atomic.Int64

	// prunedTotal is the total pruned count.
	prunedTotal atomic.Int64

	// storedCount is the current stored count.
	storedCount atomic.Int64

	// lastDurationMs is the last collection duration.
	lastDurationMs atomic.Int64

	// lastSizeBytes is the last collection size.
	lastSizeBytes atomic.Int64
}

// NewNoOpDiagnosticsMetrics creates a FOSS-tier metrics recorder.
//
// # Description
//
// Creates a metrics recorder that tracks values in memory without export.
// Useful for development, testing, or environments without Prometheus.
//
// # Outputs
//
//   - *NoOpDiagnosticsMetrics: Ready-to-use metrics recorder
//
// # Examples
//
//	metrics := NewNoOpDiagnosticsMetrics()
//	metrics.RecordCollection(SeverityError, "startup_failure", 1500, 102400)
//	fmt.Printf("Total collections: %d\n", metrics.GetCollectionsTotal())
//
// # Limitations
//
//   - Metrics are not visible in Prometheus/Grafana
//   - Lost on process restart
//
// # Assumptions
//
//   - Caller doesn't require external observability
func NewNoOpDiagnosticsMetrics() *NoOpDiagnosticsMetrics {
	return &NoOpDiagnosticsMetrics{}
}

// RecordCollection records a successful diagnostic collection.
//
// # Description
//
// Increments the collection counter and records duration/size.
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
//	metrics.RecordCollection(SeverityError, "startup_failure", 1500, 102400)
//
// # Limitations
//
//   - Labels (severity, reason) are ignored in no-op mode
//
// # Assumptions
//
//   - Called after successful collection
func (m *NoOpDiagnosticsMetrics) RecordCollection(severity DiagnosticsSeverity, reason string, durationMs int64, sizeBytes int64) {
	m.collectionsTotal.Add(1)
	m.lastDurationMs.Store(durationMs)
	m.lastSizeBytes.Store(sizeBytes)
}

// RecordError records a collection error.
//
// # Description
//
// Increments the error counter.
//
// # Inputs
//
//   - errorType: Category of error
//
// # Examples
//
//	metrics.RecordError("storage_failure")
//
// # Limitations
//
//   - Error type label is ignored in no-op mode
//
// # Assumptions
//
//   - Called when collection fails
func (m *NoOpDiagnosticsMetrics) RecordError(errorType string) {
	m.errorsTotal.Add(1)
}

// RecordContainerHealth records container health status.
//
// # Description
//
// Records container health for root cause analysis.
// In no-op mode, this is a no-op.
//
// # Inputs
//
//   - containerName: Container identifier
//   - serviceType: Service category
//   - status: Health status
//
// # Examples
//
//	metrics.RecordContainerHealth("aleutian-weaviate", "vectordb", "healthy")
//
// # Limitations
//
//   - Not recorded in no-op mode
//
// # Assumptions
//
//   - Container exists
func (m *NoOpDiagnosticsMetrics) RecordContainerHealth(containerName, serviceType, status string) {
	// No-op: container health not tracked in memory
}

// RecordContainerMetrics records container resource usage.
//
// # Description
//
// Records CPU and memory usage for performance monitoring.
// In no-op mode, this is a no-op.
//
// # Inputs
//
//   - containerName: Container identifier
//   - cpuPercent: CPU usage percentage
//   - memoryMB: Memory usage in megabytes
//
// # Examples
//
//	metrics.RecordContainerMetrics("aleutian-rag-engine", 78.5, 4096)
//
// # Limitations
//
//   - Not recorded in no-op mode
//
// # Assumptions
//
//   - Container exists
func (m *NoOpDiagnosticsMetrics) RecordContainerMetrics(containerName string, cpuPercent float64, memoryMB int64) {
	// No-op: container metrics not tracked in memory
}

// RecordPruned records diagnostics pruned by retention policy.
//
// # Description
//
// Adds to the pruned counter.
//
// # Inputs
//
//   - count: Number of diagnostics pruned
//
// # Examples
//
//	metrics.RecordPruned(15)
//
// # Limitations
//
//   - None
//
// # Assumptions
//
//   - Called after successful prune operation
func (m *NoOpDiagnosticsMetrics) RecordPruned(count int) {
	m.prunedTotal.Add(int64(count))
}

// RecordStoredCount updates the current stored diagnostics count.
//
// # Description
//
// Sets the current stored count gauge.
//
// # Inputs
//
//   - count: Current number of stored diagnostics
//
// # Examples
//
//	metrics.RecordStoredCount(42)
//
// # Limitations
//
//   - None
//
// # Assumptions
//
//   - Count is accurate
func (m *NoOpDiagnosticsMetrics) RecordStoredCount(count int) {
	m.storedCount.Store(int64(count))
}

// Register is a no-op for NoOpDiagnosticsMetrics.
//
// # Description
//
// Does nothing since there are no Prometheus collectors to register.
//
// # Outputs
//
//   - error: Always nil
//
// # Examples
//
//	_ = metrics.Register() // Always succeeds
//
// # Limitations
//
//   - None
//
// # Assumptions
//
//   - None
func (m *NoOpDiagnosticsMetrics) Register() error {
	return nil
}

// GetCollectionsTotal returns the total collection count for testing.
//
// # Description
//
// Accessor for testing the no-op recorder.
//
// # Outputs
//
//   - int64: Total collections recorded
//
// # Examples
//
//	total := metrics.GetCollectionsTotal()
//
// # Limitations
//
//   - Only available on NoOpDiagnosticsMetrics
//
// # Assumptions
//
//   - None
func (m *NoOpDiagnosticsMetrics) GetCollectionsTotal() int64 {
	return m.collectionsTotal.Load()
}

// GetErrorsTotal returns the total error count for testing.
//
// # Description
//
// Accessor for testing the no-op recorder.
//
// # Outputs
//
//   - int64: Total errors recorded
//
// # Examples
//
//	total := metrics.GetErrorsTotal()
//
// # Limitations
//
//   - Only available on NoOpDiagnosticsMetrics
//
// # Assumptions
//
//   - None
func (m *NoOpDiagnosticsMetrics) GetErrorsTotal() int64 {
	return m.errorsTotal.Load()
}

// GetPrunedTotal returns the total pruned count for testing.
//
// # Description
//
// Accessor for testing the no-op recorder.
//
// # Outputs
//
//   - int64: Total pruned recorded
//
// # Examples
//
//	total := metrics.GetPrunedTotal()
//
// # Limitations
//
//   - Only available on NoOpDiagnosticsMetrics
//
// # Assumptions
//
//   - None
func (m *NoOpDiagnosticsMetrics) GetPrunedTotal() int64 {
	return m.prunedTotal.Load()
}

// GetStoredCount returns the current stored count for testing.
//
// # Description
//
// Accessor for testing the no-op recorder.
//
// # Outputs
//
//   - int64: Current stored count
//
// # Examples
//
//	count := metrics.GetStoredCount()
//
// # Limitations
//
//   - Only available on NoOpDiagnosticsMetrics
//
// # Assumptions
//
//   - None
func (m *NoOpDiagnosticsMetrics) GetStoredCount() int64 {
	return m.storedCount.Load()
}

// -----------------------------------------------------------------------------
// PrometheusDiagnosticsMetrics Implementation (Enterprise Tier)
// -----------------------------------------------------------------------------

// PrometheusDiagnosticsMetrics exports diagnostics metrics to Prometheus.
//
// # Description
//
// This is the Enterprise-tier metrics recorder that exports to Prometheus
// for Grafana dashboards and Alertmanager alerting.
//
// # FOSS Alternative
//
// NoOpDiagnosticsMetrics (FOSS) works without Prometheus infrastructure.
//
// # Metrics
//
// Collection metrics:
//   - aleutian_diagnostics_collections_total (labels: severity, reason)
//   - aleutian_diagnostics_collection_duration_seconds (labels: severity)
//   - aleutian_diagnostics_size_bytes (labels: severity)
//   - aleutian_diagnostics_errors_total (labels: error_type)
//
// Container metrics:
//   - aleutian_container_health (labels: container, service_type, status)
//   - aleutian_container_cpu_percent (labels: container)
//   - aleutian_container_memory_mb (labels: container)
//
// Retention metrics:
//   - aleutian_diagnostics_pruned_total
//   - aleutian_diagnostics_stored_count
//
// # Thread Safety
//
// PrometheusDiagnosticsMetrics is safe for concurrent use.
type PrometheusDiagnosticsMetrics struct {
	// collectionsTotal counts collections by severity and reason.
	collectionsTotal *prometheus.CounterVec

	// collectionDuration is a histogram of collection durations.
	collectionDuration *prometheus.HistogramVec

	// collectionSize is a histogram of collection sizes.
	collectionSize *prometheus.HistogramVec

	// errorsTotal counts errors by type.
	errorsTotal *prometheus.CounterVec

	// containerHealth tracks container health status.
	containerHealth *prometheus.GaugeVec

	// containerCPU tracks container CPU usage.
	containerCPU *prometheus.GaugeVec

	// containerMemory tracks container memory usage.
	containerMemory *prometheus.GaugeVec

	// prunedTotal counts pruned diagnostics.
	prunedTotal prometheus.Counter

	// storedCount tracks current stored count.
	storedCount prometheus.Gauge

	// registered tracks if metrics are registered.
	registered bool

	// mu protects registered flag.
	mu sync.Mutex
}

// NewPrometheusDiagnosticsMetrics creates an Enterprise-tier metrics recorder.
//
// # Description
//
// Creates a metrics recorder that exports to Prometheus. Call Register()
// after creation to register with the Prometheus default registry.
//
// # Outputs
//
//   - *PrometheusDiagnosticsMetrics: Ready-to-use metrics recorder
//
// # Examples
//
//	metrics := NewPrometheusDiagnosticsMetrics()
//	if err := metrics.Register(); err != nil {
//	    log.Fatal(err)
//	}
//	metrics.RecordCollection(SeverityError, "startup_failure", 1500, 102400)
//
// # Limitations
//
//   - Requires Prometheus infrastructure
//   - Register() must be called before use
//
// # Assumptions
//
//   - Prometheus default registry is available
func NewPrometheusDiagnosticsMetrics() *PrometheusDiagnosticsMetrics {
	return &PrometheusDiagnosticsMetrics{
		collectionsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: metricsNamespaceDiag,
				Subsystem: metricsSubsystemDiag,
				Name:      "collections_total",
				Help:      "Total number of diagnostic collections by severity and reason",
			},
			[]string{"severity", "reason"},
		),

		collectionDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: metricsNamespaceDiag,
				Subsystem: metricsSubsystemDiag,
				Name:      "collection_duration_seconds",
				Help:      "Duration of diagnostic collection in seconds",
				Buckets:   []float64{0.1, 0.25, 0.5, 1.0, 2.5, 5.0, 10.0, 30.0},
			},
			[]string{"severity"},
		),

		collectionSize: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: metricsNamespaceDiag,
				Subsystem: metricsSubsystemDiag,
				Name:      "size_bytes",
				Help:      "Size of diagnostic output in bytes",
				Buckets:   []float64{1024, 10240, 102400, 1048576, 10485760},
			},
			[]string{"severity"},
		),

		errorsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: metricsNamespaceDiag,
				Subsystem: metricsSubsystemDiag,
				Name:      "errors_total",
				Help:      "Total number of diagnostic collection errors by type",
			},
			[]string{"error_type"},
		),

		containerHealth: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: metricsNamespaceDiag,
				Subsystem: metricsSubsystemContainer,
				Name:      "health",
				Help:      "Container health status (1=healthy, 0=unhealthy, -1=unknown)",
			},
			[]string{"container", "service_type", "status"},
		),

		containerCPU: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: metricsNamespaceDiag,
				Subsystem: metricsSubsystemContainer,
				Name:      "cpu_percent",
				Help:      "Container CPU usage percentage",
			},
			[]string{"container"},
		),

		containerMemory: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: metricsNamespaceDiag,
				Subsystem: metricsSubsystemContainer,
				Name:      "memory_mb",
				Help:      "Container memory usage in megabytes",
			},
			[]string{"container"},
		),

		prunedTotal: prometheus.NewCounter(
			prometheus.CounterOpts{
				Namespace: metricsNamespaceDiag,
				Subsystem: metricsSubsystemDiag,
				Name:      "pruned_total",
				Help:      "Total number of diagnostics pruned by retention policy",
			},
		),

		storedCount: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: metricsNamespaceDiag,
				Subsystem: metricsSubsystemDiag,
				Name:      "stored_count",
				Help:      "Current number of stored diagnostics",
			},
		),
	}
}

// RecordCollection records a successful diagnostic collection.
//
// # Description
//
// Updates the collection counter, duration histogram, and size histogram.
//
// # Inputs
//
//   - severity: Diagnostic severity level (used as label)
//   - reason: Why diagnostics were collected (used as label)
//   - durationMs: Collection duration in milliseconds
//   - sizeBytes: Size of formatted output in bytes
//
// # Examples
//
//	metrics.RecordCollection(SeverityError, "startup_failure", 1500, 102400)
//
// # Limitations
//
//   - High-cardinality reasons may cause metric explosion
//
// # Assumptions
//
//   - Register() has been called
func (m *PrometheusDiagnosticsMetrics) RecordCollection(severity DiagnosticsSeverity, reason string, durationMs int64, sizeBytes int64) {
	severityStr := string(severity)

	// Increment counter
	m.collectionsTotal.WithLabelValues(severityStr, reason).Inc()

	// Record duration (convert ms to seconds)
	m.collectionDuration.WithLabelValues(severityStr).Observe(float64(durationMs) / 1000.0)

	// Record size
	m.collectionSize.WithLabelValues(severityStr).Observe(float64(sizeBytes))
}

// RecordError records a collection error.
//
// # Description
//
// Increments the error counter with the error type label.
//
// # Inputs
//
//   - errorType: Category of error (e.g., "storage_failure", "container_unreachable")
//
// # Examples
//
//	metrics.RecordError("storage_failure")
//
// # Limitations
//
//   - High-cardinality error types may cause metric explosion
//
// # Assumptions
//
//   - Register() has been called
func (m *PrometheusDiagnosticsMetrics) RecordError(errorType string) {
	m.errorsTotal.WithLabelValues(errorType).Inc()
}

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
//	metrics.RecordContainerHealth("aleutian-weaviate", "vectordb", "healthy")
//
// # Limitations
//
//   - One gauge per container-service-status combination
//
// # Assumptions
//
//   - Register() has been called
func (m *PrometheusDiagnosticsMetrics) RecordContainerHealth(containerName, serviceType, status string) {
	// Set gauge value based on status
	var value float64
	switch status {
	case "healthy":
		value = 1
	case "unhealthy":
		value = 0
	default:
		value = -1
	}
	m.containerHealth.WithLabelValues(containerName, serviceType, status).Set(value)
}

// RecordContainerMetrics records container resource usage.
//
// # Description
//
// Updates CPU and memory gauges for a container.
//
// # Inputs
//
//   - containerName: Container identifier
//   - cpuPercent: CPU usage percentage (0-100+)
//   - memoryMB: Memory usage in megabytes
//
// # Examples
//
//	metrics.RecordContainerMetrics("aleutian-rag-engine", 78.5, 4096)
//
// # Limitations
//
//   - Values overwrite previous readings
//
// # Assumptions
//
//   - Register() has been called
func (m *PrometheusDiagnosticsMetrics) RecordContainerMetrics(containerName string, cpuPercent float64, memoryMB int64) {
	m.containerCPU.WithLabelValues(containerName).Set(cpuPercent)
	m.containerMemory.WithLabelValues(containerName).Set(float64(memoryMB))
}

// RecordPruned records diagnostics pruned by retention policy.
//
// # Description
//
// Adds to the pruned counter.
//
// # Inputs
//
//   - count: Number of diagnostics pruned
//
// # Examples
//
//	metrics.RecordPruned(15)
//
// # Limitations
//
//   - None
//
// # Assumptions
//
//   - Register() has been called
func (m *PrometheusDiagnosticsMetrics) RecordPruned(count int) {
	m.prunedTotal.Add(float64(count))
}

// RecordStoredCount updates the current stored diagnostics count.
//
// # Description
//
// Sets the stored count gauge.
//
// # Inputs
//
//   - count: Current number of stored diagnostics
//
// # Examples
//
//	metrics.RecordStoredCount(42)
//
// # Limitations
//
//   - None
//
// # Assumptions
//
//   - Register() has been called
func (m *PrometheusDiagnosticsMetrics) RecordStoredCount(count int) {
	m.storedCount.Set(float64(count))
}

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
//	if err := metrics.Register(); err != nil {
//	    log.Fatalf("Failed to register metrics: %v", err)
//	}
//
// # Limitations
//
//   - Cannot be called twice (will return error)
//
// # Assumptions
//
//   - Prometheus default registry is available
func (m *PrometheusDiagnosticsMetrics) Register() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.registered {
		return nil // Already registered
	}

	collectors := []prometheus.Collector{
		m.collectionsTotal,
		m.collectionDuration,
		m.collectionSize,
		m.errorsTotal,
		m.containerHealth,
		m.containerCPU,
		m.containerMemory,
		m.prunedTotal,
		m.storedCount,
	}

	for _, c := range collectors {
		if err := prometheus.Register(c); err != nil {
			return err
		}
	}

	m.registered = true
	return nil
}

// -----------------------------------------------------------------------------
// Factory Function
// -----------------------------------------------------------------------------

// NewDefaultDiagnosticsMetrics creates the appropriate metrics based on environment.
//
// # Description
//
// Factory function that returns NoOpDiagnosticsMetrics for FOSS tier
// or PrometheusDiagnosticsMetrics if PROMETHEUS_ENABLED is set.
//
// # Inputs
//
//   - enablePrometheus: Whether to enable Prometheus export
//
// # Outputs
//
//   - DiagnosticsMetrics: Appropriate metrics recorder for the environment
//
// # Examples
//
//	// FOSS mode
//	metrics := NewDefaultDiagnosticsMetrics(false)
//
//	// Enterprise mode
//	metrics := NewDefaultDiagnosticsMetrics(true)
//	if err := metrics.Register(); err != nil {
//	    log.Fatal(err)
//	}
//
// # Limitations
//
//   - Prometheus requires Register() call
//
// # Assumptions
//
//   - If Prometheus enabled, infrastructure exists
func NewDefaultDiagnosticsMetrics(enablePrometheus bool) DiagnosticsMetrics {
	if enablePrometheus {
		return NewPrometheusDiagnosticsMetrics()
	}
	return NewNoOpDiagnosticsMetrics()
}

// Compile-time interface compliance checks.
var _ DiagnosticsMetrics = (*NoOpDiagnosticsMetrics)(nil)
var _ DiagnosticsMetrics = (*PrometheusDiagnosticsMetrics)(nil)
