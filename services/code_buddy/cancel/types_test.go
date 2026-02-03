// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package cancel

import (
	"testing"
	"time"
)

func TestCancelType_String(t *testing.T) {
	tests := []struct {
		name     string
		ct       CancelType
		expected string
	}{
		{"user", CancelUser, "user"},
		{"timeout", CancelTimeout, "timeout"},
		{"deadlock", CancelDeadlock, "deadlock"},
		{"resource_limit", CancelResourceLimit, "resource_limit"},
		{"parent", CancelParent, "parent"},
		{"shutdown", CancelShutdown, "shutdown"},
		{"unknown", CancelType(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.ct.String(); got != tt.expected {
				t.Errorf("CancelType.String() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestState_String(t *testing.T) {
	tests := []struct {
		name     string
		s        State
		expected string
	}{
		{"running", StateRunning, "running"},
		{"cancelling", StateCancelling, "cancelling"},
		{"cancelled", StateCancelled, "cancelled"},
		{"done", StateDone, "done"},
		{"unknown", State(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.s.String(); got != tt.expected {
				t.Errorf("State.String() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestState_IsTerminal(t *testing.T) {
	tests := []struct {
		name     string
		s        State
		expected bool
	}{
		{"running is not terminal", StateRunning, false},
		{"cancelling is not terminal", StateCancelling, false},
		{"cancelled is terminal", StateCancelled, true},
		{"done is terminal", StateDone, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.s.IsTerminal(); got != tt.expected {
				t.Errorf("State.IsTerminal() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestLevel_String(t *testing.T) {
	tests := []struct {
		name     string
		l        Level
		expected string
	}{
		{"session", LevelSession, "session"},
		{"activity", LevelActivity, "activity"},
		{"algorithm", LevelAlgorithm, "algorithm"},
		{"unknown", Level(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.l.String(); got != tt.expected {
				t.Errorf("Level.String() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestControllerConfig_Validate(t *testing.T) {
	tests := []struct {
		name      string
		config    ControllerConfig
		wantError bool
	}{
		{
			name: "zero config after defaults is valid",
			config: func() ControllerConfig {
				c := ControllerConfig{}
				c.ApplyDefaults()
				return c
			}(),
			wantError: false,
		},
		{
			name: "valid config",
			config: ControllerConfig{
				DefaultTimeout:        30 * time.Second,
				DeadlockMultiplier:    3,
				GracePeriod:           500 * time.Millisecond,
				ForceKillTimeout:      2 * time.Second,
				ProgressCheckInterval: 100 * time.Millisecond,
			},
			wantError: false,
		},
		{
			name: "negative timeout is invalid",
			config: ControllerConfig{
				DefaultTimeout: -1 * time.Second,
			},
			wantError: true,
		},
		{
			name: "deadlock multiplier < 2 is invalid",
			config: ControllerConfig{
				DeadlockMultiplier: 1,
			},
			wantError: true,
		},
		{
			name: "negative grace period is invalid",
			config: ControllerConfig{
				GracePeriod: -1 * time.Second,
			},
			wantError: true,
		},
		{
			name: "force kill <= grace period is invalid",
			config: ControllerConfig{
				GracePeriod:      2 * time.Second,
				ForceKillTimeout: 1 * time.Second,
			},
			wantError: true,
		},
		{
			name: "negative progress check interval is invalid",
			config: ControllerConfig{
				ProgressCheckInterval: -1 * time.Millisecond,
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantError {
				t.Errorf("ControllerConfig.Validate() error = %v, wantError %v", err, tt.wantError)
			}
		})
	}
}

func TestControllerConfig_ApplyDefaults(t *testing.T) {
	config := ControllerConfig{}
	config.ApplyDefaults()

	if config.DefaultTimeout != 30*time.Second {
		t.Errorf("DefaultTimeout = %v, want 30s", config.DefaultTimeout)
	}
	if config.DeadlockMultiplier != 3 {
		t.Errorf("DeadlockMultiplier = %v, want 3", config.DeadlockMultiplier)
	}
	if config.GracePeriod != 500*time.Millisecond {
		t.Errorf("GracePeriod = %v, want 500ms", config.GracePeriod)
	}
	if config.ForceKillTimeout != 2*time.Second {
		t.Errorf("ForceKillTimeout = %v, want 2s", config.ForceKillTimeout)
	}
	if config.ProgressCheckInterval != 100*time.Millisecond {
		t.Errorf("ProgressCheckInterval = %v, want 100ms", config.ProgressCheckInterval)
	}
}

func TestSessionConfig_Validate(t *testing.T) {
	tests := []struct {
		name      string
		config    SessionConfig
		wantError bool
	}{
		{
			name:      "empty ID is invalid",
			config:    SessionConfig{},
			wantError: true,
		},
		{
			name: "valid config",
			config: SessionConfig{
				ID:               "test-session",
				Timeout:          30 * time.Second,
				ProgressInterval: 1 * time.Second,
			},
			wantError: false,
		},
		{
			name: "negative timeout is invalid",
			config: SessionConfig{
				ID:      "test-session",
				Timeout: -1 * time.Second,
			},
			wantError: true,
		},
		{
			name: "negative progress interval is invalid",
			config: SessionConfig{
				ID:               "test-session",
				ProgressInterval: -1 * time.Second,
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantError {
				t.Errorf("SessionConfig.Validate() error = %v, wantError %v", err, tt.wantError)
			}
		})
	}
}

func TestSessionConfig_ApplyDefaults(t *testing.T) {
	config := SessionConfig{ID: "test"}
	config.ApplyDefaults()

	if config.ProgressInterval != 1*time.Second {
		t.Errorf("ProgressInterval = %v, want 1s", config.ProgressInterval)
	}
}

func TestResourceLimits_Validate(t *testing.T) {
	tests := []struct {
		name      string
		limits    ResourceLimits
		wantError bool
	}{
		{
			name:      "zero limits is valid",
			limits:    ResourceLimits{},
			wantError: false,
		},
		{
			name: "valid limits",
			limits: ResourceLimits{
				MaxMemoryBytes: 1 << 30,
				MaxCPUPercent:  80,
				MaxGoroutines:  1000,
			},
			wantError: false,
		},
		{
			name: "negative memory is invalid",
			limits: ResourceLimits{
				MaxMemoryBytes: -1,
			},
			wantError: true,
		},
		{
			name: "CPU > 100 is invalid",
			limits: ResourceLimits{
				MaxCPUPercent: 150,
			},
			wantError: true,
		},
		{
			name: "negative CPU is invalid",
			limits: ResourceLimits{
				MaxCPUPercent: -1,
			},
			wantError: true,
		},
		{
			name: "negative goroutines is invalid",
			limits: ResourceLimits{
				MaxGoroutines: -1,
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.limits.Validate()
			if (err != nil) != tt.wantError {
				t.Errorf("ResourceLimits.Validate() error = %v, wantError %v", err, tt.wantError)
			}
		})
	}
}

func TestResourceLimits_HasLimits(t *testing.T) {
	tests := []struct {
		name     string
		limits   ResourceLimits
		expected bool
	}{
		{
			name:     "no limits",
			limits:   ResourceLimits{},
			expected: false,
		},
		{
			name: "memory limit",
			limits: ResourceLimits{
				MaxMemoryBytes: 1 << 30,
			},
			expected: true,
		},
		{
			name: "CPU limit",
			limits: ResourceLimits{
				MaxCPUPercent: 80,
			},
			expected: true,
		},
		{
			name: "goroutine limit",
			limits: ResourceLimits{
				MaxGoroutines: 1000,
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.limits.HasLimits(); got != tt.expected {
				t.Errorf("ResourceLimits.HasLimits() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestCancelReason(t *testing.T) {
	now := time.Now()
	reason := CancelReason{
		Type:      CancelTimeout,
		Message:   "Test timeout",
		Threshold: "5s",
		Component: "test-algo",
		Timestamp: now,
	}

	if reason.Type != CancelTimeout {
		t.Errorf("Type = %v, want %v", reason.Type, CancelTimeout)
	}
	if reason.Message != "Test timeout" {
		t.Errorf("Message = %v, want 'Test timeout'", reason.Message)
	}
	if reason.Threshold != "5s" {
		t.Errorf("Threshold = %v, want '5s'", reason.Threshold)
	}
	if reason.Component != "test-algo" {
		t.Errorf("Component = %v, want 'test-algo'", reason.Component)
	}
	if !reason.Timestamp.Equal(now) {
		t.Errorf("Timestamp = %v, want %v", reason.Timestamp, now)
	}
}
