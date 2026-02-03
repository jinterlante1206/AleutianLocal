// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package agent

import (
	"fmt"
	"sort"
	"sync"
)

// DefaultPhaseRegistry implements PhaseRegistry.
//
// Description:
//
//	DefaultPhaseRegistry maps AgentState values to PhaseExecutor implementations.
//	It provides thread-safe access to registered phases.
//
// Thread Safety: DefaultPhaseRegistry is safe for concurrent use.
type DefaultPhaseRegistry struct {
	mu     sync.RWMutex
	phases map[AgentState]PhaseExecutor
}

// NewPhaseRegistry creates a new phase registry.
//
// Description:
//
//	Creates an empty registry. Use Register() to add phases.
//
// Outputs:
//
//	*DefaultPhaseRegistry - The new registry.
func NewPhaseRegistry() *DefaultPhaseRegistry {
	return &DefaultPhaseRegistry{
		phases: make(map[AgentState]PhaseExecutor),
	}
}

// Register adds a phase executor for a state.
//
// Description:
//
//	Associates a PhaseExecutor with an AgentState. The executor will
//	be called when the agent enters that state. Overwrites any previously
//	registered executor for the state.
//
// Inputs:
//
//	state - The state to register the phase for.
//	executor - The phase executor.
//
// Thread Safety: This method is safe for concurrent use.
func (r *DefaultPhaseRegistry) Register(state AgentState, executor PhaseExecutor) {
	if executor == nil {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.phases[state] = executor
}

// GetPhase implements PhaseRegistry.
//
// Description:
//
//	Returns the phase registered for the given state. Returns false
//	if no phase is registered.
//
// Inputs:
//
//	state - The state to get the phase for.
//
// Outputs:
//
//	PhaseExecutor - The phase executor, or nil if not found.
//	bool - True if a phase was found.
//
// Thread Safety: This method is safe for concurrent use.
func (r *DefaultPhaseRegistry) GetPhase(state AgentState) (PhaseExecutor, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	executor, ok := r.phases[state]
	return executor, ok
}

// MustGetPhase returns the phase for a state or panics.
//
// Description:
//
//	Like GetPhase but panics if no phase is registered. Use only when
//	you know the phase must exist.
//
// Inputs:
//
//	state - The state to get the phase for.
//
// Outputs:
//
//	PhaseExecutor - The phase executor.
//
// Thread Safety: This method is safe for concurrent use.
func (r *DefaultPhaseRegistry) MustGetPhase(state AgentState) PhaseExecutor {
	phase, ok := r.GetPhase(state)
	if !ok {
		panic(fmt.Sprintf("no phase registered for state %s", state))
	}
	return phase
}

// States returns all registered states in sorted order.
//
// Description:
//
//	Returns a list of all states that have registered phases.
//	The list is sorted for deterministic output.
//
// Outputs:
//
//	[]AgentState - All registered states, sorted.
//
// Thread Safety: This method is safe for concurrent use.
func (r *DefaultPhaseRegistry) States() []AgentState {
	r.mu.RLock()
	defer r.mu.RUnlock()

	states := make([]AgentState, 0, len(r.phases))
	for state := range r.phases {
		states = append(states, state)
	}

	sort.Slice(states, func(i, j int) bool {
		return string(states[i]) < string(states[j])
	})

	return states
}

// Count returns the number of registered phases.
//
// Outputs:
//
//	int - The number of registered phases.
//
// Thread Safety: This method is safe for concurrent use.
func (r *DefaultPhaseRegistry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.phases)
}
