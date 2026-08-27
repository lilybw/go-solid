// Package shim_b is an end-to-end "scenario" test for the props contract
// between a consumer's Go handlers and their SolidJS components.
//
// It emulates the workflow of an application that types its pages: components
// declare what props they need, Go handlers pass structs, and go_solid says so
// when the two disagree. The call shape is the one an application uses —
//
//	bundler := go_solid.New(&Config{Components: ...})
//	bundler.Prepare(name, props)
//
// — and the assertions are on what a consumer actually observes: the log.
//
// # Why this shim needs no toolchain
//
// The contract is checked in Prepare, before anything is bundled, so the
// scenario runs with generation disabled and never invokes Node, esbuild or the
// solid transform. That makes it hermetic and fast, and it runs in a CI job
// with no JavaScript toolchain at all. shim-a covers the render path, which
// does need one.
//
// # Why the tree is staged into a temp directory
//
// Booting a bundler writes a type cache into the workspace, and cache entries
// hold absolute paths and content digests of the machine that wrote them.
// Staging testdata into t.TempDir() per test keeps those artifacts out of the
// repository and lets each test edit components without disturbing its
// neighbours.
package shim_b

import (
	"bytes"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	go_solid "github.com/lilybw/go-solid"
	types_int "github.com/lilybw/go-solid/internal/types"
	shared_esbuild "github.com/lilybw/go-solid/shared/esbuild"
	"github.com/lilybw/go-solid/shared/logging"
	"github.com/lilybw/go-solid/shared/networking"
	"github.com/lilybw/go-solid/shared/networking/events"
	shared_types "github.com/lilybw/go-solid/shared/types"
)

// project is a staged copy of the consumer's frontend.
type project struct {
	components string
	workspace  string
}

// newProject stages testdata into a temp directory and plants the definition
// go_solid will one day synthesise, so pages/Article.tsx has something to
// import.
func newProject(t *testing.T) *project {
	t.Helper()

	components := filepath.Join(t.TempDir(), "components")
	if err := os.CopyFS(components, os.DirFS(filepath.Join("testdata", "frontend", "components"))); err != nil {
		t.Fatalf("stage components: %v", err)
	}

	// The default workspace, which is what pages/Article.tsx imports through.
	workspace := filepath.Join(components, ".go_solid")
	published := filepath.Join(workspace, shared_types.TYPES_DIR_NAME)
	if err := os.MkdirAll(published, 0o755); err != nil {
		t.Fatalf("create published surface: %v", err)
	}
	navigation, err := os.ReadFile(filepath.Join("testdata", "frontend", "generated", "navigation.d.ts"))
	if err != nil {
		t.Fatalf("read staged definition: %v", err)
	}
	if err := os.WriteFile(filepath.Join(published, "navigation.d.ts"), navigation, 0o644); err != nil {
		t.Fatalf("publish definition: %v", err)
	}

	return &project{components: components, workspace: workspace}
}

// boot starts a bundler over the project with bundling off.
func (p *project) boot(t *testing.T, check shared_types.CheckMode) *go_solid.Bundler {
	t.Helper()
	return p.bootAt(t, check, logging.LEVEL_ERROR)
}

func (p *project) bootAt(t *testing.T, check shared_types.CheckMode, level logging.LogLevel) *go_solid.Bundler {
	t.Helper()

	bundler, err := go_solid.New(&go_solid.Config{
		Components: p.components,
		LogLevel:   level,
		Generation: &shared_esbuild.BundlerConfig{Disabled: true},
		Types:      &shared_types.TypesConfig{Check: check},
	})
	if err != nil {
		t.Fatalf("go_solid.New: %v", err)
	}
	t.Cleanup(bundler.Close)
	return bundler
}

func (p *project) cacheEntry(component string) string {
	return filepath.Join(p.workspace, types_int.CACHE_DIR_NAME,
		filepath.FromSlash(component)+types_int.CACHE_ENTRY_EXT)
}

func (p *project) componentFile(rel string) string {
	return filepath.Join(p.components, filepath.FromSlash(rel))
}

// TYPE_FAULT_MARKER opens every error the type checker produces, which is how
// the scenario tells a contract fault apart from any other render failure.
const TYPE_FAULT_MARKER = "[go_solid/types]"

// typeFault renders and returns the type fault, if the render was stopped by
// one. Bundling is off in this scenario, so a render that clears the type check
// still fails further down; that failure is not this shim's business and comes
// back as "".
func typeFault(t *testing.T, b *go_solid.Bundler, component string, props any) string {
	t.Helper()
	_, err := b.Prepare(component, props).Render()
	if err == nil || !strings.Contains(err.Error(), TYPE_FAULT_MARKER) {
		return ""
	}
	return err.Error()
}

// observe captures what go_solid logs while fn runs. Coverage gaps are reported
// this way rather than as errors, so this is how the scenario sees them.
//
// The logger is process-global, so nothing here may run in parallel.
func observe(t *testing.T, fn func()) string {
	t.Helper()
	var buf bytes.Buffer
	flags := log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	defer func() {
		log.SetOutput(os.Stderr)
		log.SetFlags(flags)
	}()
	fn()
	return buf.String()
}

func assertAccepted(t *testing.T, what, fault string) {
	t.Helper()
	if fault != "" {
		t.Fatalf("%s should have been accepted, got:\n%s", what, fault)
	}
}

func assertRejected(t *testing.T, what, fault string, fragments ...string) {
	t.Helper()
	if fault == "" {
		t.Fatalf("%s should have failed the render, but it was accepted", what)
	}
	for _, fragment := range fragments {
		if !strings.Contains(fault, fragment) {
			t.Errorf("%s: fault should mention %q, got:\n%s", what, fragment, fault)
		}
	}
}

// The props a consumer's handlers would pass.
type dashboardProps struct {
	Title  string `json:"title"`
	Unread int    `json:"unread"`
	Note   string `json:"note,omitempty"`
}

type profileProps struct {
	UserID      string `json:"userId"`
	DisplayName string `json:"displayName"`
}

type articleProps struct {
	Slug        string `json:"slug"`
	CurrentPath string `json:"currentPath"`
}

// ---------------------------------------------------------------- boot

func TestBootCachesEveryComponent(t *testing.T) {
	p := newProject(t)
	p.boot(t, shared_types.CHECK_RUNTIME_AND_BOOT)

	for _, component := range []string{
		"pages/Dashboard", "pages/Profile", "pages/Article", "widgets/Clock", "legacy/Banner",
	} {
		if _, err := os.Stat(p.cacheEntry(component)); err != nil {
			t.Errorf("no cache entry for %q after boot: %v", component, err)
		}
	}
}

// The one component whose props type cannot be resolved is named at startup,
// because it is the one the runtime pass will have nothing to check. It is a
// gap in coverage rather than a fault, so it is logged and the boot proceeds.
func TestBootNamesTheComponentItCannotCheck(t *testing.T) {
	p := newProject(t)

	out := observe(t, func() { p.bootAt(t, shared_types.CHECK_RUNTIME_AND_BOOT, logging.LEVEL_INFO) })

	if !strings.Contains(out, "legacy/Banner") || !strings.Contains(out, "untyped") {
		t.Fatalf("an untyped component should be named at boot, got:\n%s", out)
	}
	for _, typed := range []string{"pages/Dashboard", "pages/Profile", "pages/Article"} {
		if strings.Contains(out, typed) {
			t.Errorf("%q is typed and should not be named:\n%s", typed, out)
		}
	}
}

// An untyped component states no contract, so it cannot have broken one and
// must never stop a boot. Adopting go_solid in a codebase that is not fully
// typed depends on this.
func TestUntypedComponentDoesNotStopABoot(t *testing.T) {
	p := newProject(t)

	if _, err := os.Stat(p.componentFile("legacy/Banner.jsx")); err != nil {
		t.Fatalf("the untyped fixture is missing: %v", err)
	}
	p.boot(t, shared_types.CHECK_RUNTIME_AND_BOOT) // fatals on error
}

func TestBootIsSilentUnderRuntimeOnly(t *testing.T) {
	p := newProject(t)

	out := observe(t, func() { p.bootAt(t, shared_types.CHECK_RUNTIME, logging.LEVEL_INFO) })

	if strings.Contains(out, TYPE_FAULT_MARKER) {
		t.Fatalf("the boot pass under CHECK_RUNTIME should say nothing, got:\n%s", out)
	}
}

// The published surface exists from startup and holds only what was put there
// deliberately — nothing derived from a component.
func TestPublishedSurfaceHoldsOnlySynthesisedDefinitions(t *testing.T) {
	p := newProject(t)
	p.boot(t, shared_types.CHECK_RUNTIME_AND_BOOT)

	entries, err := os.ReadDir(filepath.Join(p.workspace, shared_types.TYPES_DIR_NAME))
	if err != nil {
		t.Fatalf("published surface missing: %v", err)
	}
	// Definitions go_solid synthesises are welcome here. What must never appear
	// is anything derived from a component, since a component already states
	// its own props type.
	//
	// Generated modules are not here: each is a self-contained TypeScript
	// library under .go_solid/modules, typed by its own source rather than by
	// an ambient declaration that has to be pulled into the program first.
	synthesised := map[string]bool{
		"navigation.d.ts": true,
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
		if !synthesised[e.Name()] {
			t.Errorf("%q is in the published surface and is not a synthesised definition", e.Name())
		}
	}
	if !slices.Contains(names, "navigation.d.ts") {
		t.Fatalf("published surface = %v, want the synthesised navigation.d.ts among them", names)
	}
}

// ------------------------------------------------------- props that fit

func TestPropsMatchingTheComponentAreAccepted(t *testing.T) {
	p := newProject(t)
	b := p.boot(t, shared_types.CHECK_RUNTIME)

	assertAccepted(t, "matching props", typeFault(t, b, "pages/Dashboard", dashboardProps{Title: "Inbox", Unread: 3, Note: "hello"}))
}

// The optional prop may simply be left out.
func TestOmittedOptionalPropIsAccepted(t *testing.T) {
	p := newProject(t)
	b := p.boot(t, shared_types.CHECK_RUNTIME)

	assertAccepted(t, "an omitted optional prop", typeFault(t, b, "pages/Dashboard", dashboardProps{Title: "Inbox", Unread: 0}))
}

// Covariance: a handler may pass more than the component reads.
func TestPropsCarryingExtraFieldsAreAccepted(t *testing.T) {
	p := newProject(t)
	b := p.boot(t, shared_types.CHECK_RUNTIME)

	type withExtras struct {
		Title    string `json:"title"`
		Unread   int    `json:"unread"`
		TraceID  string `json:"traceId"`
		Features []string
	}
	assertAccepted(t, "props carrying more than the component reads",
		typeFault(t, b, "pages/Dashboard", withExtras{Title: "Inbox", Unread: 1, TraceID: "abc"}))
}

// A props type declared as a local interface resolves the same as an inline one.
func TestPropsAgainstALocalInterfaceAreAccepted(t *testing.T) {
	p := newProject(t)
	b := p.boot(t, shared_types.CHECK_RUNTIME)

	assertAccepted(t, "props against a local interface", typeFault(t, b, "pages/Profile", profileProps{UserID: "u-1", DisplayName: "Lily"}))
}

// A component composing a synthesised definition is checked across both halves.
func TestPropsSatisfyingAComposedTypeAreAccepted(t *testing.T) {
	p := newProject(t)
	b := p.boot(t, shared_types.CHECK_RUNTIME)

	assertAccepted(t, "props satisfying a composed type", typeFault(t, b, "pages/Article", articleProps{Slug: "hello-world", CurrentPath: "/blog/hello-world"}))
}

func TestComponentWithoutPropsIsAcceptedWithNilProps(t *testing.T) {
	p := newProject(t)
	b := p.boot(t, shared_types.CHECK_RUNTIME)

	assertAccepted(t, "a component rendered without props", typeFault(t, b, "widgets/Clock", nil))
}

// ------------------------------------------------------ props that do not

func TestMissingRequiredPropIsReported(t *testing.T) {
	p := newProject(t)
	b := p.boot(t, shared_types.CHECK_RUNTIME)

	type incomplete struct {
		Title string `json:"title"`
	}
	assertRejected(t, "a missing required prop",
		typeFault(t, b, "pages/Dashboard", incomplete{Title: "Inbox"}), "pages/Dashboard", "unread")
}

func TestIncompatiblePropTypeIsReported(t *testing.T) {
	p := newProject(t)
	b := p.boot(t, shared_types.CHECK_RUNTIME)

	type wrongType struct {
		Title  string `json:"title"`
		Unread string `json:"unread"` // the component wants a number
	}
	assertRejected(t, "an incompatible prop type",
		typeFault(t, b, "pages/Dashboard", wrongType{Title: "Inbox", Unread: "three"}), "unread", "number")
}

// A plausible mistake: omitempty on a prop the component requires. The key
// vanishes when the value is empty, so the component can be handed nothing.
func TestOmitemptyOnARequiredPropIsReported(t *testing.T) {
	p := newProject(t)
	b := p.boot(t, shared_types.CHECK_RUNTIME)

	type sloppy struct {
		Title  string `json:"title,omitempty"`
		Unread int    `json:"unread"`
	}
	assertRejected(t, "omitempty on a required prop",
		typeFault(t, b, "pages/Dashboard", sloppy{Title: "Inbox", Unread: 2}), "title", "may be absent")
}

// The requirement comes from the imported definition, not from the component's
// own literal, so this only fails if the import was followed.
func TestMissingPropFromAComposedTypeIsReported(t *testing.T) {
	p := newProject(t)
	b := p.boot(t, shared_types.CHECK_RUNTIME)

	type slugOnly struct {
		Slug string `json:"slug"`
	}
	assertRejected(t, "a prop required by a composed type",
		typeFault(t, b, "pages/Article", slugOnly{Slug: "hello-world"}), "pages/Article", "currentPath")
}

// A component that takes no parameter states no requirement. Handing it props
// is the limiting case of handing a component more than it reads, which is
// always allowed.
func TestPropsForAComponentThatTakesNoneAreAccepted(t *testing.T) {
	p := newProject(t)
	b := p.boot(t, shared_types.CHECK_RUNTIME)

	assertAccepted(t, "props for a component that reads none",
		typeFault(t, b, "widgets/Clock", dashboardProps{Title: "Inbox", Unread: 1}))
}

func TestPropsThatAreNotAnObjectAreReported(t *testing.T) {
	p := newProject(t)
	b := p.boot(t, shared_types.CHECK_RUNTIME)

	assertRejected(t, "props that are not an object",
		typeFault(t, b, "pages/Dashboard", "just a string"), "pages/Dashboard", "underivable")
}

// An untyped component cannot be checked, and saying so on every render would
// be noise; it is named once at boot instead.
func TestUntypedComponentIsNotReportedPerRender(t *testing.T) {
	p := newProject(t)
	b := p.boot(t, shared_types.CHECK_RUNTIME)

	assertAccepted(t, "an untyped component at render time", typeFault(t, b, "legacy/Banner", struct {
		Message string `json:"message"`
	}{Message: "hi"}))
}

// CHECK_NEVER still caches, it just stops talking.
func TestNeverReportsNothingButStillCaches(t *testing.T) {
	p := newProject(t)
	b := p.boot(t, shared_types.CHECK_NEVER)

	type incomplete struct {
		Title string `json:"title"`
	}
	assertAccepted(t, "anything under CHECK_NEVER",
		typeFault(t, b, "pages/Dashboard", incomplete{Title: "Inbox"}))
	if _, err := os.Stat(p.cacheEntry("pages/Dashboard")); err != nil {
		t.Errorf("CHECK_NEVER should still warm the cache: %v", err)
	}
}

// A broken contract is a development failure, not a server fault the consumer
// has to infer from an error string: it dispatches an event they can hang their
// own handling off.
func TestBrokenContractDispatchesADevelopmentFailureEvent(t *testing.T) {
	p := newProject(t)
	b := p.boot(t, shared_types.CHECK_RUNTIME)

	type incomplete struct {
		Title string `json:"title"`
	}

	var caught events.FailureEvent
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/dashboard", nil)

	_, err := b.Prepare("pages/Dashboard", incomplete{Title: "Inbox"}).
		ForRequest(rec, req).
		SetHTTPBehaviour(func(behaviour *networking.RequestBehaviourBuilder) {
			behaviour.Upon(
				func(http.ResponseWriter, *http.Request, events.CompPropsInsufficientFailureEvent) error {
					caught = events.CompPropsInsufficientFailureEvent{}
					return nil
				},
			)
		}).
		Render()

	if err == nil {
		t.Fatal("a broken contract must fail the render")
	}
	if caught == nil {
		t.Fatal("a broken contract should have dispatched CompPropsInsufficientFailureEvent")
	}
	// The event is a development failure, which is the category a consumer
	// would treat differently from a genuine server fault.
	if _, ok := any(caught).(events.DevelopmentFailureEvent); !ok {
		t.Errorf("%T should be a DevelopmentFailureEvent", caught)
	}
}
