package game

import "math"

// AttackPhase defines the stages of an attack animation
type AttackPhase int

const (
	PhaseIdle     AttackPhase = iota // Not attacking
	PhaseWindUp                      // Anticipation - preparing the attack
	PhaseActive                      // Attack is hitting - hitbox active
	PhaseRecovery                    // Follow-through - cannot attack again
)

// TrailType defines the visual style of weapon trails
type TrailType int

const (
	TrailNone       TrailType = iota // No trail
	TrailArc                         // Curved swing (sword, axe, scythe)
	TrailLine                        // Straight thrust (spear, katana)
	TrailRadial                      // 360° burst (fists, hammer)
	TrailProjectile                  // Moving entity (bow)
)

// WeaponAnimationConfig defines visual and timing properties per weapon
// This separates animation/feel from damage/balance in weapons.go
type WeaponAnimationConfig struct {
	WeaponID string

	// Attack Timing (in ticks at 20 TPS)
	WindUpTicks   int // Anticipation duration
	ActiveTicks   int // Damage window
	RecoveryTicks int // Follow-through duration

	// Motion
	LungeDistance  float64 // Forward movement on attack (pixels)
	RecoilDistance float64 // Backward movement after attack (pixels)

	// Visual Effect Type
	TrailType  TrailType // None, Arc, Line, Radial, Projectile
	TrailColor string    // Override weapon color if set (empty = use weapon color)
	TrailWidth float64   // Arc width in radians, or line width in pixels
	TrailCount int       // Number of trail segments to render

	// Impact Effects
	ShakeIntensity float64 // Screen shake on hit (0-8 scale, capped by MaxShakeIntensity)
	FlashRadius    float64 // Impact flash size (pixels)
	ParticleCount  int     // Particles spawned on hit

	// Movement Control (Hit Reactions)
	KnockbackForce   float64 // How far victim is pushed (pixels)
	KnockbackAngle   float64 // Override angle (0 = use attack direction)
	AttackerPushback float64 // How far attacker slides back after hit (pixels)

	// Stun/Control
	StunDuration   float64 // Seconds victim is stunned (cannot act)
	SlowMultiplier float64 // Speed reduction during slow (1.0 = no slow, 0.5 = half speed)
	SlowDuration   float64 // How long slow lasts (seconds)

	// Projectile (bow only)
	IsProjectile    bool    // True = spawns projectile instead of instant hit
	ProjectileSpeed float64 // Pixels per second
}

// DefaultWeaponAnimations returns animation configurations for all weapons
// These are tuned for streaming readability at 720p/30 FPS
func DefaultWeaponAnimations() map[string]WeaponAnimationConfig {
	return map[string]WeaponAnimationConfig{
		// ==========================================================================
		// 👊 FISTS - Fast, close-range punches with minimal knockback
		// Feel: Rapid-fire jabs, quick in-and-out motion
		// Readable: Small radial burst on each hit, fast but visible
		// ==========================================================================
		"fists": {
			WeaponID:         "fists",
			WindUpTicks:      1,  // Very fast (0.05s)
			ActiveTicks:      2,  // Short window (0.1s)
			RecoveryTicks:    3,  // Quick recovery (0.15s)
			LungeDistance:    15, // Small forward lunge
			RecoilDistance:   8,  // Quick snap back
			TrailType:        TrailRadial,
			TrailWidth:       math.Pi / 4, // 45° burst
			TrailCount:       4,
			ShakeIntensity:   1.0, // Small shake
			FlashRadius:      8,
			ParticleCount:    2,
			KnockbackForce:   4,    // Minimal knockback
			AttackerPushback: 2,    // Slight bounce back
			StunDuration:     0.05, // Tiny stun
		},

		// ==========================================================================
		// 🔪 KNIFE - Quick slashes with short arc
		// Feel: Nimble, precise cuts
		// Readable: Short arc trail, faster than sword
		// ==========================================================================
		"knife": {
			WeaponID:         "knife",
			WindUpTicks:      1,
			ActiveTicks:      2,
			RecoveryTicks:    3,
			LungeDistance:    12,
			RecoilDistance:   6,
			TrailType:        TrailArc,
			TrailWidth:       math.Pi / 2, // 90° arc
			TrailCount:       5,
			ShakeIntensity:   1.5,
			FlashRadius:      10,
			ParticleCount:    2,
			KnockbackForce:   6,
			AttackerPushback: 3,
			StunDuration:     0.08,
		},

		// ==========================================================================
		// ⚔️ SWORD - Horizontal/diagonal slash with visible arc trail
		// Feel: Classic RPG sword swing, satisfying cleave
		// Readable: Long arc trail, clear anticipation and follow-through
		// ==========================================================================
		"sword": {
			WeaponID:         "sword",
			WindUpTicks:      3,  // Visible wind-up (0.15s)
			ActiveTicks:      3,  // Good active window (0.15s)
			RecoveryTicks:    4,  // Medium recovery (0.2s)
			LungeDistance:    20, // Forward step on swing
			RecoilDistance:   10, // Pulls back after swing
			TrailType:        TrailArc,
			TrailWidth:       2 * math.Pi / 3, // 120° arc
			TrailCount:       6,
			ShakeIntensity:   2.5,
			FlashRadius:      15,
			ParticleCount:    3,
			KnockbackForce:   12, // Medium knockback
			AttackerPushback: 5,
			StunDuration:     0.1, // Brief stun
		},

		// ==========================================================================
		// 🔱 SPEAR - Precise thrust with line trail
		// Feel: Poke-and-keep-distance, spacing tool
		// Readable: Long straight line trail, keeps enemies at range
		// ==========================================================================
		"spear": {
			WeaponID:         "spear",
			WindUpTicks:      4,  // Longer wind-up (0.2s) - telegraphed
			ActiveTicks:      2,  // Short active window - precision
			RecoveryTicks:    5,  // Longer recovery (0.25s)
			LungeDistance:    30, // Long forward thrust
			RecoilDistance:   5,  // Small pull back
			TrailType:        TrailLine,
			TrailWidth:       20, // Line width in pixels
			TrailCount:       8,
			ShakeIntensity:   2.0,
			FlashRadius:      12,
			ParticleCount:    2,
			KnockbackForce:   8,    // Moderate push - spacing control
			AttackerPushback: 8,    // Attacker retreats to maintain distance
			StunDuration:     0.15, // Longer stun - poking interrupts
		},

		// ==========================================================================
		// 🪓 AXE - Heavy overhead swing with strong impact
		// Feel: Slow but devastating, satisfying crunch
		// Readable: Long wind-up, huge arc, massive shake on impact
		// ==========================================================================
		"axe": {
			WeaponID:         "axe",
			WindUpTicks:      6,  // Slow wind-up (0.3s) - very telegraphed
			ActiveTicks:      4,  // Wide active window (0.2s)
			RecoveryTicks:    6,  // Slow recovery (0.3s)
			LungeDistance:    10, // Small lunge (heavy weapon)
			RecoilDistance:   15, // Strong recoil from impact
			TrailType:        TrailArc,
			TrailWidth:       5 * math.Pi / 6, // 150° cleave
			TrailCount:       7,
			ShakeIntensity:   5.0, // BIG shake
			FlashRadius:      25,
			ParticleCount:    5,
			KnockbackForce:   25, // MASSIVE knockback
			AttackerPushback: 8,
			StunDuration:     0.25, // Long stun - hit stagger
		},

		// ==========================================================================
		// 🏹 BOW - Projectile-based attack with distance control
		// Feel: Ranged poke, arrows visibly travel through space
		// Readable: Arrow spawns at attacker, flies to target, pushes on hit
		// ==========================================================================
		"bow": {
			WeaponID:         "bow",
			WindUpTicks:      8, // Drawing the bow (0.4s)
			ActiveTicks:      1, // Release is instant
			RecoveryTicks:    4, // Nocking next arrow (0.2s)
			LungeDistance:    0, // No lunge - ranged
			RecoilDistance:   5, // Slight knockback from shot
			TrailType:        TrailProjectile,
			TrailCount:       4,   // Arrow trail length
			ShakeIntensity:   2.0, // On arrow hit
			FlashRadius:      15,  // Impact flash
			ParticleCount:    3,
			KnockbackForce:   18,   // Strong push - distance control
			AttackerPushback: 0,    // No pushback for shooter
			StunDuration:     0,    // No stun - distance not control
			IsProjectile:     true, // Uses projectile system
			ProjectileSpeed:  500,  // Pixels per second
		},

		// ==========================================================================
		// ⚔️ SCYTHE - Wide dramatic sweeping arcs (Ultimate weapon)
		// Feel: Death's weapon, devastating sweep
		// Readable: HUGE arc, long trail, dramatic shake
		// ==========================================================================
		"scythe": {
			WeaponID:         "scythe",
			WindUpTicks:      5,  // Medium wind-up (0.25s)
			ActiveTicks:      5,  // Long active (0.25s) - sweeping motion
			RecoveryTicks:    5,  // Medium recovery (0.25s)
			LungeDistance:    25, // Forward sweep
			RecoilDistance:   12,
			TrailType:        TrailArc,
			TrailWidth:       math.Pi, // 180° HUGE sweep
			TrailCount:       8,
			ShakeIntensity:   4.0,
			FlashRadius:      20,
			ParticleCount:    4,
			KnockbackForce:   20, // Strong knockback
			AttackerPushback: 6,
			StunDuration:     0.2, // Solid stun
		},

		// ==========================================================================
		// 🗡️ KATANA - Precise line attacks with combo potential
		// Feel: Iaido quick-draw slashes, combo-oriented
		// Readable: Fast line trails, multiple quick hits
		// ==========================================================================
		"katana": {
			WeaponID:         "katana",
			WindUpTicks:      2,
			ActiveTicks:      2,
			RecoveryTicks:    3,
			LungeDistance:    18,
			RecoilDistance:   8,
			TrailType:        TrailLine,
			TrailWidth:       25,
			TrailCount:       6,
			ShakeIntensity:   2.0,
			FlashRadius:      12,
			ParticleCount:    2,
			KnockbackForce:   10,
			AttackerPushback: 4,
			StunDuration:     0.08,
		},

		// ==========================================================================
		// 🔨 HAMMER - Ground-pound style heavy weapon
		// Feel: Slow but devastating AoE, ground shake
		// Readable: Big radial burst, massive screen shake
		// ==========================================================================
		"hammer": {
			WeaponID:         "hammer",
			WindUpTicks:      7,  // Very slow (0.35s)
			ActiveTicks:      4,  // Wide window (0.2s)
			RecoveryTicks:    7,  // Very slow recovery (0.35s)
			LungeDistance:    8,  // Small lunge
			RecoilDistance:   20, // Big recoil from impact
			TrailType:        TrailRadial,
			TrailWidth:       math.Pi, // Full 180° smash
			TrailCount:       6,
			ShakeIntensity:   6.0, // MASSIVE shake
			FlashRadius:      30,
			ParticleCount:    6,
			KnockbackForce:   30, // HUGE knockback
			AttackerPushback: 10,
			StunDuration:     0.3, // Long stun - stagger
		},
	}
}

// GetWeaponAnimation returns the animation config for a weapon ID
// Defaults to fists if weapon not found
func GetWeaponAnimation(weaponID string) WeaponAnimationConfig {
	anims := DefaultWeaponAnimations()
	if anim, ok := anims[weaponID]; ok {
		return anim
	}
	return anims["fists"]
}

// TotalAttackTicks returns the full animation duration in ticks
func (c *WeaponAnimationConfig) TotalAttackTicks() int {
	return c.WindUpTicks + c.ActiveTicks + c.RecoveryTicks
}

// TotalAttackDuration returns the full animation duration in seconds (at 20 TPS)
func (c *WeaponAnimationConfig) TotalAttackDuration() float64 {
	return float64(c.TotalAttackTicks()) / 20.0
}
