package internal

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/lilybw/go-solid/internal/meta"
	watching_int "github.com/lilybw/go-solid/internal/watching"
	watching "github.com/lilybw/go-solid/shared/watching"
)

// Component is one entry discovered in the components directory.
type Component struct {
	// Name is the registry key: the path relative to the components root,
	// without extension, using forward slashes. e.g. "auth/LoginForm".
	Name meta.QualifiedName
	// Path is the absolute path to the .tsx/.jsx source file on disk.
	Path meta.AbsoluteFilePath
	// Ext is the source extension (".tsx", ".jsx", ".ts", ".js").
	Ext         string
	MountRootID string // optional: if non-empty, the HTML shell will mount this component at this id instead of the default "go-solid-root"
}

func NewComponent(name meta.QualifiedName, path meta.AbsoluteFilePath, ext string) Component {
	return Component{Name: name, Path: path, Ext: ext, MountRootID: fmt.Sprintf("%s-go-solid-root", strings.ReplaceAll(name, "/", "-"))}
}

// ComponentRegistry maps template names to component source files. It regenerates itself
// from the contents of a folder, so adding a component is just adding a file.
type ComponentRegistry struct {
	root       meta.AbsoluteDirectoryPath
	mu         sync.RWMutex
	components map[meta.QualifiedName]Component
	// nil if registry is not reactive
	watcher *watching_int.DirectoryWatcher[meta.Void] // optional: if non-nil, watches the components tree and updates the registry on change
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
func NewRegistry(root meta.AbsoluteDirectoryPath) (*ComponentRegistry, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("registry: resolve root: %w", err)
	}
	r := &ComponentRegistry{root: abs, components: make(map[meta.QualifiedName]Component)}
	if err := r.Reload(); err != nil {
		return nil, err
	}

	return r, nil
}

func (this *ComponentRegistry) MakeReactive(onDrop func(meta.QualifiedName), onErr func(error)) error {
	rw, err := watching_int.NewDirectoryWatcher(
		this.root,
		&watching.DWVoidConfig{
			OnCreation: func(file meta.AbsoluteFilePath, derived meta.Void) error {
				_, _, err := this.AddFile(file)
				return err
			},
			OnDeletion: func(file meta.AbsoluteFilePath, derived meta.Void) error {
				if qualifiedName, removed := this.RemoveFile(file); removed {
					onDrop(qualifiedName)
				}
				return nil
			},
			OnErr: onErr,
		},
	)
	if err != nil {
		return fmt.Errorf("registry: make reactive: %w", err)
	}
	this.watcher = rw
	return nil
}

// AddFile registers a single file if it's a registry-eligible component.
// Returns the qualified name and true if a component was added or updated.
func (this *ComponentRegistry) AddFile(path meta.AbsoluteFilePath) (meta.QualifiedName, bool, error) {
	ext := strings.ToLower(filepath.Ext(path))
	if !registryExtensions[ext] {
		return "", false, nil
	}
	rel, err := filepath.Rel(this.root, path)
	if err != nil {
		return "", false, err
	}
	name := strings.TrimSuffix(filepath.ToSlash(rel), ext)

	this.mu.Lock()
	defer this.mu.Unlock()
	if existing, dup := this.components[name]; dup && existing.Path != path {
		return "", false, fmt.Errorf("registry: duplicate component %q from %s and %s", name, existing.Path, path)
	}
	this.components[name] = NewComponent(name, path, ext)
	return name, true, nil
}

// RemoveFile drops a component by its absolute path. Returns the qualified
// name that was removed and true if something was actually removed.
func (this *ComponentRegistry) RemoveFile(path meta.AbsoluteFilePath) (meta.QualifiedName, bool) {
	ext := strings.ToLower(filepath.Ext(path))
	if !registryExtensions[ext] {
		return "", false
	}
	rel, err := filepath.Rel(this.root, path)
	if err != nil {
		return "", false
	}
	name := strings.TrimSuffix(filepath.ToSlash(rel), ext)

	this.mu.Lock()
	defer this.mu.Unlock()
	if _, ok := this.components[name]; !ok {
		return "", false
	}
	delete(this.components, name)
	return name, true
}

// Reload rescans the root directory, rebuilding the index from scratch. Safe to
// call at runtime (e.g. in dev mode on each request, or on a filesystem watch).
func (this *ComponentRegistry) Reload() error {
	found := make(map[meta.QualifiedName]Component)

	walkErr := filepath.WalkDir(this.root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// Skip node_modules and dotfolders defensively.
			base := d.Name()
			if base == "node_modules" || (strings.HasPrefix(base, ".") && path != this.root) {
				return fs.SkipDir
			}
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if !registryExtensions[ext] {
			return nil
		}
		rel, relErr := filepath.Rel(this.root, path)
		if relErr != nil {
			return relErr
		}
		name := strings.TrimSuffix(filepath.ToSlash(rel), ext)

		// Collision guard: two files resolving to the same name (Foo.tsx and
		// Foo.jsx) is ambiguous and almost certainly a mistake.
		if existing, dup := found[name]; dup {
			return fmt.Errorf("registry: duplicate component %q from %s and %s",
				name, existing.Path, path)
		}
		found[name] = NewComponent(name, path, ext)
		return nil
	})
	if walkErr != nil {
		return fmt.Errorf("registry: walk %s: %w", this.root, walkErr)
	}

	this.mu.Lock()
	this.components = found
	this.mu.Unlock()
	return nil
}

// Lookup returns the component registered under name, or ok=false.
func (this *ComponentRegistry) Lookup(component meta.QualifiedName) (Component, bool) {
	this.mu.RLock()
	defer this.mu.RUnlock()
	c, ok := this.components[component]
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
func (this *ComponentRegistry) Names() []string {
	this.mu.RLock()
	defer this.mu.RUnlock()
	names := make([]meta.QualifiedName, 0, len(this.components))
	for n := range this.components {
		names = append(names, n)
	}
	sort.Slice(names, func(i, j int) bool {
		return names[i] < names[j]
	})
	return names
}

// Root returns the absolute components root directory.
func (this *ComponentRegistry) Root() string { return this.root }
