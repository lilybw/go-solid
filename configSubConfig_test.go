package go_solid

// Sub-config normalization: Defaults, HMR, Static, Rasterization.
//
// The most consequential branch is Rasterization with ExpectCompleted: it forces
// DisableCaching=false, defaults Location to Workspace, and mutates
// cfg.HMR.Disabled=true and cfg.Generation.Disabled=true. Those writes land on
// the Config's own copy of each null object, never on the shared singleton;
// resetPackageState asserts that invariant holds.

import (
	"os"
	"path/filepath"
	"testing"

	shared_hmr "github.com/lilybw/go-solid/shared/hmr"
	shared_net "github.com/lilybw/go-solid/shared/networking"
	shared_raster "github.com/lilybw/go-solid/shared/rasterization"
	shared_static "github.com/lilybw/go-solid/shared/static"
)

// --- Defaults ---------------------------------------------------------------

func TestDefaults_NilBecomesNilBehaviouralDefaults(t *testing.T) {
	resetPackageState(t)
	comps := t.TempDir()
	cfg := &Config{Components: comps}
	if err := configValidationAndNormalization(cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Defaults == nil {
		t.Fatal("nil Defaults should be polyfilled")
	}
	if cfg.Defaults == NIL_BEHAVIOURAL_DEFAULTS {
		t.Fatal("Defaults must be a copy, not the shared singleton")
	}
	if cfg.Defaults.HeadSegment == nil || cfg.Defaults.Requests == nil {
		t.Fatal("copied Defaults should carry the noop configurators")
	}
}

func TestDefaults_PartialHeadSegmentFilled(t *testing.T) {
	resetPackageState(t)
	comps := t.TempDir()
	// Provide Requests but leave HeadSegment nil -> HeadSegment gets the noop.
	called := false
	cfg := &Config{
		Components: comps,
		Defaults: &BehaviouralDefaults{
			Requests: func(b shared_net.RequestBehaviourBuilder) { called = true },
		},
	}
	if err := configValidationAndNormalization(cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Defaults.HeadSegment == nil {
		t.Fatal("HeadSegment should have been polyfilled to the noop")
	}
	// The configurators are not INVOKED by configValidationAndNormalization; they
	// are only invoked later in New(). So called must still be false here.
	if called {
		t.Fatal("Requests configurator must not be invoked during normalization")
	}
}

func TestDefaults_PartialRequestsFilled(t *testing.T) {
	resetPackageState(t)
	comps := t.TempDir()
	cfg := &Config{
		Components: comps,
		Defaults: &BehaviouralDefaults{
			HeadSegment: func(b shared_net.HTMLHeadSegmentBuilder) {},
		},
	}
	if err := configValidationAndNormalization(cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Defaults.Requests == nil {
		t.Fatal("Requests should have been polyfilled to the noop")
	}
}

// --- HMR --------------------------------------------------------------------

func TestHMR_NilBecomesNilHMRConfig(t *testing.T) {
	resetPackageState(t)
	comps := t.TempDir()
	cfg := &Config{Components: comps}
	if err := configValidationAndNormalization(cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.HMR == nil {
		t.Fatal("nil HMR should be polyfilled")
	}
	if cfg.HMR == shared_hmr.NIL_HMR_CONFIG {
		t.Fatal("HMR must be a copy, not the shared singleton")
	}
	if !cfg.HMR.Disabled {
		t.Fatal("a copy of NIL_HMR_CONFIG should be Disabled")
	}
	if cfg.HMR.Path != shared_hmr.NIL_HMR_CONFIG.Path {
		t.Fatalf("Path = %q, want the null object's %q", cfg.HMR.Path, shared_hmr.NIL_HMR_CONFIG.Path)
	}
}

func TestHMR_ProvidedConfigPreserved(t *testing.T) {
	resetPackageState(t)
	comps := t.TempDir()
	provided := &shared_hmr.HMRConfig{Disabled: false, Path: "/custom"}
	cfg := &Config{Components: comps, HMR: provided}
	if err := configValidationAndNormalization(cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.HMR != provided {
		t.Fatal("provided HMR config should be preserved as-is")
	}
	if cfg.HMR.Path != "/custom" {
		t.Fatalf("Path = %q, want /custom", cfg.HMR.Path)
	}
}

// --- Static -----------------------------------------------------------------

func TestStatic_NilBecomesNilStaticConfig(t *testing.T) {
	resetPackageState(t)
	comps := t.TempDir()
	cfg := &Config{Components: comps}
	if err := configValidationAndNormalization(cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Static == nil {
		t.Fatal("nil Static should be polyfilled")
	}
	if cfg.Static == shared_static.NIL_STATIC_CONFIG {
		t.Fatal("Static must be a copy, not the shared singleton")
	}
	if cfg.Static.Location != "" {
		t.Fatalf("Location = %q, want empty", cfg.Static.Location)
	}
	// meta.Copy is shallow, so Ignore must have been cloned too.
	if cfg.Static.Ignore == nil {
		t.Fatal("Ignore should be a non-nil empty slice")
	}
	cfg.Static.Ignore = append(cfg.Static.Ignore, "*.tmp")
	if len(shared_static.NIL_STATIC_CONFIG.Ignore) != 0 {
		t.Fatal("appending to the copy leaked into NIL_STATIC_CONFIG.Ignore")
	}
}

// --- Rasterization ----------------------------------------------------------

func TestRaster_NilBecomesNilRasterConfig(t *testing.T) {
	resetPackageState(t)
	comps := t.TempDir()
	cfg := &Config{Components: comps}
	if err := configValidationAndNormalization(cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Rasterization == nil {
		t.Fatal("nil Rasterization should be polyfilled")
	}
	if cfg.Rasterization == shared_raster.NIL_RASTERIZATION_CONFIG {
		t.Fatal("Rasterization must be a copy, not the shared singleton")
	}
	if *cfg.Rasterization != *shared_raster.NIL_RASTERIZATION_CONFIG {
		t.Fatal("the copy should equal the null object by value")
	}
}

func TestRaster_ProvidedNotCompletedForcesCachingAndDefaultsLocation(t *testing.T) {
	resetPackageState(t)
	comps := t.TempDir()
	cfg := &Config{
		Components:     comps,
		DisableCaching: true, // must be flipped to false by rasterization
		Rasterization:  &shared_raster.RasterizationConfig{ExpectCompleted: false},
	}
	if err := configValidationAndNormalization(cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.DisableCaching {
		t.Fatal("Rasterization must force DisableCaching=false")
	}
	// Location defaulted to Workspace.
	if cfg.Rasterization.Location != cfg.Workspace {
		t.Fatalf("Location = %q, want Workspace %q", cfg.Rasterization.Location, cfg.Workspace)
	}
}

func TestRaster_ProvidedLocationPreserved(t *testing.T) {
	resetPackageState(t)
	comps := t.TempDir()
	loc := t.TempDir()
	cfg := &Config{
		Components:    comps,
		Rasterization: &shared_raster.RasterizationConfig{Location: loc, ExpectCompleted: false},
	}
	if err := configValidationAndNormalization(cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Rasterization.Location != loc {
		t.Fatalf("Location = %q, want preserved %q", cfg.Rasterization.Location, loc)
	}
}

func TestRaster_ExpectCompletedValidationFailsOnEmptyLocation(t *testing.T) {
	resetPackageState(t)
	comps := t.TempDir()
	// ExpectCompleted with a Workspace that has no component_cache must fail
	// ExpectCompletedValidationCheck.
	cfg := &Config{
		Components:    comps,
		Rasterization: &shared_raster.RasterizationConfig{ExpectCompleted: true},
	}
	err := configValidationAndNormalization(cfg)
	if err == nil {
		t.Fatal("expected ExpectCompleted validation to fail on empty workspace")
	}
}

// stageRasterizedWorkspace builds a workspace that passes ExpectCompletedValidationCheck:
// a component_cache dir with one valid manifest + its referenced HTML and JS
// artifacts.
func stageRasterizedWorkspace(t *testing.T, ws string) {
	t.Helper()
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatal(err)
	}
	cacheDir := filepath.Join(ws, "component_cache")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	stem := "auth_LoginForm__abc123"
	if err := os.WriteFile(filepath.Join(cacheDir, stem+".html"), []byte("<html></html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cacheDir, stem+".js"), []byte("export default 0;"), 0o644); err != nil {
		t.Fatal(err)
	}
	manifest := `{
  "component": "auth/LoginForm",
  "minify": false,
  "generatedAt": "2024-01-01T00:00:00Z",
  "key": "abcdef",
  "sources": {},
  "artifacts": { "html": "` + stem + `.html", "js": "` + stem + `.js" },
  "serveNames": { "js": "auth_LoginForm.deadbeef.js" }
}`
	if err := os.WriteFile(filepath.Join(cacheDir, stem+".meta.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRaster_ExpectCompletedForcesFlags(t *testing.T) {
	resetPackageState(t)
	comps := t.TempDir()
	ws := t.TempDir()
	stageRasterizedWorkspace(t, ws)

	cfg := &Config{
		Components:       comps,
		Workspace:        ws,
		ReactiveRegistry: true,                                   // must be forced off
		HMR:              &shared_hmr.HMRConfig{Disabled: false}, // must be forced Disabled
		Rasterization:    &shared_raster.RasterizationConfig{Location: ws, ExpectCompleted: true},
	}
	if err := configValidationAndNormalization(cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.HMR.Disabled {
		t.Fatal("ExpectCompleted must force HMR.Disabled=true")
	}
	if cfg.ReactiveRegistry {
		t.Fatal("ExpectCompleted must force ReactiveRegistry=false")
	}
	if !cfg.Generation.Disabled {
		t.Fatal("ExpectCompleted must force Generation.Disabled=true")
	}
}

func TestRaster_ExpectCompletedMutationDoesNotLeakToSharedHMRSingleton(t *testing.T) {
	resetPackageState(t)
	comps := t.TempDir()
	ws := t.TempDir()
	stageRasterizedWorkspace(t, ws)

	// HMR left nil -> becomes a COPY of NIL_HMR_CONFIG. ExpectCompleted then sets
	// cfg.HMR.Disabled = true on that copy. Assert the singleton is untouched, to
	// catch a regression back to aliasing.
	cfg := &Config{
		Components:    comps,
		Workspace:     ws,
		Rasterization: &shared_raster.RasterizationConfig{Location: ws, ExpectCompleted: true},
	}
	if err := configValidationAndNormalization(cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !shared_hmr.NIL_HMR_CONFIG.Disabled {
		t.Fatal("NIL_HMR_CONFIG.Disabled must remain true after rasterization")
	}
}
