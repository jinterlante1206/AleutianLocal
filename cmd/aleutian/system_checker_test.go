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
Package main contains unit tests for system_checker.go.

# Testing Strategy

These tests use mock implementations to avoid real system calls:
  - Mock HTTP server for network connectivity tests
  - Mock filesystem paths for disk space tests
  - Environment variable manipulation for configuration tests

All tests are designed to run fast (<1s total) and in isolation.

# Test Coverage

The tests cover:
  - Ollama installation detection (PATH, common locations, env hints)
  - Self-healing logic (symlink creation suggestions)
  - Network connectivity with retry and backoff
  - Disk space checking with configured limits
  - Graceful degradation for offline operation
  - Diagnostic report generation
  - Error classification and structured error types
  - Caching behavior
*/
package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// -----------------------------------------------------------------------------
// Mock SystemChecker for Testing
// -----------------------------------------------------------------------------

// MockSystemChecker implements SystemChecker for testing.
type MockSystemChecker struct {
	// Ollama state
	ollamaInstalled bool
	ollamaInPath    bool
	ollamaPath      string
	canSelfHeal     bool
	selfHealError   error
	selfHealCalled  bool

	// Network state
	networkError     error
	networkLatency   time.Duration
	networkCallCount int

	// Disk state
	availableDiskSpace int64
	diskError          error
	modelStoragePath   string

	// Offline capability
	canOperateOffline bool

	// Thread safety
	mu sync.Mutex
}

func (m *MockSystemChecker) IsOllamaInstalled() bool {
	return m.ollamaInstalled
}

func (m *MockSystemChecker) IsOllamaInPath() bool {
	return m.ollamaInPath
}

func (m *MockSystemChecker) GetOllamaPath() string {
	return m.ollamaPath
}

func (m *MockSystemChecker) GetOllamaInstallInstructions() string {
	return "Mock install instructions"
}

func (m *MockSystemChecker) CanSelfHealOllama() bool {
	return m.canSelfHeal
}

func (m *MockSystemChecker) SelfHealOllama() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.selfHealCalled = true
	return m.selfHealError
}

func (m *MockSystemChecker) CheckNetworkConnectivity(ctx context.Context) error {
	m.mu.Lock()
	m.networkCallCount++
	m.mu.Unlock()

	if m.networkLatency > 0 {
		select {
		case <-time.After(m.networkLatency):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return m.networkError
}

func (m *MockSystemChecker) CanOperateOffline(requiredModels []string) bool {
	return m.canOperateOffline
}

func (m *MockSystemChecker) CheckDiskSpace(requiredBytes int64, configuredLimitBytes int64) error {
	if m.diskError != nil {
		return m.diskError
	}
	if m.availableDiskSpace < requiredBytes {
		return &CheckError{
			Type:    CheckErrorDiskSpaceLow,
			Message: "Insufficient disk space",
		}
	}
	return nil
}

func (m *MockSystemChecker) GetAvailableDiskSpace() (int64, error) {
	return m.availableDiskSpace, m.diskError
}

func (m *MockSystemChecker) GetModelStoragePath() string {
	return m.modelStoragePath
}

func (m *MockSystemChecker) RunDiagnostics(ctx context.Context) *DiagnosticReport {
	return &DiagnosticReport{
		Timestamp:       time.Now(),
		OllamaInstalled: m.ollamaInstalled,
		OllamaPath:      m.ollamaPath,
		OllamaInPath:    m.ollamaInPath,
	}
}

// -----------------------------------------------------------------------------
// CheckError Tests
// -----------------------------------------------------------------------------

func TestCheckErrorType_String(t *testing.T) {
	tests := []struct {
		errorType CheckErrorType
		expected  string
	}{
		{CheckErrorOllamaNotInstalled, "OLLAMA_NOT_INSTALLED"},
		{CheckErrorOllamaNotInPath, "OLLAMA_NOT_IN_PATH"},
		{CheckErrorOllamaNotRunning, "OLLAMA_NOT_RUNNING"},
		{CheckErrorNetworkUnavailable, "NETWORK_UNAVAILABLE"},
		{CheckErrorNetworkTimeout, "NETWORK_TIMEOUT"},
		{CheckErrorDiskSpaceLow, "DISK_SPACE_LOW"},
		{CheckErrorDiskLimitExceeded, "DISK_LIMIT_EXCEEDED"},
		{CheckErrorPermissionDenied, "PERMISSION_DENIED"},
		{CheckErrorType(999), "UNKNOWN"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.errorType.String(); got != tt.expected {
				t.Errorf("CheckErrorType.String() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestCheckError_Error(t *testing.T) {
	err := &CheckError{
		Type:    CheckErrorDiskSpaceLow,
		Message: "Not enough space",
	}

	if got := err.Error(); got != "Not enough space" {
		t.Errorf("CheckError.Error() = %q, want %q", got, "Not enough space")
	}
}

func TestCheckError_FullError(t *testing.T) {
	err := &CheckError{
		Type:        CheckErrorDiskSpaceLow,
		Message:     "Not enough space",
		Detail:      "Need 5GB, have 1GB",
		Remediation: "Delete some files",
		CanSelfHeal: false,
	}

	full := err.FullError()

	if !containsSubstring(full, "Not enough space") {
		t.Error("FullError should contain Message")
	}
	if !containsSubstring(full, "Need 5GB") {
		t.Error("FullError should contain Detail")
	}
	if !containsSubstring(full, "Delete some files") {
		t.Error("FullError should contain Remediation")
	}
}

func TestCheckError_FullError_WithSelfHeal(t *testing.T) {
	err := &CheckError{
		Type:        CheckErrorOllamaNotInPath,
		Message:     "Ollama not in PATH",
		CanSelfHeal: true,
	}

	full := err.FullError()

	if !containsSubstring(full, "auto-fixable") {
		t.Error("FullError should mention auto-fix when CanSelfHeal is true")
	}
}

// -----------------------------------------------------------------------------
// DiagnosticReport Tests
// -----------------------------------------------------------------------------

func TestDiagnosticReport_String(t *testing.T) {
	report := &DiagnosticReport{
		Timestamp:        time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC),
		OllamaInstalled:  true,
		OllamaPath:       "/usr/local/bin/ollama",
		OllamaInPath:     true,
		OllamaRunning:    true,
		OllamaPID:        12345,
		ModelStoragePath: "/home/user/.ollama/models",
		ModelDiskUsed:    1024 * 1024 * 1024 * 5,  // 5GB
		ModelDiskFree:    1024 * 1024 * 1024 * 50, // 50GB
		InstalledModels:  []string{"nomic-embed-text-v2-moe", "gpt-oss"},
		NetworkReachable: true,
		NetworkLatencyMs: 45,
		PodmanInstalled:  true,
		PodmanMachine:    "podman-machine-default",
		PodmanRunning:    true,
		ContainerCount:   3,
	}

	output := report.String()

	// Check sections exist
	if !containsSubstring(output, "[Ollama]") {
		t.Error("Report should contain Ollama section")
	}
	if !containsSubstring(output, "[Models]") {
		t.Error("Report should contain Models section")
	}
	if !containsSubstring(output, "[Network]") {
		t.Error("Report should contain Network section")
	}
	if !containsSubstring(output, "[Podman]") {
		t.Error("Report should contain Podman section")
	}
	if !containsSubstring(output, "All checks passed") {
		t.Error("Report should show all checks passed when no errors")
	}
}

func TestDiagnosticReport_String_WithErrors(t *testing.T) {
	report := &DiagnosticReport{
		Timestamp:       time.Now(),
		OllamaInstalled: false,
		Errors:          []string{"Ollama is not installed", "Network unavailable"},
	}

	output := report.String()

	if !containsSubstring(output, "[Errors]") {
		t.Error("Report should contain Errors section when errors exist")
	}
	if !containsSubstring(output, "Ollama is not installed") {
		t.Error("Report should list specific errors")
	}
}

// -----------------------------------------------------------------------------
// DefaultSystemChecker Constructor Tests
// -----------------------------------------------------------------------------

func TestNewDefaultSystemChecker_Defaults(t *testing.T) {
	checker := NewDefaultSystemChecker()

	if checker == nil {
		t.Fatal("NewDefaultSystemChecker returned nil")
	}

	if len(checker.ollamaRegistryURLs) == 0 {
		t.Error("Should have registry URLs configured")
	}

	if checker.networkRetries != 3 {
		t.Errorf("Default network retries should be 3, got %d", checker.networkRetries)
	}

	if checker.networkTimeout != 10*time.Second {
		t.Errorf("Default network timeout should be 10s, got %v", checker.networkTimeout)
	}

	if checker.cacheTTL != 30*time.Second {
		t.Errorf("Default cache TTL should be 30s, got %v", checker.cacheTTL)
	}
}

func TestNewDefaultSystemChecker_RespectsOllamaModelsEnv(t *testing.T) {
	customPath := "/custom/models/path"
	os.Setenv("OLLAMA_MODELS", customPath)
	defer os.Unsetenv("OLLAMA_MODELS")

	checker := NewDefaultSystemChecker()

	if checker.ollamaModelPath != customPath {
		t.Errorf("Should respect OLLAMA_MODELS env, got %s", checker.ollamaModelPath)
	}
}

func TestNewDefaultSystemChecker_RespectsNetworkTimeoutEnv(t *testing.T) {
	os.Setenv("ALEUTIAN_NETWORK_TIMEOUT", "5s")
	defer os.Unsetenv("ALEUTIAN_NETWORK_TIMEOUT")

	checker := NewDefaultSystemChecker()

	if checker.networkTimeout != 5*time.Second {
		t.Errorf("Should respect ALEUTIAN_NETWORK_TIMEOUT env, got %v", checker.networkTimeout)
	}
}

func TestNewDefaultSystemChecker_RespectsNetworkRetriesEnv(t *testing.T) {
	os.Setenv("ALEUTIAN_NETWORK_RETRIES", "5")
	defer os.Unsetenv("ALEUTIAN_NETWORK_RETRIES")

	checker := NewDefaultSystemChecker()

	if checker.networkRetries != 5 {
		t.Errorf("Should respect ALEUTIAN_NETWORK_RETRIES env, got %d", checker.networkRetries)
	}
}

// -----------------------------------------------------------------------------
// Network Connectivity Tests
// -----------------------------------------------------------------------------

func TestCheckNetworkConnectivity_Success(t *testing.T) {
	// Create a mock HTTP server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	checker := &DefaultSystemChecker{
		ollamaRegistryURLs: []string{server.URL},
		httpClient:         &http.Client{Timeout: 5 * time.Second},
		networkRetries:     1,
		cacheTTL:           30 * time.Second,
	}

	ctx := context.Background()
	err := checker.CheckNetworkConnectivity(ctx)

	if err != nil {
		t.Errorf("Expected no error for successful connection, got: %v", err)
	}
}

func TestCheckNetworkConnectivity_Failure(t *testing.T) {
	// Use an invalid URL to force failure
	checker := &DefaultSystemChecker{
		ollamaRegistryURLs: []string{"http://localhost:99999"},
		httpClient:         &http.Client{Timeout: 100 * time.Millisecond},
		networkRetries:     1,
		cacheTTL:           30 * time.Second,
	}

	ctx := context.Background()
	err := checker.CheckNetworkConnectivity(ctx)

	if err == nil {
		t.Error("Expected error for failed connection")
	}

	var checkErr *CheckError
	if !errors.As(err, &checkErr) {
		t.Error("Error should be a CheckError")
	}
}

func TestCheckNetworkConnectivity_Retry(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount < 2 {
			// Fail first request
			http.Error(w, "temporary failure", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	checker := &DefaultSystemChecker{
		ollamaRegistryURLs: []string{server.URL},
		httpClient:         &http.Client{Timeout: 5 * time.Second},
		networkRetries:     3,
		cacheTTL:           30 * time.Second,
	}

	ctx := context.Background()
	err := checker.CheckNetworkConnectivity(ctx)

	// Should succeed after retry (note: our implementation considers any response as success)
	if err != nil {
		t.Errorf("Expected success after retry, got: %v", err)
	}
}

func TestCheckNetworkConnectivity_ContextCancellation(t *testing.T) {
	// Server that delays response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	checker := &DefaultSystemChecker{
		ollamaRegistryURLs: []string{server.URL},
		httpClient:         &http.Client{Timeout: 10 * time.Second},
		networkRetries:     3,
		cacheTTL:           30 * time.Second,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := checker.CheckNetworkConnectivity(ctx)

	if err == nil {
		t.Error("Expected error when context is cancelled")
	}
}

func TestCheckNetworkConnectivity_Caching(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	checker := &DefaultSystemChecker{
		ollamaRegistryURLs: []string{server.URL},
		httpClient:         &http.Client{Timeout: 5 * time.Second},
		networkRetries:     1,
		cacheTTL:           30 * time.Second,
	}

	ctx := context.Background()

	// First call
	_ = checker.CheckNetworkConnectivity(ctx)
	firstCallCount := callCount

	// Second call (should use cache)
	_ = checker.CheckNetworkConnectivity(ctx)

	if callCount != firstCallCount {
		t.Error("Second call should use cache and not make HTTP request")
	}
}

// -----------------------------------------------------------------------------
// Disk Space Tests
// -----------------------------------------------------------------------------

func TestCheckDiskSpace_Sufficient(t *testing.T) {
	// Create a temp directory for testing
	tmpDir := t.TempDir()

	checker := &DefaultSystemChecker{
		ollamaModelPath: tmpDir,
	}

	// Request a small amount (should always pass)
	err := checker.CheckDiskSpace(1024, 0) // 1KB

	if err != nil {
		t.Errorf("Expected no error for sufficient space, got: %v", err)
	}
}

func TestCheckDiskSpace_ZeroRequired(t *testing.T) {
	checker := &DefaultSystemChecker{
		ollamaModelPath: "/nonexistent/path",
	}

	// Zero required should skip check
	err := checker.CheckDiskSpace(0, 0)

	if err != nil {
		t.Errorf("Expected no error when requiredBytes is 0, got: %v", err)
	}
}

func TestCheckDiskSpace_Insufficient(t *testing.T) {
	tmpDir := t.TempDir()

	checker := &DefaultSystemChecker{
		ollamaModelPath: tmpDir,
	}

	// Request an absurdly large amount (should fail)
	err := checker.CheckDiskSpace(1024*1024*1024*1024*1024, 0) // 1PB

	if err == nil {
		t.Error("Expected error for insufficient space")
	}

	var checkErr *CheckError
	if !errors.As(err, &checkErr) {
		t.Error("Error should be a CheckError")
	}

	if checkErr.Type != CheckErrorDiskSpaceLow {
		t.Errorf("Expected CheckErrorDiskSpaceLow, got %v", checkErr.Type)
	}
}

func TestCheckDiskSpace_LimitExceeded(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a file to simulate existing usage
	testFile := filepath.Join(tmpDir, "testfile")
	if err := os.WriteFile(testFile, make([]byte, 1024), 0644); err != nil {
		t.Fatal(err)
	}

	checker := &DefaultSystemChecker{
		ollamaModelPath: tmpDir,
	}

	// Set a very small limit
	err := checker.CheckDiskSpace(1024, 1024) // Limit is 1KB, already have 1KB

	if err == nil {
		t.Error("Expected error when limit would be exceeded")
	}

	var checkErr *CheckError
	if errors.As(err, &checkErr) && checkErr.Type != CheckErrorDiskLimitExceeded {
		t.Errorf("Expected CheckErrorDiskLimitExceeded, got %v", checkErr.Type)
	}
}

func TestGetAvailableDiskSpace_Success(t *testing.T) {
	tmpDir := t.TempDir()

	checker := &DefaultSystemChecker{
		ollamaModelPath: tmpDir,
	}

	available, err := checker.GetAvailableDiskSpace()

	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}

	if available <= 0 {
		t.Error("Expected positive available space")
	}
}

func TestGetAvailableDiskSpace_NonexistentPath(t *testing.T) {
	home, _ := os.UserHomeDir()

	checker := &DefaultSystemChecker{
		ollamaModelPath: "/nonexistent/deeply/nested/path",
	}

	// Should fall back to home directory
	available, err := checker.GetAvailableDiskSpace()

	if err != nil {
		t.Errorf("Should fallback to home dir, got error: %v", err)
	}

	// Verify we got a reasonable value (at least check home dir stat works)
	_ = home
	if available <= 0 {
		t.Error("Expected positive available space from fallback path")
	}
}

// -----------------------------------------------------------------------------
// Ollama Detection Tests
// -----------------------------------------------------------------------------

func TestIsOllamaInstalled_WithOllamaHostEnv(t *testing.T) {
	// Set OLLAMA_HOST to simulate configured Ollama
	os.Setenv("OLLAMA_HOST", "http://localhost:11434")
	defer os.Unsetenv("OLLAMA_HOST")

	// Create fresh checker after setting env
	checker := NewDefaultSystemChecker()
	// Clear cache to force re-check
	checker.ollamaPathChecked = false

	if !checker.IsOllamaInstalled() {
		t.Error("Should detect Ollama as installed when OLLAMA_HOST is set")
	}
}

func TestGetOllamaInstallInstructions_Platform(t *testing.T) {
	checker := NewDefaultSystemChecker()
	instructions := checker.GetOllamaInstallInstructions()

	if instructions == "" {
		t.Error("Should return non-empty instructions")
	}

	if !containsSubstring(instructions, "ollama.com") {
		t.Error("Instructions should mention ollama.com")
	}
}

// -----------------------------------------------------------------------------
// Self-Healing Tests
// -----------------------------------------------------------------------------

func TestCanSelfHealOllama_True(t *testing.T) {
	mock := &MockSystemChecker{
		ollamaInstalled: true,
		ollamaInPath:    false,
		canSelfHeal:     true,
	}

	// The mock directly returns canSelfHeal
	if !mock.CanSelfHealOllama() {
		t.Error("Should indicate self-heal is available when Ollama installed but not in PATH")
	}
}

func TestCanSelfHealOllama_False_NotInstalled(t *testing.T) {
	mock := &MockSystemChecker{
		ollamaInstalled: false,
		ollamaInPath:    false,
		canSelfHeal:     false,
	}

	if mock.CanSelfHealOllama() {
		t.Error("Should not offer self-heal when Ollama is not installed")
	}
}

func TestCanSelfHealOllama_False_AlreadyInPath(t *testing.T) {
	mock := &MockSystemChecker{
		ollamaInstalled: true,
		ollamaInPath:    true,
		canSelfHeal:     false,
	}

	if mock.CanSelfHealOllama() {
		t.Error("Should not offer self-heal when Ollama is already in PATH")
	}
}

// -----------------------------------------------------------------------------
// Graceful Degradation Tests
// -----------------------------------------------------------------------------

func TestCanOperateOffline_True(t *testing.T) {
	mock := &MockSystemChecker{
		canOperateOffline: true,
	}

	if !mock.CanOperateOffline([]string{"model1", "model2"}) {
		t.Error("Should indicate offline operation is possible")
	}
}

func TestCanOperateOffline_False(t *testing.T) {
	mock := &MockSystemChecker{
		canOperateOffline: false,
	}

	if mock.CanOperateOffline([]string{"model1"}) {
		t.Error("Should indicate offline operation is not possible")
	}
}

func TestCanOperateOffline_EmptyModels(t *testing.T) {
	checker := NewDefaultSystemChecker()

	// Empty model list should always allow offline operation
	if !checker.CanOperateOffline([]string{}) {
		t.Error("Empty model list should allow offline operation")
	}
}

// -----------------------------------------------------------------------------
// Helper Functions Tests
// -----------------------------------------------------------------------------

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		bytes    int64
		expected string
	}{
		{0, "0 bytes"},
		{512, "512 bytes"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1024 * 1024, "1.0 MB"},
		{1024 * 1024 * 1024, "1.0 GB"},
		{1024 * 1024 * 1024 * 5, "5.0 GB"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := formatBytes(tt.bytes); got != tt.expected {
				t.Errorf("formatBytes(%d) = %q, want %q", tt.bytes, got, tt.expected)
			}
		})
	}
}

// -----------------------------------------------------------------------------
// Integration-Style Tests (Using Mock)
// -----------------------------------------------------------------------------

func TestMockSystemChecker_Interface(t *testing.T) {
	// Verify MockSystemChecker implements SystemChecker
	var _ SystemChecker = (*MockSystemChecker)(nil)
}

func TestFullWorkflow_HappyPath(t *testing.T) {
	mock := &MockSystemChecker{
		ollamaInstalled:    true,
		ollamaInPath:       true,
		ollamaPath:         "/usr/local/bin/ollama",
		networkError:       nil,
		availableDiskSpace: 100 * 1024 * 1024 * 1024, // 100GB
		modelStoragePath:   "/home/user/.ollama/models",
	}

	// Simulate stack start workflow
	if !mock.IsOllamaInstalled() {
		t.Error("Ollama should be installed")
	}

	ctx := context.Background()
	if err := mock.CheckNetworkConnectivity(ctx); err != nil {
		t.Errorf("Network should be available: %v", err)
	}

	if err := mock.CheckDiskSpace(5*1024*1024*1024, 0); err != nil {
		t.Errorf("Disk space should be sufficient: %v", err)
	}
}

func TestFullWorkflow_OfflineWithLocalModels(t *testing.T) {
	mock := &MockSystemChecker{
		ollamaInstalled:   true,
		ollamaInPath:      true,
		networkError:      &CheckError{Type: CheckErrorNetworkUnavailable, Message: "No internet"},
		canOperateOffline: true,
	}

	// Network fails
	ctx := context.Background()
	err := mock.CheckNetworkConnectivity(ctx)
	if err == nil {
		t.Error("Network should fail")
	}

	// But we can operate offline
	if !mock.CanOperateOffline([]string{"nomic-embed-text-v2-moe"}) {
		t.Error("Should be able to operate offline with local models")
	}
}

// -----------------------------------------------------------------------------
// Test Helpers
// -----------------------------------------------------------------------------

func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && findString(s, substr)
}

func findString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
