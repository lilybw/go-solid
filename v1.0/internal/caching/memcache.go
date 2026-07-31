package go_solid

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"

	"github.com/lilybw/go_solid/internal/meta"
)

// MemCache is a simple in-memory artifact MemCache. Keyed by a hash of
// component-name + props + build-mode. Concurrency-safe.
type MemCache struct {
	mu      sync.RWMutex
	entries map[string]*Rendered
	enabled bool
}

func NewMemCache(enabled bool) *MemCache {
	return &MemCache{entries: make(map[string]*Rendered), enabled: enabled}
}

func MemCacheKey(component meta.QualifiedName, propsJSON string, minify bool) string {
	h := sha256.New()
	h.Write([]byte(component))
	h.Write([]byte{0})
	h.Write([]byte(propsJSON))
	h.Write([]byte{0})
	if minify {
		h.Write([]byte("min"))
	}
	return hex.EncodeToString(h.Sum(nil))
}

func (c *MemCache) Get(key string) (*Rendered, bool) {
	if !c.enabled {
		return nil, false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	r, ok := c.entries[key]
	return r, ok
}

func (c *MemCache) Put(key string, r *Rendered) {
	if !c.enabled {
		return
	}
	c.mu.Lock()
	c.entries[key] = r
	c.mu.Unlock()
}

// ShortHash returns the first n hex chars of a sha256 of s — used for
// predictable-but-unique asset filenames.
func ShortHash(s string, n int) string {
	sum := sha256.Sum256([]byte(s))
	full := hex.EncodeToString(sum[:])
	if n > len(full) {
		n = len(full)
	}
	return full[:n]
}
