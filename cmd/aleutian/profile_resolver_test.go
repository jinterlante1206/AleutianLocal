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
Package main provides tests for ProfileResolver.

These tests verify:
  - Profile resolution based on hardware
  - Hardware detection abstraction
  - Custom profile support
  - Edge cases and error handling
*/
package main

import (
	"context"
	"errors"
	"runtime"
	"testing"

	"github.com/jinterlante1206/AleutianLocal/cmd/aleutian/config"
)

// =============================================================================
// MockHardwareDetector Tests
// =============================================================================

// TestNewMockHardwareDetector verifies mock creation.
//
// # Description
//
// Creates a mock and verifies it returns sensible defaults.
func TestNewMockHardwareDetector(t *testing.T) {
	t.Parallel()

	mock := NewMockHardwareDetector()

	if mock == nil {
		t.Fatal("expected non-nil mock")
	}

	// Verify no functions are set initially
	if mock.GetSystemMemoryFunc != nil {
		t.Error("expected GetSystemMemoryFunc to be nil initially")
	}
	if mock.GetGPUVRAMFunc != nil {
		t.Error("expected GetGPUVRAMFunc to be nil initially")
	}
	if mock.GetCPUCoresFunc != nil {
		t.Error("expected GetCPUCoresFunc to be nil initially")
	}
	if mock.GetPlatformFunc != nil {
		t.Error("expected GetPlatformFunc to be nil initially")
	}
}

// TestMockHardwareDetector_DefaultValues verifies default return values.
//
// # Description
//
// Calls each method without setting functions and verifies defaults.
func TestMockHardwareDetector_DefaultValues(t *testing.T) {
	t.Parallel()

	mock := NewMockHardwareDetector()
	ctx := context.Background()

	// GetSystemMemory default: 16GB
	mem, err := mock.GetSystemMemory(ctx)
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
	if mem != 16384 {
		t.Errorf("expected 16384 MB, got: %d", mem)
	}

	// GetGPUVRAM default: 0 (no GPU)
	vram, err := mock.GetGPUVRAM(ctx)
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
	if vram != 0 {
		t.Errorf("expected 0 MB VRAM, got: %d", vram)
	}

	// GetCPUCores default: 8
	cores, err := mock.GetCPUCores(ctx)
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
	if cores != 8 {
		t.Errorf("expected 8 cores, got: %d", cores)
	}

	// GetPlatform default: darwin
	platform := mock.GetPlatform()
	if platform != "darwin" {
		t.Errorf("expected darwin, got: %s", platform)
	}
}

// TestMockHardwareDetector_CustomFunctions verifies custom function injection.
//
// # Description
//
// Sets custom functions and verifies they are called.
func TestMockHardwareDetector_CustomFunctions(t *testing.T) {
	t.Parallel()

	mock := NewMockHardwareDetector()
	ctx := context.Background()

	// Custom memory function
	mock.GetSystemMemoryFunc = func(ctx context.Context) (int, error) {
		return 65536, nil // 64GB
	}
	mem, err := mock.GetSystemMemory(ctx)
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
	if mem != 65536 {
		t.Errorf("expected 65536 MB, got: %d", mem)
	}

	// Custom VRAM function
	mock.GetGPUVRAMFunc = func(ctx context.Context) (int, error) {
		return 24576, nil // 24GB
	}
	vram, err := mock.GetGPUVRAM(ctx)
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
	if vram != 24576 {
		t.Errorf("expected 24576 MB VRAM, got: %d", vram)
	}

	// Custom cores function
	mock.GetCPUCoresFunc = func(ctx context.Context) (int, error) {
		return 32, nil
	}
	cores, err := mock.GetCPUCores(ctx)
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
	if cores != 32 {
		t.Errorf("expected 32 cores, got: %d", cores)
	}

	// Custom platform function
	mock.GetPlatformFunc = func() string {
		return "linux"
	}
	platform := mock.GetPlatform()
	if platform != "linux" {
		t.Errorf("expected linux, got: %s", platform)
	}
}

// TestMockHardwareDetector_ErrorReturns verifies error propagation.
//
// # Description
//
// Sets functions that return errors and verifies they propagate.
func TestMockHardwareDetector_ErrorReturns(t *testing.T) {
	t.Parallel()

	mock := NewMockHardwareDetector()
	ctx := context.Background()
	testErr := errors.New("test error")

	// Memory error
	mock.GetSystemMemoryFunc = func(ctx context.Context) (int, error) {
		return 0, testErr
	}
	_, err := mock.GetSystemMemory(ctx)
	if err != testErr {
		t.Errorf("expected test error, got: %v", err)
	}

	// VRAM error
	mock.GetGPUVRAMFunc = func(ctx context.Context) (int, error) {
		return 0, testErr
	}
	_, err = mock.GetGPUVRAM(ctx)
	if err != testErr {
		t.Errorf("expected test error, got: %v", err)
	}

	// Cores error
	mock.GetCPUCoresFunc = func(ctx context.Context) (int, error) {
		return 0, testErr
	}
	_, err = mock.GetCPUCores(ctx)
	if err != testErr {
		t.Errorf("expected test error, got: %v", err)
	}
}

// =============================================================================
// DefaultHardwareDetector Tests
// =============================================================================

// TestDefaultHardwareDetector_GetPlatform verifies platform detection.
//
// # Description
//
// Verifies GetPlatform returns runtime.GOOS.
func TestDefaultHardwareDetector_GetPlatform(t *testing.T) {
	t.Parallel()

	proc := &MockProcessManager{}
	detector := NewDefaultHardwareDetector(proc)

	platform := detector.GetPlatform()
	if platform != runtime.GOOS {
		t.Errorf("expected %s, got: %s", runtime.GOOS, platform)
	}
}

// TestDefaultHardwareDetector_GetCPUCores verifies CPU core detection.
//
// # Description
//
// Verifies GetCPUCores returns runtime.NumCPU().
func TestDefaultHardwareDetector_GetCPUCores(t *testing.T) {
	t.Parallel()

	proc := &MockProcessManager{}
	detector := NewDefaultHardwareDetector(proc)

	cores, err := detector.GetCPUCores(context.Background())
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
	if cores != runtime.NumCPU() {
		t.Errorf("expected %d cores, got: %d", runtime.NumCPU(), cores)
	}
}

// TestDefaultHardwareDetector_GetGPUVRAM_Success verifies VRAM detection.
//
// # Description
//
// Mocks nvidia-smi output and verifies parsing.
func TestDefaultHardwareDetector_GetGPUVRAM_Success(t *testing.T) {
	t.Parallel()

	proc := &MockProcessManager{}
	proc.RunFunc = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if name == "nvidia-smi" {
			// Simulate two GPUs
			return []byte("12288\n12288\n"), nil
		}
		return nil, errors.New("unknown command")
	}

	detector := NewDefaultHardwareDetector(proc)
	vram, err := detector.GetGPUVRAM(context.Background())
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
	if vram != 24576 {
		t.Errorf("expected 24576 MB, got: %d", vram)
	}
}

// TestDefaultHardwareDetector_GetGPUVRAM_NoGPU verifies no GPU handling.
//
// # Description
//
// Mocks nvidia-smi failure to simulate no GPU.
func TestDefaultHardwareDetector_GetGPUVRAM_NoGPU(t *testing.T) {
	t.Parallel()

	proc := &MockProcessManager{}
	proc.RunFunc = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return nil, errors.New("nvidia-smi not found")
	}

	detector := NewDefaultHardwareDetector(proc)
	_, err := detector.GetGPUVRAM(context.Background())
	if err == nil {
		t.Error("expected error for no GPU")
	}
}

// TestDefaultHardwareDetector_GetGPUVRAM_EmptyOutput verifies empty output handling.
//
// # Description
//
// Mocks nvidia-smi returning empty output.
func TestDefaultHardwareDetector_GetGPUVRAM_EmptyOutput(t *testing.T) {
	t.Parallel()

	proc := &MockProcessManager{}
	proc.RunFunc = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return []byte(""), nil
	}

	detector := NewDefaultHardwareDetector(proc)
	vram, err := detector.GetGPUVRAM(context.Background())
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
	if vram != 0 {
		t.Errorf("expected 0 MB, got: %d", vram)
	}
}

// TestDefaultHardwareDetector_GetGPUVRAM_InvalidOutput verifies invalid output handling.
//
// # Description
//
// Mocks nvidia-smi returning invalid data.
func TestDefaultHardwareDetector_GetGPUVRAM_InvalidOutput(t *testing.T) {
	t.Parallel()

	proc := &MockProcessManager{}
	proc.RunFunc = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return []byte("invalid\nnot_a_number\n"), nil
	}

	detector := NewDefaultHardwareDetector(proc)
	vram, err := detector.GetGPUVRAM(context.Background())
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
	// Invalid lines should be skipped, resulting in 0
	if vram != 0 {
		t.Errorf("expected 0 MB, got: %d", vram)
	}
}

// =============================================================================
// DefaultProfileResolver Tests
// =============================================================================

// TestNewDefaultProfileResolver_NoCustomProfiles verifies creation without custom profiles.
//
// # Description
//
// Creates resolver with nil custom profiles.
func TestNewDefaultProfileResolver_NoCustomProfiles(t *testing.T) {
	t.Parallel()

	mock := NewMockHardwareDetector()
	resolver := NewDefaultProfileResolver(mock, nil)

	if resolver == nil {
		t.Fatal("expected non-nil resolver")
	}
	if resolver.detector != mock {
		t.Error("expected detector to be set")
	}
	if len(resolver.customProfiles) != 0 {
		t.Errorf("expected empty custom profiles, got: %d", len(resolver.customProfiles))
	}
}

// TestNewDefaultProfileResolver_WithCustomProfiles verifies custom profile conversion.
//
// # Description
//
// Creates resolver with custom profiles and verifies conversion.
func TestNewDefaultProfileResolver_WithCustomProfiles(t *testing.T) {
	t.Parallel()

	mock := NewMockHardwareDetector()
	customProfiles := []config.ProfileConfig{
		{
			Name:          "enterprise",
			OllamaModel:   "mixtral:8x7b",
			MaxTokens:     16384,
			RerankerModel: "custom-reranker",
			MinRAM_MB:     48000,
		},
	}

	resolver := NewDefaultProfileResolver(mock, customProfiles)

	if len(resolver.customProfiles) != 1 {
		t.Fatalf("expected 1 custom profile, got: %d", len(resolver.customProfiles))
	}

	profile, exists := resolver.customProfiles["enterprise"]
	if !exists {
		t.Fatal("expected enterprise profile to exist")
	}
	if profile.OllamaModel != "mixtral:8x7b" {
		t.Errorf("expected mixtral:8x7b, got: %s", profile.OllamaModel)
	}
	if profile.MaxTokens != 16384 {
		t.Errorf("expected 16384 tokens, got: %d", profile.MaxTokens)
	}
}

// TestDefaultProfileResolver_GetProfileInfo_BuiltIn verifies built-in profile lookup.
//
// # Description
//
// Looks up each built-in profile and verifies properties.
func TestDefaultProfileResolver_GetProfileInfo_BuiltIn(t *testing.T) {
	t.Parallel()

	mock := NewMockHardwareDetector()
	resolver := NewDefaultProfileResolver(mock, nil)

	testCases := []struct {
		name      string
		model     string
		maxTokens int
		minRAM    int
		maxRAM    int
	}{
		{"low", "gemma3:4b", 2048, 0, 16384},
		{"standard", "gemma3:12b", 4096, 16384, 32768},
		{"performance", "gpt-oss:20b", 8192, 32768, 65536},
		{"ultra", "llama3:70b", 32768, 65536, 0},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			profile, exists := resolver.GetProfileInfo(tc.name)
			if !exists {
				t.Fatalf("expected %s profile to exist", tc.name)
			}
			if profile.OllamaModel != tc.model {
				t.Errorf("expected model %s, got: %s", tc.model, profile.OllamaModel)
			}
			if profile.MaxTokens != tc.maxTokens {
				t.Errorf("expected %d tokens, got: %d", tc.maxTokens, profile.MaxTokens)
			}
			if profile.MinRAM_MB != tc.minRAM {
				t.Errorf("expected min RAM %d, got: %d", tc.minRAM, profile.MinRAM_MB)
			}
			if profile.MaxRAM_MB != tc.maxRAM {
				t.Errorf("expected max RAM %d, got: %d", tc.maxRAM, profile.MaxRAM_MB)
			}
		})
	}
}

// TestDefaultProfileResolver_GetProfileInfo_Custom verifies custom profile lookup.
//
// # Description
//
// Verifies custom profiles can be looked up.
func TestDefaultProfileResolver_GetProfileInfo_Custom(t *testing.T) {
	t.Parallel()

	mock := NewMockHardwareDetector()
	customProfiles := []config.ProfileConfig{
		{
			Name:        "custom-test",
			OllamaModel: "custom-model:7b",
			MaxTokens:   4096,
		},
	}
	resolver := NewDefaultProfileResolver(mock, customProfiles)

	profile, exists := resolver.GetProfileInfo("custom-test")
	if !exists {
		t.Fatal("expected custom-test profile to exist")
	}
	if profile.OllamaModel != "custom-model:7b" {
		t.Errorf("expected custom-model:7b, got: %s", profile.OllamaModel)
	}
}

// TestDefaultProfileResolver_GetProfileInfo_CustomOverridesBuiltIn verifies priority.
//
// # Description
//
// Verifies custom profiles take precedence over built-in.
func TestDefaultProfileResolver_GetProfileInfo_CustomOverridesBuiltIn(t *testing.T) {
	t.Parallel()

	mock := NewMockHardwareDetector()
	// Custom profile with same name as built-in
	customProfiles := []config.ProfileConfig{
		{
			Name:        "low",
			OllamaModel: "custom-low:2b",
			MaxTokens:   1024,
		},
	}
	resolver := NewDefaultProfileResolver(mock, customProfiles)

	profile, exists := resolver.GetProfileInfo("low")
	if !exists {
		t.Fatal("expected low profile to exist")
	}
	// Should return custom version
	if profile.OllamaModel != "custom-low:2b" {
		t.Errorf("expected custom-low:2b, got: %s", profile.OllamaModel)
	}
}

// TestDefaultProfileResolver_GetProfileInfo_Unknown verifies unknown profile handling.
//
// # Description
//
// Verifies unknown profile returns false.
func TestDefaultProfileResolver_GetProfileInfo_Unknown(t *testing.T) {
	t.Parallel()

	mock := NewMockHardwareDetector()
	resolver := NewDefaultProfileResolver(mock, nil)

	_, exists := resolver.GetProfileInfo("nonexistent")
	if exists {
		t.Error("expected nonexistent profile to not exist")
	}
}

// TestDefaultProfileResolver_Resolve_ExplicitProfile verifies explicit profile selection.
//
// # Description
//
// Sets an explicit profile and verifies it's used.
func TestDefaultProfileResolver_Resolve_ExplicitProfile(t *testing.T) {
	t.Parallel()

	mock := NewMockHardwareDetector()
	resolver := NewDefaultProfileResolver(mock, nil)

	env, err := resolver.Resolve(context.Background(), ProfileOptions{
		ExplicitProfile: "performance",
	})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if env["OLLAMA_MODEL"] != "gpt-oss:20b" {
		t.Errorf("expected gpt-oss:20b, got: %s", env["OLLAMA_MODEL"])
	}
	if env["LLM_DEFAULT_MAX_TOKENS"] != "8192" {
		t.Errorf("expected 8192, got: %s", env["LLM_DEFAULT_MAX_TOKENS"])
	}
}

// TestDefaultProfileResolver_Resolve_UnknownProfile verifies unknown profile error.
//
// # Description
//
// Sets an unknown profile and verifies error.
func TestDefaultProfileResolver_Resolve_UnknownProfile(t *testing.T) {
	t.Parallel()

	mock := NewMockHardwareDetector()
	resolver := NewDefaultProfileResolver(mock, nil)

	_, err := resolver.Resolve(context.Background(), ProfileOptions{
		ExplicitProfile: "nonexistent",
	})
	if err == nil {
		t.Error("expected error for unknown profile")
	}
}

// TestDefaultProfileResolver_Resolve_ManualMode verifies manual mode returns empty env.
//
// # Description
//
// Sets manual mode and verifies empty environment.
func TestDefaultProfileResolver_Resolve_ManualMode(t *testing.T) {
	t.Parallel()

	mock := NewMockHardwareDetector()
	resolver := NewDefaultProfileResolver(mock, nil)

	env, err := resolver.Resolve(context.Background(), ProfileOptions{
		ExplicitProfile: "manual",
	})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if len(env) != 0 {
		t.Errorf("expected empty env for manual mode, got: %d entries", len(env))
	}
}

// TestDefaultProfileResolver_Resolve_NonOllamaBackend verifies non-ollama backend handling.
//
// # Description
//
// Sets a non-ollama backend and verifies only backend type is set.
func TestDefaultProfileResolver_Resolve_NonOllamaBackend(t *testing.T) {
	t.Parallel()

	mock := NewMockHardwareDetector()
	resolver := NewDefaultProfileResolver(mock, nil)

	testCases := []string{"openai", "anthropic"}
	for _, backend := range testCases {
		t.Run(backend, func(t *testing.T) {
			env, err := resolver.Resolve(context.Background(), ProfileOptions{
				BackendType: backend,
			})
			if err != nil {
				t.Fatalf("expected no error, got: %v", err)
			}
			if env["LLM_BACKEND_TYPE"] != backend {
				t.Errorf("expected %s, got: %s", backend, env["LLM_BACKEND_TYPE"])
			}
			// Should not have ollama-specific vars
			if _, hasModel := env["OLLAMA_MODEL"]; hasModel {
				t.Error("expected no OLLAMA_MODEL for non-ollama backend")
			}
		})
	}
}

// TestDefaultProfileResolver_Resolve_AutoDetect verifies auto-detection based on RAM.
//
// # Description
//
// Tests auto-detection for various RAM levels.
func TestDefaultProfileResolver_Resolve_AutoDetect(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name          string
		ramMB         int
		expectedModel string
	}{
		{"low_8gb", 8192, "gemma3:4b"},
		{"low_15gb", 15000, "gemma3:4b"},
		{"standard_16gb", 16384, "gemma3:12b"},
		{"standard_24gb", 24576, "gemma3:12b"},
		{"performance_32gb", 32768, "gpt-oss:20b"},
		{"performance_48gb", 49152, "gpt-oss:20b"},
		{"ultra_64gb", 65536, "llama3:70b"},
		{"ultra_128gb", 131072, "llama3:70b"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			mock := NewMockHardwareDetector()
			mock.GetSystemMemoryFunc = func(ctx context.Context) (int, error) {
				return tc.ramMB, nil
			}
			resolver := NewDefaultProfileResolver(mock, nil)

			env, err := resolver.Resolve(context.Background(), ProfileOptions{})
			if err != nil {
				t.Fatalf("expected no error, got: %v", err)
			}
			if env["OLLAMA_MODEL"] != tc.expectedModel {
				t.Errorf("expected %s, got: %s", tc.expectedModel, env["OLLAMA_MODEL"])
			}
		})
	}
}

// TestDefaultProfileResolver_Resolve_DetectionFailure verifies fallback on detection failure.
//
// # Description
//
// Simulates hardware detection failure and verifies fallback to low.
func TestDefaultProfileResolver_Resolve_DetectionFailure(t *testing.T) {
	t.Parallel()

	mock := NewMockHardwareDetector()
	mock.GetSystemMemoryFunc = func(ctx context.Context) (int, error) {
		return 0, errors.New("detection failed")
	}
	resolver := NewDefaultProfileResolver(mock, nil)

	env, err := resolver.Resolve(context.Background(), ProfileOptions{})
	if err != nil {
		t.Fatalf("expected no error (should fallback), got: %v", err)
	}
	// Should fall back to low profile
	if env["OLLAMA_MODEL"] != "gemma3:4b" {
		t.Errorf("expected gemma3:4b (low profile), got: %s", env["OLLAMA_MODEL"])
	}
}

// TestDefaultProfileResolver_DetectHardware verifies hardware detection.
//
// # Description
//
// Tests hardware detection with various configurations.
func TestDefaultProfileResolver_DetectHardware(t *testing.T) {
	t.Parallel()

	mock := NewMockHardwareDetector()
	mock.GetSystemMemoryFunc = func(ctx context.Context) (int, error) {
		return 32768, nil
	}
	mock.GetGPUVRAMFunc = func(ctx context.Context) (int, error) {
		return 24576, nil
	}
	mock.GetCPUCoresFunc = func(ctx context.Context) (int, error) {
		return 16, nil
	}
	mock.GetPlatformFunc = func() string {
		return "darwin"
	}

	resolver := NewDefaultProfileResolver(mock, nil)
	hw, err := resolver.DetectHardware(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if hw.SystemRAM_MB != 32768 {
		t.Errorf("expected 32768 MB RAM, got: %d", hw.SystemRAM_MB)
	}
	if hw.GPUVRAM_MB != 24576 {
		t.Errorf("expected 24576 MB VRAM, got: %d", hw.GPUVRAM_MB)
	}
	if hw.CPUCores != 16 {
		t.Errorf("expected 16 cores, got: %d", hw.CPUCores)
	}
	if hw.Platform != "darwin" {
		t.Errorf("expected darwin, got: %s", hw.Platform)
	}
	// On darwin, effective memory is system RAM (unified memory)
	if hw.EffectiveMemory_MB != 32768 {
		t.Errorf("expected effective memory 32768 MB, got: %d", hw.EffectiveMemory_MB)
	}
}

// TestDefaultProfileResolver_DetectHardware_LinuxGPU verifies GPU preference on Linux.
//
// # Description
//
// On Linux with GPU, effective memory should be GPU VRAM.
func TestDefaultProfileResolver_DetectHardware_LinuxGPU(t *testing.T) {
	t.Parallel()

	mock := NewMockHardwareDetector()
	mock.GetSystemMemoryFunc = func(ctx context.Context) (int, error) {
		return 32768, nil // 32GB system RAM
	}
	mock.GetGPUVRAMFunc = func(ctx context.Context) (int, error) {
		return 24576, nil // 24GB VRAM
	}
	mock.GetPlatformFunc = func() string {
		return "linux"
	}

	resolver := NewDefaultProfileResolver(mock, nil)
	hw, err := resolver.DetectHardware(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	// On Linux with GPU, effective memory should be VRAM
	if hw.EffectiveMemory_MB != 24576 {
		t.Errorf("expected effective memory 24576 MB (VRAM), got: %d", hw.EffectiveMemory_MB)
	}
}

// TestDefaultProfileResolver_DetectHardware_LinuxNoGPU verifies fallback on Linux without GPU.
//
// # Description
//
// On Linux without GPU, effective memory should be system RAM.
func TestDefaultProfileResolver_DetectHardware_LinuxNoGPU(t *testing.T) {
	t.Parallel()

	mock := NewMockHardwareDetector()
	mock.GetSystemMemoryFunc = func(ctx context.Context) (int, error) {
		return 32768, nil
	}
	mock.GetGPUVRAMFunc = func(ctx context.Context) (int, error) {
		return 0, nil // No GPU
	}
	mock.GetPlatformFunc = func() string {
		return "linux"
	}

	resolver := NewDefaultProfileResolver(mock, nil)
	hw, err := resolver.DetectHardware(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	// Without GPU, effective memory should be system RAM
	if hw.EffectiveMemory_MB != 32768 {
		t.Errorf("expected effective memory 32768 MB (RAM), got: %d", hw.EffectiveMemory_MB)
	}
}

// TestDefaultProfileResolver_DetectHardware_MemoryError verifies error handling.
//
// # Description
//
// Simulates memory detection failure.
func TestDefaultProfileResolver_DetectHardware_MemoryError(t *testing.T) {
	t.Parallel()

	mock := NewMockHardwareDetector()
	mock.GetSystemMemoryFunc = func(ctx context.Context) (int, error) {
		return 0, errors.New("sysctl failed")
	}

	resolver := NewDefaultProfileResolver(mock, nil)
	_, err := resolver.DetectHardware(context.Background())
	if err == nil {
		t.Error("expected error for memory detection failure")
	}
}

// =============================================================================
// selectProfileForRAM Tests
// =============================================================================

// TestSelectProfileForRAM verifies RAM-to-profile mapping.
//
// # Description
//
// Tests boundary conditions for profile selection.
func TestSelectProfileForRAM(t *testing.T) {
	t.Parallel()

	mock := NewMockHardwareDetector()
	resolver := NewDefaultProfileResolver(mock, nil)

	testCases := []struct {
		ramMB           int
		expectedProfile string
	}{
		// Low tier: 0 - 16384
		{0, "low"},
		{8192, "low"},
		{16383, "low"},

		// Standard tier: 16384 - 32768
		{16384, "standard"},
		{24576, "standard"},
		{32767, "standard"},

		// Performance tier: 32768 - 65536
		{32768, "performance"},
		{49152, "performance"},
		{65535, "performance"},

		// Ultra tier: 65536+
		{65536, "ultra"},
		{131072, "ultra"},
		{262144, "ultra"},
	}

	for _, tc := range testCases {
		t.Run(tc.expectedProfile, func(t *testing.T) {
			profile := resolver.selectProfileForRAM(tc.ramMB)
			if profile != tc.expectedProfile {
				t.Errorf("for %d MB, expected %s, got: %s", tc.ramMB, tc.expectedProfile, profile)
			}
		})
	}
}

// =============================================================================
// profileToEnv Tests
// =============================================================================

// TestProfileToEnv verifies environment variable generation.
//
// # Description
//
// Tests conversion of profile to environment variables.
func TestProfileToEnv(t *testing.T) {
	t.Parallel()

	mock := NewMockHardwareDetector()
	resolver := NewDefaultProfileResolver(mock, nil)

	profile := &ProfileInfo{
		Name:               "test",
		OllamaModel:        "test-model:7b",
		MaxTokens:          4096,
		RerankerModel:      "test-reranker",
		WeaviateQueryLimit: 10,
		RerankFinalK:       15,
	}

	env := resolver.profileToEnv(profile, "test")

	if env["OLLAMA_MODEL"] != "test-model:7b" {
		t.Errorf("expected test-model:7b, got: %s", env["OLLAMA_MODEL"])
	}
	if env["LLM_DEFAULT_MAX_TOKENS"] != "4096" {
		t.Errorf("expected 4096, got: %s", env["LLM_DEFAULT_MAX_TOKENS"])
	}
	if env["RERANKER_MODEL"] != "test-reranker" {
		t.Errorf("expected test-reranker, got: %s", env["RERANKER_MODEL"])
	}
	if env["WEAVIATE_QUERY_DEFAULTS_LIMIT"] != "10" {
		t.Errorf("expected 10, got: %s", env["WEAVIATE_QUERY_DEFAULTS_LIMIT"])
	}
	if env["RERANK_FINAL_K"] != "15" {
		t.Errorf("expected 15, got: %s", env["RERANK_FINAL_K"])
	}
}

// TestProfileToEnv_ZeroValues verifies zero value handling.
//
// # Description
//
// Tests that zero values for optional fields don't create env vars.
func TestProfileToEnv_ZeroValues(t *testing.T) {
	t.Parallel()

	mock := NewMockHardwareDetector()
	resolver := NewDefaultProfileResolver(mock, nil)

	profile := &ProfileInfo{
		Name:               "minimal",
		OllamaModel:        "minimal:1b",
		MaxTokens:          1024,
		RerankerModel:      "minimal-reranker",
		WeaviateQueryLimit: 0, // Zero
		RerankFinalK:       0, // Zero
	}

	env := resolver.profileToEnv(profile, "minimal")

	// Required fields should be present
	if env["OLLAMA_MODEL"] != "minimal:1b" {
		t.Errorf("expected minimal:1b, got: %s", env["OLLAMA_MODEL"])
	}

	// Zero-value optional fields should not be present
	if _, exists := env["WEAVIATE_QUERY_DEFAULTS_LIMIT"]; exists {
		t.Error("expected WEAVIATE_QUERY_DEFAULTS_LIMIT to not be set for zero value")
	}
	if _, exists := env["RERANK_FINAL_K"]; exists {
		t.Error("expected RERANK_FINAL_K to not be set for zero value")
	}
}

// =============================================================================
// Interface Compliance Tests
// =============================================================================

// TestInterfaceCompliance verifies implementations satisfy interfaces.
//
// # Description
//
// Compile-time checks are in the main file, this is a runtime verification.
func TestInterfaceCompliance(t *testing.T) {
	t.Parallel()

	// These will fail at compile time if interfaces aren't satisfied,
	// but we include runtime checks for documentation.

	var _ ProfileResolver = (*DefaultProfileResolver)(nil)
	var _ HardwareDetector = (*DefaultHardwareDetector)(nil)
	var _ HardwareDetector = (*MockHardwareDetector)(nil)
}

// =============================================================================
// Concurrent Access Tests
// =============================================================================

// TestDefaultProfileResolver_ConcurrentResolve verifies thread safety.
//
// # Description
//
// Calls Resolve concurrently to verify no data races.
func TestDefaultProfileResolver_ConcurrentResolve(t *testing.T) {
	t.Parallel()

	mock := NewMockHardwareDetector()
	resolver := NewDefaultProfileResolver(mock, nil)

	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func() {
			_, err := resolver.Resolve(context.Background(), ProfileOptions{})
			if err != nil {
				t.Errorf("concurrent resolve failed: %v", err)
			}
			done <- true
		}()
	}

	for i := 0; i < 10; i++ {
		<-done
	}
}

// TestDefaultProfileResolver_ConcurrentDetectHardware verifies thread safety.
//
// # Description
//
// Calls DetectHardware concurrently to verify no data races.
func TestDefaultProfileResolver_ConcurrentDetectHardware(t *testing.T) {
	t.Parallel()

	mock := NewMockHardwareDetector()
	resolver := NewDefaultProfileResolver(mock, nil)

	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func() {
			_, err := resolver.DetectHardware(context.Background())
			if err != nil {
				t.Errorf("concurrent detect failed: %v", err)
			}
			done <- true
		}()
	}

	for i := 0; i < 10; i++ {
		<-done
	}
}

// TestMockHardwareDetector_ConcurrentAccess verifies mock thread safety.
//
// # Description
//
// Calls mock methods concurrently to verify no data races.
func TestMockHardwareDetector_ConcurrentAccess(t *testing.T) {
	t.Parallel()

	mock := NewMockHardwareDetector()
	ctx := context.Background()

	done := make(chan bool, 40)
	for i := 0; i < 10; i++ {
		go func() {
			_, _ = mock.GetSystemMemory(ctx)
			done <- true
		}()
		go func() {
			_, _ = mock.GetGPUVRAM(ctx)
			done <- true
		}()
		go func() {
			_, _ = mock.GetCPUCores(ctx)
			done <- true
		}()
		go func() {
			_ = mock.GetPlatform()
			done <- true
		}()
	}

	for i := 0; i < 40; i++ {
		<-done
	}
}
