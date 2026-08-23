package networking

import (
	"strings"
	"testing"

	. "github.com/lilybw/go-solid/shared/networking"
)

// DeterministicOutput exists so a test can compare generated HTML. That is only
// true if *everything* the builder emits is ordered — tags and attributes both.
// Attributes live in a map, so without an explicit sort the output varies run to
// run and the mode delivers nothing.

func deterministicSegment() string {
	return NewHTMLHeadSegmentBuilder().
		DeterministicOutput().
		SetTitle("Home").
		AddUnique("base", "/app/").
		Add(HTMLTag{Name: "meta", HTMLTagAttributes: map[string]string{
			"charset": "utf-8", "name": "viewport", "content": "width=device-width",
		}}).
		AddLink("stylesheet", "/app.css").
		Build()
}

func TestDeterministicOutput_IsStableAcrossBuilders(t *testing.T) {
	want := deterministicSegment()
	// One run can agree with itself by luck; a map with three keys has six
	// orderings, so repeat enough that an unsorted emit is near-certain to show.
	for i := range 50 {
		if got := deterministicSegment(); got != want {
			t.Fatalf("run %d differed from run 0:\n got %q\nwant %q", i, got, want)
		}
	}
}

func TestDeterministicOutput_OrdersAttributes(t *testing.T) {
	got := NewHTMLHeadSegmentBuilder().
		DeterministicOutput().
		Add(HTMLTag{Name: "meta", HTMLTagAttributes: map[string]string{
			"name": "viewport", "content": "width=device-width",
		}}).
		Build()
	if !strings.Contains(got, `<meta content="width=device-width" name="viewport">`) {
		t.Errorf("attributes not emitted in name order; got %q", got)
	}
}

func TestDeterministicOutput_OrdersTags(t *testing.T) {
	got := NewHTMLHeadSegmentBuilder().
		DeterministicOutput().
		Add(HTMLTag{Name: "script", InnerHTML: "init()"}).
		Add(HTMLTag{Name: "base", InnerHTML: "/app/"}).
		Build()
	if strings.Index(got, "<base>") > strings.Index(got, "<script>") {
		t.Errorf("tags not emitted in name order; got %q", got)
	}
}

// Two tags sharing a name carry no ordering of their own, so the sort must be
// stable or which one comes first flips between runs.
func TestDeterministicOutput_KeepsInsertionOrderWithinAName(t *testing.T) {
	build := func() string {
		return NewHTMLHeadSegmentBuilder().
			DeterministicOutput().
			Add(HTMLTag{Name: "meta", HTMLTagAttributes: map[string]string{"name": "first"}}).
			Add(HTMLTag{Name: "meta", HTMLTagAttributes: map[string]string{"name": "second"}}).
			Build()
	}
	want := build()
	if strings.Index(want, `"first"`) > strings.Index(want, `"second"`) {
		t.Fatalf("duplicate tag names were reordered; got %q", want)
	}
	for range 20 {
		if got := build(); got != want {
			t.Fatalf("duplicate tag order is unstable:\n got %q\nwant %q", got, want)
		}
	}
}

// Build reads the builder; it must not write to it. Appending the unique tags
// onto this.rest lands in its spare capacity, and sorting reorders the builder's
// own slice — both observable on a second call.
func TestBuild_DoesNotMutateTheBuilder(t *testing.T) {
	b := NewHTMLHeadSegmentBuilder().
		DeterministicOutput().
		SetTitle("Home").
		Add(HTMLTag{Name: "script", InnerHTML: "init()"})

	first := b.Build()
	if second := b.Build(); first != second {
		t.Errorf("Build is not idempotent:\nfirst  %q\nsecond %q", first, second)
	}

	// A tag added after a Build must land at the end, not overwrite whatever
	// the previous Build parked in the spare capacity.
	b.Add(HTMLTag{Name: "noscript", InnerHTML: "no js"})
	third := b.Build()
	for _, want := range []string{"<title>Home</title>", "<script>init()</script>", "<noscript>no js</noscript>"} {
		if !strings.Contains(third, want) {
			t.Errorf("Build lost %q after a prior Build; got %q", want, third)
		}
	}
}

// Without DeterministicOutput the order is unspecified, but the content is not.
func TestNonDeterministicOutputStillEmitsEverything(t *testing.T) {
	got := NewHTMLHeadSegmentBuilder().
		Add(HTMLTag{Name: "meta", HTMLTagAttributes: map[string]string{"charset": "utf-8"}}).
		Build()
	if !strings.Contains(got, `charset="utf-8"`) || !strings.Contains(got, "<title>go-solid</title>") {
		t.Errorf("unordered build dropped content; got %q", got)
	}
}
