package streaming

import (
	"io"
	"log"
	"sync"
	"sync/atomic"
	"time"
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
}

// NewAsyncFrameWriter creates a new async frame writer.
func NewAsyncFrameWriter(ringBuffer *FrameRingBuffer, pipe io.WriteCloser) *AsyncFrameWriter {
	return &AsyncFrameWriter{
		ringBuffer: ringBuffer,
		pipe:       pipe,
		stopChan:   make(chan struct{}),
	}
}

// Start begins the async writer goroutine.
// It pulls frames from the ring buffer and writes them to FFmpeg at a steady rate.
func (w *AsyncFrameWriter) Start(fps int) {
	if !atomic.CompareAndSwapInt32(&w.running, 0, 1) {
		return // Already running
	}

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
					// Don't log every error, just track them
					if atomic.LoadUint64(&w.writeErrors) <= 5 {
						log.Printf("❌ AsyncFrameWriter write error: %v", err)
					}
					continue
				}

				atomic.AddUint64(&w.framesWritten, 1)
				w.lastWriteTime = time.Now()

				// Track average write time (exponential moving average)
				avgNs := atomic.LoadInt64(&w.avgWriteTimeNs)
				newAvg := (avgNs*9 + writeTime.Nanoseconds()) / 10
				atomic.StoreInt64(&w.avgWriteTimeNs, newAvg)

				// Warn if writes are taking too long
				if writeTime > frameInterval {
					log.Printf("⚠️ FFmpeg write took %.2fms (target: %.2fms) - possible backpressure",
						writeTime.Seconds()*1000, frameInterval.Seconds()*1000)
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
		"framesWritten":   atomic.LoadUint64(&w.framesWritten),
		"writeErrors":     atomic.LoadUint64(&w.writeErrors),
		"avgWriteTimeMs":  float64(atomic.LoadInt64(&w.avgWriteTimeNs)) / 1e6,
		"bufferAvailable": w.ringBuffer.Available(),
		"bufferWritten":   bufWritten,
		"bufferDropped":   bufDropped,
		"bufferRead":      bufRead,
	}
}
