package go_solid

import (
	"fmt"

	"github.com/lilybw/go-solid/internal"
	caching "github.com/lilybw/go-solid/internal/caching"
	log_int "github.com/lilybw/go-solid/internal/logging"
	networking_int "github.com/lilybw/go-solid/internal/networking"
	"github.com/lilybw/go-solid/internal/sources"
	ssr_int "github.com/lilybw/go-solid/internal/ssr"
	static_int "github.com/lilybw/go-solid/internal/static"
	types_int "github.com/lilybw/go-solid/internal/types"
	"github.com/lilybw/go-solid/shared/esbuild"
	"github.com/lilybw/go-solid/shared/hmr"
	logging "github.com/lilybw/go-solid/shared/logging"
	"github.com/lilybw/go-solid/shared/meta"
	"github.com/lilybw/go-solid/shared/rasterization"
	"github.com/lilybw/go-solid/shared/ssr"
	"github.com/lilybw/go-solid/shared/static"
	"github.com/lilybw/go-solid/shared/types"
)

// EphemeralConfig configures a bundler with nothing behind it. Every field is
// optional: the zero value is a working bundler.
//
// The fields a Config spends on locations are absent rather than ignored, since
// an ephemeral bundler has nowhere to put anything and nowhere to look.
type EphemeralConfig struct {
	LogLevel logging.LogLevel

	// Generation carries the Solid transform and minification settings.
	// Dependencies and Alias are not read: nothing resolves from disk, so
	// Solid.Runtime must stay INTERNAL.
	Generation *esbuild.BundlerConfig

	Types    *types.TypesConfig
	SSR      *ssr.SSRConfig
	Defaults *BehaviouralDefaults
}

// NewEphemeral builds a bundler that reads and writes no files. It has no
// components directory and no workspace, so Anonymous is the only way to give
// it something to render:
//
//	bundler, err := go_solid.NewEphemeral(nil)
//	rendered, err := bundler.Anonymous(`(props) => <p>{props.msg}</p>`, msg).Render()
//
// Everything else behaves as it does on a bundler built by New. Nothing is
// cached across processes, nothing is watched, and there is nothing to clean up.
func NewEphemeral(cfg *EphemeralConfig) (*Bundler, error) {
	if cfg == nil {
		cfg = &EphemeralConfig{}
	}
	normalized, err := cfg.normalize()
	if err != nil {
		return nil, err
	}

	defaults := networking_int.NewDefaults()
	defaults.SetHTMLHeadSegment(normalized.Defaults.HeadSegment)
	defaults.SetRequestBehaviour(normalized.Defaults.Requests)

	// The one reader, with nothing under it: a path this does not hold is a
	// path this bundler cannot render.
	held := sources.NewMemory()

	// Disabled rather than absent: every call on these is already a no-op when
	// they are switched off, so nothing downstream has to ask which bundler it
	// is running in.
	disk, err := caching.NewDiskCache("", false)
	if err != nil {
		return nil, err
	}
	inert, err := static_int.NewStaticRegistry(normalized.Static, "", nil, nil)
	if err != nil {
		return nil, err
	}

	return &Bundler{
		cfg:      normalized,
		buildID:  buildFingerprint(normalized.Generation),
		registry: internal.NewRootlessRegistry(),
		mem:      caching.NewMemCache(true),
		disk:     disk,
		index:    internal.NewDepIndex(),
		static:   inert,
		defaults: defaults,
		types:    types_int.NewChecker("", normalized.Types.Check, nil, held),
		ssr:      ssr_int.NewRenderer(normalized.SSR, held),
		sources:  held,
		held:     held,
	}, nil
}

// normalize resolves the ephemeral settings into the Config the rest of the
// bundler is written against, with every feature that needs a location switched
// off rather than left to discover it has none.
func (cfg *EphemeralConfig) normalize() (*Config, error) {
	level := meta.Or(cfg.LogLevel, logging.DEFAULT_LEVEL)
	log_int.SetLevel(level)

	generation := meta.Copy(esbuild.NIL_BUNDLER_CONFIG)
	if cfg.Generation != nil {
		generation = meta.Copy(cfg.Generation)
	}
	generation.Solid.Normalize()
	if generation.Solid.Runtime != esbuild.RuntimeInternal {
		return nil, fmt.Errorf(
			"go_solid: Solid.Runtime is %s, but an ephemeral bundler has no directory to "+
				"resolve solid-js from; leave it INTERNAL to serve the embedded copy",
			generation.Solid.Runtime)
	}
	if err := generation.Solid.Validate(""); err != nil {
		return nil, err
	}
	generation.Dependencies = ""
	generation.Alias = map[string]string{}

	checks := meta.Copy(types.NIL_TYPES_CONFIG)
	if cfg.Types != nil {
		checks = meta.Copy(cfg.Types)
	}
	if checks.Check == types.CHECK_UNSET {
		checks.Check = types.DEFAULT_CHECK
	}

	rendering := meta.Copy(ssr.NIL_SSR_CONFIG)
	if cfg.SSR != nil {
		rendering = meta.Copy(cfg.SSR)
	}

	behaviour := meta.Copy(NIL_BEHAVIOURAL_DEFAULTS)
	if cfg.Defaults != nil {
		if cfg.Defaults.HeadSegment != nil {
			behaviour.HeadSegment = cfg.Defaults.HeadSegment
		}
		if cfg.Defaults.Requests != nil {
			behaviour.Requests = cfg.Defaults.Requests
		}
	}

	normalized := &Config{
		LogLevel:      level,
		Generation:    generation,
		Types:         checks,
		SSR:           rendering,
		Defaults:      behaviour,
		Static:        meta.Copy(static.NIL_STATIC_CONFIG),
		HMR:           meta.Copy(hmr.NIL_HMR_CONFIG),
		Rasterization: &rasterization.RasterizationConfig{Disabled: true},
	}
	log_int.LogJSON(logging.LEVEL_DEBUG, "normalized ephemeral configuration: ", normalized)
	return normalized, nil
}
