package go_solid

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	caching "github.com/lilybw/go_solid/internal/caching"
	"github.com/lilybw/go_solid/internal/esbuild"
	"github.com/lilybw/go_solid/internal/meta"
	networking "github.com/lilybw/go_solid/internal/networking"
)

func (this *Bundler) Prepare(component meta.QualifiedName, props any) RenderCallBuilder {
	return newRenderCallBuilder(this, component, props)
}

type renderData struct {
	ctx          context.Context
	component    meta.QualifiedName
	props        any
	rootID       string
	htmlHeadTags HTMLHeadSegmentBuilder
	// Nil if not provided
	request *networking.RequestData
}

func (this *renderData) ifRequest(fn func(r *networking.RequestData) error) {
	if this.request != nil {
		err := fn(this.request)
		if err != nil {
			panic(fmt.Sprintf("go_solid: request-bound failure handler returned an error: %v", err))
		}
	}
}

// render0 compiles the named component with the given props (marshaled to JSON
// and passed to the component) and returns the artifact set. In dev mode the
// registry is reloaded and the cache bypassed so on-disk edits take effect.
func render0(bundler *Bundler, data renderData) (*caching.Rendered, error) {
	if err := data.ctx.Err(); err != nil {
		// request ctx always take precedence over other provided ctxs, so no point in writing an http error here even if a request was provided.
		// cause if its already cancelled, the http request is already gone and writing to it will fail anyway.
		return nil, err // caller already cancelled / deadline exceeded
	}

	propsJSON := "{}"
	if data.props != nil {
		raw, err := json.Marshal(data.props)
		if err != nil {
			data.ifRequest(func(req *networking.RequestData) error { return req.UponPropsMarshalingError(err) })
			return nil, fmt.Errorf("go_solid#Render: marshal props: %w", err)
		}
		propsJSON = string(raw)
	}

	if bundler.cfg.Dev {
		if err := bundler.registry.Reload(); err != nil {
			data.ifRequest(func(req *networking.RequestData) error { return req.UponRegistryReloadError(err) })
			return nil, err
		}
	}

	key := caching.MemCacheKey(data.component, propsJSON, bundler.cfg.Minify)
	if cached, ok := bundler.cache.Get(key); ok {
		return cached, nil
	}

	// Second tier: disk cache (survives restarts; validated by source hashes).
	if bundler.disk != nil {
		if cached, ok := bundler.disk.Get(key); ok {
			bundler.cache.Put(key, cached) // promote to memory
			return cached, nil
		}
	}

	comp, ok := bundler.registry.Lookup(data.component)
	if !ok {
		data.ifRequest(func(req *networking.RequestData) error {
			return req.UponRegistryLookupFailure(fmt.Errorf("component %q not found in registry", data.component))
		})
		return nil, fmt.Errorf("go_solid#Render: no component registered as %q (have: %s)",
			data.component, strings.Join(bundler.registry.Names(), ", "))
	}
	if data.rootID == "" {
		data.rootID = comp.MountRootID
	}

	// 1. Generate the entry module that imports the component and mounts it with
	//    props read from a data island (keeps server-owned data server-owned).
	entrySource, err := generateEntry(comp)
	if err != nil {
		data.ifRequest(func(req *networking.RequestData) error { return req.UponEntryGenerationError(err) })
		return nil, err
	}

	// 2. Write the entry to a temp dir. The esbuild plugin transforms every
	//    JSX/TSX file in the graph (entry + component + its imports) through the
	//    babel-preset-solid worker pool, so we do NOT pre-transform here.
	entryPath, cleanup, err := esbuild.WriteTempEntry(bundler.workspace, entrySource)
	if err != nil {
		data.ifRequest(func(req *networking.RequestData) error { return req.UponTempEntryWriteError(err) })
		return nil, err
	}
	defer cleanup()

	// 3. Bundle with esbuild (Go): the solid plugin runs babel per-file, then
	//    esbuild typestrips, resolves imports, tree-shakes, collects CSS, minifies.
	bundle, err := esbuild.BundleEntry(data.ctx, bundler.pool, "dom", entryPath, bundler.cfg.Dependencies, bundler.cfg.Minify, bundler.cfg.Dev)
	if err != nil {
		data.ifRequest(func(req *networking.RequestData) error { return req.UponCompBundlingError(err) })
		return nil, fmt.Errorf("go_solid#Render: bundle %q: %w", data.component, err)
	}

	// 4. Assemble artifacts with predictable, content-hashed asset names.
	safeName := strings.ReplaceAll(data.component, "/", "_")
	jsHash := caching.ShortHash(string(bundle.JS), 8)
	rendered := &caching.Rendered{
		JS:     string(bundle.JS),
		CSS:    string(bundle.CSS),
		JSName: fmt.Sprintf("%s.%s.js", safeName, jsHash),
	}
	if len(bundle.CSS) > 0 {
		cssHash := caching.ShortHash(string(bundle.CSS), 8)
		rendered.CSSName = fmt.Sprintf("%s.%s.css", safeName, cssHash)
	}
	rendered.HTML = assembleHTML(data.htmlHeadTags, propsJSON, rendered, data.rootID)

	bundler.cache.Put(key, rendered)
	if bundler.disk != nil {
		// Persist to disk with the source list from the metafile, for
		// cross-restart caching and hash-based invalidation. A disk write
		// failure is non-fatal: the in-memory result is still returned.
		if err := bundler.disk.Put(key, data.component, data.rootID, bundler.cfg.Minify, rendered, bundle.Sources); err != nil {
			bundler.logDiskCacheError(err)
		}
	}
	// TODO: Send the rendered result to the request bound writer

	return rendered, nil
}

// generateEntry produces the entry .tsx that imports the component by absolute
// path and mounts it. Props flow via the data island (window / #hots-bootstrap),
// keeping the server as the source of truth for data.
func generateEntry(comp Component) (string, error) {
	// Absolute import path (without extension) so the generated entry resolves
	// the component no matter which temp directory esbuild reads it from.
	importPath := filepath.ToSlash(strings.TrimSuffix(comp.AbsPath, comp.Ext))

	// The mount target and data island id are conventions the HTML shell provides.
	return fmt.Sprintf(
		`import { render } from "solid-js/web";
		import Component from %q;

		const compName = %q;
		const propsMountId = %q;
		const compMountId = %q;

		function readProps() {
			const el = document.getElementById(propsMountId);
			if (!el || !el.textContent) return {};
			try { return JSON.parse(el.textContent); } catch (error) { 
				console.error("go_solid: Component " + comp.Name + " could not read props from data island id " + propsMountId + ": invalid JSON: " + error);
				return {}; 
			}
		}

		const root = document.getElementById(compMountId);
		if (root) {
			render(() => Component(readProps()), root);
		} else {
			console.error("go_solid: Component " + comp.Name + " could not mount: no element with id " + comp.MountRootID + " found in the HTML shell.");
		}
		`, importPath, comp.Name, derivePropsMountIdFromCompRootID(comp.MountRootID), comp.MountRootID), nil
}

// assembleHTML builds the index.html returned to the client. It embeds props as
// a JSON data island and references the emitted JS (module) and optional CSS.
// Asset URLs assume they are served from the static prefix the caller wires up.
func assembleHTML(headSegment HTMLHeadSegmentBuilder, propsJSON string, rendered *caching.Rendered, mountRootID string) string {
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
		<div id="%s"></div>
		<script id="%s" type="application/json">%s</script>
		<script type="module" src="/static/dist/%s"></script>
		</body>
		</html>
	`, headSegment.Build(), mountRootID, derivePropsMountIdFromCompRootID(mountRootID), propsJSON, rendered.JSName)
}

func derivePropsMountIdFromCompRootID(rootID string) string {
	return fmt.Sprintf("props-%s", rootID)
}
