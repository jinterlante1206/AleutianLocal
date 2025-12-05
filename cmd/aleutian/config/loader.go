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

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"gopkg.in/yaml.v3"
)

var (
	// Global is a singleton instance
	Global AleutianConfig
	once   sync.Once
)

// Load ensures the config is loaded into the Global variable
func Load() error {
	var err error
	once.Do(func() {
		err = loadInternal()
	})
	return err
}

func loadInternal() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("could not find the user's home directory: %w", err)
	}
	configPath := filepath.Join(home, ".aleutian", "aleutian.yaml")
	// create it if it doesn't exist
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		fmt.Printf(" First run detected, creating the config at %s\n", configPath)
		if err := createDefault(configPath); err != nil {
			return err
		}
	}
	// read the file
	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("failed to read the config file %w", err)
	}
	// parse the config in to the Global struct
	if err = yaml.Unmarshal(data, &Global); err != nil {
		return fmt.Errorf("failed to marshal the config to the Global singleton: %w", err)
	}
	return nil
}

func createDefault(path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create the config directory %w", err)
	}
	defaultCfg := DefaultConfig()
	data, err := yaml.Marshal(defaultCfg)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
