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
// fire through Dispatch, which keys on the event's dynamic type.
func TestBuilderUponAndTypedAddDispatchTogether(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	data := NewRequestData(rec, req)

	var order []int

	// Builder path: interface-typed handler, keyed by a runtime EventType.
	NewRequestBehaviourBuilder(data).
		SetWriter(rec).
		SetRequest(req).
		Upon(events.EVENTS.PropsMarshalingFailure, func(http.ResponseWriter, *http.Request, events.NetworkingEvent) error {
			order = append(order, 1)
			return nil
		})

	// Typed path into the SAME map, same event type, no cast at the call site.
	data.Handlers.Add(func(PMF) error {
		order = append(order, 2)
		return nil
	}, shared.HANDLER_MODE_POSTFIX)

	// One slot per concrete event type (the default responders); the handlers
	// above land in the existing PropsMarshalingFailure slot, not a new one.
	if want := len(events.EVENTS.Concrete); data.Handlers.Len() != want {
		t.Fatalf("len = %d, want %d (one default responder per concrete event)",
			data.Handlers.Len(), want)
	}
	// One chain: the default responder, then the two handlers appended above.
	c, ok := data.Handlers.Get[PMF]()
	if !ok || len(c) != 3 {
		t.Fatalf("chain = %v, want one chain of 3 (default + 2 appended)", c)
	}

	if err := data.Dispatch(events.NewPropsMarshalingFailure(errors.New("x"))); err != nil {
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

	NewRequestBehaviourBuilder(data).
		CodeUpon(events.EVENTS.RegistryLookupFailure, http.StatusBadGateway)

	if err := data.Dispatch(events.NewRegistryLookupFailure(errors.New("nope"))); err != nil {
		t.Fatalf("dispatch err: %v", err)
	}
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadGateway)
	}
}

// A handler registered on a capability bucket runs for every event in it,
// after that event's own handlers.
func TestCategoryBucketHandlerRunsForEveryFailure(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	data := NewRequestData(rec, req)

	var seen int
	data.Handlers.Add(func(events.FailureEvent) error {
		seen++
		return nil
	}, shared.HANDLER_MODE_POSTFIX)

	for _, ev := range []events.NetworkingEvent{
		events.NewPropsMarshalingFailure(errors.New("a")),
		events.NewCompBundlingFailure(errors.New("b")),
	} {
		if err := data.Dispatch(ev); err != nil {
			t.Fatalf("dispatch err: %v", err)
		}
	}
	if seen != 2 {
		t.Fatalf("bucket handler ran %d times, want 2", seen)
	}
}
