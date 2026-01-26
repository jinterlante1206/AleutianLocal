// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package ux

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/huh"
)

// PromptOption represents a selectable option in a prompt
type PromptOption struct {
	Label       string
	Description string
	Value       string
	Recommended bool
}

// AskChoice presents an interactive selection prompt and returns the selected value
func AskChoice(question string, options []PromptOption) (string, error) {
	p := GetPersonality()

	// Machine mode: use simple numbered selection
	if p.Level == PersonalityMachine {
		return askChoicePlain(question, options)
	}

	var selected string

	opts := make([]huh.Option[string], len(options))
	for i, opt := range options {
		label := opt.Label
		if opt.Recommended {
			label += " (recommended)"
		}
		opts[i] = huh.NewOption(label, opt.Value)
	}

	selectPrompt := huh.NewSelect[string]().
		Title(question).
		Options(opts...).
		Value(&selected)

	// Apply Aleutian theme
	form := huh.NewForm(huh.NewGroup(selectPrompt)).
		WithTheme(aleutianTheme())

	if err := form.Run(); err != nil {
		return "", err
	}

	return selected, nil
}

// AskConfirm presents a yes/no confirmation prompt
func AskConfirm(question string, defaultYes bool) (bool, error) {
	p := GetPersonality()

	// Machine mode: simple y/n
	if p.Level == PersonalityMachine {
		return askConfirmPlain(question, defaultYes)
	}

	var confirmed bool

	confirm := huh.NewConfirm().
		Title(question).
		Affirmative("Yes").
		Negative("No").
		Value(&confirmed)

	form := huh.NewForm(huh.NewGroup(confirm)).
		WithTheme(aleutianTheme())

	if err := form.Run(); err != nil {
		return false, err
	}

	return confirmed, nil
}

// AskInput prompts for text input
func AskInput(question string, placeholder string) (string, error) {
	p := GetPersonality()

	// Machine mode: simple stdin read
	if p.Level == PersonalityMachine {
		return askInputPlain(question)
	}

	var value string

	input := huh.NewInput().
		Title(question).
		Placeholder(placeholder).
		Value(&value)

	form := huh.NewForm(huh.NewGroup(input)).
		WithTheme(aleutianTheme())

	if err := form.Run(); err != nil {
		return "", err
	}

	return value, nil
}

// SecretPromptOptions holds options for the secret detection prompt
type SecretPromptOptions struct {
	FilePath      string
	Findings      []SecretFinding
	ShowRedact    bool
	ShowForceSkip bool
}

// SecretFinding represents a detected secret in a file
type SecretFinding struct {
	LineNumber  int
	PatternID   string
	PatternName string
	Confidence  string
	Match       string
	Reason      string
}

// SecretAction is the user's decision about a file with secrets
type SecretAction string

const (
	SecretActionSkip     SecretAction = "skip"
	SecretActionRedact   SecretAction = "redact"
	SecretActionProceed  SecretAction = "proceed"
	SecretActionShowMore SecretAction = "show"
)

// AskSecretAction presents the bidirectional prompt for files with detected secrets
func AskSecretAction(opts SecretPromptOptions) (SecretAction, error) {
	p := GetPersonality()

	// Build the findings display
	var findingsText strings.Builder
	for _, f := range opts.Findings {
		findingsText.WriteString(fmt.Sprintf("  [L%d] %s | %s\n", f.LineNumber, f.Confidence, f.PatternName))
		findingsText.WriteString(fmt.Sprintf("        %s\n", Styles.Muted.Render(f.Reason)))
		findingsText.WriteString(fmt.Sprintf("        Match: '%s'\n\n", truncate(f.Match, 50)))
	}

	// Machine mode: simple text prompt
	if p.Level == PersonalityMachine {
		fmt.Printf("SECRETS_FOUND: file=%s count=%d\n", opts.FilePath, len(opts.Findings))
		for _, f := range opts.Findings {
			fmt.Printf("  LINE=%d PATTERN=%s CONFIDENCE=%s\n", f.LineNumber, f.PatternID, f.Confidence)
		}
		return askSecretActionPlain()
	}

	// Build options
	promptOpts := []PromptOption{
		{
			Label:       "Skip this file",
			Description: "Don't ingest this file",
			Value:       string(SecretActionSkip),
			Recommended: true,
		},
	}

	if opts.ShowRedact {
		promptOpts = append(promptOpts, PromptOption{
			Label:       "Redact patterns and continue",
			Description: "Replace detected patterns with [REDACTED]",
			Value:       string(SecretActionRedact),
		})
	}

	promptOpts = append(promptOpts, PromptOption{
		Label:       "Proceed anyway",
		Description: "I understand the risk",
		Value:       string(SecretActionProceed),
	})

	// Print the warning box first
	fmt.Println()
	WarningBox(
		fmt.Sprintf("Found %d potential secret(s) in %s", len(opts.Findings), opts.FilePath),
		findingsText.String(),
	)

	result, err := AskChoice("What would you like to do?", promptOpts)
	if err != nil {
		return SecretActionSkip, err
	}

	return SecretAction(result), nil
}

// Plain text fallbacks for machine mode

func askChoicePlain(question string, options []PromptOption) (string, error) {
	fmt.Printf("%s\n", question)
	for i, opt := range options {
		rec := ""
		if opt.Recommended {
			rec = " [recommended]"
		}
		fmt.Printf("  %d) %s%s\n", i+1, opt.Label, rec)
	}
	fmt.Print("Enter number: ")

	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		return options[0].Value, err
	}

	input = strings.TrimSpace(input)
	for i, opt := range options {
		if input == fmt.Sprintf("%d", i+1) {
			return opt.Value, nil
		}
	}

	// Default to first option
	return options[0].Value, nil
}

func askConfirmPlain(question string, defaultYes bool) (bool, error) {
	def := "y/N"
	if defaultYes {
		def = "Y/n"
	}
	fmt.Printf("%s [%s]: ", question, def)

	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		return defaultYes, err
	}

	input = strings.ToLower(strings.TrimSpace(input))
	if input == "" {
		return defaultYes, nil
	}
	return input == "y" || input == "yes", nil
}

func askInputPlain(question string) (string, error) {
	fmt.Printf("%s: ", question)
	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(input), nil
}

func askSecretActionPlain() (SecretAction, error) {
	fmt.Print("Action (skip/proceed): ")
	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		return SecretActionSkip, err
	}

	input = strings.ToLower(strings.TrimSpace(input))
	switch input {
	case "proceed", "p", "yes", "y":
		return SecretActionProceed, nil
	case "redact", "r":
		return SecretActionRedact, nil
	default:
		return SecretActionSkip, nil
	}
}

// aleutianTheme returns a huh theme using the Aleutian color palette
func aleutianTheme() *huh.Theme {
	t := huh.ThemeBase()

	// Customize with Aleutian colors
	t.Focused.Title = t.Focused.Title.Foreground(ColorTealBright)
	t.Focused.SelectSelector = t.Focused.SelectSelector.Foreground(ColorTealPrimary)
	t.Focused.SelectedOption = t.Focused.SelectedOption.Foreground(ColorTealBright)
	t.Focused.UnselectedOption = t.Focused.UnselectedOption.Foreground(ColorSlate)

	t.Blurred.Title = t.Blurred.Title.Foreground(ColorSlate)

	return t
}

// truncate shortens a string to maxLen with ellipsis
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
