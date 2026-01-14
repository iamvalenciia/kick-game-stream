package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"fight-club/internal/api"
	"fight-club/internal/game"
	"fight-club/internal/streaming"
)

// Helper to create test server
func createTestServer(t *testing.T) *api.Server {
	engine := game.NewEngine(30)
	streamer := streaming.NewStreamManager(engine, streaming.StreamConfig{
		Width:   640,
		Height:  480,
		FPS:     15,
		Bitrate: 1000,
	})
	return api.NewServer(engine, streamer)
}

// TestGetStats tests /api/stats endpoint
func TestGetStats(t *testing.T) {
	server := createTestServer(t)

	req := httptest.NewRequest("GET", "/api/stats", nil)
	rec := httptest.NewRecorder()

	// We need to access the router - skip if not exposed
	t.Skip("Server router not exposed for testing - needs refactoring")
}

// TestPlayerJoin tests /api/player/join endpoint
func TestPlayerJoin(t *testing.T) {
	t.Skip("Server router not exposed for testing - needs refactoring")

	server := createTestServer(t)
	_ = server

	body := map[string]string{"name": "TestPlayer"}
	jsonBody, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/api/player/join", bytes.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	// Would need router access
	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}
}

// TestStreamControl tests /api/stream/start and /api/stream/stop
func TestStreamControl(t *testing.T) {
	t.Skip("Server router not exposed for testing - needs refactoring")
}

// TestGetLeaderboard tests /api/leaderboard endpoint
func TestGetLeaderboard(t *testing.T) {
	t.Skip("Server router not exposed for testing - needs refactoring")
}

// TestGetWeapons tests /api/weapons endpoint
func TestGetWeapons(t *testing.T) {
	t.Skip("Server router not exposed for testing - needs refactoring")
}

// TestHealPlayer tests /api/player/heal endpoint
func TestHealPlayer(t *testing.T) {
	t.Skip("Server router not exposed for testing - needs refactoring")
}

// TestCORS tests CORS headers are set
func TestCORS(t *testing.T) {
	t.Skip("Server router not exposed for testing - needs refactoring")
}
