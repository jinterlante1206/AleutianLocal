// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package history

// RingBuffer is a fixed-size circular buffer.
//
// # Description
//
// Provides O(1) push and bounded memory usage. When full, the oldest item
// is overwritten. Useful for keeping the last N events in memory.
//
// # Thread Safety
//
// NOT safe for concurrent use; caller must synchronize.
type RingBuffer[T any] struct {
	data  []T
	head  int  // Next write position
	tail  int  // First element position
	count int  // Current number of elements
	cap   int  // Maximum capacity
	full  bool // Whether buffer has wrapped
}

// NewRingBuffer creates a new ring buffer with the given capacity.
//
// # Inputs
//
//   - capacity: Maximum number of elements to store.
//
// # Outputs
//
//   - *RingBuffer[T]: Ready-to-use buffer.
func NewRingBuffer[T any](capacity int) *RingBuffer[T] {
	if capacity <= 0 {
		capacity = 100 // Default
	}
	return &RingBuffer[T]{
		data: make([]T, capacity),
		cap:  capacity,
	}
}

// Push adds an item to the buffer.
//
// # Description
//
// If the buffer is full, the oldest item is overwritten.
//
// # Inputs
//
//   - item: The item to add.
func (r *RingBuffer[T]) Push(item T) {
	r.data[r.head] = item
	r.head = (r.head + 1) % r.cap

	if r.full {
		r.tail = (r.tail + 1) % r.cap
	} else {
		r.count++
		if r.count == r.cap {
			r.full = true
		}
	}
}

// Pop removes and returns the oldest item.
//
// # Outputs
//
//   - T: The oldest item.
//   - bool: False if buffer is empty.
func (r *RingBuffer[T]) Pop() (T, bool) {
	var zero T
	if r.count == 0 {
		return zero, false
	}

	item := r.data[r.tail]
	r.data[r.tail] = zero // Clear reference
	r.tail = (r.tail + 1) % r.cap
	r.count--
	r.full = false

	return item, true
}

// Peek returns the oldest item without removing it.
//
// # Outputs
//
//   - T: The oldest item.
//   - bool: False if buffer is empty.
func (r *RingBuffer[T]) Peek() (T, bool) {
	var zero T
	if r.count == 0 {
		return zero, false
	}
	return r.data[r.tail], true
}

// PeekNewest returns the newest item without removing it.
//
// # Outputs
//
//   - T: The newest item.
//   - bool: False if buffer is empty.
func (r *RingBuffer[T]) PeekNewest() (T, bool) {
	var zero T
	if r.count == 0 {
		return zero, false
	}

	// head points to next write, so newest is at head-1
	idx := r.head - 1
	if idx < 0 {
		idx = r.cap - 1
	}
	return r.data[idx], true
}

// Slice returns all items from oldest to newest.
//
// # Description
//
// Creates a new slice containing all items in order from oldest to newest.
// The returned slice is a copy; modifications don't affect the buffer.
//
// # Outputs
//
//   - []T: All items in chronological order.
func (r *RingBuffer[T]) Slice() []T {
	if r.count == 0 {
		return nil
	}

	result := make([]T, r.count)

	if r.full {
		// Buffer has wrapped
		// Copy from tail to end
		n := copy(result, r.data[r.tail:])
		// Copy from start to head
		copy(result[n:], r.data[:r.head])
	} else {
		// Buffer hasn't wrapped, simple copy
		copy(result, r.data[r.tail:r.tail+r.count])
	}

	return result
}

// Len returns the current number of elements.
func (r *RingBuffer[T]) Len() int {
	return r.count
}

// Cap returns the maximum capacity.
func (r *RingBuffer[T]) Cap() int {
	return r.cap
}

// IsFull returns true if the buffer is at capacity.
func (r *RingBuffer[T]) IsFull() bool {
	return r.full
}

// IsEmpty returns true if the buffer has no elements.
func (r *RingBuffer[T]) IsEmpty() bool {
	return r.count == 0
}

// Clear removes all elements from the buffer.
func (r *RingBuffer[T]) Clear() {
	var zero T
	for i := range r.data {
		r.data[i] = zero
	}
	r.head = 0
	r.tail = 0
	r.count = 0
	r.full = false
}

// ForEach calls the function for each item from oldest to newest.
//
// # Inputs
//
//   - fn: Function to call for each item. Return false to stop iteration.
func (r *RingBuffer[T]) ForEach(fn func(item T) bool) {
	if r.count == 0 {
		return
	}

	for i := 0; i < r.count; i++ {
		idx := (r.tail + i) % r.cap
		if !fn(r.data[idx]) {
			return
		}
	}
}

// Filter returns a slice of items matching the predicate.
//
// # Inputs
//
//   - predicate: Function returning true for items to include.
//
// # Outputs
//
//   - []T: Matching items in chronological order.
func (r *RingBuffer[T]) Filter(predicate func(item T) bool) []T {
	var result []T

	r.ForEach(func(item T) bool {
		if predicate(item) {
			result = append(result, item)
		}
		return true
	})

	return result
}

// Last returns the last n items (newest first).
//
// # Inputs
//
//   - n: Number of items to return.
//
// # Outputs
//
//   - []T: Up to n items, newest first.
func (r *RingBuffer[T]) Last(n int) []T {
	if n <= 0 || r.count == 0 {
		return nil
	}

	if n > r.count {
		n = r.count
	}

	result := make([]T, n)
	for i := 0; i < n; i++ {
		idx := r.head - 1 - i
		if idx < 0 {
			idx += r.cap
		}
		result[i] = r.data[idx]
	}

	return result
}

// First returns the first n items (oldest first).
//
// # Inputs
//
//   - n: Number of items to return.
//
// # Outputs
//
//   - []T: Up to n items, oldest first.
func (r *RingBuffer[T]) First(n int) []T {
	if n <= 0 || r.count == 0 {
		return nil
	}

	if n > r.count {
		n = r.count
	}

	result := make([]T, n)
	for i := 0; i < n; i++ {
		idx := (r.tail + i) % r.cap
		result[i] = r.data[idx]
	}

	return result
}
