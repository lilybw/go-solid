package caching

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/lilybw/go-solid/internal/esbuild"
	"github.com/lilybw/go-solid/internal/meta"
)

// -----------------------------------------------------------------------------
// Disk-backed component cache
// -----------------------------------------------------------------------------
// Entries live under <workspace>/component_cache/ as human-readable, inspectable
// files. Each cache entry is a set of sibling files sharing a base name:
//
//   auth_LoginForm__root-a__<hash>.meta.json   manifest (component, sources, ...)
//   auth_LoginForm__root-a__<hash>.html
//   auth_LoginForm__root-a__<hash>.js
//   auth_LoginForm__root-a__<hash>.css          (only if the bundle has CSS)
//
// Invalidation is by SOURCE CONTENT HASH, not timestamps: the manifest records
// each source file's sha256, and an entry is valid only if every source still
// hashes to the recorded value. generatedAt is stored for humans, never used for
// correctness.
//
// A reverse index (component_cache/_index.json) maps each source file to the
// entry keys that depend on it, for fast "what must I invalidate" queries. The
// index is derived data: it is always rebuildable from the manifests, which are
// the source of truth. RebuildIndex regenerates it from scratch.

const CACHE_DIR_NAME = "component_cache"

// DiskManifest is the on-disk, human-readable entry descriptor.
type DiskManifest struct {
	Component   string            `json:"component"`
	RootID      string            `json:"rootID"`
	Minify      bool              `json:"minify"`
	GeneratedAt string            `json:"generatedAt"` // RFC3339, for humans
	Key         string            `json:"key"`         // cache key (also the base filename stem)
	Sources     map[string]string `json:"sources"`     // absPath -> "sha256:<hex>"
	Artifacts   struct {
		HTML meta.RelativeFilePath `json:"html"`
		JS   meta.RelativeFilePath `json:"js"`
		CSS  meta.RelativeFilePath `json:"css,omitempty"`
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

// DiskCache persists rendered bundles and maintains the reverse dependency index.
type DiskCache struct {
	workspace meta.AbsoluteDirectoryPath // <workspace>/component_cache
	mu        sync.Mutex
	enabled   bool
}

func NewDiskCache(workspace string, enabled bool) (*DiskCache, error) {
	dir := filepath.Join(workspace, CACHE_DIR_NAME)
	dc := &DiskCache{workspace: dir, enabled: enabled}
	if !enabled {
		return dc, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("disk cache: create %q: %w", dir, err)
	}
	return dc, nil
}

// hashFile returns "sha256:<hex>" for a file's contents, or ok=false if unreadable.
func hashFile(file meta.AbsoluteFilePath) (string, bool) {
	b, err := os.ReadFile(file)
	if err != nil {
		return "", false
	}
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:]), true
}

// entryStem is the human-readable base filename for an entry.
func entryStem(component, rootID, key string) string {
	safe := func(s string) string {
		s = strings.ReplaceAll(s, "/", "_")
		s = strings.ReplaceAll(s, string(filepath.Separator), "_")
		return s
	}
	short := key
	if len(short) > 12 {
		short = short[:12]
	}
	return fmt.Sprintf("%s__%s__%s", safe(component), safe(rootID), short)
}

// Get returns a cached Rendered if a valid entry exists for key. Validity means
// every recorded source still hashes to its recorded value (invalidation).
func (dc *DiskCache) Get(key string) (*Rendered, bool) {
	if !dc.enabled {
		return nil, false
	}
	dc.mu.Lock()
	defer dc.mu.Unlock()

	manifestPath, ok := dc.manifestPathForKey(key)
	if !ok {
		return nil, false
	}
	man, err := readManifest(manifestPath)
	if err != nil {
		return nil, false
	}
	// Invalidation: every source must still match its recorded hash.
	for src, want := range man.Sources {
		got, ok := hashFile(src)
		if !ok || got != want {
			return nil, false // stale (edited or deleted source)
		}
	}
	// Load artifacts.
	base := strings.TrimSuffix(manifestPath, ".meta.json")
	html, err := os.ReadFile(base + ".html")
	if err != nil {
		return nil, false
	}
	js, err := os.ReadFile(base + ".js")
	if err != nil {
		return nil, false
	}
	r := &Rendered{
		HTML:   string(html),
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

// Put writes an entry (manifest + artifacts) and updates the reverse index.
// sources are the absolute paths from the bundle's metafile.
func (dc *DiskCache) Put(key, component, rootID string, minify bool, r *Rendered, sources []string) error {
	if !dc.enabled {
		return nil
	}
	dc.mu.Lock()
	defer dc.mu.Unlock()

	man := DiskManifest{
		Component:   component,
		RootID:      rootID,
		Minify:      minify,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Key:         key,
		Sources:     map[string]string{},
	}
	for _, src := range sources {
		key := esbuild.NormalizeSourcePath(src)
		if h, ok := hashFile(src); ok {
			man.Sources[key] = h
		}
	}
	stem := entryStem(component, rootID, key)
	man.Artifacts.HTML = stem + ".html"
	man.Artifacts.JS = stem + ".js"
	if r.CSS != "" {
		man.Artifacts.CSS = stem + ".css"
	}
	man.ServeNames.JS = r.JSName
	man.ServeNames.CSS = r.CSSName

	base := filepath.Join(dc.workspace, stem)
	if err := atomicWrite(base+".html", []byte(r.HTML)); err != nil {
		return err
	}
	if err := atomicWrite(base+".js", []byte(r.JS)); err != nil {
		return err
	}
	if r.CSS != "" {
		if err := atomicWrite(base+".css", []byte(r.CSS)); err != nil {
			return err
		}
	}
	manBytes, err := json.MarshalIndent(man, "", "  ")
	if err != nil {
		return err
	}
	if err := atomicWrite(base+".meta.json", manBytes); err != nil {
		return err
	}

	return nil
}

func (dc *DiskCache) manifestPathForKey(key string) (string, bool) {
	// The stem includes a 12-char key prefix; scan for the manifest whose Key
	// matches exactly (prefix could in principle collide, so verify).
	entries, err := os.ReadDir(dc.workspace)
	if err != nil {
		return "", false
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".meta.json") {
			continue
		}
		p := filepath.Join(dc.workspace, e.Name())
		if man, err := readManifest(p); err == nil && man.Key == key {
			return p, true
		}
	}
	return "", false
}

func readManifest(path string) (*DiskManifest, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m DiskManifest
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// atomicWrite writes via temp file + rename so a reader never sees a partial file.
func atomicWrite(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func appendUnique(list []string, v string) []string {
	if slices.Contains(list, v) {
		return list
	}
	return append(list, v)
}
