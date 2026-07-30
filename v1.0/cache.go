package go_solid

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"
)

// Rendered is the cacheable artifact set for one component+props combination.
type Rendered struct {
	HTML    string // index.html referencing the CSS + JS
	CSS     string
	JS      string
	CSSName string // predictable filename, e.g. "auth_LoginForm.<hash>.css"
	JSName  string
}

// cache is a simple in-memory artifact cache. Keyed by a hash of
// component-name + props + build-mode. Concurrency-safe.
type cache struct {
	mu      sync.RWMutex
	entries map[string]*Rendered
	enabled bool
}

func newCache(enabled bool) *cache {
	return &cache{entries: make(map[string]*Rendered), enabled: enabled}
}

func cacheKey(componentName, propsJSON string, minify bool) string {
	h := sha256.New()
	h.Write([]byte(componentName))
	h.Write([]byte{0})
	h.Write([]byte(propsJSON))
	h.Write([]byte{0})
	if minify {
		h.Write([]byte("min"))
	}
	return hex.EncodeToString(h.Sum(nil))
}

func (c *cache) get(key string) (*Rendered, bool) {
	if !c.enabled {
		return nil, false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	r, ok := c.entries[key]
	return r, ok
}

func (c *cache) put(key string, r *Rendered) {
	if !c.enabled {
		return
	}
	c.mu.Lock()
	c.entries[key] = r
	c.mu.Unlock()
}

// shortHash returns the first n hex chars of a sha256 of s — used for
// predictable-but-unique asset filenames.
func shortHash(s string, n int) string {
	sum := sha256.Sum256([]byte(s))
	full := hex.EncodeToString(sum[:])
	if n > len(full) {
		n = len(full)
	}
	return full[:n]
}
