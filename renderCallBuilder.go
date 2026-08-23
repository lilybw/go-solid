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
	// Illegal after ForRequest: the request's own context wins, and this call
	// would be silently discarded. Order them the other way, or do not set a
	// context on a render bound to a request.
	WithCtx(ctx context.Context) RenderCallBuilder
	MountOnRootID(id string) RenderCallBuilder
	WithHTMLHeadTags(fn meta.Configurator[networking.HTMLHeadSegmentBuilder]) RenderCallBuilder
	// Automatically route the render call to the given request and response writer.
	// Includes basic http request handling, status codes and error handling. To
	// customize the behaviour, use SetHTTPBehaviour(configurator).
	//
	// This sets the context for the render call to the request's context, which
	// makes a later WithCtx illegal: see WithCtx.
	ForRequest(w http.ResponseWriter, r *http.Request) RenderCallBuilder
	// Alter default http request behaviour.
	// If a ResponseWriter and Request have been provided previously, these will carry over, but can be overwritten
	SetHTTPBehaviour(fn meta.Configurator[networking.RequestBehaviourBuilder]) RenderCallBuilder

	Render() (*caching.Rendered, error)
}

func newRenderCallBuilder(bundler *Bundler, componentName meta.QualifiedName, props any, typeError error) RenderCallBuilder {
	return &renderCallBuilderImpl{
		bundler: bundler,
		data: &renderData{
			component:    componentName,
			props:        props,
			htmlHeadTags: bundler.headSegment(),
			request:      nil,
			typeError:    typeError,
		},
	}
}

type renderCallBuilderImpl struct {
	bundler *Bundler
	data    *renderData
	// behaviour is built at most once per render call. Constructing it applies
	// Config.Defaults.Requests, so a second construction would register those
	// defaults twice — which for any POSTFIX/PREFIX/PARALLEL handler means
	// running it twice for a single event.
	behaviour networking.RequestBehaviourBuilder
}

// behaviourBuilder returns the render call's request behaviour, creating it and
// applying the configured defaults on first use.
func (this *renderCallBuilderImpl) behaviourBuilder() networking.RequestBehaviourBuilder {
	if this.behaviour == nil {
		if this.data.request == nil {
			// Bind reads W and R at dispatch time, so a behaviour created
			// before either is known still writes to the real ones.
			this.data.request = networking_int.NewRequestData(nil, nil)
		}
		this.behaviour = this.bundler.behaviourDefaults().NewRequestBehaviourBuilder(this.data.request)
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

// WithCtx records the context. A request bound by ForRequest supplies its own,
// which resolveCTXSource prefers, so calling this afterwards has no effect —
// see the interface for why that ordering is out of contract.
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
	// A behaviour can exist without a request: SetHTTPBehaviour alone builds
	// one with no writer and no request, and only ForRequest supplies them.
	if this.data.request != nil && this.data.request.R != nil {
		return this.data.request.R.Context()
	}
	if this.data.ctx == nil {
		return context.Background()
	}
	return this.data.ctx
}
