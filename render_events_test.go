package go_solid

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"

	logging "github.com/lilybw/go-solid/shared/logging"
	"github.com/lilybw/go-solid/shared/networking"
	"github.com/lilybw/go-solid/shared/networking/events"
)

// eventRecorder captures which events actually reached a handler during a
// render. Chains are dispatched on goroutines by Chains.Dispatch, so access
// must be guarded.
type eventRecorder struct {
	mu   sync.Mutex
	seen []events.EventType
}

func (er *eventRecorder) record(e events.NetworkingEvent) {
	er.mu.Lock()
	defer er.mu.Unlock()
	er.seen = append(er.seen, reflect.TypeOf(e))
}

func (er *eventRecorder) saw(want events.EventType) bool {
	er.mu.Lock()
	defer er.mu.Unlock()
	for _, got := range er.seen {
		if got == want {
			return true
		}
	}
	return false
}

func (er *eventRecorder) list() []events.EventType {
	er.mu.Lock()
	defer er.mu.Unlock()
	return append([]events.EventType(nil), er.seen...)
}

// watch registers an observer for every declared event type on a render call.
func (er *eventRecorder) watch(b RenderCallBuilder) RenderCallBuilder {
	return b.SetHTTPBehaviour(func(rb networking.RequestBehaviourBuilder) {
		for _, evType := range events.EVENTS.Values {
			rb.Upon(evType,
				func(_ http.ResponseWriter, _ *http.Request, e events.NetworkingEvent) error {
					er.record(e)
					return nil
				})
		}
	})
}

func bundlerWithoutGeneration(t *testing.T, files map[string]string) *Bundler {
	t.Helper()
	resetPackageState(t)
	b, err := New(&Config{
		LogLevel:   logging.LEVEL_ERROR,
		Components: componentsDirWith(t, files),
		Generation: disabledGeneration(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(b.Close)
	return b
}

// ---------------------------------------------------------------------------
// Failure events. None of these reach the compiler: each aborts the render
// before bundling starts.
// ---------------------------------------------------------------------------

func TestRender_FiresRegistryLookupFailure(t *testing.T) {
	b := bundlerWithoutGeneration(t, map[string]string{"Hello.tsx": "export default () => null;"})

	rec, req := httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil)
	er := &eventRecorder{}

	_, err := er.watch(b.Prepare("NoSuchComponent", nil)).ForRequest(rec, req).Render()
	if err == nil {
		t.Fatal("Render of an unregistered component returned no error")
	}
	if !er.saw(events.EVENTS.RegistryLookupFailure) {
		t.Errorf("RegistryLookupFailure never dispatched; saw %v", er.list())
	}
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
	if rec.Body.Len() == 0 {
		t.Error("registry lookup failed but nothing was written to the response")
	}
}

func TestRender_FiresPropsMarshalingFailure(t *testing.T) {
	b := bundlerWithoutGeneration(t, map[string]string{"Hello.tsx": "export default () => null;"})

	rec, req := httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil)
	er := &eventRecorder{}

	// A channel cannot be marshalled to JSON; this aborts before any bundling.
	_, err := er.watch(b.Prepare("Hello", make(chan int))).ForRequest(rec, req).Render()
	if err == nil {
		t.Fatal("Render with unmarshalable props returned no error")
	}
	if !er.saw(events.EVENTS.PropsMarshalingFailure) {
		t.Errorf("PropsMarshalingFailure never dispatched; saw %v", er.list())
	}
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

func TestRender_FiresCompBundlingFailure(t *testing.T) {
	// Generation.Disabled forbids bundling, so a cache miss is unrecoverable.
	// That refusal is dispatched on the CompBundlingFailure path.
	b := bundlerWithoutGeneration(t, map[string]string{"Hello.tsx": "export default () => null;"})

	rec, req := httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil)
	er := &eventRecorder{}

	_, err := er.watch(b.Prepare("Hello", nil)).ForRequest(rec, req).Render()
	if err == nil {
		t.Fatal("Render with bundling disabled returned no error")
	}
	if !strings.Contains(err.Error(), "bundling is disabled") {
		t.Errorf("unexpected error: %v", err)
	}
	if !er.saw(events.EVENTS.CompBundlingFailure) {
		t.Errorf("CompBundlingFailure never dispatched; saw %v", er.list())
	}
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

// A failing render must never leave the caller with a silent empty 200, which
// is the exact shape of the original bug.
func TestRender_FailureNeverProducesSilentEmpty200(t *testing.T) {
	b := bundlerWithoutGeneration(t, map[string]string{"Hello.tsx": "export default () => null;"})

	for name, build := range map[string]func() RenderCallBuilder{
		"unregistered component": func() RenderCallBuilder { return b.Prepare("Nope", nil) },
		"unmarshalable props":    func() RenderCallBuilder { return b.Prepare("Hello", make(chan int)) },
		"bundling disabled":      func() RenderCallBuilder { return b.Prepare("Hello", nil) },
	} {
		t.Run(name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/", nil)

			if _, err := build().ForRequest(rec, req).Render(); err == nil {
				t.Fatal("expected an error")
			}
			if rec.Code == http.StatusOK && rec.Body.Len() == 0 {
				t.Fatal("render failed but the client received an empty 200")
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Success path. Needs an installed solid-js for esbuild to resolve, so it skips
// rather than failing the suite on machines without one.
// ---------------------------------------------------------------------------

func TestRender_FiresTransmitAndWritesHTML(t *testing.T) {
	resetPackageState(t)
	b, err := New(&Config{
		LogLevel: logging.LEVEL_ERROR,
		Components: componentsDirWith(t, map[string]string{
			"Hello.tsx": "export default function Hello() { return <h1>hi</h1>; }",
		}),
	})
	if err != nil {
		t.Skipf("toolchain unavailable: %v", err)
	}
	t.Cleanup(b.Close)

	rec, req := httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil)
	er := &eventRecorder{}

	out, err := er.watch(b.Prepare("Hello", nil)).ForRequest(rec, req).Render()
	if err != nil {
		t.Skipf("bundling unavailable in this environment: %v", err)
	}

	if !er.saw(events.EVENTS.TransmitRenderedTemplate) {
		t.Errorf("TransmitRenderedTemplate never dispatched; saw %v", er.list())
	}
	if out == nil || out.HTML == "" {
		t.Fatal("Render returned no HTML")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if rec.Body.String() != out.HTML {
		t.Errorf("response body does not match the rendered HTML\n got %d bytes\nwant %d bytes",
			rec.Body.Len(), len(out.HTML))
	}
	if ct := rec.Header().Get("Content-Type"); ct == "" {
		t.Error("no Content-Type set on a rendered HTML response")
	}
}

// ---------------------------------------------------------------------------
// Builder wiring. These guard the two ways a correctly-configured handler can
// still end up never running.
// ---------------------------------------------------------------------------

// ForRequest must not discard handlers registered before it.
func TestForRequest_PreservesHandlersRegisteredEarlier(t *testing.T) {
	b := bundlerWithoutGeneration(t, map[string]string{"Hello.tsx": "export default () => null;"})

	rec, req := httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil)

	var called bool
	_, _ = b.Prepare("NoSuchComponent", nil).
		SetHTTPBehaviour(func(rb networking.RequestBehaviourBuilder) {
			rb.Upon(events.EVENTS.RegistryLookupFailure,
				func(http.ResponseWriter, *http.Request, events.NetworkingEvent) error {
					called = true
					return nil
				})
		}).
		ForRequest(rec, req).
		Render()

	if !called {
		t.Error("handler registered before ForRequest was never invoked: " +
			"ForRequest replaced the behaviour and dropped it")
	}
}

// Config.Defaults.Requests must reach the ForRequest path, not just
// SetHTTPBehaviour. This is the asymmetry with Defaults.HeadSegment.
func TestForRequest_AppliesConfiguredRequestDefaults(t *testing.T) {
	resetPackageState(t)

	var called bool
	b, err := New(&Config{
		LogLevel:   logging.LEVEL_ERROR,
		Components: componentsDirWith(t, map[string]string{"Hello.tsx": "export default () => null;"}),
		Generation: disabledGeneration(),
		Defaults: &BehaviouralDefaults{
			Requests: func(rb networking.RequestBehaviourBuilder) {
				rb.Upon(events.EVENTS.RegistryLookupFailure,
					func(http.ResponseWriter, *http.Request, events.NetworkingEvent) error {
						called = true
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
	_, _ = b.Prepare("NoSuchComponent", nil).ForRequest(rec, req).Render()

	if !called {
		t.Error("Config.Defaults.Requests was never applied on the ForRequest path")
	}
}

// Render without ForRequest is legal (caller writes the HTML itself) and must
// not touch a nil behaviour.
func TestRender_WithoutRequestDoesNotDispatch(t *testing.T) {
	b := bundlerWithoutGeneration(t, map[string]string{"Hello.tsx": "export default () => null;"})

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Render without ForRequest panicked: %v", r)
		}
	}()
	if _, err := b.Prepare("NoSuchComponent", nil).Render(); err == nil {
		t.Fatal("expected an error for an unregistered component")
	}
}

// A handler that fails must surface as an error from Render, not as a panic in
// the request goroutine.
func TestRender_HandlerErrorIsReturnedNotPanicked(t *testing.T) {
	b := bundlerWithoutGeneration(t, map[string]string{"Hello.tsx": "export default () => null;"})

	rec, req := httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil)

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("a failing handler panicked instead of returning an error: %v", r)
		}
	}()

	_, err := b.Prepare("NoSuchComponent", nil).
		SetHTTPBehaviour(func(rb networking.RequestBehaviourBuilder) {
			rb.Upon(events.EVENTS.RegistryLookupFailure,
				func(http.ResponseWriter, *http.Request, events.NetworkingEvent) error {
					return http.ErrHandlerTimeout
				})
		}).
		ForRequest(rec, req).
		Render()

	if err == nil {
		t.Fatal("expected an error")
	}
}
