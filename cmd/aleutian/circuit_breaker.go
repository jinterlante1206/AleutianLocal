package main

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

// CircuitState represents the state of a circuit breaker.
//
// # States
//
//   - Closed: Normal operation, requests flow through
//   - Open: Circuit tripped, requests are rejected immediately
//   - HalfOpen: Testing if service recovered, limited requests allowed
//
// # State Diagram
//
//	   ┌─────────────────────────────────────┐
//	   │                                     │
//	   ▼                                     │
//	CLOSED ──[failure threshold]──► OPEN ───┘
//	   ▲                              │
//	   │                              │
//	   └───[success]◄── HALF_OPEN ◄──┘
//	                    [timeout]
type CircuitState int

const (
	// CircuitClosed is the normal operating state.
	CircuitClosed CircuitState = iota

	// CircuitOpen means the circuit has tripped and requests are rejected.
	CircuitOpen

	// CircuitHalfOpen means we're testing if the service has recovered.
	CircuitHalfOpen
)

// String returns a human-readable state name.
func (s CircuitState) String() string {
	switch s {
	case CircuitClosed:
		return "CLOSED"
	case CircuitOpen:
		return "OPEN"
	case CircuitHalfOpen:
		return "HALF_OPEN"
	default:
		return fmt.Sprintf("UNKNOWN(%d)", s)
	}
}

// ErrCircuitOpen is returned when the circuit breaker is open.
var ErrCircuitOpen = errors.New("circuit breaker is open")

// CircuitBreakerConfig configures circuit breaker behavior.
//
// # Description
//
// Controls how the circuit breaker responds to failures and recovers.
//
// # Example
//
//	config := CircuitBreakerConfig{
//	    FailureThreshold: 5,      // Open after 5 consecutive failures
//	    SuccessThreshold: 2,      // Close after 2 consecutive successes
//	    OpenTimeout:      30*time.Second,  // Stay open for 30s
//	}
type CircuitBreakerConfig struct {
	// FailureThreshold is consecutive failures before opening circuit.
	// Default: 5
	FailureThreshold int

	// SuccessThreshold is consecutive successes to close from half-open.
	// Default: 2
	SuccessThreshold int

	// OpenTimeout is how long to stay open before trying half-open.
	// Default: 30 seconds
	OpenTimeout time.Duration

	// OnStateChange is called when state transitions.
	// Called asynchronously to avoid blocking.
	OnStateChange func(from, to CircuitState)
}

// DefaultCircuitBreakerConfig returns sensible defaults.
func DefaultCircuitBreakerConfig() CircuitBreakerConfig {
	return CircuitBreakerConfig{
		FailureThreshold: 5,
		SuccessThreshold: 2,
		OpenTimeout:      30 * time.Second,
	}
}

// CircuitBreaker implements the circuit breaker pattern.
//
// # Description
//
// Prevents cascading failures by stopping requests to a failing service.
// After a timeout, allows limited requests to test if the service recovered.
//
// # Thread Safety
//
// CircuitBreaker is safe for concurrent use.
//
// # Example
//
//	cb := NewCircuitBreaker(DefaultCircuitBreakerConfig())
//
//	err := cb.Execute(func() error {
//	    return callExternalService()
//	})
//	if errors.Is(err, ErrCircuitOpen) {
//	    // Service is known to be down, fail fast
//	    return cachedResult, nil
//	}
type CircuitBreaker struct {
	config      CircuitBreakerConfig
	state       CircuitState
	failures    int
	successes   int
	lastFailure time.Time
	mu          sync.RWMutex
}

// NewCircuitBreaker creates a new circuit breaker.
//
// # Inputs
//
//   - config: Configuration for the circuit breaker
//
// # Outputs
//
//   - *CircuitBreaker: New circuit breaker in closed state
func NewCircuitBreaker(config CircuitBreakerConfig) *CircuitBreaker {
	// Apply defaults for zero values
	if config.FailureThreshold <= 0 {
		config.FailureThreshold = 5
	}
	if config.SuccessThreshold <= 0 {
		config.SuccessThreshold = 2
	}
	if config.OpenTimeout <= 0 {
		config.OpenTimeout = 30 * time.Second
	}

	return &CircuitBreaker{
		config: config,
		state:  CircuitClosed,
	}
}

// Execute runs the function if the circuit allows it.
//
// # Description
//
// Checks if the circuit allows the request, executes the function if so,
// and records the result (success or failure) to update the circuit state.
//
// # Inputs
//
//   - fn: The function to execute
//
// # Outputs
//
//   - error: ErrCircuitOpen if circuit is open, or error from fn
//
// # Example
//
//	err := cb.Execute(func() error {
//	    return client.Get("/health")
//	})
func (cb *CircuitBreaker) Execute(fn func() error) error {
	if !cb.allowRequest() {
		return ErrCircuitOpen
	}

	err := fn()
	cb.recordResult(err)
	return err
}

// allowRequest checks if a request should be allowed.
func (cb *CircuitBreaker) allowRequest() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case CircuitClosed:
		return true

	case CircuitOpen:
		// Check if we should transition to half-open
		if time.Since(cb.lastFailure) > cb.config.OpenTimeout {
			cb.transitionTo(CircuitHalfOpen)
			return true
		}
		return false

	case CircuitHalfOpen:
		// Allow limited requests in half-open to test recovery
		return true

	default:
		return false
	}
}

// recordResult records the result of an operation.
func (cb *CircuitBreaker) recordResult(err error) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if err != nil {
		cb.recordFailure()
	} else {
		cb.recordSuccess()
	}
}

func (cb *CircuitBreaker) recordFailure() {
	cb.failures++
	cb.successes = 0
	cb.lastFailure = time.Now()

	switch cb.state {
	case CircuitClosed:
		if cb.failures >= cb.config.FailureThreshold {
			cb.transitionTo(CircuitOpen)
		}
	case CircuitHalfOpen:
		// Any failure in half-open goes back to open
		cb.transitionTo(CircuitOpen)
	}
}

func (cb *CircuitBreaker) recordSuccess() {
	cb.successes++

	switch cb.state {
	case CircuitClosed:
		// Reset failure count on success
		cb.failures = 0
	case CircuitHalfOpen:
		if cb.successes >= cb.config.SuccessThreshold {
			cb.failures = 0
			cb.transitionTo(CircuitClosed)
		}
	}
}

func (cb *CircuitBreaker) transitionTo(state CircuitState) {
	if cb.state == state {
		return
	}

	old := cb.state
	cb.state = state

	if cb.config.OnStateChange != nil {
		// Call callback without holding lock to prevent deadlocks
		go cb.config.OnStateChange(old, state)
	}
}

// State returns the current circuit state.
func (cb *CircuitBreaker) State() CircuitState {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.state
}

// Failures returns the current consecutive failure count.
func (cb *CircuitBreaker) Failures() int {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.failures
}

// Reset forces the circuit to closed state.
//
// # Description
//
// Resets the circuit breaker to its initial closed state, clearing
// all failure and success counts. Use when you know the service
// has been fixed externally.
func (cb *CircuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	old := cb.state
	cb.state = CircuitClosed
	cb.failures = 0
	cb.successes = 0

	if old != CircuitClosed && cb.config.OnStateChange != nil {
		go cb.config.OnStateChange(old, CircuitClosed)
	}
}

// CircuitBreakerRegistry manages circuit breakers for multiple services.
//
// # Description
//
// Provides a centralized registry for circuit breakers, creating them
// on demand with consistent configuration.
//
// # Thread Safety
//
// CircuitBreakerRegistry is safe for concurrent use.
//
// # Example
//
//	registry := NewCircuitBreakerRegistry(DefaultCircuitBreakerConfig())
//	cb := registry.Get("weaviate")
//	cb.Execute(func() error { ... })
type CircuitBreakerRegistry struct {
	defaultConfig CircuitBreakerConfig
	breakers      map[string]*CircuitBreaker
	mu            sync.RWMutex
}

// NewCircuitBreakerRegistry creates a new registry.
//
// # Inputs
//
//   - defaultConfig: Default configuration for new circuit breakers
//
// # Outputs
//
//   - *CircuitBreakerRegistry: New empty registry
func NewCircuitBreakerRegistry(defaultConfig CircuitBreakerConfig) *CircuitBreakerRegistry {
	return &CircuitBreakerRegistry{
		defaultConfig: defaultConfig,
		breakers:      make(map[string]*CircuitBreaker),
	}
}

// Get returns the circuit breaker for a service, creating if needed.
//
// # Inputs
//
//   - name: Service name (used as key)
//
// # Outputs
//
//   - *CircuitBreaker: The circuit breaker for this service
func (r *CircuitBreakerRegistry) Get(name string) *CircuitBreaker {
	r.mu.RLock()
	cb, exists := r.breakers[name]
	r.mu.RUnlock()

	if exists {
		return cb
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// Double-check after acquiring write lock
	if cb, exists = r.breakers[name]; exists {
		return cb
	}

	cb = NewCircuitBreaker(r.defaultConfig)
	r.breakers[name] = cb
	return cb
}

// GetWithConfig returns a circuit breaker with custom config.
//
// # Inputs
//
//   - name: Service name
//   - config: Custom configuration for this breaker
//
// # Outputs
//
//   - *CircuitBreaker: The circuit breaker (existing or new)
func (r *CircuitBreakerRegistry) GetWithConfig(name string, config CircuitBreakerConfig) *CircuitBreaker {
	r.mu.Lock()
	defer r.mu.Unlock()

	if cb, exists := r.breakers[name]; exists {
		return cb
	}

	cb := NewCircuitBreaker(config)
	r.breakers[name] = cb
	return cb
}

// ResetAll resets all circuit breakers in the registry.
func (r *CircuitBreakerRegistry) ResetAll() {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, cb := range r.breakers {
		cb.Reset()
	}
}

// States returns the current state of all circuit breakers.
//
// # Outputs
//
//   - map[string]CircuitState: Map of service name to state
func (r *CircuitBreakerRegistry) States() map[string]CircuitState {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make(map[string]CircuitState, len(r.breakers))
	for name, cb := range r.breakers {
		result[name] = cb.State()
	}
	return result
}
