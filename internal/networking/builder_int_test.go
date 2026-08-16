package networking

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	shared "github.com/lilybw/go-solid/shared/networking"
	"github.com/lilybw/go-solid/shared/networking/events"
)

type PMF = events.PropsMarshalingFailureEvent

// The builder's Upon and a direct typed Add land in the same slot and both
// fire through ExecHandlers, which keys on the event's dynamic type.
func TestBuilderUponAndTypedAddDispatchTogether(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	data := NewRequestData(rec, req)

	var order []int

	// Builder path (interface-typed handler, non-generic Upon).
	b := NewRequestBehaviourBuilder(data)
	b.SetWriter(rec).SetRequest(req).
		Upon(events.EVENTS.PropsMarshalingFailure, func(_ http.ResponseWriter, _ *http.Request, _ events.NetworkingEvent) error {
			order = append(order, 1)
			return nil
		})

	// Typed path (concrete handler) into the SAME map, same event type.
	shared.Add(data.Handlers, shared.RequestBoundHandler[PMF](func(PMF) error {
		order = append(order, 2)
		return nil
	}), shared.HANDLER_MODE_POSTFIX)

	// One slot, chain of two.
	// One slot per concrete event type (the default responders); the user handler
	// above lands in the existing PropsMarshalingFailure slot rather than a new one.
	wantSlots := 0
	for _, evType := range events.EVENTS.Values {
		if evType != events.EVENTS.SuccessEvent && evType != events.EVENTS.FailureEvent {
			wantSlots++
		}
	}
	if data.Handlers.Len() != wantSlots {
		t.Fatalf("len = %d, want %d (one default responder per concrete event)",
			data.Handlers.Len(), wantSlots)
	}
	// One chain: the default responder, then the two handlers appended above.
	sv, ok := data.Handlers.GetType(reflect.TypeFor[PMF]())
	if !ok || len(sv) != 1 || len(sv[0]) != 3 {
		t.Fatalf("group0 = %v, want one chain of 3 (default + 2 appended)", sv)
	}

	// Dispatch by dynamic type.
	ev := events.NewPropsMarshalingFailure(errors.New("x"))
	if err := shared.ExecHandlers(data, ev); err != nil {
		t.Fatalf("dispatch err: %v", err)
	}
	if want := []int{1, 2}; !reflect.DeepEqual(order, want) {
		t.Fatalf("order = %v, want %v", order, want)
	}
}

// CodeUpon writes the status code when the event fires.
func TestCodeUpon(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	data := NewRequestData(rec, req)

	b := NewRequestBehaviourBuilder(data)
	b.CodeUpon(events.EVENTS.RegistryLookupFailure, http.StatusBadGateway)

	ev := events.NewRegistryLookupFailure(errors.New("nope"))
	if err := shared.ExecHandlers(data, ev); err != nil {
		t.Fatalf("dispatch err: %v", err)
	}
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadGateway)
	}
}

// UponSpecialized honors the mode (PARALLEL creates a second group).
func TestUponSpecializedMode(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	data := NewRequestData(rec, req)

	b := NewRequestBehaviourBuilder(data)
	b.Upon(events.EVENTS.PropsMarshalingFailure, func(http.ResponseWriter, *http.Request, events.NetworkingEvent) error { return nil })
	b.UponSpecialized(events.EVENTS.PropsMarshalingFailure, shared.HANDLER_MODE_PARALLEL,
		func(http.ResponseWriter, *http.Request, events.NetworkingEvent) error { return nil })

	sv, _ := data.Handlers.GetType(reflect.TypeFor[PMF]())
	if len(sv) != 2 {
		t.Fatalf("outer groups = %d, want 2 (PARALLEL added a group)", len(sv))
	}
}
