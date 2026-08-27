package hashing

import (
	"crypto/sha256"
	"encoding/hex"
	"os"

	"github.com/lilybw/go-solid/internal/meta"
)

const PREFIX = "sha256:"

func OfBytes(data []byte) meta.ContentDigest {
	sum := sha256.Sum256(data)
	return PREFIX + hex.EncodeToString(sum[:])
}

func OfFile(path meta.AbsoluteFilePath) (meta.ContentDigest, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	return OfBytes(raw), true
}

// Short returns the first n hex characters of a digest of s
func Short(s string, n int) string {
	sum := sha256.Sum256([]byte(s))
	full := hex.EncodeToString(sum[:])
	return full[:min(n, len(full))]
}

// Holds reports whether every recorded path still hashes to its recorded digest
func Holds(recorded map[meta.AbsoluteFilePath]meta.ContentDigest) bool {
	for path, want := range recorded {
		got, ok := OfFile(path)
		if !ok || got != want {
			return false
		}
	}
	return len(recorded) > 0
}

// Record digests every path, failing on the first that cannot be read.
func Record(paths []meta.AbsoluteFilePath) (map[meta.AbsoluteFilePath]meta.ContentDigest, meta.AbsoluteFilePath, bool) {
	out := make(map[meta.AbsoluteFilePath]meta.ContentDigest, len(paths))
	for _, path := range paths {
		digest, ok := OfFile(path)
		if !ok {
			return nil, path, false
		}
		out[path] = digest
	}
	return out, "", true
}
