// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package rules

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/AleutianAI/AleutianFOSS/services/trace/analysis"
)

// RuleEngine evaluates custom rules against blast radius results.
//
// # Description
//
// Allows organizations to define custom risk rules that trigger actions
// such as blocking, warnings, or review requirements. Rules are loaded
// from YAML configuration files.
//
// # Thread Safety
//
// Safe for concurrent use after construction.
type RuleEngine struct {
	mu    sync.RWMutex
	rules []Rule
}

// Rule defines a custom risk rule.
type Rule struct {
	// Name is the rule identifier.
	Name string `yaml:"name" json:"name"`

	// Description explains what the rule checks.
	Description string `yaml:"description" json:"description,omitempty"`

	// Enabled indicates if the rule is active.
	Enabled bool `yaml:"enabled" json:"enabled"`

	// Condition is the condition that triggers this rule.
	Condition Condition `yaml:"condition" json:"condition"`

	// Action is what happens when the rule matches.
	Action Action `yaml:"action" json:"action"`

	// Severity is the severity level when triggered.
	Severity string `yaml:"severity" json:"severity,omitempty"`

	// Message is the message shown when the rule triggers.
	Message string `yaml:"message" json:"message,omitempty"`

	// Exemptions are patterns that exempt matching symbols.
	Exemptions []string `yaml:"exemptions" json:"exemptions,omitempty"`
}

// Condition specifies when a rule matches.
type Condition struct {
	// CallerCountGT matches if caller count is greater than this.
	CallerCountGT *int `yaml:"caller_count_gt" json:"caller_count_gt,omitempty"`

	// CallerCountGTE matches if caller count is >= this.
	CallerCountGTE *int `yaml:"caller_count_gte" json:"caller_count_gte,omitempty"`

	// SecurityPathIn matches if any security path is in this list.
	SecurityPathIn []string `yaml:"security_path_in" json:"security_path_in,omitempty"`

	// PackageMatch matches if package path matches this regex.
	PackageMatch *string `yaml:"package_match" json:"package_match,omitempty"`

	// SymbolMatch matches if symbol ID matches this regex.
	SymbolMatch *string `yaml:"symbol_match" json:"symbol_match,omitempty"`

	// ChurnLevelIn matches if churn level is in this list.
	ChurnLevelIn []string `yaml:"churn_level_in" json:"churn_level_in,omitempty"`

	// RiskLevelIn matches if risk level is in this list.
	RiskLevelIn []string `yaml:"risk_level_in" json:"risk_level_in,omitempty"`

	// HasDeadCallers matches if there are dead callers.
	HasDeadCallers *bool `yaml:"has_dead_callers" json:"has_dead_callers,omitempty"`

	// TransitiveCountGT matches if transitive count is greater than this.
	TransitiveCountGT *int `yaml:"transitive_count_gt" json:"transitive_count_gt,omitempty"`

	// CoverageRiskIn matches if coverage risk is in this list.
	CoverageRiskIn []string `yaml:"coverage_risk_in" json:"coverage_risk_in,omitempty"`

	// ConfidenceLT matches if confidence is less than this.
	ConfidenceLT *int `yaml:"confidence_lt" json:"confidence_lt,omitempty"`

	// And combines multiple conditions (all must match).
	And []Condition `yaml:"and" json:"and,omitempty"`

	// Or combines multiple conditions (any must match).
	Or []Condition `yaml:"or" json:"or,omitempty"`

	// Not negates the condition.
	Not *Condition `yaml:"not" json:"not,omitempty"`
}

// Action specifies what to do when a rule matches.
type Action string

const (
	ActionBlock         Action = "BLOCK"
	ActionWarn          Action = "WARN"
	ActionRequireReview Action = "REQUIRE_REVIEW"
	ActionNotify        Action = "NOTIFY"
	ActionLog           Action = "LOG"
)

// RuleResult represents the result of evaluating a rule.
type RuleResult struct {
	// Rule is the rule that was evaluated.
	Rule *Rule `json:"rule"`

	// Matched indicates if the rule matched.
	Matched bool `json:"matched"`

	// Action is the action to take.
	Action Action `json:"action"`

	// Message is the result message.
	Message string `json:"message"`

	// MatchDetails explains why the rule matched.
	MatchDetails []string `json:"match_details,omitempty"`
}

// EvaluationResult contains all rule evaluation results.
type EvaluationResult struct {
	// Results are the individual rule results.
	Results []RuleResult `json:"results"`

	// Blocked indicates if any BLOCK rule matched.
	Blocked bool `json:"blocked"`

	// BlockReason explains why it's blocked.
	BlockReason string `json:"block_reason,omitempty"`

	// RequiresReview indicates if review is required.
	RequiresReview bool `json:"requires_review"`

	// Warnings are warning messages.
	Warnings []string `json:"warnings,omitempty"`

	// Notifications are notification messages.
	Notifications []string `json:"notifications,omitempty"`
}

// NewRuleEngine creates a new rule engine.
func NewRuleEngine() *RuleEngine {
	return &RuleEngine{
		rules: make([]Rule, 0),
	}
}

// LoadRules loads rules from a YAML file.
//
// # Inputs
//
//   - path: Path to the rules file (e.g., .aleutian/rules.yml).
//
// # Outputs
//
//   - error: Non-nil if loading failed.
func (e *RuleEngine) LoadRules(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read rules file: %w", err)
	}

	rules, err := parseRulesYAML(string(data))
	if err != nil {
		return fmt.Errorf("parse rules: %w", err)
	}

	e.mu.Lock()
	e.rules = rules
	e.mu.Unlock()

	return nil
}

// parseRulesYAML parses rules from YAML content.
// This is a simplified YAML parser to avoid external dependencies.
func parseRulesYAML(content string) ([]Rule, error) {
	var rules []Rule
	var currentRule *Rule

	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		// Skip comments and empty lines
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		// Detect rule start
		if strings.HasPrefix(line, "- name:") || strings.HasPrefix(line, "  - name:") {
			if currentRule != nil {
				rules = append(rules, *currentRule)
			}
			currentRule = &Rule{
				Name:    parseYAMLValue(line, "name"),
				Enabled: true, // Default to enabled
			}
			continue
		}

		if currentRule == nil {
			continue
		}

		// Parse rule fields
		if strings.Contains(line, "description:") {
			currentRule.Description = parseYAMLValue(line, "description")
		} else if strings.Contains(line, "enabled:") {
			currentRule.Enabled = parseYAMLValue(line, "enabled") == "true"
		} else if strings.Contains(line, "action:") {
			currentRule.Action = Action(parseYAMLValue(line, "action"))
		} else if strings.Contains(line, "severity:") {
			currentRule.Severity = parseYAMLValue(line, "severity")
		} else if strings.Contains(line, "message:") {
			currentRule.Message = parseYAMLValue(line, "message")
		} else if strings.Contains(line, "caller_count_gt:") {
			val, _ := strconv.Atoi(parseYAMLValue(line, "caller_count_gt"))
			currentRule.Condition.CallerCountGT = &val
		} else if strings.Contains(line, "caller_count_gte:") {
			val, _ := strconv.Atoi(parseYAMLValue(line, "caller_count_gte"))
			currentRule.Condition.CallerCountGTE = &val
		} else if strings.Contains(line, "package_match:") {
			val := parseYAMLValue(line, "package_match")
			currentRule.Condition.PackageMatch = &val
		} else if strings.Contains(line, "symbol_match:") {
			val := parseYAMLValue(line, "symbol_match")
			currentRule.Condition.SymbolMatch = &val
		} else if strings.Contains(line, "transitive_count_gt:") {
			val, _ := strconv.Atoi(parseYAMLValue(line, "transitive_count_gt"))
			currentRule.Condition.TransitiveCountGT = &val
		} else if strings.Contains(line, "confidence_lt:") {
			val, _ := strconv.Atoi(parseYAMLValue(line, "confidence_lt"))
			currentRule.Condition.ConfidenceLT = &val
		} else if strings.Contains(line, "security_path_in:") {
			// Handle list inline: [AUTH, AUTHZ]
			val := parseYAMLValue(line, "security_path_in")
			currentRule.Condition.SecurityPathIn = parseYAMLList(val)
		} else if strings.Contains(line, "churn_level_in:") {
			val := parseYAMLValue(line, "churn_level_in")
			currentRule.Condition.ChurnLevelIn = parseYAMLList(val)
		} else if strings.Contains(line, "risk_level_in:") {
			val := parseYAMLValue(line, "risk_level_in")
			currentRule.Condition.RiskLevelIn = parseYAMLList(val)
		} else if strings.Contains(line, "coverage_risk_in:") {
			val := parseYAMLValue(line, "coverage_risk_in")
			currentRule.Condition.CoverageRiskIn = parseYAMLList(val)
		}
	}

	// Don't forget the last rule
	if currentRule != nil {
		rules = append(rules, *currentRule)
	}

	return rules, scanner.Err()
}

// parseYAMLValue extracts a value from a YAML line.
func parseYAMLValue(line, key string) string {
	idx := strings.Index(line, key+":")
	if idx < 0 {
		return ""
	}
	rest := strings.TrimSpace(line[idx+len(key)+1:])
	// Remove quotes
	rest = strings.Trim(rest, "\"'")
	return rest
}

// parseYAMLList parses a YAML inline list [a, b, c].
func parseYAMLList(val string) []string {
	val = strings.Trim(val, "[]")
	if val == "" {
		return nil
	}
	parts := strings.Split(val, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		p = strings.Trim(p, "\"'")
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

// AddRule adds a rule programmatically.
func (e *RuleEngine) AddRule(rule Rule) {
	e.mu.Lock()
	e.rules = append(e.rules, rule)
	e.mu.Unlock()
}

// Rules returns all loaded rules.
func (e *RuleEngine) Rules() []Rule {
	e.mu.RLock()
	defer e.mu.RUnlock()

	result := make([]Rule, len(e.rules))
	copy(result, e.rules)
	return result
}

// Evaluate evaluates all rules against a blast radius result.
//
// # Inputs
//
//   - ctx: Context for cancellation.
//   - br: The blast radius result to evaluate.
//
// # Outputs
//
//   - *EvaluationResult: The evaluation result.
//   - error: Non-nil on failure.
func (e *RuleEngine) Evaluate(ctx context.Context, br *analysis.EnhancedBlastRadius) (*EvaluationResult, error) {
	if ctx == nil {
		return nil, fmt.Errorf("context is required")
	}
	if br == nil {
		return nil, fmt.Errorf("blast radius is required")
	}

	e.mu.RLock()
	rules := make([]Rule, len(e.rules))
	copy(rules, e.rules)
	e.mu.RUnlock()

	result := &EvaluationResult{
		Results:       make([]RuleResult, 0, len(rules)),
		Warnings:      make([]string, 0),
		Notifications: make([]string, 0),
	}

	for _, rule := range rules {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		if !rule.Enabled {
			continue
		}

		rr := e.evaluateRule(&rule, br)
		result.Results = append(result.Results, rr)

		if rr.Matched {
			switch rr.Action {
			case ActionBlock:
				result.Blocked = true
				if result.BlockReason == "" {
					result.BlockReason = rr.Message
				}
			case ActionRequireReview:
				result.RequiresReview = true
			case ActionWarn:
				result.Warnings = append(result.Warnings, rr.Message)
			case ActionNotify:
				result.Notifications = append(result.Notifications, rr.Message)
			}
		}
	}

	return result, nil
}

// evaluateRule evaluates a single rule.
func (e *RuleEngine) evaluateRule(rule *Rule, br *analysis.EnhancedBlastRadius) RuleResult {
	rr := RuleResult{
		Rule:   rule,
		Action: rule.Action,
	}

	// Check exemptions first
	if e.isExempt(rule, br) {
		rr.Matched = false
		rr.Message = "Symbol is exempt from this rule"
		return rr
	}

	// Evaluate condition
	matched, details := e.evaluateCondition(&rule.Condition, br)
	rr.Matched = matched
	rr.MatchDetails = details

	if matched {
		if rule.Message != "" {
			rr.Message = rule.Message
		} else {
			rr.Message = fmt.Sprintf("Rule '%s' matched: %s", rule.Name, strings.Join(details, ", "))
		}
	}

	return rr
}

// isExempt checks if a symbol is exempt from a rule.
func (e *RuleEngine) isExempt(rule *Rule, br *analysis.EnhancedBlastRadius) bool {
	for _, pattern := range rule.Exemptions {
		if matched, _ := regexp.MatchString(pattern, br.Target); matched {
			return true
		}
	}
	return false
}

// evaluateCondition evaluates a condition against a blast radius.
func (e *RuleEngine) evaluateCondition(cond *Condition, br *analysis.EnhancedBlastRadius) (bool, []string) {
	var details []string

	// Handle composite conditions first
	if len(cond.And) > 0 {
		for _, c := range cond.And {
			matched, _ := e.evaluateCondition(&c, br)
			if !matched {
				return false, nil
			}
		}
		return true, []string{"all AND conditions matched"}
	}

	if len(cond.Or) > 0 {
		for _, c := range cond.Or {
			matched, d := e.evaluateCondition(&c, br)
			if matched {
				return true, d
			}
		}
		return false, nil
	}

	if cond.Not != nil {
		matched, _ := e.evaluateCondition(cond.Not, br)
		return !matched, []string{"NOT condition"}
	}

	// Evaluate simple conditions
	anyMatched := false

	// Caller count GT
	if cond.CallerCountGT != nil {
		if len(br.DirectCallers) > *cond.CallerCountGT {
			details = append(details, fmt.Sprintf("caller count %d > %d", len(br.DirectCallers), *cond.CallerCountGT))
			anyMatched = true
		}
	}

	// Caller count GTE
	if cond.CallerCountGTE != nil {
		if len(br.DirectCallers) >= *cond.CallerCountGTE {
			details = append(details, fmt.Sprintf("caller count %d >= %d", len(br.DirectCallers), *cond.CallerCountGTE))
			anyMatched = true
		}
	}

	// Security path in
	if len(cond.SecurityPathIn) > 0 {
		for _, sp := range br.SecurityPaths {
			for _, allowed := range cond.SecurityPathIn {
				if sp.PathType == allowed {
					details = append(details, fmt.Sprintf("security path %s", sp.PathType))
					anyMatched = true
				}
			}
		}
	}

	// Package match
	if cond.PackageMatch != nil {
		if matched, _ := regexp.MatchString(*cond.PackageMatch, br.Target); matched {
			details = append(details, fmt.Sprintf("package matches %s", *cond.PackageMatch))
			anyMatched = true
		}
	}

	// Symbol match
	if cond.SymbolMatch != nil {
		if matched, _ := regexp.MatchString(*cond.SymbolMatch, br.Target); matched {
			details = append(details, fmt.Sprintf("symbol matches %s", *cond.SymbolMatch))
			anyMatched = true
		}
	}

	// Churn level in
	if len(cond.ChurnLevelIn) > 0 {
		for _, churn := range br.ChurnScores {
			for _, allowed := range cond.ChurnLevelIn {
				if churn.ChurnLevel == allowed {
					details = append(details, fmt.Sprintf("churn level %s", churn.ChurnLevel))
					anyMatched = true
				}
			}
		}
	}

	// Transitive count GT
	if cond.TransitiveCountGT != nil {
		if br.TransitiveCount > *cond.TransitiveCountGT {
			details = append(details, fmt.Sprintf("transitive count %d > %d", br.TransitiveCount, *cond.TransitiveCountGT))
			anyMatched = true
		}
	}

	// Coverage risk in
	if len(cond.CoverageRiskIn) > 0 {
		for _, allowed := range cond.CoverageRiskIn {
			if br.CoverageRisk == allowed {
				details = append(details, fmt.Sprintf("coverage risk %s", br.CoverageRisk))
				anyMatched = true
			}
		}
	}

	// Confidence LT
	if cond.ConfidenceLT != nil && br.Confidence != nil {
		if br.Confidence.Score < *cond.ConfidenceLT {
			details = append(details, fmt.Sprintf("confidence %d < %d", br.Confidence.Score, *cond.ConfidenceLT))
			anyMatched = true
		}
	}

	return anyMatched, details
}

// LoadDefaultRules loads sensible default rules.
func (e *RuleEngine) LoadDefaultRules() {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.rules = []Rule{
		{
			Name:        "high-caller-count",
			Description: "Block changes to symbols with many callers",
			Enabled:     true,
			Condition: Condition{
				CallerCountGT: intPtr(50),
			},
			Action:   ActionBlock,
			Severity: "HIGH",
			Message:  "Symbol has more than 50 callers - requires additional review",
		},
		{
			Name:        "security-path-review",
			Description: "Require review for security-sensitive changes",
			Enabled:     true,
			Condition: Condition{
				SecurityPathIn: []string{"AUTH", "AUTHZ", "CRYPTO", "SECRETS"},
			},
			Action:   ActionRequireReview,
			Severity: "HIGH",
			Message:  "Change affects security-sensitive code path",
		},
		{
			Name:        "high-churn-warning",
			Description: "Warn about changes to high-churn areas",
			Enabled:     true,
			Condition: Condition{
				ChurnLevelIn: []string{"HIGH", "VERY_HIGH"},
			},
			Action:   ActionWarn,
			Severity: "MEDIUM",
			Message:  "This code area has high churn - consider stabilization",
		},
		{
			Name:        "low-coverage-warning",
			Description: "Warn about changes to poorly tested code",
			Enabled:     true,
			Condition: Condition{
				CoverageRiskIn: []string{"HIGH_UNTESTED"},
			},
			Action:   ActionWarn,
			Severity: "MEDIUM",
			Message:  "This change affects poorly tested code - consider adding tests",
		},
	}
}

// intPtr returns a pointer to an int.
func intPtr(i int) *int {
	return &i
}

// SaveRules saves rules to a YAML file.
func (e *RuleEngine) SaveRules(path string) error {
	e.mu.RLock()
	rules := make([]Rule, len(e.rules))
	copy(rules, e.rules)
	e.mu.RUnlock()

	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}

	var sb strings.Builder
	sb.WriteString("# Aleutian Blast Radius Rules\n")
	sb.WriteString("# See docs for rule configuration options\n\n")
	sb.WriteString("rules:\n")

	for _, rule := range rules {
		sb.WriteString(fmt.Sprintf("  - name: %s\n", rule.Name))
		if rule.Description != "" {
			sb.WriteString(fmt.Sprintf("    description: \"%s\"\n", rule.Description))
		}
		sb.WriteString(fmt.Sprintf("    enabled: %t\n", rule.Enabled))
		sb.WriteString(fmt.Sprintf("    action: %s\n", rule.Action))
		if rule.Severity != "" {
			sb.WriteString(fmt.Sprintf("    severity: %s\n", rule.Severity))
		}
		if rule.Message != "" {
			sb.WriteString(fmt.Sprintf("    message: \"%s\"\n", rule.Message))
		}
		sb.WriteString("    condition:\n")
		if rule.Condition.CallerCountGT != nil {
			sb.WriteString(fmt.Sprintf("      caller_count_gt: %d\n", *rule.Condition.CallerCountGT))
		}
		if len(rule.Condition.SecurityPathIn) > 0 {
			sb.WriteString(fmt.Sprintf("      security_path_in: [%s]\n", strings.Join(rule.Condition.SecurityPathIn, ", ")))
		}
		if len(rule.Condition.ChurnLevelIn) > 0 {
			sb.WriteString(fmt.Sprintf("      churn_level_in: [%s]\n", strings.Join(rule.Condition.ChurnLevelIn, ", ")))
		}
		if len(rule.Condition.CoverageRiskIn) > 0 {
			sb.WriteString(fmt.Sprintf("      coverage_risk_in: [%s]\n", strings.Join(rule.Condition.CoverageRiskIn, ", ")))
		}
		sb.WriteString("\n")
	}

	return os.WriteFile(path, []byte(sb.String()), 0644)
}
