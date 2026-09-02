package go_solid

import (
	"context"
	"net/http"

	caching "github.com/lilybw/go-solid/internal/caching"
	networking_int "github.com/lilybw/go-solid/internal/networking"
	"github.com/lilybw/go-solid/shared/meta"
	networking "github.com/lilybw/go-solid/shared/networking"
)

type RenderCallBuilder interface {
	// WithCtx sets the context the render is cancelled by.
	//
	// Illegal after ForRequest: the request's own context wins
	WithCtx(ctx context.Context) RenderCallBuilder
	MountOnRootID(id string) RenderCallBuilder
	WithHTMLHeadTags(fn meta.Configurator[networking.HTMLHeadSegmentBuilder]) RenderCallBuilder
	// Automatically route the render call to the given request and response writer.
	// Includes basic http request handling, status codes and error handling. To
	// customize the behaviour, use SetHTTPBehaviour(configurator).
	ForRequest(w http.ResponseWriter, r *http.Request) RenderCallBuilder
	// Discard what this call has configured, start again from the bundler's
	// defaults, and apply fn to that.
	SetHTTPBehaviour(fn meta.Configurator[*networking.RequestBehaviourBuilder]) RenderCallBuilder
	// Append upon existing behaviour
	AlterHTTPBehaviour(fn meta.Configurator[*networking.RequestBehaviourBuilder]) RenderCallBuilder

	Render() (*caching.Rendered, error)
}

func newRenderCallBuilder(bundler *Bundler, componentName meta.QualifiedName, props any, faults renderFaults) RenderCallBuilder {
	builder := &renderCallBuilderImpl{
		bundler: bundler,
		data: &renderData{
			component:    componentName,
			props:        props,
			htmlHeadTags: bundler.defaults.NewHTMLHeadSegmentBuilder(),
			faults:       faults,
		},
	}
	// Seeded here rather than on first use, which is the treatment the head
	// segment already gets. A configured default is a property of the bundler,
	// so the middleware and handlers it registers have to be there whether or
	// not this call goes on to name a writer.
	builder.behaviour = builder.seedBehaviour()
	return builder
}

type renderCallBuilderImpl struct {
	bundler   *Bundler
	data      *renderData
	behaviour *networking.RequestBehaviourBuilder
}

// seedBehaviour starts a behaviour from the bundler's defaults, carrying over
// the request the previous one was serving. Applying the defaults to a fresh
// behaviour rather than over the old one is what keeps them registered once,
// however many times a call reconfigures itself.
func (this *renderCallBuilderImpl) seedBehaviour() *networking.RequestBehaviourBuilder {
	previous := this.data.request
	this.data.request = networking_int.NewRequestData(nil, nil)
	if previous != nil {
		this.data.request.BindWriter(previous.W) // nil-safe, and never wraps twice
		this.data.request.R = previous.R
	}
	return this.bundler.defaults.NewRequestBehaviourBuilder(this.data.request)
}

func (this *renderCallBuilderImpl) AlterHTTPBehaviour(fn meta.Configurator[*networking.RequestBehaviourBuilder]) RenderCallBuilder {
	fn(this.behaviour)
	return this
}

func (this *renderCallBuilderImpl) SetHTTPBehaviour(fn meta.Configurator[*networking.RequestBehaviourBuilder]) RenderCallBuilder {
	this.behaviour = this.seedBehaviour()
	fn(this.behaviour)
	return this
}

func (this *renderCallBuilderImpl) ForRequest(w http.ResponseWriter, r *http.Request) RenderCallBuilder {
	this.data.request.BindWriter(w)
	this.data.request.R = r
	return this
}

func (this *renderCallBuilderImpl) WithHTMLHeadTags(fn meta.Configurator[networking.HTMLHeadSegmentBuilder]) RenderCallBuilder {
	fn(this.data.htmlHeadTags)
	return this
}

func (this *renderCallBuilderImpl) WithCtx(ctx context.Context) RenderCallBuilder {
	this.data.ctx = ctx
	return this
}

func (this *renderCallBuilderImpl) MountOnRootID(id string) RenderCallBuilder {
	this.data.root = id
	return this
}

func (this *renderCallBuilderImpl) Render() (*caching.Rendered, error) {
	this.data.ctx = this.resolveCTXSource()
	return render0(this.bundler, this.data)
}

func (this *renderCallBuilderImpl) resolveCTXSource() context.Context {
	if this.data.request != nil && this.data.request.R != nil {
		return this.data.request.R.Context()
	}
	if this.data.ctx == nil {
		return context.Background()
	}
	return this.data.ctx
}
