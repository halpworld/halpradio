package lyrics

import (
	"time"

	"github.com/halpworld/halpradio/pkg/util"
)

// Limits on what a lyrics provider (or a cache file it once filled) can put
// in front of the terminal and into memory.
const (
	// maxBodyBytes caps one decoded provider response. Real LRCLIB and
	// NetEase answers are a few KB; Go's transport decompresses gzip, so the
	// cap applies to the decompressed size.
	maxBodyBytes = 2 << 20
	// maxLineRunes caps one lyric line.
	maxLineRunes = 500
	// maxLines caps the number of lines kept in a sheet.
	maxLines = 2000
	// maxMetaRunes caps artist, title, album and source strings.
	maxMetaRunes = 200
	// maxSearchCandidates caps how many LRCLIB / NetEase search hits are
	// looked at.
	maxSearchCandidates = 50

	// Provider throttle: each provider gets limiterBurst lookups at once and
	// then one every limiterInterval, however fast the track title changes.
	limiterBurst    = 4
	limiterInterval = 15 * time.Second
	// failureTTL is how long a key whose providers all failed is not retried.
	failureTTL = 30 * time.Second
	// maxFailures bounds the failure dedup map.
	maxFailures = 256

	// Disk cache bounds, enforced when the cache is written.
	diskMaxBytes = 20 << 20
	pruneEvery   = 32
)

// sanitizeSheet strips terminal control sequences, C1 controls, bidi and
// zero-width characters from every string in s and caps line length and line
// count. Lyrics come from third parties and from a cache those third parties
// filled, so this runs both at parse time and on every disk-cache read.
func sanitizeSheet(s *Sheet) *Sheet {
	if s == nil {
		return nil
	}
	s.Artist = util.SanitizeDisplay(s.Artist, maxMetaRunes)
	s.Title = util.SanitizeDisplay(s.Title, maxMetaRunes)
	s.Album = util.SanitizeDisplay(s.Album, maxMetaRunes)
	s.Source = util.SanitizeDisplay(s.Source, maxMetaRunes)
	if len(s.Lines) > maxLines {
		s.Lines = s.Lines[:maxLines]
	}
	for i := range s.Lines {
		s.Lines[i].Text = util.SanitizeDisplay(s.Lines[i].Text, maxLineRunes)
		if s.Lines[i].At < 0 {
			s.Lines[i].At = 0
		}
	}
	return s
}
