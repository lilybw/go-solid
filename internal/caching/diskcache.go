package caching

import (
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/lilybw/go-solid/internal/collections"
	"github.com/lilybw/go-solid/internal/esbuild"
	"github.com/lilybw/go-solid/internal/hashing"
	io_int "github.com/lilybw/go-solid/internal/io"
	"github.com/lilybw/go-solid/shared/meta"
)

// -----------------------------------------------------------------------------
// Disk-backed component cache
// -----------------------------------------------------------------------------
// There is no on-disk reverse index: the manifests are the source of truth. The
// in-process reverse graph lives in internal.DependencyIndex.

type HTMLElementID = string

const MANIFEST_EXT = ".meta.json"

type ComponentDiskManifest struct {
	Component   meta.QualifiedName `json:"component"`
	Minify      bool               `json:"minify"`
	GeneratedAt string             `json:"generatedAt"`
	Key         string             `json:"key"`     // cache key (also the base filename stem)
	Sources     map[string]string  `json:"sources"` // absPath -> "sha256:<hex>"
	Artifacts   struct {
		JS  meta.RelativeFilePath `json:"js"`
		CSS meta.RelativeFilePath `json:"css,omitempty"`
	} `json:"artifacts"`
	// ServeNames are the names the consumer serves assets under (the
	// content-hashed names baked into the HTML). Distinct from Artifacts, which
	// are storage filenames. Restoring these keeps a disk-cache hit identical to
	// a fresh render.
	ServeNames struct {
		JS  string `json:"js"`
		CSS string `json:"css,omitempty"`
	} `json:"serveNames"`
}

func (m *ComponentDiskManifest) Validate() error {
	if m.Component == "" {
		return fmt.Errorf("manifest: missing component name")
	}
	if m.Key == "" {
		return fmt.Errorf("manifest: missing key")
	}
	if m.Artifacts.JS == "" {
		return fmt.Errorf("manifest: missing JS artifact")
	}
	return nil
}

type DiskCache struct {
	directory meta.AbsoluteDirectoryPath
	mu        sync.Mutex
	enabled   bool

	indexed bool
	// byKey maps CacheKey.String to the manifest path holding that entry.
	byKey map[meta.CacheKeyString]meta.AbsoluteFilePath
	// byComponent maps a component name to the key strings it has entries for,
	// which is what invalidation needs.
	byComponent collections.SetMap[meta.QualifiedName, meta.CacheKeyString]
}

func NewDiskCache(root meta.AbsoluteDirectoryPath, enabled bool) (*DiskCache, error) {
	dir := filepath.Join(root, CACHE_DIR_NAME)
	dc := &DiskCache{
		directory:   dir,
		enabled:     enabled,
		byKey:       map[meta.CacheKeyString]meta.AbsoluteFilePath{},
		byComponent: collections.SetMap[meta.QualifiedName, meta.CacheKeyString]{},
	}
	if !enabled {
		return dc, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("disk cache: create %q: %w", dir, err)
	}
	return dc, nil
}

func (dc *DiskCache) Directory() meta.AbsoluteDirectoryPath { return dc.directory }

// entryStem is the human-readable base filename for an entry.
func entryStem(key *CacheKey) string {
	short := key.String()
	return fmt.Sprintf("%s__%s", SafeStem(key.Component), short[:min(12, len(short))])
}

// Get returns a cached Rendered if a valid entry exists for key. Validity means
// every recorded source still hashes to its recorded value (invalidation).
func (dc *DiskCache) Get(key *CacheKey) (*Rendered, bool) {
	if !dc.enabled {
		return nil, false
	}
	dc.mu.Lock()
	defer dc.mu.Unlock()

	manifestPath, ok := dc.lockedManifestPathForKey(key)
	if !ok {
		return nil, false
	}
	man, err := ReadManifest(manifestPath)
	if err != nil {
		return nil, false
	}
	// Invalidation: every source must still match its recorded hash.
	if !hashing.Holds(man.Sources) {
		return nil, false // stale (edited or deleted source)
	}

	base := strings.TrimSuffix(manifestPath, MANIFEST_EXT)
	js, err := os.ReadFile(base + ".js")
	if err != nil {
		return nil, false
	}
	r := &Rendered{
		JS:     string(js),
		JSName: man.ServeNames.JS,
	}
	if man.Artifacts.CSS != "" {
		if css, err := os.ReadFile(base + ".css"); err == nil {
			r.CSS = string(css)
			r.CSSName = man.ServeNames.CSS
		}
	}
	return r, true
}

// Put writes an entry (manifest + artifacts) and indexes it.
// sources are the absolute paths from the bundle's metafile.
func (dc *DiskCache) Put(key *CacheKey, minify bool, r *Rendered, sources []string) error {
	if !dc.enabled {
		return nil
	}
	dc.mu.Lock()
	defer dc.mu.Unlock()

	man := ComponentDiskManifest{
		Component:   key.Component,
		Minify:      minify,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Key:         key.String(),
		Sources:     map[meta.AbsoluteFilePath]meta.ContentDigest{},
	}
	for _, src := range sources {
		normalized := esbuild.NormalizeSourcePath(src)
		if h, ok := hashing.OfFile(src); ok {
			man.Sources[normalized] = h
		}
	}
	stem := entryStem(key)
	man.Artifacts.JS = stem + ".js"
	if r.CSS != "" {
		man.Artifacts.CSS = stem + ".css"
	}
	man.ServeNames.JS = r.JSName
	man.ServeNames.CSS = r.CSSName

	base := filepath.Join(dc.directory, stem)
	if err := io_int.WriteAtomic(base+".js", []byte(r.JS)); err != nil {
		return err
	}
	if r.CSS != "" {
		if err := io_int.WriteAtomic(base+".css", []byte(r.CSS)); err != nil {
			return err
		}
	}
	manBytes, err := json.Marshal(man, json.Deterministic(true), jsontext.WithIndent("  "))
	if err != nil {
		return err
	}
	manifestPath := base + MANIFEST_EXT
	if err := io_int.WriteAtomic(manifestPath, manBytes); err != nil {
		return err
	}

	dc.lockedIndex(man.Key, man.Component, manifestPath)
	return nil
}

// InvalidateComponent removes every cached entry for the given component.
// Returns the number of entries removed. Safe to call when disabled (no-op).
func (dc *DiskCache) InvalidateComponent(component meta.QualifiedName) int {
	if !dc.enabled {
		return 0
	}
	dc.mu.Lock()
	defer dc.mu.Unlock()
	dc.lockedEnsureIndex()

	removed := 0
	for keyStr := range dc.byComponent.Members(component) {
		manifestPath, ok := dc.byKey[keyStr]
		if !ok {
			continue
		}
		base := strings.TrimSuffix(manifestPath, MANIFEST_EXT)
		// Remove the whole entry set; ignore individual errors (best-effort,
		// a missing sibling just means it was never written, e.g. no CSS).
		for _, suffix := range []string{".js", ".css", MANIFEST_EXT} {
			_ = os.Remove(base + suffix)
		}
		delete(dc.byKey, keyStr)
		removed++
	}
	dc.byComponent.Drop(component)
	return removed
}

// ComponentsInFile lists the cached components backed by one component file:
// the file's own selector and every "#" selection out of it.
func (dc *DiskCache) ComponentsInFile(file meta.QualifiedName) []meta.QualifiedName {
	if !dc.enabled {
		return nil
	}
	dc.mu.Lock()
	defer dc.mu.Unlock()
	dc.lockedEnsureIndex()

	return dc.byComponent.KeysWhere(func(component meta.QualifiedName) bool {
		return componentIsInFile(component, file)
	})
}

// lockedManifestPathForKey resolves a key to its manifest. Callers hold dc.mu.
func (dc *DiskCache) lockedManifestPathForKey(key *CacheKey) (meta.AbsoluteFilePath, bool) {
	dc.lockedEnsureIndex()
	path, ok := dc.byKey[key.String()]
	return path, ok
}

// lockedEnsureIndex reads the cache directory once and records where each entry
// lives, so a lookup is a map hit rather than a scan that parses every manifest
// in the tree. Callers hold dc.mu.
func (dc *DiskCache) lockedEnsureIndex() {
	if dc.indexed {
		return
	}
	dc.indexed = true // a directory that cannot be read stays empty, not rescanned

	entries, err := os.ReadDir(dc.directory)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), MANIFEST_EXT) {
			continue
		}
		path := filepath.Join(dc.directory, e.Name())
		man, err := ReadManifest(path)
		if err != nil || man.Key == "" {
			continue
		}
		dc.lockedIndex(man.Key, man.Component, path)
	}
}

func (dc *DiskCache) lockedIndex(keyStr meta.CacheKeyString, component meta.QualifiedName, manifestPath meta.AbsoluteFilePath) {
	dc.byKey[keyStr] = manifestPath
	dc.byComponent.Add(component, keyStr)
}

func ReadManifest(path string) (*ComponentDiskManifest, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m ComponentDiskManifest
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	return &m, nil
}
