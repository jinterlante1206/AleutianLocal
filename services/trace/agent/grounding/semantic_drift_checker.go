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
	"regexp"
	"strings"
)

// QuestionType categorizes the type of question being asked.
type QuestionType int

const (
	// QuestionUnknown for questions that don't match known patterns.
	QuestionUnknown QuestionType = iota

	// QuestionList for "What X exist?", "List", "Show all" questions.
	QuestionList

	// QuestionHow for "How does", "How is", "How to" questions.
	QuestionHow

	// QuestionWhere for "Where is", "Where are" questions.
	QuestionWhere

	// QuestionWhy for "Why does", "Why is" questions.
	QuestionWhy

	// QuestionWhat for "What is", "What does" questions (definitional).
	QuestionWhat

	// QuestionDescribe for "Describe", "Explain" questions.
	QuestionDescribe
)

// String returns the string representation of a QuestionType.
func (qt QuestionType) String() string {
	switch qt {
	case QuestionList:
		return "list"
	case QuestionHow:
		return "how"
	case QuestionWhere:
		return "where"
	case QuestionWhy:
		return "why"
	case QuestionWhat:
		return "what"
	case QuestionDescribe:
		return "describe"
	default:
		return "unknown"
	}
}

// Package-level compiled regexes for question type detection (compiled once).
var (
	// listQuestionPattern detects list-type questions (direct form).
	listQuestionPattern = regexp.MustCompile(
		`(?i)^(?:what\s+(?:\w+\s+)*(?:exist|are there|do we have)|` +
			`list\s+(?:all\s+)?|` +
			`show\s+(?:all\s+)?|` +
			`find\s+(?:all\s+)?|` +
			`enumerate|` +
			`what\s+(?:\w+\s+)*files?)`,
	)

	// listQuestionPatternIndirect detects indirect list-type questions.
	// E.g., "Can you tell me what tests exist?" or "I'd like to know what files are there"
	listQuestionPatternIndirect = regexp.MustCompile(
		`(?i)(?:can\s+you|could\s+you|please|i(?:'d| would)\s+like\s+to\s+know)\s+` +
			`(?:tell\s+me\s+)?(?:what\s+(?:\w+\s+)*(?:exist|are there|do we have)|` +
			`(?:list|show|find)\s+(?:all\s+)?)`,
	)

	// howQuestionPattern detects how-type questions (direct form).
	howQuestionPattern = regexp.MustCompile(
		`(?i)^how\s+(?:does|is|do|are|can|to|should)`,
	)

	// howQuestionPatternIndirect detects indirect how-type questions.
	howQuestionPatternIndirect = regexp.MustCompile(
		`(?i)(?:can\s+you|could\s+you|please)\s+(?:explain|tell\s+me|describe)\s+how`,
	)

	// whereQuestionPattern detects where-type questions (direct form).
	whereQuestionPattern = regexp.MustCompile(
		`(?i)^where\s+(?:is|are|do|does|can)`,
	)

	// whereQuestionPatternIndirect detects indirect where-type questions.
	whereQuestionPatternIndirect = regexp.MustCompile(
		`(?i)(?:can\s+you|could\s+you|please)\s+(?:tell\s+me|show\s+me|find)\s+where`,
	)

	// whyQuestionPattern detects why-type questions (direct form).
	whyQuestionPattern = regexp.MustCompile(
		`(?i)^why\s+(?:does|is|do|are|did|was|were)`,
	)

	// whyQuestionPatternIndirect detects indirect why-type questions.
	whyQuestionPatternIndirect = regexp.MustCompile(
		`(?i)(?:can\s+you|could\s+you|please)\s+(?:explain|tell\s+me)\s+why`,
	)

	// whatQuestionPattern detects definitional what questions (direct form).
	whatQuestionPattern = regexp.MustCompile(
		`(?i)^what\s+(?:is|does|are)\s+(?:the\s+)?[a-z]`,
	)

	// whatQuestionPatternIndirect detects indirect what questions.
	whatQuestionPatternIndirect = regexp.MustCompile(
		`(?i)(?:can\s+you|could\s+you|please)\s+(?:tell\s+me|explain)\s+what\s+(?:is|does|are)`,
	)

	// describeQuestionPattern detects describe/explain questions (direct form).
	describeQuestionPattern = regexp.MustCompile(
		`(?i)^(?:describe|explain|tell\s+me\s+about)`,
	)

	// describeQuestionPatternIndirect detects indirect describe/explain questions.
	describeQuestionPatternIndirect = regexp.MustCompile(
		`(?i)(?:can\s+you|could\s+you|please)\s+(?:describe|explain|give\s+(?:me\s+)?(?:an?\s+)?(?:overview|summary))`,
	)

	// wordPattern extracts words from text.
	wordPattern = regexp.MustCompile(`[a-zA-Z][a-zA-Z0-9_]*`)

	// numberedListPattern detects numbered lists (1., 2., etc.)
	numberedListPattern = regexp.MustCompile(`\d+\.\s`)

	// filePathIndicatorPattern detects file path references.
	filePathIndicatorPattern = regexp.MustCompile(`[a-zA-Z_][a-zA-Z0-9_/\\-]*\.(go|py|js|ts|java|rs|c|cpp|h)`)
)

// stopWords are common words to exclude from keyword extraction.
var stopWords = map[string]bool{
	"a": true, "an": true, "the": true, "is": true, "are": true,
	"was": true, "were": true, "be": true, "been": true, "being": true,
	"have": true, "has": true, "had": true, "do": true, "does": true,
	"did": true, "will": true, "would": true, "could": true, "should": true,
	"may": true, "might": true, "must": true, "shall": true,
	"i": true, "you": true, "he": true, "she": true, "it": true,
	"we": true, "they": true, "me": true, "him": true, "her": true,
	"us": true, "them": true, "my": true, "your": true, "his": true,
	"its": true, "our": true, "their": true,
	"this": true, "that": true, "these": true, "those": true,
	"what": true, "which": true, "who": true, "whom": true, "whose": true,
	"where": true, "when": true, "why": true, "how": true,
	"all": true, "each": true, "every": true, "both": true, "few": true,
	"more": true, "most": true, "other": true, "some": true, "such": true,
	"no": true, "not": true, "only": true, "own": true, "same": true,
	"so": true, "than": true, "too": true, "very": true,
	"can": true, "just": true, "now": true, "also": true,
	"and": true, "or": true, "but": true, "if": true, "then": true,
	"for": true, "of": true, "to": true, "in": true, "on": true,
	"at": true, "by": true, "from": true, "with": true, "about": true,
	"into": true, "through": true, "during": true, "before": true,
	"after": true, "above": true, "below": true, "between": true,
	"under": true, "again": true, "further": true, "once": true,
	"there": true, "here": true, "any": true, "as": true,
}

// synonymGroups maps canonical terms to their synonyms.
var synonymGroups = map[string][]string{
	"test":   {"test", "tests", "testing", "spec", "specs", "unittest", "unit"},
	"config": {"config", "configs", "configuration", "configurations", "settings", "options", "option"},
	"error":  {"error", "errors", "exception", "exceptions", "failure", "failures", "fail"},
	"file":   {"file", "files", "document", "documents"},
	"func":   {"function", "functions", "func", "funcs", "method", "methods"},
	"type":   {"type", "types", "struct", "structs", "class", "classes", "interface", "interfaces"},
	"api":    {"api", "apis", "endpoint", "endpoints", "route", "routes"},
	"db":     {"database", "databases", "db", "dbs", "storage", "store"},
	"auth":   {"auth", "authentication", "authorization", "login", "logout"},
}

// SemanticDriftChecker detects when response doesn't address the original question.
//
// This checker validates that the LLM response is actually answering the question
// that was asked. It uses keyword overlap, topic coherence, and question type
// matching to detect when the model has "drifted" to answering a different question.
//
// Thread Safety: Safe for concurrent use (stateless after construction).
type SemanticDriftChecker struct {
	config         *SemanticDriftCheckerConfig
	synonymLookup  map[string]string // maps synonym -> canonical
	stopWordsLower map[string]bool   // lowercase stop words
}

// NewSemanticDriftChecker creates a new semantic drift checker.
//
// Description:
//
//	Creates a checker that detects when responses don't address the question.
//	Uses keyword overlap, topic coherence, and question type matching.
//
// Inputs:
//   - config: Configuration for the checker (nil uses defaults).
//
// Outputs:
//   - *SemanticDriftChecker: The configured checker.
//
// Thread Safety: Safe for concurrent use.
func NewSemanticDriftChecker(config *SemanticDriftCheckerConfig) *SemanticDriftChecker {
	if config == nil {
		config = DefaultSemanticDriftCheckerConfig()
	}

	// Build synonym lookup (synonym -> canonical)
	synonymLookup := make(map[string]string)
	for canonical, synonyms := range synonymGroups {
		for _, syn := range synonyms {
			synonymLookup[strings.ToLower(syn)] = canonical
		}
	}

	// Copy stopWords for defense-in-depth (prevents accidental mutation of package-level map)
	stopWordsCopy := make(map[string]bool, len(stopWords))
	for word, val := range stopWords {
		stopWordsCopy[word] = val
	}

	return &SemanticDriftChecker{
		config:         config,
		synonymLookup:  synonymLookup,
		stopWordsLower: stopWordsCopy,
	}
}

// Name implements Checker.
func (c *SemanticDriftChecker) Name() string {
	return "semantic_drift_checker"
}

// Check implements Checker.
//
// Description:
//
//	Analyzes the response to determine if it addresses the original question.
//	Uses keyword overlap, topic coherence, and question type matching to
//	calculate a drift score. High drift scores indicate the response is
//	off-topic.
//
// Inputs:
//   - ctx: Context for cancellation.
//   - input: The check input containing response and user question.
//
// Outputs:
//   - []Violation: Any violations found.
//
// Thread Safety: Safe for concurrent use.
func (c *SemanticDriftChecker) Check(ctx context.Context, input *CheckInput) []Violation {
	if !c.config.Enabled {
		return nil
	}

	// Need question to check against
	if input.UserQuestion == "" {
		return nil
	}

	// Skip very short responses
	if len(input.Response) < c.config.MinResponseLength {
		return nil
	}

	// Extract keywords from question
	questionKeywords := c.extractKeywords(input.UserQuestion)

	// Need minimum keywords to perform meaningful check
	if len(questionKeywords) < c.config.MinKeywords {
		return nil
	}

	// Detect question type
	questionType := c.classifyQuestion(input.UserQuestion)

	// Extract keywords from response
	responseKeywords := c.extractKeywords(input.Response)

	// Calculate drift score components
	keywordOverlap := c.calculateKeywordOverlap(questionKeywords, responseKeywords)
	topicMismatch := c.calculateTopicMismatch(questionKeywords, responseKeywords)
	typeMismatch := c.calculateTypeMismatch(questionType, input.Response)

	// Calculate weighted drift score (additive formula)
	driftScore := (1.0-keywordOverlap)*c.config.KeywordWeight +
		topicMismatch*c.config.TopicWeight +
		typeMismatch*c.config.TypeWeight

	// Clamp to [0, 1]
	if driftScore > 1.0 {
		driftScore = 1.0
	}
	if driftScore < 0.0 {
		driftScore = 0.0
	}

	select {
	case <-ctx.Done():
		return nil
	default:
	}

	// Determine severity based on thresholds
	var violations []Violation
	if driftScore >= c.config.CriticalThreshold {
		violations = append(violations, c.createViolation(
			SeverityCritical,
			driftScore,
			questionType,
			input.UserQuestion,
		))
		RecordSemanticDrift(ctx, driftScore, questionType.String(), SeverityCritical)
	} else if driftScore >= c.config.HighThreshold {
		violations = append(violations, c.createViolation(
			SeverityHigh,
			driftScore,
			questionType,
			input.UserQuestion,
		))
		RecordSemanticDrift(ctx, driftScore, questionType.String(), SeverityHigh)
	} else if driftScore >= c.config.WarningThreshold {
		violations = append(violations, c.createViolation(
			SeverityWarning,
			driftScore,
			questionType,
			input.UserQuestion,
		))
		RecordSemanticDrift(ctx, driftScore, questionType.String(), SeverityWarning)
	}

	return violations
}

// classifyQuestion determines the type of question being asked.
//
// Description:
//
//	Classifies questions by checking both direct patterns (e.g., "What tests exist?")
//	and indirect patterns (e.g., "Can you tell me what tests exist?").
//	Direct patterns are checked first, then indirect patterns as fallback.
//
// Inputs:
//   - question: The question to classify.
//
// Outputs:
//   - QuestionType: The classified type.
func (c *SemanticDriftChecker) classifyQuestion(question string) QuestionType {
	q := strings.TrimSpace(question)

	// Check direct patterns first (in order of specificity)
	if listQuestionPattern.MatchString(q) {
		return QuestionList
	}
	if howQuestionPattern.MatchString(q) {
		return QuestionHow
	}
	if whereQuestionPattern.MatchString(q) {
		return QuestionWhere
	}
	if whyQuestionPattern.MatchString(q) {
		return QuestionWhy
	}
	if describeQuestionPattern.MatchString(q) {
		return QuestionDescribe
	}
	if whatQuestionPattern.MatchString(q) {
		return QuestionWhat
	}

	// Check indirect patterns as fallback for questions like
	// "Can you tell me what tests exist?" or "Could you explain how this works?"
	if listQuestionPatternIndirect.MatchString(q) {
		return QuestionList
	}
	if howQuestionPatternIndirect.MatchString(q) {
		return QuestionHow
	}
	if whereQuestionPatternIndirect.MatchString(q) {
		return QuestionWhere
	}
	if whyQuestionPatternIndirect.MatchString(q) {
		return QuestionWhy
	}
	if describeQuestionPatternIndirect.MatchString(q) {
		return QuestionDescribe
	}
	if whatQuestionPatternIndirect.MatchString(q) {
		return QuestionWhat
	}

	return QuestionUnknown
}

// extractKeywords extracts meaningful keywords from text.
//
// Description:
//
//	Extracts words from text, filters out stop words, and normalizes
//	synonyms to canonical forms. Returns a map of keywords for O(1) lookup.
//
// Inputs:
//   - text: The text to extract keywords from.
//
// Outputs:
//   - map[string]bool: Set of keywords found.
func (c *SemanticDriftChecker) extractKeywords(text string) map[string]bool {
	keywords := make(map[string]bool)

	// Extract all words
	matches := wordPattern.FindAllString(text, -1)
	for _, word := range matches {
		lower := strings.ToLower(word)

		// Skip stop words
		if c.stopWordsLower[lower] {
			continue
		}

		// Skip very short words (< 2 chars after lowercasing)
		if len(lower) < 2 {
			continue
		}

		// Normalize via synonym lookup
		if canonical, ok := c.synonymLookup[lower]; ok {
			keywords[canonical] = true
		} else {
			keywords[lower] = true
		}
	}

	return keywords
}

// calculateKeywordOverlap calculates the proportion of question keywords in response.
//
// Inputs:
//   - questionKeywords: Keywords from the question.
//   - responseKeywords: Keywords from the response.
//
// Outputs:
//   - float64: Overlap score from 0.0 (no overlap) to 1.0 (all keywords present).
func (c *SemanticDriftChecker) calculateKeywordOverlap(questionKeywords, responseKeywords map[string]bool) float64 {
	if len(questionKeywords) == 0 {
		return 1.0 // No keywords to check
	}

	matchCount := 0
	for keyword := range questionKeywords {
		if responseKeywords[keyword] {
			matchCount++
		}
	}

	return float64(matchCount) / float64(len(questionKeywords))
}

// calculateTopicMismatch detects topic drift using synonym groups.
//
// Description:
//
//	Checks if question mentions topics that response completely ignores.
//	Uses synonym groups to detect when question topic is absent from response.
//
// Inputs:
//   - questionKeywords: Keywords from the question.
//   - responseKeywords: Keywords from the response.
//
// Outputs:
//   - float64: Mismatch score from 0.0 (topics aligned) to 1.0 (topics mismatched).
func (c *SemanticDriftChecker) calculateTopicMismatch(questionKeywords, responseKeywords map[string]bool) float64 {
	// Find canonical topics in question
	questionTopics := make(map[string]bool)
	for keyword := range questionKeywords {
		if _, isCanonical := synonymGroups[keyword]; isCanonical {
			questionTopics[keyword] = true
		}
	}

	// If no recognized topics in question, can't assess mismatch
	if len(questionTopics) == 0 {
		return 0.0
	}

	// Check which topics are present in response
	responseTopics := make(map[string]bool)
	for keyword := range responseKeywords {
		if _, isCanonical := synonymGroups[keyword]; isCanonical {
			responseTopics[keyword] = true
		}
	}

	// Calculate what proportion of question topics are missing from response
	missingCount := 0
	for topic := range questionTopics {
		if !responseTopics[topic] {
			missingCount++
		}
	}

	return float64(missingCount) / float64(len(questionTopics))
}

// calculateTypeMismatch checks if response format matches question type.
//
// Description:
//
//	LIST questions should have list-like responses.
//	HOW questions should describe processes.
//	WHERE questions should reference locations.
//	Penalties are configurable via SemanticDriftCheckerConfig.
//
// Inputs:
//   - questionType: The classified question type.
//   - response: The response text.
//
// Outputs:
//   - float64: Mismatch score from 0.0 (format matches) to 1.0 (format mismatch).
func (c *SemanticDriftChecker) calculateTypeMismatch(questionType QuestionType, response string) float64 {
	switch questionType {
	case QuestionList:
		// LIST questions should have list-like responses
		if c.hasListIndicators(response) {
			return 0.0
		}
		return c.config.ListTypeMismatchPenalty

	case QuestionWhere:
		// WHERE questions should mention locations/files
		if c.hasLocationIndicators(response) {
			return 0.0
		}
		return c.config.WhereTypeMismatchPenalty

	case QuestionHow:
		// HOW questions should describe processes
		if c.hasProcessIndicators(response) {
			return 0.0
		}
		return c.config.HowTypeMismatchPenalty

	case QuestionUnknown:
		// Can't assess type match for unknown questions
		return 0.0

	default:
		// For other question types, don't penalize heavily
		return 0.0
	}
}

// hasListIndicators checks if response contains list-like structure.
func (c *SemanticDriftChecker) hasListIndicators(response string) bool {
	// Check for numbered lists (1., 2., etc.)
	if numberedListPattern.MatchString(response) {
		return true
	}

	// Check for bullet points
	if strings.Contains(response, "- ") || strings.Contains(response, "* ") {
		return true
	}

	// Check for enumeration words
	lower := strings.ToLower(response)
	if strings.Contains(lower, "following") ||
		strings.Contains(lower, "include") ||
		strings.Contains(lower, "such as") ||
		strings.Contains(lower, ":") {
		return true
	}

	return false
}

// hasLocationIndicators checks if response mentions locations/files.
func (c *SemanticDriftChecker) hasLocationIndicators(response string) bool {
	// Check for file paths
	if filePathIndicatorPattern.MatchString(response) {
		return true
	}

	// Check for location words
	lower := strings.ToLower(response)
	if strings.Contains(lower, "located") ||
		strings.Contains(lower, "found in") ||
		strings.Contains(lower, "defined in") ||
		strings.Contains(lower, "directory") ||
		strings.Contains(lower, "folder") ||
		strings.Contains(lower, "package") {
		return true
	}

	return false
}

// hasProcessIndicators checks if response describes a process.
func (c *SemanticDriftChecker) hasProcessIndicators(response string) bool {
	lower := strings.ToLower(response)

	// Process words
	if strings.Contains(lower, "first") ||
		strings.Contains(lower, "then") ||
		strings.Contains(lower, "next") ||
		strings.Contains(lower, "finally") ||
		strings.Contains(lower, "step") ||
		strings.Contains(lower, "process") ||
		strings.Contains(lower, "workflow") ||
		strings.Contains(lower, "sequence") {
		return true
	}

	// Verb patterns indicating process
	if strings.Contains(lower, "by ") ||
		strings.Contains(lower, "using ") ||
		strings.Contains(lower, "through ") ||
		strings.Contains(lower, "via ") {
		return true
	}

	return false
}

// createViolation creates a semantic drift violation.
func (c *SemanticDriftChecker) createViolation(
	severity Severity,
	driftScore float64,
	questionType QuestionType,
	question string,
) Violation {
	// Truncate question for message if too long
	truncatedQuestion := question
	if len(truncatedQuestion) > 100 {
		truncatedQuestion = truncatedQuestion[:97] + "..."
	}

	return Violation{
		Type:     ViolationSemanticDrift,
		Severity: severity,
		Code:     "SEMANTIC_DRIFT",
		Message: fmt.Sprintf(
			"Response may not address the original question (drift score: %.2f)",
			driftScore,
		),
		Evidence: truncatedQuestion,
		Expected: fmt.Sprintf("Response should address: %s", truncatedQuestion),
		Suggestion: fmt.Sprintf(
			"Re-read the original question and ensure the response directly "+
				"answers it. Question type appears to be '%s'.",
			questionType.String(),
		),
	}
}
