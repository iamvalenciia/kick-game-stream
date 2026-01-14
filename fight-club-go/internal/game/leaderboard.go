package game

import (
	"fight-club/internal/game/spatial"
	"sync"
)

// Leaderboard provides O(log n) leaderboard operations using a skip list
// Supports real-time updates, rank queries, and range queries
//
// Operations:
//   - UpdateScore: O(log n)
//   - GetRank: O(log n)
//   - GetTop: O(log n + k)
//   - GetAroundPlayer: O(log n + k)
type Leaderboard struct {
	skipList *spatial.SkipList
	mu       sync.RWMutex // For batch operations
}

// LeaderboardEntry represents a player in the leaderboard
type LeaderboardEntry struct {
	PlayerID string
	Kills    int
	Deaths   int
	Score    float64 // Computed score for ranking
	Rank     int
}

// NewLeaderboard creates a new leaderboard
func NewLeaderboard() *Leaderboard {
	return &Leaderboard{
		skipList: spatial.NewSkipList(),
	}
}

// UpdatePlayer updates a player's score in the leaderboard
// Score is computed as: kills * 100 - deaths * 10 (can be customized)
// O(log n) time complexity
func (lb *Leaderboard) UpdatePlayer(playerID string, kills, deaths int) {
	// Compute score (higher is better)
	// Using kills as primary factor, deaths as penalty
	score := float64(kills)*100.0 - float64(deaths)*10.0

	lb.skipList.Insert(playerID, score)
}

// UpdateScore updates a player's score directly
// O(log n) time complexity
func (lb *Leaderboard) UpdateScore(playerID string, score float64) {
	lb.skipList.Insert(playerID, score)
}

// RemovePlayer removes a player from the leaderboard
// O(log n) time complexity
func (lb *Leaderboard) RemovePlayer(playerID string) {
	lb.skipList.Remove(playerID)
}

// GetRank returns a player's rank (1-indexed, 1 = top)
// Returns 0 if player not found
// O(log n) time complexity
func (lb *Leaderboard) GetRank(playerID string) int {
	return lb.skipList.GetRank(playerID)
}

// GetScore returns a player's score
// Returns (score, true) if found, (0, false) if not
// O(log n) time complexity
func (lb *Leaderboard) GetScore(playerID string) (float64, bool) {
	return lb.skipList.GetScore(playerID)
}

// GetTop returns the top N players
// O(log n + k) where k is the number of players to return
func (lb *Leaderboard) GetTop(n int) []LeaderboardEntry {
	entries := lb.skipList.GetRange(1, n)
	result := make([]LeaderboardEntry, len(entries))

	for i, e := range entries {
		result[i] = LeaderboardEntry{
			PlayerID: e.Key,
			Score:    e.Score,
			Rank:     i + 1,
		}
	}

	return result
}

// GetAtRank returns the player at a specific rank
// O(log n) time complexity
func (lb *Leaderboard) GetAtRank(rank int) *LeaderboardEntry {
	entry := lb.skipList.GetByRank(rank)
	if entry == nil {
		return nil
	}

	return &LeaderboardEntry{
		PlayerID: entry.Key,
		Score:    entry.Score,
		Rank:     rank,
	}
}

// GetAroundPlayer returns players around a specific player
// Returns `above` players ranked higher, the player, and `below` players ranked lower
// O(log n + k) time complexity
func (lb *Leaderboard) GetAroundPlayer(playerID string, above, below int) []LeaderboardEntry {
	rank := lb.skipList.GetRank(playerID)
	if rank == 0 {
		return nil // Player not found
	}

	start := rank - above
	if start < 1 {
		start = 1
	}
	end := rank + below

	entries := lb.skipList.GetRange(start, end)
	result := make([]LeaderboardEntry, len(entries))

	currentRank := start
	for i, e := range entries {
		result[i] = LeaderboardEntry{
			PlayerID: e.Key,
			Score:    e.Score,
			Rank:     currentRank,
		}
		currentRank++
	}

	return result
}

// GetRange returns players in the specified rank range (1-indexed, inclusive)
// O(log n + k) time complexity
func (lb *Leaderboard) GetRange(start, end int) []LeaderboardEntry {
	entries := lb.skipList.GetRange(start, end)
	result := make([]LeaderboardEntry, len(entries))

	currentRank := start
	for i, e := range entries {
		result[i] = LeaderboardEntry{
			PlayerID: e.Key,
			Score:    e.Score,
			Rank:     currentRank,
		}
		currentRank++
	}

	return result
}

// Length returns the number of players in the leaderboard
// O(1) time complexity
func (lb *Leaderboard) Length() int {
	return lb.skipList.Length()
}

// Clear removes all players from the leaderboard
func (lb *Leaderboard) Clear() {
	lb.skipList.Clear()
}

// BatchUpdate updates multiple players efficiently
// Holds lock during entire batch to ensure consistency
func (lb *Leaderboard) BatchUpdate(updates map[string]float64) {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	for playerID, score := range updates {
		lb.skipList.Insert(playerID, score)
	}
}

// ForEach iterates over all players in rank order
// The callback receives rank (1-indexed) and entry
// Return false from callback to stop iteration
func (lb *Leaderboard) ForEach(fn func(rank int, entry LeaderboardEntry) bool) {
	lb.skipList.ForEach(func(rank int, e spatial.SkipListEntry) bool {
		return fn(rank, LeaderboardEntry{
			PlayerID: e.Key,
			Score:    e.Score,
			Rank:     rank,
		})
	})
}
