package networking

import (
	"net/http"

	"github.com/lilybw/go-solid/internal/meta"
	"github.com/lilybw/go-solid/internal/noop"
	. "github.com/lilybw/go-solid/shared/networking"
	"github.com/lilybw/go-solid/shared/networking/events"
)

var requestBehaviourBuilderTemplate = noop.T_o_Void[RequestBehaviourBuilder]()

func SetRequestBehaviourTemplate(fn meta.Configurator[RequestBehaviourBuilder]) {
	requestBehaviourBuilderTemplate = fn
}

type requestBehaviourBuilder struct {
	data *RequestBehaviour
}

// NewRequestBehaviourBuilder returns the builder as the RequestBehaviourBuilder
// interface (not a pointer to the interface — that was the previous bug).
func NewRequestBehaviourBuilder(data *RequestBehaviour) RequestBehaviourBuilder {
	instance := &requestBehaviourBuilder{data: data}
	requestBehaviourBuilderTemplate(instance)
	return instance
}

func (this *requestBehaviourBuilder) SetWriter(w http.ResponseWriter) RequestBehaviourBuilder {
	meta.PanicIfTrue(w == nil, "SetWriter: writer cannot be nil")
	this.data.W = w
	return this
}

func (this *requestBehaviourBuilder) SetRequest(r *http.Request) RequestBehaviourBuilder {
	meta.PanicIfTrue(r == nil, "SetRequest: request cannot be nil")
	this.data.R = r
	return this
}

// Upon registers a handler for event via the untyped AddRaw path. Because Upon
// is non-generic, the event type is a runtime reflect.Type; the handler is
// bound to this request and stored under that key. Dispatch (ExecHandlers)
// keys on the event's dynamic type, so it lands on these handlers.
func (this *requestBehaviourBuilder) Upon(event events.EventType, fn events.NetworkingEventHandler[events.NetworkingEvent]) RequestBehaviourBuilder {
	AddRaw(this.data.Handlers, event, this.data.Bind(fn), HANDLER_MODE_POSTFIX)
	return this
}

// UponSpecialized is Upon with an explicit insertion mode.
func (this *requestBehaviourBuilder) UponSpecialized(event events.EventType, mode SpecializedHandlerMode, fn events.NetworkingEventHandler[events.NetworkingEvent]) RequestBehaviourBuilder {
	AddRaw(this.data.Handlers, event, this.data.Bind(fn), mode)
	return this
}

// CodeUpon registers a handler that writes statusCode when event fires.
func (this *requestBehaviourBuilder) CodeUpon(event events.EventType, statusCode int) RequestBehaviourBuilder {
	code := statusCode
	AddRaw(this.data.Handlers, event, this.data.Bind(
		func(w http.ResponseWriter, _ *http.Request, _ events.NetworkingEvent) error {
			w.WriteHeader(code)
			return nil
		},
	), HANDLER_MODE_POSTFIX)
	return this
}

func NewRequestData(w http.ResponseWriter, r *http.Request) *RequestBehaviour {
	return &RequestBehaviour{
		W:        w,
		R:        r,
		Handlers: NewHandlerMap(),
	}
}
