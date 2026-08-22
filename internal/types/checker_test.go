package types

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lilybw/go-solid/shared/registry"
	shared_types "github.com/lilybw/go-solid/shared/types"
)

// collector is a Reporter that keeps what it is handed.
type collector struct{ diagnostics []Diagnostic }

func (c *collector) report(diagnostics []Diagnostic) {
	c.diagnostics = append(c.diagnostics, diagnostics...)
}

func (c *collector) kinds() []DiagnosticKind {
	out := make([]DiagnosticKind, 0, len(c.diagnostics))
	for _, d := range c.diagnostics {
		out = append(out, d.Kind)
	}
	return out
}

func (c *collector) has(kind DiagnosticKind) bool {
	for _, d := range c.diagnostics {
		if d.Kind == kind {
			return true
		}
	}
	return false
}

// harness wires a checker over a throwaway components tree.
type harness struct {
	t          *testing.T
	workspace  string
	components string
	collected  *collector
	checker    *Checker
}

func newHarness(t *testing.T, mode shared_types.CheckMode) *harness {
	t.Helper()
	root := t.TempDir()
	h := &harness{
		t:          t,
		workspace:  filepath.Join(root, "workspace"),
		components: filepath.Join(root, "components"),
		collected:  &collector{},
	}
	if err := os.MkdirAll(h.components, 0o755); err != nil {
		t.Fatal(err)
	}
	h.checker = NewChecker(h.workspace, mode, h.collected.report)
	return h
}

// component writes a source file and returns its registry entry.
func (h *harness) component(name, source string) *registry.Component {
	h.t.Helper()
	path := filepath.Join(h.components, filepath.FromSlash(name)+".tsx")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		h.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		h.t.Fatal(err)
	}
	return registry.NewComponent(name, path, ".tsx")
}

type titleProps struct {
	Title string `json:"title"`
}

func TestChecker_PropsSatisfyingTheComponentAreQuiet(t *testing.T) {
	h := newHarness(t, shared_types.CHECK_RUNTIME_AND_BOOT)
	component := h.component("auth/LoginForm",
		`export default function LoginForm(props: { title: string }) { return <div/>; }`)

	h.checker.OnPrepare(component, titleProps{Title: "hi"})

	if len(h.collected.diagnostics) != 0 {
		t.Fatalf("matching props should be quiet, got %v", h.collected.kinds())
	}
}

// Covariance: the component reads less than it is given, which is fine.
func TestChecker_WiderPropsAreAccepted(t *testing.T) {
	h := newHarness(t, shared_types.CHECK_RUNTIME)
	component := h.component("Hello",
		`export default function Hello(props: { title: string }) { return <div/>; }`)

	type widerProps struct {
		Extra string `json:"extra"`
		Title string `json:"title"`
	}
	h.checker.OnPrepare(component, widerProps{Title: "hi", Extra: "more"})

	if len(h.collected.diagnostics) != 0 {
		t.Fatalf("extra props are fine, got %v", h.collected.kinds())
	}
}

func TestChecker_MissingRequiredPropIsReported(t *testing.T) {
	h := newHarness(t, shared_types.CHECK_RUNTIME)
	component := h.component("Hello",
		`export default function Hello(props: { title: string; count: number }) { return <div/>; }`)

	h.checker.OnPrepare(component, titleProps{Title: "hi"})

	if !h.collected.has(DIAG_PROPS) {
		t.Fatalf("expected a props diagnostic, got %v", h.collected.kinds())
	}
}

func TestChecker_IncompatiblePropTypeIsReported(t *testing.T) {
	h := newHarness(t, shared_types.CHECK_RUNTIME)
	component := h.component("Hello",
		`export default function Hello(props: { title: number }) { return <div/>; }`)

	h.checker.OnPrepare(component, titleProps{Title: "hi"})

	if !h.collected.has(DIAG_PROPS) {
		t.Fatalf("expected a props diagnostic, got %v", h.collected.kinds())
	}
}

func TestChecker_UnsuppliedOptionalPropIsQuiet(t *testing.T) {
	h := newHarness(t, shared_types.CHECK_RUNTIME)
	component := h.component("Hello",
		`export default function Hello(props: { title: string; note?: string }) { return <div/>; }`)

	h.checker.OnPrepare(component, titleProps{Title: "hi"})

	if len(h.collected.diagnostics) != 0 {
		t.Fatalf("an unsupplied optional prop is fine, got %v", h.collected.kinds())
	}
}

// Two call sites no longer have to agree with each other, only with the
// component. Nothing is adopted, so there is no first-writer to disagree with.
func TestChecker_DifferentCallSitesAreCheckedIndependently(t *testing.T) {
	h := newHarness(t, shared_types.CHECK_RUNTIME)
	component := h.component("Hello",
		`export default function Hello(props: { title: string }) { return <div/>; }`)

	type widerProps struct {
		Title string `json:"title"`
		Extra int    `json:"extra"`
	}
	h.checker.OnPrepare(component, titleProps{Title: "one"})
	h.checker.OnPrepare(component, widerProps{Title: "two", Extra: 2})

	if len(h.collected.diagnostics) != 0 {
		t.Fatalf("both call sites satisfy the component, got %v", h.collected.kinds())
	}
}

func TestChecker_PropsForAComponentThatTakesNoneIsReported(t *testing.T) {
	h := newHarness(t, shared_types.CHECK_RUNTIME)
	component := h.component("Hello", `export default function Hello() { return <div/>; }`)

	h.checker.OnPrepare(component, titleProps{Title: "hi"})

	if !h.collected.has(DIAG_NO_PROPS) {
		t.Fatalf("expected a no-props diagnostic, got %v", h.collected.kinds())
	}
}

func TestChecker_UntypedComponentIsNotCheckedAtRuntime(t *testing.T) {
	h := newHarness(t, shared_types.CHECK_RUNTIME)
	component := h.component("Hello", `export default function Hello(props) { return <div/>; }`)

	h.checker.OnPrepare(component, titleProps{Title: "hi"})

	if len(h.collected.diagnostics) != 0 {
		t.Fatalf("an untyped component is reported at boot, not per render, got %v", h.collected.kinds())
	}
}

// A value that marshals fine but not as an object reaches the component as
// something it cannot destructure, and nothing else reports that.
func TestChecker_UnderivablePropsAreReported(t *testing.T) {
	h := newHarness(t, shared_types.CHECK_RUNTIME)
	component := h.component("Hello", `export default function Hello(props) { return <div/>; }`)

	h.checker.OnPrepare(component, "not an object")

	if !h.collected.has(DIAG_UNDERIVABLE) {
		t.Fatalf("expected an underivable diagnostic, got %v", h.collected.kinds())
	}
}

// A value json cannot encode at all fails the render with its own error and
// event, so repeating it here would just be noise on someone else's test.
func TestChecker_UnmarshalablePropsAreLeftToTheRenderPath(t *testing.T) {
	h := newHarness(t, shared_types.CHECK_RUNTIME)
	component := h.component("Hello", `export default function Hello(props) { return <div/>; }`)

	h.checker.OnPrepare(component, make(chan int))

	if len(h.collected.diagnostics) != 0 {
		t.Fatalf("an unmarshalable value is Render's to report, got %v", h.collected.kinds())
	}
}

func TestChecker_NeverReportsNothing(t *testing.T) {
	h := newHarness(t, shared_types.CHECK_NEVER)
	component := h.component("Hello",
		`export default function Hello(props: { title: number }) { return <div/>; }`)

	h.checker.OnPrepare(component, titleProps{Title: "hi"})
	h.checker.OnBoot([]*registry.Component{component})

	if len(h.collected.diagnostics) != 0 {
		t.Fatalf("CHECK_NEVER must report nothing, got %v", h.collected.kinds())
	}
}

// Extraction is not gated on the mode: the cache is warm even under NEVER.
func TestChecker_BootCachesEveryComponentWhateverTheMode(t *testing.T) {
	for _, mode := range []shared_types.CheckMode{
		shared_types.CHECK_NEVER,
		shared_types.CHECK_RUNTIME,
		shared_types.CHECK_BOOT,
		shared_types.CHECK_RUNTIME_AND_BOOT,
	} {
		h := newHarness(t, mode)
		component := h.component("auth/LoginForm",
			`export default function LoginForm(props: { title: string }) { return <div/>; }`)

		h.checker.OnBoot([]*registry.Component{component})

		if _, err := os.Stat(h.checker.Cache().Path("auth/LoginForm")); err != nil {
			t.Errorf("%s: no cache entry written at boot: %v", mode, err)
		}
	}
}

func TestChecker_BootNamesComponentsItCannotCheck(t *testing.T) {
	h := newHarness(t, shared_types.CHECK_BOOT)
	untyped := h.component("Untyped", `export default function Untyped(props) { return <div/>; }`)
	typed := h.component("Typed",
		`export default function Typed(props: { title: string }) { return <div/>; }`)
	noProps := h.component("NoProps", `export default function NoProps() { return <div/>; }`)

	h.checker.OnBoot([]*registry.Component{untyped, typed, noProps})

	if !h.collected.has(DIAG_UNTYPED) {
		t.Fatalf("expected an untyped diagnostic, got %v", h.collected.kinds())
	}
	if len(h.collected.diagnostics) != 1 {
		t.Fatalf("only the untyped component should be named, got %+v", h.collected.diagnostics)
	}
	if h.collected.diagnostics[0].Component != "Untyped" {
		t.Errorf("named %q, want Untyped", h.collected.diagnostics[0].Component)
	}
}

func TestChecker_RuntimeOnlyIsSilentAtBoot(t *testing.T) {
	h := newHarness(t, shared_types.CHECK_RUNTIME)
	component := h.component("Untyped", `export default function Untyped(props) { return <div/>; }`)

	h.checker.OnBoot([]*registry.Component{component})

	if len(h.collected.diagnostics) != 0 {
		t.Fatalf("CHECK_RUNTIME must not report at boot, got %v", h.collected.kinds())
	}
}

func TestChecker_BootPrunesOrphans(t *testing.T) {
	h := newHarness(t, shared_types.CHECK_NEVER)
	kept := h.component("Kept", `export default function Kept(props: { title: string }) { return <div/>; }`)
	gone := h.component("Gone", `export default function Gone(props: { title: string }) { return <div/>; }`)
	h.checker.OnBoot([]*registry.Component{kept, gone})

	h.checker.OnBoot([]*registry.Component{kept})

	if _, err := os.Stat(h.checker.Cache().Path("Kept")); err != nil {
		t.Error("a live entry must survive the boot pass")
	}
	if _, err := os.Stat(h.checker.Cache().Path("Gone")); err == nil {
		t.Error("an orphaned entry should have been pruned at boot")
	}
}

// Editing a component changes what props must satisfy, with no invalidation
// call needed.
func TestChecker_PicksUpAnEditedComponent(t *testing.T) {
	h := newHarness(t, shared_types.CHECK_RUNTIME)
	component := h.component("Hello",
		`export default function Hello(props: { title: string }) { return <div/>; }`)
	h.checker.OnPrepare(component, titleProps{Title: "hi"})
	if len(h.collected.diagnostics) != 0 {
		t.Fatalf("baseline should be quiet, got %v", h.collected.kinds())
	}

	h.component("Hello",
		`export default function Hello(props: { title: string; added: number }) { return <div/>; }`)
	future := time.Now().Add(time.Second)
	if err := os.Chtimes(component.Path, future, future); err != nil {
		t.Fatal(err)
	}

	h.checker.OnPrepare(component, titleProps{Title: "hi"})

	if !h.collected.has(DIAG_PROPS) {
		t.Fatalf("the new requirement should be reported, got %v", h.collected.kinds())
	}
}

func TestChecker_NilReceiverAndComponentAreSafe(t *testing.T) {
	var checker *Checker
	checker.OnPrepare(nil, titleProps{})
	checker.OnBoot(nil)
	checker.Invalidate("Hello")

	h := newHarness(t, shared_types.CHECK_RUNTIME)
	h.checker.OnPrepare(nil, titleProps{})
	if len(h.collected.diagnostics) != 0 {
		t.Error("a nil component is not a finding")
	}
}
