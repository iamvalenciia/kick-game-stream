package game

import (
	"fmt"
	"math/rand"
	"testing"

	"fight-club/internal/config"
	"fight-club/internal/game/spatial"
)

// =============================================================================
// BENCHMARK SUITE: CRITICAL PATH PERFORMANCE TESTS
// Run with: go test -bench=. -benchmem ./internal/game/...
// =============================================================================

// -----------------------------------------------------------------------------
// ENGINE TICK BENCHMARKS
// -----------------------------------------------------------------------------

func BenchmarkEngineTick_10Players(b *testing.B)  { benchmarkEngineTick(b, 10) }
func BenchmarkEngineTick_50Players(b *testing.B)  { benchmarkEngineTick(b, 50) }
func BenchmarkEngineTick_100Players(b *testing.B) { benchmarkEngineTick(b, 100) }
func BenchmarkEngineTick_200Players(b *testing.B) { benchmarkEngineTick(b, 200) }

func benchmarkEngineTick(b *testing.B, playerCount int) {
	engine := NewEngine(EngineConfig{
		TickRate:    30,
		WorldWidth:  1280,
		WorldHeight: 720,
		Limits:      config.DefaultLimits(),
	})

	// Add players
	for i := 0; i < playerCount; i++ {
		engine.AddPlayer(fmt.Sprintf("Player%d", i), PlayerOptions{})
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		engine.tick()
	}
}

// -----------------------------------------------------------------------------
// SNAPSHOT GENERATION BENCHMARKS
// -----------------------------------------------------------------------------

func BenchmarkProduceSnapshot_10Players(b *testing.B)  { benchmarkSnapshot(b, 10) }
func BenchmarkProduceSnapshot_50Players(b *testing.B)  { benchmarkSnapshot(b, 50) }
func BenchmarkProduceSnapshot_100Players(b *testing.B) { benchmarkSnapshot(b, 100) }
func BenchmarkProduceSnapshot_200Players(b *testing.B) { benchmarkSnapshot(b, 200) }

func benchmarkSnapshot(b *testing.B, playerCount int) {
	engine := NewEngine(EngineConfig{
		TickRate:    30,
		WorldWidth:  1280,
		WorldHeight: 720,
		Limits:      config.DefaultLimits(),
	})

	for i := 0; i < playerCount; i++ {
		engine.AddPlayer(fmt.Sprintf("Player%d", i), PlayerOptions{})
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		engine.ProduceSnapshot()
	}
}

// -----------------------------------------------------------------------------
// PLAYER UPDATE BENCHMARKS
// -----------------------------------------------------------------------------

func BenchmarkPlayerUpdate_WithTarget(b *testing.B) {
	engine := NewEngine(EngineConfig{
		TickRate:    30,
		WorldWidth:  1280,
		WorldHeight: 720,
		Limits:      config.DefaultLimits(),
	})

	// Create 50 players
	for i := 0; i < 50; i++ {
		engine.AddPlayer(fmt.Sprintf("Player%d", i), PlayerOptions{})
	}

	// Rebuild player slice
	engine.playerSlice = engine.playerSlice[:0]
	for _, p := range engine.players {
		engine.playerSlice = append(engine.playerSlice, p)
	}

	player := engine.playerSlice[0]
	player.Target = engine.playerSlice[1]
	deltaTime := 1.0 / 30.0

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		player.Update(engine.playerSlice, 0, engine.spatialGrid, deltaTime, engine)
	}
}

func BenchmarkPlayerUpdate_WithFocusTarget(b *testing.B) {
	engine := NewEngine(EngineConfig{
		TickRate:    30,
		WorldWidth:  1280,
		WorldHeight: 720,
		Limits:      config.DefaultLimits(),
	})

	// Create 50 players
	for i := 0; i < 50; i++ {
		engine.AddPlayer(fmt.Sprintf("Player%d", i), PlayerOptions{})
	}

	engine.playerSlice = engine.playerSlice[:0]
	for _, p := range engine.players {
		engine.playerSlice = append(engine.playerSlice, p)
	}

	player := engine.playerSlice[0]
	// Set focus target (this triggers O(n) scan in current implementation)
	player.FocusTarget = "Player25"
	player.FocusTTL = 10.0
	deltaTime := 1.0 / 30.0

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		player.Update(engine.playerSlice, 0, engine.spatialGrid, deltaTime, engine)
	}
}

// -----------------------------------------------------------------------------
// SPATIAL GRID BENCHMARKS
// -----------------------------------------------------------------------------

func BenchmarkSpatialGrid_Insert(b *testing.B) {
	grid := spatial.NewSpatialGrid(1280, 720, 100, 200)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		grid.Clear()
		for j := 0; j < 100; j++ {
			x := rand.Float64() * 1280
			y := rand.Float64() * 720
			grid.Insert(uint32(j), x, y)
		}
	}
}

func BenchmarkSpatialGrid_QueryRadius(b *testing.B) {
	grid := spatial.NewSpatialGrid(1280, 720, 100, 200)

	// Insert 100 entities
	for j := 0; j < 100; j++ {
		x := rand.Float64() * 1280
		y := rand.Float64() * 720
		grid.Insert(uint32(j), x, y)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		x := rand.Float64() * 1280
		y := rand.Float64() * 720
		_ = grid.QueryRadius(x, y, 300)
	}
}

// -----------------------------------------------------------------------------
// SWEEP AND PRUNE BENCHMARKS
// -----------------------------------------------------------------------------

func BenchmarkSAP_Update_10Entities(b *testing.B)  { benchmarkSAPUpdate(b, 10) }
func BenchmarkSAP_Update_50Entities(b *testing.B)  { benchmarkSAPUpdate(b, 50) }
func BenchmarkSAP_Update_100Entities(b *testing.B) { benchmarkSAPUpdate(b, 100) }
func BenchmarkSAP_Update_200Entities(b *testing.B) { benchmarkSAPUpdate(b, 200) }

func benchmarkSAPUpdate(b *testing.B, entityCount int) {
	sap := spatial.NewSweepAndPrune(entityCount)

	// Initial positions (using float32 as required by UpdateFromSlice)
	positions := make([][2]float32, entityCount)
	for i := 0; i < entityCount; i++ {
		positions[i][0] = float32(rand.Float64() * 1280)
		positions[i][1] = float32(rand.Float64() * 720)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		// Simulate movement
		for j := 0; j < entityCount; j++ {
			positions[j][0] += float32((rand.Float64() - 0.5) * 10)
			positions[j][1] += float32((rand.Float64() - 0.5) * 10)
		}
		// Update and sweep all entities at once
		_ = sap.UpdateFromSlice(positions, 30)
	}
}

// -----------------------------------------------------------------------------
// FLOW FIELD BENCHMARKS
// -----------------------------------------------------------------------------

func BenchmarkFlowField_Generate(b *testing.B) {
	fm := spatial.NewFlowFieldManager(1280, 720, 50)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		goalKey := fmt.Sprintf("goal_%d", i%10)
		goalX := rand.Float64() * 1280
		goalY := rand.Float64() * 720
		fm.GetOrCreate(goalKey, goalX, goalY)
	}
}

func BenchmarkFlowField_Lookup(b *testing.B) {
	fm := spatial.NewFlowFieldManager(1280, 720, 50)

	// Pre-create a flow field
	field := fm.GetOrCreate("target", 640, 360)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		x := rand.Float64() * 1280
		y := rand.Float64() * 720
		field.Lookup(x, y)
	}
}

// -----------------------------------------------------------------------------
// COLLISION DETECTION BENCHMARKS
// -----------------------------------------------------------------------------

func BenchmarkCollisionResolution_10Players(b *testing.B)  { benchmarkCollision(b, 10) }
func BenchmarkCollisionResolution_50Players(b *testing.B)  { benchmarkCollision(b, 50) }
func BenchmarkCollisionResolution_100Players(b *testing.B) { benchmarkCollision(b, 100) }

func benchmarkCollision(b *testing.B, playerCount int) {
	engine := NewEngine(EngineConfig{
		TickRate:    30,
		WorldWidth:  1280,
		WorldHeight: 720,
		Limits:      config.DefaultLimits(),
	})

	// Add players clustered together to maximize collisions
	for i := 0; i < playerCount; i++ {
		p := engine.AddPlayer(fmt.Sprintf("Player%d", i), PlayerOptions{})
		// Cluster players in center
		p.X = 640 + rand.Float64()*100 - 50
		p.Y = 360 + rand.Float64()*100 - 50
	}

	engine.playerSlice = engine.playerSlice[:0]
	for _, p := range engine.players {
		engine.playerSlice = append(engine.playerSlice, p)
	}

	// Rebuild spatial grid
	engine.spatialGrid.Clear()
	for i, p := range engine.playerSlice {
		engine.spatialGrid.Insert(uint32(i), p.X, p.Y)
	}

	player := engine.playerSlice[0]

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		player.ResolveCollisions(engine.playerSlice, 0, engine.spatialGrid)
	}
}

// -----------------------------------------------------------------------------
// MEMORY ALLOCATION TESTS
// -----------------------------------------------------------------------------

func BenchmarkMemoryAllocation_FullTick(b *testing.B) {
	engine := NewEngine(EngineConfig{
		TickRate:    30,
		WorldWidth:  1280,
		WorldHeight: 720,
		Limits:      config.DefaultLimits(),
	})

	// Add 50 players (typical game)
	for i := 0; i < 50; i++ {
		engine.AddPlayer(fmt.Sprintf("Player%d", i), PlayerOptions{})
	}

	// Warm up
	for i := 0; i < 10; i++ {
		engine.tick()
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		engine.tick()
	}
}

// -----------------------------------------------------------------------------
// STRESS TESTS (Run with -benchtime=10s for sustained load)
// -----------------------------------------------------------------------------

func BenchmarkStress_HighPlayerCount(b *testing.B) {
	engine := NewEngine(EngineConfig{
		TickRate:    30,
		WorldWidth:  1280,
		WorldHeight: 720,
		Limits:      config.DefaultLimits(),
	})

	// 200 players - stress test
	for i := 0; i < 200; i++ {
		engine.AddPlayer(fmt.Sprintf("Player%d", i), PlayerOptions{})
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		engine.tick()
	}
}

func BenchmarkStress_RapidJoinLeave(b *testing.B) {
	engine := NewEngine(EngineConfig{
		TickRate:    30,
		WorldWidth:  1280,
		WorldHeight: 720,
		Limits:      config.DefaultLimits(),
	})

	// Start with 50 players
	for i := 0; i < 50; i++ {
		engine.AddPlayer(fmt.Sprintf("Player%d", i), PlayerOptions{})
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		// Simulate rapid join/leave
		name := fmt.Sprintf("TempPlayer%d", i%100)
		engine.AddPlayer(name, PlayerOptions{})
		engine.tick()
		engine.RemovePlayer(name)
	}
}
