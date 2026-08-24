package go_solid

import (
	"context"
	"net/http"

	caching "github.com/lilybw/go-solid/internal/caching"
	"github.com/lilybw/go-solid/internal/meta"
	networking_int "github.com/lilybw/go-solid/internal/networking"
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
	SetHTTPBehaviour(fn meta.Configurator[networking.RequestBehaviourBuilder]) RenderCallBuilder

	Render() (*caching.Rendered, error)
}

func newRenderCallBuilder(bundler *Bundler, componentName meta.QualifiedName, props any, typeError error) RenderCallBuilder {
	return &renderCallBuilderImpl{
		bundler: bundler,
		data: &renderData{
			component:    componentName,
			props:        props,
			htmlHeadTags: bundler.defaults.NewHTMLHeadSegmentBuilder(),
			request:      nil,
			typeError:    typeError,
		},
	}
}

type renderCallBuilderImpl struct {
	bundler   *Bundler
	data      *renderData
	behaviour networking.RequestBehaviourBuilder
}

func (this *renderCallBuilderImpl) behaviourBuilder() networking.RequestBehaviourBuilder {
	if this.behaviour == nil {
		if this.data.request == nil {
			this.data.request = networking_int.NewRequestData(nil, nil)
		}
		this.behaviour = this.bundler.defaults.NewRequestBehaviourBuilder(this.data.request)
	}
	return this.behaviour
}

func (this *renderCallBuilderImpl) SetHTTPBehaviour(fn meta.Configurator[networking.RequestBehaviourBuilder]) RenderCallBuilder {
	fn(this.behaviourBuilder())
	return this
}

func (this *renderCallBuilderImpl) ForRequest(w http.ResponseWriter, r *http.Request) RenderCallBuilder {
	this.behaviourBuilder()
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
