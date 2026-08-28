package compat_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lilybw/go-solid/shared/compat"
)

// fluentRouter stands in for the routers whose Handle returns the receiver for
// chaining and matches a trailing slash as a subtree, as ServeMux does.
type fluentRouter struct {
	mux *http.ServeMux
}

func (this *fluentRouter) Handle(pattern string, handler http.Handler) *fluentRouter {
	this.mux.Handle(pattern, handler)
	return this
}

func (this *fluentRouter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	this.mux.ServeHTTP(w, r)
}

func serves(t *testing.T, mux *http.ServeMux, pattern string) bool {
	t.Helper()
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, pattern, nil))
	return rec.Code == http.StatusOK
}

func ok() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
}

// *http.ServeMux is the shape the interface was cut from, so it needs no adapter.
func TestServeMuxSatisfiesMuxLikeDirectly(t *testing.T) {
	mux := http.NewServeMux()
	var like compat.MuxLike = mux
	like.Handle("/direct", ok())

	if !serves(t, mux, "/direct") {
		t.Error("the handler was not registered")
	}
}

func TestMuxLikeFromRouterLike(t *testing.T) {
	mux := http.NewServeMux()
	compat.MuxLikeFromRouterLike[*fluentRouter](&fluentRouter{mux: mux}).Handle("/fluent", ok())

	if !serves(t, mux, "/fluent") {
		t.Error("the adapter did not reach the router")
	}
}

func TestMuxLikeFromFunc(t *testing.T) {
	mux := http.NewServeMux()
	compat.MuxLikeFromFunc(mux.Handle).Handle("/func", ok())

	if !serves(t, mux, "/func") {
		t.Error("the adapter did not reach the function")
	}
}

// Routers whose Handle is an exact match
// ---------------------------------------------------------------------------

// routeTable is the matching every stand-in below shares: first pattern that
// matches wins, and only a subtree route matches below itself.
type routeTable struct{ routes []*tableRoute }

type tableRoute struct {
	pattern string
	subtree bool
	handler http.Handler
}

func (this *routeTable) add(pattern string, subtree bool) *tableRoute {
	route := &tableRoute{pattern: pattern, subtree: subtree}
	this.routes = append(this.routes, route)
	return route
}

func (this *tableRoute) Handler(handler http.Handler) *tableRoute {
	this.handler = handler
	return this
}

func (this *routeTable) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	for _, route := range this.routes {
		if route.pattern == r.URL.Path ||
			(route.subtree && strings.HasPrefix(r.URL.Path, route.pattern)) {
			route.handler.ServeHTTP(w, r)
			return
		}
	}
	http.NotFound(w, r)
}

// prefixRouter stands in for gorilla/mux: Handle matches exactly, and a subtree
// is PathPrefix(p).Handler(h) over a route type the adapter cannot name.
type prefixRouter struct{ routeTable }

func (this *prefixRouter) Handle(pattern string, handler http.Handler) *tableRoute {
	return this.add(pattern, false).Handler(handler)
}
func (this *prefixRouter) Path(pattern string) *tableRoute      { return this.add(pattern, false) }
func (this *prefixRouter) PathPrefix(prefix string) *tableRoute { return this.add(prefix, true) }

// mountRouter stands in for chi: Handle satisfies MuxLike outright, so nothing
// wraps it, and a subtree is Mount.
type mountRouter struct{ routeTable }

func (this *mountRouter) Handle(pattern string, handler http.Handler) {
	this.add(pattern, false).Handler(handler)
}
func (this *mountRouter) Mount(pattern string, handler http.Handler) {
	this.add(pattern, true).Handler(handler)
}

// decoyRouter has a PathPrefix that means something else. Detection is by
// shape, so it must not be taken for a registration.
type decoyRouter struct{ mux *http.ServeMux }

func (this *decoyRouter) Handle(pattern string, handler http.Handler) {
	this.mux.Handle(pattern, handler)
}
func (this *decoyRouter) PathPrefix() string { return "/" }

func status(t *testing.T, handler http.Handler, path string) int {
	t.Helper()
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec.Code
}

func mounts(t *testing.T, mux compat.MuxLike, router http.Handler) {
	t.Helper()
	mux.Handle("/assets/", ok())
	mux.Handle("/socket", ok())

	// A trailing slash is a subtree, which is the whole of what the asset
	// endpoint needs and the whole of what an exact-matching Handle loses.
	for _, path := range []string{"/assets/", "/assets/img/logo.9f2a1c4b.svg"} {
		if got := status(t, router, path); got != http.StatusOK {
			t.Errorf("%s returned %d, want 200", path, got)
		}
	}
	// Anything else still matches exactly, so the socket does not swallow
	// everything sharing its first characters.
	if got := status(t, router, "/socket"); got != http.StatusOK {
		t.Errorf("/socket returned %d, want 200", got)
	}
	if got := status(t, router, "/socket/nested"); got != http.StatusNotFound {
		t.Errorf("/socket/nested returned %d, want 404", got)
	}
}

func TestPathPrefixRoutersAreDetectedThroughTheAdapter(t *testing.T) {
	router := &prefixRouter{}
	mux := compat.MuxLikeFromRouterLike[*tableRoute](router)

	if got := mux.(compat.Described).Strategy(); got != compat.STRATEGY_PATH_PREFIX {
		t.Fatalf("strategy = %q, want %q", got, compat.STRATEGY_PATH_PREFIX)
	}
	mounts(t, mux, router)
}

// A router satisfying MuxLike outright is never handed to an adapter, so the
// detection has to be reachable from the mux go_solid was given.
func TestNormalizeDetectsMountRouters(t *testing.T) {
	router := &mountRouter{}
	mux := compat.Normalize(router)

	if got := mux.(compat.Described).Strategy(); got != compat.STRATEGY_MOUNT {
		t.Fatalf("strategy = %q, want %q", got, compat.STRATEGY_MOUNT)
	}
	mounts(t, mux, router)
}

func TestNormalizeLeavesServeMuxAlone(t *testing.T) {
	mux := http.NewServeMux()
	normalized := compat.Normalize(mux)

	if got := normalized.(compat.Described).Strategy(); got != compat.STRATEGY_HANDLE {
		t.Fatalf("strategy = %q, want %q", got, compat.STRATEGY_HANDLE)
	}
	mounts(t, normalized, mux)
}

// Detection is on the shape, not the name.
func TestDetectionIgnoresAMisshapenPathPrefix(t *testing.T) {
	router := &decoyRouter{mux: http.NewServeMux()}
	mux := compat.Normalize(router)

	if got := mux.(compat.Described).Strategy(); got != compat.STRATEGY_HANDLE {
		t.Errorf("strategy = %q, want %q", got, compat.STRATEGY_HANDLE)
	}
	mounts(t, mux, router.mux)
}

// The escape hatch is the consumer's to get right, so nothing is detected
// behind their back.
func TestMuxLikeFromFuncDetectsNothing(t *testing.T) {
	router := &prefixRouter{}
	mux := compat.MuxLikeFromFunc(func(pattern string, handler http.Handler) {
		router.PathPrefix(pattern).Handler(handler)
	})

	if got := mux.(compat.Described).Strategy(); got != compat.STRATEGY_FUNC {
		t.Errorf("strategy = %q, want %q", got, compat.STRATEGY_FUNC)
	}
}

// The adapter stands between go_solid and the router, so the router has to
// remain reachable through it for the mount to be checkable.
func TestAdaptersExposeTheRouterTheyRegisterOn(t *testing.T) {
	for name, mux := range map[string]compat.MuxLike{
		"prefix":  compat.MuxLikeFromRouterLike[*tableRoute](&prefixRouter{}),
		"fluent":  compat.MuxLikeFromRouterLike[*fluentRouter](&fluentRouter{mux: http.NewServeMux()}),
		"mounted": compat.Normalize(&mountRouter{}),
	} {
		servable, ok := mux.(compat.Servable)
		if !ok || servable.Servable() == nil {
			t.Errorf("the %s adapter hides its router", name)
		}
	}
}
