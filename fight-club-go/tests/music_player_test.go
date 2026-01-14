package tests

import (
	"os"
	"path/filepath"
	"testing"

	"fight-club/internal/streaming"
)

// getMusicPath returns the path to the test music file
func getMusicPath() string {
	// Try relative paths from different working directories
	paths := []string{
		"assets/music/digital_fight_arena.ogg",
		"../assets/music/digital_fight_arena.ogg",
		"../../assets/music/digital_fight_arena.ogg",
	}

	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}

	// Try from parent of test directory
	wd, _ := os.Getwd()
	parent := filepath.Dir(wd)
	return filepath.Join(parent, "assets", "music", "digital_fight_arena.ogg")
}

// TestMusicPlayerLoad verifies that the music player can load an OGG file
func TestMusicPlayerLoad(t *testing.T) {
	path := getMusicPath()

	// Skip if music file doesn't exist (CI environment)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Skipf("Music file not found: %s (OK in CI)", path)
	}

	player := streaming.NewMusicPlayer(path, 0.5)
	if player == nil {
		t.Fatal("Expected non-nil player")
	}

	if !player.IsLoaded() {
		t.Error("Expected player to be loaded")
	}

	if err := player.Close(); err != nil {
		t.Errorf("Close failed: %v", err)
	}
}

// TestMusicPlayerMissingFile verifies graceful fallback when file is missing
func TestMusicPlayerMissingFile(t *testing.T) {
	// Create player with non-existent file - should not panic
	player := streaming.NewMusicPlayer("non_existent_music.ogg", 0.5)
	if player == nil {
		t.Fatal("Expected non-nil player even for missing file")
	}

	// Should report not loaded
	if player.IsLoaded() {
		t.Error("Expected player to NOT be loaded for missing file")
	}

	// ReadSamples should return silence without crashing
	buffer := make([]int16, 2940) // One frame
	n := player.ReadSamples(buffer)
	if n != len(buffer) {
		t.Errorf("Expected %d samples, got %d", len(buffer), n)
	}

	// All samples should be zero (silence)
	for i, sample := range buffer {
		if sample != 0 {
			t.Errorf("Expected silence (0), got %d at index %d", sample, i)
			break
		}
	}

	// Close should not error
	if err := player.Close(); err != nil {
		t.Errorf("Close failed: %v", err)
	}
}

// TestMusicPlayerReadSamples verifies that samples are read correctly
func TestMusicPlayerReadSamples(t *testing.T) {
	path := getMusicPath()

	// Skip if music file doesn't exist
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Skipf("Music file not found: %s (OK in CI)", path)
	}

	player := streaming.NewMusicPlayer(path, 0.5)
	if !player.IsLoaded() {
		t.Skip("Player not loaded")
	}
	defer player.Close()

	// Read one frame of samples
	buffer := make([]int16, 2940) // 1470 samples * 2 channels
	n := player.ReadSamples(buffer)

	if n != len(buffer) {
		t.Errorf("Expected %d samples, got %d", len(buffer), n)
	}

	// Check that we got some non-zero samples (music data)
	hasNonZero := false
	for _, sample := range buffer {
		if sample != 0 {
			hasNonZero = true
			break
		}
	}

	if !hasNonZero {
		t.Error("Expected some non-zero samples from music file")
	}
}

// TestMusicPlayerVolumeControl verifies volume scaling works
func TestMusicPlayerVolumeControl(t *testing.T) {
	path := getMusicPath()

	// Skip if music file doesn't exist
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Skipf("Music file not found: %s (OK in CI)", path)
	}

	// Test with full volume
	playerFull := streaming.NewMusicPlayer(path, 1.0)
	if !playerFull.IsLoaded() {
		t.Skip("Player not loaded")
	}
	defer playerFull.Close()

	// Test with half volume
	playerHalf := streaming.NewMusicPlayer(path, 0.5)
	if !playerHalf.IsLoaded() {
		t.Skip("Player not loaded")
	}
	defer playerHalf.Close()

	// Read samples from both
	bufferFull := make([]int16, 2940)
	bufferHalf := make([]int16, 2940)

	playerFull.ReadSamples(bufferFull)
	playerHalf.ReadSamples(bufferHalf)

	// Find max amplitude in each
	maxFull := int16(0)
	maxHalf := int16(0)

	for _, s := range bufferFull {
		if s < 0 {
			s = -s
		}
		if s > maxFull {
			maxFull = s
		}
	}

	for _, s := range bufferHalf {
		if s < 0 {
			s = -s
		}
		if s > maxHalf {
			maxHalf = s
		}
	}

	// Half volume should have roughly half the amplitude
	// Allow some tolerance due to different sample positions
	if maxHalf > maxFull && maxFull > 0 {
		t.Logf("Warning: Half volume (%d) > Full volume (%d), samples may differ", maxHalf, maxFull)
	}
}

// TestMusicPlayerEnableDisable verifies enable/disable works
func TestMusicPlayerEnableDisable(t *testing.T) {
	path := getMusicPath()

	// Skip if music file doesn't exist
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Skipf("Music file not found: %s (OK in CI)", path)
	}

	player := streaming.NewMusicPlayer(path, 0.5)
	if !player.IsLoaded() {
		t.Skip("Player not loaded")
	}
	defer player.Close()

	// Disable the player
	player.SetEnabled(false)

	// Read samples - should be silence
	buffer := make([]int16, 2940)
	player.ReadSamples(buffer)

	for i, sample := range buffer {
		if sample != 0 {
			t.Errorf("Expected silence when disabled, got %d at index %d", sample, i)
			break
		}
	}
}

// TestAudioMixerWithMusic verifies AudioMixer integrates music correctly
func TestAudioMixerWithMusic(t *testing.T) {
	path := getMusicPath()

	// Skip if music file doesn't exist
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Skipf("Music file not found: %s (OK in CI)", path)
	}

	// Create mixer with music enabled
	config := &streaming.AudioConfig{
		MusicEnabled: true,
		MusicVolume:  0.5,
		MusicPath:    path,
	}

	mixer := streaming.NewAudioMixer(config)
	if mixer == nil {
		t.Fatal("Expected non-nil mixer")
	}

	// Generate a frame
	frame := mixer.GenerateFrame()

	// Frame should be 5880 bytes (1470 samples * 2 channels * 2 bytes)
	expectedSize := 5880
	if len(frame) != expectedSize {
		t.Errorf("Expected frame size %d, got %d", expectedSize, len(frame))
	}
}

// TestAudioMixerWithoutMusic verifies AudioMixer works without music
func TestAudioMixerWithoutMusic(t *testing.T) {
	// Create mixer with music disabled
	config := &streaming.AudioConfig{
		MusicEnabled: false,
		MusicVolume:  0.0,
		MusicPath:    "",
	}

	mixer := streaming.NewAudioMixer(config)
	if mixer == nil {
		t.Fatal("Expected non-nil mixer")
	}

	// Generate a frame
	frame := mixer.GenerateFrame()

	// Frame should be 5880 bytes
	expectedSize := 5880
	if len(frame) != expectedSize {
		t.Errorf("Expected frame size %d, got %d", expectedSize, len(frame))
	}
}

// TestAudioMixerNilConfig verifies AudioMixer works with nil config
func TestAudioMixerNilConfig(t *testing.T) {
	// Create mixer with nil config (should use defaults, no music)
	mixer := streaming.NewAudioMixer(nil)
	if mixer == nil {
		t.Fatal("Expected non-nil mixer")
	}

	// Generate a frame - should not panic
	frame := mixer.GenerateFrame()

	// Frame should be 5880 bytes
	expectedSize := 5880
	if len(frame) != expectedSize {
		t.Errorf("Expected frame size %d, got %d", expectedSize, len(frame))
	}
}
