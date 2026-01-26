// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package extensions

import "context"

// =============================================================================
// Data Classification Types
// =============================================================================

// DataClassification represents the sensitivity level of data.
//
// Classifications follow common enterprise data handling policies and align
// with regulatory requirements (GDPR, HIPAA, CCPA). Higher levels require
// stricter handling controls.
//
// Example:
//
//	switch classification {
//	case ClassificationSecret:
//	    // Encrypt, audit access, restrict to need-to-know
//	case ClassificationPII:
//	    // Redact in logs, apply retention policies
//	case ClassificationConfidential:
//	    // Internal use only, no external sharing
//	case ClassificationPublic:
//	    // Safe to share externally
//	}
type DataClassification string

const (
	// ClassificationPublic indicates data that can be freely shared.
	// Examples: marketing materials, public documentation, open source code.
	ClassificationPublic DataClassification = "PUBLIC"

	// ClassificationConfidential indicates internal-only data.
	// Examples: internal memos, non-public financial data, strategy documents.
	ClassificationConfidential DataClassification = "CONFIDENTIAL"

	// ClassificationPII indicates personally identifiable information.
	// Examples: names, email addresses, phone numbers, IP addresses.
	// Requires special handling under GDPR, CCPA, and similar regulations.
	ClassificationPII DataClassification = "PII"

	// ClassificationSecret indicates highly sensitive data.
	// Examples: API keys, passwords, encryption keys, trade secrets.
	// Requires encryption at rest and in transit, strict access controls.
	ClassificationSecret DataClassification = "SECRET"
)

// ClassificationResult contains the outcome of data classification.
//
// A single piece of data may contain multiple classifications (e.g., a document
// with both PII and confidential business information). The HighestLevel field
// provides a single value for quick policy decisions.
//
// Example:
//
//	result := classifier.Classify(ctx, content)
//	if result.HighestLevel == ClassificationSecret {
//	    log.Warn("secret data detected", "locations", result.Findings)
//	    return errors.New("cannot process secret data")
//	}
type ClassificationResult struct {
	// HighestLevel is the most sensitive classification found.
	// Use this for quick policy decisions (e.g., block if SECRET).
	HighestLevel DataClassification

	// Findings lists all detected sensitive data with details.
	// May be empty if nothing sensitive was found (HighestLevel == PUBLIC).
	Findings []ClassificationFinding

	// IsClean is true if no sensitive data was detected.
	// Equivalent to HighestLevel == ClassificationPublic && len(Findings) == 0.
	IsClean bool
}

// ClassificationFinding describes a single piece of classified data.
//
// Example:
//
//	finding := ClassificationFinding{
//	    Classification: ClassificationPII,
//	    Type:           "email",
//	    Location:       "line 5, characters 10-30",
//	    Pattern:        "email_regex",
//	    Snippet:        "user@exa...",  // Truncated for logging
//	}
type ClassificationFinding struct {
	// Classification is the sensitivity level of this finding.
	Classification DataClassification

	// Type describes what kind of data was found.
	// Examples: "ssn", "credit_card", "email", "api_key", "password"
	Type string

	// Location describes where in the content the data was found.
	// Format is implementation-specific (e.g., "line 5", "offset 100-120").
	Location string

	// Pattern identifies which detection rule matched.
	// Useful for debugging and tuning classification rules.
	// Examples: "ssn_regex", "credit_card_luhn", "api_key_entropy"
	Pattern string

	// Snippet is a truncated/redacted portion of the matched content.
	// Used for audit logs without exposing full sensitive data.
	// Should be safe to log (first/last few characters only).
	Snippet string
}

// =============================================================================
// DataClassifier Interface
// =============================================================================

// DataClassifier scans data to determine its sensitivity classification.
//
// Implementations must be safe for concurrent use by multiple goroutines.
//
// # Open Source Behavior
//
// The default NopDataClassifier always returns PUBLIC classification,
// indicating no sensitive data was detected. This allows the local CLI
// to function without classification infrastructure.
//
// # Enterprise Implementation
//
// Enterprise versions implement pattern-based detection using:
//   - Regular expressions for known formats (SSN, credit cards, etc.)
//   - Entropy analysis for secrets (API keys, passwords)
//   - Machine learning for context-aware PII detection
//   - Custom patterns for organization-specific data
//
// Example enterprise implementation:
//
//	type RegexClassifier struct {
//	    patterns map[DataClassification][]*regexp.Regexp
//	}
//
//	func (c *RegexClassifier) Classify(ctx context.Context, content string) (*ClassificationResult, error) {
//	    var findings []ClassificationFinding
//	    highest := ClassificationPublic
//	    for level, patterns := range c.patterns {
//	        for _, p := range patterns {
//	            if matches := p.FindAllStringIndex(content, -1); matches != nil {
//	                // Record findings, update highest level
//	            }
//	        }
//	    }
//	    return &ClassificationResult{
//	        HighestLevel: highest,
//	        Findings:     findings,
//	        IsClean:      len(findings) == 0,
//	    }, nil
//	}
//
// # Usage
//
// Classify data before storage, logging, or external transmission:
//
//	result, err := classifier.Classify(ctx, userMessage)
//	if err != nil {
//	    return fmt.Errorf("classification failed: %w", err)
//	}
//	if result.HighestLevel == ClassificationSecret {
//	    return errors.New("cannot process messages containing secrets")
//	}
//	// Log findings for compliance
//	for _, f := range result.Findings {
//	    auditLogger.Log(ctx, AuditEvent{
//	        EventType: "data.classified",
//	        Metadata: map[string]any{
//	            "classification": f.Classification,
//	            "type":           f.Type,
//	        },
//	    })
//	}
//
// # Limitations
//
//   - Pattern-based detection has false positives/negatives
//   - Context matters: "123-45-6789" could be SSN or order number
//   - New data formats require pattern updates
//
// # Assumptions
//
//   - Content is UTF-8 encoded text
//   - Classifications are hierarchical (SECRET > PII > CONFIDENTIAL > PUBLIC)
type DataClassifier interface {
	// Classify analyzes content and returns its sensitivity classification.
	//
	// # Description
	//
	// Scans the provided content for patterns indicating sensitive data.
	// Returns the highest classification found along with detailed findings.
	//
	// # Inputs
	//
	//   - ctx: Context for cancellation and timeout control.
	//   - content: The text to classify. May be any length.
	//
	// # Outputs
	//
	//   - *ClassificationResult: Classification details, never nil on success.
	//   - error: Non-nil if classification failed (e.g., timeout, invalid input).
	//
	// # Examples
	//
	//   // Check a user message
	//   result, err := classifier.Classify(ctx, message)
	//   if result.HighestLevel >= ClassificationPII {
	//       // Apply PII handling procedures
	//   }
	//
	// # Limitations
	//
	//   - Large content may be slow to process
	//   - Binary content is not supported
	//
	// # Assumptions
	//
	//   - Content is valid UTF-8 text
	//   - Context deadline is respected for long operations
	//
	// # Thread Safety
	//
	// Safe to call concurrently from multiple goroutines.
	Classify(ctx context.Context, content string) (*ClassificationResult, error)

	// ClassifyBatch analyzes multiple content items efficiently.
	//
	// # Description
	//
	// Classifies multiple content items in a single call. Implementations
	// may process items in parallel for better performance.
	//
	// # Inputs
	//
	//   - ctx: Context for cancellation and timeout control.
	//   - contents: Slice of content items to classify.
	//
	// # Outputs
	//
	//   - []*ClassificationResult: Results in same order as input.
	//   - error: Non-nil if any classification failed.
	//
	// # Examples
	//
	//   contents := []string{message1, message2, message3}
	//   results, err := classifier.ClassifyBatch(ctx, contents)
	//   for i, result := range results {
	//       if !result.IsClean {
	//           log.Warn("sensitive data in item", "index", i)
	//       }
	//   }
	//
	// # Limitations
	//
	//   - If one item fails, the entire batch may fail
	//   - Memory usage scales with batch size
	//
	// # Assumptions
	//
	//   - All content items are valid UTF-8 text
	//   - Batch size is reasonable (implementation may limit)
	//
	// # Thread Safety
	//
	// Safe to call concurrently from multiple goroutines.
	ClassifyBatch(ctx context.Context, contents []string) ([]*ClassificationResult, error)
}

// =============================================================================
// No-Op Implementation
// =============================================================================

// NopDataClassifier is the default classifier for open source.
//
// It always returns PUBLIC classification with no findings, indicating
// no sensitive data was detected. This allows the CLI to function
// without classification infrastructure.
//
// Thread-safe: This implementation has no mutable state.
//
// Example:
//
//	classifier := &NopDataClassifier{}
//	result, err := classifier.Classify(ctx, "user SSN: 123-45-6789")
//	// result.HighestLevel == ClassificationPublic
//	// result.IsClean == true
//	// result.Findings == nil
//	// err == nil
type NopDataClassifier struct{}

// Classify always returns PUBLIC classification with no findings.
//
// # Description
//
// Returns a clean classification result regardless of content.
// This is intentional for local single-user deployments where
// data classification isn't required.
//
// # Inputs
//
//   - ctx: Ignored (no external calls).
//   - content: Ignored (not analyzed).
//
// # Outputs
//
//   - *ClassificationResult: Always PUBLIC, IsClean=true, no findings.
//   - error: Always nil.
//
// # Examples
//
//	result, _ := classifier.Classify(ctx, "any content")
//	// result.IsClean == true
//
// # Limitations
//
//   - Does not detect any sensitive data.
//
// # Assumptions
//
//   - Caller accepts that no classification is performed.
//
// # Thread Safety
//
// Safe to call concurrently (stateless).
func (c *NopDataClassifier) Classify(_ context.Context, _ string) (*ClassificationResult, error) {
	return &ClassificationResult{
		HighestLevel: ClassificationPublic,
		Findings:     nil,
		IsClean:      true,
	}, nil
}

// ClassifyBatch always returns PUBLIC classification for all items.
//
// # Description
//
// Returns clean classification results for all content items.
// This is intentional for local deployments without classification.
//
// # Inputs
//
//   - ctx: Ignored (no external calls).
//   - contents: Used only to determine result slice length.
//
// # Outputs
//
//   - []*ClassificationResult: All PUBLIC, IsClean=true, same length as input.
//   - error: Always nil.
//
// # Examples
//
//	results, _ := classifier.ClassifyBatch(ctx, []string{"a", "b", "c"})
//	// len(results) == 3
//	// all results.IsClean == true
//
// # Limitations
//
//   - Does not detect any sensitive data.
//
// # Assumptions
//
//   - Caller accepts that no classification is performed.
//
// # Thread Safety
//
// Safe to call concurrently (stateless).
func (c *NopDataClassifier) ClassifyBatch(_ context.Context, contents []string) ([]*ClassificationResult, error) {
	results := make([]*ClassificationResult, len(contents))
	for i := range contents {
		results[i] = &ClassificationResult{
			HighestLevel: ClassificationPublic,
			Findings:     nil,
			IsClean:      true,
		}
	}
	return results, nil
}

// =============================================================================
// Interface Compliance
// =============================================================================

// Compile-time interface compliance check.
var _ DataClassifier = (*NopDataClassifier)(nil)
