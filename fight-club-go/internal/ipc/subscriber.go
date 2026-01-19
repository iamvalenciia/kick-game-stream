package ipc

import (
	"io"
	"log"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// Subscriber receives game snapshots from the server via Unix socket
type Subscriber struct {
	socketPath string
	conn       net.Conn
	connMu     sync.Mutex

	// Latest snapshot (lock-free access)
	latestSnapshot atomic.Value // *SnapshotMessage

	// Config received from server
	config   ConfigMessage
	configMu sync.RWMutex
	configCh chan ConfigMessage

	// Stats
	snapshotsReceived int64 // atomic
	reconnects        int64 // atomic
	errors            int64 // atomic

	// Control
	running int32 // atomic
	stopCh  chan struct{}
	wg      sync.WaitGroup

	// Callbacks
	onSnapshot   func(*SnapshotMessage)
	onConfig     func(*ConfigMessage)
	onConnect    func()
	onDisconnect func()
}

// NewSubscriber creates a new IPC subscriber
func NewSubscriber(socketPath string) *Subscriber {
	if socketPath == "" {
		socketPath = DefaultSocketPath
	}

	return &Subscriber{
		socketPath: socketPath,
		configCh:   make(chan ConfigMessage, 1),
		stopCh:     make(chan struct{}),
	}
}

// OnSnapshot sets a callback for when a snapshot is received
func (s *Subscriber) OnSnapshot(fn func(*SnapshotMessage)) {
	s.onSnapshot = fn
}

// OnConfig sets a callback for when config is received
func (s *Subscriber) OnConfig(fn func(*ConfigMessage)) {
	s.onConfig = fn
}

// OnConnect sets a callback for when connection is established
func (s *Subscriber) OnConnect(fn func()) {
	s.onConnect = fn
}

// OnDisconnect sets a callback for when connection is lost
func (s *Subscriber) OnDisconnect(fn func()) {
	s.onDisconnect = fn
}

// Start starts the subscriber, connecting to the server
func (s *Subscriber) Start() error {
	if !atomic.CompareAndSwapInt32(&s.running, 0, 1) {
		return nil // Already running
	}

	s.wg.Add(1)
	go s.connectionLoop()

	log.Printf("📡 IPC Subscriber started, connecting to %s", s.socketPath)
	return nil
}

// Stop stops the subscriber
func (s *Subscriber) Stop() {
	if !atomic.CompareAndSwapInt32(&s.running, 1, 0) {
		return // Not running
	}

	close(s.stopCh)

	s.connMu.Lock()
	if s.conn != nil {
		s.conn.Close()
	}
	s.connMu.Unlock()

	s.wg.Wait()
	log.Println("📡 IPC Subscriber stopped")
}

// GetLatestSnapshot returns the most recent snapshot (lock-free)
func (s *Subscriber) GetLatestSnapshot() *SnapshotMessage {
	if val := s.latestSnapshot.Load(); val != nil {
		return val.(*SnapshotMessage)
	}
	return nil
}

// GetConfig returns the streaming configuration
func (s *Subscriber) GetConfig() ConfigMessage {
	s.configMu.RLock()
	defer s.configMu.RUnlock()
	return s.config
}

// WaitForConfig blocks until config is received or timeout
func (s *Subscriber) WaitForConfig(timeout time.Duration) *ConfigMessage {
	select {
	case cfg := <-s.configCh:
		return &cfg
	case <-time.After(timeout):
		return nil
	case <-s.stopCh:
		return nil
	}
}

// GetStats returns subscriber statistics
func (s *Subscriber) GetStats() (received int64, reconnects int64, errors int64) {
	return atomic.LoadInt64(&s.snapshotsReceived),
		atomic.LoadInt64(&s.reconnects),
		atomic.LoadInt64(&s.errors)
}

// IsConnected returns whether the subscriber is connected
func (s *Subscriber) IsConnected() bool {
	s.connMu.Lock()
	defer s.connMu.Unlock()
	return s.conn != nil
}

// connectionLoop maintains the connection to the server
func (s *Subscriber) connectionLoop() {
	defer s.wg.Done()

	for atomic.LoadInt32(&s.running) == 1 {
		// Try to connect
		conn, err := s.connect()
		if err != nil {
			select {
			case <-s.stopCh:
				return
			case <-time.After(ReconnectDelay):
				continue
			}
		}

		// Connection established
		s.connMu.Lock()
		s.conn = conn
		s.connMu.Unlock()

		if s.onConnect != nil {
			s.onConnect()
		}

		// Read loop
		s.readLoop(conn)

		// Connection lost
		s.connMu.Lock()
		s.conn = nil
		s.connMu.Unlock()

		if s.onDisconnect != nil {
			s.onDisconnect()
		}

		atomic.AddInt64(&s.reconnects, 1)

		select {
		case <-s.stopCh:
			return
		case <-time.After(ReconnectDelay):
			// Reconnect
		}
	}
}

// connect attempts to connect to the server
func (s *Subscriber) connect() (net.Conn, error) {
	conn, err := net.DialTimeout("unix", s.socketPath, time.Second)
	if err != nil {
		return nil, err
	}

	log.Printf("✅ Connected to server at %s", s.socketPath)
	return conn, nil
}

// readLoop reads messages from the connection
func (s *Subscriber) readLoop(conn net.Conn) {
	for atomic.LoadInt32(&s.running) == 1 {
		// Set read deadline
		conn.SetReadDeadline(time.Now().Add(ReadTimeout))

		msgType, data, err := ReadMessage(conn)
		if err != nil {
			if err == io.EOF {
				log.Println("🔌 Server closed connection")
				return
			}
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				// Timeout is normal, continue
				continue
			}
			log.Printf("⚠️ IPC read error: %v", err)
			atomic.AddInt64(&s.errors, 1)
			return
		}

		switch msgType {
		case MsgTypeSnapshot:
			s.handleSnapshot(data)

		case MsgTypeConfig:
			s.handleConfig(data)

		case MsgTypePing:
			// Respond with pong
			conn.SetWriteDeadline(time.Now().Add(WriteTimeout))
			WriteMessage(conn, MsgTypePong, nil)
		}
	}
}

// handleSnapshot processes a received snapshot
func (s *Subscriber) handleSnapshot(data []byte) {
	snapshot, err := DecodeSnapshot(data)
	if err != nil {
		log.Printf("⚠️ Failed to decode snapshot: %v", err)
		atomic.AddInt64(&s.errors, 1)
		return
	}

	// Store latest snapshot (lock-free)
	s.latestSnapshot.Store(snapshot)
	atomic.AddInt64(&s.snapshotsReceived, 1)

	// Call callback if set
	if s.onSnapshot != nil {
		s.onSnapshot(snapshot)
	}
}

// handleConfig processes a received config
func (s *Subscriber) handleConfig(data []byte) {
	config, err := DecodeConfig(data)
	if err != nil {
		log.Printf("⚠️ Failed to decode config: %v", err)
		atomic.AddInt64(&s.errors, 1)
		return
	}

	s.configMu.Lock()
	s.config = *config
	s.configMu.Unlock()

	log.Printf("📺 Received stream config: %dx%d @ %d FPS, %dk bitrate",
		config.Width, config.Height, config.FPS, config.Bitrate)

	// Non-blocking send to config channel
	select {
	case s.configCh <- *config:
	default:
	}

	// Call callback if set
	if s.onConfig != nil {
		s.onConfig(config)
	}
}
