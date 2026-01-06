package main

/*
Package main contains reliability_graph.go which documents the dependency graph
and relationships between all reliability/security components.

# Component Dependency Graph (#26)

This file documents how all Phase 10 reliability components relate to each other
and integrate with the existing Aleutian codebase.

## Dependency Layers

	┌─────────────────────────────────────────────────────────────────────────┐
	│                         APPLICATION LAYER                               │
	│  cmd_stack.go │ cmd_chat.go │ cmd_data.go │ compose_executor.go         │
	└─────────────────────────────────────────────────────────────────────────┘
	                                   │
	                                   ▼
	┌─────────────────────────────────────────────────────────────────────────┐
	│                      RELIABILITY ORCHESTRATION                          │
	│                         reliability.go (this file)                      │
	│                                                                         │
	│  ReliabilityManager coordinates all subsystems:                         │
	│  - Startup sequence                                                     │
	│  - Periodic health checks                                               │
	│  - Graceful shutdown                                                    │
	└─────────────────────────────────────────────────────────────────────────┘
	                    │                     │                     │
	         ┌──────────┴──────────┬──────────┴──────────┬─────────┴───────────┐
	         ▼                     ▼                     ▼                     ▼
	┌─────────────────┐ ┌─────────────────┐ ┌─────────────────┐ ┌─────────────────┐
	│ PROCESS SAFETY  │ │  OBSERVABILITY  │ │   DATA SAFETY   │ │    SECURITY     │
	│                 │ │                 │ │                 │ │                 │
	│ process_lock.go │ │ adaptive_       │ │ backup.go       │ │ image_pinning.go│
	│ saga.go         │ │   sampler.go    │ │ retention_      │ │ state_auditor.go│
	│ goroutine_      │ │ metrics_        │ │   policy.go     │ │                 │
	│   tracker.go    │ │   schema.go     │ │                 │ │                 │
	│ ring_buffer.go  │ │                 │ │                 │ │                 │
	│ timeouts.go     │ │                 │ │                 │ │                 │
	└─────────────────┘ └─────────────────┘ └─────────────────┘ └─────────────────┘
	         │                     │                     │                     │
	         └─────────────────────┴─────────────────────┴─────────────────────┘
	                                        │
	                                        ▼
	┌─────────────────────────────────────────────────────────────────────────┐
	│                         EXISTING INFRASTRUCTURE                          │
	│                                                                         │
	│  system_checker.go │ circuit_breaker.go │ drift_detector.go             │
	│  compose_executor.go │ diagnostics_*.go                                  │
	└─────────────────────────────────────────────────────────────────────────┘

## Component Relationships

### Process Safety Components

	┌──────────────────┐
	│  process_lock.go │────────────────────────┐
	│  - CLI mutex     │                        │
	│  - PID tracking  │                        ▼
	└──────────────────┘            ┌─────────────────────┐
	         │                      │  Used by:           │
	         │                      │  - stack start      │
	         │                      │  - stack stop       │
	         │                      │  - stack destroy    │
	         ▼                      └─────────────────────┘
	┌──────────────────┐
	│     saga.go      │────────────────────────┐
	│  - Rollback      │                        │
	│  - Compensation  │                        ▼
	└──────────────────┘            ┌─────────────────────┐
	         │                      │  Used by:           │
	         │                      │  - Stack deployment │
	         │                      │  - Data migration   │
	         ▼                      └─────────────────────┘
	┌──────────────────┐
	│ goroutine_       │────────────────────────┐
	│   tracker.go     │                        │
	│  - Leak detect   │                        ▼
	│  - Lifecycle     │            ┌─────────────────────┐
	└──────────────────┘            │  Used by:           │
	         │                      │  - Health monitors  │
	         │                      │  - Background tasks │
	         ▼                      └─────────────────────┘
	┌──────────────────┐
	│  ring_buffer.go  │────────────────────────┐
	│  - Bounded queue │                        │
	│  - Backpressure  │                        ▼
	└──────────────────┘            ┌─────────────────────┐
	                                │  Used by:           │
	                                │  - Log streaming    │
	                                │  - Event queues     │
	                                └─────────────────────┘

### Observability Components

	┌──────────────────┐
	│ adaptive_        │────────────────────────┐
	│   sampler.go     │                        │
	│  - Load adaptive │                        ▼
	│  - Rate limiting │            ┌─────────────────────┐
	└──────────────────┘            │  Feeds:             │
	         │                      │  - metrics_schema   │
	         │                      │  - Logging          │
	         ▼                      └─────────────────────┘
	┌──────────────────┐
	│ metrics_         │────────────────────────┐
	│   schema.go      │                        │
	│  - Label enum    │                        ▼
	│  - Cardinality   │            ┌─────────────────────┐
	└──────────────────┘            │  Validates:         │
	                                │  - All metric calls │
	                                │  - Label values     │
	                                └─────────────────────┘

### Data Safety Components

	┌──────────────────┐
	│    backup.go     │────────────────────────┐
	│  - Auto backup   │                        │
	│  - Restore       │                        ▼
	└──────────────────┘            ┌─────────────────────┐
	         │                      │  Called before:     │
	         │                      │  - Config changes   │
	         │                      │  - Destructive ops  │
	         ▼                      └─────────────────────┘
	┌──────────────────┐
	│ retention_       │────────────────────────┐
	│   policy.go      │                        │
	│  - GDPR cleanup  │                        ▼
	│  - Auto purge    │            ┌─────────────────────┐
	└──────────────────┘            │  Runs on:           │
	                                │  - Scheduled cron   │
	                                │  - Manual trigger   │
	                                └─────────────────────┘

### Security Components

	┌──────────────────┐
	│ image_pinning.go │────────────────────────┐
	│  - Digest check  │                        │
	│  - Tag validate  │                        ▼
	└──────────────────┘            ┌─────────────────────┐
	         │                      │  Called by:         │
	         │                      │  - compose_executor │
	         │                      │  - stack start      │
	         ▼                      └─────────────────────┘
	┌──────────────────┐
	│ state_auditor.go │────────────────────────┐
	│  - Drift detect  │                        │
	│  - Cache valid   │                        ▼
	└──────────────────┘            ┌─────────────────────┐
	                                │  Monitors:          │
	                                │  - Cached config    │
	                                │  - Container state  │
	                                └─────────────────────┘

### UX Components

	┌──────────────────┐
	│   progress.go    │────────────────────────┐
	│  - Spinner       │                        │
	│  - Status        │                        ▼
	└──────────────────┘            ┌─────────────────────┐
	         │                      │  Used by:           │
	         │                      │  - All long ops     │
	         ▼                      └─────────────────────┘
	┌──────────────────┐
	│ intentionality.go│────────────────────────┐
	│  - User confirm  │                        │
	│  - Expensive ops │                        ▼
	└──────────────────┘            ┌─────────────────────┐
	                                │  Guards:            │
	                                │  - Model downloads  │
	                                │  - Data deletion    │
	                                └─────────────────────┘

## Integration Points Summary

### cmd_stack.go Integration

	runStart() should:
	  1. Acquire ProcessLock
	  2. Check ResourceLimits (FD limits)
	  3. Validate images with ImagePinValidator
	  4. Start with Progress spinner
	  5. Use Saga for rollback on failure
	  6. Track goroutines with GoroutineTracker
	  7. Release ProcessLock on exit

	runStop() should:
	  1. Acquire ProcessLock
	  2. Use intentionality for confirmation
	  3. Graceful shutdown with Progress
	  4. Release ProcessLock

	runDestroy() should:
	  1. Acquire ProcessLock
	  2. Use intentionality (destructive action)
	  3. Backup before destruction
	  4. Release ProcessLock

### compose_executor.go Integration

	ValidateAndStart() should:
	  1. Validate compose images with ImagePinValidator
	  2. Check resource limits
	  3. Use adaptive sampling for health checks
	  4. Validate metrics with MetricsSchema
	  5. Audit state periodically

### Background Services

	Periodic tasks should:
	  1. Run retention policy enforcement
	  2. Audit cached state for drift
	  3. Track goroutine health
	  4. Adjust sampling rates

## Error Flow

	User Action
	    │
	    ▼
	┌─────────────────┐     ┌─────────────────┐
	│ Intentionality  │────▶│ User Confirms   │
	│    Check        │     │   (if needed)   │
	└─────────────────┘     └─────────────────┘
	    │                           │
	    ▼                           ▼
	┌─────────────────┐     ┌─────────────────┐
	│  Process Lock   │────▶│ Another CLI     │
	│    Acquire      │     │   Running?      │
	└─────────────────┘     └─────────────────┘
	    │                           │
	    ▼                           ▼
	┌─────────────────┐     ┌─────────────────┐
	│ Resource Check  │────▶│ Enough FDs?     │
	│                 │     │ Enough disk?    │
	└─────────────────┘     └─────────────────┘
	    │                           │
	    ▼                           ▼
	┌─────────────────┐     ┌─────────────────┐
	│  Image Check    │────▶│ Pinned? Valid?  │
	│                 │     │                 │
	└─────────────────┘     └─────────────────┘
	    │                           │
	    ▼                           ▼
	┌─────────────────┐     ┌─────────────────┐
	│  Saga Execute   │────▶│ Steps with      │
	│                 │     │   rollback      │
	└─────────────────┘     └─────────────────┘
	    │                           │
	    ▼                           ▼
	┌─────────────────┐     ┌─────────────────┐
	│  On Failure     │────▶│ Saga compensate │
	│                 │     │ Release lock    │
	└─────────────────┘     └─────────────────┘
*/

// ComponentCategory categorizes reliability components.
type ComponentCategory string

const (
	CategoryProcessSafety ComponentCategory = "process_safety"
	CategoryObservability ComponentCategory = "observability"
	CategoryDataSafety    ComponentCategory = "data_safety"
	CategorySecurity      ComponentCategory = "security"
	CategoryUX            ComponentCategory = "ux"
)

// ComponentInfo describes a reliability component.
type ComponentInfo struct {
	Name        string
	File        string
	Category    ComponentCategory
	Description string
	DependsOn   []string
	UsedBy      []string
}

// GetReliabilityComponents returns information about all reliability components.
func GetReliabilityComponents() []ComponentInfo {
	return []ComponentInfo{
		// Process Safety
		{
			Name:        "ProcessLock",
			File:        "process_lock.go",
			Category:    CategoryProcessSafety,
			Description: "Prevents concurrent CLI executions using PID file locking",
			DependsOn:   []string{},
			UsedBy:      []string{"cmd_stack.go:runStart", "cmd_stack.go:runStop", "cmd_stack.go:runDestroy"},
		},
		{
			Name:        "Saga",
			File:        "saga.go",
			Category:    CategoryProcessSafety,
			Description: "Multi-step operations with automatic rollback on failure",
			DependsOn:   []string{},
			UsedBy:      []string{"compose_executor.go", "cmd_stack.go"},
		},
		{
			Name:        "GoroutineTracker",
			File:        "goroutine_tracker.go",
			Category:    CategoryProcessSafety,
			Description: "Tracks goroutine lifecycle to detect leaks",
			DependsOn:   []string{},
			UsedBy:      []string{"health_monitors", "background_tasks"},
		},
		{
			Name:        "RingBuffer",
			File:        "ring_buffer.go",
			Category:    CategoryProcessSafety,
			Description: "Bounded buffer with backpressure for event queues",
			DependsOn:   []string{},
			UsedBy:      []string{"log_streaming", "event_queues"},
		},

		// Observability
		{
			Name:        "AdaptiveSampler",
			File:        "adaptive_sampler.go",
			Category:    CategoryObservability,
			Description: "Load-adaptive sampling to prevent observer effect",
			DependsOn:   []string{},
			UsedBy:      []string{"health_checks", "metrics_collection"},
		},
		{
			Name:        "MetricsSchema",
			File:        "metrics_schema.go",
			Category:    CategoryObservability,
			Description: "Validates metrics and prevents cardinality explosion",
			DependsOn:   []string{},
			UsedBy:      []string{"all_metric_calls"},
		},

		// Data Safety
		{
			Name:        "BackupManager",
			File:        "backup.go",
			Category:    CategoryDataSafety,
			Description: "Creates backups before destructive operations",
			DependsOn:   []string{},
			UsedBy:      []string{"config_changes", "destructive_commands"},
		},
		{
			Name:        "RetentionEnforcer",
			File:        "retention_policy.go",
			Category:    CategoryDataSafety,
			Description: "Enforces data retention policies for GDPR/CCPA compliance",
			DependsOn:   []string{},
			UsedBy:      []string{"scheduled_cron", "manual_cleanup"},
		},

		// Security
		{
			Name:        "ImagePinValidator",
			File:        "image_pinning.go",
			Category:    CategorySecurity,
			Description: "Validates container images are pinned to digests",
			DependsOn:   []string{},
			UsedBy:      []string{"compose_executor.go", "stack_start"},
		},
		{
			Name:        "StateAuditor",
			File:        "state_auditor.go",
			Category:    CategorySecurity,
			Description: "Detects drift between cached and actual state",
			DependsOn:   []string{},
			UsedBy:      []string{"periodic_checks", "health_monitoring"},
		},

		// UX
		{
			Name:        "ProgressIndicator",
			File:        "progress.go",
			Category:    CategoryUX,
			Description: "Shows progress spinner for long operations",
			DependsOn:   []string{},
			UsedBy:      []string{"all_long_operations"},
		},
		{
			Name:        "RecoveryProposer",
			File:        "intentionality.go",
			Category:    CategoryUX,
			Description: "Asks user confirmation for expensive/destructive actions",
			DependsOn:   []string{},
			UsedBy:      []string{"model_downloads", "data_deletion", "recovery_actions"},
		},

		// Resource Management
		{
			Name:        "ResourceLimitsChecker",
			File:        "resource_limits.go",
			Category:    CategoryProcessSafety,
			Description: "Checks system resource limits (file descriptors, etc.)",
			DependsOn:   []string{},
			UsedBy:      []string{"startup_checks", "health_monitoring"},
		},
	}
}

// GetDependencyGraph returns the dependency graph as an adjacency list.
func GetDependencyGraph() map[string][]string {
	return map[string][]string{
		// cmd_stack.go dependencies
		"cmd_stack.runStart": {
			"ProcessLock",
			"ResourceLimitsChecker",
			"ImagePinValidator",
			"ProgressIndicator",
			"Saga",
			"GoroutineTracker",
		},
		"cmd_stack.runStop": {
			"ProcessLock",
			"RecoveryProposer",
			"ProgressIndicator",
		},
		"cmd_stack.runDestroy": {
			"ProcessLock",
			"RecoveryProposer",
			"BackupManager",
			"ProgressIndicator",
		},

		// compose_executor.go dependencies
		"compose_executor.ValidateAndStart": {
			"ImagePinValidator",
			"ResourceLimitsChecker",
			"AdaptiveSampler",
			"MetricsSchema",
			"StateAuditor",
		},

		// Background services
		"background.PeriodicTasks": {
			"RetentionEnforcer",
			"StateAuditor",
			"GoroutineTracker",
			"AdaptiveSampler",
		},
	}
}
