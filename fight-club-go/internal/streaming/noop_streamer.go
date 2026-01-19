// Package streaming provides video streaming functionality.
// This file contains the NoOpStreamer - a dummy streamer for server-only mode.
package streaming

// NoOpStreamer is a dummy streamer that implements StreamerInterface but does nothing.
// Use this in the game server when streaming is handled by a separate process.
// This allows the API server to function without FFmpeg dependencies.
type NoOpStreamer struct {
	// Message to return when streaming endpoints are called
	message string
}

// NewNoOpStreamer creates a new NoOpStreamer.
// The server uses this when IPC mode is enabled and streaming is handled externally.
func NewNoOpStreamer() *NoOpStreamer {
	return &NoOpStreamer{
		message: "Streaming is handled by external streamer process. Use 'go run ./cmd/streamer' to start streaming.",
	}
}

// Start returns an error indicating streaming is disabled on this process.
func (n *NoOpStreamer) Start() error {
	return nil // No-op, streaming is external
}

// Stop does nothing - streaming is handled externally.
func (n *NoOpStreamer) Stop() {
	// No-op
}

// IsStreaming always returns false - this process doesn't stream.
func (n *NoOpStreamer) IsStreaming() bool {
	return false
}

// GetStats returns a status indicating streaming is external.
func (n *NoOpStreamer) GetStats() map[string]interface{} {
	return map[string]interface{}{
		"streaming":       false,
		"mode":            "external",
		"message":         n.message,
		"framesSent":      0,
		"uptime":          "0s",
		"resolution":      "N/A",
		"fps":             0,
		"bitrate":         0,
		"connectionLost":  false,
		"reconnecting":    false,
	}
}

// OnStreamStart is a no-op - streaming callbacks are handled by external streamer.
func (n *NoOpStreamer) OnStreamStart(callback func()) {
	// No-op - external streamer handles callbacks
}
