package go_solid

import (
	"strings"
	"testing"

	caching "github.com/lilybw/go-solid/internal/caching"
	"github.com/lilybw/go-solid/shared/meta"
)

// ---------------------------------------------------------------------------
// The selector.
//
// A qualified name is a file, optionally followed by "#" and the export to take
// out of it. "xxx/Yyy" is the default export of xxx/Yyy; "xxx/Yyy#Zzz" is that
// file's exported Zzz.
// ---------------------------------------------------------------------------

func TestSelector_SplitsFileFromExport(t *testing.T) {
	for name, want := range map[string]struct{ file, export string }{
		"auth/LoginForm":         {"auth/LoginForm", ""},
		"auth/LoginForm#Submit":  {"auth/LoginForm", "Submit"},
		"Panel#Sidebar":          {"Panel", "Sidebar"},
		"auth/LoginForm#default": {"auth/LoginForm", ""},
		"deep/nested/a/b/C#D":    {"deep/nested/a/b/C", "D"},
	} {
		file, export := meta.SplitSelector(name)
		if file != want.file || export != want.export {
			t.Errorf("SplitSelector(%q) = (%q, %q), want (%q, %q)", name, file, export, want.file, want.export)
		}
	}
}

// #default is an explicit spelling of the bare form, so a generator emitting
// selectors uniformly does not have to special-case the default export.
func TestSelector_DefaultSpellingRoundTrips(t *testing.T) {
	for _, name := range []string{"auth/LoginForm", "auth/LoginForm#default"} {
		file, export := meta.SplitSelector(name)
		if got := meta.JoinSelector(file, export); got != "auth/LoginForm" {
			t.Errorf("%q normalized to %q, want %q", name, got, "auth/LoginForm")
		}
	}
	if got := meta.JoinSelector("Panel", "Sidebar"); got != "Panel#Sidebar" {
		t.Errorf("JoinSelector = %q, want %q", got, "Panel#Sidebar")
	}
}

func TestSelector_ValidExportNames(t *testing.T) {
	for _, ok := range []string{"", "A", "_private", "$dollar", "Camel9Case"} {
		if !meta.ValidExportName(ok) {
			t.Errorf("%q was rejected", ok)
		}
	}
	for _, bad := range []string{"9Leading", "has space", "semi;colon", "quote\"mark", "dash-ed", "a.b"} {
		if meta.ValidExportName(bad) {
			t.Errorf("%q was accepted; it would be pasted into generated JavaScript", bad)
		}
	}
}

// ---------------------------------------------------------------------------
// Resolution through the registry.
// ---------------------------------------------------------------------------

func TestRegistry_SelectorResolvesToTheBackingFile(t *testing.T) {
	b := bundlerWithoutGeneration(t, map[string]string{
		"auth/LoginForm.tsx": "export default () => null;",
	})

	plain, ok := b.Registry().Lookup("auth/LoginForm")
	if !ok {
		t.Fatal("the file itself did not resolve")
	}
	sub, ok := b.Registry().Lookup("auth/LoginForm#Submit")
	if !ok {
		t.Fatal("a selector on a registered file did not resolve")
	}

	if sub.Path != plain.Path {
		t.Errorf("selector resolved to %q, want the same file as the bare name %q", sub.Path, plain.Path)
	}
	if sub.Export != "Submit" {
		t.Errorf("Export = %q, want %q", sub.Export, "Submit")
	}
	if plain.Export != "" {
		t.Errorf("bare name carried export %q, want the default", plain.Export)
	}
	if sub.Name != "auth/LoginForm#Submit" {
		t.Errorf("Name = %q, want the full selector", sub.Name)
	}
}

// Two exports of one file are two components: distinct cache entries, and
// distinct elements to mount on.
func TestRegistry_SelectorsGetTheirOwnMountRoot(t *testing.T) {
	b := bundlerWithoutGeneration(t, map[string]string{
		"Panel.tsx": "export default () => null;",
	})

	plain, _ := b.Registry().Lookup("Panel")
	header, _ := b.Registry().Lookup("Panel#Header")
	footer, _ := b.Registry().Lookup("Panel#Footer")

	ids := map[string]string{"Panel": plain.MountRootID, "#Header": header.MountRootID, "#Footer": footer.MountRootID}
	seen := map[string]string{}
	for label, id := range ids {
		if id == "" {
			t.Errorf("%s has no mount root", label)
		}
		if strings.ContainsAny(id, "#/") {
			t.Errorf("%s mount root %q keeps a separator; it has to be a usable element id", label, id)
		}
		if other, dup := seen[id]; dup {
			t.Errorf("%s and %s share mount root %q", label, other, id)
		}
		seen[id] = label
	}
}

// A selector on a file that is not registered is still "no such component".
func TestRegistry_SelectorOnAnUnknownFileDoesNotResolve(t *testing.T) {
	b := bundlerWithoutGeneration(t, map[string]string{
		"Panel.tsx": "export default () => null;",
	})
	if _, ok := b.Registry().Lookup("Nowhere#Header"); ok {
		t.Error("a selector on an unregistered file resolved")
	}
}

// "#" separates a file from an export, so a file named for one would register
// under a name that parses as something else. Refused at load.
func TestRegistry_RejectsSelectorSeparatorInFilenames(t *testing.T) {
	_, err := New(&Config{
		Components: componentsDirWith(t, map[string]string{"Panel#Sidebar.tsx": "export default () => null;"}),
		Generation: disabledGeneration(),
	})
	if err == nil {
		t.Fatal("a component file named with the export separator was accepted")
	}
	if !strings.Contains(err.Error(), "#") {
		t.Errorf("error does not explain the separator:\n%v", err)
	}
}

// ---------------------------------------------------------------------------
// Resolution reaches the render path.
// ---------------------------------------------------------------------------

// A selector's artifact is cached under the selector, not under its file, so
// two exports of one file never serve each other's bundle.
func TestRender_SelectorsCacheSeparately(t *testing.T) {
	b := bundlerWithoutGeneration(t, map[string]string{
		"Panel.tsx": "export default () => null;",
	})

	header, _ := b.Registry().Lookup("Panel#Header")
	b.mem.Put(
		caching.NewBuildCacheKey("Panel#Header", header.MountRootID, b.buildID),
		&caching.Rendered{JS: "/* header */", JSName: "Panel_Header.js"},
	)

	out, err := b.Prepare("Panel#Header", nil).Render()
	if err != nil {
		t.Fatalf("Render(Panel#Header): %v", err)
	}
	if !strings.Contains(out.HTML, `<div id="`+header.MountRootID+`">`) {
		t.Errorf("selector render mounted on the wrong root:\n%s", out.HTML)
	}
	if out.JS != "/* header */" {
		t.Errorf("selector render returned %q", out.JS)
	}

	// The file's default export was never cached, so it must miss rather than
	// pick up the sibling's artifact.
	if _, err := b.Prepare("Panel", nil).Render(); err == nil {
		t.Error("the bare name was answered from a selector's cache entry")
	}
}

// Invalidating a file has to reach every component backed by it, or an edit
// leaves a stale sub-component behind.
func TestCache_FileScopedEnumerationFindsEverySelector(t *testing.T) {
	mem := caching.NewMemCache(true)
	for _, name := range []string{"Panel", "Panel#Header", "Panel#Footer", "PanelOther", "Other#Panel"} {
		mem.Put(caching.NewMemCacheKey(name, "root"), &caching.Rendered{JS: name})
	}

	got := mem.ComponentsInFile("Panel")
	want := map[string]bool{"Panel": true, "Panel#Header": true, "Panel#Footer": true}
	if len(got) != len(want) {
		t.Fatalf("ComponentsInFile = %v, want exactly %v", got, want)
	}
	for _, name := range got {
		if !want[name] {
			t.Errorf("ComponentsInFile returned %q, which another file backs", name)
		}
	}
}
