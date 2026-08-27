package shim_d

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	static_int "github.com/lilybw/go-solid/internal/static"
	"github.com/lilybw/go-solid/shared/meta"
	"github.com/lilybw/go-solid/shared/registry"
	shared_types "github.com/lilybw/go-solid/shared/types"
)

// ---------------------------------------------------------------------------
// The switchboard.
//
// A feature that is off still has to be present. A component imports
// "go-solid/static" unconditionally, so the module has to resolve whether or
// not anyone configured assets — otherwise the consumer meets a bundler error
// about a file they never wrote, instead of a compiler error naming the setting
// to change.
// ---------------------------------------------------------------------------

func TestSwitchedOff_TheModuleStillResolvesAndExplainsItself(t *testing.T) {
	p := newProject(t)
	p.boot(t, options{})

	module := p.generated(t, "modules/static/assets.ts")
	if !strings.Contains(module, "export default") {
		t.Errorf("the placeholder exports nothing, so importing it fails to resolve:\n%s", module)
	}

	index := p.generated(t, "modules/static/index.ts")
	for _, want := range []string{"@deprecated", "Config.Static.Location"} {
		if !strings.Contains(index, want) {
			t.Errorf("the placeholder index is missing %q:\n%s", want, index)
		}
	}
	if !strings.Contains(module, "FeatureDisabled<") {
		t.Errorf("the placeholder graph carries no reason:\n%s", module)
	}
	// The reason has to name the fix. A message that only says "disabled" sends
	// the reader looking.
	if !strings.Contains(module, static_int.DISABLED_REASON) {
		t.Error("the definition does not carry the reason the feature is off")
	}
}

func TestSwitchedOn_TheModuleCarriesTheAssets(t *testing.T) {
	p := newProject(t)
	b := p.boot(t, options{static: true})

	module := p.generated(t, "modules/static/assets.ts")
	for _, want := range []string{"images:", "icons:", "tick:", "styles:", "theme:", "data:", "config:"} {
		if !strings.Contains(module, want) {
			t.Errorf("the module is missing %q:\n%s", want, module)
		}
	}
	if strings.Contains(module, "FeatureDisabled") {
		t.Error("the placeholder survived into an enabled build")
	}

	// The module is TypeScript, so the media type travels on the value itself
	// rather than in a separate declaration that has to be kept in step.
	for _, want := range []string{`AssetURL<"image/svg+xml">`, `AssetURL<"application/json">`, `AssetURL<"text/css">`} {
		if !strings.Contains(module, want) {
			t.Errorf("the graph is missing %q:\n%s", want, module)
		}
	}

	// The Go side reads the same manifest the browser does.
	if url := b.Static().URL("images/logo.svg"); !strings.Contains(module, url) {
		t.Errorf("Static().URL gave %q, which the module does not contain", url)
	}
}

// Switching a feature on is a per-Bundler decision, and so is the head
// template. Two Bundlers in one program must not read each other's.
func TestTwoBundlersKeepTheirOwnFeatures(t *testing.T) {
	withAssets := newProject(t)
	withAssets.boot(t, options{static: true, headTitle: "with"})

	without := newProject(t)
	b := without.boot(t, options{headTitle: "without"})

	if b.Static() != nil {
		t.Error("a Bundler with no static config picked up another's manifest")
	}
	disabledStr := "FeatureDisabled<\"" + static_int.DISABLED_REASON + "\">"
	if module := without.generated(t, "modules/static/assets.ts"); !strings.Contains(module, disabledStr) {
		t.Errorf("the second Bundler's placeholder was overwritten:\n%s", module)
	}
	if module := withAssets.generated(t, "modules/static/assets.ts"); strings.Contains(module, disabledStr) {
		t.Errorf("the first Bundler's module was replaced by the second's placeholder:\n%s", module)
	}
}

// ---------------------------------------------------------------------------
// The asset graph.
// ---------------------------------------------------------------------------

// logo.svg beside logo.png is ordinary. Extensions become subfields rather than
// one of the two winning the key.
func TestSharedStemBecomesSubfields(t *testing.T) {
	p := newProject(t)
	p.boot(t, options{static: true})

	module := p.generated(t, "modules/static/assets.ts")
	if !strings.Contains(module, "logo: {") {
		t.Errorf("logo.svg and logo.png did not become subfields:\n%s", module)
	}
	for _, want := range []string{"png:", "svg:"} {
		if !strings.Contains(module, want) {
			t.Errorf("the subfield %q is missing:\n%s", want, module)
		}
	}
}

// A filename becomes a key by replacing whatever an identifier cannot hold. The
// reader should be able to derive one from the other without knowing more.
func TestFilenamesBecomePredictableKeys(t *testing.T) {
	p := newProject(t)
	b := p.boot(t, options{static: true})

	if !strings.Contains(p.generated(t, "modules/static/assets.ts"), "hero_shot:") {
		t.Error("hero-shot.png did not become hero_shot")
	}
	// The manifest still addresses it by the name on disk, which is what a Go
	// caller has in hand.
	if b.Static().URL("images/hero-shot.png") == "" {
		t.Error("the asset is not addressable by its real path")
	}
}

func TestLeavingsAreNotAssets(t *testing.T) {
	p := newProject(t)
	b := p.boot(t, options{static: true})

	for _, rel := range []string{".DS_Store", "images/.gitkeep"} {
		if b.Static().URL(rel) != "" {
			t.Errorf("%q was published as an asset", rel)
		}
	}
	if strings.Contains(p.generated(t, "modules/static/assets.ts"), "DS_Store") {
		t.Error("editor leavings reached the generated module")
	}
}

// ---------------------------------------------------------------------------
// Boot composition.
// ---------------------------------------------------------------------------

// The published surface holds what go_solid synthesises and nothing derived
// from a component. Turning features on adds to it; it never starts holding
// component-shaped files.
func TestPublishedSurfaceHoldsOnlySynthesisedDefinitions(t *testing.T) {
	p := newProject(t)
	b := p.boot(t, options{static: true, check: shared_types.CHECK_BOOT})

	entries, err := os.ReadDir(filepath.Join(p.workspace, shared_types.TYPES_DIR_NAME))
	if err != nil {
		t.Fatalf("published surface missing: %v", err)
	}
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
		for _, component := range b.Registry().Map(func(k meta.QualifiedName, _ *registry.Component) meta.QualifiedName { return k }) {
			if strings.Contains(entry.Name(), filepath.Base(component)) {
				t.Errorf("%q is derived from the component %q", entry.Name(), component)
			}
		}
	}
	sort.Strings(names)
	// Generated modules are not published definitions: each is a self-contained
	// library under .go_solid/modules, typed by its own source. Nothing static
	// contributes here.
	if len(names) != 0 {
		t.Errorf("published surface = %v, want nothing", names)
	}
}

// A helper module in the components tree is a registry entry by extension and
// backs no component. Boot-time type checking walks every entry, so it has to
// walk past that one rather than fail over it.
func TestHelperModulesDoNotBreakBoot(t *testing.T) {
	p := newProject(t)
	b := p.boot(t, options{static: true, check: shared_types.CHECK_RUNTIME_AND_BOOT})

	if _, ok := b.Registry().Lookup("shared/tokens"); !ok {
		t.Fatal("a .ts helper was not registered")
	}
	// Addressable, and still not renderable.
	assertStops(t, b, "shared/tokens", nil, "no default export")
}

// Rasterization defaults on, and pre-renders every registered component. With
// bundling off it has nothing to do, so it has to stand down rather than fail
// the boot it cannot complete.
func TestRasterizationStandsDownWhenBundlingIsOff(t *testing.T) {
	p := newProject(t)
	b := p.boot(t, options{static: true})

	// Booting at all is the assertion; this confirms the Bundler is usable.
	if b.Registry() == nil {
		t.Fatal("no registry")
	}
}
