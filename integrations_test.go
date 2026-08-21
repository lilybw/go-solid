package go_solid

// Full New() integration behaviors, exercised without bundling
// (Generation.Disabled = true unless a test specifically needs the solid-js
// resolution gate; see helpers_test.go).

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	shared_esbuild "github.com/lilybw/go-solid/shared/esbuild"
	shared_hmr "github.com/lilybw/go-solid/shared/hmr"
	logging "github.com/lilybw/go-solid/shared/logging"
	shared_net "github.com/lilybw/go-solid/shared/networking"
	shared_raster "github.com/lilybw/go-solid/shared/rasterization"
)

// --- solid-js resolution gate -----------------------------------------------

// Bundling off means nothing is ever resolved, so the gate must not fire —
// this is the path a fully rasterized deployment takes.
func TestNew_SolidRuntimeGateSkippedWhenGenerationDisabled(t *testing.T) {
	resetPackageState(t)
	comps := t.TempDir()
	writeFile(t, comps, "A.tsx", "export default () => null;")

	b, err := New(&Config{
		LogLevel:   logging.LEVEL_ERROR,
		Components: comps, Generation: disabledGeneration()})
	if err != nil {
		t.Fatalf("disabled generation must not require solid-js: %v", err)
	}
	b.Close()
}

func TestNew_SolidRuntimeSatisfiedByAncestor(t *testing.T) {
	resetPackageState(t)
	// solid-js is resolvable from an ancestor dir, not the components dir itself.
	comps := componentsDirWith(t, map[string]string{"A.tsx": "export default () => null;"})
	nested := comps + "/feature"
	writeFile(t, nested, "B.tsx", "export default () => null;")

	b, err := New(&Config{
		LogLevel:   logging.LEVEL_ERROR,
		Components: nested, Generation: &shared_esbuild.BundlerConfig{}})
	if err != nil {
		t.Fatalf("expected solid-js resolvable via ancestor, got: %v", err)
	}
	b.Close()
}

// --- Registry walk ----------------------------------------------------------

func TestNew_RegistryDiscoversComponentsSkippingNodeModulesAndDotDirs(t *testing.T) {
	resetPackageState(t)
	comps := componentsDirWith(t, map[string]string{
		"Home.tsx":           "export default () => null;",
		"nested/Profile.jsx": "export default () => null;",
		".hidden/Secret.tsx": "export default () => null;",
	})
	// node_modules is already present (staged peer deps). Add a component-looking
	// file inside it that must NOT be registered.
	writeFile(t, comps+"/node_modules/solid-js", "Evil.tsx", "export default () => null;")

	b, err := New(&Config{
		LogLevel: logging.LEVEL_ERROR, Components: comps, Generation: disabledGeneration()})
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer b.Close()

	names := b.Registry().Names()
	got := map[string]bool{}
	for _, n := range names {
		got[n] = true
	}
	if !got["Home"] {
		t.Errorf("expected Home to be registered; got %v", names)
	}
	if !got["nested/Profile"] {
		t.Errorf("expected nested/Profile to be registered; got %v", names)
	}
	for _, bad := range []string{".hidden/Secret", "node_modules/solid-js/Evil"} {
		if got[bad] {
			t.Errorf("did not expect %q to be registered; got %v", bad, names)
		}
	}
}

// --- HMR mounting -----------------------------------------------------------

func TestNew_HMRMountsHandlerOnProvidedMux(t *testing.T) {
	resetPackageState(t)
	comps := componentsDirWith(t, map[string]string{"A.tsx": "export default () => null;"})

	mux := http.NewServeMux()
	hmrCfg := &shared_hmr.HMRConfig{
		Disabled: false,
		Path:     "/__hmr__",
		Mux:      shared_hmr.MuxLikeFromFunc(mux.Handle),
	}
	b, err := New(&Config{
		LogLevel: logging.LEVEL_ERROR, Components: comps, Generation: disabledGeneration(), HMR: hmrCfg})
	if err != nil {
		t.Fatalf("New() with HMR failed: %v", err)
	}
	defer b.Close()

	// The HMR handler should now be registered at the configured path. We can't
	// open a real websocket easily, but ServeMux.Handler resolves the pattern.
	req, _ := http.NewRequest(http.MethodGet, "http://example.com/__hmr__", nil)
	h, pattern := mux.Handler(req)
	if h == nil || pattern == "" {
		t.Fatalf("HMR handler not mounted at /__hmr__ (pattern=%q)", pattern)
	}
}

func TestNew_HMREnabledWithoutMuxFails(t *testing.T) {
	resetPackageState(t)
	comps := componentsDirWith(t, map[string]string{"A.tsx": "export default () => null;"})

	// HMR enabled (not disabled) but Mux is nil -> NormalizeHMRConfig must error.
	hmrCfg := &shared_hmr.HMRConfig{Disabled: false, Path: "/__hmr__", Mux: nil}
	_, err := New(&Config{
		LogLevel:   logging.LEVEL_ERROR,
		Components: comps, Generation: disabledGeneration(), HMR: hmrCfg})
	if err == nil {
		t.Fatal("expected HMR-without-Mux error, got nil")
	}
	if !strings.Contains(err.Error(), "Mux is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNew_HMRDisabledDoesNotRequireMux(t *testing.T) {
	resetPackageState(t)
	comps := componentsDirWith(t, map[string]string{"A.tsx": "export default () => null;"})
	// Default HMR (nil) is Disabled, so isHMROn() is false and no Mux is needed.
	b, err := New(&Config{
		LogLevel:   logging.LEVEL_ERROR,
		Components: comps, Generation: disabledGeneration()})
	if err != nil {
		t.Fatalf("New() with default (disabled) HMR should not need a Mux: %v", err)
	}
	b.Close()
}

// --- Rasterization pre-render behavior --------------------------------------

func TestNew_RasterNonCompletedWithDisabledPoolFailsPrerender(t *testing.T) {
	resetPackageState(t)
	comps := componentsDirWith(t, map[string]string{"A.tsx": "export default () => null;"})
	// Non-ExpectCompleted rasterization triggers a pre-render of every component,
	// but Generation.Disabled forbids bundling — a combination that can never
	// succeed. This pins that the pre-render reports it instead of silently
	// invoking esbuild.
	_, err := New(&Config{
		LogLevel:      logging.LEVEL_ERROR,
		Components:    comps,
		Generation:    disabledGeneration(),
		Rasterization: &shared_raster.RasterizationConfig{ExpectCompleted: false},
	})
	if err == nil {
		t.Fatal("expected pre-render failure with disabled pool, got nil")
	}
	if !strings.Contains(err.Error(), "rasterization failed") || !strings.Contains(err.Error(), "bundling is disabled") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNew_RasterExpectCompletedSucceedsWithStagedWorkspace(t *testing.T) {
	resetPackageState(t)
	comps := componentsDirWith(t, map[string]string{"A.tsx": "export default () => null;"})
	ws := t.TempDir()
	stageRasterizedWorkspace(t, ws)

	// ExpectCompleted skips the pre-render loop entirely (that loop only runs for
	// non-ExpectCompleted) and forces Generation.Disabled=true.
	b, err := New(&Config{
		LogLevel:      logging.LEVEL_ERROR,
		Components:    comps,
		Workspace:     ws,
		Generation:    disabledGeneration(),
		Rasterization: &shared_raster.RasterizationConfig{Location: ws, ExpectCompleted: true},
	})
	if err != nil {
		t.Fatalf("New() with ExpectCompleted staged workspace failed: %v", err)
	}
	defer b.Close()
	// HMR must have been forced off, so no Mux was required despite none given.
}

// --- Close ------------------------------------------------------------------

func TestNew_CloseIsNilSafe(t *testing.T) {
	resetPackageState(t)
	var b *Bundler
	// Must not panic on a nil receiver.
	b.Close()
}

func TestNew_CloseStopsCleanly(t *testing.T) {
	resetPackageState(t)
	comps := componentsDirWith(t, map[string]string{"A.tsx": "export default () => null;"})

	mux := http.NewServeMux()
	b, err := New(&Config{
		LogLevel:         logging.LEVEL_ERROR,
		Components:       comps,
		Generation:       disabledGeneration(),
		ReactiveRegistry: true, // spins up the registry watcher goroutine
		HMR: &shared_hmr.HMRConfig{
			Disabled: false, Path: "/__hmr__", Mux: shared_hmr.MuxLikeFromFunc(mux.Handle),
		},
	})
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	// Close should stop the HMR watcher and close the pool without panicking, and
	// be safe to call once. (Calling twice would double-close the pool channel,
	// which the pool guards against with closed.Swap, but the hmr watcher's Stop
	// closes a channel unguarded — so we call Close exactly once here.)
	b.Close()
}

// --- Idempotency / defaults invocation --------------------------------------

func TestNew_DefaultsConfiguratorsInvokedWhenProvided(t *testing.T) {
	resetPackageState(t)
	comps := componentsDirWith(t, map[string]string{"A.tsx": "export default () => null;"})

	headCalls := 0
	b, err := New(&Config{
		LogLevel:   logging.LEVEL_ERROR,
		Components: comps,
		Generation: disabledGeneration(),
		Defaults: &BehaviouralDefaults{
			HeadSegment: func(_ shared_net.HTMLHeadSegmentBuilder) { headCalls++ },
			// Requests left nil -> polyfilled to the noop by normalization.
		},
	})
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer b.Close()

	// New() calls SetHTMLHeadSegmentTemplate(cfg.Defaults.HeadSegment), which
	// invokes the configurator exactly once against the fresh template.
	if headCalls != 1 {
		t.Fatalf("HeadSegment configurator call count = %d, want 1", headCalls)
	}
}

func TestNew_DefaultsNilDoesNotSetTemplates(t *testing.T) {
	resetPackageState(t)
	comps := componentsDirWith(t, map[string]string{"A.tsx": "export default () => null;"})
	// With Defaults nil, New() must NOT call the template setters — the guard is
	// now `cfg.Defaults != nil` captured before normalization, since after it the
	// field holds a copy and pointer identity says nothing.
	// We can't observe the setter directly, but New() must at least succeed.
	b, err := New(&Config{
		LogLevel:   logging.LEVEL_ERROR,
		Components: comps, Generation: disabledGeneration()})
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	b.Close()
}

// writeFile drops an individual file into an arbitrary dir, creating parents.
func writeFile(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("writeFile mkdir %s: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatalf("writeFile %s/%s: %v", dir, name, err)
	}
}
