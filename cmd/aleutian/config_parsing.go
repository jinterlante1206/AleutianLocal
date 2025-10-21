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

type Config struct {
	Target         string             `yaml:"target"` // "local or server"
	ServerHost     string             `yaml:"server_host"`
	ProjectRepoURL string             `yaml:"project_repo_url"`
	Cloud          CloudConfig        `yaml:"cloud"`
	Services       map[string]Service `yaml:"services"`
	Storage        StorageConfig      `yaml:"storage"`
}

type StorageConfig struct {
	GCS GCSConfig `yaml:"gcs"`
}

type GCSConfig struct {
	BucketName string   `yaml:"bucket_name"`
	Outputs    GCSPaths `yaml:"outputs"`
	Logs       GCSPaths `yaml:"logs"`
	Security   GCSPaths `yaml:"security"`
}

type GCSPaths struct {
	Code         string `yaml:"code"`
	Embedding    string `yaml:"embedding"`
	Orchestrator string `yaml:"orchestrator"`
	VectorDB     string `yaml:"vectordb"`
	ScanResults  string `yaml:"scan_results"`
}

type CloudConfig struct {
	Provider     string `yaml:"provider"`
	Region       string `yaml:"region"`
	GCPProjectID string `yaml:"gcp_project_id"`
	GCPRepoName  string `yaml:"gcp_repo_name"`
}

type Service struct {
	BuildPath            string  `yaml:"build_path"`
	ImageTag             string  `yaml:"image_tag"`
	Image                string  `yaml:"image"`
	Port                 int     `yaml:"port"`
	LogLevel             string  `yaml:"log_level"`
	ModelName            string  `yaml:"model_name"`
	GPUMemoryUtilization float64 `yaml:"gpu_memory_utilization"`
	MaxModelLen          int     `yaml:"max_model_len"`
}
