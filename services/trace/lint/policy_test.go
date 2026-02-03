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
	"testing"
)

func TestRulePolicy_ShouldBlock(t *testing.T) {
	policy := &RulePolicy{
		BlockOn: []string{"errcheck", "staticcheck", "SA"},
	}

	tests := []struct {
		rule string
		want bool
	}{
		{"errcheck", true},
		{"staticcheck", true},
		{"SA1000", true},  // Prefix match SA
		{"SA/1000", true}, // Prefix match SA/
		{"ineffassign", false},
		{"unused", false},
	}

	for _, tt := range tests {
		got := policy.ShouldBlock(tt.rule)
		if got != tt.want {
			t.Errorf("ShouldBlock(%q) = %v, want %v", tt.rule, got, tt.want)
		}
	}
}

func TestRulePolicy_ShouldWarn(t *testing.T) {
	policy := &RulePolicy{
		WarnOn: []string{"ineffassign", "unused"},
	}

	tests := []struct {
		rule string
		want bool
	}{
		{"ineffassign", true},
		{"unused", true},
		{"errcheck", false},
	}

	for _, tt := range tests {
		got := policy.ShouldWarn(tt.rule)
		if got != tt.want {
			t.Errorf("ShouldWarn(%q) = %v, want %v", tt.rule, got, tt.want)
		}
	}
}

func TestRulePolicy_ShouldIgnore(t *testing.T) {
	policy := &RulePolicy{
		Ignore: []string{"lll", "gofmt"},
	}

	tests := []struct {
		rule string
		want bool
	}{
		{"lll", true},
		{"gofmt", true},
		{"errcheck", false},
	}

	for _, tt := range tests {
		got := policy.ShouldIgnore(tt.rule)
		if got != tt.want {
			t.Errorf("ShouldIgnore(%q) = %v, want %v", tt.rule, got, tt.want)
		}
	}
}

func TestRulePolicy_GetSeverity(t *testing.T) {
	policy := &RulePolicy{
		BlockOn: []string{"errcheck"},
		WarnOn:  []string{"ineffassign"},
		Ignore:  []string{"lll"},
	}

	tests := []struct {
		rule string
		want Severity
	}{
		{"errcheck", SeverityError},
		{"ineffassign", SeverityWarning},
		{"lll", SeverityInfo},        // Ignored
		{"unknown", SeverityWarning}, // Default
	}

	for _, tt := range tests {
		got := policy.GetSeverity(tt.rule)
		if got != tt.want {
			t.Errorf("GetSeverity(%q) = %v, want %v", tt.rule, got, tt.want)
		}
	}
}

func TestRulePolicy_CaseInsensitive(t *testing.T) {
	policy := &RulePolicy{
		BlockOn: []string{"ErrCheck"},
	}

	// Should match regardless of case
	if !policy.ShouldBlock("errcheck") {
		t.Error("ShouldBlock should be case-insensitive")
	}
	if !policy.ShouldBlock("ERRCHECK") {
		t.Error("ShouldBlock should be case-insensitive")
	}
}

func TestPolicyRegistry_Get(t *testing.T) {
	r := NewPolicyRegistry()

	// Default policies should exist
	goPolicy := r.Get("go")
	if goPolicy == nil {
		t.Error("Expected Go policy to be registered by default")
	}

	pythonPolicy := r.Get("python")
	if pythonPolicy == nil {
		t.Error("Expected Python policy to be registered by default")
	}

	tsPolicy := r.Get("typescript")
	if tsPolicy == nil {
		t.Error("Expected TypeScript policy to be registered by default")
	}

	// Unknown language returns nil
	if r.Get("unknown") != nil {
		t.Error("Expected nil for unknown language")
	}
}

func TestPolicyRegistry_Register(t *testing.T) {
	r := NewPolicyRegistry()

	customPolicy := &RulePolicy{
		BlockOn: []string{"custom-error"},
	}

	r.Register("custom", customPolicy)

	got := r.Get("custom")
	if got == nil {
		t.Error("Custom policy not registered")
	}

	if !got.ShouldBlock("custom-error") {
		t.Error("Custom policy not working")
	}
}

func TestPolicyRegistry_Languages(t *testing.T) {
	r := NewPolicyRegistry()

	langs := r.Languages()
	if len(langs) < 3 {
		t.Errorf("Expected at least 3 languages, got %d", len(langs))
	}

	// Check that expected languages are present
	found := make(map[string]bool)
	for _, lang := range langs {
		found[lang] = true
	}

	for _, expected := range []string{"go", "python", "typescript"} {
		if !found[expected] {
			t.Errorf("Expected %q in Languages()", expected)
		}
	}
}

func TestApplyPolicy(t *testing.T) {
	policy := &RulePolicy{
		BlockOn: []string{"errcheck"},
		WarnOn:  []string{"unused"},
		Ignore:  []string{"lll"},
	}

	issues := []LintIssue{
		{Rule: "errcheck", Message: "error not checked"},
		{Rule: "unused", Message: "unused var"},
		{Rule: "lll", Message: "line too long"},
		{Rule: "unknown", Message: "unknown rule"},
	}

	errors, warnings, infos := ApplyPolicy(issues, policy)

	// errcheck should be error
	if len(errors) != 1 || errors[0].Rule != "errcheck" {
		t.Errorf("Expected 1 error (errcheck), got %d", len(errors))
	}

	// unused and unknown should be warnings
	if len(warnings) != 2 {
		t.Errorf("Expected 2 warnings, got %d", len(warnings))
	}

	// lll is ignored (not in any list)
	if len(infos) != 0 {
		t.Errorf("Expected 0 infos (lll ignored), got %d", len(infos))
	}
}

func TestApplyPolicy_NilPolicy(t *testing.T) {
	issues := []LintIssue{
		{Rule: "errcheck"},
		{Rule: "unused"},
	}

	errors, warnings, _ := ApplyPolicy(issues, nil)

	// With nil policy, all issues become warnings
	if len(errors) != 0 {
		t.Errorf("Expected 0 errors with nil policy, got %d", len(errors))
	}
	if len(warnings) != 2 {
		t.Errorf("Expected 2 warnings with nil policy, got %d", len(warnings))
	}
}

func TestDefaultGoPolicy(t *testing.T) {
	// Verify critical rules are blocked
	if !DefaultGoPolicy.ShouldBlock("errcheck") {
		t.Error("errcheck should block")
	}
	if !DefaultGoPolicy.ShouldBlock("staticcheck") {
		t.Error("staticcheck should block")
	}
	if !DefaultGoPolicy.ShouldBlock("gosec") {
		t.Error("gosec should block")
	}

	// Verify style rules are ignored
	if !DefaultGoPolicy.ShouldIgnore("lll") {
		t.Error("lll should be ignored")
	}
	if !DefaultGoPolicy.ShouldIgnore("gofmt") {
		t.Error("gofmt should be ignored")
	}
}

func TestDefaultPythonPolicy(t *testing.T) {
	// Pyflakes errors should block
	if !DefaultPythonPolicy.ShouldBlock("F401") {
		t.Error("F401 should block")
	}

	// Security rules should block
	if !DefaultPythonPolicy.ShouldBlock("S101") {
		t.Error("S101 should block")
	}

	// E501 (line length) should be ignored
	if !DefaultPythonPolicy.ShouldIgnore("E501") {
		t.Error("E501 should be ignored")
	}
}
