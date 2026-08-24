package esbuild

import (
	"fmt"

	esbuild "github.com/evanw/esbuild/pkg/api"
	"github.com/lilybw/go-solid/internal/meta"
)

// RuntimeSource selects where the solid-js browser runtime comes from.
type RuntimeSource uint8

const (
	// RuntimeInternal serves solid-js from the copy embedded in
	// go-solid-compiler. No node_modules directory is required.
	RuntimeInternal RuntimeSource = iota

	// RuntimeExternal resolves solid-js from disk, from
	// BundlerConfig#Dependencies.
	RuntimeExternal
)

func (r RuntimeSource) String() string {
	switch r {
	case RuntimeInternal:
		return "INTERNAL"
	case RuntimeExternal:
		return "EXTERNAL"
	default:
		return fmt.Sprintf("RuntimeSource(%d)", uint8(r))
	}
}

// SolidConfig holds the settings that belong to Solid rather than to bundling.
type SolidConfig struct {
	// Runtime selects where solid-js comes from. Defaults to RuntimeInternal.
	Runtime RuntimeSource

	// Development selects Solid's development builds, which carry its runtime
	// warnings and ownership tracking. Independent of Minify
	Development bool

	// ModuleName is the import source for the generated runtime helpers.
	// Defaults to "solid-js/web".
	ModuleName string

	// HelperPrefix is prepended to generated helper identifiers. Defaults to
	// "_$", matching Solid's own output; changing it only affects readability
	// of the emitted code.
	HelperPrefix string

	// DisableEventDelegation attaches every event listener to its own element
	// instead of routing supported events through one document-level listener.
	//
	// Delegation is Solid's default and is usually what you want.
	DisableEventDelegation bool

	// RuntimeOverride replaces individual solid-js modules by import
	// specifier, for example {"solid-js/store": "..."}. It applies to
	// RuntimeInternal and is the finer-grained alternative to switching the
	// whole runtime to RuntimeExternal.
	RuntimeOverride map[string]string
}

// Normalize fills in defaults. It is safe to call more than once.
func (s *SolidConfig) Normalize() {
	if s.ModuleName == "" {
		s.ModuleName = "solid-js/web"
	}
	if s.HelperPrefix == "" {
		s.HelperPrefix = "_$"
	}
}

// Validate reports configurations that cannot work, as opposed to ones that
// merely differ from the default.
func (s SolidConfig) Validate(dependencies meta.AbsoluteDirectoryPath) error {
	switch s.Runtime {
	case RuntimeInternal, RuntimeExternal:
	default:
		return fmt.Errorf("Solid.Runtime: unknown value %d", uint8(s.Runtime))
	}
	if s.Runtime == RuntimeExternal && dependencies == "" {
		return fmt.Errorf(
			"Solid.Runtime is EXTERNAL but Dependencies is empty; " +
				"set Dependencies to a directory from which solid-js resolves, " +
				"or use RuntimeInternal to serve the embedded copy")
	}
	if len(s.RuntimeOverride) > 0 && s.Runtime == RuntimeExternal {
		return fmt.Errorf(
			"Solid.RuntimeOverride applies to the embedded runtime, " +
				"but Solid.Runtime is EXTERNAL; the overrides would be ignored")
	}
	return nil
}

type BundlerConfig struct {
	// Alias maps a bare module specifier to the file that satisfies it. This is
	// how a generated module reaches a component under a stable name —
	// "go-solid/static" resolves here, so the workspace path never appears in
	// user code. Set by go_solid; entries a consumer adds are kept.
	Alias map[string]string

	// Solid holds the settings that belong to Solid itself rather than to
	// bundling. Its zero value is valid.
	Solid SolidConfig

	// Sourcemap emits inline sourcemaps from esbuild for easier debugging in
	// the browser. Independent of caching, so you can debug a cached prod build.
	Sourcemap esbuild.SourceMap

	Minify bool

	// The absolute path to the directory containing the node_modules folder
	// that resolves solid-js. Required when Solid.Runtime is EXTERNAL, and
	// used as esbuild's working directory in all cases. Defaults to the same
	// value as Components if not specified.
	Dependencies meta.AbsoluteDirectoryPath

	// Disables all bundling and transpilation. Automatically set to true if
	// RasterizationConfig#ExpectCompleted is true.
	Disabled bool
}

var NIL_BUNDLER_CONFIG = &BundlerConfig{ // null object
	Solid: SolidConfig{
		Runtime:      RuntimeInternal,
		Development:  false,
		ModuleName:   "solid-js/web",
		HelperPrefix: "_$",
	},
	Minify:       true,
	Sourcemap:    esbuild.SourceMapNone,
	Dependencies: "",
	Disabled:     false,
}
