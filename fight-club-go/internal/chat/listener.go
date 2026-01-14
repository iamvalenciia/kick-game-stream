package chat

import (
	"encoding/json"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	// Pusher WebSocket URL for Kick chat
	PusherURL = "wss://ws-us2.pusher.com/app/32cbd69e4b950bf97679?protocol=7&client=js&version=8.0.1&flash=false"

	// MaxCommandBuffer is the bounded channel size for commands
	MaxCommandBuffer = 100

	// MaxReconnects before giving up
	MaxReconnects = 10

	// ReconnectBaseDelay for exponential backoff
	ReconnectBaseDelay = 2 * time.Second

	// PingInterval for keep-alive
	PingInterval = 30 * time.Second
)

// Listener connects to Kick chat via Pusher WebSocket
type Listener struct {
	chatroomID        string
	conn              *websocket.Conn
	socketID          string
	isConnected       bool
	reconnectAttempts int

	// Output channel for commands (bounded)
	Commands chan ChatCommand

	// Shutdown
	done chan struct{}
	mu   sync.RWMutex
}

// PusherMessage represents a Pusher protocol message
type PusherMessage struct {
	Event   string `json:"event"`
	Channel string `json:"channel,omitempty"`
	Data    string `json:"data"`
}

// PusherConnectionData is the data from connection_established
type PusherConnectionData struct {
	SocketID        string `json:"socket_id"`
	ActivityTimeout int    `json:"activity_timeout"`
}

// KickChatMessageData is the parsed chat message from Kick
type KickChatMessageData struct {
	ID         string `json:"id"`
	ChatroomID int64  `json:"chatroom_id"`
	Content    string `json:"content"`
	Sender     struct {
		ID       int64  `json:"id"`
		Username string `json:"username"`
		Slug     string `json:"slug"`
		Identity struct {
			ProfilePic string `json:"profile_pic"`
		} `json:"identity"`
		ProfilePic string `json:"profile_pic"` // Alternative location
	} `json:"sender"`
}

// NewListener creates a new Kick chat listener
func NewListener(chatroomID string) *Listener {
	return &Listener{
		chatroomID: chatroomID,
		Commands:   make(chan ChatCommand, MaxCommandBuffer),
		done:       make(chan struct{}),
	}
}

// Connect establishes the WebSocket connection
func (l *Listener) Connect() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.isConnected {
		return nil
	}

	log.Printf("🔌 Connecting to Kick chat (chatroom: %s)...", l.chatroomID)

	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}

	conn, _, err := dialer.Dial(PusherURL, nil)
	if err != nil {
		return err
	}

	l.conn = conn
	l.reconnectAttempts = 0
	log.Println("✅ Connected to Kick chat WebSocket")

	return nil
}

// Run starts the main read loop (call in goroutine)
func (l *Listener) Run() {
	defer func() {
		l.mu.Lock()
		l.isConnected = false
		if l.conn != nil {
			l.conn.Close()
		}
		l.mu.Unlock()
	}()

	for {
		select {
		case <-l.done:
			log.Println("🔌 Chat listener shutting down")
			return
		default:
			l.mu.RLock()
			conn := l.conn
			l.mu.RUnlock()

			if conn == nil {
				if err := l.reconnect(); err != nil {
					log.Printf("❌ Reconnect failed: %v", err)
					time.Sleep(ReconnectBaseDelay)
					continue
				}
			}

			// Read message
			_, message, err := l.conn.ReadMessage()
			if err != nil {
				log.Printf("⚠️ Chat read error: %v", err)
				l.mu.Lock()
				l.isConnected = false
				l.conn = nil
				l.mu.Unlock()
				continue
			}

			l.handleMessage(message)
		}
	}
}

// handleMessage processes Pusher protocol messages
func (l *Listener) handleMessage(data []byte) {
	var msg PusherMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		log.Printf("⚠️ Failed to parse Pusher message: %v", err)
		return
	}

	switch msg.Event {
	case "pusher:connection_established":
		var connData PusherConnectionData
		if err := json.Unmarshal([]byte(msg.Data), &connData); err != nil {
			log.Printf("⚠️ Failed to parse connection data: %v", err)
			return
		}
		l.socketID = connData.SocketID
		l.mu.Lock()
		l.isConnected = true
		l.mu.Unlock()
		log.Printf("📡 Pusher connected (socket: %s)", l.socketID)

		// Subscribe to chatroom channel
		l.subscribe("chatrooms." + l.chatroomID + ".v2")

	case "pusher_internal:subscription_succeeded":
		log.Printf("✅ Subscribed to chatroom %s", l.chatroomID)

	case "App\\Events\\ChatMessageEvent", "ChatMessageEvent":
		l.handleChatMessage(msg.Data)

	case "pusher:ping":
		l.sendPong()

	default:
		// Ignore other events (subscribed, bans, etc.)
		if !strings.HasPrefix(msg.Event, "pusher") && msg.Event != "" {
			log.Printf("📨 Event: %s", msg.Event)
		}
	}
}

// handleChatMessage parses a chat message and emits commands
func (l *Listener) handleChatMessage(data string) {
	var chatData KickChatMessageData
	if err := json.Unmarshal([]byte(data), &chatData); err != nil {
		log.Printf("⚠️ Failed to parse chat message: %v", err)
		return
	}

	username := chatData.Sender.Username
	if username == "" {
		username = chatData.Sender.Slug
	}
	if username == "" {
		username = "Unknown"
	}

	content := strings.TrimSpace(chatData.Content)

	// Get profile pic (try both locations)
	profilePic := chatData.Sender.Identity.ProfilePic
	if profilePic == "" {
		profilePic = chatData.Sender.ProfilePic
	}

	log.Printf("💬 [%s]: %s", username, content)

	// Check for commands
	if strings.HasPrefix(content, "!") {
		parts := strings.Fields(content[1:]) // Remove ! and split
		if len(parts) == 0 {
			return
		}

		command := strings.ToLower(parts[0])
		args := parts[1:]

		cmd := ChatCommand{
			Command:    command,
			Args:       args,
			Username:   username,
			UserID:     chatData.Sender.ID,
			ProfilePic: profilePic,
			ReceivedAt: time.Now(),
		}

		// Non-blocking send to command channel
		select {
		case l.Commands <- cmd:
			log.Printf("⚡ Command from %s: !%s %v", username, command, args)
		default:
			log.Printf("⚠️ Command queue full, dropping: !%s from %s", command, username)
		}
	}
}

// subscribe sends a channel subscription message
func (l *Listener) subscribe(channel string) {
	log.Printf("📻 Subscribing to channel: %s", channel)
	l.send(PusherMessage{
		Event: "pusher:subscribe",
		Data:  `{"channel":"` + channel + `"}`,
	})
}

// sendPong responds to pusher:ping
func (l *Listener) sendPong() {
	l.send(PusherMessage{
		Event: "pusher:pong",
		Data:  "{}",
	})
}

// send writes a message to the WebSocket
func (l *Listener) send(msg PusherMessage) {
	l.mu.RLock()
	conn := l.conn
	l.mu.RUnlock()

	if conn == nil {
		return
	}

	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("⚠️ Failed to marshal message: %v", err)
		return
	}

	if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
		log.Printf("⚠️ Failed to send message: %v", err)
	}
}

// reconnect attempts to reconnect with exponential backoff
func (l *Listener) reconnect() error {
	l.mu.Lock()
	l.reconnectAttempts++
	attempt := l.reconnectAttempts
	l.mu.Unlock()

	if attempt > MaxReconnects {
		log.Printf("❌ Max reconnect attempts reached (%d)", MaxReconnects)
		return nil // Don't error, just stop trying
	}

	delay := ReconnectBaseDelay * time.Duration(1<<uint(attempt-1))
	if delay > 30*time.Second {
		delay = 30 * time.Second
	}

	log.Printf("🔄 Reconnecting to Kick chat (attempt %d/%d) in %v...", attempt, MaxReconnects, delay)
	time.Sleep(delay)

	return l.Connect()
}

// Stop gracefully shuts down the listener
func (l *Listener) Stop() {
	close(l.done)
	l.mu.Lock()
	if l.conn != nil {
		l.conn.Close()
	}
	l.mu.Unlock()
	close(l.Commands)
	log.Println("🔌 Chat listener stopped")
}

// IsConnected returns connection status
func (l *Listener) IsConnected() bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.isConnected
}
