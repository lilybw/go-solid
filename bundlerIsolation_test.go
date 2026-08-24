package go_solid

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	caching "github.com/lilybw/go-solid/internal/caching"
	logging "github.com/lilybw/go-solid/shared/logging"
	shared_net "github.com/lilybw/go-solid/shared/networking"
	"github.com/lilybw/go-solid/shared/networking/events"
	shared_raster "github.com/lilybw/go-solid/shared/rasterization"
)

// ---------------------------------------------------------------------------
// Behavioural defaults belong to a Bundler.
//
// They used to be package-level: the second New in a program silently rewrote
// the first one's head template and request handlers, and a New running while
// another Bundler served traffic raced with every render reading them.
// ---------------------------------------------------------------------------

func bundlerWithDefaults(t *testing.T, defaults *BehaviouralDefaults) *Bundler {
	t.Helper()
	b, err := New(&Config{
		LogLevel:   logging.LEVEL_ERROR,
		Components: componentsDirWith(t, map[string]string{"Hello.tsx": "export default () => null;"}),
		Generation: disabledGeneration(),
		Defaults:   defaults,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(b.Close)
	return b
}

func TestDefaults_HeadTemplateIsPerBundler(t *testing.T) {
	resetPackageState(t)

	first := bundlerWithDefaults(t, &BehaviouralDefaults{
		HeadSegment: func(h shared_net.HTMLHeadSegmentBuilder) { h.SetTitle("first") },
	})
	second := bundlerWithDefaults(t, &BehaviouralDefaults{
		HeadSegment: func(h shared_net.HTMLHeadSegmentBuilder) { h.SetTitle("second") },
	})

	for label, b := range map[string]*Bundler{"first": first, "second": second} {
		got := b.defaults.NewHTMLHeadSegmentBuilder().Build()
		if !strings.Contains(got, "<title>"+label+"</title>") {
			t.Errorf("%s bundler's head template was overwritten by the other:\n%s", label, got)
		}
	}
}

// A Bundler configured with no defaults keeps the library ones rather than
// inheriting whatever the last Bundler constructed happened to set.
func TestDefaults_UnconfiguredBundlerKeepsTheLibraryHead(t *testing.T) {
	resetPackageState(t)

	bundlerWithDefaults(t, &BehaviouralDefaults{
		HeadSegment: func(h shared_net.HTMLHeadSegmentBuilder) { h.SetTitle("configured") },
	})
	plain := bundlerWithDefaults(t, nil)

	if got := plain.defaults.NewHTMLHeadSegmentBuilder().Build(); !strings.Contains(got, "<title>go-solid</title>") {
		t.Errorf("an unconfigured Bundler inherited another's head template:\n%s", got)
	}
}

func TestDefaults_RequestTemplateIsPerBundler(t *testing.T) {
	resetPackageState(t)

	var firstRan, secondRan atomic.Int64
	count := func(c *atomic.Int64) func(shared_net.RequestBehaviourBuilder) {
		return func(rb shared_net.RequestBehaviourBuilder) {
			rb.UponSpecialized(events.EVENTS.RegistryLookupFailure, shared_net.HANDLER_MODE_PARALLEL,
				func(http.ResponseWriter, *http.Request, events.NetworkingEvent) error {
					c.Add(1)
					return nil
				})
		}
	}

	_ = bundlerWithDefaults(t, &BehaviouralDefaults{Requests: count(&firstRan)})
	second := bundlerWithDefaults(t, &BehaviouralDefaults{Requests: count(&secondRan)})

	rec, req := httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil)
	_, _ = second.Prepare("Missing", nil).ForRequest(rec, req).Render()

	if secondRan.Load() != 1 {
		t.Errorf("second bundler's request default ran %d times, want 1", secondRan.Load())
	}
	if firstRan.Load() != 0 {
		t.Errorf("first bundler's request default ran %d times for a render on the second", firstRan.Load())
	}
}

// Constructing a Bundler while another serves traffic used to write the shared
// template out from under every in-flight render. Run with -race.
func TestDefaults_ConstructionDoesNotRaceWithRendering(t *testing.T) {
	resetPackageState(t)

	serving := bundlerWithDefaults(t, &BehaviouralDefaults{
		HeadSegment: func(h shared_net.HTMLHeadSegmentBuilder) { h.SetTitle("serving") },
	})

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for range 40 {
			_ = serving.defaults.NewHTMLHeadSegmentBuilder().Build()
		}
	}()
	go func() {
		defer wg.Done()
		for range 8 {
			bundlerWithDefaults(t, &BehaviouralDefaults{
				HeadSegment: func(h shared_net.HTMLHeadSegmentBuilder) { h.SetTitle("other") },
			})
		}
	}()
	wg.Wait()

	if got := serving.defaults.NewHTMLHeadSegmentBuilder().Build(); !strings.Contains(got, "<title>serving</title>") {
		t.Errorf("the serving Bundler's head template was rewritten:\n%s", got)
	}
}

// ---------------------------------------------------------------------------
// Rasterization.Location.
// ---------------------------------------------------------------------------

// Location is an override: it moves the component cache out of the workspace so
// a pre-built one can be produced into, and shipped from, somewhere else.
func TestRasterization_LocationMovesTheComponentCache(t *testing.T) {
	resetPackageState(t)

	comps := componentsDirWith(t, map[string]string{"Hello.tsx": "export default () => null;"})
	workspace := t.TempDir()
	elsewhere := t.TempDir()

	b, err := New(&Config{
		LogLevel:   logging.LEVEL_ERROR,
		Components: comps,
		Workspace:  workspace,
		Generation: disabledGeneration(),
		// Disabled, so New does not try to pre-build without a bundler. Location
		// names where the cache lives either way.
		Rasterization: &shared_raster.RasterizationConfig{Location: elsewhere, Disabled: true},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(b.Close)

	want := filepath.Join(elsewhere, caching.CACHE_DIR_NAME)
	if got := b.disk.Directory(); got != want {
		t.Errorf("cache directory = %q, want %q: Location did not move it", got, want)
	}
	if _, err := os.Stat(want); err != nil {
		t.Errorf("cache directory was not created at Location: %v", err)
	}
	if _, err := os.Stat(filepath.Join(workspace, caching.CACHE_DIR_NAME)); err == nil {
		t.Error("a cache directory was created in the workspace despite Location pointing elsewhere")
	}
}

// Without the override the cache stays in the workspace.
func TestRasterization_CacheDefaultsToTheWorkspace(t *testing.T) {
	resetPackageState(t)

	workspace := t.TempDir()
	b, err := New(&Config{
		LogLevel:   logging.LEVEL_ERROR,
		Components: componentsDirWith(t, map[string]string{"Hello.tsx": "export default () => null;"}),
		Workspace:  workspace,
		Generation: disabledGeneration(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(b.Close)

	if got, want := b.disk.Directory(), filepath.Join(workspace, caching.CACHE_DIR_NAME); got != want {
		t.Errorf("cache directory = %q, want %q", got, want)
	}
}

// A cache staged at Location is what an ExpectCompleted Bundler reads from,
// which is the whole point of being able to move it.
func TestRasterization_ExpectCompletedReadsFromLocation(t *testing.T) {
	resetPackageState(t)

	elsewhere := t.TempDir()
	stageRasterizedWorkspace(t, elsewhere)

	b, err := New(&Config{
		LogLevel:      logging.LEVEL_ERROR,
		Components:    componentsDirWith(t, map[string]string{"Hello.tsx": "export default () => null;"}),
		Workspace:     t.TempDir(), // deliberately not where the cache is
		Generation:    disabledGeneration(),
		Rasterization: &shared_raster.RasterizationConfig{Location: elsewhere, ExpectCompleted: true},
	})
	if err != nil {
		t.Fatalf("New with a cache staged at Location: %v", err)
	}
	t.Cleanup(b.Close)

	if got, want := b.disk.Directory(), filepath.Join(elsewhere, caching.CACHE_DIR_NAME); got != want {
		t.Errorf("cache directory = %q, want %q", got, want)
	}
}
