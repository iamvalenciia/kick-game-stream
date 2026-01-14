package game_test

import (
	"testing"

	"fight-club/internal/game"
)

// TestNewProjectile verifies projectile creation
func TestNewProjectile(t *testing.T) {
	engine := game.NewEngine(30)
	owner := engine.AddPlayer("Archer", game.PlayerOptions{})
	owner.Weapon = "bow"
	owner.X = 100
	owner.Y = 100

	targetX := 200.0
	targetY := 100.0

	proj := game.NewProjectile(owner, targetX, targetY, 30, 1)

	if proj == nil {
		t.Fatal("NewProjectile returned nil")
	}

	if proj.OwnerID != owner.ID {
		t.Errorf("OwnerID mismatch: got %s, want %s", proj.OwnerID, owner.ID)
	}

	if proj.Damage != 30 {
		t.Errorf("Damage mismatch: got %d, want 30", proj.Damage)
	}

	if proj.Timer != game.ProjectileLifetime {
		t.Errorf("Timer should be %d, got %d", game.ProjectileLifetime, proj.Timer)
	}

	// Projectile should start at player's edge, not center
	expectedStartX := owner.X + 40 // 40 pixels toward target
	if proj.X < owner.X || proj.X > expectedStartX+5 {
		t.Errorf("Projectile X should start near player edge, got %f", proj.X)
	}

	// Velocity should be in direction of target (positive X in this case)
	if proj.VX <= 0 {
		t.Errorf("Projectile VX should be positive when firing right, got %f", proj.VX)
	}
}

// TestProjectileUpdate verifies projectile movement
func TestProjectileUpdate(t *testing.T) {
	engine := game.NewEngine(30)
	owner := engine.AddPlayer("Archer", game.PlayerOptions{})
	owner.Weapon = "bow"
	owner.X = 100
	owner.Y = 100

	proj := game.NewProjectile(owner, 200, 100, 30, 1)
	startX := proj.X
	startTimer := proj.Timer

	// Update should move projectile
	alive := proj.Update(0.05)

	if !alive {
		t.Error("Projectile should still be alive after one update")
	}

	if proj.X <= startX {
		t.Errorf("Projectile should have moved right, started at %f, now at %f", startX, proj.X)
	}

	if proj.Timer >= startTimer {
		t.Error("Projectile timer should have decreased")
	}
}

// TestProjectileExpiration verifies projectiles expire after lifetime
func TestProjectileExpiration(t *testing.T) {
	engine := game.NewEngine(30)
	owner := engine.AddPlayer("Archer", game.PlayerOptions{})
	owner.Weapon = "bow"
	owner.X = 100
	owner.Y = 100

	proj := game.NewProjectile(owner, 200, 100, 30, 1)
	proj.Timer = 1 // About to expire

	alive := proj.Update(0.05)

	if alive {
		t.Error("Projectile should be dead after timer reaches 0")
	}
}

// TestProjectileOutOfBounds verifies projectiles die when leaving arena
func TestProjectileOutOfBounds(t *testing.T) {
	engine := game.NewEngine(30)
	owner := engine.AddPlayer("Archer", game.PlayerOptions{})
	owner.Weapon = "bow"
	owner.X = 100
	owner.Y = 100

	proj := game.NewProjectile(owner, 200, 100, 30, 1)
	proj.X = 2000 // Out of bounds (arena is 1920 wide)

	alive := proj.Update(0.05)

	if alive {
		t.Error("Projectile should be dead when out of bounds")
	}
}

// TestProjectileCheckHit verifies collision detection
func TestProjectileCheckHit(t *testing.T) {
	engine := game.NewEngine(30)
	owner := engine.AddPlayer("Archer", game.PlayerOptions{})
	owner.Weapon = "bow"
	owner.X = 100
	owner.Y = 100

	target := engine.AddPlayer("Target", game.PlayerOptions{})
	target.X = 150
	target.Y = 100

	// Ensure protection is off
	target.SpawnProtection = false

	proj := game.NewProjectile(owner, 200, 100, 30, 1)
	proj.X = 145 // Close to target (Collision dist is ~36px: 8 proj + 28 player)

	// Should hit target
	if !proj.CheckHit(target) {
		t.Error("Projectile should hit target when close enough")
	}

	// Move projectile far away
	proj.X = 500

	if proj.CheckHit(target) {
		t.Error("Projectile should not hit target when far away")
	}
}

// TestProjectileDoesNotHitOwner verifies self-collision is prevented
func TestProjectileDoesNotHitOwner(t *testing.T) {
	engine := game.NewEngine(30)
	owner := engine.AddPlayer("Archer", game.PlayerOptions{})
	owner.Weapon = "bow"
	owner.X = 100
	owner.Y = 100

	proj := game.NewProjectile(owner, 200, 100, 30, 1)
	proj.X = owner.X + 5 // Very close to owner

	// Should NOT hit owner
	if proj.CheckHit(owner) {
		t.Error("Projectile should never hit its owner")
	}
}

// TestProjectileDoesNotHitDeadPlayers verifies dead players are not hit
func TestProjectileDoesNotHitDeadPlayers(t *testing.T) {
	engine := game.NewEngine(30)
	owner := engine.AddPlayer("Archer", game.PlayerOptions{})
	owner.Weapon = "bow"
	owner.X = 100
	owner.Y = 100

	target := engine.AddPlayer("DeadTarget", game.PlayerOptions{})
	target.X = 105
	target.Y = 100
	target.IsDead = true

	proj := game.NewProjectile(owner, 200, 100, 30, 1)
	proj.X = target.X

	if proj.CheckHit(target) {
		t.Error("Projectile should not hit dead players")
	}
}

// TestProjectileDoesNotHitSpawnProtectedPlayers verifies spawn protection works
func TestProjectileDoesNotHitSpawnProtectedPlayers(t *testing.T) {
	engine := game.NewEngine(30)
	owner := engine.AddPlayer("Archer", game.PlayerOptions{})
	owner.Weapon = "bow"
	owner.X = 100
	owner.Y = 100

	target := engine.AddPlayer("SpawnProtected", game.PlayerOptions{})
	target.X = 105
	target.Y = 100
	target.SpawnProtection = true

	proj := game.NewProjectile(owner, 200, 100, 30, 1)
	proj.X = target.X

	if proj.CheckHit(target) {
		t.Error("Projectile should not hit spawn-protected players")
	}
}

// TestProjectileToSnapshot verifies snapshot creation
func TestProjectileToSnapshot(t *testing.T) {
	engine := game.NewEngine(30)
	owner := engine.AddPlayer("Archer", game.PlayerOptions{})
	owner.Weapon = "bow"
	owner.X = 100
	owner.Y = 100

	proj := game.NewProjectile(owner, 200, 100, 30, 1)
	// Add some trail points
	proj.Update(0.05)
	proj.Update(0.05)

	snap := proj.ToSnapshot()

	if snap.X != proj.X {
		t.Errorf("Snapshot X mismatch: got %f, want %f", snap.X, proj.X)
	}

	if snap.Rotation != proj.Rotation {
		t.Errorf("Snapshot Rotation mismatch: got %f, want %f", snap.Rotation, proj.Rotation)
	}

	if snap.Color != proj.Color {
		t.Errorf("Snapshot Color mismatch: got %s, want %s", snap.Color, proj.Color)
	}
}
