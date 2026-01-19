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
	// BackpressureWarningThreshold - warn if write time exceeds this multiple of frame interval
	BackpressureWarningThreshold = 2.0
	// SevereBackpressureThreshold - severe warning if write time exceeds this multiple
	SevereBackpressureThreshold = 5.0
	// BackpressureLogInterval - minimum time between backpressure warnings
	BackpressureLogInterval = 5 * time.Second
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

	// Backpressure tracking
	backpressureEvents  int64     // atomic - count of backpressure events
	severeBackpressure  int64     // atomic - count of severe backpressure events
	lastBackpressureLog time.Time // last time we logged a backpressure warning
	maxWriteTimeNs      int64     // atomic - max write time seen
	currentBitrate      int       // current configured bitrate (for recommendations)

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

// SetBitrate sets the current bitrate for connection quality recommendations
func (w *AsyncFrameWriter) SetBitrate(bitrate int) {
	w.mu.Lock()
	w.currentBitrate = bitrate
	w.mu.Unlock()
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

				// Track max write time
				maxNs := atomic.LoadInt64(&w.maxWriteTimeNs)
				if writeTime.Nanoseconds() > maxNs {
					atomic.StoreInt64(&w.maxWriteTimeNs, writeTime.Nanoseconds())
				}

				// Check for backpressure conditions
				writeRatio := float64(writeTime) / float64(frameInterval)

				if writeRatio >= SevereBackpressureThreshold {
					// Severe backpressure - connection/encoding can't keep up
					atomic.AddInt64(&w.severeBackpressure, 1)
					atomic.AddInt64(&w.backpressureEvents, 1)

					w.mu.RLock()
					lastLog := w.lastBackpressureLog
					bitrate := w.currentBitrate
					w.mu.RUnlock()

					if time.Since(lastLog) > BackpressureLogInterval {
						w.mu.Lock()
						w.lastBackpressureLog = time.Now()
						w.mu.Unlock()

						log.Printf("🔴 SEVERE BACKPRESSURE: FFmpeg write took %.0fms (target: %.1fms)",
							writeTime.Seconds()*1000, frameInterval.Seconds()*1000)
						log.Println("   This causes visible lag/stuttering in the stream!")
						log.Println("   Possible causes:")
						log.Println("   - Upload bandwidth too low for current bitrate")
						log.Println("   - Network congestion or packet loss")
						log.Println("   - CPU/GPU overload during encoding")
						if bitrate > 0 {
							recommendedBitrate := bitrate * 2 / 3 // Suggest 33% reduction
							if recommendedBitrate < 2000 {
								recommendedBitrate = 2000
							}
							log.Printf("   💡 Try reducing STREAM_BITRATE from %dk to %dk", bitrate, recommendedBitrate)
						}
					}
				} else if writeRatio >= BackpressureWarningThreshold {
					// Moderate backpressure - log warning occasionally
					atomic.AddInt64(&w.backpressureEvents, 1)

					w.mu.RLock()
					lastLog := w.lastBackpressureLog
					w.mu.RUnlock()

					if time.Since(lastLog) > BackpressureLogInterval {
						w.mu.Lock()
						w.lastBackpressureLog = time.Now()
						w.mu.Unlock()

						log.Printf("⚠️ Backpressure detected: FFmpeg write took %.0fms (target: %.1fms)",
							writeTime.Seconds()*1000, frameInterval.Seconds()*1000)
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
		"framesWritten":      atomic.LoadUint64(&w.framesWritten),
		"writeErrors":        atomic.LoadUint64(&w.writeErrors),
		"consecutiveErrors":  atomic.LoadInt32(&w.consecutiveErrors),
		"connectionLost":     atomic.LoadInt32(&w.connectionLost) == 1,
		"avgWriteTimeMs":     float64(atomic.LoadInt64(&w.avgWriteTimeNs)) / 1e6,
		"maxWriteTimeMs":     float64(atomic.LoadInt64(&w.maxWriteTimeNs)) / 1e6,
		"backpressureEvents": atomic.LoadInt64(&w.backpressureEvents),
		"severeBackpressure": atomic.LoadInt64(&w.severeBackpressure),
		"bufferAvailable":    w.ringBuffer.Available(),
		"bufferWritten":      bufWritten,
		"bufferDropped":      bufDropped,
		"bufferRead":         bufRead,
	}
}
