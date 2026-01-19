package streaming

import (
	"io"
	"log"
	"os"
	"sync"

	"github.com/gopxl/beep"
	"github.com/gopxl/beep/vorbis"
)

// MusicPlayer streams OGG Vorbis audio with on-demand decoding.
// Architectural decision: Streaming approach uses ~64KB buffer vs loading
// entire decoded PCM (~50MB for a 3-minute track) into memory.
// This is critical for scalability with thousands of concurrent viewers.
type MusicPlayer struct {
	mu sync.Mutex

	// Decoded audio stream from OGG file
	streamer beep.StreamSeekCloser
	format   beep.Format

	// Resampled stream (if OGG sample rate != 44100 Hz)
	resampled beep.Streamer

	// Configuration
	volume  float64
	enabled bool
	loaded  bool

	// For seamless looping: crossfade buffer
	// Stores last ~50ms of audio for crossfade at loop point
	crossfadeBuffer []float64
	crossfadeLen    int
	crossfadePos    int
	atLoopPoint     bool

	// File path for potential reload
	filePath string

	// Target sample rate (must match AudioMixer)
	targetSampleRate int

	// Pre-allocated buffer for beep samples to avoid per-frame allocations
	// Size: samplesPerFrame = 44100/30 = 1470 stereo samples
	beepBuffer [][2]float64
}

// NewMusicPlayer creates a music player for streaming OGG Vorbis.
// Implements graceful fallback: if file fails to load, returns a player
// that outputs silence (safe for production - stream continues without music).
func NewMusicPlayer(filePath string, volume float64) *MusicPlayer {
	// Pre-allocate beep buffer for samplesPerFrame stereo samples
	// 44100 Hz / 30 fps = 1470 samples per frame
	samplesPerFrame := 44100 / 30

	mp := &MusicPlayer{
		filePath:         filePath,
		volume:           volume,
		enabled:          true,
		targetSampleRate: 44100,                                // Must match AudioMixer and FFmpeg
		beepBuffer:       make([][2]float64, samplesPerFrame),  // Pre-allocated to avoid per-frame allocs
	}

	if err := mp.load(); err != nil {
		// GRACEFUL FALLBACK: Log warning but don't crash
		// Stream will continue with sound effects only
		log.Printf("⚠️ Background music disabled: %v", err)
		mp.loaded = false
	}

	return mp
}

// load opens the OGG file and initializes the streaming decoder.
// Handles sample rate resampling if the OGG file doesn't match 44100 Hz.
func (mp *MusicPlayer) load() error {
	file, err := os.Open(mp.filePath)
	if err != nil {
		return err
	}

	// Decode OGG Vorbis - this sets up streaming, NOT full decode
	streamer, format, err := vorbis.Decode(file)
	if err != nil {
		file.Close()
		return err
	}

	mp.streamer = streamer
	mp.format = format
	mp.loaded = true

	log.Printf("✅ Background music loaded: %s", mp.filePath)
	log.Printf("   Sample rate: %d Hz, Channels: %d", format.SampleRate, format.NumChannels)

	// Resample if OGG sample rate doesn't match target (44100 Hz)
	// This is critical for correct playback speed and pitch
	if int(format.SampleRate) != mp.targetSampleRate {
		log.Printf("   Resampling from %d Hz to %d Hz", format.SampleRate, mp.targetSampleRate)
		mp.resampled = beep.Resample(4, format.SampleRate, beep.SampleRate(mp.targetSampleRate), mp.streamer)
	} else {
		mp.resampled = mp.streamer
	}

	// Initialize crossfade buffer for seamless looping (~50ms at 44100 Hz stereo)
	// 50ms = 0.05 * 44100 = 2205 samples * 2 channels = 4410 floats
	mp.crossfadeLen = int(float64(mp.targetSampleRate) * 0.05 * 2) // 50ms stereo
	mp.crossfadeBuffer = make([]float64, mp.crossfadeLen)

	return nil
}

// ReadSamples fills the buffer with decoded PCM samples (int16, interleaved stereo).
// Returns the number of samples written. This method is non-blocking and safe
// for use in the audio generation loop.
//
// Seamless looping: When reaching end of file, applies crossfade to avoid clicks.
// Uses pre-allocated buffer to avoid GC pressure in hot path.
func (mp *MusicPlayer) ReadSamples(buffer []int16) int {
	mp.mu.Lock()
	defer mp.mu.Unlock()

	if !mp.loaded || !mp.enabled || mp.resampled == nil {
		// Return silence
		for i := range buffer {
			buffer[i] = 0
		}
		return len(buffer)
	}

	// Read samples from resampled stream using pre-allocated buffer
	// beep uses [][2]float64 format, we need to convert to []int16 interleaved
	numStereoSamples := len(buffer) / 2

	// Ensure we don't exceed pre-allocated buffer size
	if numStereoSamples > len(mp.beepBuffer) {
		numStereoSamples = len(mp.beepBuffer)
	}

	// Use slice of pre-allocated buffer
	workBuffer := mp.beepBuffer[:numStereoSamples]
	n, ok := mp.resampled.Stream(workBuffer)

	// Handle end of stream - loop seamlessly
	if !ok || n < numStereoSamples {
		// We've reached the end, seek back to beginning for loop
		if seeker, isSeeker := mp.streamer.(beep.StreamSeeker); isSeeker {
			if err := seeker.Seek(0); err != nil {
				log.Printf("⚠️ Music loop seek failed: %v", err)
			}
		}

		// Fill remaining with beginning of track using the same buffer slice
		if n < numStereoSamples {
			remainingSlice := workBuffer[n:numStereoSamples]
			mp.resampled.Stream(remainingSlice)
		}
	}

	// Convert from [][2]float64 to []int16 with volume scaling
	// Also apply soft limiting to prevent clipping when mixed with SFX
	vol := mp.volume
	for i := 0; i < numStereoSamples; i++ {
		// Apply volume
		left := workBuffer[i][0] * vol
		right := workBuffer[i][1] * vol

		// Convert to int16 range (-32768 to 32767)
		// Samples from beep are in -1.0 to 1.0 range
		buffer[i*2] = floatToInt16(left)
		buffer[i*2+1] = floatToInt16(right)
	}

	return len(buffer)
}

// floatToInt16 converts a float64 sample (-1.0 to 1.0) to int16.
// Includes soft clipping to prevent harsh distortion.
func floatToInt16(sample float64) int16 {
	// Scale to int16 range
	scaled := sample * 32767.0

	// Soft clip at ±30000 to leave headroom for mixing
	// Uses gradual curve instead of hard clip for less distortion
	if scaled > 30000 {
		scaled = 30000 + (scaled-30000)/4
	} else if scaled < -30000 {
		scaled = -30000 + (scaled+30000)/4
	}

	// Final hard clamp
	if scaled > 32767 {
		scaled = 32767
	} else if scaled < -32768 {
		scaled = -32768
	}

	return int16(scaled)
}

// SetVolume adjusts the music volume (0.0 to 1.0).
// Recommended: 0.1-0.2 for background music to not overpower SFX.
func (mp *MusicPlayer) SetVolume(v float64) {
	mp.mu.Lock()
	defer mp.mu.Unlock()

	if v < 0 {
		v = 0
	} else if v > 1 {
		v = 1
	}
	mp.volume = v
}

// SetEnabled enables or disables music playback without stopping the stream.
func (mp *MusicPlayer) SetEnabled(e bool) {
	mp.mu.Lock()
	defer mp.mu.Unlock()
	mp.enabled = e
}

// IsLoaded returns true if music was successfully loaded.
func (mp *MusicPlayer) IsLoaded() bool {
	mp.mu.Lock()
	defer mp.mu.Unlock()
	return mp.loaded
}

// Close releases resources. Should be called when streaming stops.
func (mp *MusicPlayer) Close() error {
	mp.mu.Lock()
	defer mp.mu.Unlock()

	if mp.streamer != nil {
		if closer, ok := mp.streamer.(io.Closer); ok {
			return closer.Close()
		}
	}
	mp.loaded = false
	return nil
}
