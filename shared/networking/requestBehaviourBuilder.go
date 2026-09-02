package networking

import (
	"net/http"
	"sync"

	"github.com/lilybw/go-solid/shared/meta"
	"github.com/lilybw/go-solid/shared/networking/events"
)

// RequestBehaviourBuilder configures the handlers for one request.
type RequestBehaviourBuilder struct {
	data *RequestBehaviour
}

// NewRequestBehaviourBuilder returns a builder carrying no consumer defaults.
func NewRequestBehaviourBuilder(data *RequestBehaviour) *RequestBehaviourBuilder {
	return &RequestBehaviourBuilder{data: data}
}

func (this *RequestBehaviourBuilder) With(middleware ...Middleware) *RequestBehaviourBuilder {
	this.data.Middleware = middleware
	return this
}

func (this *RequestBehaviourBuilder) Upon[T events.NetworkingEvent](fn events.NetworkingEventHandler[T]) *RequestBehaviourBuilder {
	return this.UponSpecialized[T](HANDLER_MODE_POSTFIX, fn)
}

func (this *RequestBehaviourBuilder) UponSpecialized[T events.NetworkingEvent](mode HandlerMode, fn events.NetworkingEventHandler[T]) *RequestBehaviourBuilder {
	this.data.Handlers.Add[T](this.data.Bind(fn), mode)
	return this
}

func (this *RequestBehaviourBuilder) Testing_UponRaw(t events.EventType, mode HandlerMode, fn events.NetworkingEventHandler[events.NetworkingEvent]) *RequestBehaviourBuilder {
	this.data.Handlers.AddType(t, this.data.Bind(fn), mode)
	return this
}

func (this *RequestBehaviourBuilder) SetWriter(w http.ResponseWriter) *RequestBehaviourBuilder {
	meta.PanicIfTrue(w == nil, "SetWriter: writer cannot be nil")
	this.data.BindWriter(w)
	return this
}

func (this *RequestBehaviourBuilder) SetRequest(r *http.Request) *RequestBehaviourBuilder {
	meta.PanicIfTrue(r == nil, "SetRequest: request cannot be nil")
	this.data.R = r
	return this
}

func (this *RequestBehaviourBuilder) CodeUpon[T events.NetworkingEvent](statusCode int) *RequestBehaviourBuilder {
	rb := this.data
	rb.Handlers.Add[T](func(T) error {
		rb.CommitStatus(statusCode)
		return nil
	}, HANDLER_MODE_PREFIX)
	return this
}

type RequestBehaviour struct {
	W          http.ResponseWriter
	R          *http.Request
	Handlers   *HandlerMap
	Middleware []Middleware

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

func (this *RequestBehaviour) Bind[T events.NetworkingEvent](fn events.NetworkingEventHandler[T]) func(T) error {
	return func(event T) error {
		return fn(this.W, this.R, event)
	}
}

// Dispatch runs the handlers for event: first those registered for its own
// type, then those registered for the capability bucket it belongs to.
func (this *RequestBehaviour) Dispatch[T events.NetworkingEvent](event T) error {
	return this.Handlers.Dispatch(event)
}
