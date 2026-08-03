package rasterization

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	caching_int "github.com/lilybw/go-solid/internal/caching"
	"github.com/lilybw/go-solid/internal/meta"
	. "github.com/lilybw/go-solid/shared/rasterization"
)

// writeValidLayout builds a fully valid rasterization location under a temp dir
// and returns its path. Individual tests call this, then mutate one thing to
// exercise a specific failure branch.
func writeValidLayout(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	// transform-worker.*.js at the top level
	mustWrite(t, filepath.Join(root, "transform-worker.abc123.mjs"), "// worker")

	// component_cache/ subdir
	cacheDir := filepath.Join(root, caching_int.CACHE_DIR_NAME)
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatalf("mkdir cache dir: %v", err)
	}

	// _index.json
	mustWrite(t, filepath.Join(cacheDir, "_index.json"), "{}")

	// A valid manifest + its referenced artifacts.
	writeEntry(t, cacheDir, "auth_LoginForm__root-a__deadbeef", func(m *caching_int.ComponentDiskManifest) {
		// default is already valid
	})

	return root
}

// writeEntry writes a <stem>.meta.json plus the HTML/JS/CSS artifacts it names.
// mutate lets a caller tweak the manifest before it's written.
func writeEntry(t *testing.T, cacheDir, stem string, mutate func(*caching_int.ComponentDiskManifest)) {
	t.Helper()

	var m caching_int.ComponentDiskManifest
	m.Component = "auth/LoginForm"
	m.Key = "deadbeefcafe"
	m.Artifacts.HTML = meta.RelativeFilePath(stem + ".html")
	m.Artifacts.JS = meta.RelativeFilePath(stem + ".js")
	// no CSS by default

	if mutate != nil {
		mutate(&m)
	}

	// Write artifacts the manifest points at (only the non-empty ones).
	if m.Artifacts.HTML != "" {
		mustWrite(t, filepath.Join(cacheDir, string(m.Artifacts.HTML)), "<div></div>")
	}
	if m.Artifacts.JS != "" {
		mustWrite(t, filepath.Join(cacheDir, string(m.Artifacts.JS)), "// js")
	}
	if m.Artifacts.CSS != "" {
		mustWrite(t, filepath.Join(cacheDir, string(m.Artifacts.CSS)), "/* css */")
	}

	b, err := json.MarshalIndent(&m, "", "  ")
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	mustWrite(t, filepath.Join(cacheDir, stem+".meta.json"), string(b))
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func cfgFor(dir string) *RasterizationConfig {
	return &RasterizationConfig{Location: meta.AbsoluteDirectoryPath(dir)}
}

func TestExpectCompletedValidationCheck_Valid(t *testing.T) {
	root := writeValidLayout(t)
	if err := ExpectCompletedValidationCheck(cfgFor(root)); err != nil {
		t.Fatalf("expected valid config, got error: %v", err)
	}
}

func TestExpectCompletedValidationCheck_Failures(t *testing.T) {
	tests := []struct {
		name string
		// mutate corrupts an otherwise-valid layout to trigger one failure path.
		mutate  func(t *testing.T, root string)
		wantSub string // substring expected in the error message
	}{
		{
			name: "missing transform-worker",
			mutate: func(t *testing.T, root string) {
				removeGlob(t, filepath.Join(root, "transform-worker.*.mjs"))
			},
			wantSub: "does not contain transform-worker",
		},
		{
			name: "missing component_cache dir",
			mutate: func(t *testing.T, root string) {
				if err := os.RemoveAll(filepath.Join(root, caching_int.CACHE_DIR_NAME)); err != nil {
					t.Fatal(err)
				}
			},
			wantSub: "does not contain component_cache dir",
		},
		{
			name: "component_cache is a file not a dir",
			mutate: func(t *testing.T, root string) {
				p := filepath.Join(root, caching_int.CACHE_DIR_NAME)
				if err := os.RemoveAll(p); err != nil {
					t.Fatal(err)
				}
				mustWrite(t, p, "not a dir")
			},
			wantSub: "component_cache is not a directory",
		},
		{
			name: "no meta.json files",
			mutate: func(t *testing.T, root string) {
				removeGlob(t, filepath.Join(root, caching_int.CACHE_DIR_NAME, "*.meta.json"))
			},
			wantSub: "no *.meta.json files",
		},
		{
			name: "manifest fails validation (missing component)",
			mutate: func(t *testing.T, root string) {
				cacheDir := filepath.Join(root, caching_int.CACHE_DIR_NAME)
				removeGlob(t, filepath.Join(cacheDir, "*.meta.json"))
				writeEntry(t, cacheDir, "bad__root-a__deadbeef", func(m *caching_int.ComponentDiskManifest) {
					m.Component = "" // Validate() rejects this
				})
			},
			wantSub: "is invalid",
		},
		{
			name: "manifest references missing HTML artifact",
			mutate: func(t *testing.T, root string) {
				cacheDir := filepath.Join(root, caching_int.CACHE_DIR_NAME)
				rm(t, filepath.Join(cacheDir, "auth_LoginForm__root-a__deadbeef.html"))
			},
			wantSub: "missing HTML artifact",
		},
		{
			name: "manifest references missing JS artifact",
			mutate: func(t *testing.T, root string) {
				cacheDir := filepath.Join(root, caching_int.CACHE_DIR_NAME)
				rm(t, filepath.Join(cacheDir, "auth_LoginForm__root-a__deadbeef.js"))
			},
			wantSub: "missing JS artifact",
		},
		{
			name: "manifest references missing CSS artifact",
			mutate: func(t *testing.T, root string) {
				cacheDir := filepath.Join(root, caching_int.CACHE_DIR_NAME)
				removeGlob(t, filepath.Join(cacheDir, "*.meta.json"))
				// Declare CSS in the manifest but don't write the css file.
				writeEntry(t, cacheDir, "auth_LoginForm__root-a__deadbeef", func(m *caching_int.ComponentDiskManifest) {
					m.Artifacts.CSS = "auth_LoginForm__root-a__deadbeef.css"
				})
				rm(t, filepath.Join(cacheDir, "auth_LoginForm__root-a__deadbeef.css"))
			},
			wantSub: "missing CSS artifact",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := writeValidLayout(t)
			tt.mutate(t, root)

			err := ExpectCompletedValidationCheck(cfgFor(root))
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.wantSub)
			}
			if !strings.Contains(err.Error(), tt.wantSub) {
				t.Fatalf("error = %q, want substring %q", err.Error(), tt.wantSub)
			}
		})
	}
}

func rm(t *testing.T, path string) {
	t.Helper()
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove %s: %v", path, err)
	}
}

func removeGlob(t *testing.T, pattern string) {
	t.Helper()
	matches, err := filepath.Glob(pattern)
	if err != nil {
		t.Fatalf("glob %s: %v", pattern, err)
	}
	if len(matches) == 0 {
		t.Fatalf("glob %s matched nothing (fixture assumption wrong?)", pattern)
	}
	for _, m := range matches {
		if err := os.Remove(m); err != nil {
			t.Fatalf("remove %s: %v", m, err)
		}
	}
}
