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

func shapeOf(c Chains) (chains int, lengths []int) {
	lengths = make([]int, len(c))
	for i, chain := range c {
		lengths[i] = len(chain)
	}
	return len(c), lengths
}

func noopHandler[T events.NetworkingEvent]() func(T) error {
	return func(T) error { return nil }
}

func tag(id int, order *[]int) func(PMF) error {
	return func(PMF) error { *order = append(*order, id); return nil }
}

func rawTag(id int, order *[]int) Handler {
	return func(events.NetworkingEvent) error { *order = append(*order, id); return nil }
}

func runPrimary[T events.NetworkingEvent](t *testing.T, m *HandlerMap, ev T) {
	t.Helper()
	c, ok := m.Get[T]()
	if !ok || len(c) == 0 {
		t.Fatalf("no chains stored for %T", ev)
	}
	if err := c[0].Run(ev); err != nil {
		t.Fatalf("chain returned %v", err)
	}
}

// --- headline: Add and AddType share ONE slot ---------------------------

func TestTypedAndUntypedShareSlot(t *testing.T) {
	m := NewHandlerMap()
	var order []int

	m.Add(tag(1, &order), HANDLER_MODE_POSTFIX)                                // typed path, T inferred
	m.AddType(reflect.TypeFor[PMF](), rawTag(2, &order), HANDLER_MODE_POSTFIX) // untyped path

	if m.Len() != 1 {
		t.Fatalf("len = %d, want 1 (shared slot)", m.Len())
	}
	c, ok := m.Get[PMF]()
	if !ok {
		t.Fatal("Get[PMF] must find the slot AddType wrote to")
	}
	if chains, lengths := shapeOf(c); chains != 1 || !reflect.DeepEqual(lengths, []int{2}) {
		t.Fatalf("shape = (%d,%v), want (1,[2])", chains, lengths)
	}

	runPrimary(t, m, PMF{})
	if want := []int{1, 2}; !reflect.DeepEqual(order, want) {
		t.Fatalf("order = %v, want %v", order, want)
	}
}

// --- typed handler only ever sees its concrete T -------------------------

func TestTypedHandlerReceivesConcrete(t *testing.T) {
	m := NewHandlerMap()
	m.Add(func(e PMF) error {
		if e.Err() == nil {
			return errors.New("handler received an event with no error attached")
		}
		return e.Err()
	}, HANDLER_MODE_POSTFIX)

	c, _ := m.Get[PMF]()
	err := c[0].Run(events.NewPropsMarshalingFailure(errors.New("boom")))
	if err == nil || err.Error() != "boom" {
		t.Fatalf("err = %v, want boom", err)
	}
}

// A capability bucket is a legal T: one typed handler for every failure.
func TestTypedAddOnCategoryBucket(t *testing.T) {
	m := NewHandlerMap()
	var seen []string
	m.Add(func(e events.FailureEvent) error {
		seen = append(seen, e.Err().Error())
		return nil
	}, HANDLER_MODE_POSTFIX)

	if !m.Has[events.FailureEvent]() {
		t.Fatal("handler was not filed under the FailureEvent bucket")
	}
	c, _ := m.Get[events.FailureEvent]()
	for _, ev := range []events.NetworkingEvent{
		events.NewPropsMarshalingFailure(errors.New("a")),
		events.NewCompBundlingFailure(errors.New("b")),
	} {
		if err := c[0].Run(ev); err != nil {
			t.Fatalf("chain returned %v", err)
		}
	}
	if want := []string{"a", "b"}; !reflect.DeepEqual(seen, want) {
		t.Fatalf("seen = %v, want %v", seen, want)
	}
}

// A typed handler that somehow receives the wrong event reports an error
// rather than panicking the request goroutine.
func TestTypedHandlerMismatchIsAnError(t *testing.T) {
	m := NewHandlerMap()
	m.Add(func(PMF) error { return nil }, HANDLER_MODE_POSTFIX)

	c, _ := m.Get[PMF]()
	if err := c[0].Run(events.NewRegistryLookupFailure(errors.New("x"))); err == nil {
		t.Fatal("mismatched event produced no error")
	}
}

// --- Add modes ------------------------------------------------------------

func TestAddModes(t *testing.T) {
	t.Run("postfix", func(t *testing.T) {
		m := NewHandlerMap()
		var o []int
		m.Add(tag(1, &o), HANDLER_MODE_POSTFIX).
			Add(tag(2, &o), HANDLER_MODE_POSTFIX).
			Add(tag(3, &o), HANDLER_MODE_POSTFIX)
		runPrimary(t, m, PMF{})
		if want := []int{1, 2, 3}; !reflect.DeepEqual(o, want) {
			t.Fatalf("got %v want %v", o, want)
		}
	})
	t.Run("prefix", func(t *testing.T) {
		m := NewHandlerMap()
		var o []int
		m.Add(tag(1, &o), HANDLER_MODE_PREFIX).
			Add(tag(2, &o), HANDLER_MODE_PREFIX).
			Add(tag(3, &o), HANDLER_MODE_PREFIX)
		runPrimary(t, m, PMF{})
		if want := []int{3, 2, 1}; !reflect.DeepEqual(o, want) {
			t.Fatalf("got %v want %v", o, want)
		}
	})
	t.Run("parallel", func(t *testing.T) {
		m := NewHandlerMap()
		m.Add(noopHandler[PMF](), HANDLER_MODE_PARALLEL).
			Add(noopHandler[PMF](), HANDLER_MODE_PARALLEL).
			Add(noopHandler[PMF](), HANDLER_MODE_PARALLEL)
		c, _ := m.Get[PMF]()
		if chains, lengths := shapeOf(c); chains != 3 || !reflect.DeepEqual(lengths, []int{1, 1, 1}) {
			t.Fatalf("shape (%d,%v) want (3,[1 1 1])", chains, lengths)
		}
	})
	t.Run("replace preserves parallel chains", func(t *testing.T) {
		m := NewHandlerMap()
		m.Add(noopHandler[PMF](), HANDLER_MODE_POSTFIX).
			Add(noopHandler[PMF](), HANDLER_MODE_PARALLEL).
			Add(noopHandler[PMF](), HANDLER_MODE_REPLACE)
		c, _ := m.Get[PMF]()
		if chains, lengths := shapeOf(c); chains != 2 || !reflect.DeepEqual(lengths, []int{1, 1}) {
			t.Fatalf("shape (%d,%v) want (2,[1 1])", chains, lengths)
		}
	})
	t.Run("invalid panics", func(t *testing.T) {
		m := NewHandlerMap()
		defer func() {
			if recover() == nil {
				t.Fatal("the zero HandlerMode must not be usable")
			}
		}()
		m.Add(noopHandler[PMF](), HANDLER_MODE_INVALID)
	})
	t.Run("nil handler panics", func(t *testing.T) {
		m := NewHandlerMap()
		defer func() {
			if recover() == nil {
				t.Fatal("want panic")
			}
		}()
		m.AddType(reflect.TypeFor[PMF](), nil, HANDLER_MODE_POSTFIX)
	})
}

// --- distinct types, distinct slots --------------------------------------

func TestDistinctSlots(t *testing.T) {
	m := NewHandlerMap()
	m.Add(noopHandler[PMF](), HANDLER_MODE_POSTFIX)
	m.Add(noopHandler[RLF](), HANDLER_MODE_POSTFIX)
	m.Add(noopHandler[events.TransmitRenderedTemplateEvent](), HANDLER_MODE_POSTFIX)

	if m.Len() != 3 {
		t.Fatalf("len %d want 3", m.Len())
	}
	if !m.Has[PMF]() || !m.Has[RLF]() || !m.Has[events.TransmitRenderedTemplateEvent]() {
		t.Fatal("missing slot")
	}
}

// --- Set ------------------------------------------------------------------

func TestSetReplacesSlot(t *testing.T) {
	m := NewHandlerMap()
	m.Add(noopHandler[PMF](), HANDLER_MODE_POSTFIX)

	var ran int
	m.Set[PMF](Chains{{
		func(events.NetworkingEvent) error { ran++; return nil },
		func(events.NetworkingEvent) error { ran++; return nil },
	}})

	c, ok := m.Get[PMF]()
	if !ok {
		t.Fatal("absent after Set")
	}
	if chains, lengths := shapeOf(c); chains != 1 || !reflect.DeepEqual(lengths, []int{2}) {
		t.Fatalf("shape (%d,%v) want (1,[2])", chains, lengths)
	}
	if err := c.Dispatch(PMF{}); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if ran != 2 {
		t.Fatalf("ran = %d, want 2 (Set replaced the previous handler)", ran)
	}
}

// --- Has / Delete / Types / Clear ----------------------------------------

func TestHasDeleteAndClear(t *testing.T) {
	m := NewHandlerMap()
	m.Add(noopHandler[PMF](), HANDLER_MODE_POSTFIX)
	m.Add(noopHandler[events.CompBundlingFailureEvent](), HANDLER_MODE_POSTFIX)

	if !m.Has[PMF]() || !m.Has[events.CompBundlingFailureEvent]() {
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

	if !m.Delete[PMF]() || m.Delete[PMF]() {
		t.Fatal("Delete must report whether it removed anything")
	}
	m.Clear()
	if m.Len() != 0 {
		t.Fatalf("len after clear %d", m.Len())
	}
}

// --- nil contract ---------------------------------------------------------

func TestNilContract(t *testing.T) {
	var m *HandlerMap

	if _, ok := m.Get[PMF](); ok {
		t.Fatal("nil Get")
	}
	if m.Has[PMF]() {
		t.Fatal("nil Has")
	}
	if m.Delete[PMF]() {
		t.Fatal("nil Delete")
	}
	if m.Len() != 0 || m.Types() != nil {
		t.Fatal("nil Len/Types")
	}
	m.Clear()

	for name, register := range map[string]func(){
		"Add":     func() { m.Add(noopHandler[PMF](), HANDLER_MODE_POSTFIX) },
		"AddType": func() { m.AddType(reflect.TypeFor[PMF](), rawTag(0, nil), HANDLER_MODE_POSTFIX) },
		"Set":     func() { m.Set[PMF](nil) },
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Fatalf("%s on a nil map must panic rather than drop the registration", name)
				}
			}()
			register()
		}()
	}
}

// --- dispatch: parallel chains + sequential order + error stop -----------

func TestDispatchRunsEveryChain(t *testing.T) {
	m := NewHandlerMap()
	seen := make(chan int, 16)

	// 1 and 2 share the primary chain; 3 gets a chain of its own.
	m.Add(func(PMF) error { seen <- 1; return nil }, HANDLER_MODE_POSTFIX).
		Add(func(PMF) error { seen <- 2; return nil }, HANDLER_MODE_POSTFIX).
		Add(func(PMF) error { seen <- 3; return nil }, HANDLER_MODE_PARALLEL)

	c, _ := m.Get[PMF]()
	if err := c.Dispatch(PMF{}); err != nil {
		t.Fatalf("dispatch err %v", err)
	}
	close(seen)

	got := map[int]bool{}
	for v := range seen {
		got[v] = true
	}
	if !got[1] || !got[2] || !got[3] {
		t.Fatalf("not every handler ran: %v", got)
	}
}

func TestDispatchStopsChainOnError(t *testing.T) {
	m := NewHandlerMap()
	ran := 0
	m.Add(func(PMF) error { ran++; return errors.New("stop") }, HANDLER_MODE_POSTFIX).
		Add(func(PMF) error { ran++; return nil }, HANDLER_MODE_POSTFIX) // same chain, after the error

	c, _ := m.Get[PMF]()
	err := c.Dispatch(PMF{})
	if err == nil || err.Error() != "stop" {
		t.Fatalf("err = %v want stop", err)
	}
	if ran != 1 {
		t.Fatalf("ran = %d, want 1 (a chain stops at its first error)", ran)
	}
}

// An error in one chain must not stop the others.
func TestDispatchReportsErrorWithoutStoppingOtherChains(t *testing.T) {
	m := NewHandlerMap()
	done := make(chan struct{})
	m.Add(func(PMF) error { return errors.New("boom") }, HANDLER_MODE_POSTFIX).
		Add(func(PMF) error { close(done); return nil }, HANDLER_MODE_PARALLEL)

	c, _ := m.Get[PMF]()
	if err := c.Dispatch(PMF{}); err == nil || err.Error() != "boom" {
		t.Fatalf("err = %v want boom", err)
	}
	select {
	case <-done:
	default:
		t.Fatal("the parallel chain was skipped because another chain failed")
	}
}

func TestDispatchEmptyIsNoop(t *testing.T) {
	if err := (Chains)(nil).Dispatch(PMF{}); err != nil {
		t.Fatalf("nil Chains dispatch = %v", err)
	}
}
