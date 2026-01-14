package kick

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// Kick API endpoints
	APIBase   = "https://api.kick.com/public/v1"
	APIBaseV2 = "https://api.kick.com/public/v2"
	AuthBase  = "https://id.kick.com"

	// Token file for persistence
	TokenFileName = ".kick-tokens-go.json"
)

// Service handles Kick API authentication and webhook events
type Service struct {
	mu sync.RWMutex

	// OAuth tokens
	accessToken  string
	refreshToken string
	tokenExpiry  time.Time

	// User info
	userID          int64
	broadcasterID   int64
	broadcasterSlug string
	chatroomID      int // Cached chatroom ID from webhooks

	// PKCE state
	codeVerifier string
	pkceState    string

	// Config
	clientID     string
	clientSecret string
	webhookURL   string

	// HTTP client
	client *http.Client

	// Event handlers
	onChatMessage func(msg ChatMessage)

	// Status
	isConnected bool
}

// TokenData for persistence
type TokenData struct {
	AccessToken   string    `json:"access_token"`
	RefreshToken  string    `json:"refresh_token"`
	TokenExpiry   time.Time `json:"token_expiry"`
	UserID        int64     `json:"user_id"`
	BroadcasterID int64     `json:"broadcaster_id"`
}

// ChatMessage represents a parsed chat message from webhook
type ChatMessage struct {
	MessageID     string
	Content       string
	Username      string
	UserID        int64
	ProfilePic    string
	IsCommand     bool
	Command       string
	Args          []string
	BroadcasterID int64
	CreatedAt     time.Time
}

// WebhookChatPayload matches Kick webhook structure
type WebhookChatPayload struct {
	MessageID   string `json:"message_id"`
	Content     string `json:"content"`
	CreatedAt   string `json:"created_at"`
	ChatroomID  int    `json:"chatroom_id"` // Parsed from webhook
	Broadcaster struct {
		UserID         int64  `json:"user_id"`
		Username       string `json:"username"`
		ProfilePicture string `json:"profile_picture"`
	} `json:"broadcaster"`
	Sender struct {
		UserID         int64  `json:"user_id"`
		Username       string `json:"username"`
		ProfilePicture string `json:"profile_picture"`
		ChannelSlug    string `json:"channel_slug"`
	} `json:"sender"`
}

// NewService creates a new Kick service
func NewService(clientID, clientSecret string) *Service {
	s := &Service{
		clientID:     clientID,
		clientSecret: clientSecret,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}

	// Try to load saved tokens
	s.loadTokens()

	return s
}

// SetWebhookURL sets the public URL for receiving webhooks
func (s *Service) SetWebhookURL(url string) {
	s.mu.Lock()
	s.webhookURL = url
	s.mu.Unlock()
}

// SetBroadcasterID sets the broadcaster ID manually
func (s *Service) SetBroadcasterID(id int64) {
	s.mu.Lock()
	s.broadcasterID = id
	s.mu.Unlock()
}

// OnChatMessage registers a handler for chat messages
func (s *Service) OnChatMessage(handler func(ChatMessage)) {
	s.mu.Lock()
	s.onChatMessage = handler
	s.mu.Unlock()
}

// generateRandomString creates a cryptographically random string
func generateRandomString(length int) string {
	b := make([]byte, length)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)[:length]
}

// generateCodeChallenge creates S256 code challenge from verifier
func generateCodeChallenge(verifier string) string {
	hash := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(hash[:])
}

// GetAuthURL generates OAuth authorization URL with PKCE
func (s *Service) GetAuthURL(redirectURI string) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Generate PKCE values
	s.codeVerifier = generateRandomString(64)
	s.pkceState = generateRandomString(32)
	codeChallenge := generateCodeChallenge(s.codeVerifier)

	scopes := []string{
		"user:read",
		"channel:read",
		"chat:write",
		"events:subscribe",
		"channel:write",
	}

	params := url.Values{
		"client_id":             {s.clientID},
		"redirect_uri":          {redirectURI},
		"response_type":         {"code"},
		"scope":                 {strings.Join(scopes, " ")},
		"state":                 {s.pkceState},
		"code_challenge":        {codeChallenge},
		"code_challenge_method": {"S256"},
	}

	return AuthBase + "/oauth/authorize?" + params.Encode()
}

// ExchangeCode exchanges authorization code for access token
func (s *Service) ExchangeCode(code, redirectURI, state string) error {
	// First, verify PKCE state under lock
	s.mu.Lock()
	pkceState := s.pkceState
	codeVerifier := s.codeVerifier
	clientID := s.clientID
	clientSecret := s.clientSecret
	s.mu.Unlock()

	// Verify state
	if state != pkceState {
		return errors.New("state mismatch - possible CSRF attack")
	}

	if codeVerifier == "" {
		return errors.New("no code verifier - OAuth flow not initiated")
	}

	data := url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {clientID},
		"client_secret": {clientSecret},
		"redirect_uri":  {redirectURI},
		"code":          {code},
		"code_verifier": {codeVerifier},
	}

	resp, err := s.client.PostForm(AuthBase+"/oauth/token", data)
	if err != nil {
		return fmt.Errorf("token request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("token exchange failed: %d - %s", resp.StatusCode, string(body))
	}

	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
		TokenType    string `json:"token_type"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return fmt.Errorf("failed to decode token response: %w", err)
	}

	// Update state under lock
	s.mu.Lock()
	s.accessToken = tokenResp.AccessToken
	s.refreshToken = tokenResp.RefreshToken
	s.tokenExpiry = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	// Clear PKCE state
	s.codeVerifier = ""
	s.pkceState = ""
	s.isConnected = true
	s.mu.Unlock()

	log.Println("✅ Kick OAuth token obtained")

	// Get user info (no lock held - getUserInfo manages its own locking)
	if err := s.getUserInfo(); err != nil {
		log.Printf("⚠️ Failed to get user info: %v", err)
	}

	// Save tokens (no lock held - saveTokens manages its own locking)
	s.saveTokens()

	return nil
}

// AuthenticateClient uses client credentials flow (for server-to-server)
func (s *Service) AuthenticateClient() error {
	data := url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {s.clientID},
		"client_secret": {s.clientSecret},
		"scope":         {"user:read events:subscribe"},
	}

	resp, err := s.client.PostForm(AuthBase+"/oauth/token", data)
	if err != nil {
		return fmt.Errorf("client auth failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("client auth failed: %d - %s", resp.StatusCode, string(body))
	}

	var tokenResp struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return fmt.Errorf("failed to decode token: %w", err)
	}

	s.mu.Lock()
	s.accessToken = tokenResp.AccessToken
	s.tokenExpiry = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	s.isConnected = true
	s.mu.Unlock()

	log.Println("✅ Kick client credentials authenticated")
	return nil
}

// RefreshToken refreshes the access token
func (s *Service) RefreshToken() error {
	s.mu.RLock()
	refreshToken := s.refreshToken
	s.mu.RUnlock()

	if refreshToken == "" {
		return errors.New("no refresh token available")
	}

	data := url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {s.clientID},
		"client_secret": {s.clientSecret},
		"refresh_token": {refreshToken},
	}

	resp, err := s.client.PostForm(AuthBase+"/oauth/token", data)
	if err != nil {
		return fmt.Errorf("token refresh failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("token refresh failed: %d - %s", resp.StatusCode, string(body))
	}

	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return fmt.Errorf("failed to decode token: %w", err)
	}

	s.mu.Lock()
	s.accessToken = tokenResp.AccessToken
	s.refreshToken = tokenResp.RefreshToken
	s.tokenExpiry = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	s.mu.Unlock()

	log.Println("✅ Kick token refreshed")
	s.saveTokens()

	return nil
}

// getUserInfo fetches current user info
func (s *Service) getUserInfo() error {
	resp, err := s.apiRequest("GET", "/users/me", nil)
	if err != nil {
		return err
	}

	var result struct {
		Data []struct {
			UserID int64  `json:"user_id"`
			Name   string `json:"name"`
		} `json:"data"`
	}

	if err := json.Unmarshal(resp, &result); err != nil {
		return err
	}

	if len(result.Data) > 0 {
		s.userID = result.Data[0].UserID
		if s.broadcasterID == 0 {
			s.broadcasterID = result.Data[0].UserID
		}
		log.Printf("✅ Kick user: %s (ID: %d)", result.Data[0].Name, result.Data[0].UserID)
	}

	return nil
}

// GetUserProfilePicture fetches a user's profile picture URL by their user ID
func (s *Service) GetUserProfilePicture(userID int64) (string, error) {
	endpoint := fmt.Sprintf("/users?id=%d", userID)
	resp, err := s.apiRequest("GET", endpoint, nil)
	if err != nil {
		return "", err
	}

	var result struct {
		Data []struct {
			UserID         int64  `json:"user_id"`
			Name           string `json:"name"`
			ProfilePicture string `json:"profile_picture"`
		} `json:"data"`
	}

	if err := json.Unmarshal(resp, &result); err != nil {
		return "", err
	}

	if len(result.Data) > 0 && result.Data[0].ProfilePicture != "" {
		return result.Data[0].ProfilePicture, nil
	}

	return "", nil
}

// apiRequest makes an authenticated API request
func (s *Service) apiRequest(method, endpoint string, body interface{}) ([]byte, error) {
	s.mu.RLock()
	token := s.accessToken
	expiry := s.tokenExpiry
	s.mu.RUnlock()

	// Refresh if needed
	if time.Now().Add(time.Minute).After(expiry) {
		if err := s.RefreshToken(); err != nil {
			return nil, fmt.Errorf("token refresh failed: %w", err)
		}
		s.mu.RLock()
		token = s.accessToken
		s.mu.RUnlock()
	}

	if token == "" {
		return nil, errors.New("not authenticated")
	}

	var bodyReader io.Reader
	if body != nil {
		jsonBody, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		bodyReader = bytes.NewReader(jsonBody)
	}

	req, err := http.NewRequest(method, APIBase+endpoint, bodyReader)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(respBody))
	}

	return respBody, nil
}

// SubscribeToChatEvents subscribes to chat message events via webhook
func (s *Service) SubscribeToChatEvents() error {
	s.mu.RLock()
	broadcasterID := s.broadcasterID
	webhookURL := s.webhookURL
	s.mu.RUnlock()

	if broadcasterID == 0 {
		return errors.New("broadcaster ID not set")
	}

	// Note: Kick webhooks are registered against your app,
	// the callback URL is set in the Kick Developer Dashboard
	body := map[string]interface{}{
		"events": []map[string]interface{}{
			{"name": "chat.message.sent", "version": 1},
		},
		"method":              "webhook",
		"broadcaster_user_id": broadcasterID,
	}

	resp, err := s.apiRequest("POST", "/events/subscriptions", body)
	if err != nil {
		return fmt.Errorf("subscription failed: %w", err)
	}

	log.Printf("✅ Subscribed to Kick chat events (broadcaster: %d)", broadcasterID)
	log.Printf("📡 Webhook URL should be configured in Kick Dashboard: %s", webhookURL)

	var result map[string]interface{}
	json.Unmarshal(resp, &result)

	return nil
}

// GetSubscriptions returns current event subscriptions
func (s *Service) GetSubscriptions() ([]map[string]interface{}, error) {
	resp, err := s.apiRequest("GET", "/events/subscriptions", nil)
	if err != nil {
		return nil, err
	}

	var result struct {
		Data []map[string]interface{} `json:"data"`
	}
	json.Unmarshal(resp, &result)

	return result.Data, nil
}

// HandleWebhook processes incoming webhook requests
func (s *Service) HandleWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Read body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read body", http.StatusBadRequest)
		return
	}

	// Get event type from header
	eventType := r.Header.Get("Kick-Event-Type")
	log.Printf("📨 Kick webhook: %s", eventType)

	// Handle chat message
	if eventType == "chat.message.sent" {
		var payload WebhookChatPayload
		if err := json.Unmarshal(body, &payload); err != nil {
			log.Printf("⚠️ Failed to parse chat webhook: %v", err)
			http.Error(w, "Invalid payload", http.StatusBadRequest)
			return
		}

		// Capture chatroom ID if present
		if payload.ChatroomID != 0 {
			s.mu.Lock()
			if s.chatroomID == 0 {
				log.Printf("✅ Captured Chatroom ID: %d", payload.ChatroomID)
			}
			s.chatroomID = payload.ChatroomID
			s.mu.Unlock()
		}

		msg := ChatMessage{
			MessageID:     payload.MessageID,
			Content:       payload.Content,
			Username:      payload.Sender.Username,
			UserID:        payload.Sender.UserID,
			ProfilePic:    payload.Sender.ProfilePicture,
			BroadcasterID: payload.Broadcaster.UserID,
		}

		// Parse command
		if strings.HasPrefix(payload.Content, "!") {
			parts := strings.Fields(payload.Content[1:])
			if len(parts) > 0 {
				msg.IsCommand = true
				msg.Command = strings.ToLower(parts[0])
				msg.Args = parts[1:]
			}
		}

		// Debug logging
		if msg.IsCommand {
			log.Printf("💬 [%s] COMMAND: !%s (UserID: %d, ProfilePic: %v)",
				msg.Username, msg.Command, msg.UserID, msg.ProfilePic != "")
		} else {
			log.Printf("💬 [%s]: %s", msg.Username, msg.Content)
		}

		// Call handler
		s.mu.RLock()
		handler := s.onChatMessage
		s.mu.RUnlock()

		if handler != nil {
			handler(msg)
		}
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

// IsConnected returns connection status
func (s *Service) IsConnected() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.isConnected
}

// saveTokens persists tokens to disk
func (s *Service) saveTokens() {
	s.mu.RLock()
	data := TokenData{
		AccessToken:   s.accessToken,
		RefreshToken:  s.refreshToken,
		TokenExpiry:   s.tokenExpiry,
		UserID:        s.userID,
		BroadcasterID: s.broadcasterID,
	}
	s.mu.RUnlock()

	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		log.Printf("⚠️ Failed to marshal tokens: %v", err)
		return
	}

	// Save to current directory
	if err := os.WriteFile(TokenFileName, jsonData, 0600); err != nil {
		// Try parent directory
		parentPath := filepath.Join("..", TokenFileName)
		if err := os.WriteFile(parentPath, jsonData, 0600); err != nil {
			log.Printf("⚠️ Failed to save tokens: %v", err)
			return
		}
	}

	log.Println("💾 Kick tokens saved")
}

// loadTokens loads persisted tokens from disk
func (s *Service) loadTokens() {
	// Try current directory first
	data, err := os.ReadFile(TokenFileName)
	if err != nil {
		// Try parent directory
		data, err = os.ReadFile(filepath.Join("..", TokenFileName))
		if err != nil {
			return // No saved tokens
		}
	}

	var tokens TokenData
	if err := json.Unmarshal(data, &tokens); err != nil {
		log.Printf("⚠️ Failed to parse saved tokens: %v", err)
		return
	}

	s.mu.Lock()
	s.accessToken = tokens.AccessToken
	s.refreshToken = tokens.RefreshToken
	s.tokenExpiry = tokens.TokenExpiry
	s.userID = tokens.UserID
	s.broadcasterID = tokens.BroadcasterID
	s.mu.Unlock()

	if tokens.AccessToken != "" && time.Now().Before(tokens.TokenExpiry) {
		s.isConnected = true
		log.Println("📂 Loaded valid Kick tokens from disk")
	} else if tokens.RefreshToken != "" {
		log.Println("📂 Loaded expired tokens, refreshing...")
		if err := s.RefreshToken(); err != nil {
			log.Printf("⚠️ Token refresh failed: %v", err)
		}
	}
}

// SendMessage sends a message to the chat
// Uses official Kick API: POST /public/v1/chat
func (s *Service) SendMessage(content string) error {
	s.mu.RLock()
	broadcasterID := s.broadcasterID
	s.mu.RUnlock()

	if broadcasterID == 0 {
		return errors.New("broadcaster ID not set")
	}

	log.Printf("📨 Sending chat message as user (broadcaster: %d): %s", broadcasterID, content)

	// Official API endpoint: POST /public/v1/chat
	// Documentation: https://docs.kick.com
	// Sending as USER (not bot):
	//   - type: "user"
	//   - broadcaster_user_id: REQUIRED (the channel where message will be posted)

	body := map[string]interface{}{
		"content":             content,
		"type":                "user",
		"broadcaster_user_id": broadcasterID,
	}

	endpoint := "/chat"
	respBytes, err := s.apiRequest("POST", endpoint, body)
	if err != nil {
		return fmt.Errorf("failed to send message: %w", err)
	}

	log.Printf("✅ Chat API response: %s", string(respBytes))

	return nil
}

func (s *Service) SendBotMessage(content string) error {
	// For bot messages, broadcaster_user_id is NOT required per Kick API docs:
	// "When sending as a bot, the broadcaster_user_id is not required and is ignored.
	// As a bot, the message will always be sent to the channel attached to your token."

	log.Printf("🤖 Sending BOT message: %s", content)

	// Bot type messages only need content and type
	body := map[string]interface{}{
		"content": content,
		"type":    "bot",
	}

	endpoint := "/chat"
	respBytes, err := s.apiRequest("POST", endpoint, body)
	if err != nil {
		return fmt.Errorf("failed to send bot message: %w", err)
	}

	log.Printf("✅ Chat API response (BOT): %s", string(respBytes))

	return nil
}

// SendChatroomMessage sends a message using chatroom_id
func (s *Service) SendChatroomMessage(content string) error {
	s.mu.RLock()
	chatroomID := s.chatroomID
	s.mu.RUnlock()

	if chatroomID == 0 {
		return errors.New("chatroom ID not initialized")
	}

	log.Printf("📨 Sending CHATROOM message (chatroom: %d): %s", chatroomID, content)

	body := map[string]interface{}{
		"content":     content,
		"type":        "user",
		"chatroom_id": chatroomID,
	}

	endpoint := "/chat"
	respBytes, err := s.apiRequest("POST", endpoint, body)
	if err != nil {
		return fmt.Errorf("failed to send chatroom message: %w", err)
	}

	log.Printf("✅ Chat API response (Chatroom): %s", string(respBytes))

	return nil
}

// Category represents a Kick category
type Category struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Slug     string `json:"slug"`
	ParentID int    `json:"parent_id"`
}

// SearchCategory searches for a category by name using v2 API
func (s *Service) SearchCategory(name string) (*Category, error) {
	// API v2: GET /public/v2/categories?names=["Name"]

	namesJSON := fmt.Sprintf("[\"%s\"]", name)
	params := url.Values{}
	params.Add("names", namesJSON)

	endpoint := APIBaseV2 + "/categories?" + params.Encode()

	req, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return nil, err
	}

	s.mu.RLock()
	token := s.accessToken
	s.mu.RUnlock()

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("search category failed: %d - %s", resp.StatusCode, string(body))
	}

	var result struct {
		Data []Category `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	if len(result.Data) == 0 {
		return nil, fmt.Errorf("category not found: %s", name)
	}

	return &result.Data[0], nil
}

// SetCategory updates the channel's category
func (s *Service) SetCategory(categoryName string) error {
	// 1. Search for category to get ID
	cat, err := s.SearchCategory(categoryName)
	if err != nil {
		return fmt.Errorf("could not find category '%s': %w", categoryName, err)
	}

	// 2. Update Channel using v1 API: PATCH /public/v1/channels
	body := map[string]interface{}{
		"category_id": cat.ID,
	}

	endpoint := "/channels"
	respBytes, err := s.apiRequest("PATCH", endpoint, body)
	if err != nil {
		return fmt.Errorf("failed to update category: %w", err)
	}

	log.Printf("✅ Category updated to: %s (ID: %d)", cat.Name, cat.ID)
	log.Printf("📝 Response: %s", string(respBytes))
	return nil
}

// getChannelID fetches the channel ID
func (s *Service) getChannelID() (int64, error) {
	s.mu.RLock()
	broadcasterID := s.broadcasterID
	s.mu.RUnlock()

	if broadcasterID == 0 {
		return 0, errors.New("broadcaster ID not set")
	}

	endpoint := fmt.Sprintf("/users/%d", broadcasterID)
	resp, err := s.apiRequest("GET", endpoint, nil)
	if err != nil {
		return 0, err
	}

	var userResp struct {
		StreamerChannel struct {
			ID int64 `json:"id"`
		} `json:"streamer_channel"`
	}

	if err := json.Unmarshal(resp, &userResp); err != nil {
		return 0, err
	}

	if userResp.StreamerChannel.ID == 0 {
		return 0, errors.New("channel ID not found in user response")
	}

	return userResp.StreamerChannel.ID, nil
}

// getChatroomID fetches the chatroom ID for the broadcaster's channel
// Uses the PUBLIC v2 endpoint which doesn't require authentication
func (s *Service) getChatroomID() (int, error) {
	s.mu.RLock()
	broadcasterID := s.broadcasterID
	s.mu.RUnlock()

	if broadcasterID == 0 {
		return 0, errors.New("broadcaster ID not set")
	}

	// 0. Manual Override via Environment Variable (or previously set)
	// Check if the user set KICK_CHATROOM_ID directly to bypass lookup
	if manualIDStr := os.Getenv("KICK_CHATROOM_ID"); manualIDStr != "" {
		if id, err := strconv.Atoi(manualIDStr); err == nil && id != 0 {
			log.Printf("🔧 Using manual KICK_CHATROOM_ID: %d", id)
			return id, nil
		}
	}

	// First, get the channel slug/username from broadcaster ID
	// Try public API v2 endpoint: GET /v2/channels/{slug}
	// We need to get the slug first using v1/users/{id}

	endpoint := fmt.Sprintf("https://api.kick.com/public/v1/users/%d", broadcasterID)
	req, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to create request: %w", err)
	}

	s.mu.RLock()
	token := s.accessToken
	s.mu.RUnlock()

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("failed to get user info: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return 0, fmt.Errorf("user API returned %d: %s", resp.StatusCode, string(body))
	}

	var userResp struct {
		Slug string `json:"slug"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&userResp); err != nil {
		return 0, fmt.Errorf("failed to decode user response: %w", err)
	}

	if userResp.Slug == "" {
		return 0, errors.New("user slug not found")
	}

	// Now get channel info using the slug
	channelEndpoint := fmt.Sprintf("https://api.kick.com/v2/channels/%s", userResp.Slug)
	req2, err := http.NewRequest("GET", channelEndpoint, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to create channel request: %w", err)
	}

	req2.Header.Set("Accept", "application/json")

	resp2, err := s.client.Do(req2)
	if err != nil {
		return 0, fmt.Errorf("failed to get channel info: %w", err)
	}
	defer resp2.Body.Close()

	if resp2.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp2.Body)
		return 0, fmt.Errorf("channel API returned %d: %s", resp2.StatusCode, string(body))
	}

	var channelResp struct {
		ChatroomID int `json:"chatroom_id"`
	}

	if err := json.NewDecoder(resp2.Body).Decode(&channelResp); err != nil {
		return 0, fmt.Errorf("failed to decode channel response: %w", err)
	}

	if channelResp.ChatroomID == 0 {
		return 0, errors.New("chatroom ID not found in channel response")
	}

	log.Printf("🔍 Found chatroom ID %d for channel %s", channelResp.ChatroomID, userResp.Slug)

	return channelResp.ChatroomID, nil
}

// InitializeChatroomID fetches and caches the chatroom ID
func (s *Service) InitializeChatroomID() error {
	chatroomID, err := s.getChatroomID()
	if err != nil {
		return fmt.Errorf("failed to get chatroom ID: %w", err)
	}

	s.mu.Lock()
	s.chatroomID = chatroomID
	s.mu.Unlock()

	log.Printf("✅ Chatroom ID initialized: %d", chatroomID)
	return nil
}

// HasChatroomID checks if we have a valid chatroom ID cached
func (s *Service) HasChatroomID() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.chatroomID != 0
}

// InvalidateChatroomID clears the cached chatroom ID to force a refresh on next use
func (s *Service) InvalidateChatroomID() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.chatroomID = 0
	log.Println("⚠️ Chatroom ID invalidated due to API error")
}
