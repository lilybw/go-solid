package go_solid

// Path handling in configValidationAndNormalization:
//   - Components required, resolved to absolute
//   - Workspace: absolute-resolved if set, else <Components>/.go_solid; created via MkdirAll
//   - Generation.Dependencies: defaults to Components, resolved to absolute
//   - Generation.ScriptLocation: materialized from the embedded worker script when empty
//
// configValidationAndNormalization is unexported but these tests live in the
// same package (package go_solid), so they call it directly.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	shared_esbuild "github.com/lilybw/go-solid/shared/esbuild"
)

func TestConfig_ComponentsRequired(t *testing.T) {
	resetPackageState(t)
	err := configValidationAndNormalization(&Config{})
	if err == nil {
		t.Fatal("expected error when Components is empty, got nil")
	}
	if !strings.Contains(err.Error(), "ComponentsDir is required") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestConfig_ComponentsResolvedToAbsolute(t *testing.T) {
	resetPackageState(t)
	// Use a relative path and confirm it becomes absolute.
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	// Create a real relative dir under the working dir so MkdirAll(workspace) works.
	rel := "reltestcomp"
	abs := filepath.Join(wd, rel)
	if err := os.MkdirAll(abs, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(abs) })

	cfg := &Config{Components: rel}
	if err := configValidationAndNormalization(cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !filepath.IsAbs(cfg.Components) {
		t.Fatalf("Components not absolute: %q", cfg.Components)
	}
	if cfg.Components != abs {
		t.Fatalf("Components = %q, want %q", cfg.Components, abs)
	}
}

func TestConfig_WorkspaceDefaultsUnderComponents(t *testing.T) {
	resetPackageState(t)
	comps := t.TempDir()
	cfg := &Config{Components: comps}
	if err := configValidationAndNormalization(cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := filepath.Join(comps, ".go_solid")
	if cfg.Workspace != want {
		t.Fatalf("Workspace = %q, want %q", cfg.Workspace, want)
	}
	// MkdirAll must have created it.
	info, err := os.Stat(cfg.Workspace)
	if err != nil {
		t.Fatalf("workspace not created: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("workspace is not a directory")
	}
}

func TestConfig_WorkspaceExplicitResolvedToAbsolute(t *testing.T) {
	resetPackageState(t)
	comps := t.TempDir()
	wd, _ := os.Getwd()
	rel := "relworkspace"
	absWorkspace := filepath.Join(wd, rel)
	t.Cleanup(func() { os.RemoveAll(absWorkspace) })

	cfg := &Config{Components: comps, Workspace: rel}
	if err := configValidationAndNormalization(cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Workspace != absWorkspace {
		t.Fatalf("Workspace = %q, want %q", cfg.Workspace, absWorkspace)
	}
	if _, err := os.Stat(cfg.Workspace); err != nil {
		t.Fatalf("explicit workspace not created: %v", err)
	}
}

func TestConfig_WorkspaceCreationFailsOnFileCollision(t *testing.T) {
	resetPackageState(t)
	comps := t.TempDir()
	// Put a regular FILE where the default workspace directory would go, so
	// MkdirAll fails.
	wsPath := filepath.Join(comps, ".go_solid")
	if err := os.WriteFile(wsPath, []byte("i am a file"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &Config{Components: comps}
	err := configValidationAndNormalization(cfg)
	if err == nil {
		t.Fatal("expected workspace creation error, got nil")
	}
	if !strings.Contains(err.Error(), "create workspace") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestConfig_DependenciesDefaultToComponents(t *testing.T) {
	resetPackageState(t)
	comps := t.TempDir()
	cfg := &Config{Components: comps}
	if err := configValidationAndNormalization(cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Generation was nil -> set to NIL_BUNDLER_CONFIG whose Dependencies == "",
	// which matches NIL_BUNDLER_CONFIG.Dependencies... but the nil branch does
	// NOT run the "== NIL.Dependencies -> Components" assignment (that's only in
	// the else branch). However the later filepath.Abs("") resolves to the cwd.
	// This test pins the ACTUAL behavior so a refactor that changes it is caught.
	//
	// Because Generation was nil, Dependencies started "" and was made absolute
	// via filepath.Abs(""), i.e. the current working directory — NOT Components.
	wd, _ := os.Getwd()
	if cfg.Generation.Dependencies != wd {
		t.Fatalf("Dependencies = %q, want cwd %q (nil-Generation path)", cfg.Generation.Dependencies, wd)
	}
}

func TestConfig_DependenciesDefaultToComponents_WhenGenerationProvided(t *testing.T) {
	resetPackageState(t)
	comps := t.TempDir()
	// Generation provided but Dependencies empty -> else branch sets it to Components.
	cfg := &Config{Components: comps, Generation: &shared_esbuild.BundlerConfig{}}
	if err := configValidationAndNormalization(cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Generation.Dependencies != comps {
		t.Fatalf("Dependencies = %q, want Components %q", cfg.Generation.Dependencies, comps)
	}
}

func TestConfig_DependenciesExplicitResolvedToAbsolute(t *testing.T) {
	resetPackageState(t)
	comps := t.TempDir()
	wd, _ := os.Getwd()
	cfg := &Config{
		Components: comps,
		Generation: &shared_esbuild.BundlerConfig{Dependencies: "somerel"},
	}
	if err := configValidationAndNormalization(cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := filepath.Join(wd, "somerel")
	if cfg.Generation.Dependencies != want {
		t.Fatalf("Dependencies = %q, want %q", cfg.Generation.Dependencies, want)
	}
}

func TestConfig_ScriptLocationMaterializedWhenEmpty(t *testing.T) {
	resetPackageState(t)
	comps := t.TempDir()
	cfg := &Config{Components: comps}
	if err := configValidationAndNormalization(cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Generation.ScriptLocation == "" {
		t.Fatal("ScriptLocation was not materialized")
	}
	base := filepath.Base(cfg.Generation.ScriptLocation)
	if !strings.HasPrefix(base, "transform-worker.") || !strings.HasSuffix(base, ".mjs") {
		t.Fatalf("materialized script has unexpected name: %q", base)
	}
	if _, err := os.Stat(cfg.Generation.ScriptLocation); err != nil {
		t.Fatalf("materialized script not on disk: %v", err)
	}
}

func TestConfig_ScriptLocationExplicitPreserved(t *testing.T) {
	resetPackageState(t)
	comps := t.TempDir()
	explicit := filepath.Join(comps, "my-worker.mjs")
	cfg := &Config{
		Components: comps,
		Generation: &shared_esbuild.BundlerConfig{ScriptLocation: explicit},
	}
	if err := configValidationAndNormalization(cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Generation.ScriptLocation != explicit {
		t.Fatalf("ScriptLocation = %q, want explicit %q", cfg.Generation.ScriptLocation, explicit)
	}
}
