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
	"testing"

	"github.com/AleutianAI/AleutianFOSS/cmd/aleutian/config"
)

// =============================================================================
// TESTS FOR StackFactory
// =============================================================================

// TestNewDefaultStackFactory verifies factory constructor.
//
// # Description
//
// Tests that NewDefaultStackFactory returns a non-nil factory instance.
//
// # Inputs
//
// None.
//
// # Outputs
//
// Test passes if factory is created successfully.
func TestNewDefaultStackFactory(t *testing.T) {
	factory := NewDefaultStackFactory()
	if factory == nil {
		t.Fatal("NewDefaultStackFactory returned nil")
	}
}

// TestDefaultStackFactory_ImplementsInterface verifies interface compliance.
//
// # Description
//
// Compile-time test that DefaultStackFactory implements StackFactory interface.
//
// # Inputs
//
// None.
//
// # Outputs
//
// Compilation fails if interface not implemented.
func TestDefaultStackFactory_ImplementsInterface(t *testing.T) {
	var _ StackFactory = (*DefaultStackFactory)(nil)
}

// TestCreateProductionStackManager_Success verifies successful manager creation.
//
// # Description
//
// Tests that CreateProductionStackManager creates a valid StackManager
// with all dependencies wired correctly when given valid configuration.
//
// # Inputs
//
// Valid AleutianConfig, stack directory, and CLI version.
//
// # Outputs
//
// Test passes if manager is created without error.
func TestCreateProductionStackManager_Success(t *testing.T) {
	// Create minimal valid config
	cfg := &config.AleutianConfig{
		Machine: config.MachineConfig{
			Id:     "test-machine",
			Drives: []string{"/tmp/test"},
		},
		ModelBackend: config.BackendConfig{
			Type: "ollama",
			Ollama: config.OllamaConfig{
				BaseURL:        "http://localhost:11434",
				EmbeddingModel: "nomic-embed-text",
				LLMModel:       "gpt-oss:latest",
			},
		},
		Secrets:  config.SecretsConfig{},
		Profiles: []config.ProfileConfig{},
	}

	stackDir := t.TempDir()
	cliVersion := "0.4.0-test"

	mgr, err := CreateProductionStackManager(cfg, stackDir, cliVersion)
	if err != nil {
		t.Fatalf("CreateProductionStackManager failed: %v", err)
	}
	if mgr == nil {
		t.Fatal("CreateProductionStackManager returned nil manager")
	}
}

// TestCreateProductionStackManager_NonOllamaBackend tests non-Ollama backends.
//
// # Description
//
// Tests that CreateProductionStackManager handles non-Ollama backends
// by creating a nil ModelEnsurer (which is valid behavior).
//
// # Inputs
//
// Config with non-Ollama backend type.
//
// # Outputs
//
// Test passes if manager is created without error.
func TestCreateProductionStackManager_NonOllamaBackend(t *testing.T) {
	cfg := &config.AleutianConfig{
		Machine: config.MachineConfig{
			Id: "test-machine",
		},
		ModelBackend: config.BackendConfig{
			Type: "cloud", // Not ollama
		},
		Secrets:  config.SecretsConfig{},
		Profiles: []config.ProfileConfig{},
	}

	stackDir := t.TempDir()
	cliVersion := "0.4.0-test"

	mgr, err := CreateProductionStackManager(cfg, stackDir, cliVersion)
	if err != nil {
		t.Fatalf("CreateProductionStackManager failed for non-Ollama backend: %v", err)
	}
	if mgr == nil {
		t.Fatal("CreateProductionStackManager returned nil manager")
	}
}

// TestDefaultStackFactory_CreateStackManager_ValidConfig tests factory method.
//
// # Description
//
// Tests that DefaultStackFactory.CreateStackManager creates managers
// correctly when invoked via the factory method.
//
// # Inputs
//
// Valid AleutianConfig, stack directory, and CLI version.
//
// # Outputs
//
// Test passes if manager is created without error.
func TestDefaultStackFactory_CreateStackManager_ValidConfig(t *testing.T) {
	factory := NewDefaultStackFactory()

	cfg := &config.AleutianConfig{
		Machine: config.MachineConfig{
			Id: "test-machine",
		},
		ModelBackend: config.BackendConfig{
			Type: "ollama",
			Ollama: config.OllamaConfig{
				BaseURL: "http://localhost:11434",
			},
		},
		Secrets:  config.SecretsConfig{},
		Profiles: []config.ProfileConfig{},
	}

	stackDir := t.TempDir()
	cliVersion := "0.4.0-test"

	mgr, err := factory.CreateStackManager(cfg, stackDir, cliVersion)
	if err != nil {
		t.Fatalf("CreateStackManager failed: %v", err)
	}
	if mgr == nil {
		t.Fatal("CreateStackManager returned nil manager")
	}
}

// =============================================================================
// TESTS FOR Individual Factory Methods
// =============================================================================

// TestDefaultStackFactory_createProcessManager tests process manager creation.
//
// # Description
//
// Tests that createProcessManager returns a non-nil process manager.
//
// # Inputs
//
// None.
//
// # Outputs
//
// Test passes if manager is created successfully.
func TestDefaultStackFactory_createProcessManager(t *testing.T) {
	factory := NewDefaultStackFactory()
	proc := factory.createProcessManager()
	if proc == nil {
		t.Fatal("createProcessManager returned nil")
	}
}

// TestDefaultStackFactory_createUserPrompter tests user prompter creation.
//
// # Description
//
// Tests that createUserPrompter returns a non-nil prompter.
//
// # Inputs
//
// None.
//
// # Outputs
//
// Test passes if prompter is created successfully.
func TestDefaultStackFactory_createUserPrompter(t *testing.T) {
	factory := NewDefaultStackFactory()
	prompter := factory.createUserPrompter()
	if prompter == nil {
		t.Fatal("createUserPrompter returned nil")
	}
}

// TestDefaultStackFactory_createDiagnosticsCollector tests collector creation.
//
// # Description
//
// Tests that createDiagnosticsCollector returns a non-nil collector
// when given a valid CLI version.
//
// # Inputs
//
// Valid CLI version string.
//
// # Outputs
//
// Test passes if collector is created without error.
func TestDefaultStackFactory_createDiagnosticsCollector(t *testing.T) {
	factory := NewDefaultStackFactory()
	collector, err := factory.createDiagnosticsCollector("0.4.0-test")
	if err != nil {
		t.Fatalf("createDiagnosticsCollector failed: %v", err)
	}
	if collector == nil {
		t.Fatal("createDiagnosticsCollector returned nil")
	}
}

// TestDefaultStackFactory_createDiagnosticsMetrics tests metrics creation.
//
// # Description
//
// Tests that createDiagnosticsMetrics returns a non-nil metrics recorder.
//
// # Inputs
//
// None.
//
// # Outputs
//
// Test passes if metrics recorder is created successfully.
func TestDefaultStackFactory_createDiagnosticsMetrics(t *testing.T) {
	factory := NewDefaultStackFactory()
	metrics := factory.createDiagnosticsMetrics()
	if metrics == nil {
		t.Fatal("createDiagnosticsMetrics returned nil")
	}
}

// TestDefaultStackFactory_createHealthChecker tests health checker creation.
//
// # Description
//
// Tests that createHealthChecker returns a non-nil health checker
// when given a valid process manager.
//
// # Inputs
//
// Valid process manager.
//
// # Outputs
//
// Test passes if health checker is created successfully.
func TestDefaultStackFactory_createHealthChecker(t *testing.T) {
	factory := NewDefaultStackFactory()
	proc := factory.createProcessManager()

	checker := factory.createHealthChecker(proc)
	if checker == nil {
		t.Fatal("createHealthChecker returned nil")
	}
}

// TestDefaultStackFactory_createModelEnsurer_Ollama tests Ollama model ensurer.
//
// # Description
//
// Tests that createModelEnsurer returns a non-nil ensurer when
// backend type is "ollama".
//
// # Inputs
//
// Config with Ollama backend.
//
// # Outputs
//
// Test passes if ensurer is created successfully.
func TestDefaultStackFactory_createModelEnsurer_Ollama(t *testing.T) {
	factory := NewDefaultStackFactory()
	cfg := &config.AleutianConfig{
		ModelBackend: config.BackendConfig{
			Type: "ollama",
			Ollama: config.OllamaConfig{
				BaseURL:        "http://localhost:11434",
				EmbeddingModel: "nomic-embed-text",
				LLMModel:       "gpt-oss:latest",
			},
		},
	}

	ensurer := factory.createModelEnsurer(cfg)
	if ensurer == nil {
		t.Fatal("createModelEnsurer returned nil for Ollama backend")
	}
}

// TestDefaultStackFactory_createModelEnsurer_NonOllama tests non-Ollama backend.
//
// # Description
//
// Tests that createModelEnsurer returns nil when backend type is not "ollama".
// This is expected behavior - non-Ollama backends don't need model ensurer.
//
// # Inputs
//
// Config with non-Ollama backend.
//
// # Outputs
//
// Test passes if ensurer is nil.
func TestDefaultStackFactory_createModelEnsurer_NonOllama(t *testing.T) {
	factory := NewDefaultStackFactory()
	cfg := &config.AleutianConfig{
		ModelBackend: config.BackendConfig{
			Type: "cloud",
		},
	}

	ensurer := factory.createModelEnsurer(cfg)
	if ensurer != nil {
		t.Fatal("createModelEnsurer should return nil for non-Ollama backend")
	}
}

// =============================================================================
// MOCK FACTORY FOR TESTING CLI HANDLERS
// =============================================================================

// MockStackFactory is a test double for StackFactory.
//
// Configure the mock by setting CreateStackManagerFunc before use.
// If the function is nil and CreateStackManager is called, it will panic.
type MockStackFactory struct {
	CreateStackManagerFunc func(cfg *config.AleutianConfig, stackDir, cliVersion string) (StackManager, error)
}

// CreateStackManager delegates to CreateStackManagerFunc.
func (m *MockStackFactory) CreateStackManager(cfg *config.AleutianConfig, stackDir, cliVersion string) (StackManager, error) {
	if m.CreateStackManagerFunc == nil {
		panic("MockStackFactory.CreateStackManagerFunc not set")
	}
	return m.CreateStackManagerFunc(cfg, stackDir, cliVersion)
}

// TestMockStackFactory_ImplementsInterface verifies mock interface compliance.
func TestMockStackFactory_ImplementsInterface(t *testing.T) {
	var _ StackFactory = (*MockStackFactory)(nil)
}

// =============================================================================
// BENCHMARK TESTS
// =============================================================================

// BenchmarkCreateProductionStackManager measures factory performance.
//
// # Description
//
// Benchmarks the time to create a StackManager with all dependencies.
// Useful for detecting performance regressions in factory code.
func BenchmarkCreateProductionStackManager(b *testing.B) {
	cfg := &config.AleutianConfig{
		Machine: config.MachineConfig{
			Id: "bench-machine",
		},
		ModelBackend: config.BackendConfig{
			Type: "ollama",
			Ollama: config.OllamaConfig{
				BaseURL: "http://localhost:11434",
			},
		},
		Secrets:  config.SecretsConfig{},
		Profiles: []config.ProfileConfig{},
	}

	stackDir := b.TempDir()
	cliVersion := "0.4.0-bench"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := CreateProductionStackManager(cfg, stackDir, cliVersion)
		if err != nil {
			b.Fatalf("CreateProductionStackManager failed: %v", err)
		}
	}
}
