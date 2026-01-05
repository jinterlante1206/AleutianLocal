package main

import (
	"sync"
	"sync/atomic"
)

// RingBufferable defines the interface for bounded ring buffer operations.
//
// # Description
//
// RingBufferable provides a fixed-size buffer that drops oldest items
// when full, preventing unbounded memory growth. Ideal for logging,
// metrics collection, and any producer-consumer scenario where dropping
// old data is acceptable.
//
// # Thread Safety
//
// Implementations must be safe for concurrent use from multiple goroutines.
type RingBufferable[T any] interface {
	// Push adds an item to the buffer. Returns true if an item was dropped.
	Push(item T) bool

	// Pop removes and returns the oldest item. Returns zero value and false if empty.
	Pop() (T, bool)

	// Peek returns the oldest item without removing it.
	Peek() (T, bool)

	// PopN removes and returns up to n oldest items.
	PopN(n int) []T

	// Drain removes and returns all items.
	Drain() []T

	// Size returns the current number of items.
	Size() int

	// Capacity returns the maximum capacity.
	Capacity() int

	// IsFull returns true if buffer is at capacity.
	IsFull() bool

	// IsEmpty returns true if buffer has no items.
	IsEmpty() bool

	// DroppedCount returns total items dropped due to capacity.
	DroppedCount() int64

	// Clear removes all items and resets dropped count.
	Clear()
}

// RingBuffer is a thread-safe, fixed-size circular buffer.
//
// # Description
//
// RingBuffer implements a circular buffer (ring buffer) that automatically
// drops the oldest items when full. This provides backpressure handling
// for producer-consumer scenarios where unbounded growth would cause OOM.
//
// # Use Cases
//
//   - Log collection where recent logs matter most
//   - Metrics buffering before flushing to disk
//   - Event queues with bounded memory
//   - Sliding window calculations
//
// # How It Works
//
//  1. Items are added at the tail position
//  2. Items are removed from the head position
//  3. When full, Push overwrites the oldest item
//  4. DroppedCount tracks how many items were dropped
//
// # Thread Safety
//
// RingBuffer is safe for concurrent use from multiple goroutines.
// All operations are protected by a mutex.
//
// # Limitations
//
//   - Fixed capacity (cannot grow)
//   - Drops oldest items when full (no backpressure signal)
//   - Memory is pre-allocated for full capacity
//
// # Assumptions
//
//   - Capacity is known and fixed at creation time
//   - Dropping old items is acceptable
//   - Items can be copied (stored by value)
//
// # Example
//
//	buffer := NewRingBuffer[string](100)
//
//	// Producer
//	if dropped := buffer.Push("log line"); dropped {
//	    // An old log was dropped to make room
//	}
//
//	// Consumer
//	items := buffer.PopN(10)
//	for _, item := range items {
//	    process(item)
//	}
type RingBuffer[T any] struct {
	buffer   []T
	head     int
	tail     int
	size     int
	capacity int
	dropped  int64
	mu       sync.Mutex
}

// NewRingBuffer creates a new ring buffer with the specified capacity.
//
// # Description
//
// Creates a ring buffer that can hold up to `capacity` items.
// The buffer is initially empty.
//
// # Inputs
//
//   - capacity: Maximum number of items to hold (must be > 0)
//
// # Outputs
//
//   - *RingBuffer[T]: New empty ring buffer
//
// # Panics
//
// Panics if capacity <= 0.
//
// # Example
//
//	// Create buffer for 1000 metric points
//	metrics := NewRingBuffer[MetricPoint](1000)
//
//	// Create buffer for log lines
//	logs := NewRingBuffer[string](500)
func NewRingBuffer[T any](capacity int) *RingBuffer[T] {
	if capacity <= 0 {
		panic("ring buffer capacity must be positive")
	}

	return &RingBuffer[T]{
		buffer:   make([]T, capacity),
		capacity: capacity,
	}
}

// Push adds an item to the buffer.
//
// # Description
//
// Adds the item to the tail of the buffer. If the buffer is full,
// the oldest item is dropped and DroppedCount is incremented.
//
// # Inputs
//
//   - item: Item to add
//
// # Outputs
//
//   - bool: true if an item was dropped to make room
//
// # Example
//
//	if dropped := buffer.Push(logLine); dropped {
//	    if buffer.DroppedCount() % 1000 == 0 {
//	        log.Printf("WARNING: Dropped %d items", buffer.DroppedCount())
//	    }
//	}
func (r *RingBuffer[T]) Push(item T) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	dropped := false

	if r.size == r.capacity {
		// Buffer is full, drop oldest
		r.head = (r.head + 1) % r.capacity
		r.size--
		atomic.AddInt64(&r.dropped, 1)
		dropped = true
	}

	r.buffer[r.tail] = item
	r.tail = (r.tail + 1) % r.capacity
	r.size++

	return dropped
}

// Pop removes and returns the oldest item.
//
// # Description
//
// Removes the oldest item from the buffer and returns it.
// Returns the zero value and false if the buffer is empty.
//
// # Outputs
//
//   - T: The oldest item (or zero value if empty)
//   - bool: true if an item was returned, false if empty
//
// # Example
//
//	for {
//	    item, ok := buffer.Pop()
//	    if !ok {
//	        break // Buffer empty
//	    }
//	    process(item)
//	}
func (r *RingBuffer[T]) Pop() (T, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.size == 0 {
		var zero T
		return zero, false
	}

	item := r.buffer[r.head]
	var zero T
	r.buffer[r.head] = zero // Clear reference for GC
	r.head = (r.head + 1) % r.capacity
	r.size--

	return item, true
}

// Peek returns the oldest item without removing it.
//
// # Description
//
// Returns a copy of the oldest item without modifying the buffer.
// Useful for inspection without consumption.
//
// # Outputs
//
//   - T: The oldest item (or zero value if empty)
//   - bool: true if an item exists, false if empty
//
// # Example
//
//	if oldest, ok := buffer.Peek(); ok {
//	    fmt.Printf("Next item will be: %v\n", oldest)
//	}
func (r *RingBuffer[T]) Peek() (T, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.size == 0 {
		var zero T
		return zero, false
	}

	return r.buffer[r.head], true
}

// PopN removes and returns up to n oldest items.
//
// # Description
//
// Removes and returns the oldest n items from the buffer.
// Returns fewer items if the buffer contains less than n items.
//
// # Inputs
//
//   - n: Maximum number of items to pop
//
// # Outputs
//
//   - []T: Slice of items (oldest first), may be shorter than n
//
// # Example
//
//	// Batch process up to 100 items
//	batch := buffer.PopN(100)
//	if len(batch) > 0 {
//	    writeToDisk(batch)
//	}
func (r *RingBuffer[T]) PopN(n int) []T {
	r.mu.Lock()
	defer r.mu.Unlock()

	if n <= 0 || r.size == 0 {
		return nil
	}

	count := n
	if count > r.size {
		count = r.size
	}

	result := make([]T, count)
	var zero T

	for i := 0; i < count; i++ {
		result[i] = r.buffer[r.head]
		r.buffer[r.head] = zero // Clear for GC
		r.head = (r.head + 1) % r.capacity
		r.size--
	}

	return result
}

// Drain removes and returns all items.
//
// # Description
//
// Removes all items from the buffer and returns them.
// The buffer is empty after this call.
//
// # Outputs
//
//   - []T: All items (oldest first)
//
// # Example
//
//	// Flush all buffered items on shutdown
//	items := buffer.Drain()
//	for _, item := range items {
//	    flush(item)
//	}
func (r *RingBuffer[T]) Drain() []T {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.size == 0 {
		return nil
	}

	result := make([]T, r.size)
	var zero T

	for i := 0; i < len(result); i++ {
		result[i] = r.buffer[r.head]
		r.buffer[r.head] = zero
		r.head = (r.head + 1) % r.capacity
	}

	r.size = 0
	return result
}

// Size returns the current number of items.
//
// # Description
//
// Returns the number of items currently in the buffer.
//
// # Outputs
//
//   - int: Current item count
func (r *RingBuffer[T]) Size() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.size
}

// Capacity returns the maximum capacity.
//
// # Description
//
// Returns the maximum number of items the buffer can hold.
//
// # Outputs
//
//   - int: Maximum capacity
func (r *RingBuffer[T]) Capacity() int {
	return r.capacity // Immutable, no lock needed
}

// IsFull returns true if buffer is at capacity.
//
// # Description
//
// Returns whether the buffer is completely full. The next Push
// will cause an item to be dropped.
//
// # Outputs
//
//   - bool: true if full
func (r *RingBuffer[T]) IsFull() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.size == r.capacity
}

// IsEmpty returns true if buffer has no items.
//
// # Description
//
// Returns whether the buffer contains no items.
//
// # Outputs
//
//   - bool: true if empty
func (r *RingBuffer[T]) IsEmpty() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.size == 0
}

// DroppedCount returns total items dropped due to capacity.
//
// # Description
//
// Returns the total number of items that have been dropped since
// the buffer was created (or since Clear was called).
//
// # Outputs
//
//   - int64: Total dropped count
//
// # Example
//
//	if buffer.DroppedCount() > 0 {
//	    log.Printf("WARNING: %d items dropped", buffer.DroppedCount())
//	}
func (r *RingBuffer[T]) DroppedCount() int64 {
	return atomic.LoadInt64(&r.dropped)
}

// Clear removes all items and resets dropped count.
//
// # Description
//
// Removes all items from the buffer and resets the dropped count
// to zero. The capacity remains unchanged.
func (r *RingBuffer[T]) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()

	var zero T
	for i := 0; i < r.capacity; i++ {
		r.buffer[i] = zero
	}

	r.head = 0
	r.tail = 0
	r.size = 0
	atomic.StoreInt64(&r.dropped, 0)
}

// ToSlice returns a copy of all items without removing them.
//
// # Description
//
// Returns a snapshot of all items in the buffer. The buffer
// is not modified. Items are returned oldest-first.
//
// # Outputs
//
//   - []T: Copy of all items
//
// # Example
//
//	// Inspect buffer contents
//	items := buffer.ToSlice()
//	for i, item := range items {
//	    fmt.Printf("[%d] %v\n", i, item)
//	}
func (r *RingBuffer[T]) ToSlice() []T {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.size == 0 {
		return nil
	}

	result := make([]T, r.size)
	idx := r.head

	for i := 0; i < r.size; i++ {
		result[i] = r.buffer[idx]
		idx = (idx + 1) % r.capacity
	}

	return result
}
