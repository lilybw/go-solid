package networking

import (
	"net/http"
	"reflect"

	"github.com/lilybw/go-solid/internal/meta"
	. "github.com/lilybw/go-solid/shared/networking"
	"github.com/lilybw/go-solid/shared/networking/events"
)

func NewRequestData(w http.ResponseWriter, r *http.Request) *RequestBehaviour {
	rb := &RequestBehaviour{R: r, Handlers: NewHandlerMap()}
	rb.BindWriter(w)
	// A default responder per concrete event type. Ranging over EVENTS.Concrete
	// means a newly declared event gets one for free. Bind resolves W/R at
	// dispatch time, so a behaviour built with (nil, nil) by SetHTTPBehaviour
	// still writes to the real writer once ForRequest/SetWriter supplies one.
	for _, evType := range events.EVENTS.Concrete {
		responder, ok := defaultResponderFor(evType)
		if !ok {
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
			return responder.respond(rb, e)
		}, HANDLER_MODE_REPLACE)
	}
	return rb
}

type defaultResponder struct {
	// requires is the type respond asserts the event to. An interface here
	// means "anything implementing it"; a concrete type means exactly that one.
	requires events.EventType
	respond  func(rb *RequestBehaviour, event events.NetworkingEvent) error
}

func defaultResponderFor(evType events.EventType) (defaultResponder, bool) {
	if evType == nil || evType.Kind() == reflect.Interface {
		return defaultResponder{}, false
	}
	switch {
	case evType == events.EVENTS.TransmitRenderedTemplate:
		return defaultResponder{
			requires: events.EVENTS.TransmitRenderedTemplate,
			respond:  defaultTransmitHandler,
		}, true
	case evType == events.EVENTS.CompPropsInsufficientFailure:
		return defaultResponder{
			requires: events.EVENTS.DevelopmentFailureEvent,
			respond:  defaultDevelopmentFailureHandler,
		}, true
	case evType.Implements(events.EVENTS.FailureEvent):
		return defaultResponder{
			requires: events.EVENTS.FailureEvent,
			respond:  defaultFailureHandler,
		}, true
	case evType.Implements(events.EVENTS.SuccessEvent):
		return defaultResponder{
			requires: events.EVENTS.SuccessEvent,
			respond:  defaultSuccessHandler,
		}, true
	default:
		return meta.Zero[defaultResponder](), false
	}
}

func assertNarrowable(evType, requires events.EventType) {
	if NarrowableTo(evType, requires) {
		return
	}
	panic(HandlerNarrowingDefect{
		Stage:        DEFECT_AT_REGISTRATION,
		RegisteredAs: evType,
		Requires:     requires,
	})
}

// statusOf resolves the status an event asks for, falling back to the caller's
// default when the event carries none.
func statusOf(event events.NetworkingEvent, fallback int) int {
	return meta.Ternary(event.HTTPCode() == 0, fallback, int(event.HTTPCode()))
}

// Assumptions are DANGEROUS
func assume[T events.NetworkingEvent](event events.NetworkingEvent) T {
	typed, ok := event.(T)
	if !ok {
		panic(HandlerNarrowingDefect{
			Stage:    DEFECT_AT_DISPATCH,
			Requires: reflect.TypeFor[T](),

			Received: reflect.TypeOf(event),
		})
	}
	return typed
}

func defaultFailureHandler(rb *RequestBehaviour, event events.NetworkingEvent) error {
	cast := assume[events.FailureEvent](event)
	// Status first: Write implicitly commits 200 and freezes the header.
	rb.CommitStatus(statusOf(event, http.StatusInternalServerError))
	_, err := rb.W.Write([]byte(cast.Err().Error()))
	return err
}

func defaultDevelopmentFailureHandler(rb *RequestBehaviour, event events.NetworkingEvent) error {
	cast := assume[events.CompPropsInsufficientFailureEvent](event)
	rb.CommitStatus(statusOf(event, http.StatusInternalServerError))
	_, err := rb.W.Write([]byte(cast.Err().Error()))
	return err
}

func defaultSuccessHandler(rb *RequestBehaviour, event events.NetworkingEvent) error {
	rb.CommitStatus(statusOf(event, http.StatusOK))
	return nil
}

func defaultTransmitHandler(rb *RequestBehaviour, event events.NetworkingEvent) error {
	cast := assume[events.TransmitRenderedTemplateEvent](event)
	rb.W.Header().Set("Content-Type", "text/html; charset=utf-8")
	rb.CommitStatus(statusOf(event, http.StatusOK))
	_, err := rb.W.Write([]byte(cast.Rendered.HTML))
	return err
}
