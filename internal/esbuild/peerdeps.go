package esbuild

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/lilybw/go-solid/internal/meta"
)

// RequiredPeerDeps are the npm packages the consumer must have installed.
//
// Only the browser runtime: the generated entry imports solid-js/web, so
// esbuild has to resolve the consumer's own solid-js. The compiler is Go
// (github.com/lilybw/go-solid-compiler); no Node runtime is involved at any
// point, so nothing else needs installing.
var RequiredPeerDeps = []string{"solid-js"}

// PeerDepsMissing returns which of pkgs cannot be resolved from startDir,
// walking up ancestor directories the way esbuild resolves node_modules.
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
// ancestor. Scoped names like "@babel/core" map to the nested path @babel/core.
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
