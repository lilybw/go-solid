package hashing

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestShortHash_LengthAndStability(t *testing.T) {
	h1 := Short("hello world", 8)
	h2 := Short("hello world", 8)
	if h1 != h2 {
		t.Errorf("shortHash not stable: %q != %q", h1, h2)
	}
	if len(h1) != 8 {
		t.Errorf("shortHash len = %d, want 8", len(h1))
	}
}

func TestShortHash_DifferentInputsDiffer(t *testing.T) {
	if Short("a", 8) == Short("b", 8) {
		t.Error("shortHash produced same prefix for different inputs")
	}
}

func TestShortHash_ClampsOverlongN(t *testing.T) {
	// sha256 hex is 64 chars; asking for more must not panic and must clamp.
	h := Short("x", 999)
	if len(h) != 64 {
		t.Errorf("shortHash(x, 999) len = %d, want 64 (clamped)", len(h))
	}
}

func TestHashFile_StableAndDetectsChange(t *testing.T) {
	dir := t.TempDir()
	p := writeSource(t, dir, "f.txt", "hello")
	h1, ok := OfFile(p)
	if !ok {
		t.Fatal("hashFile ok=false on existing file")
	}
	h2, _ := OfFile(p)
	if h1 != h2 {
		t.Errorf("hash not stable: %q != %q", h1, h2)
	}
	if !strings.HasPrefix(h1, "sha256:") {
		t.Errorf("hash missing algorithm prefix: %q", h1)
	}
	os.WriteFile(p, []byte("hello world"), 0o644)
	h3, _ := OfFile(p)
	if h3 == h1 {
		t.Error("hash did not change after content change")
	}
}

func TestHashFile_MissingFile(t *testing.T) {
	if _, ok := OfFile(filepath.Join(t.TempDir(), "nope")); ok {
		t.Error("hashFile should return ok=false for missing file")
	}
}

// writeSource writes a source file and returns its absolute path.
func writeSource(t *testing.T, dir, name, contents string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}
