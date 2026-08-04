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
	"time"

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
	if cfg.Generation.NodeBin != "node" {
		t.Fatalf("NodeBin = %q, want node", cfg.Generation.NodeBin)
	}
	if cfg.Generation.PoolSize != 1 {
		t.Fatalf("PoolSize = %d, want 1", cfg.Generation.PoolSize)
	}
}

func TestGeneration_PartialNodeBinDefault(t *testing.T) {
	resetPackageState(t)
	comps := t.TempDir()
	cfg := &Config{Components: comps, Generation: &shared_esbuild.BundlerConfig{PoolSize: 3}}
	if err := configValidationAndNormalization(cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Generation.NodeBin != "node" {
		t.Fatalf("NodeBin = %q, want node", cfg.Generation.NodeBin)
	}
	if cfg.Generation.PoolSize != 3 {
		t.Fatalf("PoolSize = %d, want preserved 3", cfg.Generation.PoolSize)
	}
}

func TestGeneration_PoolSizeZeroAndNegativeDefaultToOne(t *testing.T) {
	resetPackageState(t)
	for _, ps := range []int{0, -5} {
		comps := t.TempDir()
		cfg := &Config{Components: comps, Generation: &shared_esbuild.BundlerConfig{PoolSize: ps}}
		if err := configValidationAndNormalization(cfg); err != nil {
			t.Fatalf("ps=%d: unexpected error: %v", ps, err)
		}
		if cfg.Generation.PoolSize != 1 {
			t.Fatalf("ps=%d: PoolSize = %d, want 1", ps, cfg.Generation.PoolSize)
		}
	}
}

func TestGeneration_PoolSizePositivePreserved(t *testing.T) {
	resetPackageState(t)
	comps := t.TempDir()
	cfg := &Config{Components: comps, Generation: &shared_esbuild.BundlerConfig{PoolSize: 8}}
	if err := configValidationAndNormalization(cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Generation.PoolSize != 8 {
		t.Fatalf("PoolSize = %d, want 8", cfg.Generation.PoolSize)
	}
}

func TestGeneration_ExplicitNodeBinPreserved(t *testing.T) {
	resetPackageState(t)
	comps := t.TempDir()
	cfg := &Config{Components: comps, Generation: &shared_esbuild.BundlerConfig{NodeBin: "/usr/local/bin/node20"}}
	if err := configValidationAndNormalization(cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Generation.NodeBin != "/usr/local/bin/node20" {
		t.Fatalf("NodeBin = %q, want preserved", cfg.Generation.NodeBin)
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

func TestGeneration_TimeoutNotNormalized(t *testing.T) {
	resetPackageState(t)
	comps := t.TempDir()
	// Timeout is not polyfilled here (the pool treats 0 as 30s at runtime).
	cfg := &Config{Components: comps, Generation: &shared_esbuild.BundlerConfig{Timeout: 0}}
	if err := configValidationAndNormalization(cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Generation.Timeout != time.Duration(0) {
		t.Fatalf("Timeout = %v, want left at 0", cfg.Generation.Timeout)
	}
}
