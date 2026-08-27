package networking

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	shared "github.com/lilybw/go-solid/shared/networking"
	"github.com/lilybw/go-solid/shared/networking/events"
)

// ---------------------------------------------------------------------------
// The development-failure bucket.
//
// A development failure is a fault in how the library was used, not in serving
// the request. It is also a failure, so it belongs to two buckets at once —
// which is why Dispatch tests the categories independently instead of picking
// the first match.
// ---------------------------------------------------------------------------

func TestDevelopmentFailureBucket_IsRegisteredAsACategory(t *testing.T) {
	for _, category := range events.EVENTS.Categories {
		if category == events.EVENTS.DevelopmentFailureEvent {
			return
		}
	}
	t.Error("DevelopmentFailureEvent is not in EVENTS.Categories; handlers for it can never be reached")
}

func TestDevelopmentFailureBucket_RunsForDevelopmentFailures(t *testing.T) {
	data, _ := newBoundRequestData(t)

	var devRan, failRan bool
	data.Handlers.Add(func(events.DevelopmentFailureEvent) error { devRan = true; return nil },
		shared.HANDLER_MODE_POSTFIX)
	data.Handlers.Add(func(events.FailureEvent) error { failRan = true; return nil },
		shared.HANDLER_MODE_POSTFIX)

	if err := data.Dispatch(events.NewCompPropsInsufficientFailure(errors.New("props"))); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if !devRan {
		t.Error("the development bucket did not run for a development failure")
	}
	if !failRan {
		t.Error("the failure bucket was skipped; a development failure is still a failure")
	}
}

// The narrower bucket must not widen: an ordinary failure is not a development
// one, and a handler scoped to development feedback must stay silent for it.
func TestDevelopmentFailureBucket_DoesNotRunForOrdinaryFailures(t *testing.T) {
	data, _ := newBoundRequestData(t)

	var devRan bool
	data.Handlers.Add(func(events.DevelopmentFailureEvent) error { devRan = true; return nil },
		shared.HANDLER_MODE_POSTFIX)

	if err := data.Dispatch(events.NewPropsMarshalingFailure(errors.New("x"))); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if devRan {
		t.Error("the development bucket ran for a plain failure")
	}
}

// ---------------------------------------------------------------------------
// The synchronized writer.
//
// Chains under one event dispatch concurrently, and http.ResponseWriter is not
// safe for concurrent use. Run these with -race.
// ---------------------------------------------------------------------------

func TestSynchronized_WrapsOnceAndSkipsNil(t *testing.T) {
	if shared.Synchronized(nil) != nil {
		t.Error("Synchronized(nil) produced a writer wrapping nothing")
	}
	once := shared.Synchronized(httptest.NewRecorder())
	if twice := shared.Synchronized(once); twice != once {
		t.Error("Synchronized re-wrapped an already-wrapped writer")
	}
}

func TestNewRequestData_BindsASynchronizedWriter(t *testing.T) {
	data, _ := newBoundRequestData(t)
	if _, ok := data.W.(*shared.SynchronizedResponseWriter); !ok {
		t.Errorf("W is %T; parallel chains can corrupt an unwrapped writer", data.W)
	}
}

func TestSetWriter_BindsASynchronizedWriter(t *testing.T) {
	data := NewRequestData(nil, nil)
	NewRequestBehaviourBuilder(data).SetWriter(httptest.NewRecorder())
	if _, ok := data.W.(*shared.SynchronizedResponseWriter); !ok {
		t.Errorf("SetWriter installed a bare %T", data.W)
	}
}

func TestSynchronizedWriter_ConcurrentUseIsSafe(t *testing.T) {
	rec := httptest.NewRecorder()
	w := shared.Synchronized(rec)

	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 50 {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte("x"))
			}
		}()
	}
	wg.Wait()

	if got := rec.Body.Len(); got != 16*50 {
		t.Errorf("wrote %d bytes, want %d", got, 16*50)
	}
}

// http.ResponseController reaches capabilities the wrapper does not forward
// itself, which is what Unwrap is for.
func TestSynchronizedWriter_Unwraps(t *testing.T) {
	rec := httptest.NewRecorder()
	w := shared.Synchronized(rec).(*shared.SynchronizedResponseWriter)
	if w.Unwrap() != rec {
		t.Error("Unwrap did not return the wrapped writer")
	}
	w.Flush() // must not panic when the wrapped writer supports it
	if !rec.Flushed {
		t.Error("Flush did not reach the wrapped writer")
	}
}
