package go_solid

// Shared helpers for the configValidationAndNormalization / New test suites.
//
// These tests deliberately avoid spawning real Node workers. The worker pool is
// only spawned when Generation.Disabled is false (see workers.NewPool), so every
// integration test that reaches New() sets Generation.Disabled = true. That path
// still exercises all of configValidationAndNormalization, the registry walk, the
// caches, the reactive registry, and (optionally) HMR — everything except the
// Node process itself.

import (
	"os"
	"path/filepath"
	"testing"

	shared_esbuild "github.com/lilybw/go-solid/shared/esbuild"
	shared_hmr "github.com/lilybw/go-solid/shared/hmr"
	shared_raster "github.com/lilybw/go-solid/shared/rasterization"
	shared_static "github.com/lilybw/go-solid/shared/static"
)

// resetPackageState restores the mutable package-level globals that New() and
// configValidationAndNormalization touch, so tests don't leak into one another.
//
// Three distinct kinds of shared state are in play:
//  1. The NIL_* null-object singletons in the shared/* packages. Because
//     configValidationAndNormalization stores pointers to these singletons on
//     the Config AND New() later mutates fields through those pointers
//     (e.g. cfg.HMR.Disabled = true), a careless test can permanently corrupt
//     the singleton for every later test. We snapshot and restore them.
//  2. The esbuild peer-dep worker-script memo (parsedEmbeddedWorkerScript) —
//     not resettable from this package, but idempotent, so harmless.
//  3. The networking head/request templates set by SetHTMLHeadSegmentTemplate /
//     SetRequestBehaviourTemplate — only touched when Defaults != NIL.
func resetPackageState(t *testing.T) {
	t.Helper()

	hmrSnap := *shared_hmr.NIL_HMR_CONFIG
	rasterSnap := *shared_raster.NIL_RASTERIZATION_CONFIG
	staticSnap := *shared_static.NIL_STATIC_CONFIG
	genSnap := *shared_esbuild.NIL_BUNDLER_CONFIG
	behSnap := *NIL_BEHAVIOURAL_DEFAULTS

	t.Cleanup(func() {
		*shared_hmr.NIL_HMR_CONFIG = hmrSnap
		*shared_raster.NIL_RASTERIZATION_CONFIG = rasterSnap
		*shared_static.NIL_STATIC_CONFIG = staticSnap
		*shared_esbuild.NIL_BUNDLER_CONFIG = genSnap
		*NIL_BEHAVIOURAL_DEFAULTS = behSnap
	})
}

// stagePeerDeps creates the node_modules layout that PeerDepsMissing looks for,
// so New() gets past the peer-dependency gate. It writes into dir/node_modules.
func stagePeerDeps(t *testing.T, dir string) {
	t.Helper()
	for _, pkg := range []string{"solid-js", "babel-preset-solid", filepath.Join("@babel", "core")} {
		p := filepath.Join(dir, "node_modules", pkg)
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatalf("stagePeerDeps: mkdir %q: %v", p, err)
		}
	}
}

// componentsDirWith creates a fresh components root under a temp dir, writes the
// given component files (relative paths -> contents), stages peer deps, and
// returns the absolute components root.
func componentsDirWith(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	comps := filepath.Join(root, "components")
	if err := os.MkdirAll(comps, 0o755); err != nil {
		t.Fatalf("componentsDirWith: mkdir: %v", err)
	}
	for rel, body := range files {
		full := filepath.Join(comps, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("componentsDirWith: mkdir %q: %v", filepath.Dir(full), err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatalf("componentsDirWith: write %q: %v", full, err)
		}
	}
	stagePeerDeps(t, comps)
	return comps
}

// disabledGeneration returns a BundlerConfig whose Disabled flag prevents any
// Node worker from spawning. Use this in every New() integration test.
func disabledGeneration() *shared_esbuild.BundlerConfig {
	return &shared_esbuild.BundlerConfig{
		Disabled: true,
	}
}

// mustAbs is filepath.Abs with a test failure on error.
func mustAbs(t *testing.T, p string) string {
	t.Helper()
	a, err := filepath.Abs(p)
	if err != nil {
		t.Fatalf("mustAbs(%q): %v", p, err)
	}
	return a
}
