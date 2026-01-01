// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.

// Package ux provides rich terminal output styling for the Aleutian CLI.
package ux

import (
	"fmt"
	"os"

	"github.com/charmbracelet/lipgloss"
)

// Aleutian color palette - deep ocean teals and arctic waters
var (
	// Primary palette (brightest to darkest)
	ColorTealBright  = lipgloss.Color("#2CD7C7") // Bright teal - highlights, success
	ColorTealPrimary = lipgloss.Color("#20B9B4") // Primary teal - main brand color
	ColorTealVibrant = lipgloss.Color("#1D9EA3") // Vibrant teal - interactive elements
	ColorTealMedium  = lipgloss.Color("#1D9DA0") // Medium teal - secondary elements
	ColorTealDeep    = lipgloss.Color("#16858E") // Deep teal - borders, accents
	ColorTealOcean   = lipgloss.Color("#157483") // Ocean teal - subtle accents

	// Dark palette (for backgrounds, muted elements)
	ColorDeepSea  = lipgloss.Color("#104855") // Deep sea blue
	ColorAbyss    = lipgloss.Color("#0C424E") // Abyss - darker backgrounds
	ColorMidnight = lipgloss.Color("#0D2F39") // Midnight - deep backgrounds
	ColorSlate    = lipgloss.Color("#2C4A54") // Slate - muted text, borders
	ColorDarkest  = lipgloss.Color("#0F1923") // Darkest - near black

	// Semantic colors (keeping standard conventions for clarity)
	ColorSuccess = lipgloss.Color("#2CD7C7") // Bright teal for success
	ColorWarning = lipgloss.Color("#F4D03F") // Gold/amber for warnings
	ColorError   = lipgloss.Color("#E74C3C") // Red for errors
	ColorMuted   = lipgloss.Color("#2C4A54") // Slate for muted text
)

// Styles provides pre-configured lipgloss styles
var Styles = struct {
	// Text styles
	Title     lipgloss.Style
	Subtitle  lipgloss.Style
	Bold      lipgloss.Style
	Muted     lipgloss.Style
	Success   lipgloss.Style
	Warning   lipgloss.Style
	Error     lipgloss.Style
	Highlight lipgloss.Style

	// Box styles
	Box        lipgloss.Style
	InfoBox    lipgloss.Style
	WarningBox lipgloss.Style
	ErrorBox   lipgloss.Style

	// Status indicators
	StatusOK      lipgloss.Style
	StatusWarning lipgloss.Style
	StatusError   lipgloss.Style
	StatusPending lipgloss.Style
}{
	// Text styles
	Title:     lipgloss.NewStyle().Bold(true).Foreground(ColorTealBright),
	Subtitle:  lipgloss.NewStyle().Foreground(ColorTealPrimary),
	Bold:      lipgloss.NewStyle().Bold(true),
	Muted:     lipgloss.NewStyle().Foreground(ColorSlate),
	Success:   lipgloss.NewStyle().Foreground(ColorSuccess),
	Warning:   lipgloss.NewStyle().Foreground(ColorWarning),
	Error:     lipgloss.NewStyle().Foreground(ColorError),
	Highlight: lipgloss.NewStyle().Foreground(ColorTealBright).Bold(true),

	// Box styles
	Box: lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorTealDeep).
		Padding(0, 1),
	InfoBox: lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorTealPrimary).
		Padding(0, 1),
	WarningBox: lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorWarning).
		Padding(0, 1),
	ErrorBox: lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorError).
		Padding(0, 1),

	// Status indicators
	StatusOK:      lipgloss.NewStyle().SetString("✓").Foreground(ColorSuccess),
	StatusWarning: lipgloss.NewStyle().SetString("⚠").Foreground(ColorWarning),
	StatusError:   lipgloss.NewStyle().SetString("✗").Foreground(ColorError),
	StatusPending: lipgloss.NewStyle().SetString("○").Foreground(ColorSlate),
}

// Icon provides themed status icons
type Icon string

const (
	IconSuccess Icon = "✓"
	IconWarning Icon = "⚠"
	IconError   Icon = "✗"
	IconPending Icon = "○"
	IconArrow   Icon = "→"
	IconBullet  Icon = "•"
	IconAnchor  Icon = "⚓"
	IconShip    Icon = "⛵"
	IconWave    Icon = "〰"
)

// Render returns the icon with appropriate styling
func (i Icon) Render() string {
	switch i {
	case IconSuccess:
		return Styles.Success.Render(string(i))
	case IconWarning:
		return Styles.Warning.Render(string(i))
	case IconError:
		return Styles.Error.Render(string(i))
	case IconPending:
		return Styles.Muted.Render(string(i))
	default:
		return string(i)
	}
}

// Print helpers that respect personality level

// Title prints a styled title
func Title(text string) {
	if GetPersonality().Level == PersonalityMachine {
		return
	}
	fmt.Println(Styles.Title.Render(text))
}

// Success prints a success message with checkmark
func Success(text string) {
	p := GetPersonality()
	switch p.Level {
	case PersonalityMachine:
		fmt.Fprintf(os.Stdout, "OK: %s\n", text)
	case PersonalityMinimal:
		fmt.Printf("%s %s\n", IconSuccess.Render(), text)
	default:
		fmt.Printf("%s %s\n", IconSuccess.Render(), Styles.Success.Render(text))
	}
}

// Warning prints a warning message
func Warning(text string) {
	p := GetPersonality()
	switch p.Level {
	case PersonalityMachine:
		fmt.Fprintf(os.Stderr, "WARN: %s\n", text)
	case PersonalityMinimal:
		fmt.Printf("%s %s\n", IconWarning.Render(), text)
	default:
		fmt.Printf("%s %s\n", IconWarning.Render(), Styles.Warning.Render(text))
	}
}

// Error prints an error message
func Error(text string) {
	p := GetPersonality()
	switch p.Level {
	case PersonalityMachine:
		fmt.Fprintf(os.Stderr, "ERROR: %s\n", text)
	case PersonalityMinimal:
		fmt.Printf("%s %s\n", IconError.Render(), text)
	default:
		fmt.Printf("%s %s\n", IconError.Render(), Styles.Error.Render(text))
	}
}

// Info prints an informational message
func Info(text string) {
	p := GetPersonality()
	switch p.Level {
	case PersonalityMachine:
		fmt.Println(text)
	default:
		fmt.Printf("%s %s\n", Styles.Muted.Render("│"), text)
	}
}

// Muted prints muted/secondary text
func Muted(text string) {
	if GetPersonality().Level == PersonalityMachine {
		return
	}
	fmt.Println(Styles.Muted.Render(text))
}

// Box prints text in a rounded box
func Box(title, content string) {
	if GetPersonality().Level == PersonalityMachine {
		fmt.Printf("%s: %s\n", title, content)
		return
	}
	boxStyle := Styles.Box.Width(60)
	titleLine := Styles.Title.Render(title)
	fmt.Println(boxStyle.Render(titleLine + "\n" + content))
}

// WarningBox prints text in a warning-styled box
func WarningBox(title, content string) {
	if GetPersonality().Level == PersonalityMachine {
		fmt.Fprintf(os.Stderr, "WARN %s: %s\n", title, content)
		return
	}
	boxStyle := Styles.WarningBox.Width(60)
	titleLine := Styles.Warning.Bold(true).Render(title)
	fmt.Println(boxStyle.Render(titleLine + "\n" + content))
}

// FileStatus prints a file with its scan status
func FileStatus(path string, status Icon, reason string) {
	p := GetPersonality()
	switch p.Level {
	case PersonalityMachine:
		fmt.Printf("%s\t%s\t%s\n", status, path, reason)
	case PersonalityMinimal:
		fmt.Printf("%s %s\n", status.Render(), path)
	default:
		if reason != "" {
			fmt.Printf("%s %s %s\n", status.Render(), path, Styles.Muted.Render("("+reason+")"))
		} else {
			fmt.Printf("%s %s\n", status.Render(), path)
		}
	}
}

// Summary prints a summary line with counts
func Summary(approved, skipped, total int) {
	p := GetPersonality()
	switch p.Level {
	case PersonalityMachine:
		fmt.Printf("SUMMARY: approved=%d skipped=%d total=%d\n", approved, skipped, total)
	default:
		fmt.Printf("\n%s %s  %s %s  %s %s\n",
			Styles.Success.Render(fmt.Sprintf("%d", approved)), Styles.Muted.Render("approved"),
			Styles.Warning.Render(fmt.Sprintf("%d", skipped)), Styles.Muted.Render("skipped"),
			Styles.Bold.Render(fmt.Sprintf("%d", total)), Styles.Muted.Render("total"),
		)
	}
}

// ProgressBar renders a simple progress bar
func ProgressBar(current, total int, width int) string {
	if GetPersonality().Level == PersonalityMachine {
		return fmt.Sprintf("%d/%d", current, total)
	}
	pct := float64(current) / float64(total)
	filled := int(pct * float64(width))
	empty := width - filled

	bar := Styles.Success.Render(repeatChar('█', filled)) +
		Styles.Muted.Render(repeatChar('░', empty))

	return fmt.Sprintf("%s %3.0f%%", bar, pct*100)
}

func repeatChar(c rune, n int) string {
	if n <= 0 {
		return ""
	}
	result := make([]rune, n)
	for i := range result {
		result[i] = c
	}
	return string(result)
}
