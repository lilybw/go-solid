package networking

import (
	"net/http"
	"reflect"

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
}

// Bind adapts an http-shaped event handler into a request-bound handler by
// capturing this behaviour's writer and request. The returned handler is the
// uniform stored element type, so it can be handed straight to AddRaw.
func (this *RequestBehaviour) Bind(fn events.NetworkingEventHandler[events.NetworkingEvent]) RequestBoundHandler[events.NetworkingEvent] {
	return func(event events.NetworkingEvent) error {
		return fn(this.W, this.R, event)
	}
}

// ExecHandlers dispatches event to every handler registered under its dynamic
// type. Parallel groups run concurrently; each sequential chain runs in order
// and stops at its first error. Returns the first error across all groups.
//
// Dispatch keys on the DYNAMIC type of event (reflect.TypeOf), not on T — so
// emitting a value typed statically as the interface still finds the concrete
// handlers registered under the concrete type.
func ExecHandlers[T events.NetworkingEvent](rb *RequestBehaviour, event T) error {
	if rb == nil || rb.Handlers == nil {
		return nil
	}
	key := reflect.TypeOf(event)
	sv, ok := rb.Handlers.GetType(key)
	if !ok {
		return nil
	}
	return dispatchStored(sv, event)
}

// RequestBoundHandler is a handler bound to a specific request (writer/request
// already captured). It handles an event of concrete type T.
type RequestBoundHandler[T events.NetworkingEvent] func(event T) error

// justForGetCast lets RequestBoundHandler satisfy the sealed marker. The type
// parameter is named T (not `any`) — a method on a generic type covers ALL
// instantiations regardless of the name, so naming it `any` was a misleading
// shadow of the builtin, not a wildcard.
func (RequestBoundHandler[T]) justForGetCast() {}
