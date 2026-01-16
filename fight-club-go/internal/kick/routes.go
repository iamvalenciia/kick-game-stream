package kick

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
)

// OnAuthSuccessFunc is called when OAuth authentication succeeds
// Returns a session token that will be set as a cookie
type OnAuthSuccessFunc func(userID int64, username string, broadcasterID int64) (sessionToken string, err error)

// RouteOptions configures optional behaviors for Kick routes
type RouteOptions struct {
	// OnAuthSuccess is called after successful OAuth to create admin sessions
	OnAuthSuccess OnAuthSuccessFunc
	// SetSessionCookie is called to set the session cookie on the response
	SetSessionCookie func(w http.ResponseWriter, sessionID string)
}

// SetupRoutes adds Kick OAuth and webhook routes to a mux
// localPort is used for OAuth callback (localhost), baseURL is for webhooks (tunnel)
func (s *Service) SetupRoutes(mux *http.ServeMux, baseURL string, localPort int) {
	s.SetupRoutesWithOptions(mux, baseURL, localPort, nil)
}

// SetupRoutesWithOptions adds Kick OAuth and webhook routes with optional callbacks
func (s *Service) SetupRoutesWithOptions(mux *http.ServeMux, baseURL string, localPort int, opts *RouteOptions) {
	// OAuth redirect URI - use baseURL to support tunneling (ngrok)
	// If baseURL is localhost, it works locally. If it's a tunnel, it works remotely.
	callbackURL := fmt.Sprintf("%s/api/kick/callback", baseURL)

	// OAuth login initiation
	mux.HandleFunc("/auth", func(w http.ResponseWriter, r *http.Request) {
		authURL := s.GetAuthURL(callbackURL)

		// Redirect to Kick OAuth
		http.Redirect(w, r, authURL, http.StatusFound)
	})

	// OAuth callback
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		log.Printf("🔔 OAuth callback received: method=%s path=%s query=%s", r.Method, r.URL.Path, r.URL.RawQuery)
		code := r.URL.Query().Get("code")
		state := r.URL.Query().Get("state")

		if code == "" {
			http.Error(w, "Missing authorization code", http.StatusBadRequest)
			return
		}

		// Use the same callback URI for token exchange
		if err := s.ExchangeCode(code, callbackURL, state); err != nil {
			log.Printf("❌ OAuth callback failed: %v", err)
			http.Error(w, "Authentication failed: "+err.Error(), http.StatusInternalServerError)
			return
		}

		// Create admin session if callback is provided
		sessionCreated := false
		if opts != nil && opts.OnAuthSuccess != nil && opts.SetSessionCookie != nil {
			authInfo := s.GetAuthInfo()
			sessionID, err := opts.OnAuthSuccess(authInfo.UserID, authInfo.Username, authInfo.BroadcasterID)
			if err != nil {
				log.Printf("⚠️ Failed to create admin session: %v", err)
			} else {
				opts.SetSessionCookie(w, sessionID)
				sessionCreated = true
				log.Printf("🔐 Admin session created for user %d", authInfo.UserID)
			}
		}

		// Subscribe to chat events in background - don't block the response
		go func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("❌ PANIC in SubscribeToChatEvents: %v", r)
				}
			}()

			// Initialize chatroom ID first
			log.Println("🔄 Fetching chatroom ID...")
			if err := s.InitializeChatroomID(); err != nil {
				log.Printf("⚠️ Failed to initialize chatroom ID: %v", err)
			}

			log.Println("🔄 Starting chat events subscription...")
			if err := s.SubscribeToChatEvents(); err != nil {
				log.Printf("⚠️ Failed to subscribe to chat events: %v", err)
			} else {
				log.Println("✅ Subscribed to Kick chat events")
			}
		}()

		log.Println("✅ OAuth callback successful, sending success page")

		// Determine redirect based on session creation
		redirectTarget := "/admin"
		if !sessionCreated {
			redirectTarget = "/login?error=unauthorized"
		}

		// Return success page that notifies opener and closes popup
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `
			<!DOCTYPE html>
			<html>
			<head><title>Kick Connected</title></head>
			<body style="font-family: sans-serif; text-align: center; padding: 50px; background: linear-gradient(135deg, #1a1a2e 0%, #16213e 100%); color: white;">
				<h1 style="color: #4ecdc4;">✅ Kick Connected!</h1>
				<p>You are now authenticated with Kick.</p>
				<p>Chat commands are now enabled.</p>
				<p style="color: #888;">This window will close automatically...</p>
				<script>
					// Notify opener window of successful auth
					if (window.opener) {
						try {
							window.opener.postMessage({type: 'kick-auth-success'}, '*');
							console.log('Notified opener of auth success');
						} catch(e) {
							console.error('Failed to notify opener:', e);
						}
						setTimeout(() => window.close(), 2000);
					} else {
						// No opener, redirect to admin
						setTimeout(() => window.location.href = '%s', 3000);
					}
				</script>
			</body>
			</html>
		`, redirectTarget)
	})

	// Webhook endpoint for Kick events (this needs the tunnel URL)
	mux.HandleFunc("/webhook", s.HandleWebhook)

	// Status endpoint
	mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"connected":     s.IsConnected(),
			"broadcasterID": s.broadcasterID,
		})
	})

	// Test endpoint to send a USER message to chat
	mux.HandleFunc("/test-message", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req struct {
			Message string `json:"message"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}

		if req.Message == "" {
			req.Message = "🎮 Test message from Fight Club!"
		}

		log.Printf("🧪 Test USER message requested: %s", req.Message)

		if err := s.SendMessage(req.Message); err != nil {
			log.Printf("❌ Test message failed: %v", err)
			http.Error(w, fmt.Sprintf("Failed to send: %v", err), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"message": req.Message,
			"type":    "user",
		})
	})

	// Test endpoint to send a BOT message to chat (for kill feed)
	mux.HandleFunc("/test-bot-message", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req struct {
			Message string `json:"message"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}

		if req.Message == "" {
			req.Message = "🗡️ TestKiller eliminated TestVictim"
		}

		log.Printf("🧪 Test BOT message requested: %s", req.Message)

		if err := s.SendBotMessage(req.Message); err != nil {
			log.Printf("❌ Test bot message failed: %v", err)
			http.Error(w, fmt.Sprintf("Failed to send: %v", err), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"message": req.Message,
			"type":    "bot",
		})
	})

	// Update category endpoint
	mux.HandleFunc("/update-category", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req struct {
			Category string `json:"category"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}

		if req.Category == "" {
			req.Category = "Just Chatting"
		}

		log.Printf("🔄 Category update requested: %s", req.Category)

		if err := s.SetCategory(req.Category); err != nil {
			log.Printf("❌ Category update failed: %v", err)
			http.Error(w, fmt.Sprintf("Failed to update category: %v", err), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success":  true,
			"category": req.Category,
		})
	})

	log.Printf("📡 Kick routes configured:")
	log.Printf("   - OAuth: http://localhost:%d/api/kick/auth", localPort)
	log.Printf("   - Callback: %s (Auto: %s)", callbackURL, baseURL)
	log.Printf("   - Webhook: %s/api/kick/webhook (tunnel required)", baseURL)
	log.Printf("   - Test User Message: POST %s/api/kick/test-message", baseURL)
	log.Printf("   - Test Bot Message: POST %s/api/kick/test-bot-message", baseURL)
	log.Printf("   - Update Category: POST %s/api/kick/update-category", baseURL)
}
