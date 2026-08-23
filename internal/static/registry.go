package static

import (
	"os"
	"path/filepath"

	"github.com/lilybw/go-solid/internal/meta"
	"github.com/lilybw/go-solid/internal/noop"
	. "github.com/lilybw/go-solid/shared/static"
)

type StaticRegistry interface {
}

type enabledStaticRegistry struct {
	cfg   *StaticConfig
	graph *Node[os.DirEntry]
}

// NewStaticRegistry returns a disabled registry when no location is configured.
// Identity against NIL_STATIC_CONFIG is not a valid test: normalization hands
// each Config its own copy of the null object.
func NewStaticRegistry(cfg *StaticConfig) (StaticRegistry, error) {
	if cfg == nil || cfg.Location == "" {
		return &disabledStaticRegistry{
			cfg:   cfg,
			graph: nil,
		}, nil
	}

	graph, err := Scan(cfg.Location, noop.T_o_T[os.DirEntry](), cfg.Ignore...)
	if err != nil {
		return nil, err
	}

	return &enabledStaticRegistry{
		cfg:   cfg,
		graph: graph,
	}, nil
}

type Node[T any] struct {
	Self   T
	Leaves []T
	Nodes  []Node[T]
}

type disabledStaticRegistry struct {
	cfg   *StaticConfig
	graph *Node[os.DirEntry]
}

// Construct any graph representation of directory contents
func Scan[T any](directory meta.AbsoluteDirectoryPath, derive func(os.DirEntry) T, ignore ...FileSelectorPattern) (*Node[T], error) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, err
	}

	currentNode := Node[T]{
		Self:   derive(rootNode{name: string(directory), isDir: true}),
		Leaves: make([]T, 0),
		Nodes:  make([]Node[T], 0),
	}

	for _, entry := range entries {
		if skip, err := checkNameAgainstPatterns(entry.Name(), ignore...); err != nil {
			return nil, err
		} else if skip {
			continue
		}

		if entry.IsDir() {
			subDir := meta.AbsoluteDirectoryPath(filepath.Join(string(directory), entry.Name()))
			subNode, err := Scan(subDir, derive, ignore...)
			if err != nil {
				return nil, err
			}
			// Overwrite Self with the entry-derived value (Scan derives it
			// as a rootNode; the parent knows it via the real DirEntry).
			subNode.Self = derive(entry)
			currentNode.Nodes = append(currentNode.Nodes, *subNode)
		} else {
			currentNode.Leaves = append(currentNode.Leaves, derive(entry))
		}
	}

	return &currentNode, nil
}

func checkNameAgainstPatterns(name string, patterns ...FileSelectorPattern) (bool, error) {
	for _, pattern := range patterns {
		matches, err := filepath.Match(pattern, name)
		if err != nil {
			return false, err
		}
		if matches {
			return true, nil
		}
	}
	return false, nil
}

type rootNode struct {
	name  string
	isDir bool
}

func (n rootNode) Name() string               { return n.name }
func (n rootNode) IsDir() bool                { return n.isDir }
func (n rootNode) Type() os.FileMode          { return os.ModeDir }
func (n rootNode) Info() (os.FileInfo, error) { return os.Stat(n.name) }
