// Package spatial provides cache-efficient spatial data structures for
// broad-phase collision detection and neighbor queries.
//
// All structures use preallocated slices with integer indices (not pointers)
// to minimize GC pressure and maximize cache locality.
package spatial

import (
	"math"
)

// SpatialGrid provides O(1) average spatial queries via fixed-size cells.
// Uses preallocated slices with entity indices (not pointers) for GC efficiency.
//
// Optimal cell size equals the largest query radius. For Fight Club:
//   - Detection range: 500px → Cell size: 500px
//   - Collision radius: 56px (28×2) → Could use smaller grid for collision only
//
// Memory layout: cells are stored in row-major order (cells[row*cols+col])
type SpatialGrid struct {
	cellSize    float64
	invCellSize float64 // 1/cellSize for faster division
	cols, rows  int
	cells       [][]uint32 // cells[row*cols+col] = list of entity indices
	scratch     []uint32   // reusable buffer for query results
	maxEntities int
}

// NewSpatialGrid creates a grid for the given world bounds.
// cellSize should equal the largest query radius for optimal performance.
// maxEntities is used to preallocate cell capacity.
func NewSpatialGrid(worldWidth, worldHeight, cellSize float64, maxEntities int) *SpatialGrid {
	cols := int(math.Ceil(worldWidth / cellSize))
	rows := int(math.Ceil(worldHeight / cellSize))

	// Ensure at least 1x1 grid
	if cols < 1 {
		cols = 1
	}
	if rows < 1 {
		rows = 1
	}

	cells := make([][]uint32, cols*rows)
	avgPerCell := maxEntities / len(cells)
	if avgPerCell < 4 {
		avgPerCell = 4
	}
	for i := range cells {
		cells[i] = make([]uint32, 0, avgPerCell)
	}

	return &SpatialGrid{
		cellSize:    cellSize,
		invCellSize: 1.0 / cellSize,
		cols:        cols,
		rows:        rows,
		cells:       cells,
		scratch:     make([]uint32, 0, 64),
		maxEntities: maxEntities,
	}
}

// Clear resets all cells without deallocating underlying memory.
// This is O(n) where n = number of cells, not number of entities.
func (g *SpatialGrid) Clear() {
	for i := range g.cells {
		g.cells[i] = g.cells[i][:0] // Keep capacity, reset length
	}
}

// Insert adds an entity at position (x, y).
// The entityID should be the index into your entity slice.
// O(1) time complexity.
func (g *SpatialGrid) Insert(entityID uint32, x, y float64) {
	col := int(x * g.invCellSize)
	row := int(y * g.invCellSize)

	// Clamp to grid bounds
	if col < 0 {
		col = 0
	}
	if col >= g.cols {
		col = g.cols - 1
	}
	if row < 0 {
		row = 0
	}
	if row >= g.rows {
		row = g.rows - 1
	}

	idx := row*g.cols + col
	g.cells[idx] = append(g.cells[idx], entityID)
}

// cellIndex computes the cell index for a position, with bounds checking.
func (g *SpatialGrid) cellIndex(x, y float64) int {
	col := int(x * g.invCellSize)
	row := int(y * g.invCellSize)

	if col < 0 {
		col = 0
	}
	if col >= g.cols {
		col = g.cols - 1
	}
	if row < 0 {
		row = 0
	}
	if row >= g.rows {
		row = g.rows - 1
	}

	return row*g.cols + col
}

// QueryRadius returns all entity IDs potentially within radius of (cx, cy).
// Uses an internal scratch buffer to avoid allocation.
//
// IMPORTANT: The returned slice is reused on subsequent calls.
// Copy the results if you need to persist them.
//
// The returned candidates may include entities outside the radius;
// the caller must perform a precise distance check (narrow phase).
func (g *SpatialGrid) QueryRadius(cx, cy, radius float64) []uint32 {
	g.scratch = g.scratch[:0]

	// Calculate cell range that could contain entities within radius
	minCol := int((cx - radius) * g.invCellSize)
	maxCol := int((cx + radius) * g.invCellSize)
	minRow := int((cy - radius) * g.invCellSize)
	maxRow := int((cy + radius) * g.invCellSize)

	// Clamp to grid bounds
	if minCol < 0 {
		minCol = 0
	}
	if maxCol >= g.cols {
		maxCol = g.cols - 1
	}
	if minRow < 0 {
		minRow = 0
	}
	if maxRow >= g.rows {
		maxRow = g.rows - 1
	}

	// Collect candidates from all cells in range
	for row := minRow; row <= maxRow; row++ {
		for col := minCol; col <= maxCol; col++ {
			idx := row*g.cols + col
			g.scratch = append(g.scratch, g.cells[idx]...)
		}
	}

	return g.scratch
}

// QueryCell returns all entity IDs in the cell containing (x, y).
// Useful for point queries where you only care about the exact cell.
func (g *SpatialGrid) QueryCell(x, y float64) []uint32 {
	idx := g.cellIndex(x, y)
	return g.cells[idx]
}

// Stats returns grid statistics for debugging/profiling.
func (g *SpatialGrid) Stats() GridStats {
	var totalEntities, maxInCell, nonEmpty int
	for _, cell := range g.cells {
		count := len(cell)
		totalEntities += count
		if count > maxInCell {
			maxInCell = count
		}
		if count > 0 {
			nonEmpty++
		}
	}

	avgPerCell := 0.0
	if nonEmpty > 0 {
		avgPerCell = float64(totalEntities) / float64(nonEmpty)
	}

	return GridStats{
		TotalCells:     len(g.cells),
		NonEmptyCells:  nonEmpty,
		TotalEntities:  totalEntities,
		MaxInCell:      maxInCell,
		AvgPerNonEmpty: avgPerCell,
	}
}

// GridStats contains grid statistics for debugging.
type GridStats struct {
	TotalCells     int
	NonEmptyCells  int
	TotalEntities  int
	MaxInCell      int
	AvgPerNonEmpty float64
}

// Dimensions returns the grid dimensions.
func (g *SpatialGrid) Dimensions() (cols, rows int, cellSize float64) {
	return g.cols, g.rows, g.cellSize
}
