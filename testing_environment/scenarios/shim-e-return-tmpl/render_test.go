// Package shim_e is the end-to-end guard for the return-statement splice
// regression.
//
// A component whose body was a parenthesized return compiled to
// `return_tmpl$()` - one undefined identifier rather than a return of a
// template clone. It built cleanly, bundled cleanly, served a 200, and threw
// only once a browser executed it:
//
//	Uncaught ReferenceError: return_tmpl$ is not defined
//
// Nothing here executes JavaScript, so the ReferenceError itself is out of
// reach. It does not need to be reached: the fusion is a lexical artifact of
// the compiled output, so finding it in the payload the browser would receive
// is the same signal, raised earlier and without a browser to raise it.
package shim_e

import (
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"

	go_solid "github.com/lilybw/go-solid"
	"github.com/lilybw/go-solid/shared/hmr"
	"github.com/lilybw/go-solid/shared/logging"
)

// fusedToken matches a JavaScript keyword welded onto one of the compiler's
// generated identifiers.
//
// Deliberately narrow: it does not match a keyword followed by any identifier,
// because a bundle carries third-party code where `new_line` or `in_progress`
// are ordinary names. Only the compiler's own generated prefixes - _$ for
// runtime helpers, _tmpl$ _el$ _c$ _p$ _v$ _ref$ for locals - can end up fused,
// so those are the only ones worth looking for.
var fusedToken = regexp.MustCompile(
	`\b(?:return|typeof|yield|await|case|in|of|instanceof|new|delete|void|else|do)` +
		`(?:_\$|_(?:tmpl|el|c|p|v|ref)\$)`)

// Each component places its JSX in a different syntactic position, since the
// bug was in the positions rather than in the lowering. Marker is text from
// the component's own template, which reaching the payload proves it compiled
// and shipped rather than being quietly skipped.
var components = []struct {
	name   string
	marker string
}{
	{name: "TopBar", marker: "top-bar-container"},
	{name: "Returns", marker: "returns-container"},
}

func TestServedPayloadHasNoFusedTokens(t *testing.T) {
	srv, renderErr := startShim(t)

	for _, c := range components {
		t.Run(c.name, func(t *testing.T) {
			doc := get(t, srv.URL+"/?c="+c.name)
			if err := renderErr(); err != nil {
				t.Fatalf("rendering %s: %v", c.name, err)
			}

			js := withScripts(t, srv.URL, doc)
			if !strings.Contains(js, c.marker) {
				t.Fatalf("%s never reached the browser: %q is absent from the payload,"+
					" so the assertion below would pass vacuously\n%s", c.name, c.marker, js)
			}
			if m := fusedToken.FindString(js); m != "" {
				t.Errorf("payload contains %q: a keyword is welded onto the identifier"+
					" after it, and the browser will raise"+
					" ReferenceError: %s is not defined", m, m)
			}
		})
	}
}

// startShim builds the bundler and serves it on an ephemeral port, returning
// the server and an accessor for the last render error.
//
// The error needs an accessor because the handler runs on another goroutine
// and cannot fail the test itself: calling t.Fatalf off the test goroutine
// stops that goroutine only, and the test carries on as though nothing had
// gone wrong.
func startShim(t *testing.T) (*httptest.Server, func() error) {
	t.Helper()

	mux := http.NewServeMux()
	bundler := newBundler(t, mux)

	var mu sync.Mutex
	var lastErr error

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		name := r.URL.Query().Get("c")
		if name == "" {
			http.Error(w, "no component requested", http.StatusBadRequest)
			return
		}
		_, err := bundler.Prepare(name, nil).ForRequest(w, r).Render()
		mu.Lock()
		lastErr = err
		mu.Unlock()
	})

	// Registered after the bundler's own cleanup, so it runs first: the
	// bundler must outlive every request still in flight.
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return srv, func() error {
		mu.Lock()
		defer mu.Unlock()
		return lastErr
	}
}

func newBundler(t *testing.T, mux *http.ServeMux) *go_solid.Bundler {
	t.Helper()

	wd, err := filepath.Abs(".")
	if err != nil {
		t.Fatalf("resolving the working directory: %v", err)
	}
	b, err := go_solid.New(&go_solid.Config{
		Components:     filepath.Join(wd, "components"),
		DisableCaching: true,
		HMR:            &hmr.HMRConfig{Mux: mux},
		// Raise to logging.LEVEL_TRACE when a failure needs the build log. A
		// passing run should say nothing.
		LogLevel: logging.LEVEL_ERROR,
	})
	if err != nil {
		t.Fatalf("go_solid.New: %v", err)
	}
	t.Cleanup(func() { b.Close() })
	return b
}

var scriptSrc = regexp.MustCompile(`<script[^>]+src=["']([^"']+)["']`)

// withScripts returns the document together with every same-origin script it
// pulls in, so the assertions hold whether the bundle is inlined or served
// separately. Without this the test would keep passing if the bundle ever
// moved out of line, having scanned a document that no longer contains it.
func withScripts(t *testing.T, base, doc string) string {
	t.Helper()

	var b strings.Builder
	b.WriteString(doc)
	for _, m := range scriptSrc.FindAllStringSubmatch(doc, -1) {
		src := m[1]
		if !strings.HasPrefix(src, "/") {
			continue // off-origin, and not ours to vouch for
		}
		b.WriteString("\n")
		b.WriteString(get(t, base+src))
	}
	return b.String()
}

func get(t *testing.T, url string) string {
	t.Helper()

	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading %s: %v", url, err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: %s\n%s", url, resp.Status, body)
	}
	return string(body)
}
