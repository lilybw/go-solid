package static

import (
	"strings"
	"unicode"
)

// Sanitizing a filename into a key
// -----------------------------------------------------------------------------
// Keys in the generated object are JavaScript identifiers, so a filename has to
// be turned into one. The rule is deliberately the smallest that works:
//
//	runs of characters an identifier cannot hold become a single "_"
//	a leading digit is prefixed with "_"
//
// and nothing else. No case conversion, so "logo-dark.svg" is logo_dark rather
// than logoDark. The point is that the key can be read off the filename without
// knowing a convention, and read back the other way just as easily — which
// matters more for an API surface generated behind your back than idiomatic
// casing does.
//
// Two filenames can still collide (logo-dark and logo_dark both give logo_dark).
// That is an error at build time naming both files, not a silent last-one-wins.

// sanitize converts a filename component into a JavaScript identifier.
func sanitize(name string) string {
	if name == "" {
		return ""
	}

	var b strings.Builder
	b.Grow(len(name) + 1)

	underscored := false
	for _, r := range name {
		if isIdentifierRune(r) {
			b.WriteRune(r)
			underscored = false
			continue
		}
		if !underscored {
			b.WriteByte('_')
			underscored = true
		}
	}

	out := b.String()
	// A name of nothing but separators collapses to "_", which is a valid
	// identifier and unambiguous enough to report a collision against.
	if out == "" {
		return "_"
	}
	if first := []rune(out)[0]; unicode.IsDigit(first) {
		return "_" + out
	}
	return out
}

// isIdentifierRune reports whether r may appear in a JavaScript identifier.
// Unicode letters are allowed, which keeps a non-ASCII filename readable rather
// than mangling it into underscores.
func isIdentifierRune(r rune) bool {
	return r == '_' || r == '$' || unicode.IsLetter(r) || unicode.IsDigit(r)
}
