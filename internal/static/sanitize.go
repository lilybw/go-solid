package static

import (
	"strings"
	"unicode"
)

// sanitize converts a filename component into a JavaScript identifier.
func sanitize(name string) SanitizedString {
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
func isIdentifierRune(r rune) bool {
	return r == '_' || r == '$' || unicode.IsLetter(r) || unicode.IsDigit(r)
}
