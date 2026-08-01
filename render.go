package go_solid

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/lilybw/go_solid/internal"
	caching "github.com/lilybw/go_solid/internal/caching"
	"github.com/lilybw/go_solid/internal/esbuild"
	"github.com/lilybw/go_solid/internal/hmr"
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
	htmlHeadTags networking.HTMLHeadSegmentBuilder
	request      *networking.RequestBehaviour
}

func (this *renderData) ifRequest(fn func(r *networking.RequestBehaviour) error) {
	if this.request != nil {
		err := fn(this.request)
		if err != nil {
			panic(fmt.Sprintf("go_solid: request-bound failure handler returned an error: %v", err))
		}
	}
}

func render0(bundler *Bundler, data renderData) (*caching.Rendered, error) {
	if err := data.ctx.Err(); err != nil {
		return nil, err // caller already cancelled / deadline exceeded
	}

	propsJSON := "{}"
	if data.props != nil {
		raw, err := json.Marshal(data.props)
		if err != nil {
			data.ifRequest(func(req *networking.RequestBehaviour) error { return req.UponPropsMarshalingError(err) })
			return nil, fmt.Errorf("go_solid#Render: marshal props: %w", err)
		}
		propsJSON = string(raw)
	}

	// Registry hot reload is now its own flag, independent of caching.
	if bundler.cfg.HotReloadRegistry {
		if err := bundler.registry.Reload(); err != nil {
			data.ifRequest(func(req *networking.RequestBehaviour) error { return req.UponRegistryReloadError(err) })
			return nil, err
		}
	}

	key := caching.MemCacheKey(data.component, propsJSON, bundler.cfg.Minify)
	if cached, ok := bundler.cache.Get(key); ok {
		return cached, nil
	}

	if bundler.disk != nil {
		if cached, ok := bundler.disk.Get(key); ok {
			bundler.cache.Put(key, cached) // promote to memory
			return cached, nil
		}
	}

	comp, ok := bundler.registry.Lookup(data.component)
	if !ok {
		data.ifRequest(func(req *networking.RequestBehaviour) error {
			return req.UponRegistryLookupFailure(fmt.Errorf("component %q not found in registry", data.component))
		})
		return nil, fmt.Errorf("go_solid#Render: no component registered as %q (have: %s)",
			data.component, strings.Join(bundler.registry.Names(), ", "))
	}
	if data.rootID == "" {
		data.rootID = comp.MountRootID
	}

	entrySource, err := internal.GenerateEntry(comp)
	if err != nil {
		data.ifRequest(func(req *networking.RequestBehaviour) error { return req.UponEntryGenerationError(err) })
		return nil, err
	}

	entryPath, cleanup, err := esbuild.WriteTempEntry(bundler.workspace, entrySource)
	if err != nil {
		data.ifRequest(func(req *networking.RequestBehaviour) error { return req.UponTempEntryWriteError(err) })
		return nil, err
	}
	defer cleanup()

	// Sourcemaps is now its own flag (last BundleEntry arg), independent of caching.
	bundle, err := esbuild.BundleEntry(data.ctx, bundler.pool, "dom", entryPath, bundler.cfg.Dependencies, bundler.cfg.Minify, bundler.cfg.Sourcemaps)
	if err != nil {
		data.ifRequest(func(req *networking.RequestBehaviour) error { return req.UponCompBundlingError(err) })
		return nil, fmt.Errorf("go_solid#Render: bundle %q: %w", data.component, err)
	}

	// Maintain the dependency graph on every render regardless of cache settings;
	// the watcher inverts it to decide which tabs to reload.
	bundler.index.Record(data.component, bundle.Sources)

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

	// Inject the hot-reload client only when HMR is active. Generated here in
	// package go_solid (which imports both hmr and internal) and passed to
	// AssembleHTML as a plain string, so internal never imports hmr — avoiding
	// the import cycle (hmr already imports internal).
	hmrScript := ""
	if bundler.hub != nil {
		hmrScript = hmr.ClientScript(bundler.cfg.HMR.HMRPath, data.component)
	}
	rendered.HTML = internal.AssembleHTML(data.htmlHeadTags, propsJSON, rendered, data.rootID, hmrScript)

	bundler.cache.Put(key, rendered)
	if bundler.disk != nil {
		if err := bundler.disk.Put(key, data.component, data.rootID, bundler.cfg.Minify, rendered, bundle.Sources); err != nil {
			bundler.logDiskCacheError(err)
		}
	}

	data.ifRequest(func(req *networking.RequestBehaviour) error { return req.TransmitRenderedTemplate(rendered) })

	return rendered, nil
}
