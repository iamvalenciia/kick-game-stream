// Standalone streamer process for fight-club
// This binary connects to the server via IPC and handles ONLY rendering + FFmpeg streaming
// This isolates the streaming from server load, ensuring smooth frames even under heavy webhook traffic
package main

import (
	"log"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"fight-club/internal/ipc"
	"fight-club/internal/streaming"

	"github.com/joho/godotenv"
)

func main() {
	// Load environment
	if err := godotenv.Load("../.env"); err != nil {
		if err := godotenv.Load(".env"); err != nil {
			log.Println("No .env file found, using environment variables")
		}
	}

	log.Println("🎬 ================================")
	log.Println("🎬  FIGHT CLUB - STREAMER")
	log.Println("🎬  Isolated Render Process")
	log.Println("🎬 ================================")

	// Configuration from environment
	socketPath := getEnvWithDefault("IPC_SOCKET", ipc.DefaultSocketPath)
	streamKey := os.Getenv("STREAM_KEY_KICK")
	rtmpURL := getEnvWithDefault("RTMP_URL", "rtmps://fa723fc1b171.global-contribute.live-video.net:443/app")

	// Video config (will be overridden by server config if received)
	width := getEnvInt("VIDEO_WIDTH", 1280)
	height := getEnvInt("VIDEO_HEIGHT", 720)
	fps := getEnvInt("VIDEO_FPS", 24)
	bitrate := getEnvInt("VIDEO_BITRATE", 4000)

	// Audio config
	musicEnabled := os.Getenv("MUSIC_ENABLED") != "false"
	musicVolume := getEnvFloat("MUSIC_VOLUME", 0.15)
	musicPath := getEnvWithDefault("MUSIC_PATH", "assets/music/digital_fight_arena.ogg")

	// Hardware encoding
	useNVENC := os.Getenv("USE_NVENC") == "true"
	forceNVENC := os.Getenv("FORCE_NVENC") == "true"

	if streamKey == "" {
		log.Println("WARNING: STREAM_KEY_KICK not set!")
	}

	log.Printf("📡 IPC Socket: %s", socketPath)
	log.Printf("🎮 Initial config: %dx%d @ %d FPS, %dk bitrate", width, height, fps, bitrate)

	// Create IPC subscriber
	subscriber := ipc.NewSubscriber(socketPath)

	// Create snapshot source from IPC
	snapshotSource := streaming.NewIPCSnapshotSource(subscriber)

	// Initial stream config (may be updated by server)
	streamConfig := streaming.StreamConfig{
		Width:        width,
		Height:       height,
		FPS:          fps,
		Bitrate:      bitrate,
		RTMPURL:      rtmpURL,
		StreamKey:    streamKey,
		MusicEnabled: musicEnabled,
		MusicVolume:  musicVolume,
		MusicPath:    musicPath,
		UseNVENC:     useNVENC,
		ForceNVENC:   forceNVENC,
	}

	// Create stream manager with IPC source
	streamer := streaming.NewStreamManagerWithSource(snapshotSource, streamConfig)

	// Track connection state
	connected := false
	var startedStream bool

	// Set up connection callbacks
	subscriber.OnConnect(func() {
		log.Println("✅ Connected to game server")
		connected = true
	})

	subscriber.OnDisconnect(func() {
		log.Println("🔌 Disconnected from game server")
		connected = false
		// Don't stop streaming immediately - IPC will reconnect
	})

	subscriber.OnConfig(func(cfg *ipc.ConfigMessage) {
		log.Printf("📺 Received config from server: %dx%d @ %d FPS", cfg.Width, cfg.Height, cfg.FPS)
		// Note: In a production system, you might want to restart the stream
		// with the new config if it differs significantly
	})

	// Start IPC subscriber
	if err := subscriber.Start(); err != nil {
		log.Fatalf("Failed to start IPC subscriber: %v", err)
	}

	// Wait for connection and first config
	log.Println("⏳ Waiting for connection to game server...")
	for i := 0; i < 30; i++ { // Wait up to 30 seconds
		if subscriber.IsConnected() {
			break
		}
		time.Sleep(time.Second)
	}

	if !subscriber.IsConnected() {
		log.Println("⚠️ Could not connect to server, starting anyway (will retry)")
	}

	// Wait for first snapshot before starting stream
	log.Println("⏳ Waiting for first game snapshot...")
	for i := 0; i < 30; i++ {
		if snapshotSource.GetSnapshot() != nil {
			break
		}
		time.Sleep(time.Second)
	}

	if snapshotSource.GetSnapshot() == nil {
		log.Println("⚠️ No snapshot received yet, starting stream anyway")
	}

	// Start streaming
	if streamKey != "" {
		log.Println("🎬 Starting stream...")
		if err := streamer.Start(); err != nil {
			log.Printf("Failed to start stream: %v", err)
		} else {
			startedStream = true
			log.Println("✅ Stream started!")
		}
	} else {
		log.Println("⚠️ No stream key provided, running in preview mode")
	}

	// Stats logging goroutine
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		for range ticker.C {
			received, reconnects, errors := subscriber.GetStats()
			seq := snapshotSource.GetSequence()
			log.Printf("📊 IPC Stats: snapshots=%d, seq=%d, reconnects=%d, errors=%d, connected=%v",
				received, seq, reconnects, errors, connected)

			stats := streamer.GetStats()
			log.Printf("📊 Stream Stats: frames=%v, uptime=%v, streaming=%v",
				stats["framesSent"], stats["uptime"], stats["streaming"])
		}
	}()

	// Wait for shutdown signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	log.Println("✅ Streamer ready! Press Ctrl+C to stop.")
	<-quit

	log.Println("🛑 Shutting down streamer...")

	if startedStream {
		streamer.Stop()
	}
	subscriber.Stop()

	log.Println("👋 Streamer stopped!")
}

func getEnvWithDefault(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

func getEnvInt(key string, defaultVal int) int {
	if val := os.Getenv(key); val != "" {
		if i, err := strconv.Atoi(val); err == nil {
			return i
		}
	}
	return defaultVal
}

func getEnvFloat(key string, defaultVal float64) float64 {
	if val := os.Getenv(key); val != "" {
		if f, err := strconv.ParseFloat(val, 64); err == nil {
			return f
		}
	}
	return defaultVal
}
