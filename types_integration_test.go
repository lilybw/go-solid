package go_solid

// Props checking as a consumer meets it: through Prepare, and through the boot
// pass New runs over the registry.
//
// The component file is the contract; <Workspace>/type_cache holds the shapes
// extracted from it, and <Workspace>/types is the importable surface reserved
// for definitions go_solid synthesises.

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	types_int "github.com/lilybw/go-solid/internal/types"
	logging "github.com/lilybw/go-solid/shared/logging"
	shared_raster "github.com/lilybw/go-solid/shared/rasterization"
	shared_types "github.com/lilybw/go-solid/shared/types"
)

type loginFormProps struct {
	Title    string   `json:"title"`
	Attempts int      `json:"attempts"`
	Hint     *string  `json:"hint,omitempty"`
	Tags     []string `json:"tags"`
}

func cacheEntryPath(workspace, component string) string {
	return filepath.Join(workspace, types_int.CACHE_DIR_NAME,
		filepath.FromSlash(component)+types_int.CACHE_ENTRY_EXT)
}

// captureLog redirects the process logger for the duration of the test.
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	flags := log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(os.Stderr)
		log.SetFlags(flags)
	})
	return &buf
}

// The cache is warm before anything is rendered, with no rasterization and no
// bundling in play.
func TestTypes_BootCachesEveryComponent(t *testing.T) {
	resetPackageState(t)
	comps := componentsDirWith(t, map[string]string{
		"auth/LoginForm.tsx": `export default function LoginForm(props: { title: string }) { return <div/>; }`,
		"Plain.tsx":          `export default function Plain(props: { id: number }) { return <div/>; }`,
	})

	b, err := New(&Config{
		LogLevel: logging.LEVEL_ERROR, Components: comps, Generation: disabledGeneration()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer b.Close()

	if b.cfg.Rasterization.Active() {
		t.Fatal("rasterization should be off with bundling disabled")
	}
	for _, name := range []string{"auth/LoginForm", "Plain"} {
		if _, err := os.Stat(cacheEntryPath(b.cfg.Workspace, name)); err != nil {
			t.Errorf("no cache entry for %q at boot: %v", name, err)
		}
	}
}

// The importable surface exists from startup, and is not where the cache lives.
func TestTypes_PublishedSurfaceIsCreatedAndSeparate(t *testing.T) {
	resetPackageState(t)
	comps := componentsDirWith(t, map[string]string{
		"Hello.tsx": `export default function Hello(props: { title: string }) { return <div/>; }`,
	})

	b, err := New(&Config{
		LogLevel: logging.LEVEL_ERROR, Components: comps, Generation: disabledGeneration()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer b.Close()

	published := filepath.Join(b.cfg.Workspace, shared_types.TYPES_DIR_NAME)
	info, err := os.Stat(published)
	if err != nil || !info.IsDir() {
		t.Fatalf("published surface not created: %v", err)
	}
	entries, err := os.ReadDir(published)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("nothing derived from a component belongs in the published surface, found %d entries", len(entries))
	}
}

func TestTypes_PrepareReportsPropsThatMissARequiredField(t *testing.T) {
	resetPackageState(t)
	comps := componentsDirWith(t, map[string]string{
		"Hello.tsx": `export default function Hello(props: { title: string; missing: number }) { return <div/>; }`,
	})

	logged := captureLog(t)
	b, err := New(&Config{
		LogLevel: logging.LEVEL_ERROR, Components: comps, Generation: disabledGeneration()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer b.Close()

	b.Prepare("Hello", loginFormProps{Title: "Sign in"})

	out := logged.String()
	if !strings.Contains(out, "[go_solid/types]") || !strings.Contains(out, "missing") {
		t.Fatalf("expected a props diagnostic naming the absent field, got:\n%s", out)
	}
}

// Covariance end to end: the Go side may pass more than the component reads.
func TestTypes_PrepareIsQuietWhenPropsSatisfyTheComponent(t *testing.T) {
	resetPackageState(t)
	comps := componentsDirWith(t, map[string]string{
		"Hello.tsx": `export default function Hello(props: { title: string }) { return <div/>; }`,
	})

	logged := captureLog(t)
	b, err := New(&Config{
		LogLevel: logging.LEVEL_ERROR, Components: comps, Generation: disabledGeneration()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer b.Close()

	b.Prepare("Hello", loginFormProps{Title: "Sign in", Attempts: 2})

	if out := logged.String(); strings.Contains(out, "[go_solid/types]") {
		t.Fatalf("props carrying more than the component reads are fine, got:\n%s", out)
	}
}

// A component composing a synthesised definition into its props is checked as a
// whole, which is what the published surface is for.
func TestTypes_ComponentComposingAPublishedTypeIsChecked(t *testing.T) {
	resetPackageState(t)
	comps := componentsDirWith(t, map[string]string{
		"pages/Home.tsx": `
import type { Navigation } from "../types/navigation";
export default function Home(props: { title: string } & Navigation) { return <div/>; }
`,
		"types/navigation.d.ts": "export interface Navigation { currentPath: string }\n",
	})

	logged := captureLog(t)
	b, err := New(&Config{
		LogLevel: logging.LEVEL_ERROR, Components: comps, Generation: disabledGeneration()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer b.Close()

	// currentPath comes from the imported definition, and is not supplied.
	b.Prepare("pages/Home", loginFormProps{Title: "Home"})

	out := logged.String()
	if !strings.Contains(out, "currentPath") {
		t.Fatalf("a requirement from an imported definition should be enforced, got:\n%s", out)
	}
}

func TestTypes_NilPropsAreNotChecked(t *testing.T) {
	resetPackageState(t)
	comps := componentsDirWith(t, map[string]string{
		"Hello.tsx": `export default function Hello(props: { title: string }) { return <div/>; }`,
	})

	logged := captureLog(t)
	b, err := New(&Config{
		LogLevel: logging.LEVEL_ERROR, Components: comps, Generation: disabledGeneration()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer b.Close()

	b.Prepare("Hello", nil)

	if out := logged.String(); strings.Contains(out, "[go_solid/types]") {
		t.Fatalf("nil props are not a finding, got:\n%s", out)
	}
}

func TestTypes_PrepareForAnUnknownComponentIsHarmless(t *testing.T) {
	resetPackageState(t)
	comps := componentsDirWith(t, map[string]string{"Hello.tsx": "export default () => null;"})

	b, err := New(&Config{
		LogLevel: logging.LEVEL_ERROR, Components: comps, Generation: disabledGeneration()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer b.Close()

	// Render will report the missing component; Prepare must not panic or
	// cache anything on the way there.
	b.Prepare("does/NotExist", loginFormProps{Title: "x"})

	if _, err := os.Stat(cacheEntryPath(b.cfg.Workspace, "does/NotExist")); err == nil {
		t.Fatal("an unregistered component must not get a cache entry")
	}
}

// The cache and the published surface both live under the workspace, whose
// .d.ts files would otherwise look like components when it sits inside the
// components tree.
func TestTypes_WorkspaceArtifactsAreNotRegisteredAsComponents(t *testing.T) {
	resetPackageState(t)
	comps := componentsDirWith(t, map[string]string{
		"Hello.tsx":       `export default function Hello(props: { title: string }) { return <div/>; }`,
		"types/Hand.d.ts": "export interface HandProps { a: string }\n",
	})

	b, err := New(&Config{
		LogLevel: logging.LEVEL_ERROR,
		// Workspace inside Components, the worst case for the registry walk.
		Components: comps, Workspace: comps, Generation: disabledGeneration()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer b.Close()

	if err := b.Registry().Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	for _, name := range b.Registry().Names() {
		if strings.HasPrefix(name, "types/") || strings.HasPrefix(name, types_int.CACHE_DIR_NAME+"/") {
			t.Errorf("workspace artifact %q must not be registered as a component", name)
		}
	}
}

func TestTypes_BootNamesUncheckableComponents(t *testing.T) {
	resetPackageState(t)
	comps := componentsDirWith(t, map[string]string{
		"Untyped.tsx": `export default function Untyped(props) { return <div/>; }`,
	})

	logged := captureLog(t)
	b, err := New(&Config{
		LogLevel: logging.LEVEL_ERROR, Components: comps, Generation: disabledGeneration()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer b.Close()

	out := logged.String()
	if !strings.Contains(out, "untyped") || !strings.Contains(out, "Untyped") {
		t.Fatalf("the boot pass should name a component it cannot check, got:\n%s", out)
	}
}

func TestTypes_BootRunsWhenRasterizationIsComplete(t *testing.T) {
	resetPackageState(t)
	comps := componentsDirWith(t, map[string]string{
		"Hello.tsx": `export default function Hello(props: { title: string }) { return <div/>; }`,
	})
	workspace := t.TempDir()
	stageRasterizedWorkspace(t, workspace)

	b, err := New(&Config{
		LogLevel:      logging.LEVEL_ERROR,
		Components:    comps,
		Workspace:     workspace,
		Rasterization: &shared_raster.RasterizationConfig{Location: workspace, ExpectCompleted: true},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer b.Close()

	if _, err := os.Stat(cacheEntryPath(workspace, "Hello")); err != nil {
		t.Errorf("boot pass should have run alongside a completed rasterization: %v", err)
	}
}
