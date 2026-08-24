package io

import (
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"
)

func TestWriteAtomic_CreatesAndReplaces(t *testing.T) {
	path := filepath.Join(t.TempDir(), "entry.json")

	if err := WriteAtomic(path, []byte("first")); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := WriteAtomic(path, []byte("second")); err != nil {
		t.Fatalf("replace: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != "second" {
		t.Errorf("contents = %q, want %q", got, "second")
	}
}

// The staging file is a sibling, and it must not survive the write.
func TestWriteAtomic_LeavesNoStagingFile(t *testing.T) {
	dir := t.TempDir()
	if err := WriteAtomic(filepath.Join(dir, "entry.json"), []byte("x")); err != nil {
		t.Fatalf("WriteAtomic: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "entry.json" {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		t.Errorf("directory holds %v, want only entry.json", names)
	}
}

// os.CreateTemp stages at 0600, which is rarely what a published artifact
// wants, so the mode has to be applied before the rename rather than after.
func TestWriteAtomicMode_AppliesTheMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file modes are not meaningful on Windows")
	}
	path := filepath.Join(t.TempDir(), "entry.json")
	if err := WriteAtomicMode(path, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteAtomicMode: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o644 {
		t.Errorf("mode = %v, want 0644", got)
	}
}

// A reader sees one version or the other, never a partial file. That is the
// whole reason this exists.
//
// The reader loops without pausing on purpose: on Windows an open handle
// refuses the replace outright, so a reader that stepped politely aside would
// prove nothing about the case that actually breaks.
func TestWriteAtomic_ReaderNeverSeesAPartialFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "entry.json")
	short, long := []byte("aa"), []byte(string(make([]byte, 1<<16)))
	for i := range long {
		long[i] = 'b'
	}
	if err := WriteAtomic(path, short); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := range 100 {
			payload := short
			if i%2 == 0 {
				payload = long
			}
			if err := WriteAtomic(path, payload); err != nil {
				t.Errorf("write: %v", err)
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		for range 400 {
			got, err := os.ReadFile(path)
			if err != nil {
				continue // the rename window is not a read error, but tolerate it
			}
			if len(got) != len(short) && len(got) != len(long) {
				t.Errorf("read a partial file of %d bytes", len(got))
				return
			}
		}
	}()
	wg.Wait()
}

// Windows refuses to replace a file another handle has open, where POSIX simply
// unlinks the old inode. Anything reading a cache entry or a generated module
// while it is republished hits exactly this, so the replace has to outlast a
// reader rather than fail in front of one.
func TestWriteAtomic_ReplacesAFileAReaderHasOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "entry.json")
	if err := WriteAtomic(path, []byte("before")); err != nil {
		t.Fatalf("seed: %v", err)
	}

	held, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	// Let go of it shortly, so a bounded retry has a window to land in.
	go func() {
		time.Sleep(40 * time.Millisecond)
		held.Close()
	}()

	if err := WriteAtomic(path, []byte("after")); err != nil {
		t.Fatalf("replacing a file a reader had open: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "after" {
		t.Errorf("contents = %q, want %q", got, "after")
	}
}

// The directory must exist; a clear error beats a stray temp file somewhere.
func TestWriteAtomic_MissingDirectoryErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "absent", "entry.json")
	if err := WriteAtomic(path, []byte("x")); err == nil {
		t.Error("writing into a missing directory succeeded")
	}
}
