package networking

import (
	"fmt"
	"net/http"
	"reflect"

	"github.com/lilybw/go-solid/internal/meta"
	. "github.com/lilybw/go-solid/shared/networking"
	"github.com/lilybw/go-solid/shared/networking/events"
)

type requestBehaviourBuilder struct {
	data *RequestBehaviour
}

// NewRequestBehaviourBuilder returns a builder carrying no consumer defaults.
// For one with a Bundler's configured request template applied, use
// Defaults.NewRequestBehaviourBuilder.
func NewRequestBehaviourBuilder(data *RequestBehaviour) RequestBehaviourBuilder {
	return &requestBehaviourBuilder{data: data}
}

func (this *requestBehaviourBuilder) SetWriter(w http.ResponseWriter) RequestBehaviourBuilder {
	meta.PanicIfTrue(w == nil, "SetWriter: writer cannot be nil")
	this.data.BindWriter(w)
	return this
}

func (this *requestBehaviourBuilder) SetRequest(r *http.Request) RequestBehaviourBuilder {
	meta.PanicIfTrue(r == nil, "SetRequest: request cannot be nil")
	this.data.R = r
	return this
}

func (this *requestBehaviourBuilder) Upon(event events.EventType, fn events.NetworkingEventHandler) RequestBehaviourBuilder {
	return this.UponSpecialized(event, HANDLER_MODE_POSTFIX, fn)
}

func (this *requestBehaviourBuilder) UponSpecialized(event events.EventType, mode HandlerMode, fn events.NetworkingEventHandler) RequestBehaviourBuilder {
	this.data.Handlers.AddType(event, this.data.Bind(fn), mode)
	return this
}

func (this *requestBehaviourBuilder) CodeUpon(event events.EventType, statusCode int) RequestBehaviourBuilder {
	rb := this.data
	rb.Handlers.AddType(event, func(events.NetworkingEvent) error {
		rb.CommitStatus(statusCode)
		return nil
	}, HANDLER_MODE_PREFIX)
	return this
}

func NewRequestData(w http.ResponseWriter, r *http.Request) *RequestBehaviour {
	rb := &RequestBehaviour{R: r, Handlers: NewHandlerMap()}
	rb.BindWriter(w)
	// A default responder per concrete event type. Ranging over EVENTS.Concrete
	// means a newly declared event gets one for free. Bind resolves W/R at
	// dispatch time, so a behaviour built with (nil, nil) by SetHTTPBehaviour
	// still writes to the real writer once ForRequest/SetWriter supplies one.
	for _, evType := range events.EVENTS.Concrete {
		responder := defaultResponderFor(evType)
		if responder == nil {
			continue
		}
		rb.Handlers.AddType(evType, func(e events.NetworkingEvent) error {
			if rb.W == nil {
				// No writer was ever bound. SetHTTPBehaviour without
				// ForRequest is a supported way to render — the caller takes
				// the HTML and writes it itself — so there is simply no
				// response for a built-in responder to write. A configuration,
				// not a fault.
				return nil
			}
			return responder(rb, e)
		}, HANDLER_MODE_REPLACE)
	}
	return rb
}

// defaultResponder is the built-in reply for one event type. It takes the
// behaviour rather than a bare writer so it reads W at dispatch time and routes
// the status through CommitStatus.
type defaultResponder func(rb *RequestBehaviour, event events.NetworkingEvent) error

// defaultResponderFor picks the built-in responder for a concrete event type.
// The capability buckets in events.EVENTS.Categories deliberately get none:
// they are reserved for cross-cutting user handlers.
// Order matters: the concrete cases come first. A development failure also
// implements FailureEvent, so behind the category case its own responder would
// never be reached.
func defaultResponderFor(evType events.EventType) defaultResponder {
	switch {
	case evType == events.EVENTS.TransmitRenderedTemplate:
		return defaultTransmitHandler
	case evType == events.EVENTS.CompPropsInsufficientFailure:
		return func(rb *RequestBehaviour, event events.NetworkingEvent) error {
			cast, err := require[events.CompPropsInsufficientFailureEvent](event)
			if err != nil {
				return err
			}
			rb.CommitStatus(statusOf(event, http.StatusInternalServerError))
			_, err = rb.W.Write([]byte(cast.Err().Error()))
			return err
		}
	case evType.Implements(events.EVENTS.FailureEvent):
		return defaultFailureHandler
	case evType.Implements(events.EVENTS.SuccessEvent):
		return defaultSuccessHandler
	default:
		return nil
	}
}

// statusOf resolves the status an event asks for, falling back to the caller's
// default when the event carries none.
func statusOf(event events.NetworkingEvent, fallback int) int {
	return meta.Ternary(event.HTTPCode() == 0, fallback, int(event.HTTPCode()))
}

// require narrows event to the type its responder was registered for. Failing
// it means defaultResponderFor mispicked, which is a bug in this package: it
// reports an error rather than writing a diagnostic into the user's response
// body, where it would masquerade as the page.
//
// An absent writer is not its concern; NewRequestData stops the responder
// before it gets here.
func require[T events.NetworkingEvent](event events.NetworkingEvent) (T, error) {
	var zero T
	typed, ok := event.(T)
	if !ok {
		return zero, fmt.Errorf("go_solid: default handler for %v received %T", reflect.TypeFor[T](), event)
	}
	return typed, nil
}

func defaultFailureHandler(rb *RequestBehaviour, event events.NetworkingEvent) error {
	cast, err := require[events.FailureEvent](event)
	if err != nil { // not a failure event after all; still honour any code it carries
		code := meta.Ternary(int(event.HTTPCode()) != 0, int(event.HTTPCode()), http.StatusInternalServerError)
		rb.CommitStatus(code) // still want the code if present though
		return nil
	}
	// Status first: Write implicitly commits 200 and freezes the header.
	rb.CommitStatus(statusOf(event, http.StatusInternalServerError))
	_, err = rb.W.Write([]byte(cast.Err().Error()))
	return err
}

func defaultSuccessHandler(rb *RequestBehaviour, event events.NetworkingEvent) error {
	if _, err := require[events.SuccessEvent](event); err != nil {
		return err
	}
	rb.CommitStatus(statusOf(event, http.StatusOK))
	return nil
}

func defaultTransmitHandler(rb *RequestBehaviour, event events.NetworkingEvent) error {
	cast, err := require[events.TransmitRenderedTemplateEvent](event)
	if err != nil {
		return err
	}
	rb.W.Header().Set("Content-Type", "text/html; charset=utf-8")
	rb.CommitStatus(statusOf(event, http.StatusOK))
	_, err = rb.W.Write([]byte(cast.Rendered.HTML))
	return err
}
