package chat

import (
	"log"
	"sync"
	"sync/atomic"
	"time"
)

// CommandQueue provides a non-blocking queue for chat commands with worker pool processing.
// This decouples webhook handlers from game engine operations, eliminating latency caused
// by synchronous command processing.
type CommandQueue struct {
	commands    chan ChatCommand
	handler     *Handler
	workers     int
	wg          sync.WaitGroup
	running     atomic.Bool
	stopChan    chan struct{}

	// Metrics
	enqueued    atomic.Uint64
	processed   atomic.Uint64
	dropped     atomic.Uint64
	avgWaitTime atomic.Int64 // nanoseconds, exponential moving average
}

// QueueConfig holds configuration for the command queue
type QueueConfig struct {
	BufferSize int // Number of commands to buffer (default: 256)
	Workers    int // Number of worker goroutines (default: 4)
}

// DefaultQueueConfig returns sensible defaults for production
func DefaultQueueConfig() QueueConfig {
	return QueueConfig{
		BufferSize: 256, // ~10 seconds of commands at peak load
		Workers:    4,   // Enough parallelism without lock contention
	}
}

// NewCommandQueue creates a new command queue with worker pool
func NewCommandQueue(handler *Handler, config QueueConfig) *CommandQueue {
	if config.BufferSize <= 0 {
		config.BufferSize = 256
	}
	if config.Workers <= 0 {
		config.Workers = 4
	}

	return &CommandQueue{
		commands: make(chan ChatCommand, config.BufferSize),
		handler:  handler,
		workers:  config.Workers,
		stopChan: make(chan struct{}),
	}
}

// Start launches the worker pool
func (q *CommandQueue) Start() {
	if q.running.Swap(true) {
		return // Already running
	}

	log.Printf("🚀 CommandQueue starting with %d workers, buffer size %d", q.workers, cap(q.commands))

	for i := 0; i < q.workers; i++ {
		q.wg.Add(1)
		go q.worker(i)
	}
}

// Stop gracefully shuts down the queue
func (q *CommandQueue) Stop() {
	if !q.running.Swap(false) {
		return // Not running
	}

	close(q.stopChan)
	q.wg.Wait()

	log.Printf("📊 CommandQueue stopped - enqueued: %d, processed: %d, dropped: %d",
		q.enqueued.Load(), q.processed.Load(), q.dropped.Load())
}

// Enqueue adds a command to the queue (non-blocking)
// Returns true if enqueued, false if queue is full (command dropped)
func (q *CommandQueue) Enqueue(cmd ChatCommand) bool {
	// Set receive time for latency tracking
	cmd.ReceivedAt = time.Now()

	select {
	case q.commands <- cmd:
		q.enqueued.Add(1)
		return true
	default:
		// Queue full - drop command to prevent backpressure
		q.dropped.Add(1)
		if q.dropped.Load()%100 == 1 {
			log.Printf("⚠️ CommandQueue full, dropped command from %s (total dropped: %d)",
				cmd.Username, q.dropped.Load())
		}
		return false
	}
}

// worker processes commands from the queue
func (q *CommandQueue) worker(id int) {
	defer q.wg.Done()

	for {
		select {
		case <-q.stopChan:
			return
		case cmd, ok := <-q.commands:
			if !ok {
				return
			}

			// Track wait time
			waitTime := time.Since(cmd.ReceivedAt)
			q.updateAvgWaitTime(waitTime)

			// Warn if commands are waiting too long
			if waitTime > 100*time.Millisecond {
				log.Printf("⚠️ Command from %s waited %.1fms in queue",
					cmd.Username, float64(waitTime.Microseconds())/1000)
			}

			// Process the command
			q.handler.ProcessCommand(cmd)
			q.processed.Add(1)
		}
	}
}

// updateAvgWaitTime updates exponential moving average
func (q *CommandQueue) updateAvgWaitTime(waitTime time.Duration) {
	current := q.avgWaitTime.Load()
	// EMA with alpha = 0.1 (smooth over ~10 samples)
	newAvg := (current*9 + waitTime.Nanoseconds()) / 10
	q.avgWaitTime.Store(newAvg)
}

// Stats returns current queue statistics
func (q *CommandQueue) Stats() QueueStats {
	return QueueStats{
		Enqueued:       q.enqueued.Load(),
		Processed:      q.processed.Load(),
		Dropped:        q.dropped.Load(),
		Pending:        uint64(len(q.commands)),
		BufferSize:     uint64(cap(q.commands)),
		AvgWaitTimeMs:  float64(q.avgWaitTime.Load()) / 1e6,
		BufferUsagePct: float64(len(q.commands)) / float64(cap(q.commands)) * 100,
	}
}

// QueueStats holds queue metrics
type QueueStats struct {
	Enqueued       uint64  `json:"enqueued"`
	Processed      uint64  `json:"processed"`
	Dropped        uint64  `json:"dropped"`
	Pending        uint64  `json:"pending"`
	BufferSize     uint64  `json:"buffer_size"`
	AvgWaitTimeMs  float64 `json:"avg_wait_time_ms"`
	BufferUsagePct float64 `json:"buffer_usage_pct"`
}
