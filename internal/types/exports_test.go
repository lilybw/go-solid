package types

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lilybw/go-solid/shared/registry"
)

// componentIn writes a source file and describes it as the registry would.
func componentIn(t *testing.T, source string) *registry.Component {
	t.Helper()
	path := filepath.Join(t.TempDir(), "Panel.tsx")
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	return registry.NewComponent("Panel", path, ".tsx")
}

func verify(t *testing.T, comp *registry.Component) error {
	t.Helper()
	return NewExtractor(nil).VerifyComponentExport(comp)
}

// A file whose default export is a component resolves, whatever the function is
// called: the selector names the file, the default export names the component.
func TestVerify_DefaultExportResolvesRegardlessOfItsName(t *testing.T) {
	for name, source := range map[string]string{
		"named declaration":  `export default function AnythingAtAll(props: {}) { return <div/>; }`,
		"anonymous function": `export default function (props: {}) { return <div/>; }`,
		"arrow expression":   `export default (props: {}) => <div/>;`,
		"local binding":      "const Widget = (props: {}) => <div/>;\nexport default Widget;",
	} {
		t.Run(name, func(t *testing.T) {
			if err := verify(t, componentIn(t, source)); err != nil {
				t.Errorf("a valid default export was rejected: %v", err)
			}
		})
	}
}

// The message has to say which of the two mistakes was made, because they have
// different fixes: add an export, or select one that is already there.
func TestVerify_NoDefaultExportSaysSoAndListsWhatThereIs(t *testing.T) {
	comp := componentIn(t, `
export function Header(props: {}) { return <div/>; }
export const Footer = (props: {}) => <div/>;
`)
	err := verify(t, comp)
	if err == nil {
		t.Fatal("a file with no default export was accepted")
	}
	if !IsNotAComponent(err) {
		t.Errorf("err is %T, want a NotAComponentError so rasterization can walk past it", err)
	}
	for _, want := range []string{"no default export", "Panel.tsx", "Header", "Footer"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("message is missing %q:\n%s", want, err)
		}
	}
	// The suggestion has to be a selector the reader can actually paste, but
	// which of the exports it picks is not part of the contract.
	if !strings.Contains(err.Error(), "Panel#Header") && !strings.Contains(err.Error(), "Panel#Footer") {
		t.Errorf("message suggests no usable selector:\n%s", err)
	}
}

// A default export that is not a function is a different mistake again.
func TestVerify_NonComponentDefaultExportIsDistinguished(t *testing.T) {
	err := verify(t, componentIn(t, `export default { title: "not a component" };`))
	if err == nil {
		t.Fatal("a non-function default export was accepted")
	}
	if !strings.Contains(err.Error(), "not a component") {
		t.Errorf("message does not say what is wrong:\n%s", err)
	}
	if strings.Contains(err.Error(), "no default export") {
		t.Errorf("message blames a missing export when one is present:\n%s", err)
	}
}

func TestVerify_SelectorResolvesANamedExport(t *testing.T) {
	comp := componentIn(t, `
export function Header(props: {}) { return <div/>; }
export const Footer = (props: {}) => <div/>;
function Hidden(props: {}) { return <div/>; }
export { Hidden as Aside };
`)
	for _, export := range []string{"Header", "Footer", "Aside"} {
		if err := verify(t, comp.WithExport(export)); err != nil {
			t.Errorf("#%s was rejected: %v", export, err)
		}
	}
}

// Declared-but-not-exported is a missing keyword, not a typo, and saying which
// saves the reader a search.
func TestVerify_DeclaredButNotExportedSaysWhich(t *testing.T) {
	comp := componentIn(t, `
export default function Panel(props: {}) { return <div/>; }
function Sidebar(props: {}) { return <div/>; }
`)
	err := verify(t, comp.WithExport("Sidebar"))
	if err == nil {
		t.Fatal("an unexported declaration was accepted as a selector target")
	}
	if !strings.Contains(err.Error(), "does not export") {
		t.Errorf("message does not distinguish unexported from absent:\n%s", err)
	}
}

func TestVerify_AbsentExportIsReported(t *testing.T) {
	comp := componentIn(t, `export default function Panel(props: {}) { return <div/>; }`)
	err := verify(t, comp.WithExport("Nope"))
	if err == nil {
		t.Fatal("a selector naming nothing was accepted")
	}
	if !strings.Contains(err.Error(), `no exported "Nope"`) {
		t.Errorf("message does not name the missing export:\n%s", err)
	}
}

// An export that exists but holds something else is neither absent nor
// unexported, whatever shape the declaration takes. Blaming a missing export
// keyword here sends the reader to add one that is already there.
func TestVerify_ExportedNonComponentIsReported(t *testing.T) {
	for name, source := range map[string]struct{ decl, export string }{
		"const":         {`export const TITLE = "text";`, "TITLE"},
		"interface":     {`export interface Props { a: string }`, "Props"},
		"type alias":    {`export type Id = string;`, "Id"},
		"class":         {`export class Store {}`, "Store"},
		"export clause": {"const TITLE = \"text\";\nexport { TITLE };", "TITLE"},
		"renamed":       {"const t = \"text\";\nexport { t as TITLE };", "TITLE"},
	} {
		t.Run(name, func(t *testing.T) {
			comp := componentIn(t, `
export default function Panel(props: {}) { return <div/>; }
`+source.decl+`
`)
			err := verify(t, comp.WithExport(source.export))
			if err == nil {
				t.Fatal("a non-component export was accepted")
			}
			if !strings.Contains(err.Error(), "not a component") {
				t.Errorf("message does not say what is wrong:\n%s", err)
			}
			if strings.Contains(err.Error(), "does not export") {
				t.Errorf("message blames a missing export keyword for something exported:\n%s", err)
			}
			if strings.Contains(err.Error(), "has no exported") {
				t.Errorf("message calls an existing export absent:\n%s", err)
			}
		})
	}
}

// Selector names go into generated JavaScript as import clauses, so a name that
// is not an identifier is refused before it gets there.
func TestVerify_RejectsExportNamesThatCannotBeImported(t *testing.T) {
	comp := componentIn(t, `export default function Panel(props: {}) { return <div/>; }`)
	for _, bad := range []string{"has space", "1Leading", "semi;colon", "quote\"mark", "dash-ed"} {
		if err := verify(t, comp.WithExport(bad)); err == nil {
			t.Errorf("export name %q was accepted", bad)
		}
	}
}

func TestExportedComponents_ListsOnlyFunctionValuedExports(t *testing.T) {
	comp := componentIn(t, `
export default function Panel(props: {}) { return <div/>; }
export function Header(props: {}) { return <div/>; }
export const Footer = (props: {}) => <div/>;
export const TITLE = "text";
export interface Props { a: string }
function Hidden(props: {}) { return <div/>; }
`)
	got, err := NewExtractor(nil).ExportedComponents(comp.Path)
	if err != nil {
		t.Fatalf("ExportedComponents: %v", err)
	}
	want := []string{"Footer", "Header"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v (sorted)", got, want)
		}
	}
}
