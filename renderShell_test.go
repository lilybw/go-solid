package go_solid

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	caching "github.com/lilybw/go-solid/internal/caching"
	code_gen "github.com/lilybw/go-solid/internal/code-gen"
	networking_int "github.com/lilybw/go-solid/internal/networking"
	shared_esbuild "github.com/lilybw/go-solid/shared/esbuild"
	logging "github.com/lilybw/go-solid/shared/logging"
	networking "github.com/lilybw/go-solid/shared/networking"
	"github.com/lilybw/go-solid/shared/networking/events"
)

// ---------------------------------------------------------------------------
// The mount root on a cache hit.
//
// The shell names the element the bundle mounts on. The bundle takes that id
// from the component at generation time; the shell takes it from renderData,
// which only learns it from the registry on the way to a build. Anything that
// returns before that point has to have resolved it already, or the two
// disagree and the page mounts nothing — a failure that appears only once the
// cache is warm, which is to say never in development and always in production.
// ---------------------------------------------------------------------------

// seedArtifact puts a stand-in bundle in the memory cache under the key the
// render path is expected to look for, so the warm path can be exercised with
// bundling switched off.
func seedArtifact(t *testing.T, b *Bundler, component string) string {
	t.Helper()
	comp, ok := b.Registry().Lookup(component)
	if !ok {
		t.Fatalf("%q is not registered", component)
	}
	b.mem.Put(
		caching.NewBuildCacheKey(component, comp.MountRootID, b.buildID),
		&caching.Rendered{JS: "/* cached */", JSName: component + ".cached.js"},
	)
	return comp.MountRootID
}

func TestRender_WarmRenderNamesTheComponentsMountRoot(t *testing.T) {
	b := bundlerWithoutGeneration(t, map[string]string{
		"Hello.tsx": "export default () => null;",
	})
	root := seedArtifact(t, b, "Hello")

	out, err := b.Prepare("Hello", nil).Render()
	if err != nil {
		t.Fatalf("Render off a warm cache: %v", err)
	}
	if !strings.Contains(out.HTML, fmt.Sprintf(`<div id="%s">`, root)) {
		t.Errorf("shell does not mount on %q; the cached bundle will find no root.\n%s", root, out.HTML)
	}
	if strings.Contains(out.HTML, `<div id="">`) {
		t.Errorf("shell emitted an empty root id:\n%s", out.HTML)
	}
	if !strings.Contains(out.HTML, fmt.Sprintf(`<script id="props-%s"`, root)) {
		t.Errorf("data island id does not track the mount root:\n%s", out.HTML)
	}
}

// MountOnRootID overrides the component's default. It has to reach the shell on
// the warm path too, and it has to key separately: two roots are two shells.
func TestRender_ExplicitMountRootReachesTheShell(t *testing.T) {
	b := bundlerWithoutGeneration(t, map[string]string{
		"Hello.tsx": "export default () => null;",
	})
	const custom = "my-app-root"
	b.mem.Put(
		caching.NewBuildCacheKey("Hello", custom, b.buildID),
		&caching.Rendered{JS: "/* cached */", JSName: "Hello.cached.js"},
	)

	out, err := b.Prepare("Hello", nil).MountOnRootID(custom).Render()
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(out.HTML, `<div id="my-app-root">`) {
		t.Errorf("MountOnRootID did not reach the shell:\n%s", out.HTML)
	}
	if !strings.Contains(out.HTML, `<script id="props-my-app-root"`) {
		t.Errorf("data island did not follow the explicit root:\n%s", out.HTML)
	}
}

// An artifact cached for one root must not answer for another: the key that
// stored it named the root, so the key that looks it up has to as well.
func TestRender_ArtifactCachedForOneRootDoesNotServeAnother(t *testing.T) {
	b := bundlerWithoutGeneration(t, map[string]string{
		"Hello.tsx": "export default () => null;",
	})
	seedArtifact(t, b, "Hello")

	// Bundling is off, so a miss is an error — which is exactly the assertion.
	if _, err := b.Prepare("Hello", nil).MountOnRootID("somewhere-else").Render(); err == nil {
		t.Error("a different mount root was answered from the entry cached for the default one")
	}
}

// A component dropped from the registry must stop rendering, even while its
// artifact is still in the cache. Resolving the registry before the lookup is
// what makes that true; consulting the cache first would keep serving it until
// something got round to invalidating.
func TestRender_CacheDoesNotOutliveTheRegistryEntry(t *testing.T) {
	b := bundlerWithoutGeneration(t, map[string]string{
		"Hello.tsx": "export default () => null;",
	})
	seedArtifact(t, b, "Hello")

	comp, _ := b.Registry().Lookup("Hello")
	if _, err := b.Prepare("Hello", nil).Render(); err != nil {
		t.Fatalf("warm render before the drop: %v", err)
	}

	// Drop the registration but leave the caches alone.
	if _, removed := b.Registry().RemoveFile(comp.Path); !removed {
		t.Fatal("RemoveFile did not drop the component")
	}
	if _, err := b.Prepare("Hello", nil).Render(); err == nil {
		t.Error("a deregistered component still rendered out of the cache")
	}
}

// ---------------------------------------------------------------------------
// Build settings are part of what an artifact is.
// ---------------------------------------------------------------------------

// Nothing but the fingerprint distinguishes a minified bundle from a readable
// one, so without it flipping Minify serves the bundle built before the change.
func TestBuildFingerprint_TracksTheSettingsThatChangeTheOutput(t *testing.T) {
	base := func() *shared_esbuild.BundlerConfig {
		return &shared_esbuild.BundlerConfig{Solid: shared_esbuild.SolidConfig{ModuleName: "solid-js/web"}}
	}
	want := buildFingerprint(base())

	if got := buildFingerprint(base()); got != want {
		t.Errorf("fingerprint is not stable: %q != %q", got, want)
	}

	for name, mutate := range map[string]func(*shared_esbuild.BundlerConfig){
		"minify":      func(c *shared_esbuild.BundlerConfig) { c.Minify = true },
		"sourcemap":   func(c *shared_esbuild.BundlerConfig) { c.Sourcemap = 1 },
		"development": func(c *shared_esbuild.BundlerConfig) { c.Solid.Development = true },
		"runtime":     func(c *shared_esbuild.BundlerConfig) { c.Solid.Runtime = shared_esbuild.RuntimeExternal },
		"delegation":  func(c *shared_esbuild.BundlerConfig) { c.Solid.DisableEventDelegation = true },
		"module name": func(c *shared_esbuild.BundlerConfig) { c.Solid.ModuleName = "./wrapper" },
		"override":    func(c *shared_esbuild.BundlerConfig) { c.Solid.RuntimeOverride = map[string]string{"solid-js/store": "x"} },
	} {
		cfg := base()
		mutate(cfg)
		if buildFingerprint(cfg) == want {
			t.Errorf("changing %s left the fingerprint unchanged; the cache would serve a stale bundle", name)
		}
	}

	// Neither of these alters the bytes of a build that happens.
	for name, mutate := range map[string]func(*shared_esbuild.BundlerConfig){
		"dependencies": func(c *shared_esbuild.BundlerConfig) { c.Dependencies = "/somewhere/else" },
		"disabled":     func(c *shared_esbuild.BundlerConfig) { c.Disabled = true },
	} {
		cfg := base()
		mutate(cfg)
		if buildFingerprint(cfg) != want {
			t.Errorf("changing %s invalidated the cache needlessly", name)
		}
	}
}

func TestBuildFingerprint_SurvivesMapOrdering(t *testing.T) {
	// fmt sorts map keys, so a many-keyed override must still fingerprint the
	// same way every time rather than once per iteration order.
	cfg := &shared_esbuild.BundlerConfig{Solid: shared_esbuild.SolidConfig{
		RuntimeOverride: map[string]string{"a": "1", "b": "2", "c": "3", "d": "4", "e": "5"},
	}}
	want := buildFingerprint(cfg)
	for range 50 {
		if got := buildFingerprint(cfg); got != want {
			t.Fatalf("fingerprint varies with map iteration order: %q != %q", got, want)
		}
	}
}

// ---------------------------------------------------------------------------
// Builder wiring.
// ---------------------------------------------------------------------------

// SetHTTPBehaviour builds a behaviour with no writer and no request. Rendering
// one is legal — the caller takes the HTML and writes it itself — so nothing on
// the render path may assume a request is there.
func TestSetHTTPBehaviour_WithoutForRequestRenders(t *testing.T) {
	b := bundlerWithoutGeneration(t, map[string]string{
		"Hello.tsx": "export default () => null;",
	})
	seedArtifact(t, b, "Hello")

	var dispatched bool
	out, err := b.Prepare("Hello", nil).
		SetHTTPBehaviour(func(rb networking.RequestBehaviourBuilder) {
			rb.UponSpecialized(events.EVENTS.TransmitRenderedTemplate, networking.HANDLER_MODE_PARALLEL,
				func(http.ResponseWriter, *http.Request, events.NetworkingEvent) error {
					dispatched = true
					return nil
				})
		}).
		Render()

	if err != nil {
		t.Fatalf("Render with a behaviour but no request: %v", err)
	}
	if out == nil || out.HTML == "" {
		t.Fatal("no HTML returned")
	}
	if !dispatched {
		t.Error("the transmit event never reached the handler")
	}
}

// The same, with an explicit context: a behaviour that carries no request must
// not shadow WithCtx.
func TestSetHTTPBehaviour_WithoutForRequestHonoursWithCtx(t *testing.T) {
	b := bundlerWithoutGeneration(t, map[string]string{
		"Hello.tsx": "export default () => null;",
	})
	seedArtifact(t, b, "Hello")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := b.Prepare("Hello", nil).
		SetHTTPBehaviour(func(networking.RequestBehaviourBuilder) {}).
		WithCtx(ctx).
		Render(); err == nil {
		t.Error("a cancelled context was ignored because a request-less behaviour shadowed it")
	}
}

// Config.Defaults.Requests is applied when the behaviour is constructed.
// Constructing it twice — which both entry points used to do independently —
// registers every default handler twice and runs it twice per event.
func TestDefaults_RequestsAreAppliedExactlyOncePerRenderCall(t *testing.T) {
	for _, order := range []struct {
		name  string
		build func(RenderCallBuilder, http.ResponseWriter, *http.Request) RenderCallBuilder
	}{
		{"ForRequest only", func(rc RenderCallBuilder, w http.ResponseWriter, r *http.Request) RenderCallBuilder {
			return rc.ForRequest(w, r)
		}},
		{"SetHTTPBehaviour then ForRequest", func(rc RenderCallBuilder, w http.ResponseWriter, r *http.Request) RenderCallBuilder {
			return rc.SetHTTPBehaviour(func(networking.RequestBehaviourBuilder) {}).ForRequest(w, r)
		}},
		{"ForRequest then SetHTTPBehaviour", func(rc RenderCallBuilder, w http.ResponseWriter, r *http.Request) RenderCallBuilder {
			return rc.ForRequest(w, r).SetHTTPBehaviour(func(networking.RequestBehaviourBuilder) {})
		}},
	} {
		t.Run(order.name, func(t *testing.T) {
			resetPackageState(t)

			// Chains dispatch on goroutines, and a doubly-registered handler
			// would be two of them incrementing at once.
			var calls atomic.Int64
			b, err := New(&Config{
				LogLevel:   logging.LEVEL_ERROR,
				Components: componentsDirWith(t, map[string]string{"Hello.tsx": "export default () => null;"}),
				Generation: disabledGeneration(),
				Defaults: &BehaviouralDefaults{
					Requests: func(rb networking.RequestBehaviourBuilder) {
						rb.UponSpecialized(events.EVENTS.RegistryLookupFailure, networking.HANDLER_MODE_PARALLEL,
							func(http.ResponseWriter, *http.Request, events.NetworkingEvent) error {
								calls.Add(1)
								return nil
							})
					},
				},
			})
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			t.Cleanup(b.Close)

			rec, req := httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil)
			_, _ = order.build(b.Prepare("NoSuchComponent", nil), rec, req).Render()

			if got := calls.Load(); got != 1 {
				t.Errorf("Defaults.Requests handler ran %d times, want 1", got)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Escaping in the shell.
// ---------------------------------------------------------------------------

// The HTML parser ends a <script> block at the first "</script", matched
// without regard to case. A bundle holding "</SCRIPT>" in a string literal
// would otherwise close the module block early and spill the rest as markup.
func TestAssembleHTML_EscapesScriptCloseInAnyCase(t *testing.T) {
	head := networking_int.NewHTMLHeadSegmentBuilder().DeterministicOutput()

	// The shell closes exactly two script elements of its own: the data island
	// and the module block. A third means the literal opened its way out.
	const ownCloses = 2

	for _, literal := range []string{"</script>", "</SCRIPT>", "</Script>", "</ScRiPt >"} {
		js := `const s = "` + literal + `";`
		html := code_gen.AssembleHTML(head, "{}", &caching.Rendered{JS: js}, "root", "")

		if got := strings.Count(strings.ToLower(html), "</script"); got != ownCloses {
			t.Errorf("%q leaked a script close: found %d, want %d\n%s", literal, got, ownCloses, html)
		}
		if !strings.Contains(html, `<\/`) {
			t.Errorf("%q was not escaped at all:\n%s", literal, html)
		}
	}
}

// Escaping must not rewrite what the string evaluates to: only the slash is
// escaped, and the casing of the tag name is left alone.
func TestAssembleHTML_ScriptEscapePreservesCasing(t *testing.T) {
	head := networking_int.NewHTMLHeadSegmentBuilder().DeterministicOutput()
	html := code_gen.AssembleHTML(head, "{}",
		&caching.Rendered{JS: `const s = "</SCRIPT>";`}, "root", "")

	if !strings.Contains(html, `<\/SCRIPT>`) {
		t.Errorf("escaping changed the literal's casing:\n%s", html)
	}
}

// The data island is a non-executed context, but the parser still ends it at
// "</", so props carrying markup must not be able to break out.
func TestAssembleHTML_PropsCannotEscapeTheDataIsland(t *testing.T) {
	head := networking_int.NewHTMLHeadSegmentBuilder().DeterministicOutput()
	props := `{"bio":"</script><img src=x onerror=alert(1)>"}`
	html := code_gen.AssembleHTML(head, props, &caching.Rendered{JS: ""}, "root", "")

	if strings.Contains(html, "</script><img") {
		t.Errorf("props escaped the data island:\n%s", html)
	}
	if !strings.Contains(html, `<\/script>`) {
		t.Errorf("props were not escaped:\n%s", html)
	}
}
