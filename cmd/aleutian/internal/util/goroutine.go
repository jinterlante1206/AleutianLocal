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
	"runtime/debug"
)

// =============================================================================
// Result Types
// =============================================================================

// SafeGoResult captures the result of a safe goroutine execution.
//
// # Description
//
// Contains information about a panic that occurred in a goroutine,
// including the panic value and full stack trace for debugging.
// This struct is passed to panic handlers to provide complete
// diagnostic information.
//
// # Thread Safety
//
// SafeGoResult is immutable after creation and safe for concurrent reads.
//
// # Example
//
//	SafeGo(func() {
//	    panic("something went wrong")
//	}, func(r SafeGoResult) {
//	    log.Printf("Panic: %v\nStack:\n%s", r.PanicValue, r.Stack)
//	})
//
// # Limitations
//
//   - Stack trace format depends on Go runtime version
//   - PanicValue may be any type, requiring type assertion for specific handling
type SafeGoResult struct {
	// PanicValue is the value passed to panic().
	// Can be any type (string, error, int, struct, etc.).
	PanicValue interface{}

	// Stack is the full stack trace at panic time.
	// Formatted by runtime/debug.Stack().
	Stack string
}

// =============================================================================
// Goroutine Safety Functions
// =============================================================================

// SafeGo runs a function in a goroutine with panic recovery.
//
// # Description
//
// Wraps a function execution in a goroutine with deferred panic recovery.
// If the function panics, the panic is caught and passed to the onPanic
// callback instead of crashing the application. This is essential for
// background operations where a panic should be logged but not terminate
// the entire process.
//
// # Inputs
//
//   - fn: The function to execute in the goroutine
//   - onPanic: Callback invoked if fn panics (may be nil to silently recover)
//
// # Outputs
//
//   - None (results passed via callbacks)
//
// # Example
//
//	var wg sync.WaitGroup
//	wg.Add(1)
//	SafeGo(func() {
//	    defer wg.Done()
//	    riskyOperation()
//	}, func(r SafeGoResult) {
//	    defer wg.Done()
//	    log.Printf("Operation failed: %v", r.PanicValue)
//	})
//	wg.Wait()
//
// # Limitations
//
//   - onPanic is called synchronously in the recovered goroutine
//   - If onPanic itself panics, the application will crash
//   - No way to return values from fn; use channels if needed
//
// # Assumptions
//
//   - fn is non-nil (will panic if nil)
func SafeGo(fn func(), onPanic func(SafeGoResult)) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				result := SafeGoResult{
					PanicValue: r,
					Stack:      string(debug.Stack()),
				}
				if onPanic != nil {
					onPanic(result)
				}
			}
		}()
		fn()
	}()
}

// SafeGoWithContext is like SafeGo but checks context before execution.
//
// # Description
//
// Similar to SafeGo but performs an initial context check before executing
// the function. If the context is already cancelled, the function is not
// executed at all. This prevents unnecessary work when the operation has
// already been cancelled.
//
// # Inputs
//
//   - ctx: Context to check for cancellation
//   - fn: The function to execute if context is valid
//   - onPanic: Callback invoked if fn panics (may be nil)
//
// # Outputs
//
//   - None
//
// # Example
//
//	ctx, cancel := context.WithCancel(context.Background())
//	cancel() // Already cancelled
//	SafeGoWithContext(ctx, func() {
//	    // This will NOT execute
//	}, nil)
//
// # Limitations
//
//   - Only checks context at start, not during execution
//   - fn should check ctx.Done() itself for long operations
//   - No notification when skipped due to cancellation
//
// # Assumptions
//
//   - ctx and fn are non-nil (will panic if nil)
func SafeGoWithContext(ctx context.Context, fn func(), onPanic func(SafeGoResult)) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				result := SafeGoResult{
					PanicValue: r,
					Stack:      string(debug.Stack()),
				}
				if onPanic != nil {
					onPanic(result)
				}
			}
		}()

		select {
		case <-ctx.Done():
			return
		default:
			fn()
		}
	}()
}

// RecoverPanic returns a deferred function that recovers panics.
//
// # Description
//
// Returns a function suitable for use with defer that recovers panics
// and passes them to the provided callback. Useful when you can't use
// SafeGo but still want panic recovery, such as in synchronous code
// or when you need to control the goroutine lifecycle yourself.
//
// # Inputs
//
//   - onPanic: Callback invoked if a panic is recovered (may be nil)
//
// # Outputs
//
//   - func(): A function to be deferred that performs panic recovery
//
// # Example
//
//	func riskyOperation() {
//	    defer RecoverPanic(func(r SafeGoResult) {
//	        log.Printf("Recovered: %v", r.PanicValue)
//	    })()
//	    // ... risky code that might panic
//	}
//
// # Limitations
//
//   - Must be called with () after defer: defer RecoverPanic(handler)()
//   - onPanic runs in the panicking goroutine
//   - After recovery, the function returns normally (does not re-panic)
//
// # Assumptions
//
//   - Called with defer statement
//   - The trailing () is not forgotten
func RecoverPanic(onPanic func(SafeGoResult)) func() {
	return func() {
		if r := recover(); r != nil {
			result := SafeGoResult{
				PanicValue: r,
				Stack:      string(debug.Stack()),
			}
			if onPanic != nil {
				onPanic(result)
			}
		}
	}
}
