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
	"os"
	"path/filepath"
	"strings"
	"testing"

	go_solid "github.com/lilybw/go-solid"
	types_int "github.com/lilybw/go-solid/internal/types"
	shared_esbuild "github.com/lilybw/go-solid/shared/esbuild"
	"github.com/lilybw/go-solid/shared/logging"
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

	bundler, err := go_solid.New(&go_solid.Config{
		Components: p.components,
		LogLevel:   logging.LEVEL_ERROR,
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

// observe captures what go_solid logs while fn runs. Findings are reported
// through the standard logger, so that is what a consumer sees and what this
// scenario watches.
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

func assertQuiet(t *testing.T, what, out string) {
	t.Helper()
	if strings.Contains(out, "[go_solid/types]") {
		t.Fatalf("%s should not have been reported, got:\n%s", what, out)
	}
}

func assertReports(t *testing.T, what, out string, fragments ...string) {
	t.Helper()
	if !strings.Contains(out, "[go_solid/types]") {
		t.Fatalf("%s should have been reported, got nothing", what)
	}
	for _, fragment := range fragments {
		if !strings.Contains(out, fragment) {
			t.Errorf("%s: report should mention %q, got:\n%s", what, fragment, out)
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
// because it is the one the runtime pass will have nothing to check.
func TestBootNamesTheComponentItCannotCheck(t *testing.T) {
	p := newProject(t)

	out := observe(t, func() { p.boot(t, shared_types.CHECK_RUNTIME_AND_BOOT) })

	assertReports(t, "an untyped component", out, "legacy/Banner", "untyped")
	for _, typed := range []string{"pages/Dashboard", "pages/Profile", "pages/Article"} {
		if strings.Contains(out, typed) {
			t.Errorf("%q is typed and should not be named:\n%s", typed, out)
		}
	}
}

func TestBootIsSilentUnderRuntimeOnly(t *testing.T) {
	p := newProject(t)

	out := observe(t, func() { p.boot(t, shared_types.CHECK_RUNTIME) })

	assertQuiet(t, "the boot pass under CHECK_RUNTIME", out)
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
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if len(names) != 1 || names[0] != "navigation.d.ts" {
		t.Fatalf("published surface = %v, want only the synthesised navigation.d.ts", names)
	}
}

// ------------------------------------------------------- props that fit

func TestPropsMatchingTheComponentAreAccepted(t *testing.T) {
	p := newProject(t)
	b := p.boot(t, shared_types.CHECK_RUNTIME)

	out := observe(t, func() {
		b.Prepare("pages/Dashboard", dashboardProps{Title: "Inbox", Unread: 3, Note: "hello"})
	})

	assertQuiet(t, "matching props", out)
}

// The optional prop may simply be left out.
func TestOmittedOptionalPropIsAccepted(t *testing.T) {
	p := newProject(t)
	b := p.boot(t, shared_types.CHECK_RUNTIME)

	out := observe(t, func() {
		b.Prepare("pages/Dashboard", dashboardProps{Title: "Inbox", Unread: 0})
	})

	assertQuiet(t, "an omitted optional prop", out)
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
	out := observe(t, func() {
		b.Prepare("pages/Dashboard", withExtras{Title: "Inbox", Unread: 1, TraceID: "abc"})
	})

	assertQuiet(t, "props carrying more than the component reads", out)
}

// A props type declared as a local interface resolves the same as an inline one.
func TestPropsAgainstALocalInterfaceAreAccepted(t *testing.T) {
	p := newProject(t)
	b := p.boot(t, shared_types.CHECK_RUNTIME)

	out := observe(t, func() {
		b.Prepare("pages/Profile", profileProps{UserID: "u-1", DisplayName: "Lily"})
	})

	assertQuiet(t, "props against a local interface", out)
}

// A component composing a synthesised definition is checked across both halves.
func TestPropsSatisfyingAComposedTypeAreAccepted(t *testing.T) {
	p := newProject(t)
	b := p.boot(t, shared_types.CHECK_RUNTIME)

	out := observe(t, func() {
		b.Prepare("pages/Article", articleProps{Slug: "hello-world", CurrentPath: "/blog/hello-world"})
	})

	assertQuiet(t, "props satisfying a composed type", out)
}

func TestComponentWithoutPropsIsAcceptedWithNilProps(t *testing.T) {
	p := newProject(t)
	b := p.boot(t, shared_types.CHECK_RUNTIME)

	out := observe(t, func() { b.Prepare("widgets/Clock", nil) })

	assertQuiet(t, "a component rendered without props", out)
}

// ------------------------------------------------------ props that do not

func TestMissingRequiredPropIsReported(t *testing.T) {
	p := newProject(t)
	b := p.boot(t, shared_types.CHECK_RUNTIME)

	type incomplete struct {
		Title string `json:"title"`
	}
	out := observe(t, func() { b.Prepare("pages/Dashboard", incomplete{Title: "Inbox"}) })

	assertReports(t, "a missing required prop", out, "pages/Dashboard", "unread")
}

func TestIncompatiblePropTypeIsReported(t *testing.T) {
	p := newProject(t)
	b := p.boot(t, shared_types.CHECK_RUNTIME)

	type wrongType struct {
		Title  string `json:"title"`
		Unread string `json:"unread"` // the component wants a number
	}
	out := observe(t, func() { b.Prepare("pages/Dashboard", wrongType{Title: "Inbox", Unread: "three"}) })

	assertReports(t, "an incompatible prop type", out, "unread", "number")
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
	out := observe(t, func() { b.Prepare("pages/Dashboard", sloppy{Title: "Inbox", Unread: 2}) })

	assertReports(t, "omitempty on a required prop", out, "title", "may be absent")
}

// The requirement comes from the imported definition, not from the component's
// own literal, so this only fails if the import was followed.
func TestMissingPropFromAComposedTypeIsReported(t *testing.T) {
	p := newProject(t)
	b := p.boot(t, shared_types.CHECK_RUNTIME)

	type slugOnly struct {
		Slug string `json:"slug"`
	}
	out := observe(t, func() { b.Prepare("pages/Article", slugOnly{Slug: "hello-world"}) })

	assertReports(t, "a prop required by a composed type", out, "pages/Article", "currentPath")
}

func TestPropsForAComponentThatTakesNoneAreReported(t *testing.T) {
	p := newProject(t)
	b := p.boot(t, shared_types.CHECK_RUNTIME)

	out := observe(t, func() {
		b.Prepare("widgets/Clock", dashboardProps{Title: "Inbox", Unread: 1})
	})

	assertReports(t, "props for a component that takes none", out, "widgets/Clock", "no-props")
}

func TestPropsThatAreNotAnObjectAreReported(t *testing.T) {
	p := newProject(t)
	b := p.boot(t, shared_types.CHECK_RUNTIME)

	out := observe(t, func() { b.Prepare("pages/Dashboard", "just a string") })

	assertReports(t, "props that are not an object", out, "pages/Dashboard", "underivable")
}

// An untyped component cannot be checked, and saying so on every render would
// be noise; it is named once at boot instead.
func TestUntypedComponentIsNotReportedPerRender(t *testing.T) {
	p := newProject(t)
	b := p.boot(t, shared_types.CHECK_RUNTIME)

	out := observe(t, func() {
		b.Prepare("legacy/Banner", struct {
			Message string `json:"message"`
		}{Message: "hi"})
	})

	assertQuiet(t, "an untyped component at render time", out)
}

// CHECK_NEVER still caches, it just stops talking.
func TestNeverReportsNothingButStillCaches(t *testing.T) {
	p := newProject(t)
	b := p.boot(t, shared_types.CHECK_NEVER)

	type incomplete struct {
		Title string `json:"title"`
	}
	out := observe(t, func() { b.Prepare("pages/Dashboard", incomplete{Title: "Inbox"}) })

	assertQuiet(t, "anything under CHECK_NEVER", out)
	if _, err := os.Stat(p.cacheEntry("pages/Dashboard")); err != nil {
		t.Errorf("CHECK_NEVER should still warm the cache: %v", err)
	}
}
