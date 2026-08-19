package networking

import (
	"fmt"
	"reflect"

	"github.com/lilybw/go-solid/shared/networking/events"
)

// Handler is a request-bound handler: the writer and request are already
// captured, so all it receives is the event. It is the single stored handler
// representation — nothing in the map is typed by event, only keyed by it.
type Handler func(event events.NetworkingEvent) error

// Chain is a sequence of handlers run in order. The first error stops the rest
// of the chain.
type Chain []Handler

// Chains are dispatched concurrently. Chain 0 is the primary chain (see
// HandlerMode); the others exist only because a handler was added in
// HANDLER_MODE_PARALLEL.
type Chains []Chain

// HandlerMap holds the handlers for one request, keyed by event type. The key
// is either a concrete event type or one of the capability buckets in
// events.EVENTS.Categories.
//
// Every method tolerates a nil map except those that would have to store
// something, which panic: silently dropping a handler registration is worse
// than a stack trace at the call site.
type HandlerMap struct {
	internal map[events.EventType]Chains
}

func NewHandlerMap() *HandlerMap {
	return &HandlerMap{internal: make(map[events.EventType]Chains)}
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

// Add registers a handler that receives its event already typed as T. The key
// is derived from T rather than passed in, so the handler cannot be filed
// under a type it does not accept.
//
// T may be a capability bucket: Add(func(e events.FailureEvent) error {...})
// registers one handler for every failure the library can emit.
func (m *HandlerMap) Add[T events.NetworkingEvent](handler func(T) error, mode HandlerMode) *HandlerMap {
	if handler == nil {
		panic("networking: Add with a nil handler")
	}
	key := reflect.TypeFor[T]()
	return m.AddType(key, func(event events.NetworkingEvent) error {
		typed, ok := event.(T)
		if !ok {
			// Unreachable while dispatch keys off the dynamic type; an error
			// rather than a panic so a library bug cannot take down a request.
			return fmt.Errorf("networking: handler registered for %v received %T", key, event)
		}
		return handler(typed)
	}, mode)
}

// AddType is Add for callers holding a runtime event type instead of a type
// parameter, and is the only entry point that takes one.
//
// It exists because two callers genuinely cannot name their event type at
// compile time: RequestBehaviourBuilder, whose methods cannot be generic
// because Go 1.27 forbids type parameters on interface methods, and any caller
// registering across events.EVENTS. Prefer Add everywhere else — it derives
// the key from the handler, so the two cannot disagree.
func (m *HandlerMap) AddType(key events.EventType, handler Handler, mode HandlerMode) *HandlerMap {
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
	case HANDLER_MODE_PARALLEL:
		m.internal[key] = append(existing, Chain{handler})
	case HANDLER_MODE_REPLACE:
		m.internal[key] = withPrimary(existing, Chain{handler})
	case HANDLER_MODE_PREFIX:
		m.internal[key] = withPrimary(existing, append(Chain{handler}, primary(existing)...))
	case HANDLER_MODE_POSTFIX:
		m.internal[key] = withPrimary(existing, append(primary(existing), handler))
	default:
		panic("networking: " + mode.String() + " is not a usable handler mode")
	}
	return m
}

// --- lookup and removal ---------------------------------------------------
//
// Keyed by type parameter only. Dispatch reaches the same storage through the
// unexported accessors below, so a runtime-keyed read never has to be exported.

func (m *HandlerMap) Get[T events.NetworkingEvent]() (Chains, bool) {
	return m.chains(reflect.TypeFor[T]())
}

// Set replaces everything stored for T.
func (m *HandlerMap) Set[T events.NetworkingEvent](chains Chains) *HandlerMap {
	return m.store(reflect.TypeFor[T](), chains)
}

func (m *HandlerMap) Has[T events.NetworkingEvent]() bool {
	_, ok := m.chains(reflect.TypeFor[T]())
	return ok
}

func (m *HandlerMap) Delete[T events.NetworkingEvent]() bool {
	return m.remove(reflect.TypeFor[T]())
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

// Dispatch runs every chain concurrently and returns the first error reported
// by any of them. A single chain runs on the calling goroutine.
func (c Chains) Dispatch(event events.NetworkingEvent) error {
	switch len(c) {
	case 0:
		return nil
	case 1:
		return c[0].Run(event)
	}

	errs := make(chan error, len(c))
	for _, chain := range c {
		go func() { errs <- chain.Run(event) }()
	}

	var first error
	for range c {
		if err := <-errs; err != nil && first == nil {
			first = err
		}
	}
	return first
}

// --- internals ------------------------------------------------------------

// chains, store and remove are the runtime-keyed accessors. They stay
// unexported: the only caller outside this package that needs a runtime key is
// the fluent builder, and it only ever registers (see AddType).

func (m *HandlerMap) chains(key events.EventType) (Chains, bool) {
	if m == nil {
		return nil, false
	}
	c, ok := m.internal[key]
	return c, ok
}

func (m *HandlerMap) store(key events.EventType, chains Chains) *HandlerMap {
	switch {
	case m == nil:
		panic("networking: Set on a nil *HandlerMap")
	case key == nil:
		panic("networking: Set with a nil event type")
	}
	m.internal[key] = chains
	return m
}

func (m *HandlerMap) remove(key events.EventType) bool {
	if _, ok := m.chains(key); !ok {
		return false
	}
	delete(m.internal, key)
	return true
}

// primary is chain 0, the chain every non-PARALLEL mode edits.
func primary(c Chains) Chain {
	if len(c) == 0 {
		return nil
	}
	return c[0]
}

// withPrimary writes chain back as chain 0, creating the slot if needed and
// leaving any parallel chains untouched.
func withPrimary(c Chains, chain Chain) Chains {
	if len(c) == 0 {
		return Chains{chain}
	}
	c[0] = chain
	return c
}
