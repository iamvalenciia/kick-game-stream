package avatar

import (
	"image"
	"image/draw"
	_ "image/gif"  // Support GIF format
	_ "image/jpeg" // Support JPEG format
	_ "image/png"  // Support PNG format
	"log"
	"net/http"
	"sync"
	"time"

	_ "golang.org/x/image/webp" // Support WebP format (Kick profile pictures)
)

// Cache stores decoded avatar images with LRU eviction
type Cache struct {
	mu      sync.RWMutex
	images  map[string]*CachedAvatar
	order   []string // LRU order (oldest first)
	maxSize int

	// Pending fetches
	pending map[string]bool
	client  *http.Client
	sem     chan struct{} // Semaphore for concurrent fetches
}

// CachedAvatar holds a decoded image and metadata
type CachedAvatar struct {
	Image     image.Image
	FetchedAt time.Time
}

const (
	DefaultMaxAvatars    = 200
	AvatarTTL            = 30 * time.Minute
	MaxConcurrentFetches = 3
	FetchTimeout         = 5 * time.Second
)

// NewCache creates a new avatar cache
func NewCache(maxSize int) *Cache {
	if maxSize <= 0 {
		maxSize = DefaultMaxAvatars
	}
	return &Cache{
		images:  make(map[string]*CachedAvatar),
		order:   make([]string, 0, maxSize),
		maxSize: maxSize,
		pending: make(map[string]bool),
		client: &http.Client{
			Timeout: FetchTimeout,
		},
		sem: make(chan struct{}, MaxConcurrentFetches),
	}
}

// Get returns a cached avatar or nil
func (c *Cache) Get(url string) image.Image {
	if url == "" {
		return nil
	}

	c.mu.RLock()
	cached, exists := c.images[url]
	c.mu.RUnlock()

	if !exists {
		return nil
	}

	// Check TTL
	if time.Since(cached.FetchedAt) > AvatarTTL {
		c.mu.Lock()
		delete(c.images, url)
		c.mu.Unlock()
		return nil
	}

	return cached.Image
}

// GetOrFetch returns cached avatar or starts async fetch
// Never blocks - returns nil immediately if not cached
func (c *Cache) GetOrFetch(url string) image.Image {
	if url == "" {
		return nil
	}

	// Try cache first
	if img := c.Get(url); img != nil {
		return img
	}

	// Start async fetch if not pending
	c.mu.Lock()
	if !c.pending[url] {
		c.pending[url] = true
		go c.fetchAsync(url)
	}
	c.mu.Unlock()

	return nil
}

// fetchAsync downloads and caches an avatar
func (c *Cache) fetchAsync(url string) {
	// Acquire semaphore
	c.sem <- struct{}{}
	defer func() { <-c.sem }()

	defer func() {
		c.mu.Lock()
		delete(c.pending, url)
		c.mu.Unlock()
	}()

	resp, err := c.client.Get(url)
	if err != nil {
		log.Printf("⚠️ Avatar fetch failed for %s: %v", url[:min(50, len(url))], err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("⚠️ Avatar fetch returned %d for %s", resp.StatusCode, url[:min(50, len(url))])
		return
	}

	img, format, err := image.Decode(resp.Body)
	if err != nil {
		log.Printf("⚠️ Avatar decode failed for %s: %v (Content-Type: %s)",
			url[:min(60, len(url))], err, resp.Header.Get("Content-Type"))
		return
	}
	log.Printf("🖼️ Avatar decoded (format: %s) for %s", format, url[:min(40, len(url))])

	// Make circular
	circleImg := c.makeCircular(img)

	c.mu.Lock()
	defer c.mu.Unlock()

	// Evict if at capacity
	if len(c.images) >= c.maxSize {
		c.evict()
	}

	c.images[url] = &CachedAvatar{
		Image:     circleImg,
		FetchedAt: time.Now(),
	}
	c.order = append(c.order, url)

	log.Printf("✅ Avatar cached for %s", url[:min(40, len(url))])
}

// makeCircular creates a circular crop of the image
func (c *Cache) makeCircular(img image.Image) image.Image {
	bounds := img.Bounds()
	size := bounds.Dx()
	if bounds.Dy() < size {
		size = bounds.Dy()
	}

	// Create circular mask
	circle := image.NewRGBA(image.Rect(0, 0, size, size))
	centerX, centerY := size/2, size/2
	radius := size / 2

	// Draw with circular mask
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			dx := x - centerX
			dy := y - centerY
			if dx*dx+dy*dy <= radius*radius {
				circle.Set(x, y, img.At(bounds.Min.X+x, bounds.Min.Y+y))
			}
		}
	}

	return circle
}

// evict removes the oldest cached avatar
func (c *Cache) evict() {
	if len(c.order) == 0 {
		return
	}

	oldest := c.order[0]
	c.order = c.order[1:]
	delete(c.images, oldest)
}

// Size returns the current cache size
func (c *Cache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.images)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// Unused but keeping for completeness
var _ draw.Image = &image.RGBA{}
