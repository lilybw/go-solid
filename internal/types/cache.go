package types

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/lilybw/go-solid/internal/meta"
)

// CACHE_DIR_NAME is the workspace sub-directory holding extracted props shapes.
//
// It is an internal cache, not a published surface: nothing here is meant to be
// imported, the layout and format are free to change, and deleting it costs
// only the work of extracting again. The importable definitions live under
// shared/types#TYPES_DIR_NAME.
const CACHE_DIR_NAME = "type_cache"

// CACHE_ENTRY_EXT is the extension of a cache entry, and the one place the
// on-disk format is named. Entries are JSON while the shape of the data is
// still moving; encodeEntry and decodeEntry are the only seam a packed binary
// format would have to replace.
const CACHE_ENTRY_EXT = ".types.json"

// entry is one component's extraction as it is stored.
//
// Sources records every file the shape was read from, each with the digest it
// had at the time. An entry is valid only while all of them still match, so
// editing a definition a component imports invalidates that component too —
// which a timestamp on the component alone would miss.
type entry struct {
	Component  meta.QualifiedName `json:"component"`
	Sources    map[string]string  `json:"sources"` // absPath -> "sha256:<hex>"
	Fields     []Field            `json:"fields"`
	Name       string             `json:"name,omitzero"`
	Found      bool               `json:"found"`
	HasParam   bool               `json:"hasParameter"`
	Unresolved []string           `json:"unresolved,omitzero"`
}

func encodeEntry(e entry) ([]byte, error) {
	// Deterministic fixes the ordering of Sources, so an unchanged entry is
	// byte-identical and never rewritten.
	return json.Marshal(e, json.Deterministic(true), jsontext.WithIndent("  "))
}

func decodeEntry(raw []byte) (entry, error) {
	var e entry
	return e, json.Unmarshal(raw, &e)
}

// stamp is a file's cheap identity, enough for the in-process layer.
type stamp struct {
	modTime time.Time
	size    int64
}

func stampOf(path meta.AbsoluteFilePath) (stamp, error) {
	info, err := os.Stat(path)
	if err != nil {
		return stamp{}, err
	}
	return stamp{modTime: info.ModTime(), size: info.Size()}, nil
}

func digestOf(path meta.AbsoluteFilePath) (string, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:]), true
}

// Cache holds extracted props shapes in memory and on disk.
//
// The two layers validate differently, on purpose. The memory layer compares
// each source's size and modification time: a stat is cheap enough to do on
// every render and catches an edit without waiting for HMR to say so. The disk
// layer compares content digests, which survive a restart, a fresh checkout, or
// a filesystem whose timestamps lie.
type Cache struct {
	root meta.AbsoluteDirectoryPath
	mu   sync.Mutex
	mem  map[meta.QualifiedName]memEntry
}

type memEntry struct {
	extraction Extraction
	stamps     map[meta.AbsoluteFilePath]stamp
}

func NewCache(workspace meta.AbsoluteDirectoryPath) *Cache {
	return &Cache{
		root: filepath.Join(workspace, CACHE_DIR_NAME),
		mem:  make(map[meta.QualifiedName]memEntry),
	}
}

// Root is the directory holding the cache.
func (c *Cache) Root() meta.AbsoluteDirectoryPath { return c.root }

// Path is where a component's entry lives. The tree mirrors the components
// directory, so an entry is findable from the component it describes.
// Path is where a component's entry lives. The export selector folds into the
// filename, so two components backed by one file get two entries and neither
// puts a "#" in a path.
func (c *Cache) Path(component meta.QualifiedName) meta.AbsoluteFilePath {
	file, export := meta.SplitSelector(component)
	stem := filepath.FromSlash(file)
	if export != "" {
		stem += "__" + export
	}
	return filepath.Join(c.root, stem+CACHE_ENTRY_EXT)
}

// Get returns a valid extraction for component, from memory or from disk.
func (c *Cache) Get(component meta.QualifiedName) (Extraction, bool) {
	c.mu.Lock()
	hit, ok := c.mem[component]
	c.mu.Unlock()
	if ok && stampsHold(hit.stamps) {
		return hit.extraction, true
	}

	stored, ok := c.read(component)
	if !ok {
		return Extraction{}, false
	}
	c.remember(component, stored)
	return stored, true
}

// Put stores an extraction in both layers.
func (c *Cache) Put(component meta.QualifiedName, extraction Extraction) error {
	if err := validComponentName(component); err != nil {
		return err
	}
	c.remember(component, extraction)
	return c.write(component, extraction)
}

// Invalidate drops component from memory. The disk entry is left to its own
// digests, which already refuse to answer for a changed source.
func (c *Cache) Invalidate(component meta.QualifiedName) {
	c.mu.Lock()
	delete(c.mem, component)
	c.mu.Unlock()
}

func (c *Cache) remember(component meta.QualifiedName, extraction Extraction) {
	stamps := make(map[meta.AbsoluteFilePath]stamp, len(extraction.Sources))
	for _, source := range extraction.Sources {
		s, err := stampOf(source)
		if err != nil {
			return // a source vanished; refuse to remember something stale
		}
		stamps[source] = s
	}
	c.mu.Lock()
	c.mem[component] = memEntry{extraction: extraction, stamps: stamps}
	c.mu.Unlock()
}

func stampsHold(stamps map[meta.AbsoluteFilePath]stamp) bool {
	for path, was := range stamps {
		now, err := stampOf(path)
		if err != nil || now != was {
			return false
		}
	}
	return len(stamps) > 0
}

func (c *Cache) read(component meta.QualifiedName) (Extraction, bool) {
	raw, err := os.ReadFile(c.Path(component))
	if err != nil {
		return Extraction{}, false
	}
	stored, err := decodeEntry(raw)
	if err != nil {
		return Extraction{}, false
	}
	if len(stored.Sources) == 0 {
		return Extraction{}, false
	}

	sources := make([]meta.AbsoluteFilePath, 0, len(stored.Sources))
	for path, recorded := range stored.Sources {
		digest, ok := digestOf(path)
		if !ok || digest != recorded {
			return Extraction{}, false
		}
		sources = append(sources, path)
	}
	slices.Sort(sources)

	return Extraction{
		Shape:        NewShape(stored.Fields),
		Name:         stored.Name,
		Found:        stored.Found,
		HasParameter: stored.HasParam,
		Sources:      sources,
		Unresolved:   stored.Unresolved,
	}, true
}

func (c *Cache) write(component meta.QualifiedName, extraction Extraction) error {
	sources := make(map[string]string, len(extraction.Sources))
	for _, source := range extraction.Sources {
		digest, ok := digestOf(source)
		if !ok {
			return fmt.Errorf("go_solid/types: cannot digest %q", source)
		}
		sources[source] = digest
	}

	raw, err := encodeEntry(entry{
		Component:  component,
		Sources:    sources,
		Fields:     extraction.Shape.fields,
		Name:       extraction.Name,
		Found:      extraction.Found,
		HasParam:   extraction.HasParameter,
		Unresolved: extraction.Unresolved,
	})
	if err != nil {
		return fmt.Errorf("go_solid/types: encode entry for %q: %w", component, err)
	}

	path := c.Path(component)
	if current, err := os.ReadFile(path); err == nil && slices.Equal(current, raw) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("go_solid/types: create %q: %w", filepath.Dir(path), err)
	}

	// Rename onto the target so a reader never sees a half-written entry.
	tmp, err := os.CreateTemp(filepath.Dir(path), ".go_solid-*.tmp")
	if err != nil {
		return fmt.Errorf("go_solid/types: stage %q: %w", path, err)
	}
	defer os.Remove(tmp.Name())

	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		return fmt.Errorf("go_solid/types: write %q: %w", tmp.Name(), err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("go_solid/types: close %q: %w", tmp.Name(), err)
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return fmt.Errorf("go_solid/types: chmod %q: %w", tmp.Name(), err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("go_solid/types: replace %q: %w", path, err)
	}
	return nil
}

// Prune removes entries for components that no longer exist, and any directory
// left empty by doing so.
func (c *Cache) Prune(known []meta.QualifiedName) (int, error) {
	if _, err := os.Stat(c.root); err != nil {
		return 0, nil // nothing cached yet
	}
	live := make(map[meta.AbsoluteFilePath]bool, len(known))
	for _, name := range known {
		live[c.Path(name)] = true
	}

	var stale []string
	if err := filepath.WalkDir(c.root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || live[path] {
			return err
		}
		if strings.HasSuffix(path, CACHE_ENTRY_EXT) {
			stale = append(stale, path)
		}
		return nil
	}); err != nil {
		return 0, fmt.Errorf("go_solid/types: walk %q: %w", c.root, err)
	}

	removed := 0
	for _, path := range stale {
		if err := os.Remove(path); err != nil {
			return removed, fmt.Errorf("go_solid/types: prune %q: %w", path, err)
		}
		removed++
	}
	c.removeEmptyDirs()
	return removed, nil
}

func (c *Cache) removeEmptyDirs() {
	var dirs []string
	_ = filepath.WalkDir(c.root, func(path string, d fs.DirEntry, err error) error {
		if err == nil && d.IsDir() && path != c.root {
			dirs = append(dirs, path)
		}
		return nil
	})
	// Deepest first, so a directory emptied by its children is caught too.
	slices.Reverse(dirs)
	for _, dir := range dirs {
		_ = os.Remove(dir) // fails harmlessly when not empty
	}
}

// validComponentName rejects names that would escape the cache tree.
func validComponentName(component meta.QualifiedName) error {
	if component == "" {
		return fmt.Errorf("go_solid/types: empty component name")
	}
	file, export := meta.SplitSelector(component)
	for segment := range strings.SplitSeq(file, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return fmt.Errorf("go_solid/types: component name %q is not a relative path", component)
		}
	}
	if !meta.ValidExportName(export) {
		return fmt.Errorf("go_solid/types: component name %q selects an export that cannot be imported", component)
	}
	return nil
}
