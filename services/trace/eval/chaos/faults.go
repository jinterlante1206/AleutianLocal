// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package chaos

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

// -----------------------------------------------------------------------------
// Errors
// -----------------------------------------------------------------------------

var (
	// ErrFaultActive indicates a fault is already active.
	ErrFaultActive = errors.New("fault is already active")

	// ErrFaultInactive indicates a fault is not active.
	ErrFaultInactive = errors.New("fault is not active")

	// ErrChaosInjected is the default error returned by ErrorFault.
	ErrChaosInjected = errors.New("chaos: injected error")
)

// -----------------------------------------------------------------------------
// Fault Interface
// -----------------------------------------------------------------------------

// Fault represents an injectable failure condition.
//
// Thread Safety: Implementations must be safe for concurrent use.
type Fault interface {
	// Name returns a unique identifier for this fault.
	Name() string

	// Description returns a human-readable description.
	Description() string

	// Inject activates the fault.
	// Returns error if fault cannot be activated.
	Inject(ctx context.Context) error

	// Revert deactivates the fault.
	// Returns error if fault cannot be reverted.
	Revert(ctx context.Context) error

	// IsActive returns true if the fault is currently active.
	IsActive() bool

	// Apply applies the fault effect to an operation.
	// Returns the (possibly modified) error from the operation.
	Apply(ctx context.Context, originalErr error) error
}

// -----------------------------------------------------------------------------
// Latency Fault
// -----------------------------------------------------------------------------

// LatencyFault injects artificial latency.
//
// Description:
//
//	LatencyFault adds random delays to operations within a configured range.
//	Use this to test timeout handling and latency sensitivity.
//
// Thread Safety: Safe for concurrent use.
type LatencyFault struct {
	name     string
	minDelay time.Duration
	maxDelay time.Duration
	active   atomic.Bool
	mu       sync.RWMutex
	seed     uint64
}

// NewLatencyFault creates a latency fault with the given delay range.
//
// Inputs:
//   - minDelay: Minimum delay to inject.
//   - maxDelay: Maximum delay to inject.
//
// Outputs:
//   - *LatencyFault: The new fault. Never nil.
func NewLatencyFault(minDelay, maxDelay time.Duration) *LatencyFault {
	if minDelay > maxDelay {
		minDelay, maxDelay = maxDelay, minDelay
	}
	return &LatencyFault{
		name:     "latency",
		minDelay: minDelay,
		maxDelay: maxDelay,
		seed:     uint64(time.Now().UnixNano()),
	}
}

// Name implements Fault.
func (f *LatencyFault) Name() string { return f.name }

// Description implements Fault.
func (f *LatencyFault) Description() string {
	return "Injects random latency between " + f.minDelay.String() + " and " + f.maxDelay.String()
}

// Inject implements Fault.
func (f *LatencyFault) Inject(_ context.Context) error {
	if !f.active.CompareAndSwap(false, true) {
		return ErrFaultActive
	}
	return nil
}

// Revert implements Fault.
func (f *LatencyFault) Revert(_ context.Context) error {
	if !f.active.CompareAndSwap(true, false) {
		return ErrFaultInactive
	}
	return nil
}

// IsActive implements Fault.
func (f *LatencyFault) IsActive() bool {
	return f.active.Load()
}

// Apply implements Fault.
func (f *LatencyFault) Apply(ctx context.Context, originalErr error) error {
	if !f.IsActive() {
		return originalErr
	}

	delay := f.randomDelay()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(delay):
	}

	return originalErr
}

// randomDelay returns a random delay in the configured range.
func (f *LatencyFault) randomDelay() time.Duration {
	f.mu.Lock()
	f.seed = f.seed*6364136223846793005 + 1442695040888963407
	seed := f.seed
	f.mu.Unlock()

	rangeNs := uint64(f.maxDelay - f.minDelay)
	if rangeNs == 0 {
		return f.minDelay
	}

	delayNs := seed % rangeNs
	return f.minDelay + time.Duration(delayNs)
}

// -----------------------------------------------------------------------------
// Error Fault
// -----------------------------------------------------------------------------

// ErrorFault injects errors at a configurable rate.
//
// Description:
//
//	ErrorFault returns an error for a percentage of operations.
//	Use this to test error handling and retry logic.
//
// Thread Safety: Safe for concurrent use.
type ErrorFault struct {
	name     string
	rate     float64
	err      error
	active   atomic.Bool
	mu       sync.RWMutex
	seed     uint64
	injected atomic.Int64
	total    atomic.Int64
}

// NewErrorFault creates an error fault with the given rate.
//
// Inputs:
//   - rate: Probability of error (0.0 to 1.0).
//   - err: Error to return. If nil, uses ErrChaosInjected.
//
// Outputs:
//   - *ErrorFault: The new fault. Never nil.
func NewErrorFault(rate float64, err error) *ErrorFault {
	if rate < 0 {
		rate = 0
	}
	if rate > 1 {
		rate = 1
	}
	if err == nil {
		err = ErrChaosInjected
	}
	return &ErrorFault{
		name: "error",
		rate: rate,
		err:  err,
		seed: uint64(time.Now().UnixNano()),
	}
}

// Name implements Fault.
func (f *ErrorFault) Name() string { return f.name }

// Description implements Fault.
func (f *ErrorFault) Description() string {
	return "Injects errors at " + formatPercent(f.rate) + " rate"
}

// Inject implements Fault.
func (f *ErrorFault) Inject(_ context.Context) error {
	if !f.active.CompareAndSwap(false, true) {
		return ErrFaultActive
	}
	return nil
}

// Revert implements Fault.
func (f *ErrorFault) Revert(_ context.Context) error {
	if !f.active.CompareAndSwap(true, false) {
		return ErrFaultInactive
	}
	return nil
}

// IsActive implements Fault.
func (f *ErrorFault) IsActive() bool {
	return f.active.Load()
}

// Apply implements Fault.
func (f *ErrorFault) Apply(_ context.Context, originalErr error) error {
	if !f.IsActive() {
		return originalErr
	}

	f.total.Add(1)

	if f.shouldInject() {
		f.injected.Add(1)
		return f.err
	}

	return originalErr
}

// shouldInject returns true if an error should be injected.
func (f *ErrorFault) shouldInject() bool {
	f.mu.Lock()
	f.seed = f.seed*6364136223846793005 + 1442695040888963407
	seed := f.seed
	f.mu.Unlock()

	return float64(seed%1000000)/1000000 < f.rate
}

// Stats returns injection statistics.
func (f *ErrorFault) Stats() (injected, total int64) {
	return f.injected.Load(), f.total.Load()
}

// -----------------------------------------------------------------------------
// Panic Fault
// -----------------------------------------------------------------------------

// PanicFault triggers panics at a configurable rate.
//
// Description:
//
//	PanicFault causes panics for testing recovery mechanisms.
//	Use with caution as unrecovered panics will crash the process.
//
// Thread Safety: Safe for concurrent use.
type PanicFault struct {
	name    string
	rate    float64
	message string
	active  atomic.Bool
	mu      sync.RWMutex
	seed    uint64
}

// NewPanicFault creates a panic fault with the given rate.
//
// Inputs:
//   - rate: Probability of panic (0.0 to 1.0).
//   - message: Panic message. If empty, uses "chaos: injected panic".
//
// Outputs:
//   - *PanicFault: The new fault. Never nil.
func NewPanicFault(rate float64, message string) *PanicFault {
	if rate < 0 {
		rate = 0
	}
	if rate > 1 {
		rate = 1
	}
	if message == "" {
		message = "chaos: injected panic"
	}
	return &PanicFault{
		name:    "panic",
		rate:    rate,
		message: message,
		seed:    uint64(time.Now().UnixNano()),
	}
}

// Name implements Fault.
func (f *PanicFault) Name() string { return f.name }

// Description implements Fault.
func (f *PanicFault) Description() string {
	return "Triggers panic at " + formatPercent(f.rate) + " rate"
}

// Inject implements Fault.
func (f *PanicFault) Inject(_ context.Context) error {
	if !f.active.CompareAndSwap(false, true) {
		return ErrFaultActive
	}
	return nil
}

// Revert implements Fault.
func (f *PanicFault) Revert(_ context.Context) error {
	if !f.active.CompareAndSwap(true, false) {
		return ErrFaultInactive
	}
	return nil
}

// IsActive implements Fault.
func (f *PanicFault) IsActive() bool {
	return f.active.Load()
}

// Apply implements Fault.
func (f *PanicFault) Apply(_ context.Context, originalErr error) error {
	if !f.IsActive() {
		return originalErr
	}

	if f.shouldPanic() {
		panic(f.message)
	}

	return originalErr
}

// shouldPanic returns true if a panic should be triggered.
func (f *PanicFault) shouldPanic() bool {
	f.mu.Lock()
	f.seed = f.seed*6364136223846793005 + 1442695040888963407
	seed := f.seed
	f.mu.Unlock()

	return float64(seed%1000000)/1000000 < f.rate
}

// -----------------------------------------------------------------------------
// Timeout Fault
// -----------------------------------------------------------------------------

// TimeoutFault forces context deadline exceeded errors.
//
// Description:
//
//	TimeoutFault cancels operations early to simulate timeouts.
//	Use this to test timeout handling.
//
// Thread Safety: Safe for concurrent use.
type TimeoutFault struct {
	name    string
	rate    float64
	timeout time.Duration
	active  atomic.Bool
	mu      sync.RWMutex
	seed    uint64
}

// NewTimeoutFault creates a timeout fault.
//
// Inputs:
//   - rate: Probability of timeout (0.0 to 1.0).
//   - timeout: Duration after which to timeout (0 means immediate).
//
// Outputs:
//   - *TimeoutFault: The new fault. Never nil.
func NewTimeoutFault(rate float64, timeout time.Duration) *TimeoutFault {
	if rate < 0 {
		rate = 0
	}
	if rate > 1 {
		rate = 1
	}
	return &TimeoutFault{
		name:    "timeout",
		rate:    rate,
		timeout: timeout,
		seed:    uint64(time.Now().UnixNano()),
	}
}

// Name implements Fault.
func (f *TimeoutFault) Name() string { return f.name }

// Description implements Fault.
func (f *TimeoutFault) Description() string {
	return "Forces timeout at " + formatPercent(f.rate) + " rate after " + f.timeout.String()
}

// Inject implements Fault.
func (f *TimeoutFault) Inject(_ context.Context) error {
	if !f.active.CompareAndSwap(false, true) {
		return ErrFaultActive
	}
	return nil
}

// Revert implements Fault.
func (f *TimeoutFault) Revert(_ context.Context) error {
	if !f.active.CompareAndSwap(true, false) {
		return ErrFaultInactive
	}
	return nil
}

// IsActive implements Fault.
func (f *TimeoutFault) IsActive() bool {
	return f.active.Load()
}

// Apply implements Fault.
func (f *TimeoutFault) Apply(ctx context.Context, originalErr error) error {
	if !f.IsActive() {
		return originalErr
	}

	if f.shouldTimeout() {
		// Wait for timeout then return deadline exceeded
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(f.timeout):
			return context.DeadlineExceeded
		}
	}

	return originalErr
}

// shouldTimeout returns true if a timeout should be injected.
func (f *TimeoutFault) shouldTimeout() bool {
	f.mu.Lock()
	f.seed = f.seed*6364136223846793005 + 1442695040888963407
	seed := f.seed
	f.mu.Unlock()

	return float64(seed%1000000)/1000000 < f.rate
}

// WrapContext returns a context that may be cancelled early if fault is active.
//
// Inputs:
//   - ctx: Original context.
//
// Outputs:
//   - context.Context: Possibly wrapped context with earlier deadline.
//   - context.CancelFunc: Cancel function (always call to avoid leaks).
func (f *TimeoutFault) WrapContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if !f.IsActive() || !f.shouldTimeout() {
		ctx, cancel := context.WithCancel(ctx)
		return ctx, cancel
	}

	return context.WithTimeout(ctx, f.timeout)
}

// -----------------------------------------------------------------------------
// Composite Fault
// -----------------------------------------------------------------------------

// CompositeFault combines multiple faults.
//
// Description:
//
//	CompositeFault applies multiple faults in sequence.
//	All faults are injected/reverted together.
//
// Thread Safety: Safe for concurrent use.
type CompositeFault struct {
	name   string
	faults []Fault
	active atomic.Bool
}

// NewCompositeFault creates a composite fault.
//
// Inputs:
//   - name: Name for the composite fault.
//   - faults: Faults to combine.
//
// Outputs:
//   - *CompositeFault: The new fault. Never nil.
func NewCompositeFault(name string, faults ...Fault) *CompositeFault {
	return &CompositeFault{
		name:   name,
		faults: faults,
	}
}

// Name implements Fault.
func (f *CompositeFault) Name() string { return f.name }

// Description implements Fault.
func (f *CompositeFault) Description() string {
	return "Combines " + formatInt(len(f.faults)) + " faults"
}

// Inject implements Fault.
func (f *CompositeFault) Inject(ctx context.Context) error {
	if !f.active.CompareAndSwap(false, true) {
		return ErrFaultActive
	}

	for i, fault := range f.faults {
		if err := fault.Inject(ctx); err != nil {
			// Revert already-injected faults
			for j := i - 1; j >= 0; j-- {
				f.faults[j].Revert(ctx)
			}
			f.active.Store(false)
			return err
		}
	}
	return nil
}

// Revert implements Fault.
func (f *CompositeFault) Revert(ctx context.Context) error {
	if !f.active.CompareAndSwap(true, false) {
		return ErrFaultInactive
	}

	var lastErr error
	// Revert in reverse order
	for i := len(f.faults) - 1; i >= 0; i-- {
		if err := f.faults[i].Revert(ctx); err != nil {
			lastErr = err
		}
	}
	return lastErr
}

// IsActive implements Fault.
func (f *CompositeFault) IsActive() bool {
	return f.active.Load()
}

// Apply implements Fault.
func (f *CompositeFault) Apply(ctx context.Context, originalErr error) error {
	if !f.IsActive() {
		return originalErr
	}

	err := originalErr
	for _, fault := range f.faults {
		err = fault.Apply(ctx, err)
	}
	return err
}

// -----------------------------------------------------------------------------
// Helpers
// -----------------------------------------------------------------------------

func formatPercent(rate float64) string {
	return formatFloat(rate*100) + "%"
}

func formatFloat(f float64) string {
	s := ""
	i := int64(f * 100)
	s = formatInt(int(i/100)) + "." + formatInt(int(i%100))
	return s
}

func formatInt(n int) string {
	if n == 0 {
		return "0"
	}
	if n < 0 {
		return "-" + formatInt(-n)
	}

	digits := make([]byte, 0, 20)
	for n > 0 {
		digits = append(digits, byte('0'+n%10))
		n /= 10
	}

	// Reverse
	for i, j := 0, len(digits)-1; i < j; i, j = i+1, j-1 {
		digits[i], digits[j] = digits[j], digits[i]
	}

	return string(digits)
}
