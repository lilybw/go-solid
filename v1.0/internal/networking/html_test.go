package networking

import (
	"strings"
	"testing"
)

func TestNewBuilderHasDefaultTitle(t *testing.T) {
	got := NewHTMLHeadSegmentBuilder().Build()
	if !strings.Contains(got, "<title>go-solid</title>") {
		t.Errorf("expected default title, got %q", got)
	}
}

func TestSetTitleOverridesDefault(t *testing.T) {
	got := NewHTMLHeadSegmentBuilder().SetTitle("My Page").Build()
	if !strings.Contains(got, "<title>My Page</title>") {
		t.Errorf("expected custom title, got %q", got)
	}
	if strings.Contains(got, "go-solid") {
		t.Errorf("default title should have been overwritten, got %q", got)
	}
}

func TestSetTitleLastWins(t *testing.T) {
	got := NewHTMLHeadSegmentBuilder().SetTitle("First").SetTitle("Second").Build()
	if strings.Count(got, "<title>") != 1 {
		t.Errorf("expected exactly one title tag, got %q", got)
	}
	if !strings.Contains(got, "<title>Second</title>") {
		t.Errorf("expected last title to win, got %q", got)
	}
}

func TestAddUnique(t *testing.T) {
	got := NewHTMLHeadSegmentBuilder().AddUnique("base", "/root/").Build()
	if !strings.Contains(got, "<base>/root/</base>") {
		t.Errorf("expected unique tag, got %q", got)
	}
}

func TestAddUniqueOverwritesSameKey(t *testing.T) {
	got := NewHTMLHeadSegmentBuilder().
		AddUnique("base", "/first/").
		AddUnique("base", "/second/").
		Build()
	if strings.Contains(got, "/first/") {
		t.Errorf("first value should have been overwritten, got %q", got)
	}
	if !strings.Contains(got, "<base>/second/</base>") {
		t.Errorf("expected second value, got %q", got)
	}
}

func TestAddUniqueCanOverrideTitle(t *testing.T) {
	got := NewHTMLHeadSegmentBuilder().AddUnique("title", "Via AddUnique").Build()
	if strings.Count(got, "<title>") != 1 {
		t.Errorf("expected exactly one title tag, got %q", got)
	}
	if !strings.Contains(got, "<title>Via AddUnique</title>") {
		t.Errorf("expected AddUnique to override title, got %q", got)
	}
}

func TestAddTagWithInnerHTML(t *testing.T) {
	got := NewHTMLHeadSegmentBuilder().Add(HTMLTag{
		Name:      "script",
		InnerHTML: "console.log('hi')",
	}).Build()
	if !strings.Contains(got, "<script>console.log('hi')</script>") {
		t.Errorf("expected script tag with inner html, got %q", got)
	}
}

func TestAddTagWithoutInnerHTML(t *testing.T) {
	got := NewHTMLHeadSegmentBuilder().Add(HTMLTag{Name: "meta"}).Build()
	if !strings.Contains(got, "<meta></meta>") {
		t.Errorf("expected empty meta tag, got %q", got)
	}
}

func TestAddTagWithSingleAttribute(t *testing.T) {
	got := NewHTMLHeadSegmentBuilder().Add(HTMLTag{
		Name:              "meta",
		HTMLTagAttributes: map[string]string{"charset": "utf-8"},
	}).Build()
	if !strings.Contains(got, `<meta charset="utf-8">`) {
		t.Errorf("expected meta with charset, got %q", got)
	}
	if !strings.Contains(got, "</meta>") {
		t.Errorf("expected closing meta tag, got %q", got)
	}
}

func TestAddTagWithMultipleAttributes(t *testing.T) {
	got := NewHTMLHeadSegmentBuilder().Add(HTMLTag{
		Name: "meta",
		HTMLTagAttributes: map[string]string{
			"name":    "viewport",
			"content": "width=device-width",
		},
	}).Build()
	// Attribute order is non-deterministic; check each independently.
	if !strings.Contains(got, `name="viewport"`) {
		t.Errorf("missing name attribute, got %q", got)
	}
	if !strings.Contains(got, `content="width=device-width"`) {
		t.Errorf("missing content attribute, got %q", got)
	}
	if !strings.Contains(got, "<meta ") {
		t.Errorf("expected meta opening tag, got %q", got)
	}
}

func TestAddMultipleTagsAllPresent(t *testing.T) {
	got := NewHTMLHeadSegmentBuilder().
		Add(HTMLTag{Name: "meta", HTMLTagAttributes: map[string]string{"charset": "utf-8"}}).
		Add(HTMLTag{Name: "link", HTMLTagAttributes: map[string]string{"rel": "stylesheet", "href": "/a.css"}}).
		Build()
	if !strings.Contains(got, `charset="utf-8"`) {
		t.Errorf("missing first tag, got %q", got)
	}
	if !strings.Contains(got, `href="/a.css"`) {
		t.Errorf("missing second tag, got %q", got)
	}
}

func TestAddDuplicateTagsBothKept(t *testing.T) {
	got := NewHTMLHeadSegmentBuilder().
		Add(HTMLTag{Name: "script", HTMLTagAttributes: map[string]string{"src": "/a.js"}}).
		Add(HTMLTag{Name: "script", HTMLTagAttributes: map[string]string{"src": "/b.js"}}).
		Build()
	if strings.Count(got, "<script") != 2 {
		t.Errorf("expected both script tags kept (rest is not deduped), got %q", got)
	}
}

func TestChainingReturnsSameBuilder(t *testing.T) {
	b := NewHTMLHeadSegmentBuilder()
	if b.SetTitle("x") != b {
		t.Error("SetTitle should return the same builder")
	}
	if b.AddUnique("k", "v") != b {
		t.Error("AddUnique should return the same builder")
	}
	if b.Add(HTMLTag{Name: "meta"}) != b {
		t.Error("Add should return the same builder")
	}
}

func TestBuildEndsTagsWithNewline(t *testing.T) {
	got := NewHTMLHeadSegmentBuilder().Build()
	if !strings.HasSuffix(got, "\n") {
		t.Errorf("expected trailing newline, got %q", got)
	}
}

func TestFullHeadSegment(t *testing.T) {
	got := NewHTMLHeadSegmentBuilder().
		SetTitle("Home").
		AddUnique("base", "/app/").
		Add(HTMLTag{Name: "meta", HTMLTagAttributes: map[string]string{"charset": "utf-8"}}).
		Add(HTMLTag{Name: "script", InnerHTML: "init()"}).
		Build()

	for _, want := range []string{
		"<title>Home</title>",
		"<base>/app/</base>",
		`charset="utf-8"`,
		"<script>init()</script>",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("full segment missing %q, got %q", want, got)
		}
	}
}
