// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package integration

import (
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/trace/agent/mcts/crs"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// -----------------------------------------------------------------------------
// Known Activity Names (for cardinality protection)
// -----------------------------------------------------------------------------

// knownActivities contains the set of valid activity names.
// Any activity name not in this set will be recorded as "unknown".
// This prevents cardinality explosion from invalid activity names.
var knownActivities = map[string]bool{
	"search":     true,
	"awareness":  true,
	"constraint": true,
	"learning":   true,
	"memory":     true,
	"planning":   true,
	"similarity": true,
	"streaming":  true,
	// Add new activities here as they are created
}

// sanitizeActivityName returns a valid activity name for metrics.
//
// Description:
//
//	Validates the activity name against the known activities set.
//	Returns "unknown" if the name is empty or not recognized.
//	This prevents cardinality explosion from invalid activity names.
//
// Inputs:
//
//	name - The activity name to validate.
//
// Outputs:
//
//	string - The sanitized activity name, or "unknown" if invalid.
//
// Thread Safety: Safe for concurrent use (read-only map access).
func sanitizeActivityName(name string) string {
	if name == "" {
		return "unknown"
	}
	if knownActivities[name] {
		return name
	}
	return "unknown"
}

// -----------------------------------------------------------------------------
// CRS Metrics - First-class observability for reasoning state
// -----------------------------------------------------------------------------

var (
	// crsActivitiesTotal counts activity executions by name and outcome.
	//
	// Labels:
	//   - activity: Activity name (sanitized against knownActivities)
	//   - status: "success", "failure", or "partial"
	crsActivitiesTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "code_buddy",
			Subsystem: "crs",
			Name:      "activities_total",
			Help:      "Total CRS activity executions by activity and status",
		},
		[]string{"activity", "status"},
	)

	// crsProofUpdatesTotal counts proof status changes.
	//
	// Labels:
	//   - status: "proven", "disproven", "expanded", or "unknown"
	crsProofUpdatesTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "code_buddy",
			Subsystem: "crs",
			Name:      "proof_updates_total",
			Help:      "Total proof status updates by new status",
		},
		[]string{"status"},
	)

	// crsConstraintsAddedTotal counts new constraints.
	//
	// Labels:
	//   - type: "mutual_exclusion", "implication", "ordering", or "resource"
	crsConstraintsAddedTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "code_buddy",
			Subsystem: "crs",
			Name:      "constraints_added_total",
			Help:      "Total constraints added by type",
		},
		[]string{"type"},
	)

	// crsDependenciesFoundTotal counts dependency edges discovered.
	crsDependenciesFoundTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: "code_buddy",
			Subsystem: "crs",
			Name:      "dependencies_found_total",
			Help:      "Total dependency edges discovered",
		},
	)

	// crsActivityDurationSeconds measures activity execution time.
	//
	// Labels:
	//   - activity: Activity name (sanitized against knownActivities)
	crsActivityDurationSeconds = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "code_buddy",
			Subsystem: "crs",
			Name:      "activity_duration_seconds",
			Help:      "Activity execution duration in seconds",
			Buckets:   []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
		},
		[]string{"activity"},
	)

	// crsStepsRecordedTotal counts trace steps recorded.
	crsStepsRecordedTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: "code_buddy",
			Subsystem: "crs",
			Name:      "steps_recorded_total",
			Help:      "Total reasoning trace steps recorded",
		},
	)

	// crsConflictsTotal counts delta application conflicts.
	crsConflictsTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: "code_buddy",
			Subsystem: "crs",
			Name:      "conflicts_total",
			Help:      "Total delta application conflicts requiring retry",
		},
	)

	// crsGenerationGauge tracks the current CRS generation.
	crsGenerationGauge = promauto.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "code_buddy",
			Subsystem: "crs",
			Name:      "generation",
			Help:      "Current CRS generation number",
		},
	)

	// crsRecordingErrorsTotal counts recording failures (DR-14).
	crsRecordingErrorsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "code_buddy",
			Subsystem: "crs",
			Name:      "recording_errors_total",
			Help:      "Total errors during trace recording by error type",
		},
		[]string{"error_type"},
	)

	// crsRecordingDurationSeconds measures recording overhead (DR-9).
	crsRecordingDurationSeconds = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Namespace: "code_buddy",
			Subsystem: "crs",
			Name:      "recording_duration_seconds",
			Help:      "Duration of trace recording operations in seconds",
			Buckets:   []float64{0.0001, 0.0005, 0.001, 0.005, 0.01, 0.05, 0.1},
		},
	)

	// --- Per-Activity Distribution Histograms (Option B) ---
	// These provide insight into "which activities produce the most changes?"

	// crsProofUpdatesPerActivity tracks proof updates distribution by activity.
	// Answers: "Does trace_flow prove more nodes than explore_file?"
	crsProofUpdatesPerActivity = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "code_buddy",
			Subsystem: "crs",
			Name:      "proof_updates_per_activity",
			Help:      "Distribution of proof updates per activity execution",
			Buckets:   []float64{0, 1, 2, 5, 10, 25, 50, 100},
		},
		[]string{"activity"},
	)

	// crsConstraintsPerActivity tracks constraints added distribution by activity.
	// Answers: "Which activities add the most constraints?"
	crsConstraintsPerActivity = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "code_buddy",
			Subsystem: "crs",
			Name:      "constraints_per_activity",
			Help:      "Distribution of constraints added per activity execution",
			Buckets:   []float64{0, 1, 2, 5, 10, 25, 50},
		},
		[]string{"activity"},
	)

	// crsDependenciesPerActivity tracks dependencies found distribution by activity.
	// Answers: "Which activities discover the most dependency edges?"
	crsDependenciesPerActivity = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "code_buddy",
			Subsystem: "crs",
			Name:      "dependencies_per_activity",
			Help:      "Distribution of dependencies found per activity execution",
			Buckets:   []float64{0, 1, 5, 10, 25, 50, 100, 250},
		},
		[]string{"activity"},
	)

	// crsSymbolsPerActivity tracks symbols discovered distribution by activity.
	// Answers: "Which activities find the most symbols?"
	crsSymbolsPerActivity = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "code_buddy",
			Subsystem: "crs",
			Name:      "symbols_per_activity",
			Help:      "Distribution of symbols found per activity execution",
			Buckets:   []float64{0, 1, 5, 10, 25, 50, 100, 250, 500},
		},
		[]string{"activity"},
	)
)

// -----------------------------------------------------------------------------
// Metric Recording Functions
// -----------------------------------------------------------------------------

// RecordActivityMetrics records metrics for a completed activity.
//
// Description:
//
//	Records the activity execution result as Prometheus metrics:
//	- Increments activities_total counter with status label
//	- Observes activity_duration_seconds histogram
//
// Inputs:
//
//	activityName - The name of the activity. Sanitized against knownActivities.
//	success - True if the activity fully succeeded.
//	partial - True if the activity partially succeeded (some algorithms failed).
//	durationSeconds - The activity execution duration in seconds.
//
// Outputs:
//
//	None.
//
// Thread Safety: Safe for concurrent use.
func RecordActivityMetrics(activityName string, success bool, partial bool, durationSeconds float64) {
	name := sanitizeActivityName(activityName)

	status := "success"
	if !success {
		if partial {
			status = "partial"
		} else {
			status = "failure"
		}
	}

	crsActivitiesTotal.WithLabelValues(name, status).Inc()
	crsActivityDurationSeconds.WithLabelValues(name).Observe(durationSeconds)
}

// RecordStepMetrics records metrics from a trace step.
//
// Description:
//
//	Extracts and records Prometheus metrics from a TraceStep:
//	- Increments steps_recorded_total
//	- Increments proof_updates_total for each proof update
//	- Increments constraints_added_total for each constraint
//	- Increments dependencies_found_total
//	- Observes per-activity distribution histograms
//
// Inputs:
//
//	activityName - The name of the activity. Sanitized against knownActivities.
//	step - The trace step to extract metrics from. If nil, returns immediately.
//
// Outputs:
//
//	None.
//
// Thread Safety: Safe for concurrent use.
func RecordStepMetrics(activityName string, step *crs.TraceStep) {
	// DR-11: Validate step pointer at function entry
	if step == nil {
		return
	}

	name := sanitizeActivityName(activityName)

	crsStepsRecordedTotal.Inc()

	// Count proof updates by status (total counters)
	for _, update := range step.ProofUpdates {
		status := update.Status
		if status == "" {
			status = "unknown"
		}
		crsProofUpdatesTotal.WithLabelValues(status).Inc()
	}

	// Count constraints by type (total counters)
	for _, constraint := range step.ConstraintsAdded {
		constraintType := constraint.Type
		if constraintType == "" {
			constraintType = "unknown"
		}
		crsConstraintsAddedTotal.WithLabelValues(constraintType).Inc()
	}

	// Count dependencies (total counter)
	if len(step.DependenciesFound) > 0 {
		crsDependenciesFoundTotal.Add(float64(len(step.DependenciesFound)))
	}

	// --- Per-Activity Distribution Histograms (Option B) ---
	// These track HOW MANY changes each activity produces per execution
	crsProofUpdatesPerActivity.WithLabelValues(name).Observe(float64(len(step.ProofUpdates)))
	crsConstraintsPerActivity.WithLabelValues(name).Observe(float64(len(step.ConstraintsAdded)))
	crsDependenciesPerActivity.WithLabelValues(name).Observe(float64(len(step.DependenciesFound)))
	crsSymbolsPerActivity.WithLabelValues(name).Observe(float64(len(step.SymbolsFound)))
}

// UpdateGenerationGauge updates the CRS generation gauge.
//
// Description:
//
//	Sets the generation gauge to the current CRS generation number.
//	Called after each successful delta application.
//
// Inputs:
//
//	generation - The current CRS generation number.
//
// Outputs:
//
//	None.
//
// Thread Safety: Safe for concurrent use.
func UpdateGenerationGauge(generation int64) {
	crsGenerationGauge.Set(float64(generation))
}

// RecordConflict increments the conflicts counter.
//
// Description:
//
//	Called when a delta application fails due to conflict,
//	triggering a retry with a fresh snapshot.
//
// Thread Safety: Safe for concurrent use.
func RecordConflict() {
	crsConflictsTotal.Inc()
}

// RecordRecordingError increments the recording errors counter.
//
// Description:
//
//	Called when trace recording fails. Used for alerting on
//	observability infrastructure issues.
//
// Inputs:
//
//	errorType - The type of error: "panic", "recorder_nil", "extraction_failed".
//
// Thread Safety: Safe for concurrent use.
func RecordRecordingError(errorType string) {
	crsRecordingErrorsTotal.WithLabelValues(errorType).Inc()
}

// RecordRecordingDuration records the time taken to record a trace step.
//
// Description:
//
//	Observes the recording duration to monitor observability overhead.
//	Helps identify if recording is impacting activity latency.
//
// Inputs:
//
//	startTime - When recording started.
//
// Outputs:
//
//	None.
//
// Thread Safety: Safe for concurrent use.
func RecordRecordingDuration(startTime time.Time) {
	crsRecordingDurationSeconds.Observe(time.Since(startTime).Seconds())
}

// -----------------------------------------------------------------------------
// Testing Support
// -----------------------------------------------------------------------------

// RegisterActivityName adds an activity name to the known activities set.
// This is primarily for testing purposes.
//
// Thread Safety: NOT safe for concurrent use. Call during initialization only.
func RegisterActivityName(name string) {
	knownActivities[name] = true
}
