// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package crs

import (
	"testing"
)

func TestProofStatus_String(t *testing.T) {
	tests := []struct {
		status ProofStatus
		want   string
	}{
		{ProofStatusUnknown, "unknown"},
		{ProofStatusProven, "proven"},
		{ProofStatusDisproven, "disproven"},
		{ProofStatusExpanded, "expanded"},
		{ProofStatus(99), "ProofStatus(99)"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.status.String(); got != tt.want {
				t.Errorf("String() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSignalSource_String(t *testing.T) {
	tests := []struct {
		source SignalSource
		want   string
	}{
		{SignalSourceUnknown, "unknown"},
		{SignalSourceHard, "hard"},
		{SignalSourceSoft, "soft"},
		{SignalSourceSafety, "safety"},
		{SignalSource(99), "SignalSource(99)"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.source.String(); got != tt.want {
				t.Errorf("String() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSignalSource_IsHard(t *testing.T) {
	tests := []struct {
		source SignalSource
		want   bool
	}{
		{SignalSourceUnknown, false},
		{SignalSourceHard, true},
		{SignalSourceSoft, false},
		{SignalSourceSafety, true}, // Safety violations are treated as hard signals
	}

	for _, tt := range tests {
		t.Run(tt.source.String(), func(t *testing.T) {
			if got := tt.source.IsHard(); got != tt.want {
				t.Errorf("IsHard() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSignalSource_IsSafety(t *testing.T) {
	tests := []struct {
		source SignalSource
		want   bool
	}{
		{SignalSourceUnknown, false},
		{SignalSourceHard, false},
		{SignalSourceSoft, false},
		{SignalSourceSafety, true},
	}

	for _, tt := range tests {
		t.Run(tt.source.String(), func(t *testing.T) {
			if got := tt.source.IsSafety(); got != tt.want {
				t.Errorf("IsSafety() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestConstraintType_String(t *testing.T) {
	tests := []struct {
		ctype ConstraintType
		want  string
	}{
		{ConstraintTypeUnknown, "unknown"},
		{ConstraintTypeMutualExclusion, "mutual_exclusion"},
		{ConstraintTypeImplication, "implication"},
		{ConstraintTypeOrdering, "ordering"},
		{ConstraintTypeResource, "resource"},
		{ConstraintType(99), "ConstraintType(99)"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.ctype.String(); got != tt.want {
				t.Errorf("String() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDeltaType_String(t *testing.T) {
	tests := []struct {
		dtype DeltaType
		want  string
	}{
		{DeltaTypeUnknown, "unknown"},
		{DeltaTypeProof, "proof"},
		{DeltaTypeConstraint, "constraint"},
		{DeltaTypeSimilarity, "similarity"},
		{DeltaTypeDependency, "dependency"},
		{DeltaTypeHistory, "history"},
		{DeltaTypeStreaming, "streaming"},
		{DeltaTypeComposite, "composite"},
		{DeltaType(99), "DeltaType(99)"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.dtype.String(); got != tt.want {
				t.Errorf("String() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestConfig_Validate(t *testing.T) {
	t.Run("default config is valid", func(t *testing.T) {
		config := DefaultConfig()
		if err := config.Validate(); err != nil {
			t.Errorf("DefaultConfig should be valid: %v", err)
		}
	})

	t.Run("negative snapshot epoch limit", func(t *testing.T) {
		config := &Config{
			SnapshotEpochLimit: -1,
		}
		if err := config.Validate(); err == nil {
			t.Error("negative SnapshotEpochLimit should fail")
		}
	})

	t.Run("zero snapshot epoch limit is valid", func(t *testing.T) {
		config := &Config{
			SnapshotEpochLimit: 0,
		}
		if err := config.Validate(); err != nil {
			t.Errorf("zero SnapshotEpochLimit should be valid: %v", err)
		}
	})
}

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	if config.MaxGeneration != 0 {
		t.Errorf("MaxGeneration = %d, want 0", config.MaxGeneration)
	}
	if config.SnapshotEpochLimit != 1000 {
		t.Errorf("SnapshotEpochLimit = %d, want 1000", config.SnapshotEpochLimit)
	}
	if !config.EnableMetrics {
		t.Error("EnableMetrics should be true")
	}
	if !config.EnableTracing {
		t.Error("EnableTracing should be true")
	}
}

// -----------------------------------------------------------------------------
// StepRecord v2 Tests (CRS-01)
// -----------------------------------------------------------------------------

func TestActor_IsValid(t *testing.T) {
	tests := []struct {
		actor Actor
		want  bool
	}{
		{ActorRouter, true},
		{ActorMainAgent, true},
		{ActorSystem, true},
		{Actor("unknown"), false},
		{Actor(""), false},
	}

	for _, tt := range tests {
		t.Run(string(tt.actor), func(t *testing.T) {
			if got := tt.actor.IsValid(); got != tt.want {
				t.Errorf("IsValid() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDecision_IsValid(t *testing.T) {
	tests := []struct {
		decision Decision
		want     bool
	}{
		{DecisionSelectTool, true},
		{DecisionExecuteTool, true},
		{DecisionSynthesize, true},
		{DecisionCircuitBreaker, true},
		{DecisionRetry, true},
		{DecisionComplete, true},
		{DecisionError, true},
		{Decision("unknown"), false},
		{Decision(""), false},
	}

	for _, tt := range tests {
		t.Run(string(tt.decision), func(t *testing.T) {
			if got := tt.decision.IsValid(); got != tt.want {
				t.Errorf("IsValid() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestOutcome_IsValid(t *testing.T) {
	tests := []struct {
		outcome Outcome
		want    bool
	}{
		{OutcomeSuccess, true},
		{OutcomeFailure, true},
		{OutcomeSkipped, true},
		{OutcomeForced, true},
		{Outcome("unknown"), false},
		{Outcome(""), false},
	}

	for _, tt := range tests {
		t.Run(string(tt.outcome), func(t *testing.T) {
			if got := tt.outcome.IsValid(); got != tt.want {
				t.Errorf("IsValid() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestErrorCategory_IsRetryable(t *testing.T) {
	tests := []struct {
		category ErrorCategory
		want     bool
	}{
		{ErrorCategoryNone, false},
		{ErrorCategoryToolNotFound, false},
		{ErrorCategoryInvalidParams, false},
		{ErrorCategoryTimeout, true},
		{ErrorCategoryRateLimited, true},
		{ErrorCategoryPermission, false},
		{ErrorCategoryNetwork, true},
		{ErrorCategoryInternal, false},
		{ErrorCategorySafety, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.category), func(t *testing.T) {
			if got := tt.category.IsRetryable(); got != tt.want {
				t.Errorf("IsRetryable() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestStepRecord_Validate(t *testing.T) {
	validStep := StepRecord{
		SessionID:  "session-123",
		StepNumber: 1,
		Actor:      ActorRouter,
		Decision:   DecisionSelectTool,
		Outcome:    OutcomeSuccess,
		Tool:       "list_packages",
		Confidence: 0.95,
	}

	t.Run("valid step passes", func(t *testing.T) {
		if err := validStep.Validate(); err != nil {
			t.Errorf("valid step should pass: %v", err)
		}
	})

	t.Run("empty session_id fails", func(t *testing.T) {
		step := validStep
		step.SessionID = ""
		if err := step.Validate(); err == nil {
			t.Error("empty session_id should fail")
		}
	})

	t.Run("step_number < 1 fails", func(t *testing.T) {
		step := validStep
		step.StepNumber = 0
		if err := step.Validate(); err == nil {
			t.Error("step_number < 1 should fail")
		}
	})

	t.Run("empty actor fails", func(t *testing.T) {
		step := validStep
		step.Actor = ""
		if err := step.Validate(); err == nil {
			t.Error("empty actor should fail")
		}
	})

	t.Run("invalid actor fails", func(t *testing.T) {
		step := validStep
		step.Actor = Actor("bogus")
		if err := step.Validate(); err == nil {
			t.Error("invalid actor should fail")
		}
	})

	t.Run("empty decision fails", func(t *testing.T) {
		step := validStep
		step.Decision = ""
		if err := step.Validate(); err == nil {
			t.Error("empty decision should fail")
		}
	})

	t.Run("invalid decision fails", func(t *testing.T) {
		step := validStep
		step.Decision = Decision("bogus")
		if err := step.Validate(); err == nil {
			t.Error("invalid decision should fail")
		}
	})

	t.Run("empty outcome fails", func(t *testing.T) {
		step := validStep
		step.Outcome = ""
		if err := step.Validate(); err == nil {
			t.Error("empty outcome should fail")
		}
	})

	t.Run("invalid outcome fails", func(t *testing.T) {
		step := validStep
		step.Outcome = Outcome("bogus")
		if err := step.Validate(); err == nil {
			t.Error("invalid outcome should fail")
		}
	})

	t.Run("failure without error_category fails", func(t *testing.T) {
		step := validStep
		step.Outcome = OutcomeFailure
		step.ErrorCategory = ErrorCategoryNone
		if err := step.Validate(); err == nil {
			t.Error("failure without error_category should fail")
		}
	})

	t.Run("failure with error_category passes", func(t *testing.T) {
		step := validStep
		step.Outcome = OutcomeFailure
		step.ErrorCategory = ErrorCategoryTimeout
		if err := step.Validate(); err != nil {
			t.Errorf("failure with error_category should pass: %v", err)
		}
	})

	t.Run("confidence < 0 fails", func(t *testing.T) {
		step := validStep
		step.Confidence = -0.1
		if err := step.Validate(); err == nil {
			t.Error("confidence < 0 should fail")
		}
	})

	t.Run("confidence > 1 fails", func(t *testing.T) {
		step := validStep
		step.Confidence = 1.1
		if err := step.Validate(); err == nil {
			t.Error("confidence > 1 should fail")
		}
	})

	t.Run("negative duration_ms fails", func(t *testing.T) {
		step := validStep
		step.DurationMs = -1
		if err := step.Validate(); err == nil {
			t.Error("negative duration_ms should fail")
		}
	})
}

func TestStepRecord_IsToolExecution(t *testing.T) {
	tests := []struct {
		decision Decision
		want     bool
	}{
		{DecisionExecuteTool, true},
		{DecisionSelectTool, false},
		{DecisionSynthesize, false},
		{DecisionCircuitBreaker, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.decision), func(t *testing.T) {
			step := StepRecord{Decision: tt.decision}
			if got := step.IsToolExecution(); got != tt.want {
				t.Errorf("IsToolExecution() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestStepRecord_IsCircuitBreakerIntervention(t *testing.T) {
	tests := []struct {
		decision Decision
		want     bool
	}{
		{DecisionCircuitBreaker, true},
		{DecisionSelectTool, false},
		{DecisionExecuteTool, false},
		{DecisionSynthesize, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.decision), func(t *testing.T) {
			step := StepRecord{Decision: tt.decision}
			if got := step.IsCircuitBreakerIntervention(); got != tt.want {
				t.Errorf("IsCircuitBreakerIntervention() = %v, want %v", got, tt.want)
			}
		})
	}
}
