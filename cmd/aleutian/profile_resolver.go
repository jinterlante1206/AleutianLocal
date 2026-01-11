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
Package main provides ProfileResolver for hardware-based configuration optimization.

The ProfileResolver determines optimal environment configuration based on detected
hardware capabilities. It supports automatic detection, explicit profile selection,
and user-defined custom profiles.

# Profile Tiers

Built-in profiles match hardware to optimal model configurations:

  - Low (< 16GB): gemma3:4b, 2048 tokens - Basic local inference
  - Standard (16-32GB): qwen3:14b, 4096 tokens - Balanced performance
  - Performance (32-128GB): gpt-oss:20b, 8192 tokens - Enhanced context with thinking
  - Ultra (128GB+): gpt-oss:120b, 32768 tokens - Enterprise grade with thinking

# Hardware Detection

On macOS: Uses unified memory (sysctl hw.memsize)
On Linux: Prefers NVIDIA VRAM (nvidia-smi), falls back to system RAM
On Windows: Safe default (8GB assumed)

# Custom Profiles

Users can define custom profiles in aleutian.yaml:

	profiles:
	  - name: my-custom
	    ollama_model: mixtral:8x7b
	    max_tokens: 16384
	    reranker_model: cross-encoder/ms-marco-MiniLM-L-6-v2
	    min_ram_mb: 48000
*/
package main

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"

	"github.com/jinterlante1206/AleutianLocal/cmd/aleutian/config"
	"github.com/jinterlante1206/AleutianLocal/cmd/aleutian/internal/infra/process"
)

// -----------------------------------------------------------------------------
// ProfileResolver Interface
// -----------------------------------------------------------------------------

// ProfileResolver determines optimal configuration based on hardware capabilities.
//
// # Description
//
// This interface abstracts hardware detection and profile calculation,
// enabling support for different detection strategies, custom profiles,
// and testability through mocking.
//
// # Thread Safety
//
// Implementations must be safe for concurrent use.
type ProfileResolver interface {
	// Resolve determines the optimal environment configuration.
	//
	// # Description
	//
	// Calculates environment variables based on hardware capabilities and
	// the specified profile. Returns a map suitable for passing to compose.
	//
	// # Inputs
	//
	//   - ctx: Context for cancellation and timeout
	//   - opts: Profile resolution options
	//
	// # Outputs
	//
	//   - map[string]string: Environment variables for container configuration
	//   - error: Non-nil if hardware detection or profile lookup fails
	//
	// # Examples
	//
	//   resolver := NewDefaultProfileResolver(detector, nil)
	//   env, err := resolver.Resolve(ctx, ProfileOptions{
	//       ExplicitProfile: "performance",
	//   })
	//   if err != nil {
	//       log.Fatalf("Profile resolution failed: %v", err)
	//   }
	//
	// # Limitations
	//
	//   - GPU detection requires nvidia-smi on Linux
	//   - macOS uses unified memory, not discrete GPU
	//
	// # Assumptions
	//
	//   - Hardware detection commands are available
	Resolve(ctx context.Context, opts ProfileOptions) (map[string]string, error)

	// DetectHardware returns detected hardware capabilities.
	//
	// # Description
	//
	// Probes the system for RAM, GPU VRAM, and CPU information.
	// Used internally by Resolve and exposed for diagnostics.
	//
	// # Inputs
	//
	//   - ctx: Context for cancellation
	//
	// # Outputs
	//
	//   - *HardwareInfo: Detected hardware capabilities
	//   - error: Non-nil if detection fails
	//
	// # Examples
	//
	//   hw, err := resolver.DetectHardware(ctx)
	//   if err != nil {
	//       log.Printf("Hardware detection failed: %v", err)
	//   }
	//   fmt.Printf("System RAM: %d MB\n", hw.SystemRAM_MB)
	//
	// # Limitations
	//
	//   - GPU detection not available on all platforms
	//
	// # Assumptions
	//
	//   - System commands (sysctl, nvidia-smi) are accessible
	DetectHardware(ctx context.Context) (*HardwareInfo, error)

	// GetProfileInfo returns information about a named profile.
	//
	// # Description
	//
	// Returns the configuration for a built-in or custom profile.
	//
	// # Inputs
	//
	//   - name: Profile name (low, standard, performance, ultra, or custom name)
	//
	// # Outputs
	//
	//   - *ProfileInfo: Profile configuration
	//   - bool: True if profile exists
	//
	// # Examples
	//
	//   info, exists := resolver.GetProfileInfo("performance")
	//   if exists {
	//       fmt.Printf("Model: %s\n", info.OllamaModel)
	//   }
	//
	// # Limitations
	//
	//   - None
	//
	// # Assumptions
	//
	//   - None
	GetProfileInfo(name string) (*ProfileInfo, bool)
}

// -----------------------------------------------------------------------------
// HardwareDetector Interface
// -----------------------------------------------------------------------------

// HardwareDetector probes system hardware capabilities.
//
// # Description
//
// Abstracts hardware detection for testability. The default implementation
// uses system commands; tests can inject mocks.
//
// # Thread Safety
//
// Implementations must be safe for concurrent use.
type HardwareDetector interface {
	// GetSystemMemory returns effective AI compute memory in MB.
	//
	// # Description
	//
	// On macOS: Returns unified memory
	// On Linux: Returns GPU VRAM if available, else system RAM
	// On Windows: Returns system RAM
	//
	// # Inputs
	//
	//   - ctx: Context for cancellation
	//
	// # Outputs
	//
	//   - int: Memory in megabytes
	//   - error: Non-nil if detection fails
	//
	// # Examples
	//
	//   mem, err := detector.GetSystemMemory(ctx)
	//
	// # Limitations
	//
	//   - Requires sysctl on macOS, nvidia-smi on Linux
	//
	// # Assumptions
	//
	//   - Commands are in PATH
	GetSystemMemory(ctx context.Context) (int, error)

	// GetGPUVRAM returns total GPU VRAM in MB.
	//
	// # Description
	//
	// Sums VRAM across all detected NVIDIA GPUs using nvidia-smi.
	// Returns 0 if no GPU or nvidia-smi unavailable.
	//
	// # Inputs
	//
	//   - ctx: Context for cancellation
	//
	// # Outputs
	//
	//   - int: GPU VRAM in megabytes (0 if none)
	//   - error: Non-nil if detection fails unexpectedly
	//
	// # Examples
	//
	//   vram, err := detector.GetGPUVRAM(ctx)
	//   if vram > 0 {
	//       fmt.Printf("GPU VRAM: %d MB\n", vram)
	//   }
	//
	// # Limitations
	//
	//   - Only detects NVIDIA GPUs
	//   - Not available on macOS
	//
	// # Assumptions
	//
	//   - nvidia-smi installed if GPU present
	GetGPUVRAM(ctx context.Context) (int, error)

	// GetCPUCores returns the number of CPU cores.
	//
	// # Description
	//
	// Returns the number of logical CPU cores available.
	//
	// # Inputs
	//
	//   - ctx: Context for cancellation
	//
	// # Outputs
	//
	//   - int: Number of CPU cores
	//   - error: Non-nil if detection fails
	//
	// # Examples
	//
	//   cores, _ := detector.GetCPUCores(ctx)
	//
	// # Limitations
	//
	//   - Returns logical cores, not physical
	//
	// # Assumptions
	//
	//   - None
	GetCPUCores(ctx context.Context) (int, error)

	// GetPlatform returns the current operating system.
	//
	// # Description
	//
	// Returns "darwin", "linux", or "windows".
	//
	// # Outputs
	//
	//   - string: OS identifier
	//
	// # Examples
	//
	//   platform := detector.GetPlatform()
	//
	// # Limitations
	//
	//   - None
	//
	// # Assumptions
	//
	//   - None
	GetPlatform() string
}

// -----------------------------------------------------------------------------
// Supporting Types
// -----------------------------------------------------------------------------

// ProfileOptions configures profile resolution.
//
// # Description
//
// Controls how the ProfileResolver selects and applies a profile.
type ProfileOptions struct {
	// ExplicitProfile is a user-specified profile name.
	// If set, overrides automatic detection.
	// Valid values: "low", "standard", "performance", "ultra", "manual", or custom profile name.
	ExplicitProfile string

	// BackendType is the model backend type.
	// Affects which environment variables are set.
	// Valid values: "ollama", "openai", "anthropic"
	BackendType string

	// CustomProfiles are user-defined profiles from config.
	// These extend or override built-in profiles.
	CustomProfiles []config.ProfileConfig
}

// HardwareInfo contains detected hardware capabilities.
//
// # Description
//
// Represents the detected hardware configuration used for profile selection.
type HardwareInfo struct {
	// SystemRAM_MB is total system RAM in megabytes.
	SystemRAM_MB int

	// GPUVRAM_MB is GPU video RAM in megabytes (0 if no GPU).
	GPUVRAM_MB int

	// GPUName is the GPU model name (empty if no GPU).
	GPUName string

	// CPUCores is the number of CPU cores.
	CPUCores int

	// Platform is the OS: "darwin", "linux", or "windows".
	Platform string

	// EffectiveMemory_MB is the memory used for profile selection.
	// This is VRAM on Linux with GPU, else system RAM.
	EffectiveMemory_MB int
}

// ProfileInfo describes a configuration profile.
//
// # Description
//
// Contains all configuration values for a single profile tier.
type ProfileInfo struct {
	// Name is the profile identifier.
	Name string

	// Description is a human-readable profile description.
	Description string

	// OllamaModel is the LLM model for this profile.
	OllamaModel string

	// MaxTokens is the context window size.
	MaxTokens int

	// RerankerModel is the reranking model.
	RerankerModel string

	// WeaviateQueryLimit is the default query result limit.
	WeaviateQueryLimit int

	// RerankFinalK is the number of results to re-rank.
	RerankFinalK int

	// MinRAM_MB is the minimum RAM required for this profile.
	MinRAM_MB int

	// MaxRAM_MB is the maximum RAM for this profile (0 = unlimited).
	MaxRAM_MB int
}

// -----------------------------------------------------------------------------
// Built-in Profiles
// -----------------------------------------------------------------------------

// builtInProfiles is populated from config.BuiltInHardwareProfiles.
// This ensures a single source of truth for model definitions.
var builtInProfiles = func() map[string]*ProfileInfo {
	profiles := make(map[string]*ProfileInfo)
	for name, hp := range config.BuiltInHardwareProfiles {
		profiles[name] = &ProfileInfo{
			Name:               hp.Name,
			Description:        hp.Description,
			OllamaModel:        hp.OllamaModel,
			MaxTokens:          hp.MaxTokens,
			RerankerModel:      hp.RerankerModel,
			WeaviateQueryLimit: hp.WeaviateQueryLimit,
			RerankFinalK:       hp.RerankFinalK,
			MinRAM_MB:          hp.MinRAM_MB,
			MaxRAM_MB:          hp.MaxRAM_MB,
		}
	}
	return profiles
}()

// -----------------------------------------------------------------------------
// DefaultProfileResolver Implementation
// -----------------------------------------------------------------------------

// DefaultProfileResolver implements ProfileResolver with hardware detection.
//
// # Description
//
// Uses HardwareDetector to probe system capabilities and selects the
// optimal profile based on detected memory.
//
// # Thread Safety
//
// DefaultProfileResolver is safe for concurrent use.
type DefaultProfileResolver struct {
	// detector probes hardware capabilities.
	detector HardwareDetector

	// customProfiles are user-defined profiles from config.
	customProfiles map[string]*ProfileInfo
}

// NewDefaultProfileResolver creates a profile resolver with the given detector.
//
// # Description
//
// Creates a resolver that uses the provided hardware detector. Custom profiles
// from config are converted and cached.
//
// # Inputs
//
//   - detector: HardwareDetector for probing system capabilities
//   - customProfiles: User-defined profiles from config (may be nil)
//
// # Outputs
//
//   - *DefaultProfileResolver: Ready-to-use resolver
//
// # Examples
//
//	detector := NewDefaultHardwareDetector(procMgr)
//	resolver := NewDefaultProfileResolver(detector, cfg.Profiles)
//	env, err := resolver.Resolve(ctx, ProfileOptions{})
//
// # Limitations
//
//   - None
//
// # Assumptions
//
//   - Detector is properly initialized
func NewDefaultProfileResolver(detector HardwareDetector, customProfiles []config.ProfileConfig) *DefaultProfileResolver {
	resolver := &DefaultProfileResolver{
		detector:       detector,
		customProfiles: make(map[string]*ProfileInfo),
	}

	// Convert config profiles to internal format
	for _, cp := range customProfiles {
		resolver.customProfiles[cp.Name] = &ProfileInfo{
			Name:          cp.Name,
			Description:   fmt.Sprintf("Custom profile: %s", cp.Name),
			OllamaModel:   cp.OllamaModel,
			MaxTokens:     cp.MaxTokens,
			RerankerModel: cp.RerankerModel,
			MinRAM_MB:     int(cp.MinRAM_MB),
		}
	}

	return resolver
}

// Resolve determines the optimal environment configuration.
//
// # Description
//
// Selects a profile based on explicit selection or hardware detection,
// then converts profile settings to environment variables.
//
// # Inputs
//
//   - ctx: Context for cancellation
//   - opts: Resolution options including explicit profile
//
// # Outputs
//
//   - map[string]string: Environment variables for containers
//   - error: Non-nil if detection or lookup fails
//
// # Examples
//
//	env, err := resolver.Resolve(ctx, ProfileOptions{
//	    ExplicitProfile: "", // Auto-detect
//	    BackendType:     "ollama",
//	})
//
// # Limitations
//
//   - Manual profile returns empty env map
//
// # Assumptions
//
//   - HardwareDetector is functional
func (r *DefaultProfileResolver) Resolve(ctx context.Context, opts ProfileOptions) (map[string]string, error) {
	env := make(map[string]string)

	// Handle manual mode - no optimization
	if opts.ExplicitProfile == "manual" {
		return env, nil
	}

	// Handle non-ollama backends
	if opts.BackendType != "" && opts.BackendType != "ollama" {
		env["LLM_BACKEND_TYPE"] = opts.BackendType
		return env, nil
	}

	// Determine which profile to use
	var profile *ProfileInfo
	var profileName string

	if opts.ExplicitProfile != "" && opts.ExplicitProfile != "auto" {
		// Explicit profile requested (not auto-detection)
		profileName = opts.ExplicitProfile
		var exists bool
		profile, exists = r.GetProfileInfo(profileName)
		if !exists {
			return nil, fmt.Errorf("unknown profile: %s", profileName)
		}
	} else {
		// Auto-detect based on hardware
		hw, err := r.DetectHardware(ctx)
		if err != nil {
			// Fall back to low profile on detection failure
			profile = builtInProfiles["low"]
			profileName = "low"
		} else {
			profileName = r.selectProfileForRAM(hw.EffectiveMemory_MB)
			profile = builtInProfiles[profileName]
		}
	}

	// Convert profile to environment variables
	return r.profileToEnv(profile, profileName), nil
}

// DetectHardware returns detected hardware capabilities.
//
// # Description
//
// Probes system using the configured HardwareDetector.
//
// # Inputs
//
//   - ctx: Context for cancellation
//
// # Outputs
//
//   - *HardwareInfo: Detected capabilities
//   - error: Non-nil if detection fails
//
// # Examples
//
//	hw, err := resolver.DetectHardware(ctx)
//	fmt.Printf("Effective memory: %d MB\n", hw.EffectiveMemory_MB)
//
// # Limitations
//
//   - GPU detection only on Linux
//
// # Assumptions
//
//   - Detector is functional
func (r *DefaultProfileResolver) DetectHardware(ctx context.Context) (*HardwareInfo, error) {
	hw := &HardwareInfo{
		Platform: r.detector.GetPlatform(),
	}

	// Get system memory
	mem, err := r.detector.GetSystemMemory(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to detect system memory: %w", err)
	}
	hw.SystemRAM_MB = mem
	hw.EffectiveMemory_MB = mem

	// Try to get GPU VRAM (may not be available)
	vram, _ := r.detector.GetGPUVRAM(ctx)
	if vram > 0 {
		hw.GPUVRAM_MB = vram
		// On Linux, prefer GPU VRAM for profile selection
		if hw.Platform == "linux" {
			hw.EffectiveMemory_MB = vram
		}
	}

	// Get CPU cores
	cores, _ := r.detector.GetCPUCores(ctx)
	hw.CPUCores = cores

	return hw, nil
}

// GetProfileInfo returns information about a named profile.
//
// # Description
//
// Looks up profile in custom profiles first, then built-in profiles.
//
// # Inputs
//
//   - name: Profile name
//
// # Outputs
//
//   - *ProfileInfo: Profile configuration
//   - bool: True if found
//
// # Examples
//
//	info, exists := resolver.GetProfileInfo("ultra")
//
// # Limitations
//
//   - None
//
// # Assumptions
//
//   - None
func (r *DefaultProfileResolver) GetProfileInfo(name string) (*ProfileInfo, bool) {
	// Check custom profiles first
	if profile, exists := r.customProfiles[name]; exists {
		return profile, true
	}

	// Check built-in profiles
	if profile, exists := builtInProfiles[name]; exists {
		return profile, true
	}

	return nil, false
}

// selectProfileForRAM chooses a profile based on RAM amount.
//
// # Description
//
// Matches RAM to the appropriate tier threshold.
//
// # Inputs
//
//   - ramMB: Available RAM in megabytes
//
// # Outputs
//
//   - string: Profile name
//
// # Examples
//
//	profile := r.selectProfileForRAM(48000) // Returns "performance"
//
// # Limitations
//
//   - None
//
// # Assumptions
//
//   - Thresholds are sorted descending
func (r *DefaultProfileResolver) selectProfileForRAM(ramMB int) string {
	return config.GetProfileForRAM(ramMB)
}

// profileToEnv converts a profile to environment variables.
//
// # Description
//
// Maps profile fields to container environment variable names.
//
// # Inputs
//
//   - profile: Profile to convert
//   - name: Profile name for logging
//
// # Outputs
//
//   - map[string]string: Environment variables
//
// # Examples
//
//	env := r.profileToEnv(profile, "performance")
//
// # Limitations
//
//   - None
//
// # Assumptions
//
//   - Profile is not nil
func (r *DefaultProfileResolver) profileToEnv(profile *ProfileInfo, _ string) map[string]string {
	env := make(map[string]string)

	env["OLLAMA_MODEL"] = profile.OllamaModel
	env["LLM_DEFAULT_MAX_TOKENS"] = strconv.Itoa(profile.MaxTokens)
	env["RERANKER_MODEL"] = profile.RerankerModel

	if profile.WeaviateQueryLimit > 0 {
		env["WEAVIATE_QUERY_DEFAULTS_LIMIT"] = strconv.Itoa(profile.WeaviateQueryLimit)
	}

	if profile.RerankFinalK > 0 {
		env["RERANK_FINAL_K"] = strconv.Itoa(profile.RerankFinalK)
	}

	return env
}

// -----------------------------------------------------------------------------
// DefaultHardwareDetector Implementation
// -----------------------------------------------------------------------------

// DefaultHardwareDetector probes hardware using system commands.
//
// # Description
//
// Uses ProcessManager to run system commands for hardware detection.
// Supports macOS (sysctl) and Linux (nvidia-smi, /proc/meminfo).
//
// # Thread Safety
//
// DefaultHardwareDetector is safe for concurrent use.
type DefaultHardwareDetector struct {
	// proc executes system commands.
	proc process.Manager
}

// NewDefaultHardwareDetector creates a hardware detector with the given process manager.
//
// # Description
//
// Creates a detector that uses ProcessManager for system command execution.
//
// # Inputs
//
//   - proc: ProcessManager for command execution
//
// # Outputs
//
//   - *DefaultHardwareDetector: Ready-to-use detector
//
// # Examples
//
//	proc := NewDefaultProcessManager()
//	detector := NewDefaultHardwareDetector(proc)
//	mem, err := detector.GetSystemMemory(ctx)
//
// # Limitations
//
//   - None
//
// # Assumptions
//
//   - ProcessManager is functional
func NewDefaultHardwareDetector(proc process.Manager) *DefaultHardwareDetector {
	return &DefaultHardwareDetector{
		proc: proc,
	}
}

// GetSystemMemory returns effective AI compute memory in MB.
//
// # Description
//
// On macOS: Returns unified memory via sysctl
// On Linux: Prefers GPU VRAM, falls back to system RAM
// On Windows: Returns safe default (8GB)
//
// # Inputs
//
//   - ctx: Context for cancellation
//
// # Outputs
//
//   - int: Memory in megabytes
//   - error: Non-nil if detection fails
//
// # Examples
//
//	mem, err := detector.GetSystemMemory(ctx)
//	fmt.Printf("System memory: %d MB\n", mem)
//
// # Limitations
//
//   - Requires sysctl on macOS, /proc/meminfo on Linux
//
// # Assumptions
//
//   - Commands are available
func (d *DefaultHardwareDetector) GetSystemMemory(ctx context.Context) (int, error) {
	switch runtime.GOOS {
	case "darwin":
		return d.getMacOSMemory(ctx)
	case "linux":
		// Try GPU VRAM first
		vram, err := d.GetGPUVRAM(ctx)
		if err == nil && vram > 0 {
			return vram, nil
		}
		// Fall back to system RAM
		return d.getLinuxSystemRAM(ctx)
	default:
		// Safe default for Windows/other
		return 8192, nil
	}
}

// GetGPUVRAM returns total GPU VRAM in MB.
//
// # Description
//
// Sums VRAM across all NVIDIA GPUs using nvidia-smi.
//
// # Inputs
//
//   - ctx: Context for cancellation
//
// # Outputs
//
//   - int: GPU VRAM in megabytes
//   - error: Non-nil if nvidia-smi fails
//
// # Examples
//
//	vram, err := detector.GetGPUVRAM(ctx)
//
// # Limitations
//
//   - Only detects NVIDIA GPUs
//
// # Assumptions
//
//   - nvidia-smi installed if GPU present
func (d *DefaultHardwareDetector) GetGPUVRAM(ctx context.Context) (int, error) {
	output, err := d.proc.Run(ctx, "nvidia-smi", "--query-gpu=memory.total", "--format=csv,noheader,nounits")
	if err != nil {
		return 0, err
	}

	totalMemMB := 0
	scanner := bufio.NewScanner(bytes.NewReader(output))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		memVal, err := strconv.Atoi(line)
		if err == nil {
			totalMemMB += memVal
		}
	}

	return totalMemMB, nil
}

// GetCPUCores returns the number of CPU cores.
//
// # Description
//
// Uses runtime.NumCPU() for portability.
//
// # Inputs
//
//   - ctx: Context (unused but required for interface)
//
// # Outputs
//
//   - int: Number of logical CPU cores
//   - error: Always nil
//
// # Examples
//
//	cores, _ := detector.GetCPUCores(ctx)
//
// # Limitations
//
//   - Returns logical cores, not physical
//
// # Assumptions
//
//   - None
func (d *DefaultHardwareDetector) GetCPUCores(_ context.Context) (int, error) {
	return runtime.NumCPU(), nil
}

// GetPlatform returns the current operating system.
//
// # Description
//
// Returns runtime.GOOS value.
//
// # Outputs
//
//   - string: "darwin", "linux", or "windows"
//
// # Examples
//
//	platform := detector.GetPlatform()
//
// # Limitations
//
//   - None
//
// # Assumptions
//
//   - None
func (d *DefaultHardwareDetector) GetPlatform() string {
	return runtime.GOOS
}

// getMacOSMemory returns system memory on macOS.
//
// # Description
//
// Uses sysctl hw.memsize to get unified memory.
//
// # Inputs
//
//   - ctx: Context for cancellation
//
// # Outputs
//
//   - int: Memory in megabytes
//   - error: Non-nil if sysctl fails
//
// # Examples
//
//	mem, err := d.getMacOSMemory(ctx)
//
// # Limitations
//
//   - macOS only
//
// # Assumptions
//
//   - sysctl is available
func (d *DefaultHardwareDetector) getMacOSMemory(ctx context.Context) (int, error) {
	output, err := d.proc.Run(ctx, "sysctl", "-n", "hw.memsize")
	if err != nil {
		return 0, fmt.Errorf("sysctl failed: %w", err)
	}

	bytesStr := strings.TrimSpace(string(output))
	bytesVal, err := strconv.ParseInt(bytesStr, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse memory size: %w", err)
	}

	return int(bytesVal / 1024 / 1024), nil // Convert bytes to MB
}

// getLinuxSystemRAM returns system RAM on Linux.
//
// # Description
//
// Parses /proc/meminfo to find MemTotal.
//
// # Inputs
//
//   - ctx: Context (unused)
//
// # Outputs
//
//   - int: Memory in megabytes
//   - error: Non-nil if parsing fails
//
// # Examples
//
//	mem, err := d.getLinuxSystemRAM(ctx)
//
// # Limitations
//
//   - Linux only
//
// # Assumptions
//
//   - /proc/meminfo is readable
func (d *DefaultHardwareDetector) getLinuxSystemRAM(_ context.Context) (int, error) {
	file, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, err
	}
	defer func() { _ = file.Close() }()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "MemTotal:") {
			fields := strings.Fields(line)
			if len(fields) < 2 {
				return 0, fmt.Errorf("unexpected format in /proc/meminfo")
			}

			memkB, err := strconv.ParseInt(fields[1], 10, 64)
			if err != nil {
				return 0, err
			}

			return int(memkB / 1024), nil // Convert kB to MB
		}
	}

	return 0, fmt.Errorf("MemTotal not found in /proc/meminfo")
}

// -----------------------------------------------------------------------------
// Mock Hardware Detector for Testing
// -----------------------------------------------------------------------------

// MockHardwareDetector is a test double for HardwareDetector.
//
// # Description
//
// Provides configurable behavior for hardware detection in tests.
//
// # Thread Safety
//
// MockHardwareDetector is safe for concurrent use.
type MockHardwareDetector struct {
	// GetSystemMemoryFunc is called by GetSystemMemory.
	GetSystemMemoryFunc func(ctx context.Context) (int, error)

	// GetGPUVRAMFunc is called by GetGPUVRAM.
	GetGPUVRAMFunc func(ctx context.Context) (int, error)

	// GetCPUCoresFunc is called by GetCPUCores.
	GetCPUCoresFunc func(ctx context.Context) (int, error)

	// GetPlatformFunc is called by GetPlatform.
	GetPlatformFunc func() string
}

// NewMockHardwareDetector creates a mock with configurable defaults.
//
// # Description
//
// Creates a mock that returns sensible defaults (16GB RAM, no GPU, 8 cores, darwin).
//
// # Outputs
//
//   - *MockHardwareDetector: Ready-to-use mock
//
// # Examples
//
//	mock := NewMockHardwareDetector()
//	mock.GetSystemMemoryFunc = func(ctx context.Context) (int, error) {
//	    return 65536, nil // 64GB
//	}
//
// # Limitations
//
//   - None
//
// # Assumptions
//
//   - None
func NewMockHardwareDetector() *MockHardwareDetector {
	return &MockHardwareDetector{}
}

// GetSystemMemory invokes GetSystemMemoryFunc or returns default.
//
// # Description
//
// Calls configured function or returns 16384 MB (16GB).
//
// # Inputs
//
//   - ctx: Context
//
// # Outputs
//
//   - int: Memory in MB
//   - error: From function or nil
//
// # Examples
//
//	mem, err := mock.GetSystemMemory(ctx)
//
// # Limitations
//
//   - None
//
// # Assumptions
//
//   - None
func (m *MockHardwareDetector) GetSystemMemory(ctx context.Context) (int, error) {
	if m.GetSystemMemoryFunc != nil {
		return m.GetSystemMemoryFunc(ctx)
	}
	return 16384, nil // 16GB default
}

// GetGPUVRAM invokes GetGPUVRAMFunc or returns default.
//
// # Description
//
// Calls configured function or returns 0 (no GPU).
//
// # Inputs
//
//   - ctx: Context
//
// # Outputs
//
//   - int: VRAM in MB
//   - error: From function or nil
//
// # Examples
//
//	vram, err := mock.GetGPUVRAM(ctx)
//
// # Limitations
//
//   - None
//
// # Assumptions
//
//   - None
func (m *MockHardwareDetector) GetGPUVRAM(ctx context.Context) (int, error) {
	if m.GetGPUVRAMFunc != nil {
		return m.GetGPUVRAMFunc(ctx)
	}
	return 0, nil // No GPU default
}

// GetCPUCores invokes GetCPUCoresFunc or returns default.
//
// # Description
//
// Calls configured function or returns 8 cores.
//
// # Inputs
//
//   - ctx: Context
//
// # Outputs
//
//   - int: CPU cores
//   - error: From function or nil
//
// # Examples
//
//	cores, _ := mock.GetCPUCores(ctx)
//
// # Limitations
//
//   - None
//
// # Assumptions
//
//   - None
func (m *MockHardwareDetector) GetCPUCores(ctx context.Context) (int, error) {
	if m.GetCPUCoresFunc != nil {
		return m.GetCPUCoresFunc(ctx)
	}
	return 8, nil
}

// GetPlatform invokes GetPlatformFunc or returns default.
//
// # Description
//
// Calls configured function or returns "darwin".
//
// # Outputs
//
//   - string: Platform
//
// # Examples
//
//	platform := mock.GetPlatform()
//
// # Limitations
//
//   - None
//
// # Assumptions
//
//   - None
func (m *MockHardwareDetector) GetPlatform() string {
	if m.GetPlatformFunc != nil {
		return m.GetPlatformFunc()
	}
	return "darwin"
}

// -----------------------------------------------------------------------------
// Compile-time Interface Compliance Checks
// -----------------------------------------------------------------------------

var _ ProfileResolver = (*DefaultProfileResolver)(nil)
var _ HardwareDetector = (*DefaultHardwareDetector)(nil)
var _ HardwareDetector = (*MockHardwareDetector)(nil)
