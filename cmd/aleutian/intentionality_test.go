// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestDefaultIntentionalityConfig(t *testing.T) {
	config := DefaultIntentionalityConfig()

	if !config.AutoApproveNonExpensive {
		t.Error("AutoApproveNonExpensive should default to true")
	}
	if config.AlwaysAsk {
		t.Error("AlwaysAsk should default to false")
	}
	if config.DefaultTimeout <= 0 {
		t.Error("DefaultTimeout should be positive")
	}
}

func TestNewRecoveryProposer(t *testing.T) {
	proposer := NewRecoveryProposer(DefaultIntentionalityConfig())

	if proposer == nil {
		t.Fatal("NewRecoveryProposer returned nil")
	}
}

func TestRecoveryProposer_AutoApprove_NonExpensive(t *testing.T) {
	var executed bool

	config := DefaultIntentionalityConfig()
	config.AutoApproveNonExpensive = true
	proposer := NewRecoveryProposer(config)

	err := proposer.ProposeRecovery(context.Background(), "test issue",
		RecoveryAction{
			Description: "Non-expensive fix",
			Expensive:   false,
			Execute: func(ctx context.Context) error {
				executed = true
				return nil
			},
		})

	if err != nil {
		t.Errorf("ProposeRecovery failed: %v", err)
	}
	if !executed {
		t.Error("Non-expensive action should be auto-executed")
	}
}

func TestRecoveryProposer_NonInteractive_Declines(t *testing.T) {
	config := DefaultIntentionalityConfig()
	config.NonInteractive = true
	proposer := NewRecoveryProposer(config)

	err := proposer.ProposeRecovery(context.Background(), "test issue",
		RecoveryAction{
			Description: "Expensive fix",
			Expensive:   true,
			Execute: func(ctx context.Context) error {
				t.Error("Should not execute in non-interactive mode")
				return nil
			},
		})

	if !errors.Is(err, ErrRecoveryDeclined) {
		t.Errorf("Expected ErrRecoveryDeclined, got: %v", err)
	}
}

func TestRecoveryProposer_UserApproves(t *testing.T) {
	var executed bool
	input := strings.NewReader("y\n")
	output := &bytes.Buffer{}

	config := IntentionalityConfig{
		AutoApproveNonExpensive: false,
		AlwaysAsk:               true,
		Input:                   input,
		Output:                  output,
	}
	proposer := NewRecoveryProposer(config)

	err := proposer.ProposeRecovery(context.Background(), "test issue",
		RecoveryAction{
			Description: "Test fix",
			Expensive:   false,
			Execute: func(ctx context.Context) error {
				executed = true
				return nil
			},
		})

	if err != nil {
		t.Errorf("ProposeRecovery failed: %v", err)
	}
	if !executed {
		t.Error("Action should be executed after approval")
	}
}

func TestRecoveryProposer_UserDeclines(t *testing.T) {
	input := strings.NewReader("n\n")
	output := &bytes.Buffer{}

	config := IntentionalityConfig{
		AlwaysAsk: true,
		Input:     input,
		Output:    output,
	}
	proposer := NewRecoveryProposer(config)

	err := proposer.ProposeRecovery(context.Background(), "test issue",
		RecoveryAction{
			Description: "Test fix",
			Expensive:   false,
			Execute: func(ctx context.Context) error {
				t.Error("Should not execute when declined")
				return nil
			},
		})

	if !errors.Is(err, ErrRecoveryDeclined) {
		t.Errorf("Expected ErrRecoveryDeclined, got: %v", err)
	}
}

func TestRecoveryProposer_DestructiveRequiresYes(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		executed bool
	}{
		{"yes approved", "yes\n", true},
		{"y not enough", "y\n", false},
		{"YES approved", "YES\n", true},
		{"no declined", "no\n", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var executed bool
			input := strings.NewReader(tt.input)
			output := &bytes.Buffer{}

			config := IntentionalityConfig{
				Input:  input,
				Output: output,
			}
			proposer := NewRecoveryProposer(config)

			err := proposer.ProposeRecovery(context.Background(), "test issue",
				RecoveryAction{
					Description: "Destructive fix",
					Destructive: true,
					Execute: func(ctx context.Context) error {
						executed = true
						return nil
					},
				})

			if tt.executed && err != nil {
				t.Errorf("Expected success, got: %v", err)
			}
			if !tt.executed && err == nil {
				t.Error("Expected decline")
			}
			if executed != tt.executed {
				t.Errorf("executed = %v, want %v", executed, tt.executed)
			}
		})
	}
}

func TestRecoveryProposer_ShowsDetails(t *testing.T) {
	input := strings.NewReader("y\n")
	output := &bytes.Buffer{}

	config := IntentionalityConfig{
		AlwaysAsk: true,
		Input:     input,
		Output:    output,
	}
	proposer := NewRecoveryProposer(config)

	proposer.ProposeRecovery(context.Background(), "Model missing",
		RecoveryAction{
			Description:       "Download llama2",
			Expensive:         true,
			EstimatedDuration: 5 * time.Minute,
			EstimatedSize:     7 * 1024 * 1024 * 1024, // 7GB
			Execute:           func(ctx context.Context) error { return nil },
		})

	outputStr := output.String()

	if !strings.Contains(outputStr, "Model missing") {
		t.Error("Output should contain issue")
	}
	if !strings.Contains(outputStr, "Download llama2") {
		t.Error("Output should contain description")
	}
	if !strings.Contains(outputStr, "5m") {
		t.Error("Output should contain estimated time")
	}
	if !strings.Contains(outputStr, "GB") {
		t.Error("Output should contain download size")
	}
}

func TestRecoveryProposer_ActionError(t *testing.T) {
	expectedErr := errors.New("action failed")

	config := DefaultIntentionalityConfig()
	proposer := NewRecoveryProposer(config)

	err := proposer.ProposeRecovery(context.Background(), "test issue",
		RecoveryAction{
			Description: "Failing action",
			Expensive:   false,
			Execute: func(ctx context.Context) error {
				return expectedErr
			},
		})

	if !errors.Is(err, expectedErr) {
		t.Errorf("Expected action error, got: %v", err)
	}
}

func TestRecoveryProposer_SetAutoApprove(t *testing.T) {
	config := DefaultIntentionalityConfig()
	config.AutoApproveNonExpensive = true
	proposer := NewRecoveryProposer(config)

	proposer.SetAutoApprove(false)

	// Now it should NOT auto-approve
	config.NonInteractive = true // Use non-interactive to avoid needing input
	proposer = NewRecoveryProposer(config)
	proposer.SetAutoApprove(false)
	proposer.SetAlwaysAsk(true)

	// This would hang without NonInteractive, but we've set it
}

func TestRecoveryProposer_SetAlwaysAsk(t *testing.T) {
	input := strings.NewReader("y\n")
	output := &bytes.Buffer{}

	config := IntentionalityConfig{
		AutoApproveNonExpensive: true,
		AlwaysAsk:               false,
		Input:                   input,
		Output:                  output,
	}
	proposer := NewRecoveryProposer(config)
	proposer.SetAlwaysAsk(true)

	var executed bool
	err := proposer.ProposeRecovery(context.Background(), "test",
		RecoveryAction{
			Description: "test",
			Expensive:   false, // Would normally auto-approve
			Execute: func(ctx context.Context) error {
				executed = true
				return nil
			},
		})

	if err != nil {
		t.Errorf("ProposeRecovery failed: %v", err)
	}
	if !executed {
		t.Error("Action should have executed after approval")
	}

	// Should have prompted
	if !strings.Contains(output.String(), "Proceed?") {
		t.Error("Should have asked for confirmation")
	}
}

func TestFormatBytesHuman(t *testing.T) {
	tests := []struct {
		bytes    int64
		expected string
	}{
		{0, "0 bytes"},
		{500, "500 bytes"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1024 * 1024, "1.0 MB"},
		{1024 * 1024 * 1024, "1.0 GB"},
		{7 * 1024 * 1024 * 1024, "7.0 GB"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			got := formatBytesHuman(tt.bytes)
			if got != tt.expected {
				t.Errorf("formatBytesHuman(%d) = %q, want %q", tt.bytes, got, tt.expected)
			}
		})
	}
}

func TestRecoveryProposer_InterfaceCompliance(t *testing.T) {
	var _ RecoveryProposer = (*DefaultRecoveryProposer)(nil)
}
