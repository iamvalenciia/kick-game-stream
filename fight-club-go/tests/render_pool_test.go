package tests

import (
	"testing"

	"fight-club/internal/game"
	"fight-club/internal/streaming"
)

// TestNewRenderWorkerPool tests worker pool creation
func TestNewRenderWorkerPool(t *testing.T) {
	pool := streaming.NewRenderWorkerPool(4)

	if pool == nil {
		t.Fatal("RenderWorkerPool should not be nil")
	}

	if pool.GetNumWorkers() != 4 {
		t.Errorf("Expected 4 workers, got %d", pool.GetNumWorkers())
	}
}

// TestRenderWorkerPoolDefaultWorkers tests default worker count
func TestRenderWorkerPoolDefaultWorkers(t *testing.T) {
	pool := streaming.NewRenderWorkerPool(0)

	if pool.GetNumWorkers() <= 0 {
		t.Error("Worker pool should have at least 1 worker")
	}
}

// TestRenderWorkerPoolMaxWorkers tests max worker cap
func TestRenderWorkerPoolMaxWorkers(t *testing.T) {
	pool := streaming.NewRenderWorkerPool(100)

	if pool.GetNumWorkers() > 16 {
		t.Errorf("Worker pool should cap at 16 workers, got %d", pool.GetNumWorkers())
	}
}

// TestRenderWorkerPoolStartStop tests starting and stopping the pool
func TestRenderWorkerPoolStartStop(t *testing.T) {
	pool := streaming.NewRenderWorkerPool(2)

	// Should not be running initially
	if pool.IsRunning() {
		t.Error("Pool should not be running initially")
	}

	pool.Start()
	if !pool.IsRunning() {
		t.Error("Pool should be running after Start()")
	}

	pool.Stop()
	if pool.IsRunning() {
		t.Error("Pool should not be running after Stop()")
	}
}

// TestRenderWorkerPoolParallelParticles tests parallel particle rendering
func TestRenderWorkerPoolParallelParticles(t *testing.T) {
	pool := streaming.NewRenderWorkerPool(4)
	pool.Start()
	defer pool.Stop()

	width, height := 100, 100
	buffer := make([]byte, width*height*4)

	// Create test particles
	particles := make([]*game.Particle, 100)
	for i := 0; i < 100; i++ {
		particles[i] = &game.Particle{
			X:     float64(i % width),
			Y:     float64(i / width),
			Color: "#ff0000",
			Alpha: 1.0,
		}
	}

	// Render particles
	pool.RenderParticlesParallel(particles, buffer, width, height)

	// Verify some pixels were set (not all zeros)
	hasNonZero := false
	for i := 0; i < len(buffer); i += 4 {
		if buffer[i] != 0 || buffer[i+1] != 0 || buffer[i+2] != 0 {
			hasNonZero = true
			break
		}
	}

	if !hasNonZero {
		t.Error("Buffer should have non-zero pixels after rendering particles")
	}
}

// TestRenderWorkerPoolEmptyParticles tests with empty particle list
func TestRenderWorkerPoolEmptyParticles(t *testing.T) {
	pool := streaming.NewRenderWorkerPool(2)
	pool.Start()
	defer pool.Stop()

	width, height := 50, 50
	buffer := make([]byte, width*height*4)

	// Should not panic with empty particles
	pool.RenderParticlesParallel([]*game.Particle{}, buffer, width, height)
}

// TestRenderWorkerPoolSmallParticleCount tests sequential fallback
func TestRenderWorkerPoolSmallParticleCount(t *testing.T) {
	pool := streaming.NewRenderWorkerPool(4)
	pool.Start()
	defer pool.Stop()

	width, height := 50, 50
	buffer := make([]byte, width*height*4)

	// Create few particles (should use sequential rendering)
	particles := make([]*game.Particle, 10)
	for i := 0; i < 10; i++ {
		particles[i] = &game.Particle{
			X:     25,
			Y:     25,
			Color: "#00ff00",
			Alpha: 1.0,
		}
	}

	pool.RenderParticlesParallel(particles, buffer, width, height)

	// Check center area for green pixels
	centerIdx := (25*width + 25) * 4
	if buffer[centerIdx] == 0 && buffer[centerIdx+1] == 0 && buffer[centerIdx+2] == 0 {
		t.Error("Center area should have rendered particles")
	}
}

// BenchmarkRenderWorkerPoolParallel benchmarks parallel rendering
func BenchmarkRenderWorkerPoolParallel(b *testing.B) {
	pool := streaming.NewRenderWorkerPool(0) // Use NumCPU
	pool.Start()
	defer pool.Stop()

	width, height := 1280, 720
	buffer := make([]byte, width*height*4)

	// Create many particles
	particles := make([]*game.Particle, 500)
	for i := 0; i < 500; i++ {
		particles[i] = &game.Particle{
			X:     float64(i % width),
			Y:     float64((i * 3) % height),
			Color: "#ff5500",
			Alpha: 0.8,
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pool.RenderParticlesParallel(particles, buffer, width, height)
	}
}
