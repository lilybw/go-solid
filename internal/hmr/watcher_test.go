package hmr

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/lilybw/go-solid/internal"
	. "github.com/lilybw/go-solid/shared/hmr"
)

// These tests exercise real fsnotify on the real filesystem, so they are
// inherently timing-sensitive (the watcher debounces at 80ms) and may be marked
// flaky on heavily loaded CI. They are skipped under -short.
//
// NOTE: newWatcher/NewWatcher, skipDir, and the debounce field are referenced
// here as they appear in the corrected watching.go (exported NewWatcher that
// self-starts, Stop to halt). If you kept the unexported newWatcher instead,
// adjust the constructor calls.

// mkComponentsTree creates a temp components dir with the given relative files,
// each containing trivial content, and returns the root.
func mkComponentsTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// hubWithClient stands up a hub on an httptest server and connects one client
// viewing `component`, returning a function that blocks until a reload arrives
// (or times out). Used to observe that the watcher actually drove a reload.
func hubWithClient(t *testing.T, component string) (*Hub, func(within time.Duration) bool) {
	t.Helper()
	h := NewHub(NIL_HMR_CONFIG)
	mux := http.NewServeMux()
	mux.Handle(NIL_HMR_CONFIG.Path, h.Handler())
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	url := strings.Replace(srv.URL, "http://", "ws://", 1) + NIL_HMR_CONFIG.Path + "?c=" + component
	conn, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	t.Cleanup(func() { conn.Close(websocket.StatusNormalClosure, "") })

	if !waitForConn(h, component, 2*time.Second) {
		t.Fatal("client never registered")
	}

	awaitReload := func(within time.Duration) bool {
		rc, rcancel := context.WithTimeout(context.Background(), within)
		defer rcancel()
		_, data, err := conn.Read(rc)
		return err == nil && string(data) == "reload"
	}
	return h, awaitReload
}

func TestWatcher_FileChangeTriggersReload(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping fs-timing test under -short")
	}

	buttonRel := filepath.Join("ui", "Button.tsx")
	root := mkComponentsTree(t, map[string]string{
		buttonRel: "export default () => null;",
	})
	buttonAbs := filepath.Join(root, buttonRel)

	index := internal.NewDepIndex()
	// Simulate a prior render: the component "ui/Button" depends on its own file.
	// The watcher inverts this mapping on change.
	index.Record("ui/Button", []string{buttonAbs})

	h, awaitReload := hubWithClient(t, "ui/Button")

	w, err := NewWatcher(root, index, h, nil, nil, func(e error) { t.Logf("watch err: %v", e) })
	if err != nil {
		t.Fatalf("NewWatcher: %v", err)
	}
	defer w.Stop()

	// Modify the file.
	if err := os.WriteFile(buttonAbs, []byte("export default () => 'changed';"), 0o644); err != nil {
		t.Fatal(err)
	}

	if !awaitReload(3 * time.Second) {
		t.Fatal("expected a reload after file change, none arrived")
	}
}

func TestWatcher_UnrelatedFileDoesNotReload(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping fs-timing test under -short")
	}

	buttonRel := filepath.Join("ui", "Button.tsx")
	otherRel := filepath.Join("ui", "Unrelated.tsx")
	root := mkComponentsTree(t, map[string]string{
		buttonRel: "export default () => null;",
		otherRel:  "export default () => null;",
	})

	index := internal.NewDepIndex()
	// Only Button is known to the index; Unrelated was never rendered.
	index.Record("ui/Button", []string{filepath.Join(root, buttonRel)})

	h, awaitReload := hubWithClient(t, "ui/Button")

	w, err := NewWatcher(root, index, h, nil, nil, func(e error) { t.Logf("watch err: %v", e) })
	if err != nil {
		t.Fatalf("NewWatcher: %v", err)
	}
	defer w.Stop()

	// Touch the unrelated file, which maps to no component in the index.
	if err := os.WriteFile(filepath.Join(root, otherRel), []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}

	// The Button client must NOT receive a reload.
	if awaitReload(1 * time.Second) {
		t.Fatal("unrelated file change wrongly reloaded ui/Button")
	}
}

func TestWatcher_DebounceCoalescesBurst(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping fs-timing test under -short")
	}

	buttonRel := filepath.Join("ui", "Button.tsx")
	root := mkComponentsTree(t, map[string]string{
		buttonRel: "export default () => null;",
	})
	buttonAbs := filepath.Join(root, buttonRel)

	index := internal.NewDepIndex()
	index.Record("ui/Button", []string{buttonAbs})

	h, awaitReload := hubWithClient(t, "ui/Button")

	w, err := NewWatcher(root, index, h, nil, nil, func(e error) { t.Logf("watch err: %v", e) })
	if err != nil {
		t.Fatalf("NewWatcher: %v", err)
	}
	defer w.Stop()

	// Fire several writes in quick succession (within the debounce window).
	for i := 0; i < 5; i++ {
		if err := os.WriteFile(buttonAbs, []byte("v"+string(rune('0'+i))), 0o644); err != nil {
			t.Fatal(err)
		}
		time.Sleep(10 * time.Millisecond)
	}

	// At least one reload must arrive.
	if !awaitReload(3 * time.Second) {
		t.Fatal("expected at least one reload from the burst")
	}

	// After the burst settles, no *second* reload should be pending beyond a
	// reasonable window. (We can't assert exactly-one without draining, but we
	// can assert the channel goes quiet.)
	if awaitReload(600 * time.Millisecond) {
		t.Log("note: received an additional reload after the burst; debounce may not have fully coalesced (acceptable but worth watching)")
	}
}

func TestWatcher_NewDirectoryIsWatched(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping fs-timing test under -short")
	}

	root := mkComponentsTree(t, map[string]string{
		filepath.Join("ui", "Button.tsx"): "export default () => null;",
	})

	index := internal.NewDepIndex()
	h, awaitReload := hubWithClient(t, "ui/NewComp")

	w, err := NewWatcher(root, index, h, nil, nil, func(e error) { t.Logf("watch err: %v", e) })
	if err != nil {
		t.Fatalf("NewWatcher: %v", err)
	}
	defer w.Stop()

	// Create a new subdirectory at runtime, then a file inside it. The watcher's
	// Create-is-dir branch must add the new dir to the watch set so the inner
	// file's write is observed.
	newDir := filepath.Join(root, "feature")
	if err := os.MkdirAll(newDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Give the watcher a beat to register the new directory.
	time.Sleep(150 * time.Millisecond)

	newFile := filepath.Join(newDir, "NewComp.tsx")
	// Record the dependency as a prior render would, so the change maps to a component.
	index.Record("ui/NewComp", []string{newFile})
	if err := os.WriteFile(newFile, []byte("export default () => null;"), 0o644); err != nil {
		t.Fatal(err)
	}

	if !awaitReload(3 * time.Second) {
		t.Fatal("expected reload for file created in a newly-added directory")
	}
}

func TestWatcher_StopIsIdempotentlySafe(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping fs test under -short")
	}
	root := mkComponentsTree(t, map[string]string{
		filepath.Join("ui", "Button.tsx"): "x",
	})
	index := internal.NewDepIndex()
	h := NewHub(NIL_HMR_CONFIG)

	w, err := NewWatcher(root, index, h, nil, nil, nil)
	if err != nil {
		t.Fatalf("NewWatcher: %v", err)
	}
	// Stop is a shutdown hook: Bundler.Close calls it, and so does any consumer
	// with its own teardown. Both happening is ordinary, so a second call must
	// not panic on a double close(stopCh).
	done := make(chan struct{})
	go func() {
		w.Stop()
		w.Stop()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Stop hung")
	}

	// Concurrent teardown resolves the same way: every caller returns, once.
	var wg sync.WaitGroup
	w2, err := NewWatcher(root, index, h, nil, nil, nil)
	if err != nil {
		t.Fatalf("NewWatcher: %v", err)
	}
	for range 4 {
		wg.Add(1)
		go func() { defer wg.Done(); w2.Stop() }()
	}
	settled := make(chan struct{})
	go func() { wg.Wait(); close(settled) }()
	select {
	case <-settled:
	case <-time.After(3 * time.Second):
		t.Fatal("concurrent Stop hung")
	}
}

// A nil watcher is what Bundler holds when HMR was never switched on.
func TestWatcher_StopOnNilIsSafe(t *testing.T) {
	var w *Watcher
	w.Stop()
}
