package esbuild

import (
	"time"

	esbuild "github.com/evanw/esbuild/pkg/api"
	"github.com/lilybw/go-solid/internal/meta"
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
	// absolute path to transform-worker.mjs, materialized by go-solid.
	// Can be set, but will resolve itself by default.
	ScriptLocation meta.AbsoluteFilePath
	// The absolute path to the directory containing the node_modules folder with the node dependencies.
	// Defaults to the same value as Components if not specified.
	Dependencies meta.AbsoluteDirectoryPath // cwd for workers (must resolve babel-preset-solid)
	// per-transform timeout; 0 means 30s
	Timeout time.Duration
	// disables all bundling and transpilation, so the transform workers are never spawned.
	// Automatically set to true if RasterizationConfig#ExpectCompleted is true
	Disabled bool
}

var NIL_BUNDLER_CONFIG = &BundlerConfig{ // null object
	Minify:         true,
	Sourcemap:      esbuild.SourceMapNone,
	PoolSize:       1,
	NodeBin:        "node",
	ScriptLocation: "",
	Dependencies:   "",
	Timeout:        30 * time.Second,
	Disabled:       false,
}
