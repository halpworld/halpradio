// Package lyrics implements halpradio's synced-lyrics engine.
//
// It provides LRC parsing, radio stream title splitting, and a lyric Client
// that resolves tracks against LRCLIB with a NetEase fallback, memoising
// results in RAM and on disk. The package depends on the standard library
// only and never panics: every failure path returns an error.
package lyrics

import (
	"strings"
	"time"
)

// Line is one lyric line with its playback offset. For unsynced sheets At is 0.
type Line struct {
	At   time.Duration
	Text string
}

// Sheet is a fetched lyric document for one track.
type Sheet struct {
	Artist       string
	Title        string
	Album        string
	Duration     time.Duration
	Synced       bool // true when Lines carry real timestamps
	Instrumental bool
	Lines        []Line
	Source       string // "LRCLIB" or "NetEase"
}

// ActiveIndex returns the index of the line that should be highlighted at the
// given elapsed playback offset, or -1 when the sheet is unsynced or elapsed
// precedes the first line.
func (s *Sheet) ActiveIndex(elapsed time.Duration) int {
	if s == nil || !s.Synced || len(s.Lines) == 0 {
		return -1
	}
	if elapsed < s.Lines[0].At {
		return -1
	}

	// Binary search for the last line whose timestamp is <= elapsed.
	lo, hi := 0, len(s.Lines)-1
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if s.Lines[mid].At <= elapsed {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	return lo
}

// Progress returns how far (0.0-1.0) playback has advanced through the line at
// index i, or 0 when unknown.
func (s *Sheet) Progress(i int, elapsed time.Duration) float64 {
	if s == nil || !s.Synced || i < 0 || i >= len(s.Lines) {
		return 0
	}

	start := s.Lines[i].At
	var end time.Duration
	switch {
	case i+1 < len(s.Lines):
		end = s.Lines[i+1].At
	case s.Duration > start:
		end = s.Duration
	default:
		// Last line of a sheet with unknown total duration.
		return 0
	}

	if end <= start || elapsed <= start {
		return 0
	}
	if elapsed >= end {
		return 1
	}
	return float64(elapsed-start) / float64(end-start)
}

// IsEmpty reports whether the sheet carries no renderable lines.
func (s *Sheet) IsEmpty() bool {
	if s == nil {
		return true
	}
	for _, l := range s.Lines {
		if strings.TrimSpace(l.Text) != "" {
			return false
		}
	}
	return true
}

// PlainLines returns the lyric text without timestamps.
func (s *Sheet) PlainLines() []string {
	if s == nil || len(s.Lines) == 0 {
		return nil
	}
	out := make([]string, 0, len(s.Lines))
	for _, l := range s.Lines {
		out = append(out, l.Text)
	}
	return out
}

// clone returns a deep copy so cached sheets can never be mutated by callers.
func (s *Sheet) clone() *Sheet {
	if s == nil {
		return nil
	}
	out := *s
	if s.Lines != nil {
		out.Lines = make([]Line, len(s.Lines))
		copy(out.Lines, s.Lines)
	}
	return &out
}
