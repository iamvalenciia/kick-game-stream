package kick

import (
	"log"
	"sync"
	"sync/atomic"
	"time"
)

// ProfileURLCache provides non-blocking access to user profile picture URLs.
// It uses lazy loading to fetch URLs in the background without blocking the webhook handler.
type ProfileURLCache struct {
	cache   sync.Map // map[int64]cachedURL
	pending sync.Map // map[int64]bool - prevents duplicate fetches
	fetcher ProfileFetcher

	// Concurrency control
	sem      chan struct{}
	maxAge   time.Duration

	// Metrics
	hits    atomic.Uint64
	misses  atomic.Uint64
	fetches atomic.Uint64
	errors  atomic.Uint64
}

// ProfileFetcher is the interface for fetching profile URLs
type ProfileFetcher interface {
	GetUserProfilePicture(userID int64) (string, error)
}

// cachedURL holds a URL with expiry
type cachedURL struct {
	URL       string
	FetchedAt time.Time
}

// ProfileCacheConfig holds configuration options
type ProfileCacheConfig struct {
	MaxConcurrentFetches int           // Max parallel API calls (default: 5)
	MaxAge               time.Duration // How long to cache URLs (default: 1 hour)
}

// DefaultProfileCacheConfig returns sensible defaults
func DefaultProfileCacheConfig() ProfileCacheConfig {
	return ProfileCacheConfig{
		MaxConcurrentFetches: 5,
		MaxAge:               1 * time.Hour,
	}
}

// NewProfileURLCache creates a new profile URL cache
func NewProfileURLCache(fetcher ProfileFetcher, config ProfileCacheConfig) *ProfileURLCache {
	if config.MaxConcurrentFetches <= 0 {
		config.MaxConcurrentFetches = 5
	}
	if config.MaxAge <= 0 {
		config.MaxAge = 1 * time.Hour
	}

	return &ProfileURLCache{
		fetcher: fetcher,
		sem:     make(chan struct{}, config.MaxConcurrentFetches),
		maxAge:  config.MaxAge,
	}
}

// Get returns the cached profile URL for a user, or empty string if not cached.
// This is a non-blocking operation.
func (c *ProfileURLCache) Get(userID int64) string {
	if userID == 0 {
		return ""
	}

	if cached, ok := c.cache.Load(userID); ok {
		entry := cached.(cachedURL)
		if time.Since(entry.FetchedAt) < c.maxAge {
			c.hits.Add(1)
			return entry.URL
		}
		// Expired - delete and trigger refresh
		c.cache.Delete(userID)
	}

	c.misses.Add(1)
	return ""
}

// GetOrFetchAsync returns the cached URL or triggers an async fetch.
// Never blocks - returns empty string immediately if not cached.
// The URL will be available on subsequent calls after the fetch completes.
func (c *ProfileURLCache) GetOrFetchAsync(userID int64) string {
	if userID == 0 {
		return ""
	}

	// Check cache first
	if url := c.Get(userID); url != "" {
		return url
	}

	// Trigger async fetch if not already pending
	if _, alreadyPending := c.pending.LoadOrStore(userID, true); !alreadyPending {
		go c.fetchAsync(userID)
	}

	return "" // Return empty, will be available later
}

// Set manually sets a profile URL (useful when URL comes from webhook)
func (c *ProfileURLCache) Set(userID int64, url string) {
	if userID == 0 || url == "" {
		return
	}

	c.cache.Store(userID, cachedURL{
		URL:       url,
		FetchedAt: time.Now(),
	})
}

// fetchAsync downloads the profile URL in the background
func (c *ProfileURLCache) fetchAsync(userID int64) {
	// Acquire semaphore (limits concurrent fetches)
	c.sem <- struct{}{}
	defer func() { <-c.sem }()

	// Clear pending flag when done
	defer c.pending.Delete(userID)

	c.fetches.Add(1)

	url, err := c.fetcher.GetUserProfilePicture(userID)
	if err != nil {
		c.errors.Add(1)
		// Only log occasionally to avoid spam
		if c.errors.Load()%10 == 1 {
			log.Printf("⚠️ Profile fetch failed for user %d: %v", userID, err)
		}
		return
	}

	if url != "" {
		c.cache.Store(userID, cachedURL{
			URL:       url,
			FetchedAt: time.Now(),
		})
	}
}

// Prefetch triggers background fetches for multiple users
func (c *ProfileURLCache) Prefetch(userIDs []int64) {
	for _, id := range userIDs {
		c.GetOrFetchAsync(id)
	}
}

// Stats returns cache statistics
func (c *ProfileURLCache) Stats() ProfileCacheStats {
	total := c.hits.Load() + c.misses.Load()
	hitRate := float64(0)
	if total > 0 {
		hitRate = float64(c.hits.Load()) / float64(total) * 100
	}

	// Count cached entries
	var size int
	c.cache.Range(func(_, _ interface{}) bool {
		size++
		return true
	})

	return ProfileCacheStats{
		Hits:      c.hits.Load(),
		Misses:    c.misses.Load(),
		HitRate:   hitRate,
		Fetches:   c.fetches.Load(),
		Errors:    c.errors.Load(),
		CacheSize: size,
	}
}

// ProfileCacheStats holds cache metrics
type ProfileCacheStats struct {
	Hits      uint64  `json:"hits"`
	Misses    uint64  `json:"misses"`
	HitRate   float64 `json:"hit_rate_pct"`
	Fetches   uint64  `json:"fetches"`
	Errors    uint64  `json:"errors"`
	CacheSize int     `json:"cache_size"`
}
