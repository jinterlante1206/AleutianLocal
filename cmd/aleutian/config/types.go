// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package config

type AleutianConfig struct {
	// Infrastructure (Podman Machine)
	Machine MachineConfig `yaml:"machine"`

	// Extensions: Paths to custom docker/podman-compose files
	Extensions []string `yaml:"extensions"`

	// Secrets: Pointes to where secrets are stored like the keychain or env
	Secrets SecretsConfig `yaml:"secrets"`

	// Features: toggle for system services
	Features FeatureConfig `yaml:"features"`

	// ModelBackend: decides if you want local or cloud
	ModelBackend BackendConfig `yaml:"model_backend"`
}

type MachineConfig struct {
	Id           string   `yaml:"id"`            // e.g. podman-machine-default
	CPUCount     int      `yaml:"cpu_count"`     // e.g. 6
	MemoryAmount int      `yaml:"memory_amount"` // e.g. 20480
	Drives       []string `yaml:"drives"`        // e.g. ["/Volumes/ai_models"]
}

type SecretsConfig struct {
	UseEnv bool `yaml:"use_env"`
}

type FeatureConfig struct {
	Observability bool `yaml:"observability"`
	RagEngine     bool `yaml:"rag_engine"`
}

type BackendConfig struct {
	// Type can be "ollama", "openai", "anthropic", "remote_tgi", etc.
	Type    string `yaml:"type"`
	BaseURL string `yaml:"base_url,omitempty"`
}

func DefaultConfig() AleutianConfig {
	return AleutianConfig{
		Machine: MachineConfig{
			Id:           "podman-machine-default",
			CPUCount:     6,
			MemoryAmount: 20480,
			Drives:       []string{"/Volumes", "/Users"},
		},
		Extensions: []string{},
		Secrets:    SecretsConfig{UseEnv: false},
		Features: FeatureConfig{
			Observability: true,
			RagEngine:     true,
		},
		ModelBackend: BackendConfig{
			Type:    "ollama",
			BaseURL: "http://host.containers.internal:11434",
		},
	}
}
