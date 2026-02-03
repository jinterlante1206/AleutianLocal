// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package lint

import (
	"encoding/json"
	"fmt"
	"strings"
)

// =============================================================================
// GOLANGCI-LINT PARSER
// =============================================================================

// golangciOutput represents the JSON output from golangci-lint.
type golangciOutput struct {
	Issues []golangciIssue `json:"Issues"`
}

type golangciIssue struct {
	FromLinter           string           `json:"FromLinter"`
	Text                 string           `json:"Text"`
	Severity             string           `json:"Severity"`
	SourceLines          []string         `json:"SourceLines"`
	Pos                  golangciPosition `json:"Pos"`
	LineRange            *golangciRange   `json:"LineRange,omitempty"`
	Replacement          *golangciReplace `json:"Replacement,omitempty"`
	ExpectNoLint         bool             `json:"ExpectNoLint"`
	ExpectedNoLintLinter string           `json:"ExpectedNoLintLinter"`
}

type golangciPosition struct {
	Filename string `json:"Filename"`
	Line     int    `json:"Line"`
	Column   int    `json:"Column"`
	Offset   int    `json:"Offset"`
}

type golangciRange struct {
	From int `json:"From"`
	To   int `json:"To"`
}

type golangciReplace struct {
	NeedOnlyDelete bool            `json:"NeedOnlyDelete"`
	NewLines       []string        `json:"NewLines"`
	Inline         *golangciInline `json:"Inline,omitempty"`
}

type golangciInline struct {
	StartCol  int    `json:"StartCol"`
	Length    int    `json:"Length"`
	NewString string `json:"NewString"`
}

// parseGolangCIOutput parses JSON output from golangci-lint.
//
// Description:
//
//	golangci-lint --out-format=json produces a JSON object with an
//	"Issues" array. Each issue contains linter name, message, position,
//	and optional replacement information.
//
// Inputs:
//
//	data - Raw JSON output from golangci-lint
//
// Outputs:
//
//	[]LintIssue - Parsed issues
//	error - Non-nil if JSON parsing fails
func parseGolangCIOutput(data []byte) ([]LintIssue, error) {
	var output golangciOutput
	if err := json.Unmarshal(data, &output); err != nil {
		// Try to provide helpful error message
		return nil, fmt.Errorf("parsing golangci-lint output: %w", err)
	}

	if len(output.Issues) == 0 {
		return nil, nil
	}

	issues := make([]LintIssue, 0, len(output.Issues))
	for _, gi := range output.Issues {
		issue := LintIssue{
			File:     gi.Pos.Filename,
			Line:     gi.Pos.Line,
			Column:   gi.Pos.Column,
			Rule:     gi.FromLinter,
			Severity: mapGolangCISeverity(gi.Severity),
			Message:  gi.Text,
			Linter:   "golangci-lint",
		}

		// Set end line if available
		if gi.LineRange != nil {
			issue.EndLine = gi.LineRange.To
		}

		// Check for auto-fix
		if gi.Replacement != nil {
			issue.CanAutoFix = true
			if gi.Replacement.Inline != nil {
				issue.Replacement = gi.Replacement.Inline.NewString
			} else if len(gi.Replacement.NewLines) > 0 {
				issue.Replacement = strings.Join(gi.Replacement.NewLines, "\n")
			}
			issue.Suggestion = fmt.Sprintf("Replace with: %s", issue.Replacement)
		}

		issues = append(issues, issue)
	}

	return issues, nil
}

// mapGolangCISeverity maps golangci-lint severity to our Severity.
func mapGolangCISeverity(s string) Severity {
	switch strings.ToLower(s) {
	case "error":
		return SeverityError
	case "warning":
		return SeverityWarning
	default:
		// golangci-lint doesn't always set severity
		return SeverityWarning
	}
}

// =============================================================================
// RUFF PARSER
// =============================================================================

// ruffIssue represents a single issue from Ruff JSON output.
type ruffIssue struct {
	Code        string       `json:"code"`
	EndLocation ruffLocation `json:"end_location"`
	Filename    string       `json:"filename"`
	Fix         *ruffFix     `json:"fix"`
	Location    ruffLocation `json:"location"`
	Message     string       `json:"message"`
	NoqaRow     int          `json:"noqa_row"`
	URL         string       `json:"url"`
}

type ruffLocation struct {
	Column int `json:"column"`
	Row    int `json:"row"`
}

type ruffFix struct {
	Applicability string     `json:"applicability"`
	Edits         []ruffEdit `json:"edits"`
	Message       string     `json:"message"`
}

type ruffEdit struct {
	Content     string       `json:"content"`
	EndLocation ruffLocation `json:"end_location"`
	Location    ruffLocation `json:"location"`
}

// parseRuffOutput parses JSON output from Ruff.
//
// Description:
//
//	Ruff produces a JSON array of issues. Each issue contains a code,
//	location, message, and optional fix information.
//
// Inputs:
//
//	data - Raw JSON output from ruff check --output-format=json
//
// Outputs:
//
//	[]LintIssue - Parsed issues
//	error - Non-nil if JSON parsing fails
func parseRuffOutput(data []byte) ([]LintIssue, error) {
	var ruffIssues []ruffIssue
	if err := json.Unmarshal(data, &ruffIssues); err != nil {
		return nil, fmt.Errorf("parsing ruff output: %w", err)
	}

	if len(ruffIssues) == 0 {
		return nil, nil
	}

	issues := make([]LintIssue, 0, len(ruffIssues))
	for _, ri := range ruffIssues {
		issue := LintIssue{
			File:      ri.Filename,
			Line:      ri.Location.Row,
			Column:    ri.Location.Column,
			EndLine:   ri.EndLocation.Row,
			EndColumn: ri.EndLocation.Column,
			Rule:      ri.Code,
			RuleURL:   ri.URL,
			Severity:  mapRuffSeverity(ri.Code),
			Message:   ri.Message,
			Linter:    "ruff",
		}

		// Check for auto-fix
		if ri.Fix != nil && len(ri.Fix.Edits) > 0 {
			issue.CanAutoFix = ri.Fix.Applicability == "safe" || ri.Fix.Applicability == "always"
			issue.Suggestion = ri.Fix.Message
			if len(ri.Fix.Edits) == 1 {
				issue.Replacement = ri.Fix.Edits[0].Content
			}
		}

		issues = append(issues, issue)
	}

	return issues, nil
}

// mapRuffSeverity maps Ruff rule codes to our Severity.
func mapRuffSeverity(code string) Severity {
	// Ruff uses letter prefixes for rule categories
	if len(code) == 0 {
		return SeverityWarning
	}

	prefix := strings.ToUpper(code[:1])
	switch prefix {
	case "E": // pycodestyle errors
		return SeverityError
	case "F": // Pyflakes
		return SeverityError
	case "S": // flake8-bandit (security)
		return SeverityError
	case "W": // pycodestyle warnings
		return SeverityWarning
	case "C": // complexity
		return SeverityWarning
	case "I": // isort
		return SeverityInfo
	case "D": // pydocstyle
		return SeverityInfo
	default:
		return SeverityWarning
	}
}

// =============================================================================
// ESLINT PARSER
// =============================================================================

// eslintOutput represents the JSON output from ESLint.
type eslintOutput []eslintFile

type eslintFile struct {
	FilePath            string          `json:"filePath"`
	Messages            []eslintMessage `json:"messages"`
	ErrorCount          int             `json:"errorCount"`
	WarningCount        int             `json:"warningCount"`
	FixableErrorCount   int             `json:"fixableErrorCount"`
	FixableWarningCount int             `json:"fixableWarningCount"`
}

type eslintMessage struct {
	RuleID      string             `json:"ruleId"`
	Severity    int                `json:"severity"` // 1 = warning, 2 = error
	Message     string             `json:"message"`
	Line        int                `json:"line"`
	Column      int                `json:"column"`
	EndLine     int                `json:"endLine"`
	EndColumn   int                `json:"endColumn"`
	Fix         *eslintFix         `json:"fix"`
	Suggestions []eslintSuggestion `json:"suggestions"`
}

type eslintFix struct {
	Range [2]int `json:"range"`
	Text  string `json:"text"`
}

type eslintSuggestion struct {
	Desc string    `json:"desc"`
	Fix  eslintFix `json:"fix"`
}

// parseESLintOutput parses JSON output from ESLint.
//
// Description:
//
//	ESLint --format=json produces an array of file results.
//	Each file result contains messages with severity, rule, and
//	optional fix information.
//
// Inputs:
//
//	data - Raw JSON output from eslint --format=json
//
// Outputs:
//
//	[]LintIssue - Parsed issues
//	error - Non-nil if JSON parsing fails
func parseESLintOutput(data []byte) ([]LintIssue, error) {
	var output eslintOutput
	if err := json.Unmarshal(data, &output); err != nil {
		return nil, fmt.Errorf("parsing eslint output: %w", err)
	}

	var issues []LintIssue
	for _, file := range output {
		for _, msg := range file.Messages {
			issue := LintIssue{
				File:      file.FilePath,
				Line:      msg.Line,
				Column:    msg.Column,
				EndLine:   msg.EndLine,
				EndColumn: msg.EndColumn,
				Rule:      msg.RuleID,
				Severity:  mapESLintSeverity(msg.Severity),
				Message:   msg.Message,
				Linter:    "eslint",
			}

			// Check for auto-fix
			if msg.Fix != nil {
				issue.CanAutoFix = true
				issue.Replacement = msg.Fix.Text
			}

			// Check for suggestions
			if len(msg.Suggestions) > 0 {
				issue.Suggestion = msg.Suggestions[0].Desc
				if !issue.CanAutoFix {
					issue.CanAutoFix = true
					issue.Replacement = msg.Suggestions[0].Fix.Text
				}
			}

			issues = append(issues, issue)
		}
	}

	return issues, nil
}

// mapESLintSeverity maps ESLint numeric severity to our Severity.
func mapESLintSeverity(severity int) Severity {
	switch severity {
	case 2: // error
		return SeverityError
	case 1: // warning
		return SeverityWarning
	default:
		return SeverityInfo
	}
}

// =============================================================================
// PARSER REGISTRY
// =============================================================================

// ParserFunc is a function that parses linter output into issues.
type ParserFunc func(data []byte) ([]LintIssue, error)

// parserRegistry maps language names to parser functions.
var parserRegistry = map[string]ParserFunc{
	"go":         parseGolangCIOutput,
	"python":     parseRuffOutput,
	"typescript": parseESLintOutput,
	"javascript": parseESLintOutput,
}

// GetParser returns the parser function for a language.
//
// Description:
//
//	Returns the registered parser for the given language, or nil if
//	no parser is registered.
//
// Inputs:
//
//	language - The language identifier
//
// Outputs:
//
//	ParserFunc - The parser function, or nil if not found
func GetParser(language string) ParserFunc {
	return parserRegistry[language]
}

// RegisterParser adds or replaces a parser for a language.
//
// Description:
//
//	Allows custom parsers to be registered for additional linters
//	or to override default behavior.
//
// Inputs:
//
//	language - The language identifier
//	parser - The parser function
func RegisterParser(language string, parser ParserFunc) {
	parserRegistry[language] = parser
}
