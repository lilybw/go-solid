package caching

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"sync"

	"github.com/lilybw/go-solid/internal/meta"
)

// MemCache is a simple in-memory artifact MemCache. Keyed by a hash of
// component-name + props + build-mode. Concurrency-safe.
type MemCache struct {
	mu      sync.RWMutex
	entries map[CacheKey]*Rendered
	byName  map[meta.QualifiedName]map[CacheKey]struct{}
	enabled bool
}

func NewMemCache(enabled bool) *MemCache {
	return &MemCache{entries: make(map[CacheKey]*Rendered), byName: make(map[meta.QualifiedName]map[CacheKey]struct{}), enabled: enabled}
}

type CacheKey struct {
	Component meta.QualifiedName
	Root      HTMLElementID
	Build     meta.Fingerprint
}

func (k *CacheKey) String() string {
	h := sha256.New()
	for _, part := range []string{k.Component, k.Root, k.Build} {
		h.Write([]byte(strconv.Itoa(len(part))))
		h.Write([]byte{':'})
		h.Write([]byte(part))
	}
	return hex.EncodeToString(h.Sum(nil))
}

func NewMemCacheKey(component meta.QualifiedName, root HTMLElementID) *CacheKey {
	return &CacheKey{Component: component, Root: root}
}

func NewBuildCacheKey(component meta.QualifiedName, root HTMLElementID, build meta.Fingerprint) *CacheKey {
	return &CacheKey{Component: component, Root: root, Build: build}
}

// Panics on nil key
func (c *MemCache) Get(key *CacheKey) (*Rendered, bool) {
	if !c.enabled {
		return nil, false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	r, ok := c.entries[*key]
	return r, ok
}

// Panics on nil key
func (c *MemCache) Put(key *CacheKey, r *Rendered) {
	if !c.enabled {
		return
	}
	c.mu.Lock()
	c.entries[*key] = r
	set := c.byName[key.Component]
	if set == nil {
		set = make(map[CacheKey]struct{})
		c.byName[key.Component] = set
	}
	set[*key] = struct{}{}
	c.mu.Unlock()
}

func (c *MemCache) InvalidateComponent(component meta.QualifiedName) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for key := range c.byName[component] {
		delete(c.entries, key)
	}
	delete(c.byName, component)
}

func (c *MemCache) ComponentsInFile(file meta.QualifiedName) []meta.QualifiedName {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var out []meta.QualifiedName
	for name := range c.byName {
		if componentIsInFile(name, file) {
			out = append(out, name)
		}
	}
	return out
}

func (c *MemCache) Clear() {
	c.mu.Lock()
	c.entries = make(map[CacheKey]*Rendered)
	c.byName = make(map[meta.QualifiedName]map[CacheKey]struct{})
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
