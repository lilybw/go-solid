package static

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	io_int "github.com/lilybw/go-solid/internal/io"
	"github.com/lilybw/go-solid/internal/meta"
	watching_int "github.com/lilybw/go-solid/internal/watching"
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
		if _, err := writeIfChanged(filepath.Join(root, name), contents); err != nil {
			return err
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
	// endpoint is registered under is the prefix those URLs carry.
	cfg.Mux.Handle(cfg.EffectiveMountPath(), reg.Handler())

	if cfg.Reactive {
		if err := reg.watch(); err != nil {
			return nil, err
		}
	}
	return reg, nil
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

	// The debounce outlives the event that armed it, so Close has to reach it
	// as well as the watcher: a rebuild that fires afterwards reads a directory
	// the caller may already have torn down, and calls onChange into caches
	// that consider themselves shut.
	debounceMu sync.Mutex
	debounce   *time.Timer
	closed     bool
	rebuilds   sync.WaitGroup
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

// writeIfChanged republishes only on a real difference, so nothing downstream
// is invalidated by a rebuild that produced the same bytes.
func writeIfChanged(path meta.AbsoluteFilePath, contents string) (bool, error) {
	if current, err := os.ReadFile(path); err == nil && string(current) == contents {
		return false, nil
	}
	if err := io_int.WriteAtomicMode(path, []byte(contents), 0o644); err != nil {
		return false, fmt.Errorf("go_solid/static: publish %q: %w", path, err)
	}
	return true, nil
}

// rebuildWindow is how long events are collected before a rebuild.
const rebuildWindow = 120 * time.Millisecond

func (this *enabledStaticRegistry) watch() error {
	touched := func(meta.AbsoluteFilePath, meta.Void) error {
		this.scheduleRebuild(rebuildWindow)
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

func (this *enabledStaticRegistry) scheduleRebuild(window time.Duration) {
	this.debounceMu.Lock()
	defer this.debounceMu.Unlock()

	if this.closed {
		return
	}
	if this.debounce != nil {
		this.debounce.Stop()
	}
	this.debounce = time.AfterFunc(window, func() {
		this.debounceMu.Lock()
		if this.closed {
			this.debounceMu.Unlock()
			return
		}
		this.rebuilds.Add(1)
		this.debounceMu.Unlock()
		defer this.rebuilds.Done()

		if err := this.rebuild(); err != nil {
			this.reportErr(err)
		}
	})
}

func (this *enabledStaticRegistry) reportErr(err error) {
	if this.onErr != nil {
		this.onErr(err)
	}
}

func (this *enabledStaticRegistry) Close() {
	this.closeOnce.Do(func() {
		this.debounceMu.Lock()
		this.closed = true
		if this.debounce != nil {
			this.debounce.Stop()
		}
		this.debounceMu.Unlock()

		this.rebuilds.Wait()
		this.watcher.Stop() // nil-safe
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

		asset, ok := manifest.ByURL[r.URL.Path]
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
