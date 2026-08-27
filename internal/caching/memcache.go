package caching

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"sync"

	"github.com/lilybw/go-solid/internal/collections"
	"github.com/lilybw/go-solid/internal/meta"
)

// MemCache is a simple in-memory artifact MemCache. Keyed by a hash of
// component-name + props + build-mode. Concurrency-safe.
type MemCache struct {
	mu      sync.RWMutex
	entries map[CacheKey]*Rendered
	byName  collections.SetMap[meta.QualifiedName, CacheKey]
	enabled bool
}

func NewMemCache(enabled bool) *MemCache {
	return &MemCache{
		entries: make(map[CacheKey]*Rendered),
		byName:  collections.SetMap[meta.QualifiedName, CacheKey]{},
		enabled: enabled,
	}
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
	c.byName.Add(key.Component, *key)
	c.mu.Unlock()
}

func (c *MemCache) InvalidateComponent(component meta.QualifiedName) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for key := range c.byName.Members(component) {
		delete(c.entries, key)
	}
	c.byName.Drop(component)
}

func (c *MemCache) ComponentsInFile(file meta.QualifiedName) []meta.QualifiedName {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.byName.KeysWhere(func(name meta.QualifiedName) bool {
		return componentIsInFile(name, file)
	})
}

func (c *MemCache) Clear() {
	c.mu.Lock()
	c.entries = make(map[CacheKey]*Rendered)
	c.byName = collections.SetMap[meta.QualifiedName, CacheKey]{}
	c.mu.Unlock()
}
