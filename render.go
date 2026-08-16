package go_solid

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	caching "github.com/lilybw/go-solid/internal/caching"
	code_gen "github.com/lilybw/go-solid/internal/code-gen"
	"github.com/lilybw/go-solid/internal/esbuild"
	"github.com/lilybw/go-solid/internal/meta"
	networking "github.com/lilybw/go-solid/shared/networking"
	"github.com/lilybw/go-solid/shared/networking/events"
	"github.com/lilybw/go-solid/shared/registry"
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

func (this *renderData) ifRequest(fn func(r *networking.RequestBehaviour) error) error {
	if this.request == nil {
		return nil
	}
	return fn(this.request)
}

// TODO: Move to internal alongside renderData and ifRequest if possible
// compiled is the props-independent, cacheable half: JS/CSS + asset names.
// It is what lives in the caches. It is never mutated after construction.
// (This is just caching.Rendered with HTML left empty; see note below.)

func render0(bundler *Bundler, data renderData) (*caching.Rendered, error) {
	if err := data.ctx.Err(); err != nil {
		return nil, err // caller already cancelled / deadline exceeded
	}

	propsJSON, err := marshalProps(data)
	if err != nil {
		_ = data.ifRequest(func(req *networking.RequestBehaviour) error {
			return networking.ExecHandlers(req, events.NewPropsMarshalingFailure(err))
		})
		return nil, err
	}

	artifact, err := bundler.compiledArtifact(data)
	if err != nil {
		return nil, err
	}

	// Assemble a request-local response. Never mutate the cached artifact:
	// HTML carries props/root/HMR, which vary per call.
	resp := assembleResponse(bundler, data, artifact, propsJSON)
	if err := data.ifRequest(func(req *networking.RequestBehaviour) error {
		return networking.ExecHandlers(req, events.NewTransmitRenderedTemplate(resp))
	}); err != nil {
		return nil, fmt.Errorf("go_solid#Render: transmit %q: %w", data.component, err)
	}
	return resp, nil
}

// marshalProps isolates the props->JSON step and its request error hook.
func marshalProps(data renderData) (string, error) {
	if data.props == nil {
		return "{}", nil
	}
	raw, err := json.Marshal(data.props)
	if err != nil {
		return "", fmt.Errorf("go_solid#Render: marshal props: %w", err)
	}
	return string(raw), nil
}

// compiledArtifact returns the props-independent JS/CSS artifact for this
// component, from cache if present, otherwise by bundling and caching it.
// The returned *Rendered is shared and must be treated as read-only.
func (bundler *Bundler) compiledArtifact(data renderData) (*caching.Rendered, error) {
	key := caching.NewMemCacheKey(data.component, data.root)
	if cached, ok := bundler.searchCaches(key); ok {
		return cached, nil
	}

	comp, ok := bundler.registry.Lookup(data.component)
	if !ok {
		_ = data.ifRequest(func(req *networking.RequestBehaviour) error {
			return networking.ExecHandlers(req, events.NewRegistryLookupFailure(
				fmt.Errorf("component %q not found in registry", data.component)))
		})
		return nil, fmt.Errorf("go_solid#Render: no component registered as %q (have: %s)",
			data.component, strings.Join(bundler.registry.Names(), ", "))
	}
	if data.root == "" {
		data.root = comp.MountRootID
	}

	artifact, sources, err := bundler.bundleComponent(data, comp)
	if err != nil {
		return nil, err
	}

	bundler.mem.Put(key, artifact)
	if bundler.disk != nil {
		if err := bundler.disk.Put(key, bundler.cfg.Generation.Minify, artifact, sources); err != nil {
			bundler.logDiskCacheError(err)
		}
	}
	return artifact, nil
}

// bundleComponent runs the esbuild pipeline and packages the JS/CSS artifact.
// It does not assemble HTML and does not touch the caches.
func (bundler *Bundler) bundleComponent(
	data renderData, comp *registry.Component, // adjust to your real type
) (*caching.Rendered, []meta.AbsoluteFilePath, error) {

	entrySource, err := code_gen.GenerateEntry(comp)
	if err != nil {
		_ = data.ifRequest(func(req *networking.RequestBehaviour) error {
			return networking.ExecHandlers(req, events.NewEntryGenerationFailure(err))
		})
		return nil, nil, err
	}

	entryPath, cleanup, err := esbuild.WriteTempEntry(bundler.cfg.Workspace, entrySource)
	if err != nil {
		_ = data.ifRequest(func(req *networking.RequestBehaviour) error {
			return networking.ExecHandlers(req, events.NewTempEntryWriteFailure(err))
		})
		return nil, nil, err
	}
	defer cleanup()

	bundle, err := esbuild.BundleEntry(
		data.ctx, bundler.pool, "dom", entryPath,
		bundler.cfg.Generation.Dependencies, bundler.cfg.Generation)
	if err != nil {
		_ = data.ifRequest(func(req *networking.RequestBehaviour) error {
			return networking.ExecHandlers(req, events.NewCompBundlingFailure(err))
		})
		return nil, nil, fmt.Errorf("go_solid#Render: bundle %q: %w", data.component, err)
	}

	// Keep the dependency graph current regardless of cache settings; the
	// watcher inverts it to decide which tabs to reload.
	bundler.index.Record(data.component, bundle.Sources)

	safeName := strings.ReplaceAll(data.component, "/", "_")
	artifact := &caching.Rendered{
		JS:     string(bundle.JS),
		CSS:    string(bundle.CSS),
		JSName: fmt.Sprintf("%s.%s.js", safeName, caching.ShortHash(string(bundle.JS), 8)),
	}
	if len(bundle.CSS) > 0 {
		artifact.CSSName = fmt.Sprintf("%s.%s.css", safeName, caching.ShortHash(string(bundle.CSS), 8))
	}
	return artifact, bundle.Sources, nil
}

// assembleResponse builds the request-local Rendered: a shallow copy of the
// cached artifact with freshly assembled HTML. The cached artifact is never
// mutated, so concurrent renders of the same component don't clobber each other.
func assembleResponse(
	bundler *Bundler, data renderData,
	artifact *caching.Rendered, propsJSON string,
) *caching.Rendered {
	resp := *artifact // copy JS/CSS/JSName/CSSName by value
	resp.HTML = code_gen.AssembleHTML(
		data.htmlHeadTags,
		propsJSON,
		&resp,
		data.root,
		bundler.constructHMRScript(data.component),
	)
	return &resp
}
