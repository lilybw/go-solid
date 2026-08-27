package hmr

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/lilybw/go-solid/internal/collections"
	"github.com/lilybw/go-solid/internal/meta"
	. "github.com/lilybw/go-solid/shared/hmr"
)

// NormalizeHMRConfig fills defaults and validates. It errors if HMR is enabled
// without a Mux
func NormalizeHMRConfig(cfg *HMRConfig) (*HMRConfig, error) {
	cfg = meta.Or(cfg, NIL_HMR_CONFIG)
	cfg.Path = meta.Or(cfg.Path, NIL_HMR_CONFIG.Path)
	if cfg.Mux == nil {
		return nil, fmt.Errorf("go_solid HMR: Config.HMR.Mux is required when HMR is enabled (go_solid mounts its handler on your mux)")
	}
	return cfg, nil
}

type Hub struct {
	mu    sync.Mutex
	conns collections.SetMap[meta.QualifiedName, *websocket.Conn]
}

func NewHub() *Hub {
	return &Hub{conns: collections.SetMap[meta.QualifiedName, *websocket.Conn]{}}
}

func (h *Hub) add(component meta.QualifiedName, c *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.conns.Add(component, c)
}

func (h *Hub) remove(component meta.QualifiedName, c *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.conns.Remove(component, c)
}

// reloadWriteTimeout bounds one reload write. Writes are sequential and run on
// the watcher goroutine, so an unbounded one lets a single wedged tab stop every
// other tab from ever being reloaded again.
const reloadWriteTimeout = 5 * time.Second

// Reload pushes a reload to every connection viewing the named component. Write
// failures are ignored: a dead connection is cleaned up by its own read loop.
func (h *Hub) Reload(component meta.QualifiedName) {
	h.mu.Lock()
	targets := h.conns.MembersOf(component)
	h.mu.Unlock()

	for _, c := range targets {
		ctx, cancel := context.WithTimeout(context.Background(), reloadWriteTimeout)
		_ = c.Write(ctx, websocket.MessageText, []byte("reload"))
		cancel()
	}
}

// Handler returns the WebSocket handler go_solid mounts on the consumer's mux.
func (h *Hub) Handler() http.Handler {
	// It is self-contained: upgrades, registers the connection under the component
	// named in ?c=, and blocks reading only to detect disconnect.
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		component := r.URL.Query().Get("c")
		if component == "" {
			http.Error(w, "go_solid HMR: missing component (?c=) in websocket request", http.StatusBadRequest)
			return
		}

		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			// Same-origin by construction (the injected script derives the URL
			// from location); permit explicitly so a dev proxy on another
			// host:port doesn't break the handshake.
			InsecureSkipVerify: true,
		})
		if err != nil {
			return // Accept already wrote an error response
		}

		h.add(component, c)
		defer func() {
			h.remove(component, c)
			c.Close(websocket.StatusNormalClosure, "")
		}()

		// Block until the client disconnects. We expect no inbound messages;
		// reading is only how we learn the socket closed.
		ctx := r.Context()
		for {
			if _, _, err := c.Read(ctx); err != nil {
				return
			}
		}
	})
}
