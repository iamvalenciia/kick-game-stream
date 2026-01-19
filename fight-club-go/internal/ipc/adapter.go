package ipc

import (
	"time"

	"fight-club/internal/game"
)

// ToGameSnapshot converts an IPC SnapshotMessage to a game.GameSnapshot
// This allows reusing existing render code that expects GameSnapshot
func (msg *SnapshotMessage) ToGameSnapshot() *game.GameSnapshot {
	snap := &game.GameSnapshot{
		Sequence:    msg.Sequence,
		Timestamp:   time.Unix(0, msg.Timestamp),
		TickNumber:  msg.TickNumber,
		PlayerCount: msg.PlayerCount,
		AliveCount:  msg.AliveCount,
		TotalKills:  msg.TotalKills,
		Shake: game.ShakeSnapshot{
			OffsetX:   msg.ShakeOffsetX,
			OffsetY:   msg.ShakeOffsetY,
			Intensity: msg.ShakeIntensity,
		},
	}

	// Convert players
	snap.Players = make([]game.PlayerSnapshot, len(msg.Players))
	for i, p := range msg.Players {
		snap.Players[i] = game.PlayerSnapshot{
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
			DodgeDirection:  p.DodgeDirection,
			ComboCount:      p.ComboCount,
			Stamina:         p.Stamina,
		}
	}

	// Convert particles
	snap.Particles = make([]game.ParticleSnapshot, len(msg.Particles))
	for i, p := range msg.Particles {
		snap.Particles[i] = game.ParticleSnapshot{
			X:     p.X,
			Y:     p.Y,
			Color: p.Color,
			Alpha: p.Alpha,
		}
	}

	// Convert effects
	snap.Effects = make([]game.EffectSnapshot, len(msg.Effects))
	for i, e := range msg.Effects {
		snap.Effects[i] = game.EffectSnapshot{
			X:     e.X,
			Y:     e.Y,
			TX:    e.TX,
			TY:    e.TY,
			Color: e.Color,
			Timer: e.Timer,
		}
	}

	// Convert texts
	snap.Texts = make([]game.TextSnapshot, len(msg.Texts))
	for i, t := range msg.Texts {
		snap.Texts[i] = game.TextSnapshot{
			X:     t.X,
			Y:     t.Y,
			Text:  t.Text,
			Color: t.Color,
			Alpha: t.Alpha,
		}
	}

	// Convert trails
	snap.Trails = make([]game.TrailSnapshot, len(msg.Trails))
	for i, tr := range msg.Trails {
		td := game.TrailSnapshot{
			Count:    tr.Count,
			Color:    tr.Color,
			Alpha:    tr.Alpha,
			PlayerID: tr.PlayerID,
		}
		for j := 0; j < 8 && j < tr.Count; j++ {
			td.Points[j] = game.TrailPointSnapshot{
				X:     tr.Points[j].X,
				Y:     tr.Points[j].Y,
				Alpha: tr.Points[j].Alpha,
			}
		}
		snap.Trails[i] = td
	}

	// Convert flashes
	snap.Flashes = make([]game.FlashSnapshot, len(msg.Flashes))
	for i, f := range msg.Flashes {
		snap.Flashes[i] = game.FlashSnapshot{
			X:         f.X,
			Y:         f.Y,
			Radius:    f.Radius,
			Color:     f.Color,
			Intensity: f.Intensity,
		}
	}

	// Convert projectiles
	snap.Projectiles = make([]game.ProjectileSnapshot, len(msg.Projectiles))
	for i, p := range msg.Projectiles {
		snap.Projectiles[i] = game.ProjectileSnapshot{
			X:          p.X,
			Y:          p.Y,
			Rotation:   p.Rotation,
			Color:      p.Color,
			TrailX:     p.TrailX,
			TrailY:     p.TrailY,
			TrailCount: p.TrailCount,
		}
	}

	return snap
}
