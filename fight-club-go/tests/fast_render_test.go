package tests

import (
	"image/color"
	"testing"

	"fight-club/internal/streaming"
)

// TestNewFastRenderer tests FastRenderer creation
func TestNewFastRenderer(t *testing.T) {
	width, height := 100, 100
	fr := streaming.NewFastRenderer(width, height, nil)

	if fr == nil {
		t.Fatal("FastRenderer should not be nil")
	}

	buffer := fr.GetBuffer()
	expectedSize := width * height * 4
	if len(buffer) != expectedSize {
		t.Errorf("Buffer size should be %d, got %d", expectedSize, len(buffer))
	}
}

// TestFastRendererWithExistingBuffer tests using an existing buffer
func TestFastRendererWithExistingBuffer(t *testing.T) {
	width, height := 50, 50
	existingBuffer := make([]byte, width*height*4)
	existingBuffer[0] = 255 // Mark first byte

	fr := streaming.NewFastRenderer(width, height, existingBuffer)
	buffer := fr.GetBuffer()

	if buffer[0] != 255 {
		t.Error("FastRenderer should use the provided buffer")
	}
}

// TestFastRendererClear tests Clear function
func TestFastRendererClear(t *testing.T) {
	width, height := 10, 10
	fr := streaming.NewFastRenderer(width, height, nil)

	testColor := color.RGBA{100, 150, 200, 255}
	fr.Clear(testColor)

	buffer := fr.GetBuffer()

	// Check first pixel
	if buffer[0] != 100 || buffer[1] != 150 || buffer[2] != 200 || buffer[3] != 255 {
		t.Errorf("First pixel should be (100, 150, 200, 255), got (%d, %d, %d, %d)",
			buffer[0], buffer[1], buffer[2], buffer[3])
	}

	// Check last pixel
	lastIdx := len(buffer) - 4
	if buffer[lastIdx] != 100 || buffer[lastIdx+1] != 150 || buffer[lastIdx+2] != 200 || buffer[lastIdx+3] != 255 {
		t.Errorf("Last pixel should be (100, 150, 200, 255), got (%d, %d, %d, %d)",
			buffer[lastIdx], buffer[lastIdx+1], buffer[lastIdx+2], buffer[lastIdx+3])
	}
}

// TestFastRendererDrawFilledRect tests rectangle drawing
func TestFastRendererDrawFilledRect(t *testing.T) {
	width, height := 20, 20
	fr := streaming.NewFastRenderer(width, height, nil)

	// Clear to black
	fr.Clear(color.RGBA{0, 0, 0, 255})

	// Draw a red rectangle
	rectColor := color.RGBA{255, 0, 0, 255}
	fr.DrawFilledRect(5, 5, 5, 5, rectColor)

	buffer := fr.GetBuffer()

	// Check pixel inside rectangle (at 7, 7)
	idx := (7*width + 7) * 4
	if buffer[idx] != 255 || buffer[idx+1] != 0 || buffer[idx+2] != 0 {
		t.Errorf("Pixel inside rect should be red, got (%d, %d, %d)",
			buffer[idx], buffer[idx+1], buffer[idx+2])
	}

	// Check pixel outside rectangle (at 0, 0)
	if buffer[0] != 0 || buffer[1] != 0 || buffer[2] != 0 {
		t.Errorf("Pixel outside rect should be black, got (%d, %d, %d)",
			buffer[0], buffer[1], buffer[2])
	}
}

// TestFastRendererDrawFilledCircle tests circle drawing
func TestFastRendererDrawFilledCircle(t *testing.T) {
	width, height := 50, 50
	fr := streaming.NewFastRenderer(width, height, nil)

	// Clear to black
	fr.Clear(color.RGBA{0, 0, 0, 255})

	// Draw a green circle at center
	circleColor := color.RGBA{0, 255, 0, 255}
	fr.DrawFilledCircle(25, 25, 10, circleColor)

	buffer := fr.GetBuffer()

	// Check pixel at center (should be green)
	centerIdx := (25*width + 25) * 4
	if buffer[centerIdx] != 0 || buffer[centerIdx+1] != 255 || buffer[centerIdx+2] != 0 {
		t.Errorf("Center pixel should be green, got (%d, %d, %d)",
			buffer[centerIdx], buffer[centerIdx+1], buffer[centerIdx+2])
	}

	// Check pixel far from center (should be black)
	cornerIdx := (0*width + 0) * 4
	if buffer[cornerIdx] != 0 || buffer[cornerIdx+1] != 0 || buffer[cornerIdx+2] != 0 {
		t.Errorf("Corner pixel should be black, got (%d, %d, %d)",
			buffer[cornerIdx], buffer[cornerIdx+1], buffer[cornerIdx+2])
	}
}

// TestFastRendererDrawLine tests line drawing
func TestFastRendererDrawLine(t *testing.T) {
	width, height := 20, 20
	fr := streaming.NewFastRenderer(width, height, nil)

	// Clear to black
	fr.Clear(color.RGBA{0, 0, 0, 255})

	// Draw a blue horizontal line
	lineColor := color.RGBA{0, 0, 255, 255}
	fr.DrawHorizontalLine(5, 15, 10, lineColor)

	buffer := fr.GetBuffer()

	// Check pixel on the line (at 10, 10)
	idx := (10*width + 10) * 4
	if buffer[idx] != 0 || buffer[idx+1] != 0 || buffer[idx+2] != 255 {
		t.Errorf("Pixel on line should be blue, got (%d, %d, %d)",
			buffer[idx], buffer[idx+1], buffer[idx+2])
	}
}

// TestFastRendererDrawVerticalLine tests vertical line drawing
func TestFastRendererDrawVerticalLine(t *testing.T) {
	width, height := 20, 20
	fr := streaming.NewFastRenderer(width, height, nil)

	// Clear to black
	fr.Clear(color.RGBA{0, 0, 0, 255})

	// Draw a white vertical line
	lineColor := color.RGBA{255, 255, 255, 255}
	fr.DrawVerticalLine(10, 5, 15, lineColor)

	buffer := fr.GetBuffer()

	// Check pixel on the line (at 10, 10)
	idx := (10*width + 10) * 4
	if buffer[idx] != 255 || buffer[idx+1] != 255 || buffer[idx+2] != 255 {
		t.Errorf("Pixel on line should be white, got (%d, %d, %d)",
			buffer[idx], buffer[idx+1], buffer[idx+2])
	}
}

// BenchmarkFastRendererClear benchmarks Clear performance
func BenchmarkFastRendererClear(b *testing.B) {
	width, height := 1280, 720
	fr := streaming.NewFastRenderer(width, height, nil)
	testColor := color.RGBA{12, 12, 28, 255}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		fr.Clear(testColor)
	}
}

// BenchmarkFastRendererDrawFilledCircle benchmarks circle drawing
func BenchmarkFastRendererDrawFilledCircle(b *testing.B) {
	width, height := 1280, 720
	fr := streaming.NewFastRenderer(width, height, nil)
	testColor := color.RGBA{255, 0, 0, 255}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		fr.DrawFilledCircle(640, 360, 30, testColor)
	}
}
