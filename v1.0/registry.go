package go_solid

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/lilybw/go_solid/internal/meta"
)

// Component is one entry discovered in the components directory.
type Component struct {
	// Name is the registry key: the path relative to the components root,
	// without extension, using forward slashes. e.g. "auth/LoginForm".
	Name string
	// AbsPath is the absolute path to the .tsx/.jsx source file on disk.
	AbsPath string
	// Ext is the source extension (".tsx", ".jsx", ".ts", ".js").
	Ext         string
	MountRootID string // optional: if non-empty, the HTML shell will mount this component at this id instead of the default "go-solid-root"
}

func newComponent(name, absPath, ext string) Component {
	return Component{Name: name, AbsPath: absPath, Ext: ext, MountRootID: fmt.Sprintf("%s-go-solid-root", strings.ReplaceAll(name, "/", "-"))}
}

// Registry maps template names to component source files. It regenerates itself
// from the contents of a folder, so adding a component is just adding a file.
type Registry struct {
	root       meta.AbsoluteDirectoryPath
	mu         sync.RWMutex
	components map[meta.QualifiedName]Component
}

// registryExtensions are the file types treated as component entry points.
var registryExtensions = map[string]bool{
	".tsx": true,
	".jsx": true,
	".ts":  true,
	".js":  true,
}

// NewRegistry walks root and indexes every component file. The template name is
// the relative path minus extension: components/auth/LoginForm.tsx => "auth/LoginForm".
func NewRegistry(root meta.AbsoluteDirectoryPath) (*Registry, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("registry: resolve root: %w", err)
	}
	r := &Registry{root: abs, components: make(map[meta.QualifiedName]Component)}
	if err := r.Reload(); err != nil {
		return nil, err
	}
	return r, nil
}

// Reload rescans the root directory, rebuilding the index from scratch. Safe to
// call at runtime (e.g. in dev mode on each request, or on a filesystem watch).
func (r *Registry) Reload() error {
	found := make(map[meta.QualifiedName]Component)

	walkErr := filepath.WalkDir(r.root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// Skip node_modules and dotfolders defensively.
			base := d.Name()
			if base == "node_modules" || (strings.HasPrefix(base, ".") && path != r.root) {
				return fs.SkipDir
			}
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if !registryExtensions[ext] {
			return nil
		}
		rel, relErr := filepath.Rel(r.root, path)
		if relErr != nil {
			return relErr
		}
		name := strings.TrimSuffix(filepath.ToSlash(rel), ext)

		// Collision guard: two files resolving to the same name (Foo.tsx and
		// Foo.jsx) is ambiguous and almost certainly a mistake.
		if existing, dup := found[name]; dup {
			return fmt.Errorf("registry: duplicate component %q from %s and %s",
				name, existing.AbsPath, path)
		}
		found[name] = newComponent(name, path, ext)
		return nil
	})
	if walkErr != nil {
		return fmt.Errorf("registry: walk %s: %w", r.root, walkErr)
	}

	r.mu.Lock()
	r.components = found
	r.mu.Unlock()
	return nil
}

// Lookup returns the component registered under name, or ok=false.
func (r *Registry) Lookup(component meta.QualifiedName) (Component, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, ok := r.components[component]
	return c, ok
}

type QualifiedNameSlice []meta.QualifiedName

func (this QualifiedNameSlice) ToStringSlice() []string {
	out := make([]string, len(this))
	copy(out, this)
	return out
}

// Names returns all registered component names, sorted. Useful for debugging
// and for a dev-mode index page.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]meta.QualifiedName, 0, len(r.components))
	for n := range r.components {
		names = append(names, n)
	}
	sort.Slice(names, func(i, j int) bool {
		return names[i] < names[j]
	})
	return names
}

// Root returns the absolute components root directory.
func (r *Registry) Root() string { return r.root }
