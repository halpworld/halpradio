package lyrics

import (
	"fmt"
	"html"
	"regexp"
	"strings"
	"time"
	"unicode"
)

var (
	// "Now Playing: ...", "*** CURRENT TRACK *** ..." and friends.
	bannerPrefixRe = regexp.MustCompile(`(?i)^[\s*#~_=♫♪▶►|•\-]*\b(now\s+playing|currently\s+playing|current\s+track|on\s+air|playing\s+now)\b[\s*#~_=♫♪▶►|•]*[:\-–—]?\s*`)

	decorPrefixRe = regexp.MustCompile(`^[\s*#~_=♫♪▶►•]+`)
	decorSuffixRe = regexp.MustCompile(`[\s*#~_=♫♪▶►•]+$`)

	// Advert, jingle and station-liner markers. A title containing one of these
	// carries no track information.
	adMarkerRe = regexp.MustCompile(`(?i)\b(advert(is(e|ing|ement))?s?|commercial\s+break|ad\s+break|adbreak|jingle|station\s+id|sponsored\s+by|buy\s+ads?\s+at|you(\s+are|'re)\s+listening\s+to|tune\s+in(\s+to)?|stay\s+tuned|we('ll|\s+will)\s+be\s+right\s+back|sweeper|promo(tion)?al\s+spot|news\s+bulletin|traffic\s+(report|and\s+weather)|weather\s+update)\b`)

	urlNoiseRe = regexp.MustCompile(`(?i)(https?://\S+|www\.[^\s|]+)`)

	// Whole-string values that carry no usable metadata.
	placeholderRe = regexp.MustCompile(`(?i)^(unknown(\s+(artist|track|title))?|no\s+(artist|title)|not\s+available|n/?a|none|various\s+artists|untitled|unnamed|track\s*\d*|audio\s*track|song|artist|title|default)$`)

	// YouTube style channel suffix.
	topicSuffixRe = regexp.MustCompile(`(?i)\s*[-–—]\s*topic\s*$`)

	// Trailing " | Some Station FM" / " / Some Station".
	stationSuffixRe = regexp.MustCompile(`\s*\|[^|]*$`)

	// Any square-bracketed chunk: "[HQ]", "[128kbps]", "[Official Video]".
	bracketRe = regexp.MustCompile(`\s*\[[^\]]*\]`)

	// Parenthesised marketing noise. Musically meaningful parentheticals such
	// as "(Remix)" or "(Live at Wembley)" are deliberately kept.
	noiseParenRe = regexp.MustCompile(`(?i)\s*\([^()]*\b(official(\s+\w+)*|lyrics?(\s+video)?|music\s+video|video|audio|visuali[sz]er|hd|hq|4k|full\s+album|free\s+download|explicit|clean\s+version|radio\s+edit|remaster(ed)?|\d{4}\s+remaster(ed)?|stream\s+version)\b[^()]*\)`)

	// "(feat. X)" / "[ft. X]" anywhere, and a bare "feat. X" tail.
	featParenRe = regexp.MustCompile(`(?i)\s*[\(\[]\s*(feat|ft|featuring|w/)\.?\s*[^)\]]*[\)\]]`)
	featBareRe  = regexp.MustCompile(`(?i)\s+(feat|ft|featuring)\.?\s+.*$`)

	// "- Remastered 2011", "- 2011 Remaster".
	remasterTailRe = regexp.MustCompile(`(?i)\s*[-–—]\s*((19|20)\d{2}\s+)?remaster(ed)?(\s+version)?(\s+(19|20)\d{2})?\s*$`)

	// A trailing release year, bare or bracketed.
	yearTailRe = regexp.MustCompile(`\s*[\(\[]?((19|20)\d{2})[\)\]]?\s*$`)

	whitespaceRe = regexp.MustCompile(`\s+`)

	// Playlist track numbers: "01. Artist - Title", "3) Artist - Title",
	// "01 - Artist - Title".
	trackNumberPrefixRe = regexp.MustCompile(`^\d{1,3}\s*[.)]\s+|^\d{1,3}\s+[-–—]\s+`)

	// Separators between artist and title, longest/most explicit first.
	dashSeparators = []string{" -- ", " - ", " – ", " — ", " ‐ ", " ‑ ", " − ", " : "}

	// Unspaced typographic dashes and double hyphens are safe to split on; a
	// bare ASCII hyphen is not, so it is only used as a last resort in
	// splitOnDash.
	tightDashSeparators = []string{"--", "–", "—"}
)

// SplitTrackTitle splits a raw radio stream title into artist and title.
// Handles "Artist - Title", "Artist – Title", "Artist — Title", "Title by Artist".
// It strips common station noise: leading/trailing whitespace, wrapping quotes,
// bracketed suffixes like "(Official Video)", "[HQ]", " - Topic", trailing
// " | StationName", and advert/jingle markers. Returns empty strings when the
// input is unusable (e.g. just a station name or an ad slug).
//
// The "Title by Artist" form is only recognised for a lower-case " by ", so
// that Title-Cased song names such as "Stand By Me" are not mis-split.
func SplitTrackTitle(raw string) (artist, title string) {
	cleaned := cleanStreamTitle(raw)
	if cleaned == "" {
		return "", ""
	}

	a, t, ok := splitOnDash(cleaned)
	if !ok {
		a, t, ok = splitOnBy(cleaned)
	}
	if !ok {
		return "", ""
	}

	a = finishPart(a)
	t = finishPart(t)
	if isUnusablePart(a) || isUnusablePart(t) {
		return "", ""
	}
	return a, t
}

// cleanStreamTitle removes station noise from a raw stream title and returns an
// empty string when nothing usable is left.
func cleanStreamTitle(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}

	s = html.UnescapeString(s)
	s = stripUnprintable(s)

	if adMarkerRe.MatchString(s) {
		return ""
	}

	s = bannerPrefixRe.ReplaceAllString(s, "")
	s = urlNoiseRe.ReplaceAllString(s, " ")
	s = topicSuffixRe.ReplaceAllString(s, "")
	s = stationSuffixRe.ReplaceAllString(s, "")
	s = bracketRe.ReplaceAllString(s, "")
	s = noiseParenRe.ReplaceAllString(s, "")
	s = decorPrefixRe.ReplaceAllString(s, "")
	s = decorSuffixRe.ReplaceAllString(s, "")
	s = unwrapQuotes(s)
	s = trackNumberPrefixRe.ReplaceAllString(collapseSpace(s), "")

	return collapseSpace(s)
}

// stripUnprintable drops control and non-printable runes that stations
// occasionally emit inside ICY metadata.
func stripUnprintable(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r == '\t' || r == ' ' {
			b.WriteRune(' ')
			continue
		}
		if r < 32 || r == 127 || !unicode.IsPrint(r) {
			continue
		}
		b.WriteRune(r)
	}
	return strings.TrimSpace(b.String())
}

func collapseSpace(s string) string {
	return strings.TrimSpace(whitespaceRe.ReplaceAllString(s, " "))
}

// unwrapQuotes removes a single matching pair of wrapping quotes.
func unwrapQuotes(s string) string {
	s = strings.TrimSpace(s)
	pairs := [][2]string{{`"`, `"`}, {"'", "'"}, {"“", "”"}, {"‘", "’"}, {"«", "»"}}
	for _, p := range pairs {
		if len(s) > len(p[0])+len(p[1]) && strings.HasPrefix(s, p[0]) && strings.HasSuffix(s, p[1]) {
			return strings.TrimSpace(s[len(p[0]) : len(s)-len(p[1])])
		}
	}
	return s
}

// splitOnDash splits at the earliest artist/title separator in s. When several
// separators start at the same offset the longest one wins.
func splitOnDash(s string) (string, string, bool) {
	for _, group := range [][]string{dashSeparators, tightDashSeparators} {
		at, sep := -1, ""
		for _, cand := range group {
			i := strings.Index(s, cand)
			if i <= 0 || i+len(cand) >= len(s) {
				continue
			}
			if at < 0 || i < at || (i == at && len(cand) > len(sep)) {
				at, sep = i, cand
			}
		}
		if at > 0 {
			return s[:at], s[at+len(sep):], true
		}
	}

	// Last resort: a single unspaced ASCII hyphen ("Artist-Title").
	if strings.Count(s, "-") == 1 {
		if i := strings.Index(s, "-"); i > 0 && i+1 < len(s) {
			return s[:i], s[i+1:], true
		}
	}
	return "", "", false
}

// splitOnBy handles the "Title by Artist" form.
func splitOnBy(s string) (string, string, bool) {
	const sep = " by "
	i := strings.LastIndex(s, sep)
	if i <= 0 || i+len(sep) >= len(s) {
		return "", "", false
	}
	return s[i+len(sep):], s[:i], true
}

// finishPart tidies one half of a split title.
func finishPart(s string) string {
	s = collapseSpace(s)
	s = unwrapQuotes(s)
	s = strings.Trim(s, " \t,;:-–—*_|")
	return collapseSpace(s)
}

// isUnusablePart reports whether a split half carries no track information.
func isUnusablePart(s string) bool {
	if s == "" {
		return true
	}
	if placeholderRe.MatchString(s) || adMarkerRe.MatchString(s) {
		return true
	}
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

// normalizeQuery strips featured-artist annotations, marketing suffixes,
// bracketed noise, remaster tails and trailing years so provider lookups and
// cache keys stay stable. The caller's original strings are never altered.
func normalizeQuery(s string) string {
	out := collapseSpace(stripUnprintable(html.UnescapeString(s)))
	if out == "" {
		return ""
	}

	out = bracketRe.ReplaceAllString(out, "")
	out = featParenRe.ReplaceAllString(out, "")
	out = featBareRe.ReplaceAllString(out, "")
	out = noiseParenRe.ReplaceAllString(out, "")
	out = remasterTailRe.ReplaceAllString(out, "")

	// Only drop a trailing year when something survives it, so that titles
	// which are themselves a year (e.g. "1999") stay intact.
	if trimmed := strings.TrimSpace(yearTailRe.ReplaceAllString(out, "")); trimmed != "" {
		out = trimmed
	}

	out = strings.Trim(collapseSpace(out), " \t.,;:-–—*_|")
	return collapseSpace(out)
}

// normalizeKey lower-cases a normalised value for use inside cache keys.
func normalizeKey(s string) string {
	return strings.ToLower(normalizeQuery(s))
}

// cacheKey builds the stable identity of a lyric lookup. It never contains raw
// user text in a form that could escape a directory, because callers hash it.
func cacheKey(artist, title, album string, duration time.Duration) string {
	secs := int64(0)
	if duration > 0 {
		secs = int64(duration.Round(time.Second) / time.Second)
	}
	return fmt.Sprintf("%s|%s|%s|%d",
		normalizeKey(artist), normalizeKey(title), normalizeKey(album), secs)
}
