package main

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestDefaultGoroutineTrackerConfig(t *testing.T) {
	config := DefaultGoroutineTrackerConfig()

	if config.LongRunningThreshold <= 0 {
		t.Error("LongRunningThreshold should be positive")
	}
	if config.Logger == nil {
		t.Error("Logger should not be nil")
	}
}

func TestNewGoroutineTracker(t *testing.T) {
	tests := []struct {
		name   string
		config GoroutineTrackerConfig
	}{
		{
			name:   "with defaults",
			config: DefaultGoroutineTrackerConfig(),
		},
		{
			name: "with zero values",
			config: GoroutineTrackerConfig{
				LongRunningThreshold: 0, // Should be set to default
			},
		},
		{
			name: "with custom values",
			config: GoroutineTrackerConfig{
				LongRunningThreshold: time.Minute,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tracker := NewGoroutineTracker(tt.config)
			if tracker == nil {
				t.Fatal("NewGoroutineTracker returned nil")
			}
			if tracker.Active() != 0 {
				t.Errorf("Active() = %d, want 0", tracker.Active())
			}
		})
	}
}

func TestGoroutineTracker_Track(t *testing.T) {
	tracker := NewGoroutineTracker(quietTrackerConfig())

	// Initially no active goroutines
	if tracker.Active() != 0 {
		t.Errorf("Initial Active() = %d, want 0", tracker.Active())
	}

	// Track a goroutine
	done := tracker.Track("test-goroutine")

	if tracker.Active() != 1 {
		t.Errorf("After Track(): Active() = %d, want 1", tracker.Active())
	}

	// Mark as done
	done()

	if tracker.Active() != 0 {
		t.Errorf("After done(): Active() = %d, want 0", tracker.Active())
	}
}

func TestGoroutineTracker_Peak(t *testing.T) {
	tracker := NewGoroutineTracker(quietTrackerConfig())

	// Track multiple goroutines
	done1 := tracker.Track("g1")
	done2 := tracker.Track("g2")
	done3 := tracker.Track("g3")

	if tracker.Peak() != 3 {
		t.Errorf("Peak() = %d, want 3", tracker.Peak())
	}

	// Complete some
	done2()

	// Peak should remain at 3
	if tracker.Peak() != 3 {
		t.Errorf("After done2, Peak() = %d, want 3", tracker.Peak())
	}

	done1()
	done3()

	// Peak still 3
	if tracker.Peak() != 3 {
		t.Errorf("After all done, Peak() = %d, want 3", tracker.Peak())
	}
}

func TestGoroutineTracker_Stats(t *testing.T) {
	tracker := NewGoroutineTracker(quietTrackerConfig())

	done1 := tracker.Track("g1")
	done2 := tracker.Track("g2")

	stats := tracker.Stats()

	if stats.Active != 2 {
		t.Errorf("Stats.Active = %d, want 2", stats.Active)
	}
	if stats.Peak != 2 {
		t.Errorf("Stats.Peak = %d, want 2", stats.Peak)
	}
	if stats.Total != 2 {
		t.Errorf("Stats.Total = %d, want 2", stats.Total)
	}
	if stats.RuntimeGoroutines == 0 {
		t.Error("Stats.RuntimeGoroutines should be > 0")
	}

	done1()
	done2()

	stats = tracker.Stats()
	if stats.Active != 0 {
		t.Errorf("After done, Stats.Active = %d, want 0", stats.Active)
	}
	if stats.Total != 2 {
		t.Errorf("After done, Stats.Total = %d, want 2", stats.Total)
	}
}

func TestGoroutineTracker_LongRunning(t *testing.T) {
	var longRunningCalled int32

	tracker := NewGoroutineTracker(GoroutineTrackerConfig{
		LongRunningThreshold: 50 * time.Millisecond,
		OnLongRunning: func(name string, duration time.Duration) {
			atomic.AddInt32(&longRunningCalled, 1)
		},
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})

	done := tracker.Track("long-running")

	// Sleep longer than threshold
	time.Sleep(100 * time.Millisecond)

	done()

	if atomic.LoadInt32(&longRunningCalled) != 1 {
		t.Errorf("OnLongRunning should have been called once, got %d", longRunningCalled)
	}
}

func TestGoroutineTracker_LongRunningInStats(t *testing.T) {
	tracker := NewGoroutineTracker(GoroutineTrackerConfig{
		LongRunningThreshold: 50 * time.Millisecond,
		Logger:               slog.New(slog.NewTextHandler(io.Discard, nil)),
	})

	done := tracker.Track("will-be-long")

	// Sleep longer than threshold
	time.Sleep(100 * time.Millisecond)

	stats := tracker.Stats()
	if len(stats.LongRunning) != 1 {
		t.Errorf("LongRunning count = %d, want 1", len(stats.LongRunning))
	}
	if len(stats.LongRunning) > 0 && stats.LongRunning[0].Name != "will-be-long" {
		t.Errorf("LongRunning name = %q, want %q", stats.LongRunning[0].Name, "will-be-long")
	}

	done()
}

func TestGoroutineTracker_OnComplete(t *testing.T) {
	var completedName string
	var completedDuration time.Duration

	tracker := NewGoroutineTracker(GoroutineTrackerConfig{
		LongRunningThreshold: time.Hour, // Won't trigger
		OnComplete: func(name string, duration time.Duration) {
			completedName = name
			completedDuration = duration
		},
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})

	done := tracker.Track("my-task")
	time.Sleep(10 * time.Millisecond)
	done()

	if completedName != "my-task" {
		t.Errorf("OnComplete name = %q, want %q", completedName, "my-task")
	}
	if completedDuration < 10*time.Millisecond {
		t.Errorf("OnComplete duration = %v, expected >= 10ms", completedDuration)
	}
}

func TestGoroutineTracker_WaitAll(t *testing.T) {
	tracker := NewGoroutineTracker(quietTrackerConfig())

	var wg sync.WaitGroup
	wg.Add(3)

	for i := 0; i < 3; i++ {
		go func() {
			done := tracker.Track("worker")
			defer done()
			time.Sleep(50 * time.Millisecond)
			wg.Done()
		}()
	}

	// Should complete within timeout
	err := tracker.WaitAll(time.Second)
	if err != nil {
		t.Errorf("WaitAll failed: %v", err)
	}

	if tracker.Active() != 0 {
		t.Errorf("After WaitAll, Active() = %d, want 0", tracker.Active())
	}
}

func TestGoroutineTracker_WaitAll_Timeout(t *testing.T) {
	tracker := NewGoroutineTracker(quietTrackerConfig())

	// Start a goroutine that will run for a long time
	go func() {
		done := tracker.Track("slow-worker")
		defer done()
		time.Sleep(5 * time.Second)
	}()

	// Give it time to start
	time.Sleep(50 * time.Millisecond)

	// Wait with short timeout
	err := tracker.WaitAll(100 * time.Millisecond)
	if err == nil {
		t.Fatal("WaitAll should have timed out")
	}
}

func TestGoroutineTracker_TrackContext(t *testing.T) {
	tracker := NewGoroutineTracker(quietTrackerConfig())

	ctx, cancel := context.WithCancel(context.Background())
	var executed bool

	err := tracker.TrackContext(ctx, "ctx-goroutine", func(ctx context.Context) {
		executed = true
		<-ctx.Done() // Wait for cancellation
	})

	if err != nil {
		t.Fatalf("TrackContext failed: %v", err)
	}

	// Give it time to start
	time.Sleep(50 * time.Millisecond)

	if !executed {
		t.Error("Goroutine should have executed")
	}

	if tracker.Active() != 1 {
		t.Errorf("Active() = %d, want 1", tracker.Active())
	}

	// Cancel and wait
	cancel()
	time.Sleep(50 * time.Millisecond)

	if tracker.Active() != 0 {
		t.Errorf("After cancel, Active() = %d, want 0", tracker.Active())
	}
}

func TestGoroutineTracker_ListActive(t *testing.T) {
	tracker := NewGoroutineTracker(quietTrackerConfig())

	done1 := tracker.Track("goroutine-a")
	done2 := tracker.Track("goroutine-b")
	defer done1()
	defer done2()

	active := tracker.ListActive()

	if len(active) != 2 {
		t.Errorf("ListActive() returned %d items, want 2", len(active))
	}

	names := make(map[string]bool)
	for _, g := range active {
		names[g.Name] = true
	}

	if !names["goroutine-a"] {
		t.Error("ListActive should include goroutine-a")
	}
	if !names["goroutine-b"] {
		t.Error("ListActive should include goroutine-b")
	}
}

func TestGoroutineTracker_ConcurrentAccess(t *testing.T) {
	tracker := NewGoroutineTracker(quietTrackerConfig())

	const numGoroutines = 100
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	// Start many goroutines concurrently
	for i := 0; i < numGoroutines; i++ {
		go func(n int) {
			defer wg.Done()
			done := tracker.Track("concurrent")
			time.Sleep(time.Duration(n%10) * time.Millisecond)
			done()
		}(i)
	}

	wg.Wait()

	if tracker.Active() != 0 {
		t.Errorf("After all done, Active() = %d, want 0", tracker.Active())
	}

	stats := tracker.Stats()
	if stats.Total != numGoroutines {
		t.Errorf("Total = %d, want %d", stats.Total, numGoroutines)
	}
	if stats.Peak <= 0 {
		t.Error("Peak should be > 0")
	}
}

func TestGoroutineTracker_DoubleDone(t *testing.T) {
	tracker := NewGoroutineTracker(quietTrackerConfig())

	done := tracker.Track("test")

	done() // First call
	done() // Second call - should not panic or cause issues

	if tracker.Active() != 0 {
		t.Errorf("Active() = %d, want 0", tracker.Active())
	}
}

func TestGoroutineTracker_InterfaceCompliance(t *testing.T) {
	var _ GoroutineTrackable = (*GoroutineTracker)(nil)
}

// quietTrackerConfig returns a config with no logging
func quietTrackerConfig() GoroutineTrackerConfig {
	return GoroutineTrackerConfig{
		LongRunningThreshold: time.Hour, // Long threshold to avoid warnings
		Logger:               slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}
