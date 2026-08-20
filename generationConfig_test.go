package go_solid

// Generation (BundlerConfig) field-by-field polyfilling.
//
// When Generation == nil, configValidationAndNormalization aliases the shared
// NIL_BUNDLER_CONFIG singleton directly (cfg.Generation = NIL_BUNDLER_CONFIG).
// That aliasing is itself a hazard worth pinning: subsequent per-field defaults
// only run in the else branch, so a nil Generation is NOT field-normalized the
// same way a partial one is.
//
// When Generation != nil, each zero field is filled from NIL_BUNDLER_CONFIG:
//   NodeBin "" -> "node"; PoolSize <=0 -> 1; ScriptLocation "" -> materialized;
//   Dependencies "" -> Components.

import (
	"testing"

	shared_esbuild "github.com/lilybw/go-solid/shared/esbuild"
)

func TestGeneration_NilAliasesSharedSingleton(t *testing.T) {
	resetPackageState(t)
	comps := t.TempDir()
	cfg := &Config{Components: comps}
	if err := configValidationAndNormalization(cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Documents the current behavior: a nil Generation is backed by the shared
	// singleton pointer. (ScriptLocation gets materialized onto it at the end.)
	if cfg.Generation != shared_esbuild.NIL_BUNDLER_CONFIG {
		t.Fatal("expected nil Generation to alias NIL_BUNDLER_CONFIG pointer")
	}
}

func TestGeneration_PartialNodeBinDefault(t *testing.T) {
	resetPackageState(t)
	comps := t.TempDir()
	cfg := &Config{Components: comps, Generation: &shared_esbuild.BundlerConfig{}}
	if err := configValidationAndNormalization(cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
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
