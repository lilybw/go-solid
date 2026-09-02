package go_solid

import (
	"context"
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"fmt"
	"reflect"
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
	return newRenderCallBuilder(this, component, props, renderFaults{types: this.checkTypes(component, props)})
}

func (this *Bundler) checkTypes(component meta.QualifiedName, props any) error {
	if this == nil {
		return nil
	}
	comp, ok := this.registry.Lookup(component)
	if !ok {
		return nil
	}
	// Renderability is checked whether or not props were supplied: a component
	// that cannot be server-rendered is worth reporting before the request is
	// served, not after.
	if err := this.ssr.OnPrepare(comp); err != nil {
		return err
	}
	if this.types == nil || props == nil {
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

// renderFaults are the faults found before a render begins. Each is held until
// Render, where it fails the call and dispatches the event that describes it.
type renderFaults struct {
	types  error // the props were not assignable to the component's declared type
	source error // an inline component could not be turned into a module
}

type renderData struct {
	ctx          context.Context
	component    meta.QualifiedName
	props        any
	root         networking.HTMLElementID
	htmlHeadTags networking.HTMLHeadSegmentBuilder
	request      *networking.RequestBehaviour
	faults       renderFaults
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
	dispatchErr := this.request.Dispatch(event)
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

	// Neither fault can be reported before now without cluttering the render
	// call builder api.
	if err := data.faults.source; err != nil {
		return nil, data.emitEvent(err, events.NewAnonymousSourceFailure(err))
	}
	if err := data.faults.types; err != nil {
		return nil, data.emitEvent(err, events.NewCompPropsInsufficientFailure(err))
	}

	propsJSON, err := marshalProps(&data.props)
	if err != nil {
		return nil, data.emitEvent(err, events.NewPropsMarshalingFailure(err))
	}

	artifact, err := bundler.compiledArtifact(data, propsJSON)
	if err != nil {
		return nil, fmt.Errorf("[go-solid]: Error during artifact compilation: %s", err.Error())
	}

	if err := data.execMiddleware(artifact); err != nil {
		return nil, fmt.Errorf("[go-solid]: Error during execution of middleware: %s", err.Error())
	}

	// Assemble a request-local response. Never mutate the cached artifact:
	// HTML carries props/root/HMR, which vary per call.
	resp, err := assembleResponse(bundler, data, artifact, "" /*SSR DISABLED*/)
	if err != nil {
		return nil, fmt.Errorf("[go-solid]: Error while assembling response: %s", err.Error())
	}

	if err := data.ifRequest(func(req *networking.RequestBehaviour) error {
		return req.Dispatch(events.NewTransmitRenderedTemplate(resp))
	}); err != nil {
		return nil, fmt.Errorf("go_solid#Render: transmit %q: %w", data.component, err)
	}
	return resp, nil
}

type limitedAccessView struct {
	artifact *caching.Rendered
}

func (this *limitedAccessView) PutDataIsland(key meta.HTMLElementID, data meta.JSONString) networking.LimitedAccessView {
	this.artifact.DataIslands[key] = data
	return this
}

func (this *renderData) execMiddleware(artifact *caching.Rendered) error {
	return this.ifRequest(func(req *networking.RequestBehaviour) error {
		limAcc := &limitedAccessView{artifact: artifact}
		for _, fn := range req.Middleware {
			if err := fn(limAcc, req); err != nil {
				return err
			}
		}
		return nil
	})
}

func marshalProps[T any](props *T) (string, error) {
	if props == nil {
		return "{}", nil
	}

	v := reflect.ValueOf(*props)
	for v.IsValid() && (v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface) {
		if v.IsNil() {
			return "{}", nil
		}
		v = v.Elem()
	}
	if !v.IsValid() { // *props held an untyped nil
		return "{}", nil
	}

	raw, err := json.Marshal(v.Interface(),
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
func (bundler *Bundler) compiledArtifact(data *renderData, propsJSON meta.JSONString) (*caching.Rendered, error) {

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

	dataIslands := map[meta.HTMLElementID]meta.JSONString{code_gen.DerivePropsMountIdFromCompRootID(data.root): propsJSON}

	key := caching.NewBuildCacheKey(data.component, data.root, bundler.buildID)
	if cached, ok := bundler.searchCaches(key); ok {
		cached.DataIslands = dataIslands
		return cached, nil
	}

	if err := bundler.types.VerifyComponentExport(comp); err != nil {
		return nil, data.emitEvent(err, events.NewRegistryLookupFailure(err))
	}

	artifact, sources, err := bundler.bundleComponent(data, comp)
	if err != nil {
		return nil, err
	}
	artifact.DataIslands = dataIslands

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

	source, err := bundler.sources.Read(comp.Path)
	if err != nil {
		return nil, nil, data.emitEvent(err, events.NewEntryGenerationFailure(err))
	}

	entrySource, err := code_gen.GenerateEntry(comp, source)
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

	safeName := caching.SafeStem(data.component)
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
	artifact *caching.Rendered, ssrHTML string,
) (*caching.Rendered, error) {
	resp := *artifact // copy JS/CSS/JSName/CSSName by value
	var err error
	resp.HTML, err = code_gen.AssembleHTML(
		data.htmlHeadTags,
		&resp,
		data.root,
		bundler.constructHMRScript(data.component),
		ssrHTML,
	)
	return &resp, err
}

// serverMarkup renders the component's first paint, or returns empty when
// server rendering is off or the component needs the client.
//
// A component that cannot be rendered is not an error here: Strict already
// turned it into one at Prepare, and without Strict falling back to an empty
// mount point is the intended behaviour.
func (bundler *Bundler) serverMarkup(data *renderData, propsJSON string) string {
	if !bundler.ssr.Active() {
		return ""
	}
	comp, ok := bundler.registry.Lookup(data.component)
	if !ok {
		return ""
	}
	props, err := propsMap(propsJSON)
	if err == nil {
		var markup string
		if markup, err = bundler.ssr.Markup(comp, props); err == nil {
			return markup
		}
	}
	log_int.Log(logging.LEVEL_DEBUG, fmt.Sprintf(
		"[go_solid/ssr] %q served without server markup: %v", data.component, err))
	return ""
}

// propsMap re-reads the props island so that slots can be filled from it.
//
// The marshalled form is the one the client receives, so rendering from it is
// what keeps the two paints agreeing.
func propsMap(propsJSON string) (map[string]any, error) {
	out := map[string]any{}
	if propsJSON == "" || propsJSON == "{}" {
		return out, nil
	}
	if err := json.Unmarshal([]byte(propsJSON), &out); err != nil {
		return nil, fmt.Errorf("go_solid: re-reading props for server rendering: %w", err)
	}
	return out, nil
}
