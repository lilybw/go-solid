package shared

import "net/http"

type HMRConfig struct {
	// Disabled turns HMR off even when a config is supplied
	Disabled bool
	// HMRPath is where go_solid mounts the WebSocket handler and where the
	// injected client connects. Defaults to "/__go_solid_hmr__".
	HMRPath string
	// ServeMux, Router or the like to mount the WebSocket handler on. Required when HMR is enabled.
	Mux MuxLike
}

// MuxLike is anything that can register an http.Handler under a string pattern
type MuxLike interface {
	Handle(pattern string, handler http.Handler)
}
