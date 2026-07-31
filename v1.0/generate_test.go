package go_solid

import (
	"strings"
	"testing"

	caching "github.com/lilybw/go_solid/internal/caching"
)

func TestGenerateEntry_ImportsComponentWithoutExtension(t *testing.T) {
	comp := Component{
		Name:    "auth/LoginForm",
		AbsPath: "/srv/frontend/components/auth/LoginForm.tsx",
		Ext:     ".tsx",
	}
	src, err := generateEntry(comp, "/srv/frontend")
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
		`getElementById("solidbundle-props")`,
		`getElementById("solidbundle-root")`,
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
	html := assembleHTML(getTestHeadSegment(), `{"title":"Hi"}`, rendered)

	for _, want := range []string{
		`<title>test/test</title>`,
		`<link href="/static/dist/auth_LoginForm.def.css" rel="stylesheet">`,
		`<script id="solidbundle-props" type="application/json">{"title":"Hi"}</script>`,
		`<script type="module" src="/static/dist/auth_LoginForm.abc.js"></script>`,
		`<div id="solidbundle-root"></div>`,
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
	html := assembleHTML(getTestHeadSegment(), `{}`, rendered)
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
	html := assembleHTML(getTestHeadSegment(), `{"x":1}`, rendered)
	island := `<script id="solidbundle-props" type="application/json">{"x":1}</script>`
	if !strings.Contains(html, island) {
		t.Errorf("props not placed in application/json data island; got:\n%s", html)
	}
}
