package go_solid

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/lilybw/go-solid/internal"
	caching "github.com/lilybw/go-solid/internal/caching"
	"github.com/lilybw/go-solid/internal/esbuild"
	"github.com/lilybw/go-solid/internal/hmr"
	"github.com/lilybw/go-solid/internal/meta"
	networking_int "github.com/lilybw/go-solid/internal/networking"
	"github.com/lilybw/go-solid/internal/workers"
	"github.com/lilybw/go-solid/shared"
	networking "github.com/lilybw/go-solid/shared/networking"
)

type Config struct {
	Components   meta.AbsoluteDirectoryPath
	Dependencies meta.AbsoluteDirectoryPath
	Workspace    meta.AbsoluteDirectoryPath

	PoolSize int
	NodeBin  string

	// DisableCaching bypasses both the in-memory and on-disk caches, so every
	// render rebuilds from source. Previously implied by Dev.
	DisableCaching bool

	// Sourcemaps emits inline sourcemaps from esbuild for easier debugging in
	// the browser. Independent of caching, so you can debug a cached prod build.
	// Previously implied by Dev.
	Sourcemaps bool

	// HotReloadRegistry rescans the components directory on every render, so new
	// or renamed component files are picked up without a restart. Previously
	// implied by Dev. Note this is registry reload only; hot *browser* reload is
	// the separate HMR feature below.
	HotReloadRegistry bool

	// HMR enables hot browser reload in development. When non-nil and not
	// Disabled, go_solid watches the components tree and pushes reloads to the
	// tabs viewing an affected template. Requires HMR.Mux so go_solid can mount
	// its WebSocket handler itself.
	HMR *shared.HMRConfig

	Minify   bool
	Defaults *BehaviouralDefaults
}

type BehaviouralDefaults struct {
	HTMLHeadAttributes meta.Configurator[networking.HTMLHeadSegmentBuilder]
}

type Bundler struct {
	cfg      Config
	registry *internal.Registry
	pool     *workers.Pool
	cache    *caching.MemCache
	disk     *caching.DiskCache
	index    *internal.DependencyIndex
	hub      *hmr.Hub
	watcher  *hmr.Watcher

	workspace meta.AbsoluteDirectoryPath // resolved workspace (.go_solid) for worker + temp files
}

func New(cfg Config) (*Bundler, error) {
	if err := configValidationCheck(cfg); err != nil {
		return nil, err
	}
	if cfg.Dependencies == "" {
		cfg.Dependencies = cfg.Components
	}
	workspace := cfg.Workspace
	if workspace == "" {
		workspace = filepath.Join(cfg.Components, ".go_solid")
	}
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		return nil, fmt.Errorf("go_solid: create workspace %q: %w", workspace, err)
	}
	scriptLocation, err := esbuild.MaterializeWorkerScript(workspace)
	if err != nil {
		return nil, fmt.Errorf("go_solid: materialize worker script: %w", err)
	}

	if missing := esbuild.PeerDepsMissing(cfg.Dependencies, esbuild.RequiredPeerDeps); len(missing) > 0 {
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
	pool, err := workers.NewPool(workers.PoolConfig{
		Size:         cfg.PoolSize,
		NodeBin:      cfg.NodeBin,
		Script:       scriptLocation,
		Dependencies: cfg.Dependencies,
	})
	if err != nil {
		return nil, err
	}

	if cfg.Defaults != nil && cfg.Defaults.HTMLHeadAttributes != nil {
		networking_int.SetHTMLHeadSegmentTemplate(cfg.Defaults.HTMLHeadAttributes)
	}

	// Caches are enabled unless explicitly disabled.
	disk, err := caching.NewDiskCache(workspace, !cfg.DisableCaching)
	if err != nil {
		// Don't leak the pool if disk cache setup fails.
		pool.Close()
		return nil, err
	}

	bundler := &Bundler{
		cfg:       cfg,
		registry:  registry,
		pool:      pool,
		cache:     caching.NewMemCache(!cfg.DisableCaching),
		disk:      disk,
		workspace: workspace,
		index:     internal.NewDepIndex(),
	}

	// Hot browser reload: opt-in, and go_solid mounts its own handler on the
	// consumer-provided mux. When inactive, none of this is constructed and the
	// emitted HTML is byte-identical to a plain render.
	if cfg.HMR != nil && !cfg.HMR.Disabled {
		normalized, err := hmr.NormalizeHMRConfig(cfg.HMR)
		if err != nil {
			pool.Close()
			return nil, err
		}
		bundler.cfg.HMR = normalized

		bundler.hub = hmr.NewHub(normalized)
		// go_solid registers its own endpoint — the consumer never wires it.
		normalized.Mux.Handle(normalized.HMRPath, bundler.hub.Handler())

		// NewWatcher starts its own goroutine before returning, so there is no
		// separate Start call to forget.
		w, err := hmr.NewWatcher(string(cfg.Components), bundler.index, bundler.hub, registry, func(e error) {
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

func configValidationCheck(cfg Config) error {
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
	}
	if cfg.Workspace != "" {
		abs, err := filepath.Abs(cfg.Workspace)
		if err != nil {
			return fmt.Errorf("go_solid: Expected absolute path to Workspace %q: %w", cfg.Workspace, err)
		}
		cfg.Workspace = abs
	}
	return nil
}

func (b *Bundler) Registry() *internal.Registry { return b.registry }

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
