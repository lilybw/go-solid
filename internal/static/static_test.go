package static

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
	if !strings.Contains(asset.URL, asset.Digest[:8]) {
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

	module := GenerateModule(m)
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
		"logo.svg":         "<svg/>",
		".DS_Store":        "junk",
		".gitkeep":         "",
		"bundle.js.map":    "{}",
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

// Leaves are string constants so esbuild can inline them and drop the assets a
// component never names. An object literal per leaf would survive tree-shaking.
func TestGenerateModule_LeavesAreStringConstants(t *testing.T) {
	m := manifestOf(t, map[string]string{"logo.svg": "<svg/>"})
	module := GenerateModule(m)

	if !strings.Contains(module, `logo: "`+m.URL("logo.svg")+`"`) {
		t.Errorf("the leaf is not a plain string:\n%s", module)
	}
}

func TestGenerateDefinition_TypesLeavesByMediaType(t *testing.T) {
	m := manifestOf(t, map[string]string{"data/config.json": "{}", "logo.svg": "<svg/>"})
	definition := GenerateDefinition(m)

	for _, want := range []string{
		`AssetURL<"application/json">`,
		`AssetURL<"image/svg+xml">`,
		`declare module "go-solid/static"`,
		"export function load",
	} {
		if !strings.Contains(definition, want) {
			t.Errorf("definition is missing %q:\n%s", want, definition)
		}
	}
}

// The disabled artifacts have two jobs: resolve, so the bundler never fails on
// a missing module; and carry the reason, so the compiler names the fix.
func TestDisabledArtifactsResolveAndExplain(t *testing.T) {
	module := GenerateDisabledModule()
	if !strings.Contains(module, "export default") {
		t.Errorf("the disabled module exports nothing, so importing it fails to resolve:\n%s", module)
	}

	definition := GenerateDisabledDefinition()
	for _, want := range []string{"FeatureDisabled<", "@deprecated", "Config.Static.Location"} {
		if !strings.Contains(definition, want) {
			t.Errorf("the disabled definition is missing %q:\n%s", want, definition)
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
