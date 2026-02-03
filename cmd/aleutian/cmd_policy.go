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
	"os"
	"strings"

	"github.com/AleutianAI/AleutianFOSS/services/policy_engine"
	"github.com/AleutianAI/AleutianFOSS/services/policy_engine/enforcement"
	"github.com/spf13/cobra"
)

// =============================================================================
// COMMAND FLAGS
// =============================================================================

var (
	policyVerifyJSON bool
	policyDumpJSON   bool
	policyTestJSON   bool
	policyTestRedact bool
)

// =============================================================================
// POLICY VERIFY COMMAND
// =============================================================================

// verifyPolicies is the CLI handler for the "aleutian policy verify" command.
//
// It retrieves the raw bytes of the embedded policy file from the enforcement package
// and calculates a SHA256 checksum.
//
// This allows operators to cryptographically verify that the binary they are running
// contains the expected version of the governance rules, ensuring that the policies
// have not been tampered with or accidentally swapped during the build process.
//
// # Exit Codes
//
//   - 0: Policy verified successfully
//   - 2: Error (should not happen for embedded policies)
func verifyPolicies(cmd *cobra.Command, args []string) {
	data := enforcement.DataClassificationPatterns
	hash := sha256.Sum256(data)
	hashStr := fmt.Sprintf("sha256:%x", hash)

	if policyVerifyJSON {
		result := PolicyVerifyResult{
			Valid:    true,
			Hash:     hashStr,
			ByteSize: len(data),
			Version:  "1.0",
		}
		if err := OutputJSON(result, false); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to encode JSON: %v\n", err)
			os.Exit(CLIExitError)
		}
		os.Exit(CLIExitSuccess)
	}

	fmt.Println("--- Embedded Policy Verification ---")
	fmt.Printf("Policy byte size: %d bytes\n", len(data))
	fmt.Printf("SHA256 Fingerprint: %x\n", hash)
	fmt.Println("------------------------------------")
}

// =============================================================================
// POLICY DUMP COMMAND
// =============================================================================

// dumpPolicies outputs the embedded policy rules.
//
// With --json, outputs the rules as a JSON object instead of YAML.
func dumpPolicies(cmd *cobra.Command, args []string) {
	data := enforcement.DataClassificationPatterns

	if policyDumpJSON {
		// Parse YAML and output as JSON
		// For now, wrap the raw data in a JSON structure
		result := struct {
			APIVersion string `json:"api_version"`
			Format     string `json:"format"`
			Content    string `json:"content"`
		}{
			APIVersion: "1.0",
			Format:     "yaml",
			Content:    string(data),
		}
		if err := OutputJSON(result, false); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to encode JSON: %v\n", err)
			os.Exit(CLIExitError)
		}
		os.Exit(CLIExitSuccess)
	}

	fmt.Println(string(data))
}

// =============================================================================
// POLICY TEST COMMAND
// =============================================================================

// testPolicyString tests a string against policy rules.
//
// # Exit Codes
//
//   - 0: No policy violations found
//   - 1: Policy violations found
//   - 2: Error
func testPolicyString(cmd *cobra.Command, args []string) {
	inputString := args[0]
	engine, err := policy_engine.NewPolicyEngine()
	if err != nil {
		OutputError(policyTestJSON, "Failed to create policy engine", err)
		os.Exit(CLIExitError)
	}

	findings := engine.ScanFileContent(inputString)
	hasFindings := len(findings) > 0

	if policyTestJSON {
		matches := make([]PolicyTestMatch, 0, len(findings))
		for _, f := range findings {
			match := f.MatchedContent
			if policyTestRedact {
				match = "[REDACTED]"
			}
			matches = append(matches, PolicyTestMatch{
				Rule:           f.PatternId,
				Severity:       confidenceToSeverityString(f.ClassificationName, f.Confidence),
				Match:          match,
				Classification: f.ClassificationName,
				Confidence:     string(f.Confidence),
				LineNumber:     f.LineNumber,
			})
		}

		result := PolicyTestResult{
			Input:   inputString,
			Matches: matches,
			Matched: hasFindings,
		}
		if err := OutputJSON(result, false); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to encode JSON: %v\n", err)
			os.Exit(CLIExitError)
		}
	} else {
		if hasFindings {
			fmt.Println("Policy findings:")
			for _, f := range findings {
				fmt.Printf("  [%s] %s: %s\n",
					confidenceToSeverityString(f.ClassificationName, f.Confidence),
					f.ClassificationName,
					f.PatternDescription)
				if !policyTestRedact {
					fmt.Printf("         Match: %s\n", f.MatchedContent)
				}
			}
		} else {
			fmt.Println("No policy violations found.")
		}
	}

	if hasFindings {
		os.Exit(CLIExitFindings)
	}
	os.Exit(CLIExitSuccess)
}

// confidenceToSeverityString maps classification + confidence to severity string.
func confidenceToSeverityString(classification string, confidence policy_engine.ConfidenceLevel) string {
	classLower := strings.ToLower(classification)

	if strings.Contains(classLower, "secret") ||
		strings.Contains(classLower, "credential") ||
		strings.Contains(classLower, "password") {
		if confidence == policy_engine.High {
			return "CRITICAL"
		}
		return "HIGH"
	}

	if strings.Contains(classLower, "pii") ||
		strings.Contains(classLower, "personal") {
		if confidence == policy_engine.High {
			return "HIGH"
		}
		return "MEDIUM"
	}

	switch confidence {
	case policy_engine.High:
		return "HIGH"
	case policy_engine.Medium:
		return "MEDIUM"
	default:
		return "LOW"
	}
}
