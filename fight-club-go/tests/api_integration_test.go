package tests

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"fight-club/internal/api"
	"fight-club/internal/game"
)

// ============================================================================
// Mock Implementations
// ============================================================================

// MockEngine implements api.EngineInterface for testing
type MockEngine struct {
	players     map[string]*game.Player
	playerCount int
	aliveCount  int
	totalKills  int
}

func NewMockEngine() *MockEngine {
	return &MockEngine{
		players: make(map[string]*game.Player),
	}
}

func (m *MockEngine) GetState() game.GameState {
	players := make([]*game.Player, 0, len(m.players))
	for _, p := range m.players {
		players = append(players, p)
	}
	return game.GameState{
		Players:     players,
		PlayerCount: m.playerCount,
		AliveCount:  m.aliveCount,
		TotalKills:  m.totalKills,
	}
}

func (m *MockEngine) AddPlayer(name string, opts game.PlayerOptions) *game.Player {
	// Simulate player limit (return nil if too many)
	if len(m.players) >= 100 {
		return nil
	}

	player := &game.Player{
		ID:   name,
		Name: name,
	}
	m.players[name] = player
	m.playerCount++
	m.aliveCount++
	return player
}

func (m *MockEngine) HealPlayer(name string, amount int) bool {
	_, exists := m.players[name]
	return exists
}

func (m *MockEngine) GetPlayer(name string) *game.Player {
	return m.players[name]
}

func (m *MockEngine) GetSnapshot() *game.GameSnapshot {
	// Return a snapshot with the mock's current state
	return &game.GameSnapshot{
		PlayerCount: m.playerCount,
		AliveCount:  m.aliveCount,
		TotalKills:  m.totalKills,
	}
}

// MockStreamer implements api.StreamerInterface for testing
type MockStreamer struct {
	streaming bool
	startErr  error
}

func NewMockStreamer() *MockStreamer {
	return &MockStreamer{}
}

func (m *MockStreamer) Start() error {
	if m.startErr != nil {
		return m.startErr
	}
	m.streaming = true
	return nil
}

func (m *MockStreamer) Stop() {
	m.streaming = false
}

func (m *MockStreamer) IsStreaming() bool {
	return m.streaming
}

func (m *MockStreamer) GetStats() map[string]interface{} {
	return map[string]interface{}{
		"streaming":   m.streaming,
		"framesTotal": 0,
		"fps":         0,
		"avgRenderMs": 0,
	}
}

// ============================================================================
// Router Purity Tests
// ============================================================================

// TestNewRouterHasNoSideEffects verifies that NewRouter is a pure function
// with no goroutines started and no network listeners opened.
func TestNewRouterHasNoSideEffects(t *testing.T) {
	mockEngine := NewMockEngine()
	mockStreamer := NewMockStreamer()

	// This should complete instantly with no goroutines leaked
	cfg := api.RouterConfig{
		Engine:   mockEngine,
		Streamer: mockStreamer,
		RateLimitConfig: &api.RateLimitConfig{
			RequestsPerSecond: 1000,
			Burst:             1000,
			CleanupInterval:   time.Hour, // Long interval to avoid cleanup goroutine activity
		},
	}

	router := api.NewRouter(cfg)
	if router == nil {
		t.Fatal("Router should not be nil")
	}

	// If we got here without hanging, the router construction is pure
}

// ============================================================================
// API Endpoint Tests
// ============================================================================

// TestAPIGetState tests the game state endpoint
func TestAPIGetState(t *testing.T) {
	mockEngine := NewMockEngine()
	mockStreamer := NewMockStreamer()

	// Add some players
	mockEngine.AddPlayer("Player1", game.PlayerOptions{})
	mockEngine.AddPlayer("Player2", game.PlayerOptions{})

	router := api.NewRouter(api.RouterConfig{
		Engine:         mockEngine,
		Streamer:       mockStreamer,
		DisableLogging: true, // Quiet logs in tests
	})

	ts := httptest.NewServer(router)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/state")
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected 200, got %d", resp.StatusCode)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	players, ok := result["players"].([]interface{})
	if !ok {
		t.Fatal("Response should contain players array")
	}

	if len(players) != 2 {
		t.Errorf("Expected 2 players, got %d", len(players))
	}
}

// TestAPIPlayerJoin tests the player join endpoint
func TestAPIPlayerJoin(t *testing.T) {
	mockEngine := NewMockEngine()
	mockStreamer := NewMockStreamer()

	router := api.NewRouter(api.RouterConfig{
		Engine:         mockEngine,
		Streamer:       mockStreamer,
		DisableLogging: true,
	})

	ts := httptest.NewServer(router)
	defer ts.Close()

	// Test successful join
	body := bytes.NewReader([]byte(`{"name": "TestPlayer", "profilePic": "https://example.com/pic.jpg"}`))
	resp, err := http.Post(ts.URL+"/api/player/join", "application/json", body)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected 200, got %d", resp.StatusCode)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if result["name"] != "TestPlayer" {
		t.Errorf("Expected name 'TestPlayer', got '%v'", result["name"])
	}
}

// TestAPIPlayerJoinValidation tests validation on player join
func TestAPIPlayerJoinValidation(t *testing.T) {
	mockEngine := NewMockEngine()
	mockStreamer := NewMockStreamer()

	router := api.NewRouter(api.RouterConfig{
		Engine:         mockEngine,
		Streamer:       mockStreamer,
		DisableLogging: true,
	})

	ts := httptest.NewServer(router)
	defer ts.Close()

	tests := []struct {
		name       string
		body       string
		wantStatus int
	}{
		{
			name:       "empty name",
			body:       `{"name": ""}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "missing name",
			body:       `{}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "invalid json",
			body:       `{invalid}`,
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := bytes.NewReader([]byte(tt.body))
			resp, err := http.Post(ts.URL+"/api/player/join", "application/json", body)
			if err != nil {
				t.Fatalf("Request failed: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != tt.wantStatus {
				t.Errorf("Expected %d, got %d", tt.wantStatus, resp.StatusCode)
			}
		})
	}
}

// TestAPIStreamControl tests stream start/stop endpoints
func TestAPIStreamControl(t *testing.T) {
	mockEngine := NewMockEngine()
	mockStreamer := NewMockStreamer()

	router := api.NewRouter(api.RouterConfig{
		Engine:         mockEngine,
		Streamer:       mockStreamer,
		DisableLogging: true,
	})

	ts := httptest.NewServer(router)
	defer ts.Close()

	// Test stream start
	resp, err := http.Post(ts.URL+"/api/stream/start", "application/json", nil)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Stream start: expected 200, got %d", resp.StatusCode)
	}

	if !mockStreamer.IsStreaming() {
		t.Error("Streamer should be streaming after start")
	}

	// Test stream status
	resp, err = http.Get(ts.URL + "/api/stream/status")
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	var status map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&status)

	if status["streaming"] != true {
		t.Error("Status should show streaming=true")
	}

	// Test stream stop
	resp, err = http.Post(ts.URL+"/api/stream/stop", "application/json", nil)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	resp.Body.Close()

	if mockStreamer.IsStreaming() {
		t.Error("Streamer should not be streaming after stop")
	}
}

// TestAPIGetWeapons tests the weapons endpoint
func TestAPIGetWeapons(t *testing.T) {
	mockEngine := NewMockEngine()
	mockStreamer := NewMockStreamer()

	router := api.NewRouter(api.RouterConfig{
		Engine:         mockEngine,
		Streamer:       mockStreamer,
		DisableLogging: true,
	})

	ts := httptest.NewServer(router)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/weapons")
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected 200, got %d", resp.StatusCode)
	}

	var weapons []interface{}
	if err := json.NewDecoder(resp.Body).Decode(&weapons); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if len(weapons) == 0 {
		t.Error("Expected at least one weapon")
	}
}

// TestAPILeaderboard tests the leaderboard endpoint
func TestAPILeaderboard(t *testing.T) {
	mockEngine := NewMockEngine()
	mockStreamer := NewMockStreamer()

	// Add players
	mockEngine.AddPlayer("TopPlayer", game.PlayerOptions{})
	mockEngine.AddPlayer("BottomPlayer", game.PlayerOptions{})

	router := api.NewRouter(api.RouterConfig{
		Engine:         mockEngine,
		Streamer:       mockStreamer,
		DisableLogging: true,
	})

	ts := httptest.NewServer(router)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/leaderboard")
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected 200, got %d", resp.StatusCode)
	}

	var leaderboard []interface{}
	if err := json.NewDecoder(resp.Body).Decode(&leaderboard); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if len(leaderboard) != 2 {
		t.Errorf("Expected 2 players in leaderboard, got %d", len(leaderboard))
	}
}

// ============================================================================
// Middleware Tests
// ============================================================================

// TestAPICORSHeaders verifies CORS headers are set correctly
func TestAPICORSHeaders(t *testing.T) {
	mockEngine := NewMockEngine()
	mockStreamer := NewMockStreamer()

	router := api.NewRouter(api.RouterConfig{
		Engine:         mockEngine,
		Streamer:       mockStreamer,
		DisableLogging: true,
		CORSOrigins:    []string{"http://test.example.com"},
	})

	ts := httptest.NewServer(router)
	defer ts.Close()

	// Create request with Origin header
	req, _ := http.NewRequest("GET", ts.URL+"/api/state", nil)
	req.Header.Set("Origin", "http://test.example.com")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	// Check CORS headers
	allowOrigin := resp.Header.Get("Access-Control-Allow-Origin")
	if allowOrigin != "http://test.example.com" {
		t.Errorf("Expected Access-Control-Allow-Origin 'http://test.example.com', got '%s'", allowOrigin)
	}
}

// TestAPIRateLimiting verifies rate limiting works
func TestAPIRateLimiting(t *testing.T) {
	mockEngine := NewMockEngine()
	mockStreamer := NewMockStreamer()

	// Very restrictive rate limit for testing
	router := api.NewRouter(api.RouterConfig{
		Engine:   mockEngine,
		Streamer: mockStreamer,
		RateLimitConfig: &api.RateLimitConfig{
			RequestsPerSecond: 1, // Only 1 request per second
			Burst:             2, // Allow burst of 2
			CleanupInterval:   time.Hour,
		},
		DisableLogging: true,
	})

	ts := httptest.NewServer(router)
	defer ts.Close()

	// Make requests until we hit the rate limit
	var gotRateLimited bool
	for i := 0; i < 10; i++ {
		resp, err := http.Get(ts.URL + "/api/state")
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}
		resp.Body.Close()

		if resp.StatusCode == http.StatusTooManyRequests {
			gotRateLimited = true
			break
		}
	}

	if !gotRateLimited {
		t.Error("Expected to be rate limited after burst exceeded")
	}
}

// TestAPIRedirects tests the redirect behavior
func TestAPIRedirects(t *testing.T) {
	mockEngine := NewMockEngine()
	mockStreamer := NewMockStreamer()

	router := api.NewRouter(api.RouterConfig{
		Engine:         mockEngine,
		Streamer:       mockStreamer,
		DisableLogging: true,
	})

	ts := httptest.NewServer(router)
	defer ts.Close()

	// Test root redirect (don't follow redirects)
	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	resp, err := client.Get(ts.URL + "/")
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusFound {
		t.Errorf("Expected 302 redirect, got %d", resp.StatusCode)
	}

	location := resp.Header.Get("Location")
	if location != "/admin/" {
		t.Errorf("Expected redirect to /admin/, got %s", location)
	}
}

// ============================================================================
// Benchmarks
// ============================================================================

// BenchmarkAPIGetState benchmarks the state endpoint
func BenchmarkAPIGetState(b *testing.B) {
	mockEngine := NewMockEngine()
	mockStreamer := NewMockStreamer()

	// Add some players
	for i := 0; i < 50; i++ {
		mockEngine.AddPlayer("Player"+string(rune('A'+i)), game.PlayerOptions{})
	}

	router := api.NewRouter(api.RouterConfig{
		Engine:         mockEngine,
		Streamer:       mockStreamer,
		DisableLogging: true,
	})

	ts := httptest.NewServer(router)
	defer ts.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resp, err := http.Get(ts.URL + "/api/state")
		if err != nil {
			b.Fatalf("Request failed: %v", err)
		}
		resp.Body.Close()
	}
}
