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
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDefaultReliabilityConfig(t *testing.T) {
	config := DefaultReliabilityConfig()

	if config.DataDir == "" {
		t.Error("DataDir should have default value")
	}
	if config.LockDir == "" {
		t.Error("LockDir should have default value")
	}
	if config.BackupDir == "" {
		t.Error("BackupDir should have default value")
	}
	if !config.EnableProcessLock {
		t.Error("EnableProcessLock should default to true")
	}
	if config.SamplingRate <= 0 {
		t.Error("SamplingRate should be positive")
	}
}

func TestNewReliabilityManager(t *testing.T) {
	config := DefaultReliabilityConfig()
	manager := NewReliabilityManager(config)

	if manager == nil {
		t.Fatal("NewReliabilityManager returned nil")
	}
}

func TestReliabilityManager_Initialize(t *testing.T) {
	tempDir := t.TempDir()

	config := ReliabilityConfig{
		DataDir:                    tempDir,
		LockDir:                    filepath.Join(tempDir, "locks"),
		BackupDir:                  filepath.Join(tempDir, "backups"),
		EnableProcessLock:          true,
		EnableRetentionEnforcement: false, // Disable to avoid background task
		EnableStateAudit:           false, // Disable to avoid background task
		EnableImageValidation:      true,
		SamplingRate:               0.5,
	}

	manager := NewReliabilityManager(config)
	defer manager.Shutdown()

	err := manager.Initialize(context.Background())
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	// Directories should exist
	if _, err := os.Stat(config.LockDir); os.IsNotExist(err) {
		t.Error("LockDir should be created")
	}
	if _, err := os.Stat(config.BackupDir); os.IsNotExist(err) {
		t.Error("BackupDir should be created")
	}
}

func TestReliabilityManager_InitializeIdempotent(t *testing.T) {
	tempDir := t.TempDir()

	config := ReliabilityConfig{
		DataDir:                    tempDir,
		LockDir:                    filepath.Join(tempDir, "locks"),
		BackupDir:                  filepath.Join(tempDir, "backups"),
		EnableRetentionEnforcement: false,
		EnableStateAudit:           false,
	}

	manager := NewReliabilityManager(config)
	defer manager.Shutdown()

	// First initialize
	err := manager.Initialize(context.Background())
	if err != nil {
		t.Fatalf("First Initialize failed: %v", err)
	}

	// Second initialize should be no-op
	err = manager.Initialize(context.Background())
	if err != nil {
		t.Fatalf("Second Initialize failed: %v", err)
	}
}

func TestReliabilityManager_Shutdown(t *testing.T) {
	tempDir := t.TempDir()

	config := ReliabilityConfig{
		DataDir:                    tempDir,
		LockDir:                    filepath.Join(tempDir, "locks"),
		BackupDir:                  filepath.Join(tempDir, "backups"),
		EnableRetentionEnforcement: false,
		EnableStateAudit:           false,
	}

	manager := NewReliabilityManager(config)

	manager.Initialize(context.Background())
	manager.Shutdown()

	// Double shutdown should not panic
	manager.Shutdown()
}

func TestReliabilityManager_ProcessLock(t *testing.T) {
	tempDir := t.TempDir()

	config := ReliabilityConfig{
		DataDir:                    tempDir,
		LockDir:                    filepath.Join(tempDir, "locks"),
		BackupDir:                  filepath.Join(tempDir, "backups"),
		EnableProcessLock:          true,
		EnableRetentionEnforcement: false,
		EnableStateAudit:           false,
	}

	manager := NewReliabilityManager(config)
	defer manager.Shutdown()
	manager.Initialize(context.Background())

	// Acquire lock
	err := manager.AcquireProcessLock()
	if err != nil {
		t.Fatalf("AcquireProcessLock failed: %v", err)
	}

	// Release lock
	err = manager.ReleaseProcessLock()
	if err != nil {
		t.Fatalf("ReleaseProcessLock failed: %v", err)
	}
}

func TestReliabilityManager_ProcessLockDisabled(t *testing.T) {
	tempDir := t.TempDir()

	config := ReliabilityConfig{
		DataDir:                    tempDir,
		LockDir:                    filepath.Join(tempDir, "locks"),
		BackupDir:                  filepath.Join(tempDir, "backups"),
		EnableProcessLock:          false, // Disabled
		EnableRetentionEnforcement: false,
		EnableStateAudit:           false,
	}

	manager := NewReliabilityManager(config)
	defer manager.Shutdown()
	manager.Initialize(context.Background())

	// Should be no-ops when disabled
	err := manager.AcquireProcessLock()
	if err != nil {
		t.Errorf("AcquireProcessLock should succeed when disabled: %v", err)
	}

	err = manager.ReleaseProcessLock()
	if err != nil {
		t.Errorf("ReleaseProcessLock should succeed when disabled: %v", err)
	}
}

func TestReliabilityManager_CheckResources(t *testing.T) {
	tempDir := t.TempDir()

	config := ReliabilityConfig{
		DataDir:                    tempDir,
		LockDir:                    filepath.Join(tempDir, "locks"),
		BackupDir:                  filepath.Join(tempDir, "backups"),
		EnableRetentionEnforcement: false,
		EnableStateAudit:           false,
	}

	manager := NewReliabilityManager(config)
	defer manager.Shutdown()
	manager.Initialize(context.Background())

	limits := manager.CheckResources()

	// Should return some data
	if limits.CheckedAt.IsZero() {
		t.Error("CheckedAt should be set")
	}
}

func TestReliabilityManager_ValidateImage(t *testing.T) {
	tempDir := t.TempDir()

	config := ReliabilityConfig{
		DataDir:                    tempDir,
		LockDir:                    filepath.Join(tempDir, "locks"),
		BackupDir:                  filepath.Join(tempDir, "backups"),
		EnableImageValidation:      true,
		EnableRetentionEnforcement: false,
		EnableStateAudit:           false,
	}

	manager := NewReliabilityManager(config)
	defer manager.Shutdown()
	manager.Initialize(context.Background())

	// Test unpinned image
	result, err := manager.ValidateImage("nginx:latest")
	if err != nil {
		t.Fatalf("ValidateImage failed: %v", err)
	}

	if result.IsPinned {
		t.Error("nginx:latest should not be pinned")
	}

	// Test pinned image
	result, err = manager.ValidateImage("nginx@sha256:abc123def456abc123def456abc123def456abc123def456abc123def456abcd")
	if err != nil {
		t.Fatalf("ValidateImage failed: %v", err)
	}

	if !result.IsPinned {
		t.Error("Image with digest should be pinned")
	}
}

func TestReliabilityManager_ValidateImageDisabled(t *testing.T) {
	tempDir := t.TempDir()

	config := ReliabilityConfig{
		DataDir:                    tempDir,
		LockDir:                    filepath.Join(tempDir, "locks"),
		BackupDir:                  filepath.Join(tempDir, "backups"),
		EnableImageValidation:      false, // Disabled
		EnableRetentionEnforcement: false,
		EnableStateAudit:           false,
	}

	manager := NewReliabilityManager(config)
	defer manager.Shutdown()
	manager.Initialize(context.Background())

	result, err := manager.ValidateImage("nginx:latest")
	if err != nil {
		t.Fatalf("ValidateImage failed: %v", err)
	}

	// When disabled, should always return pinned=true
	if !result.IsPinned {
		t.Error("Should return pinned=true when validation disabled")
	}
}

func TestReliabilityManager_BackupBeforeChange(t *testing.T) {
	tempDir := t.TempDir()

	config := ReliabilityConfig{
		DataDir:                    tempDir,
		LockDir:                    filepath.Join(tempDir, "locks"),
		BackupDir:                  filepath.Join(tempDir, "backups"),
		EnableRetentionEnforcement: false,
		EnableStateAudit:           false,
	}

	manager := NewReliabilityManager(config)
	defer manager.Shutdown()
	manager.Initialize(context.Background())

	// Create a file to backup
	testFile := filepath.Join(tempDir, "test.txt")
	os.WriteFile(testFile, []byte("original content"), 0644)

	backupPath, err := manager.BackupBeforeChange(testFile)
	if err != nil {
		t.Fatalf("BackupBeforeChange failed: %v", err)
	}

	if backupPath == "" {
		t.Error("BackupPath should not be empty")
	}

	// Backup file should exist
	if _, err := os.Stat(backupPath); os.IsNotExist(err) {
		t.Error("Backup file should exist")
	}
}

func TestReliabilityManager_ShouldSample(t *testing.T) {
	tempDir := t.TempDir()

	config := ReliabilityConfig{
		DataDir:                    tempDir,
		LockDir:                    filepath.Join(tempDir, "locks"),
		BackupDir:                  filepath.Join(tempDir, "backups"),
		SamplingRate:               1.0, // 100% sampling
		EnableRetentionEnforcement: false,
		EnableStateAudit:           false,
	}

	manager := NewReliabilityManager(config)
	defer manager.Shutdown()
	manager.Initialize(context.Background())

	// At 100% rate, should always sample
	for i := 0; i < 10; i++ {
		if !manager.ShouldSample() {
			t.Error("Should always sample at 100% rate")
		}
	}
}

func TestReliabilityManager_RecordLatency(t *testing.T) {
	tempDir := t.TempDir()

	config := ReliabilityConfig{
		DataDir:                    tempDir,
		LockDir:                    filepath.Join(tempDir, "locks"),
		BackupDir:                  filepath.Join(tempDir, "backups"),
		EnableRetentionEnforcement: false,
		EnableStateAudit:           false,
	}

	manager := NewReliabilityManager(config)
	defer manager.Shutdown()
	manager.Initialize(context.Background())

	// Should not panic
	manager.RecordLatency(100 * time.Millisecond)
}

func TestReliabilityManager_ValidateMetric(t *testing.T) {
	tempDir := t.TempDir()

	config := ReliabilityConfig{
		DataDir:                    tempDir,
		LockDir:                    filepath.Join(tempDir, "locks"),
		BackupDir:                  filepath.Join(tempDir, "backups"),
		EnableRetentionEnforcement: false,
		EnableStateAudit:           false,
	}

	manager := NewReliabilityManager(config)
	defer manager.Shutdown()
	manager.Initialize(context.Background())

	// Known metric should be valid
	err := manager.ValidateMetric("aleutian_health_check_duration")
	if err != nil {
		t.Errorf("Known metric should be valid: %v", err)
	}
}

func TestReliabilityManager_NormalizeLabel(t *testing.T) {
	tempDir := t.TempDir()

	config := ReliabilityConfig{
		DataDir:                    tempDir,
		LockDir:                    filepath.Join(tempDir, "locks"),
		BackupDir:                  filepath.Join(tempDir, "backups"),
		EnableRetentionEnforcement: false,
		EnableStateAudit:           false,
	}

	manager := NewReliabilityManager(config)
	defer manager.Shutdown()
	manager.Initialize(context.Background())

	// Known value should be returned as-is
	result := manager.NormalizeLabel("status", "healthy")
	if result != "healthy" {
		t.Errorf("NormalizeLabel = %q, want 'healthy'", result)
	}

	// Unknown value should be normalized to "unknown"
	result = manager.NormalizeLabel("error_type", "some_random_error")
	if result != "unknown" {
		t.Errorf("NormalizeLabel = %q, want 'unknown'", result)
	}
}

func TestReliabilityManager_TrackGoroutine(t *testing.T) {
	tempDir := t.TempDir()

	config := ReliabilityConfig{
		DataDir:                    tempDir,
		LockDir:                    filepath.Join(tempDir, "locks"),
		BackupDir:                  filepath.Join(tempDir, "backups"),
		EnableRetentionEnforcement: false,
		EnableStateAudit:           false,
	}

	manager := NewReliabilityManager(config)
	defer manager.Shutdown()
	manager.Initialize(context.Background())

	// Track a goroutine
	done := manager.TrackGoroutine("test_worker")

	stats := manager.GetGoroutineStats()
	if stats.Active != 1 {
		t.Errorf("Active goroutines = %d, want 1", stats.Active)
	}

	// Complete the goroutine
	done()

	stats = manager.GetGoroutineStats()
	if stats.Active != 0 {
		t.Errorf("Active goroutines = %d, want 0", stats.Active)
	}
}

func TestReliabilityManager_StateAudit(t *testing.T) {
	tempDir := t.TempDir()

	config := ReliabilityConfig{
		DataDir:                    tempDir,
		LockDir:                    filepath.Join(tempDir, "locks"),
		BackupDir:                  filepath.Join(tempDir, "backups"),
		EnableStateAudit:           true,
		StateAuditInterval:         time.Hour, // Long interval to prevent automatic runs
		EnableRetentionEnforcement: false,
	}

	manager := NewReliabilityManager(config)
	defer manager.Shutdown()
	manager.Initialize(context.Background())

	// Register a state
	err := manager.RegisterStateForAudit("test_state", StateRegistration{
		GetCached: func() (interface{}, error) { return "same", nil },
		GetActual: func(ctx context.Context) (interface{}, error) { return "same", nil },
	})
	if err != nil {
		t.Fatalf("RegisterStateForAudit failed: %v", err)
	}

	report := manager.GetDriftReport()
	// Should have no drift initially
	if len(report.DriftingStates) != 0 {
		t.Errorf("Should have no drift initially, got: %v", report.DriftingStates)
	}
}

func TestReliabilityManager_CreateSaga(t *testing.T) {
	tempDir := t.TempDir()

	config := ReliabilityConfig{
		DataDir:                    tempDir,
		LockDir:                    filepath.Join(tempDir, "locks"),
		BackupDir:                  filepath.Join(tempDir, "backups"),
		EnableRetentionEnforcement: false,
		EnableStateAudit:           false,
	}

	manager := NewReliabilityManager(config)
	defer manager.Shutdown()
	manager.Initialize(context.Background())

	saga := manager.CreateSaga()
	if saga == nil {
		t.Error("CreateSaga returned nil")
	}
}

func TestReliabilityManager_HealthCheck(t *testing.T) {
	tempDir := t.TempDir()

	config := ReliabilityConfig{
		DataDir:                    tempDir,
		LockDir:                    filepath.Join(tempDir, "locks"),
		BackupDir:                  filepath.Join(tempDir, "backups"),
		EnableProcessLock:          true,
		SamplingRate:               0.5,
		EnableRetentionEnforcement: false,
		EnableStateAudit:           false,
	}

	manager := NewReliabilityManager(config)
	defer manager.Shutdown()
	manager.Initialize(context.Background())

	health := manager.HealthCheck()

	if health.Timestamp.IsZero() {
		t.Error("Timestamp should be set")
	}

	if health.SamplingRate <= 0 {
		t.Error("SamplingRate should be set")
	}
}

func TestReliabilityManager_InterfaceCompliance(t *testing.T) {
	var _ ReliabilityOrchestrator = (*ReliabilityManager)(nil)
}

func TestGetReliabilityManager(t *testing.T) {
	rm1 := GetReliabilityManager()
	rm2 := GetReliabilityManager()

	if rm1 != rm2 {
		t.Error("GetReliabilityManager should return singleton")
	}
}

func TestGetReliabilityComponents(t *testing.T) {
	components := GetReliabilityComponents()

	if len(components) == 0 {
		t.Error("Should have components defined")
	}

	// Check that each component has required fields
	for _, c := range components {
		if c.Name == "" {
			t.Error("Component name should not be empty")
		}
		if c.File == "" {
			t.Error("Component file should not be empty")
		}
		if c.Category == "" {
			t.Error("Component category should not be empty")
		}
	}
}

func TestGetDependencyGraph(t *testing.T) {
	graph := GetDependencyGraph()

	if len(graph) == 0 {
		t.Error("Dependency graph should not be empty")
	}

	// Check that cmd_stack.runStart has dependencies
	deps, ok := graph["cmd_stack.runStart"]
	if !ok {
		t.Error("Should have cmd_stack.runStart in graph")
	}
	if len(deps) == 0 {
		t.Error("cmd_stack.runStart should have dependencies")
	}
}
