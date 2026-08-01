package hmr

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/lilybw/go-solid/shared"
)

// timeoutAfter is the shared deadline channel for "this must not hang" assertions.
// Kept generous so slow CI does not flake, but short enough to fail in finite time.
func timeoutAfter() <-chan time.Time {
	return time.After(3 * time.Second)
}

// wsURL rewrites an httptest http:// server URL to ws:// and appends the HMR path
// and component query the injected client would use.
func wsURL(serverURL, path, component string) string {
	u := strings.Replace(serverURL, "http://", "ws://", 1)
	return u + path + "?c=" + component
}

// newHubServer wires a Hub's handler onto an httptest server at DEFAULT_HMR_PATH.
func newHubServer(t *testing.T) (*Hub, *httptest.Server) {
	t.Helper()
	h := NewHub(&shared.HMRConfig{})
	mux := http.NewServeMux()
	mux.Handle(DEFAULT_HMR_PATH, h.Handler())
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return h, srv
}

func TestHubHandler_RejectsMissingComponent(t *testing.T) {
	_, srv := newHubServer(t)

	// Connect without ?c= — the handler must 400 before upgrading.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	badURL := strings.Replace(srv.URL, "http://", "ws://", 1) + DEFAULT_HMR_PATH
	_, resp, err := websocket.Dial(ctx, badURL, nil)
	if err == nil {
		t.Fatal("expected dial to fail without ?c=, but it succeeded")
	}
	if resp != nil && resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestHubHandler_ReloadReachesConnectedClient(t *testing.T) {
	h, srv := newHubServer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, wsURL(srv.URL, DEFAULT_HMR_PATH, "ui/Button"), nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	// The server registers the connection during the handshake goroutine; give it
	// a moment to appear in the hub before triggering a reload. Poll rather than
	// sleep-fixed so this is not timing-fragile.
	if !waitForConn(h, "ui/Button", 2*time.Second) {
		t.Fatal("connection never registered in hub")
	}

	h.Reload("ui/Button")

	// The client must receive a message (the reload signal).
	readCtx, readCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer readCancel()
	typ, data, err := conn.Read(readCtx)
	if err != nil {
		t.Fatalf("expected reload message, read failed: %v", err)
	}
	if typ != websocket.MessageText || string(data) != "reload" {
		t.Fatalf("expected text 'reload', got typ=%v data=%q", typ, data)
	}
}

func TestHubHandler_ReloadOnlyReachesMatchingComponent(t *testing.T) {
	// The precision guarantee: a reload for component A must NOT wake a client
	// viewing component B.
	h, srv := newHubServer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	connA, _, err := websocket.Dial(ctx, wsURL(srv.URL, DEFAULT_HMR_PATH, "ui/A"), nil)
	if err != nil {
		t.Fatalf("dial A failed: %v", err)
	}
	defer connA.Close(websocket.StatusNormalClosure, "")

	connB, _, err := websocket.Dial(ctx, wsURL(srv.URL, DEFAULT_HMR_PATH, "ui/B"), nil)
	if err != nil {
		t.Fatalf("dial B failed: %v", err)
	}
	defer connB.Close(websocket.StatusNormalClosure, "")

	if !waitForConn(h, "ui/A", 2*time.Second) || !waitForConn(h, "ui/B", 2*time.Second) {
		t.Fatal("connections never registered")
	}

	h.Reload("ui/A")

	// A must receive.
	readCtx, readCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer readCancel()
	if _, data, err := connA.Read(readCtx); err != nil || string(data) != "reload" {
		t.Fatalf("A should have received reload: data=%q err=%v", data, err)
	}

	// B must NOT receive within a short window. A read with a short deadline
	// should time out (context deadline exceeded), proving no message came.
	noMsgCtx, noMsgCancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer noMsgCancel()
	_, _, err = connB.Read(noMsgCtx)
	if err == nil {
		t.Fatal("B received a reload it should not have (precision violated)")
	}
}

func TestHubHandler_DisconnectRemovesFromHub(t *testing.T) {
	h, srv := newHubServer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, wsURL(srv.URL, DEFAULT_HMR_PATH, "ui/Ephemeral"), nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	if !waitForConn(h, "ui/Ephemeral", 2*time.Second) {
		t.Fatal("connection never registered")
	}

	// Close from the client side; the server's read loop should error and the
	// deferred remove should drop the component from the map entirely.
	conn.Close(websocket.StatusNormalClosure, "bye")

	if !waitForNoConn(h, "ui/Ephemeral", 2*time.Second) {
		t.Fatal("connection was not removed from hub after client disconnect")
	}
}

func TestHubHandler_MultipleClientsSameComponent(t *testing.T) {
	h, srv := newHubServer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	const n = 5
	conns := make([]*websocket.Conn, 0, n)
	for i := 0; i < n; i++ {
		c, _, err := websocket.Dial(ctx, wsURL(srv.URL, DEFAULT_HMR_PATH, "ui/Shared"), nil)
		if err != nil {
			t.Fatalf("dial %d failed: %v", i, err)
		}
		conns = append(conns, c)
		defer c.Close(websocket.StatusNormalClosure, "")
	}

	if !waitForConnCount(h, "ui/Shared", n, 2*time.Second) {
		t.Fatalf("expected %d connections registered", n)
	}

	h.Reload("ui/Shared")

	// Every client must receive the reload.
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i, c := range conns {
		wg.Add(1)
		go func(i int, c *websocket.Conn) {
			defer wg.Done()
			rc, rcancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer rcancel()
			_, data, err := c.Read(rc)
			if err != nil {
				errs[i] = err
				return
			}
			if string(data) != "reload" {
				errs[i] = errUnexpected(string(data))
			}
		}(i, c)
	}
	wg.Wait()
	for i, e := range errs {
		if e != nil {
			t.Fatalf("client %d did not receive reload: %v", i, e)
		}
	}
}

// --- hub introspection helpers ---------------------------------------------
// These read h.conns under the hub's own lock. They live in the test file and
// rely on being in package hmr (white-box) to touch the unexported field.

func hubConnCount(h *Hub, component string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.conns[component])
}

func waitForConn(h *Hub, component string, within time.Duration) bool {
	return waitForConnCount(h, component, 1, within)
}

func waitForConnCount(h *Hub, component string, want int, within time.Duration) bool {
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if hubConnCount(h, component) >= want {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return hubConnCount(h, component) >= want
}

func waitForNoConn(h *Hub, component string, within time.Duration) bool {
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if hubConnCount(h, component) == 0 {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return hubConnCount(h, component) == 0
}

type unexpectedData string

func (u unexpectedData) Error() string { return "unexpected message data: " + string(u) }
func errUnexpected(s string) error     { return unexpectedData(s) }
