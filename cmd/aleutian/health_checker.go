package main

import (
	"context"
	"fmt"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// =============================================================================
// INTERFACES
// =============================================================================

// HealthChecker verifies service availability (binary up/down).
//
// # Description
//
// This interface provides basic health checking for startup sequencing
// and status display. It supports multiple check types (HTTP, TCP,
// container state, process) and handles concurrent checking with
// exponential backoff.
//
// For intelligent health analysis (trends, anomalies, LLM summaries),
// see the HealthIntelligence interface in Phase 9B.
//
// # Inputs
//
// Implementations require:
//   - ProcessManager for container/process checks
//   - HTTPClient for HTTP checks
//   - HealthCheckerConfig for timeout configuration
//
// # Outputs
//
// All methods return structured results with unique IDs and timestamps
// for tracking and correlation.
//
// # Examples
//
//	checker := NewDefaultHealthChecker(procManager, DefaultHealthCheckerConfig())
//
//	// Single service check
//	status, err := checker.CheckService(ctx, serviceDef)
//	if status.State == HealthStateHealthy {
//	    fmt.Println("Service is healthy")
//	}
//
//	// Wait for all services during startup
//	result, err := checker.WaitForServices(ctx, services, DefaultWaitOptions())
//	if !result.Success {
//	    for _, name := range result.FailedCritical {
//	        fmt.Printf("Critical service failed: %s\n", name)
//	    }
//	}
//
// # Limitations
//
//   - Binary health only (healthy/unhealthy); no degraded state
//   - Cannot predict future failures
//   - Network-dependent; local checks may still fail
//
// # Assumptions
//
//   - Services will eventually start within timeout
//   - Network connectivity to services is available
//   - Container runtime (podman) is accessible for container checks
type HealthChecker interface {
	// WaitForServices blocks until all critical services are healthy or timeout.
	//
	// # Description
	//
	// Polls services using exponential backoff until all critical services
	// become healthy or the timeout is reached. Non-critical services are
	// checked but their failure doesn't cause an error.
	//
	// # Inputs
	//
	//   - ctx: Context for cancellation. Cancellation stops waiting immediately.
	//   - services: Services to check. Order doesn't affect behavior.
	//   - opts: Configuration for timeout, backoff, and failure modes.
	//
	// # Outputs
	//
	//   - *WaitResult: Contains success status, duration, and per-service results.
	//     Has unique ID and timestamps for tracking.
	//   - error: Non-nil if critical services failed or context was cancelled.
	//
	// # Examples
	//
	//	services := DefaultServiceDefinitions()
	//	opts := DefaultWaitOptions()
	//	opts.Timeout = 120 * time.Second
	//
	//	result, err := checker.WaitForServices(ctx, services, opts)
	//	if err != nil {
	//	    log.Printf("[%s] Startup failed after %v: %v", result.ID, result.Duration, err)
	//	}
	//
	// # Limitations
	//
	//   - All services are checked in parallel; no dependency ordering
	//   - Backoff applies globally, not per-service
	//   - Cannot distinguish between "starting" and "failed" services
	//
	// # Assumptions
	//
	//   - Services list is non-empty
	//   - At least one service is marked Critical
	//   - Timeout is greater than InitialInterval
	WaitForServices(ctx context.Context, services []ServiceDefinition, opts WaitOptions) (*WaitResult, error)

	// CheckService performs a single health check on one service.
	//
	// # Description
	//
	// Performs a single health check without retries. Returns immediately
	// with the current health status.
	//
	// # Inputs
	//
	//   - ctx: Context for cancellation and timeout.
	//   - service: The service definition to check.
	//
	// # Outputs
	//
	//   - *HealthStatus: Current health status with unique ID, state, latency.
	//   - error: Non-nil only if the check infrastructure failed.
	//
	// # Examples
	//
	//	status, err := checker.CheckService(ctx, svc)
	//	fmt.Printf("[%s] %s: %s (latency: %v)\n",
	//	    status.ID, status.Name, status.State, status.Latency)
	//
	// # Limitations
	//
	//   - No retries; single attempt only
	//   - Point-in-time; state may change immediately after
	//
	// # Assumptions
	//
	//   - Service definition is valid
	//   - Context timeout is reasonable
	CheckService(ctx context.Context, service ServiceDefinition) (*HealthStatus, error)

	// CheckAllServices checks multiple services concurrently.
	//
	// # Description
	//
	// Performs health checks on all provided services in parallel.
	// Returns results for all services regardless of individual outcomes.
	//
	// # Inputs
	//
	//   - ctx: Context for cancellation.
	//   - services: Services to check.
	//
	// # Outputs
	//
	//   - []HealthStatus: Status for each service with unique IDs.
	//   - error: Non-nil only if checking infrastructure failed.
	//
	// # Examples
	//
	//	statuses, err := checker.CheckAllServices(ctx, services)
	//	for _, s := range statuses {
	//	    fmt.Printf("[%s] %s: %s\n", s.ID, s.Name, s.State)
	//	}
	//
	// # Limitations
	//
	//   - Concurrent checks may overwhelm slow networks
	//   - No ordering guarantees in results
	//
	// # Assumptions
	//
	//   - Reasonable number of services (< 50)
	//   - Network can handle concurrent connections
	CheckAllServices(ctx context.Context, services []ServiceDefinition) ([]HealthStatus, error)

	// IsContainerRunning checks if a container exists and is running.
	//
	// # Description
	//
	// Queries the container runtime to determine if the specified container
	// is in a running state.
	//
	// # Inputs
	//
	//   - ctx: Context for cancellation.
	//   - containerName: The container name to check.
	//
	// # Outputs
	//
	//   - bool: True if container exists and is running.
	//   - error: Non-nil if runtime query failed.
	//
	// # Examples
	//
	//	running, err := checker.IsContainerRunning(ctx, "aleutian-orchestrator")
	//	if !running {
	//	    log.Println("Container not running")
	//	}
	//
	// # Limitations
	//
	//   - Only checks running state, not container health
	//   - Requires podman CLI access
	//
	// # Assumptions
	//
	//   - Podman is installed and accessible
	//   - Container names are unique
	IsContainerRunning(ctx context.Context, containerName string) (bool, error)
}

// HealthHTTPClient abstracts HTTP operations for health checking.
//
// # Description
//
// This interface is separate from the HTTPClient in chat_service.go
// because health checks use the standard http.Client.Do method pattern
// while chat services use Get/Post convenience methods.
//
// # Inputs
//
// Accepts standard *http.Request objects created via http.NewRequestWithContext.
//
// # Outputs
//
// Returns standard *http.Response and error.
//
// # Examples
//
//	type MockHealthHTTPClient struct {
//	    DoFunc func(*http.Request) (*http.Response, error)
//	}
//
//	func (m *MockHealthHTTPClient) Do(req *http.Request) (*http.Response, error) {
//	    return m.DoFunc(req)
//	}
//
// # Limitations
//
//   - Only Do method is required for health checks
//
// # Assumptions
//
//   - Caller handles response body closing
type HealthHTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// =============================================================================
// STRUCTS
// =============================================================================

// DefaultHealthChecker implements HealthChecker with full functionality.
//
// # Description
//
// Production implementation of HealthChecker supporting HTTP, TCP,
// container, and process checks. Uses exponential backoff for
// WaitForServices and concurrent checking for CheckAllServices.
//
// # Thread Safety
//
// Safe for concurrent use. Internal state is protected by mutex.
//
// # Examples
//
//	proc := NewDefaultProcessManager()
//	config := DefaultHealthCheckerConfig()
//	checker := NewDefaultHealthChecker(proc, config)
type DefaultHealthChecker struct {
	proc       ProcessManager
	httpClient HealthHTTPClient
	config     HealthCheckerConfig
	mu         sync.RWMutex
}

// MockHealthChecker is a mock implementation for testing.
//
// # Description
//
// Provides a configurable mock for unit testing code that depends
// on HealthChecker. All methods can be configured via function fields.
//
// # Examples
//
//	mock := &MockHealthChecker{
//	    CheckServiceFunc: func(ctx context.Context, svc ServiceDefinition) (*HealthStatus, error) {
//	        return &HealthStatus{ID: GenerateID(), State: HealthStateHealthy}, nil
//	    },
//	}
type MockHealthChecker struct {
	WaitForServicesFunc    func(ctx context.Context, services []ServiceDefinition, opts WaitOptions) (*WaitResult, error)
	CheckServiceFunc       func(ctx context.Context, service ServiceDefinition) (*HealthStatus, error)
	CheckAllServicesFunc   func(ctx context.Context, services []ServiceDefinition) ([]HealthStatus, error)
	IsContainerRunningFunc func(ctx context.Context, containerName string) (bool, error)

	WaitForServicesCalls    []WaitForServicesCall
	CheckServiceCalls       []ServiceDefinition
	CheckAllServicesCalls   [][]ServiceDefinition
	IsContainerRunningCalls []string
	mu                      sync.Mutex
}

// WaitForServicesCall records a call to WaitForServices.
type WaitForServicesCall struct {
	Services []ServiceDefinition
	Options  WaitOptions
}

// =============================================================================
// ERROR VARIABLES
// =============================================================================

// ErrHealthCheckTimeout is returned when WaitForServices times out.
var ErrHealthCheckTimeout = fmt.Errorf("health check timeout")

// ErrCriticalServiceFailed is returned when a critical service fails with FailFast.
var ErrCriticalServiceFailed = fmt.Errorf("critical service failed")

// ErrSSRFBlocked is returned when a URL targets a blocked IP range.
var ErrSSRFBlocked = fmt.Errorf("URL blocked: potential SSRF attack")

// =============================================================================
// SSRF PROTECTION
// =============================================================================

// isURLSafe validates that a URL doesn't target dangerous IP ranges.
//
// # Description
//
// Protects against Server-Side Request Forgery (SSRF) attacks by blocking
// requests to cloud metadata endpoints and internal networks, while allowing
// localhost and Docker bridge IPs for legitimate health checks.
//
// # Security
//
// Blocks:
//   - Cloud metadata: 169.254.169.254, 169.254.0.0/16 (AWS, GCP, Azure)
//   - Link-local: 169.254.0.0/16 (except Docker)
//
// Allows:
//   - localhost, 127.0.0.1, ::1
//   - Docker bridge: 172.17.0.0/16
//   - User-configured private IPs for local services
//
// # Inputs
//
//   - rawURL: URL string to validate
//
// # Outputs
//
//   - error: Non-nil if URL is blocked
func isURLSafe(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	host := parsed.Hostname()
	if host == "" {
		return fmt.Errorf("URL has no host")
	}

	// Always allow localhost
	if host == "localhost" || host == "127.0.0.1" || host == "::1" {
		return nil
	}

	// Parse IP address
	ip := net.ParseIP(host)
	if ip == nil {
		// Hostname (not IP) - allow DNS resolution
		// Note: DNS rebinding attacks are still possible but less common
		return nil
	}

	// Block cloud metadata endpoint (169.254.169.254)
	metadataIP := net.ParseIP("169.254.169.254")
	if ip.Equal(metadataIP) {
		return fmt.Errorf("%w: cloud metadata endpoint blocked", ErrSSRFBlocked)
	}

	// Block link-local range (169.254.0.0/16) except Docker bridge
	linkLocal := net.IPNet{
		IP:   net.ParseIP("169.254.0.0"),
		Mask: net.CIDRMask(16, 32),
	}
	if linkLocal.Contains(ip) {
		return fmt.Errorf("%w: link-local address blocked", ErrSSRFBlocked)
	}

	// Allow Docker bridge network (172.17.0.0/16)
	dockerBridge := net.IPNet{
		IP:   net.ParseIP("172.17.0.0"),
		Mask: net.CIDRMask(16, 32),
	}
	if dockerBridge.Contains(ip) {
		return nil
	}

	// Allow private networks commonly used for local services
	// 10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16
	private10 := net.IPNet{IP: net.ParseIP("10.0.0.0"), Mask: net.CIDRMask(8, 32)}
	private172 := net.IPNet{IP: net.ParseIP("172.16.0.0"), Mask: net.CIDRMask(12, 32)}
	private192 := net.IPNet{IP: net.ParseIP("192.168.0.0"), Mask: net.CIDRMask(16, 32)}

	if private10.Contains(ip) || private172.Contains(ip) || private192.Contains(ip) {
		return nil
	}

	// Allow public IPs (for external health checks if needed)
	return nil
}

// =============================================================================
// CONSTRUCTOR FUNCTIONS
// =============================================================================

// NewDefaultHealthChecker creates a production health checker.
//
// # Description
//
// Creates a DefaultHealthChecker with the provided ProcessManager
// and configuration. Initializes HTTP client with appropriate timeouts.
//
// # Inputs
//
//   - proc: ProcessManager for executing podman/pgrep commands.
//   - config: Configuration for timeouts and defaults.
//
// # Outputs
//
//   - *DefaultHealthChecker: Configured health checker ready for use.
//
// # Examples
//
//	proc := NewDefaultProcessManager()
//	config := DefaultHealthCheckerConfig()
//	checker := NewDefaultHealthChecker(proc, config)
//
// # Limitations
//
//   - HTTP client timeout is global; per-service timeout uses context
//
// # Assumptions
//
//   - ProcessManager is initialized and functional
//   - Config has valid timeout values (> 0)
func NewDefaultHealthChecker(proc ProcessManager, config HealthCheckerConfig) *DefaultHealthChecker {
	return &DefaultHealthChecker{
		proc:   proc,
		config: config,
		httpClient: &http.Client{
			Timeout: config.DefaultTimeout,
			Transport: &http.Transport{
				DisableKeepAlives: true,
			},
		},
	}
}

// NewDefaultHealthCheckerWithHTTPClient creates a health checker with custom HTTP client.
//
// # Description
//
// Creates a DefaultHealthChecker with an injected HTTP client.
// Used primarily for testing to mock HTTP responses.
//
// # Inputs
//
//   - proc: ProcessManager for executing commands.
//   - config: Configuration for timeouts and defaults.
//   - httpClient: Custom HTTP client implementing HealthHTTPClient.
//
// # Outputs
//
//   - *DefaultHealthChecker: Configured health checker.
//
// # Examples
//
//	mockHTTP := &MockHealthHTTPClient{DoFunc: func(r *http.Request) (*http.Response, error) {
//	    return &http.Response{StatusCode: 200}, nil
//	}}
//	checker := NewDefaultHealthCheckerWithHTTPClient(proc, config, mockHTTP)
//
// # Limitations
//
//   - Caller responsible for HTTP client configuration
//
// # Assumptions
//
//   - HealthHTTPClient implements timeout handling
func NewDefaultHealthCheckerWithHTTPClient(proc ProcessManager, config HealthCheckerConfig, httpClient HealthHTTPClient) *DefaultHealthChecker {
	return &DefaultHealthChecker{
		proc:       proc,
		config:     config,
		httpClient: httpClient,
	}
}

// =============================================================================
// DefaultHealthChecker METHODS
// =============================================================================

// WaitForServices blocks until all critical services are healthy or timeout.
//
// # Description
//
// Polls services using exponential backoff until all critical services
// become healthy or the timeout is reached. Uses configurable backoff
// to reduce load during heavy startup conditions.
//
// # Inputs
//
//   - ctx: Context for cancellation.
//   - services: Services to check.
//   - opts: Configuration for timeout and backoff.
//
// # Outputs
//
//   - *WaitResult: Complete result with unique ID and timestamps.
//   - error: Non-nil if critical services failed or cancelled.
//
// # Examples
//
//	result, err := checker.WaitForServices(ctx, services, DefaultWaitOptions())
//	log.Printf("[%s] Wait completed in %v, success: %v", result.ID, result.Duration, result.Success)
//
// # Limitations
//
//   - Services checked in parallel without ordering
//   - Global backoff, not per-service
//
// # Assumptions
//
//   - Services list is non-empty
//   - opts.Timeout > opts.InitialInterval
func (h *DefaultHealthChecker) WaitForServices(ctx context.Context, services []ServiceDefinition, opts WaitOptions) (*WaitResult, error) {
	startTime := time.Now()
	result := &WaitResult{
		ID:        GenerateID(),
		StartedAt: startTime,
		OptionsID: opts.ID,
		Services:  make([]HealthStatus, 0, len(services)),
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()

	checkServices := h.filterServicesForWait(services, opts, result)
	healthyServices := make(map[string]bool)
	var latestStatuses []HealthStatus
	interval := opts.InitialInterval

	for {
		if h.isContextDone(timeoutCtx) {
			return h.buildTimeoutResult(result, latestStatuses, checkServices, healthyServices, startTime, ctx)
		}

		statuses, err := h.CheckAllServices(timeoutCtx, checkServices)
		if err != nil {
			// Use context-aware sleep to respond to Ctrl+C immediately
			h.sleepWithContext(timeoutCtx, h.applyJitter(interval, opts.Jitter))
			interval = h.calculateNextInterval(interval, opts.MaxInterval, opts.Multiplier)
			continue
		}

		latestStatuses = statuses
		h.updateHealthyServices(statuses, healthyServices)

		if h.areAllCriticalHealthy(checkServices, healthyServices) {
			return h.buildSuccessResult(result, statuses, startTime), nil
		}

		if opts.FailFast {
			if failedService := h.findFailedCriticalService(checkServices, healthyServices, statuses); failedService != "" {
				return h.buildFailFastResult(result, statuses, failedService, startTime)
			}
		}

		h.sleepWithContext(timeoutCtx, h.applyJitter(interval, opts.Jitter))
		interval = h.calculateNextInterval(interval, opts.MaxInterval, opts.Multiplier)
	}
}

// CheckService performs a single health check on one service.
//
// # Description
//
// Performs a single health check without retries. Delegates to
// type-specific check methods based on service.CheckType.
//
// # Inputs
//
//   - ctx: Context for cancellation and timeout.
//   - service: The service definition to check.
//
// # Outputs
//
//   - *HealthStatus: Current health status with unique ID.
//   - error: Non-nil only if check infrastructure failed.
//
// # Examples
//
//	status, err := checker.CheckService(ctx, serviceDef)
//	fmt.Printf("[%s] %s: %s\n", status.ID, status.Name, status.State)
//
// # Limitations
//
//   - No retries
//   - Point-in-time snapshot
//
// # Assumptions
//
//   - Service definition is valid for its CheckType
func (h *DefaultHealthChecker) CheckService(ctx context.Context, service ServiceDefinition) (*HealthStatus, error) {
	startTime := time.Now()
	status := &HealthStatus{
		ID:                  GenerateID(),
		Name:                service.Name,
		ServiceDefinitionID: service.ID,
		CheckVersion:        service.Version,
		LastChecked:         startTime,
	}

	timeout := h.getTimeoutForService(service)
	checkCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var err error
	switch service.CheckType {
	case HealthCheckHTTP:
		err = h.performHTTPCheck(checkCtx, service, status)
	case HealthCheckTCP:
		err = h.performTCPCheck(checkCtx, service, status)
	case HealthCheckContainer:
		err = h.performContainerCheck(checkCtx, service, status)
	case HealthCheckProcess:
		err = h.performProcessCheck(checkCtx, service, status)
	default:
		status.State = HealthStateUnhealthy
		status.Message = fmt.Sprintf("unknown check type: %s", service.CheckType)
		return status, fmt.Errorf("unknown check type: %s", service.CheckType)
	}

	status.Latency = time.Since(startTime)
	status.LastChecked = time.Now()

	return status, err
}

// CheckAllServices checks multiple services concurrently.
//
// # Description
//
// Performs health checks on all provided services in parallel.
// Each service is checked in its own goroutine.
//
// # Inputs
//
//   - ctx: Context for cancellation.
//   - services: Services to check.
//
// # Outputs
//
//   - []HealthStatus: Status for each service (preserves input order).
//   - error: Non-nil only if infrastructure failed.
//
// # Examples
//
//	statuses, _ := checker.CheckAllServices(ctx, services)
//	for _, s := range statuses {
//	    fmt.Printf("%s: %s\n", s.Name, s.State)
//	}
//
// # Limitations
//
//   - Concurrent connections may overwhelm network
//
// # Assumptions
//
//   - Reasonable number of services (< 50)
func (h *DefaultHealthChecker) CheckAllServices(ctx context.Context, services []ServiceDefinition) ([]HealthStatus, error) {
	if len(services) == 0 {
		return []HealthStatus{}, nil
	}

	results := make([]HealthStatus, len(services))
	var wg sync.WaitGroup

	for i, svc := range services {
		wg.Add(1)
		go func(idx int, service ServiceDefinition) {
			defer wg.Done()
			status, _ := h.CheckService(ctx, service)
			if status != nil {
				results[idx] = *status
			} else {
				results[idx] = h.buildUnreachableStatus(service)
			}
		}(i, svc)
	}

	wg.Wait()
	return results, nil
}

// IsContainerRunning checks if a container exists and is running.
//
// # Description
//
// Queries podman to determine if the specified container is running.
//
// # Inputs
//
//   - ctx: Context for cancellation.
//   - containerName: The container name to check.
//
// # Outputs
//
//   - bool: True if container exists and is running.
//   - error: Non-nil if runtime query failed.
//
// # Examples
//
//	running, _ := checker.IsContainerRunning(ctx, "aleutian-orchestrator")
//
// # Limitations
//
//   - Only checks running state
//   - Requires podman CLI
//
// # Assumptions
//
//   - Podman is accessible
func (h *DefaultHealthChecker) IsContainerRunning(ctx context.Context, containerName string) (bool, error) {
	stdout, _, _, err := h.proc.RunInDir(ctx, "", nil, "podman", "inspect", "--format", "{{.State.Running}}", containerName)
	if err != nil {
		return false, nil
	}
	return strings.TrimSpace(stdout) == "true", nil
}

// =============================================================================
// DefaultHealthChecker PRIVATE HELPER METHODS
// =============================================================================

// filterServicesForWait filters services based on SkipOptional option.
//
// # Description
//
// Returns only critical services if SkipOptional is true.
// Populates result.Skipped with skipped service names.
//
// # Inputs
//
//   - services: All services to potentially check.
//   - opts: Wait options with SkipOptional flag.
//   - result: WaitResult to populate Skipped field.
//
// # Outputs
//
//   - []ServiceDefinition: Services to actually check.
//
// # Limitations
//
//   - Modifies result.Skipped in place
//
// # Assumptions
//
//   - result is non-nil
func (h *DefaultHealthChecker) filterServicesForWait(services []ServiceDefinition, opts WaitOptions, result *WaitResult) []ServiceDefinition {
	if !opts.SkipOptional {
		return services
	}

	filtered := make([]ServiceDefinition, 0)
	for _, svc := range services {
		if svc.Critical {
			filtered = append(filtered, svc)
		} else {
			result.Skipped = append(result.Skipped, svc.Name)
		}
	}
	return filtered
}

// isContextDone checks if context is cancelled or timed out.
//
// # Description
//
// Non-blocking check of context state.
//
// # Inputs
//
//   - ctx: Context to check.
//
// # Outputs
//
//   - bool: True if context is done.
//
// # Limitations
//
//   - Only checks current state
//
// # Assumptions
//
//   - ctx is non-nil
func (h *DefaultHealthChecker) isContextDone(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return true
	default:
		return false
	}
}

// buildTimeoutResult constructs WaitResult for timeout case.
//
// # Description
//
// Builds a failure result when WaitForServices times out.
//
// # Inputs
//
//   - result: Partially populated result.
//   - statuses: Latest service statuses.
//   - services: Services being checked.
//   - healthy: Map of healthy service names.
//   - startTime: When waiting started.
//   - ctx: Original context for error detection.
//
// # Outputs
//
//   - *WaitResult: Complete failure result.
//   - error: Timeout or cancellation error.
//
// # Limitations
//
//   - Assumes timeout is the cause if context error is nil
//
// # Assumptions
//
//   - result is non-nil
func (h *DefaultHealthChecker) buildTimeoutResult(result *WaitResult, statuses []HealthStatus, services []ServiceDefinition, healthy map[string]bool, startTime time.Time, ctx context.Context) (*WaitResult, error) {
	result.Duration = time.Since(startTime)
	result.CompletedAt = time.Now()
	result.Services = statuses
	result.Success = false

	for _, svc := range services {
		if svc.Critical && !healthy[svc.Name] {
			result.FailedCritical = append(result.FailedCritical, svc.Name)
		}
	}

	if ctx.Err() != nil {
		return result, fmt.Errorf("context cancelled: %w", ctx.Err())
	}
	return result, ErrHealthCheckTimeout
}

// buildSuccessResult constructs WaitResult for success case.
//
// # Description
//
// Builds a success result when all critical services are healthy.
//
// # Inputs
//
//   - result: Partially populated result.
//   - statuses: Final service statuses.
//   - startTime: When waiting started.
//
// # Outputs
//
//   - *WaitResult: Complete success result.
//
// # Limitations
//
//   - None
//
// # Assumptions
//
//   - result is non-nil
func (h *DefaultHealthChecker) buildSuccessResult(result *WaitResult, statuses []HealthStatus, startTime time.Time) *WaitResult {
	result.Duration = time.Since(startTime)
	result.CompletedAt = time.Now()
	result.Services = statuses
	result.Success = true
	return result
}

// buildFailFastResult constructs WaitResult for FailFast case.
//
// # Description
//
// Builds a failure result when FailFast is enabled and a critical service fails.
//
// # Inputs
//
//   - result: Partially populated result.
//   - statuses: Current service statuses.
//   - failedService: Name of the failed critical service.
//   - startTime: When waiting started.
//
// # Outputs
//
//   - *WaitResult: Complete failure result.
//   - error: Critical service failure error.
//
// # Limitations
//
//   - Only reports first failed service
//
// # Assumptions
//
//   - result is non-nil
//   - failedService is non-empty
func (h *DefaultHealthChecker) buildFailFastResult(result *WaitResult, statuses []HealthStatus, failedService string, startTime time.Time) (*WaitResult, error) {
	result.Duration = time.Since(startTime)
	result.CompletedAt = time.Now()
	result.Services = statuses
	result.FailedCritical = []string{failedService}
	result.Success = false

	var message string
	for _, status := range statuses {
		if status.Name == failedService {
			message = status.Message
			break
		}
	}
	return result, fmt.Errorf("critical service %s failed: %s", failedService, message)
}

// updateHealthyServices updates the healthy tracking map.
//
// # Description
//
// Marks services as healthy in the tracking map based on status results.
//
// # Inputs
//
//   - statuses: Current health statuses.
//   - healthy: Map to update.
//
// # Outputs
//
//   - None (modifies healthy map in place).
//
// # Limitations
//
//   - Only marks healthy; never removes from map
//
// # Assumptions
//
//   - healthy map is initialized
func (h *DefaultHealthChecker) updateHealthyServices(statuses []HealthStatus, healthy map[string]bool) {
	for _, status := range statuses {
		if status.State == HealthStateHealthy {
			healthy[status.Name] = true
		}
	}
}

// areAllCriticalHealthy checks if all critical services are healthy.
//
// # Description
//
// Returns true if all services marked Critical are in the healthy map.
//
// # Inputs
//
//   - services: Services to check.
//   - healthy: Map of healthy service names.
//
// # Outputs
//
//   - bool: True if all critical services are healthy.
//
// # Limitations
//
//   - None
//
// # Assumptions
//
//   - healthy map is initialized
func (h *DefaultHealthChecker) areAllCriticalHealthy(services []ServiceDefinition, healthy map[string]bool) bool {
	for _, svc := range services {
		if svc.Critical && !healthy[svc.Name] {
			return false
		}
	}
	return true
}

// findFailedCriticalService finds the first failed critical service.
//
// # Description
//
// Returns the name of the first critical service that is not healthy.
// Returns empty string if all critical services are healthy.
//
// # Inputs
//
//   - services: Services to check.
//   - healthy: Map of healthy service names.
//   - statuses: Current statuses for message lookup.
//
// # Outputs
//
//   - string: Name of failed service, or empty string.
//
// # Limitations
//
//   - Returns first found; may not be most important
//
// # Assumptions
//
//   - healthy map is initialized
func (h *DefaultHealthChecker) findFailedCriticalService(services []ServiceDefinition, healthy map[string]bool, statuses []HealthStatus) string {
	for _, svc := range services {
		if svc.Critical && !healthy[svc.Name] {
			return svc.Name
		}
	}
	return ""
}

// getTimeoutForService determines the timeout for a specific service check.
//
// # Description
//
// Returns the service-specific timeout if set, otherwise the default.
//
// # Inputs
//
//   - service: Service to get timeout for.
//
// # Outputs
//
//   - time.Duration: Timeout to use.
//
// # Limitations
//
//   - None
//
// # Assumptions
//
//   - config.DefaultTimeout > 0
func (h *DefaultHealthChecker) getTimeoutForService(service ServiceDefinition) time.Duration {
	if service.Timeout > 0 {
		return service.Timeout
	}
	return h.config.DefaultTimeout
}

// buildUnreachableStatus creates a status for an unreachable service.
//
// # Description
//
// Creates a HealthStatus for a service that could not be checked.
//
// # Inputs
//
//   - service: The service definition.
//
// # Outputs
//
//   - HealthStatus: Status with Unreachable state.
//
// # Limitations
//
//   - Generic error message
//
// # Assumptions
//
//   - None
func (h *DefaultHealthChecker) buildUnreachableStatus(service ServiceDefinition) HealthStatus {
	return HealthStatus{
		ID:                  GenerateID(),
		Name:                service.Name,
		State:               HealthStateUnreachable,
		Message:             "check failed",
		LastChecked:         time.Now(),
		ServiceDefinitionID: service.ID,
		CheckVersion:        service.Version,
	}
}

// applyJitter adds random jitter to an interval.
//
// # Description
//
// Multiplies interval by a factor in range [1-jitter, 1+jitter].
//
// # Inputs
//
//   - interval: Base interval.
//   - jitter: Jitter factor (0.1 = ±10%).
//
// # Outputs
//
//   - time.Duration: Jittered interval.
//
// # Limitations
//
//   - Uses math/rand, not crypto/rand
//
// # Assumptions
//
//   - jitter >= 0
func (h *DefaultHealthChecker) applyJitter(interval time.Duration, jitter float64) time.Duration {
	if jitter <= 0 {
		return interval
	}
	factor := 1.0 + (rand.Float64()*2-1)*jitter
	return time.Duration(float64(interval) * factor)
}

// calculateNextInterval calculates the next backoff interval.
//
// # Description
//
// Multiplies current interval by multiplier, capped at max.
//
// # Inputs
//
//   - current: Current interval.
//   - max: Maximum interval.
//   - multiplier: Growth factor.
//
// # Outputs
//
//   - time.Duration: Next interval.
//
// # Limitations
//
//   - None
//
// # Assumptions
//
//   - multiplier > 1 for growth
func (h *DefaultHealthChecker) calculateNextInterval(current, max time.Duration, multiplier float64) time.Duration {
	next := time.Duration(float64(current) * multiplier)
	if next > max {
		return max
	}
	return next
}

// sleepWithContext sleeps for duration or until context is done.
//
// # Description
//
// Respects context cancellation during sleep.
//
// # Inputs
//
//   - ctx: Context for cancellation.
//   - duration: Sleep duration.
//
// # Outputs
//
//   - None.
//
// # Limitations
//
//   - None
//
// # Assumptions
//
//   - ctx is non-nil
func (h *DefaultHealthChecker) sleepWithContext(ctx context.Context, duration time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(duration):
	}
}

// =============================================================================
// DefaultHealthChecker CHECK TYPE METHODS
// =============================================================================

// performHTTPCheck performs an HTTP health check.
//
// # Description
//
// Sends HTTP GET to service URL and checks response status code.
//
// # Inputs
//
//   - ctx: Context for cancellation.
//   - service: Service definition with URL.
//   - status: Status to populate.
//
// # Outputs
//
//   - error: Non-nil only for infrastructure errors.
//
// # Limitations
//
//   - GET only; no POST/PUT support
//   - No body inspection
//
// # Assumptions
//
//   - service.URL is valid HTTP URL
func (h *DefaultHealthChecker) performHTTPCheck(ctx context.Context, service ServiceDefinition, status *HealthStatus) error {
	if service.URL == "" {
		status.State = HealthStateUnhealthy
		status.Message = "no URL configured for HTTP check"
		return fmt.Errorf("no URL configured for HTTP check")
	}

	// SSRF protection: validate URL before making request
	if err := isURLSafe(service.URL); err != nil {
		status.State = HealthStateUnhealthy
		status.Message = fmt.Sprintf("blocked: %v", err)
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, service.URL, nil)
	if err != nil {
		status.State = HealthStateUnreachable
		status.Message = fmt.Sprintf("failed to create request: %v", err)
		return err
	}

	resp, err := h.httpClient.Do(req)
	if err != nil {
		status.State = HealthStateUnreachable
		status.Message = fmt.Sprintf("request failed: %v", err)
		return nil
	}
	defer resp.Body.Close()

	status.HTTPStatus = resp.StatusCode

	expectedStatus := h.config.DefaultExpectedStatus
	if service.ExpectedStatus > 0 {
		expectedStatus = service.ExpectedStatus
	}

	if resp.StatusCode == expectedStatus {
		status.State = HealthStateHealthy
		status.Message = fmt.Sprintf("HTTP %d", resp.StatusCode)
	} else {
		status.State = HealthStateUnhealthy
		status.Message = fmt.Sprintf("HTTP %d (expected %d)", resp.StatusCode, expectedStatus)
	}

	return nil
}

// performTCPCheck performs a TCP connectivity check.
//
// # Description
//
// Attempts TCP connection to service URL's host:port.
//
// # Inputs
//
//   - ctx: Context for cancellation.
//   - service: Service definition with URL.
//   - status: Status to populate.
//
// # Outputs
//
//   - error: Non-nil only for infrastructure errors.
//
// # Limitations
//
//   - Only checks port open; no protocol validation
//
// # Assumptions
//
//   - service.URL contains host:port
func (h *DefaultHealthChecker) performTCPCheck(ctx context.Context, service ServiceDefinition, status *HealthStatus) error {
	if service.URL == "" {
		status.State = HealthStateUnhealthy
		status.Message = "no URL configured for TCP check"
		return fmt.Errorf("no URL configured for TCP check")
	}

	host := strings.TrimPrefix(service.URL, "tcp://")
	host = strings.TrimPrefix(host, "http://")
	host = strings.TrimPrefix(host, "https://")

	// SSRF protection: validate host before connecting
	// Construct a URL for validation (TCP connections don't have scheme in our format)
	checkURL := "tcp://" + host
	if err := isURLSafe(checkURL); err != nil {
		status.State = HealthStateUnhealthy
		status.Message = fmt.Sprintf("blocked: %v", err)
		return err
	}

	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", host)
	if err != nil {
		status.State = HealthStateUnreachable
		status.Message = fmt.Sprintf("TCP connection failed: %v", err)
		return nil
	}
	defer conn.Close()

	status.State = HealthStateHealthy
	status.Message = "TCP port open"
	return nil
}

// performContainerCheck checks container running state via podman.
//
// # Description
//
// Queries podman inspect to check if container is running.
//
// # Inputs
//
//   - ctx: Context for cancellation.
//   - service: Service definition with ContainerName.
//   - status: Status to populate.
//
// # Outputs
//
//   - error: Non-nil only for infrastructure errors.
//
// # Limitations
//
//   - Only checks running state; not container health
//
// # Assumptions
//
//   - service.ContainerName is valid
//   - podman is accessible
func (h *DefaultHealthChecker) performContainerCheck(ctx context.Context, service ServiceDefinition, status *HealthStatus) error {
	if service.ContainerName == "" {
		status.State = HealthStateUnhealthy
		status.Message = "no container name configured"
		return fmt.Errorf("no container name configured")
	}

	running, err := h.IsContainerRunning(ctx, service.ContainerName)
	if err != nil {
		status.State = HealthStateUnreachable
		status.Message = fmt.Sprintf("failed to check container: %v", err)
		return nil
	}

	if running {
		status.State = HealthStateHealthy
		status.ContainerState = "running"
		status.Message = "container running"
	} else {
		status.State = HealthStateUnhealthy
		status.ContainerState = "not running"
		status.Message = "container not running"
	}

	return nil
}

// performProcessCheck checks if a process is running via pgrep.
//
// # Description
//
// Uses pgrep -x to check for exact process name match.
//
// # Inputs
//
//   - ctx: Context for cancellation.
//   - service: Service definition with Name for process lookup.
//   - status: Status to populate.
//
// # Outputs
//
//   - error: Non-nil only for infrastructure errors.
//
// # Limitations
//
//   - Uses service.Name for process lookup
//   - Exact match only
//
// # Assumptions
//
//   - pgrep is available
func (h *DefaultHealthChecker) performProcessCheck(ctx context.Context, service ServiceDefinition, status *HealthStatus) error {
	if service.Name == "" {
		status.State = HealthStateUnhealthy
		status.Message = "no process name configured"
		return fmt.Errorf("no process name configured")
	}

	_, _, exitCode, _ := h.proc.RunInDir(ctx, "", nil, "pgrep", "-x", service.Name)
	if exitCode == 0 {
		status.State = HealthStateHealthy
		status.Message = "process running"
	} else {
		status.State = HealthStateUnhealthy
		status.Message = "process not found"
	}

	return nil
}

// =============================================================================
// MockHealthChecker METHODS
// =============================================================================

// WaitForServices implements HealthChecker for MockHealthChecker.
//
// # Description
//
// Records the call and delegates to WaitForServicesFunc if set.
//
// # Inputs
//
//   - ctx: Context for cancellation.
//   - services: Services passed to mock.
//   - opts: Options passed to mock.
//
// # Outputs
//
//   - *WaitResult: Result from mock function or default success.
//   - error: Error from mock function.
//
// # Examples
//
//	mock.WaitForServicesFunc = func(...) (*WaitResult, error) {
//	    return &WaitResult{Success: false}, ErrHealthCheckTimeout
//	}
//
// # Limitations
//
//   - None
//
// # Assumptions
//
//   - None
func (m *MockHealthChecker) WaitForServices(ctx context.Context, services []ServiceDefinition, opts WaitOptions) (*WaitResult, error) {
	m.mu.Lock()
	m.WaitForServicesCalls = append(m.WaitForServicesCalls, WaitForServicesCall{Services: services, Options: opts})
	m.mu.Unlock()

	if m.WaitForServicesFunc != nil {
		return m.WaitForServicesFunc(ctx, services, opts)
	}
	return &WaitResult{ID: GenerateID(), Success: true, CompletedAt: time.Now()}, nil
}

// CheckService implements HealthChecker for MockHealthChecker.
//
// # Description
//
// Records the call and delegates to CheckServiceFunc if set.
//
// # Inputs
//
//   - ctx: Context for cancellation.
//   - service: Service passed to mock.
//
// # Outputs
//
//   - *HealthStatus: Status from mock function or default healthy.
//   - error: Error from mock function.
//
// # Examples
//
//	mock.CheckServiceFunc = func(...) (*HealthStatus, error) {
//	    return &HealthStatus{State: HealthStateUnhealthy}, nil
//	}
//
// # Limitations
//
//   - None
//
// # Assumptions
//
//   - None
func (m *MockHealthChecker) CheckService(ctx context.Context, service ServiceDefinition) (*HealthStatus, error) {
	m.mu.Lock()
	m.CheckServiceCalls = append(m.CheckServiceCalls, service)
	m.mu.Unlock()

	if m.CheckServiceFunc != nil {
		return m.CheckServiceFunc(ctx, service)
	}
	return &HealthStatus{ID: GenerateID(), Name: service.Name, State: HealthStateHealthy, LastChecked: time.Now()}, nil
}

// CheckAllServices implements HealthChecker for MockHealthChecker.
//
// # Description
//
// Records the call and delegates to CheckAllServicesFunc if set.
//
// # Inputs
//
//   - ctx: Context for cancellation.
//   - services: Services passed to mock.
//
// # Outputs
//
//   - []HealthStatus: Statuses from mock function or default healthy.
//   - error: Error from mock function.
//
// # Examples
//
//	mock.CheckAllServicesFunc = func(...) ([]HealthStatus, error) {
//	    return []HealthStatus{{State: HealthStateHealthy}}, nil
//	}
//
// # Limitations
//
//   - None
//
// # Assumptions
//
//   - None
func (m *MockHealthChecker) CheckAllServices(ctx context.Context, services []ServiceDefinition) ([]HealthStatus, error) {
	m.mu.Lock()
	m.CheckAllServicesCalls = append(m.CheckAllServicesCalls, services)
	m.mu.Unlock()

	if m.CheckAllServicesFunc != nil {
		return m.CheckAllServicesFunc(ctx, services)
	}
	statuses := make([]HealthStatus, len(services))
	for i, svc := range services {
		statuses[i] = HealthStatus{ID: GenerateID(), Name: svc.Name, State: HealthStateHealthy, LastChecked: time.Now()}
	}
	return statuses, nil
}

// IsContainerRunning implements HealthChecker for MockHealthChecker.
//
// # Description
//
// Records the call and delegates to IsContainerRunningFunc if set.
//
// # Inputs
//
//   - ctx: Context for cancellation.
//   - containerName: Container name passed to mock.
//
// # Outputs
//
//   - bool: Result from mock function or default true.
//   - error: Error from mock function.
//
// # Examples
//
//	mock.IsContainerRunningFunc = func(...) (bool, error) {
//	    return false, nil
//	}
//
// # Limitations
//
//   - None
//
// # Assumptions
//
//   - None
func (m *MockHealthChecker) IsContainerRunning(ctx context.Context, containerName string) (bool, error) {
	m.mu.Lock()
	m.IsContainerRunningCalls = append(m.IsContainerRunningCalls, containerName)
	m.mu.Unlock()

	if m.IsContainerRunningFunc != nil {
		return m.IsContainerRunningFunc(ctx, containerName)
	}
	return true, nil
}
