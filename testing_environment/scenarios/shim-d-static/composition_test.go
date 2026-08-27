package shim_d

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	go_solid "github.com/lilybw/go-solid"
	shared_esbuild "github.com/lilybw/go-solid/shared/esbuild"
	"github.com/lilybw/go-solid/shared/logging"
	shared_net "github.com/lilybw/go-solid/shared/networking"
	"github.com/lilybw/go-solid/shared/networking/events"
	shared_types "github.com/lilybw/go-solid/shared/types"
)

// Where features meet
// ---------------------------------------------------------------------------
// Selectors decide which component a name resolves to. Type checking decides
// whether the props handed to it fit. Request behaviour decides what the caller
// sees when either says no. Each is tested alone elsewhere; what is left is
// whether they agree about the same render.

// A selector resolves to a component in a file that imports static assets, and
// the props are checked against the export the selector named — not against the
// file's default one, and not against its sibling.
func TestSelectorAndTypeCheckingAgreeOnWhichExport(t *testing.T) {
	p := newProject(t)
	b := p.boot(t, options{static: true, check: shared_types.CHECK_RUNTIME})

	// Header and Footer both take PanelProps; Dashboard takes DashboardProps.
	assertResolves(t, b, "panels/Sidebar#Header", map[string]any{"label": "hi"})
	assertResolves(t, b, "Dashboard", map[string]any{"title": "hi"})

	// The selector's own contract is what is enforced, so the sibling's props
	// are the wrong props.
	assertStops(t, b, "panels/Sidebar#Header", map[string]any{"title": "hi"}, "label")
	assertStops(t, b, "Dashboard", map[string]any{"label": "hi"}, "title")
}

// The file exports something that is not a component. Resolution has to say so
// before the type checker is asked what its props are.
func TestANonComponentExportIsRejectedBeforeItIsTypeChecked(t *testing.T) {
	p := newProject(t)
	b := p.boot(t, options{static: true, check: shared_types.CHECK_RUNTIME})

	err := assertStops(t, b, "panels/Sidebar#TITLE", map[string]any{"label": "hi"}, "not a component")
	if strings.Contains(err.Error(), "label") {
		t.Errorf("the props were checked against something that is not a component:\n%v", err)
	}
}

// A props mismatch is a development failure: a fault in how the library was
// used, not in serving the request. It belongs to two buckets at once, and a
// handler for either has to see it.
func TestAPropsMismatchReachesBothEventBuckets(t *testing.T) {
	p := newProject(t)
	b := p.boot(t, options{check: shared_types.CHECK_RUNTIME})

	var development, failure, success atomic.Int64
	rec, req := httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil)

	_, _ = b.Prepare("Dashboard", map[string]any{"wrong": true}).
		SetHTTPBehaviour(func(rb shared_net.RequestBehaviourBuilder) {
			rb.Upon(events.EVENTS.DevelopmentFailureEvent,
				func(http.ResponseWriter, *http.Request, events.NetworkingEvent) error {
					development.Add(1)
					return nil
				})
			rb.Upon(events.EVENTS.FailureEvent,
				func(http.ResponseWriter, *http.Request, events.NetworkingEvent) error {
					failure.Add(1)
					return nil
				})
			rb.Upon(events.EVENTS.SuccessEvent,
				func(http.ResponseWriter, *http.Request, events.NetworkingEvent) error {
					success.Add(1)
					return nil
				})
		}).
		ForRequest(rec, req).
		Render()

	if development.Load() == 0 {
		t.Error("the development bucket never saw the props mismatch")
	}
	if failure.Load() == 0 {
		t.Error("the failure bucket was skipped; a development failure is still a failure")
	}
	if success.Load() != 0 {
		t.Error("a success handler ran for a failed render")
	}
	if rec.Code == http.StatusOK {
		t.Errorf("status = %d; a failed render must not look like a page", rec.Code)
	}
	if rec.Body.Len() == 0 {
		t.Error("nothing was written, so the caller sees an empty 500 with no reason")
	}
}

// Defaults are per-Bundler, and the ones a render call adds sit alongside them
// rather than replacing them. Both have to run, once each.
func TestConfiguredDefaultsAndPerCallHandlersBothRunOnce(t *testing.T) {
	p := newProject(t)

	var fromDefaults, fromCall atomic.Int64
	b, err := go_solid.New(&go_solid.Config{
		Components: p.components,
		LogLevel:   logging.LEVEL_ERROR,
		Generation: &shared_esbuild.BundlerConfig{Disabled: true},
		Defaults: &go_solid.BehaviouralDefaults{
			Requests: func(rb shared_net.RequestBehaviourBuilder) {
				rb.Upon(events.EVENTS.RegistryLookupFailure,
					func(http.ResponseWriter, *http.Request, events.NetworkingEvent) error {
						fromDefaults.Add(1)
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
	_, _ = b.Prepare("NoSuchComponent", nil).
		ForRequest(rec, req).
		SetHTTPBehaviour(func(rb shared_net.RequestBehaviourBuilder) {
			rb.Upon(events.EVENTS.RegistryLookupFailure,
				func(http.ResponseWriter, *http.Request, events.NetworkingEvent) error {
					fromCall.Add(1)
					return nil
				})
		}).
		Render()

	if got := fromDefaults.Load(); got != 1 {
		t.Errorf("the configured default ran %d times, want 1", got)
	}
	if got := fromCall.Load(); got != 1 {
		t.Errorf("the per-call handler ran %d times, want 1", got)
	}
}

// A component that imports static assets is still a component. Switching the
// feature off changes what the import resolves to, not whether the component
// resolves.
func TestComponentsImportingAssetsResolveWithTheFeatureOff(t *testing.T) {
	p := newProject(t)
	b := p.boot(t, options{check: shared_types.CHECK_RUNTIME})

	assertResolves(t, b, "Dashboard", map[string]any{"title": "hi"})
	assertResolves(t, b, "panels/Sidebar#Footer", map[string]any{"label": "hi"})
}
