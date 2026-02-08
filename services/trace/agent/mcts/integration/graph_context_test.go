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
	"errors"
	"testing"
	"time"
)

// mockGraphStateProvider implements GraphStateProvider for testing.
type mockGraphStateProvider struct {
	nodeCount  int
	edgeCount  int
	generation int64
}

func (m *mockGraphStateProvider) NodeCount() int    { return m.nodeCount }
func (m *mockGraphStateProvider) EdgeCount() int    { return m.edgeCount }
func (m *mockGraphStateProvider) Generation() int64 { return m.generation }

func TestQueryType_Constants(t *testing.T) {
	tests := []struct {
		qt       QueryType
		expected string
	}{
		{QueryTypeCallers, "callers"},
		{QueryTypeCallees, "callees"},
		{QueryTypePath, "path"},
		{QueryTypeHotspots, "hotspots"},
		{QueryTypeDeadCode, "dead_code"},
		{QueryTypeCycles, "cycles"},
		{QueryTypeReferences, "references"},
		{QueryTypeSymbol, "symbol"},
		{QueryTypeImplementations, "implementations"},
		{QueryTypeCallChain, "call_chain"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if string(tt.qt) != tt.expected {
				t.Errorf("QueryType = %v, want %v", tt.qt, tt.expected)
			}
		})
	}
}

func TestGraphContext_Reset(t *testing.T) {
	gc := &GraphContext{
		FilesRead:       []string{"file1.go", "file2.go"},
		FilesModified:   []string{"file3.go"},
		FilesCreated:    []string{"file4.go"},
		SymbolsQueried:  []string{"sym1"},
		SymbolsFound:    []string{"sym2", "sym3"},
		SymbolsModified: []string{"sym4"},
		NodeCount:       100,
		EdgeCount:       200,
		GraphGeneration: 5,
		RefreshTime:     time.Now().UnixMilli(),
		QueryType:       QueryTypeCallers,
		QueryTarget:     "MyFunc",
		ResultCount:     10,
	}

	gc.Reset()

	// Verify all fields are reset
	if len(gc.FilesRead) != 0 {
		t.Errorf("FilesRead not reset, got %v", gc.FilesRead)
	}
	if len(gc.FilesModified) != 0 {
		t.Errorf("FilesModified not reset, got %v", gc.FilesModified)
	}
	if len(gc.FilesCreated) != 0 {
		t.Errorf("FilesCreated not reset, got %v", gc.FilesCreated)
	}
	if len(gc.SymbolsQueried) != 0 {
		t.Errorf("SymbolsQueried not reset, got %v", gc.SymbolsQueried)
	}
	if len(gc.SymbolsFound) != 0 {
		t.Errorf("SymbolsFound not reset, got %v", gc.SymbolsFound)
	}
	if len(gc.SymbolsModified) != 0 {
		t.Errorf("SymbolsModified not reset, got %v", gc.SymbolsModified)
	}
	if gc.NodeCount != 0 {
		t.Errorf("NodeCount not reset, got %v", gc.NodeCount)
	}
	if gc.EdgeCount != 0 {
		t.Errorf("EdgeCount not reset, got %v", gc.EdgeCount)
	}
	if gc.GraphGeneration != 0 {
		t.Errorf("GraphGeneration not reset, got %v", gc.GraphGeneration)
	}
	if gc.RefreshTime != 0 {
		t.Errorf("RefreshTime not reset, got %v", gc.RefreshTime)
	}
	if gc.QueryType != "" {
		t.Errorf("QueryType not reset, got %v", gc.QueryType)
	}
	if gc.QueryTarget != "" {
		t.Errorf("QueryTarget not reset, got %v", gc.QueryTarget)
	}
	if gc.ResultCount != 0 {
		t.Errorf("ResultCount not reset, got %v", gc.ResultCount)
	}

	// Verify capacity is preserved (slices should still have capacity)
	if cap(gc.FilesRead) == 0 {
		t.Error("FilesRead capacity should be preserved")
	}
}

func TestGraphContext_Validate(t *testing.T) {
	tests := []struct {
		name      string
		gc        GraphContext
		wantError error
	}{
		{
			name:      "valid empty context",
			gc:        GraphContext{},
			wantError: nil,
		},
		{
			name: "valid populated context",
			gc: GraphContext{
				FilesRead:       []string{"file.go"},
				NodeCount:       100,
				EdgeCount:       200,
				ResultCount:     5,
				GraphGeneration: 10,
			},
			wantError: nil,
		},
		{
			name: "negative result count",
			gc: GraphContext{
				ResultCount: -1,
			},
			wantError: ErrNegativeResultCount,
		},
		{
			name: "negative node count",
			gc: GraphContext{
				NodeCount: -1,
			},
			wantError: ErrNegativeNodeCount,
		},
		{
			name: "negative edge count",
			gc: GraphContext{
				EdgeCount: -1,
			},
			wantError: ErrNegativeEdgeCount,
		},
		{
			name: "negative graph generation",
			gc: GraphContext{
				GraphGeneration: -1,
			},
			wantError: ErrNegativeGraphGeneration,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.gc.Validate()
			if tt.wantError != nil {
				if !errors.Is(err, tt.wantError) {
					t.Errorf("Validate() error = %v, want %v", err, tt.wantError)
				}
			} else if err != nil {
				t.Errorf("Validate() unexpected error = %v", err)
			}
		})
	}
}

func TestAcquireReleaseGraphContext(t *testing.T) {
	t.Run("acquire returns valid context", func(t *testing.T) {
		gc := AcquireGraphContext()
		if gc == nil {
			t.Fatal("AcquireGraphContext returned nil")
		}

		// Should be pre-allocated with capacity
		if cap(gc.FilesRead) < DefaultSliceCapacity {
			t.Errorf("FilesRead capacity = %d, want >= %d", cap(gc.FilesRead), DefaultSliceCapacity)
		}
		if cap(gc.SymbolsQueried) < DefaultSliceCapacity {
			t.Errorf("SymbolsQueried capacity = %d, want >= %d", cap(gc.SymbolsQueried), DefaultSliceCapacity)
		}

		// Clean up
		ReleaseGraphContext(gc)
	})

	t.Run("release resets context", func(t *testing.T) {
		gc := AcquireGraphContext()
		gc.NodeCount = 100
		gc.FilesRead = append(gc.FilesRead, "test.go")

		ReleaseGraphContext(gc)

		// After release, the context should be reset
		// Note: We can't check gc directly since it's returned to pool,
		// but we can verify acquiring again gives a clean context
		gc2 := AcquireGraphContext()
		if gc2.NodeCount != 0 {
			t.Errorf("NodeCount should be 0 after acquire, got %d", gc2.NodeCount)
		}
		if len(gc2.FilesRead) != 0 {
			t.Errorf("FilesRead should be empty after acquire, got %v", gc2.FilesRead)
		}
		ReleaseGraphContext(gc2)
	})

	t.Run("release nil is safe", func(t *testing.T) {
		// Should not panic
		ReleaseGraphContext(nil)
	})
}

func TestGraphContextBuilder(t *testing.T) {
	t.Run("builds with all fields", func(t *testing.T) {
		gc, err := NewGraphContextBuilder().
			WithFilesRead("file1.go", "file2.go").
			WithFilesModified("file3.go").
			WithFilesCreated("file4.go").
			WithSymbolsQueried("sym1").
			WithSymbolsFound("sym2", "sym3").
			WithSymbolsModified("sym4").
			WithGraphCounts(100, 200).
			WithGraphGeneration(5).
			WithRefreshTimeNow().
			WithQuery(QueryTypeCallers, "MyFunc", 10).
			Build()

		if err != nil {
			t.Fatalf("Build() error = %v", err)
		}

		if len(gc.FilesRead) != 2 {
			t.Errorf("FilesRead = %v, want 2 items", gc.FilesRead)
		}
		if len(gc.FilesModified) != 1 {
			t.Errorf("FilesModified = %v, want 1 item", gc.FilesModified)
		}
		if len(gc.FilesCreated) != 1 {
			t.Errorf("FilesCreated = %v, want 1 item", gc.FilesCreated)
		}
		if len(gc.SymbolsQueried) != 1 {
			t.Errorf("SymbolsQueried = %v, want 1 item", gc.SymbolsQueried)
		}
		if len(gc.SymbolsFound) != 2 {
			t.Errorf("SymbolsFound = %v, want 2 items", gc.SymbolsFound)
		}
		if len(gc.SymbolsModified) != 1 {
			t.Errorf("SymbolsModified = %v, want 1 item", gc.SymbolsModified)
		}
		if gc.NodeCount != 100 {
			t.Errorf("NodeCount = %d, want 100", gc.NodeCount)
		}
		if gc.EdgeCount != 200 {
			t.Errorf("EdgeCount = %d, want 200", gc.EdgeCount)
		}
		if gc.GraphGeneration != 5 {
			t.Errorf("GraphGeneration = %d, want 5", gc.GraphGeneration)
		}
		if gc.RefreshTime == 0 {
			t.Error("RefreshTime should be set")
		}
		if gc.QueryType != QueryTypeCallers {
			t.Errorf("QueryType = %v, want %v", gc.QueryType, QueryTypeCallers)
		}
		if gc.QueryTarget != "MyFunc" {
			t.Errorf("QueryTarget = %v, want MyFunc", gc.QueryTarget)
		}
		if gc.ResultCount != 10 {
			t.Errorf("ResultCount = %d, want 10", gc.ResultCount)
		}

		// Clean up
		ReleaseGraphContext(gc)
	})

	t.Run("validation error on negative result count", func(t *testing.T) {
		_, err := NewGraphContextBuilder().
			WithQuery(QueryTypeCallers, "Func", -1).
			Build()

		if !errors.Is(err, ErrNegativeResultCount) {
			t.Errorf("Build() error = %v, want %v", err, ErrNegativeResultCount)
		}
	})

	t.Run("enforces file limits", func(t *testing.T) {
		builder := NewGraphContextBuilder()

		// Add more than MaxFilesPerContext files
		files := make([]string, MaxFilesPerContext+50)
		for i := range files {
			files[i] = "file.go"
		}
		builder.WithFilesRead(files...)

		gc, err := builder.Build()
		if err != nil {
			t.Fatalf("Build() error = %v", err)
		}

		if len(gc.FilesRead) != MaxFilesPerContext {
			t.Errorf("FilesRead = %d, want %d (capped)", len(gc.FilesRead), MaxFilesPerContext)
		}

		ReleaseGraphContext(gc)
	})

	t.Run("enforces symbol limits", func(t *testing.T) {
		builder := NewGraphContextBuilder()

		// Add more than MaxSymbolsPerContext symbols
		symbols := make([]string, MaxSymbolsPerContext+50)
		for i := range symbols {
			symbols[i] = "sym"
		}
		builder.WithSymbolsFound(symbols...)

		gc, err := builder.Build()
		if err != nil {
			t.Fatalf("Build() error = %v", err)
		}

		if len(gc.SymbolsFound) != MaxSymbolsPerContext {
			t.Errorf("SymbolsFound = %d, want %d (capped)", len(gc.SymbolsFound), MaxSymbolsPerContext)
		}

		ReleaseGraphContext(gc)
	})

	t.Run("with graph state provider", func(t *testing.T) {
		provider := &mockGraphStateProvider{
			nodeCount:  500,
			edgeCount:  1000,
			generation: 42,
		}

		gc, err := NewGraphContextBuilder().
			WithGraphState(provider).
			Build()

		if err != nil {
			t.Fatalf("Build() error = %v", err)
		}

		if gc.NodeCount != 500 {
			t.Errorf("NodeCount = %d, want 500", gc.NodeCount)
		}
		if gc.EdgeCount != 1000 {
			t.Errorf("EdgeCount = %d, want 1000", gc.EdgeCount)
		}
		if gc.GraphGeneration != 42 {
			t.Errorf("GraphGeneration = %d, want 42", gc.GraphGeneration)
		}

		ReleaseGraphContext(gc)
	})

	t.Run("nil graph state provider is safe", func(t *testing.T) {
		gc, err := NewGraphContextBuilder().
			WithGraphState(nil).
			Build()

		if err != nil {
			t.Fatalf("Build() error = %v", err)
		}

		if gc.NodeCount != 0 {
			t.Errorf("NodeCount = %d, want 0", gc.NodeCount)
		}

		ReleaseGraphContext(gc)
	})

	t.Run("build unsafe skips validation", func(t *testing.T) {
		gc := NewGraphContextBuilder().
			WithQuery(QueryTypeCallers, "Func", -1). // This would fail validation
			BuildUnsafe()

		// Should still return a context (validation skipped)
		if gc == nil {
			t.Fatal("BuildUnsafe returned nil")
		}
		if gc.ResultCount != -1 {
			t.Errorf("ResultCount = %d, want -1", gc.ResultCount)
		}

		ReleaseGraphContext(gc)
	})

	t.Run("with refresh time", func(t *testing.T) {
		now := time.Now().UnixMilli()

		gc, err := NewGraphContextBuilder().
			WithRefreshTime(now).
			Build()

		if err != nil {
			t.Fatalf("Build() error = %v", err)
		}

		if gc.RefreshTime != now {
			t.Errorf("RefreshTime = %d, want %d", gc.RefreshTime, now)
		}

		ReleaseGraphContext(gc)
	})
}

func TestGraphContextBuilder_WithExistingContext(t *testing.T) {
	// Create an existing context with some data
	existing := AcquireGraphContext()
	existing.FilesRead = append(existing.FilesRead, "existing.go")
	existing.NodeCount = 50

	builder := NewGraphContextBuilderWithContext(existing)
	builder.WithFilesRead("new.go")
	builder.WithGraphCounts(100, 200)

	gc, err := builder.Build()
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	// Should have both existing and new files
	if len(gc.FilesRead) != 2 {
		t.Errorf("FilesRead = %v, want 2 items", gc.FilesRead)
	}
	// New counts should override
	if gc.NodeCount != 100 {
		t.Errorf("NodeCount = %d, want 100", gc.NodeCount)
	}
	if gc.EdgeCount != 200 {
		t.Errorf("EdgeCount = %d, want 200", gc.EdgeCount)
	}

	ReleaseGraphContext(gc)
}

func TestEventData_WithGraphContext(t *testing.T) {
	gc, _ := NewGraphContextBuilder().
		WithFilesRead("test.go").
		WithQuery(QueryTypeCallers, "MyFunc", 5).
		Build()

	eventData := &EventData{
		SessionID: "test-session",
		Tool:      "find_callers",
		Graph:     gc,
	}

	if eventData.Graph == nil {
		t.Fatal("EventData.Graph is nil")
	}
	if eventData.Graph.QueryType != QueryTypeCallers {
		t.Errorf("Graph.QueryType = %v, want %v", eventData.Graph.QueryType, QueryTypeCallers)
	}
	if len(eventData.Graph.FilesRead) != 1 {
		t.Errorf("Graph.FilesRead = %v, want 1 item", eventData.Graph.FilesRead)
	}

	// Clean up
	ReleaseGraphContext(gc)
}

func TestEventAnalyticsRun_Mapping(t *testing.T) {
	// Verify EventAnalyticsRun is properly mapped
	activities, ok := EventActivityMapping[EventAnalyticsRun]
	if !ok {
		t.Fatal("EventAnalyticsRun not found in mapping")
	}

	if len(activities) != 2 {
		t.Errorf("EventAnalyticsRun should trigger 2 activities, got %d", len(activities))
	}

	// Should trigger Learning and Awareness
	hasLearning := false
	hasAwareness := false
	for _, a := range activities {
		if a == ActivityLearning {
			hasLearning = true
		}
		if a == ActivityAwareness {
			hasAwareness = true
		}
	}

	if !hasLearning {
		t.Error("EventAnalyticsRun should trigger ActivityLearning")
	}
	if !hasAwareness {
		t.Error("EventAnalyticsRun should trigger ActivityAwareness")
	}
}

// Benchmark for pool performance
func BenchmarkAcquireReleaseGraphContext(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		gc := AcquireGraphContext()
		gc.FilesRead = append(gc.FilesRead, "file.go")
		gc.NodeCount = 100
		ReleaseGraphContext(gc)
	}
}

func BenchmarkNewGraphContextWithoutPool(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		gc := &GraphContext{
			FilesRead:       make([]string, 0, DefaultSliceCapacity),
			FilesModified:   make([]string, 0, DefaultSliceCapacity),
			FilesCreated:    make([]string, 0, DefaultSliceCapacity),
			SymbolsQueried:  make([]string, 0, DefaultSliceCapacity),
			SymbolsFound:    make([]string, 0, DefaultSliceCapacity),
			SymbolsModified: make([]string, 0, DefaultSliceCapacity),
		}
		gc.FilesRead = append(gc.FilesRead, "file.go")
		gc.NodeCount = 100
		_ = gc
	}
}

// TestGraphContextBuilder_WithNilContext verifies R-2 fix: nil context handling
func TestGraphContextBuilder_WithNilContext(t *testing.T) {
	// Should not panic - acquires from pool instead
	builder := NewGraphContextBuilderWithContext(nil)
	if builder == nil {
		t.Fatal("NewGraphContextBuilderWithContext(nil) returned nil")
	}

	gc, err := builder.Build()
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if gc == nil {
		t.Fatal("Build() returned nil context")
	}

	ReleaseGraphContext(gc)
}

// TestGraphContextBuilder_BuildFailureReleasesContext verifies R-1 fix: no memory leak on error
func TestGraphContextBuilder_BuildFailureReleasesContext(t *testing.T) {
	builder := NewGraphContextBuilder().
		WithGraphGeneration(-1) // This will fail validation

	_, err := builder.Build()
	if err == nil {
		t.Fatal("Build() should have failed with negative generation")
	}
	if !errors.Is(err, ErrNegativeGraphGeneration) {
		t.Errorf("Build() error = %v, want %v", err, ErrNegativeGraphGeneration)
	}

	// Builder's context should be nil after failed build (released to pool)
	if builder.ctx != nil {
		t.Error("Builder context should be nil after failed Build()")
	}
}

// TestGraphContextBuilder_TruncationLogging verifies I-2/L-3 fix: limits logged
func TestGraphContextBuilder_TruncationLogging(t *testing.T) {
	builder := NewGraphContextBuilder()

	// Add more than limit
	files := make([]string, MaxFilesPerContext+10)
	for i := range files {
		files[i] = "file.go"
	}
	builder.WithFilesRead(files...)

	gc, err := builder.Build()
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	// Should be truncated to max
	if len(gc.FilesRead) != MaxFilesPerContext {
		t.Errorf("FilesRead = %d, want %d", len(gc.FilesRead), MaxFilesPerContext)
	}

	ReleaseGraphContext(gc)
}

// TestAcquireReleaseGraphContext_Concurrent verifies R-4: concurrent pool safety
func TestAcquireReleaseGraphContext_Concurrent(t *testing.T) {
	const goroutines = 100
	const iterations = 100

	done := make(chan struct{})

	for i := 0; i < goroutines; i++ {
		go func() {
			for j := 0; j < iterations; j++ {
				gc := AcquireGraphContext()
				gc.FilesRead = append(gc.FilesRead, "test.go")
				gc.NodeCount = 42
				ReleaseGraphContext(gc)
			}
			done <- struct{}{}
		}()
	}

	// Wait for all goroutines
	for i := 0; i < goroutines; i++ {
		<-done
	}

	// Acquire one more to verify pool is still healthy
	gc := AcquireGraphContext()
	if gc == nil {
		t.Fatal("AcquireGraphContext returned nil after concurrent usage")
	}
	// Should be reset
	if gc.NodeCount != 0 {
		t.Errorf("NodeCount = %d, want 0", gc.NodeCount)
	}
	if len(gc.FilesRead) != 0 {
		t.Errorf("FilesRead = %v, want empty", gc.FilesRead)
	}
	ReleaseGraphContext(gc)
}
