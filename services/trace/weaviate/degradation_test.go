// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package weaviate

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// -----------------------------------------------------------------------------
// DegradationMode Tests
// -----------------------------------------------------------------------------

func TestDegradationMode_String(t *testing.T) {
	tests := []struct {
		mode     DegradationMode
		expected string
	}{
		{ModeNormal, "normal"},
		{ModeDegraded, "degraded"},
		{ModeDisabled, "disabled"},
		{DegradationMode(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.mode.String())
		})
	}
}

// -----------------------------------------------------------------------------
// BaseDegradationHandler Tests
// -----------------------------------------------------------------------------

func TestBaseDegradationHandler_StartsNormal(t *testing.T) {
	handler := NewBaseDegradationHandler("test", nil)
	assert.Equal(t, ModeNormal, handler.GetMode())
	assert.True(t, handler.IsNormal())
	assert.False(t, handler.IsDegraded())
	assert.False(t, handler.IsDisabled())
}

func TestBaseDegradationHandler_OnDegraded(t *testing.T) {
	handler := NewBaseDegradationHandler("test", nil)
	handler.OnDegraded("test reason")

	assert.Equal(t, ModeDegraded, handler.GetMode())
	assert.False(t, handler.IsNormal())
	assert.True(t, handler.IsDegraded())
}

func TestBaseDegradationHandler_OnRecovered(t *testing.T) {
	handler := NewBaseDegradationHandler("test", nil)
	handler.OnDegraded("test reason")
	handler.OnRecovered()

	assert.Equal(t, ModeNormal, handler.GetMode())
	assert.True(t, handler.IsNormal())
	assert.False(t, handler.IsDegraded())
}

func TestBaseDegradationHandler_SetDisabled(t *testing.T) {
	handler := NewBaseDegradationHandler("test", nil)
	handler.SetDisabled()

	assert.Equal(t, ModeDisabled, handler.GetMode())
	assert.True(t, handler.IsDisabled())
}

// -----------------------------------------------------------------------------
// LibraryDocsDegradation Tests
// -----------------------------------------------------------------------------

func TestLibraryDocsDegradation_ShouldSkipSearch(t *testing.T) {
	t.Run("normal mode allows search", func(t *testing.T) {
		handler := NewLibraryDocsDegradation(nil)
		assert.False(t, handler.ShouldSkipSearch())
	})

	t.Run("degraded mode skips search", func(t *testing.T) {
		handler := NewLibraryDocsDegradation(nil)
		handler.OnDegraded("test")
		assert.True(t, handler.ShouldSkipSearch())
	})

	t.Run("recovered mode allows search", func(t *testing.T) {
		handler := NewLibraryDocsDegradation(nil)
		handler.OnDegraded("test")
		handler.OnRecovered()
		assert.False(t, handler.ShouldSkipSearch())
	})

	t.Run("disabled mode skips search", func(t *testing.T) {
		handler := NewLibraryDocsDegradation(nil)
		handler.SetDisabled()
		assert.True(t, handler.ShouldSkipSearch())
	})
}

// -----------------------------------------------------------------------------
// SyntheticMemoryDegradation Tests
// -----------------------------------------------------------------------------

func TestSyntheticMemoryDegradation_ShouldSkipMemoryOps(t *testing.T) {
	t.Run("normal mode allows memory ops", func(t *testing.T) {
		handler := NewSyntheticMemoryDegradation(nil)
		assert.False(t, handler.ShouldSkipMemoryOps())
	})

	t.Run("degraded mode skips memory ops", func(t *testing.T) {
		handler := NewSyntheticMemoryDegradation(nil)
		handler.OnDegraded("test")
		assert.True(t, handler.ShouldSkipMemoryOps())
	})

	t.Run("recovered mode allows memory ops", func(t *testing.T) {
		handler := NewSyntheticMemoryDegradation(nil)
		handler.OnDegraded("test")
		handler.OnRecovered()
		assert.False(t, handler.ShouldSkipMemoryOps())
	})
}

// -----------------------------------------------------------------------------
// PromptCacheDegradation Tests
// -----------------------------------------------------------------------------

func TestPromptCacheDegradation_ShouldBypassCache(t *testing.T) {
	t.Run("normal mode uses cache", func(t *testing.T) {
		handler := NewPromptCacheDegradation(nil)
		assert.False(t, handler.ShouldBypassCache())
	})

	t.Run("degraded mode bypasses cache", func(t *testing.T) {
		handler := NewPromptCacheDegradation(nil)
		handler.OnDegraded("test")
		assert.True(t, handler.ShouldBypassCache())
	})

	t.Run("recovered mode uses cache", func(t *testing.T) {
		handler := NewPromptCacheDegradation(nil)
		handler.OnDegraded("test")
		handler.OnRecovered()
		assert.False(t, handler.ShouldBypassCache())
	})
}

// -----------------------------------------------------------------------------
// Thread Safety Tests
// -----------------------------------------------------------------------------

func TestBaseDegradationHandler_ConcurrentAccess(t *testing.T) {
	handler := NewBaseDegradationHandler("test", nil)

	// Run concurrent operations
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				handler.OnDegraded("test")
				_ = handler.GetMode()
				handler.OnRecovered()
				_ = handler.IsNormal()
				_ = handler.IsDegraded()
			}
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// Should not panic and final state should be consistent
	mode := handler.GetMode()
	assert.Contains(t, []DegradationMode{ModeNormal, ModeDegraded}, mode)
}

// -----------------------------------------------------------------------------
// Interface Compliance Tests
// -----------------------------------------------------------------------------

func TestDegradationHandler_InterfaceCompliance(t *testing.T) {
	// Verify all handlers implement the interface
	var _ DegradationHandler = &BaseDegradationHandler{}
	var _ DegradationHandler = &LibraryDocsDegradation{}
	var _ DegradationHandler = &SyntheticMemoryDegradation{}
	var _ DegradationHandler = &PromptCacheDegradation{}
}
