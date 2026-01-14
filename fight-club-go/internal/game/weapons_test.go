package game

import (
	"testing"
)

// TestGetWeapon tests weapon retrieval
func TestGetWeapon(t *testing.T) {
	tests := []struct {
		id       string
		expected string
	}{
		{"fists", "Fists"},
		{"knife", "Knife"},
		{"sword", "Sword"},
		{"axe", "Battle Axe"},
		{"katana", "Katana"},
		{"hammer", "War Hammer"},
		{"scythe", "Scythe"},
	}

	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			weapon := GetWeapon(tt.id)
			if weapon.Name != tt.expected {
				t.Errorf("Expected name '%s', got '%s'", tt.expected, weapon.Name)
			}
		})
	}
}

// TestGetWeaponDefaults tests default weapon for unknown ID
func TestGetWeaponDefaults(t *testing.T) {
	weapon := GetWeapon("unknown_weapon")

	if weapon.ID != "fists" {
		t.Errorf("Unknown weapon should default to fists, got '%s'", weapon.ID)
	}
}

// TestGetAllWeapons tests all weapons retrieval
func TestGetAllWeapons(t *testing.T) {
	weapons := GetAllWeapons()

	if len(weapons) != 9 {
		t.Errorf("Expected 9 weapons, got %d", len(weapons))
	}

	// Verify all weapons have required fields
	for _, w := range weapons {
		if w.ID == "" {
			t.Error("Weapon ID should not be empty")
		}
		if w.Name == "" {
			t.Error("Weapon Name should not be empty")
		}
		if w.MinDamage <= 0 {
			t.Errorf("Weapon %s should have positive MinDamage", w.ID)
		}
		if w.MaxDamage < w.MinDamage {
			t.Errorf("Weapon %s MaxDamage should be >= MinDamage", w.ID)
		}
		if w.Range <= 0 {
			t.Errorf("Weapon %s should have positive Range", w.ID)
		}
		if w.Cooldown <= 0 {
			t.Errorf("Weapon %s should have positive Cooldown", w.ID)
		}
	}
}

// TestWeaponRangeGreaterThanCollision tests weapon ranges are > 60
func TestWeaponRangeGreaterThanCollision(t *testing.T) {
	weapons := GetAllWeapons()

	// Minimum distance between two players is 60 (radius 30 + 30)
	minRequiredRange := 60.0

	for _, w := range weapons {
		if w.Range <= minRequiredRange {
			t.Errorf("Weapon %s has range %.0f which is <= collision distance %.0f",
				w.ID, w.Range, minRequiredRange)
		}
	}
}

// TestWeaponDamageRanges tests damage makes sense
func TestWeaponDamageRanges(t *testing.T) {
	tests := []struct {
		id        string
		minDamage int
		maxDamage int
	}{
		{"fists", 8, 15},
		{"knife", 12, 22},
		{"sword", 18, 35},
		{"hammer", 45, 75}, // Updated from 40-70
	}

	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			weapon := GetWeapon(tt.id)
			if weapon.MinDamage != tt.minDamage {
				t.Errorf("Expected MinDamage %d, got %d", tt.minDamage, weapon.MinDamage)
			}
			if weapon.MaxDamage != tt.maxDamage {
				t.Errorf("Expected MaxDamage %d, got %d", tt.maxDamage, weapon.MaxDamage)
			}
		})
	}
}

// TestWeaponPrices tests weapon prices are reasonable
func TestWeaponPrices(t *testing.T) {
	weapons := GetAllWeapons()

	fists := GetWeapon("fists")
	if fists.Price != 0 {
		t.Error("Fists should be free")
	}

	// All other weapons should cost money
	for _, w := range weapons {
		if w.ID != "fists" && w.Price <= 0 {
			t.Errorf("Weapon %s should have a price > 0", w.ID)
		}
	}
}

// TestWeaponColors tests all weapons have valid colors
func TestWeaponColors(t *testing.T) {
	weapons := GetAllWeapons()

	for _, w := range weapons {
		if len(w.Color) != 7 || w.Color[0] != '#' {
			t.Errorf("Weapon %s has invalid color format: %s", w.ID, w.Color)
		}
	}
}

// TestWeaponCooldowns tests cooldowns are reasonable
func TestWeaponCooldowns(t *testing.T) {
	weapons := GetAllWeapons()

	for _, w := range weapons {
		if w.Cooldown < 0.1 {
			t.Errorf("Weapon %s cooldown %.2f is too fast", w.ID, w.Cooldown)
		}
		if w.Cooldown > 2.0 {
			t.Errorf("Weapon %s cooldown %.2f is too slow", w.ID, w.Cooldown)
		}
	}
}

// TestWeaponEmojis tests all weapons have emojis
func TestWeaponEmojis(t *testing.T) {
	weapons := GetAllWeapons()

	for _, w := range weapons {
		if w.Emoji == "" {
			t.Errorf("Weapon %s should have an emoji", w.ID)
		}
	}
}
