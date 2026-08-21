package go_solid

// Generation (BundlerConfig) field-by-field polyfilling.
//
// When Generation == nil, configValidationAndNormalization installs a COPY of
// NIL_BUNDLER_CONFIG and sets Dependencies on it. The copy matters: the old
// code aliased the singleton and wrote Dependencies straight through it,
// corrupting the default for every later Config in the process.
//
// When Generation != nil, only Dependencies is polyfilled ("" -> Components).
// Minify and Sourcemap are deliberately left as the consumer set them.

import (
	"testing"

	shared_esbuild "github.com/lilybw/go-solid/shared/esbuild"
)

func TestGeneration_NilCopiesSharedSingleton(t *testing.T) {
	resetPackageState(t)
	comps := t.TempDir()
	cfg := &Config{Components: comps}
	if err := configValidationAndNormalization(cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Generation == shared_esbuild.NIL_BUNDLER_CONFIG {
		t.Fatal("nil Generation must be a copy, not the shared singleton")
	}
	if cfg.Generation.Dependencies != cfg.Components {
		t.Fatalf("Dependencies = %q, want Components %q", cfg.Generation.Dependencies, cfg.Components)
	}
	if shared_esbuild.NIL_BUNDLER_CONFIG.Dependencies != "" {
		t.Fatalf("writing Dependencies leaked into the singleton: %q", shared_esbuild.NIL_BUNDLER_CONFIG.Dependencies)
	}
}

func TestGeneration_PartialConfigGetsDependencies(t *testing.T) {
	resetPackageState(t)
	comps := t.TempDir()
	cfg := &Config{Components: comps, Generation: &shared_esbuild.BundlerConfig{}}
	if err := configValidationAndNormalization(cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Generation.Dependencies != cfg.Components {
		t.Fatalf("Dependencies = %q, want Components %q", cfg.Generation.Dependencies, cfg.Components)
	}
}

func TestGeneration_MinifyAndSourcemapNotForcedByNormalization(t *testing.T) {
	resetPackageState(t)
	comps := t.TempDir()
	// The function explicitly does NOT touch Minify or Sourcemap on a provided
	// Generation. A user who leaves Minify=false keeps false, even though the
	// NIL singleton defaults Minify=true.
	cfg := &Config{Components: comps, Generation: &shared_esbuild.BundlerConfig{}}
	if err := configValidationAndNormalization(cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Generation.Minify != false {
		t.Fatalf("Minify = %v, want false (normalization must not force it)", cfg.Generation.Minify)
	}
}
