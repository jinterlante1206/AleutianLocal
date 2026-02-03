// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package grounding

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/trace/agent"
)

// StrictnessLevel indicates the level of verification strictness.
type StrictnessLevel int

const (
	// StrictnessNormal is the default strictness level.
	StrictnessNormal StrictnessLevel = iota

	// StrictnessElevated adds violation feedback to the prompt.
	StrictnessElevated

	// StrictnessHigh requires explicit grounding and adds warnings.
	StrictnessHigh

	// StrictnessFeedback indicates retries exhausted, need exploration.
	StrictnessFeedback
)

// String returns the string representation of StrictnessLevel.
func (s StrictnessLevel) String() string {
	switch s {
	case StrictnessNormal:
		return "normal"
	case StrictnessElevated:
		return "elevated"
	case StrictnessHigh:
		return "high"
	case StrictnessFeedback:
		return "feedback"
	default:
		return "unknown"
	}
}

// PostSynthesisConfig configures post-synthesis verification.
type PostSynthesisConfig struct {
	// Enabled determines if post-synthesis verification runs.
	Enabled bool

	// MaxRetries is the maximum number of synthesis retries before feedback loop.
	MaxRetries int

	// RelevantCheckers specifies which checkers to run post-synthesis.
	// Empty means use default relevant checkers.
	RelevantCheckers []string
}

// DefaultPostSynthesisConfig returns sensible defaults for post-synthesis verification.
//
// Outputs:
//
//	*PostSynthesisConfig - The default configuration.
func DefaultPostSynthesisConfig() *PostSynthesisConfig {
	return &PostSynthesisConfig{
		Enabled:    true,
		MaxRetries: 3,
		RelevantCheckers: []string{
			"structural_claim_checker",
			"phantom_file_checker",
			"language_checker",
		},
	}
}

// PostSynthesisResult contains the outcome of post-synthesis verification.
type PostSynthesisResult struct {
	// Passed is true if verification passed without critical violations.
	Passed bool

	// Violations found during post-synthesis checking.
	Violations []Violation

	// RetryCount is how many retries were attempted.
	RetryCount int

	// StrictnessLevel indicates the strictness at which verification ran.
	StrictnessLevel StrictnessLevel

	// NeedsFeedbackLoop is true if retries exhausted and should explore more.
	NeedsFeedbackLoop bool

	// FeedbackQuestions are questions to explore if feedback loop triggered.
	FeedbackQuestions []string

	// CheckDuration is how long verification took.
	CheckDuration time.Duration

	// CheckersRun is the number of checkers that ran.
	CheckersRun int
}

// PostSynthesisVerifier verifies synthesized responses.
//
// This interface allows phases to use post-synthesis verification
// without depending on the concrete DefaultGrounder type.
//
// Thread Safety: Implementations must be safe for concurrent use.
type PostSynthesisVerifier interface {
	// VerifyPostSynthesis checks a synthesized response for hallucinations.
	//
	// Inputs:
	//   ctx - Context for cancellation.
	//   response - The synthesized response text.
	//   assembledCtx - The context that was given to the LLM.
	//   retryCount - Current retry attempt (0 = first attempt).
	//
	// Outputs:
	//   *PostSynthesisResult - The verification result.
	//   error - Non-nil only if verification itself fails.
	VerifyPostSynthesis(ctx context.Context, response string, assembledCtx *agent.AssembledContext, retryCount int) (*PostSynthesisResult, error)

	// GenerateStricterPrompt creates a prompt with increased strictness.
	//
	// Inputs:
	//   basePrompt - The original synthesis prompt.
	//   violations - The violations from the previous attempt.
	//   level - The strictness level to apply.
	//
	// Outputs:
	//   string - The enhanced prompt with strictness guidance.
	GenerateStricterPrompt(basePrompt string, violations []Violation, level StrictnessLevel) string
}

// VerifyPostSynthesis checks a synthesized response for hallucinations.
//
// Description:
//
//	Runs a subset of grounding checkers specifically designed to catch
//	hallucinations in synthesized responses. These checkers focus on:
//	- Structural claims (fabricated directories/files)
//	- Phantom files (references to non-existent files)
//	- Language confusion (wrong-language patterns)
//
// Inputs:
//
//	ctx - Context for cancellation.
//	response - The synthesized response text.
//	assembledCtx - The context that was given to the LLM.
//	retryCount - Current retry attempt (0 = first attempt).
//
// Outputs:
//
//	*PostSynthesisResult - The verification result.
//	error - Non-nil only if verification itself fails.
//
// Thread Safety: Safe for concurrent use.
func (g *DefaultGrounder) VerifyPostSynthesis(ctx context.Context, response string, assembledCtx *agent.AssembledContext, retryCount int) (*PostSynthesisResult, error) {
	config := g.config.PostSynthesisConfig
	if config == nil {
		config = DefaultPostSynthesisConfig()
	}

	if !config.Enabled {
		return &PostSynthesisResult{
			Passed:          true,
			StrictnessLevel: StrictnessNormal,
		}, nil
	}

	start := time.Now()

	// Determine strictness level based on retry count
	level := determineStrictnessLevel(retryCount, config.MaxRetries)

	// Build check input
	input, err := g.buildCheckInput(response, assembledCtx)
	if err != nil {
		return nil, fmt.Errorf("building check input: %w", err)
	}

	// Get relevant checkers
	checkers := g.getPostSynthesisCheckers()

	result := &PostSynthesisResult{
		Passed:          true,
		RetryCount:      retryCount,
		StrictnessLevel: level,
	}

	// Run each checker
	for _, checker := range checkers {
		select {
		case <-ctx.Done():
			result.CheckDuration = time.Since(start)
			return result, ctx.Err()
		default:
		}

		violations := checker.Check(ctx, input)
		result.CheckersRun++

		for _, v := range violations {
			// Mark as post-synthesis violation
			v.Phase = "post_synthesis"
			v.RetryCount = retryCount

			result.Violations = append(result.Violations, v)

			// Record metric for post-synthesis violation
			RecordPostSynthesisViolation(ctx, v.Type, v.Severity, retryCount)

			// Any critical or high violation means we didn't pass
			if v.Severity == SeverityCritical || v.Severity == SeverityHigh {
				result.Passed = false
			}
		}
	}

	// Check if we need feedback loop
	if !result.Passed && level == StrictnessFeedback {
		result.NeedsFeedbackLoop = true
		result.FeedbackQuestions = generateFeedbackQuestions(result.Violations)

		// Record metric for feedback loop triggered
		RecordFeedbackLoopTriggered(ctx, len(result.FeedbackQuestions))
	}

	result.CheckDuration = time.Since(start)
	return result, nil
}

// GenerateStricterPrompt creates a prompt with increased strictness.
//
// Description:
//
//	Modifies the base synthesis prompt to include violation feedback
//	and strictness guidance based on the current strictness level.
//
// Inputs:
//
//	basePrompt - The original synthesis prompt.
//	violations - The violations from the previous attempt.
//	level - The strictness level to apply.
//
// Outputs:
//
//	string - The enhanced prompt with strictness guidance.
//
// Thread Safety: Safe for concurrent use (stateless function).
func (g *DefaultGrounder) GenerateStricterPrompt(basePrompt string, violations []Violation, level StrictnessLevel) string {
	var sb strings.Builder

	switch level {
	case StrictnessElevated:
		sb.WriteString("IMPORTANT: Your previous response had grounding issues. ")
		sb.WriteString("Please revise to address these violations:\n\n")

		for _, v := range violations {
			sb.WriteString(fmt.Sprintf("- %s: %s\n", v.Type, v.Message))
			if v.Suggestion != "" {
				sb.WriteString(fmt.Sprintf("  Suggestion: %s\n", v.Suggestion))
			}
		}
		sb.WriteString("\n")
		sb.WriteString(basePrompt)

	case StrictnessHigh:
		sb.WriteString("CRITICAL: Previous responses contained hallucinations. ")
		sb.WriteString("You MUST base your response ONLY on the tool results and code shown in context.\n\n")
		sb.WriteString("AVOID THESE PATTERNS:\n")

		for _, v := range violations {
			sb.WriteString(fmt.Sprintf("- DO NOT %s\n", describeViolationToAvoid(v)))
		}

		sb.WriteString("\nREQUIREMENTS:\n")
		sb.WriteString("- Only reference files that appear in tool output\n")
		sb.WriteString("- Only describe structures shown by ls/tree commands\n")
		sb.WriteString("- Only use patterns appropriate for the project's language\n\n")
		sb.WriteString(basePrompt)

	case StrictnessFeedback:
		// This level shouldn't generate a prompt - it triggers exploration
		sb.WriteString(basePrompt)

	default:
		sb.WriteString(basePrompt)
	}

	return sb.String()
}

// getPostSynthesisCheckers returns the subset of checkers for post-synthesis.
func (g *DefaultGrounder) getPostSynthesisCheckers() []Checker {
	var checkers []Checker

	// Find the relevant checkers from our checker list
	for _, checker := range g.checkers {
		name := checker.Name()
		if isPostSynthesisChecker(name) {
			checkers = append(checkers, checker)
		}
	}

	// If no checkers found (e.g., custom grounder), create defaults
	if len(checkers) == 0 {
		checkers = g.createDefaultPostSynthesisCheckers()
	}

	return checkers
}

// isPostSynthesisChecker returns true if the checker is relevant for post-synthesis.
func isPostSynthesisChecker(name string) bool {
	relevantNames := map[string]bool{
		"structural_claim_checker": true,
		"phantom_file_checker":     true,
		"language_checker":         true,
	}
	return relevantNames[name]
}

// createDefaultPostSynthesisCheckers creates the default post-synthesis checkers.
func (g *DefaultGrounder) createDefaultPostSynthesisCheckers() []Checker {
	checkers := make([]Checker, 0, 3)

	// Structural claim checker
	structConfig := g.config.StructuralClaimCheckerConfig
	if structConfig == nil {
		structConfig = DefaultStructuralClaimCheckerConfig()
	}
	checkers = append(checkers, NewStructuralClaimChecker(structConfig))

	// Phantom file checker
	phantomConfig := g.config.PhantomCheckerConfig
	if phantomConfig == nil {
		phantomConfig = DefaultPhantomCheckerConfig()
	}
	checkers = append(checkers, NewPhantomFileChecker(phantomConfig))

	// Language checker
	langConfig := g.config.LanguageCheckerConfig
	if langConfig == nil {
		langConfig = DefaultLanguageCheckerConfig()
	}
	checkers = append(checkers, NewLanguageChecker(langConfig))

	return checkers
}

// determineStrictnessLevel returns the strictness level based on retry count.
func determineStrictnessLevel(retryCount, maxRetries int) StrictnessLevel {
	if retryCount == 0 {
		return StrictnessNormal
	}
	if retryCount == 1 {
		return StrictnessElevated
	}
	if retryCount == 2 {
		return StrictnessHigh
	}
	// retryCount >= 3
	return StrictnessFeedback
}

// generateFeedbackQuestions creates questions to guide exploration based on violations.
//
// Description:
//
//	Analyzes the violations and generates specific questions that the
//	exploration phase should try to answer to gather better evidence.
//
// Inputs:
//
//	violations - The violations that triggered feedback loop.
//
// Outputs:
//
//	[]string - Questions to explore.
func generateFeedbackQuestions(violations []Violation) []string {
	questions := make([]string, 0, len(violations))
	seen := make(map[string]bool) // Deduplicate similar questions

	for _, v := range violations {
		var question string

		switch v.Type {
		case ViolationPhantomFile:
			// Ask to find the actual file
			question = fmt.Sprintf("Find the actual location of functionality related to: %s", v.Evidence)

		case ViolationStructuralClaim:
			// Ask to explore the real structure
			question = "Use ls or tree to explore the actual project directory structure"

		case ViolationLanguageConfusion:
			// Ask to identify the actual patterns used
			question = fmt.Sprintf("Identify the actual implementation patterns used instead of: %s", v.Evidence)

		case ViolationGenericPattern:
			// Ask to find specific implementations
			question = "Search for the specific implementation details in the codebase"

		default:
			// Generic question for other violation types
			if v.Evidence != "" {
				question = fmt.Sprintf("Verify the claim about: %s", v.Evidence)
			}
		}

		if question != "" && !seen[question] {
			seen[question] = true
			questions = append(questions, question)
		}
	}

	// Add a general grounding question if no specific ones generated
	if len(questions) == 0 {
		questions = append(questions, "Use exploration tools to gather concrete evidence from the codebase")
	}

	return questions
}

// describeViolationToAvoid creates a description of what to avoid based on violation.
func describeViolationToAvoid(v Violation) string {
	switch v.Type {
	case ViolationPhantomFile:
		return fmt.Sprintf("reference non-existent file: %s", v.Evidence)

	case ViolationStructuralClaim:
		return "describe directory structures without ls/tree evidence"

	case ViolationLanguageConfusion:
		if v.Evidence != "" {
			return fmt.Sprintf("use %s patterns in this project", v.Evidence)
		}
		return "mix patterns from different programming languages"

	case ViolationGenericPattern:
		return "describe generic patterns instead of project-specific code"

	default:
		if v.Message != "" {
			return strings.ToLower(v.Message)
		}
		return "make ungrounded claims"
	}
}
