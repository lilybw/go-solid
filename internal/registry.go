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
	. "github.com/lilybw/go-solid/shared/registry"
	watching "github.com/lilybw/go-solid/shared/watching"
)

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

// DECLARATION_SUFFIX marks a TypeScript declaration file. Its extension is
// .ts, but it declares types rather than exporting a component, so it is never
// a registry entry — including the definitions go_solid generates, which land
// under the workspace and would otherwise be indexed when the workspace sits
// inside the components tree.
const DECLARATION_SUFFIX = ".d.ts"

// eligible reports whether path can back a component.
func eligible(path meta.AbsoluteFilePath) bool {
	if strings.HasSuffix(strings.ToLower(path), DECLARATION_SUFFIX) {
		return false
	}
	return registryExtensions[strings.ToLower(filepath.Ext(path))]
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

// MakeReactive starts watching the components tree. onDrop is called with the
// qualified name of a component whose backing file was removed; onTouch is
// called with the path of any watched file that was created or written, so the
// caller can invalidate whatever it derived from that file.
// A registry can only be made reactive once: a second watcher over the same
// tree would double every callback and orphan the first one's goroutine.
func (this *ComponentRegistry) MakeReactive(onDrop func(meta.QualifiedName), onTouch func(meta.AbsoluteFilePath), onErr func(error)) error {
	this.mu.Lock()
	already := this.watcher != nil
	this.mu.Unlock()
	if already {
		return fmt.Errorf("registry: already reactive; call Close before making it reactive again")
	}

	if onDrop == nil {
		onDrop = func(meta.QualifiedName) {}
	}
	if onTouch == nil {
		onTouch = func(meta.AbsoluteFilePath) {}
	}
	rw, err := watching_int.NewDirectoryWatcher(
		this.root,
		&watching.DWVoidConfig{
			OnCreation: func(file meta.AbsoluteFilePath, derived meta.Void) error {
				_, _, err := this.AddFile(file)
				onTouch(file)
				return err
			},
			OnDeletion: func(file meta.AbsoluteFilePath, derived meta.Void) error {
				if qualifiedName, removed := this.RemoveFile(file); removed {
					onDrop(qualifiedName)
				}
				return nil
			},
			OnMutation: func(file meta.AbsoluteFilePath, derived meta.Void) error {
				onTouch(file)
				return nil
			},
			OnErr: onErr,
		},
	)
	if err != nil {
		return fmt.Errorf("registry: make reactive: %w", err)
	}

	this.mu.Lock()
	this.watcher = rw
	this.mu.Unlock()
	return nil
}

// Close stops the reactive watcher, if MakeReactive started one. Idempotent,
// and safe on a nil receiver.
func (this *ComponentRegistry) Close() {
	if this == nil {
		return
	}
	this.mu.Lock()
	watcher := this.watcher
	this.watcher = nil
	this.mu.Unlock()

	watcher.Stop() // nil-safe
}

// NameForFile maps a path under the components root to its qualified name.
func (this *ComponentRegistry) NameForFile(path meta.AbsoluteFilePath) (meta.QualifiedName, bool) {
	name, ok := this.qualifiedNameFor(path)
	if !ok {
		return "", false
	}
	this.mu.RLock()
	defer this.mu.RUnlock()
	_, known := this.components[name]
	return name, known
}

func (this *ComponentRegistry) qualifiedNameFor(path meta.AbsoluteFilePath) (meta.QualifiedName, bool) {
	ext := strings.ToLower(filepath.Ext(path))
	if !eligible(path) {
		return "", false
	}
	rel, err := filepath.Rel(this.root, path)
	if err != nil {
		return "", false
	}
	return strings.TrimSuffix(filepath.ToSlash(rel), ext), true
}

// AddFile registers a single file if it's a registry-eligible component.
// Returns the qualified name and true if a component was added or updated.
func (this *ComponentRegistry) AddFile(path meta.AbsoluteFilePath) (meta.QualifiedName, bool, error) {
	ext := strings.ToLower(filepath.Ext(path))
	if !eligible(path) {
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
	this.components[name] = *NewComponent(name, path, ext)
	return name, true, nil
}

// RemoveFile drops a component by its absolute path. Returns the qualified
// name that was removed and true if something was actually removed.
func (this *ComponentRegistry) RemoveFile(path meta.AbsoluteFilePath) (meta.QualifiedName, bool) {
	ext := strings.ToLower(filepath.Ext(path))
	if !eligible(path) {
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
		if !eligible(path) {
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
		found[name] = *NewComponent(name, path, ext)
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
func (this *ComponentRegistry) Lookup(component meta.QualifiedName) (*Component, bool) {
	this.mu.RLock()
	defer this.mu.RUnlock()
	c, ok := this.components[component]
	return &c, ok
}

// Names returns all registered component names, sorted.
func (this *ComponentRegistry) Names() []meta.QualifiedName {
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

// Components returns every registered component, ordered by name.
func (this *ComponentRegistry) Components() []*Component {
	this.mu.RLock()
	defer this.mu.RUnlock()
	out := make([]*Component, 0, len(this.components))
	for _, c := range this.components {
		out = append(out, &c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Root returns the absolute components root directory.
func (this *ComponentRegistry) Root() string { return this.root }
