// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package correctness

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/eval"
)

// -----------------------------------------------------------------------------
// Errors
// -----------------------------------------------------------------------------

var (
	// ErrVerificationFailed indicates that one or more properties failed.
	ErrVerificationFailed = errors.New("verification failed")

	// ErrNoProperties indicates that the component has no properties to verify.
	ErrNoProperties = errors.New("component has no properties")

	// ErrNoGenerator indicates that a property has no generator and no input was provided.
	ErrNoGenerator = errors.New("property has no generator")
)

// -----------------------------------------------------------------------------
// Verifier Options
// -----------------------------------------------------------------------------

// VerifyOption configures verification behavior.
type VerifyOption func(*verifyConfig)

type verifyConfig struct {
	iterations       int
	timeout          time.Duration
	propertyTimeout  time.Duration
	parallelism      int
	stopOnFailure    bool
	tags             []string
	logger           *slog.Logger
	shrinkIterations int
}

func defaultConfig() *verifyConfig {
	return &verifyConfig{
		iterations:       100,
		timeout:          5 * time.Minute,
		propertyTimeout:  30 * time.Second,
		parallelism:      1,
		stopOnFailure:    false,
		shrinkIterations: 100,
	}
}

// WithIterations sets the number of test iterations per property.
// Default is 100.
func WithIterations(n int) VerifyOption {
	return func(c *verifyConfig) {
		if n > 0 {
			c.iterations = n
		}
	}
}

// WithTimeout sets the total verification timeout.
// Default is 5 minutes.
func WithTimeout(d time.Duration) VerifyOption {
	return func(c *verifyConfig) {
		if d > 0 {
			c.timeout = d
		}
	}
}

// WithPropertyTimeout sets the timeout per property.
// Default is 30 seconds.
func WithPropertyTimeout(d time.Duration) VerifyOption {
	return func(c *verifyConfig) {
		if d > 0 {
			c.propertyTimeout = d
		}
	}
}

// WithParallelism sets the number of properties to verify in parallel.
// Default is 1 (sequential).
func WithParallelism(n int) VerifyOption {
	return func(c *verifyConfig) {
		if n > 0 {
			c.parallelism = n
		}
	}
}

// WithStopOnFailure causes verification to stop at the first failure.
// Default is false (verify all properties).
func WithStopOnFailure(stop bool) VerifyOption {
	return func(c *verifyConfig) {
		c.stopOnFailure = stop
	}
}

// WithTags filters properties to only those with specified tags.
// If empty, all properties are verified.
func WithTags(tags ...string) VerifyOption {
	return func(c *verifyConfig) {
		c.tags = tags
	}
}

// WithLogger sets the logger for verification progress.
func WithLogger(logger *slog.Logger) VerifyOption {
	return func(c *verifyConfig) {
		c.logger = logger
	}
}

// WithShrinkIterations sets the maximum shrink iterations when a failure is found.
// Default is 100.
func WithShrinkIterations(n int) VerifyOption {
	return func(c *verifyConfig) {
		if n >= 0 {
			c.shrinkIterations = n
		}
	}
}

// -----------------------------------------------------------------------------
// Verifier
// -----------------------------------------------------------------------------

// Verifier runs property-based tests against evaluable components.
//
// Description:
//
//	The Verifier is the core of the correctness verification framework.
//	It generates random inputs, runs them through component properties,
//	and reports failures with minimal counterexamples.
//
// Thread Safety: Safe for concurrent use.
type Verifier struct {
	registry *eval.Registry
	mu       sync.RWMutex
	logger   *slog.Logger
}

// NewVerifier creates a new Verifier.
//
// Inputs:
//   - registry: The registry of evaluable components. Must not be nil.
//
// Outputs:
//   - *Verifier: The new verifier. Never nil.
//
// Example:
//
//	verifier := correctness.NewVerifier(registry)
//	result, err := verifier.Verify(ctx, "cdcl")
func NewVerifier(registry *eval.Registry) *Verifier {
	return &Verifier{
		registry: registry,
		logger:   slog.Default(),
	}
}

// SetLogger sets the logger for the verifier.
//
// Thread Safety: Safe for concurrent use.
func (v *Verifier) SetLogger(logger *slog.Logger) {
	if logger == nil {
		return
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	v.logger = logger
}

// Verify runs all property tests for a component.
//
// Description:
//
//	For each property, generates random inputs using the property's Generator,
//	runs the Check function, and reports any failures. If a failure is found
//	and the property has a Shrink function, attempts to find a minimal
//	counterexample.
//
// Inputs:
//   - ctx: Context for cancellation. Must not be nil.
//   - name: The name of the component to verify.
//   - opts: Optional configuration options.
//
// Outputs:
//   - *eval.VerifyResult: The verification results.
//   - error: Non-nil if verification could not be performed (not if properties fail).
//
// Example:
//
//	result, err := verifier.Verify(ctx, "cdcl",
//	    correctness.WithIterations(10000),
//	    correctness.WithStopOnFailure(true),
//	)
//	if err != nil {
//	    log.Fatalf("Verification error: %v", err)
//	}
//	if !result.Passed {
//	    for _, pr := range result.FailedProperties() {
//	        log.Printf("Property %s failed: %v", pr.Name, pr.Error)
//	    }
//	}
func (v *Verifier) Verify(ctx context.Context, name string, opts ...VerifyOption) (*eval.VerifyResult, error) {
	if ctx == nil {
		return nil, errors.New("context must not be nil")
	}

	component, ok := v.registry.Get(name)
	if !ok {
		return nil, fmt.Errorf("%w: %s", eval.ErrNotFound, name)
	}

	config := defaultConfig()
	for _, opt := range opts {
		opt(config)
	}

	// Get logger safely - prefer config logger, fall back to verifier's logger
	var logger *slog.Logger
	if config.logger != nil {
		logger = config.logger
	} else {
		v.mu.RLock()
		logger = v.logger
		v.mu.RUnlock()
	}

	logger.Debug("starting verification",
		slog.String("component", name),
		slog.Int("iterations", config.iterations),
	)

	properties := component.Properties()
	if len(properties) == 0 {
		return nil, fmt.Errorf("%w: %s", ErrNoProperties, name)
	}

	// Filter by tags if specified
	if len(config.tags) > 0 {
		properties = filterByTags(properties, config.tags)
		if len(properties) == 0 {
			return nil, fmt.Errorf("%w: no properties match tags %v", ErrNoProperties, config.tags)
		}
	}

	// Create timeout context
	ctx, cancel := context.WithTimeout(ctx, config.timeout)
	defer cancel()

	start := time.Now()

	result := &eval.VerifyResult{
		Component:  name,
		Properties: make([]eval.PropertyResult, 0, len(properties)),
		Passed:     true,
	}

	// Run properties
	if config.parallelism <= 1 {
		// Sequential verification
		for _, prop := range properties {
			select {
			case <-ctx.Done():
				result.Duration = time.Since(start)
				return result, ctx.Err()
			default:
			}

			pr := v.verifyProperty(ctx, prop, config)
			result.Properties = append(result.Properties, pr)
			result.Iterations += pr.Iterations

			if !pr.Passed {
				result.Passed = false
				if config.stopOnFailure {
					break
				}
			}
		}
	} else {
		// Parallel verification
		result.Properties = v.verifyPropertiesParallel(ctx, properties, config)
		for _, pr := range result.Properties {
			result.Iterations += pr.Iterations
			if !pr.Passed {
				result.Passed = false
			}
		}
	}

	result.Duration = time.Since(start)
	return result, nil
}

// VerifyAll runs property tests for all registered components.
//
// Inputs:
//   - ctx: Context for cancellation. Must not be nil.
//   - opts: Optional configuration options (applied to each component).
//
// Outputs:
//   - []*eval.VerifyResult: Results for all components.
//   - error: Non-nil only if verification could not be started.
//
// Example:
//
//	results, err := verifier.VerifyAll(ctx, correctness.WithIterations(1000))
func (v *Verifier) VerifyAll(ctx context.Context, opts ...VerifyOption) ([]*eval.VerifyResult, error) {
	if ctx == nil {
		return nil, errors.New("context must not be nil")
	}

	// Get logger safely
	v.mu.RLock()
	logger := v.logger
	v.mu.RUnlock()

	names := v.registry.List()
	results := make([]*eval.VerifyResult, 0, len(names))

	for _, name := range names {
		select {
		case <-ctx.Done():
			return results, ctx.Err()
		default:
		}

		// Skip components without properties
		component, _ := v.registry.Get(name)
		if len(component.Properties()) == 0 {
			continue
		}

		result, err := v.Verify(ctx, name, opts...)
		if err != nil {
			// Log but continue with other components
			logger.Warn("verification failed",
				slog.String("component", name),
				slog.String("error", err.Error()),
			)
			continue
		}

		results = append(results, result)
	}

	return results, nil
}

// verifyProperty verifies a single property.
func (v *Verifier) verifyProperty(ctx context.Context, prop eval.Property, config *verifyConfig) eval.PropertyResult {
	start := time.Now()

	result := eval.PropertyResult{
		Name:   prop.Name,
		Passed: true,
	}

	// Validate property
	if err := prop.Validate(); err != nil {
		result.Passed = false
		result.Error = err
		result.Duration = time.Since(start)
		return result
	}

	// Check if property has a generator
	if prop.Generator == nil {
		result.Passed = false
		result.Error = fmt.Errorf("%w: %s", ErrNoGenerator, prop.Name)
		result.Duration = time.Since(start)
		return result
	}

	// Determine timeout
	timeout := config.propertyTimeout
	if prop.Timeout > 0 {
		timeout = prop.Timeout
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Run iterations
	for i := 0; i < config.iterations; i++ {
		select {
		case <-ctx.Done():
			result.Iterations = i
			result.Duration = time.Since(start)
			if result.Error == nil && ctx.Err() != nil {
				result.Error = ctx.Err()
				result.Passed = false
			}
			return result
		default:
		}

		// Generate input
		input := prop.Generator()

		// For this framework, we don't have a component.Process() to call
		// The Check function receives input and output (which may be the same
		// or the output may be derived from the input by a test harness)
		// For pure property testing, we pass nil as output
		output := input // In a real scenario, this would be component.Process(input)

		// Check property
		if err := prop.Check(input, output); err != nil {
			result.Passed = false
			result.FailingInput = input
			result.FailingOutput = output
			result.Error = err
			result.Iterations = i + 1

			// Try to shrink
			if prop.Shrink != nil && config.shrinkIterations > 0 {
				shrunkInput, shrinkSteps := v.shrinkInput(ctx, prop, input, output, config.shrinkIterations)
				if shrunkInput != nil {
					result.FailingInput = shrunkInput
					result.ShrinkSteps = shrinkSteps
				}
			}

			result.Duration = time.Since(start)
			return result
		}

		result.Iterations = i + 1
	}

	result.Duration = time.Since(start)
	return result
}

// shrinkInput attempts to find a minimal failing input.
func (v *Verifier) shrinkInput(ctx context.Context, prop eval.Property, input, output any, maxIterations int) (any, int) {
	if prop.Shrink == nil {
		return nil, 0
	}

	current := input
	steps := 0

	for i := 0; i < maxIterations; i++ {
		select {
		case <-ctx.Done():
			return current, steps
		default:
		}

		candidates := prop.Shrink(current)
		if len(candidates) == 0 {
			break
		}

		foundSmaller := false
		for _, candidate := range candidates {
			// Check if this smaller input still fails
			if err := prop.Check(candidate, candidate); err != nil {
				current = candidate
				steps++
				foundSmaller = true
				break
			}
		}

		if !foundSmaller {
			break
		}
	}

	return current, steps
}

// verifyPropertiesParallel verifies properties in parallel.
func (v *Verifier) verifyPropertiesParallel(ctx context.Context, properties []eval.Property, config *verifyConfig) []eval.PropertyResult {
	results := make([]eval.PropertyResult, len(properties))
	resultsCh := make(chan struct {
		index  int
		result eval.PropertyResult
	}, len(properties))

	// Semaphore for parallelism control
	sem := make(chan struct{}, config.parallelism)

	var wg sync.WaitGroup
	for i, prop := range properties {
		wg.Add(1)
		go func(index int, prop eval.Property) {
			defer wg.Done()

			// Acquire semaphore
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				resultsCh <- struct {
					index  int
					result eval.PropertyResult
				}{
					index: index,
					result: eval.PropertyResult{
						Name:   prop.Name,
						Passed: false,
						Error:  ctx.Err(),
					},
				}
				return
			}

			result := v.verifyProperty(ctx, prop, config)
			resultsCh <- struct {
				index  int
				result eval.PropertyResult
			}{index: index, result: result}
		}(i, prop)
	}

	// Wait and close
	go func() {
		wg.Wait()
		close(resultsCh)
	}()

	// Collect results
	for r := range resultsCh {
		results[r.index] = r.result
	}

	return results
}

// filterByTags filters properties to only those with at least one specified tag.
func filterByTags(properties []eval.Property, tags []string) []eval.Property {
	var filtered []eval.Property
	for _, prop := range properties {
		for _, tag := range tags {
			if prop.HasTag(tag) {
				filtered = append(filtered, prop)
				break
			}
		}
	}
	return filtered
}

// -----------------------------------------------------------------------------
// Verification Runner
// -----------------------------------------------------------------------------

// Runner provides a high-level interface for running verification.
//
// Example:
//
//	runner := correctness.NewRunner(registry).
//	    WithIterations(10000).
//	    WithParallelism(4)
//
//	results := runner.RunAll(ctx)
type Runner struct {
	verifier *Verifier
	opts     []VerifyOption
}

// NewRunner creates a new verification runner.
func NewRunner(registry *eval.Registry) *Runner {
	return &Runner{
		verifier: NewVerifier(registry),
		opts:     make([]VerifyOption, 0),
	}
}

// WithIterations configures the number of iterations.
func (r *Runner) WithIterations(n int) *Runner {
	r.opts = append(r.opts, WithIterations(n))
	return r
}

// WithParallelism configures parallel verification.
func (r *Runner) WithParallelism(n int) *Runner {
	r.opts = append(r.opts, WithParallelism(n))
	return r
}

// WithTimeout configures the timeout.
func (r *Runner) WithTimeout(d time.Duration) *Runner {
	r.opts = append(r.opts, WithTimeout(d))
	return r
}

// WithStopOnFailure configures stop-on-failure behavior.
func (r *Runner) WithStopOnFailure(stop bool) *Runner {
	r.opts = append(r.opts, WithStopOnFailure(stop))
	return r
}

// WithTags filters by tags.
func (r *Runner) WithTags(tags ...string) *Runner {
	r.opts = append(r.opts, WithTags(tags...))
	return r
}

// Run verifies a single component.
func (r *Runner) Run(ctx context.Context, name string) (*eval.VerifyResult, error) {
	return r.verifier.Verify(ctx, name, r.opts...)
}

// RunAll verifies all components.
func (r *Runner) RunAll(ctx context.Context) ([]*eval.VerifyResult, error) {
	return r.verifier.VerifyAll(ctx, r.opts...)
}
