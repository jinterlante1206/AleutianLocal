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
Package main provides CachePathResolver for optimal cache location resolution.

CachePathResolver determines the best location for model caches (HuggingFace,
Ollama) with support for external drive detection, container accessibility
verification, and platform-specific path handling.

# Security Context

This is a MODERATE-RISK component. It handles filesystem paths and may trigger
Podman machine restarts. Improper path resolution could lead to data being
written to unexpected locations.

# Resolution Priority

Caches are resolved using this priority:

 1. Environment variable override (ALEUTIAN_MODELS_CACHE)
 2. User-designated preferred drive (config.PreferredDrive)
 3. External drive with existing cache (aleutian_data/models_cache)
 4. External drive with most free space
 5. Local stack directory fallback

# Platform Behavior

  - macOS: External paths under /Volumes require Podman VM mounts
  - Linux: Direct filesystem access, no VM mounts needed

# Design Principles

  - Interface-first design for testability
  - Dependencies injected (ProcessManager, UserPrompter)
  - Thread-safe for concurrent use
  - Single responsibility per method
*/
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/jinterlante1206/AleutianLocal/cmd/aleutian/internal/infra/process"
	"github.com/jinterlante1206/AleutianLocal/cmd/aleutian/internal/util"
)

// -----------------------------------------------------------------------------
// Error Sentinel Values
// -----------------------------------------------------------------------------

// ErrCachePathNotFound is returned when no suitable cache path can be resolved.
// This typically means all configured paths are inaccessible or don't exist.
var ErrCachePathNotFound = errors.New("no suitable cache path found")

// ErrCacheNotAccessible is returned when the cache path exists but cannot
// be accessed by containers (e.g., missing Podman VM mount on macOS).
var ErrCacheNotAccessible = errors.New("cache path not accessible from containers")

// ErrInsufficientSpace is returned when the cache path has insufficient
// free disk space (below MinFreeSpaceMB threshold).
var ErrInsufficientSpace = errors.New("insufficient disk space")

// ErrMountConfigFailed is returned when Podman machine mount configuration fails.
// This can happen if the user declines or if podman commands fail.
var ErrMountConfigFailed = errors.New("failed to configure container mount")

// -----------------------------------------------------------------------------
// Cache Type Constants
// -----------------------------------------------------------------------------

// CacheType identifies the type of cache being resolved.
type CacheType string

const (
	// CacheTypeModels is for ML model files (HuggingFace, Ollama).
	CacheTypeModels CacheType = "models"

	// CacheTypeEmbeddings is for embedding vector caches (future use).
	CacheTypeEmbeddings CacheType = "embeddings"

	// CacheTypeInference is for inference artifacts and KV caches (future use).
	CacheTypeInference CacheType = "inference"
)

// -----------------------------------------------------------------------------
// Cache Location Type Constants
// -----------------------------------------------------------------------------

// CacheLocationType identifies where a cache is stored.
type CacheLocationType string

const (
	// CacheLocationLocal is within the stack directory.
	CacheLocationLocal CacheLocationType = "local"

	// CacheLocationExternal is on an external/removable drive.
	CacheLocationExternal CacheLocationType = "external"

	// CacheLocationEnvOverride is from environment variable.
	CacheLocationEnvOverride CacheLocationType = "env_override"

	// CacheLocationPreferred is from user-designated preferred drive.
	CacheLocationPreferred CacheLocationType = "preferred"
)

// -----------------------------------------------------------------------------
// Default Values
// -----------------------------------------------------------------------------

const (
	// DefaultMinFreeSpaceMB is the default minimum free space (25 GB).
	DefaultMinFreeSpaceMB int64 = 25600

	// DefaultStaleLockThreshold is how old a lock must be to be stale (1 hour).
	DefaultStaleLockThreshold = 1 * time.Hour

	// DefaultEnvVarModels is the environment variable for model cache override.
	DefaultEnvVarModels = "ALEUTIAN_MODELS_CACHE"

	// cacheSubdir is the standard subdirectory structure for caches.
	cacheSubdir = "aleutian_data/models_cache"
)

// -----------------------------------------------------------------------------
// Interface Definition
// -----------------------------------------------------------------------------

// CachePathResolver determines the optimal model cache location.
//
// # Description
//
// This interface abstracts cache path resolution from the underlying
// filesystem and container runtime. It supports multiple cache types
// with automatic external drive detection and container accessibility.
//
// # Security
//
//   - Validates paths to prevent directory traversal
//   - Checks disk permissions before use
//   - Clears stale locks to prevent DoS
//
// # Thread Safety
//
// Implementations must be safe for concurrent use.
type CachePathResolver interface {
	// Resolve finds or creates the best cache directory for the given type.
	//
	// # Description
	//
	// Resolves the optimal cache path using this priority:
	//   1. Environment variable override (if set)
	//   2. User-designated preferred drive (if set)
	//   3. External drive with existing cache (auto-detected)
	//   4. External drive with most free space
	//   5. Default location (stackDir/models_cache)
	//
	// Creates the directory if it doesn't exist.
	//
	// # Inputs
	//
	//   - ctx: Context for cancellation and timeout control
	//   - cacheType: Type of cache to resolve (models, embeddings, inference)
	//
	// # Outputs
	//
	//   - string: Resolved absolute path to cache directory
	//   - error: ErrCachePathNotFound, ErrCacheNotAccessible, or other errors
	//
	// # Examples
	//
	//   path, err := resolver.Resolve(ctx, CacheTypeModels)
	//   if errors.Is(err, ErrCachePathNotFound) {
	//       log.Fatal("No suitable cache location found")
	//   }
	//   fmt.Printf("Using cache at: %s\n", path)
	//
	// # Limitations
	//
	//   - May trigger Podman machine restart on macOS
	//   - External drives must be mounted before resolution
	//   - Only CacheTypeModels is currently supported
	//
	// # Assumptions
	//
	//   - Configured drives list is populated from aleutian.yaml
	//   - Podman machine exists and is initialized if on macOS
	//   - Caller handles the resolved path appropriately
	Resolve(ctx context.Context, cacheType CacheType) (string, error)

	// VerifyContainerAccess checks if containers can access the path.
	//
	// # Description
	//
	// On macOS, verifies the path is mounted in the Podman VM by
	// attempting to list files from within a container.
	// On Linux, verifies the path exists and has correct permissions.
	//
	// # Inputs
	//
	//   - ctx: Context for cancellation and timeout control
	//   - path: Absolute filesystem path to verify
	//
	// # Outputs
	//
	//   - bool: True if containers can access the path
	//   - error: If verification process fails (not if inaccessible)
	//
	// # Examples
	//
	//   accessible, err := resolver.VerifyContainerAccess(ctx, "/Volumes/ai_models")
	//   if err != nil {
	//       return fmt.Errorf("verification failed: %w", err)
	//   }
	//   if !accessible {
	//       fmt.Println("Path not accessible from containers")
	//   }
	//
	// # Limitations
	//
	//   - On macOS, requires running Podman machine
	//   - Spawns a temporary container for verification
	//
	// # Assumptions
	//
	//   - Path exists on the host filesystem
	//   - Podman is installed and functional
	VerifyContainerAccess(ctx context.Context, path string) (bool, error)

	// EnsureContainerAccess attempts to make a path accessible to containers.
	//
	// # Description
	//
	// On macOS, adds a volume mount to the Podman machine if needed,
	// which requires stopping and restarting the machine.
	// On Linux, this is typically a no-op since containers have direct access.
	//
	// # Inputs
	//
	//   - ctx: Context for cancellation and timeout control
	//   - path: Absolute filesystem path to make accessible
	//
	// # Outputs
	//
	//   - error: ErrMountConfigFailed if unable to configure, nil on success
	//
	// # Examples
	//
	//   if err := resolver.EnsureContainerAccess(ctx, "/Volumes/ai_models"); err != nil {
	//       if errors.Is(err, ErrMountConfigFailed) {
	//           fmt.Println("User declined or mount failed")
	//       }
	//       return err
	//   }
	//   fmt.Println("Path is now accessible to containers")
	//
	// # Limitations
	//
	//   - May restart Podman machine (interrupts running containers)
	//   - Requires user confirmation if AutoFixMounts is false
	//   - Mount configuration persists across machine restarts
	//
	// # Assumptions
	//
	//   - User has permission to modify Podman machine config
	//   - Podman machine can be stopped and started
	EnsureContainerAccess(ctx context.Context, path string) error

	// GetCacheInfo returns information about a resolved cache path.
	//
	// # Description
	//
	// Returns metadata about the cache including location type,
	// available space, total space, and container accessibility status.
	//
	// # Inputs
	//
	//   - ctx: Context for cancellation and timeout control
	//   - path: Absolute cache path to inspect
	//
	// # Outputs
	//
	//   - *CacheInfo: Cache metadata structure with all fields populated
	//   - error: If inspection fails (e.g., path doesn't exist)
	//
	// # Examples
	//
	//   info, err := resolver.GetCacheInfo(ctx, "/Volumes/ai_models/aleutian_data/models_cache")
	//   if err != nil {
	//       return fmt.Errorf("inspection failed: %w", err)
	//   }
	//   fmt.Printf("Location: %s\n", info.LocationType)
	//   fmt.Printf("Free space: %d MB\n", info.FreeSpaceMB)
	//   fmt.Printf("Container accessible: %v\n", info.IsContainerAccessible)
	//
	// # Limitations
	//
	//   - Disk space is a snapshot; may change during operation
	//   - DriveLabel field may be empty on some platforms
	//
	// # Assumptions
	//
	//   - Path exists and is accessible to the current user
	GetCacheInfo(ctx context.Context, path string) (*CacheInfo, error)

	// ClearStaleLocks removes stale HuggingFace lock files.
	//
	// # Description
	//
	// HuggingFace creates .lock files during model downloads. If a process
	// is killed mid-download, these locks become stale and block future
	// downloads. This method removes locks older than StaleLockThreshold.
	//
	// # Inputs
	//
	//   - ctx: Context for cancellation and timeout control
	//   - cachePath: Root path to scan for stale lock files
	//
	// # Outputs
	//
	//   - int: Number of lock files successfully removed
	//   - error: If directory walk fails (partial cleanup may have occurred)
	//
	// # Examples
	//
	//   cleared, err := resolver.ClearStaleLocks(ctx, "/path/to/cache")
	//   if err != nil {
	//       log.Printf("Warning: lock cleanup had errors: %v", err)
	//   }
	//   if cleared > 0 {
	//       fmt.Printf("Cleared %d stale lock files\n", cleared)
	//   }
	//
	// # Limitations
	//
	//   - Only clears locks older than StaleLockThreshold config value
	//   - May miss locks in inaccessible subdirectories
	//   - Does not distinguish between HuggingFace and other .lock files
	//
	// # Assumptions
	//
	//   - Lock files follow *.lock naming pattern
	//   - Locks older than threshold are safe to remove
	ClearStaleLocks(ctx context.Context, cachePath string) (int, error)

	// ListExternalDrives returns detected external drives suitable for caching.
	//
	// # Description
	//
	// Scans the configured drives list and returns those that are:
	// external (not the home directory), currently mounted, and
	// optionally have sufficient free space.
	//
	// # Inputs
	//
	//   - ctx: Context for cancellation and timeout control
	//
	// # Outputs
	//
	//   - []DriveInfo: Slice of drive information structs (may be empty)
	//   - error: If drive enumeration fails
	//
	// # Examples
	//
	//   drives, err := resolver.ListExternalDrives(ctx)
	//   if err != nil {
	//       return fmt.Errorf("drive detection failed: %w", err)
	//   }
	//   for _, d := range drives {
	//       fmt.Printf("Drive: %s\n", d.Path)
	//       fmt.Printf("  Free: %d MB\n", d.FreeSpaceMB)
	//       fmt.Printf("  Has cache: %v\n", d.HasExistingCache)
	//   }
	//
	// # Limitations
	//
	//   - Only scans drives listed in ConfiguredDrives config
	//   - Does not discover drives not in the configuration
	//   - Does not auto-mount disconnected drives
	//
	// # Assumptions
	//
	//   - Drives are already mounted by the operating system
	//   - ConfiguredDrives is populated from aleutian.yaml
	ListExternalDrives(ctx context.Context) ([]DriveInfo, error)
}

// -----------------------------------------------------------------------------
// Supporting Types
// -----------------------------------------------------------------------------

// CacheConfig provides configuration for cache resolution.
//
// # Description
//
// Contains all settings needed for cache path resolution including
// drive lists, space thresholds, and behavior flags. This struct
// is passed to the resolver constructor and copied internally.
type CacheConfig struct {
	// StackDir is the base directory for the Aleutian stack.
	// The default cache location is {StackDir}/models_cache.
	// This field is required.
	StackDir string

	// ConfiguredDrives is the list of drives from aleutian.yaml.
	// External drives are auto-detected from this list during resolution.
	// Example: []string{"/Volumes/ai_models", "/Volumes/backup"}
	ConfiguredDrives []string

	// PreferredDrive is the user-designated drive for caching.
	// Takes highest priority in external drive selection after env override.
	// Example: "/Volumes/ai_models"
	PreferredDrive string

	// MachineName is the Podman machine name (macOS only).
	// Used for adding volume mounts. Default: "podman-machine-default"
	MachineName string

	// MinFreeSpaceMB is minimum required free space in megabytes.
	// Default: 25600 MB (25 GB)
	MinFreeSpaceMB int64

	// StaleLockThreshold is how old a lock file must be to be considered stale.
	// Default: 1 hour
	StaleLockThreshold time.Duration

	// EnvOverrides maps cache types to environment variable names.
	// Default: {CacheTypeModels: "ALEUTIAN_MODELS_CACHE"}
	EnvOverrides map[CacheType]string

	// AutoFixMounts enables automatic Podman machine mount configuration.
	// When true, mounts are added without user confirmation.
	// Default: false (prompts user via UserPrompter)
	AutoFixMounts bool
}

// CacheInfo contains metadata about a cache location.
//
// # Description
//
// Provides detailed information about a resolved cache path
// including disk space, location type, and container accessibility.
// Returned by GetCacheInfo method.
type CacheInfo struct {
	// Path is the absolute path to the cache directory.
	Path string

	// LocationType indicates where the cache is stored
	// (local, external, env_override, or preferred).
	LocationType CacheLocationType

	// FreeSpaceMB is available space in megabytes at time of query.
	FreeSpaceMB int64

	// TotalSpaceMB is total space in megabytes on the filesystem.
	TotalSpaceMB int64

	// IsContainerAccessible indicates if containers can access this path.
	// On macOS, this requires a Podman VM mount.
	IsContainerAccessible bool

	// MountRoot is the root mount point for external drives.
	// Empty for local paths.
	MountRoot string

	// DriveLabel is the volume label if available.
	// May be empty on some platforms.
	DriveLabel string
}

// DriveInfo contains information about an external drive.
//
// # Description
//
// Provides details about an external drive including capacity,
// existing cache presence, and container accessibility.
// Returned by ListExternalDrives method.
type DriveInfo struct {
	// Path is the mount point path (e.g., "/Volumes/ai_models").
	Path string

	// Label is the volume label if available.
	Label string

	// FreeSpaceMB is available space in megabytes.
	FreeSpaceMB int64

	// TotalSpaceMB is total capacity in megabytes.
	TotalSpaceMB int64

	// HasExistingCache indicates if aleutian_data/models_cache exists on this drive.
	HasExistingCache bool

	// IsContainerAccessible indicates if mounted in Podman VM (macOS).
	IsContainerAccessible bool
}

// -----------------------------------------------------------------------------
// CacheConfig Methods
// -----------------------------------------------------------------------------

// GetMinFreeSpaceMB returns the configured minimum free space or default.
//
// # Description
//
// Returns the minimum free space threshold for cache paths.
// Uses 25 GB as default if not configured or if receiver is nil.
//
// # Inputs
//
// None (method receiver only).
//
// # Outputs
//
//   - int64: Minimum free space in megabytes (never zero)
//
// # Examples
//
//	cfg := &CacheConfig{MinFreeSpaceMB: 10240}
//	minSpace := cfg.GetMinFreeSpaceMB() // 10240
//
//	var nilCfg *CacheConfig
//	minSpace := nilCfg.GetMinFreeSpaceMB() // 25600 (default)
//
// # Limitations
//
//   - Returns default even for negative values
//
// # Assumptions
//
//   - Caller uses return value without additional nil checks
func (c *CacheConfig) GetMinFreeSpaceMB() int64 {
	if c == nil || c.MinFreeSpaceMB <= 0 {
		return DefaultMinFreeSpaceMB
	}
	return c.MinFreeSpaceMB
}

// GetStaleLockThreshold returns the configured stale lock threshold or default.
//
// # Description
//
// Returns the age threshold for considering lock files stale.
// Uses 1 hour as default if not configured or if receiver is nil.
//
// # Inputs
//
// None (method receiver only).
//
// # Outputs
//
//   - time.Duration: Stale lock threshold (never zero)
//
// # Examples
//
//	cfg := &CacheConfig{StaleLockThreshold: 30 * time.Minute}
//	threshold := cfg.GetStaleLockThreshold() // 30m
//
//	var nilCfg *CacheConfig
//	threshold := nilCfg.GetStaleLockThreshold() // 1h (default)
//
// # Limitations
//
//   - Returns default even for negative values
//
// # Assumptions
//
//   - Caller uses return value without additional nil checks
func (c *CacheConfig) GetStaleLockThreshold() time.Duration {
	if c == nil || c.StaleLockThreshold <= 0 {
		return DefaultStaleLockThreshold
	}
	return c.StaleLockThreshold
}

// GetEnvVar returns the environment variable name for a cache type.
//
// # Description
//
// Returns the environment variable name to check for override.
// Uses ALEUTIAN_MODELS_CACHE as default for CacheTypeModels.
// Other cache types return empty string if not configured.
//
// # Inputs
//
//   - cacheType: The type of cache to get env var for
//
// # Outputs
//
//   - string: Environment variable name, or empty if not configured
//
// # Examples
//
//	cfg := &CacheConfig{}
//	envVar := cfg.GetEnvVar(CacheTypeModels) // "ALEUTIAN_MODELS_CACHE"
//
//	cfg := &CacheConfig{EnvOverrides: map[CacheType]string{CacheTypeModels: "MY_CACHE"}}
//	envVar := cfg.GetEnvVar(CacheTypeModels) // "MY_CACHE"
//
// # Limitations
//
//   - Only CacheTypeModels has a default; other types return empty
//
// # Assumptions
//
//   - Caller handles empty return value appropriately
func (c *CacheConfig) GetEnvVar(cacheType CacheType) string {
	if c != nil && c.EnvOverrides != nil {
		if envVar, ok := c.EnvOverrides[cacheType]; ok {
			return envVar
		}
	}
	if cacheType == CacheTypeModels {
		return DefaultEnvVarModels
	}
	return ""
}

// GetMachineName returns the Podman machine name or default.
//
// # Description
//
// Returns the machine name for Podman operations on macOS.
// Uses "podman-machine-default" if not configured or if receiver is nil.
//
// # Inputs
//
// None (method receiver only).
//
// # Outputs
//
//   - string: Podman machine name (never empty)
//
// # Examples
//
//	cfg := &CacheConfig{MachineName: "aleutian-vm"}
//	name := cfg.GetMachineName() // "aleutian-vm"
//
//	var nilCfg *CacheConfig
//	name := nilCfg.GetMachineName() // "podman-machine-default"
//
// # Limitations
//
//   - Does not validate that the machine exists
//
// # Assumptions
//
//   - Machine with returned name exists if on macOS
func (c *CacheConfig) GetMachineName() string {
	if c == nil || c.MachineName == "" {
		return "podman-machine-default"
	}
	return c.MachineName
}

// -----------------------------------------------------------------------------
// Implementation Struct
// -----------------------------------------------------------------------------

// DefaultCachePathResolver implements CachePathResolver with multi-source support.
//
// # Description
//
// This is the production implementation that supports multiple cache
// sources with automatic fallback. Sources are tried in priority order
// until a suitable cache path is found.
//
// # Resolution Priority
//
//  1. Environment variable override (e.g., ALEUTIAN_MODELS_CACHE)
//  2. User-designated preferred drive (config.PreferredDrive)
//  3. External drive with existing aleutian_data/models_cache
//  4. External drive with most free space above threshold
//  5. Local stack directory ({StackDir}/models_cache)
//
// # Security
//
//   - Validates paths to prevent directory traversal
//   - Checks permissions before use
//   - Does not follow symlinks outside configured areas
//
// # Thread Safety
//
// DefaultCachePathResolver is safe for concurrent use. Internal
// mutex protects shared state during resolution.
type DefaultCachePathResolver struct {
	config               CacheConfig
	proc                 process.Manager
	prompter             util.UserPrompter
	osStatFunc           func(string) (os.FileInfo, error)
	osMkdirAllFunc       func(string, os.FileMode) error
	osGetenvFunc         func(string) string
	osUserHomeDirFunc    func() (string, error)
	osRemoveFunc         func(string) error
	filepathWalkFunc     func(string, filepath.WalkFunc) error
	filepathEvalSymlinks func(string) (string, error)
	runtimeGOOSFunc      func() string
	syscallStatfsFunc    func(string, *syscall.Statfs_t) error
	mu                   sync.RWMutex
}

// -----------------------------------------------------------------------------
// Constructor
// -----------------------------------------------------------------------------

// NewDefaultCachePathResolver creates a cache resolver with the given configuration.
//
// # Description
//
// Creates a new CachePathResolver that resolves cache paths using
// the configured priority chain. The configuration is copied internally
// so subsequent changes to the passed config have no effect.
//
// # Inputs
//
//   - cfg: Cache configuration with paths, thresholds, and options
//   - proc: ProcessManager for executing podman commands (required for macOS)
//   - prompter: UserPrompter for confirmation dialogs (may be nil if AutoFixMounts=true)
//
// # Outputs
//
//   - *DefaultCachePathResolver: Ready-to-use resolver instance
//
// # Examples
//
//	cfg := CacheConfig{
//	    StackDir:         "/home/user/.aleutian",
//	    ConfiguredDrives: []string{"/Volumes/ai_models"},
//	    MinFreeSpaceMB:   25600,
//	}
//	resolver := NewDefaultCachePathResolver(cfg, proc, prompter)
//	path, err := resolver.Resolve(ctx, CacheTypeModels)
//
// # Limitations
//
//   - Configuration is copied; changes after creation have no effect
//   - ProcessManager must be non-nil for macOS mount operations
//
// # Assumptions
//
//   - ProcessManager is properly configured for podman commands
//   - StackDir is a valid, writable directory path
func NewDefaultCachePathResolver(cfg CacheConfig, proc process.Manager, prompter util.UserPrompter) *DefaultCachePathResolver {
	return &DefaultCachePathResolver{
		config:               cfg,
		proc:                 proc,
		prompter:             prompter,
		osStatFunc:           os.Stat,
		osMkdirAllFunc:       os.MkdirAll,
		osGetenvFunc:         os.Getenv,
		osUserHomeDirFunc:    os.UserHomeDir,
		osRemoveFunc:         os.Remove,
		filepathWalkFunc:     filepath.Walk,
		filepathEvalSymlinks: filepath.EvalSymlinks,
		runtimeGOOSFunc:      func() string { return runtime.GOOS },
		syscallStatfsFunc:    syscall.Statfs,
	}
}

// -----------------------------------------------------------------------------
// Interface Implementation - Resolve
// -----------------------------------------------------------------------------

// Resolve finds or creates the best cache directory for the given type.
//
// # Description
//
// Resolves the optimal cache path using this priority:
//  1. Environment variable override (if set and valid)
//  2. User-designated preferred drive (if set and accessible)
//  3. External drive with existing cache (auto-detected from config)
//  4. External drive with most free space above threshold
//  5. Default location ({StackDir}/models_cache)
//
// Creates the directory if it doesn't exist and clears stale locks.
//
// # Inputs
//
//   - ctx: Context for cancellation and timeout control
//   - cacheType: Type of cache to resolve (currently only CacheTypeModels)
//
// # Outputs
//
//   - string: Resolved absolute path to cache directory
//   - error: ErrCachePathNotFound if no suitable path, other errors on failure
//
// # Examples
//
//	path, err := resolver.Resolve(ctx, CacheTypeModels)
//	if errors.Is(err, ErrCachePathNotFound) {
//	    log.Fatal("No suitable cache location found")
//	}
//	if err != nil {
//	    return fmt.Errorf("cache resolution failed: %w", err)
//	}
//	os.Setenv("ALEUTIAN_MODELS_CACHE", path)
//
// # Limitations
//
//   - May trigger Podman machine restart on macOS
//   - External drives must already be mounted
//   - Only CacheTypeModels is currently fully supported
//
// # Assumptions
//
//   - ConfiguredDrives is populated from aleutian.yaml
//   - Podman machine exists and is initialized on macOS
//   - StackDir is writable as last-resort fallback
func (r *DefaultCachePathResolver) Resolve(ctx context.Context, cacheType CacheType) (string, error) {
	// Step 1: Check environment variable override
	path, err := r.resolveFromEnv(ctx, cacheType)
	if err == nil && path != "" {
		return path, nil
	}

	// Step 2: Check user-designated preferred drive
	path, err = r.resolveFromPreferred(ctx)
	if err == nil && path != "" {
		return path, nil
	}

	// Step 3: Check for existing cache on external drives
	path, err = r.resolveFromExistingExternal(ctx)
	if err == nil && path != "" {
		return path, nil
	}

	// Step 4: Check for external drive with most free space
	path, err = r.resolveFromBestExternal(ctx)
	if err == nil && path != "" {
		return path, nil
	}

	// Step 5: Fall back to local cache
	return r.resolveLocal(ctx)
}

// -----------------------------------------------------------------------------
// Interface Implementation - VerifyContainerAccess
// -----------------------------------------------------------------------------

// VerifyContainerAccess checks if containers can access the path.
//
// # Description
//
// On macOS, verifies the path is mounted in the Podman VM by attempting
// to run a container that accesses the path. On Linux, verifies the
// path exists since containers have direct filesystem access.
//
// # Inputs
//
//   - ctx: Context for cancellation and timeout control
//   - path: Absolute filesystem path to verify
//
// # Outputs
//
//   - bool: True if containers can access the path
//   - error: If verification process itself fails
//
// # Examples
//
//	accessible, err := resolver.VerifyContainerAccess(ctx, "/Volumes/ai_models")
//	if err != nil {
//	    return fmt.Errorf("verification failed: %w", err)
//	}
//	if !accessible {
//	    // Need to add mount via EnsureContainerAccess
//	}
//
// # Limitations
//
//   - On macOS, requires running Podman machine
//   - Spawns a temporary busybox container for verification
//   - May take a few seconds on first run
//
// # Assumptions
//
//   - Path exists on the host filesystem
//   - Podman is installed and the machine is running (macOS)
func (r *DefaultCachePathResolver) VerifyContainerAccess(ctx context.Context, path string) (bool, error) {
	if !r.needsMachineMount() {
		// Linux: direct access, just check path exists
		_, err := r.osStatFunc(path)
		return err == nil, nil
	}

	// macOS: check if path is mounted in Podman VM
	mountRoot := r.extractMountRoot(path)
	return r.canAccessFromContainer(ctx, mountRoot), nil
}

// -----------------------------------------------------------------------------
// Interface Implementation - EnsureContainerAccess
// -----------------------------------------------------------------------------

// EnsureContainerAccess attempts to make a path accessible to containers.
//
// # Description
//
// On macOS, checks if the path is already accessible. If not, adds a
// volume mount to the Podman machine configuration and restarts the
// machine. On Linux, this is a no-op since containers have direct access.
//
// # Inputs
//
//   - ctx: Context for cancellation and timeout control
//   - path: Absolute filesystem path to make accessible
//
// # Outputs
//
//   - error: ErrMountConfigFailed if user declines or command fails, nil on success
//
// # Examples
//
//	err := resolver.EnsureContainerAccess(ctx, "/Volumes/ai_models")
//	if errors.Is(err, ErrMountConfigFailed) {
//	    fmt.Println("Could not configure mount, falling back to local cache")
//	    return nil
//	}
//	if err != nil {
//	    return fmt.Errorf("unexpected error: %w", err)
//	}
//
// # Limitations
//
//   - Restarts Podman machine, interrupting running containers
//   - Requires user confirmation unless AutoFixMounts is true
//   - Mount persists across machine restarts
//
// # Assumptions
//
//   - User has permission to modify Podman machine configuration
//   - Podman machine can be stopped and restarted
//   - ProcessManager is properly configured
func (r *DefaultCachePathResolver) EnsureContainerAccess(ctx context.Context, path string) error {
	if !r.needsMachineMount() {
		return nil
	}

	mountRoot := r.extractMountRoot(path)

	// Check if already accessible
	if r.canAccessFromContainer(ctx, mountRoot) {
		return nil
	}

	// Need to add mount - check if auto-fix is enabled
	if !r.config.AutoFixMounts {
		if r.prompter == nil {
			return ErrMountConfigFailed
		}
		prompt := fmt.Sprintf("Add mount for %s and restart Podman machine?", mountRoot)
		confirmed, err := r.prompter.Confirm(ctx, prompt)
		if err != nil {
			return ErrMountConfigFailed
		}
		if !confirmed {
			return ErrMountConfigFailed
		}
	}

	return r.addMachineMount(ctx, mountRoot)
}

// -----------------------------------------------------------------------------
// Interface Implementation - GetCacheInfo
// -----------------------------------------------------------------------------

// GetCacheInfo returns information about a resolved cache path.
//
// # Description
//
// Gathers metadata about the specified cache path including filesystem
// space, location classification, and container accessibility status.
//
// # Inputs
//
//   - ctx: Context for cancellation and timeout control
//   - path: Absolute cache path to inspect
//
// # Outputs
//
//   - *CacheInfo: Populated cache metadata structure
//   - error: If disk space query or other inspection fails
//
// # Examples
//
//	info, err := resolver.GetCacheInfo(ctx, cachePath)
//	if err != nil {
//	    return fmt.Errorf("failed to inspect cache: %w", err)
//	}
//	fmt.Printf("Cache at %s:\n", info.Path)
//	fmt.Printf("  Type: %s\n", info.LocationType)
//	fmt.Printf("  Free: %d MB / %d MB\n", info.FreeSpaceMB, info.TotalSpaceMB)
//	fmt.Printf("  Container access: %v\n", info.IsContainerAccessible)
//
// # Limitations
//
//   - Disk space values are snapshots that may change
//   - DriveLabel is not populated in current implementation
//
// # Assumptions
//
//   - Path exists and is accessible to current user
func (r *DefaultCachePathResolver) GetCacheInfo(ctx context.Context, path string) (*CacheInfo, error) {
	info := &CacheInfo{
		Path: path,
	}

	// Determine location type
	info.LocationType = r.determineLocationType(path)

	// Get disk space
	freeMB, totalMB, err := r.getDiskSpace(path)
	if err != nil {
		return nil, fmt.Errorf("failed to get disk space: %w", err)
	}
	info.FreeSpaceMB = freeMB
	info.TotalSpaceMB = totalMB

	// Check container accessibility
	accessible, _ := r.VerifyContainerAccess(ctx, path)
	info.IsContainerAccessible = accessible

	// Get mount root for external paths
	if r.isExternalPath(path) {
		info.MountRoot = r.extractMountRoot(path)
	}

	return info, nil
}

// -----------------------------------------------------------------------------
// Interface Implementation - ClearStaleLocks
// -----------------------------------------------------------------------------

// ClearStaleLocks removes stale HuggingFace lock files.
//
// # Description
//
// Walks the cache directory tree looking for .lock files older than
// the configured StaleLockThreshold. These stale locks are removed
// to prevent blocking future model downloads.
//
// # Inputs
//
//   - ctx: Context for cancellation (walk stops on context cancel)
//   - cachePath: Root path to scan recursively for lock files
//
// # Outputs
//
//   - int: Number of lock files successfully removed
//   - error: If walk fails (partial cleanup may have occurred)
//
// # Examples
//
//	cleared, err := resolver.ClearStaleLocks(ctx, "/path/to/cache")
//	if err != nil && !errors.Is(err, context.Canceled) {
//	    log.Printf("Warning: lock cleanup error: %v", err)
//	}
//	fmt.Printf("Removed %d stale locks\n", cleared)
//
// # Limitations
//
//   - Only removes locks older than StaleLockThreshold
//   - Skips inaccessible directories without error
//   - Does not distinguish HuggingFace locks from other .lock files
//
// # Assumptions
//
//   - Lock files follow *.lock naming convention
//   - Old locks are safe to remove (process is dead)
func (r *DefaultCachePathResolver) ClearStaleLocks(ctx context.Context, cachePath string) (int, error) {
	threshold := r.config.GetStaleLockThreshold()
	cutoff := time.Now().Add(-threshold)
	cleared := 0

	err := r.filepathWalkFunc(cachePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip inaccessible paths
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if info.IsDir() {
			return nil
		}

		if !strings.HasSuffix(info.Name(), ".lock") {
			return nil
		}

		if info.ModTime().Before(cutoff) {
			if removeErr := r.osRemoveFunc(path); removeErr == nil {
				cleared++
			}
		}

		return nil
	})

	if err != nil && !errors.Is(err, context.Canceled) {
		return cleared, fmt.Errorf("error walking cache directory: %w", err)
	}

	return cleared, nil
}

// -----------------------------------------------------------------------------
// Interface Implementation - ListExternalDrives
// -----------------------------------------------------------------------------

// ListExternalDrives returns detected external drives suitable for caching.
//
// # Description
//
// Iterates through ConfiguredDrives, filtering to those that are
// external mount points, currently mounted, and gathering disk
// space and cache presence information.
//
// # Inputs
//
//   - ctx: Context for cancellation and timeout control
//
// # Outputs
//
//   - []DriveInfo: Slice of drive info (may be empty if none found)
//   - error: Currently always nil (errors are skipped per-drive)
//
// # Examples
//
//	drives, _ := resolver.ListExternalDrives(ctx)
//	for _, d := range drives {
//	    status := "no cache"
//	    if d.HasExistingCache {
//	        status = "has cache"
//	    }
//	    fmt.Printf("%s: %d MB free (%s)\n", d.Path, d.FreeSpaceMB, status)
//	}
//
// # Limitations
//
//   - Only scans drives in ConfiguredDrives list
//   - Unmounted drives are silently skipped
//   - Does not populate Label field
//
// # Assumptions
//
//   - Drives are already mounted by the OS
//   - ConfiguredDrives contains valid path strings
func (r *DefaultCachePathResolver) ListExternalDrives(ctx context.Context) ([]DriveInfo, error) {
	var drives []DriveInfo

	homeDir, _ := r.osUserHomeDirFunc()

	for _, drivePath := range r.config.ConfiguredDrives {
		if r.shouldSkipDrive(drivePath, homeDir) {
			continue
		}

		if _, err := r.osStatFunc(drivePath); err != nil {
			continue
		}

		freeMB, totalMB, err := r.getDiskSpace(drivePath)
		if err != nil {
			continue
		}

		cachePath := filepath.Join(drivePath, cacheSubdir)
		_, hasCache := r.osStatFunc(cachePath)

		accessible, _ := r.VerifyContainerAccess(ctx, drivePath)

		drives = append(drives, DriveInfo{
			Path:                  drivePath,
			FreeSpaceMB:           freeMB,
			TotalSpaceMB:          totalMB,
			HasExistingCache:      hasCache == nil,
			IsContainerAccessible: accessible,
		})
	}

	return drives, nil
}

// -----------------------------------------------------------------------------
// Private Resolution Methods
// -----------------------------------------------------------------------------

// resolveFromEnv checks for environment variable override.
//
// # Description
//
// Checks if an environment variable override is set for the cache type.
// If set, resolves symlinks, ensures directory exists, verifies container
// access, and clears stale locks.
//
// # Inputs
//
//   - ctx: Context for cancellation
//   - cacheType: Type of cache to resolve
//
// # Outputs
//
//   - string: Resolved path if env var set and valid, empty otherwise
//   - error: If path creation or access verification fails
//
// # Examples
//
//	// With ALEUTIAN_MODELS_CACHE=/custom/path set
//	path, err := r.resolveFromEnv(ctx, CacheTypeModels)
//	// path == "/custom/path", err == nil
//
// # Limitations
//
//   - Returns empty string if env var not set (not an error)
//
// # Assumptions
//
//   - Env var contains a valid filesystem path
func (r *DefaultCachePathResolver) resolveFromEnv(ctx context.Context, cacheType CacheType) (string, error) {
	envVar := r.config.GetEnvVar(cacheType)
	if envVar == "" {
		return "", nil
	}

	path := r.osGetenvFunc(envVar)
	if path == "" {
		return "", nil
	}

	resolved, err := r.filepathEvalSymlinks(path)
	if err == nil {
		path = resolved
	}

	if err := r.ensureDirectory(path); err != nil {
		return "", err
	}

	if err := r.verifyAndEnsureAccess(ctx, path); err != nil {
		return "", err
	}

	r.ClearStaleLocks(ctx, path)

	return path, nil
}

// resolveFromPreferred checks user-designated preferred drive.
//
// # Description
//
// Checks if PreferredDrive is configured and mounted. If so, ensures
// the cache subdirectory exists, verifies container access, checks
// disk space, and clears stale locks.
//
// # Inputs
//
//   - ctx: Context for cancellation
//
// # Outputs
//
//   - string: Cache path on preferred drive if valid, empty otherwise
//   - error: If directory creation, access, or space check fails
//
// # Examples
//
//	// With config.PreferredDrive = "/Volumes/ai_models"
//	path, err := r.resolveFromPreferred(ctx)
//	// path == "/Volumes/ai_models/aleutian_data/models_cache"
//
// # Limitations
//
//   - Returns empty if PreferredDrive not configured or not mounted
//
// # Assumptions
//
//   - PreferredDrive is a valid external mount point
func (r *DefaultCachePathResolver) resolveFromPreferred(ctx context.Context) (string, error) {
	if r.config.PreferredDrive == "" {
		return "", nil
	}

	if _, err := r.osStatFunc(r.config.PreferredDrive); err != nil {
		return "", nil
	}

	cachePath := filepath.Join(r.config.PreferredDrive, cacheSubdir)

	if err := r.ensureDirectory(cachePath); err != nil {
		return "", err
	}

	if err := r.verifyAndEnsureAccess(ctx, cachePath); err != nil {
		return "", err
	}

	if err := r.checkMinSpace(cachePath); err != nil {
		return "", err
	}

	r.ClearStaleLocks(ctx, cachePath)

	return cachePath, nil
}

// resolveFromExistingExternal finds an external drive with existing cache.
//
// # Description
//
// Scans ConfiguredDrives for external drives that already have an
// aleutian_data/models_cache directory. Returns the first accessible
// drive with sufficient space.
//
// # Inputs
//
//   - ctx: Context for cancellation
//
// # Outputs
//
//   - string: Cache path on drive with existing cache, empty if none found
//   - error: Currently nil (failures cause skip to next drive)
//
// # Examples
//
//	path, _ := r.resolveFromExistingExternal(ctx)
//	if path != "" {
//	    fmt.Printf("Found existing cache at %s\n", path)
//	}
//
// # Limitations
//
//   - First match wins; doesn't compare space or recency
//
// # Assumptions
//
//   - Existing cache directories are valid and usable
func (r *DefaultCachePathResolver) resolveFromExistingExternal(ctx context.Context) (string, error) {
	homeDir, _ := r.osUserHomeDirFunc()

	for _, drive := range r.config.ConfiguredDrives {
		if r.shouldSkipDrive(drive, homeDir) {
			continue
		}

		cachePath := filepath.Join(drive, cacheSubdir)
		if _, err := r.osStatFunc(cachePath); err != nil {
			continue
		}

		if err := r.verifyAndEnsureAccess(ctx, cachePath); err != nil {
			continue
		}

		if err := r.checkMinSpace(cachePath); err != nil {
			continue
		}

		r.ClearStaleLocks(ctx, cachePath)

		return cachePath, nil
	}

	return "", nil
}

// resolveFromBestExternal finds external drive with most free space.
//
// # Description
//
// Lists all external drives, filters to those with sufficient space,
// sorts by free space descending, and returns a cache path on the
// drive with the most available space.
//
// # Inputs
//
//   - ctx: Context for cancellation
//
// # Outputs
//
//   - string: Cache path on best drive, empty if none qualify
//   - error: If directory creation or access fails
//
// # Examples
//
//	path, err := r.resolveFromBestExternal(ctx)
//	if path != "" {
//	    fmt.Printf("Using drive with most space: %s\n", path)
//	}
//
// # Limitations
//
//   - Creates new cache directory on the selected drive
//
// # Assumptions
//
//   - At least one external drive has sufficient space
func (r *DefaultCachePathResolver) resolveFromBestExternal(ctx context.Context) (string, error) {
	drives, err := r.ListExternalDrives(ctx)
	if err != nil || len(drives) == 0 {
		return "", nil
	}

	minSpace := r.config.GetMinFreeSpaceMB()
	var candidates []DriveInfo
	for _, d := range drives {
		if d.FreeSpaceMB >= minSpace {
			candidates = append(candidates, d)
		}
	}

	if len(candidates) == 0 {
		return "", nil
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].FreeSpaceMB > candidates[j].FreeSpaceMB
	})

	best := candidates[0]
	cachePath := filepath.Join(best.Path, cacheSubdir)

	if err := r.ensureDirectory(cachePath); err != nil {
		return "", err
	}

	if err := r.verifyAndEnsureAccess(ctx, cachePath); err != nil {
		return "", err
	}

	r.ClearStaleLocks(ctx, cachePath)

	return cachePath, nil
}

// resolveLocal uses the local stack directory.
//
// # Description
//
// Creates cache in the local stack directory as last-resort fallback.
// This always succeeds if StackDir is writable.
//
// # Inputs
//
//   - ctx: Context for cancellation
//
// # Outputs
//
//   - string: Local cache path
//   - error: ErrCachePathNotFound if directory creation fails
//
// # Examples
//
//	path, err := r.resolveLocal(ctx)
//	// path == "/home/user/.aleutian/models_cache"
//
// # Limitations
//
//   - Uses local disk which may have limited space
//
// # Assumptions
//
//   - StackDir is configured and writable
func (r *DefaultCachePathResolver) resolveLocal(ctx context.Context) (string, error) {
	cachePath := filepath.Join(r.config.StackDir, "models_cache")

	if err := r.ensureDirectory(cachePath); err != nil {
		return "", fmt.Errorf("%w: %v", ErrCachePathNotFound, err)
	}

	r.ClearStaleLocks(ctx, cachePath)

	return cachePath, nil
}

// -----------------------------------------------------------------------------
// Private Helper Methods - Platform Detection
// -----------------------------------------------------------------------------

// needsMachineMount returns true if platform requires Podman VM mounts.
//
// # Description
//
// Returns true on macOS where containers run in a VM that needs
// explicit volume mounts to access host paths.
//
// # Inputs
//
// None (uses runtime detection).
//
// # Outputs
//
//   - bool: True on macOS, false on Linux
//
// # Examples
//
//	if r.needsMachineMount() {
//	    // macOS: need to configure VM mounts
//	}
//
// # Limitations
//
//   - Windows not supported (would return false)
//
// # Assumptions
//
//   - Runtime GOOS accurately reflects platform
func (r *DefaultCachePathResolver) needsMachineMount() bool {
	return r.runtimeGOOSFunc() == "darwin"
}

// isExternalPath determines if a path is on an external mount point.
//
// # Description
//
// Checks if the path starts with known external mount prefixes
// for the current platform.
//
// # Inputs
//
//   - path: Absolute path to check
//
// # Outputs
//
//   - bool: True if path appears to be on external storage
//
// # Examples
//
//	r.isExternalPath("/Volumes/ExternalDrive/data") // true on macOS
//	r.isExternalPath("/home/user/data")              // false
//
// # Limitations
//
//   - Uses prefix matching only; doesn't verify actual mount status
//
// # Assumptions
//
//   - Standard mount point conventions are followed
func (r *DefaultCachePathResolver) isExternalPath(path string) bool {
	goos := r.runtimeGOOSFunc()
	switch goos {
	case "darwin":
		return strings.HasPrefix(path, "/Volumes/") ||
			strings.HasPrefix(path, "/private/var/folders/")
	case "linux":
		return strings.HasPrefix(path, "/mnt/") ||
			strings.HasPrefix(path, "/media/") ||
			strings.HasPrefix(path, "/run/media/")
	default:
		return false
	}
}

// extractMountRoot extracts the root mount point from a full path.
//
// # Description
//
// Given a full path, returns the mount point root. This is used
// to determine what path needs to be mounted in the Podman VM.
//
// # Inputs
//
//   - path: Full absolute path
//
// # Outputs
//
//   - string: Mount root path
//
// # Examples
//
//	r.extractMountRoot("/Volumes/DriveName/path/to/cache")
//	// Returns "/Volumes/DriveName"
//
//	r.extractMountRoot("/media/user/drive/data")
//	// Returns "/media/user/drive"
//
// # Limitations
//
//   - Returns original path if pattern not recognized
//
// # Assumptions
//
//   - Standard mount point directory structure
func (r *DefaultCachePathResolver) extractMountRoot(path string) string {
	goos := r.runtimeGOOSFunc()

	switch goos {
	case "darwin":
		if strings.HasPrefix(path, "/Volumes/") {
			parts := strings.Split(path, "/")
			if len(parts) >= 3 {
				return "/" + parts[1] + "/" + parts[2]
			}
		}
	case "linux":
		if strings.HasPrefix(path, "/run/media/") {
			parts := strings.Split(path, "/")
			if len(parts) >= 5 {
				return "/" + parts[1] + "/" + parts[2] + "/" + parts[3] + "/" + parts[4]
			}
		} else if strings.HasPrefix(path, "/media/") {
			parts := strings.Split(path, "/")
			if len(parts) >= 4 {
				return "/" + parts[1] + "/" + parts[2] + "/" + parts[3]
			}
		} else if strings.HasPrefix(path, "/mnt/") {
			parts := strings.Split(path, "/")
			if len(parts) >= 3 {
				return "/" + parts[1] + "/" + parts[2]
			}
		}
	}

	return path
}

// -----------------------------------------------------------------------------
// Private Helper Methods - Drive Filtering
// -----------------------------------------------------------------------------

// shouldSkipDrive determines if a drive should be skipped during scanning.
//
// # Description
//
// Returns true for drives that should not be considered as external
// cache locations: home directory, generic mount roots, and non-external paths.
//
// # Inputs
//
//   - drivePath: Path to evaluate
//   - homeDir: User's home directory for comparison
//
// # Outputs
//
//   - bool: True if drive should be skipped
//
// # Examples
//
//	r.shouldSkipDrive("/home/user/data", "/home/user")  // true
//	r.shouldSkipDrive("/Volumes", "/home/user")          // true
//	r.shouldSkipDrive("/Volumes/External", "/home/user") // false
//
// # Limitations
//
//   - Does not check if drive is actually mounted
//
// # Assumptions
//
//   - homeDir is accurate for current user
func (r *DefaultCachePathResolver) shouldSkipDrive(drivePath, homeDir string) bool {
	if homeDir != "" && strings.HasPrefix(drivePath, homeDir) {
		return true
	}

	if drivePath == "/Volumes" || drivePath == "/mnt" || drivePath == "/media" {
		return true
	}

	if !r.isExternalPath(drivePath) {
		return true
	}

	return false
}

// -----------------------------------------------------------------------------
// Private Helper Methods - Directory Management
// -----------------------------------------------------------------------------

// ensureDirectory creates the directory if it doesn't exist.
//
// # Description
//
// Creates the specified directory with permissions 0755 if it
// doesn't already exist. Parent directories are created as needed.
//
// # Inputs
//
//   - path: Directory path to ensure exists
//
// # Outputs
//
//   - error: If directory creation fails
//
// # Examples
//
//	err := r.ensureDirectory("/path/to/cache")
//	if err != nil {
//	    return fmt.Errorf("cannot create cache dir: %w", err)
//	}
//
// # Limitations
//
//   - Uses fixed permission 0755
//
// # Assumptions
//
//   - Current user has permission to create the directory
func (r *DefaultCachePathResolver) ensureDirectory(path string) error {
	if _, err := r.osStatFunc(path); os.IsNotExist(err) {
		if err := r.osMkdirAllFunc(path, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", path, err)
		}
	}
	return nil
}

// -----------------------------------------------------------------------------
// Private Helper Methods - Access Verification
// -----------------------------------------------------------------------------

// verifyAndEnsureAccess verifies and ensures container access.
//
// # Description
//
// First checks if containers can access the path. If not, attempts
// to configure access via EnsureContainerAccess.
//
// # Inputs
//
//   - ctx: Context for cancellation
//   - path: Path to verify and configure
//
// # Outputs
//
//   - error: If verification fails and access cannot be ensured
//
// # Examples
//
//	if err := r.verifyAndEnsureAccess(ctx, cachePath); err != nil {
//	    // Path is not usable, try next option
//	}
//
// # Limitations
//
//   - May trigger machine restart on macOS
//
// # Assumptions
//
//   - Path exists on the host filesystem
func (r *DefaultCachePathResolver) verifyAndEnsureAccess(ctx context.Context, path string) error {
	accessible, err := r.VerifyContainerAccess(ctx, path)
	if err != nil {
		return err
	}

	if !accessible {
		if err := r.EnsureContainerAccess(ctx, path); err != nil {
			return err
		}
	}

	return nil
}

// canAccessFromContainer checks if Podman can access the path.
//
// # Description
//
// Runs a temporary container with the path mounted to verify
// the Podman VM can access the mount point.
//
// # Inputs
//
//   - ctx: Context for cancellation
//   - mountRoot: Mount root path to verify
//
// # Outputs
//
//   - bool: True if container can access the path
//
// # Examples
//
//	if r.canAccessFromContainer(ctx, "/Volumes/External") {
//	    // Path is accessible
//	}
//
// # Limitations
//
//   - Requires Podman and busybox image
//   - Spawns actual container (has some overhead)
//
// # Assumptions
//
//   - ProcessManager is configured and Podman is available
func (r *DefaultCachePathResolver) canAccessFromContainer(ctx context.Context, mountRoot string) bool {
	if r.proc == nil {
		return false
	}

	output, err := r.proc.Run(ctx, "podman", "run", "--rm",
		"-v", mountRoot+":"+mountRoot+":ro",
		"busybox", "ls", mountRoot)
	if err != nil {
		return false
	}

	return len(output) > 0
}

// addMachineMount adds a volume mount to the Podman machine.
//
// # Description
//
// Stops the Podman machine, adds a volume mount using podman machine set,
// and restarts the machine.
//
// # Inputs
//
//   - ctx: Context for cancellation
//   - mountPath: Path to mount in the VM
//
// # Outputs
//
//   - error: ErrMountConfigFailed if any step fails
//
// # Examples
//
//	if err := r.addMachineMount(ctx, "/Volumes/External"); err != nil {
//	    // Mount failed
//	}
//
// # Limitations
//
//   - Stops running containers during machine restart
//   - May not work on older Podman versions
//
// # Assumptions
//
//   - Podman machine exists with the configured name
//   - User has permission to modify machine config
func (r *DefaultCachePathResolver) addMachineMount(ctx context.Context, mountPath string) error {
	if r.proc == nil {
		return ErrMountConfigFailed
	}

	machineName := r.config.GetMachineName()

	_, _ = r.proc.Run(ctx, "podman", "machine", "stop", machineName)

	_, err := r.proc.Run(ctx, "podman", "machine", "set", machineName,
		"--volume", mountPath+":"+mountPath)
	if err != nil {
		_, startErr := r.proc.Run(ctx, "podman", "machine", "start", machineName)
		if startErr != nil {
			return fmt.Errorf("%w: %v", ErrMountConfigFailed, startErr)
		}
		return ErrMountConfigFailed
	}

	_, err = r.proc.Run(ctx, "podman", "machine", "start", machineName)
	if err != nil {
		return fmt.Errorf("%w: failed to start machine: %v", ErrMountConfigFailed, err)
	}

	return nil
}

// -----------------------------------------------------------------------------
// Private Helper Methods - Disk Space
// -----------------------------------------------------------------------------

// checkMinSpace verifies minimum disk space at path.
//
// # Description
//
// Checks that the filesystem at the given path has at least
// MinFreeSpaceMB megabytes available.
//
// # Inputs
//
//   - path: Path to check disk space for
//
// # Outputs
//
//   - error: ErrInsufficientSpace if below threshold
//
// # Examples
//
//	if err := r.checkMinSpace("/path/to/cache"); err != nil {
//	    if errors.Is(err, ErrInsufficientSpace) {
//	        fmt.Println("Not enough space")
//	    }
//	}
//
// # Limitations
//
//   - Space is checked at point in time; may change
//
// # Assumptions
//
//   - Path exists and is accessible
func (r *DefaultCachePathResolver) checkMinSpace(path string) error {
	freeMB, _, err := r.getDiskSpace(path)
	if err != nil {
		return err
	}

	minSpace := r.config.GetMinFreeSpaceMB()
	if freeMB < minSpace {
		return fmt.Errorf("%w: %d MB available, %d MB required",
			ErrInsufficientSpace, freeMB, minSpace)
	}

	return nil
}

// getDiskSpace returns free and total space in MB for a path.
//
// # Description
//
// Uses syscall.Statfs to query filesystem statistics and converts
// to megabytes.
//
// # Inputs
//
//   - path: Path to query disk space for
//
// # Outputs
//
//   - freeMB: Available space in megabytes
//   - totalMB: Total space in megabytes
//   - err: If statfs call fails
//
// # Examples
//
//	free, total, err := r.getDiskSpace("/Volumes/External")
//	if err == nil {
//	    fmt.Printf("%d MB free of %d MB\n", free, total)
//	}
//
// # Limitations
//
//   - Unix-only (uses syscall.Statfs)
//
// # Assumptions
//
//   - Path exists and is on a mounted filesystem
func (r *DefaultCachePathResolver) getDiskSpace(path string) (freeMB, totalMB int64, err error) {
	var stat syscall.Statfs_t
	if err := r.syscallStatfsFunc(path, &stat); err != nil {
		return 0, 0, fmt.Errorf("statfs failed for %s: %w", path, err)
	}

	freeMB = int64(stat.Bavail) * int64(stat.Bsize) / (1024 * 1024)
	totalMB = int64(stat.Blocks) * int64(stat.Bsize) / (1024 * 1024)
	return freeMB, totalMB, nil
}

// -----------------------------------------------------------------------------
// Private Helper Methods - Location Classification
// -----------------------------------------------------------------------------

// determineLocationType determines the location type of a path.
//
// # Description
//
// Classifies a path as local, external, env_override, or preferred
// based on how it was likely resolved.
//
// # Inputs
//
//   - path: Path to classify
//
// # Outputs
//
//   - CacheLocationType: Classification of the path
//
// # Examples
//
//	locType := r.determineLocationType("/Volumes/External/cache")
//	// Returns CacheLocationExternal
//
// # Limitations
//
//   - Classification is heuristic; may not match actual resolution path
//
// # Assumptions
//
//   - Path was resolved by this resolver
func (r *DefaultCachePathResolver) determineLocationType(path string) CacheLocationType {
	envVar := r.config.GetEnvVar(CacheTypeModels)
	if envVar != "" {
		envPath := r.osGetenvFunc(envVar)
		if envPath != "" {
			if resolved, err := r.filepathEvalSymlinks(envPath); err == nil {
				envPath = resolved
			}
			if path == envPath || strings.HasPrefix(path, envPath) {
				return CacheLocationEnvOverride
			}
		}
	}

	if r.config.PreferredDrive != "" && strings.HasPrefix(path, r.config.PreferredDrive) {
		return CacheLocationPreferred
	}

	if r.isExternalPath(path) {
		return CacheLocationExternal
	}

	return CacheLocationLocal
}

// -----------------------------------------------------------------------------
// Compile-time Interface Check
// -----------------------------------------------------------------------------

var _ CachePathResolver = (*DefaultCachePathResolver)(nil)
