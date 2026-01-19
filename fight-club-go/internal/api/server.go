package api

import (
	"log"
	"net/http"

	"fight-club/internal/game"

	"github.com/go-chi/chi/v5"
)

// Server is the HTTP API server with WebSocket support.
// It combines the HTTP router with WebSocket hub for real-time updates.
type Server struct {
	engine      *game.Engine
	streamer    StreamerInterface
	router      *chi.Mux
	wsHub       *WebSocketHub
	rateLimiter *IPRateLimiter
	kickHandler http.Handler
}

// NewServer creates a new API server with default production configuration.
//
// IMPORTANT: Background workers do NOT start until Start() is called.
// This enables testing by allowing the server to be constructed without
// starting goroutines or opening network listeners.
//
// For testing HTTP endpoints without WebSocket support, use NewRouter() directly.
func NewServer(engine *game.Engine, streamer StreamerInterface) *Server {
	return NewServerWithKick(engine, streamer, nil)
}

// NewServerWithKick creates a new API server with Kick OAuth support.
func NewServerWithKick(engine *game.Engine, streamer StreamerInterface, kickHandler http.Handler) *Server {
	return NewServerWithKickAndAuth(engine, streamer, kickHandler, nil, false)
}

// NewServerWithKickAndAuth creates a new API server with Kick OAuth and admin authentication support.
func NewServerWithKickAndAuth(engine *game.Engine, streamer StreamerInterface, kickHandler http.Handler, sessionMgr *SessionManager, enableAuth bool) *Server {
	s := &Server{
		engine:      engine,
		streamer:    streamer,
		wsHub:       NewWebSocketHub(),
		kickHandler: kickHandler,
	}

	// Create rate limiter (we track it for potential cleanup)
	s.rateLimiter = NewIPRateLimiter(DefaultRateLimitConfig)

	// Build router using the factory
	s.router = NewRouter(RouterConfig{
		Engine:             engine,
		Streamer:           streamer,
		RateLimiter:        s.rateLimiter,
		KickWebhookHandler: kickHandler,
		SessionManager:     sessionMgr,
		EnableAdminAuth:    enableAuth,
	})

	// Add WebSocket routes (these need the wsHub instance)
	s.setupWebSocketRoutes()

	return s
}

// setupWebSocketRoutes adds WebSocket-specific routes to the router.
// These routes need access to the wsHub instance, so they can't be
// part of the generic NewRouter factory.
func (s *Server) setupWebSocketRoutes() {
	// WebSocket endpoint (compatible with Socket.IO path)
	s.router.Get("/socket.io/", s.handleSocketIO)
	s.router.Get("/ws", s.handleWS)
}

// Start begins the HTTP server AND starts background workers.
// This is the ONLY method that starts goroutines or opens network listeners.
//
// Call this method only once. To stop the server, signal the process.
func (s *Server) Start(addr string) error {
	// Start background workers NOW, not in constructor
	// This is critical for testability - tests can construct the server
	// and use Router() without these workers running.
	go s.wsHub.Run()
	s.wsHub.StartBroadcastLoop(s.engine, s.streamer)

	log.Printf("🌐 API server starting on %s", addr)
	log.Printf("🎮 Admin Panel: http://localhost%s/admin", addr)

	return http.ListenAndServe(addr, s.router)
}

// Router returns the HTTP handler for use with httptest.
// Use this in integration tests instead of calling Start().
//
// Example:
//
//	server := api.NewServer(engine, streamer)
//	ts := httptest.NewServer(server.Router())
//	defer ts.Close()
//	resp, _ := http.Get(ts.URL + "/api/state")
func (s *Server) Router() http.Handler {
	return s.router
}

// Stop performs graceful shutdown of background workers.
// Call this before process exit to ensure clean cleanup.
func (s *Server) Stop() {
	if s.rateLimiter != nil {
		s.rateLimiter.Stop()
	}
	// Note: WebSocket hub doesn't have a Stop method yet,
	// connections will be closed when the process exits.
}

// WebSocket handlers - these need access to wsHub

func (s *Server) handleSocketIO(w http.ResponseWriter, r *http.Request) {
	// Check if this is a WebSocket upgrade request
	if r.Header.Get("Upgrade") == "websocket" {
		s.wsHub.HandleWebSocket(w, r)
		return
	}

	// For polling fallback, return 404 (we only support WebSocket)
	w.WriteHeader(http.StatusNotFound)
	w.Write([]byte(`{"error":"use websocket"}`))
}

func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	s.wsHub.HandleWebSocket(w, r)
}
