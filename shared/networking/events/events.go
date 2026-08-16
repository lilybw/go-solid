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

type networkingEventBase struct {
	httpCode uint
}

// HTTPCode shadows networkingEventBase.HTTPCode: a failure must never report a
// 2xx just because no explicit code was attached to the event.
func (f failureBase) HTTPCode() uint {
	return meta.Ternary(f.httpCode == 0, 500, f.httpCode)
}

type FailureEvent interface {
	NetworkingEvent
	Err() error
}

type SuccessEvent interface {
	NetworkingEvent
	isSuccessEvent()
}

// --- embeddable bases -----------------------------------------------------

type failureBase struct {
	networkingEventBase
	Error error
}

func (f failureBase) Err() error { return f.Error }

// successBase is embedded by every success event.
type successBase struct {
	networkingEventBase
}

func (successBase) isSuccessEvent() {}

// --- FAILURE events -------------------------------------------------------
//

type PropsMarshalingFailureEvent struct{ failureBase }
type RegistryLookupFailureEvent struct{ failureBase }
type EntryGenerationFailureEvent struct{ failureBase }
type TempEntryWriteFailureEvent struct{ failureBase }
type CompBundlingFailureEvent struct{ failureBase }

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

type NetworkingEventHandler[T NetworkingEvent] func(w http.ResponseWriter, r *http.Request, event T) error

// --- registry -------------------------------------------------------------

// EVENTS holds the reflect.Type of each event, for callers that need the type
// token without instantiating a value.
var EVENTS = struct {
	PropsMarshalingFailure   EventType
	RegistryLookupFailure    EventType
	EntryGenerationFailure   EventType
	TempEntryWriteFailure    EventType
	CompBundlingFailure      EventType
	TransmitRenderedTemplate EventType

	FailureEvent EventType
	SuccessEvent EventType

	Values []reflect.Type
}{
	PropsMarshalingFailure:   reflect.TypeFor[PropsMarshalingFailureEvent](),
	RegistryLookupFailure:    reflect.TypeFor[RegistryLookupFailureEvent](),
	EntryGenerationFailure:   reflect.TypeFor[EntryGenerationFailureEvent](),
	TempEntryWriteFailure:    reflect.TypeFor[TempEntryWriteFailureEvent](),
	CompBundlingFailure:      reflect.TypeFor[CompBundlingFailureEvent](),
	TransmitRenderedTemplate: reflect.TypeFor[TransmitRenderedTemplateEvent](),

	// Comes with universal fallback handlers
	FailureEvent: reflect.TypeFor[FailureEvent](),
	SuccessEvent: reflect.TypeFor[SuccessEvent](),
}

func init() {
	EVENTS.Values = []reflect.Type{
		EVENTS.PropsMarshalingFailure,
		EVENTS.RegistryLookupFailure,
		EVENTS.EntryGenerationFailure,
		EVENTS.TempEntryWriteFailure,
		EVENTS.CompBundlingFailure,
		EVENTS.TransmitRenderedTemplate,
		EVENTS.FailureEvent,
		EVENTS.SuccessEvent,
	}
}
