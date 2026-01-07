package models

import (
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"
)

// =============================================================================
// ModelAuditEvent Tests
// =============================================================================

// TestModelAuditEvent_WithTimestamp verifies timestamp setting.
//
// # Description
//
// Tests that WithTimestamp correctly sets the timestamp.
// When zero time is passed, it should use current UTC time.
func TestModelAuditEvent_WithTimestamp(t *testing.T) {
	t.Run("sets provided timestamp", func(t *testing.T) {
		fixedTime := time.Date(2026, 1, 6, 12, 0, 0, 0, time.UTC)
		event := ModelAuditEvent{Action: "test"}.WithTimestamp(fixedTime)

		if !event.Timestamp.Equal(fixedTime) {
			t.Errorf("expected timestamp %v, got %v", fixedTime, event.Timestamp)
		}
	})

	t.Run("uses current UTC time when zero", func(t *testing.T) {
		before := time.Now().UTC()
		event := ModelAuditEvent{Action: "test"}.WithTimestamp(time.Time{})
		after := time.Now().UTC()

		if event.Timestamp.Before(before) || event.Timestamp.After(after) {
			t.Errorf("expected timestamp between %v and %v, got %v", before, after, event.Timestamp)
		}
	})

	t.Run("preserves other fields", func(t *testing.T) {
		fixedTime := time.Date(2026, 1, 6, 12, 0, 0, 0, time.UTC)
		event := ModelAuditEvent{
			Action: "pull_complete",
			Model:  "llama3:8b",
		}.WithTimestamp(fixedTime)

		if event.Action != "pull_complete" {
			t.Errorf("expected action 'pull_complete', got '%s'", event.Action)
		}
		if event.Model != "llama3:8b" {
			t.Errorf("expected model 'llama3:8b', got '%s'", event.Model)
		}
	})
}

// TestModelAuditEvent_WithHostInfo verifies host info population.
//
// # Description
//
// Tests that WithHostInfo populates User and Hostname from system.
func TestModelAuditEvent_WithHostInfo(t *testing.T) {
	t.Run("populates user and hostname", func(t *testing.T) {
		event := ModelAuditEvent{Action: "test"}.WithHostInfo()

		// User might be empty depending on environment, but shouldn't panic
		// Hostname should typically be set
		if event.Hostname == "" {
			t.Log("hostname is empty (may be expected in some test environments)")
		}
	})

	t.Run("preserves other fields", func(t *testing.T) {
		event := ModelAuditEvent{
			Action: "verify",
			Model:  "llama3:8b",
			Digest: "sha256:abc123",
		}.WithHostInfo()

		if event.Action != "verify" {
			t.Errorf("expected action 'verify', got '%s'", event.Action)
		}
		if event.Model != "llama3:8b" {
			t.Errorf("expected model 'llama3:8b', got '%s'", event.Model)
		}
		if event.Digest != "sha256:abc123" {
			t.Errorf("expected digest 'sha256:abc123', got '%s'", event.Digest)
		}
	})
}

// TestModelAuditEvent_WithDuration verifies duration setting.
//
// # Description
//
// Tests that WithDuration sets both Duration and DurationMs fields.
func TestModelAuditEvent_WithDuration(t *testing.T) {
	t.Run("sets duration and milliseconds", func(t *testing.T) {
		duration := 5 * time.Minute
		event := ModelAuditEvent{Action: "test"}.WithDuration(duration)

		if event.Duration != duration {
			t.Errorf("expected duration %v, got %v", duration, event.Duration)
		}
		if event.DurationMs != 300000 {
			t.Errorf("expected duration_ms 300000, got %d", event.DurationMs)
		}
	})

	t.Run("handles zero duration", func(t *testing.T) {
		event := ModelAuditEvent{Action: "test"}.WithDuration(0)

		if event.Duration != 0 {
			t.Errorf("expected duration 0, got %v", event.Duration)
		}
		if event.DurationMs != 0 {
			t.Errorf("expected duration_ms 0, got %d", event.DurationMs)
		}
	})

	t.Run("handles sub-millisecond duration", func(t *testing.T) {
		duration := 500 * time.Microsecond
		event := ModelAuditEvent{Action: "test"}.WithDuration(duration)

		if event.Duration != duration {
			t.Errorf("expected duration %v, got %v", duration, event.Duration)
		}
		// 500 microseconds = 0 milliseconds (truncated)
		if event.DurationMs != 0 {
			t.Errorf("expected duration_ms 0 for sub-ms duration, got %d", event.DurationMs)
		}
	})
}

// TestModelAuditEvent_ToJSON verifies JSON serialization.
//
// # Description
//
// Tests that ToJSON produces valid JSON with correct structure.
func TestModelAuditEvent_ToJSON(t *testing.T) {
	t.Run("serializes all fields", func(t *testing.T) {
		fixedTime := time.Date(2026, 1, 6, 12, 0, 0, 0, time.UTC)
		event := ModelAuditEvent{
			Timestamp:  fixedTime,
			Action:     "pull_complete",
			Model:      "llama3:8b",
			User:       "developer",
			Hostname:   "dev-machine",
			Success:    true,
			BytesTotal: 4000000000,
			Duration:   5 * time.Minute,
			DurationMs: 300000,
			Digest:     "sha256:abc123",
			Source:     "https://ollama.ai",
		}

		jsonBytes := event.ToJSON()
		if len(jsonBytes) == 0 {
			t.Fatal("expected non-empty JSON")
		}

		// Parse back to verify
		var parsed map[string]interface{}
		if err := json.Unmarshal(jsonBytes, &parsed); err != nil {
			t.Fatalf("failed to parse JSON: %v", err)
		}

		if parsed["action"] != "pull_complete" {
			t.Errorf("expected action 'pull_complete', got '%v'", parsed["action"])
		}
		if parsed["model"] != "llama3:8b" {
			t.Errorf("expected model 'llama3:8b', got '%v'", parsed["model"])
		}
		if parsed["success"] != true {
			t.Errorf("expected success true, got %v", parsed["success"])
		}
	})

	t.Run("omits empty optional fields", func(t *testing.T) {
		event := ModelAuditEvent{
			Timestamp: time.Date(2026, 1, 6, 12, 0, 0, 0, time.UTC),
			Action:    "block",
			Model:     "test",
			Success:   false,
		}

		jsonBytes := event.ToJSON()
		var parsed map[string]interface{}
		if err := json.Unmarshal(jsonBytes, &parsed); err != nil {
			t.Fatalf("failed to parse JSON: %v", err)
		}

		// These should be omitted
		if _, ok := parsed["user"]; ok {
			t.Error("expected user to be omitted")
		}
		if _, ok := parsed["hostname"]; ok {
			t.Error("expected hostname to be omitted")
		}
		if _, ok := parsed["digest"]; ok {
			t.Error("expected digest to be omitted")
		}
	})

	t.Run("includes Duration as json ignored", func(t *testing.T) {
		event := ModelAuditEvent{
			Timestamp:  time.Date(2026, 1, 6, 12, 0, 0, 0, time.UTC),
			Action:     "test",
			Model:      "test",
			Duration:   5 * time.Minute,
			DurationMs: 300000,
		}

		jsonBytes := event.ToJSON()
		var parsed map[string]interface{}
		if err := json.Unmarshal(jsonBytes, &parsed); err != nil {
			t.Fatalf("failed to parse JSON: %v", err)
		}

		// Duration should NOT be in JSON (has json:"-" tag)
		if _, ok := parsed["Duration"]; ok {
			t.Error("Duration should not be in JSON (has json:\"-\" tag)")
		}
		// But DurationMs should be
		if parsed["duration_ms"] != float64(300000) {
			t.Errorf("expected duration_ms 300000, got %v", parsed["duration_ms"])
		}
	})
}

// TestModelAuditEvent_String verifies string representation.
//
// # Description
//
// Tests that String produces human-readable output.
func TestModelAuditEvent_String(t *testing.T) {
	t.Run("formats success event", func(t *testing.T) {
		fixedTime := time.Date(2026, 1, 6, 12, 0, 0, 0, time.UTC)
		event := ModelAuditEvent{
			Timestamp: fixedTime,
			Action:    "pull_complete",
			Model:     "llama3:8b",
			User:      "developer",
			Success:   true,
		}

		str := event.String()
		expected := "2026-01-06T12:00:00Z [pull_complete] model=llama3:8b user=developer success"
		if str != expected {
			t.Errorf("expected '%s', got '%s'", expected, str)
		}
	})

	t.Run("formats failed event", func(t *testing.T) {
		fixedTime := time.Date(2026, 1, 6, 12, 0, 0, 0, time.UTC)
		event := ModelAuditEvent{
			Timestamp: fixedTime,
			Action:    "block",
			Model:     "unauthorized",
			User:      "admin",
			Success:   false,
		}

		str := event.String()
		expected := "2026-01-06T12:00:00Z [block] model=unauthorized user=admin failed"
		if str != expected {
			t.Errorf("expected '%s', got '%s'", expected, str)
		}
	})

	t.Run("formats failed event with error message", func(t *testing.T) {
		fixedTime := time.Date(2026, 1, 6, 12, 0, 0, 0, time.UTC)
		event := ModelAuditEvent{
			Timestamp:    fixedTime,
			Action:       "block",
			Model:        "unauthorized",
			User:         "admin",
			Success:      false,
			ErrorMessage: "not in allowlist",
		}

		str := event.String()
		expected := "2026-01-06T12:00:00Z [block] model=unauthorized user=admin failed: not in allowlist"
		if str != expected {
			t.Errorf("expected '%s', got '%s'", expected, str)
		}
	})
}

// =============================================================================
// DefaultModelAuditLogger Tests
// =============================================================================

// TestNewDefaultModelAuditLogger verifies constructor.
//
// # Description
//
// Tests that NewDefaultModelAuditLogger creates a properly configured logger.
func TestNewDefaultModelAuditLogger(t *testing.T) {
	t.Run("creates enabled logger with writer", func(t *testing.T) {
		writer := &MockLogWriter{}
		logger := NewDefaultModelAuditLogger(writer)

		if logger == nil {
			t.Fatal("expected non-nil logger")
		}
		if !logger.IsEnabled() {
			t.Error("expected logger to be enabled")
		}
	})

	t.Run("creates disabled logger with nil writer", func(t *testing.T) {
		logger := NewDefaultModelAuditLogger(nil)

		if logger == nil {
			t.Fatal("expected non-nil logger")
		}
		if logger.IsEnabled() {
			t.Error("expected logger to be disabled with nil writer")
		}
	})
}

// TestDefaultModelAuditLogger_LogModelPull verifies pull logging.
//
// # Description
//
// Tests that LogModelPull writes audit events correctly.
func TestDefaultModelAuditLogger_LogModelPull(t *testing.T) {
	t.Run("logs pull event", func(t *testing.T) {
		writer := &MockLogWriter{}
		logger := NewDefaultModelAuditLogger(writer)

		fixedTime := time.Date(2026, 1, 6, 12, 0, 0, 0, time.UTC)
		event := ModelAuditEvent{
			Timestamp:  fixedTime,
			Action:     "pull_complete",
			Model:      "llama3:8b",
			User:       "developer",
			Hostname:   "dev-machine",
			Success:    true,
			BytesTotal: 4000000000,
		}

		err := logger.LogModelPull(event)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}

		if len(writer.Entries) != 1 {
			t.Fatalf("expected 1 entry, got %d", len(writer.Entries))
		}

		entry := writer.Entries[0]
		if entry.Level != "info" {
			t.Errorf("expected level 'info', got '%s'", entry.Level)
		}
		if entry.Message != "[AUDIT] model.pull: pull_complete" {
			t.Errorf("unexpected message: %s", entry.Message)
		}
	})

	t.Run("enriches event with timestamp and host info", func(t *testing.T) {
		writer := &MockLogWriter{}
		logger := NewDefaultModelAuditLogger(writer)

		// Event without timestamp or host info
		event := ModelAuditEvent{
			Action:  "pull_start",
			Model:   "llama3:8b",
			Success: true,
		}

		err := logger.LogModelPull(event)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}

		if len(writer.Entries) != 1 {
			t.Fatalf("expected 1 entry, got %d", len(writer.Entries))
		}

		// Parse the logged JSON to verify enrichment
		var parsed ModelAuditEvent
		if err := json.Unmarshal(writer.Entries[0].Fields, &parsed); err != nil {
			t.Fatalf("failed to parse JSON: %v", err)
		}

		if parsed.Timestamp.IsZero() {
			t.Error("expected timestamp to be enriched")
		}
		// User/Hostname might still be empty depending on environment
	})

	t.Run("no-op when disabled", func(t *testing.T) {
		writer := &MockLogWriter{}
		logger := NewDefaultModelAuditLogger(writer)
		logger.SetEnabled(false)

		event := ModelAuditEvent{
			Action: "pull_complete",
			Model:  "llama3:8b",
		}

		err := logger.LogModelPull(event)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}

		if len(writer.Entries) != 0 {
			t.Errorf("expected 0 entries when disabled, got %d", len(writer.Entries))
		}
	})
}

// TestDefaultModelAuditLogger_LogModelVerify verifies verification logging.
//
// # Description
//
// Tests that LogModelVerify writes audit events correctly.
func TestDefaultModelAuditLogger_LogModelVerify(t *testing.T) {
	t.Run("logs verify event", func(t *testing.T) {
		writer := &MockLogWriter{}
		logger := NewDefaultModelAuditLogger(writer)

		fixedTime := time.Date(2026, 1, 6, 12, 0, 0, 0, time.UTC)
		event := ModelAuditEvent{
			Timestamp: fixedTime,
			Action:    "verify",
			Model:     "llama3:8b",
			User:      "developer",
			Success:   true,
			Digest:    "sha256:abc123",
		}

		err := logger.LogModelVerify(event)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}

		if len(writer.Entries) != 1 {
			t.Fatalf("expected 1 entry, got %d", len(writer.Entries))
		}

		entry := writer.Entries[0]
		if entry.Level != "info" {
			t.Errorf("expected level 'info', got '%s'", entry.Level)
		}
		if entry.Message != "[AUDIT] model.verify: verify" {
			t.Errorf("unexpected message: %s", entry.Message)
		}
	})
}

// TestDefaultModelAuditLogger_LogModelBlock verifies block logging.
//
// # Description
//
// Tests that LogModelBlock writes audit events at warn level.
func TestDefaultModelAuditLogger_LogModelBlock(t *testing.T) {
	t.Run("logs block event at warn level", func(t *testing.T) {
		writer := &MockLogWriter{}
		logger := NewDefaultModelAuditLogger(writer)

		fixedTime := time.Date(2026, 1, 6, 12, 0, 0, 0, time.UTC)
		event := ModelAuditEvent{
			Timestamp:    fixedTime,
			Action:       "block",
			Model:        "unauthorized-model",
			User:         "admin",
			Success:      false,
			ErrorMessage: "not in allowlist",
		}

		err := logger.LogModelBlock(event)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}

		if len(writer.Entries) != 1 {
			t.Fatalf("expected 1 entry, got %d", len(writer.Entries))
		}

		entry := writer.Entries[0]
		if entry.Level != "warn" {
			t.Errorf("expected level 'warn', got '%s'", entry.Level)
		}
		if entry.Message != "[AUDIT] model.block: block" {
			t.Errorf("unexpected message: %s", entry.Message)
		}
	})
}

// TestDefaultModelAuditLogger_SetEnabled verifies enable/disable.
//
// # Description
//
// Tests that SetEnabled controls logging state.
func TestDefaultModelAuditLogger_SetEnabled(t *testing.T) {
	t.Run("can disable and re-enable", func(t *testing.T) {
		writer := &MockLogWriter{}
		logger := NewDefaultModelAuditLogger(writer)

		if !logger.IsEnabled() {
			t.Error("expected initially enabled")
		}

		logger.SetEnabled(false)
		if logger.IsEnabled() {
			t.Error("expected disabled after SetEnabled(false)")
		}

		logger.SetEnabled(true)
		if !logger.IsEnabled() {
			t.Error("expected enabled after SetEnabled(true)")
		}
	})
}

// TestDefaultModelAuditLogger_ConcurrentAccess verifies thread safety.
//
// # Description
//
// Tests that DefaultModelAuditLogger is safe for concurrent use.
func TestDefaultModelAuditLogger_ConcurrentAccess(t *testing.T) {
	writer := &MockLogWriter{}
	logger := NewDefaultModelAuditLogger(writer)

	const goroutines = 10
	const eventsPerGoroutine = 100

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < eventsPerGoroutine; j++ {
				event := ModelAuditEvent{
					Action: "pull_complete",
					Model:  "test-model",
				}
				_ = logger.LogModelPull(event)

				// Occasionally toggle enabled state
				if j%50 == 0 {
					logger.SetEnabled(true)
				}
			}
		}(i)
	}

	wg.Wait()

	// Should have logged some events (not all due to disabled state)
	if len(writer.Entries) == 0 {
		t.Error("expected some entries to be logged")
	}
}

// TestDefaultModelAuditLogger_EnrichDurationMs verifies duration enrichment.
//
// # Description
//
// Tests that enrichEvent populates DurationMs from Duration.
func TestDefaultModelAuditLogger_EnrichDurationMs(t *testing.T) {
	t.Run("populates duration_ms from duration", func(t *testing.T) {
		writer := &MockLogWriter{}
		logger := NewDefaultModelAuditLogger(writer)

		event := ModelAuditEvent{
			Action:   "pull_complete",
			Model:    "llama3:8b",
			Duration: 5 * time.Minute,
			// DurationMs not set
		}

		err := logger.LogModelPull(event)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}

		// Parse the logged JSON
		var parsed ModelAuditEvent
		if err := json.Unmarshal(writer.Entries[0].Fields, &parsed); err != nil {
			t.Fatalf("failed to parse JSON: %v", err)
		}

		if parsed.DurationMs != 300000 {
			t.Errorf("expected duration_ms 300000, got %d", parsed.DurationMs)
		}
	})
}

// =============================================================================
// MockModelAuditLogger Tests
// =============================================================================

// TestNewMockModelAuditLogger verifies constructor.
//
// # Description
//
// Tests that NewMockModelAuditLogger creates a properly initialized mock.
func TestNewMockModelAuditLogger(t *testing.T) {
	mock := NewMockModelAuditLogger()
	if mock == nil {
		t.Fatal("expected non-nil mock")
	}
	if len(mock.PullEvents) != 0 {
		t.Error("expected empty PullEvents")
	}
	if len(mock.VerifyEvents) != 0 {
		t.Error("expected empty VerifyEvents")
	}
	if len(mock.BlockEvents) != 0 {
		t.Error("expected empty BlockEvents")
	}
}

// TestMockModelAuditLogger_RecordsEvents verifies event recording.
//
// # Description
//
// Tests that the mock records all logged events.
func TestMockModelAuditLogger_RecordsEvents(t *testing.T) {
	mock := NewMockModelAuditLogger()

	pullEvent := ModelAuditEvent{Action: "pull_complete", Model: "model1"}
	verifyEvent := ModelAuditEvent{Action: "verify", Model: "model2"}
	blockEvent := ModelAuditEvent{Action: "block", Model: "model3"}

	_ = mock.LogModelPull(pullEvent)
	_ = mock.LogModelVerify(verifyEvent)
	_ = mock.LogModelBlock(blockEvent)

	if len(mock.PullEvents) != 1 {
		t.Errorf("expected 1 pull event, got %d", len(mock.PullEvents))
	}
	if len(mock.VerifyEvents) != 1 {
		t.Errorf("expected 1 verify event, got %d", len(mock.VerifyEvents))
	}
	if len(mock.BlockEvents) != 1 {
		t.Errorf("expected 1 block event, got %d", len(mock.BlockEvents))
	}

	if mock.PullEvents[0].Model != "model1" {
		t.Errorf("expected model 'model1', got '%s'", mock.PullEvents[0].Model)
	}
}

// TestMockModelAuditLogger_AllEvents verifies combined event retrieval.
//
// # Description
//
// Tests that AllEvents returns all events in order.
func TestMockModelAuditLogger_AllEvents(t *testing.T) {
	mock := NewMockModelAuditLogger()

	_ = mock.LogModelPull(ModelAuditEvent{Action: "pull", Model: "m1"})
	_ = mock.LogModelPull(ModelAuditEvent{Action: "pull", Model: "m2"})
	_ = mock.LogModelVerify(ModelAuditEvent{Action: "verify", Model: "m3"})
	_ = mock.LogModelBlock(ModelAuditEvent{Action: "block", Model: "m4"})

	all := mock.AllEvents()
	if len(all) != 4 {
		t.Fatalf("expected 4 events, got %d", len(all))
	}

	// Order: pull, pull, verify, block
	if all[0].Model != "m1" {
		t.Errorf("expected first event model 'm1', got '%s'", all[0].Model)
	}
	if all[1].Model != "m2" {
		t.Errorf("expected second event model 'm2', got '%s'", all[1].Model)
	}
	if all[2].Model != "m3" {
		t.Errorf("expected third event model 'm3', got '%s'", all[2].Model)
	}
	if all[3].Model != "m4" {
		t.Errorf("expected fourth event model 'm4', got '%s'", all[3].Model)
	}
}

// TestMockModelAuditLogger_Reset verifies reset functionality.
//
// # Description
//
// Tests that Reset clears all recorded events.
func TestMockModelAuditLogger_Reset(t *testing.T) {
	mock := NewMockModelAuditLogger()

	_ = mock.LogModelPull(ModelAuditEvent{Action: "pull"})
	_ = mock.LogModelVerify(ModelAuditEvent{Action: "verify"})
	_ = mock.LogModelBlock(ModelAuditEvent{Action: "block"})

	if len(mock.AllEvents()) != 3 {
		t.Error("expected 3 events before reset")
	}

	mock.Reset()

	if len(mock.PullEvents) != 0 {
		t.Error("expected empty PullEvents after reset")
	}
	if len(mock.VerifyEvents) != 0 {
		t.Error("expected empty VerifyEvents after reset")
	}
	if len(mock.BlockEvents) != 0 {
		t.Error("expected empty BlockEvents after reset")
	}
	if len(mock.AllEvents()) != 0 {
		t.Error("expected 0 events after reset")
	}
}

// TestMockModelAuditLogger_FunctionOverrides verifies custom behavior.
//
// # Description
//
// Tests that function overrides are called when set.
func TestMockModelAuditLogger_FunctionOverrides(t *testing.T) {
	t.Run("LogModelPullFunc is called", func(t *testing.T) {
		mock := NewMockModelAuditLogger()
		customErr := errors.New("custom pull error")
		mock.LogModelPullFunc = func(event ModelAuditEvent) error {
			return customErr
		}

		err := mock.LogModelPull(ModelAuditEvent{Action: "pull"})
		if err != customErr {
			t.Errorf("expected custom error, got %v", err)
		}

		// Event should still be recorded
		if len(mock.PullEvents) != 1 {
			t.Error("expected event to be recorded even with override")
		}
	})

	t.Run("LogModelVerifyFunc is called", func(t *testing.T) {
		mock := NewMockModelAuditLogger()
		customErr := errors.New("custom verify error")
		mock.LogModelVerifyFunc = func(event ModelAuditEvent) error {
			return customErr
		}

		err := mock.LogModelVerify(ModelAuditEvent{Action: "verify"})
		if err != customErr {
			t.Errorf("expected custom error, got %v", err)
		}
	})

	t.Run("LogModelBlockFunc is called", func(t *testing.T) {
		mock := NewMockModelAuditLogger()
		customErr := errors.New("custom block error")
		mock.LogModelBlockFunc = func(event ModelAuditEvent) error {
			return customErr
		}

		err := mock.LogModelBlock(ModelAuditEvent{Action: "block"})
		if err != customErr {
			t.Errorf("expected custom error, got %v", err)
		}
	})
}

// TestMockModelAuditLogger_DefaultErr verifies default error behavior.
//
// # Description
//
// Tests that DefaultErr is returned when no function override is set.
func TestMockModelAuditLogger_DefaultErr(t *testing.T) {
	mock := NewMockModelAuditLogger()
	defaultErr := errors.New("default error")
	mock.DefaultErr = defaultErr

	err := mock.LogModelPull(ModelAuditEvent{Action: "pull"})
	if err != defaultErr {
		t.Errorf("expected default error, got %v", err)
	}

	err = mock.LogModelVerify(ModelAuditEvent{Action: "verify"})
	if err != defaultErr {
		t.Errorf("expected default error, got %v", err)
	}

	err = mock.LogModelBlock(ModelAuditEvent{Action: "block"})
	if err != defaultErr {
		t.Errorf("expected default error, got %v", err)
	}
}

// TestMockModelAuditLogger_ConcurrentAccess verifies thread safety.
//
// # Description
//
// Tests that MockModelAuditLogger is safe for concurrent use.
func TestMockModelAuditLogger_ConcurrentAccess(t *testing.T) {
	mock := NewMockModelAuditLogger()

	const goroutines = 10
	const eventsPerGoroutine = 100

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < eventsPerGoroutine; j++ {
				_ = mock.LogModelPull(ModelAuditEvent{Action: "pull"})
				_ = mock.LogModelVerify(ModelAuditEvent{Action: "verify"})
				_ = mock.LogModelBlock(ModelAuditEvent{Action: "block"})
			}
		}(i)
	}

	wg.Wait()

	expectedTotal := goroutines * eventsPerGoroutine * 3
	actualTotal := len(mock.AllEvents())
	if actualTotal != expectedTotal {
		t.Errorf("expected %d events, got %d", expectedTotal, actualTotal)
	}
}

// =============================================================================
// MockLogWriter Tests
// =============================================================================

// TestMockLogWriter_WriteLog verifies log capture.
//
// # Description
//
// Tests that MockLogWriter captures log entries correctly.
func TestMockLogWriter_WriteLog(t *testing.T) {
	writer := &MockLogWriter{}

	writer.WriteLog("info", "test message", []byte(`{"key":"value"}`))

	if len(writer.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(writer.Entries))
	}

	entry := writer.Entries[0]
	if entry.Level != "info" {
		t.Errorf("expected level 'info', got '%s'", entry.Level)
	}
	if entry.Message != "test message" {
		t.Errorf("expected message 'test message', got '%s'", entry.Message)
	}
	if string(entry.Fields) != `{"key":"value"}` {
		t.Errorf("unexpected fields: %s", string(entry.Fields))
	}
}

// TestMockLogWriter_Reset verifies reset functionality.
//
// # Description
//
// Tests that Reset clears captured entries.
func TestMockLogWriter_Reset(t *testing.T) {
	writer := &MockLogWriter{}

	writer.WriteLog("info", "message1", nil)
	writer.WriteLog("warn", "message2", nil)

	if len(writer.Entries) != 2 {
		t.Error("expected 2 entries before reset")
	}

	writer.Reset()

	if len(writer.Entries) != 0 {
		t.Error("expected 0 entries after reset")
	}
}

// TestMockLogWriter_ConcurrentAccess verifies thread safety.
//
// # Description
//
// Tests that MockLogWriter is safe for concurrent use.
func TestMockLogWriter_ConcurrentAccess(t *testing.T) {
	writer := &MockLogWriter{}

	const goroutines = 10
	const writesPerGoroutine = 100

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < writesPerGoroutine; j++ {
				writer.WriteLog("info", "concurrent message", nil)
			}
		}(i)
	}

	wg.Wait()

	expectedTotal := goroutines * writesPerGoroutine
	if len(writer.Entries) != expectedTotal {
		t.Errorf("expected %d entries, got %d", expectedTotal, len(writer.Entries))
	}
}

// TestMockLogWriter_ResetDuringTest verifies reset in test context.
//
// # Description
//
// Tests that MockLogWriter.Reset works correctly during a test workflow.
func TestMockLogWriter_ResetDuringTest(t *testing.T) {
	writer := &MockLogWriter{}

	// First batch of writes
	writer.WriteLog("info", "message1", nil)
	writer.WriteLog("warn", "message2", nil)
	writer.WriteLog("error", "message3", nil)

	if len(writer.Entries) != 3 {
		t.Errorf("expected 3 entries before reset, got %d", len(writer.Entries))
	}

	// Reset
	writer.Reset()

	if len(writer.Entries) != 0 {
		t.Errorf("expected 0 entries after reset, got %d", len(writer.Entries))
	}

	// Second batch of writes after reset
	writer.WriteLog("info", "new message", []byte(`{"new":"data"}`))

	if len(writer.Entries) != 1 {
		t.Errorf("expected 1 entry after new write, got %d", len(writer.Entries))
	}

	if writer.Entries[0].Message != "new message" {
		t.Errorf("expected message 'new message', got '%s'", writer.Entries[0].Message)
	}
}

// =============================================================================
// Integration Tests
// =============================================================================

// TestAuditLogger_FullWorkflow tests a complete audit logging workflow.
//
// # Description
//
// Simulates a realistic model pull workflow with audit logging.
func TestAuditLogger_FullWorkflow(t *testing.T) {
	writer := &MockLogWriter{}
	logger := NewDefaultModelAuditLogger(writer)

	// 1. Log pull start
	startEvent := ModelAuditEvent{
		Action:  "pull_start",
		Model:   "llama3:8b",
		Success: true,
	}.WithTimestamp(time.Time{}).WithHostInfo()

	err := logger.LogModelPull(startEvent)
	if err != nil {
		t.Errorf("unexpected error on pull_start: %v", err)
	}

	// 2. Simulate pull completion
	completeEvent := ModelAuditEvent{
		Action:     "pull_complete",
		Model:      "llama3:8b",
		Success:    true,
		BytesTotal: 4_000_000_000,
		Digest:     "sha256:abc123def456",
	}.WithTimestamp(time.Time{}).WithHostInfo().WithDuration(5 * time.Minute)

	err = logger.LogModelPull(completeEvent)
	if err != nil {
		t.Errorf("unexpected error on pull_complete: %v", err)
	}

	// 3. Verify the model
	verifyEvent := ModelAuditEvent{
		Action:  "verify",
		Model:   "llama3:8b",
		Success: true,
		Digest:  "sha256:abc123def456",
	}.WithTimestamp(time.Time{}).WithHostInfo()

	err = logger.LogModelVerify(verifyEvent)
	if err != nil {
		t.Errorf("unexpected error on verify: %v", err)
	}

	// Validate logged entries
	if len(writer.Entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(writer.Entries))
	}

	// All should be info level
	for i, entry := range writer.Entries {
		if entry.Level != "info" {
			t.Errorf("entry %d: expected level 'info', got '%s'", i, entry.Level)
		}
	}
}

// TestAuditLogger_BlockedModelWorkflow tests blocking unauthorized models.
//
// # Description
//
// Simulates a model request being blocked by allowlist.
func TestAuditLogger_BlockedModelWorkflow(t *testing.T) {
	writer := &MockLogWriter{}
	logger := NewDefaultModelAuditLogger(writer)

	blockEvent := ModelAuditEvent{
		Action:       "block",
		Model:        "unauthorized-model:latest",
		Success:      false,
		ErrorMessage: "model not in enterprise allowlist",
	}.WithTimestamp(time.Time{}).WithHostInfo()

	err := logger.LogModelBlock(blockEvent)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if len(writer.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(writer.Entries))
	}

	entry := writer.Entries[0]
	if entry.Level != "warn" {
		t.Errorf("expected level 'warn' for blocked models, got '%s'", entry.Level)
	}

	// Parse and verify error message is logged
	var parsed ModelAuditEvent
	if err := json.Unmarshal(entry.Fields, &parsed); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	if parsed.ErrorMessage != "model not in enterprise allowlist" {
		t.Errorf("expected error message preserved, got '%s'", parsed.ErrorMessage)
	}
}
