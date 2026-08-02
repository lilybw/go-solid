package internal

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/fsnotify/fsnotify"
	. "github.com/lilybw/go-solid/shared/registry"
)

type registryInvalidator interface {
	InvalidateComponent(name string) // disk: exact; you pass a closure that also flushes mem
}

type RegistryWatcher struct {
	fsw    *fsnotify.Watcher
	root   string
	reg    *Registry
	onDrop func(name string) // called with the qualified name on delete, for cache cascade
	onErr  func(error)
	stopCh chan struct{}
	wg     sync.WaitGroup
}

func NewRegistryWatcher(root string, reg *Registry, onDrop func(string), onErr func(error)) (*RegistryWatcher, error) {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("go_solid ReactiveRegistry: create watcher: %w", err)
	}
	w := &RegistryWatcher{
		fsw: fsw, root: root, reg: reg,
		onDrop: onDrop, onErr: onErr,
		stopCh: make(chan struct{}),
	}
	if err := w.addTree(root); err != nil {
		fsw.Close()
		return nil, err
	}
	w.wg.Add(1)
	go w.loop()
	return w, nil
}

func (w *RegistryWatcher) addTree(root string) error {
	return filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		if SkipDir(d.Name(), path, root) { // reuse the existing helper in watching.go
			return filepath.SkipDir
		}
		return w.fsw.Add(path)
	})
}

func (w *RegistryWatcher) loop() {
	defer w.wg.Done()
	for {
		select {
		case <-w.stopCh:
			return
		case event, ok := <-w.fsw.Events:
			if !ok {
				return
			}
			w.handle(event)
		case err, ok := <-w.fsw.Errors:
			if !ok {
				return
			}
			if w.onErr != nil {
				w.onErr(err)
			}
		}
	}
}

func (w *RegistryWatcher) handle(event fsnotify.Event) {
	// New directory: start watching it so files created inside are seen.
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

	switch {
	case event.Op&fsnotify.Create != 0:
		if _, ok, err := w.reg.AddFile(event.Name); err != nil && w.onErr != nil {
			w.onErr(err)
		} else if ok && w.onErr == nil {
			_ = ok
		}
	case event.Op&(fsnotify.Remove|fsnotify.Rename) != 0:
		if name, ok := w.reg.RemoveFile(event.Name); ok && w.onDrop != nil {
			w.onDrop(name)
		}
	}
}

func (w *RegistryWatcher) Stop() {
	close(w.stopCh)
	w.wg.Wait()
	w.fsw.Close()
}
