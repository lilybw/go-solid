package go_solid

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/lilybw/go-solid/internal"
	caching "github.com/lilybw/go-solid/internal/caching"
	"github.com/lilybw/go-solid/internal/esbuild"
	"github.com/lilybw/go-solid/internal/hmr"
	"github.com/lilybw/go-solid/internal/meta"
	networking "github.com/lilybw/go-solid/shared/networking"
)

// TODO: expand props to varparam, construct props js object from safe-made reflect.Type and let an interface be implemented to enable a props property key overwrite
func (this *Bundler) Prepare(component meta.QualifiedName, props any) RenderCallBuilder {
	return newRenderCallBuilder(this, component, props)
}

func (this *Bundler) Render(component meta.QualifiedName, configurator meta.Configurator[RenderCallBuilder], props any) (*caching.Rendered, error) {
	builder := this.Prepare(component, props)
	if configurator != nil {
		configurator(builder)
	}
	return builder.Render()
}

type renderData struct {
	ctx          context.Context
	component    meta.QualifiedName
	props        any
	root         networking.HTMLElementID
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

// TODO: Move to internal alongside renderData and ifRequest if possible
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

	key := caching.NewMemCacheKey(data.component, data.root)
	if cached, ok := bundler.mem.Get(key); ok {
		return cached, nil
	}

	if bundler.disk != nil {
		if cached, ok := bundler.disk.Get(key); ok {
			bundler.mem.Put(key, cached) // promote to memory
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
	if data.root == "" {
		data.root = comp.MountRootID
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
	bundle, err := esbuild.BundleEntry(data.ctx, bundler.pool, "dom", entryPath, bundler.cfg.Dependencies, bundler.cfg.Generation)
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
		hmrScript = hmr.ClientScript(bundler.cfg.HMR.Path, data.component)
	}
	rendered.HTML = internal.AssembleHTML(data.htmlHeadTags, propsJSON, rendered, data.root, hmrScript)

	bundler.mem.Put(key, rendered)
	if bundler.disk != nil {
		if err := bundler.disk.Put(key, data.root, bundler.cfg.Generation.Minify, rendered, bundle.Sources); err != nil {
			bundler.logDiskCacheError(err)
		}
	}

	data.ifRequest(func(req *networking.RequestBehaviour) error { return req.TransmitRenderedTemplate(rendered) })

	return rendered, nil
}
