package game

import (
	"testing"

	"fight-club/internal/config"
)

// TestNewPlayer tests player creation with defaults
func TestNewPlayer(t *testing.T) {
	player := NewPlayer("TestPlayer", PlayerOptions{})

	if player == nil {
		t.Fatal("NewPlayer returned nil")
	}
	if player.Name != "TestPlayer" {
		t.Errorf("Expected name 'TestPlayer', got '%s'", player.Name)
	}
	if player.HP != 100 {
		t.Errorf("Expected HP 100, got %d", player.HP)
	}
	if player.MaxHP != 100 {
		t.Errorf("Expected MaxHP 100, got %d", player.MaxHP)
	}
	if player.Weapon != "fists" {
		t.Errorf("Expected weapon 'fists', got '%s'", player.Weapon)
	}
	if player.Money != 0 {
		t.Errorf("Expected money 0, got %d", player.Money)
	}
	if !player.SpawnProtection {
		t.Error("New player should have spawn protection")
	}
}

// TestNewPlayerWithOptions tests player creation with custom options
func TestNewPlayerWithOptions(t *testing.T) {
	opts := PlayerOptions{
		ProfilePic: "http://example.com/pic.png",
		Color:      "#ff0000",
	}
	player := NewPlayer("CustomPlayer", opts)

	if player.Color != "#ff0000" {
		t.Errorf("Expected color '#ff0000', got '%s'", player.Color)
	}
	if player.ProfilePic != "http://example.com/pic.png" {
		t.Errorf("Expected profilePic to be set")
	}
}

// TestPlayerTakeDamage tests damage application
func TestPlayerTakeDamage(t *testing.T) {
	player := NewPlayer("TestPlayer", PlayerOptions{})
	player.SpawnProtection = false

	attacker := NewPlayer("Attacker", PlayerOptions{})

	initialHP := player.HP
	player.TakeDamage(30, attacker)

	if player.HP != initialHP-30 {
		t.Errorf("Expected HP %d, got %d", initialHP-30, player.HP)
	}
}

// TestPlayerTakeDamageWithProtection tests spawn protection
func TestPlayerTakeDamageWithProtection(t *testing.T) {
	player := NewPlayer("TestPlayer", PlayerOptions{})
	player.SpawnProtection = true

	initialHP := player.HP
	player.TakeDamage(50, nil)

	if player.HP != initialHP {
		t.Error("Player with spawn protection should not take damage")
	}
}

// TestPlayerDeath tests player death
func TestPlayerDeath(t *testing.T) {
	player := NewPlayer("TestPlayer", PlayerOptions{})
	player.SpawnProtection = false
	player.HP = 10

	attacker := NewPlayer("Attacker", PlayerOptions{})
	attacker.SpawnProtection = false

	player.TakeDamage(20, attacker)

	if !player.IsDead {
		t.Error("Player should be dead after taking fatal damage")
	}
	if !player.IsRagdoll {
		t.Error("Dead player should be in ragdoll state")
	}
	if player.Deaths != 1 {
		t.Errorf("Expected 1 death, got %d", player.Deaths)
	}
}

// TestPlayerHeal tests healing
func TestPlayerHeal(t *testing.T) {
	player := NewPlayer("TestPlayer", PlayerOptions{})
	player.HP = 50

	player.Heal(30)

	if player.HP != 80 {
		t.Errorf("Expected HP 80, got %d", player.HP)
	}
}

// TestPlayerHealCap tests healing doesn't exceed max
func TestPlayerHealCap(t *testing.T) {
	player := NewPlayer("TestPlayer", PlayerOptions{})
	player.HP = 90

	player.Heal(50)

	if player.HP > player.MaxHP {
		t.Errorf("HP should not exceed MaxHP, got %d", player.HP)
	}
}

// TestPlayerRespawn tests respawning
func TestPlayerRespawn(t *testing.T) {
	player := NewPlayer("TestPlayer", PlayerOptions{})
	player.SpawnProtection = false
	player.HP = 1
	player.TakeDamage(10, nil)

	if !player.IsDead {
		t.Fatal("Player should be dead")
	}

	player.Respawn()

	if player.IsDead {
		t.Error("Player should not be dead after respawn")
	}
	if player.IsRagdoll {
		t.Error("Player should not be ragdoll after respawn")
	}
	if player.HP != player.MaxHP {
		t.Error("Player should have full HP after respawn")
	}
	if !player.SpawnProtection {
		t.Error("Player should have spawn protection after respawn")
	}
}

// TestPlayerToJSON tests JSON serialization
func TestPlayerToJSON(t *testing.T) {
	player := NewPlayer("TestPlayer", PlayerOptions{})
	player.Kills = 5
	player.Deaths = 2
	player.Money = 150

	json := player.ToJSON()

	if json["name"] != "TestPlayer" {
		t.Error("JSON should contain correct name")
	}
	if json["kills"] != 5 {
		t.Error("JSON should contain correct kills")
	}
	if json["deaths"] != 2 {
		t.Error("JSON should contain correct deaths")
	}
	if json["money"] != 150 {
		t.Error("JSON should contain correct money")
	}
}

// TestPlayerUpdate tests player update with no target
func TestPlayerUpdate(t *testing.T) {
	engine := NewEngine(EngineConfig{
		TickRate:    30,
		WorldWidth:  1280,
		WorldHeight: 720,
		Limits:      config.DefaultLimits(),
	})
	player := NewPlayer("TestPlayer", PlayerOptions{})
	player.SpawnProtection = false

	players := []*Player{player}

	// Create spatial grid for the test
	grid := engine.GetSpatialGrid()
	grid.Clear()
	grid.Insert(0, player.X, player.Y)

	// Update should not panic with empty player list
	player.Update(players, 0, grid, 0.033, engine)
}

// TestPlayerUpdateRagdoll tests ragdoll physics
func TestPlayerUpdateRagdoll(t *testing.T) {
	player := NewPlayer("TestPlayer", PlayerOptions{})
	player.SpawnProtection = false
	player.HP = 1
	player.TakeDamage(10, nil)

	if !player.IsRagdoll {
		t.Fatal("Player should be ragdoll")
	}

	initialX := player.X
	player.VX = 5

	player.UpdateRagdoll(0.033)

	if player.X <= initialX {
		t.Error("Ragdoll should move based on velocity")
	}
}
