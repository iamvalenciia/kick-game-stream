// =============================================================================
// FIGHT CLUB - STREAMER
// =============================================================================
// This standalone process handles ONLY streaming:
// - Receives game snapshots via IPC from the game server
// - Renders frames and encodes with FFmpeg (NVENC GPU by default)
// - Streams to Kick via RTMP
//
// This separation ensures smooth streaming without interference from game logic,
// webhooks, or API requests.
//
// USAGE:
//   1. Start the game server first: go run ./cmd/server
//   2. Then start this streamer: go run ./cmd/streamer
// =============================================================================
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

	log.Println("================================")
	log.Println("  FIGHT CLUB - STREAMER")
	log.Println("  GPU Encoding (NVENC)")
	log.Println("================================")

	// IPC configuration
	socketPath := getEnvWithDefault("IPC_SOCKET", ipc.DefaultSocketPath)

	// Stream configuration - from environment
	streamKey := os.Getenv("STREAM_KEY_KICK")
	rtmpURL := getEnvWithDefault("RTMP_URL", "rtmps://fa723fc1b171.global-contribute.live-video.net:443/app")

	// Video config - will be overridden by server config when received
	width := getEnvInt("STREAM_WIDTH", 1280)
	height := getEnvInt("STREAM_HEIGHT", 720)
	fps := getEnvInt("STREAM_FPS", 24)
	bitrate := getEnvInt("STREAM_BITRATE", 4000)

	// Audio config
	musicEnabled := os.Getenv("MUSIC_ENABLED") != "false"
	musicVolume := getEnvFloat("MUSIC_VOLUME", 0.15)
	musicPath := getEnvWithDefault("MUSIC_PATH", "assets/music/digital_fight_arena.ogg")

	if streamKey == "" {
		log.Println("ERROR: STREAM_KEY_KICK not set!")
		log.Println("Set STREAM_KEY_KICK in your .env file")
		os.Exit(1)
	}

	log.Printf("IPC Socket: %s", socketPath)
	log.Printf("Video: %dx%d @ %d FPS, %dk bitrate", width, height, fps, bitrate)
	log.Printf("RTMP: %s", rtmpURL)
	log.Printf("Stream Key: %s...", streamKey[:min(15, len(streamKey))])

	// =========================================================================
	// HARDWARE ENCODING - NVENC by default
	// =========================================================================
	// We use NVIDIA NVENC GPU encoding by default for best performance.
	// This requires an NVIDIA GPU with NVENC support (GTX 600+ / RTX series).
	// The ForceNVENC flag skips the availability check - if you're sure you
	// have NVENC, this avoids a test encode on startup.
	useNVENC := true
	forceNVENC := true // Skip test, just use it
	log.Println("Hardware encoding: NVENC (NVIDIA GPU)")

	// Create IPC subscriber to receive game snapshots
	subscriber := ipc.NewSubscriber(socketPath)

	// Create snapshot source from IPC
	snapshotSource := streaming.NewIPCSnapshotSource(subscriber)

	// Stream configuration
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
		log.Println("Connected to game server")
		connected = true
	})

	subscriber.OnDisconnect(func() {
		log.Println("Disconnected from game server")
		connected = false
		// Don't stop streaming immediately - IPC will reconnect
	})

	subscriber.OnConfig(func(cfg *ipc.ConfigMessage) {
		log.Printf("Received config from server: %dx%d @ %d FPS, %dk bitrate",
			cfg.Width, cfg.Height, cfg.FPS, cfg.Bitrate)
		// Note: To apply new config, would need to restart stream
	})

	// Start IPC subscriber
	log.Println("Connecting to game server...")
	if err := subscriber.Start(); err != nil {
		log.Fatalf("Failed to start IPC subscriber: %v", err)
	}

	// Wait for connection to game server
	log.Println("Waiting for game server connection...")
	for i := 0; i < 30; i++ { // Wait up to 30 seconds
		if subscriber.IsConnected() {
			break
		}
		time.Sleep(time.Second)
	}

	if !subscriber.IsConnected() {
		log.Println("WARNING: Could not connect to game server")
		log.Println("Make sure the game server is running: go run ./cmd/server")
		log.Println("Continuing anyway (will retry connection)...")
	}

	// Wait for first snapshot before starting stream
	log.Println("Waiting for first game snapshot...")
	for i := 0; i < 30; i++ {
		if snapshotSource.GetSnapshot() != nil {
			log.Println("Received first snapshot!")
			break
		}
		time.Sleep(time.Second)
	}

	if snapshotSource.GetSnapshot() == nil {
		log.Println("WARNING: No snapshot received yet, starting stream anyway")
	}

	// Start streaming
	log.Println("Starting stream to Kick...")
	if err := streamer.Start(); err != nil {
		log.Printf("ERROR: Failed to start stream: %v", err)
		log.Println("Check that your STREAM_KEY_KICK is valid")
	} else {
		startedStream = true
		log.Println("Stream started successfully!")
	}

	// Stats logging goroutine
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		for range ticker.C {
			received, reconnects, errors := subscriber.GetStats()
			seq := snapshotSource.GetSequence()
			log.Printf("IPC: snapshots=%d, seq=%d, reconnects=%d, errors=%d, connected=%v",
				received, seq, reconnects, errors, connected)

			stats := streamer.GetStats()
			log.Printf("Stream: frames=%v, uptime=%v, streaming=%v",
				stats["framesSent"], stats["uptime"], stats["streaming"])
		}
	}()

	// Wait for shutdown signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	log.Println("")
	log.Println("Streamer ready! Press Ctrl+C to stop.")
	log.Println("")
	<-quit

	log.Println("Shutting down streamer...")

	if startedStream {
		streamer.Stop()
	}
	subscriber.Stop()

	log.Println("Streamer stopped!")
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

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
