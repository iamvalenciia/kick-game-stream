package api

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"fight-club/internal/game"
	"fight-club/internal/streaming"

	"github.com/gorilla/websocket"
)

const (
	// MaxWSConnectionsTotal is the maximum number of WebSocket connections allowed
	MaxWSConnectionsTotal = 500

	// MaxWSConnectionsPerIP is the maximum WebSocket connections per IP
	MaxWSConnectionsPerIP = 10
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")

		// Use the centralized origin checker
		if IsAllowedOrigin(origin) {
			return true
		}

		// Log rejected origin for security monitoring
		log.Printf("⚠️ WebSocket connection rejected from origin: %s", origin)
		RecordConnectionRejected("origin")
		return false
	},
}

// wsClient tracks a WebSocket connection with its source IP
type wsClient struct {
	conn *websocket.Conn
	ip   string
}

// WebSocketHub manages all WebSocket connections with DoS protection
type WebSocketHub struct {
	clients    map[*websocket.Conn]*wsClient
	broadcast  chan []byte
	register   chan *wsClient
	unregister chan *websocket.Conn
	mu         sync.RWMutex

	// Connection limiting per IP
	wsLimiter *WebSocketRateLimiter
}

// NewWebSocketHub creates a new hub with connection limiting
func NewWebSocketHub() *WebSocketHub {
	return &WebSocketHub{
		clients:    make(map[*websocket.Conn]*wsClient),
		broadcast:  make(chan []byte, 256),
		register:   make(chan *wsClient),
		unregister: make(chan *websocket.Conn),
		wsLimiter:  NewWebSocketRateLimiter(MaxWSConnectionsPerIP),
	}
}

// Run starts the hub
func (h *WebSocketHub) Run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client.conn] = client
			h.mu.Unlock()

			count := len(h.clients)
			log.Printf("📱 Client connected from %s (%d total)", client.ip, count)
			UpdateWSConnections(count)

		case conn := <-h.unregister:
			h.mu.Lock()
			if client, ok := h.clients[conn]; ok {
				// Release the connection slot for this IP
				h.wsLimiter.Release(client.ip)
				delete(h.clients, conn)
				conn.Close()
			}
			h.mu.Unlock()

			count := len(h.clients)
			log.Printf("📱 Client disconnected (%d remaining)", count)
			UpdateWSConnections(count)

		case message := <-h.broadcast:
			h.mu.RLock()
			for conn := range h.clients {
				err := conn.WriteMessage(websocket.TextMessage, message)
				if err != nil {
					conn.Close()
					h.mu.RUnlock()
					h.mu.Lock()
					if client, ok := h.clients[conn]; ok {
						h.wsLimiter.Release(client.ip)
						delete(h.clients, conn)
					}
					h.mu.Unlock()
					h.mu.RLock()
				}
			}
			h.mu.RUnlock()
			IncrementWSMessages()
		}
	}
}

// Broadcast sends a message to all connected clients
func (h *WebSocketHub) Broadcast(event string, data interface{}) {
	msg := map[string]interface{}{
		"event": event,
		"data":  data,
	}

	jsonBytes, err := json.Marshal(msg)
	if err != nil {
		return
	}

	select {
	case h.broadcast <- jsonBytes:
	default:
		// Channel full, skip (backpressure)
	}
}

// ClientCount returns the number of connected clients
func (h *WebSocketHub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

// StartBroadcastLoop starts broadcasting game state periodically
func (h *WebSocketHub) StartBroadcastLoop(engine *game.Engine, streamer *streaming.StreamManager) {
	ticker := time.NewTicker(100 * time.Millisecond) // 10 updates per second

	go func() {
		for range ticker.C {
			if h.ClientCount() == 0 {
				continue
			}

			state := engine.GetState()

			// Convert players to JSON-friendly format
			players := make([]map[string]interface{}, 0)
			for _, p := range state.Players {
				players = append(players, p.ToJSON())
			}

			// Broadcast game state
			h.Broadcast("game:state", map[string]interface{}{
				"players":     players,
				"playerCount": state.PlayerCount,
				"aliveCount":  state.AliveCount,
				"stats": map[string]interface{}{
					"totalKills": state.TotalKills,
				},
			})

			// Broadcast stream stats
			h.Broadcast("stream:stats", streamer.GetStats())
		}
	}()
}

// HandleWebSocket handles incoming WebSocket connections with DoS protection
func (h *WebSocketHub) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	// Get client IP for rate limiting
	ip := GetClientIP(r)

	// Check total connection limit
	h.mu.RLock()
	totalConnections := len(h.clients)
	h.mu.RUnlock()

	if totalConnections >= MaxWSConnectionsTotal {
		log.Printf("⚠️ WebSocket connection rejected: total limit reached (%d)", totalConnections)
		RecordConnectionRejected("ws_total_limit")
		http.Error(w, "Too many connections", http.StatusServiceUnavailable)
		return
	}

	// Check per-IP connection limit
	if !h.wsLimiter.Allow(ip) {
		log.Printf("⚠️ WebSocket connection rejected from %s: per-IP limit reached", ip)
		RecordConnectionRejected("ws_ip_limit")
		http.Error(w, "Too many connections from your IP", http.StatusTooManyRequests)
		return
	}

	// Upgrade to WebSocket
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		h.wsLimiter.Release(ip) // Release the slot we reserved
		return
	}

	// Register the connection
	client := &wsClient{conn: conn, ip: ip}
	h.register <- client

	// Read messages (for commands from client)
	go func() {
		defer func() {
			h.unregister <- conn
		}()

		for {
			_, message, err := conn.ReadMessage()
			if err != nil {
				break
			}

			// Parse message
			var msg map[string]interface{}
			if err := json.Unmarshal(message, &msg); err != nil {
				continue
			}

			// Handle commands (if needed)
			log.Printf("📨 WebSocket message from %s: %v", ip, msg)
		}
	}()
}
