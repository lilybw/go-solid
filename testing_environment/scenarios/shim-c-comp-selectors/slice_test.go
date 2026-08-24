// Package shim_c is an end-to-end scenario for how a component is *addressed*:
// which file a name resolves to, which export inside it, what happens when the
// answer is nothing, and how the browser is told to reload the right one.
//
// A qualified name is a selector. "widgets/Toolbar" is the default export of
// widgets/Toolbar.tsx; "widgets/Toolbar#Primary" is that file's exported
// Primary. Everything below is about the consequences of that.
//
// # Why this shim needs no toolchain
//
// Resolution happens before bundling: the registry finds the file, the
// extractor settles which export inside it, and only then does esbuild run. So
// these tests boot with Generation.Disabled and read the boundary — a selector
// that resolves gets as far as "bundling is disabled", one that does not gets a
// diagnosis instead. That is the seam under test, and it keeps the scenario
// runnable anywhere.
//
// Reload routing is the same story: the dependency index maps a source file to
// the component names built from it, and the watcher walks that map to the hub.
// Recording those names directly is what the bundler does after a build, so the
// wiring can be exercised without one.
package shim_c

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	go_solid "github.com/lilybw/go-solid"
	shared_esbuild "github.com/lilybw/go-solid/shared/esbuild"
	"github.com/lilybw/go-solid/shared/logging"
)

// watchSettle is how long a filesystem event is given to reach a watcher.
// Generous, because CI filesystems are not quick and a flake here reads as a
// bug in reload routing.
const watchSettle = 3 * time.Second

type project struct {
	components string
}

// newProject stages the fixture components in a temp dir, so a test may edit
// them without disturbing the next one.
func newProject(t *testing.T) *project {
	t.Helper()

	components := filepath.Join(t.TempDir(), "components")
	if err := os.CopyFS(components, os.DirFS(filepath.Join("testdata", "frontend", "components"))); err != nil {
		t.Fatalf("stage components: %v", err)
	}
	return &project{components: components}
}

func (p *project) componentFile(rel string) string {
	return filepath.Join(p.components, filepath.FromSlash(rel))
}

func (p *project) boot(t *testing.T) *go_solid.Bundler {
	t.Helper()

	bundler, err := go_solid.New(&go_solid.Config{
		Components: p.components,
		LogLevel:   logging.LEVEL_ERROR,
		Generation: &shared_esbuild.BundlerConfig{Disabled: true},
	})
	if err != nil {
		t.Fatalf("go_solid.New: %v", err)
	}
	t.Cleanup(bundler.Close)
	return bundler
}

// resolutionOf renders a selector and reports what the render path made of it.
//
// With bundling off, a selector that resolves cannot produce HTML — it reaches
// esbuild and is turned away there. That refusal is the signal: it means every
// question about addressing was answered, and only the build was missing.
func resolutionOf(t *testing.T, b *go_solid.Bundler, selector string) error {
	t.Helper()
	_, err := b.Prepare(selector, nil).Render()
	if err == nil {
		t.Fatalf("Render(%q) succeeded with bundling disabled, which should be impossible", selector)
	}
	return err
}

// assertResolves says the selector named a component, and the only thing
// standing between it and a page is the bundler.
func assertResolves(t *testing.T, b *go_solid.Bundler, selector string) {
	t.Helper()
	err := resolutionOf(t, b, selector)
	if !strings.Contains(err.Error(), "bundling is disabled") {
		t.Errorf("Render(%q) failed before bundling, so it did not resolve:\n%v", selector, err)
	}
}

// assertRejected says the selector named nothing renderable, and the message
// explains which of the several possible mistakes was made.
func assertRejected(t *testing.T, b *go_solid.Bundler, selector string, wants ...string) {
	t.Helper()
	err := resolutionOf(t, b, selector)
	if strings.Contains(err.Error(), "bundling is disabled") {
		t.Fatalf("Render(%q) resolved; expected it to be turned away", selector)
	}
	for _, want := range wants {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Render(%q): message is missing %q:\n%v", selector, want, err)
		}
	}
}
