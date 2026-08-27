package networking

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lilybw/go-solid/internal/caching"
	shared "github.com/lilybw/go-solid/shared/networking"
	"github.com/lilybw/go-solid/shared/networking/events"
)

// newBoundRequestData is the shape every render path ends up using: a behaviour
// bound to a real writer, carrying whatever defaults NewRequestData installs.
func newBoundRequestData(t *testing.T) (*shared.RequestBehaviour, *httptest.ResponseRecorder) {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	return NewRequestData(rec, req), rec
}

func rendered(html string) *caching.Rendered {
	return &caching.Rendered{HTML: html, JSName: "X.deadbeef.js"}
}

// ---------------------------------------------------------------------------
// 1. The defaults exist at all.
//
// This is the regression test for the original bug: NewRequestData returned an
// empty HandlerMap, so Dispatch found nothing, nothing was ever written to
// the ResponseWriter, and the caller saw a 200 with an empty body.
// ---------------------------------------------------------------------------

func TestNewRequestData_InstallsDefaultHandlers(t *testing.T) {
	data, _ := newBoundRequestData(t)

	if data.Handlers == nil {
		t.Fatal("NewRequestData returned a behaviour with a nil HandlerMap")
	}

	for _, tc := range []struct {
		name   string
		lookup func() (shared.Chain, bool)
	}{
		{"TransmitRenderedTemplate", func() (shared.Chain, bool) {
			return data.Handlers.Get[events.TransmitRenderedTemplateEvent]()
		}},
		{"PropsMarshalingFailure", func() (shared.Chain, bool) {
			return data.Handlers.Get[events.PropsMarshalingFailureEvent]()
		}},
		{"RegistryLookupFailure", func() (shared.Chain, bool) {
			return data.Handlers.Get[events.RegistryLookupFailureEvent]()
		}},
		{"EntryGenerationFailure", func() (shared.Chain, bool) {
			return data.Handlers.Get[events.EntryGenerationFailureEvent]()
		}},
		{"TempEntryWriteFailure", func() (shared.Chain, bool) {
			return data.Handlers.Get[events.TempEntryWriteFailureEvent]()
		}},
		{"CompBundlingFailure", func() (shared.Chain, bool) {
			return data.Handlers.Get[events.CompBundlingFailureEvent]()
		}},
		{"CompPropsInsufficientFailure", func() (shared.Chain, bool) {
			return data.Handlers.Get[events.CompPropsInsufficientFailureEvent]()
		}},
	} {
		chains, ok := tc.lookup()
		if !ok {
			t.Errorf("no default handler registered for %s", tc.name)
			continue
		}
		if len(chains) == 0 {
			t.Errorf("%s: handler slot present but empty", tc.name)
			continue
		}
		for i, h := range chains {
			if h == nil {
				t.Errorf("%s: handler [%d] is nil", tc.name, i)
			}
		}
	}
}

// Every event the library can emit must reach a handler. If a new event type is
// added to EVENTS.Concrete without a default, this fails rather than silently
// producing an empty response in production.
func TestEveryDeclaredEventReachesAHandler(t *testing.T) {
	data, _ := newBoundRequestData(t)

	registered := make(map[events.EventType]bool, data.Handlers.Len())
	for _, key := range data.Handlers.Types() {
		registered[key] = true
	}

	for _, evType := range events.EVENTS.Concrete {
		if !registered[evType] {
			t.Errorf("event %s has no default responder registered: emitting it would "+
				"write nothing to the response", evType)
		}
	}
}

// ---------------------------------------------------------------------------
// 2. The defaults do the right thing when dispatched.
// ---------------------------------------------------------------------------

func TestDefaultTransmitHandler_WritesHTMLAndStatus200(t *testing.T) {
	data, rec := newBoundRequestData(t)

	const html = "<!doctype html><html><body>hello</body></html>"
	if err := data.Dispatch(events.NewTransmitRenderedTemplate(rendered(html))); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if got := rec.Body.String(); got != html {
		t.Errorf("body = %q, want exactly the rendered HTML %q", got, html)
	}
}

func TestDefaultFailureHandler_WritesErrorAndStatus500(t *testing.T) {
	failures := []struct {
		name  string
		event events.NetworkingEvent
	}{
		{"PropsMarshalingFailure", events.NewPropsMarshalingFailure(errors.New("boom-props"))},
		{"RegistryLookupFailure", events.NewRegistryLookupFailure(errors.New("boom-registry"))},
		{"EntryGenerationFailure", events.NewEntryGenerationFailure(errors.New("boom-entry"))},
		{"TempEntryWriteFailure", events.NewTempEntryWriteFailure(errors.New("boom-temp"))},
		{"CompBundlingFailure", events.NewCompBundlingFailure(errors.New("boom-bundle"))},
		{"CompPropsInsufficientFailure", events.NewCompPropsInsufficientFailure(errors.New("boom-props-type"))},
	}

	for _, tc := range failures {
		t.Run(tc.name, func(t *testing.T) {
			data, rec := newBoundRequestData(t)

			if err := data.Dispatch(tc.event); err != nil {
				t.Fatalf("Dispatch: %v", err)
			}

			if rec.Code != http.StatusInternalServerError {
				t.Errorf("status = %d, want 500 (a failure event must not report success)", rec.Code)
			}
			if rec.Body.Len() == 0 {
				t.Error("failure produced an empty body; the caller gets no diagnostic")
			}
		})
	}
}

// A success event must not also be handled as a failure, and vice versa. With
// the interface-keyed buckets in Dispatch it is easy for one event to be
// picked up twice and write two bodies into one response.
func TestDispatchWritesExactlyOneBody(t *testing.T) {
	const html = "<html>only-me</html>"

	data, rec := newBoundRequestData(t)
	if err := data.Dispatch(events.NewTransmitRenderedTemplate(rendered(html))); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	body := rec.Body.String()
	if body != html {
		t.Errorf("body = %q, want %q\n"+
			"a specific handler and an interface-bucket handler both ran and "+
			"concatenated their output into one response", body, html)
	}
}

// ---------------------------------------------------------------------------
// 3. The defaults must survive user configuration.
// ---------------------------------------------------------------------------

func TestUserHandlersDoNotDisplaceDefaults(t *testing.T) {
	data, _ := newBoundRequestData(t)

	var called bool
	NewRequestBehaviourBuilder(data).
		Upon(events.EVENTS.PropsMarshalingFailure, func(http.ResponseWriter, *http.Request, events.NetworkingEvent) error {
			called = true
			return nil
		})

	if !data.Handlers.Has[events.TransmitRenderedTemplateEvent]() ||
		!data.Handlers.Has[events.RegistryLookupFailureEvent]() ||
		!data.Handlers.Has[events.CompBundlingFailureEvent]() {
		t.Error("registering a user handler dropped a default responder")
	}

	if err := data.Dispatch(events.NewPropsMarshalingFailure(errors.New("x"))); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if !called {
		t.Error("user handler was registered but never invoked")
	}
}

// A user-supplied template (Config.Defaults.Requests) is applied through
// Defaults.NewRequestBehaviourBuilder. It must be additive, not destructive.
func TestRequestBehaviourTemplate_AppliesWithoutLosingDefaults(t *testing.T) {
	var templateRan bool
	defaults := NewDefaults()
	defaults.SetRequestBehaviour(func(b shared.RequestBehaviourBuilder) {
		b.Upon(events.EVENTS.TransmitRenderedTemplate, func(http.ResponseWriter, *http.Request, events.NetworkingEvent) error {
			templateRan = true
			return nil
		})
	})

	data, rec := newBoundRequestData(t)
	defaults.NewRequestBehaviourBuilder(data) // applies the template

	const html = "<html>tpl</html>"
	if err := data.Dispatch(events.NewTransmitRenderedTemplate(rendered(html))); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if !templateRan {
		t.Error("Defaults.Requests template handler was never invoked")
	}
	if rec.Body.String() != html {
		t.Errorf("body = %q, want %q: the template must not suppress the default transmit",
			rec.Body.String(), html)
	}
}

// ---------------------------------------------------------------------------
// 4. Writer binding.
//
// Handlers must resolve the writer at dispatch time, not capture whatever was
// passed to NewRequestData. SetHTTPBehaviour constructs a behaviour with
// (nil, nil) before the real writer is known.
// ---------------------------------------------------------------------------

func TestHandlersWriteToTheCurrentWriter(t *testing.T) {
	data := NewRequestData(nil, nil) // as SetHTTPBehaviour does

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	NewRequestBehaviourBuilder(data).SetWriter(rec).SetRequest(req)

	const html = "<html>late-bound</html>"
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("dispatch panicked, handlers captured the nil writer from "+
				"NewRequestData instead of reading RequestBehaviour.W: %v", r)
		}
	}()

	if err := data.Dispatch(events.NewTransmitRenderedTemplate(rendered(html))); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if rec.Body.String() != html {
		t.Errorf("body = %q, want %q", rec.Body.String(), html)
	}
}
