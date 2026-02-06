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
	"log/slog"
	"runtime"
	"sync"
	"time"
)

// -----------------------------------------------------------------------------
// Deadlock Detector
// -----------------------------------------------------------------------------

// DeadlockDetector monitors contexts for progress and detects deadlocks.
//
// A deadlock is detected when a context has not reported progress for
// DeadlockMultiplier * ProgressInterval.
//
// Thread Safety: Safe for concurrent use.
type DeadlockDetector struct {
	controller    *CancellationController
	checkInterval time.Duration
	multiplier    int
	logger        *slog.Logger
}

// NewDeadlockDetector creates a new deadlock detector.
//
// Description:
//
//	Creates a deadlock detector that monitors all contexts registered with
//	the controller and cancels any that haven't reported progress.
//
// Inputs:
//   - controller: The cancellation controller to monitor.
//   - checkInterval: How often to check for deadlocks.
//   - multiplier: Deadlock threshold = multiplier * context's ProgressInterval.
//
// Outputs:
//   - *DeadlockDetector: The created detector. Never nil.
func NewDeadlockDetector(controller *CancellationController, checkInterval time.Duration, multiplier int) *DeadlockDetector {
	return &DeadlockDetector{
		controller:    controller,
		checkInterval: checkInterval,
		multiplier:    multiplier,
		logger:        controller.logger.With(slog.String("subsystem", "deadlock_detector")),
	}
}

// Run starts the deadlock detection loop.
//
// Description:
//
//	Periodically checks all contexts for progress. If a context hasn't
//	reported progress within the deadlock threshold, it is cancelled.
//
// Inputs:
//   - stopCh: Channel that signals the detector to stop.
//   - wg: WaitGroup to signal when the detector has stopped.
//
// Thread Safety: Should only be called once. Safe when called from a goroutine.
func (d *DeadlockDetector) Run(stopCh <-chan struct{}, wg *sync.WaitGroup) {
	defer wg.Done()

	ticker := time.NewTicker(d.checkInterval)
	defer ticker.Stop()

	d.logger.Debug("deadlock detector started",
		slog.Duration("check_interval", d.checkInterval),
		slog.Int("multiplier", d.multiplier),
	)

	for {
		select {
		case <-stopCh:
			d.logger.Debug("deadlock detector stopped")
			return
		case <-ticker.C:
			d.checkAllContexts()
		}
	}
}

// checkAllContexts checks all registered contexts for deadlock.
func (d *DeadlockDetector) checkAllContexts() {
	d.controller.contextsMu.RLock()
	contexts := make([]Cancellable, 0, len(d.controller.contexts))
	for _, ctx := range d.controller.contexts {
		contexts = append(contexts, ctx)
	}
	d.controller.contextsMu.RUnlock()

	now := time.Now().UnixMilli()

	for _, ctx := range contexts {
		// Skip terminal states
		if ctx.State().IsTerminal() {
			continue
		}

		// Get progress interval for this context
		progressInterval := d.controller.getProgressInterval(ctx)
		threshold := time.Duration(d.multiplier) * progressInterval

		// Check last progress
		var lastProgress int64
		switch v := ctx.(type) {
		case *SessionContext:
			lastProgress = v.LastProgress()
		case *ActivityContext:
			lastProgress = v.LastProgress()
		case *AlgorithmContext:
			lastProgress = v.LastProgress()
		default:
			continue
		}

		elapsed := time.Duration(now-lastProgress) * time.Millisecond
		if elapsed > threshold {
			d.logger.Warn("deadlock detected",
				slog.String("id", ctx.ID()),
				slog.String("level", ctx.Level().String()),
				slog.Duration("elapsed", elapsed),
				slog.Duration("threshold", threshold),
			)

			reason := CancelReason{
				Type:      CancelDeadlock,
				Message:   "No progress reported within threshold",
				Threshold: threshold.String(),
				Component: ctx.ID(),
				Timestamp: now,
			}

			ctx.Cancel(reason)

			if d.controller.metrics != nil {
				d.controller.metrics.DeadlockDetectedTotal.WithLabelValues(ctx.ID()).Inc()
			}
		}
	}
}

// -----------------------------------------------------------------------------
// Resource Monitor
// -----------------------------------------------------------------------------

// ResourceMonitor monitors resource usage and cancels contexts that exceed limits.
//
// Thread Safety: Safe for concurrent use.
type ResourceMonitor struct {
	controller *CancellationController
	logger     *slog.Logger
}

// NewResourceMonitor creates a new resource monitor.
//
// Description:
//
//	Creates a resource monitor that watches for memory, CPU, and goroutine
//	limit violations.
//
// Inputs:
//   - controller: The cancellation controller to use for cancellation.
//
// Outputs:
//   - *ResourceMonitor: The created monitor. Never nil.
func NewResourceMonitor(controller *CancellationController) *ResourceMonitor {
	return &ResourceMonitor{
		controller: controller,
		logger:     controller.logger.With(slog.String("subsystem", "resource_monitor")),
	}
}

// MonitorSession monitors a session's resource usage.
//
// Description:
//
//	Periodically checks if the session exceeds its configured resource limits.
//	If limits are exceeded, the session is cancelled.
//
// Inputs:
//   - session: The session to monitor.
//   - stopCh: Channel that signals the monitor to stop.
//   - wg: WaitGroup to signal when the monitor has stopped.
//
// Thread Safety: Should only be called once per session.
func (m *ResourceMonitor) MonitorSession(session *SessionContext, stopCh <-chan struct{}, wg *sync.WaitGroup) {
	defer wg.Done()

	limits := session.resourceLimits
	if !limits.HasLimits() {
		return
	}

	// Check interval based on controller config
	checkInterval := m.controller.config.ProgressCheckInterval
	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()

	m.logger.Debug("resource monitor started",
		slog.String("session_id", session.ID()),
		slog.Int64("max_memory_bytes", limits.MaxMemoryBytes),
		slog.Float64("max_cpu_percent", limits.MaxCPUPercent),
		slog.Int("max_goroutines", limits.MaxGoroutines),
	)

	for {
		select {
		case <-stopCh:
			m.logger.Debug("resource monitor stopped",
				slog.String("session_id", session.ID()),
			)
			return
		case <-session.Done():
			m.logger.Debug("session done, stopping resource monitor",
				slog.String("session_id", session.ID()),
			)
			return
		case <-ticker.C:
			if violation := m.checkLimits(session, limits); violation != nil {
				m.logger.Warn("resource limit exceeded",
					slog.String("session_id", session.ID()),
					slog.String("resource", violation.resource),
					slog.String("current", violation.current),
					slog.String("limit", violation.limit),
				)

				reason := CancelReason{
					Type:      CancelResourceLimit,
					Message:   violation.message,
					Threshold: violation.limit,
					Component: session.ID(),
					Timestamp: time.Now().UnixMilli(),
				}

				session.Cancel(reason)

				if m.controller.metrics != nil {
					m.controller.metrics.ResourceLimitExceededTotal.WithLabelValues(
						violation.resource,
						session.ID(),
					).Inc()
				}

				return
			}
		}
	}
}

// resourceViolation describes a resource limit violation.
type resourceViolation struct {
	resource string
	current  string
	limit    string
	message  string
}

// checkLimits checks if any resource limits are exceeded.
func (m *ResourceMonitor) checkLimits(session *SessionContext, limits ResourceLimits) *resourceViolation {
	// Check memory
	if limits.MaxMemoryBytes > 0 {
		var memStats runtime.MemStats
		runtime.ReadMemStats(&memStats)

		if int64(memStats.Alloc) > limits.MaxMemoryBytes {
			return &resourceViolation{
				resource: "memory",
				current:  formatBytes(int64(memStats.Alloc)),
				limit:    formatBytes(limits.MaxMemoryBytes),
				message:  "Memory limit exceeded",
			}
		}
	}

	// Check goroutines
	if limits.MaxGoroutines > 0 {
		numGoroutines := runtime.NumGoroutine()
		if numGoroutines > limits.MaxGoroutines {
			return &resourceViolation{
				resource: "goroutines",
				current:  formatInt(numGoroutines),
				limit:    formatInt(limits.MaxGoroutines),
				message:  "Goroutine limit exceeded",
			}
		}
	}

	// Note: CPU percentage monitoring is more complex and requires sampling
	// over time. For now, we focus on memory and goroutines which are
	// instantaneously measurable.

	return nil
}

// formatBytes formats bytes as a human-readable string.
func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return formatInt64(b) + " B"
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return formatFloat64(float64(b)/float64(div)) + " " + string("KMGTPE"[exp]) + "iB"
}

// formatInt formats an integer as a string.
func formatInt(n int) string {
	return formatInt64(int64(n))
}

// formatInt64 formats an int64 as a string.
func formatInt64(n int64) string {
	// Simple implementation without importing strconv
	if n == 0 {
		return "0"
	}

	negative := n < 0
	if negative {
		n = -n
	}

	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}

	if negative {
		digits = append([]byte{'-'}, digits...)
	}

	return string(digits)
}

// formatFloat64 formats a float64 as a string with 2 decimal places.
func formatFloat64(f float64) string {
	// Simple implementation
	intPart := int64(f)
	fracPart := int64((f - float64(intPart)) * 100)
	if fracPart < 0 {
		fracPart = -fracPart
	}

	result := formatInt64(intPart) + "."
	if fracPart < 10 {
		result += "0"
	}
	result += formatInt64(fracPart)

	return result
}

// -----------------------------------------------------------------------------
// Timeout Enforcer
// -----------------------------------------------------------------------------

// TimeoutEnforcer monitors algorithm timeouts and cancels expired algorithms.
// This is integrated into the context creation with context.WithTimeout,
// but we provide additional monitoring for algorithms that don't respect
// context cancellation.
//
// Thread Safety: Safe for concurrent use.
type TimeoutEnforcer struct {
	controller *CancellationController
	logger     *slog.Logger
}

// NewTimeoutEnforcer creates a new timeout enforcer.
func NewTimeoutEnforcer(controller *CancellationController) *TimeoutEnforcer {
	return &TimeoutEnforcer{
		controller: controller,
		logger:     controller.logger.With(slog.String("subsystem", "timeout_enforcer")),
	}
}

// EnforceTimeout monitors an algorithm and force-cancels it if the timeout
// is exceeded and the algorithm hasn't responded to context cancellation.
//
// Description:
//
//	Waits for the algorithm to complete or timeout. If the algorithm
//	doesn't respond to context cancellation within the grace period,
//	it is force-cancelled.
//
// Inputs:
//   - ctx: The algorithm context to monitor.
//   - timeout: The timeout duration.
//   - gracePeriod: How long to wait for graceful cancellation before force-kill.
//
// Thread Safety: Should only be called once per algorithm context.
func (t *TimeoutEnforcer) EnforceTimeout(ctx *AlgorithmContext, timeout time.Duration, gracePeriod time.Duration) {
	if timeout <= 0 {
		return
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		// Context completed or was cancelled
		return
	case <-timer.C:
		// Timeout expired
		t.logger.Warn("algorithm timeout",
			slog.String("id", ctx.ID()),
			slog.Duration("timeout", timeout),
		)

		// Try graceful cancellation first
		reason := CancelReason{
			Type:      CancelTimeout,
			Message:   "Algorithm timeout exceeded",
			Threshold: timeout.String(),
			Component: ctx.ID(),
			Timestamp: time.Now().UnixMilli(),
		}
		ctx.Cancel(reason)

		if t.controller.metrics != nil {
			t.controller.metrics.TimeoutTotal.WithLabelValues(ctx.ID()).Inc()
		}

		// Wait for grace period
		graceTimer := time.NewTimer(gracePeriod)
		defer graceTimer.Stop()

		select {
		case <-ctx.Done():
			// Graceful cancellation succeeded
		case <-graceTimer.C:
			// Grace period expired, force kill
			t.logger.Error("force killing algorithm after grace period",
				slog.String("id", ctx.ID()),
			)
			ctx.markCancelled()
			if t.controller.metrics != nil {
				t.controller.metrics.ForceKilledTotal.Inc()
			}
		}
	}
}
