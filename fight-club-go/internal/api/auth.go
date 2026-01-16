package api

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	// Session cookie name
	SessionCookieName = "fight_club_session"

	// Session duration (24 hours)
	SessionDuration = 24 * time.Hour

	// Cookie settings
	CookieSecure   = false // Set to true in production with HTTPS
	CookieHTTPOnly = true
	CookieSameSite = http.SameSiteLaxMode
)

// AdminSession represents an authenticated admin session
type AdminSession struct {
	UserID        int64     `json:"user_id"`
	Username      string    `json:"username"`
	BroadcasterID int64     `json:"broadcaster_id"`
	CreatedAt     time.Time `json:"created_at"`
	ExpiresAt     time.Time `json:"expires_at"`
}

// SessionManager handles admin session authentication
type SessionManager struct {
	mu sync.RWMutex

	// Active sessions (sessionID -> session)
	sessions map[string]*AdminSession

	// Secret key for signing session cookies
	secretKey []byte

	// Authorized broadcaster ID (only this user can access admin)
	broadcasterID int64
}

// NewSessionManager creates a new session manager
func NewSessionManager(broadcasterID int64) *SessionManager {
	// Generate random secret key for this instance
	secretKey := make([]byte, 32)
	if _, err := rand.Read(secretKey); err != nil {
		log.Printf("⚠️ Failed to generate secret key, using fallback")
		secretKey = []byte("fight-club-default-secret-key-32")
	}

	sm := &SessionManager{
		sessions:      make(map[string]*AdminSession),
		secretKey:     secretKey,
		broadcasterID: broadcasterID,
	}

	// Start cleanup goroutine
	go sm.cleanupExpiredSessions()

	return sm
}

// SetBroadcasterID updates the authorized broadcaster ID
func (sm *SessionManager) SetBroadcasterID(id int64) {
	sm.mu.Lock()
	sm.broadcasterID = id
	sm.mu.Unlock()
	log.Printf("🔐 Admin access authorized for broadcaster ID: %d", id)
}

// CreateSession creates a new admin session for an authenticated user
func (sm *SessionManager) CreateSession(userID int64, username string, broadcasterID int64) (string, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Verify user is the broadcaster
	if sm.broadcasterID != 0 && userID != sm.broadcasterID {
		return "", fmt.Errorf("unauthorized: user %d is not the broadcaster (%d)", userID, sm.broadcasterID)
	}

	// Generate session ID
	sessionID := generateSessionID()

	// Create session
	session := &AdminSession{
		UserID:        userID,
		Username:      username,
		BroadcasterID: broadcasterID,
		CreatedAt:     time.Now(),
		ExpiresAt:     time.Now().Add(SessionDuration),
	}

	sm.sessions[sessionID] = session

	log.Printf("🔐 Admin session created for user: %s (ID: %d)", username, userID)

	return sessionID, nil
}

// GetSession retrieves a session by ID
func (sm *SessionManager) GetSession(sessionID string) *AdminSession {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	session, exists := sm.sessions[sessionID]
	if !exists {
		return nil
	}

	// Check if expired
	if time.Now().After(session.ExpiresAt) {
		return nil
	}

	return session
}

// DeleteSession removes a session
func (sm *SessionManager) DeleteSession(sessionID string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	delete(sm.sessions, sessionID)
}

// ValidateSession checks if a request has a valid session
func (sm *SessionManager) ValidateSession(r *http.Request) *AdminSession {
	cookie, err := r.Cookie(SessionCookieName)
	if err != nil {
		return nil
	}

	// Decode and verify cookie
	sessionID, err := sm.decodeCookie(cookie.Value)
	if err != nil {
		return nil
	}

	return sm.GetSession(sessionID)
}

// SetSessionCookie sets the session cookie on the response
func (sm *SessionManager) SetSessionCookie(w http.ResponseWriter, sessionID string) {
	encodedCookie := sm.encodeCookie(sessionID)

	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    encodedCookie,
		Path:     "/",
		MaxAge:   int(SessionDuration.Seconds()),
		HttpOnly: CookieHTTPOnly,
		Secure:   CookieSecure,
		SameSite: CookieSameSite,
	})
}

// ClearSessionCookie removes the session cookie
func (sm *SessionManager) ClearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: CookieHTTPOnly,
		Secure:   CookieSecure,
		SameSite: CookieSameSite,
	})
}

// encodeCookie creates a signed cookie value
func (sm *SessionManager) encodeCookie(sessionID string) string {
	// Create signature
	mac := hmac.New(sha256.New, sm.secretKey)
	mac.Write([]byte(sessionID))
	signature := hex.EncodeToString(mac.Sum(nil))

	// Return sessionID.signature
	return base64.URLEncoding.EncodeToString([]byte(sessionID + "." + signature))
}

// decodeCookie verifies and extracts the session ID from cookie
func (sm *SessionManager) decodeCookie(cookieValue string) (string, error) {
	// Decode base64
	decoded, err := base64.URLEncoding.DecodeString(cookieValue)
	if err != nil {
		return "", fmt.Errorf("invalid cookie encoding")
	}

	// Split sessionID.signature
	parts := strings.SplitN(string(decoded), ".", 2)
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid cookie format")
	}

	sessionID := parts[0]
	providedSig := parts[1]

	// Verify signature
	mac := hmac.New(sha256.New, sm.secretKey)
	mac.Write([]byte(sessionID))
	expectedSig := hex.EncodeToString(mac.Sum(nil))

	if !hmac.Equal([]byte(providedSig), []byte(expectedSig)) {
		return "", fmt.Errorf("invalid cookie signature")
	}

	return sessionID, nil
}

// cleanupExpiredSessions removes expired sessions periodically
func (sm *SessionManager) cleanupExpiredSessions() {
	ticker := time.NewTicker(10 * time.Minute)
	for range ticker.C {
		sm.mu.Lock()
		now := time.Now()
		for id, session := range sm.sessions {
			if now.After(session.ExpiresAt) {
				delete(sm.sessions, id)
			}
		}
		sm.mu.Unlock()
	}
}

// generateSessionID creates a cryptographically random session ID
func generateSessionID() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// AdminAuthMiddleware creates middleware that requires admin authentication
func (sm *SessionManager) AdminAuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session := sm.ValidateSession(r)
		if session == nil {
			// Return 401 for API requests
			if strings.HasPrefix(r.URL.Path, "/api/") {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				json.NewEncoder(w).Encode(map[string]interface{}{
					"error":   "unauthorized",
					"message": "Admin authentication required",
				})
				return
			}

			// Redirect to login page for browser requests
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}

		// Session valid, continue
		next.ServeHTTP(w, r)
	})
}

// AuthStatus returns the current authentication status
type AuthStatus struct {
	Authenticated bool   `json:"authenticated"`
	UserID        int64  `json:"user_id,omitempty"`
	Username      string `json:"username,omitempty"`
	ExpiresAt     int64  `json:"expires_at,omitempty"`
}

// HandleAuthStatus returns current auth status
func (sm *SessionManager) HandleAuthStatus(w http.ResponseWriter, r *http.Request) {
	session := sm.ValidateSession(r)

	status := AuthStatus{
		Authenticated: session != nil,
	}

	if session != nil {
		status.UserID = session.UserID
		status.Username = session.Username
		status.ExpiresAt = session.ExpiresAt.Unix()
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

// HandleLogout clears the session and redirects to login
func (sm *SessionManager) HandleLogout(w http.ResponseWriter, r *http.Request) {
	// Get current session and delete it
	cookie, err := r.Cookie(SessionCookieName)
	if err == nil {
		sessionID, err := sm.decodeCookie(cookie.Value)
		if err == nil {
			sm.DeleteSession(sessionID)
		}
	}

	// Clear cookie
	sm.ClearSessionCookie(w)

	// Redirect to login
	http.Redirect(w, r, "/login", http.StatusFound)
}
