package api

import (
	"net/http"

	"fight-club/internal/game"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
)

// EngineInterface defines the game engine methods used by the API.
// This interface enables mocking for tests without spinning up the full game loop.
// Keep this minimal - only include methods the API layer actually calls.
type EngineInterface interface {
	// GetState returns the current game state for rendering/API responses
	GetState() game.GameState
	// GetSnapshot returns the latest lock-free immutable snapshot (preferred for API stats)
	GetSnapshot() *game.GameSnapshot
	// AddPlayer adds a new player to the game
	AddPlayer(name string, opts game.PlayerOptions) *game.Player
	// HealPlayer heals a player by the specified amount
	HealPlayer(name string, amount int) bool
	// GetPlayer returns a player by name (may be nil)
	GetPlayer(name string) *game.Player
}

// StreamerInterface defines the streamer methods used by the API.
// This interface enables mocking for tests that don't need actual streaming.
type StreamerInterface interface {
	// Start begins streaming to the configured RTMP endpoint
	Start() error
	// Stop ends the current stream
	Stop()
	// IsStreaming returns whether the stream is currently active
	IsStreaming() bool
	// GetStats returns current streaming statistics
	GetStats() map[string]interface{}
}

// RouterConfig contains all dependencies needed to construct the HTTP router.
// This struct is designed for dependency injection and testability.
//
// Example usage in tests:
//
//	cfg := api.RouterConfig{
//	    Engine:   mockEngine,
//	    Streamer: mockStreamer,
//	    RateLimitConfig: &api.RateLimitConfig{
//	        RequestsPerSecond: 1000, // High limit for tests
//	        Burst:             1000,
//	    },
//	}
//	router := api.NewRouter(cfg)
//	ts := httptest.NewServer(router)
type RouterConfig struct {
	// Engine is the game engine (required)
	Engine EngineInterface

	// Streamer is the stream manager (required)
	Streamer StreamerInterface

	// RateLimiter is an optional pre-configured rate limiter.
	// If nil, a new one will be created using RateLimitConfig.
	RateLimiter *IPRateLimiter

	// RateLimitConfig is optional configuration for the rate limiter.
	// Only used if RateLimiter is nil. If both are nil, uses DefaultRateLimitConfig.
	RateLimitConfig *RateLimitConfig

	// CORSOrigins is an optional list of allowed CORS origins.
	// If nil, uses the default production origins.
	CORSOrigins []string

	// StaticFilesDir is the directory to serve static files from for the admin panel.
	// If empty, defaults to "./admin-panel".
	StaticFilesDir string

	// DisableLogging disables the request logger middleware (useful for benchmarks).
	DisableLogging bool

	// KickWebhookHandler is an optional HTTP handler for Kick webhook callbacks
	KickWebhookHandler http.Handler
}

// routerHandlers holds the handler functions for the router.
// This is used internally to pass handlers to route setup.
type routerHandlers struct {
	engine   EngineInterface
	streamer StreamerInterface
}

// NewRouter constructs the HTTP router with all middleware and routes.
//
// IMPORTANT: This function is PURE - it has no side effects:
//   - No goroutines are started
//   - No network listeners are opened
//   - No background workers are launched
//
// This makes it safe to use in tests with httptest.NewServer.
//
// Example:
//
//	router := api.NewRouter(cfg)
//	ts := httptest.NewServer(router)
//	defer ts.Close()
//	resp, _ := http.Get(ts.URL + "/api/state")
func NewRouter(cfg RouterConfig) *chi.Mux {
	r := chi.NewRouter()

	// Middleware - Order matters!
	if !cfg.DisableLogging {
		r.Use(middleware.Logger)
	}
	r.Use(middleware.Recoverer)

	// Rate limiting (BEFORE CORS to reject early and save CPU)
	rateLimiter := cfg.RateLimiter
	if rateLimiter == nil {
		rateLimitCfg := DefaultRateLimitConfig
		if cfg.RateLimitConfig != nil {
			rateLimitCfg = *cfg.RateLimitConfig
		}
		rateLimiter = NewIPRateLimiter(rateLimitCfg)
	}
	r.Use(rateLimiter.Middleware)

	// CORS configuration
	corsOrigins := cfg.CORSOrigins
	if corsOrigins == nil {
		corsOrigins = []string{
			"http://localhost:*",
			"http://127.0.0.1:*",
			"https://kick.com",
			"https://*.kick.com",
		}
	}
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   corsOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"*"},
		AllowCredentials: true,
	}))

	// Create handlers struct
	h := &routerHandlers{
		engine:   cfg.Engine,
		streamer: cfg.Streamer,
	}

	// API routes
	r.Route("/api", func(r chi.Router) {
		// Game state
		r.Get("/state", h.handleGetState)
		r.Get("/stats", h.handleGetStats)
		r.Get("/leaderboard", h.handleGetLeaderboard)

		// Player management
		r.Post("/player/join", h.handlePlayerJoin)
		r.Post("/player/batch", h.handlePlayerBatchJoin)
		r.Post("/player/heal", h.handlePlayerHeal)
		r.Post("/player/weapon", h.handlePlayerWeapon)

		// Stream control
		r.Post("/stream/start", h.handleStreamStart)
		r.Post("/stream/stop", h.handleStreamStop)
		r.Get("/stream/status", h.handleStreamStatus)

		// Admin
		r.Get("/weapons", h.handleGetWeapons)

		// Kick routes (OAuth callback, webhook) - if handler provided
		// Use chi Route group with catch-all that modifies path for http.ServeMux
		if cfg.KickWebhookHandler != nil {
			r.Route("/kick", func(kickRouter chi.Router) {
				// Handle all methods with catch-all pattern
				kickRouter.HandleFunc("/*", func(w http.ResponseWriter, req *http.Request) {
					// Get the path after /api/kick (chi provides this via RouteContext)
					rctx := chi.RouteContext(req.Context())
					pathPrefix := "/api/kick"
					newPath := req.URL.Path[len(pathPrefix):]
					if newPath == "" {
						newPath = "/"
					}
					// Create a new request with the modified path
					req2 := req.Clone(req.Context())
					req2.URL.Path = newPath
					req2.URL.RawPath = ""
					req2.RequestURI = newPath
					if req.URL.RawQuery != "" {
						req2.RequestURI = newPath + "?" + req.URL.RawQuery
					}
					_ = rctx // Acknowledge usage
					cfg.KickWebhookHandler.ServeHTTP(w, req2)
				})
			})
		}
	})

	// Serve static files for admin panel
	staticDir := cfg.StaticFilesDir
	if staticDir == "" {
		staticDir = "./admin-panel"
	}
	r.Handle("/admin/*", http.StripPrefix("/admin/", http.FileServer(http.Dir(staticDir))))
	r.Get("/admin", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/admin/", http.StatusMovedPermanently)
	})

	// Default route
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/admin/", http.StatusFound)
	})

	return r
}

// GetRateLimiterFromRouter is a helper to extract the rate limiter from a configured router.
// This is useful for tests that need to verify rate limiting behavior.
// Note: This returns nil if you need to track the limiter - pass it via RouterConfig instead.
func GetRateLimiterFromRouter(cfg RouterConfig) *IPRateLimiter {
	if cfg.RateLimiter != nil {
		return cfg.RateLimiter
	}
	rateLimitCfg := DefaultRateLimitConfig
	if cfg.RateLimitConfig != nil {
		rateLimitCfg = *cfg.RateLimitConfig
	}
	return NewIPRateLimiter(rateLimitCfg)
}
