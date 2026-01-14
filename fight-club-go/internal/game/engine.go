package game

import (
	"fmt"
	"log"
	"math"
	"math/rand"
	"sort"
	"sync"
	"time"

	"fight-club/internal/game/spatial"
)

// Engine is the main game engine handling the game loop and physics
type Engine struct {
	mu        sync.RWMutex
	players   map[string]*Player
	particles []*Particle
	effects   []*AttackEffect
	texts     []*FloatingText

	// Spatial indexing for O(1) neighbor queries (replaces O(n²) scans)
	spatialGrid *spatial.SpatialGrid
	playerSlice []*Player // Cached slice for index-based access

	// Phase 2: Sweep-and-Prune for broad-phase collision detection
	// Uses temporal coherence - nearly O(n) when entities move little
	sap *spatial.SweepAndPrune

	// Phase 3: Flow Fields for intelligent AI navigation
	// O(1) pathfinding via precomputed vector fields
	flowFieldManager *spatial.FlowFieldManager

	// New combat visual effects
	trails  []*WeaponTrail
	flashes []*ImpactFlash
	shake   *ScreenShake

	// Projectiles (arrows, thrown weapons)
	projectiles   []*Projectile
	shakeThisTick int // Rate limit shakes per tick

	// Combat configuration
	comboDefinitions map[string]ComboDefinition

	tickRate int
	running  bool
	ticker   *time.Ticker
	stopChan chan struct{}

	// Stats
	totalKills int
	tickCount  int64

	// Event callbacks
	onDamage  func(attacker, victim *Player, damage int)
	OnKill    func(killer, victim *Player)
	onJoin    func(player *Player)
	onRespawn func(player *Player)

	// World bounds
	worldWidth  float64
	worldHeight float64

	// DoS Protection: Resource limits
	limits ResourceLimits

	// Snapshot system for lock-free render separation
	snapshotPool *SnapshotPool

	// Event sourcing for replay and debugging
	eventLog *EventLog

	// Deterministic RNG for replay consistency
	rng     *rand.Rand
	rngSeed int64

	// Team management
	teamManager *TeamManager
}

// NewEngine creates a new game engine with DoS-resilient defaults
func NewEngine(tickRate int) *Engine {
	limits := DefaultLimits
	seed := time.Now().UnixNano()

	// Cell size 100px for ~500px detection range (covers 5x5 cells)
	// This balances between too many cells (memory) and too few (clustering)
	grid := spatial.NewSpatialGrid(1280, 720, 100, limits.MaxPlayers)

	return &Engine{
		players:          make(map[string]*Player),
		particles:        make([]*Particle, 0, limits.MaxParticles),
		effects:          make([]*AttackEffect, 0, limits.MaxEffects),
		texts:            make([]*FloatingText, 0, limits.MaxTexts),
		trails:           make([]*WeaponTrail, 0, limits.MaxTrails),
		flashes:          make([]*ImpactFlash, 0, limits.MaxFlashes),
		projectiles:      make([]*Projectile, 0, MaxProjectiles),
		spatialGrid:      grid,
		playerSlice:      make([]*Player, 0, limits.MaxPlayers),
		sap:              spatial.NewSweepAndPrune(limits.MaxPlayers),
		flowFieldManager: spatial.NewFlowFieldManager(1280, 720, 50), // 50px cells for smoother nav
		comboDefinitions: DefaultComboDefinitions(),
		tickRate:         tickRate,
		stopChan:         make(chan struct{}),
		worldWidth:       1280,
		worldHeight:      720,
		limits:           limits,
		snapshotPool:     NewSnapshotPool(limits),
		eventLog:         NewEventLog(),
		rng:              rand.New(rand.NewSource(seed)),
		rngSeed:          seed,
		teamManager:      NewTeamManager(),
	}
}

// Start begins the game loop
func (e *Engine) Start() {
	e.mu.Lock()
	if e.running {
		e.mu.Unlock()
		return
	}
	e.running = true
	e.mu.Unlock()

	e.ticker = time.NewTicker(time.Second / time.Duration(e.tickRate))

	go func() {
		for {
			select {
			case <-e.ticker.C:
				e.tick()
			case <-e.stopChan:
				return
			}
		}
	}()

	log.Printf("🎮 Game engine started at %d TPS", e.tickRate)
}

// Stop stops the game loop
func (e *Engine) Stop() {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.running {
		return
	}

	e.running = false
	if e.ticker != nil {
		e.ticker.Stop()
	}
	close(e.stopChan)
	log.Println("🛑 Game engine stopped")
}

// tick is called at tickRate times per second
func (e *Engine) tick() {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.tickCount++
	deltaTime := 1.0 / float64(e.tickRate)

	// Log tick event with RNG seed for deterministic replay
	e.eventLog.EmitSimple(EventTypeTick, uint64(e.tickCount), "",
		TickPayload{
			RNGSeed:     e.rngSeed,
			PlayerCount: len(e.players),
			DeltaTimeNs: int64(deltaTime * 1e9),
		})

	// Advance RNG seed deterministically for next tick
	e.rngSeed = e.rng.Int63()
	e.rng.Seed(e.rngSeed)

	// Build player list and spatial grid for O(1) neighbor queries
	// Reuse playerSlice to avoid allocation
	e.playerSlice = e.playerSlice[:0]
	for _, p := range e.players {
		e.playerSlice = append(e.playerSlice, p)
	}
	playerList := e.playerSlice

	// Rebuild spatial grid (O(n) - much faster than O(n²) scans)
	e.spatialGrid.Clear()
	for i, p := range playerList {
		if !p.IsDead && !p.IsRagdoll {
			e.spatialGrid.Insert(uint32(i), p.X, p.Y)
		}
	}

	for i, player := range playerList {
		if player.IsDead && !player.IsRagdoll {
			continue
		}

		if player.IsRagdoll {
			player.UpdateRagdoll(deltaTime)
			continue
		}

		// AI movement and combat with spatial index
		player.Update(playerList, uint32(i), e.spatialGrid, deltaTime, e)
	}

	// Resolve collisions using spatial grid
	for i, player := range playerList {
		if !player.IsDead && !player.IsRagdoll {
			player.ResolveCollisions(playerList, uint32(i), e.spatialGrid)
		}
	}

	// Update particles
	e.updateParticles()

	// Update floating texts
	e.updateFloatingTexts()

	// Update attack effects
	e.updateAttackEffects()

	// Update new combat visual effects
	e.updateTrails()
	e.updateFlashes()
	e.updateShake()
	e.updateProjectiles()
	e.shakeThisTick = 0 // Reset shake rate limiter

	// Produce immutable snapshot for lock-free render access
	e.ProduceSnapshot()
}

// AddPlayer adds a new player to the game
func (e *Engine) AddPlayer(name string, opts PlayerOptions) *Player {
	e.mu.Lock()
	defer e.mu.Unlock()

	// HARD CAP: Prevent DoS via player flooding
	if len(e.players) >= e.limits.MaxTotalPlayers {
		log.Printf("⚠️ Player limit reached (%d), rejecting: %s", e.limits.MaxTotalPlayers, name)
		return nil
	}

	// Check if player already exists
	if existing, ok := e.players[name]; ok {
		if existing.IsDead {
			existing.Respawn()
			// Log respawn event
			e.eventLog.EmitSimple(EventTypeRespawn, uint64(e.tickCount), existing.ID,
				RespawnPayload{PlayerID: existing.ID, SpawnX: existing.X, SpawnY: existing.Y})
			if e.onRespawn != nil {
				go e.onRespawn(existing)
			}
		}
		return existing
	}

	// Create new player with deterministic spawn position
	player := NewPlayer(name, opts)
	player.X = e.rng.Float64()*e.worldWidth*0.8 + e.worldWidth*0.1
	player.Y = e.rng.Float64()*e.worldHeight*0.8 + e.worldHeight*0.1

	e.players[name] = player

	// Log join event for audit trail
	e.eventLog.EmitSimple(EventTypePlayerJoin, uint64(e.tickCount), player.ID,
		PlayerJoinPayload{
			PlayerID:   player.ID,
			PlayerName: player.Name,
			SpawnX:     player.X,
			SpawnY:     player.Y,
			Color:      player.Color,
		})

	if e.onJoin != nil {
		go e.onJoin(player)
	}

	log.Printf("👤 Player joined: %s", name)
	return player
}

// RemovePlayer removes a player from the game
func (e *Engine) RemovePlayer(name string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.players, name)
}

// GetPlayer returns a player by name
func (e *Engine) GetPlayer(name string) *Player {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.players[name]
}

// HealPlayer heals a player
func (e *Engine) HealPlayer(name string, amount int) bool {
	e.mu.Lock()
	defer e.mu.Unlock()

	player, ok := e.players[name]
	if !ok || player.IsDead {
		return false
	}

	player.Heal(amount)
	return true
}

// ProcessAttack handles attack results with shaped hitbox and combo mechanics
func (e *Engine) ProcessAttack(attacker, victim *Player, damage int) {
	// Get animation config for weapon-specific behavior
	anim := GetWeaponAnimation(attacker.Weapon)

	// PROJECTILE WEAPONS: Spawn projectile instead of instant damage
	if anim.IsProjectile {
		e.CreateProjectile(attacker, victim.X, victim.Y, damage)
		return // Damage will be applied when projectile hits
	}

	// Check all invulnerability states
	if victim.IsDead || victim.SpawnProtection || victim.Combat.IsInvulnerable() {
		return
	}

	// NO FRIENDLY FIRE: Teammates cannot damage each other
	if attacker.TeamID != "" && attacker.TeamID == victim.TeamID {
		return
	}

	// Validate shaped hitbox collision
	hitbox := GetHitbox(attacker.Weapon)
	if !hitbox.CheckHit(attacker.X, attacker.Y, victim.X, victim.Y, attacker.AttackAngle) {
		return // Missed - attack didn't connect
	}

	// Apply combo damage multiplier
	combo, ok := e.comboDefinitions[attacker.Weapon]
	if !ok {
		combo = ComboDefinition{MaxHits: 1, DamageScale: []float64{1.0}}
	}
	comboMultiplier := attacker.Combat.RegisterHit(uint64(e.tickCount), combo)
	damage = int(float64(damage) * comboMultiplier)

	// Log the attack for debugging
	log.Printf("⚔️ %s attacks %s for %d damage (HP: %d -> %d) [combo x%.1f]",
		attacker.Name, victim.Name, damage, victim.HP, victim.HP-damage, comboMultiplier)

	victim.TakeDamage(damage, attacker)

	// Log damage event for audit trail
	e.eventLog.EmitSimple(EventTypeDamage, uint64(e.tickCount), attacker.ID,
		DamagePayload{
			AttackerID: attacker.ID,
			VictimID:   victim.ID,
			Damage:     damage,
			VictimHP:   victim.HP,
			WeaponID:   attacker.Weapon,
		})

	if e.onDamage != nil {
		go e.onDamage(attacker, victim, damage)
	}

	// Create particles (use weapon-specific count from animation config)
	particleCount := anim.ParticleCount
	if particleCount < 2 {
		particleCount = 2 // Minimum particles
	}
	for i := 0; i < particleCount; i++ {
		e.createParticle(victim.X, victim.Y, "#ff0000")
	}

	// Create arc swing effect (now created here only when hit connects)
	weapon := GetWeapon(attacker.Weapon)
	if len(e.effects) < e.limits.MaxEffects {
		e.effects = append(e.effects, &AttackEffect{
			X:     attacker.X,
			Y:     attacker.Y,
			TX:    victim.X,
			TY:    victim.Y,
			Color: weapon.Color,
			Timer: 6, // Short duration: 0.2 seconds at 30 TPS
		})
	}

	// Create impact flash effect (use weapon-specific radius)
	e.CreateFlash(victim.X, victim.Y, weapon.Color, comboMultiplier)

	// Screen shake using weapon-specific intensity from animation config
	e.AddShake(anim.ShakeIntensity)

	// Create damage text with cap
	if len(e.texts) < e.limits.MaxTexts {
		comboText := ""
		if attacker.Combat.ComboCount > 1 {
			comboText = fmt.Sprintf(" (x%d)", attacker.Combat.ComboCount)
		}
		e.texts = append(e.texts, &FloatingText{
			X:     victim.X,
			Y:     victim.Y - 30,
			Text:  fmt.Sprintf("-%d%s", damage, comboText),
			Color: "#ff3e3e",
			Alpha: 1.0,
			VY:    -2,
		})
	}

	if victim.IsDead {
		e.totalKills++
		attacker.Kills++
		attacker.Money += 50

		// Track team kills for leaderboard
		if attacker.TeamID != "" {
			e.teamManager.AddKill(attacker.TeamID)
		}

		log.Printf("💀 %s killed by %s! (Kills: %d)", victim.Name, attacker.Name, attacker.Kills)

		// Log kill event for audit trail
		e.eventLog.EmitSimple(EventTypeKill, uint64(e.tickCount), attacker.ID,
			KillPayload{
				KillerID:     attacker.ID,
				VictimID:     victim.ID,
				KillerKills:  attacker.Kills,
				VictimDeaths: victim.Deaths,
			})

		if e.OnKill != nil {
			go e.OnKill(attacker, victim)
		}

		// Death particles (already capped in createParticle)
		for i := 0; i < 20; i++ {
			e.createParticle(victim.X, victim.Y, victim.Color)
		}

		// Extra shake for kills
		e.AddShake(6.0)
	}
}

func (e *Engine) createParticle(x, y float64, color string) {
	// HARD CAP: Prevent DoS via particle flooding
	if len(e.particles) >= e.limits.MaxParticles {
		return // Silently drop - this is intentional under attack
	}

	// Use deterministic RNG for replay consistency
	angle := e.rng.Float64() * math.Pi * 2
	speed := e.rng.Float64()*3 + 1

	e.particles = append(e.particles, &Particle{
		X:     x,
		Y:     y,
		VX:    math.Cos(angle) * speed,
		VY:    math.Sin(angle) * speed,
		Color: color,
		Alpha: 1.0,
		Life:  1.0,
	})
}

func (e *Engine) updateParticles() {
	// REAL-TIME FIX: Zero-allocation in-place filtering
	// Avoids creating new slice each tick, reduces GC pressure
	n := 0
	for _, p := range e.particles {
		p.X += p.VX
		p.Y += p.VY
		p.Life -= 0.02
		p.Alpha = p.Life

		if p.Life > 0 {
			e.particles[n] = p
			n++
		}
	}
	e.particles = e.particles[:n]
}

func (e *Engine) updateFloatingTexts() {
	// REAL-TIME FIX: Zero-allocation in-place filtering
	n := 0
	for _, t := range e.texts {
		t.Y += t.VY
		t.Alpha -= 0.02

		if t.Alpha > 0 {
			e.texts[n] = t
			n++
		}
	}
	e.texts = e.texts[:n]
}

func (e *Engine) updateAttackEffects() {
	// REAL-TIME FIX: Zero-allocation in-place filtering
	n := 0
	for _, ef := range e.effects {
		ef.Timer--
		if ef.Timer > 0 {
			e.effects[n] = ef
			n++
		}
	}
	e.effects = e.effects[:n]
}

// updateTrails updates all weapon trails (zero-allocation in-place filtering)
func (e *Engine) updateTrails() {
	n := 0
	for _, tr := range e.trails {
		if tr.Update() {
			e.trails[n] = tr
			n++
		}
	}
	e.trails = e.trails[:n]
}

// updateFlashes updates all impact flashes (zero-allocation in-place filtering)
func (e *Engine) updateFlashes() {
	n := 0
	for _, fl := range e.flashes {
		if fl.Update() {
			e.flashes[n] = fl
			n++
		}
	}
	e.flashes = e.flashes[:n]
}

// updateShake updates screen shake state
func (e *Engine) updateShake() {
	if e.shake != nil {
		if !e.shake.Update(e.rngSeed) {
			e.shake = nil
		}
	}
}

// updateProjectiles moves projectiles and checks for collisions
// Projectiles that hit a target or expire are removed
func (e *Engine) updateProjectiles() {
	deltaTime := 1.0 / float64(e.tickRate)
	playerList := e.playerSlice

	n := 0
	for _, proj := range e.projectiles {
		// Check for collision with any player
		hit := false
		for _, target := range playerList {
			if proj.CheckHit(target) {
				// Apply damage and effects
				e.processProjectileHit(proj, target)
				hit = true
				break
			}
		}

		// Remove if hit something or expired/out of bounds
		if hit || !proj.Update(deltaTime) {
			continue // Don't keep this projectile
		}

		e.projectiles[n] = proj
		n++
	}
	e.projectiles = e.projectiles[:n]
}

// processProjectileHit handles when a projectile hits a player
func (e *Engine) processProjectileHit(proj *Projectile, victim *Player) {
	// Find the attacker for kill credit
	var attacker *Player
	for _, p := range e.players {
		if p.ID == proj.OwnerID {
			attacker = p
			break
		}
	}

	if attacker == nil {
		return // Attacker disconnected, no damage
	}

	// NO FRIENDLY FIRE: Check teams
	if attacker.TeamID != "" && attacker.TeamID == victim.TeamID {
		return
	}

	// Get animation config for weapon-specific effects
	anim := GetWeaponAnimation(attacker.Weapon)

	// Apply damage
	victim.TakeDamage(proj.Damage, attacker)

	// Create impact effects
	e.CreateFlash(victim.X, victim.Y, proj.Color, 1.5)
	e.AddShake(anim.ShakeIntensity)

	// Create particles
	for i := 0; i < anim.ParticleCount; i++ {
		e.createParticle(victim.X, victim.Y, proj.Color)
	}

	// Create damage text
	if len(e.texts) < e.limits.MaxTexts {
		e.texts = append(e.texts, &FloatingText{
			X:     victim.X,
			Y:     victim.Y - 30,
			Text:  fmt.Sprintf("-%d", proj.Damage),
			Color: "#ff3e3e",
			Alpha: 1.0,
			VY:    -2,
		})
	}

	// Log damage event
	e.eventLog.EmitSimple(EventTypeDamage, uint64(e.tickCount), attacker.ID,
		DamagePayload{
			AttackerID: attacker.ID,
			VictimID:   victim.ID,
			Damage:     proj.Damage,
			VictimHP:   victim.HP,
			WeaponID:   attacker.Weapon,
		})

	if e.onDamage != nil {
		go e.onDamage(attacker, victim, proj.Damage)
	}

	// Handle kill
	if victim.IsDead {
		e.totalKills++
		attacker.Kills++
		attacker.Money += 50

		if attacker.TeamID != "" {
			e.teamManager.AddKill(attacker.TeamID)
		}

		log.Printf("🏹💀 %s killed by %s's arrow! (Kills: %d)", victim.Name, attacker.Name, attacker.Kills)

		e.eventLog.EmitSimple(EventTypeKill, uint64(e.tickCount), attacker.ID,
			KillPayload{
				KillerID:     attacker.ID,
				VictimID:     victim.ID,
				KillerKills:  attacker.Kills,
				VictimDeaths: victim.Deaths,
			})

		if e.OnKill != nil {
			go e.OnKill(attacker, victim)
		}

		// Death particles
		for i := 0; i < 20; i++ {
			e.createParticle(victim.X, victim.Y, victim.Color)
		}

		e.AddShake(6.0)
	}
}

// CreateProjectile spawns a new projectile (arrow) from a player
func (e *Engine) CreateProjectile(owner *Player, targetX, targetY float64, damage int) {
	if len(e.projectiles) >= MaxProjectiles {
		return // DoS protection
	}

	proj := NewProjectile(owner, targetX, targetY, damage, e.tickCount)
	e.projectiles = append(e.projectiles, proj)

	log.Printf("🏹 %s fires arrow toward (%.0f, %.0f)", owner.Name, targetX, targetY)
}

// CreateTrail creates a new weapon trail effect with rate limiting
func (e *Engine) CreateTrail(startX, startY float64, color, playerID string) {
	if len(e.trails) >= e.limits.MaxTrails {
		return // DoS protection
	}
	e.trails = append(e.trails, NewWeaponTrail(startX, startY, color, playerID))
}

// CreateFlash creates an impact flash effect with rate limiting
func (e *Engine) CreateFlash(x, y float64, color string, intensity float64) {
	if len(e.flashes) >= e.limits.MaxFlashes {
		return // DoS protection
	}
	e.flashes = append(e.flashes, NewImpactFlash(x, y, color, intensity))
}

// AddShake adds screen shake with rate limiting
func (e *Engine) AddShake(intensity float64) {
	if e.shakeThisTick >= MaxShakePerTick {
		return // Rate limited
	}
	e.shakeThisTick++

	if e.shake == nil {
		e.shake = NewScreenShake(intensity)
	} else {
		// Combine shakes (don't exceed max)
		e.shake.Intensity += intensity * 0.5
		if e.shake.Intensity > MaxShakeIntensity {
			e.shake.Intensity = MaxShakeIntensity
		}
		e.shake.Duration = 8 // Reset duration
	}
}

// GetState returns the current game state for rendering
func (e *Engine) GetState() GameState {
	e.mu.RLock()
	defer e.mu.RUnlock()

	players := make([]*Player, 0, len(e.players))
	aliveCount := 0
	for _, p := range e.players {
		players = append(players, p)
		if !p.IsDead {
			aliveCount++
		}
	}

	// STABLE SORT by kills (descending), then by name for consistency
	sort.SliceStable(players, func(i, j int) bool {
		if players[i].Kills != players[j].Kills {
			return players[i].Kills > players[j].Kills
		}
		return players[i].Name < players[j].Name
	})

	return GameState{
		Players:     players,
		Particles:   e.particles,
		Effects:     e.effects,
		Texts:       e.texts,
		PlayerCount: len(players),
		AliveCount:  aliveCount,
		TotalKills:  e.totalKills,
	}
}

// SetCallbacks sets event callbacks
func (e *Engine) SetCallbacks(onDamage func(*Player, *Player, int), OnKill func(*Player, *Player), onJoin, onRespawn func(*Player)) {
	e.onDamage = onDamage
	e.OnKill = OnKill
	e.onJoin = onJoin
	e.onRespawn = onRespawn
}

// GameState represents the current state for rendering
type GameState struct {
	Players     []*Player
	Particles   []*Particle
	Effects     []*AttackEffect
	Texts       []*FloatingText
	PlayerCount int
	AliveCount  int
	TotalKills  int
}

// Particle represents a visual particle
type Particle struct {
	X, Y   float64
	VX, VY float64
	Color  string
	Alpha  float64
	Life   float64
}

// AttackEffect represents an attack visual effect
type AttackEffect struct {
	X, Y   float64
	TX, TY float64
	Color  string
	Timer  int
}

// FloatingText represents floating damage numbers
type FloatingText struct {
	X, Y  float64
	VY    float64
	Text  string
	Color string
	Alpha float64
}

// GetSnapshot returns the latest immutable snapshot for lock-free rendering
// This is the preferred method for the render loop
func (e *Engine) GetSnapshot() *GameSnapshot {
	return e.snapshotPool.AcquireRead()
}

// ProduceSnapshot creates an immutable snapshot of the current game state
// Called at the end of each tick
func (e *Engine) ProduceSnapshot() {
	snap := e.snapshotPool.AcquireWrite()
	snap.TickNumber = uint64(e.tickCount)
	snap.RNGSeed = e.rngSeed
	snap.TotalKills = e.totalKills

	// Copy players to snapshot (value types, immutable)
	// Sort players by priority for rendering (Alive > Kills > Name)
	// We do this BEFORE appending to snapshot to ensure we keep the "best" players if we hit the limit
	// Create a temporary slice of pointers for sorting
	playerPtrs := make([]*Player, 0, len(e.players))
	for _, p := range e.players {
		playerPtrs = append(playerPtrs, p)
	}

	sort.Slice(playerPtrs, func(i, j int) bool {
		// Priority 1: Alive players first
		if !playerPtrs[i].IsDead && playerPtrs[j].IsDead {
			return true
		}
		if playerPtrs[i].IsDead && !playerPtrs[j].IsDead {
			return false
		}
		// Priority 2: Higher kills
		if playerPtrs[i].Kills != playerPtrs[j].Kills {
			return playerPtrs[i].Kills > playerPtrs[j].Kills
		}
		// Priority 3: Name (deterministic tie-break)
		return playerPtrs[i].Name < playerPtrs[j].Name
	})

	// Copy sorted players to snapshot (up to MaxPlayers)
	aliveCount := 0
	for _, p := range playerPtrs {
		if len(snap.Players) >= e.limits.MaxPlayers {
			if !p.IsDead {
				aliveCount++ // Still count alive players even if not rendered (for stats)
			}
			continue
		}
		snap.Players = append(snap.Players, PlayerSnapshot{
			ID:              p.ID,
			Name:            p.Name,
			X:               p.X,
			Y:               p.Y,
			VX:              p.VX,
			VY:              p.VY,
			HP:              p.HP,
			MaxHP:           p.MaxHP,
			Money:           p.Money,
			Kills:           p.Kills,
			Deaths:          p.Deaths,
			Weapon:          p.Weapon,
			Color:           p.Color,
			Avatar:          p.Avatar,
			AttackAngle:     p.AttackAngle,
			IsDead:          p.IsDead,
			IsRagdoll:       p.IsRagdoll,
			RagdollRotation: p.RagdollRotation,
			SpawnProtection: p.SpawnProtection,
			IsAttacking:     p.IsAttacking,
			ProfilePic:      p.ProfilePic,
			IsDodging:       p.IsDodging,
			DodgeDirection:  p.Combat.DodgeDirection,
			ComboCount:      p.Combat.ComboCount,
			Stamina:         p.Stamina,
		})
		if !p.IsDead {
			aliveCount++
		}
	}

	// DONE: Players are already sorted and culled above

	// Copy particles to snapshot
	for _, p := range e.particles {
		if len(snap.Particles) >= e.limits.MaxParticles {
			break
		}
		snap.Particles = append(snap.Particles, ParticleSnapshot{
			X:     p.X,
			Y:     p.Y,
			Color: p.Color,
			Alpha: p.Alpha,
		})
	}

	// Copy effects to snapshot
	for _, ef := range e.effects {
		if len(snap.Effects) >= e.limits.MaxEffects {
			break
		}
		snap.Effects = append(snap.Effects, EffectSnapshot{
			X:     ef.X,
			Y:     ef.Y,
			TX:    ef.TX,
			TY:    ef.TY,
			Color: ef.Color,
			Timer: ef.Timer,
		})
	}

	// Copy texts to snapshot
	for _, t := range e.texts {
		if len(snap.Texts) >= e.limits.MaxTexts {
			break
		}
		snap.Texts = append(snap.Texts, TextSnapshot{
			X:     t.X,
			Y:     t.Y,
			Text:  t.Text,
			Color: t.Color,
			Alpha: t.Alpha,
		})
	}

	// Copy weapon trails to snapshot
	for _, tr := range e.trails {
		if len(snap.Trails) >= e.limits.MaxTrails {
			break
		}
		trailSnap := TrailSnapshot{
			Count:    tr.PointCount,
			Color:    tr.Color,
			Alpha:    float64(tr.Timer) / 15.0,
			PlayerID: tr.PlayerID,
		}
		// Copy trail points
		points := tr.GetPoints()
		for i, pt := range points {
			if i >= 8 {
				break
			}
			trailSnap.Points[i] = TrailPointSnapshot{
				X:     pt.X,
				Y:     pt.Y,
				Alpha: pt.Alpha,
			}
		}
		snap.Trails = append(snap.Trails, trailSnap)
	}

	// Copy impact flashes to snapshot
	for _, fl := range e.flashes {
		if len(snap.Flashes) >= e.limits.MaxFlashes {
			break
		}
		snap.Flashes = append(snap.Flashes, FlashSnapshot{
			X:         fl.X,
			Y:         fl.Y,
			Radius:    fl.Radius,
			Color:     fl.Color,
			Intensity: fl.GetAlpha(),
		})
	}

	// Copy projectiles to snapshot (arrows, thrown weapons)
	for _, proj := range e.projectiles {
		if len(snap.Projectiles) >= MaxProjectiles {
			break
		}
		snap.Projectiles = append(snap.Projectiles, proj.ToSnapshot())
	}

	// Copy screen shake
	if e.shake != nil && e.shake.Intensity > 0.5 {
		snap.Shake = ShakeSnapshot{
			OffsetX:   e.shake.OffsetX,
			OffsetY:   e.shake.OffsetY,
			Intensity: e.shake.Intensity,
		}
	}

	snap.PlayerCount = len(snap.Players)
	snap.AliveCount = aliveCount

	e.snapshotPool.PublishWrite()
}

// StartEventLog initializes the event logging system
func (e *Engine) StartEventLog(filePath string) error {
	return e.eventLog.Start(filePath)
}

// StopEventLog gracefully stops the event logging system
func (e *Engine) StopEventLog() {
	e.eventLog.Stop()
}

// GetEventLogStats returns event log statistics for monitoring
func (e *Engine) GetEventLogStats() map[string]interface{} {
	return e.eventLog.GetStats()
}

// GetLimits returns the current resource limits
func (e *Engine) GetLimits() ResourceLimits {
	return e.limits
}

// GetSpatialGrid returns the spatial grid for testing and external queries
func (e *Engine) GetSpatialGrid() *spatial.SpatialGrid {
	return e.spatialGrid
}

// GetTeamManager returns the team manager for team operations
func (e *Engine) GetTeamManager() *TeamManager {
	return e.teamManager
}

// GetFlowFieldManager returns the flow field manager for AI navigation
func (e *Engine) GetFlowFieldManager() *spatial.FlowFieldManager {
	return e.flowFieldManager
}

// SetFocus sets a player's focus target
func (e *Engine) SetFocus(playerName, targetName string, duration float64) bool {
	e.mu.Lock()
	defer e.mu.Unlock()

	player, ok := e.players[playerName]
	if !ok || player.IsDead {
		return false
	}

	// Validate target exists and is valid
	target, ok := e.players[targetName]
	if !ok || target.IsDead {
		return false
	}

	// Can't focus teammates
	if player.TeamID != "" && player.TeamID == target.TeamID {
		return false
	}

	player.FocusTarget = targetName
	player.FocusTTL = duration
	return true
}

// ClearFocus clears a player's focus target
func (e *Engine) ClearFocus(playerName string) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if player, ok := e.players[playerName]; ok {
		player.FocusTarget = ""
		player.FocusTTL = 0
	}
}

// SetChatBubble sets a player's chat bubble message
func (e *Engine) SetChatBubble(playerName, message string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()

	player, ok := e.players[playerName]
	if !ok || player.IsDead || player.State != StateAlive {
		return false // Only show bubbles for alive, joined players
	}

	// Truncate to 50 chars
	if len(message) > 50 {
		message = message[:50]
	}

	player.ChatBubble = message
	player.ChatBubbleTTL = 5.0 // 5 seconds TTL
	return true
}

// SetPlayerTeam updates a player's team ID
func (e *Engine) SetPlayerTeam(playerName, teamID string) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if player, ok := e.players[playerName]; ok {
		player.TeamID = teamID
	}
}
