package static

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	io_int "github.com/lilybw/go-solid/internal/io"
	watching_int "github.com/lilybw/go-solid/internal/watching"
	"github.com/lilybw/go-solid/shared/compat"
	"github.com/lilybw/go-solid/shared/meta"
	. "github.com/lilybw/go-solid/shared/static"
	"github.com/lilybw/go-solid/shared/watching"
)

type StaticRegistry interface {
	// Active reports whether assets are being served.
	Active() bool
	// Manifest is the current set of servable assets, nil when inactive.
	Manifest() *Manifest
	// ModulePath is the generated module on disk. Bundles import it, so it is
	// also a source they depend on.
	ModulePath() meta.AbsoluteFilePath
	// Close stops the watcher, if one was started.
	Close()
}

// Paths the registry writes to.
const MODULES_DIR_NAME = "modules"

// ModulesRoot is where generated modules live, <workspace>/modules.
func ModulesRoot(workspace meta.AbsoluteDirectoryPath) meta.AbsoluteDirectoryPath {
	return filepath.Join(workspace, MODULES_DIR_NAME)
}

// ModuleRoot is the static module's own directory. Everything belonging to it
// lives here and nowhere else, so it can be read, deleted or ignored as a unit.
func ModuleRoot(workspace meta.AbsoluteDirectoryPath) meta.AbsoluteDirectoryPath {
	return filepath.Join(ModulesRoot(workspace), MODULE_NAME)
}

// ModulePathFor is the module's entry point, which is what the bundler aliases
// the specifier to and what the dependency graph records as a source.
func ModulePathFor(workspace meta.AbsoluteDirectoryPath) meta.AbsoluteFilePath {
	return filepath.Join(ModuleRoot(workspace), INDEX_FILENAME)
}

// EnsureDisabled writes a module that resolves and carries nothing.
//
// This is the first of the two passes. Every switchable feature gets a
// resolvable placeholder before anything is configured, so a component may
// import one unconditionally and a build never fails on a module that was never
// generated. It also writes the tsconfig fragment, which describes where
// modules live rather than what is in them and is therefore the same either way.
func EnsureDisabled(workspace meta.AbsoluteDirectoryPath) error {
	fragment, err := EnsureTSConfigFragment(workspace)
	if err != nil {
		return err
	}
	return writeModule(workspace, moduleSources{
		index:  GenerateDisabledIndex(),
		assets: GenerateDisabledAssets(),
		readme: GenerateReadme(fragment, false),
	})
}

// moduleSources is one complete rendering of the module.
type moduleSources struct {
	index  string
	assets string
	readme string
}

// writeModule publishes the module and reports whether anything changed.
//
// Only a real difference is written. The index is a bundle input, so touching
// it invalidates every bundle that imported it — doing that for a rebuild that
// produced identical bytes would rebuild the world every time an editor saved a
// file it had not altered.
func writeModule(workspace meta.AbsoluteDirectoryPath, sources moduleSources) error {
	root := ModuleRoot(workspace)
	if err := os.MkdirAll(root, 0o755); err != nil {
		return fmt.Errorf("go_solid/static: create %q: %w", root, err)
	}
	for name, contents := range map[string]string{
		INDEX_FILENAME:   sources.index,
		RUNTIME_FILENAME: GenerateRuntime(),
		ASSETS_FILENAME:  sources.assets,
		README_FILENAME:  sources.readme,
	} {
		if _, err := io_int.WriteIfChanged(filepath.Join(root, name), []byte(contents), 0o644); err != nil {
			return fmt.Errorf("go_solid/static: publish %q: %w", name, err)
		}
	}
	return nil
}

func NewStaticRegistry(
	cfg *StaticConfig,
	workspace meta.AbsoluteDirectoryPath,
	onChange func(meta.AbsoluteFilePath),
	onErr func(error),
) (StaticRegistry, error) {
	if !cfg.Active() {
		return &disabledStaticRegistry{module: ModulePathFor(workspace)}, nil
	}

	reg := &enabledStaticRegistry{
		cfg:         cfg,
		workspace:   workspace,
		module:      ModulePathFor(workspace),
		onChange:    onChange,
		onErr:       onErr,
		inlineLimit: cfg.EffectiveInlineLimit(),
	}
	if err := reg.rebuild(); err != nil {
		return nil, err
	}

	// The same accessor the manifest built its URLs from, so the pattern the
	// endpoint is registered under is the prefix those URLs carry. Normalize
	// resolves how this particular router registers a subtree, which is not
	// something every router spells the way ServeMux does.
	mux := compat.Normalize(cfg.Mux)
	mux.Handle(cfg.EffectiveMountPath(), reg.Handler())

	if err := verifyMount(cfg, mux, reg.Manifest()); err != nil {
		return nil, err
	}

	if cfg.Reactive {
		if err := reg.watch(); err != nil {
			return nil, err
		}
	}
	return reg, nil
}

// verifyMount asks the consumer's router for an asset that was just published.
//
// The mount is a URL prefix — every asset is served from below it — but the
// registration is one pattern, so a router that matches it exactly rather than
// as a subtree accepts the registration and then 404s every asset. Asking once
// at boot makes that an error here instead of a broken page later.
func verifyMount(cfg *StaticConfig, mux compat.MuxLike, manifest *Manifest) error {
	router, addressable := servable(mux)
	if !addressable || manifest == nil || len(manifest.ByURL) == 0 {
		return nil // nothing to ask for, or nobody to ask
	}

	canary := ""
	for candidate := range manifest.ByURL {
		if canary == "" || candidate < canary {
			canary = candidate // lowest key, so the probe is deterministic
		}
	}

	request := &http.Request{
		Method:     http.MethodHead,
		URL:        &url.URL{Path: canary},
		Proto:      "HTTP/1.1",
		ProtoMajor: 1,
		ProtoMinor: 1,
		Header:     http.Header{PROBE_HEADER: []string{"1"}},
		Host:       "localhost",
		RemoteAddr: "127.0.0.1:0",
		RequestURI: canary,
	}

	probe := &statusProbe{status: http.StatusOK, header: http.Header{}}
	if inconclusive := func() (panicked bool) {
		// Consumer middleware sits on this path and may not tolerate a request
		// that never came off a socket. That says nothing about the mount.
		defer func() { panicked = recover() != nil }()
		router.ServeHTTP(probe, request)
		return false
	}(); inconclusive || probe.status != http.StatusNotFound {
		return nil
	}

	return fmt.Errorf(
		"go_solid/static: mounted the asset endpoint at %q with %s, but the supplied Mux "+
			"answered 404 for %q.\n\tThe mount is a prefix and has to match as a subtree. If this "+
			"router spells that some other way, register it yourself:\n"+
			"\t\tcfg.Mux = compat.MuxLikeFromFunc(func(p string, h http.Handler) { ... })",
		cfg.EffectiveMountPath(), strategyOf(mux), canary)
}

// servable resolves the thing that answers requests, whether the config holds
// it directly or an adapter stands in front of it.
func servable(mux compat.MuxLike) (http.Handler, bool) {
	if handler, ok := mux.(http.Handler); ok {
		return handler, true
	}
	if adapter, ok := mux.(compat.Servable); ok {
		handler := adapter.Servable()
		return handler, handler != nil
	}
	return nil, false
}

func strategyOf(mux compat.MuxLike) compat.Strategy {
	if described, ok := mux.(compat.Described); ok {
		return described.Strategy()
	}
	return compat.STRATEGY_HANDLE
}

// PROBE_HEADER marks the boot-time mount check, so middleware that must not run
// for it can tell it apart from a real request.
const PROBE_HEADER = "X-Go-Solid-Mount-Probe"

// statusProbe is a ResponseWriter that keeps the status and drops the body.
type statusProbe struct {
	header  http.Header
	status  int
	written bool
}

func (this *statusProbe) Header() http.Header { return this.header }

func (this *statusProbe) Write(body []byte) (int, error) {
	this.WriteHeader(http.StatusOK)
	return len(body), nil
}

func (this *statusProbe) WriteHeader(status int) {
	if !this.written {
		this.status, this.written = status, true
	}
}

type disabledStaticRegistry struct {
	module meta.AbsoluteFilePath
}

func (this *disabledStaticRegistry) Active() bool                      { return false }
func (this *disabledStaticRegistry) Manifest() *Manifest               { return nil }
func (this *disabledStaticRegistry) ModulePath() meta.AbsoluteFilePath { return this.module }
func (this *disabledStaticRegistry) Close()                            {}

type enabledStaticRegistry struct {
	cfg         *StaticConfig
	workspace   meta.AbsoluteDirectoryPath
	module      meta.AbsoluteFilePath
	inlineLimit int64
	onChange    func(meta.AbsoluteFilePath)
	onErr       func(error)

	mu       sync.RWMutex
	manifest *Manifest

	watcher   *watching_int.DirectoryWatcher[meta.Void]
	closeOnce sync.Once

	debounce *watching_int.Debouncer
}

func (this *enabledStaticRegistry) Active() bool { return true }

func (this *enabledStaticRegistry) Manifest() *Manifest {
	this.mu.RLock()
	defer this.mu.RUnlock()
	return this.manifest
}

func (this *enabledStaticRegistry) ModulePath() meta.AbsoluteFilePath { return this.module }

// rebuild walks the asset directory and republishes both artifacts.
func (this *enabledStaticRegistry) rebuild() error {
	manifest, err := BuildManifest(this.cfg)
	if err != nil {
		return err
	}

	fragment, err := EnsureTSConfigFragment(this.workspace)
	if err != nil {
		return err
	}

	this.mu.Lock()
	this.manifest = manifest
	this.mu.Unlock()

	// The graph is what an asset edit changes; the index and runtime are the
	// same bytes every time, so writeIfChanged leaves them alone. Only the
	// index being touched can invalidate a bundle, and it is not.
	assets := filepath.Join(ModuleRoot(this.workspace), ASSETS_FILENAME)
	before, err := os.ReadFile(assets)
	changedBefore := err != nil || string(before) != GenerateAssets(manifest)

	if err := writeModule(this.workspace, moduleSources{
		index:  GenerateIndex(),
		assets: GenerateAssets(manifest),
		readme: GenerateReadme(fragment, true),
	}); err != nil {
		return err
	}

	// The graph changed, so every bundle that imported the module is stale —
	// the index is what they name, so that is what is reported.
	if changedBefore && this.onChange != nil {
		this.onChange(this.module)
	}
	return nil
}

// rebuildWindow is how long events are collected before a rebuild.
const rebuildWindow = 120 * time.Millisecond

func (this *enabledStaticRegistry) watch() error {
	this.debounce = watching_int.NewDebouncer(rebuildWindow, func() {
		if err := this.rebuild(); err != nil {
			this.reportErr(err)
		}
	})
	touched := func(meta.AbsoluteFilePath, meta.Void) error {
		this.debounce.Arm()
		return nil
	}
	watcher, err := watching_int.NewDirectoryWatcher(this.cfg.Location, &watching.DWVoidConfig{
		OnCreation: touched,
		OnMutation: touched,
		OnDeletion: touched,
		OnErr:      this.reportErr,
	})
	if err != nil {
		return fmt.Errorf("go_solid/static: watch %q: %w", this.cfg.Location, err)
	}
	this.watcher = watcher
	return nil
}

func (this *enabledStaticRegistry) reportErr(err error) {
	if this.onErr != nil {
		this.onErr(err)
	}
}

func (this *enabledStaticRegistry) Close() {
	this.closeOnce.Do(func() {
		this.watcher.Stop() // nil-safe
		this.debounce.Stop()
	})
}

func (this *enabledStaticRegistry) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		this.mu.RLock()
		manifest := this.manifest
		this.mu.RUnlock()

		asset, ok := manifest.Resolve(r.URL.Path)
		if !ok {
			http.NotFound(w, r)
			return
		}

		header := w.Header()
		header.Set("Content-Type", asset.MIME)
		header.Set("ETag", strconv.Quote(asset.ContentHash[:16]))
		// The URL carries the content hash, so this body can never change under
		// this path. Nothing needs to revalidate, ever.
		header.Set("Cache-Control", "public, max-age=31536000, immutable")
		header.Set("X-Content-Type-Options", "nosniff")

		if asset.MemCached != nil {
			http.ServeContent(w, r, asset.Rel, time.Time{}, strings.NewReader(string(asset.MemCached)))
			return
		}

		file, err := os.Open(asset.Path)
		if err != nil {
			http.NotFound(w, r) // deleted since the manifest was built
			return
		}
		defer file.Close()
		// ServeContent handles range requests, which is what makes audio and
		// video seekable rather than restartable.
		http.ServeContent(w, r, asset.Rel, time.Time{}, file)
	})
}
