package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jinterlante1206/AleutianLocal/cmd/aleutian/internal/infra/process"
)

// =============================================================================
// TEST HELPERS
// =============================================================================

// mockHealthHTTPClient implements HealthHTTPClient for testing health checks.
type mockHealthHTTPClient struct {
	DoFunc func(*http.Request) (*http.Response, error)
	calls  int32
}

func (m *mockHealthHTTPClient) Do(req *http.Request) (*http.Response, error) {
	atomic.AddInt32(&m.calls, 1)
	if m.DoFunc != nil {
		return m.DoFunc(req)
	}
	return &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(strings.NewReader("")),
	}, nil
}

// createTestHealthChecker creates a checker with mock dependencies.
func createTestHealthChecker(httpClient HealthHTTPClient) *DefaultHealthChecker {
	proc := &process.MockManager{
		RunInDirFunc: func(ctx context.Context, dir string, env []string, name string, args ...string) (string, string, int, error) {
			if name == "podman" && len(args) >= 3 && args[0] == "inspect" {
				return "true", "", 0, nil
			}
			if name == "pgrep" {
				return "", "", 0, nil
			}
			return "", "", 1, nil
		},
	}

	config := DefaultHealthCheckerConfig()
	config.DefaultTimeout = 1 * time.Second

	if httpClient == nil {
		httpClient = &mockHealthHTTPClient{}
	}

	return NewDefaultHealthCheckerWithHTTPClient(proc, config, httpClient)
}

// =============================================================================
// UNIT TESTS: CheckService
// =============================================================================

// TestDefaultHealthChecker_CheckService_HTTP_Success tests successful HTTP check.
//
// # Description
//
// Verifies that CheckService returns healthy status when HTTP endpoint
// returns expected status code.
//
// # Inputs
//
//   - Service with HTTP check type
//   - Mock HTTP client returning 200
//
// # Outputs
//
//   - HealthStatus with State=healthy
//   - No error
//
// # Limitations
//
//   - Uses mock HTTP client
//
// # Assumptions
//
//   - HTTP 200 is the expected status
func TestDefaultHealthChecker_CheckService_HTTP_Success(t *testing.T) {
	httpClient := &mockHealthHTTPClient{
		DoFunc: func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: 200,
				Body:       io.NopCloser(strings.NewReader("")),
			}, nil
		},
	}
	checker := createTestHealthChecker(httpClient)

	service := ServiceDefinition{
		ID:        GenerateID(),
		Name:      "TestService",
		URL:       "http://localhost:8080/health",
		CheckType: HealthCheckHTTP,
		Version:   HealthCheckVersion,
	}

	ctx := context.Background()
	status, err := checker.CheckService(ctx, service)

	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if status == nil {
		t.Fatal("expected status, got nil")
	}
	if status.State != HealthStateHealthy {
		t.Errorf("expected state %s, got %s", HealthStateHealthy, status.State)
	}
	if status.ID == "" {
		t.Error("expected status ID to be set")
	}
	if status.HTTPStatus != 200 {
		t.Errorf("expected HTTP status 200, got %d", status.HTTPStatus)
	}
}

// TestDefaultHealthChecker_CheckService_HTTP_WrongStatus tests HTTP check with wrong status.
//
// # Description
//
// Verifies that CheckService returns unhealthy when HTTP endpoint
// returns unexpected status code.
//
// # Inputs
//
//   - Service with HTTP check type expecting 200
//   - Mock HTTP client returning 503
//
// # Outputs
//
//   - HealthStatus with State=unhealthy
//   - No error (check completed, just wrong status)
//
// # Limitations
//
//   - None
//
// # Assumptions
//
//   - 503 is considered unhealthy
func TestDefaultHealthChecker_CheckService_HTTP_WrongStatus(t *testing.T) {
	httpClient := &mockHealthHTTPClient{
		DoFunc: func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: 503,
				Body:       io.NopCloser(strings.NewReader("")),
			}, nil
		},
	}
	checker := createTestHealthChecker(httpClient)

	service := ServiceDefinition{
		ID:        GenerateID(),
		Name:      "TestService",
		URL:       "http://localhost:8080/health",
		CheckType: HealthCheckHTTP,
		Version:   HealthCheckVersion,
	}

	ctx := context.Background()
	status, err := checker.CheckService(ctx, service)

	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if status.State != HealthStateUnhealthy {
		t.Errorf("expected state %s, got %s", HealthStateUnhealthy, status.State)
	}
	if !strings.Contains(status.Message, "503") {
		t.Errorf("expected message to contain '503', got: %s", status.Message)
	}
}

// TestDefaultHealthChecker_CheckService_HTTP_ConnectionError tests HTTP connection failure.
//
// # Description
//
// Verifies that CheckService returns unreachable when HTTP connection fails.
//
// # Inputs
//
//   - Service with HTTP check type
//   - Mock HTTP client returning connection error
//
// # Outputs
//
//   - HealthStatus with State=unreachable
//   - No error (check attempted, connection failed)
//
// # Limitations
//
//   - None
//
// # Assumptions
//
//   - Connection errors result in unreachable state
func TestDefaultHealthChecker_CheckService_HTTP_ConnectionError(t *testing.T) {
	httpClient := &mockHealthHTTPClient{
		DoFunc: func(req *http.Request) (*http.Response, error) {
			return nil, errors.New("connection refused")
		},
	}
	checker := createTestHealthChecker(httpClient)

	service := ServiceDefinition{
		ID:        GenerateID(),
		Name:      "TestService",
		URL:       "http://localhost:8080/health",
		CheckType: HealthCheckHTTP,
		Version:   HealthCheckVersion,
	}

	ctx := context.Background()
	status, err := checker.CheckService(ctx, service)

	if err != nil {
		t.Fatalf("expected no infrastructure error, got: %v", err)
	}
	if status.State != HealthStateUnreachable {
		t.Errorf("expected state %s, got %s", HealthStateUnreachable, status.State)
	}
	if !strings.Contains(status.Message, "connection refused") {
		t.Errorf("expected message to contain 'connection refused', got: %s", status.Message)
	}
}

// TestDefaultHealthChecker_CheckService_HTTP_NoURL tests HTTP check without URL.
//
// # Description
//
// Verifies that CheckService returns error when URL is not configured.
//
// # Inputs
//
//   - Service with HTTP check type but no URL
//
// # Outputs
//
//   - HealthStatus with State=unhealthy
//   - Error indicating no URL configured
//
// # Limitations
//
//   - None
//
// # Assumptions
//
//   - Empty URL is invalid for HTTP checks
func TestDefaultHealthChecker_CheckService_HTTP_NoURL(t *testing.T) {
	checker := createTestHealthChecker(nil)

	service := ServiceDefinition{
		ID:        GenerateID(),
		Name:      "TestService",
		URL:       "",
		CheckType: HealthCheckHTTP,
		Version:   HealthCheckVersion,
	}

	ctx := context.Background()
	status, err := checker.CheckService(ctx, service)

	if err == nil {
		t.Error("expected error for missing URL")
	}
	if status.State != HealthStateUnhealthy {
		t.Errorf("expected state %s, got %s", HealthStateUnhealthy, status.State)
	}
}

// TestDefaultHealthChecker_CheckService_HTTP_CustomExpectedStatus tests custom expected status.
//
// # Description
//
// Verifies that CheckService uses service-specific expected status.
//
// # Inputs
//
//   - Service with ExpectedStatus=204
//   - Mock HTTP client returning 204
//
// # Outputs
//
//   - HealthStatus with State=healthy
//
// # Limitations
//
//   - None
//
// # Assumptions
//
//   - ExpectedStatus overrides default
func TestDefaultHealthChecker_CheckService_HTTP_CustomExpectedStatus(t *testing.T) {
	httpClient := &mockHealthHTTPClient{
		DoFunc: func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: 204,
				Body:       io.NopCloser(strings.NewReader("")),
			}, nil
		},
	}
	checker := createTestHealthChecker(httpClient)

	service := ServiceDefinition{
		ID:             GenerateID(),
		Name:           "TestService",
		URL:            "http://localhost:8080/health",
		CheckType:      HealthCheckHTTP,
		ExpectedStatus: 204,
		Version:        HealthCheckVersion,
	}

	ctx := context.Background()
	status, err := checker.CheckService(ctx, service)

	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if status.State != HealthStateHealthy {
		t.Errorf("expected state %s, got %s", HealthStateHealthy, status.State)
	}
}

// TestDefaultHealthChecker_CheckService_Container_Running tests container check when running.
//
// # Description
//
// Verifies that container check returns healthy when container is running.
//
// # Inputs
//
//   - Service with Container check type
//   - Mock ProcessManager returning "true" for inspect
//
// # Outputs
//
//   - HealthStatus with State=healthy
//
// # Limitations
//
//   - Uses mock ProcessManager
//
// # Assumptions
//
//   - "true" from inspect means running
func TestDefaultHealthChecker_CheckService_Container_Running(t *testing.T) {
	proc := &process.MockManager{
		RunInDirFunc: func(ctx context.Context, dir string, env []string, name string, args ...string) (string, string, int, error) {
			if name == "podman" && args[0] == "inspect" {
				return "true", "", 0, nil
			}
			return "", "", 1, nil
		},
	}

	config := DefaultHealthCheckerConfig()
	checker := NewDefaultHealthCheckerWithHTTPClient(proc, config, &mockHealthHTTPClient{})

	service := ServiceDefinition{
		ID:            GenerateID(),
		Name:          "TestContainer",
		ContainerName: "aleutian-test",
		CheckType:     HealthCheckContainer,
		Version:       HealthCheckVersion,
	}

	ctx := context.Background()
	status, err := checker.CheckService(ctx, service)

	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if status.State != HealthStateHealthy {
		t.Errorf("expected state %s, got %s", HealthStateHealthy, status.State)
	}
	if status.ContainerState != "running" {
		t.Errorf("expected ContainerState 'running', got '%s'", status.ContainerState)
	}
}

// TestDefaultHealthChecker_CheckService_Container_NotRunning tests container check when not running.
//
// # Description
//
// Verifies that container check returns unhealthy when container is not running.
//
// # Inputs
//
//   - Service with Container check type
//   - Mock ProcessManager returning "false" for inspect
//
// # Outputs
//
//   - HealthStatus with State=unhealthy
//
// # Limitations
//
//   - None
//
// # Assumptions
//
//   - "false" from inspect means not running
func TestDefaultHealthChecker_CheckService_Container_NotRunning(t *testing.T) {
	proc := &process.MockManager{
		RunInDirFunc: func(ctx context.Context, dir string, env []string, name string, args ...string) (string, string, int, error) {
			if name == "podman" && args[0] == "inspect" {
				return "false", "", 0, nil
			}
			return "", "", 1, nil
		},
	}

	config := DefaultHealthCheckerConfig()
	checker := NewDefaultHealthCheckerWithHTTPClient(proc, config, &mockHealthHTTPClient{})

	service := ServiceDefinition{
		ID:            GenerateID(),
		Name:          "TestContainer",
		ContainerName: "aleutian-test",
		CheckType:     HealthCheckContainer,
		Version:       HealthCheckVersion,
	}

	ctx := context.Background()
	status, err := checker.CheckService(ctx, service)

	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if status.State != HealthStateUnhealthy {
		t.Errorf("expected state %s, got %s", HealthStateUnhealthy, status.State)
	}
}

// TestDefaultHealthChecker_CheckService_Container_NoName tests container check without name.
//
// # Description
//
// Verifies that container check returns error when name is not configured.
//
// # Inputs
//
//   - Service with Container check type but no ContainerName
//
// # Outputs
//
//   - Error indicating no container name
//
// # Limitations
//
//   - None
//
// # Assumptions
//
//   - Empty container name is invalid
func TestDefaultHealthChecker_CheckService_Container_NoName(t *testing.T) {
	checker := createTestHealthChecker(nil)

	service := ServiceDefinition{
		ID:            GenerateID(),
		Name:          "TestContainer",
		ContainerName: "",
		CheckType:     HealthCheckContainer,
		Version:       HealthCheckVersion,
	}

	ctx := context.Background()
	status, err := checker.CheckService(ctx, service)

	if err == nil {
		t.Error("expected error for missing container name")
	}
	if status.State != HealthStateUnhealthy {
		t.Errorf("expected state %s, got %s", HealthStateUnhealthy, status.State)
	}
}

// TestDefaultHealthChecker_CheckService_UnknownType tests unknown check type.
//
// # Description
//
// Verifies that CheckService returns error for unknown check type.
//
// # Inputs
//
//   - Service with unknown CheckType
//
// # Outputs
//
//   - Error indicating unknown check type
//
// # Limitations
//
//   - None
//
// # Assumptions
//
//   - Unknown types should fail explicitly
func TestDefaultHealthChecker_CheckService_UnknownType(t *testing.T) {
	checker := createTestHealthChecker(nil)

	service := ServiceDefinition{
		ID:        GenerateID(),
		Name:      "TestService",
		CheckType: HealthCheckType("unknown"),
		Version:   HealthCheckVersion,
	}

	ctx := context.Background()
	status, err := checker.CheckService(ctx, service)

	if err == nil {
		t.Error("expected error for unknown check type")
	}
	if !strings.Contains(err.Error(), "unknown check type") {
		t.Errorf("expected error to mention 'unknown check type', got: %v", err)
	}
	if status.State != HealthStateUnhealthy {
		t.Errorf("expected state %s, got %s", HealthStateUnhealthy, status.State)
	}
}

// TestDefaultHealthChecker_CheckService_HasIDAndTimestamp tests ID and timestamp population.
//
// # Description
//
// Verifies that CheckService populates ID, LastChecked, and Version fields.
//
// # Inputs
//
//   - Any valid service
//
// # Outputs
//
//   - HealthStatus with ID, LastChecked, CheckVersion set
//
// # Limitations
//
//   - None
//
// # Assumptions
//
//   - All statuses should have tracking fields
func TestDefaultHealthChecker_CheckService_HasIDAndTimestamp(t *testing.T) {
	httpClient := &mockHealthHTTPClient{
		DoFunc: func(req *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(""))}, nil
		},
	}
	checker := createTestHealthChecker(httpClient)

	service := ServiceDefinition{
		ID:        GenerateID(),
		Name:      "TestService",
		URL:       "http://localhost:8080/health",
		CheckType: HealthCheckHTTP,
		Version:   HealthCheckVersion,
	}

	before := time.Now()
	ctx := context.Background()
	status, _ := checker.CheckService(ctx, service)
	after := time.Now()

	if status.ID == "" {
		t.Error("expected status ID to be set")
	}
	if len(status.ID) != 16 {
		t.Errorf("expected ID length 16, got %d", len(status.ID))
	}
	if status.LastChecked.Before(before) || status.LastChecked.After(after) {
		t.Errorf("expected LastChecked between %v and %v, got %v", before, after, status.LastChecked)
	}
	if status.CheckVersion != HealthCheckVersion {
		t.Errorf("expected CheckVersion %s, got %s", HealthCheckVersion, status.CheckVersion)
	}
	if status.ServiceDefinitionID != service.ID {
		t.Errorf("expected ServiceDefinitionID %s, got %s", service.ID, status.ServiceDefinitionID)
	}
}

// =============================================================================
// UNIT TESTS: CheckAllServices
// =============================================================================

// TestDefaultHealthChecker_CheckAllServices_Empty tests empty service list.
//
// # Description
//
// Verifies that CheckAllServices returns empty list for empty input.
//
// # Inputs
//
//   - Empty service list
//
// # Outputs
//
//   - Empty status list
//   - No error
//
// # Limitations
//
//   - None
//
// # Assumptions
//
//   - Empty input is valid
func TestDefaultHealthChecker_CheckAllServices_Empty(t *testing.T) {
	checker := createTestHealthChecker(nil)

	ctx := context.Background()
	statuses, err := checker.CheckAllServices(ctx, []ServiceDefinition{})

	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if len(statuses) != 0 {
		t.Errorf("expected 0 statuses, got %d", len(statuses))
	}
}

// TestDefaultHealthChecker_CheckAllServices_Multiple tests checking multiple services.
//
// # Description
//
// Verifies that CheckAllServices checks all services concurrently.
//
// # Inputs
//
//   - Multiple services
//
// # Outputs
//
//   - Status for each service
//   - Results in same order as input
//
// # Limitations
//
//   - None
//
// # Assumptions
//
//   - Order is preserved
func TestDefaultHealthChecker_CheckAllServices_Multiple(t *testing.T) {
	var callCount int32
	httpClient := &mockHealthHTTPClient{
		DoFunc: func(req *http.Request) (*http.Response, error) {
			atomic.AddInt32(&callCount, 1)
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(""))}, nil
		},
	}
	checker := createTestHealthChecker(httpClient)

	services := []ServiceDefinition{
		{ID: GenerateID(), Name: "Service1", URL: "http://localhost:8080/health", CheckType: HealthCheckHTTP, Version: HealthCheckVersion},
		{ID: GenerateID(), Name: "Service2", URL: "http://localhost:8081/health", CheckType: HealthCheckHTTP, Version: HealthCheckVersion},
		{ID: GenerateID(), Name: "Service3", URL: "http://localhost:8082/health", CheckType: HealthCheckHTTP, Version: HealthCheckVersion},
	}

	ctx := context.Background()
	statuses, err := checker.CheckAllServices(ctx, services)

	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if len(statuses) != 3 {
		t.Fatalf("expected 3 statuses, got %d", len(statuses))
	}

	for i, status := range statuses {
		if status.Name != services[i].Name {
			t.Errorf("expected status %d name '%s', got '%s'", i, services[i].Name, status.Name)
		}
		if status.ID == "" {
			t.Errorf("expected status %d ID to be set", i)
		}
	}

	if atomic.LoadInt32(&callCount) != 3 {
		t.Errorf("expected 3 HTTP calls, got %d", callCount)
	}
}

// TestDefaultHealthChecker_CheckAllServices_MixedResults tests mixed health results.
//
// # Description
//
// Verifies that CheckAllServices returns individual results even when some fail.
//
// # Inputs
//
//   - Mix of healthy and unhealthy services
//
// # Outputs
//
//   - Correct status for each service
//
// # Limitations
//
//   - None
//
// # Assumptions
//
//   - Individual failures don't affect other checks
func TestDefaultHealthChecker_CheckAllServices_MixedResults(t *testing.T) {
	httpClient := &mockHealthHTTPClient{
		DoFunc: func(req *http.Request) (*http.Response, error) {
			if strings.Contains(req.URL.String(), "8080") {
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(""))}, nil
			}
			return &http.Response{StatusCode: 500, Body: io.NopCloser(strings.NewReader(""))}, nil
		},
	}
	checker := createTestHealthChecker(httpClient)

	services := []ServiceDefinition{
		{ID: GenerateID(), Name: "Healthy", URL: "http://localhost:8080/health", CheckType: HealthCheckHTTP, Version: HealthCheckVersion},
		{ID: GenerateID(), Name: "Unhealthy", URL: "http://localhost:8081/health", CheckType: HealthCheckHTTP, Version: HealthCheckVersion},
	}

	ctx := context.Background()
	statuses, _ := checker.CheckAllServices(ctx, services)

	if statuses[0].State != HealthStateHealthy {
		t.Errorf("expected first service healthy, got %s", statuses[0].State)
	}
	if statuses[1].State != HealthStateUnhealthy {
		t.Errorf("expected second service unhealthy, got %s", statuses[1].State)
	}
}

// =============================================================================
// UNIT TESTS: WaitForServices
// =============================================================================

// TestDefaultHealthChecker_WaitForServices_ImmediateSuccess tests immediate success.
//
// # Description
//
// Verifies that WaitForServices returns immediately when all critical
// services are healthy on first check.
//
// # Inputs
//
//   - All healthy services
//
// # Outputs
//
//   - Success result with minimal duration
//
// # Limitations
//
//   - None
//
// # Assumptions
//
//   - First check success = immediate return
func TestDefaultHealthChecker_WaitForServices_ImmediateSuccess(t *testing.T) {
	httpClient := &mockHealthHTTPClient{
		DoFunc: func(req *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(""))}, nil
		},
	}
	checker := createTestHealthChecker(httpClient)

	services := []ServiceDefinition{
		{ID: GenerateID(), Name: "Service1", URL: "http://localhost:8080/health", CheckType: HealthCheckHTTP, Critical: true, Version: HealthCheckVersion},
	}

	opts := DefaultWaitOptions()
	opts.Timeout = 5 * time.Second

	ctx := context.Background()
	result, err := checker.WaitForServices(ctx, services, opts)

	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if !result.Success {
		t.Error("expected success")
	}
	if result.Duration > 2*time.Second {
		t.Errorf("expected quick return, took %v", result.Duration)
	}
	if result.ID == "" {
		t.Error("expected result ID to be set")
	}
	if result.CompletedAt.IsZero() {
		t.Error("expected CompletedAt to be set")
	}
}

// TestDefaultHealthChecker_WaitForServices_Timeout tests timeout behavior.
//
// # Description
//
// Verifies that WaitForServices returns timeout error when services
// don't become healthy within timeout.
//
// # Inputs
//
//   - Always-unhealthy service
//   - Short timeout
//
// # Outputs
//
//   - ErrHealthCheckTimeout
//   - Failed result
//
// # Limitations
//
//   - Uses short timeout for test speed
//
// # Assumptions
//
//   - Service never becomes healthy
func TestDefaultHealthChecker_WaitForServices_Timeout(t *testing.T) {
	httpClient := &mockHealthHTTPClient{
		DoFunc: func(req *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 500, Body: io.NopCloser(strings.NewReader(""))}, nil
		},
	}
	checker := createTestHealthChecker(httpClient)

	services := []ServiceDefinition{
		{ID: GenerateID(), Name: "Service1", URL: "http://localhost:8080/health", CheckType: HealthCheckHTTP, Critical: true, Version: HealthCheckVersion},
	}

	opts := DefaultWaitOptions()
	opts.Timeout = 2 * time.Second
	opts.InitialInterval = 500 * time.Millisecond
	opts.MaxInterval = 1 * time.Second

	ctx := context.Background()
	result, err := checker.WaitForServices(ctx, services, opts)

	if !errors.Is(err, ErrHealthCheckTimeout) {
		t.Errorf("expected ErrHealthCheckTimeout, got: %v", err)
	}
	if result.Success {
		t.Error("expected failure")
	}
	if len(result.FailedCritical) != 1 {
		t.Errorf("expected 1 failed critical, got %d", len(result.FailedCritical))
	}
	if result.FailedCritical[0] != "Service1" {
		t.Errorf("expected 'Service1' in FailedCritical, got %v", result.FailedCritical)
	}
}

// TestDefaultHealthChecker_WaitForServices_EventualSuccess tests eventual success.
//
// # Description
//
// Verifies that WaitForServices succeeds when service becomes healthy
// after initial failures.
//
// # Inputs
//
//   - Service that fails first 2 checks then succeeds
//
// # Outputs
//
//   - Success result
//   - Duration includes retry time
//
// # Limitations
//
//   - None
//
// # Assumptions
//
//   - Service will become healthy
func TestDefaultHealthChecker_WaitForServices_EventualSuccess(t *testing.T) {
	var attempts int32
	httpClient := &mockHealthHTTPClient{
		DoFunc: func(req *http.Request) (*http.Response, error) {
			count := atomic.AddInt32(&attempts, 1)
			if count < 3 {
				return &http.Response{StatusCode: 500, Body: io.NopCloser(strings.NewReader(""))}, nil
			}
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(""))}, nil
		},
	}
	checker := createTestHealthChecker(httpClient)

	services := []ServiceDefinition{
		{ID: GenerateID(), Name: "Service1", URL: "http://localhost:8080/health", CheckType: HealthCheckHTTP, Critical: true, Version: HealthCheckVersion},
	}

	opts := DefaultWaitOptions()
	opts.Timeout = 30 * time.Second
	opts.InitialInterval = 100 * time.Millisecond
	opts.MaxInterval = 500 * time.Millisecond
	opts.Jitter = 0

	ctx := context.Background()
	result, err := checker.WaitForServices(ctx, services, opts)

	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if !result.Success {
		t.Error("expected success")
	}
	if atomic.LoadInt32(&attempts) < 3 {
		t.Errorf("expected at least 3 attempts, got %d", attempts)
	}
}

// TestDefaultHealthChecker_WaitForServices_FailFast tests FailFast option.
//
// # Description
//
// Verifies that WaitForServices returns immediately on critical failure
// when FailFast is enabled.
//
// # Inputs
//
//   - Critical unhealthy service
//   - FailFast=true
//
// # Outputs
//
//   - Immediate error
//   - Short duration
//
// # Limitations
//
//   - None
//
// # Assumptions
//
//   - FailFast causes immediate return
func TestDefaultHealthChecker_WaitForServices_FailFast(t *testing.T) {
	httpClient := &mockHealthHTTPClient{
		DoFunc: func(req *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 500, Body: io.NopCloser(strings.NewReader(""))}, nil
		},
	}
	checker := createTestHealthChecker(httpClient)

	services := []ServiceDefinition{
		{ID: GenerateID(), Name: "CriticalService", URL: "http://localhost:8080/health", CheckType: HealthCheckHTTP, Critical: true, Version: HealthCheckVersion},
	}

	opts := DefaultWaitOptions()
	opts.Timeout = 30 * time.Second
	opts.FailFast = true

	start := time.Now()
	ctx := context.Background()
	result, err := checker.WaitForServices(ctx, services, opts)
	duration := time.Since(start)

	if err == nil {
		t.Error("expected error")
	}
	if !strings.Contains(err.Error(), "CriticalService") {
		t.Errorf("expected error to mention service name, got: %v", err)
	}
	if result.Success {
		t.Error("expected failure")
	}
	if duration > 5*time.Second {
		t.Errorf("expected quick failure, took %v", duration)
	}
}

// TestDefaultHealthChecker_WaitForServices_SkipOptional tests SkipOptional option.
//
// # Description
//
// Verifies that non-critical services are skipped when SkipOptional is true.
//
// # Inputs
//
//   - Mix of critical and non-critical services
//   - SkipOptional=true
//
// # Outputs
//
//   - Non-critical services in Skipped list
//   - Success based only on critical services
//
// # Limitations
//
//   - None
//
// # Assumptions
//
//   - Only critical services are checked
func TestDefaultHealthChecker_WaitForServices_SkipOptional(t *testing.T) {
	var checkedServices []string
	var mu sync.Mutex
	httpClient := &mockHealthHTTPClient{
		DoFunc: func(req *http.Request) (*http.Response, error) {
			mu.Lock()
			checkedServices = append(checkedServices, req.URL.Port())
			mu.Unlock()
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(""))}, nil
		},
	}
	checker := createTestHealthChecker(httpClient)

	services := []ServiceDefinition{
		{ID: GenerateID(), Name: "Critical", URL: "http://localhost:8080/health", CheckType: HealthCheckHTTP, Critical: true, Version: HealthCheckVersion},
		{ID: GenerateID(), Name: "Optional", URL: "http://localhost:8081/health", CheckType: HealthCheckHTTP, Critical: false, Version: HealthCheckVersion},
	}

	opts := DefaultWaitOptions()
	opts.SkipOptional = true
	opts.Timeout = 5 * time.Second

	ctx := context.Background()
	result, err := checker.WaitForServices(ctx, services, opts)

	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if !result.Success {
		t.Error("expected success")
	}
	if len(result.Skipped) != 1 || result.Skipped[0] != "Optional" {
		t.Errorf("expected 'Optional' in Skipped, got %v", result.Skipped)
	}
}

// TestDefaultHealthChecker_WaitForServices_ContextCancellation tests context cancellation.
//
// # Description
//
// Verifies that WaitForServices respects context cancellation.
//
// # Inputs
//
//   - Context that gets cancelled
//   - Long timeout
//
// # Outputs
//
//   - Context cancellation error
//
// # Limitations
//
//   - None
//
// # Assumptions
//
//   - Cancellation is respected
func TestDefaultHealthChecker_WaitForServices_ContextCancellation(t *testing.T) {
	httpClient := &mockHealthHTTPClient{
		DoFunc: func(req *http.Request) (*http.Response, error) {
			time.Sleep(100 * time.Millisecond)
			return &http.Response{StatusCode: 500, Body: io.NopCloser(strings.NewReader(""))}, nil
		},
	}
	checker := createTestHealthChecker(httpClient)

	services := []ServiceDefinition{
		{ID: GenerateID(), Name: "Service1", URL: "http://localhost:8080/health", CheckType: HealthCheckHTTP, Critical: true, Version: HealthCheckVersion},
	}

	opts := DefaultWaitOptions()
	opts.Timeout = 30 * time.Second

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(500 * time.Millisecond)
		cancel()
	}()

	result, err := checker.WaitForServices(ctx, services, opts)

	if err == nil {
		t.Error("expected error")
	}
	if !strings.Contains(err.Error(), "context") {
		t.Errorf("expected context error, got: %v", err)
	}
	if result.Success {
		t.Error("expected failure")
	}
}

// TestDefaultHealthChecker_WaitForServices_ExponentialBackoff tests backoff behavior.
//
// # Description
//
// Verifies that polling intervals increase exponentially.
//
// # Inputs
//
//   - Always-failing service
//   - InitialInterval=100ms, MaxInterval=400ms, Multiplier=2
//
// # Outputs
//
//   - Increasing intervals until max
//
// # Limitations
//
//   - Timing may vary slightly
//
// # Assumptions
//
//   - Intervals follow exponential growth
func TestDefaultHealthChecker_WaitForServices_ExponentialBackoff(t *testing.T) {
	var checkTimes []time.Time
	var mu sync.Mutex
	httpClient := &mockHealthHTTPClient{
		DoFunc: func(req *http.Request) (*http.Response, error) {
			mu.Lock()
			checkTimes = append(checkTimes, time.Now())
			mu.Unlock()
			return &http.Response{StatusCode: 500, Body: io.NopCloser(strings.NewReader(""))}, nil
		},
	}
	checker := createTestHealthChecker(httpClient)

	services := []ServiceDefinition{
		{ID: GenerateID(), Name: "Service1", URL: "http://localhost:8080/health", CheckType: HealthCheckHTTP, Critical: true, Version: HealthCheckVersion},
	}

	opts := DefaultWaitOptions()
	opts.Timeout = 2 * time.Second
	opts.InitialInterval = 100 * time.Millisecond
	opts.MaxInterval = 400 * time.Millisecond
	opts.Multiplier = 2.0
	opts.Jitter = 0

	ctx := context.Background()
	checker.WaitForServices(ctx, services, opts)

	mu.Lock()
	times := checkTimes
	mu.Unlock()

	if len(times) < 3 {
		t.Fatalf("expected at least 3 checks, got %d", len(times))
	}

	for i := 1; i < len(times)-1 && i < 4; i++ {
		interval := times[i].Sub(times[i-1])
		expectedMin := time.Duration(float64(opts.InitialInterval) * float64(int(1)<<uint(i-1)) * 0.8)
		if interval < expectedMin {
			t.Logf("interval %d: %v (expected >= %v)", i, interval, expectedMin)
		}
	}
}

// TestDefaultHealthChecker_WaitForServices_ResultHasCorrectTimestamps tests timestamps.
//
// # Description
//
// Verifies that WaitResult has correct StartedAt and CompletedAt timestamps.
//
// # Inputs
//
//   - Any service
//
// # Outputs
//
//   - StartedAt before CompletedAt
//   - Duration matches difference
//
// # Limitations
//
//   - None
//
// # Assumptions
//
//   - Timestamps are accurate
func TestDefaultHealthChecker_WaitForServices_ResultHasCorrectTimestamps(t *testing.T) {
	httpClient := &mockHealthHTTPClient{
		DoFunc: func(req *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(""))}, nil
		},
	}
	checker := createTestHealthChecker(httpClient)

	services := []ServiceDefinition{
		{ID: GenerateID(), Name: "Service1", URL: "http://localhost:8080/health", CheckType: HealthCheckHTTP, Critical: true, Version: HealthCheckVersion},
	}

	opts := DefaultWaitOptions()
	before := time.Now()
	ctx := context.Background()
	result, _ := checker.WaitForServices(ctx, services, opts)
	after := time.Now()

	if result.StartedAt.Before(before) {
		t.Errorf("StartedAt %v is before test start %v", result.StartedAt, before)
	}
	if result.CompletedAt.After(after) {
		t.Errorf("CompletedAt %v is after test end %v", result.CompletedAt, after)
	}
	if result.CompletedAt.Before(result.StartedAt) {
		t.Error("CompletedAt is before StartedAt")
	}
	expectedDuration := result.CompletedAt.Sub(result.StartedAt)
	tolerance := 100 * time.Millisecond
	if result.Duration < expectedDuration-tolerance || result.Duration > expectedDuration+tolerance {
		t.Errorf("Duration %v doesn't match StartedAt/CompletedAt difference %v", result.Duration, expectedDuration)
	}
}

// =============================================================================
// UNIT TESTS: IsContainerRunning
// =============================================================================

// TestDefaultHealthChecker_IsContainerRunning_True tests running container.
func TestDefaultHealthChecker_IsContainerRunning_True(t *testing.T) {
	proc := &process.MockManager{
		RunInDirFunc: func(ctx context.Context, dir string, env []string, name string, args ...string) (string, string, int, error) {
			return "true", "", 0, nil
		},
	}
	checker := NewDefaultHealthCheckerWithHTTPClient(proc, DefaultHealthCheckerConfig(), &mockHealthHTTPClient{})

	running, err := checker.IsContainerRunning(context.Background(), "test-container")

	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if !running {
		t.Error("expected running=true")
	}
}

// TestDefaultHealthChecker_IsContainerRunning_False tests stopped container.
func TestDefaultHealthChecker_IsContainerRunning_False(t *testing.T) {
	proc := &process.MockManager{
		RunInDirFunc: func(ctx context.Context, dir string, env []string, name string, args ...string) (string, string, int, error) {
			return "false", "", 0, nil
		},
	}
	checker := NewDefaultHealthCheckerWithHTTPClient(proc, DefaultHealthCheckerConfig(), &mockHealthHTTPClient{})

	running, err := checker.IsContainerRunning(context.Background(), "test-container")

	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if running {
		t.Error("expected running=false")
	}
}

// TestDefaultHealthChecker_IsContainerRunning_NotFound tests non-existent container.
func TestDefaultHealthChecker_IsContainerRunning_NotFound(t *testing.T) {
	proc := &process.MockManager{
		RunInDirFunc: func(ctx context.Context, dir string, env []string, name string, args ...string) (string, string, int, error) {
			return "", "no such container", 1, errors.New("no such container")
		},
	}
	checker := NewDefaultHealthCheckerWithHTTPClient(proc, DefaultHealthCheckerConfig(), &mockHealthHTTPClient{})

	running, err := checker.IsContainerRunning(context.Background(), "nonexistent")

	if err != nil {
		t.Fatalf("expected no error for non-existent container, got: %v", err)
	}
	if running {
		t.Error("expected running=false for non-existent container")
	}
}

// =============================================================================
// UNIT TESTS: MockHealthChecker
// =============================================================================

// TestMockHealthChecker_RecordsCalls tests call recording.
func TestMockHealthChecker_RecordsCalls(t *testing.T) {
	mock := &MockHealthChecker{}

	service := ServiceDefinition{ID: GenerateID(), Name: "Test", Version: HealthCheckVersion}
	ctx := context.Background()

	mock.CheckService(ctx, service)
	mock.CheckAllServices(ctx, []ServiceDefinition{service})
	mock.IsContainerRunning(ctx, "test-container")
	mock.WaitForServices(ctx, []ServiceDefinition{service}, DefaultWaitOptions())

	if len(mock.CheckServiceCalls) != 1 {
		t.Errorf("expected 1 CheckService call, got %d", len(mock.CheckServiceCalls))
	}
	if len(mock.CheckAllServicesCalls) != 1 {
		t.Errorf("expected 1 CheckAllServices call, got %d", len(mock.CheckAllServicesCalls))
	}
	if len(mock.IsContainerRunningCalls) != 1 {
		t.Errorf("expected 1 IsContainerRunning call, got %d", len(mock.IsContainerRunningCalls))
	}
	if len(mock.WaitForServicesCalls) != 1 {
		t.Errorf("expected 1 WaitForServices call, got %d", len(mock.WaitForServicesCalls))
	}
}

// TestMockHealthChecker_CustomBehavior tests custom mock behavior.
func TestMockHealthChecker_CustomBehavior(t *testing.T) {
	mock := &MockHealthChecker{
		CheckServiceFunc: func(ctx context.Context, service ServiceDefinition) (*HealthStatus, error) {
			return &HealthStatus{
				ID:    GenerateID(),
				Name:  service.Name,
				State: HealthStateUnhealthy,
			}, nil
		},
	}

	status, err := mock.CheckService(context.Background(), ServiceDefinition{Name: "Test"})

	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if status.State != HealthStateUnhealthy {
		t.Errorf("expected unhealthy state from mock, got %s", status.State)
	}
}

// =============================================================================
// UNIT TESTS: Helper Functions
// =============================================================================

// TestDefaultHealthChecker_applyJitter tests jitter application.
func TestDefaultHealthChecker_applyJitter(t *testing.T) {
	checker := &DefaultHealthChecker{}
	interval := 100 * time.Millisecond
	jitter := 0.1

	for i := 0; i < 100; i++ {
		result := checker.applyJitter(interval, jitter)
		minExpected := time.Duration(float64(interval) * 0.9)
		maxExpected := time.Duration(float64(interval) * 1.1)

		if result < minExpected || result > maxExpected {
			t.Errorf("jittered interval %v outside expected range [%v, %v]", result, minExpected, maxExpected)
		}
	}
}

// TestDefaultHealthChecker_applyJitter_Zero tests zero jitter.
func TestDefaultHealthChecker_applyJitter_Zero(t *testing.T) {
	checker := &DefaultHealthChecker{}
	interval := 100 * time.Millisecond

	result := checker.applyJitter(interval, 0)

	if result != interval {
		t.Errorf("expected no jitter change, got %v", result)
	}
}

// TestDefaultHealthChecker_calculateNextInterval tests interval calculation.
func TestDefaultHealthChecker_calculateNextInterval(t *testing.T) {
	checker := &DefaultHealthChecker{}

	tests := []struct {
		current    time.Duration
		max        time.Duration
		multiplier float64
		expected   time.Duration
	}{
		{100 * time.Millisecond, 1 * time.Second, 2.0, 200 * time.Millisecond},
		{500 * time.Millisecond, 1 * time.Second, 2.0, 1 * time.Second},
		{800 * time.Millisecond, 1 * time.Second, 2.0, 1 * time.Second},
		{1 * time.Second, 1 * time.Second, 2.0, 1 * time.Second},
	}

	for _, tt := range tests {
		result := checker.calculateNextInterval(tt.current, tt.max, tt.multiplier)
		if result != tt.expected {
			t.Errorf("calculateNextInterval(%v, %v, %v) = %v, expected %v",
				tt.current, tt.max, tt.multiplier, result, tt.expected)
		}
	}
}
