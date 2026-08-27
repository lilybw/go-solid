package internal

import (
	"sync"

	"github.com/lilybw/go-solid/internal/collections"
	"github.com/lilybw/go-solid/internal/esbuild"
	"github.com/lilybw/go-solid/shared/meta"
)

type DependencyIndex struct {
	mu       sync.RWMutex
	bySource collections.SetMap[meta.AbsoluteFilePath, meta.QualifiedName]
}

func NewDepIndex() *DependencyIndex {
	return &DependencyIndex{bySource: collections.SetMap[meta.AbsoluteFilePath, meta.QualifiedName]{}}
}

// Record registers that component depends on the given source files.
func (d *DependencyIndex) Record(component string, sources []string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, src := range sources {
		d.bySource.Add(esbuild.NormalizeSourcePath(src), component)
	}
}

// DependentsOf returns the component names that depend on the given source file.
func (d *DependencyIndex) DependentsOf(source meta.AbsoluteFilePath) []string {
	key := esbuild.NormalizeSourcePath(source)
	d.mu.RLock()
	defer d.mu.RUnlock()

	return d.bySource.MembersOf(key)
}
