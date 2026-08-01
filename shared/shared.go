package shared

import (
	"net/http"
)

type HMRConfig struct {
	// Disabled turns HMR off even when a config is supplied
	Disabled bool
	// HMRPath is where go_solid mounts the WebSocket handler and where the
	// injected client connects. Defaults to "/__go_solid_hmr__".
	HMRPath string
	// ServeMux, Router or the like to mount the WebSocket handler on. Required when HMR is enabled.
	//
	// Adapters are available for:
	// 	github.com/gorilla/mux.Router use MuxLikeFromRouterLike
	// 	http.ServeMux use <self>
	Mux MuxLike
}

type VoidBiFunc[T any, U any] interface {
	Call(t T, u U)
}

// MuxLike is anything that can register an http.Handler under a string pattern
type MuxLike interface {
	Handle(pattern string, handler http.Handler)
}

type RouterLike[T any] interface {
	Handle(pattern string, handler http.Handler) T
}

// Sometimes I get a tired of go's type system...
type functionToMethodWrapper[T func(pattern string, handler http.Handler)] struct{ fn T }

func (i *functionToMethodWrapper[T]) Handle(pattern string, handler http.Handler) {
	i.fn(pattern, handler)
}

func MuxLikeFromRouterLike[T any](router RouterLike[T]) MuxLike {
	return MuxLikeFromFunc(func(pattern string, handler http.Handler) {
		router.Handle(pattern, handler)
	})
}

func MuxLikeFromFunc[T func(pattern string, handler http.Handler)](fn T) MuxLike {
	return &functionToMethodWrapper[T]{fn: fn}
}
