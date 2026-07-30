package go_solid

import (
	"strings"
	"testing"
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

func TestAssembleHTML_WithCSS(t *testing.T) {
	html := assembleHTML("auth/LoginForm", `{"title":"Hi"}`, "auth_LoginForm.abc.js", "auth_LoginForm.def.css")

	for _, want := range []string{
		`<title>auth/LoginForm</title>`,
		`<link rel="stylesheet" href="/static/dist/auth_LoginForm.def.css">`,
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
	html := assembleHTML("Version", `{}`, "Version.abc.js", "")
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
	html := assembleHTML("C", `{"x":1}`, "c.js", "")
	island := `<script id="solidbundle-props" type="application/json">{"x":1}</script>`
	if !strings.Contains(html, island) {
		t.Errorf("props not placed in application/json data island; got:\n%s", html)
	}
}
