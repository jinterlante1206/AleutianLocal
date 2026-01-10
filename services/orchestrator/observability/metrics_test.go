// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.

package observability

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// ============================================================================
// Test Helper: Create isolated metrics for testing
// ============================================================================

// newTestMetrics creates a StreamingMetrics instance with a custom registry.
// This avoids conflicts with the global Prometheus registry and allows
// parallel testing.
func newTestMetrics(t *testing.T) *StreamingMetrics {
	t.Helper()

	reg := prometheus.NewRegistry()

	requestsTotal := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Subsystem: streamingSubsystem,
			Name:      "requests_total",
			Help:      "Total number of streaming requests by endpoint and status",
		},
		[]string{"endpoint", "status"},
	)

	tokensTotal := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Subsystem: streamingSubsystem,
			Name:      "tokens_total",
			Help:      "Total tokens processed by direction and model",
		},
		[]string{"direction", "model"},
	)

	timeToFirstTokenSeconds := prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: metricsNamespace,
			Subsystem: streamingSubsystem,
			Name:      "time_to_first_token_seconds",
			Help:      "Time from request to first token in seconds",
			Buckets:   []float64{0.1, 0.25, 0.5, 1.0, 2.5, 5.0, 10.0, 30.0},
		},
		[]string{"endpoint"},
	)

	streamDurationSeconds := prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: metricsNamespace,
			Subsystem: streamingSubsystem,
			Name:      "stream_duration_seconds",
			Help:      "Total stream duration in seconds",
			Buckets:   []float64{1, 5, 10, 30, 60, 120, 300},
		},
		[]string{"endpoint", "status"},
	)

	activeStreams := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: metricsNamespace,
			Subsystem: streamingSubsystem,
			Name:      "active_streams",
			Help:      "Number of currently active streaming connections",
		},
		[]string{"endpoint"},
	)

	errorsTotal := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Subsystem: streamingSubsystem,
			Name:      "errors_total",
			Help:      "Total streaming errors by type and endpoint",
		},
		[]string{"endpoint", "error_code"},
	)

	keepAlivesTotal := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Subsystem: streamingSubsystem,
			Name:      "keepalives_total",
			Help:      "Total keepalive pings sent",
		},
		[]string{"endpoint"},
	)

	clientDisconnectsTotal := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Subsystem: streamingSubsystem,
			Name:      "client_disconnects_total",
			Help:      "Total client disconnections during streaming",
		},
		[]string{"endpoint"},
	)

	// Register all metrics with the test registry
	reg.MustRegister(
		requestsTotal,
		tokensTotal,
		timeToFirstTokenSeconds,
		streamDurationSeconds,
		activeStreams,
		errorsTotal,
		keepAlivesTotal,
		clientDisconnectsTotal,
	)

	return &StreamingMetrics{
		RequestsTotal:           requestsTotal,
		TokensTotal:             tokensTotal,
		TimeToFirstTokenSeconds: timeToFirstTokenSeconds,
		StreamDurationSeconds:   streamDurationSeconds,
		ActiveStreams:           activeStreams,
		ErrorsTotal:             errorsTotal,
		KeepAlivesTotal:         keepAlivesTotal,
		ClientDisconnectsTotal:  clientDisconnectsTotal,
	}
}

// ============================================================================
// InitMetrics Tests
// ============================================================================

// Note: InitMetrics uses promauto which registers with the default Prometheus
// registry. This test must only run once per test binary execution since
// duplicate registration will panic. We use a sync.Once to ensure this.
var initMetricsTestOnce bool

func TestInitMetrics(t *testing.T) {
	if initMetricsTestOnce {
		t.Skip("InitMetrics can only be called once per test run (promauto restriction)")
	}
	initMetricsTestOnce = true

	// Call InitMetrics
	result := InitMetrics()

	// Verify it returns a valid StreamingMetrics
	if result == nil {
		t.Fatal("InitMetrics() returned nil")
	}

	// Verify DefaultMetrics is set
	if DefaultMetrics == nil {
		t.Fatal("DefaultMetrics should be set after InitMetrics()")
	}

	// Verify DefaultMetrics is the same as the returned value
	if DefaultMetrics != result {
		t.Error("DefaultMetrics should equal the returned value")
	}

	// Verify all fields are set
	if result.RequestsTotal == nil {
		t.Error("RequestsTotal should not be nil")
	}
	if result.TokensTotal == nil {
		t.Error("TokensTotal should not be nil")
	}
	if result.TimeToFirstTokenSeconds == nil {
		t.Error("TimeToFirstTokenSeconds should not be nil")
	}
	if result.StreamDurationSeconds == nil {
		t.Error("StreamDurationSeconds should not be nil")
	}
	if result.ActiveStreams == nil {
		t.Error("ActiveStreams should not be nil")
	}
	if result.ErrorsTotal == nil {
		t.Error("ErrorsTotal should not be nil")
	}
	if result.KeepAlivesTotal == nil {
		t.Error("KeepAlivesTotal should not be nil")
	}
	if result.ClientDisconnectsTotal == nil {
		t.Error("ClientDisconnectsTotal should not be nil")
	}

	// Verify metrics can be used
	result.RecordRequest(EndpointDirectStream, true)
	result.RecordError(EndpointRAGStream, ErrorCodeTimeout)
	result.RecordTokens(100, 50, "claude-3")
	result.StreamStarted(EndpointDirectStream)
	result.StreamEnded(EndpointDirectStream)
}

// ============================================================================
// Constants Tests
// ============================================================================

func TestConstants(t *testing.T) {
	if metricsNamespace != "aleutian" {
		t.Errorf("metricsNamespace = %q, want %q", metricsNamespace, "aleutian")
	}
	if streamingSubsystem != "streaming" {
		t.Errorf("streamingSubsystem = %q, want %q", streamingSubsystem, "streaming")
	}
}

func TestEndpointConstants(t *testing.T) {
	if EndpointDirectStream != "direct_stream" {
		t.Errorf("EndpointDirectStream = %q, want %q", EndpointDirectStream, "direct_stream")
	}
	if EndpointRAGStream != "rag_stream" {
		t.Errorf("EndpointRAGStream = %q, want %q", EndpointRAGStream, "rag_stream")
	}
}

func TestErrorCodeConstants(t *testing.T) {
	tests := []struct {
		code ErrorCode
		want string
	}{
		{ErrorCodePolicyViolation, "policy_violation"},
		{ErrorCodeValidation, "validation"},
		{ErrorCodeLLMError, "llm_error"},
		{ErrorCodeTimeout, "timeout"},
		{ErrorCodeRAGError, "rag_error"},
		{ErrorCodeInternal, "internal"},
		{ErrorCodeClientDisconnect, "client_disconnect"},
	}

	for _, tt := range tests {
		if string(tt.code) != tt.want {
			t.Errorf("ErrorCode = %q, want %q", tt.code, tt.want)
		}
	}
}

// ============================================================================
// StreamingMetrics Struct Tests
// ============================================================================

func TestStreamingMetrics_Fields(t *testing.T) {
	m := newTestMetrics(t)

	if m.RequestsTotal == nil {
		t.Error("RequestsTotal should not be nil")
	}
	if m.TokensTotal == nil {
		t.Error("TokensTotal should not be nil")
	}
	if m.TimeToFirstTokenSeconds == nil {
		t.Error("TimeToFirstTokenSeconds should not be nil")
	}
	if m.StreamDurationSeconds == nil {
		t.Error("StreamDurationSeconds should not be nil")
	}
	if m.ActiveStreams == nil {
		t.Error("ActiveStreams should not be nil")
	}
	if m.ErrorsTotal == nil {
		t.Error("ErrorsTotal should not be nil")
	}
	if m.KeepAlivesTotal == nil {
		t.Error("KeepAlivesTotal should not be nil")
	}
	if m.ClientDisconnectsTotal == nil {
		t.Error("ClientDisconnectsTotal should not be nil")
	}
}

// ============================================================================
// RecordRequest Tests
// ============================================================================

func TestStreamingMetrics_RecordRequest_Success(t *testing.T) {
	m := newTestMetrics(t)

	m.RecordRequest(EndpointDirectStream, true)

	val := testutil.ToFloat64(m.RequestsTotal.WithLabelValues("direct_stream", "success"))
	if val != 1 {
		t.Errorf("RequestsTotal[direct_stream,success] = %f, want 1", val)
	}
}

func TestStreamingMetrics_RecordRequest_Error(t *testing.T) {
	m := newTestMetrics(t)

	m.RecordRequest(EndpointRAGStream, false)

	val := testutil.ToFloat64(m.RequestsTotal.WithLabelValues("rag_stream", "error"))
	if val != 1 {
		t.Errorf("RequestsTotal[rag_stream,error] = %f, want 1", val)
	}
}

func TestStreamingMetrics_RecordRequest_Multiple(t *testing.T) {
	m := newTestMetrics(t)

	// Record multiple requests
	m.RecordRequest(EndpointDirectStream, true)
	m.RecordRequest(EndpointDirectStream, true)
	m.RecordRequest(EndpointDirectStream, false)
	m.RecordRequest(EndpointRAGStream, true)

	successVal := testutil.ToFloat64(m.RequestsTotal.WithLabelValues("direct_stream", "success"))
	if successVal != 2 {
		t.Errorf("RequestsTotal[direct_stream,success] = %f, want 2", successVal)
	}

	errorVal := testutil.ToFloat64(m.RequestsTotal.WithLabelValues("direct_stream", "error"))
	if errorVal != 1 {
		t.Errorf("RequestsTotal[direct_stream,error] = %f, want 1", errorVal)
	}

	ragVal := testutil.ToFloat64(m.RequestsTotal.WithLabelValues("rag_stream", "success"))
	if ragVal != 1 {
		t.Errorf("RequestsTotal[rag_stream,success] = %f, want 1", ragVal)
	}
}

// ============================================================================
// RecordError Tests
// ============================================================================

func TestStreamingMetrics_RecordError(t *testing.T) {
	m := newTestMetrics(t)

	tests := []struct {
		endpoint Endpoint
		code     ErrorCode
	}{
		{EndpointDirectStream, ErrorCodePolicyViolation},
		{EndpointDirectStream, ErrorCodeValidation},
		{EndpointDirectStream, ErrorCodeLLMError},
		{EndpointRAGStream, ErrorCodeTimeout},
		{EndpointRAGStream, ErrorCodeRAGError},
		{EndpointRAGStream, ErrorCodeInternal},
		{EndpointDirectStream, ErrorCodeClientDisconnect},
	}

	for _, tt := range tests {
		m.RecordError(tt.endpoint, tt.code)

		val := testutil.ToFloat64(m.ErrorsTotal.WithLabelValues(string(tt.endpoint), string(tt.code)))
		if val != 1 {
			t.Errorf("ErrorsTotal[%s,%s] = %f, want 1", tt.endpoint, tt.code, val)
		}
	}
}

func TestStreamingMetrics_RecordError_Multiple(t *testing.T) {
	m := newTestMetrics(t)

	// Record same error multiple times
	m.RecordError(EndpointDirectStream, ErrorCodeLLMError)
	m.RecordError(EndpointDirectStream, ErrorCodeLLMError)
	m.RecordError(EndpointDirectStream, ErrorCodeLLMError)

	val := testutil.ToFloat64(m.ErrorsTotal.WithLabelValues("direct_stream", "llm_error"))
	if val != 3 {
		t.Errorf("ErrorsTotal[direct_stream,llm_error] = %f, want 3", val)
	}
}

// ============================================================================
// RecordTokens Tests
// ============================================================================

func TestStreamingMetrics_RecordTokens(t *testing.T) {
	m := newTestMetrics(t)

	m.RecordTokens(100, 50, "claude-3-5-sonnet")

	inputVal := testutil.ToFloat64(m.TokensTotal.WithLabelValues("input", "claude-3-5-sonnet"))
	if inputVal != 100 {
		t.Errorf("TokensTotal[input,claude-3-5-sonnet] = %f, want 100", inputVal)
	}

	outputVal := testutil.ToFloat64(m.TokensTotal.WithLabelValues("output", "claude-3-5-sonnet"))
	if outputVal != 50 {
		t.Errorf("TokensTotal[output,claude-3-5-sonnet] = %f, want 50", outputVal)
	}
}

func TestStreamingMetrics_RecordTokens_ZeroTokens(t *testing.T) {
	m := newTestMetrics(t)

	m.RecordTokens(0, 0, "gpt-4")

	inputVal := testutil.ToFloat64(m.TokensTotal.WithLabelValues("input", "gpt-4"))
	if inputVal != 0 {
		t.Errorf("TokensTotal[input,gpt-4] = %f, want 0", inputVal)
	}

	outputVal := testutil.ToFloat64(m.TokensTotal.WithLabelValues("output", "gpt-4"))
	if outputVal != 0 {
		t.Errorf("TokensTotal[output,gpt-4] = %f, want 0", outputVal)
	}
}

func TestStreamingMetrics_RecordTokens_Multiple(t *testing.T) {
	m := newTestMetrics(t)

	m.RecordTokens(100, 50, "claude-3")
	m.RecordTokens(200, 100, "claude-3")
	m.RecordTokens(50, 25, "gpt-4")

	claude3Input := testutil.ToFloat64(m.TokensTotal.WithLabelValues("input", "claude-3"))
	if claude3Input != 300 {
		t.Errorf("TokensTotal[input,claude-3] = %f, want 300", claude3Input)
	}

	claude3Output := testutil.ToFloat64(m.TokensTotal.WithLabelValues("output", "claude-3"))
	if claude3Output != 150 {
		t.Errorf("TokensTotal[output,claude-3] = %f, want 150", claude3Output)
	}

	gpt4Input := testutil.ToFloat64(m.TokensTotal.WithLabelValues("input", "gpt-4"))
	if gpt4Input != 50 {
		t.Errorf("TokensTotal[input,gpt-4] = %f, want 50", gpt4Input)
	}
}

// ============================================================================
// StreamStarted/StreamEnded Tests
// ============================================================================

func TestStreamingMetrics_StreamStarted(t *testing.T) {
	m := newTestMetrics(t)

	m.StreamStarted(EndpointDirectStream)

	val := testutil.ToFloat64(m.ActiveStreams.WithLabelValues("direct_stream"))
	if val != 1 {
		t.Errorf("ActiveStreams[direct_stream] = %f, want 1", val)
	}
}

func TestStreamingMetrics_StreamEnded(t *testing.T) {
	m := newTestMetrics(t)

	// Start then end
	m.StreamStarted(EndpointDirectStream)
	m.StreamEnded(EndpointDirectStream)

	val := testutil.ToFloat64(m.ActiveStreams.WithLabelValues("direct_stream"))
	if val != 0 {
		t.Errorf("ActiveStreams[direct_stream] = %f, want 0", val)
	}
}

func TestStreamingMetrics_StreamStarted_Multiple(t *testing.T) {
	m := newTestMetrics(t)

	m.StreamStarted(EndpointDirectStream)
	m.StreamStarted(EndpointDirectStream)
	m.StreamStarted(EndpointRAGStream)

	directVal := testutil.ToFloat64(m.ActiveStreams.WithLabelValues("direct_stream"))
	if directVal != 2 {
		t.Errorf("ActiveStreams[direct_stream] = %f, want 2", directVal)
	}

	ragVal := testutil.ToFloat64(m.ActiveStreams.WithLabelValues("rag_stream"))
	if ragVal != 1 {
		t.Errorf("ActiveStreams[rag_stream] = %f, want 1", ragVal)
	}
}

func TestStreamingMetrics_StreamLifecycle(t *testing.T) {
	m := newTestMetrics(t)

	// Simulate realistic stream lifecycle
	m.StreamStarted(EndpointDirectStream)
	m.StreamStarted(EndpointDirectStream)
	m.StreamStarted(EndpointDirectStream)

	val := testutil.ToFloat64(m.ActiveStreams.WithLabelValues("direct_stream"))
	if val != 3 {
		t.Errorf("After 3 starts: ActiveStreams = %f, want 3", val)
	}

	m.StreamEnded(EndpointDirectStream)

	val = testutil.ToFloat64(m.ActiveStreams.WithLabelValues("direct_stream"))
	if val != 2 {
		t.Errorf("After 1 end: ActiveStreams = %f, want 2", val)
	}

	m.StreamEnded(EndpointDirectStream)
	m.StreamEnded(EndpointDirectStream)

	val = testutil.ToFloat64(m.ActiveStreams.WithLabelValues("direct_stream"))
	if val != 0 {
		t.Errorf("After all ends: ActiveStreams = %f, want 0", val)
	}
}

// ============================================================================
// RecordTimeToFirstToken Tests
// ============================================================================

func TestStreamingMetrics_RecordTimeToFirstToken(t *testing.T) {
	m := newTestMetrics(t)

	m.RecordTimeToFirstToken(EndpointDirectStream, 0.5)

	// For histograms, we verify by collecting and checking count
	// The histogram itself should have observations recorded
	// We use CollectAndCount to verify the metric exists and was updated
	count := testutil.CollectAndCount(m.TimeToFirstTokenSeconds)
	if count == 0 {
		t.Error("Expected at least one metric to be collected")
	}
}

func TestStreamingMetrics_RecordTimeToFirstToken_MultipleBuckets(t *testing.T) {
	m := newTestMetrics(t)

	// Record values in different buckets
	m.RecordTimeToFirstToken(EndpointDirectStream, 0.05) // bucket 0.1
	m.RecordTimeToFirstToken(EndpointDirectStream, 0.3)  // bucket 0.5
	m.RecordTimeToFirstToken(EndpointDirectStream, 2.0)  // bucket 2.5
	m.RecordTimeToFirstToken(EndpointDirectStream, 15.0) // bucket 30.0
	m.RecordTimeToFirstToken(EndpointRAGStream, 1.0)     // bucket 1.0

	// Just verify no panics - histogram testing is done via prometheus testutil
}

// ============================================================================
// RecordStreamDuration Tests
// ============================================================================

func TestStreamingMetrics_RecordStreamDuration_Success(t *testing.T) {
	m := newTestMetrics(t)

	m.RecordStreamDuration(EndpointDirectStream, 10.5, true)

	// Just verify no panic
}

func TestStreamingMetrics_RecordStreamDuration_Error(t *testing.T) {
	m := newTestMetrics(t)

	m.RecordStreamDuration(EndpointRAGStream, 5.0, false)

	// Just verify no panic
}

func TestStreamingMetrics_RecordStreamDuration_MultipleBuckets(t *testing.T) {
	m := newTestMetrics(t)

	// Record values in different buckets: 1, 5, 10, 30, 60, 120, 300
	m.RecordStreamDuration(EndpointDirectStream, 0.5, true)   // bucket 1
	m.RecordStreamDuration(EndpointDirectStream, 3.0, true)   // bucket 5
	m.RecordStreamDuration(EndpointDirectStream, 8.0, true)   // bucket 10
	m.RecordStreamDuration(EndpointDirectStream, 45.0, true)  // bucket 60
	m.RecordStreamDuration(EndpointDirectStream, 200.0, true) // bucket 300
	m.RecordStreamDuration(EndpointRAGStream, 100.0, false)   // bucket 120

	// Just verify no panics
}

// ============================================================================
// RecordKeepAlive Tests
// ============================================================================

func TestStreamingMetrics_RecordKeepAlive(t *testing.T) {
	m := newTestMetrics(t)

	m.RecordKeepAlive(EndpointDirectStream)

	val := testutil.ToFloat64(m.KeepAlivesTotal.WithLabelValues("direct_stream"))
	if val != 1 {
		t.Errorf("KeepAlivesTotal[direct_stream] = %f, want 1", val)
	}
}

func TestStreamingMetrics_RecordKeepAlive_Multiple(t *testing.T) {
	m := newTestMetrics(t)

	m.RecordKeepAlive(EndpointDirectStream)
	m.RecordKeepAlive(EndpointDirectStream)
	m.RecordKeepAlive(EndpointDirectStream)
	m.RecordKeepAlive(EndpointRAGStream)

	directVal := testutil.ToFloat64(m.KeepAlivesTotal.WithLabelValues("direct_stream"))
	if directVal != 3 {
		t.Errorf("KeepAlivesTotal[direct_stream] = %f, want 3", directVal)
	}

	ragVal := testutil.ToFloat64(m.KeepAlivesTotal.WithLabelValues("rag_stream"))
	if ragVal != 1 {
		t.Errorf("KeepAlivesTotal[rag_stream] = %f, want 1", ragVal)
	}
}

// ============================================================================
// RecordClientDisconnect Tests
// ============================================================================

func TestStreamingMetrics_RecordClientDisconnect(t *testing.T) {
	m := newTestMetrics(t)

	m.RecordClientDisconnect(EndpointDirectStream)

	val := testutil.ToFloat64(m.ClientDisconnectsTotal.WithLabelValues("direct_stream"))
	if val != 1 {
		t.Errorf("ClientDisconnectsTotal[direct_stream] = %f, want 1", val)
	}
}

func TestStreamingMetrics_RecordClientDisconnect_Multiple(t *testing.T) {
	m := newTestMetrics(t)

	m.RecordClientDisconnect(EndpointRAGStream)
	m.RecordClientDisconnect(EndpointRAGStream)

	val := testutil.ToFloat64(m.ClientDisconnectsTotal.WithLabelValues("rag_stream"))
	if val != 2 {
		t.Errorf("ClientDisconnectsTotal[rag_stream] = %f, want 2", val)
	}
}

// ============================================================================
// Integration / Scenario Tests
// ============================================================================

func TestStreamingMetrics_CompleteStreamScenario(t *testing.T) {
	m := newTestMetrics(t)

	// Simulate a complete successful stream
	m.StreamStarted(EndpointDirectStream)
	m.RecordTimeToFirstToken(EndpointDirectStream, 0.5)
	m.RecordKeepAlive(EndpointDirectStream)
	m.RecordKeepAlive(EndpointDirectStream)
	m.RecordTokens(150, 200, "claude-3-5-sonnet")
	m.RecordStreamDuration(EndpointDirectStream, 30.0, true)
	m.StreamEnded(EndpointDirectStream)
	m.RecordRequest(EndpointDirectStream, true)

	// Verify final state
	activeVal := testutil.ToFloat64(m.ActiveStreams.WithLabelValues("direct_stream"))
	if activeVal != 0 {
		t.Errorf("ActiveStreams should be 0 after stream ended, got %f", activeVal)
	}

	requestsVal := testutil.ToFloat64(m.RequestsTotal.WithLabelValues("direct_stream", "success"))
	if requestsVal != 1 {
		t.Errorf("RequestsTotal[success] should be 1, got %f", requestsVal)
	}

	keepAliveVal := testutil.ToFloat64(m.KeepAlivesTotal.WithLabelValues("direct_stream"))
	if keepAliveVal != 2 {
		t.Errorf("KeepAlivesTotal should be 2, got %f", keepAliveVal)
	}
}

func TestStreamingMetrics_FailedStreamScenario(t *testing.T) {
	m := newTestMetrics(t)

	// Simulate a failed stream
	m.StreamStarted(EndpointRAGStream)
	m.RecordTimeToFirstToken(EndpointRAGStream, 0.3)
	m.RecordError(EndpointRAGStream, ErrorCodeLLMError)
	m.RecordStreamDuration(EndpointRAGStream, 5.0, false)
	m.StreamEnded(EndpointRAGStream)
	m.RecordRequest(EndpointRAGStream, false)

	// Verify final state
	activeVal := testutil.ToFloat64(m.ActiveStreams.WithLabelValues("rag_stream"))
	if activeVal != 0 {
		t.Errorf("ActiveStreams should be 0 after stream ended, got %f", activeVal)
	}

	requestsVal := testutil.ToFloat64(m.RequestsTotal.WithLabelValues("rag_stream", "error"))
	if requestsVal != 1 {
		t.Errorf("RequestsTotal[error] should be 1, got %f", requestsVal)
	}

	errorsVal := testutil.ToFloat64(m.ErrorsTotal.WithLabelValues("rag_stream", "llm_error"))
	if errorsVal != 1 {
		t.Errorf("ErrorsTotal[llm_error] should be 1, got %f", errorsVal)
	}
}

func TestStreamingMetrics_ClientDisconnectScenario(t *testing.T) {
	m := newTestMetrics(t)

	// Simulate client disconnect
	m.StreamStarted(EndpointDirectStream)
	m.RecordKeepAlive(EndpointDirectStream)
	m.RecordClientDisconnect(EndpointDirectStream)
	m.RecordError(EndpointDirectStream, ErrorCodeClientDisconnect)
	m.StreamEnded(EndpointDirectStream)
	m.RecordRequest(EndpointDirectStream, false)

	disconnectVal := testutil.ToFloat64(m.ClientDisconnectsTotal.WithLabelValues("direct_stream"))
	if disconnectVal != 1 {
		t.Errorf("ClientDisconnectsTotal should be 1, got %f", disconnectVal)
	}
}

// ============================================================================
// Concurrent Safety Tests
// ============================================================================

func TestStreamingMetrics_ConcurrentSafety(t *testing.T) {
	m := newTestMetrics(t)

	done := make(chan bool, 100)

	// Run multiple goroutines performing various metric operations
	for i := 0; i < 20; i++ {
		go func() {
			m.RecordRequest(EndpointDirectStream, true)
			done <- true
		}()
	}

	for i := 0; i < 20; i++ {
		go func() {
			m.RecordError(EndpointRAGStream, ErrorCodeTimeout)
			done <- true
		}()
	}

	for i := 0; i < 20; i++ {
		go func() {
			m.RecordTokens(10, 5, "test-model")
			done <- true
		}()
	}

	for i := 0; i < 20; i++ {
		go func() {
			m.StreamStarted(EndpointDirectStream)
			m.StreamEnded(EndpointDirectStream)
			done <- true
		}()
	}

	for i := 0; i < 20; i++ {
		go func() {
			m.RecordTimeToFirstToken(EndpointRAGStream, 0.5)
			m.RecordStreamDuration(EndpointRAGStream, 10.0, true)
			m.RecordKeepAlive(EndpointDirectStream)
			m.RecordClientDisconnect(EndpointRAGStream)
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 100; i++ {
		<-done
	}

	// Verify expected values
	requestsVal := testutil.ToFloat64(m.RequestsTotal.WithLabelValues("direct_stream", "success"))
	if requestsVal != 20 {
		t.Errorf("RequestsTotal[direct_stream,success] = %f, want 20", requestsVal)
	}

	errorsVal := testutil.ToFloat64(m.ErrorsTotal.WithLabelValues("rag_stream", "timeout"))
	if errorsVal != 20 {
		t.Errorf("ErrorsTotal[rag_stream,timeout] = %f, want 20", errorsVal)
	}
}
