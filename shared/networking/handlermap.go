package networking

import (
	"reflect"

	"github.com/lilybw/go-solid/shared/networking/events"
)

type Handler func(event events.NetworkingEvent) error
type Chain []Handler

type HandlerMap struct {
	internal map[events.EventType]Chain
}

func NewHandlerMap() *HandlerMap {
	return &HandlerMap{internal: make(map[events.EventType]Chain)}
}

// --- introspection --------------------------------------------------------

func (m *HandlerMap) Len() int {
	if m == nil {
		return 0
	}
	return len(m.internal)
}

func (m *HandlerMap) Types() []events.EventType {
	if m == nil {
		return nil
	}
	out := make([]events.EventType, 0, len(m.internal))
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

// --- registration ---------------------------------------------------------

// Add registers a handler that receives its event already typed as T.
func (m *HandlerMap) Add[T events.NetworkingEvent](handler func(T) error, mode HandlerMode) *HandlerMap {
	if handler == nil {
		panic("networking: Add with a nil handler")
	}
	key := reflect.TypeFor[T]()
	return m.AddType(key, func(event events.NetworkingEvent) error {
		typed, ok := event.(T)
		if !ok {
			// Unreachable: dispatch keys concrete handlers by the event's own
			// dynamic type, and runs a bucket only after testing the event
			// against it. See HandlerNarrowingDefect for why reaching it panics
			// rather than returning.
			panic(HandlerNarrowingDefect{
				Stage:    DEFECT_AT_DISPATCH,
				Requires: key,
				Received: reflect.TypeOf(event),
			})
		}
		return handler(typed)
	}, mode)
}

func (m *HandlerMap) AddType(key events.EventType, handler func(events.NetworkingEvent) error, mode HandlerMode) *HandlerMap {
	switch {
	case m == nil:
		panic("networking: AddType on a nil *HandlerMap")
	case key == nil:
		panic("networking: AddType with a nil event type")
	case handler == nil:
		panic("networking: AddType with a nil handler")
	}

	existing := m.internal[key]
	switch mode {
	case HANDLER_MODE_REPLACE:
		m.internal[key] = []Handler{handler}
	case HANDLER_MODE_PREFIX:
		m.internal[key] = append([]Handler{handler}, existing...)
	case HANDLER_MODE_POSTFIX:
		m.internal[key] = append(existing, handler)
	default:
		panic("networking: " + mode.String() + " is not a usable handler mode")
	}
	return m
}

// --- lookup and removal ---------------------------------------------------

func (m *HandlerMap) Get[T events.NetworkingEvent]() (Chain, bool) {
	c, k := m.internal[reflect.TypeFor[T]()]
	return c, k
}

// Set replaces everything stored for T.
func (m *HandlerMap) Set[T events.NetworkingEvent](chain Chain) *HandlerMap {
	m.internal[reflect.TypeFor[T]()] = chain
	return m
}

func (m *HandlerMap) Has[T events.NetworkingEvent]() bool {
	_, ok := m.internal[reflect.TypeFor[T]()]
	return ok
}

func (m *HandlerMap) Delete[T events.NetworkingEvent]() bool {
	key := reflect.TypeFor[T]()
	if _, ok := m.internal[key]; !ok {
		return false
	}
	delete(m.internal, key)
	return true
}

// --- dispatch -------------------------------------------------------------

// Run executes the chain in order, stopping at the first error and returning
// it. Nil handlers are skipped.
func (c Chain) Run(event events.NetworkingEvent) error {
	for _, handler := range c {
		if handler == nil {
			continue
		}
		if err := handler(event); err != nil {
			return err
		}
	}
	return nil
}

func (this *HandlerMap) Dispatch[T events.NetworkingEvent](event T) error {
	for _, handlerType := range events.DynamicAncestry(event) {
		var err error
		if chain, ok := this.internal[handlerType]; ok {
			err = chain.Run(event)
		}
		if err != nil {
			return err
		}
	}

	return nil
}
