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

	// SessionManager is optional - if provided, admin routes will be protected
	SessionManager *SessionManager

	// EnableAdminAuth enables authentication for admin panel (requires SessionManager)
	EnableAdminAuth bool

	// LoginPagePath is the path to the login HTML file
	// If empty, a default embedded login page will be used
	LoginPagePath string
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

	// Login and authentication routes (always public)
	r.Get("/login", handleLoginPage(cfg))
	r.Get("/logout", func(w http.ResponseWriter, req *http.Request) {
		if cfg.SessionManager != nil {
			cfg.SessionManager.HandleLogout(w, req)
		} else {
			http.Redirect(w, req, "/admin/", http.StatusFound)
		}
	})
	r.Get("/api/auth/status", func(w http.ResponseWriter, req *http.Request) {
		if cfg.SessionManager != nil {
			cfg.SessionManager.HandleAuthStatus(w, req)
		} else {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"authenticated":true,"message":"auth disabled"}`))
		}
	})

	// Admin panel routes - protected if auth enabled
	if cfg.EnableAdminAuth && cfg.SessionManager != nil {
		// Protected admin routes
		r.Group(func(r chi.Router) {
			r.Use(cfg.SessionManager.AdminAuthMiddleware)
			r.Handle("/admin/*", http.StripPrefix("/admin/", http.FileServer(http.Dir(staticDir))))
			r.Get("/admin", func(w http.ResponseWriter, req *http.Request) {
				http.Redirect(w, req, "/admin/", http.StatusMovedPermanently)
			})
		})

		// Protected admin API routes
		r.Route("/api/admin", func(r chi.Router) {
			r.Use(cfg.SessionManager.AdminAuthMiddleware)
			r.Post("/stream/start", h.handleStreamStart)
			r.Post("/stream/stop", h.handleStreamStop)
			r.Post("/player/batch", h.handlePlayerBatchJoin)
		})
	} else {
		// Unprotected admin routes (default behavior)
		r.Handle("/admin/*", http.StripPrefix("/admin/", http.FileServer(http.Dir(staticDir))))
		r.Get("/admin", func(w http.ResponseWriter, req *http.Request) {
			http.Redirect(w, req, "/admin/", http.StatusMovedPermanently)
		})
	}

	// Default route
	r.Get("/", func(w http.ResponseWriter, req *http.Request) {
		http.Redirect(w, req, "/admin/", http.StatusFound)
	})

	return r
}

// handleLoginPage returns the login page handler
func handleLoginPage(cfg RouterConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Check if already logged in
		if cfg.SessionManager != nil {
			session := cfg.SessionManager.ValidateSession(r)
			if session != nil {
				http.Redirect(w, r, "/admin/", http.StatusFound)
				return
			}
		}

		// Serve login page
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(loginPageHTML))
	}
}

// loginPageHTML is the embedded login page
const loginPageHTML = `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Fight Club - Admin Login</title>
    <style>
        * {
            margin: 0;
            padding: 0;
            box-sizing: border-box;
        }
        body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
            background: linear-gradient(135deg, #1a1a2e 0%, #16213e 50%, #0f3460 100%);
            min-height: 100vh;
            display: flex;
            align-items: center;
            justify-content: center;
            color: #fff;
        }
        .login-container {
            background: rgba(255, 255, 255, 0.05);
            backdrop-filter: blur(10px);
            border-radius: 20px;
            padding: 40px;
            width: 100%;
            max-width: 400px;
            border: 1px solid rgba(255, 255, 255, 0.1);
            box-shadow: 0 25px 50px rgba(0, 0, 0, 0.3);
        }
        .logo {
            text-align: center;
            margin-bottom: 30px;
        }
        .logo h1 {
            font-size: 2.5rem;
            background: linear-gradient(135deg, #4ecdc4, #44a08d);
            -webkit-background-clip: text;
            -webkit-text-fill-color: transparent;
            background-clip: text;
        }
        .logo p {
            color: #888;
            margin-top: 5px;
        }
        .login-btn {
            display: flex;
            align-items: center;
            justify-content: center;
            gap: 12px;
            width: 100%;
            padding: 16px 24px;
            background: linear-gradient(135deg, #53fc18 0%, #3db912 100%);
            color: #000;
            border: none;
            border-radius: 12px;
            font-size: 1.1rem;
            font-weight: 600;
            cursor: pointer;
            transition: transform 0.2s, box-shadow 0.2s;
        }
        .login-btn:hover {
            transform: translateY(-2px);
            box-shadow: 0 10px 30px rgba(83, 252, 24, 0.3);
        }
        .login-btn svg {
            width: 24px;
            height: 24px;
        }
        .info {
            margin-top: 24px;
            padding: 16px;
            background: rgba(255, 255, 255, 0.05);
            border-radius: 10px;
            font-size: 0.9rem;
            color: #aaa;
            text-align: center;
        }
        .info strong {
            color: #4ecdc4;
        }
        .error-msg {
            background: rgba(255, 82, 82, 0.2);
            border: 1px solid rgba(255, 82, 82, 0.3);
            color: #ff5252;
            padding: 12px;
            border-radius: 8px;
            margin-bottom: 20px;
            text-align: center;
        }
        .kick-logo {
            fill: currentColor;
        }
    </style>
</head>
<body>
    <div class="login-container">
        <div class="logo">
            <h1>Fight Club</h1>
            <p>Admin Panel</p>
        </div>

        <div id="error" class="error-msg" style="display: none;"></div>

        <button class="login-btn" onclick="loginWithKick()">
            <svg class="kick-logo" viewBox="0 0 24 24" xmlns="http://www.w3.org/2000/svg">
                <path d="M12 2C6.48 2 2 6.48 2 12s4.48 10 10 10 10-4.48 10-10S17.52 2 12 2zm-1 14H9V8h2v8zm4 0h-2l-2-4v4h-2V8h2l2 4V8h2v8z"/>
            </svg>
            Login with Kick
        </button>

        <div class="info">
            <strong>Broadcaster Only</strong><br>
            Only the channel owner can access the admin panel.
        </div>
    </div>

    <script>
        // Check for error in URL
        const params = new URLSearchParams(window.location.search);
        if (params.get('error') === 'unauthorized') {
            document.getElementById('error').textContent = 'Access denied. Only the broadcaster can access the admin panel.';
            document.getElementById('error').style.display = 'block';
        }

        function loginWithKick() {
            // Open Kick OAuth in a popup
            const width = 600;
            const height = 700;
            const left = (screen.width - width) / 2;
            const top = (screen.height - height) / 2;

            const popup = window.open(
                '/api/kick/auth',
                'Kick Login',
                'width=' + width + ',height=' + height + ',left=' + left + ',top=' + top
            );

            // Listen for auth success message
            window.addEventListener('message', function(event) {
                if (event.data && event.data.type === 'kick-auth-success') {
                    // Redirect to admin panel
                    window.location.href = '/admin/';
                }
            });
        }
    </script>
</body>
</html>
`

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
