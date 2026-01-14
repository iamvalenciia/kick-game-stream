package game

import (
	"encoding/json"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/time/rate"
)

const (
	EventBufferSize      = 1024                   // Circular buffer size
	MaxEventsPerSec      = 10000                  // Global rate limit
	MaxEventsPerPlayer   = 100                    // Per-player rate limit per second
	BatchFlushSize       = 64                     // Events per batch write
	BatchFlushInterval   = 100 * time.Millisecond // How often to flush
	PlayerLimiterCleanup = 5 * time.Minute        // Cleanup interval for player limiters
)

// EventLog provides bounded, rate-limited event logging with backpressure
type EventLog struct {
	// Circular buffer (lock-free SPSC pattern)
	buffer    [EventBufferSize]Event
	writeHead uint64 // atomic - producer position
	readHead  uint64 // atomic - consumer position

	// Rate limiting for DoS protection
	globalLimiter  *rate.Limiter
	playerLimiters sync.Map // map[string]*playerLimiterEntry

	// Async writer
	writerWg sync.WaitGroup
	stopChan chan struct{}
	stopOnce sync.Once
	running  atomic.Bool

	// File output
	filePath string
	file     *os.File
	fileMu   sync.Mutex

	// Stats for DoS detection and monitoring
	droppedCount uint64 // atomic
	totalCount   uint64 // atomic
}

// playerLimiterEntry tracks per-player rate limiting
type playerLimiterEntry struct {
	limiter  *rate.Limiter
	lastUsed time.Time
}

// NewEventLog creates a new bounded event log
func NewEventLog() *EventLog {
	el := &EventLog{
		globalLimiter: rate.NewLimiter(MaxEventsPerSec, MaxEventsPerSec/10),
		stopChan:      make(chan struct{}),
	}
	return el
}

// Start begins the async writer goroutine
func (el *EventLog) Start(filePath string) error {
	if el.running.Load() {
		return nil
	}

	el.filePath = filePath

	// Open file for append
	if filePath != "" {
		file, err := os.OpenFile(filePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			return err
		}
		el.file = file
	}

	el.running.Store(true)
	el.writerWg.Add(2)
	go el.writerLoop()
	go el.cleanupLoop()

	return nil
}

// Stop gracefully shuts down the event log
func (el *EventLog) Stop() {
	el.stopOnce.Do(func() {
		el.running.Store(false)
		close(el.stopChan)
		el.writerWg.Wait()

		el.fileMu.Lock()
		if el.file != nil {
			el.file.Close()
		}
		el.fileMu.Unlock()
	})
}

// Emit adds an event with rate limiting
// Returns false if rate limited or buffer full (DoS protection)
func (el *EventLog) Emit(event Event) bool {
	if !el.running.Load() {
		return false
	}

	// Global rate limit check
	if !el.globalLimiter.Allow() {
		atomic.AddUint64(&el.droppedCount, 1)
		return false
	}

	// Per-player rate limit (prevents single attacker from flooding)
	if event.PlayerID != "" {
		limiter := el.getPlayerLimiter(event.PlayerID)
		if !limiter.Allow() {
			atomic.AddUint64(&el.droppedCount, 1)
			return false
		}
	}

	// Acquire write slot in circular buffer
	head := atomic.AddUint64(&el.writeHead, 1)
	tail := atomic.LoadUint64(&el.readHead)

	// Check if buffer is full (DoS backpressure)
	if head-tail >= EventBufferSize {
		// Drop oldest events (rolling window) - this is intentional under attack
		atomic.AddUint64(&el.readHead, 1)
		atomic.AddUint64(&el.droppedCount, 1)
	}

	// Assign sequence number and write to buffer
	event.Sequence = head
	idx := head % EventBufferSize
	el.buffer[idx] = event

	atomic.AddUint64(&el.totalCount, 1)
	return true
}

// EmitSimple is a convenience method to emit an event with automatic creation
func (el *EventLog) EmitSimple(eventType EventType, tickNum uint64, playerID string, payload interface{}) bool {
	event := NewEvent(eventType, tickNum, playerID, payload)
	return el.Emit(event)
}

// getPlayerLimiter returns/creates a per-player rate limiter
func (el *EventLog) getPlayerLimiter(playerID string) *rate.Limiter {
	if entry, ok := el.playerLimiters.Load(playerID); ok {
		e := entry.(*playerLimiterEntry)
		e.lastUsed = time.Now()
		return e.limiter
	}

	entry := &playerLimiterEntry{
		limiter:  rate.NewLimiter(MaxEventsPerPlayer, MaxEventsPerPlayer/10),
		lastUsed: time.Now(),
	}
	actual, _ := el.playerLimiters.LoadOrStore(playerID, entry)
	return actual.(*playerLimiterEntry).limiter
}

// writerLoop batches and writes events to disk asynchronously
func (el *EventLog) writerLoop() {
	defer el.writerWg.Done()

	ticker := time.NewTicker(BatchFlushInterval)
	defer ticker.Stop()

	batch := make([]Event, 0, BatchFlushSize)

	for {
		select {
		case <-el.stopChan:
			// Final flush
			batch = el.collectBatch(batch[:0])
			if len(batch) > 0 {
				el.flushBatch(batch)
			}
			return

		case <-ticker.C:
			// Periodic flush
			batch = el.collectBatch(batch[:0])
			if len(batch) > 0 {
				el.flushBatch(batch)
			}
		}
	}
}

// cleanupLoop removes stale player limiters to prevent memory leak
func (el *EventLog) cleanupLoop() {
	defer el.writerWg.Done()

	ticker := time.NewTicker(PlayerLimiterCleanup)
	defer ticker.Stop()

	for {
		select {
		case <-el.stopChan:
			return
		case <-ticker.C:
			el.cleanupPlayerLimiters()
		}
	}
}

// cleanupPlayerLimiters removes inactive player limiters
func (el *EventLog) cleanupPlayerLimiters() {
	cutoff := time.Now().Add(-PlayerLimiterCleanup)
	el.playerLimiters.Range(func(key, value interface{}) bool {
		entry := value.(*playerLimiterEntry)
		if entry.lastUsed.Before(cutoff) {
			el.playerLimiters.Delete(key)
		}
		return true
	})
}

// collectBatch reads available events from circular buffer
func (el *EventLog) collectBatch(batch []Event) []Event {
	head := atomic.LoadUint64(&el.writeHead)
	tail := atomic.LoadUint64(&el.readHead)

	for i := tail; i < head && len(batch) < BatchFlushSize; i++ {
		idx := i % EventBufferSize
		batch = append(batch, el.buffer[idx])
	}

	// Advance read head
	if len(batch) > 0 {
		atomic.AddUint64(&el.readHead, uint64(len(batch)))
	}

	return batch
}

// flushBatch writes events to disk (append-only, newline-delimited JSON)
func (el *EventLog) flushBatch(batch []Event) {
	el.fileMu.Lock()
	defer el.fileMu.Unlock()

	if el.file == nil {
		return
	}

	for _, event := range batch {
		data, err := json.Marshal(event)
		if err != nil {
			continue
		}
		el.file.Write(data)
		el.file.Write([]byte("\n"))
	}
}

// GetStats returns metrics for DoS monitoring
func (el *EventLog) GetStats() map[string]interface{} {
	head := atomic.LoadUint64(&el.writeHead)
	tail := atomic.LoadUint64(&el.readHead)

	return map[string]interface{}{
		"total":   atomic.LoadUint64(&el.totalCount),
		"dropped": atomic.LoadUint64(&el.droppedCount),
		"pending": head - tail,
		"running": el.running.Load(),
	}
}

// GetDroppedCount returns the number of dropped events
func (el *EventLog) GetDroppedCount() uint64 {
	return atomic.LoadUint64(&el.droppedCount)
}

// GetTotalCount returns the total number of events processed
func (el *EventLog) GetTotalCount() uint64 {
	return atomic.LoadUint64(&el.totalCount)
}
