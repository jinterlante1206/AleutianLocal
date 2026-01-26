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

import (
	"context"
	"errors"
)

// ErrMessageBlocked is returned when a message is rejected by the filter.
// Enterprise implementations should wrap this error with the reason.
//
// Example:
//
//	if containsPII(msg) {
//	    return "", fmt.Errorf("message contains PII: %w", ErrMessageBlocked)
//	}
var ErrMessageBlocked = errors.New("message blocked by filter")

// FilterResult contains the outcome of a filter operation.
//
// This struct provides detailed information about what the filter did,
// useful for debugging, audit trails, and user feedback.
//
// Example:
//
//	result := FilterResult{
//	    Original:  "My SSN is 123-45-6789",
//	    Filtered:  "My SSN is [REDACTED]",
//	    WasModified: true,
//	    Detections: []Detection{
//	        {Type: "SSN", Location: "position 10-21", Action: "redacted"},
//	    },
//	}
type FilterResult struct {
	// Original is the input message before filtering.
	Original string

	// Filtered is the message after filtering transformations.
	// If WasModified is false, this equals Original.
	Filtered string

	// WasModified indicates if any transformations were applied.
	WasModified bool

	// WasBlocked indicates if the message was completely rejected.
	// If true, Filtered should not be used.
	WasBlocked bool

	// BlockReason explains why the message was blocked (if WasBlocked).
	BlockReason string

	// Detections lists what the filter found in the message.
	// Useful for audit logging and debugging.
	Detections []Detection
}

// Detection describes a single item found by the filter.
//
// Example:
//
//	detection := Detection{
//	    Type:     "credit_card",
//	    Location: "characters 45-64",
//	    Action:   "redacted",
//	    Original: "4111-1111-1111-1111",  // Only in debug mode
//	}
type Detection struct {
	// Type categorizes what was detected.
	// Common types: "ssn", "credit_card", "email", "phone", "api_key",
	// "profanity", "pii", "secret", "prompt_injection"
	Type string

	// Location describes where in the message the item was found.
	// Format is implementation-specific (e.g., "characters 10-20", "line 3")
	Location string

	// Action describes what was done with the detected item.
	// Values: "redacted", "masked", "replaced", "blocked", "flagged"
	Action string

	// Original is the detected content (only populated in debug mode).
	// WARNING: This may contain sensitive data - handle carefully.
	Original string

	// Replacement is what the content was replaced with (if Action is "replaced").
	Replacement string
}

// MessageFilter transforms messages before and after LLM processing.
//
// Implementations must be safe for concurrent use by multiple goroutines.
//
// # Filter Pipeline
//
// Messages flow through filters at two points:
//
//  1. FilterInput: Before sending to LLM
//     - Remove PII from user messages
//     - Block policy violations
//     - Detect prompt injection attempts
//
//  2. FilterOutput: Before returning to user
//     - Remove leaked secrets from responses
//     - Add compliance disclaimers
//     - Mask sensitive generated content
//
// # Open Source Behavior
//
// The default NopMessageFilter passes all messages through unchanged.
// This is appropriate for local single-user deployments where content
// filtering isn't required.
//
// # Enterprise Implementation
//
// Enterprise versions implement content policies, PII detection,
// and compliance requirements.
//
// Example enterprise implementation:
//
//	type PIIFilter struct {
//	    patterns []PIIPattern
//	    policy   *Policy
//	}
//
//	func (f *PIIFilter) FilterInput(ctx context.Context, msg string) (*FilterResult, error) {
//	    result := &FilterResult{Original: msg, Filtered: msg}
//
//	    for _, pattern := range f.patterns {
//	        if matches := pattern.FindAll(msg); len(matches) > 0 {
//	            result.Filtered = pattern.Redact(result.Filtered)
//	            result.WasModified = true
//	            result.Detections = append(result.Detections, Detection{
//	                Type:   pattern.Name,
//	                Action: "redacted",
//	            })
//	        }
//	    }
//
//	    return result, nil
//	}
//
// # Blocking vs Transforming
//
// Filters can either:
//   - Transform: Modify content and allow it through (e.g., redact SSN)
//   - Block: Reject the entire message (e.g., policy violation)
//
// To block, return a FilterResult with WasBlocked=true and BlockReason set.
// The caller should then return ErrMessageBlocked to the user.
type MessageFilter interface {
	// FilterInput processes a user message before LLM inference.
	//
	// Parameters:
	//   - ctx: Context for cancellation and timeout control
	//   - message: The raw user input
	//
	// Returns:
	//   - *FilterResult: The filtered message and metadata
	//   - error: Non-nil only for filter failures (not for blocks)
	//
	// If WasBlocked is true, the caller should:
	//  1. Log the block via AuditLogger
	//  2. Return ErrMessageBlocked to the user
	//  3. NOT send the message to the LLM
	FilterInput(ctx context.Context, message string) (*FilterResult, error)

	// FilterOutput processes an LLM response before returning to user.
	//
	// Parameters:
	//   - ctx: Context for cancellation and timeout control
	//   - message: The LLM response
	//
	// Returns:
	//   - *FilterResult: The filtered response and metadata
	//   - error: Non-nil only for filter failures (not for blocks)
	//
	// Common output filtering:
	//   - Remove accidentally leaked API keys
	//   - Add compliance disclaimers
	//   - Mask generated PII
	FilterOutput(ctx context.Context, message string) (*FilterResult, error)

	// FilterContext processes context/system prompts before use.
	//
	// Parameters:
	//   - ctx: Context for cancellation and timeout control
	//   - context: System prompt or context being injected
	//
	// Returns:
	//   - *FilterResult: The filtered context and metadata
	//   - error: Non-nil only for filter failures
	//
	// This is called when injecting retrieved documents (RAG),
	// system prompts, or other context into the conversation.
	FilterContext(ctx context.Context, contextMsg string) (*FilterResult, error)
}

// NopMessageFilter is the default message filter for open source.
//
// It passes all messages through unchanged without any transformation
// or blocking. This is appropriate for local single-user deployments
// where content filtering isn't required.
//
// Thread-safe: This implementation has no mutable state.
//
// Example:
//
//	filter := &NopMessageFilter{}
//	result, err := filter.FilterInput(ctx, "My SSN is 123-45-6789")
//	// result.Filtered == "My SSN is 123-45-6789" (unchanged)
//	// result.WasModified == false
//	// err == nil
type NopMessageFilter struct{}

// FilterInput returns the message unchanged.
//
// No transformations or blocking are applied.
func (f *NopMessageFilter) FilterInput(ctx context.Context, message string) (*FilterResult, error) {
	return &FilterResult{
		Original:    message,
		Filtered:    message,
		WasModified: false,
		WasBlocked:  false,
		Detections:  nil,
	}, nil
}

// FilterOutput returns the message unchanged.
//
// No transformations or blocking are applied.
func (f *NopMessageFilter) FilterOutput(ctx context.Context, message string) (*FilterResult, error) {
	return &FilterResult{
		Original:    message,
		Filtered:    message,
		WasModified: false,
		WasBlocked:  false,
		Detections:  nil,
	}, nil
}

// FilterContext returns the context unchanged.
//
// No transformations or blocking are applied.
func (f *NopMessageFilter) FilterContext(ctx context.Context, contextMsg string) (*FilterResult, error) {
	return &FilterResult{
		Original:    contextMsg,
		Filtered:    contextMsg,
		WasModified: false,
		WasBlocked:  false,
		Detections:  nil,
	}, nil
}

// Compile-time interface compliance check.
// This ensures NopMessageFilter implements MessageFilter.
var _ MessageFilter = (*NopMessageFilter)(nil)
