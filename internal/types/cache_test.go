package types

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// meta.AbsoluteFilePath is an alias for string, so sources pass through plainly.
func sampleExtraction(sources ...string) Extraction {
	return Extraction{
		Shape:        NewShape([]Field{{Name: "title", TS: "string"}, {Name: "count", TS: "number", Optional: true}}),
		Name:         "Props",
		Found:        true,
		HasParameter: true,
		Sources:      sources,
	}
}

func writeSource(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestCache_PathMirrorsTheComponentsTree(t *testing.T) {
	workspace := t.TempDir()
	c := NewCache(workspace)

	want := filepath.Join(workspace, CACHE_DIR_NAME, "auth", "LoginForm"+CACHE_ENTRY_EXT)
	if got := c.Path("auth/LoginForm"); got != want {
		t.Fatalf("Path = %q, want %q", got, want)
	}
}

func TestCache_RoundTripsThroughDisk(t *testing.T) {
	workspace, sources := t.TempDir(), t.TempDir()
	source := writeSource(t, sources, "Hello.tsx", "export default function Hello(props: { title: string }) {}")

	first := NewCache(workspace)
	if err := first.Put("auth/LoginForm", sampleExtraction(source)); err != nil {
		t.Fatalf("Put: %v", err)
	}

	// A fresh process: memory is empty, so this can only come off disk.
	second := NewCache(workspace)
	got, ok := second.Get("auth/LoginForm")
	if !ok {
		t.Fatal("entry should have been read back from disk")
	}
	if !got.Shape.Equal(sampleExtraction(source).Shape) {
		t.Fatalf("shape did not survive the round trip: %q", got.Shape.Fingerprint())
	}
	if got.Name != "Props" || !got.Found || !got.HasParameter {
		t.Fatalf("entry lost its flags: %+v", got)
	}
}

func TestCache_EntryIsHumanReadable(t *testing.T) {
	workspace, sources := t.TempDir(), t.TempDir()
	source := writeSource(t, sources, "Hello.tsx", "export default function Hello() {}")

	c := NewCache(workspace)
	if err := c.Put("Hello", sampleExtraction(source)); err != nil {
		t.Fatalf("Put: %v", err)
	}
	raw, err := os.ReadFile(c.Path("Hello"))
	if err != nil {
		t.Fatal(err)
	}
	// The format is an implementation detail, but while it is JSON it should
	// be legible when someone goes looking.
	for _, want := range []string{`"component": "Hello"`, `"sources"`, `"sha256:`, `"name": "title"`} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("entry missing %q:\n%s", want, raw)
		}
	}
}

// The point of hashing every contributing file: editing an imported definition
// has to invalidate the components that read it.
func TestCache_AChangedSourceInvalidatesTheEntry(t *testing.T) {
	workspace, sources := t.TempDir(), t.TempDir()
	component := writeSource(t, sources, "Hello.tsx", "export default function Hello(props: Props) {}")
	imported := writeSource(t, sources, "shared.d.ts", "export interface Props { title: string }\n")

	first := NewCache(workspace)
	if err := first.Put("Hello", sampleExtraction(component, imported)); err != nil {
		t.Fatalf("Put: %v", err)
	}

	// Not the component — the file it imported.
	writeSource(t, sources, "shared.d.ts", "export interface Props { title: number }\n")

	if _, ok := NewCache(workspace).Get("Hello"); ok {
		t.Fatal("an entry must not survive a change to any of its sources")
	}
}

func TestCache_MemoryLayerNoticesAnEditWithoutBeingTold(t *testing.T) {
	workspace, sources := t.TempDir(), t.TempDir()
	source := writeSource(t, sources, "Hello.tsx", "export default function Hello() {}")

	c := NewCache(workspace)
	if err := c.Put("Hello", sampleExtraction(source)); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if _, ok := c.Get("Hello"); !ok {
		t.Fatal("a fresh entry should be served")
	}

	// Rewrite and move the timestamp on, as an editor would.
	writeSource(t, sources, "Hello.tsx", "export default function Hello(props: { a: string }) {}")
	future := time.Now().Add(time.Second)
	if err := os.Chtimes(source, future, future); err != nil {
		t.Fatal(err)
	}

	if _, ok := c.Get("Hello"); ok {
		t.Fatal("the memory layer should have noticed the edit without an invalidation")
	}
}

func TestCache_InvalidateDropsTheMemoryLayer(t *testing.T) {
	workspace, sources := t.TempDir(), t.TempDir()
	source := writeSource(t, sources, "Hello.tsx", "export default function Hello() {}")

	c := NewCache(workspace)
	if err := c.Put("Hello", sampleExtraction(source)); err != nil {
		t.Fatalf("Put: %v", err)
	}
	c.Invalidate("Hello")

	// The disk entry is still valid, so Get succeeds — from disk, not memory.
	if _, ok := c.Get("Hello"); !ok {
		t.Fatal("Invalidate must not destroy a still-valid disk entry")
	}
}

func TestCache_MissOnAnUnknownComponent(t *testing.T) {
	if _, ok := NewCache(t.TempDir()).Get("Nope"); ok {
		t.Fatal("an unwritten component should miss")
	}
}

func TestCache_RejectsEscapingNames(t *testing.T) {
	c := NewCache(t.TempDir())
	for _, name := range []string{"", "../escape", "a/../../b", "./x"} {
		if err := c.Put(name, sampleExtraction()); err == nil {
			t.Errorf("Put(%q) should have been rejected", name)
		}
	}
}

func TestCache_PruneRemovesOrphansAndEmptiedDirectories(t *testing.T) {
	workspace, sources := t.TempDir(), t.TempDir()
	source := writeSource(t, sources, "Hello.tsx", "export default function Hello() {}")

	c := NewCache(workspace)
	for _, name := range []string{"Kept", "nested/Dropped"} {
		if err := c.Put(name, sampleExtraction(source)); err != nil {
			t.Fatalf("Put(%q): %v", name, err)
		}
	}

	removed, err := c.Prune([]string{"Kept"})
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if removed != 1 {
		t.Fatalf("removed = %d, want 1", removed)
	}
	if _, err := os.Stat(c.Path("Kept")); err != nil {
		t.Error("a live entry must survive pruning")
	}
	if _, err := os.Stat(c.Path("nested/Dropped")); err == nil {
		t.Error("an orphaned entry should have been pruned")
	}
	if _, err := os.Stat(filepath.Join(c.Root(), "nested")); err == nil {
		t.Error("a directory emptied by pruning should have been removed")
	}
}

func TestCache_PruneOnAnEmptyTreeIsANoop(t *testing.T) {
	if removed, err := NewCache(t.TempDir()).Prune(nil); err != nil || removed != 0 {
		t.Fatalf("Prune = %d, %v; want 0, nil", removed, err)
	}
}

func TestCache_RewritingAnUnchangedEntryLeavesTheFileAlone(t *testing.T) {
	workspace, sources := t.TempDir(), t.TempDir()
	source := writeSource(t, sources, "Hello.tsx", "export default function Hello() {}")

	c := NewCache(workspace)
	if err := c.Put("Hello", sampleExtraction(source)); err != nil {
		t.Fatalf("Put: %v", err)
	}
	past := time.Now().Add(-time.Hour).Truncate(time.Second)
	if err := os.Chtimes(c.Path("Hello"), past, past); err != nil {
		t.Fatal(err)
	}

	if err := NewCache(workspace).Put("Hello", sampleExtraction(source)); err != nil {
		t.Fatalf("Put: %v", err)
	}
	info, err := os.Stat(c.Path("Hello"))
	if err != nil {
		t.Fatal(err)
	}
	if !info.ModTime().Equal(past) {
		t.Error("an identical entry should not be rewritten")
	}
}

func TestEnsurePublished_CreatesTheImportableSurface(t *testing.T) {
	workspace := t.TempDir()
	if err := EnsurePublished(workspace); err != nil {
		t.Fatalf("EnsurePublished: %v", err)
	}
	info, err := os.Stat(PublishedRoot(workspace))
	if err != nil || !info.IsDir() {
		t.Fatalf("published surface not created: %v", err)
	}
	// It must not be where the cache lives.
	if PublishedRoot(workspace) == NewCache(workspace).Root() {
		t.Fatal("the published surface and the cache must be separate directories")
	}
}
