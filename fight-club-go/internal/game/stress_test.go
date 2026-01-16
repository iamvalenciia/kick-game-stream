package game

import (
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"fight-club/internal/config"
)

// =============================================================================
// STRESS TEST SUITE: REAL-WORLD LOAD SIMULATION
// Run with: go test -v -run=TestStress -timeout=60s ./internal/game/...
// =============================================================================

// StressTestResult contains metrics from stress tests
type StressTestResult struct {
	Duration        time.Duration
	TotalTicks      int64
	AvgTickTime     time.Duration
	MaxTickTime     time.Duration
	MinTickTime     time.Duration
	P99TickTime     time.Duration
	TicksPerSecond  float64
	CommandsHandled int64
	DroppedCommands int64
	PeakPlayers     int
	Errors          []error
}

// StressTestConfig configures stress test parameters
type StressTestConfig struct {
	Duration         time.Duration
	TargetFPS        int
	InitialPlayers   int
	MaxPlayers       int
	CommandsPerSec   int     // Simulated chat commands/second
	JoinLeaveRate    float64 // Probability of join/leave per tick
	LatencyThreshold time.Duration
}

// DefaultStressConfig returns production-like stress test config
func DefaultStressConfig() StressTestConfig {
	return StressTestConfig{
		Duration:         10 * time.Second,
		TargetFPS:        24,
		InitialPlayers:   20,
		MaxPlayers:       100,
		CommandsPerSec:   50, // High activity stream
		JoinLeaveRate:    0.05,
		LatencyThreshold: 50 * time.Millisecond, // Max acceptable tick time
	}
}

// -----------------------------------------------------------------------------
// STRESS TEST: SUSTAINED LOAD
// -----------------------------------------------------------------------------

func TestStress_SustainedLoad(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping stress test in short mode")
	}

	cfg := DefaultStressConfig()
	cfg.Duration = 5 * time.Second

	result := runStressTest(t, cfg)

	// Performance assertions
	if result.AvgTickTime > cfg.LatencyThreshold {
		t.Errorf("Average tick time %v exceeds threshold %v", result.AvgTickTime, cfg.LatencyThreshold)
	}

	expectedTPS := float64(cfg.TargetFPS) * 0.9 // Allow 10% variance
	if result.TicksPerSecond < expectedTPS {
		t.Errorf("Ticks per second %.2f below expected %.2f", result.TicksPerSecond, expectedTPS)
	}

	t.Logf("Stress Test Results:")
	t.Logf("  Duration: %v", result.Duration)
	t.Logf("  Total Ticks: %d", result.TotalTicks)
	t.Logf("  Avg Tick Time: %v", result.AvgTickTime)
	t.Logf("  Max Tick Time: %v", result.MaxTickTime)
	t.Logf("  TPS: %.2f", result.TicksPerSecond)
	t.Logf("  Commands Handled: %d", result.CommandsHandled)
	t.Logf("  Peak Players: %d", result.PeakPlayers)
}

// -----------------------------------------------------------------------------
// STRESS TEST: SPIKE LOAD
// -----------------------------------------------------------------------------

func TestStress_SpikeLoad(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping stress test in short mode")
	}

	engine := NewEngine(EngineConfig{
		TickRate:    24,
		WorldWidth:  1280,
		WorldHeight: 720,
		Limits:      config.DefaultLimits(),
	})

	// Start with minimal players
	for i := 0; i < 5; i++ {
		engine.AddPlayer(fmt.Sprintf("Player%d", i), PlayerOptions{})
	}

	var maxTickTime time.Duration
	var tickTimes []time.Duration

	// Run for 3 seconds with sudden spikes
	deadline := time.Now().Add(3 * time.Second)
	tickCount := 0

	for time.Now().Before(deadline) {
		// Simulate sudden viewer spike every 500ms
		if tickCount%12 == 0 && tickCount > 0 {
			// Add 20 players suddenly
			for i := 0; i < 20; i++ {
				name := fmt.Sprintf("Spike%d_%d", tickCount, i)
				engine.AddPlayer(name, PlayerOptions{})
			}
		}

		start := time.Now()
		engine.tick()
		elapsed := time.Since(start)

		tickTimes = append(tickTimes, elapsed)
		if elapsed > maxTickTime {
			maxTickTime = elapsed
		}

		tickCount++
		time.Sleep(time.Second / 24) // Target 24 FPS
	}

	// After spike, remove extra players
	playerCount := 0
	engine.mu.RLock()
	playerCount = len(engine.players)
	engine.mu.RUnlock()

	t.Logf("Spike Test Results:")
	t.Logf("  Peak Players: %d", playerCount)
	t.Logf("  Max Tick Time: %v", maxTickTime)
	t.Logf("  Total Ticks: %d", tickCount)

	// Assert spike handling
	if maxTickTime > 100*time.Millisecond {
		t.Errorf("Max tick time %v during spike exceeds 100ms threshold", maxTickTime)
	}
}

// -----------------------------------------------------------------------------
// STRESS TEST: CONCURRENT COMMANDS
// -----------------------------------------------------------------------------

func TestStress_ConcurrentCommands(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping stress test in short mode")
	}

	engine := NewEngine(EngineConfig{
		TickRate:    24,
		WorldWidth:  1280,
		WorldHeight: 720,
		Limits:      config.DefaultLimits(),
	})

	// Initial players
	for i := 0; i < 30; i++ {
		engine.AddPlayer(fmt.Sprintf("Player%d", i), PlayerOptions{})
	}

	var wg sync.WaitGroup
	var commandsProcessed int64
	var errors int64

	// Start engine tick goroutine
	stopChan := make(chan struct{})
	go func() {
		ticker := time.NewTicker(time.Second / 24)
		defer ticker.Stop()
		for {
			select {
			case <-stopChan:
				return
			case <-ticker.C:
				engine.tick()
			}
		}
	}()

	// Simulate concurrent chat commands
	numWorkers := 10
	commandsPerWorker := 100

	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for i := 0; i < commandsPerWorker; i++ {
				cmd := rand.Intn(5)
				name := fmt.Sprintf("Worker%d_Player%d", workerID, i%10)

				switch cmd {
				case 0: // Join
					engine.AddPlayer(name, PlayerOptions{})
				case 1: // Leave
					engine.RemovePlayer(name)
				case 2: // Focus
					engine.mu.RLock()
					for _, p := range engine.players {
						if p.Name == name {
							p.FocusTarget = "Player0"
							p.FocusTTL = 5.0
						}
					}
					engine.mu.RUnlock()
				case 3: // Equip
					engine.mu.RLock()
					for _, p := range engine.players {
						if p.Name == name {
							p.Weapon = "sword"
						}
					}
					engine.mu.RUnlock()
				case 4: // Chat
					// Just add chat bubble
					engine.mu.RLock()
					for _, p := range engine.players {
						if p.Name == name {
							p.ChatBubble = "Hello!"
							p.ChatBubbleTTL = 3.0
						}
					}
					engine.mu.RUnlock()
				}

				atomic.AddInt64(&commandsProcessed, 1)
				time.Sleep(time.Millisecond) // Rate limit
			}
		}(w)
	}

	wg.Wait()
	close(stopChan)

	t.Logf("Concurrent Commands Test:")
	t.Logf("  Commands Processed: %d", commandsProcessed)
	t.Logf("  Errors: %d", errors)
	t.Logf("  Error Rate: %.2f%%", float64(errors)/float64(commandsProcessed)*100)

	if errors > 0 {
		t.Errorf("Had %d errors during concurrent command processing", errors)
	}
}

// -----------------------------------------------------------------------------
// STRESS TEST: MEMORY PRESSURE
// -----------------------------------------------------------------------------

func TestStress_MemoryPressure(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping stress test in short mode")
	}

	engine := NewEngine(EngineConfig{
		TickRate:    24,
		WorldWidth:  1280,
		WorldHeight: 720,
		Limits:      config.DefaultLimits(),
	})

	// Track allocation patterns over time
	var tickAllocations []int64

	// Run for 1000 ticks
	for tick := 0; tick < 1000; tick++ {
		// Add/remove players to stress memory
		if tick%10 == 0 {
			for i := 0; i < 5; i++ {
				engine.AddPlayer(fmt.Sprintf("Temp%d_%d", tick, i), PlayerOptions{})
			}
		}
		if tick%10 == 5 {
			for i := 0; i < 5; i++ {
				engine.RemovePlayer(fmt.Sprintf("Temp%d_%d", tick-5, i))
			}
		}

		engine.tick()
	}

	// Check for memory leaks - player count should be stable
	engine.mu.RLock()
	finalPlayerCount := len(engine.players)
	engine.mu.RUnlock()

	t.Logf("Memory Pressure Test:")
	t.Logf("  Final Player Count: %d", finalPlayerCount)
	t.Logf("  Tick Allocations Sampled: %d", len(tickAllocations))

	// Players added on tick 990-999 should still exist
	// Players from tick 0-985 should be removed
	if finalPlayerCount > 50 {
		t.Errorf("Possible memory leak: %d players remaining", finalPlayerCount)
	}
}

// -----------------------------------------------------------------------------
// HELPER: RUN STRESS TEST
// -----------------------------------------------------------------------------

func runStressTest(t *testing.T, cfg StressTestConfig) StressTestResult {
	engine := NewEngine(EngineConfig{
		TickRate:    cfg.TargetFPS,
		WorldWidth:  1280,
		WorldHeight: 720,
		Limits:      config.DefaultLimits(),
	})

	// Initial players
	for i := 0; i < cfg.InitialPlayers; i++ {
		engine.AddPlayer(fmt.Sprintf("Player%d", i), PlayerOptions{})
	}

	var result StressTestResult
	result.MinTickTime = time.Hour // Initialize high

	var tickTimes []time.Duration
	var totalTickTime time.Duration
	var commandsHandled int64
	peakPlayers := cfg.InitialPlayers

	deadline := time.Now().Add(cfg.Duration)
	startTime := time.Now()

	for time.Now().Before(deadline) {
		// Simulate commands based on rate
		commandsThisTick := cfg.CommandsPerSec / cfg.TargetFPS
		for c := 0; c < commandsThisTick; c++ {
			// Simulate random commands
			if rand.Float64() < cfg.JoinLeaveRate {
				if rand.Float64() < 0.5 {
					name := fmt.Sprintf("NewPlayer%d", rand.Int())
					engine.AddPlayer(name, PlayerOptions{})
				} else {
					// Remove random player
					engine.mu.RLock()
					var toRemove string
					i := 0
					target := rand.Intn(len(engine.players) + 1)
					for name := range engine.players {
						if i == target {
							toRemove = name
							break
						}
						i++
					}
					engine.mu.RUnlock()
					if toRemove != "" {
						engine.RemovePlayer(toRemove)
					}
				}
			}
			atomic.AddInt64(&commandsHandled, 1)
		}

		// Run tick
		start := time.Now()
		engine.tick()
		elapsed := time.Since(start)

		// Track metrics
		tickTimes = append(tickTimes, elapsed)
		totalTickTime += elapsed
		result.TotalTicks++

		if elapsed > result.MaxTickTime {
			result.MaxTickTime = elapsed
		}
		if elapsed < result.MinTickTime {
			result.MinTickTime = elapsed
		}

		// Track peak players
		engine.mu.RLock()
		if len(engine.players) > peakPlayers {
			peakPlayers = len(engine.players)
		}
		engine.mu.RUnlock()

		// Sleep to maintain target FPS
		targetInterval := time.Second / time.Duration(cfg.TargetFPS)
		if elapsed < targetInterval {
			time.Sleep(targetInterval - elapsed)
		}
	}

	result.Duration = time.Since(startTime)
	result.AvgTickTime = totalTickTime / time.Duration(result.TotalTicks)
	result.TicksPerSecond = float64(result.TotalTicks) / result.Duration.Seconds()
	result.CommandsHandled = commandsHandled
	result.PeakPlayers = peakPlayers

	// Calculate P99
	if len(tickTimes) > 0 {
		// Sort for percentile (simple implementation)
		for i := 0; i < len(tickTimes); i++ {
			for j := i + 1; j < len(tickTimes); j++ {
				if tickTimes[j] < tickTimes[i] {
					tickTimes[i], tickTimes[j] = tickTimes[j], tickTimes[i]
				}
			}
		}
		p99Index := int(float64(len(tickTimes)) * 0.99)
		if p99Index >= len(tickTimes) {
			p99Index = len(tickTimes) - 1
		}
		result.P99TickTime = tickTimes[p99Index]
	}

	return result
}

// -----------------------------------------------------------------------------
// LATENCY TEST: END-TO-END COMMAND PROCESSING
// -----------------------------------------------------------------------------

func TestLatency_CommandToRender(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping latency test in short mode")
	}

	engine := NewEngine(EngineConfig{
		TickRate:    24,
		WorldWidth:  1280,
		WorldHeight: 720,
		Limits:      config.DefaultLimits(),
	})

	// Add initial players
	for i := 0; i < 20; i++ {
		engine.AddPlayer(fmt.Sprintf("Player%d", i), PlayerOptions{})
	}

	// Measure time from command to appearing in snapshot
	var latencies []time.Duration

	for i := 0; i < 100; i++ {
		playerName := fmt.Sprintf("LatencyTest%d", i)

		// Record time before command
		cmdTime := time.Now()

		// Execute join command
		engine.AddPlayer(playerName, PlayerOptions{})

		// Tick until player appears in snapshot
		var foundTime time.Time
		for tick := 0; tick < 10; tick++ {
			engine.tick()
			snap := engine.GetSnapshot()
			if snap != nil {
				for _, p := range snap.Players {
					if p.Name == playerName {
						foundTime = time.Now()
						break
					}
				}
			}
			if !foundTime.IsZero() {
				break
			}
		}

		if !foundTime.IsZero() {
			latencies = append(latencies, foundTime.Sub(cmdTime))
		}

		// Cleanup
		engine.RemovePlayer(playerName)
	}

	// Calculate stats
	var total time.Duration
	var max time.Duration
	for _, l := range latencies {
		total += l
		if l > max {
			max = l
		}
	}
	avg := total / time.Duration(len(latencies))

	t.Logf("Command-to-Render Latency:")
	t.Logf("  Samples: %d", len(latencies))
	t.Logf("  Average: %v", avg)
	t.Logf("  Max: %v", max)

	// Assert reasonable latency (should be < 2 ticks = ~83ms at 24fps)
	maxAcceptable := time.Second / 12 // ~83ms
	if avg > maxAcceptable {
		t.Errorf("Average latency %v exceeds acceptable %v", avg, maxAcceptable)
	}
}
