package types

import (
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	io_int "github.com/lilybw/go-solid/internal/io"
	"github.com/lilybw/go-solid/internal/sources"
	"github.com/lilybw/go-solid/shared/meta"
)

const CACHE_DIR_NAME = "type_cache"
const CACHE_ENTRY_EXT = ".types.json"

// entry is one component's extraction as it is stored.
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

// Cache holds extracted props shapes in memory and, when it is rooted anywhere,
// on disk. An unrooted cache is the whole of what a bundler with no filesystem
// keeps: the in-process layer, and nothing under it.
type Cache struct {
	root    meta.AbsoluteDirectoryPath
	mu      sync.Mutex
	mem     map[meta.QualifiedName]memEntry
	sources sources.Reader
}

type memEntry struct {
	extraction Extraction
	stamps     map[meta.AbsoluteFilePath]sources.Stamp
}

// NewCache roots a cache on a workspace, reading sources through reader. An
// empty workspace keeps the cache in memory; a nil reader reads from disk.
func NewCache(workspace meta.AbsoluteDirectoryPath, reader sources.Reader) *Cache {
	root := meta.AbsoluteDirectoryPath("")
	if workspace != "" {
		root = filepath.Join(workspace, CACHE_DIR_NAME)
	}
	return &Cache{
		root:    root,
		mem:     make(map[meta.QualifiedName]memEntry),
		sources: sources.OrDisk(reader),
	}
}

// persists reports whether entries outlive the process.
func (c *Cache) persists() bool { return c.root != "" }

func (c *Cache) Root() meta.AbsoluteDirectoryPath { return c.root }

// Path is where a component's entry lives. The tree mirrors the components
// directory, so an entry is findable from the component it describes.
func (c *Cache) Path(component meta.QualifiedName) meta.AbsoluteFilePath {
	file, export := meta.SplitSelector(component)
	stem := filepath.FromSlash(file)
	if export != "" {
		stem += "__" + export
	}
	return filepath.Join(c.root, stem+CACHE_ENTRY_EXT)
}

func (c *Cache) Get(component meta.QualifiedName) (Extraction, bool) {
	c.mu.Lock()
	hit, ok := c.mem[component]
	c.mu.Unlock()
	if ok && c.stampsHold(hit.stamps) {
		return hit.extraction, true
	}

	stored, ok := c.read(component)
	if !ok {
		return Extraction{}, false
	}
	c.remember(component, stored)
	return stored, true
}

func (c *Cache) Put(component meta.QualifiedName, extraction Extraction) error {
	if err := validComponentName(component); err != nil {
		return err
	}
	c.remember(component, extraction)
	return c.write(component, extraction)
}

func (c *Cache) Invalidate(component meta.QualifiedName) {
	c.mu.Lock()
	delete(c.mem, component)
	c.mu.Unlock()
}

func (c *Cache) remember(component meta.QualifiedName, extraction Extraction) {
	stamps := make(map[meta.AbsoluteFilePath]sources.Stamp, len(extraction.Sources))
	for _, source := range extraction.Sources {
		s, err := c.sources.Stamp(source)
		if err != nil {
			return // a source vanished; refuse to remember something stale
		}
		stamps[source] = s
	}
	c.mu.Lock()
	c.mem[component] = memEntry{extraction: extraction, stamps: stamps}
	c.mu.Unlock()
}

func (c *Cache) stampsHold(stamps map[meta.AbsoluteFilePath]sources.Stamp) bool {
	for path, was := range stamps {
		now, err := c.sources.Stamp(path)
		if err != nil || now != was {
			return false
		}
	}
	return len(stamps) > 0
}

// digestsHold reports whether every recorded source still hashes to what was
// recorded for it.
func (c *Cache) digestsHold(recorded map[meta.AbsoluteFilePath]meta.ContentDigest) bool {
	for path, want := range recorded {
		got, ok := c.sources.Digest(path)
		if !ok || got != want {
			return false
		}
	}
	return len(recorded) > 0
}

// digests records every source, failing on the first that cannot be hashed.
func (c *Cache) digests(paths []meta.AbsoluteFilePath) (map[meta.AbsoluteFilePath]meta.ContentDigest, meta.AbsoluteFilePath, bool) {
	out := make(map[meta.AbsoluteFilePath]meta.ContentDigest, len(paths))
	for _, path := range paths {
		digest, ok := c.sources.Digest(path)
		if !ok {
			return nil, path, false
		}
		out[path] = digest
	}
	return out, "", true
}

func (c *Cache) read(component meta.QualifiedName) (Extraction, bool) {
	if !c.persists() {
		return Extraction{}, false
	}
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

	if !c.digestsHold(stored.Sources) {
		return Extraction{}, false
	}
	paths := slices.Sorted(maps.Keys(stored.Sources))

	return Extraction{
		Shape:        NewShape(stored.Fields),
		Name:         stored.Name,
		Found:        stored.Found,
		HasParameter: stored.HasParam,
		Sources:      paths,
		Unresolved:   stored.Unresolved,
	}, true
}

func (c *Cache) write(component meta.QualifiedName, extraction Extraction) error {
	if !c.persists() {
		return nil
	}
	recorded, undigestible, ok := c.digests(extraction.Sources)
	if !ok {
		return fmt.Errorf("go_solid/types: cannot digest %q", undigestible)
	}

	raw, err := encodeEntry(entry{
		Component:  component,
		Sources:    recorded,
		Fields:     extraction.Shape.fields,
		Name:       extraction.Name,
		Found:      extraction.Found,
		HasParam:   extraction.HasParameter,
		Unresolved: extraction.Unresolved,
	})
	if err != nil {
		return fmt.Errorf("go_solid/types: encode entry for %q: %w", component, err)
	}

	if _, err := io_int.WriteIfChanged(c.Path(component), raw, 0o644); err != nil {
		return fmt.Errorf("go_solid/types: publish entry for %q: %w", component, err)
	}
	return nil
}

func (c *Cache) Prune(known []meta.QualifiedName) (int, error) {
	if !c.persists() {
		return 0, nil
	}
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
	for _, dir := range slices.Backward(dirs) {
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
