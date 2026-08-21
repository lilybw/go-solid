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
	"github.com/lilybw/go-solid/shared/logging"
)

func TestEditedComponentIsRebuilt(t *testing.T) {
	gate(t)
	src := filepath.Join(componentsDir, "Home.jsx")
	orig, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.WriteFile(src, orig, 0o644) })
	os.RemoveAll(filepath.Join(componentsDir, ".go_solid", "component_cache"))

	b, err := go_solid.New(&go_solid.Config{
		Components:       componentsDir,
		LogLevel:         logging.LEVEL_ERROR,
		ReactiveRegistry: true,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(b.Close)

	render := func() string {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		out, err := b.Prepare("Home", nil).ForRequest(rec, req).Render()
		if err != nil {
			t.Fatalf("render: %v", err)
		}
		return out.JS
	}

	first := render()
	if err := os.WriteFile(src, []byte("export default () => <h1>EDITED_MARKER_XYZ</h1>;"), 0o644); err != nil {
		t.Fatal(err)
	}
	time.Sleep(1500 * time.Millisecond) // let any watcher fire

	second := render()
	if strings.Contains(second, "EDITED_MARKER_XYZ") {
		return // rebuilt correctly
	}
	t.Errorf("edited component was not rebuilt; served stale bundle (identical=%v)", first == second)
}
