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
	. "github.com/lilybw/go-solid/shared/registry"
)

// Watcher watches the components tree and, on change, inverts through DepIndex
// to find affected components and asks the Hub to reload the tabs viewing them.
type Watcher struct {
	fsw    *fsnotify.Watcher
	root   string
	index  *internal.DependencyIndex
	hub    *Hub
	reg    *internal.Registry
	onErr  func(error)
	stopCh chan struct{}
	wg     sync.WaitGroup

	debounce time.Duration
}

// NewWatcher constructs the watcher, seeds the watch set from the tree, and
// starts its goroutine before returning. There is no separate Start method: a
// constructed Watcher is already running, so the caller can't forget to start
// it. Stop halts it.
func NewWatcher(root string, index *internal.DependencyIndex, hub *Hub, reg *internal.Registry, onErr func(error)) (*Watcher, error) {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("go_solid HMR: create watcher: %w", err)
	}
	w := &Watcher{
		fsw:      fsw,
		root:     root,
		index:    index,
		hub:      hub,
		reg:      reg,
		onErr:    onErr,
		stopCh:   make(chan struct{}),
		debounce: 80 * time.Millisecond,
	}
	if err := w.addTree(root); err != nil {
		fsw.Close()
		return nil, err
	}
	w.wg.Add(1)
	go w.loop()
	return w, nil
}

func (w *Watcher) addTree(root string) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		if SkipDir(d.Name(), path, root) {
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
				w.hub.Reload(name)
				delete(pending, name)
			}
		}
	}
}

func (w *Watcher) handleEvent(event fsnotify.Event, pending map[meta.QualifiedName]struct{}, arm func()) {
	// A newly created directory must be added to the watch set (fsnotify is
	// non-recursive). Do this before anything else so files created inside it
	// immediately after are not missed.
	if event.Op&fsnotify.Create != 0 {
		if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
			base := filepath.Base(event.Name)
			if !SkipDir(base, event.Name, w.root) {
				if err := w.addTree(event.Name); err != nil && w.onErr != nil {
					w.onErr(err)
				}
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

// Stop halts the watcher and releases fsnotify resources. Safe to call once.
func (w *Watcher) Stop() {
	close(w.stopCh)
	w.wg.Wait()
	w.fsw.Close()
}
