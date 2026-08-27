package static

import (
	"fmt"
	"mime"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/lilybw/go-solid/internal/hashing"
	"github.com/lilybw/go-solid/internal/meta"
	. "github.com/lilybw/go-solid/shared/static"
)

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

// String santized to be usable as a js identifier
type SanitizedString = string

// Dir is one directory in the asset tree.
type Dir struct {
	Name SanitizedString
	Rel  meta.RelativeDirectoryPath
	// Dirs and Files are sorted by key, so generation is deterministic.
	Dirs  []*Dir
	Files []*Entry
}

type Entry struct {
	Key         SanitizedString
	Asset       *Asset
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
	mount := cfg.EffectiveMountPath()
	ignore := cfg.EffectiveIgnore()
	limit := cfg.EffectiveInlineLimit()

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

func (m *Manifest) Lookup(file meta.RelativeFilePath) (*Asset, bool) {
	if m == nil {
		return nil, false
	}
	asset, ok := m.ByRel[path.Clean(strings.TrimPrefix(filepath.ToSlash(file), "/"))]
	return asset, ok
}

// TODO: NO SILENT FAILURES! NO CAPES!
func (m *Manifest) URL(rel string) string {
	if asset, ok := m.Lookup(rel); ok {
		return asset.URL
	}
	return ""
}

func scanDir(m *Manifest, dir meta.AbsoluteDirectoryPath, rel meta.RelativeDirectoryPath, ignore []FileSelectorPattern, limit int64) (*Dir, error) {
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
	claimed := map[SanitizedString]string{}
	grouped := map[SanitizedString][]*Asset{}
	var order []SanitizedString

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
func entryFor(key SanitizedString, assets []*Asset) (*Entry, error) {
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

// kind: 'directory'|'file'
func claim(claimed map[SanitizedString]string, key SanitizedString, by meta.RelativeFilePath, kind string) error {
	if previous, taken := claimed[key]; taken {
		return fmt.Errorf(
			"go_solid/static: %q and %q both resolve to the key %q; rename one",
			previous, by, key)
	}
	claimed[key] = by + " (" + kind + ")"
	return nil
}

func describe(abs meta.AbsoluteFilePath, rel meta.RelativeFilePath, mount string, limit int64) (*Asset, error) {
	body, err := os.ReadFile(abs)
	if err != nil {
		return nil, fmt.Errorf("go_solid/static: read %q: %w", abs, err)
	}
	digest := hashing.Short(string(body), 64)
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
