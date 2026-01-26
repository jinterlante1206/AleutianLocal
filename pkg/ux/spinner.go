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
	"fmt"
	"sync"
	"time"
)

// SpinnerType defines the animation style
type SpinnerType int

const (
	SpinnerDots SpinnerType = iota
	SpinnerWave
	SpinnerAnchor
	SpinnerCompass
)

var spinnerFrames = map[SpinnerType][]string{
	SpinnerDots:    {"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"},
	SpinnerWave:    {"~", "≈", "≋", "≈"},
	SpinnerAnchor:  {"⚓", "⚓ ", "⚓  ", "⚓   ", "⚓  ", "⚓ "},
	SpinnerCompass: {"◐", "◓", "◑", "◒"},
}

// Spinner provides an animated loading indicator
type Spinner struct {
	message    string
	spinType   SpinnerType
	stop       chan struct{}
	done       chan struct{}
	mu         sync.Mutex
	isRunning  bool
	frameIndex int
}

// NewSpinner creates a new spinner with the given message
func NewSpinner(message string) *Spinner {
	return &Spinner{
		message:  message,
		spinType: SpinnerDots,
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
	}
}

// WithType sets the spinner animation type
func (s *Spinner) WithType(t SpinnerType) *Spinner {
	s.spinType = t
	return s
}

// Start begins the spinner animation
func (s *Spinner) Start() {
	s.mu.Lock()
	if s.isRunning {
		s.mu.Unlock()
		return
	}
	s.isRunning = true
	s.mu.Unlock()

	// In machine mode, just print the message once
	if GetPersonality().Level == PersonalityMachine {
		fmt.Printf("PROGRESS: %s\n", s.message)
		return
	}

	go func() {
		frames := spinnerFrames[s.spinType]
		ticker := time.NewTicker(80 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-s.stop:
				// Clear the spinner line
				fmt.Print("\r\033[K")
				close(s.done)
				return
			case <-ticker.C:
				frame := Styles.Highlight.Render(frames[s.frameIndex])
				fmt.Printf("\r%s %s", frame, s.message)
				s.frameIndex = (s.frameIndex + 1) % len(frames)
			}
		}
	}()
}

// Stop halts the spinner animation
func (s *Spinner) Stop() {
	s.mu.Lock()
	if !s.isRunning {
		s.mu.Unlock()
		return
	}
	s.isRunning = false
	s.mu.Unlock()

	if GetPersonality().Level == PersonalityMachine {
		return
	}

	close(s.stop)
	<-s.done
}

// UpdateMessage changes the spinner message while running
func (s *Spinner) UpdateMessage(message string) {
	s.mu.Lock()
	s.message = message
	s.mu.Unlock()
}

// StopWithSuccess stops and prints a success message
func (s *Spinner) StopWithSuccess(message string) {
	s.Stop()
	Success(message)
}

// StopWithError stops and prints an error message
func (s *Spinner) StopWithError(message string) {
	s.Stop()
	Error(message)
}

// StopWithWarning stops and prints a warning message
func (s *Spinner) StopWithWarning(message string) {
	s.Stop()
	Warning(message)
}

// WithSpinner runs a function with a spinner, handling success/error automatically
func WithSpinner(message string, fn func() error) error {
	spin := NewSpinner(message)
	spin.Start()

	err := fn()

	if err != nil {
		spin.StopWithError(fmt.Sprintf("%s: %v", message, err))
		return err
	}

	spin.StopWithSuccess(message)
	return nil
}

// ProgressSpinner combines a spinner with progress tracking
type ProgressSpinner struct {
	*Spinner
	current int
	total   int
}

// NewProgressSpinner creates a spinner that shows progress
func NewProgressSpinner(message string, total int) *ProgressSpinner {
	return &ProgressSpinner{
		Spinner: NewSpinner(message),
		total:   total,
	}
}

// Increment advances the progress counter
func (p *ProgressSpinner) Increment() {
	p.mu.Lock()
	p.current++
	if GetPersonality().Level != PersonalityMachine {
		p.message = fmt.Sprintf("%s [%d/%d]", p.Spinner.message, p.current, p.total)
	}
	p.mu.Unlock()
}

// SetProgress sets the current progress value
func (p *ProgressSpinner) SetProgress(current int) {
	p.mu.Lock()
	p.current = current
	if GetPersonality().Level != PersonalityMachine {
		p.message = fmt.Sprintf("%s [%d/%d]", p.Spinner.message, p.current, p.total)
	}
	p.mu.Unlock()
}
