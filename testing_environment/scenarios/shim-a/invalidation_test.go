package shim_a

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	go_solid "github.com/lilybw/go-solid"
	"github.com/lilybw/go-solid/shared/hmr"
)

// The bug these guard: nothing invalidated the caches on a file *write*.
// DirectoryWatcher swallowed fsnotify.Write, and the HMR watcher told the
// browser to reload without dropping the cached artifact first — so the reload
// re-fetched a byte-identical stale bundle and dev edits needed a restart.

const watchSettle = 2 * time.Second

// writeFile writes body to path and restores the original at test end.
func writeFile(t *testing.T, path, body string) {
	t.Helper()
	orig, err := os.ReadFile(path)
	if err == nil {
		t.Cleanup(func() { _ = os.WriteFile(path, orig, 0o644) })
	} else if os.IsNotExist(err) {
		t.Cleanup(func() { _ = os.Remove(path) })
	} else {
		t.Fatalf("read %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func freshBundler(t *testing.T, cfg *go_solid.Config) *go_solid.Bundler {
	t.Helper()
	// A warm disk cache would mask a mem-cache miss, so start from empty.
	if err := os.RemoveAll(filepath.Join(componentsDir, ".go_solid", "component_cache")); err != nil {
		t.Fatalf("clear disk cache: %v", err)
	}
	cfg.Components = componentsDir
	b, err := go_solid.New(cfg)
	if err != nil {
		t.Fatalf("go_solid.New: %v", err)
	}
	t.Cleanup(b.Close)
	return b
}

func renderJS(t *testing.T, b *go_solid.Bundler, component string) string {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	out, err := b.Prepare(component, nil).ForRequest(rec, req).Render()
	if err != nil {
		t.Fatalf("render %s: %v", component, err)
	}
	return out.JS
}

// waitForMarker polls until the rebuilt bundle contains marker, so the test
// tracks the debounce rather than racing it.
func waitForMarker(t *testing.T, b *go_solid.Bundler, component, marker string) bool {
	t.Helper()
	deadline := time.Now().Add(watchSettle)
	for {
		if strings.Contains(renderJS(t, b, component), marker) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestEditedComponentIsRebuilt_ReactiveRegistry(t *testing.T) {
	gate(t)
	b := freshBundler(t, &go_solid.Config{ReactiveRegistry: true})

	before := renderJS(t, b, "Home")
	writeFile(t, filepath.Join(componentsDir, "Home.jsx"),
		`export default () => <h1>MARKER_REACTIVE</h1>;`)

	if !waitForMarker(t, b, "Home", "MARKER_REACTIVE") {
		t.Fatalf("edit never rebuilt; still serving the cached bundle (unchanged=%v)",
			before == renderJS(t, b, "Home"))
	}
}

func TestEditedComponentIsRebuilt_HMROnly(t *testing.T) {
	gate(t)
	// HMR without ReactiveRegistry: the HMR watcher is the only thing running,
	// so it has to invalidate on its own.
	b := freshBundler(t, &go_solid.Config{
		HMR: &hmr.HMRConfig{Mux: http.NewServeMux()}, // ServeMux.Handle already satisfies MuxLike
	})

	renderJS(t, b, "Home") // populate the caches and the dependency index
	writeFile(t, filepath.Join(componentsDir, "Home.jsx"),
		`export default () => <h1>MARKER_HMR</h1>;`)

	if !waitForMarker(t, b, "Home", "MARKER_HMR") {
		t.Fatal("HMR signalled a reload but the server kept serving the stale bundle")
	}
}

// Editing a file a component imports must rebuild the importer, not just the
// edited file. This is the DependencyIndex path.
func TestEditedDependencyRebuildsImporter(t *testing.T) {
	gate(t)

	dep := filepath.Join(componentsDir, "greeting.js")
	writeFile(t, dep, `export const greeting = "DEP_BEFORE";`)
	writeFile(t, filepath.Join(componentsDir, "Home.jsx"),
		`import { greeting } from "./greeting.js";
		 export default () => <h1>{greeting}</h1>;`)

	b := freshBundler(t, &go_solid.Config{ReactiveRegistry: true})

	if js := renderJS(t, b, "Home"); !strings.Contains(js, "DEP_BEFORE") {
		t.Fatalf("dependency was not inlined on first render; got:\n%s", js)
	}

	if err := os.WriteFile(dep, []byte(`export const greeting = "DEP_AFTER";`), 0o644); err != nil {
		t.Fatal(err)
	}

	if !waitForMarker(t, b, "Home", "DEP_AFTER") {
		t.Fatal("editing an imported file did not rebuild the component that imports it")
	}
}

// Deleting a component must drop it from the registry and the caches.
func TestDeletedComponentIsDropped(t *testing.T) {
	gate(t)

	scratch := filepath.Join(componentsDir, "Scratch.jsx")
	writeFile(t, scratch, `export default () => <p>scratch</p>;`)

	b := freshBundler(t, &go_solid.Config{ReactiveRegistry: true})
	renderJS(t, b, "Scratch")

	if err := os.Remove(scratch); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(watchSettle)
	for {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		_, err := b.Prepare("Scratch", nil).ForRequest(rec, req).Render()
		if err != nil {
			return // correctly gone
		}
		if time.Now().After(deadline) {
			t.Fatal("deleted component still renders from cache")
		}
		time.Sleep(50 * time.Millisecond)
	}
}
