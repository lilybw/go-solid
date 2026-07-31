package go_solid

import "context"

type RenderCallBuilder interface {
	WithRunCtx(ctx context.Context) RenderCallBuilder
	MountOnRootID(id string) RenderCallBuilder
	WithHTMLHeadTags(fn func(configurator HTMLHeadSegmentBuilder)) RenderCallBuilder

	Render() (*Rendered, error)
}

func newRenderCallBuilder(bundler *Bundler, componentName QualifiedName, props any) RenderCallBuilder {
	return &renderCallBuilderImpl{
		bundler: bundler,
		data: renderData{
			component:    componentName,
			props:        props,
			htmlHeadTags: newHTMLHeadSegmentBuilder(),
		},
	}
}

type renderCallBuilderImpl struct {
	bundler *Bundler
	data    renderData
}

func (this *renderCallBuilderImpl) WithHTMLHeadTags(fn func(configurator HTMLHeadSegmentBuilder)) RenderCallBuilder {

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

func (this *renderCallBuilderImpl) Render() (*Rendered, error) {
	if this.data.ctx == nil {
		this.data.ctx = context.Background()
	}
	return render0(this.bundler, this.data)
}
