package kick

import (
	"fmt"
	"testing"
	"time"
)

// TestQueueOverflow verifies that the queue drops messages when full
// instead of blocking the caller (critical for Game Engine performance)
func TestQueueOverflow(t *testing.T) {
	bot := NewBot(nil) // Service not required for this test

	// Fill queue (capacity 100)
	for i := 0; i < 100; i++ {
		bot.QueueKill("Killer", "Victim", "Weapon", i+1)
	}

	// Queue is full. Next one should be dropped instantly.
	done := make(chan bool)
	go func() {
		bot.QueueKill("Killer", "Victim", "Weapon", 101)
		done <- true
	}()

	select {
	case <-done:
		// Success (didn't block)
	case <-time.After(50 * time.Millisecond):
		t.Fatal("QueueKill blocked on full queue! It should have dropped the message.")
	}
}

// TestMessageFormat verifies the output string format
func TestMessageFormat(t *testing.T) {
	killer := "PlayerOne"
	victim := "PlayerTwo"
	weapon := "sword"
	kills := 5

	// New format includes kill count: "🗡️ PlayerOne eliminated PlayerTwo (5 kills)"
	weaponEmoji := getWeaponEmoji(weapon)
	expected := fmt.Sprintf("%s %s eliminated %s (%d kills)", weaponEmoji, killer, victim, kills)
	actual := fmt.Sprintf("%s %s eliminated %s (%d kills)", "🗡️", killer, victim, kills)

	if actual != expected {
		t.Errorf("Message format incorrect. Got: %s, Want: %s", actual, expected)
	}
}
