package game

import (
	"encoding/json"
	"time"
)

// EventType enum for event classification
type EventType uint8

const (
	EventTypeUnknown EventType = iota
	EventTypeTick              // Tick boundary with RNG seed
	EventTypePlayerJoin
	EventTypePlayerLeave
	EventTypeDamage
	EventTypeKill
	EventTypeHeal
	EventTypeRespawn
	EventTypeAttack
)

// EventVersion for backwards compatibility in replay
const EventVersion uint8 = 1

// Event is the core event structure for the event log
type Event struct {
	Version   uint8     `json:"version"`   // Schema version
	Type      EventType `json:"type"`      // Event type
	Timestamp int64     `json:"timestamp"` // Unix nano
	Sequence  uint64    `json:"sequence"`  // Monotonic sequence
	TickNum   uint64    `json:"tickNum"`   // Game tick this occurred in
	PlayerID  string    `json:"playerId"`  // Source player (for rate limiting)
	Payload   []byte    `json:"payload"`   // JSON-encoded payload
}

// String returns human-readable event type
func (t EventType) String() string {
	switch t {
	case EventTypeTick:
		return "tick"
	case EventTypePlayerJoin:
		return "player_join"
	case EventTypePlayerLeave:
		return "player_leave"
	case EventTypeDamage:
		return "damage"
	case EventTypeKill:
		return "kill"
	case EventTypeHeal:
		return "heal"
	case EventTypeRespawn:
		return "respawn"
	case EventTypeAttack:
		return "attack"
	default:
		return "unknown"
	}
}

// Typed payloads for different event types

// TickPayload contains tick boundary information for replay
type TickPayload struct {
	RNGSeed     int64 `json:"rngSeed"`
	PlayerCount int   `json:"playerCount"`
	DeltaTimeNs int64 `json:"deltaTimeNs"`
}

// DamagePayload contains damage event details
type DamagePayload struct {
	AttackerID string `json:"attackerId"`
	VictimID   string `json:"victimId"`
	Damage     int    `json:"damage"`
	VictimHP   int    `json:"victimHp"`
	WeaponID   string `json:"weaponId"`
}

// KillPayload contains kill event details
type KillPayload struct {
	KillerID     string `json:"killerId"`
	VictimID     string `json:"victimId"`
	KillerKills  int    `json:"killerKills"`
	VictimDeaths int    `json:"victimDeaths"`
}

// PlayerJoinPayload contains player join details
type PlayerJoinPayload struct {
	PlayerID   string  `json:"playerId"`
	PlayerName string  `json:"playerName"`
	SpawnX     float64 `json:"spawnX"`
	SpawnY     float64 `json:"spawnY"`
	Color      string  `json:"color"`
}

// HealPayload contains heal event details
type HealPayload struct {
	PlayerID  string `json:"playerId"`
	Amount    int    `json:"amount"`
	CurrentHP int    `json:"currentHp"`
}

// RespawnPayload contains respawn event details
type RespawnPayload struct {
	PlayerID string  `json:"playerId"`
	SpawnX   float64 `json:"spawnX"`
	SpawnY   float64 `json:"spawnY"`
}

// EncodePayload marshals a payload to JSON bytes
func EncodePayload(payload interface{}) []byte {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil
	}
	return data
}

// NewEvent creates a new event with the current timestamp
func NewEvent(eventType EventType, tickNum uint64, playerID string, payload interface{}) Event {
	return Event{
		Version:   EventVersion,
		Type:      eventType,
		Timestamp: time.Now().UnixNano(),
		TickNum:   tickNum,
		PlayerID:  playerID,
		Payload:   EncodePayload(payload),
	}
}
