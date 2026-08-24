package shim_c

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/lilybw/go-solid/internal"
	hmr_int "github.com/lilybw/go-solid/internal/hmr"
	"github.com/lilybw/go-solid/internal/meta"
	shared_hmr "github.com/lilybw/go-solid/shared/hmr"
)

// Selectors and hot reload
// ---------------------------------------------------------------------------
// A reload is routed by component name, and a selector is a component name that
// contains "#". That character is a fragment delimiter in a URL, so the client
// registration is the one place a selector could plausibly fall apart: encode
// it wrongly and the browser never sends the half after the "#", the server
// registers a shorter name than the one being reloaded, and edits to a
// sub-component silently stop reaching the page.
//
// The tests below drive the real hub over a real websocket, and the real
// watcher over a real file change.

const (
	panelHeader = meta.QualifiedName("Panel#Header")
	panelFooter = meta.QualifiedName("Panel#Footer")
	bannerName  = meta.QualifiedName("legacy/Banner")
)

// hmrFixture is a running hub on a real server, plus whatever the test needs to
// pretend to be a browser talking to it.
type hmrFixture struct {
	hub  *hmr_int.Hub
	path string
	srv  *httptest.Server
}

func newHMRFixture(t *testing.T) *hmrFixture {
	t.Helper()

	mux := http.NewServeMux()
	cfg, err := hmr_int.NormalizeHMRConfig(&shared_hmr.HMRConfig{Mux: mux})
	if err != nil {
		t.Fatalf("NormalizeHMRConfig: %v", err)
	}
	hub := hmr_int.NewHub(cfg)
	mux.Handle(cfg.Path, hub.Handler())

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return &hmrFixture{hub: hub, path: cfg.Path, srv: srv}
}

// tab is a connected client, standing in for an open browser tab showing one
// component.
type tab struct {
	conn    *websocket.Conn
	reloads chan struct{}
}

// open connects exactly the way the injected script does: the same path, the
// same query parameter, the same encoding.
func (f *hmrFixture) open(t *testing.T, component meta.QualifiedName) *tab {
	t.Helper()

	endpoint := "ws" + strings.TrimPrefix(f.srv.URL, "http") + f.path +
		"?c=" + url.QueryEscape(component)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, endpoint, nil)
	if err != nil {
		t.Fatalf("dial for %q: %v", component, err)
	}
	t.Cleanup(func() { _ = conn.Close(websocket.StatusNormalClosure, "") })

	tab := &tab{conn: conn, reloads: make(chan struct{}, 8)}
	go func() {
		for {
			if _, _, err := conn.Read(context.Background()); err != nil {
				return
			}
			select {
			case tab.reloads <- struct{}{}:
			default:
			}
		}
	}()
	return tab
}

// awaitReload waits for the tab to be told to reload.
func (this *tab) awaitReload(t *testing.T, label string) {
	t.Helper()
	select {
	case <-this.reloads:
	case <-time.After(watchSettle):
		t.Errorf("%s was never told to reload", label)
	}
}

// expectQuiet asserts the tab is left alone.
func (this *tab) expectQuiet(t *testing.T, label string) {
	t.Helper()
	select {
	case <-this.reloads:
		t.Errorf("%s was reloaded and should not have been", label)
	case <-time.After(500 * time.Millisecond):
	}
}

// The connection has to survive the round trip through a URL query, where "#"
// would otherwise end the query and take the export name with it.
func TestSelectorSurvivesTheWebsocketRegistration(t *testing.T) {
	f := newHMRFixture(t)

	header := f.open(t, panelHeader)
	footer := f.open(t, panelFooter)
	// Registration is asynchronous on the server side; give it a moment before
	// asserting on who receives what.
	time.Sleep(200 * time.Millisecond)

	f.hub.Reload(panelHeader)

	header.awaitReload(t, "the Panel#Header tab")
	footer.expectQuiet(t, "the Panel#Footer tab")
}

// If the "#" were lost on the way in, every selector on a file would register
// under the file's own name and reloads would go to all of them at once.
func TestSelectorsAreNotCollapsedOntoTheirFile(t *testing.T) {
	f := newHMRFixture(t)

	header := f.open(t, panelHeader)
	bare := f.open(t, "Panel")
	time.Sleep(200 * time.Millisecond)

	f.hub.Reload("Panel")

	bare.awaitReload(t, "the Panel tab")
	header.expectQuiet(t, "the Panel#Header tab")
}

// The injected script is what a browser actually runs, so it has to carry the
// selector intact and hand it to encodeURIComponent rather than splicing it
// into the URL raw.
func TestInjectedClientScriptCarriesTheSelector(t *testing.T) {
	script := hmr_int.ClientScript("/__go_solid_hmr__", panelHeader)

	if !strings.Contains(script, `"Panel#Header"`) {
		t.Errorf("the script does not carry the selector:\n%s", script)
	}
	if !strings.Contains(script, "encodeURIComponent") {
		t.Errorf("the selector is not encoded before it goes into the query:\n%s", script)
	}
	if strings.Contains(script, `"?c=" + comp`) {
		t.Errorf("the selector is spliced in raw; the browser would cut it at the #:\n%s", script)
	}
}

// ---------------------------------------------------------------------------
// Reload routing: from a file change to the tabs showing it.
// ---------------------------------------------------------------------------

// recorder captures which components the watcher invalidated, which is the half
// of a reload the browser cannot observe: dropping the cached artifact before
// telling the page to fetch it again.
type recorder struct {
	mu   sync.Mutex
	seen []meta.QualifiedName
}

func (this *recorder) record(name meta.QualifiedName) {
	this.mu.Lock()
	defer this.mu.Unlock()
	this.seen = append(this.seen, name)
}

func (this *recorder) contains(name meta.QualifiedName) bool {
	this.mu.Lock()
	defer this.mu.Unlock()
	return contains(this.seen, name)
}

func contains(names []meta.QualifiedName, want meta.QualifiedName) bool {
	for _, name := range names {
		if name == want {
			return true
		}
	}
	return false
}

// One file backs several components. Editing it has to reach every one of them
// that somebody is looking at — the index maps a source to the names built from
// it, and those names are selectors.
func TestEditingAFileReloadsEverySelectorBuiltFromIt(t *testing.T) {
	p := newProject(t)
	f := newHMRFixture(t)

	// What the bundler records after building each component. Both selections
	// come out of one file; Banner is an unrelated component so the test can
	// tell targeted routing from a broadcast.
	index := internal.NewDepIndex()
	panelFile := p.componentFile("Panel.tsx")
	typesFile := p.componentFile("types.ts")
	index.Record(panelHeader, []string{panelFile, typesFile})
	index.Record(panelFooter, []string{panelFile, typesFile})
	index.Record(bannerName, []string{p.componentFile("legacy/Banner.jsx")})

	invalidated := &recorder{}
	watcher, err := hmr_int.NewWatcher(p.components, index, f.hub, invalidated.record,
		func(e error) { t.Logf("watch error: %v", e) })
	if err != nil {
		t.Fatalf("NewWatcher: %v", err)
	}
	t.Cleanup(watcher.Stop)

	header := f.open(t, panelHeader)
	footer := f.open(t, panelFooter)
	banner := f.open(t, bannerName)
	time.Sleep(200 * time.Millisecond)

	touch(t, panelFile)

	header.awaitReload(t, "the Panel#Header tab")
	footer.awaitReload(t, "the Panel#Footer tab")
	banner.expectQuiet(t, "the legacy/Banner tab")

	for _, name := range []meta.QualifiedName{panelHeader, panelFooter} {
		if !invalidated.contains(name) {
			t.Errorf("%q was reloaded without its cached artifact being dropped first", name)
		}
	}
	if invalidated.contains(bannerName) {
		t.Errorf("%q was invalidated by an edit to another file", bannerName)
	}
}

// A dependency shared by two selectors reaches both, which is the case that
// distinguishes a real reverse index from reloading whatever file was touched.
func TestEditingASharedDependencyReachesEverySelectorThatUsesIt(t *testing.T) {
	p := newProject(t)
	f := newHMRFixture(t)

	index := internal.NewDepIndex()
	typesFile := p.componentFile("types.ts")
	index.Record(panelHeader, []string{p.componentFile("Panel.tsx"), typesFile})
	index.Record(panelFooter, []string{p.componentFile("Panel.tsx"), typesFile})

	watcher, err := hmr_int.NewWatcher(p.components, index, f.hub,
		func(meta.QualifiedName) {}, func(e error) { t.Logf("watch error: %v", e) })
	if err != nil {
		t.Fatalf("NewWatcher: %v", err)
	}
	t.Cleanup(watcher.Stop)

	header := f.open(t, panelHeader)
	footer := f.open(t, panelFooter)
	time.Sleep(200 * time.Millisecond)

	touch(t, typesFile)

	header.awaitReload(t, "the Panel#Header tab")
	footer.awaitReload(t, "the Panel#Footer tab")
}

// touch rewrites a file with a changed body and moves its timestamp on, so the
// change is unmistakable whatever the filesystem's granularity.
func touch(t *testing.T, path string) {
	t.Helper()

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if err := os.WriteFile(path, append(body, []byte("\n// edited\n")...), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	future := time.Now().Add(time.Second)
	if err := os.Chtimes(path, future, future); err != nil {
		t.Fatalf("touch %s: %v", path, err)
	}
}
