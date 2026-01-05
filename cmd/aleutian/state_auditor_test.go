package main

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestDefaultStateAuditConfig(t *testing.T) {
	config := DefaultStateAuditConfig()

	if config.DefaultMaxStaleness <= 0 {
		t.Error("DefaultMaxStaleness should be positive")
	}
	if config.AuditTimeout <= 0 {
		t.Error("AuditTimeout should be positive")
	}
	if !config.ContinueOnError {
		t.Error("ContinueOnError should default to true")
	}
}

func TestNewStateAuditor(t *testing.T) {
	auditor := NewStateAuditor(DefaultStateAuditConfig())

	if auditor == nil {
		t.Fatal("NewStateAuditor returned nil")
	}
}

func TestNewStateAuditor_DefaultsZeroConfig(t *testing.T) {
	auditor := NewStateAuditor(StateAuditConfig{
		// All zero values
	})

	if auditor == nil {
		t.Fatal("NewStateAuditor returned nil")
	}
}

func TestStateAuditor_RegisterState(t *testing.T) {
	auditor := NewStateAuditor(DefaultStateAuditConfig())

	err := auditor.RegisterState("test_state", StateRegistration{
		GetCached: func() (interface{}, error) { return "cached", nil },
		GetActual: func(ctx context.Context) (interface{}, error) { return "actual", nil },
	})

	if err != nil {
		t.Errorf("RegisterState failed: %v", err)
	}
}

func TestStateAuditor_RegisterState_Validation(t *testing.T) {
	auditor := NewStateAuditor(DefaultStateAuditConfig())

	tests := []struct {
		name    string
		state   string
		opts    StateRegistration
		wantErr bool
	}{
		{
			name:  "empty name",
			state: "",
			opts: StateRegistration{
				GetCached: func() (interface{}, error) { return nil, nil },
				GetActual: func(ctx context.Context) (interface{}, error) { return nil, nil },
			},
			wantErr: true,
		},
		{
			name:  "missing GetCached",
			state: "test",
			opts: StateRegistration{
				GetActual: func(ctx context.Context) (interface{}, error) { return nil, nil },
			},
			wantErr: true,
		},
		{
			name:  "missing GetActual",
			state: "test",
			opts: StateRegistration{
				GetCached: func() (interface{}, error) { return nil, nil },
			},
			wantErr: true,
		},
		{
			name:  "valid registration",
			state: "valid",
			opts: StateRegistration{
				GetCached: func() (interface{}, error) { return nil, nil },
				GetActual: func(ctx context.Context) (interface{}, error) { return nil, nil },
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := auditor.RegisterState(tt.state, tt.opts)
			if (err != nil) != tt.wantErr {
				t.Errorf("RegisterState() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestStateAuditor_AuditOne_NoDrift(t *testing.T) {
	auditor := NewStateAuditor(DefaultStateAuditConfig())

	auditor.RegisterState("test", StateRegistration{
		GetCached: func() (interface{}, error) { return "same", nil },
		GetActual: func(ctx context.Context) (interface{}, error) { return "same", nil },
	})

	audit, err := auditor.AuditOne(context.Background(), "test")

	if err != nil {
		t.Errorf("AuditOne failed: %v", err)
	}
	if audit.HasDrift {
		t.Error("Should not have drift when values are equal")
	}
	if audit.Error != nil {
		t.Errorf("Audit error: %v", audit.Error)
	}
}

func TestStateAuditor_AuditOne_WithDrift(t *testing.T) {
	auditor := NewStateAuditor(DefaultStateAuditConfig())

	auditor.RegisterState("test", StateRegistration{
		GetCached: func() (interface{}, error) { return "cached", nil },
		GetActual: func(ctx context.Context) (interface{}, error) { return "actual", nil },
	})

	audit, err := auditor.AuditOne(context.Background(), "test")

	if err != nil {
		t.Errorf("AuditOne failed: %v", err)
	}
	if !audit.HasDrift {
		t.Error("Should have drift when values differ")
	}
	if audit.CachedValue != "cached" {
		t.Errorf("CachedValue = %v, want 'cached'", audit.CachedValue)
	}
	if audit.ActualValue != "actual" {
		t.Errorf("ActualValue = %v, want 'actual'", audit.ActualValue)
	}
}

func TestStateAuditor_AuditOne_NotFound(t *testing.T) {
	auditor := NewStateAuditor(DefaultStateAuditConfig())

	_, err := auditor.AuditOne(context.Background(), "nonexistent")

	if err == nil {
		t.Error("Should return error for non-existent state")
	}
}

func TestStateAuditor_AuditOne_GetCachedError(t *testing.T) {
	auditor := NewStateAuditor(DefaultStateAuditConfig())

	auditor.RegisterState("test", StateRegistration{
		GetCached: func() (interface{}, error) { return nil, errors.New("cache error") },
		GetActual: func(ctx context.Context) (interface{}, error) { return "actual", nil },
	})

	audit, _ := auditor.AuditOne(context.Background(), "test")

	if audit.Error == nil {
		t.Error("Audit should have error when GetCached fails")
	}
}

func TestStateAuditor_AuditOne_GetActualError(t *testing.T) {
	auditor := NewStateAuditor(DefaultStateAuditConfig())

	auditor.RegisterState("test", StateRegistration{
		GetCached: func() (interface{}, error) { return "cached", nil },
		GetActual: func(ctx context.Context) (interface{}, error) { return nil, errors.New("actual error") },
	})

	audit, _ := auditor.AuditOne(context.Background(), "test")

	if audit.Error == nil {
		t.Error("Audit should have error when GetActual fails")
	}
}

func TestStateAuditor_AuditOne_CustomCompare(t *testing.T) {
	auditor := NewStateAuditor(DefaultStateAuditConfig())

	// Custom compare that ignores case
	auditor.RegisterState("test", StateRegistration{
		GetCached: func() (interface{}, error) { return "HELLO", nil },
		GetActual: func(ctx context.Context) (interface{}, error) { return "hello", nil },
		CompareFunc: func(cached, actual interface{}) bool {
			return true // Always match
		},
	})

	audit, _ := auditor.AuditOne(context.Background(), "test")

	if audit.HasDrift {
		t.Error("Custom compare should have matched")
	}
}

func TestStateAuditor_AuditOne_OnDriftCallback(t *testing.T) {
	auditor := NewStateAuditor(DefaultStateAuditConfig())

	var callbackInvoked bool

	auditor.RegisterState("test", StateRegistration{
		GetCached: func() (interface{}, error) { return "cached", nil },
		GetActual: func(ctx context.Context) (interface{}, error) { return "actual", nil },
		OnDrift: func(cached, actual interface{}) {
			callbackInvoked = true
		},
	})

	auditor.AuditOne(context.Background(), "test")

	if !callbackInvoked {
		t.Error("OnDrift callback should have been invoked")
	}
}

func TestStateAuditor_AuditAll(t *testing.T) {
	auditor := NewStateAuditor(DefaultStateAuditConfig())

	// Register multiple states
	auditor.RegisterState("state1", StateRegistration{
		GetCached: func() (interface{}, error) { return "same", nil },
		GetActual: func(ctx context.Context) (interface{}, error) { return "same", nil },
	})

	auditor.RegisterState("state2", StateRegistration{
		GetCached: func() (interface{}, error) { return "cached", nil },
		GetActual: func(ctx context.Context) (interface{}, error) { return "actual", nil },
		Critical:  true,
	})

	result, err := auditor.AuditAll(context.Background())

	if err != nil {
		t.Errorf("AuditAll failed: %v", err)
	}

	if result.TotalChecked != 2 {
		t.Errorf("TotalChecked = %d, want 2", result.TotalChecked)
	}

	if result.DriftCount != 1 {
		t.Errorf("DriftCount = %d, want 1", result.DriftCount)
	}

	if result.CriticalDriftCount != 1 {
		t.Errorf("CriticalDriftCount = %d, want 1", result.CriticalDriftCount)
	}

	if !result.HasCriticalDrift() {
		t.Error("HasCriticalDrift() should return true")
	}
}

func TestStateAuditor_AuditAll_ContextCancellation(t *testing.T) {
	auditor := NewStateAuditor(DefaultStateAuditConfig())

	// Register a slow state
	auditor.RegisterState("slow", StateRegistration{
		GetCached: func() (interface{}, error) { return "cached", nil },
		GetActual: func(ctx context.Context) (interface{}, error) {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Second):
				return "actual", nil
			}
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := auditor.AuditAll(ctx)

	if err == nil {
		t.Error("Should return error when context is cancelled")
	}
}

func TestStateAuditor_OnDrift_GlobalCallback(t *testing.T) {
	auditor := NewStateAuditor(DefaultStateAuditConfig())

	var callbackCount int32

	auditor.OnDrift(func(audit StateAudit) {
		atomic.AddInt32(&callbackCount, 1)
	})

	auditor.RegisterState("test", StateRegistration{
		GetCached: func() (interface{}, error) { return "cached", nil },
		GetActual: func(ctx context.Context) (interface{}, error) { return "actual", nil },
	})

	auditor.AuditOne(context.Background(), "test")

	if atomic.LoadInt32(&callbackCount) != 1 {
		t.Error("Global OnDrift callback should have been invoked")
	}
}

func TestStateAuditor_GetDriftReport(t *testing.T) {
	auditor := NewStateAuditor(DefaultStateAuditConfig())

	auditor.RegisterState("drifting", StateRegistration{
		GetCached: func() (interface{}, error) { return "cached", nil },
		GetActual: func(ctx context.Context) (interface{}, error) { return "actual", nil },
	})

	auditor.AuditAll(context.Background())

	report := auditor.GetDriftReport()

	if len(report.DriftingStates) != 1 {
		t.Errorf("DriftingStates count = %d, want 1", len(report.DriftingStates))
	}

	if report.TotalAudits == 0 {
		t.Error("TotalAudits should be > 0")
	}

	if report.TotalDriftDetected == 0 {
		t.Error("TotalDriftDetected should be > 0")
	}
}

func TestStateAuditor_PeriodicAudit(t *testing.T) {
	auditor := NewStateAuditor(DefaultStateAuditConfig())

	var auditCount int32

	auditor.RegisterState("test", StateRegistration{
		GetCached: func() (interface{}, error) { return "same", nil },
		GetActual: func(ctx context.Context) (interface{}, error) {
			atomic.AddInt32(&auditCount, 1)
			return "same", nil
		},
	})

	// Start with short interval
	auditor.StartPeriodicAudit(50 * time.Millisecond)

	// Wait for some audits
	time.Sleep(200 * time.Millisecond)

	auditor.StopPeriodicAudit()

	count := atomic.LoadInt32(&auditCount)
	if count < 2 {
		t.Errorf("Expected at least 2 periodic audits, got %d", count)
	}
}

func TestStateAuditor_PeriodicAudit_DoubleStart(t *testing.T) {
	auditor := NewStateAuditor(DefaultStateAuditConfig())

	auditor.RegisterState("test", StateRegistration{
		GetCached: func() (interface{}, error) { return "same", nil },
		GetActual: func(ctx context.Context) (interface{}, error) { return "same", nil },
	})

	auditor.StartPeriodicAudit(100 * time.Millisecond)
	auditor.StartPeriodicAudit(100 * time.Millisecond) // Should be no-op

	auditor.StopPeriodicAudit()
	// Should not panic
}

func TestStateAuditor_PeriodicAudit_DoubleStop(t *testing.T) {
	auditor := NewStateAuditor(DefaultStateAuditConfig())

	auditor.StopPeriodicAudit() // Should be no-op when not started
	auditor.StopPeriodicAudit() // Should not panic
}

func TestStateAuditor_DriftClears(t *testing.T) {
	auditor := NewStateAuditor(DefaultStateAuditConfig())

	var actualValue string = "different"

	auditor.RegisterState("test", StateRegistration{
		GetCached: func() (interface{}, error) { return "cached", nil },
		GetActual: func(ctx context.Context) (interface{}, error) { return actualValue, nil },
	})

	// First audit - should have drift
	auditor.AuditOne(context.Background(), "test")
	report := auditor.GetDriftReport()
	if len(report.DriftingStates) != 1 {
		t.Error("Should have drift after first audit")
	}

	// Fix the drift
	actualValue = "cached"

	// Second audit - drift should clear
	auditor.AuditOne(context.Background(), "test")
	report = auditor.GetDriftReport()
	if len(report.DriftingStates) != 0 {
		t.Error("Drift should clear when values match")
	}
}

func TestStateAuditor_CriticalState(t *testing.T) {
	auditor := NewStateAuditor(DefaultStateAuditConfig())

	auditor.RegisterState("critical", StateRegistration{
		GetCached: func() (interface{}, error) { return "cached", nil },
		GetActual: func(ctx context.Context) (interface{}, error) { return "actual", nil },
		Critical:  true,
	})

	audit, _ := auditor.AuditOne(context.Background(), "critical")

	if !audit.IsCritical {
		t.Error("Audit should mark critical state")
	}
}

func TestAuditResult_HasCriticalDrift(t *testing.T) {
	tests := []struct {
		name     string
		count    int
		expected bool
	}{
		{"no critical drift", 0, false},
		{"has critical drift", 1, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := AuditResult{CriticalDriftCount: tt.count}
			if result.HasCriticalDrift() != tt.expected {
				t.Errorf("HasCriticalDrift() = %v, want %v", result.HasCriticalDrift(), tt.expected)
			}
		})
	}
}

func TestAuditContainerState(t *testing.T) {
	reg := AuditContainerState(
		"test-container",
		func() (string, error) { return "running", nil },
		func(ctx context.Context) (string, error) { return "running", nil },
	)

	if reg.GetCached == nil {
		t.Error("GetCached should be set")
	}
	if reg.GetActual == nil {
		t.Error("GetActual should be set")
	}
	if !reg.Critical {
		t.Error("Container state should be critical")
	}
}

func TestAuditConfigState(t *testing.T) {
	reg := AuditConfigState(
		"/etc/app/config.yaml",
		func() (string, error) { return "hash123", nil },
		func(ctx context.Context) (string, error) { return "hash123", nil },
	)

	if reg.GetCached == nil {
		t.Error("GetCached should be set")
	}
	if reg.GetActual == nil {
		t.Error("GetActual should be set")
	}
	if reg.Critical {
		t.Error("Config state should not be critical by default")
	}
}

func TestStateAuditor_InterfaceCompliance(t *testing.T) {
	var _ StateAuditor = (*DefaultStateAuditor)(nil)
}

func TestDefaultCompare(t *testing.T) {
	tests := []struct {
		name     string
		cached   interface{}
		actual   interface{}
		expected bool
	}{
		{"equal strings", "hello", "hello", true},
		{"different strings", "hello", "world", false},
		{"equal ints", 42, 42, true},
		{"different ints", 42, 43, false},
		{"mixed types same repr", "42", 42, true}, // string "42" == fmt.Sprintf("%v", 42) = "42"
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := defaultCompare(tt.cached, tt.actual)
			if got != tt.expected {
				t.Errorf("defaultCompare(%v, %v) = %v, want %v", tt.cached, tt.actual, got, tt.expected)
			}
		})
	}
}
