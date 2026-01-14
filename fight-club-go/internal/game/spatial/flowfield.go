package spatial

import (
	"math"
)

// FlowField provides O(1) per-agent navigation via precomputed vector fields.
// Instead of running A* for each agent, we compute a single field that all agents share.
//
// For 100 agents targeting one goal: 100× A* vs 1× field generation.
//
// Origin: Treuille, Cooper, Popović. "Continuum Crowds." SIGGRAPH 2006.
type FlowField struct {
	cols, rows  int
	cellSize    float64
	invCellSize float64
	integration []float32 // Cost to reach goal from each cell
	flowX       []float32 // X component of flow direction
	flowY       []float32 // Y component of flow direction
	blocked     []bool    // Impassable cells
	queue       []int     // Reusable BFS queue
}

// NewFlowField creates a flow field for the given world size.
// cellSize determines the resolution of the field.
// Smaller cells = smoother paths but more memory/computation.
func NewFlowField(worldWidth, worldHeight, cellSize float64) *FlowField {
	cols := int(math.Ceil(worldWidth / cellSize))
	rows := int(math.Ceil(worldHeight / cellSize))

	if cols < 1 {
		cols = 1
	}
	if rows < 1 {
		rows = 1
	}

	size := cols * rows

	return &FlowField{
		cols:        cols,
		rows:        rows,
		cellSize:    cellSize,
		invCellSize: 1.0 / cellSize,
		integration: make([]float32, size),
		flowX:       make([]float32, size),
		flowY:       make([]float32, size),
		blocked:     make([]bool, size),
		queue:       make([]int, 0, size),
	}
}

// SetBlocked marks cells as blocked/unblocked.
// blocked[cellIndex] = true means impassable.
func (f *FlowField) SetBlocked(blocked []bool) {
	if len(blocked) != len(f.blocked) {
		return
	}
	copy(f.blocked, blocked)
}

// SetCellBlocked marks a single cell as blocked/unblocked by world position.
func (f *FlowField) SetCellBlocked(worldX, worldY float64, isBlocked bool) {
	col := int(worldX * f.invCellSize)
	row := int(worldY * f.invCellSize)
	if col < 0 || col >= f.cols || row < 0 || row >= f.rows {
		return
	}
	f.blocked[row*f.cols+col] = isBlocked
}

// Generate computes the flow field toward (goalX, goalY).
// This uses BFS/Dijkstra to compute integration field, then gradient descent for flow.
//
// Time complexity: O(cols × rows)
// Should be called when goal changes or blocked cells change.
func (f *FlowField) Generate(goalX, goalY float64) {
	maxCost := float32(math.MaxFloat32)

	// Reset integration field
	for i := range f.integration {
		f.integration[i] = maxCost
	}

	// Find goal cell
	goalCol := int(goalX * f.invCellSize)
	goalRow := int(goalY * f.invCellSize)

	// Clamp goal to grid
	if goalCol < 0 {
		goalCol = 0
	}
	if goalCol >= f.cols {
		goalCol = f.cols - 1
	}
	if goalRow < 0 {
		goalRow = 0
	}
	if goalRow >= f.rows {
		goalRow = f.rows - 1
	}

	goalIdx := goalRow*f.cols + goalCol

	// Don't generate if goal is blocked
	if f.blocked[goalIdx] {
		return
	}

	f.integration[goalIdx] = 0

	// BFS from goal (Dijkstra-lite for uniform cost)
	f.queue = f.queue[:0]
	f.queue = append(f.queue, goalIdx)

	// Direction offsets: 8-way connectivity
	// dx, dy for each neighbor
	dx := []int{-1, 0, 1, -1, 1, -1, 0, 1}
	dy := []int{-1, -1, -1, 0, 0, 1, 1, 1}
	// Cost for each direction (√2 for diagonals)
	cost := []float32{1.41421356, 1.0, 1.41421356, 1.0, 1.0, 1.41421356, 1.0, 1.41421356}

	head := 0
	for head < len(f.queue) {
		current := f.queue[head]
		head++

		row := current / f.cols
		col := current % f.cols
		currentCost := f.integration[current]

		// Check all 8 neighbors
		for i := 0; i < 8; i++ {
			nc := col + dx[i]
			nr := row + dy[i]

			if nc < 0 || nc >= f.cols || nr < 0 || nr >= f.rows {
				continue
			}

			nidx := nr*f.cols + nc
			if f.blocked[nidx] {
				continue
			}

			newCost := currentCost + cost[i]
			if newCost < f.integration[nidx] {
				f.integration[nidx] = newCost
				f.queue = append(f.queue, nidx)
			}
		}
	}

	// Generate flow vectors (gradient descent)
	for idx := 0; idx < len(f.integration); idx++ {
		if f.integration[idx] == maxCost {
			f.flowX[idx], f.flowY[idx] = 0, 0
			continue
		}

		row := idx / f.cols
		col := idx % f.cols
		bestDX, bestDY := float32(0), float32(0)
		bestCost := f.integration[idx]

		// Find neighbor with lowest cost
		for i := 0; i < 8; i++ {
			nc := col + dx[i]
			nr := row + dy[i]

			if nc < 0 || nc >= f.cols || nr < 0 || nr >= f.rows {
				continue
			}

			nidx := nr*f.cols + nc
			if f.integration[nidx] < bestCost {
				bestCost = f.integration[nidx]
				bestDX = float32(dx[i])
				bestDY = float32(dy[i])
			}
		}

		// Normalize flow vector
		length := float32(math.Sqrt(float64(bestDX*bestDX + bestDY*bestDY)))
		if length > 0 {
			f.flowX[idx] = bestDX / length
			f.flowY[idx] = bestDY / length
		} else {
			f.flowX[idx] = 0
			f.flowY[idx] = 0
		}
	}
}

// Lookup returns the flow direction at world position (x, y).
// Returns (0, 0) if position is out of bounds or unreachable.
//
// Time complexity: O(1)
func (f *FlowField) Lookup(x, y float64) (vx, vy float32) {
	col := int(x * f.invCellSize)
	row := int(y * f.invCellSize)

	if col < 0 || col >= f.cols || row < 0 || row >= f.rows {
		return 0, 0
	}

	idx := row*f.cols + col
	return f.flowX[idx], f.flowY[idx]
}

// LookupWithCost returns the flow direction and integration cost at (x, y).
// Cost indicates distance to goal (lower = closer).
func (f *FlowField) LookupWithCost(x, y float64) (vx, vy float32, cost float32) {
	col := int(x * f.invCellSize)
	row := int(y * f.invCellSize)

	if col < 0 || col >= f.cols || row < 0 || row >= f.rows {
		return 0, 0, float32(math.MaxFloat32)
	}

	idx := row*f.cols + col
	return f.flowX[idx], f.flowY[idx], f.integration[idx]
}

// GetCost returns the integration cost at (x, y).
// Returns MaxFloat32 if unreachable.
func (f *FlowField) GetCost(x, y float64) float32 {
	col := int(x * f.invCellSize)
	row := int(y * f.invCellSize)

	if col < 0 || col >= f.cols || row < 0 || row >= f.rows {
		return float32(math.MaxFloat32)
	}

	return f.integration[row*f.cols+col]
}

// Dimensions returns the grid dimensions.
func (f *FlowField) Dimensions() (cols, rows int, cellSize float64) {
	return f.cols, f.rows, f.cellSize
}

// FlowFieldManager manages multiple flow fields for different goals.
// Useful when agents have different targets (e.g., team bases, objectives).
type FlowFieldManager struct {
	worldWidth  float64
	worldHeight float64
	cellSize    float64
	fields      map[string]*FlowField
}

// NewFlowFieldManager creates a manager for multiple flow fields.
func NewFlowFieldManager(worldWidth, worldHeight, cellSize float64) *FlowFieldManager {
	return &FlowFieldManager{
		worldWidth:  worldWidth,
		worldHeight: worldHeight,
		cellSize:    cellSize,
		fields:      make(map[string]*FlowField),
	}
}

// GetOrCreate returns a flow field for the given goal key, creating if needed.
// The goalKey should be a unique identifier (e.g., "team-red-base", "objective-1").
func (m *FlowFieldManager) GetOrCreate(goalKey string, goalX, goalY float64) *FlowField {
	if field, ok := m.fields[goalKey]; ok {
		return field
	}

	field := NewFlowField(m.worldWidth, m.worldHeight, m.cellSize)
	field.Generate(goalX, goalY)
	m.fields[goalKey] = field
	return field
}

// Regenerate re-generates the flow field for a goal.
// Call when goal position changes or obstacles change.
func (m *FlowFieldManager) Regenerate(goalKey string, goalX, goalY float64) *FlowField {
	field := NewFlowField(m.worldWidth, m.worldHeight, m.cellSize)
	field.Generate(goalX, goalY)
	m.fields[goalKey] = field
	return field
}

// Remove removes a flow field.
func (m *FlowFieldManager) Remove(goalKey string) {
	delete(m.fields, goalKey)
}

// Clear removes all flow fields.
func (m *FlowFieldManager) Clear() {
	m.fields = make(map[string]*FlowField)
}
