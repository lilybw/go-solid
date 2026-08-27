package types

import (
	jsonv1 "encoding/json"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"
)

var (
	// ErrNoProps reports props that carry no object to describe: a nil
	// interface, or a nil pointer.
	ErrNoProps = errors.New("go_solid/types: no props to derive a shape from")
	// ErrNotAnObject reports props that marshal to something other than a
	// JSON object.
	ErrNotAnObject = errors.New("go_solid/types: props do not marshal to a JSON object")
	// ErrUnmarshalable reports props json cannot encode at all. The render
	// path reports these itself, so the type subsystem stays quiet about them.
	ErrUnmarshalable = errors.New("go_solid/types: props cannot be marshaled")
)

// DEFAULT_MAX_DEPTH bounds how far nested structs are expanded before the
// mapper falls back to "unknown".
const DEFAULT_MAX_DEPTH = 12

// Mapper turns Go types into the TypeScript types a browser actually receives.
//
// The mapping follows encoding/json/v2, the marshaller the render path uses.
type Mapper struct {
	// MaxDepth bounds nested struct expansion. Zero means DEFAULT_MAX_DEPTH.
	MaxDepth int
}

var (
	timeType       = reflect.TypeFor[time.Time]()
	rawMessageType = reflect.TypeFor[jsonv1.RawMessage]()
	jsonNumberType = reflect.TypeFor[jsonv1.Number]()
	rawValueType   = reflect.TypeFor[jsontext.Value]()
)

func (m *Mapper) Shape[T any]() (Shape, error) {
	return m.ShapeOfType(reflect.TypeFor[T]())
}

// ShapeOfType derives the props shape of a struct type, or of a pointer to one.
func (m *Mapper) ShapeOfType(t reflect.Type) (Shape, error) {
	if t == nil {
		return Shape{}, ErrNoProps
	}
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return Shape{}, notAnObject(t)
	}
	return NewShape(m.structFields(t, 0, map[reflect.Type]bool{})), nil
}

// ShapeOfValue derives the props shape of a value. Maps are described from the
// keys the value actually carries, since their type says nothing about them.
func (m *Mapper) ShapeOfValue(props any) (Shape, error) {
	if props == nil {
		return Shape{}, ErrNoProps
	}
	rv := reflect.ValueOf(props)
	for rv.Kind() == reflect.Pointer || rv.Kind() == reflect.Interface {
		if rv.IsNil() {
			return Shape{}, ErrNoProps
		}
		rv = rv.Elem()
	}
	switch rv.Kind() {
	case reflect.Struct:
		return m.ShapeOfType(rv.Type())
	case reflect.Map:
		return m.shapeOfMap(rv)
	default:
		return Shape{}, notAnObject(rv.Type())
	}
}

// notAnObject classifies props that cannot be described as a JSON object
func notAnObject(t reflect.Type) error {
	switch t.Kind() {
	case reflect.Chan, reflect.Func, reflect.Complex64, reflect.Complex128, reflect.UnsafePointer:
		return fmt.Errorf("%w: %s", ErrUnmarshalable, t)
	}
	return fmt.Errorf("%w: %s", ErrNotAnObject, t)
}

func (m *Mapper) maxDepth() int {
	if m.MaxDepth > 0 {
		return m.MaxDepth
	}
	return DEFAULT_MAX_DEPTH
}

func (m *Mapper) shapeOfMap(rv reflect.Value) (Shape, error) {
	if rv.Type().Key().Kind() != reflect.String {
		return Shape{}, fmt.Errorf("%w: map key %s is not a string", ErrNotAnObject, rv.Type().Key())
	}
	var (
		seen    = map[reflect.Type]bool{}
		dynamic = rv.Type().Elem().Kind() == reflect.Interface
		static  string
		fields  = make([]Field, 0, rv.Len())
	)
	if !dynamic {
		static = m.tsType(rv.Type().Elem(), 1, seen)
	}
	for iter := rv.MapRange(); iter.Next(); {
		ts := static
		if dynamic {
			// An `any` element says nothing; the value in hand does.
			if v := iter.Value(); v.IsNil() {
				ts = "unknown"
			} else {
				ts = m.tsType(v.Elem().Type(), 1, seen)
			}
		}
		fields = append(fields, Field{Name: iter.Key().String(), TS: ts})
	}
	return NewShape(fields), nil
}

// jsonField is one candidate produced while flattening embedded structs.
type jsonField struct {
	Field
	depth  int  // embedding depth; 0 is a field declared on the struct itself
	tagged bool // the json tag named it explicitly
}

// structFields resolves the JSON view of t, applying encoding/json's rule for
// embedded fields: shallowest wins, a tie at the same depth is broken by a json
// tag, and an unbroken tie drops every candidate.
func (m *Mapper) structFields(t reflect.Type, depth int, seen map[reflect.Type]bool) []Field {
	if depth > m.maxDepth() || seen[t] {
		return nil
	}
	seen[t] = true
	defer delete(seen, t)

	var candidates []jsonField
	m.collect(t, depth, 0, seen, &candidates)

	byName := map[string][]jsonField{}
	for _, c := range candidates {
		byName[c.Name] = append(byName[c.Name], c)
	}
	out := make([]Field, 0, len(byName))
	for _, group := range byName {
		if winner, ok := pickField(group); ok {
			out = append(out, winner.Field)
		}
	}
	return out
}

func (m *Mapper) collect(t reflect.Type, depth, embedDepth int, seen map[reflect.Type]bool, out *[]jsonField) {
	for i := range t.NumField() {
		sf := t.Field(i)
		raw, hasTag := sf.Tag.Lookup("json")
		tag := parseJSONTag(raw, hasTag)
		if tag.ignored {
			continue
		}

		ft := sf.Type
		if ft.Kind() == reflect.Pointer {
			ft = ft.Elem()
		}

		// An embedded struct is reachable even when its own type is
		// unexported, because its exported fields are promoted.
		if sf.Anonymous {
			if !sf.IsExported() && ft.Kind() != reflect.Struct {
				continue
			}
		} else if !sf.IsExported() {
			continue
		}

		// An embedded struct the tag does not name is flattened, as v2 does;
		// the `embed` option asks for the same treatment explicitly.
		if ft.Kind() == reflect.Struct && ((sf.Anonymous && !tag.hasName) || tag.embed) {
			if seen[ft] {
				continue
			}
			seen[ft] = true
			m.collect(ft, depth, embedDepth+1, seen, out)
			delete(seen, ft)
			continue
		}

		name := tag.name
		if name == "" {
			name = sf.Name
		}
		ts, optional := m.fieldType(sf.Type, tag, depth+1, seen)
		*out = append(*out, jsonField{
			Name: name, TS: ts, Optional: optional,
			depth:  embedDepth,
			tagged: tag.hasName,
		})
	}
}

// jsonTag is the parsed form of a `json` struct tag.
type jsonTag struct {
	name      string
	hasName   bool
	ignored   bool
	omitEmpty bool
	omitZero  bool
	embed     bool
}

// parseJSONTag reads the subset of v2's tag grammar that changes a field's
// shape. .
func parseJSONTag(tag string, hasTag bool) jsonTag {
	if !hasTag {
		return jsonTag{}
	}
	if tag == "-" {
		return jsonTag{ignored: true} // v2 spells a literal "-" name as `'-'`
	}

	out := jsonTag{}
	rest := tag
	switch {
	case strings.HasPrefix(rest, ","):
		rest = rest[len(","):]
	case strings.HasPrefix(rest, "'"):
		closing := strings.Index(rest[1:], "'")
		if closing < 0 {
			return jsonTag{} // malformed; v2 reports it when marshaling
		}
		out.name, out.hasName = rest[1:1+closing], true
		rest = strings.TrimPrefix(rest[closing+2:], ",")
	default:
		name, remainder, _ := strings.Cut(rest, ",")
		out.name, out.hasName = name, name != ""
		rest = remainder
	}

	for opt := range strings.SplitSeq(rest, ",") {
		switch opt {
		case "omitempty":
			out.omitEmpty = true
		case "omitzero":
			out.omitZero = true
		case "embed":
			out.embed = true
		}
	}
	return out
}

func pickField(group []jsonField) (jsonField, bool) {
	if len(group) == 1 {
		return group[0], true
	}
	shallowest := slices.MinFunc(group, func(a, b jsonField) int { return a.depth - b.depth }).depth
	group = slices.DeleteFunc(slices.Clone(group), func(f jsonField) bool { return f.depth != shallowest })
	if len(group) == 1 {
		return group[0], true
	}
	tagged := slices.DeleteFunc(slices.Clone(group), func(f jsonField) bool { return !f.tagged })
	if len(tagged) == 1 {
		return tagged[0], true
	}
	return jsonField{}, false
}

// fieldType renders a struct field's TypeScript type and decides whether the
// key can be absent.
func (m *Mapper) fieldType(t reflect.Type, tag jsonTag, depth int, seen map[reflect.Type]bool) (string, bool) {
	ts := m.tsType(t, depth, seen)
	if tag.omitZero || (tag.omitEmpty && m.canEncodeEmpty(t, depth, seen)) {
		return strings.TrimSuffix(ts, " | null"), true
	}
	return ts, false
}

// canEncodeEmpty reports whether a value of t can marshal to one of JSON's
// empty values, which is what omitempty tests.
func (m *Mapper) canEncodeEmpty(t reflect.Type, depth int, seen map[reflect.Type]bool) bool {
	switch t {
	case timeType, jsonNumberType:
		return false // an RFC 3339 timestamp and a JSON number are never empty
	case rawMessageType, rawValueType:
		return true // raw JSON may hold null, "", {} or []
	}
	switch t.Kind() {
	case reflect.String, reflect.Slice, reflect.Array, reflect.Map,
		reflect.Pointer, reflect.Interface:
		return true
	case reflect.Struct:
		// Only a struct that can encode as {}: one with no required members.
		for _, f := range m.structFields(t, depth, seen) {
			if !f.Optional {
				return false
			}
		}
		return true
	}
	return false // numbers and booleans
}

func (m *Mapper) tsType(t reflect.Type, depth int, seen map[reflect.Type]bool) string {
	switch t {
	case timeType:
		return "string"
	case jsonNumberType:
		return "number"
	case rawMessageType, rawValueType:
		return "unknown"
	}

	switch t.Kind() {
	case reflect.Pointer:
		return orNull(m.tsType(t.Elem(), depth, seen))
	case reflect.Bool:
		return "boolean"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64:
		return "number"
	case reflect.String:
		return "string"
	case reflect.Slice:
		if t.Elem().Kind() == reflect.Uint8 {
			return "string" // base64; a nil byte slice encodes as ""
		}
		// A nil slice encodes as [], not null.
		return arrayOf(m.tsType(t.Elem(), depth, seen))
	case reflect.Array:
		return arrayOf(m.tsType(t.Elem(), depth, seen))
	case reflect.Map:
		if t.Key().Kind() != reflect.String && !isIntegerKind(t.Key().Kind()) {
			return "unknown"
		}
		// A nil map encodes as {}, not null.
		return "Record<string, " + m.tsType(t.Elem(), depth, seen) + ">"
	case reflect.Struct:
		if depth > m.maxDepth() || seen[t] {
			return "unknown"
		}
		return objectLiteral(NewShape(m.structFields(t, depth, seen)))
	default:
		// Interfaces, and anything json cannot describe statically.
		return "unknown"
	}
}

func isIntegerKind(k reflect.Kind) bool {
	switch k {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return true
	}
	return false
}

func orNull(ts string) string {
	if strings.HasSuffix(ts, " | null") {
		return ts
	}
	return ts + " | null"
}

// arrayOf suffixes "[]", parenthesising a top-level union, where the suffix
// would otherwise bind to the last member alone.
func arrayOf(elem string) string {
	if hasTopLevelUnion(elem) {
		return "(" + elem + ")[]"
	}
	return elem + "[]"
}

func hasTopLevelUnion(ts string) bool {
	depth := 0
	for _, c := range ts {
		switch c {
		case '{', '(', '[', '<':
			depth++
		case '}', ')', ']', '>':
			depth--
		case '|', '&':
			if depth == 0 {
				return true
			}
		}
	}
	return false
}

// objectLiteral renders a shape inline, for use as a nested member type.
func objectLiteral(s Shape) string {
	if s.Empty() {
		return "{}"
	}
	var b strings.Builder
	b.WriteString("{ ")
	for i, f := range s.fields {
		if i > 0 {
			b.WriteString("; ")
		}
		b.WriteString(f.Name)
		if f.Optional {
			b.WriteByte('?')
		}
		b.WriteString(": ")
		b.WriteString(f.TS)
	}
	b.WriteString(" }")
	return b.String()
}
