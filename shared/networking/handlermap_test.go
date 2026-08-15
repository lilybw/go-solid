package networking

import (
	"errors"
	"reflect"
	"sort"
	"testing"

	"github.com/lilybw/go-solid/shared/networking/events"
)

// --- helpers ------------------------------------------------------------

type PMF = events.PropsMarshalingFailureEvent
type RLF = events.RegistryLookupFailureEvent

func shapeOf[T events.NetworkingEvent](v HandlerMapValue[T]) (outer int, inner []int) {
	inner = make([]int, len(v))
	for i, g := range v {
		inner[i] = len(g)
	}
	return len(v), inner
}

func typedNoop[T events.NetworkingEvent]() RequestBoundHandler[T] {
	return func(T) error { return nil }
}

func typedTag(id int, order *[]int) RequestBoundHandler[PMF] {
	return func(PMF) error { *order = append(*order, id); return nil }
}

func ifaceTag(id int, order *[]int) RequestBoundHandler[events.NetworkingEvent] {
	return func(events.NetworkingEvent) error { *order = append(*order, id); return nil }
}

func runGroup0[T events.NetworkingEvent](t *testing.T, m *HandlerMap, ev T) {
	t.Helper()
	sv, ok := m.GetType(reflect.TypeFor[T]())
	if !ok || len(sv) == 0 {
		t.Fatalf("no group 0 for %v", reflect.TypeFor[T]())
	}
	for _, h := range sv[0] {
		if err := h(ev); err != nil {
			t.Fatalf("handler err: %v", err)
		}
	}
}

// --- headline: typed Add and untyped AddRaw share ONE slot ---------------

func TestTypedAndRawShareSlot(t *testing.T) {
	m := NewHandlerMap()
	var order []int

	Add(m, typedTag(1, &order), HANDLER_MODE_POSTFIX)                            // typed path
	AddRaw(m, reflect.TypeFor[PMF](), ifaceTag(2, &order), HANDLER_MODE_POSTFIX) // untyped path

	if m.Len() != 1 {
		t.Fatalf("len = %d, want 1 (shared slot)", m.Len())
	}
	sv, _ := m.GetType(reflect.TypeFor[PMF]())
	if len(sv) != 1 || len(sv[0]) != 2 {
		t.Fatalf("group0 = %v, want one chain of 2", sv)
	}

	// Typed reader still sees the slot (this was the regression before).
	tv, ok := Get[PMF](m)
	if !ok {
		t.Fatal("Get[PMF] must still find the slot after AddRaw")
	}
	if outer, inner := shapeOf(tv); outer != 1 || !reflect.DeepEqual(inner, []int{2}) {
		t.Fatalf("typed view = (%d,%v), want (1,[2])", outer, inner)
	}

	runGroup0(t, m, PMF{})
	if want := []int{1, 2}; !reflect.DeepEqual(order, want) {
		t.Fatalf("order = %v, want %v", order, want)
	}
}

// --- typed handler only ever sees its concrete T -------------------------

func TestTypedHandlerReceivesConcrete(t *testing.T) {
	m := NewHandlerMap()
	Add(m, func(e PMF) error {
		if e.Err() == nil {
			return errors.New("no err")
		}
		return e.Err()
	}, HANDLER_MODE_POSTFIX)

	sv, _ := m.GetType(reflect.TypeFor[PMF]())
	err := sv[0][0](events.NewPropsMarshalingFailure(errors.New("boom")))
	if err == nil || err.Error() != "boom" {
		t.Fatalf("err = %v, want boom", err)
	}
}

// --- no clobbering: raw-then-typed also coexists -------------------------

func TestRawThenTypedCoexist(t *testing.T) {
	m := NewHandlerMap()
	var order []int
	AddRaw(m, reflect.TypeFor[PMF](), ifaceTag(1, &order), HANDLER_MODE_POSTFIX)
	Add(m, typedTag(2, &order), HANDLER_MODE_POSTFIX)

	runGroup0(t, m, PMF{})
	if want := []int{1, 2}; !reflect.DeepEqual(order, want) {
		t.Fatalf("order = %v, want %v", order, want)
	}
}

// --- Add modes ------------------------------------------------------------

func TestAddModes(t *testing.T) {
	t.Run("postfix", func(t *testing.T) {
		m := NewHandlerMap()
		var o []int
		Add(m, typedTag(1, &o), HANDLER_MODE_POSTFIX)
		Add(m, typedTag(2, &o), HANDLER_MODE_POSTFIX)
		Add(m, typedTag(3, &o), HANDLER_MODE_POSTFIX)
		runGroup0(t, m, PMF{})
		if want := []int{1, 2, 3}; !reflect.DeepEqual(o, want) {
			t.Fatalf("got %v want %v", o, want)
		}
	})
	t.Run("prefix", func(t *testing.T) {
		m := NewHandlerMap()
		var o []int
		Add(m, typedTag(1, &o), HANDLER_MODE_PREFIX)
		Add(m, typedTag(2, &o), HANDLER_MODE_PREFIX)
		Add(m, typedTag(3, &o), HANDLER_MODE_PREFIX)
		runGroup0(t, m, PMF{})
		if want := []int{3, 2, 1}; !reflect.DeepEqual(o, want) {
			t.Fatalf("got %v want %v", o, want)
		}
	})
	t.Run("parallel", func(t *testing.T) {
		m := NewHandlerMap()
		Add(m, typedNoop[PMF](), HANDLER_MODE_PARALLEL)
		Add(m, typedNoop[PMF](), HANDLER_MODE_PARALLEL)
		Add(m, typedNoop[PMF](), HANDLER_MODE_PARALLEL)
		v, _ := Get[PMF](m)
		if outer, inner := shapeOf(v); outer != 3 || !reflect.DeepEqual(inner, []int{1, 1, 1}) {
			t.Fatalf("shape (%d,%v) want (3,[1 1 1])", outer, inner)
		}
	})
	t.Run("replace preserves parallel", func(t *testing.T) {
		m := NewHandlerMap()
		Add(m, typedNoop[PMF](), HANDLER_MODE_POSTFIX)
		Add(m, typedNoop[PMF](), HANDLER_MODE_PARALLEL)
		Add(m, typedNoop[PMF](), HANDLER_MODE_REPLACE)
		v, _ := Get[PMF](m)
		if outer, inner := shapeOf(v); outer != 2 || !reflect.DeepEqual(inner, []int{1, 1}) {
			t.Fatalf("shape (%d,%v) want (2,[1 1])", outer, inner)
		}
	})
	t.Run("invalid panics", func(t *testing.T) {
		m := NewHandlerMap()
		defer func() {
			if recover() == nil {
				t.Fatal("want panic")
			}
		}()
		Add(m, typedNoop[PMF](), HANDLER_MODE_INVALID)
	})
}

// --- distinct types, distinct slots --------------------------------------

func TestDistinctSlots(t *testing.T) {
	m := NewHandlerMap()
	Add(m, typedNoop[PMF](), HANDLER_MODE_POSTFIX)
	Add(m, typedNoop[RLF](), HANDLER_MODE_POSTFIX)
	Add(m, typedNoop[events.TransmitRenderedTemplateEvent](), HANDLER_MODE_POSTFIX)
	if m.Len() != 3 {
		t.Fatalf("len %d want 3", m.Len())
	}
	if !Has[PMF](m) || !Has[RLF](m) || !Has[events.TransmitRenderedTemplateEvent](m) {
		t.Fatal("missing slot")
	}
}

// --- Set / GetOrSet round-trip through wrapping ---------------------------

func TestSetRoundTrip(t *testing.T) {
	m := NewHandlerMap()
	Set(m, HandlerMapValue[PMF]{{typedNoop[PMF](), typedNoop[PMF]()}})
	v, ok := Get[PMF](m)
	if !ok {
		t.Fatal("absent after Set")
	}
	if outer, inner := shapeOf(v); outer != 1 || !reflect.DeepEqual(inner, []int{2}) {
		t.Fatalf("shape (%d,%v) want (1,[2])", outer, inner)
	}
}

func TestGetOrSet(t *testing.T) {
	m := NewHandlerMap()
	_, stored := GetOrSet(m, HandlerMapValue[PMF]{{typedNoop[PMF]()}})
	if !stored {
		t.Fatal("first GetOrSet should store")
	}
	_, stored = GetOrSet(m, HandlerMapValue[PMF]{{typedNoop[PMF]()}})
	if stored {
		t.Fatal("second should not store")
	}
}

// --- Has / Delete / Types / Clear ----------------------------------------

func TestHasDeleteTypesClear(t *testing.T) {
	m := NewHandlerMap()
	Add(m, typedNoop[PMF](), HANDLER_MODE_POSTFIX)
	Add(m, typedNoop[events.CompBundlingFailureEvent](), HANDLER_MODE_POSTFIX)

	if !Has[PMF](m) || !m.HasType(reflect.TypeFor[events.CompBundlingFailureEvent]()) {
		t.Fatal("Has wrong")
	}
	names := []string{}
	for _, tp := range m.Types() {
		names = append(names, tp.String())
	}
	sort.Strings(names)
	want := []string{"events.CompBundlingFailureEvent", "events.PropsMarshalingFailureEvent"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("Types %v want %v", names, want)
	}
	if !Delete[PMF](m) || Delete[PMF](m) {
		t.Fatal("Delete semantics wrong")
	}
	m.Clear()
	if m.Len() != 0 {
		t.Fatalf("len after clear %d", m.Len())
	}
}

// --- nil contract ---------------------------------------------------------

func TestNilContract(t *testing.T) {
	var m *HandlerMap
	if _, ok := Get[PMF](m); ok {
		t.Fatal("nil Get")
	}
	if Has[PMF](m) || m.HasType(reflect.TypeFor[PMF]()) {
		t.Fatal("nil Has")
	}
	if Delete[PMF](m) || m.DeleteType(reflect.TypeFor[PMF]()) {
		t.Fatal("nil Delete")
	}
	if m.Len() != 0 || m.Types() != nil {
		t.Fatal("nil Len/Types")
	}
	m.Clear()
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("nil Add should panic")
			}
		}()
		Add(m, typedNoop[PMF](), HANDLER_MODE_POSTFIX)
	}()
}

// --- dispatch: parallel groups + sequential chains + error stop -----------

func TestDispatchStored(t *testing.T) {
	m := NewHandlerMap()
	var mu = make(chan int, 16)

	// group 0 sequential: A then B
	Add(m, func(PMF) error { mu <- 1; return nil }, HANDLER_MODE_POSTFIX)
	Add(m, func(PMF) error { mu <- 2; return nil }, HANDLER_MODE_POSTFIX)
	// group 1 parallel: C
	Add(m, func(PMF) error { mu <- 3; return nil }, HANDLER_MODE_PARALLEL)

	sv, _ := m.GetType(reflect.TypeFor[PMF]())
	if err := dispatchStored(sv, PMF{}); err != nil {
		t.Fatalf("dispatch err %v", err)
	}
	close(mu)
	seen := map[int]bool{}
	for v := range mu {
		seen[v] = true
	}
	if !seen[1] || !seen[2] || !seen[3] {
		t.Fatalf("not all handlers ran: %v", seen)
	}
}

func TestDispatchSequentialStopsOnError(t *testing.T) {
	m := NewHandlerMap()
	ran := 0
	Add(m, func(PMF) error { ran++; return errors.New("stop") }, HANDLER_MODE_POSTFIX)
	Add(m, func(PMF) error { ran++; return nil }, HANDLER_MODE_POSTFIX) // same chain, after error

	sv, _ := m.GetType(reflect.TypeFor[PMF]())
	err := dispatchStored(sv, PMF{})
	if err == nil || err.Error() != "stop" {
		t.Fatalf("err = %v want stop", err)
	}
	if ran != 1 {
		t.Fatalf("ran = %d, want 1 (chain stops at first error)", ran)
	}
}
