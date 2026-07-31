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
	"os"
	"path/filepath"
	"strings"

	caching "github.com/lilybw/go_solid/internal/caching"
	"github.com/lilybw/go_solid/internal/meta"
	"github.com/lilybw/go_solid/internal/workers"
)

// Config configures a Bundler.
type Config struct {
	// Components is the root folder scanned for components. A file at
	// Components/auth/LoginForm.tsx registers as template "auth/LoginForm".
	Components meta.AbsoluteDirectoryPath

	// Dependencies is the directory esbuild and Node resolve node_modules from. It
	// must contain (or have an ancestor containing) the peer dependencies
	// (solid-js, babel-preset-solid, @babel/core). Usually the frontend project
	// root.
	//
	// Optional: if empty, it defaults to ComponentsDir. Since consumers already
	// point ComponentsDir at their frontend tree, the peer deps installed there
	// (npm install solid-js babel-preset-solid @babel/core) resolve without any
	// extra configuration.
	Dependencies meta.AbsoluteDirectoryPath

	// Workspace is where go_solid writes its runtime state: the materialized
	// worker script and temporary bundle-entry files. Optional: defaults to
	// <ComponentsDir>/.go_solid. Override only if the components tree is
	// read-only at runtime (e.g. baked into an image); point it at a writable
	// path then. Safe to gitignore: it holds only regenerable artifacts.
	Workspace meta.AbsoluteDirectoryPath

	// PoolSize is the number of persistent Node workers. 0/1 => single worker.
	PoolSize int

	// NodeBin overrides the node executable ("" => "node" on PATH).
	NodeBin string

	// Dev disables caching and emits sourcemaps; Minify controls esbuild
	// minification (usually !Dev).
	Dev      bool
	Minify   bool
	Defaults *BehaviouralDefaults
}

type BehaviouralDefaults struct {
	HTMLHeadAttributes Configurator[HTMLHeadSegmentBuilder]
}

// Bundler is the top-level handle. Construct with New, close with Close.
type Bundler struct {
	cfg       Config
	registry  *Registry
	pool      *workers.Pool
	cache     *caching.MemCache
	disk      *caching.DiskCache
	workspace meta.AbsoluteDirectoryPath // resolved workspace (.go_solid) for worker + temp files
}

// New constructs a Bundler: scans components, starts the worker pool.
func New(cfg Config) (*Bundler, error) {
	if cfg.Components == "" {
		return nil, fmt.Errorf("go_solid: ComponentsDir is required")
	}
	// Dependencies defaults to the components directory: consumers already point that
	// at their frontend tree, so peer deps installed there resolve out of the box.
	if cfg.Dependencies == "" {
		cfg.Dependencies = cfg.Components
	}
	// Resolve and create the workspace: one visible, gitignorable folder inside
	// the registry directory the consumer already knows go_solid owns.
	workspace := cfg.Workspace
	if workspace == "" {
		workspace = filepath.Join(cfg.Components, ".go_solid")
	}
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		return nil, fmt.Errorf("go_solid: create workspace %q: %w", workspace, err)
	}
	scriptLocation, err := materializeWorkerScript(workspace)
	if err != nil {
		return nil, fmt.Errorf("go_solid: materialize worker script: %w", err)
	}

	if missing := peerDepsMissing(cfg.Dependencies, requiredPeerDeps); len(missing) > 0 {
		return nil, fmt.Errorf(
			"go_solid: missing Node peer dependencies %v in %q (or any ancestor).\n"+
				"Install them in your frontend project:\n"+
				"    npm install --save-dev %s",
			missing, cfg.Dependencies, strings.Join(missing, " "))
	}
	reg, err := NewRegistry(cfg.Components)
	if err != nil {
		return nil, err
	}
	pool, err := workers.NewPool(workers.PoolConfig{
		Size:         cfg.PoolSize,
		NodeBin:      cfg.NodeBin,
		Script:       scriptLocation,
		Dependencies: cfg.Dependencies,
	})
	if err != nil {
		return nil, err
	}

	if cfg.Defaults != nil && cfg.Defaults.HTMLHeadAttributes != nil {
		setHTMLHeadSegmentTemplate(cfg.Defaults.HTMLHeadAttributes)
	}

	// Disk cache lives in the workspace; enabled outside dev mode (dev bypasses
	// caching so on-disk edits always rebuild).
	disk, err := caching.NewDiskCache(workspace, !cfg.Dev)
	if err != nil {
		return nil, err
	}

	return &Bundler{
		cfg:       cfg,
		registry:  reg,
		pool:      pool,
		cache:     caching.NewMemCache(!cfg.Dev),
		disk:      disk,
		workspace: workspace,
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

// logDiskCacheError reports a non-fatal disk cache failure. Kept minimal; wire
// to a real logger if the consumer provides one.
func (b *Bundler) logDiskCacheError(err error) {
	fmt.Fprintf(os.Stderr, "[go_solid] disk cache write failed (non-fatal): %v\n", err)
}
