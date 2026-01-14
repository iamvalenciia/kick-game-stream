package kick

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
)

// KillEvent represents a single kill to be broadcast
type KillEvent struct {
	Killer      string
	Victim      string
	Weapon      string
	KillerKills int
}

// Bot handles the high-level logic for the Kick Kill-Feed Bot
// It wraps the Service and adds:
// - Rate limiting
// - Queue management (Drop Newest)
// - Automatic failure recovery (404/ID handling)
type Bot struct {
	service *Service
	queue   chan KillEvent
	quit    chan struct{}
	wg      sync.WaitGroup
	// Backoff state
	rateLimit      time.Duration
	currentBackoff time.Duration
	maxBackoff     time.Duration
}

// NewBot creates a new Kick Bot
func NewBot(service *Service) *Bot {
	return &Bot{
		service:    service,
		queue:      make(chan KillEvent, 100), // Buffer size 100 from Architecture
		quit:       make(chan struct{}),
		rateLimit:  2000 * time.Millisecond, // 2.0s per message to avoid spam filters
		maxBackoff: 60 * time.Second,
	}
}

// Start begins the dispatcher loop
func (b *Bot) Start() {
	b.wg.Add(1)
	go b.dispatcher()
	log.Println("🤖 Kick Bot dispatcher started")
}

// Stop gracefully shuts down the bot
func (b *Bot) Stop() {
	close(b.quit)
	b.wg.Wait()
	log.Println("🤖 Kick Bot dispatcher stopped")
}

// QueueKill attempts to queue a kill event.
// Non-blocking: if queue is full, the event is intentionally DROPPED (Drop Newest policy).
func (b *Bot) QueueKill(killer, victim, weapon string, killerKills int) {
	event := KillEvent{
		Killer:      killer,
		Victim:      victim,
		Weapon:      weapon,
		KillerKills: killerKills,
	}

	select {
	case b.queue <- event:
		// Successfully queued
	default:
		// Queue full, drop newest
		// Optional: Metric increment here
		// log.Printf("⚠️ Kill feed full, dropping event: %s -> %s", killer, victim)
	}
}

// dispatcher is the main event loop
func (b *Bot) dispatcher() {
	defer b.wg.Done()

	ticker := time.NewTicker(b.rateLimit)
	defer ticker.Stop()

	for {
		select {
		case <-b.quit:
			return

		case event := <-b.queue:
			// Wait for rate limit tick
			select {
			case <-ticker.C:
			case <-b.quit:
				return
			}

			b.processEvent(event)
		}
	}
}

// getWeaponEmoji returns an emoji for the weapon type
func getWeaponEmoji(weapon string) string {
	weaponLower := strings.ToLower(weapon)
	switch weaponLower {
	case "sword":
		return "🗡️"
	case "spear":
		return "🔱"
	case "axe":
		return "🪓"
	case "bow":
		return "🏹"
	case "scythe":
		return "⚔️"
	case "hammer":
		return "🔨"
	case "fists", "fist":
		return "👊"
	default:
		return "⚔️"
	}
}

// processEvent handles the actual sending and error recovery
func (b *Bot) processEvent(event KillEvent) {
	// 1. Format Message with weapon emoji and kill count
	weaponEmoji := getWeaponEmoji(event.Weapon)
	msg := fmt.Sprintf("%s %s eliminated %s (%d kills)", weaponEmoji, event.Killer, event.Victim, event.KillerKills)

	// 2. Send Message as USER (type: "user")
	// Using SendMessage with broadcaster_user_id - this sends as the streamer account
	// Note: type "bot" returns 500 error, so we use type "user" instead
	log.Printf("🎮 Kill event: %s -> %s (weapon: %s)", event.Killer, event.Victim, event.Weapon)
	err := b.service.SendMessage(msg)

	// 3. Handle Errors
	if err != nil {
		errMsg := err.Error()

		if strings.Contains(errMsg, "429") {
			// Exponential Backoff
			if b.currentBackoff == 0 {
				b.currentBackoff = 2 * time.Second
			} else {
				b.currentBackoff *= 2
				if b.currentBackoff > b.maxBackoff {
					b.currentBackoff = b.maxBackoff
				}
			}
			log.Printf("⚠️ Rate Limited (429). Backing off for %v. Error: %v", b.currentBackoff, err)
			time.Sleep(b.currentBackoff)
		} else {
			// Other errors (400, 404, 500)
			// For 400/404 on SendMessage, it usually means Broadcaster ID is wrong or Token is invalid.
			// We log but don't crash or sleep extensively, just continue to next message.
			log.Printf("⚠️ Failed to send kill feed: %v", err)
		}
	} else {
		// Success - reset backoff
		if b.currentBackoff > 0 {
			b.currentBackoff = 0
			log.Println("✅ Kick API recovered, backoff reset")
		}
	}
}
