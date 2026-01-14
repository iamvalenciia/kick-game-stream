package game

// CombatState tracks combo chains and defensive moves for a player.
// All timers use tick-based counting for deterministic replay.
type CombatState struct {
	// Combo system
	ComboCount     int    // Current combo hits (0 to MaxHits)
	ComboWindow    int    // Ticks remaining to chain next hit
	LastAttackTick uint64 // Tick when last attack occurred

	// Dodge system
	IsDodging      bool    // Currently in dodge animation
	DodgeTimer     int     // Remaining dodge ticks
	DodgeCooldown  int     // Ticks until next dodge
	DodgeDirection float64 // Direction of dodge (radians)

	// Invulnerability (from dodge)
	InvulnFrames int // Remaining invulnerability ticks
}

// CombatConstants defines balance parameters for combat mechanics.
// These are server-authoritative and cannot be modified by clients.
const (
	// Stamina
	MaxStamina       float64 = 100.0
	StaminaRegenRate float64 = 20.0 // Per second
	DodgeStaminaCost float64 = 40.0

	// Dodge timing (in ticks at 20 TPS)
	DodgeDurationTicks int     = 6  // 0.3 seconds
	DodgeCooldownTicks int     = 20 // 1.0 second
	DodgeInvulnTicks   int     = 4  // 0.2 seconds (i-frames)
	DodgeDistance      float64 = 120.0

	// Combo timing
	ComboWindowTicks int = 12 // 0.6 seconds to chain

	// Effect limits (DoS protection)
	MaxWeaponTrails   int     = 50
	MaxImpactFlashes  int     = 30
	MaxShakePerTick   int     = 2
	MaxShakeIntensity float64 = 8.0
)

// ComboDefinition defines timing windows and damage scaling for weapon combos.
type ComboDefinition struct {
	MaxHits     int       // Maximum combo chain length
	WindowTicks int       // Ticks to chain next hit
	DamageScale []float64 // Damage multiplier per hit in chain
}

// DefaultComboDefinitions returns combo stats for each weapon.
func DefaultComboDefinitions() map[string]ComboDefinition {
	return map[string]ComboDefinition{
		"fists": {
			MaxHits:     4,
			WindowTicks: 10,
			DamageScale: []float64{1.0, 1.1, 1.2, 1.5}, // Fast combo finisher
		},
		"knife": {
			MaxHits:     3,
			WindowTicks: 8,
			DamageScale: []float64{1.0, 1.2, 1.4},
		},
		"sword": {
			MaxHits:     3,
			WindowTicks: 12,
			DamageScale: []float64{1.0, 1.3, 1.6},
		},
		"axe": {
			MaxHits:     2,
			WindowTicks: 16,
			DamageScale: []float64{1.0, 1.8}, // Slow but powerful finisher
		},
		"katana": {
			MaxHits:     4,
			WindowTicks: 10,
			DamageScale: []float64{1.0, 1.15, 1.3, 2.0}, // Precision combo
		},
		"hammer": {
			MaxHits:     2,
			WindowTicks: 20,
			DamageScale: []float64{1.0, 2.0}, // Devastating finisher
		},
		"scythe": {
			MaxHits:     3,
			WindowTicks: 14,
			DamageScale: []float64{1.0, 1.4, 1.8},
		},
	}
}

// Reset clears combat state (called on respawn).
func (c *CombatState) Reset() {
	c.ComboCount = 0
	c.ComboWindow = 0
	c.LastAttackTick = 0
	c.IsDodging = false
	c.DodgeTimer = 0
	c.DodgeCooldown = 0
	c.DodgeDirection = 0
	c.InvulnFrames = 0
}

// UpdateTimers decrements all tick-based timers. Called once per game tick.
func (c *CombatState) UpdateTimers() {
	if c.ComboWindow > 0 {
		c.ComboWindow--
		if c.ComboWindow == 0 {
			c.ComboCount = 0 // Reset combo if window expires
		}
	}

	if c.DodgeTimer > 0 {
		c.DodgeTimer--
		if c.DodgeTimer == 0 {
			c.IsDodging = false
		}
	}

	if c.DodgeCooldown > 0 {
		c.DodgeCooldown--
	}

	if c.InvulnFrames > 0 {
		c.InvulnFrames--
	}
}

// CanDodge returns whether a dodge can be initiated.
func (c *CombatState) CanDodge(stamina float64) bool {
	return !c.IsDodging && c.DodgeCooldown == 0 && stamina >= DodgeStaminaCost
}

// StartDodge initiates a dodge in the given direction.
func (c *CombatState) StartDodge(direction float64) {
	c.IsDodging = true
	c.DodgeTimer = DodgeDurationTicks
	c.DodgeCooldown = DodgeCooldownTicks
	c.DodgeDirection = direction
	c.InvulnFrames = DodgeInvulnTicks
}

// IsInvulnerable returns whether the player cannot be hit.
func (c *CombatState) IsInvulnerable() bool {
	return c.InvulnFrames > 0
}

// RegisterHit records an attack hit for combo tracking.
// Returns the combo multiplier to apply to damage.
func (c *CombatState) RegisterHit(currentTick uint64, combo ComboDefinition) float64 {
	// Check if within combo window
	if c.ComboWindow > 0 && c.ComboCount < combo.MaxHits {
		c.ComboCount++
	} else {
		c.ComboCount = 1 // Start new combo
	}

	c.ComboWindow = combo.WindowTicks
	c.LastAttackTick = currentTick

	// Return damage multiplier (bounds-checked)
	idx := c.ComboCount - 1
	if idx >= 0 && idx < len(combo.DamageScale) {
		return combo.DamageScale[idx]
	}
	return 1.0
}
