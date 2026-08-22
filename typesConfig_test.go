package go_solid

// Types sub-config normalization.
//
// The only real interaction is with rasterization: the boot pass rides on the
// registry walk rasterization performs, so CHECK_BOOT cannot stand alone. An
// explicit request for it errors; the default quietly drops the boot half,
// because the consumer never asked for something undeliverable.

import (
	"testing"

	shared_raster "github.com/lilybw/go-solid/shared/rasterization"
	shared_types "github.com/lilybw/go-solid/shared/types"
)

func TestTypes_NilBecomesTheDefaultCheck(t *testing.T) {
	resetPackageState(t)
	cfg := &Config{Components: t.TempDir()}
	if err := configValidationAndNormalization(cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Types == nil {
		t.Fatal("nil Types should be polyfilled")
	}
	if cfg.Types == shared_types.NIL_TYPES_CONFIG {
		t.Fatal("Types must be a copy, not the shared singleton")
	}
	if cfg.Types.Check != shared_types.CHECK_RUNTIME_AND_BOOT {
		t.Fatalf("Check = %s, want RUNTIME_AND_BOOT", cfg.Types.Check)
	}
}

func TestTypes_UnsetResolvesToTheDefault(t *testing.T) {
	resetPackageState(t)
	cfg := &Config{Components: t.TempDir(), Types: &shared_types.TypesConfig{}}
	if err := configValidationAndNormalization(cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Types.Check != shared_types.DEFAULT_CHECK {
		t.Fatalf("Check = %s, want %s", cfg.Types.Check, shared_types.DEFAULT_CHECK)
	}
}

func TestTypes_ExplicitCheckIsPreserved(t *testing.T) {
	resetPackageState(t)
	for _, mode := range []shared_types.CheckMode{
		shared_types.CHECK_RUNTIME,
		shared_types.CHECK_NEVER,
		shared_types.CHECK_BOOT,
		shared_types.CHECK_RUNTIME_AND_BOOT,
	} {
		cfg := &Config{Components: t.TempDir(), Types: &shared_types.TypesConfig{Check: mode}}
		if err := configValidationAndNormalization(cfg); err != nil {
			t.Fatalf("%s: unexpected error: %v", mode, err)
		}
		if cfg.Types.Check != mode {
			t.Errorf("Check = %s, want %s", cfg.Types.Check, mode)
		}
	}
}

// Rasterization is on by default, so the default boot pass is deliverable.
func TestTypes_BootIsAvailableByDefault(t *testing.T) {
	resetPackageState(t)
	cfg := &Config{Components: t.TempDir()}
	if err := configValidationAndNormalization(cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.Types.Check.AtBoot() {
		t.Fatal("the default check should include the boot pass")
	}
}

// Check is honoured as written. It used to be reconciled with rasterization,
// which meant a disabled rasterization could silently rewrite it.
func TestTypes_CheckSurvivesADisabledRasterization(t *testing.T) {
	resetPackageState(t)
	for _, mode := range []shared_types.CheckMode{
		shared_types.CHECK_BOOT,
		shared_types.CHECK_RUNTIME,
		shared_types.CHECK_RUNTIME_AND_BOOT,
		shared_types.CHECK_NEVER,
	} {
		cfg := &Config{
			Components:    t.TempDir(),
			Rasterization: &shared_raster.RasterizationConfig{Disabled: true},
			Types:         &shared_types.TypesConfig{Check: mode},
		}
		if err := configValidationAndNormalization(cfg); err != nil {
			t.Fatalf("%s: unexpected error: %v", mode, err)
		}
		if cfg.Types.Check != mode {
			t.Errorf("Check = %s, want %s", cfg.Types.Check, mode)
		}
		if cfg.Rasterization.Active() {
			t.Errorf("%s: the boot pass must not switch rasterization back on", mode)
		}
	}
}

// The defaulted Check asks for the boot pass, which must not drag rasterization
// in with it and undo an explicit opt-out.
func TestTypes_DefaultCheckLeavesRasterizationAlone(t *testing.T) {
	resetPackageState(t)
	cases := map[string]*Config{
		"caching disabled":  {Components: t.TempDir(), DisableCaching: true},
		"bundling disabled": {Components: t.TempDir(), Generation: disabledGeneration()},
	}
	for name, cfg := range cases {
		if err := configValidationAndNormalization(cfg); err != nil {
			t.Fatalf("%s: unexpected error: %v", name, err)
		}
		if !cfg.Types.Check.AtBoot() {
			t.Errorf("%s: the default check should still include the boot pass", name)
		}
		if cfg.Rasterization.Active() {
			t.Errorf("%s: rasterization should have stayed off", name)
		}
	}
	if cases["caching disabled"].DisableCaching != true {
		t.Error("DisableCaching must survive")
	}
}

func TestCheckMode_Predicates(t *testing.T) {
	cases := []struct {
		mode             shared_types.CheckMode
		atBoot, atRuntim bool
	}{
		{shared_types.CHECK_BOOT, true, false},
		{shared_types.CHECK_RUNTIME, false, true},
		{shared_types.CHECK_RUNTIME_AND_BOOT, true, true},
		{shared_types.CHECK_NEVER, false, false},
		{shared_types.CHECK_UNSET, false, false},
	}
	for _, c := range cases {
		if c.mode.AtBoot() != c.atBoot {
			t.Errorf("%s.AtBoot() = %v, want %v", c.mode, c.mode.AtBoot(), c.atBoot)
		}
		if c.mode.AtRuntime() != c.atRuntim {
			t.Errorf("%s.AtRuntime() = %v, want %v", c.mode, c.mode.AtRuntime(), c.atRuntim)
		}
	}
}

func TestCheckMode_MarshalsByName(t *testing.T) {
	raw, err := shared_types.CHECK_RUNTIME_AND_BOOT.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}
	if string(raw) != `"RUNTIME_AND_BOOT"` {
		t.Fatalf("MarshalJSON = %s, want \"RUNTIME_AND_BOOT\"", raw)
	}
}
