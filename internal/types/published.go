package types

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/lilybw/go-solid/shared/meta"
	. "github.com/lilybw/go-solid/shared/types"
)

// PublishedRoot is the importable definition surface, <workspace>/types.
// Nothing derived from a component is written here.
func PublishedRoot(workspace meta.AbsoluteDirectoryPath) meta.AbsoluteDirectoryPath {
	return filepath.Join(workspace, TYPES_DIR_NAME)
}

// EnsurePublished creates the published directory, so the path a component
// imports from resolves before anything has been synthesised into it.
func EnsurePublished(workspace meta.AbsoluteDirectoryPath) error {
	root := PublishedRoot(workspace)
	if err := os.MkdirAll(root, 0o755); err != nil {
		return fmt.Errorf("go_solid/types: create %q: %w", root, err)
	}
	return nil
}
