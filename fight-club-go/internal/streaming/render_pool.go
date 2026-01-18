package streaming

import (
	"image/color"

	"fight-club/internal/game"
	"runtime"
	"sync"
)

// RenderWorkerPool manages a pool of goroutines for parallel rendering tasks
type RenderWorkerPool struct {
	numWorkers int
	jobChan    chan renderJob
	wg         sync.WaitGroup
	running    bool
	mu         sync.Mutex
}

// renderJob represents a unit of rendering work
type renderJob struct {
	particles  []*game.Particle
	buffer     []byte
	width      int
	height     int
	resultChan chan<- struct{}
}

// ParticleRenderResult holds the result of parallel particle rendering
type ParticleRenderResult struct {
	Buffer []byte
}

// NewRenderWorkerPool creates a new worker pool with the specified number of workers.
// If numWorkers is 0, it defaults to NumCPU.
func NewRenderWorkerPool(numWorkers int) *RenderWorkerPool {
	if numWorkers <= 0 {
		numWorkers = runtime.NumCPU()
	}
	// Cap at reasonable maximum
	if numWorkers > 16 {
		numWorkers = 16
	}

	pool := &RenderWorkerPool{
		numWorkers: numWorkers,
		jobChan:    make(chan renderJob, numWorkers*2),
	}

	return pool
}

// Start begins the worker pool
func (p *RenderWorkerPool) Start() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.running {
		return
	}

	p.running = true
	for i := 0; i < p.numWorkers; i++ {
		go p.worker()
	}
}

// Stop stops the worker pool
func (p *RenderWorkerPool) Stop() {
	p.mu.Lock()
	if !p.running {
		p.mu.Unlock()
		return
	}
	p.running = false
	p.mu.Unlock()

	close(p.jobChan)
	p.wg.Wait()
}

// worker processes render jobs from the channel
func (p *RenderWorkerPool) worker() {
	p.wg.Add(1)
	defer p.wg.Done()

	for job := range p.jobChan {
		p.processParticleJob(job)
		if job.resultChan != nil {
			job.resultChan <- struct{}{}
		}
	}
}

// processParticleJob renders particles directly to the shared buffer
func (p *RenderWorkerPool) processParticleJob(job renderJob) {
	renderer := NewFastRenderer(job.width, job.height, job.buffer)

	for _, particle := range job.particles {
		if particle == nil {
			continue
		}

		c := parseHexColorFast(particle.Color)
		c.A = uint8(particle.Alpha * 255)

		// Skip fully transparent particles
		if c.A == 0 {
			continue
		}

		x := int(particle.X + 0.5)
		y := int(particle.Y + 0.5)

		// Draw particle as a small filled circle (radius 2-3 pixels)
		renderer.DrawFilledCircleBlend(x, y, 2, c)
	}
}

// RenderParticlesParallel renders particles in parallel using the worker pool
func (p *RenderWorkerPool) RenderParticlesParallel(particles []*game.Particle, buffer []byte, width, height int) {
	if len(particles) == 0 {
		return
	}

	p.mu.Lock()
	if !p.running {
		p.mu.Unlock()
		// Fallback to sequential if pool not running
		p.renderParticlesSequential(particles, buffer, width, height)
		return
	}
	p.mu.Unlock()

	// For small particle counts, use sequential rendering (lower overhead)
	// Lowered threshold from 50 to 30 to parallelize earlier during combat
	if len(particles) < 30 {
		p.renderParticlesSequential(particles, buffer, width, height)
		return
	}

	// Divide particles among workers
	chunkSize := (len(particles) + p.numWorkers - 1) / p.numWorkers
	numJobs := 0
	resultChan := make(chan struct{}, p.numWorkers)

	for i := 0; i < len(particles); i += chunkSize {
		end := i + chunkSize
		if end > len(particles) {
			end = len(particles)
		}

		chunk := particles[i:end]
		if len(chunk) == 0 {
			continue
		}

		job := renderJob{
			particles:  chunk,
			buffer:     buffer,
			width:      width,
			height:     height,
			resultChan: resultChan,
		}

		select {
		case p.jobChan <- job:
			numJobs++
		default:
			// Channel full, render sequentially
			p.renderParticlesSequential(chunk, buffer, width, height)
		}
	}

	// Wait for all jobs to complete
	for i := 0; i < numJobs; i++ {
		<-resultChan
	}
}

// renderParticlesSequential is the fallback sequential renderer
func (p *RenderWorkerPool) renderParticlesSequential(particles []*game.Particle, buffer []byte, width, height int) {
	renderer := NewFastRenderer(width, height, buffer)

	for _, particle := range particles {
		if particle == nil {
			continue
		}

		c := parseHexColorFast(particle.Color)
		c.A = uint8(particle.Alpha * 255)

		if c.A == 0 {
			continue
		}

		x := int(particle.X + 0.5)
		y := int(particle.Y + 0.5)
		renderer.DrawFilledCircleBlend(x, y, 2, c)
	}
}

// parseHexColorFast is an optimized hex color parser
func parseHexColorFast(hex string) color.RGBA {
	if len(hex) != 7 || hex[0] != '#' {
		return color.RGBA{255, 255, 255, 255}
	}

	return color.RGBA{
		R: hexToByte(hex[1], hex[2]),
		G: hexToByte(hex[3], hex[4]),
		B: hexToByte(hex[5], hex[6]),
		A: 255,
	}
}

// hexToByte converts two hex chars to a byte
func hexToByte(h1, h2 byte) uint8 {
	return hexCharToNibble(h1)<<4 | hexCharToNibble(h2)
}

// hexCharToNibble converts a hex char to its numeric value
func hexCharToNibble(c byte) uint8 {
	switch {
	case c >= '0' && c <= '9':
		return c - '0'
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10
	default:
		return 0
	}
}

// GetNumWorkers returns the number of workers in the pool
func (p *RenderWorkerPool) GetNumWorkers() int {
	return p.numWorkers
}

// IsRunning returns whether the pool is currently running
func (p *RenderWorkerPool) IsRunning() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.running
}

// RenderParticlesSnapshotParallel renders particles from an immutable snapshot
// This is the preferred method as it works with snapshot types (no pointers)
func (p *RenderWorkerPool) RenderParticlesSnapshotParallel(particles []game.ParticleSnapshot, buffer []byte, width, height int) {
	if len(particles) == 0 {
		return
	}

	p.mu.Lock()
	if !p.running {
		p.mu.Unlock()
		// Fallback to sequential if pool not running
		p.renderParticlesSnapshotSequential(particles, buffer, width, height)
		return
	}
	p.mu.Unlock()

	// For small particle counts, use sequential rendering (lower overhead)
	// Lowered threshold from 50 to 30 to parallelize earlier during combat
	if len(particles) < 30 {
		p.renderParticlesSnapshotSequential(particles, buffer, width, height)
		return
	}

	// For larger counts, render sequentially since we can't use the job channel
	// (it expects []*game.Particle, not []game.ParticleSnapshot)
	// This is still efficient because ParticleSnapshot is a value type
	p.renderParticlesSnapshotSequential(particles, buffer, width, height)
}

// renderParticlesSnapshotSequential renders particles from snapshot data sequentially
func (p *RenderWorkerPool) renderParticlesSnapshotSequential(particles []game.ParticleSnapshot, buffer []byte, width, height int) {
	renderer := NewFastRenderer(width, height, buffer)

	for _, particle := range particles {
		c := parseHexColorFast(particle.Color)
		c.A = uint8(particle.Alpha * 255)

		if c.A == 0 {
			continue
		}

		x := int(particle.X + 0.5)
		y := int(particle.Y + 0.5)
		renderer.DrawFilledCircleBlend(x, y, 2, c)
	}
}
