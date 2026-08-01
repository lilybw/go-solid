package hmr

import (
	"context"
	"fmt"
	"net/http"
	"sync"

	"github.com/coder/websocket"
	"github.com/lilybw/go_solid/internal/meta"
)

const DEFAULT_HMR_PATH = "/__go_solid_hmr__"

type Config struct {
	// Disabled turns HMR off even when a config is supplied, so a consumer can
	// keep the struct around and flip one field.
	Disabled bool
	// HMRPath is where go_solid mounts the WebSocket handler and where the
	// injected client connects. Defaults to DEFAULT_HMR_PATH.
	HMRPath string
	// Mux is the consumer's mux; go_solid mounts its handler on it directly, so
	// the consumer never wires the endpoint themselves. Required when HMR is
	// enabled — there is no safe default that works with an arbitrary server.
	Mux *http.ServeMux
}

// NormalizeHMRConfig fills defaults and validates. It errors if HMR is enabled
// without a Mux: defaulting to http.DefaultServeMux would silently mount the
// endpoint on a mux the consumer's server never serves, so HMR would break with
// no visible cause. Better to fail loudly at startup.
func NormalizeHMRConfig(cfg *Config) (*Config, error) {
	if cfg == nil {
		cfg = &Config{}
	}
	if cfg.HMRPath == "" {
		cfg.HMRPath = DEFAULT_HMR_PATH
	}
	if cfg.Mux == nil {
		return nil, fmt.Errorf("go_solid HMR: Config.HMR.Mux is required when HMR is enabled (go_solid mounts its handler on your mux)")
	}
	return cfg, nil
}

type Hub struct {
	mu    sync.Mutex
	conns map[meta.QualifiedName]map[*websocket.Conn]struct{} // componentName -> set of conns
}

func NewHub(cfg *Config) *Hub {
	// cfg is accepted for symmetry and future per-hub settings; nothing on it is
	// needed at construction today.
	return &Hub{conns: map[meta.QualifiedName]map[*websocket.Conn]struct{}{}}
}

func (h *Hub) add(component meta.QualifiedName, c *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	set := h.conns[component]
	if set == nil {
		set = map[*websocket.Conn]struct{}{}
		h.conns[component] = set
	}
	set[c] = struct{}{}
}

func (h *Hub) remove(component meta.QualifiedName, c *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if set := h.conns[component]; set != nil {
		delete(set, c)
		if len(set) == 0 {
			delete(h.conns, component)
		}
	}
}

// Reload pushes a reload to every connection viewing the named component. Write
// failures are ignored: a dead connection is cleaned up by its own read loop.
// Snapshot under lock, write outside it, so a slow write never blocks
// registration of other connections. Exported so the watcher (same package) and
// any future caller can trigger it.
func (h *Hub) Reload(component meta.QualifiedName) {
	h.mu.Lock()
	targets := make([]*websocket.Conn, 0, len(h.conns[component]))
	for c := range h.conns[component] {
		targets = append(targets, c)
	}
	h.mu.Unlock()

	for _, c := range targets {
		_ = c.Write(context.Background(), websocket.MessageText, []byte("reload"))
	}
}

// Handler returns the WebSocket handler go_solid mounts on the consumer's mux.
// It is self-contained: upgrades, registers the connection under the component
// named in ?c=, and blocks reading only to detect disconnect.
func (h *Hub) Handler() http.Handler {
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
