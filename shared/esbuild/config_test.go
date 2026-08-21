package esbuild

import "testing"

func TestZeroValueNeedsNothingInstalled(t *testing.T) {
	// A config left unset must not quietly depend on node_modules.
	var s SolidConfig
	if s.Runtime != RuntimeInternal {
		t.Errorf("zero value is %v, want RuntimeInternal", s.Runtime)
	}
	if err := s.Validate(""); err != nil {
		t.Errorf("zero value should be valid with no Dependencies: %v", err)
	}
}

func TestNormalizeIsIdempotent(t *testing.T) {
	var s SolidConfig
	s.Normalize()
	first := s
	s.Normalize()
	if s.ModuleName != first.ModuleName || s.HelperPrefix != first.HelperPrefix ||
		s.Runtime != first.Runtime || s.Development != first.Development {
		t.Error("Normalize changed the config on a second call")
	}
	if s.ModuleName != "solid-js/web" || s.HelperPrefix != "_$" {
		t.Errorf("defaults not applied: %+v", s)
	}
}

func TestNormalizeKeepsExplicitValues(t *testing.T) {
	s := SolidConfig{ModuleName: "my/wrapper", HelperPrefix: "$$"}
	s.Normalize()
	if s.ModuleName != "my/wrapper" || s.HelperPrefix != "$$" {
		t.Errorf("explicit values overwritten: %+v", s)
	}
}

func TestExternalRequiresDependencies(t *testing.T) {
	s := SolidConfig{Runtime: RuntimeExternal}
	if err := s.Validate(""); err == nil {
		t.Error("EXTERNAL with no Dependencies should be rejected")
	}
	if err := s.Validate("/proj"); err != nil {
		t.Errorf("EXTERNAL with Dependencies should be valid: %v", err)
	}
}

func TestOverrideWithExternalIsRejected(t *testing.T) {
	// The overrides would be silently ignored, which is worse than an error.
	s := SolidConfig{
		Runtime:         RuntimeExternal,
		RuntimeOverride: map[string]string{"solid-js": "x"},
	}
	if err := s.Validate("/proj"); err == nil {
		t.Error("override + EXTERNAL should be rejected")
	}
}

func TestOverrideWithInternalIsFine(t *testing.T) {
	s := SolidConfig{RuntimeOverride: map[string]string{"solid-js": "x"}}
	if err := s.Validate(""); err != nil {
		t.Errorf("override + INTERNAL should be valid: %v", err)
	}
}

func TestUnknownRuntimeIsRejected(t *testing.T) {
	s := SolidConfig{Runtime: RuntimeSource(42)}
	if err := s.Validate("/proj"); err == nil {
		t.Error("an unknown Runtime value should be rejected")
	}
}

func TestRuntimeNames(t *testing.T) {
	if RuntimeInternal.String() != "INTERNAL" || RuntimeExternal.String() != "EXTERNAL" {
		t.Errorf("got %q and %q", RuntimeInternal, RuntimeExternal)
	}
}

// Development must not be coupled to Minify: both combinations are legitimate.
func TestDevelopmentIsIndependent(t *testing.T) {
	for _, tc := range []struct{ minify, dev bool }{
		{true, true}, {true, false}, {false, true}, {false, false},
	} {
		c := BundlerConfig{Minify: tc.minify, Solid: SolidConfig{Development: tc.dev}}
		c.Solid.Normalize()
		if err := c.Solid.Validate("/p"); err != nil {
			t.Errorf("minify=%v dev=%v rejected: %v", tc.minify, tc.dev, err)
		}
		if c.Solid.Development != tc.dev {
			t.Errorf("Development was altered by normalization")
		}
	}
}
