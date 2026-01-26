// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package models

import (
	"bytes"
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

// =============================================================================
// DefaultProgressRenderer Tests
// =============================================================================

// TestNewDefaultProgressRenderer_CreatesValidRenderer verifies constructor.
func TestNewDefaultProgressRenderer_CreatesValidRenderer(t *testing.T) {
	var buf bytes.Buffer
	renderer := NewDefaultProgressRenderer(&buf)

	if renderer == nil {
		t.Fatal("NewDefaultProgressRenderer returned nil")
	}

	if renderer.output != &buf {
		t.Error("output not set correctly")
	}

	if renderer.operations == nil {
		t.Error("operations map not initialized")
	}

	if renderer.minUpdateInterval != 100*time.Millisecond {
		t.Errorf("minUpdateInterval = %v, want 100ms", renderer.minUpdateInterval)
	}

	if renderer.rateWindowSec != 5 {
		t.Errorf("rateWindowSec = %d, want 5", renderer.rateWindowSec)
	}
}

// TestDefaultProgressRenderer_Render_WritesToOutput verifies output.
func TestDefaultProgressRenderer_Render_WritesToOutput(t *testing.T) {
	var buf bytes.Buffer
	renderer := NewDefaultProgressRenderer(&buf)
	ctx := context.Background()

	renderer.Render(ctx, "test-op", "downloading", 1024, 4096)

	output := buf.String()
	if output == "" {
		t.Error("Render produced no output")
	}

	if !strings.Contains(output, "test-op") {
		t.Error("output missing operation name")
	}
}

// TestDefaultProgressRenderer_Render_RateLimits verifies rate limiting.
func TestDefaultProgressRenderer_Render_RateLimits(t *testing.T) {
	var buf bytes.Buffer
	renderer := NewDefaultProgressRenderer(&buf)
	ctx := context.Background()

	// First render should succeed
	renderer.Render(ctx, "test-op", "status1", 0, 100)
	firstLen := buf.Len()

	// Immediate second render should be rate-limited
	renderer.Render(ctx, "test-op", "status2", 50, 100)
	secondLen := buf.Len()

	if secondLen != firstLen {
		t.Error("rate limiting failed - second render was not skipped")
	}

	// Wait for rate limit to expire
	time.Sleep(150 * time.Millisecond)

	// Third render should succeed
	renderer.Render(ctx, "test-op", "status3", 100, 100)
	thirdLen := buf.Len()

	if thirdLen <= secondLen {
		t.Error("third render after rate limit should have produced output")
	}
}

// TestDefaultProgressRenderer_Render_CancelledContext verifies context handling.
func TestDefaultProgressRenderer_Render_CancelledContext(t *testing.T) {
	var buf bytes.Buffer
	renderer := NewDefaultProgressRenderer(&buf)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	renderer.Render(ctx, "test-op", "downloading", 1024, 4096)

	if buf.Len() > 0 {
		t.Error("Render should not produce output when context is cancelled")
	}
}

// TestDefaultProgressRenderer_Render_NilOutput verifies nil output handling.
func TestDefaultProgressRenderer_Render_NilOutput(t *testing.T) {
	renderer := NewDefaultProgressRenderer(nil)
	ctx := context.Background()

	// Should not panic
	renderer.Render(ctx, "test-op", "downloading", 1024, 4096)
}

// TestDefaultProgressRenderer_Complete_OutputsSuccess verifies success completion.
func TestDefaultProgressRenderer_Complete_OutputsSuccess(t *testing.T) {
	var buf bytes.Buffer
	renderer := NewDefaultProgressRenderer(&buf)
	ctx := context.Background()

	// Start an operation
	renderer.Render(ctx, "test-op", "downloading", 0, 100)
	buf.Reset()

	// Complete it
	renderer.Complete(ctx, "test-op", true, "finished")

	output := buf.String()
	if !strings.Contains(output, "✓") {
		t.Error("success completion should contain checkmark")
	}
	if !strings.Contains(output, "finished") {
		t.Error("completion should contain message")
	}
}

// TestDefaultProgressRenderer_Complete_OutputsFailure verifies failure completion.
func TestDefaultProgressRenderer_Complete_OutputsFailure(t *testing.T) {
	var buf bytes.Buffer
	renderer := NewDefaultProgressRenderer(&buf)
	ctx := context.Background()

	renderer.Render(ctx, "test-op", "downloading", 0, 100)
	buf.Reset()

	renderer.Complete(ctx, "test-op", false, "failed")

	output := buf.String()
	if !strings.Contains(output, "✗") {
		t.Error("failure completion should contain X mark")
	}
	if !strings.Contains(output, "failed") {
		t.Error("completion should contain message")
	}
}

// TestDefaultProgressRenderer_Complete_RemovesOperation verifies cleanup.
func TestDefaultProgressRenderer_Complete_RemovesOperation(t *testing.T) {
	var buf bytes.Buffer
	renderer := NewDefaultProgressRenderer(&buf)
	ctx := context.Background()

	renderer.Render(ctx, "test-op", "downloading", 50, 100)
	renderer.Complete(ctx, "test-op", true, "done")

	renderer.mu.Lock()
	_, exists := renderer.operations["test-op"]
	renderer.mu.Unlock()

	if exists {
		t.Error("Complete should remove operation from tracking")
	}
}

// TestDefaultProgressRenderer_SetOutput_ChangesOutput verifies output switching.
func TestDefaultProgressRenderer_SetOutput_ChangesOutput(t *testing.T) {
	var buf1, buf2 bytes.Buffer
	renderer := NewDefaultProgressRenderer(&buf1)
	ctx := context.Background()

	renderer.Render(ctx, "op1", "status", 0, 100)

	renderer.SetOutput(&buf2)
	time.Sleep(150 * time.Millisecond) // Wait for rate limit
	renderer.Render(ctx, "op2", "status", 0, 100)

	if buf2.Len() == 0 {
		t.Error("SetOutput should change output destination")
	}
}

// TestDefaultProgressRenderer_IsTTY_ReturnsTrue verifies TTY flag.
func TestDefaultProgressRenderer_IsTTY_ReturnsTrue(t *testing.T) {
	var buf bytes.Buffer
	renderer := NewDefaultProgressRenderer(&buf)

	if !renderer.IsTTY() {
		t.Error("DefaultProgressRenderer.IsTTY() should return true")
	}
}

// TestDefaultProgressRenderer_ConcurrentAccess verifies thread safety.
func TestDefaultProgressRenderer_ConcurrentAccess(t *testing.T) {
	var buf bytes.Buffer
	renderer := NewDefaultProgressRenderer(&buf)
	ctx := context.Background()

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				renderer.Render(ctx, "op", "status", int64(j), 100)
			}
		}(i)
	}

	wg.Wait()
	// Should not panic or deadlock
}

// =============================================================================
// LineProgressRenderer Tests
// =============================================================================

// TestNewLineProgressRenderer_CreatesValidRenderer verifies constructor.
func TestNewLineProgressRenderer_CreatesValidRenderer(t *testing.T) {
	var buf bytes.Buffer
	renderer := NewLineProgressRenderer(&buf)

	if renderer == nil {
		t.Fatal("NewLineProgressRenderer returned nil")
	}

	if renderer.output != &buf {
		t.Error("output not set correctly")
	}

	if renderer.minUpdateInterval != 5*time.Second {
		t.Errorf("minUpdateInterval = %v, want 5s", renderer.minUpdateInterval)
	}
}

// TestLineProgressRenderer_Render_OutputsLogLines verifies output format.
func TestLineProgressRenderer_Render_OutputsLogLines(t *testing.T) {
	var buf bytes.Buffer
	renderer := NewLineProgressRenderer(&buf)
	ctx := context.Background()

	renderer.Render(ctx, "test-op", "downloading", 1024, 4096)

	output := buf.String()
	if output == "" {
		t.Error("Render produced no output")
	}

	// Should contain timestamp
	if !strings.Contains(output, "T") && !strings.Contains(output, "Z") {
		t.Error("output should contain RFC3339 timestamp")
	}

	// Should contain [INFO]
	if !strings.Contains(output, "[INFO]") {
		t.Error("output should contain [INFO] log level")
	}

	// Should end with newline
	if !strings.HasSuffix(output, "\n") {
		t.Error("output should end with newline")
	}
}

// TestLineProgressRenderer_Render_RateLimitsPerOperation verifies rate limiting.
func TestLineProgressRenderer_Render_RateLimitsPerOperation(t *testing.T) {
	var buf bytes.Buffer
	renderer := NewLineProgressRenderer(&buf)
	// Override for faster testing
	renderer.minUpdateInterval = 100 * time.Millisecond
	ctx := context.Background()

	// First render should succeed
	renderer.Render(ctx, "op1", "status", 0, 100)
	firstLen := buf.Len()

	// Immediate second render for same op should be skipped
	renderer.Render(ctx, "op1", "status", 50, 100)
	secondLen := buf.Len()

	if secondLen != firstLen {
		t.Error("second render for same op should be rate-limited")
	}

	// Different operation should not be rate-limited
	renderer.Render(ctx, "op2", "status", 0, 100)
	thirdLen := buf.Len()

	if thirdLen == secondLen {
		t.Error("render for different op should not be rate-limited")
	}
}

// TestLineProgressRenderer_Complete_OutputsLogLine verifies completion.
func TestLineProgressRenderer_Complete_OutputsLogLine(t *testing.T) {
	var buf bytes.Buffer
	renderer := NewLineProgressRenderer(&buf)
	ctx := context.Background()

	renderer.Render(ctx, "test-op", "downloading", 0, 100)
	buf.Reset()

	renderer.Complete(ctx, "test-op", true, "done")

	output := buf.String()
	if !strings.Contains(output, "[INFO]") {
		t.Error("success should log as INFO")
	}
	if !strings.Contains(output, "done") {
		t.Error("completion message should appear")
	}
}

// TestLineProgressRenderer_Complete_ErrorLevel verifies error logging.
func TestLineProgressRenderer_Complete_ErrorLevel(t *testing.T) {
	var buf bytes.Buffer
	renderer := NewLineProgressRenderer(&buf)
	ctx := context.Background()

	renderer.Complete(ctx, "test-op", false, "failed")

	output := buf.String()
	if !strings.Contains(output, "[ERROR]") {
		t.Error("failure should log as ERROR")
	}
}

// TestLineProgressRenderer_IsTTY_ReturnsFalse verifies TTY flag.
func TestLineProgressRenderer_IsTTY_ReturnsFalse(t *testing.T) {
	var buf bytes.Buffer
	renderer := NewLineProgressRenderer(&buf)

	if renderer.IsTTY() {
		t.Error("LineProgressRenderer.IsTTY() should return false")
	}
}

// =============================================================================
// SilentProgressRenderer Tests
// =============================================================================

// TestNewSilentProgressRenderer_CreatesValidRenderer verifies constructor.
func TestNewSilentProgressRenderer_CreatesValidRenderer(t *testing.T) {
	var buf bytes.Buffer
	renderer := NewSilentProgressRenderer(&buf)

	if renderer == nil {
		t.Fatal("NewSilentProgressRenderer returned nil")
	}
}

// TestSilentProgressRenderer_Render_NoOutput verifies silence.
func TestSilentProgressRenderer_Render_NoOutput(t *testing.T) {
	var buf bytes.Buffer
	renderer := NewSilentProgressRenderer(&buf)
	ctx := context.Background()

	renderer.Render(ctx, "test-op", "downloading", 1024, 4096)

	if buf.Len() > 0 {
		t.Error("SilentProgressRenderer.Render should produce no output")
	}
}

// TestSilentProgressRenderer_Render_TracksState verifies internal tracking.
func TestSilentProgressRenderer_Render_TracksState(t *testing.T) {
	var buf bytes.Buffer
	renderer := NewSilentProgressRenderer(&buf)
	ctx := context.Background()

	renderer.Render(ctx, "test-op", "downloading", 1024, 4096)

	renderer.mu.Lock()
	_, exists := renderer.operations["test-op"]
	renderer.mu.Unlock()

	if !exists {
		t.Error("SilentProgressRenderer should track operations internally")
	}
}

// TestSilentProgressRenderer_Complete_OutputsOnlyOnCompletion verifies completion output.
func TestSilentProgressRenderer_Complete_OutputsOnlyOnCompletion(t *testing.T) {
	var buf bytes.Buffer
	renderer := NewSilentProgressRenderer(&buf)
	ctx := context.Background()

	renderer.Render(ctx, "test-op", "downloading", 1024, 4096)
	if buf.Len() > 0 {
		t.Error("should not output during Render")
	}

	renderer.Complete(ctx, "test-op", true, "done")

	if buf.Len() == 0 {
		t.Error("should output on Complete")
	}
	if !strings.Contains(buf.String(), "✓") {
		t.Error("success should have checkmark")
	}
}

// TestSilentProgressRenderer_Complete_NilOutput verifies nil handling.
func TestSilentProgressRenderer_Complete_NilOutput(t *testing.T) {
	renderer := NewSilentProgressRenderer(nil)
	ctx := context.Background()

	// Should not panic
	renderer.Complete(ctx, "test-op", true, "done")
}

// TestSilentProgressRenderer_IsTTY_ReturnsFalse verifies TTY flag.
func TestSilentProgressRenderer_IsTTY_ReturnsFalse(t *testing.T) {
	var buf bytes.Buffer
	renderer := NewSilentProgressRenderer(&buf)

	if renderer.IsTTY() {
		t.Error("SilentProgressRenderer.IsTTY() should return false")
	}
}

// =============================================================================
// MockProgressRenderer Tests
// =============================================================================

// TestNewMockProgressRenderer_CreatesValidMock verifies constructor.
func TestNewMockProgressRenderer_CreatesValidMock(t *testing.T) {
	mock := NewMockProgressRenderer()

	if mock == nil {
		t.Fatal("NewMockProgressRenderer returned nil")
	}

	if mock.RenderCalls == nil {
		t.Error("RenderCalls not initialized")
	}

	if mock.CompleteCalls == nil {
		t.Error("CompleteCalls not initialized")
	}
}

// TestMockProgressRenderer_Render_RecordsCalls verifies call recording.
func TestMockProgressRenderer_Render_RecordsCalls(t *testing.T) {
	mock := NewMockProgressRenderer()
	ctx := context.Background()

	mock.Render(ctx, "op1", "status1", 100, 200)
	mock.Render(ctx, "op2", "status2", 300, 400)

	if mock.RenderCallCount() != 2 {
		t.Errorf("RenderCallCount = %d, want 2", mock.RenderCallCount())
	}

	if mock.RenderCalls[0].Operation != "op1" {
		t.Error("first call operation incorrect")
	}
	if mock.RenderCalls[0].Completed != 100 {
		t.Error("first call completed incorrect")
	}
	if mock.RenderCalls[1].Operation != "op2" {
		t.Error("second call operation incorrect")
	}
}

// TestMockProgressRenderer_Complete_RecordsCalls verifies call recording.
func TestMockProgressRenderer_Complete_RecordsCalls(t *testing.T) {
	mock := NewMockProgressRenderer()
	ctx := context.Background()

	mock.Complete(ctx, "op1", true, "success")
	mock.Complete(ctx, "op2", false, "failure")

	if mock.CompleteCallCount() != 2 {
		t.Errorf("CompleteCallCount = %d, want 2", mock.CompleteCallCount())
	}

	if mock.CompleteCalls[0].Success != true {
		t.Error("first call success incorrect")
	}
	if mock.CompleteCalls[1].Success != false {
		t.Error("second call success incorrect")
	}
}

// TestMockProgressRenderer_CustomFunction verifies custom behavior.
func TestMockProgressRenderer_CustomFunction(t *testing.T) {
	mock := NewMockProgressRenderer()
	ctx := context.Background()

	called := false
	mock.RenderFunc = func(ctx context.Context, op, status string, completed, total int64) {
		called = true
	}

	mock.Render(ctx, "op", "status", 0, 100)

	if !called {
		t.Error("custom RenderFunc should be called")
	}
}

// TestMockProgressRenderer_Reset verifies reset functionality.
func TestMockProgressRenderer_Reset(t *testing.T) {
	mock := NewMockProgressRenderer()
	ctx := context.Background()

	mock.Render(ctx, "op", "status", 0, 100)
	mock.Complete(ctx, "op", true, "done")

	mock.Reset()

	if mock.RenderCallCount() != 0 {
		t.Error("Reset should clear RenderCalls")
	}
	if mock.CompleteCallCount() != 0 {
		t.Error("Reset should clear CompleteCalls")
	}
}

// TestMockProgressRenderer_TTYValue verifies TTY configuration.
func TestMockProgressRenderer_TTYValue(t *testing.T) {
	mock := NewMockProgressRenderer()

	if mock.IsTTY() {
		t.Error("default TTYValue should be false")
	}

	mock.TTYValue = true
	if !mock.IsTTY() {
		t.Error("IsTTY should return configured TTYValue")
	}
}

// TestMockProgressRenderer_ConcurrentAccess verifies thread safety.
func TestMockProgressRenderer_ConcurrentAccess(t *testing.T) {
	mock := NewMockProgressRenderer()
	ctx := context.Background()

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				mock.Render(ctx, "op", "status", int64(j), 100)
			}
		}()
	}

	wg.Wait()

	if mock.RenderCallCount() != 1000 {
		t.Errorf("RenderCallCount = %d, want 1000", mock.RenderCallCount())
	}
}

// =============================================================================
// operationState Tests
// =============================================================================

// TestOperationState_CalculateRate_NoSamples verifies empty state.
func TestOperationState_CalculateRate_NoSamples(t *testing.T) {
	op := &operationState{
		RateWindowSec: 5,
		RateSamples:   make([]rateSample, 0),
	}

	rate := op.calculateRate()
	if rate != 0 {
		t.Errorf("rate with no samples = %f, want 0", rate)
	}
}

// TestOperationState_CalculateRate_OneSample verifies single sample.
func TestOperationState_CalculateRate_OneSample(t *testing.T) {
	op := &operationState{
		RateWindowSec: 5,
		RateSamples: []rateSample{
			{Time: time.Now(), Completed: 100},
		},
	}

	rate := op.calculateRate()
	if rate != 0 {
		t.Errorf("rate with one sample = %f, want 0", rate)
	}
}

// TestOperationState_CalculateRate_ValidSamples verifies calculation.
func TestOperationState_CalculateRate_ValidSamples(t *testing.T) {
	now := time.Now()
	op := &operationState{
		RateWindowSec: 5,
		RateSamples: []rateSample{
			{Time: now.Add(-2 * time.Second), Completed: 0},
			{Time: now, Completed: 2000},
		},
	}

	rate := op.calculateRate()
	expected := 1000.0 // 2000 bytes / 2 seconds

	if rate < expected*0.9 || rate > expected*1.1 {
		t.Errorf("rate = %f, want ~%f", rate, expected)
	}
}

// TestOperationState_CalculateETA_NoTotal verifies no-total case.
func TestOperationState_CalculateETA_NoTotal(t *testing.T) {
	op := &operationState{
		Total: 0,
	}

	eta := op.calculateETA()
	if eta != 0 {
		t.Errorf("ETA with no total = %v, want 0", eta)
	}
}

// TestOperationState_CalculateETA_AlreadyComplete verifies completion.
func TestOperationState_CalculateETA_AlreadyComplete(t *testing.T) {
	op := &operationState{
		Completed: 100,
		Total:     100,
	}

	eta := op.calculateETA()
	if eta != 0 {
		t.Errorf("ETA when complete = %v, want 0", eta)
	}
}

// TestOperationState_CalculateETA_ValidRate verifies ETA calculation.
func TestOperationState_CalculateETA_ValidRate(t *testing.T) {
	now := time.Now()
	op := &operationState{
		Completed:     500,
		Total:         1000,
		RateWindowSec: 5,
		RateSamples: []rateSample{
			{Time: now.Add(-1 * time.Second), Completed: 0},
			{Time: now, Completed: 500},
		},
	}

	eta := op.calculateETA()
	// 500 remaining / 500 per second = 1 second
	expected := 1 * time.Second

	if eta < expected/2 || eta > expected*2 {
		t.Errorf("ETA = %v, want ~%v", eta, expected)
	}
}

// =============================================================================
// Helper Function Tests
// =============================================================================

// TestSanitizeForTerminal_RemovesANSI verifies ANSI removal.
func TestSanitizeForTerminal_RemovesANSI(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"hello", "hello"},
		{"\x1b[31mred\x1b[0m", "red"},
		{"\x1b[1;32mbold green\x1b[0m", "bold green"},
		{"no \x1b[0m escapes", "no  escapes"},
	}

	for _, tt := range tests {
		result := sanitizeForTerminal(tt.input)
		if result != tt.expected {
			t.Errorf("sanitizeForTerminal(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

// TestSanitizeForTerminal_RemovesControlChars verifies control char removal.
func TestSanitizeForTerminal_RemovesControlChars(t *testing.T) {
	input := "hello\x00world\x07"
	result := sanitizeForTerminal(input)
	expected := "helloworld"

	if result != expected {
		t.Errorf("sanitizeForTerminal(%q) = %q, want %q", input, result, expected)
	}
}

// TestSanitizeForTerminal_PreservesNewlineTab verifies preservation.
func TestSanitizeForTerminal_PreservesNewlineTab(t *testing.T) {
	input := "hello\nworld\ttab"
	result := sanitizeForTerminal(input)

	if result != input {
		t.Errorf("sanitizeForTerminal(%q) = %q, want %q", input, result, input)
	}
}

// TestFormatBytes verifies byte formatting.
func TestFormatBytes(t *testing.T) {
	tests := []struct {
		bytes    int64
		expected string
	}{
		{0, "0 B"},
		{100, "100 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1048576, "1.0 MB"},
		{1073741824, "1.0 GB"},
		{1099511627776, "1.0 TB"},
	}

	for _, tt := range tests {
		result := formatBytes(tt.bytes)
		if result != tt.expected {
			t.Errorf("formatBytes(%d) = %q, want %q", tt.bytes, result, tt.expected)
		}
	}
}

// TestFormatRate verifies rate formatting.
func TestFormatRate(t *testing.T) {
	tests := []struct {
		rate     float64
		expected string
	}{
		{0, "-- MB/s"},
		{-100, "-- MB/s"},
		{500, "500 B/s"},
		{1024, "1.0 KB/s"},
		{1048576, "1.0 MB/s"},
		{1073741824, "1.0 GB/s"},
	}

	for _, tt := range tests {
		result := formatRate(tt.rate)
		if result != tt.expected {
			t.Errorf("formatRate(%f) = %q, want %q", tt.rate, result, tt.expected)
		}
	}
}

// TestFormatDuration verifies duration formatting.
func TestFormatDuration(t *testing.T) {
	tests := []struct {
		duration time.Duration
		expected string
	}{
		{0, "0s"},
		{-5 * time.Second, "0s"},
		{30 * time.Second, "30s"},
		{65 * time.Second, "1m 5s"},
		{3665 * time.Second, "1h 1m 5s"},
	}

	for _, tt := range tests {
		result := formatDuration(tt.duration)
		if result != tt.expected {
			t.Errorf("formatDuration(%v) = %q, want %q", tt.duration, result, tt.expected)
		}
	}
}

// TestFormatETA verifies ETA formatting.
func TestFormatETA(t *testing.T) {
	tests := []struct {
		eta      time.Duration
		expected string
	}{
		{0, "calculating..."},
		{-5 * time.Second, "calculating..."},
		{30 * time.Second, "ETA: 30s"},
		{150 * time.Second, "ETA: 2m 30s"},
	}

	for _, tt := range tests {
		result := formatETA(tt.eta)
		if result != tt.expected {
			t.Errorf("formatETA(%v) = %q, want %q", tt.eta, result, tt.expected)
		}
	}
}

// TestTruncateString verifies string truncation.
func TestTruncateString(t *testing.T) {
	tests := []struct {
		input    string
		maxLen   int
		expected string
	}{
		{"hello", 10, "hello"},
		{"hello world", 8, "hello..."},
		{"hi", 2, "hi"},
		{"hello", 3, "hel"},
		{"hello", 4, "h..."},
	}

	for _, tt := range tests {
		result := truncateString(tt.input, tt.maxLen)
		if result != tt.expected {
			t.Errorf("truncateString(%q, %d) = %q, want %q", tt.input, tt.maxLen, result, tt.expected)
		}
	}
}

// =============================================================================
// Interface Compliance Tests
// =============================================================================

// TestInterfaceCompliance verifies all implementations satisfy the interface.
func TestInterfaceCompliance(t *testing.T) {
	var _ ProgressRenderer = (*DefaultProgressRenderer)(nil)
	var _ ProgressRenderer = (*LineProgressRenderer)(nil)
	var _ ProgressRenderer = (*SilentProgressRenderer)(nil)
	var _ ProgressRenderer = (*MockProgressRenderer)(nil)
}

// =============================================================================
// Progress Display Format Tests
// =============================================================================

// TestDefaultProgressRenderer_ProgressBarFormat verifies progress bar format.
func TestDefaultProgressRenderer_ProgressBarFormat(t *testing.T) {
	var buf bytes.Buffer
	renderer := NewDefaultProgressRenderer(&buf)
	ctx := context.Background()

	renderer.Render(ctx, "model:test", "downloading", 2048, 4096)

	output := buf.String()

	// Should contain progress bar characters
	if !strings.Contains(output, "█") || !strings.Contains(output, "░") {
		t.Error("progress bar should contain fill and empty characters")
	}

	// Should contain percentage
	if !strings.Contains(output, "%") {
		t.Error("progress should contain percentage")
	}

	// Should contain emoji indicator
	if !strings.Contains(output, "⏳") {
		t.Error("progress should contain status emoji")
	}
}

// TestDefaultProgressRenderer_IndeterminateProgress verifies unknown total.
func TestDefaultProgressRenderer_IndeterminateProgress(t *testing.T) {
	var buf bytes.Buffer
	renderer := NewDefaultProgressRenderer(&buf)
	ctx := context.Background()

	renderer.Render(ctx, "test-op", "starting", 0, 0)

	output := buf.String()

	// Should show operation name and status
	if !strings.Contains(output, "test-op") {
		t.Error("should contain operation name")
	}
	if !strings.Contains(output, "starting") {
		t.Error("should contain status for indeterminate progress")
	}
}
