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
Package health provides HealthIntelligence for AI-native health analysis.

HealthIntelligence extends basic health checking with intelligent analysis:
log anomaly detection, metric trends, code freshness checking, and
LLM-powered health summaries using local Gemma models.

# Design Rationale

Basic health checking (Phase 9) answers "is it running?"
Health intelligence answers "how well is it running?" by:
  - Analyzing logs for anomalies and error patterns
  - Tracking metric trends over time
  - Checking if containers are running stale code
  - Generating human-readable summaries via LLM
*/
package health

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/jinterlante1206/AleutianLocal/cmd/aleutian/internal/infra/process"
)

// =============================================================================
// ERROR DEFINITIONS
// =============================================================================

var (
	// ErrAnalysisFailed indicates health analysis could not be completed.
	ErrAnalysisFailed = errors.New("health analysis failed")

	// ErrLLMUnavailable indicates the LLM service is not available.
	ErrLLMUnavailable = errors.New("LLM service unavailable")

	// ErrLogCollectionFailed indicates log collection failed.
	ErrLogCollectionFailed = errors.New("failed to collect logs")

	// ErrMetricsUnavailable indicates metrics data is not available.
	ErrMetricsUnavailable = errors.New("metrics not available")

	// ErrFreshnessCheckFailed indicates code freshness check failed.
	ErrFreshnessCheckFailed = errors.New("freshness check failed")
)

// =============================================================================
// INTERFACE DEFINITIONS
// =============================================================================

// HealthIntelligence provides AI-native health analysis.
//
// # Description
//
// This interface extends basic health checking with intelligent analysis:
// log anomaly detection, metric trends, code freshness checking, and
// LLM-powered health summaries.
//
// # Dependencies
//
//   - Requires HealthChecker for basic checks
//   - Uses Ollama (gemma3:1b) for LLM summaries
//   - Integrates with OTel for trace export
//
// # Thread Safety
//
// Implementations must be safe for concurrent use.
type HealthIntelligence interface {
	// AnalyzeHealth performs comprehensive health analysis.
	//
	// # Description
	//
	// Combines multiple signals (logs, metrics, basic health) into
	// an intelligent health assessment with insights and recommendations.
	//
	// # Inputs
	//
	//   - ctx: Context for cancellation
	//   - opts: Analysis options (time window, services)
	//
	// # Outputs
	//
	//   - *IntelligentHealthReport: Comprehensive health analysis
	//   - error: If analysis failed
	AnalyzeHealth(ctx context.Context, opts AnalysisOptions) (*IntelligentHealthReport, error)

	// GetServiceInsights returns detailed insights for one service.
	//
	// # Description
	//
	// Deep analysis of a single service including log patterns,
	// performance trends, and specific recommendations.
	//
	// # Inputs
	//
	//   - ctx: Context for cancellation
	//   - serviceName: Service to analyze
	//   - opts: Analysis options
	//
	// # Outputs
	//
	//   - *ServiceInsights: Detailed service analysis
	//   - error: If analysis failed
	GetServiceInsights(ctx context.Context, serviceName string, opts AnalysisOptions) (*ServiceInsights, error)

	// CheckCodeFreshness compares container age vs source code.
	//
	// # Description
	//
	// Detects when containers are running stale code by comparing
	// image creation time to git commit timestamps.
	//
	// # Inputs
	//
	//   - ctx: Context for cancellation
	//   - services: Services to check
	//
	// # Outputs
	//
	//   - []FreshnessReport: Freshness status per service
	//   - error: If check failed
	CheckCodeFreshness(ctx context.Context, services []ServiceDefinition) ([]FreshnessReport, error)

	// GetMetricTrends returns performance trends over time.
	//
	// # Description
	//
	// Analyzes latency, error rates, and resource usage trends
	// to detect degradation before failures occur.
	//
	// # Inputs
	//
	//   - ctx: Context for cancellation
	//   - serviceName: Service to analyze
	//   - window: Time window for analysis
	//
	// # Outputs
	//
	//   - *MetricTrends: Trend analysis results
	//   - error: If analysis failed
	GetMetricTrends(ctx context.Context, serviceName string, window time.Duration) (*MetricTrends, error)

	// GenerateLLMSummary creates an AI-generated health summary.
	//
	// # Description
	//
	// Uses local Gemma model to generate human-readable health summary
	// from collected data (logs, metrics, statuses).
	//
	// # Inputs
	//
	//   - ctx: Context for cancellation
	//   - data: Health data to summarize
	//
	// # Outputs
	//
	//   - string: LLM-generated summary
	//   - error: If generation failed
	GenerateLLMSummary(ctx context.Context, data *HealthDataBundle) (string, error)
}

// HealthTextGenerator abstracts LLM text generation for health summaries.
//
// # Description
//
// Provides a minimal interface for LLM text generation via Ollama.
// This is separate from the OllamaClient struct in ollama_client.go
// which handles model management (listing, pulling).
type HealthTextGenerator interface {
	// Generate produces text from a prompt.
	//
	// # Inputs
	//
	//   - ctx: Context for cancellation
	//   - model: Model name (e.g., "gemma3:1b")
	//   - prompt: Input prompt
	//
	// # Outputs
	//
	//   - string: Generated text
	//   - error: If generation failed
	Generate(ctx context.Context, model, prompt string) (string, error)
}

// =============================================================================
// STRUCT DEFINITIONS
// =============================================================================

// AnalysisOptions configures health analysis.
type AnalysisOptions struct {
	// ID is a unique identifier for this analysis request.
	ID string

	// TimeWindow is the lookback period for logs/metrics.
	TimeWindow time.Duration

	// Services to analyze (nil = all default services).
	Services []ServiceDefinition

	// IncludeLLMSummary enables LLM-generated summary.
	IncludeLLMSummary bool

	// MaxLogLines limits log analysis (default: 1000).
	MaxLogLines int

	// CreatedAt is when this request was created.
	CreatedAt time.Time
}

// IntelligentHealthReport is the comprehensive health analysis output.
type IntelligentHealthReport struct {
	// ID is a unique identifier for this report.
	ID string

	// Timestamp when analysis was performed.
	Timestamp time.Time

	// OverallState is the aggregate health state.
	OverallState IntelligentHealthState

	// Summary is the LLM-generated health summary (if requested).
	Summary string

	// Services contains per-service analysis.
	Services []ServiceInsights

	// FreshnessReports contains code freshness analysis.
	FreshnessReports []FreshnessReport

	// Alerts contains detected issues requiring attention.
	Alerts []HealthAlert

	// Recommendations contains suggested actions.
	Recommendations []string

	// Metrics contains aggregate metrics.
	Metrics *AggregateMetrics

	// Duration is how long the analysis took.
	Duration time.Duration

	// TraceID is the OpenTelemetry trace ID for correlation.
	// Enables "Support Ticket Revolution" - users can report trace ID
	// and support can view the entire analysis in Jaeger.
	TraceID string

	// CreatedAt is when this report was created.
	CreatedAt time.Time
}

// IntelligentHealthState represents nuanced health state.
type IntelligentHealthState string

const (
	// IntelligentStateHealthy - all services healthy, no anomalies.
	IntelligentStateHealthy IntelligentHealthState = "healthy"

	// IntelligentStateDegraded - running but with issues.
	IntelligentStateDegraded IntelligentHealthState = "degraded"

	// IntelligentStateAtRisk - trending toward failure.
	IntelligentStateAtRisk IntelligentHealthState = "at_risk"

	// IntelligentStateCritical - critical services failing.
	IntelligentStateCritical IntelligentHealthState = "critical"

	// IntelligentStateUnknown - cannot determine state.
	IntelligentStateUnknown IntelligentHealthState = "unknown"
)

// ServiceInsights contains detailed analysis for one service.
type ServiceInsights struct {
	// ID is a unique identifier for this insight.
	ID string

	// Name is the service name.
	Name string

	// BasicHealth from Phase 9 checker.
	BasicHealth HealthStatus

	// IntelligentState is the derived health state.
	IntelligentState IntelligentHealthState

	// LogAnomalies detected in logs.
	LogAnomalies []LogAnomaly

	// ErrorPatterns found in logs.
	ErrorPatterns []ErrorPattern

	// RecentErrors count in analysis window.
	RecentErrors int

	// LatencyP50 is 50th percentile latency.
	LatencyP50 time.Duration

	// LatencyP99 is 99th percentile latency.
	LatencyP99 time.Duration

	// LatencyTrend is the latency direction.
	LatencyTrend Trend

	// ErrorRate is the error rate (0-1).
	ErrorRate float64

	// ErrorRateTrend is the error rate direction.
	ErrorRateTrend Trend

	// MemoryUsageMB is current memory usage.
	MemoryUsageMB int64

	// MemoryTrend is the memory direction.
	MemoryTrend Trend

	// CPUPercent is current CPU usage.
	CPUPercent float64

	// CPUTrend is the CPU direction.
	CPUTrend Trend

	// ImageAge is how old the container image is.
	ImageAge time.Duration

	// LastCodeChange is when code was last changed.
	LastCodeChange time.Time

	// IsStale is true if container is older than code.
	IsStale bool

	// Insights are specific observations.
	Insights []string

	// Recommendations are suggested actions.
	Recommendations []string

	// CreatedAt is when this insight was generated.
	CreatedAt time.Time
}

// LogAnomaly represents a detected anomaly in logs.
type LogAnomaly struct {
	// ID is a unique identifier.
	ID string

	// Timestamp when detected.
	Timestamp time.Time

	// Severity of the anomaly.
	Severity string

	// Description of what was detected.
	Description string

	// LogSnippet showing the anomaly.
	LogSnippet string

	// Count of occurrences.
	Count int

	// CreatedAt is when this was detected.
	CreatedAt time.Time
}

// ErrorPattern represents a recurring error in logs.
type ErrorPattern struct {
	// ID is a unique identifier.
	ID string

	// Pattern that was matched.
	Pattern string

	// Count of occurrences.
	Count int

	// FirstSeen timestamp.
	FirstSeen time.Time

	// LastSeen timestamp.
	LastSeen time.Time

	// Example log line.
	Example string

	// Severity level.
	Severity string

	// CreatedAt is when this was detected.
	CreatedAt time.Time
}

// FreshnessReport compares container age to code age.
type FreshnessReport struct {
	// ID is a unique identifier.
	ID string

	// ServiceName is the service checked.
	ServiceName string

	// ContainerName is the container name.
	ContainerName string

	// ImageCreatedAt is when the image was built.
	ImageCreatedAt time.Time

	// ImageAge is how old the image is.
	ImageAge time.Duration

	// LastCodeCommit is when code was last changed.
	LastCodeCommit time.Time

	// CodeAge is how long since last commit.
	CodeAge time.Duration

	// IsStale is true if image is older than code.
	IsStale bool

	// StaleBy is how much older the image is.
	StaleBy time.Duration

	// SourcePath is the source code path.
	SourcePath string

	// CreatedAt is when this report was generated.
	CreatedAt time.Time
}

// MetricTrends contains performance trend analysis.
type MetricTrends struct {
	// ID is a unique identifier.
	ID string

	// ServiceName is the service analyzed.
	ServiceName string

	// TimeWindow is the analysis window.
	TimeWindow time.Duration

	// DataPoints is how many points were analyzed.
	DataPoints int

	// LatencyP50Current is current p50 latency.
	LatencyP50Current time.Duration

	// LatencyP50Baseline is baseline p50 latency.
	LatencyP50Baseline time.Duration

	// LatencyP99Current is current p99 latency.
	LatencyP99Current time.Duration

	// LatencyP99Baseline is baseline p99 latency.
	LatencyP99Baseline time.Duration

	// LatencyTrend is the latency direction.
	LatencyTrend Trend

	// ErrorRateCurrent is current error rate.
	ErrorRateCurrent float64

	// ErrorRateBaseline is baseline error rate.
	ErrorRateBaseline float64

	// ErrorRateTrend is the error rate direction.
	ErrorRateTrend Trend

	// MemoryUsageCurrent is current memory (MB).
	MemoryUsageCurrent int64

	// MemoryUsageBaseline is baseline memory (MB).
	MemoryUsageBaseline int64

	// MemoryTrend is the memory direction.
	MemoryTrend Trend

	// CPUUsageCurrent is current CPU percent.
	CPUUsageCurrent float64

	// CPUUsageBaseline is baseline CPU percent.
	CPUUsageBaseline float64

	// CPUTrend is the CPU direction.
	CPUTrend Trend

	// CreatedAt is when this was computed.
	CreatedAt time.Time
}

// Trend represents the direction of a metric.
type Trend string

const (
	// TrendStable indicates no significant change.
	TrendStable Trend = "stable"

	// TrendIncreasing indicates values are rising.
	TrendIncreasing Trend = "increasing"

	// TrendDecreasing indicates values are falling.
	TrendDecreasing Trend = "decreasing"

	// TrendUnknown indicates insufficient data.
	TrendUnknown Trend = "unknown"
)

// HealthAlert represents an issue requiring attention.
type HealthAlert struct {
	// ID is a unique identifier.
	ID string

	// Severity indicates importance.
	Severity AlertSeverity

	// Service is the affected service.
	Service string

	// Title is a short description.
	Title string

	// Description provides details.
	Description string

	// DetectedAt is when the alert was triggered.
	DetectedAt time.Time

	// Metric is which metric triggered this.
	Metric string

	// Value is the current value.
	Value string

	// Threshold is the expected value/range.
	Threshold string

	// CreatedAt is when this alert was created.
	CreatedAt time.Time
}

// AlertSeverity indicates alert importance.
type AlertSeverity string

const (
	// AlertSeverityInfo is informational.
	AlertSeverityInfo AlertSeverity = "info"

	// AlertSeverityWarning requires attention.
	AlertSeverityWarning AlertSeverity = "warning"

	// AlertSeverityError is a significant issue.
	AlertSeverityError AlertSeverity = "error"

	// AlertSeverityCritical requires immediate action.
	AlertSeverityCritical AlertSeverity = "critical"
)

// HealthDataBundle contains all data for LLM summarization.
type HealthDataBundle struct {
	// ID is a unique identifier.
	ID string

	// BasicStatuses from Phase 9 health checks.
	BasicStatuses []HealthStatus

	// ServiceInsights from analysis.
	ServiceInsights []ServiceInsights

	// RecentLogs maps service to recent logs.
	RecentLogs map[string]string

	// MetricSummary is a text summary of metrics.
	MetricSummary string

	// FreshnessReports for code staleness.
	FreshnessReports []FreshnessReport

	// Alerts requiring attention.
	Alerts []HealthAlert

	// CreatedAt is when this bundle was created.
	CreatedAt time.Time
}

// AggregateMetrics contains stack-wide metrics.
type AggregateMetrics struct {
	// ID is a unique identifier.
	ID string

	// TotalServices is the count of services.
	TotalServices int

	// HealthyServices count.
	HealthyServices int

	// DegradedServices count.
	DegradedServices int

	// FailingServices count.
	FailingServices int

	// AverageLatency across services.
	AverageLatency time.Duration

	// TotalErrorRate across services.
	TotalErrorRate float64

	// TotalMemoryMB used by all services.
	TotalMemoryMB int64

	// TotalCPUPercent used by all services.
	TotalCPUPercent float64

	// CreatedAt is when computed.
	CreatedAt time.Time
}

// IntelligenceConfig configures health intelligence.
type IntelligenceConfig struct {
	// ID is a unique identifier.
	ID string

	// OllamaHost is the Ollama API endpoint.
	OllamaHost string

	// SummaryModel is the model for LLM summaries.
	SummaryModel string

	// StackDir is the Aleutian stack directory.
	StackDir string

	// SourcePaths maps services to source directories.
	SourcePaths map[string]string

	// LatencyWarningThreshold for alerts.
	LatencyWarningThreshold time.Duration

	// LatencyErrorThreshold for alerts.
	LatencyErrorThreshold time.Duration

	// ErrorRateWarningThreshold for alerts.
	ErrorRateWarningThreshold float64

	// ErrorRateErrorThreshold for alerts.
	ErrorRateErrorThreshold float64

	// MemoryWarningThresholdMB for alerts.
	MemoryWarningThresholdMB int64

	// ErrorPatterns for log detection.
	ErrorPatterns []string

	// WarningPatterns for log detection.
	WarningPatterns []string

	// CreatedAt is when this config was created.
	CreatedAt time.Time
}

// DefaultHealthIntelligence implements HealthIntelligence.
type DefaultHealthIntelligence struct {
	checker   HealthChecker
	proc      process.Manager
	textGen   HealthTextGenerator
	metrics   MetricsStore
	sanitizer LogSanitizer
	config    IntelligenceConfig
	mu        sync.RWMutex

	// Compiled patterns
	errorPatterns   []*regexp.Regexp
	warningPatterns []*regexp.Regexp
}

// DefaultHealthTextGenerator implements HealthTextGenerator using HTTP to Ollama.
type DefaultHealthTextGenerator struct {
	host       string
	httpClient *http.Client
}

// MockHealthIntelligence is a test double.
type MockHealthIntelligence struct {
	AnalyzeHealthFunc      func(ctx context.Context, opts AnalysisOptions) (*IntelligentHealthReport, error)
	GetServiceInsightsFunc func(ctx context.Context, name string, opts AnalysisOptions) (*ServiceInsights, error)
	CheckCodeFreshnessFunc func(ctx context.Context, services []ServiceDefinition) ([]FreshnessReport, error)
	GetMetricTrendsFunc    func(ctx context.Context, name string, window time.Duration) (*MetricTrends, error)
	GenerateLLMSummaryFunc func(ctx context.Context, data *HealthDataBundle) (string, error)

	AnalyzeCalls []AnalysisOptions
	mu           sync.Mutex
}

// MockHealthTextGenerator is a test double for HealthTextGenerator.
type MockHealthTextGenerator struct {
	GenerateFunc func(ctx context.Context, model, prompt string) (string, error)
	Calls        []struct {
		Model  string
		Prompt string
	}
	mu sync.Mutex
}

// =============================================================================
// CONSTRUCTOR FUNCTIONS
// =============================================================================

// DefaultIntelligenceConfig returns sensible defaults.
func DefaultIntelligenceConfig(stackDir string) IntelligenceConfig {
	return IntelligenceConfig{
		ID:                        GenerateID(),
		OllamaHost:                "http://localhost:11434",
		SummaryModel:              "gemma3:1b",
		StackDir:                  stackDir,
		SourcePaths:               defaultSourcePaths(),
		LatencyWarningThreshold:   500 * time.Millisecond,
		LatencyErrorThreshold:     2 * time.Second,
		ErrorRateWarningThreshold: 0.01,
		ErrorRateErrorThreshold:   0.05,
		MemoryWarningThresholdMB:  1024,
		ErrorPatterns:             defaultErrorPatterns,
		WarningPatterns:           defaultWarningPatterns,
		CreatedAt:                 time.Now(),
	}
}

// DefaultAnalysisOptions returns sensible defaults.
func DefaultAnalysisOptions() AnalysisOptions {
	return AnalysisOptions{
		ID:                GenerateID(),
		TimeWindow:        5 * time.Minute,
		Services:          nil, // Use defaults
		IncludeLLMSummary: false,
		MaxLogLines:       1000,
		CreatedAt:         time.Now(),
	}
}

// NewDefaultHealthIntelligence creates a health intelligence instance.
func NewDefaultHealthIntelligence(
	checker HealthChecker,
	proc process.Manager,
	textGen HealthTextGenerator,
	metrics MetricsStore,
	sanitizer LogSanitizer,
	config IntelligenceConfig,
) *DefaultHealthIntelligence {
	intel := &DefaultHealthIntelligence{
		checker:   checker,
		proc:      proc,
		textGen:   textGen,
		metrics:   metrics,
		sanitizer: sanitizer,
		config:    config,
	}

	// Compile patterns
	for _, p := range config.ErrorPatterns {
		if re, err := regexp.Compile(p); err == nil {
			intel.errorPatterns = append(intel.errorPatterns, re)
		}
	}
	for _, p := range config.WarningPatterns {
		if re, err := regexp.Compile(p); err == nil {
			intel.warningPatterns = append(intel.warningPatterns, re)
		}
	}

	return intel
}

// NewDefaultHealthTextGenerator creates a text generator using Ollama.
func NewDefaultHealthTextGenerator(host string) *DefaultHealthTextGenerator {
	return &DefaultHealthTextGenerator{
		host: host,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// =============================================================================
// DEFAULT PATTERNS
// =============================================================================

var defaultErrorPatterns = []string{
	`(?i)error`,
	`(?i)exception`,
	`(?i)failed`,
	`(?i)panic`,
	`(?i)fatal`,
	`(?i)timeout`,
	`(?i)connection refused`,
	`(?i)out of memory`,
	`(?i)oom`,
}

var defaultWarningPatterns = []string{
	`(?i)warning`,
	`(?i)deprecated`,
	`(?i)retry`,
	`(?i)slow`,
	`(?i)high latency`,
}

func defaultSourcePaths() map[string]string {
	return map[string]string{
		"orchestrator": "services/orchestrator",
		"rag-engine":   "services/rag_engine",
		"forecast":     "services/forecast",
		"data-fetcher": "services/data_fetcher",
	}
}

// =============================================================================
// LLM PROMPT TEMPLATE
// =============================================================================

const healthSummaryPromptTemplate = `You are a system health analyzer for Aleutian, a local AI platform.

Analyze the following health data and provide a concise summary (2-4 sentences):

**Service Status:**
{{range .BasicStatuses}}
- {{.Name}}: {{.State}}{{if .Message}} ({{.Message}}){{end}}
{{end}}

**Detected Issues:**
{{range .Alerts}}
- [{{.Severity}}] {{.Service}}: {{.Title}}
{{end}}

**Metrics Summary:**
{{.MetricSummary}}

Provide:
1. Overall health status (healthy/degraded/critical)
2. Key issues requiring attention (if any)
3. One actionable recommendation (if needed)

Be concise and actionable. Focus on what the user should know or do.`

// =============================================================================
// DefaultHealthIntelligence METHODS
// =============================================================================

// AnalyzeHealth performs comprehensive health analysis.
func (d *DefaultHealthIntelligence) AnalyzeHealth(ctx context.Context, opts AnalysisOptions) (*IntelligentHealthReport, error) {
	start := time.Now()

	// Use default services if not specified
	services := opts.Services
	if services == nil {
		services = DefaultServiceDefinitions()
	}

	// Get basic health statuses
	statuses, err := d.checker.CheckAllServices(ctx, services)
	if err != nil {
		return nil, fmt.Errorf("%w: basic health check: %v", ErrAnalysisFailed, err)
	}

	// Analyze each service
	var serviceInsights []ServiceInsights
	var alerts []HealthAlert
	for _, svc := range services {
		insight, err := d.GetServiceInsights(ctx, svc.Name, opts)
		if err != nil {
			// Log error but continue with partial data
			continue
		}
		serviceInsights = append(serviceInsights, *insight)
		alerts = append(alerts, d.generateAlertsForService(insight)...)
	}

	// Check code freshness
	freshnessReports, _ := d.CheckCodeFreshness(ctx, services)

	// Calculate aggregate metrics
	aggregateMetrics := d.calculateAggregateMetrics(serviceInsights)

	// Determine overall state
	overallState := d.determineOverallState(serviceInsights, alerts)

	// Generate recommendations
	recommendations := d.generateRecommendations(serviceInsights, alerts)

	report := &IntelligentHealthReport{
		ID:               GenerateID(),
		Timestamp:        time.Now(),
		OverallState:     overallState,
		Services:         serviceInsights,
		FreshnessReports: freshnessReports,
		Alerts:           alerts,
		Recommendations:  recommendations,
		Metrics:          aggregateMetrics,
		Duration:         time.Since(start),
		CreatedAt:        time.Now(),
	}

	// Generate LLM summary if requested
	if opts.IncludeLLMSummary && d.textGen != nil {
		bundle := &HealthDataBundle{
			ID:               GenerateID(),
			BasicStatuses:    statuses,
			ServiceInsights:  serviceInsights,
			FreshnessReports: freshnessReports,
			Alerts:           alerts,
			MetricSummary:    d.formatMetricSummary(aggregateMetrics),
			CreatedAt:        time.Now(),
		}
		if summary, err := d.GenerateLLMSummary(ctx, bundle); err == nil {
			report.Summary = summary
		}
	}

	return report, nil
}

// GetServiceInsights returns detailed insights for one service.
func (d *DefaultHealthIntelligence) GetServiceInsights(ctx context.Context, serviceName string, opts AnalysisOptions) (*ServiceInsights, error) {
	insight := &ServiceInsights{
		ID:        GenerateID(),
		Name:      serviceName,
		CreatedAt: time.Now(),
	}

	// Find service definition
	var svcDef *ServiceDefinition
	services := opts.Services
	if services == nil {
		services = DefaultServiceDefinitions()
	}
	for i := range services {
		if services[i].Name == serviceName {
			svcDef = &services[i]
			break
		}
	}

	if svcDef == nil {
		return nil, fmt.Errorf("service %q not found", serviceName)
	}

	// Get basic health
	status, err := d.checker.CheckService(ctx, *svcDef)
	if err == nil && status != nil {
		insight.BasicHealth = *status
	}

	// Analyze logs if container exists
	if svcDef.ContainerName != "" {
		logs, err := d.collectLogs(ctx, svcDef.ContainerName, opts.TimeWindow, opts.MaxLogLines)
		if err == nil {
			anomalies, patterns := d.analyzeLogs(logs)
			insight.LogAnomalies = anomalies
			insight.ErrorPatterns = patterns

			// Count recent errors
			for _, p := range patterns {
				if p.Severity == "error" {
					insight.RecentErrors += p.Count
				}
			}
		}
	}

	// Get metric trends
	if d.metrics != nil {
		trends, err := d.GetMetricTrends(ctx, serviceName, opts.TimeWindow)
		if err == nil && trends != nil {
			insight.LatencyP50 = trends.LatencyP50Current
			insight.LatencyP99 = trends.LatencyP99Current
			insight.LatencyTrend = trends.LatencyTrend
			insight.ErrorRate = trends.ErrorRateCurrent
			insight.ErrorRateTrend = trends.ErrorRateTrend
			insight.MemoryUsageMB = trends.MemoryUsageCurrent
			insight.MemoryTrend = trends.MemoryTrend
			insight.CPUPercent = trends.CPUUsageCurrent
			insight.CPUTrend = trends.CPUTrend
		}
	}

	// Determine intelligent state
	insight.IntelligentState = d.determineServiceState(insight)

	// Generate insights and recommendations
	insight.Insights = d.generateInsights(insight)
	insight.Recommendations = d.generateServiceRecommendations(insight)

	return insight, nil
}

// CheckCodeFreshness compares container age vs source code.
func (d *DefaultHealthIntelligence) CheckCodeFreshness(ctx context.Context, services []ServiceDefinition) ([]FreshnessReport, error) {
	var reports []FreshnessReport

	for _, svc := range services {
		if svc.ContainerName == "" {
			continue
		}

		report := FreshnessReport{
			ID:            GenerateID(),
			ServiceName:   svc.Name,
			ContainerName: svc.ContainerName,
			CreatedAt:     time.Now(),
		}

		// Get container image creation time
		output, _, _, err := d.proc.RunInDir(ctx, "", nil, "podman", "inspect",
			svc.ContainerName, "--format", "{{.Created}}")
		if err == nil {
			output = strings.TrimSpace(output)
			if t, err := time.Parse(time.RFC3339, output); err == nil {
				report.ImageCreatedAt = t
				report.ImageAge = time.Since(t)
			}
		}

		// Get last git commit for source
		sourcePath, ok := d.config.SourcePaths[svc.Name]
		if !ok {
			sourcePath = d.config.SourcePaths[strings.ToLower(svc.Name)]
		}
		if sourcePath != "" {
			report.SourcePath = sourcePath
			output, _, _, err := d.proc.RunInDir(ctx, d.config.StackDir, nil, "git", "log",
				"-1", "--format=%ct", "--", sourcePath)
			if err == nil {
				output = strings.TrimSpace(output)
				var timestamp int64
				if _, err := fmt.Sscanf(output, "%d", &timestamp); err == nil {
					report.LastCodeCommit = time.Unix(timestamp, 0)
					report.CodeAge = time.Since(report.LastCodeCommit)
				}
			}
		}

		// Determine staleness
		if !report.ImageCreatedAt.IsZero() && !report.LastCodeCommit.IsZero() {
			if report.ImageCreatedAt.Before(report.LastCodeCommit) {
				report.IsStale = true
				report.StaleBy = report.LastCodeCommit.Sub(report.ImageCreatedAt)
			}
		}

		reports = append(reports, report)
	}

	return reports, nil
}

// GetMetricTrends returns performance trends over time.
func (d *DefaultHealthIntelligence) GetMetricTrends(ctx context.Context, serviceName string, window time.Duration) (*MetricTrends, error) {
	if d.metrics == nil {
		return nil, ErrMetricsUnavailable
	}

	now := time.Now()
	start := now.Add(-window)

	trends := &MetricTrends{
		ID:          GenerateID(),
		ServiceName: serviceName,
		TimeWindow:  window,
		CreatedAt:   time.Now(),
	}

	// Get latency baseline and current
	latencyBaseline := d.metrics.GetBaseline(serviceName, "latency_p99", 24*time.Hour)
	latencyPoints := d.metrics.Query(serviceName, "latency_p99", start, now)

	if latencyBaseline != nil {
		trends.LatencyP50Baseline = time.Duration(latencyBaseline.P50) * time.Millisecond
		trends.LatencyP99Baseline = time.Duration(latencyBaseline.P99) * time.Millisecond
	}

	if len(latencyPoints) > 0 {
		trends.DataPoints = len(latencyPoints)
		trends.LatencyP99Current = time.Duration(latencyPoints[len(latencyPoints)-1].Value) * time.Millisecond
		trends.LatencyTrend = d.calculateTrend(latencyPoints)
	}

	// Get error rate
	errorBaseline := d.metrics.GetBaseline(serviceName, "error_rate", 24*time.Hour)
	errorPoints := d.metrics.Query(serviceName, "error_rate", start, now)

	if errorBaseline != nil {
		trends.ErrorRateBaseline = errorBaseline.Mean
	}

	if len(errorPoints) > 0 {
		trends.ErrorRateCurrent = errorPoints[len(errorPoints)-1].Value
		trends.ErrorRateTrend = d.calculateTrend(errorPoints)
	}

	// Get memory
	memoryBaseline := d.metrics.GetBaseline(serviceName, "memory_mb", 24*time.Hour)
	memoryPoints := d.metrics.Query(serviceName, "memory_mb", start, now)

	if memoryBaseline != nil {
		trends.MemoryUsageBaseline = int64(memoryBaseline.Mean)
	}

	if len(memoryPoints) > 0 {
		trends.MemoryUsageCurrent = int64(memoryPoints[len(memoryPoints)-1].Value)
		trends.MemoryTrend = d.calculateTrend(memoryPoints)
	}

	// Get CPU
	cpuPoints := d.metrics.Query(serviceName, "cpu_percent", start, now)
	if len(cpuPoints) > 0 {
		trends.CPUUsageCurrent = cpuPoints[len(cpuPoints)-1].Value
		trends.CPUTrend = d.calculateTrend(cpuPoints)
	}

	return trends, nil
}

// GenerateLLMSummary creates an AI-generated health summary.
func (d *DefaultHealthIntelligence) GenerateLLMSummary(ctx context.Context, data *HealthDataBundle) (string, error) {
	if d.textGen == nil {
		return "", ErrLLMUnavailable
	}

	// Build prompt from template
	tmpl, err := template.New("health").Parse(healthSummaryPromptTemplate)
	if err != nil {
		return "", fmt.Errorf("failed to parse template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("failed to execute template: %w", err)
	}

	// Generate summary
	summary, err := d.textGen.Generate(ctx, d.config.SummaryModel, buf.String())
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrLLMUnavailable, err)
	}

	return strings.TrimSpace(summary), nil
}

// =============================================================================
// HELPER METHODS
// =============================================================================

func (d *DefaultHealthIntelligence) collectLogs(ctx context.Context, containerName string, since time.Duration, maxLines int) (string, error) {
	args := []string{
		"logs",
		"--since", fmt.Sprintf("%dm", int(since.Minutes())),
		"--tail", fmt.Sprintf("%d", maxLines),
		containerName,
	}

	stdout, _, _, err := d.proc.RunInDir(ctx, "", nil, "podman", args...)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrLogCollectionFailed, err)
	}

	// Sanitize before returning
	if d.sanitizer != nil {
		return d.sanitizer.Sanitize(stdout), nil
	}
	return stdout, nil
}

func (d *DefaultHealthIntelligence) analyzeLogs(logs string) ([]LogAnomaly, []ErrorPattern) {
	var anomalies []LogAnomaly
	patternCounts := make(map[string]int)
	patternExamples := make(map[string]string)
	patternSeverity := make(map[string]string)

	lines := strings.Split(logs, "\n")
	now := time.Now()

	for _, line := range lines {
		// Check error patterns
		for _, re := range d.errorPatterns {
			if re.MatchString(line) {
				pattern := re.String()
				patternCounts[pattern]++
				patternExamples[pattern] = line
				patternSeverity[pattern] = "error"
			}
		}

		// Check warning patterns
		for _, re := range d.warningPatterns {
			if re.MatchString(line) {
				pattern := re.String()
				patternCounts[pattern]++
				patternExamples[pattern] = line
				if patternSeverity[pattern] == "" {
					patternSeverity[pattern] = "warning"
				}
			}
		}
	}

	// Convert to ErrorPatterns
	var patterns []ErrorPattern
	for pattern, count := range patternCounts {
		patterns = append(patterns, ErrorPattern{
			ID:        GenerateID(),
			Pattern:   pattern,
			Count:     count,
			FirstSeen: now,
			LastSeen:  now,
			Example:   patternExamples[pattern],
			Severity:  patternSeverity[pattern],
			CreatedAt: now,
		})
	}

	// Sort by count descending
	sort.Slice(patterns, func(i, j int) bool {
		return patterns[i].Count > patterns[j].Count
	})

	// Generate anomalies for significant patterns
	for _, p := range patterns {
		if p.Count >= 3 { // Threshold for anomaly
			anomalies = append(anomalies, LogAnomaly{
				ID:          GenerateID(),
				Timestamp:   now,
				Severity:    p.Severity,
				Description: fmt.Sprintf("Pattern '%s' occurred %d times", p.Pattern, p.Count),
				LogSnippet:  p.Example,
				Count:       p.Count,
				CreatedAt:   now,
			})
		}
	}

	return anomalies, patterns
}

func (d *DefaultHealthIntelligence) calculateTrend(points []MetricPoint) Trend {
	if len(points) < 2 {
		return TrendUnknown
	}

	// Simple trend: compare first half to second half
	mid := len(points) / 2
	var firstHalf, secondHalf float64

	for i, p := range points {
		if i < mid {
			firstHalf += p.Value
		} else {
			secondHalf += p.Value
		}
	}

	firstAvg := firstHalf / float64(mid)
	secondAvg := secondHalf / float64(len(points)-mid)

	// 10% change threshold
	change := (secondAvg - firstAvg) / firstAvg
	if change > 0.1 {
		return TrendIncreasing
	} else if change < -0.1 {
		return TrendDecreasing
	}
	return TrendStable
}

func (d *DefaultHealthIntelligence) determineServiceState(insight *ServiceInsights) IntelligentHealthState {
	if insight.BasicHealth.State == HealthStateUnhealthy || insight.BasicHealth.State == HealthStateUnreachable {
		return IntelligentStateCritical
	}

	// Check for degradation signals
	degraded := false

	if insight.LatencyP99 > d.config.LatencyWarningThreshold {
		degraded = true
	}
	if insight.ErrorRate > d.config.ErrorRateWarningThreshold {
		degraded = true
	}
	if insight.RecentErrors > 5 {
		degraded = true
	}

	if degraded {
		if insight.LatencyP99 > d.config.LatencyErrorThreshold ||
			insight.ErrorRate > d.config.ErrorRateErrorThreshold ||
			insight.RecentErrors > 20 {
			return IntelligentStateAtRisk
		}
		return IntelligentStateDegraded
	}

	return IntelligentStateHealthy
}

func (d *DefaultHealthIntelligence) determineOverallState(insights []ServiceInsights, alerts []HealthAlert) IntelligentHealthState {
	hasCritical := false
	hasAtRisk := false
	hasDegraded := false

	for _, insight := range insights {
		switch insight.IntelligentState {
		case IntelligentStateCritical:
			hasCritical = true
		case IntelligentStateAtRisk:
			hasAtRisk = true
		case IntelligentStateDegraded:
			hasDegraded = true
		}
	}

	// Check alert severity
	for _, alert := range alerts {
		if alert.Severity == AlertSeverityCritical {
			hasCritical = true
		}
	}

	if hasCritical {
		return IntelligentStateCritical
	}
	if hasAtRisk {
		return IntelligentStateAtRisk
	}
	if hasDegraded {
		return IntelligentStateDegraded
	}
	return IntelligentStateHealthy
}

func (d *DefaultHealthIntelligence) generateAlertsForService(insight *ServiceInsights) []HealthAlert {
	var alerts []HealthAlert
	now := time.Now()

	if insight.LatencyP99 > d.config.LatencyErrorThreshold {
		alerts = append(alerts, HealthAlert{
			ID:          GenerateID(),
			Severity:    AlertSeverityError,
			Service:     insight.Name,
			Title:       "High latency",
			Description: fmt.Sprintf("P99 latency %v exceeds threshold %v", insight.LatencyP99, d.config.LatencyErrorThreshold),
			DetectedAt:  now,
			Metric:      "latency_p99",
			Value:       insight.LatencyP99.String(),
			Threshold:   d.config.LatencyErrorThreshold.String(),
			CreatedAt:   now,
		})
	} else if insight.LatencyP99 > d.config.LatencyWarningThreshold {
		alerts = append(alerts, HealthAlert{
			ID:          GenerateID(),
			Severity:    AlertSeverityWarning,
			Service:     insight.Name,
			Title:       "Elevated latency",
			Description: fmt.Sprintf("P99 latency %v above warning threshold %v", insight.LatencyP99, d.config.LatencyWarningThreshold),
			DetectedAt:  now,
			Metric:      "latency_p99",
			Value:       insight.LatencyP99.String(),
			Threshold:   d.config.LatencyWarningThreshold.String(),
			CreatedAt:   now,
		})
	}

	if insight.ErrorRate > d.config.ErrorRateErrorThreshold {
		alerts = append(alerts, HealthAlert{
			ID:          GenerateID(),
			Severity:    AlertSeverityError,
			Service:     insight.Name,
			Title:       "High error rate",
			Description: fmt.Sprintf("Error rate %.2f%% exceeds threshold %.2f%%", insight.ErrorRate*100, d.config.ErrorRateErrorThreshold*100),
			DetectedAt:  now,
			Metric:      "error_rate",
			Value:       fmt.Sprintf("%.2f%%", insight.ErrorRate*100),
			Threshold:   fmt.Sprintf("%.2f%%", d.config.ErrorRateErrorThreshold*100),
			CreatedAt:   now,
		})
	}

	if insight.IsStale {
		alerts = append(alerts, HealthAlert{
			ID:          GenerateID(),
			Severity:    AlertSeverityWarning,
			Service:     insight.Name,
			Title:       "Stale code",
			Description: "Container is running code older than latest commit",
			DetectedAt:  now,
			Metric:      "code_freshness",
			Value:       insight.ImageAge.String(),
			Threshold:   "code changed after image built",
			CreatedAt:   now,
		})
	}

	return alerts
}

func (d *DefaultHealthIntelligence) generateInsights(insight *ServiceInsights) []string {
	var insights []string

	if insight.LatencyTrend == TrendIncreasing {
		insights = append(insights, "Latency is trending upward")
	}
	if insight.ErrorRateTrend == TrendIncreasing {
		insights = append(insights, "Error rate is increasing")
	}
	if insight.MemoryTrend == TrendIncreasing {
		insights = append(insights, "Memory usage is growing")
	}
	if len(insight.LogAnomalies) > 0 {
		insights = append(insights, fmt.Sprintf("Detected %d log anomalies", len(insight.LogAnomalies)))
	}
	if insight.IsStale {
		insights = append(insights, "Running stale code - consider rebuilding")
	}

	return insights
}

func (d *DefaultHealthIntelligence) generateServiceRecommendations(insight *ServiceInsights) []string {
	var recs []string

	if insight.LatencyP99 > d.config.LatencyWarningThreshold {
		recs = append(recs, "Investigate latency issues - check downstream dependencies")
	}
	if insight.RecentErrors > 5 {
		recs = append(recs, "Review recent error logs for root cause")
	}
	if insight.IsStale {
		recs = append(recs, "Rebuild container with latest code: aleutian stack up --build")
	}
	if insight.MemoryUsageMB > d.config.MemoryWarningThresholdMB {
		recs = append(recs, "Memory usage is high - consider increasing limits or investigating leaks")
	}

	return recs
}

func (d *DefaultHealthIntelligence) generateRecommendations(insights []ServiceInsights, alerts []HealthAlert) []string {
	var recs []string

	// Collect unique recommendations
	seen := make(map[string]bool)
	for _, insight := range insights {
		for _, rec := range insight.Recommendations {
			if !seen[rec] {
				recs = append(recs, rec)
				seen[rec] = true
			}
		}
	}

	// Add alert-based recommendations
	for _, alert := range alerts {
		if alert.Severity == AlertSeverityCritical || alert.Severity == AlertSeverityError {
			rec := fmt.Sprintf("Address %s issue in %s", alert.Title, alert.Service)
			if !seen[rec] {
				recs = append(recs, rec)
				seen[rec] = true
			}
		}
	}

	return recs
}

func (d *DefaultHealthIntelligence) calculateAggregateMetrics(insights []ServiceInsights) *AggregateMetrics {
	metrics := &AggregateMetrics{
		ID:            GenerateID(),
		TotalServices: len(insights),
		CreatedAt:     time.Now(),
	}

	var totalLatency time.Duration
	var totalErrors float64
	latencyCount := 0

	for _, insight := range insights {
		switch insight.IntelligentState {
		case IntelligentStateHealthy:
			metrics.HealthyServices++
		case IntelligentStateDegraded, IntelligentStateAtRisk:
			metrics.DegradedServices++
		case IntelligentStateCritical:
			metrics.FailingServices++
		}

		if insight.LatencyP99 > 0 {
			totalLatency += insight.LatencyP99
			latencyCount++
		}
		totalErrors += insight.ErrorRate
		metrics.TotalMemoryMB += insight.MemoryUsageMB
		metrics.TotalCPUPercent += insight.CPUPercent
	}

	if latencyCount > 0 {
		metrics.AverageLatency = totalLatency / time.Duration(latencyCount)
	}
	if len(insights) > 0 {
		metrics.TotalErrorRate = totalErrors / float64(len(insights))
	}

	return metrics
}

func (d *DefaultHealthIntelligence) formatMetricSummary(metrics *AggregateMetrics) string {
	return fmt.Sprintf("%d/%d services healthy, avg latency %v, error rate %.2f%%, memory %dMB, CPU %.1f%%",
		metrics.HealthyServices, metrics.TotalServices,
		metrics.AverageLatency,
		metrics.TotalErrorRate*100,
		metrics.TotalMemoryMB,
		metrics.TotalCPUPercent)
}

// =============================================================================
// DefaultHealthTextGenerator METHODS
// =============================================================================

// Generate produces text from a prompt using Ollama API.
func (c *DefaultHealthTextGenerator) Generate(ctx context.Context, model, prompt string) (string, error) {
	reqBody := map[string]interface{}{
		"model":  model,
		"prompt": prompt,
		"stream": false,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.host+"/api/generate", bytes.NewReader(jsonBody))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to call Ollama: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("Ollama returned status %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Response string `json:"response"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	return result.Response, nil
}

// =============================================================================
// MockHealthIntelligence METHODS
// =============================================================================

func (m *MockHealthIntelligence) AnalyzeHealth(ctx context.Context, opts AnalysisOptions) (*IntelligentHealthReport, error) {
	m.mu.Lock()
	m.AnalyzeCalls = append(m.AnalyzeCalls, opts)
	m.mu.Unlock()

	if m.AnalyzeHealthFunc != nil {
		return m.AnalyzeHealthFunc(ctx, opts)
	}
	return &IntelligentHealthReport{
		ID:           GenerateID(),
		Timestamp:    time.Now(),
		OverallState: IntelligentStateHealthy,
		CreatedAt:    time.Now(),
	}, nil
}

func (m *MockHealthIntelligence) GetServiceInsights(ctx context.Context, name string, opts AnalysisOptions) (*ServiceInsights, error) {
	if m.GetServiceInsightsFunc != nil {
		return m.GetServiceInsightsFunc(ctx, name, opts)
	}
	return &ServiceInsights{
		ID:               GenerateID(),
		Name:             name,
		IntelligentState: IntelligentStateHealthy,
		CreatedAt:        time.Now(),
	}, nil
}

func (m *MockHealthIntelligence) CheckCodeFreshness(ctx context.Context, services []ServiceDefinition) ([]FreshnessReport, error) {
	if m.CheckCodeFreshnessFunc != nil {
		return m.CheckCodeFreshnessFunc(ctx, services)
	}
	return nil, nil
}

func (m *MockHealthIntelligence) GetMetricTrends(ctx context.Context, name string, window time.Duration) (*MetricTrends, error) {
	if m.GetMetricTrendsFunc != nil {
		return m.GetMetricTrendsFunc(ctx, name, window)
	}
	return &MetricTrends{
		ID:           GenerateID(),
		ServiceName:  name,
		TimeWindow:   window,
		LatencyTrend: TrendStable,
		CreatedAt:    time.Now(),
	}, nil
}

func (m *MockHealthIntelligence) GenerateLLMSummary(ctx context.Context, data *HealthDataBundle) (string, error) {
	if m.GenerateLLMSummaryFunc != nil {
		return m.GenerateLLMSummaryFunc(ctx, data)
	}
	return "All services healthy.", nil
}

// =============================================================================
// MockHealthTextGenerator METHODS
// =============================================================================

func (m *MockHealthTextGenerator) Generate(ctx context.Context, model, prompt string) (string, error) {
	m.mu.Lock()
	m.Calls = append(m.Calls, struct {
		Model  string
		Prompt string
	}{model, prompt})
	m.mu.Unlock()

	if m.GenerateFunc != nil {
		return m.GenerateFunc(ctx, model, prompt)
	}
	return "Mock LLM response: All services are healthy.", nil
}

// =============================================================================
// COMPILE-TIME INTERFACE CHECKS
// =============================================================================

// Verify that implementations satisfy the interfaces.
var (
	_ HealthIntelligence  = (*DefaultHealthIntelligence)(nil)
	_ HealthIntelligence  = (*MockHealthIntelligence)(nil)
	_ HealthTextGenerator = (*DefaultHealthTextGenerator)(nil)
	_ HealthTextGenerator = (*MockHealthTextGenerator)(nil)
)
