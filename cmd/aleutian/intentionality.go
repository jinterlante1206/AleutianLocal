package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

// RecoveryProposer defines the interface for proposing recovery actions.
//
// # Description
//
// RecoveryProposer allows the system to propose fixes for detected issues,
// but respects user intentionality by asking before expensive operations.
//
// # Thread Safety
//
// Implementations should be safe for use from a single goroutine.
type RecoveryProposer interface {
	// ProposeRecovery proposes a recovery action and optionally executes it.
	ProposeRecovery(ctx context.Context, issue string, action RecoveryAction) error

	// SetAutoApprove enables automatic approval for non-expensive actions.
	SetAutoApprove(enabled bool)

	// SetAlwaysAsk forces asking even for non-expensive actions.
	SetAlwaysAsk(enabled bool)
}

// RecoveryAction describes a proposed fix for an issue.
//
// # Description
//
// Contains the action to execute and metadata about whether it's
// expensive (large download, destructive recreate, etc.).
//
// # Example
//
//	action := RecoveryAction{
//	    Description: "Re-download the llama2 model (7GB)",
//	    Expensive:   true,
//	    Execute: func(ctx context.Context) error {
//	        return ollama.Pull(ctx, "llama2")
//	    },
//	}
type RecoveryAction struct {
	// Description explains what the action will do.
	Description string

	// Expensive indicates if the action is costly (time, bandwidth, etc.).
	// Expensive actions always require confirmation.
	Expensive bool

	// Destructive indicates if the action might cause data loss.
	// Destructive actions require explicit "yes" confirmation.
	Destructive bool

	// Execute performs the recovery action.
	Execute func(ctx context.Context) error

	// EstimatedDuration is how long the action might take.
	EstimatedDuration time.Duration

	// EstimatedSize is the size of any downloads (0 if not applicable).
	EstimatedSize int64
}

// IntentionalityConfig configures intentionality checking.
//
// # Description
//
// Controls how recovery proposals are handled.
//
// # Example
//
//	config := IntentionalityConfig{
//	    AutoApproveNonExpensive: true,
//	    DefaultTimeout:          30 * time.Second,
//	}
type IntentionalityConfig struct {
	// AutoApproveNonExpensive auto-executes cheap actions.
	// Default: true
	AutoApproveNonExpensive bool

	// AlwaysAsk forces asking even for non-expensive actions.
	// Default: false
	AlwaysAsk bool

	// DefaultTimeout for user input.
	// Default: 30 seconds
	DefaultTimeout time.Duration

	// Input is where to read user input from.
	// Default: os.Stdin
	Input io.Reader

	// Output is where to write prompts.
	// Default: os.Stderr
	Output io.Writer

	// NonInteractive mode auto-declines all prompts.
	// Default: false
	NonInteractive bool
}

// DefaultIntentionalityConfig returns sensible defaults.
//
// # Description
//
// Returns configuration that auto-approves cheap fixes but asks
// for expensive ones.
//
// # Outputs
//
//   - IntentionalityConfig: Configuration with default values
func DefaultIntentionalityConfig() IntentionalityConfig {
	return IntentionalityConfig{
		AutoApproveNonExpensive: true,
		AlwaysAsk:               false,
		DefaultTimeout:          30 * time.Second,
		Input:                   os.Stdin,
		Output:                  os.Stderr,
		NonInteractive:          false,
	}
}

// DefaultRecoveryProposer implements RecoveryProposer.
//
// # Description
//
// Implements intentionality checks to prevent error fatigue. Instead of
// automatically re-downloading a 10GB model the user intentionally deleted,
// this asks first.
//
// # Use Cases
//
//   - Model re-download after intentional deletion
//   - Config regeneration
//   - Volume recreation
//
// # Thread Safety
//
// DefaultRecoveryProposer is NOT safe for concurrent use.
// Use from a single goroutine (typically the main CLI flow).
//
// # Limitations
//
//   - Requires TTY for interactive prompts
//   - Non-interactive mode declines all expensive actions
//
// # Example
//
//	proposer := NewRecoveryProposer(DefaultIntentionalityConfig())
//
//	err := proposer.ProposeRecovery(ctx, "Model not found",
//	    RecoveryAction{
//	        Description: "Download llama2 (7GB)",
//	        Expensive:   true,
//	        Execute:     downloadModel,
//	    })
type DefaultRecoveryProposer struct {
	config IntentionalityConfig
}

// NewRecoveryProposer creates a new recovery proposer.
//
// # Description
//
// Creates a proposer with the specified configuration.
//
// # Inputs
//
//   - config: Configuration for intentionality checks
//
// # Outputs
//
//   - *DefaultRecoveryProposer: New proposer
func NewRecoveryProposer(config IntentionalityConfig) *DefaultRecoveryProposer {
	if config.DefaultTimeout <= 0 {
		config.DefaultTimeout = 30 * time.Second
	}
	if config.Input == nil {
		config.Input = os.Stdin
	}
	if config.Output == nil {
		config.Output = os.Stderr
	}

	return &DefaultRecoveryProposer{
		config: config,
	}
}

// ProposeRecovery proposes a recovery action.
//
// # Description
//
// If the action is expensive or AlwaysAsk is set, prompts the user
// for confirmation. Otherwise, auto-executes the action.
//
// # Inputs
//
//   - ctx: Context for cancellation
//   - issue: Description of the issue
//   - action: Proposed recovery action
//
// # Outputs
//
//   - error: ErrRecoveryDeclined if user declined, or action execution error
//
// # Example
//
//	err := proposer.ProposeRecovery(ctx, "Config missing",
//	    RecoveryAction{
//	        Description: "Generate default config",
//	        Expensive:   false,
//	        Execute:     generateConfig,
//	    })
func (p *DefaultRecoveryProposer) ProposeRecovery(ctx context.Context, issue string, action RecoveryAction) error {
	// Determine if we need to ask
	needsConfirmation := action.Expensive || action.Destructive || p.config.AlwaysAsk

	if !needsConfirmation && p.config.AutoApproveNonExpensive {
		// Auto-execute non-expensive actions
		return action.Execute(ctx)
	}

	// Non-interactive mode declines expensive actions
	if p.config.NonInteractive {
		return ErrRecoveryDeclined
	}

	// Show proposal
	p.showProposal(issue, action)

	// Get user confirmation
	approved, err := p.getUserConfirmation(action.Destructive)
	if err != nil {
		return fmt.Errorf("failed to get user input: %w", err)
	}

	if !approved {
		return ErrRecoveryDeclined
	}

	// Execute the action
	return action.Execute(ctx)
}

// SetAutoApprove enables/disables automatic approval.
//
// # Description
//
// When enabled, non-expensive actions are executed without asking.
//
// # Inputs
//
//   - enabled: Whether to auto-approve non-expensive actions
func (p *DefaultRecoveryProposer) SetAutoApprove(enabled bool) {
	p.config.AutoApproveNonExpensive = enabled
}

// SetAlwaysAsk enables/disables always asking.
//
// # Description
//
// When enabled, even non-expensive actions require confirmation.
//
// # Inputs
//
//   - enabled: Whether to always ask for confirmation
func (p *DefaultRecoveryProposer) SetAlwaysAsk(enabled bool) {
	p.config.AlwaysAsk = enabled
}

// showProposal displays the recovery proposal to the user.
func (p *DefaultRecoveryProposer) showProposal(issue string, action RecoveryAction) {
	fmt.Fprintln(p.config.Output)

	// Issue
	if action.Destructive {
		fmt.Fprintf(p.config.Output, "⚠️  Issue: %s\n", issue)
	} else {
		fmt.Fprintf(p.config.Output, "ℹ️  Issue: %s\n", issue)
	}

	// Proposed fix
	fmt.Fprintf(p.config.Output, "   Proposed fix: %s\n", action.Description)

	// Details
	if action.EstimatedDuration > 0 {
		fmt.Fprintf(p.config.Output, "   Estimated time: %s\n", action.EstimatedDuration)
	}
	if action.EstimatedSize > 0 {
		fmt.Fprintf(p.config.Output, "   Download size: %s\n", formatBytesHuman(action.EstimatedSize))
	}

	// Warning for destructive actions
	if action.Destructive {
		fmt.Fprintf(p.config.Output, "   ⚠️  This action may cause data loss!\n")
	}

	fmt.Fprintln(p.config.Output)
}

// getUserConfirmation prompts the user for confirmation.
func (p *DefaultRecoveryProposer) getUserConfirmation(destructive bool) (bool, error) {
	prompt := "   Proceed? [y/N]: "
	if destructive {
		prompt = "   Type 'yes' to confirm: "
	}

	fmt.Fprint(p.config.Output, prompt)

	reader := bufio.NewReader(p.config.Input)
	response, err := reader.ReadString('\n')
	if err != nil {
		return false, err
	}

	response = strings.TrimSpace(strings.ToLower(response))

	if destructive {
		return response == "yes", nil
	}

	return response == "y" || response == "yes", nil
}

// formatBytesHuman formats bytes as human-readable string.
func formatBytesHuman(bytes int64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
	)

	switch {
	case bytes >= GB:
		return fmt.Sprintf("%.1f GB", float64(bytes)/GB)
	case bytes >= MB:
		return fmt.Sprintf("%.1f MB", float64(bytes)/MB)
	case bytes >= KB:
		return fmt.Sprintf("%.1f KB", float64(bytes)/KB)
	default:
		return fmt.Sprintf("%d bytes", bytes)
	}
}

// ErrRecoveryDeclined is returned when the user declines a recovery action.
var ErrRecoveryDeclined = fmt.Errorf("recovery declined by user")

// Compile-time interface check
var _ RecoveryProposer = (*DefaultRecoveryProposer)(nil)

// ProposeRecovery is a convenience function using default config.
//
// # Description
//
// Proposes a recovery action using default configuration.
// In non-interactive environments, declines expensive actions.
//
// # Inputs
//
//   - ctx: Context for cancellation
//   - issue: Description of the issue
//   - action: Proposed recovery action
//
// # Outputs
//
//   - error: ErrRecoveryDeclined if declined, or action error
func ProposeRecovery(ctx context.Context, issue string, action RecoveryAction) error {
	proposer := NewRecoveryProposer(DefaultIntentionalityConfig())
	return proposer.ProposeRecovery(ctx, issue, action)
}

// MustApprove executes an action that requires explicit approval.
//
// # Description
//
// Always asks for confirmation regardless of expense level.
// Useful for actions that shouldn't be auto-approved.
//
// # Inputs
//
//   - ctx: Context
//   - issue: Issue description
//   - action: Action to execute
//
// # Outputs
//
//   - error: ErrRecoveryDeclined if declined, or action error
func MustApprove(ctx context.Context, issue string, action RecoveryAction) error {
	config := DefaultIntentionalityConfig()
	config.AlwaysAsk = true
	proposer := NewRecoveryProposer(config)
	return proposer.ProposeRecovery(ctx, issue, action)
}
