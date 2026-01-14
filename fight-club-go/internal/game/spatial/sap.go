package spatial

import (
	"sort"
)

// SweepAndPrune implements 1-axis sweep with temporal coherence for broad-phase collision detection.
// It projects entity bounding intervals onto the X-axis, sorts endpoints, and detects overlaps.
//
// With temporal coherence (entities move little between frames), insertion sort approaches O(n).
// This is a well-established technique from physics engines (Bullet, Box2D).
//
// Origin: Baraff & Witkin (SIGGRAPH 1992); Bullet Physics (2003)
type SweepAndPrune struct {
	endpoints  []SAPEndpoint   // All min/max endpoints
	pairs      []CollisionPair // Output buffer (reused)
	active     []uint32        // Active interval set (reused)
	useInsSort bool            // Use insertion sort for temporal coherence
}

// SAPEndpoint represents one end of a bounding interval on the sweep axis.
type SAPEndpoint struct {
	Value    float32 // X coordinate
	EntityID uint32  // Which entity
	IsMin    bool    // true = start of interval, false = end
}

// CollisionPair represents two entities whose bounding intervals overlap.
type CollisionPair struct {
	A, B uint32
}

// SAPEntity is the interface entities must implement for sweep-and-prune.
type SAPEntity interface {
	GetBounds() (minX, maxX float32)
}

// NewSweepAndPrune creates a new sweep-and-prune broad phase.
// maxEntities is used to preallocate buffers.
func NewSweepAndPrune(maxEntities int) *SweepAndPrune {
	return &SweepAndPrune{
		endpoints:  make([]SAPEndpoint, 0, maxEntities*2),
		pairs:      make([]CollisionPair, 0, maxEntities),
		active:     make([]uint32, 0, maxEntities/4),
		useInsSort: true,
	}
}

// UpdateFromSlice rebuilds endpoints from a slice of positions and radii,
// then finds all overlapping pairs.
//
// positions: [entityID] -> (x, y) position
// radius: uniform radius for all entities (AABB half-width)
//
// Returns overlapping pairs. The returned slice is reused on subsequent calls.
func (s *SweepAndPrune) UpdateFromSlice(positions [][2]float32, radius float32) []CollisionPair {
	s.pairs = s.pairs[:0]
	s.endpoints = s.endpoints[:0]

	// Build endpoint list
	for i, pos := range positions {
		x := pos[0]
		s.endpoints = append(s.endpoints,
			SAPEndpoint{x - radius, uint32(i), true},
			SAPEndpoint{x + radius, uint32(i), false},
		)
	}

	// Sort endpoints
	if s.useInsSort && len(s.endpoints) > 1 {
		// Insertion sort: O(n) for nearly-sorted data (temporal coherence)
		insertionSortEndpoints(s.endpoints)
	} else {
		sort.Slice(s.endpoints, func(i, j int) bool {
			return s.endpoints[i].Value < s.endpoints[j].Value
		})
	}

	// Sweep: track active intervals
	s.active = s.active[:0]

	for _, ep := range s.endpoints {
		if ep.IsMin {
			// Starting new interval - pair with all active intervals
			for _, other := range s.active {
				s.pairs = append(s.pairs, CollisionPair{ep.EntityID, other})
			}
			s.active = append(s.active, ep.EntityID)
		} else {
			// Ending interval - remove from active set
			for i, id := range s.active {
				if id == ep.EntityID {
					// Swap with last and truncate
					s.active[i] = s.active[len(s.active)-1]
					s.active = s.active[:len(s.active)-1]
					break
				}
			}
		}
	}

	return s.pairs
}

// Update rebuilds from entities implementing SAPEntity interface.
func (s *SweepAndPrune) Update(entities []SAPEntity) []CollisionPair {
	s.pairs = s.pairs[:0]
	s.endpoints = s.endpoints[:0]

	// Build endpoint list
	for i, e := range entities {
		minX, maxX := e.GetBounds()
		s.endpoints = append(s.endpoints,
			SAPEndpoint{minX, uint32(i), true},
			SAPEndpoint{maxX, uint32(i), false},
		)
	}

	// Sort endpoints
	if s.useInsSort && len(s.endpoints) > 1 {
		insertionSortEndpoints(s.endpoints)
	} else {
		sort.Slice(s.endpoints, func(i, j int) bool {
			return s.endpoints[i].Value < s.endpoints[j].Value
		})
	}

	// Sweep
	s.active = s.active[:0]

	for _, ep := range s.endpoints {
		if ep.IsMin {
			for _, other := range s.active {
				s.pairs = append(s.pairs, CollisionPair{ep.EntityID, other})
			}
			s.active = append(s.active, ep.EntityID)
		} else {
			for i, id := range s.active {
				if id == ep.EntityID {
					s.active[i] = s.active[len(s.active)-1]
					s.active = s.active[:len(s.active)-1]
					break
				}
			}
		}
	}

	return s.pairs
}

// SetInsertionSort enables/disables insertion sort optimization.
// When true (default), uses insertion sort which is O(n) for nearly-sorted data.
// When false, uses Go's standard sort which is O(n log n).
func (s *SweepAndPrune) SetInsertionSort(enabled bool) {
	s.useInsSort = enabled
}

// insertionSortEndpoints sorts endpoints in-place using insertion sort.
// This is O(n) for nearly-sorted data due to temporal coherence.
func insertionSortEndpoints(eps []SAPEndpoint) {
	for i := 1; i < len(eps); i++ {
		key := eps[i]
		j := i - 1
		for j >= 0 && eps[j].Value > key.Value {
			eps[j+1] = eps[j]
			j--
		}
		eps[j+1] = key
	}
}
