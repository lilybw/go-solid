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
const (
	MODULES_DIR_NAME = "modules"
	TYPES_DIR_NAME   = "types"
)

// ModulesRoot is where generated modules live, <workspace>/modules.
func ModulesRoot(workspace meta.AbsoluteDirectoryPath) meta.AbsoluteDirectoryPath {
	return filepath.Join(workspace, MODULES_DIR_NAME)
}

// ModulePathFor is the generated static module, <workspace>/modules/static.js.
func ModulePathFor(workspace meta.AbsoluteDirectoryPath) meta.AbsoluteFilePath {
	return filepath.Join(ModulesRoot(workspace), MODULE_FILENAME)
}

// EnsureDisabled writes the placeholder module and definition.
func EnsureDisabled(workspace meta.AbsoluteDirectoryPath, publishedTypes meta.AbsoluteDirectoryPath) error {
	if err := os.MkdirAll(ModulesRoot(workspace), 0o755); err != nil {
		return fmt.Errorf("go_solid/static: create %q: %w", ModulesRoot(workspace), err)
	}
	if err := io_int.WriteAtomicMode(ModulePathFor(workspace), []byte(GenerateDisabledModule()), 0o644); err != nil {
		return fmt.Errorf("go_solid/static: write disabled module: %w", err)
	}
	definition := filepath.Join(publishedTypes, DEFINITION_FILENAME)
	if err := io_int.WriteAtomicMode(definition, []byte(GenerateDisabledDefinition()), 0o644); err != nil {
		return fmt.Errorf("go_solid/static: write disabled definition: %w", err)
	}
	return nil
}

func NewStaticRegistry(
	cfg *StaticConfig,
	workspace meta.AbsoluteDirectoryPath,
	publishedTypes meta.AbsoluteDirectoryPath,
	onChange func(meta.AbsoluteFilePath),
	onErr func(error),
) (StaticRegistry, error) {
	if !cfg.Active() {
		return &disabledStaticRegistry{module: ModulePathFor(workspace)}, nil
	}

	reg := &enabledStaticRegistry{
		cfg:            cfg,
		module:         ModulePathFor(workspace),
		definition:     filepath.Join(publishedTypes, DEFINITION_FILENAME),
		onChange:       onChange,
		onErr:          onErr,
		inlineLimit:    cfg.EffectiveInlineLimit(),
		publishedTypes: publishedTypes,
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
	cfg            *StaticConfig
	module         meta.AbsoluteFilePath
	definition     meta.AbsoluteFilePath
	publishedTypes meta.AbsoluteDirectoryPath
	inlineLimit    int64
	onChange       func(meta.AbsoluteFilePath)
	onErr          func(error)

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

	module := GenerateModule(manifest)
	definition := GenerateDefinition(manifest)

	this.mu.Lock()
	this.manifest = manifest
	this.mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(this.module), 0o755); err != nil {
		return fmt.Errorf("go_solid/static: create %q: %w", filepath.Dir(this.module), err)
	}
	changed, err := writeIfChanged(this.module, module)
	if err != nil {
		return err
	}
	if _, err := writeIfChanged(this.definition, definition); err != nil {
		return err
	}

	if changed && this.onChange != nil {
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
