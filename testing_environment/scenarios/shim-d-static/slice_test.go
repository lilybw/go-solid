// Package shim_d is a scenario about features composing.
//
// Each of the others takes one thing and follows it end to end. This one runs
// several at once — static assets, export selectors, type checking, hot reload,
// per-Bundler defaults — because the failures worth catching here are the ones
// that live between features rather than inside any of them: two things
// mounted on one mux, one generated file that another feature's cache has to
// notice, a boot order where each step needs the previous one finished.
//
// # No toolchain
//
// Bundling is disabled throughout. Everything under test — resolution,
// generation, serving, invalidation routing — happens either side of esbuild,
// so a scenario that leaves it out still exercises the seams and runs anywhere.
// A render therefore never produces HTML; what it produces is the point at
// which it stopped, which the helpers below read.
package shim_d

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	go_solid "github.com/lilybw/go-solid"
	shared_esbuild "github.com/lilybw/go-solid/shared/esbuild"
	shared_hmr "github.com/lilybw/go-solid/shared/hmr"
	"github.com/lilybw/go-solid/shared/logging"
	shared_net "github.com/lilybw/go-solid/shared/networking"
	shared_static "github.com/lilybw/go-solid/shared/static"
	shared_types "github.com/lilybw/go-solid/shared/types"
)

// settle is how long a filesystem event is given to reach a watcher and be
// acted on. Generous: a flake here reads as a bug in invalidation routing.
const settle = 3 * time.Second

type project struct {
	root       string
	components string
	assets     string
	workspace  string
	mux        *http.ServeMux
}

// newProject stages the fixture into a temp tree, so a test may edit it without
// disturbing the next one.
func newProject(t *testing.T) *project {
	t.Helper()

	root := t.TempDir()
	for _, dir := range []string{"components", "assets"} {
		if err := os.CopyFS(filepath.Join(root, dir), os.DirFS(filepath.Join("testdata", "frontend", dir))); err != nil {
			t.Fatalf("stage %s: %v", dir, err)
		}
	}
	return &project{
		root:       root,
		components: filepath.Join(root, "components"),
		assets:     filepath.Join(root, "assets"),
		workspace:  filepath.Join(root, "components", ".go_solid"),
		mux:        http.NewServeMux(),
	}
}

// options are the feature switches a scenario turns on. The zero value is
// everything off, which is the state a consumer starts from.
type options struct {
	static    bool
	reactive  bool
	hmr       bool
	check     shared_types.CheckMode
	headTitle string
}

func (p *project) boot(t *testing.T, opts options) *go_solid.Bundler {
	t.Helper()

	cfg := &go_solid.Config{
		Components: p.components,
		LogLevel:   logging.LEVEL_ERROR,
		Generation: &shared_esbuild.BundlerConfig{Disabled: true},
		Types:      &shared_types.TypesConfig{Check: opts.check},
	}
	if opts.static {
		cfg.Static = &shared_static.StaticConfig{
			Location: p.assets,
			Mux:      p.mux,
			Reactive: opts.reactive,
		}
	}
	if opts.hmr {
		cfg.HMR = &shared_hmr.HMRConfig{Mux: p.mux}
	}
	if opts.headTitle != "" {
		cfg.Defaults = &go_solid.BehaviouralDefaults{
			HeadSegment: func(h shared_net.HTMLHeadSegmentBuilder) { h.SetTitle(opts.headTitle) },
		}
	}

	bundler, err := go_solid.New(cfg)
	if err != nil {
		t.Fatalf("go_solid.New: %v", err)
	}
	t.Cleanup(bundler.Close)
	return bundler
}

// generated reads a file go_solid wrote into the workspace.
func (p *project) generated(t *testing.T, rel string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(p.workspace, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("read generated %s: %v", rel, err)
	}
	return string(body)
}

func (p *project) assetFile(rel string) string {
	return filepath.Join(p.assets, filepath.FromSlash(rel))
}

func (p *project) componentFile(rel string) string {
	return filepath.Join(p.components, filepath.FromSlash(rel))
}

// resolutionOf renders and reports where the render path stopped. With bundling
// off, a component that resolves cannot produce HTML — it reaches esbuild and
// is turned away there — so that refusal means everything before it succeeded.
func resolutionOf(t *testing.T, b *go_solid.Bundler, selector string, props any) error {
	t.Helper()
	_, err := b.Prepare(selector, props).Render()
	if err == nil {
		t.Fatalf("Render(%q) succeeded with bundling disabled, which should be impossible", selector)
	}
	return err
}

func assertResolves(t *testing.T, b *go_solid.Bundler, selector string, props any) {
	t.Helper()
	if err := resolutionOf(t, b, selector, props); !strings.Contains(err.Error(), "bundling is disabled") {
		t.Errorf("Render(%q) stopped before bundling:\n%v", selector, err)
	}
}

func assertStops(t *testing.T, b *go_solid.Bundler, selector string, props any, wants ...string) error {
	t.Helper()
	err := resolutionOf(t, b, selector, props)
	if strings.Contains(err.Error(), "bundling is disabled") {
		t.Fatalf("Render(%q) reached the bundler; expected it to be turned away first", selector)
	}
	for _, want := range wants {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Render(%q): message is missing %q:\n%v", selector, want, err)
		}
	}
	return err
}
