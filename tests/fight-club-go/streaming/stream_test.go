package streaming_test

import (
	"testing"

	"fight-club/internal/game"
	"fight-club/internal/streaming"
)

// TestNewStreamManager tests stream manager creation
func TestNewStreamManager(t *testing.T) {
	engine := game.NewEngine(30)

	// Test with defaults
	sm := streaming.NewStreamManager(engine, streaming.StreamConfig{})
	if sm == nil {
		t.Fatal("NewStreamManager returned nil")
	}

	// Test with custom config
	sm2 := streaming.NewStreamManager(engine, streaming.StreamConfig{
		Width:   1920,
		Height:  1080,
		FPS:     60,
		Bitrate: 6000,
	})
	if sm2 == nil {
		t.Fatal("NewStreamManager with config returned nil")
	}
}

// TestStreamManagerDefaults tests default values are set
func TestStreamManagerDefaults(t *testing.T) {
	engine := game.NewEngine(30)
	sm := streaming.NewStreamManager(engine, streaming.StreamConfig{})

	stats := sm.GetStats()

	// Check resolution defaults - now 1280x720
	if res, ok := stats["resolution"].(string); ok {
		if res != "1280x720" {
			t.Errorf("Expected default resolution '1280x720', got '%s'", res)
		}
	}
}

// TestStreamManagerStats tests stats retrieval
func TestStreamManagerStats(t *testing.T) {
	engine := game.NewEngine(30)
	sm := streaming.NewStreamManager(engine, streaming.StreamConfig{
		Width:   1280,
		Height:  720,
		FPS:     30,
		Bitrate: 3500,
	})

	stats := sm.GetStats()

	if stats == nil {
		t.Fatal("GetStats returned nil")
	}

	// Verify expected fields
	if _, ok := stats["streaming"]; !ok {
		t.Error("Stats should contain 'streaming' field")
	}
	if _, ok := stats["framesSent"]; !ok {
		t.Error("Stats should contain 'framesSent' field")
	}
	if _, ok := stats["resolution"]; !ok {
		t.Error("Stats should contain 'resolution' field")
	}
	if _, ok := stats["fps"]; !ok {
		t.Error("Stats should contain 'fps' field")
	}
	if _, ok := stats["bitrate"]; !ok {
		t.Error("Stats should contain 'bitrate' field")
	}
}

// TestIsStreaming tests streaming state
func TestIsStreaming(t *testing.T) {
	engine := game.NewEngine(30)
	sm := streaming.NewStreamManager(engine, streaming.StreamConfig{})

	// Initially not streaming
	if sm.IsStreaming() {
		t.Error("Should not be streaming initially")
	}
}

// TestStartWithoutRTMP tests start fails without RTMP config
func TestStartWithoutRTMP(t *testing.T) {
	engine := game.NewEngine(30)
	sm := streaming.NewStreamManager(engine, streaming.StreamConfig{
		RTMPURL:   "",
		StreamKey: "",
	})

	// Start should fail or return error with empty RTMP
	err := sm.Start()
	if err == nil {
		// If it started, stop it
		sm.Stop()
	}
	// We expect it to fail or at least not crash
}

// TestStopWhenNotStreaming tests stop when not streaming
func TestStopWhenNotStreaming(t *testing.T) {
	engine := game.NewEngine(30)
	sm := streaming.NewStreamManager(engine, streaming.StreamConfig{})

	// Stop when not streaming should not panic
	sm.Stop()
}

// TestStreamConfig tests StreamConfig fields
func TestStreamConfig(t *testing.T) {
	config := streaming.StreamConfig{
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

// TestMultipleStartAttempts tests double start is rejected
func TestMultipleStartAttempts(t *testing.T) {
	engine := game.NewEngine(30)
	sm := streaming.NewStreamManager(engine, streaming.StreamConfig{
		RTMPURL:   "rtmp://test.com/live",
		StreamKey: "test_key",
	})

	// First start - may fail due to no actual RTMP server
	err1 := sm.Start()

	// If first start succeeded, second should fail
	if err1 == nil {
		err2 := sm.Start()
		if err2 == nil {
			t.Error("Second start attempt should return error")
		}
		sm.Stop()
	}
}
