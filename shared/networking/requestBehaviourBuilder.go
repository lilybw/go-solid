package networking

import (
	"net/http"
	"sync"

	"github.com/lilybw/go-solid/shared/networking/events"
)

// RequestBehaviourBuilder configures the handlers for one request.
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
// for this request, reporting whether it did. Later calls are no-ops
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

func (this *RequestBehaviour) BindWriter(w http.ResponseWriter) {
	this.W = Synchronized(w)
}

func (this *RequestBehaviour) Bind(fn events.NetworkingEventHandler) Handler {
	return func(event events.NetworkingEvent) error {
		return fn(this.W, this.R, event)
	}
}

// Dispatch runs the handlers for event: first those registered for its own
// type, then those registered for the capability bucket it belongs to.
func (this *RequestBehaviour) Dispatch[T events.NetworkingEvent](event T) error {
	return this.Handlers.Dispatch(event)
}
