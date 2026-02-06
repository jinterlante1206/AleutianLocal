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
	"log/slog"
	"sync/atomic"
)

// -----------------------------------------------------------------------------
// Degradation Mode
// -----------------------------------------------------------------------------

// DegradationMode represents the operational mode of a component.
type DegradationMode int32

const (
	// ModeNormal indicates full functionality.
	ModeNormal DegradationMode = iota
	// ModeDegraded indicates reduced functionality.
	ModeDegraded
	// ModeDisabled indicates the component is completely disabled.
	ModeDisabled
)

// String returns the string representation of DegradationMode.
func (m DegradationMode) String() string {
	switch m {
	case ModeNormal:
		return "normal"
	case ModeDegraded:
		return "degraded"
	case ModeDisabled:
		return "disabled"
	default:
		return "unknown"
	}
}

// -----------------------------------------------------------------------------
// Degradation Handler Interface
// -----------------------------------------------------------------------------

// DegradationHandler is notified of Weaviate availability changes.
//
// Description:
//
//	Components that depend on Weaviate should implement this interface
//	to handle degradation gracefully.
//
// Thread Safety: Implementations must be safe for concurrent use.
type DegradationHandler interface {
	// OnDegraded is called when Weaviate becomes unavailable.
	//
	// Inputs:
	//   - reason: Description of why degradation occurred.
	//
	// Implementations should:
	//   - Switch to fallback behavior
	//   - Log the degradation
	//   - Update metrics if applicable
	OnDegraded(reason string)

	// OnRecovered is called when Weaviate becomes available again.
	//
	// Implementations should:
	//   - Restore normal behavior
	//   - Log the recovery
	//   - Optionally replay queued operations
	OnRecovered()

	// GetMode returns the current degradation mode.
	GetMode() DegradationMode
}

// -----------------------------------------------------------------------------
// Base Degradation Handler
// -----------------------------------------------------------------------------

// BaseDegradationHandler provides a basic implementation of DegradationHandler.
//
// Description:
//
//	Tracks degradation state and provides logging. Embed this in
//	component-specific handlers.
//
// Thread Safety: Safe for concurrent use.
type BaseDegradationHandler struct {
	name   string
	mode   atomic.Int32
	logger *slog.Logger
}

// NewBaseDegradationHandler creates a new base handler.
//
// Inputs:
//
//	name - Component name for logging.
//	logger - Logger instance. Uses slog.Default() if nil.
//
// Outputs:
//
//	*BaseDegradationHandler - Ready-to-use handler.
func NewBaseDegradationHandler(name string, logger *slog.Logger) *BaseDegradationHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &BaseDegradationHandler{
		name:   name,
		logger: logger.With(slog.String("component", name)),
	}
}

// OnDegraded marks the handler as degraded.
func (h *BaseDegradationHandler) OnDegraded(reason string) {
	h.mode.Store(int32(ModeDegraded))
	h.logger.Warn("component degraded due to weaviate unavailability",
		slog.String("reason", reason))
}

// OnRecovered marks the handler as normal.
func (h *BaseDegradationHandler) OnRecovered() {
	h.mode.Store(int32(ModeNormal))
	h.logger.Info("component recovered, weaviate available")
}

// GetMode returns the current mode.
func (h *BaseDegradationHandler) GetMode() DegradationMode {
	return DegradationMode(h.mode.Load())
}

// IsNormal returns true if operating normally.
func (h *BaseDegradationHandler) IsNormal() bool {
	return h.GetMode() == ModeNormal
}

// IsDegraded returns true if operating with reduced functionality.
func (h *BaseDegradationHandler) IsDegraded() bool {
	return h.GetMode() == ModeDegraded
}

// IsDisabled returns true if the component is disabled.
func (h *BaseDegradationHandler) IsDisabled() bool {
	return h.GetMode() == ModeDisabled
}

// SetDisabled explicitly disables the handler.
func (h *BaseDegradationHandler) SetDisabled() {
	h.mode.Store(int32(ModeDisabled))
	h.logger.Warn("component explicitly disabled")
}

// -----------------------------------------------------------------------------
// Component-Specific Handlers
// -----------------------------------------------------------------------------

// LibraryDocsDegradation handles degradation for library documentation search.
//
// Description:
//
//	When Weaviate is unavailable, library documentation search is skipped.
//	The system continues with code-only context.
type LibraryDocsDegradation struct {
	*BaseDegradationHandler
}

// NewLibraryDocsDegradation creates a handler for library docs.
func NewLibraryDocsDegradation(logger *slog.Logger) *LibraryDocsDegradation {
	return &LibraryDocsDegradation{
		BaseDegradationHandler: NewBaseDegradationHandler("library_docs", logger),
	}
}

// OnDegraded handles library docs degradation.
func (h *LibraryDocsDegradation) OnDegraded(reason string) {
	h.BaseDegradationHandler.OnDegraded(reason)
	h.logger.Warn("library documentation search disabled, using code-only context",
		slog.String("reason", reason))
}

// OnRecovered handles library docs recovery.
func (h *LibraryDocsDegradation) OnRecovered() {
	h.BaseDegradationHandler.OnRecovered()
	h.logger.Info("library documentation search restored")
}

// ShouldSkipSearch returns true if library doc search should be skipped.
func (h *LibraryDocsDegradation) ShouldSkipSearch() bool {
	return h.GetMode() != ModeNormal
}

// -----------------------------------------------------------------------------

// SyntheticMemoryDegradation handles degradation for synthetic memory.
//
// Description:
//
//	When Weaviate is unavailable, synthetic memory operations are skipped.
//	Learned constraints are not persisted or retrieved.
type SyntheticMemoryDegradation struct {
	*BaseDegradationHandler
}

// NewSyntheticMemoryDegradation creates a handler for synthetic memory.
func NewSyntheticMemoryDegradation(logger *slog.Logger) *SyntheticMemoryDegradation {
	return &SyntheticMemoryDegradation{
		BaseDegradationHandler: NewBaseDegradationHandler("synthetic_memory", logger),
	}
}

// OnDegraded handles synthetic memory degradation.
func (h *SyntheticMemoryDegradation) OnDegraded(reason string) {
	h.BaseDegradationHandler.OnDegraded(reason)
	h.logger.Warn("synthetic memory disabled, learned constraints will not persist",
		slog.String("reason", reason))
}

// OnRecovered handles synthetic memory recovery.
func (h *SyntheticMemoryDegradation) OnRecovered() {
	h.BaseDegradationHandler.OnRecovered()
	h.logger.Info("synthetic memory restored")
}

// ShouldSkipMemoryOps returns true if memory operations should be skipped.
func (h *SyntheticMemoryDegradation) ShouldSkipMemoryOps() bool {
	return h.GetMode() != ModeNormal
}

// -----------------------------------------------------------------------------

// PromptCacheDegradation handles degradation for prompt caching.
//
// Description:
//
//	When Weaviate is unavailable, prompt cache is disabled.
//	All requests pass through without caching.
type PromptCacheDegradation struct {
	*BaseDegradationHandler
}

// NewPromptCacheDegradation creates a handler for prompt caching.
func NewPromptCacheDegradation(logger *slog.Logger) *PromptCacheDegradation {
	return &PromptCacheDegradation{
		BaseDegradationHandler: NewBaseDegradationHandler("prompt_cache", logger),
	}
}

// OnDegraded handles prompt cache degradation.
func (h *PromptCacheDegradation) OnDegraded(reason string) {
	h.BaseDegradationHandler.OnDegraded(reason)
	h.logger.Warn("prompt cache disabled, all requests will be pass-through",
		slog.String("reason", reason))
}

// OnRecovered handles prompt cache recovery.
func (h *PromptCacheDegradation) OnRecovered() {
	h.BaseDegradationHandler.OnRecovered()
	h.logger.Info("prompt cache restored")
}

// ShouldBypassCache returns true if cache should be bypassed.
func (h *PromptCacheDegradation) ShouldBypassCache() bool {
	return h.GetMode() != ModeNormal
}
