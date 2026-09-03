package go_solid

import (
	"strings"
	"testing"

	"github.com/lilybw/go-solid/shared/ssr"
)

// A fixed root, so the assertions can name the element the markup has to land
// inside rather than deriving it.
const ssrRoot = "ssr-root"

// Server rendering is a first paint: it fills the mount root the client is
// about to replace. Anywhere else in the document is the wrong place.
func TestSSR_FirstPaintLandsInsideTheMountRoot(t *testing.T) {
	bundler, err := NewEphemeral(nil)
	if err != nil {
		t.Fatal(err)
	}

	rendered, err := bundler.
		Anonymous(`(props) => <h1>{props.title}</h1>`, map[string]string{"title": "Hi"}).
		MountOnRootID(ssrRoot).
		Render()
	if err != nil {
		t.Fatal(err)
	}

	const want = `<div id="` + ssrRoot + `"><h1>Hi</h1></div>`
	if !strings.Contains(rendered.HTML, want) {
		t.Fatalf("no first paint in the mount root; wanted %s\n%s", want, rendered.HTML)
	}
}

// Switching it off has to leave the shell exactly as it was, since that is what
// every consumer who never mentions SSR is relying on.
func TestSSR_DisabledLeavesTheMountEmpty(t *testing.T) {
	bundler, err := NewEphemeral(&EphemeralConfig{SSR: &ssr.SSRConfig{Disabled: true}})
	if err != nil {
		t.Fatal(err)
	}

	rendered, err := bundler.
		Anonymous(`(props) => <h1>{props.title}</h1>`, map[string]string{"title": "Hi"}).
		MountOnRootID(ssrRoot).
		Render()
	if err != nil {
		t.Fatal(err)
	}

	const want = `<div id="` + ssrRoot + `"></div>`
	if !strings.Contains(rendered.HTML, want) {
		t.Fatalf("a disabled renderer still filled the mount root:\n%s", rendered.HTML)
	}
}

// The props island was the only place a prop reached the document, and it is a
// non-executed context. The first paint is markup, so a prop that reads as
// markup now has a second way out.
func TestSSR_EscapesPropsInTheFirstPaint(t *testing.T) {
	bundler, err := NewEphemeral(nil)
	if err != nil {
		t.Fatal(err)
	}

	rendered, err := bundler.
		Anonymous(`(props) => <h1>{props.title}</h1>`, map[string]string{"title": `<script>alert(1)</script>`}).
		MountOnRootID(ssrRoot).
		Render()
	if err != nil {
		t.Fatal(err)
	}

	if strings.Contains(rendered.HTML, "<script>alert(1)") {
		t.Errorf("a prop reached the document as markup:\n%s", rendered.HTML)
	}
	if !strings.Contains(rendered.HTML, "&lt;script&gt;alert(1)") {
		t.Errorf("the first paint did not escape the prop at all:\n%s", rendered.HTML)
	}
}

// A component the compiler cannot describe is an ordinary fall back to client
// rendering: the mount is empty and the render succeeds.
func TestSSR_AComponentNeedingTheClientStillRenders(t *testing.T) {
	bundler, err := NewEphemeral(nil)
	if err != nil {
		t.Fatal(err)
	}

	rendered, err := bundler.Anonymous(clientOnlyComponent, nil).
		MountOnRootID(ssrRoot).
		Render()
	if err != nil {
		t.Fatalf("without Strict a component needing the client should still render: %v", err)
	}

	const want = `<div id="` + ssrRoot + `"></div>`
	if !strings.Contains(rendered.HTML, want) {
		t.Fatalf("the fall back to client rendering did not leave the mount empty:\n%s", rendered.HTML)
	}
}

// Strict turns that fall back into a failure at Prepare, before a request is
// served a blank first paint nobody noticed.
func TestSSR_StrictRefusesAComponentNeedingTheClient(t *testing.T) {
	bundler, err := NewEphemeral(&EphemeralConfig{SSR: &ssr.SSRConfig{Strict: true}})
	if err != nil {
		t.Fatal(err)
	}

	_, err = bundler.Anonymous(clientOnlyComponent, nil).Render()
	if err == nil {
		t.Fatal("Strict must refuse a component that cannot be fully server-rendered")
	}
	if !strings.Contains(err.Error(), "cannot be server-rendered") {
		t.Errorf("the failure does not say why the render was refused: %v", err)
	}
}

const clientOnlyComponent = `
import { createSignal } from "solid-js";
(props) => {
	const [count] = createSignal(0);
	return <p>{count()}</p>;
}
`
