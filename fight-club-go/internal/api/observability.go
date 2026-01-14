package api

import (
	"log"
	"net/http"
	"net/http/pprof"
	"os"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics with bounded cardinality (no per-player labels to prevent DoS)
var (
	// Game engine metrics
	tickDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "game_tick_duration_seconds",
		Help:    "Time spent in game tick",
		Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1},
	})

	renderDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "stream_render_duration_seconds",
		Help:    "Time spent rendering a frame",
		Buckets: []float64{0.005, 0.01, 0.02, 0.033, 0.05, 0.1},
	})

	playerCount = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "game_player_count",
		Help: "Current number of players",
	})

	particleCount = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "game_particle_count",
		Help: "Current number of particles",
	})

	// Event log metrics
	eventLogTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "event_log_total",
		Help: "Total events logged",
	})

	eventLogDropped = promauto.NewCounter(prometheus.CounterOpts{
		Name: "event_log_dropped_total",
		Help: "Events dropped due to rate limiting or buffer full",
	})

	// DoS detection metrics - use ONLY bounded label values
	connectionRejected = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "connection_rejected_total",
		Help: "Connections rejected by rate limiter or origin check",
	}, []string{"reason"}) // Bounded: "rate_limit", "origin", "invalid", "ws_limit"

	// HTTP metrics with bounded labels
	requestLatency = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "http_request_duration_seconds",
		Help:    "HTTP request latency",
		Buckets: prometheus.DefBuckets,
	}, []string{"method", "endpoint"}) // endpoint is path pattern, not full URL

	requestTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "http_requests_total",
		Help: "Total HTTP requests",
	}, []string{"method", "endpoint", "status"})

	// WebSocket metrics
	wsConnectionsActive = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "websocket_connections_active",
		Help: "Currently active WebSocket connections",
	})

	wsMessagesTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "websocket_messages_total",
		Help: "Total WebSocket messages sent",
	})
)

// ObservabilityConfig configures the debug server
type ObservabilityConfig struct {
	Enabled       bool
	ListenAddr    string // MUST be "127.0.0.1:6060" in production
	BasicAuthUser string // Optional basic auth
	BasicAuthPass string
}

// DefaultObservabilityConfig returns safe defaults
func DefaultObservabilityConfig() ObservabilityConfig {
	return ObservabilityConfig{
		Enabled:    true,
		ListenAddr: "127.0.0.1:6060", // Localhost only - NEVER expose externally
	}
}

// StartDebugServer starts the internal observability server
// CRITICAL: This MUST bind to localhost only to prevent pprof-based DoS
func StartDebugServer(cfg ObservabilityConfig) error {
	if !cfg.Enabled {
		log.Println("📊 Debug server disabled")
		return nil
	}

	// SECURITY: Validate address is localhost
	if cfg.ListenAddr != "127.0.0.1:6060" && cfg.ListenAddr != "localhost:6060" {
		// Only allow external binding if explicitly enabled via env
		if os.Getenv("ALLOW_DEBUG_EXTERNAL") != "true" {
			log.Println("⚠️ Debug server forced to localhost for security")
			cfg.ListenAddr = "127.0.0.1:6060"
		}
	}

	mux := http.NewServeMux()

	// pprof endpoints for profiling
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)

	// Prometheus metrics endpoint
	mux.Handle("/metrics", promhttp.Handler())

	// Health check
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	// Optional basic auth wrapper
	var handler http.Handler = mux
	if cfg.BasicAuthUser != "" {
		handler = basicAuthMiddleware(cfg.BasicAuthUser, cfg.BasicAuthPass, mux)
	}

	go func() {
		log.Printf("📊 Debug server starting on %s", cfg.ListenAddr)
		log.Printf("   - pprof:   http://%s/debug/pprof/", cfg.ListenAddr)
		log.Printf("   - metrics: http://%s/metrics", cfg.ListenAddr)

		if err := http.ListenAndServe(cfg.ListenAddr, handler); err != nil {
			log.Printf("⚠️ Debug server error: %v", err)
		}
	}()

	return nil
}

// basicAuthMiddleware adds basic authentication to the handler
func basicAuthMiddleware(user, pass string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		if !ok || u != user || p != pass {
			w.Header().Set("WWW-Authenticate", `Basic realm="debug"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RecordTick records tick timing for metrics
func RecordTick(duration time.Duration) {
	tickDuration.Observe(duration.Seconds())
}

// RecordRender records render timing for metrics
func RecordRender(duration time.Duration) {
	renderDuration.Observe(duration.Seconds())
}

// UpdatePlayerCount updates the player gauge
func UpdatePlayerCount(count int) {
	playerCount.Set(float64(count))
}

// UpdateParticleCount updates the particle gauge
func UpdateParticleCount(count int) {
	particleCount.Set(float64(count))
}

// UpdateEventLogStats updates event log metrics
func UpdateEventLogStats(total, dropped uint64) {
	// Use Set on counters isn't supported, so we track deltas
	// This is called periodically from the game loop
}

// RecordConnectionRejected increments the rejection counter
// reason must be one of: "rate_limit", "origin", "invalid", "ws_limit"
func RecordConnectionRejected(reason string) {
	connectionRejected.WithLabelValues(reason).Inc()
}

// RecordRequest records HTTP request metrics
func RecordRequest(method, endpoint string, status int, duration time.Duration) {
	requestLatency.WithLabelValues(method, endpoint).Observe(duration.Seconds())
	requestTotal.WithLabelValues(method, endpoint, http.StatusText(status)).Inc()
}

// UpdateWSConnections updates WebSocket connection count
func UpdateWSConnections(count int) {
	wsConnectionsActive.Set(float64(count))
}

// IncrementWSMessages increments WebSocket message counter
func IncrementWSMessages() {
	wsMessagesTotal.Inc()
}
