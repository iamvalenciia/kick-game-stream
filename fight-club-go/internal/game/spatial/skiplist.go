// Package spatial provides cache-efficient spatial data structures.
//
// This file implements a concurrent skip list with augmented span counts
// for O(log n) rank queries - ideal for real-time leaderboards.
//
// Origin: Pugh (1990), "Skip Lists: A Probabilistic Alternative to Balanced Trees"
// Redis ZSET uses this exact pattern for leaderboards.
package spatial

import (
	"math/rand"
	"sync"
	"sync/atomic"
)

const (
	maxLevel      = 32          // Max skip list height (supports 2^32 elements)
	levelProbability = 0.25     // P=0.25 gives optimal balance
)

// SkipListEntry represents a scored entry in the leaderboard
type SkipListEntry struct {
	Key   string  // Player ID
	Score float64 // Score (kills, points, etc.)
}

// skipNode is a node in the skip list
type skipNode struct {
	entry SkipListEntry
	next  []*skipNode // Forward pointers (one per level)
	span  []int       // Span counts (distance to next node at each level)
}

// SkipList is a concurrent skip list with O(log n) rank queries
// Designed for real-time leaderboards with thousands of entries
type SkipList struct {
	head   *skipNode
	level  int32       // Current max level (atomic for reads)
	length int32       // Number of elements (atomic)
	mu     sync.RWMutex // Mutex for writes (reads can be lock-free for simple lookups)
	rng    *rand.Rand
}

// NewSkipList creates a new concurrent skip list
func NewSkipList() *SkipList {
	head := &skipNode{
		next: make([]*skipNode, maxLevel),
		span: make([]int, maxLevel),
	}
	// Initialize spans to 0 (all point to end initially)
	for i := range head.span {
		head.span[i] = 0
	}

	return &SkipList{
		head:  head,
		level: 1,
		rng:   rand.New(rand.NewSource(rand.Int63())),
	}
}

// randomLevel generates a random level for a new node
// Returns level in range [1, maxLevel] with geometric distribution
func (sl *SkipList) randomLevel() int {
	level := 1
	for level < maxLevel && sl.rng.Float64() < levelProbability {
		level++
	}
	return level
}

// Insert adds or updates an entry in the skip list
// If key already exists, updates the score and repositions
// Time complexity: O(log n) average
func (sl *SkipList) Insert(key string, score float64) {
	sl.mu.Lock()
	defer sl.mu.Unlock()

	// Track update path and rank increments
	update := make([]*skipNode, maxLevel)
	rank := make([]int, maxLevel)

	x := sl.head
	for i := int(atomic.LoadInt32(&sl.level)) - 1; i >= 0; i-- {
		// Store rank at this level
		if i == int(sl.level)-1 {
			rank[i] = 0
		} else {
			rank[i] = rank[i+1]
		}

		// Find insert position at this level
		for x.next[i] != nil && (x.next[i].entry.Score > score ||
			(x.next[i].entry.Score == score && x.next[i].entry.Key < key)) {
			rank[i] += x.span[i]
			x = x.next[i]
		}
		update[i] = x
	}

	// Check if key already exists (for update)
	if x.next[0] != nil && x.next[0].entry.Key == key {
		// Remove old position first
		sl.removeNode(x.next[0], update)
		// Re-insert with new score (recursive but efficient)
		sl.mu.Unlock()
		sl.Insert(key, score)
		sl.mu.Lock()
		return
	}

	// Generate random level for new node
	newLevel := sl.randomLevel()
	currentLevel := int(sl.level)

	// Extend update array if new level is higher
	if newLevel > currentLevel {
		for i := currentLevel; i < newLevel; i++ {
			rank[i] = 0
			update[i] = sl.head
			update[i].span[i] = int(sl.length)
		}
		atomic.StoreInt32(&sl.level, int32(newLevel))
	}

	// Create new node
	node := &skipNode{
		entry: SkipListEntry{Key: key, Score: score},
		next:  make([]*skipNode, newLevel),
		span:  make([]int, newLevel),
	}

	// Insert at each level
	for i := 0; i < newLevel; i++ {
		node.next[i] = update[i].next[i]
		update[i].next[i] = node

		// Calculate span
		node.span[i] = update[i].span[i] - (rank[0] - rank[i])
		update[i].span[i] = (rank[0] - rank[i]) + 1
	}

	// Update spans for levels above new node
	for i := newLevel; i < int(sl.level); i++ {
		update[i].span[i]++
	}

	atomic.AddInt32(&sl.length, 1)
}

// Remove removes an entry by key
// Time complexity: O(log n) average
func (sl *SkipList) Remove(key string) bool {
	sl.mu.Lock()
	defer sl.mu.Unlock()

	update := make([]*skipNode, maxLevel)
	x := sl.head

	for i := int(sl.level) - 1; i >= 0; i-- {
		for x.next[i] != nil && x.next[i].entry.Key < key {
			x = x.next[i]
		}
		update[i] = x
	}

	x = x.next[0]
	if x == nil || x.entry.Key != key {
		return false
	}

	sl.removeNode(x, update)
	return true
}

// removeNode removes a node given its update path
func (sl *SkipList) removeNode(node *skipNode, update []*skipNode) {
	for i := 0; i < int(sl.level); i++ {
		if update[i].next[i] == node {
			update[i].span[i] += node.span[i] - 1
			update[i].next[i] = node.next[i]
		} else {
			update[i].span[i]--
		}
	}

	// Reduce level if needed
	for sl.level > 1 && sl.head.next[sl.level-1] == nil {
		atomic.AddInt32(&sl.level, -1)
	}

	atomic.AddInt32(&sl.length, -1)
}

// GetRank returns the rank of a key (1-indexed, 1 = highest score)
// Returns 0 if key not found
// Time complexity: O(log n)
func (sl *SkipList) GetRank(key string) int {
	sl.mu.RLock()
	defer sl.mu.RUnlock()

	rank := 0
	x := sl.head

	for i := int(sl.level) - 1; i >= 0; i-- {
		for x.next[i] != nil && x.next[i].entry.Key <= key {
			rank += x.span[i]
			x = x.next[i]
			if x.entry.Key == key {
				return rank
			}
		}
	}

	return 0 // Not found
}

// GetByRank returns the entry at a given rank (1-indexed)
// Time complexity: O(log n)
func (sl *SkipList) GetByRank(rank int) *SkipListEntry {
	sl.mu.RLock()
	defer sl.mu.RUnlock()

	if rank <= 0 || rank > int(sl.length) {
		return nil
	}

	traversed := 0
	x := sl.head

	for i := int(sl.level) - 1; i >= 0; i-- {
		for x.next[i] != nil && traversed+x.span[i] <= rank {
			traversed += x.span[i]
			x = x.next[i]
		}
		if traversed == rank {
			return &x.entry
		}
	}

	return nil
}

// GetRange returns entries in rank range [start, end] (1-indexed, inclusive)
// Time complexity: O(log n + k) where k is range size
func (sl *SkipList) GetRange(start, end int) []SkipListEntry {
	sl.mu.RLock()
	defer sl.mu.RUnlock()

	if start <= 0 {
		start = 1
	}
	if end > int(sl.length) {
		end = int(sl.length)
	}
	if start > end {
		return nil
	}

	result := make([]SkipListEntry, 0, end-start+1)

	// Find start position
	traversed := 0
	x := sl.head

	for i := int(sl.level) - 1; i >= 0; i-- {
		for x.next[i] != nil && traversed+x.span[i] < start {
			traversed += x.span[i]
			x = x.next[i]
		}
	}

	// Collect entries from start to end
	x = x.next[0]
	for x != nil && traversed < end {
		traversed++
		if traversed >= start {
			result = append(result, x.entry)
		}
		x = x.next[0]
	}

	return result
}

// GetScore returns the score for a key
// Returns (score, true) if found, (0, false) if not
func (sl *SkipList) GetScore(key string) (float64, bool) {
	sl.mu.RLock()
	defer sl.mu.RUnlock()

	x := sl.head
	for i := int(sl.level) - 1; i >= 0; i-- {
		for x.next[i] != nil && x.next[i].entry.Key < key {
			x = x.next[i]
		}
	}

	x = x.next[0]
	if x != nil && x.entry.Key == key {
		return x.entry.Score, true
	}
	return 0, false
}

// Length returns the number of entries
func (sl *SkipList) Length() int {
	return int(atomic.LoadInt32(&sl.length))
}

// Clear removes all entries
func (sl *SkipList) Clear() {
	sl.mu.Lock()
	defer sl.mu.Unlock()

	for i := range sl.head.next {
		sl.head.next[i] = nil
		sl.head.span[i] = 0
	}
	atomic.StoreInt32(&sl.level, 1)
	atomic.StoreInt32(&sl.length, 0)
}

// ForEach iterates over all entries in rank order (highest score first)
// Time complexity: O(n)
func (sl *SkipList) ForEach(fn func(rank int, entry SkipListEntry) bool) {
	sl.mu.RLock()
	defer sl.mu.RUnlock()

	rank := 0
	x := sl.head.next[0]
	for x != nil {
		rank++
		if !fn(rank, x.entry) {
			break
		}
		x = x.next[0]
	}
}
