package networking

import (
	"net/http"
	"reflect"
	"sync"

	"github.com/lilybw/go-solid/shared/networking/events"
)

// RequestBehaviourBuilder configures the handlers for one request.
//
// Its methods take a runtime events.EventType rather than a type parameter:
// Go 1.27 allows generic methods on concrete types but not on interfaces, and
// a generic method cannot implement an interface method. For handlers that
// receive their concrete event type, reach for RequestBehaviour.Handlers.Add.
type RequestBehaviourBuilder interface {
	SetWriter(w http.ResponseWriter) RequestBehaviourBuilder
	SetRequest(r *http.Request) RequestBehaviourBuilder

	// Upon appends fn to the primary chain for event.
	Upon(event events.EventType, fn events.NetworkingEventHandler) RequestBehaviourBuilder

	// UponSpecialized is Upon with an explicit HandlerMode.
	UponSpecialized(event events.EventType, mode HandlerMode, fn events.NetworkingEventHandler) RequestBehaviourBuilder

	// CodeUpon commits statusCode as the response status when event fires,
	// before any other handler in the primary chain runs.
	CodeUpon(event events.EventType, statusCode int) RequestBehaviourBuilder
}

type RequestBehaviour struct {
	W        http.ResponseWriter
	R        *http.Request
	Handlers *HandlerMap

	statusMu      sync.Mutex
	statusWritten bool
}

// CommitStatus writes code as the response status the first time it is called
// for this request, reporting whether it did. Later calls are no-ops, so the
// built-in default responder cannot clobber a status an explicit handler
// (CodeUpon, or a user handler running earlier in the chain) already set.
//
// Chains are dispatched concurrently, hence the lock.
func (this *RequestBehaviour) CommitStatus(code int) bool {
	this.statusMu.Lock()
	defer this.statusMu.Unlock()
	if this.statusWritten || this.W == nil {
		return false
	}
	this.statusWritten = true
	this.W.WriteHeader(code)
	return true
}

// Bind turns a builder-style handler into a stored Handler. W and R are read at
// dispatch time, so a behaviour built before the writer is known still writes
// to the real one.
func (this *RequestBehaviour) Bind(fn events.NetworkingEventHandler) Handler {
	return func(event events.NetworkingEvent) error {
		return fn(this.W, this.R, event)
	}
}

// Dispatch runs the handlers for event: first those registered for its own
// type, then those registered for the capability bucket it belongs to. It
// returns the first error any of them reported.
//
// A nil behaviour, a nil handler map or a nil event is a no-op, so a render
// that was never bound to a request needs no guard at the call site.
func (this *RequestBehaviour) Dispatch(event events.NetworkingEvent) error {
	if this == nil || this.Handlers == nil || event == nil {
		return nil
	}

	var firstErr error
	run := func(key events.EventType) {
		if chains, ok := this.Handlers.chains(key); ok {
			if err := chains.Dispatch(event); err != nil && firstErr == nil {
				firstErr = err
			}
		}
	}

	run(reflect.TypeOf(event))

	// Category fallback: an event belongs to exactly one bucket.
	switch event.(type) {
	case events.SuccessEvent:
		run(events.EVENTS.SuccessEvent)
	case events.FailureEvent:
		run(events.EVENTS.FailureEvent)
	}

	return firstErr
}
