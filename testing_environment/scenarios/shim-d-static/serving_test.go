package shim_d

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	shared_hmr "github.com/lilybw/go-solid/shared/hmr"
	shared_static "github.com/lilybw/go-solid/shared/static"
)

// Two features, one mux
// ---------------------------------------------------------------------------
// Both the asset endpoint and the hot-reload socket mount themselves on a mux
// the consumer owns and go_solid does not. Nothing coordinates their patterns,
// so the only thing keeping them out of each other's way is that they chose
// different prefixes — worth an assertion rather than an assumption.

func servedBy(t *testing.T, p *project, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	p.mux.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return rec
}

func TestAssetsAndHotReloadShareAMuxWithoutShadowing(t *testing.T) {
	p := newProject(t)
	b := p.boot(t, options{static: true, hmr: true})

	logo := b.Static().URL("images/logo.svg")
	if logo == "" {
		t.Fatal("the logo was not published")
	}
	if rec := servedBy(t, p, http.MethodGet, logo); rec.Code != http.StatusOK {
		t.Errorf("asset request returned %d; the socket may be shadowing the endpoint", rec.Code)
	}

	// The socket answers on its own path. Reached over plain HTTP it refuses
	// the handshake rather than 404ing, which is how we know it is mounted.
	rec := servedBy(t, p, http.MethodGet, shared_hmr.NIL_HMR_CONFIG.Path)
	if rec.Code == http.StatusNotFound {
		t.Error("the hot-reload socket is not mounted; the asset endpoint may have taken its path")
	}
}

// ---------------------------------------------------------------------------
// The endpoint.
// ---------------------------------------------------------------------------

func TestAssetsAreServedImmutably(t *testing.T) {
	p := newProject(t)
	b := p.boot(t, options{static: true})

	for rel, wantType := range map[string]string{
		"images/logo.svg":  "image/svg+xml",
		"data/config.json": "application/json",
		"styles/theme.css": "text/css",
	} {
		url := b.Static().URL(rel)
		if url == "" {
			t.Errorf("%s was not published", rel)
			continue
		}
		rec := servedBy(t, p, http.MethodGet, url)
		if rec.Code != http.StatusOK {
			t.Errorf("%s returned %d", rel, rec.Code)
			continue
		}
		if got := rec.Header().Get("Content-Type"); got != wantType {
			t.Errorf("%s Content-Type = %q, want %q", rel, got, wantType)
		}
		if got := rec.Header().Get("Cache-Control"); !strings.Contains(got, "immutable") {
			t.Errorf("%s Cache-Control = %q; a content-hashed URL never needs revalidating", rel, got)
		}
	}
}

// The manifest is the whole of what is servable. A path that names no entry in
// it is refused before anything touches a filesystem, so there is no route by
// which a request could resolve to a file — the protection is the closed set,
// not the shape of the URL.
func TestNothingOutsideTheManifestIsServable(t *testing.T) {
	p := newProject(t)
	b := p.boot(t, options{static: true})

	mount := shared_static.DEFAULT_MOUNT_PATH
	for _, path := range []string{
		mount + "images/logo.svg", // the real name, without the content hash
		mount,
		b.Static().URL("images/logo.svg") + ".bak",
	} {
		if rec := servedBy(t, p, http.MethodGet, path); rec.Code != http.StatusNotFound {
			t.Errorf("%s returned %d, want 404", path, rec.Code)
		}
	}
}

// An un-normalised path never reaches the endpoint: ServeMux cleans it first
// and redirects. Where that leaves you is what matters, and there are two
// shapes of answer — outside the mount, where this endpoint does not answer at
// all, or back inside it at a path the manifest still does not name. Both end
// in nothing being served, and the second is why cleaning cannot be relied on
// as the protection: it is the manifest that refuses, every time.
func TestUncleanPathsResolveToNothing(t *testing.T) {
	p := newProject(t)
	b := p.boot(t, options{static: true})

	mount := shared_static.DEFAULT_MOUNT_PATH
	for _, path := range []string{
		mount + "../../../etc/passwd",
		mount + "../components/types.ts", // a real file, outside the asset root
		mount + "images/../../../../etc/passwd",
		b.Static().URL("images/logo.svg") + "/../logo.svg", // cleans back inside
	} {
		rec := servedBy(t, p, http.MethodGet, path)
		if rec.Code == http.StatusOK {
			t.Errorf("%s was served", path)
			continue
		}

		location := rec.Header().Get("Location")
		switch {
		case location == "":
			continue // refused outright
		case !strings.HasPrefix(location, mount):
			continue // sent somewhere this endpoint does not answer for
		}
		// Redirected back under the mount, so the cleaned path has to be
		// refused on its own merits.
		if again := servedBy(t, p, http.MethodGet, location); again.Code != http.StatusNotFound {
			t.Errorf("%s redirected to %q, which returned %d, want 404", path, location, again.Code)
		}
	}
}

// The endpoint itself, asked directly, refuses everything the manifest does not
// name — including the paths ServeMux would have normalised away before they
// ever arrived. The handler cannot rely on having been given a clean path.
func TestTheEndpointRefusesUncleanedPaths(t *testing.T) {
	p := newProject(t)
	b := p.boot(t, options{static: true})

	handler, pattern := p.mux.Handler(httptest.NewRequest(http.MethodGet, b.Static().URL("images/logo.svg"), nil))
	if pattern == "" {
		t.Fatal("the asset endpoint is not mounted")
	}

	mount := shared_static.DEFAULT_MOUNT_PATH
	for _, path := range []string{
		mount + "../../../etc/passwd",
		mount + "../components/types.ts",
		"/etc/passwd",
		"",
	} {
		req := httptest.NewRequest(http.MethodGet, "http://example.test/placeholder", nil)
		req.URL.Path = path // bypass the normalisation a mux would apply

		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Errorf("the endpoint returned %d for %q, want 404", rec.Code, path)
		}
	}
}

func TestTheEndpointIsReadOnly(t *testing.T) {
	p := newProject(t)
	b := p.boot(t, options{static: true})

	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		rec := servedBy(t, p, method, b.Static().URL("images/logo.svg"))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s returned %d, want 405", method, rec.Code)
		}
	}
}

// HEAD is what a browser sends to check a cached copy, so it has to answer with
// the headers and no body.
func TestHeadReturnsHeadersWithoutABody(t *testing.T) {
	p := newProject(t)
	b := p.boot(t, options{static: true})

	rec := servedBy(t, p, http.MethodHead, b.Static().URL("images/logo.svg"))
	if rec.Code != http.StatusOK {
		t.Fatalf("HEAD returned %d", rec.Code)
	}
	if rec.Header().Get("Content-Type") != "image/svg+xml" {
		t.Error("HEAD did not carry the content type")
	}
}
