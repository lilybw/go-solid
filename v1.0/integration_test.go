package go_solid

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// -----------------------------------------------------------------------------
// Integration test harness
// -----------------------------------------------------------------------------
// These tests exercise the real pipeline: a persistent Node worker running
// babel-preset-solid, plus esbuild-in-Go. They require:
//   - `node` on PATH
//   - a node_modules containing solid-js + babel-preset-solid + @babel/core
//   - the worker script internal/worker/transform-worker.mjs
//
// If any are missing the tests SKIP (not fail), so `go test ./...` stays green
// on a machine without the JS toolchain. Set GO_SOLID_REQUIRE_INTEGRATION=1
// to turn those skips into failures (useful in CI where the toolchain must exist).

func requireIntegration() bool {
	return os.Getenv("GO_SOLID_REQUIRE_INTEGRATION") == "1"
}

// integrationEnv locates the worker script and a node_modules that resolves the
// required packages. It returns (workerScript, nodeModulesParent) or skips.
func integrationEnv(t *testing.T) (workerScript, modulesParent string) {
	t.Helper()

	skip := func(format string, args ...any) {
		if requireIntegration() {
			t.Fatalf(format, args...)
		}
		t.Skipf(format, args...)
	}

	if _, err := exec.LookPath("node"); err != nil {
		skip("node not on PATH; skipping integration test")
	}

	// Locate this test file's directory to find the worker script relative to it.
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		skip("cannot determine test file location")
	}
	pkgDir := filepath.Dir(thisFile)

	workerScript = filepath.Join(pkgDir, "internal", "worker", "transform-worker.mjs")
	if _, err := os.Stat(workerScript); err != nil {
		skip("worker script not found at %s", workerScript)
	}

	// Find a node_modules with the required packages. Check pkgDir and example/.
	candidates := []string{
		pkgDir,
		filepath.Join(pkgDir, "example"),
	}
	for _, dir := range candidates {
		nm := filepath.Join(dir, "node_modules")
		if hasSolidToolchain(nm) {
			return workerScript, dir
		}
	}
	skip("node_modules with solid-js + babel-preset-solid not found; run `npm install solid-js babel-preset-solid @babel/core`")
	return "", ""
}

func hasSolidToolchain(nodeModules string) bool {
	for _, pkg := range []string{"solid-js", "babel-preset-solid", "@babel/core"} {
		if _, err := os.Stat(filepath.Join(nodeModules, pkg)); err != nil {
			return false
		}
	}
	return true
}

// newTestBundler builds a Bundler over a temp components dir, symlinking the
// discovered node_modules so esbuild and the worker can resolve packages.
func newTestBundler(t *testing.T, components map[string]string, cfg Config) *Bundler {
	t.Helper()
	workerScript, modulesParent := integrationEnv(t)

	workDir := t.TempDir()
	// Symlink node_modules into the work dir.
	if err := os.Symlink(filepath.Join(modulesParent, "node_modules"), filepath.Join(workDir, "node_modules")); err != nil {
		t.Fatalf("symlink node_modules: %v", err)
	}
	compDir := filepath.Join(workDir, "components")
	for rel, contents := range components {
		full := filepath.Join(compDir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(full, []byte(contents), 0o644); err != nil {
			t.Fatalf("write %s: %v", full, err)
		}
	}

	cfg.ComponentsDir = compDir
	cfg.WorkDir = workDir
	cfg.WorkerScript = workerScript
	if cfg.PoolSize == 0 {
		cfg.PoolSize = 1
	}

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

func TestPool_TransformProducesSolidOutput(t *testing.T) {
	workerScript, modulesParent := integrationEnv(t)
	pool, err := newPool(PoolConfig{
		Size:       1,
		ScriptPath: workerScript,
		WorkDir:    modulesParent,
	})
	if err != nil {
		t.Fatalf("newPool: %v", err)
	}
	defer pool.Close()

	out, err := pool.Transform(context.Background(), transformRequest{
		Filename: "Hello.tsx",
		Code:     simpleComponent,
		Generate: "dom",
	})
	if err != nil {
		t.Fatalf("Transform: %v", err)
	}

	// babel-preset-solid emits template() calls and Solid web imports.
	for _, want := range []string{"_$template", "solid-js/web"} {
		if !strings.Contains(out, want) {
			t.Errorf("transform output missing %q; got first 300 chars:\n%s", want, head(out, 300))
		}
	}
	// It must NOT be React-shaped.
	if strings.Contains(out, "React.createElement") {
		t.Error("transform output contains React.createElement — wrong JSX runtime")
	}
}

func TestPool_HandlesConcurrentTransforms(t *testing.T) {
	workerScript, modulesParent := integrationEnv(t)
	pool, err := newPool(PoolConfig{
		Size:       2,
		ScriptPath: workerScript,
		WorkDir:    modulesParent,
	})
	if err != nil {
		t.Fatalf("newPool: %v", err)
	}
	defer pool.Close()

	const n = 12
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		go func() {
			_, err := pool.Transform(context.Background(), transformRequest{
				Filename: "Hello.tsx",
				Code:     simpleComponent,
				Generate: "dom",
			})
			errs <- err
		}()
	}
	for i := 0; i < n; i++ {
		if err := <-errs; err != nil {
			t.Errorf("concurrent transform %d failed: %v", i, err)
		}
	}
}

func TestPool_TransformSurfacesBabelErrors(t *testing.T) {
	workerScript, modulesParent := integrationEnv(t)
	pool, err := newPool(PoolConfig{
		Size:       1,
		ScriptPath: workerScript,
		WorkDir:    modulesParent,
	})
	if err != nil {
		t.Fatalf("newPool: %v", err)
	}
	defer pool.Close()

	// Syntactically broken JSX should produce an error, and the worker should
	// stay alive to serve the next request.
	_, err = pool.Transform(context.Background(), transformRequest{
		Filename: "Bad.tsx",
		Code:     `export default () => <div class=>;`,
		Generate: "dom",
	})
	if err == nil {
		t.Error("expected error for malformed JSX, got nil")
	}

	// Worker still usable afterwards.
	if _, err := pool.Transform(context.Background(), transformRequest{
		Filename: "Hello.tsx", Code: simpleComponent, Generate: "dom",
	}); err != nil {
		t.Errorf("worker unusable after error: %v", err)
	}
}

func TestRender_ProducesSolidBundleAndHTML(t *testing.T) {
	b := newTestBundler(t, map[string]string{
		"Hello.tsx": simpleComponent,
	}, Config{Minify: false})

	r, err := b.Render(context.Background(), "Hello", map[string]any{"name": "HOTS"})
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
	// JS asset name is present and referenced by the HTML.
	if r.JSName == "" || !strings.Contains(r.HTML, r.JSName) {
		t.Errorf("JSName %q not referenced in HTML", r.JSName)
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
	}, Config{Minify: true})

	r, err := b.Render(context.Background(), "Styled", nil)
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
	if r.CSSName == "" || !strings.Contains(r.HTML, r.CSSName) {
		t.Errorf("CSSName %q not referenced in HTML", r.CSSName)
	}
}

func TestRender_CacheHitReturnsSamePointer(t *testing.T) {
	b := newTestBundler(t, map[string]string{
		"Hello.tsx": simpleComponent,
	}, Config{Minify: true}) // Minify implies !Dev caching path; New sets cache enabled when !Dev

	ctx := context.Background()
	first, err := b.Render(ctx, "Hello", map[string]any{"name": "A"})
	if err != nil {
		t.Fatalf("first Render: %v", err)
	}
	second, err := b.Render(ctx, "Hello", map[string]any{"name": "A"})
	if err != nil {
		t.Fatalf("second Render: %v", err)
	}
	if first != second {
		t.Error("expected cache hit to return identical *Rendered pointer")
	}

	// Different props must NOT hit the same cache entry.
	third, err := b.Render(ctx, "Hello", map[string]any{"name": "B"})
	if err != nil {
		t.Fatalf("third Render: %v", err)
	}
	if third == first {
		t.Error("different props returned cached bundle for other props")
	}
}

func TestRender_UnknownComponentErrors(t *testing.T) {
	b := newTestBundler(t, map[string]string{
		"Hello.tsx": simpleComponent,
	}, Config{})

	_, err := b.Render(context.Background(), "does/not/exist", nil)
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
	}, Config{Dev: true, Minify: false})

	ctx := context.Background()
	first, err := b.Render(ctx, "Hello", nil)
	if err != nil {
		t.Fatalf("first Render: %v", err)
	}
	second, err := b.Render(ctx, "Hello", nil)
	if err != nil {
		t.Fatalf("second Render: %v", err)
	}
	// In dev mode the cache is disabled, so each call rebuilds -> distinct pointers.
	if first == second {
		t.Error("dev mode should not cache; got identical pointer")
	}
}

func TestNew_RequiresMandatoryConfig(t *testing.T) {
	// Missing everything: should error without needing Node.
	if _, err := New(Config{}); err == nil {
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
// pathologically slow (e.g. spawning a node process per call).
func TestRender_WarmRenderIsFast(t *testing.T) {
	b := newTestBundler(t, map[string]string{
		"Hello.tsx": simpleComponent,
	}, Config{Minify: true})

	ctx := context.Background()
	if _, err := b.Render(ctx, "Hello", map[string]any{"name": "warm"}); err != nil {
		t.Fatalf("warm-up Render: %v", err)
	}
	// Second identical render is a cache hit; should be near-instant.
	start := time.Now()
	if _, err := b.Render(ctx, "Hello", map[string]any{"name": "warm"}); err != nil {
		t.Fatalf("cached Render: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 50*time.Millisecond {
		t.Errorf("cache hit took %v, expected <50ms (is caching working?)", elapsed)
	}
}
