package fingerprint

import (
	"fmt"
	"strings"
)

// Result contains acoustically verified track metadata and confidence.
type Result struct {
	Artist      string  `json:"artist"`
	Title       string  `json:"title"`
	Album       string  `json:"album,omitempty"`
	Year        int     `json:"year,omitempty"`
	Confidence  float64 `json:"confidence"` // 0.0 to 1.0 (e.g. 0.94)
	Source      string  `json:"source"`     // e.g. "AcoustID", "MusicBrainz"
	StationID   string  `json:"station_id,omitempty"`
	StationName string  `json:"station_name,omitempty"`
	Fingerprint string  `json:"fingerprint,omitempty"`
	Duration    float64 `json:"duration,omitempty"`
}

// Badge returns the formatted UI identification badge.
func (r *Result) Badge() string {
	if r == nil {
		return ""
	}
	src := r.Source
	if src == "" {
		src = "AcoustID"
	}
	pct := int(r.Confidence * 100)
	if pct <= 0 {
		pct = 95
	}
	return fmt.Sprintf("[✨ Identified via %s (%d%%)]", src, pct)
}

// ShortBadge returns a compact badge suitable for small widths.
func (r *Result) ShortBadge() string {
	if r == nil {
		return ""
	}
	pct := int(r.Confidence * 100)
	if pct <= 0 {
		pct = 95
	}
	return fmt.Sprintf("[✨ %s %d%%]", r.Source, pct)
}

// SimpleTitle returns "Artist - Title" or just Title.
func (r *Result) SimpleTitle() string {
	if r == nil {
		return ""
	}
	if r.Artist != "" && r.Title != "" {
		return fmt.Sprintf("%s - %s", r.Artist, r.Title)
	}
	if r.Title != "" {
		return r.Title
	}
	return r.Artist
}

// FullDisplay returns a formatted track string including album and release year when available.
func (r *Result) FullDisplay() string {
	if r == nil {
		return ""
	}
	var b strings.Builder
	if r.Artist != "" && r.Title != "" {
		b.WriteString(fmt.Sprintf("%s — %q", r.Artist, r.Title))
	} else if r.Title != "" {
		b.WriteString(r.Title)
	} else if r.Artist != "" {
		b.WriteString(r.Artist)
	}

	if r.Album != "" && r.Year > 0 {
		b.WriteString(fmt.Sprintf(" [%s, %d]", r.Album, r.Year))
	} else if r.Album != "" {
		b.WriteString(fmt.Sprintf(" [%s]", r.Album))
	} else if r.Year > 0 {
		b.WriteString(fmt.Sprintf(" [%d]", r.Year))
	}

	return b.String()
}

// ConfidenceBar returns a visual progress bar representing the match percentage.
func (r *Result) ConfidenceBar(barWidth int) string {
	if r == nil {
		return ""
	}
	if barWidth <= 0 {
		barWidth = 10
	}
	pct := int(r.Confidence * 100)
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	filled := (pct * barWidth) / 100
	empty := barWidth - filled
	if filled > barWidth {
		filled = barWidth
		empty = 0
	}
	return fmt.Sprintf("%s%s %d%%", strings.Repeat("█", filled), strings.Repeat("░", empty), pct)
}
