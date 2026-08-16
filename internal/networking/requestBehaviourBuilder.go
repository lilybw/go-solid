package networking

import (
	"fmt"
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

func (this *requestBehaviourBuilder) Upon(event events.EventType, fn events.NetworkingEventHandler[events.NetworkingEvent]) RequestBehaviourBuilder {
	AddRaw(this.data.Handlers, event, this.data.Bind(fn), HANDLER_MODE_POSTFIX)
	return this
}

func (this *requestBehaviourBuilder) UponSpecialized(event events.EventType, mode SpecializedHandlerMode, fn events.NetworkingEventHandler[events.NetworkingEvent]) RequestBehaviourBuilder {
	AddRaw(this.data.Handlers, event, this.data.Bind(fn), mode)
	return this
}

func (this *requestBehaviourBuilder) CodeUpon(event events.EventType, statusCode int) RequestBehaviourBuilder {
	code := statusCode
	rb := this.data
	AddRaw(this.data.Handlers, event, func(events.NetworkingEvent) error {
		rb.CommitStatus(code)
		return nil
	}, HANDLER_MODE_PREFIX)
	return this
}

func NewRequestData(w http.ResponseWriter, r *http.Request) *RequestBehaviour {
	rb := &RequestBehaviour{W: w, R: r, Handlers: NewHandlerMap()}
	// A default responder per concrete event type. Bind resolves W/R at dispatch
	// time, so a behaviour built with (nil, nil) by SetHTTPBehaviour still writes
	// to the real writer once ForRequest/SetWriter supplies one.
	for _, evType := range events.EVENTS.Values {
		handler := defaultHandlerFor(evType)
		if handler == nil {
			continue
		}
		AddRaw(rb.Handlers, evType, func(e events.NetworkingEvent) error {
			return handler(rb, e)
		}, HANDLER_MODE_REPLACE)
	}
	return rb
}

// defaultHandlerFor picks the default responder for a concrete event type, or
// nil for the SuccessEvent/FailureEvent buckets, which are reserved for
// user-supplied cross-cutting handlers.
func defaultHandlerFor(evType events.EventType) defaultResponder {
	switch {
	case evType == events.EVENTS.SuccessEvent || evType == events.EVENTS.FailureEvent:
		return nil
	case evType == events.EVENTS.TransmitRenderedTemplate:
		return defaultTransmitHandler
	case evType.Implements(events.EVENTS.FailureEvent):
		return defaultFailureHandler
	case evType.Implements(events.EVENTS.SuccessEvent):
		return defaultSuccessHandler
	default:
		return nil
	}
}

// defaultResponder is the built-in reply for one event type. It takes the
// behaviour rather than a bare writer so it reads W at dispatch time and routes
// the status through CommitStatus.
type defaultResponder func(rb *RequestBehaviour, event events.NetworkingEvent) error

// statusOf resolves the status an event asks for, falling back to the caller's
// default when the event carries none.
func statusOf(event events.NetworkingEvent, fallback int) int {
	return meta.Ternary(event.HTTPCode() == 0, fallback, int(event.HTTPCode()))
}

func defaultFailureHandler(rb *RequestBehaviour, event events.NetworkingEvent) error {
	if rb.W == nil {
		return fmt.Errorf("go_solid: failure event %T dispatched with no ResponseWriter bound", event)
	}
	body := fmt.Sprintf("go_solid: default failure handler received an event not castable "+
		"to events.FailureEvent. Observed: %v", event)
	if cast, ok := event.(events.FailureEvent); ok {
		body = cast.Err().Error()
	}
	// Status first: Write implicitly commits 200 and freezes the header.
	rb.CommitStatus(statusOf(event, http.StatusInternalServerError))
	_, err := rb.W.Write([]byte(body))
	return err
}

func defaultSuccessHandler(rb *RequestBehaviour, event events.NetworkingEvent) error {
	if rb.W == nil {
		return fmt.Errorf("go_solid: success event %T dispatched with no ResponseWriter bound", event)
	}
	if _, ok := event.(events.SuccessEvent); !ok {
		rb.CommitStatus(http.StatusInternalServerError)
		_, err := rb.W.Write([]byte("go_solid: default success handler received an event not " +
			"castable to events.SuccessEvent"))
		return err
	}
	rb.CommitStatus(statusOf(event, http.StatusOK))
	return nil
}

func defaultTransmitHandler(rb *RequestBehaviour, event events.NetworkingEvent) error {
	if rb.W == nil {
		return fmt.Errorf("go_solid: transmit event dispatched with no ResponseWriter bound")
	}
	cast, ok := event.(events.TransmitRenderedTemplateEvent)
	if !ok {
		rb.CommitStatus(http.StatusInternalServerError)
		_, err := rb.W.Write([]byte("go_solid: default TransmitRenderedTemplateEvent handler " +
			"received an event not castable to events.TransmitRenderedTemplateEvent"))
		return err
	}
	rb.W.Header().Set("Content-Type", "text/html; charset=utf-8")
	rb.CommitStatus(statusOf(event, http.StatusOK))
	_, err := rb.W.Write([]byte(cast.Rendered.HTML))
	return err
}
