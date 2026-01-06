package main

import (
	"context"
	"runtime/debug"
)

// SafeGoResult captures the result of a safe goroutine execution.
//
// # Description
//
// Contains information about a panic that occurred in a goroutine,
// including the panic value and full stack trace for debugging.
//
// # Example
//
//	SafeGo(func() {
//	    panic("something went wrong")
//	}, func(r SafeGoResult) {
//	    log.Printf("Panic: %v\nStack:\n%s", r.PanicValue, r.Stack)
//	})
type SafeGoResult struct {
	// PanicValue is the value passed to panic().
	PanicValue interface{}

	// Stack is the full stack trace at panic time.
	Stack string
}

// SafeGo runs a function in a goroutine with panic recovery.
//
// # Description
//
// Wraps a function execution in a goroutine with deferred panic recovery.
// If the function panics, the panic is caught and passed to the onPanic
// callback instead of crashing the application.
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
//
// # Assumptions
//
//   - fn is non-nil
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
// executed at all.
//
// # Inputs
//
//   - ctx: Context to check for cancellation
//   - fn: The function to execute if context is valid
//   - onPanic: Callback invoked if fn panics
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
//
// # Assumptions
//
//   - ctx and fn are non-nil
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
// SafeGo but still want panic recovery.
//
// # Inputs
//
//   - onPanic: Callback invoked if a panic is recovered
//
// # Outputs
//
//   - func(): A function to be deferred
//
// # Example
//
//	func riskyOperation() {
//	    defer RecoverPanic(func(r SafeGoResult) {
//	        log.Printf("Recovered: %v", r.PanicValue)
//	    })()
//	    // ... risky code
//	}
//
// # Limitations
//
//   - Must be called with () after defer
//   - onPanic runs in the panicking goroutine
//
// # Assumptions
//
//   - Called with defer
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
