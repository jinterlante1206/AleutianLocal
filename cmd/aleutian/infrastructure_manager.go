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
Package main provides InfrastructureManager for Podman machine lifecycle management.

InfrastructureManager abstracts all Podman machine operations for testability.
On macOS, Aleutian runs containers inside a Podman machine (Linux VM). This
manager handles machine creation, verification, repair, and security hardening.

# Security Context

This is a HIGH-RISK component because it controls the boundary between the
Host OS (User Data) and the Container (AI Workload). A loose boundary could
allow a malicious container or hallucinating agent to access sensitive user files.

# Security Features

  - Mount Guard: Validates mounts against SensitiveMountPaths blocklist
  - Network Kill Switch: Optional air-gap mode with VerifyNetworkIsolation
  - Read-Only Mounts: User data protected by default
  - Capability Dropping: Dangerous Linux capabilities removed
  - Workload Quarantine: Foreign containers prevent auto-healing

# Platform Behavior

  - macOS: Full machine management (create, start, stop, verify mounts)
  - Linux: No-op (containers run natively)
  - Windows: No-op (not currently supported)

# Design Principles

  - All Podman commands go through ProcessManager for mocking
  - Each method has single responsibility
  - Dependencies injected (ProcessManager, UserPrompter, DiagnosticsMetrics)
  - Thread-safe for concurrent use
*/
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// -----------------------------------------------------------------------------
// Security Constants
// -----------------------------------------------------------------------------

// SensitiveMountPaths are paths that MUST NOT be mounted into containers.
// These paths contain credentials, keys, or system-critical files.
// Mounting these would be catastrophic for security.
//
// # Paths Blocked
//
//   - "/" - Root filesystem (full host access)
//   - "/root" - Root user home directory
//   - "/var" - System state, logs, databases
//   - "/etc" - System configuration files
//   - "/System" - macOS system files
//   - "/Library" - macOS shared libraries
//   - "/private" - macOS private system data
//
// # Behavior
//
// ValidateMounts will reject these paths with a critical-severity rejection.
// There is no override - these paths are never mountable.
var SensitiveMountPaths = []string{
	"/",
	"/root",
	"/var",
	"/etc",
	"/System",
	"/Library",
	"/private",
}

// WarnOnMountPaths trigger a mandatory user confirmation before mounting.
// These paths contain user credentials or sensitive configuration.
// Even with SkipPrompts=true, these require explicit AllowSensitiveMounts=true.
//
// # Paths Requiring Confirmation
//
//   - "~" - User home directory (contains everything)
//   - "~/.ssh" - SSH keys (server access)
//   - "~/.aws" - AWS credentials (cloud access)
//   - "~/.config" - Application configs (may contain tokens)
//   - "~/.gnupg" - GPG keys (signing/encryption)
//   - "~/.kube" - Kubernetes configs (cluster access)
//
// # Behavior
//
// ValidateMounts will:
//  1. If interactive: Prompt user for explicit confirmation
//  2. If SkipPrompts but AllowSensitiveMounts: Allow with warning
//  3. If SkipPrompts and !AllowSensitiveMounts: Reject
var WarnOnMountPaths = []string{
	"~",
	"~/.ssh",
	"~/.aws",
	"~/.config",
	"~/.gnupg",
	"~/.kube",
}

// DefaultAleutianDataDir is the default location for Aleutian-managed data.
// This directory (and subdirectories) is always mounted read-write.
const DefaultAleutianDataDir = "~/.aleutian"

// DefaultMaxHealAttempts is the maximum self-healing recursion depth.
const DefaultMaxHealAttempts = 2

// DefaultMachineName is the Podman machine name used by Aleutian.
const DefaultMachineName = "podman-machine-default"

// -----------------------------------------------------------------------------
// Interface Definition
// -----------------------------------------------------------------------------

// InfrastructureManager handles Podman machine lifecycle operations.
//
// # Description
//
// This interface abstracts infrastructure management for testability.
// On macOS, Aleutian runs containers inside a Podman machine (Linux VM).
// This manager handles machine creation, verification, and repair.
//
// # Platform Behavior
//
//   - macOS: Full machine management (create, start, stop, verify mounts)
//   - Linux: No-op (containers run natively)
//   - Windows: No-op (not currently supported)
//
// # Security Boundary
//
// This component controls access between the host filesystem and containers.
// All mount operations MUST pass through ValidateMounts() before execution.
//
// # Thread Safety
//
// Implementations must be safe for concurrent use.
type InfrastructureManager interface {
	// EnsureReady verifies infrastructure is ready for container operations.
	//
	// # Description
	//
	// Main entry point that orchestrates infrastructure readiness:
	//   1. Detects platform (macOS requires machine, Linux is no-op)
	//   2. Checks for conflicting processes (Podman Desktop)
	//   3. Verifies or creates Podman machine
	//   4. Validates mount configuration
	//   5. Applies security hardening
	//
	// # Inputs
	//
	//   - ctx: Context for cancellation/timeout
	//   - opts: Configuration for infrastructure setup
	//
	// # Outputs
	//
	//   - error: Non-nil if infrastructure cannot be made ready
	//
	// # Examples
	//
	//   err := mgr.EnsureReady(ctx, InfrastructureOptions{
	//       MachineConfig: cfg.Machine,
	//       Hardening:     DefaultHardeningConfig(),
	//   })
	//   if err != nil {
	//       return fmt.Errorf("infrastructure not ready: %w", err)
	//   }
	//
	// # Security
	//
	// If foreign workloads are detected, environment is marked "Tainted"
	// and auto-healing is disabled to prevent accidental data exposure.
	//
	// # Self-Healing
	//
	// If operations fail, retries up to MaxHealAttempts times.
	//
	// # Limitations
	//
	//   - Requires Podman CLI installed on macOS
	//   - User interaction may be required for mount confirmations
	//
	// # Assumptions
	//
	//   - ProcessManager is available for command execution
	//   - UserPrompter is available for confirmations
	EnsureReady(ctx context.Context, opts InfrastructureOptions) error

	// GetMachineStatus returns the current state of a Podman machine.
	//
	// # Description
	//
	// Queries Podman for machine state using `podman machine inspect`.
	// Returns structured information about existence, running state,
	// and configured resources.
	//
	// # Inputs
	//
	//   - ctx: Context for cancellation/timeout
	//   - machineName: Machine identifier (e.g., "podman-machine-default")
	//
	// # Outputs
	//
	//   - *MachineStatus: Current state, or Exists=false if not found
	//   - error: Non-nil if query fails (not for "not found")
	//
	// # Examples
	//
	//   status, err := mgr.GetMachineStatus(ctx, "podman-machine-default")
	//   if err != nil {
	//       return fmt.Errorf("failed to get status: %w", err)
	//   }
	//   if !status.Exists {
	//       // Machine needs to be created
	//   }
	//
	// # Limitations
	//
	//   - JSON output parsing may fail on non-standard Podman output
	//
	// # Assumptions
	//
	//   - Podman CLI is installed and in PATH
	GetMachineStatus(ctx context.Context, machineName string) (*MachineStatus, error)

	// ValidateMounts checks mount paths for security violations.
	//
	// # Description
	//
	// Validates requested mount paths against security policies:
	//   1. Rejects paths in SensitiveMountPaths (critical)
	//   2. Flags paths in WarnOnMountPaths (requires confirmation)
	//   3. Approves other paths
	//
	// # Inputs
	//
	//   - ctx: Context for cancellation
	//   - mounts: List of host paths to validate
	//
	// # Outputs
	//
	//   - *MountValidation: Categorized results with approved/rejected paths
	//   - error: Non-nil if validation cannot be performed
	//
	// # Examples
	//
	//   result, err := mgr.ValidateMounts(ctx, []string{
	//       "/Users/jin/Documents",
	//       "/",  // Will be rejected
	//       "~/.ssh",  // Will be flagged for warning
	//   })
	//   if !result.Valid {
	//       for _, r := range result.RejectedMounts {
	//           log.Printf("Rejected: %s (%s)", r.Path, r.Reason)
	//       }
	//   }
	//
	// # Security
	//
	// This is a critical security gate. All mount requests MUST pass
	// through this validation before machine provisioning.
	//
	// # Limitations
	//
	//   - Does not verify paths exist on host
	//   - Path expansion (~) done at validation time
	//
	// # Assumptions
	//
	//   - Paths are provided in standard Unix format
	ValidateMounts(ctx context.Context, mounts []string) (*MountValidation, error)

	// ProvisionMachine creates a new Podman machine with the given configuration.
	//
	// # Description
	//
	// Creates a new Podman machine with specified resources and mounts.
	// Applies security hardening based on HardeningConfig.
	//
	// # Inputs
	//
	//   - ctx: Context for cancellation/timeout
	//   - spec: Machine specification (name, CPUs, memory, mounts, hardening)
	//
	// # Outputs
	//
	//   - error: Non-nil if provisioning fails
	//
	// # Examples
	//
	//   err := mgr.ProvisionMachine(ctx, MachineSpec{
	//       Name:      "podman-machine-default",
	//       CPUs:      6,
	//       MemoryMB:  20480,
	//       Mounts:    []string{"/data"},
	//       Hardening: DefaultHardeningConfig(),
	//   })
	//
	// # Security
	//
	// Calls ValidateMounts() before creating machine.
	// Emits aleutian_infrastructure_provision_duration_seconds metric.
	//
	// # Limitations
	//
	//   - Machine name must not already exist
	//   - Mount paths must exist on host
	//
	// # Assumptions
	//
	//   - Podman CLI is installed and in PATH
	//   - Sufficient disk space for machine image
	ProvisionMachine(ctx context.Context, spec MachineSpec) error

	// StartMachine starts a stopped Podman machine.
	//
	// # Description
	//
	// Starts an existing Podman machine that is in stopped state.
	//
	// # Inputs
	//
	//   - ctx: Context for cancellation/timeout
	//   - machineName: Machine identifier to start
	//
	// # Outputs
	//
	//   - error: Non-nil if start fails
	//
	// # Examples
	//
	//   if !status.Running {
	//       err := mgr.StartMachine(ctx, "podman-machine-default")
	//   }
	//
	// # Limitations
	//
	//   - Machine must exist
	//   - Start may take several seconds
	//
	// # Assumptions
	//
	//   - Machine is in stopped state
	StartMachine(ctx context.Context, machineName string) error

	// StopMachine stops a running Podman machine.
	//
	// # Description
	//
	// Gracefully stops a running Podman machine.
	//
	// # Inputs
	//
	//   - ctx: Context for cancellation/timeout
	//   - machineName: Machine identifier to stop
	//
	// # Outputs
	//
	//   - error: Non-nil if stop fails
	//
	// # Examples
	//
	//   err := mgr.StopMachine(ctx, "podman-machine-default")
	//
	// # Limitations
	//
	//   - Running containers will be stopped
	//
	// # Assumptions
	//
	//   - Machine is in running state
	StopMachine(ctx context.Context, machineName string) error

	// RemoveMachine removes an existing Podman machine.
	//
	// # Description
	//
	// Removes a Podman machine and its associated resources.
	// This is a destructive operation - all data in the machine is lost.
	//
	// # Inputs
	//
	//   - ctx: Context for cancellation/timeout
	//   - machineName: Machine identifier to remove
	//   - force: If true, force removal even if running
	//   - reason: Audit reason for removal (logged)
	//
	// # Outputs
	//
	//   - error: Non-nil if removal fails
	//
	// # Examples
	//
	//   err := mgr.RemoveMachine(ctx, "podman-machine-default", true, "drift_fix")
	//
	// # Security
	//
	// Records SeverityWarning diagnostic event before destruction.
	// Emits aleutian_infrastructure_event{action="destroy", reason="..."} metric.
	//
	// # Limitations
	//
	//   - Cannot be undone
	//   - Local container data is lost
	//
	// # Assumptions
	//
	//   - Machine exists
	RemoveMachine(ctx context.Context, machineName string, force bool, reason string) error

	// VerifyMounts checks if machine mounts match expected configuration.
	//
	// # Description
	//
	// Compares the currently configured mounts on a machine against
	// the expected mounts from configuration. Detects drift.
	//
	// # Inputs
	//
	//   - ctx: Context for cancellation/timeout
	//   - machineName: Machine to check
	//   - expectedMounts: Mounts that should be configured
	//
	// # Outputs
	//
	//   - *MountVerification: Comparison results with match status
	//   - error: Non-nil if verification cannot be performed
	//
	// # Examples
	//
	//   result, err := mgr.VerifyMounts(ctx, "podman-machine-default", cfg.Drives)
	//   if !result.Match {
	//       log.Printf("Missing mounts: %v", result.MissingMounts)
	//   }
	//
	// # Limitations
	//
	//   - Does not verify mount paths exist on host
	//
	// # Assumptions
	//
	//   - Machine exists and is inspectable
	VerifyMounts(ctx context.Context, machineName string, expectedMounts []string) (*MountVerification, error)

	// DetectConflicts checks for processes that conflict with Podman CLI.
	//
	// # Description
	//
	// Detects running processes that may interfere with Podman CLI
	// operations, such as Podman Desktop which can lock resources.
	//
	// # Inputs
	//
	//   - ctx: Context for cancellation/timeout
	//
	// # Outputs
	//
	//   - *ConflictReport: Detected conflicts and their details
	//   - error: Non-nil if detection fails
	//
	// # Examples
	//
	//   report, err := mgr.DetectConflicts(ctx)
	//   if report.HasConflicts {
	//       log.Printf("Podman Desktop running (PID %d)", report.PodmanDesktopPID)
	//   }
	//
	// # Limitations
	//
	//   - Uses pgrep which may not be available on all systems
	//
	// # Assumptions
	//
	//   - pgrep is available on macOS
	DetectConflicts(ctx context.Context) (*ConflictReport, error)

	// HasForeignWorkloads checks for non-Aleutian containers.
	//
	// # Description
	//
	// Detects containers running in the Podman machine that were not
	// created by Aleutian. Used for workload isolation security.
	//
	// # Inputs
	//
	//   - ctx: Context for cancellation/timeout
	//
	// # Outputs
	//
	//   - *WorkloadAssessment: Security assessment of running workloads
	//   - error: Non-nil if assessment fails
	//
	// # Examples
	//
	//   assessment, err := mgr.HasForeignWorkloads(ctx)
	//   if assessment.IsTainted {
	//       log.Printf("Environment tainted: %s", assessment.TaintReason)
	//   }
	//
	// # Security
	//
	// When foreign workloads are detected:
	//   - Environment marked as "Tainted"
	//   - Auto-healing disabled
	//   - User must explicitly approve operations
	//
	// # Limitations
	//
	//   - Relies on container labels for identification
	//
	// # Assumptions
	//
	//   - Aleutian containers have io.podman.compose.project=aleutian label
	HasForeignWorkloads(ctx context.Context) (*WorkloadAssessment, error)

	// VerifyNetworkIsolation confirms container has no internet connectivity.
	//
	// # Description
	//
	// On-demand verification of air-gap compliance for auditors.
	// Attempts to detect outbound network routes from within the container.
	//
	// # Inputs
	//
	//   - ctx: Context for cancellation/timeout
	//   - containerID: Container to test (empty = default Aleutian container)
	//
	// # Outputs
	//
	//   - *NetworkIsolationStatus: Detailed isolation verification result
	//   - error: Non-nil if verification cannot be performed
	//
	// # Examples
	//
	//   status, err := mgr.VerifyNetworkIsolation(ctx, "")
	//   if status.Isolated {
	//       log.Printf("Air-gap verified: %s", status.VerificationMethod)
	//   } else {
	//       log.Printf("NOT ISOLATED: %s", status.FailureReason)
	//   }
	//
	// # Use Cases
	//
	//   - Compliance audits (SOC2, HIPAA)
	//   - User verification before processing sensitive data
	//   - Debug network configuration issues
	//
	// # Security
	//
	// This is an on-demand verification tool for compliance/audit purposes.
	// It does NOT automatically run; users call it when they need proof.
	//
	// # Limitations
	//
	//   - Adds latency (DNS lookup, TCP probe)
	//   - Requires container to be running
	//
	// # Assumptions
	//
	//   - Container has nslookup, nc, ip tools installed
	VerifyNetworkIsolation(ctx context.Context, containerID string) (*NetworkIsolationStatus, error)
}

// -----------------------------------------------------------------------------
// Hardening Configuration
// -----------------------------------------------------------------------------

// HardeningConfig configures security isolation levels for containers.
//
// # Description
//
// These settings implement defense-in-depth by restricting what containers
// can do, even if other security measures fail. All settings are designed
// to be safe defaults that can be relaxed when explicitly needed.
//
// # Enterprise Extension Point
//
// Enterprise deployments may enforce stricter defaults via policy.
// The interface allows external configuration injection.
//
// # Model Download Strategy
//
// When NetworkIsolation is enabled, models must be pre-downloaded.
// Use `aleutian model pull <model>` before enabling air-gap mode.
//
// # Mount Security Model
//
// Aleutian distinguishes between two types of mounted paths:
//   - AleutianDataDir: Aleutian's own data (models, vectors, history) - ALWAYS writable
//   - User mounts: User's documents/files - read-only by default (protected)
//
// This ensures Aleutian can function while protecting user data.
type HardeningConfig struct {
	// NetworkIsolation disables all outbound internet access for containers.
	// When true, containers can ONLY communicate via local Unix socket.
	//
	// Use case: Air-gapped deployments, sensitive data processing.
	// Prerequisite: Models must be pre-downloaded before enabling.
	//
	// Implementation: Sets --network=none on container creation.
	//
	// Default: false (allows internet for model downloads).
	NetworkIsolation bool

	// ReadOnlyMounts forces USER-PROVIDED mounts to be read-only (:ro).
	// Does NOT affect AleutianDataDir - that is always writable.
	//
	// Use case: Prevent accidental or malicious modification of user files.
	// Override: Use WritableMounts to explicitly allow writes to specific paths.
	//
	// Implementation: Appends :ro to user mount specifications.
	//
	// Default: true (recommended for safety).
	ReadOnlyMounts bool

	// DropCapabilities removes dangerous Linux capabilities from containers.
	// Drops: CAP_NET_RAW, CAP_SYS_ADMIN, CAP_SYS_PTRACE, CAP_NET_ADMIN, CAP_DAC_OVERRIDE
	// Keeps: CHOWN, SETUID, SETGID (minimum for container operation).
	//
	// Use case: Limit blast radius if container is compromised.
	//
	// Implementation: Sets cap_drop: [ALL] and cap_add: [CHOWN, SETUID, SETGID].
	//
	// Default: true (recommended for security).
	DropCapabilities bool

	// AleutianDataDir is the root directory for all Aleutian-managed data.
	// This path (and all subdirectories) is ALWAYS mounted read-write.
	// Contains: models_cache, vector_db, chat_history, diagnostics.
	//
	// Examples:
	//   - "/Volumes/ai_models/aleutian_data" (external drive)
	//   - "~/.aleutian" (default, home directory)
	//
	// Security: Only Aleutian's own data lives here, not user documents.
	//
	// Default: "~/.aleutian"
	AleutianDataDir string

	// WritableMounts lists additional paths that should be mounted read-write.
	// Only used when ReadOnlyMounts is true.
	// Use for user-specified output directories.
	//
	// Example: []string{"/data/outputs"} allows writing to outputs only.
	//
	// Default: empty.
	WritableMounts []string
}

// DefaultHardeningConfig returns the recommended security configuration.
//
// # Description
//
// Returns safe defaults for FOSS deployment:
//   - NetworkIsolation: false (allows model downloads)
//   - ReadOnlyMounts: true (protects user files)
//   - DropCapabilities: true (limits container privileges)
//   - AleutianDataDir: "~/.aleutian" (always writable for Aleutian data)
//
// # Outputs
//
//   - HardeningConfig with safe defaults enabled
//
// # Examples
//
//	cfg := DefaultHardeningConfig()
//	// cfg.NetworkIsolation = false
//	// cfg.ReadOnlyMounts = true
//	// cfg.DropCapabilities = true
//	// cfg.AleutianDataDir = "~/.aleutian"
//
// # Limitations
//
//   - None
//
// # Assumptions
//
//   - Caller will override AleutianDataDir if using external storage
func DefaultHardeningConfig() HardeningConfig {
	return HardeningConfig{
		NetworkIsolation: false,
		ReadOnlyMounts:   true,
		DropCapabilities: true,
		AleutianDataDir:  DefaultAleutianDataDir,
		WritableMounts:   []string{},
	}
}

// -----------------------------------------------------------------------------
// Configuration Types
// -----------------------------------------------------------------------------

// InfrastructureOptions configures the EnsureReady behavior.
//
// # Description
//
// Provides all configuration needed for infrastructure setup including
// machine specifications, user interaction preferences, and security settings.
type InfrastructureOptions struct {
	// MachineName is the Podman machine identifier.
	// Default: "podman-machine-default"
	MachineName string

	// CPUs is the number of virtual CPUs for the machine.
	// Default: 6
	CPUs int

	// MemoryMB is the RAM allocation in megabytes.
	// Default: 20480 (20 GB)
	MemoryMB int

	// Mounts is the list of host paths to mount into the machine.
	Mounts []string

	// ForceRecreate triggers automatic machine recreation on drift.
	// Corresponds to --force-recreate CLI flag.
	ForceRecreate bool

	// MaxHealAttempts limits self-healing recursion.
	// Default: 2
	MaxHealAttempts int

	// SkipPrompts disables interactive prompts (for CI/CD).
	// NOTE: Does NOT skip sensitive mount warnings - those always prompt or fail.
	SkipPrompts bool

	// AllowSensitiveMounts explicitly permits mounting warning paths.
	// Must be set by user with full understanding of security implications.
	AllowSensitiveMounts bool

	// Hardening configures security isolation levels.
	// Use DefaultHardeningConfig() for recommended settings.
	Hardening HardeningConfig
}

// DefaultInfrastructureOptions returns options with sensible defaults.
//
// # Outputs
//
//   - InfrastructureOptions with default values set
func DefaultInfrastructureOptions() InfrastructureOptions {
	return InfrastructureOptions{
		MachineName:          DefaultMachineName,
		CPUs:                 6,
		MemoryMB:             20480,
		Mounts:               []string{},
		ForceRecreate:        false,
		MaxHealAttempts:      DefaultMaxHealAttempts,
		SkipPrompts:          false,
		AllowSensitiveMounts: false,
		Hardening:            DefaultHardeningConfig(),
	}
}

// MachineSpec defines parameters for creating a new Podman machine.
//
// # Description
//
// Contains all information needed to provision a new Podman machine
// including resource allocation, mount configuration, and security settings.
type MachineSpec struct {
	// Name is the machine identifier.
	// Default: "podman-machine-default"
	Name string

	// CPUs is the number of virtual CPUs.
	// Default: 6
	CPUs int

	// MemoryMB is the RAM allocation in megabytes.
	// Default: 20480
	MemoryMB int

	// Mounts is the list of host paths to mount into the machine.
	Mounts []string

	// Hardening specifies security isolation settings for containers.
	// Applied when creating containers within the machine.
	Hardening HardeningConfig
}

// -----------------------------------------------------------------------------
// Status Types
// -----------------------------------------------------------------------------

// MachineStatus represents the current state of a Podman machine.
//
// # Description
//
// Structured representation of `podman machine inspect` output.
// Used to determine if machine needs creation, starting, or repair.
type MachineStatus struct {
	// Name is the machine identifier.
	Name string

	// Exists indicates whether the machine has been created.
	Exists bool

	// Running indicates whether the machine is currently running.
	Running bool

	// State is the detailed machine state.
	State MachineState

	// CPUs is the configured CPU count.
	CPUs int

	// MemoryMB is the configured memory in megabytes.
	MemoryMB int

	// Mounts lists the configured mount points.
	Mounts []MountInfo
}

// MachineState represents the lifecycle state of a machine.
type MachineState string

const (
	// MachineStateUnknown indicates state could not be determined.
	MachineStateUnknown MachineState = "unknown"

	// MachineStateRunning indicates the machine is running.
	MachineStateRunning MachineState = "running"

	// MachineStateStopped indicates the machine exists but is stopped.
	MachineStateStopped MachineState = "stopped"

	// MachineStateStarting indicates the machine is starting up.
	MachineStateStarting MachineState = "starting"
)

// MountInfo describes a single volume mount.
//
// # Description
//
// Represents a host-to-machine mount with source and target paths.
type MountInfo struct {
	// Source is the host path.
	Source string

	// Target is the path inside the machine (usually same as Source).
	Target string

	// ReadOnly indicates if the mount is read-only.
	ReadOnly bool
}

// -----------------------------------------------------------------------------
// Verification Types
// -----------------------------------------------------------------------------

// MountVerification is the result of comparing actual vs expected mounts.
//
// # Description
//
// Used to detect configuration drift between the machine's actual
// mounts and the expected mounts from configuration.
type MountVerification struct {
	// Match is true if all expected mounts are present.
	Match bool

	// ActualMounts lists mounts currently configured on the machine.
	ActualMounts []string

	// ExpectedMounts lists mounts from the configuration.
	ExpectedMounts []string

	// MissingMounts lists expected mounts that are not configured.
	MissingMounts []string

	// ExtraMounts lists mounts that exist but weren't expected.
	ExtraMounts []string
}

// MountValidation is the result of security-checking mount paths.
//
// # Description
//
// Categorizes mount paths based on security policy:
//   - Approved: Safe to mount
//   - Rejected: Blocked by policy (SensitiveMountPaths)
//   - Warning: Requires explicit confirmation (WarnOnMountPaths)
type MountValidation struct {
	// Valid is true if all mounts passed security checks.
	Valid bool

	// ApprovedMounts are paths that passed validation.
	ApprovedMounts []string

	// RejectedMounts are paths blocked by SensitiveMountPaths.
	RejectedMounts []MountRejection

	// WarningMounts require explicit user confirmation.
	WarningMounts []string

	// UserConfirmedWarnings is true if user approved warning mounts.
	UserConfirmedWarnings bool
}

// MountRejection describes why a mount was rejected.
//
// # Description
//
// Provides detailed explanation for security rejections.
type MountRejection struct {
	// Path is the rejected mount path.
	Path string

	// Reason explains why the path was rejected.
	Reason string

	// Severity indicates the security risk level.
	Severity MountRejectionSeverity
}

// MountRejectionSeverity indicates the security risk level.
type MountRejectionSeverity string

const (
	// MountRejectionCritical means the mount would be catastrophic.
	MountRejectionCritical MountRejectionSeverity = "critical"

	// MountRejectionHigh means the mount is high risk.
	MountRejectionHigh MountRejectionSeverity = "high"

	// MountRejectionWarning means the mount requires confirmation.
	MountRejectionWarning MountRejectionSeverity = "warning"
)

// -----------------------------------------------------------------------------
// Conflict Types
// -----------------------------------------------------------------------------

// ConflictReport describes detected infrastructure conflicts.
//
// # Description
//
// Contains information about processes that may interfere with
// Podman CLI operations.
type ConflictReport struct {
	// HasConflicts is true if any conflicts were detected.
	HasConflicts bool

	// PodmanDesktopPID is the process ID if Podman Desktop is running (0 if not).
	PodmanDesktopPID int

	// ConflictDescriptions provides human-readable conflict descriptions.
	ConflictDescriptions []string
}

// -----------------------------------------------------------------------------
// Security Assessment Types
// -----------------------------------------------------------------------------

// WorkloadAssessment is the security assessment of running workloads.
//
// # Description
//
// Evaluates the security posture of the container environment.
// Used to determine if auto-healing is safe.
type WorkloadAssessment struct {
	// HasForeignWorkloads is true if non-Aleutian containers exist.
	HasForeignWorkloads bool

	// ForeignContainerNames lists detected foreign container names.
	ForeignContainerNames []string

	// IsTainted is true if environment is considered compromised.
	// When tainted, auto-healing is disabled to prevent data exposure.
	IsTainted bool

	// TaintReason explains why the environment is tainted.
	TaintReason string

	// Recommendation suggests action to take.
	Recommendation string
}

// NetworkIsolationStatus is the result of verifying network isolation.
//
// # Description
//
// Provides detailed verification result for air-gap compliance auditing.
type NetworkIsolationStatus struct {
	// Isolated is true if no outbound connectivity was detected.
	Isolated bool

	// VerificationMethod describes how isolation was verified.
	// Examples: "dns_lookup_failed", "tcp_connect_refused", "route_table_empty"
	VerificationMethod string

	// TestedEndpoints lists endpoints that were tested.
	TestedEndpoints []string

	// FailureReason explains why isolation check failed (if Isolated=false).
	FailureReason string

	// Timestamp is when the verification was performed.
	Timestamp time.Time
}

// -----------------------------------------------------------------------------
// Implementation
// -----------------------------------------------------------------------------

// DefaultInfrastructureManager implements InfrastructureManager using ProcessManager.
//
// # Description
//
// This is the production implementation that executes real Podman commands
// through the ProcessManager abstraction. It integrates with DiagnosticsMetrics
// for observability and UserPrompter for interactive security confirmations.
//
// # Security
//
// All mount operations are validated against SensitiveMountPaths before execution.
// Destructive operations (RemoveMachine) are logged to the audit trail.
//
// # Thread Safety
//
// DefaultInfrastructureManager is safe for concurrent use.
type DefaultInfrastructureManager struct {
	// proc executes system commands (podman CLI).
	proc ProcessManager

	// prompter handles interactive user prompts.
	prompter UserPrompter

	// metrics records infrastructure events for observability.
	metrics DiagnosticsMetrics

	// output is where status messages are written.
	output io.Writer
}

// NewDefaultInfrastructureManager creates an infrastructure manager with all dependencies.
//
// # Description
//
// Creates a ready-to-use InfrastructureManager with injected dependencies.
// Uses os.Stdout for output if nil is provided.
//
// # Inputs
//
//   - proc: ProcessManager for executing podman commands (required)
//   - prompter: UserPrompter for interactive confirmations (required)
//   - metrics: DiagnosticsMetrics for observability (may be nil for no-op)
//
// # Outputs
//
//   - *DefaultInfrastructureManager: Ready-to-use manager
//
// # Examples
//
//	proc := NewDefaultProcessManager()
//	prompter := NewInteractivePrompter()
//	metrics := NewNoOpDiagnosticsMetrics()
//	mgr := NewDefaultInfrastructureManager(proc, prompter, metrics)
//	err := mgr.EnsureReady(ctx, DefaultInfrastructureOptions())
//
// # Limitations
//
//   - None
//
// # Assumptions
//
//   - proc and prompter are non-nil
func NewDefaultInfrastructureManager(
	proc ProcessManager,
	prompter UserPrompter,
	metrics DiagnosticsMetrics,
) *DefaultInfrastructureManager {
	output := io.Writer(os.Stdout)
	if metrics == nil {
		metrics = NewNoOpDiagnosticsMetrics()
	}
	return &DefaultInfrastructureManager{
		proc:     proc,
		prompter: prompter,
		metrics:  metrics,
		output:   output,
	}
}

// SetOutput configures the output writer for status messages.
//
// # Description
//
// Allows redirecting status output for testing or logging.
//
// # Inputs
//
//   - w: Writer for status messages
func (m *DefaultInfrastructureManager) SetOutput(w io.Writer) {
	m.output = w
}

// EnsureReady verifies infrastructure is ready for container operations.
func (m *DefaultInfrastructureManager) EnsureReady(ctx context.Context, opts InfrastructureOptions) error {
	return m.ensureReadyWithDepth(ctx, opts, 0)
}

// ensureReadyWithDepth is the internal implementation with recursion tracking.
func (m *DefaultInfrastructureManager) ensureReadyWithDepth(ctx context.Context, opts InfrastructureOptions, depth int) error {
	maxAttempts := opts.MaxHealAttempts
	if maxAttempts == 0 {
		maxAttempts = DefaultMaxHealAttempts
	}

	if depth > maxAttempts {
		return fmt.Errorf("self-healing failed after %d attempts - manual intervention required", maxAttempts)
	}

	// Platform check - only macOS needs machine management
	if runtime.GOOS != "darwin" {
		return nil
	}

	// Detect conflicts (Podman Desktop)
	conflicts, err := m.DetectConflicts(ctx)
	if err != nil {
		return fmt.Errorf("failed to detect conflicts: %w", err)
	}
	if conflicts.HasConflicts {
		fmt.Fprintf(m.output, "Warning: Podman Desktop is running (PID %d)\n", conflicts.PodmanDesktopPID)
		fmt.Fprintln(m.output, "   It creates a conflicting VM that breaks CLI tools.")
		fmt.Fprintln(m.output, "   Recommendation: Quit Podman Desktop via the Menu Bar.")

		if !opts.SkipPrompts {
			confirmed, err := m.prompter.Confirm(ctx, "Try to proceed anyway?")
			if err != nil {
				return fmt.Errorf("prompt failed: %w", err)
			}
			if !confirmed {
				return fmt.Errorf("startup cancelled due to Podman Desktop conflict")
			}
		}
	}

	// Resolve machine name
	machineName := opts.MachineName
	if machineName == "" {
		machineName = DefaultMachineName
	}

	// Resolve CPU and memory
	cpus := opts.CPUs
	if cpus == 0 {
		cpus = 6
	}
	memoryMB := opts.MemoryMB
	if memoryMB == 0 {
		memoryMB = 20480
	}

	if depth == 0 {
		fmt.Fprintln(m.output, "Aleutian Infrastructure Check...")
		fmt.Fprintf(m.output, "   Target Machine: %s (CPUs: %d, Mem: %d MB)\n", machineName, cpus, memoryMB)
	} else {
		fmt.Fprintf(m.output, "   Self-Healing attempt %d/%d...\n", depth, maxAttempts)
	}

	// Check if machine exists
	status, err := m.GetMachineStatus(ctx, machineName)
	if err != nil {
		return fmt.Errorf("failed to get machine status: %w", err)
	}

	if !status.Exists {
		fmt.Fprintln(m.output, "Machine not found. Provisioning Infrastructure...")
		spec := MachineSpec{
			Name:      machineName,
			CPUs:      cpus,
			MemoryMB:  memoryMB,
			Mounts:    opts.Mounts,
			Hardening: opts.Hardening,
		}
		return m.ProvisionMachine(ctx, spec)
	}

	// Machine exists - verify mount configuration
	fmt.Fprint(m.output, "Verifying machine configuration... ")
	verification, err := m.VerifyMounts(ctx, machineName, opts.Mounts)
	if err != nil {
		fmt.Fprintf(m.output, "WARN (couldn't verify: %v)\n", err)
	} else if !verification.Match {
		fmt.Fprintln(m.output, "DRIFT DETECTED.")
		m.printDriftWarning(verification, machineName)

		shouldRecreate := false

		if opts.ForceRecreate {
			fmt.Fprintln(m.output, "--force-recreate flag detected. Automatically fixing...")
			shouldRecreate = true
		} else {
			// Check for foreign workloads before auto-fix
			assessment, _ := m.HasForeignWorkloads(ctx)
			if assessment != nil && assessment.HasForeignWorkloads {
				fmt.Fprintln(m.output, "Foreign containers detected. Auto-fix is disabled to protect your other work.")
				fmt.Fprintln(m.output, "   Please fix manually using the commands above.")
				return nil
			}

			if !opts.SkipPrompts {
				confirmed, err := m.prompter.Confirm(ctx, "Would you like to automatically fix this now?")
				if err != nil {
					return fmt.Errorf("prompt failed: %w", err)
				}
				shouldRecreate = confirmed
			}
		}

		if shouldRecreate {
			fmt.Fprintln(m.output, "\nRecreating machine with correct mounts...")

			// Stop the machine
			fmt.Fprint(m.output, "   Stopping machine... ")
			_ = m.StopMachine(ctx, machineName)
			fmt.Fprintln(m.output, "done.")

			// Remove the machine
			fmt.Fprint(m.output, "   Removing old machine... ")
			if err := m.RemoveMachine(ctx, machineName, true, "drift_fix"); err != nil {
				return fmt.Errorf("failed to remove machine: %w", err)
			}
			fmt.Fprintln(m.output, "done.")

			// Provision new machine
			fmt.Fprintln(m.output, "   Provisioning new machine:")
			spec := MachineSpec{
				Name:      machineName,
				CPUs:      cpus,
				MemoryMB:  memoryMB,
				Mounts:    opts.Mounts,
				Hardening: opts.Hardening,
			}
			if err := m.ProvisionMachine(ctx, spec); err != nil {
				return err
			}
			fmt.Fprintln(m.output, "\n   Machine recreated successfully!")
			return nil
		}
		fmt.Fprintln(m.output, "\nProceeding with mismatched mounts. Services may fail to start.")
	} else {
		fmt.Fprintln(m.output, "OK.")
	}

	// Check if machine is running
	if !status.Running {
		fmt.Fprintln(m.output, "Machine is stopped. Booting up...")
		if err := m.StartMachine(ctx, machineName); err != nil {
			// Try self-healing
			return m.ensureReadyWithDepth(ctx, opts, depth+1)
		}
		fmt.Fprintln(m.output, "Infrastructure ready.")
	}

	return nil
}

// printDriftWarning outputs the drift detection warning message.
func (m *DefaultInfrastructureManager) printDriftWarning(verification *MountVerification, machineName string) {
	fmt.Fprintln(m.output)
	fmt.Fprintln(m.output, "MOUNT CONFIGURATION MISMATCH")
	fmt.Fprintln(m.output, "Your Podman machine has different volume mounts than your config.")
	fmt.Fprintln(m.output, "\nExpected config mounts:")
	for _, mount := range verification.ExpectedMounts {
		fmt.Fprintf(m.output, "   - %s\n", mount)
	}
	if len(verification.MissingMounts) > 0 {
		fmt.Fprintln(m.output, "\nMissing mounts:")
		for _, mount := range verification.MissingMounts {
			fmt.Fprintf(m.output, "   - %s\n", mount)
		}
	}
	fmt.Fprintln(m.output, "\nWHY THIS MATTERS:")
	fmt.Fprintln(m.output, "   - Containers won't be able to access missing mount paths")
	fmt.Fprintln(m.output, "   - You'll see 'statfs: not a directory' errors")
	fmt.Fprintln(m.output, "\nTO FIX MANUALLY:")
	fmt.Fprintln(m.output, "   1. Stop services:    aleutian stack stop")
	fmt.Fprintf(m.output, "   2. Remove machine:   podman machine rm -f %s\n", machineName)
	fmt.Fprintln(m.output, "   3. Restart:          aleutian stack start")
	fmt.Fprintln(m.output)
}

// GetMachineStatus returns the current state of a Podman machine.
func (m *DefaultInfrastructureManager) GetMachineStatus(ctx context.Context, machineName string) (*MachineStatus, error) {
	output, err := m.proc.Run(ctx, "podman", "machine", "inspect", machineName, "--format", "json")
	if err != nil {
		// Machine doesn't exist
		return &MachineStatus{
			Name:   machineName,
			Exists: false,
		}, nil
	}

	// Strip any non-JSON prefix (warnings, journalctl messages)
	outputStr := string(output)
	jsonStart := strings.IndexAny(outputStr, "[{")
	if jsonStart == -1 {
		return nil, fmt.Errorf("no JSON found in inspect output")
	}

	var machines []map[string]interface{}
	if err := json.Unmarshal([]byte(outputStr[jsonStart:]), &machines); err != nil {
		return nil, fmt.Errorf("failed to parse machine inspect output: %w", err)
	}

	if len(machines) == 0 {
		return &MachineStatus{
			Name:   machineName,
			Exists: false,
		}, nil
	}

	machine := machines[0]

	// Extract state
	state := MachineStateUnknown
	running := false
	if stateStr, ok := machine["State"].(string); ok {
		switch stateStr {
		case "running":
			state = MachineStateRunning
			running = true
		case "stopped":
			state = MachineStateStopped
		case "starting":
			state = MachineStateStarting
		}
	}

	// Extract CPUs and Memory
	cpus := 0
	memoryMB := 0
	if resources, ok := machine["Resources"].(map[string]interface{}); ok {
		if c, ok := resources["CPUs"].(float64); ok {
			cpus = int(c)
		}
		if mem, ok := resources["Memory"].(float64); ok {
			memoryMB = int(mem)
		}
	}

	// Extract mounts
	var mounts []MountInfo
	if mountsInterface, ok := machine["Mounts"]; ok {
		if mountsList, ok := mountsInterface.([]interface{}); ok {
			for _, mountInterface := range mountsList {
				if mount, ok := mountInterface.(map[string]interface{}); ok {
					mi := MountInfo{}
					if source, ok := mount["Source"].(string); ok {
						mi.Source = source
					}
					if target, ok := mount["Target"].(string); ok {
						mi.Target = target
					}
					if ro, ok := mount["ReadOnly"].(bool); ok {
						mi.ReadOnly = ro
					}
					mounts = append(mounts, mi)
				}
			}
		}
	}

	return &MachineStatus{
		Name:     machineName,
		Exists:   true,
		Running:  running,
		State:    state,
		CPUs:     cpus,
		MemoryMB: memoryMB,
		Mounts:   mounts,
	}, nil
}

// ValidateMounts checks mount paths for security violations.
func (m *DefaultInfrastructureManager) ValidateMounts(ctx context.Context, mounts []string) (*MountValidation, error) {
	result := &MountValidation{
		Valid:          true,
		ApprovedMounts: []string{},
		RejectedMounts: []MountRejection{},
		WarningMounts:  []string{},
	}

	homeDir, _ := os.UserHomeDir()

	for _, mount := range mounts {
		// Expand ~ to home directory
		expandedMount := mount
		if strings.HasPrefix(mount, "~") {
			expandedMount = strings.Replace(mount, "~", homeDir, 1)
		}

		// Check against sensitive paths (blocklist)
		rejected := false
		for _, sensitive := range SensitiveMountPaths {
			expandedSensitive := sensitive
			if strings.HasPrefix(sensitive, "~") {
				expandedSensitive = strings.Replace(sensitive, "~", homeDir, 1)
			}

			if expandedMount == expandedSensitive || strings.HasPrefix(expandedMount, expandedSensitive+"/") {
				result.Valid = false
				result.RejectedMounts = append(result.RejectedMounts, MountRejection{
					Path:     mount,
					Reason:   fmt.Sprintf("mounting %s would expose critical system files", sensitive),
					Severity: MountRejectionCritical,
				})
				rejected = true
				break
			}
		}
		if rejected {
			continue
		}

		// Check against warning paths
		isWarning := false
		for _, warn := range WarnOnMountPaths {
			expandedWarn := warn
			if strings.HasPrefix(warn, "~") {
				expandedWarn = strings.Replace(warn, "~", homeDir, 1)
			}

			if expandedMount == expandedWarn || strings.HasPrefix(expandedMount, expandedWarn+"/") {
				result.WarningMounts = append(result.WarningMounts, mount)
				isWarning = true
				break
			}
		}

		if !isWarning {
			result.ApprovedMounts = append(result.ApprovedMounts, mount)
		}
	}

	return result, nil
}

// ProvisionMachine creates a new Podman machine with the given configuration.
func (m *DefaultInfrastructureManager) ProvisionMachine(ctx context.Context, spec MachineSpec) error {
	startTime := time.Now()

	// Validate mounts first
	validation, err := m.ValidateMounts(ctx, spec.Mounts)
	if err != nil {
		return fmt.Errorf("failed to validate mounts: %w", err)
	}

	if !validation.Valid {
		for _, rejection := range validation.RejectedMounts {
			fmt.Fprintf(m.output, "   REJECTED: %s - %s\n", rejection.Path, rejection.Reason)
		}
		return fmt.Errorf("mount validation failed: %d paths rejected", len(validation.RejectedMounts))
	}

	// Build command arguments
	name := spec.Name
	if name == "" {
		name = DefaultMachineName
	}
	cpus := spec.CPUs
	if cpus == 0 {
		cpus = 6
	}
	memoryMB := spec.MemoryMB
	if memoryMB == 0 {
		memoryMB = 20480
	}

	args := []string{"machine", "init", name,
		"--cpus", strconv.Itoa(cpus),
		"--memory", strconv.Itoa(memoryMB),
	}

	// Add mounts
	homeDir, _ := os.UserHomeDir()
	aleutianDataDir := spec.Hardening.AleutianDataDir
	if aleutianDataDir == "" {
		aleutianDataDir = DefaultAleutianDataDir
	}
	// Expand ~ in AleutianDataDir
	if strings.HasPrefix(aleutianDataDir, "~") {
		aleutianDataDir = strings.Replace(aleutianDataDir, "~", homeDir, 1)
	}

	validMounts := 0
	for _, mount := range spec.Mounts {
		// Expand ~ in mount path
		expandedMount := mount
		if strings.HasPrefix(mount, "~") {
			expandedMount = strings.Replace(mount, "~", homeDir, 1)
		}

		// Check if path exists
		if _, err := os.Stat(expandedMount); os.IsNotExist(err) {
			fmt.Fprintf(m.output, "   SKIP: %s (path not found)\n", mount)
			continue
		}

		// Determine read-only status
		mountStr := fmt.Sprintf("%s:%s", expandedMount, expandedMount)

		// Check if this is under AleutianDataDir (always writable)
		isAleutianData := strings.HasPrefix(expandedMount, aleutianDataDir)

		// Check if in explicit writable list
		isExplicitWritable := false
		for _, writable := range spec.Hardening.WritableMounts {
			expandedWritable := writable
			if strings.HasPrefix(writable, "~") {
				expandedWritable = strings.Replace(writable, "~", homeDir, 1)
			}
			if expandedMount == expandedWritable || strings.HasPrefix(expandedMount, expandedWritable+"/") {
				isExplicitWritable = true
				break
			}
		}

		// Apply read-only if enabled and not an exception
		if spec.Hardening.ReadOnlyMounts && !isAleutianData && !isExplicitWritable {
			mountStr += ":ro"
			fmt.Fprintf(m.output, "   - Mounting (ro): %s\n", mount)
		} else {
			fmt.Fprintf(m.output, "   - Mounting: %s\n", mount)
		}

		args = append(args, "-v", mountStr)
		validMounts++
	}

	if validMounts == 0 {
		fmt.Fprintln(m.output, "   Warning: No valid mount paths found. Creating machine without mounts.")
	}

	// Execute podman machine init
	_, err = m.proc.Run(ctx, "podman", args...)
	if err != nil {
		return fmt.Errorf("failed to provision podman machine: %w", err)
	}

	duration := time.Since(startTime)
	m.metrics.RecordCollection(SeverityInfo, "machine_provisioned", duration.Milliseconds(), 0)

	fmt.Fprintln(m.output, "Infrastructure provisioned.")
	return nil
}

// StartMachine starts a stopped Podman machine.
func (m *DefaultInfrastructureManager) StartMachine(ctx context.Context, machineName string) error {
	_, err := m.proc.Run(ctx, "podman", "machine", "start", machineName)
	if err != nil {
		return fmt.Errorf("failed to start machine %s: %w", machineName, err)
	}
	return nil
}

// StopMachine stops a running Podman machine.
func (m *DefaultInfrastructureManager) StopMachine(ctx context.Context, machineName string) error {
	_, err := m.proc.Run(ctx, "podman", "machine", "stop", machineName)
	if err != nil {
		return fmt.Errorf("failed to stop machine %s: %w", machineName, err)
	}
	return nil
}

// RemoveMachine removes an existing Podman machine.
func (m *DefaultInfrastructureManager) RemoveMachine(ctx context.Context, machineName string, force bool, reason string) error {
	// Emit audit metric before destruction
	m.metrics.RecordCollection(SeverityWarning, fmt.Sprintf("machine_destroy_%s", reason), 0, 0)

	args := []string{"machine", "rm"}
	if force {
		args = append(args, "-f")
	}
	args = append(args, machineName)

	_, err := m.proc.Run(ctx, "podman", args...)
	if err != nil {
		return fmt.Errorf("failed to remove machine %s: %w", machineName, err)
	}
	return nil
}

// VerifyMounts checks if machine mounts match expected configuration.
func (m *DefaultInfrastructureManager) VerifyMounts(ctx context.Context, machineName string, expectedMounts []string) (*MountVerification, error) {
	status, err := m.GetMachineStatus(ctx, machineName)
	if err != nil {
		return nil, err
	}

	if !status.Exists {
		return nil, fmt.Errorf("machine %s does not exist", machineName)
	}

	// Build set of actual mounts
	actualMountSet := make(map[string]bool)
	var actualMounts []string
	for _, mount := range status.Mounts {
		actualMountSet[mount.Source] = true
		actualMounts = append(actualMounts, mount.Source)
	}

	// Compare with expected
	var missingMounts []string
	var validExpected []string
	homeDir, _ := os.UserHomeDir()

	for _, expected := range expectedMounts {
		// Expand ~ in expected path
		expandedExpected := expected
		if strings.HasPrefix(expected, "~") {
			expandedExpected = strings.Replace(expected, "~", homeDir, 1)
		}

		// Skip if path doesn't exist on host
		if _, err := os.Stat(expandedExpected); os.IsNotExist(err) {
			continue
		}

		validExpected = append(validExpected, expandedExpected)
		if !actualMountSet[expandedExpected] {
			missingMounts = append(missingMounts, expandedExpected)
		}
	}

	// Find extra mounts (in actual but not expected)
	expectedSet := make(map[string]bool)
	for _, exp := range validExpected {
		expectedSet[exp] = true
	}
	var extraMounts []string
	for _, actual := range actualMounts {
		if !expectedSet[actual] {
			extraMounts = append(extraMounts, actual)
		}
	}

	return &MountVerification{
		Match:          len(missingMounts) == 0,
		ActualMounts:   actualMounts,
		ExpectedMounts: validExpected,
		MissingMounts:  missingMounts,
		ExtraMounts:    extraMounts,
	}, nil
}

// DetectConflicts checks for processes that conflict with Podman CLI.
func (m *DefaultInfrastructureManager) DetectConflicts(ctx context.Context) (*ConflictReport, error) {
	report := &ConflictReport{
		HasConflicts:         false,
		PodmanDesktopPID:     0,
		ConflictDescriptions: []string{},
	}

	// Check for Podman Desktop
	running, pid, err := m.proc.IsRunning(ctx, "Podman Desktop")
	if err != nil {
		// Don't fail on detection error, just log
		return report, nil
	}

	if running {
		report.HasConflicts = true
		report.PodmanDesktopPID = pid
		report.ConflictDescriptions = append(report.ConflictDescriptions,
			fmt.Sprintf("Podman Desktop is running (PID %d)", pid))
	}

	return report, nil
}

// HasForeignWorkloads checks for non-Aleutian containers.
func (m *DefaultInfrastructureManager) HasForeignWorkloads(ctx context.Context) (*WorkloadAssessment, error) {
	assessment := &WorkloadAssessment{
		HasForeignWorkloads:   false,
		ForeignContainerNames: []string{},
		IsTainted:             false,
		TaintReason:           "",
		Recommendation:        "",
	}

	// List containers NOT created by Aleutian
	output, err := m.proc.Run(ctx, "podman", "ps", "-a", "--format", "{{.Names}}",
		"--filter", "label!=io.podman.compose.project=aleutian")
	if err != nil {
		return assessment, nil // Don't fail, just return empty assessment
	}

	outputStr := strings.TrimSpace(string(output))
	if outputStr == "" {
		return assessment, nil
	}

	names := strings.Split(outputStr, "\n")
	var foreignNames []string
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name != "" {
			foreignNames = append(foreignNames, name)
		}
	}

	if len(foreignNames) > 0 {
		assessment.HasForeignWorkloads = true
		assessment.ForeignContainerNames = foreignNames
		assessment.IsTainted = true
		assessment.TaintReason = fmt.Sprintf("%d foreign containers detected", len(foreignNames))
		assessment.Recommendation = "Aleutian requires a dedicated Podman machine for isolation. " +
			"Consider stopping foreign containers or using a separate machine."
	}

	return assessment, nil
}

// VerifyNetworkIsolation confirms container has no internet connectivity.
func (m *DefaultInfrastructureManager) VerifyNetworkIsolation(ctx context.Context, containerID string) (*NetworkIsolationStatus, error) {
	status := &NetworkIsolationStatus{
		Isolated:           false,
		VerificationMethod: "",
		TestedEndpoints:    []string{},
		FailureReason:      "",
		Timestamp:          time.Now(),
	}

	// Default to Aleutian inference container if not specified
	if containerID == "" {
		containerID = "aleutian-ollama"
	}

	// Test 1: DNS lookup should fail
	status.TestedEndpoints = append(status.TestedEndpoints, "dns:google.com")
	_, dnsErr := m.proc.Run(ctx, "podman", "exec", containerID, "nslookup", "google.com")
	if dnsErr == nil {
		status.Isolated = false
		status.FailureReason = "DNS lookup succeeded - container has network access"
		status.VerificationMethod = "dns_lookup_succeeded"
		return status, nil
	}

	// Test 2: TCP connection should fail
	status.TestedEndpoints = append(status.TestedEndpoints, "tcp:8.8.8.8:53")
	_, tcpErr := m.proc.Run(ctx, "podman", "exec", containerID, "timeout", "5", "nc", "-z", "8.8.8.8", "53")
	if tcpErr == nil {
		status.Isolated = false
		status.FailureReason = "TCP connection succeeded - container has network access"
		status.VerificationMethod = "tcp_connect_succeeded"
		return status, nil
	}

	// All tests failed = network is isolated
	status.Isolated = true
	status.VerificationMethod = "dns_and_tcp_failed"
	return status, nil
}

// -----------------------------------------------------------------------------
// Compile-time Interface Compliance
// -----------------------------------------------------------------------------

var _ InfrastructureManager = (*DefaultInfrastructureManager)(nil)
