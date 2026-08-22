package types

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/lilybw/go-solid/internal/meta"
	. "github.com/lilybw/go-solid/shared/types"
)

// PublishedRoot is the importable definition surface, <workspace>/types.
//
// Nothing derived from a component is written here. A component already states
// its own props type, and that statement is the contract go_solid checks
// against — restating it as a generated file would only add a second copy to
// keep in step. Extracted shapes live in the cache under CACHE_DIR_NAME
// instead, where the format is free to change.
//
// What this directory is for is definitions go_solid synthesises from what only
// it knows — routes, static assets, and so on — which a component composes into
// its props:
//
//	import type { Navigation } from "../.go_solid/types/navigation";
//	export default function Page(props: { title: string } & Navigation) { ... }
//
// The extractor follows those relative imports, so a composed props type is
// checked as a whole.
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
