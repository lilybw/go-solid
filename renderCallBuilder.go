package go_solid

import (
	"context"
	"net/http"

	caching "github.com/lilybw/go-solid/internal/caching"
	"github.com/lilybw/go-solid/internal/meta"
	networking "github.com/lilybw/go-solid/internal/networking"
)

type RenderCallBuilder interface {
	WithCtx(ctx context.Context) RenderCallBuilder
	MountOnRootID(id string) RenderCallBuilder
	WithHTMLHeadTags(fn meta.Configurator[networking.HTMLHeadSegmentBuilder]) RenderCallBuilder
	// Automatically route the render call to the given request and response writer.
	// Includes basic http request handling, status codes and error handling. To
	// customize the behaviour, use WithHTTPBehaviour(configurator).
	//
	// Using this method will automatically set the context for the render call to the request's context.
	ForRequest(w http.ResponseWriter, r *http.Request) RenderCallBuilder
	// Alter default http request behaviour.
	// If a ResponseWriter and Request have been provided previously, these will carry over, but can be overwritten
	SetHTTPBehaviour(fn meta.Configurator[networking.RequestBehaviourBuilder]) RenderCallBuilder

	Render() (*caching.Rendered, error)
}

func newRenderCallBuilder(bundler *Bundler, componentName meta.QualifiedName, props any) RenderCallBuilder {
	return &renderCallBuilderImpl{
		bundler: bundler,
		data: renderData{
			component:    componentName,
			props:        props,
			htmlHeadTags: networking.NewHTMLHeadSegmentBuilder(),
			request:      nil,
		},
	}
}

type renderCallBuilderImpl struct {
	bundler *Bundler
	data    renderData
}

func (this *renderCallBuilderImpl) SetHTTPBehaviour(fn meta.Configurator[networking.RequestBehaviourBuilder]) RenderCallBuilder {
	if this.data.request == nil {
		this.data.request = networking.NewRequestData(nil, nil)
	}
	fn(networking.NewRequestBehaviourBuilder(this.data.request))
	return this
}

func (this *renderCallBuilderImpl) ForRequest(w http.ResponseWriter, r *http.Request) RenderCallBuilder {
	this.data.request = networking.NewRequestData(w, r)
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
	this.data.rootID = id
	return this
}

func (this *renderCallBuilderImpl) Render() (*caching.Rendered, error) {
	this.data.ctx = this.resolveCTXSource()
	return render0(this.bundler, this.data)
}

func (this *renderCallBuilderImpl) resolveCTXSource() context.Context {
	if this.data.request != nil {
		return this.data.request.R.Context()
	}
	if this.data.ctx == nil {
		return context.Background()
	}
	return this.data.ctx
}
