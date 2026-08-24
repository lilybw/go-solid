package internal

import (
	"sync"

	"github.com/lilybw/go-solid/internal/esbuild"
	"github.com/lilybw/go-solid/internal/meta"
)

// DependencyIndex is a reverse dependency graph: it maps each source file to the set
// of component names whose bundles include that source. It is maintained on
// every render
//
// It is safe for concurrent use.
type DependencyIndex struct {
	mu sync.RWMutex
	// source absolute path -> set of component names depending on it
	bySource map[meta.AbsoluteFilePath]map[string]struct{}
}

func NewDepIndex() *DependencyIndex {
	return &DependencyIndex{bySource: map[meta.AbsoluteFilePath]map[string]struct{}{}}
}

// Record registers that component depends on the given source files.
func (d *DependencyIndex) Record(component string, sources []string) {
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
func (d *DependencyIndex) DependentsOf(source meta.AbsoluteFilePath) []string {
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
