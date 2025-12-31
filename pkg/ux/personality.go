// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.

package ux

import (
	"os"
	"strings"
	"sync"
)

// PersonalityLevel defines the verbosity and richness of CLI output
type PersonalityLevel string

const (
	// PersonalityFull enables all visual flourishes, nautical theming, and rich formatting
	PersonalityFull PersonalityLevel = "full"

	// PersonalityStandard enables colors, icons, and boxes but minimal theming
	PersonalityStandard PersonalityLevel = "standard"

	// PersonalityMinimal uses icons and basic formatting only
	PersonalityMinimal PersonalityLevel = "minimal"

	// PersonalityMachine outputs plain text suitable for scripting and parsing
	PersonalityMachine PersonalityLevel = "machine"
)

// Personality holds the current UX personality configuration
type Personality struct {
	// Level controls overall verbosity (full, standard, minimal, machine)
	Level PersonalityLevel

	// Theme is the color theme (reserved for future use)
	Theme string

	// ShowTips enables "message in a bottle" style tips
	ShowTips bool

	// NauticalMode enables nautical terminology throughout
	NauticalMode bool
}

var (
	currentPersonality = Personality{
		Level:        PersonalityFull,
		Theme:        "default",
		ShowTips:     true,
		NauticalMode: true,
	}
	personalityMu sync.RWMutex
)

// GetPersonality returns the current personality settings
func GetPersonality() Personality {
	personalityMu.RLock()
	defer personalityMu.RUnlock()
	return currentPersonality
}

// SetPersonality updates the current personality settings
func SetPersonality(p Personality) {
	personalityMu.Lock()
	defer personalityMu.Unlock()
	currentPersonality = p
}

// SetPersonalityLevel updates just the personality level
func SetPersonalityLevel(level PersonalityLevel) {
	personalityMu.Lock()
	defer personalityMu.Unlock()
	currentPersonality.Level = level
}

// ParsePersonalityLevel converts a string to PersonalityLevel
func ParsePersonalityLevel(s string) PersonalityLevel {
	switch strings.ToLower(s) {
	case "full", "f":
		return PersonalityFull
	case "standard", "std", "s":
		return PersonalityStandard
	case "minimal", "min", "m":
		return PersonalityMinimal
	case "machine", "quiet", "q":
		return PersonalityMachine
	default:
		return PersonalityStandard
	}
}

// InitPersonality initializes personality from environment and defaults
func InitPersonality() {
	// Check environment variable first
	if envLevel := os.Getenv("ALEUTIAN_PERSONALITY"); envLevel != "" {
		SetPersonalityLevel(ParsePersonalityLevel(envLevel))
		return
	}

	// Check if we're in a non-interactive context
	if !isTerminal() {
		SetPersonalityLevel(PersonalityMachine)
		return
	}

	// Default to full (all nautical flourishes)
	SetPersonalityLevel(PersonalityFull)
}

// isTerminal checks if stdout is a terminal
func isTerminal() bool {
	fileInfo, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return (fileInfo.Mode() & os.ModeCharDevice) != 0
}

// IsInteractive returns true if we should show interactive prompts
func IsInteractive() bool {
	p := GetPersonality()
	return p.Level != PersonalityMachine && isTerminal()
}

// ShouldShowProgress returns true if we should show progress indicators
func ShouldShowProgress() bool {
	p := GetPersonality()
	return p.Level != PersonalityMachine
}

// ShouldShowColors returns true if we should use colors
func ShouldShowColors() bool {
	p := GetPersonality()
	return p.Level != PersonalityMachine
}

// DefaultPersonality returns the default personality settings
func DefaultPersonality() Personality {
	return Personality{
		Level:        PersonalityFull,
		Theme:        "default",
		ShowTips:     true,
		NauticalMode: true,
	}
}
