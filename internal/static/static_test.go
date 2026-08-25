package static

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	shared_static "github.com/lilybw/go-solid/shared/static"
)

func tree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, body := range files {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func manifestOf(t *testing.T, files map[string]string) *Manifest {
	t.Helper()
	m, err := BuildManifest(&shared_static.StaticConfig{
		Location:  tree(t, files),
		MountPath: shared_static.DEFAULT_MOUNT_PATH,
		Ignore:    shared_static.DEFAULT_IGNORE,
	})
	if err != nil {
		t.Fatalf("BuildManifest: %v", err)
	}
	return m
}

// --- sanitizing ------------------------------------------------------------

// The rule is the smallest one that produces an identifier: runs of unusable
// characters collapse to "_", a leading digit gets one prefixed, nothing else.
// A reader should be able to derive the key from the filename without knowing a
// convention.
func TestSanitize(t *testing.T) {
	for name, want := range map[string]string{
		"logo":        "logo",
		"logo-dark":   "logo_dark",
		"hero image":  "hero_image",
		"a.b.c":       "a_b_c",
		"2024-report": "_2024_report",
		"$money":      "$money",
		"_private":    "_private",
		"café":        "café",
		"---":         "_",
		"":            "",
	} {
		if got := sanitize(name); got != want {
			t.Errorf("sanitize(%q) = %q, want %q", name, got, want)
		}
	}
}

// --- the manifest ----------------------------------------------------------

func TestManifest_URLsAreContentHashedAndKeepTheExtension(t *testing.T) {
	m := manifestOf(t, map[string]string{"images/logo.svg": "<svg/>"})

	asset, ok := m.Lookup("images/logo.svg")
	if !ok {
		t.Fatal("the asset was not indexed by its path")
	}
	if !strings.HasPrefix(asset.URL, shared_static.DEFAULT_MOUNT_PATH) {
		t.Errorf("URL %q is not under the mount path", asset.URL)
	}
	if !strings.HasSuffix(asset.URL, ".svg") {
		t.Errorf("URL %q lost the extension; the browser needs it and so does the loader", asset.URL)
	}
	if !strings.Contains(asset.URL, asset.ContentHash[:8]) {
		t.Errorf("URL %q carries no content hash, so it cannot be cached immutably", asset.URL)
	}
	if asset.MIME != "image/svg+xml" {
		t.Errorf("MIME = %q, want image/svg+xml", asset.MIME)
	}
}

// Changing the bytes has to change the URL. That is what lets a cached copy be
// immutable and what lets the rest of the system notice an edit.
func TestManifest_ChangedBytesChangeTheURL(t *testing.T) {
	before := manifestOf(t, map[string]string{"a.txt": "one"})
	after := manifestOf(t, map[string]string{"a.txt": "two"})

	if before.URL("a.txt") == after.URL("a.txt") {
		t.Error("editing an asset left its URL unchanged")
	}
}

func TestManifest_MirrorsTheDirectoryStructure(t *testing.T) {
	m := manifestOf(t, map[string]string{
		"logo.svg":              "<svg/>",
		"images/hero.png":       "png",
		"images/icons/tick.svg": "<svg/>",
	})

	module := GenerateAssets(m)
	for _, want := range []string{"logo:", "images:", "icons:", "tick:", "hero:"} {
		if !strings.Contains(module, want) {
			t.Errorf("generated module is missing %q:\n%s", want, module)
		}
	}
}

// logo.svg beside logo.png is ordinary — format fallbacks — so it must not be
// an error. Extensions become subfields.
func TestManifest_SharedStemBecomesExtensionSubfields(t *testing.T) {
	m := manifestOf(t, map[string]string{
		"logo.svg": "<svg/>",
		"logo.png": "png",
	})

	entry := m.Tree.Files[0]
	if entry.Key != "logo" {
		t.Fatalf("key = %q, want logo", entry.Key)
	}
	if entry.Asset != nil {
		t.Fatal("one of the two assets won the key outright")
	}
	var keys []string
	for _, variant := range entry.ByExtension {
		keys = append(keys, variant.Key)
	}
	if strings.Join(keys, ",") != "png,svg" {
		t.Errorf("subfields = %v, want [png svg] sorted", keys)
	}
}

// Two names sanitizing to one key cannot be resolved, so it is an error naming
// both rather than whichever was read last.
func TestManifest_CollidingKeysAreAnError(t *testing.T) {
	_, err := BuildManifest(&shared_static.StaticConfig{
		Location:  tree(t, map[string]string{"logo-dark.svg": "a", "logo_dark.svg": "b"}),
		MountPath: shared_static.DEFAULT_MOUNT_PATH,
	})
	if err == nil {
		t.Fatal("a key collision was accepted")
	}
	for _, want := range []string{"logo-dark.svg", "logo_dark.svg", "logo_dark"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error is missing %q:\n%v", want, err)
		}
	}
}

// A directory and a file claiming one key is the same problem.
func TestManifest_DirectoryAndFileCollisionIsAnError(t *testing.T) {
	_, err := BuildManifest(&shared_static.StaticConfig{
		Location:  tree(t, map[string]string{"logo.svg": "a", "logo/inner.png": "b"}),
		MountPath: shared_static.DEFAULT_MOUNT_PATH,
	})
	if err == nil {
		t.Fatal("a directory colliding with a file was accepted")
	}
}

func TestManifest_IgnoresTheUsualLeavings(t *testing.T) {
	m := manifestOf(t, map[string]string{
		"logo.svg":          "<svg/>",
		".DS_Store":         "junk",
		".gitkeep":          "",
		"bundle.js.map":     "{}",
		".hidden/thing.png": "x",
	})

	if len(m.ByRel) != 1 {
		t.Errorf("indexed %d assets, want only logo.svg: %v", len(m.ByRel), m.ByRel)
	}
}

// Every file the manifest was built from is a source: a change to any of them
// changes the module, and the dependency graph needs to know that.
func TestManifest_RecordsEverySourceItRead(t *testing.T) {
	m := manifestOf(t, map[string]string{"a.txt": "a", "sub/b.txt": "b"})
	if len(m.Sources) != 2 {
		t.Errorf("Sources = %v, want both files", m.Sources)
	}
}

// --- generation ------------------------------------------------------------

// A leaf is a string, asserted to the branded type so its media type travels
// with the value. It has to stay a string: that is what lets it drop into
// anything expecting one, without a call or a property access.
func TestGenerateAssets_LeavesAreBrandedStrings(t *testing.T) {
	m := manifestOf(t, map[string]string{"logo.svg": "<svg/>"})
	assets := GenerateAssets(m)

	want := `logo: "` + m.URL("logo.svg") + `" as AssetURL<"image/svg+xml">`
	if !strings.Contains(assets, want) {
		t.Errorf("the leaf is not a branded string literal; wanted %q in:\n%s", want, assets)
	}
}

// The module is TypeScript, so the media type is carried by the value rather
// than by a separate declaration that has to be pulled into the program and
// kept in step with it.
func TestGenerateAssets_TypesLeavesByMediaType(t *testing.T) {
	m := manifestOf(t, map[string]string{"data/config.json": "{}", "logo.svg": "<svg/>"})
	assets := GenerateAssets(m)

	for _, want := range []string{
		`AssetURL<"application/json">`,
		`AssetURL<"image/svg+xml">`,
		`import type { AssetURL } from "./runtime"`,
	} {
		if !strings.Contains(assets, want) {
			t.Errorf("the graph is missing %q:\n%s", want, assets)
		}
	}
}

// The index is the module's entry, so it has to re-export the runtime and put
// the graph and the loaders behind one default.
func TestGenerateIndex_HoistsTheWholeSurface(t *testing.T) {
	index := GenerateIndex()

	for _, want := range []string{
		`export * from "./runtime"`,
		`import assets from "./assets"`,
		"export default { ...assets, load, loadText, loadJSON }",
		MODULE_SPECIFIER,
	} {
		if !strings.Contains(index, want) {
			t.Errorf("the index is missing %q:\n%s", want, index)
		}
	}
}

// The disabled artifacts have two jobs: resolve, so the bundler never fails on
// a missing module; and carry the reason, so the compiler names the fix.
func TestDisabledArtifactsResolveAndExplain(t *testing.T) {
	assets := GenerateDisabledAssets()
	if !strings.Contains(assets, "export default") {
		t.Errorf("the disabled graph exports nothing, so importing it fails to resolve:\n%s", assets)
	}
	if !strings.Contains(assets, "FeatureDisabled<") || !strings.Contains(assets, "Config.Static.Location") {
		t.Errorf("the disabled graph carries no reason:\n%s", assets)
	}

	index := GenerateDisabledIndex()
	for _, want := range []string{"@deprecated", "Config.Static.Location", `export * from "./runtime"`} {
		if !strings.Contains(index, want) {
			t.Errorf("the disabled index is missing %q:\n%s", want, index)
		}
	}
	// The loaders survive being switched off; only the graph is empty.
	if strings.Contains(index, "FeatureDisabled<") == false && strings.Contains(index, "assets") == false {
		t.Errorf("the disabled index does not reach the graph:\n%s", index)
	}
}

// The runtime is the same bytes whatever is in the asset directory, which is
// why an asset edit does not touch it.
func TestGenerateRuntime_IsIndependentOfTheAssets(t *testing.T) {
	first := GenerateRuntime()
	second := GenerateRuntime()

	if first != second {
		t.Error("the runtime is not stable across renderings")
	}
	for _, want := range []string{"export type AssetURL", "export function load", "FeatureDisabled"} {
		if !strings.Contains(first, want) {
			t.Errorf("the runtime is missing %q", want)
		}
	}
}

// --- the endpoint ----------------------------------------------------------

func servingRegistry(t *testing.T, files map[string]string) (*enabledStaticRegistry, *Manifest) {
	t.Helper()
	cfg := &shared_static.StaticConfig{
		Location:    tree(t, files),
		MountPath:   shared_static.DEFAULT_MOUNT_PATH,
		Ignore:      shared_static.DEFAULT_IGNORE,
		InlineLimit: shared_static.DEFAULT_INLINE_LIMIT,
	}
	m, err := BuildManifest(cfg)
	if err != nil {
		t.Fatalf("BuildManifest: %v", err)
	}
	return &enabledStaticRegistry{cfg: cfg, manifest: m}, m
}

func TestHandler_ServesAKnownAssetImmutably(t *testing.T) {
	reg, m := servingRegistry(t, map[string]string{"logo.svg": "<svg/>"})

	rec := httptest.NewRecorder()
	reg.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, m.URL("logo.svg"), nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if rec.Body.String() != "<svg/>" {
		t.Errorf("body = %q", rec.Body.String())
	}
	if got := rec.Header().Get("Cache-Control"); !strings.Contains(got, "immutable") {
		t.Errorf("Cache-Control = %q; a content-hashed URL never needs revalidating", got)
	}
	if got := rec.Header().Get("Content-Type"); got != "image/svg+xml" {
		t.Errorf("Content-Type = %q", got)
	}
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Error("the browser is left to guess the type")
	}
}

// The manifest is the whole of what is servable. A path that names no entry is
// a 404 before anything touches a filesystem, which is why traversal has no
// route rather than being defended against.
func TestHandler_ServesNothingOutsideTheManifest(t *testing.T) {
	reg, m := servingRegistry(t, map[string]string{"logo.svg": "<svg/>"})

	for _, path := range []string{
		shared_static.DEFAULT_MOUNT_PATH + "../../etc/passwd",
		shared_static.DEFAULT_MOUNT_PATH + "logo.svg", // the un-hashed name
		shared_static.DEFAULT_MOUNT_PATH,
		m.URL("logo.svg") + "x",
	} {
		rec := httptest.NewRecorder()
		reg.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s returned %d, want 404", path, rec.Code)
		}
	}
}

func TestHandler_RefusesWrites(t *testing.T) {
	reg, m := servingRegistry(t, map[string]string{"logo.svg": "<svg/>"})

	rec := httptest.NewRecorder()
	reg.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, m.URL("logo.svg"), nil))

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rec.Code)
	}
}

// Assets above the inline limit stream from disk, which is what gives range
// requests and so makes audio and video seekable.
func TestHandler_SupportsRangeRequests(t *testing.T) {
	body := strings.Repeat("0123456789", 64)
	cfg := &shared_static.StaticConfig{
		Location:    tree(t, map[string]string{"clip.bin": body}),
		MountPath:   shared_static.DEFAULT_MOUNT_PATH,
		InlineLimit: 1, // force the streaming path
	}
	m, err := BuildManifest(cfg)
	if err != nil {
		t.Fatal(err)
	}
	reg := &enabledStaticRegistry{cfg: cfg, manifest: m}

	req := httptest.NewRequest(http.MethodGet, m.URL("clip.bin"), nil)
	req.Header.Set("Range", "bytes=10-19")
	rec := httptest.NewRecorder()
	reg.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusPartialContent {
		t.Fatalf("status = %d, want 206", rec.Code)
	}
	if rec.Body.String() != "0123456789" {
		t.Errorf("body = %q, want the requested range", rec.Body.String())
	}
}

// --- settings resolve in one place -----------------------------------------

// The manifest, the generated module and the endpoint all read the mount path.
// Resolving it separately in each is how they come to disagree: a default
// applied in one and not another registers the endpoint under a pattern no
// asset URL begins with, which used to be a panic on an empty pattern.
func TestUnsetSettingsResolveConsistently(t *testing.T) {
	assets := tree(t, map[string]string{"logo.svg": "<svg/>"})
	workspace := t.TempDir()
	mux := http.NewServeMux()

	reg, err := NewStaticRegistry(
		// Nothing but the two required settings; everything else unset.
		&shared_static.StaticConfig{Location: assets, Mux: mux},
		workspace, nil, nil,
	)
	if err != nil {
		t.Fatalf("NewStaticRegistry: %v", err)
	}
	t.Cleanup(reg.Close)

	url := reg.Manifest().URL("logo.svg")
	if url == "" {
		t.Fatal("the asset was not published")
	}
	if !strings.HasPrefix(url, shared_static.DEFAULT_MOUNT_PATH) {
		t.Errorf("URL %q does not begin with the default mount path", url)
	}

	// The endpoint has to be reachable at the prefix those URLs carry.
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, url, nil))
	if rec.Code != http.StatusOK {
		t.Errorf("the endpoint returned %d for its own URL; it is mounted somewhere else", rec.Code)
	}
}

// A mount path written without its slashes still names the same prefix.
func TestMountPathIsBoundedBySlashes(t *testing.T) {
	for _, given := range []string{"assets", "/assets", "assets/", "/assets/"} {
		cfg := &shared_static.StaticConfig{MountPath: given}
		if got := cfg.EffectiveMountPath(); got != "/assets/" {
			t.Errorf("MountPath %q resolved to %q, want %q", given, got, "/assets/")
		}
	}
}

// --- shutdown --------------------------------------------------------------

// Close has to reach the debounce, not only the watcher. A rebuild armed just
// before shutdown fires afterwards, reads a directory the caller may already
// have torn down, and calls onChange into caches that consider themselves shut.
func TestClose_CancelsAPendingRebuild(t *testing.T) {
	assets := tree(t, map[string]string{"logo.svg": "<svg/>"})
	workspace := t.TempDir()

	var changes int64
	reg, err := NewStaticRegistry(
		&shared_static.StaticConfig{
			Location: assets,
			Mux:      http.NewServeMux(),
			Reactive: true,
			Ignore:   shared_static.DEFAULT_IGNORE,
		},
		workspace,
		func(string) { atomic.AddInt64(&changes, 1) },
		func(err error) { t.Errorf("a rebuild ran after Close: %v", err) },
	)
	if err != nil {
		t.Fatalf("NewStaticRegistry: %v", err)
	}

	// Arm the debounce, then shut down inside its window.
	if err := os.WriteFile(filepath.Join(assets, "added.svg"), []byte("<svg/>"), 0o644); err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	reg.Close()

	settled := atomic.LoadInt64(&changes)

	// Removing the directory is what a t.TempDir cleanup does next. Nothing
	// should be left to notice.
	if err := os.RemoveAll(assets); err != nil {
		t.Fatal(err)
	}
	time.Sleep(400 * time.Millisecond) // well past the debounce window

	if got := atomic.LoadInt64(&changes); got != settled {
		t.Errorf("the module was republished %d times after Close", got-settled)
	}
}

// Close is a teardown hook, reachable from more than one owner.
func TestClose_IsIdempotent(t *testing.T) {
	reg, err := NewStaticRegistry(
		&shared_static.StaticConfig{
			Location: tree(t, map[string]string{"logo.svg": "<svg/>"}),
			Mux:      http.NewServeMux(),
			Reactive: true,
		},
		t.TempDir(), nil, nil,
	)
	if err != nil {
		t.Fatalf("NewStaticRegistry: %v", err)
	}

	done := make(chan struct{})
	go func() {
		reg.Close()
		reg.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Close hung or panicked on a second call")
	}
}

// A disabled registry has nothing to close, and is closed all the same.
func TestClose_OnADisabledRegistry(t *testing.T) {
	reg, err := NewStaticRegistry(&shared_static.StaticConfig{}, t.TempDir(), nil, nil)
	if err != nil {
		t.Fatalf("NewStaticRegistry: %v", err)
	}
	reg.Close()
	reg.Close()
}

// --- the module as a directory ---------------------------------------------

// Everything belonging to the module lives in one directory, so it can be read,
// deleted or ignored as a unit — and so a second generated module drops in
// beside it without either knowing about the other.
func TestModuleIsSelfContained(t *testing.T) {
	workspace := t.TempDir()
	if err := EnsureDisabled(workspace); err != nil {
		t.Fatalf("EnsureDisabled: %v", err)
	}

	root := ModuleRoot(workspace)
	for _, name := range []string{INDEX_FILENAME, RUNTIME_FILENAME, ASSETS_FILENAME, README_FILENAME} {
		if _, err := os.Stat(filepath.Join(root, name)); err != nil {
			t.Errorf("%s is missing from the module directory: %v", name, err)
		}
	}
	if ModulePathFor(workspace) != filepath.Join(root, INDEX_FILENAME) {
		t.Errorf("the entry point is %q, want the index", ModulePathFor(workspace))
	}
}

// An asset edit rewrites the graph and nothing else. The index is what bundles
// name, so leaving it alone is what keeps an unrelated rebuild from
// invalidating them.
func TestOnlyTheGraphIsRewrittenByAnAssetChange(t *testing.T) {
	assets := tree(t, map[string]string{"logo.svg": "<svg/>"})
	workspace := t.TempDir()

	reg, err := NewStaticRegistry(
		&shared_static.StaticConfig{Location: assets, Mux: http.NewServeMux()},
		workspace, nil, nil,
	)
	if err != nil {
		t.Fatalf("NewStaticRegistry: %v", err)
	}
	t.Cleanup(reg.Close)

	root := ModuleRoot(workspace)
	stamps := map[string]time.Time{}
	for _, name := range []string{INDEX_FILENAME, RUNTIME_FILENAME} {
		info, err := os.Stat(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		stamps[name] = info.ModTime()
	}

	if err := os.WriteFile(filepath.Join(assets, "logo.svg"), []byte("<svg id='x'/>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := reg.(*enabledStaticRegistry).rebuild(); err != nil {
		t.Fatalf("rebuild: %v", err)
	}

	for name, before := range stamps {
		info, err := os.Stat(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		if !info.ModTime().Equal(before) {
			t.Errorf("%s was rewritten by an asset change; only the graph should be", name)
		}
	}
	graph, err := os.ReadFile(filepath.Join(root, ASSETS_FILENAME))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(graph), reg.Manifest().URL("logo.svg")) {
		t.Error("the graph does not carry the new URL")
	}
}

// --- the tsconfig fragment --------------------------------------------------

// The fragment maps the namespace at the modules directory rather than listing
// what is in it, so a consumer extends it once and never touches it again as
// modules are added.
func TestTSConfigFragmentMapsTheNamespaceByWildcard(t *testing.T) {
	workspace := t.TempDir()
	path, err := EnsureTSConfigFragment(workspace)
	if err != nil {
		t.Fatalf("EnsureTSConfigFragment: %v", err)
	}
	if path != TSConfigFragmentPath(workspace) {
		t.Errorf("wrote to %q, want %q", path, TSConfigFragmentPath(workspace))
	}

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var parsed struct {
		CompilerOptions struct {
			BaseURL string              `json:"baseUrl"`
			Paths   map[string][]string `json:"paths"`
		} `json:"compilerOptions"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("the fragment is not valid JSON: %v\n%s", err, body)
	}

	targets, mapped := parsed.CompilerOptions.Paths[MODULE_NAMESPACE+"/*"]
	if !mapped {
		t.Fatalf("no wildcard mapping for %q:\n%s", MODULE_NAMESPACE+"/*", body)
	}
	if len(targets) != 1 || targets[0] != "./"+MODULES_DIR_NAME+"/*" {
		t.Errorf("mapping = %v, want a single relative entry at the modules directory", targets)
	}
	// A baseUrl here would change how the consumer's own non-relative imports
	// resolve. Since TypeScript 4.1 paths needs none, so it must not set one.
	if parsed.CompilerOptions.BaseURL != "" {
		t.Errorf("the fragment sets baseUrl to %q, which alters resolution beyond its own mapping",
			parsed.CompilerOptions.BaseURL)
	}
}

// The specifier the fragment resolves has to be the one the module says to
// import, and the one the bundler is told to alias.
func TestSpecifierIsConsistentEverywhere(t *testing.T) {
	if MODULE_SPECIFIER != MODULE_NAMESPACE+"/"+MODULE_NAME {
		t.Errorf("the specifier %q is not the namespace plus the module name", MODULE_SPECIFIER)
	}
	if !strings.Contains(GenerateIndex(), MODULE_SPECIFIER) {
		t.Error("the index does not name the specifier it is reached by")
	}
	if !strings.Contains(GenerateReadme("/ws/.go_solid/tsconfig.paths.json", true), MODULE_SPECIFIER) {
		t.Error("the README does not name the specifier")
	}
}

// The README exists because neither the specifier nor the tsconfig line is
// discoverable from the outside.
func TestReadmeSaysHowToReachTheModule(t *testing.T) {
	readme := GenerateReadme("/ws/.go_solid/tsconfig.paths.json", true)
	for _, want := range []string{MODULE_SPECIFIER, "extends", ".go_solid/tsconfig.paths.json"} {
		if !strings.Contains(readme, want) {
			t.Errorf("the README does not mention %q:\n%s", want, readme)
		}
	}

	off := GenerateReadme("/ws/.go_solid/tsconfig.paths.json", false)
	if !strings.Contains(off, DISABLED_REASON) {
		t.Errorf("a README for a disabled feature does not say so:\n%s", off)
	}
}
