package go_solid

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
)

// Name of component with path from registry directory, however without extension.
type QualifiedName = string

func (this *Bundler) Prepare(component QualifiedName, props any) RenderCallBuilder {
	return newRenderCallBuilder(this, component, props)
}

type renderData struct {
	ctx          context.Context
	component    QualifiedName
	props        any
	rootID       string
	htmlHeadTags HTMLHeadSegmentBuilder
}

// render0 compiles the named component with the given props (marshaled to JSON
// and passed to the component) and returns the artifact set. In dev mode the
// registry is reloaded and the cache bypassed so on-disk edits take effect.
func render0(bundler *Bundler, data renderData) (*Rendered, error) {
	if err := data.ctx.Err(); err != nil {
		return nil, err // caller already cancelled / deadline exceeded
	}

	propsJSON := "{}"
	if data.props != nil {
		raw, err := json.Marshal(data.props)
		if err != nil {
			return nil, fmt.Errorf("go_solid#Render: marshal props: %w", err)
		}
		propsJSON = string(raw)
	}

	if bundler.cfg.Dev {
		if err := bundler.registry.Reload(); err != nil {
			return nil, err
		}
	}

	key := cacheKey(data.component, propsJSON, bundler.cfg.Minify)
	if cached, ok := bundler.cache.get(key); ok {
		return cached, nil
	}

	comp, ok := bundler.registry.Lookup(data.component)
	if !ok {
		return nil, fmt.Errorf("go_solid#Render: no component registered as %q (have: %s)",
			data.component, strings.Join(bundler.registry.Names().ToStringSlice(), ", "))
	}

	// 1. Generate the entry module that imports the component and mounts it with
	//    props read from a data island (keeps server-owned data server-owned).
	entrySource, err := generateEntry(comp, bundler.cfg.Dependencies)
	if err != nil {
		return nil, err
	}

	// 2. Write the entry to a temp dir. The esbuild plugin transforms every
	//    JSX/TSX file in the graph (entry + component + its imports) through the
	//    babel-preset-solid worker pool, so we do NOT pre-transform here.
	entryPath, cleanup, err := writeTempEntry(bundler.workspace, entrySource)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	// 3. Bundle with esbuild (Go): the solid plugin runs babel per-file, then
	//    esbuild typestrips, resolves imports, tree-shakes, collects CSS, minifies.
	bundle, err := bundleEntry(bundler.pool, "dom", entryPath, bundler.cfg.Dependencies, bundler.cfg.Minify, bundler.cfg.Dev)
	if err != nil {
		return nil, fmt.Errorf("go_solid#Render: bundle %q: %w", data.component, err)
	}

	// 4. Assemble artifacts with predictable, content-hashed asset names.
	safeName := strings.ReplaceAll(string(data.component), "/", "_")
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
	rendered.HTML = assembleHTML(data.htmlHeadTags, propsJSON, rendered)

	bundler.cache.put(key, rendered)
	return rendered, nil
}

// generateEntry produces the entry .tsx that imports the component by absolute
// path and mounts it. Props flow via the data island (window / #hots-bootstrap),
// keeping the server as the source of truth for data.
func generateEntry(comp Component, _ /*workDir*/ AbsoluteDirectoryPath) (string, error) {
	// Absolute import path (without extension) so the generated entry resolves
	// the component no matter which temp directory esbuild reads it from.
	importPath := filepath.ToSlash(strings.TrimSuffix(comp.AbsPath, comp.Ext))

	// The mount target and data island id are conventions the HTML shell provides.
	return fmt.Sprintf(
		`import { render } from "solid-js/web";
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
func assembleHTML(headSegment HTMLHeadSegmentBuilder, propsJSON string, rendered *Rendered) string {
	if rendered.CSSName != "" {
		headSegment.AddLink("stylesheet", fmt.Sprintf("/static/dist/%s", rendered.CSSName))
	}
	return fmt.Sprintf(
		`<!doctype html>
		<html>
		<head>
		%s
		</head>
		<body>
		<div id="solidbundle-root"></div>
		<script id="solidbundle-props" type="application/json">%s</script>
		<script type="module" src="/static/dist/%s"></script>
		</body>
		</html>
	`, headSegment.Build(), propsJSON, rendered.JSName)
}
