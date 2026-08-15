package typemap

import (
	"fmt"
	"reflect"
	"sort"
	"testing"
)

// --- test fixtures -------------------------------------------------------

type UserCreated struct{ ID int }
type UserDeleted struct{ ID int }

type Named interface{ Name() string }

type person struct{ name string }

func (p person) Name() string { return p.name }

// --- core round-trip ------------------------------------------------------

func TestSetGetRoundTrip(t *testing.T) {
	m := New()
	Set(m, UserCreated{ID: 42})

	got, ok := Get[UserCreated](m)
	if !ok {
		t.Fatal("expected present, got absent")
	}
	if got.ID != 42 {
		t.Fatalf("got ID %d, want 42", got.ID)
	}
}

func TestGetAbsent(t *testing.T) {
	m := New()
	got, ok := Get[UserCreated](m)
	if ok {
		t.Fatal("expected absent")
	}
	if got != (UserCreated{}) {
		t.Fatalf("expected zero value, got %+v", got)
	}
}

// The central invariant: distinct types occupy distinct slots and never
// collide, even when structurally identical.
func TestDistinctTypesDoNotCollide(t *testing.T) {
	m := New()
	Set(m, UserCreated{ID: 1})
	Set(m, UserDeleted{ID: 2}) // same underlying shape, different type

	c, ok := Get[UserCreated](m)
	if !ok || c.ID != 1 {
		t.Fatalf("UserCreated: got %+v ok=%v", c, ok)
	}
	d, ok := Get[UserDeleted](m)
	if !ok || d.ID != 2 {
		t.Fatalf("UserDeleted: got %+v ok=%v", d, ok)
	}
	if m.Len() != 2 {
		t.Fatalf("len = %d, want 2", m.Len())
	}
}

// --- overwrite ------------------------------------------------------------

func TestSetOverwrites(t *testing.T) {
	m := New()
	Set(m, UserCreated{ID: 1})
	Set(m, UserCreated{ID: 2})

	got, _ := Get[UserCreated](m)
	if got.ID != 2 {
		t.Fatalf("got ID %d, want 2 (overwrite)", got.ID)
	}
	if m.Len() != 1 {
		t.Fatalf("len = %d, want 1 after overwrite", m.Len())
	}
}

// --- primitive and pointer types -----------------------------------------

func TestPrimitiveTypes(t *testing.T) {
	m := New()
	Set(m, 7)
	Set(m, "hello")
	Set(m, 3.14)

	if v, ok := Get[int](m); !ok || v != 7 {
		t.Fatalf("int: %v %v", v, ok)
	}
	if v, ok := Get[string](m); !ok || v != "hello" {
		t.Fatalf("string: %v %v", v, ok)
	}
	if v, ok := Get[float64](m); !ok || v != 3.14 {
		t.Fatalf("float64: %v %v", v, ok)
	}
	// int and a named-int type must not alias.
	type myInt int
	Set(m, myInt(7))
	if v, ok := Get[myInt](m); !ok || v != 7 {
		t.Fatalf("myInt: %v %v", v, ok)
	}
	if v, _ := Get[int](m); v != 7 {
		t.Fatalf("int slot disturbed by myInt: %v", v)
	}
}

func TestPointerVsValueAreDistinct(t *testing.T) {
	m := New()
	p := &UserCreated{ID: 9}
	Set(m, *p) // UserCreated
	Set(m, p)  // *UserCreated

	if _, ok := Get[UserCreated](m); !ok {
		t.Fatal("value entry missing")
	}
	if got, ok := Get[*UserCreated](m); !ok || got.ID != 9 {
		t.Fatalf("pointer entry: %v %v", got, ok)
	}
	if m.Len() != 2 {
		t.Fatalf("len = %d, want 2", m.Len())
	}
}

// --- interface vs concrete keying ----------------------------------------

// Storing through a concrete type param keys on the concrete type; storing
// through an interface type param keys on the interface type. They are
// separate slots. This documents the asymmetry discussed in design.
func TestInterfaceKeyingIsSeparateFromConcrete(t *testing.T) {
	m := New()
	Set[person](m, person{name: "alice"})
	Set[Named](m, person{name: "bob"})

	concrete, ok := Get[person](m)
	if !ok || concrete.name != "alice" {
		t.Fatalf("concrete: %+v %v", concrete, ok)
	}
	iface, ok := Get[Named](m)
	if !ok || iface.Name() != "bob" {
		t.Fatalf("interface: %+v %v", iface, ok)
	}
	if m.Len() != 2 {
		t.Fatalf("len = %d, want 2", m.Len())
	}
}

// --- Has / Delete ---------------------------------------------------------

func TestHas(t *testing.T) {
	m := New()
	if Has[UserCreated](m) {
		t.Fatal("Has on empty should be false")
	}
	Set(m, UserCreated{ID: 1})
	if !Has[UserCreated](m) {
		t.Fatal("Has after Set should be true")
	}
	if Has[UserDeleted](m) {
		t.Fatal("Has for unrelated type should be false")
	}
}

func TestDelete(t *testing.T) {
	m := New()
	Set(m, UserCreated{ID: 1})

	if !Delete[UserCreated](m) {
		t.Fatal("Delete of present should report true")
	}
	if Delete[UserCreated](m) {
		t.Fatal("Delete of absent should report false")
	}
	if Has[UserCreated](m) {
		t.Fatal("entry should be gone after Delete")
	}
}

// --- GetOr / GetOrSet -----------------------------------------------------

func TestGetOr(t *testing.T) {
	m := New()
	if v := GetOr(m, UserCreated{ID: 99}); v.ID != 99 {
		t.Fatalf("GetOr fallback: %+v", v)
	}
	Set(m, UserCreated{ID: 1})
	if v := GetOr(m, UserCreated{ID: 99}); v.ID != 1 {
		t.Fatalf("GetOr present: %+v", v)
	}
}

func TestGetOrSet(t *testing.T) {
	m := New()
	v, stored := GetOrSet(m, UserCreated{ID: 1})
	if !stored || v.ID != 1 {
		t.Fatalf("first GetOrSet: v=%+v stored=%v", v, stored)
	}
	v, stored = GetOrSet(m, UserCreated{ID: 2})
	if stored {
		t.Fatal("second GetOrSet should not store")
	}
	if v.ID != 1 {
		t.Fatalf("second GetOrSet should return existing 1, got %+v", v)
	}
}

// --- introspection --------------------------------------------------------

func TestTypesAndClear(t *testing.T) {
	m := New()
	Set(m, UserCreated{})
	Set(m, 0)
	Set(m, "")

	types := m.Types()
	if len(types) != 3 {
		t.Fatalf("Types len = %d, want 3", len(types))
	}
	names := make([]string, len(types))
	for i, tp := range types {
		names[i] = tp.String()
	}
	sort.Strings(names)
	want := []string{"int", "string", "typemap.UserCreated"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("Types = %v, want %v", names, want)
	}

	m.Clear()
	if m.Len() != 0 {
		t.Fatalf("len after Clear = %d, want 0", m.Len())
	}
}

// --- nil-receiver contract ------------------------------------------------

func TestNilMapReads(t *testing.T) {
	var m *Map // nil

	if v, ok := Get[UserCreated](m); ok || v != (UserCreated{}) {
		t.Fatalf("nil Get: v=%+v ok=%v", v, ok)
	}
	if Has[UserCreated](m) {
		t.Fatal("nil Has should be false")
	}
	if Delete[UserCreated](m) {
		t.Fatal("nil Delete should be false")
	}
	if m.Len() != 0 {
		t.Fatal("nil Len should be 0")
	}
	if m.Types() != nil {
		t.Fatal("nil Types should be nil")
	}
	if v := GetOr(m, UserCreated{ID: 5}); v.ID != 5 {
		t.Fatalf("nil GetOr should return fallback, got %+v", v)
	}
	m.Clear() // must not panic
}

func TestNilMapSetPanics(t *testing.T) {
	var m *Map
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("Set on nil *Map should panic")
		}
	}()
	Set(m, UserCreated{ID: 1})
}

// --- event-handler-style usage (the motivating case) ----------------------

// Demonstrates the typemap holding per-event-type handler slices, which is the
// pattern the whole design grew out of.
func TestAsHandlerRegistry(t *testing.T) {
	type Handler[E any] func(E)

	m := New()

	var createdLog []int
	Set(m, []Handler[UserCreated]{
		func(e UserCreated) { createdLog = append(createdLog, e.ID) },
		func(e UserCreated) { createdLog = append(createdLog, e.ID*10) },
	})

	emit := func(e UserCreated) {
		hs, _ := Get[[]Handler[UserCreated]](m)
		for _, h := range hs {
			h(e)
		}
	}
	emit(UserCreated{ID: 3})

	want := []int{3, 30}
	if !reflect.DeepEqual(createdLog, want) {
		t.Fatalf("handler log = %v, want %v", createdLog, want)
	}
}

// --- example (doubles as documentation) -----------------------------------

func ExampleGet() {
	m := New()
	Set(m, UserCreated{ID: 7})

	if e, ok := Get[UserCreated](m); ok {
		fmt.Println(e.ID)
	}
	// Output: 7
}
