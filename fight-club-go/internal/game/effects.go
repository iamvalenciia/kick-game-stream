package game

// WeaponTrail represents a trailing visual effect for swing attacks.
// Uses a fixed-size ring buffer to avoid allocations.
type WeaponTrail struct {
	Points     [8]TrailPoint // Fixed-size ring buffer
	WriteIndex int           // Current write position
	PointCount int           // Number of valid points
	Color      string        // Trail color (hex)
	Timer      int           // Remaining lifetime in ticks
	PlayerID   string        // Owning player

	// Weapon-specific trail config
	TrailType TrailType // Arc, Line, Radial
	ArcAngle  float64   // Start angle for arc trails
	ArcSweep  float64   // Sweep amount for arc trails
}

// TrailPoint is a single point in a weapon trail.
type TrailPoint struct {
	X, Y  float64
	Alpha float64
}

// ImpactFlash creates a burst effect on hit.
type ImpactFlash struct {
	X, Y      float64
	Radius    float64
	MaxRadius float64
	Color     string
	Timer     int // Remaining ticks
}

// ScreenShake represents camera shake from heavy hits.
type ScreenShake struct {
	Intensity float64 // Current shake magnitude
	Duration  int     // Remaining ticks
	OffsetX   float64 // Current X offset (computed each frame)
	OffsetY   float64 // Current Y offset (computed each frame)
}

// NewWeaponTrail creates a new weapon trail effect.
func NewWeaponTrail(startX, startY float64, color, playerID string) *WeaponTrail {
	t := &WeaponTrail{
		Color:    color,
		Timer:    15, // ~0.75 seconds at 20 TPS
		PlayerID: playerID,
	}
	t.AddPoint(startX, startY)
	return t
}

// AddPoint adds a new point to the trail.
func (t *WeaponTrail) AddPoint(x, y float64) {
	t.Points[t.WriteIndex] = TrailPoint{
		X:     x,
		Y:     y,
		Alpha: 1.0,
	}
	t.WriteIndex = (t.WriteIndex + 1) % len(t.Points)
	if t.PointCount < len(t.Points) {
		t.PointCount++
	}
}

// Update updates the trail (fade out points).
func (t *WeaponTrail) Update() bool {
	t.Timer--

	// Fade all points
	for i := 0; i < len(t.Points); i++ {
		t.Points[i].Alpha *= 0.85
	}

	return t.Timer > 0
}

// GetPoints returns all valid points in order (oldest first).
func (t *WeaponTrail) GetPoints() []TrailPoint {
	if t.PointCount == 0 {
		return nil
	}

	result := make([]TrailPoint, t.PointCount)
	startIdx := t.WriteIndex - t.PointCount
	if startIdx < 0 {
		startIdx += len(t.Points)
	}

	for i := 0; i < t.PointCount; i++ {
		idx := (startIdx + i) % len(t.Points)
		result[i] = t.Points[idx]
	}
	return result
}

// NewImpactFlash creates a new impact flash effect.
func NewImpactFlash(x, y float64, color string, intensity float64) *ImpactFlash {
	// Keep effects SMALL to prevent visual accumulation
	maxRadius := 10.0 + intensity*5.0 // Much smaller: 10-15px (was 30-50)
	return &ImpactFlash{
		X:         x,
		Y:         y,
		Radius:    3.0, // Start smaller
		MaxRadius: maxRadius,
		Color:     color,
		Timer:     5, // Quick fade: 0.17 seconds at 30 TPS
	}
}

// Update updates the flash (expand and fade).
func (f *ImpactFlash) Update() bool {
	f.Timer--

	// Expand rapidly then slow down
	progress := 1.0 - float64(f.Timer)/5.0
	f.Radius = f.MaxRadius * (1.0 - (1.0-progress)*(1.0-progress))

	return f.Timer > 0
}

// GetAlpha returns the current opacity.
func (f *ImpactFlash) GetAlpha() float64 {
	return float64(f.Timer) / 5.0
}

// NewScreenShake creates a new screen shake effect.
func NewScreenShake(intensity float64) *ScreenShake {
	if intensity > MaxShakeIntensity {
		intensity = MaxShakeIntensity
	}
	return &ScreenShake{
		Intensity: intensity,
		Duration:  8, // 0.4 seconds at 20 TPS
	}
}

// Update updates the shake (decay over time).
// Uses deterministic RNG seed for replay consistency.
func (s *ScreenShake) Update(rngSeed int64) bool {
	s.Duration--

	// Decay intensity
	s.Intensity *= 0.8

	// Compute offsets using deterministic pseudo-random
	// Simple LCG for determinism: next = (a * seed + c) mod m
	seed := rngSeed + int64(s.Duration)
	x := float64((seed*1103515245+12345)%256) / 256.0
	y := float64((seed*1103515245*2+12345)%256) / 256.0

	s.OffsetX = (x - 0.5) * 2 * s.Intensity
	s.OffsetY = (y - 0.5) * 2 * s.Intensity

	return s.Duration > 0 && s.Intensity > 0.5
}

// DodgeAfterimage represents a ghost trail from dodging.
type DodgeAfterimage struct {
	X, Y  float64
	Color string
	Alpha float64
	Timer int
}

// NewDodgeAfterimage creates a new afterimage effect.
func NewDodgeAfterimage(x, y float64, color string) *DodgeAfterimage {
	return &DodgeAfterimage{
		X:     x,
		Y:     y,
		Color: color,
		Alpha: 0.6,
		Timer: 8,
	}
}

// Update updates the afterimage (fade out).
func (a *DodgeAfterimage) Update() bool {
	a.Timer--
	a.Alpha *= 0.75
	return a.Timer > 0 && a.Alpha > 0.1
}
