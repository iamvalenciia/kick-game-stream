// Package config provides centralized configuration management.
// This is the SINGLE SOURCE OF TRUTH for all game and stream settings.
//
// IMPORTANT: When changing values, only modify this file.
// All other parts of the codebase should reference these values.
package config

import (
	"os"
	"strconv"
)

// =============================================================================
// VIDEO & CANVAS CONFIGURATION
// =============================================================================

// VideoConfig holds all video/canvas related settings.
// These values are shared between the game engine and the stream encoder.
type VideoConfig struct {
	Width   int // Canvas/stream width in pixels
	Height  int // Canvas/stream height in pixels
	FPS     int // Frames per second (also used for game tick rate)
	Bitrate int // Stream bitrate in kbps
}

// DefaultVideo returns the default video configuration.
// This is the SINGLE SOURCE OF TRUTH for resolution and video settings.
func DefaultVideo() VideoConfig {
	return VideoConfig{
		Width:   1280, // 720p - good balance for VPS encoding
		Height:  720,
		FPS:     24,   // Reduced from 30 - VPS CPU can't encode 30fps in realtime
		Bitrate: 4000, // kbps - reduced for faster encoding on VPS
	}
}

// VideoFromEnv returns video configuration with environment variable overrides.
// Environment variables take precedence over defaults.
func VideoFromEnv() VideoConfig {
	cfg := DefaultVideo()

	if w := getEnvInt("STREAM_WIDTH", 0); w > 0 {
		cfg.Width = w
	}
	if h := getEnvInt("STREAM_HEIGHT", 0); h > 0 {
		cfg.Height = h
	}
	if fps := getEnvInt("STREAM_FPS", 0); fps > 0 {
		cfg.FPS = fps
	}
	if br := getEnvInt("STREAM_BITRATE", 0); br > 0 {
		cfg.Bitrate = br
	}

	return cfg
}

// =============================================================================
// GAME RESOURCE LIMITS
// =============================================================================

// ResourceLimits controls DoS protection and performance limits.
type ResourceLimits struct {
	MaxTotalPlayers int // Hard cap on total connected players (logic)
	MaxPlayers      int // Hard cap on rendered players per frame
	MaxParticles    int // Per-frame particle limit
	MaxEffects      int // Per-frame effect limit
	MaxTexts        int // Per-frame floating text limit
	MaxTrails       int // Per-frame weapon trail limit
	MaxFlashes      int // Per-frame impact flash limit
	MaxProjectiles  int // Maximum active projectiles
}

// DefaultLimits returns the default resource limits.
func DefaultLimits() ResourceLimits {
	return ResourceLimits{
		MaxTotalPlayers: 1_000_000,
		MaxPlayers:      200,
		MaxParticles:    200,
		MaxEffects:      20,
		MaxTexts:        30,
		MaxTrails:       20,
		MaxFlashes:      10,
		MaxProjectiles:  30,
	}
}

// =============================================================================
// AUDIO CONFIGURATION
// =============================================================================

// AudioConfig holds audio mixer settings.
type AudioConfig struct {
	SampleRate int     // Audio sample rate in Hz
	Channels   int     // Number of audio channels (1=mono, 2=stereo)
	Volume     float64 // Master volume (0.0 to 1.0)
	Enabled    bool    // Whether audio/music is enabled
}

// DefaultAudio returns the default audio configuration.
func DefaultAudio() AudioConfig {
	return AudioConfig{
		SampleRate: 44100,
		Channels:   2, // Stereo
		Volume:     0.15,
		Enabled:    true,
	}
}

// AudioFromEnv returns audio configuration with environment variable overrides.
func AudioFromEnv() AudioConfig {
	cfg := DefaultAudio()

	if v := getEnvFloat("MUSIC_VOLUME", -1); v >= 0 {
		cfg.Volume = v
	}
	if os.Getenv("MUSIC_ENABLED") == "false" {
		cfg.Enabled = false
	}

	return cfg
}

// =============================================================================
// SERVER CONFIGURATION
// =============================================================================

// ServerConfig holds HTTP server settings.
type ServerConfig struct {
	Port       int
	MaxPlayers int
}

// DefaultServer returns the default server configuration.
func DefaultServer() ServerConfig {
	return ServerConfig{
		Port:       3000,
		MaxPlayers: 100,
	}
}

// ServerFromEnv returns server configuration with environment variable overrides.
func ServerFromEnv() ServerConfig {
	cfg := DefaultServer()

	if p := getEnvInt("PORT", 0); p > 0 {
		cfg.Port = p
	}
	if mp := getEnvInt("MAX_PLAYERS", 0); mp > 0 {
		cfg.MaxPlayers = mp
	}

	return cfg
}

// =============================================================================
// SPATIAL CONFIGURATION
// =============================================================================

// SpatialConfig holds spatial indexing settings.
type SpatialConfig struct {
	GridCellSize      int // Spatial grid cell size for collision detection
	FlowFieldCellSize int // Flow field cell size for pathfinding
}

// DefaultSpatial returns the default spatial configuration.
func DefaultSpatial() SpatialConfig {
	return SpatialConfig{
		GridCellSize:      100, // pixels
		FlowFieldCellSize: 50,  // pixels (smaller = smoother navigation)
	}
}

// =============================================================================
// COMPLETE APP CONFIGURATION
// =============================================================================

// AppConfig holds the complete application configuration.
type AppConfig struct {
	Video    VideoConfig
	Audio    AudioConfig
	Server   ServerConfig
	Limits   ResourceLimits
	Spatial  SpatialConfig
}

// Load returns the complete configuration with environment overrides.
func Load() AppConfig {
	return AppConfig{
		Video:   VideoFromEnv(),
		Audio:   AudioFromEnv(),
		Server:  ServerFromEnv(),
		Limits:  DefaultLimits(),
		Spatial: DefaultSpatial(),
	}
}

// =============================================================================
// HELPER FUNCTIONS
// =============================================================================

func getEnvInt(key string, defaultVal int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return defaultVal
}

func getEnvFloat(key string, defaultVal float64) float64 {
	if v := os.Getenv(key); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return defaultVal
}
