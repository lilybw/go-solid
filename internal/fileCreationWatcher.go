package internal

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/fsnotify/fsnotify"
	"github.com/lilybw/go-solid/internal/meta"
	. "github.com/lilybw/go-solid/shared/registry"
)

type registryInvalidator interface {
	InvalidateComponent(name string) // disk: exact; you pass a closure that also flushes mem
}

type OnCreationCallback func(file meta.AbsoluteFilePath) error
type onDeletionCallback func(file meta.AbsoluteFilePath) error

type FileCreationWatcher struct {
	fsw        *fsnotify.Watcher
	root       meta.AbsoluteDirectoryPath
	onCreation OnCreationCallback
	onDeletion onDeletionCallback
	onErr      func(error)
	stopCh     chan struct{}
	wg         sync.WaitGroup
}

func NewFileCreationWatcher(root meta.AbsoluteDirectoryPath, onWatcherErr func(error), onCreation OnCreationCallback, onDeletion onDeletionCallback) (*FileCreationWatcher, error) {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("go_solid ReactiveRegistry: create watcher: %w", err)
	}
	w := &FileCreationWatcher{
		fsw: fsw, root: root,
		onCreation: onCreation, onDeletion: onDeletion,
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

func (w *FileCreationWatcher) addTree(root string) error {
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

func (w *FileCreationWatcher) loop() {
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

func (this *FileCreationWatcher) handle(event fsnotify.Event) {
	// New directory: start watching it so files created inside are seen.
	if event.Op&fsnotify.Create != 0 {
		if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
			base := filepath.Base(event.Name)
			if !SkipDir(base, event.Name, this.root) {
				if err := this.addTree(event.Name); err != nil && this.onErr != nil {
					this.onErr(err)
				}
			}
			return
		}
	}

	switch {
	case event.Op&fsnotify.Create != 0:
		if err := this.onCreation(event.Name); err != nil && this.onErr != nil {
			this.onErr(err)
		}
	case event.Op&(fsnotify.Remove|fsnotify.Rename) != 0:
		if err := this.onDeletion(event.Name); err != nil && this.onErr != nil {
			this.onErr(err)
		}
	}
}

func (w *FileCreationWatcher) Stop() {
	close(w.stopCh)
	w.wg.Wait()
	w.fsw.Close()
}
