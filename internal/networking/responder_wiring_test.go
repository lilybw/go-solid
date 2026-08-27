package networking

import (
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"

	shared "github.com/lilybw/go-solid/shared/networking"
	"github.com/lilybw/go-solid/shared/networking/events"
)

// The wiring behind the default responders
// ---------------------------------------------------------------------------
// Three separate facts have to agree for a built-in responder to work: the type
// is listed in EVENTS.Concrete, defaultResponderFor picks a responder whose
// narrowing that type satisfies, and Dispatch keys concrete handlers by the
// event's own dynamic type. Nothing in the language ties them together, so the
// tests below tie them instead — an event added without a matching branch fails
// here rather than at a consumer's boot, or on the first request that emits it.

// Every emittable event must get a responder it can actually be narrowed to.
// This is the check that catches a new event type added to EVENTS.Concrete
// without a branch that fits it.
func TestEveryConcreteEventHasAResponderItSatisfies(t *testing.T) {
	for _, evType := range events.EVENTS.Concrete {
		responder, ok := defaultResponderFor(evType)
		if !ok {
			t.Errorf("%v is emittable but has no default responder, so emitting it writes nothing", evType)
			continue
		}
		if responder.requires == nil {
			t.Errorf("%v has a responder that declares no narrowing", evType)
			continue
		}
		if !shared.NarrowableTo(evType, responder.requires) {
			t.Errorf("%v is registered under a responder that narrows to %v, which it does not satisfy",
				evType, responder.requires)
		}
	}
}

// The capability buckets are reserved for cross-cutting user handlers. A bucket
// carrying a built-in responder would write a second body for every event in
// it, on top of the one that event's own responder already wrote.
//
// An interface implements itself, so every bucket satisfies one of the
// Implements branches; the guard at the top of defaultResponderFor is what
// keeps them out.
func TestCategoryBucketsGetNoDefaultResponder(t *testing.T) {
	for _, category := range events.EVENTS.Categories {
		if _, ok := defaultResponderFor(category); ok {
			t.Errorf("the %v bucket has a built-in responder; it is meant to stay free for user handlers", category)
		}
	}
}

// The same thing said about behaviour rather than about the helper: whatever
// defaultResponderFor decides, nothing may end up registered under a bucket.
func TestNewRequestDataRegistersNothingUnderABucket(t *testing.T) {
	data, _ := newBoundRequestData(t)

	empty := func(name string, chains shared.Chain, ok bool) {
		t.Helper()
		if ok && len(chains) > 0 {
			t.Errorf("%s has %d handler chain(s) registered by default; the bucket is meant to stay empty",
				name, len(chains))
		}
	}
	// Get is keyed by type parameter rather than by value, so the buckets are
	// named one at a time.
	chains, ok := data.Handlers.Get[events.DevelopmentFailureEvent]()
	empty("DevelopmentFailureEvent", chains, ok)
	chainsa, oka := data.Handlers.Get[events.FailureEvent]()
	empty("FailureEvent", chainsa, oka)
	chainsb, okb := data.Handlers.Get[events.SuccessEvent]()
	empty("SuccessEvent", chainsb, okb)
}

// Branch order decides which responder a type gets, and a broader case placed
// above a narrower one silently swallows it. A development failure is also a
// failure, so FailureEvent is the case that would swallow it.
//
// What matters is that it does not end up on the generic failure responder, not
// which of the narrower ones it lands on — those are interchangeable, and
// pinning one here would break every time the taxonomy grew a level.
func TestBranchOrderKeepsTheNarrowestResponder(t *testing.T) {
	responder, ok := defaultResponderFor(events.EVENTS.CompPropsInsufficientFailure)
	if !ok {
		t.Fatal("a development failure has no responder")
	}
	if responder.requires == events.EVENTS.FailureEvent {
		t.Error("a development failure resolved to the generic failure responder; " +
			"a broader case above it is shadowing the specific one")
	}
	if !shared.NarrowableTo(events.EVENTS.CompPropsInsufficientFailure, responder.requires) {
		t.Errorf("a development failure resolved to a responder narrowing to %v, which it does not satisfy",
			responder.requires)
	}
}

// ---------------------------------------------------------------------------
// Failing loudly.
// ---------------------------------------------------------------------------

func defectFrom(t *testing.T, fn func()) shared.HandlerNarrowingDefect {
	t.Helper()

	var caught any
	func() {
		defer func() { caught = recover() }()
		fn()
	}()
	if caught == nil {
		t.Fatal("no panic; an impossible wiring was accepted")
	}
	defect, ok := caught.(shared.HandlerNarrowingDefect)
	if !ok {
		t.Fatalf("panicked with %T (%v), want a HandlerNarrowingDefect", caught, caught)
	}
	return defect
}

// A pairing that could never work is caught while handlers are registered,
// before any request exists. That is what makes this fail fast: the process
// dies at New rather than on whichever request first emits the event.
func TestAMispairedResponderPanicsAtRegistration(t *testing.T) {
	defect := defectFrom(t, func() {
		// A failure event paired with the success narrowing: the shape a
		// mis-ordered branch in defaultResponderFor would produce.
		assertNarrowable(
			reflect.TypeFor[events.PropsMarshalingFailureEvent](),
			events.EVENTS.SuccessEvent,
		)
	})

	if defect.Stage != shared.DEFECT_AT_REGISTRATION {
		t.Errorf("Stage = %q, want %q", defect.Stage, shared.DEFECT_AT_REGISTRATION)
	}
	for _, want := range []string{"PropsMarshalingFailureEvent", "SuccessEvent", "registration"} {
		if !strings.Contains(defect.Error(), want) {
			t.Errorf("the message does not name %q:\n%s", want, defect.Error())
		}
	}
}

// The message is the whole point: a defect nobody can act on is a crash with
// extra steps. It has to say what was registered, what was expected, where it
// was caught, and that the fault is the library's rather than the consumer's.
func TestTheDefectMessageSaysEnoughToActOn(t *testing.T) {
	defect := shared.HandlerNarrowingDefect{
		Stage:        shared.DEFECT_AT_DISPATCH,
		RegisteredAs: reflect.TypeFor[events.PropsMarshalingFailureEvent](),
		Requires:     events.EVENTS.SuccessEvent,
		Received:     reflect.TypeFor[events.RegistryLookupFailureEvent](),
	}

	message := defect.Error()
	for _, want := range []string{
		"PropsMarshalingFailureEvent", // registered under
		"SuccessEvent",                // narrows to
		"RegistryLookupFailureEvent",  // received
		"dispatch",                    // where
		"EVENTS.Concrete",             // what to look at
		"defaultResponderFor",
		"defect in go_solid", // whose fault
		"every request",      // and its scope
	} {
		if !strings.Contains(message, want) {
			t.Errorf("the message does not mention %q:\n%s", want, message)
		}
	}
}

// Fields absent at a given stage are left out rather than printed empty.
func TestTheDefectMessageOmitsWhatItDoesNotKnow(t *testing.T) {
	message := shared.HandlerNarrowingDefect{
		Stage:        shared.DEFECT_AT_REGISTRATION,
		RegisteredAs: reflect.TypeFor[events.PropsMarshalingFailureEvent](),
		Requires:     events.EVENTS.SuccessEvent,
	}.Error()

	if strings.Contains(message, "event received:") {
		t.Errorf("a registration defect claims to have received an event:\n%s", message)
	}
}

// ---------------------------------------------------------------------------
// The responders still behave.
// ---------------------------------------------------------------------------

// Removing the swallow from the failure responder must not have removed the
// body with it: a failure that writes nothing is an empty 500 with no reason in
// it and nothing in the log.
func TestAFailureAlwaysWritesItsReason(t *testing.T) {
	for name, event := range map[string]events.NetworkingEvent{
		"plain failure":       events.NewPropsMarshalingFailure(errors.New("marshal blew up")),
		"development failure": events.NewCompPropsInsufficientFailure(errors.New("props do not fit")),
	} {
		t.Run(name, func(t *testing.T) {
			data, rec := newBoundRequestData(t)
			if err := data.Dispatch(event); err != nil {
				t.Fatalf("Dispatch: %v", err)
			}
			if rec.Code == http.StatusOK {
				t.Errorf("status = %d; a failure must not look like a page", rec.Code)
			}
			if rec.Body.Len() == 0 {
				t.Error("nothing was written, so the caller sees an empty response with no reason")
			}
		})
	}
}

// Every emittable event, dispatched for real, through the wiring the checks
// above only inspect statically.
func TestEveryConcreteEventDispatchesWithoutDefect(t *testing.T) {
	for name, event := range map[string]events.NetworkingEvent{
		"props marshaling":   events.NewPropsMarshalingFailure(errors.New("x")),
		"registry lookup":    events.NewRegistryLookupFailure(errors.New("x")),
		"entry generation":   events.NewEntryGenerationFailure(errors.New("x")),
		"temp entry write":   events.NewTempEntryWriteFailure(errors.New("x")),
		"comp bundling":      events.NewCompBundlingFailure(errors.New("x")),
		"props insufficient": events.NewCompPropsInsufficientFailure(errors.New("x")),
		"transmit":           events.NewTransmitRenderedTemplate(rendered("<div/>")),
	} {
		t.Run(name, func(t *testing.T) {
			data, rec := newBoundRequestData(t)
			if err := data.Dispatch(event); err != nil {
				t.Fatalf("Dispatch: %v", err)
			}
			if rec.Code == 0 {
				t.Error("no status was committed")
			}
		})
	}
}

// A writer is never bound when the caller takes the rendered output itself, and
// the responders stand down rather than narrowing an event they will not answer.
func TestNoWriterMeansNoNarrowingAtAll(t *testing.T) {
	data := NewRequestData(nil, nil)
	for _, event := range []events.NetworkingEvent{
		events.NewPropsMarshalingFailure(errors.New("x")),
		events.NewTransmitRenderedTemplate(rendered("<div/>")),
	} {
		if err := data.Dispatch(event); err != nil {
			t.Errorf("dispatching %T without a writer: %v", event, err)
		}
	}
}
