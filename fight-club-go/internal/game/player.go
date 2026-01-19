package game

import (
	"fmt"
	"math"
	"math/rand"
	"time"

	"fight-club/internal/game/spatial"
)

// PlayerState represents the player's lifecycle state
type PlayerState int

const (
	StateOut   PlayerState = iota // Not in arena (never joined or left)
	StateAlive                    // In arena, alive and fighting
	StateDead                     // Dead, must type !join to re-enter
)

// Player represents a game player with AI behavior
type Player struct {
	ID     string  `json:"id"`
	Name   string  `json:"name"`
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	VX     float64 `json:"vx"`
	VY     float64 `json:"vy"`
	HP     int     `json:"hp"`
	MaxHP  int     `json:"maxHp"`
	Money  int     `json:"money"`
	Kills  int     `json:"kills"`
	Deaths int     `json:"deaths"`
	Weapon string  `json:"weapon"`
	Color  string  `json:"color"`
	Avatar string  `json:"avatar"`

	// Combat state
	Target         *Player `json:"-"`
	IsAttacking    bool    `json:"isAttacking"`
	AttackCooldown float64 `json:"-"`
	AttackAngle    float64 `json:"attackAngle"`

	// Aggression (personality)
	Aggression float64 `json:"-"`

	// Death state
	IsDead          bool    `json:"isDead"`
	IsRagdoll       bool    `json:"isRagdoll"`
	RagdollTimer    float64 `json:"-"`
	RagdollRotation float64 `json:"ragdollRotation"`

	// Protection
	SpawnProtection bool    `json:"spawnProtection"`
	SpawnTimer      float64 `json:"-"`

	// Stun state
	IsStunned bool    `json:"isStunned"`
	StunTimer float64 `json:"-"`

	// Profile
	ProfilePic string `json:"profilePic"`

	// Advanced combat state (combos, dodge)
	Combat CombatState `json:"-"`

	// Stamina for dodge (regenerates over time)
	Stamina    float64 `json:"stamina"`
	MaxStamina float64 `json:"-"`

	// Dodge state (for rendering)
	IsDodging bool `json:"isDodging"`

	// Lifecycle state (explicit state machine)
	State PlayerState `json:"-"`

	// Focus targeting
	FocusTarget string  `json:"-"` // Username of focused target
	FocusTTL    float64 `json:"-"` // Time remaining for focus

	// Team membership
	TeamID string `json:"teamId"`

	// Chat bubble (visible above player)
	ChatBubble    string  `json:"chatBubble"`
	ChatBubbleTTL float64 `json:"-"`

	// World bounds (stored for consistent bounds clamping)
	worldWidth  float64
	worldHeight float64
}

// PlayerOptions contains options for creating a player
type PlayerOptions struct {
	ProfilePic  string
	Color       string
	WorldWidth  float64 // Spawn bounds - defaults to 1280 if not set
	WorldHeight float64 // Spawn bounds - defaults to 720 if not set
}

var playerColors = []string{
	"#ff6b6b", "#4ecdc4", "#45b7d1", "#96ceb4",
	"#ffeaa7", "#dfe6e9", "#fd79a8", "#00b894",
	"#6c5ce7", "#fdcb6e", "#e17055", "#00cec9",
}

var avatars = []string{"😀", "😎", "🤠", "🥷", "👽", "🤖", "👻", "💀", "🐱", "🐶"}

// NewPlayer creates a new player
func NewPlayer(name string, opts PlayerOptions) *Player {
	id := fmt.Sprintf("player_%d_%s", time.Now().UnixNano(), name)

	color := opts.Color
	if color == "" {
		color = playerColors[rand.Intn(len(playerColors))]
	}

	// Use provided world bounds or defaults
	worldWidth := opts.WorldWidth
	worldHeight := opts.WorldHeight
	if worldWidth == 0 {
		worldWidth = 1280
	}
	if worldHeight == 0 {
		worldHeight = 720
	}

	return &Player{
		ID:              id,
		Name:            name,
		X:               rand.Float64() * worldWidth,
		Y:               rand.Float64() * worldHeight,
		HP:              100,
		MaxHP:           100,
		Money:           0,
		Weapon:          "fists",
		Color:           color,
		Avatar:          avatars[rand.Intn(len(avatars))],
		SpawnProtection: true,
		SpawnTimer:      0.3, // Reduced to 0.3s for instant combat (was 1.5)
		Aggression:      0.5 + rand.Float64()*0.5, // 0.5 to 1.0
		ProfilePic:      opts.ProfilePic,
		Stamina:         MaxStamina,
		MaxStamina:      MaxStamina,
		State:           StateAlive, // Explicitly set initial state
		worldWidth:      worldWidth,
		worldHeight:     worldHeight,
	}
}

// Update updates the player state each tick
// selfIdx: index of this player in the players slice
// grid: spatial grid for O(1) neighbor queries
// playerMap: optional map[string]*Player for O(1) focus target lookup
func (p *Player) Update(players []*Player, selfIdx uint32, grid *spatial.SpatialGrid, deltaTime float64, engine *Engine, playerMap ...map[string]*Player) {
	if p.IsDead || p.IsRagdoll {
		return
	}

	// Update combat state timers (ticks, deterministic)
	p.Combat.UpdateTimers()
	p.IsDodging = p.Combat.IsDodging

	// Stamina regeneration
	if p.Stamina < p.MaxStamina {
		p.Stamina += StaminaRegenRate * deltaTime
		if p.Stamina > p.MaxStamina {
			p.Stamina = p.MaxStamina
		}
	}

	// Apply dodge velocity if dodging
	if p.Combat.IsDodging {
		dodgeSpeed := DodgeDistance / float64(DodgeDurationTicks) * float64(engine.tickRate)
		p.VX = math.Cos(p.Combat.DodgeDirection) * dodgeSpeed * deltaTime
		p.VY = math.Sin(p.Combat.DodgeDirection) * dodgeSpeed * deltaTime
	}

	// Update timers
	if p.SpawnTimer > 0 {
		p.SpawnTimer -= deltaTime
		if p.SpawnTimer <= 0 {
			p.SpawnProtection = false
		}
	}

	if p.StunTimer > 0 {
		p.StunTimer -= deltaTime
		if p.StunTimer <= 0 {
			p.IsStunned = false
		}
		return // Can't act while stunned
	}

	if p.AttackCooldown > 0 {
		p.AttackCooldown -= deltaTime
	}

	// Find target using spatial grid (O(k) instead of O(n))
	// Pass playerMap for O(1) focus target lookup if available
	p.findTarget(players, selfIdx, grid, playerMap...)

	// AI behavior
	if p.Target != nil {
		p.combatBehavior(deltaTime, engine)
	} else {
		p.wander(deltaTime)
	}

	// Apply velocity with speed limit
	speed := math.Sqrt(p.VX*p.VX + p.VY*p.VY)
	maxSpeed := 6.0
	if speed > maxSpeed {
		p.VX = (p.VX / speed) * maxSpeed
		p.VY = (p.VY / speed) * maxSpeed
	}

	p.X += p.VX
	p.Y += p.VY

	// Friction
	p.VX *= 0.85
	p.VY *= 0.85

	// World bounds (use stored bounds with margin)
	margin := 40.0
	p.X = math.Max(margin, math.Min(p.worldWidth-margin, p.X))
	p.Y = math.Max(margin, math.Min(p.worldHeight-margin, p.Y))
}

// findTarget uses spatial grid for O(k) neighbor lookup instead of O(n) scan
// When no nearby target is found, falls back to global search for exploration
// playerMap is optional - if provided, enables O(1) focus target lookup
func (p *Player) findTarget(players []*Player, selfIdx uint32, grid *spatial.SpatialGrid, playerMap ...map[string]*Player) {
	// Priority 1: Focus target (if valid and alive)
	if p.FocusTarget != "" {
		var focusedPlayer *Player

		// Use O(1) map lookup if playerMap is provided, otherwise O(n) linear search
		if len(playerMap) > 0 && playerMap[0] != nil {
			focusedPlayer = playerMap[0][p.FocusTarget]
		} else {
			// Fallback to linear search if map not provided
			for _, other := range players {
				if other.Name == p.FocusTarget {
					focusedPlayer = other
					break
				}
			}
		}

		if focusedPlayer != nil && !focusedPlayer.IsDead && !focusedPlayer.IsRagdoll {
			// NOTE: Allows targeting spawn-protected - approach immediately
			// Also check team - can't focus teammates
			if p.TeamID == "" || p.TeamID != focusedPlayer.TeamID {
				p.Target = focusedPlayer
				return
			}
		}
		// Focus target not found or invalid, clear it
		p.FocusTarget = ""
		p.FocusTTL = 0
	}

	// Priority 2: Closest valid target using spatial grid (O(k) instead of O(n))
	var closest *Player
	minDist := math.MaxFloat64

	// First try nearby detection range for immediate combat
	const combatRange = 300.0 // Immediate combat detection
	candidates := grid.QueryRadius(p.X, p.Y, combatRange)

	for _, idx := range candidates {
		if idx == selfIdx {
			continue // Skip self
		}
		other := players[idx]
		if other.IsDead || other.IsRagdoll {
			continue
		}
		// NOTE: Removed SpawnProtection check - allows approaching spawn-protected
		// targets immediately. Attack will be blocked, but movement starts instantly.
		// Skip teammates (no friendly fire)
		if p.TeamID != "" && p.TeamID == other.TeamID {
			continue
		}

		dist := p.distanceTo(other)
		if dist < minDist {
			minDist = dist
			closest = other
		}
	}

	// If no nearby target found, do GLOBAL search for exploration
	// This ensures players always find someone to fight
	if closest == nil {
		minDist = math.MaxFloat64
		for i, other := range players {
			if uint32(i) == selfIdx {
				continue
			}
			if other.IsDead || other.IsRagdoll {
				continue
			}
			// NOTE: Allows targeting spawn-protected for immediate approach
			// Skip teammates
			if p.TeamID != "" && p.TeamID == other.TeamID {
				continue
			}

			dist := p.distanceTo(other)
			if dist < minDist {
				minDist = dist
				closest = other
			}
		}
	}

	p.Target = closest
}

// combatBehavior handles all combat AI - approaching, attacking, retreating
// OPTIMIZED: No dead zones - always moving or attacking for instant combat
func (p *Player) combatBehavior(deltaTime float64, engine *Engine) {
	if p.Target == nil {
		return
	}

	weapon := GetWeapon(p.Weapon)
	dist := p.distanceTo(p.Target)

	dx := p.Target.X - p.X
	dy := p.Target.Y - p.Y

	if dist > 0 {
		dx /= dist
		dy /= dist
	}

	attackRange := weapon.Range
	canAttack := p.AttackCooldown <= 0 && !p.Target.SpawnProtection && !p.SpawnProtection

	// Always face target first
	p.AttackAngle = math.Atan2(dy, dx)

	// IMMEDIATE ATTACK: In range and ready - highest priority
	if dist <= attackRange && canAttack {
		p.attack(engine)
		// Small backwards movement after attack
		p.VX -= dx * 1.5
		p.VY -= dy * 1.5
		return
	}

	// MOVEMENT LOGIC: No dead zones - always moving toward optimal position
	moveSpeed := 5.0 * p.Aggression
	minCombatDist := 40.0 // Minimum distance to maintain (avoids clipping)

	if dist < minCombatDist {
		// TOO CLOSE - back up slightly while strafing
		perpX := -dy
		perpY := dx
		if rand.Float64() < 0.5 {
			perpX = dy
			perpY = -dx
		}
		// Move backward + strafe
		p.VX += (-dx*0.5 + perpX*0.5) * 3.0 * deltaTime * 60
		p.VY += (-dy*0.5 + perpY*0.5) * 3.0 * deltaTime * 60
	} else if dist > attackRange*0.8 {
		// OUT OF RANGE - aggressive approach (0.8 gives buffer for attack)
		// Very far away (>400px) - use flow fields for smart navigation
		if dist > 400 {
			if flowMgr := engine.GetFlowFieldManager(); flowMgr != nil {
				goalKey := p.Target.ID
				field := flowMgr.GetOrCreate(goalKey, p.Target.X, p.Target.Y)
				flowX, flowY := field.Lookup(p.X, p.Y)

				if flowX != 0 || flowY != 0 {
					blendedDX := float64(flowX)*0.5 + dx*0.5
					blendedDY := float64(flowY)*0.5 + dy*0.5
					length := math.Sqrt(blendedDX*blendedDX + blendedDY*blendedDY)
					if length > 0 {
						blendedDX /= length
						blendedDY /= length
					}
					p.VX += blendedDX * moveSpeed * deltaTime * 60
					p.VY += blendedDY * moveSpeed * deltaTime * 60
				} else {
					p.VX += dx * moveSpeed * deltaTime * 60
					p.VY += dy * moveSpeed * deltaTime * 60
				}
			} else {
				p.VX += dx * moveSpeed * deltaTime * 60
				p.VY += dy * moveSpeed * deltaTime * 60
			}
		} else {
			// Close-mid range: DIRECT aggressive approach
			p.VX += dx * moveSpeed * deltaTime * 60
			p.VY += dy * moveSpeed * deltaTime * 60
		}
	} else {
		// IN ATTACK ZONE but waiting for cooldown - aggressive strafe toward target
		// Mix of approach + strafe to stay in range and pressure
		perpX := -dy
		perpY := dx
		if rand.Float64() < 0.5 {
			perpX = dy
			perpY = -dx
		}
		// 70% strafe, 30% approach - keeps pressure on
		p.VX += (dx*0.3 + perpX*0.7) * 3.5 * deltaTime * 60
		p.VY += (dy*0.3 + perpY*0.7) * 3.5 * deltaTime * 60
	}
}

func (p *Player) wander(deltaTime float64) {
	// Random wandering towards center (using stored world bounds)
	centerX := p.worldWidth / 2
	centerY := p.worldHeight / 2

	dx := centerX - p.X
	dy := centerY - p.Y
	dist := math.Sqrt(dx*dx + dy*dy)

	// Slight pull towards center if far away
	if dist > 400 {
		p.VX += (dx / dist) * 0.3
		p.VY += (dy / dist) * 0.3
	}

	// Random movement
	if rand.Float64() < 0.05 {
		angle := rand.Float64() * math.Pi * 2
		p.VX += math.Cos(angle) * 1.0
		p.VY += math.Sin(angle) * 1.0
	}
}

func (p *Player) attack(engine *Engine) {
	if p.Target == nil {
		return
	}

	weapon := GetWeapon(p.Weapon)
	anim := GetWeaponAnimation(p.Weapon)

	p.AttackCooldown = weapon.Cooldown
	p.IsAttacking = true
	p.AttackAngle = math.Atan2(p.Target.Y-p.Y, p.Target.X-p.X)

	// Apply lunge motion toward target (weapon-specific)
	if anim.LungeDistance > 0 && p.Target != nil {
		dx := p.Target.X - p.X
		dy := p.Target.Y - p.Y
		dist := math.Sqrt(dx*dx + dy*dy)
		if dist > 0 {
			// Add velocity toward target
			p.VX += (dx / dist) * anim.LungeDistance * 0.5
			p.VY += (dy / dist) * anim.LungeDistance * 0.5
		}
	}

	// Calculate damage with variance
	damageRange := weapon.MaxDamage - weapon.MinDamage
	damage := weapon.MinDamage + rand.Intn(damageRange+1)

	// Critical hit chance (10%)
	if rand.Float64() < 0.1 {
		damage = int(float64(damage) * 1.5)
	}

	// NOTE: Visual effects are now created in ProcessAttack ONLY when hit connects
	// This prevents effect accumulation from missed attacks

	// Process the attack - effects created here if hit connects
	engine.ProcessAttack(p, p.Target, damage)

	// Reset attacking flag after a delay (longer for slow weapons)
	attackDuration := time.Duration(anim.ActiveTicks*50) * time.Millisecond
	if attackDuration < 150*time.Millisecond {
		attackDuration = 150 * time.Millisecond
	}
	go func() {
		time.Sleep(attackDuration)
		p.IsAttacking = false
	}()
}

// TakeDamage applies damage to the player
func (p *Player) TakeDamage(amount int, attacker *Player) {
	// Check all protection states
	if p.SpawnProtection || p.IsDead || p.Combat.IsInvulnerable() {
		return
	}

	p.HP -= amount

	// Weapon-specific knockback and stun
	if attacker != nil {
		anim := GetWeaponAnimation(attacker.Weapon)

		dx := p.X - attacker.X
		dy := p.Y - attacker.Y
		dist := math.Sqrt(dx*dx + dy*dy)
		if dist > 0 {
			// Apply weapon-specific knockback force
			p.VX += (dx / dist) * anim.KnockbackForce
			p.VY += (dy / dist) * anim.KnockbackForce

			// Attacker pushback (recoil) - makes heavy weapons feel impactful
			if anim.AttackerPushback > 0 {
				attacker.VX -= (dx / dist) * anim.AttackerPushback
				attacker.VY -= (dy / dist) * anim.AttackerPushback
			}
		}

		// Apply weapon-specific stun duration
		if anim.StunDuration > 0 {
			p.IsStunned = true
			p.StunTimer = anim.StunDuration
		}
	}

	if p.HP <= 0 {
		p.die(attacker)
	}
}

// Heal heals the player
func (p *Player) Heal(amount int) {
	p.HP = int(math.Min(float64(p.HP+amount), float64(p.MaxHP)))
}

func (p *Player) die(killer *Player) {
	p.IsDead = true
	p.IsRagdoll = true
	p.State = StateDead  // Explicit state transition
	p.RagdollTimer = 4.0 // 4 seconds ragdoll animation
	p.Deaths++
	p.Target = nil

	// Clear focus on death
	p.FocusTarget = ""
	p.FocusTTL = 0

	// Random spin direction
	p.RagdollRotation = 0
	p.VX = (rand.Float64() - 0.5) * 15
	p.VY = (rand.Float64() - 0.5) * 15
}

// UpdateRagdoll updates ragdoll physics
func (p *Player) UpdateRagdoll(deltaTime float64) {
	if !p.IsRagdoll {
		return
	}

	// Spin
	p.RagdollRotation += 0.15

	// Apply velocity with friction
	p.X += p.VX
	p.Y += p.VY
	p.VX *= 0.92
	p.VY *= 0.92

	// World bounds (use stored bounds with margin)
	margin := 40.0
	p.X = math.Max(margin, math.Min(p.worldWidth-margin, p.X))
	p.Y = math.Max(margin, math.Min(p.worldHeight-margin, p.Y))

	// Timer - ragdoll animation complete
	p.RagdollTimer -= deltaTime
	if p.RagdollTimer <= 0 {
		// CRITICAL: Do NOT auto-respawn!
		// Player stays dead until they explicitly type !join
		p.IsRagdoll = false
		p.State = StateDead
	}
}

// Respawn respawns the player (only called when player types !join)
func (p *Player) Respawn() {
	p.IsDead = false
	p.IsRagdoll = false
	p.State = StateAlive
	p.HP = p.MaxHP
	// Spawn within 80% of world bounds (10% margin on each side)
	p.X = rand.Float64()*p.worldWidth*0.8 + p.worldWidth*0.1
	p.Y = rand.Float64()*p.worldHeight*0.8 + p.worldHeight*0.1
	p.VX = 0
	p.VY = 0
	p.SpawnProtection = true
	p.SpawnTimer = 0.5 // Reduced to 0.5s for fast combat (was 3.0)
	p.Target = nil
	p.RagdollRotation = 0
	p.AttackCooldown = 0
	p.Stamina = p.MaxStamina
	p.Combat.Reset()
	p.IsDodging = false
	// Clear focus on respawn
	p.FocusTarget = ""
	p.FocusTTL = 0
}

// ResolveCollisions resolves collisions with nearby players using spatial grid
// selfIdx: index of this player in the players slice
// grid: spatial grid for O(1) neighbor queries
func (p *Player) ResolveCollisions(players []*Player, selfIdx uint32, grid *spatial.SpatialGrid) {
	const radius = 28.0
	const collisionRadius = radius * 2 // 56px - diameter for collision detection

	// Query only nearby entities (collision radius + buffer)
	candidates := grid.QueryRadius(p.X, p.Y, collisionRadius+10)

	for _, idx := range candidates {
		if idx == selfIdx {
			continue // Skip self
		}
		other := players[idx]
		if other.IsDead || other.IsRagdoll {
			continue
		}

		dx := other.X - p.X
		dy := other.Y - p.Y
		dist := math.Sqrt(dx*dx + dy*dy)
		minDist := radius * 2

		if dist < minDist && dist > 0 {
			// Collision! Push apart
			overlap := minDist - dist
			nx := dx / dist
			ny := dy / dist

			// Push both players
			pushForce := 0.6
			p.X -= nx * overlap * pushForce
			p.Y -= ny * overlap * pushForce
			other.X += nx * overlap * pushForce
			other.Y += ny * overlap * pushForce

			// Bounce velocity (reduced to prevent excessive bouncing)
			bounce := 1.0
			p.VX -= nx * bounce
			p.VY -= ny * bounce
			other.VX += nx * bounce
			other.VY += ny * bounce
		}
	}
}

func (p *Player) distanceTo(other *Player) float64 {
	dx := other.X - p.X
	dy := other.Y - p.Y
	return math.Sqrt(dx*dx + dy*dy)
}

// ToJSON returns a map representation for JSON serialization
func (p *Player) ToJSON() map[string]interface{} {
	return map[string]interface{}{
		"id":              p.ID,
		"name":            p.Name,
		"x":               p.X,
		"y":               p.Y,
		"vx":              p.VX,
		"vy":              p.VY,
		"hp":              p.HP,
		"maxHp":           p.MaxHP,
		"money":           p.Money,
		"kills":           p.Kills,
		"deaths":          p.Deaths,
		"weapon":          p.Weapon,
		"color":           p.Color,
		"avatar":          p.Avatar,
		"isAttacking":     p.IsAttacking,
		"attackAngle":     p.AttackAngle,
		"isDead":          p.IsDead,
		"isRagdoll":       p.IsRagdoll,
		"ragdollRotation": p.RagdollRotation,
		"spawnProtection": p.SpawnProtection,
		"isStunned":       p.IsStunned,
		"profilePic":      p.ProfilePic,
		"stamina":         p.Stamina,
		"isDodging":       p.IsDodging,
		"comboCount":      p.Combat.ComboCount,
	}
}
