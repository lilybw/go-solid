package esbuild

import (
	esbuild "github.com/evanw/esbuild/pkg/api"
)

type BundlerConfig struct {
	// Sourcemap emits inline sourcemaps from esbuild for easier debugging in
	// the browser. Independent of caching, so you can debug a cached prod build.
	// Previously implied by Dev.
	Sourcemap esbuild.SourceMap
	// Node worker pool size. Default 1
	PoolSize int
	// Node binary path. Default "node" (must be in PATH)
	NodeBin string

	Minify bool
}

var NIL_BUNDLER_CONFIG = &BundlerConfig{ // null object
	Minify:    true,
	Sourcemap: esbuild.SourceMapNone,
	PoolSize:  1,
	NodeBin:   "node",
}
