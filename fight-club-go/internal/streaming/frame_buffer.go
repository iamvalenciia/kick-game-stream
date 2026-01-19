package streaming

import (
	"sync/atomic"
)

// BufferSize is the number of frame slots in the ring buffer.
// Increased from 8 to 16 for better backpressure handling during:
// - Combat peaks with many particles/effects
// - Network congestion or upload speed fluctuations
// - Initial RTMPS handshake latency spikes
// At 30fps: 16 frames = ~533ms buffer
// At 24fps: 16 frames = ~667ms buffer
// This gives FFmpeg more time to catch up during encoding/upload spikes.
const BufferSize = 16

// FrameRingBuffer provides lock-free frame buffering for backpressure handling.
// Uses a ring buffer with atomic operations to decouple frame production from FFmpeg writes.
// If the buffer is full, new frames are dropped (better than blocking the render loop).
type FrameRingBuffer struct {
	frames    [BufferSize][]byte // ring buffer for backpressure handling
	readIdx   uint32             // atomic - consumer index
	writeIdx  uint32             // atomic - producer index
	frameSize int

	// Stats
	framesWritten uint64
	framesDropped uint64
	framesRead    uint64
}

// NewFrameRingBuffer creates a new frame ring buffer with pre-allocated frames.
func NewFrameRingBuffer(frameSize int) *FrameRingBuffer {
	rb := &FrameRingBuffer{
		frameSize: frameSize,
	}

	// Pre-allocate all frame buffers
	for i := 0; i < BufferSize; i++ {
		rb.frames[i] = make([]byte, frameSize)
	}

	return rb
}

// TryWrite attempts to write a frame to the buffer.
// Returns true if successful, false if buffer is full (frame dropped).
// This method is lock-free and safe to call from the render goroutine.
func (rb *FrameRingBuffer) TryWrite(frame []byte) bool {
	if len(frame) != rb.frameSize {
		return false
	}

	currentWrite := atomic.LoadUint32(&rb.writeIdx)
	nextWrite := (currentWrite + 1) % BufferSize

	// Check if buffer is full (would catch up to reader)
	if nextWrite == atomic.LoadUint32(&rb.readIdx) {
		atomic.AddUint64(&rb.framesDropped, 1)
		return false // Buffer full, drop frame
	}

	// Copy frame to buffer slot
	copy(rb.frames[currentWrite], frame)

	// Advance write index
	atomic.StoreUint32(&rb.writeIdx, nextWrite)
	atomic.AddUint64(&rb.framesWritten, 1)

	return true
}

// TryRead attempts to read a frame from the buffer.
// Returns the frame data if available, nil if buffer is empty.
// This method is lock-free and safe to call from the writer goroutine.
func (rb *FrameRingBuffer) TryRead() []byte {
	readIdx := atomic.LoadUint32(&rb.readIdx)
	writeIdx := atomic.LoadUint32(&rb.writeIdx)

	// Check if buffer is empty
	if readIdx == writeIdx {
		return nil // Buffer empty
	}

	// Get frame from current read slot
	frame := rb.frames[readIdx]

	// Advance read index
	nextRead := (readIdx + 1) % BufferSize
	atomic.StoreUint32(&rb.readIdx, nextRead)
	atomic.AddUint64(&rb.framesRead, 1)

	return frame
}

// Available returns the number of frames available to read.
func (rb *FrameRingBuffer) Available() int {
	readIdx := atomic.LoadUint32(&rb.readIdx)
	writeIdx := atomic.LoadUint32(&rb.writeIdx)

	if writeIdx >= readIdx {
		return int(writeIdx - readIdx)
	}
	return int(BufferSize - readIdx + writeIdx)
}

// GetStats returns buffer statistics.
func (rb *FrameRingBuffer) GetStats() (written, dropped, read uint64) {
	return atomic.LoadUint64(&rb.framesWritten),
		atomic.LoadUint64(&rb.framesDropped),
		atomic.LoadUint64(&rb.framesRead)
}

// Reset resets the buffer indices and stats.
func (rb *FrameRingBuffer) Reset() {
	atomic.StoreUint32(&rb.readIdx, 0)
	atomic.StoreUint32(&rb.writeIdx, 0)
	atomic.StoreUint64(&rb.framesWritten, 0)
	atomic.StoreUint64(&rb.framesDropped, 0)
	atomic.StoreUint64(&rb.framesRead, 0)
}
