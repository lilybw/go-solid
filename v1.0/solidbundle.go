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
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
)

// Config configures a Bundler.
type Config struct {
	// ComponentsDir is the root folder scanned for components. A file at
	// ComponentsDir/auth/LoginForm.tsx registers as template "auth/LoginForm".
	ComponentsDir string

	// WorkDir is the directory esbuild and Node resolve node_modules from. It
	// must contain (or have an ancestor containing) solid-js and
	// babel-preset-solid. Usually the frontend project root.
	WorkDir string

	// WorkerScript is the absolute path to transform-worker.mjs.
	WorkerScript string

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
	if cfg.ComponentsDir == "" || cfg.WorkDir == "" || cfg.WorkerScript == "" {
		return nil, fmt.Errorf("solidbundle: ComponentsDir, WorkDir, WorkerScript are required")
	}
	reg, err := NewRegistry(cfg.ComponentsDir)
	if err != nil {
		return nil, err
	}
	pool, err := newPool(PoolConfig{
		Size:       cfg.PoolSize,
		NodeBin:    cfg.NodeBin,
		ScriptPath: cfg.WorkerScript,
		WorkDir:    cfg.WorkDir,
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

// Render compiles the named component with the given props (marshaled to JSON
// and passed to the component) and returns the artifact set. In dev mode the
// registry is reloaded and the cache bypassed so on-disk edits take effect.
func (b *Bundler) Render(ctx context.Context, name string, props any) (*Rendered, error) {
	propsJSON := "{}"
	if props != nil {
		raw, err := json.Marshal(props)
		if err != nil {
			return nil, fmt.Errorf("solidbundle: marshal props: %w", err)
		}
		propsJSON = string(raw)
	}

	if b.cfg.Dev {
		if err := b.registry.Reload(); err != nil {
			return nil, err
		}
	}

	key := cacheKey(name, propsJSON, b.cfg.Minify)
	if cached, ok := b.cache.get(key); ok {
		return cached, nil
	}

	comp, ok := b.registry.Lookup(name)
	if !ok {
		return nil, fmt.Errorf("solidbundle: no component registered as %q (have: %s)",
			name, strings.Join(b.registry.Names(), ", "))
	}

	// 1. Generate the entry module that imports the component and mounts it with
	//    props read from a data island (keeps server-owned data server-owned).
	entrySource, err := generateEntry(comp, b.cfg.WorkDir)
	if err != nil {
		return nil, err
	}

	// 2. Write the entry to a temp dir. The esbuild plugin transforms every
	//    JSX/TSX file in the graph (entry + component + its imports) through the
	//    babel-preset-solid worker pool, so we do NOT pre-transform here.
	entryPath, cleanup, err := writeTempEntry(b.cfg.WorkDir, entrySource)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	// 3. Bundle with esbuild (Go): the solid plugin runs babel per-file, then
	//    esbuild typestrips, resolves imports, tree-shakes, collects CSS, minifies.
	bundle, err := bundleEntry(b.pool, "dom", entryPath, b.cfg.WorkDir, b.cfg.Minify, b.cfg.Dev)
	if err != nil {
		return nil, fmt.Errorf("solidbundle: bundle %q: %w", name, err)
	}

	// 4. Assemble artifacts with predictable, content-hashed asset names.
	safeName := strings.ReplaceAll(name, "/", "_")
	jsHash := shortHash(string(bundle.JS), 8)
	rendered := &Rendered{
		JS:     string(bundle.JS),
		CSS:    string(bundle.CSS),
		JSName: fmt.Sprintf("%s.%s.js", safeName, jsHash),
	}
	if len(bundle.CSS) > 0 {
		cssHash := shortHash(string(bundle.CSS), 8)
		rendered.CSSName = fmt.Sprintf("%s.%s.css", safeName, cssHash)
	}
	rendered.HTML = assembleHTML(name, propsJSON, rendered.JSName, rendered.CSSName)

	b.cache.put(key, rendered)
	return rendered, nil
}

// generateEntry produces the entry .tsx that imports the component by absolute
// path and mounts it. Props flow via the data island (window / #hots-bootstrap),
// keeping the server as the source of truth for data.
func generateEntry(comp Component, workDir string) (string, error) {
	// Absolute import path (without extension) so the generated entry resolves
	// the component no matter which temp directory esbuild reads it from.
	importPath := filepath.ToSlash(strings.TrimSuffix(comp.AbsPath, comp.Ext))

	// The mount target and data island id are conventions the HTML shell provides.
	return fmt.Sprintf(`import { render } from "solid-js/web";
import Component from %q;

function readProps() {
  const el = document.getElementById("solidbundle-props");
  if (!el || !el.textContent) return {};
  try { return JSON.parse(el.textContent); } catch { return {}; }
}

const root = document.getElementById("solidbundle-root");
if (root) {
  render(() => Component(readProps()), root);
}
`, importPath), nil
}

// assembleHTML builds the index.html returned to the client. It embeds props as
// a JSON data island and references the emitted JS (module) and optional CSS.
// Asset URLs assume they are served from the static prefix the caller wires up.
func assembleHTML(name, propsJSON, jsName, cssName string) string {
	var css string
	if cssName != "" {
		css = fmt.Sprintf(`  <link rel="stylesheet" href="/static/dist/%s">`+"\n", cssName)
	}
	return fmt.Sprintf(`<!doctype html>
<html>
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>%s</title>
%s</head>
<body>
  <div id="solidbundle-root"></div>
  <script id="solidbundle-props" type="application/json">%s</script>
  <script type="module" src="/static/dist/%s"></script>
</body>
</html>
`, name, css, propsJSON, jsName)
}
