package shim_d

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The reactive loop
// ---------------------------------------------------------------------------
// An asset changes, the manifest is rebuilt, the generated module is rewritten,
// and whatever imported it is invalidated. The last step is the dependency
// index doing its ordinary job — the module is a bundle input like any other —
// so what is worth testing here is the part that is new: that a change to a
// directory nobody watches for components reaches a file the bundler tracks.

// touch rewrites a file with different bytes and moves its timestamp on, so the
// change is unmistakable whatever the filesystem's granularity.
func touch(t *testing.T, path string) {
	t.Helper()

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if err := os.WriteFile(path, append(body, []byte("\n/* edited */\n")...), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	future := time.Now().Add(time.Second)
	if err := os.Chtimes(path, future, future); err != nil {
		t.Fatalf("touch %s: %v", path, err)
	}
}

// awaitModule waits for the generated module to satisfy a condition, and says
// what it last saw when it does not.
func awaitModule(t *testing.T, p *project, describe string, ok func(string) bool) {
	t.Helper()

	deadline := time.Now().Add(settle)
	var last string
	for time.Now().Before(deadline) {
		last = p.generated(t, "modules/static/assets.ts")
		if ok(last) {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Errorf("the module never %s; it currently reads:\n%s", describe, last)
}

// Editing an asset changes its content hash, which changes its URL, which is
// what makes the old URL safe to have cached forever and the new one a
// different thing to fetch.
func TestEditingAnAssetChangesItsURL(t *testing.T) {
	p := newProject(t)
	b := p.boot(t, options{static: true, reactive: true})

	before := b.Static().URL("images/logo.svg")
	if before == "" {
		t.Fatal("the logo was not published")
	}

	touch(t, p.assetFile("images/logo.svg"))

	awaitModule(t, p, "stopped carrying the old URL", func(module string) bool {
		return !strings.Contains(module, before)
	})
	if after := b.Static().URL("images/logo.svg"); after == before {
		t.Error("the manifest kept the old URL for changed bytes")
	}
}

// A file dropped into the directory becomes addressable without a restart,
// which is the whole reason the watcher exists.
func TestAddingAnAssetAddsAKey(t *testing.T) {
	p := newProject(t)
	b := p.boot(t, options{static: true, reactive: true})

	if err := os.WriteFile(p.assetFile("images/badge-new.svg"), []byte("<svg/>"), 0o644); err != nil {
		t.Fatal(err)
	}

	awaitModule(t, p, "gained the new key", func(module string) bool {
		return strings.Contains(module, "badge_new:")
	})
	if b.Static().URL("images/badge-new.svg") == "" {
		t.Error("the new asset is not addressable through the manifest")
	}
}

func TestRemovingAnAssetRemovesItsKey(t *testing.T) {
	p := newProject(t)
	b := p.boot(t, options{static: true, reactive: true})

	if err := os.Remove(p.assetFile("styles/theme.css")); err != nil {
		t.Fatal(err)
	}

	awaitModule(t, p, "dropped the removed key", func(module string) bool {
		return !strings.Contains(module, "theme:")
	})
	if b.Static().URL("styles/theme.css") != "" {
		t.Error("a removed asset is still addressable")
	}
}

// A rebuild that produces the same bytes must not rewrite the module. The
// module is a bundle input, so touching it invalidates every bundle that
// imported it — doing that for a no-op change would rebuild the world every
// time an editor saved a file it had not altered.
func TestAnUnchangedRebuildDoesNotDisturbTheModule(t *testing.T) {
	p := newProject(t)
	p.boot(t, options{static: true, reactive: true})

	modulePath := p.componentFile(".go_solid/modules/static/assets.ts")
	before, err := os.Stat(modulePath)
	if err != nil {
		t.Fatalf("stat module: %v", err)
	}

	// Rewrite an asset with the bytes it already had. The watcher fires, the
	// manifest is rebuilt, and the result is identical.
	body, err := os.ReadFile(p.assetFile("images/logo.svg"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.assetFile("images/logo.svg"), body, 0o644); err != nil {
		t.Fatal(err)
	}
	time.Sleep(600 * time.Millisecond) // past the debounce, with room to spare

	after, err := os.Stat(modulePath)
	if err != nil {
		t.Fatalf("stat module: %v", err)
	}
	if !after.ModTime().Equal(before.ModTime()) {
		t.Error("the module was republished for a change that produced identical bytes")
	}
}

// Reactivity is a setting. With it off the manifest is what it was at boot, so
// nothing pays for a watcher in production.
func TestWithoutReactivityTheManifestIsFrozen(t *testing.T) {
	p := newProject(t)
	b := p.boot(t, options{static: true})

	before := b.Static().URL("images/logo.svg")
	touch(t, p.assetFile("images/logo.svg"))
	if err := os.WriteFile(p.assetFile("images/badge-new.svg"), []byte("<svg/>"), 0o644); err != nil {
		t.Fatal(err)
	}
	time.Sleep(600 * time.Millisecond)

	if got := b.Static().URL("images/logo.svg"); got != before {
		t.Errorf("the manifest moved without reactivity: %q became %q", before, got)
	}
	if b.Static().URL("images/badge-new.svg") != "" {
		t.Error("an asset added after boot appeared without reactivity")
	}
	if strings.Contains(p.generated(t, "modules/static/assets.ts"), "badge_new:") {
		t.Error("the module was regenerated without reactivity")
	}
}

// A directory created after boot is watched too, and a file written into it
// afterwards is an ordinary event.
func TestANewSubdirectoryIsPickedUp(t *testing.T) {
	p := newProject(t)
	b := p.boot(t, options{static: true, reactive: true})

	if err := os.MkdirAll(p.assetFile("fonts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.assetFile("fonts/body.woff2"), []byte("woff2"), 0o644); err != nil {
		t.Fatal(err)
	}

	awaitModule(t, p, "gained the new directory", func(module string) bool {
		return strings.Contains(module, "fonts:") && strings.Contains(module, "body:")
	})
	if b.Static().URL("fonts/body.woff2") == "" {
		t.Error("an asset in a directory created after boot is not addressable")
	}
}

// A folder of assets normally arrives whole — copied, checked out, renamed into
// place — and its contents predate any watch on it. Nothing will report them,
// so they have to be walked for.
//
// Staging outside the asset root and renaming in is what makes this
// deterministic. The version above depends on a write losing a race with the
// watch being added, which it does on inotify and does not on Windows; this one
// puts the files there first on every platform.
func TestADirectoryThatArrivesCompleteIsPickedUp(t *testing.T) {
	p := newProject(t)
	b := p.boot(t, options{static: true, reactive: true})

	incoming := filepath.Join(t.TempDir(), "fonts")
	if err := os.MkdirAll(filepath.Join(incoming, "display"), 0o755); err != nil {
		t.Fatal(err)
	}
	for rel, body := range map[string]string{
		"body.woff2":          "woff2-body",
		"display/title.woff2": "woff2-title",
	} {
		if err := os.WriteFile(filepath.Join(incoming, filepath.FromSlash(rel)), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	if err := os.Rename(incoming, p.assetFile("fonts")); err != nil {
		t.Fatalf("move the directory into the asset root: %v", err)
	}

	awaitModule(t, p, "gained the directory that arrived complete", func(module string) bool {
		return strings.Contains(module, "fonts:") &&
			strings.Contains(module, "body:") &&
			strings.Contains(module, "title:")
	})
	for _, rel := range []string{"fonts/body.woff2", "fonts/display/title.woff2"} {
		if b.Static().URL(rel) == "" {
			t.Errorf("%s arrived with its directory and is not addressable", rel)
		}
	}
}
