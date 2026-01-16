package game

import (
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"fight-club/internal/config"
)

// =============================================================================
// INTEGRATION TESTS: SIMULATE REAL STREAMING CONDITIONS
// These tests simulate the full game loop with concurrent rendering pressure
// =============================================================================

// TestIntegration_GameLoopWithRenderPressure simulates real streaming conditions
// where the game loop runs while a renderer continuously consumes snapshots
func TestIntegration_GameLoopWithRenderPressure(t *testing.T) {
	cfg := EngineConfig{
		TickRate:    24, // Match production FPS
		WorldWidth:  1280,
		WorldHeight: 720,
		Limits:      config.DefaultLimits(),
	}
	engine := NewEngine(cfg)

	// Add typical player count
	for i := 0; i < 30; i++ {
		engine.AddPlayer("Player"+string(rune('A'+i%26))+string(rune('0'+i/26)), PlayerOptions{})
	}

	// Metrics
	var (
		tickCount      int64
		snapshotCount  int64
		maxTickTime    int64
		totalTickTime  int64
		droppedFrames  int64
	)

	// Target frame time for 24 FPS
	targetFrameTime := time.Second / 24
	testDuration := 5 * time.Second

	var wg sync.WaitGroup
	stopChan := make(chan struct{})

	// Simulate render loop (consumer) - runs at 24 FPS
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(targetFrameTime)
		defer ticker.Stop()

		lastSnapshot := time.Now()
		for {
			select {
			case <-stopChan:
				return
			case <-ticker.C:
				start := time.Now()
				engine.ProduceSnapshot()
				elapsed := time.Since(start)

				atomic.AddInt64(&snapshotCount, 1)

				// Check if we're keeping up with frame rate
				timeSinceLastSnapshot := time.Since(lastSnapshot)
				if timeSinceLastSnapshot > targetFrameTime*2 {
					atomic.AddInt64(&droppedFrames, 1)
				}
				lastSnapshot = time.Now()

				// Track snapshot generation time
				_ = elapsed
			}
		}
	}()

	// Simulate game loop (producer) - runs at tick rate
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(time.Second / time.Duration(cfg.TickRate))
		defer ticker.Stop()

		for {
			select {
			case <-stopChan:
				return
			case <-ticker.C:
				start := time.Now()
				engine.tick()
				elapsed := time.Since(start).Nanoseconds()

				atomic.AddInt64(&tickCount, 1)
				atomic.AddInt64(&totalTickTime, elapsed)

				// Track max tick time
				for {
					current := atomic.LoadInt64(&maxTickTime)
					if elapsed <= current || atomic.CompareAndSwapInt64(&maxTickTime, current, elapsed) {
						break
					}
				}
			}
		}
	}()

	// Simulate player activity (commands coming in)
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(50 * time.Millisecond) // 20 commands/sec
		defer ticker.Stop()

		cmdIndex := 0
		for {
			select {
			case <-stopChan:
				return
			case <-ticker.C:
				cmdIndex++

				// Simulate various commands by modifying player state
				// Note: Players have AI behavior that handles movement,
				// we just trigger attacks and healing periodically
				switch cmdIndex % 4 {
				case 0, 1, 2:
					// Players move via AI, just get state
					engine.GetState()
				case 3:
					// Heal a random player periodically
					engine.HealPlayer("PlayerA0", 10)
				}
			}
		}
	}()

	// Simulate memory pressure from GC
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-stopChan:
				return
			case <-ticker.C:
				runtime.GC() // Force GC to simulate real-world pressure
			}
		}
	}()

	// Run test
	time.Sleep(testDuration)
	close(stopChan)
	wg.Wait()

	// Calculate results
	ticks := atomic.LoadInt64(&tickCount)
	snapshots := atomic.LoadInt64(&snapshotCount)
	maxTick := atomic.LoadInt64(&maxTickTime)
	totalTick := atomic.LoadInt64(&totalTickTime)
	dropped := atomic.LoadInt64(&droppedFrames)

	avgTickNs := float64(0)
	if ticks > 0 {
		avgTickNs = float64(totalTick) / float64(ticks)
	}

	actualTPS := float64(ticks) / testDuration.Seconds()
	actualFPS := float64(snapshots) / testDuration.Seconds()

	t.Logf("Integration Test Results (Simulated Streaming):")
	t.Logf("  Test Duration: %v", testDuration)
	t.Logf("  Total Ticks: %d (%.1f TPS)", ticks, actualTPS)
	t.Logf("  Total Snapshots: %d (%.1f FPS)", snapshots, actualFPS)
	t.Logf("  Avg Tick Time: %.2f µs", avgTickNs/1000)
	t.Logf("  Max Tick Time: %.2f µs", float64(maxTick)/1000)
	t.Logf("  Dropped Frames: %d", dropped)

	// Assertions
	targetTPS := float64(cfg.TickRate)
	if actualTPS < targetTPS*0.9 {
		t.Errorf("TPS too low: %.1f < %.1f (90%% of target)", actualTPS, targetTPS*0.9)
	}

	targetFPS := float64(24)
	if actualFPS < targetFPS*0.9 {
		t.Errorf("FPS too low: %.1f < %.1f (90%% of target)", actualFPS, targetFPS*0.9)
	}

	// Max tick should stay under 2 frames (83ms at 24 FPS)
	maxTickMs := float64(maxTick) / 1e6
	if maxTickMs > 83 {
		t.Errorf("Max tick time too high: %.2f ms > 83 ms (2 frames)", maxTickMs)
	}

	// Dropped frames should be minimal
	droppedPercent := float64(dropped) / float64(snapshots) * 100
	if droppedPercent > 5 {
		t.Errorf("Too many dropped frames: %.1f%% > 5%%", droppedPercent)
	}
}

// TestIntegration_HighConcurrencyStress tests behavior under high concurrent load
func TestIntegration_HighConcurrencyStress(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping stress test in short mode")
	}

	cfg := EngineConfig{
		TickRate:    24,
		WorldWidth:  1280,
		WorldHeight: 720,
		Limits:      config.DefaultLimits(),
	}
	engine := NewEngine(cfg)

	// Start with many players
	for i := 0; i < 100; i++ {
		engine.AddPlayer("Stress"+string(rune('A'+i%26))+string(rune('0'+i/26)), PlayerOptions{})
	}

	var (
		tickErrors   int64
		totalTicks   int64
		concurrentOps int64
	)

	testDuration := 3 * time.Second
	stopChan := make(chan struct{})
	var wg sync.WaitGroup

	// Many concurrent goroutines doing operations
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for {
				select {
				case <-stopChan:
					return
				default:
					atomic.AddInt64(&concurrentOps, 1)

					// Mix of operations
					switch id % 5 {
					case 0:
						engine.GetState()
					case 1:
						name := "TempPlayer" + string(rune('A'+id))
						engine.AddPlayer(name, PlayerOptions{})
						engine.RemovePlayer(name)
					case 2:
						engine.ProduceSnapshot()
					case 3:
						// Get player state (read-only operation)
						_ = engine.GetPlayer("StressA0")
					case 4:
						engine.HealPlayer("StressB1", 10)
					}

					time.Sleep(time.Millisecond)
				}
			}
		}(i)
	}

	// Game loop
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(time.Second / 24)
		defer ticker.Stop()

		for {
			select {
			case <-stopChan:
				return
			case <-ticker.C:
				func() {
					defer func() {
						if r := recover(); r != nil {
							atomic.AddInt64(&tickErrors, 1)
						}
					}()
					engine.tick()
					atomic.AddInt64(&totalTicks, 1)
				}()
			}
		}
	}()

	time.Sleep(testDuration)
	close(stopChan)
	wg.Wait()

	ticks := atomic.LoadInt64(&totalTicks)
	errors := atomic.LoadInt64(&tickErrors)
	ops := atomic.LoadInt64(&concurrentOps)

	t.Logf("High Concurrency Results:")
	t.Logf("  Total Ticks: %d", ticks)
	t.Logf("  Tick Errors: %d", errors)
	t.Logf("  Concurrent Ops: %d", ops)

	if errors > 0 {
		t.Errorf("Had %d tick errors (panics) during concurrent access", errors)
	}

	expectedTicks := int64(testDuration.Seconds() * 24 * 0.9)
	if ticks < expectedTicks {
		t.Errorf("Too few ticks: %d < %d expected", ticks, expectedTicks)
	}
}

// TestIntegration_MemoryStability tests for memory leaks during extended operation
func TestIntegration_MemoryStability(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping memory stability test in short mode")
	}

	cfg := EngineConfig{
		TickRate:    24,
		WorldWidth:  1280,
		WorldHeight: 720,
		Limits:      config.DefaultLimits(),
	}
	engine := NewEngine(cfg)

	// Force GC and get baseline
	runtime.GC()
	var baselineStats runtime.MemStats
	runtime.ReadMemStats(&baselineStats)

	// Run for a period with churn
	iterations := 1000
	for i := 0; i < iterations; i++ {
		// Add players
		for j := 0; j < 10; j++ {
			engine.AddPlayer("MemTest"+string(rune('A'+j)), PlayerOptions{})
		}

		// Run ticks
		for k := 0; k < 10; k++ {
			engine.tick()
			engine.ProduceSnapshot()
		}

		// Remove players
		for j := 0; j < 10; j++ {
			engine.RemovePlayer("MemTest" + string(rune('A'+j)))
		}

		// Periodic GC
		if i%100 == 0 {
			runtime.GC()
		}
	}

	// Final GC and measure
	runtime.GC()
	var finalStats runtime.MemStats
	runtime.ReadMemStats(&finalStats)

	heapGrowthMB := float64(finalStats.HeapAlloc-baselineStats.HeapAlloc) / (1024 * 1024)

	t.Logf("Memory Stability Results:")
	t.Logf("  Iterations: %d", iterations)
	t.Logf("  Baseline Heap: %.2f MB", float64(baselineStats.HeapAlloc)/(1024*1024))
	t.Logf("  Final Heap: %.2f MB", float64(finalStats.HeapAlloc)/(1024*1024))
	t.Logf("  Heap Growth: %.2f MB", heapGrowthMB)
	t.Logf("  Total Allocations: %d", finalStats.Mallocs-baselineStats.Mallocs)

	// Allow some growth but flag significant leaks
	if heapGrowthMB > 50 {
		t.Errorf("Significant memory growth: %.2f MB", heapGrowthMB)
	}
}
