// Package typemap provides a heterogeneous, type-safe map keyed by Go type.
//
// Each distinct type T maps to at most one value of type T. The correlation
// between a key's type and its value's type is an invariant enforced at the
// API boundary: the only way to insert a value is through a generic function
// that derives the key from the value's static type, so the type assertion on
// retrieval can never fail for a value that was inserted through the API.
//
// The zero value is not usable; construct a Map with New. A nil *Map is a
// valid empty, read-only map: reads report "absent" and mutations panic (the
// same contract as writing to a nil built-in map).
package typemap

import "reflect"

// Map is a heterogeneous container mapping each type to a single value of that
// type. It is not safe for concurrent use; guard it with a sync.Mutex or use
// a higher-level wrapper if you need concurrency.
type Map struct {
	internal map[reflect.Type]any
}

// New returns an empty Map ready for use.
func New() *Map {
	return &Map{
		internal: make(map[reflect.Type]any),
	}
}

// Len reports the number of entries. A nil *Map has length zero.
func (m *Map) Len() int {
	if m == nil {
		return 0
	}
	return len(m.internal)
}

// Types returns the key types currently stored, in unspecified order.
// A nil *Map returns nil.
func (m *Map) Types() []reflect.Type {
	if m == nil {
		return nil
	}
	out := make([]reflect.Type, 0, len(m.internal))
	for t := range m.internal {
		out = append(out, t)
	}
	return out
}

// Clear removes all entries. A nil *Map is a no-op.
func (m *Map) Clear() {
	if m == nil {
		return
	}
	clear(m.internal)
}

// Get returns the value stored for type T and whether it was present.
// A nil *Map (or an absent key) yields the zero value of T and false.
func Get[T any](m *Map) (T, bool) {
	if m == nil {
		var zero T
		return zero, false
	}
	if value, ok := m.internal[reflect.TypeFor[T]()]; ok {
		// This assertion is structurally guaranteed for values inserted via
		// Set: the key was derived from T, so the value has dynamic type T.
		if result, ok := value.(T); ok {
			return result, true
		}
	}
	var zero T
	return zero, false
}

// Set stores value under key type T, overwriting any existing entry.
// It panics on a nil *Map, matching the semantics of a nil built-in map.
func Set[T any](m *Map, value T) {
	if m == nil {
		panic("typemap: Set on nil *Map")
	}
	m.internal[reflect.TypeFor[T]()] = value
}

// Has reports whether a value is stored for type T.
// A nil *Map always reports false.
func Has[T any](m *Map) bool {
	if m == nil {
		return false
	}
	_, ok := m.internal[reflect.TypeFor[T]()]
	return ok
}

// Delete removes the entry for type T and reports whether one was present.
// A nil *Map always reports false.
func Delete[T any](m *Map) bool {
	if m == nil {
		return false
	}
	key := reflect.TypeFor[T]()
	if _, ok := m.internal[key]; ok {
		delete(m.internal, key)
		return true
	}
	return false
}

// GetOr returns the value stored for type T, or fallback if absent.
func GetOr[T any](m *Map, fallback T) T {
	if v, ok := Get[T](m); ok {
		return v
	}
	return fallback
}

// GetOrSet returns the value stored for type T. If absent, it stores and
// returns value. The second result reports whether value was newly stored
// (true) rather than an existing entry being returned (false).
// It panics on a nil *Map when a store is required.
func GetOrSet[T any](m *Map, value T) (T, bool) {
	if v, ok := Get[T](m); ok {
		return v, false
	}
	Set(m, value)
	return value, true
}

// IfPresent calls fn with the value stored for type T if present, returning true.
// If absent, it does nothing and returns false.
func IfPresent[T any](m *Map, fn func(T)) bool {
	if v, ok := Get[T](m); ok {
		fn(v)
		return true
	}
	return false
}
