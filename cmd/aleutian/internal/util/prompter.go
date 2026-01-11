// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

/*
Package util provides UserPrompter for handling interactive user input.

UserPrompter abstracts all user interaction, enabling:
  - Interactive prompts in terminal environments
  - Non-interactive modes for CI/CD pipelines (--non-interactive)
  - Auto-approve modes for scripting (--yes)
  - Testability through mock implementations

# Design Rationale

User prompts are scattered throughout command handlers. By abstracting them behind
an interface, we can:
  - Test code that prompts users without real stdin
  - Support CI environments that cannot respond to prompts
  - Provide --yes flag to auto-approve all prompts
*/
package util

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

// -----------------------------------------------------------------------------
// Errors
// -----------------------------------------------------------------------------

// ErrNonInteractive is returned when a prompt is attempted in non-interactive mode.
var ErrNonInteractive = errors.New("prompt not allowed in non-interactive mode")

// ErrCancelled is returned when the user cancels a prompt.
var ErrCancelled = errors.New("operation cancelled by user")

// ErrInvalidSelection is returned when the user provides an invalid selection.
var ErrInvalidSelection = errors.New("invalid selection")

// -----------------------------------------------------------------------------
// Interface Definition
// -----------------------------------------------------------------------------

// UserPrompter handles interactive user prompts.
//
// This interface abstracts user interaction, enabling non-interactive modes
// for CI/scripting while maintaining UX for interactive terminal use.
//
// # Thread Safety
//
// Implementations should be safe for sequential use but are not designed
// for concurrent prompt handling from multiple goroutines.
//
// # Context Handling
//
// Methods accept context for cancellation support, though interactive
// prompts may not immediately respond to cancellation while waiting for input.
//
// # Assumptions
//
//   - Callers handle the case where context is cancelled before prompt completes
//   - For interactive use, stdin/stdout are available and functional
type UserPrompter interface {
	// Confirm asks a yes/no question and returns the answer.
	//
	// # Description
	//
	// Displays a yes/no prompt to the user and waits for their response.
	// Accepts various forms of yes (y, yes, Y, YES) and no (n, no, N, NO).
	//
	// # Inputs
	//
	//   - ctx: Context for cancellation
	//   - prompt: The question to display (e.g., "Continue?")
	//
	// # Outputs
	//
	//   - bool: True if user confirmed, false if declined
	//   - error: Non-nil if cancelled, non-interactive, or I/O error
	//
	// # Examples
	//
	//   confirmed, err := p.Confirm(ctx, "Delete all data?")
	//   if err != nil {
	//       return fmt.Errorf("prompt failed: %w", err)
	//   }
	//   if !confirmed {
	//       return ErrCancelled
	//   }
	//
	// # Limitations
	//
	//   - May not immediately respond to context cancellation while blocking on read
	//   - Empty input defaults to 'no' for safety
	//
	// # Assumptions
	//
	//   - prompt string does not contain control characters
	Confirm(ctx context.Context, prompt string) (bool, error)

	// Select presents options and returns the selected index.
	//
	// # Description
	//
	// Displays a numbered list of options and waits for the user to
	// select one by entering its number (1-based).
	//
	// # Inputs
	//
	//   - ctx: Context for cancellation
	//   - prompt: Header text to display above options
	//   - options: List of option strings to display
	//
	// # Outputs
	//
	//   - int: Zero-based index of selected option
	//   - error: Non-nil if cancelled, invalid selection, or I/O error
	//
	// # Examples
	//
	//   idx, err := p.Select(ctx, "Choose machine action:", []string{
	//       "Fix automatically",
	//       "Skip",
	//       "Abort",
	//   })
	//   if err != nil {
	//       return fmt.Errorf("selection failed: %w", err)
	//   }
	//
	// # Limitations
	//
	//   - User must enter 1-based number, not option text
	//   - Options slice must not be empty
	//   - Cannot interrupt blocking read once started
	//
	// # Assumptions
	//
	//   - options slice is non-empty (returns error if empty)
	//   - option strings do not contain newlines
	Select(ctx context.Context, prompt string, options []string) (int, error)

	// IsInteractive returns true if prompts are enabled.
	//
	// # Description
	//
	// Returns whether the prompter is configured for interactive use.
	// Non-interactive prompters will return errors from Confirm/Select.
	//
	// # Outputs
	//
	//   - bool: True if prompts will be displayed, false otherwise
	//
	// # Examples
	//
	//   if !p.IsInteractive() {
	//       log.Warn("Running in non-interactive mode, using defaults")
	//   }
	IsInteractive() bool
}

// -----------------------------------------------------------------------------
// Interactive Implementation
// -----------------------------------------------------------------------------

// InteractivePrompter implements UserPrompter using stdin/stdout.
//
// This is the default prompter for terminal environments. It reads user
// input from the configured reader (typically os.Stdin) and writes prompts
// to the configured writer (typically os.Stdout).
//
// # Thread Safety
//
// Not safe for concurrent use. Prompts should be serialized.
//
// # Assumptions
//
//   - reader and writer are valid and not nil
//   - reader provides line-based input (newline-terminated)
type InteractivePrompter struct {
	reader io.Reader
	writer io.Writer
}

// NewInteractivePrompter creates a prompter that uses stdin/stdout.
//
// # Description
//
// Creates a UserPrompter that displays prompts to stdout and reads
// responses from stdin. Use this in production terminal environments.
//
// # Outputs
//
//   - *InteractivePrompter: Ready-to-use interactive prompter
//
// # Examples
//
//	prompter := NewInteractivePrompter()
//	confirmed, err := prompter.Confirm(ctx, "Continue?")
//
// # Assumptions
//
//   - os.Stdin and os.Stdout are available
func NewInteractivePrompter() *InteractivePrompter {
	return &InteractivePrompter{
		reader: os.Stdin,
		writer: os.Stdout,
	}
}

// NewInteractivePrompterWithIO creates a prompter with custom I/O.
//
// # Description
//
// Creates a UserPrompter with custom reader/writer. Useful for testing
// with mock I/O or for redirecting prompts to alternative streams.
//
// # Inputs
//
//   - reader: Source for user input (must not be nil)
//   - writer: Destination for prompts (must not be nil)
//
// # Outputs
//
//   - *InteractivePrompter: Configured prompter
//
// # Examples
//
//	var buf bytes.Buffer
//	buf.WriteString("y\n")
//	prompter := NewInteractivePrompterWithIO(&buf, os.Stdout)
//
// # Assumptions
//
//   - reader and writer are not nil
//   - reader provides newline-terminated input
func NewInteractivePrompterWithIO(reader io.Reader, writer io.Writer) *InteractivePrompter {
	return &InteractivePrompter{
		reader: reader,
		writer: writer,
	}
}

// Confirm asks a yes/no question and returns the answer.
func (p *InteractivePrompter) Confirm(ctx context.Context, prompt string) (bool, error) {
	// Check context before blocking on input
	select {
	case <-ctx.Done():
		return false, ctx.Err()
	default:
	}

	fmt.Fprintf(p.writer, "%s [y/N]: ", prompt)

	scanner := bufio.NewScanner(p.reader)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return false, fmt.Errorf("failed to read input: %w", err)
		}
		// EOF without input
		return false, nil
	}

	response := strings.TrimSpace(strings.ToLower(scanner.Text()))

	switch response {
	case "y", "yes":
		return true, nil
	case "n", "no", "":
		return false, nil
	default:
		// Invalid input, treat as no for safety
		return false, nil
	}
}

// Select presents options and returns the selected index.
func (p *InteractivePrompter) Select(ctx context.Context, prompt string, options []string) (int, error) {
	// Check context before blocking on input
	select {
	case <-ctx.Done():
		return -1, ctx.Err()
	default:
	}

	if len(options) == 0 {
		return -1, errors.New("no options provided")
	}

	fmt.Fprintln(p.writer, prompt)
	for i, opt := range options {
		fmt.Fprintf(p.writer, "  %d. %s\n", i+1, opt)
	}
	fmt.Fprint(p.writer, "Enter number: ")

	scanner := bufio.NewScanner(p.reader)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return -1, fmt.Errorf("failed to read input: %w", err)
		}
		return -1, ErrCancelled
	}

	input := strings.TrimSpace(scanner.Text())
	num, err := strconv.Atoi(input)
	if err != nil {
		return -1, fmt.Errorf("%w: %q is not a number", ErrInvalidSelection, input)
	}

	// Convert 1-based to 0-based index
	idx := num - 1
	if idx < 0 || idx >= len(options) {
		return -1, fmt.Errorf("%w: %d is out of range (1-%d)",
			ErrInvalidSelection, num, len(options))
	}

	return idx, nil
}

// IsInteractive returns true for InteractivePrompter.
func (p *InteractivePrompter) IsInteractive() bool {
	return true
}

// -----------------------------------------------------------------------------
// Non-Interactive Implementation
// -----------------------------------------------------------------------------

// NonInteractivePrompter implements UserPrompter for CI/scripting.
//
// This prompter either auto-approves all prompts (for --yes flag) or
// returns errors for all prompts (for --non-interactive without --yes).
//
// # Thread Safety
//
// Safe for concurrent use (stateless after construction).
//
// # Assumptions
//
//   - Auto-approve mode always selects the first option for Select()
type NonInteractivePrompter struct {
	// autoApprove determines behavior:
	//   - true: Confirm returns true, Select returns 0 (first option)
	//   - false: All methods return ErrNonInteractive
	autoApprove bool
}

// NewNonInteractivePrompter creates a prompter that rejects all prompts.
//
// # Description
//
// Creates a UserPrompter that returns ErrNonInteractive for all prompts.
// Use this for --non-interactive mode without --yes.
//
// # Outputs
//
//   - *NonInteractivePrompter: Prompter that rejects prompts
//
// # Examples
//
//	prompter := NewNonInteractivePrompter()
//	_, err := prompter.Confirm(ctx, "Continue?")
//	// err == ErrNonInteractive
func NewNonInteractivePrompter() *NonInteractivePrompter {
	return &NonInteractivePrompter{autoApprove: false}
}

// NewAutoApprovePrompter creates a prompter that auto-approves all prompts.
//
// # Description
//
// Creates a UserPrompter that automatically approves confirmations and
// selects the first option. Use this for --yes flag in CI/scripting.
//
// # Outputs
//
//   - *NonInteractivePrompter: Prompter that auto-approves
//
// # Examples
//
//	prompter := NewAutoApprovePrompter()
//	confirmed, err := prompter.Confirm(ctx, "Continue?")
//	// confirmed == true, err == nil
//
// # Assumptions
//
//   - For Select(), the first option (index 0) is acceptable as default
func NewAutoApprovePrompter() *NonInteractivePrompter {
	return &NonInteractivePrompter{autoApprove: true}
}

// Confirm either auto-approves or returns an error.
func (p *NonInteractivePrompter) Confirm(ctx context.Context, prompt string) (bool, error) {
	if p.autoApprove {
		return true, nil
	}
	return false, fmt.Errorf("%w: cannot prompt for %q", ErrNonInteractive, prompt)
}

// Select either returns the first option or an error.
func (p *NonInteractivePrompter) Select(ctx context.Context, prompt string, options []string) (int, error) {
	if p.autoApprove {
		if len(options) == 0 {
			return -1, errors.New("no options provided")
		}
		return 0, nil // Select first option
	}
	return -1, fmt.Errorf("%w: cannot prompt for %q", ErrNonInteractive, prompt)
}

// IsInteractive returns false for NonInteractivePrompter.
func (p *NonInteractivePrompter) IsInteractive() bool {
	return false
}

// -----------------------------------------------------------------------------
// Mock Implementation for Testing
// -----------------------------------------------------------------------------

// MockPrompter is a test double for UserPrompter.
//
// Configure the mock by setting function fields before use. If a function
// field is nil and the corresponding method is called, it will panic.
//
// # Thread Safety
//
// Not safe for concurrent use due to Calls slice mutation.
//
// # Examples
//
//	mock := &MockPrompter{
//	    ConfirmFunc: func(ctx context.Context, prompt string) (bool, error) {
//	        if prompt == "Delete all data?" {
//	            return true, nil
//	        }
//	        return false, nil
//	    },
//	}
//
// # Assumptions
//
//   - ConfirmFunc/SelectFunc are set before calling respective methods
//   - Panics if function fields are nil (fail-fast for test debugging)
type MockPrompter struct {
	// ConfirmFunc is called when Confirm is invoked
	ConfirmFunc func(ctx context.Context, prompt string) (bool, error)

	// SelectFunc is called when Select is invoked
	SelectFunc func(ctx context.Context, prompt string, options []string) (int, error)

	// IsInteractiveFunc is called when IsInteractive is invoked
	// If nil, defaults to returning true
	IsInteractiveFunc func() bool

	// Calls records all method invocations for verification
	Calls []PrompterCall
}

// PrompterCall records a single method invocation.
type PrompterCall struct {
	Method  string
	Prompt  string
	Options []string
}

// Confirm delegates to ConfirmFunc and records the call.
func (m *MockPrompter) Confirm(ctx context.Context, prompt string) (bool, error) {
	m.Calls = append(m.Calls, PrompterCall{
		Method: "Confirm",
		Prompt: prompt,
	})
	if m.ConfirmFunc == nil {
		panic("MockPrompter.ConfirmFunc not set")
	}
	return m.ConfirmFunc(ctx, prompt)
}

// Select delegates to SelectFunc and records the call.
func (m *MockPrompter) Select(ctx context.Context, prompt string, options []string) (int, error) {
	m.Calls = append(m.Calls, PrompterCall{
		Method:  "Select",
		Prompt:  prompt,
		Options: options,
	})
	if m.SelectFunc == nil {
		panic("MockPrompter.SelectFunc not set")
	}
	return m.SelectFunc(ctx, prompt, options)
}

// IsInteractive delegates to IsInteractiveFunc or returns true if nil.
func (m *MockPrompter) IsInteractive() bool {
	if m.IsInteractiveFunc != nil {
		return m.IsInteractiveFunc()
	}
	return true
}

// Reset clears all recorded calls.
func (m *MockPrompter) Reset() {
	m.Calls = nil
}

// Compile-time interface compliance check.
var (
	_ UserPrompter = (*InteractivePrompter)(nil)
	_ UserPrompter = (*NonInteractivePrompter)(nil)
	_ UserPrompter = (*MockPrompter)(nil)
)
