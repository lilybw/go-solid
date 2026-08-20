package esbuild

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/lilybw/go-solid/internal/meta"
)

// RequiredPeerDeps are the Node packages the consumer must provide.
//
// Only the browser runtime remains: solid-js must be the consumer's version,
// because the code go_solid emits calls into it. The compiler itself is Go and
// needs nothing installed.
var RequiredPeerDeps = []string{"solid-js"}

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
