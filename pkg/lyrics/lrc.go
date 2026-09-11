package lyrics

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

var (
	// Leading timestamp of an LRC line, e.g. "[01:23.45]" / "[01:23]" / "[01:23.456]".
	lrcStampRe = regexp.MustCompile(`^\[\s*(\d{1,3}):(\d{1,2})(?:[.:](\d{1,3}))?\s*\]`)

	// Enhanced (word level) LRC timings embedded in the text, e.g. "<00:12.34>".
	lrcWordStampRe = regexp.MustCompile(`<\s*\d{1,3}:\d{1,2}(?:[.:]\d{1,3})?\s*>`)

	// Global offset tag in milliseconds, e.g. "[offset:+250]".
	lrcOffsetRe = regexp.MustCompile(`(?i)^\[\s*offset\s*:\s*([+-]?\d{1,9})\s*\]$`)
)

// ParseLRC parses an LRC document into timestamped lines, sorted ascending.
// Handles [mm:ss.xx], [mm:ss.xxx], [mm:ss], multiple timestamps on one line,
// and skips metadata tags like [ar:], [ti:], [al:], [length:], [offset:].
// An [offset:NNN] tag (milliseconds) shifts every timestamp accordingly: a
// positive offset delays the lines, a negative one advances them, and no
// timestamp is ever shifted below zero. Lines that carry a timestamp but no
// text are preserved, because they mark instrumental gaps.
func ParseLRC(raw string) []Line {
	if strings.TrimSpace(raw) == "" {
		return nil
	}

	var (
		lines  []Line
		offset time.Duration
	)

	for _, rawLine := range strings.Split(raw, "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}

		if m := lrcOffsetRe.FindStringSubmatch(line); m != nil {
			if ms, err := strconv.Atoi(m[1]); err == nil {
				offset = time.Duration(ms) * time.Millisecond
			}
			continue
		}

		stamps, rest := leadingTimestamps(line)
		if len(stamps) == 0 {
			// Metadata tag ([ar:], [ti:], ...) or untimed filler; skip it.
			continue
		}

		text := strings.TrimSpace(lrcWordStampRe.ReplaceAllString(rest, ""))
		for _, at := range stamps {
			lines = append(lines, Line{At: at, Text: text})
		}
	}

	if offset != 0 {
		for i := range lines {
			lines[i].At += offset
			if lines[i].At < 0 {
				lines[i].At = 0
			}
		}
	}

	sort.SliceStable(lines, func(i, j int) bool { return lines[i].At < lines[j].At })
	return lines
}

// leadingTimestamps peels every timestamp off the front of an LRC line and
// returns them together with the remaining lyric text.
func leadingTimestamps(line string) ([]time.Duration, string) {
	var stamps []time.Duration
	rest := line

	for {
		idx := lrcStampRe.FindStringSubmatchIndex(rest)
		if idx == nil {
			break
		}
		group := func(n int) string {
			if 2*n+1 >= len(idx) || idx[2*n] < 0 {
				return ""
			}
			return rest[idx[2*n]:idx[2*n+1]]
		}

		minutes, err := strconv.Atoi(group(1))
		if err != nil {
			break
		}
		seconds, err := strconv.Atoi(group(2))
		if err != nil {
			break
		}

		at := time.Duration(minutes)*time.Minute + time.Duration(seconds)*time.Second
		at += fractionToDuration(group(3))
		stamps = append(stamps, at)

		rest = strings.TrimLeft(rest[idx[1]:], " \t")
	}

	return stamps, rest
}

// fractionToDuration converts the sub-second digits of a timestamp, which may
// be tenths, hundredths, or milliseconds, into a duration.
func fractionToDuration(frac string) time.Duration {
	if frac == "" {
		return 0
	}
	n, err := strconv.Atoi(frac)
	if err != nil {
		return 0
	}
	switch len(frac) {
	case 1:
		return time.Duration(n) * 100 * time.Millisecond
	case 2:
		return time.Duration(n) * 10 * time.Millisecond
	default:
		return time.Duration(n) * time.Millisecond
	}
}

// plainToLines converts an unsynced lyric blob into untimed lines, trimming
// leading and trailing blank lines.
func plainToLines(raw string) []Line {
	if strings.TrimSpace(raw) == "" {
		return nil
	}

	fields := strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")
	start, end := 0, len(fields)
	for start < end && strings.TrimSpace(fields[start]) == "" {
		start++
	}
	for end > start && strings.TrimSpace(fields[end-1]) == "" {
		end--
	}
	if start >= end {
		return nil
	}

	out := make([]Line, 0, end-start)
	for _, f := range fields[start:end] {
		out = append(out, Line{Text: strings.TrimSpace(f)})
	}
	return out
}
