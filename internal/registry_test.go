package internal

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/lilybw/go-solid/shared/meta"
	"github.com/lilybw/go-solid/shared/registry"
)

// writeTree creates a set of files (relative path -> contents) under a fresh
// temp dir and returns the dir. Directories are created as needed.
func writeTree(t *testing.T, files map[string]string) meta.AbsoluteDirectoryPath {
	t.Helper()
	root := t.TempDir()
	for rel, contents := range files {
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(full), err)
		}
		if err := os.WriteFile(full, []byte(contents), 0o644); err != nil {
			t.Fatalf("write %s: %v", full, err)
		}
	}
	return root
}

func TestRegistry_DerivesNamesFromPaths(t *testing.T) {
	root := writeTree(t, map[string]string{
		"Version.tsx":          "export default () => null;",
		"auth/LoginForm.tsx":   "export default () => null;",
		"auth/nested/Deep.jsx": "export default () => null;",
		"widgets/Chart.ts":     "export default 1;",
		"notes.md":             "ignore me",
		"styles.css":           "ignore me too",
	})

	reg, err := NewRegistry(root)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	got := reg.Map(func(name meta.QualifiedName, _ *registry.Component) meta.QualifiedName { return name })
	sort.Strings(got)
	want := []string{"Version", "auth/LoginForm", "auth/nested/Deep", "widgets/Chart"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Names() = %v, want %v", got, want)
	}
}

func TestRegistry_LookupReturnsCorrectComponent(t *testing.T) {
	root := writeTree(t, map[string]string{
		"auth/LoginForm.tsx": "export default () => null;",
	})
	reg, err := NewRegistry(root)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	comp, ok := reg.Lookup("auth/LoginForm")
	if !ok {
		t.Fatal("Lookup(auth/LoginForm) not found")
	}
	if comp.Name != "auth/LoginForm" {
		t.Errorf("Name = %q, want auth/LoginForm", comp.Name)
	}
	if comp.Ext != ".tsx" {
		t.Errorf("Ext = %q, want .tsx", comp.Ext)
	}
	if filepath.Base(comp.Path) != "LoginForm.tsx" {
		t.Errorf("AbsPath base = %q, want LoginForm.tsx", filepath.Base(comp.Path))
	}
	if !filepath.IsAbs(comp.Path) {
		t.Errorf("AbsPath = %q, want absolute", comp.Path)
	}
}

func TestRegistry_LookupMissingReturnsFalse(t *testing.T) {
	root := writeTree(t, map[string]string{"A.tsx": "export default () => null;"})
	reg, err := NewRegistry(root)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	if _, ok := reg.Lookup("does/not/exist"); ok {
		t.Error("Lookup of missing component returned ok=true")
	}
}

func TestRegistry_SkipsNodeModulesAndDotDirs(t *testing.T) {
	root := writeTree(t, map[string]string{
		"Real.tsx":                       "export default () => null;",
		"node_modules/solid-js/index.js": "module.exports = {};",
		"node_modules/pkg/Thing.tsx":     "export default () => null;",
		".hidden/Secret.tsx":             "export default () => null;",
	})
	reg, err := NewRegistry(root)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	got := reg.Map(func(name meta.QualifiedName, _ *registry.Component) meta.QualifiedName { return name })
	sort.Strings(got)
	want := []string{"Real"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Names() = %#v (len %d), want %#v (len %d)", got, len(got), want, len(want))
		for i, s := range got {
			t.Errorf("got[%d] = %q", i, s)
		}
	}
}

func TestRegistry_DuplicateNamesAreAnError(t *testing.T) {
	// Foo.tsx and Foo.jsx both resolve to name "Foo" -> ambiguous.
	root := writeTree(t, map[string]string{
		"Foo.tsx": "export default () => null;",
		"Foo.jsx": "export default () => null;",
	})
	_, err := NewRegistry(root)
	if err == nil {
		t.Fatal("expected duplicate-name error, got nil")
	}
}

func TestRegistry_ReloadPicksUpNewFiles(t *testing.T) {
	root := writeTree(t, map[string]string{
		"A.tsx": "export default () => null;",
	})
	reg, err := NewRegistry(root)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	names := reg.Map(func(name meta.QualifiedName, _ *registry.Component) meta.QualifiedName { return name })
	sort.Strings(names)
	if !reflect.DeepEqual(strings.Join(names, ","), "A") {
		t.Fatalf("initial Names() = %v, want [A]", names)
	}

	// Add a file on disk, then reload.
	if err := os.WriteFile(filepath.Join(root, "B.tsx"), []byte("export default () => null;"), 0o644); err != nil {
		t.Fatalf("write B.tsx: %v", err)
	}
	if err := reg.Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}

	got := reg.Map(func(name meta.QualifiedName, _ *registry.Component) meta.QualifiedName { return name })
	sort.Strings(got)
	want := []string{"A", "B"}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("after Reload Names() = %v, want %v", got, want)
	}
}

func TestRegistry_ReloadDropsDeletedFiles(t *testing.T) {
	root := writeTree(t, map[string]string{
		"A.tsx": "export default () => null;",
		"B.tsx": "export default () => null;",
	})
	reg, err := NewRegistry(root)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	if err := os.Remove(filepath.Join(root, "B.tsx")); err != nil {
		t.Fatalf("remove B.tsx: %v", err)
	}
	if err := reg.Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if _, ok := reg.Lookup("B"); ok {
		t.Error("B still present after deletion + Reload")
	}
	if _, ok := reg.Lookup("A"); !ok {
		t.Error("A missing after Reload")
	}
}

func TestRegistry_RootIsAbsolute(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("GitHubs' actions' runners on Windows does not provide for a predictable filesystem surrounding the test. Skipped.")
	}

	root := writeTree(t, map[string]string{"A.tsx": "export default () => null;"})
	// Pass a relative path; Root() should still be absolute.
	rel, err := filepath.Rel(mustGetwd(t), root)
	if err != nil {
		// Different volume etc — skip rather than fail spuriously.
		t.Skipf("cannot relativize temp dir: %v", err)
	}
	reg, err := NewRegistry(rel)
	if err != nil {
		t.Fatalf("NewRegistry(rel): %v", err)
	}
	if !filepath.IsAbs(reg.Root()) {
		t.Errorf("Root() = %q, want absolute", reg.Root())
	}
}

func TestRegistry_NonexistentRootErrors(t *testing.T) {
	_, err := NewRegistry(filepath.Join(t.TempDir(), "nope-does-not-exist"))
	if err == nil {
		t.Fatal("expected error for nonexistent root, got nil")
	}
}

func mustGetwd(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	return wd
}

// MakeReactive starts a watcher. Starting a second over the same tree would
// double every callback and leave the first goroutine running with nothing
// holding it, so the second call is refused rather than accepted quietly.
func TestRegistry_MakeReactiveRefusesASecondWatcher(t *testing.T) {
	root := writeTree(t, map[string]string{"A.tsx": "export default () => null;"})
	reg, err := NewRegistry(root)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	t.Cleanup(reg.Close)

	if err := reg.MakeReactive(nil, nil, nil); err != nil {
		t.Fatalf("first MakeReactive: %v", err)
	}
	if err := reg.MakeReactive(nil, nil, nil); err == nil {
		t.Error("a second MakeReactive was accepted; the first watcher is now orphaned")
	}

	// Close releases the watcher, so the registry can be made reactive again.
	reg.Close()
	if err := reg.MakeReactive(nil, nil, nil); err != nil {
		t.Errorf("MakeReactive after Close: %v", err)
	}
}

// Every callback is optional. A nil onDrop used to reach the watcher goroutine
// and panic there, where no caller could recover it.
func TestRegistry_MakeReactiveAcceptsNilCallbacks(t *testing.T) {
	root := writeTree(t, map[string]string{"A.tsx": "export default () => null;"})
	reg, err := NewRegistry(root)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	t.Cleanup(reg.Close)

	if err := reg.MakeReactive(nil, nil, nil); err != nil {
		t.Fatalf("MakeReactive with nil callbacks: %v", err)
	}
	// Drive a change through the watcher; a nil callback must not panic it.
	if err := os.WriteFile(filepath.Join(root, "B.tsx"), []byte("export default () => null;"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "A.tsx")); err != nil {
		t.Fatal(err)
	}
	time.Sleep(150 * time.Millisecond)
}

// Close is a teardown hook, reachable from several owners.
func TestRegistry_CloseIsIdempotentAndNilSafe(t *testing.T) {
	var absent *ComponentRegistry
	absent.Close()

	root := writeTree(t, map[string]string{"A.tsx": "export default () => null;"})
	reg, err := NewRegistry(root)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	if err := reg.MakeReactive(nil, nil, nil); err != nil {
		t.Fatalf("MakeReactive: %v", err)
	}
	reg.Close()
	reg.Close()
}
