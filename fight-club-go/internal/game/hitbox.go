package game

import "math"

// HitboxType defines the shape of a weapon's attack area.
type HitboxType int

const (
	HitboxCircle     HitboxType = iota // 360° area (fists, hammer)
	HitboxArc                          // Directional sweep (sword, axe)
	HitboxLine                         // Narrow thrust (spear, katana)
	HitboxProjectile                   // Separate entity (bow) - not handled here
)

// Hitbox represents a weapon's attack collision shape.
// All checks are O(1) - no polygon iteration.
type Hitbox struct {
	Type      HitboxType
	Range     float64 // Distance from attacker center
	Width     float64 // Arc width in radians (for Arc), line width in pixels (for Line)
	Direction float64 // Facing direction in radians (set per attack)
}

// CheckHit tests if a target point is within the hitbox.
// attackerX, attackerY: attacker position
// targetX, targetY: target position
// direction: attack facing direction (radians)
//
// Returns true if the target is hit.
// All calculations are O(1) using angle/distance math.
func (h *Hitbox) CheckHit(attackerX, attackerY, targetX, targetY, direction float64) bool {
	dx := targetX - attackerX
	dy := targetY - attackerY
	distance := math.Sqrt(dx*dx + dy*dy)

	// Range check (applies to all types)
	if distance > h.Range {
		return false
	}

	// Minimum distance to prevent self-collision
	if distance < 1.0 {
		return false
	}

	switch h.Type {
	case HitboxCircle:
		// Circle hits everything in range (already checked above)
		return true

	case HitboxArc:
		// Check if target is within the arc angle
		targetAngle := math.Atan2(dy, dx)
		angleDiff := normalizeAngle(targetAngle - direction)
		halfWidth := h.Width / 2
		return angleDiff >= -halfWidth && angleDiff <= halfWidth

	case HitboxLine:
		// Line hitbox: check if target is within a narrow cone
		// Width is used as the line's half-width in pixels
		targetAngle := math.Atan2(dy, dx)
		angleDiff := normalizeAngle(targetAngle - direction)

		// Convert pixel width to angular width at the target distance
		angularWidth := math.Atan2(h.Width, distance)
		return angleDiff >= -angularWidth && angleDiff <= angularWidth

	case HitboxProjectile:
		// Projectiles are handled separately by their own collision
		return false
	}

	return false
}

// normalizeAngle normalizes an angle to the range [-π, π].
// Uses O(1) modulo arithmetic instead of O(iterations) while loops.
func normalizeAngle(angle float64) float64 {
	// Normalize to [0, 2π) first, then shift to [-π, π]
	const twoPi = 2 * math.Pi
	angle = math.Mod(angle, twoPi)
	if angle < 0 {
		angle += twoPi
	}
	if angle > math.Pi {
		angle -= twoPi
	}
	return angle
}

// cachedHitboxes stores the hitbox configurations to avoid map allocation on every call.
// This is a package-level cache initialized once.
var cachedHitboxes = map[string]Hitbox{
	"fists": {
		Type:  HitboxCircle,
		Range: 80,
	},
	"knife": {
		Type:  HitboxArc,
		Range: 90,
		Width: math.Pi / 2, // 90 degrees
	},
	"sword": {
		Type:  HitboxArc,
		Range: 100,
		Width: 2 * math.Pi / 3, // 120 degrees
	},
	"spear": {
		Type:  HitboxLine,
		Range: 150,
		Width: 15, // 15 pixel half-width (narrow thrust)
	},
	"axe": {
		Type:  HitboxArc,
		Range: 95,
		Width: 5 * math.Pi / 6, // 150 degrees (wide cleave)
	},
	"bow": {
		Type:  HitboxProjectile,
		Range: 250, // Maximum projectile range (for AI targeting)
	},
	"katana": {
		Type:  HitboxLine,
		Range: 120,
		Width: 20, // 20 pixel half-width
	},
	"hammer": {
		Type:  HitboxCircle,
		Range: 90,
	},
	"scythe": {
		Type:  HitboxArc,
		Range: 140,
		Width: math.Pi, // 180 degrees (huge sweep)
	},
}

// defaultFistsHitbox is cached for fallback to avoid map lookup.
var defaultFistsHitbox = cachedHitboxes["fists"]

// DefaultHitboxes returns hitbox configurations for each weapon.
// Returns the cached map - callers should NOT modify the returned map.
func DefaultHitboxes() map[string]Hitbox {
	return cachedHitboxes
}

// GetHitbox returns the hitbox for a weapon ID.
// Uses cached map lookup - O(1) with no allocation.
func GetHitbox(weaponID string) Hitbox {
	if h, ok := cachedHitboxes[weaponID]; ok {
		return h
	}
	// Default to fists hitbox
	return defaultFistsHitbox
}
