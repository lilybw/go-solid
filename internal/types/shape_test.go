package types

import "testing"

func TestCanonicalTS_FormattingIsNotSignificant(t *testing.T) {
	cases := []struct{ a, b string }{
		{"{ a: string; b?: number }", "{a:string;b?:number}"},
		{"{ a: string, b: number }", "{a: string; b: number}"},
		{"{ a: string; }", "{a:string}"},
		{"string []", "string[]"},
		{"Record<string, number>", "Record<string,number>"},
		{"string /* a note */ []", "string[]"},
		{"string // trailing\n[]", "string[]"},
		{"{ nested: { deep: string; }; }", "{nested:{deep:string}}"},
	}
	for _, c := range cases {
		if got, want := CanonicalTS(c.a), CanonicalTS(c.b); got != want {
			t.Errorf("CanonicalTS(%q) = %q, CanonicalTS(%q) = %q; want equal", c.a, got, c.b, want)
		}
	}
}

func TestCanonicalTS_KeepsSignificantSpace(t *testing.T) {
	if got := CanonicalTS("keyof  Props"); got != "keyof Props" {
		t.Errorf("CanonicalTS = %q, want %q", got, "keyof Props")
	}
	// A space inside a string literal type is part of the type.
	if got := CanonicalTS(`"a b" | "c"`); got != `"a b"|"c"` {
		t.Errorf("CanonicalTS = %q, want %q", got, `"a b"|"c"`)
	}
}

func TestCanonicalTS_DistinctTypesStayDistinct(t *testing.T) {
	if CanonicalTS("string[]") == CanonicalTS("number[]") {
		t.Fatal("string[] and number[] must not canonicalize alike")
	}
	if CanonicalTS("string") == CanonicalTS("string | null") {
		t.Fatal("nullability must survive canonicalization")
	}
}

func TestNewShape_SortsAndDeduplicates(t *testing.T) {
	shape := NewShape([]Field{
		{Name: "b", TS: "number"},
		{Name: "a", TS: "string"},
		{Name: "a", TS: "boolean"}, // later duplicate is dropped
	})
	fields := shape.Fields()
	if len(fields) != 2 {
		t.Fatalf("Len() = %d, want 2", len(fields))
	}
	if fields[0].Name != "a" || fields[1].Name != "b" {
		t.Fatalf("fields not sorted: %+v", fields)
	}
	if fields[0].TS != "string" {
		t.Fatalf("first duplicate should win, got %q", fields[0].TS)
	}
}

func TestShape_FingerprintReadsAsACanonicalObjectBody(t *testing.T) {
	shape := NewShape([]Field{
		{Name: "name", TS: "  string  ", Optional: true},
		{Name: "count", TS: "number"},
	})
	// Sorted by name, formatting canonicalized, ";" between members and not
	// after the last one.
	if got, want := shape.Fingerprint(), "count:number;name?:string"; got != want {
		t.Fatalf("Fingerprint = %q, want %q", got, want)
	}
	if got := (Shape{}).Fingerprint(); got != "" {
		t.Errorf("an empty shape should fingerprint as \"\", got %q", got)
	}
	if got := NewShape([]Field{{Name: "only", TS: "string"}}).Fingerprint(); got != "only:string" {
		t.Errorf("a single field should carry no separator, got %q", got)
	}
}

func TestShape_EqualIgnoresDeclarationOrderAndFormatting(t *testing.T) {
	a := NewShape([]Field{{Name: "x", TS: "{ a: string, b: number }"}, {Name: "y", TS: "string"}})
	b := NewShape([]Field{{Name: "y", TS: "string"}, {Name: "x", TS: "{a: string; b: number}"}})
	if !a.Equal(b) {
		t.Fatalf("shapes should be equal:\n%q\n%q", a.Fingerprint(), b.Fingerprint())
	}
}

func TestShape_Lookup(t *testing.T) {
	shape := NewShape([]Field{{Name: "a", TS: "string"}, {Name: "c", TS: "number"}})
	if f, ok := shape.Lookup("c"); !ok || f.TS != "number" {
		t.Fatalf("Lookup(c) = %+v, %v", f, ok)
	}
	if _, ok := shape.Lookup("b"); ok {
		t.Fatal("Lookup(b) should miss")
	}
}

func TestViolations_ExtraFieldsAndOrderAreFine(t *testing.T) {
	target := NewShape([]Field{
		{Name: "title", TS: "string"},
		{Name: "count", TS: "number"},
	})
	// Everything the target requires, plus more, declared in another order.
	source := NewShape([]Field{
		{Name: "count", TS: "number"},
		{Name: "extra", TS: "boolean"},
		{Name: "title", TS: "string"},
		{Name: "alsoExtra", TS: "{ nested: string }"},
	})

	if violations := Violations(target, source); len(violations) != 0 {
		t.Fatalf("a widened shape must satisfy its target, got %+v", violations)
	}
	if !Satisfies(target, source) {
		t.Fatal("Satisfies should agree with Violations")
	}
	// The relation is one-way: the target does not satisfy the wider source.
	if Satisfies(source, target) {
		t.Fatal("covariance must not be symmetric")
	}
}

func TestViolations_MissingRequiredField(t *testing.T) {
	target := NewShape([]Field{
		{Name: "id", TS: "string"},
		{Name: "note", TS: "string", Optional: true},
	})
	source := NewShape([]Field{{Name: "note", TS: "string", Optional: true}})

	violations := Violations(target, source)
	if len(violations) != 1 {
		t.Fatalf("Violations = %+v, want exactly the missing id", violations)
	}
	if violations[0].Kind != VIOLATION_MISSING || violations[0].Field != "id" {
		t.Errorf("violations[0] = %+v, want id missing", violations[0])
	}
}

func TestViolations_AbsentOptionalIsNotAViolation(t *testing.T) {
	target := NewShape([]Field{{Name: "note", TS: "string", Optional: true}})
	if violations := Violations(target, Shape{}); len(violations) != 0 {
		t.Fatalf("an absent optional is what optional means, got %+v", violations)
	}
}

func TestViolations_RequiredFieldSuppliedAsOptional(t *testing.T) {
	target := NewShape([]Field{{Name: "id", TS: "string"}})
	source := NewShape([]Field{{Name: "id", TS: "string", Optional: true}})

	violations := Violations(target, source)
	if len(violations) != 1 || violations[0].Kind != VIOLATION_OPTIONAL {
		t.Fatalf("Violations = %+v, want a may-be-absent violation", violations)
	}
}

// Supplying a required field as optional is a violation; the reverse is not.
func TestViolations_OptionalTargetAcceptsARequiredSource(t *testing.T) {
	target := NewShape([]Field{{Name: "note", TS: "string", Optional: true}})
	source := NewShape([]Field{{Name: "note", TS: "string"}})
	if violations := Violations(target, source); len(violations) != 0 {
		t.Fatalf("always supplying an optional field is fine, got %+v", violations)
	}
}

func TestViolations_IncompatibleType(t *testing.T) {
	target := NewShape([]Field{{Name: "count", TS: "number"}})
	source := NewShape([]Field{{Name: "count", TS: "string"}})

	violations := Violations(target, source)
	if len(violations) != 1 || violations[0].Kind != VIOLATION_TYPE {
		t.Fatalf("Violations = %+v, want a type violation", violations)
	}
	if violations[0].Want != "number" || violations[0].Got != "string" {
		t.Errorf("violation should carry both types, got %+v", violations[0])
	}
}

func TestViolations_UnionWidening(t *testing.T) {
	// A narrower source satisfies a union target.
	if !Satisfies(
		NewShape([]Field{{Name: "note", TS: "string | null"}}),
		NewShape([]Field{{Name: "note", TS: "string"}}),
	) {
		t.Error("string should satisfy string | null")
	}
	// But not the other way round: null is not acceptable where string is.
	if Satisfies(
		NewShape([]Field{{Name: "note", TS: "string"}}),
		NewShape([]Field{{Name: "note", TS: "string | null"}}),
	) {
		t.Error("string | null must not satisfy string")
	}
	// A "|" inside brackets is not a top-level union member.
	if Satisfies(
		NewShape([]Field{{Name: "m", TS: "string"}}),
		NewShape([]Field{{Name: "m", TS: "Record<string, A | B>"}}),
	) {
		t.Error("a nested union must not be split apart")
	}
}

func TestViolations_UnknownAcceptsAnything(t *testing.T) {
	target := NewShape([]Field{{Name: "anything", TS: "unknown"}})
	source := NewShape([]Field{{Name: "anything", TS: "{ a: string }"}})
	if !Satisfies(target, source) {
		t.Fatal("unknown should accept any type")
	}
}

func TestViolations_FormattingIsNotAViolation(t *testing.T) {
	target := NewShape([]Field{{Name: "x", TS: "{ a: string, b: number }"}})
	source := NewShape([]Field{{Name: "x", TS: "{a: string; b: number}"}})
	if !Satisfies(target, source) {
		t.Fatal("formatting must not produce a violation")
	}
}

func TestViolations_AreOrderedByField(t *testing.T) {
	target := NewShape([]Field{
		{Name: "zulu", TS: "string"},
		{Name: "alpha", TS: "string"},
	})
	violations := Violations(target, Shape{})
	if len(violations) != 2 {
		t.Fatalf("Violations = %+v, want 2", violations)
	}
	if violations[0].Field != "alpha" || violations[1].Field != "zulu" {
		t.Errorf("violations should be ordered by field name, got %+v", violations)
	}
}

func TestSplitTopLevelUnion(t *testing.T) {
	cases := map[string]int{
		"string":                    1,
		"string|null":               2,
		"Record<string,A|B>":        1,
		"{a:string|null}":           1,
		"(string|null)[]|undefined": 2,
	}
	for input, want := range cases {
		if got := len(splitTopLevelUnion(input)); got != want {
			t.Errorf("splitTopLevelUnion(%q) produced %d members, want %d", input, got, want)
		}
	}
}
