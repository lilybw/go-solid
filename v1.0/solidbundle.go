// Package solidbundle turns named SolidJS components into self-contained
// HTML+CSS+JS bundles, generated on demand and cached. It is designed to be
// imported into a Go web server (such as hots) so that a template name maps to
// a Solid component that is compiled adaptively — only the JS actually needed
// for that component is emitted.
//
// Pipeline per render:
//
//	name+props -> generate entry .tsx  (mounts <Component/> with props)
//	           -> babel-preset-solid   (JSX -> Solid template() calls; Node pool)
//	           -> esbuild (Go)          (typestrip, bundle, tree-shake, CSS, minify)
//	           -> assemble index.html   (references emitted CSS + JS)
//	           -> cache
//
// Node is required ONLY for the babel transform step, and only at build /
// cache-miss time — never per request once warm and cached.
package go_solid

import (
	"fmt"
	"strings"
)

// Config configures a Bundler.
type Config struct {
	// ComponentsDir is the root folder scanned for components. A file at
	// ComponentsDir/auth/LoginForm.tsx registers as template "auth/LoginForm".
	ComponentsDir string

	// DependenciesDir is the directory esbuild and Node resolve node_modules from. It
	// must contain (or have an ancestor containing) the peer dependencies
	// (solid-js, babel-preset-solid, @babel/core). Usually the frontend project
	// root.
	//
	// Optional: if empty, it defaults to ComponentsDir. Since consumers already
	// point ComponentsDir at their frontend tree, the peer deps installed there
	// (npm install solid-js babel-preset-solid @babel/core) resolve without any
	// extra configuration.
	DependenciesDir string

	// PoolSize is the number of persistent Node workers. 0/1 => single worker.
	PoolSize int

	// NodeBin overrides the node executable ("" => "node" on PATH).
	NodeBin string

	// Dev disables caching and emits sourcemaps; Minify controls esbuild
	// minification (usually !Dev).
	Dev    bool
	Minify bool
}

// Bundler is the top-level handle. Construct with New, close with Close.
type Bundler struct {
	cfg      Config
	registry *Registry
	pool     *Pool
	cache    *cache
}

// New constructs a Bundler: scans components, starts the worker pool.
func New(cfg Config) (*Bundler, error) {
	if cfg.ComponentsDir == "" {
		return nil, fmt.Errorf("go_solid: ComponentsDir is required")
	}
	// DependenciesDir defaults to the components directory: consumers already point that
	// at their frontend tree, so peer deps installed there resolve out of the box.
	if cfg.DependenciesDir == "" {
		cfg.DependenciesDir = cfg.ComponentsDir
	}

	scriptLocation, err := materializeWorkerScript()
	if err != nil {
		return nil, fmt.Errorf("go_solid: materialize worker script: %w", err)
	}

	if missing := peerDepsMissing(cfg.DependenciesDir, requiredPeerDeps); len(missing) > 0 {
		return nil, fmt.Errorf(
			"go_solid: missing Node peer dependencies %v in %q (or any ancestor).\n"+
				"Install them in your frontend project:\n"+
				"    npm install --save-dev %s",
			missing, cfg.DependenciesDir, strings.Join(missing, " "))
	}
	reg, err := NewRegistry(cfg.ComponentsDir)
	if err != nil {
		return nil, err
	}
	pool, err := newPool(PoolConfig{
		Size:    cfg.PoolSize,
		NodeBin: cfg.NodeBin,
		Script:  scriptLocation,
		WorkDir: cfg.DependenciesDir,
	})
	if err != nil {
		return nil, err
	}
	return &Bundler{
		cfg:      cfg,
		registry: reg,
		pool:     pool,
		cache:    newCache(!cfg.Dev),
	}, nil
}

// Registry exposes the underlying registry (for dev index pages, warmup, etc.).
func (b *Bundler) Registry() *Registry { return b.registry }

// Close shuts down the worker pool.
func (b *Bundler) Close() {
	if b == nil || b.pool == nil {
		return
	}
	b.pool.Close()
}
