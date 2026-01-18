package main

import (
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"fight-club/internal/api"
	"fight-club/internal/avatar"
	"fight-club/internal/chat"
	"fight-club/internal/config"
	"fight-club/internal/game"
	"fight-club/internal/kick"
	"fight-club/internal/streaming"

	"github.com/joho/godotenv"
)

func main() {
	// Load .env file from parent directory
	if err := godotenv.Load("../.env"); err != nil {
		// Try current directory as fallback
		if err := godotenv.Load(".env"); err != nil {
			log.Println("💡 No .env file found, using environment variables only")
		}
	} else {
		log.Println("✅ Loaded environment from ../.env")
	}

	log.Println("🎮 ================================")
	log.Println("🎮  FIGHT CLUB - GO ENGINE")
	log.Println("🎮  Kick OAuth + Webhooks")
	log.Println("🎮 ================================")

	// Load centralized configuration (SSOT - Single Source of Truth)
	appConfig := config.Load()
	videoCfg := appConfig.Video
	audioCfg := appConfig.Audio
	serverCfg := appConfig.Server

	// Load environment variables for external services
	streamKey := os.Getenv("STREAM_KEY_KICK")
	clientID := os.Getenv("CLIENT_ID_KICK")
	clientSecret := os.Getenv("CLIENT_SECRET_KICK")
	broadcasterID := os.Getenv("KICK_BROADCASTER_USER_ID")
	publicURL := os.Getenv("PUBLIC_URL") // For webhook callbacks (e.g., ngrok URL)

	// Port from config (allows env override via serverCfg)
	port := strconv.Itoa(serverCfg.Port)

	// Kick RTMP URL
	rtmpURL := "rtmps://fa723fc1b171.global-contribute.live-video.net:443/app"

	// Log configuration
	log.Printf("📡 RTMP URL: %s", rtmpURL)
	if streamKey != "" {
		keyPreview := streamKey[:min(15, len(streamKey))] + "..."
		log.Printf("🔑 Stream Key: %s", keyPreview)
	} else {
		log.Println("⚠️ WARNING: STREAM_KEY_KICK not set!")
	}
	if clientID != "" {
		log.Printf("📺 Client ID: %s...", clientID[:min(10, len(clientID))])
	}
	if broadcasterID != "" {
		log.Printf("👤 Broadcaster ID: %s", broadcasterID)
	}
	if publicURL != "" {
		log.Printf("🌐 Public URL: %s", publicURL)
	}
	log.Printf("🎮 Config: %d TPS, %d FPS, %dk bitrate, %dx%d", videoCfg.FPS, videoCfg.FPS, videoCfg.Bitrate, videoCfg.Width, videoCfg.Height)

	// Create game engine with centralized config
	engine := game.NewEngine(game.EngineConfig{
		TickRate:    videoCfg.FPS, // Use FPS as tick rate for consistency
		WorldWidth:  videoCfg.Width,
		WorldHeight: videoCfg.Height,
		Limits:      appConfig.Limits,
	})
	limits := engine.GetLimits()
	log.Printf("🛡️ Resource limits: %d players, %d particles, %d effects, %d texts",
		limits.MaxPlayers, limits.MaxParticles, limits.MaxEffects, limits.MaxTexts)

	// Start event log
	eventLogPath := getEnvWithDefault("EVENT_LOG_PATH", "events.jsonl")
	if err := engine.StartEventLog(eventLogPath); err != nil {
		log.Printf("⚠️ Event log disabled: %v", err)
	} else {
		log.Printf("📝 Event log: %s", eventLogPath)
	}

	// Start debug server
	debugCfg := api.DefaultObservabilityConfig()
	if os.Getenv("DISABLE_DEBUG_SERVER") != "true" {
		if err := api.StartDebugServer(debugCfg); err != nil {
			log.Printf("⚠️ Debug server disabled: %v", err)
		}
	}

	// Music configuration from centralized config
	musicPath := getEnvWithDefault("MUSIC_PATH", "assets/music/digital_fight_arena.ogg")

	// Hardware encoding configuration
	// USE_NVENC=true enables NVIDIA GPU hardware encoding (requires NVIDIA GPU with NVENC support)
	// FORCE_NVENC=true skips the availability check and forces NVENC (use if test fails but you have NVENC)
	// This significantly reduces CPU usage and improves streaming performance
	useNVENC := os.Getenv("USE_NVENC") == "true"
	forceNVENC := os.Getenv("FORCE_NVENC") == "true"

	// Create stream manager with centralized video config
	streamer := streaming.NewStreamManager(engine, streaming.StreamConfig{
		Width:        videoCfg.Width,
		Height:       videoCfg.Height,
		FPS:          videoCfg.FPS,
		Bitrate:      videoCfg.Bitrate,
		RTMPURL:      rtmpURL,
		StreamKey:    streamKey,
		MusicEnabled: audioCfg.Enabled,
		MusicVolume:  audioCfg.Volume,
		MusicPath:    musicPath,
		UseNVENC:     useNVENC,
		ForceNVENC:   forceNVENC,
	})

	// Initialize avatar cache
	avatarCache := avatar.NewCache(200)
	_ = avatarCache

	// Initialize Kick service for OAuth webhooks
	var kickService *kick.Service
	var kickBot *kick.Bot
	chatHandler := chat.NewHandler(engine)

	if clientID != "" && clientSecret != "" {
		kickService = kick.NewService(clientID, clientSecret)
		// ... (rest of init)

		// Set broadcaster ID if avail
		if broadcasterID != "" {
			bid, _ := strconv.ParseInt(broadcasterID, 10, 64)
			kickService.SetBroadcasterID(bid)
		}

		if publicURL != "" {
			kickService.SetWebhookURL(publicURL + "/api/kick/webhook")
		}

		// Register chat message handler
		kickService.OnChatMessage(func(msg kick.ChatMessage) {
			if msg.IsCommand {
				profilePic := msg.ProfilePic

				// If profile picture is not in webhook, fetch from API
				if profilePic == "" && msg.UserID != 0 {
					if pic, err := kickService.GetUserProfilePicture(msg.UserID); err == nil && pic != "" {
						profilePic = pic
						log.Printf("📷 Fetched profile picture for %s: %s", msg.Username, pic[:min(50, len(pic))])
					}
				}

				cmd := chat.ChatCommand{
					Command:    msg.Command,
					Args:       msg.Args,
					Username:   msg.Username,
					UserID:     msg.UserID,
					ProfilePic: profilePic,
				}
				chatHandler.ProcessCommand(cmd)
			}
		})

		// Wire up OnKill event to sending chat messages
		// Initialize Kick Bot for Kill Feed
		kickBot = kick.NewBot(kickService)
		kickBot.Start()

		engine.OnKill = func(killer, victim *game.Player) {
			kickBot.QueueKill(killer.Name, victim.Name, killer.Weapon, killer.Kills)
		}

		log.Println("✅ Kick OAuth service initialized")

		// Try to auto-subscribe if already authenticated
		if kickService.IsConnected() {
			log.Println("📡 Already authenticated, subscribing to chat events...")
			go func() {
				// Initialize chatroom ID
				log.Println("🔄 Fetching chatroom ID...")
				if err := kickService.InitializeChatroomID(); err != nil {
					log.Printf("⚠️ Failed to initialize chatroom ID: %v", err)
				}

				if err := kickService.SubscribeToChatEvents(); err != nil {
					log.Printf("⚠️ Auto-subscribe failed: %v", err)
				}

				// Auto-set category to Just Chatting
				log.Println("🔄 Updating category to 'Just Chatting'...")
				if err := kickService.SetCategory("Just Chatting"); err != nil {
					log.Printf("⚠️ Failed to update category: %v", err)
				}
			}()
		}
	} else {
		log.Println("⚠️ CLIENT_ID_KICK or CLIENT_SECRET_KICK not set - OAuth disabled")
	}

	// Register stream start callback to set category to IRL
	if kickService != nil {
		// Capture kickService in closure
		ks := kickService
		streamer.OnStreamStart(func() {
			// Try multiple category names in order of preference
			categories := []string{"Just Chatting", "IRL", "just chatting"}

			for _, cat := range categories {
				log.Printf("🔄 Stream started, trying to set category to '%s'...", cat)
				if err := ks.SetCategory(cat); err != nil {
					log.Printf("⚠️ Failed to update category to %s: %v", cat, err)
					continue // Try next category
				}
				log.Printf("✅ Category automatically set to '%s'", cat)
				return // Success, stop trying
			}
			log.Println("❌ Failed to set category to any IRL variant")
		})
	}

	// Setup Kick routes on separate mux BEFORE creating API server
	var kickMux http.Handler
	baseURL := "http://localhost:" + port
	if publicURL != "" {
		baseURL = publicURL
	}
	portInt := 3000 // default
	if p, err := strconv.Atoi(port); err == nil {
		portInt = p
	}

	// Admin authentication setup
	adminAuthEnabled := os.Getenv("ADMIN_AUTH_ENABLED") == "true"
	var sessionManager *api.SessionManager

	// Parse broadcaster ID for session manager
	var broadcasterIDInt int64
	if broadcasterID != "" {
		broadcasterIDInt, _ = strconv.ParseInt(broadcasterID, 10, 64)
	}

	if adminAuthEnabled {
		sessionManager = api.NewSessionManager(broadcasterIDInt)
		log.Printf("🔐 Admin authentication ENABLED (broadcaster ID: %d)", broadcasterIDInt)
	} else {
		log.Println("⚠️ Admin authentication DISABLED (set ADMIN_AUTH_ENABLED=true to enable)")
	}

	if kickService != nil {
		mux := http.NewServeMux()

		// Setup routes with session callback if auth is enabled
		if sessionManager != nil {
			kickService.SetupRoutesWithOptions(mux, baseURL, portInt, &kick.RouteOptions{
				OnAuthSuccess: func(userID int64, username string, bcasterID int64) (string, error) {
					return sessionManager.CreateSession(userID, username, bcasterID)
				},
				SetSessionCookie: sessionManager.SetSessionCookie,
			})
		} else {
			kickService.SetupRoutes(mux, baseURL, portInt)
		}

		kickMux = mux
		log.Printf("✅ Kick routes mounted at /api/kick (OAuth: localhost:%d, Webhook: %s/api/kick/webhook)", portInt, baseURL)
	}

	// Create API server with Kick handler and auth
	server := api.NewServerWithKickAndAuth(engine, streamer, kickMux, sessionManager, adminAuthEnabled)

	// Start game engine
	engine.Start()
	log.Println("✅ Game Engine started")

	// Start API server in goroutine
	go func() {
		addr := ":" + port
		log.Printf("🌐 API server on http://localhost%s", addr)
		log.Printf("🎮 Admin Panel: http://localhost%s/admin", addr)

		if kickService != nil {
			log.Printf("🔑 Kick OAuth: %s/api/kick/auth", baseURL)
			log.Printf("📡 Webhook URL: %s/api/kick/webhook", baseURL)
		}

		if err := server.Start(addr); err != nil {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()

	log.Println("")
	log.Println("📋 To enable chat commands:")
	log.Println("   1. Start localtunnel: npx localtunnel --port 3000")
	log.Println("   2. Set PUBLIC_URL in .env to tunnel URL")
	log.Println("   3. Visit /api/kick/auth to login with Kick")
	log.Println("   4. Type !join in Kick chat")
	log.Println("")

	// Wait for shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	log.Println("✅ Server ready! Press Ctrl+C to stop.")
	<-quit

	log.Println("🛑 Shutting down...")
	if kickBot != nil {
		kickBot.Stop()
	}
	streamer.Stop()
	engine.StopEventLog()
	engine.Stop()
	log.Println("👋 Goodbye!")
}

func getEnvWithDefault(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

func getEnvInt(key string, defaultVal int) int {
	if val := os.Getenv(key); val != "" {
		if i, err := strconv.Atoi(val); err == nil {
			return i
		}
	}
	return defaultVal
}

func getEnvFloat(key string, defaultVal float64) float64 {
	if val := os.Getenv(key); val != "" {
		if f, err := strconv.ParseFloat(val, 64); err == nil {
			return f
		}
	}
	return defaultVal
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
