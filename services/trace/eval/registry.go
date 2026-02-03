// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package eval

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"
)

// Registry manages all evaluable components in the system.
//
// Description:
//
//	The Registry provides a central location for registering and looking up
//	evaluable components. It supports concurrent access and provides methods
//	for batch operations like health checks.
//
// Thread Safety: Safe for concurrent use via read-write mutex.
type Registry struct {
	mu         sync.RWMutex
	components map[string]Evaluable
	hooks      []RegistrationHook
}

// RegistrationHook is called when a component is registered or unregistered.
type RegistrationHook func(name string, component Evaluable, registered bool)

// NewRegistry creates a new empty registry.
//
// Outputs:
//   - *Registry: The new registry. Never nil.
//
// Example:
//
//	registry := eval.NewRegistry()
//	registry.Register(myAlgorithm)
func NewRegistry() *Registry {
	return &Registry{
		components: make(map[string]Evaluable),
		hooks:      make([]RegistrationHook, 0),
	}
}

// Register adds a component to the registry.
//
// Description:
//
//	Registers the component under its Name(). The name must be unique
//	within the registry.
//
// Inputs:
//   - component: The evaluable component to register. Must not be nil.
//
// Outputs:
//   - error: nil on success, ErrNilComponent if component is nil,
//     ErrAlreadyRegistered if name is already taken.
//
// Thread Safety: Safe for concurrent use.
//
// Example:
//
//	err := registry.Register(myAlgorithm)
//	if err != nil {
//	    log.Fatalf("Failed to register: %v", err)
//	}
func (r *Registry) Register(component Evaluable) error {
	if component == nil {
		return ErrNilComponent
	}

	name := component.Name()

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.components[name]; exists {
		return fmt.Errorf("%w: %s", ErrAlreadyRegistered, name)
	}

	r.components[name] = component

	// Notify hooks
	for _, hook := range r.hooks {
		hook(name, component, true)
	}

	return nil
}

// MustRegister registers a component and panics on error.
//
// Description:
//
//	Convenience method for registration during initialization.
//	Should only be used during startup, not at runtime.
//
// Inputs:
//   - component: The evaluable component to register. Must not be nil.
//
// Thread Safety: Safe for concurrent use.
//
// Example:
//
//	func init() {
//	    DefaultRegistry.MustRegister(myAlgorithm)
//	}
func (r *Registry) MustRegister(component Evaluable) {
	if err := r.Register(component); err != nil {
		panic(fmt.Sprintf("eval: failed to register %v: %v", component.Name(), err))
	}
}

// Unregister removes a component from the registry.
//
// Description:
//
//	Removes the component with the given name. Returns an error if
//	the component is not found.
//
// Inputs:
//   - name: The name of the component to unregister.
//
// Outputs:
//   - error: nil on success, ErrNotFound if not registered.
//
// Thread Safety: Safe for concurrent use.
func (r *Registry) Unregister(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	component, exists := r.components[name]
	if !exists {
		return fmt.Errorf("%w: %s", ErrNotFound, name)
	}

	delete(r.components, name)

	// Notify hooks
	for _, hook := range r.hooks {
		hook(name, component, false)
	}

	return nil
}

// Get retrieves a component by name.
//
// Inputs:
//   - name: The name of the component to retrieve.
//
// Outputs:
//   - Evaluable: The component, or nil if not found.
//   - bool: true if found, false otherwise.
//
// Thread Safety: Safe for concurrent use.
//
// Example:
//
//	if component, ok := registry.Get("cdcl"); ok {
//	    // Use component
//	}
func (r *Registry) Get(name string) (Evaluable, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	component, exists := r.components[name]
	return component, exists
}

// MustGet retrieves a component by name, panicking if not found.
//
// Inputs:
//   - name: The name of the component to retrieve.
//
// Outputs:
//   - Evaluable: The component. Panics if not found.
//
// Thread Safety: Safe for concurrent use.
func (r *Registry) MustGet(name string) Evaluable {
	component, ok := r.Get(name)
	if !ok {
		panic(fmt.Sprintf("eval: component not found: %s", name))
	}
	return component
}

// List returns all registered component names.
//
// Outputs:
//   - []string: Sorted list of component names.
//
// Thread Safety: Safe for concurrent use.
func (r *Registry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.components))
	for name := range r.components {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// All returns all registered components.
//
// Outputs:
//   - map[string]Evaluable: Copy of the components map.
//
// Thread Safety: Safe for concurrent use.
func (r *Registry) All() map[string]Evaluable {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make(map[string]Evaluable, len(r.components))
	for name, component := range r.components {
		result[name] = component
	}
	return result
}

// Count returns the number of registered components.
//
// Outputs:
//   - int: Number of components.
//
// Thread Safety: Safe for concurrent use.
func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.components)
}

// Clear removes all components from the registry.
//
// Thread Safety: Safe for concurrent use.
func (r *Registry) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Notify hooks for each component
	for name, component := range r.components {
		for _, hook := range r.hooks {
			hook(name, component, false)
		}
	}

	r.components = make(map[string]Evaluable)
}

// AddHook adds a registration hook.
//
// Description:
//
//	Hooks are called when components are registered or unregistered.
//	They receive the component name, the component, and a boolean
//	indicating whether it was registered (true) or unregistered (false).
//
// Inputs:
//   - hook: The hook function to add.
//
// Thread Safety: Safe for concurrent use.
func (r *Registry) AddHook(hook RegistrationHook) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.hooks = append(r.hooks, hook)
}

// HealthCheckAll runs health checks on all registered components.
//
// Description:
//
//	Runs health checks concurrently with the given concurrency limit.
//	Returns results for all components, including those that fail.
//
// Inputs:
//   - ctx: Context for cancellation. Must not be nil.
//   - concurrency: Maximum number of concurrent health checks. If <= 0, defaults to 10.
//
// Outputs:
//   - []HealthResult: Results for all components.
//
// Thread Safety: Safe for concurrent use.
//
// Example:
//
//	results := registry.HealthCheckAll(ctx, 5)
//	for _, result := range results {
//	    if result.Status != eval.HealthHealthy {
//	        log.Printf("Unhealthy: %s - %s", result.Component, result.Message)
//	    }
//	}
func (r *Registry) HealthCheckAll(ctx context.Context, concurrency int) []HealthResult {
	if concurrency <= 0 {
		concurrency = 10
	}

	components := r.All()
	results := make([]HealthResult, 0, len(components))
	resultsCh := make(chan HealthResult, len(components))

	// Semaphore for concurrency control
	sem := make(chan struct{}, concurrency)

	var wg sync.WaitGroup
	for name, component := range components {
		wg.Add(1)
		go func(name string, component Evaluable) {
			defer wg.Done()

			// Acquire semaphore
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				resultsCh <- HealthResult{
					Component: name,
					Status:    HealthUnknown,
					Message:   "context cancelled",
					Timestamp: time.Now(),
				}
				return
			}

			// Run health check
			start := time.Now()
			err := component.HealthCheck(ctx)
			duration := time.Since(start)

			result := HealthResult{
				Component: name,
				Duration:  duration,
				Timestamp: time.Now(),
			}

			if err != nil {
				result.Status = HealthUnhealthy
				result.Message = err.Error()
			} else {
				result.Status = HealthHealthy
				result.Message = "OK"
			}

			resultsCh <- result
		}(name, component)
	}

	// Wait for all health checks to complete
	go func() {
		wg.Wait()
		close(resultsCh)
	}()

	// Collect results
	for result := range resultsCh {
		results = append(results, result)
	}

	// Sort by component name for deterministic output
	sort.Slice(results, func(i, j int) bool {
		return results[i].Component < results[j].Component
	})

	return results
}

// GetAllProperties returns all properties from all registered components.
//
// Outputs:
//   - map[string][]Property: Map from component name to its properties.
//
// Thread Safety: Safe for concurrent use.
func (r *Registry) GetAllProperties() map[string][]Property {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make(map[string][]Property)
	for name, component := range r.components {
		props := component.Properties()
		if len(props) > 0 {
			result[name] = props
		}
	}
	return result
}

// GetAllMetrics returns all metric definitions from all registered components.
//
// Outputs:
//   - map[string][]MetricDefinition: Map from component name to its metrics.
//
// Thread Safety: Safe for concurrent use.
func (r *Registry) GetAllMetrics() map[string][]MetricDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make(map[string][]MetricDefinition)
	for name, component := range r.components {
		metrics := component.Metrics()
		if len(metrics) > 0 {
			result[name] = metrics
		}
	}
	return result
}

// FindByTag finds all components that have a property with the given tag.
//
// Inputs:
//   - tag: The tag to search for.
//
// Outputs:
//   - []string: Names of components with properties having the tag.
//
// Thread Safety: Safe for concurrent use.
func (r *Registry) FindByTag(tag string) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var names []string
	for name, component := range r.components {
		for _, prop := range component.Properties() {
			if prop.HasTag(tag) {
				names = append(names, name)
				break
			}
		}
	}
	sort.Strings(names)
	return names
}

// -----------------------------------------------------------------------------
// Default Registry
// -----------------------------------------------------------------------------

// DefaultRegistry is the global registry instance.
// Components can register themselves during init() using MustRegister.
var DefaultRegistry = NewRegistry()

// Register registers a component with the default registry.
func Register(component Evaluable) error {
	return DefaultRegistry.Register(component)
}

// MustRegister registers a component with the default registry, panicking on error.
func MustRegister(component Evaluable) {
	DefaultRegistry.MustRegister(component)
}

// Get retrieves a component from the default registry.
func Get(name string) (Evaluable, bool) {
	return DefaultRegistry.Get(name)
}

// List returns all component names from the default registry.
func List() []string {
	return DefaultRegistry.List()
}
