package api

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/time/rate"
)

// RateLimitConfig configures the IP-based rate limiter
type RateLimitConfig struct {
	RequestsPerSecond float64       // Requests allowed per second per IP
	Burst             int           // Maximum burst size
	CleanupInterval   time.Duration // How often to clean up stale limiters
}

// DefaultRateLimitConfig returns production-safe defaults
var DefaultRateLimitConfig = RateLimitConfig{
	RequestsPerSecond: 10,              // 10 requests per second per IP
	Burst:             20,              // Allow burst of 20
	CleanupInterval:   5 * time.Minute, // Clean up every 5 minutes
}

// ipLimiterEntry tracks per-IP rate limiting state
type ipLimiterEntry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// IPRateLimiter provides IP-based rate limiting for HTTP requests
type IPRateLimiter struct {
	limiters sync.Map // map[string]*ipLimiterEntry
	config   RateLimitConfig
	stopChan chan struct{}
	stopOnce sync.Once

	// Stats for monitoring
	rejectedCount uint64 // atomic
	allowedCount  uint64 // atomic
}

// NewIPRateLimiter creates a new IP-based rate limiter
func NewIPRateLimiter(cfg RateLimitConfig) *IPRateLimiter {
	rl := &IPRateLimiter{
		config:   cfg,
		stopChan: make(chan struct{}),
	}

	// Start cleanup goroutine to prevent memory leak from abandoned IPs
	go rl.cleanupLoop()

	return rl
}

// Stop stops the rate limiter cleanup goroutine
func (rl *IPRateLimiter) Stop() {
	rl.stopOnce.Do(func() {
		close(rl.stopChan)
	})
}

// getLimiter returns or creates a rate limiter for the given IP
func (rl *IPRateLimiter) getLimiter(ip string) *rate.Limiter {
	now := time.Now()

	if entry, ok := rl.limiters.Load(ip); ok {
		e := entry.(*ipLimiterEntry)
		e.lastSeen = now
		return e.limiter
	}

	entry := &ipLimiterEntry{
		limiter:  rate.NewLimiter(rate.Limit(rl.config.RequestsPerSecond), rl.config.Burst),
		lastSeen: now,
	}

	actual, _ := rl.limiters.LoadOrStore(ip, entry)
	return actual.(*ipLimiterEntry).limiter
}

// cleanupLoop periodically removes stale rate limiters
func (rl *IPRateLimiter) cleanupLoop() {
	ticker := time.NewTicker(rl.config.CleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-rl.stopChan:
			return
		case <-ticker.C:
			rl.cleanup()
		}
	}
}

// cleanup removes rate limiters that haven't been used recently
func (rl *IPRateLimiter) cleanup() {
	cutoff := time.Now().Add(-rl.config.CleanupInterval * 2)

	rl.limiters.Range(func(key, value interface{}) bool {
		entry := value.(*ipLimiterEntry)
		if entry.lastSeen.Before(cutoff) {
			rl.limiters.Delete(key)
		}
		return true
	})
}

// Allow checks if a request from the given IP should be allowed
func (rl *IPRateLimiter) Allow(ip string) bool {
	limiter := rl.getLimiter(ip)
	if limiter.Allow() {
		atomic.AddUint64(&rl.allowedCount, 1)
		return true
	}
	atomic.AddUint64(&rl.rejectedCount, 1)
	return false
}

// Middleware returns an HTTP middleware for rate limiting
func (rl *IPRateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := GetClientIP(r)
		if !rl.Allow(ip) {
			RecordConnectionRejected("rate_limit")
			w.Header().Set("Retry-After", "1")
			http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// GetStats returns rate limiter statistics
func (rl *IPRateLimiter) GetStats() map[string]uint64 {
	return map[string]uint64{
		"allowed":  atomic.LoadUint64(&rl.allowedCount),
		"rejected": atomic.LoadUint64(&rl.rejectedCount),
	}
}

// GetClientIP extracts the client IP from an HTTP request
// Handles X-Forwarded-For header for proxied requests
func GetClientIP(r *http.Request) string {
	// Check X-Forwarded-For for proxied requests
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// Take first IP (original client IP)
		// CAUTION: This can be spoofed if not behind a trusted proxy
		if idx := strings.Index(xff, ","); idx >= 0 {
			return strings.TrimSpace(xff[:idx])
		}
		return strings.TrimSpace(xff)
	}

	// Check X-Real-IP header
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return strings.TrimSpace(xri)
	}

	// Fall back to RemoteAddr
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}

// WebSocketRateLimiter limits concurrent WebSocket connections per IP
type WebSocketRateLimiter struct {
	connections sync.Map // map[string]*int32 (atomic counter)
	maxPerIP    int

	// Stats
	rejectedCount uint64 // atomic
}

// NewWebSocketRateLimiter creates a WebSocket connection limiter
func NewWebSocketRateLimiter(maxPerIP int) *WebSocketRateLimiter {
	return &WebSocketRateLimiter{maxPerIP: maxPerIP}
}

// Allow checks if a new WebSocket connection from this IP is allowed
func (wrl *WebSocketRateLimiter) Allow(ip string) bool {
	// Load or create counter for this IP
	actual, _ := wrl.connections.LoadOrStore(ip, new(int32))
	counter := actual.(*int32)

	// Atomically check and increment
	for {
		current := atomic.LoadInt32(counter)
		if int(current) >= wrl.maxPerIP {
			atomic.AddUint64(&wrl.rejectedCount, 1)
			return false
		}
		if atomic.CompareAndSwapInt32(counter, current, current+1) {
			return true
		}
	}
}

// Release decrements the connection count for this IP
func (wrl *WebSocketRateLimiter) Release(ip string) {
	if val, ok := wrl.connections.Load(ip); ok {
		counter := val.(*int32)
		atomic.AddInt32(counter, -1)
	}
}

// GetConnectionCount returns current connection count for an IP
func (wrl *WebSocketRateLimiter) GetConnectionCount(ip string) int {
	if val, ok := wrl.connections.Load(ip); ok {
		return int(atomic.LoadInt32(val.(*int32)))
	}
	return 0
}

// GetStats returns WebSocket rate limiter statistics
func (wrl *WebSocketRateLimiter) GetStats() map[string]uint64 {
	return map[string]uint64{
		"rejected": atomic.LoadUint64(&wrl.rejectedCount),
	}
}

// AllowedOrigins defines the allowed origins for CORS and WebSocket
var AllowedOrigins = []string{
	"http://localhost",
	"http://localhost:3000",
	"http://localhost:8080",
	"https://kick.com",
	"https://www.kick.com",
}

// IsAllowedOrigin checks if an origin is in the allowed list
func IsAllowedOrigin(origin string) bool {
	if origin == "" {
		return false
	}

	// Allow localhost with any port
	if strings.HasPrefix(origin, "http://localhost") {
		return true
	}

	// Check against allowed list
	for _, allowed := range AllowedOrigins {
		if origin == allowed {
			return true
		}
	}

	// Allow subdomains of kick.com
	if strings.HasSuffix(origin, ".kick.com") {
		return true
	}

	return false
}
