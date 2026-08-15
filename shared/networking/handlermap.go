package networking

import (
	"reflect"

	"github.com/lilybw/go-solid/shared/networking/events"
)

type HandlerMapValue[T events.NetworkingEvent] [][]RequestBoundHandler[T]
type storedValue = HandlerMapValue[events.NetworkingEvent]
type ooRequestBoundHandlerT interface {
	justForGetCast()
}

func (HandlerMapValue[T]) justForGetCast() {}

type HandlerMap struct {
	internal map[reflect.Type]ooRequestBoundHandlerT
}

func NewHandlerMap() *HandlerMap {
	return &HandlerMap{
		internal: make(map[reflect.Type]ooRequestBoundHandlerT),
	}
}

// --- introspection --------------------------------------------------------

func (m *HandlerMap) Len() int {
	if m == nil {
		return 0
	}
	return len(m.internal)
}

func (m *HandlerMap) Types() []reflect.Type {
	if m == nil {
		return nil
	}
	out := make([]reflect.Type, 0, len(m.internal))
	for t := range m.internal {
		out = append(out, t)
	}
	return out
}

func (m *HandlerMap) Clear() {
	if m == nil {
		return
	}
	clear(m.internal)
}

// --- untyped storage core (builder- and dispatch-facing) ------------------

// getStored returns the single stored value type under key, or nil.
func (m *HandlerMap) getStored(key reflect.Type) (storedValue, bool) {
	if m == nil {
		return nil, false
	}
	if v, ok := m.internal[key]; ok {
		if sv, ok := v.(storedValue); ok {
			return sv, true
		}
	}
	return nil, false
}

// GetType returns the raw stored value under key (the interface-typed 2D
// structure). Dispatch and the builder use this to walk handlers without a type
// parameter.
func (m *HandlerMap) GetType(t reflect.Type) (storedValue, bool) {
	return m.getStored(t)
}

func (m *HandlerMap) setType(t reflect.Type, value storedValue) {
	if m == nil {
		panic("networking: SetType on nil *HandlerMap")
	}
	m.internal[t] = value
}

// AddRaw inserts an already-interface-typed handler under key according to mode.
// This is the entry point for non-generic call sites (e.g. the fluent builder),
// where the event type is known only as a runtime reflect.Type.
//
// Because the handler is already RequestBoundHandler[NetworkingEvent] — the
// stored element type — there are no per-handler type assertions and no way to
// smuggle in a mismatched value type. Add[T] funnels through here after wrapping.
func AddRaw(m *HandlerMap, key events.EventType, handler RequestBoundHandler[events.NetworkingEvent], mode SpecializedHandlerMode) {
	if m == nil {
		panic("networking: AddRaw on nil *HandlerMap")
	}
	existing, _ := m.getStored(key)

	switch mode {
	case HANDLER_MODE_INVALID:
		panic("networking: invalid handler mode")

	case HANDLER_MODE_PARALLEL:
		m.setType(key, append(existing, []RequestBoundHandler[events.NetworkingEvent]{handler}))

	case HANDLER_MODE_REPLACE:
		if len(existing) == 0 {
			m.setType(key, storedValue{{handler}})
			return
		}
		existing[0] = []RequestBoundHandler[events.NetworkingEvent]{handler}
		m.setType(key, existing)

	case HANDLER_MODE_PREFIX:
		if len(existing) == 0 {
			m.setType(key, storedValue{{handler}})
			return
		}
		existing[0] = append([]RequestBoundHandler[events.NetworkingEvent]{handler}, existing[0]...)
		m.setType(key, existing)

	case HANDLER_MODE_POSTFIX:
		if len(existing) == 0 {
			m.setType(key, storedValue{{handler}})
			return
		}
		existing[0] = append(existing[0], handler)
		m.setType(key, existing)

	default:
		panic("networking: unknown handler mode")
	}
}

func (m *HandlerMap) HasType(t reflect.Type) bool {
	if m == nil {
		return false
	}
	_, ok := m.internal[t]
	return ok
}

func (m *HandlerMap) DeleteType(t reflect.Type) bool {
	if m == nil {
		return false
	}
	if _, ok := m.internal[t]; ok {
		delete(m.internal, t)
		return true
	}
	return false
}

// --- typed convenience API (boundary-wrapping sugar) ----------------------

// wrap adapts a T-typed handler into the stored interface handler by asserting
// the concrete event on the way in. Safe because the handler is only ever
// stored under key reflect.TypeFor[T](), so the event delivered under that key
// has dynamic type T.
func wrap[T events.NetworkingEvent](fn RequestBoundHandler[T]) RequestBoundHandler[events.NetworkingEvent] {
	return func(e events.NetworkingEvent) error {
		return fn(e.(T))
	}
}

func Add[T events.NetworkingEvent](m *HandlerMap, handler RequestBoundHandler[T], mode SpecializedHandlerMode) {
	AddRaw(m, reflect.TypeFor[T](), wrap(handler), mode)
}

func Get[T events.NetworkingEvent](m *HandlerMap) (HandlerMapValue[T], bool) {
	sv, ok := m.getStored(reflect.TypeFor[T]())
	if !ok {
		return nil, false
	}
	out := make(HandlerMapValue[T], len(sv))
	for i, group := range sv {
		chain := make([]RequestBoundHandler[T], len(group))
		for j, h := range group {
			h := h
			chain[j] = func(e T) error { return h(e) }
		}
		out[i] = chain
	}
	return out, true
}

func Set[T events.NetworkingEvent](m *HandlerMap, value HandlerMapValue[T]) {
	if m == nil {
		panic("networking: Set on nil *HandlerMap")
	}
	sv := make(storedValue, len(value))
	for i, group := range value {
		chain := make([]RequestBoundHandler[events.NetworkingEvent], len(group))
		for j, h := range group {
			chain[j] = wrap(h)
		}
		sv[i] = chain
	}
	m.setType(reflect.TypeFor[T](), sv)
}

func Has[T events.NetworkingEvent](m *HandlerMap) bool {
	return m.HasType(reflect.TypeFor[T]())
}

func Delete[T events.NetworkingEvent](m *HandlerMap) bool {
	return m.DeleteType(reflect.TypeFor[T]())
}

func IfPresent[T events.NetworkingEvent](m *HandlerMap, fn func(HandlerMapValue[T])) bool {
	if v, ok := Get[T](m); ok {
		fn(v)
		return true
	}
	return false
}

func (m *HandlerMap) IfPresentType(t reflect.Type, fn func(storedValue)) bool {
	if v, ok := m.getStored(t); ok {
		fn(v)
		return true
	}
	return false
}

// GetOr returns the typed value for T, or fallback if absent.
func GetOr[T events.NetworkingEvent](m *HandlerMap, fallback HandlerMapValue[T]) HandlerMapValue[T] {
	if v, ok := Get[T](m); ok {
		return v
	}
	return fallback
}

// GetOrType returns the raw stored value under t, or fallback if absent.
func (m *HandlerMap) GetOrType(t reflect.Type, fallback storedValue) storedValue {
	if v, ok := m.getStored(t); ok {
		return v
	}
	return fallback
}

func GetOrSet[T events.NetworkingEvent](m *HandlerMap, value HandlerMapValue[T]) (HandlerMapValue[T], bool) {
	if v, ok := Get[T](m); ok {
		return v, false
	}
	// Wrap each typed handler down to the stored element type.
	sv := make(storedValue, len(value))
	for i, group := range value {
		chain := make([]RequestBoundHandler[events.NetworkingEvent], len(group))
		for j, h := range group {
			chain[j] = wrap(h)
		}
		sv[i] = chain
	}
	m.setType(reflect.TypeFor[T](), sv)
	return value, true
}

// --- dispatch -------------------------------------------------------------

// dispatchStored runs a stored 2D handler structure: each parallel group runs
// concurrently, and within a group the sequential chain runs in order, stopping
// that chain at the first error. Returns the first error encountered across all
// groups (if any). A nil stored value is a no-op.
//
// It takes the event as events.NetworkingEvent because dispatch is keyed by the
// event's dynamic type; each stored handler already closes over the concrete
// assertion (via wrap), so passing the interface value is correct.
func dispatchStored(sv storedValue, event events.NetworkingEvent) error {
	if len(sv) == 0 {
		return nil
	}

	type result struct{ err error }
	results := make(chan result, len(sv))

	for _, group := range sv {
		group := group
		go func() {
			for _, h := range group {
				if h == nil {
					continue
				}
				if err := h(event); err != nil {
					results <- result{err}
					return
				}
			}
			results <- result{nil}
		}()
	}

	var firstErr error
	for range sv {
		if r := <-results; r.err != nil && firstErr == nil {
			firstErr = r.err
		}
	}
	return firstErr
}
