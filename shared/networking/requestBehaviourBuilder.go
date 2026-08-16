package networking

import (
	"net/http"
	"reflect"
	"sync"

	"github.com/lilybw/go-solid/shared/networking/events"
)

type RequestBehaviourBuilder interface {
	SetWriter(w http.ResponseWriter) RequestBehaviourBuilder
	SetRequest(r *http.Request) RequestBehaviourBuilder
	Upon(event events.EventType, fn events.NetworkingEventHandler[events.NetworkingEvent]) RequestBehaviourBuilder
	UponSpecialized(event events.EventType, mode SpecializedHandlerMode, fn events.NetworkingEventHandler[events.NetworkingEvent]) RequestBehaviourBuilder
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
// Handler groups are dispatched concurrently, hence the lock.
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

func (this *RequestBehaviour) Bind(fn events.NetworkingEventHandler[events.NetworkingEvent]) RequestBoundHandler[events.NetworkingEvent] {
	return func(event events.NetworkingEvent) error {
		return fn(this.W, this.R, event)
	}
}

func ExecHandlers[T events.NetworkingEvent](rb *RequestBehaviour, event T) error {
	if rb == nil || rb.Handlers == nil {
		return nil
	}

	var firstErr error
	run := func(key reflect.Type) {
		if sv, ok := rb.Handlers.GetType(key); ok {
			if err := dispatchStored(sv, event); err != nil && firstErr == nil {
				firstErr = err
			}
		}
	}

	// Specific handlers for this concrete event type.
	run(reflect.TypeOf(event))

	// Category fallback: whichever capability interface the event implements.
	switch any(event).(type) {
	case events.SuccessEvent:
		run(events.EVENTS.SuccessEvent)
	case events.FailureEvent:
		run(events.EVENTS.FailureEvent)
	}

	return firstErr
}

// RequestBoundHandler is a handler bound to a specific request (writer/request
// already captured). It handles an event of concrete type T.
type RequestBoundHandler[T events.NetworkingEvent] func(event T) error

// justForGetCast lets RequestBoundHandler satisfy the sealed marker. The type
// parameter is named T (not `any`) — a method on a generic type covers ALL
// instantiations regardless of the name, so naming it `any` was a misleading
// shadow of the builtin, not a wildcard.
func (RequestBoundHandler[T]) justForGetCast() {}
