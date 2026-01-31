// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package mcts

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDefaultMCTSFullConfig(t *testing.T) {
	config := DefaultMCTSFullConfig()

	// Verify budget defaults
	if config.Budget.MaxNodes != 10 {
		t.Errorf("Budget.MaxNodes = %d, want 10", config.Budget.MaxNodes)
	}
	if config.Budget.MaxDepth != 4 {
		t.Errorf("Budget.MaxDepth = %d, want 4", config.Budget.MaxDepth)
	}
	if config.Budget.CostLimitUSD != 0.10 {
		t.Errorf("Budget.CostLimitUSD = %f, want 0.10", config.Budget.CostLimitUSD)
	}

	// Verify algorithm defaults
	if config.Algorithm.ExplorationConstant != 1.41 {
		t.Errorf("Algorithm.ExplorationConstant = %f, want 1.41", config.Algorithm.ExplorationConstant)
	}

	// Verify observability defaults
	if !config.Observability.TracingEnabled {
		t.Error("Observability.TracingEnabled should be true by default")
	}
	if config.Observability.SampleRate != 1.0 {
		t.Errorf("Observability.SampleRate = %f, want 1.0", config.Observability.SampleRate)
	}
}

func TestMCTSFullConfig_Validate(t *testing.T) {
	tests := []struct {
		name      string
		modify    func(*MCTSFullConfig)
		wantError bool
	}{
		{
			name:      "valid default config",
			modify:    func(_ *MCTSFullConfig) {},
			wantError: false,
		},
		{
			name: "invalid max_nodes",
			modify: func(c *MCTSFullConfig) {
				c.Budget.MaxNodes = 0
			},
			wantError: true,
		},
		{
			name: "invalid max_depth",
			modify: func(c *MCTSFullConfig) {
				c.Budget.MaxDepth = 0
			},
			wantError: true,
		},
		{
			name: "invalid llm_call_limit",
			modify: func(c *MCTSFullConfig) {
				c.Budget.LLMCallLimit = 0
			},
			wantError: true,
		},
		{
			name: "invalid cost_limit_usd",
			modify: func(c *MCTSFullConfig) {
				c.Budget.CostLimitUSD = 0
			},
			wantError: true,
		},
		{
			name: "invalid exploration_constant",
			modify: func(c *MCTSFullConfig) {
				c.Algorithm.ExplorationConstant = 0
			},
			wantError: true,
		},
		{
			name: "sample_rate too high",
			modify: func(c *MCTSFullConfig) {
				c.Observability.SampleRate = 1.5
			},
			wantError: true,
		},
		{
			name: "sample_rate negative",
			modify: func(c *MCTSFullConfig) {
				c.Observability.SampleRate = -0.1
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := DefaultMCTSFullConfig()
			tt.modify(&config)
			err := config.Validate()
			if (err != nil) != tt.wantError {
				t.Errorf("Validate() error = %v, wantError = %v", err, tt.wantError)
			}
		})
	}
}

func TestLoadMCTSConfig_FromYAML(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	yamlContent := `
budget:
  max_nodes: 20
  max_depth: 6
  cost_limit_usd: 0.50

algorithm:
  exploration_constant: 2.0

observability:
  log_level: debug
  sample_rate: 0.5
`

	if err := os.WriteFile(configPath, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}

	config, err := LoadMCTSConfig(configPath)
	if err != nil {
		t.Fatalf("LoadMCTSConfig() error = %v", err)
	}

	if config.Budget.MaxNodes != 20 {
		t.Errorf("Budget.MaxNodes = %d, want 20", config.Budget.MaxNodes)
	}
	if config.Budget.MaxDepth != 6 {
		t.Errorf("Budget.MaxDepth = %d, want 6", config.Budget.MaxDepth)
	}
	if config.Budget.CostLimitUSD != 0.50 {
		t.Errorf("Budget.CostLimitUSD = %f, want 0.50", config.Budget.CostLimitUSD)
	}
	if config.Algorithm.ExplorationConstant != 2.0 {
		t.Errorf("Algorithm.ExplorationConstant = %f, want 2.0", config.Algorithm.ExplorationConstant)
	}
	if config.Observability.LogLevel != "debug" {
		t.Errorf("Observability.LogLevel = %s, want debug", config.Observability.LogLevel)
	}
}

func TestLoadMCTSConfig_FromJSON(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	jsonContent := `{
  "budget": {
    "max_nodes": 15,
    "llm_call_limit": 5
  },
  "parallel": {
    "enabled": true,
    "max_concurrency": 8
  }
}`

	if err := os.WriteFile(configPath, []byte(jsonContent), 0644); err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}

	config, err := LoadMCTSConfig(configPath)
	if err != nil {
		t.Fatalf("LoadMCTSConfig() error = %v", err)
	}

	if config.Budget.MaxNodes != 15 {
		t.Errorf("Budget.MaxNodes = %d, want 15", config.Budget.MaxNodes)
	}
	if config.Budget.LLMCallLimit != 5 {
		t.Errorf("Budget.LLMCallLimit = %d, want 5", config.Budget.LLMCallLimit)
	}
	if !config.Parallel.Enabled {
		t.Error("Parallel.Enabled should be true")
	}
	if config.Parallel.MaxConcurrency != 8 {
		t.Errorf("Parallel.MaxConcurrency = %d, want 8", config.Parallel.MaxConcurrency)
	}
}

func TestLoadMCTSConfig_EnvOverrides(t *testing.T) {
	// Save and restore env vars
	oldVars := map[string]string{
		"MCTS_MAX_NODES":            os.Getenv("MCTS_MAX_NODES"),
		"MCTS_EXPLORATION_CONSTANT": os.Getenv("MCTS_EXPLORATION_CONSTANT"),
		"MCTS_PARALLEL_ENABLED":     os.Getenv("MCTS_PARALLEL_ENABLED"),
		"MCTS_LOG_LEVEL":            os.Getenv("MCTS_LOG_LEVEL"),
	}
	defer func() {
		for k, v := range oldVars {
			if v == "" {
				os.Unsetenv(k)
			} else {
				os.Setenv(k, v)
			}
		}
	}()

	// Set env vars
	os.Setenv("MCTS_MAX_NODES", "25")
	os.Setenv("MCTS_EXPLORATION_CONSTANT", "1.5")
	os.Setenv("MCTS_PARALLEL_ENABLED", "true")
	os.Setenv("MCTS_LOG_LEVEL", "warn")

	config, err := LoadMCTSConfig("")
	if err != nil {
		t.Fatalf("LoadMCTSConfig() error = %v", err)
	}

	if config.Budget.MaxNodes != 25 {
		t.Errorf("Budget.MaxNodes = %d, want 25", config.Budget.MaxNodes)
	}
	if config.Algorithm.ExplorationConstant != 1.5 {
		t.Errorf("Algorithm.ExplorationConstant = %f, want 1.5", config.Algorithm.ExplorationConstant)
	}
	if !config.Parallel.Enabled {
		t.Error("Parallel.Enabled should be true from env")
	}
	if config.Observability.LogLevel != "warn" {
		t.Errorf("Observability.LogLevel = %s, want warn", config.Observability.LogLevel)
	}
}

func TestLoadMCTSConfig_MissingFile(t *testing.T) {
	// Non-existent file should return defaults
	config, err := LoadMCTSConfig("/nonexistent/path/config.yaml")
	if err != nil {
		t.Fatalf("LoadMCTSConfig() should not error for missing file: %v", err)
	}

	// Should have defaults
	if config.Budget.MaxNodes != 10 {
		t.Errorf("Should return default MaxNodes=10, got %d", config.Budget.MaxNodes)
	}
}

func TestLoadMCTSConfig_InvalidFile(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	// Write invalid content
	if err := os.WriteFile(configPath, []byte("not: valid: yaml: content:::"), 0644); err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}

	_, err := LoadMCTSConfig(configPath)
	if err == nil {
		t.Error("LoadMCTSConfig() should error for invalid file")
	}
}

func TestBudgetConfig_ToTreeBudgetConfig(t *testing.T) {
	budget := BudgetConfig{
		MaxNodes:      15,
		MaxDepth:      5,
		MaxExpansions: 4,
		TimeLimit:     1 * time.Minute,
		LLMTokenLimit: 10000,
		LLMCallLimit:  20,
		CostLimitUSD:  0.50,
	}

	treeBudget := budget.ToTreeBudgetConfig()

	if treeBudget.MaxNodes != 15 {
		t.Errorf("MaxNodes = %d, want 15", treeBudget.MaxNodes)
	}
	if treeBudget.MaxDepth != 5 {
		t.Errorf("MaxDepth = %d, want 5", treeBudget.MaxDepth)
	}
	if treeBudget.CostLimitUSD != 0.50 {
		t.Errorf("CostLimitUSD = %f, want 0.50", treeBudget.CostLimitUSD)
	}
}

func TestSimulationFullConfig_ToSimulatorConfig(t *testing.T) {
	simConfig := SimulationFullConfig{
		QuickScoreThreshold:    0.6,
		StandardScoreThreshold: 0.8,
		QuickTimeout:           10 * time.Second,
		StandardTimeout:        1 * time.Minute,
		FullTimeout:            5 * time.Minute,
		EnableSecurityScan:     true,
	}

	simulatorConfig := simConfig.ToSimulatorConfig()

	if simulatorConfig.QuickScoreThreshold != 0.6 {
		t.Errorf("QuickScoreThreshold = %f, want 0.6", simulatorConfig.QuickScoreThreshold)
	}
	if simulatorConfig.StandardScoreThreshold != 0.8 {
		t.Errorf("StandardScoreThreshold = %f, want 0.8", simulatorConfig.StandardScoreThreshold)
	}
	if simulatorConfig.QuickTimeout != 10*time.Second {
		t.Errorf("QuickTimeout = %v, want 10s", simulatorConfig.QuickTimeout)
	}
}

func TestDefaultPruningConfig(t *testing.T) {
	config := DefaultPruningConfig()

	if config.PruneInterval != 10 {
		t.Errorf("PruneInterval = %d, want 10", config.PruneInterval)
	}
	if config.KeepBestN != 3 {
		t.Errorf("KeepBestN = %d, want 3", config.KeepBestN)
	}
	if config.MaxAbandonedAge != 5*time.Minute {
		t.Errorf("MaxAbandonedAge = %v, want 5m", config.MaxAbandonedAge)
	}
}
