package static

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/lilybw/go-solid/internal/meta"
)

// deriveName is a simple derive func returning the entry's base name.
func deriveName(e os.DirEntry) string { return e.Name() }

// buildTree creates:
//
//	root/
//	  a.txt
//	  b.txt
//	  sub1/
//	    c.txt
//	    sub1a/
//	      d.txt
//	  sub2/        (empty)
func buildTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	mustWrite := func(p string) {
		t.Helper()
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
	}
	mustMkdir := func(p string) {
		t.Helper()
		if err := os.Mkdir(p, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", p, err)
		}
	}

	mustWrite(filepath.Join(root, "a.txt"))
	mustWrite(filepath.Join(root, "b.txt"))
	mustMkdir(filepath.Join(root, "sub1"))
	mustWrite(filepath.Join(root, "sub1", "c.txt"))
	mustMkdir(filepath.Join(root, "sub1", "sub1a"))
	mustWrite(filepath.Join(root, "sub1", "sub1a", "d.txt"))
	mustMkdir(filepath.Join(root, "sub2"))

	return root
}

func sortedLeaves(n *Node[string]) []string {
	out := append([]string(nil), n.Leaves...)
	sort.Strings(out)
	return out
}

func findChild(n *Node[string], name string) *Node[string] {
	for i := range n.Nodes {
		if n.Nodes[i].Self == name {
			return &n.Nodes[i]
		}
	}
	return nil
}

func TestScan_TopLevel(t *testing.T) {
	root := buildTree(t)

	node, err := Scan(meta.AbsoluteDirectoryPath(root), deriveName)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if node == nil {
		t.Fatal("Scan returned nil node")
	}

	if got, want := sortedLeaves(node), []string{"a.txt", "b.txt"}; !equal(got, want) {
		t.Errorf("top-level leaves = %v, want %v", got, want)
	}
	if len(node.Nodes) != 2 {
		t.Fatalf("top-level subdirs = %d, want 2", len(node.Nodes))
	}
}

func TestScan_Recursion(t *testing.T) {
	root := buildTree(t)

	node, err := Scan(meta.AbsoluteDirectoryPath(root), deriveName)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	sub1 := findChild(node, "sub1")
	if sub1 == nil {
		t.Fatal("sub1 not found")
	}
	if got, want := sortedLeaves(sub1), []string{"c.txt"}; !equal(got, want) {
		t.Errorf("sub1 leaves = %v, want %v", got, want)
	}

	sub1a := findChild(sub1, "sub1a")
	if sub1a == nil {
		t.Fatal("sub1a not found")
	}
	if got, want := sortedLeaves(sub1a), []string{"d.txt"}; !equal(got, want) {
		t.Errorf("sub1a leaves = %v, want %v", got, want)
	}
}

func TestScan_EmptyDir(t *testing.T) {
	root := buildTree(t)

	node, err := Scan(meta.AbsoluteDirectoryPath(root), deriveName)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	sub2 := findChild(node, "sub2")
	if sub2 == nil {
		t.Fatal("sub2 not found")
	}
	if len(sub2.Leaves) != 0 || len(sub2.Nodes) != 0 {
		t.Errorf("sub2 = %+v, want empty", sub2)
	}
}

func TestScan_NonexistentDir(t *testing.T) {
	_, err := Scan(meta.AbsoluteDirectoryPath("/no/such/path/hopefully"), deriveName)
	if err == nil {
		t.Fatal("expected error for nonexistent dir, got nil")
	}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
