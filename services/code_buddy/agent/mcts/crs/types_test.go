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
	}

	for _, tt := range tests {
		t.Run(tt.source.String(), func(t *testing.T) {
			if got := tt.source.IsHard(); got != tt.want {
				t.Errorf("IsHard() = %v, want %v", got, tt.want)
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
