package go_solid

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/lilybw/go-solid/internal"
	caching "github.com/lilybw/go-solid/internal/caching"
	esbuild_int "github.com/lilybw/go-solid/internal/esbuild"
	hmr_int "github.com/lilybw/go-solid/internal/hmr"
	"github.com/lilybw/go-solid/internal/meta"
	networking_int "github.com/lilybw/go-solid/internal/networking"
	"github.com/lilybw/go-solid/internal/noop"
	rasterization_int "github.com/lilybw/go-solid/internal/rasterization"
	static_int "github.com/lilybw/go-solid/internal/static"
	"github.com/lilybw/go-solid/internal/workers"
	"github.com/lilybw/go-solid/shared/esbuild"
	"github.com/lilybw/go-solid/shared/hmr"
	networking "github.com/lilybw/go-solid/shared/networking"
	"github.com/lilybw/go-solid/shared/rasterization"
	"github.com/lilybw/go-solid/shared/static"
)

type Config struct {
	// The absolute path to the directory containing the solidjs components.
	// The registry will scan this and all subdirectories, skipping directories prefixed with a dot (.)
	// or with the exact name "node_modules"
	Components meta.AbsoluteDirectoryPath
	// The absolute path to the directory containing the node_modules folder with the node dependencies.
	// Defaults to the same value as Components if not specified.
	//
	// This field is a shorthand and overwritten by Config#Generation#Dependencies if present
	Dependencies meta.AbsoluteDirectoryPath // easy access field, single source of truth is the BundlerConfig
	// The absolute path to the directory where go_solid will place its .go_solid workspace directory, which contains the worker script and the disk cache.
	// Defaults to Components if not specified.
	Workspace meta.AbsoluteDirectoryPath

	// DisableCaching bypasses both the in-memory and on-disk caches, so every
	// render rebuilds from source.
	DisableCaching bool

	Generation *esbuild.BundlerConfig

	// Enable a filewatcher that watches the component dir to trigger registry updates when new component files are added.
	// This enables usecases which may attempt to ask for procedural component names, since the registry is constant otherwise.
	ReactiveRegistry bool

	// !! NOT IMPLEMENTED !! If you provide this config, bundle and cache all components in the registry on next boot (may take a moment).
	// This disables all js activity, the node workers, and esbuild, and thus means that Node no longer is required to run your application.
	//
	// Do be aware that this disables HMR, ReactiveRegistry and DisableCaching (caches are now mandatory).
	Rasterization *rasterization.RasterizationConfig

	// !! NOT IMPLEMENTED !! Enable component-integrated static content serving. If provided, any component's props (if any) will gain a "static" property of a type
	// that is a 1 to 1 recreation of the structure of the Static.Location directory. This places some limitations upon names of files and sub-directories.
	//
	// In the resulting graph-like js object at props.static, each file becomes a function that returns a corresponding Promise. I.e. font at:
	//
	// <StaticConfig.Location>/svg/homeIcon.svg
	//
	// becomes accessible in a component as:
	//
	// props.static.svg.homeIcon()
	Static *static.StaticConfig

	// HMR enables hot browser reload in development. When non-nil and not
	// Disabled, go_solid watches the components tree and pushes reloads to the
	// tabs viewing an affected template. Requires HMR.Mux so go_solid can mount
	// its WebSocket handler itself.
	HMR      *hmr.HMRConfig
	Defaults *BehaviouralDefaults
}

type BehaviouralDefaults struct {
	// Define the default elements of the <head> tag to be included in every page.
	// These defaults can be modified upon any Bundler#Prepare call by using the method: WithHTMLHeadTags
	HeadSegment meta.Configurator[networking.HTMLHeadSegmentBuilder]
	// Define the default behaviour of the http request handling. These defaults can be modified upon any Bundler#Prepare call by using the method: SetHTTPBehaviour
	Requests meta.Configurator[networking.RequestBehaviourBuilder]
}

var NIL_BEHAVIOURAL_DEFAULTS = &BehaviouralDefaults{ // null object
	HeadSegment: noop.T_o_Void[networking.HTMLHeadSegmentBuilder](),
	Requests:    noop.T_o_Void[networking.RequestBehaviourBuilder](),
}

type Bundler struct {
	cfg      *Config
	registry *internal.ComponentRegistry
	pool     *workers.Pool
	mem      *caching.MemCache
	disk     *caching.DiskCache
	index    *internal.DependencyIndex
	static   *static_int.StaticRegistry
	hub      *hmr_int.Hub
	watcher  *hmr_int.Watcher
}

func New(cfg *Config) (*Bundler, error) {
	if err := configValidationAndNormalization(cfg); err != nil {
		return nil, err
	}

	if cfg.Defaults != NIL_BEHAVIOURAL_DEFAULTS {
		networking_int.SetHTMLHeadSegmentTemplate(cfg.Defaults.HeadSegment)
		networking_int.SetRequestBehaviourTemplate(cfg.Defaults.Requests)
	}

	if missing := esbuild_int.PeerDepsMissing(cfg.Dependencies, esbuild_int.RequiredPeerDeps); len(missing) > 0 {
		return nil, fmt.Errorf(
			"go_solid: missing Node peer dependencies %v in %q (or any ancestor).\n"+
				"Install them in your frontend project:\n"+
				"    npm install --save-dev %s",
			missing, cfg.Dependencies, strings.Join(missing, " "))
	}

	registry, err := internal.NewRegistry(cfg.Components)
	if err != nil {
		return nil, err
	}

	pool, err := workers.NewPool(cfg.Generation)
	if err != nil {
		return nil, err
	}

	// Caches are enabled unless explicitly disabled.
	disk, err := caching.NewDiskCache(cfg.Workspace, !cfg.DisableCaching)
	if err != nil {
		// Don't leak the pool if disk cache setup fails.
		pool.Close()
		return nil, err
	}

	mem := caching.NewMemCache(!cfg.DisableCaching)

	if cfg.ReactiveRegistry {
		if err := registry.MakeReactive(
			func(name meta.QualifiedName) {
				disk.InvalidateComponent(name)
				mem.InvalidateComponent(name)
			},
			func(e error) { fmt.Fprintf(os.Stderr, "[go_solid] reactive registry error: %v\n", e) },
		); err != nil {
			pool.Close()
			return nil, err
		}
	}

	bundler := &Bundler{
		// if a bundler is correctly made through New(), the config is at this point assured to be validated, all fields present, and all field values correctly assigned.
		cfg:      cfg,
		registry: registry,
		pool:     pool,
		mem:      mem,
		disk:     disk,
		index:    internal.NewDepIndex(),
	}

	if cfg.Rasterization != rasterization.NIL_RASTERIZATION_CONFIG && !cfg.Rasterization.ExpectCompleted {
		// begin only rasterization when BehaviouralDefaults have been applied
		for _, comp := range registry.Names() {
			// pre-render all components with disk cache enabled
			_, err := bundler.Render(comp, noop.T_o_Void[RenderCallBuilder](), meta.NIL_PROPS)
			if err != nil {
				pool.Close()
				return nil, fmt.Errorf("go_solid: rasterization failed for component %q: %w", comp, err)
			}
		}
	}

	// Hot browser reload: opt-in, and go_solid mounts its own handler on the
	// consumer-provided mux. When inactive, none of this is constructed and the
	// emitted HTML is byte-identical to a plain render.
	if cfg.isHMROn() {
		normalized, err := hmr_int.NormalizeHMRConfig(cfg.HMR)
		if err != nil {
			pool.Close()
			return nil, err
		}
		bundler.cfg.HMR = normalized

		bundler.hub = hmr_int.NewHub(normalized)
		// go_solid registers its own endpoint — the consumer never wires it.
		normalized.Mux.Handle(normalized.Path, bundler.hub.Handler())

		// NewWatcher starts its own goroutine before returning, so there is no
		// separate Start call to forget.
		w, err := hmr_int.NewWatcher(string(cfg.Components), bundler.index, bundler.hub, registry, func(e error) {
			fmt.Fprintf(os.Stderr, "[go_solid] hmr watch error: %v\n", e)
		})
		if err != nil {
			pool.Close()
			return nil, err
		}
		bundler.watcher = w
	}

	return bundler, nil
}

func (cfg *Config) isHMROn() bool {
	return cfg.HMR != hmr.NIL_HMR_CONFIG && !cfg.HMR.Disabled && cfg.Rasterization.ExpectCompleted == false
}

// Ensures all fields are valid and non-nil, defaulting to defined DEFAULT_XXXX objects where appropriate to indicate no consumer configuration
func configValidationAndNormalization(cfg *Config) error {
	// POLYFILL
	if cfg.Components == "" {
		return fmt.Errorf("go_solid: ComponentsDir is required")
	}
	abs, err := filepath.Abs(cfg.Components)
	if err != nil {
		return fmt.Errorf("go_solid: Expected absolute path to ComponentsDir %q: %w", cfg.Components, err)
	}
	cfg.Components = abs
	if cfg.Dependencies != "" {
		abs, err := filepath.Abs(cfg.Dependencies)
		if err != nil {
			return fmt.Errorf("go_solid: Expected absolute path to Dependencies %q: %w", cfg.Dependencies, err)
		}
		cfg.Dependencies = abs
	} else {
		cfg.Dependencies = cfg.Components
	}
	if cfg.Workspace != "" {
		abs, err := filepath.Abs(cfg.Workspace)
		if err != nil {
			return fmt.Errorf("go_solid: Expected absolute path to Workspace %q: %w", cfg.Workspace, err)
		}
		cfg.Workspace = abs
	} else {
		cfg.Workspace = filepath.Join(cfg.Components, ".go_solid")
	}

	// Check if workspace exists or can be made
	if err := os.MkdirAll(cfg.Workspace, 0o755); err != nil {
		return fmt.Errorf("go_solid: create workspace %q: %w", cfg.Workspace, err)
	}

	if cfg.Generation == nil {
		cfg.Generation = esbuild.NIL_BUNDLER_CONFIG
	} else {
		if cfg.Generation.NodeBin == "" {
			cfg.Generation.NodeBin = esbuild.NIL_BUNDLER_CONFIG.NodeBin
		}
		if cfg.Generation.PoolSize <= 0 {
			cfg.Generation.PoolSize = esbuild.NIL_BUNDLER_CONFIG.PoolSize
		}
		if cfg.Generation.ScriptLocation == "" {
			cfg.Generation.ScriptLocation = esbuild.NIL_BUNDLER_CONFIG.ScriptLocation
		}
		// no need for minify: defaults to false
		// no need for sourcemap: defaults to 0 aka SourceMapNone
		if cfg.Generation.Dependencies == esbuild.NIL_BUNDLER_CONFIG.Dependencies {
			cfg.Generation.Dependencies = cfg.Dependencies
		}
	}
	if cfg.Defaults == nil {
		cfg.Defaults = NIL_BEHAVIOURAL_DEFAULTS
	} else {
		if cfg.Defaults.HeadSegment == nil {
			cfg.Defaults.HeadSegment = NIL_BEHAVIOURAL_DEFAULTS.HeadSegment
		}
		if cfg.Defaults.Requests == nil {
			cfg.Defaults.Requests = NIL_BEHAVIOURAL_DEFAULTS.Requests
		}
	}
	if cfg.HMR == nil {
		cfg.HMR = hmr.NIL_HMR_CONFIG
	}
	if cfg.Static == nil {
		cfg.Static = static.NIL_STATIC_CONFIG
	}
	if cfg.Rasterization == nil {
		cfg.Rasterization = rasterization.NIL_RASTERIZATION_CONFIG
	} else {
		cfg.DisableCaching = false // rasterization requires caching
		if cfg.Rasterization.Location == "" {
			cfg.Rasterization.Location = cfg.Workspace
		}
		if cfg.Rasterization.ExpectCompleted {
			if err := rasterization_int.ExpectCompletedValidationCheck(cfg.Rasterization); err != nil {
				return err
			}
			cfg.HMR.Disabled = true // expecting completed rasterization disables HMR
			cfg.ReactiveRegistry = false
			cfg.Generation.Disabled = true // expecting completed rasterization disables esbuild and node workers
		}
	}

	// Resolve generation settings
	if cfg.Generation.ScriptLocation == esbuild.NIL_BUNDLER_CONFIG.ScriptLocation {
		scriptLocation, err := esbuild_int.MaterializeWorkerScript(cfg.Workspace)
		if err != nil {
			return fmt.Errorf("go_solid: materialize worker script: %w", err)
		}
		cfg.Generation.ScriptLocation = scriptLocation
	}

	return nil
}

func (b *Bundler) Registry() *internal.ComponentRegistry { return b.registry }

func (b *Bundler) Close() {
	if b == nil {
		return
	}
	if b.watcher != nil {
		b.watcher.Stop()
	}
	if b.pool != nil {
		b.pool.Close()
	}
}

func (b *Bundler) logDiskCacheError(err error) {
	fmt.Fprintf(os.Stderr, "[go_solid] disk cache write failed (non-fatal): %v\n", err)
}
