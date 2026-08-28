package compat

import (
	"net/http"
	"reflect"
	"strings"
)

// MuxLike is anything that can register an http.Handler under a string pattern,
// with *http.ServeMux semantics: a pattern ending in "/" matches the subtree
// below it, anything else matches that path exactly. go_solid mounts the asset
// endpoint as a subtree, so a router that does not honour that serves 404s.
type MuxLike interface {
	Handle(pattern string, handler http.Handler)
}

// RouterLike is the same registration under the fluent signature that routers
// returning themselves for chaining use, such as *gorilla/mux.Router.
type RouterLike[T any] interface {
	Handle(pattern string, handler http.Handler) T
}

// MountRouterLike registers a subtree under its own method, as chi does.
type MountRouterLike interface {
	Mount(pattern string, handler http.Handler)
}

// Strategy is how an adapter registers a subtree.
type Strategy = string

const (
	// STRATEGY_HANDLE is Handle alone, which is only a subtree on a router with
	// ServeMux's trailing-slash rule.
	STRATEGY_HANDLE Strategy = "Handle"
	// STRATEGY_PATH_PREFIX is PathPrefix(p).Handler(h), the gorilla/mux shape.
	STRATEGY_PATH_PREFIX Strategy = "PathPrefix"
	// STRATEGY_MOUNT is Mount(p, h), the chi shape.
	STRATEGY_MOUNT Strategy = "Mount"
	// STRATEGY_FUNC is a registration function, whose semantics are the
	// caller's to get right.
	STRATEGY_FUNC Strategy = "func"
)

// Described reports how an adapter registers, so a failed mount can say what
// was tried rather than what might have been.
type Described interface {
	Strategy() Strategy
}

// Servable exposes the router an adapter registers on, when that router can
// also answer requests. go_solid uses it to check a mount it just registered.
type Servable interface {
	Servable() http.Handler
}

// Normalize upgrades a mux to subtree-correct registration when its concrete
// type offers it, and is what go_solid applies to a supplied Mux.
//
//	cfg.Mux = compat.Normalize(chiRouter) // registers subtrees with Mount
func Normalize(mux MuxLike) MuxLike {
	if mux == nil {
		return nil
	}
	if _, resolved := mux.(*adapter); resolved {
		return mux
	}
	return adapt(mux, mux.Handle)
}

// MuxLikeFromRouterLike adapts a router whose Handle returns a value.
//
//	cfg.Mux = compat.MuxLikeFromRouterLike(router) // *mux.Router
func MuxLikeFromRouterLike[T any](router RouterLike[T]) MuxLike {
	return adapt(router, func(pattern string, handler http.Handler) {
		router.Handle(pattern, handler)
	})
}

// MuxLikeFromFunc adapts a bare registration function, which is the escape
// hatch when a router matches no known shape. Nothing is detected: the function
// is used for exact patterns and subtrees alike.
//
//	cfg.Mux = compat.MuxLikeFromFunc(func(p string, h http.Handler) {
//		router.PathPrefix(p).Handler(h)
//	})
func MuxLikeFromFunc[T func(pattern string, handler http.Handler)](fn T) MuxLike {
	register := (func(string, http.Handler))(fn)
	return &adapter{exact: register, subtree: register, strategy: STRATEGY_FUNC}
}

// adapt resolves how the router registers a subtree, falling back to the
// registration it was given.
func adapt(router any, register func(string, http.Handler)) MuxLike {
	resolved := &adapter{exact: register, subtree: register, strategy: STRATEGY_HANDLE}
	if handler, servable := router.(http.Handler); servable {
		resolved.serve = handler
	}
	if mount, mountable := router.(MountRouterLike); mountable {
		resolved.subtree, resolved.strategy = mount.Mount, STRATEGY_MOUNT
	}
	// Last, so it wins: on a router offering both, PathPrefix is the plain
	// prefix match and Mount is the one that brings a subrouter with it.
	if prefix, prefixable := pathPrefixRegistration(router); prefixable {
		resolved.subtree, resolved.strategy = prefix, STRATEGY_PATH_PREFIX
	}
	return resolved
}

type adapter struct {
	exact    func(string, http.Handler)
	subtree  func(string, http.Handler)
	strategy Strategy
	serve    http.Handler
}

func (this *adapter) Handle(pattern string, handler http.Handler) {
	if strings.HasSuffix(pattern, "/") {
		this.subtree(pattern, handler)
		return
	}
	this.exact(pattern, handler)
}

func (this *adapter) Strategy() Strategy     { return this.strategy }
func (this *adapter) Servable() http.Handler { return this.serve }

var (
	stringType  = reflect.TypeFor[string]()
	handlerType = reflect.TypeFor[http.Handler]()
)

// pathPrefixRegistration resolves router.PathPrefix(p).Handler(h).
//
// The route it returns is the router package's own type and cannot be named in
// an interface here, so the shape is checked instead: one string in, one value
// out, and on that value a Handler method taking something an http.Handler
// satisfies. A method named PathPrefix that is not that is left alone.
func pathPrefixRegistration(router any) (func(string, http.Handler), bool) {
	value := reflect.ValueOf(router)
	if !value.IsValid() {
		return nil, false
	}

	prefix := value.MethodByName("PathPrefix")
	if !prefix.IsValid() {
		return nil, false
	}
	signature := prefix.Type()
	if signature.IsVariadic() ||
		signature.NumIn() != 1 || signature.In(0) != stringType ||
		signature.NumOut() != 1 {
		return nil, false
	}

	// In(0) of a method read off a type is the receiver, so the parameter is
	// In(1).
	attach, attachable := signature.Out(0).MethodByName("Handler")
	if !attachable || attach.Type.IsVariadic() ||
		attach.Type.NumIn() != 2 || !handlerType.AssignableTo(attach.Type.In(1)) {
		return nil, false
	}

	return func(pattern string, handler http.Handler) {
		route := prefix.Call([]reflect.Value{reflect.ValueOf(pattern)})[0]
		route.MethodByName("Handler").Call([]reflect.Value{reflect.ValueOf(handler)})
	}, true
}
