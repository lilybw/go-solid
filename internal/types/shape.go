package types

import (
	"slices"
	"strings"

	"github.com/lilybw/go-solid/internal/meta"
)

type Field struct {
	Name     string `json:"name"`
	TS       string `json:"ts"`
	Optional bool   `json:"optional,omitzero"`
}

// Shape is the structural description of a component's props.
type Shape struct {
	fields []Field
}

// NewShape sorts fields by name and drops later duplicates.
func NewShape(fields []Field) Shape {
	sorted := slices.Clone(fields)
	slices.SortStableFunc(sorted, func(a, b Field) int { return strings.Compare(a.Name, b.Name) })
	sorted = slices.CompactFunc(sorted, func(a, b Field) bool { return a.Name == b.Name })
	return Shape{fields: sorted}
}

// Fields returns the members, sorted by name. The result is a copy; mutating it
// cannot break the ordering the shape relies on.
func (s Shape) Fields() []Field { return slices.Clone(s.fields) }

func (s Shape) Len() int { return len(s.fields) }

func (s Shape) Empty() bool { return len(s.fields) == 0 }

// Lookup returns the field named name.
func (s Shape) Lookup(name string) (Field, bool) {
	i, ok := slices.BinarySearchFunc(s.fields, name, func(f Field, n string) int {
		return strings.Compare(f.Name, n)
	})
	if !ok {
		return Field{}, false
	}
	return s.fields[i], true
}

// Fingerprint is a canonical single-line encoding, stable across formatting
// differences in the underlying type expressions. Two shapes with the same
// fingerprint are identical, which is stricter than Satisfies.
func (s Shape) Fingerprint() meta.Fingerprint {
	var b strings.Builder
	for i, f := range s.fields {
		if i > 0 {
			b.WriteByte(';')
		}
		b.WriteString(f.Name)
		if f.Optional {
			b.WriteByte('?')
		}
		b.WriteByte(':')
		b.WriteString(CanonicalTS(f.TS))
	}
	return b.String()
}

// Equal reports whether both shapes describe the same object.
func (s Shape) Equal(other Shape) bool { return s.Fingerprint() == other.Fingerprint() }

// ViolationKind classifies one way a supplied shape fails its target.
type ViolationKind uint8

const (
	// VIOLATION_MISSING: the target requires a field the source does not carry.
	VIOLATION_MISSING ViolationKind = iota
	// VIOLATION_OPTIONAL: the target requires a field the source may omit.
	VIOLATION_OPTIONAL
	// VIOLATION_TYPE: both carry the field, but the source's type cannot stand
	// in for the target's.
	VIOLATION_TYPE
)

func (k ViolationKind) String() string {
	switch k {
	case VIOLATION_MISSING:
		return "missing"
	case VIOLATION_OPTIONAL:
		return "may-be-absent"
	case VIOLATION_TYPE:
		return "type"
	default:
		return "unknown"
	}
}

// Violation is one way a supplied shape fails to satisfy a target.
type Violation struct {
	Kind  ViolationKind
	Field string
	Want  string
	Got   string
}

// Violations lists every way source fails to stand in for target, ordered by
// field name. It is empty exactly when Satisfies reports true.
// Strictness: typeof Target === Class<? extends Source>
func Violations(target, source Shape) []Violation {
	var out []Violation
	for _, want := range target.fields {
		got, present := source.Lookup(want.Name)
		if !present {
			if !want.Optional {
				out = append(out, Violation{Kind: VIOLATION_MISSING, Field: want.Name, Want: want.TS})
			}
			continue
		}
		if !want.Optional && got.Optional {
			out = append(out, Violation{
				Kind:  VIOLATION_OPTIONAL,
				Field: want.Name,
				Want:  "always present",
				Got:   "optional",
			})
		}
		if !assignableTS(want.TS, got.TS) {
			out = append(out, Violation{Kind: VIOLATION_TYPE, Field: want.Name, Want: want.TS, Got: got.TS})
		}
	}
	slices.SortStableFunc(out, func(a, b Violation) int { return strings.Compare(a.Field, b.Field) })
	return out
}

// Satisfies reports whether source can be supplied wherever target is required.
func Satisfies(target, source Shape) bool { return len(Violations(target, source)) == 0 }

// assignableTS reports whether a value typed source can stand in for target.
//
// Type expressions are compared as text, canonicalized first, with one
// widening rule: every top-level union member of source must appear among
// target's, so string satisfies string | null but not the other way round.
func assignableTS(target, source string) bool {
	target, source = CanonicalTS(target), CanonicalTS(source)
	if target == source {
		return true
	}
	if target == "unknown" {
		return true // unknown accepts anything
	}
	wanted := make(map[string]bool)
	for _, member := range splitTopLevelUnion(target) {
		wanted[member] = true
	}
	for _, member := range splitTopLevelUnion(source) {
		if !wanted[member] {
			return false
		}
	}
	return true
}

// splitTopLevelUnion breaks a canonicalized type on the "|" separators that sit
// outside any bracket, so Record<string,A|B> stays whole.
func splitTopLevelUnion(canonical string) []string {
	var (
		members []string
		depth   int
		start   int
	)
	for i, c := range canonical {
		switch c {
		case '{', '(', '[', '<':
			depth++
		case '}', ')', ']', '>':
			depth--
		case '|':
			if depth == 0 {
				members = append(members, canonical[start:i])
				start = i + len("|")
			}
		}
	}
	return append(members, canonical[start:])
}
