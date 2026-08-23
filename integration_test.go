package go_solid

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	caching "github.com/lilybw/go-solid/internal/caching"
	"github.com/lilybw/go-solid/internal/meta"
	"github.com/lilybw/go-solid/shared/esbuild"
	"github.com/lilybw/go-solid/shared/logging"
)

// -----------------------------------------------------------------------------
// Integration test harness
// -----------------------------------------------------------------------------

// integrationEnv locates a node_modules that resolves solid-js and returns the
// directory holding it, or skips.
func integrationEnv(t *testing.T) (modulesParent meta.AbsoluteDirectoryPath) {
	t.Helper()

	skip := func(format string, args ...any) {
		t.Skipf(format, args...)
	}

	// Locate this test file's directory to anchor the search.
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		skip("cannot determine test file location")
	}
	pkgDir := filepath.Dir(thisFile)

	// Find a node_modules with the required packages. Check pkgDir and example/.
	candidates := []string{
		pkgDir,
		filepath.Join(pkgDir, "example"),
	}
	for _, dir := range candidates {
		nm := filepath.Join(dir, "node_modules")
		if hasSolidRuntime(nm) {
			return dir
		}
	}
	skip("node_modules with solid-js not found; run `npm install solid-js`")
	return ""
}

// hasSolidRuntime reports whether nodeModules resolves the browser runtime the
// generated entry imports. That is the only npm package go_solid needs.
func hasSolidRuntime(nodeModules string) bool {
	_, err := os.Stat(filepath.Join(nodeModules, "solid-js"))
	return err == nil
}

// newTestBundler builds a Bundler over a temp components dir placed where the
// discovered node_modules is resolvable, so esbuild can find solid-js.
func newTestBundler(t *testing.T, components map[string]string, cfg *Config) *Bundler {
	t.Helper()
	modulesParent := integrationEnv(t)

	workDir := t.TempDir()
	// Make node_modules resolvable from workDir. Symlinks require elevated
	// privileges on Windows (Developer Mode / admin), so we can't rely on them.
	// Instead, point the bundler's module resolution at the real tree by using
	// a junction-free approach: copy is too slow for a big node_modules, so we
	// set WorkDir to a dir that already HAS node_modules — the modulesParent —
	// and put components under a subdir there via TempDir-on-same-volume.
	//
	// Simplest portable choice: run the bundler with WorkDir = modulesParent
	// (which already resolves the packages) and a components dir created inside
	// a fresh temp subfolder of modulesParent so cleanup is contained.
	_ = workDir // no longer used; kept for clarity of intent

	compBase, err := os.MkdirTemp(modulesParent, "solidbundle-test-*")
	if err != nil {
		t.Fatalf("mkdtemp under modulesParent: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(compBase) })

	compDir := filepath.Join(compBase, "components")
	for rel, contents := range components {
		full := filepath.Join(compDir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(full, []byte(contents), 0o644); err != nil {
			t.Fatalf("write %s: %v", full, err)
		}
	}

	if cfg.Generation == nil {
		// Copy, never alias: writing Dependencies through the singleton would
		// change the default for every other test in the binary.
		cfg.Generation = meta.Copy(esbuild.NIL_BUNDLER_CONFIG)
	}
	cfg.LogLevel = logging.LEVEL_ERROR
	cfg.Components = compDir
	cfg.Generation.Dependencies = modulesParent // already contains node_modules with solid-js

	b, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(b.Close)
	return b
}

const simpleComponent = `import { createSignal } from "solid-js";
export default function Hello(props: { name?: string }) {
  const [n, setN] = createSignal(0);
  return <div class="hello"><h1>Hi {props.name ?? "world"}</h1><button onClick={() => setN(n() + 1)}>{n()}</button></div>;
}
`

func TestRender_ProducesSolidBundleAndHTML(t *testing.T) {
	b := newTestBundler(t, map[string]string{
		"Hello.tsx": simpleComponent,
	}, &Config{
		Generation: &esbuild.BundlerConfig{Minify: false},
	})

	r, err := b.Prepare("Hello", map[string]any{"name": "HOTS"}).WithCtx(context.Background()).Render()
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	// Genuine Solid compiler output, not React.
	if !strings.Contains(r.JS, "template(") {
		t.Error("bundle JS missing Solid template() call")
	}
	if strings.Contains(r.JS, "React.createElement") {
		t.Error("bundle JS contains React.createElement")
	}
	// Props embedded in the HTML data island.
	if !strings.Contains(r.HTML, `"name":"HOTS"`) {
		t.Errorf("HTML missing props; got:\n%s", r.HTML)
	}
	// The shell is self-contained: AssembleHTML inlines the bundle rather than
	// linking it, so JSName is a serving name for the disk cache, not an href.
	if !strings.Contains(r.HTML, "<script type=\"module\">") || !strings.Contains(r.HTML, "template(") {
		t.Errorf("HTML does not inline the bundle; got:\n%s", r.HTML)
	}
	if !strings.HasPrefix(r.JSName, "Hello.") || !strings.HasSuffix(r.JSName, ".js") {
		t.Errorf("JSName %q is not the expected <component>.<hash>.js", r.JSName)
	}
}

func TestRender_CollectsImportedCSS(t *testing.T) {
	b := newTestBundler(t, map[string]string{
		"Styled.tsx": `import "./styled.css";
export default function Styled() { return <div class="styled">hi</div>; }
`,
		// Use a class selector + property as the assertion target. Avoid color
		// keywords: esbuild's minifier folds them to hex (rebeccapurple -> #639),
		// so assert on tokens that survive minification unchanged.
		"styled.css": `.styled { padding: 4px; letter-spacing: 2px; }`,
	}, &Config{Generation: &esbuild.BundlerConfig{Minify: true}})

	r, err := b.Prepare("Styled", nil).WithCtx(context.Background()).Render()
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if r.CSS == "" {
		t.Fatal("expected CSS to be collected, got empty")
	}
	for _, want := range []string{".styled", "padding", "letter-spacing"} {
		if !strings.Contains(r.CSS, want) {
			t.Errorf("CSS missing %q; got: %q", want, r.CSS)
		}
	}
	// Same as the JS: collected CSS is inlined into a <style>, never linked.
	if !strings.Contains(r.HTML, "<style>") || !strings.Contains(r.HTML, ".styled") {
		t.Errorf("HTML does not inline the collected CSS; got:\n%s", r.HTML)
	}
	if !strings.HasPrefix(r.CSSName, "Styled.") || !strings.HasSuffix(r.CSSName, ".css") {
		t.Errorf("CSSName %q is not the expected <component>.<hash>.css", r.CSSName)
	}
}

// Render never hands back the cached pointer: assembleResponse copies the
// artifact so per-request HTML cannot mutate it. The cache hit is observable
// through the artifact half instead.
func TestRender_CacheHitReusesArtifact(t *testing.T) {
	b := newTestBundler(t, map[string]string{
		"Hello.tsx": simpleComponent,
	}, &Config{Generation: &esbuild.BundlerConfig{Minify: true}})

	ctx := context.Background()
	first, err := b.Prepare("Hello", map[string]any{"name": "A"}).WithCtx(ctx).Render()
	if err != nil {
		t.Fatalf("first Render: %v", err)
	}
	second, err := b.Prepare("Hello", map[string]any{"name": "A"}).WithCtx(ctx).Render()
	if err != nil {
		t.Fatalf("second Render: %v", err)
	}
	if first == second {
		t.Fatal("Render must not expose the cached *Rendered pointer")
	}
	if first.JSName != second.JSName || first.JS != second.JS {
		t.Errorf("cache hit rebuilt the bundle: JSName %q vs %q", first.JSName, second.JSName)
	}

	// The cache key is component+root; props live in the HTML, not the bundle.
	// So a different props set reuses the same JS and produces different HTML.
	third, err := b.Prepare("Hello", map[string]any{"name": "B"}).WithCtx(ctx).Render()
	if err != nil {
		t.Fatalf("third Render: %v", err)
	}
	if third.JSName != first.JSName {
		t.Errorf("props must not affect the cached bundle: %q vs %q", third.JSName, first.JSName)
	}
	if third.HTML == first.HTML {
		t.Error("different props produced identical HTML")
	}
}

// The end-to-end form of the shell/cache agreement: a warm render must produce
// the same document a cold one did. Anything the cache short-circuits past that
// the shell also needs — the mount root above all — shows up here as a diff
// between the first render and the second.
func TestRender_WarmRenderMatchesColdRender(t *testing.T) {
	b := newTestBundler(t, map[string]string{
		"Hello.tsx": simpleComponent,
	}, &Config{Generation: &esbuild.BundlerConfig{Minify: true}})

	ctx := context.Background()
	props := map[string]any{"name": "same"}

	cold, err := b.Prepare("Hello", props).WithCtx(ctx).Render()
	if err != nil {
		t.Fatalf("cold Render: %v", err)
	}
	warm, err := b.Prepare("Hello", props).WithCtx(ctx).Render()
	if err != nil {
		t.Fatalf("warm Render: %v", err)
	}

	if cold.HTML != warm.HTML {
		t.Errorf("warm render differs from cold render\ncold:\n%s\nwarm:\n%s",
			head(cold.HTML, 600), head(warm.HTML, 600))
	}

	comp, ok := b.Registry().Lookup("Hello")
	if !ok {
		t.Fatal("Hello not registered")
	}
	mount := `<div id="` + comp.MountRootID + `">`
	for label, html := range map[string]string{"cold": cold.HTML, "warm": warm.HTML} {
		if !strings.Contains(html, mount) {
			t.Errorf("%s render does not mount on %q; the bundle will find no root\n%s",
				label, comp.MountRootID, head(html, 600))
		}
	}
}

// The disk layer has to agree with the memory layer about what an entry is: a
// second Bundler over the same workspace reads only the disk, so a shell it
// assembles from a disk hit must still name the right root.
func TestRender_DiskHitInAFreshBundlerKeepsTheMountRoot(t *testing.T) {
	components := map[string]string{"Hello.tsx": simpleComponent}
	first := newTestBundler(t, components, &Config{Generation: &esbuild.BundlerConfig{Minify: true}})

	ctx := context.Background()
	cold, err := first.Prepare("Hello", nil).WithCtx(ctx).Render()
	if err != nil {
		t.Fatalf("cold Render: %v", err)
	}
	comp, _ := first.Registry().Lookup("Hello")
	first.Close()

	// Same components root, so the same workspace and the same disk cache.
	second, err := New(&Config{
		LogLevel:   logging.LEVEL_ERROR,
		Components: first.cfg.Components,
		Generation: &esbuild.BundlerConfig{
			Minify: true, Dependencies: first.cfg.Generation.Dependencies, Disabled: true,
		},
	})
	if err != nil {
		t.Fatalf("second New: %v", err)
	}
	t.Cleanup(second.Close)

	// Disabled generation means this can only be answered from disk.
	warm, err := second.Prepare("Hello", nil).WithCtx(ctx).Render()
	if err != nil {
		t.Fatalf("disk-only Render: %v", err)
	}
	if !strings.Contains(warm.HTML, `<div id="`+comp.MountRootID+`">`) {
		t.Errorf("disk hit lost the mount root:\n%s", head(warm.HTML, 600))
	}
	if warm.JS != cold.JS {
		t.Error("disk hit returned a different bundle than the one written")
	}
}

// Changing a setting that changes the emitted bytes must not be answered from
// an entry written before the change.
func TestRender_ChangedBuildSettingsAreNotServedFromCache(t *testing.T) {
	components := map[string]string{"Hello.tsx": simpleComponent}
	minified := newTestBundler(t, components, &Config{Generation: &esbuild.BundlerConfig{Minify: true}})

	ctx := context.Background()
	small, err := minified.Prepare("Hello", nil).WithCtx(ctx).Render()
	if err != nil {
		t.Fatalf("minified Render: %v", err)
	}
	componentsRoot, deps := minified.cfg.Components, minified.cfg.Generation.Dependencies
	minified.Close()

	readable, err := New(&Config{
		LogLevel:   logging.LEVEL_ERROR,
		Components: componentsRoot,
		Generation: &esbuild.BundlerConfig{Minify: false, Dependencies: deps},
	})
	if err != nil {
		t.Fatalf("second New: %v", err)
	}
	t.Cleanup(readable.Close)

	large, err := readable.Prepare("Hello", nil).WithCtx(ctx).Render()
	if err != nil {
		t.Fatalf("unminified Render: %v", err)
	}
	if large.JS == small.JS {
		t.Error("flipping Minify was answered from the cache written under the old setting")
	}
}

func TestRender_UnknownComponentErrors(t *testing.T) {
	b := newTestBundler(t, map[string]string{
		"Hello.tsx": simpleComponent,
	}, &Config{DisableCaching: true})

	_, err := b.Prepare("does/not/exist", nil).WithCtx(context.Background()).Render()
	if err == nil {
		t.Fatal("expected error for unknown component, got nil")
	}
	if !strings.Contains(err.Error(), "does/not/exist") {
		t.Errorf("error should name the missing component; got: %v", err)
	}
}

func TestRender_DevModeBypassesCache(t *testing.T) {
	b := newTestBundler(t, map[string]string{
		"Hello.tsx": simpleComponent,
	}, &Config{DisableCaching: true})

	ctx := context.Background()
	if _, err := b.Prepare("Hello", nil).WithCtx(ctx).Render(); err != nil {
		t.Fatalf("first Render: %v", err)
	}
	if _, err := b.Prepare("Hello", nil).WithCtx(ctx).Render(); err != nil {
		t.Fatalf("second Render: %v", err)
	}

	// Pointer inequality proves nothing here — Render always copies. Assert the
	// cache itself never retained the artifact.
	comp, ok := b.Registry().Lookup("Hello")
	if !ok {
		t.Fatal("Hello not registered")
	}
	if _, hit := b.mem.Get(caching.NewMemCacheKey("Hello", comp.MountRootID)); hit {
		t.Error("DisableCaching should leave the memory cache empty")
	}
}

func TestNew_RequiresMandatoryConfig(t *testing.T) {
	// Missing everything: should error before touching the filesystem.
	if _, err := New(&Config{
		LogLevel: logging.LEVEL_ERROR,
	}); err == nil {
		t.Error("New with empty Config should error")
	}
}

func head(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// Guard: integration tests should complete reasonably quickly per render.
// This isn't a strict benchmark, just a canary that warm renders aren't
// pathologically slow (i.e. that the cache is actually being consulted).
func TestRender_WarmRenderIsFast(t *testing.T) {
	b := newTestBundler(t, map[string]string{
		"Hello.tsx": simpleComponent,
	}, &Config{Generation: &esbuild.BundlerConfig{Minify: true}})

	ctx := context.Background()

	if _, err := b.Prepare("Hello", map[string]any{"name": "warm"}).WithCtx(ctx).Render(); err != nil {
		t.Fatalf("warm-up Render: %v", err)
	}
	// Second identical render is a cache hit; should be near-instant.
	start := time.Now()
	if _, err := b.Prepare("Hello", map[string]any{"name": "warm"}).WithCtx(ctx).Render(); err != nil {
		t.Fatalf("cached Render: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 50*time.Millisecond {
		t.Errorf("cache hit took %v, expected <50ms (is caching working?)", elapsed)
	}
}
