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

	"github.com/AleutianAI/AleutianFOSS/cmd/aleutian/config"
)

var version = "dev"

func main() {
	log.Println("Starting up Aleutian Deployment")
	// Initialize the Aleutian config
	if err := config.Load(); err != nil {
		log.Fatalf("Failed to set the initial Aleutian state from the aleutian.yaml: %v", err)
	}
	log.Println("Starting the Aleutian Controller")
	// Execute the root command. Cobra handles parsing the arguments.
	if err := rootCmd.Execute(); err != nil {
		log.Fatalf("Error executing command: %v", err)
	}
}

func init() {
	rootCmd.Version = version
	rootCmd.Flags().BoolP("version", "v", false, "print the version number")
}
