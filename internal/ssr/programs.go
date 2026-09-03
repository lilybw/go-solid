package ssr

import (
	"fmt"
	"sync"

	"github.com/lilybw/go-solid-compiler/solid"
	"github.com/lilybw/go-solid-compiler/tsx"
	"github.com/lilybw/go-solid/internal/sources"
	types_int "github.com/lilybw/go-solid/internal/types"
	"github.com/lilybw/go-solid/shared/meta"
	"github.com/lilybw/go-solid/shared/registry"
)

// A program is the compiler's description of one component's markup: static
// runs interleaved with slots it proved derivable from props, plus what it
// could not describe. Resolving one is parsing and analysis, so results are
// held until the file behind them changes.

type entry struct {
	program *solid.Program
	stamp   sources.Stamp
	err     error
}

// programs caches one analysis per component.
type programs struct {
	mu      sync.RWMutex
	entries map[meta.QualifiedName]entry
	sources sources.Reader
}

func newPrograms(reader sources.Reader) *programs {
	return &programs{
		entries: make(map[meta.QualifiedName]entry),
		sources: sources.OrDisk(reader),
	}
}

func (p *programs) forget(component meta.QualifiedName) {
	p.mu.Lock()
	delete(p.entries, component)
	p.mu.Unlock()
}

// get returns the component's program, analyzing the file when the cached
// result is absent or stale.
func (p *programs) get(comp *registry.Component) (*solid.Program, error) {
	current, err := p.sources.Stamp(comp.Path)
	if err != nil {
		return nil, fmt.Errorf("go_solid/ssr: stat %q: %w", comp.Path, err)
	}

	p.mu.RLock()
	hit, ok := p.entries[comp.Name]
	p.mu.RUnlock()
	if ok && hit.stamp == current {
		return hit.program, hit.err
	}

	program, err := p.analyze(comp)
	p.mu.Lock()
	p.entries[comp.Name] = entry{program: program, stamp: current, err: err}
	p.mu.Unlock()
	return program, err
}

// analyze parses the component's file and asks the compiler to describe the
// markup of the export this component selects.
func (p *programs) analyze(comp *registry.Component) (*solid.Program, error) {
	source, err := p.sources.Read(comp.Path)
	if err != nil {
		return nil, fmt.Errorf("go_solid/ssr: read %q: %w", comp.Path, err)
	}
	tree := types_int.Parse(comp.Path, source.Text)
	if tree == nil {
		return nil, fmt.Errorf("go_solid/ssr: %q could not be parsed", comp.Path)
	}

	found := tsx.Components(tree)
	if len(found) == 0 {
		return nil, fmt.Errorf("go_solid/ssr: no component returning JSX found in %q", comp.Path)
	}

	// A selector names one export and nothing else will do.
	if want := comp.Export; want != "" {
		for _, c := range found {
			if c.Name == want {
				return c.Program, nil
			}
		}
		return nil, fmt.Errorf("go_solid/ssr: %q declares no component named %q", comp.Path, want)
	}

	// Without one the component is whatever the file exports by default, which
	// the compiler resolves through every shape that export is written in —
	// including the ones that leave it with no name to be found by.
	for _, c := range found {
		if c.Default {
			return c.Program, nil
		}
	}
	if len(found) == 1 {
		return found[0].Program, nil // one component and no default is unambiguous
	}
	return nil, fmt.Errorf(
		"go_solid/ssr: %q declares %d components and exports none of them by default; "+
			"name one with a selector", comp.Path, len(found))
}
