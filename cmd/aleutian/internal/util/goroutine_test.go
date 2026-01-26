// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package util

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

// =============================================================================
// SafeGo Tests
// =============================================================================

// TestSafeGo_NoPanic verifies SafeGo executes function without panic.
//
// # Description
//
// When the function completes normally, no panic callback should be invoked.
func TestSafeGo_NoPanic(t *testing.T) {
	var wg sync.WaitGroup
	executed := false
	panicCalled := false

	wg.Add(1)
	SafeGo(func() {
		defer wg.Done()
		executed = true
	}, func(r SafeGoResult) {
		panicCalled = true
		wg.Done()
	})

	wg.Wait()

	if !executed {
		t.Error("function was not executed")
	}
	if panicCalled {
		t.Error("panic callback should not be called when no panic occurs")
	}
}

// TestSafeGo_WithPanic verifies SafeGo recovers from panic.
//
// # Description
//
// When the function panics, the panic should be recovered and passed to
// the onPanic callback with the panic value and stack trace.
func TestSafeGo_WithPanic(t *testing.T) {
	var wg sync.WaitGroup
	var result SafeGoResult
	panicCalled := false

	wg.Add(1)
	SafeGo(func() {
		panic("test panic")
	}, func(r SafeGoResult) {
		defer wg.Done()
		panicCalled = true
		result = r
	})

	wg.Wait()

	if !panicCalled {
		t.Fatal("panic callback was not called")
	}
	if result.PanicValue != "test panic" {
		t.Errorf("PanicValue = %v, want 'test panic'", result.PanicValue)
	}
	if result.Stack == "" {
		t.Error("Stack should not be empty")
	}
	if !strings.Contains(result.Stack, "goroutine") {
		t.Error("Stack should contain goroutine information")
	}
}

// TestSafeGo_WithPanicNilCallback verifies SafeGo handles nil callback.
//
// # Description
//
// When onPanic is nil, the panic should still be recovered (not crash).
func TestSafeGo_WithPanicNilCallback(t *testing.T) {
	var wg sync.WaitGroup
	wg.Add(1)

	// This should not crash even with nil callback
	SafeGo(func() {
		defer wg.Done()
		panic("test panic")
	}, nil)

	// Give goroutine time to complete
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success - goroutine completed without crashing
	case <-time.After(1 * time.Second):
		t.Error("timeout waiting for goroutine - may have crashed")
	}
}

// TestSafeGo_PanicWithDifferentTypes verifies SafeGo handles various panic types.
//
// # Description
//
// Panic values can be any type. Verify they are captured correctly.
func TestSafeGo_PanicWithDifferentTypes(t *testing.T) {
	tests := []struct {
		name       string
		panicValue interface{}
	}{
		{"string panic", "error message"},
		{"int panic", 42},
		{"error panic", testError{"wrapped error"}},
		{"struct panic", struct{ msg string }{"struct panic"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var wg sync.WaitGroup
			var result SafeGoResult

			wg.Add(1)
			SafeGo(func() {
				panic(tt.panicValue)
			}, func(r SafeGoResult) {
				defer wg.Done()
				result = r
			})

			wg.Wait()

			if result.PanicValue != tt.panicValue {
				t.Errorf("PanicValue = %v, want %v", result.PanicValue, tt.panicValue)
			}
		})
	}
}

// =============================================================================
// SafeGoWithContext Tests
// =============================================================================

// TestSafeGoWithContext_NotCancelled verifies execution when context is valid.
//
// # Description
//
// When context is not cancelled, the function should execute normally.
func TestSafeGoWithContext_NotCancelled(t *testing.T) {
	var wg sync.WaitGroup
	executed := false

	ctx := context.Background()

	wg.Add(1)
	SafeGoWithContext(ctx, func() {
		defer wg.Done()
		executed = true
	}, nil)

	wg.Wait()

	if !executed {
		t.Error("function should execute when context is not cancelled")
	}
}

// TestSafeGoWithContext_AlreadyCancelled verifies skip when context is cancelled.
//
// # Description
//
// When context is already cancelled, the function should not execute.
func TestSafeGoWithContext_AlreadyCancelled(t *testing.T) {
	executed := false

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel before SafeGoWithContext

	SafeGoWithContext(ctx, func() {
		executed = true
	}, nil)

	// Give goroutine time to potentially execute
	time.Sleep(50 * time.Millisecond)

	if executed {
		t.Error("function should NOT execute when context is already cancelled")
	}
}

// TestSafeGoWithContext_WithPanic verifies panic recovery with context.
//
// # Description
//
// When the function panics, it should be recovered just like SafeGo.
func TestSafeGoWithContext_WithPanic(t *testing.T) {
	var wg sync.WaitGroup
	var result SafeGoResult
	panicCalled := false

	ctx := context.Background()

	wg.Add(1)
	SafeGoWithContext(ctx, func() {
		panic("context panic")
	}, func(r SafeGoResult) {
		defer wg.Done()
		panicCalled = true
		result = r
	})

	wg.Wait()

	if !panicCalled {
		t.Fatal("panic callback was not called")
	}
	if result.PanicValue != "context panic" {
		t.Errorf("PanicValue = %v, want 'context panic'", result.PanicValue)
	}
}

// =============================================================================
// RecoverPanic Tests
// =============================================================================

// TestRecoverPanic_NoPanic verifies no callback when no panic occurs.
//
// # Description
//
// When used with defer and no panic occurs, callback should not be invoked.
func TestRecoverPanic_NoPanic(t *testing.T) {
	panicCalled := false

	func() {
		defer RecoverPanic(func(r SafeGoResult) {
			panicCalled = true
		})()

		// Normal execution, no panic
	}()

	if panicCalled {
		t.Error("panic callback should not be called when no panic occurs")
	}
}

// TestRecoverPanic_WithPanic verifies callback when panic occurs.
//
// # Description
//
// When used with defer and panic occurs, callback should receive panic info.
func TestRecoverPanic_WithPanic(t *testing.T) {
	var result SafeGoResult
	panicCalled := false

	func() {
		defer RecoverPanic(func(r SafeGoResult) {
			panicCalled = true
			result = r
		})()

		panic("deferred panic")
	}()

	if !panicCalled {
		t.Fatal("panic callback was not called")
	}
	if result.PanicValue != "deferred panic" {
		t.Errorf("PanicValue = %v, want 'deferred panic'", result.PanicValue)
	}
	if result.Stack == "" {
		t.Error("Stack should not be empty")
	}
}

// TestRecoverPanic_NilCallback verifies no crash with nil callback.
//
// # Description
//
// When callback is nil, panic should still be recovered.
func TestRecoverPanic_NilCallback(t *testing.T) {
	// This should not crash
	func() {
		defer RecoverPanic(nil)()
		panic("nil callback panic")
	}()

	// If we reach here, the panic was recovered successfully
}

// =============================================================================
// SafeGoResult Tests
// =============================================================================

// TestSafeGoResult_StackContainsUsefulInfo verifies stack trace content.
//
// # Description
//
// The stack trace should contain useful debugging information.
func TestSafeGoResult_StackContainsUsefulInfo(t *testing.T) {
	var wg sync.WaitGroup
	var result SafeGoResult

	wg.Add(1)
	SafeGo(func() {
		panic("stack test")
	}, func(r SafeGoResult) {
		defer wg.Done()
		result = r
	})

	wg.Wait()

	// Stack should contain:
	// 1. goroutine number
	if !strings.Contains(result.Stack, "goroutine") {
		t.Error("Stack should contain 'goroutine'")
	}

	// 2. Function names
	if !strings.Contains(result.Stack, "panic") {
		t.Error("Stack should contain 'panic'")
	}

	// 3. File paths
	if !strings.Contains(result.Stack, ".go:") {
		t.Error("Stack should contain file paths (.go:)")
	}
}

// =============================================================================
// Concurrency Tests
// =============================================================================

// TestSafeGo_MultipleConcurrent verifies multiple concurrent SafeGo calls.
//
// # Description
//
// Multiple SafeGo calls should work correctly in parallel.
func TestSafeGo_MultipleConcurrent(t *testing.T) {
	const numGoroutines = 100
	var wg sync.WaitGroup
	var mu sync.Mutex
	results := make([]int, 0, numGoroutines)

	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		i := i // capture loop variable
		SafeGo(func() {
			defer wg.Done()
			mu.Lock()
			results = append(results, i)
			mu.Unlock()
		}, nil)
	}

	wg.Wait()

	if len(results) != numGoroutines {
		t.Errorf("Expected %d results, got %d", numGoroutines, len(results))
	}
}

// TestSafeGo_MixedPanicAndNormal verifies mixed panic/normal executions.
//
// # Description
//
// Some goroutines panic, some complete normally. All should be handled.
func TestSafeGo_MixedPanicAndNormal(t *testing.T) {
	const numGoroutines = 50
	var wg sync.WaitGroup
	var mu sync.Mutex
	normalCount := 0
	panicCount := 0

	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		i := i
		SafeGo(func() {
			if i%2 == 0 {
				mu.Lock()
				normalCount++
				mu.Unlock()
				wg.Done()
			} else {
				panic("intentional panic")
			}
		}, func(r SafeGoResult) {
			mu.Lock()
			panicCount++
			mu.Unlock()
			wg.Done()
		})
	}

	wg.Wait()

	expectedNormal := numGoroutines / 2
	expectedPanic := numGoroutines / 2

	if normalCount != expectedNormal {
		t.Errorf("normalCount = %d, want %d", normalCount, expectedNormal)
	}
	if panicCount != expectedPanic {
		t.Errorf("panicCount = %d, want %d", panicCount, expectedPanic)
	}
}

// =============================================================================
// Helper Types
// =============================================================================

type testError struct {
	msg string
}

func (e testError) Error() string {
	return e.msg
}
