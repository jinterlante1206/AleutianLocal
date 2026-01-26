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
	"crypto/sha256"
	"fmt"

	"github.com/AleutianAI/AleutianFOSS/services/policy_engine"
	"github.com/AleutianAI/AleutianFOSS/services/policy_engine/enforcement"
	"github.com/spf13/cobra"
)

// verifyPolicies is the CLI handler for the "aleutian policy verify" command.
//
// It retrieves the raw bytes of the embedded policy file from the enforcement package
// and calculates a SHA256 checksum.
//
// This allows operators to cryptographically verify that the binary they are running
// contains the expected version of the governance rules, ensuring that the policies
// have not been tampered with or accidentally swapped during the build process.
func verifyPolicies(cmd *cobra.Command, args []string) {
	data := enforcement.DataClassificationPatterns
	hash := sha256.Sum256(data)
	fmt.Println("--- Embedded Policy Verification ---")
	fmt.Printf("Policy byte size: %d bytes\n", len(data))
	fmt.Printf("SHA256 Fingerprint: %x\n", hash)
	fmt.Println("------------------------------------")
}

func dumpPolicies(cmd *cobra.Command, args []string) {
	fmt.Println(string(enforcement.DataClassificationPatterns))
}

func testPolicyString(cmd *cobra.Command, args []string) {
	inputString := args[0]
	engine, _ := policy_engine.NewPolicyEngine()
	findings := engine.ScanFileContent(inputString)
	if len(findings) > 0 {
		fmt.Println("Printing policy findings")
		for _, f := range findings {
			fmt.Println(f)
		}
	}
}
