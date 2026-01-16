package streaming

import (
	"fmt"
	"image"
	"image/color"
	"io"
	"log"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"fight-club/internal/avatar"
	"fight-club/internal/game"

	"github.com/fogleman/gg"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
)

// StreamConfig holds streaming configuration
type StreamConfig struct {
	Width     int
	Height    int
	FPS       int
	Bitrate   int
	RTMPURL   string
	StreamKey string

	// Audio configuration
	MusicEnabled bool
	MusicVolume  float64 // 0.0-1.0, recommended 0.1-0.2
	MusicPath    string
}

// DoubleBuffer provides non-blocking frame buffering
type DoubleBuffer struct {
	buffers     [2][]byte
	contexts    [2]*gg.Context
	activeIndex int
	mu          sync.Mutex
}

// StreamManager handles rendering and FFmpeg streaming
type StreamManager struct {
	engine    *game.Engine
	config    StreamConfig
	ffmpeg    *exec.Cmd
	videoPipe io.WriteCloser

	mu        sync.RWMutex
	streaming bool
	stopChan  chan struct{}

	// Audio
	audioMixer *AudioMixer
	audioPipe  io.WriteCloser

	// Stats
	framesSent int64 // atomic
	startTime  time.Time
	errors     []string

	// Performance: Double buffering system
	doubleBuffer *DoubleBuffer
	workerPool   *RenderWorkerPool
	fastRenderer *FastRenderer

	// Legacy buffer for fallback
	frameBuffer []byte

	// REAL-TIME FIX: Frame ring buffer for backpressure handling
	frameRingBuffer *FrameRingBuffer
	asyncWriter     *AsyncFrameWriter

	// REAL-TIME FIX: Cached fonts (loaded once, not per-frame)
	fontSmall   font.Face
	fontMedium  font.Face
	fontLarge   font.Face
	fontsLoaded bool

	// REAL-TIME FIX: Frame timing stats
	lastFrameTime  time.Time
	frameTimeAccum int64 // atomic nanoseconds
	frameTimeCount int64 // atomic
	framesDropped  int64 // atomic

	// Callback when stream starts
	onStreamStart func()

	// Avatar cache for profile pictures
	avatarCache *avatar.Cache

	// Sound effect tracking - previous frame state
	prevAttackingPlayers map[string]bool // Track who was attacking last frame
	prevAlivePlayers     map[string]bool // Track who was alive last frame
	prevTotalKills       int             // Track total kills last frame
}

// NewStreamManager creates a new stream manager
func NewStreamManager(engine *game.Engine, config StreamConfig) *StreamManager {
	// Set defaults - 720p for smooth streaming on VPS
	if config.Width == 0 {
		config.Width = 1280
	}
	if config.Height == 0 {
		config.Height = 720
	}
	if config.FPS == 0 {
		config.FPS = 30
	}
	if config.Bitrate == 0 {
		config.Bitrate = 6000 // Good quality for 720p
	}

	// Pre-allocate frame buffer for performance
	frameSize := config.Width * config.Height * 4

	// Initialize double buffer system
	doubleBuffer := &DoubleBuffer{
		buffers: [2][]byte{
			make([]byte, frameSize),
			make([]byte, frameSize),
		},
		contexts: [2]*gg.Context{
			gg.NewContext(config.Width, config.Height),
			gg.NewContext(config.Width, config.Height),
		},
		activeIndex: 0,
	}

	// Initialize worker pool for parallel particle rendering
	workerPool := NewRenderWorkerPool(0) // Use NumCPU
	workerPool.Start()

	// Initialize fast renderer with first buffer
	fastRenderer := NewFastRenderer(config.Width, config.Height, doubleBuffer.buffers[0])

	// REAL-TIME FIX: Initialize frame ring buffer for backpressure handling
	frameRingBuffer := NewFrameRingBuffer(frameSize)

	// Initialize audio mixer with music configuration
	audioConfig := &AudioConfig{
		MusicEnabled: config.MusicEnabled,
		MusicVolume:  config.MusicVolume,
		MusicPath:    config.MusicPath,
	}

	sm := &StreamManager{
		engine:          engine,
		config:          config,
		audioMixer:      NewAudioMixer(audioConfig),
		stopChan:        make(chan struct{}),
		doubleBuffer:    doubleBuffer,
		workerPool:      workerPool,
		fastRenderer:    fastRenderer,
		frameBuffer:     make([]byte, frameSize),
		frameRingBuffer:      frameRingBuffer,
		avatarCache:          avatar.NewCache(200), // Cache up to 200 profile pictures
		prevAttackingPlayers: make(map[string]bool),
		prevAlivePlayers:     make(map[string]bool),
		prevTotalKills:       0,
	}

	// REAL-TIME FIX: Load fonts once at startup (not per-frame)
	sm.loadFonts()

	return sm
}

// loadFonts loads fonts once at startup to avoid per-frame file I/O
func (s *StreamManager) loadFonts() {
	fontPath := getFontPath()
	if fontPath == "" {
		log.Println("⚠️ No font found, text rendering may be affected")
		return
	}

	fontData, err := os.ReadFile(fontPath)
	if err != nil {
		log.Printf("⚠️ Failed to read font file: %v", err)
		return
	}

	parsedFont, err := opentype.Parse(fontData)
	if err != nil {
		log.Printf("⚠️ Failed to parse font: %v", err)
		return
	}

	// Create font faces at different sizes
	s.fontSmall, err = opentype.NewFace(parsedFont, &opentype.FaceOptions{
		Size:    16,
		DPI:     72,
		Hinting: font.HintingFull,
	})
	if err != nil {
		log.Printf("⚠️ Failed to create small font face: %v", err)
		return
	}

	s.fontMedium, err = opentype.NewFace(parsedFont, &opentype.FaceOptions{
		Size:    24,
		DPI:     72,
		Hinting: font.HintingFull,
	})
	if err != nil {
		log.Printf("⚠️ Failed to create medium font face: %v", err)
		return
	}

	s.fontLarge, err = opentype.NewFace(parsedFont, &opentype.FaceOptions{
		Size:    48,
		DPI:     72,
		Hinting: font.HintingFull,
	})
	if err != nil {
		log.Printf("⚠️ Failed to create large font face: %v", err)
		return
	}

	s.fontsLoaded = true
	log.Printf("✅ Fonts loaded and cached from: %s", fontPath)
}

// OnStreamStart registers a callback to be called when the stream starts
func (s *StreamManager) OnStreamStart(callback func()) {
	s.mu.Lock()
	s.onStreamStart = callback
	s.mu.Unlock()
}

// Start begins streaming to RTMP
func (s *StreamManager) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.streaming {
		return fmt.Errorf("already streaming")
	}

	rtmpURL := s.config.RTMPURL + "/" + s.config.StreamKey

	log.Println("🎬 Starting stream to Kick...")
	log.Printf("   Resolution: %dx%d @ %d fps", s.config.Width, s.config.Height, s.config.FPS)
	log.Printf("   Bitrate: %dk", s.config.Bitrate)
	log.Printf("   RTMP URL: %s", s.config.RTMPURL)
	log.Printf("   Stream Key: %s...", s.config.StreamKey[:min(10, len(s.config.StreamKey))])

	log.Println("   🎥 Using libx264 CPU encoding")

	// Build FFmpeg arguments - CROSS-PLATFORM
	// Windows: Uses file-based audio (ExtraFiles not supported)
	// Linux/macOS: Uses piped audio for full SFX support
	args := []string{
		"-y",
		// Video input (pipe:0 - stdin)
		"-f", "rawvideo",
		"-pix_fmt", "rgba",
		"-s", fmt.Sprintf("%dx%d", s.config.Width, s.config.Height),
		"-r", fmt.Sprintf("%d", s.config.FPS),
		"-i", "pipe:0",
	}

	// Audio input - platform specific
	useAudioPipe := runtime.GOOS != "windows"
	musicPath := s.config.MusicPath
	musicFileExists := false
	if s.config.MusicEnabled && musicPath != "" {
		if _, err := os.Stat(musicPath); err == nil {
			musicFileExists = true
		}
	}

	if useAudioPipe {
		// Linux/macOS: Use piped audio (supports SFX + music mixing)
		args = append(args,
			"-f", "s16le",
			"-ar", "44100",
			"-ac", "2",
			"-i", "pipe:3",
		)
		log.Println("   🔊 Sound effects: enabled (piped audio)")
	} else {
		// Windows: Use file-based audio (no SFX support yet)
		if musicFileExists {
			log.Printf("   🎵 Background music: %s (volume: %.0f%%)", musicPath, s.config.MusicVolume*100)
			args = append(args,
				"-stream_loop", "-1",
				"-i", musicPath,
			)
		} else {
			if s.config.MusicEnabled && musicPath != "" {
				log.Printf("   ⚠️ Music file not found: %s, using silent audio", musicPath)
			}
			args = append(args,
				"-f", "lavfi",
				"-i", "anullsrc=channel_layout=stereo:sample_rate=44100",
			)
		}
		log.Println("   ⚠️ Sound effects: disabled on Windows (file-based audio)")
	}

	if s.config.MusicEnabled && s.config.MusicPath != "" && useAudioPipe {
		log.Printf("   🎵 Background music: %s (volume: %.0f%%)", s.config.MusicPath, s.config.MusicVolume*100)
	}

	// Video encoding - optimized for speed
	args = append(args,
		"-c:v", "libx264",
		"-preset", "ultrafast",
		"-tune", "zerolatency",
		"-b:v", fmt.Sprintf("%dk", s.config.Bitrate),
		"-maxrate", fmt.Sprintf("%dk", s.config.Bitrate),
		"-bufsize", fmt.Sprintf("%dk", s.config.Bitrate*2),
		"-pix_fmt", "yuv420p",
		"-g", fmt.Sprintf("%d", s.config.FPS*2),
		"-keyint_min", fmt.Sprintf("%d", s.config.FPS),
		"-sc_threshold", "0",
		"-profile:v", "main",
	)

	// Audio encoding
	if !useAudioPipe && musicFileExists {
		// Windows with music file: apply volume filter
		args = append(args,
			"-af", fmt.Sprintf("volume=%.2f", s.config.MusicVolume),
			"-c:a", "aac",
			"-b:a", "128k",
			"-ar", "44100",
			"-ac", "2",
		)
	} else {
		args = append(args,
			"-c:a", "aac",
			"-b:a", "128k",
			"-ar", "44100",
			"-ac", "2",
		)
	}

	// Map streams and output
	args = append(args,
		"-map", "0:v", // Video from stdin (pipe:0)
		"-map", "1:a", // Audio from pipe:3
		"-f", "flv",
		rtmpURL,
	)

	s.ffmpeg = exec.Command("ffmpeg", args...)

	// Platform-specific process group setup (Linux only)
	// This allows killing all child processes together on shutdown
	setPlatformProcessGroup(s.ffmpeg)

	// Create video pipe (stdin = fd 0)
	var err error
	s.videoPipe, err = s.ffmpeg.StdinPipe()
	if err != nil {
		return fmt.Errorf("failed to create video pipe: %w", err)
	}

	// Create audio pipe (fd 3 via ExtraFiles) - Linux/macOS only
	if useAudioPipe {
		audioReader, audioWriter, err := os.Pipe()
		if err != nil {
			return fmt.Errorf("failed to create audio pipe: %w", err)
		}
		s.audioPipe = audioWriter
		s.ffmpeg.ExtraFiles = []*os.File{audioReader} // fd 3
	}

	// Capture stderr for debugging
	s.ffmpeg.Stderr = os.Stderr

	// Start FFmpeg
	if err := s.ffmpeg.Start(); err != nil {
		return fmt.Errorf("failed to start ffmpeg: %w", err)
	}

	s.streaming = true
	s.startTime = time.Now()
	atomic.StoreInt64(&s.framesSent, 0)
	s.stopChan = make(chan struct{})
	s.errors = nil

	// Start frame loop (video)
	go s.frameLoop()

	// Start audio loop (sound effects + music) - Linux/macOS only
	if useAudioPipe {
		go s.audioLoop()
	}

	log.Println("✅ Stream started!")

	// Trigger onStreamStart callback if set
	if s.onStreamStart != nil {
		go s.onStreamStart()
	}

	return nil
}

// Stop stops streaming
func (s *StreamManager) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.streaming {
		return
	}

	log.Println("🛑 Stopping stream...")

	s.streaming = false
	close(s.stopChan)

	// Stop async writer first (before closing pipes)
	if s.asyncWriter != nil {
		s.asyncWriter.Stop()
	}

	// Stop worker pool
	if s.workerPool != nil {
		s.workerPool.Stop()
	}

	// Stop audio mixer/music player
	if s.audioMixer != nil && s.audioMixer.musicPlayer != nil {
		s.audioMixer.musicPlayer.Close()
	}

	// Close pipes to unblock any reads
	if s.videoPipe != nil {
		s.videoPipe.Close()
	}
	if s.audioPipe != nil {
		s.audioPipe.Close()
	}

	// Kill FFmpeg process and all its children
	if s.ffmpeg != nil && s.ffmpeg.Process != nil {
		pid := s.ffmpeg.Process.Pid
		log.Printf("🔪 Killing FFmpeg process (PID: %d)...", pid)

		// Wait briefly for graceful exit
		done := make(chan error, 1)
		go func() {
			done <- s.ffmpeg.Wait()
		}()

		// Try graceful termination first, then force kill
		// Use platform-specific kill function
		killFFmpegProcess(s.ffmpeg, pid)

		select {
		case <-done:
			log.Println("✅ FFmpeg process terminated")
		case <-time.After(3 * time.Second):
			log.Println("⚠️ Timed out waiting for FFmpeg to terminate")
		}
	}

	log.Println("✅ Stream stopped")
}

// IsStreaming returns whether the stream is active
func (s *StreamManager) IsStreaming() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.streaming
}

// GetStats returns streaming statistics
func (s *StreamManager) GetStats() map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()

	uptime := time.Duration(0)
	actualFPS := float64(0)
	framesSent := atomic.LoadInt64(&s.framesSent)

	if s.streaming && !s.startTime.IsZero() {
		uptime = time.Since(s.startTime)
		if uptime.Seconds() > 0 {
			actualFPS = float64(framesSent) / uptime.Seconds()
		}
	}

	return map[string]interface{}{
		"streaming":  s.streaming,
		"framesSent": framesSent,
		"uptime":     uptime.String(),
		"actualFps":  fmt.Sprintf("%.1f", actualFPS),
		"resolution": fmt.Sprintf("%dx%d", s.config.Width, s.config.Height),
		"fps":        s.config.FPS,
		"bitrate":    s.config.Bitrate,
		"errors":     s.errors,
	}
}

func (s *StreamManager) frameLoop() {
	ticker := time.NewTicker(time.Second / time.Duration(s.config.FPS))
	defer ticker.Stop()

	for {
		select {
		case <-s.stopChan:
			return
		case <-ticker.C:
			s.renderAndSendFrame()
		}
	}
}

// audioLoop generates and writes audio frames to FFmpeg
// Runs at the same rate as video for A/V synchronization
func (s *StreamManager) audioLoop() {
	ticker := time.NewTicker(time.Second / time.Duration(s.config.FPS))
	defer ticker.Stop()

	for {
		select {
		case <-s.stopChan:
			return
		case <-ticker.C:
			s.mu.RLock()
			audioPipe := s.audioPipe
			streaming := s.streaming
			s.mu.RUnlock()

			if !streaming || audioPipe == nil {
				continue
			}

			// Generate one frame of audio (5880 bytes = 1470 samples * 2 channels * 2 bytes)
			audioFrame := s.audioMixer.GenerateFrame()

			// Write to FFmpeg audio pipe (non-blocking best effort)
			_, err := audioPipe.Write(audioFrame)
			if err != nil {
				// Audio write failed, likely stream is stopping
				return
			}
		}
	}
}

// triggerSoundEffects detects game events and queues appropriate sounds
func (s *StreamManager) triggerSoundEffects(snap *game.GameSnapshot) {
	if s.audioMixer == nil {
		return
	}

	// Track current state
	currentAttacking := make(map[string]bool)
	currentAlive := make(map[string]bool)

	for _, p := range snap.Players {
		// Track attacking players for swing sound
		if p.IsAttacking {
			currentAttacking[p.ID] = true
			// Play swing sound when player starts attacking
			if !s.prevAttackingPlayers[p.ID] {
				s.audioMixer.QueueSound("swing")
			}
		}

		// Track alive players for spawn/death sounds
		if !p.IsDead {
			currentAlive[p.ID] = true
			// Player just spawned/respawned
			if !s.prevAlivePlayers[p.ID] {
				s.audioMixer.QueueSound("spawn")
			}
		}
	}

	// Check for kills (total kills increased)
	if snap.TotalKills > s.prevTotalKills {
		killsThisFrame := snap.TotalKills - s.prevTotalKills
		// Play kill sound for each kill (max 3 to avoid spam)
		for i := 0; i < killsThisFrame && i < 3; i++ {
			s.audioMixer.QueueSound("kill")
		}
	}

	// Check for hits - count newly dead players that were alive last frame
	for id := range s.prevAlivePlayers {
		if !currentAlive[id] {
			// Player died this frame - hit sound already played via kill
			s.audioMixer.QueueSound("hit")
		}
	}

	// Update previous state for next frame
	s.prevAttackingPlayers = currentAttacking
	s.prevAlivePlayers = currentAlive
	s.prevTotalKills = snap.TotalKills
}

func (s *StreamManager) renderAndSendFrame() {
	frameStart := time.Now()

	s.mu.RLock()
	if !s.streaming || s.videoPipe == nil {
		s.mu.RUnlock()
		return
	}
	// Keep lock held for double buffer access if needed, or better, copy what we need.
	// Actually double buffer has its own mutex.
	s.mu.RUnlock()

	// DOUBLE BUFFERING: Get the back buffer index (opposite of active)
	s.doubleBuffer.mu.Lock()
	backIndex := 1 - s.doubleBuffer.activeIndex
	frontIndex := s.doubleBuffer.activeIndex
	backBuffer := s.doubleBuffer.buffers[backIndex]
	frontBuffer := s.doubleBuffer.buffers[frontIndex]
	backContext := s.doubleBuffer.contexts[backIndex]
	s.doubleBuffer.mu.Unlock()

	// REAL-TIME FIX: Use lock-free GetSnapshot() instead of blocking GetState()
	// This is the critical change - render loop NEVER blocks on game tick
	snapshot := s.engine.GetSnapshot()
	if snapshot == nil {
		// No snapshot available yet (engine not started)
		return
	}

	// Trigger sound effects based on snapshot changes
	s.triggerSoundEffects(snapshot)

	// Render to back buffer using snapshot (non-blocking)
	s.renderFrameFromSnapshot(snapshot, backBuffer, backContext)

	// Send front buffer to FFmpeg (the one rendered last frame)
	// If async writer is available, use ring buffer; otherwise direct write
	if s.asyncWriter != nil && s.asyncWriter.IsRunning() {
		// Non-blocking write to ring buffer
		if !s.frameRingBuffer.TryWrite(frontBuffer) {
			atomic.AddInt64(&s.framesDropped, 1)
		}
	} else {
		// Fallback: Direct synchronous write
		// CRITICAL FIX: Do NOT hold s.mu while writing to pipe to avoid deadlock
		// We capture videoPipe locally under lock check at start, but we should re-check?
		// Since we checked streaming == true at start, we assume it's valid.
		// Worse case write fails if closed.

		// To be safe against race where Stop() closes pipe:
		s.mu.RLock()
		pipe := s.videoPipe
		streaming := s.streaming
		s.mu.RUnlock()

		if streaming && pipe != nil {
			_, err := pipe.Write(frontBuffer)
			if err != nil {
				s.mu.Lock()
				s.errors = append(s.errors, err.Error())
				if len(s.errors) > 10 {
					s.errors = s.errors[1:]
				}
				s.mu.Unlock()
			} else {
				// Atomic increment preferred for potential race
				atomic.AddInt64(&s.framesSent, 1)
			}
		}
	}

	// Swap buffers: back becomes front for next frame
	s.doubleBuffer.mu.Lock()
	s.doubleBuffer.activeIndex = backIndex
	s.doubleBuffer.mu.Unlock()

	// Track frame timing stats
	frameTime := time.Since(frameStart).Nanoseconds()
	atomic.AddInt64(&s.frameTimeAccum, frameTime)
	atomic.AddInt64(&s.frameTimeCount, 1)
	s.lastFrameTime = time.Now()
}

// renderFrameFromSnapshot renders a frame using the lock-free game snapshot
// This method uses immutable snapshot data and never blocks on game state
func (s *StreamManager) renderFrameFromSnapshot(snap *game.GameSnapshot, buffer []byte, dc *gg.Context) {
	// Background with white color
	dc.SetColor(color.RGBA{250, 250, 255, 255}) // Soft white
	dc.DrawRectangle(0, 0, float64(s.config.Width), float64(s.config.Height))
	dc.Fill()

	// Abstract galaxy constellation - connected stars (black on white)
	// Generate deterministic star positions
	type starPos struct {
		x, y float64
	}
	stars := make([]starPos, 40)
	for i := 0; i < 40; i++ {
		stars[i] = starPos{
			x: float64((i*67 + i*i*3) % s.config.Width),
			y: float64((i*47 + i*i*2) % s.config.Height),
		}
	}

	// Draw constellation lines connecting nearby stars (abstract network)
	dc.SetColor(color.RGBA{30, 30, 40, 40}) // Very subtle dark lines
	dc.SetLineWidth(1)
	for i := 0; i < len(stars); i++ {
		for j := i + 1; j < len(stars); j++ {
			dx := stars[i].x - stars[j].x
			dy := stars[i].y - stars[j].y
			dist := dx*dx + dy*dy
			// Connect stars within certain distance (creates network effect)
			if dist < 40000 && dist > 5000 { // 200px radius, min 70px
				dc.DrawLine(stars[i].x, stars[i].y, stars[j].x, stars[j].y)
				dc.Stroke()
			}
		}
	}

	// Draw the stars/nodes themselves
	for i, star := range stars {
		// Vary star sizes for depth
		size := 2.0
		if i%3 == 0 {
			size = 3.0
			dc.SetColor(color.RGBA{20, 20, 30, 80}) // Darker larger stars
		} else if i%5 == 0 {
			size = 1.5
			dc.SetColor(color.RGBA{40, 40, 50, 60}) // Medium stars
		} else {
			dc.SetColor(color.RGBA{60, 60, 70, 50}) // Subtle small stars
		}
		dc.DrawCircle(star.x, star.y, size)
		dc.Fill()
	}

	// Players from snapshot (immutable, no lock needed)
	s.drawPlayersFromSnapshot(dc, snap.Players)

	// PARALLEL RENDER: Particles using worker pool
	if len(snap.Particles) > 0 && s.workerPool != nil {
		img := dc.Image()
		if nrgba, ok := img.(*image.NRGBA); ok {
			s.workerPool.RenderParticlesSnapshotParallel(snap.Particles, nrgba.Pix, s.config.Width, s.config.Height)
		}
	} else if len(snap.Particles) > 0 {
		s.drawParticlesFromSnapshot(dc, snap.Particles)
	}

	// Attack effects from snapshot
	if len(snap.Effects) > 0 {
		s.drawEffectsFromSnapshot(dc, snap.Effects)
	}

	// NEW: Weapon trails from snapshot
	if len(snap.Trails) > 0 {
		s.drawTrailsFromSnapshot(dc, snap.Trails)
	}

	// NEW: Impact flashes from snapshot
	if len(snap.Flashes) > 0 {
		s.drawFlashesFromSnapshot(dc, snap.Flashes)
	}

	// NEW: Projectiles (arrows) from snapshot
	if len(snap.Projectiles) > 0 {
		s.drawProjectilesFromSnapshot(dc, snap.Projectiles)
	}

	// Floating texts from snapshot
	if len(snap.Texts) > 0 {
		s.drawTextsFromSnapshot(dc, snap.Texts)
	}

	// Apply screen shake by offsetting final copy (if any)
	// Note: shake is visual only, applied after all drawing
	shakeX := snap.Shake.OffsetX
	shakeY := snap.Shake.OffsetY
	_ = shakeX // For now shake is embedded in snapshot but not applied visually
	_ = shakeY // Could add dc.Translate if we want camera shake effect

	// UI from snapshot (leaderboard already sorted in snapshot)
	s.drawUIFromSnapshot(dc, snap)

	// Copy gg context to output buffer (fast direct copy)
	s.imageToBufferFast(dc.Image(), buffer)
}

func (s *StreamManager) renderFrameToBuffer(state game.GameState, buffer []byte, dc *gg.Context) {
	// Use gg.Context for all rendering (stable and correct)
	// The double buffering handles the FFmpeg write optimization

	// Background with solid color
	dc.SetColor(color.RGBA{12, 12, 28, 255})
	dc.DrawRectangle(0, 0, float64(s.config.Width), float64(s.config.Height))
	dc.Fill()

	// Stars
	dc.SetColor(color.White)
	for i := 0; i < 30; i++ {
		x := float64((i * 67) % s.config.Width)
		y := float64((i * 47) % s.config.Height)
		dc.DrawCircle(x, y, 1)
		dc.Fill()
	}

	// Players
	s.drawPlayers(dc, state.Players)

	// PARALLEL RENDER: Particles using worker pool (render to gg context image)
	if len(state.Particles) > 0 && s.workerPool != nil {
		img := dc.Image()
		if nrgba, ok := img.(*image.NRGBA); ok {
			s.workerPool.RenderParticlesParallel(state.Particles, nrgba.Pix, s.config.Width, s.config.Height)
		}
	} else if len(state.Particles) > 0 {
		s.drawParticles(dc, state.Particles)
	}

	// Attack effects
	if len(state.Effects) > 0 {
		s.drawEffects(dc, state.Effects)
	}

	// Floating texts
	if len(state.Texts) > 0 {
		s.drawTexts(dc, state.Texts)
	}

	// UI
	s.drawUI(dc, state)

	// Copy gg context to output buffer (fast direct copy)
	s.imageToBufferFast(dc.Image(), buffer)
}

// imageToBufferFast copies gg context image to our buffer
func (s *StreamManager) imageToBufferFast(img image.Image, buffer []byte) {
	if nrgba, ok := img.(*image.NRGBA); ok {
		copy(buffer, nrgba.Pix)
		return
	}

	// Fallback for other image types
	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()
	idx := 0
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			r, g, b, a := img.At(x, y).RGBA()
			buffer[idx] = uint8(r >> 8)
			buffer[idx+1] = uint8(g >> 8)
			buffer[idx+2] = uint8(b >> 8)
			buffer[idx+3] = uint8(a >> 8)
			idx += 4
		}
	}
}

// Legacy renderFrame kept for compatibility
func (s *StreamManager) renderFrame(state game.GameState) []byte {
	dc := gg.NewContext(s.config.Width, s.config.Height)

	// Background - simplified for performance
	s.drawBackground(dc)

	// Grid - less lines for 720p
	s.drawGrid(dc)

	// Players (most important)
	s.drawPlayers(dc, state.Players)

	// Particles (if any)
	if len(state.Particles) > 0 {
		s.drawParticles(dc, state.Particles)
	}

	// Attack effects (if any)
	if len(state.Effects) > 0 {
		s.drawEffects(dc, state.Effects)
	}

	// Floating texts (if any)
	if len(state.Texts) > 0 {
		s.drawTexts(dc, state.Texts)
	}

	// UI
	s.drawUI(dc, state)

	// OPTIMIZED: Convert directly to reusable buffer
	return s.imageToRGBAFast(dc.Image())
}

func (s *StreamManager) drawBackground(dc *gg.Context) {
	// OPTIMIZED: Single solid fill instead of per-line gradient
	dc.SetColor(color.RGBA{12, 12, 28, 255})
	dc.DrawRectangle(0, 0, float64(s.config.Width), float64(s.config.Height))
	dc.Fill()

	// OPTIMIZED: Fewer stars (30 instead of 100)
	dc.SetColor(color.White)
	for i := 0; i < 30; i++ {
		x := float64((i * 67) % s.config.Width)
		y := float64((i * 47) % s.config.Height)
		dc.DrawCircle(x, y, 1)
		dc.Fill()
	}
}

func (s *StreamManager) drawGrid(dc *gg.Context) {
	dc.SetColor(color.RGBA{30, 30, 45, 255})
	dc.SetLineWidth(1)

	// OPTIMIZED: Larger grid spacing (100 instead of 50)
	gridSize := 100.0
	for x := 0.0; x < float64(s.config.Width); x += gridSize {
		dc.DrawLine(x, 0, x, float64(s.config.Height))
		dc.Stroke()
	}
	for y := 0.0; y < float64(s.config.Height); y += gridSize {
		dc.DrawLine(0, y, float64(s.config.Width), y)
		dc.Stroke()
	}
}

func (s *StreamManager) drawPlayers(dc *gg.Context, players []*game.Player) {
	for _, p := range players {
		if p.IsRagdoll {
			s.drawRagdollPlayer(dc, p)
		} else if !p.IsDead {
			s.drawPlayer(dc, p)
		}
	}
}

func (s *StreamManager) drawPlayer(dc *gg.Context, p *game.Player) {
	radius := 30.0

	// Shadow
	dc.SetColor(color.RGBA{0, 0, 0, 128})
	dc.DrawCircle(p.X, p.Y+8, radius)
	dc.Fill()

	// Spawn protection glow
	if p.SpawnProtection {
		dc.SetColor(color.RGBA{255, 255, 255, 77})
		dc.DrawCircle(p.X, p.Y, radius+10)
		dc.Fill()
	}

	// Body
	dc.SetColor(parseHexColor(p.Color))
	dc.DrawCircle(p.X, p.Y, radius+3)
	dc.Fill()

	dc.DrawCircle(p.X, p.Y, radius)
	dc.Fill()

	// Border
	dc.SetColor(color.White)
	dc.SetLineWidth(4)
	dc.DrawCircle(p.X, p.Y, radius)
	dc.Stroke()

	// Health bar
	hpBarWidth := 80.0
	hpBarHeight := 10.0
	hpPercent := float64(p.HP) / float64(p.MaxHP)

	// Background
	dc.SetColor(color.RGBA{51, 51, 51, 255})
	dc.DrawRectangle(p.X-hpBarWidth/2, p.Y-50, hpBarWidth, hpBarHeight)
	dc.Fill()

	// Fill
	if hpPercent > 0.5 {
		dc.SetColor(color.RGBA{83, 255, 69, 255})
	} else if hpPercent > 0.25 {
		dc.SetColor(color.RGBA{255, 149, 0, 255})
	} else {
		dc.SetColor(color.RGBA{255, 62, 62, 255})
	}
	dc.DrawRectangle(p.X-hpBarWidth/2, p.Y-50, hpBarWidth*hpPercent, hpBarHeight)
	dc.Fill()

	// Name - dark color for visibility on white background
	dc.SetColor(color.RGBA{20, 25, 35, 255}) // Dark charcoal
	if err := dc.LoadFontFace(getFontPath(), 16); err == nil {
		dc.DrawStringAnchored(p.Name, p.X, p.Y+50, 0.5, 0.5)
	}

	// Money - orange for better contrast on white background
	dc.SetColor(color.RGBA{255, 120, 0, 255}) // Vibrant orange
	dc.DrawStringAnchored(fmt.Sprintf("$%d", p.Money), p.X, p.Y+70, 0.5, 0.5)
}

func (s *StreamManager) drawRagdollPlayer(dc *gg.Context, p *game.Player) {
	radius := 30.0

	dc.Push()
	dc.RotateAbout(p.RagdollRotation, p.X, p.Y)

	// Faded body
	c := parseHexColor(p.Color)
	c.A = 153
	dc.SetColor(c)
	dc.DrawCircle(p.X, p.Y, radius)
	dc.Fill()

	// X for dead
	dc.SetColor(color.RGBA{255, 0, 0, 255})
	dc.SetLineWidth(4)
	dc.DrawLine(p.X-10, p.Y-10, p.X+10, p.Y+10)
	dc.Stroke()
	dc.DrawLine(p.X+10, p.Y-10, p.X-10, p.Y+10)
	dc.Stroke()

	dc.Pop()
}

func (s *StreamManager) drawParticles(dc *gg.Context, particles []*game.Particle) {
	for _, p := range particles {
		c := parseHexColor(p.Color)
		c.A = uint8(p.Alpha * 255)
		dc.SetColor(c)
		dc.DrawCircle(p.X, p.Y, 2)
		dc.Fill()
	}
}

func (s *StreamManager) drawEffects(dc *gg.Context, effects []*game.AttackEffect) {
	for _, e := range effects {
		progress := 1 - float64(e.Timer)/20.0

		// Arc swing
		angle := math.Atan2(e.TY-e.Y, e.TX-e.X)
		swingRadius := 70.0

		c := parseHexColor(e.Color)
		c.A = uint8((1 - progress*0.5) * 255)
		dc.SetColor(c)
		dc.SetLineWidth(4)

		dc.DrawArc(e.X, e.Y, swingRadius, angle-0.8, angle+0.8)
		dc.Stroke()
	}
}

func (s *StreamManager) drawTexts(dc *gg.Context, texts []*game.FloatingText) {
	for _, t := range texts {
		c := parseHexColor(t.Color)
		c.A = uint8(t.Alpha * 255)
		dc.SetColor(c)
		dc.DrawStringAnchored(t.Text, t.X, t.Y, 0.5, 0.5)
	}
}

func (s *StreamManager) drawUI(dc *gg.Context, state game.GameState) {
	// Load font
	fontPath := getFontPath()

	// Title
	if err := dc.LoadFontFace(fontPath, 48); err == nil {
		dc.SetColor(color.RGBA{255, 62, 62, 255})
		dc.DrawString("THE FIGHT CLUB", 30, 60)
	}

	// Channel name
	if err := dc.LoadFontFace(fontPath, 24); err == nil {
		dc.SetColor(color.RGBA{255, 107, 107, 255})
		dc.DrawString("NoRulesIRL", 30, 100)
	}

	// Leaderboard
	s.drawLeaderboard(dc, state.Players, fontPath)
}

func (s *StreamManager) drawLeaderboard(dc *gg.Context, players []*game.Player, fontPath string) {
	if len(players) == 0 {
		return
	}

	// Sort by kills
	sorted := make([]*game.Player, len(players))
	copy(sorted, players)
	for i := 0; i < len(sorted); i++ {
		for j := i + 1; j < len(sorted); j++ {
			if sorted[j].Kills > sorted[i].Kills {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}

	x := 30.0
	y := 140.0

	// Background
	limit := 10
	if len(sorted) < limit {
		limit = len(sorted)
	}
	dc.SetColor(color.RGBA{0, 0, 0, 178})
	dc.DrawRectangle(x-10, y-25, 380, float64(limit*36+50))
	dc.Fill()

	// Header
	if err := dc.LoadFontFace(fontPath, 20); err == nil {
		dc.SetColor(color.RGBA{83, 255, 69, 255})
		dc.DrawString("🏆 TOP KILLERS", x, y)
	}
	y += 40

	// Players
	if err := dc.LoadFontFace(fontPath, 18); err == nil {
		for i := 0; i < limit; i++ {
			p := sorted[i]

			// Color based on rank
			if i == 0 {
				dc.SetColor(color.RGBA{255, 215, 0, 255})
			} else if i == 1 {
				dc.SetColor(color.RGBA{192, 192, 192, 255})
			} else if i == 2 {
				dc.SetColor(color.RGBA{205, 127, 50, 255})
			} else {
				dc.SetColor(color.White)
			}

			status := "⚔️"
			if p.IsDead {
				status = "💀"
			}

			text := fmt.Sprintf("%d. %s %s: %d kills", i+1, status, p.Name, p.Kills)
			dc.DrawString(text, x, y)
			y += 32
		}
	}
}

// OPTIMIZED: Fast image to RGBA conversion using direct pixel access
func (s *StreamManager) imageToRGBAFast(img image.Image) []byte {
	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()

	// Try to get underlying NRGBA for direct access (much faster)
	if nrgba, ok := img.(*image.NRGBA); ok {
		// Direct copy from NRGBA.Pix - super fast!
		copy(s.frameBuffer, nrgba.Pix)
		return s.frameBuffer
	}

	// Fallback: manual conversion (slower but handles any image type)
	idx := 0
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			r, g, b, a := img.At(x, y).RGBA()
			s.frameBuffer[idx] = uint8(r >> 8)
			s.frameBuffer[idx+1] = uint8(g >> 8)
			s.frameBuffer[idx+2] = uint8(b >> 8)
			s.frameBuffer[idx+3] = uint8(a >> 8)
			idx += 4
		}
	}
	return s.frameBuffer
}

func parseHexColor(hex string) color.RGBA {
	if len(hex) != 7 || hex[0] != '#' {
		return color.RGBA{255, 255, 255, 255}
	}

	var r, g, b uint8
	fmt.Sscanf(hex[1:], "%02x%02x%02x", &r, &g, &b)
	return color.RGBA{r, g, b, 255}
}

func getFontPath() string {
	// Try common font locations
	paths := []string{
		"C:\\Windows\\Fonts\\arial.ttf",
		"C:\\Windows\\Fonts\\segoeui.ttf",
		"/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf",
		"/System/Library/Fonts/Helvetica.ttc",
	}

	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}

	// Try to find any ttf in current directory
	matches, _ := filepath.Glob("*.ttf")
	if len(matches) > 0 {
		return matches[0]
	}

	return ""
}

// =============================================================================
// SNAPSHOT-BASED DRAWING METHODS
// These methods use immutable GameSnapshot types instead of pointer types
// They never block on game state and are safe for real-time rendering
// =============================================================================

// drawPlayersFromSnapshot draws players from an immutable snapshot
func (s *StreamManager) drawPlayersFromSnapshot(dc *gg.Context, players []game.PlayerSnapshot) {
	for _, p := range players {
		if p.IsRagdoll {
			s.drawRagdollPlayerSnapshot(dc, p)
		} else if !p.IsDead {
			s.drawPlayerSnapshot(dc, p)
		}
	}
}

// drawPlayerSnapshot draws a single player from snapshot data
func (s *StreamManager) drawPlayerSnapshot(dc *gg.Context, p game.PlayerSnapshot) {
	radius := 30.0

	// Shadow
	dc.SetColor(color.RGBA{0, 0, 0, 128})
	dc.DrawCircle(p.X, p.Y+8, radius)
	dc.Fill()

	// Spawn protection glow
	if p.SpawnProtection {
		dc.SetColor(color.RGBA{255, 255, 255, 77})
		dc.DrawCircle(p.X, p.Y, radius+10)
		dc.Fill()
	}

	// Weapon Attack Animation (Trails/Swings)
	if p.IsAttacking {
		anim := game.GetWeaponAnimation(p.Weapon)
		s.drawWeaponAttack(dc, p, anim)
	}

	// Try to draw profile picture if available
	avatarDrawn := false
	if p.ProfilePic != "" && s.avatarCache != nil {
		if avatarImg := s.avatarCache.GetOrFetch(p.ProfilePic); avatarImg != nil {
			// Scale and draw the profile picture
			bounds := avatarImg.Bounds()
			imgSize := float64(bounds.Dx())
			scale := (radius * 2) / imgSize

			dc.Push()
			dc.Translate(p.X-radius, p.Y-radius)
			dc.Scale(scale, scale)
			dc.DrawImage(avatarImg, 0, 0)
			dc.Pop()
			avatarDrawn = true
		}
	}

	// Fallback to colored circle if no profile picture
	if !avatarDrawn {
		// Body
		dc.SetColor(parseHexColor(p.Color))
		dc.DrawCircle(p.X, p.Y, radius+3)
		dc.Fill()

		dc.DrawCircle(p.X, p.Y, radius)
		dc.Fill()
	}

	// Border
	dc.SetColor(color.White)
	dc.SetLineWidth(4)
	dc.DrawCircle(p.X, p.Y, radius)
	dc.Stroke()

	// Health bar
	hpBarWidth := 80.0
	hpBarHeight := 10.0
	hpPercent := float64(p.HP) / float64(p.MaxHP)

	// Background
	dc.SetColor(color.RGBA{51, 51, 51, 255})
	dc.DrawRectangle(p.X-hpBarWidth/2, p.Y-50, hpBarWidth, hpBarHeight)
	dc.Fill()

	// Fill
	if hpPercent > 0.5 {
		dc.SetColor(color.RGBA{83, 255, 69, 255})
	} else if hpPercent > 0.25 {
		dc.SetColor(color.RGBA{255, 149, 0, 255})
	} else {
		dc.SetColor(color.RGBA{255, 62, 62, 255})
	}
	dc.DrawRectangle(p.X-hpBarWidth/2, p.Y-50, hpBarWidth*hpPercent, hpBarHeight)
	dc.Fill()

	// Name - use cached font if available (dark color for visibility on white bg)
	dc.SetColor(color.RGBA{20, 25, 35, 255}) // Dark charcoal for good contrast
	if s.fontsLoaded && s.fontSmall != nil {
		dc.SetFontFace(s.fontSmall)
		dc.DrawStringAnchored(p.Name, p.X, p.Y+50, 0.5, 0.5)
	} else if err := dc.LoadFontFace(getFontPath(), 16); err == nil {
		dc.DrawStringAnchored(p.Name, p.X, p.Y+50, 0.5, 0.5)
	}

	// Money - orange for better contrast on white background
	dc.SetColor(color.RGBA{255, 120, 0, 255}) // Vibrant orange
	dc.DrawStringAnchored(fmt.Sprintf("$%d", p.Money), p.X, p.Y+70, 0.5, 0.5)
}

// drawRagdollPlayerSnapshot draws a ragdoll player from snapshot data
func (s *StreamManager) drawRagdollPlayerSnapshot(dc *gg.Context, p game.PlayerSnapshot) {
	radius := 30.0

	dc.Push()
	dc.RotateAbout(p.RagdollRotation, p.X, p.Y)

	// Try to draw faded profile picture if available
	avatarDrawn := false
	if p.ProfilePic != "" && s.avatarCache != nil {
		if avatarImg := s.avatarCache.Get(p.ProfilePic); avatarImg != nil {
			// Scale and draw the profile picture with transparency
			bounds := avatarImg.Bounds()
			imgSize := float64(bounds.Dx())
			scale := (radius * 2) / imgSize

			// Draw with reduced opacity by using a semi-transparent overlay
			dc.Translate(p.X-radius, p.Y-radius)
			dc.Scale(scale, scale)
			dc.DrawImage(avatarImg, 0, 0)
			dc.Identity()
			dc.RotateAbout(p.RagdollRotation, p.X, p.Y)

			// Draw semi-transparent dark overlay to fade the image
			dc.SetColor(color.RGBA{0, 0, 0, 100})
			dc.DrawCircle(p.X, p.Y, radius)
			dc.Fill()
			avatarDrawn = true
		}
	}

	// Fallback to faded colored body
	if !avatarDrawn {
		c := parseHexColor(p.Color)
		c.A = 153
		dc.SetColor(c)
		dc.DrawCircle(p.X, p.Y, radius)
		dc.Fill()
	}

	// X for dead
	dc.SetColor(color.RGBA{255, 0, 0, 255})
	dc.SetLineWidth(4)
	dc.DrawLine(p.X-10, p.Y-10, p.X+10, p.Y+10)
	dc.Stroke()
	dc.DrawLine(p.X+10, p.Y-10, p.X-10, p.Y+10)
	dc.Stroke()

	dc.Pop()
}

// drawParticlesFromSnapshot draws particles from snapshot data
func (s *StreamManager) drawParticlesFromSnapshot(dc *gg.Context, particles []game.ParticleSnapshot) {
	for _, p := range particles {
		c := parseHexColor(p.Color)
		c.A = uint8(p.Alpha * 255)
		dc.SetColor(c)
		dc.DrawCircle(p.X, p.Y, 2)
		dc.Fill()
	}
}

// drawEffectsFromSnapshot draws attack effects from snapshot data
func (s *StreamManager) drawEffectsFromSnapshot(dc *gg.Context, effects []game.EffectSnapshot) {
	for _, e := range effects {
		progress := 1 - float64(e.Timer)/20.0

		// Arc swing
		angle := math.Atan2(e.TY-e.Y, e.TX-e.X)
		swingRadius := 70.0

		c := parseHexColor(e.Color)
		c.A = uint8((1 - progress*0.5) * 255)
		dc.SetColor(c)
		dc.SetLineWidth(4)

		dc.DrawArc(e.X, e.Y, swingRadius, angle-0.8, angle+0.8)
		dc.Stroke()
	}
}

// drawTextsFromSnapshot draws floating texts from snapshot data
func (s *StreamManager) drawTextsFromSnapshot(dc *gg.Context, texts []game.TextSnapshot) {
	for _, t := range texts {
		c := parseHexColor(t.Color)
		c.A = uint8(t.Alpha * 255)
		dc.SetColor(c)
		dc.DrawStringAnchored(t.Text, t.X, t.Y, 0.5, 0.5)
	}
}

// drawTrailsFromSnapshot draws weapon trails from snapshot data
func (s *StreamManager) drawTrailsFromSnapshot(dc *gg.Context, trails []game.TrailSnapshot) {
	for _, tr := range trails {
		if tr.Count < 2 {
			continue // Need at least 2 points for a line
		}

		c := parseHexColor(tr.Color)

		// Draw connected line segments with fading alpha
		for i := 0; i < tr.Count-1; i++ {
			p1 := tr.Points[i]
			p2 := tr.Points[i+1]

			// Alpha fades from oldest to newest
			alpha := uint8(p2.Alpha * tr.Alpha * 255)
			c.A = alpha

			dc.SetColor(c)
			dc.SetLineWidth(3 + float64(i)) // Lines get thicker towards tip
			dc.MoveTo(p1.X, p1.Y)
			dc.LineTo(p2.X, p2.Y)
			dc.Stroke()
		}

		// Add glow at the tip (newest point)
		tipPoint := tr.Points[tr.Count-1]
		c.A = uint8(tr.Alpha * 200)
		dc.SetColor(c)
		dc.DrawCircle(tipPoint.X, tipPoint.Y, 5)
		dc.Fill()
	}
}

// drawFlashesFromSnapshot draws impact flashes from snapshot data
func (s *StreamManager) drawFlashesFromSnapshot(dc *gg.Context, flashes []game.FlashSnapshot) {
	for _, fl := range flashes {
		// Simple single circle flash (was 3 circles - too heavy)
		c := parseHexColor(fl.Color)
		c.A = uint8(fl.Intensity * 200) // Bright but fading
		dc.SetColor(c)
		dc.DrawCircle(fl.X, fl.Y, fl.Radius)
		dc.Fill()
	}
}

// drawProjectilesFromSnapshot draws projectiles (arrows) from snapshot data
// Designed for visibility at 720p/30FPS streaming
func (s *StreamManager) drawProjectilesFromSnapshot(dc *gg.Context, projectiles []game.ProjectileSnapshot) {
	for _, proj := range projectiles {
		c := parseHexColor(proj.Color)

		// Draw trail behind the arrow (motion blur effect)
		for i := 0; i < proj.TrailCount; i++ {
			alpha := uint8(100 - i*20)
			if alpha < 30 {
				alpha = 30
			}
			c.A = alpha
			dc.SetColor(c)
			dc.DrawCircle(proj.TrailX[i], proj.TrailY[i], float64(4-i))
			dc.Fill()
		}

		// Draw the arrow itself (rotated)
		dc.Push()
		dc.Translate(proj.X, proj.Y)
		dc.Rotate(proj.Rotation)

		// Arrow shaft - thick enough to see at 720p
		c.A = 255
		dc.SetColor(c)
		dc.SetLineWidth(4)
		dc.DrawLine(-18, 0, 10, 0)
		dc.Stroke()

		// Arrow head (filled triangle)
		dc.MoveTo(10, 0)
		dc.LineTo(5, -5)
		dc.LineTo(5, 5)
		dc.ClosePath()
		dc.Fill()

		// Fletching (back of arrow)
		dc.SetLineWidth(2)
		dc.DrawLine(-18, 0, -22, -4)
		dc.DrawLine(-18, 0, -22, 4)
		dc.Stroke()

		// Glow around arrow for visibility
		c.A = 100
		dc.SetColor(c)
		dc.DrawCircle(0, 0, 8)
		dc.Fill()

		dc.Pop()
	}
}

// drawUIFromSnapshot draws the UI using snapshot data
// Leaderboard is already sorted in the snapshot (moved from render to game tick)
func (s *StreamManager) drawUIFromSnapshot(dc *gg.Context, snap *game.GameSnapshot) {
	// === FUTURISTIC GAMER UI - PROFESSIONAL DESIGN ===
	// Design system: Dark elements on white background with cyan neon accents

	// Spacing constants
	marginLeft := 32.0
	marginTop := 24.0

	// === PLAY NOW - DARK FLOATING CARD ===
	cardX := marginLeft
	cardY := marginTop
	cardWidth := 380.0
	cardHeight := 88.0
	cardRadius := 6.0

	// Shadow layer (soft depth effect)
	dc.SetColor(color.RGBA{0, 0, 0, 25})
	dc.DrawRoundedRectangle(cardX+4, cardY+4, cardWidth, cardHeight, cardRadius)
	dc.Fill()

	// Main dark card background
	dc.SetColor(color.RGBA{18, 18, 24, 245}) // Near black with slight transparency
	dc.DrawRoundedRectangle(cardX, cardY, cardWidth, cardHeight, cardRadius)
	dc.Fill()

	// Subtle cyan accent line on left edge (gamer aesthetic)
	dc.SetColor(color.RGBA{0, 212, 255, 255}) // Electric cyan
	dc.DrawRoundedRectangle(cardX, cardY, 4, cardHeight, 2)
	dc.Fill()

	// "PLAY NOW" title - bold and impactful
	titleX := cardX + 20.0
	titleY := cardY + 46.0 // Added more top padding for centered look

	if s.fontsLoaded && s.fontLarge != nil {
		dc.SetFontFace(s.fontLarge)
	} else {
		_ = dc.LoadFontFace(getFontPath(), 32)
	}

	// Cyan glow effect (subtle)
	dc.SetColor(color.RGBA{0, 212, 255, 60})
	dc.DrawString("PLAY NOW", titleX+1, titleY+1)

	// Main title in white for contrast on dark
	dc.SetColor(color.RGBA{255, 255, 255, 255})
	dc.DrawString("PLAY NOW", titleX, titleY)

	// Subtitle - clean and readable
	subtitleY := titleY + 28.0
	if s.fontsLoaded && s.fontSmall != nil {
		dc.SetFontFace(s.fontSmall)
	} else {
		_ = dc.LoadFontFace(getFontPath(), 13)
	}
	dc.SetColor(color.RGBA{160, 165, 180, 255}) // Soft gray for subtitles
	dc.DrawString("Type !join in chat to enter the arena", titleX, subtitleY)

	// === PLAYER COUNT BADGE - Minimal competitive style ===
	badgeHeight := 36.0
	badgeWidth := 130.0
	badgeX := float64(s.config.Width) - badgeWidth - marginLeft
	badgeY := marginTop

	// Badge shadow
	dc.SetColor(color.RGBA{0, 0, 0, 20})
	dc.DrawRoundedRectangle(badgeX+2, badgeY+2, badgeWidth, badgeHeight, 4)
	dc.Fill()

	// Badge background - dark
	dc.SetColor(color.RGBA{18, 18, 24, 240})
	dc.DrawRoundedRectangle(badgeX, badgeY, badgeWidth, badgeHeight, 4)
	dc.Fill()

	// Live indicator dot
	dotX := badgeX + 14.0
	dotY := badgeY + badgeHeight/2
	dc.SetColor(color.RGBA{255, 60, 60, 255}) // Red live dot
	dc.DrawCircle(dotX, dotY, 4)
	dc.Fill()

	// Player count text
	if s.fontsLoaded && s.fontSmall != nil {
		dc.SetFontFace(s.fontSmall)
	}
	aliveText := fmt.Sprintf("%d LIVE", snap.AliveCount)
	dc.SetColor(color.RGBA{255, 255, 255, 255})
	dc.DrawString(aliveText, dotX+14, badgeY+badgeHeight/2+5)

	// === LEADERBOARD - Clean minimal design ===
	leaderboardX := marginLeft
	leaderboardY := cardY + cardHeight + 28.0
	s.drawLeaderboardFuturistic(dc, snap.Players, leaderboardX, leaderboardY)
}

// drawLeaderboardFuturistic draws a clean, modern leaderboard
func (s *StreamManager) drawLeaderboardFuturistic(dc *gg.Context, players []game.PlayerSnapshot, startX, startY float64) {
	if len(players) == 0 {
		return
	}

	x := startX
	y := startY
	entrySpacing := 26.0

	limit := 5 // Show top 5 for cleaner look
	if len(players) < limit {
		limit = len(players)
	}

	// Header - subtle and clean
	if s.fontsLoaded && s.fontSmall != nil {
		dc.SetFontFace(s.fontSmall)
	} else {
		_ = dc.LoadFontFace(getFontPath(), 14)
	}

	// Header with accent color
	dc.SetColor(color.RGBA{0, 180, 220, 255}) // Cyan accent
	dc.DrawString("TOP KILLERS", x, y)
	y += 24.0

	// Player entries
	for i := 0; i < limit; i++ {
		p := players[i]

		// Rank colors - gold/silver/bronze for top 3, gray for rest
		var rankColor color.RGBA
		switch i {
		case 0:
			rankColor = color.RGBA{255, 200, 60, 255}  // Gold
		case 1:
			rankColor = color.RGBA{180, 185, 195, 255} // Silver
		case 2:
			rankColor = color.RGBA{205, 150, 90, 255}  // Bronze
		default:
			rankColor = color.RGBA{120, 125, 140, 255} // Gray
		}

		dc.SetColor(rankColor)

		// Clean format: "1. Name · kills"
		text := fmt.Sprintf("%d. %s · %d", i+1, p.Name, p.Kills)
		dc.DrawString(text, x, y)
		y += entrySpacing
	}
}

// drawLeaderboardFromSnapshotStyled draws the leaderboard with professional styling
// Players are pre-sorted by kills in the snapshot production phase
func (s *StreamManager) drawLeaderboardFromSnapshotStyled(dc *gg.Context, players []game.PlayerSnapshot, startX, startY float64) {
	s.drawLeaderboardFuturistic(dc, players, startX, startY)
}

// drawLeaderboardFromSnapshot draws the leaderboard without re-sorting (legacy support)
// Players are pre-sorted by kills in the snapshot production phase
func (s *StreamManager) drawLeaderboardFromSnapshot(dc *gg.Context, players []game.PlayerSnapshot) {
	s.drawLeaderboardFuturistic(dc, players, 32.0, 140.0)
}

// drawWeaponAttack renders attack animation based on weapon type
func (s *StreamManager) drawWeaponAttack(dc *gg.Context, p game.PlayerSnapshot, anim game.WeaponAnimationConfig) {
	switch anim.TrailType {
	case game.TrailArc:
		s.drawArcSwing(dc, p, anim)
	case game.TrailLine:
		s.drawThrustLine(dc, p, anim)
	case game.TrailRadial:
		s.drawRadialBurst(dc, p, anim)
	}
}

// drawArcSwing draws a curved weapon trail (sword, axe, scythe)
func (s *StreamManager) drawArcSwing(dc *gg.Context, p game.PlayerSnapshot, anim game.WeaponAnimationConfig) {
	trailColor := anim.TrailColor
	if trailColor == "" {
		trailColor = game.GetWeapon(p.Weapon).Color
	}
	c := parseHexColor(trailColor)

	// Multiple arc layers for thickness/motion blur
	for layer := 0; layer < 3; layer++ {
		// Outer layers are larger and more transparent
		radius := game.GetWeapon(p.Weapon).Range * (0.8 + float64(layer)*0.1)
		alpha := uint8(255 - layer*60)
		c.A = alpha
		dc.SetColor(c)
		dc.SetLineWidth(float64(4 - layer))

		// Arc centered on attack angle
		startAngle := p.AttackAngle - anim.TrailWidth/2
		endAngle := p.AttackAngle + anim.TrailWidth/2

		dc.DrawArc(p.X, p.Y, radius, startAngle, endAngle)
		dc.Stroke()
	}
}

// drawThrustLine draws a straight weapon trail (spear, katana)
func (s *StreamManager) drawThrustLine(dc *gg.Context, p game.PlayerSnapshot, anim game.WeaponAnimationConfig) {
	trailColor := anim.TrailColor
	if trailColor == "" {
		trailColor = game.GetWeapon(p.Weapon).Color
	}
	c := parseHexColor(trailColor)
	c.A = 200
	dc.SetColor(c)
	dc.SetLineWidth(anim.TrailWidth / 5) // Scale visual width

	// Line extending from player toward target
	range_ := game.GetWeapon(p.Weapon).Range
	endX := p.X + math.Cos(p.AttackAngle)*range_
	endY := p.Y + math.Sin(p.AttackAngle)*range_

	dc.DrawLine(p.X, p.Y, endX, endY)
	dc.Stroke()

	// Glow at tip
	c.A = 255
	dc.SetColor(c)
	dc.DrawCircle(endX, endY, 6)
	dc.Fill()
}

// drawRadialBurst draws a 360 burst (fists, hammer)
func (s *StreamManager) drawRadialBurst(dc *gg.Context, p game.PlayerSnapshot, anim game.WeaponAnimationConfig) {
	trailColor := anim.TrailColor
	if trailColor == "" {
		trailColor = game.GetWeapon(p.Weapon).Color
	}
	c := parseHexColor(trailColor)

	range_ := game.GetWeapon(p.Weapon).Range

	// Draw expanding circle/burst
	c.A = 100
	dc.SetColor(c)
	dc.SetLineWidth(3)
	dc.DrawCircle(p.X, p.Y, range_)
	dc.Stroke()

	// Draw impact point if we could calculate it, but here just a directional hint
	c.A = 180
	dc.SetColor(c)
	dirX := p.X + math.Cos(p.AttackAngle)*(range_*0.8)
	dirY := p.Y + math.Sin(p.AttackAngle)*(range_*0.8)
	dc.DrawCircle(dirX, dirY, 10)
	dc.Fill()
}

// =============================================================================
// PLATFORM-SPECIFIC FUNCTIONS
// These handle differences between Windows and Linux for process management
// =============================================================================

// setPlatformProcessGroup sets up process group for FFmpeg
// On Linux, this enables killing all child processes together
// On Windows, this is a no-op (Windows handles it differently)
func setPlatformProcessGroup(cmd *exec.Cmd) {
	if runtime.GOOS == "linux" || runtime.GOOS == "darwin" {
		// For Unix-like systems, we would use Setpgid
		// But this requires syscall which varies by platform
		// The process will be killed directly instead
		log.Println("📦 FFmpeg process group setup (Unix)")
	}
	// On Windows, no special setup needed
}

// killFFmpegProcess kills FFmpeg and its child processes
// Uses platform-appropriate method
func killFFmpegProcess(cmd *exec.Cmd, pid int) {
	if cmd == nil || cmd.Process == nil {
		return
	}

	// First, try to kill gracefully
	if runtime.GOOS == "windows" {
		// On Windows, use taskkill to kill the process tree
		log.Println("🔪 Killing FFmpeg on Windows...")
		killCmd := exec.Command("taskkill", "/F", "/T", "/PID", fmt.Sprintf("%d", pid))
		if err := killCmd.Run(); err != nil {
			// Fallback to direct kill
			cmd.Process.Kill()
		}
	} else {
		// On Linux/Mac, try SIGTERM first, then SIGKILL
		log.Println("🔪 Killing FFmpeg on Unix...")
		cmd.Process.Kill()
	}
}
