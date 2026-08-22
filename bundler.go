package go_solid

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/lilybw/go-solid/internal"
	caching "github.com/lilybw/go-solid/internal/caching"
	esbuild_int "github.com/lilybw/go-solid/internal/esbuild"
	hmr_int "github.com/lilybw/go-solid/internal/hmr"
	log_int "github.com/lilybw/go-solid/internal/logging"
	"github.com/lilybw/go-solid/internal/meta"
	networking_int "github.com/lilybw/go-solid/internal/networking"
	"github.com/lilybw/go-solid/internal/noop"
	rasterization_int "github.com/lilybw/go-solid/internal/rasterization"
	static_int "github.com/lilybw/go-solid/internal/static"
	types_int "github.com/lilybw/go-solid/internal/types"
	"github.com/lilybw/go-solid/shared/esbuild"
	"github.com/lilybw/go-solid/shared/hmr"
	logging "github.com/lilybw/go-solid/shared/logging"
	networking "github.com/lilybw/go-solid/shared/networking"
	"github.com/lilybw/go-solid/shared/rasterization"
	"github.com/lilybw/go-solid/shared/static"
	"github.com/lilybw/go-solid/shared/types"
)

type Config struct {
	// The absolute path to the directory containing the solidjs components.
	// The registry will scan this and all subdirectories, skipping directories prefixed with a dot (.)
	// or with the exact name "node_modules"
	Components meta.AbsoluteDirectoryPath

	// The absolute path to the directory where go_solid will place its .go_solid workspace directory, which contains the worker script and the disk cache.
	// Defaults to Components if not specified.
	Workspace meta.AbsoluteDirectoryPath

	// LogLevel gates all diagnostic output. Left unset it resolves to
	// logging.DEFAULT_LEVEL (errors only); set logging.LEVEL_DEBUG to have the
	// normalized config dumped at construction.
	//
	// The logger is process-global, so the most recent New wins.
	LogLevel logging.LogLevel

	// DisableCaching bypasses both the in-memory and on-disk caches, so every
	// render rebuilds from source.
	DisableCaching bool

	// Settings for code generation, solidjs transform application, worker pool size, and the like.
	//
	// Expects node_modules to be located within Config#Components by default. Can be overwritten using this sub-config.
	Generation *esbuild.BundlerConfig

	// Enable a filewatcher that watches the component dir to trigger registry updates when new component files are added.
	// This enables usecases which may attempt to ask for procedural component names, since the registry is constant otherwise.
	ReactiveRegistry bool

	// If you provide this config, bundle and cache all components in the registry on next boot (may take a moment).
	// This is purely a performance measure — every component is pre-built, so no request pays bundling cost.
	// With ExpectCompleted set, esbuild is skipped entirely and components are served straight from the cache.
	//
	// Do be aware that this disables HMR, ReactiveRegistry and DisableCaching (caches are now mandatory).
	//
	// Enabled by default; set Rasterization.Disabled to opt out.
	Rasterization *rasterization.RasterizationConfig

	// Types governs how go_solid checks the Go props a template is rendered
	// with against the type its component declares for them.
	//
	// The component is the contract. Shapes extracted from it are cached under
	// the workspace whatever this holds; Types.Check only selects when the
	// props are held against them.
	Types *types.TypesConfig

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
	HeadSegment meta.Configurator[networking.HTMLHeadSegmentBuilder] `json:"-"`
	// Define the default behaviour of the http request handling. These defaults can be modified upon any Bundler#Prepare call by using the method: SetHTTPBehaviour
	Requests meta.Configurator[networking.RequestBehaviourBuilder] `json:"-"`
}

var NIL_BEHAVIOURAL_DEFAULTS = &BehaviouralDefaults{ // null object
	HeadSegment: noop.T_o_Void[networking.HTMLHeadSegmentBuilder](),
	Requests:    noop.T_o_Void[networking.RequestBehaviourBuilder](),
}

type Bundler struct {
	cfg      *Config
	registry *internal.ComponentRegistry
	mem      *caching.MemCache
	disk     *caching.DiskCache
	index    *internal.DependencyIndex
	static   static_int.StaticRegistry
	hub      *hmr_int.Hub
	watcher  *hmr_int.Watcher
	types    *types_int.Checker
}

func New(cfg *Config) (*Bundler, error) {
	consumerDefaults := cfg.Defaults != nil
	// A rasterization the consumer asked for is load-bearing and its failures
	// are fatal. The default-on one is an optimisation, so it warns instead.
	consumerRasterization := cfg.Rasterization != nil
	if err := configValidationAndNormalization(cfg); err != nil {
		return nil, err
	}

	if consumerDefaults {
		networking_int.SetHTMLHeadSegmentTemplate(cfg.Defaults.HeadSegment)
		networking_int.SetRequestBehaviourTemplate(cfg.Defaults.Requests)
	}

	if !cfg.Generation.Disabled {
		if missing := esbuild_int.PeerDepsMissing(cfg.Generation.Dependencies, esbuild_int.PeerDepsForConfig(cfg.Generation.Solid)); len(missing) > 0 {
			return nil, fmt.Errorf(
				"go_solid: missing npm dependencies %v in %q (or any ancestor).\n"+
					"Install them in your frontend project:\n"+
					"    npm install %s",
				missing, cfg.Generation.Dependencies, strings.Join(missing, " "))
		}
	}

	registry, err := internal.NewRegistry(cfg.Components)
	if err != nil {
		return nil, err
	}

	// Caches are enabled unless explicitly disabled.
	disk, err := caching.NewDiskCache(cfg.Workspace, !cfg.DisableCaching)
	if err != nil {
		return nil, err
	}

	mem := caching.NewMemCache(!cfg.DisableCaching)
	index := internal.NewDepIndex()

	typeChecker := types_int.NewChecker(cfg.Workspace, cfg.Types.Check, nil)
	if err := types_int.EnsurePublished(cfg.Workspace); err != nil {
		return nil, err
	}

	invalidateComponent := func(name meta.QualifiedName) {
		disk.InvalidateComponent(name)
		mem.InvalidateComponent(name)
		typeChecker.Invalidate(name)
	}

	// A touched file invalidates the component it backs plus every component
	// that bundled it as a dependency.
	invalidateForSource := func(file meta.AbsoluteFilePath) {
		affected := index.DependentsOf(file)
		if name, ok := registry.NameForFile(file); ok && !slices.Contains(affected, name) {
			affected = append(affected, name)
		}
		for _, name := range affected {
			invalidateComponent(name)
		}
	}

	if cfg.ReactiveRegistry {
		if err := registry.MakeReactive(
			invalidateComponent,
			invalidateForSource,
			func(e error) { fmt.Fprintf(os.Stderr, "[go_solid] reactive registry error: %v\n", e) },
		); err != nil {
			return nil, err
		}
	}

	static, err := static_int.NewStaticRegistry(cfg.Static)
	if err != nil {
		return nil, err
	}

	bundler := &Bundler{
		// if a bundler is correctly made through New(), the config is at this point assured to be validated, all fields present, and all field values correctly assigned.
		cfg:      cfg,
		registry: registry,
		mem:      mem,
		disk:     disk,
		index:    index,
		static:   static,
		types:    typeChecker,
	}

	// Ahead of rasterization: the cache should be warm, and anything unchecked
	// named, even if a component later fails to bundle.
	bundler.types.OnBoot(registry.Components())

	if cfg.Rasterization.Active() && !cfg.Rasterization.ExpectCompleted {
		// begin only rasterization when BehaviouralDefaults.HeadSegment have been applied
		for _, comp := range registry.Names() {
			// pre-render all components with disk cache enabled
			_, err := bundler.Render(comp, noop.T_o_Void[RenderCallBuilder](), meta.NIL_PROPS)
			if err == nil {
				continue
			}
			if consumerRasterization {
				return nil, fmt.Errorf("go_solid: rasterization failed for component %q: %w", comp, err)
			}
			log_int.Log(logging.LEVEL_ERROR, fmt.Sprintf(
				"[go_solid] rasterization skipped %q: %v", comp, err))
		}
	}

	// Hot browser reload: opt-in, and go_solid mounts its own handler on the
	// consumer-provided mux. When inactive, none of this is constructed and the
	// emitted HTML is byte-identical to a plain render.
	if cfg.isHMROn() {
		normalized, err := hmr_int.NormalizeHMRConfig(cfg.HMR)
		if err != nil {
			return nil, err
		}
		bundler.cfg.HMR = normalized

		bundler.hub = hmr_int.NewHub(normalized)
		// go_solid registers its own endpoint — the consumer never wires it.
		normalized.Mux.Handle(normalized.Path, bundler.hub.Handler())

		// NewWatcher starts its own goroutine before returning, so there is no
		// separate Start call to forget.
		w, err := hmr_int.NewWatcher(
			string(cfg.Components), bundler.index, bundler.hub, registry,
			invalidateComponent,
			func(e error) {
				fmt.Fprintf(os.Stderr, "[go_solid] hmr watch error: %v\n", e)
			})
		if err != nil {
			return nil, err
		}
		bundler.watcher = w
	}

	return bundler, nil
}

func (cfg *Config) isHMROn() bool {
	return !cfg.HMR.Disabled && !cfg.Rasterization.ExpectCompleted
}

// Ensures all fields are valid and non-nil, defaulting to defined DEFAULT_XXXX objects where appropriate to indicate no consumer configuration
func configValidationAndNormalization(cfg *Config) error {
	if cfg.LogLevel == logging.LEVEL_UNSET {
		cfg.LogLevel = logging.DEFAULT_LEVEL
	}
	log_int.SetLevel(cfg.LogLevel)
	log_int.LogJSON(logging.LEVEL_TRACE, "[bundler.go#configValidationAndNormalization] user config:", cfg)
	// POLYFILL
	if cfg.Components == "" {
		return fmt.Errorf("go_solid: ComponentsDir is required")
	}
	abs, err := filepath.Abs(cfg.Components)
	if err != nil {
		return fmt.Errorf("go_solid: Expected absolute path to ComponentsDir %q: %w", cfg.Components, err)
	}
	cfg.Components = abs
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
		cfg.Generation = meta.Copy(esbuild.NIL_BUNDLER_CONFIG)
		cfg.Generation.Dependencies = cfg.Components
	} else {
		cfg.Generation.Solid.Normalize()
		// no need for minify: defaults to false
		// no need for sourcemap: defaults to 0 aka SourceMapNone
		if cfg.Generation.Dependencies == esbuild.NIL_BUNDLER_CONFIG.Dependencies {
			cfg.Generation.Dependencies = cfg.Components
		}
		if err := cfg.Generation.Solid.Validate(cfg.Generation.Dependencies); err != nil {
			return err
		}
	}

	if abs, err := filepath.Abs(cfg.Generation.Dependencies); err != nil {
		return fmt.Errorf("go_solid: Expected absolute path to Dependencies %q: %w", cfg.Generation.Dependencies, err)
	} else {
		cfg.Generation.Dependencies = abs
	}

	if cfg.Defaults == nil {
		cfg.Defaults = meta.Copy(NIL_BEHAVIOURAL_DEFAULTS)
	} else {
		if cfg.Defaults.HeadSegment == nil {
			cfg.Defaults.HeadSegment = NIL_BEHAVIOURAL_DEFAULTS.HeadSegment
		}
		if cfg.Defaults.Requests == nil {
			cfg.Defaults.Requests = NIL_BEHAVIOURAL_DEFAULTS.Requests
		}
	}

	if cfg.HMR == nil {
		cfg.HMR = meta.Copy(hmr.NIL_HMR_CONFIG)
	}
	if cfg.Static == nil {
		cfg.Static = meta.Copy(static.NIL_STATIC_CONFIG)
	} else {
		if cfg.Static.Ignore == nil {
			cfg.Static.Ignore = static.NIL_STATIC_CONFIG.Ignore
		}
		if cfg.Static.Location == "" {
			return fmt.Errorf("Static config provided yet location unset. Kindly state.")
		}
	}
	rasterizationProvided := cfg.Rasterization != nil
	if !rasterizationProvided {
		cfg.Rasterization = meta.Copy(rasterization.NIL_RASTERIZATION_CONFIG)
		// Rasterization is on by default, but a default must not override an
		// explicit choice: it has nowhere to write without caches, and nothing
		// to build without bundling.
		if cfg.DisableCaching || cfg.Generation.Disabled {
			cfg.Rasterization.Disabled = true
		}
	}
	if cfg.Rasterization.Active() {
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
			cfg.Generation.Disabled = true // expecting completed rasterization disables esbuild
		}
	}

	// Must follow rasterization: whether the boot pass is possible depends on it.
	if err := normalizeTypes(cfg); err != nil {
		return err
	}

	log_int.LogJSON(logging.LEVEL_DEBUG, "normalized configuration: ", cfg)
	return nil
}

// normalizeTypes resolves Types.Check and reconciles it with rasterization.
//
// The boot pass rides on rasterization's registry walk, so it cannot run
// without it. Asking for it outright is an error; arriving at it by leaving
// Check unset drops the boot half and keeps going, since the consumer never
// asked for something that cannot be delivered.
func normalizeTypes(cfg *Config) error {
	if cfg.Types == nil {
		cfg.Types = meta.Copy(types.NIL_TYPES_CONFIG)
	}
	if cfg.Types.Check == types.CHECK_UNSET {
		cfg.Types.Check = types.DEFAULT_CHECK
	}
	// Check is honoured as given. The boot pass reads and parses component
	// sources; it neither bundles nor renders, so it has no bearing on
	// rasterization and must not quietly switch it on.
	return nil
}

func (b *Bundler) Registry() *internal.ComponentRegistry { return b.registry }

func (b *Bundler) Close() {
	if b == nil {
		return
	}
	if b.watcher != nil {
		b.watcher.Stop()
		b.watcher = nil
	}
	if b.registry != nil {
		b.registry.Close() // reactive registry watcher, if MakeReactive ran
	}
}

func (b *Bundler) logDiskCacheError(err error) {
	fmt.Fprintf(os.Stderr, "[go_solid] disk cache write failed (non-fatal): %v\n", err)
}

func (b *Bundler) FromDisk(key *caching.CacheKey) (*caching.Rendered, bool) {
	if b.disk != nil {
		if cached, ok := b.disk.Get(key); ok {
			b.mem.Put(key, cached) // promote to memory
			return cached, true
		}
	}
	return nil, false
}

func (b *Bundler) searchCaches(key *caching.CacheKey) (*caching.Rendered, bool) {
	if cached, ok := b.mem.Get(key); ok {
		return cached, true
	}
	return b.FromDisk(key)
}

func (b *Bundler) constructHMRScript(component meta.QualifiedName) string {
	// Inject the hot-reload client only when HMR is active. Generated here in
	// package go_solid (which imports both hmr and internal) and passed to
	// AssembleHTML as a plain string, so internal never imports hmr — avoiding
	// the import cycle (hmr already imports internal).
	hmrScript := ""
	if b.hub != nil {
		hmrScript = hmr_int.ClientScript(b.cfg.HMR.Path, component)
	}
	return hmrScript
}
