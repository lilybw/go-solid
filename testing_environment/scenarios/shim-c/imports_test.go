package shim_c

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// The bare name is the file's default export.
// ---------------------------------------------------------------------------

// Whatever the default export is called, and however it is written, the file's
// name reaches it. The function's own identifier is not part of the address.
func TestBareNameTakesTheDefaultExport(t *testing.T) {
	b := newProject(t).boot(t)

	for _, selector := range []string{
		"Panel",         // export default function Panel
		"legacy/Banner", // const Banner = ...; export default Banner
	} {
		assertResolves(t, b, selector)
	}
}

// #default is the same address written out. A generator emitting selectors
// uniformly should not have to special-case the default export.
func TestDefaultSpellingIsTheBareName(t *testing.T) {
	b := newProject(t).boot(t)
	assertResolves(t, b, "Panel#default")
}

// A file with only named exports has no bare address. Saying so, and saying
// what it does export, turns a dead end into the next thing to type.
func TestFileWithoutADefaultExportSaysWhatItHas(t *testing.T) {
	b := newProject(t).boot(t)

	assertRejected(t, b, "widgets/Toolbar",
		"no default export", "Toolbar.tsx", "Primary", "Secondary")

	err := resolutionOf(t, b, "widgets/Toolbar")
	if !strings.Contains(err.Error(), "widgets/Toolbar#Primary") &&
		!strings.Contains(err.Error(), "widgets/Toolbar#Secondary") {
		t.Errorf("message suggests no selector to try:\n%v", err)
	}
}

// A default export that is not a component is a different mistake from having
// none, and used to surface as an esbuild error about a generated temp file.
func TestDefaultExportThatIsNotAComponentIsDiagnosed(t *testing.T) {
	b := newProject(t).boot(t)

	assertRejected(t, b, "Settings", "not a component", "Settings.tsx")

	err := resolutionOf(t, b, "Settings")
	if strings.Contains(err.Error(), "no default export") {
		t.Errorf("message blames a missing export when one is present:\n%v", err)
	}
}

// ---------------------------------------------------------------------------
// A selector picks an export out of the file.
// ---------------------------------------------------------------------------

func TestSelectorPicksANamedExport(t *testing.T) {
	b := newProject(t).boot(t)

	for name, selector := range map[string]string{
		"exported function declaration": "Panel#Header",
		"exported const arrow":          "Panel#Footer",
		"renamed re-export":             "Panel#Aside",
		"named export in a nested dir":  "widgets/Toolbar#Primary",
		"named export in a .jsx file":   "legacy/Banner#Dismiss",
	} {
		t.Run(name, func(t *testing.T) { assertResolves(t, b, selector) })
	}
}

// One file, both addresses. The default export and a selection out of the same
// file are two components, not two spellings of one.
func TestBareNameAndSelectorAddressTheSameFileSeparately(t *testing.T) {
	b := newProject(t).boot(t)

	panel, ok := b.Registry().Lookup("Panel")
	if !ok {
		t.Fatal("Panel did not resolve")
	}
	header, ok := b.Registry().Lookup("Panel#Header")
	if !ok {
		t.Fatal("Panel#Header did not resolve")
	}

	if header.Path != panel.Path {
		t.Errorf("the selector resolved to %q, want the same file as the bare name %q", header.Path, panel.Path)
	}
	if panel.Export != "" || header.Export != "Header" {
		t.Errorf("exports = (%q, %q), want (\"\", \"Header\")", panel.Export, header.Export)
	}
	if panel.MountRootID == header.MountRootID {
		t.Errorf("both mount on %q; two components on one page would collide", panel.MountRootID)
	}
	for _, id := range []string{panel.MountRootID, header.MountRootID} {
		if strings.ContainsAny(id, "#/") {
			t.Errorf("mount root %q keeps a separator; it has to be a usable element id", id)
		}
	}
}

// ---------------------------------------------------------------------------
// Selectors that name nothing.
// ---------------------------------------------------------------------------

// The three ways a selector can miss have three different fixes, so they get
// three different messages.
func TestSelectorMissesAreDistinguished(t *testing.T) {
	b := newProject(t).boot(t)

	t.Run("declared but not exported", func(t *testing.T) {
		assertRejected(t, b, "Panel#Scratch", "Scratch", "does not export")
	})
	t.Run("exported but not a component", func(t *testing.T) {
		assertRejected(t, b, "Panel#TITLE", "TITLE", "not a component")
	})
	t.Run("type-only export", func(t *testing.T) {
		assertRejected(t, b, "Panel#Decoration", "Decoration", "not a component")
	})
	t.Run("no such name", func(t *testing.T) {
		assertRejected(t, b, "Panel#Nowhere", "no exported", "Nowhere")
	})
}

// A selector on a file the registry never saw is still "no such component" —
// the file half is what the registry indexes, and it is missing.
func TestSelectorOnAnUnregisteredFileIsAComponentLookupFailure(t *testing.T) {
	b := newProject(t).boot(t)

	err := resolutionOf(t, b, "Nowhere#Primary")
	if !strings.Contains(err.Error(), "no component registered") {
		t.Errorf("expected a registry miss, got:\n%v", err)
	}
	if _, ok := b.Registry().Lookup("Nowhere#Primary"); ok {
		t.Error("a selector on an unregistered file resolved")
	}
}

// Selector names are pasted into the generated import clause, so anything that
// is not an identifier is refused rather than emitted.
func TestSelectorNamesThatCannotBeImportedAreRefused(t *testing.T) {
	b := newProject(t).boot(t)

	for _, bad := range []string{"Panel#has space", "Panel#1Leading", "Panel#semi;colon", "Panel#dash-ed"} {
		err := resolutionOf(t, b, bad)
		if strings.Contains(err.Error(), "bundling is disabled") {
			t.Errorf("%q was accepted; it would be pasted into generated JavaScript", bad)
		}
	}
}

// ---------------------------------------------------------------------------
// Helper modules.
// ---------------------------------------------------------------------------

// .ts is a registry extension, so a shared helper in the components tree is a
// registry entry. It backs no component, and asking for one says so — but its
// presence must not stop the Bundler from booting, which is what a strict
// default-export rule plus a rasterization pass over every name could have
// caused.
func TestHelperModulesAreAddressableButNotRenderable(t *testing.T) {
	b := newProject(t).boot(t)

	if _, ok := b.Registry().Lookup("shared/tokens"); !ok {
		t.Fatal("a .ts file in the components tree was not registered")
	}
	assertRejected(t, b, "shared/tokens", "no default export")
	// resolves cause string part of JSX Element and classFor is a function.
	assertResolves(t, b, "shared/tokens#classFor")
}

// ---------------------------------------------------------------------------
// Imports the components themselves make.
// ---------------------------------------------------------------------------

// Panel.tsx types its props through "./types.js", which is how TypeScript spells
// a sibling import under node16 and bundler resolution: the emitted name, not
// the source one. Both the default export and a selection out of the file have
// to be reachable through it.
func TestEmittedExtensionImportsResolveForEveryExport(t *testing.T) {
	b := newProject(t).boot(t)

	for _, selector := range []string{"Panel", "Panel#Header", "Panel#Footer", "Panel#Aside"} {
		assertResolves(t, b, selector)
	}
}
