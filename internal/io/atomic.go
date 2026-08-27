// Package io holds the filesystem primitives go_solid's caches and generators
// share.
//
// Note the name shadows the standard library's io. A file needing both should
// alias one of them.
package io

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/lilybw/go-solid/shared/meta"
)

// WriteAtomic replaces the file at path with data, or leaves it untouched.
//
// The write lands on a temporary sibling and is renamed into place
func WriteAtomic(path string, data []byte) error {
	return writeAtomic(path, data, 0)
}

func WriteAtomicMode(path string, data []byte, mode os.FileMode) error {
	return writeAtomic(path, data, mode)
}

func writeAtomic(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)

	tmp, err := os.CreateTemp(dir, ".go_solid-*.tmp")
	if err != nil {
		return fmt.Errorf("stage %q: %w", path, err)
	}
	staged := tmp.Name()
	// Removing the staging path after a successful rename is a no-op; leaving
	// it in place after a failure is a leak.
	defer os.Remove(staged)

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write %q: %w", staged, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close %q: %w", staged, err)
	}
	if mode != 0 {
		if err := os.Chmod(staged, mode); err != nil {
			return fmt.Errorf("chmod %q: %w", staged, err)
		}
	}
	if err := renameOver(staged, path); err != nil {
		return fmt.Errorf("replace %q: %w", path, err)
	}
	return nil
}

// renameOver replaces to with from, retrying briefly.
//
// POSIX rename unlinks the old inode and leaves any reader holding it reading
// the bytes it already had, so it never fails for being observed. Windows
// refuses instead: replacing a file another handle has open fails with
// ERROR_ACCESS_DENIED, and Go opens files for reading without
// FILE_SHARE_DELETE, so any concurrent reader blocks the replace.
//
// The window is a reader's open-read-close, which is microseconds, so a
// bounded retry closes it. On POSIX the first attempt succeeds and the loop
// costs nothing.
func renameOver(from, to string) error {
	const attempts = 100
	const maxBackoff = 10 * time.Millisecond

	backoff := 250 * time.Microsecond
	var err error
	for attempt := range attempts {
		if err = os.Rename(from, to); err == nil {
			return nil
		}
		if attempt == attempts-1 {
			break
		}
		time.Sleep(backoff)
		if backoff < maxBackoff {
			backoff *= 2
		}
	}
	return err
}

func WriteIfChanged(path meta.AbsoluteFilePath, data []byte, mode os.FileMode) (bool, error) {
	if current, err := os.ReadFile(path); err == nil && bytes.Equal(current, data) {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, fmt.Errorf("create %q: %w", filepath.Dir(path), err)
	}
	return true, writeAtomic(path, data, mode)
}
