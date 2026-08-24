package networking

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/lilybw/go-solid/shared/networking/events"
)

// DefectStage says where an inconsistency was caught.
type DefectStage string

const (
	// DEFECT_AT_REGISTRATION is a pairing that could never work, caught while
	// handlers are being registered and before any request exists.
	DEFECT_AT_REGISTRATION DefectStage = "registration"
	// DEFECT_AT_DISPATCH is an event reaching a handler that cannot accept it,
	// which means registration and dispatch disagree about the keying.
	DEFECT_AT_DISPATCH DefectStage = "dispatch"
)

// HandlerNarrowingDefect is what a handler wrapper panics with when it cannot
// narrow an event to the type it was registered for.
type HandlerNarrowingDefect struct {
	Stage DefectStage
	// RegisteredAs is the event type the handler was registered under. Nil when
	// the defect was caught at dispatch, where the dynamic type of the event is
	// by definition the type it was registered under.
	RegisteredAs events.EventType
	// Requires is the type the handler narrows the event to.
	Requires events.EventType
	// Received is the dynamic type of the event that arrived. Nil at
	// registration, where no event exists yet.
	Received events.EventType
}

func (this HandlerNarrowingDefect) Error() string {
	var b strings.Builder

	fmt.Fprintf(&b, "go_solid: internal defect in event handler wiring (caught at %s)\n\n", this.Stage)
	line := func(label string, value events.EventType) {
		if value == nil {
			return
		}
		fmt.Fprintf(&b, "    %-24s %v\n", label, value)
	}
	line("registered under:", this.RegisteredAs)
	line("handler narrows to:", this.Requires)
	line("event received:", this.Received)

	b.WriteString(`
A handler registered for a concrete event type is reached only through that
type, so the event arriving at one always has the type it was registered under.
That makes this impossible in a correctly wired package, and reaching it means
one of the following has drifted apart:

  - events.EVENTS.Concrete lists a type whose branch in defaultResponderFor
    does not match it
  - defaultResponderFor's branch order changed, so a narrow case is now
    shadowed by a broader one above it
  - RequestBehaviour.Dispatch no longer keys concrete handlers by
    reflect.TypeOf(event), or runs a capability bucket without first testing
    the event against it

This is a defect in go_solid rather than in your configuration, and it is wrong
for every request from the moment the process started. It fails here, loudly,
rather than surfacing later as one request's rendering error — or, worse, as an
empty response body with nothing logged.

Please report it, quoting this message in full.`)

	return b.String()
}

// NarrowableTo reports whether an event whose dynamic type is evType can be
// asserted to want.
func NarrowableTo(evType, want events.EventType) bool {
	if evType == nil || want == nil {
		return false
	}
	if want.Kind() == reflect.Interface {
		return evType.Implements(want)
	}
	return evType == want
}
