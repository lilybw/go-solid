// Package compat adapts the router types consumers already have into the
// narrow interfaces go_solid needs.
//
// go_solid mounts handlers on a mux it does not own — the HMR socket, and in
// time the static asset endpoint. Rather than name a router library, it asks
// for the one method it uses and provides the adapters to get there from the
// shapes routers actually have.
package compat

import "net/http"

// MuxLike is anything that can register an http.Handler under a string pattern.
// *http.ServeMux satisfies it directly.
type MuxLike interface {
	Handle(pattern string, handler http.Handler)
}

// RouterLike is the same registration under the fluent signature that routers
// returning themselves for chaining use, such as *gorilla/mux.Router.
type RouterLike[T any] interface {
	Handle(pattern string, handler http.Handler) T
}

// MuxLikeFromRouterLike adapts a router whose Handle returns a value.
//
//	cfg.Mux = compat.MuxLikeFromRouterLike(router)
func MuxLikeFromRouterLike[T any](router RouterLike[T]) MuxLike {
	return MuxLikeFromFunc(func(pattern string, handler http.Handler) {
		router.Handle(pattern, handler)
	})
}

// MuxLikeFromFunc adapts a bare registration function, which is the escape
// hatch when a router matches neither shape.
//
//	cfg.Mux = compat.MuxLikeFromFunc(myRouter.Register)
func MuxLikeFromFunc[T func(pattern string, handler http.Handler)](fn T) MuxLike {
	return &functionToMethodWrapper[T]{fn: fn}
}

type functionToMethodWrapper[T func(pattern string, handler http.Handler)] struct{ fn T }

func (this *functionToMethodWrapper[T]) Handle(pattern string, handler http.Handler) {
	this.fn(pattern, handler)
}
