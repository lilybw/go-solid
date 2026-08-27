package hmr

import (
	"github.com/lilybw/go-solid/shared/compat"
)

type HMRConfig struct {
	// Disabled turns HMR off even when a config is supplied
	Disabled bool
	// Path is where go_solid mounts the WebSocket handler and where the
	// injected client connects. Defaults to "/__go_solid_hmr__".
	Path string
	// ServeMux, Router or the like to mount the WebSocket handler on. Required when HMR is enabled.
	//
	// Adapters are available for:
	// 	github.com/gorilla/mux.Router use compat.MuxLikeFromRouterLike
	// 	http.ServeMux use <self>
	Mux compat.MuxLike `json:"-"`
}

var NIL_HMR_CONFIG = &HMRConfig{ // null object
	Disabled: true,
	Path:     "/__go_solid_hmr__",
	Mux:      nil,
}
