package streaming

import (
	"io"
	"log"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// MaxConsecutiveErrors before triggering connection lost callback
	MaxConsecutiveErrors = 10
	// ErrorResetInterval - reset error count if no errors for this duration
	ErrorResetInterval = 5 * time.Second
	// BackpressureThresholdMultiplier - only warn when write takes this many times longer than target
	BackpressureThresholdMultiplier = 3.0
	// BackpressureLogInterval - only log backpressure warning every N slow frames
	BackpressureLogInterval = 30
)

// AsyncFrameWriter handles non-blocking frame delivery to FFmpeg.
// It reads frames from a ring buffer and writes them to FFmpeg's stdin pipe.
// This isolates the render loop from FFmpeg backpressure.
type AsyncFrameWriter struct {
	ringBuffer *FrameRingBuffer
	pipe       io.WriteCloser
	stopChan   chan struct{}
	wg         sync.WaitGroup
	running    int32 // atomic

	// Stats
	framesWritten  uint64
	writeErrors    uint64
	lastWriteTime  time.Time
	avgWriteTimeNs int64

	// Backpressure tracking (to reduce log spam)
	slowFrameCount int32 // atomic - counts slow frames for log throttling

	// Connection health tracking
	consecutiveErrors int32          // atomic - consecutive write failures
	lastErrorTime     time.Time      // time of last error
	connectionLost    int32          // atomic - flag indicating connection is lost
	onConnectionLost  func()         // callback when connection is determined lost
	mu                sync.RWMutex   // protects callback and lastErrorTime
}

// NewAsyncFrameWriter creates a new async frame writer.
func NewAsyncFrameWriter(ringBuffer *FrameRingBuffer, pipe io.WriteCloser) *AsyncFrameWriter {
	return &AsyncFrameWriter{
		ringBuffer: ringBuffer,
		pipe:       pipe,
		stopChan:   make(chan struct{}),
	}
}

// SetOnConnectionLost sets a callback that will be called when the connection
// is determined to be lost (after MaxConsecutiveErrors consecutive write failures).
func (w *AsyncFrameWriter) SetOnConnectionLost(callback func()) {
	w.mu.Lock()
	w.onConnectionLost = callback
	w.mu.Unlock()
}

// IsConnectionLost returns true if the connection has been determined to be lost.
func (w *AsyncFrameWriter) IsConnectionLost() bool {
	return atomic.LoadInt32(&w.connectionLost) == 1
}

// Start begins the async writer goroutine.
// It pulls frames from the ring buffer and writes them to FFmpeg at a steady rate.
func (w *AsyncFrameWriter) Start(fps int) {
	if !atomic.CompareAndSwapInt32(&w.running, 0, 1) {
		return // Already running
	}

	// Reset connection state
	atomic.StoreInt32(&w.connectionLost, 0)
	atomic.StoreInt32(&w.consecutiveErrors, 0)

	w.stopChan = make(chan struct{})
	w.wg.Add(1)

	go func() {
		defer w.wg.Done()
		defer atomic.StoreInt32(&w.running, 0)

		// Target frame interval
		frameInterval := time.Second / time.Duration(fps)
		ticker := time.NewTicker(frameInterval)
		defer ticker.Stop()

		log.Printf("📡 AsyncFrameWriter started at %d FPS (%.2fms interval)", fps, frameInterval.Seconds()*1000)

		consecutiveEmpty := 0

		for {
			select {
			case <-w.stopChan:
				log.Println("📡 AsyncFrameWriter stopping...")
				return
			case <-ticker.C:
				// Skip processing if connection is already lost
				if atomic.LoadInt32(&w.connectionLost) == 1 {
					continue
				}

				frame := w.ringBuffer.TryRead()
				if frame == nil {
					consecutiveEmpty++
					// Log if we're starving (buffer empty for too long)
					if consecutiveEmpty == 30 { // ~1 second at 30fps
						log.Println("⚠️ AsyncFrameWriter: buffer starving - render loop may be too slow")
					}
					continue
				}
				consecutiveEmpty = 0

				// Write frame to FFmpeg
				startTime := time.Now()
				_, err := w.pipe.Write(frame)
				writeTime := time.Since(startTime)

				if err != nil {
					atomic.AddUint64(&w.writeErrors, 1)
					errCount := atomic.AddInt32(&w.consecutiveErrors, 1)

					// Log first few errors
					if errCount <= 5 {
						log.Printf("❌ AsyncFrameWriter write error (%d/%d): %v", errCount, MaxConsecutiveErrors, err)
					}

					// Update last error time
					w.mu.Lock()
					w.lastErrorTime = time.Now()
					w.mu.Unlock()

					// Check if we've hit the threshold for connection lost
					if errCount >= MaxConsecutiveErrors {
						if atomic.CompareAndSwapInt32(&w.connectionLost, 0, 1) {
							log.Printf("🔴 Connection lost detected after %d consecutive errors", errCount)

							// Trigger callback in separate goroutine to avoid blocking
							w.mu.RLock()
							callback := w.onConnectionLost
							w.mu.RUnlock()

							if callback != nil {
								go callback()
							}
						}
					}
					continue
				}

				// Successful write - reset consecutive error counter
				// Only reset if there were errors and enough time has passed
				if atomic.LoadInt32(&w.consecutiveErrors) > 0 {
					w.mu.RLock()
					lastErr := w.lastErrorTime
					w.mu.RUnlock()

					if time.Since(lastErr) > ErrorResetInterval {
						atomic.StoreInt32(&w.consecutiveErrors, 0)
						log.Println("✅ Connection recovered - error counter reset")
					}
				}

				atomic.AddUint64(&w.framesWritten, 1)
				w.lastWriteTime = time.Now()

				// Track average write time (exponential moving average)
				avgNs := atomic.LoadInt64(&w.avgWriteTimeNs)
				newAvg := (avgNs*9 + writeTime.Nanoseconds()) / 10
				atomic.StoreInt64(&w.avgWriteTimeNs, newAvg)

				// Warn if writes are taking significantly too long (throttled to reduce log spam)
				backpressureThreshold := time.Duration(float64(frameInterval) * BackpressureThresholdMultiplier)
				if writeTime > backpressureThreshold {
					count := atomic.AddInt32(&w.slowFrameCount, 1)
					if count%BackpressureLogInterval == 1 {
						log.Printf("⚠️ FFmpeg backpressure: write took %.2fms (target: %.2fms) - %d slow frames total",
							writeTime.Seconds()*1000, frameInterval.Seconds()*1000, count)
					}
				}
			}
		}
	}()
}

// Stop stops the async writer and waits for it to finish.
func (w *AsyncFrameWriter) Stop() {
	if !atomic.CompareAndSwapInt32(&w.running, 1, 0) {
		return // Not running
	}

	close(w.stopChan)
	w.wg.Wait()
	log.Println("📡 AsyncFrameWriter stopped")
}

// IsRunning returns whether the writer is currently running.
func (w *AsyncFrameWriter) IsRunning() bool {
	return atomic.LoadInt32(&w.running) == 1
}

// GetStats returns writer statistics.
func (w *AsyncFrameWriter) GetStats() map[string]interface{} {
	bufWritten, bufDropped, bufRead := w.ringBuffer.GetStats()

	return map[string]interface{}{
		"framesWritten":     atomic.LoadUint64(&w.framesWritten),
		"writeErrors":       atomic.LoadUint64(&w.writeErrors),
		"consecutiveErrors": atomic.LoadInt32(&w.consecutiveErrors),
		"connectionLost":    atomic.LoadInt32(&w.connectionLost) == 1,
		"avgWriteTimeMs":    float64(atomic.LoadInt64(&w.avgWriteTimeNs)) / 1e6,
		"bufferAvailable":   w.ringBuffer.Available(),
		"bufferWritten":     bufWritten,
		"bufferDropped":     bufDropped,
		"bufferRead":        bufRead,
	}
}
