package main

import (
	"sync"
	"testing"
)

func TestNewRingBuffer(t *testing.T) {
	buffer := NewRingBuffer[int](10)

	if buffer.Capacity() != 10 {
		t.Errorf("Capacity() = %d, want 10", buffer.Capacity())
	}
	if buffer.Size() != 0 {
		t.Errorf("Size() = %d, want 0", buffer.Size())
	}
	if !buffer.IsEmpty() {
		t.Error("IsEmpty() should be true for new buffer")
	}
	if buffer.IsFull() {
		t.Error("IsFull() should be false for new buffer")
	}
	if buffer.DroppedCount() != 0 {
		t.Errorf("DroppedCount() = %d, want 0", buffer.DroppedCount())
	}
}

func TestNewRingBuffer_PanicsOnZeroCapacity(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("NewRingBuffer(0) should panic")
		}
	}()
	NewRingBuffer[int](0)
}

func TestNewRingBuffer_PanicsOnNegativeCapacity(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("NewRingBuffer(-1) should panic")
		}
	}()
	NewRingBuffer[int](-1)
}

func TestRingBuffer_Push(t *testing.T) {
	buffer := NewRingBuffer[int](3)

	// First 3 pushes should not drop
	for i := 1; i <= 3; i++ {
		dropped := buffer.Push(i)
		if dropped {
			t.Errorf("Push(%d) should not have dropped", i)
		}
	}

	if buffer.Size() != 3 {
		t.Errorf("Size() = %d, want 3", buffer.Size())
	}
	if !buffer.IsFull() {
		t.Error("IsFull() should be true")
	}

	// 4th push should drop
	dropped := buffer.Push(4)
	if !dropped {
		t.Error("Push(4) should have dropped oldest")
	}

	if buffer.DroppedCount() != 1 {
		t.Errorf("DroppedCount() = %d, want 1", buffer.DroppedCount())
	}
}

func TestRingBuffer_Pop(t *testing.T) {
	buffer := NewRingBuffer[int](5)

	// Pop from empty buffer
	val, ok := buffer.Pop()
	if ok {
		t.Error("Pop() from empty buffer should return false")
	}
	if val != 0 {
		t.Errorf("Pop() value should be zero value, got %d", val)
	}

	// Add items and pop them
	buffer.Push(1)
	buffer.Push(2)
	buffer.Push(3)

	// Should pop in FIFO order
	val, ok = buffer.Pop()
	if !ok || val != 1 {
		t.Errorf("Pop() = (%d, %v), want (1, true)", val, ok)
	}

	val, ok = buffer.Pop()
	if !ok || val != 2 {
		t.Errorf("Pop() = (%d, %v), want (2, true)", val, ok)
	}

	val, ok = buffer.Pop()
	if !ok || val != 3 {
		t.Errorf("Pop() = (%d, %v), want (3, true)", val, ok)
	}

	// Buffer should be empty
	_, ok = buffer.Pop()
	if ok {
		t.Error("Pop() should return false for empty buffer")
	}
}

func TestRingBuffer_Peek(t *testing.T) {
	buffer := NewRingBuffer[string](3)

	// Peek empty buffer
	val, ok := buffer.Peek()
	if ok {
		t.Error("Peek() from empty buffer should return false")
	}
	if val != "" {
		t.Errorf("Peek() value should be zero value, got %q", val)
	}

	buffer.Push("first")
	buffer.Push("second")

	// Peek should return oldest without removing
	val, ok = buffer.Peek()
	if !ok || val != "first" {
		t.Errorf("Peek() = (%q, %v), want (first, true)", val, ok)
	}

	// Size should not change
	if buffer.Size() != 2 {
		t.Errorf("Size() = %d, want 2 (Peek should not remove)", buffer.Size())
	}

	// Peek again should return same value
	val, _ = buffer.Peek()
	if val != "first" {
		t.Errorf("Peek() = %q, want first", val)
	}
}

func TestRingBuffer_PopN(t *testing.T) {
	buffer := NewRingBuffer[int](10)

	// PopN from empty buffer
	items := buffer.PopN(5)
	if len(items) != 0 {
		t.Errorf("PopN(5) from empty = %d items, want 0", len(items))
	}

	// Add items
	for i := 1; i <= 10; i++ {
		buffer.Push(i)
	}

	// PopN less than size
	items = buffer.PopN(3)
	if len(items) != 3 {
		t.Errorf("PopN(3) returned %d items, want 3", len(items))
	}
	expected := []int{1, 2, 3}
	for i, v := range items {
		if v != expected[i] {
			t.Errorf("items[%d] = %d, want %d", i, v, expected[i])
		}
	}

	// PopN more than remaining
	items = buffer.PopN(100)
	if len(items) != 7 {
		t.Errorf("PopN(100) with 7 remaining returned %d items", len(items))
	}

	// Buffer should be empty
	if buffer.Size() != 0 {
		t.Errorf("Size() = %d, want 0", buffer.Size())
	}
}

func TestRingBuffer_PopN_EdgeCases(t *testing.T) {
	buffer := NewRingBuffer[int](5)
	buffer.Push(1)

	// PopN with n <= 0
	items := buffer.PopN(0)
	if len(items) != 0 {
		t.Errorf("PopN(0) = %d items, want 0", len(items))
	}

	items = buffer.PopN(-1)
	if len(items) != 0 {
		t.Errorf("PopN(-1) = %d items, want 0", len(items))
	}

	// Size unchanged
	if buffer.Size() != 1 {
		t.Errorf("Size() = %d, want 1", buffer.Size())
	}
}

func TestRingBuffer_Drain(t *testing.T) {
	buffer := NewRingBuffer[int](5)

	// Drain empty buffer
	items := buffer.Drain()
	if len(items) != 0 {
		t.Errorf("Drain() from empty = %d items, want 0", len(items))
	}

	// Add items and drain
	for i := 1; i <= 5; i++ {
		buffer.Push(i)
	}

	items = buffer.Drain()
	if len(items) != 5 {
		t.Errorf("Drain() = %d items, want 5", len(items))
	}

	for i, v := range items {
		if v != i+1 {
			t.Errorf("items[%d] = %d, want %d", i, v, i+1)
		}
	}

	// Buffer should be empty
	if buffer.Size() != 0 {
		t.Errorf("After Drain(), Size() = %d, want 0", buffer.Size())
	}
}

func TestRingBuffer_Clear(t *testing.T) {
	buffer := NewRingBuffer[int](5)

	for i := 0; i < 10; i++ {
		buffer.Push(i)
	}

	if buffer.DroppedCount() != 5 {
		t.Errorf("DroppedCount() = %d, want 5", buffer.DroppedCount())
	}

	buffer.Clear()

	if buffer.Size() != 0 {
		t.Errorf("After Clear(), Size() = %d, want 0", buffer.Size())
	}
	if buffer.DroppedCount() != 0 {
		t.Errorf("After Clear(), DroppedCount() = %d, want 0", buffer.DroppedCount())
	}
	if !buffer.IsEmpty() {
		t.Error("After Clear(), IsEmpty() should be true")
	}
}

func TestRingBuffer_ToSlice(t *testing.T) {
	buffer := NewRingBuffer[int](5)

	// ToSlice on empty buffer
	items := buffer.ToSlice()
	if len(items) != 0 {
		t.Errorf("ToSlice() on empty = %d items, want 0", len(items))
	}

	// Add items
	buffer.Push(1)
	buffer.Push(2)
	buffer.Push(3)

	// ToSlice should not modify buffer
	items = buffer.ToSlice()
	if len(items) != 3 {
		t.Errorf("ToSlice() = %d items, want 3", len(items))
	}
	if buffer.Size() != 3 {
		t.Errorf("Size() after ToSlice() = %d, want 3", buffer.Size())
	}

	// Verify order
	expected := []int{1, 2, 3}
	for i, v := range items {
		if v != expected[i] {
			t.Errorf("items[%d] = %d, want %d", i, v, expected[i])
		}
	}
}

func TestRingBuffer_Wraparound(t *testing.T) {
	buffer := NewRingBuffer[int](3)

	// Fill buffer
	buffer.Push(1)
	buffer.Push(2)
	buffer.Push(3)

	// Remove one
	val, _ := buffer.Pop()
	if val != 1 {
		t.Errorf("Pop() = %d, want 1", val)
	}

	// Add one (wraps around)
	buffer.Push(4)

	// Should have [2, 3, 4]
	items := buffer.ToSlice()
	expected := []int{2, 3, 4}
	if len(items) != 3 {
		t.Errorf("len(items) = %d, want 3", len(items))
	}
	for i, v := range items {
		if v != expected[i] {
			t.Errorf("items[%d] = %d, want %d", i, v, expected[i])
		}
	}
}

func TestRingBuffer_DroppedCount(t *testing.T) {
	buffer := NewRingBuffer[int](3)

	// Fill and overflow
	for i := 0; i < 10; i++ {
		buffer.Push(i)
	}

	// First 3 items dropped nothing, last 7 each dropped one
	if buffer.DroppedCount() != 7 {
		t.Errorf("DroppedCount() = %d, want 7", buffer.DroppedCount())
	}

	// Buffer should contain [7, 8, 9]
	items := buffer.Drain()
	expected := []int{7, 8, 9}
	for i, v := range items {
		if v != expected[i] {
			t.Errorf("items[%d] = %d, want %d", i, v, expected[i])
		}
	}
}

func TestRingBuffer_ConcurrentAccess(t *testing.T) {
	buffer := NewRingBuffer[int](100)

	var wg sync.WaitGroup
	const numWriters = 10
	const numReaders = 5
	const itemsPerWriter = 100

	// Start writers
	for w := 0; w < numWriters; w++ {
		wg.Add(1)
		go func(writer int) {
			defer wg.Done()
			for i := 0; i < itemsPerWriter; i++ {
				buffer.Push(writer*1000 + i)
			}
		}(w)
	}

	// Start readers
	for r := 0; r < numReaders; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < itemsPerWriter; i++ {
				buffer.Pop()
			}
		}()
	}

	wg.Wait()

	// No panics or data races = success
	// Size should be non-negative
	if buffer.Size() < 0 {
		t.Errorf("Size() = %d, should be non-negative", buffer.Size())
	}
}

func TestRingBuffer_GenericTypes(t *testing.T) {
	// Test with strings
	stringBuffer := NewRingBuffer[string](2)
	stringBuffer.Push("hello")
	stringBuffer.Push("world")
	val, _ := stringBuffer.Pop()
	if val != "hello" {
		t.Errorf("string buffer Pop() = %q, want hello", val)
	}

	// Test with structs
	type Item struct {
		ID   int
		Name string
	}
	structBuffer := NewRingBuffer[Item](2)
	structBuffer.Push(Item{1, "one"})
	structBuffer.Push(Item{2, "two"})
	item, _ := structBuffer.Pop()
	if item.ID != 1 || item.Name != "one" {
		t.Errorf("struct buffer Pop() = %+v, want {1 one}", item)
	}

	// Test with pointers
	ptrBuffer := NewRingBuffer[*Item](2)
	ptrBuffer.Push(&Item{1, "one"})
	ptrBuffer.Push(&Item{2, "two"})
	ptr, _ := ptrBuffer.Pop()
	if ptr.ID != 1 {
		t.Errorf("pointer buffer Pop().ID = %d, want 1", ptr.ID)
	}
}

func TestRingBuffer_IsEmpty_IsFull(t *testing.T) {
	buffer := NewRingBuffer[int](3)

	if !buffer.IsEmpty() {
		t.Error("New buffer should be empty")
	}
	if buffer.IsFull() {
		t.Error("New buffer should not be full")
	}

	buffer.Push(1)
	if buffer.IsEmpty() {
		t.Error("Buffer with 1 item should not be empty")
	}
	if buffer.IsFull() {
		t.Error("Buffer with 1/3 items should not be full")
	}

	buffer.Push(2)
	buffer.Push(3)
	if buffer.IsEmpty() {
		t.Error("Full buffer should not be empty")
	}
	if !buffer.IsFull() {
		t.Error("Buffer with 3/3 items should be full")
	}

	buffer.Pop()
	if buffer.IsFull() {
		t.Error("Buffer with 2/3 items should not be full")
	}
}

func BenchmarkRingBuffer_Push(b *testing.B) {
	buffer := NewRingBuffer[int](1000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buffer.Push(i)
	}
}

func BenchmarkRingBuffer_PushPop(b *testing.B) {
	buffer := NewRingBuffer[int](1000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buffer.Push(i)
		buffer.Pop()
	}
}
