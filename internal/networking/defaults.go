package networking

import (
	"sync"

	"github.com/lilybw/go-solid/internal/meta"
	"github.com/lilybw/go-solid/internal/noop"
	. "github.com/lilybw/go-solid/shared/networking"
)

type Defaults struct {
	mu       sync.RWMutex
	head     *htmlHeadSegmentBuilder
	requests meta.Configurator[*RequestBehaviourBuilder]
}

func NewDefaults() *Defaults {
	return &Defaults{
		head:     newHeadSegmentBuilder(),
		requests: noop.T_o_Void[*RequestBehaviourBuilder](),
	}
}

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

func (d *Defaults) SetRequestBehaviour(fn meta.Configurator[*RequestBehaviourBuilder]) {
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

func (d *Defaults) NewRequestBehaviourBuilder(data *RequestBehaviour) *RequestBehaviourBuilder {
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
