package main

import (
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"fight-club/internal/api"
	"fight-club/internal/chat"
	"fight-club/internal/config"
	"fight-club/internal/game"
	"fight-club/internal/ipc"
	"fight-club/internal/kick"
	"fight-club/internal/streaming"

	"github.com/joho/godotenv"
)

// =============================================================================
// FIGHT CLUB - GAME SERVER
// =============================================================================
// This server handles ONLY the game logic:
// - Game engine (physics, combat, players)
// - Kick OAuth & webhooks
// - API endpoints
// - IPC publishing for external streamer
//
// STREAMING IS HANDLED BY A SEPARATE PROCESS: go run ./cmd/streamer
// This separation ensures smooth streaming without interference from game logic.
// =============================================================================

func main() {
	// Load .env file from parent directory
	if err := godotenv.Load("../.env"); err != nil {
		// Try current directory as fallback
		if err := godotenv.Load(".env"); err != nil {
			log.Println("No .env file found, using environment variables only")
		}
	} else {
		log.Println("Loaded environment from ../.env")
	}

	log.Println("================================")
	log.Println("  FIGHT CLUB - GAME SERVER")
	log.Println("  (Streaming handled separately)")
	log.Println("================================")

	// Load centralized configuration (SSOT - Single Source of Truth)
	appConfig := config.Load()
	videoCfg := appConfig.Video
	serverCfg := appConfig.Server

	// Load environment variables for external services
	clientID := os.Getenv("CLIENT_ID_KICK")
	clientSecret := os.Getenv("CLIENT_SECRET_KICK")
	broadcasterID := os.Getenv("KICK_BROADCASTER_USER_ID")
	publicURL := os.Getenv("PUBLIC_URL") // For webhook callbacks (e.g., ngrok URL)

	// Port from config (allows env override via serverCfg)
	port := strconv.Itoa(serverCfg.Port)

	// Log configuration
	log.Printf("Config: %d TPS, %dx%d world", videoCfg.FPS, videoCfg.Width, videoCfg.Height)
	if clientID != "" {
		log.Printf("Client ID: %s...", clientID[:min(10, len(clientID))])
	}
	if broadcasterID != "" {
		log.Printf("Broadcaster ID: %s", broadcasterID)
	}
	if publicURL != "" {
		log.Printf("Public URL: %s", publicURL)
	}

	// Create game engine with centralized config
	engine := game.NewEngine(game.EngineConfig{
		TickRate:    videoCfg.FPS, // Use FPS as tick rate for consistency
		WorldWidth:  videoCfg.Width,
		WorldHeight: videoCfg.Height,
		Limits:      appConfig.Limits,
	})
	limits := engine.GetLimits()
	log.Printf("Resource limits: %d players, %d particles, %d effects, %d texts",
		limits.MaxPlayers, limits.MaxParticles, limits.MaxEffects, limits.MaxTexts)

	// ==========================================================================
	// IPC PUBLISHER - Always enabled for external streamer
	// ==========================================================================
	ipcSocketPath := getEnvWithDefault("IPC_SOCKET", ipc.DefaultSocketPath)
	log.Println("Starting IPC publisher for external streamer...")

	ipcPublisher := ipc.NewPublisher(ipcSocketPath)
	ipcPublisher.SetConfig(videoCfg.Width, videoCfg.Height, videoCfg.FPS, videoCfg.Bitrate)

	if err := ipcPublisher.Start(); err != nil {
		log.Printf("WARNING: Failed to start IPC publisher: %v", err)
		log.Println("External streamer will not be able to connect!")
	} else {
		// Connect engine snapshot callback to IPC publisher
		engine.OnSnapshot = func(snapshot *game.GameSnapshot) {
			ipcPublisher.PublishSnapshot(snapshot)
		}
		log.Printf("IPC Publisher started on %s", ipcSocketPath)
		log.Println("")
		log.Println(">>> To start streaming, run in another terminal:")
		log.Println(">>> go run ./cmd/streamer")
		log.Println("")
	}

	// ==========================================================================
	// NO-OP STREAMER - API expects a streamer, but we don't stream from server
	// ==========================================================================
	// The API server needs a streamer interface for endpoints like /api/stats
	// We use a NoOpStreamer that returns "streaming handled externally" status
	noopStreamer := streaming.NewNoOpStreamer()

	// Start event log
	eventLogPath := getEnvWithDefault("EVENT_LOG_PATH", "events.jsonl")
	if err := engine.StartEventLog(eventLogPath); err != nil {
		log.Printf("Event log disabled: %v", err)
	} else {
		log.Printf("Event log: %s", eventLogPath)
	}

	// Start debug server
	debugCfg := api.DefaultObservabilityConfig()
	if os.Getenv("DISABLE_DEBUG_SERVER") != "true" {
		if err := api.StartDebugServer(debugCfg); err != nil {
			log.Printf("Debug server disabled: %v", err)
		}
	}

	// Initialize Kick service for OAuth webhooks
	var kickService *kick.Service
	var kickBot *kick.Bot
	var profileCache *kick.ProfileURLCache
	chatHandler := chat.NewHandler(engine)

	// Create command queue with worker pool for non-blocking command processing
	// This decouples webhook handlers from game engine, eliminating latency
	commandQueue := chat.NewCommandQueue(chatHandler, chat.DefaultQueueConfig())
	commandQueue.Start()

	if clientID != "" && clientSecret != "" {
		kickService = kick.NewService(clientID, clientSecret)

		// Create profile URL cache for lazy-loading avatars (non-blocking)
		profileCache = kick.NewProfileURLCache(kickService, kick.DefaultProfileCacheConfig())

		// Enable async webhook handling to prevent backpressure
		kickService.SetAsyncHandler(true)

		// Set broadcaster ID if available
		if broadcasterID != "" {
			bid, _ := strconv.ParseInt(broadcasterID, 10, 64)
			kickService.SetBroadcasterID(bid)
		}

		if publicURL != "" {
			kickService.SetWebhookURL(publicURL + "/api/kick/webhook")
		}

		// Register chat message handler - NOW NON-BLOCKING
		// Commands are enqueued and processed by worker pool
		kickService.OnChatMessage(func(msg kick.ChatMessage) {
			if msg.IsCommand {
				profilePic := msg.ProfilePic

				// If profile picture is not in webhook, use cache (non-blocking)
				if profilePic == "" && msg.UserID != 0 {
					if msg.ProfilePic != "" {
						profileCache.Set(msg.UserID, msg.ProfilePic)
					}
					profilePic = profileCache.GetOrFetchAsync(msg.UserID)
				}

				cmd := chat.ChatCommand{
					Command:    msg.Command,
					Args:       msg.Args,
					Username:   msg.Username,
					UserID:     msg.UserID,
					ProfilePic: profilePic,
				}

				// Non-blocking enqueue - returns immediately
				if !commandQueue.Enqueue(cmd) {
					log.Printf("Command queue full, dropped !%s from %s", cmd.Command, cmd.Username)
				}
			}
		})

		// Wire up OnKill event to sending chat messages
		// Initialize Kick Bot for Kill Feed
		kickBot = kick.NewBot(kickService)
		kickBot.Start()

		engine.OnKill = func(killer, victim *game.Player) {
			kickBot.QueueKill(killer.Name, victim.Name, killer.Weapon, killer.Kills)
		}

		log.Println("Kick OAuth service initialized")

		// Try to auto-subscribe if already authenticated
		if kickService.IsConnected() {
			log.Println("Already authenticated, subscribing to chat events...")
			go func() {
				log.Println("Fetching chatroom ID...")
				if err := kickService.InitializeChatroomID(); err != nil {
					log.Printf("Failed to initialize chatroom ID: %v", err)
				}

				if err := kickService.SubscribeToChatEvents(); err != nil {
					log.Printf("Auto-subscribe failed: %v", err)
				}

				log.Println("Updating category to 'Just Chatting'...")
				if err := kickService.SetCategory("Just Chatting"); err != nil {
					log.Printf("Failed to update category: %v", err)
				}
			}()
		}
	} else {
		log.Println("CLIENT_ID_KICK or CLIENT_SECRET_KICK not set - OAuth disabled")
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
		log.Printf("Admin authentication ENABLED (broadcaster ID: %d)", broadcasterIDInt)
	} else {
		log.Println("Admin authentication DISABLED (set ADMIN_AUTH_ENABLED=true to enable)")
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
		log.Printf("Kick routes mounted at /api/kick (OAuth: localhost:%d, Webhook: %s/api/kick/webhook)", portInt, baseURL)
	}

	// Create API server with NoOp streamer (streaming is external)
	server := api.NewServerWithKickAndAuth(engine, noopStreamer, kickMux, sessionManager, adminAuthEnabled)

	// Start game engine
	engine.Start()
	log.Println("Game Engine started")

	// Start API server in goroutine
	go func() {
		addr := ":" + port
		log.Printf("API server on http://localhost%s", addr)
		log.Printf("Admin Panel: http://localhost%s/admin", addr)

		if kickService != nil {
			log.Printf("Kick OAuth: %s/api/kick/auth", baseURL)
			log.Printf("Webhook URL: %s/api/kick/webhook", baseURL)
		}

		if err := server.Start(addr); err != nil {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()

	log.Println("")
	log.Println("To enable chat commands:")
	log.Println("   1. Set PUBLIC_URL in .env to your ngrok URL")
	log.Println("   2. Visit /api/kick/auth to login with Kick")
	log.Println("   3. Type !join in Kick chat")
	log.Println("")

	// Wait for shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	log.Println("Server ready! Press Ctrl+C to stop.")
	<-quit

	log.Println("Shutting down...")

	// Stop command queue first (drain pending commands)
	commandQueue.Stop()

	if kickBot != nil {
		kickBot.Stop()
	}

	// Stop IPC publisher
	if ipcPublisher != nil {
		ipcPublisher.Stop()
	}

	// Note: No streamer.Stop() - streaming is handled by external process

	engine.StopEventLog()
	engine.Stop()
	log.Println("Goodbye!")
}

func getEnvWithDefault(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
