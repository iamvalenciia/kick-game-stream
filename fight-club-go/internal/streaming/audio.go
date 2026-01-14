package streaming

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"sync"
)

// AudioConfig holds audio mixer configuration
type AudioConfig struct {
	MusicEnabled bool
	MusicVolume  float64 // 0.0-1.0, recommended 0.1-0.2 for background
	MusicPath    string
}

// AudioMixer handles audio generation and mixing
type AudioMixer struct {
	mu            sync.Mutex
	sampleRate    int
	channels      int
	bytesPerFrame int

	// Loaded sounds
	sounds map[string][]int16

	// Active sounds being played
	activeSounds []*activeSound

	// Ambient loop position
	ambientPos int

	// Background music player (OGG Vorbis streaming)
	musicPlayer *MusicPlayer
}

type activeSound struct {
	name     string
	data     []int16
	position int
	volume   float64
}

// NewAudioMixer creates a new audio mixer
// Pass nil config for defaults (no music)
func NewAudioMixer(config *AudioConfig) *AudioMixer {
	m := &AudioMixer{
		sampleRate:   44100,
		channels:     2,
		sounds:       make(map[string][]int16),
		activeSounds: make([]*activeSound, 0),
	}

	// 44100 / 30 fps = 1470 samples per frame
	// 1470 * 2 channels * 2 bytes = 5880 bytes per frame
	m.bytesPerFrame = (m.sampleRate / 30) * m.channels * 2

	m.loadSounds()

	// Initialize background music if enabled
	// Music is tied to stream start, not individual matches
	if config != nil && config.MusicEnabled && config.MusicPath != "" {
		m.musicPlayer = NewMusicPlayer(config.MusicPath, config.MusicVolume)
	}

	return m
}

func (m *AudioMixer) loadSounds() {
	soundsDir := filepath.Join("assets", "sounds")
	soundNames := []string{"hit", "kill", "spawn", "swing", "ambient"}

	for _, name := range soundNames {
		path := filepath.Join(soundsDir, name+".wav")
		data, err := loadWAV(path)
		if err != nil {
			// Try parent directory
			path = filepath.Join("..", "assets", "sounds", name+".wav")
			data, err = loadWAV(path)
		}
		if err == nil {
			m.sounds[name] = data
		}
	}
}

// QueueSound queues a sound to be played
func (m *AudioMixer) QueueSound(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	data, ok := m.sounds[name]
	if !ok {
		return
	}

	volume := 1.0
	if name == "ambient" {
		volume = 0.3
	}

	m.activeSounds = append(m.activeSounds, &activeSound{
		name:   name,
		data:   data,
		volume: volume,
	})

	// Limit concurrent sounds
	if len(m.activeSounds) > 8 {
		m.activeSounds = m.activeSounds[1:]
	}
}

// GenerateFrame generates one frame of audio (5880 bytes)
// Mixes: background music + ambient + sound effects
// Applies soft limiting at ±30000 to prevent clipping when mixed
func (m *AudioMixer) GenerateFrame() []byte {
	m.mu.Lock()
	defer m.mu.Unlock()

	samplesPerFrame := m.sampleRate / 30
	mixBuffer := make([]int32, samplesPerFrame*m.channels)

	// Mix background music first (lowest priority, continuous)
	if m.musicPlayer != nil && m.musicPlayer.IsLoaded() {
		musicSamples := make([]int16, len(mixBuffer))
		m.musicPlayer.ReadSamples(musicSamples)
		for i := 0; i < len(mixBuffer); i++ {
			mixBuffer[i] += int32(musicSamples[i])
		}
	}

	// Mix ambient (slightly above music)
	if ambient, ok := m.sounds["ambient"]; ok && len(ambient) > 0 {
		for i := 0; i < len(mixBuffer); i++ {
			idx := (m.ambientPos + i) % len(ambient)
			mixBuffer[i] += int32(float64(ambient[idx]) * 0.20) // Reduced from 0.25
		}
		m.ambientPos = (m.ambientPos + len(mixBuffer)) % len(ambient)
	}

	// Mix active sounds (highest priority - SFX)
	alive := make([]*activeSound, 0)
	for _, s := range m.activeSounds {
		remaining := len(s.data) - s.position
		if remaining <= 0 {
			continue
		}

		toRead := len(mixBuffer)
		if toRead > remaining {
			toRead = remaining
		}

		for i := 0; i < toRead; i++ {
			mixBuffer[i] += int32(float64(s.data[s.position+i]) * s.volume)
		}

		s.position += toRead
		if s.position < len(s.data) {
			alive = append(alive, s)
		}
	}
	m.activeSounds = alive

	// Convert to bytes with SOFT LIMITING (prevents harsh clipping)
	// Soft limit at ±30000 leaves headroom, gradual curve for less distortion
	output := make([]byte, m.bytesPerFrame)
	for i := 0; i < len(mixBuffer) && i*2+1 < len(output); i++ {
		sample := mixBuffer[i]

		// Soft limiting: gradual compression above ±30000
		if sample > 30000 {
			sample = 30000 + (sample-30000)/4
		} else if sample < -30000 {
			sample = -30000 + (sample+30000)/4
		}

		// Final hard clamp
		if sample > 32767 {
			sample = 32767
		} else if sample < -32768 {
			sample = -32768
		}

		binary.LittleEndian.PutUint16(output[i*2:], uint16(int16(sample)))
	}

	return output
}

// loadWAV loads a WAV file and returns the raw PCM samples
func loadWAV(path string) ([]int16, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	// Skip 44-byte WAV header
	if len(data) < 44 {
		return nil, err
	}

	pcmData := data[44:]
	samples := make([]int16, len(pcmData)/2)

	for i := 0; i < len(samples); i++ {
		samples[i] = int16(binary.LittleEndian.Uint16(pcmData[i*2:]))
	}

	return samples, nil
}

// GenerateTone generates a simple tone for testing
func GenerateTone(frequency float64, duration float64, sampleRate int) []int16 {
	numSamples := int(duration * float64(sampleRate))
	samples := make([]int16, numSamples*2) // stereo

	for i := 0; i < numSamples; i++ {
		t := float64(i) / float64(sampleRate)
		value := math.Sin(2*math.Pi*frequency*t) * 16000
		sample := int16(value)
		samples[i*2] = sample   // Left
		samples[i*2+1] = sample // Right
	}

	return samples
}
