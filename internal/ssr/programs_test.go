package ssr

import (
	"strings"
	"testing"

	"github.com/lilybw/go-solid/internal/sources"
	"github.com/lilybw/go-solid/shared/registry"
	shared_ssr "github.com/lilybw/go-solid/shared/ssr"
)

// A component whose markup is entirely derivable from props.
const greenSource = `export const Hello = (props) => <h1>{props.title}</h1>;`

// A component whose markup reads a signal, which no amount of props can fill.
const redSource = `
import { createSignal } from "solid-js";
export const Counter = (props) => {
	const [count] = createSignal(0);
	return <p>{count()}</p>;
};
`

// held names a component over a source kept in memory. Analysis reads text and
// never a location, so nothing here needs a directory to put a file in.
func held(t *testing.T, name, export, source string) (*sources.Memory, *registry.Component) {
	t.Helper()
	memory := sources.NewMemory()
	path := sources.MemoryPath(name + ".tsx")
	memory.Put(path, source)

	comp := registry.NewComponent(name, path, ".tsx")
	if export == "" {
		return memory, comp
	}
	return memory, comp.WithExport(export)
}

// on returns a renderer with server rendering enabled and nothing else set.
func on(memory *sources.Memory) *Renderer {
	return NewRenderer(&shared_ssr.SSRConfig{}, memory)
}

// ---------------------------------------------------------------------------
// Reaching the right component.
// ---------------------------------------------------------------------------

func TestMarkupRendersFromSource(t *testing.T) {
	memory, comp := held(t, "Hello", "Hello", greenSource)

	got, err := on(memory).Markup(comp, map[string]any{"title": "Hi"})
	if err != nil {
		t.Fatalf("Markup: %v", err)
	}
	if want := "<h1>Hi</h1>"; got != want {
		t.Errorf("markup = %q, want %q", got, want)
	}
}

// A selector names one export of a file that declares several. Rendering the
// wrong one is worse than rendering nothing.
func TestAnalyzeSelectsTheNamedExport(t *testing.T) {
	const twoComponents = `
export const Header = (props) => <header>{props.a}</header>;
export const Footer = (props) => <footer>{props.b}</footer>;
`
	memory, comp := held(t, "Panel", "Footer", twoComponents)

	got, err := on(memory).Markup(comp, map[string]any{"a": "A", "b": "B"})
	if err != nil {
		t.Fatalf("Markup: %v", err)
	}
	if want := "<footer>B</footer>"; got != want {
		t.Errorf("markup = %q, want %q — the selector reached the wrong component", got, want)
	}
}

// Without a selector the default export decides, in each of the shapes a file
// can write one. The last has no name to follow, so it exercises the fallback
// that a file declaring one component means that component.
func TestAnalyzeFollowsTheDefaultExport(t *testing.T) {
	for shape, source := range map[string]string{
		"declaration": `export default function Hello(props) { return <h1>{props.title}</h1>; }`,
		"binding":     greenSource + "\nexport default Hello;",
		"expression":  `export default (props) => <h1>{props.title}</h1>;`,
	} {
		memory, comp := held(t, "Hello", "", source)

		got, err := on(memory).Markup(comp, map[string]any{"title": "Hi"})
		if err != nil {
			t.Errorf("%s: Markup: %v", shape, err)
			continue
		}
		if want := "<h1>Hi</h1>"; got != want {
			t.Errorf("%s: markup = %q, want %q", shape, got, want)
		}
	}
}

// A capitalized helper beside an anonymous default export is not the component
// the file is about, however few others there are to choose from.
func TestAnalyzeDoesNotSettleForTheWrongComponent(t *testing.T) {
	const helperBesideDefault = `
const Row = (props) => <li>{props.item}</li>;
export default (props) => <ul>{props.title}</ul>;
`
	memory, comp := held(t, "List", "", helperBesideDefault)

	got, err := on(memory).Markup(comp, map[string]any{"item": "Item", "title": "Title"})
	if err != nil {
		t.Fatalf("Markup: %v", err)
	}
	if want := "<ul>Title</ul>"; got != want {
		t.Errorf("markup = %q, want %q — a helper was rendered in place of the default export", got, want)
	}
}

// Two components and no default export is a file the selector has to name.
func TestAnalyzeRefusesToGuessBetweenComponents(t *testing.T) {
	const twoNamedExports = `
export const Header = (props) => <header>{props.a}</header>;
export const Footer = (props) => <footer>{props.b}</footer>;
`
	memory, comp := held(t, "Panel", "", twoNamedExports)

	if _, err := on(memory).Markup(comp, nil); err == nil {
		t.Fatal("a file exporting no default component should not have resolved to one")
	}
}

func TestAnalyzeRefusesAnExportItCannotFind(t *testing.T) {
	memory, comp := held(t, "Hello", "Missing", greenSource)

	if _, err := on(memory).Markup(comp, nil); err == nil {
		t.Fatal("a selector naming no component should not have resolved to one")
	}
}

// ---------------------------------------------------------------------------
// Holding an analysis.
// ---------------------------------------------------------------------------

// Analysis is a parse. Repeating it per render would make the first paint cost
// more than the paint it saves.
func TestProgramsAreReusedUntilTheSourceChanges(t *testing.T) {
	memory, comp := held(t, "Hello", "Hello", greenSource)
	renderer := on(memory)

	first, err := renderer.programs.get(comp)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if again, _ := renderer.programs.get(comp); again != first {
		t.Error("an unchanged source was analyzed twice")
	}

	memory.Put(comp.Path, `export const Hello = (props) => <h2>{props.title}</h2>;`)
	changed, err := renderer.programs.get(comp)
	if err != nil {
		t.Fatalf("get after edit: %v", err)
	}
	if changed == first {
		t.Fatal("an edited source served the program analyzed before it")
	}

	got, err := renderer.Markup(comp, map[string]any{"title": "Hi"})
	if err != nil {
		t.Fatalf("Markup after edit: %v", err)
	}
	if want := "<h2>Hi</h2>"; got != want {
		t.Errorf("markup = %q, want %q", got, want)
	}
}

// The watcher invalidates by name when a file it cannot re-stamp changes, so
// the drop has to be enough on its own.
func TestInvalidateForcesReanalysis(t *testing.T) {
	memory, comp := held(t, "Hello", "Hello", greenSource)
	renderer := on(memory)

	first, err := renderer.programs.get(comp)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	renderer.Invalidate(comp.Name)

	if again, _ := renderer.programs.get(comp); again == first {
		t.Error("Invalidate left the analysis in place")
	}
}

// ---------------------------------------------------------------------------
// Components that need the client.
// ---------------------------------------------------------------------------

func TestUnrenderableNamesWhatNeedsTheClient(t *testing.T) {
	memory, comp := held(t, "Counter", "Counter", redSource)
	renderer := on(memory)

	reasons := renderer.Unrenderable(comp)
	if len(reasons) == 0 {
		t.Fatal("a component reading a signal cannot be server-rendered, and nothing said so")
	}
	if _, err := renderer.Markup(comp, nil); err == nil {
		t.Error("a component that needs the client must not produce partial markup")
	}
}

// Falling back to client rendering is the ordinary case; failing the render is
// the opt-in.
func TestOnPrepareOnlyFailsUnderStrict(t *testing.T) {
	memory, comp := held(t, "Counter", "Counter", redSource)

	if err := on(memory).OnPrepare(comp); err != nil {
		t.Errorf("without Strict an unrenderable component should fall back, not fail: %v", err)
	}

	strict := NewRenderer(&shared_ssr.SSRConfig{Strict: true}, memory)
	err := strict.OnPrepare(comp)
	if err == nil {
		t.Fatal("Strict must refuse a component that cannot be fully server-rendered")
	}
	if !strings.Contains(err.Error(), comp.Name) {
		t.Errorf("the failure does not name the component: %v", err)
	}
}

func TestStrictPassesAComponentItCanRender(t *testing.T) {
	memory, comp := held(t, "Hello", "Hello", greenSource)

	strict := NewRenderer(&shared_ssr.SSRConfig{Strict: true}, memory)
	if err := strict.OnPrepare(comp); err != nil {
		t.Errorf("Strict refused a component whose markup is fully derivable: %v", err)
	}
}

func TestDisabledRendererProducesNoMarkup(t *testing.T) {
	memory, comp := held(t, "Hello", "Hello", greenSource)

	off := NewRenderer(&shared_ssr.SSRConfig{Disabled: true}, memory)
	got, err := off.Markup(comp, map[string]any{"title": "Hi"})
	if err != nil || got != "" {
		t.Errorf("Markup = %q, %v; a disabled renderer should produce nothing and no error", got, err)
	}
}
