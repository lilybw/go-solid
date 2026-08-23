package events

import (
	"net/http"
	"reflect"

	"github.com/lilybw/go-solid/internal/caching"
	"github.com/lilybw/go-solid/internal/meta"
)

// EventType identifies an event by its Go type.
type EventType = reflect.Type

type NetworkingEvent interface {
	HTTPCode() uint
}

type FailureEvent interface {
	NetworkingEvent
	Err() error
}

type DevelopmentFailureEvent interface {
	FailureEvent
	isDevelopmentFailureEvent()
}

type SuccessEvent interface {
	NetworkingEvent
	isSuccessEvent()
}

// --- embeddable bases -----------------------------------------------------

// networkingEventBase carries the status an event explicitly asks for. Zero
// means "no opinion": the responder picks the default for the category.
type networkingEventBase struct {
	httpCode uint
}

func (b networkingEventBase) HTTPCode() uint { return b.httpCode }

// failureBase is embedded by every failure event.
type failureBase struct {
	networkingEventBase
	Error error
}

func (f failureBase) Err() error { return f.Error }

// HTTPCode shadows networkingEventBase.HTTPCode: a failure must never report a
// 2xx just because no explicit code was attached to the event.
func (f failureBase) HTTPCode() uint {
	return meta.Ternary(f.httpCode == 0, 500, f.httpCode)
}

type developmentFailureEventBase struct {
	failureBase
}

func (developmentFailureEventBase) isDevelopmentFailureEvent() {}

// successBase is embedded by every success event.
type successBase struct {
	networkingEventBase
}

func (successBase) isSuccessEvent() {}

// --- FAILURE events -------------------------------------------------------

type PropsMarshalingFailureEvent struct{ failureBase }
type RegistryLookupFailureEvent struct{ failureBase }
type EntryGenerationFailureEvent struct{ failureBase }
type TempEntryWriteFailureEvent struct{ failureBase }
type CompBundlingFailureEvent struct{ failureBase }
type CompPropsInsufficientFailureEvent struct{ developmentFailureEventBase }

// Constructors keep the embedded Error ergonomic to set.

func NewPropsMarshalingFailure(err error) PropsMarshalingFailureEvent {
	return PropsMarshalingFailureEvent{failureBase{Error: err}}
}
func NewRegistryLookupFailure(err error) RegistryLookupFailureEvent {
	return RegistryLookupFailureEvent{failureBase{Error: err}}
}
func NewEntryGenerationFailure(err error) EntryGenerationFailureEvent {
	return EntryGenerationFailureEvent{failureBase{Error: err}}
}
func NewTempEntryWriteFailure(err error) TempEntryWriteFailureEvent {
	return TempEntryWriteFailureEvent{failureBase{Error: err}}
}
func NewCompBundlingFailure(err error) CompBundlingFailureEvent {
	return CompBundlingFailureEvent{failureBase{Error: err}}
}
func NewCompPropsInsufficientFailure(err error) CompPropsInsufficientFailureEvent {
	return CompPropsInsufficientFailureEvent{
		developmentFailureEventBase{
			failureBase{
				Error: err,
				networkingEventBase: networkingEventBase{
					httpCode: http.StatusInternalServerError,
				},
			},
		},
	}
}

// --- SUCCESS events -------------------------------------------------------

type TransmitRenderedTemplateEvent struct {
	successBase
	Rendered *caching.Rendered
}

func (b TransmitRenderedTemplateEvent) HTTPCode() uint {
	return meta.Ternary(b.httpCode == 0, 200, b.httpCode)
}

func NewTransmitRenderedTemplate(r *caching.Rendered) TransmitRenderedTemplateEvent {
	return TransmitRenderedTemplateEvent{Rendered: r}
}

// --- handler type ---------------------------------------------------------

// NetworkingEventHandler is a handler as the request builder takes it: not yet
// bound to a request, and keyed by a runtime EventType rather than a type
// parameter, so it receives the event as the interface.
//
// For a handler that receives its concrete event type, use HandlerMap.Add.
type NetworkingEventHandler func(w http.ResponseWriter, r *http.Request, event NetworkingEvent) error

// --- registry -------------------------------------------------------------

// EVENTS holds the reflect.Type of each event, for callers that need the type
// token without instantiating a value.
var EVENTS = struct {
	PropsMarshalingFailure       EventType
	RegistryLookupFailure        EventType
	EntryGenerationFailure       EventType
	TempEntryWriteFailure        EventType
	CompBundlingFailure          EventType
	TransmitRenderedTemplate     EventType
	CompPropsInsufficientFailure EventType

	FailureEvent EventType
	SuccessEvent EventType

	// Concrete is every event the library can actually emit. Each one gets a
	// built-in responder; ranging over this is how a new event type is picked
	// up automatically.
	Concrete []EventType

	// Categories are the capability buckets. They are never emitted: a handler
	// registered under one runs for every event implementing it, after that
	// event's own handlers. They carry no built-in responder, so they are free
	// for cross-cutting user handlers.
	Categories []EventType

	// Values is Concrete followed by Categories.
	Values []EventType
}{
	PropsMarshalingFailure:       reflect.TypeFor[PropsMarshalingFailureEvent](),
	RegistryLookupFailure:        reflect.TypeFor[RegistryLookupFailureEvent](),
	EntryGenerationFailure:       reflect.TypeFor[EntryGenerationFailureEvent](),
	TempEntryWriteFailure:        reflect.TypeFor[TempEntryWriteFailureEvent](),
	CompBundlingFailure:          reflect.TypeFor[CompBundlingFailureEvent](),
	TransmitRenderedTemplate:     reflect.TypeFor[TransmitRenderedTemplateEvent](),
	CompPropsInsufficientFailure: reflect.TypeFor[CompPropsInsufficientFailureEvent](),

	FailureEvent: reflect.TypeFor[FailureEvent](),
	SuccessEvent: reflect.TypeFor[SuccessEvent](),
}

func init() {
	EVENTS.Concrete = []EventType{
		EVENTS.PropsMarshalingFailure,
		EVENTS.RegistryLookupFailure,
		EVENTS.EntryGenerationFailure,
		EVENTS.TempEntryWriteFailure,
		EVENTS.CompBundlingFailure,
		EVENTS.CompPropsInsufficientFailure,
		EVENTS.TransmitRenderedTemplate,
	}
	EVENTS.Categories = []EventType{
		EVENTS.FailureEvent,
		EVENTS.SuccessEvent,
	}
	EVENTS.Values = append(append(make([]EventType, 0, len(EVENTS.Concrete)+len(EVENTS.Categories)),
		EVENTS.Concrete...), EVENTS.Categories...)
}
