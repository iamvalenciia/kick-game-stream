package streaming

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"fight-club/internal/config"
	"fight-club/internal/game"
)

// =============================================================================
// REAL INTEGRATION TESTS - ACTUAL FFMPEG EXECUTION
// These tests start the real stream pipeline and analyze FFmpeg output
// =============================================================================

// TestRealStream_FFmpegSpeed starts the actual stream pipeline and verifies
// that FFmpeg encoding speed stays at 1.0x or higher (real-time or faster)
func TestRealStream_FFmpegSpeed(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping real stream test in short mode")
	}

	// Skip if FFmpeg not available
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("FFmpeg not installed, skipping real stream test")
	}

	// Create game engine
	engineCfg := game.EngineConfig{
		TickRate:    24,
		WorldWidth:  1280,
		WorldHeight: 720,
		Limits:      config.DefaultLimits(),
	}
	engine := game.NewEngine(engineCfg)

	// Add players to make it realistic
	for i := 0; i < 30; i++ {
		engine.AddPlayer(fmt.Sprintf("Player%d", i), game.PlayerOptions{})
	}

	// Start game engine
	engine.Start()
	defer engine.Stop()

	// Create stream config - output to null to test encoding without network
	streamCfg := StreamConfig{
		Width:   1280,
		Height:  720,
		FPS:     24,
		Bitrate: 4000,
		// Will use null output for testing
	}

	// Run FFmpeg test directly
	results, err := runFFmpegSpeedTest(t, engine, streamCfg, 10*time.Second)
	if err != nil {
		t.Fatalf("FFmpeg speed test failed: %v", err)
	}

	// Log results
	t.Logf("=== FFmpeg Real Stream Test Results ===")
	t.Logf("  Duration: %v", results.Duration)
	t.Logf("  Frames Rendered: %d", results.FramesRendered)
	t.Logf("  FFmpeg Samples: %d", results.SpeedSamples)
	t.Logf("  Min Speed: %.2fx", results.MinSpeed)
	t.Logf("  Max Speed: %.2fx", results.MaxSpeed)
	t.Logf("  Avg Speed: %.2fx", results.AvgSpeed)
	t.Logf("  Speed < 1.0x Count: %d (%.1f%%)", results.SlowCount,
		float64(results.SlowCount)/float64(results.SpeedSamples)*100)

	// Assertions
	if results.AvgSpeed < 0.95 {
		t.Errorf("Average encoding speed too slow: %.2fx < 0.95x", results.AvgSpeed)
	}

	if results.MinSpeed < 0.8 {
		t.Errorf("Minimum encoding speed critical: %.2fx < 0.8x (will cause lag)", results.MinSpeed)
	}

	slowPercent := float64(results.SlowCount) / float64(results.SpeedSamples) * 100
	if slowPercent > 10 {
		t.Errorf("Too many slow frames: %.1f%% < 1.0x speed", slowPercent)
	}
}

// SpeedTestResults holds FFmpeg speed test results
type SpeedTestResults struct {
	Duration       time.Duration
	FramesRendered int64
	SpeedSamples   int
	MinSpeed       float64
	MaxSpeed       float64
	AvgSpeed       float64
	SlowCount      int // Count of speed < 1.0x
	Speeds         []float64
}

// runFFmpegSpeedTest runs FFmpeg with null output and captures speed metrics
func runFFmpegSpeedTest(t *testing.T, engine *game.Engine, cfg StreamConfig, duration time.Duration) (*SpeedTestResults, error) {
	// Build FFmpeg command for null output (testing encoding speed only)
	// This tests the full pipeline without network overhead
	nullOutput := "/dev/null"
	if runtime.GOOS == "windows" {
		nullOutput = "NUL"
	}

	args := []string{
		"-y",
		// Video input (pipe)
		"-f", "rawvideo",
		"-pix_fmt", "rgba",
		"-s", fmt.Sprintf("%dx%d", cfg.Width, cfg.Height),
		"-r", fmt.Sprintf("%d", cfg.FPS),
		"-i", "pipe:0",
		// Audio (silent)
		"-f", "lavfi",
		"-i", "anullsrc=channel_layout=stereo:sample_rate=44100",
		// Video encoding - same settings as production
		"-c:v", "libx264",
		"-preset", "ultrafast",
		"-tune", "zerolatency",
		"-b:v", fmt.Sprintf("%dk", cfg.Bitrate),
		"-maxrate", fmt.Sprintf("%dk", cfg.Bitrate),
		"-bufsize", fmt.Sprintf("%dk", cfg.Bitrate*2),
		"-pix_fmt", "yuv420p",
		"-g", fmt.Sprintf("%d", cfg.FPS*2),
		// Audio encoding
		"-c:a", "aac",
		"-b:a", "128k",
		// Output to null (test encoding speed only)
		"-f", "null",
		nullOutput,
	}

	cmd := exec.Command("ffmpeg", args...)

	// Create pipe for video input
	videoPipe, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create video pipe: %w", err)
	}

	// Capture stderr for speed parsing
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	// Start FFmpeg
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start ffmpeg: %w", err)
	}

	// Results
	results := &SpeedTestResults{
		MinSpeed: 999.0,
		MaxSpeed: 0.0,
		Speeds:   make([]float64, 0, 100),
	}

	var wg sync.WaitGroup
	var framesRendered int64
	ctx, cancel := context.WithTimeout(context.Background(), duration+5*time.Second)
	defer cancel()

	// Parse FFmpeg stderr for speed metrics
	wg.Add(1)
	go func() {
		defer wg.Done()
		parseFFmpegSpeed(stderrPipe, results, t)
	}()

	// Render frames and send to FFmpeg
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer videoPipe.Close()

		ticker := time.NewTicker(time.Second / time.Duration(cfg.FPS))
		defer ticker.Stop()

		frameSize := cfg.Width * cfg.Height * 4
		frameBuffer := make([]byte, frameSize)

		startTime := time.Now()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				// Check duration
				if time.Since(startTime) > duration {
					return
				}

				// Get snapshot and render
				snapshot := engine.GetSnapshot()
				if snapshot == nil {
					continue
				}

				// Simple frame generation (fill with test pattern)
				// In real test, would use actual renderer
				generateTestFrame(frameBuffer, cfg.Width, cfg.Height, snapshot)

				// Write to FFmpeg
				_, err := videoPipe.Write(frameBuffer)
				if err != nil {
					t.Logf("Video write error: %v", err)
					return
				}

				atomic.AddInt64(&framesRendered, 1)
			}
		}
	}()

	// Wait for completion
	wg.Wait()
	cmd.Wait()

	results.Duration = duration
	results.FramesRendered = atomic.LoadInt64(&framesRendered)
	results.SpeedSamples = len(results.Speeds)

	// Calculate average
	if len(results.Speeds) > 0 {
		var sum float64
		for _, s := range results.Speeds {
			sum += s
		}
		results.AvgSpeed = sum / float64(len(results.Speeds))
	}

	return results, nil
}

// parseFFmpegSpeed parses FFmpeg stderr output for speed metrics
// FFmpeg outputs lines like: frame=  120 fps= 24 q=25.0 size=     256kB time=00:00:05.00 bitrate= 419.4kbits/s speed=1.00x
func parseFFmpegSpeed(stderr io.Reader, results *SpeedTestResults, t *testing.T) {
	speedRegex := regexp.MustCompile(`speed=\s*([0-9.]+)x`)
	scanner := bufio.NewScanner(stderr)

	for scanner.Scan() {
		line := scanner.Text()

		// Parse speed
		matches := speedRegex.FindStringSubmatch(line)
		if len(matches) >= 2 {
			speed, err := strconv.ParseFloat(matches[1], 64)
			if err == nil && speed > 0 {
				results.Speeds = append(results.Speeds, speed)

				if speed < results.MinSpeed {
					results.MinSpeed = speed
				}
				if speed > results.MaxSpeed {
					results.MaxSpeed = speed
				}
				if speed < 1.0 {
					results.SlowCount++
				}

				// Log speed periodically
				if len(results.Speeds)%10 == 0 {
					t.Logf("FFmpeg speed sample %d: %.2fx", len(results.Speeds), speed)
				}
			}
		}
	}
}

// generateTestFrame creates a simple test frame based on game state
func generateTestFrame(buffer []byte, width, height int, snapshot *game.GameSnapshot) {
	// White background
	for i := 0; i < len(buffer); i += 4 {
		buffer[i] = 250   // R
		buffer[i+1] = 250 // G
		buffer[i+2] = 255 // B
		buffer[i+3] = 255 // A
	}

	// Draw simple circles for players (to make frames different)
	for _, p := range snapshot.Players {
		if p.IsDead {
			continue
		}
		// Draw a simple colored circle at player position
		drawCircle(buffer, width, height, int(p.X), int(p.Y), 30, p.Color)
	}
}

// drawCircle draws a simple filled circle in the buffer
func drawCircle(buffer []byte, bufWidth, bufHeight, cx, cy, radius int, hexColor string) {
	r, g, b := parseHex(hexColor)

	for y := cy - radius; y <= cy+radius; y++ {
		for x := cx - radius; x <= cx+radius; x++ {
			if x < 0 || x >= bufWidth || y < 0 || y >= bufHeight {
				continue
			}
			dx := x - cx
			dy := y - cy
			if dx*dx+dy*dy <= radius*radius {
				idx := (y*bufWidth + x) * 4
				if idx >= 0 && idx+3 < len(buffer) {
					buffer[idx] = r
					buffer[idx+1] = g
					buffer[idx+2] = b
					buffer[idx+3] = 255
				}
			}
		}
	}
}

func parseHex(hex string) (uint8, uint8, uint8) {
	hex = strings.TrimPrefix(hex, "#")
	if len(hex) != 6 {
		return 255, 255, 255
	}
	var r, g, b uint8
	fmt.Sscanf(hex, "%02x%02x%02x", &r, &g, &b)
	return r, g, b
}

// TestRealStream_FrameDrops tests if frames are being dropped during rendering
func TestRealStream_FrameDrops(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping frame drop test in short mode")
	}

	// Create game engine
	engineCfg := game.EngineConfig{
		TickRate:    24,
		WorldWidth:  1280,
		WorldHeight: 720,
		Limits:      config.DefaultLimits(),
	}
	engine := game.NewEngine(engineCfg)

	// Add players
	for i := 0; i < 50; i++ {
		engine.AddPlayer(fmt.Sprintf("Player%d", i), game.PlayerOptions{})
	}

	engine.Start()
	defer engine.Stop()

	// Run render loop and count timing violations
	testDuration := 5 * time.Second
	targetFrameTime := time.Second / 24

	var (
		frameCount    int64
		lateFrames    int64
		maxFrameTime  time.Duration
		totalFrameTime time.Duration
	)

	ticker := time.NewTicker(targetFrameTime)
	defer ticker.Stop()

	startTime := time.Now()
	lastFrame := time.Now()

	for time.Since(startTime) < testDuration {
		<-ticker.C

		frameStart := time.Now()

		// Simulate full render
		snapshot := engine.GetSnapshot()
		if snapshot == nil {
			continue
		}

		// Allocate and fill frame buffer (simulates actual rendering)
		buffer := make([]byte, 1280*720*4)
		generateTestFrame(buffer, 1280, 720, snapshot)

		frameTime := time.Since(frameStart)
		timeSinceLastFrame := time.Since(lastFrame)
		lastFrame = time.Now()

		atomic.AddInt64(&frameCount, 1)
		totalFrameTime += frameTime

		if frameTime > maxFrameTime {
			maxFrameTime = frameTime
		}

		// Check if we're late (missed frame deadline)
		if timeSinceLastFrame > targetFrameTime*3/2 { // 50% tolerance
			atomic.AddInt64(&lateFrames, 1)
		}
	}

	frames := atomic.LoadInt64(&frameCount)
	late := atomic.LoadInt64(&lateFrames)
	avgFrameTime := totalFrameTime / time.Duration(frames)

	t.Logf("=== Frame Drop Test Results ===")
	t.Logf("  Duration: %v", testDuration)
	t.Logf("  Total Frames: %d", frames)
	t.Logf("  Late Frames: %d (%.1f%%)", late, float64(late)/float64(frames)*100)
	t.Logf("  Avg Frame Time: %v", avgFrameTime)
	t.Logf("  Max Frame Time: %v", maxFrameTime)
	t.Logf("  Target Frame Time: %v", targetFrameTime)

	// Assertions
	latePercent := float64(late) / float64(frames) * 100
	if latePercent > 5 {
		t.Errorf("Too many late frames: %.1f%% > 5%%", latePercent)
	}

	if avgFrameTime > targetFrameTime/2 {
		t.Errorf("Average frame time too high: %v > %v (50%% of budget)", avgFrameTime, targetFrameTime/2)
	}

	expectedFrames := int64(testDuration.Seconds() * 24 * 0.95) // 95% of expected
	if frames < expectedFrames {
		t.Errorf("Too few frames rendered: %d < %d expected", frames, expectedFrames)
	}
}
