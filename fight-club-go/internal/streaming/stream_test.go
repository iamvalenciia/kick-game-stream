package streaming

import (
	"testing"
)

// TestNewStreamManager tests stream manager creation
func TestNewStreamManager(t *testing.T) {
	// Skip - requires game package
	t.Skip("Requires game.Engine dependency")
}

// TestStreamConfig tests StreamConfig fields
func TestStreamConfig(t *testing.T) {
	config := StreamConfig{
		Width:     1920,
		Height:    1080,
		FPS:       30,
		Bitrate:   4500,
		RTMPURL:   "rtmp://test.com/live",
		StreamKey: "test_key_123",
	}

	if config.Width != 1920 {
		t.Error("Width should be 1920")
	}
	if config.Height != 1080 {
		t.Error("Height should be 1080")
	}
	if config.FPS != 30 {
		t.Error("FPS should be 30")
	}
	if config.Bitrate != 4500 {
		t.Error("Bitrate should be 4500")
	}
}
