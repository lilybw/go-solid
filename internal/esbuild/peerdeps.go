package esbuild

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"

	"github.com/lilybw/go_solid/internal/meta"
)

// requiredPeerDeps are the Node packages the consumer must provide (Model A:
// peer dependencies). solid-js is the browser RUNTIME and must be the
// consumer's version; babel-preset-solid   @babel/core are the compiler.
// go_solid deliberately does not vendor these.
var RequiredPeerDeps = []string{"solid-js", "babel-preset-solid", "@babel/core"}

// peerDepsMissing returns which of pkgs cannot be resolved from startDir,
// walking up ancestor directories the way Node/esbuild resolve node_modules.
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

//go:embed transform-worker.mjs
var rawEmbeddedWorkerScript []byte

var parsedEmbeddedWorkerScript meta.AbsoluteFilePath

// materializeWorkerScript writes the embedded worker script to a stable,
// content-hashed path under the user cache dir and returns it. The library
// ships the script embedded in the binary, so consumers never provide a path;
// it works even when the module source isn't present at runtime (deployed
// binaries). Content-hashing means a library upgrade that changes the script
// lands at a new path automatically; identical content reuses the file.
func MaterializeWorkerScript(dir meta.AbsoluteDirectoryPath) (meta.AbsoluteFilePath, error) {
	if parsedEmbeddedWorkerScript != "" {
		return parsedEmbeddedWorkerScript, nil
	}

	sum := sha256.Sum256(rawEmbeddedWorkerScript)
	hash := hex.EncodeToString(sum[:])[:16]

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	dst := filepath.Join(dir, "transform-worker."+hash+".mjs")

	if _, err := os.Stat(dst); err == nil {
		return dst, nil // already materialized, identical content
	}

	// Write to temp then atomically rename so concurrent starts never see a
	// half-written script.
	tmp, err := os.CreateTemp(dir, "worker-*.mjs.tmp")
	if err != nil {
		return "", err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(rawEmbeddedWorkerScript); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(tmpName, dst); err != nil {
		if _, statErr := os.Stat(dst); statErr == nil {
			return dst, nil // lost a race, file exists — fine
		}
		return "", err
	}
	return dst, nil
}
