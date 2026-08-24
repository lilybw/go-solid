// Package shim_a is an end-to-end "scenario" test: a vertical-slice cutout of
// how a real consumer (the HOTS / hermestraffic.com "v1.0" server) drives
// go-solid. It exercises the full render path — registry -> esbuild ->
// node/babel-preset-solid transform -> HTML assembly — against the real
// library, using the same call shapes the application uses:
//
//	go_solid.New(&Config{ Components, Defaults{HeadSegment} })
//	bundler.Prepare(name, props).ForRequest(w, r).Render()
//
// It lives under testing_environment/scenarios/ and is part of the root module,
// so `go test ./...` compiles and runs it alongside the unit tests.
//
// # Toolchain
//
// The render tests need Node on PATH plus the SolidJS peer deps
// (solid-js, babel-preset-solid, @babel/core) resolvable from the components
// dir. Those deps are declared in testdata/frontend/package.json. When they are
// not yet installed, ensureToolchain will run `npm ci` (falling back to
// `npm install`) in testdata/frontend automatically, so the scenario is
// self-healing whether run in CI or by hand locally.
//
// # Environment switches
//
//   - GO_SOLID_REQUIRE_INTEGRATION=1 : treat a missing/broken toolchain as a
//     hard failure instead of a skip. CI sets this so scenarios genuinely run.
//   - GO_SOLID_NO_NPM_INSTALL=1       : never auto-run npm; skip (or fail, if
//     integration is required) when peer deps are absent. Useful for hermetic
//     runners that pre-install deps and want no implicit network access.
package shim_a

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	go_solid "github.com/lilybw/go-solid"
	"github.com/lilybw/go-solid/internal/meta"
	"github.com/lilybw/go-solid/shared/logging"
	"github.com/lilybw/go-solid/shared/networking"
	"github.com/lilybw/go-solid/shared/registry"
)

var (
	componentsDir = filepath.Join("testdata", "frontend", "components")
	frontendDir   = filepath.Join("testdata", "frontend")

	// toolchainOnce guards a single auto-install / probe per test binary so the
	// parallel tests below don't race on `npm` or on the node_modules tree.
	toolchainOnce sync.Once
	toolchainErr  error
)

func requireIntegration() bool { return os.Getenv("GO_SOLID_REQUIRE_INTEGRATION") == "1" }
func npmInstallDisabled() bool { return os.Getenv("GO_SOLID_NO_NPM_INSTALL") == "1" }

func peerDepsPresent() bool {
	for _, pkg := range []string{"solid-js", "babel-preset-solid", filepath.Join("@babel", "core")} {
		if fi, err := os.Stat(filepath.Join(frontendDir, "node_modules", pkg)); err != nil || !fi.IsDir() {
			return false
		}
	}
	return true
}

// installPeerDeps runs `npm ci` (falling back to `npm install`) in
// testdata/frontend. `npm ci` is preferred because it is reproducible from the
// committed lockfile; it falls back to `npm install` when there is no lockfile
// or when `ci` refuses (e.g. lock out of sync).
func installPeerDeps() error {
	npm, err := exec.LookPath("npm")
	if err != nil {
		return err
	}
	run := func(args ...string) error {
		cmd := exec.Command(npm, args...)
		cmd.Dir = frontendDir
		cmd.Stdout = os.Stderr // surface npm output in the test log, keep stdout clean
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}
	if _, statErr := os.Stat(filepath.Join(frontendDir, "package-lock.json")); statErr == nil {
		if ciErr := run("ci", "--no-audit", "--no-fund"); ciErr == nil {
			return nil
		}
		// fall through to install if ci failed (lock drift, etc.)
	}
	return run("install", "--no-audit", "--no-fund")
}

func newBundler(t *testing.T) *go_solid.Bundler {
	t.Helper()

	bundler, err := go_solid.New(&go_solid.Config{
		Components: componentsDir,
		LogLevel:   logging.LEVEL_ERROR,
		Defaults: &go_solid.BehaviouralDefaults{
			HeadSegment: func(b networking.HTMLHeadSegmentBuilder) {
				b.SetTitle("HOTS")
			},
		},
	})
	if err != nil {
		t.Fatalf("go_solid.New: %v", err)
	}
	t.Cleanup(bundler.Close)
	return bundler
}

func TestRegistryDiscoversComponents(t *testing.T) {
	bundler := newBundler(t)

	names := bundler.Registry().Map(func(k meta.QualifiedName, _ *registry.Component) meta.QualifiedName { return k })
	want := map[string]bool{"Home": false, "auth/LoginForm": false}
	for _, n := range names {
		if _, ok := want[n]; ok {
			want[n] = true
		}
	}
	for n, found := range want {
		if !found {
			t.Errorf("component %q not discovered; registry has %v", n, names)
		}
	}
}

func TestRenderHomeForRequest(t *testing.T) {
	bundler := newBundler(t)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	rendered, err := bundler.Prepare("Home", nil).ForRequest(rec, req).Render()
	if err != nil {
		t.Fatalf("Render(Home): %v", err)
	}
	if rendered.HTML == "" {
		t.Fatal("Render(Home): empty HTML")
	}
	if !strings.Contains(rendered.HTML, "<title>HOTS</title>") {
		t.Errorf("Render(Home): head-segment default not applied; missing <title>HOTS</title>")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

func TestRenderQualifiedLoginForm(t *testing.T) {
	bundler := newBundler(t)

	req := httptest.NewRequest(http.MethodGet, "/account", nil)
	rec := httptest.NewRecorder()

	rendered, err := bundler.Prepare("auth/LoginForm", nil).ForRequest(rec, req).Render()
	if err != nil {
		t.Fatalf("Render(auth/LoginForm): %v", err)
	}
	if rendered.HTML == "" {
		t.Fatal("Render(auth/LoginForm): empty HTML")
	}
}

// TestUnqualifiedLoginFormFails is the regression guard for the bug found in the
// work codebase: it calls Prepare("LoginForm", ...) with the bare name, but the
// component is registered under its path-relative qualified name
// "auth/LoginForm", so the lookup must fail with a clear error.
func TestUnqualifiedLoginFormFails(t *testing.T) {
	bundler := newBundler(t)

	req := httptest.NewRequest(http.MethodGet, "/account", nil)
	rec := httptest.NewRecorder()

	_, err := bundler.Prepare("LoginForm", nil).ForRequest(rec, req).Render()
	if err == nil {
		t.Fatal("expected Render(\"LoginForm\") to fail: component is registered as \"auth/LoginForm\"")
	}
	if !strings.Contains(err.Error(), "no component registered") {
		t.Fatalf("unexpected error shape: %v", err)
	}
	if !strings.Contains(err.Error(), "auth/LoginForm") {
		t.Errorf("error should surface the available qualified name; got: %v", err)
	}
}
