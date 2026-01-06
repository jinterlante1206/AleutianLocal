package main

import (
	"fmt"
	"sync"
)

// MetricsValidator defines the interface for metrics schema validation.
//
// # Description
//
// MetricsValidator ensures metrics follow a strict schema to prevent
// cardinality explosion. Dynamic values (like error messages or user IDs)
// should go in logs, not metric labels.
//
// # Thread Safety
//
// Implementations must be safe for concurrent use.
type MetricsValidator interface {
	// ValidateMetric checks if a metric name is registered.
	ValidateMetric(name string) error

	// ValidateLabels checks if labels are valid for a metric.
	ValidateLabels(metric string, labels map[string]string) error

	// NormalizeLabel normalizes a label value to a known enum.
	NormalizeLabel(labelName, value string) string

	// RegisterMetric adds a new metric to the schema.
	RegisterMetric(name string, allowedLabels []string) error

	// RegisterLabelEnum defines valid values for a label.
	RegisterLabelEnum(labelName string, validValues []string) error
}

// MetricSchema defines the allowed labels for a metric.
type MetricSchema struct {
	// Name is the metric name.
	Name string

	// AllowedLabels lists the valid label names for this metric.
	AllowedLabels []string

	// Description explains what this metric measures.
	Description string
}

// MetricsSchemaConfig configures metrics validation.
//
// # Description
//
// Defines the allowed metrics and their labels to prevent
// cardinality explosion.
//
// # Example
//
//	config := MetricsSchemaConfig{
//	    StrictMode:      true,
//	    MaxLabelValues:  100,
//	}
type MetricsSchemaConfig struct {
	// StrictMode rejects unknown metrics (instead of logging warning).
	// Default: false
	StrictMode bool

	// MaxLabelValues limits unique values per label to detect explosion.
	// Default: 100
	MaxLabelValues int
}

// DefaultMetricsSchemaConfig returns sensible defaults.
func DefaultMetricsSchemaConfig() MetricsSchemaConfig {
	return MetricsSchemaConfig{
		StrictMode:     false,
		MaxLabelValues: 100,
	}
}

// DefaultMetricsSchema implements MetricsValidator with a predefined schema.
//
// # Description
//
// Enforces a strict schema for metrics to prevent cardinality explosion.
// Cardinality explosion happens when dynamic values (like error messages
// or user IDs) are used as metric labels, creating a new time series
// for each unique value.
//
// # Use Cases
//
//   - Validate metrics before recording
//   - Normalize error types to known categories
//   - Detect cardinality issues early
//
// # Thread Safety
//
// DefaultMetricsSchema is safe for concurrent use.
//
// # Limitations
//
//   - Requires upfront metric registration
//   - May reject valid metrics if schema is incomplete
//
// # Example
//
//	schema := NewMetricsSchema(DefaultMetricsSchemaConfig())
//	schema.RegisterMetric("aleutian_health_check_duration",
//	    []string{"service", "check_type", "status"})
//	schema.RegisterLabelEnum("status", []string{"healthy", "unhealthy", "timeout"})
//
//	err := schema.ValidateLabels("aleutian_health_check_duration",
//	    map[string]string{"service": "weaviate", "status": "healthy"})
type DefaultMetricsSchema struct {
	config      MetricsSchemaConfig
	metrics     map[string]MetricSchema
	labelEnums  map[string]map[string]bool
	labelCounts map[string]map[string]int // Track unique values per label
	mu          sync.RWMutex
}

// NewMetricsSchema creates a new metrics schema validator.
//
// # Description
//
// Creates a validator with predefined metrics for Aleutian.
// Additional metrics can be registered with RegisterMetric.
//
// # Inputs
//
//   - config: Configuration for validation behavior
//
// # Outputs
//
//   - *DefaultMetricsSchema: New schema validator with default metrics
func NewMetricsSchema(config MetricsSchemaConfig) *DefaultMetricsSchema {
	if config.MaxLabelValues <= 0 {
		config.MaxLabelValues = 100
	}

	s := &DefaultMetricsSchema{
		config:      config,
		metrics:     make(map[string]MetricSchema),
		labelEnums:  make(map[string]map[string]bool),
		labelCounts: make(map[string]map[string]int),
	}

	// Register default Aleutian metrics
	s.registerDefaults()

	return s
}

// registerDefaults registers the standard Aleutian metrics.
func (s *DefaultMetricsSchema) registerDefaults() {
	// Health check metrics
	s.metrics["aleutian_health_check_duration"] = MetricSchema{
		Name:          "aleutian_health_check_duration",
		AllowedLabels: []string{"service", "check_type", "status"},
		Description:   "Duration of health checks in seconds",
	}

	s.metrics["aleutian_health_check_total"] = MetricSchema{
		Name:          "aleutian_health_check_total",
		AllowedLabels: []string{"service", "check_type", "status"},
		Description:   "Total number of health checks",
	}

	s.metrics["aleutian_error_count"] = MetricSchema{
		Name:          "aleutian_error_count",
		AllowedLabels: []string{"service", "error_type"},
		Description:   "Count of errors by type (NOT by message!)",
	}

	s.metrics["aleutian_container_status"] = MetricSchema{
		Name:          "aleutian_container_status",
		AllowedLabels: []string{"service", "status"},
		Description:   "Current container status",
	}

	s.metrics["aleutian_circuit_breaker_state"] = MetricSchema{
		Name:          "aleutian_circuit_breaker_state",
		AllowedLabels: []string{"service", "state"},
		Description:   "Circuit breaker state",
	}

	// Register label enums
	s.labelEnums["status"] = toSet([]string{
		"healthy", "unhealthy", "degraded", "unknown", "timeout",
	})

	s.labelEnums["check_type"] = toSet([]string{
		"http", "tcp", "process", "container", "readiness",
	})

	s.labelEnums["error_type"] = toSet([]string{
		"connection_refused",
		"timeout",
		"unhealthy_response",
		"container_not_running",
		"network_error",
		"configuration_error",
		"unknown",
	})

	s.labelEnums["state"] = toSet([]string{
		"closed", "open", "half_open",
	})

	s.labelEnums["service"] = toSet([]string{
		"weaviate", "ollama", "orchestrator", "embeddings", "policy_engine",
		"rag_engine", "aleutian", "unknown",
	})
}

// ValidateMetric checks if a metric name is registered.
//
// # Description
//
// Returns an error if the metric is unknown and strict mode is enabled.
// In non-strict mode, logs a warning and allows the metric.
//
// # Inputs
//
//   - name: Metric name to validate
//
// # Outputs
//
//   - error: Non-nil if metric is invalid in strict mode
func (s *DefaultMetricsSchema) ValidateMetric(name string) error {
	s.mu.RLock()
	_, exists := s.metrics[name]
	s.mu.RUnlock()

	if !exists && s.config.StrictMode {
		return fmt.Errorf("unknown metric: %q (register with RegisterMetric)", name)
	}

	return nil
}

// ValidateLabels checks if labels are valid for a metric.
//
// # Description
//
// Validates that all provided labels are allowed for the metric and
// that label values match registered enums (if any).
//
// # Inputs
//
//   - metric: Metric name
//   - labels: Label key-value pairs to validate
//
// # Outputs
//
//   - error: Non-nil if any label is invalid
//
// # Example
//
//	err := schema.ValidateLabels("aleutian_error_count",
//	    map[string]string{"service": "weaviate", "error_type": "timeout"})
func (s *DefaultMetricsSchema) ValidateLabels(metric string, labels map[string]string) error {
	s.mu.RLock()
	schema, exists := s.metrics[metric]
	s.mu.RUnlock()

	if !exists {
		if s.config.StrictMode {
			return fmt.Errorf("unknown metric: %q", metric)
		}
		return nil
	}

	// Build allowed labels set
	allowed := toSet(schema.AllowedLabels)

	// Check each provided label
	for key, value := range labels {
		// Check if label is allowed for this metric
		if !allowed[key] {
			return fmt.Errorf("label %q not allowed for metric %q (allowed: %v)",
				key, metric, schema.AllowedLabels)
		}

		// Check if label value is in enum (if enum exists)
		s.mu.RLock()
		enumValues, hasEnum := s.labelEnums[key]
		s.mu.RUnlock()

		if hasEnum && !enumValues[value] {
			return fmt.Errorf("invalid value %q for label %q (use NormalizeLabel to map unknown values)",
				value, key)
		}

		// Track unique values for cardinality detection
		s.trackLabelValue(key, value)
	}

	return nil
}

// NormalizeLabel normalizes a label value to a known enum.
//
// # Description
//
// Maps unknown label values to "unknown" to prevent cardinality explosion.
// If the value is already valid, returns it unchanged.
//
// # Inputs
//
//   - labelName: Name of the label
//   - value: Value to normalize
//
// # Outputs
//
//   - string: Normalized value (or "unknown" if not in enum)
//
// # Example
//
//	// If "connection_reset" is not in error_type enum:
//	normalized := schema.NormalizeLabel("error_type", "connection_reset")
//	// normalized == "unknown"
func (s *DefaultMetricsSchema) NormalizeLabel(labelName, value string) string {
	s.mu.RLock()
	enumValues, hasEnum := s.labelEnums[labelName]
	s.mu.RUnlock()

	if !hasEnum {
		return value // No enum, accept as-is
	}

	if enumValues[value] {
		return value // Valid enum value
	}

	return "unknown" // Map unknown to "unknown"
}

// RegisterMetric adds a new metric to the schema.
//
// # Description
//
// Registers a metric with its allowed labels. Should be called during
// initialization before metrics are recorded.
//
// # Inputs
//
//   - name: Metric name (should follow prometheus conventions)
//   - allowedLabels: Label names allowed for this metric
//
// # Outputs
//
//   - error: Non-nil if registration fails
func (s *DefaultMetricsSchema) RegisterMetric(name string, allowedLabels []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.metrics[name]; exists {
		return fmt.Errorf("metric %q already registered", name)
	}

	s.metrics[name] = MetricSchema{
		Name:          name,
		AllowedLabels: allowedLabels,
	}

	return nil
}

// RegisterLabelEnum defines valid values for a label.
//
// # Description
//
// Registers an enum of valid values for a label. Any value not in
// the enum will be normalized to "unknown" by NormalizeLabel.
//
// # Inputs
//
//   - labelName: Label name
//   - validValues: List of valid values
//
// # Outputs
//
//   - error: Non-nil if registration fails
func (s *DefaultMetricsSchema) RegisterLabelEnum(labelName string, validValues []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.labelEnums[labelName] = toSet(validValues)
	return nil
}

// trackLabelValue tracks unique label values for cardinality monitoring.
func (s *DefaultMetricsSchema) trackLabelValue(labelName, value string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.labelCounts[labelName] == nil {
		s.labelCounts[labelName] = make(map[string]int)
	}

	s.labelCounts[labelName][value]++

	// Log warning if cardinality is getting high
	if len(s.labelCounts[labelName]) > s.config.MaxLabelValues {
		// Only warn once when threshold is crossed
		if len(s.labelCounts[labelName]) == s.config.MaxLabelValues+1 {
			// In production, this would use a logger
			// log.Printf("WARNING: Label %q has %d unique values (exceeds %d)",
			//     labelName, len(s.labelCounts[labelName]), s.config.MaxLabelValues)
		}
	}
}

// GetLabelCardinality returns the number of unique values seen for a label.
//
// # Description
//
// Returns cardinality information for monitoring purposes.
//
// # Inputs
//
//   - labelName: Label to check
//
// # Outputs
//
//   - int: Number of unique values seen
func (s *DefaultMetricsSchema) GetLabelCardinality(labelName string) int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.labelCounts[labelName] == nil {
		return 0
	}

	return len(s.labelCounts[labelName])
}

// GetHighCardinalityLabels returns labels that exceed the threshold.
//
// # Description
//
// Returns labels whose cardinality exceeds MaxLabelValues.
// Useful for detecting cardinality problems.
//
// # Outputs
//
//   - map[string]int: Label name to unique value count
func (s *DefaultMetricsSchema) GetHighCardinalityLabels() map[string]int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make(map[string]int)

	for label, values := range s.labelCounts {
		if len(values) > s.config.MaxLabelValues {
			result[label] = len(values)
		}
	}

	return result
}

// toSet converts a slice to a set (map[string]bool).
func toSet(slice []string) map[string]bool {
	result := make(map[string]bool, len(slice))
	for _, s := range slice {
		result[s] = true
	}
	return result
}

// Compile-time interface check
var _ MetricsValidator = (*DefaultMetricsSchema)(nil)
