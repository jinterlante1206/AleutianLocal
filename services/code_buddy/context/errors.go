// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package context

import "errors"

// Sentinel errors for context assembly.
var (
	// ErrGraphNotInitialized indicates the graph is nil or not frozen.
	ErrGraphNotInitialized = errors.New("graph not initialized or not frozen")

	// ErrEmptyQuery indicates an empty or whitespace-only query.
	ErrEmptyQuery = errors.New("query must not be empty")

	// ErrQueryTooLong indicates the query exceeds the maximum length.
	ErrQueryTooLong = errors.New("query exceeds maximum length")

	// ErrInvalidBudget indicates a non-positive token budget.
	ErrInvalidBudget = errors.New("token budget must be positive")

	// ErrAssemblyTimeout indicates the assembly operation timed out.
	ErrAssemblyTimeout = errors.New("context assembly timed out")
)

// LLM-related errors for summary generation.
var (
	// ErrLLMRateLimited indicates the LLM rate limit was exceeded.
	// This error is retryable with exponential backoff.
	ErrLLMRateLimited = errors.New("LLM rate limit exceeded")

	// ErrLLMTimeout indicates the LLM request timed out.
	// This error is retryable.
	ErrLLMTimeout = errors.New("LLM request timed out")

	// ErrLLMServerError indicates a server error (5xx) from the LLM.
	// This error is retryable.
	ErrLLMServerError = errors.New("LLM server error")

	// ErrLLMInvalidRequest indicates an invalid request to the LLM.
	// This error is NOT retryable.
	ErrLLMInvalidRequest = errors.New("invalid LLM request")

	// ErrLLMEmptyPrompt indicates an empty prompt was provided.
	ErrLLMEmptyPrompt = errors.New("prompt must not be empty")

	// ErrLLMEmptyResponse indicates the LLM returned an empty response.
	ErrLLMEmptyResponse = errors.New("LLM returned empty response")

	// ErrLLMUnavailable indicates the LLM service is unavailable.
	// The circuit breaker may be open.
	ErrLLMUnavailable = errors.New("LLM service unavailable")
)

// Circuit breaker errors.
var (
	// ErrCircuitOpen indicates the circuit breaker is open.
	// Requests are being rejected to prevent cascade failures.
	ErrCircuitOpen = errors.New("circuit breaker is open")
)

// Summary generation errors.
var (
	// ErrSummaryTooShort indicates the generated summary is too short.
	ErrSummaryTooShort = errors.New("summary too short")

	// ErrInvalidKeyword indicates a keyword was not found in source.
	ErrInvalidKeyword = errors.New("keyword not found in source")

	// ErrLevelMismatch indicates the summary level doesn't match entity type.
	ErrLevelMismatch = errors.New("summary level mismatch")

	// ErrInvalidChild indicates a child reference is invalid.
	ErrInvalidChild = errors.New("invalid child reference")

	// ErrOrphanedSummary indicates a summary has no valid parent.
	ErrOrphanedSummary = errors.New("summary has no valid parent")

	// ErrStaleSummary indicates the summary hash doesn't match source.
	ErrStaleSummary = errors.New("summary is stale")
)

// Cache errors.
var (
	// ErrCacheNotFound indicates the requested item was not in cache.
	ErrCacheNotFound = errors.New("item not found in cache")

	// ErrCacheVersionConflict indicates an optimistic locking conflict.
	ErrCacheVersionConflict = errors.New("cache version conflict")

	// ErrBatchValidationFailed indicates batch validation failed.
	ErrBatchValidationFailed = errors.New("batch validation failed")
)

// Cost-related errors.
var (
	// ErrCostLimitExceeded indicates the operation would exceed cost limits.
	ErrCostLimitExceeded = errors.New("cost limit exceeded")

	// ErrTokenLimitExceeded indicates the operation would exceed token limits.
	ErrTokenLimitExceeded = errors.New("token limit exceeded")

	// ErrConfirmationRequired indicates user confirmation is needed.
	ErrConfirmationRequired = errors.New("confirmation required for expensive operation")
)

// Hierarchy errors.
var (
	// ErrUnsupportedLanguage indicates the language is not supported.
	ErrUnsupportedLanguage = errors.New("unsupported language")

	// ErrInvalidEntityID indicates the entity ID is malformed.
	ErrInvalidEntityID = errors.New("invalid entity ID")

	// ErrNoParent indicates the entity has no parent (is root).
	ErrNoParent = errors.New("entity has no parent")
)

// IsRetryable returns true if the error is retryable.
func IsRetryable(err error) bool {
	return errors.Is(err, ErrLLMRateLimited) ||
		errors.Is(err, ErrLLMTimeout) ||
		errors.Is(err, ErrLLMServerError)
}
