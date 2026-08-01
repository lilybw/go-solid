package go_solid

import (
	"strings"
	"testing"

	"github.com/lilybw/go-solid/internal"
	caching "github.com/lilybw/go-solid/internal/caching"
	networking_int "github.com/lilybw/go-solid/internal/networking"
	networking "github.com/lilybw/go-solid/shared/networking"
)

func TestGenerateEntry_AssignsCorrectIdValues(t *testing.T) {
	comp := internal.NewComponent("auth/LoginForm", "/srv/frontend/components/auth/LoginForm.tsx", ".tsx")
	src, err := internal.GenerateEntry(comp)
	if err != nil {
		t.Fatalf("generateEntry: %v", err)
	}

	// Import path must drop the extension (esbuild resolves it) and be absolute.
	if !strings.Contains(src, `import Component from "/srv/frontend/components/auth/LoginForm"`) {
		t.Errorf("entry missing expected import; got:\n%s", src)
	}
	if strings.Contains(src, ".tsx") {
		t.Errorf("entry import should not include .tsx extension; got:\n%s", src)
	}
	// Must mount and read props from the data island.
	for _, want := range []string{
		`from "solid-js/web"`,
		`auth/LoginForm`,
		`"props-auth-LoginForm-go-solid-root"`,
		`"auth-LoginForm-go-solid-root"`,
		`document.getElementById("auth-LoginForm-go-solid-root")`,
		`document.getElementById("auth-LoginForm-go-solid-root")`,
		`render(() => Component(readProps()), root)`,
	} {
		if !strings.Contains(src, want) {
			t.Errorf("entry missing %q; got:\n%s", want, src)
		}
	}
}

func getTestHeadSegment() networking.HTMLHeadSegmentBuilder {
	return networking_int.NewHTMLHeadSegmentBuilder().
		DeterministicOutput().
		SetTitle("test/test")
}

func TestAssembleHTML_WithCSS(t *testing.T) {
	rendered := &caching.Rendered{
		JSName:  "auth_LoginForm.abc.js",
		CSSName: "auth_LoginForm.def.css",
		CSS:     "/* css to be included */",
	}
	html := internal.AssembleHTML(getTestHeadSegment(), `{"title":"Hi"}`, rendered, "go-solid-root", "")

	for _, want := range []string{
		`<title>test/test</title>`,
		`<style>/* css to be included */</style>`,
		`<script id="props-go-solid-root" type="application/json">{"title":"Hi"}</script>`,
		`<script type="module"></script>`,
		`<div id="go-solid-root"></div>`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("HTML missing %q; got:\n%s", want, html)
		}
	}
}

func TestAssembleHTML_WithoutCSSOmitsLink(t *testing.T) {
	rendered := &caching.Rendered{
		JSName:  "Version.abc.js",
		CSSName: "",
	}
	html := internal.AssembleHTML(getTestHeadSegment(), `{}`, rendered, "go-solid-root", "")
	if strings.Contains(html, "<link") {
		t.Errorf("HTML should omit <link> when cssName empty; got:\n%s", html)
	}
}

// The props JSON is embedded raw inside a <script type="application/json">.
// This test documents that generateEntry/assembleHTML place it in a non-executed
// context (the data island), which is what makes raw embedding acceptable.
func TestAssembleHTML_PropsGoInDataIsland(t *testing.T) {
	rendered := &caching.Rendered{
		JSName:  "c.js",
		CSSName: "",
	}
	html := internal.AssembleHTML(getTestHeadSegment(), `{"x":1}`, rendered, "go-solid-root", "")
	island := `<script id="props-go-solid-root" type="application/json">{"x":1}</script>`
	if !strings.Contains(html, island) {
		t.Errorf("props not placed in application/json data island; got:\n%s", html)
	}
}
