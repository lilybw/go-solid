package networking

import (
	"sync"

	"github.com/lilybw/go-solid/internal/meta"
	"github.com/lilybw/go-solid/internal/noop"
	. "github.com/lilybw/go-solid/shared/networking"
)

// Defaults holds one Bundler's behaviour templates: the head segment every
// render starts from, and the configurator applied to every request behaviour
// it builds.
//
// It belongs to a Bundler rather than to the process, so two Bundlers in one
// program keep their own defaults. Renders read it while New writes it, hence
// the lock.
//
//	d := networking.NewDefaults()
//	d.SetHTMLHeadSegment(func(h networking.HTMLHeadSegmentBuilder) { h.SetTitle("app") })
//	head := d.NewHTMLHeadSegmentBuilder() // carries the title
//
// A nil *Defaults is usable and yields the library defaults, so a caller
// holding no configuration need not branch.
type Defaults struct {
	mu       sync.RWMutex
	head     *htmlHeadSegmentBuilder
	requests meta.Configurator[RequestBehaviourBuilder]
}

func NewDefaults() *Defaults {
	return &Defaults{
		head:     newHeadSegmentBuilder(),
		requests: noop.T_o_Void[RequestBehaviourBuilder](),
	}
}

// SetHTMLHeadSegment replaces the head template with a fresh one configured by
// fn. Builders already handed out are unaffected.
func (d *Defaults) SetHTMLHeadSegment(fn meta.Configurator[HTMLHeadSegmentBuilder]) {
	if d == nil || fn == nil {
		return
	}
	template := newHeadSegmentBuilder()
	fn(template)

	d.mu.Lock()
	d.head = template
	d.mu.Unlock()
}

// SetRequestBehaviour replaces the configurator applied to every request
// behaviour built from these defaults.
func (d *Defaults) SetRequestBehaviour(fn meta.Configurator[RequestBehaviourBuilder]) {
	if d == nil || fn == nil {
		return
	}
	d.mu.Lock()
	d.requests = fn
	d.mu.Unlock()
}

// NewHTMLHeadSegmentBuilder returns a builder seeded from the head template.
func (d *Defaults) NewHTMLHeadSegmentBuilder() HTMLHeadSegmentBuilder {
	if d == nil {
		return NewHTMLHeadSegmentBuilder()
	}
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.head.clone()
}

// NewRequestBehaviourBuilder returns a builder for data with the request
// template already applied. Applying it is what registers the consumer's
// default handlers, so it must happen once per render call and not once per
// builder method.
func (d *Defaults) NewRequestBehaviourBuilder(data *RequestBehaviour) RequestBehaviourBuilder {
	instance := NewRequestBehaviourBuilder(data)
	if d == nil {
		return instance
	}
	d.mu.RLock()
	fn := d.requests
	d.mu.RUnlock()

	if fn != nil {
		fn(instance)
	}
	return instance
}
