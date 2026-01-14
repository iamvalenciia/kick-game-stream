package game

import (
	"fmt"
	"math"
)

// Projectile represents a moving attack entity (arrows, thrown weapons)
// Projectiles travel through space over multiple frames and check collision each tick
type Projectile struct {
	ID        string // Unique identifier
	OwnerID   string // Player who fired this projectile
	OwnerName string // For kill credit

	// Position and motion
	X, Y   float64 // Current position
	VX, VY float64 // Velocity (pixels per tick at 20 TPS)
	Speed  float64 // Speed magnitude

	// Combat
	Damage    int     // Damage dealt on hit
	HitRadius float64 // Collision radius

	// Visual
	Color    string  // Projectile color
	Rotation float64 // Angle of travel (radians)

	// Lifetime
	Timer int // Remaining lifetime in ticks

	// Trail positions (ring buffer for efficiency)
	TrailX   [4]float64
	TrailY   [4]float64
	TrailIdx int
}

// Projectile system constants
const (
	MaxProjectiles     = 30   // Hard cap to prevent DoS
	ProjectileLifetime = 60   // 3 seconds at 20 TPS
	ProjectileRadius   = 8.0  // Collision radius
	PlayerRadius       = 28.0 // Player hitbox radius
)

// NewProjectile creates a new projectile aimed at a target
func NewProjectile(owner *Player, targetX, targetY float64, damage int, tickCount int64) *Projectile {
	anim := GetWeaponAnimation(owner.Weapon)
	weapon := GetWeapon(owner.Weapon)

	dx := targetX - owner.X
	dy := targetY - owner.Y
	dist := math.Sqrt(dx*dx + dy*dy)

	if dist == 0 {
		dist = 1 // Prevent division by zero
	}

	// Normalize direction
	dirX := dx / dist
	dirY := dy / dist

	// Speed is in pixels/second, convert to pixels/tick at 20 TPS
	speedPerTick := anim.ProjectileSpeed / 20.0

	// Start projectile at player's edge (not center) in the direction of fire
	startX := owner.X + dirX*40
	startY := owner.Y + dirY*40

	return &Projectile{
		ID:        fmt.Sprintf("proj_%d_%s", tickCount, owner.ID),
		OwnerID:   owner.ID,
		OwnerName: owner.Name,
		X:         startX,
		Y:         startY,
		VX:        dirX * speedPerTick,
		VY:        dirY * speedPerTick,
		Speed:     speedPerTick,
		Damage:    damage,
		HitRadius: ProjectileRadius,
		Color:     weapon.Color,
		Rotation:  math.Atan2(dy, dx),
		Timer:     ProjectileLifetime,
		TrailIdx:  0,
	}
}

// Update moves the projectile and decrements its lifetime
// Returns false if the projectile should be removed
func (p *Projectile) Update(deltaTime float64) bool {
	// Store current position in trail (before moving)
	p.TrailX[p.TrailIdx] = p.X
	p.TrailY[p.TrailIdx] = p.Y
	p.TrailIdx = (p.TrailIdx + 1) % 4

	// Move projectile (velocity is already in pixels/tick)
	p.X += p.VX
	p.Y += p.VY

	// Decrease lifetime
	p.Timer--

	// Check if out of bounds or expired
	if p.X < -50 || p.X > 1970 || p.Y < -50 || p.Y > 1130 {
		return false // Remove - out of bounds
	}

	if p.Timer <= 0 {
		return false // Remove - expired
	}

	return true // Keep alive
}

// CheckHit tests if this projectile collides with a player
// Returns true if collision detected
func (p *Projectile) CheckHit(target *Player) bool {
	// Don't hit dead players or players with spawn protection
	if target.IsDead || target.IsRagdoll || target.SpawnProtection {
		return false
	}

	// Don't hit the owner
	if target.ID == p.OwnerID {
		return false
	}

	// Check invulnerability (dodge i-frames)
	if target.Combat.IsInvulnerable() {
		return false
	}

	// Distance check
	dx := target.X - p.X
	dy := target.Y - p.Y
	dist := math.Sqrt(dx*dx + dy*dy)

	return dist < (p.HitRadius + PlayerRadius)
}

// GetTrailPoints returns the trail positions in order (oldest to newest)
func (p *Projectile) GetTrailPoints() (xs, ys [4]float64, count int) {
	// Return all 4 trail points starting from oldest
	startIdx := p.TrailIdx
	for i := 0; i < 4; i++ {
		idx := (startIdx + i) % 4
		xs[i] = p.TrailX[idx]
		ys[i] = p.TrailY[idx]
	}
	return xs, ys, 4
}

// ProjectileSnapshot is an immutable copy of projectile state for rendering
type ProjectileSnapshot struct {
	X, Y       float64
	Rotation   float64
	Color      string
	TrailX     [4]float64
	TrailY     [4]float64
	TrailCount int
}

// ToSnapshot creates an immutable snapshot for rendering
func (p *Projectile) ToSnapshot() ProjectileSnapshot {
	xs, ys, count := p.GetTrailPoints()
	return ProjectileSnapshot{
		X:          p.X,
		Y:          p.Y,
		Rotation:   p.Rotation,
		Color:      p.Color,
		TrailX:     xs,
		TrailY:     ys,
		TrailCount: count,
	}
}
