package esbuild

import (
	"os"
	"path/filepath"
	"strings"

	. "github.com/lilybw/go-solid/shared/esbuild"
	"github.com/lilybw/go-solid/shared/meta"
)

// PeerDepsForConfig returns the packages that must resolve from disk for a
// given configuration.
func PeerDepsForConfig(cfg SolidConfig) []string {
	if cfg.Runtime == RuntimeExternal {
		return []string{"solid-js"}
	}
	return nil
}

// PeerDepsMissing returns which of pkgs cannot be resolved from startDir,
// walking up ancestor directories the way Node and esbuild resolve
// node_modules.
func PeerDepsMissing(startDir meta.AbsoluteDirectoryPath, pkgs []string) []string {
	var missing []string
	for _, pkg := range pkgs {
		if !peerDepResolvable(startDir, pkg) {
			missing = append(missing, pkg)
		}
	}
	return missing
}

// peerDepResolvable reports whether node_modules/<pkg> exists in startDir or any
// ancestor. Scoped names like "@scope/pkg" map to the nested path @scope/pkg.
func peerDepResolvable(start meta.AbsoluteDirectoryPath, pkg string) bool {
	rel := filepath.Join(strings.Split(pkg, "/")...)
	dir := start
	for {
		if _, err := os.Stat(filepath.Join(dir, "node_modules", rel)); err == nil {
			return true
		}
		parent := filepath.Dir(dir)
		if parent == dir { // reached filesystem root
			return false
		}
		dir = parent
	}
}
