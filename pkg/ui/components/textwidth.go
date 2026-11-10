package components

import "github.com/charmbracelet/lipgloss"

// fitWidth returns the longest prefix of s (cut at a rune boundary) whose
// display width is at most max columns, and the rest.
//
// TTY-7: the old helpers re-measured the whole string for every rune they
// removed, so one huge station name or lyric line froze View(). This looks
// only at the first 4*max+16 runes (a fitting prefix longer than that would
// be mostly zero-width runes; cutting it earlier is harmless) and
// binary-searches the cut point, measuring candidates with lipgloss.Width so
// grapheme clusters such as emoji + VS16 are counted the way lipgloss
// renders them. Cost: O(max log max), independent of len(s).
func fitWidth(s string, max int) (string, string) {
	if max <= 0 {
		return "", s
	}
	limit := 4*max + 16
	bounds := make([]int, 0, limit+1)
	whole := true
	for i := range s {
		if len(bounds) > limit {
			whole = false
			break
		}
		bounds = append(bounds, i)
	}
	if whole {
		if lipgloss.Width(s) <= max {
			return s, ""
		}
		bounds = append(bounds, len(s))
	}
	last := len(bounds) - 1
	if lipgloss.Width(s[:bounds[last]]) <= max {
		return s[:bounds[last]], s[bounds[last]:]
	}
	lo, hi := 0, last // s[:bounds[lo]] fits, s[:bounds[hi]] does not
	for hi-lo > 1 {
		mid := (lo + hi) / 2
		if lipgloss.Width(s[:bounds[mid]]) <= max {
			lo = mid
		} else {
			hi = mid
		}
	}
	return s[:bounds[lo]], s[bounds[lo]:]
}
