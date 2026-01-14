package streaming

import (
	"image/color"
	"math"
)

// FastRenderer provides optimized direct-to-buffer rendering for simple primitives.
// This bypasses the overhead of gg.Context for basic shapes.
type FastRenderer struct {
	buffer []byte
	width  int
	height int
	stride int // bytes per row (width * 4 for RGBA)
}

// NewFastRenderer creates a new fast renderer with the given dimensions.
// It uses the provided buffer or creates a new one if nil.
func NewFastRenderer(width, height int, buffer []byte) *FastRenderer {
	stride := width * 4
	if buffer == nil {
		buffer = make([]byte, width*height*4)
	}
	return &FastRenderer{
		buffer: buffer,
		width:  width,
		height: height,
		stride: stride,
	}
}

// GetBuffer returns the underlying pixel buffer
func (r *FastRenderer) GetBuffer() []byte {
	return r.buffer
}

// SetBuffer sets the pixel buffer to use for rendering
func (r *FastRenderer) SetBuffer(buffer []byte) {
	r.buffer = buffer
}

// Clear fills the entire buffer with a solid color
func (r *FastRenderer) Clear(c color.RGBA) {
	for i := 0; i < len(r.buffer); i += 4 {
		r.buffer[i] = c.R
		r.buffer[i+1] = c.G
		r.buffer[i+2] = c.B
		r.buffer[i+3] = c.A
	}
}

// setPixel sets a single pixel with bounds checking
func (r *FastRenderer) setPixel(x, y int, c color.RGBA) {
	if x < 0 || x >= r.width || y < 0 || y >= r.height {
		return
	}
	idx := y*r.stride + x*4
	if idx >= 0 && idx+3 < len(r.buffer) {
		r.buffer[idx] = c.R
		r.buffer[idx+1] = c.G
		r.buffer[idx+2] = c.B
		r.buffer[idx+3] = c.A
	}
}

// setPixelBlend sets a pixel with alpha blending
func (r *FastRenderer) setPixelBlend(x, y int, c color.RGBA) {
	if x < 0 || x >= r.width || y < 0 || y >= r.height {
		return
	}
	idx := y*r.stride + x*4
	if idx < 0 || idx+3 >= len(r.buffer) {
		return
	}

	if c.A == 255 {
		// Fully opaque - direct write
		r.buffer[idx] = c.R
		r.buffer[idx+1] = c.G
		r.buffer[idx+2] = c.B
		r.buffer[idx+3] = c.A
		return
	}

	if c.A == 0 {
		return // Fully transparent - skip
	}

	// Alpha blending: result = src * srcA + dst * (1 - srcA)
	srcA := float64(c.A) / 255.0
	invA := 1.0 - srcA

	r.buffer[idx] = uint8(float64(c.R)*srcA + float64(r.buffer[idx])*invA)
	r.buffer[idx+1] = uint8(float64(c.G)*srcA + float64(r.buffer[idx+1])*invA)
	r.buffer[idx+2] = uint8(float64(c.B)*srcA + float64(r.buffer[idx+2])*invA)
	r.buffer[idx+3] = 255 // Assume destination is always opaque
}

// DrawFilledRect draws a filled rectangle
func (r *FastRenderer) DrawFilledRect(x, y, w, h int, c color.RGBA) {
	// Clip to bounds
	x1 := max(0, x)
	y1 := max(0, y)
	x2 := min(r.width, x+w)
	y2 := min(r.height, y+h)

	if x1 >= x2 || y1 >= y2 {
		return
	}

	for py := y1; py < y2; py++ {
		rowStart := py * r.stride
		for px := x1; px < x2; px++ {
			idx := rowStart + px*4
			r.buffer[idx] = c.R
			r.buffer[idx+1] = c.G
			r.buffer[idx+2] = c.B
			r.buffer[idx+3] = c.A
		}
	}
}

// DrawFilledRectBlend draws a filled rectangle with alpha blending
func (r *FastRenderer) DrawFilledRectBlend(x, y, w, h int, c color.RGBA) {
	if c.A == 255 {
		r.DrawFilledRect(x, y, w, h, c)
		return
	}

	x1 := max(0, x)
	y1 := max(0, y)
	x2 := min(r.width, x+w)
	y2 := min(r.height, y+h)

	if x1 >= x2 || y1 >= y2 {
		return
	}

	srcA := float64(c.A) / 255.0
	invA := 1.0 - srcA

	for py := y1; py < y2; py++ {
		rowStart := py * r.stride
		for px := x1; px < x2; px++ {
			idx := rowStart + px*4
			r.buffer[idx] = uint8(float64(c.R)*srcA + float64(r.buffer[idx])*invA)
			r.buffer[idx+1] = uint8(float64(c.G)*srcA + float64(r.buffer[idx+1])*invA)
			r.buffer[idx+2] = uint8(float64(c.B)*srcA + float64(r.buffer[idx+2])*invA)
			r.buffer[idx+3] = 255
		}
	}
}

// DrawFilledCircle draws a filled circle using midpoint algorithm
func (r *FastRenderer) DrawFilledCircle(cx, cy int, radius float64, c color.RGBA) {
	rad := int(radius + 0.5)
	radSq := radius * radius

	y1 := max(0, cy-rad)
	y2 := min(r.height, cy+rad+1)

	for py := y1; py < y2; py++ {
		dy := float64(py - cy)
		dySq := dy * dy
		// Calculate x extent for this row
		xExtent := math.Sqrt(radSq - dySq)
		x1 := max(0, cx-int(xExtent+0.5))
		x2 := min(r.width, cx+int(xExtent+0.5)+1)

		rowStart := py * r.stride
		for px := x1; px < x2; px++ {
			dx := float64(px - cx)
			if dx*dx+dySq <= radSq {
				idx := rowStart + px*4
				r.buffer[idx] = c.R
				r.buffer[idx+1] = c.G
				r.buffer[idx+2] = c.B
				r.buffer[idx+3] = c.A
			}
		}
	}
}

// DrawFilledCircleBlend draws a filled circle with alpha blending
func (r *FastRenderer) DrawFilledCircleBlend(cx, cy int, radius float64, c color.RGBA) {
	if c.A == 255 {
		r.DrawFilledCircle(cx, cy, radius, c)
		return
	}

	rad := int(radius + 0.5)
	radSq := radius * radius
	srcA := float64(c.A) / 255.0
	invA := 1.0 - srcA

	y1 := max(0, cy-rad)
	y2 := min(r.height, cy+rad+1)

	for py := y1; py < y2; py++ {
		dy := float64(py - cy)
		dySq := dy * dy
		xExtent := math.Sqrt(radSq - dySq)
		x1 := max(0, cx-int(xExtent+0.5))
		x2 := min(r.width, cx+int(xExtent+0.5)+1)

		rowStart := py * r.stride
		for px := x1; px < x2; px++ {
			dx := float64(px - cx)
			if dx*dx+dySq <= radSq {
				idx := rowStart + px*4
				r.buffer[idx] = uint8(float64(c.R)*srcA + float64(r.buffer[idx])*invA)
				r.buffer[idx+1] = uint8(float64(c.G)*srcA + float64(r.buffer[idx+1])*invA)
				r.buffer[idx+2] = uint8(float64(c.B)*srcA + float64(r.buffer[idx+2])*invA)
				r.buffer[idx+3] = 255
			}
		}
	}
}

// DrawCircleOutline draws a circle outline (not filled)
func (r *FastRenderer) DrawCircleOutline(cx, cy int, radius float64, lineWidth int, c color.RGBA) {
	outerRad := radius + float64(lineWidth)/2
	innerRad := radius - float64(lineWidth)/2
	if innerRad < 0 {
		innerRad = 0
	}
	outerRadSq := outerRad * outerRad
	innerRadSq := innerRad * innerRad

	rad := int(outerRad + 0.5)
	y1 := max(0, cy-rad)
	y2 := min(r.height, cy+rad+1)

	for py := y1; py < y2; py++ {
		dy := float64(py - cy)
		dySq := dy * dy
		xExtent := math.Sqrt(outerRadSq - dySq)
		x1 := max(0, cx-int(xExtent+0.5))
		x2 := min(r.width, cx+int(xExtent+0.5)+1)

		rowStart := py * r.stride
		for px := x1; px < x2; px++ {
			dx := float64(px - cx)
			distSq := dx*dx + dySq
			if distSq <= outerRadSq && distSq >= innerRadSq {
				idx := rowStart + px*4
				r.buffer[idx] = c.R
				r.buffer[idx+1] = c.G
				r.buffer[idx+2] = c.B
				r.buffer[idx+3] = c.A
			}
		}
	}
}

// DrawLine draws a line using Bresenham's algorithm
func (r *FastRenderer) DrawLine(x0, y0, x1, y1 int, c color.RGBA) {
	dx := absInt(x1 - x0)
	dy := -absInt(y1 - y0)
	sx := 1
	if x0 > x1 {
		sx = -1
	}
	sy := 1
	if y0 > y1 {
		sy = -1
	}
	err := dx + dy

	for {
		r.setPixel(x0, y0, c)
		if x0 == x1 && y0 == y1 {
			break
		}
		e2 := 2 * err
		if e2 >= dy {
			err += dy
			x0 += sx
		}
		if e2 <= dx {
			err += dx
			y0 += sy
		}
	}
}

// DrawThickLine draws a line with thickness
func (r *FastRenderer) DrawThickLine(x0, y0, x1, y1 int, thickness int, c color.RGBA) {
	if thickness <= 1 {
		r.DrawLine(x0, y0, x1, y1, c)
		return
	}

	// Draw multiple parallel lines for thickness
	dx := float64(x1 - x0)
	dy := float64(y1 - y0)
	length := math.Sqrt(dx*dx + dy*dy)
	if length == 0 {
		r.DrawFilledCircle(x0, y0, float64(thickness)/2, c)
		return
	}

	// Perpendicular vector
	px := -dy / length
	py := dx / length

	halfThick := float64(thickness) / 2
	for i := -int(halfThick); i <= int(halfThick); i++ {
		offset := float64(i)
		r.DrawLine(
			x0+int(px*offset),
			y0+int(py*offset),
			x1+int(px*offset),
			y1+int(py*offset),
			c,
		)
	}
}

// DrawHorizontalLine draws a fast horizontal line
func (r *FastRenderer) DrawHorizontalLine(x1, x2, y int, c color.RGBA) {
	if y < 0 || y >= r.height {
		return
	}
	if x1 > x2 {
		x1, x2 = x2, x1
	}
	x1 = max(0, x1)
	x2 = min(r.width-1, x2)

	rowStart := y * r.stride
	for x := x1; x <= x2; x++ {
		idx := rowStart + x*4
		r.buffer[idx] = c.R
		r.buffer[idx+1] = c.G
		r.buffer[idx+2] = c.B
		r.buffer[idx+3] = c.A
	}
}

// DrawVerticalLine draws a fast vertical line
func (r *FastRenderer) DrawVerticalLine(x, y1, y2 int, c color.RGBA) {
	if x < 0 || x >= r.width {
		return
	}
	if y1 > y2 {
		y1, y2 = y2, y1
	}
	y1 = max(0, y1)
	y2 = min(r.height-1, y2)

	for y := y1; y <= y2; y++ {
		idx := y*r.stride + x*4
		r.buffer[idx] = c.R
		r.buffer[idx+1] = c.G
		r.buffer[idx+2] = c.B
		r.buffer[idx+3] = c.A
	}
}

// absInt returns absolute value of int (Go builtin abs is for floats)
func absInt(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
