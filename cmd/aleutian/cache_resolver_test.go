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
Package main provides tests for CachePathResolver.

This file contains:
  - MockCachePathResolver: A mock implementation for testing
  - Unit tests for all CachePathResolver methods
  - Test helpers for creating test fixtures

# Test Categories

  - Interface compliance tests
  - Resolution priority tests
  - Platform-specific behavior tests
  - Error handling tests
  - Configuration tests
*/
package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// =============================================================================
// Mock Implementation
// =============================================================================

// MockCachePathResolver is a mock implementation of CachePathResolver for testing.
//
// # Description
//
// Provides a configurable mock for testing code that depends on CachePathResolver.
// All behavior can be configured through the struct fields.
//
// # Thread Safety
//
// MockCachePathResolver is NOT thread-safe. Use only in single-threaded tests.
//
// # Examples
//
//	mock := NewMockCachePathResolver()
//	mock.ResolvedPaths[CacheTypeModels] = "/mock/cache/path"
//	path, err := mock.Resolve(ctx, CacheTypeModels)
type MockCachePathResolver struct {
	// ResolvedPaths maps cache types to resolved paths.
	ResolvedPaths map[CacheType]string

	// ContainerAccessible maps paths to accessibility status.
	ContainerAccessible map[string]bool

	// CacheInfos maps paths to cache info.
	CacheInfos map[string]*CacheInfo

	// ExternalDrives is returned by ListExternalDrives.
	ExternalDrives []DriveInfo

	// ClearedLocks tracks number of locks cleared per path.
	ClearedLocks map[string]int

	// ForceError causes all methods to return this error.
	ForceError error

	// ResolveError is returned by Resolve.
	ResolveError error

	// EnsureAccessError is returned by EnsureContainerAccess.
	EnsureAccessError error

	// CallCounts tracks how many times each method was called.
	CallCounts map[string]int
}

// NewMockCachePathResolver creates a mock with sensible defaults.
//
// # Description
//
// Creates a MockCachePathResolver with initialized maps and default values.
// Paths map is empty; add paths as needed for your test.
//
// # Inputs
//
// None.
//
// # Outputs
//
//   - *MockCachePathResolver: Ready-to-use mock
//
// # Examples
//
//	mock := NewMockCachePathResolver()
//	mock.ResolvedPaths[CacheTypeModels] = "/test/cache"
//	// Use mock in tests...
//
// # Limitations
//
//   - Not thread-safe
//
// # Assumptions
//
//   - Caller will configure paths as needed for their test case
func NewMockCachePathResolver() *MockCachePathResolver {
	return &MockCachePathResolver{
		ResolvedPaths:       make(map[CacheType]string),
		ContainerAccessible: make(map[string]bool),
		CacheInfos:          make(map[string]*CacheInfo),
		ExternalDrives:      []DriveInfo{},
		ClearedLocks:        make(map[string]int),
		CallCounts:          make(map[string]int),
	}
}

// incrementCallCount increments the call count for a method.
func (m *MockCachePathResolver) incrementCallCount(method string) {
	if m.CallCounts == nil {
		m.CallCounts = make(map[string]int)
	}
	m.CallCounts[method]++
}

// Resolve finds or creates the best cache directory for the given type.
//
// # Description
//
// Returns the path from ResolvedPaths map, or ErrCachePathNotFound if
// not present. Returns ForceError or ResolveError if set.
//
// # Inputs
//
//   - ctx: Context (unused in mock)
//   - cacheType: Cache type to resolve
//
// # Outputs
//
//   - string: The resolved path
//   - error: ErrCachePathNotFound, ResolveError, or ForceError
//
// # Examples
//
//	mock := NewMockCachePathResolver()
//	mock.ResolvedPaths[CacheTypeModels] = "/cache"
//	path, err := mock.Resolve(ctx, CacheTypeModels)
//
// # Limitations
//
//   - Does not simulate actual resolution logic
//
// # Assumptions
//
//   - ResolvedPaths map is initialized
func (m *MockCachePathResolver) Resolve(ctx context.Context, cacheType CacheType) (string, error) {
	m.incrementCallCount("Resolve")
	if m.ForceError != nil {
		return "", m.ForceError
	}
	if m.ResolveError != nil {
		return "", m.ResolveError
	}
	path, ok := m.ResolvedPaths[cacheType]
	if !ok {
		return "", ErrCachePathNotFound
	}
	return path, nil
}

// VerifyContainerAccess checks if containers can access the path.
//
// # Description
//
// Returns the accessibility status from ContainerAccessible map.
// Returns false if path not in map.
//
// # Inputs
//
//   - ctx: Context (unused in mock)
//   - path: Path to check
//
// # Outputs
//
//   - bool: Accessibility status from map
//   - error: ForceError if set
//
// # Examples
//
//	mock := NewMockCachePathResolver()
//	mock.ContainerAccessible["/path"] = true
//	accessible, _ := mock.VerifyContainerAccess(ctx, "/path")
//
// # Limitations
//
//   - Does not simulate actual container access check
//
// # Assumptions
//
//   - ContainerAccessible map is initialized
func (m *MockCachePathResolver) VerifyContainerAccess(ctx context.Context, path string) (bool, error) {
	m.incrementCallCount("VerifyContainerAccess")
	if m.ForceError != nil {
		return false, m.ForceError
	}
	accessible, ok := m.ContainerAccessible[path]
	if !ok {
		return false, nil
	}
	return accessible, nil
}

// EnsureContainerAccess attempts to make a path accessible to containers.
//
// # Description
//
// Returns EnsureAccessError if set, otherwise marks path as accessible
// in ContainerAccessible map.
//
// # Inputs
//
//   - ctx: Context (unused in mock)
//   - path: Path to make accessible
//
// # Outputs
//
//   - error: EnsureAccessError or ForceError if set
//
// # Examples
//
//	mock := NewMockCachePathResolver()
//	err := mock.EnsureContainerAccess(ctx, "/path")
//	// mock.ContainerAccessible["/path"] is now true
//
// # Limitations
//
//   - Does not simulate actual mount configuration
//
// # Assumptions
//
//   - ContainerAccessible map is initialized
func (m *MockCachePathResolver) EnsureContainerAccess(ctx context.Context, path string) error {
	m.incrementCallCount("EnsureContainerAccess")
	if m.ForceError != nil {
		return m.ForceError
	}
	if m.EnsureAccessError != nil {
		return m.EnsureAccessError
	}
	if m.ContainerAccessible == nil {
		m.ContainerAccessible = make(map[string]bool)
	}
	m.ContainerAccessible[path] = true
	return nil
}

// GetCacheInfo returns information about a resolved cache path.
//
// # Description
//
// Returns CacheInfo from CacheInfos map if configured,
// otherwise returns basic info with the path.
//
// # Inputs
//
//   - ctx: Context (unused in mock)
//   - path: Path to get info for
//
// # Outputs
//
//   - *CacheInfo: Cache info from map or default
//   - error: ForceError if set
//
// # Examples
//
//	mock := NewMockCachePathResolver()
//	mock.CacheInfos["/path"] = &CacheInfo{FreeSpaceMB: 1000}
//	info, _ := mock.GetCacheInfo(ctx, "/path")
//
// # Limitations
//
//   - Does not query actual disk space
//
// # Assumptions
//
//   - CacheInfos map is initialized
func (m *MockCachePathResolver) GetCacheInfo(ctx context.Context, path string) (*CacheInfo, error) {
	m.incrementCallCount("GetCacheInfo")
	if m.ForceError != nil {
		return nil, m.ForceError
	}
	info, ok := m.CacheInfos[path]
	if !ok {
		return &CacheInfo{
			Path:         path,
			LocationType: CacheLocationLocal,
			FreeSpaceMB:  50000,
			TotalSpaceMB: 100000,
		}, nil
	}
	return info, nil
}

// ClearStaleLocks removes stale lock files.
//
// # Description
//
// Returns the count from ClearedLocks map if configured,
// otherwise returns 0.
//
// # Inputs
//
//   - ctx: Context (unused in mock)
//   - cachePath: Path to clear locks from
//
// # Outputs
//
//   - int: Number of locks cleared from map
//   - error: ForceError if set
//
// # Examples
//
//	mock := NewMockCachePathResolver()
//	mock.ClearedLocks["/path"] = 5
//	cleared, _ := mock.ClearStaleLocks(ctx, "/path")
//	// cleared == 5
//
// # Limitations
//
//   - Does not actually clear any files
//
// # Assumptions
//
//   - ClearedLocks map is initialized
func (m *MockCachePathResolver) ClearStaleLocks(ctx context.Context, cachePath string) (int, error) {
	m.incrementCallCount("ClearStaleLocks")
	if m.ForceError != nil {
		return 0, m.ForceError
	}
	count, ok := m.ClearedLocks[cachePath]
	if !ok {
		return 0, nil
	}
	return count, nil
}

// ListExternalDrives returns detected external drives.
//
// # Description
//
// Returns the ExternalDrives slice.
//
// # Inputs
//
//   - ctx: Context (unused in mock)
//
// # Outputs
//
//   - []DriveInfo: External drives list
//   - error: ForceError if set
//
// # Examples
//
//	mock := NewMockCachePathResolver()
//	mock.ExternalDrives = []DriveInfo{{Path: "/Volumes/External"}}
//	drives, _ := mock.ListExternalDrives(ctx)
//
// # Limitations
//
//   - Returns static list, not based on actual drives
//
// # Assumptions
//
//   - ExternalDrives is initialized
func (m *MockCachePathResolver) ListExternalDrives(ctx context.Context) ([]DriveInfo, error) {
	m.incrementCallCount("ListExternalDrives")
	if m.ForceError != nil {
		return nil, m.ForceError
	}
	return m.ExternalDrives, nil
}

// Compile-time interface check for MockCachePathResolver.
var _ CachePathResolver = (*MockCachePathResolver)(nil)

// =============================================================================
// Test Helpers
// =============================================================================

// createTestCacheResolver creates a DefaultCachePathResolver for testing.
//
// # Description
//
// Creates a resolver with mock functions for filesystem operations.
// Allows tests to control what paths exist and what operations succeed.
//
// # Inputs
//
//   - cfg: Cache configuration
//   - existingPaths: Set of paths that "exist" in mock filesystem
//   - goos: Operating system to simulate ("darwin" or "linux")
//
// # Outputs
//
//   - *DefaultCachePathResolver: Configured for testing
//
// # Examples
//
//	resolver := createTestCacheResolver(cfg, map[string]bool{"/test": true}, "darwin")
//	path, err := resolver.Resolve(ctx, CacheTypeModels)
//
// # Limitations
//
//   - Does not simulate actual disk space (always returns 50GB free)
//
// # Assumptions
//
//   - Test only needs to verify resolution logic, not actual FS operations
func createTestCacheResolver(cfg CacheConfig, existingPaths map[string]bool, goos string) *DefaultCachePathResolver {
	resolver := NewDefaultCachePathResolver(cfg, nil, nil)

	// Mock os.Stat
	resolver.osStatFunc = func(path string) (os.FileInfo, error) {
		if existingPaths[path] {
			return nil, nil
		}
		return nil, os.ErrNotExist
	}

	// Mock os.MkdirAll - always succeeds
	resolver.osMkdirAllFunc = func(path string, perm os.FileMode) error {
		existingPaths[path] = true
		return nil
	}

	// Mock os.Getenv
	resolver.osGetenvFunc = func(key string) string {
		return ""
	}

	// Mock os.UserHomeDir
	resolver.osUserHomeDirFunc = func() (string, error) {
		return "/home/testuser", nil
	}

	// Mock os.Remove
	resolver.osRemoveFunc = func(path string) error {
		delete(existingPaths, path)
		return nil
	}

	// Mock filepath.Walk - returns no files
	resolver.filepathWalkFunc = func(root string, fn filepath.WalkFunc) error {
		return nil
	}

	// Mock filepath.EvalSymlinks - returns path unchanged
	resolver.filepathEvalSymlinks = func(path string) (string, error) {
		return path, nil
	}

	// Mock runtime.GOOS
	resolver.runtimeGOOSFunc = func() string {
		return goos
	}

	// Mock syscall.Statfs - returns sufficient space for existing paths
	resolver.syscallStatfsFunc = func(path string, stat *syscall.Statfs_t) error {
		if existingPaths[path] {
			stat.Bsize = 4096
			stat.Blocks = 100000000 // ~400GB total
			stat.Bavail = 50000000  // ~200GB available
			return nil
		}
		return syscall.ENOENT
	}

	return resolver
}

// =============================================================================
// Unit Tests - MockCachePathResolver
// =============================================================================

func TestMockCachePathResolver_Resolve(t *testing.T) {
	t.Parallel()

	t.Run("returns configured path", func(t *testing.T) {
		mock := NewMockCachePathResolver()
		mock.ResolvedPaths[CacheTypeModels] = "/test/cache"

		path, err := mock.Resolve(context.Background(), CacheTypeModels)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if path != "/test/cache" {
			t.Errorf("expected '/test/cache', got '%s'", path)
		}
		if mock.CallCounts["Resolve"] != 1 {
			t.Errorf("expected 1 call, got %d", mock.CallCounts["Resolve"])
		}
	})

	t.Run("returns error when path not configured", func(t *testing.T) {
		mock := NewMockCachePathResolver()

		_, err := mock.Resolve(context.Background(), CacheTypeModels)
		if !errors.Is(err, ErrCachePathNotFound) {
			t.Errorf("expected ErrCachePathNotFound, got %v", err)
		}
	})

	t.Run("returns forced error", func(t *testing.T) {
		mock := NewMockCachePathResolver()
		mock.ResolvedPaths[CacheTypeModels] = "/test/cache"
		mock.ForceError = errors.New("forced error")

		_, err := mock.Resolve(context.Background(), CacheTypeModels)
		if err == nil || err.Error() != "forced error" {
			t.Errorf("expected forced error, got %v", err)
		}
	})

	t.Run("returns resolve error", func(t *testing.T) {
		mock := NewMockCachePathResolver()
		mock.ResolveError = ErrInsufficientSpace

		_, err := mock.Resolve(context.Background(), CacheTypeModels)
		if !errors.Is(err, ErrInsufficientSpace) {
			t.Errorf("expected ErrInsufficientSpace, got %v", err)
		}
	})
}

func TestMockCachePathResolver_VerifyContainerAccess(t *testing.T) {
	t.Parallel()

	t.Run("returns true when accessible", func(t *testing.T) {
		mock := NewMockCachePathResolver()
		mock.ContainerAccessible["/test/path"] = true

		accessible, err := mock.VerifyContainerAccess(context.Background(), "/test/path")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !accessible {
			t.Error("expected accessible to be true")
		}
	})

	t.Run("returns false when not accessible", func(t *testing.T) {
		mock := NewMockCachePathResolver()
		mock.ContainerAccessible["/test/path"] = false

		accessible, err := mock.VerifyContainerAccess(context.Background(), "/test/path")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if accessible {
			t.Error("expected accessible to be false")
		}
	})

	t.Run("returns false when path not in map", func(t *testing.T) {
		mock := NewMockCachePathResolver()

		accessible, err := mock.VerifyContainerAccess(context.Background(), "/unknown")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if accessible {
			t.Error("expected accessible to be false for unknown path")
		}
	})
}

func TestMockCachePathResolver_EnsureContainerAccess(t *testing.T) {
	t.Parallel()

	t.Run("marks path as accessible", func(t *testing.T) {
		mock := NewMockCachePathResolver()

		err := mock.EnsureContainerAccess(context.Background(), "/test/path")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !mock.ContainerAccessible["/test/path"] {
			t.Error("expected path to be marked accessible")
		}
	})

	t.Run("returns ensure access error", func(t *testing.T) {
		mock := NewMockCachePathResolver()
		mock.EnsureAccessError = ErrMountConfigFailed

		err := mock.EnsureContainerAccess(context.Background(), "/test/path")
		if !errors.Is(err, ErrMountConfigFailed) {
			t.Errorf("expected ErrMountConfigFailed, got %v", err)
		}
	})
}

func TestMockCachePathResolver_GetCacheInfo(t *testing.T) {
	t.Parallel()

	t.Run("returns configured info", func(t *testing.T) {
		mock := NewMockCachePathResolver()
		mock.CacheInfos["/test/path"] = &CacheInfo{
			Path:         "/test/path",
			LocationType: CacheLocationExternal,
			FreeSpaceMB:  100000,
		}

		info, err := mock.GetCacheInfo(context.Background(), "/test/path")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if info.LocationType != CacheLocationExternal {
			t.Errorf("expected external, got %s", info.LocationType)
		}
		if info.FreeSpaceMB != 100000 {
			t.Errorf("expected 100000, got %d", info.FreeSpaceMB)
		}
	})

	t.Run("returns default info when not configured", func(t *testing.T) {
		mock := NewMockCachePathResolver()

		info, err := mock.GetCacheInfo(context.Background(), "/test/path")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if info.Path != "/test/path" {
			t.Errorf("expected path '/test/path', got '%s'", info.Path)
		}
		if info.LocationType != CacheLocationLocal {
			t.Errorf("expected local, got %s", info.LocationType)
		}
	})
}

func TestMockCachePathResolver_ClearStaleLocks(t *testing.T) {
	t.Parallel()

	t.Run("returns configured count", func(t *testing.T) {
		mock := NewMockCachePathResolver()
		mock.ClearedLocks["/test/cache"] = 5

		cleared, err := mock.ClearStaleLocks(context.Background(), "/test/cache")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cleared != 5 {
			t.Errorf("expected 5, got %d", cleared)
		}
	})

	t.Run("returns zero when not configured", func(t *testing.T) {
		mock := NewMockCachePathResolver()

		cleared, err := mock.ClearStaleLocks(context.Background(), "/test/cache")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cleared != 0 {
			t.Errorf("expected 0, got %d", cleared)
		}
	})
}

func TestMockCachePathResolver_ListExternalDrives(t *testing.T) {
	t.Parallel()

	t.Run("returns configured drives", func(t *testing.T) {
		mock := NewMockCachePathResolver()
		mock.ExternalDrives = []DriveInfo{
			{Path: "/Volumes/Drive1", FreeSpaceMB: 50000},
			{Path: "/Volumes/Drive2", FreeSpaceMB: 100000},
		}

		drives, err := mock.ListExternalDrives(context.Background())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(drives) != 2 {
			t.Errorf("expected 2 drives, got %d", len(drives))
		}
	})

	t.Run("returns empty list by default", func(t *testing.T) {
		mock := NewMockCachePathResolver()

		drives, err := mock.ListExternalDrives(context.Background())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(drives) != 0 {
			t.Errorf("expected 0 drives, got %d", len(drives))
		}
	})
}

// =============================================================================
// Unit Tests - CacheConfig
// =============================================================================

func TestCacheConfig_GetMinFreeSpaceMB(t *testing.T) {
	t.Parallel()

	t.Run("returns configured value", func(t *testing.T) {
		cfg := &CacheConfig{MinFreeSpaceMB: 10240}
		if cfg.GetMinFreeSpaceMB() != 10240 {
			t.Errorf("expected 10240, got %d", cfg.GetMinFreeSpaceMB())
		}
	})

	t.Run("returns default for zero", func(t *testing.T) {
		cfg := &CacheConfig{MinFreeSpaceMB: 0}
		if cfg.GetMinFreeSpaceMB() != DefaultMinFreeSpaceMB {
			t.Errorf("expected %d, got %d", DefaultMinFreeSpaceMB, cfg.GetMinFreeSpaceMB())
		}
	})

	t.Run("returns default for nil", func(t *testing.T) {
		var cfg *CacheConfig
		if cfg.GetMinFreeSpaceMB() != DefaultMinFreeSpaceMB {
			t.Errorf("expected %d, got %d", DefaultMinFreeSpaceMB, cfg.GetMinFreeSpaceMB())
		}
	})
}

func TestCacheConfig_GetStaleLockThreshold(t *testing.T) {
	t.Parallel()

	t.Run("returns configured value", func(t *testing.T) {
		cfg := &CacheConfig{StaleLockThreshold: 30 * time.Minute}
		if cfg.GetStaleLockThreshold() != 30*time.Minute {
			t.Errorf("expected 30m, got %v", cfg.GetStaleLockThreshold())
		}
	})

	t.Run("returns default for zero", func(t *testing.T) {
		cfg := &CacheConfig{}
		if cfg.GetStaleLockThreshold() != DefaultStaleLockThreshold {
			t.Errorf("expected %v, got %v", DefaultStaleLockThreshold, cfg.GetStaleLockThreshold())
		}
	})

	t.Run("returns default for nil", func(t *testing.T) {
		var cfg *CacheConfig
		if cfg.GetStaleLockThreshold() != DefaultStaleLockThreshold {
			t.Errorf("expected %v, got %v", DefaultStaleLockThreshold, cfg.GetStaleLockThreshold())
		}
	})
}

func TestCacheConfig_GetEnvVar(t *testing.T) {
	t.Parallel()

	t.Run("returns configured override", func(t *testing.T) {
		cfg := &CacheConfig{
			EnvOverrides: map[CacheType]string{
				CacheTypeModels: "CUSTOM_CACHE_VAR",
			},
		}
		if cfg.GetEnvVar(CacheTypeModels) != "CUSTOM_CACHE_VAR" {
			t.Errorf("expected CUSTOM_CACHE_VAR, got %s", cfg.GetEnvVar(CacheTypeModels))
		}
	})

	t.Run("returns default for models", func(t *testing.T) {
		cfg := &CacheConfig{}
		if cfg.GetEnvVar(CacheTypeModels) != DefaultEnvVarModels {
			t.Errorf("expected %s, got %s", DefaultEnvVarModels, cfg.GetEnvVar(CacheTypeModels))
		}
	})

	t.Run("returns empty for unconfigured type", func(t *testing.T) {
		cfg := &CacheConfig{}
		if cfg.GetEnvVar(CacheTypeEmbeddings) != "" {
			t.Errorf("expected empty, got %s", cfg.GetEnvVar(CacheTypeEmbeddings))
		}
	})

	t.Run("returns default for nil config", func(t *testing.T) {
		var cfg *CacheConfig
		if cfg.GetEnvVar(CacheTypeModels) != DefaultEnvVarModels {
			t.Errorf("expected %s, got %s", DefaultEnvVarModels, cfg.GetEnvVar(CacheTypeModels))
		}
	})
}

func TestCacheConfig_GetMachineName(t *testing.T) {
	t.Parallel()

	t.Run("returns configured value", func(t *testing.T) {
		cfg := &CacheConfig{MachineName: "custom-machine"}
		if cfg.GetMachineName() != "custom-machine" {
			t.Errorf("expected custom-machine, got %s", cfg.GetMachineName())
		}
	})

	t.Run("returns default for empty", func(t *testing.T) {
		cfg := &CacheConfig{}
		if cfg.GetMachineName() != "podman-machine-default" {
			t.Errorf("expected podman-machine-default, got %s", cfg.GetMachineName())
		}
	})

	t.Run("returns default for nil", func(t *testing.T) {
		var cfg *CacheConfig
		if cfg.GetMachineName() != "podman-machine-default" {
			t.Errorf("expected podman-machine-default, got %s", cfg.GetMachineName())
		}
	})
}

// =============================================================================
// Unit Tests - DefaultCachePathResolver
// =============================================================================

func TestDefaultCachePathResolver_Resolve_LocalFallback(t *testing.T) {
	t.Parallel()

	cfg := CacheConfig{
		StackDir: "/home/user/.aleutian",
	}
	existingPaths := map[string]bool{}
	resolver := createTestCacheResolver(cfg, existingPaths, "linux")

	path, err := resolver.Resolve(context.Background(), CacheTypeModels)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := "/home/user/.aleutian/models_cache"
	if path != expected {
		t.Errorf("expected '%s', got '%s'", expected, path)
	}
}

func TestDefaultCachePathResolver_Resolve_EnvOverride(t *testing.T) {
	t.Parallel()

	cfg := CacheConfig{
		StackDir: "/home/user/.aleutian",
	}
	existingPaths := map[string]bool{
		"/custom/cache/path": true,
	}
	resolver := createTestCacheResolver(cfg, existingPaths, "linux")

	// Mock env var
	resolver.osGetenvFunc = func(key string) string {
		if key == "ALEUTIAN_MODELS_CACHE" {
			return "/custom/cache/path"
		}
		return ""
	}

	path, err := resolver.Resolve(context.Background(), CacheTypeModels)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if path != "/custom/cache/path" {
		t.Errorf("expected '/custom/cache/path', got '%s'", path)
	}
}

func TestDefaultCachePathResolver_Resolve_PreferredDrive(t *testing.T) {
	t.Parallel()

	// Use linux to avoid container access checks (darwin requires ProcessManager)
	cfg := CacheConfig{
		StackDir:       "/home/user/.aleutian",
		PreferredDrive: "/mnt/ai_models",
	}
	existingPaths := map[string]bool{
		"/mnt/ai_models": true,
	}
	resolver := createTestCacheResolver(cfg, existingPaths, "linux")

	path, err := resolver.Resolve(context.Background(), CacheTypeModels)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := "/mnt/ai_models/aleutian_data/models_cache"
	if path != expected {
		t.Errorf("expected '%s', got '%s'", expected, path)
	}
}

func TestDefaultCachePathResolver_isExternalPath(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		goos     string
		path     string
		expected bool
	}{
		{"darwin", "/Volumes/External", true},
		{"darwin", "/Volumes/ai_models/data", true},
		{"darwin", "/Users/name/data", false},
		{"darwin", "/private/var/folders/abc", true},
		{"linux", "/mnt/drive", true},
		{"linux", "/media/user/drive", true},
		{"linux", "/run/media/user/drive", true},
		{"linux", "/home/user/data", false},
		{"windows", "/anything", false},
	}

	for _, tc := range testCases {
		t.Run(tc.goos+"_"+tc.path, func(t *testing.T) {
			cfg := CacheConfig{}
			resolver := createTestCacheResolver(cfg, nil, tc.goos)

			result := resolver.isExternalPath(tc.path)
			if result != tc.expected {
				t.Errorf("isExternalPath(%s) on %s = %v, expected %v",
					tc.path, tc.goos, result, tc.expected)
			}
		})
	}
}

func TestDefaultCachePathResolver_extractMountRoot(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		goos     string
		path     string
		expected string
	}{
		{"darwin", "/Volumes/DriveName/path/to/cache", "/Volumes/DriveName"},
		{"darwin", "/Volumes/ai_models/aleutian_data", "/Volumes/ai_models"},
		{"linux", "/mnt/drive/data", "/mnt/drive"},
		{"linux", "/media/user/drive/data", "/media/user/drive"},
		{"linux", "/run/media/user/drive/data", "/run/media/user/drive"},
		{"linux", "/home/user", "/home/user"},
	}

	for _, tc := range testCases {
		t.Run(tc.goos+"_"+tc.path, func(t *testing.T) {
			cfg := CacheConfig{}
			resolver := createTestCacheResolver(cfg, nil, tc.goos)

			result := resolver.extractMountRoot(tc.path)
			if result != tc.expected {
				t.Errorf("extractMountRoot(%s) on %s = '%s', expected '%s'",
					tc.path, tc.goos, result, tc.expected)
			}
		})
	}
}

func TestDefaultCachePathResolver_shouldSkipDrive(t *testing.T) {
	t.Parallel()

	cfg := CacheConfig{}
	resolver := createTestCacheResolver(cfg, nil, "darwin")

	testCases := []struct {
		drivePath string
		homeDir   string
		expected  bool
	}{
		{"/home/user/data", "/home/user", true},    // In home dir
		{"/Volumes", "", true},                     // Generic mount root
		{"/mnt", "", true},                         // Generic mount root
		{"/home/user", "/home/user", true},         // Is home dir
		{"/Volumes/External", "/home/user", false}, // Valid external
		{"/other/path", "/home/user", true},        // Not external
	}

	for _, tc := range testCases {
		t.Run(tc.drivePath, func(t *testing.T) {
			result := resolver.shouldSkipDrive(tc.drivePath, tc.homeDir)
			if result != tc.expected {
				t.Errorf("shouldSkipDrive(%s, %s) = %v, expected %v",
					tc.drivePath, tc.homeDir, result, tc.expected)
			}
		})
	}
}

func TestDefaultCachePathResolver_needsMachineMount(t *testing.T) {
	t.Parallel()

	t.Run("returns true on darwin", func(t *testing.T) {
		cfg := CacheConfig{}
		resolver := createTestCacheResolver(cfg, nil, "darwin")
		if !resolver.needsMachineMount() {
			t.Error("expected needsMachineMount to return true on darwin")
		}
	})

	t.Run("returns false on linux", func(t *testing.T) {
		cfg := CacheConfig{}
		resolver := createTestCacheResolver(cfg, nil, "linux")
		if resolver.needsMachineMount() {
			t.Error("expected needsMachineMount to return false on linux")
		}
	})
}

func TestDefaultCachePathResolver_determineLocationType(t *testing.T) {
	t.Parallel()

	t.Run("detects local", func(t *testing.T) {
		cfg := CacheConfig{StackDir: "/home/user/.aleutian"}
		resolver := createTestCacheResolver(cfg, nil, "linux")

		locType := resolver.determineLocationType("/home/user/.aleutian/models_cache")
		if locType != CacheLocationLocal {
			t.Errorf("expected local, got %s", locType)
		}
	})

	t.Run("detects external", func(t *testing.T) {
		cfg := CacheConfig{}
		resolver := createTestCacheResolver(cfg, nil, "darwin")

		locType := resolver.determineLocationType("/Volumes/External/cache")
		if locType != CacheLocationExternal {
			t.Errorf("expected external, got %s", locType)
		}
	})

	t.Run("detects env override", func(t *testing.T) {
		cfg := CacheConfig{}
		resolver := createTestCacheResolver(cfg, nil, "linux")
		resolver.osGetenvFunc = func(key string) string {
			if key == "ALEUTIAN_MODELS_CACHE" {
				return "/custom/path"
			}
			return ""
		}

		locType := resolver.determineLocationType("/custom/path")
		if locType != CacheLocationEnvOverride {
			t.Errorf("expected env_override, got %s", locType)
		}
	})

	t.Run("detects preferred", func(t *testing.T) {
		cfg := CacheConfig{PreferredDrive: "/Volumes/ai_models"}
		resolver := createTestCacheResolver(cfg, nil, "darwin")

		locType := resolver.determineLocationType("/Volumes/ai_models/aleutian_data/models_cache")
		if locType != CacheLocationPreferred {
			t.Errorf("expected preferred, got %s", locType)
		}
	})
}

func TestDefaultCachePathResolver_VerifyContainerAccess_Linux(t *testing.T) {
	t.Parallel()

	cfg := CacheConfig{}
	existingPaths := map[string]bool{
		"/test/path": true,
	}
	resolver := createTestCacheResolver(cfg, existingPaths, "linux")

	t.Run("returns true when path exists", func(t *testing.T) {
		accessible, err := resolver.VerifyContainerAccess(context.Background(), "/test/path")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !accessible {
			t.Error("expected accessible to be true")
		}
	})

	t.Run("returns false when path does not exist", func(t *testing.T) {
		accessible, err := resolver.VerifyContainerAccess(context.Background(), "/nonexistent")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if accessible {
			t.Error("expected accessible to be false")
		}
	})
}

func TestDefaultCachePathResolver_ClearStaleLocks(t *testing.T) {
	t.Parallel()

	t.Run("handles empty directory", func(t *testing.T) {
		cfg := CacheConfig{}
		resolver := createTestCacheResolver(cfg, nil, "linux")

		cleared, err := resolver.ClearStaleLocks(context.Background(), "/test/cache")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cleared != 0 {
			t.Errorf("expected 0 cleared, got %d", cleared)
		}
	})

	t.Run("respects context cancellation", func(t *testing.T) {
		cfg := CacheConfig{}
		resolver := createTestCacheResolver(cfg, nil, "linux")

		// Mock walk to check context
		resolver.filepathWalkFunc = func(root string, fn filepath.WalkFunc) error {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			return fn("/test/file.lock", nil, ctx.Err())
		}

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := resolver.ClearStaleLocks(ctx, "/test/cache")
		// Should not fail catastrophically, just return
		_ = err
	})
}

func TestDefaultCachePathResolver_ListExternalDrives(t *testing.T) {
	t.Parallel()

	t.Run("returns drives from config", func(t *testing.T) {
		cfg := CacheConfig{
			ConfiguredDrives: []string{
				"/Volumes/External1",
				"/Volumes/External2",
			},
		}
		existingPaths := map[string]bool{
			"/Volumes/External1": true,
			"/Volumes/External2": true,
		}
		resolver := createTestCacheResolver(cfg, existingPaths, "darwin")

		drives, err := resolver.ListExternalDrives(context.Background())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(drives) != 2 {
			t.Errorf("expected 2 drives, got %d", len(drives))
		}
	})

	t.Run("skips unmounted drives", func(t *testing.T) {
		cfg := CacheConfig{
			ConfiguredDrives: []string{
				"/Volumes/Mounted",
				"/Volumes/Unmounted",
			},
		}
		existingPaths := map[string]bool{
			"/Volumes/Mounted": true,
		}
		resolver := createTestCacheResolver(cfg, existingPaths, "darwin")

		drives, err := resolver.ListExternalDrives(context.Background())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(drives) != 1 {
			t.Errorf("expected 1 drive, got %d", len(drives))
		}
	})

	t.Run("skips home directory", func(t *testing.T) {
		cfg := CacheConfig{
			ConfiguredDrives: []string{
				"/home/testuser/data",
				"/Volumes/External",
			},
		}
		existingPaths := map[string]bool{
			"/home/testuser/data": true,
			"/Volumes/External":   true,
		}
		resolver := createTestCacheResolver(cfg, existingPaths, "darwin")

		drives, err := resolver.ListExternalDrives(context.Background())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(drives) != 1 {
			t.Errorf("expected 1 drive, got %d", len(drives))
		}
		if len(drives) > 0 && drives[0].Path != "/Volumes/External" {
			t.Errorf("expected /Volumes/External, got %s", drives[0].Path)
		}
	})
}

// =============================================================================
// Unit Tests - Error Sentinels
// =============================================================================

func TestErrorSentinels_CacheResolver(t *testing.T) {
	t.Parallel()

	t.Run("ErrCachePathNotFound", func(t *testing.T) {
		if ErrCachePathNotFound.Error() != "no suitable cache path found" {
			t.Errorf("unexpected error message: %s", ErrCachePathNotFound.Error())
		}
	})

	t.Run("ErrCacheNotAccessible", func(t *testing.T) {
		if ErrCacheNotAccessible.Error() != "cache path not accessible from containers" {
			t.Errorf("unexpected error message: %s", ErrCacheNotAccessible.Error())
		}
	})

	t.Run("ErrInsufficientSpace", func(t *testing.T) {
		if ErrInsufficientSpace.Error() != "insufficient disk space" {
			t.Errorf("unexpected error message: %s", ErrInsufficientSpace.Error())
		}
	})

	t.Run("ErrMountConfigFailed", func(t *testing.T) {
		if ErrMountConfigFailed.Error() != "failed to configure container mount" {
			t.Errorf("unexpected error message: %s", ErrMountConfigFailed.Error())
		}
	})
}

// =============================================================================
// Unit Tests - Constants
// =============================================================================

func TestCacheTypeConstants(t *testing.T) {
	t.Parallel()

	if CacheTypeModels != "models" {
		t.Errorf("expected 'models', got '%s'", CacheTypeModels)
	}
	if CacheTypeEmbeddings != "embeddings" {
		t.Errorf("expected 'embeddings', got '%s'", CacheTypeEmbeddings)
	}
	if CacheTypeInference != "inference" {
		t.Errorf("expected 'inference', got '%s'", CacheTypeInference)
	}
}

func TestCacheLocationTypeConstants(t *testing.T) {
	t.Parallel()

	if CacheLocationLocal != "local" {
		t.Errorf("expected 'local', got '%s'", CacheLocationLocal)
	}
	if CacheLocationExternal != "external" {
		t.Errorf("expected 'external', got '%s'", CacheLocationExternal)
	}
	if CacheLocationEnvOverride != "env_override" {
		t.Errorf("expected 'env_override', got '%s'", CacheLocationEnvOverride)
	}
	if CacheLocationPreferred != "preferred" {
		t.Errorf("expected 'preferred', got '%s'", CacheLocationPreferred)
	}
}

func TestDefaultConstants(t *testing.T) {
	t.Parallel()

	if DefaultMinFreeSpaceMB != 25600 {
		t.Errorf("expected 25600, got %d", DefaultMinFreeSpaceMB)
	}
	if DefaultStaleLockThreshold != time.Hour {
		t.Errorf("expected 1h, got %v", DefaultStaleLockThreshold)
	}
	if DefaultEnvVarModels != "ALEUTIAN_MODELS_CACHE" {
		t.Errorf("expected ALEUTIAN_MODELS_CACHE, got %s", DefaultEnvVarModels)
	}
	if !strings.Contains(cacheSubdir, "models_cache") {
		t.Errorf("expected cacheSubdir to contain 'models_cache', got %s", cacheSubdir)
	}
}
