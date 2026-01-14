package game

import (
	"sync/atomic"
	"time"
)

// ResourceLimits defines hard caps to prevent DoS attacks
type ResourceLimits struct {
	MaxTotalPlayers int // Hard cap on total connected players (logic)
	MaxPlayers      int // Hard cap on rendered players (snapshot)
	MaxParticles    int // Per frame particle limit
	MaxEffects      int // Per frame effect limit
	MaxTexts        int // Per frame floating text limit
	MaxTrails       int // Per frame weapon trail limit
	MaxFlashes      int // Per frame impact flash limit
}

// DefaultLimits provides production-safe default limits
var DefaultLimits = ResourceLimits{
	MaxTotalPlayers: 1000000,
	MaxPlayers:      200,
	MaxParticles:    200, // Reduced from 500
	MaxEffects:      20,  // Reduced from 100 - prevents arc accumulation
	MaxTexts:        30,  // Reduced from 50
	MaxTrails:       20,  // Reduced from 50
	MaxFlashes:      10,  // Reduced from 30 - prevents flash accumulation
}

// PlayerSnapshot is an immutable copy of player state for rendering
// Uses value types (not pointers) to ensure immutability
type PlayerSnapshot struct {
	ID              string
	Name            string
	X, Y            float64
	VX, VY          float64
	HP, MaxHP       int
	Money           int
	Kills           int
	Deaths          int
	Weapon          string
	Color           string
	Avatar          string
	AttackAngle     float64
	IsDead          bool
	IsRagdoll       bool
	RagdollRotation float64
	SpawnProtection bool
	IsAttacking     bool
	ProfilePic      string

	// Advanced combat state for rendering
	IsDodging      bool
	DodgeDirection float64
	ComboCount     int
	Stamina        float64
}

// ParticleSnapshot is an immutable particle for rendering
type ParticleSnapshot struct {
	X, Y  float64
	Color string
	Alpha float64
}

// EffectSnapshot is an immutable attack effect
type EffectSnapshot struct {
	X, Y   float64
	TX, TY float64
	Color  string
	Timer  int
}

// TextSnapshot is an immutable floating text
type TextSnapshot struct {
	X, Y  float64
	Text  string
	Color string
	Alpha float64
}

// TrailSnapshot is an immutable weapon trail for rendering
type TrailSnapshot struct {
	Points   [8]TrailPointSnapshot // Fixed-size for zero-allocation
	Count    int                   // Number of valid points
	Color    string
	Alpha    float64
	PlayerID string
}

// TrailPointSnapshot is a single point in a trail
type TrailPointSnapshot struct {
	X, Y  float64
	Alpha float64
}

// FlashSnapshot is an immutable impact flash
type FlashSnapshot struct {
	X, Y      float64
	Radius    float64
	Color     string
	Intensity float64
}

// ShakeSnapshot captures screen shake state
type ShakeSnapshot struct {
	OffsetX   float64
	OffsetY   float64
	Intensity float64
}

// GameSnapshot is a complete immutable game state for rendering
// All slices are pre-allocated and capped to prevent memory attacks
type GameSnapshot struct {
	Sequence   uint64    // Monotonic sequence for ordering
	Timestamp  time.Time // When snapshot was created
	TickNumber uint64    // Game tick this represents
	RNGSeed    int64     // Seed for deterministic replay

	// Pre-allocated capped slices (never grows beyond limits)
	Players   []PlayerSnapshot
	Particles []ParticleSnapshot
	Effects   []EffectSnapshot
	Texts     []TextSnapshot

	// New combat visual effects
	Trails      []TrailSnapshot
	Flashes     []FlashSnapshot
	Projectiles []ProjectileSnapshot // Bow arrows and thrown weapons
	Shake       ShakeSnapshot        // Single global shake state

	// Aggregate stats
	PlayerCount int
	AliveCount  int
	TotalKills  int
}

// SnapshotPool pre-allocates snapshots to avoid GC pressure
// Uses triple buffering for lock-free producer/consumer
type SnapshotPool struct {
	snapshots [3]GameSnapshot // Triple buffer
	limits    ResourceLimits
	writeIdx  uint32 // atomic - producer index
	readIdx   uint32 // atomic - consumer index
	sequence  uint64 // atomic - monotonic sequence
}

// NewSnapshotPool creates a pool with pre-allocated slices
func NewSnapshotPool(limits ResourceLimits) *SnapshotPool {
	pool := &SnapshotPool{limits: limits}

	// Pre-allocate all slices to avoid runtime allocations
	for i := 0; i < 3; i++ {
		pool.snapshots[i] = GameSnapshot{
			Players:     make([]PlayerSnapshot, 0, limits.MaxPlayers),
			Particles:   make([]ParticleSnapshot, 0, limits.MaxParticles),
			Effects:     make([]EffectSnapshot, 0, limits.MaxEffects),
			Texts:       make([]TextSnapshot, 0, limits.MaxTexts),
			Trails:      make([]TrailSnapshot, 0, limits.MaxTrails),
			Flashes:     make([]FlashSnapshot, 0, limits.MaxFlashes),
			Projectiles: make([]ProjectileSnapshot, 0, MaxProjectiles),
		}
	}

	return pool
}

// AcquireWrite gets the next write slot (producer only, called from game tick)
// Returns a snapshot with reset slices but preserved capacity
func (p *SnapshotPool) AcquireWrite() *GameSnapshot {
	idx := atomic.AddUint32(&p.writeIdx, 1) % 3
	snap := &p.snapshots[idx]

	// Reset ALL slices but keep capacity (zero allocation)
	snap.Players = snap.Players[:0]
	snap.Particles = snap.Particles[:0]
	snap.Effects = snap.Effects[:0]
	snap.Texts = snap.Texts[:0]
	snap.Trails = snap.Trails[:0]           // BUGFIX: Was missing, caused stale trails
	snap.Flashes = snap.Flashes[:0]         // BUGFIX: Was missing, caused stale flashes
	snap.Projectiles = snap.Projectiles[:0] // Reset projectiles

	// Reset shake state
	snap.Shake = ShakeSnapshot{} // Zero out shake

	// Assign new sequence number
	snap.Sequence = atomic.AddUint64(&p.sequence, 1)
	snap.Timestamp = time.Now()

	return snap
}

// PublishWrite marks write complete and advances read pointer
// Called after snapshot is fully populated
func (p *SnapshotPool) PublishWrite() {
	atomic.StoreUint32(&p.readIdx, atomic.LoadUint32(&p.writeIdx))
}

// AcquireRead gets the latest complete snapshot (consumer only, called from render)
// Returns nil if no snapshot is available yet
func (p *SnapshotPool) AcquireRead() *GameSnapshot {
	idx := atomic.LoadUint32(&p.readIdx) % 3
	return &p.snapshots[idx]
}

// GetLimits returns the resource limits
func (p *SnapshotPool) GetLimits() ResourceLimits {
	return p.limits
}
