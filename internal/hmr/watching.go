package hmr

import (
	"fmt"
	"sync"
	"time"

	"github.com/lilybw/go-solid/internal"
	watching_int "github.com/lilybw/go-solid/internal/watching"
	"github.com/lilybw/go-solid/shared/meta"
	"github.com/lilybw/go-solid/shared/watching"
)

const reloadWindow = 80 * time.Millisecond

// Watcher watches the components tree and, on change, inverts through DepIndex
// to find affected components and asks the Hub to reload the tabs viewing them.
type Watcher struct {
	tree    *watching_int.DirectoryWatcher[meta.Void]
	mu      sync.Mutex
	pending map[meta.QualifiedName]struct{}

	debounce *watching_int.Debouncer
}

// NewWatcher constructs the watcher, seeds the watch set from the tree, and
// starts its goroutine before returning. There is no separate Start method: a
// constructed Watcher is already running
func NewWatcher(
	root meta.AbsoluteDirectoryPath,
	index *internal.DependencyIndex,
	hub *Hub,
	invalidate func(meta.QualifiedName),
	onErr func(error),
) (*Watcher, error) {
	if invalidate == nil {
		invalidate = func(meta.QualifiedName) {}
	}
	w := &Watcher{pending: map[meta.QualifiedName]struct{}{}}
	w.debounce = watching_int.NewDebouncer(reloadWindow, func() {
		for _, name := range w.drain() {
			invalidate(name) // must precede Reload, or the tab refetches the stale bundle
			hub.Reload(name)
		}
	})
	// Creation, mutation and deletion are the same question here: which
	// components had this file in their bundle graph.
	touched := func(file meta.AbsoluteFilePath, _ meta.Void) error {
		w.enqueue(index.DependentsOf(file))
		return nil
	}
	tree, err := watching_int.NewDirectoryWatcher(root, &watching.DWVoidConfig{
		OnCreation: touched,
		OnMutation: touched,
		OnDeletion: touched,
		OnErr:      onErr,
	})
	if err != nil {
		w.debounce.Stop()
		return nil, fmt.Errorf("go_solid HMR: watch %q: %w", root, err)
	}
	w.tree = tree
	return w, nil
}

func (w *Watcher) enqueue(names []meta.QualifiedName) {
	if len(names) == 0 {
		return
	}
	w.mu.Lock()
	for _, name := range names {
		w.pending[name] = struct{}{}
	}
	w.mu.Unlock()
	w.debounce.Arm()
}
func (w *Watcher) drain() []meta.QualifiedName {
	w.mu.Lock()
	defer w.mu.Unlock()
	names := make([]meta.QualifiedName, 0, len(w.pending))
	for name := range w.pending {
		names = append(names, name)
		delete(w.pending, name)
	}
	return names
}

// Stop is idempotent and safe on a nil receiver.
func (w *Watcher) Stop() {
	if w == nil {
		return
	}
	w.tree.Stop()
	w.debounce.Stop()
}
