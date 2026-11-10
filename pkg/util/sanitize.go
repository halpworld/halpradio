package util

import (
	"strings"
	"unicode"
)

// SanitizeDisplay makes untrusted text safe to write to a terminal, a window
// title, the clipboard or a desktop notification.
//
// It drops every rune that is not printable: C0 controls (ESC, BEL, CR, BS…),
// DEL, C1 controls (U+0080–U+009F, which some terminals treat as CSI/OSC
// introducers), and Unicode format characters (bidi overrides, zero-width
// characters, BOM). Runs of whitespace, including newlines and tabs, collapse
// to one space and leading/trailing whitespace is removed. Invalid UTF-8
// becomes U+FFFD. The result holds at most maxRunes runes; maxRunes <= 0
// means no cap.
//
// Every string that comes from outside the program (station catalogs, stream
// metadata, lyrics, party peers, plugins, theme registries, HTTP error bodies)
// must pass through this function, or SanitizeMultiline, before it is stored
// in the UI model or printed.
func SanitizeDisplay(s string, maxRunes int) string {
	var b strings.Builder
	b.Grow(min(len(s), 256))
	n := 0
	pendingSpace := false
	for _, r := range s {
		if unicode.IsSpace(r) {
			pendingSpace = n > 0
			continue
		}
		if !unicode.IsPrint(r) {
			continue
		}
		need := 1
		if pendingSpace {
			need = 2
		}
		if maxRunes > 0 && n+need > maxRunes {
			break
		}
		if pendingSpace {
			b.WriteByte(' ')
			n++
			pendingSpace = false
		}
		b.WriteRune(r)
		n++
	}
	return b.String()
}

// SanitizeMultiline is SanitizeDisplay for text where line breaks carry
// meaning (lyrics, liner notes). Each line is sanitized on its own and capped
// at maxLineRunes; at most maxLines lines are kept. "\r\n" and "\r" count as
// line breaks. A cap <= 0 means no cap for that dimension.
func SanitizeMultiline(s string, maxLineRunes, maxLines int) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	var b strings.Builder
	b.Grow(min(len(s), 4096))
	lines := 0
	for len(s) > 0 {
		if maxLines > 0 && lines >= maxLines {
			break
		}
		line, rest, found := strings.Cut(s, "\n")
		if lines > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(SanitizeDisplay(line, maxLineRunes))
		lines++
		if !found {
			break
		}
		s = rest
	}
	return b.String()
}

// IsDisplaySafe reports whether s is already free of everything
// SanitizeDisplay would remove or rewrite, other than the length cap. Use it
// where untrusted input should be rejected instead of silently cleaned, such
// as plugin manifests whose text is shown in a permission prompt.
func IsDisplaySafe(s string) bool {
	return SanitizeDisplay(s, 0) == s
}
