package ipc

import (
	"log"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"fight-club/internal/game"
)

// Publisher publishes game snapshots to connected streamers via Unix socket
type Publisher struct {
	socketPath string
	listener   net.Listener

	// Connected clients
	clients   map[net.Conn]struct{}
	clientsMu sync.RWMutex

	// Snapshot channel (ring buffer behavior - drop old if full)
	snapshotCh chan *game.GameSnapshot

	// Config to send to new clients
	config   ConfigMessage
	configMu sync.RWMutex

	// Stats
	clientCount   int32 // atomic
	snapshotsSent int64 // atomic
	droppedFrames int64 // atomic

	// Control
	running int32 // atomic
	stopCh  chan struct{}
	wg      sync.WaitGroup
}

// NewPublisher creates a new IPC publisher
func NewPublisher(socketPath string) *Publisher {
	if socketPath == "" {
		socketPath = DefaultSocketPath
	}

	return &Publisher{
		socketPath: socketPath,
		clients:    make(map[net.Conn]struct{}),
		snapshotCh: make(chan *game.GameSnapshot, 8), // Buffer 8 frames
		stopCh:     make(chan struct{}),
	}
}

// SetConfig sets the streaming configuration to send to new clients
func (p *Publisher) SetConfig(width, height, fps, bitrate int) {
	p.configMu.Lock()
	p.config = ConfigMessage{
		Width:   width,
		Height:  height,
		FPS:     fps,
		Bitrate: bitrate,
	}
	p.configMu.Unlock()
}

// Start starts the publisher server
func (p *Publisher) Start() error {
	if !atomic.CompareAndSwapInt32(&p.running, 0, 1) {
		return nil // Already running
	}

	listener, err := CreateListener(p.socketPath)
	if err != nil {
		atomic.StoreInt32(&p.running, 0)
		return err
	}
	p.listener = listener

	// Start accept loop
	p.wg.Add(1)
	go p.acceptLoop()

	// Start broadcast loop
	p.wg.Add(1)
	go p.broadcastLoop()

	log.Printf("📡 IPC Publisher started on %s", p.socketPath)
	return nil
}

// Stop stops the publisher
func (p *Publisher) Stop() {
	if !atomic.CompareAndSwapInt32(&p.running, 1, 0) {
		return // Not running
	}

	close(p.stopCh)

	if p.listener != nil {
		p.listener.Close()
	}

	// Close all clients
	p.clientsMu.Lock()
	for conn := range p.clients {
		conn.Close()
	}
	p.clients = make(map[net.Conn]struct{})
	p.clientsMu.Unlock()

	p.wg.Wait()

	CleanupSocket(p.socketPath)
	log.Println("📡 IPC Publisher stopped")
}

// PublishSnapshot queues a snapshot for broadcast
// This is non-blocking - drops the oldest snapshot if buffer is full
func (p *Publisher) PublishSnapshot(snapshot *game.GameSnapshot) {
	if atomic.LoadInt32(&p.running) == 0 {
		return
	}

	// Convert to IPC message
	select {
	case p.snapshotCh <- snapshot:
		// Sent successfully
	default:
		// Buffer full, drop oldest and add new
		select {
		case <-p.snapshotCh:
			atomic.AddInt64(&p.droppedFrames, 1)
		default:
		}
		select {
		case p.snapshotCh <- snapshot:
		default:
		}
	}
}

// GetStats returns publisher statistics
func (p *Publisher) GetStats() (clients int, sent int64, dropped int64) {
	return int(atomic.LoadInt32(&p.clientCount)),
		atomic.LoadInt64(&p.snapshotsSent),
		atomic.LoadInt64(&p.droppedFrames)
}

// acceptLoop accepts new client connections
func (p *Publisher) acceptLoop() {
	defer p.wg.Done()

	for atomic.LoadInt32(&p.running) == 1 {
		conn, err := p.listener.Accept()
		if err != nil {
			if atomic.LoadInt32(&p.running) == 0 {
				return // Expected during shutdown
			}
			log.Printf("⚠️ IPC accept error: %v", err)
			continue
		}

		p.addClient(conn)
	}
}

// addClient adds a new client connection
func (p *Publisher) addClient(conn net.Conn) {
	p.clientsMu.Lock()
	p.clients[conn] = struct{}{}
	p.clientsMu.Unlock()

	atomic.AddInt32(&p.clientCount, 1)
	log.Printf("✅ Streamer connected: %s (total: %d)", conn.RemoteAddr(), atomic.LoadInt32(&p.clientCount))

	// Send config to new client
	p.configMu.RLock()
	config := p.config
	p.configMu.RUnlock()

	go func() {
		conn.SetWriteDeadline(time.Now().Add(time.Second))
		if err := WriteMessage(conn, MsgTypeConfig, config); err != nil {
			log.Printf("⚠️ Failed to send config to streamer: %v", err)
		}
	}()
}

// removeClient removes a client connection
func (p *Publisher) removeClient(conn net.Conn) {
	p.clientsMu.Lock()
	if _, ok := p.clients[conn]; ok {
		delete(p.clients, conn)
		conn.Close()
		p.clientsMu.Unlock()

		count := atomic.AddInt32(&p.clientCount, -1)
		log.Printf("🔌 Streamer disconnected (remaining: %d)", count)
	} else {
		p.clientsMu.Unlock()
	}
}

// broadcastLoop broadcasts snapshots to all clients
func (p *Publisher) broadcastLoop() {
	defer p.wg.Done()

	for {
		select {
		case <-p.stopCh:
			return

		case snapshot := <-p.snapshotCh:
			p.broadcast(snapshot)
		}
	}
}

// broadcast sends a snapshot to all connected clients
func (p *Publisher) broadcast(snapshot *game.GameSnapshot) {
	msg := snapshotToMessage(snapshot)

	p.clientsMu.RLock()
	clients := make([]net.Conn, 0, len(p.clients))
	for conn := range p.clients {
		clients = append(clients, conn)
	}
	p.clientsMu.RUnlock()

	var failed []net.Conn
	for _, conn := range clients {
		conn.SetWriteDeadline(time.Now().Add(WriteTimeout))
		if err := WriteMessage(conn, MsgTypeSnapshot, msg); err != nil {
			failed = append(failed, conn)
		}
	}

	// Remove failed clients
	for _, conn := range failed {
		p.removeClient(conn)
	}

	if len(clients) > 0 && len(failed) < len(clients) {
		atomic.AddInt64(&p.snapshotsSent, 1)
	}
}

// snapshotToMessage converts a game snapshot to IPC message
func snapshotToMessage(s *game.GameSnapshot) *SnapshotMessage {
	msg := &SnapshotMessage{
		Sequence:       s.Sequence,
		Timestamp:      s.Timestamp.UnixNano(),
		TickNumber:     s.TickNumber,
		PlayerCount:    s.PlayerCount,
		AliveCount:     s.AliveCount,
		TotalKills:     s.TotalKills,
		ShakeOffsetX:   s.Shake.OffsetX,
		ShakeOffsetY:   s.Shake.OffsetY,
		ShakeIntensity: s.Shake.Intensity,
	}

	// Convert players
	msg.Players = make([]PlayerData, len(s.Players))
	for i, p := range s.Players {
		msg.Players[i] = PlayerData{
			ID:              p.ID,
			Name:            p.Name,
			X:               p.X,
			Y:               p.Y,
			VX:              p.VX,
			VY:              p.VY,
			HP:              p.HP,
			MaxHP:           p.MaxHP,
			Money:           p.Money,
			Kills:           p.Kills,
			Deaths:          p.Deaths,
			Weapon:          p.Weapon,
			Color:           p.Color,
			Avatar:          p.Avatar,
			AttackAngle:     p.AttackAngle,
			IsDead:          p.IsDead,
			IsRagdoll:       p.IsRagdoll,
			RagdollRotation: p.RagdollRotation,
			SpawnProtection: p.SpawnProtection,
			IsAttacking:     p.IsAttacking,
			ProfilePic:      p.ProfilePic,
			IsDodging:       p.IsDodging,
			DodgeDirection:  p.DodgeDirection,
			ComboCount:      p.ComboCount,
			Stamina:         p.Stamina,
		}
	}

	// Convert particles
	msg.Particles = make([]ParticleData, len(s.Particles))
	for i, p := range s.Particles {
		msg.Particles[i] = ParticleData{
			X:     p.X,
			Y:     p.Y,
			Color: p.Color,
			Alpha: p.Alpha,
		}
	}

	// Convert effects
	msg.Effects = make([]EffectData, len(s.Effects))
	for i, e := range s.Effects {
		msg.Effects[i] = EffectData{
			X:     e.X,
			Y:     e.Y,
			TX:    e.TX,
			TY:    e.TY,
			Color: e.Color,
			Timer: e.Timer,
		}
	}

	// Convert texts
	msg.Texts = make([]TextData, len(s.Texts))
	for i, t := range s.Texts {
		msg.Texts[i] = TextData{
			X:     t.X,
			Y:     t.Y,
			Text:  t.Text,
			Color: t.Color,
			Alpha: t.Alpha,
		}
	}

	// Convert trails
	msg.Trails = make([]TrailData, len(s.Trails))
	for i, tr := range s.Trails {
		td := TrailData{
			Count:    tr.Count,
			Color:    tr.Color,
			Alpha:    tr.Alpha,
			PlayerID: tr.PlayerID,
		}
		for j := 0; j < 8 && j < tr.Count; j++ {
			td.Points[j] = TrailPointData{
				X:     tr.Points[j].X,
				Y:     tr.Points[j].Y,
				Alpha: tr.Points[j].Alpha,
			}
		}
		msg.Trails[i] = td
	}

	// Convert flashes
	msg.Flashes = make([]FlashData, len(s.Flashes))
	for i, f := range s.Flashes {
		msg.Flashes[i] = FlashData{
			X:         f.X,
			Y:         f.Y,
			Radius:    f.Radius,
			Color:     f.Color,
			Intensity: f.Intensity,
		}
	}

	// Convert projectiles
	msg.Projectiles = make([]ProjectileData, len(s.Projectiles))
	for i, p := range s.Projectiles {
		msg.Projectiles[i] = ProjectileData{
			X:          p.X,
			Y:          p.Y,
			Rotation:   p.Rotation,
			Color:      p.Color,
			TrailX:     p.TrailX,
			TrailY:     p.TrailY,
			TrailCount: p.TrailCount,
		}
	}

	return msg
}
