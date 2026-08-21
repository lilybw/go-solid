package go_solid

// Shared helpers for the configValidationAndNormalization / New test suites.
//
// These tests avoid bundling: Generation.Disabled = true skips esbuild and the
// solid-js resolution gate, while still exercising all of
// configValidationAndNormalization, the registry walk, the caches, the reactive
// registry, and (optionally) HMR.

import (
	"os"
	"path/filepath"
	"testing"

	shared_esbuild "github.com/lilybw/go-solid/shared/esbuild"
	shared_hmr "github.com/lilybw/go-solid/shared/hmr"
	shared_raster "github.com/lilybw/go-solid/shared/rasterization"
	shared_static "github.com/lilybw/go-solid/shared/static"
)

// resetPackageState is a tripwire on the null-object singletons in the shared/*
// packages.
//
// configValidationAndNormalization hands every Config its own copy of each NIL_*
// object (meta.Copy), so a mutation observed here is a regression back to
// aliasing — where one Config's normalization silently rewrote the defaults for
// every other Config in the process. Values are restored either way so a single
// regression does not cascade through the rest of the suite.
//
// Not covered: the networking head/request templates set by
// SetHTMLHeadSegmentTemplate / SetRequestBehaviourTemplate, which are only
// touched when Defaults was supplied by the consumer.
func resetPackageState(t *testing.T) {
	t.Helper()

	hmrSnap := *shared_hmr.NIL_HMR_CONFIG
	rasterSnap := *shared_raster.NIL_RASTERIZATION_CONFIG
	staticSnap := *shared_static.NIL_STATIC_CONFIG
	genSnap := *shared_esbuild.NIL_BUNDLER_CONFIG
	behSnap := *NIL_BEHAVIOURAL_DEFAULTS

	t.Cleanup(func() {
		if *shared_raster.NIL_RASTERIZATION_CONFIG != rasterSnap {
			t.Error("NIL_RASTERIZATION_CONFIG was mutated; normalization must copy it")
		}
		if shared_hmr.NIL_HMR_CONFIG.Disabled != hmrSnap.Disabled || shared_hmr.NIL_HMR_CONFIG.Path != hmrSnap.Path {
			t.Error("NIL_HMR_CONFIG was mutated; normalization must copy it")
		}
		if shared_static.NIL_STATIC_CONFIG.Location != staticSnap.Location || len(shared_static.NIL_STATIC_CONFIG.Ignore) != len(staticSnap.Ignore) {
			t.Error("NIL_STATIC_CONFIG was mutated; normalization must copy it")
		}

		*shared_hmr.NIL_HMR_CONFIG = hmrSnap
		*shared_raster.NIL_RASTERIZATION_CONFIG = rasterSnap
		*shared_static.NIL_STATIC_CONFIG = staticSnap
		*shared_esbuild.NIL_BUNDLER_CONFIG = genSnap
		*NIL_BEHAVIOURAL_DEFAULTS = behSnap
	})
}

// stageSolidRuntime creates the node_modules layout PeerDepsMissing looks for,
// so New() gets past the solid-js resolution gate. It writes into
// dir/node_modules. No Node runtime or Babel packages are involved.
func stageSolidRuntime(t *testing.T, dir string) {
	t.Helper()
	p := filepath.Join(dir, "node_modules", "solid-js")
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatalf("stageSolidRuntime: mkdir %q: %v", p, err)
	}
}

// componentsDirWith creates a fresh components root under a temp dir, writes the
// given component files (relative paths -> contents), stages the solid-js
// runtime, and returns the absolute components root.
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
	stageSolidRuntime(t, comps)
	return comps
}

// disabledGeneration returns a BundlerConfig whose Disabled flag skips esbuild
// and the solid-js resolution gate. Use this in New() tests that do not render.
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
