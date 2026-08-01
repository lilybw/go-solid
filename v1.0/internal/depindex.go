package internal

import (
	"sync"

	"github.com/lilybw/go_solid/internal/esbuild"
	"github.com/lilybw/go_solid/internal/meta"
)

// DepIndex is the reverse dependency graph: it maps each source file to the set
// of component names whose bundles include that source. It is maintained on
// every render (regardless of cache settings), because the graph is orthogonal
// to whether artifacts are persisted.
//
// Note the granularity: this maps source -> componentName, deliberately coarser
// than the disk cache's source -> entryKey mapping. HMR reloads a *template*,
// and one component viewed with different props is several entry keys but a
// single reloadable component. The watcher wants "which open tabs are affected",
// which is a question about component names, not cache keys.
//
// It is safe for concurrent use.
type DepIndex struct {
	mu sync.RWMutex
	// source absolute path -> set of component names depending on it
	bySource map[meta.AbsoluteFilePath]map[string]struct{}
}

func NewDepIndex() *DepIndex {
	return &DepIndex{bySource: map[meta.AbsoluteFilePath]map[string]struct{}{}}
}

// Record registers that component depends on the given source files. Sources are
// normalized to the same form the disk cache uses so lookups from the watcher
// (which sees raw fsnotify paths) match. Existing associations for the component
// are left intact — a component's source set only grows within a process
// lifetime, and stale sources are harmless (they map to a component that simply
// won't be viewed if it no longer imports them).
func (d *DepIndex) Record(component string, sources []string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, src := range sources {
		key := esbuild.NormalizeSourcePath(src)
		set := d.bySource[key]
		if set == nil {
			set = map[string]struct{}{}
			d.bySource[key] = set
		}
		set[component] = struct{}{}
	}
}

// DependentsOf returns the component names that depend on the given source file.
// The source is normalized before lookup, so callers may pass a raw path.
func (d *DepIndex) DependentsOf(source meta.AbsoluteFilePath) []string {
	key := esbuild.NormalizeSourcePath(source)
	d.mu.RLock()
	defer d.mu.RUnlock()
	set := d.bySource[key]
	if len(set) == 0 {
		return nil
	}
	out := make([]string, 0, len(set))
	for name := range set {
		out = append(out, name)
	}
	return out
}
