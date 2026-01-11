// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jinterlante1206/AleutianLocal/cmd/aleutian/internal/infra/process"
	"github.com/jinterlante1206/AleutianLocal/cmd/aleutian/internal/util"
)

// -----------------------------------------------------------------------------
// Test Helpers
// -----------------------------------------------------------------------------

// createTestInfraManager creates an InfrastructureManager with mocks for testing.
func createTestInfraManager(t *testing.T) (*DefaultInfrastructureManager, *process.MockManager, *util.MockPrompter, *bytes.Buffer) {
	t.Helper()

	proc := &process.MockManager{}
	prompter := &util.MockPrompter{
		ConfirmFunc: func(ctx context.Context, prompt string) (bool, error) {
			return true, nil
		},
		SelectFunc: func(ctx context.Context, prompt string, options []string) (int, error) {
			return 0, nil
		},
	}
	metrics := NewNoOpDiagnosticsMetrics()
	output := &bytes.Buffer{}

	mgr := NewDefaultInfrastructureManager(proc, prompter, metrics)
	mgr.SetOutput(output)

	return mgr, proc, prompter, output
}

// -----------------------------------------------------------------------------
// DefaultHardeningConfig Tests
// -----------------------------------------------------------------------------

func TestDefaultHardeningConfig(t *testing.T) {
	cfg := DefaultHardeningConfig()

	if cfg.NetworkIsolation {
		t.Error("NetworkIsolation should default to false")
	}
	if !cfg.ReadOnlyMounts {
		t.Error("ReadOnlyMounts should default to true")
	}
	if !cfg.DropCapabilities {
		t.Error("DropCapabilities should default to true")
	}
	if cfg.AleutianDataDir != DefaultAleutianDataDir {
		t.Errorf("AleutianDataDir should default to %s, got %s", DefaultAleutianDataDir, cfg.AleutianDataDir)
	}
	if len(cfg.WritableMounts) != 0 {
		t.Error("WritableMounts should default to empty")
	}
}

func TestDefaultInfrastructureOptions(t *testing.T) {
	opts := DefaultInfrastructureOptions()

	if opts.MachineName != DefaultMachineName {
		t.Errorf("MachineName should default to %s, got %s", DefaultMachineName, opts.MachineName)
	}
	if opts.CPUs != 6 {
		t.Errorf("CPUs should default to 6, got %d", opts.CPUs)
	}
	if opts.MemoryMB != 20480 {
		t.Errorf("MemoryMB should default to 20480, got %d", opts.MemoryMB)
	}
	if opts.MaxHealAttempts != DefaultMaxHealAttempts {
		t.Errorf("MaxHealAttempts should default to %d, got %d", DefaultMaxHealAttempts, opts.MaxHealAttempts)
	}
	if opts.ForceRecreate {
		t.Error("ForceRecreate should default to false")
	}
	if opts.SkipPrompts {
		t.Error("SkipPrompts should default to false")
	}
	if opts.AllowSensitiveMounts {
		t.Error("AllowSensitiveMounts should default to false")
	}
}

// -----------------------------------------------------------------------------
// ValidateMounts Tests
// -----------------------------------------------------------------------------

func TestValidateMounts_ApprovedPaths(t *testing.T) {
	mgr, _, _, _ := createTestInfraManager(t)
	ctx := context.Background()

	result, err := mgr.ValidateMounts(ctx, []string{
		"/data/documents",
		"/Users/test/projects",
		"/Volumes/external",
	})

	if err != nil {
		t.Fatalf("ValidateMounts failed: %v", err)
	}
	if !result.Valid {
		t.Error("Expected Valid to be true for safe paths")
	}
	if len(result.ApprovedMounts) != 3 {
		t.Errorf("Expected 3 approved mounts, got %d", len(result.ApprovedMounts))
	}
	if len(result.RejectedMounts) != 0 {
		t.Errorf("Expected 0 rejected mounts, got %d", len(result.RejectedMounts))
	}
}

func TestValidateMounts_RejectedPaths(t *testing.T) {
	mgr, _, _, _ := createTestInfraManager(t)
	ctx := context.Background()

	result, err := mgr.ValidateMounts(ctx, []string{
		"/",
		"/root",
		"/var",
		"/etc",
	})

	if err != nil {
		t.Fatalf("ValidateMounts failed: %v", err)
	}
	if result.Valid {
		t.Error("Expected Valid to be false for sensitive paths")
	}
	if len(result.RejectedMounts) != 4 {
		t.Errorf("Expected 4 rejected mounts, got %d", len(result.RejectedMounts))
	}
	for _, rejection := range result.RejectedMounts {
		if rejection.Severity != MountRejectionCritical {
			t.Errorf("Expected severity to be critical, got %s", rejection.Severity)
		}
	}
}

func TestValidateMounts_WarningPaths(t *testing.T) {
	mgr, _, _, _ := createTestInfraManager(t)
	ctx := context.Background()

	homeDir, _ := os.UserHomeDir()
	result, err := mgr.ValidateMounts(ctx, []string{
		homeDir,
		filepath.Join(homeDir, ".ssh"),
		filepath.Join(homeDir, ".aws"),
	})

	if err != nil {
		t.Fatalf("ValidateMounts failed: %v", err)
	}
	// Warning paths should still be valid (they require confirmation, not rejection)
	if !result.Valid {
		t.Error("Expected Valid to be true for warning paths (they need confirmation, not rejection)")
	}
	if len(result.WarningMounts) != 3 {
		t.Errorf("Expected 3 warning mounts, got %d", len(result.WarningMounts))
	}
}

func TestValidateMounts_MixedPaths(t *testing.T) {
	mgr, _, _, _ := createTestInfraManager(t)
	ctx := context.Background()

	homeDir, _ := os.UserHomeDir()
	result, err := mgr.ValidateMounts(ctx, []string{
		"/data/safe",                   // Approved
		"/",                            // Rejected
		filepath.Join(homeDir, ".ssh"), // Warning
	})

	if err != nil {
		t.Fatalf("ValidateMounts failed: %v", err)
	}
	if result.Valid {
		t.Error("Expected Valid to be false due to rejected path")
	}
	if len(result.ApprovedMounts) != 1 {
		t.Errorf("Expected 1 approved mount, got %d", len(result.ApprovedMounts))
	}
	if len(result.RejectedMounts) != 1 {
		t.Errorf("Expected 1 rejected mount, got %d", len(result.RejectedMounts))
	}
	if len(result.WarningMounts) != 1 {
		t.Errorf("Expected 1 warning mount, got %d", len(result.WarningMounts))
	}
}

// -----------------------------------------------------------------------------
// GetMachineStatus Tests
// -----------------------------------------------------------------------------

func TestGetMachineStatus_MachineExists_Running(t *testing.T) {
	mgr, proc, _, _ := createTestInfraManager(t)
	ctx := context.Background()

	proc.RunFunc = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if name == "podman" && len(args) > 0 && args[0] == "machine" && args[1] == "inspect" {
			return []byte(`[{
				"Name": "podman-machine-default",
				"State": "running",
				"Resources": {
					"CPUs": 6,
					"Memory": 20480
				},
				"Mounts": [
					{"Source": "/data", "Target": "/data", "ReadOnly": false}
				]
			}]`), nil
		}
		return nil, errors.New("unexpected command")
	}

	status, err := mgr.GetMachineStatus(ctx, "podman-machine-default")
	if err != nil {
		t.Fatalf("GetMachineStatus failed: %v", err)
	}

	if !status.Exists {
		t.Error("Expected Exists to be true")
	}
	if !status.Running {
		t.Error("Expected Running to be true")
	}
	if status.State != MachineStateRunning {
		t.Errorf("Expected state to be running, got %s", status.State)
	}
	if status.CPUs != 6 {
		t.Errorf("Expected 6 CPUs, got %d", status.CPUs)
	}
	if len(status.Mounts) != 1 {
		t.Errorf("Expected 1 mount, got %d", len(status.Mounts))
	}
}

func TestGetMachineStatus_MachineExists_Stopped(t *testing.T) {
	mgr, proc, _, _ := createTestInfraManager(t)
	ctx := context.Background()

	proc.RunFunc = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if name == "podman" && len(args) > 0 && args[0] == "machine" && args[1] == "inspect" {
			return []byte(`[{
				"Name": "podman-machine-default",
				"State": "stopped",
				"Resources": {"CPUs": 4, "Memory": 8192}
			}]`), nil
		}
		return nil, errors.New("unexpected command")
	}

	status, err := mgr.GetMachineStatus(ctx, "podman-machine-default")
	if err != nil {
		t.Fatalf("GetMachineStatus failed: %v", err)
	}

	if !status.Exists {
		t.Error("Expected Exists to be true")
	}
	if status.Running {
		t.Error("Expected Running to be false")
	}
	if status.State != MachineStateStopped {
		t.Errorf("Expected state to be stopped, got %s", status.State)
	}
}

func TestGetMachineStatus_MachineNotExists(t *testing.T) {
	mgr, proc, _, _ := createTestInfraManager(t)
	ctx := context.Background()

	proc.RunFunc = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return nil, errors.New("machine not found")
	}

	status, err := mgr.GetMachineStatus(ctx, "nonexistent-machine")
	if err != nil {
		t.Fatalf("GetMachineStatus failed: %v", err)
	}

	if status.Exists {
		t.Error("Expected Exists to be false for nonexistent machine")
	}
}

func TestGetMachineStatus_StripJournalPrefix(t *testing.T) {
	mgr, proc, _, _ := createTestInfraManager(t)
	ctx := context.Background()

	// Simulate podman output with warning prefix (no array brackets in warning)
	proc.RunFunc = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return []byte(`WARN: some warning message about journalctl
[{"Name": "podman-machine-default", "State": "running"}]`), nil
	}

	status, err := mgr.GetMachineStatus(ctx, "podman-machine-default")
	if err != nil {
		t.Fatalf("GetMachineStatus failed to strip prefix: %v", err)
	}

	if !status.Exists {
		t.Error("Expected Exists to be true after stripping prefix")
	}
}

// -----------------------------------------------------------------------------
// VerifyMounts Tests
// -----------------------------------------------------------------------------

func TestVerifyMounts_AllMatch(t *testing.T) {
	mgr, proc, _, _ := createTestInfraManager(t)
	ctx := context.Background()

	// Create a temp directory for testing
	tempDir := t.TempDir()

	proc.RunFunc = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return []byte(`[{
			"Name": "podman-machine-default",
			"State": "running",
			"Mounts": [
				{"Source": "` + tempDir + `", "Target": "` + tempDir + `"}
			]
		}]`), nil
	}

	result, err := mgr.VerifyMounts(ctx, "podman-machine-default", []string{tempDir})
	if err != nil {
		t.Fatalf("VerifyMounts failed: %v", err)
	}

	if !result.Match {
		t.Error("Expected Match to be true")
	}
	if len(result.MissingMounts) != 0 {
		t.Errorf("Expected 0 missing mounts, got %d", len(result.MissingMounts))
	}
}

func TestVerifyMounts_MissingMounts(t *testing.T) {
	mgr, proc, _, _ := createTestInfraManager(t)
	ctx := context.Background()

	tempDir1 := t.TempDir()
	tempDir2 := t.TempDir()

	proc.RunFunc = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return []byte(`[{
			"Name": "podman-machine-default",
			"State": "running",
			"Mounts": [
				{"Source": "` + tempDir1 + `", "Target": "` + tempDir1 + `"}
			]
		}]`), nil
	}

	result, err := mgr.VerifyMounts(ctx, "podman-machine-default", []string{tempDir1, tempDir2})
	if err != nil {
		t.Fatalf("VerifyMounts failed: %v", err)
	}

	if result.Match {
		t.Error("Expected Match to be false")
	}
	if len(result.MissingMounts) != 1 {
		t.Errorf("Expected 1 missing mount, got %d", len(result.MissingMounts))
	}
	if result.MissingMounts[0] != tempDir2 {
		t.Errorf("Expected missing mount to be %s, got %s", tempDir2, result.MissingMounts[0])
	}
}

func TestVerifyMounts_SkipsNonexistentPaths(t *testing.T) {
	mgr, proc, _, _ := createTestInfraManager(t)
	ctx := context.Background()

	proc.RunFunc = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return []byte(`[{
			"Name": "podman-machine-default",
			"State": "running",
			"Mounts": []
		}]`), nil
	}

	result, err := mgr.VerifyMounts(ctx, "podman-machine-default", []string{
		"/nonexistent/path/that/does/not/exist",
	})
	if err != nil {
		t.Fatalf("VerifyMounts failed: %v", err)
	}

	// Nonexistent paths are skipped, so there should be no missing mounts
	if !result.Match {
		t.Error("Expected Match to be true (nonexistent paths are skipped)")
	}
}

// -----------------------------------------------------------------------------
// DetectConflicts Tests
// -----------------------------------------------------------------------------

func TestDetectConflicts_NoConflicts(t *testing.T) {
	mgr, proc, _, _ := createTestInfraManager(t)
	ctx := context.Background()

	proc.IsRunningFunc = func(ctx context.Context, processName string) (bool, int, error) {
		return false, 0, nil
	}

	report, err := mgr.DetectConflicts(ctx)
	if err != nil {
		t.Fatalf("DetectConflicts failed: %v", err)
	}

	if report.HasConflicts {
		t.Error("Expected HasConflicts to be false")
	}
	if report.PodmanDesktopPID != 0 {
		t.Errorf("Expected PodmanDesktopPID to be 0, got %d", report.PodmanDesktopPID)
	}
}

func TestDetectConflicts_PodmanDesktopRunning(t *testing.T) {
	mgr, proc, _, _ := createTestInfraManager(t)
	ctx := context.Background()

	proc.IsRunningFunc = func(ctx context.Context, processName string) (bool, int, error) {
		if strings.Contains(processName, "Podman Desktop") {
			return true, 12345, nil
		}
		return false, 0, nil
	}

	report, err := mgr.DetectConflicts(ctx)
	if err != nil {
		t.Fatalf("DetectConflicts failed: %v", err)
	}

	if !report.HasConflicts {
		t.Error("Expected HasConflicts to be true")
	}
	if report.PodmanDesktopPID != 12345 {
		t.Errorf("Expected PodmanDesktopPID to be 12345, got %d", report.PodmanDesktopPID)
	}
	if len(report.ConflictDescriptions) == 0 {
		t.Error("Expected conflict description to be added")
	}
}

// -----------------------------------------------------------------------------
// HasForeignWorkloads Tests
// -----------------------------------------------------------------------------

func TestHasForeignWorkloads_None(t *testing.T) {
	mgr, proc, _, _ := createTestInfraManager(t)
	ctx := context.Background()

	proc.RunFunc = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		// Empty output = no foreign workloads
		return []byte(""), nil
	}

	assessment, err := mgr.HasForeignWorkloads(ctx)
	if err != nil {
		t.Fatalf("HasForeignWorkloads failed: %v", err)
	}

	if assessment.HasForeignWorkloads {
		t.Error("Expected HasForeignWorkloads to be false")
	}
	if assessment.IsTainted {
		t.Error("Expected IsTainted to be false")
	}
}

func TestHasForeignWorkloads_Present(t *testing.T) {
	mgr, proc, _, _ := createTestInfraManager(t)
	ctx := context.Background()

	proc.RunFunc = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return []byte("my-other-container\nsome-dev-container\n"), nil
	}

	assessment, err := mgr.HasForeignWorkloads(ctx)
	if err != nil {
		t.Fatalf("HasForeignWorkloads failed: %v", err)
	}

	if !assessment.HasForeignWorkloads {
		t.Error("Expected HasForeignWorkloads to be true")
	}
	if !assessment.IsTainted {
		t.Error("Expected IsTainted to be true")
	}
	if len(assessment.ForeignContainerNames) != 2 {
		t.Errorf("Expected 2 foreign containers, got %d", len(assessment.ForeignContainerNames))
	}
}

// -----------------------------------------------------------------------------
// ProvisionMachine Tests
// -----------------------------------------------------------------------------

func TestProvisionMachine_Success(t *testing.T) {
	mgr, proc, _, output := createTestInfraManager(t)
	ctx := context.Background()

	// Create temp dir inside home directory to avoid /var blocking
	homeDir, _ := os.UserHomeDir()
	tempDir := filepath.Join(homeDir, ".aleutian-test-temp")
	os.MkdirAll(tempDir, 0755)
	defer os.RemoveAll(tempDir)

	var capturedArgs []string

	proc.RunFunc = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		capturedArgs = args
		return []byte(""), nil
	}

	spec := MachineSpec{
		Name:      "test-machine",
		CPUs:      4,
		MemoryMB:  8192,
		Mounts:    []string{tempDir},
		Hardening: DefaultHardeningConfig(),
	}

	err := mgr.ProvisionMachine(ctx, spec)
	if err != nil {
		t.Fatalf("ProvisionMachine failed: %v", err)
	}

	// Verify command arguments
	if len(capturedArgs) == 0 {
		t.Fatal("Expected command arguments to be captured")
	}
	if capturedArgs[0] != "machine" || capturedArgs[1] != "init" {
		t.Error("Expected 'machine init' command")
	}

	// Verify output contains mount info
	if !strings.Contains(output.String(), "Mounting") {
		t.Error("Expected output to contain mount information")
	}
}

func TestProvisionMachine_ReadOnlyMounts(t *testing.T) {
	mgr, proc, _, output := createTestInfraManager(t)
	ctx := context.Background()

	// Create temp dir inside home directory to avoid /var blocking
	homeDir, _ := os.UserHomeDir()
	tempDir := filepath.Join(homeDir, ".aleutian-test-ro")
	os.MkdirAll(tempDir, 0755)
	defer os.RemoveAll(tempDir)

	var capturedArgs []string

	proc.RunFunc = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		capturedArgs = args
		return []byte(""), nil
	}

	spec := MachineSpec{
		Name:     "test-machine",
		CPUs:     4,
		MemoryMB: 8192,
		Mounts:   []string{tempDir},
		Hardening: HardeningConfig{
			ReadOnlyMounts:   true,
			DropCapabilities: true,
			AleutianDataDir:  "/nonexistent/aleutian", // Ensure tempDir is not under this
		},
	}

	err := mgr.ProvisionMachine(ctx, spec)
	if err != nil {
		t.Fatalf("ProvisionMachine failed: %v", err)
	}

	// Verify the mount has :ro suffix
	foundROMount := false
	for i, arg := range capturedArgs {
		if arg == "-v" && i+1 < len(capturedArgs) {
			if strings.HasSuffix(capturedArgs[i+1], ":ro") {
				foundROMount = true
				break
			}
		}
	}
	if !foundROMount {
		t.Error("Expected mount to have :ro suffix when ReadOnlyMounts=true")
	}

	if !strings.Contains(output.String(), "(ro)") {
		t.Error("Expected output to indicate read-only mount")
	}
}

func TestProvisionMachine_AleutianDataDirWritable(t *testing.T) {
	mgr, proc, _, _ := createTestInfraManager(t)
	ctx := context.Background()

	// Create a temp dir structure inside home directory to simulate AleutianDataDir
	homeDir, _ := os.UserHomeDir()
	tempBase := filepath.Join(homeDir, ".aleutian-test-data")
	aleutianData := filepath.Join(tempBase, "aleutian_data")
	modelsCache := filepath.Join(aleutianData, "models_cache")
	os.MkdirAll(modelsCache, 0755)
	defer os.RemoveAll(tempBase)

	var capturedArgs []string
	proc.RunFunc = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		capturedArgs = args
		return []byte(""), nil
	}

	spec := MachineSpec{
		Name:     "test-machine",
		CPUs:     4,
		MemoryMB: 8192,
		Mounts:   []string{aleutianData},
		Hardening: HardeningConfig{
			ReadOnlyMounts:   true,
			DropCapabilities: true,
			AleutianDataDir:  aleutianData,
		},
	}

	err := mgr.ProvisionMachine(ctx, spec)
	if err != nil {
		t.Fatalf("ProvisionMachine failed: %v", err)
	}

	// Verify AleutianDataDir mount does NOT have :ro suffix
	for i, arg := range capturedArgs {
		if arg == "-v" && i+1 < len(capturedArgs) {
			mountSpec := capturedArgs[i+1]
			if strings.Contains(mountSpec, aleutianData) {
				if strings.HasSuffix(mountSpec, ":ro") {
					t.Error("AleutianDataDir should NOT have :ro suffix")
				}
				break
			}
		}
	}
}

func TestProvisionMachine_RejectsSensitivePaths(t *testing.T) {
	mgr, proc, _, _ := createTestInfraManager(t)
	ctx := context.Background()

	proc.RunFunc = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return []byte(""), nil
	}

	spec := MachineSpec{
		Name:      "test-machine",
		CPUs:      4,
		MemoryMB:  8192,
		Mounts:    []string{"/"},
		Hardening: DefaultHardeningConfig(),
	}

	err := mgr.ProvisionMachine(ctx, spec)
	if err == nil {
		t.Error("Expected error when mounting root path")
	}
	if !strings.Contains(err.Error(), "rejected") {
		t.Errorf("Expected rejection error, got: %v", err)
	}
}

// -----------------------------------------------------------------------------
// StartMachine / StopMachine / RemoveMachine Tests
// -----------------------------------------------------------------------------

func TestStartMachine_Success(t *testing.T) {
	mgr, proc, _, _ := createTestInfraManager(t)
	ctx := context.Background()

	var capturedArgs []string
	proc.RunFunc = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		capturedArgs = args
		return []byte(""), nil
	}

	err := mgr.StartMachine(ctx, "test-machine")
	if err != nil {
		t.Fatalf("StartMachine failed: %v", err)
	}

	if len(capturedArgs) < 3 || capturedArgs[0] != "machine" || capturedArgs[1] != "start" {
		t.Error("Expected 'machine start' command")
	}
}

func TestStopMachine_Success(t *testing.T) {
	mgr, proc, _, _ := createTestInfraManager(t)
	ctx := context.Background()

	var capturedArgs []string
	proc.RunFunc = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		capturedArgs = args
		return []byte(""), nil
	}

	err := mgr.StopMachine(ctx, "test-machine")
	if err != nil {
		t.Fatalf("StopMachine failed: %v", err)
	}

	if len(capturedArgs) < 3 || capturedArgs[0] != "machine" || capturedArgs[1] != "stop" {
		t.Error("Expected 'machine stop' command")
	}
}

func TestRemoveMachine_Success(t *testing.T) {
	mgr, proc, _, _ := createTestInfraManager(t)
	ctx := context.Background()

	var capturedArgs []string
	proc.RunFunc = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		capturedArgs = args
		return []byte(""), nil
	}

	err := mgr.RemoveMachine(ctx, "test-machine", true, "test_reason")
	if err != nil {
		t.Fatalf("RemoveMachine failed: %v", err)
	}

	// Verify force flag
	foundForce := false
	for _, arg := range capturedArgs {
		if arg == "-f" {
			foundForce = true
			break
		}
	}
	if !foundForce {
		t.Error("Expected -f flag for force removal")
	}
}

// -----------------------------------------------------------------------------
// VerifyNetworkIsolation Tests
// -----------------------------------------------------------------------------

func TestVerifyNetworkIsolation_Isolated(t *testing.T) {
	mgr, proc, _, _ := createTestInfraManager(t)
	ctx := context.Background()

	proc.RunFunc = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		// All network tests should fail in isolated mode
		return nil, errors.New("network unreachable")
	}

	status, err := mgr.VerifyNetworkIsolation(ctx, "aleutian-ollama")
	if err != nil {
		t.Fatalf("VerifyNetworkIsolation failed: %v", err)
	}

	if !status.Isolated {
		t.Error("Expected Isolated to be true when network tests fail")
	}
	if status.VerificationMethod != "dns_and_tcp_failed" {
		t.Errorf("Expected verification method to be dns_and_tcp_failed, got %s", status.VerificationMethod)
	}
}

func TestVerifyNetworkIsolation_NotIsolated_DNSWorks(t *testing.T) {
	mgr, proc, _, _ := createTestInfraManager(t)
	ctx := context.Background()

	proc.RunFunc = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		// DNS lookup succeeds = not isolated
		for _, arg := range args {
			if arg == "nslookup" {
				return []byte("Server: 8.8.8.8\nAddress: 142.250.80.14"), nil
			}
		}
		return nil, errors.New("command failed")
	}

	status, err := mgr.VerifyNetworkIsolation(ctx, "aleutian-ollama")
	if err != nil {
		t.Fatalf("VerifyNetworkIsolation failed: %v", err)
	}

	if status.Isolated {
		t.Error("Expected Isolated to be false when DNS works")
	}
	if status.VerificationMethod != "dns_lookup_succeeded" {
		t.Errorf("Expected verification method to be dns_lookup_succeeded, got %s", status.VerificationMethod)
	}
}

func TestVerifyNetworkIsolation_DefaultContainer(t *testing.T) {
	mgr, proc, _, _ := createTestInfraManager(t)
	ctx := context.Background()

	var capturedContainerID string
	proc.RunFunc = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		for i, arg := range args {
			if arg == "exec" && i+1 < len(args) {
				capturedContainerID = args[i+1]
			}
		}
		return nil, errors.New("network unreachable")
	}

	// Empty containerID should default to aleutian-ollama
	_, err := mgr.VerifyNetworkIsolation(ctx, "")
	if err != nil {
		t.Fatalf("VerifyNetworkIsolation failed: %v", err)
	}

	if capturedContainerID != "aleutian-ollama" {
		t.Errorf("Expected default container to be aleutian-ollama, got %s", capturedContainerID)
	}
}

// -----------------------------------------------------------------------------
// Compile-time Interface Compliance Tests
// -----------------------------------------------------------------------------

func TestInfrastructureManagerInterfaceCompliance(t *testing.T) {
	var _ InfrastructureManager = (*DefaultInfrastructureManager)(nil)
}
