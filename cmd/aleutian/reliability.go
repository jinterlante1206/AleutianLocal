package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ReliabilityOrchestrator defines the interface for coordinating reliability subsystems.
//
// # Description
//
// ReliabilityOrchestrator is the central interface for managing all Phase 10
// reliability components including process safety, observability, data safety,
// and security subsystems.
//
// # Thread Safety
//
// Implementations must be safe for concurrent use from multiple goroutines.
//
// # Use Cases
//
//   - CLI startup initialization
//   - Background health monitoring
//   - Graceful shutdown coordination
//
// # Example
//
//	manager := NewReliabilityManager(DefaultReliabilityConfig())
//	if err := manager.Initialize(ctx); err != nil {
//	    log.Fatal(err)
//	}
//	defer manager.Shutdown()
//
//	// Use reliability features
//	if err := manager.AcquireProcessLock(); err != nil {
//	    log.Fatal("Another instance is running")
//	}
//	defer manager.ReleaseProcessLock()
//
// # Limitations
//
//   - Must call Initialize() before using other methods
//   - Process lock only works on single machine (not distributed)
//   - State audit requires periodic background task
//
// # Assumptions
//
//   - File system is accessible for locks and backups
//   - Container runtime is available for image validation
type ReliabilityOrchestrator interface {
	// Initialize starts all reliability subsystems.
	// Must be called before using other methods.
	Initialize(ctx context.Context) error

	// Shutdown stops all subsystems gracefully.
	// Should be called on application exit.
	Shutdown()

	// AcquireProcessLock acquires the CLI mutex.
	// Prevents concurrent CLI executions.
	AcquireProcessLock() error

	// ReleaseProcessLock releases the CLI mutex.
	ReleaseProcessLock() error

	// CheckResources validates system resources (FD limits, etc.).
	CheckResources() ResourceLimits

	// ValidateImage checks if a container image is properly pinned.
	ValidateImage(image string) (ImageValidation, error)

	// BackupBeforeChange creates a backup before modifying a file.
	BackupBeforeChange(path string) (string, error)

	// ProposeRecovery asks user before expensive recovery actions.
	ProposeRecovery(ctx context.Context, issue string, action RecoveryAction) error

	// ShouldSample returns whether to sample this operation.
	ShouldSample() bool

	// RecordLatency records operation latency for adaptive sampling.
	RecordLatency(latency time.Duration)

	// ValidateMetric checks if a metric name is valid.
	ValidateMetric(name string) error

	// NormalizeLabel normalizes a label value to prevent cardinality explosion.
	NormalizeLabel(labelName, value string) string

	// TrackGoroutine tracks a goroutine for leak detection.
	// Returns cleanup function to call on goroutine exit.
	TrackGoroutine(name string) func()

	// GetGoroutineStats returns goroutine statistics.
	GetGoroutineStats() GoroutineStats

	// RegisterStateForAudit registers state for drift detection.
	RegisterStateForAudit(name string, opts StateRegistration) error

	// GetDriftReport returns the latest state drift report.
	GetDriftReport() DriftReport

	// CreateSaga creates a new saga for multi-step operations.
	CreateSaga() SagaExecutor

	// HealthCheck performs a comprehensive reliability health check.
	HealthCheck() ReliabilityHealthCheck
}

// ReliabilityConfig configures the reliability manager.
//
// # Description
//
// Defines configuration for all reliability subsystems including
// directories, intervals, and feature flags.
//
// # Example
//
//	config := ReliabilityConfig{
//	    DataDir:           "/var/lib/aleutian",
//	    EnableProcessLock: true,
//	    SamplingRate:      0.1,
//	}
type ReliabilityConfig struct {
	// DataDir is the base directory for data files.
	// Default: ~/.aleutian
	DataDir string

	// LockDir is the directory for lock files.
	// Default: ~/.aleutian/locks
	LockDir string

	// BackupDir is the directory for backups.
	// Default: ~/.aleutian/backups
	BackupDir string

	// EnableProcessLock enables CLI mutex to prevent concurrent execution.
	// Default: true
	EnableProcessLock bool

	// EnableRetentionEnforcement enables automatic data cleanup.
	// Default: true
	EnableRetentionEnforcement bool

	// RetentionCheckInterval is how often to run retention checks.
	// Default: 24 hours
	RetentionCheckInterval time.Duration

	// EnableStateAudit enables periodic state drift checks.
	// Default: true
	EnableStateAudit bool

	// StateAuditInterval is how often to audit state.
	// Default: 5 minutes
	StateAuditInterval time.Duration

	// EnableImageValidation validates container images are pinned.
	// Default: true
	EnableImageValidation bool

	// SamplingRate is the base sampling rate for observability (0.0-1.0).
	// Default: 0.1 (10%)
	SamplingRate float64
}

// ReliabilityHealthCheck contains results of a reliability health check.
//
// # Description
//
// Provides a snapshot of all reliability subsystem statuses.
type ReliabilityHealthCheck struct {
	// Timestamp is when the check was performed.
	Timestamp time.Time

	// ResourcesOK indicates if resource limits are acceptable.
	ResourcesOK bool

	// ResourceWarnings contains resource-related warnings.
	ResourceWarnings []string

	// GoroutineCount is the current number of tracked goroutines.
	GoroutineCount int64

	// GoroutinePeak is the peak goroutine count.
	GoroutinePeak int64

	// SamplingRate is the current adaptive sampling rate.
	SamplingRate float64

	// DriftingStates lists states currently showing drift.
	DriftingStates []string

	// ProcessLockHeld indicates if the CLI mutex is held.
	ProcessLockHeld bool
}

// DefaultReliabilityConfig returns sensible defaults.
//
// # Description
//
// Returns configuration with reasonable default values for all settings.
// Uses ~/.aleutian as the base directory.
//
// # Outputs
//
//   - ReliabilityConfig: Configuration with default values
//
// # Example
//
//	config := DefaultReliabilityConfig()
//	config.SamplingRate = 0.5 // Override sampling rate
//	manager := NewReliabilityManager(config)
func DefaultReliabilityConfig() ReliabilityConfig {
	homeDir, _ := os.UserHomeDir()
	dataDir := filepath.Join(homeDir, ".aleutian")

	return ReliabilityConfig{
		DataDir:                    dataDir,
		LockDir:                    filepath.Join(dataDir, "locks"),
		BackupDir:                  filepath.Join(dataDir, "backups"),
		EnableProcessLock:          true,
		EnableRetentionEnforcement: true,
		RetentionCheckInterval:     24 * time.Hour,
		EnableStateAudit:           true,
		StateAuditInterval:         5 * time.Minute,
		EnableImageValidation:      true,
		SamplingRate:               0.1,
	}
}

// ReliabilityManager implements ReliabilityOrchestrator.
//
// # Description
//
// ReliabilityManager is the concrete implementation that coordinates
// all Phase 10 reliability components. It manages:
//
//   - Process safety (locks, sagas, goroutine tracking)
//   - Observability (adaptive sampling, metrics schema)
//   - Data safety (backups, retention policies)
//   - Security (image validation, state auditing)
//
// # Thread Safety
//
// ReliabilityManager is safe for concurrent use. Internal state is
// protected by a read-write mutex.
//
// # Lifecycle
//
//  1. Create with NewReliabilityManager()
//  2. Initialize with Initialize()
//  3. Use reliability features
//  4. Shutdown with Shutdown()
//
// # Limitations
//
//   - Process lock is file-based, only works on single machine
//   - Retention enforcement requires background goroutine
//   - Image validation requires docker/podman for digest resolution
//
// # Assumptions
//
//   - Write access to DataDir, LockDir, BackupDir
//   - Sufficient file descriptors for lock files
//   - Container runtime available if EnableImageValidation is true
type ReliabilityManager struct {
	config ReliabilityConfig

	// Subsystems
	processLock      *ProcessLock
	goroutineTracker *GoroutineTracker
	sampler          *DefaultAdaptiveSampler
	metricsSchema    *DefaultMetricsSchema
	backupManager    *DefaultBackupManager
	retention        *DefaultRetentionEnforcer
	imageValidator   *DefaultImagePinValidator
	stateAuditor     *DefaultStateAuditor
	resourceChecker  *DefaultResourceLimitsChecker

	// State
	initialized bool
	mu          sync.RWMutex

	// Background tasks
	stopCh chan struct{}
	wg     sync.WaitGroup
}

// NewReliabilityManager creates a new reliability manager.
//
// # Description
//
// Creates a manager with the specified configuration but does not
// initialize subsystems. Call Initialize() to start subsystems.
//
// # Inputs
//
//   - config: Configuration for all subsystems
//
// # Outputs
//
//   - *ReliabilityManager: New manager instance (not yet initialized)
//
// # Example
//
//	config := DefaultReliabilityConfig()
//	manager := NewReliabilityManager(config)
//	defer manager.Shutdown()
//
//	if err := manager.Initialize(ctx); err != nil {
//	    log.Fatal(err)
//	}
//
// # Limitations
//
//   - Manager is not usable until Initialize() is called
//
// # Assumptions
//
//   - Configuration values are valid
func NewReliabilityManager(config ReliabilityConfig) *ReliabilityManager {
	return &ReliabilityManager{
		config: config,
		stopCh: make(chan struct{}),
	}
}

// Initialize starts all reliability subsystems.
//
// # Description
//
// Initializes all enabled subsystems and starts background tasks.
// Must be called before using other methods. Safe to call multiple
// times (subsequent calls are no-ops).
//
// # Inputs
//
//   - ctx: Context for initialization (used for cancellation)
//
// # Outputs
//
//   - error: Non-nil if initialization fails (directory creation, etc.)
//
// # Example
//
//	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
//	defer cancel()
//
//	if err := manager.Initialize(ctx); err != nil {
//	    log.Fatalf("Failed to initialize: %v", err)
//	}
//
// # Limitations
//
//   - Creates directories with 0750 permissions
//   - Background tasks start immediately after initialization
//
// # Assumptions
//
//   - File system is writable at configured paths
//   - Process has sufficient permissions
func (rm *ReliabilityManager) Initialize(ctx context.Context) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	if rm.initialized {
		return nil
	}

	// Ensure directories exist
	if err := rm.ensureDirectories(); err != nil {
		return fmt.Errorf("failed to create directories: %w", err)
	}

	// Initialize subsystems
	if err := rm.initializeSubsystems(); err != nil {
		return fmt.Errorf("failed to initialize subsystems: %w", err)
	}

	// Start background tasks
	rm.startBackgroundTasks()

	rm.initialized = true
	return nil
}

// ensureDirectories creates required directories.
func (rm *ReliabilityManager) ensureDirectories() error {
	dirs := []string{
		rm.config.DataDir,
		rm.config.LockDir,
		rm.config.BackupDir,
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0750); err != nil {
			return err
		}
	}

	return nil
}

// initializeSubsystems creates all subsystem instances.
func (rm *ReliabilityManager) initializeSubsystems() error {
	// Process lock
	if rm.config.EnableProcessLock {
		rm.processLock = NewProcessLock(ProcessLockConfig{
			LockDir:  rm.config.LockDir,
			LockName: "aleutian",
		})
	}

	// Goroutine tracker
	rm.goroutineTracker = NewGoroutineTracker(GoroutineTrackerConfig{
		LongRunningThreshold: 5 * time.Minute,
	})

	// Adaptive sampler
	rm.sampler = NewAdaptiveSampler(SamplingConfig{
		BaseSamplingRate: rm.config.SamplingRate,
		MinSamplingRate:  0.01,
		MaxSamplingRate:  1.0,
		LatencyThreshold: 100 * time.Millisecond,
	})

	// Metrics schema
	rm.metricsSchema = NewMetricsSchema(DefaultMetricsSchemaConfig())

	// Backup manager
	rm.backupManager = NewBackupManager(BackupConfig{
		BackupDir:  rm.config.BackupDir,
		MaxBackups: 10,
	})

	// Retention enforcer
	if rm.config.EnableRetentionEnforcement {
		rm.retention = NewRetentionEnforcer(RetentionConfig{
			DryRun:      false,
			ArchivePath: filepath.Join(rm.config.DataDir, "archive"),
		})
	}

	// Image validator
	if rm.config.EnableImageValidation {
		rm.imageValidator = NewImagePinValidator(DefaultImagePinConfig())
	}

	// State auditor
	if rm.config.EnableStateAudit {
		rm.stateAuditor = NewStateAuditor(DefaultStateAuditConfig())
	}

	// Resource checker
	rm.resourceChecker = NewResourceLimitsChecker(DefaultResourceLimitsConfig())

	return nil
}

// startBackgroundTasks starts periodic maintenance tasks.
func (rm *ReliabilityManager) startBackgroundTasks() {
	// Retention enforcement
	if rm.config.EnableRetentionEnforcement && rm.retention != nil {
		rm.wg.Add(1)
		go rm.runRetentionLoop()
	}

	// State audit
	if rm.config.EnableStateAudit && rm.stateAuditor != nil {
		rm.stateAuditor.StartPeriodicAudit(rm.config.StateAuditInterval)
	}
}

// runRetentionLoop periodically enforces retention policies.
func (rm *ReliabilityManager) runRetentionLoop() {
	defer rm.wg.Done()

	ticker := time.NewTicker(rm.config.RetentionCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-rm.stopCh:
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			rm.retention.Enforce(ctx)
			cancel()
		}
	}
}

// Shutdown stops all subsystems gracefully.
//
// # Description
//
// Stops background tasks and releases resources. Should be called
// on application exit. Safe to call multiple times.
//
// # Example
//
//	defer manager.Shutdown()
//
// # Limitations
//
//   - Blocks until all background tasks complete
//
// # Assumptions
//
//   - Manager was initialized before shutdown
func (rm *ReliabilityManager) Shutdown() {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	if !rm.initialized {
		return
	}

	// Signal stop
	close(rm.stopCh)

	// Stop subsystems
	if rm.sampler != nil {
		rm.sampler.Stop()
	}
	if rm.stateAuditor != nil {
		rm.stateAuditor.StopPeriodicAudit()
	}

	// Wait for background tasks
	rm.wg.Wait()

	rm.initialized = false
}

// AcquireProcessLock acquires the CLI mutex.
//
// # Description
//
// Prevents concurrent CLI executions using file-based locking.
// Must call ReleaseProcessLock when done.
//
// # Outputs
//
//   - error: Non-nil if lock cannot be acquired (another instance running)
//
// # Example
//
//	if err := manager.AcquireProcessLock(); err != nil {
//	    log.Fatal("Another instance is already running")
//	}
//	defer manager.ReleaseProcessLock()
//
// # Limitations
//
//   - File-based lock, only works on single machine
//   - Stale locks may need manual cleanup
//
// # Assumptions
//
//   - LockDir is writable
//   - Process has permissions to create lock files
func (rm *ReliabilityManager) AcquireProcessLock() error {
	if rm.processLock == nil {
		return nil
	}
	return rm.processLock.Acquire()
}

// ReleaseProcessLock releases the CLI mutex.
//
// # Description
//
// Releases the process lock acquired by AcquireProcessLock.
// Safe to call even if lock was not acquired.
//
// # Outputs
//
//   - error: Non-nil if release fails
//
// # Example
//
//	defer manager.ReleaseProcessLock()
func (rm *ReliabilityManager) ReleaseProcessLock() error {
	if rm.processLock == nil {
		return nil
	}
	return rm.processLock.Release()
}

// CheckResources validates system resources.
//
// # Description
//
// Checks file descriptor limits and other system resources.
// Returns warnings if limits are too low.
//
// # Outputs
//
//   - ResourceLimits: Current resource status with any warnings
//
// # Example
//
//	limits := manager.CheckResources()
//	if limits.HasWarnings() {
//	    for _, w := range limits.Warnings {
//	        log.Printf("WARNING: %s", w)
//	    }
//	}
func (rm *ReliabilityManager) CheckResources() ResourceLimits {
	if rm.resourceChecker == nil {
		return ResourceLimits{}
	}
	return rm.resourceChecker.Check()
}

// ValidateImage checks if a container image is properly pinned.
//
// # Description
//
// Validates that an image reference uses a SHA256 digest
// rather than a mutable tag like "latest".
//
// # Inputs
//
//   - image: Container image reference (e.g., "nginx:latest")
//
// # Outputs
//
//   - ImageValidation: Validation result with risk assessment
//   - error: Non-nil if parsing fails
//
// # Example
//
//	result, err := manager.ValidateImage("nginx:latest")
//	if result.Risk >= RiskHigh {
//	    log.Printf("WARNING: %s is not pinned", result.Image)
//	}
func (rm *ReliabilityManager) ValidateImage(image string) (ImageValidation, error) {
	if rm.imageValidator == nil {
		return ImageValidation{Image: image, IsPinned: true}, nil
	}
	return rm.imageValidator.ValidateImage(image)
}

// BackupBeforeChange creates a backup before modifying a file.
//
// # Description
//
// Creates a timestamped backup of a file before modification.
// Use for configuration files or other important data.
//
// # Inputs
//
//   - path: Absolute path to file to backup
//
// # Outputs
//
//   - string: Path to backup file (empty if backup disabled)
//   - error: Non-nil if backup fails
//
// # Example
//
//	backupPath, err := manager.BackupBeforeChange("/etc/aleutian/config.yaml")
//	if err != nil {
//	    log.Printf("Backup failed: %v", err)
//	}
//	// Now safe to modify original file
func (rm *ReliabilityManager) BackupBeforeChange(path string) (string, error) {
	if rm.backupManager == nil {
		return "", nil
	}
	return rm.backupManager.BackupBeforeOverwrite(path)
}

// ProposeRecovery asks user before expensive recovery actions.
//
// # Description
//
// Uses intentionality checking to confirm expensive or destructive
// actions before executing them. Respects non-interactive mode.
//
// # Inputs
//
//   - ctx: Context for cancellation
//   - issue: Description of the issue being fixed
//   - action: Proposed recovery action with details
//
// # Outputs
//
//   - error: ErrRecoveryDeclined if user declines, or action error
//
// # Example
//
//	err := manager.ProposeRecovery(ctx, "Model not found",
//	    RecoveryAction{
//	        Description: "Download llama2 (7GB)",
//	        Expensive:   true,
//	        Execute:     downloadModel,
//	    })
func (rm *ReliabilityManager) ProposeRecovery(ctx context.Context, issue string, action RecoveryAction) error {
	return ProposeRecovery(ctx, issue, action)
}

// ShouldSample returns whether to sample this operation.
//
// # Description
//
// Uses adaptive sampling based on system load. When latency
// increases, sampling rate automatically decreases.
//
// # Outputs
//
//   - bool: True if operation should be sampled/traced
//
// # Example
//
//	if manager.ShouldSample() {
//	    span := tracer.StartSpan("operation")
//	    defer span.End()
//	}
func (rm *ReliabilityManager) ShouldSample() bool {
	if rm.sampler == nil {
		return true
	}
	return rm.sampler.ShouldSample()
}

// RecordLatency records operation latency for adaptive sampling.
//
// # Description
//
// Records latency to adjust sampling rate based on system load.
// High latency causes sampling rate to decrease automatically.
//
// # Inputs
//
//   - latency: Duration of the operation
//
// # Example
//
//	start := time.Now()
//	doOperation()
//	manager.RecordLatency(time.Since(start))
func (rm *ReliabilityManager) RecordLatency(latency time.Duration) {
	if rm.sampler != nil {
		rm.sampler.RecordLatency(latency)
	}
}

// ValidateMetric checks if a metric name is valid.
//
// # Description
//
// Validates metric against schema to prevent cardinality explosion.
// Unknown metrics are rejected in strict mode.
//
// # Inputs
//
//   - name: Metric name to validate
//
// # Outputs
//
//   - error: Non-nil if metric is invalid in strict mode
//
// # Example
//
//	if err := manager.ValidateMetric("aleutian_custom_metric"); err != nil {
//	    log.Printf("Unknown metric: %v", err)
//	}
func (rm *ReliabilityManager) ValidateMetric(name string) error {
	if rm.metricsSchema == nil {
		return nil
	}
	return rm.metricsSchema.ValidateMetric(name)
}

// NormalizeLabel normalizes a label value to prevent cardinality explosion.
//
// # Description
//
// Maps unknown label values to "unknown" to prevent cardinality
// explosion in metrics. Known values are returned unchanged.
//
// # Inputs
//
//   - labelName: Name of the label (e.g., "error_type")
//   - value: Value to normalize
//
// # Outputs
//
//   - string: Normalized value (or "unknown" if not in enum)
//
// # Example
//
//	errorType := manager.NormalizeLabel("error_type", someErrorMessage)
//	// errorType is "unknown" if not in the defined enum
func (rm *ReliabilityManager) NormalizeLabel(labelName, value string) string {
	if rm.metricsSchema == nil {
		return value
	}
	return rm.metricsSchema.NormalizeLabel(labelName, value)
}

// TrackGoroutine tracks a goroutine for leak detection.
//
// # Description
//
// Registers a goroutine for lifecycle tracking. Returns a cleanup
// function that must be called when the goroutine exits.
//
// # Inputs
//
//   - name: Descriptive name for the goroutine
//
// # Outputs
//
//   - func(): Cleanup function to call on goroutine exit
//
// # Example
//
//	go func() {
//	    done := manager.TrackGoroutine("worker")
//	    defer done()
//	    // ... goroutine work ...
//	}()
func (rm *ReliabilityManager) TrackGoroutine(name string) func() {
	if rm.goroutineTracker == nil {
		return func() {}
	}
	return rm.goroutineTracker.Track(name)
}

// GetGoroutineStats returns goroutine statistics.
//
// # Description
//
// Returns statistics about tracked goroutines including current
// count, peak count, and any detected leaks.
//
// # Outputs
//
//   - GoroutineStats: Current goroutine statistics
//
// # Example
//
//	stats := manager.GetGoroutineStats()
//	log.Printf("Active: %d, Peak: %d", stats.Active, stats.Peak)
func (rm *ReliabilityManager) GetGoroutineStats() GoroutineStats {
	if rm.goroutineTracker == nil {
		return GoroutineStats{}
	}
	return rm.goroutineTracker.Stats()
}

// RegisterStateForAudit registers a piece of state for drift detection.
//
// # Description
//
// Registers state to be periodically checked for drift between
// cached and actual values. Useful for detecting split-brain scenarios.
//
// # Inputs
//
//   - name: Unique identifier for this state
//   - opts: Registration options including getter functions
//
// # Outputs
//
//   - error: Non-nil if registration fails
//
// # Example
//
//	manager.RegisterStateForAudit("container_status", StateRegistration{
//	    GetCached: func() (interface{}, error) { return cache.Get("status") },
//	    GetActual: func(ctx context.Context) (interface{}, error) {
//	        return docker.InspectContainer(ctx, "mycontainer")
//	    },
//	    Critical: true,
//	})
func (rm *ReliabilityManager) RegisterStateForAudit(name string, opts StateRegistration) error {
	if rm.stateAuditor == nil {
		return nil
	}
	return rm.stateAuditor.RegisterState(name, opts)
}

// GetDriftReport returns the latest state drift report.
//
// # Description
//
// Returns a summary of current drift status including which
// states are showing drift.
//
// # Outputs
//
//   - DriftReport: Current drift status
//
// # Example
//
//	report := manager.GetDriftReport()
//	if len(report.DriftingStates) > 0 {
//	    log.Printf("Drift detected in: %v", report.DriftingStates)
//	}
func (rm *ReliabilityManager) GetDriftReport() DriftReport {
	if rm.stateAuditor == nil {
		return DriftReport{}
	}
	return rm.stateAuditor.GetDriftReport()
}

// CreateSaga creates a new saga for multi-step operations.
//
// # Description
//
// Creates a saga executor for operations that need automatic
// rollback on failure. Each step can have a compensating action.
//
// # Outputs
//
//   - SagaExecutor: New saga executor
//
// # Example
//
//	saga := manager.CreateSaga()
//	saga.AddStep(SagaStep{
//	    Name: "create_container",
//	    Execute: createContainer,
//	    Compensate: removeContainer,
//	})
//	if err := saga.Execute(ctx); err != nil {
//	    // Compensation already ran
//	}
func (rm *ReliabilityManager) CreateSaga() SagaExecutor {
	return NewSaga(DefaultSagaConfig())
}

// HealthCheck performs a comprehensive reliability health check.
//
// # Description
//
// Checks all reliability subsystems and returns their status.
// Useful for monitoring and diagnostics.
//
// # Outputs
//
//   - ReliabilityHealthCheck: Current health status of all subsystems
//
// # Example
//
//	health := manager.HealthCheck()
//	if !health.ResourcesOK {
//	    for _, w := range health.ResourceWarnings {
//	        log.Printf("Resource warning: %s", w)
//	    }
//	}
func (rm *ReliabilityManager) HealthCheck() ReliabilityHealthCheck {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	check := ReliabilityHealthCheck{
		Timestamp: time.Now(),
	}

	// Check resources
	if rm.resourceChecker != nil {
		limits := rm.resourceChecker.Check()
		check.ResourcesOK = !limits.HasWarnings()
		check.ResourceWarnings = limits.Warnings
	} else {
		check.ResourcesOK = true
	}

	// Check goroutines
	if rm.goroutineTracker != nil {
		stats := rm.goroutineTracker.Stats()
		check.GoroutineCount = stats.Active
		check.GoroutinePeak = stats.Peak
	}

	// Check sampling
	if rm.sampler != nil {
		check.SamplingRate = rm.sampler.GetSamplingRate()
	} else {
		check.SamplingRate = 1.0
	}

	// Check state drift
	if rm.stateAuditor != nil {
		report := rm.stateAuditor.GetDriftReport()
		check.DriftingStates = report.DriftingStates
	}

	// Check process lock
	if rm.processLock != nil {
		check.ProcessLockHeld = rm.processLock.IsHeld()
	}

	return check
}

// Compile-time interface check
var _ ReliabilityOrchestrator = (*ReliabilityManager)(nil)

// Global singleton for convenience
var globalReliabilityManager *ReliabilityManager
var globalReliabilityOnce sync.Once

// GetReliabilityManager returns the global reliability manager.
//
// # Description
//
// Returns a lazily-initialized global reliability manager singleton.
// Use this for simple cases; create a custom manager for more control.
//
// Note: You must call Initialize() on the returned manager before use.
//
// # Outputs
//
//   - *ReliabilityManager: Global manager instance
//
// # Example
//
//	rm := GetReliabilityManager()
//	if err := rm.Initialize(ctx); err != nil {
//	    log.Fatal(err)
//	}
//
// # Limitations
//
//   - Returns same instance for entire process lifetime
//   - Cannot change configuration after first access
//
// # Assumptions
//
//   - Default configuration is acceptable
func GetReliabilityManager() *ReliabilityManager {
	globalReliabilityOnce.Do(func() {
		globalReliabilityManager = NewReliabilityManager(DefaultReliabilityConfig())
	})
	return globalReliabilityManager
}
