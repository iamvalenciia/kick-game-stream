package game_test

import (
	"math"
	"testing"

	"fight-club/internal/game"
)

// TestWeaponAnimationConfigs verifies all weapons have valid animation configs
func TestWeaponAnimationConfigs(t *testing.T) {
	weapons := []string{"fists", "knife", "sword", "spear", "axe", "bow", "scythe", "katana", "hammer"}

	for _, weaponID := range weapons {
		t.Run(weaponID, func(t *testing.T) {
			anim := game.GetWeaponAnimation(weaponID)

			// Verify weapon ID matches
			if anim.WeaponID != weaponID {
				t.Errorf("WeaponID mismatch: got %s, want %s", anim.WeaponID, weaponID)
			}

			// Verify timing is positive
			if anim.WindUpTicks < 0 {
				t.Errorf("WindUpTicks should be non-negative, got %d", anim.WindUpTicks)
			}
			if anim.ActiveTicks <= 0 {
				t.Errorf("ActiveTicks should be positive, got %d", anim.ActiveTicks)
			}
			if anim.RecoveryTicks < 0 {
				t.Errorf("RecoveryTicks should be non-negative, got %d", anim.RecoveryTicks)
			}

			// Verify knockback is non-negative
			if anim.KnockbackForce < 0 {
				t.Errorf("KnockbackForce should be non-negative, got %f", anim.KnockbackForce)
			}

			// Verify shake intensity is within limits
			if anim.ShakeIntensity < 0 || anim.ShakeIntensity > game.MaxShakeIntensity {
				t.Errorf("ShakeIntensity out of range [0, %f]: got %f", game.MaxShakeIntensity, anim.ShakeIntensity)
			}

			// Verify trail type is set
			if anim.TrailType < game.TrailNone || anim.TrailType > game.TrailProjectile {
				t.Errorf("Invalid TrailType: %d", anim.TrailType)
			}
		})
	}
}

// TestBowIsProjectile verifies bow uses projectile system
func TestBowIsProjectile(t *testing.T) {
	anim := game.GetWeaponAnimation("bow")

	if !anim.IsProjectile {
		t.Error("Bow should be a projectile weapon")
	}

	if anim.ProjectileSpeed <= 0 {
		t.Errorf("Bow projectile speed should be positive, got %f", anim.ProjectileSpeed)
	}

	if anim.TrailType != game.TrailProjectile {
		t.Errorf("Bow trail type should be TrailProjectile, got %d", anim.TrailType)
	}
}

// TestHeavyWeaponsHaveHighKnockback verifies axe and hammer feel heavy
func TestHeavyWeaponsHaveHighKnockback(t *testing.T) {
	heavyWeapons := []string{"axe", "hammer"}
	lightWeapons := []string{"fists", "knife"}

	var heavyMin float64 = math.MaxFloat64
	var lightMax float64 = 0

	for _, w := range heavyWeapons {
		anim := game.GetWeaponAnimation(w)
		if anim.KnockbackForce < heavyMin {
			heavyMin = anim.KnockbackForce
		}
	}

	for _, w := range lightWeapons {
		anim := game.GetWeaponAnimation(w)
		if anim.KnockbackForce > lightMax {
			lightMax = anim.KnockbackForce
		}
	}

	if heavyMin <= lightMax {
		t.Errorf("Heavy weapons should have higher knockback than light weapons. Heavy min: %f, Light max: %f", heavyMin, lightMax)
	}
}

// TestWeaponAnimationTotalDuration verifies total attack duration calculation
func TestWeaponAnimationTotalDuration(t *testing.T) {
	anim := game.GetWeaponAnimation("sword")

	expectedTicks := anim.WindUpTicks + anim.ActiveTicks + anim.RecoveryTicks
	if anim.TotalAttackTicks() != expectedTicks {
		t.Errorf("TotalAttackTicks mismatch: got %d, want %d", anim.TotalAttackTicks(), expectedTicks)
	}

	expectedDuration := float64(expectedTicks) / 20.0
	if anim.TotalAttackDuration() != expectedDuration {
		t.Errorf("TotalAttackDuration mismatch: got %f, want %f", anim.TotalAttackDuration(), expectedDuration)
	}
}

// TestDefaultAnimationFallback verifies unknown weapons default to fists
func TestDefaultAnimationFallback(t *testing.T) {
	unknownAnim := game.GetWeaponAnimation("unknown_weapon")
	fistsAnim := game.GetWeaponAnimation("fists")

	if unknownAnim.WeaponID != fistsAnim.WeaponID {
		t.Errorf("Unknown weapon should default to fists, got %s", unknownAnim.WeaponID)
	}
}

// TestTrailTypesMatchHitboxTypes verifies consistency between trail and hitbox designs
func TestTrailTypesMatchHitboxTypes(t *testing.T) {
	testCases := []struct {
		weaponID      string
		expectedTrail game.TrailType
	}{
		{"fists", game.TrailRadial},   // Circle hitbox -> radial trail
		{"sword", game.TrailArc},      // Arc hitbox -> arc trail
		{"spear", game.TrailLine},     // Line hitbox -> line trail
		{"bow", game.TrailProjectile}, // Projectile hitbox -> projectile trail
	}

	for _, tc := range testCases {
		t.Run(tc.weaponID, func(t *testing.T) {
			anim := game.GetWeaponAnimation(tc.weaponID)
			if anim.TrailType != tc.expectedTrail {
				t.Errorf("Weapon %s should have trail type %d, got %d", tc.weaponID, tc.expectedTrail, anim.TrailType)
			}
		})
	}
}
