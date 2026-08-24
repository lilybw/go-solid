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

	// Static enables asset serving. Provide Location and Mux to switch it on.
	//
	// go_solid walks Location, mounts a read-only endpoint on Mux, and
	// generates a module mirroring the directory that a component imports:
	//
	//	import S from "go-solid/static";
	//	<img src={S.images.logo} />
	//	const config = await S.load(S.data.config);
	//
	// Leaves are content-hashed URLs, so they are immutable and cached
	// forever, and a file name becomes a key by replacing whatever a
	// JavaScript identifier cannot hold with "_". Two names colliding is an
	// error at New naming both files.
	//
	// The module exists whether or not the feature is on; left off, reaching
	// into it is a TypeScript error naming the setting to change.
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
	cfg *Config
	// buildID fingerprints the generation settings every artifact in the
	// caches was produced under; see buildFingerprint.
	buildID  string
	registry *internal.ComponentRegistry
	mem      *caching.MemCache
	disk     *caching.DiskCache
	index    *internal.DependencyIndex
	static   static_int.StaticRegistry
	defaults *networking_int.Defaults
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

	// Behavioural defaults belong to this Bundler, not to the process: two
	// Bundlers in one program keep their own.
	defaults := networking_int.NewDefaults()
	if consumerDefaults {
		defaults.SetHTMLHeadSegment(cfg.Defaults.HeadSegment)
		defaults.SetRequestBehaviour(cfg.Defaults.Requests)
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

	// Caches are enabled unless explicitly disabled. The cache root is the
	// workspace unless Rasterization.Location moved it.
	disk, err := caching.NewDiskCache(cfg.componentCacheRoot(), !cfg.DisableCaching)
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

	// invalidateComponentFile drops every component backed by one file: the
	// default export and every "#" selection out of it.
	invalidateComponentFile := func(file meta.QualifiedName) {
		for _, name := range mem.ComponentsInFile(file) {
			invalidateComponent(name)
		}
		for _, name := range disk.ComponentsInFile(file) {
			invalidateComponent(name)
		}
		invalidateComponent(file)
	}

	// A touched file invalidates the component it backs plus every component
	// that bundled it as a dependency.
	//
	// The dependency index only knows components that have been rendered, so
	// after a restart it is empty and the file's own entries are reached by
	// name instead. One file can back several — its default export and any
	// number of "#" selections — so that fallback is file-scoped.
	invalidateForSource := func(file meta.AbsoluteFilePath) {
		for _, name := range index.DependentsOf(file) {
			invalidateComponent(name)
		}
		if name, ok := registry.NameForFile(file); ok {
			invalidateComponentFile(name)
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

	// Pass one: every switchable feature gets a resolvable placeholder, so a
	// component may import one unconditionally and a build never fails on a
	// module that was never generated.
	publishedTypes := types_int.PublishedRoot(cfg.Workspace)
	if err := static_int.EnsureDisabled(cfg.Workspace, publishedTypes); err != nil {
		return nil, err
	}

	// Pass two: with the caches and the dependency graph in place, build the
	// features that were configured. Regenerating the static module invalidates
	// every bundle that imported it, which is why this needs invalidateForSource.
	static, err := static_int.NewStaticRegistry(
		cfg.Static, cfg.Workspace, publishedTypes,
		invalidateForSource,
		func(e error) {
			log_int.Log(logging.LEVEL_ERROR, fmt.Sprintf("[go_solid/static] %v", e))
		},
	)
	if err != nil {
		return nil, err
	}

	bundler := &Bundler{
		// if a bundler is correctly made through New(), the config is at this point assured to be validated, all fields present, and all field values correctly assigned.
		cfg:      cfg,
		buildID:  buildFingerprint(cfg.Generation),
		registry: registry,
		mem:      mem,
		disk:     disk,
		index:    index,
		static:   static,
		defaults: defaults,
		types:    typeChecker,
	}

	if err := bundler.types.OnBoot(registry.Components()); err != nil {
		return nil, err
	}

	if cfg.Rasterization.Active() && !cfg.Rasterization.ExpectCompleted {
		// begin only rasterization when BehaviouralDefaults.HeadSegment have been applied
		for _, comp := range registry.Names() {
			// pre-render all components with disk cache enabled
			_, err := bundler.Render(comp, noop.T_o_Void[RenderCallBuilder](), meta.NIL_PROPS)
			if err == nil {
				continue
			}
			// A file in the components tree that exports no component is a
			// helper module, not a broken build. Rasterization walks past it;
			// asking for it by name is still an error, at the render that asks.
			if types_int.IsNotAComponent(err) {
				log_int.Log(logging.LEVEL_INFO, fmt.Sprintf(
					"[go_solid] rasterization skipped %q: %v", comp, err))
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
			string(cfg.Components), bundler.index, bundler.hub,
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

// buildFingerprint identifies the settings that change the bytes esbuild emits.
// It is part of every cache key, so an artifact built under one set of settings
// is never served after they change.
//
// Dependencies and Disabled are deliberately absent: neither alters the output
// of a build that does happen.
func buildFingerprint(cfg *esbuild.BundlerConfig) string {
	if cfg == nil {
		return ""
	}
	return caching.ShortHash(fmt.Sprintf("minify=%v;sourcemap=%d;solid=%+v",
		cfg.Minify, cfg.Sourcemap, cfg.Solid), 16)
}

// componentCacheRoot is the directory holding the component cache.
//
// Rasterization.Location overrides the workspace, so a pre-built cache can be
// produced into, and shipped from, somewhere the workspace does not reach.
func (cfg *Config) componentCacheRoot() meta.AbsoluteDirectoryPath {
	if cfg.Rasterization != nil && cfg.Rasterization.Location != "" {
		return cfg.Rasterization.Location
	}
	return cfg.Workspace
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

	// The two unconfigured states differ on purpose. A nil Generation takes the
	// null object's opinions — Minify on, which is what an unattended build
	// should do. A supplied one is taken at its word, zero values included, so
	// nothing the consumer wrote is overridden by a default they did not ask
	// for. Both are reachable by "I did not configure bundling", so the choice
	// of nil versus &BundlerConfig{} is a choice of Minify.
	if cfg.Generation == nil {
		cfg.Generation = meta.Copy(esbuild.NIL_BUNDLER_CONFIG)
		cfg.Generation.Dependencies = cfg.Components
	} else {
		cfg.Generation.Solid.Normalize()
		// Minify and Sourcemap are left as given; see above.
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
		// A supplied config means the feature was asked for, so an incomplete
		// one is a mistake rather than a way of leaving it off. Disabled is how
		// you leave it off while keeping the settings written down.
		if !cfg.Static.Disabled {
			if cfg.Static.Location == "" {
				return fmt.Errorf("go_solid: Config.Static was provided but Location is unset; " +
					"name the directory holding your assets, or set Static.Disabled")
			}
			if cfg.Static.Mux == nil {
				return fmt.Errorf("go_solid: Config.Static.Mux is required when static assets are " +
					"enabled (go_solid mounts the asset endpoint on your mux)")
			}
			abs, err := filepath.Abs(cfg.Static.Location)
			if err != nil {
				return fmt.Errorf("go_solid: Expected absolute path to Static.Location %q: %w",
					cfg.Static.Location, err)
			}
			cfg.Static.Location = abs
			if info, err := os.Stat(abs); err != nil || !info.IsDir() {
				return fmt.Errorf("go_solid: Static.Location %q is not a readable directory", abs)
			}
		}
		if cfg.Static.Ignore == nil {
			cfg.Static.Ignore = static.DEFAULT_IGNORE
		}
		if cfg.Static.MountPath == "" {
			cfg.Static.MountPath = static.DEFAULT_MOUNT_PATH
		}
		if !strings.HasPrefix(cfg.Static.MountPath, "/") {
			cfg.Static.MountPath = "/" + cfg.Static.MountPath
		}
		if !strings.HasSuffix(cfg.Static.MountPath, "/") {
			cfg.Static.MountPath += "/"
		}
		if cfg.Static.InlineLimit == 0 {
			cfg.Static.InlineLimit = static.DEFAULT_INLINE_LIMIT
		}
	}
	// The generated modules resolve under stable specifiers rather than
	// workspace paths, so nothing in user code depends on where the workspace
	// happens to be. Registered before the fingerprint is taken, since a change
	// here changes what esbuild emits.
	if cfg.Generation.Alias == nil {
		cfg.Generation.Alias = map[string]string{}
	}
	cfg.Generation.Alias[static_int.MODULE_SPECIFIER] = static_int.ModulePathFor(cfg.Workspace)

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
	// Location names the component cache wherever it is, so it is resolved
	// whether or not rasterization itself is on.
	if cfg.Rasterization.Location != "" {
		abs, err := filepath.Abs(cfg.Rasterization.Location)
		if err != nil {
			return fmt.Errorf("go_solid: Expected absolute path to Rasterization.Location %q: %w",
				cfg.Rasterization.Location, err)
		}
		cfg.Rasterization.Location = abs
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
		} else if err := os.MkdirAll(cfg.Rasterization.Location, 0o755); err != nil {
			return fmt.Errorf("go_solid: create rasterization location %q: %w",
				cfg.Rasterization.Location, err)
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

// Static is the asset manifest, for the Go side of the same data the generated
// module exposes to the browser. Nil when the feature is off.
//
//	url := bundler.Static().URL("images/hero.png")
func (b *Bundler) Static() *static_int.Manifest {
	if b == nil || b.static == nil {
		return nil
	}
	return b.static.Manifest()
}

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
	if b.static != nil {
		b.static.Close() // static asset watcher, if Static.Reactive was set
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
