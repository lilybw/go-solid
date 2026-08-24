package hmr

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/lilybw/go-solid/internal"
	"github.com/lilybw/go-solid/internal/meta"
	"github.com/lilybw/go-solid/shared"
)

// Watcher watches the components tree and, on change, inverts through DepIndex
// to find affected components and asks the Hub to reload the tabs viewing them.
type Watcher struct {
	fsw   *fsnotify.Watcher
	root  string
	index *internal.DependencyIndex
	hub   *Hub
	onErr func(error)
	// invalidate drops cached artifacts for a component. It must run before the
	// browser is told to reload, or the reload re-fetches the stale bundle.
	invalidate func(meta.QualifiedName)
	stopCh     chan struct{}
	stopOnce   sync.Once
	wg         sync.WaitGroup

	debounce time.Duration
}

// NewWatcher constructs the watcher, seeds the watch set from the tree, and
// starts its goroutine before returning. There is no separate Start method: a
// constructed Watcher is already running
func NewWatcher(
	root string,
	index *internal.DependencyIndex,
	hub *Hub,
	invalidate func(meta.QualifiedName),
	onErr func(error),
) (*Watcher, error) {
	if invalidate == nil {
		invalidate = func(meta.QualifiedName) {}
	}
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("go_solid HMR: create watcher: %w", err)
	}
	w := &Watcher{
		fsw:        fsw,
		root:       root,
		index:      index,
		hub:        hub,
		invalidate: invalidate,
		onErr:      onErr,
		stopCh:     make(chan struct{}),
		debounce:   80 * time.Millisecond,
	}
	if err := w.addTree(root, nil); err != nil {
		fsw.Close()
		return nil, err
	}
	w.wg.Add(1)
	go w.loop()
	return w, nil
}

// addTree registers watches for root and every directory beneath it, calling
// found for each file it passes. Beware ubuntu vs windows differences in file creation events and constraints
func (w *Watcher) addTree(root string, found func(path string)) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			if found != nil {
				found(path)
			}
			return nil
		}
		if shared.SkipDir(d.Name(), path, root) {
			return fs.SkipDir
		}
		if err := w.fsw.Add(path); err != nil {
			return fmt.Errorf("go_solid HMR: watch %q: %w", path, err)
		}
		return nil
	})
}

func (w *Watcher) loop() {
	defer w.wg.Done()

	pending := map[meta.QualifiedName]struct{}{}
	var timer *time.Timer
	var timerCh <-chan time.Time

	arm := func() {
		if timer == nil {
			timer = time.NewTimer(w.debounce)
			timerCh = timer.C
		} else {
			timer.Reset(w.debounce)
		}
	}

	for {
		select {
		case <-w.stopCh:
			if timer != nil {
				timer.Stop()
			}
			return

		case event, ok := <-w.fsw.Events:
			if !ok {
				return
			}
			w.handleEvent(event, pending, arm)

		case err, ok := <-w.fsw.Errors:
			if !ok {
				return
			}
			if w.onErr != nil {
				w.onErr(err)
			}

		case <-timerCh:
			for name := range pending {
				w.invalidate(name) // must precede Reload
				w.hub.Reload(name)
				delete(pending, name)
			}
		}
	}
}

func (w *Watcher) handleEvent(event fsnotify.Event, pending map[meta.QualifiedName]struct{}, arm func()) {
	// A newly created directory must be added to the watch set (fsnotify is
	// non-recursive), and whatever is already inside it has to be picked up by
	// walking: those files predate the watch, so no event carries them.
	if event.Op&fsnotify.Create != 0 {
		if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
			base := filepath.Base(event.Name)
			if shared.SkipDir(base, event.Name, w.root) {
				return
			}
			before := len(pending)
			err := w.addTree(event.Name, func(path string) {
				for _, name := range w.index.DependentsOf(path) {
					pending[name] = struct{}{}
				}
			})
			if err != nil && w.onErr != nil {
				w.onErr(err)
			}
			if len(pending) > before {
				arm()
			}
			return
		}
	}

	if event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename|fsnotify.Remove) == 0 {
		return
	}

	affected := w.index.DependentsOf(event.Name)
	for _, name := range affected {
		pending[name] = struct{}{}
	}
	if len(affected) > 0 {
		arm()
	}
}

func (w *Watcher) Stop() {
	if w == nil {
		return
	}
	w.stopOnce.Do(func() {
		close(w.stopCh)
		w.wg.Wait()
		w.fsw.Close()
	})
}
