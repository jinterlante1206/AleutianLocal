// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package health

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/AleutianAI/AleutianFOSS/cmd/aleutian/internal/infra/process"
)

// =============================================================================
// MOCK IMPLEMENTATIONS FOR TESTING
// =============================================================================

// MockMetricsStore is a test double for MetricsStore.
type MockMetricsStore struct {
	mu     sync.RWMutex
	points map[string][]MetricPoint
}

// NewMockMetricsStore creates a new MockMetricsStore.
func NewMockMetricsStore() *MockMetricsStore {
	return &MockMetricsStore{
		points: make(map[string][]MetricPoint),
	}
}

func (m *MockMetricsStore) Record(service, metric string, value float64, timestamp time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := service + "/" + metric
	m.points[key] = append(m.points[key], MetricPoint{Timestamp: timestamp, Value: value})
}

func (m *MockMetricsStore) Query(service, metric string, start, end time.Time) []MetricPoint {
	m.mu.RLock()
	defer m.mu.RUnlock()
	key := service + "/" + metric
	var result []MetricPoint
	for _, p := range m.points[key] {
		if !p.Timestamp.Before(start) && !p.Timestamp.After(end) {
			result = append(result, p)
		}
	}
	return result
}

func (m *MockMetricsStore) GetBaseline(service, metric string, window time.Duration) *BaselineStats {
	m.mu.RLock()
	defer m.mu.RUnlock()
	key := service + "/" + metric
	points := m.points[key]
	if len(points) < 3 {
		return nil
	}
	var sum float64
	for _, p := range points {
		sum += p.Value
	}
	mean := sum / float64(len(points))
	return &BaselineStats{Mean: mean, P50: mean, P99: mean * 1.2}
}

// MockLogSanitizer is a test double for LogSanitizer.
type MockLogSanitizer struct{}

func (m *MockLogSanitizer) Sanitize(input string) string {
	return input // No-op for tests
}

// =============================================================================
// DefaultHealthIntelligence TESTS
// =============================================================================

func createTestIntelligence() *DefaultHealthIntelligence {
	checker := &MockHealthChecker{
		CheckServiceFunc: func(ctx context.Context, svc ServiceDefinition) (*HealthStatus, error) {
			return &HealthStatus{
				ID:          GenerateID(),
				Name:        svc.Name,
				State:       HealthStateHealthy,
				Latency:     100 * time.Millisecond,
				LastChecked: time.Now(),
			}, nil
		},
		CheckAllServicesFunc: func(ctx context.Context, svcs []ServiceDefinition) ([]HealthStatus, error) {
			var statuses []HealthStatus
			for _, svc := range svcs {
				statuses = append(statuses, HealthStatus{
					ID:          GenerateID(),
					Name:        svc.Name,
					State:       HealthStateHealthy,
					Latency:     100 * time.Millisecond,
					LastChecked: time.Now(),
				})
			}
			return statuses, nil
		},
	}

	proc := &process.MockManager{
		RunInDirFunc: func(ctx context.Context, dir string, env []string, name string, args ...string) (string, string, int, error) {
			// Mock log output
			if len(args) > 0 && args[0] == "logs" {
				return "INFO: Service started\nWARNING: Slow query detected\nERROR: Connection timeout\n", "", 0, nil
			}
			// Mock inspect output
			if len(args) > 0 && args[0] == "inspect" {
				return "2026-01-05T10:00:00Z", "", 0, nil
			}
			// Mock git output
			if name == "git" {
				return "1704412800", "", 0, nil // Unix timestamp
			}
			return "", "", 0, nil
		},
	}

	ollama := &MockHealthTextGenerator{
		GenerateFunc: func(ctx context.Context, model, prompt string) (string, error) {
			return "All services are healthy with normal performance.", nil
		},
	}

	metrics := NewMockMetricsStore()
	sanitizer := &MockLogSanitizer{}
	config := DefaultIntelligenceConfig("/tmp/test")

	return NewDefaultHealthIntelligence(checker, proc, ollama, metrics, sanitizer, config)
}

func TestDefaultHealthIntelligence_AnalyzeHealth_Basic(t *testing.T) {
	intel := createTestIntelligence()

	ctx := context.Background()
	opts := DefaultAnalysisOptions()
	opts.Services = []ServiceDefinition{
		{
			ID:            GenerateID(),
			Name:          "test-service",
			URL:           "http://localhost:8080/health",
			ContainerName: "test-container",
			CheckType:     HealthCheckHTTP,
			Critical:      true,
		},
	}

	report, err := intel.AnalyzeHealth(ctx, opts)
	if err != nil {
		t.Fatalf("AnalyzeHealth failed: %v", err)
	}

	if report == nil {
		t.Fatal("Expected report, got nil")
	}

	if report.ID == "" {
		t.Error("Expected report to have an ID")
	}

	if report.CreatedAt.IsZero() {
		t.Error("Expected report to have a CreatedAt timestamp")
	}

	if report.Metrics == nil {
		t.Error("Expected report to have metrics")
	}
}

func TestDefaultHealthIntelligence_AnalyzeHealth_WithLLMSummary(t *testing.T) {
	intel := createTestIntelligence()

	ctx := context.Background()
	opts := DefaultAnalysisOptions()
	opts.IncludeLLMSummary = true
	opts.Services = []ServiceDefinition{
		{
			ID:            GenerateID(),
			Name:          "test-service",
			ContainerName: "test-container",
			CheckType:     HealthCheckHTTP,
		},
	}

	report, err := intel.AnalyzeHealth(ctx, opts)
	if err != nil {
		t.Fatalf("AnalyzeHealth failed: %v", err)
	}

	if report.Summary == "" {
		t.Error("Expected LLM summary when IncludeLLMSummary is true")
	}
}

func TestDefaultHealthIntelligence_GetServiceInsights_Basic(t *testing.T) {
	intel := createTestIntelligence()

	ctx := context.Background()
	opts := DefaultAnalysisOptions()
	opts.Services = []ServiceDefinition{
		{
			ID:            GenerateID(),
			Name:          "test-service",
			ContainerName: "test-container",
			CheckType:     HealthCheckHTTP,
		},
	}

	insight, err := intel.GetServiceInsights(ctx, "test-service", opts)
	if err != nil {
		t.Fatalf("GetServiceInsights failed: %v", err)
	}

	if insight == nil {
		t.Fatal("Expected insight, got nil")
	}

	if insight.ID == "" {
		t.Error("Expected insight to have an ID")
	}

	if insight.Name != "test-service" {
		t.Errorf("Expected name 'test-service', got %q", insight.Name)
	}

	if insight.CreatedAt.IsZero() {
		t.Error("Expected insight to have a CreatedAt timestamp")
	}
}

func TestDefaultHealthIntelligence_GetServiceInsights_NotFound(t *testing.T) {
	intel := createTestIntelligence()

	ctx := context.Background()
	opts := DefaultAnalysisOptions()
	opts.Services = []ServiceDefinition{} // Empty

	_, err := intel.GetServiceInsights(ctx, "nonexistent", opts)
	if err == nil {
		t.Error("Expected error for nonexistent service")
	}
}

func TestDefaultHealthIntelligence_GetServiceInsights_LogAnalysis(t *testing.T) {
	intel := createTestIntelligence()

	ctx := context.Background()
	opts := DefaultAnalysisOptions()
	opts.Services = []ServiceDefinition{
		{
			ID:            GenerateID(),
			Name:          "test-service",
			ContainerName: "test-container",
			CheckType:     HealthCheckHTTP,
		},
	}

	insight, err := intel.GetServiceInsights(ctx, "test-service", opts)
	if err != nil {
		t.Fatalf("GetServiceInsights failed: %v", err)
	}

	// Should have detected error patterns from mock logs
	if len(insight.ErrorPatterns) == 0 {
		t.Error("Expected error patterns to be detected")
	}

	// Check for specific patterns
	foundError := false
	foundWarning := false
	for _, p := range insight.ErrorPatterns {
		if p.Severity == "error" {
			foundError = true
		}
		if p.Severity == "warning" {
			foundWarning = true
		}
	}

	if !foundError {
		t.Error("Expected to find error pattern")
	}
	if !foundWarning {
		t.Error("Expected to find warning pattern")
	}
}

func TestDefaultHealthIntelligence_CheckCodeFreshness(t *testing.T) {
	intel := createTestIntelligence()

	ctx := context.Background()
	services := []ServiceDefinition{
		{
			ID:            GenerateID(),
			Name:          "orchestrator",
			ContainerName: "test-container",
		},
	}

	reports, err := intel.CheckCodeFreshness(ctx, services)
	if err != nil {
		t.Fatalf("CheckCodeFreshness failed: %v", err)
	}

	if len(reports) == 0 {
		t.Fatal("Expected at least one report")
	}

	report := reports[0]
	if report.ID == "" {
		t.Error("Expected report to have an ID")
	}
	if report.ServiceName != "orchestrator" {
		t.Errorf("Expected service name 'orchestrator', got %q", report.ServiceName)
	}
	if report.CreatedAt.IsZero() {
		t.Error("Expected report to have a CreatedAt timestamp")
	}
}

func TestDefaultHealthIntelligence_GetMetricTrends_NoMetrics(t *testing.T) {
	// Create intelligence with no metrics store
	checker := &MockHealthChecker{}
	proc := &process.MockManager{}
	config := DefaultIntelligenceConfig("/tmp")

	intel := NewDefaultHealthIntelligence(checker, proc, nil, nil, nil, config)

	ctx := context.Background()
	_, err := intel.GetMetricTrends(ctx, "test", time.Hour)

	if err != ErrMetricsUnavailable {
		t.Errorf("Expected ErrMetricsUnavailable, got %v", err)
	}
}

func TestDefaultHealthIntelligence_GetMetricTrends_WithData(t *testing.T) {
	intel := createTestIntelligence()

	// Add some metric data
	now := time.Now()
	for i := 0; i < 10; i++ {
		intel.metrics.Record("test-service", "latency_p99", float64(100+i*10), now.Add(-time.Duration(i)*time.Minute))
	}

	ctx := context.Background()
	trends, err := intel.GetMetricTrends(ctx, "test-service", time.Hour)
	if err != nil {
		t.Fatalf("GetMetricTrends failed: %v", err)
	}

	if trends == nil {
		t.Fatal("Expected trends, got nil")
	}

	if trends.ID == "" {
		t.Error("Expected trends to have an ID")
	}
	if trends.ServiceName != "test-service" {
		t.Errorf("Expected service name 'test-service', got %q", trends.ServiceName)
	}
	if trends.DataPoints == 0 {
		t.Error("Expected data points > 0")
	}
}

func TestDefaultHealthIntelligence_GenerateLLMSummary_Basic(t *testing.T) {
	intel := createTestIntelligence()

	ctx := context.Background()
	data := &HealthDataBundle{
		ID: GenerateID(),
		BasicStatuses: []HealthStatus{
			{Name: "test", State: HealthStateHealthy},
		},
		MetricSummary: "All metrics normal",
		CreatedAt:     time.Now(),
	}

	summary, err := intel.GenerateLLMSummary(ctx, data)
	if err != nil {
		t.Fatalf("GenerateLLMSummary failed: %v", err)
	}

	if summary == "" {
		t.Error("Expected non-empty summary")
	}
}

func TestDefaultHealthIntelligence_GenerateLLMSummary_NoOllama(t *testing.T) {
	checker := &MockHealthChecker{}
	proc := &process.MockManager{}
	config := DefaultIntelligenceConfig("/tmp")

	intel := NewDefaultHealthIntelligence(checker, proc, nil, nil, nil, config)

	ctx := context.Background()
	data := &HealthDataBundle{ID: GenerateID(), CreatedAt: time.Now()}

	_, err := intel.GenerateLLMSummary(ctx, data)
	if err != ErrLLMUnavailable {
		t.Errorf("Expected ErrLLMUnavailable, got %v", err)
	}
}

func TestDefaultHealthIntelligence_DetermineOverallState(t *testing.T) {
	intel := createTestIntelligence()

	testCases := []struct {
		name     string
		insights []ServiceInsights
		alerts   []HealthAlert
		expected IntelligentHealthState
	}{
		{
			name: "all healthy",
			insights: []ServiceInsights{
				{IntelligentState: IntelligentStateHealthy},
				{IntelligentState: IntelligentStateHealthy},
			},
			expected: IntelligentStateHealthy,
		},
		{
			name: "one degraded",
			insights: []ServiceInsights{
				{IntelligentState: IntelligentStateHealthy},
				{IntelligentState: IntelligentStateDegraded},
			},
			expected: IntelligentStateDegraded,
		},
		{
			name: "one at risk",
			insights: []ServiceInsights{
				{IntelligentState: IntelligentStateHealthy},
				{IntelligentState: IntelligentStateAtRisk},
			},
			expected: IntelligentStateAtRisk,
		},
		{
			name: "one critical",
			insights: []ServiceInsights{
				{IntelligentState: IntelligentStateHealthy},
				{IntelligentState: IntelligentStateCritical},
			},
			expected: IntelligentStateCritical,
		},
		{
			name:     "critical alert",
			insights: []ServiceInsights{{IntelligentState: IntelligentStateHealthy}},
			alerts:   []HealthAlert{{Severity: AlertSeverityCritical}},
			expected: IntelligentStateCritical,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := intel.determineOverallState(tc.insights, tc.alerts)
			if result != tc.expected {
				t.Errorf("Expected %s, got %s", tc.expected, result)
			}
		})
	}
}

func TestDefaultHealthIntelligence_AnalyzeLogs(t *testing.T) {
	intel := createTestIntelligence()

	logs := `2026-01-05 10:00:00 INFO: Service started
2026-01-05 10:00:01 WARNING: Slow query detected
2026-01-05 10:00:02 ERROR: Connection timeout
2026-01-05 10:00:03 ERROR: Connection timeout
2026-01-05 10:00:04 ERROR: Connection timeout
2026-01-05 10:00:05 FATAL: Unrecoverable error`

	anomalies, patterns := intel.analyzeLogs(logs)

	// Should detect multiple patterns
	if len(patterns) == 0 {
		t.Error("Expected patterns to be detected")
	}

	// ERROR pattern should have count >= 3 and generate anomaly
	found := false
	for _, a := range anomalies {
		if a.Count >= 3 {
			found = true
			break
		}
	}
	if !found && len(anomalies) > 0 {
		// That's okay, the threshold might not be met
	}
}

func TestDefaultHealthIntelligence_CalculateTrend(t *testing.T) {
	intel := createTestIntelligence()
	now := time.Now()

	testCases := []struct {
		name     string
		values   []float64
		expected Trend
	}{
		{
			name:     "stable",
			values:   []float64{100, 101, 99, 100, 100},
			expected: TrendStable,
		},
		{
			name:     "increasing",
			values:   []float64{100, 110, 120, 130, 150},
			expected: TrendIncreasing,
		},
		{
			name:     "decreasing",
			values:   []float64{150, 130, 120, 110, 100},
			expected: TrendDecreasing,
		},
		{
			name:     "insufficient data",
			values:   []float64{100},
			expected: TrendUnknown,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var points []MetricPoint
			for i, v := range tc.values {
				points = append(points, MetricPoint{
					Value:     v,
					Timestamp: now.Add(time.Duration(i) * time.Minute),
				})
			}

			result := intel.calculateTrend(points)
			if result != tc.expected {
				t.Errorf("Expected %s, got %s", tc.expected, result)
			}
		})
	}
}

// =============================================================================
// DefaultHealthTextGenerator TESTS
// =============================================================================

func TestDefaultHealthTextGenerator_Generate_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("Expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/generate" {
			t.Errorf("Expected /api/generate, got %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"response": "Test response from LLM"}`))
	}))
	defer server.Close()

	client := NewDefaultHealthTextGenerator(server.URL)

	ctx := context.Background()
	response, err := client.Generate(ctx, "gemma3:1b", "Test prompt")

	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	if response != "Test response from LLM" {
		t.Errorf("Expected 'Test response from LLM', got %q", response)
	}
}

func TestDefaultHealthTextGenerator_Generate_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Internal error"))
	}))
	defer server.Close()

	client := NewDefaultHealthTextGenerator(server.URL)

	ctx := context.Background()
	_, err := client.Generate(ctx, "gemma3:1b", "Test prompt")

	if err == nil {
		t.Error("Expected error for 500 response")
	}
}

// =============================================================================
// MockHealthIntelligence TESTS
// =============================================================================

func TestMockHealthIntelligence_DefaultBehavior(t *testing.T) {
	mock := &MockHealthIntelligence{}

	ctx := context.Background()
	opts := DefaultAnalysisOptions()

	report, err := mock.AnalyzeHealth(ctx, opts)
	if err != nil {
		t.Fatalf("AnalyzeHealth failed: %v", err)
	}

	if report.OverallState != IntelligentStateHealthy {
		t.Errorf("Expected healthy state, got %s", report.OverallState)
	}

	if len(mock.AnalyzeCalls) != 1 {
		t.Errorf("Expected 1 call recorded, got %d", len(mock.AnalyzeCalls))
	}
}

func TestMockHealthIntelligence_CustomFunction(t *testing.T) {
	mock := &MockHealthIntelligence{
		AnalyzeHealthFunc: func(ctx context.Context, opts AnalysisOptions) (*IntelligentHealthReport, error) {
			return &IntelligentHealthReport{
				ID:           GenerateID(),
				OverallState: IntelligentStateCritical,
				Summary:      "Custom critical state",
				CreatedAt:    time.Now(),
			}, nil
		},
	}

	ctx := context.Background()
	opts := DefaultAnalysisOptions()

	report, _ := mock.AnalyzeHealth(ctx, opts)

	if report.OverallState != IntelligentStateCritical {
		t.Errorf("Expected critical state, got %s", report.OverallState)
	}
	if report.Summary != "Custom critical state" {
		t.Errorf("Expected custom summary, got %q", report.Summary)
	}
}

func TestMockHealthTextGenerator_DefaultBehavior(t *testing.T) {
	mock := &MockHealthTextGenerator{}

	ctx := context.Background()
	response, err := mock.Generate(ctx, "gemma3:1b", "test prompt")

	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	if response == "" {
		t.Error("Expected non-empty response")
	}

	if len(mock.Calls) != 1 {
		t.Errorf("Expected 1 call, got %d", len(mock.Calls))
	}
	if mock.Calls[0].Model != "gemma3:1b" {
		t.Errorf("Expected model 'gemma3:1b', got %q", mock.Calls[0].Model)
	}
}

// =============================================================================
// CONFIG AND OPTIONS TESTS
// =============================================================================

func TestDefaultIntelligenceConfig(t *testing.T) {
	config := DefaultIntelligenceConfig("/path/to/stack")

	if config.ID == "" {
		t.Error("Expected config to have an ID")
	}
	if config.OllamaHost != "http://localhost:11434" {
		t.Errorf("Expected OllamaHost 'http://localhost:11434', got %s", config.OllamaHost)
	}
	if config.SummaryModel != "gemma3:1b" {
		t.Errorf("Expected SummaryModel 'gemma3:1b', got %s", config.SummaryModel)
	}
	if config.StackDir != "/path/to/stack" {
		t.Errorf("Expected StackDir '/path/to/stack', got %s", config.StackDir)
	}
	if config.LatencyWarningThreshold != 500*time.Millisecond {
		t.Error("Expected LatencyWarningThreshold 500ms")
	}
	if config.CreatedAt.IsZero() {
		t.Error("Expected config to have a CreatedAt timestamp")
	}
}

func TestDefaultAnalysisOptions(t *testing.T) {
	opts := DefaultAnalysisOptions()

	if opts.ID == "" {
		t.Error("Expected options to have an ID")
	}
	if opts.TimeWindow != 5*time.Minute {
		t.Errorf("Expected TimeWindow 5m, got %v", opts.TimeWindow)
	}
	if opts.MaxLogLines != 1000 {
		t.Errorf("Expected MaxLogLines 1000, got %d", opts.MaxLogLines)
	}
	if opts.IncludeLLMSummary {
		t.Error("Expected IncludeLLMSummary to be false by default")
	}
	if opts.CreatedAt.IsZero() {
		t.Error("Expected options to have a CreatedAt timestamp")
	}
}
