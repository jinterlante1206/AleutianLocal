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
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/agent/mcts/activities"
	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/agent/mcts/crs"
)

// -----------------------------------------------------------------------------
// Standard Go Benchmarks
// -----------------------------------------------------------------------------

func BenchmarkBridge_RunActivity(b *testing.B) {
	bridge := NewBridge(crs.New(nil), nil)
	activity := activities.NewSearchActivity(nil)
	input := activities.NewSearchInput("bench", "root", crs.SignalSourceHard)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = bridge.RunActivity(ctx, activity, input)
	}
}

func BenchmarkBridge_Apply(b *testing.B) {
	bridge := NewBridge(crs.New(nil), nil)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		updates := map[string]crs.ProofNumber{
			"node-bench": {
				Proof:    uint64(i),
				Disproof: 1,
				Status:   crs.ProofStatusExpanded,
				Source:   crs.SignalSourceHard,
			},
		}
		delta := crs.NewProofDelta(crs.SignalSourceHard, updates)
		_, _ = bridge.Apply(ctx, delta)
	}
}

func BenchmarkCoordinator_RunOnce(b *testing.B) {
	bridge := NewBridge(crs.New(nil), nil)
	coord := NewCoordinator(bridge, nil)
	coord.Register(activities.NewStreamingActivity(nil))
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = coord.RunOnce(ctx)
	}
}

func BenchmarkCoordinator_Schedule(b *testing.B) {
	bridge := NewBridge(crs.New(nil), nil)
	coord := NewCoordinator(bridge, nil)
	coord.Register(activities.NewSearchActivity(nil))
	coord.Register(activities.NewLearningActivity(nil))
	coord.Register(activities.NewConstraintActivity(nil))
	coord.Register(activities.NewPlanningActivity(nil))
	coord.Register(activities.NewAwarenessActivity(nil))
	coord.Register(activities.NewSimilarityActivity(nil))
	coord.Register(activities.NewStreamingActivity(nil))
	coord.Register(activities.NewMemoryActivity(nil))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = coord.Schedule()
	}
}

func BenchmarkABHarness_Process(b *testing.B) {
	exp := &mockAlgorithm{name: "experiment", output: "exp"}
	ctrl := &mockAlgorithm{name: "control", output: "ctrl"}
	harness := NewABHarness(exp, ctrl, &ABConfig{SampleRate: 0.0}) // No sampling for speed
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _ = harness.Process(ctx, nil, nil)
	}
}

func BenchmarkABHarness_ProcessWithSampling(b *testing.B) {
	exp := &mockAlgorithm{name: "experiment", output: "exp"}
	ctrl := &mockAlgorithm{name: "control", output: "ctrl"}
	harness := NewABHarness(exp, ctrl, &ABConfig{SampleRate: 1.0}) // 100% sampling
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _ = harness.Process(ctx, nil, nil)
	}
}

// -----------------------------------------------------------------------------
// Load Tests (100+ Concurrent Algorithms)
// -----------------------------------------------------------------------------

// TestLoadTest_100ConcurrentActivities tests the system under load with
// 100+ concurrent activity executions.
func TestLoadTest_100ConcurrentActivities(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping load test in short mode")
	}

	const numGoroutines = 100
	const opsPerGoroutine = 10

	bridge := NewBridge(crs.New(nil), nil)
	coord := NewCoordinator(bridge, nil)

	// Register all activities
	coord.Register(activities.NewSearchActivity(nil))
	coord.Register(activities.NewLearningActivity(nil))
	coord.Register(activities.NewConstraintActivity(nil))
	coord.Register(activities.NewPlanningActivity(nil))
	coord.Register(activities.NewAwarenessActivity(nil))
	coord.Register(activities.NewSimilarityActivity(nil))
	coord.Register(activities.NewStreamingActivity(nil))
	coord.Register(activities.NewMemoryActivity(nil))

	var wg sync.WaitGroup
	var successCount, errorCount atomic.Int64
	startTime := time.Now()

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			activityNames := []string{"search", "learning", "constraint", "planning",
				"awareness", "similarity", "streaming", "memory"}

			for j := 0; j < opsPerGoroutine; j++ {
				activityName := activityNames[(workerID+j)%len(activityNames)]
				var input activities.ActivityInput

				switch activityName {
				case "search":
					input = activities.NewSearchInput("load-test", "root", crs.SignalSourceHard)
				case "learning":
					input = activities.NewLearningInput("load-test", "conflict-node", crs.SignalSourceHard)
				case "constraint":
					input = activities.NewConstraintInput("load-test", "propagate", crs.SignalSourceHard)
				case "planning":
					input = activities.NewPlanningInput("load-test", "decompose", crs.SignalSourceHard)
				case "awareness":
					input = activities.NewAwarenessInput("load-test", crs.SignalSourceHard)
				case "similarity":
					input = activities.NewSimilarityInput("load-test", crs.SignalSourceHard)
				case "streaming":
					input = activities.NewStreamingInput("load-test", crs.SignalSourceHard)
				case "memory":
					input = activities.NewMemoryInput("load-test", "record", crs.SignalSourceHard)
				}

				_, err := coord.RunActivity(context.Background(), activityName, input)
				if err != nil {
					errorCount.Add(1)
				} else {
					successCount.Add(1)
				}
			}
		}(i)
	}

	wg.Wait()
	duration := time.Since(startTime)

	totalOps := int64(numGoroutines * opsPerGoroutine)
	t.Logf("Load test completed:")
	t.Logf("  Total operations: %d", totalOps)
	t.Logf("  Successful: %d", successCount.Load())
	t.Logf("  Errors: %d", errorCount.Load())
	t.Logf("  Duration: %v", duration)
	t.Logf("  Throughput: %.2f ops/sec", float64(totalOps)/duration.Seconds())

	// Verify reasonable success rate (allow some algorithm failures due to nil inputs)
	successRate := float64(successCount.Load()) / float64(totalOps)
	if successRate < 0.5 {
		t.Errorf("success rate too low: %.2f%%, expected >= 50%%", successRate*100)
	}
}

// TestLoadTest_100ConcurrentApplies tests concurrent CRS delta applications.
func TestLoadTest_100ConcurrentApplies(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping load test in short mode")
	}

	const numGoroutines = 100
	const opsPerGoroutine = 50

	bridge := NewBridge(crs.New(nil), nil)

	var wg sync.WaitGroup
	var successCount, errorCount atomic.Int64
	startTime := time.Now()

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			for j := 0; j < opsPerGoroutine; j++ {
				updates := map[string]crs.ProofNumber{
					"node-load-test": {
						Proof:    uint64(workerID*1000 + j),
						Disproof: uint64(j),
						Status:   crs.ProofStatusExpanded,
						Source:   crs.SignalSourceHard,
					},
				}
				delta := crs.NewProofDelta(crs.SignalSourceHard, updates)

				_, err := bridge.Apply(context.Background(), delta)
				if err != nil {
					errorCount.Add(1)
				} else {
					successCount.Add(1)
				}
			}
		}(i)
	}

	wg.Wait()
	duration := time.Since(startTime)

	totalOps := int64(numGoroutines * opsPerGoroutine)
	t.Logf("Concurrent Apply test completed:")
	t.Logf("  Total operations: %d", totalOps)
	t.Logf("  Successful: %d", successCount.Load())
	t.Logf("  Errors: %d", errorCount.Load())
	t.Logf("  Duration: %v", duration)
	t.Logf("  Throughput: %.2f ops/sec", float64(totalOps)/duration.Seconds())

	// All applies should succeed
	if errorCount.Load() > 0 {
		t.Errorf("unexpected errors during concurrent applies: %d", errorCount.Load())
	}
}

// TestLoadTest_CancellationUnderLoad tests context cancellation under heavy load.
func TestLoadTest_CancellationUnderLoad(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping load test in short mode")
	}

	const numGoroutines = 100

	bridge := NewBridge(crs.New(nil), nil)
	coord := NewCoordinator(bridge, nil)
	coord.Register(activities.NewStreamingActivity(nil))

	ctx, cancel := context.WithCancel(context.Background())

	var wg sync.WaitGroup
	var startedCount, cancelledCount atomic.Int64
	ready := make(chan struct{})

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			<-ready // Wait for all goroutines to be ready
			startedCount.Add(1)

			_, err := coord.RunOnce(ctx)
			if err != nil && err == context.Canceled {
				cancelledCount.Add(1)
			}
		}()
	}

	// Start all goroutines
	close(ready)

	// Give them a moment to start
	time.Sleep(10 * time.Millisecond)

	// Cancel while they're running
	cancel()

	// Wait for completion
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		t.Logf("Cancellation under load completed:")
		t.Logf("  Started: %d", startedCount.Load())
		t.Logf("  Cancelled: %d", cancelledCount.Load())
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for cancellation to complete")
	}
}

// TestLoadTest_ABHarnessUnderLoad tests A/B harness with 100+ concurrent processes.
func TestLoadTest_ABHarnessUnderLoad(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping load test in short mode")
	}

	const numGoroutines = 100
	const opsPerGoroutine = 20

	exp := &mockAlgorithm{name: "experiment", output: "exp"}
	ctrl := &mockAlgorithm{name: "control", output: "ctrl"}
	harness := NewABHarness(exp, ctrl, &ABConfig{SampleRate: 0.5})

	var wg sync.WaitGroup
	startTime := time.Now()

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for j := 0; j < opsPerGoroutine; j++ {
				_, _, _ = harness.Process(context.Background(), nil, nil)
			}
		}()
	}

	wg.Wait()
	duration := time.Since(startTime)

	stats := harness.Stats()
	totalOps := int64(numGoroutines * opsPerGoroutine)

	t.Logf("A/B Harness load test completed:")
	t.Logf("  Total requests: %d", stats.TotalRequests)
	t.Logf("  Sampled: %d (%.2f%%)", stats.SampledRequests, stats.SampleRate()*100)
	t.Logf("  Duration: %v", duration)
	t.Logf("  Throughput: %.2f ops/sec", float64(totalOps)/duration.Seconds())

	// Verify all requests were counted
	if stats.TotalRequests != totalOps {
		t.Errorf("expected %d total requests, got %d", totalOps, stats.TotalRequests)
	}

	// Sample rate should be approximately 50%
	if stats.SampleRate() < 0.4 || stats.SampleRate() > 0.6 {
		t.Errorf("sample rate outside expected range: %.2f%%", stats.SampleRate()*100)
	}
}

// -----------------------------------------------------------------------------
// Parallel Benchmarks
// -----------------------------------------------------------------------------

func BenchmarkBridge_RunActivity_Parallel(b *testing.B) {
	bridge := NewBridge(crs.New(nil), nil)
	activity := activities.NewSearchActivity(nil)
	ctx := context.Background()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			input := activities.NewSearchInput("bench", "root", crs.SignalSourceHard)
			_, _ = bridge.RunActivity(ctx, activity, input)
		}
	})
}

func BenchmarkBridge_Apply_Parallel(b *testing.B) {
	bridge := NewBridge(crs.New(nil), nil)
	ctx := context.Background()
	var counter atomic.Uint64

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			n := counter.Add(1)
			updates := map[string]crs.ProofNumber{
				"node-parallel": {
					Proof:    n,
					Disproof: 1,
					Status:   crs.ProofStatusExpanded,
					Source:   crs.SignalSourceHard,
				},
			}
			delta := crs.NewProofDelta(crs.SignalSourceHard, updates)
			_, _ = bridge.Apply(ctx, delta)
		}
	})
}

func BenchmarkCoordinator_RunOnce_Parallel(b *testing.B) {
	bridge := NewBridge(crs.New(nil), nil)
	coord := NewCoordinator(bridge, nil)
	coord.Register(activities.NewStreamingActivity(nil))
	ctx := context.Background()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, _ = coord.RunOnce(ctx)
		}
	})
}
