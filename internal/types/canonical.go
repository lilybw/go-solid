package types

import "strings"

// CanonicalTS rewrites a TypeScript type expression into the form used for
// comparison: comments dropped, insignificant whitespace dropped, commas folded
// onto semicolons, and trailing member separators removed.
//
// The result is a comparison key, not valid TypeScript. Both sides of every
// comparison pass through it, so a hand-written `{ a: string, b?: number }` and
// an emitted `{a: string; b?: number}` match.
//
// Whitespace between two word characters is significant ("keyof T") and is
// preserved as a single space; everywhere else it is dropped.
func CanonicalTS(s string) string {
	var b strings.Builder
	b.Grow(len(s))

	var (
		prev    rune // last rune written
		pending bool // whitespace or a comment seen since the last write
	)
	write := func(c rune) {
		if pending {
			if isWord(prev) && isWord(c) {
				b.WriteByte(' ')
			}
			pending = false
		}
		b.WriteRune(c)
		prev = c
	}

	runes := []rune(s)
	for i := 0; i < len(runes); i++ {
		c := runes[i]
		switch {
		case c == '/' && i+1 < len(runes) && runes[i+1] == '/':
			for i+1 < len(runes) && runes[i+1] != '\n' {
				i++
			}
			pending = true
		case c == '/' && i+1 < len(runes) && runes[i+1] == '*':
			i += 2
			for i+1 < len(runes) && !(runes[i] == '*' && runes[i+1] == '/') {
				i++
			}
			i++
			pending = true
		case c == '"' || c == '\'' || c == '`':
			write(c)
			for i++; i < len(runes); i++ {
				if runes[i] == '\\' && i+1 < len(runes) {
					b.WriteRune(runes[i])
					i++
					b.WriteRune(runes[i])
					prev = runes[i]
					continue
				}
				b.WriteRune(runes[i])
				prev = runes[i]
				if runes[i] == c {
					break
				}
			}
		case c == ',':
			write(';')
		case isSpace(c):
			pending = true
		default:
			write(c)
		}
	}

	out := strings.ReplaceAll(b.String(), ";}", "}")
	return strings.TrimSuffix(out, ";")
}

func isSpace(c rune) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' || c == '\v'
}

func isWord(c rune) bool {
	return c == '_' || c == '$' ||
		(c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}
