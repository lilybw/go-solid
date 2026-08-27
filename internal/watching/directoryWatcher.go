package watching

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"

	"github.com/fsnotify/fsnotify"
	"github.com/lilybw/go-solid/internal/noop"
	"github.com/lilybw/go-solid/shared"
	"github.com/lilybw/go-solid/shared/meta"
	. "github.com/lilybw/go-solid/shared/watching"
)

type DirectoryWatcher[T any] struct {
	fsw      *fsnotify.Watcher
	root     meta.AbsoluteDirectoryPath
	cfg      *DWConfig[T]
	stopCh   chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup
}

func NewDirectoryWatcher[T any](root meta.AbsoluteDirectoryPath, cfg *DWConfig[T]) (*DirectoryWatcher[T], error) {
	if err := polyfillConfig(cfg); err != nil {
		return nil, err
	}

	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("go_solid ReactiveRegistry: create watcher: %w", err)
	}
	w := &DirectoryWatcher[T]{
		fsw: fsw, root: root,
		cfg:    cfg,
		stopCh: make(chan struct{}),
	}
	// Nothing has happened yet, so the existing tree is watched silently.
	if err := w.addTree(root, false); err != nil {
		fsw.Close()
		return nil, err
	}
	w.wg.Add(1)
	go w.loop()
	return w, nil
}

// assures that all fields are present and non-nil
func polyfillConfig[T any](cfg *DWConfig[T]) error {
	if cfg == nil {
		return fmt.Errorf("go_solid: DWConfig is required for a DirectoryWatcher")
	}
	if cfg.OnCreation == nil {
		cfg.OnCreation = noop.TR_o_Err[meta.AbsoluteFilePath, T]()
	}
	if cfg.OnDeletion == nil {
		cfg.OnDeletion = noop.TR_o_Err[meta.AbsoluteFilePath, T]()
	}
	if cfg.OnMutation == nil {
		cfg.OnMutation = noop.TR_o_Err[meta.AbsoluteFilePath, T]()
	}
	if cfg.OnErr == nil {
		cfg.OnErr = func(err error) {
			log.Printf("[go_solid] DirectoryWatcher error: %v", err)
		}
	}
	if cfg.DeriveAndInclude == nil {
		cfg.DeriveAndInclude = noop.T_o_R[fsnotify.Event](meta.Zero[T]())
	}

	return nil
}

// addTree registers watches for root and every directory beneath it.
// The walk visits a directory before its entries, so the watch is in place
// before the listing is read. A file appearing in between is reported twice
// rather than not at all — the callbacks are idempotent, and a duplicate is the
// cheaper mistake.
func (this *DirectoryWatcher[T]) addTree(root meta.AbsoluteFilePath, announce bool) error {
	return filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			if announce {
				this.announceCreation(path)
			}
			return nil
		}
		if shared.SkipDir(d.Name(), path, root) { // reuse the existing helper in watching.go
			return filepath.SkipDir
		}
		return this.fsw.Add(path)
	})
}

// announceCreation reports a file the watcher found rather than was told about,
// in the same shape as the event that would have carried it.
func (this *DirectoryWatcher[T]) announceCreation(path meta.AbsoluteFilePath) {
	synthetic := fsnotify.Event{Name: path, Op: fsnotify.Create}
	if err := this.cfg.OnCreation(path, this.cfg.DeriveAndInclude(synthetic)); err != nil && this.cfg.OnErr != nil {
		this.cfg.OnErr(err)
	}
}

func (this *DirectoryWatcher[T]) loop() {
	defer this.wg.Done()
	for {
		select {
		case <-this.stopCh:
			return
		case event, ok := <-this.fsw.Events:
			if !ok {
				return
			}
			this.handle(event)
		case err, ok := <-this.fsw.Errors:
			if !ok {
				return
			}
			if this.cfg.OnErr != nil {
				this.cfg.OnErr(err)
			}
		}
	}
}

func (this *DirectoryWatcher[T]) handle(event fsnotify.Event) {
	// New sub-directory: start watching it, and report whatever is already
	// inside. A directory rarely arrives empty — it is copied in with its
	// contents — and those files predate the watch, so no event will carry
	// them.
	if event.Op&fsnotify.Create != 0 {
		if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
			base := filepath.Base(event.Name)
			if !shared.SkipDir(base, event.Name, this.root) {
				if err := this.addTree(event.Name, true); err != nil && this.cfg.OnErr != nil {
					this.cfg.OnErr(err)
				}
			}
			return
		}
	}

	switch {
	case event.Op&fsnotify.Create != 0:
		if err := this.cfg.OnCreation(event.Name, this.cfg.DeriveAndInclude(event)); err != nil && this.cfg.OnErr != nil {
			this.cfg.OnErr(err)
		}
	case event.Op&(fsnotify.Remove|fsnotify.Rename) != 0:
		if err := this.cfg.OnDeletion(event.Name, this.cfg.DeriveAndInclude(event)); err != nil && this.cfg.OnErr != nil {
			this.cfg.OnErr(err)
		}
	case event.Op&fsnotify.Write != 0:
		if err := this.cfg.OnMutation(event.Name, this.cfg.DeriveAndInclude(event)); err != nil && this.cfg.OnErr != nil {
			this.cfg.OnErr(err)
		}
	}
}

func (this *DirectoryWatcher[T]) Stop() {
	if this == nil {
		return
	}
	this.stopOnce.Do(func() {
		close(this.stopCh)
		this.wg.Wait()
		this.fsw.Close()
	})
}
