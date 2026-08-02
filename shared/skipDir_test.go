package shared

import (
	"path/filepath"
	"testing"
)

func Test_SkipsNodeModulesAndDotDirs(t *testing.T) {
	// skipDir is pure; test it directly rather than through fs timing.
	root := "/project/components"
	cases := []struct {
		base, path string
		want       bool
	}{
		{"node_modules", filepath.Join(root, "node_modules"), true},
		{".go_solid", filepath.Join(root, ".go_solid"), true},
		{".git", filepath.Join(root, ".git"), true},
		{"ui", filepath.Join(root, "ui"), false},
		// The root itself, even if dot-prefixed, must not be skipped.
		{".components", root, false}, // path == root exempts it
	}
	for _, c := range cases {
		if got := SkipDir(c.base, c.path, root); got != c.want {
			t.Errorf("skipDir(%q, %q, root=%q) = %v, want %v", c.base, c.path, root, got, c.want)
		}
	}
}
