package chat

import (
	"sync"
	"time"
)

// RateLimiter implements per-user command rate limiting
type RateLimiter struct {
	mu         sync.RWMutex
	userCounts map[string]*userLimit
	config     RateLimitConfig
}

type userLimit struct {
	count     int
	windowEnd time.Time
	lastCmd   time.Time
}

// RateLimitConfig configures rate limiting behavior
type RateLimitConfig struct {
	// MaxPerWindow is max commands per window
	MaxPerWindow int
	// WindowDuration is the sliding window size
	WindowDuration time.Duration
	// CooldownDuration is minimum time between commands
	CooldownDuration time.Duration
}

// DefaultRateLimitConfig for chat commands
var DefaultRateLimitConfig = RateLimitConfig{
	MaxPerWindow:     5,                      // 5 commands
	WindowDuration:   5 * time.Second,        // per 5 seconds
	CooldownDuration: 500 * time.Millisecond, // 500ms between commands
}

// NewRateLimiter creates a new rate limiter
func NewRateLimiter(cfg RateLimitConfig) *RateLimiter {
	rl := &RateLimiter{
		userCounts: make(map[string]*userLimit),
		config:     cfg,
	}

	// Start cleanup goroutine
	go rl.cleanup()

	return rl
}

// Allow checks if a user can execute a command
func (rl *RateLimiter) Allow(username string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	key := username

	limit, exists := rl.userCounts[key]
	if !exists {
		rl.userCounts[key] = &userLimit{
			count:     1,
			windowEnd: now.Add(rl.config.WindowDuration),
			lastCmd:   now,
		}
		return true
	}

	// Check cooldown
	if now.Sub(limit.lastCmd) < rl.config.CooldownDuration {
		return false
	}

	// Check/reset window
	if now.After(limit.windowEnd) {
		limit.count = 1
		limit.windowEnd = now.Add(rl.config.WindowDuration)
		limit.lastCmd = now
		return true
	}

	// Check count
	if limit.count >= rl.config.MaxPerWindow {
		return false
	}

	limit.count++
	limit.lastCmd = now
	return true
}

// cleanup removes old entries every minute
func (rl *RateLimiter) cleanup() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		rl.mu.Lock()
		now := time.Now()
		cutoff := now.Add(-5 * time.Minute)

		for key, limit := range rl.userCounts {
			if limit.lastCmd.Before(cutoff) {
				delete(rl.userCounts, key)
			}
		}
		rl.mu.Unlock()
	}
}
