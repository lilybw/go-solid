package compat_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lilybw/go-solid/shared/compat"
)

// fluentRouter stands in for the routers whose Handle returns the receiver for
// chaining, such as gorilla/mux.
type fluentRouter struct {
	mux *http.ServeMux
}

func (this *fluentRouter) Handle(pattern string, handler http.Handler) *fluentRouter {
	this.mux.Handle(pattern, handler)
	return this
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
