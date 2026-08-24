package caching

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lilybw/go-solid/internal/esbuild"
	"github.com/lilybw/go-solid/internal/io"
)

// These tests exercise the disk cache in isolation. They construct *Rendered
// values directly and never invoke Node or esbuild, so they run anywhere with
// no toolchain. A few source files are written to temp dirs purely as hash
// targets — they are never compiled.

// --- helpers ----------------------------------------------------------------

// newTestDiskCache builds an enabled DiskCache rooted at a fresh temp workspace.
func newTestDiskCache(t *testing.T) (*DiskCache, string) {
	t.Helper()
	ws := t.TempDir()
	dc, err := NewDiskCache(ws, true)
	if err != nil {
		t.Fatalf("newDiskCache: %v", err)
	}
	return dc, ws
}

// writeSource writes a source file and returns its absolute path.
func writeSource(t *testing.T, dir, name, contents string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func sampleRendered() *Rendered {
	return &Rendered{
		JS:      "console.log('hi')",
		CSS:     ".r{color:red}",
		JSName:  "W.abc.js",
		CSSName: "W.def.css",
	}
}

// --- construction -----------------------------------------------------------

func TestDiskCache_NewCreatesDir(t *testing.T) {
	ws := t.TempDir()
	_, err := NewDiskCache(ws, true)
	if err != nil {
		t.Fatalf("newDiskCache: %v", err)
	}
	if _, err := os.Stat(filepath.Join(ws, CACHE_DIR_NAME)); err != nil {
		t.Errorf("cache dir not created: %v", err)
	}
}

func TestDiskCache_DisabledIsNoop(t *testing.T) {
	ws := t.TempDir()
	dc, err := NewDiskCache(ws, false)
	if err != nil {
		t.Fatalf("newDiskCache(disabled): %v", err)
	}
	// Disabled: no dir, put is a noop, get always misses.
	if _, err := os.Stat(filepath.Join(ws, CACHE_DIR_NAME)); !os.IsNotExist(err) {
		t.Error("disabled cache should not create its directory")
	}
	if err := dc.Put(&CacheKey{Component: "auth/LoginForm"}, true, sampleRendered(), []string{}); err != nil {
		t.Errorf("disabled put should be noop, got: %v", err)
	}
	if _, ok := dc.Get(&CacheKey{Component: "auth/LoginForm"}); ok {
		t.Error("disabled get should always miss")
	}
}

// --- hashing ----------------------------------------------------------------

func TestHashFile_StableAndDetectsChange(t *testing.T) {
	dir := t.TempDir()
	p := writeSource(t, dir, "f.txt", "hello")
	h1, ok := hashFile(p)
	if !ok {
		t.Fatal("hashFile ok=false on existing file")
	}
	h2, _ := hashFile(p)
	if h1 != h2 {
		t.Errorf("hash not stable: %q != %q", h1, h2)
	}
	if !strings.HasPrefix(h1, "sha256:") {
		t.Errorf("hash missing algorithm prefix: %q", h1)
	}
	os.WriteFile(p, []byte("hello world"), 0o644)
	h3, _ := hashFile(p)
	if h3 == h1 {
		t.Error("hash did not change after content change")
	}
}

func TestHashFile_MissingFile(t *testing.T) {
	if _, ok := hashFile(filepath.Join(t.TempDir(), "nope")); ok {
		t.Error("hashFile should return ok=false for missing file")
	}
}

// --- entry naming -----------------------------------------------------------

func TestEntryStem_HumanReadableAndSafe(t *testing.T) {
	stem := entryStem(&CacheKey{Component: "auth/LoginForm"})
	// Slashes sanitized.
	if strings.Contains(stem, "/") {
		t.Errorf("stem contains slash: %q", stem)
	}
	// Component and rootID visible for debugging.
	if !strings.Contains(stem, "auth_LoginForm") {
		t.Errorf("stem not human-readable: %q", stem)
	}
	// Hash string truncated to 12 chars.
	str := strings.Split(stem, "__")[1]
	if len(str) > 12 || len(str) < 6 {
		t.Errorf("stem hash string not between lengths 6-12: %q", str)
	}
}

// --- put / get round trip ---------------------------------------------------

func TestDiskCache_PutGetRoundTrip(t *testing.T) {
	dc, ws := newTestDiskCache(t)
	src := writeSource(t, ws, "W.tsx", "export default 1;")
	want := sampleRendered()
	if err := dc.Put(&CacheKey{Component: "auth/LoginForm"}, true, want, []string{src}); err != nil {
		t.Fatalf("put: %v", err)
	}
	got, ok := dc.Get(&CacheKey{Component: "auth/LoginForm"})
	if !ok {
		t.Fatal("get miss after put")
	}
	if got.JS != want.JS || got.CSS != want.CSS {
		t.Errorf("round trip mismatch:\n got %+v\nwant %+v", got, want)
	}
	if got.HTML != "" {
		t.Errorf("disk cache returned HTML %q; the document is assembled per request, never stored", got.HTML)
	}
	if got.JSName != want.JSName || got.CSSName != want.CSSName {
		t.Errorf("asset names lost: got js=%q css=%q", got.JSName, got.CSSName)
	}
}

func TestDiskCache_ManifestIsReadableJSON(t *testing.T) {
	dc, ws := newTestDiskCache(t)
	src := writeSource(t, ws, "W.tsx", "export default 1;")
	dc.Put(&CacheKey{Component: "key1"}, true, sampleRendered(), []string{src})

	// Find and parse the manifest as plain JSON — the whole point of the format.
	cacheDir := filepath.Join(ws, CACHE_DIR_NAME)
	files, _ := os.ReadDir(cacheDir)
	var manName string
	for _, f := range files {
		if strings.HasSuffix(f.Name(), ".meta.json") {
			manName = f.Name()
		}
	}
	if manName == "" {
		t.Fatal("no .meta.json written")
	}
	// Filename should carry the human-readable identity.
	if !strings.Contains(manName, "key1") {
		t.Errorf("manifest filename not human-readable: %q", manName)
	}
	b, _ := os.ReadFile(filepath.Join(cacheDir, manName))
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("manifest not valid JSON: %v", err)
	}
	for _, field := range []string{"component", "generatedAt", "sources", "artifacts"} {
		if _, ok := m[field]; !ok {
			t.Errorf("manifest missing field %q", field)
		}
	}
}

func TestDiskCache_NoCSSOmitsCSSFile(t *testing.T) {
	dc, ws := newTestDiskCache(t)
	src := writeSource(t, ws, "W.tsx", "export default 1;")
	r := &Rendered{JS: "1", JSName: "w.js"} // no CSS
	dc.Put(&CacheKey{Component: "k"}, false, r, []string{src})

	got, ok := dc.Get(&CacheKey{Component: "k"})
	if !ok {
		t.Fatal("get miss")
	}
	if got.CSS != "" || got.CSSName != "" {
		t.Errorf("expected no CSS, got css=%q name=%q", got.CSS, got.CSSName)
	}
	// No .css file on disk.
	cacheDir := filepath.Join(ws, CACHE_DIR_NAME)
	files, _ := os.ReadDir(cacheDir)
	for _, f := range files {
		if strings.HasSuffix(f.Name(), ".css") {
			t.Errorf("unexpected .css file written: %s", f.Name())
		}
	}
}

// --- invalidation -----------------------------------------------------------

func TestDiskCache_InvalidatesOnSourceEdit(t *testing.T) {
	dc, ws := newTestDiskCache(t)
	src := writeSource(t, ws, "W.tsx", "export default 1;")
	dc.Put(&CacheKey{Component: "k"}, false, sampleRendered(), []string{src})

	if _, ok := dc.Get(&CacheKey{Component: "k"}); !ok {
		t.Fatal("expected hit before edit")
	}
	// Edit source: hash changes -> entry stale.
	os.WriteFile(src, []byte("export default 2;"), 0o644)
	if _, ok := dc.Get(&CacheKey{Component: "k"}); ok {
		t.Error("expected stale (miss) after source edit")
	}
}

func TestDiskCache_InvalidatesOnSourceDelete(t *testing.T) {
	dc, ws := newTestDiskCache(t)
	src := writeSource(t, ws, "W.tsx", "export default 1;")
	dc.Put(&CacheKey{Component: "k"}, false, sampleRendered(), []string{src})
	os.Remove(src)
	if _, ok := dc.Get(&CacheKey{Component: "k"}); ok {
		t.Error("expected miss after source deleted (unhashable)")
	}
}

func TestDiskCache_ValidWhenSourceUnchanged(t *testing.T) {
	dc, ws := newTestDiskCache(t)
	a := writeSource(t, ws, "A.tsx", "export default 1;")
	b := writeSource(t, ws, "sub/B.tsx", "export default 2;")
	dc.Put(&CacheKey{Component: "k"}, true, sampleRendered(), []string{a, b})
	// Touch mtime without changing content — must still be valid (hash-based).
	future := time.Now().Add(48 * time.Hour)
	os.Chtimes(a, future, future)
	if _, ok := dc.Get(&CacheKey{Component: "k"}); !ok {
		t.Error("entry should remain valid when content unchanged (mtime must not matter)")
	}
}

// --- atomic write -----------------------------------------------------------

func TestAtomicWrite_OverwritesCleanly(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f")
	if err := io.WriteAtomic(p, []byte("first")); err != nil {
		t.Fatal(err)
	}
	if err := io.WriteAtomic(p, []byte("second")); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	if string(b) != "second" {
		t.Errorf("got %q, want second", b)
	}
	// No leftover temp files in the dir.
	files, _ := os.ReadDir(dir)
	for _, f := range files {
		if strings.HasPrefix(f.Name(), ".tmp-") {
			t.Errorf("leftover temp file: %s", f.Name())
		}
	}
}

// --- concurrency (meaningful under -race) -----------------------------------

func TestDiskCache_ConcurrentPutGet(t *testing.T) {
	dc, ws := newTestDiskCache(t)
	srcs := make([]string, 4)
	for i := range srcs {
		srcs[i] = writeSource(t, ws, "S"+string(rune('0'+i))+".tsx", "export default 0;")
	}

	var wg sync.WaitGroup
	const workers = 16
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func(n int) {
			defer wg.Done()
			key := "k" + string(rune('0'+n%4))
			src := srcs[n%4]
			for j := 0; j < 25; j++ {
				_ = dc.Put(&CacheKey{Component: key}, true, sampleRendered(), []string{src})
				dc.Get(&CacheKey{Component: key})
			}
		}(i)
	}
	wg.Wait()
}

// --- extractSources (metafile parsing, pure) --------------------------------

func TestExtractSources_FiltersNodeModulesAndTempEntry(t *testing.T) {
	workspace := t.TempDir()
	entryDir := filepath.Join(workspace, ".solidbundle-123")

	// Minimal esbuild-metafile-shaped JSON. Input keys are relative, forward-slashed
	// (as esbuild emits); ExtractSourcesFromMetafile joins them against workspace.
	meta := `{
      "inputs": {
        "components/A.tsx": {"bytes": 1},
        "components/sub/B.tsx": {"bytes": 1},
        "node_modules/solid-js/dist/solid.js": {"bytes": 1},
        ".solidbundle-123/entry.jsx": {"bytes": 1}
      }
    }`
	got := esbuild.ExtractSourcesFromMetafile(meta, workspace, entryDir)

	// Only the two consumer sources survive; node_modules and temp entry dropped.
	var hasA, hasB, hasNM, hasEntry bool
	for _, s := range got {
		switch {
		case strings.HasSuffix(s, "A.tsx"):
			hasA = true
		case strings.HasSuffix(s, "B.tsx"):
			hasB = true
		case strings.Contains(s, "node_modules"):
			hasNM = true
		case strings.Contains(s, "entry.jsx"):
			hasEntry = true
		}
	}
	if !hasA || !hasB {
		t.Errorf("consumer sources missing: A=%v B=%v (%v)", hasA, hasB, got)
	}
	if hasNM {
		t.Error("node_modules leaked into sources")
	}
	if hasEntry {
		t.Error("temp entry leaked into sources")
	}
	// Paths must be absolute (joined with workspace).
	for _, s := range got {
		if !filepath.IsAbs(s) {
			t.Errorf("source not absolute: %q", s)
		}
	}
}

// Everything under the workspace that is not the throwaway entry is a real
// dependency. Generated modules live there, and a bundle that imports one is
// stale the moment it is rewritten — so it has to be recorded, or neither the
// dependency index nor the disk cache can notice.
func TestExtractSources_KeepsGeneratedModulesUnderTheWorkspace(t *testing.T) {
	workspace := t.TempDir()
	entryDir := filepath.Join(workspace, ".solidbundle-123")

	meta := `{
      "inputs": {
        "components/A.tsx": {"bytes": 1},
        "modules/static.js": {"bytes": 1},
        ".solidbundle-123/entry.jsx": {"bytes": 1}
      }
    }`
	got := esbuild.ExtractSourcesFromMetafile(meta, workspace, entryDir)

	var hasModule bool
	for _, s := range got {
		if strings.HasSuffix(s, filepath.Join("modules", "static.js")) {
			hasModule = true
		}
		if strings.Contains(s, "entry.jsx") {
			t.Errorf("the throwaway entry was recorded: %q", s)
		}
	}
	if !hasModule {
		t.Errorf("a generated module under the workspace was dropped: %v", got)
	}
}

// The exclusion is by identity, so it cannot depend on the workspace being
// named .go_solid — that is only the default, and a consumer may point
// Workspace anywhere.
func TestExtractSources_ExclusionDoesNotDependOnTheWorkspaceName(t *testing.T) {
	workspace := filepath.Join(t.TempDir(), "custom-workspace")
	entryDir := filepath.Join(workspace, ".solidbundle-abc")

	meta := `{
      "inputs": {
        "components/A.tsx": {"bytes": 1},
        "modules/static.js": {"bytes": 1},
        ".solidbundle-abc/entry.jsx": {"bytes": 1}
      }
    }`
	got := esbuild.ExtractSourcesFromMetafile(meta, workspace, entryDir)

	if len(got) != 2 {
		t.Fatalf("got %d sources, want 2 (the component and the generated module): %v", len(got), got)
	}
	for _, s := range got {
		if strings.Contains(s, "entry.jsx") {
			t.Errorf("the throwaway entry was recorded: %q", s)
		}
	}
}

// A sibling directory sharing a prefix with the entry directory is not inside
// it, so segment-wise comparison matters.
func TestExtractSources_PrefixSiblingOfTheEntryDirIsKept(t *testing.T) {
	workspace := t.TempDir()
	entryDir := filepath.Join(workspace, ".solidbundle-1")

	meta := `{
      "inputs": {
        ".solidbundle-1/entry.jsx": {"bytes": 1},
        ".solidbundle-12/leftover.js": {"bytes": 1}
      }
    }`
	got := esbuild.ExtractSourcesFromMetafile(meta, workspace, entryDir)

	if len(got) != 1 || !strings.HasSuffix(got[0], "leftover.js") {
		t.Errorf("got %v, want only the sibling directory's file", got)
	}
}

func TestExtractSources_EmptyMetafile(t *testing.T) {
	if got := esbuild.ExtractSourcesFromMetafile("", "/work", ""); got != nil {
		t.Errorf("empty metafile should yield nil, got %v", got)
	}
	if got := esbuild.ExtractSourcesFromMetafile("not json", "/work", ""); got != nil {
		t.Errorf("bad metafile should yield nil, got %v", got)
	}
}

// --- gap coverage added after review -----------------------------------------

// The core feature: a brand-new DiskCache over the same workspace sees prior
// entries (survives process restart).
func TestDiskCache_PersistsAcrossNewInstance(t *testing.T) {
	ws := t.TempDir()
	src := writeSource(t, ws, "C.tsx", "export default 1;")

	dc1, err := NewDiskCache(ws, true)
	if err != nil {
		t.Fatal(err)
	}
	dc1.Put(&CacheKey{Component: "k"}, true, sampleRendered(), []string{src})

	dc2, err := NewDiskCache(ws, true) // "restart"
	if err != nil {
		t.Fatal(err)
	}
	got, ok := dc2.Get(&CacheKey{Component: "k"})
	if !ok {
		t.Fatal("new instance did not see persisted entry")
	}
	if got.JS != sampleRendered().JS {
		t.Error("persisted JS differs")
	}
}

// Regression: a disk-cache hit must return the ORIGINAL serving names (the
// content-hashed names the shell references), not storage filenames. Otherwise
// the shell and JSName disagree and asset serving breaks.
func TestDiskCache_PreservesServingNames(t *testing.T) {
	dc, ws := newTestDiskCache(t)
	src := writeSource(t, ws, "C.tsx", "x")
	want := &Rendered{
		JS:      "1;",
		CSS:     ".x{}",
		JSName:  "comp.abc123.js",
		CSSName: "comp.def456.css",
	}
	dc.Put(&CacheKey{Component: "k"}, true, want, []string{src})

	got, ok := dc.Get(&CacheKey{Component: "k"})
	if !ok {
		t.Fatal("miss")
	}
	if got.JSName != want.JSName {
		t.Errorf("JSName not preserved: got %q want %q", got.JSName, want.JSName)
	}
	if got.CSSName != want.CSSName {
		t.Errorf("CSSName not preserved: got %q want %q", got.CSSName, want.CSSName)
	}
}

// Two keys sharing the 12-char stem prefix must not be confused: get matches on
// the full Key stored in the manifest, not the truncated filename.
// The memory layer keys on root and build; the disk layer must agree, or a
// warm process misses and a restarted one hits the wrong entry.
func TestDiskCache_DistinguishesRootAndBuild(t *testing.T) {
	dc, ws := newTestDiskCache(t)
	src := writeSource(t, ws, "C.tsx", "x")

	entries := map[*CacheKey]string{
		NewBuildCacheKey("C", "root-a", "min"): "a-min",
		NewBuildCacheKey("C", "root-b", "min"): "b-min",
		NewBuildCacheKey("C", "root-a", "raw"): "a-raw",
	}
	for key, js := range entries {
		if err := dc.Put(key, true, &Rendered{JS: js, JSName: js + ".js"}, []string{src}); err != nil {
			t.Fatalf("Put(%+v): %v", *key, err)
		}
	}
	for key, want := range entries {
		got, ok := dc.Get(key)
		if !ok {
			t.Fatalf("Get(%+v) missed", *key)
		}
		if got.JS != want {
			t.Errorf("Get(%+v).JS = %q, want %q — entries share a stem", *key, got.JS, want)
		}
	}
}

func TestDiskCache_PrefixCollisionSafe(t *testing.T) {
	dc, ws := newTestDiskCache(t)
	src := writeSource(t, ws, "C.tsx", "x")

	keyA := "0123456789ab_AAAA"
	keyB := "0123456789ab_BBBB" // identical first 12 chars
	dc.Put(&CacheKey{Component: keyA}, true, &Rendered{JS: "jsA", JSName: "a.js"}, []string{src})
	dc.Put(&CacheKey{Component: keyB}, true, &Rendered{JS: "jsB", JSName: "b.js"}, []string{src})

	gotA, okA := dc.Get(&CacheKey{Component: keyA})
	gotB, okB := dc.Get(&CacheKey{Component: keyB})
	if !okA || !okB {
		t.Fatalf("missed one: A=%v B=%v", okA, okB)
	}
	if gotA.JS != "jsA" || gotB.JS != "jsB" {
		t.Errorf("prefix collision confused entries: A=%q B=%q", gotA.JS, gotB.JS)
	}
}

// An entry with multiple sources is invalidated if ANY one changes.
func TestDiskCache_InvalidatesOnTransitiveSourceEdit(t *testing.T) {
	dc, ws := newTestDiskCache(t)
	a := writeSource(t, ws, "A.tsx", "a1")
	b := writeSource(t, ws, "sub/B.tsx", "b1")
	dc.Put(&CacheKey{Component: "k"}, false, sampleRendered(), []string{a, b})

	os.WriteFile(b, []byte("b2-changed"), 0o644) // edit the transitive dep
	if _, ok := dc.Get(&CacheKey{Component: "k"}); ok {
		t.Error("entry not invalidated when a transitive source changed")
	}
}

func TestAppendUnique_Dedups(t *testing.T) {
	l := []string{"a", "b"}
	if len(l) != 2 {
		t.Errorf("dup not removed: %v", l)
	}
}
