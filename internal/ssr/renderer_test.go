package ssr

import (
	"strings"
	"testing"

	"github.com/lilybw/go-solid-compiler/solid"
)

// prop builds a plan reading props.<name>, as the compiler attaches once it
// has proved a hole derivable.
func prop(name string) solid.Plan { return &solid.Path{Steps: []string{name}} }

func renderTo(t *testing.T, segs []solid.Segment, props map[string]any) string {
	t.Helper()
	var b strings.Builder
	program := &solid.Program{Segments: segs}
	if err := render(&b, program, props); err != nil {
		t.Fatalf("render: %v", err)
	}
	return b.String()
}

func TestRenderFillsHoles(t *testing.T) {
	got := renderTo(t, []solid.Segment{
		&solid.Static{HTML: "<h1>"},
		&solid.Hole{Slot: solid.Slot{Plan: prop("title"), Escape: solid.EscapeText}},
		&solid.Static{HTML: "</h1>"},
	}, map[string]any{"title": "Hello"})
	if got != "<h1>Hello</h1>" {
		t.Errorf("got %q", got)
	}
}

func TestRenderEscapesValues(t *testing.T) {
	// A value that reads as markup is data. An author-written entity in the
	// static run is left alone, which is why the two are escaped separately.
	got := renderTo(t, []solid.Segment{
		&solid.Static{HTML: "<p>&amp;"},
		&solid.Hole{Slot: solid.Slot{Plan: prop("v"), Escape: solid.EscapeText}},
		&solid.Static{HTML: "</p>"},
	}, map[string]any{"v": "<script>&amp;"})
	want := "<p>&amp;&lt;script&gt;&amp;amp;</p>"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRenderOmitsAbsentChildren(t *testing.T) {
	// JSX writes nothing for null, undefined, and the booleans.
	for name, props := range map[string]map[string]any{
		"null":      {"v": nil},
		"missing":   {},
		"false":     {"v": false},
		"true":      {"v": true},
		"zero":      {"v": 0.0},
		"emptyText": {"v": ""},
	} {
		got := renderTo(t, []solid.Segment{
			&solid.Hole{Slot: solid.Slot{Plan: prop("v"), Escape: solid.EscapeText}},
		}, props)
		want := ""
		if name == "zero" {
			want = "0" // a zero is a value, unlike a boolean
		}
		if got != want {
			t.Errorf("%s: got %q, want %q", name, got, want)
		}
	}
}

func TestRenderAttributeRules(t *testing.T) {
	// Attributes do not follow the rules for children: true writes an
	// attribute where it would write no child.
	cases := []struct {
		name  string
		attr  solid.Attribute
		props map[string]any
		want  string
	}{
		{"content", solid.Attribute{Name: "title", Slot: solid.Slot{Plan: prop("v")}},
			map[string]any{"v": "hi"}, ` title="hi"`},
		{"content true", solid.Attribute{Name: "title", Slot: solid.Slot{Plan: prop("v")}},
			map[string]any{"v": true}, ` title="true"`},
		{"content false", solid.Attribute{Name: "title", Slot: solid.Slot{Plan: prop("v")}},
			map[string]any{"v": false}, ""},
		{"content absent", solid.Attribute{Name: "title", Slot: solid.Slot{Plan: prop("v")}},
			map[string]any{}, ""},
		{"content quoted", solid.Attribute{Name: "title", Slot: solid.Slot{Plan: prop("v")}},
			map[string]any{"v": `a"b`}, ` title="a&quot;b"`},
		{"bool on", solid.Attribute{Name: "checked", Slot: solid.Slot{Plan: prop("v")}, Bool: true},
			map[string]any{"v": true}, " checked"},
		{"bool off", solid.Attribute{Name: "checked", Slot: solid.Slot{Plan: prop("v")}, Bool: true},
			map[string]any{"v": false}, ""},
		{"bool empty string", solid.Attribute{Name: "checked", Slot: solid.Slot{Plan: prop("v")}, Bool: true},
			map[string]any{"v": ""}, ""},
	}
	for _, c := range cases {
		attr := c.attr
		got := renderTo(t, []solid.Segment{&attr}, c.props)
		if got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestRenderRefusesRedProgram(t *testing.T) {
	program := &solid.Program{
		Segments: []solid.Segment{&solid.Static{HTML: "<div></div>"}},
		Red:      []solid.Red{{Code: "count()", Reason: "not derivable from props"}},
	}
	var b strings.Builder
	if err := render(&b, program, nil); err == nil {
		t.Error("a program with red holes must not render partial markup")
	}
}

func TestDisabledRendererIsInert(t *testing.T) {
	r := NewRenderer(nil)
	if r.Active() {
		t.Error("a nil config should leave server rendering off")
	}
	if got := r.Unrenderable(nil); got != nil {
		t.Errorf("an inactive renderer should report nothing, got %v", got)
	}
	if err := r.OnPrepare(nil); err != nil {
		t.Errorf("an inactive renderer should not fail Prepare: %v", err)
	}
}
