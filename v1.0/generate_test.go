package go_solid

import (
	"strings"
	"testing"

	caching "github.com/lilybw/go_solid/internal/caching"
)

func TestGenerateEntry_ImportsComponentWithoutExtension(t *testing.T) {
	comp := newComponent("auth/LoginForm", "/srv/frontend/components/auth/LoginForm.tsx", ".tsx")
	src, err := generateEntry(comp)
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
		`const compName = "auth/LoginForm";`,
		`const propsMountId = "props-auth-LoginForm-go-solid-root"`,
		`const compMountId = "auth-LoginForm-go-solid-root"`,
		`document.getElementById(propsMountId)`,
		`document.getElementById(compMountId)`,
		`render(() => Component(readProps()), root)`,
	} {
		if !strings.Contains(src, want) {
			t.Errorf("entry missing %q; got:\n%s", want, src)
		}
	}
}

func getTestHeadSegment() HTMLHeadSegmentBuilder {
	return newHTMLHeadSegmentBuilder().
		DeterministicOutput().
		SetTitle("test/test")
}

func TestAssembleHTML_WithCSS(t *testing.T) {
	rendered := &caching.Rendered{
		JSName:  "auth_LoginForm.abc.js",
		CSSName: "auth_LoginForm.def.css",
	}
	html := assembleHTML(getTestHeadSegment(), `{"title":"Hi"}`, rendered, "go-solid-root")

	for _, want := range []string{
		`<title>test/test</title>`,
		`<link href="/static/dist/auth_LoginForm.def.css" rel="stylesheet">`,
		`<script id="props-go-solid-root" type="application/json">{"title":"Hi"}</script>`,
		`<script type="module" src="/static/dist/auth_LoginForm.abc.js"></script>`,
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
	html := assembleHTML(getTestHeadSegment(), `{}`, rendered, "go-solid-root")
	if strings.Contains(html, "<link") {
		t.Errorf("HTML should omit <link> when cssName empty; got:\n%s", html)
	}
	if !strings.Contains(html, `src="/static/dist/Version.abc.js"`) {
		t.Errorf("HTML missing JS script tag; got:\n%s", html)
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
	html := assembleHTML(getTestHeadSegment(), `{"x":1}`, rendered, "go-solid-root")
	island := `<script id="props-go-solid-root" type="application/json">{"x":1}</script>`
	if !strings.Contains(html, island) {
		t.Errorf("props not placed in application/json data island; got:\n%s", html)
	}
}
