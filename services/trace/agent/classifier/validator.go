// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.

package classifier

import (
	"regexp"
	"strings"

	"github.com/AleutianAI/AleutianFOSS/services/trace/agent/llm"
)

// ValidationResult contains the outcome of response validation.
type ValidationResult struct {
	// Valid indicates if the response passed all checks.
	Valid bool

	// Reason explains why validation passed or failed.
	Reason string

	// Retryable indicates if a retry might succeed.
	Retryable bool

	// MatchedPattern contains the prohibited pattern that was matched (if any).
	MatchedPattern string
}

// ResponseValidator checks LLM responses for quality and compliance.
//
// The validator ensures responses:
// - Include tool calls when required
// - Don't contain prohibited patterns (e.g., "I'm ready to help")
// - Are not empty
//
// Thread Safety: This type is safe for concurrent use after initialization.
type ResponseValidator struct {
	// prohibitedPatterns are regex patterns that indicate unhelpful responses.
	prohibitedPatterns []*regexp.Regexp
}

// NewResponseValidator creates a validator with default prohibited patterns.
//
// Description:
//
//	Creates a validator configured to catch common unhelpful response
//	patterns like "I'm ready to help" or "What would you like me to do".
//
// Outputs:
//
//	*ResponseValidator - Ready-to-use validator.
//
// Example:
//
//	validator := NewResponseValidator()
//	result := validator.Validate(response, toolChoice)
//	if !result.Valid && result.Retryable {
//	    // Retry with stronger tool forcing
//	}
func NewResponseValidator() *ResponseValidator {
	return &ResponseValidator{
		prohibitedPatterns: []*regexp.Regexp{
			// "Ready to help" variants
			regexp.MustCompile(`(?i)I'?m ready to help`),
			regexp.MustCompile(`(?i)I am ready to help`),
			regexp.MustCompile(`(?i)I'?d be happy to help`),
			regexp.MustCompile(`(?i)I would be happy to help`),

			// "What would you like" variants
			regexp.MustCompile(`(?i)What would you like`),
			regexp.MustCompile(`(?i)What do you want me to`),
			regexp.MustCompile(`(?i)What should I`),

			// "How can I assist" variants
			regexp.MustCompile(`(?i)How can I (assist|help)`),
			regexp.MustCompile(`(?i)How may I (assist|help)`),

			// "Let me know" variants
			regexp.MustCompile(`(?i)Let me know (if|what|how|when)`),
			regexp.MustCompile(`(?i)Please let me know`),

			// "I can help" without action
			regexp.MustCompile(`(?i)I can help you with`),
			regexp.MustCompile(`(?i)I'?m here to help`),

			// Offering menus/options instead of acting
			regexp.MustCompile(`(?i)Would you like me to`),
			regexp.MustCompile(`(?i)Do you want me to`),
			regexp.MustCompile(`(?i)Shall I`),

			// Asking for clarification instead of investigating (Lazy Agent)
			regexp.MustCompile(`(?i)Could you (please )?(specify|clarify|tell me)`),
			regexp.MustCompile(`(?i)Please (let me know|specify|clarify)`),
			regexp.MustCompile(`(?i)Are you (looking for|interested in)`),
			regexp.MustCompile(`(?i)Do you (need|want) (help|me to)`),
			regexp.MustCompile(`(?i)What (specific )?(analysis|aspect|part)`),

			// Generic greetings that should be tool calls
			regexp.MustCompile(`(?i)^Hello!?\s+I'?m`),
			regexp.MustCompile(`(?i)^Hi!?\s+I'?m`),
		},
	}
}

// Validate checks if an LLM response meets quality requirements.
//
// Description:
//
//	Validates the response based on the tool_choice that was used:
//	- If tool_choice was "any" or "tool", requires tool calls
//	- Checks for prohibited patterns in the response text
//	- Checks for empty responses
//
// Inputs:
//
//	response - The LLM response to validate. May be nil.
//	toolChoice - The tool_choice that was used for the request.
//
// Outputs:
//
//	ValidationResult - Contains validation status and reasoning.
//
// Example:
//
//	result := validator.Validate(response, llm.ToolChoiceAny())
//	if !result.Valid {
//	    log.Printf("Validation failed: %s", result.Reason)
//	}
//
// Thread Safety: This method is safe for concurrent use.
func (v *ResponseValidator) Validate(response *llm.Response, toolChoice *llm.ToolChoice) ValidationResult {
	// Nil response check
	if response == nil {
		return ValidationResult{
			Valid:     false,
			Reason:    "nil response",
			Retryable: true,
		}
	}

	// Check for completely empty response
	if response.Content == "" && len(response.ToolCalls) == 0 {
		return ValidationResult{
			Valid:     false,
			Reason:    "empty response with no tool calls",
			Retryable: true,
		}
	}

	// If tool_choice required tools, check they were called
	if toolChoice != nil {
		switch toolChoice.Type {
		case "any":
			if len(response.ToolCalls) == 0 {
				return ValidationResult{
					Valid:     false,
					Reason:    "tool_choice 'any' required tools but none were called",
					Retryable: true,
				}
			}
		case "tool":
			if len(response.ToolCalls) == 0 {
				return ValidationResult{
					Valid:     false,
					Reason:    "tool_choice 'tool' required " + toolChoice.Name + " but no tools were called",
					Retryable: true,
				}
			}
			// Check if the correct tool was called
			correctToolCalled := false
			for _, call := range response.ToolCalls {
				if call.Name == toolChoice.Name {
					correctToolCalled = true
					break
				}
			}
			if !correctToolCalled {
				return ValidationResult{
					Valid:     false,
					Reason:    "tool_choice required " + toolChoice.Name + " but it was not called",
					Retryable: true,
				}
			}
		}
	}

	// Check for prohibited patterns in content
	for _, pattern := range v.prohibitedPatterns {
		if pattern.MatchString(response.Content) {
			return ValidationResult{
				Valid:          false,
				Reason:         "prohibited pattern detected",
				Retryable:      true,
				MatchedPattern: pattern.String(),
			}
		}
	}

	// All checks passed
	return ValidationResult{
		Valid:  true,
		Reason: "passed all validation checks",
	}
}

// ValidateWithToolRequirement checks if response has tool calls when required.
//
// Description:
//
//	Simplified validation that only checks for tool call presence.
//	Use this when you only care about whether tools were called.
//
// Inputs:
//
//	response - The LLM response to check.
//	requireTools - If true, response must have tool calls.
//
// Outputs:
//
//	bool - True if validation passes.
//	string - Reason for failure (empty if valid).
//
// Thread Safety: This method is safe for concurrent use.
func (v *ResponseValidator) ValidateWithToolRequirement(response *llm.Response, requireTools bool) (bool, string) {
	if response == nil {
		return false, "nil response"
	}

	if requireTools && len(response.ToolCalls) == 0 {
		return false, "tools required but none called"
	}

	return true, ""
}

// HasProhibitedPattern checks if text contains any prohibited patterns.
//
// Description:
//
//	Standalone check for prohibited patterns without full validation.
//	Useful for checking intermediate text before response is complete.
//
// Inputs:
//
//	text - The text to check.
//
// Outputs:
//
//	bool - True if a prohibited pattern was found.
//	string - The matched pattern (empty if none).
//
// Thread Safety: This method is safe for concurrent use.
func (v *ResponseValidator) HasProhibitedPattern(text string) (bool, string) {
	for _, pattern := range v.prohibitedPatterns {
		if pattern.MatchString(text) {
			return true, pattern.String()
		}
	}
	return false, ""
}

// AddProhibitedPattern adds a custom prohibited pattern.
//
// Description:
//
//	Extends the validator with additional patterns to check.
//	The pattern is compiled as a case-insensitive regex.
//
// Inputs:
//
//	pattern - Regex pattern string to add.
//
// Outputs:
//
//	error - Non-nil if pattern fails to compile.
//
// Thread Safety: This method is NOT safe for concurrent use.
// Call only during initialization.
func (v *ResponseValidator) AddProhibitedPattern(pattern string) error {
	compiled, err := regexp.Compile("(?i)" + pattern)
	if err != nil {
		return err
	}
	v.prohibitedPatterns = append(v.prohibitedPatterns, compiled)
	return nil
}

// SuggestRetryToolChoice returns a stronger tool_choice for retry.
//
// Description:
//
//	Given a failed validation result and the original tool_choice,
//	suggests a stronger tool_choice to use for retry.
//
// Inputs:
//
//	original - The tool_choice that was used originally.
//	suggestedTool - A tool to force if available.
//
// Outputs:
//
//	*llm.ToolChoice - Stronger tool_choice for retry.
//
// Example:
//
//	if !result.Valid && result.Retryable {
//	    retry := SuggestRetryToolChoice(original, "find_entry_points")
//	    // retry.Type == "any" or "tool"
//	}
func SuggestRetryToolChoice(original *llm.ToolChoice, suggestedTool string) *llm.ToolChoice {
	if original == nil {
		// No original → require any tool
		return llm.ToolChoiceAny()
	}

	switch original.Type {
	case "auto":
		// Auto → require any tool
		return llm.ToolChoiceAny()
	case "any":
		// Any → force specific tool if available
		if suggestedTool != "" {
			return llm.ToolChoiceRequired(suggestedTool)
		}
		return llm.ToolChoiceAny()
	case "tool":
		// Already forcing specific tool, keep it
		return original
	default:
		return llm.ToolChoiceAny()
	}
}

// NeedsToolCalls returns true if the tool_choice requires tool calls.
func NeedsToolCalls(toolChoice *llm.ToolChoice) bool {
	if toolChoice == nil {
		return false
	}
	return toolChoice.Type == "any" || toolChoice.Type == "tool"
}

// DescribeToolChoice returns a human-readable description of the tool_choice.
func DescribeToolChoice(toolChoice *llm.ToolChoice) string {
	if toolChoice == nil {
		return "auto (model decides)"
	}

	switch toolChoice.Type {
	case "auto":
		return "auto (model decides)"
	case "any":
		return "any (must call at least one tool)"
	case "tool":
		return "tool:" + toolChoice.Name + " (must call specific tool)"
	case "none":
		return "none (no tools allowed)"
	default:
		return "unknown: " + toolChoice.Type
	}
}

// WrapWithRetry creates a response validator that can retry on failure.
type RetryableValidator struct {
	validator    *ResponseValidator
	maxRetries   int
	retryHandler func(attempt int, original *llm.ToolChoice, suggestedTool string) *llm.ToolChoice
}

// NewRetryableValidator creates a validator with retry capabilities.
func NewRetryableValidator(maxRetries int) *RetryableValidator {
	return &RetryableValidator{
		validator:  NewResponseValidator(),
		maxRetries: maxRetries,
		retryHandler: func(attempt int, original *llm.ToolChoice, suggestedTool string) *llm.ToolChoice {
			// Default: escalate to stronger forcing on each attempt
			if attempt == 1 {
				return llm.ToolChoiceAny()
			}
			if suggestedTool != "" {
				return llm.ToolChoiceRequired(suggestedTool)
			}
			return llm.ToolChoiceAny()
		},
	}
}

// Validate validates the response.
func (r *RetryableValidator) Validate(response *llm.Response, toolChoice *llm.ToolChoice) ValidationResult {
	return r.validator.Validate(response, toolChoice)
}

// ShouldRetry determines if a failed validation should be retried.
func (r *RetryableValidator) ShouldRetry(result ValidationResult, attempt int) bool {
	return !result.Valid && result.Retryable && attempt < r.maxRetries
}

// GetRetryToolChoice returns the tool_choice to use for retry.
func (r *RetryableValidator) GetRetryToolChoice(attempt int, original *llm.ToolChoice, suggestedTool string) *llm.ToolChoice {
	return r.retryHandler(attempt, original, suggestedTool)
}

// MaxRetries returns the maximum number of retries configured.
func (r *RetryableValidator) MaxRetries() int {
	return r.maxRetries
}

// LooksLikeOfferToHelp checks if response content appears to offer help
// rather than actually helping. This is a quick heuristic check.
func LooksLikeOfferToHelp(content string) bool {
	if content == "" {
		return false
	}

	lower := strings.ToLower(content)

	// Check for common "offer to help" phrases
	offerPhrases := []string{
		"ready to help",
		"happy to help",
		"here to help",
		"can help you",
		"would you like me to",
		"do you want me to",
		"shall i",
		"let me know",
		"what would you like",
		// Lazy Agent patterns
		"could you please specify",
		"could you specify",
		"please specify",
		"are you looking for",
		"are you interested in",
		"do you need help",
		"what specific analysis",
		"what aspect",
	}

	for _, phrase := range offerPhrases {
		if strings.Contains(lower, phrase) {
			return true
		}
	}

	return false
}

// QualityConfig configures the quality validator's strictness.
type QualityConfig struct {
	// MinLengthForCitationRequirement is the minimum response length (chars)
	// before citations become required. Default: 200.
	MinLengthForCitationRequirement int

	// EnableHedgingDetection enables detection of hedging language.
	// Default: true.
	EnableHedgingDetection bool

	// EnableCitationRequirement enables citation requirement checking.
	// Default: true.
	EnableCitationRequirement bool

	// StrictnessLevel controls validation strictness.
	// 0 = disabled, 1 = warnings only (logged), 2 = soft fail (retryable), 3 = hard fail
	// Default: 2 (soft fail).
	StrictnessLevel int
}

// DefaultQualityConfig returns sensible defaults.
func DefaultQualityConfig() QualityConfig {
	return QualityConfig{
		MinLengthForCitationRequirement: 200,
		EnableHedgingDetection:          true,
		EnableCitationRequirement:       true,
		StrictnessLevel:                 2, // Soft fail (retryable)
	}
}

// hedgingPatterns detects uncertain language that should be citations.
// These patterns match hedging language followed by action verbs.
var hedgingPatterns = []*regexp.Regexp{
	// "likely/probably/might + verb" patterns
	regexp.MustCompile(`(?i)\b(likely|probably|might|may|could)\b.{0,20}\b(use[sd]?|call[sd]?|load[sd]?|have|has|contain[sd]?|handle[sd]?|process(?:es|ed)?)`),
	// "appears to/seems to" patterns
	regexp.MustCompile(`(?i)\b(appears?|seems?)\s+to\s+\b`),
	// "based on function/method names" - guessing from naming
	regexp.MustCompile(`(?i)based on the (function |method )?names?`),
	// "it is/it's probably/likely"
	regexp.MustCompile(`(?i)\bit('s| is)\s+(probably|likely)\b`),
	// "I think/I believe" for code facts
	regexp.MustCompile(`(?i)\bI (think|believe|assume)\b.{0,30}\b(this|the|it)\b`),
}

// citationPatterns match valid code citations in various formats.
// Supports: [file.go:42], (file.go:42), file.go:42, [file.go:42-50]
var citationPatterns = []*regexp.Regexp{
	// [file.ext:line] or [file.ext:line-line]
	regexp.MustCompile(`\[[^\]]+\.(go|py|js|ts|jsx|tsx|java|rs|c|cpp|h|hpp|rb|php|swift|kt):\d+(-\d+)?\]`),
	// (file.ext:line)
	regexp.MustCompile(`\([^)]+\.(go|py|js|ts|jsx|tsx|java|rs|c|cpp|h|hpp|rb|php|swift|kt):\d+(-\d+)?\)`),
	// file.ext:line (bare, must be preceded by space or start)
	regexp.MustCompile(`(?:^|\s)[a-zA-Z0-9_/.-]+\.(go|py|js|ts|jsx|tsx|java|rs|c|cpp|h|hpp|rb|php|swift|kt):\d+`),
}

// QualityValidator validates response quality for analytical queries.
//
// It checks for:
// - Hedging language that should be citations
// - Missing citations in long analytical responses
// - Proper evidence-based claims
//
// Thread Safety: This type is safe for concurrent use after initialization.
type QualityValidator struct {
	config QualityConfig
}

// NewQualityValidator creates a quality validator with the given config.
//
// Inputs:
//
//	config - Configuration for strictness. Use nil for defaults.
//
// Outputs:
//
//	*QualityValidator - Ready-to-use validator.
func NewQualityValidator(config *QualityConfig) *QualityValidator {
	cfg := DefaultQualityConfig()
	if config != nil {
		cfg = *config
	}
	return &QualityValidator{config: cfg}
}

// ValidateQuality checks response content for quality issues.
//
// Description:
//
//	Validates the response against quality rules:
//	- Detects hedging language that should be evidence
//	- Checks for citation presence in long responses
//	- Returns validation result based on strictness level
//
// Inputs:
//
//	content - The response content to validate.
//	isAnalytical - Whether this is an analytical query requiring evidence.
//
// Outputs:
//
//	ValidationResult - Contains validation status and reasoning.
//
// Thread Safety: This method is safe for concurrent use.
func (v *QualityValidator) ValidateQuality(content string, isAnalytical bool) ValidationResult {
	// Skip validation for non-analytical queries
	if !isAnalytical {
		return ValidationResult{Valid: true, Reason: "non-analytical query"}
	}

	// Skip validation if disabled
	if v.config.StrictnessLevel == 0 {
		return ValidationResult{Valid: true, Reason: "quality validation disabled"}
	}

	// Check for hedging language
	if v.config.EnableHedgingDetection {
		for _, pattern := range hedgingPatterns {
			if pattern.MatchString(content) {
				return v.buildResult(false, "hedging language detected - should cite evidence", pattern.String())
			}
		}
	}

	// Check for citation requirement in long responses
	if v.config.EnableCitationRequirement {
		if len(content) >= v.config.MinLengthForCitationRequirement {
			if !v.hasCitation(content) {
				return v.buildResult(false, "analytical response lacks [file:line] citations", "")
			}
		}
	}

	return ValidationResult{Valid: true, Reason: "quality checks passed"}
}

// hasCitation checks if the content contains any valid citation format.
func (v *QualityValidator) hasCitation(content string) bool {
	for _, pattern := range citationPatterns {
		if pattern.MatchString(content) {
			return true
		}
	}

	// Also accept explicit "I don't see X" as valid evidence-based response
	lower := strings.ToLower(content)
	if strings.Contains(lower, "i don't see") || strings.Contains(lower, "i do not see") ||
		strings.Contains(lower, "not found in") || strings.Contains(lower, "couldn't find") {
		return true
	}

	return false
}

// buildResult constructs a ValidationResult based on strictness level.
func (v *QualityValidator) buildResult(valid bool, reason, matchedPattern string) ValidationResult {
	result := ValidationResult{
		Valid:          valid,
		Reason:         reason,
		MatchedPattern: matchedPattern,
	}

	// Determine retryability based on strictness
	if !valid {
		switch v.config.StrictnessLevel {
		case 1: // Warnings only - treat as valid but log
			result.Valid = true
			result.Reason = "[warning] " + reason
		case 2: // Soft fail - retryable
			result.Retryable = true
		case 3: // Hard fail - not retryable
			result.Retryable = false
		}
	}

	return result
}

// HasHedgingLanguage checks if content contains hedging language.
// This is a convenience method for external callers.
func HasHedgingLanguage(content string) (bool, string) {
	for _, pattern := range hedgingPatterns {
		if pattern.MatchString(content) {
			return true, pattern.String()
		}
	}
	return false, ""
}

// HasCitation checks if content contains a valid citation.
// This is a convenience method for external callers.
func HasCitation(content string) bool {
	for _, pattern := range citationPatterns {
		if pattern.MatchString(content) {
			return true
		}
	}
	return false
}
