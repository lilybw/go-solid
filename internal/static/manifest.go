package static

import (
	"fmt"
	"mime"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	caching "github.com/lilybw/go-solid/internal/caching"
	"github.com/lilybw/go-solid/internal/meta"
	. "github.com/lilybw/go-solid/shared/static"
)

// The manifest
// -----------------------------------------------------------------------------
// Walking the asset directory produces one closed list of what may be served,
// and every later question is answered from it. The endpoint never resolves a
// client-supplied path against the filesystem — a request either names an entry
// in this list or it names nothing — so path traversal is not something to
// defend against so much as something there is no route to.
//
// URLs are content-hashed. That makes them immutable, so they can be cached
// forever without revalidation, and it makes a changed asset a changed URL,
// which is what lets the rest of the system notice.

type Asset struct {
	Rel  meta.RelativeFilePath
	Path meta.AbsoluteFilePath
	// URL is the content-hashed public path, e.g.
	// "/__go_solid_static__/images/logo.9f2a1c4b.svg".
	URL         string
	MIME        string
	Size        int64
	ContentHash string
	MemCached   []byte
}

// Manifest is the closed set of servable assets, indexed by URL.
type Manifest struct {
	// Root is the asset directory the manifest describes.
	Root meta.AbsoluteDirectoryPath
	// MountPath is the URL prefix every asset sits under.
	MountPath string
	// ByURL is what the endpoint looks in. Nothing outside it is servable.
	ByURL map[string]*Asset
	// ByRel resolves an asset from its path within the directory, which is how
	// Go-side callers address one.
	ByRel map[string]*Asset
	// Tree is the directory structure the generated module mirrors.
	Tree *Dir
	// Sources are every file the manifest was built from, sorted. A change to
	// any of them changes the manifest.
	Sources []meta.AbsoluteFilePath
}

// Dir is one directory in the asset tree.
type Dir struct {
	// Name is the sanitized key this directory appears under in its parent.
	Name string
	// Rel is the directory's path within the asset root, forward-slashed.
	Rel string
	// Dirs and Files are sorted by key, so generation is deterministic.
	Dirs  []*Dir
	Files []*Entry
}

// Entry is a leaf in the generated object. One key may cover several assets
// that differ only by extension, in which case they become subfields.
type Entry struct {
	// Key is the sanitized name this entry appears under.
	Key string
	// Asset is set when the key resolves straight to one asset.
	Asset *Asset
	// ByExtension is set instead when several assets share a key, and holds
	// them under their sanitized extensions.
	ByExtension []*ExtensionEntry
}

// ExtensionEntry is one asset under a key shared with others.
type ExtensionEntry struct {
	Key   string
	Asset *Asset
}

// BuildManifest walks root and describes everything servable under it.
func BuildManifest(cfg *StaticConfig) (*Manifest, error) {
	root := cfg.Location
	mount := cfg.MountPath
	if mount == "" {
		mount = DEFAULT_MOUNT_PATH
	}
	ignore := cfg.Ignore
	if ignore == nil {
		ignore = DEFAULT_IGNORE
	}
	limit := cfg.InlineLimit
	if limit == 0 {
		limit = DEFAULT_INLINE_LIMIT
	}

	m := &Manifest{
		Root:      root,
		MountPath: mount,
		ByURL:     map[string]*Asset{},
		ByRel:     map[string]*Asset{},
	}
	tree, err := scanDir(m, root, "", ignore, limit)
	if err != nil {
		return nil, err
	}
	m.Tree = tree
	sort.Strings(m.Sources)
	return m, nil
}

// Lookup resolves an asset by its path within the asset directory.
//
//	asset, ok := manifest.Lookup("images/logo.svg")
func (m *Manifest) Lookup(rel string) (*Asset, bool) {
	if m == nil {
		return nil, false
	}
	asset, ok := m.ByRel[path.Clean(strings.TrimPrefix(filepath.ToSlash(rel), "/"))]
	return asset, ok
}

// URL is Lookup returning only the public path, empty when there is no such
// asset.
func (m *Manifest) URL(rel string) string {
	if asset, ok := m.Lookup(rel); ok {
		return asset.URL
	}
	return ""
}

func scanDir(m *Manifest, dir meta.AbsoluteDirectoryPath, rel string, ignore []FileSelectorPattern, limit int64) (*Dir, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("go_solid/static: read %q: %w", dir, err)
	}

	node := &Dir{Name: sanitize(path.Base(rel)), Rel: rel}
	if rel == "" {
		node.Name = ""
	}

	// keys maps a sanitized name to what claims it, so a collision is reported
	// with both sides rather than resolved by whoever was read last.
	claimed := map[string]string{}
	grouped := map[string][]*Asset{}
	var order []string

	for _, entry := range entries {
		name := entry.Name()
		if skip, err := checkNameAgainstPatterns(name, ignore...); err != nil {
			return nil, fmt.Errorf("go_solid/static: ignore pattern: %w", err)
		} else if skip {
			continue
		}
		if strings.HasPrefix(name, ".") {
			continue // dotfiles are never assets
		}

		childRel := name
		if rel != "" {
			childRel = rel + "/" + name
		}

		if entry.IsDir() {
			key := sanitize(name)
			if err := claim(claimed, key, childRel+"/", "directory"); err != nil {
				return nil, err
			}
			child, err := scanDir(m, filepath.Join(dir, name), childRel, ignore, limit)
			if err != nil {
				return nil, err
			}
			child.Name = key
			node.Dirs = append(node.Dirs, child)
			continue
		}

		asset, err := describe(filepath.Join(dir, name), childRel, m.MountPath, limit)
		if err != nil {
			return nil, err
		}
		m.ByURL[asset.URL] = asset
		m.ByRel[asset.Rel] = asset
		m.Sources = append(m.Sources, asset.Path)

		key := sanitize(strings.TrimSuffix(name, filepath.Ext(name)))
		if _, seen := grouped[key]; !seen {
			order = append(order, key)
		}
		grouped[key] = append(grouped[key], asset)
	}

	for _, key := range order {
		assets := grouped[key]
		if err := claim(claimed, key, assets[0].Rel, "file"); err != nil {
			return nil, err
		}
		entry, err := entryFor(key, assets)
		if err != nil {
			return nil, err
		}
		node.Files = append(node.Files, entry)
	}

	sort.Slice(node.Dirs, func(i, j int) bool { return node.Dirs[i].Name < node.Dirs[j].Name })
	sort.Slice(node.Files, func(i, j int) bool { return node.Files[i].Key < node.Files[j].Key })
	return node, nil
}

// entryFor turns the assets sharing a key into one leaf. A single asset becomes
// the key itself; several become subfields under their extensions, which is the
// only unambiguous thing to do with logo.svg beside logo.png.
func entryFor(key string, assets []*Asset) (*Entry, error) {
	if len(assets) == 1 {
		return &Entry{Key: key, Asset: assets[0]}, nil
	}

	byExt := map[string]*Asset{}
	entry := &Entry{Key: key}
	for _, asset := range assets {
		ext := sanitize(strings.TrimPrefix(filepath.Ext(asset.Rel), "."))
		if ext == "" {
			ext = "none"
		}
		if other, dup := byExt[ext]; dup {
			return nil, fmt.Errorf(
				"go_solid/static: %q and %q both resolve to the key %q.%q; rename one",
				other.Rel, asset.Rel, key, ext)
		}
		byExt[ext] = asset
		entry.ByExtension = append(entry.ByExtension, &ExtensionEntry{Key: ext, Asset: asset})
	}
	sort.Slice(entry.ByExtension, func(i, j int) bool {
		return entry.ByExtension[i].Key < entry.ByExtension[j].Key
	})
	return entry, nil
}

func claim(claimed map[string]string, key, by, kind string) error {
	if previous, taken := claimed[key]; taken {
		return fmt.Errorf(
			"go_solid/static: %q and %q both resolve to the key %q; rename one",
			previous, by, key)
	}
	claimed[key] = by + " (" + kind + ")"
	return nil
}

func describe(abs meta.AbsoluteFilePath, rel, mount string, limit int64) (*Asset, error) {
	body, err := os.ReadFile(abs)
	if err != nil {
		return nil, fmt.Errorf("go_solid/static: read %q: %w", abs, err)
	}
	digest := caching.ShortHash(string(body), 64)
	ext := filepath.Ext(rel)

	asset := &Asset{
		Rel:         rel,
		Path:        abs,
		URL:         mount + strings.TrimSuffix(rel, ext) + "." + digest[:8] + ext,
		MIME:        mediaType(ext),
		Size:        int64(len(body)),
		ContentHash: digest,
	}
	if asset.Size <= limit {
		asset.MemCached = body
	}
	return asset, nil
}

// mediaType resolves an extension, falling back to a type that tells a browser
// to download rather than guess.
func mediaType(ext string) string {
	if t := mime.TypeByExtension(ext); t != "" {
		if base, _, err := mime.ParseMediaType(t); err == nil {
			return base
		}
		return t
	}
	switch strings.ToLower(ext) {
	case ".woff2":
		return "font/woff2"
	case ".woff":
		return "font/woff"
	case ".webmanifest":
		return "application/manifest+json"
	}
	return "application/octet-stream"
}

func checkNameAgainstPatterns(name string, patterns ...FileSelectorPattern) (bool, error) {
	for _, pattern := range patterns {
		matches, err := filepath.Match(pattern, name)
		if err != nil {
			return false, err
		}
		if matches {
			return true, nil
		}
	}
	return false, nil
}
