package go_solid

import (
	"context"

	caching "github.com/lilybw/go_solid/internal/caching"
	"github.com/lilybw/go_solid/internal/meta"
)

type RenderCallBuilder interface {
	WithRunCtx(ctx context.Context) RenderCallBuilder
	MountOnRootID(id string) RenderCallBuilder
	WithHTMLHeadTags(fn Configurator[HTMLHeadSegmentBuilder]) RenderCallBuilder

	Render() (*caching.Rendered, error)
}

func newRenderCallBuilder(bundler *Bundler, componentName meta.QualifiedName, props any) RenderCallBuilder {
	return &renderCallBuilderImpl{
		bundler: bundler,
		data: renderData{
			component:    componentName,
			props:        props,
			rootID:       "go-solid-root",
			htmlHeadTags: newHTMLHeadSegmentBuilder(),
		},
	}
}

type renderCallBuilderImpl struct {
	bundler *Bundler
	data    renderData
}

func (this *renderCallBuilderImpl) WithHTMLHeadTags(fn Configurator[HTMLHeadSegmentBuilder]) RenderCallBuilder {
	fn(this.data.htmlHeadTags)
	return this
}

func (this *renderCallBuilderImpl) WithRunCtx(ctx context.Context) RenderCallBuilder {
	this.data.ctx = ctx
	return this
}

func (this *renderCallBuilderImpl) MountOnRootID(id string) RenderCallBuilder {
	this.data.rootID = id
	return this
}

func (this *renderCallBuilderImpl) Render() (*caching.Rendered, error) {
	if this.data.ctx == nil {
		this.data.ctx = context.Background()
	}
	return render0(this.bundler, this.data)
}
