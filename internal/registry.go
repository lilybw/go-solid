package internal

import (
	"fmt"
	"io/fs"
	"path/filepath"
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

var registryExtensions = map[string]bool{
	".tsx": true,
	".jsx": true,
	".ts":  true,
	".js":  true,
}

const DECLARATION_SUFFIX = ".d.ts"

func eligible(path meta.AbsoluteFilePath) bool {
	if strings.HasSuffix(strings.ToLower(path), DECLARATION_SUFFIX) {
		return false
	}
	return registryExtensions[strings.ToLower(filepath.Ext(path))]
}

// nameIsAddressable rejects a derived name a selector could never reach.
func nameIsAddressable(name meta.QualifiedName, path meta.AbsoluteFilePath) error {
	if strings.Contains(name, meta.EXPORT_SELECTOR) {
		return fmt.Errorf("registry: %s: %q may not appear in a component path (it selects an export, as in %q)",
			path, meta.EXPORT_SELECTOR, "auth/LoginForm"+meta.EXPORT_SELECTOR+"Submit")
	}
	return nil
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
// called with the path of any watched file that was created or written.
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
	if err := nameIsAddressable(name, path); err != nil {
		return "", false, err
	}

	this.mu.Lock()
	defer this.mu.Unlock()
	if existing, dup := this.components[name]; dup && existing.Path != path {
		return "", false, fmt.Errorf("registry: duplicate component %q from %s and %s", name, existing.Path, path)
	}
	this.components[name] = *NewComponent(name, path, ext)
	return name, true, nil
}

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

		if err := nameIsAddressable(name, path); err != nil {
			return err
		}

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
// Lookup resolves a selector to the component it names.
func (this *ComponentRegistry) Lookup(component meta.QualifiedName) (*Component, bool) {
	file, export := meta.SplitSelector(component)

	this.mu.RLock()
	c, ok := this.components[file]
	this.mu.RUnlock()
	if !ok {
		return nil, false
	}
	if export == "" {
		return &c, true
	}
	return c.WithExport(export), true
}

// Safely map each member of the internal map to some repressentation and return it.
// Depending on the repressentation, this might not b
func (this *ComponentRegistry) Map[T any](fn func(k meta.QualifiedName, v *Component) T) []T {
	this.mu.RLock()
	defer this.mu.RUnlock()

	mapped := make([]T, 0, len(this.components))
	for k, v := range this.components {
		mapped = append(mapped, fn(k, &v))
	}

	return mapped
}

func (this *ComponentRegistry) Root() string { return this.root }
