package go_solid

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

// writeTree creates a set of files (relative path -> contents) under a fresh
// temp dir and returns the dir. Directories are created as needed.
func writeTree(t *testing.T, files map[string]string) AbsoluteDirectoryPath {
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
	return AbsoluteDirectoryPath(root)
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

	got := reg.Names()
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
	if filepath.Base(comp.AbsPath) != "LoginForm.tsx" {
		t.Errorf("AbsPath base = %q, want LoginForm.tsx", filepath.Base(comp.AbsPath))
	}
	if !filepath.IsAbs(comp.AbsPath) {
		t.Errorf("AbsPath = %q, want absolute", comp.AbsPath)
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

	got := reg.Names()
	want := []string{"Real"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Names() = %v, want %v (node_modules and dotdirs must be skipped)", got, want)
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
	if names := reg.Names(); !reflect.DeepEqual(names, []string{"A"}) {
		t.Fatalf("initial Names() = %v, want [A]", names)
	}

	// Add a file on disk, then reload.
	if err := os.WriteFile(filepath.Join(string(root), "B.tsx"), []byte("export default () => null;"), 0o644); err != nil {
		t.Fatalf("write B.tsx: %v", err)
	}
	if err := reg.Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}

	got := reg.Names().ToStringSlice()
	want := []string{"A", "B"}
	sort.Strings(got)
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
	if err := os.Remove(filepath.Join(string(root), "B.tsx")); err != nil {
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
	root := writeTree(t, map[string]string{"A.tsx": "export default () => null;"})
	// Pass a relative path; Root() should still be absolute.
	rel, err := filepath.Rel(mustGetwd(t), string(root))
	if err != nil {
		// Different volume etc — skip rather than fail spuriously.
		t.Skipf("cannot relativize temp dir: %v", err)
	}
	reg, err := NewRegistry(AbsoluteDirectoryPath(rel))
	if err != nil {
		t.Fatalf("NewRegistry(rel): %v", err)
	}
	if !filepath.IsAbs(reg.Root()) {
		t.Errorf("Root() = %q, want absolute", reg.Root())
	}
}

func TestRegistry_NonexistentRootErrors(t *testing.T) {
	_, err := NewRegistry(AbsoluteDirectoryPath(filepath.Join(t.TempDir(), "nope-does-not-exist")))
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
