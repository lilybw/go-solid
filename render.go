package go_solid

import (
	"context"
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"fmt"
	"strings"

	caching "github.com/lilybw/go-solid/internal/caching"
	code_gen "github.com/lilybw/go-solid/internal/code-gen"
	"github.com/lilybw/go-solid/internal/esbuild"
	"github.com/lilybw/go-solid/internal/fn"
	"github.com/lilybw/go-solid/internal/hashing"
	log_int "github.com/lilybw/go-solid/internal/logging"
	"github.com/lilybw/go-solid/shared/logging"
	"github.com/lilybw/go-solid/shared/meta"
	networking "github.com/lilybw/go-solid/shared/networking"
	"github.com/lilybw/go-solid/shared/networking/events"
	"github.com/lilybw/go-solid/shared/registry"
)

// TODO: expand props to varparam, construct props js object from safe-made reflect.Type and let an interface be implemented to enable a props property key overwrite
func (this *Bundler) Prepare(component meta.QualifiedName, props any) RenderCallBuilder {
	typeError := this.checkTypes(component, props)
	return newRenderCallBuilder(this, component, props, typeError)
}

func (this *Bundler) checkTypes(component meta.QualifiedName, props any) error {
	if this == nil || this.types == nil || props == nil {
		return nil
	}
	comp, ok := this.registry.Lookup(component)
	if !ok {
		return nil
	}
	return this.types.OnPrepare(comp, props)
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
	typeError    error // if non-nil, the props were not assignable to the component's declared types
}

func (this *renderData) ifRequest(fn func(r *networking.RequestBehaviour) error) error {
	if this.request == nil {
		return nil
	}
	return fn(this.request)
}

func (this *renderData) emitEvent[T events.NetworkingEvent](err error, event T) error {
	if this.request == nil {
		return err
	}
	dispatchErr := this.request.Dispatch[T](event)
	if dispatchErr == nil {
		return err
	}
	log_int.Log(logging.LEVEL_ERROR, "Error causing error during event dispatch: "+err.Error())
	return dispatchErr
}

// TODO: Move to internal alongside renderData and ifRequest if possible
// compiled is the props-independent, cacheable half: JS/CSS + asset names.
// It is what lives in the caches. It is never mutated after construction.
// (This is just caching.Rendered with HTML left empty; see note below.)

func render0(bundler *Bundler, data *renderData) (*caching.Rendered, error) {
	if err := data.ctx.Err(); err != nil {
		return nil, err // caller already cancelled / deadline exceeded
	}

	if data.typeError != nil { // type check error can not be resolved before now without cluttering the render call builder api
		return nil, data.emitEvent(data.typeError, events.NewCompPropsInsufficientFailure(data.typeError))
	}

	propsJSON, err := marshalProps(data)
	if err != nil {
		return nil, data.emitEvent(err, events.NewPropsMarshalingFailure(err))
	}

	artifact, err := bundler.compiledArtifact(data)
	if err != nil {
		return nil, err
	}

	// Assemble a request-local response. Never mutate the cached artifact:
	// HTML carries props/root/HMR, which vary per call.
	resp := assembleResponse(bundler, data, artifact, propsJSON)
	if err := data.ifRequest(func(req *networking.RequestBehaviour) error {
		return req.Dispatch(events.NewTransmitRenderedTemplate(resp))
	}); err != nil {
		return nil, fmt.Errorf("go_solid#Render: transmit %q: %w", data.component, err)
	}
	return resp, nil
}

func marshalProps(data *renderData) (string, error) {
	if data.props == nil {
		return "{}", nil
	}

	raw, err := json.Marshal(data.props,
		json.Deterministic(true),
		jsontext.EscapeForHTML(true),
		jsontext.EscapeForJS(true),
	)
	if err != nil {
		return "", fmt.Errorf("go_solid#Render: marshal props: %w", err)
	}
	return string(raw), nil
}

// compiledArtifact returns the props-independent JS/CSS artifact for this
// component, from cache if present, otherwise by bundling and caching it.
// The returned *Rendered is shared and must be treated as read-only.
func (bundler *Bundler) compiledArtifact(data *renderData) (*caching.Rendered, error) {

	comp, ok := bundler.registry.Lookup(data.component)
	if !ok {
		err := fmt.Errorf("go_solid#Render: no component registered as %q (have: %s)",
			data.component, strings.Join(
				// now just imagine the lambda equivalent: .Map((k, v) -> k), but nooooo. How does Java, THE boilerplate language, do this more concisely?
				bundler.registry.Map(fn.First[meta.QualifiedName, *registry.Component]()),
				", "))
		return nil, data.emitEvent(err, events.NewRegistryLookupFailure(err))
	}
	if data.root == "" {
		data.root = comp.MountRootID
	}

	key := caching.NewBuildCacheKey(data.component, data.root, bundler.buildID)
	if cached, ok := bundler.searchCaches(key); ok {
		return cached, nil
	}

	if err := bundler.types.VerifyComponentExport(comp); err != nil {
		return nil, data.emitEvent(err, events.NewRegistryLookupFailure(err))
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
	data *renderData, comp *registry.Component,
) (*caching.Rendered, []meta.AbsoluteFilePath, error) {

	if bundler.cfg.Generation.Disabled {
		err := fmt.Errorf("go_solid#Render: bundling is disabled and %q is not cached", data.component)
		return nil, nil, data.emitEvent(err, events.NewCompBundlingFailure(err))
	}

	entrySource, err := code_gen.GenerateEntry(comp)
	if err != nil {
		return nil, nil, data.emitEvent(err, events.NewEntryGenerationFailure(err))
	}

	entryPath, entryDir, cleanup, err := esbuild.WriteTempEntry(bundler.cfg.Workspace, entrySource)
	if err != nil {
		return nil, nil, data.emitEvent(err, events.NewTempEntryWriteFailure(err))
	}
	defer cleanup()

	bundle, err := esbuild.BundleEntry(
		entryPath, bundler.cfg.Workspace, entryDir, bundler.cfg.Generation)
	if err != nil {
		errExpanded := fmt.Errorf("go_solid#Render: bundle %q: %w", data.component, err)
		return nil, nil, data.emitEvent(errExpanded, events.NewCompBundlingFailure(errExpanded))
	}

	// Keep the dependency graph current regardless of cache settings; the
	// watcher inverts it to decide which tabs to reload.
	bundler.index.Record(data.component, bundle.Sources)

	safeName := strings.ReplaceAll(data.component, "/", "_")
	artifact := &caching.Rendered{
		JS:     string(bundle.JS),
		CSS:    string(bundle.CSS),
		JSName: fmt.Sprintf("%s.%s.js", safeName, hashing.Short(string(bundle.JS), 8)),
	}
	if len(bundle.CSS) > 0 {
		artifact.CSSName = fmt.Sprintf("%s.%s.css", safeName, hashing.Short(string(bundle.CSS), 8))
	}
	return artifact, bundle.Sources, nil
}

func assembleResponse(
	bundler *Bundler, data *renderData,
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
