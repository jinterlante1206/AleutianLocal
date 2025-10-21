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
	"log"
	"os"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var config Config

func main() {
	log.Println("Starting up Aleutian Deployment")
	// Execute the root command. Cobra handles parsing the arguments.
	if err := rootCmd.Execute(); err != nil {
		log.Fatalf("Error executing command: %v", err)
	}
}

func init() {
	rootCmd.PersistentPreRun = func(cmd *cobra.Command, args []string) {
		configPath := "config.yaml"
		yamlFile, err := os.ReadFile(configPath)
		if err != nil {
			log.Fatalf("Error reading config.yaml: %v. Please ensure it exists.", err)
		}

		if err := yaml.Unmarshal(yamlFile, &config); err != nil {
			log.Fatalf("Error parsing config.yaml: %v", err)
		}
		log.Println("Configuration loaded successfully.")
	}
}
