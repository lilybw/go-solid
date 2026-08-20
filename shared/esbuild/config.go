package esbuild

import (
	esbuild "github.com/evanw/esbuild/pkg/api"
	"github.com/lilybw/go-solid/internal/meta"
)

type BundlerConfig struct {
	// Sourcemap emits inline sourcemaps from esbuild for easier debugging in
	// the browser. Independent of caching, so you can debug a cached prod build.
	Sourcemap esbuild.SourceMap

	Minify bool

	// The absolute path to the directory containing the node_modules folder
	// that resolves solid-js. Defaults to the same value as Components if not
	// specified.
	Dependencies meta.AbsoluteDirectoryPath

	// Disables all bundling and transpilation. Automatically set to true if
	// RasterizationConfig#ExpectCompleted is true.
	Disabled bool
}

var NIL_BUNDLER_CONFIG = &BundlerConfig{ // null object
	Minify:       true,
	Sourcemap:    esbuild.SourceMapNone,
	Dependencies: "",
	Disabled:     false,
}
