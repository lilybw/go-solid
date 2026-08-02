package caching

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"

	"github.com/lilybw/go-solid/internal/meta"
)

// MemCache is a simple in-memory artifact MemCache. Keyed by a hash of
// component-name + props + build-mode. Concurrency-safe.
type MemCache struct {
	mu      sync.RWMutex
	entries map[MemCacheKey]*Rendered
	enabled bool
}

func NewMemCache(enabled bool) *MemCache {
	return &MemCache{entries: make(map[MemCacheKey]*Rendered), enabled: enabled}
}

type MemCacheKey struct {
	Component meta.QualifiedName
	PropsJSON string

	cached_string string // cached String() result
}

func (k MemCacheKey) String() string {
	if k.cached_string != "" {
		return k.cached_string
	}
	h := sha256.New()
	h.Write([]byte(k.Component))
	h.Write([]byte{0})
	h.Write([]byte(k.PropsJSON))
	k.cached_string = hex.EncodeToString(h.Sum(nil))

	return k.cached_string
}

func NewMemCacheKey(component meta.QualifiedName, propsJSON string) *MemCacheKey {
	return &MemCacheKey{Component: component, PropsJSON: propsJSON}
}

// Panics on nil key
func (c *MemCache) Get(key *MemCacheKey) (*Rendered, bool) {
	if !c.enabled {
		return nil, false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	r, ok := c.entries[*key]
	return r, ok
}

// Panics on nil key
func (c *MemCache) Put(key *MemCacheKey, r *Rendered) {
	if !c.enabled {
		return
	}
	c.mu.Lock()
	c.entries[*key] = r
	c.mu.Unlock()
}

func (c *MemCache) Clear() {
	c.mu.Lock()
	c.entries = make(map[MemCacheKey]*Rendered)
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
