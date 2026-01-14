package game

import (
	"testing"
	"time"
)

// TestNewEngine verifies engine creation with correct defaults
func TestNewEngine(t *testing.T) {
	tests := []struct {
		name     string
		tickRate int
	}{
		{"standard 30 TPS", 30},
		{"high 60 TPS", 60},
		{"low 15 TPS", 15},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine := NewEngine(tt.tickRate)
			if engine == nil {
				t.Fatal("NewEngine returned nil")
			}
		})
	}
}

// TestEngineStartStop verifies engine can start and stop without panics
func TestEngineStartStop(t *testing.T) {
	engine := NewEngine(30)

	// Start engine
	engine.Start()
	time.Sleep(100 * time.Millisecond)

	// Stop engine
	engine.Stop()

	// Should not panic on double stop
	engine.Stop()
}

// TestAddPlayer tests adding players to the engine
func TestAddPlayer(t *testing.T) {
	engine := NewEngine(30)

	// Add first player
	player1 := engine.AddPlayer("Player1", PlayerOptions{})
	if player1 == nil {
		t.Fatal("AddPlayer returned nil")
	}
	if player1.Name != "Player1" {
		t.Errorf("Expected name 'Player1', got '%s'", player1.Name)
	}
	if player1.HP != 100 {
		t.Errorf("Expected HP 100, got %d", player1.HP)
	}

	// Add second player
	player2 := engine.AddPlayer("Player2", PlayerOptions{Color: "#ff0000"})
	if player2 == nil {
		t.Fatal("AddPlayer returned nil for second player")
	}
	if player2.Color != "#ff0000" {
		t.Errorf("Expected color '#ff0000', got '%s'", player2.Color)
	}

	// Adding same player should return existing
	player1Again := engine.AddPlayer("Player1", PlayerOptions{})
	if player1Again != player1 {
		t.Error("Adding same player should return existing player")
	}
}

// TestRemovePlayer tests player removal
func TestRemovePlayer(t *testing.T) {
	engine := NewEngine(30)

	engine.AddPlayer("TestPlayer", PlayerOptions{})

	// Verify player exists
	if engine.GetPlayer("TestPlayer") == nil {
		t.Fatal("Player should exist after adding")
	}

	// Remove player
	engine.RemovePlayer("TestPlayer")

	// Verify player is gone
	if engine.GetPlayer("TestPlayer") != nil {
		t.Error("Player should not exist after removal")
	}

	// Removing non-existent player should not panic
	engine.RemovePlayer("NonExistent")
}

// TestGetPlayer tests player retrieval
func TestGetPlayer(t *testing.T) {
	engine := NewEngine(30)

	// Non-existent player
	if engine.GetPlayer("Nobody") != nil {
		t.Error("GetPlayer should return nil for non-existent player")
	}

	// Add and retrieve
	engine.AddPlayer("Existing", PlayerOptions{})
	player := engine.GetPlayer("Existing")
	if player == nil {
		t.Error("GetPlayer should return player after adding")
	}
}

// TestHealPlayer tests player healing
func TestHealPlayer(t *testing.T) {
	engine := NewEngine(30)

	player := engine.AddPlayer("TestPlayer", PlayerOptions{})

	// Manually damage player
	player.HP = 50

	// Heal player
	success := engine.HealPlayer("TestPlayer", 30)
	if !success {
		t.Error("HealPlayer should return true for existing player")
	}
	if player.HP != 80 {
		t.Errorf("Expected HP 80 after healing, got %d", player.HP)
	}

	// Heal beyond max
	engine.HealPlayer("TestPlayer", 100)
	if player.HP > player.MaxHP {
		t.Error("HP should not exceed MaxHP")
	}

	// Heal non-existent player
	success = engine.HealPlayer("Nobody", 20)
	if success {
		t.Error("HealPlayer should return false for non-existent player")
	}
}

// TestGetState tests game state retrieval
func TestGetState(t *testing.T) {
	engine := NewEngine(30)

	// Empty state
	state := engine.GetState()
	if state.PlayerCount != 0 {
		t.Error("Empty engine should have 0 players")
	}

	// Add players
	engine.AddPlayer("Player1", PlayerOptions{})
	engine.AddPlayer("Player2", PlayerOptions{})

	state = engine.GetState()
	if state.PlayerCount != 2 {
		t.Errorf("Expected 2 players, got %d", state.PlayerCount)
	}
	if state.AliveCount != 2 {
		t.Errorf("Expected 2 alive, got %d", state.AliveCount)
	}
}

// TestProcessAttack tests attack processing
func TestProcessAttack(t *testing.T) {
	engine := NewEngine(30)

	attacker := engine.AddPlayer("Attacker", PlayerOptions{})
	victim := engine.AddPlayer("Victim", PlayerOptions{})

	// Position players close together for hitbox to connect
	attacker.X = 100
	attacker.Y = 100
	victim.X = 150 // Within fists range (80)
	victim.Y = 100

	// Set attack angle towards victim
	attacker.AttackAngle = 0 // Facing right (towards victim at X=150)

	// Wait for spawn protection to expire
	attacker.SpawnProtection = false
	victim.SpawnProtection = false

	initialHP := victim.HP

	// Process attack
	engine.ProcessAttack(attacker, victim, 25)

	if victim.HP != initialHP-25 {
		t.Errorf("Expected HP %d after attack, got %d", initialHP-25, victim.HP)
	}
}

// TestProcessAttackKill tests kill processing
func TestProcessAttackKill(t *testing.T) {
	engine := NewEngine(30)

	attacker := engine.AddPlayer("Attacker", PlayerOptions{})
	victim := engine.AddPlayer("Victim", PlayerOptions{})

	// Position players close together for hitbox to connect
	attacker.X = 100
	attacker.Y = 100
	victim.X = 140 // Within fists range (80)
	victim.Y = 100

	// Set attack angle towards victim
	attacker.AttackAngle = 0 // Facing right

	attacker.SpawnProtection = false
	victim.SpawnProtection = false
	victim.HP = 10

	initialKills := attacker.Kills
	initialMoney := attacker.Money

	// Fatal attack
	engine.ProcessAttack(attacker, victim, 50)

	if !victim.IsDead {
		t.Error("Victim should be dead after fatal attack")
	}
	if attacker.Kills != initialKills+1 {
		t.Error("Attacker should have +1 kills")
	}
	if attacker.Money != initialMoney+50 {
		t.Error("Attacker should have +50 money")
	}
}

// TestAttackWithSpawnProtection tests that spawn protection blocks attacks
func TestAttackWithSpawnProtection(t *testing.T) {
	engine := NewEngine(30)

	attacker := engine.AddPlayer("Attacker", PlayerOptions{})
	victim := engine.AddPlayer("Victim", PlayerOptions{})

	attacker.SpawnProtection = false
	victim.SpawnProtection = true // Protected

	initialHP := victim.HP

	engine.ProcessAttack(attacker, victim, 50)

	if victim.HP != initialHP {
		t.Error("Victim with spawn protection should not take damage")
	}
}

// TestSetCallbacks tests callback registration
func TestSetCallbacks(t *testing.T) {
	engine := NewEngine(30)

	joinCalled := false

	engine.SetCallbacks(
		func(attacker, victim *Player, damage int) {},
		func(killer, victim *Player) {},
		func(player *Player) { joinCalled = true },
		func(player *Player) {},
	)

	// Trigger join callback
	engine.AddPlayer("Test", PlayerOptions{})
	time.Sleep(10 * time.Millisecond)

	if !joinCalled {
		t.Error("Join callback should have been called")
	}
}

// TestConcurrentAccess tests thread safety
func TestConcurrentAccess(t *testing.T) {
	engine := NewEngine(30)
	engine.Start()
	defer engine.Stop()

	done := make(chan bool)

	// Concurrent reads and writes
	for i := 0; i < 10; i++ {
		go func(id int) {
			for j := 0; j < 100; j++ {
				engine.AddPlayer("Player"+string(rune('A'+id)), PlayerOptions{})
				engine.GetState()
				engine.GetPlayer("Player" + string(rune('A'+id)))
			}
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}
}
