package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"fight-club/internal/game"
)

// Handler methods for routerHandlers
// These are used by both the standalone router (for testing) and the full Server.

func (h *routerHandlers) handleGetState(w http.ResponseWriter, r *http.Request) {
	state := h.engine.GetState()

	players := make([]map[string]interface{}, 0)
	for _, p := range state.Players {
		players = append(players, p.ToJSON())
	}

	writeJSON(w, map[string]interface{}{
		"players":     players,
		"playerCount": state.PlayerCount,
		"aliveCount":  state.AliveCount,
	})
}

func (h *routerHandlers) handleGetStats(w http.ResponseWriter, r *http.Request) {
	// OPTIMIZATION: Use lock-free snapshot instead of GetState()
	// This avoids RWMutex contention and redundant sorting on every poll request
	snapshot := h.engine.GetSnapshot()
	stats := map[string]interface{}{
		"playerCount": snapshot.PlayerCount,
		"aliveCount":  snapshot.AliveCount,
		"totalKills":  snapshot.TotalKills,
		"streaming":   h.streamer.IsStreaming(),
		"streamStats": h.streamer.GetStats(),
	}
	writeJSON(w, stats)
}

func (h *routerHandlers) handleGetLeaderboard(w http.ResponseWriter, r *http.Request) {
	state := h.engine.GetState()

	// Sort by kills (simple bubble sort for now)
	players := state.Players
	for i := 0; i < len(players); i++ {
		for j := i + 1; j < len(players); j++ {
			if players[j].Kills > players[i].Kills {
				players[i], players[j] = players[j], players[i]
			}
		}
	}

	// Top 10
	limit := 10
	if len(players) < limit {
		limit = len(players)
	}

	result := make([]map[string]interface{}, 0)
	for i := 0; i < limit; i++ {
		result = append(result, players[i].ToJSON())
	}

	writeJSON(w, result)
}

func (h *routerHandlers) handlePlayerJoin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name       string `json:"name"`
		ProfilePic string `json:"profilePic"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, "Invalid request", http.StatusBadRequest)
		return
	}

	if req.Name == "" {
		writeError(w, "Name is required", http.StatusBadRequest)
		return
	}

	player := h.engine.AddPlayer(req.Name, game.PlayerOptions{
		ProfilePic: req.ProfilePic,
	})

	// Handle player limit reached (DoS protection)
	if player == nil {
		writeError(w, "Player limit reached", http.StatusServiceUnavailable)
		return
	}

	writeJSON(w, player.ToJSON())
}

func (h *routerHandlers) handlePlayerBatchJoin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Count int `json:"count"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, "Invalid request", http.StatusBadRequest)
		return
	}

	if req.Count <= 0 {
		req.Count = 10 // Default
	}
	if req.Count > 200 {
		req.Count = 200 // Cap
	}

	count := 0
	for i := 0; i < req.Count; i++ {
		// Generate random bot name
		name := fmt.Sprintf("Bot-%d", h.engine.GetSnapshot().TotalKills+i+1000)

		// Add random variance to name to avoid collisions
		if h.engine.GetPlayer(name) != nil {
			name = fmt.Sprintf("Bot-%d-%d", h.engine.GetSnapshot().TotalKills+i, h.engine.GetSnapshot().TickNumber%1000)
		}

		player := h.engine.AddPlayer(name, game.PlayerOptions{})
		if player != nil {
			count++
		}
	}

	writeJSON(w, map[string]interface{}{
		"success": true,
		"count":   count,
		"message": fmt.Sprintf("Successfully added %d bots", count),
	})
}

func (h *routerHandlers) handlePlayerHeal(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name   string `json:"name"`
		Amount int    `json:"amount"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, "Invalid request", http.StatusBadRequest)
		return
	}

	amount := req.Amount
	if amount <= 0 {
		amount = 20
	}

	success := h.engine.HealPlayer(req.Name, amount)
	writeJSON(w, map[string]bool{"success": success})
}

func (h *routerHandlers) handlePlayerWeapon(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name     string `json:"name"`
		WeaponID string `json:"weaponId"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, "Invalid request", http.StatusBadRequest)
		return
	}

	// TODO: Implement weapon buying
	writeJSON(w, map[string]bool{"success": true})
}

func (h *routerHandlers) handleStreamStart(w http.ResponseWriter, r *http.Request) {
	log.Println("📡 Stream start requested via API")
	if err := h.streamer.Start(); err != nil {
		log.Printf("❌ Stream start failed: %v", err)
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]bool{"success": true})
}

func (h *routerHandlers) handleStreamStop(w http.ResponseWriter, r *http.Request) {
	log.Println("📡 Stream stop requested via API")
	h.streamer.Stop()
	writeJSON(w, map[string]bool{"success": true})
}

func (h *routerHandlers) handleStreamStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, h.streamer.GetStats())
}

func (h *routerHandlers) handleGetWeapons(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, game.GetAllWeapons())
}

// Helper functions (package-level for reuse)

func writeJSON(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, message string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}
